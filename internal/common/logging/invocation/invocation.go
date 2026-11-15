// Package invocation 提供单次 agent 调用范围内的有界诊断日志（JSONL 落盘）。
//
// 对齐 Python openjiuwen/core/common/logging/invocation.py：每次 invocation 在
// 指定目录写一个 invocation-<id>.jsonl，捕获 INFO 及以上的诊断摘要（脱敏后），
// 有单文件 4MiB / 单记录 2KiB 预算，子任务经 ctx 继承 scope、嵌套 scope 各写各的
// 文件并保留 parent_invocation_id。捕获失败绝不中断 agent 主流程。
//
// 文件目录：
//
//	invocation/
//	├── doc.go           # 包文档
//	└── invocation.go   # 常量/正则/助手 + InvocationLogScope + 作用域入口 + capture
//
// 对应 Python 代码：openjiuwen/core/common/logging/invocation.py
package invocation

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 常量 ────────────────────────────

const (
	// maxFileBytes 单文件字节预算（Python _MAX_FILE_BYTES = 4MiB）
	maxFileBytes = 4 * 1024 * 1024
	// maxRecordBytes 单记录字节上限（Python _MAX_RECORD_BYTES）
	maxRecordBytes = 2048
	// maxJSONChars 结构化消息字符上限（Python _MAX_JSON_CHARS）
	maxJSONChars = 64 * 1024
)

// 正则族逐字对齐 Python（RE2 差异核对：全部无回溯引用，语义等价）
var (
	// secretRE Python _SECRET_RE
	secretRE = regexp.MustCompile(`(?i)(\b(?:api[_-]?key|access[_-]?token|token|secret|password|authorization)\b["']?\s*[:=]\s*)(?:"[^"]*"|'[^']*'|[^\s,;}]+)`)
	// bearerRE Python _BEARER_RE
	bearerRE = regexp.MustCompile(`(?i)\bBearer\s+[^\s,;"']+`)
	// keyRE Python _KEY_RE
	keyRE = regexp.MustCompile(`\b(?:sk|pk)-[A-Za-z0-9_-]{12,}\b`)
	// payloadRE Python _PAYLOAD_RE
	payloadRE = regexp.MustCompile(`(?i)\b(?:prompt|messages?|response_content|input_content|output_content|content|arguments|args|tool_calls|input_data|output_data|request_body|response_body)["']?\s*[:=]`)
	// structureRE Python _STRUCTURE_RE
	structureRE = regexp.MustCompile(`\{\s*"|\[\s*"['{]`)
	// sensitiveTemplateRE Python _SENSITIVE_TEMPLATE_RE
	sensitiveTemplateRE = regexp.MustCompile(`(?i)\b(?:prompt|messages?|query|response|result|request|content|arguments|args|api[_-]?key|token|secret|password|authorization|credentials?)\b`)
	// braceArgRE Python _BRACE_ARG_RE
	braceArgRE = regexp.MustCompile(`\{(\d*)\}`)
	// percentArgRE Python _PERCENT_ARG_RE
	percentArgRE = regexp.MustCompile(`%[sdrf]`)
)

// ──────────────────────────── 非导出函数 ────────────────────────────

// clip 按字节上限裁剪且不产生坏 rune（Python _clip：rune 截断 → encode replace → 字节截断 → decode ignore）
func clip(value string, limit int) string {
	if limit <= 0 {
		return ""
	}
	// Python 第一段 value[:limit] 是按字符截断
	r := []rune(value)
	if len(r) > limit {
		r = r[:limit]
	}
	s := string(r)
	// 第二段 [:limit] 是按字节截断（encode errors=replace 保证字节序列完整）
	if len(s) <= limit {
		return s
	}
	b := []byte(s)[:limit]
	// decode errors=ignore：丢弃尾部残缺字节
	for len(b) > 0 && !utf8.Valid(b) {
		// 从尾部回退到上一个完整 rune 起点
		for len(b) > 0 && b[len(b)-1]&0xC0 == 0x80 {
			b = b[:len(b)-1]
		}
		if len(b) > 0 {
			b = b[:len(b)-1]
		}
	}
	return string(b)
}

// summary 保留诊断文本、丢弃载荷与凭证（Python _summary）
func summary(value any, limit int) string {
	// Python: isinstance(value, Enum) → value.value；Go 无 Enum 入参场景，标量直取
	switch v := value.(type) {
	case nil:
		return ""
	case string:
		return summaryText(v, limit)
	case int, int64, float64, bool:
		return summaryText(fmt.Sprintf("%v", v), limit)
	default:
		// Python: not isinstance((str,int,float,bool)) → "[payload omitted]"
		return "[payload omitted]"
	}
}

// summaryText 对字符串执行脱敏与预算（Python _summary 的字符串主体）
func summaryText(text string, limit int) string {
	// Python: text = str(value)[:8192]
	if len(text) > 8192 {
		text = text[:8192]
	}
	text = bearerRE.ReplaceAllString(text, "Bearer <REDACTED>")
	text = secretRE.ReplaceAllString(text, "$1<REDACTED>")
	text = keyRE.ReplaceAllString(text, "<REDACTED>")
	// Python: payload/structure 首个命中处截断
	loc := payloadRE.FindStringIndex(text)
	if loc == nil {
		loc = structureRE.FindStringIndex(text)
	} else {
		if loc2 := structureRE.FindStringIndex(text); loc2 != nil && loc2[0] < loc[0] {
			loc = loc2
		}
	}
	if loc != nil {
		text = text[:loc[0]] + "[payload omitted]"
	}
	// Python: re.sub(r"[\x00-\x1f\x7f]", " ", text)
	text = regexp.MustCompile(`[\x00-\x1f\x7f]`).ReplaceAllString(text, " ")
	return clip(text, limit)
}

// label 身份标签摘要（Python _label：_summary(value, 96)）
func label(value any) string {
	return summary(value, 96)
}

// formatSummary 简单占位符替换，不展开任意格式宽度（Python _format_summary）
func formatSummary(message any, args []any) string {
	template := summary(message, 768)
	// Python: if _SENSITIVE_TEMPLATE_RE.search(template): return template
	if sensitiveTemplateRE.MatchString(template) {
		return template
	}
	// Python: safe_args = tuple(_summary(arg, 256) for arg in args[:16])
	bound := len(args)
	if bound > 16 {
		bound = 16
	}
	safeArgs := make([]string, bound)
	for i := 0; i < bound; i++ {
		safeArgs[i] = summary(args[i], 256)
	}
	nextIndex := 0
	replace := func(indexed bool, groups []string) string {
		index := nextIndex
		// Python: if match.re is _BRACE_ARG_RE and match.group(1): index = int(...)
		if indexed && groups[1] != "" && braceArgRE.MatchString(groups[0]) {
			fmt.Sscanf(groups[1], "%d", &index)
		}
		nextIndex++
		if index < len(safeArgs) {
			return safeArgs[index]
		}
		return groups[0]
	}
	if braceArgRE.MatchString(template) {
		return braceArgRE.ReplaceAllStringFunc(template, func(m string) string {
			return replace(true, braceArgRE.FindStringSubmatch(m))
		})
	}
	return percentArgRE.ReplaceAllStringFunc(template, func(m string) string {
		return replace(false, []string{m})
	})
}

// messageFields 从 JSON 日志消息提取顶层诊断字段（Python _message_fields）
func messageFields(message any) map[string]any {
	if m, ok := message.(map[string]any); ok {
		return m
	}
	if s, ok := message.(string); ok && strings.HasPrefix(strings.TrimLeft(s, " \t\r\n"), "{") ||
		(ok && strings.HasPrefix(strings.TrimLeft(s, " \t\r\n"), "[")) {
		if len(s) > maxJSONChars {
			return map[string]any{"message": "[oversized structured payload omitted]"}
		}
		var parsed any
		if err := json.Unmarshal([]byte(s), &parsed); err != nil {
			return map[string]any{"message": summary(s, 768)}
		}
		if m, ok := parsed.(map[string]any); ok {
			return m
		}
		return map[string]any{"message": "[structured payload omitted]"}
	}
	return map[string]any{"message": message}
}

// exceptionSummary 异常摘要（Python _exception_summary）。
// Go error 不携带 traceback，位置帧用捕获点 runtime 栈等价提取（前 4 帧，
// Python traceback.extract_tb(limit=4)），偏差登记于提交说明与 spec D 系。
func exceptionSummary(exc error) string {
	if exc == nil {
		return ""
	}
	// 捕获点栈帧位置（file:line:function），跳过本函数与调用侧包装
	var locations string
	pcs := make([]uintptr, 8)
	n := runtime.Callers(3, pcs)
	frames := runtime.CallersFrames(pcs[:n])
	parts := make([]string, 0, 4)
	for i := 0; i < 4; i++ {
		frame, more := frames.Next()
		if frame.Function == "" {
			if !more {
				break
			}
			continue
		}
		parts = append(parts, fmt.Sprintf("%s:%d:%s",
			filepath.Base(frame.File), frame.Line, shortFuncName(frame.Function)))
		if !more {
			break
		}
	}
	locations = strings.Join(parts, "; ")
	detail := summary(exc.Error(), 256)
	return clip(fmt.Sprintf("%T: %s; %s", exc, detail, locations), 512)
}

// shortFuncName 取函数短名（Python Path(frame.filename).name 等价的函数名部分）
func shortFuncName(full string) string {
	if i := strings.LastIndex(full, "/"); i >= 0 {
		full = full[i+1:]
	}
	return full
}

// ──────────────────────────── 结构体 ────────────────────────────

// InvocationLogScope 单次调用的文件与捕获状态（Python InvocationLogScope）。
// Python ContextVar 隐式继承 → Go 经 context.Context 显式传递（spec D-14）。
type InvocationLogScope struct {
	// InvocationID 本次调用唯一标识
	InvocationID string
	// Path JSONL 文件路径
	Path string
	// Truncated 是否发生过预算丢弃
	Truncated bool
	// WriteError 最近一次写失败类型名
	WriteError string
	// LastCaptureError 最近一次畸形记录错误名
	LastCaptureError string
	// BytesWritten 已写字节
	BytesWritten int
	// DroppedRecords 丢弃记录数
	DroppedRecords int

	// identity 身份字段
	identity map[string]string
	// parent 父 scope
	parent *InvocationLogScope
	// previousTrace / previousMember 进入前上下文
	previousTrace  string
	previousMember string
	mu             sync.Mutex
	closed         bool
	stream         *os.File
}

// InvocationSpan 作用域句柄（Python invocation_log_scope contextmanager 等价）
type InvocationSpan struct {
	// Scope 本次调用 scope
	Scope *InvocationLogScope
	// Ctx 注入活动 scope 与身份后的上下文
	Ctx context.Context
	// finish 收尾函数
	finish func(status string, exc error)
}

// ──────────────────────────── 全局变量 ────────────────────────────

// activeInvocationKey 活动 scope 上下文键（Python _ACTIVE_INVOCATION ContextVar 等价）
type activeInvocationKey struct{}

// ──────────────────────────── 导出函数 ────────────────────────────

// ActiveInvocationFrom 读取当前活动 scope（Python _ACTIVE_INVOCATION.get()）
func ActiveInvocationFrom(ctx context.Context) *InvocationLogScope {
	if v, ok := ctx.Value(activeInvocationKey{}).(*InvocationLogScope); ok {
		return v
	}
	return nil
}

// NewInvocationLogScope 构造 scope 并打开独占文件（Python InvocationLogScope.__init__）
func NewInvocationLogScope(directory string, agentID string, sessionID string, role string, parent *InvocationLogScope) (*InvocationLogScope, error) {
	// Python: root = Path(normalize_and_validate_log_path(directory)); root.mkdir(mode=0o750, parents=True, exist_ok=True)
	root, err := logger.NormalizeAndValidateLogPath(directory)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(root, 0o750); err != nil {
		return nil, err
	}
	// Python: root = Path(normalize_and_validate_log_path(root))（归一化回读）
	root, err = logger.NormalizeAndValidateLogPath(root)
	if err != nil {
		return nil, err
	}
	s := &InvocationLogScope{parent: parent}
	// Python: self.invocation_id = uuid4().hex
	s.InvocationID = strings.ReplaceAll(uuid.NewString(), "-", "")
	// Python: self.path = root / f"invocation-{id}.jsonl"
	s.Path = filepath.Join(root, "invocation-"+s.InvocationID+".jsonl")
	// Python: if self.path.parent != root: raise ValueError(...)
	// Go：EvalSymlinks 归一化后比对，防符号链接逃逸
	if realParent, err := filepath.EvalSymlinks(filepath.Dir(s.Path)); err == nil {
		realRoot, err2 := filepath.EvalSymlinks(root)
		if err2 != nil || realParent != realRoot {
			return nil, errors.New("Invocation log must remain inside its configured directory")
		}
	}
	// Python: identity = {...}（_label 摘要）；parent 为 nil 时 parent_invocation_id 为空串
	parentInvocationID := ""
	if parent != nil {
		parentInvocationID = parent.InvocationID
	}
	s.identity = map[string]string{
		"agent_id":             label(agentID),
		"session_id":           label(sessionID),
		"invocation_id":        s.InvocationID,
		"parent_invocation_id": parentInvocationID,
		"role":                 label(role),
	}
	// Python: flags = O_WRONLY|O_CREAT|O_EXCL|O_NOFOLLOW, 0o600
	s.stream, err = os.OpenFile(s.Path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return nil, err
	}
	return s, nil
}

// StartInvocationLog 启动调用日志作用域（Python invocation_log_scope）。
// 返回 span（含注入后的 ctx）与收尾函数；start 记录在此写入。
func StartInvocationLog(parent context.Context, directory string, agentID string, sessionID string, role string) (*InvocationSpan, error) {
	scope, err := NewInvocationLogScope(directory, agentID, sessionID, role, ActiveInvocationFrom(parent))
	if err != nil {
		return nil, err
	}
	// Python: previous_trace = get_session_id(); previous_member = get_member_id()
	scope.previousTrace = logger.SessionIDFrom(parent)
	scope.previousMember = logger.MemberIDFrom(parent)
	// Python: token = _ACTIVE_INVOCATION.set(scope); set_session_id(session_id or previous_trace); set_member_id(agent_id or previous_member)
	ctx := context.WithValue(parent, activeInvocationKey{}, scope)
	if sessionID != "" {
		ctx = logger.WithSessionID(ctx, sessionID)
	}
	if agentID != "" {
		ctx = logger.WithMemberID(ctx, agentID)
	}
	span := &InvocationSpan{Scope: scope, Ctx: ctx}
	// Python: scope.write_record("INFO", "invocation", {...invocation_start...})
	scope.WriteRecord("INFO", "invocation", map[string]any{
		"event_type": "invocation_start", "status": "running",
	}, false)
	span.finish = func(status string, exc error) {
		exception := exceptionSummary(exc)
		level := "INFO"
		if status != "success" {
			level = "ERROR"
		}
		// Python: finally { try { write_record(..., final=True) } finally { close; reset; restore } }
		scope.WriteRecord(level, "invocation", map[string]any{
			"event_type": "invocation_end", "status": status, "exception": exception,
		}, true)
		scope.CloseLog()
	}
	return span, nil
}

// Finish 结束作用域并写 end 记录（Python invocation_log_scope 的 except/finally）。
// exc==nil → success；context.Canceled → cancelled（Python CancelledError 分支）；否则 failure。
func (s *InvocationSpan) Finish(exc error) {
	status := "success"
	switch {
	case exc == nil:
	case errors.Is(exc, context.Canceled):
		// Python: isinstance(exc, (CancelledError, GeneratorExit, KeyboardInterrupt)) → cancelled
		status = "cancelled"
	default:
		status = "failure"
	}
	s.finish(status, exc)
}

// WriteRecord 写一条 JSONL 记录（Python InvocationLogScope.write_record）
func (s *InvocationLogScope) WriteRecord(level string, logType string, fields map[string]any, final bool) {
	ctx := s.ctxForFields()
	record := map[string]any{
		"timestamp": utcTimestamp(),
		"level":     label(level),
	}
	for key, value := range s.identity {
		record[key] = value
	}
	record["trace_id"] = label(logger.SessionIDFrom(ctx))
	record["member_id"] = label(logger.MemberIDFrom(ctx))
	record["log_type"] = label(logType)
	record["message"] = summary(fields["message"], 768)
	record["exception"] = summary(fields["exception"], 512)
	record["event_type"] = label(fields["event_type"])
	record["status"] = label(fields["status"])

	s.mu.Lock()
	defer s.mu.Unlock()
	// Python: if self._closed or self.write_error: return
	if s.closed || s.WriteError != "" {
		return
	}
	if final {
		record["truncated"] = s.Truncated
		record["dropped_records"] = s.DroppedRecords
	}
	encoded := mustJSONLine(record)
	// Python: while len(encoded) > _MAX_RECORD_BYTES: 收缩循环
	for len(encoded) > maxRecordBytes {
		if record["message"] == "" && record["exception"] == "" {
			for _, key := range []string{"role", "trace_id", "member_id", "event_type", "status"} {
				record[key] = clip(record[key].(string), 32)
			}
		}
		for _, key := range []string{"message", "exception"} {
			record[key] = clip(record[key].(string), len([]byte(record[key].(string)))/2)
		}
		encoded = mustJSONLine(record)
	}
	// Python: ceiling = _MAX_FILE_BYTES if final else _MAX_FILE_BYTES - _MAX_RECORD_BYTES
	ceiling := maxFileBytes
	if !final {
		ceiling = maxFileBytes - maxRecordBytes
	}
	if s.BytesWritten+len(encoded) > ceiling {
		// Python: self.truncated = True; self.dropped_records += 1; return
		s.Truncated = true
		s.DroppedRecords++
		return
	}
	// Python: stream.write + flush + bytes_written += len; final → closed = True
	if _, err := s.stream.Write(encoded); err != nil {
		// 捕获失败绝不中断 agent 或递归进入日志
		s.WriteError = "*fs.PathError"
		return
	}
	s.stream.Sync()
	s.BytesWritten += len(encoded)
	if final {
		s.closed = true
	}
}

// RecordCaptureFailure 计一次畸形记录（Python record_capture_failure）
func (s *InvocationLogScope) RecordCaptureFailure(errorName string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.DroppedRecords++
	s.LastCaptureError = errorName
}

// CloseLog 关闭文件（Python close_log）
func (s *InvocationLogScope) CloseLog() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.closed = true
	if s.stream != nil {
		if err := s.stream.Close(); err != nil {
			s.WriteError = "*fs.PathError"
		}
	}
}

// SuspendContext 挂起：返回恢复父上下文的临时 ctx 与还原函数（Python suspend）。
// 仅当前活动 scope 可挂起自身；否则原样返回。
func (s *InvocationLogScope) SuspendContext(ctx context.Context) (context.Context, func()) {
	if ActiveInvocationFrom(ctx) != s {
		return ctx, func() {}
	}
	// Python: token = _ACTIVE_INVOCATION.set(self._parent_scope); set_session_id(self._previous_trace); set_member_id(self._previous_member)
	currentTrace := logger.SessionIDFrom(ctx)
	currentMember := logger.MemberIDFrom(ctx)
	suspended := context.WithValue(ctx, activeInvocationKey{}, s.parent)
	suspended = logger.WithSessionID(suspended, s.previousTrace)
	suspended = logger.WithMemberID(suspended, s.previousMember)
	return suspended, func() {
		// Python finally: reset(token); set_session_id(current_trace); set_member_id(current_member)
		_ = currentTrace
		_ = currentMember
	}
}

// ParentInvocationID 父调用标识
func (s *InvocationLogScope) ParentInvocationID() string { return s.identity["parent_invocation_id"] }

// ──────────────────────────── 非导出函数 ────────────────────────────

// ctxForFields 写记录时用的上下文：scope 自身进入时的身份快照。
// Python 在 scope 生存期内全局 session/member 已被 set，等价于记录身份快照。
func (s *InvocationLogScope) ctxForFields() context.Context {
	ctx := context.Background()
	ctx = logger.WithSessionID(ctx, s.previousTrace)
	ctx = logger.WithMemberID(ctx, s.previousMember)
	if v, ok := s.identity["session_id"]; ok {
		ctx = logger.WithSessionID(ctx, v)
	}
	if v, ok := s.identity["agent_id"]; ok {
		ctx = logger.WithMemberID(ctx, v)
	}
	return ctx
}

// utcTimestamp UTC ISO8601 时间戳（Python datetime.now(timezone.utc).isoformat()）
func utcTimestamp() string {
	return time.Now().UTC().Format("2006-01-02T15:04:05.000000+00:00")
}

// mustJSONLine 紧凑 JSON + 换行（Python json.dumps(..., ensure_ascii=False)+"\n"）
func mustJSONLine(record map[string]any) []byte {
	data, err := json.Marshal(record)
	if err != nil {
		data = []byte("{}")
	}
	return append(data, '\n')
}

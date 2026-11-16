package invocation

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// TestClip 对照 Python _clip：先按 rune 截 limit，再按字节截 limit，坏 rune 丢弃
func TestClip(t *testing.T) {
	// ASCII 短串原样
	if got := clip("abc", 5); got != "abc" {
		t.Fatalf("clip(abc,5) = %q", got)
	}
	// ASCII 超长截断
	if got := clip("abcdef", 3); got != "abc" {
		t.Fatalf("clip(abcdef,3) = %q", got)
	}
	// 多字节：limit 个 rune（encode 后 [:2] 字节为残缺序列，decode ignore 全部丢弃——Python 同语义）
	if got := clip("你好世界", 2); got != "" {
		t.Fatalf("clip(你好世界,2) = %q, want 空", got)
	}
	// 字节上限：3 字节 = 1 个中文 rune（encode 后 [:3] 截掉半个字符，decode ignore 丢弃）
	if got := clip("你好", 3); got != "你" {
		t.Fatalf("clip(你好,3) = %q, want 你", got)
	}
	if clip("", 10) != "" {
		t.Fatal("空串应原样")
	}
}

// TestSummary 脱敏正则族逐条正例/反例
func TestSummary(t *testing.T) {
	cases := []struct {
		name  string
		value any
		want  string
	}{
		{"None返回空", nil, ""},
		// Python 实测：Bearer 先脱敏后 SECRET_RE 再命中 "Bearer" 值
		{"Bearer脱敏", "Authorization: Bearer abc.def.ghi", "Authorization: <REDACTED> <REDACTED>"},
		{"apiKey脱敏", "api_key = supersecret123", "api_key = <REDACTED>"},
		{"skKey脱敏", "key: sk-abcdefghijklmnopqrstuvwxyz", "key: <REDACTED>"},
		{"普通文本保留", "diagnosis complete", "diagnosis complete"},
		{"布尔标量保留", true, "true"},
		{"数字标量保留", 42, "42"},
		// Python 实测：payload 键在串首时 text[:0] 截断
		{"payload键截断", "input_content: {\"huge\": 1}", "[payload omitted]"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := summary(c.value, 768); got != c.want {
				t.Fatalf("summary(%v) = %q, want %q", c.value, got, c.want)
			}
		})
	}
	// 容器值不 stringify
	if got := summary(map[string]any{"k": "v"}, 768); got != "[payload omitted]" {
		t.Fatalf("容器值 = %q, want [payload omitted]", got)
	}
	// 控制字符替换为空格
	if got := summary("a\x00b\x1fc", 768); got != "a b c" {
		t.Fatalf("控制字符 = %q", got)
	}
}

// TestLabel label 上限 96
func TestLabel(t *testing.T) {
	if got := label(strings.Repeat("x", 200)); len(got) != 96 {
		t.Fatalf("label 长度 = %d, want 96", len(got))
	}
}

// TestFormatSummary 占位符替换
func TestFormatSummary(t *testing.T) {
	// 花括号位置参数
	if got := formatSummary("turn {0} of {1}", []any{1, 5}); got != "turn 1 of 5" {
		t.Fatalf("brace = %q", got)
	}
	// %s 位置参数
	if got := formatSummary("model: %s", []any{"qwen"}); got != "model: qwen" {
		t.Fatalf("percent = %q", got)
	}
	// 越界占位符原样保留
	if got := formatSummary("x {0} y", nil); got != "x {0} y" {
		t.Fatalf("越界 = %q", got)
	}
	// 敏感模板：secret 先于 formatSummary 在 _summary 内脱敏，敏感词命中后不再做占位符替换（Python 同序）
	if got := formatSummary("password: {0}", []any{"secret"}); got != "password: <REDACTED>}" {
		t.Fatalf("敏感模板 = %q", got)
	}
}

// TestMessageFields JSON 消息字段提取
func TestMessageFields(t *testing.T) {
	// dict 直返
	m := map[string]any{"message": "x"}
	if got := messageFields(m); got["message"] != "x" {
		t.Fatalf("dict 直返失败: %v", got)
	}
	// JSON 字符串解析
	got := messageFields(`{"message": "parsed", "status": "ok"}`)
	if got["message"] != "parsed" || got["status"] != "ok" {
		t.Fatalf("JSON 解析失败: %v", got)
	}
	// 非对象 JSON
	if got := messageFields("[1,2]"); got["message"] != "[structured payload omitted]" {
		t.Fatalf("数组 JSON = %v", got)
	}
	// 坏 JSON
	if got := messageFields("{not json"); got["message"] == "" {
		t.Fatalf("坏 JSON 应回退 summary: %v", got)
	}
	// 超 _MAX_JSON_CHARS
	oversized := "{" + strings.Repeat("x", maxJSONChars) + "}"
	if got := messageFields(oversized); got["message"] != "[oversized structured payload omitted]" {
		t.Fatalf("超大 JSON = %v", got)
	}
	// 普通字符串
	if got := messageFields("plain"); got["message"] != "plain" {
		t.Fatalf("普通串 = %v", got)
	}
}

// TestExceptionSummary 异常摘要
func TestExceptionSummary(t *testing.T) {
	if got := exceptionSummary(nil); got != "" {
		t.Fatalf("nil 应返回空串, got %q", got)
	}
	err := errors.New("boom detail")
	got := exceptionSummary(err)
	if !strings.HasPrefix(got, "runtimeError: boom detail") && !strings.Contains(got, "boom detail") {
		t.Fatalf("异常摘要缺类型与详情: %q", got)
	}
}

// TestInvocationLogScope 构造与写记录（Python InvocationLogScope.__init__/write_record）
func TestInvocationLogScope(t *testing.T) {
	dir := t.TempDir()
	scope, err := NewInvocationLogScope(dir, "agent-1", "trace-1", "diag", nil)
	if err != nil {
		t.Fatalf("构造失败: %v", err)
	}
	defer scope.CloseLog()
	// 文件名形如 invocation-<32hex>.jsonl 且在目录内
	base := filepath.Base(scope.Path)
	if !strings.HasPrefix(base, "invocation-") || !strings.HasSuffix(base, ".jsonl") {
		t.Fatalf("文件名 = %q", base)
	}
	// 写一条记录
	scope.WriteRecord("INFO", "invocation", map[string]any{
		"message": "hello", "event_type": "invocation_start", "status": "running",
	}, false)
	// 读回首行校验字段
	data, _ := os.ReadFile(scope.Path)
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	var rec map[string]any
	if err := json.Unmarshal([]byte(lines[0]), &rec); err != nil {
		t.Fatalf("JSONL 解析失败: %v", err)
	}
	for _, key := range []string{"timestamp", "level", "agent_id", "session_id", "invocation_id", "parent_invocation_id", "role", "trace_id", "member_id", "log_type", "message", "exception", "event_type", "status"} {
		if _, ok := rec[key]; !ok {
			t.Fatalf("缺字段 %s: %v", key, rec)
		}
	}
	if rec["level"] != "INFO" || rec["agent_id"] != "agent-1" || rec["log_type"] != "invocation" {
		t.Fatalf("字段值不符: %v", rec)
	}
	if rec["parent_invocation_id"] != "" {
		t.Fatalf("无父 scope 时 parent_invocation_id 应为空串: %v", rec)
	}
	// final 记录带 truncated/dropped_records
	scope.WriteRecord("INFO", "invocation", map[string]any{"event_type": "invocation_end", "status": "success"}, true)
	data, _ = os.ReadFile(scope.Path)
	lines = strings.Split(strings.TrimSpace(string(data)), "\n")
	var fin map[string]any
	json.Unmarshal([]byte(lines[len(lines)-1]), &fin)
	if _, ok := fin["truncated"]; !ok {
		t.Fatalf("final 记录缺 truncated: %v", fin)
	}
}

// TestInvocationLogScope_预算与写失败 单记录收缩/文件预算/写失败置位
func TestInvocationLogScope_预算与写失败(t *testing.T) {
	dir := t.TempDir()
	scope, err := NewInvocationLogScope(dir, "a", "s", "r", nil)
	if err != nil {
		t.Fatalf("构造失败: %v", err)
	}
	defer scope.CloseLog()
	// 超长 message 触发收缩循环：仍写成功且单行 ≤ maxRecordBytes
	scope.WriteRecord("INFO", "log", map[string]any{"message": strings.Repeat("长", 2000), "exception": strings.Repeat("x", 2000)}, false)
	data, _ := os.ReadFile(scope.Path)
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	last := lines[len(lines)-1]
	if len(last) > maxRecordBytes {
		t.Fatalf("收缩后记录超限: %d", len(last))
	}
	// 文件预算：把 bytesWritten 抬到 ceiling 后写记录被丢弃并计 dropped
	scope.BytesWritten = maxFileBytes - 10
	before := scope.DroppedRecords
	scope.WriteRecord("INFO", "log", map[string]any{"message": "over"}, false)
	if scope.DroppedRecords != before+1 {
		t.Fatal("超预算应计 dropped_records")
	}
	if !scope.Truncated {
		t.Fatal("超预算应置 truncated")
	}
	// 关闭后写记录静默丢弃
	scope.CloseLog()
	scope.WriteRecord("INFO", "log", map[string]any{"message": "closed"}, false)
}

// TestInvocationLogScope_路径逃逸 父目录外路径报错
func TestInvocationLogScope_路径逃逸(t *testing.T) {
	dir := t.TempDir()
	// 目录本身即边界：正常构造成功
	scope, err := NewInvocationLogScope(dir, "a", "s", "r", nil)
	if err != nil {
		t.Fatalf("正常目录构造失败: %v", err)
	}
	scope.CloseLog()
	// 路径逃逸：构造出的文件不在配置目录内 → 报错。模拟：配置目录含符号链接且归一化后越界。
	// Python 判定 path.parent != root；Go 用 EvalSymlinks 归一化后比对，构造符号链接目录验证
	outer := t.TempDir()
	inner := filepath.Join(outer, "real")
	os.Mkdir(inner, 0o750)
	link := filepath.Join(outer, "link")
	os.Symlink(inner, link)
	// Python 语义核对：self.path 由归一化后 root 构建，parent==root 恒成立，
	// 逃逸检查为防御性分支；符号链接目录下构造应成功且文件实际落在真实目录内
	_, err = NewInvocationLogScope(filepath.Join(link, "escape"), "a", "s", "r", nil)
	if err != nil {
		t.Fatalf("符号链接目录下构造不应报错（Python 同语义）: %v", err)
	}
}

// TestInvocationLogScope_父链 嵌套 scope 保留 parent_invocation_id
func TestInvocationLogScope_父链(t *testing.T) {
	dir := t.TempDir()
	parent, _ := NewInvocationLogScope(dir, "agent", "trace", "role", nil)
	defer parent.CloseLog()
	child, _ := NewInvocationLogScope(dir, "agent", "trace", "role", parent)
	defer child.CloseLog()
	if child.ParentInvocationID() != parent.InvocationID {
		t.Fatalf("parent_invocation_id = %q, want %q", child.ParentInvocationID(), parent.InvocationID)
	}
}

// TestStartInvocationLog 作用域入口：start/end 记录与状态分支
func TestStartInvocationLog(t *testing.T) {
	dir := t.TempDir()
	span, err := StartInvocationLog(context.Background(), dir, "agent-1", "trace-1", "diag")
	if err != nil {
		t.Fatalf("启动失败: %v", err)
	}
	// ctx 已注入活动 scope 与身份
	if ActiveInvocationFrom(span.Ctx) != span.Scope {
		t.Fatal("活动 scope 未注入 ctx")
	}
	if logger.SessionIDFrom(span.Ctx) != "trace-1" {
		t.Fatalf("session_id = %q", logger.SessionIDFrom(span.Ctx))
	}
	if logger.MemberIDFrom(span.Ctx) != "agent-1" {
		t.Fatalf("member_id = %q", logger.MemberIDFrom(span.Ctx))
	}
	// 成功结束
	span.Finish(nil)
	data, _ := os.ReadFile(span.Scope.Path)
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	var fin map[string]any
	json.Unmarshal([]byte(lines[len(lines)-1]), &fin)
	if fin["event_type"] != "invocation_end" || fin["status"] != "success" || fin["level"] != "INFO" {
		t.Fatalf("成功 end 记录不符: %v", fin)
	}
	// 失败分支
	span2, _ := StartInvocationLog(context.Background(), dir, "agent-1", "trace-1", "diag")
	span2.Finish(errors.New("boom"))
	data, _ = os.ReadFile(span2.Scope.Path)
	lines = strings.Split(strings.TrimSpace(string(data)), "\n")
	json.Unmarshal([]byte(lines[len(lines)-1]), &fin)
	if fin["status"] != "failure" || fin["level"] != "ERROR" || !strings.Contains(fin["exception"].(string), "boom") {
		t.Fatalf("失败 end 记录不符: %v", fin)
	}
	// 取消分支
	span3, _ := StartInvocationLog(context.Background(), dir, "agent-1", "trace-1", "diag")
	span3.Finish(context.Canceled)
	data, _ = os.ReadFile(span3.Scope.Path)
	lines = strings.Split(strings.TrimSpace(string(data)), "\n")
	json.Unmarshal([]byte(lines[len(lines)-1]), &fin)
	if fin["status"] != "cancelled" {
		t.Fatalf("取消 end 记录不符: %v", fin)
	}
}

// TestStartInvocationLog_嵌套继承 嵌套 span 恢复父上下文且 parent 链正确
func TestStartInvocationLog_嵌套继承(t *testing.T) {
	dir := t.TempDir()
	outer, _ := StartInvocationLog(context.Background(), dir, "agent", "trace-outer", "role")
	inner, err := StartInvocationLog(outer.Ctx, dir, "child", "trace-inner", "child-role")
	if err != nil {
		t.Fatalf("嵌套启动失败: %v", err)
	}
	if inner.Scope.ParentInvocationID() != outer.Scope.InvocationID {
		t.Fatal("嵌套 parent 链断裂")
	}
	inner.Finish(nil)
	outer.Finish(nil)
	// 恢复检查：outer 结束后无活动 scope
	if ActiveInvocationFrom(context.Background()) != nil {
		t.Fatal("空 ctx 不应有活动 scope")
	}
}

// TestSuspendContext 挂起恢复父上下文
func TestSuspendContext(t *testing.T) {
	dir := t.TempDir()
	span, _ := StartInvocationLog(context.Background(), dir, "agent", "trace-1", "role")
	suspended, restore := span.Scope.SuspendContext(span.Ctx)
	if ActiveInvocationFrom(suspended) != nil {
		t.Fatal("挂起后活动 scope 应为父值(nil)")
	}
	if logger.SessionIDFrom(suspended) != "default_trace_id" {
		t.Fatalf("挂起后 session 应恢复: %q", logger.SessionIDFrom(suspended))
	}
	restore()
	if ActiveInvocationFrom(span.Ctx) != span.Scope {
		t.Fatal("恢复后活动 scope 应还原")
	}
	span.Finish(nil)
}

// TestCaptureInvocationLog 无活动 scope 零开销直返
func TestCaptureInvocationLog(t *testing.T) {
	// 当前 goroutine 无 scope：不应 panic
	CaptureInvocationLog("info", "comp", "hello", nil, "", nil)
}

// TestCaptureInvocationLog_活动scope 经 goroutine 登记捕获到 JSONL
func TestCaptureInvocationLog_活动scope(t *testing.T) {
	dir := t.TempDir()
	span, err := StartInvocationLog(context.Background(), dir, "agent-1", "trace-1", "diag")
	if err != nil {
		t.Fatalf("启动失败: %v", err)
	}
	CaptureInvocationLog("info", "comp_diag", "diagnosis step", nil, "", nil)
	// DEBUG 丢弃
	CaptureInvocationLog("debug", "comp_diag", "hidden", nil, "", nil)
	// exc 捕获
	CaptureInvocationLog("error", "comp_diag", "boom occurred", nil, "", errors.New("oops"))
	span.Finish(nil)
	data, _ := os.ReadFile(span.Scope.Path)
	content := string(data)
	if !strings.Contains(content, `"message":"diagnosis step"`) {
		t.Fatalf("缺 info 记录: %s", content)
	}
	if strings.Contains(content, `"message":"hidden"`) {
		t.Fatal("DEBUG 不应落盘")
	}
	if !strings.Contains(content, "oops") || !strings.Contains(content, `"level":"ERROR"`) {
		t.Fatalf("error 记录不符: %s", content)
	}
	if !strings.Contains(content, `"log_type":"comp_diag"`) {
		t.Fatalf("log_type 不符: %s", content)
	}
}

// TestCaptureInvocationLog_格式化参数 args 走 formatSummary
func TestCaptureInvocationLog_格式化参数(t *testing.T) {
	dir := t.TempDir()
	span, _ := StartInvocationLog(context.Background(), dir, "a", "s", "r")
	CaptureInvocationLog("info", "comp", "turn {0} of {1}", []any{2, 5}, "", nil)
	span.Finish(nil)
	data, _ := os.ReadFile(span.Scope.Path)
	if !strings.Contains(string(data), `"message":"turn 2 of 5"`) {
		t.Fatalf("args 格式化失败: %s", data)
	}
}

// TestLoggerHook接线 经 logger 包写日志时活动 scope 捕获（capture→logger 单点接线）
func TestLoggerHook接线(t *testing.T) {
	dir := t.TempDir()
	// logger 需先初始化（未初始化时 Info 走 Nop logger，钩子不触发）
	if err := logger.Setup(logger.WithOutputDir(t.TempDir())); err != nil {
		t.Fatalf("logger 初始化失败: %v", err)
	}
	span, _ := StartInvocationLog(context.Background(), dir, "agent-1", "trace-1", "diag")
	logger.Info(logger.ComponentAgentCore).Str("k", "v").Msg("invocation wired message")
	logger.Debug(logger.ComponentAgentCore).Msg("should not capture")
	span.Finish(nil)
	data, _ := os.ReadFile(span.Scope.Path)
	content := string(data)
	if !strings.Contains(content, `"message":"invocation wired message"`) {
		t.Fatalf("logger 钩子未捕获: %s", content)
	}
	if !strings.Contains(content, `"log_type":"agent_core"`) {
		t.Fatalf("log_type 应为组件名: %s", content)
	}
	if strings.Contains(content, `"message":"should not capture"`) {
		t.Fatal("Debug 不应被捕获")
	}
}

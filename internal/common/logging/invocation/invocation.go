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
	"encoding/json"
	"fmt"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"unicode/utf8"
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

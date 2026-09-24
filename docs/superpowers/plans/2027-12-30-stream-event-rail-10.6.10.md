# StreamEvent Rail（10.6.10）实现计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 全量一比一对齐 Python `JiuClawStreamEventRail`，实现流式事件发射、暂停/中止检查点、上下文修复三大功能，并回填 DeepAdapter 中 6 处占位。

**Architecture:** `JiuClawStreamEventRail` 嵌入 `rails.DeepAgentRail`，实现 6 个 AgentRail 钩子 + 对外 API（Pause/Resume/Abort/ResetAbort/ResetForNewTask/CleanupSession/Collect/Get/ClearCancelledToolResults）。Per-session 状态统一管理在 `streamSessionState` 中，使用 `sync.Cond` 实现暂停/中止阻塞检查点。通过 `Session.WriteStream(ctx, OutputSchema{...})` 发射流式事件。

**Tech Stack:** Go 1.26, `github.com/RealAlexandreAI/json-repair`, 项目内已有的 `rails.DeepAgentRail` / `sessioninterfaces.SessionFacade` / `stream.OutputSchema` / `ceinterface.ModelContext` / `interrupt.ConvertInteractionsToAskUserQuestion` / `todo.TodoTool`

---

## File Structure

| 操作 | 文件 | 职责 |
|------|------|------|
| Create | `internal/swarm/agents/harness/common/rails/stream_event_helpers.go` | 辅助函数：boolishFalse/True, nonzeroExit, inferToolResultError, structuredToolResultPayload, parseToolCallArguments, extractToolInterrupt, askUserQuestionPayloadFromInterrupt, truncateString, 常量 |
| Create | `internal/swarm/agents/harness/common/rails/stream_event_helpers_test.go` | 辅助函数单元测试 |
| Create | `internal/swarm/agents/harness/common/rails/stream_event_emit.go` | 流式事件发射：emitToolCall/emitToolResult/emitToolUpdate/emitContextUsage/emitAskUserQuestionIfInterrupted/emitTodoUpdated, formatTodosForFrontend |
| Create | `internal/swarm/agents/harness/common/rails/stream_event_emit_test.go` | 发射方法单元测试 |
| Create | `internal/swarm/agents/harness/common/rails/stream_event_context.go` | 上下文修复：fixIncompleteToolContext, ensureJSONArguments, fixMissingQuotes |
| Create | `internal/swarm/agents/harness/common/rails/stream_event_context_test.go` | 上下文修复单元测试 |
| Create | `internal/swarm/agents/harness/common/rails/stream_event_rail.go` | 核心结构体：JiuClawStreamEventRail + streamSessionState + inflightToolInfo, 对外API, 钩子方法, GetCallbacks, resolveSID, checkpoint, getOrCreateState |
| Create | `internal/swarm/agents/harness/common/rails/stream_event_rail_test.go` | 核心结构体单元测试 |
| Modify | `internal/swarm/agents/harness/common/rails/doc.go` | 更新文件目录 |
| Modify | `internal/swarm/server/adapter/deep_adapter.go` | streamEventRail 字段类型 + 6处占位回填 |
| Modify | `internal/swarm/server/adapter/deep_adapter_rails.go` | buildStreamEventRail 实现 |
| Modify | `internal/swarm/server/hooks/user_hook_rail.go` | 修正优先级注释 50→80 |
| Modify | `go.mod` / `go.sum` | 引入 json-repair 依赖 |
| Modify | `IMPLEMENTATION_PLAN.md` | 更新 10.6.3-10 状态 |

---

### Task 1: 引入 json-repair 依赖

**Files:**
- Modify: `go.mod`, `go.sum`

- [ ] **Step 1: 安装 json-repair**

Run: `cd /home/opensource/uapclaw-gateway && export GOPROXY=https://goproxy.cn,direct && go get github.com/RealAlexandreAI/json-repair`

Expected: go.mod 和 go.sum 中新增该依赖

- [ ] **Step 2: 验证依赖可用**

Run: `cd /home/opensource/uapclaw-gateway && go list -m github.com/RealAlexandreAI/json-repair`

Expected: 输出版本号

- [ ] **Step 3: Commit**

```bash
git add go.mod go.sum
git commit -m "chore: 引入 json-repair 依赖（StreamEvent Rail 10.6.10）"
```

---

### Task 2: 辅助函数 — stream_event_helpers.go

**Files:**
- Create: `internal/swarm/agents/harness/common/rails/stream_event_helpers.go`
- Create: `internal/swarm/agents/harness/common/rails/stream_event_helpers_test.go`

- [ ] **Step 1: 写辅助函数测试**

```go
// File: internal/swarm/agents/harness/common/rails/stream_event_helpers_test.go
package rails

import (
	"encoding/json"
	"testing"

	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	saschema "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/schema"
)

func TestBoolishFalse(t *testing.T) {
	tests := []struct {
		input any
		want  bool
	}{
		{false, true},
		{"false", true},
		{"False", true},
		{"FALSE", true},
		{"0", true},
		{"no", true},
		{"NO", true},
		{true, false},
		{"true", false},
		{1, false},
		{"yes", false},
		{nil, false},
	}
	for _, tt := range tests {
		if got := boolishFalse(tt.input); got != tt.want {
			t.Errorf("boolishFalse(%v) = %v, want %v", tt.input, got, tt.want)
		}
	}
}

func TestBoolishTrue(t *testing.T) {
	tests := []struct {
		input any
		want  bool
	}{
		{true, true},
		{"true", true},
		{"1", true},
		{"yes", true},
		{false, false},
		{"false", false},
		{0, false},
		{nil, false},
	}
	for _, tt := range tests {
		if got := boolishTrue(tt.input); got != tt.want {
			t.Errorf("boolishTrue(%v) = %v, want %v", tt.input, got, tt.want)
		}
	}
}

func TestNonzeroExit(t *testing.T) {
	tests := []struct {
		input any
		want  *bool
	}{
		{false, nil},            // bool → nil
		{0, boolPtr(false)},     // int 0 → 非非零
		{1, boolPtr(true)},      // int 1 → 非零
		{-1, boolPtr(true)},     // int -1 → 非零
		{"0", boolPtr(false)},   // string "0"
		{"1", boolPtr(true)},    // string "1"
		{"abc", nil},            // 无法解析
		{nil, nil},
	}
	for _, tt := range tests {
		got := nonzeroExit(tt.input)
		if (got == nil) != (tt.want == nil) {
			t.Errorf("nonzeroExit(%v) = %v, want %v", tt.input, got, tt.want)
		} else if got != nil && *got != *tt.want {
			t.Errorf("nonzeroExit(%v) = %v, want %v", tt.input, *got, *tt.want)
		}
	}
}

func TestInferToolResultError(t *testing.T) {
	// success=false
	got := inferToolResultError(map[string]any{"success": false})
	assertBoolPtr(t, got, true)

	// success=true
	got = inferToolResultError(map[string]any{"success": true})
	assertBoolPtr(t, got, false)

	// is_error=true
	got = inferToolResultError(map[string]any{"is_error": true})
	assertBoolPtr(t, got, true)

	// status="error"
	got = inferToolResultError(map[string]any{"status": "error"})
	assertBoolPtr(t, got, true)

	// exit_code != 0
	got = inferToolResultError(map[string]any{"exit_code": 1})
	assertBoolPtr(t, got, true)

	// exit_code == 0
	got = inferToolResultError(map[string]any{"exit_code": 0})
	assertBoolPtr(t, got, false)

	// nested data
	got = inferToolResultError(map[string]any{"data": map[string]any{"success": false}})
	assertBoolPtr(t, got, true)

	// normal string
	got = inferToolResultError("some result")
	if got != nil {
		t.Errorf("expected nil for plain string, got %v", got)
	}

	// [ERROR] prefix
	got = inferToolResultError("[ERROR] something failed")
	assertBoolPtr(t, got, true)

	// nil
	got = inferToolResultError(nil)
	if got != nil {
		t.Errorf("expected nil, got %v", got)
	}
}

func TestStructuredToolResultPayload(t *testing.T) {
	if structuredToolResultPayload(map[string]any{"a": 1}) == nil {
		t.Error("expected non-nil for dict")
	}
	if structuredToolResultPayload([]any{1, 2}) == nil {
		t.Error("expected non-nil for list")
	}
	if structuredToolResultPayload("string") != nil {
		t.Error("expected nil for string")
	}
	if structuredToolResultPayload(42) != nil {
		t.Error("expected nil for int")
	}
}

func TestParseToolCallArguments(t *testing.T) {
	// dict args
	got := parseToolCallArguments(&llmschema.ToolCall{Arguments: `{"key": "value"}`})
	if got["key"] != "value" {
		t.Errorf("expected key=value, got %v", got)
	}

	// invalid JSON
	got = parseToolCallArguments(&llmschema.ToolCall{Arguments: `not json`})
	if len(got) != 0 {
		t.Errorf("expected empty map for invalid JSON, got %v", got)
	}

	// nil tool call
	got = parseToolCallArguments(nil)
	if len(got) != 0 {
		t.Errorf("expected empty map for nil, got %v", got)
	}
}

func TestExtractToolInterrupt(t *testing.T) {
	// ToolInterruptException
	exc := &saschema.ToolInterruptException{Request: testInterruptRequest("test")}
	got := extractToolInterrupt(exc)
	if got == nil {
		t.Error("expected non-nil for ToolInterruptException")
	}

	// nested cause
	wrapper := &testError{cause: exc}
	got = extractToolInterrupt(wrapper)
	if got == nil {
		t.Error("expected non-nil for nested ToolInterruptException")
	}

	// no interrupt
	got = extractToolInterrupt(fmt.Errorf("plain error"))
	if got != nil {
		t.Error("expected nil for plain error")
	}

	// nil
	got = extractToolInterrupt(nil)
	if got != nil {
		t.Error("expected nil for nil input")
	}
}

func TestTruncateString(t *testing.T) {
	if truncateString("hello", 3) != "hel" {
		t.Errorf("expected 'hel', got '%s'", truncateString("hello", 3))
	}
	if truncateString("hi", 10) != "hi" {
		t.Errorf("expected 'hi', got '%s'", truncateString("hi", 10))
	}
}

func TestTodoToolNames(t *testing.T) {
	if !todoToolNames["todo_create"] {
		t.Error("expected todo_create in todoToolNames")
	}
	if !todoToolNames["todo_list"] {
		t.Error("expected todo_list in todoToolNames")
	}
	if todoToolNames["other_tool"] {
		t.Error("unexpected other_tool in todoToolNames")
	}
}

// ── 辅助 ──

func boolPtr(b bool) *bool { return &b }

func assertBoolPtr(t *testing.T, got *bool, want bool) {
	t.Helper()
	if got == nil {
		t.Errorf("expected %v, got nil", want)
	} else if *got != want {
		t.Errorf("expected %v, got %v", want, *got)
	}
}

type testError struct {
	cause error
}

func (e *testError) Error() string { return "test error" }
func (e *testError) Unwrap() error { return e.cause }

type testInterruptRequest struct{ msg string }

func (r testInterruptRequest) GetMessage() string       { return r.msg }
func (r testInterruptRequest) GetAutoConfirmKey() string { return "" }
```

- [ ] **Step 2: 运行测试确认失败**

Run: `cd /home/opensource/uapclaw-gateway && go test ./internal/swarm/agents/harness/common/rails/ -run "TestBoolish|TestNonzeroExit|TestInferToolResult|TestStructuredTool|TestParseToolCall|TestExtractToolInterrupt|TestTruncate|TestTodoTool" -v 2>&1 | head -30`

Expected: 编译失败，函数未定义

- [ ] **Step 3: 写辅助函数实现**

```go
// File: internal/swarm/agents/harness/common/rails/stream_event_helpers.go
package rails

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	saschema "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/schema"
)

// ──────────────────────────── 常量 ────────────────────────────

const (
	// sidKey 跨 Rail 传递 session ID 的 extra 键，对齐 Python: _SID_KEY
	sidKey = "__jiuwenswarm_session_id__"
	// defaultSessionID 默认 session ID，对齐 Python: "default"
	defaultSessionID = "default"
)

// todoToolNames TODO 相关工具名称集合，对齐 Python: _TODO_TOOL_NAMES
var todoToolNames = map[string]bool{
	"todo_create": true,
	"todo_get":    true,
	"todo_list":   true,
	"todo_modify": true,
}

// ──────────────────────────── 导出函数 ────────────────────────────

// BoolishFalse 判断值是否为"假-ish"，对齐 Python: _boolish_false
func boolishFalse(value any) bool {
	if v, ok := value.(bool); ok {
		return !v
	}
	if s, ok := value.(string); ok {
		lower := strings.TrimSpace(strings.ToLower(s))
		return lower == "false" || lower == "0" || lower == "no"
	}
	return false
}

// BoolishTrue 判断值是否为"真-ish"，对齐 Python: _boolish_true
func boolishTrue(value any) bool {
	if v, ok := value.(bool); ok {
		return v
	}
	if s, ok := value.(string); ok {
		lower := strings.TrimSpace(strings.ToLower(s))
		return lower == "true" || lower == "1" || lower == "yes"
	}
	return false
}

// NonzeroExit 判断值是否为非零退出码，对齐 Python: _nonzero_exit
func nonzeroExit(value any) *bool {
	if _, ok := value.(bool); ok {
		return nil
	}
	if v, ok := value.(int); ok {
		result := v != 0
		return &result
	}
	if s, ok := value.(string); ok {
		parsed, err := parseInt(strings.TrimSpace(s))
		if err != nil {
			return nil
		}
		result := parsed != 0
		return &result
	}
	return nil
}

// InferToolResultError 推断工具结果是否为错误，对齐 Python: _infer_tool_result_error
func inferToolResultError(value any) *bool {
	switch v := value.(type) {
	case map[string]any:
		return inferDictError(v)
	case []any:
		for _, item := range v {
			if itemErr := inferToolResultError(item); itemErr != nil && *itemErr {
				return boolPtr(true)
			}
		}
		return nil
	case string:
		return inferStringError(v)
	default:
		return nil
	}
}

// StructuredToolResultPayload 提取结构化工具结果，对齐 Python: _structured_tool_result_payload
func structuredToolResultPayload(result any) any {
	if _, ok := result.(map[string]any); ok {
		return result
	}
	if _, ok := result.([]any); ok {
		return result
	}
	return nil
}

// ParseToolCallArguments 解析工具调用参数为 map，对齐 Python: _parse_tool_call_arguments
func parseToolCallArguments(tc *llmschema.ToolCall) map[string]any {
	if tc == nil {
		return map[string]any{}
	}
	args := tc.Arguments
	if args == "" {
		return map[string]any{}
	}
	var result map[string]any
	if err := json.Unmarshal([]byte(args), &result); err != nil {
		return map[string]any{}
	}
	return result
}

// ExtractToolInterrupt 递归查找 ToolInterruptException，对齐 Python: _extract_tool_interrupt
func extractToolInterrupt(value any) any {
	if value == nil {
		return nil
	}
	// 直接匹配 ToolInterruptException
	if exc, ok := value.(*saschema.ToolInterruptException); ok {
		return exc
	}
	// 通过 Unwrap 链递归查找（对齐 Python: cause / __cause__）
	if unwrapper, ok := value.(interface{ Unwrap() error }); ok {
		if cause := unwrapper.Unwrap(); cause != nil {
			if result := extractToolInterrupt(cause); result != nil {
				return result
			}
		}
	}
	return nil
}

// AskUserQuestionPayloadFromInterrupt 从中断构造 ask_user_question payload，对齐 Python: _ask_user_question_payload_from_interrupt
func askUserQuestionPayloadFromInterrupt(tc *llmschema.ToolCall, interrupt any) map[string]any {
	// 获取 request_id
	var requestID string
	if exc, ok := interrupt.(*saschema.ToolInterruptException); ok && exc.Request != nil {
		// 尝试从 Request 获取 tool_call_id（如果 Request 实现了 ToolCallIDAccessor）
		if accessor, ok := exc.Request.(interface{ GetToolCallID() string }); ok {
			requestID = strings.TrimSpace(accessor.GetToolCallID())
		}
	}
	if requestID == "" && tc != nil {
		requestID = strings.TrimSpace(tc.ID)
	}
	if requestID == "" {
		return nil
	}

	// 构造 interaction 列表传给 ConvertInteractionsToAskUserQuestion
	var valueObj any
	if exc, ok := interrupt.(*saschema.ToolInterruptException); ok && exc.Request != nil {
		valueObj = exc.Request
	}
	if valueObj == nil {
		args := parseToolCallArguments(tc)
		if len(args) == 0 {
			return nil
		}
		valueObj = map[string]any{
			"tool_args": args,
			"questions": args["questions"],
		}
	}

	// 使用已有的 ConvertInteractionsToAskUserQuestion
	interactions := []any{map[string]any{
		"id":    requestID,
		"value": valueObj,
	}}
	return interruptHelpers.ConvertInteractionsToAskUserQuestion(interactions)
}

// TruncateString 截断字符串到指定最大长度
func truncateString(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen]
}

// ToolCallName 安全获取 ToolCall 的 Name 字段
func toolCallName(tc *llmschema.ToolCall) string {
	if tc == nil {
		return ""
	}
	return tc.Name
}

// ToolCallID 安全获取 ToolCall 的 ID 字段
func toolCallID(tc *llmschema.ToolCall) string {
	if tc == nil {
		return ""
	}
	return tc.ID
}

// ToolCallArguments 安全获取 ToolCall 的 Arguments 字段
func toolCallArguments(tc *llmschema.ToolCall) any {
	if tc == nil {
		return map[string]any{}
	}
	if tc.Arguments == "" {
		return map[string]any{}
	}
	return tc.Arguments
}

// ──────────────────────────── 非导出函数 ────────────────────────────

func boolPtr(b bool) *bool { return &b }

func parseInt(s string) (int, error) {
	var neg bool
	s = strings.TrimSpace(s)
	if len(s) > 0 && s[0] == '-' {
		neg = true
		s = s[1:]
	}
	if s == "" {
		return 0, fmt.Errorf("invalid int: empty")
	}
	var result int
	for _, c := range s {
		if c < '0' || c > '9' {
			return 0, fmt.Errorf("invalid int: %s", s)
		}
		result = result*10 + int(c-'0')
	}
	if neg {
		return -result, nil
	}
	return result, nil
}

// inferDictError 推断 dict 类型结果的错误状态
func inferDictError(v map[string]any) *bool {
	// success 字段
	if val, ok := v["success"]; ok {
		if boolishFalse(val) {
			return boolPtr(true)
		}
		if boolishTrue(val) {
			return boolPtr(false)
		}
	}
	// is_error / isError 字段
	if boolishTrue(v["is_error"]) || boolishTrue(v["isError"]) {
		return boolPtr(true)
	}
	// status 字段
	if status, ok := v["status"].(string); ok {
		lower := strings.TrimSpace(strings.ToLower(status))
		if lower == "error" || lower == "failed" || lower == "failure" {
			return boolPtr(true)
		}
	}
	// exit_code / exitCode / returncode / return_code
	for _, key := range []string{"exit_code", "exitCode", "returncode", "return_code"} {
		if val, ok := v[key]; ok {
			if exitFailed := nonzeroExit(val); exitFailed != nil {
				return exitFailed
			}
		}
	}
	// 递归检查嵌套
	for _, key := range []string{"data", "raw_output", "rawOutput", "result"} {
		if nested, ok := v[key]; ok {
			switch n := nested.(type) {
			case map[string]any:
				if nestedErr := inferDictError(n); nestedErr != nil {
					return nestedErr
				}
			case []any:
				if nestedErr := inferToolResultError(n); nestedErr != nil {
					return nestedErr
				}
			}
		}
	}
	return nil
}

// inferStringError 推断 string 类型结果的错误状态
func inferStringError(s string) *bool {
	text := strings.TrimSpace(s)
	if text == "" {
		return nil
	}
	// 尝试解析为 JSON
	var parsed any
	if err := json.Unmarshal([]byte(text), &parsed); err == nil {
		if nestedErr := inferToolResultError(parsed); nestedErr != nil {
			return nestedErr
		}
	}
	// success=False 模式
	if matched, _ := regexp.MatchString(`\bsuccess\s*[:=]\s*False\b`, text); matched {
		return boolPtr(true)
	}
	// [ERROR] 前缀
	if strings.HasPrefix(text, "[ERROR]") {
		return boolPtr(true)
	}
	// exit_code 模式
	re := regexp.MustCompile(`\b(?:exit(?:[_ ]?code)?|returncode|return[_ ]code)\s*[:= ]\s*(-?\d+)\b`)
	if m := re.FindStringSubmatch(text); len(m) > 1 {
		if code := parseIntSafe(m[1]); code != 0 {
			return boolPtr(true)
		}
		return boolPtr(false)
	}
	return nil
}

func parseIntSafe(s string) int {
	s = strings.TrimSpace(s)
	if len(s) > 0 && s[0] == '-' {
		n := parseIntPositive(s[1:])
		return -n
	}
	return parseIntPositive(s)
}

func parseIntPositive(s string) int {
	var result int
	for _, c := range s {
		if c < '0' || c > '9' {
			return 0
		}
		result = result*10 + int(c-'0')
	}
	return result
}
```

注意：`askUserQuestionPayloadFromInterrupt` 引用了 `interruptHelpers.ConvertInteractionsToAskUserQuestion`，需要在 import 中添加别名导入：

```go
import (
	interruptHelpers "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/rails/interrupt"
)
```

- [ ] **Step 4: 运行测试确认通过**

Run: `cd /home/opensource/uapclaw-gateway && go test ./internal/swarm/agents/harness/common/rails/ -run "TestBoolish|TestNonzeroExit|TestInferToolResult|TestStructuredTool|TestParseToolCall|TestExtractToolInterrupt|TestTruncate|TestTodoTool" -v`

Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/swarm/agents/harness/common/rails/stream_event_helpers.go internal/swarm/agents/harness/common/rails/stream_event_helpers_test.go
git commit -m "feat(10.6.10): StreamEvent 辅助函数 — boolish/infer/extract/parse/truncate"
```

---

### Task 3: 上下文修复 — stream_event_context.go

**Files:**
- Create: `internal/swarm/agents/harness/common/rails/stream_event_context.go`
- Create: `internal/swarm/agents/harness/common/rails/stream_event_context_test.go`

- [ ] **Step 1: 写上下文修复测试**

```go
// File: internal/swarm/agents/harness/common/rails/stream_event_context_test.go
package rails

import (
	"testing"
)

func TestEnsureJSONArguments_合法JSON(t *testing.T) {
	input := `{"key": "value"}`
	result := ensureJSONArguments(input)
	if result != input {
		t.Errorf("expected original, got %s", result)
	}
}

func TestEnsureJSONArguments_空字符串(t *testing.T) {
	result := ensureJSONArguments("")
	if result != "{}" {
		t.Errorf("expected '{}', got %s", result)
	}
}

func TestEnsureJSONArguments_闭合括号修复(t *testing.T) {
	input := `{"key": "value"`
	result := ensureJSONArguments(input)
	// 应修复为 {"key": "value"}
	if result == "{}" {
		t.Errorf("修复失败，返回了兜底值")
	}
}

func TestEnsureJSONArguments_缺引号修复(t *testing.T) {
	input := `{query: "hello"}`
	result := ensureJSONArguments(input)
	if result == "{}" {
		t.Errorf("修复失败，返回了兜底值")
	}
}

func TestEnsureJSONArguments_全部失败兜底(t *testing.T) {
	input := `not json at all {{{`
	result := ensureJSONArguments(input)
	if result != "{}" {
		t.Errorf("expected '{}', got %s", result)
	}
}

func TestFixMissingQuotes_Windows路径(t *testing.T) {
	input := `{"path": D:/work/file.txt}`
	result := fixMissingQuotes(input)
	// 应修复为 {"path": "D:/work/file.txt"}
	if result == input {
		t.Error("Windows 路径未修复")
	}
}

func TestFixMissingQuotes_缺键引号(t *testing.T) {
	input := `{query: "hello"}`
	result := fixMissingQuotes(input)
	if result == input {
		t.Error("缺键引号未修复")
	}
}

func TestFixMissingQuotes_无需修复(t *testing.T) {
	input := `{"key": "value"}`
	result := fixMissingQuotes(input)
	if result != input {
		t.Errorf("不应修改合法 JSON，got %s", result)
	}
}
```

- [ ] **Step 2: 运行测试确认失败**

Run: `cd /home/opensource/uapclaw-gateway && go test ./internal/swarm/agents/harness/common/rails/ -run "TestEnsureJSON|TestFixMissing" -v 2>&1 | head -20`

Expected: 编译失败

- [ ] **Step 3: 写上下文修复实现**

```go
// File: internal/swarm/agents/harness/common/rails/stream_event_context.go
package rails

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	jsonrepair "github.com/RealAlexandreAI/json-repair"

	ceinterface "github.com/uapclaw/uapclaw-go/internal/agentcore/context_engine/interface"
	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

const logComponent = logger.ComponentChannel

// ──────────────────────────── 导出函数 ────────────────────────────

// EnsureJSONArguments 确保工具调用参数为合法 JSON 字符串，对齐 Python: _ensure_json_arguments
//
// 四阶段修复：
// 1. 直接 json.Unmarshal → 成功返回原文
// 2. json_repair.RepairJSON → 修复后验证
// 3. fixMissingQuotes 规则修复 → 修复后验证
// 兜底：返回 "{}"
func ensureJSONArguments(arguments string) string {
	if arguments == "" {
		return "{}"
	}
	text := strings.TrimSpace(arguments)
	if text == "" {
		return "{}"
	}

	// 阶段 1: 直接解析
	if json.Valid([]byte(text)) {
		return arguments
	}

	// 阶段 2: json_repair 库
	repaired := jsonrepair.RepairJSON(text)
	if repaired != "" && repaired != text {
		if json.Valid([]byte(repaired)) {
			logger.Info(logComponent).Str("stage", "json_repair").Str("outcome", "success").Msg("ensureJSONArguments 阶段2修复成功")
			return repaired
		}
	}
	logger.Warn(logComponent).Str("stage", "json_repair").Str("outcome", "failed").Msg("ensureJSONArguments 阶段2修复失败")

	// 阶段 3: 规则修复
	fixed := fixMissingQuotes(text)
	if fixed != text {
		if json.Valid([]byte(fixed)) {
			logger.Info(logComponent).Str("stage", "rule_fix").Str("outcome", "success").Msg("ensureJSONArguments 阶段3修复成功")
			return fixed
		}
	}
	logger.Warn(logComponent).Str("stage", "rule_fix").Str("outcome", "failed").Msg("ensureJSONArguments 阶段3修复失败")

	logger.Warn(logComponent).Str("outcome", "failed_all_stages").Msg("ensureJSONArguments 所有阶段修复失败")
	return "{}"
}

// FixMissingQuotes 尝试修复 JSON 中缺失的引号，对齐 Python: _fix_missing_quotes
//
// 三种修复模式：
// 1. Windows 路径：{"path": D:/work/file.txt} → {"path": "D:/work/file.txt"}
// 2. 缺结束引号：{"query": hello} → {"query": "hello"}
// 3. 缺键引号：{query: "hello"} → {"query": "hello"}
func fixMissingQuotes(jsonStr string) string {
	s := strings.TrimSpace(jsonStr)

	// 模式 1: 修复 Windows 路径
	winPathRe := regexp.MustCompile(`:\s+([A-Za-z]:/[^\{\[]*?)(?=\s*[,\}\]])`)
	s = winPathRe.ReplaceAllStringFunc(s, func(m string) string {
		sub := winPathRe.FindStringSubmatch(m)
		if len(sub) > 1 {
			return fmt.Sprintf(`: "%s"`, sub[1])
		}
		return m
	})

	// 模式 2: 修复缺结束引号的值
	missingEndQuoteRe := regexp.MustCompile(`:\s+(?!"|true|false|null|\d+|{|\[|:|"|[A-Za-z]:/)([^\s,\}\[\]"]+?)(?=\s*[,}\]])`)
	s = missingEndQuoteRe.ReplaceAllStringFunc(s, func(m string) string {
		sub := missingEndQuoteRe.FindStringSubmatch(m)
		if len(sub) > 1 {
			return fmt.Sprintf(`: "%s"`, sub[1])
		}
		return m
	})

	// 模式 3: 修复缺键引号
	missingKeyQuoteRe := regexp.MustCompile(`{\s+([a-zA-Z_][a-zA-Z0-9_]*)\s*:`)
	s = missingKeyQuoteRe.ReplaceAllString(s, `{"$1":`)

	return s
}

// FixIncompleteToolContext 修复不完整的工具上下文，对齐 Python: _fix_incomplete_tool_context
//
// 遍历消息列表，确保每个 tool_call 都有对应的 ToolMessage，
// 缺失时插入占位 ToolMessage（内容为"工具执行被中断"）。
func fixIncompleteToolContext(ctx context.Context, modelCtx ceinterface.ModelContext, getPromptLanguage func() string) error {
	// 延迟处理：此方法逻辑较复杂，需操作 BaseMessage 接口类型判断
	// 核心流程与 Python 一致：PopMessages → 遍历匹配 → AddMessages
	messages := modelCtx.PopMessages(0, true)
	if len(messages) == 0 {
		return nil
	}

	var toolIDCache []toolCacheEntry
	toolMessageCache := make(map[string]llmschema.BaseMessage)

	for i := range messages {
		msg := messages[i]
		role := msg.GetRole()

		switch role {
		case llmschema.RoleTypeAssistant:
			asstMsg, ok := msg.(*llmschema.AssistantMessage)
			if !ok {
				// 非 AssistantMessage 类型，当作普通消息处理
				if len(toolIDCache) > 0 {
					insertPlaceholderToolMessages(ctx, modelCtx, toolIDCache, toolMessageCache, getPromptLanguage)
					toolIDCache = nil
				}
				_, _ = modelCtx.AddMessages(ctx, []llmschema.BaseMessage{msg})
				continue
			}

			if len(toolIDCache) > 0 {
				// 有未匹配的 tool_call → 插入占位 ToolMessage
				insertPlaceholderToolMessages(ctx, modelCtx, toolIDCache, toolMessageCache, getPromptLanguage)
				toolIDCache = nil
				logger.Info(logComponent).Msg("修复不完整工具上下文：已插入占位 ToolMessage")
			}

			// 记录新的 tool_calls
			if asstMsg.ToolCalls != nil {
				for _, tc := range asstMsg.ToolCalls {
					// 修复 Arguments
					if tc.Arguments != "" {
						tc.Arguments = ensureJSONArguments(tc.Arguments)
					}
					toolIDCache = append(toolIDCache, toolCacheEntry{
						toolCallID: tc.ID,
						toolName:   tc.Name,
					})
				}
			}

			_, _ = modelCtx.AddMessages(ctx, []llmschema.BaseMessage{msg})

		case llmschema.RoleTypeTool:
			toolMsg, ok := msg.(*llmschema.ToolMessage)
			if !ok {
				if len(toolIDCache) > 0 {
					insertPlaceholderToolMessages(ctx, modelCtx, toolIDCache, toolMessageCache, getPromptLanguage)
					toolIDCache = nil
				}
				_, _ = modelCtx.AddMessages(ctx, []llmschema.BaseMessage{msg})
				continue
			}

			if len(toolIDCache) == 0 {
				toolMessageCache[toolMsg.ToolCallID] = msg
				continue
			}

			// 尝试匹配 toolIDCache 中的第一个
			if toolMsg.ToolCallID == toolIDCache[0].toolCallID {
				_, _ = modelCtx.AddMessages(ctx, []llmschema.BaseMessage{msg})
				toolIDCache = toolIDCache[1:]
			} else {
				toolMessageCache[toolMsg.ToolCallID] = msg
			}

		default:
			if len(toolIDCache) > 0 {
				insertPlaceholderToolMessages(ctx, modelCtx, toolIDCache, toolMessageCache, getPromptLanguage)
				toolIDCache = nil
				logger.Info(logComponent).Msg("修复不完整工具上下文：已插入占位 ToolMessage")
			}
			_, _ = modelCtx.AddMessages(ctx, []llmschema.BaseMessage{msg})
		}
	}

	// 遍历结束后仍有未匹配的 tool_call
	if len(toolIDCache) > 0 {
		insertPlaceholderToolMessages(ctx, modelCtx, toolIDCache, toolMessageCache, getPromptLanguage)
		logger.Info(logComponent).Msg("修复不完整工具上下文：尾部已插入占位 ToolMessage")
	}

	return nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// toolCacheEntry 待匹配的 tool_call 缓存条目
type toolCacheEntry struct {
	toolCallID string
	toolName   string
}

// insertPlaceholderToolMessages 为未匹配的 tool_call 插入占位 ToolMessage
func insertPlaceholderToolMessages(
	ctx context.Context,
	modelCtx ceinterface.ModelContext,
	toolIDCache []toolCacheEntry,
	toolMessageCache map[string]llmschema.BaseMessage,
	getPromptLanguage func() string,
) {
	for _, entry := range toolIDCache {
		if cached, ok := toolMessageCache[entry.toolCallID]; ok {
			_, _ = modelCtx.AddMessages(ctx, []llmschema.BaseMessage{cached})
			delete(toolMessageCache, entry.toolCallID)
		} else {
			content := toolInterruptedMessage(entry.toolName, getPromptLanguage)
			placeholder := llmschema.NewToolMessage(entry.toolCallID, content)
			_, _ = modelCtx.AddMessages(ctx, []llmschema.BaseMessage{placeholder})
		}
	}
}

// toolInterruptedMessage 构建语言感知的工具中断消息，对齐 Python: _tool_interrupted_message
func toolInterruptedMessage(toolName string, getPromptLanguage func() string) string {
	if getPromptLanguage != nil {
		lang := getPromptLanguage()
		if lang == "en" {
			return fmt.Sprintf("[Tool interrupted] Tool %s was interrupted by the user and has no result.", toolName)
		}
	}
	return fmt.Sprintf("[工具执行被中断] 工具 %s 执行过程中被用户打断，没有执行结果。", toolName)
}
```

- [ ] **Step 4: 运行测试确认通过**

Run: `cd /home/opensource/uapclaw-gateway && go test ./internal/swarm/agents/harness/common/rails/ -run "TestEnsureJSON|TestFixMissing" -v`

Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/swarm/agents/harness/common/rails/stream_event_context.go internal/swarm/agents/harness/common/rails/stream_event_context_test.go
git commit -m "feat(10.6.10): StreamEvent 上下文修复 — ensureJSONArguments/fixMissingQuotes/fixIncompleteToolContext"
```

---

### Task 4: 流式事件发射 — stream_event_emit.go

**Files:**
- Create: `internal/swarm/agents/harness/common/rails/stream_event_emit.go`
- Create: `internal/swarm/agents/harness/common/rails/stream_event_emit_test.go`

- [ ] **Step 1: 写发射方法测试**

测试使用 mock SessionFacade 验证 WriteStream 写入的 OutputSchema 的 Type 和 Payload 结构。

```go
// File: internal/swarm/agents/harness/common/rails/stream_event_emit_test.go
package rails

import (
	"context"
	"testing"

	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/stream"
	sessioninterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/session/interfaces"
	hschema "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/schema"
)

// mockSessionFacade 用于测试的 SessionFacade mock
type mockSessionFacade struct {
	sessioninterfaces.SessionFacade
	written []stream.OutputSchema
}

func (m *mockSessionFacade) WriteStream(_ context.Context, data any) error {
	if schema, ok := data.(stream.OutputSchema); ok {
		m.written = append(m.written, schema)
	}
	return nil
}

func TestEmitToolCall(t *testing.T) {
	sess := &mockSessionFacade{}
	tc := &llmschema.ToolCall{ID: "tc_1", Name: "read_file", Arguments: `{"path": "/tmp"}`}

	emitToolCall(context.Background(), sess, tc)

	if len(sess.written) != 1 {
		t.Fatalf("expected 1 write, got %d", len(sess.written))
	}
	if sess.written[0].Type != "tool_call" {
		t.Errorf("expected type 'tool_call', got %s", sess.written[0].Type)
	}
	payload, ok := sess.written[0].Payload.(map[string]any)
	if !ok {
		t.Fatal("payload is not map[string]any")
	}
	tcPayload, ok := payload["tool_call"].(map[string]any)
	if !ok {
		t.Fatal("tool_call payload is not map[string]any")
	}
	if tcPayload["name"] != "read_file" {
		t.Errorf("expected name 'read_file', got %v", tcPayload["name"])
	}
}

func TestEmitToolResult(t *testing.T) {
	sess := &mockSessionFacade{}
	tc := &llmschema.ToolCall{ID: "tc_2", Name: "run_command", Arguments: `{}`}

	emitToolResult(context.Background(), sess, tc, map[string]any{"success": true, "output": "ok"})

	if len(sess.written) != 1 {
		t.Fatalf("expected 1 write, got %d", len(sess.written))
	}
	if sess.written[0].Type != "tool_result" {
		t.Errorf("expected type 'tool_result', got %s", sess.written[0].Type)
	}
}

func TestEmitToolUpdate(t *testing.T) {
	sess := &mockSessionFacade{}
	tc := &llmschema.ToolCall{ID: "tc_3", Name: "search", Arguments: `{}`}

	emitToolUpdate(context.Background(), sess, tc, "in_progress")

	if len(sess.written) != 1 {
		t.Fatalf("expected 1 write, got %d", len(sess.written))
	}
	if sess.written[0].Type != "tool_update" {
		t.Errorf("expected type 'tool_update', got %s", sess.written[0].Type)
	}
}

func TestEmitToolUpdate_空状态默认in_progress(t *testing.T) {
	sess := &mockSessionFacade{}
	tc := &llmschema.ToolCall{ID: "tc_4", Name: "search", Arguments: `{}`}

	emitToolUpdate(context.Background(), sess, tc, "")

	payload, _ := sess.written[0].Payload.(map[string]any)
	updatePayload := payload["tool_update"].(map[string]any)
	if updatePayload["status"] != "in_progress" {
		t.Errorf("expected default status 'in_progress', got %v", updatePayload["status"])
	}
}

func TestFormatTodosForFrontend(t *testing.T) {
	items := []hschema.TodoItem{
		{ID: "1", Content: "task1", ActiveForm: "doing", Status: hschema.TodoStatusInProgress},
		{ID: "2", Content: "task2", ActiveForm: "pending", Status: hschema.TodoStatusPending},
		{ID: "3", Content: "task3", ActiveForm: "done", Status: hschema.TodoStatusCompleted},
		{ID: "4", Content: "task4", ActiveForm: "cancelled", Status: hschema.TodoStatusCancelled},
	}

	result := formatTodosForFrontend(items)

	// cancelled 应被过滤
	if len(result) != 3 {
		t.Errorf("expected 3 items (cancelled filtered), got %d", len(result))
	}

	// 验证状态映射
	if result[0]["status"] != "in_progress" {
		t.Errorf("expected 'in_progress', got %v", result[0]["status"])
	}
	if result[1]["status"] != "pending" {
		t.Errorf("expected 'pending', got %v", result[1]["status"])
	}
	if result[2]["status"] != "completed" {
		t.Errorf("expected 'completed', got %v", result[2]["status"])
	}
}
```

注意：`todoItem` 和 `todoStatus*` 需要定义对应的本地类型或直接使用 `harness/schema` 包的类型。实际实现时需要根据 Go 侧 `hschema.TodoItem` / `hschema.TodoStatus` 调整。

- [ ] **Step 2: 运行测试确认失败**

- [ ] **Step 3: 写发射方法实现**

```go
// File: internal/swarm/agents/harness/common/rails/stream_event_emit.go
package rails

import (
	"context"
	"fmt"
	"strings"

	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/stream"
	sessioninterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/session/interfaces"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	contextutils "github.com/uapclaw/uapclaw-go/internal/agentcore/context_engine/context"
	hschema "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/schema"
	todotool "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/tools/todo"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// EmitToolCall 发射 tool_call 事件，对齐 Python: _emit_tool_call
func emitToolCall(ctx context.Context, session sessioninterfaces.SessionFacade, tc *llmschema.ToolCall) {
	payload := map[string]any{
		"tool_call": map[string]any{
			"name":         toolCallName(tc),
			"arguments":    toolCallArguments(tc),
			"tool_call_id": toolCallID(tc),
		},
	}
	_ = session.WriteStream(ctx, stream.OutputSchema{
		Type:    "tool_call",
		Index:   0,
		Payload: payload,
	})
}

// EmitToolResult 发射 tool_result 事件，对齐 Python: _emit_tool_result
func emitToolResult(ctx context.Context, session sessioninterfaces.SessionFacade, tc *llmschema.ToolCall, result any) {
	rawOutput := structuredToolResultPayload(result)
	toolResultPayload := map[string]any{
		"tool_name":    toolCallName(tc),
		"tool_call_id": toolCallID(tc),
		"result":       truncateString(fmt.Sprintf("%v", result), 60000),
	}
	if rawOutput != nil {
		toolResultPayload["raw_output"] = rawOutput
	}
	errorState := inferToolResultError(rawOutput)
	if errorState != nil {
		toolResultPayload["success"] = !*errorState
		if *errorState {
			toolResultPayload["status"] = "error"
			toolResultPayload["is_error"] = true
		}
	}
	_ = session.WriteStream(ctx, stream.OutputSchema{
		Type:    "tool_result",
		Index:   0,
		Payload: map[string]any{"tool_result": toolResultPayload},
	})
}

// EmitToolUpdate 发射 tool_update 事件，对齐 Python: _emit_tool_update
func emitToolUpdate(ctx context.Context, session sessioninterfaces.SessionFacade, tc *llmschema.ToolCall, status string) {
	if status == "" {
		status = "in_progress"
	}
	payload := map[string]any{
		"tool_update": map[string]any{
			"tool_name":     toolCallName(tc),
			"tool_call_id":  toolCallID(tc),
			"arguments":     toolCallArguments(tc),
			"status":        status,
		},
	}
	_ = session.WriteStream(ctx, stream.OutputSchema{
		Type:    "tool_update",
		Index:   0,
		Payload: payload,
	})
}

// EmitContextUsage 发射 context.usage 事件，对齐 Python: _emit_context_usage
func emitContextUsage(ctx context.Context, cbc *agentinterfaces.AgentCallbackContext) error {
	session := cbc.Session()
	if session == nil {
		return nil
	}

	// 获取 model_name
	var modelName string
	agent := cbc.Agent()
	if agent != nil {
		// 尝试从 agent 的 Config 获取 model_name
		if configProvider, ok := agent.(interface{ Config() any }); ok {
			if cfg := configProvider.Config(); cfg != nil {
				if modelNameProvider, ok := cfg.(interface{ ModelName() string }); ok {
					modelName = modelNameProvider.ModelName()
				}
			}
		}
	}

	// 解析 context_max
	rawTotalTokens := contextutils.ResolveContextMax(modelName, 0, nil)

	// 从 response.usage_metadata 获取当前 token 使用量
	var currentContextTokens int
	if inputs, ok := cbc.Inputs().(*agentinterfaces.ModelCallInputs); ok && inputs != nil && inputs.Response != nil && inputs.Response.UsageMetadata != nil {
		currentContextTokens = inputs.Response.UsageMetadata.TotalTokens
	}

	var rate float64
	if rawTotalTokens != 0 {
		rate = float64(currentContextTokens) / float64(rawTotalTokens) * 100
	}

	_ = session.WriteStream(ctx, stream.OutputSchema{
		Type:  "context.usage",
		Index: 0,
		Payload: map[string]any{
			"rate":         rate,
			"context_max":  rawTotalTokens,
			"tokens_used":  currentContextTokens,
		},
	})
	return nil
}

// EmitAskUserQuestionIfInterrupted 发射 chat.ask_user_question 事件，对齐 Python: _emit_ask_user_question_if_interrupted
func emitAskUserQuestionIfInterrupted(
	ctx context.Context,
	session sessioninterfaces.SessionFacade,
	tc *llmschema.ToolCall,
	toolName string,
	result any,
	exception error,
) {
	if strings.TrimSpace(toolName) != "ask_user" {
		return
	}
	interrupt := extractToolInterrupt(result)
	if interrupt == nil {
		interrupt = extractToolInterrupt(exception)
	}
	if interrupt == nil {
		return
	}
	payload := askUserQuestionPayloadFromInterrupt(tc, interrupt)
	if payload == nil {
		logger.Debug(logComponent).Msg("ask_user interrupt payload 不可用")
		return
	}
	_ = session.WriteStream(ctx, stream.OutputSchema{
		Type:    "chat.ask_user_question",
		Index:   0,
		Payload: payload,
	})
}

// EmitTodoUpdated 发射 todo.updated 事件，对齐 Python: _emit_todo_updated
func (r *JiuClawStreamEventRail) emitTodoUpdated(ctx context.Context, session sessioninterfaces.SessionFacade, conversationID string) {
	todoTool := r.getTodoTool()
	if todoTool == nil {
		logger.Debug(logComponent).Msg("TodoTool 不可用")
		return
	}
	todosData, err := todoTool.LoadTodos(ctx, conversationID)
	if err != nil {
		logger.Debug(logComponent).Err(err).Msg("加载 TODO 列表失败")
		return
	}
	todos := formatTodosForFrontend(todosData)
	_ = session.WriteStream(ctx, stream.OutputSchema{
		Type:    "todo.updated",
		Index:   0,
		Payload: map[string]any{"todos": todos},
	})
}

// FormatTodosForFrontend 格式化 TODO 列表供前端展示，对齐 Python: _format_todos_for_frontend
func formatTodosForFrontend(todosData []hschema.TodoItem) []map[string]any {
	statusMapping := map[hschema.TodoStatus]string{
		hschema.TodoStatusPending:    "pending",
		hschema.TodoStatusInProgress: "in_progress",
		hschema.TodoStatusCompleted:  "completed",
	}

	var result []map[string]any
	for _, item := range todosData {
		if item.Status == hschema.TodoStatusCancelled {
			continue
		}
		status := statusMapping[item.Status]
		if status == "" {
			status = item.Status.String()
		}
		result = append(result, map[string]any{
			"id":         item.ID,
			"content":    item.Content,
			"activeForm": item.ActiveForm,
			"status":     status,
		})
	}
	return result
}
```

- [ ] **Step 4: 运行测试确认通过**

Run: `cd /home/opensource/uapclaw-gateway && go test ./internal/swarm/agents/harness/common/rails/ -run "TestEmit|TestFormatTodos" -v`

Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/swarm/agents/harness/common/rails/stream_event_emit.go internal/swarm/agents/harness/common/rails/stream_event_emit_test.go
git commit -m "feat(10.6.10): StreamEvent 流式事件发射 — emitToolCall/Result/Update/ContextUsage/AskUser/Todo"
```

---

### Task 5: 核心结构体 — stream_event_rail.go

**Files:**
- Create: `internal/swarm/agents/harness/common/rails/stream_event_rail.go`
- Create: `internal/swarm/agents/harness/common/rails/stream_event_rail_test.go`

- [ ] **Step 1: 写核心结构体测试**

```go
// File: internal/swarm/agents/harness/common/rails/stream_event_rail_test.go
package rails

import (
	"context"
	"sync"
	"testing"
	"time"
)

func TestNewJiuClawStreamEventRail(t *testing.T) {
	rail := NewJiuClawStreamEventRail()
	if rail == nil {
		t.Fatal("expected non-nil rail")
	}
	if rail.Priority() != 80 {
		t.Errorf("expected priority 80, got %d", rail.Priority())
	}
}

func TestPause_Resume(t *testing.T) {
	rail := NewJiuClawStreamEventRail()
	sid := "test-session"

	var checkpointErr error
	var wg sync.WaitGroup

	// 在 goroutine 中模拟 checkpoint
	wg.Add(1)
	go func() {
		defer wg.Done()
		checkpointErr = rail.checkpoint(context.Background(), sid)
	}()

	// 等待一小段时间让 goroutine 进入 checkpoint
	time.Sleep(50 * time.Millisecond)

	// 暂停 — goroutine 应阻塞
	rail.Pause(sid)
	time.Sleep(50 * time.Millisecond)

	// 恢复 — goroutine 应继续
	rail.Resume(sid)
	wg.Wait()

	if checkpointErr != nil {
		t.Errorf("expected nil error after resume, got %v", checkpointErr)
	}
}

func TestAbort(t *testing.T) {
	rail := NewJiuClawStreamEventRail()
	sid := "test-session"

	var checkpointErr error
	var wg sync.WaitGroup

	wg.Add(1)
	go func() {
		defer wg.Done()
		checkpointErr = rail.checkpoint(context.Background(), sid)
	}()

	time.Sleep(50 * time.Millisecond)

	rail.Abort(sid)
	wg.Wait()

	if checkpointErr == nil {
		t.Error("expected error after abort, got nil")
	}
	if checkpointErr.Error() != "Agent abort requested" {
		t.Errorf("expected 'Agent abort requested', got %v", checkpointErr)
	}
}

func TestResetAbort(t *testing.T) {
	rail := NewJiuClawStreamEventRail()
	sid := "test-session"

	rail.Abort(sid)
	rail.ResetAbort(sid)

	// Abort 已清除，checkpoint 应能通过
	err := rail.checkpoint(context.Background(), sid)
	if err != nil {
		t.Errorf("expected nil after reset_abort, got %v", err)
	}
}

func TestResetForNewTask_保留Abort(t *testing.T) {
	rail := NewJiuClawStreamEventRail()
	sid := "test-session"

	rail.Abort(sid)
	rail.ResetForNewTask(sid)

	// ResetForNewTask 不清除 abort，但解除了 pause
	// checkpoint 应返回 abort 错误
	err := rail.checkpoint(context.Background(), sid)
	if err == nil {
		t.Error("expected abort error after reset_for_new_task, got nil")
	}
}

func TestCleanupSession(t *testing.T) {
	rail := NewJiuClawStreamEventRail()
	sid := "test-session"

	rail.Pause(sid)
	rail.CleanupSession(sid)

	// cleanup 后状态已删除，getOrCreateState 会创建新状态（默认 unpaused）
	err := rail.checkpoint(context.Background(), sid)
	if err != nil {
		t.Errorf("expected nil after cleanup, got %v", err)
	}
}

func TestCollectCancelledToolUpdates(t *testing.T) {
	rail := NewJiuClawStreamEventRail()
	sid := "test-session"

	// 模拟 inflight tool
	rail.mu.Lock()
	state := rail.getOrCreateState(sid)
	state.inflightToolCalls["tc_1"] = &inflightToolInfo{
		toolCall:  &llmschema.ToolCall{ID: "tc_1", Name: "read_file"},
		sessionID: sid,
	}
	rail.mu.Unlock()

	rail.CollectCancelledToolUpdates(sid)

	results := rail.GetCancelledToolResults(sid)
	if len(results) != 1 {
		t.Fatalf("expected 1 cancelled result, got %d", len(results))
	}
	if results[0]["tool_name"] != "read_file" {
		t.Errorf("expected 'read_file', got %v", results[0]["tool_name"])
	}

	rail.ClearCancelledToolResults(sid)
	results = rail.GetCancelledToolResults(sid)
	if len(results) != 0 {
		t.Errorf("expected 0 after clear, got %d", len(results))
	}
}

func TestResolveSID(t *testing.T) {
	rail := NewJiuClawStreamEventRail()

	// extra 中有 sidKey
	cbc := &agentinterfaces.AgentCallbackContext{}
	cbc.Extra()[sidKey] = "my-session"
	sid := rail.resolveSID(cbc, nil)
	if sid != "my-session" {
		t.Errorf("expected 'my-session', got %s", sid)
	}

	// extra 中无 sidKey，无 session
	delete(cbc.Extra(), sidKey)
	sid = rail.resolveSID(cbc, nil)
	if sid != "default" {
		t.Errorf("expected 'default', got %s", sid)
	}
}
```

注意：`AgentCallbackContext` 是非导出字段结构，测试中需要用正确的方式构造。实际实现时可能需要使用工厂函数或测试辅助。

- [ ] **Step 2: 运行测试确认失败**

- [ ] **Step 3: 写核心结构体实现**

```go
// File: internal/swarm/agents/harness/common/rails/stream_event_rail.go
package rails

import (
	"context"
	"fmt"
	"sync"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/rails"
	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	sessioninterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/session/interfaces"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/runner/callback"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	todotool "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/tools/todo"
)

// ──────────────────────────── 结构体 ────────────────────────────

// JiuClawStreamEventRail 流事件护栏，对齐 Python JiuClawStreamEventRail(priority=80)
//
// 三重职责：
// 1. 流式事件发射：tool_call/tool_result/tool_update/context.usage/todo.updated/ask_user_question
// 2. 暂停/中止检查点：在 BeforeModelCall/BeforeToolCall 中阻塞等待恢复或中止
// 3. 上下文修复：修复不完整的 tool context + 畸形 JSON 参数
type JiuClawStreamEventRail struct {
	rails.DeepAgentRail

	// deepAgent 主 Agent 实例引用（用于获取 language/workspace/todo tool）
	deepAgent agentinterfaces.BaseAgent

	// mainTodoTool 缓存的 TodoTool 实例
	mainTodoTool *todotool.TodoTool

	// mu 保护 sessionStates
	mu            sync.Mutex
	sessionStates map[string]*streamSessionState
}

// streamSessionState 每个 session 的暂停/中止/追踪状态
type streamSessionState struct {
	// ── 暂停/中止 ──
	paused  bool
	aborted bool
	cond    *sync.Cond // 基于 JiuClawStreamEventRail.mu 的条件变量

	// ── 会话上下文 ──
	conversationID string
	mainSession    sessioninterfaces.SessionFacade

	// ── 工具调用追踪 ──
	inflightToolCalls    map[string]*inflightToolInfo
	cancelledToolResults []map[string]any
}

// inflightToolInfo 正在执行中的工具调用信息
type inflightToolInfo struct {
	toolCall  *llmschema.ToolCall
	session   sessioninterfaces.SessionFacade
	sessionID string
}

// ──────────────────────────── 常量 ────────────────────────────

// 此处常量已移至 stream_event_helpers.go

// ──────────────────────────── 导出函数 ────────────────────────────

// NewJiuClawStreamEventRail 创建流事件护栏实例
func NewJiuClawStreamEventRail() *JiuClawStreamEventRail {
	r := &JiuClawStreamEventRail{
		sessionStates: make(map[string]*streamSessionState),
	}
	_ = r.DeepAgentRail.WithPriority(80)
	return r
}

// Priority 返回优先级，对齐 Python: priority = 80
func (r *JiuClawStreamEventRail) Priority() int {
	return 80
}

// Pause 暂停 session，对齐 Python: pause(session_id)
func (r *JiuClawStreamEventRail) Pause(sessionID string) {
	r.mu.Lock()
	state := r.getOrCreateState(sessionID)
	state.paused = true
	r.mu.Unlock()
}

// Resume 恢复 session，对齐 Python: resume(session_id)
func (r *JiuClawStreamEventRail) Resume(sessionID string) {
	r.mu.Lock()
	state := r.getOrCreateState(sessionID)
	state.aborted = false
	state.paused = false
	state.cond.Broadcast()
	r.mu.Unlock()
}

// Abort 中止 session，对齐 Python: abort(session_id)
func (r *JiuClawStreamEventRail) Abort(sessionID string) {
	r.mu.Lock()
	state := r.getOrCreateState(sessionID)
	state.aborted = true
	state.paused = false
	state.cond.Broadcast()
	r.mu.Unlock()
}

// ResetAbort 清除中止标志，对齐 Python: reset_abort(session_id)
func (r *JiuClawStreamEventRail) ResetAbort(sessionID string) {
	r.mu.Lock()
	state := r.getOrCreateState(sessionID)
	state.aborted = false
	r.mu.Unlock()
}

// ResetForNewTask 解除暂停但保留 abort 标志，对齐 Python: reset_for_new_task(session_id)
func (r *JiuClawStreamEventRail) ResetForNewTask(sessionID string) {
	r.mu.Lock()
	state := r.getOrCreateState(sessionID)
	state.paused = false
	state.conversationID = ""
	state.mainSession = nil
	state.cond.Broadcast()
	r.mu.Unlock()
}

// CleanupSession 清除所有 per-session 状态，对齐 Python: cleanup_session(session_id)
func (r *JiuClawStreamEventRail) CleanupSession(sessionID string) {
	r.mu.Lock()
	delete(r.sessionStates, sessionID)
	r.mu.Unlock()
}

// CollectCancelledToolUpdates 收集中断时的工具调用信息，对齐 Python: collect_cancelled_tool_updates
func (r *JiuClawStreamEventRail) CollectCancelledToolUpdates(sessionID string) {
	r.mu.Lock()
	state := r.getOrCreateState(sessionID)
	for tcID, info := range state.inflightToolCalls {
		if sessionID != "" && info.sessionID != sessionID {
			continue
		}
		if info.toolCall == nil {
			continue
		}
		state.cancelledToolResults = append(state.cancelledToolResults, map[string]any{
			"tool_name":    info.toolCall.Name,
			"tool_call_id": tcID,
			"result":       "[Interrupted] Tool execution cancelled by user.",
			"status":       "error",
		})
		delete(state.inflightToolCalls, tcID)
	}
	count := len(state.cancelledToolResults)
	r.mu.Unlock()
	logger.Info(logComponent).Int("count", count).Str("session_id", sessionID).Msg("收集已取消工具信息")
}

// GetCancelledToolResults 获取中断收集的工具结果，对齐 Python: get_cancelled_tool_results
func (r *JiuClawStreamEventRail) GetCancelledToolResults(sessionID string) []map[string]any {
	r.mu.Lock()
	state := r.getOrCreateState(sessionID)
	results := make([]map[string]any, len(state.cancelledToolResults))
	copy(results, state.cancelledToolResults)
	r.mu.Unlock()
	return results
}

// ClearCancelledToolResults 清除已收集的工具结果，对齐 Python: clear_cancelled_tool_results
func (r *JiuClawStreamEventRail) ClearCancelledToolResults(sessionID string) {
	r.mu.Lock()
	state := r.getOrCreateState(sessionID)
	state.cancelledToolResults = nil
	r.mu.Unlock()
}

// GetCallbacks 覆盖基类，注册 6 个生命周期钩子
func (r *JiuClawStreamEventRail) GetCallbacks() map[agentinterfaces.AgentCallbackEvent]callback.PerAgentCallbackFunc {
	callbacks := r.DeepAgentRail.GetCallbacks()
	callbacks[agentinterfaces.CallbackBeforeInvoke] = func(ctx context.Context, railCtx any) error {
		return r.BeforeInvoke(ctx, railCtx.(*agentinterfaces.AgentCallbackContext))
	}
	callbacks[agentinterfaces.CallbackBeforeModelCall] = func(ctx context.Context, railCtx any) error {
		return r.BeforeModelCall(ctx, railCtx.(*agentinterfaces.AgentCallbackContext))
	}
	callbacks[agentinterfaces.CallbackAfterModelCall] = func(ctx context.Context, railCtx any) error {
		return r.AfterModelCall(ctx, railCtx.(*agentinterfaces.AgentCallbackContext))
	}
	callbacks[agentinterfaces.CallbackBeforeToolCall] = func(ctx context.Context, railCtx any) error {
		return r.BeforeToolCall(ctx, railCtx.(*agentinterfaces.AgentCallbackContext))
	}
	callbacks[agentinterfaces.CallbackAfterToolCall] = func(ctx context.Context, railCtx any) error {
		return r.AfterToolCall(ctx, railCtx.(*agentinterfaces.AgentCallbackContext))
	}
	callbacks[agentinterfaces.CallbackOnModelException] = func(ctx context.Context, railCtx any) error {
		return r.OnModelException(ctx, railCtx.(*agentinterfaces.AgentCallbackContext))
	}
	return callbacks
}

// ──────────────────────────── AgentRail 钩子 ────────────────────────────

// BeforeInvoke 捕获 conversation_id + session，对齐 Python: before_invoke
func (r *JiuClawStreamEventRail) BeforeInvoke(ctx context.Context, cbc *agentinterfaces.AgentCallbackContext) error {
	inputs, ok := cbc.Inputs().(*agentinterfaces.InvokeInputs)
	if !ok {
		return nil
	}
	if cbc.Session() == nil {
		return nil
	}

	rawConvID := inputs.ConversationID
	sid := rawConvID
	if sid == "" {
		sid = defaultSessionID
	}

	r.mu.Lock()
	state := r.getOrCreateState(sid)
	if rawConvID != "" {
		state.conversationID = rawConvID
	}
	state.mainSession = cbc.Session()
	r.mu.Unlock()

	r.deepAgent = cbc.Agent()
	cbc.Extra()[sidKey] = sid
	return nil
}

// BeforeModelCall 暂停/中止检查点 + 上下文修复，对齐 Python: before_model_call
func (r *JiuClawStreamEventRail) BeforeModelCall(ctx context.Context, cbc *agentinterfaces.AgentCallbackContext) error {
	sid := r.resolveSID(cbc, cbc.Session())
	if err := r.checkpoint(ctx, sid); err != nil {
		return err
	}
	if cbc.ModelContext() != nil {
		if err := fixIncompleteToolContext(ctx, cbc.ModelContext(), r.getPromptLanguage); err != nil {
			logger.Warn(logComponent).Err(err).Msg("修复不完整工具上下文失败")
		}
	}
	return nil
}

// AfterModelCall 发射 context.usage 事件，对齐 Python: after_model_call
func (r *JiuClawStreamEventRail) AfterModelCall(ctx context.Context, cbc *agentinterfaces.AgentCallbackContext) error {
	return emitContextUsage(ctx, cbc)
}

// BeforeToolCall 暂停/中止检查点 + 发射事件 + 记录 inflight，对齐 Python: before_tool_call
func (r *JiuClawStreamEventRail) BeforeToolCall(ctx context.Context, cbc *agentinterfaces.AgentCallbackContext) error {
	sid := r.resolveSID(cbc, cbc.Session())
	if err := r.checkpoint(ctx, sid); err != nil {
		return err
	}

	session := cbc.Session()
	inputs, ok := cbc.Inputs().(*agentinterfaces.ToolCallInputs)
	if !ok || session == nil {
		return nil
	}
	tc := inputs.ToolCall
	if tc == nil {
		return nil
	}

	emitToolCall(ctx, session, tc)
	emitToolUpdate(ctx, session, tc, "in_progress")

	tcID := tc.ID
	if tcID != "" {
		r.mu.Lock()
		state := r.getOrCreateState(sid)
		state.inflightToolCalls[tcID] = &inflightToolInfo{
			toolCall:  tc,
			session:   session,
			sessionID: sid,
		}
		r.mu.Unlock()
	}
	return nil
}

// AfterToolCall 发射 tool_result + ask_user + todo.updated，对齐 Python: after_tool_call
func (r *JiuClawStreamEventRail) AfterToolCall(ctx context.Context, cbc *agentinterfaces.AgentCallbackContext) error {
	session := cbc.Session()
	inputs, ok := cbc.Inputs().(*agentinterfaces.ToolCallInputs)
	if !ok || session == nil {
		return nil
	}
	tc := inputs.ToolCall
	tcID := ""
	if tc != nil {
		tcID = tc.ID
	}

	if tcID != "" {
		r.mu.Lock()
		sid := r.resolveSID(cbc, session)
		state := r.getOrCreateState(sid)
		delete(state.inflightToolCalls, tcID)
		r.mu.Unlock()
	}

	emitToolResult(ctx, session, tc, inputs.ToolResult)
	emitAskUserQuestionIfInterrupted(ctx, session, tc, inputs.ToolName, inputs.ToolResult, cbc.Exception())

	toolName := inputs.ToolName
	sid := r.resolveSID(cbc, session)
	r.mu.Lock()
	state := r.getOrCreateState(sid)
	convID := state.conversationID
	r.mu.Unlock()
	if convID != "" && todoToolNames[toolName] {
		r.emitTodoUpdated(ctx, session, convID)
	}
	return nil
}

// OnModelException 上下文修复，对齐 Python: on_model_exception
func (r *JiuClawStreamEventRail) OnModelException(ctx context.Context, cbc *agentinterfaces.AgentCallbackContext) error {
	if cbc.ModelContext() != nil {
		logger.Info(logComponent).Msg("模型异常后尝试修复上下文")
		if err := fixIncompleteToolContext(ctx, cbc.ModelContext(), r.getPromptLanguage); err != nil {
			logger.Warn(logComponent).Err(err).Msg("修复不完整工具上下文失败")
		}
	}
	return nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// getOrCreateState 获取或创建 per-session 状态
func (r *JiuClawStreamEventRail) getOrCreateState(sid string) *streamSessionState {
	state, ok := r.sessionStates[sid]
	if !ok {
		state = &streamSessionState{
			cond:             sync.NewCond(&r.mu),
			inflightToolCalls: make(map[string]*inflightToolInfo),
		}
		r.sessionStates[sid] = state
	}
	return state
}

// checkpoint 暂停/中止检查点，对齐 Python: await _pause_event.wait() + abort check
func (r *JiuClawStreamEventRail) checkpoint(ctx context.Context, sid string) error {
	r.mu.Lock()
	state := r.getOrCreateState(sid)
	for state.paused && !state.aborted {
		state.cond.Wait()
	}
	aborted := state.aborted
	r.mu.Unlock()

	if aborted {
		return fmt.Errorf("Agent abort requested")
	}
	return nil
}

// resolveSID 解析 per-session 键，对齐 Python: _resolve_sid
func (r *JiuClawStreamEventRail) resolveSID(cbc *agentinterfaces.AgentCallbackContext, session sessioninterfaces.SessionFacade) string {
	if sid, ok := cbc.Extra()[sidKey]; ok {
		if s, ok := sid.(string); ok && s != "" {
			return s
		}
	}
	// 回退：通过 _main_sessions 反查
	if session != nil {
		r.mu.Lock()
		for knownSID, knownSession := range r.sessionStates {
			if knownSession.mainSession == session {
				r.mu.Unlock()
				cbc.Extra()[sidKey] = knownSID
				return knownSID
			}
		}
		r.mu.Unlock()
	}
	return defaultSessionID
}

// getPromptLanguage 获取当前提示词语言，对齐 Python: _get_prompt_language
func (r *JiuClawStreamEventRail) getPromptLanguage() string {
	if r.deepAgent != nil {
		if spb := r.deepAgent.SystemPromptBuilder(); spb != nil {
			if lang := spb.Language(); lang != "" {
				return lang
			}
		}
	}
	return "cn"
}

// getTodoTool 获取或创建 TodoTool 实例，对齐 Python: _get_todo_tool
func (r *JiuClawStreamEventRail) getTodoTool() *todotool.TodoTool {
	if r.mainTodoTool != nil {
		return r.mainTodoTool
	}
	if r.deepAgent == nil {
		return nil
	}
	// 通过 deepAgent 的 workspace 和 sys_operation 创建
	// 留 TODO：需要 DeepAgent 暴露 workspace 路径和 sys_operation
	// 当前先返回 nil
	return nil
}
```

- [ ] **Step 4: 运行测试确认通过**

Run: `cd /home/opensource/uapclaw-gateway && go test ./internal/swarm/agents/harness/common/rails/ -v`

Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/swarm/agents/harness/common/rails/stream_event_rail.go internal/swarm/agents/harness/common/rails/stream_event_rail_test.go
git commit -m "feat(10.6.10): JiuClawStreamEventRail 核心结构体 — 钩子方法 + 暂停/中止/收集 API"
```

---

### Task 6: DeepAdapter 回填

**Files:**
- Modify: `internal/swarm/server/adapter/deep_adapter.go` (行 122-124, 843, 1316-1319, 1320-1323, 1324-1344, 1537-1547, 2097)
- Modify: `internal/swarm/server/adapter/deep_adapter_rails.go` (行 403-409)

- [ ] **Step 1: 修改 streamEventRail 字段类型**

在 `deep_adapter.go` 行 122-124，将 `sainterfaces.AgentRail` 改为 `*commrails.JiuClawStreamEventRail`：

```go
// streamEventRail 流事件护栏
// ⤴️ 10.6.3-10: JiuClawStreamEventRail
streamEventRail *commrails.JiuClawStreamEventRail
```

添加 import 别名：
```go
commrails "github.com/uapclaw/uapclaw-go/internal/swarm/agents/harness/common/rails"
```

- [ ] **Step 2: 实现 buildStreamEventRail**

在 `deep_adapter_rails.go` 行 403-409，替换占位：

```go
// buildStreamEventRail 构建流事件护栏。
// ⤴️ 10.6.3-10: JiuClawStreamEventRail
// Python: _build_stream_event_rail()
func (d *DeepAdapter) buildStreamEventRail() sainterfaces.AgentRail {
	rail := commrails.NewJiuClawStreamEventRail()
	logger.Info(logComponent).Msg("JiuClawStreamEventRail 创建成功")
	return rail
}
```

同时修改返回类型和赋值逻辑（步骤 5 中 `d.streamEventRail = se` 需要类型断言）：

```go
// 步骤 5: streamEventRail
se := d.buildStreamEventRail()
if se != nil {
	d.streamEventRail = se.(*commrails.JiuClawStreamEventRail)
	railsList = append(railsList, se)
}
```

- [ ] **Step 3: 回填 ProcessMessageImpl reset_abort**

在 `deep_adapter.go` 行 843 附近，替换占位：

```go
// 步骤 15: streamEventRail.reset_abort(sessionID)
if d.streamEventRail != nil {
	d.streamEventRail.ResetAbort(sessionID)
}
```

- [ ] **Step 4: 回填 ProcessInterrupt pause/resume**

在 `deep_adapter.go` 行 1316-1323，替换占位：

```go
case "pause":
	if sessionActive && d.streamEventRail != nil {
		d.streamEventRail.Pause(normalizedSID)
	}
	interruptMsg = "执行已暂停"
	logger.Info(logComponent).Str("intent", "pause").Msg("中断: 已暂停执行")

case "resume":
	if sessionActive && d.streamEventRail != nil {
		d.streamEventRail.Resume(normalizedSID)
	}
	interruptMsg = "执行已恢复"
	logger.Info(logComponent).Str("intent", "resume").Msg("中断: 已恢复执行")
```

- [ ] **Step 5: 回填 ProcessInterrupt supplement/cancel**

在 `deep_adapter.go` 行 1324-1344，替换 abort + collect 占位：

```go
case "supplement":
	if newInput != nil {
		d.markSessionActive(normalizedSID)
	}
	if d.streamEventRail != nil {
		d.streamEventRail.Abort(normalizedSID)
		d.streamEventRail.CollectCancelledToolUpdates(normalizedSID)
	}
	// Python: instance.abort() 仅当 otherActiveSessions == 0
	if sessionActive && d.instance != nil && d.otherActiveSessions(normalizedSID) == 0 {
		d.instance.Abort(ctx)
	}
	interruptMsg = "supplement 已处理"
	logger.Info(logComponent).Str("intent", "supplement").Msg("中断: supplement 处理")

case "cancel":
	if d.streamEventRail != nil {
		d.streamEventRail.Abort(normalizedSID)
		d.streamEventRail.CollectCancelledToolUpdates(normalizedSID)
	}
	if sessionActive && d.instance != nil && d.otherActiveSessions(normalizedSID) == 0 {
		d.instance.Abort(ctx)
	}
	d.unmarkSessionActive(normalizedSID)
	if d.streamEventRail != nil {
		d.streamEventRail.ResetForNewTask(normalizedSID)
	}
	interruptMsg = "执行已取消"
	logger.Info(logComponent).Str("intent", "cancel").Msg("中断: cancel 处理")
```

在 interrupt response payload 中添加 cancelled_tools（如当前有 `⤵️ 10.6.3-10` 占位）：

```go
// 收集 cancelled_tool_results 传给前端
var cancelledTools []map[string]any
if d.streamEventRail != nil {
	cancelledTools = d.streamEventRail.GetCancelledToolResults(normalizedSID)
	d.streamEventRail.ClearCancelledToolResults(normalizedSID)
}
// 添加到 response payload
```

- [ ] **Step 6: 回填 AbortOnGatewayDisconnect**

在 `deep_adapter.go` 行 1537-1547，替换占位：

```go
// 步骤 1: 中止 rail 上所有活跃 session
if d.streamEventRail != nil {
	var activeSIDs []string
	for sid, count := range d.activeSessionIDs {
		if count > 0 {
			activeSIDs = append(activeSIDs, sid)
		}
	}
	for _, sid := range activeSIDs {
		d.streamEventRail.Abort(sid)
	}
}
```

- [ ] **Step 7: 回填 unmarkSessionActive**

在 `deep_adapter.go` 行 2097，替换占位：

```go
if count <= 1 {
	delete(d.activeSessionIDs, sessionID)
	if d.streamEventRail != nil {
		d.streamEventRail.CleanupSession(sessionID)
	}
}
```

- [ ] **Step 8: 编译验证**

Run: `cd /home/opensource/uapclaw-gateway && export GOPROXY=https://goproxy.cn,direct && go build ./...`

Expected: 编译成功

- [ ] **Step 9: Commit**

```bash
git add internal/swarm/server/adapter/deep_adapter.go internal/swarm/server/adapter/deep_adapter_rails.go
git commit -m "feat(10.6.10): DeepAdapter 回填 StreamEventRail — pause/resume/abort/reset/cleanup/collect"
```

---

### Task 7: 修正 UserHookRail 优先级注释

**Files:**
- Modify: `internal/swarm/server/hooks/user_hook_rail.go:19`

- [ ] **Step 1: 修正注释**

将行 19 的 `JiuClawStreamEventRail(50)` 改为 `JiuClawStreamEventRail(80)`：

```go
// Priority=60: 在 SecurityRail(80) 之后，JiuClawStreamEventRail(80) 同优先级
```

- [ ] **Step 2: Commit**

```bash
git add internal/swarm/server/hooks/user_hook_rail.go
git commit -m "fix: 修正 UserHookRail 注释中 StreamEventRail 优先级 50→80"
```

---

### Task 8: 更新 doc.go

**Files:**
- Modify: `internal/swarm/agents/harness/common/rails/doc.go`

- [ ] **Step 1: 更新文件目录**

在 doc.go 的文件目录中添加 StreamEvent 相关文件：

```
//	rails/
//	├── doc.go                        # 包文档
//	├── avatar_rail.go                # AvatarPromptRail 数字分身 Rail
//	├── stream_event_rail.go          # JiuClawStreamEventRail 流事件护栏（钩子 + 暂停/中止/收集 API）
//	├── stream_event_helpers.go       # 辅助函数（boolish/infer/extract/parse/truncate/常量）
//	├── stream_event_emit.go          # 流式事件发射（emitToolCall/Result/Update/ContextUsage/AskUser/Todo）
//	├── stream_event_context.go       # 上下文修复（fixIncompleteToolContext/ensureJSONArguments/fixMissingQuotes）
//	├── project_memory_rail.go        # ProjectMemoryRail 项目记忆护栏
//	├── response_prompt_rail.go       # ResponsePromptRail 响应提示词护栏
//	├── structured_ask_user_rail.go    # StructuredAskUserRail + StructuredAskUserPayload
//	├── structured_ask_user_tool.go    # StructuredAskUserTool + 扩展 schema
//	├── runtime_prompt_rail.go        # RuntimePromptRail 运行时提示词护栏
//	├── project_memory/               # 项目记忆文件发现/加载/缓存/合并
//	│   ├── doc.go                    # 子包文档
//	│   ├── files.go                  # 文件发现/加载/缓存/合并
//	│   └── section.go                # PromptSection 工厂
//	└── permissions/
//	    ├── doc.go                    # 包文档
//	    └── owner_scopes.go           # OwnerScopesPermissionContext 权限上下文
```

同时在包功能概述中添加 StreamEvent 描述。

- [ ] **Step 2: Commit**

```bash
git add internal/swarm/agents/harness/common/rails/doc.go
git commit -m "docs(10.6.10): 更新 rails/doc.go 文件目录 — 添加 StreamEvent 文件"
```

---

### Task 9: 更新 IMPLEMENTATION_PLAN.md

**Files:**
- Modify: `IMPLEMENTATION_PLAN.md`

- [ ] **Step 1: 更新 10.6.3-10 行**

将 `| 10.6.3-10 | 🔄 | Swarm Rails | AskUser✅/Avatar✅/Permissions✅/Interrupt✅/ProjectMemory✅/ResponsePrompt✅/RuntimePrompt✅/StreamEvent |` 改为 `| 10.6.3-10 | ✅ | Swarm Rails | AskUser✅/Avatar✅/Permissions✅/Interrupt✅/ProjectMemory✅/ResponsePrompt✅/RuntimePrompt✅/StreamEvent✅ |`

- [ ] **Step 2: Commit**

```bash
git add IMPLEMENTATION_PLAN.md
git commit -m "docs: 更新 IMPLEMENTATION_PLAN.md — 10.6.3-10 StreamEvent 完成"
```

---

### Task 10: 全量编译和测试验证

**Files:** 无

- [ ] **Step 1: 编译**

Run: `cd /home/opensource/uapclaw-gateway && export GOPROXY=https://goproxy.cn,direct && go build ./...`

Expected: 编译成功，无错误

- [ ] **Step 2: 运行单元测试**

Run: `cd /home/opensource/uapclaw-gateway && export GOPROXY=https://goproxy.cn,direct && go test ./internal/swarm/agents/harness/common/rails/ -v -cover`

Expected: 所有测试通过，覆盖率 ≥ 85%

- [ ] **Step 3: 运行 adapter 包测试**

Run: `cd /home/opensource/uapclaw-gateway && export GOPROXY=https://goproxy.cn,direct && go test ./internal/swarm/server/adapter/ -v -cover`

Expected: 所有测试通过

- [ ] **Step 4: Final commit if any fixes needed**

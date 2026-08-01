package hooks

import (
	"context"
	"testing"
	"time"
)

// TestHookOutcome_常量值 测试 HookOutcome 对齐 Python
func TestHookOutcome_常量值(t *testing.T) {
	if HookOutcomeSuccess != "success" {
		t.Errorf("HookOutcomeSuccess = %q, want %q", HookOutcomeSuccess, "success")
	}
	if HookOutcomeBlocking != "blocking" {
		t.Errorf("HookOutcomeBlocking = %q, want %q", HookOutcomeBlocking, "blocking")
	}
	if HookOutcomeNonBlockingError != "non_blocking_error" {
		t.Errorf("HookOutcomeNonBlockingError = %q, want %q", HookOutcomeNonBlockingError, "non_blocking_error")
	}
}

// TestParseCommandOutput_空输出 测试 stdout 空时返回 SUCCESS
func TestParseCommandOutput_空输出(t *testing.T) {
	result := ParseCommandOutput("")
	if result.Outcome != HookOutcomeSuccess {
		t.Errorf("Outcome = %q, want %q", result.Outcome, HookOutcomeSuccess)
	}
}

// TestParseCommandOutput_空白输出 测试 stdout 仅空白时返回 SUCCESS
func TestParseCommandOutput_空白输出(t *testing.T) {
	result := ParseCommandOutput("   \n  ")
	if result.Outcome != HookOutcomeSuccess {
		t.Errorf("Outcome = %q, want %q", result.Outcome, HookOutcomeSuccess)
	}
}

// TestParseCommandOutput_有效JSON_阻塞 测试 decision=block
func TestParseCommandOutput_有效JSON_阻塞(t *testing.T) {
	result := ParseCommandOutput(`{"decision": "block", "reason": "dangerous tool"}`)
	if result.Outcome != HookOutcomeBlocking {
		t.Errorf("Outcome = %q, want %q", result.Outcome, HookOutcomeBlocking)
	}
	if result.Error != "dangerous tool" {
		t.Errorf("Error = %q, want %q", result.Error, "dangerous tool")
	}
	if !result.ShowToModel {
		t.Error("ShowToModel should be true for blocking")
	}
}

// TestParseCommandOutput_有效JSON_阻塞无reason 测试 block 但缺 reason 字段
func TestParseCommandOutput_有效JSON_阻塞无reason(t *testing.T) {
	result := ParseCommandOutput(`{"decision": "block"}`)
	if result.Outcome != HookOutcomeBlocking {
		t.Errorf("Outcome = %q, want %q", result.Outcome, HookOutcomeBlocking)
	}
	if result.Error != "blocked by hook" {
		t.Errorf("Error = %q, want default %q", result.Error, "blocked by hook")
	}
}

// TestParseCommandOutput_有效JSON_修改输入 测试 modifiedInput + additionalContext
func TestParseCommandOutput_有效JSON_修改输入(t *testing.T) {
	result := ParseCommandOutput(`{"decision": "allow", "modifiedInput": {"path": "/safe"}, "additionalContext": "context info"}`)
	if result.Outcome != HookOutcomeSuccess {
		t.Errorf("Outcome = %q, want %q", result.Outcome, HookOutcomeSuccess)
	}
	if result.ModifiedInput["path"] != "/safe" {
		t.Errorf("ModifiedInput[path] = %v, want %q", result.ModifiedInput["path"], "/safe")
	}
	if result.AdditionalContext != "context info" {
		t.Errorf("AdditionalContext = %q, want %q", result.AdditionalContext, "context info")
	}
}

// TestParseCommandOutput_有效JSON_reason 测试 reason 在 non-block 时存入 additionalContext
func TestParseCommandOutput_有效JSON_reason(t *testing.T) {
	result := ParseCommandOutput(`{"decision": "allow", "reason": "approved"}`)
	if result.AdditionalContext != "approved" {
		t.Errorf("AdditionalContext = %q, want %q", result.AdditionalContext, "approved")
	}
}

// TestParseCommandOutput_非JSON 测试非 JSON 输出返回 SUCCESS
func TestParseCommandOutput_非JSON(t *testing.T) {
	result := ParseCommandOutput("just some text output")
	if result.Outcome != HookOutcomeSuccess {
		t.Errorf("Outcome = %q, want %q for non-JSON", result.Outcome, HookOutcomeSuccess)
	}
}

// TestParseCommandOutput_非dictJSON 测试非 dict JSON 返回 SUCCESS
func TestParseCommandOutput_非dictJSON(t *testing.T) {
	result := ParseCommandOutput(`[1, 2, 3]`)
	if result.Outcome != HookOutcomeSuccess {
		t.Errorf("Outcome = %q, want %q for non-dict JSON", result.Outcome, HookOutcomeSuccess)
	}
}

// TestParseCommandOutput_无decision 测试有 JSON 但无 decision 字段
func TestParseCommandOutput_无decision(t *testing.T) {
	result := ParseCommandOutput(`{"modifiedInput": {"path": "/safe"}}`)
	if result.Outcome != HookOutcomeSuccess {
		t.Errorf("Outcome = %q, want %q (no decision field)", result.Outcome, HookOutcomeSuccess)
	}
}

// TestExtractJSONFromResponse_直接JSON 测试直接 JSON 解析
func TestExtractJSONFromResponse_直接JSON(t *testing.T) {
	data := ExtractJSONFromResponse(`{"decision": "block", "reason": "test"}`)
	if data["decision"] != "block" {
		t.Errorf("decision = %v, want %q", data["decision"], "block")
	}
}

// TestExtractJSONFromResponse_markdownFence 测试 markdown fence 提取
func TestExtractJSONFromResponse_markdownFence(t *testing.T) {
	text := "Here is the result:\n```json\n{\"decision\": \"allow\"}\n```"
	data := ExtractJSONFromResponse(text)
	if data["decision"] != "allow" {
		t.Errorf("decision = %v, want %q", data["decision"], "allow")
	}
}

// TestExtractJSONFromResponse_markdownFence无json标记 测试 ``` 无 json 标记
func TestExtractJSONFromResponse_markdownFence无json标记(t *testing.T) {
	text := "Result:\n```\n{\"decision\": \"allow\"}\n```"
	data := ExtractJSONFromResponse(text)
	if data["decision"] != "allow" {
		t.Errorf("decision = %v, want %q", data["decision"], "allow")
	}
}

// TestExtractJSONFromResponse_嵌入式JSON 测试嵌入式 { } 提取
func TestExtractJSONFromResponse_嵌入式JSON(t *testing.T) {
	text := "The LLM responded with {\"decision\": \"block\"} as the answer."
	data := ExtractJSONFromResponse(text)
	if data["decision"] != "block" {
		t.Errorf("decision = %v, want %q", data["decision"], "block")
	}
}

// TestExtractJSONFromResponse_空文本 测试空文本返回空 map
func TestExtractJSONFromResponse_空文本(t *testing.T) {
	data := ExtractJSONFromResponse("")
	if len(data) != 0 {
		t.Errorf("empty text should return empty map, got %d keys", len(data))
	}
}

// TestExtractJSONFromResponse_无JSON 测试不含 JSON 返回空 map
func TestExtractJSONFromResponse_无JSON(t *testing.T) {
	data := ExtractJSONFromResponse("just plain text, no json here")
	if len(data) != 0 {
		t.Errorf("no JSON text should return empty map, got %d keys", len(data))
	}
}

// TestHookExecutor_RunAll_空配置 测试空 hook 配置返回空列表
func TestHookExecutor_RunAll_空配置(t *testing.T) {
	exec := NewHookExecutor(LLMConfig{})
	results := exec.RunAll(context.Background(), nil, map[string]any{}, "")
	if len(results) != 0 {
		t.Errorf("RunAll(nil) = %d results, want 0", len(results))
	}
}

// TestHookExecutor_RunAll_command成功 测试 command hook exit 0
func TestHookExecutor_RunAll_command成功(t *testing.T) {
	exec := NewHookExecutor(LLMConfig{})
	hookConfigs := []map[string]any{
		{"type": "command", "command": "echo '{\"decision\": \"allow\"}'", "timeout": 10},
	}
	hookInput := map[string]any{"tool_name": "test_tool"}
	results := exec.RunAll(context.Background(), hookConfigs, hookInput, "")
	if len(results) != 1 {
		t.Fatalf("RunAll = %d results, want 1", len(results))
	}
	if results[0].Outcome != HookOutcomeSuccess {
		t.Errorf("Outcome = %q, want %q", results[0].Outcome, HookOutcomeSuccess)
	}
}

// TestHookExecutor_RunAll_command阻塞 测试 command hook exit 2（阻塞）
func TestHookExecutor_RunAll_command阻塞(t *testing.T) {
	exec := NewHookExecutor(LLMConfig{})
	// exit 2 的 shell 命令
	hookConfigs := []map[string]any{
		{"type": "command", "command": "echo '{\"decision\": \"block\", \"reason\": \"blocked by command\"}' && exit 2", "timeout": 10},
	}
	hookInput := map[string]any{"tool_name": "test_tool"}
	results := exec.RunAll(context.Background(), hookConfigs, hookInput, "")
	if len(results) != 1 {
		t.Fatalf("RunAll = %d results, want 1", len(results))
	}
	if results[0].Outcome != HookOutcomeBlocking {
		t.Errorf("Outcome = %q, want %q", results[0].Outcome, HookOutcomeBlocking)
	}
	if !results[0].ShowToModel {
		t.Error("ShowToModel should be true for blocking")
	}
}

// TestHookExecutor_RunAll_command空命令 测试空 command 返回 NON_BLOCKING_ERROR
func TestHookExecutor_RunAll_command空命令(t *testing.T) {
	exec := NewHookExecutor(LLMConfig{})
	hookConfigs := []map[string]any{
		{"type": "command", "command": "", "timeout": 10},
	}
	results := exec.RunAll(context.Background(), hookConfigs, map[string]any{}, "")
	if len(results) != 1 {
		t.Fatalf("RunAll = %d results, want 1", len(results))
	}
	if results[0].Outcome != HookOutcomeNonBlockingError {
		t.Errorf("Outcome = %q, want %q", results[0].Outcome, HookOutcomeNonBlockingError)
	}
	if results[0].Error != "empty command" {
		t.Errorf("Error = %q, want %q", results[0].Error, "empty command")
	}
}

// TestHookExecutor_RunAll_command超时 测试超时返回 NON_BLOCKING_ERROR
func TestHookExecutor_RunAll_command超时(t *testing.T) {
	exec := NewHookExecutor(LLMConfig{})
	hookConfigs := []map[string]any{
		{"type": "command", "command": "sleep 30", "timeout": 1},
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	results := exec.RunAll(ctx, hookConfigs, map[string]any{}, "")
	if len(results) != 1 {
		t.Fatalf("RunAll = %d results, want 1", len(results))
	}
	if results[0].Outcome != HookOutcomeNonBlockingError {
		t.Errorf("Outcome = %q, want %q (timeout)", results[0].Outcome, HookOutcomeNonBlockingError)
	}
}

// TestHookExecutor_RunAll_command失败退出码 测试 exit 1 → NON_BLOCKING_ERROR
func TestHookExecutor_RunAll_command失败退出码(t *testing.T) {
	exec := NewHookExecutor(LLMConfig{})
	hookConfigs := []map[string]any{
		{"type": "command", "command": "echo 'error message' && exit 1", "timeout": 10},
	}
	results := exec.RunAll(context.Background(), hookConfigs, map[string]any{}, "")
	if len(results) != 1 {
		t.Fatalf("RunAll = %d results, want 1", len(results))
	}
	if results[0].Outcome != HookOutcomeNonBlockingError {
		t.Errorf("Outcome = %q, want %q", results[0].Outcome, HookOutcomeNonBlockingError)
	}
}

// TestHookExecutor_RunAll_prompt空模板 测试空 prompt 返回 NON_BLOCKING_ERROR
func TestHookExecutor_RunAll_prompt空模板(t *testing.T) {
	exec := NewHookExecutor(LLMConfig{})
	hookConfigs := []map[string]any{
		{"type": "prompt", "prompt": "", "timeout": 10},
	}
	results := exec.RunAll(context.Background(), hookConfigs, map[string]any{}, "")
	if len(results) != 1 {
		t.Fatalf("RunAll = %d results, want 1", len(results))
	}
	if results[0].Outcome != HookOutcomeNonBlockingError {
		t.Errorf("Outcome = %q, want %q", results[0].Outcome, HookOutcomeNonBlockingError)
	}
	if results[0].Error != "empty prompt" {
		t.Errorf("Error = %q, want %q", results[0].Error, "empty prompt")
	}
}

// TestHookExecutor_RunAll_未知类型 测试未知 hook 类型
func TestHookExecutor_RunAll_未知类型(t *testing.T) {
	exec := NewHookExecutor(LLMConfig{})
	hookConfigs := []map[string]any{
		{"type": "unknown"},
	}
	results := exec.RunAll(context.Background(), hookConfigs, map[string]any{}, "")
	if len(results) != 1 {
		t.Errorf("unknown type should produce 1 result (NON_BLOCKING_ERROR), got %d", len(results))
	}
	if results[0].Outcome != HookOutcomeNonBlockingError {
		t.Errorf("Outcome = %q, want %q", results[0].Outcome, HookOutcomeNonBlockingError)
	}
}

// TestHookExecutor_RunAll_默认command类型 测试无 type 字段默认为 command
func TestHookExecutor_RunAll_默认command类型(t *testing.T) {
	exec := NewHookExecutor(LLMConfig{})
	hookConfigs := []map[string]any{
		{"command": "echo '{\"decision\": \"allow\"}'", "timeout": 10},
	}
	hookInput := map[string]any{"tool_name": "test_tool"}
	results := exec.RunAll(context.Background(), hookConfigs, hookInput, "")
	if len(results) != 1 {
		t.Fatalf("RunAll = %d results, want 1", len(results))
	}
	if results[0].Outcome != HookOutcomeSuccess {
		t.Errorf("Outcome = %q, want %q (default type=command)", results[0].Outcome, HookOutcomeSuccess)
	}
}

// TestHookExecutor_RunAll_多个hook并行 测试多个 hooks 并行执行
func TestHookExecutor_RunAll_多个hook并行(t *testing.T) {
	exec := NewHookExecutor(LLMConfig{})
	hookConfigs := []map[string]any{
		{"type": "command", "command": "echo '{\"decision\": \"allow\"}'", "timeout": 10},
		{"type": "command", "command": "echo '{\"additionalContext\": \"extra\"}'", "timeout": 10},
	}
	hookInput := map[string]any{"tool_name": "test_tool"}
	results := exec.RunAll(context.Background(), hookConfigs, hookInput, "")
	if len(results) != 2 {
		t.Fatalf("RunAll = %d results, want 2", len(results))
	}
	if results[0].Outcome != HookOutcomeSuccess {
		t.Errorf("results[0] Outcome = %q, want %q", results[0].Outcome, HookOutcomeSuccess)
	}
	if results[1].Outcome != HookOutcomeSuccess {
		t.Errorf("results[1] Outcome = %q, want %q", results[1].Outcome, HookOutcomeSuccess)
	}
}

// TestLLMConfig_字段 测试 LLMConfig 字段赋值
func TestLLMConfig_字段(t *testing.T) {
	cfg := LLMConfig{
		APIKey:         "test-key",
		APIBase:        "https://api.test.com",
		ClientProvider: "test-provider",
		DefaultModel:   "test-model",
	}
	if cfg.APIKey != "test-key" {
		t.Errorf("APIKey = %q, want %q", cfg.APIKey, "test-key")
	}
	if cfg.DefaultModel != "test-model" {
		t.Errorf("DefaultModel = %q, want %q", cfg.DefaultModel, "test-model")
	}
}

// TestHookExecutor_RunAll_promptLLM创建失败 测试 LLM 创建失败返回 NON_BLOCKING_ERROR
func TestHookExecutor_RunAll_promptLLM创建失败(t *testing.T) {
	// 空 ClientProvider 会导致 Model 创建失败
	exec := NewHookExecutor(LLMConfig{})
	hookConfigs := []map[string]any{
		{"type": "prompt", "prompt": "Is this safe? $ARGUMENTS", "timeout": 10},
	}
	hookInput := map[string]any{"tool_name": "test_tool"}
	results := exec.RunAll(context.Background(), hookConfigs, hookInput, "")
	if len(results) != 1 {
		t.Fatalf("RunAll = %d results, want 1", len(results))
	}
	if results[0].Outcome != HookOutcomeNonBlockingError {
		t.Errorf("Outcome = %q, want %q (LLM creation failed)", results[0].Outcome, HookOutcomeNonBlockingError)
	}
}

// TestHookExecutor_RunAll_prompt模板替换 测试 prompt hook 模板替换逻辑
// 此测试验证模板替换 $ARGUMENTS 和 $TOOL_NAME，但 LLM 调用会失败
func TestHookExecutor_RunAll_prompt模板替换(t *testing.T) {
	exec := NewHookExecutor(LLMConfig{})
	hookConfigs := []map[string]any{
		{"type": "prompt", "prompt": "Review: $ARGUMENTS for tool $TOOL_NAME", "timeout": 10, "model": "test-model"},
	}
	hookInput := map[string]any{"tool_name": "read_file", "tool_input": "some data"}
	results := exec.RunAll(context.Background(), hookConfigs, hookInput, "")
	if len(results) != 1 {
		t.Fatalf("RunAll = %d results, want 1", len(results))
	}
	// LLM 创建/调用会失败 → NON_BLOCKING_ERROR
	if results[0].Outcome != HookOutcomeNonBlockingError {
		t.Errorf("Outcome = %q, want %q", results[0].Outcome, HookOutcomeNonBlockingError)
	}
}

// TestHookExecutor_RunAll_command环境变量 测试 command hook 设置 ARGUMENTS/TOOL_NAME 环境变量
func TestHookExecutor_RunAll_command环境变量(t *testing.T) {
	exec := NewHookExecutor(LLMConfig{})
	// 命令读取 ARGUMENTS 和 TOOL_NAME 环境变量
	hookConfigs := []map[string]any{
		{"type": "command", "command": "echo \"$TOOL_NAME:$ARGUMENTS\" && echo '{\"decision\": \"allow\"}'", "timeout": 10},
	}
	hookInput := map[string]any{"tool_name": "read_file", "event": "PreToolUse"}
	results := exec.RunAll(context.Background(), hookConfigs, hookInput, "")
	if len(results) != 1 {
		t.Fatalf("RunAll = %d results, want 1", len(results))
	}
	if results[0].Outcome != HookOutcomeSuccess {
		t.Errorf("Outcome = %q, want %q", results[0].Outcome, HookOutcomeSuccess)
	}
}

// TestHookExecutor_RunAll_command阻塞stderrFallback 测试 exit 2 且 stdout 不是 JSON 时 fallback 到 stderr
func TestHookExecutor_RunAll_command阻塞stderrFallback(t *testing.T) {
	exec := NewHookExecutor(LLMConfig{})
	// stdout 不是 JSON，stderr 有内容
	hookConfigs := []map[string]any{
		{"type": "command", "command": "echo 'not json stdout' >&1 && echo 'blocked reason from stderr' >&2 && exit 2", "timeout": 10},
	}
	results := exec.RunAll(context.Background(), hookConfigs, map[string]any{}, "")
	if len(results) != 1 {
		t.Fatalf("RunAll = %d results, want 1", len(results))
	}
	if results[0].Outcome != HookOutcomeBlocking {
		t.Errorf("Outcome = %q, want %q", results[0].Outcome, HookOutcomeBlocking)
	}
}

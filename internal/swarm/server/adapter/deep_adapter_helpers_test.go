package adapter

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool"
	hschema "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/schema"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/stream"
	agentschema "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/schema"
	"github.com/uapclaw/uapclaw-go/internal/swarm/schema"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// TestDeepAdapter_Cleanup 测试 Cleanup 不报错。
func TestDeepAdapter_Cleanup(t *testing.T) {
	d := NewDeepAdapter()
	if err := d.Cleanup(); err != nil {
		t.Errorf("Cleanup() error = %v", err)
	}
}

// TestDeepAdapter_AbortOnGatewayDisconnect_无实例 测试 instance 为 nil 时不 panic。
func TestDeepAdapter_AbortOnGatewayDisconnect_无实例(t *testing.T) {
	d := NewDeepAdapter()
	ctx := t.Context()
	// 不应 panic
	d.AbortOnGatewayDisconnect(ctx)
}

// TestDeepAdapter_AbortOnGatewayDisconnect_有活跃Session 测试有活跃 session 时的逻辑。
func TestDeepAdapter_AbortOnGatewayDisconnect_有活跃Session(t *testing.T) {
	d := NewDeepAdapter()
	d.markSessionActive("s1")
	d.markSessionActive("s2")
	ctx := t.Context()
	// streamEventRail 为 nil，仅遍历 activeSessionIDs
	d.AbortOnGatewayDisconnect(ctx)
}

// TestDeepAdapter_TryStartDreaming_已启动 测试已启动时直接返回。
func TestDeepAdapter_TryStartDreaming_已启动(t *testing.T) {
	d := NewDeepAdapter()
	d.dreamingStarted = true
	ctx := t.Context()
	if err := d.TryStartDreaming(ctx, nil); err != nil {
		t.Errorf("TryStartDreaming 已启动应返回 nil, got %v", err)
	}
}

// TestDeepAdapter_TryStartDreaming_模式未启用 测试 dreaming 模式未启用时跳过。
func TestDeepAdapter_TryStartDreaming_模式未启用(t *testing.T) {
	d := NewDeepAdapter()
	d.dreamingMode = ""
	ctx := t.Context()
	if err := d.TryStartDreaming(ctx, nil); err != nil {
		t.Errorf("TryStartDreaming 模式未启用应返回 nil, got %v", err)
	}
}

// TestDeepAdapter_TryStartDreaming_Agent忙碌 测试忙碌时跳过。
func TestDeepAdapter_TryStartDreaming_Agent忙碌(t *testing.T) {
	d := NewDeepAdapter()
	d.dreamingMode = "agent"
	ctx := t.Context()
	busyChecker := func() bool { return true }
	if err := d.TryStartDreaming(ctx, busyChecker); err != nil {
		t.Errorf("TryStartDreaming 忙碌时应返回 nil, got %v", err)
	}
	if d.dreamingStarted {
		t.Error("忙碌时不应标记 dreamingStarted")
	}
}

// TestDeepAdapter_TryStartDreaming_正常启动 测试正常启动。
func TestDeepAdapter_TryStartDreaming_正常启动(t *testing.T) {
	d := NewDeepAdapter()
	d.dreamingMode = "agent"
	ctx := t.Context()
	if err := d.TryStartDreaming(ctx, nil); err != nil {
		t.Errorf("TryStartDreaming error = %v", err)
	}
	if !d.dreamingStarted {
		t.Error("应标记 dreamingStarted = true")
	}
}

// TestDeepAdapter_TryStopDreaming_未启动 测试未启动时跳过。
func TestDeepAdapter_TryStopDreaming_未启动(t *testing.T) {
	d := NewDeepAdapter()
	ctx := t.Context()
	if err := d.TryStopDreaming(ctx); err != nil {
		t.Errorf("TryStopDreaming 未启动应返回 nil, got %v", err)
	}
}

// TestDeepAdapter_TryStopDreaming_已启动 测试已启动时停止。
func TestDeepAdapter_TryStopDreaming_已启动(t *testing.T) {
	d := NewDeepAdapter()
	d.dreamingStarted = true
	ctx := t.Context()
	if err := d.TryStopDreaming(ctx); err != nil {
		t.Errorf("TryStopDreaming error = %v", err)
	}
	if d.dreamingStarted {
		t.Error("应标记 dreamingStarted = false")
	}
}

// TestDeepAdapter_CompressContext_无实例 测试 instance 为 nil 时返回 noop。
func TestDeepAdapter_CompressContext_无实例(t *testing.T) {
	d := NewDeepAdapter()
	ctx := t.Context()
	result, err := d.CompressContext(ctx, "s1", nil, false)
	if err != nil {
		t.Errorf("CompressContext 无实例应返回 nil error, got %v", err)
	}
	if result == nil || result["result"] != "noop" {
		t.Errorf("CompressContext 无实例应返回 {result: noop}, got %v", result)
	}
}

// TestDeepAdapter_GetContextUsage_无实例 测试 instance 为 nil 时返回 nil, nil。
func TestDeepAdapter_GetContextUsage_无实例(t *testing.T) {
	d := NewDeepAdapter()
	ctx := t.Context()
	result, err := d.GetContextUsage(ctx, "s1")
	if result != nil || err != nil {
		t.Errorf("GetContextUsage 无实例应返回 nil, nil")
	}
}

// TestDeepAdapter_GenerateRecap_无实例 测试 instance 为 nil 时返回 no_turn。
func TestDeepAdapter_GenerateRecap_无实例(t *testing.T) {
	d := NewDeepAdapter()
	ctx := t.Context()
	result, err := d.GenerateRecap(ctx, "s1")
	if err != nil {
		t.Errorf("GenerateRecap 无实例应返回 nil error, got %v", err)
	}
	if result == nil || result["status"] != "no_turn" {
		t.Errorf("GenerateRecap 无实例应返回 {status: no_turn}, got %v", result)
	}
}

// TestBuildRecapPrompt_空memory 测试 memory 为空字符串时不拼接前缀。
func TestBuildRecapPrompt_空memory(t *testing.T) {
	result := buildRecapPrompt("")
	if result == "" {
		t.Error("buildRecapPrompt 返回不应为空")
	}
	if strings.HasPrefix(result, "Session memory") {
		t.Error("空 memory 不应包含 Session memory 前缀块")
	}
	if !strings.Contains(result, "quick recap") {
		t.Error("prompt 应包含 'quick recap' 指令")
	}
	if !strings.Contains(result, "1-3 short sentences") {
		t.Error("prompt 应包含 '1-3 short sentences' 指令")
	}
}

// TestBuildRecapPrompt_有memory 测试 memory 非空时拼接前缀块。
func TestBuildRecapPrompt_有memory(t *testing.T) {
	result := buildRecapPrompt("user is building a CLI tool")
	if !strings.Contains(result, "Session memory (broader context):") {
		t.Error("非空 memory 应包含 'Session memory (broader context):' 前缀")
	}
	if !strings.Contains(result, "user is building a CLI tool") {
		t.Error("prompt 应包含传入的 memory 内容")
	}
	if !strings.Contains(result, "quick recap") {
		t.Error("prompt 应包含英文指令模板")
	}
}

// TestDeepAdapter_IsApprovalEvent 测试审批事件前缀检查。
func TestDeepAdapter_IsApprovalEvent(t *testing.T) {
	d := NewDeepAdapter()
	tests := []struct {
		requestID string
		want      bool
	}{
		{"skill_evolve_123", true},
		{"evolve_simplify_456", true},
		{"team_skill_evolve_789", true},
		{"unknown_prefix", false},
		{"", false},
	}
	for _, tt := range tests {
		if got := d.isApprovalEvent(tt.requestID); got != tt.want {
			t.Errorf("isApprovalEvent(%q) = %v, want %v", tt.requestID, got, tt.want)
		}
	}
}

// TestDeepAdapter_HandleSlashCommand_非斜杠命令 测试非斜杠命令返回 nil。
func TestDeepAdapter_HandleSlashCommand_非斜杠命令(t *testing.T) {
	d := NewDeepAdapter()
	ctx := t.Context()
	result, err := d.handleSlashCommand(ctx, "hello", "s1", "agent.plan")
	if result != nil || err != nil {
		t.Errorf("非斜杠命令应返回 nil, nil")
	}
}

// TestDeepAdapter_HandleSlashCommand_空查询 测试空查询返回 nil。
func TestDeepAdapter_HandleSlashCommand_空查询(t *testing.T) {
	d := NewDeepAdapter()
	ctx := t.Context()
	result, err := d.handleSlashCommand(ctx, "", "s1", "agent.plan")
	if result != nil || err != nil {
		t.Errorf("空查询应返回 nil, nil")
	}
}

// TestDeepAdapter_HandleSlashCommand_未知斜杠命令 测试未知斜杠命令返回 nil。
func TestDeepAdapter_HandleSlashCommand_未知斜杠命令(t *testing.T) {
	d := NewDeepAdapter()
	ctx := t.Context()
	result, err := d.handleSlashCommand(ctx, "/unknown", "s1", "agent.plan")
	if result != nil || err != nil {
		t.Errorf("未知斜杠命令应返回 nil, nil")
	}
}

// TestDeepAdapter_OptionMatches 测试选项匹配逻辑。
func TestDeepAdapter_OptionMatches(t *testing.T) {
	d := NewDeepAdapter()
	option := map[string]any{"id": "opt1"}

	tests := []struct {
		name    string
		answers any
		want    bool
	}{
		{"nil选项", nil, false},
		{"nil答案", nil, false},
		{"切片匹配", []any{map[string]any{"id": "opt1"}}, true},
		{"切片不匹配", []any{map[string]any{"id": "opt2"}}, false},
		{"字符串匹配", []any{"opt1"}, true},
		{"字符串不匹配", []any{"opt2"}, false},
		{"map匹配", map[string]any{"id": "opt1"}, true},
		{"map不匹配", map[string]any{"id": "opt2"}, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.name == "nil选项" {
				if d.optionMatches(nil, tt.answers) != tt.want {
					t.Errorf("optionMatches(nil, answers) != %v", tt.want)
				}
				return
			}
			if got := d.optionMatches(option, tt.answers); got != tt.want {
				t.Errorf("optionMatches() = %v, want %v", got, tt.want)
			}
		})
	}
}

// TestDeepAdapter_OptionMatches_空ID 测试空 ID 返回 false。
func TestDeepAdapter_OptionMatches_空ID(t *testing.T) {
	d := NewDeepAdapter()
	option := map[string]any{"id": ""}
	if d.optionMatches(option, []any{"opt1"}) {
		t.Error("空 ID 应返回 false")
	}
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// TestResolveModelForRequest_默认模型 测试空请求返回默认模型。
func TestResolveModelForRequest_默认模型(t *testing.T) {
	d := NewDeepAdapter()
	d.modelCache = map[string]*llm.Model{"gpt-4": nil}
	d.model = nil
	d.modelNameToKeys = map[string][]string{"gpt-4": {"gpt-4#0"}}

	req := schema.NewAgentRequest("req-1", "ch-1", schema.ReqMethod("chat.send"), nil)
	got := d.resolveModelForRequest(req)
	if got != nil {
		t.Errorf("默认模型为 nil，got %v", got)
	}
}

// TestResolveModelForRequest_精确匹配 测试精确匹配模型缓存。
func TestResolveModelForRequest_精确匹配(t *testing.T) {
	d := NewDeepAdapter()
	fakeModel := &llm.Model{}
	d.modelCache = map[string]*llm.Model{"qwen-max": fakeModel}
	d.model = nil
	d.modelNameToKeys = map[string][]string{"qwen-max": {"qwen-max#0"}}

	params, _ := json.Marshal(map[string]any{"model_name": "qwen-max"})
	req := schema.NewAgentRequest("req-1", "ch-1", schema.ReqMethod("chat.send"), params)
	got := d.resolveModelForRequest(req)
	if got != fakeModel {
		t.Error("应精确匹配到 qwen-max 模型")
	}
}

// TestGetDefaultModels_defaults列表 测试新格式 defaults 列表。
func TestGetDefaultModels_defaults列表(t *testing.T) {
	configBase := map[string]any{
		"models": map[string]any{
			"defaults": []any{
				map[string]any{"model_client_config": map[string]any{"model_name": "qwen"}},
				map[string]any{"model_client_config": map[string]any{"model_name": "gpt4"}},
			},
		},
	}
	result := getDefaultModels(configBase)
	if len(result) != 2 {
		t.Errorf("getDefaultModels 返回 %d 条，want 2", len(result))
	}
}

// TestGetDefaultModels_default对象 测试旧格式 default 对象。
func TestGetDefaultModels_default对象(t *testing.T) {
	configBase := map[string]any{
		"models": map[string]any{
			"default": map[string]any{"model_client_config": map[string]any{"model_name": "qwen"}},
		},
	}
	result := getDefaultModels(configBase)
	if len(result) != 1 {
		t.Errorf("getDefaultModels 返回 %d 条，want 1", len(result))
	}
}

// TestGetDefaultModels_无models段 测试无 models 段。
func TestGetDefaultModels_无models段(t *testing.T) {
	result := getDefaultModels(map[string]any{})
	if result != nil {
		t.Errorf("无 models 段应返回 nil，got %v", result)
	}
}

// TestParamsInt 测试整数取值。
func TestParamsInt(t *testing.T) {
	params := map[string]any{
		"float_val": float64(10),
		"int_val":   15,
		"str_val":   "not_int",
	}

	tests := []struct {
		key        string
		defaultVal int
		want       int
	}{
		{"float_val", 0, 10},
		{"int_val", 0, 15},
		{"str_val", 99, 99},
		{"missing", 99, 99},
	}

	for _, tt := range tests {
		if got := paramsInt(params, tt.key, tt.defaultVal); got != tt.want {
			t.Errorf("paramsInt(%q) = %d, want %d", tt.key, got, tt.want)
		}
	}
}

// TestResolveEnableTaskLoop 测试任务循环开关解析。
func TestResolveEnableTaskLoop(t *testing.T) {
	d := NewDeepAdapter()
	if !d.resolveEnableTaskLoop(map[string]any{"enable_task_loop": true}, nil) {
		t.Error("显式 true 应返回 true")
	}
	if d.resolveEnableTaskLoop(map[string]any{"enable_task_loop": false}, nil) {
		t.Error("显式 false 应返回 false")
	}
	if !d.resolveEnableTaskLoop(map[string]any{}, nil) {
		t.Error("未设置时默认 true")
	}
}

// TestResolveEnableTaskPlanning 测试任务规划开关解析。
func TestResolveEnableTaskPlanning(t *testing.T) {
	d := NewDeepAdapter()
	if !d.resolveEnableTaskPlanning(map[string]any{"enable_task_planning": true}, nil) {
		t.Error("显式 true 应返回 true")
	}
	if d.resolveEnableTaskPlanning(map[string]any{"enable_task_planning": false}, nil) {
		t.Error("显式 false 应返回 false")
	}
	if !d.resolveEnableTaskPlanning(map[string]any{}, nil) {
		t.Error("未设置时默认 true")
	}
}

// TestResolvePromptMode 测试提示词模式解析。
func TestResolvePromptMode(t *testing.T) {
	d := NewDeepAdapter()
	tests := []struct {
		configBase map[string]any
		want       hschema.PromptMode
	}{
		{map[string]any{"prompt_mode": "minimal"}, hschema.PromptModeMinimal},
		{map[string]any{"prompt_mode": "none"}, hschema.PromptModeNone},
		{map[string]any{"prompt_mode": "full"}, hschema.PromptModeFull},
		{map[string]any{"prompt_mode": "unknown"}, hschema.PromptModeFull},
		{map[string]any{}, hschema.PromptModeFull},
		{nil, hschema.PromptModeFull},
	}
	for _, tt := range tests {
		if got := d.resolvePromptMode(tt.configBase); got != tt.want {
			t.Errorf("resolvePromptMode() = %v, want %v", got, tt.want)
		}
	}
}

// TestMakeDeepAgentConfig 测试构造 DeepAgentConfig。
func TestMakeDeepAgentConfig(t *testing.T) {
	d := NewDeepAdapter()
	d.configCache = map[string]any{"language": "en"}
	model := &llm.Model{}
	card := agentschema.NewAgentCard()

	cfg := d.makeDeepAgentConfig(model, map[string]any{"max_iterations": 20}, card, nil, nil)
	if cfg == nil {
		t.Fatal("makeDeepAgentConfig 返回 nil")
	}
	if cfg.Model != model {
		t.Error("Model 不匹配")
	}
	if cfg.MaxIterations != 20 {
		t.Errorf("MaxIterations = %d, want 20", cfg.MaxIterations)
	}
}

// TestGetCurrentAgentRails 测试获取当前 Agent Rails。
func TestGetCurrentAgentRails(t *testing.T) {
	d := NewDeepAdapter()
	d.mode = "agent.plan"
	config := map[string]any{}
	configBase := map[string]any{}
	result := d.getCurrentAgentRails(config, configBase)
	// 应返回非 nil 切片（至少包含 heartbeat + taskPlanning + filesystem + mcp rails）
	if result == nil {
		t.Error("getCurrentAgentRails 不应返回 nil")
	}
}

// TestExtractTextContent 测试文本内容提取。
func TestExtractTextContent(t *testing.T) {
	tests := []struct {
		name    string
		payload map[string]any
		want    string
	}{
		{"nil", nil, ""},
		{"map_content", map[string]any{"content": "hello"}, "hello"},
		{"map_text", map[string]any{"text": "world"}, "world"},
		{"map_content优先", map[string]any{"content": "hello", "text": "world"}, "hello"},
		{"map无匹配", map[string]any{"other": "val"}, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := extractTextContent(tt.payload); got != tt.want {
				t.Errorf("extractTextContent() = %q, want %q", got, tt.want)
			}
		})
	}
}

// TestExtractReasoningContent 测试推理内容提取。
func TestExtractReasoningContent(t *testing.T) {
	tests := []struct {
		name    string
		payload map[string]any
		want    string
	}{
		{"nil", nil, ""},
		{"map_content", map[string]any{"content": "reason"}, "reason"},
		{"map_reasoning", map[string]any{"reasoning": "think"}, "think"},
		{"map_content优先", map[string]any{"content": "reason", "reasoning": "think"}, "reason"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := extractReasoningContent(tt.payload); got != tt.want {
				t.Errorf("extractReasoningContent() = %q, want %q", got, tt.want)
			}
		})
	}
}

// TestParseStreamChunk_各种类型 测试流式 chunk 解析各类型。
func TestParseStreamChunk_各种类型(t *testing.T) {
	d := NewDeepAdapter()
	emittedIDs := make(map[string]bool)

	tests := []struct {
		name      string
		chunkType string
		payload   map[string]any
		wantNil   bool
		wantEvent string
	}{
		{"nil输出", "", nil, true, ""},
		{"controller_output_task_completion", "controller_output", map[string]any{"type": "task_completion"}, true, ""},
		{"controller_output_task_failed", "controller_output", map[string]any{"type": "task_failed", "error": "fail"}, false, "chat.error"},
		{"controller_output_default", "controller_output", map[string]any{"type": "other"}, false, "chat.delta"},
		{"content_chunk", "content_chunk", map[string]any{"content": "text"}, false, "chat.delta"},
		{"answer", "answer", map[string]any{"content": "final"}, false, "chat.final"},
		{"tool_call", "tool_call", map[string]any{"name": "tool1"}, false, "chat.tool_call"},
		{"tool_update", "tool_update", map[string]any{"status": "running"}, false, "chat.tool_update"},
		{"tool_result", "tool_result", map[string]any{"result": "ok"}, false, "chat.tool_result"},
		{"error", "error", map[string]any{"error": "oops"}, false, "chat.error"},
		{"thinking", "thinking", map[string]any{"content": "think"}, false, "chat.thinking"},
		{"todo_updated", "todo.updated", map[string]any{"todos": []any{}}, false, "todo.updated"},
		{"context_usage", "context.usage", map[string]any{"percent": 0.5}, false, "chat.context_usage"},
		{"context_compression_state", "context.compression_state", map[string]any{"state": "done"}, false, "chat.context_compression_state"},
		{"ask_user_question", "ask_user_question", map[string]any{"request_id": "ask1"}, false, "chat.ask_user_question"},
		{"interaction", "__interaction__", map[string]any{"type": "confirm"}, false, "chat.interaction"},
		{"message", "message", map[string]any{}, false, "chat.message"},
		{"stage_result", "stage_result", map[string]any{}, false, "chat.stage_result"},
		{"未知类型", "unknown_type", map[string]any{}, false, "chat.delta"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			output := &stream.OutputSchema{
				Type:    tt.chunkType,
				Payload: tt.payload,
			}
			if tt.payload == nil {
				output = nil
			}
			result := d.parseStreamChunk(output, &usageAccumulator{}, emittedIDs)
			if tt.wantNil {
				if result != nil {
					t.Errorf("parseStreamChunk() 应返回 nil，got %v", result)
				}
				return
			}
			if result == nil {
				t.Fatal("parseStreamChunk() 返回 nil")
			}
			if got, _ := result["event_type"].(string); got != tt.wantEvent {
				t.Errorf("event_type = %q, want %q", got, tt.wantEvent)
			}
		})
	}
}

// TestParseStreamChunk_askUser去重 测试 ask_user_question 去重。
func TestParseStreamChunk_askUser去重(t *testing.T) {
	d := NewDeepAdapter()
	emittedIDs := make(map[string]bool)
	usage := &usageAccumulator{}

	output1 := &stream.OutputSchema{
		Type:    "ask_user_question",
		Payload: map[string]any{"request_id": "ask1"},
	}
	// 第一次应返回
	result1 := d.parseStreamChunk(output1, usage, emittedIDs)
	if result1 == nil {
		t.Error("首次 ask_user_question 不应被去重")
	}

	// 第二次相同 request_id 应被去重
	output2 := &stream.OutputSchema{
		Type:    "ask_user_question",
		Payload: map[string]any{"request_id": "ask1"},
	}
	result2 := d.parseStreamChunk(output2, usage, emittedIDs)
	if result2 != nil {
		t.Error("重复 request_id 应被去重")
	}
}

// TestAccumulateUsage 测试 usage 累加。
func TestAccumulateUsage(t *testing.T) {
	d := NewDeepAdapter()
	usage := &usageAccumulator{}

	// nil payload
	d.accumulateUsage(usage, nil)
	if usage.TotalTokens != 0 {
		t.Error("nil payload 不应累加")
	}

	// 正常累加
	d.accumulateUsage(usage, map[string]any{
		"input_tokens":  100,
		"output_tokens": 50,
		"total_tokens":  150,
		"input_cost":    0.01,
		"output_cost":   0.02,
		"total_cost":    0.03,
	})
	if usage.InputTokens != 100 || usage.OutputTokens != 50 || usage.TotalTokens != 150 {
		t.Errorf("token 累加不正确: input=%d, output=%d, total=%d", usage.InputTokens, usage.OutputTokens, usage.TotalTokens)
	}
}

// TestAccumulateUsage_多次累加 测试多次累加。
func TestAccumulateUsage_多次累加(t *testing.T) {
	d := NewDeepAdapter()
	usage := &usageAccumulator{}

	d.accumulateUsage(usage, map[string]any{"input_tokens": 100, "output_tokens": 50, "total_tokens": 150})
	d.accumulateUsage(usage, map[string]any{"input_tokens": 200, "output_tokens": 100, "total_tokens": 300})

	if usage.InputTokens != 300 {
		t.Errorf("InputTokens = %d, want 300", usage.InputTokens)
	}
	if usage.OutputTokens != 150 {
		t.Errorf("OutputTokens = %d, want 150", usage.OutputTokens)
	}
}

// TestExtractStringFromPayload 测试字符串提取。
func TestExtractStringFromPayload(t *testing.T) {
	payload := map[string]any{"key": "value", "num": 123}
	if got := extractStringFromPayload(payload, "key"); got != "value" {
		t.Errorf("got %q, want %q", got, "value")
	}
	if got := extractStringFromPayload(payload, "num"); got != "" {
		t.Errorf("非字符串应返回空，got %q", got)
	}
	if got := extractStringFromPayload(payload, "missing"); got != "" {
		t.Errorf("不存在的键应返回空，got %q", got)
	}
}

// TestExtractIntFromPayload 测试整数提取。
func TestExtractIntFromPayload(t *testing.T) {
	payload := map[string]any{"float": float64(42), "int": 10, "str": "not_int"}
	if got := extractIntFromPayload(payload, "float"); got != 42 {
		t.Errorf("got %d, want 42", got)
	}
	if got := extractIntFromPayload(payload, "int"); got != 10 {
		t.Errorf("got %d, want 10", got)
	}
	if got := extractIntFromPayload(payload, "str"); got != 0 {
		t.Errorf("非数字应返回 0，got %d", got)
	}
	if got := extractIntFromPayload(payload, "missing"); got != 0 {
		t.Errorf("不存在的键应返回 0，got %d", got)
	}
}

// TestExtractFloatFromPayload 测试浮点数提取。
func TestExtractFloatFromPayload(t *testing.T) {
	payload := map[string]any{"float": 3.14, "int": 10, "str": "not_float"}
	if got := extractFloatFromPayload(payload, "float"); got != 3.14 {
		t.Errorf("got %f, want 3.14", got)
	}
	if got := extractFloatFromPayload(payload, "int"); got != 10.0 {
		t.Errorf("got %f, want 10.0", got)
	}
	if got := extractFloatFromPayload(payload, "str"); got != 0 {
		t.Errorf("非数字应返回 0，got %f", got)
	}
}

// TestDeepAdapter_HandleHeartbeat_注入query 测试 heartbeat 注入 query。
func TestDeepAdapter_HandleHeartbeat_注入query(t *testing.T) {
	d := NewDeepAdapter()
	ctx := t.Context()

	params, _ := json.Marshal(map[string]any{"mode": "agent.plan"})
	sid := "heartbeat_session_1"
	req := schema.NewAgentRequest("req-1", "ch-1", schema.ReqMethod("chat.send"), params,
		schema.WithAgentSessionID(sid),
	)
	resp, err := d.HandleHeartbeat(ctx, req)
	if err != nil {
		t.Errorf("HandleHeartbeat error: %v", err)
	}
	if resp != nil {
		t.Errorf("heartbeat 应返回 nil（继续正常流程），got %v", resp)
	}
	// 验证 params 中已注入 query
	var updatedParams map[string]any
	if err := json.Unmarshal(req.Params, &updatedParams); err != nil {
		t.Fatalf("反序列化 params 失败: %v", err)
	}
	if _, ok := updatedParams["query"]; !ok {
		t.Error("heartbeat 应注入 query 字段")
	}
}

// TestDeepAdapter_HandleHeartbeat_空Params 测试 heartbeat 空 Params 时注入 query。
func TestDeepAdapter_HandleHeartbeat_空Params(t *testing.T) {
	d := NewDeepAdapter()
	ctx := t.Context()

	sid := "heartbeat_session_2"
	req := schema.NewAgentRequest("req-1", "ch-1", schema.ReqMethod("chat.send"), nil,
		schema.WithAgentSessionID(sid),
	)
	resp, err := d.HandleHeartbeat(ctx, req)
	if err != nil {
		t.Errorf("HandleHeartbeat error: %v", err)
	}
	if resp != nil {
		t.Errorf("heartbeat 应返回 nil，got %v", resp)
	}
	// 空 Params 时应直接构造带 query 的 JSON
	if len(req.Params) == 0 {
		t.Error("heartbeat 应注入 query 到空 Params")
	}
}

// TestBuildVisionModelConfig 测试视觉模型配置构建。
func TestBuildVisionModelConfig(t *testing.T) {
	d := NewDeepAdapter()

	t.Run("无models段", func(t *testing.T) {
		if got := d.buildVisionModelConfig(nil); got != nil {
			t.Error("无 models 段应返回 nil")
		}
	})

	t.Run("无vision段", func(t *testing.T) {
		configBase := map[string]any{"models": map[string]any{}}
		if got := d.buildVisionModelConfig(configBase); got != nil {
			t.Error("无 vision 段应返回 nil")
		}
	})

	t.Run("完整配置", func(t *testing.T) {
		configBase := map[string]any{
			"models": map[string]any{
				"vision": map[string]any{
					"api_key":  "test_key",
					"base_url": "https://api.test.com",
					"model":    "vision-model",
				},
			},
		}
		got := d.buildVisionModelConfig(configBase)
		if got == nil {
			t.Fatal("应返回配置")
		}
		if got.APIKey != "test_key" {
			t.Errorf("APIKey = %q, want %q", got.APIKey, "test_key")
		}
		if got.Model != "vision-model" {
			t.Errorf("Model = %q, want %q", got.Model, "vision-model")
		}
	})

	t.Run("空apiKey和model", func(t *testing.T) {
		configBase := map[string]any{
			"models": map[string]any{
				"vision": map[string]any{},
			},
		}
		if got := d.buildVisionModelConfig(configBase); got != nil {
			t.Error("空 apiKey 和 model 应返回 nil")
		}
	})

	t.Run("maxRetries自定义", func(t *testing.T) {
		configBase := map[string]any{
			"models": map[string]any{
				"vision": map[string]any{
					"api_key":     "key",
					"model":       "model",
					"max_retries": float64(5),
				},
			},
		}
		got := d.buildVisionModelConfig(configBase)
		if got == nil {
			t.Fatal("应返回配置")
		}
		if got.MaxRetries != 5 {
			t.Errorf("MaxRetries = %d, want 5", got.MaxRetries)
		}
	})
}

// TestBuildAudioModelConfig 测试音频模型配置构建。
func TestBuildAudioModelConfig(t *testing.T) {
	d := NewDeepAdapter()

	t.Run("无models段", func(t *testing.T) {
		if got := d.buildAudioModelConfig(nil); got != nil {
			t.Error("无 models 段应返回 nil")
		}
	})

	t.Run("无audio段", func(t *testing.T) {
		configBase := map[string]any{"models": map[string]any{}}
		if got := d.buildAudioModelConfig(configBase); got != nil {
			t.Error("无 audio 段应返回 nil")
		}
	})

	t.Run("空apiKey", func(t *testing.T) {
		configBase := map[string]any{
			"models": map[string]any{
				"audio": map[string]any{
					"base_url": "https://api.test.com",
				},
			},
		}
		if got := d.buildAudioModelConfig(configBase); got != nil {
			t.Error("空 apiKey 应返回 nil")
		}
	})

	t.Run("完整配置", func(t *testing.T) {
		configBase := map[string]any{
			"models": map[string]any{
				"audio": map[string]any{
					"api_key":             "audio_key",
					"base_url":            "https://api.test.com",
					"transcription_model": "whisper",
					"qa_model":            "qwen",
					"max_retries":         float64(5),
					"http_timeout":        float64(60),
					"max_audio_bytes":     float64(50000000),
				},
			},
		}
		got := d.buildAudioModelConfig(configBase)
		if got == nil {
			t.Fatal("应返回配置")
		}
		if got.APIKey != "audio_key" {
			t.Errorf("APIKey = %q, want %q", got.APIKey, "audio_key")
		}
		if got.TranscriptionModel != "whisper" {
			t.Errorf("TranscriptionModel = %q, want %q", got.TranscriptionModel, "whisper")
		}
		if got.MaxRetries != 5 {
			t.Errorf("MaxRetries = %d, want 5", got.MaxRetries)
		}
		if got.HTTPTimeout != 60 {
			t.Errorf("HTTPTimeout = %d, want 60", got.HTTPTimeout)
		}
		if got.MaxAudioBytes != 50000000 {
			t.Errorf("MaxAudioBytes = %d, want 50000000", got.MaxAudioBytes)
		}
	})
}

// TestBuildVideoModelConfig 测试视频模型配置构建。
func TestBuildVideoModelConfig(t *testing.T) {
	d := NewDeepAdapter()

	t.Run("无环境变量", func(t *testing.T) {
		os.Unsetenv("VIDEO_API_KEY")
		if d.buildVideoModelConfig(nil) != false {
			t.Error("无 VIDEO_API_KEY 应返回 false")
		}
	})

	t.Run("有环境变量", func(t *testing.T) {
		os.Setenv("VIDEO_API_KEY", "test_key")
		defer os.Unsetenv("VIDEO_API_KEY")
		if d.buildVideoModelConfig(nil) != true {
			t.Error("有 VIDEO_API_KEY 应返回 true")
		}
	})
}

// TestBuildImageGenModelConfig 测试图片生成模型配置构建。
func TestBuildImageGenModelConfig(t *testing.T) {
	d := NewDeepAdapter()

	t.Run("无环境变量", func(t *testing.T) {
		os.Unsetenv("IMAGE_GEN_API_KEY")
		if d.buildImageGenModelConfig(nil) != false {
			t.Error("无 IMAGE_GEN_API_KEY 应返回 false")
		}
	})

	t.Run("有环境变量", func(t *testing.T) {
		os.Setenv("IMAGE_GEN_API_KEY", "test_key")
		defer os.Unsetenv("IMAGE_GEN_API_KEY")
		if d.buildImageGenModelConfig(nil) != true {
			t.Error("有 IMAGE_GEN_API_KEY 应返回 true")
		}
	})
}

// TestRefreshMultimodalConfigs 测试多模态配置刷新。
func TestRefreshMultimodalConfigs(t *testing.T) {
	d := NewDeepAdapter()
	os.Unsetenv("VIDEO_API_KEY")
	os.Unsetenv("IMAGE_GEN_API_KEY")

	configBase := map[string]any{
		"models": map[string]any{
			"vision": map[string]any{
				"api_key": "vision_key",
				"model":   "vision_model",
			},
		},
	}
	d.refreshMultimodalConfigs(configBase)
	if d.visionModelConfig == nil {
		t.Error("visionModelConfig 不应为 nil")
	}
}

// TestSyncMultimodalToolsForRuntime 测试多模态工具同步。
func TestSyncMultimodalToolsForRuntime(t *testing.T) {
	d := NewDeepAdapter()
	// 不应 panic
	d.syncMultimodalToolsForRuntime()

	// 有配置但未注册
	d.visionModelConfig = &hschema.VisionModelConfig{APIKey: "key"}
	d.audioModelConfig = &hschema.AudioModelConfig{APIKey: "key"}
	d.syncMultimodalToolsForRuntime()
}

// TestSyncPaidSearchToolForRuntime 测试付费搜索工具同步。
func TestSyncPaidSearchToolForRuntime(t *testing.T) {
	d := NewDeepAdapter()
	// 不应 panic
	d.syncPaidSearchToolForRuntime()
}

// TestPrioritizePaidSearchToolCard 测试付费搜索工具优先排序。
func TestPrioritizePaidSearchToolCard(t *testing.T) {
	d := NewDeepAdapter()
	var cards []*tool.ToolCard
	result := d.prioritizePaidSearchToolCard(cards)
	if len(result) != 0 {
		t.Errorf("prioritizePaidSearchToolCard 返回 %d 项，want 0", len(result))
	}
}

// TestPruneToolCards 测试工具卡片裁剪。
func TestPruneToolCards(t *testing.T) {
	d := NewDeepAdapter()
	var cards []*tool.ToolCard
	namesToRemove := map[string]bool{"test": true}
	result := d.pruneToolCards(cards, namesToRemove)
	if len(result) != 0 {
		t.Errorf("pruneToolCards 返回 %d 项，want 0", len(result))
	}
}

// TestAppendToolCard 测试工具卡片追加。
func TestAppendToolCard(t *testing.T) {
	d := NewDeepAdapter()
	d.appendToolCard(nil)
}

// TestRemoveRegisteredTools 测试工具移除。
func TestRemoveRegisteredTools(t *testing.T) {
	d := NewDeepAdapter()
	d.removeRegisteredTools([]string{"tool1", "tool2"})
}

// TestSyncToolGroup 测试工具组同步。
func TestSyncToolGroup(t *testing.T) {
	d := NewDeepAdapter()
	d.syncToolGroup("search", nil)
}

// TestExtractEnabledMcpServerEntries 测试启用 MCP 条目提取。
func TestExtractEnabledMcpServerEntries(t *testing.T) {
	d := NewDeepAdapter()

	t.Run("无mcp_servers段", func(t *testing.T) {
		result := d.extractEnabledMcpServerEntries(map[string]any{})
		if result != nil {
			t.Errorf("无 mcp_servers 段应返回 nil，got %v", result)
		}
	})

	t.Run("有启用的条目", func(t *testing.T) {
		configBase := map[string]any{
			"mcp_servers": map[string]any{
				"server1": map[string]any{
					"server_path": "/path/to/server1",
					"enabled":     true,
				},
				"server2": map[string]any{
					"server_path": "/path/to/server2",
					"enabled":     false,
				},
			},
		}
		result := d.extractEnabledMcpServerEntries(configBase)
		if len(result) != 1 {
			t.Errorf("应返回 1 个启用条目，got %d", len(result))
		}
	})

	t.Run("默认启用", func(t *testing.T) {
		configBase := map[string]any{
			"mcp_servers": map[string]any{
				"server1": map[string]any{
					"server_path": "/path/to/server1",
				},
			},
		}
		result := d.extractEnabledMcpServerEntries(configBase)
		if len(result) != 1 {
			t.Errorf("未设置 enabled 时默认启用，应返回 1 条目，got %d", len(result))
		}
	})

	t.Run("自动补充name字段", func(t *testing.T) {
		configBase := map[string]any{
			"mcp_servers": map[string]any{
				"my_server": map[string]any{
					"server_path": "/path",
				},
			},
		}
		result := d.extractEnabledMcpServerEntries(configBase)
		if len(result) != 1 {
			t.Fatalf("应返回 1 条目")
		}
		if result[0]["name"] != "my_server" {
			t.Errorf("name = %v, want my_server", result[0]["name"])
		}
	})
}

// TestBuildMcpServerConfig 测试 MCP 服务配置构建。
func TestBuildMcpServerConfig(t *testing.T) {
	d := NewDeepAdapter()

	t.Run("空name", func(t *testing.T) {
		entry := map[string]any{"server_path": "/path"}
		if got := d.buildMcpServerConfig(entry); got != nil {
			t.Error("空 name 应返回 nil")
		}
	})

	t.Run("空server_path", func(t *testing.T) {
		entry := map[string]any{"name": "server1"}
		if got := d.buildMcpServerConfig(entry); got != nil {
			t.Error("空 server_path 应返回 nil")
		}
	})

	t.Run("完整配置", func(t *testing.T) {
		entry := map[string]any{
			"name":        "server1",
			"server_path": "/path/to/server",
			"client_type": "stdio",
			"server_id":   "srv1",
			"params":      map[string]any{"key": "val"},
			"auth_headers": map[string]any{
				"Authorization": "Bearer token",
			},
		}
		got := d.buildMcpServerConfig(entry)
		if got == nil {
			t.Fatal("应返回配置")
		}
		if got.ServerName != "server1" {
			t.Errorf("ServerName = %q, want %q", got.ServerName, "server1")
		}
		if got.ServerPath != "/path/to/server" {
			t.Errorf("ServerPath = %q, want %q", got.ServerPath, "/path/to/server")
		}
	})
}

// TestDeepAdapter_ReloadAgentConfig_未初始化 测试 instance 为 nil 时返回错误。
func TestDeepAdapter_ReloadAgentConfig_未初始化(t *testing.T) {
	d := NewDeepAdapter()
	ctx := t.Context()
	err := d.ReloadAgentConfig(ctx, nil, nil)
	if err == nil {
		t.Error("instance 为 nil 时应返回错误")
	}
}

// TestUpdateRailsForMode 测试按模式切换 Rails。
func TestUpdateRailsForMode(t *testing.T) {
	d := NewDeepAdapter()
	d.updateRailsForMode("agent.plan")
	d.updateRailsForMode("agent.fast")
}

// TestUpdatePromptForMode 测试按模式更新提示词。
func TestUpdatePromptForMode(t *testing.T) {
	d := NewDeepAdapter()
	d.updatePromptForMode("agent.plan")
}

// TestDeepAdapter_A2x占位函数 测试 A2X 占位函数不 panic。
func TestDeepAdapter_A2x占位函数(t *testing.T) {
	d := NewDeepAdapter()
	ctx := t.Context()

	d.clearA2xRuntimeState()
	if err := d.closeA2xClient(); err != nil {
		t.Errorf("closeA2xClient error: %v", err)
	}
	if err := d.initA2xClient(ctx, nil); err != nil {
		t.Errorf("initA2xClient error: %v", err)
	}
	if err := d.tryInitA2xClient(ctx, nil); err != nil {
		t.Errorf("tryInitA2xClient error: %v", err)
	}
	if err := d.syncA2xRuntimeState(); err != nil {
		t.Errorf("syncA2xRuntimeState error: %v", err)
	}
	d.bindRuntimeCronContext("req1", "s1")
	d.resetRuntimeCronContext(nil)
}

// TestDeepAdapter_Evolution占位函数 测试 evolution 占位函数不 panic。
func TestDeepAdapter_Evolution占位函数(t *testing.T) {
	d := NewDeepAdapter()
	ctx := t.Context()

	if err := d.watchEvolutionAndPush(ctx, "s1", "req1"); err != nil {
		t.Errorf("watchEvolutionAndPush error: %v", err)
	}
	d.onEvolutionWatcherDone("s1")
	if d.handleEvolutionApproval("req1", nil) != false {
		t.Error("handleEvolutionApproval 占位应返回 false")
	}
	if msgs := d.getRecentMessages("s1"); msgs != nil {
		t.Errorf("getRecentMessages 占位应返回 nil")
	}
	if msg, err := d.callModelForRecap(ctx, nil, ""); msg != "" || err != nil {
		t.Errorf("callModelForRecap 占位应返回 '', nil")
	}
	if count, err := d.countFullContextTokens(ctx, "s1"); count != 0 || err != nil {
		t.Errorf("countFullContextTokens 占位应返回 0, nil")
	}
}

// TestDeepAdapter_Team占位函数 测试 team 占位函数不 panic。
func TestDeepAdapter_Team占位函数(t *testing.T) {
	d := NewDeepAdapter()
	ctx := t.Context()

	if d.findTeamSkillRail() != nil {
		t.Error("findTeamSkillRail 占位应返回 nil")
	}
	if d.handleTeamSkillEvolveApproval(ctx, "req1", nil, "s1", "ch1") != false {
		t.Error("handleTeamSkillEvolveApproval 占位应返回 false")
	}
	if err := d.pushTeamSkillEvolveResolutionStatus(ctx, "req1", "approved"); err != nil {
		t.Errorf("pushTeamSkillEvolveResolutionStatus error: %v", err)
	}
	if err := d.processTeamMessageStream(ctx, nil, nil); err != nil {
		t.Errorf("processTeamMessageStream error: %v", err)
	}
}

// TestDeepAdapter_Slash占位函数 测试 slash 占位函数不 panic。
func TestDeepAdapter_Slash占位函数(t *testing.T) {
	d := NewDeepAdapter()
	ctx := t.Context()

	if result, err := d.handleEvolveCommand(ctx, "/evolve", "s1"); result != nil || err != nil {
		t.Errorf("handleEvolveCommand 占位应返回 nil, nil")
	}
	if result, err := d.handleEvolveListCommand(ctx, "s1"); result != nil || err != nil {
		t.Errorf("handleEvolveListCommand 占位应返回 nil, nil")
	}
	if result, err := d.handleEvolveSimplifyCommand(ctx, "/evolve_simplify", "s1"); result != nil || err != nil {
		t.Errorf("handleEvolveSimplifyCommand 占位应返回 nil, nil")
	}
	if result, err := d.handleEvolveRebuildCommand(ctx, "/evolve_rebuild", "s1"); result != nil || err != nil {
		t.Errorf("handleEvolveRebuildCommand 占位应返回 nil, nil")
	}
	if result, err := d.handleEvolveRollbackCommand(ctx, "/evolve_rollback", "s1"); result != nil || err != nil {
		t.Errorf("handleEvolveRollbackCommand 占位应返回 nil, nil")
	}
	if d.handleGovernanceApproval("req1", nil, "simplify") != false {
		t.Error("handleGovernanceApproval 占位应返回 false")
	}
}

// TestDeepAdapter_HandleSlashCommand_evolve前缀 测试 /evolve 前缀分发。
func TestDeepAdapter_HandleSlashCommand_evolve前缀(t *testing.T) {
	d := NewDeepAdapter()
	ctx := t.Context()

	tests := []string{
		"/evolve",
		"/evolve_simplify",
		"/evolve_rebuild",
		"/evolve_rollback",
		"/evolve_list",
	}
	for _, query := range tests {
		result, err := d.handleSlashCommand(ctx, query, "s1", "agent.plan")
		if result != nil || err != nil {
			t.Errorf("handleSlashCommand(%q) 占位应返回 nil, nil", query)
		}
	}
}

// TestDeepAdapter_RegisterMcpServersFromConfig 测试 MCP 服务注册。
func TestDeepAdapter_RegisterMcpServersFromConfig(t *testing.T) {
	d := NewDeepAdapter()
	ctx := t.Context()

	// 无 mcp_servers 段
	if err := d.registerMcpServersFromConfig(ctx, map[string]any{}, "agent.test"); err != nil {
		t.Errorf("registerMcpServersFromConfig error: %v", err)
	}

	// 有空 mcp_servers 段
	configBase := map[string]any{
		"mcp_servers": map[string]any{},
	}
	if err := d.registerMcpServersFromConfig(ctx, configBase, "agent.test"); err != nil {
		t.Errorf("registerMcpServersFromConfig error: %v", err)
	}
}

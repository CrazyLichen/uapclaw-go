package utils

import (
	"testing"
	"time"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/stream"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// makeOutput 创建 OutputSchema 用于测试
func makeOutput(chunkType string, payload map[string]any) *stream.OutputSchema {
	return &stream.OutputSchema{
		Type:    chunkType,
		Payload: payload,
	}
}

func TestParseStreamChunk_nil(t *testing.T) {
	result := ParseStreamChunk(nil, nil, nil, nil)
	if result != nil {
		t.Errorf("nil input 应返回 nil, 实际 %v", result)
	}
}

func TestParseStreamChunk_controllerOutput_taskCompletion(t *testing.T) {
	output := makeOutput("controller_output", map[string]any{"type": "task_completion"})
	result := ParseStreamChunk(output, nil, nil, nil)
	if result != nil {
		t.Errorf("task_completion 应返回 nil, 实际 %v", result)
	}
}

func TestParseStreamChunk_controllerOutput_taskFailed(t *testing.T) {
	output := makeOutput("controller_output", map[string]any{
		"type": "task_failed",
		"data": []any{
			map[string]any{"text": "执行超时"},
			map[string]any{"code": 500},
		},
	})
	result := ParseStreamChunk(output, nil, nil, nil)
	if result["event_type"] != "chat.error" {
		t.Errorf("期望 chat.error, 实际 %v", result["event_type"])
	}
	if result["error"] != "执行超时" {
		t.Errorf("期望 '执行超时', 实际 %v", result["error"])
	}
}

func TestParseStreamChunk_controllerOutput_taskFailed_无data(t *testing.T) {
	output := makeOutput("controller_output", map[string]any{"type": "task_failed"})
	result := ParseStreamChunk(output, nil, nil, nil)
	if result["event_type"] != "chat.error" {
		t.Errorf("期望 chat.error, 实际 %v", result["event_type"])
	}
	if result["error"] != "任务执行失败" {
		t.Errorf("期望默认错误信息, 实际 %v", result["error"])
	}
}

func TestParseStreamChunk_controllerOutput_interaction(t *testing.T) {
	// M-05: 对齐 interface_deep._parse_stream_chunk，controller_output 不搜索 __interaction__
	// 只有 task_completion/task_failed 两种 inner type
	output := makeOutput("controller_output", map[string]any{
		"type":    "__interaction__",
		"payload": map[string]any{"request_id": "req-1"},
	})
	converter := func(payload any) map[string]any {
		return map[string]any{"event_type": "chat.ask_user_question", "data": payload}
	}
	result := ParseStreamChunk(output, nil, nil, converter)
	// controller_output 不搜索 __interaction__，type 不匹配 task_completion/task_failed → 走 default
	if result["event_type"] != "chat.delta" {
		t.Errorf("controller_output 不搜索 __interaction__，期望 chat.delta, 实际 %v", result["event_type"])
	}
}

func TestParseStreamChunk_controllerOutput_default(t *testing.T) {
	output := makeOutput("controller_output", map[string]any{"type": "other", "data": "hello"})
	result := ParseStreamChunk(output, nil, nil, nil)
	if result["event_type"] != "chat.delta" {
		t.Errorf("期望 chat.delta, 实际 %v", result["event_type"])
	}
}

func TestParseStreamChunk_contentChunk(t *testing.T) {
	output := makeOutput("content_chunk", map[string]any{"content": "hello"})
	result := ParseStreamChunk(output, nil, nil, nil)
	if result["event_type"] != "chat.delta" {
		t.Errorf("期望 chat.delta, 实际 %v", result["event_type"])
	}
	if result["content"] != "hello" {
		t.Errorf("期望 hello, 实际 %v", result["content"])
	}
}

func TestParseStreamChunk_contentChunk_空内容(t *testing.T) {
	output := makeOutput("content_chunk", map[string]any{"content": ""})
	result := ParseStreamChunk(output, nil, nil, nil)
	if result != nil {
		t.Errorf("空内容应返回 nil, 实际 %v", result)
	}
}

func TestParseStreamChunk_contentChunk_空白内容(t *testing.T) {
	output := makeOutput("content_chunk", map[string]any{"content": "   "})
	result := ParseStreamChunk(output, nil, nil, nil)
	if result != nil {
		t.Errorf("空白内容应返回 nil, 实际 %v", result)
	}
}

func TestParseStreamChunk_answer_默认(t *testing.T) {
	output := makeOutput("answer", map[string]any{"content": "最终答案"})
	result := ParseStreamChunk(output, nil, nil, nil)
	if result["event_type"] != "chat.final" {
		t.Errorf("期望 chat.final, 实际 %v", result["event_type"])
	}
}

func TestParseStreamChunk_answer_resultTypeError(t *testing.T) {
	output := makeOutput("answer", map[string]any{
		"result_type": "error",
		"output":      "未知错误",
	})
	result := ParseStreamChunk(output, nil, nil, nil)
	if result["event_type"] != "chat.error" {
		t.Errorf("期望 chat.error, 实际 %v", result["event_type"])
	}
	if result["error"] != "未知错误" {
		t.Errorf("期望 '未知错误', 实际 %v", result["error"])
	}
}

func TestParseStreamChunk_answer_chunked(t *testing.T) {
	output := makeOutput("answer", map[string]any{
		"output": map[string]any{"output": "部分内容", "chunked": true},
	})
	result := ParseStreamChunk(output, nil, nil, nil)
	if result["event_type"] != "chat.delta" {
		t.Errorf("is_chunked=true 期望 chat.delta, 实际 %v", result["event_type"])
	}
}

func TestParseStreamChunk_answer_hasStreamedContent(t *testing.T) {
	output := makeOutput("answer", map[string]any{
		"output": map[string]any{"output": "最终内容", "chunked": false},
	})
	result := ParseStreamChunk(output, nil, nil, nil, true)
	if result["event_type"] != "chat.final" {
		t.Errorf("hasStreamedContent=true 期望 chat.final, 实际 %v", result["event_type"])
	}
}

func TestParseStreamChunk_toolCall(t *testing.T) {
	output := makeOutput("tool_call", map[string]any{"name": "read_file"})
	result := ParseStreamChunk(output, nil, nil, nil)
	if result["event_type"] != "chat.tool_call" {
		t.Errorf("期望 chat.tool_call, 实际 %v", result["event_type"])
	}
}

func TestParseStreamChunk_toolUpdate(t *testing.T) {
	output := makeOutput("tool_update", map[string]any{
		"tool_update": map[string]any{"progress": "50%", "status": "running"},
	})
	result := ParseStreamChunk(output, nil, nil, nil)
	if result["event_type"] != "chat.tool_update" {
		t.Errorf("期望 chat.tool_update, 实际 %v", result["event_type"])
	}
	if result["progress"] != "50%" {
		t.Errorf("期望 progress='50%%', 实际 %v", result["progress"])
	}
	if result["status"] != "running" {
		t.Errorf("期望 status='running', 实际 %v", result["status"])
	}
}

func TestParseStreamChunk_toolResult(t *testing.T) {
	output := makeOutput("tool_result", map[string]any{
		"tool_result": map[string]any{
			"result":       "文件内容",
			"tool_name":    "read_file",
			"tool_call_id": "call-1",
			"success":      true,
		},
	})
	result := ParseStreamChunk(output, nil, nil, nil)
	if result["event_type"] != "chat.tool_result" {
		t.Errorf("期望 chat.tool_result, 实际 %v", result["event_type"])
	}
	if result["result"] != "文件内容" {
		t.Errorf("期望 result='文件内容', 实际 %v", result["result"])
	}
	if result["tool_name"] != "read_file" {
		t.Errorf("期望 tool_name='read_file', 实际 %v", result["tool_name"])
	}
	if result["tool_call_id"] != "call-1" {
		t.Errorf("期望 tool_call_id='call-1', 实际 %v", result["tool_call_id"])
	}
	if result["success"] != true {
		t.Errorf("期望 success=true, 实际 %v", result["success"])
	}
}

func TestParseStreamChunk_toolResult_toolName回退(t *testing.T) {
	output := makeOutput("tool_result", map[string]any{
		"tool_result": map[string]any{
			"result": "ok",
			"name":   "legacy_name",
		},
	})
	result := ParseStreamChunk(output, nil, nil, nil)
	if result["tool_name"] != "legacy_name" {
		t.Errorf("tool_name 应回退到 name, 实际 %v", result["tool_name"])
	}
}

func TestParseStreamChunk_toolResult_toolCallId回退(t *testing.T) {
	output := makeOutput("tool_result", map[string]any{
		"tool_result": map[string]any{
			"result":     "ok",
			"toolCallId": "tc-123",
		},
	})
	result := ParseStreamChunk(output, nil, nil, nil)
	if result["tool_call_id"] != "tc-123" {
		t.Errorf("tool_call_id 应回退到 toolCallId, 实际 %v", result["tool_call_id"])
	}
}

func TestParseStreamChunk_toolResult_rawOutput回退(t *testing.T) {
	output := makeOutput("tool_result", map[string]any{
		"tool_result": map[string]any{
			"result":    "ok",
			"rawOutput": "raw data",
		},
	})
	result := ParseStreamChunk(output, nil, nil, nil)
	if result["raw_output"] != "raw data" {
		t.Errorf("raw_output 应回退到 rawOutput, 实际 %v", result["raw_output"])
	}
}

func TestParseStreamChunk_error(t *testing.T) {
	output := makeOutput("error", map[string]any{"error": "something broke"})
	result := ParseStreamChunk(output, nil, nil, nil)
	if result["event_type"] != "chat.error" {
		t.Errorf("期望 chat.error, 实际 %v", result["event_type"])
	}
}

func TestParseStreamChunk_thinking(t *testing.T) {
	output := makeOutput("thinking", map[string]any{"content": "思考中"})
	result := ParseStreamChunk(output, nil, nil, nil)
	if result["event_type"] != "chat.processing_status" {
		t.Errorf("期望 chat.processing_status, 实际 %v", result["event_type"])
	}
	if result["is_processing"] != true {
		t.Errorf("期望 is_processing=true, 实际 %v", result["is_processing"])
	}
	if result["current_task"] != "thinking" {
		t.Errorf("期望 current_task=thinking, 实际 %v", result["current_task"])
	}
}

func TestParseStreamChunk_todoUpdated(t *testing.T) {
	output := makeOutput("todo.updated", map[string]any{
		"todos": []any{map[string]any{"id": "1", "text": "任务1"}},
	})
	result := ParseStreamChunk(output, nil, nil, nil)
	if result["event_type"] != "todo.updated" {
		t.Errorf("期望 todo.updated, 实际 %v", result["event_type"])
	}
	todos, ok := result["todos"].([]any)
	if !ok || len(todos) != 1 {
		t.Errorf("期望 todos 列表长度 1, 实际 %v", result["todos"])
	}
}

func TestParseStreamChunk_todoUpdated_无todos(t *testing.T) {
	output := makeOutput("todo.updated", map[string]any{})
	result := ParseStreamChunk(output, nil, nil, nil)
	if result["event_type"] != "todo.updated" {
		t.Errorf("期望 todo.updated, 实际 %v", result["event_type"])
	}
	todos, ok := result["todos"].([]any)
	if !ok || len(todos) != 0 {
		t.Errorf("无 todos 应返回空列表, 实际 %v", result["todos"])
	}
}

func TestParseStreamChunk_contextUsage(t *testing.T) {
	usage := &UsageAccumulator{}
	output := makeOutput("context.usage", map[string]any{
		"rate":        0.5,
		"context_max": 128000,
		"tokens_used": 64000,
	})
	result := ParseStreamChunk(output, usage, nil, nil)
	if result["event_type"] != "context.usage" {
		t.Errorf("期望 context.usage, 实际 %v", result["event_type"])
	}
	if result["rate"] != 0.5 {
		t.Errorf("期望 rate=0.5, 实际 %v", result["rate"])
	}
	if result["context_max"] != 128000 {
		t.Errorf("期望 context_max=128000, 实际 %v", result["context_max"])
	}
	if result["tokens_used"] != 64000 {
		t.Errorf("期望 tokens_used=64000, 实际 %v", result["tokens_used"])
	}
}

func TestParseStreamChunk_contextCompressionState(t *testing.T) {
	output := makeOutput("context.compression_state", map[string]any{
		"status":       "in_progress",
		"phase":        "compressing",
		"processor":    "test_processor",
		"summary":      "test summary",
		"operation_id": "op-123",
	})
	result := ParseStreamChunk(output, nil, nil, nil)
	if result["event_type"] != "context.compression_state" {
		t.Errorf("期望 context.compression_state, 实际 %v", result["event_type"])
	}
	if result["status"] != "in_progress" {
		t.Errorf("期望 status=in_progress, 实际 %v", result["status"])
	}
	if result["phase"] != "compressing" {
		t.Errorf("期望 phase=compressing, 实际 %v", result["phase"])
	}
	if result["processor"] != "test_processor" {
		t.Errorf("期望 processor=test_processor, 实际 %v", result["processor"])
	}
	if result["summary"] != "test summary" {
		t.Errorf("期望 summary=test summary, 实际 %v", result["summary"])
	}
	if result["operation_id"] != "op-123" {
		t.Errorf("期望 operation_id=op-123, 实际 %v", result["operation_id"])
	}
}

func TestParseStreamChunk_askUserQuestion_去重(t *testing.T) {
	emittedIDs := map[string]bool{}
	output := makeOutput("ask_user_question", map[string]any{"request_id": "req-1", "questions": []any{}})

	// 第一次应返回事件
	result1 := ParseStreamChunk(output, nil, emittedIDs, nil)
	if result1["event_type"] != "chat.ask_user_question" {
		t.Errorf("第一次应返回 ask_user_question, 实际 %v", result1["event_type"])
	}

	// 第二次相同 requestID 应返回 nil（去重）
	result2 := ParseStreamChunk(output, nil, emittedIDs, nil)
	if result2 != nil {
		t.Errorf("去重后应返回 nil, 实际 %v", result2)
	}
}

func TestParseStreamChunk_askUserQuestion_无RequestID不去重(t *testing.T) {
	emittedIDs := map[string]bool{}
	output := makeOutput("ask_user_question", map[string]any{"questions": []any{}})
	result := ParseStreamChunk(output, nil, emittedIDs, nil)
	if result["event_type"] != "chat.ask_user_question" {
		t.Errorf("无 requestID 不去重, 期望 ask_user_question, 实际 %v", result["event_type"])
	}
}

func TestParseStreamChunk_interaction_有converter(t *testing.T) {
	converter := func(payload any) map[string]any {
		return map[string]any{
			"event_type": "chat.ask_user_question",
			"payload":    payload,
		}
	}
	// 非 activate_confirm 类型 → 走 converter
	output := makeOutput("__interaction__", map[string]any{"type": "ask_user"})
	result := ParseStreamChunk(output, nil, nil, converter)
	if result["event_type"] != "chat.ask_user_question" {
		t.Errorf("有 converter 且非 activate_confirm 时应委托转换, 实际 %v", result["event_type"])
	}
}

func TestParseStreamChunk_interaction_activateConfirm(t *testing.T) {
	// S-03: activate_confirm 先于 converter 检查
	converter := func(payload any) map[string]any {
		return map[string]any{"event_type": "chat.ask_user_question"}
	}
	output := makeOutput("__interaction__", map[string]any{
		"interaction_type": "activate_confirm",
		"interaction_id":   "int-123",
		"extension_name":  "test_ext",
		"runtime_path":    "/tmp/runtime",
	})
	result := ParseStreamChunk(output, nil, nil, converter)
	if result["event_type"] != "harness.activate_interaction" {
		t.Errorf("activate_confirm 应返回 harness.activate_interaction, 实际 %v", result["event_type"])
	}
	if result["interaction_type"] != "activate_confirm" {
		t.Errorf("期望 interaction_type=activate_confirm, 实际 %v", result["interaction_type"])
	}
	if result["interaction_id"] != "int-123" {
		t.Errorf("期望 interaction_id=int-123, 实际 %v", result["interaction_id"])
	}
}

func TestParseStreamChunk_interaction_无converter(t *testing.T) {
	output := makeOutput("__interaction__", map[string]any{"type": "ask_user"})
	result := ParseStreamChunk(output, nil, nil, nil)
	if result["event_type"] != "chat.interaction" {
		t.Errorf("无 converter 时应回退为 chat.interaction, 实际 %v", result["event_type"])
	}
}

func TestParseStreamChunk_specialTypes(t *testing.T) {
	// S-04: 五种 harness 类型各有独立的 event_type 和结构
	tests := []struct {
		chunkType  string
		payload    map[string]any
		wantEvent  string
	}{
		{"message", map[string]any{"content": "hello", "stage": "test"}, "harness.message"},
		{"stage_result", map[string]any{"stage": "build", "status": "success"}, "harness.stage_result"},
		{"extension_ready", map[string]any{"extension_name": "ext1"}, "harness.extension_ready"},
		{"harness_session_finished", map[string]any{"pipeline": "p1", "status": "success"}, "harness.session_finished"},
	}
	for _, tt := range tests {
		output := makeOutput(tt.chunkType, tt.payload)
		result := ParseStreamChunk(output, nil, nil, nil)
		if result["event_type"] != tt.wantEvent {
			t.Errorf("类型 %s 期望 %s, 实际 %v", tt.chunkType, tt.wantEvent, result["event_type"])
		}
	}

	// activate_testing_guide 特殊：返回 chat.delta
	output := makeOutput("activate_testing_guide", map[string]any{"text": "guide text"})
	result := ParseStreamChunk(output, nil, nil, nil)
	if result["event_type"] != "chat.delta" {
		t.Errorf("activate_testing_guide 期望 chat.delta, 实际 %v", result["event_type"])
	}
}

func TestParseStreamChunk_defaultFallback(t *testing.T) {
	// Python: default 分支对 payload dict 提取 content/output 字段
	output := makeOutput("custom_event", map[string]any{"content": "value"})
	result := ParseStreamChunk(output, nil, nil, nil)
	if result["event_type"] != "chat.delta" {
		t.Errorf("默认类型期望 chat.delta, 实际 %v", result["event_type"])
	}
	if result["content"] != "value" {
		t.Errorf("期望 content=value, 实际 %v", result["content"])
	}
}

func TestParseStreamChunk_defaultFallback_team(t *testing.T) {
	output := makeOutput("team_event", map[string]any{
		"event_type": "team.runtime_ready",
		"status":     "ok",
	})
	result := ParseStreamChunk(output, nil, nil, nil)
	if result["event_type"] != "team.runtime_ready" {
		t.Errorf("team 命名空间应直传, 实际 %v", result["event_type"])
	}
}

func TestParseStreamChunk_defaultFallback_空payload(t *testing.T) {
	output := makeOutput("custom_empty", map[string]any{})
	result := ParseStreamChunk(output, nil, nil, nil)
	if result["event_type"] != "chat.custom_empty" {
		t.Errorf("空 payload 期望 chat.{chunkType}, 实际 %v", result["event_type"])
	}
}

func TestAccumulateUsage(t *testing.T) {
	usage := &UsageAccumulator{}
	AccumulateUsage(usage, map[string]any{
		"input_tokens":  100,
		"output_tokens": 50,
		"total_tokens":  150,
		"input_cost":    0.01,
		"output_cost":   0.02,
		"total_cost":    0.03,
	})
	if usage.InputTokens != 100 {
		t.Errorf("InputTokens 期望 100, 实际 %d", usage.InputTokens)
	}
	if usage.OutputTokens != 50 {
		t.Errorf("OutputTokens 期望 50, 实际 %d", usage.OutputTokens)
	}
	if usage.InputCost != 0.01 {
		t.Errorf("InputCost 期望 0.01, 实际 %f", usage.InputCost)
	}
}

func TestAccumulateUsage_累加(t *testing.T) {
	usage := &UsageAccumulator{}
	AccumulateUsage(usage, map[string]any{"input_tokens": 100})
	AccumulateUsage(usage, map[string]any{"input_tokens": 50})
	if usage.InputTokens != 150 {
		t.Errorf("累加 InputTokens 期望 150, 实际 %d", usage.InputTokens)
	}
}

func TestAccumulateUsage_nil(t *testing.T) {
	AccumulateUsage(nil, map[string]any{"input_tokens": 100}) // 不 panic
	AccumulateUsage(&UsageAccumulator{}, nil)                 // 不 panic
}

func TestExtractStringFromPayload(t *testing.T) {
	payload := map[string]any{"key": "value"}
	if ExtractStringFromPayload(payload, "key") != "value" {
		t.Errorf("期望 value")
	}
	if ExtractStringFromPayload(payload, "missing") != "" {
		t.Errorf("缺失键期望空")
	}
}

func TestExtractIntFromPayload(t *testing.T) {
	payload := map[string]any{"count": float64(42)}
	if ExtractIntFromPayload(payload, "count") != 42 {
		t.Errorf("期望 42")
	}
}

func TestExtractFloatFromPayload(t *testing.T) {
	payload := map[string]any{"cost": float64(0.5)}
	if ExtractFloatFromPayload(payload, "cost") != 0.5 {
		t.Errorf("期望 0.5")
	}
}

func TestParseInteractionPayload_有converter(t *testing.T) {
	converter := func(payload any) map[string]any {
		return map[string]any{"event_type": "custom.event", "data": payload}
	}
	result := ParseInteractionPayload(map[string]any{"type": "test"}, converter)
	if result["event_type"] != "custom.event" {
		t.Errorf("期望 custom.event, 实际 %v", result["event_type"])
	}
}

func TestParseInteractionPayload_无converter(t *testing.T) {
	payload := map[string]any{"type": "ask_user", "questions": []any{}}
	result := ParseInteractionPayload(payload, nil)
	if result["event_type"] != "chat.interaction" {
		t.Errorf("期望 chat.interaction, 实际 %v", result["event_type"])
	}
}

func TestSerializeValue_time(t *testing.T) {
	now := time.Now()
	result := SerializeValue(now)
	s, ok := result.(string)
	if !ok {
		t.Fatalf("期望 string, 实际 %T", result)
	}
	_, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		t.Errorf("期望 RFC3339Nano 格式, 解析失败: %v", err)
	}
}

func TestSerializeValue_普通值(t *testing.T) {
	if SerializeValue("hello") != "hello" {
		t.Error("字符串应直传")
	}
	if SerializeValue(42) != 42 {
		t.Error("整数应直传")
	}
	if SerializeValue(nil) != nil {
		t.Error("nil 应直传")
	}
}

func TestFindInteractionPayloads_type为Interaction(t *testing.T) {
	obj := map[string]any{
		"type":    "__interaction__",
		"payload": map[string]any{"request_id": "req-1"},
	}
	result := FindInteractionPayloads(obj)
	if len(result) != 1 {
		t.Fatalf("期望 1 个结果, 实际 %d", len(result))
	}
	m, ok := result[0].(map[string]any)
	if !ok {
		t.Fatalf("期望 map, 实际 %T", result[0])
	}
	if m["request_id"] != "req-1" {
		t.Errorf("期望 req-1, 实际 %v", m["request_id"])
	}
}

func TestFindInteractionPayloads_askUserQuestion(t *testing.T) {
	obj := map[string]any{
		"event_type": "chat.ask_user_question",
		"request_id": "req-2",
		"questions":  []any{map[string]any{"question": "是否继续？"}},
	}
	result := FindInteractionPayloads(obj)
	if len(result) != 1 {
		t.Fatalf("期望 1 个结果, 实际 %d", len(result))
	}
	m, ok := result[0].(map[string]any)
	if !ok {
		t.Fatalf("期望 map, 实际 %T", result[0])
	}
	if m["id"] != "req-2" {
		t.Errorf("期望 req-2, 实际 %v", m["id"])
	}
}

func TestFindInteractionPayloads_嵌套(t *testing.T) {
	obj := map[string]any{
		"controller_output": map[string]any{
			"type":    "__interaction__",
			"payload": map[string]any{"data": "nested"},
		},
	}
	result := FindInteractionPayloads(obj)
	if len(result) != 1 {
		t.Fatalf("期望 1 个结果, 实际 %d", len(result))
	}
}

func TestFindInteractionPayloads_列表(t *testing.T) {
	obj := []any{
		map[string]any{"type": "__interaction__", "payload": "first"},
		map[string]any{"type": "__interaction__", "payload": "second"},
	}
	result := FindInteractionPayloads(obj)
	if len(result) != 2 {
		t.Fatalf("期望 2 个结果, 实际 %d", len(result))
	}
}

func TestFindInteractionPayloads_深度限制(t *testing.T) {
	// 构造 10 层嵌套
	inner := map[string]any{"type": "__interaction__", "payload": "deep"}
	for i := 0; i < 10; i++ {
		inner = map[string]any{"nested": inner}
	}
	result := FindInteractionPayloads(inner)
	if len(result) != 0 {
		t.Errorf("超过深度限制应返回空, 实际 %d", len(result))
	}
}

func TestFindInteractionPayloads_循环检测(t *testing.T) {
	// 用共享子 map 模拟循环引用
	shared := map[string]any{"type": "normal"}
	obj := map[string]any{
		"a": shared,
		"b": shared,
	}
	// 不应死循环
	result := FindInteractionPayloads(obj)
	_ = result
}

func TestFindInteractionPayloads_nil(t *testing.T) {
	result := FindInteractionPayloads(nil)
	if len(result) != 0 {
		t.Errorf("nil 应返回空, 实际 %d", len(result))
	}
}

func TestFindInteractionPayload_有匹配(t *testing.T) {
	obj := map[string]any{"type": "__interaction__", "payload": "found"}
	result := FindInteractionPayload(obj)
	if result != "found" {
		t.Errorf("期望 found, 实际 %v", result)
	}
}

func TestFindInteractionPayload_无匹配(t *testing.T) {
	result := FindInteractionPayload(map[string]any{"type": "other"})
	if result != nil {
		t.Errorf("无匹配应返回 nil, 实际 %v", result)
	}
}

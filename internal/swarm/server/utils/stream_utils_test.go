//go:build test

package utils

import (
	"testing"

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
	output := makeOutput("controller_output", map[string]any{"type": "task_failed", "error": "出错了"})
	result := ParseStreamChunk(output, nil, nil, nil)
	if result["event_type"] != "chat.error" {
		t.Errorf("期望 chat.error, 实际 %v", result["event_type"])
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

func TestParseStreamChunk_answer(t *testing.T) {
	output := makeOutput("answer", map[string]any{"content": "最终答案"})
	result := ParseStreamChunk(output, nil, nil, nil)
	if result["event_type"] != "chat.final" {
		t.Errorf("期望 chat.final, 实际 %v", result["event_type"])
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
	output := makeOutput("tool_update", map[string]any{"progress": "50%"})
	result := ParseStreamChunk(output, nil, nil, nil)
	if result["event_type"] != "chat.tool_update" {
		t.Errorf("期望 chat.tool_update, 实际 %v", result["event_type"])
	}
}

func TestParseStreamChunk_toolResult(t *testing.T) {
	output := makeOutput("tool_result", map[string]any{"output": "result"})
	result := ParseStreamChunk(output, nil, nil, nil)
	if result["event_type"] != "chat.tool_result" {
		t.Errorf("期望 chat.tool_result, 实际 %v", result["event_type"])
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
	if result["event_type"] != "chat.thinking" {
		t.Errorf("期望 chat.thinking, 实际 %v", result["event_type"])
	}
}

func TestParseStreamChunk_todoUpdated(t *testing.T) {
	output := makeOutput("todo.updated", map[string]any{"todo": "item"})
	result := ParseStreamChunk(output, nil, nil, nil)
	if result["event_type"] != "todo.updated" {
		t.Errorf("期望 todo.updated, 实际 %v", result["event_type"])
	}
}

func TestParseStreamChunk_contextUsage(t *testing.T) {
	usage := &UsageAccumulator{}
	output := makeOutput("context.usage", map[string]any{"input_tokens": 100, "output_tokens": 50})
	result := ParseStreamChunk(output, usage, nil, nil)
	if result["event_type"] != "chat.context_usage" {
		t.Errorf("期望 chat.context_usage, 实际 %v", result["event_type"])
	}
}

func TestParseStreamChunk_contextCompressionState(t *testing.T) {
	output := makeOutput("context.compression_state", map[string]any{"state": "compressed"})
	result := ParseStreamChunk(output, nil, nil, nil)
	if result["event_type"] != "chat.context_compression_state" {
		t.Errorf("期望 chat.context_compression_state, 实际 %v", result["event_type"])
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
			"event_type": "harness.activate_interaction",
			"payload":    payload,
		}
	}
	output := makeOutput("__interaction__", map[string]any{"type": "activate_confirm"})
	result := ParseStreamChunk(output, nil, nil, converter)
	if result["event_type"] != "harness.activate_interaction" {
		t.Errorf("有 converter 时应委托转换, 实际 %v", result["event_type"])
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
	specialTypes := []string{"message", "stage_result", "extension_ready", "harness_session_finished", "activate_testing_guide"}
	for _, st := range specialTypes {
		output := makeOutput(st, map[string]any{"data": "test"})
		result := ParseStreamChunk(output, nil, nil, nil)
		expected := "chat." + st
		if result["event_type"] != expected {
			t.Errorf("类型 %s 期望 %s, 实际 %v", st, expected, result["event_type"])
		}
	}
}

func TestParseStreamChunk_defaultFallback(t *testing.T) {
	output := makeOutput("unknown_type", map[string]any{"data": "test"})
	result := ParseStreamChunk(output, nil, nil, nil)
	if result["event_type"] != "chat.delta" {
		t.Errorf("未知类型期望 chat.delta, 实际 %v", result["event_type"])
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
		"total_cost":     0.03,
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
	AccumulateUsage(&UsageAccumulator{}, nil)                  // 不 panic
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

func TestParseDictChunk(t *testing.T) {
	chunk := map[string]any{"event_type": "test", "data": "hello"}
	result := ParseDictChunk(chunk)
	if result["data"] != "hello" {
		t.Errorf("期望保留原始数据")
	}
}

func TestParseDictChunk_nil(t *testing.T) {
	result := ParseDictChunk(nil)
	if result != nil {
		t.Errorf("nil 应返回 nil")
	}
}

func TestParseTypedChunk_nil(t *testing.T) {
	result := ParseTypedChunk(nil)
	if result != nil {
		t.Errorf("nil 应返回 nil")
	}
}

func TestParseEventTypedChunk_nil(t *testing.T) {
	result := ParseEventTypedChunk(nil)
	if result != nil {
		t.Errorf("nil 应返回 nil")
	}
}

func TestParseResponseChunk_nil(t *testing.T) {
	result := ParseResponseChunk(nil)
	if result != nil {
		t.Errorf("nil 应返回 nil")
	}
}

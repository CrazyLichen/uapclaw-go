package processor

import (
	"testing"

	llm_schema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// TestGroupCompletedAPIRounds_空消息 验证空消息列表
func TestGroupCompletedAPIRounds_空消息(t *testing.T) {
	result := GroupCompletedAPIRounds(nil)
	if len(result) != 0 {
		t.Errorf("空消息应返回空切片，实际 %d 项", len(result))
	}
}

// TestGroupCompletedAPIRounds_纯对话 验证不含工具调用的对话轮次
func TestGroupCompletedAPIRounds_纯对话(t *testing.T) {
	messages := []llm_schema.BaseMessage{
		llm_schema.NewUserMessage("你好"),
		llm_schema.NewAssistantMessage("你好！"),
	}
	result := GroupCompletedAPIRounds(messages)
	if len(result) != 1 {
		t.Fatalf("轮次数 = %d, want 1", len(result))
	}
	if result[0] != [2]int{0, 2} {
		t.Errorf("轮次区间 = %v, want [0, 2)", result[0])
	}
}

// TestGroupCompletedAPIRounds_多轮纯对话 验证多轮不含工具调用的对话
func TestGroupCompletedAPIRounds_多轮纯对话(t *testing.T) {
	messages := []llm_schema.BaseMessage{
		llm_schema.NewUserMessage("你好"),
		llm_schema.NewAssistantMessage("你好！"),
		llm_schema.NewUserMessage("天气怎样"),
		llm_schema.NewAssistantMessage("晴天"),
	}
	result := GroupCompletedAPIRounds(messages)
	if len(result) != 2 {
		t.Fatalf("轮次数 = %d, want 2", len(result))
	}
	if result[0] != [2]int{0, 2} {
		t.Errorf("轮次1 = %v, want [0, 2)", result[0])
	}
	if result[1] != [2]int{2, 4} {
		t.Errorf("轮次2 = %v, want [2, 4)", result[1])
	}
}

// TestGroupCompletedAPIRounds_含工具调用 验证含工具调用的轮次
func TestGroupCompletedAPIRounds_含工具调用(t *testing.T) {
	messages := []llm_schema.BaseMessage{
		llm_schema.NewUserMessage("查询天气"),
		llm_schema.NewAssistantMessage("我来查一下",
			llm_schema.WithToolCalls([]*llm_schema.ToolCall{
				{ID: "call_1", Name: "get_weather", Arguments: `{"city":"北京"}`},
			}),
		),
		llm_schema.NewToolMessage("call_1", "北京：晴天 25°C"),
	}
	result := GroupCompletedAPIRounds(messages)
	if len(result) != 1 {
		t.Fatalf("轮次数 = %d, want 1", len(result))
	}
	if result[0] != [2]int{0, 3} {
		t.Errorf("轮次区间 = %v, want [0, 3)", result[0])
	}
}

// TestGroupCompletedAPIRounds_多轮含工具 验证多轮含工具调用
func TestGroupCompletedAPIRounds_多轮含工具(t *testing.T) {
	messages := []llm_schema.BaseMessage{
		llm_schema.NewUserMessage("查询天气"),
		llm_schema.NewAssistantMessage("我来查",
			llm_schema.WithToolCalls([]*llm_schema.ToolCall{
				{ID: "call_1", Name: "get_weather", Arguments: `{"city":"北京"}`},
			}),
		),
		llm_schema.NewToolMessage("call_1", "晴天"),
		llm_schema.NewAssistantMessage("北京今天晴天"),
		llm_schema.NewUserMessage("上海呢"),
		llm_schema.NewAssistantMessage("我也查一下",
			llm_schema.WithToolCalls([]*llm_schema.ToolCall{
				{ID: "call_2", Name: "get_weather", Arguments: `{"city":"上海"}`},
			}),
		),
		llm_schema.NewToolMessage("call_2", "多云"),
		llm_schema.NewAssistantMessage("上海多云"),
	}
	result := GroupCompletedAPIRounds(messages)
	if len(result) != 4 {
		t.Fatalf("轮次数 = %d, want 4", len(result))
	}
	// 第1轮: [0, 3) — User → Assistant(tool_calls) → Tool
	if result[0] != [2]int{0, 3} {
		t.Errorf("轮次1 = %v, want [0, 3)", result[0])
	}
	// 第2轮: [3, 4) — Assistant(无tool_calls，紧接 Tool 后)
	if result[1] != [2]int{3, 4} {
		t.Errorf("轮次2 = %v, want [3, 4)", result[1])
	}
	// 第3轮: [4, 7) — User → Assistant(tool_calls) → Tool
	if result[2] != [2]int{4, 7} {
		t.Errorf("轮次3 = %v, want [4, 7)", result[2])
	}
	// 第4轮: [7, 8) — Assistant(无tool_calls，紧接 Tool 后)
	if result[3] != [2]int{7, 8} {
		t.Errorf("轮次4 = %v, want [7, 8)", result[3])
	}
}

// TestGroupCompletedAPIRounds_未完成轮次 验证未完成的轮次不计入结果
func TestGroupCompletedAPIRounds_未完成轮次(t *testing.T) {
	messages := []llm_schema.BaseMessage{
		llm_schema.NewUserMessage("查询天气"),
		llm_schema.NewAssistantMessage("我来查",
			llm_schema.WithToolCalls([]*llm_schema.ToolCall{
				{ID: "call_1", Name: "get_weather", Arguments: `{"city":"北京"}`},
			}),
		),
		// 缺少 ToolMessage 回复 → 轮次未完成
	}
	result := GroupCompletedAPIRounds(messages)
	if len(result) != 0 {
		t.Errorf("未完成轮次应返回 0 项，实际 %d 项", len(result))
	}
}

// TestGroupCompletedAPIRounds_多个并行工具调用 验证同一轮次中的多个工具调用
func TestGroupCompletedAPIRounds_多个并行工具调用(t *testing.T) {
	messages := []llm_schema.BaseMessage{
		llm_schema.NewUserMessage("查询北京和上海天气"),
		llm_schema.NewAssistantMessage("查询中",
			llm_schema.WithToolCalls([]*llm_schema.ToolCall{
				{ID: "call_1", Name: "get_weather", Arguments: `{"city":"北京"}`},
				{ID: "call_2", Name: "get_weather", Arguments: `{"city":"上海"}`},
			}),
		),
		llm_schema.NewToolMessage("call_1", "北京：晴天"),
		llm_schema.NewToolMessage("call_2", "上海：多云"),
	}
	result := GroupCompletedAPIRounds(messages)
	if len(result) != 1 {
		t.Fatalf("轮次数 = %d, want 1", len(result))
	}
	if result[0] != [2]int{0, 4} {
		t.Errorf("轮次区间 = %v, want [0, 4)", result[0])
	}
}

// TestGroupCompletedAPIRounds_部分完成 验证部分完成的情况
func TestGroupCompletedAPIRounds_部分完成(t *testing.T) {
	messages := []llm_schema.BaseMessage{
		llm_schema.NewUserMessage("你好"),
		llm_schema.NewAssistantMessage("你好！"),
		llm_schema.NewUserMessage("查询天气"),
		llm_schema.NewAssistantMessage("查询中",
			llm_schema.WithToolCalls([]*llm_schema.ToolCall{
				{ID: "call_1", Name: "get_weather", Arguments: `{"city":"北京"}`},
			}),
		),
		// 缺少 ToolMessage → 第二轮未完成
	}
	result := GroupCompletedAPIRounds(messages)
	if len(result) != 1 {
		t.Fatalf("轮次数 = %d, want 1", len(result))
	}
	if result[0] != [2]int{0, 2} {
		t.Errorf("轮次区间 = %v, want [0, 2)", result[0])
	}
}

// TestIsAPIRound 验证 IsAPIRound 方法
func TestIsAPIRound(t *testing.T) {
	c := &testConfig{Name: "test"}
	p := NewBaseProcessor(c)

	// 完整轮次
	complete := []llm_schema.BaseMessage{
		llm_schema.NewUserMessage("你好"),
		llm_schema.NewAssistantMessage("你好！"),
	}
	if !p.IsAPIRound(complete) {
		t.Error("完整轮次应返回 true")
	}

	// 不完整轮次
	incomplete := []llm_schema.BaseMessage{
		llm_schema.NewUserMessage("你好"),
		llm_schema.NewAssistantMessage("查询中",
			llm_schema.WithToolCalls([]*llm_schema.ToolCall{
				{ID: "call_1", Name: "get_weather", Arguments: `{}`},
			}),
		),
	}
	if p.IsAPIRound(incomplete) {
		t.Error("未完成轮次应返回 false")
	}

	// 空消息
	if p.IsAPIRound(nil) {
		t.Error("空消息应返回 false")
	}
}

package trajectory

import (
	"fmt"

	obs "github.com/uapclaw/uapclaw-go/internal/agent_teams/observability"
)

// testfixture.go 模型迁移期的测试夹具构造支持（25.2b）。
// 【12-04 注记】仅供 *_test.go 使用（Python 无对应实现）：把旧 step 模型的
// 测试夹具语义（LLM 步骤带 messages/response、工具步骤带 args/result）转换
// 为 span 模型规范载荷。生产代码不得引用。

// ──────────────────────────── 结构体 ────────────────────────────

// TestStep 旧 step 模型语义的一条测试步骤。
type TestStep struct {
	// Kind "llm" 或 "tool"。
	Kind string
	// Messages LLM 步骤：扁平 prompt 消息。
	Messages []map[string]any
	// Response LLM 步骤：响应（作为 completion 的 content 来源）。
	Response map[string]any
	// ToolName 工具步骤：工具名。
	ToolName string
	// CallArgs 工具步骤：参数（编码为 JSON 字符串属性）。
	CallArgs any
	// CallResult 工具步骤：结果（编码为 JSON 字符串属性）。
	CallResult any
	// ToolCallID 工具步骤：调用 ID（可选）。
	ToolCallID string
}

// ──────────────────────────── 导出函数 ────────────────────────────

// NewTestTrajectory 按 span 模型构造测试轨迹。
// meta 的 member_id/source 键映射到资源属性（对齐 offline builder 别名表）。
func NewTestTrajectory(sessionID string, meta map[string]any, steps []TestStep) (*Trajectory, error) {
	if sessionID == "" {
		sessionID = "test-session"
	}
	builderMeta := map[string]any{}
	if source, exists := meta["source"]; exists {
		builderMeta["source"] = source
	} else {
		builderMeta["source"] = "online"
	}
	if memberID, exists := meta["member_id"]; exists {
		builderMeta["member_id"] = memberID
	}
	if teamID, exists := meta["team_id"]; exists {
		builderMeta["team_id"] = teamID
	}
	builder, err := NewTrajectoryBuilder(sessionID, fmt.Sprint(builderMeta["source"]), nil, nil, nil, builderMeta, 0, nil, nil)
	if err != nil {
		return nil, err
	}
	for index, step := range steps {
		span := testSpanOf(step, index)
		if span != nil {
			// 按序注入时间戳：真实 span 有时钟序，投影按时间排序消费。
			start := fmt.Sprint(int64(index) * 1_000_000)
			span["startTimeUnixNano"] = start
			span["endTimeUnixNano"] = start
			if err := builder.RecordSpan(span); err != nil {
				return nil, err
			}
		}
	}
	return builder.Build()
}

// testSpanOf 一条测试步骤 → 规范 span。
func testSpanOf(step TestStep, index int) map[string]any {
	switch step.Kind {
	case "llm":
		attrs := map[string]any{"gen_ai.operation.name": "chat", "gen_ai.request.model": "test-model"}
		// Response 为 nil 时不合成 completion（对齐真实投影：无响应即无输出消息）。
		var completions []map[string]any
		if step.Response != nil {
			completions = []map[string]any{{"role": "assistant", "content": fmt.Sprint(step.Response)}}
		}
		for key, value := range WriteLLMExchange(step.Messages, completions) {
			attrs[key] = value
		}
		return map[string]any{"name": "llm.call", "attributes": attrs}
	case "tool":
		attrs := map[string]any{"gen_ai.operation.name": "execute_tool"}
		// 空值属性省略（对齐真实 span：ReadToolCall 以 nil 触发调用方回退）。
		if step.ToolName != "" {
			attrs[obs.GenAIToolName] = step.ToolName
		}
		if step.CallArgs != nil {
			attrs[obs.GenAIToolCallArguments] = jsonStringOf(step.CallArgs)
		}
		if step.CallResult != nil {
			attrs[obs.GenAIToolCallResult] = jsonStringOf(step.CallResult)
		}
		if step.ToolCallID != "" {
			attrs[obs.GenAIToolCallID] = step.ToolCallID
		}
		return map[string]any{"name": fmt.Sprintf("execute_tool %s", step.ToolName), "attributes": attrs}
	default:
		return map[string]any{
			"name":       fmt.Sprintf("agent.step-%d", index),
			"attributes": AttributesFromMap(map[string]any{"gen_ai.operation.name": "invoke_agent"}),
		}
	}
}

// jsonStringOf 值 → JSON 字符串（字符串原样）。
func jsonStringOf(value any) string {
	if value == nil {
		return ""
	}
	if text, ok := value.(string); ok {
		return text
	}
	data, err := jsonMarshal(value)
	if err != nil {
		return fmt.Sprint(value)
	}
	return string(data)
}

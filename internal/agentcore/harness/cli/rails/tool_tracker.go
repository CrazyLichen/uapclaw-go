package rails

import (
	"context"
	"encoding/json"
	"fmt"

	cb "github.com/uapclaw/uapclaw-go/internal/agentcore/runner/callback"

	harnessrails "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/rails"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/stream"
)

// ToolTrackingRail 把工具调用/结果块写进会话流，供 CLI 渲染器以
// Claude Code 风格展示（`● ToolName(args)` / `⎿ result summary`）。
//
// 优先级很低：其他 rail（如 SecurityRail）可先改写或拒绝工具调用，
// UI 才收到通知。
//
// Python: openjiuwen/harness/cli/rails/tool_tracker.py（145 行）
// 对应类 ToolTrackingRail。
type ToolTrackingRail struct {
	DeepAgentRailAlias
}

// toolTrackingRailPriority 极低——在所有 rail 之后运行。
// Python: tool_tracker.py `priority = 5`
const toolTrackingRailPriority = 5

// NewToolTrackingRail 构造一个工具展示 rail。
// Python: tool_tracker.py（无 __init__，priority 类属性）
func NewToolTrackingRail() *ToolTrackingRail {
	r := &ToolTrackingRail{
		DeepAgentRailAlias: *harnessrails.NewDeepAgentRail(),
	}
	r.WithPriority(toolTrackingRailPriority)
	return r
}

// Callbacks 返回本 rail 挂接的回调集合。
// Python: tool_tracker.py before_tool_call / after_tool_call
func (r *ToolTrackingRail) Callbacks() map[agentinterfaces.AgentCallbackEvent]cb.PerAgentCallbackFunc {
	callbacks := r.BaseRail.GetCallbacks()
	callbacks[agentinterfaces.CallbackBeforeToolCall] = r.beforeToolCall
	callbacks[agentinterfaces.CallbackAfterToolCall] = r.afterToolCall
	return callbacks
}

// buildToolResultPayload 构造一次完成的工具调用的 UI 载荷。
// Python: tool_tracker.py:29-62 _build_tool_result_payload（staticmethod）
//
// tool_result 是兼容字段：结构化结果的 str()，既有消费者仍解析它。
// 展示层读 rendered_result（见 afterToolCall）。结构化的
// structured_result 字段刻意不发；UI 迁到结构化数据后这个字符串字段
// 与建立在它之上的解析会端到端移除。
//
// read_file 特例：读取 data.content 与 data.line_count（对齐 :46-61）。
func buildToolResultPayload(toolName string, toolResult any) map[string]any {
	payload := map[string]any{"tool_result": stringifyResult(toolResult)}
	if toolName != "read_file" || toolResult == nil {
		return payload
	}
	data, ok := toolResult.(map[string]any)
	if !ok {
		return payload
	}
	if content, exists := data["content"]; exists && content != nil {
		if encoded, isBytes := content.([]byte); isBytes {
			payload["tool_result"] = string(encoded)
		} else {
			payload["tool_result"] = stringifyResult(content)
		}
	}
	if lineCount, exists := data["line_count"]; exists && lineCount != nil {
		if n, ok := toInt(lineCount); ok {
			payload["line_count"] = n
		}
	}
	return payload
}

// stringifyResult 结果的字符串投影（Python: str(tool_result)）。
//
// Python 的 str() 对 dict 产生 repr；Go 侧走 JSON 编码，可读性等价。
func stringifyResult(value any) string {
	if value == nil {
		return ""
	}
	switch typed := value.(type) {
	case string:
		return typed
	case []byte:
		return string(typed)
	case map[string]any, []any:
		encoded, err := json.Marshal(typed)
		if err != nil {
			return fmt.Sprint(typed)
		}
		return string(encoded)
	default:
		return fmt.Sprint(typed)
	}
}

// toInt 宽松转 int（Python: int(value)，失败静默跳过）。
func toInt(value any) (int, bool) {
	switch typed := value.(type) {
	case int:
		return typed, true
	case int64:
		return int(typed), true
	case float64:
		return int(typed), true
	case string:
		var parsed int
		if _, err := fmt.Sscanf(typed, "%d", &parsed); err == nil {
			return parsed, true
		}
	}
	return 0, false
}

// beforeToolCall 工具开始时写一条 tool_call 块。
// Python: tool_tracker.py:64-89 before_tool_call
func (r *ToolTrackingRail) beforeToolCall(ctx context.Context, railCtx any) error {
	cbc, ok := railCtx.(*agentinterfaces.AgentCallbackContext)
	if !ok || cbc.Session() == nil {
		return nil
	}
	inputs, ok := cbc.Inputs().(*agentinterfaces.ToolCallInputs)
	if !ok {
		return nil
	}
	return cbc.Session().WriteStream(ctx, stream.OutputSchema{
		Type:  "tool_call",
		Index: 0,
		Payload: map[string]any{
			"tool_name": inputs.ToolName,
			"tool_args": inputs.ToolArgs,
		},
	})
}

// afterToolCall 工具结束时写一条 tool_result 块。
// Python: tool_tracker.py:91-145 after_tool_call
//
// 除兼容字段外，块携带 rendered_result：本次调用模型读到的确切文本。
// 本 rail 的优先级（5）低于每个会改写工具消息的 rail，因此文本是最终的。
//
// ⚠️ Python: :97-99 `ctx.is_paused` 检查——Go 侧 ToolCallInputs 无
// is_paused 字段（偏差，⤵️ 工具暂停回填时对齐）：Go 的工具审批暂停走
// interrupt 机制，被暂停的调用不会触发 after_tool_call，语义等价。
func (r *ToolTrackingRail) afterToolCall(ctx context.Context, railCtx any) error {
	cbc, ok := railCtx.(*agentinterfaces.AgentCallbackContext)
	if !ok || cbc.Session() == nil {
		return nil
	}
	inputs, ok := cbc.Inputs().(*agentinterfaces.ToolCallInputs)
	if !ok {
		return nil
	}
	payload := map[string]any{
		"tool_name": inputs.ToolName,
		"tool_args": inputs.ToolArgs,
	}
	for key, value := range buildToolResultPayload(inputs.ToolName, inputs.ToolResult) {
		payload[key] = value
	}
	// rendered_result：模型可见文本（Python: :138-141 经
	// resolve_tool_result_text）。Go 侧从 ToolResult 的 rendered_result
	// 字段或其字符串投影取（resolve_tool_message 的等价物，⤵️ 15.15a
	// 统一回填）。
	if rendered, hasRendered := extractRendered(inputs.ToolResult); hasRendered {
		payload["rendered_result"] = rendered
	}
	return cbc.Session().WriteStream(ctx, stream.OutputSchema{
		Type:    "tool_result",
		Index:   0,
		Payload: payload,
	})
}

// extractRendered 从工具结果中取模型可见文本。
// Python: ability_manager.py:125-146 resolve_tool_result_text（简化投影）
func extractRendered(toolResult any) (string, bool) {
	if toolResult == nil {
		return "", false
	}
	if data, ok := toolResult.(map[string]any); ok {
		if rendered, exists := data["rendered_result"]; exists {
			if text, isText := rendered.(string); isText && text != "" {
				return text, true
			}
		}
	}
	text := stringifyResult(toolResult)
	return text, text != ""
}

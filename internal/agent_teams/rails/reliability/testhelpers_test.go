// Package reliability 测试助手（exception 链构造与 cbc 构造）。
package reliability

import (
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	"github.com/uapclaw/uapclaw-go/internal/common/exception"
)

// exceptionWithRouteCase 构造携带模型组路由裁决 details 的异常。
func exceptionWithRouteCase() *exception.BaseError {
	return exception.NewBaseError(exception.StatusError,
		exception.WithDetails(map[string]any{
			"route_case":           "switchable",
			"reason":               "deterministic",
			"pinned_model":         "",
			"failures":             []any{},
			"usable":               []any{},
			"user_action_required": false,
		}))
}

// userMsgForMeasure 构造携带响应的 cbc（AfterModelCall 测量用）。
func userMsgForMeasure(content, thinking string) *agentinterfaces.AgentCallbackContext {
	var resp *schema.AssistantMessage
	if thinking != "" {
		resp = schema.NewAssistantMessage(content, schema.WithReasoningContent(thinking))
	} else {
		resp = schema.NewAssistantMessage(content)
	}
	return agentinterfaces.NewAgentCallbackContext(nil, &agentinterfaces.ModelCallInputs{Response: resp}, nil)
}

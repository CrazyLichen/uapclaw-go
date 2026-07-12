package interrupt

import (
	"context"

	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	saschema "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/schema"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ConfirmPayload 用户确认载荷。
//
// 对齐 Python: ConfirmPayload
type ConfirmPayload struct {
	// Approved 是否批准
	Approved bool `json:"approved"`
	// Feedback 反馈信息
	Feedback string `json:"feedback"`
	// AutoConfirm 是否自动确认（"始终允许"标记）
	AutoConfirm bool `json:"auto_confirm"`
}

// ConfirmRequest 确认请求配置。
//
// 对齐 Python: ConfirmRequest
type ConfirmRequest struct {
	// Message 向用户展示的确认消息
	Message string `json:"message"`
	// PayloadSchema 用户输入的数据结构定义
	PayloadSchema map[string]any `json:"payload_schema"`
}

// ConfirmInterruptRail 确认中断 Rail。
// 仅当 ConfirmPayload.Approved 为 true 时放行工具执行。
// 支持 auto_confirm 机制：当 session 状态中对应 key 为 true 时自动放行。
//
// 对齐 Python: ConfirmInterruptRail(BaseInterruptRail) — openjiuwen/harness/rails/interrupt/confirm_rail.py
type ConfirmInterruptRail struct {
	BaseInterruptRail
	// request 确认请求配置
	request ConfirmRequest
}

// ──────────────────────────── 全局变量 ────────────────────────────

// 编译时验证 ConfirmInterruptRail 满足 AgentRail 接口
var _ agentinterfaces.AgentRail = (*ConfirmInterruptRail)(nil)

// ──────────────────────────── 导出函数 ────────────────────────────

// NewConfirmInterruptRail 创建 ConfirmInterruptRail 实例。
// toolNames 为需确认拦截的工具名列表。
//
// 对齐 Python: ConfirmInterruptRail.__init__(tool_names)
func NewConfirmInterruptRail(toolNames ...string) *ConfirmInterruptRail {
	r := &ConfirmInterruptRail{
		BaseInterruptRail: *NewBaseInterruptRail(toolNames...),
		request: ConfirmRequest{
			Message:       "请确认或拒绝?",
			PayloadSchema: confirmPayloadSchema(),
		},
	}
	// 覆盖 resolveInterruptFn
	r.resolveInterruptFn = r.resolveConfirmInterrupt
	return r
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// resolveConfirmInterrupt ConfirmInterruptRail 的中断解析逻辑。
//
// auto_confirm → Approve；无输入 → Interrupt；
// approved → Approve；!approved → Reject(feedback)。
//
// 对齐 Python: ConfirmInterruptRail.resolve_interrupt(ctx, tool_call, user_input, auto_confirm_config)
func (r *ConfirmInterruptRail) resolveConfirmInterrupt(
	_ context.Context,
	_ *agentinterfaces.AgentCallbackContext,
	toolCall *llmschema.ToolCall,
	userInput any,
	autoConfirmConfig map[string]any,
) InterruptDecision {
	autoConfirmKey := r.getAutoConfirmKey(toolCall)

	// 无用户输入
	if userInput == nil {
		// 检查 auto_confirm
		if isAutoConfirmed(autoConfirmConfig, autoConfirmKey) {
			return r.Approve("")
		}
		return r.Interrupt(&saschema.InterruptRequest{
			Message:        r.request.Message,
			PayloadSchema:  r.request.PayloadSchema,
			AutoConfirmKey: autoConfirmKey,
		})
	}

	// 解析用户输入为 ConfirmPayload
	payload, ok := r.parseConfirmInput(userInput)
	if !ok {
		return r.Interrupt(&saschema.InterruptRequest{
			Message:        r.request.Message,
			PayloadSchema:  r.request.PayloadSchema,
			AutoConfirmKey: autoConfirmKey,
		})
	}

	// approved → Approve; !approved → Reject(feedback)
	if payload.Approved {
		return r.Approve("")
	}
	feedback := payload.Feedback
	if feedback == "" {
		feedback = "User feedback: rejected\n action"
	}
	return r.Reject(feedback)
}

// getAutoConfirmKey 返回 auto_confirm 配置键。
// 默认使用 toolCall.Name 作为 key。
//
// 对齐 Python: ConfirmInterruptRail._get_auto_confirm_key(tool_call)
func (r *ConfirmInterruptRail) getAutoConfirmKey(toolCall *llmschema.ToolCall) string {
	if toolCall == nil {
		return ""
	}
	return toolCall.Name
}

// parseConfirmInput 解析用户输入为 ConfirmPayload。
// 支持 ConfirmPayload / map[string]any 两种格式。
func (r *ConfirmInterruptRail) parseConfirmInput(userInput any) (*ConfirmPayload, bool) {
	switch input := userInput.(type) {
	case *ConfirmPayload:
		return input, true
	case map[string]any:
		payload := &ConfirmPayload{}
		// approved 为必填字段，缺少或类型不匹配时重新中断（对齐 Python model_validate 行为）
		v, hasApproved := input["approved"]
		if !hasApproved {
			return nil, false
		}
		b, ok := v.(bool)
		if !ok {
			return nil, false
		}
		payload.Approved = b
		if v, ok := input["feedback"]; ok {
			if s, ok := v.(string); ok {
				payload.Feedback = s
			}
		}
		if v, ok := input["auto_confirm"]; ok {
			if b, ok := v.(bool); ok {
				payload.AutoConfirm = b
			}
		}
		return payload, true
	default:
		return nil, false
	}
}

// isAutoConfirmed 检查 auto_confirm 配置中指定 key 是否为 true。
//
// 对齐 Python: ConfirmInterruptRail._is_auto_confirmed(config, key)
func isAutoConfirmed(config map[string]any, key string) bool {
	if config == nil {
		return false
	}
	val, ok := config[key]
	if !ok {
		return false
	}
	b, ok := val.(bool)
	return ok && b
}

// confirmPayloadSchema 返回 ConfirmPayload 的 JSON Schema。
func confirmPayloadSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"approved": map[string]any{
				"type":        "boolean",
				"description": "是否批准",
			},
			"feedback": map[string]any{
				"type":        "string",
				"description": "反馈信息",
			},
			"auto_confirm": map[string]any{
				"type":        "boolean",
				"description": "是否自动确认（始终允许）",
			},
		},
		"required": []string{"approved"},
	}
}

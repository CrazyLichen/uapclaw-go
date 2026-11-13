package rails

import (
	"context"

	cb "github.com/uapclaw/uapclaw-go/internal/agentcore/runner/callback"

	harnessrails "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/rails"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
)

// TokenTrackingRail 累计模型调用的 token 用量。
//
// 挂到 after_model_call 钩子上累计 LLM 响应的 token 数，
// GetSummary 供 /status 与 /cost 命令读取。
//
// Python: openjiuwen/harness/cli/rails/token_tracker.py（61 行）
// 对应类 TokenTrackingRail。
type TokenTrackingRail struct {
	DeepAgentRailAlias

	// totalInputTokens 输入 token 累计
	totalInputTokens int
	// totalOutputTokens 输出 token 累计
	totalOutputTokens int
	// callCount 模型调用次数
	callCount int
}

// DeepAgentRailAlias 为本包引入 rails 基座类型的短别名。
//
// harness/rails 的 DeepAgentRail 与本包同名（包路径不同），
// 嵌入时用别名避免读者混淆。
type DeepAgentRailAlias = harnessrails.DeepAgentRail

// tokenTrackingRailPriority 低优先级——在其他 rail 之后运行。
// Python: token_tracker.py `priority = 10`
const tokenTrackingRailPriority = 10

// NewTokenTrackingRail 构造一个用量统计 rail。
// Python: token_tracker.py __init__
func NewTokenTrackingRail() *TokenTrackingRail {
	r := &TokenTrackingRail{
		DeepAgentRailAlias: *harnessrails.NewDeepAgentRail(),
	}
	r.WithPriority(tokenTrackingRailPriority)
	return r
}

// Callbacks 返回本 rail 挂接的回调集合。
// Python: token_tracker.py after_model_call
func (r *TokenTrackingRail) Callbacks() map[agentinterfaces.AgentCallbackEvent]cb.PerAgentCallbackFunc {
	callbacks := r.BaseRail.GetCallbacks()
	callbacks[agentinterfaces.CallbackAfterModelCall] = r.afterModelCall
	return callbacks
}

// afterModelCall 从模型响应提取 token 用量。
// Python: token_tracker.py:30-45 after_model_call
//
// 响应对象应携带 usage（prompt_tokens / completion_tokens）。
// Go 侧 Response 是 *llmschema.AssistantMessage，用量在 UsageMetadata。
func (r *TokenTrackingRail) afterModelCall(ctx context.Context, railCtx any) error {
	r.callCount++
	cbc, ok := railCtx.(*agentinterfaces.AgentCallbackContext)
	if !ok {
		return nil
	}
	inputs, ok := cbc.Inputs().(*agentinterfaces.ModelCallInputs)
	if !ok || inputs.Response == nil || inputs.Response.UsageMetadata == nil {
		return nil
	}
	r.totalInputTokens += inputs.Response.UsageMetadata.InputTokens
	r.totalOutputTokens += inputs.Response.UsageMetadata.OutputTokens
	return nil
}

// GetSummary 返回 /status 与 /cost 可用的摘要。
// Python: token_tracker.py:47-55 get_summary
func (r *TokenTrackingRail) GetSummary() map[string]any {
	return map[string]any{
		"input_tokens":  r.totalInputTokens,
		"output_tokens": r.totalOutputTokens,
		"total_tokens":  r.totalInputTokens + r.totalOutputTokens,
		"model_calls":   r.callCount,
	}
}

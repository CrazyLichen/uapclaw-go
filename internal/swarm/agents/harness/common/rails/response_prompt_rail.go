package rails

import (
	"context"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/rails"
	cb "github.com/uapclaw/uapclaw-go/internal/agentcore/runner/callback"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	saprompt "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/prompts"
	"github.com/uapclaw/uapclaw-go/internal/swarm/agents/harness/common/prompt"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ResponsePromptRail 响应提示词护栏 — 在 before_model_call 中注入消息格式说明节。
//
// 告知 LLM 如何区分用户消息与系统消息（cron/heartbeat/notify），
// 使 LLM 能按消息来源和类型采取不同处理策略。
//
// 生命周期：
//   - Init：从 Agent 获取 system_prompt_builder 引用
//   - BeforeModelCall：调 BuildResponseSection(language) 构建节，注入 builder
//   - Uninit：清除注入的 response section，释放 builder 引用
//
// Python: ResponsePromptRail(DeepAgentRail) — response_prompt_rail.py (36 行)
type ResponsePromptRail struct {
	rails.DeepAgentRail
	// builder 系统提示词构建器引用，Init 时从 Agent 获取，Uninit 时置 nil
	// Python: self.system_prompt_builder
	builder saprompt.SystemPromptBuilderInterface
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

const (
	// responsePromptRailPriority ResponsePromptRail 优先级
	// Python: ResponsePromptRail.priority = 5
	responsePromptRailPriority = 5
)

// ──────────────────────────── 全局变量 ────────────────────────────

// 编译时验证 ResponsePromptRail 满足 AgentRail 接口
var _ agentinterfaces.AgentRail = (*ResponsePromptRail)(nil)

// ──────────────────────────── 导出函数 ────────────────────────────

// NewResponsePromptRail 创建 ResponsePromptRail 实例。
// Python: ResponsePromptRail()
func NewResponsePromptRail() *ResponsePromptRail {
	r := &ResponsePromptRail{}
	r.WithPriority(responsePromptRailPriority)
	return r
}

// Init 从 Agent 获取 system_prompt_builder 引用并存储。
// Python: ResponsePromptRail.init(agent)
func (r *ResponsePromptRail) Init(_ context.Context, agent agentinterfaces.BaseAgent) error {
	if agent != nil {
		r.builder = agent.SystemPromptBuilder()
	}
	return nil
}

// Uninit 清除注入的 response section 并释放 builder 引用。
// Python: ResponsePromptRail.uninit(agent)
func (r *ResponsePromptRail) Uninit(_ agentinterfaces.BaseAgent) error {
	if r.builder != nil {
		r.builder.RemoveSection("response")
	}
	r.builder = nil
	return nil
}

// BeforeModelCall 在模型调用前注入消息格式说明 PromptSection。
// Python: ResponsePromptRail.before_model_call(ctx)
func (r *ResponsePromptRail) BeforeModelCall(_ context.Context, cbc *agentinterfaces.AgentCallbackContext) error {
	// 优先使用 Init 时存储的 builder（对齐 Python: self.system_prompt_builder）
	builder := r.builder
	if builder == nil && cbc != nil {
		builder = cbc.Agent().SystemPromptBuilder()
	}
	if builder == nil {
		return nil
	}

	// Python: section = _response_prompt(self.system_prompt_builder.language or "cn")
	language := builder.Language()
	if language == "" {
		language = "cn"
	}
	section := prompt.BuildResponseSection(language)
	builder.AddSection(section)
	return nil
}

// GetCallbacks 覆写基类回调映射，注册 BeforeModelCall。
func (r *ResponsePromptRail) GetCallbacks() map[agentinterfaces.AgentCallbackEvent]cb.PerAgentCallbackFunc {
	callbacks := r.DeepAgentRail.GetCallbacks()
	callbacks[agentinterfaces.CallbackBeforeModelCall] = func(ctx context.Context, railCtx any) error {
		return r.BeforeModelCall(ctx, railCtx.(*agentinterfaces.AgentCallbackContext))
	}
	return callbacks
}

// ──────────────────────────── 非导出函数 ────────────────────────────

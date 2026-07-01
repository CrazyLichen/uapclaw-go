package rail

import (
	"context"

	cb "github.com/uapclaw/uapclaw-go/internal/agentcore/runner/callback"
)

// ──────────────────────────── 结构体 ────────────────────────────

// AgentRail Agent 生命周期 Rail 接口。
//
// Rail 是 class-based 的生命周期钩子容器，允许在 Agent 执行流程的
// 特定时机注入拦截逻辑（重试、提前终止、steering 等）。
// 嵌入 BaseRail 后只需覆盖关心的钩子方法，并在 GetCallbacks() 中
// 声明已覆盖的事件映射。
//
// 对应 Python: AgentRail(ABC) (openjiuwen/core/single_agent/rail/base.py L451-573)
type AgentRail interface {
	// Priority 返回执行优先级（数值越大越先执行）
	Priority() int
	// Init Rail 初始化钩子（注册时调用，用于工具自注册等）
	Init(agent RailAgent) error
	// Uninit Rail 注销钩子（注销时调用，用于工具清理等）
	Uninit(agent RailAgent) error

	// ── 10 个生命周期钩子方法 ──

	// BeforeInvoke invoke 开始前
	BeforeInvoke(ctx context.Context, cbc *AgentCallbackContext) error
	// AfterInvoke invoke 完成后
	AfterInvoke(ctx context.Context, cbc *AgentCallbackContext) error
	// BeforeTaskIteration 外层任务循环迭代开始前
	BeforeTaskIteration(ctx context.Context, cbc *AgentCallbackContext) error
	// AfterTaskIteration 外层任务循环迭代完成后
	AfterTaskIteration(ctx context.Context, cbc *AgentCallbackContext) error
	// BeforeModelCall LLM 调用前
	BeforeModelCall(ctx context.Context, cbc *AgentCallbackContext) error
	// AfterModelCall LLM 响应后
	AfterModelCall(ctx context.Context, cbc *AgentCallbackContext) error
	// OnModelException LLM 调用异常
	OnModelException(ctx context.Context, cbc *AgentCallbackContext) error
	// BeforeToolCall 工具执行前
	BeforeToolCall(ctx context.Context, cbc *AgentCallbackContext) error
	// AfterToolCall 工具执行后
	AfterToolCall(ctx context.Context, cbc *AgentCallbackContext) error
	// OnToolException 工具执行异常
	OnToolException(ctx context.Context, cbc *AgentCallbackContext) error

	// GetCallbacks 提取已覆盖的钩子方法映射，供 RegisterRail 批量注册。
	//
	// 用户嵌入 BaseRail 后通过 BuildCallbacks(CallbackFrom(...)) 实现。
	GetCallbacks() map[AgentCallbackEvent]cb.PerAgentCallbackFunc
}

// BaseRail AgentRail 的 no-op 默认实现。
//
// 用户嵌入此结构体后只需覆盖关心的钩子方法，并在 GetCallbacks() 中
// 通过 CallbackFrom + BuildCallbacks 声明已覆盖的事件映射。
//
// 对应 Python: AgentRail 基类的 10 个默认 no-op 方法
type BaseRail struct {
	// priority 执行优先级（数值越大越先执行），默认 50
	priority int
}

// callbackEntry 事件→回调映射条目，BuildCallbacks 的参数。
type callbackEntry struct {
	event AgentCallbackEvent
	fn    cb.PerAgentCallbackFunc
}

// ──────────────────────────── 导出函数 ────────────────────────────

// NewBaseRail 创建默认优先级(50)的 BaseRail。
func NewBaseRail() *BaseRail {
	return &BaseRail{priority: 50}
}

// Priority 返回优先级。
func (r *BaseRail) Priority() int {
	return r.priority
}

// WithPriority 设置优先级（Functional Options 模式）。
func (r *BaseRail) WithPriority(p int) *BaseRail {
	r.priority = p
	return r
}

// Init 默认 no-op。
func (r *BaseRail) Init(_ RailAgent) error { return nil }

// Uninit 默认 no-op。
func (r *BaseRail) Uninit(_ RailAgent) error { return nil }

// BeforeInvoke 默认 no-op。
func (r *BaseRail) BeforeInvoke(_ context.Context, _ *AgentCallbackContext) error { return nil }

// AfterInvoke 默认 no-op。
func (r *BaseRail) AfterInvoke(_ context.Context, _ *AgentCallbackContext) error { return nil }

// BeforeTaskIteration 默认 no-op。
func (r *BaseRail) BeforeTaskIteration(_ context.Context, _ *AgentCallbackContext) error { return nil }

// AfterTaskIteration 默认 no-op。
func (r *BaseRail) AfterTaskIteration(_ context.Context, _ *AgentCallbackContext) error { return nil }

// BeforeModelCall 默认 no-op。
func (r *BaseRail) BeforeModelCall(_ context.Context, _ *AgentCallbackContext) error { return nil }

// AfterModelCall 默认 no-op。
func (r *BaseRail) AfterModelCall(_ context.Context, _ *AgentCallbackContext) error { return nil }

// OnModelException 默认 no-op。
func (r *BaseRail) OnModelException(_ context.Context, _ *AgentCallbackContext) error { return nil }

// BeforeToolCall 默认 no-op。
func (r *BaseRail) BeforeToolCall(_ context.Context, _ *AgentCallbackContext) error { return nil }

// AfterToolCall 默认 no-op。
func (r *BaseRail) AfterToolCall(_ context.Context, _ *AgentCallbackContext) error { return nil }

// OnToolException 默认 no-op。
func (r *BaseRail) OnToolException(_ context.Context, _ *AgentCallbackContext) error { return nil }

// GetCallbacks 返回空映射（默认无钩子覆盖）。
func (r *BaseRail) GetCallbacks() map[AgentCallbackEvent]cb.PerAgentCallbackFunc {
	return make(map[AgentCallbackEvent]cb.PerAgentCallbackFunc)
}

// CallbackFrom 创建一条事件→回调映射条目。
//
// 用法：
//
//	r.CallbackFrom(CallbackBeforeModelCall, wrappedFn)
func (r *BaseRail) CallbackFrom(event AgentCallbackEvent, fn cb.PerAgentCallbackFunc) callbackEntry {
	return callbackEntry{event: event, fn: fn}
}

// BuildCallbacks 从多条映射条目构建 GetCallbacks 返回值。
//
// 用法：
//
//	func (r *MyRail) GetCallbacks() map[AgentCallbackEvent]cb.PerAgentCallbackFunc {
//	    return r.BuildCallbacks(
//	        r.CallbackFrom(CallbackBeforeModelCall, r.BeforeModelCall),
//	        r.CallbackFrom(CallbackAfterModelCall, r.AfterModelCall),
//	    )
//	}
func (r *BaseRail) BuildCallbacks(entries ...callbackEntry) map[AgentCallbackEvent]cb.PerAgentCallbackFunc {
	m := make(map[AgentCallbackEvent]cb.PerAgentCallbackFunc, len(entries))
	for _, e := range entries {
		m[e.event] = e.fn
	}
	return m
}

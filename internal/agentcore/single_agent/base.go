package single_agent

import (
	"context"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/runner/callback"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/stream"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	agentschema "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/schema"
	"github.com/uapclaw/uapclaw-go/internal/common/exception"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// agentInvoker 子类真实执行接口，用于虚分发。
//
// Go 内嵌结构体无法虚分发到子类方法（w.invokeImpl() 在编译期
// 绑定 WarpBaseAgent.invokeImpl，不会调用 ReActAgent.invokeImpl）。
// 通过接口字段 invoker agentInvoker 实现等价虚方法表：
// 构造时 agent.invoker = agent，调用 w.invoker.invokeImpl() 走虚分发。
type agentInvoker interface {
	// invokeImpl 子类实现的非流式调用逻辑
	invokeImpl(ctx context.Context, inputs map[string]any, opts ...AgentOption) (any, error)
	// streamImpl 子类实现的流式调用逻辑
	streamImpl(ctx context.Context, inputs map[string]any, opts ...AgentOption) (<-chan stream.Schema, error)
}

// WarpBaseAgent BaseAgent 的默认实现，提供 Invoke/Stream 的回调包装骨架。
// 子类内嵌 WarpBaseAgent 并实现 agentInvoker 接口。
//
// 对应 Python: openjiuwen/core/single_agent/base.py (BaseAgent)
type WarpBaseAgent struct {
	// card Agent 身份卡片（必需）
	card *agentschema.AgentCard
	// config Agent 配置（可选，Configure 时设置）
	config any
	// abilityManager 能力管理器
	abilityManager *AbilityManager
	// callbackManager 回调管理器
	// ⤵️ 6.6 回填：从 any 改为 *AgentCallbackManager
	callbackManager any
	// invoker 子类注入的真实执行逻辑，实现虚分发
	invoker agentInvoker
}

// ──────────────────────────── 枚举 ────────────────────────────

// AgentOption Agent 调用选项函数（re-export from interfaces）。
type AgentOption = interfaces.AgentOption

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// NewWarpBaseAgent 创建 WarpBaseAgent 实例。
func NewWarpBaseAgent(card *agentschema.AgentCard, resourceMgr ResourceManager) *WarpBaseAgent {
	return &WarpBaseAgent{
		card:           card,
		abilityManager: NewAbilityManager(resourceMgr),
	}
}

// Configure 配置 Agent。
// 对应 Python: BaseAgent.configure(config)
func (w *WarpBaseAgent) Configure(_ context.Context, config any) error {
	w.config = config
	return nil
}

// Invoke 非流式调用，包含回调包装骨架。
// 执行顺序：① emit_before → ② transform_io(输入) → invokeImpl → ② transform_io(输出) → ③ emit_after
//
// 对应 Python: _AgentMeta 元类装饰后的 invoke
func (w *WarpBaseAgent) Invoke(ctx context.Context, inputs map[string]any, opts ...AgentOption) (any, error) {
	if w.invoker == nil {
		return nil, exception.NewBaseError(exception.StatusAgentNotConfigured,
			exception.WithMsg("invoker 未设置，子类构造时必须设置 invoker"))
	}

	fw := callback.GetCallbackFramework()

	// ① emit_before: 触发全局 AgentInvokeInput 事件
	fw.TriggerAgent(ctx, &callback.AgentCallEventData{
		Event:   callback.AgentInvokeInput,
		AgentID: w.card.ID,
		Inputs:  inputs,
	})

	// ② transform_io 输入变换（⤵️ 预留，6.24 回填）
	// inputs = fw.TriggerTransform(ctx, callback.AgentInvokeInput, inputs)

	// 执行子类的真实逻辑
	result, err := w.invoker.invokeImpl(ctx, inputs, opts...)
	if err != nil {
		// 已经是 BaseError 则直接返回（对齐 Python except BaseError: raise）
		if _, ok := err.(*exception.BaseError); ok {
			return nil, err
		}
		// 其他错误包装（对齐 Python except Exception as e: raise build_error(...)）
		logger.Error(logger.ComponentAgentCore).
			Str("agent_id", w.card.ID).
			Err(err).
			Msg("Agent invoke 错误")
		return nil, exception.NewBaseError(exception.StatusAgentControllerRuntimeError,
			exception.WithCause(err),
		)
	}

	// ② transform_io 输出变换（⤵️ 预留，6.24 回填）
	// result = fw.TriggerTransform(ctx, callback.AgentInvokeOutput, result)

	// ③ emit_after: 触发全局 AgentInvokeOutput 事件
	fw.TriggerAgent(ctx, &callback.AgentCallEventData{
		Event:   callback.AgentInvokeOutput,
		AgentID: w.card.ID,
		Result:  result,
	})

	return result, nil
}

// Stream 流式调用，包含回调包装骨架。
// 执行顺序：① emit_before → streamImpl → per-item { ② transform_io(输出) → ③ emit_after }
//
// 对应 Python: _AgentMeta 元类装饰后的 stream
func (w *WarpBaseAgent) Stream(ctx context.Context, inputs map[string]any, opts ...AgentOption) (<-chan stream.Schema, error) {
	if w.invoker == nil {
		return nil, exception.NewBaseError(exception.StatusAgentNotConfigured,
			exception.WithMsg("invoker 未设置，子类构造时必须设置 invoker"))
	}

	fw := callback.GetCallbackFramework()

	// ① emit_before
	fw.TriggerAgent(ctx, &callback.AgentCallEventData{
		Event:   callback.AgentStreamInput,
		AgentID: w.card.ID,
		Inputs:  inputs,
	})

	// 调用子类的真实 stream
	ch, err := w.invoker.streamImpl(ctx, inputs, opts...)
	if err != nil {
		if _, ok := err.(*exception.BaseError); ok {
			return nil, err
		}
		logger.Error(logger.ComponentAgentCore).
			Str("agent_id", w.card.ID).
			Err(err).
			Msg("Agent stream 错误")
		return nil, exception.NewBaseError(exception.StatusAgentControllerRuntimeError,
			exception.WithCause(err),
		)
	}

	// 包装 channel：每个 item 触发 ③ emit_after 后转发
	out := make(chan stream.Schema)
	go func() {
		defer close(out)
		for item := range ch {
			// ② transform_io 输出变换（⤵️ 预留，6.24 回填）
			// item = fw.TriggerTransform(ctx, callback.AgentStreamOutput, item)

			// ③ emit_after (per-item)
			fw.TriggerAgent(ctx, &callback.AgentCallEventData{
				Event:   callback.AgentStreamOutput,
				AgentID: w.card.ID,
				Result:  item,
			})

			out <- item
		}
	}()

	return out, nil
}

// Card 返回 Agent 身份卡片。
func (w *WarpBaseAgent) Card() *agentschema.AgentCard { return w.card }

// Config 返回当前配置。
func (w *WarpBaseAgent) Config() any { return w.config }

// AbilityManager 返回能力管理器。
func (w *WarpBaseAgent) AbilityManager() any { return w.abilityManager }

// CallbackManager 返回回调管理器。
// ⤵️ 6.6 回填：返回类型从 any 改为 *AgentCallbackManager
func (w *WarpBaseAgent) CallbackManager() any { return w.callbackManager }

// RegisterCallback 注册回调。
// ⤵️ 预留：6.4-6.6 实现后委托给 AgentCallbackManager
func (w *WarpBaseAgent) RegisterCallback(_ context.Context, _ any, _ any, _ int) error {
	return nil
}

// RegisterRail 注册 Rail。
// ⤵️ 预留：6.7 实现后委托给 AgentCallbackManager
func (w *WarpBaseAgent) RegisterRail(_ context.Context, _ any) error {
	return nil
}

// UnregisterRail 注销 Rail。
// ⤵️ 预留：6.7 实现后委托给 AgentCallbackManager
func (w *WarpBaseAgent) UnregisterRail(_ context.Context, _ any) error {
	return nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

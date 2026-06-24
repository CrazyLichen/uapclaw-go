package single_agent

import (
	"context"
	"fmt"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/runner/callback"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/stream"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/ability"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/rail"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/resource"
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
	config interfaces.AgentConfig
	// abilityManager 能力管理器
	abilityManager *ability.AbilityManager
	// callbackManager 回调管理器
	callbackManager *rail.AgentCallbackManager
	// invoker 子类注入的真实执行逻辑，实现虚分发
	invoker agentInvoker
}

// ──────────────────────────── 枚举 ────────────────────────────

// AgentOption Agent 调用选项函数（re-export from interfaces）。
type AgentOption = interfaces.AgentOption

// 以下类型别名为子包 re-export，保持包内兼容。
type (
	// AbilityManager 能力管理器（re-export from ability 子包）
	AbilityManager = ability.AbilityManager
	// AddAbilityResult 添加能力结果（re-export from ability 子包）
	AddAbilityResult = ability.AddAbilityResult
	// ExecuteResult 工具执行结果（re-export from ability 子包）
	ExecuteResult = ability.ExecuteResult
	// ToolRail 工具调用钩子接口（re-export from ability 子包）
	ToolRail = ability.ToolRail
	// AbilityExecutionError 能力执行错误（re-export from ability 子包）
	AbilityExecutionError = ability.AbilityExecutionError
	// ResourceManager 资源管理器接口（re-export from resource 子包）
	ResourceManager = resource.ResourceManager
	// NoopResourceManager 空资源管理器（re-export from resource 子包）
	NoopResourceManager = resource.NoopResourceManager
	// ResourceOptions 资源选项（re-export from resource 子包）
	ResourceOptions = resource.ResourceOptions
	// ResourceOption 资源选项函数（re-export from resource 子包）
	ResourceOption = resource.ResourceOption
)

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// NewWarpBaseAgent 创建 WarpBaseAgent 实例。
func NewWarpBaseAgent(card *agentschema.AgentCard, resourceMgr resource.ResourceManager) *WarpBaseAgent {
	return &WarpBaseAgent{
		card:           card,
		abilityManager: ability.NewAbilityManager(resourceMgr),
		callbackManager: rail.NewAgentCallbackManager(card.ID),
	}
}

// Configure 配置 Agent。
// 对应 Python: BaseAgent.configure(config)
func (w *WarpBaseAgent) Configure(_ context.Context, config interfaces.AgentConfig) error {
	w.config = config
	return nil
}

// Invoke 非流式调用，包含回调包装骨架。
// 执行顺序：① transform_io input → ② emit_before → invokeImpl → ③ transform_io output → ④ emit_after
//
// 对应 Python: _AgentMeta 元类装饰后的 invoke
func (w *WarpBaseAgent) Invoke(ctx context.Context, inputs map[string]any, opts ...AgentOption) (any, error) {
	if w.invoker == nil {
		return nil, exception.NewBaseError(exception.StatusAgentNotConfigured,
			exception.WithMsg("invoker 未设置，子类构造时必须设置 invoker"))
	}

	fw := callback.GetCallbackFramework()

	// 解析 AgentOptions，获取 Session 等选项（对齐 Python invoke(inputs, session=None)）
	agentOpts := interfaces.NewAgentOptions(opts...)

	// ① transform_io 输入变换（对齐 Python transform_io 的 input_fn）
	if transformed := fw.TransformAgentIOInput(ctx, callback.GlobalAgentInvokeInput, inputs); transformed != nil {
		if v, ok := transformed.(map[string]any); ok {
			inputs = v
		} else {
			logger.Warn(logger.ComponentAgentCore).
				Str("event", "TransformAgentIOInput").
				Str("agent_id", w.card.ID).
				Str("expected", "map[string]any").
				Str("actual", fmt.Sprintf("%T", transformed)).
				Msg("TransformIO 返回类型不匹配，使用原始输入")
		}
	}

	// ② emit_before: 触发全局 AgentInvokeInput 事件
	fw.TriggerGlobalAgent(ctx, &callback.GlobalAgentEventData{
		Event:     callback.GlobalAgentInvokeInput,
		AgentID:   w.card.ID,
		AgentName: w.card.Name,
		Inputs:    inputs,
		Session:   agentOpts.Session,
	})

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

	// ③ transform_io 输出变换（对齐 Python transform_io 的 output_fn）
	result = fw.TransformAgentIOOutput(ctx, callback.GlobalAgentInvokeOutput, result)

	// ④ emit_after: 触发全局 AgentInvokeOutput 事件
	fw.TriggerGlobalAgent(ctx, &callback.GlobalAgentEventData{
		Event:     callback.GlobalAgentInvokeOutput,
		AgentID:   w.card.ID,
		AgentName: w.card.Name,
		Result:    result,
	})

	return result, nil
}

// Stream 流式调用，包含回调包装骨架。
// 执行顺序：① transform_io input → ② emit_before → streamImpl → per-item { ③ transform_io output → ④ emit_after }
//
// 对应 Python: _AgentMeta 元类装饰后的 stream
func (w *WarpBaseAgent) Stream(ctx context.Context, inputs map[string]any, opts ...AgentOption) (<-chan stream.Schema, error) {
	if w.invoker == nil {
		return nil, exception.NewBaseError(exception.StatusAgentNotConfigured,
			exception.WithMsg("invoker 未设置，子类构造时必须设置 invoker"))
	}

	fw := callback.GetCallbackFramework()

	// 解析 AgentOptions，获取 Session 等选项（对齐 Python stream(inputs, session=None)）
	agentOpts := interfaces.NewAgentOptions(opts...)

	// ① transform_io 输入变换（对齐 Python transform_io 的 input_fn）
	if transformed := fw.TransformAgentIOInput(ctx, callback.GlobalAgentStreamInput, inputs); transformed != nil {
		if v, ok := transformed.(map[string]any); ok {
			inputs = v
		} else {
			logger.Warn(logger.ComponentAgentCore).
				Str("event", "TransformAgentIOInput").
				Str("agent_id", w.card.ID).
				Str("expected", "map[string]any").
				Str("actual", fmt.Sprintf("%T", transformed)).
				Msg("TransformIO 返回类型不匹配，使用原始输入")
		}
	}

	// ② emit_before: 触发全局 AgentStreamInput 事件
	fw.TriggerGlobalAgent(ctx, &callback.GlobalAgentEventData{
		Event:     callback.GlobalAgentStreamInput,
		AgentID:   w.card.ID,
		AgentName: w.card.Name,
		Inputs:    inputs,
		Session:   agentOpts.Session,
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

	// 包装 channel：per-item { ③ transform_io 输出变换 → ④ emit_after }
	out := make(chan stream.Schema)
	go func() {
		defer close(out)
		for item := range ch {
			// ③ transform_io 输出变换（对齐 Python transform_io 的 output_fn，per item）
			if transformed := fw.TransformAgentIOOutput(ctx, callback.GlobalAgentStreamOutput, item); transformed != nil {
				if v, ok := transformed.(stream.Schema); ok {
					item = v
				} else {
					logger.Warn(logger.ComponentAgentCore).
						Str("event", "TransformAgentIOOutput").
						Str("agent_id", w.card.ID).
						Str("expected", "stream.Schema").
						Str("actual", fmt.Sprintf("%T", transformed)).
						Msg("TransformIO 返回类型不匹配，使用原始输出")
				}
			}
			// ④ emit_after (per_item)
			fw.TriggerGlobalAgent(ctx, &callback.GlobalAgentEventData{
				Event:     callback.GlobalAgentStreamOutput,
				AgentID:   w.card.ID,
				AgentName: w.card.Name,
				Result:    item,
			})
			out <- item
		}
	}()

	return out, nil
}

// Card 返回 Agent 身份卡片。
func (w *WarpBaseAgent) Card() *agentschema.AgentCard { return w.card }

// Config 返回当前配置。
func (w *WarpBaseAgent) Config() interfaces.AgentConfig { return w.config }

// AbilityManager 返回能力管理器。
// 返回 any，调用方通过类型断言获取 *ability.AbilityManager。
func (w *WarpBaseAgent) AbilityManager() any { return w.abilityManager }

// CallbackManager 返回回调管理器。
// 返回 any（实际类型 *rail.AgentCallbackManager），避免循环依赖。
func (w *WarpBaseAgent) CallbackManager() any { return w.callbackManager }

// RegisterCallback 注册回调。
// 委托给 AgentCallbackManager.RegisterCallback。
func (w *WarpBaseAgent) RegisterCallback(ctx context.Context, event any, fn any, opts ...callback.CallbackOption) error {
	if w.callbackManager != nil {
		w.callbackManager.RegisterCallback(ctx, event.(rail.AgentCallbackEvent), fn.(callback.PerAgentCallbackFunc), opts...)
	}
	return nil
}

// RegisterRail 注册 Rail。
// 委托给 AgentCallbackManager.RegisterRail。
func (w *WarpBaseAgent) RegisterRail(ctx context.Context, railObj any, opts ...callback.CallbackOption) error {
	if w.callbackManager != nil {
		return w.callbackManager.RegisterRail(ctx, railObj, opts...)
	}
	return nil
}

// UnregisterRail 注销 Rail。
// 委托给 AgentCallbackManager.UnregisterRail。
func (w *WarpBaseAgent) UnregisterRail(ctx context.Context, railObj any) error {
	if w.callbackManager != nil {
		return w.callbackManager.UnregisterRail(ctx, railObj)
	}
	return nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

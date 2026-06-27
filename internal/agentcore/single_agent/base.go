package single_agent

import (
	"context"

	resourcesmanager "github.com/uapclaw/uapclaw-go/internal/agentcore/runner/resources_manager"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/runner/callback"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/ability"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/rail"
	agentschema "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/schema"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// BaseAgent Agent 基础配置/管理容器。
// 不实现 Invoke/Stream，子类自行实现并在方法体内调用回调骨架。
//
// 对应 Python: BaseAgent（不含 _AgentMeta 装饰逻辑）
type BaseAgent struct {
	// card Agent 身份卡片（必需）
	card *agentschema.AgentCard
	// config Agent 配置（可选，Configure 时设置）
	config interfaces.AgentConfig
	// abilityManager 能力管理器
	abilityManager *ability.AbilityManager
	// callbackManager 回调管理器
	callbackManager *rail.AgentCallbackManager
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
	// AbilityExecutionError 能力执行错误（re-export from ability 子包）
	AbilityExecutionError = ability.AbilityExecutionError
)

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// NewBaseAgent 创建 BaseAgent 实例。
func NewBaseAgent(card *agentschema.AgentCard, resourceMgr *resourcesmanager.ResourceMgr) *BaseAgent {
	return &BaseAgent{
		card:            card,
		abilityManager:  ability.NewAbilityManager(resourceMgr),
		callbackManager: rail.NewAgentCallbackManager(card.ID),
	}
}

// Configure 配置 Agent。
// 对应 Python: BaseAgent.configure(config)
func (b *BaseAgent) Configure(_ context.Context, config interfaces.AgentConfig) error {
	b.config = config
	return nil
}

// Card 返回 Agent 身份卡片。
func (b *BaseAgent) Card() *agentschema.AgentCard { return b.card }

// Config 返回当前配置。
func (b *BaseAgent) Config() interfaces.AgentConfig { return b.config }

// AbilityManager 返回能力管理器。
// 返回 any，调用方通过类型断言获取 *ability.AbilityManager。
func (b *BaseAgent) AbilityManager() any { return b.abilityManager }

// CallbackManager 返回回调管理器。
// 返回具体类型 *rail.AgentCallbackManager（通过 rail 包内 RailAgent 最小接口打破循环依赖）。
func (b *BaseAgent) CallbackManager() *rail.AgentCallbackManager { return b.callbackManager }

// AgentID 返回 Agent 唯一标识。
// 满足 rail.RailAgent 最小接口。
func (b *BaseAgent) AgentID() string {
	if b.card != nil {
		return b.card.ID
	}
	return ""
}

// RegisterCallback 注册回调。
// 委托给 AgentCallbackManager.RegisterCallback。
func (b *BaseAgent) RegisterCallback(ctx context.Context, event any, fn any, opts ...callback.CallbackOption) error {
	if b.callbackManager != nil {
		b.callbackManager.RegisterCallback(ctx, event.(rail.AgentCallbackEvent), fn.(callback.PerAgentCallbackFunc), opts...)
	}
	return nil
}

// RegisterRail 注册 Rail。
// 调用 rail.Init() 初始化后委托给 AgentCallbackManager.RegisterRail。
//
// 对应 Python: BaseAgent.register_rail(rail) → rail.init(self) → manager.register_rail(rail, self)
func (b *BaseAgent) RegisterRail(ctx context.Context, r rail.AgentRail, opts ...callback.CallbackOption) error {
	if b.callbackManager != nil {
		// 调用 Rail 初始化钩子（对齐 Python: rail.init(self)）
		if err := r.Init(b); err != nil {
			return err
		}
		return b.callbackManager.RegisterRail(ctx, r, opts...)
	}
	return nil
}

// UnregisterRail 注销 Rail。
// 委托给 AgentCallbackManager.UnregisterRail 后调用 rail.Uninit()。
//
// 对应 Python: BaseAgent.unregister_rail(rail) → manager.unregister_rail(rail, self) → rail.uninit(self)
func (b *BaseAgent) UnregisterRail(ctx context.Context, r rail.AgentRail) error {
	if b.callbackManager != nil {
		err := b.callbackManager.UnregisterRail(ctx, r)
		// 调用 Rail 注销钩子（对齐 Python: rail.uninit(self)）
		if uninitErr := r.Uninit(b); uninitErr != nil {
			if err == nil {
				return uninitErr
			}
			// 两个错误都存在时，返回注销错误，注销钩子错误记录日志
			logger.Error(logger.ComponentAgentCore).
				Str("event_type", "rail_uninit_error").
				Err(uninitErr).
				Msg("Rail Uninit 返回错误")
		}
		return err
	}
	return nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

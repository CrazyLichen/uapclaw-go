package rail

import (
	"context"

	cb "github.com/uapclaw/uapclaw-go/internal/agentcore/runner/callback"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// AgentCallbackManager PerAgent 实例级回调管理器。
//
// 对应 Python: AgentCallbackManager (openjiuwen/core/single_agent/agent_callback_manager.py)
// 不自持回调存储，将注册/触发委托给全局 CallbackFramework，
// 通过 "{agentID}_{event}" 前缀实现命名空间隔离。
type AgentCallbackManager struct {
	// agentID Agent 唯一标识，用于构造事件名前缀
	agentID string
}

// ──────────────────────────── 导出函数 ────────────────────────────

// NewAgentCallbackManager 创建回调管理器。
func NewAgentCallbackManager(agentID string) *AgentCallbackManager {
	return &AgentCallbackManager{agentID: agentID}
}

// RegisterCallback 注册回调。
//
// 对应 Python: AgentCallbackManager.register_callback(event, callback, priority)
// 委托给 CallbackFramework.OnPerAgent(agentEvent, fn, opts...)
func (m *AgentCallbackManager) RegisterCallback(ctx context.Context, event AgentCallbackEvent, fn cb.PerAgentCallbackFunc, opts ...cb.CallbackOption) {
	agentEvent := m.getAgentEvent(event)
	cb.GetCallbackFramework().OnPerAgent(agentEvent, fn, opts...)
}

// RegisterRail 批量注册一个 Rail 实例的所有回调。
//
// 对应 Python: AgentCallbackManager.register_rail(rail)
// 遍历 rail.GetCallbacks()，将每个钩子按 rail.Priority() 注册到 CallbackFramework。
func (m *AgentCallbackManager) RegisterRail(ctx context.Context, r AgentRail, opts ...cb.CallbackOption) error {
	callbacks := r.GetCallbacks()
	priorityOpt := cb.WithPriority(r.Priority())
	allOpts := append([]cb.CallbackOption{priorityOpt}, opts...)
	for event, fn := range callbacks {
		m.RegisterCallback(ctx, event, fn, allOpts...)
		logger.Debug(logComponent).
			Str("event_type", "rail_register_callback").
			Str("event", string(event)).
			Int("priority", r.Priority()).
			Msg("Rail 钩子注册到回调框架")
	}
	return nil
}

// UnregisterRail 批量注销一个 Rail 实例的所有回调。
//
// 对应 Python: AgentCallbackManager.unregister_rail(rail)
// 遍历 rail.GetCallbacks()，逐个注销。
func (m *AgentCallbackManager) UnregisterRail(_ context.Context, r AgentRail) error {
	callbacks := r.GetCallbacks()
	for event, fn := range callbacks {
		m.Unregister(event, fn)
		logger.Debug(logComponent).
			Str("event_type", "rail_unregister_callback").
			Str("event", string(event)).
			Msg("Rail 钩子从回调框架注销")
	}
	return nil
}

// Unregister 注销指定事件上的单个回调。
//
// 对应 Python: AgentCallbackManager.unregister(event, callback)
func (m *AgentCallbackManager) Unregister(event AgentCallbackEvent, fn cb.PerAgentCallbackFunc) {
	agentEvent := m.getAgentEvent(event)
	cb.GetCallbackFramework().OffPerAgent(agentEvent, fn)
}

// Clear 清除回调。不传 event 时清除所有事件的回调。
//
// 对应 Python: AgentCallbackManager.clear(event)
func (m *AgentCallbackManager) Clear(events ...AgentCallbackEvent) {
	fw := cb.GetCallbackFramework()
	if len(events) == 0 {
		for _, e := range AllCallbackEvents() {
			fw.OffAllPerAgent(m.getAgentEvent(e))
		}
		return
	}
	for _, e := range events {
		fw.OffAllPerAgent(m.getAgentEvent(e))
	}
}

// HasHooks 检查指定事件是否有已注册的回调。
//
// 对应 Python: AgentCallbackManager.has_hooks(event)
func (m *AgentCallbackManager) HasHooks(event AgentCallbackEvent) bool {
	agentEvent := m.getAgentEvent(event)
	return cb.GetCallbackFramework().HasPerAgentHooks(agentEvent)
}

// Execute 触发指定事件的所有回调。
//
// 对应 Python: AgentCallbackManager.execute(event, ctx)
// 委托给 CallbackFramework.TriggerPerAgent(ctx, agentEvent, railCtx)
func (m *AgentCallbackManager) Execute(ctx context.Context, event AgentCallbackEvent, railCtx *AgentCallbackContext) error {
	agentEvent := m.getAgentEvent(event)
	return cb.GetCallbackFramework().TriggerPerAgent(ctx, agentEvent, railCtx)
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// getAgentEvent 生成带 agentID 前缀的事件名。
//
// 对应 Python: AgentCallbackManager._get_agent_event(event)
// 返回格式: "{agentID}_{event}"，如 "agent1_before_model_call"
func (m *AgentCallbackManager) getAgentEvent(event AgentCallbackEvent) string {
	return m.agentID + "_" + string(event)
}

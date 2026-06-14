package state

import "github.com/uapclaw/uapclaw-go/internal/common/logger"

// ──────────────────────────── 结构体 ────────────────────────────

// AgentStateCollection Agent 会话状态集合。
//
// 组合 globalState（跨 Agent 共享的全局状态）和 agentState（当前 Agent 专属状态），
// 提供统一的状态读写接口。实现 State 接口。
//
// 对应 Python: openjiuwen/core/session/state/agent_state.py (StateCollection)
type AgentStateCollection struct {
	// globalState 全局状态（跨 Agent 共享）
	globalState *InMemoryState
	// agentState Agent 专属状态
	agentState *InMemoryState
}

// ──────────────────────────── 导出函数 ────────────────────────────

// NewAgentStateCollection 创建 Agent 状态集合实例。
func NewAgentStateCollection() *AgentStateCollection {
	logger.Info(logger.ComponentAgentCore).Str("action", "new_agent_state_collection").Msg("创建 Agent 状态集合")
	return &AgentStateCollection{
		globalState: NewInMemoryState(),
		agentState:  NewInMemoryState(),
	}
}

// ──────────────────────────── AgentStateCollection 方法 ────────────────────────────

// GetGlobal 从全局状态获取值。key 为零值时返回完整全局状态。
func (s *AgentStateCollection) GetGlobal(key StateKey) any {
	if key.IsZero() {
		return s.globalState.GetState()
	}
	return s.globalState.Get(key)
}

// UpdateGlobal 更新全局状态。
func (s *AgentStateCollection) UpdateGlobal(data map[string]any) {
	_ = s.globalState.Update(data)
}

// GetAgent 从 Agent 状态获取值。key 为零值时返回完整 Agent 状态。
func (s *AgentStateCollection) GetAgent(key StateKey) any {
	if key.IsZero() {
		return s.agentState.GetState()
	}
	return s.agentState.Get(key)
}

// Update 更新 Agent 状态。
func (s *AgentStateCollection) Update(data map[string]any) error {
	return s.agentState.Update(data)
}

// GetState 导出状态快照（用于检查点恢复）。
// 返回 {global_state: {...}, agent_state: {...}}。
func (s *AgentStateCollection) GetState() map[string]any {
	return map[string]any{
		GlobalStateKey: s.globalState.GetState(),
		AgentStateKey:  s.agentState.GetState(),
	}
}

// SetState 从快照恢复状态。
func (s *AgentStateCollection) SetState(st map[string]any) {
	if st == nil {
		return
	}
	if gs, ok := st[GlobalStateKey]; ok {
		if gm, ok := gs.(map[string]any); ok {
			s.globalState.SetState(gm)
		}
	}
	if as, ok := st[AgentStateKey]; ok {
		if am, ok := as.(map[string]any); ok {
			s.agentState.SetState(am)
		}
	}
}

// Dump 导出完整状态。
func (s *AgentStateCollection) Dump() map[string]any {
	return map[string]any{
		GlobalStateKey: s.globalState.GetState(),
		AgentStateKey:  s.agentState.GetState(),
	}
}

// GlobalState 返回底层全局状态的 InMemoryState 引用。
// 用于 AgentSession 创建 WorkflowSession 时传递全局状态。
func (s *AgentStateCollection) GlobalState() *InMemoryState {
	return s.globalState
}

// ──────────────────────────── State 接口实现 ────────────────────────────

// Get 根据 StateKey 获取状态值。委托到 agentState。
func (s *AgentStateCollection) Get(key StateKey) any {
	return s.agentState.Get(key)
}

// GetByPrefix 根据 key 和嵌套前缀获取状态值。委托到 agentState。
func (s *AgentStateCollection) GetByPrefix(key StateKey, nestedPrefix string) any {
	return s.agentState.GetByPrefix(key, nestedPrefix)
}

// GetByTransformer 通过转换函数获取状态值。委托到 agentState。
func (s *AgentStateCollection) GetByTransformer(transformer Transformer) any {
	return s.agentState.GetByTransformer(transformer)
}

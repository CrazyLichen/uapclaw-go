package state

import (
	"sync"

	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// AgentStateCollection Agent 会话状态集合。
//
// 组合 globalState（跨 Agent 共享的全局状态）和 agentState（当前 Agent 专属状态），
// 提供统一的状态读写接口。实现 SessionState 接口。
//
// 对应 Python: openjiuwen/core/session/state/agent_state.py (StateCollection)
type AgentStateCollection struct {
	// mu 并发读写锁
	mu sync.RWMutex
	// globalState 全局状态（跨 Agent 共享）
	globalState *InMemoryStateLike
	// agentState Agent 专属状态
	agentState *InMemoryStateLike
	// traceState 追踪状态
	traceState map[string]any
}

// ──────────────────────────── 导出函数 ────────────────────────────

// NewAgentStateCollection 创建 Agent 状态集合实例。
func NewAgentStateCollection() *AgentStateCollection {
	logger.Info(logger.ComponentAgentCore).Str("action", "new_agent_state_collection").Msg("创建 Agent 状态集合")
	return &AgentStateCollection{
		globalState: NewInMemoryStateLike(),
		agentState:  NewInMemoryStateLike(),
		traceState:  make(map[string]any),
	}
}

// GetGlobal 从全局状态获取值。key 为零值时返回完整全局状态。
func (s *AgentStateCollection) GetGlobal(key StateKey) any {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if key.IsZero() {
		return s.globalState.GetState()
	}
	return s.globalState.Get(key)
}

// UpdateGlobal 更新全局状态。
func (s *AgentStateCollection) UpdateGlobal(data map[string]any) {
	s.mu.Lock()
	defer s.mu.Unlock()
	_ = s.globalState.Update(data)
}

// UpdateTrace 更新追踪状态。Agent 层为空实现，与 Python 一致。
func (s *AgentStateCollection) UpdateTrace(span any) {
	// Agent 层的 trace 是空实现，与 Python 一致
}

// Update 更新 Agent 状态。
func (s *AgentStateCollection) Update(data map[string]any) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.agentState.Update(data)
}

// Get 根据 StateKey 获取状态值。委托到 agentState。
func (s *AgentStateCollection) Get(key StateKey) any {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.agentState.Get(key)
}

// Dump 导出完整状态快照（含 trace_state）。
func (s *AgentStateCollection) Dump() map[string]any {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return map[string]any{
		GlobalStateKey: s.globalState.GetState(),
		AgentStateKey:  s.agentState.GetState(),
		"trace_state":  s.traceState,
	}
}

// GetState 导出状态快照（用于检查点恢复）。
// 返回 {global_state: {...}, agent_state: {...}}。
func (s *AgentStateCollection) GetState() map[string]any {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return map[string]any{
		GlobalStateKey: s.globalState.GetState(),
		AgentStateKey:  s.agentState.GetState(),
	}
}

// SetState 从快照恢复状态。
func (s *AgentStateCollection) SetState(st map[string]any) {
	s.mu.Lock()
	defer s.mu.Unlock()
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

// GetByPrefix 根据 key 和嵌套前缀获取状态值。委托到 agentState。
func (s *AgentStateCollection) GetByPrefix(key StateKey, nestedPrefix string) any {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.agentState.GetByPrefix(key, nestedPrefix)
}

// GetByTransformer 通过转换函数获取状态值。委托到 agentState。
func (s *AgentStateCollection) GetByTransformer(transformer Transformer) any {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.agentState.GetByTransformer(transformer)
}

// GetAgent 从 Agent 状态获取值。key 为零值时返回完整 Agent 状态。
func (s *AgentStateCollection) GetAgent(key StateKey) any {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if key.IsZero() {
		return s.agentState.GetState()
	}
	return s.agentState.Get(key)
}

// GlobalState 返回底层全局状态的 InMemoryStateLike 引用。
// 用于 AgentSession 创建 WorkflowSession 时传递全局状态。
func (s *AgentStateCollection) GlobalState() *InMemoryStateLike {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.globalState
}

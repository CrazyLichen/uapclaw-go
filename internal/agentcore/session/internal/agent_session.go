package internal

import (
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/checkpointer"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/state"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// AgentSession Agent 内部会话，实现 BaseSession 接口。
//
// 持有会话运行所需的基础设施组件（配置、状态、追踪器、流写入管理器、检查点器），
// 是纯粹的组件容器，不包含业务逻辑。业务逻辑由公开层 Session 负责。
//
// 对应 Python: openjiuwen/core/session/internal/agent.py (AgentSession)
type AgentSession struct {
	// sessionID 会话唯一标识
	sessionID string
	// config 会话配置
	// ⤵️ 5.12 回填：any → SessionConfig
	config any
	// state 会话状态（AgentStateCollection）
	state state.SessionState
	// tracer 追踪器
	// ⤵️ 5.11 回填：any → Tracer
	tracer any
	// streamWriterManager 流写入管理器
	// ⤵️ 5.10 回填：any → StreamWriterManager
	streamWriterManager any
	// checkpointer 检查点器
	checkpointer checkpointer.Checkpointer
	// agentSpan Agent 追踪跨度
	agentSpan any
	// card Agent 身份元数据
	// ⤵️ 后续回填：any → *schema.AgentCard
	card any
}

// ──────────────────────────── 枚举 ────────────────────────────

// AgentSessionOption AgentSession 构造选项函数类型
type AgentSessionOption func(*AgentSession)

// ──────────────────────────── 导出函数 ────────────────────────────

// NewAgentSession 创建内部 AgentSession 实例。
//
// 默认创建 AgentStateCollection 作为状态存储。
// 可通过选项函数注入各基础设施组件。
func NewAgentSession(sessionID string, opts ...AgentSessionOption) *AgentSession {
	logger.Info(logger.ComponentAgentCore).
		Str("action", "new_agent_session").
		Str("session_id", sessionID).
		Msg("创建内部 AgentSession")

	s := &AgentSession{
		sessionID: sessionID,
		state:     state.NewAgentStateCollection(),
	}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

// WithConfig 设置会话配置的选项
func WithConfig(config any) AgentSessionOption {
	return func(s *AgentSession) {
		s.config = config
	}
}

// WithState 设置会话状态的选项
func WithState(st state.SessionState) AgentSessionOption {
	return func(s *AgentSession) {
		s.state = st
	}
}

// WithTracer 设置追踪器的选项
func WithTracer(tracer any) AgentSessionOption {
	return func(s *AgentSession) {
		s.tracer = tracer
	}
}

// WithStreamWriterManager 设置流写入管理器的选项
func WithStreamWriterManager(mgr any) AgentSessionOption {
	return func(s *AgentSession) {
		s.streamWriterManager = mgr
	}
}

// WithCheckpointer 设置检查点器的选项
func WithCheckpointer(cp checkpointer.Checkpointer) AgentSessionOption {
	return func(s *AgentSession) {
		s.checkpointer = cp
	}
}

// WithCard 设置 Agent 身份元数据的选项
func WithCard(card any) AgentSessionOption {
	return func(s *AgentSession) {
		s.card = card
	}
}

// WithAgentSpan 设置 Agent 追踪跨度的选项
func WithAgentSpan(span any) AgentSessionOption {
	return func(s *AgentSession) {
		s.agentSpan = span
	}
}

// Config 获取会话配置
func (s *AgentSession) Config() any {
	return s.config
}

// State 获取会话状态
func (s *AgentSession) State() state.SessionState {
	return s.state
}

// Tracer 获取追踪器
func (s *AgentSession) Tracer() any {
	return s.tracer
}

// StreamWriterManager 获取流写入管理器
func (s *AgentSession) StreamWriterManager() any {
	return s.streamWriterManager
}

// SessionID 获取会话唯一标识
func (s *AgentSession) SessionID() string {
	return s.sessionID
}

// Checkpointer 获取检查点管理器
func (s *AgentSession) Checkpointer() checkpointer.Checkpointer {
	return s.checkpointer
}

// ActorManager 获取 Actor 管理器（当前始终返回 nil）
func (s *AgentSession) ActorManager() any {
	return nil
}

// Close 关闭会话（当前为空实现，返回 nil）
func (s *AgentSession) Close() error {
	return nil
}

// Card 获取 Agent 身份元数据
func (s *AgentSession) Card() any {
	return s.card
}

// AgentSpan 获取 Agent 追踪跨度
func (s *AgentSession) AgentSpan() any {
	return s.agentSpan
}

// ──────────────────────────── 非导出函数 ────────────────────────────

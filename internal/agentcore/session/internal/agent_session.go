package internal

import (
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/checkpointer"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/state"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	"github.com/uapclaw/uapclaw-go/internal/common/schema"
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
	card *schema.AgentCard
}

// ──────────────────────────── 枚举 ────────────────────────────

// AgentSessionOption AgentSession 构造选项函数类型
type AgentSessionOption func(*AgentSession)

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// NewAgentSession 创建内部 AgentSession 实例。
//
// 默认行为（对齐 Python AgentSession.__init__）：
//   - state: 自动创建 AgentStateCollection（Python: StateCollection()）
//   - config: 不自动创建，由外层 Session 传入（Python: 外层总创建 Config()）
//   - checkpointer: nil 时从全局工厂获取（Python: CheckpointerFactory.get_checkpointer()）
//   - streamWriterManager: nil 时留空（⤵️ 5.10 回填：自动创建 StreamWriterManager(StreamEmitter())）
//   - tracer: nil 时留空（⤵️ 5.11 回填：自动创建 Tracer() + init(swm)）
//   - agentSpan: nil 时留空（⤵️ 5.11 回填：自动创建 tracer.create_agent_span()）
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

	// 默认值处理（对齐 Python AgentSession.__init__）：

	// checkpointer: nil 时从全局工厂获取
	// Python: self._checkpointer = CheckpointerFactory.get_checkpointer() if checkpointer is None else checkpointer
	if s.checkpointer == nil {
		s.checkpointer = checkpointer.GetCheckpointer()
	}

	// streamWriterManager: nil 时自动创建默认实例
	// Python: self._stream_writer_manager = StreamWriterManager(StreamEmitter()) if stream_writer_manager is None else stream_writer_manager
	// ⤵️ 5.10 回填：StreamWriterManager 包实现后，取消下面的注释
	// if s.streamWriterManager == nil {
	// 	s.streamWriterManager = stream.NewStreamWriterManager(stream.NewStreamEmitter())
	// }

	// tracer: nil 时自动创建并初始化
	// Python: tracer = Tracer(); tracer.init(self._stream_writer_manager); self._tracer = tracer
	// ⤵️ 5.11 回填：Tracer 包实现后，取消下面的注释
	// if s.tracer == nil {
	// 	s.tracer = tracer.NewTracer()
	// 	s.tracer.Init(s.streamWriterManager)
	// }

	// agentSpan: 从 tracer 创建
	// Python: self._agent_span = self._tracer.tracer_agent_span_manager.create_agent_span() if self._tracer else None
	// ⤵️ 5.11 回填：Tracer 包实现后，取消下面的注释
	// if s.agentSpan == nil && s.tracer != nil {
	// 	s.agentSpan = s.tracer.CreateAgentSpan()
	// }

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
func WithCard(card *schema.AgentCard) AgentSessionOption {
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
func (s *AgentSession) Card() *schema.AgentCard {
	return s.card
}

// AgentID 获取 Agent ID，满足 checkpointer.AgentIDProvider 接口。
//
// 优先级链（对齐 Python AgentSession.agent_id()）：
//  1. config.get_agent_config().id（⤵️ 5.12 回填：config 类型确定后实现）
//  2. card.AbilityID()（当前走此分支）
//
// Python 原始逻辑：
//
//	def agent_id(self):
//	    agent_config = self._config.get_agent_config()
//	    if agent_config is not None:
//	        return agent_config.id
//	    return self._card.id
func (s *AgentSession) AgentID() string {
	// ⤵️ 5.12 回填：从 config 获取 agent_config.id
	// if s.config != nil {
	//     if agentConfig, ok := s.config.(AgentConfigProvider); ok {
	//         if ac := agentConfig.GetAgentConfig(); ac != nil {
	//             if id := ac.ID(); id != "" {
	//                 return id
	//             }
	//         }
	//     }
	// }
	if s.card != nil {
		return s.card.AbilityID()
	}
	return ""
}

// AgentSpan 获取 Agent 追踪跨度
func (s *AgentSession) AgentSpan() any {
	return s.agentSpan
}

// ──────────────────────────── 非导出函数 ────────────────────────────

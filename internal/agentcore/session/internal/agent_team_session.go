package internal

import (
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/checkpointer"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/config"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/interfaces"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/state"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/stream"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/tracer"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// AgentTeamSession AgentTeam 内部会话，实现 InnerSession 和 TeamIDProvider 接口。
//
// 持有 AgentTeam 会话运行所需的基础设施组件（配置、状态、追踪器、流写入管理器、检查点器），
// 是纯粹的组件容器，不包含业务逻辑。业务逻辑由公开层 Session 负责。
//
// 对应 Python: openjiuwen/core/session/internal/agent_team.py (AgentTeamSession)
type AgentTeamSession struct {
	// sessionID 会话唯一标识
	sessionID string
	// teamID 团队唯一标识
	teamID string
	// config 会话配置
	config config.SessionConfig
	// state 会话状态（AgentStateCollection）
	state state.SessionState
	// streamWriterManager 流写入管理器
	streamWriterManager *stream.StreamWriterManager
	// tracer 追踪器
	tracer *tracer.Tracer
	// checkpointer 检查点器
	checkpointer interfaces.Checkpointer
	// teamSpan 团队追踪跨度
	teamSpan *tracer.TraceAgentSpan
}

// AgentTeamSessionOption AgentTeamSession 构造选项函数类型
type AgentTeamSessionOption func(*AgentTeamSession)

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// 编译时检查接口实现
var (
	_ interfaces.InnerSession   = (*AgentTeamSession)(nil)
	_ interfaces.TeamIDProvider = (*AgentTeamSession)(nil)
)

// ──────────────────────────── 导出函数 ────────────────────────────

// NewAgentTeamSession 创建内部 AgentTeamSession 实例。
//
// 默认行为（对齐 Python AgentTeamSession.__init__）：
//   - state: 自动创建 AgentStateCollection（Python: StateCollection()）
//   - config: 不自动创建，由外层 Session 传入（Python: 外层总创建 Config()）
//   - checkpointer: nil 时从全局工厂获取（Python: CheckpointerFactory.get_checkpointer()）
//   - streamWriterManager: nil 时自动创建 StreamWriterManager(StreamEmitter())
//   - tracer: nil 时自动创建 Tracer() + init(swm)
//   - teamSpan: nil 时从 tracer.AgentSpanManager.CreateAgentSpan() 创建
func NewAgentTeamSession(sessionID, teamID string, opts ...AgentTeamSessionOption) *AgentTeamSession {
	logger.Info(logger.ComponentAgentCore).
		Str("action", "new_agent_team_session").
		Str("session_id", sessionID).
		Str("team_id", teamID).
		Msg("创建内部 AgentTeamSession")

	s := &AgentTeamSession{
		sessionID: sessionID,
		teamID:    teamID,
		state:     state.NewAgentStateCollection(),
	}
	for _, opt := range opts {
		opt(s)
	}

	// 默认值处理（对齐 Python AgentTeamSession.__init__）：

	// checkpointer: nil 时从全局工厂获取
	// 对齐 Python：self._checkpointer = CheckpointerFactory.get_checkpointer() if checkpointer is None else checkpointer
	if s.checkpointer == nil {
		s.checkpointer = checkpointer.GetCheckpointer()
	}

	// streamWriterManager: nil 时自动创建默认实例
	// 对齐 Python：self._stream_writer_manager = StreamWriterManager(StreamEmitter()) if stream_writer_manager is None else stream_writer_manager
	if s.streamWriterManager == nil {
		s.streamWriterManager = stream.NewStreamWriterManager(stream.NewStreamEmitter())
	}

	// tracer: nil 时自动创建并初始化
	// 对齐 Python：tracer = Tracer(); tracer.init(self._stream_writer_manager); self._tracer = tracer
	if s.tracer == nil {
		s.tracer = tracer.NewTracer()
		s.tracer.Init(s.streamWriterManager)
	}

	// teamSpan: 从 tracer 创建
	// 对齐 Python：self._team_span = self._tracer.tracer_agent_span_manager.create_agent_span() if self._tracer else None
	if s.teamSpan == nil && s.tracer != nil {
		s.teamSpan = s.tracer.AgentSpanManager.CreateAgentSpan()
	}

	return s
}

// WithTeamConfig 设置会话配置的选项
func WithTeamConfig(config config.SessionConfig) AgentTeamSessionOption {
	return func(s *AgentTeamSession) {
		s.config = config
	}
}

// WithTeamState 设置会话状态的选项
func WithTeamState(st state.SessionState) AgentTeamSessionOption {
	return func(s *AgentTeamSession) {
		s.state = st
	}
}

// WithTeamTracer 设置追踪器的选项
func WithTeamTracer(t *tracer.Tracer) AgentTeamSessionOption {
	return func(s *AgentTeamSession) {
		s.tracer = t
	}
}

// WithTeamStreamWriterManager 设置流写入管理器的选项
func WithTeamStreamWriterManager(mgr *stream.StreamWriterManager) AgentTeamSessionOption {
	return func(s *AgentTeamSession) {
		s.streamWriterManager = mgr
	}
}

// WithTeamCheckpointer 设置检查点器的选项
func WithTeamCheckpointer(cp interfaces.Checkpointer) AgentTeamSessionOption {
	return func(s *AgentTeamSession) {
		s.checkpointer = cp
	}
}

// WithTeamSpan 设置团队追踪跨度的选项
func WithTeamSpan(span *tracer.TraceAgentSpan) AgentTeamSessionOption {
	return func(s *AgentTeamSession) {
		s.teamSpan = span
	}
}

// Config 获取会话配置
func (s *AgentTeamSession) Config() config.SessionConfig {
	return s.config
}

// State 获取会话状态
func (s *AgentTeamSession) State() state.SessionState {
	return s.state
}

// Tracer 获取追踪器
func (s *AgentTeamSession) Tracer() *tracer.Tracer {
	return s.tracer
}

// StreamWriterManager 获取流写入管理器
func (s *AgentTeamSession) StreamWriterManager() *stream.StreamWriterManager {
	return s.streamWriterManager
}

// SessionID 获取会话唯一标识
func (s *AgentTeamSession) SessionID() string {
	return s.sessionID
}

// Checkpointer 获取检查点管理器
func (s *AgentTeamSession) Checkpointer() interfaces.Checkpointer {
	return s.checkpointer
}

// ActorManager 获取 Actor 管理器（当前始终返回 nil）
func (s *AgentTeamSession) ActorManager() any {
	return nil
}

// Close 关闭会话（当前为空实现，返回 nil）
func (s *AgentTeamSession) Close() error {
	return nil
}

// TeamID 获取团队唯一标识，满足 TeamIDProvider 接口。
//
// 对齐 Python AgentTeamSession.team_id()
func (s *AgentTeamSession) TeamID() string {
	return s.teamID
}

// TeamSpan 获取团队追踪跨度
func (s *AgentTeamSession) TeamSpan() *tracer.TraceAgentSpan {
	return s.teamSpan
}

// ──────────────────────────── 非导出函数 ────────────────────────────

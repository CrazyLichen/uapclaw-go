package session

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/runner/callback"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/checkpointer"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/config"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/interfaces"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/internal"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/state"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/stream"
	agentschema "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/schema"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// AgentTeamSession Agent 团队公开会话，实现 SessionFacade 接口。
//
// 组合内部层 AgentTeamSession，提供 PreRun→Execute→PostRun 完整生命周期。
// 负责：状态读写、流写入、检查点持久化。
// 与 Agent Session 不同：Interact 返回错误（team session 不支持交互），
// 不注册到 controller（无需从磁盘恢复）。
//
// 对应 Python: openjiuwen/core/session/agent_team.py (Session)
type AgentTeamSession struct {
	// sessionID 会话唯一标识
	sessionID string
	// teamID 团队唯一标识
	teamID string
	// inner 内部 AgentTeamSession 实例
	inner *internal.AgentTeamSession
	// envs 环境变量（通过 WithAgentTeamEnvs 设置）
	envs map[string]any
	// checkpointer 检查点器（通过 WithAgentTeamCheckpointer option 设置）
	checkpointer checkpointer.Checkpointer
	// streamWriterManager 流写入管理器（通过 WithAgentTeamStreamWriterManager 设置）
	streamWriterManager *stream.StreamWriterManager
	// preRunDone PreRun 是否已执行
	preRunDone bool
	// postRunDone PostRun 是否已执行
	postRunDone bool
}

// ──────────────────────────── 枚举 ────────────────────────────

// AgentTeamSessionOption AgentTeamSession 构造选项函数类型
type AgentTeamSessionOption func(*AgentTeamSession)

// ──────────────────────────── 全局变量 ────────────────────────────

// 编译时检查 *AgentTeamSession 满足 SessionFacade 接口
var _ interfaces.SessionFacade = (*AgentTeamSession)(nil)

// ──────────────────────────── 导出函数 ────────────────────────────

// NewAgentTeamSession 创建公开层 AgentTeamSession 实例。
//
// 默认行为（对齐 Python Session.__init__）：
//   - sessionID: 若未指定，自动生成 UUID
//   - teamID: 默认 "agent_team"
//   - config: 创建默认 Config 并设置 envs，传入 inner
//   - checkpointer: 若未通过 WithAgentTeamCheckpointer 设置，使用 checkpointer.GetCheckpointer()
//   - streamWriterManager: 若未通过 WithAgentTeamStreamWriterManager 设置，传 nil 给 inner
//     （inner 会自动创建默认实例）
func NewAgentTeamSession(opts ...AgentTeamSessionOption) *AgentTeamSession {
	s := &AgentTeamSession{
		sessionID: uuid.New().String(),
		teamID:    "agent_team",
		envs:      make(map[string]any),
	}
	for _, opt := range opts {
		opt(s)
	}

	// 1. config：外层创建 SessionConfig + SetEnvs，传给 inner
	cfg := config.NewSessionConfig(context.Background())
	if len(s.envs) > 0 {
		cfg.SetEnvs(s.envs)
	}

	// 2. checkpointer：未设置时从全局工厂获取
	cp := s.checkpointer
	if cp == nil {
		cp = checkpointer.GetCheckpointer()
	}

	// 3. streamWriterManager：外层透传给 inner
	//    未设置时传 nil，由 inner 自动创建默认实例

	s.inner = internal.NewAgentTeamSession(s.sessionID, s.teamID,
		internal.WithTeamConfig(cfg),
		internal.WithTeamCheckpointer(cp),
		internal.WithTeamStreamWriterManager(s.streamWriterManager),
	)

	logger.Info(logger.ComponentAgentCore).
		Str("action", "new_agent_team_session").
		Str("session_id", s.sessionID).
		Str("team_id", s.teamID).
		Msg("创建公开层 AgentTeamSession")

	return s
}

// WithAgentTeamSessionID 设置会话 ID 的选项
func WithAgentTeamSessionID(id string) AgentTeamSessionOption {
	return func(s *AgentTeamSession) {
		s.sessionID = id
	}
}

// WithAgentTeamTeamID 设置团队 ID 的选项
func WithAgentTeamTeamID(teamID string) AgentTeamSessionOption {
	return func(s *AgentTeamSession) {
		s.teamID = teamID
	}
}

// WithAgentTeamEnvs 设置环境变量的选项。
// 外层 NewAgentTeamSession 会创建默认 Config 并将 envs 写入，再传给 inner。
func WithAgentTeamEnvs(envs map[string]any) AgentTeamSessionOption {
	return func(s *AgentTeamSession) {
		s.envs = envs
	}
}

// WithAgentTeamCheckpointer 设置检查点器的选项。
// 若不设置，NewAgentTeamSession 默认注入 checkpointer.GetCheckpointer()。
func WithAgentTeamCheckpointer(cp checkpointer.Checkpointer) AgentTeamSessionOption {
	return func(s *AgentTeamSession) {
		s.checkpointer = cp
	}
}

// WithAgentTeamStreamWriterManager 设置流写入管理器的选项。
// 外层透传给 inner，由 inner 自动创建默认实例。
func WithAgentTeamStreamWriterManager(mgr *stream.StreamWriterManager) AgentTeamSessionOption {
	return func(s *AgentTeamSession) {
		s.streamWriterManager = mgr
	}
}

// CreateAgentTeamSession 通过指定参数创建 AgentTeamSession 的工厂函数。
//
// 对齐 Python: Session(session_id, envs, team_id)
func CreateAgentTeamSession(sessionID string, envs map[string]any, teamID string) *AgentTeamSession {
	opts := []AgentTeamSessionOption{WithAgentTeamSessionID(sessionID)}
	if teamID != "" {
		opts = append(opts, WithAgentTeamTeamID(teamID))
	}
	if envs != nil {
		opts = append(opts, WithAgentTeamEnvs(envs))
	}
	return NewAgentTeamSession(opts...)
}

// GetSessionID 返回会话唯一标识
func (s *AgentTeamSession) GetSessionID() string {
	return s.sessionID
}

// GetTeamID 返回团队唯一标识
func (s *AgentTeamSession) GetTeamID() string {
	return s.teamID
}

// UpdateState 更新全局状态，委托到 inner.State() 的 SessionState
func (s *AgentTeamSession) UpdateState(data map[string]any) {
	s.inner.State().UpdateGlobal(data)
}

// GetState 获取全局状态值，委托到 inner.State() 的 SessionState
func (s *AgentTeamSession) GetState(key state.StateKey) (any, error) {
	return s.inner.State().GetGlobal(key), nil
}

// DumpState 导出完整状态快照，委托到 inner.State() 的 SessionState
func (s *AgentTeamSession) DumpState() map[string]any {
	return s.inner.State().Dump()
}

// WriteStream 写入标准输出流。
//
// SessionFacade 接口实现。
// 对应 Python: Session.write_stream(data)
func (s *AgentTeamSession) WriteStream(ctx context.Context, data any) error {
	return s.writeStream(data)
}

// WriteCustomStream 写入自定义流。
//
// SessionFacade 接口实现。
// 对应 Python: Session.write_custom_stream(data)
func (s *AgentTeamSession) WriteCustomStream(ctx context.Context, data any) error {
	return s.writeCustomStream(data)
}

// GetEnv 获取环境变量值。
// 对应 Python: Session.get_env(key, default) → self._inner.config().get_env(key, default)
func (s *AgentTeamSession) GetEnv(key string, defaultValue ...any) any {
	cfg := s.inner.Config()
	if cfg == nil {
		return nil
	}
	return cfg.GetEnv(key, defaultValue...)
}

// GetEnvs 获取所有环境变量。
// 对应 Python: Session.get_envs() → self._inner.config().get_envs()
func (s *AgentTeamSession) GetEnvs() map[string]any {
	cfg := s.inner.Config()
	if cfg == nil {
		return nil
	}
	return cfg.GetEnvs()
}

// Interact 团队会话不支持交互，始终返回错误。
// 对应 Python: raise ValueError("team session does not support interact")
func (s *AgentTeamSession) Interact(ctx context.Context, value any) error {
	return fmt.Errorf("team session does not support interact")
}

// PreRun 会话预运行：检查点 PreAgentTeamExecute。
//
// 幂等：多次调用只执行一次。
//
// 对应 Python: Session.pre_run()
func (s *AgentTeamSession) PreRun(ctx context.Context, inputs ...map[string]any) error {
	if s.preRunDone {
		return nil
	}

	// 检查点预执行
	if cp := s.inner.Checkpointer(); cp != nil {
		var inputVal any
		if len(inputs) > 0 {
			inputVal = inputs[0]
		}
		if err := cp.PreAgentTeamExecute(ctx, s.inner, inputVal); err != nil {
			logger.Error(logger.ComponentAgentCore).Err(err).
				Str("action", "agent_team_pre_run").
				Str("session_id", s.GetSessionID()).
				Str("team_id", s.GetTeamID()).
				Msg("AgentTeamSession PreRun 检查点失败")
			return err
		}
	}

	s.preRunDone = true
	logger.Info(logger.ComponentAgentCore).
		Str("action", "agent_team_pre_run").
		Str("session_id", s.GetSessionID()).
		Str("team_id", s.GetTeamID()).
		Msg("AgentTeamSession PreRun 完成")

	return nil
}

// PostRun 会话后运行：关闭流 + 提交检查点。
//
// 幂等：多次调用只执行一次。
//
// 对应 Python: Session.post_run()
func (s *AgentTeamSession) PostRun(ctx context.Context) error {
	if s.postRunDone {
		return nil
	}

	// 关闭流
	_ = s.CloseStream()

	// 提交检查点
	if err := s.Commit(ctx); err != nil {
		s.postRunDone = true
		logger.Error(logger.ComponentAgentCore).Err(err).
			Str("action", "agent_team_post_run").
			Str("session_id", s.GetSessionID()).
			Str("team_id", s.GetTeamID()).
			Msg("AgentTeamSession PostRun Commit 失败")
		return err
	}

	s.postRunDone = true
	logger.Info(logger.ComponentAgentCore).
		Str("action", "agent_team_post_run").
		Str("session_id", s.GetSessionID()).
		Str("team_id", s.GetTeamID()).
		Msg("AgentTeamSession PostRun 完成")

	return nil
}

// Commit 提交当前状态到检查点（不关闭流）。
// 对应 Python: Session.commit()
func (s *AgentTeamSession) Commit(ctx context.Context) error {
	if cp := s.inner.Checkpointer(); cp != nil {
		return cp.PostAgentTeamExecute(ctx, s.inner)
	}
	return nil
}

// FlushCheckpoint 等价 Commit，刷新检查点到存储。
// 对应 Python: Session.flush_checkpoint() → Session.commit()
func (s *AgentTeamSession) FlushCheckpoint(ctx context.Context) error {
	return s.Commit(ctx)
}

// CloseStream 关闭流发射器。
// 对应 Python: Session.close_stream()
func (s *AgentTeamSession) CloseStream() error {
	ctx := context.Background()
	mgr := s.inner.StreamWriterManager()
	if mgr == nil {
		return nil
	}
	// 关闭 emitter，发送 END_FRAME
	_ = mgr.StreamEmitter().Close(ctx)

	// 注销该 session 的 StreamWrite 全部回调
	callback.GetCallbackFramework().OffAllCustom(s.GetSessionID() + "write_stream")
	return nil
}

// CreateAgentSession 创建子 AgentSession。
//
// 从当前 AgentTeamSession 的 sessionID 创建子 Agent 会话，
// 子会话共享同一个 sessionID。
//
// 对应 Python: Session.create_agent_session(card, agent_id)
func (s *AgentTeamSession) CreateAgentSession(card *agentschema.AgentCard, agentID string) *Session {
	return NewSession(
		WithSessionID(s.sessionID),
		WithCard(card),
	)
}

// Inner 返回内部层 AgentTeamSession
func (s *AgentTeamSession) Inner() *internal.AgentTeamSession {
	return s.inner
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// writeStream 写入标准输出流（内部实现）。
// 对应 Python: Session.write_stream(data)
// data 接受 any 类型，内部通过 normalizeOutputStream 统一转为 OutputSchema。
func (s *AgentTeamSession) writeStream(data any) error {
	ctx := context.Background()
	streamData := normalizeOutputStream(s.tagStreamPayload(data))

	// 触发自定义 StreamWrite 事件
	callback.GetCallbackFramework().TriggerCustom(ctx,
		s.GetSessionID()+"write_stream",
		map[string]any{"data": streamData},
	)

	mgr := s.inner.StreamWriterManager()
	if mgr == nil {
		return nil
	}
	writer := mgr.GetOutputWriter()
	if writer == nil {
		return nil
	}
	return writer.Write(ctx, streamData)
}

// writeCustomStream 写入自定义流（内部实现）。
// 对应 Python: Session.write_custom_stream(data)
func (s *AgentTeamSession) writeCustomStream(data any) error {
	ctx := context.Background()
	streamData := s.tagStreamPayload(data)

	// 触发自定义 StreamWrite 事件
	callback.GetCallbackFramework().TriggerCustom(ctx,
		s.GetSessionID()+"write_stream",
		map[string]any{"data": streamData},
	)

	mgr := s.inner.StreamWriterManager()
	if mgr == nil {
		return nil
	}
	writer := mgr.GetCustomWriter()
	if writer == nil {
		return nil
	}
	schema := normalizeCustomStream(streamData)
	return writer.Write(ctx, schema)
}

// tagStreamPayload 为流数据添加来源元数据。
// 对应 Python: Session._tag_stream_payload(data)
func (s *AgentTeamSession) tagStreamPayload(data any) any {
	// AgentTeamSession 当前不持有 sourceMetadata，直接返回
	return data
}

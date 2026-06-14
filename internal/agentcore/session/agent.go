package session

import (
	"context"

	"github.com/google/uuid"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/runner/callback"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/interaction"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/internal"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/state"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// Session Agent 公开会话，提供用户面向的 API。
//
// 组合内部层 AgentSession，实现 PreRun→Invoke/Stream→PostRun 完整生命周期。
// 负责：状态读写、流写入、交互、回调触发、检查点持久化。
//
// 对应 Python: openjiuwen/core/session/agent.py (Session)
type Session struct {
	// inner 内部 AgentSession 实例
	inner *internal.AgentSession
	// card Agent 身份元数据
	// ⤵️ 后续回填：any → *schema.AgentCard
	card any
	// preRunDone PreRun 是否已执行
	preRunDone bool
	// postRunDone PostRun 是否已执行
	postRunDone bool
	// closeStreamOnPostRun PostRun 时是否自动关闭流
	closeStreamOnPostRun bool
	// interaction 交互实例（懒初始化）
	interaction *interaction.SimpleAgentInteraction
	// sourceMetadata 流数据来源元数据
	sourceMetadata map[string]any
}

// ──────────────────────────── 枚举 ────────────────────────────

// SessionOption Session 构造选项函数类型
type SessionOption func(*Session)

// ──────────────────────────── 导出函数 ────────────────────────────

// NewSession 创建公开层 Session 实例。
//
// 若未指定 sessionID，自动生成 UUID。
// 可通过选项函数注入各组件和配置。
//
// 对应 Python: openjiuwen/core/session/agent.py create_agent_session()
func NewSession(opts ...SessionOption) *Session {
	sessionID := uuid.New().String()

	logger.Info(logger.ComponentAgentCore).
		Str("action", "new_session").
		Str("session_id", sessionID).
		Msg("创建公开层 Session")

	s := &Session{
		inner:                internal.NewAgentSession(sessionID),
		closeStreamOnPostRun: true,
		sourceMetadata:       make(map[string]any),
	}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

// WithSessionID 设置会话 ID 的选项
func WithSessionID(id string) SessionOption {
	return func(s *Session) {
		s.inner = internal.NewAgentSession(id)
	}
}

// WithCard 设置 Agent 身份元数据的选项
func WithCard(card any) SessionOption {
	return func(s *Session) {
		s.card = card
	}
}

// WithCloseStreamOnPostRun 设置 PostRun 时是否自动关闭流的选项
func WithCloseStreamOnPostRun(v bool) SessionOption {
	return func(s *Session) {
		s.closeStreamOnPostRun = v
	}
}

// WithSourceMetadata 设置流数据来源元数据的选项
func WithSourceMetadata(meta map[string]any) SessionOption {
	return func(s *Session) {
		s.sourceMetadata = meta
	}
}

// ──────────────────────────── 身份/配置方法 ────────────────────────────

// GetSessionID 返回会话唯一标识
func (s *Session) GetSessionID() string {
	return s.inner.SessionID()
}

// GetEnv 获取环境变量值
// ⤵️ 5.12 回填：Config() 返回真实类型后实现
func (s *Session) GetEnv(key string, defaultValue ...any) any {
	return nil
}

// GetEnvs 获取所有环境变量
// ⤵️ 5.12 回填：Config() 返回真实类型后实现
func (s *Session) GetEnvs() map[string]any {
	return nil
}

// GetAgentID 返回 Agent ID
// ⤵️ 后续回填：card 类型从 any → *schema.AgentCard 后实现
func (s *Session) GetAgentID() string {
	return ""
}

// GetAgentName 返回 Agent 名称
// ⤵️ 后续回填：card 类型从 any → *schema.AgentCard 后实现
func (s *Session) GetAgentName() string {
	return ""
}

// GetAgentDescription 返回 Agent 描述
// ⤵️ 后续回填：card 类型从 any → *schema.AgentCard 后实现
func (s *Session) GetAgentDescription() string {
	return ""
}

// ──────────────────────────── 状态读写方法 ────────────────────────────

// UpdateState 更新全局状态，委托到 inner.State() 的 AgentStateCollection
func (s *Session) UpdateState(data map[string]any) {
	if coll, ok := s.inner.State().(*state.AgentStateCollection); ok {
		coll.UpdateGlobal(data)
	}
}

// GetState 获取全局状态值，委托到 inner.State() 的 AgentStateCollection
func (s *Session) GetState(key state.StateKey) (any, error) {
	if coll, ok := s.inner.State().(*state.AgentStateCollection); ok {
		return coll.GetGlobal(key), nil
	}
	return nil, nil
}

// DumpState 导出完整状态快照，委托到 inner.State() 的 AgentStateCollection
func (s *Session) DumpState() map[string]any {
	if coll, ok := s.inner.State().(*state.AgentStateCollection); ok {
		return coll.Dump()
	}
	return nil
}

// ──────────────────────────── 流操作方法（桩实现） ────────────────────────────

// WriteStream 写入标准输出流。
// ⤵️ 5.10 回填：StreamWriterManager 实现后填充真实逻辑
func (s *Session) WriteStream(data any) error {
	return nil
}

// WriteCustomStream 写入自定义流。
// ⤵️ 5.10 回填：StreamWriterManager 实现后填充真实逻辑
func (s *Session) WriteCustomStream(data any) error {
	return nil
}

// StreamIterator 返回流迭代 channel。
// ⤵️ 5.10 回填：StreamWriterManager 实现后填充真实逻辑
func (s *Session) StreamIterator() <-chan any {
	return nil
}

// CloseStream 关闭流发射器并注销回调。
// ⤵️ 5.10 回填：StreamWriterManager 实现后填充真实逻辑
func (s *Session) CloseStream() error {
	return nil
}

// ──────────────────────────── 生命周期方法 ────────────────────────────

// PreRun 会话预运行：触发 AGENT_SESSION_CREATED 回调 + 检查点预执行。
//
// 幂等：多次调用只执行一次。
//
// 对应 Python: Session.pre_run()
func (s *Session) PreRun(ctx context.Context, inputs ...map[string]any) error {
	if s.preRunDone {
		return nil
	}

	// 触发 AgentSessionCreated 回调
	callback.GetCallbackFramework().TriggerSession(ctx, &callback.SessionCallEventData{
		Event:     callback.AgentSessionCreated,
		SessionID: s.GetSessionID(),
		Card:      s.card,
		Session:   s,
	})

	// 检查点预执行
	// ⤵️ 5.8 回填：Checkpointer 实现后调用 pre_agent_execute
	// 当前 checkpointer 为 nil，跳过

	s.preRunDone = true
	logger.Info(logger.ComponentAgentCore).
		Str("action", "session_pre_run").
		Str("session_id", s.GetSessionID()).
		Msg("Session PreRun 完成")

	return nil
}

// PostRun 会话后运行：关闭流 + 提交检查点。
//
// 幂等：多次调用只执行一次。
//
// 对应 Python: Session.post_run()
func (s *Session) PostRun(ctx context.Context) error {
	if s.postRunDone {
		return nil
	}

	if s.closeStreamOnPostRun {
		_ = s.CloseStream()
	}

	_ = s.Commit(ctx)

	s.postRunDone = true
	logger.Info(logger.ComponentAgentCore).
		Str("action", "session_post_run").
		Str("session_id", s.GetSessionID()).
		Msg("Session PostRun 完成")

	return nil
}

// Commit 提交当前状态到检查点（不关闭流）。
// ⤵️ 5.8 回填：Checkpointer 实现后调用 post_agent_execute
// 对应 Python: Session.commit()
func (s *Session) Commit(ctx context.Context) error {
	// 当前 checkpointer 为 nil，跳过
	return nil
}

// ──────────────────────────── 交互方法 ────────────────────────────

// Interact 请求用户输入。
// ✅ 5.7 已回填：SimpleAgentInteraction 实现后填充真实逻辑
// 对应 Python: Session.interact(value)
func (s *Session) Interact(value any) error {
	if s.interaction == nil {
		s.interaction = interaction.NewSimpleAgentInteraction(s.inner)
	}
	return s.interaction.WaitUserInputs(context.Background(), value)
}

// ──────────────────────────── 子会话方法（桩实现） ────────────────────────────

// CreateWorkflowSession 创建子 WorkflowSession。
//
// 从 AgentSession 的 AgentStateCollection 获取 globalState，
// 包装为 WorkflowCommitState 与 AgentSession 共享全局状态。
// WorkflowSession 的 globalState 更新 commit 后 AgentSession 也能读到。
//
// 对应 Python: Session.create_workflow_session()
func (s *Session) CreateWorkflowSession() *WorkflowSession {
	// 取出 AgentStateCollection 的 globalState（*InMemoryState 实例）
	var workflowState *state.WorkflowCommitState
	if coll, ok := s.inner.State().(*state.AgentStateCollection); ok {
		// 用 globalState 包装为 InMemoryCommitState，与 AgentSession 共享同一个底层实例
		sharedGlobalState := state.NewInMemoryCommitState(coll.GlobalState())
		workflowState = state.NewInMemoryWorkflowState(sharedGlobalState)
	} else {
		workflowState = state.NewInMemoryWorkflowState()
	}

	inner := internal.NewWorkflowSession(
		internal.WithWorkflowParent(s.inner),
		internal.WithWorkflowSessionID(s.inner.SessionID()),
		internal.WithWorkflowState(workflowState),
	)

	return NewWorkflowSession(
		WithWorkflowSessionInner(inner),
		WithWorkflowSessionParent(s.inner),
		WithWorkflowSessionSessionID(s.inner.SessionID()),
	)
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// tagStreamPayload 为流数据添加来源元数据。
// 对应 Python: Session._tag_stream_payload()
func (s *Session) tagStreamPayload(data map[string]any) map[string]any {
	if len(s.sourceMetadata) == 0 {
		return data
	}
	result := make(map[string]any, len(data)+len(s.sourceMetadata))
	for k, v := range data {
		result[k] = v
	}
	for k, v := range s.sourceMetadata {
		result[k] = v
	}
	return result
}

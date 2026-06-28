package session

import (
	"context"

	"github.com/google/uuid"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/runner/callback"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/checkpointer"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/config"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/controller"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/interaction"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/interfaces"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/internal"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/state"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/stream"
	agentschema "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/schema"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	"github.com/uapclaw/uapclaw-go/internal/common/schema"
)

// ──────────────────────────── 结构体 ────────────────────────────

// Session Agent 公开会话，提供用户面向的 API。
//
// 组合内部层 AgentSession，实现 PreRun→Invoke/Stream→PostRun 完整生命周期。
// 负责：状态读写、流写入、交互、回调触发、检查点持久化。
//
// 对应 Python: openjiuwen/core/session/agent.py (Session)
type Session struct {
	// sessionID 会话唯一标识，Session 自身持有，不依赖 inner
	sessionID string
	// inner 内部 AgentSession 实例
	inner *internal.AgentSession
	// card Agent 身份元数据
	card *agentschema.AgentCard
	// envs 环境变量（通过 WithEnvs 设置）
	// 对齐 Python: Session.__init__(envs=dict)
	envs map[string]any
	// checkpointer 检查点器（通过 WithCheckpointer option 设置）
	checkpointer checkpointer.Checkpointer
	// streamWriterManager 流写入管理器（通过 WithStreamWriterManager 设置）
	// 对齐 Python: Session.__init__(stream_writer_manager=StreamWriterManager|None)
	// ✅ 5.10 已回填：any → *stream.StreamWriterManager
	streamWriterManager *stream.StreamWriterManager
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

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// 编译时检查 *Session 满足 SessionFacade 接口
var _ interfaces.SessionFacade = (*Session)(nil)

// NewSession 创建公开层 Session 实例。
//
// 默认行为（对齐 Python Session.__init__）：
//   - sessionID: 若未指定，自动生成 UUID
//   - config: 创建默认 Config 并设置 envs，传入 inner
//     ⤵️ 5.12 回填：创建真实 SessionConfig 实例，当前用 map[string]any 占位
//   - checkpointer: 若未通过 WithCheckpointer 设置，使用 checkpointer.GetCheckpointer()
//   - streamWriterManager: 若未通过 WithStreamWriterManager 设置，传 nil 给 inner
//     （inner 会自动创建默认实例，✅ 5.10 已回填）
//   - closeStreamOnPostRun: 默认 true
//   - sourceMetadata: 默认空 map
//
// 对应 Python: openjiuwen/core/session/agent.py create_agent_session()
func NewSession(opts ...SessionOption) *Session {
	s := &Session{
		sessionID:            uuid.New().String(),
		closeStreamOnPostRun: true,
		sourceMetadata:       make(map[string]any),
		envs:                 make(map[string]any),
	}
	for _, opt := range opts {
		opt(s)
	}

	// 统一用最终确定的 sessionID 创建一次 AgentSession，
	// 同时注入各组件，对齐 Python AgentSession 的默认行为

	// 1. config：外层创建 SessionConfig + SetEnvs，传给 inner
	//    Python: config = Config(); config.set_envs(envs); self._inner = AgentSession(config=config)
	cfg := config.NewSessionConfig(context.Background())
	if len(s.envs) > 0 {
		cfg.SetEnvs(s.envs)
	}

	// 2. checkpointer：未设置时从全局工厂获取
	//    Python: checkpointer = CheckpointerFactory.get_checkpointer()
	cp := s.checkpointer
	if cp == nil {
		cp = checkpointer.GetCheckpointer()
	}

	// 3. streamWriterManager：外层透传给 inner
	//    Python: self._inner = AgentSession(stream_writer_manager=stream_writer_manager)
	//    未设置时传 nil，由 inner 自动创建默认实例（✅ 5.10 已回填）

	s.inner = internal.NewAgentSession(s.sessionID,
		internal.WithConfig(cfg),
		internal.WithCard(s.card),
		internal.WithCheckpointer(cp),
		internal.WithStreamWriterManager(s.streamWriterManager),
	)

	logger.Info(logger.ComponentAgentCore).
		Str("action", "new_session").
		Str("session_id", s.sessionID).
		Msg("创建公开层 Session")

	return s
}

// WithSessionID 设置会话 ID 的选项
func WithSessionID(id string) SessionOption {
	return func(s *Session) {
		s.sessionID = id
	}
}

// WithCard 设置 Agent 身份元数据的选项
func WithCard(card *agentschema.AgentCard) SessionOption {
	return func(s *Session) {
		s.card = card
	}
}

// WithEnvs 设置环境变量的选项。
// 外层 NewSession 会创建默认 Config 并将 envs 写入，再传给 inner。
// 对齐 Python: Session.__init__(envs=dict[str, Any]=None) → config.set_envs(envs)
func WithEnvs(envs map[string]any) SessionOption {
	return func(s *Session) {
		s.envs = envs
	}
}

// WithCheckpointer 设置检查点器的选项。
// 若不设置，NewSession 默认注入 checkpointer.GetCheckpointer()。
func WithCheckpointer(cp checkpointer.Checkpointer) SessionOption {
	return func(s *Session) {
		// 存到临时字段，在 NewSession 创建 inner 时使用
		s.checkpointer = cp
	}
}

// WithStreamWriterManager 设置流写入管理器的选项。
// 外层透传给 inner，由 inner 自动创建默认实例。
// 对齐 Python: Session.__init__(stream_writer_manager=StreamWriterManager|None)
// ✅ 5.10 已回填：参数类型从 any 改为 *stream.StreamWriterManager
func WithStreamWriterManager(mgr *stream.StreamWriterManager) SessionOption {
	return func(s *Session) {
		s.streamWriterManager = mgr
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

// GetSessionID 返回会话唯一标识
func (s *Session) GetSessionID() string {
	return s.sessionID
}

// GetEnv 获取环境变量值。
// 对应 Python: Session.get_env(key, default) → self._inner.config().get_env(key, default)
func (s *Session) GetEnv(key string, defaultValue ...any) any {
	cfg := s.inner.Config()
	if cfg == nil {
		return nil
	}
	return cfg.GetEnv(key, defaultValue...)
}

// GetEnvs 获取所有环境变量。
// 对应 Python: Session.get_envs() → self._inner.config().get_envs()
func (s *Session) GetEnvs() map[string]any {
	cfg := s.inner.Config()
	if cfg == nil {
		return nil
	}
	return cfg.GetEnvs()
}

// GetAgentID 返回 Agent ID
// 对应 Python: AgentSession.agent_id()
func (s *Session) GetAgentID() string {
	if s.card != nil {
		return s.card.AbilityID()
	}
	return ""
}

// GetAgentName 返回 Agent 名称
func (s *Session) GetAgentName() string {
	if s.card != nil {
		return s.card.Name
	}
	return ""
}

// GetAgentDescription 返回 Agent 描述
func (s *Session) GetAgentDescription() string {
	if s.card != nil {
		return s.card.Description
	}
	return ""
}

// UpdateState 更新全局状态，委托到 inner.State() 的 SessionState
func (s *Session) UpdateState(data map[string]any) {
	s.inner.State().UpdateGlobal(data)
}

// GetState 获取全局状态值，委托到 inner.State() 的 SessionState
func (s *Session) GetState(key state.StateKey) (any, error) {
	return s.inner.State().GetGlobal(key), nil
}

// DumpState 导出完整状态快照，委托到 inner.State() 的 SessionState
func (s *Session) DumpState() map[string]any {
	return s.inner.State().Dump()
}

// WriteStream 写入标准输出流。
//
// SessionFacade 接口实现。
// 对应 Python: Session.write_stream(data)
func (s *Session) WriteStream(ctx context.Context, data any) error {
	return s.writeStream(data)
}

// WriteCustomStream 写入自定义流。
//
// SessionFacade 接口实现。
// 对应 Python: Session.write_custom_stream(data)
func (s *Session) WriteCustomStream(ctx context.Context, data any) error {
	return s.writeCustomStream(data)
}

// StreamIterator 返回流迭代 channel。
// 对应 Python: Session.stream_iterator()
// ✅ 5.10 已回填：StreamWriterManager 实现后填充真实逻辑
func (s *Session) StreamIterator() <-chan stream.Schema {
	mgr := s.inner.StreamWriterManager()
	if mgr == nil {
		ch := make(chan stream.Schema)
		close(ch)
		return ch
	}
	return mgr.StreamOutput()
}

// CloseStream 关闭流发射器。
// 对应 Python: Session.close_stream()
// ✅ 5.10 已回填：StreamWriterManager 实现后填充真实逻辑
// ✅ SW-33 已回填：注销该 session 的 StreamWrite 全部回调
func (s *Session) CloseStream() error {
	ctx := context.Background()
	mgr := s.inner.StreamWriterManager()
	if mgr == nil {
		return nil
	}
	// 关闭 emitter，发送 END_FRAME
	_ = mgr.StreamEmitter().Close(ctx)

	// ⤴️ SW-33 回填：注销该 session 的 StreamWrite 全部回调
	// 对应 Python: await Runner.callback_framework.unregister_event(event=self._session_id + "write_stream")
	callback.GetCallbackFramework().OffAllCustom(s.GetSessionID() + "write_stream")
	return nil
}

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
	var cardValue any
	if s.card != nil {
		cardValue = s.card
	}
	callback.GetCallbackFramework().TriggerSession(ctx, &callback.SessionCallEventData{
		Event:     callback.AgentSessionCreated,
		SessionID: s.GetSessionID(),
		Card:      cardValue,
		Session:   s,
	})

	// 检查点预执行
	if cp := s.inner.Checkpointer(); cp != nil {
		var inputVal any
		if len(inputs) > 0 {
			inputVal = inputs[0]
		}
		if err := cp.PreAgentExecute(ctx, s.inner, inputVal); err != nil {
			return err
		}
	}

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
// Python 中 commit() 内部 post_agent_execute 抛异常时会向上传播，Go 同样返回 Commit 错误。
func (s *Session) PostRun(ctx context.Context) error {
	if s.postRunDone {
		return nil
	}

	if s.closeStreamOnPostRun {
		_ = s.CloseStream()
	}

	// G-08 修复：不再吞掉 Commit 错误，对齐 Python 异常传播行为
	if err := s.Commit(ctx); err != nil {
		s.postRunDone = true
		logger.Error(logger.ComponentAgentCore).Err(err).
			Str("action", "session_post_run").
			Str("session_id", s.GetSessionID()).
			Msg("Session PostRun Commit 失败")
		return err
	}

	s.postRunDone = true
	logger.Info(logger.ComponentAgentCore).
		Str("action", "session_post_run").
		Str("session_id", s.GetSessionID()).
		Msg("Session PostRun 完成")

	return nil
}

// Commit 提交当前状态到检查点（不关闭流）。
// 对应 Python: Session.commit()
func (s *Session) Commit(ctx context.Context) error {
	if cp := s.inner.Checkpointer(); cp != nil {
		return cp.PostAgentExecute(ctx, s.inner)
	}
	return nil
}

// Interact 请求用户输入。
// ✅ 5.7 已回填：SimpleAgentInteraction 实现后填充真实逻辑
// 对应 Python: Session.interact(value)
func (s *Session) Interact(ctx context.Context, value any) error {
	if s.interaction == nil {
		s.interaction = interaction.NewSimpleAgentInteraction(s.inner)
	}
	return s.interaction.WaitUserInputs(ctx, value)
}

// CreateWorkflowSession 创建子 WorkflowSession。
//
// 从 AgentSession 的 AgentStateCollection 获取 globalState，
// 包装为 WorkflowCommitState 与 AgentSession 共享全局状态。
// WorkflowSession 的 globalState 更新 commit 后 AgentSession 也能读到。
//
// 对应 Python: Session.create_workflow_session()
func (s *Session) CreateWorkflowSession() *WorkflowSession {
	// 取出 AgentStateCollection 的 globalState（*InMemoryStateLike 实例）
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

// CreateAgentSession 创建 Agent 会话实例。
// 对齐 Python: openjiuwen/core/session/agent.py create_agent_session()
// 用于 AgentSessionContainer.load() 从磁盘恢复会话时创建真实 Session。
func CreateAgentSession(agentID, sessionID string) *Session {
	card := &agentschema.AgentCard{
		BaseCard: schema.BaseCard{ID: agentID},
	}
	return NewSession(WithSessionID(sessionID), WithCard(card))
}

// ──────────────────────────── 非导出函数 ────────────────────────────

func init() {
	// 注册 Session 创建函数到 controller 包，解决循环依赖
	controller.RegisterSessionCreator(func(agentID, sessionID string) controller.StateAccessor {
		return CreateAgentSession(agentID, sessionID)
	})
}

// writeStream 写入标准输出流（内部实现）。
// 对应 Python: Session.write_stream(data)
// data 接受 any 类型，内部通过 normalizeOutputStream 统一转为 OutputSchema。
// ✅ 5.10 已回填：StreamWriterManager 实现后填充真实逻辑
// ✅ SW-31 已回填：触发自定义 StreamWrite 回调
func (s *Session) writeStream(data any) error {
	ctx := context.Background()
	streamData := normalizeOutputStream(s.tagStreamPayload(data))

	// ⤴️ SW-31 回填：触发自定义 StreamWrite 事件
	// 对应 Python: await trigger(self._session_id + "write_stream", data=stream_data)
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
// ✅ 5.10 已回填：StreamWriterManager 实现后填充真实逻辑
// ✅ SW-32 已回填：触发自定义 StreamWrite 回调
func (s *Session) writeCustomStream(data any) error {
	ctx := context.Background()
	streamData := s.tagStreamPayload(data)

	// ⤴️ SW-32 回填：触发自定义 StreamWrite 事件
	// 对应 Python: await trigger(self._session_id + "write_stream", data=stream_data)
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
// ✅ 5.10 已回填：支持 map[string]any、OutputSchema 和 CustomSchema
func (s *Session) tagStreamPayload(data any) any {
	if len(s.sourceMetadata) == 0 {
		return data
	}
	switch v := data.(type) {
	case map[string]any:
		result := make(map[string]any, len(v)+len(s.sourceMetadata))
		for k, val := range v {
			result[k] = val
		}
		for k, val := range s.sourceMetadata {
			result[k] = val
		}
		return result
	case *stream.OutputSchema:
		// 指针类型：在副本上修改 Payload 注入 sourceMetadata
		payload := v.Payload
		if payloadMap, ok := payload.(map[string]any); ok {
			newPayload := make(map[string]any, len(payloadMap)+len(s.sourceMetadata))
			for k, val := range payloadMap {
				newPayload[k] = val
			}
			for k, val := range s.sourceMetadata {
				newPayload[k] = val
			}
			payload = newPayload
		} else {
			newPayload := make(map[string]any, 1+len(s.sourceMetadata))
			newPayload["value"] = payload
			for k, val := range s.sourceMetadata {
				newPayload[k] = val
			}
			payload = newPayload
		}
		return &stream.OutputSchema{Type: v.Type, Index: v.Index, Payload: payload, IsLastSchema: v.IsLastSchema}
	case stream.OutputSchema:
		payload := v.Payload
		if payloadMap, ok := payload.(map[string]any); ok {
			newPayload := make(map[string]any, len(payloadMap)+len(s.sourceMetadata))
			for k, val := range payloadMap {
				newPayload[k] = val
			}
			for k, val := range s.sourceMetadata {
				newPayload[k] = val
			}
			payload = newPayload
		} else {
			newPayload := make(map[string]any, 1+len(s.sourceMetadata))
			newPayload["value"] = payload
			for k, val := range s.sourceMetadata {
				newPayload[k] = val
			}
			payload = newPayload
		}
		return stream.OutputSchema{Type: v.Type, Index: v.Index, Payload: payload, IsLastSchema: v.IsLastSchema}
	case stream.CustomSchema:
		// 对齐 Python：CustomSchema 的 extra="allow" 语义下，metadata 合并进 Data 字段
		newData := make(map[string]any, len(v.Data)+len(s.sourceMetadata))
		for k, val := range v.Data {
			newData[k] = val
		}
		for k, val := range s.sourceMetadata {
			newData[k] = val
		}
		return stream.CustomSchema{Type: v.Type, Data: newData}
	default:
		return data
	}
}

// normalizeOutputStream 将流数据统一转为 OutputSchema。
// 对应 Python: Session._normalize_output_stream(data)
// ✅ 5.10 已回填：R1 回填
//
// 转换逻辑对齐 Python:
//   - *OutputSchema / OutputSchema → 直接返回（指针解引用为值）
//   - dict 含 type/index/payload → 构造 OutputSchema（对齐 Pydantic model_validate）
//   - 其他 → OutputSchema{Type:"message", Index:0, Payload:data}
func normalizeOutputStream(data any) stream.OutputSchema {
	switch v := data.(type) {
	case *stream.OutputSchema:
		// 指针类型：解引用为值返回（对齐 Python isinstance(data, OutputSchema) 直接 return）
		return *v
	case stream.OutputSchema:
		return v
	case map[string]any:
		// 检查是否包含完整 OutputSchema 字段
		if _, hasType := v["type"]; hasType {
			if _, hasIndex := v["index"]; hasIndex {
				if _, hasPayload := v["payload"]; hasPayload {
					index, _ := v["index"].(int)
					typeStr, _ := v["type"].(string)
					return stream.OutputSchema{
						Type:    typeStr,
						Index:   index,
						Payload: v["payload"],
					}
				}
			}
		}
	}
	// 默认构造
	return stream.OutputSchema{Type: "message", Index: 0, Payload: data}
}

// normalizeCustomStream 将流数据统一转为 CustomSchema。
// 对应 Python: CustomStreamWriter.write(data) → CustomSchema.model_validate(data)
//
// 转换逻辑对齐 Python:
//   - CustomSchema → 直接返回
//   - dict → CustomSchema{Type:"custom", Data:dict}
//   - 其他 → CustomSchema{Type:"custom", Data:{"value": data}}
func normalizeCustomStream(data any) stream.CustomSchema {
	switch v := data.(type) {
	case stream.CustomSchema:
		return v
	case map[string]any:
		return stream.CustomSchema{Type: "custom", Data: v}
	default:
		return stream.CustomSchema{Type: "custom", Data: map[string]any{"value": data}}
	}
}

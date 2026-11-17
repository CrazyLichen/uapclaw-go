package externalcli

// 外部 harness SPI 到 AgentTeam MemberRuntime 的桥接：对齐 Python
// openjiuwen/agent_teams/external/member_runtime.py（1105 行）。
//
// HarnessIOAdapter 已把任意 HarnessProtocol 转成 DeepAgent 式输入/输出契约；
// 本文件在其上叠加团队专属层：成员子 AgentSession（checkpoint 汇与
// team-context 投递基线）、TeamContextTracker 搭载、外部 runtime 可靠性循环、
// 可选的轨迹记录器（经协议事件流供数）与认证回退晋升钩子。
//
// 本批落骨架：结构体字段、构造器、只读面、pre-start 绑定、成员会话/团队
// 上下文助手、checkpoint 序列化助手、no-op 钩子与纯函数助手。依赖
// external/reliability.py（RuntimeReliabilityContext，⤵️15.7/15.8）与
// workflow activity emitter（⤵️对应章节）的行为方法（start/send/_on_event/
// stop 等）暂缺，见文件尾「暂缺清单」，随 provider 路（Task 8 / 15.7/15.8）
// 一并移植。

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"

	"github.com/uapclaw/uapclaw-go/internal/agent_teams/tracker"
	harness_protocol "github.com/uapclaw/uapclaw-go/internal/agentcore/harness_protocol"
	harnessproviders "github.com/uapclaw/uapclaw-go/internal/agentcore/harness_providers"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/interaction"
	sessioninterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/session/interfaces"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/state"
	agentschema "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/schema"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 常量 ────────────────────────────

// 团队 MCP server 的逻辑名（cli_agent/__init__.py:24）。
// Python: TEAM_MCP_SERVER_NAME
const TeamMCPServerName = "openjiuwen-team"

const (
	// harness 私有事件主题 + 命名空间（member_runtime.py:88-90）。
	// Python: _EVENT_STATE/_EVENT_ROUND/_EVENT_NAMESPACE
	eventStateTopicMR = "harness.state"
	eventRoundTopicMR = "harness.round"
	eventNamespaceMR  = "external_harness_runtime"

	// 成员会话状态键与子键（member_runtime.py:91-93）。
	// Python: _EXTERNAL_RUNTIME_STATE_KEY/_EXTERNAL_BACKEND_KEY/_EXTERNAL_CHECKPOINT_KEY
	externalRuntimeStateKey = "external_runtime"
	externalBackendKey      = "backend"
	externalCheckpointKey   = "checkpoint"

	// 携带成员实际运行模型的 provider 事件（member_runtime.py:100-101）：
	// Codex harness 经 session/model_changed 宣告线程模型，Claude CLI 在每条
	// system/init 消息里报告其服务的模型。
	// Python: _MODEL_CHANGED_EVENT/_SYSTEM_INIT_EVENT
	modelChangedEvent = "session/model_changed"
	systemInitEvent   = "system/init"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ContextFactory 由团队会话解析 HarnessContext 的工厂（Python 上下文可以是
// 现成 HarnessContext 或工厂；Go 统一为工厂，现成上下文由调用方包一层）。
//
// Python: ContextFactory (member_runtime.py:103)
type ContextFactory func(teamSession any) (*harness_protocol.HarnessContext, error)

// PromoteFallbackModel 持久化 provider 认证回退的晋升回调；返回 true 表示
// 回退已获批准。
//
// Python: PromoteFallbackModel (member_runtime.py:107)
type PromoteFallbackModel func(ctx context.Context) (bool, error)

// memberSessionCommitter 成员会话的 commit 面（SessionFacade 未含 Commit；
// Python member_session.commit 的 Go 等价由具体会话类型提供）。
type memberSessionCommitter interface {
	Commit(ctx context.Context) error
}

// memberSessionPreRunner 成员会话的 pre_run/post_run 面。
type memberSessionPreRunner interface {
	PreRun(ctx context.Context) error
	PostRun(ctx context.Context) error
}

// TeamContextAwareRuntime 协调处理器推送名册宣告所依赖的行为面。
//
// Python: TeamContextAwareRuntime (member_runtime.py:111)
type TeamContextAwareRuntime interface {
	// State 返回当前 harness 相位。
	State() harness_protocol.HarnessState
	// AnnounceTeamContext 把挂起的团队名册宣告推给成员。
	AnnounceTeamContext() error
}

// MemberSessionCheckpointSink 把 provider checkpoint 持久化到成员子 AgentSession。
//
// Python: _MemberSessionCheckpointSink (member_runtime.py:124)
type MemberSessionCheckpointSink struct {
	// memberSession 成员子会话
	memberSession sessioninterfaces.SessionFacade
	// backend 拥有 checkpoint 的后端名
	backend string
}

// NewMemberSessionCheckpointSink 绑定成员会话与后端名。
// Python: _MemberSessionCheckpointSink.__init__ (member_runtime.py:127)
func NewMemberSessionCheckpointSink(memberSession sessioninterfaces.SessionFacade, backend string) *MemberSessionCheckpointSink {
	return &MemberSessionCheckpointSink{memberSession: memberSession, backend: backend}
}

// Save 把 checkpoint 写进成员会话状态并提交。
// reason 与 expectedStorageRevision 在 Python 侧同样被显式忽略（:138）。
//
// Python: _MemberSessionCheckpointSink.save (member_runtime.py:131)
func (s *MemberSessionCheckpointSink) Save(
	checkpoint *harness_protocol.HarnessCheckpoint,
	reason harness_protocol.CheckpointReason,
	expectedStorageRevision string,
) (harness_protocol.CheckpointSaveReceipt, error) {
	_ = reason
	_ = expectedStorageRevision
	s.memberSession.UpdateState(map[string]any{
		externalRuntimeStateKey: map[string]any{
			externalBackendKey:    s.backend,
			externalCheckpointKey: CheckpointToDict(checkpoint),
		},
	})
	if committer, ok := s.memberSession.(memberSessionCommitter); ok {
		if err := committer.Commit(context.Background()); err != nil {
			return harness_protocol.CheckpointSaveReceipt{}, err
		}
	}
	return harness_protocol.CheckpointSaveReceipt{
		CheckpointID:    checkpoint.CheckpointID,
		Sequence:        checkpoint.Sequence,
		StorageRevision: checkpoint.CheckpointID,
	}, nil
}

// ExternalHarnessMemberRuntimeOptions 构造选项（Python __init__ 关键字参数）。
//
// Python: ExternalHarnessMemberRuntime.__init__ (member_runtime.py:225)
type ExternalHarnessMemberRuntimeOptions struct {
	// TeamContextTracker 把团队状态喂进出站消息的 tracker；nil 关闭团队状态投递
	TeamContextTracker *tracker.TeamContextTracker
	// StopOnUnsupportedForceAbort 宿主要求立即中止而 provider 不能交付时停整个周期
	StopOnUnsupportedForceAbort bool
	// ResumeExternalBackend 要求 provider 从成员 checkpoint 恢复其持久化会话
	ResumeExternalBackend bool
	// RequireCheckpoint 与 ResumeExternalBackend 连用时无 checkpoint 指针则拒绝启动
	RequireCheckpoint bool
	// SeedCheckpoint 宿主预置的 checkpoint 数据（启动时写入成员会话）
	SeedCheckpoint map[string]any
	// AgentKind 可靠性词汇（"claude"/"codex"）；空关闭外部 runtime 可靠性循环
	AgentKind string
	// CLIPath 传给可靠性上下文的 CLI 可执行路径
	CLIPath string
	// InjectMCP spawn 路径是否挂载团队 MCP server
	InjectMCP bool
	// MCPServerName 团队 MCP server 逻辑名；空取 TeamMCPServerName
	MCPServerName string
}

// ExternalHarnessMemberRuntime 把 HarnessProtocol 投影到内部团队 runtime 缝。
//
// immediate=True 是能力感知的：IDLE 正常启动，provider 声明 STEER 才中途
// steer，否则降级为显式 follow-up。为 DSH 之类 provider 保留输入投递而不虚报
// 中途 steer。
//
// Python: ExternalHarnessMemberRuntime (member_runtime.py:199)
type ExternalHarnessMemberRuntime struct {
	mu sync.Mutex

	// harness 驱动成员的协议实现
	harness harness_protocol.HarnessProtocol
	// adapter harness 上的 DeepAgent 式门面
	adapter *harnessproviders.HarnessIOAdapter
	// contextSource 上下文来源（现成上下文或工厂）
	contextSource ContextFactory
	// teamContextTracker 团队状态 tracker
	teamContextTracker *tracker.TeamContextTracker
	// taskOutcomeTracker 把未告知任务事实折叠进下一条消息的 tracker（F_119）
	taskOutcomeTracker tracker.TaskEventTracker
	// resumeExternalBackend / requireCheckpoint / seedCheckpoint 恢复语义
	resumeExternalBackend bool
	requireCheckpoint     bool
	seedCheckpoint        map[string]any
	// agentKind / cliPath 可靠性循环词汇与 CLI 路径
	agentKind string
	cliPath   string
	// InjectMCP / MCPServerName 团队 MCP server 挂载配置（公开字段，对齐 Python）
	InjectMCP     bool
	MCPServerName string
	// memberName / memberAgentID 成员身份（启动时从上下文刷新）
	memberName    string
	memberAgentID string
	memberSession sessioninterfaces.SessionFacade
	// stopped 周期是否未启动或已停止
	stopped bool
	// extraMCPServers 下一次 start 追加挂载的 MCP server
	extraMCPServers []harness_protocol.McpServerConfig
	// boundTools 下一次 start 挂载的原生工具网关
	boundTools harness_protocol.ToolGateway
	// reliabilityCtx 可靠性上下文（RuntimeReliabilityContext ⤵️15.7/15.8）
	reliabilityCtx any
	// trajectory 把本成员事件流转成轨迹 span 的记录器
	trajectory *harnessproviders.HarnessTrajectoryRecorder
	// promoteFallbackModel 认证回退晋升回调
	promoteFallbackModel PromoteFallbackModel
	// teardownHooks harness 停止后执行的钩子（可观测接收器等）
	teardownHooks []func(ctx context.Context) error
	// roundSeq / currentRoundID 轮次计数
	roundSeq       int
	currentRoundID int
	// reportedModel 成员 CLI 报告的运行模型（空 = 尚未见过）
	reportedModel string
	// activitySink 结构化 AGENT_ACTIVITY 事件汇；nil 保持静默
	activitySink func(event any)
	// activityIdentity 随每个事件携带的身份元组
	// (agent_id, phase, label, correlation_id)
	activityIdentity [4]string
	// onState/onRound 订阅回调（与 cliRuntimeBase 同款契约）
	onState []stateCallback
	onRound []roundCallback
}

// NewExternalHarnessMemberRuntime 构造 runtime 并组装 HarnessIOAdapter。
// contextSource 为 nil 时报错（Python 允许传现成 HarnessContext；Go 统一为
// 工厂，现成上下文包一层工厂）。
//
// Python: ExternalHarnessMemberRuntime.__init__ (member_runtime.py:225)
func NewExternalHarnessMemberRuntime(
	harness harness_protocol.HarnessProtocol,
	contextSource ContextFactory,
	opts ExternalHarnessMemberRuntimeOptions,
) (*ExternalHarnessMemberRuntime, error) {
	if contextSource == nil {
		return nil, fmt.Errorf("external harness member runtime requires a context source")
	}
	mcpServerName := opts.MCPServerName
	if mcpServerName == "" {
		mcpServerName = TeamMCPServerName
	}
	adapter := harnessproviders.NewHarnessIOAdapter(harness, harnessproviders.HarnessIOAdapterOptions{
		AutoApproveTools:            true,
		StopOnUnsupportedForceAbort: opts.StopOnUnsupportedForceAbort,
	})
	r := &ExternalHarnessMemberRuntime{
		harness:               harness,
		adapter:               adapter,
		contextSource:         contextSource,
		teamContextTracker:    opts.TeamContextTracker,
		resumeExternalBackend: opts.ResumeExternalBackend,
		requireCheckpoint:     opts.RequireCheckpoint,
		seedCheckpoint:        opts.SeedCheckpoint,
		agentKind:             opts.AgentKind,
		cliPath:               opts.CLIPath,
		InjectMCP:             opts.InjectMCP,
		MCPServerName:         mcpServerName,
		stopped:               true,
	}
	// 成员名/agent id：Python 依上下文是现成 HarnessContext 还是工厂分别取
	// context.agent_name / harness.card.name；Go 统一工厂，构造期先按卡片名
	// 兜底，start 时从解析出的上下文刷新（:260-265）。
	r.memberName = harness.Card().Name
	return r, nil
}

// ──────────────────────────── 导出函数 ────────────────────────────

// Harness 返回驱动的协议实现。
// Python: harness property (member_runtime.py:293)
func (r *ExternalHarnessMemberRuntime) Harness() harness_protocol.HarnessProtocol { return r.harness }

// ProviderName 返回 provider 卡名（"claude-code"/"codex"/...）。
// Python: provider_name property (member_runtime.py:297)
func (r *ExternalHarnessMemberRuntime) ProviderName() string { return r.harness.Card().Name }

// State 返回当前 harness 相位。
// Python: state property (member_runtime.py:302)
func (r *ExternalHarnessMemberRuntime) State() harness_protocol.HarnessState {
	return r.harness.State()
}

// SessionID 返回 provider 原生会话标识（尚不可用时为 nil）。
// Python: session_id property (member_runtime.py:306)
func (r *ExternalHarnessMemberRuntime) SessionID() *string {
	return r.harness.ProviderSessionID()
}

// ReportedModel 返回成员 CLI 报告的运行模型（空 = 尚未见过）。
// 直接从 Claude system/init / Codex session/model_changed 事件捕获；绑定了
// 可靠性上下文时保持其同步，但不再以它为源——swarmflow worker 不绑定，
// 其报告仍须携带真实端点。
// Python: reported_model property (member_runtime.py:310)
func (r *ExternalHarnessMemberRuntime) ReportedModel() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.reportedModel
}

// TrajectoryRecorder 返回把本成员事件流转成轨迹 span 的记录器。
// Python: trajectory_recorder property (member_runtime.py:321)
func (r *ExternalHarnessMemberRuntime) TrajectoryRecorder() *harnessproviders.HarnessTrajectoryRecorder {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.trajectory
}

// MemberSession 返回已启动时成员的子 AgentSession（否则 nil）。
// 宿主要在 provider checkpoint 旁持久化自己的状态时经本会话写入：runtime 在
// 每次 checkpoint 提交整个状态，同桶上的并行会话会被擦除。
// Python: member_session property (member_runtime.py:326)
func (r *ExternalHarnessMemberRuntime) MemberSession() sessioninterfaces.SessionFacade {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.memberSession
}

// ReliabilityAgentKind 返回本成员的可靠性词汇（空 = 可靠性循环关闭）。
// Python: reliability_agent_kind property (member_runtime.py:336)
func (r *ExternalHarnessMemberRuntime) ReliabilityAgentKind() string {
	return r.agentKind
}

// BindTeamContextTracker 绑定团队状态 tracker。
// Python: bind_team_context_tracker (member_runtime.py:343)
func (r *ExternalHarnessMemberRuntime) BindTeamContextTracker(t *tracker.TeamContextTracker) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.teamContextTracker = t
}

// BindTaskOutcomeTracker 绑定把未告知任务事实（评审通过/漏掉的编辑）折叠进
// 下一条消息的 tracker。
// Python: bind_task_outcome_tracker (member_runtime.py:346)
func (r *ExternalHarnessMemberRuntime) BindTaskOutcomeTracker(t tracker.TaskEventTracker) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.taskOutcomeTracker = t
}

// BindMCPServers 在下一次 start 追加挂载 MCP server。
// Python: bind_mcp_servers (member_runtime.py:350)
func (r *ExternalHarnessMemberRuntime) BindMCPServers(servers []harness_protocol.McpServerConfig) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.extraMCPServers = append(r.extraMCPServers, servers...)
}

// BindTools 在下一次 start 挂载原生工具网关。
// Python: bind_tools (member_runtime.py:354)
func (r *ExternalHarnessMemberRuntime) BindTools(tools harness_protocol.ToolGateway) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.boundTools = tools
}

// BindTrajectoryRecorder 把本成员的协议事件流记录为轨迹 span。
// 绑定记录器同时使宿主声明 HostCapability.MODEL_REQUEST_OBSERVATION，
// provider 从下一次 start 起上报其模型请求。
// Python: bind_trajectory_recorder (member_runtime.py:358)
func (r *ExternalHarnessMemberRuntime) BindTrajectoryRecorder(recorder *harnessproviders.HarnessTrajectoryRecorder) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.trajectory = recorder
}

// BindFallbackPromotion 在 provider 提交前持久化其认证回退。
// provider 经 auth_fallback provider 交互询问；仅当 promote 返回 true 时
// 切换才被批准，否则 provider 恢复其原生端点。
// Python: bind_fallback_promotion (member_runtime.py:367)
func (r *ExternalHarnessMemberRuntime) BindFallbackPromotion(promote PromoteFallbackModel) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.promoteFallbackModel = promote
}

// BindActivitySink 绑定接收本成员结构化 AGENT_ACTIVITY 事件的汇。
// 对齐 AgentBackend.bind_progress_sink：nil 保持 runtime 静默。身份元组随每个
// 事件携带，使消费者能按会话注入提供的同一 agent_id 定位节点。
// Python: bind_activity_sink (member_runtime.py:376)
func (r *ExternalHarnessMemberRuntime) BindActivitySink(
	sink func(event any), agentID, phase, label, correlationID string,
) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.activitySink = sink
	r.activityIdentity = [4]string{agentID, phase, label, correlationID}
}

// AddTeardownHook 在 harness 停止后执行 hook（可观测接收器等）。
// Python: add_teardown_hook (member_runtime.py:394)
func (r *ExternalHarnessMemberRuntime) AddTeardownHook(hook func(ctx context.Context) error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.teardownHooks = append(r.teardownHooks, hook)
}

// BindReliabilityContext 绑定可靠性失败/重试投递面。
// agent_kind 为空（可靠循环关闭）时直接返回；RuntimeReliabilityContext 归属
// external/reliability.py，Go 侧未移植——构造逻辑 ⤵️15.7/15.8（D-15.13-x 关联）。
// Python: bind_reliability_context (member_runtime.py:398)
func (r *ExternalHarnessMemberRuntime) BindReliabilityContext(
	_ string, _ any, _ string, _ any, _ any,
) {
	if r.agentKind == "" {
		return
	}
	// Python: self._reliability_ctx = RuntimeReliabilityContext(...) ⤵️15.7/15.8
}

// CheckpointToDict 把 checkpoint 信封序列化成纯 JSON 数据。
// Python: checkpoint_to_dict (member_runtime.py:155)
func CheckpointToDict(checkpoint *harness_protocol.HarnessCheckpoint) map[string]any {
	if checkpoint == nil {
		return nil
	}
	providerSessionID := ""
	if checkpoint.ProviderSessionID != nil {
		providerSessionID = *checkpoint.ProviderSessionID
	}
	revision := ""
	if checkpoint.Revision != nil {
		revision = *checkpoint.Revision
	}
	return map[string]any{
		"provider":            checkpoint.Provider,
		"schema_version":      checkpoint.SchemaVersion,
		"agent_id":            checkpoint.AgentID,
		"host_session_id":     checkpoint.HostSessionID,
		"checkpoint_id":       checkpoint.CheckpointID,
		"sequence":            checkpoint.Sequence,
		"data":                checkpoint.Data,
		"provider_session_id": providerSessionID,
		"revision":            revision,
	}
}

// CheckpointFromDict 反序列化 checkpoint_to_dict 写出的信封；形状非法时记
// Warn 并返回 nil（不报错）。
// Python: checkpoint_from_dict (member_runtime.py:170)
func CheckpointFromDict(value any) *harness_protocol.HarnessCheckpoint {
	m, ok := value.(map[string]any)
	if !ok {
		return nil
	}
	raw, err := json.Marshal(m)
	if err != nil {
		logger.Warn(logComponent).Err(err).Msg("ignoring malformed external runtime checkpoint")
		return nil
	}
	var payload struct {
		Provider          string `json:"provider"`
		SchemaVersion     string `json:"schema_version"`
		AgentID           string `json:"agent_id"`
		HostSessionID     string `json:"host_session_id"`
		CheckpointID      string `json:"checkpoint_id"`
		Sequence          *int   `json:"sequence"`
		Data              map[string]any
		ProviderSessionID *string `json:"provider_session_id"`
		Revision          *string `json:"revision"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		logger.Warn(logComponent).Err(err).Msg("ignoring malformed external runtime checkpoint")
		return nil
	}
	sequence := 0
	if payload.Sequence != nil {
		sequence = *payload.Sequence
	}
	return &harness_protocol.HarnessCheckpoint{
		Provider:          payload.Provider,
		SchemaVersion:     payload.SchemaVersion,
		AgentID:           payload.AgentID,
		HostSessionID:     payload.HostSessionID,
		CheckpointID:      payload.CheckpointID,
		Sequence:          sequence,
		Data:              payload.Data,
		ProviderSessionID: payload.ProviderSessionID,
		Revision:          payload.Revision,
	}
}

// ReadMemberCheckpoint 读取该后端持久化到成员会话的 checkpoint。
// Python: read_member_checkpoint (member_runtime.py:191)
func ReadMemberCheckpoint(memberSession sessioninterfaces.SessionFacade, backend string) *harness_protocol.HarnessCheckpoint {
	stateValue, err := memberSession.GetState(state.StringKey(externalRuntimeStateKey))
	if err != nil || stateValue == nil {
		return nil
	}
	m, ok := stateValue.(map[string]any)
	if !ok {
		return nil
	}
	if b, _ := m[externalBackendKey].(string); b != backend {
		return nil
	}
	return CheckpointFromDict(m[externalCheckpointKey])
}

// SetModelSelection 在下一轮之前把运行中的 harness 切到 selection。
// harness 已切换（或已排队切换）返回 true；未运行返回 false，此时选择只经
// 持久化的成员选项在下一次 start 生效。
// Python: set_model_selection (member_runtime.py:645)
func (r *ExternalHarnessMemberRuntime) SetModelSelection(
	ctx context.Context, selection harness_protocol.ModelSelection,
) (bool, error) {
	harness := r.harness
	if !harness.Card().Supports(harness_protocol.HarnessCapabilityModelSelection) {
		return false, &harness_protocol.UnsupportedHarnessCapabilityError{
			Msg: fmt.Sprintf("%s does not support model selection", harness.Card().Name),
		}
	}
	control, ok := harness.(harness_protocol.HarnessModelControl)
	if !ok || harness.State() == harness_protocol.HarnessStateTerminated {
		return false, nil
	}
	if err := control.SetModel(ctx, selection); err != nil {
		return false, err
	}
	return true, nil
}

// Subscribe 注册可选相位/轮次回调（与 cliRuntimeBase 同款契约）。
// Python: subscribe (member_runtime.py:661)
func (r *ExternalHarnessMemberRuntime) Subscribe(onState stateCallback, onRound roundCallback) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if onState != nil {
		r.onState = append(r.onState, onState)
	}
	if onRound != nil {
		r.onRound = append(r.onRound, onRound)
	}
}

// ──────────────────────────── 成员会话 / 团队上下文 ────────────────────────────

// resolveContext 解析上下文来源（工厂返回非 HarnessContext 形状由类型系统约束）。
// Python: _resolve_context (member_runtime.py:933)
func (r *ExternalHarnessMemberRuntime) resolveContext(teamSession any) (*harness_protocol.HarnessContext, error) {
	return r.contextSource(teamSession)
}

// ensureMemberSession 打开本成员的稳定子 AgentSession（一次）。
// 以 memberAgentID 为键，checkpoint 是本成员自己的，不会与兄弟冲突；
// 无团队会话或无 member_agent_id 时无持久化成员状态运行。
// Python: _ensure_member_session (member_runtime.py:944)
func (r *ExternalHarnessMemberRuntime) ensureMemberSession(teamSession any) (sessioninterfaces.SessionFacade, error) {
	r.mu.Lock()
	if r.memberSession != nil {
		s := r.memberSession
		r.mu.Unlock()
		return s, nil
	}
	agentID := r.memberAgentID
	r.mu.Unlock()
	if teamSession == nil || agentID == "" {
		return nil, nil
	}
	// Python hasattr(team_session, "create_agent_session")（member_runtime.py:947）
	// 的 Go 等价：接口形状与真实 AgentTeamSession.CreateAgentSession
	// （agent_team.go:371，card/agentID/shareStreamWriter/memberName 四参返回
	// *session.Session）逐参对齐——此前误断言两参签名，鸭子路径对真实团队
	// 会话恒不命中而静默返回 nil（Task 10 集成测试实施中发现的缺陷）。
	factory, ok := teamSession.(interface {
		CreateAgentSession(card *agentschema.AgentCard, agentID string, shareStreamWriter bool, memberName string) *session.Session
	})
	if !ok {
		return nil, nil
	}
	// Python create_agent_session(agent_id=..., share_stream_writer=False)：
	// card nil 由会话侧回落默认卡；member_name 不传（Python 无此实参）。
	var memberSession sessioninterfaces.SessionFacade = factory.CreateAgentSession(nil, agentID, false, "")
	if preRunner, ok := memberSession.(memberSessionPreRunner); ok {
		if err := preRunner.PreRun(context.Background()); err != nil {
			return nil, err
		}
	}
	r.mu.Lock()
	r.memberSession = memberSession
	r.mu.Unlock()
	return memberSession, nil
}

// finalizeMemberSession 收尾成员子会话（post_run 并清空槽位）。
// Python: _finalize_member_session (member_runtime.py:957)
func (r *ExternalHarnessMemberRuntime) finalizeMemberSession(ctx context.Context) error {
	r.mu.Lock()
	sess := r.memberSession
	r.mu.Unlock()
	if sess == nil {
		return nil
	}
	if preRunner, ok := sess.(memberSessionPreRunner); ok {
		if err := preRunner.PostRun(ctx); err != nil {
			return err
		}
	}
	r.mu.Lock()
	if r.memberSession == sess {
		r.memberSession = nil
	}
	r.mu.Unlock()
	return nil
}

// pendingTeamContext 渲染本成员尚未被告知的团队状态。
// Python: _pending_team_context (member_runtime.py:965)
func (r *ExternalHarnessMemberRuntime) pendingTeamContext() (string, bool) {
	r.mu.Lock()
	tr := r.teamContextTracker
	sess := r.memberSession
	r.mu.Unlock()
	if tr == nil || sess == nil {
		return "", false
	}
	return tr.PendingText(sess)
}

// commitTeamContext 成功发送后推进投递基线。
// Python: _commit_team_context (member_runtime.py:970)
func (r *ExternalHarnessMemberRuntime) commitTeamContext() {
	r.mu.Lock()
	tr := r.teamContextTracker
	sess := r.memberSession
	r.mu.Unlock()
	if tr == nil || sess == nil {
		return
	}
	tr.Commit(sess, nil)
}

// ──────────────────────────── 中断助手与 no-op 钩子 ────────────────────────────

// HasPendingInterrupt 是否有挂起交互（透传 adapter）。
// Python: has_pending_interrupt (member_runtime.py:979)
func (r *ExternalHarnessMemberRuntime) HasPendingInterrupt() bool {
	return r.adapter.HasPendingInterrupt()
}

// IsPendingInterruptResumeValid user_input 是否至少答到一个挂起交互。
// Python 收任意 user_input；Go 契约面是 *interaction.InteractiveInput，
// 断言失败（非交互输入形状）按无有效回答处理返回 false。
// Python: is_pending_interrupt_resume_valid (member_runtime.py:982)
func (r *ExternalHarnessMemberRuntime) IsPendingInterruptResumeValid(userInput any) bool {
	input, ok := userInput.(*interaction.InteractiveInput)
	if !ok {
		return false
	}
	return r.adapter.IsPendingInterruptResumeValid(input)
}

// InitCwdForRound no-op：外部 harness 自己持有工作目录。
// Python: init_cwd_for_round (member_runtime.py:987)
func (r *ExternalHarnessMemberRuntime) InitCwdForRound() {}

// FindRails 无 rail（外部 harness 自己持有 rail 面）。
// Python: find_rails (member_runtime.py:991)
func (r *ExternalHarnessMemberRuntime) FindRails(railType any) []any {
	_ = railType
	return []any{}
}

// RegisterRail no-op。
// Python: register_rail (member_runtime.py:995)
func (r *ExternalHarnessMemberRuntime) RegisterRail(rail any) error {
	_ = rail
	return nil
}

// UnregisterRail no-op。
// Python: unregister_rail (member_runtime.py:998)
func (r *ExternalHarnessMemberRuntime) UnregisterRail(rail any) error {
	_ = rail
	return nil
}

// RegisterMemberTools no-op：外部 harness 成员不用团队记忆工具包。
// Python: register_member_tools (member_runtime.py:1002)
func (r *ExternalHarnessMemberRuntime) RegisterMemberTools(memoryManager any) { _ = memoryManager }

// InjectMemberMemory no-op：外部 harness 成员不用团队记忆注入。
// Python: inject_member_memory (member_runtime.py:1005)
func (r *ExternalHarnessMemberRuntime) InjectMemberMemory(memoryManager any, query string) {
	_ = memoryManager
	_ = query
}

// SetBackgroundTaskController no-op：外部 harness 成员不启动需要控制的后台工具。
// Python: set_background_task_controller (member_runtime.py:1009)
func (r *ExternalHarnessMemberRuntime) SetBackgroundTaskController(controller any) { _ = controller }

// BackgroundTaskController 恒 nil：外部 harness 成员自己不持有后台控制器。
// Python: background_task_controller property (member_runtime.py:1013)
func (r *ExternalHarnessMemberRuntime) BackgroundTaskController() any { return nil }

// Workspace 外部 harness runtime 不暴露团队工作空间句柄。
// Python: workspace property (member_runtime.py:1018)
func (r *ExternalHarnessMemberRuntime) Workspace() any { return nil }

// SysOperation 外部 harness runtime 不暴露 sys_operation 句柄。
// Python: sys_operation property (member_runtime.py:1022)
func (r *ExternalHarnessMemberRuntime) SysOperation() any { return nil }

// ──────────────────────────── 非导出函数（纯助手） ────────────────────────────

// memberRoundKind 把协议轮次事件种类映射到团队轮次词汇。
// Python: _member_round_kind (member_runtime.py:1039)
func memberRoundKind(kind harness_protocol.TurnEventKind) string {
	switch kind {
	case harness_protocol.TurnEventKindStarted:
		return "started"
	case harness_protocol.TurnEventKindPaused:
		return "paused"
	case harness_protocol.TurnEventKindResumed:
		return "started"
	case harness_protocol.TurnEventKindFinished:
		return "finished"
	case harness_protocol.TurnEventKindAborted:
		return "aborted"
	case harness_protocol.TurnEventKindFailed:
		return "failed"
	default:
		return ""
	}
}

// activityName 把 provider 工具名收敛为字符串；缺席时返回空串。
// Python: _activity_name (member_runtime.py:1050)
func activityName(value any) string {
	if s, ok := value.(string); ok && s != "" {
		return s
	}
	return ""
}

// activityPreview 把工具调用/结果载荷渲染成紧凑单行预览。
// 字符串原样通过；结构化载荷单行 dump，让旁观者看到预览而不是一堵 JSON 墙。
// Python: _activity_preview (member_runtime.py:1057)
func activityPreview(value any) string {
	if value == nil {
		return ""
	}
	if s, ok := value.(string); ok {
		return s
	}
	b, err := json.Marshal(value)
	if err != nil {
		return fmt.Sprintf("%v", value)
	}
	return string(b)
}

// prependContext 把前缀文本并入 harness 输入内容：
// 字符串内容为 "prefix\n\nvalue"（value 空则只有前缀）；全 dict 的列表在头部
// 前置一个文本块；其他形状 JSON 序列化后拼接。metadata 原样保留。
// Python: _prepend_context (member_runtime.py:1026)
func prependContext(content harness_protocol.HarnessInput, prefix string) harness_protocol.HarnessInput {
	value := content.Content
	switch v := value.(type) {
	case string:
		combined := prefix
		if v != "" {
			combined = prefix + "\n\n" + v
		}
		out, err := harness_protocol.NewHarnessInput(combined, content.Metadata)
		if err != nil {
			return content
		}
		return out
	case []any:
		allDict := true
		for _, block := range v {
			if _, ok := block.(map[string]any); !ok {
				allDict = false
				break
			}
		}
		if allDict {
			combined := append([]any{map[string]any{"type": "text", "text": prefix}}, v...)
			out, err := harness_protocol.NewHarnessInput(combined, content.Metadata)
			if err != nil {
				return content
			}
			return out
		}
	}
	// 其他形状：JSON 序列化后拼接（Python json.dumps(value, ensure_ascii=False)）
	raw, err := json.Marshal(value)
	if err != nil {
		return content
	}
	out, err := harness_protocol.NewHarnessInput(prefix+"\n\n"+string(raw), content.Metadata)
	if err != nil {
		return content
	}
	return out
}

// ──────────────────────────── 暂缺清单（⤵️ provider 路 / 15.7/15.8） ────────────────────────────
//
// 以下 Python 方法依赖 RuntimeReliabilityContext（external/reliability.py）或
// activity emitter（workflow.backends.activity_emitter），Go 侧上游件未移植，
// 且其消费面（Task 8 BuildProviderMemberRuntime / 15.7/15.8 harness 构造）尚未
// 落地；随 provider 路实施一并移植，行号对齐 member_runtime.py：
//
//   - start / _start（:429/:435）——provider 周期启动与事件泵
//   - _host_context（:462）——宿主能力/checkpoint/resume/mcp/tools 注入
//   - _write_seed_checkpoint（:510）——宿主预置 checkpoint 落盘
//   - stop / _stop / dispose / outputs（:526/:532/:554/:557）
//   - send / announce_team_context / announce_task_revisions（:564/:604/:613）
//   - abort / pause / resume（:636/:639/:642）
//   - _on_event / _on_turn_event / _on_diagnostic / _on_tool_item（:676-800）
//   - _emit_activity / _on_provider_interaction / _persist_fallback（:803/:835/:843）
//   - _finalize_turn_failure / _finalize_startup_failure（:863/:872）
//   - _record_input / _record_event / _open_member_turn（:887/:897/:917）

package external

// 外部 harness SPI 到 AgentTeam MemberRuntime 的桥接：对齐 Python
// openjiuwen/agent_teams/external/member_runtime.py（1105 行）。
//
// HarnessIOAdapter 已把任意 HarnessProtocol 转成 DeepAgent 式输入/输出契约；
// 本文件在其上叠加团队专属层：成员子 AgentSession（checkpoint 汇与
// team-context 投递基线）、TeamContextTracker 搭载、外部 runtime 可靠性循环、
// 可选的轨迹记录器（经协议事件流供数）与认证回退晋升钩子。
//
// 生命周期/交互方法（Start/Stop/Outputs/Send/Abort/Pause/Resume）为转发实现
// （偏差 D-15.13-n）：折叠链与事件泵消费面（_on_event/_record_input 等）
// 已随 15.16a 成员事件循环补齐。

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"

	"github.com/google/uuid"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_teams/harness"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_teams/inboundtags"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_teams/messager"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_teams/schema"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_teams/tools"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_teams/tools/database"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_teams/tracker"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/session"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/session/interaction"
	sessioninterfaces "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/session/interfaces"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/session/state"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/session/stream"
	agentschema "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/single_agent/schema"
	harness_protocol "github.com/uapclaw/uapclaw-go/internal/openuapclaw/harness_protocol"
	harnessproviders "github.com/uapclaw/uapclaw-go/internal/openuapclaw/harness_providers"
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
	// reliabilityCtx 可靠性上下文（RuntimeReliabilityContext 实现见 reliability.go，
	// external/reliability.py 430 行已随 15.16b0 移植；SDK 流内 record_pending/
	// publish_retrying 挂点与 claude/codex 分类器仍 ⤴️15.7/15.8；经
	// BindReliabilityContext 构造注入，未绑定为 nil 对齐 Python `ctx is None` 分支）
	reliabilityCtx *RuntimeReliabilityContext
	// trajectory 把本成员事件流转成轨迹 span 的记录器
	trajectory *harnessproviders.HarnessTrajectoryRecorder
	// promoteFallbackModel 认证回退晋升回调
	promoteFallbackModel PromoteFallbackModel
	// teardownHooks harness 停止后执行的钩子（可观测接收器等）
	teardownHooks []func(ctx context.Context) error
	// asyncWG 异步可靠性派发的收口 WaitGroup（偏差 D-15.16-a2；Stop 的
	// teardown 段等待，防止收尾后 finalize 仍写状态）
	asyncWG sync.WaitGroup
	// roundSeq / currentRoundID 轮次计数
	roundSeq       int
	currentRoundID int
	// reportedModel 成员 CLI 报告的运行模型（空 = 尚未见过）
	reportedModel string
	// activitySink 结构化 AGENT_ACTIVITY 事件汇；nil 保持静默
	activitySink func(*AgentActivityEvent)
	// activityIdentity 随每个事件携带的身份元组
	// (agent_id, phase, label, correlation_id)
	activityIdentity [4]string
	// onState/onRound 订阅回调（与 cliRuntimeBase 同款契约）
	onState []stateCallback
	onRound []roundCallback

	// lifecycleMu 生命周期串行锁（Python _lifecycle_lock，start/stop 互斥）
	lifecycleMu sync.Mutex
	// contextDeliveryMu 团队上下文投递串行锁（Python _context_delivery_lock，
	// start/stop/send/announce 共用）
	contextDeliveryMu sync.Mutex
	// outputsOnce/outputsCh/outputsStop Outputs 通道惰性装配（单次泵 goroutine
	// 把 adapter 输出迭代器桥接到 channel 面，Stop 时收编）
	outputsOnce sync.Once
	outputsCh   chan stream.OutputSchema
	outputsStop chan struct{}
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
	r := &ExternalHarnessMemberRuntime{
		harness:               harness,
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
	// 每条原始事件在投影前进成员事件泵（Python 构造处
	// event_observer=self._on_event，member_runtime.py:253-258）。
	adapter := harnessproviders.NewHarnessIOAdapter(harness, harnessproviders.HarnessIOAdapterOptions{
		AutoApproveTools:            true,
		StopOnUnsupportedForceAbort: opts.StopOnUnsupportedForceAbort,
		EventObserver:               r.onEvent,
		// provider 扩展请求宿主应答：仅认 auth_fallback（Python
		// provider_interaction_handler=self._on_provider_interaction，
		// member_runtime.py:256-257）
		ProviderInteractionHandler: func(_ context.Context,
			request *harness_protocol.ProviderInteractionRequest) (*harness_protocol.ProviderInteractionResponse, error) {
			return r.onProviderInteraction(request), nil
		},
	})
	r.adapter = adapter
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
	sink func(*AgentActivityEvent), agentID, phase, label, correlationID string,
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
// agent_kind 为空（可靠循环关闭）时直接返回；team_backend 为 None 时 team_name
// 为空串、message_manager 为 nil（失败消息走未投递日志路径）。
// Python: bind_reliability_context (member_runtime.py:398-421)
func (r *ExternalHarnessMemberRuntime) BindReliabilityContext(
	sessionID string,
	teamBackend *tools.TeamBackend,
	leaderName string,
	updateStatusCB UpdateStatusCallback,
	msgr messager.Messager,
) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.agentKind == "" {
		return
	}
	teamName := ""
	var messageMgr *tools.TeamMessageManager
	if teamBackend != nil {
		teamName = teamBackend.TeamName()
		messageMgr = teamBackend.MessageManager()
	}
	// Python: self._reliability_ctx = RuntimeReliabilityContext(...) (member_runtime.py:412-421)
	r.reliabilityCtx = NewRuntimeReliabilityContext(
		r.memberName, teamName, sessionID, r.agentKind,
		messageMgr, msgr, leaderName, updateStatusCB,
		r.trajectory, r.cliPath,
	)
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
// 参数用字面 func 签名承载（agent.MemberRuntime 接口面），落库时转回
// stateCallback/roundCallback 命名类型。
// Python: subscribe (member_runtime.py:661)
func (r *ExternalHarnessMemberRuntime) Subscribe(
	onState func(old, new harness_protocol.HarnessState, sessionID string),
	onRound func(kind string, roundID int, turnID string, result *harness_protocol.TurnResult),
) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if onState != nil {
		r.onState = append(r.onState, stateCallback(onState))
	}
	if onRound != nil {
		r.onRound = append(r.onRound, roundCallback(onRound))
	}
}

// ──────────────────────────── 生命周期（转发实现，D-15.13-n） ────────────────────────────

// Start 启动 provider 周期。转发实现：Python _start 的宿主能力注入已并入
// resolveContext（spec A.4 归并注记）、可靠性 begin_attempt 因
// RuntimeReliabilityContext 实现类 ⤴️15.7/15.8 而恒跳过（D-15.13-m，
// ctx 恒 nil 对齐 Python `is not None` 不命中分支）；启动失败路径走
// _finalize_startup_failure（15.16a 已落）。
//
// Python: start/_start (member_runtime.py:429/:435)
func (r *ExternalHarnessMemberRuntime) Start(teamSession any) error {
	r.lifecycleMu.Lock()
	defer r.lifecycleMu.Unlock()
	// Python: if not self._stopped: raise HarnessStateError（:436-437）
	r.mu.Lock()
	stopped := r.stopped
	r.mu.Unlock()
	if !stopped {
		return fmt.Errorf("external harness member runtime is already started")
	}
	// Python: async with _context_delivery_lock: await _finalize_member_session()（:439-440）
	r.contextDeliveryMu.Lock()
	err := r.finalizeMemberSession(context.Background())
	r.contextDeliveryMu.Unlock()
	if err != nil {
		return err
	}
	// Python: context = await self._resolve_context(team_session)（:441）
	hctx, err := r.resolveContext(teamSession)
	if err != nil {
		return err
	}
	// Python: context = self._host_context(context)（:449）的 mcp 半区
	// （member_runtime.py:492-495）：bind_mcp_servers 注入的团队 server 合并
	// 进下发 context 并声明宿主 MCP_SERVERS 能力。checkpoint/resume/tools
	// 半区在 Go 侧由 writeSeedCheckpoint / ResumeExternalBackend 等位点承担。
	// 此前 extraMCPServers 只存不读（偏差补齐），provider 成员拿不到团队工具。
	hctx = r.mergeHostMcpServers(hctx)
	// Python: self._member_name/_member_agent_id = context...（:442-443）
	r.mu.Lock()
	r.memberName = hctx.AgentName
	r.memberAgentID = hctx.AgentID
	r.mu.Unlock()
	// Python: await self._ensure_member_session(team_session)（:444）
	memberSession, err := r.ensureMemberSession(teamSession)
	if err != nil {
		return err
	}
	// Python: seed checkpoint 落盘后清槽（:445-448）
	if r.seedCheckpoint != nil && memberSession != nil {
		if err := r.writeSeedCheckpoint(hctx, memberSession); err != nil {
			return err
		}
		r.mu.Lock()
		r.seedCheckpoint = nil
		r.mu.Unlock()
	}
	// Python: context = self._host_context(context)（:449）——宿主能力注入
	// 已并入 resolveContext（spec A.4）。
	// Python: if self._reliability_ctx is not None: begin_attempt(phase="startup",
	// round_id=None)（member_runtime.py:448-449）
	if ctx := r.reliabilityCtxSnap(); ctx != nil {
		ctx.BeginAttempt(schema.ExternalRuntimePhaseStartup, nil)
	}
	// Python: await self._adapter.start(context)（:452）
	if err := r.adapter.Start(context.Background(), *hctx); err != nil {
		// Python except 分支：_finalize_startup_failure + 成员会话收尾（:453-458）。
		r.finalizeStartupFailure(err)
		r.contextDeliveryMu.Lock()
		_ = r.finalizeMemberSession(context.Background())
		r.contextDeliveryMu.Unlock()
		return err
	}
	// Python: self._stopped = False（:459）
	r.mu.Lock()
	r.stopped = false
	r.mu.Unlock()
	return nil
}

// writeSeedCheckpoint 把宿主预置的 checkpoint 指针落成成员自己的 checkpoint
// 信封（写进成员子会话状态）。
//
// Python: _write_seed_checkpoint (member_runtime.py:510)
func (r *ExternalHarnessMemberRuntime) writeSeedCheckpoint(
	hctx *harness_protocol.HarnessContext,
	memberSession sessioninterfaces.SessionFacade,
) error {
	r.mu.Lock()
	seed := r.seedCheckpoint
	backend := r.harness.Card().Name
	r.mu.Unlock()
	data, err := harness_protocol.FreezeJsonObject(seed)
	if err != nil {
		return err
	}
	checkpoint := &harness_protocol.HarnessCheckpoint{
		Provider:      backend,
		SchemaVersion: "1",
		AgentID:       hctx.AgentID,
		HostSessionID: hctx.HostSessionID,
		CheckpointID:  "seed-" + strings.ReplaceAll(uuid.New().String(), "-", ""),
		Sequence:      0,
		Data:          data,
	}
	sink := NewMemberSessionCheckpointSink(memberSession, backend)
	_, err = sink.Save(checkpoint, harness_protocol.CheckpointReasonSessionActivated, "")
	return err
}

// Stop 停止 provider 并关闭投影的 MemberRuntime 输出。幂等。
// Go 无事件总线（回调注册表即订阅面），Python _events.unregister_namespace
// （member_runtime.py:536）为 no-op（登记注记）。
//
// Python: stop/_stop (member_runtime.py:526/:532)
func (r *ExternalHarnessMemberRuntime) Stop() {
	r.lifecycleMu.Lock()
	defer r.lifecycleMu.Unlock()
	r.mu.Lock()
	stopped := r.stopped
	r.mu.Unlock()
	if stopped {
		r.contextDeliveryMu.Lock()
		_ = r.finalizeMemberSession(context.Background())
		r.contextDeliveryMu.Unlock()
		return
	}
	// Python: try: await self._adapter.stop()（:533-534）——错误在 Python 于
	// finally 前上抛；转发面记 Warn 保持收尾顺序完整（D-15.13-n）。
	if err := r.adapter.Stop(context.Background()); err != nil {
		logger.Warn(logComponent).Err(err).
			Str("member", r.memberName).
			Msg("[external-cli] harness stop failed")
	}
	// Python finally 块（:535-545）：事件命名空间注销（Go no-op）、stopped
	// 置位、轨迹收口、teardown 钩子逐个执行（失败记日志不上抛）。
	r.mu.Lock()
	r.stopped = true
	traj := r.trajectory
	hooks := append([]func(ctx context.Context) error(nil), r.teardownHooks...)
	r.mu.Unlock()
	if traj != nil {
		// 停在轮中不会发出终局轮事件；把记录器仍持有的收口掉。
		traj.Close()
	}
	for _, hook := range hooks {
		if err := hook(context.Background()); err != nil {
			logger.Error(logComponent).Err(err).
				Str("member", r.memberName).
				Msg("external harness teardown hook failed")
		}
	}
	// 异步可靠性派发收口（偏差 D-15.16-a2）：收尾后不再有 finalize 写状态。
	r.asyncWG.Wait()
	// Go channel 面收尾：停泵并关闭 outputs（Python _END 哨兵语义）。
	r.terminateOutputs()
	r.contextDeliveryMu.Lock()
	_ = r.finalizeMemberSession(context.Background())
	r.contextDeliveryMu.Unlock()
}

// terminateOutputs 停止 outputs 泵 goroutine 并关闭通道（未装配则 no-op）。
func (r *ExternalHarnessMemberRuntime) terminateOutputs() {
	if r.outputsStop != nil {
		select {
		case <-r.outputsStop:
		default:
			close(r.outputsStop)
		}
	}
}

// ──────────────────────────── 交互（转发实现，D-15.13-n） ────────────────────────────

// Outputs 返回队列背书的输出块通道。Python 直接返回 adapter.outputs() 的
// async 迭代器；Go 接口面是 channel（cliRuntimeBase 同款），此处以惰性泵
// goroutine 把 adapter 输出迭代器桥接到 channel——Python 无此中间层，登记于
// D-15.13-n。Stop 收编泵并关通道（Python _END 哨兵语义）。
//
// Python: outputs (member_runtime.py:557)
func (r *ExternalHarnessMemberRuntime) Outputs() <-chan stream.OutputSchema {
	r.outputsOnce.Do(func() {
		ch := make(chan stream.OutputSchema)
		stop := make(chan struct{})
		r.outputsCh = ch
		r.outputsStop = stop
		it := r.adapter.Outputs()
		go func() {
			defer close(ch)
			for {
				item, ok := it.Next(context.Background())
				if !ok {
					return
				}
				// adapter 输出队列条目恒为 OutputSchema 投影块。
				chunk, ok := item.(stream.OutputSchema)
				if !ok {
					continue
				}
				select {
				case ch <- chunk:
				case <-stop:
					return
				}
			}
		}()
	})
	return r.outputsCh
}

// Send 能力感知地提交输入：pending 团队状态与任务通知折叠进同一消息。
// 折叠链（时钟锚/任务通知/团队上下文）与输入登记（record_input 系列）
// 对齐 Python send 逐步；InteractiveInput 直投（应答挂起中断）。
//
// Python: send (member_runtime.py:564)
func (r *ExternalHarnessMemberRuntime) Send(content any, immediate bool) (any, error) {
	r.contextDeliveryMu.Lock()
	defer r.contextDeliveryMu.Unlock()
	// Python: isinstance(content, InteractiveInput) → adapter 直投（:569-570）
	if _, ok := content.(*interaction.InteractiveInput); ok {
		return r.adapter.Send(context.Background(), content, immediate)
	}
	// Python: external_input = to_harness_input(content)（:571）
	externalInput, err := harnessproviders.ToHarnessInput(content)
	if err != nil {
		return nil, err
	}
	// Python: delivered_text = harness_input_text(external_input)（:572）
	// ——须在 prepend 前取，登记的正文才与时钟/上下文块区分开。
	deliveredText := harnessproviders.HarnessInputText(externalInput)
	// Python: external_input = _prepend_context(external_input, clock)（:575-576）
	clock := inboundtags.RenderClock(inboundtags.FormatAbsoluteTime(database.GetCurrentTime()))
	externalInput = prependContext(externalInput, clock)
	// Python: pending_outcome = await tracker.claim(...)（:578-579）
	r.mu.Lock()
	outcomeTracker := r.taskOutcomeTracker
	sess := r.memberSession
	r.mu.Unlock()
	var pendingOutcome *tracker.ClaimedNotice
	if outcomeTracker != nil && sess != nil {
		pendingOutcome = outcomeTracker.Claim(sess)
	}
	if pendingOutcome != nil {
		externalInput = prependContext(externalInput, pendingOutcome.Text)
	}
	// Python: pending_context = await self._pending_team_context()（:581-582）
	pendingContext, _ := r.pendingTeamContext()
	if pendingContext != "" {
		externalInput = prependContext(externalInput, pendingContext)
	}
	// Python: receipt = await self._adapter.send(...)；失败 release（:584-589）
	receipt, err := r.adapter.Send(context.Background(), externalInput, immediate)
	if err != nil {
		if pendingOutcome != nil && outcomeTracker != nil {
			outcomeTracker.Release(pendingOutcome)
		}
		return nil, err
	}
	// Python: record_input 系列（:594-602）。每一段都登记，且消息最先——
	// 它是轮次状态所认定的输入；provider 会把投递拆成每块一条消息，
	// 合并文本会与它们全都不匹配，轮次会读作"没人说过话"。
	// 失败回收（release）已在上方 except 分支。
	r.recordInput(receipt, deliveredText)
	r.recordInput(receipt, clock)
	if pendingOutcome != nil {
		r.recordInput(receipt, pendingOutcome.Text)
		if outcomeTracker != nil && sess != nil {
			outcomeTracker.Persist(sess)
		}
	}
	if pendingContext != "" {
		r.recordInput(receipt, pendingContext)
		r.commitTeamContext()
	}
	return receipt, nil
}

// AnnounceTeamContext 把待告知团队状态作为独立消息投递；无新内容 no-op。
//
// Python: announce_team_context (member_runtime.py:604)
func (r *ExternalHarnessMemberRuntime) AnnounceTeamContext() error {
	r.contextDeliveryMu.Lock()
	defer r.contextDeliveryMu.Unlock()
	pending, _ := r.pendingTeamContext()
	if pending == "" {
		return nil
	}
	receipt, err := r.adapter.Send(context.Background(), pending, false)
	if err != nil {
		return err
	}
	r.recordInput(receipt, pending)
	r.commitTeamContext()
	return nil
}

// AnnounceTaskRevisions 把待发任务编辑通知作为独立消息投递（F_129）：
// steer 忙碌成员；tracker 无新内容时 no-op。已投递的通知登记为该轮输入
// （record_input，15.16a 已落）。
//
// Python: announce_task_revisions (member_runtime.py:613)
func (r *ExternalHarnessMemberRuntime) AnnounceTaskRevisions() error {
	r.mu.Lock()
	outcomeTracker := r.taskOutcomeTracker
	sess := r.memberSession
	r.mu.Unlock()
	if outcomeTracker == nil {
		return nil
	}
	r.contextDeliveryMu.Lock()
	defer r.contextDeliveryMu.Unlock()
	var delivered *harness_protocol.SendReceipt
	var deliveredText string
	_, err := outcomeTracker.Announce(sess, func(text string) error {
		receipt, err := r.adapter.Send(context.Background(), text, true)
		if err == nil {
			// Python: delivered = (await _adapter.send(text, immediate=True), text)；
			// 已投递的通知是本轮的输入，无论其后发生什么（member_runtime.py:629-634）。
			delivered = receipt
			deliveredText = text
		}
		return err
	})
	if delivered != nil {
		r.recordInput(delivered, deliveredText)
	}
	return err
}

// Abort 中止在飞轮次（能力感知，经 adapter）。Python await _adapter.abort
// （member_runtime.py:636）；接口面无返回（与 cliRuntimeBase 契约一致），
// 失败记 Warn。
func (r *ExternalHarnessMemberRuntime) Abort(immediate bool) {
	if err := r.adapter.Abort(context.Background(), immediate); err != nil {
		logger.Warn(logComponent).Err(err).
			Str("member", r.memberName).
			Bool("immediate", immediate).
			Msg("[external-cli] harness abort failed")
	}
}

// Pause 暂停在飞轮次（支持 pause_resume 能力时生效）。
// Python: pause (member_runtime.py:639)
func (r *ExternalHarnessMemberRuntime) Pause() {
	if err := r.adapter.Pause(context.Background()); err != nil {
		logger.Warn(logComponent).Err(err).
			Str("member", r.memberName).
			Msg("[external-cli] harness pause failed")
	}
}

// Resume 恢复热的或 checkpoint 还原的暂停轮次；query 透传 adapter。
// Python: resume (member_runtime.py:642)
func (r *ExternalHarnessMemberRuntime) Resume(query any) error {
	return r.adapter.Resume(context.Background(), query)
}

// ──────────────────────────── 成员会话 / 团队上下文 ────────────────────────────

// resolveContext 解析上下文来源（工厂返回非 HarnessContext 形状由类型系统约束）。
// Python: _resolve_context (member_runtime.py:933)
func (r *ExternalHarnessMemberRuntime) resolveContext(teamSession any) (*harness_protocol.HarnessContext, error) {
	return r.contextSource(teamSession)
}

// mergeHostMcpServers 把 BindMCPServers 追加的团队 server 合并进下发
// context（Python: _host_context 的 mcp 半区，member_runtime.py:492-506）：
// mcp_servers = context.mcp_servers + extraMCPServers；非空即声明宿主
// HostCapability.MCP_SERVERS。provider 据此把 server 翻译成自己的
// CLI 配置（如 codebuddy 的 --mcp-config）。
func (r *ExternalHarnessMemberRuntime) mergeHostMcpServers(
	hctx *harness_protocol.HarnessContext) *harness_protocol.HarnessContext {
	r.mu.Lock()
	extra := append([]harness_protocol.McpServerConfig(nil), r.extraMCPServers...)
	r.mu.Unlock()
	if len(extra) == 0 {
		return hctx
	}
	servers := append(append([]harness_protocol.McpServerConfig(nil), hctx.McpServers...), extra...)
	hctx.McpServers = servers
	capabilities := hctx.HostCapabilities
	if capabilities == nil {
		capabilities = map[harness_protocol.HostCapability]bool{}
	}
	capabilities[harness_protocol.HostCapabilityMcpServers] = true
	hctx.HostCapabilities = capabilities
	return hctx
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

// failureReason 构造结构化失败原因；字符串实参经 or-default 空串、
// http_status 仅接受整数（int/整值 float64，其余 nil）。
// Python 关键字参数 → Go 定位参数（顺序 message/code/sdk_error_type/http_status）。
// Python: _failure_reason (member_runtime.py:1070)
func failureReason(message string, code, sdkErrorType, httpStatus any) *schema.ExternalRuntimeFailureReason {
	return schema.NewExternalRuntimeFailureReason(
		message,
		anyToStr(sdkErrorType),
		anyToStr(code),
		httpStatusPtr(httpStatus),
	)
}

// classifyFailure 把协议 TurnError 翻译进团队可靠性词汇。
// 类别仅在 USER_ACTION_REQUIRED 闭集内保留，否则 sdk_error；
// provider_data 提供 sdk_error_type/http_status。
// Python: _classify (member_runtime.py:1081)
func classifyFailure(err *harness_protocol.TurnError) (schema.ExternalRuntimeFailureCategory, *schema.ExternalRuntimeFailureReason) {
	if err == nil {
		return schema.ExternalRuntimeFailureCategorySDKError,
			failureReason("external harness turn failed without a structured error", nil, nil, nil)
	}
	category := schema.ExternalRuntimeFailureCategorySDKError
	if err.Category != nil {
		if _, ok := schema.UserActionRequiredMap[schema.ExternalRuntimeFailureCategory(*err.Category)]; ok {
			category = schema.ExternalRuntimeFailureCategory(*err.Category)
		}
	}
	providerData, _ := harness_protocol.JsonValueToBuiltin(err.ProviderData).(map[string]any)
	data := providerData
	if data == nil {
		data = map[string]any{}
	}
	return category, failureReason(err.Message, err.Code, data["sdk_error_type"], data["http_status"])
}

// anyToStr 把 any 收敛成字符串；缺席/非串返回空串（Python str(x or "")）。
// *string（如 TurnError.Code）解引用后取值。
func anyToStr(value any) string {
	switch v := value.(type) {
	case string:
		return v
	case *string:
		if v != nil {
			return *v
		}
	}
	return ""
}

// httpStatusPtr 把 http_status 收敛成 *int；int/整值 float64 收 int，其余 nil
// （Python `isinstance(http_status, int)`；JSON 往返会把整数解成 float64）。
func httpStatusPtr(value any) *int {
	switch v := value.(type) {
	case int:
		return &v
	case *int:
		return v
	case int64:
		i := int(v)
		return &i
	case float64:
		if v == float64(int(v)) {
			i := int(v)
			return &i
		}
	}
	return nil
}

// onEvent 协议事件信封总入口：先轨迹记账，再按 payload 类型分派。
// 由 HarnessIOAdapter 的 EventObserver 缝逐信封调用（io_adapter.go:757）。
// 同步段：轨迹记账；异步段：可靠性 finalize（偏差 D-15.16-a2）。
// 值/指针两形状都接受（对齐 io_adapter.go:776 既有分派惯例）。
// Python: _on_event (member_runtime.py:676)
func (r *ExternalHarnessMemberRuntime) onEvent(_ context.Context, envelope harness_protocol.HarnessEvent) error {
	r.recordEvent(envelope)
	switch payload := envelope.Event.(type) {
	case harness_protocol.StateChangedEvent:
		r.triggerState(payload.Old, payload.New, envelope.ProviderSessionID)
	case *harness_protocol.StateChangedEvent:
		if payload != nil {
			r.triggerState(payload.Old, payload.New, envelope.ProviderSessionID)
		}
	case harness_protocol.TurnLifecycleEvent:
		r.onTurnEvent(envelope, &payload)
	case *harness_protocol.TurnLifecycleEvent:
		if payload != nil {
			r.onTurnEvent(envelope, payload)
		}
	case harness_protocol.DiagnosticEvent:
		r.onDiagnostic(&payload)
	case *harness_protocol.DiagnosticEvent:
		if payload != nil {
			r.onDiagnostic(payload)
		}
	case harness_protocol.ItemLifecycleEvent:
		r.onToolItem(envelope, &payload)
	case *harness_protocol.ItemLifecycleEvent:
		if payload != nil {
			r.onToolItem(envelope, payload)
		}
	case harness_protocol.ProviderEvent:
		r.onProviderEvent(envelope, &payload)
	case *harness_protocol.ProviderEvent:
		if payload != nil {
			r.onProviderEvent(envelope, payload)
		}
	}
	return nil
}

// onProviderEvent provider 扩展事件：AUTH_FALLBACK 走日志；模型变更/初始化
// 记录成员实际服务的端点模型（swarmflow workers 无可靠性上下文时也须记在
// runtime 本身）。
// Python: _on_event 的 ProviderEvent 分支 (member_runtime.py:691-708)
func (r *ExternalHarnessMemberRuntime) onProviderEvent(_ harness_protocol.HarnessEvent, payload *harness_protocol.ProviderEvent) {
	if payload.EventType == harness_protocol.AuthFallbackEventType {
		logger.Info(logComponent).
			Str("member", r.memberNameSnap()).
			Str("payload", fmt.Sprintf("%v", harness_protocol.JsonValueToBuiltin(payload.Payload))).
			Msg("[external-cli] member switched to the authentication fallback")
		return
	}
	if payload.EventType != modelChangedEvent && payload.EventType != systemInitEvent {
		return
	}
	model := providerEventModel(payload.Payload)
	if model == "" {
		return
	}
	r.mu.Lock()
	r.reportedModel = model
	r.mu.Unlock()
	if ctx := r.reliabilityCtxSnap(); ctx != nil {
		ctx.UpdateModel(&model)
	}
}

// providerEventModel 从 provider 载荷取非空 model 字符串（TrimSpace）。
// Python: payload.payload.get("model") + isinstance(str) + strip
func providerEventModel(payload harness_protocol.JsonObject) string {
	if payload == nil {
		return ""
	}
	m, _ := harness_protocol.JsonValueToBuiltin(harness_protocol.JsonValue(payload)).(map[string]any)
	if m == nil {
		return ""
	}
	s, _ := m["model"].(string)
	return strings.TrimSpace(s)
}

// onTurnEvent 轮次生命周期：STARTED 计数+begin_attempt；FAILED finalize（异步）；
// 末尾触发 round 回调。Python: _on_turn_event (member_runtime.py:714)
func (r *ExternalHarnessMemberRuntime) onTurnEvent(envelope harness_protocol.HarnessEvent, payload *harness_protocol.TurnLifecycleEvent) {
	kind := memberRoundKind(payload.Kind)
	if payload.Kind == harness_protocol.TurnEventKindStarted {
		r.mu.Lock()
		r.roundSeq++
		roundID := r.roundSeq
		r.mu.Unlock()
		if ctx := r.reliabilityCtxSnap(); ctx != nil {
			ctx.BeginAttempt(schema.ExternalRuntimePhaseTurn, &roundID)
		}
	}
	if payload.Kind == harness_protocol.TurnEventKindFailed {
		// 异步派发：finalize 涉及信箱/状态写，不阻塞事件流（D-15.16-a2）。
		result := payload.Result
		r.asyncGo(func() { r.finalizeTurnFailure(result) })
	}
	r.mu.Lock()
	roundID := r.currentRoundID
	r.mu.Unlock()
	r.triggerRound(kind, roundID, envelope.TurnID, payload.Result)
}

// onDiagnostic 诊断事件：仅 kind=retrying 发布重试进度。
// Python: _on_diagnostic (member_runtime.py:730)
func (r *ExternalHarnessMemberRuntime) onDiagnostic(payload *harness_protocol.DiagnosticEvent) {
	ctx := r.reliabilityCtxSnap()
	if ctx == nil {
		return
	}
	data, _ := harness_protocol.JsonValueToBuiltin(payload.Data).(map[string]any)
	if data == nil || data["kind"] != "retrying" {
		return
	}
	category, _ := data["category"].(string)
	if category == "" {
		category = string(schema.ExternalRuntimeFailureCategorySDKError)
	}
	reason := failureReason(payload.Message, data["code"], nil, nil)
	summary := fmt.Sprintf("%s %s SDK retrying: %s", r.memberNameSnap(), r.agentKind, category)
	_ = ctx.PublishRetrying(schema.ExternalRuntimeFailureCategory(category), reason, summary)
}

// triggerState 触发已注册的 state 回调（快照切片后锁外直调，沿 emitRound 惯例）。
// Python: _events.trigger(_EVENT_STATE, old, new, session_id) (member_runtime.py:683-687)
func (r *ExternalHarnessMemberRuntime) triggerState(old, new harness_protocol.HarnessState, sessionID string) {
	r.mu.Lock()
	cbs := append([]stateCallback(nil), r.onState...)
	r.mu.Unlock()
	for _, cb := range cbs {
		cb(old, new, sessionID)
	}
}

// triggerRound 触发已注册的 round 回调（快照切片后锁外直调）。
// Python: _events.trigger(_EVENT_ROUND, kind, round_id, result) (member_runtime.py:744-749)
func (r *ExternalHarnessMemberRuntime) triggerRound(kind string, roundID int, turnID string, result *harness_protocol.TurnResult) {
	r.mu.Lock()
	cbs := append([]roundCallback(nil), r.onRound...)
	r.mu.Unlock()
	for _, cb := range cbs {
		cb(kind, roundID, turnID, result)
	}
}

// reliabilityCtxSnap 快照 reliabilityCtx（锁内读）。
func (r *ExternalHarnessMemberRuntime) reliabilityCtxSnap() *RuntimeReliabilityContext {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.reliabilityCtx
}

// memberNameSnap 快照成员名（锁内读）。
func (r *ExternalHarnessMemberRuntime) memberNameSnap() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.memberName
}

// asyncGo 受控异步派发：WaitGroup 在 Stop 的 teardown 段收口，panic recover
// 吞为静默（Python 协程内异常由事件循环兜底）。
func (r *ExternalHarnessMemberRuntime) asyncGo(fn func()) {
	r.asyncWG.Add(1)
	go func() {
		defer r.asyncWG.Done()
		defer func() { _ = recover() }()
		fn()
	}()
}

// onToolItem 把一个 tool 类 item 转发为 tool_call/tool_result 对半边。
// 两 provider 已把原生工具事件归一成此形状——Claude 的 tool_use/tool_result
// 内容块与 Codex 的 item/started·completed（tool 类 item kinds）——信封 item_id
// 承载配对键。永远配不上的另一半（无配对 id / 数据非 dict / kind 非生命周期
// 端点）以 debug 丢弃，绝不上抛。
// Python: _on_tool_item (member_runtime.py:741)
func (r *ExternalHarnessMemberRuntime) onToolItem(envelope harness_protocol.HarnessEvent, payload *harness_protocol.ItemLifecycleEvent) {
	if payload.ItemType != "tool" {
		return
	}
	data, _ := harness_protocol.JsonValueToBuiltin(payload.Data).(map[string]any)
	if data == nil {
		r.debugDrop("dropped a tool item with unparsable data", payload.Kind, envelope.ItemID)
		return
	}
	if envelope.ItemID == "" {
		r.debugDrop("dropped an id-less tool item", payload.Kind, envelope.ItemID)
		return
	}
	name := activityName(data["tool_name"])
	if name == "" {
		// Python None：string 收窄后空串承载缺席
		r.debugDrop("dropped a tool item without a tool name", payload.Kind, envelope.ItemID)
		return
	}
	var activityType, content string
	switch payload.Kind {
	case harness_protocol.ItemEventKindStarted:
		activityType, content = "tool_call", activityPreview(data["arguments"])
	case harness_protocol.ItemEventKindCompleted:
		activityType, content = "tool_result", activityPreview(data["result"])
	default:
		r.debugDrop("dropped a tool item with an unmapped kind", payload.Kind, envelope.ItemID)
		return
	}
	r.emitActivity(activityType, envelope.ItemID, &name, &content)
}

// debugDrop 四条 drop 分支的统一 debug 记录。
func (r *ExternalHarnessMemberRuntime) debugDrop(reason string, kind harness_protocol.ItemEventKind, itemID string) {
	logger.Debug(logComponent).
		Str("member", r.memberNameSnap()).
		Str("kind", string(kind)).
		Str("item_id", itemID).
		Msg("[external-cli] " + reason)
}

// emitActivity 构造结构化 AGENT_ACTIVITY 事件并推 sink；构造/推送异常吞为 Debug
// （Python except Exception: team_logger.debug，member_runtime.py:822-828）。
// Python: _emit_activity (member_runtime.py:803)
func (r *ExternalHarnessMemberRuntime) emitActivity(activityType, toolCallID string, toolName, content *string) {
	r.mu.Lock()
	sink := r.activitySink
	id := r.activityIdentity
	r.mu.Unlock()
	if sink == nil {
		return
	}
	func() {
		defer func() { _ = recover() }()
		sink(BuildActivityEvent(activityType, toolCallID, toolName, content, id[0], id[1], id[2], id[3]))
	}()
}

// onProviderInteraction 应答 provider 扩展请求；仅认 auth_fallback。
// Python: _on_provider_interaction (member_runtime.py:835)
func (r *ExternalHarnessMemberRuntime) onProviderInteraction(request *harness_protocol.ProviderInteractionRequest) *harness_protocol.ProviderInteractionResponse {
	if request.RequestType != harness_protocol.AuthFallbackRequestType {
		return &harness_protocol.ProviderInteractionResponse{
			RequestID: request.RequestID,
			Status:    harness_protocol.InteractionResponseStatusDeclined,
		}
	}
	status := harness_protocol.InteractionResponseStatusDeclined
	if r.persistFallback() {
		status = harness_protocol.InteractionResponseStatusCompleted
	}
	return &harness_protocol.ProviderInteractionResponse{
		RequestID: request.RequestID,
		Status:    status,
	}
}

// persistFallback 把 fallback 模型持久化为成员活动模型。
// Python: _persist_fallback (member_runtime.py:843)
func (r *ExternalHarnessMemberRuntime) persistFallback() bool {
	r.mu.Lock()
	promote := r.promoteFallbackModel
	r.mu.Unlock()
	if promote == nil {
		return true
	}
	promoted, err := promote(context.Background())
	if err != nil {
		logger.Error(logComponent).Err(err).
			Str("member", r.memberNameSnap()).
			Msg("[external-cli] failed to persist the authentication fallback")
		return false
	}
	if !promoted {
		logger.Warn(logComponent).
			Str("member", r.memberNameSnap()).
			Msg("[external-cli] could not persist the authentication fallback; the provider keeps its native endpoint")
	}
	return promoted
}

// finalizeTurnFailure 以可靠性词汇收口一次轮次失败；已 finalize 短路。
// Python: _finalize_turn_failure (member_runtime.py:863)
func (r *ExternalHarnessMemberRuntime) finalizeTurnFailure(result *harness_protocol.TurnResult) {
	ctx := r.reliabilityCtxSnap()
	if ctx == nil || ctx.HasFinalized() {
		return
	}
	var err *harness_protocol.TurnError
	if result != nil {
		err = result.Error
	}
	category, reason := classifyFailure(err)
	detail := string(category)
	if reason != nil && reason.Message != "" {
		detail = reason.Message
	}
	summary := fmt.Sprintf("%s %s turn failed: %s", r.memberNameSnap(), r.agentKind, detail)
	_ = ctx.FinalizeFailure(category, reason, summary)
}

// finalizeStartupFailure 收口启动失败并 mark_member_error。
// Python: _finalize_startup_failure (member_runtime.py:872)
func (r *ExternalHarnessMemberRuntime) finalizeStartupFailure(exc error) {
	ctx := r.reliabilityCtxSnap()
	if ctx == nil || ctx.HasFinalized() {
		return
	}
	var err *harness_protocol.TurnError
	if pse, ok := exc.(*harnessproviders.ProviderStartupError); ok {
		e := pse.Err
		err = &e
	}
	category, reason := classifyFailure(err)
	if err == nil {
		// Python: reason = _failure_reason(message=str(exc) or type(exc).__name__,
		//                                  sdk_error_type=type(exc).__name__)
		name := fmt.Sprintf("%T", exc)
		reason = failureReason(firstNonEmpty(exc.Error(), name), nil, name, nil)
	}
	_ = ctx.FinalizeFailure(category, reason,
		fmt.Sprintf("%s %s startup failed: %T", r.memberNameSnap(), r.agentKind, exc))
	_ = ctx.MarkMemberError()
}

// firstNonEmpty 返回第一个非空串。
func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}

// recordInput 告诉记录器宿主向成员本轮投递了什么。
// Python: _record_input (member_runtime.py:887)
func (r *ExternalHarnessMemberRuntime) recordInput(receipt *harness_protocol.SendReceipt, text string) {
	r.mu.Lock()
	recorder := r.trajectory
	r.mu.Unlock()
	if recorder == nil || receipt == nil {
		return
	}
	func() {
		defer func() { _ = recover() }() // Python except: debug 吞
		recorder.RecordInput(receipt.TurnID, text)
	}()
}

// recordEvent 轨迹观察一个信封；仅 STARTED 开新轨迹 turn 并记录 identity。
// Python: _record_event (member_runtime.py:897)
func (r *ExternalHarnessMemberRuntime) recordEvent(envelope harness_protocol.HarnessEvent) {
	r.mu.Lock()
	recorder := r.trajectory
	r.mu.Unlock()
	if recorder == nil {
		return
	}
	func() {
		defer func() { _ = recover() }()
		if payload, ok := envelope.Event.(*harness_protocol.TurnLifecycleEvent); ok &&
			payload.Kind == harness_protocol.TurnEventKindStarted {
			// 成员的每个 provider turn 都是一条轨迹 turn；计数存成员会话，
			// 编号在重启后延续（member_runtime.py:906-908 注释）。
			turn := r.openMemberTurn()
			recorder.RecordTurnIdentity(envelope.TurnID, turn.TurnID, turn.TurnNumber)
		}
		recorder.Observe(envelope)
	}()
}

// openMemberTurn 打开下一条轨迹 turn 并提交推进后的计数。
// Python: _open_member_turn (member_runtime.py:917)
func (r *ExternalHarnessMemberRuntime) openMemberTurn() harness.MemberTurn {
	r.mu.Lock()
	memberSession := r.memberSession
	r.mu.Unlock()
	var store harness.TurnStateStore
	if memberSession != nil {
		// Python resolve_member_turn(member_session, ...)：会话本身即状态面
		// （native_harness.go:834-837 同款惯例）。
		store = memberSession
	}
	turn, _ := harness.ResolveMemberTurn(store, false)
	if memberSession == nil {
		return turn
	}
	// Python member_session.commit()；SessionFacade 未含 Commit，鸭子面同
	// memberSessionCommitter 惯例（:135）。
	if committer, ok := memberSession.(memberSessionCommitter); ok {
		if err := committer.Commit(context.Background()); err != nil {
			logger.Debug(logComponent).Err(err).Msg("[external-cli] turn state commit failed")
		}
	}
	return turn
}

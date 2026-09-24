package evolution

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/operator/skill_call"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/stream"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/sys_operation"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	"github.com/uapclaw/uapclaw-go/internal/evolving/checkpointing"
	"github.com/uapclaw/uapclaw-go/internal/evolving/experience"
	"github.com/uapclaw/uapclaw-go/internal/evolving/optimizer/llm_resilience"
	skillopt "github.com/uapclaw/uapclaw-go/internal/evolving/optimizer/skill_call"
	"github.com/uapclaw/uapclaw-go/internal/evolving/signal"
	"github.com/uapclaw/uapclaw-go/internal/evolving/trajectory"
	"github.com/uapclaw/uapclaw-go/internal/evolving/updater/single_dim"
)

// ──────────────────────────── 结构体 ────────────────────────────

// TeamSkillEvolutionRail 团队技能演进护栏，在团队任务完成后自动触发演进。
//
// 嵌入 EvolutionRail 基类获得自动轨迹收集和异步触发能力，
// 通过实现 EvolutionExtension 接口的 10 个方法完成子类多态分派。
//
// 与 SkillEvolutionRail 为兄弟关系（都嵌入 EvolutionRail），零共享。
// SkillEvolutionRail 处理单 Agent 常规技能（kind=skill），
// TeamSkillEvolutionRail 处理团队技能（kind=team-skill/swarm-skill）。
//
// 核心流程：团队任务完成 → 聚合团队轨迹 → TeamSignalDetector → 暂存审批 → 应用更新
// Slash 命令入口：/evolve /evolve_simplify /evolve_rebuild
//
// 对齐 Python: openjiuwen/harness/rails/evolution/team_skill_evolution_rail.py TeamSkillEvolutionRail
type TeamSkillEvolutionRail struct {
	*EvolutionRail

	// ─── 核心组件 ───

	// evolutionStore 技能演进数据存储
	evolutionStore *checkpointing.EvolutionStore
	// generator 团队技能经验优化器
	generator *skillopt.TeamSkillExperienceOptimizer
	// scorer 经验评分器
	scorer *experience.ExperienceScorer
	// manager 经验生命周期管理器
	manager *experience.ExperienceManager
	// approvalRuntime 审批运行时
	approvalRuntime *EvolutionApprovalRuntime
	// onlineUpdater 单维更新器
	onlineUpdater *single_dim.SingleDimUpdater
	// orchestrator 在线演进编排器
	orchestrator *experience.OnlineEvolutionOrchestrator
	// teamSignalDetector 团队信号检测器
	teamSignalDetector *signal.TeamSignalDetector
	// experienceTracker 经验展示追踪器
	experienceTracker *experience.ExperienceTracker

	// ─── 配置 ───

	// autoScan 是否自动检测团队完成信号
	autoScan bool
	// autoSave 是否自动保存（跳过审批）
	autoSave bool
	// language 语言（"cn" 或 "en"）
	language string
	// evalInterval 评估间隔
	evalInterval int
	// evolutionTotalTimeoutSec 后台演化超时秒数
	evolutionTotalTimeoutSec float64
	// teamID 团队标识
	teamID string
	// trajectorySource 团队轨迹聚合源
	trajectorySource trajectory.TrajectorySource
	// trajectoriesDir 轨迹调试目录
	trajectoriesDir string

	// ─── LLM 策略 ───

	// userRequestLLMPolicy 用户意图检测 LLM 调用策略
	userRequestLLMPolicy llm_resilience.LLMInvokePolicy
	// trajectoryIssueLLMPolicy 轨迹问题检测 LLM 调用策略
	trajectoryIssueLLMPolicy llm_resilience.LLMInvokePolicy
	// recordLLMPolicy 记录生成 LLM 调用策略
	recordLLMPolicy llm_resilience.LLMInvokePolicy
	// evaluateLLMPolicy 评估 LLM 调用策略
	evaluateLLMPolicy llm_resilience.LLMInvokePolicy
	// simplifyLLMPolicy 精简 LLM 调用策略
	simplifyLLMPolicy llm_resilience.LLMInvokePolicy

	// ─── 运行时状态 ───

	// passiveEvolutionPending 当前 invoke 是否观测到团队完成
	passiveEvolutionPending bool
	// hostCompletionPendingSessionID 外部通知的团队完成会话 ID
	hostCompletionPendingSessionID *string
	// processedSignalKeys 已处理的信号指纹集合
	processedSignalKeys map[[4]string]bool
	// pendingApprovalSnapshots 暂存审批快照映射
	pendingApprovalSnapshots map[string]*experience.PendingChange
	// pendingGovernance 暂存治理操作映射
	pendingGovernance map[string]*experience.PendingGovernance
	// experienceSkillOps 技能经验操作器映射
	experienceSkillOps map[string]*skill_call.SkillExperienceOperator
	// skillsDir 技能目录
	skillsDir []string
}

// TeamSkillEvolutionRailOption TeamSkillEvolutionRail 构造选项函数。
type TeamSkillEvolutionRailOption func(*TeamSkillEvolutionRail)

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

const (
	// teamSkillDefaultEvolutionTimeoutSecs 默认团队后台演化超时秒数
	// Python: _DEFAULT_TEAM_EVOLUTION_TOTAL_TIMEOUT_SECS = 720.0
	teamSkillDefaultEvolutionTimeoutSecs = 720.0

	// teamSkillDefaultEvalInterval 默认评估间隔
	teamSkillDefaultEvalInterval = 5
)

// ──────────────────────────── 全局变量 ────────────────────────────

var (
	// teamSkillKinds 团队技能类型集合
	// Python: _TEAM_SKILL_KINDS = {"team-skill", "swarm-skill"}
	teamSkillKinds = map[string]bool{"team-skill": true, "swarm-skill": true}

	// teamTaskNonTerminalStates 团队任务非终态集合
	// Python: _TEAM_TASK_NON_TERMINAL_STATES = ("pending", "claimed", "in_progress", "blocked")
	teamTaskNonTerminalStates = []string{"pending", "claimed", "in_progress", "blocked"}

	// teamUserRequestLLMPolicy 用户意图检测 LLM 策略
	// Python: _TEAM_USER_REQUEST_LLM_POLICY
	teamUserRequestLLMPolicy = llm_resilience.LLMInvokePolicy{
		AttemptTimeoutSecs: 60,
		TotalBudgetSecs:    120,
		MaxAttempts:        2,
	}

	// teamTrajectoryIssueLLMPolicy 轨迹问题检测 LLM 策略
	// Python: _TEAM_TRAJECTORY_ISSUE_LLM_POLICY
	teamTrajectoryIssueLLMPolicy = llm_resilience.LLMInvokePolicy{
		AttemptTimeoutSecs: 150,
		TotalBudgetSecs:    300,
		MaxAttempts:        2,
	}

	// teamRecordLLMPolicy 记录生成 LLM 策略
	// Python: _TEAM_RECORD_LLM_POLICY
	teamRecordLLMPolicy = llm_resilience.LLMInvokePolicy{
		AttemptTimeoutSecs: 150,
		TotalBudgetSecs:    300,
		MaxAttempts:        2,
	}

	// teamSkillMDRE 匹配文件路径中的技能名/SKILL.md
	// Python: TeamSkillEvolutionRail._SKILL_MD_RE
	teamSkillMDRE = regexp.MustCompile(`[/\\]([^/\\]+)[/\\]SKILL\.md`)

	// teamExperienceRecordHeadingRE 匹配经验记录标题行中的 record ID
	// Python: TeamSkillEvolutionRail._EXPERIENCE_RECORD_HEADING_RE
	teamExperienceRecordHeadingRE = regexp.MustCompile(`#+\s*\[([A-Za-z0-9_-]+)\]`)
)

// ──────────────────────────── 导出函数 ────────────────────────────

// NewTeamSkillEvolutionRail 创建 TeamSkillEvolutionRail 实例。
//
// 必选参数：skillsDir（技能目录）、llmModel（LLM 客户端）、model（模型名称）、language（语言）。
// 可选参数通过 Functional Options 覆盖默认配置。
//
// 对齐 Python: TeamSkillEvolutionRail.__init__(
//
//	skills_dir, llm, model, language, trajectory_store, team_trajectory_store,
//	trajectory_source, trajectory_sink, member_role, auto_scan, auto_save,
//	async_evolution, max_concurrent_evolution, team_id, trajectories_dir,
//	user_request_llm_policy, trajectory_issue_llm_policy, record_llm_policy,
//	evaluate_llm_policy, simplify_llm_policy, eval_interval,
//	evolution_total_timeout_secs, disabled_skills
//
// )
func NewTeamSkillEvolutionRail(
	skillsDir []string,
	llmModel *llm.Model,
	model string,
	language string,
	opts ...TeamSkillEvolutionRailOption,
) *TeamSkillEvolutionRail {
	r := &TeamSkillEvolutionRail{
		skillsDir:                skillsDir,
		language:                 language,
		autoScan:                 true,
		autoSave:                 false, // Python 默认 auto_save=False
		evalInterval:             teamSkillDefaultEvalInterval,
		evolutionTotalTimeoutSec: teamSkillDefaultEvolutionTimeoutSecs,
		processedSignalKeys:      make(map[[4]string]bool),
		pendingApprovalSnapshots: make(map[string]*experience.PendingChange),
		pendingGovernance:        make(map[string]*experience.PendingGovernance),
		experienceSkillOps:       make(map[string]*skill_call.SkillExperienceOperator),
		userRequestLLMPolicy:     teamUserRequestLLMPolicy,
		trajectoryIssueLLMPolicy: teamTrajectoryIssueLLMPolicy,
		recordLLMPolicy:          teamRecordLLMPolicy,
		evaluateLLMPolicy:        experience.EvaluateLLMPolicy,
		simplifyLLMPolicy:        experience.SimplifyLLMPolicy,
	}

	// 初始化 EvolutionRail 基类（先于 Options，确保 Option 可安全访问基类字段）
	// Python: super().__init__(evolution_trigger=AFTER_INVOKE, async_evolution=True, ...)
	r.EvolutionRail = NewEvolutionRail(r,
		WithDefaultMemberRole("leader"),
		WithEvolutionTrigger(TriggerAfterInvoke),
	)

	// 应用选项（此时基类已初始化，Option 可安全设置基类字段）
	for _, opt := range opts {
		opt(r)
	}

	if r.evalInterval < 1 {
		r.evalInterval = 1
	}

	// 初始化 EvolutionStore
	// Python: self._store = EvolutionStore(skills_dir)
	r.evolutionStore = checkpointing.NewEvolutionStore(r.skillsDir)

	// 初始化 TeamSkillExperienceOptimizer
	// Python: self._generator = TeamSkillExperienceOptimizer(llm, model, language, ...)
	debugDir := ""
	if len(r.evolutionStore.BaseDirs()) > 0 {
		debugDir = filepath.Join(filepath.Dir(r.evolutionStore.BaseDirs()[0]), "_debug")
	}
	r.generator = skillopt.NewTeamSkillExperienceOptimizer(
		llmModel, model, language,
		debugDir,
		r.recordLLMPolicy,
		r.evolutionStore,
	)

	// 初始化 ExperienceScorer
	// Python: self._scorer = ExperienceScorer(llm, model, language, ...)
	r.scorer, _ = experience.NewExperienceScorer(
		llmModel, model, language,
		&r.evaluateLLMPolicy, &r.simplifyLLMPolicy,
	)

	// 初始化 ExperienceManager
	// Python: self._manager = ExperienceManager(store=self._store, scorer=self._scorer, kind="team-skill", ...)
	r.manager, _ = experience.NewExperienceManager(
		r.evolutionStore, r.scorer,
		"team-skill", r.language,
		r.experienceSkillOps,
		r.pendingApprovalSnapshots,
		r.pendingGovernance,
	)

	// 初始化 EvolutionApprovalRuntime
	// Python: self._approval_runtime = EvolutionApprovalRuntime(manager=self._manager, ...)
	r.approvalRuntime = NewEvolutionApprovalRuntime(
		r.manager,
		r.pendingApprovalSnapshots,
	)

	// 初始化 SingleDimUpdater
	// Python: self._online_updater = SingleDimUpdater(self._generator)
	r.onlineUpdater = single_dim.NewSingleDimUpdater(r.generator)

	// 初始化 OnlineEvolutionOrchestrator
	// Python: self._online_orchestrator = OnlineEvolutionOrchestrator(
	//   store=self._store, updater=self._online_updater, manager=self._manager,
	//   skill_ops=self._experience_skill_ops, request_id_prefix="team_skill_evolve",
	//   stage_source="team_skill_experience_updater",
	// )
	r.orchestrator = experience.NewOnlineEvolutionOrchestrator(
		r.evolutionStore, r.onlineUpdater, r.manager,
		r.experienceSkillOps,
		"team_skill_evolve",
		"team_skill_experience_updater",
	)

	// 初始化 TeamSignalDetector
	// Python: self._team_signal_detector = TeamSignalDetector(
	//   llm=llm, model=model, language=language,
	//   trajectory_issue_llm_policy=..., user_intent_llm_policy=...,
	// )
	r.teamSignalDetector = signal.NewTeamSignalDetector(
		llmModel, model, language,
		&r.trajectoryIssueLLMPolicy,
		&r.userRequestLLMPolicy,
	)

	// 初始化 ExperienceTracker
	// Python: self._experience_tracker = ExperienceTracker(
	//   store=self._store, scorer=self._scorer, eval_interval=self._eval_interval,
	// )
	r.experienceTracker = experience.NewExperienceTracker(
		r.evolutionStore, r.scorer, r.evalInterval,
	)

	logger.Info(logComponent).
		Str("skills_dir", fmt.Sprintf("%v", r.skillsDir)).
		Str("model", model).
		Bool("auto_save", r.autoSave).
		Str("team_id", r.teamID).
		Msg("[TeamSkillEvolutionRail] 初始化完成")

	return r
}

// WithTeamSkillAutoScan 设置是否自动检测团队完成信号。
func WithTeamSkillAutoScan(v bool) TeamSkillEvolutionRailOption {
	return func(r *TeamSkillEvolutionRail) { r.autoScan = v }
}

// WithTeamSkillAutoSave 设置是否自动保存。
func WithTeamSkillAutoSave(v bool) TeamSkillEvolutionRailOption {
	return func(r *TeamSkillEvolutionRail) { r.autoSave = v }
}

// WithTeamSkillEvalInterval 设置评估间隔。
func WithTeamSkillEvalInterval(v int) TeamSkillEvolutionRailOption {
	return func(r *TeamSkillEvolutionRail) { r.evalInterval = v }
}

// WithTeamSkillEvolutionTimeout 设置后台演化超时秒数。
func WithTeamSkillEvolutionTimeout(v float64) TeamSkillEvolutionRailOption {
	return func(r *TeamSkillEvolutionRail) { r.evolutionTotalTimeoutSec = v }
}

// WithTeamSkillTeamID 设置团队标识。
func WithTeamSkillTeamID(v string) TeamSkillEvolutionRailOption {
	return func(r *TeamSkillEvolutionRail) { r.teamID = v }
}

// WithTeamSkillTrajectorySource 设置团队轨迹聚合源。
func WithTeamSkillTrajectorySource(src trajectory.TrajectorySource) TeamSkillEvolutionRailOption {
	return func(r *TeamSkillEvolutionRail) { r.trajectorySource = src }
}

// WithTeamSkillTrajectorySink 设置轨迹写入端点。
func WithTeamSkillTrajectorySink(sink trajectory.TrajectorySink, teamID string, memberRole ...string) TeamSkillEvolutionRailOption {
	return func(r *TeamSkillEvolutionRail) {
		r.teamID = teamID
		_ = r.SetTrajectorySink(sink, teamID, memberRole...)
	}
}

// WithTeamSkillMemberRole 设置成员角色。
// 基类已初始化后，直接修改基类 defaultMemberRole 字段。
func WithTeamSkillMemberRole(role string) TeamSkillEvolutionRailOption {
	return func(r *TeamSkillEvolutionRail) {
		if r.EvolutionRail != nil {
			r.EvolutionRail.defaultMemberRole = role
		}
	}
}

// WithTeamSkillUserRequestLLMPolicy 设置用户意图检测 LLM 策略。
func WithTeamSkillUserRequestLLMPolicy(p llm_resilience.LLMInvokePolicy) TeamSkillEvolutionRailOption {
	return func(r *TeamSkillEvolutionRail) { r.userRequestLLMPolicy = p }
}

// WithTeamSkillTrajectoryIssueLLMPolicy 设置轨迹问题检测 LLM 策略。
func WithTeamSkillTrajectoryIssueLLMPolicy(p llm_resilience.LLMInvokePolicy) TeamSkillEvolutionRailOption {
	return func(r *TeamSkillEvolutionRail) { r.trajectoryIssueLLMPolicy = p }
}

// WithTeamSkillRecordLLMPolicy 设置记录生成 LLM 策略。
func WithTeamSkillRecordLLMPolicy(p llm_resilience.LLMInvokePolicy) TeamSkillEvolutionRailOption {
	return func(r *TeamSkillEvolutionRail) { r.recordLLMPolicy = p }
}

// WithTeamSkillEvaluateLLMPolicy 设置评估 LLM 策略。
func WithTeamSkillEvaluateLLMPolicy(p llm_resilience.LLMInvokePolicy) TeamSkillEvolutionRailOption {
	return func(r *TeamSkillEvolutionRail) { r.evaluateLLMPolicy = p }
}

// WithTeamSkillSimplifyLLMPolicy 设置精简 LLM 策略。
func WithTeamSkillSimplifyLLMPolicy(p llm_resilience.LLMInvokePolicy) TeamSkillEvolutionRailOption {
	return func(r *TeamSkillEvolutionRail) { r.simplifyLLMPolicy = p }
}

// WithTeamSkillAsyncEvolution 设置是否异步执行演化。
// 基类已初始化后，直接修改基类 asyncEvolution 字段。
func WithTeamSkillAsyncEvolution(v bool) TeamSkillEvolutionRailOption {
	return func(r *TeamSkillEvolutionRail) {
		if r.EvolutionRail != nil {
			r.EvolutionRail.asyncEvolution = v
		}
	}
}

// WithTeamSkillMaxConcurrentEvolution 设置最大并发演化数。
// 基类已初始化后，重建 evolutionSem 通道容量。
func WithTeamSkillMaxConcurrentEvolution(v int) TeamSkillEvolutionRailOption {
	return func(r *TeamSkillEvolutionRail) {
		if r.EvolutionRail != nil {
			r.EvolutionRail.evolutionSem = make(chan struct{}, v)
		}
	}
}

// WithTeamSkillDisabledSkills 设置禁用的技能名称列表。
// 基类已初始化后，直接修改基类 disabledSkills 字段。
func WithTeamSkillDisabledSkills(names []string) TeamSkillEvolutionRailOption {
	return func(r *TeamSkillEvolutionRail) {
		if r.EvolutionRail != nil {
			r.EvolutionRail.disabledSkills = normalizeSkillNames(names)
		}
	}
}

// WithTeamSkillTrajectoriesDir 设置轨迹调试目录。
func WithTeamSkillTrajectoriesDir(dir string) TeamSkillEvolutionRailOption {
	return func(r *TeamSkillEvolutionRail) { r.trajectoriesDir = dir }
}

// Priority 返回优先级 80。
// Python: TeamSkillEvolutionRail.priority = 80
func (r *TeamSkillEvolutionRail) Priority() int { return 80 }

// ─── EvolutionExtension 10 个方法 ───

// OnBeforeInvoke 在每次 invoke 开始时重置团队完成标记。
// Python: TeamSkillEvolutionRail._on_before_invoke(ctx)
func (r *TeamSkillEvolutionRail) OnBeforeInvoke(_ context.Context, _ *agentinterfaces.AgentCallbackContext) error {
	r.passiveEvolutionPending = false
	return nil
}

// OnAfterModelCall 空实现——团队演化不在 model_call 后触发。
// Python: TeamSkillEvolutionRail 无此覆写
func (r *TeamSkillEvolutionRail) OnAfterModelCall(_ context.Context, _ *agentinterfaces.AgentCallbackContext) error {
	return nil
}

// OnAfterToolCall 在每次工具调用后检测经验详情读取和团队任务完成。
//
// 对齐 Python: TeamSkillEvolutionRail._on_after_tool_call(ctx)
//
//	(1) record_presented_experience_detail: 检测经验详情读取
//	(2) 拦截 view_task → 检测所有任务完成 → 标记 passiveEvolutionPending
func (r *TeamSkillEvolutionRail) OnAfterToolCall(ctx context.Context, cbc *agentinterfaces.AgentCallbackContext) error {
	inputs, ok := cbc.Inputs().(*agentinterfaces.ToolCallInputs)
	if !ok {
		return nil
	}

	// Python: await self._record_presented_experience_detail(ctx, inputs)
	r.recordPresentedExperienceDetail(ctx, inputs)

	if !r.autoScan {
		return nil
	}

	// Python: if inputs.tool_name != "view_task": return
	if inputs.ToolName != "view_task" {
		return nil
	}

	// Python: result_preview = str(inputs.tool_result)[:300]
	resultPreview := ""
	if inputs.ToolResult != nil {
		resultPreview = fmt.Sprintf("%v", inputs.ToolResult)
		if len(resultPreview) > 300 {
			resultPreview = resultPreview[:300]
		}
	}
	logger.Info(logComponent).
		Str("result_preview", resultPreview).
		Msg("[TeamSkillEvolutionRail] view_task 拦截")

	// Python: completed = self._all_tasks_completed(inputs.tool_result)
	completed := isCompletedTeamTaskView(inputs.ToolResult)
	logger.Debug(logComponent).
		Bool("completed", completed).
		Str("session_id", r.currentBuilderSessionID()).
		Msg("[TeamSkillEvolutionRail] view_task 完成检查结果")

	if !completed {
		logger.Info(logComponent).Msg("[TeamSkillEvolutionRail] view_task: 任务仍在进行中，跳过")
		return nil
	}

	// Python: self._mark_passive_evolution_pending()
	r.markPassiveEvolutionPending()
	return nil
}

// OnAfterInvoke 空实现——触发由基类 AllowEvolutionTrigger 控制。
// Python: TeamSkillEvolutionRail 无此覆写
func (r *TeamSkillEvolutionRail) OnAfterInvoke(_ context.Context, _ *agentinterfaces.AgentCallbackContext) error {
	return nil
}

// OnAfterTaskIteration 空实现。
// Python: TeamSkillEvolutionRail 无此覆写
func (r *TeamSkillEvolutionRail) OnAfterTaskIteration(_ context.Context, _ *agentinterfaces.AgentCallbackContext) error {
	return nil
}

// OnAfterEvolutionTriggered 在 after_invoke 演化触发完成后消费外部完成标记。
// Python: TeamSkillEvolutionRail._on_after_evolution_triggered(trajectory, ctx)
func (r *TeamSkillEvolutionRail) OnAfterEvolutionTriggered(_ context.Context, traj *trajectory.Trajectory, _ *agentinterfaces.AgentCallbackContext) error {
	if r.hostCompletionPendingSessionID != nil && traj != nil && *r.hostCompletionPendingSessionID == traj.SessionID {
		r.hostCompletionPendingSessionID = nil
	}
	return nil
}

// AllowEvolutionTrigger 返回当前触发点是否允许启动演化。
// 仅在团队任务完成时（passiveEvolutionPending 或 hostCompletionPending）允许触发。
// Python: TeamSkillEvolutionRail._allow_evolution_trigger(trigger_point, ctx)
func (r *TeamSkillEvolutionRail) AllowEvolutionTrigger(_ EvolutionTriggerPoint, _ *agentinterfaces.AgentCallbackContext) bool {
	if !r.autoScan {
		return false
	}
	if r.passiveEvolutionPending {
		return true
	}
	sessionID := r.currentBuilderSessionID()
	if r.hostCompletionPendingSessionID != nil && sessionID != "" && *r.hostCompletionPendingSessionID == sessionID {
		return true
	}
	return false
}

// SnapshotForEvolution 同步捕获快照，供后台演化任务使用。
//
// Python: TeamSkillEvolutionRail._snapshot_for_evolution(trajectory, ctx)
//
//	捕获轨迹 + 消息 + skill_name="team-skill" + presentedEntries
func (r *TeamSkillEvolutionRail) SnapshotForEvolution(ctx context.Context, traj *trajectory.Trajectory, cbc *agentinterfaces.AgentCallbackContext) *EvolutionSnapshot {
	if !r.autoScan {
		return nil
	}

	// 收集基础快照
	messages := collectMessagesFromTrajectory(traj)
	skillName := "team-skill"

	// Python: presented_entries = self._consume_presented_entries(session)
	var presentedEntries []experience.PresentedRecordEntry
	if cbc != nil {
		session := cbc.Session()
		if session != nil {
			presentedEntries = r.consumePresentedEntries(session.GetSessionID())
		}
	}

	return &EvolutionSnapshot{
		Trajectory:       traj,
		Messages:         messages,
		SkillName:        &skillName,
		PresentedEntries: presentedEntries,
		SessionID:        traj.SessionID,
	}
}

// RunEvolution 执行团队技能演化。
//
// 对齐 Python: TeamSkillEvolutionRail.run_evolution(trajectory, ctx=None, *, snapshot=None)
//
//	核心流程：
//	1. 从 snapshot 或 ctx 获取 messages 和 presentedEntries
//	2. 聚合团队轨迹（aggregateTeamTrajectory）
//	3. 检测使用的团队技能（detectUsedTeamSkill）
//	4. 运行 TeamSignalDetector（trajectory signals + user intent）
//	5. handleEvolutionFromSignals（从信号触发演化）
//	6. evaluatePresentedEntries（评估已呈现条目）
func (r *TeamSkillEvolutionRail) RunEvolution(ctx context.Context, traj *trajectory.Trajectory, snapshot *EvolutionSnapshot) error {
	if !r.autoScan {
		logger.Info(logComponent).Msg("[TeamSkillEvolutionRail] auto_scan 已禁用，跳过")
		return nil
	}

	// 对齐 Python: try: ... except Exception as exc: logger.error(...); _emit_progress("failed", ...)
	defer func() {
		if rec := recover(); rec != nil {
			logger.Error(logComponent).
				Any("panic", rec).
				Msg("[TeamSkillEvolutionRail] run_evolution 全局异常捕获")
			r.emitProgress("failed", fmt.Sprintf("团队技能演进因意外错误失败: %v", rec))
		}
	}()

	t0 := time.Now()
	defer func() {
		elapsed := time.Since(t0).Seconds()
		logger.Info(logComponent).Float64("elapsed_secs", elapsed).Msg("[TeamSkillEvolutionRail] run_evolution 完成")
	}()

	// Python: emit_progress("started", "team tasks completed; starting team skill evolution analysis")
	r.emitProgress("started", "团队任务已完成，开始团队技能演进分析")

	// Python: 从 snapshot 或 ctx 获取数据
	var messages []map[string]any
	var presentedEntries []experience.PresentedRecordEntry

	if snapshot != nil {
		messages = snapshot.Messages
		presentedEntries = snapshot.PresentedEntries
	} else {
		messages = collectMessagesFromTrajectory(traj)
	}

	// Python: team_trajectory = self._aggregate_team_trajectory(trajectory)
	teamTrajectory := r.aggregateTeamTrajectory(traj)
	if teamTrajectory != traj {
		traj = teamTrajectory
		if snapshot == nil {
			messages = collectMessagesFromTrajectory(traj)
		}
	}

	// Python: used_skill = self._detect_used_team_skill(trajectory)
	usedSkill := r.detectUsedTeamSkill(ctx, traj)
	if usedSkill == "" {
		logger.Info(logComponent).Msg("[TeamSkillEvolutionRail] 未检测到现有技能，跳过")
		r.emitProgress("cancelled", "轨迹中未检测到团队/集群技能使用，取消团队技能演进分析")
		r.evaluatePresentedEntries(ctx, presentedEntries)
		return nil
	}

	logger.Info(logComponent).Str("skill_name", usedSkill).Msg("[TeamSkillEvolutionRail] 检测到现有技能")

	// Python: current_content = await self._store.read_skill_content(used_skill)
	currentContent, err := r.evolutionStore.ReadSkillContent(ctx, usedSkill)
	if err != nil {
		logger.Warn(logComponent).Err(err).Str("skill_name", usedSkill).Msg("[TeamSkillEvolutionRail] 读取技能内容失败")
		currentContent = ""
	}

	// Python: signals = await self.team_signal_detector.detect_trajectory_signals(...)
	signals, err := r.teamSignalDetector.DetectTrajectorySignals(ctx, traj, usedSkill, currentContent)
	if err != nil {
		logger.Warn(logComponent).Err(err).Str("skill_name", usedSkill).Msg("[TeamSkillEvolutionRail] 轨迹信号检测失败")
		signals = nil
	}

	// Python: user_intent = await self._detect_user_request(messages, current_content)
	userIntent, err := r.detectUserRequest(ctx, messages, currentContent)
	if err != nil {
		logger.Warn(logComponent).Err(err).Msg("[TeamSkillEvolutionRail] 用户意图检测失败")
	}
	if userIntent != nil && userIntent.Intent != "" {
		r.teamAppendUniqueSignal(&signals, signal.MakeTeamUserIntentSignal(usedSkill, userIntent.Intent))
	}

	if len(signals) == 0 {
		logger.Info(logComponent).Str("skill_name", usedSkill).Msg("[TeamSkillEvolutionRail] 未检测到信号")
		r.emitProgress("cancelled", fmt.Sprintf("未检测到 '%s' 的可操作演进信号，取消团队技能演进分析", usedSkill), WithSkillName(usedSkill))
		r.evaluatePresentedEntries(ctx, presentedEntries)
		return nil
	}

	// Python: 统计信号类型
	trajectoryIssueSignals := 0
	for _, sig := range signals {
		if sig.SignalType == string(signal.TeamSignalTypeTrajectoryIssue) {
			trajectoryIssueSignals++
		}
	}
	userIntentSignals := len(signals) - trajectoryIssueSignals
	r.emitProgress("detecting_signals", fmt.Sprintf("检测到演进信号: %d 个轨迹问题, %d 个用户意图", trajectoryIssueSignals, userIntentSignals))

	// Python: request = await self._handle_evolution_from_signals(...)
	userQuery := ""
	if userIntent != nil {
		userQuery = userIntent.Intent
	}
	request, err := r.handleEvolutionFromSignals(
		ctx, usedSkill, traj, signals,
		r.autoSave, userQuery, messages, true,
	)
	if err != nil {
		logger.Warn(logComponent).Err(err).Str("skill_name", usedSkill).Msg("[TeamSkillEvolutionRail] handleEvolutionFromSignals 失败")
	}

	if request == nil {
		r.emitProgress("completed", "未生成演进记录")
	} else {
		requestID := ""
		if request.RequestID != "" {
			requestID = request.RequestID
		}
		r.emitProgress("completed", fmt.Sprintf("'%s' 的演进请求已就绪", usedSkill), WithSkillName(usedSkill), WithRequestID(requestID))
	}

	// Python: await self._evaluate_presented_entries(presented_entries)
	r.evaluatePresentedEntries(ctx, presentedEntries)

	return nil
}

// GetEvolutionTotalTimeoutSecs 返回团队演化总超时秒数。
// Python: TeamSkillEvolutionRail._get_evolution_total_timeout_secs() → 720.0
func (r *TeamSkillEvolutionRail) GetEvolutionTotalTimeoutSecs() float64 {
	return r.evolutionTotalTimeoutSec
}

// ─── 公开 API ───

// NotifyTeamCompleted 标记当前 invoke 在下一个 after_invoke 边界触发被动演化。
//
// 对齐 Python: TeamSkillEvolutionRail.notify_team_completed(ctx)
//
//	检查 autoScan、builder 可用性，设置 hostCompletionPendingSessionID
func (r *TeamSkillEvolutionRail) NotifyTeamCompleted(_ context.Context) (bool, error) {
	if !r.autoScan {
		logger.Info(logComponent).Msg("[TeamSkillEvolutionRail] notify_team_completed 因 auto_scan 禁用被忽略")
		return false, nil
	}

	sessionID := r.currentBuilderSessionID()
	if sessionID == "" {
		logger.Warn(logComponent).Msg("[TeamSkillEvolutionRail] notify_team_completed: 无可用轨迹（before_invoke 可能未触发）")
		return false, nil
	}

	r.hostCompletionPendingSessionID = &sessionID
	logger.Debug(logComponent).
		Str("session_id", sessionID).
		Msg("[TeamSkillEvolutionRail] notify_team_completed 已标记")
	return true, nil
}

// RequestUserEvolution 用户主动触发演化入口。
//
// 对齐 Python: TeamSkillEvolutionRail.request_user_evolution(skill_name, user_intent, *, auto_approve=False)
//
//	允许用户显式提供团队技能改进建议，是唯一的用户驱动团队技能演化路径
func (r *TeamSkillEvolutionRail) RequestUserEvolution(
	ctx context.Context,
	skillName string,
	userIntent string,
	autoApprove bool,
) (*EvolutionRequestResult, error) {
	// Python: if not self._is_active_request_subject(skill_name): return ...
	if !r.isActiveRequestSubject(ctx, skillName) {
		return &EvolutionRequestResult{SkillName: skillName}, nil
	}

	// Python: trajectory = self._build_trajectory()
	traj := r.buildTrajectory()
	if traj == nil {
		traj = &trajectory.Trajectory{
			ExecutionID: "user_triggered",
			SessionID:   "user_triggered",
			Source:      "user_triggered",
			Steps:       []*trajectory.TrajectoryStep{},
		}
	}

	// Python: trajectory = self._aggregate_team_trajectory(trajectory)
	traj = r.aggregateTeamTrajectory(traj)

	// Python: messages = self._collect_messages_from_trajectory(trajectory) if trajectory.steps else []
	messages := []map[string]any{}
	if len(traj.Steps) > 0 {
		messages = collectMessagesFromTrajectory(traj)
	}

	// Python: signals = await self._detect_active_request_signals(...) if trajectory.steps else []
	signals := []*signal.EvolutionSignal{}
	if len(traj.Steps) > 0 {
		detected, err := r.detectActiveRequestSignals(ctx, skillName, traj)
		if err != nil {
			logger.Warn(logComponent).Err(err).Str("skill_name", skillName).Msg("[TeamSkillEvolutionRail] active request 信号检测失败")
		} else {
			signals = detected
		}
	}

	// Python: if user_intent: self._append_unique_signal(signals, make_team_user_intent_signal(...))
	if userIntent != "" {
		r.teamAppendUniqueSignal(&signals, signal.MakeTeamUserIntentSignal(skillName, userIntent))
	}

	if len(signals) == 0 {
		logger.Info(logComponent).Str("skill_name", skillName).Msg("[TeamSkillEvolutionRail] request_user_evolution: 无证据或用户意图")
		return &EvolutionRequestResult{SkillName: skillName}, nil
	}

	if len(messages) == 0 && userIntent != "" {
		messages = []map[string]any{{"role": "user", "content": userIntent}}
	}

	// Python: request = await self._handle_evolution_from_signals(..., emit_host_events=False)
	request, err := r.handleEvolutionFromSignals(
		ctx, skillName, traj, signals,
		autoApprove, userIntent, messages, false,
	)
	if err != nil {
		logger.Warn(logComponent).Err(err).Str("skill_name", skillName).Msg("[TeamSkillEvolutionRail] request_user_evolution 演化处理失败")
		return &EvolutionRequestResult{SkillName: skillName}, nil
	}

	if request == nil {
		logger.Info(logComponent).Str("skill_name", skillName).Msg("[TeamSkillEvolutionRail] request_user_evolution: 未生成记录")
		return &EvolutionRequestResult{SkillName: skillName}, nil
	}

	// 构建返回结果
	records := []checkpointing.EvolutionRecord{}
	if request.PendingChange != nil {
		records = request.PendingChange.Payload
	}

	var approvalEvent *stream.OutputSchema
	if !autoApprove && request.PendingChange != nil {
		approvalEvent = r.buildRecordApprovalEvent(skillName, request)
	}

	requestID := request.RequestID
	return &EvolutionRequestResult{
		SkillName:     skillName,
		RequestID:     &requestID,
		ApprovalEvent: approvalEvent,
		Records:       records,
		AutoApproved:  autoApprove,
	}, nil
}

// ApproveRecord 处理暂存演进记录的审批通过。
// Python: TeamSkillEvolutionRail.approve_record(request_id)
func (r *TeamSkillEvolutionRail) ApproveRecord(ctx context.Context, requestID string) error {
	pending, result, err := r.approvalRuntime.ApprovePendingRequest(ctx, requestID, "TeamSkillEvolutionRail", "approve_record")
	if err != nil {
		return err
	}
	if pending == nil {
		return nil
	}
	if result.PendingCount > 0 {
		return nil
	}
	delete(r.pendingApprovalSnapshots, requestID)
	logger.Info(logComponent).
		Int("applied_count", result.AppliedCount).
		Str("skill_name", pending.SkillName).
		Msg("[TeamSkillEvolutionRail] 用户已批准记录")
	return nil
}

// RejectRecord 处理暂存演进记录的审批拒绝。
// Python: TeamSkillEvolutionRail.reject_record(request_id)
func (r *TeamSkillEvolutionRail) RejectRecord(ctx context.Context, requestID string) error {
	pending, result, err := r.approvalRuntime.RejectPendingRequest(ctx, requestID, "TeamSkillEvolutionRail", "reject_record")
	if err != nil {
		return err
	}
	if pending == nil {
		return nil
	}
	if result.RejectedCount > 0 {
		logger.Info(logComponent).
			Int("rejected_count", result.RejectedCount).
			Str("skill_name", pending.SkillName).
			Msg("[TeamSkillEvolutionRail] 用户已拒绝记录")
	}
	return nil
}

// RequestSimplify 发起精简请求。
// Python: TeamSkillEvolutionRail.request_simplify(skill_name, user_intent=None)
func (r *TeamSkillEvolutionRail) RequestSimplify(ctx context.Context, skillName string, userIntent *string) (*SimplifyRequestResult, error) {
	requestID, err := r.manager.RequestSimplify(ctx, skillName, userIntent)
	if err != nil {
		return &SimplifyRequestResult{SkillName: skillName}, err
	}
	if requestID == "" {
		return &SimplifyRequestResult{SkillName: skillName}, nil
	}

	governance, ok := r.pendingGovernance[requestID]
	if !ok {
		return &SimplifyRequestResult{SkillName: skillName, RequestID: &requestID}, nil
	}

	actions := governance.Actions
	event := BuildSimplifyApprovalEvent(skillName, requestID, actions, r.language)
	logger.Info(logComponent).
		Str("skill_name", skillName).
		Str("request_id", requestID).
		Msg("[TeamSkillEvolutionRail] simplify 已暂存")

	return &SimplifyRequestResult{
		SkillName:     skillName,
		RequestID:     &requestID,
		ApprovalEvent: event,
		Actions:       actions,
	}, nil
}

// OnApproveSimplify 执行精简审批通过。
// Python: TeamSkillEvolutionRail.on_approve_simplify(request_id)
func (r *TeamSkillEvolutionRail) OnApproveSimplify(ctx context.Context, requestID string) (map[string]int, error) {
	result, err := r.manager.ApproveSimplify(ctx, requestID)
	if err != nil {
		return nil, err
	}
	logger.Info(logComponent).Str("request_id", requestID).Msg("[TeamSkillEvolutionRail] simplify 已批准")
	return result, nil
}

// OnRejectSimplify 丢弃精简请求。
// Python: TeamSkillEvolutionRail.on_reject_simplify(request_id)
func (r *TeamSkillEvolutionRail) OnRejectSimplify(requestID string) {
	r.manager.RejectSimplify(requestID)
	logger.Info(logComponent).Str("request_id", requestID).Msg("[TeamSkillEvolutionRail] simplify 已拒绝")
}

// RequestRebuild 构建重建提示词。
// Python: TeamSkillEvolutionRail.request_rebuild(skill_name, user_intent=None, min_score=0.5)
func (r *TeamSkillEvolutionRail) RequestRebuild(ctx context.Context, skillName string, userIntent *string, minScore float64) (string, error) {
	followupText, err := r.manager.RequestRebuild(ctx, skillName, userIntent, minScore)
	if err != nil {
		return "", err
	}
	if followupText == "" {
		return "", nil
	}
	logger.Info(logComponent).Str("skill_name", skillName).Msg("[TeamSkillEvolutionRail] rebuild 提示词已生成")
	return followupText, nil
}

// RecordPresentedExperiences 记录团队技能经验展示。
// Python: TeamSkillEvolutionRail.record_presented_experiences(skill_name, presentation_snippet, *, session, record_ids)
func (r *TeamSkillEvolutionRail) RecordPresentedExperiences(ctx context.Context, skillName string, snippet string, sessionID string, recordIDs []string) {
	if r.experienceTracker == nil {
		return
	}
	if len(recordIDs) > 0 {
		r.experienceTracker.RecordPresentedRecords(ctx, sessionID, skillName, snippet, recordIDs)
		return
	}
	r.experienceTracker.RecordPresented(ctx, sessionID, skillName, snippet)
}

// ─── 属性访问器 ───

// EvolutionStore 返回技能演进数据存储。
func (r *TeamSkillEvolutionRail) EvolutionStore() *checkpointing.EvolutionStore {
	return r.evolutionStore
}

// Scorer 返回经验评分器。
func (r *TeamSkillEvolutionRail) Scorer() *experience.ExperienceScorer {
	return r.scorer
}

// Generator 返回团队技能经验优化器。
func (r *TeamSkillEvolutionRail) Generator() *skillopt.TeamSkillExperienceOptimizer {
	return r.generator
}

// TeamSignalDetector 返回团队信号检测器。
// Python: TeamSkillEvolutionRail.team_signal_detector (lazy property)
func (r *TeamSkillEvolutionRail) TeamSignalDetector() *signal.TeamSignalDetector {
	return r.teamSignalDetector
}

// ApprovalRuntime 返回审批运行时。
// Python: TeamSkillEvolutionRail.approval_runtime (lazy property)
func (r *TeamSkillEvolutionRail) ApprovalRuntime() *EvolutionApprovalRuntime {
	return r.approvalRuntime
}

// AutoScan 返回是否自动检测团队完成信号。
func (r *TeamSkillEvolutionRail) AutoScan() bool { return r.autoScan }

// SetAutoScan 设置是否自动检测团队完成信号。
func (r *TeamSkillEvolutionRail) SetAutoScan(v bool) { r.autoScan = v }

// AutoSave 返回是否自动保存。
func (r *TeamSkillEvolutionRail) AutoSave() bool { return r.autoSave }

// SetAutoSave 设置是否自动保存。
func (r *TeamSkillEvolutionRail) SetAutoSave(v bool) { r.autoSave = v }

// ProcessedSignalKeys 返回已处理的信号指纹集合。
func (r *TeamSkillEvolutionRail) ProcessedSignalKeys() map[[4]string]bool {
	return r.processedSignalKeys
}

// ClearProcessedSignals 清空已处理的信号指纹集合。
func (r *TeamSkillEvolutionRail) ClearProcessedSignals() {
	r.processedSignalKeys = make(map[[4]string]bool)
}

// EvolutionConfig 返回有效的演化配置。
// Python: TeamSkillEvolutionRail.evolution_config (property)
func (r *TeamSkillEvolutionRail) EvolutionConfig() map[string]any {
	return map[string]any{
		"user_request_llm_policy":      r.userRequestLLMPolicy,
		"trajectory_issue_llm_policy":  r.trajectoryIssueLLMPolicy,
		"record_llm_policy":            r.recordLLMPolicy,
		"evaluate_llm_policy":          r.evaluateLLMPolicy,
		"simplify_llm_policy":          r.simplifyLLMPolicy,
		"eval_interval":                r.evalInterval,
		"evolution_total_timeout_secs": r.evolutionTotalTimeoutSec,
		"max_concurrent_evolution": func() int {
			if r.EvolutionRail != nil && r.evolutionSem != nil {
				return cap(r.evolutionSem)
			}
			return 0
		}(),
	}
}

// SetTrajectorySource 绑定团队轨迹聚合源。
// Python: TeamSkillEvolutionRail.set_trajectory_source(source)
func (r *TeamSkillEvolutionRail) SetTrajectorySource(src trajectory.TrajectorySource) {
	r.trajectorySource = src
}

// UpdateLLM 更新 LLM 客户端和模型名称引用。
// Python: SkillEvolutionRail.update_llm(llm, model) — Team 中也有类似逻辑
func (r *TeamSkillEvolutionRail) UpdateLLM(llmModel *llm.Model, model string) {
	if r.generator != nil {
		r.generator.UpdateLLM(llmModel, model)
	}
	if r.scorer != nil {
		r.scorer.UpdateLLM(llmModel, model)
	}
	if r.teamSignalDetector != nil {
		r.teamSignalDetector = signal.NewTeamSignalDetector(
			llmModel, model, r.language,
			&r.trajectoryIssueLLMPolicy,
			&r.userRequestLLMPolicy,
		)
	}
}

// SetSysOperation 设置系统操作，同时传递给 EvolutionRail 基类和 EvolutionStore。
//
// 对齐 Python: SkillEvolutionRail.set_sys_operation(sys_operation)
// TeamSkillEvolutionRail 同理
func (r *TeamSkillEvolutionRail) SetSysOperation(op sys_operation.SysOperation) {
	// Python: super().set_sys_operation(sys_operation)
	r.EvolutionRail.SetSysOperation(op)
	// Python: self._evolution_store.sys_operation = sys_operation
	r.evolutionStore.SetSysOperation(op)
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// isTeamSkill 判断技能是否为团队类型（kind=team-skill 或 swarm-skill）。
// 对齐 Python: TeamSkillEvolutionRail._is_team_skill(name)
func (r *TeamSkillEvolutionRail) isTeamSkill(ctx context.Context, name string) bool {
	skillDir := r.evolutionStore.ResolveSkillDir(ctx, name)
	if skillDir == "" {
		return false
	}
	skillMD := filepath.Join(skillDir, "SKILL.md")
	data, err := os.ReadFile(skillMD)
	if err != nil {
		return false
	}
	frontmatter := parseTopLevelFrontmatter(string(data))
	kind, ok := frontmatter["kind"]
	if !ok {
		return false
	}
	return teamSkillKinds[kind]
}

// detectUsedTeamSkill 从轨迹推断使用的团队技能。
// 对齐 Python: TeamSkillEvolutionRail._detect_used_team_skill(trajectory)
func (r *TeamSkillEvolutionRail) detectUsedTeamSkill(ctx context.Context, traj *trajectory.Trajectory) string {
	allSkillNames := r.evolutionStore.ListSkillNames(ctx)
	if len(allSkillNames) == 0 {
		logger.Info(logComponent).Msg("[TeamSkillEvolutionRail] 磁盘上无现有团队技能")
		return ""
	}

	// Python: known_skills = {name for name in all_skill_names if self._is_team_skill(name)}
	knownSkills := make([]string, 0)
	for _, name := range allSkillNames {
		if r.isTeamSkill(ctx, name) && !r.isSkillDisabled(name) {
			knownSkills = append(knownSkills, name)
		}
	}
	if len(knownSkills) == 0 {
		logger.Info(logComponent).
			Int("total", len(allSkillNames)).
			Msg("[TeamSkillEvolutionRail] 未找到 team-skill 类型技能")
		return ""
	}

	best := inferTeamSkillFromTrajectory(traj, knownSkills)
	if best != "" {
		logger.Info(logComponent).Str("skill_name", best).Msg("[TeamSkillEvolutionRail] 从轨迹检测到团队技能")
		return best
	}

	logger.Info(logComponent).Msg("[TeamSkillEvolutionRail] 轨迹中未找到 SKILL.md 读取痕迹")
	return ""
}

// teamSkillForExperienceDetailFile 根据文件路径反查团队技能名。
// 对齐 Python: TeamSkillEvolutionRail._team_skill_for_experience_detail_file(file_path)
func (r *TeamSkillEvolutionRail) teamSkillForExperienceDetailFile(ctx context.Context, filePath string) string {
	readPath := filepath.Clean(filePath)
	skillNames := r.evolutionStore.ListSkillNames(ctx)

	for _, skillName := range skillNames {
		if !r.isTeamSkill(ctx, skillName) {
			continue
		}
		skillDir := r.evolutionStore.ResolveSkillDir(ctx, skillName)
		if skillDir == "" {
			continue
		}
		rel, err := filepath.Rel(skillDir, readPath)
		if err != nil {
			continue
		}
		if isExperienceDetailRelativePath(rel) {
			return skillName
		}
	}
	return ""
}

// aggregateTeamTrajectory 聚合团队轨迹。
// 对齐 Python: TeamSkillEvolutionRail._aggregate_team_trajectory(trajectory)
func (r *TeamSkillEvolutionRail) aggregateTeamTrajectory(traj *trajectory.Trajectory) *trajectory.Trajectory {
	if r.trajectorySource == nil {
		return traj
	}

	teamID := r.teamID
	if teamID == "" {
		teamID = "unknown"
	}

	sessionID := ""
	if traj != nil {
		sessionID = traj.SessionID
	}

	// Python: team_traj = source.get_trajectory(team_id=team_id, session_id=..., filter_collaborative=True)
	teamTraj := r.trajectorySource.GetTrajectory(teamID, sessionID, true)
	if teamTraj == nil || len(teamTraj.Steps) == 0 {
		return traj
	}

	// Python: emit_progress("detecting_signals", "aggregated ...")
	memberCount := 0
	if teamTraj.Meta != nil {
		if mc, ok := teamTraj.Meta["member_count"].(int); ok {
			memberCount = mc
		}
	}
	r.emitProgress("detecting_signals", fmt.Sprintf("聚合 %d 个成员，%d 个协作步骤", memberCount, len(teamTraj.Steps)))
	return teamTraj
}

// detectUserRequest 桥接 TeamSignalDetector 检测用户意图。
// 对齐 Python: TeamSkillEvolutionRail._detect_user_request(messages, team_skill_content)
func (r *TeamSkillEvolutionRail) detectUserRequest(ctx context.Context, messages []map[string]any, skillContent string) (*signal.UserIntent, error) {
	if len(messages) == 0 {
		return nil, nil
	}
	return r.teamSignalDetector.DetectUserIntent(ctx, messages, skillContent)
}

// detectActiveRequestSignals 检测显式请求的活跃信号。
// 对齐 Python: TeamSkillEvolutionRail._detect_active_request_signals(skill_name, trajectory)
func (r *TeamSkillEvolutionRail) detectActiveRequestSignals(
	ctx context.Context,
	skillName string,
	traj *trajectory.Trajectory,
) ([]*signal.EvolutionSignal, error) {
	// Python: current_content = await self._store.read_skill_content(skill_name)
	currentContent, err := r.evolutionStore.ReadSkillContent(ctx, skillName)
	if err != nil {
		return nil, err
	}

	// Python: detected = await self.team_signal_detector.detect_trajectory_signals(...)
	detected, err := r.teamSignalDetector.DetectTrajectorySignals(ctx, traj, skillName, currentContent)
	if err != nil {
		logger.Warn(logComponent).Err(err).Str("skill_name", skillName).Msg("[TeamSkillEvolutionRail] active request 轨迹检测失败")
		return nil, nil
	}

	// Python: 过滤非目标技能的信号
	signals := make([]*signal.EvolutionSignal, 0, len(detected))
	for _, sig := range detected {
		if sig.SkillName != nil && *sig.SkillName != "" && *sig.SkillName != skillName {
			continue
		}
		r.teamAppendUniqueSignal(&signals, sig)
	}
	return signals, nil
}

// detectExperienceDetailRead 检测经验详情读取操作（团队技能变体）。
// 对齐 Python: TeamSkillEvolutionRail._detect_experience_detail_read(inputs)
func (r *TeamSkillEvolutionRail) detectExperienceDetailRead(ctx context.Context, inputs *agentinterfaces.ToolCallInputs) string {
	toolName := inputs.ToolName
	args := extractToolArgs(inputs.ToolArgs)

	// Python: if tool_name == "skill_tool"
	if toolName == "skill_tool" {
		skillName := strings.TrimSpace(str(args["skill_name"]))
		relativePath := strings.TrimSpace(str(args["relative_file_path"]))
		if relativePath == "" {
			relativePath = "SKILL.md"
		}
		// Python: 区别于 SkillEvolutionRail —— 这里检查 _is_team_skill
		if skillName != "" && isExperienceDetailRelativePath(relativePath) {
			if r.isTeamSkill(ctx, skillName) {
				return skillName
			}
		}
		return ""
	}

	// Python: if "read" not in tool_name.lower() or "file" not in tool_name.lower()
	if !strings.Contains(strings.ToLower(toolName), "read") || !strings.Contains(strings.ToLower(toolName), "file") {
		return ""
	}

	filePath := strings.TrimSpace(str(args["file_path"]))
	if filePath == "" {
		return ""
	}
	if !strings.Contains(strings.ReplaceAll(filePath, "\\", "/"), "/evolution/") {
		return ""
	}

	// Python: return self._team_skill_for_experience_detail_file(file_path)
	return r.teamSkillForExperienceDetailFile(context.Background(), filePath)
}

// teamExtractToolContent 提取工具调用返回内容（独立于 SkillEvolutionRail.extractToolContent）。
// 对齐 Python: TeamSkillEvolutionRail._extract_tool_content(inputs)
func teamExtractToolContent(inputs *agentinterfaces.ToolCallInputs) string {
	result := inputs.ToolResult
	if result != nil {
		if data, ok := result.(map[string]any); ok {
			if content, ok := data["skill_content"].(string); ok && content != "" {
				return content
			}
			if content, ok := data["content"].(string); ok && content != "" {
				return content
			}
		}
	}

	// Python: tool_msg = inputs.tool_msg
	if inputs.ToolMsg != nil {
		content := inputs.ToolMsg.GetContent().String()
		if content != "" {
			return content
		}
	}

	if result != nil {
		if content, ok := result.(string); ok && content != "" {
			return content
		}
	}
	return ""
}

// teamExtractPresentedRecordIDs 从内容中提取经验记录 ID（独立版本）。
// 对齐 Python: TeamSkillEvolutionRail._extract_presented_record_ids(content)
func teamExtractPresentedRecordIDs(content string) []string {
	seen := make(map[string]bool)
	var recordIDs []string
	for _, match := range teamExperienceRecordHeadingRE.FindAllStringSubmatch(content, -1) {
		recordID := match[1]
		if seen[recordID] {
			continue
		}
		seen[recordID] = true
		recordIDs = append(recordIDs, recordID)
	}
	return recordIDs
}

// recordPresentedExperienceDetail 在 after_tool_call 中记录经验详情读取。
// 对齐 Python: TeamSkillEvolutionRail._record_presented_experience_detail(ctx, inputs)
func (r *TeamSkillEvolutionRail) recordPresentedExperienceDetail(ctx context.Context, inputs *agentinterfaces.ToolCallInputs) {
	skillName := r.detectExperienceDetailRead(ctx, inputs)
	if skillName == "" {
		return
	}

	content := teamExtractToolContent(inputs)
	recordIDs := teamExtractPresentedRecordIDs(content)
	if len(recordIDs) == 0 {
		return
	}

	sessionID := r.currentBuilderSessionID()
	r.experienceTracker.RecordPresentedRecords(ctx, sessionID, skillName, content, recordIDs)
}

// consumePresentedEntries 消费评估状态条目。
// Python: TeamSkillEvolutionRail._consume_presented_entries(session)
func (r *TeamSkillEvolutionRail) consumePresentedEntries(sessionID string) []experience.PresentedRecordEntry {
	if r.experienceTracker == nil {
		return nil
	}
	return r.experienceTracker.ConsumeEvalState(sessionID)
}

// evaluatePresentedEntries 评估已展示的经验。
// Python: TeamSkillEvolutionRail._evaluate_presented_entries(presented_entries)
func (r *TeamSkillEvolutionRail) evaluatePresentedEntries(ctx context.Context, entries []experience.PresentedRecordEntry) {
	if r.experienceTracker == nil || len(entries) == 0 {
		return
	}
	r.experienceTracker.EvaluatePresented(ctx, entries)
}

// markPassiveEvolutionPending 标记当前 invoke 已观测到团队完成（幂等）。
// Python: TeamSkillEvolutionRail._mark_passive_evolution_pending()
func (r *TeamSkillEvolutionRail) markPassiveEvolutionPending() {
	if r.passiveEvolutionPending {
		return
	}
	r.passiveEvolutionPending = true
}

// isActiveRequestSubject 判断请求的技能是否为团队技能且存在。
// Python: TeamSkillEvolutionRail._is_active_request_subject(skill_name)
func (r *TeamSkillEvolutionRail) isActiveRequestSubject(ctx context.Context, skillName string) bool {
	if !r.evolutionStore.SkillExists(ctx, skillName) {
		return false
	}
	return r.isTeamSkill(ctx, skillName)
}

// currentBuilderSessionID 返回当前轨迹构造器的会话 ID。
// Python: TeamSkillEvolutionRail._current_builder_session_id()
func (r *TeamSkillEvolutionRail) currentBuilderSessionID() string {
	if r.Builder() == nil {
		return ""
	}
	return r.Builder().SessionID()
}

// buildTrajectory 从基类 builder 构建轨迹。
// Python: TeamSkillEvolutionRail._build_trajectory()
func (r *TeamSkillEvolutionRail) buildTrajectory() *trajectory.Trajectory {
	if r.Builder() == nil {
		return nil
	}
	return r.Builder().Build()
}

// isSkillDisabled 检查技能是否被禁用。
func (r *TeamSkillEvolutionRail) isSkillDisabled(name string) bool {
	return r.DisabledSkills()[name]
}

// emitProgress 发送进度事件。
// Python: TeamSkillEvolutionRail._emit_progress(stage, message, *, skill_name, request_id)
func (r *TeamSkillEvolutionRail) emitProgress(stage, message string, opts ...ProgressEventOption) {
	logger.Info(logComponent).Str("stage", stage).Msg(message)
	r.EmitHostEvent(BuildEvolutionProgressEvent("team", stage, message, append(opts, WithPrefix("[Team Skill Evolution]"))...))
}

// buildRecordApprovalEvent 构建团队技能审批事件。
// Python: TeamSkillEvolutionRail._build_record_approval_event(skill_name, pending, *, proposal)
func (r *TeamSkillEvolutionRail) buildRecordApprovalEvent(skillName string, request *experience.ExperienceApprovalRequest) *stream.OutputSchema {
	if request == nil {
		return nil
	}

	pending := request.PendingChange
	if pending == nil {
		return nil
	}

	records := pending.Payload
	event := BuildTeamSkillApprovalEventFromRecords(
		skillName,
		request.RequestID,
		r.language,
		records,
	)
	AttachEvolutionMeta(event, request.Proposal.SignalType, request.Proposal.SignalSource)
	return event
}

// emitRecordApprovalEvent 缓存团队技能审批事件。
// Python: TeamSkillEvolutionRail._emit_record_approval_event(skill_name, pending, proposal)
func (r *TeamSkillEvolutionRail) emitRecordApprovalEvent(skillName string, request *experience.ExperienceApprovalRequest) {
	event := r.buildRecordApprovalEvent(skillName, request)
	if event == nil {
		return
	}
	r.EmitHostEvent(event)

	pending := request.PendingChange
	if pending != nil {
		sections := make([]string, 0, len(pending.Payload))
		for _, rec := range pending.Payload {
			if rec.Change.Section != "" {
				sections = append(sections, rec.Change.Section)
			}
		}
		r.emitProgress("approval_required",
			fmt.Sprintf("团队技能演进提议: '%s'\n  章节: %s\n  记录数: %d\n  变更 ID: %s\n  操作: 审批对话框应已弹出；如不可见，请检查审批面板或重新运行任务",
				skillName,
				strings.Join(sections, ", "),
				len(pending.Payload),
				pending.ChangeID,
			),
			WithSkillName(skillName),
		)
	}
}

// handleEvolutionFromSignals 共享下游处理器——暂存 + 路由审批。
//
// 对齐 Python: TeamSkillEvolutionRail._handle_evolution_from_signals(
//
//	skill_name, trajectory, signals, auto_approve, user_query, messages, emit_host_events
//
// )
func (r *TeamSkillEvolutionRail) handleEvolutionFromSignals(
	ctx context.Context,
	skillName string,
	traj *trajectory.Trajectory,
	signals []*signal.EvolutionSignal,
	autoApprove bool,
	userQuery string,
	messages []map[string]any,
	emitHostEvents bool,
) (*experience.ExperienceApprovalRequest, error) {
	if emitHostEvents {
		r.emitProgress("generating_updates", fmt.Sprintf("正在为 '%s' 生成演化记录", skillName), WithSkillName(skillName))
	}

	// Python: result = await self._stage_evolution_from_signals(...)
	result, err := r.stageEvolutionFromSignals(ctx, skillName, traj, signals, autoApprove, userQuery, messages)
	if err != nil {
		return nil, err
	}

	request := result.Request
	if request == nil {
		if emitHostEvents && result.Status == "no_evolution_no_records" {
			msg := result.Message
			if msg == "" {
				msg = fmt.Sprintf("在线演化完成，状态=%s", result.Status)
			}
			r.emitBackgroundOutcomeEvent(map[string]string{
				"status":     result.Status,
				"message":    msg,
				"rail_kind":  "team",
				"skill_name": result.SkillName,
				"stage":      "completed",
				"source":     "team_skill_experience_updater",
			})
		}
		return nil, nil
	}

	// Python: def _emit_approval_request(staged_request)
	// Python: emit_host_events=False 时 _emit_approval_request = lambda: None（no-op）
	var emitApprovalRequest func(*experience.ExperienceApprovalRequest) error
	if emitHostEvents {
		emitApprovalRequest = func(stagedReq *experience.ExperienceApprovalRequest) error {
			if stagedReq == nil {
				return nil
			}
			r.emitRecordApprovalEvent(skillName, stagedReq)
			logger.Info(logComponent).
				Str("change_id", stagedReq.RequestID).
				Msg("[TeamSkillEvolutionRail] 信号已消费，记录已暂存待审批")
			r.emitProgress("approval_required",
				fmt.Sprintf("'%s' 的经验记录已就绪，等待审批", skillName),
				WithSkillName(skillName),
				WithRequestID(stagedReq.RequestID),
			)
			return nil
		}
	}

	// Python: def _on_auto_approved(staged_request)
	onAutoApproved := func(stagedReq *experience.ExperienceApprovalRequest) error {
		logger.Info(logComponent).Str("skill_name", skillName).Msg("[TeamSkillEvolutionRail] 信号已消费，记录已自动批准")
		if emitHostEvents {
			requestID := ""
			if stagedReq != nil {
				requestID = stagedReq.RequestID
			}
			r.emitProgress("auto_approved",
				fmt.Sprintf("'%s' 的经验记录已自动保存", skillName),
				WithSkillName(skillName),
				WithRequestID(requestID),
			)
		}
		return nil
	}

	// Python: return await self.approval_runtime.finalize_staged_evolution_request(...)
	err = r.approvalRuntime.FinalizeStagedEvolutionRequest(
		request,
		!autoApprove, // requires_approval 是否需要审批
		emitApprovalRequest,
		onAutoApproved,
	)
	if err != nil {
		return nil, err
	}
	return request, nil
}

// stageEvolutionFromSignals 通过 OnlineEvolutionOrchestrator 暂存演化记录。
// Python: TeamSkillEvolutionRail._stage_evolution_from_signals(...)
func (r *TeamSkillEvolutionRail) stageEvolutionFromSignals(
	ctx context.Context,
	skillName string,
	traj *trajectory.Trajectory,
	signals []*signal.EvolutionSignal,
	autoApprove bool,
	userQuery string,
	messages []map[string]any,
) (*experience.OnlineEvolutionResult, error) {
	r.emitProgress("staging", fmt.Sprintf("暂存 '%s' 的演进请求", skillName), WithSkillName(skillName))

	// 转换信号类型
	signalValues := make([]signal.EvolutionSignal, len(signals))
	for i, s := range signals {
		if s != nil {
			signalValues[i] = *s
		}
	}

	source := "team_skill_experience_updater"
	result, err := r.orchestrator.Evolve(
		ctx, skillName, signalValues, messages, userQuery, traj,
		!autoApprove, // requires_approval 是否需要审批
		map[string]any{},
		&source,
	)
	if err != nil {
		return nil, err
	}
	return result, nil
}

// teamAppendUniqueSignal 去重追加信号（独立于 appendUniqueSignal）。
// Python: TeamSkillEvolutionRail._append_unique_signal(signals, signal)
func (r *TeamSkillEvolutionRail) teamAppendUniqueSignal(signals *[]*signal.EvolutionSignal, sig *signal.EvolutionSignal) {
	if sig == nil || signals == nil {
		return
	}
	fp := signal.MakeSignalFingerprint(sig)
	for _, existing := range *signals {
		if signal.MakeSignalFingerprint(existing) == fp {
			return
		}
	}
	*signals = append(*signals, sig)
}

// dumpTrajectoryDebug 将轨迹写入 JSON 调试文件。
// Python: TeamSkillEvolutionRail._dump_trajectory_debug(trajectory)
func (r *TeamSkillEvolutionRail) dumpTrajectoryDebug(traj *trajectory.Trajectory) {
	if traj == nil {
		return
	}

	debugDir := r.trajectoriesDir
	if debugDir == "" && len(r.evolutionStore.BaseDirs()) > 0 {
		debugDir = filepath.Join(filepath.Dir(r.evolutionStore.BaseDirs()[0]), "_debug")
	}
	if debugDir == "" {
		return
	}

	if err := os.MkdirAll(debugDir, 0o755); err != nil {
		logger.Warn(logComponent).Err(err).Msg("[TeamSkillEvolutionRail] 创建调试目录失败")
		return
	}

	ts := time.Now().Format("20060102_150405")
	execID := traj.ExecutionID
	if len(execID) > 8 {
		execID = execID[:8]
	}
	path := filepath.Join(debugDir, fmt.Sprintf("trajectory_%s_%s.json", ts, execID))

	type stepEntry struct {
		Kind            string         `json:"kind"`
		ToolName        string         `json:"tool_name,omitempty"`
		CallArgs        string         `json:"call_args,omitempty"`
		CallResult      string         `json:"call_result,omitempty"`
		ResponsePreview string         `json:"response_preview,omitempty"`
		Meta            map[string]any `json:"meta,omitempty"`
	}

	stepsData := make([]stepEntry, 0, len(traj.Steps))
	for _, step := range traj.Steps {
		entry := stepEntry{Kind: string(step.Kind)}
		if step.Detail != nil {
			switch string(step.Kind) {
			case "tool":
				if td, ok := step.Detail.(*trajectory.ToolCallDetail); ok && td != nil {
					entry.ToolName = td.ToolName
					argsStr := fmt.Sprintf("%v", td.CallArgs)
					if len(argsStr) > 500 {
						argsStr = argsStr[:500]
					}
					entry.CallArgs = argsStr
					resultStr := fmt.Sprintf("%v", td.CallResult)
					if len(resultStr) > 500 {
						resultStr = resultStr[:500]
					}
					entry.CallResult = resultStr
				}
			case "llm":
				if ld, ok := step.Detail.(*trajectory.LLMCallDetail); ok && ld != nil {
					respStr := fmt.Sprintf("%v", ld.Response)
					if len(respStr) > 300 {
						respStr = respStr[:300]
					}
					entry.ResponsePreview = respStr
				}
			}
		}
		if step.Meta != nil {
			entry.Meta = step.Meta
		}
		stepsData = append(stepsData, entry)
	}

	dump := map[string]any{
		"execution_id": traj.ExecutionID,
		"session_id":   traj.SessionID,
		"source":       traj.Source,
		"step_count":   len(traj.Steps),
		"steps":        stepsData,
	}

	data, err := json.MarshalIndent(dump, "", "  ")
	if err != nil {
		logger.Warn(logComponent).Err(err).Msg("[TeamSkillEvolutionRail] 轨迹 JSON 序列化失败")
		return
	}

	if err := os.WriteFile(path, data, 0o644); err != nil {
		logger.Warn(logComponent).Err(err).Msg("[TeamSkillEvolutionRail] 轨迹写入失败")
		return
	}
	logger.Info(logComponent).Str("path", path).Msg("[TeamSkillEvolutionRail] 轨迹已导出")
}

// emitBackgroundOutcomeEvent 将后台执行结果写入主机事件缓冲。
// 对齐 Python: EvolutionRail._emit_background_outcome_event(outcome)
func (r *TeamSkillEvolutionRail) emitBackgroundOutcomeEvent(outcome map[string]string) {
	status := outcome["status"]
	if status == "" {
		status = "unknown"
	}
	meta := EvolutionHostEventMeta{
		EventKind:  EvolutionEventKindOutcome,
		RailKind:   stringPtr(outcome["rail_kind"]),
		Stage:      stringPtr(outcome["stage"]),
		SkillName:  stringPtr(outcome["skill_name"]),
		RequestID:  stringPtr(outcome["request_id"]),
		SignalType: stringPtr(outcome["signal_type"]),
		Source:     stringPtr(outcome["source"]),
		Status:     &status,
	}

	message := outcome["message"]
	if message == "" {
		message = "后台演进以未知结果完成"
	}

	r.EmitHostEvent(&stream.OutputSchema{
		Type:  "llm_reasoning",
		Index: 0,
		Payload: map[string]any{
			"content":         "[Team Skill Evolution] " + message + "\n",
			"_evolution_meta": meta.ToPayload(),
		},
	})
}

// ─── 模块级辅助函数 ───

// isCompletedTeamTaskView 检查 view_task 结果是否显示所有任务已完成。
// 对齐 Python: is_completed_team_task_view(result)
//
//	逻辑：结果中包含 "completed" 且不包含任何非终态状态
func isCompletedTeamTaskView(result any) bool {
	if result == nil {
		return false
	}
	text := strings.ToLower(fmt.Sprintf("%v", result))
	if !strings.Contains(text, "completed") {
		return false
	}
	for _, state := range teamTaskNonTerminalStates {
		if strings.Contains(text, state) {
			return false
		}
	}
	return true
}

// inferTeamSkillFromTrajectory 从轨迹推断使用的团队技能。
// 对齐 Python: infer_team_skill_from_trajectory(trajectory, known_team_skills)
//
//	遍历轨迹步骤，收集 skill_tool 参数和文本，调用 inferSkillFromTexts
func inferTeamSkillFromTrajectory(traj *trajectory.Trajectory, knownSkills []string) string {
	if traj == nil || len(traj.Steps) == 0 || len(knownSkills) == 0 {
		return ""
	}

	var skillToolPayloads []string
	var texts []string

	for _, step := range traj.Steps {
		if step.Kind != trajectory.StepKindTool || step.Detail == nil {
			continue
		}
		if td, ok := step.Detail.(*trajectory.ToolCallDetail); ok && td != nil {
			if td.ToolName == "skill_tool" {
				skillToolPayloads = append(skillToolPayloads, fmt.Sprintf("%v", td.CallArgs))
			}
			texts = append(texts, fmt.Sprintf("%v", td.CallArgs))
			texts = append(texts, fmt.Sprintf("%v", td.CallResult))
		}
	}

	return inferSkillFromTexts(knownSkills, skillToolPayloads, texts)
}

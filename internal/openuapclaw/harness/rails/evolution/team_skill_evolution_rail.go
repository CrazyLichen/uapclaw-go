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

	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_evolving/checkpointing"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_evolving/experience"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_evolving/optimizer/llm_resilience"
	skillopt "github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_evolving/optimizer/skill_call"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_evolving/signal"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_evolving/trajectory"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_evolving/updater/single_dim"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/foundation/llm"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/operator/skill_call"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/session/stream"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/single_agent/interfaces"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/sys_operation"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/harness/rails/evolution/review"
)

// ──────────────────────────── 结构体 ────────────────────────────

// TeamSkillEvolutionRail 团队技能演进护栏，在团队任务完成后自动触发演进。
// 嵌入 EvolutionRail 基类获得自动轨迹收集和异步触发能力，
// 通过实现 EvolutionExtension 接口的 10 个方法完成子类多态分派。
// 与 SkillEvolutionRail 为兄弟关系（都嵌入 EvolutionRail），零共享。
// SkillEvolutionRail 处理单 Agent 常规技能（kind=skill），
// TeamSkillEvolutionRail 处理团队技能（kind=team-skill/swarm-skill）。
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

	// signalTrigger 是否触发确定性团队信号演化（Python 新版以 signal_trigger
	// 取代旧 auto_scan；08-13 版 team ctor kwarg signal_trigger）
	signalTrigger bool
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
	// reviewFeedbackCoordinator 评审反馈协调器（25.2b F_62，接口注入）。
	reviewFeedbackCoordinator ReviewFeedbackCoordinator
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
	// completionFollowupPendingSessionID 活跃 follow-up 模式的待排入会话
	// Python: self._completion_followup_pending_session_id
	completionFollowupPendingSessionID *string
	// skipSignalTriggerThisInvoke 本 invoke 已有活动演化活动时跳过被动扫描
	// Python: self._skip_signal_trigger_this_invoke（继承自 SkillEvolutionRail）
	skipSignalTriggerThisInvoke bool
	// reviewTrigger 团队完成是否排入审查自检 follow-up
	// Python: self._review_trigger
	reviewTrigger bool
	// reviewInterval 审查自检间隔（团队版继承基类语义，占位）
	reviewInterval int
	// reviewAgentMaxIterations 审查子 agent 最大迭代数
	reviewAgentMaxIterations int
	// reviewFeedbackSkillCreateRail 绑定的创建 rail（审查反馈重复模式复用）
	// Python: self._review_feedback_skill_create_rail
	reviewFeedbackSkillCreateRail any
	// reviewFeedbackApprovalContinuations 审批后续动作（request_id → continuation）
	// Python: self._review_feedback_approval_continuations
	reviewFeedbackApprovalContinuations map[string]string
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
	// reviewRuntime 审查运行时（configure 注入，供活动演化工具共享）
	// Python: self._review_runtime
	reviewRuntime *review.EvolutionReviewRuntime
	// trajectorySpanProcessor 共享观测处理器（configure 注入；消费面偏差
	// 同 SkillEvolutionRail）
	trajectorySpanProcessor *trajectory.TrajectorySpanProcessor
}

// TeamSkillEvolutionRailOption TeamSkillEvolutionRail 构造选项函数。
type TeamSkillEvolutionRailOption func(*TeamSkillEvolutionRail)

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
// 必选参数：skillsDir（技能目录）、llmModel（LLM 客户端）、model（模型名称）、language（语言）。
// 可选参数通过 Functional Options 覆盖默认配置。
// 对齐 Python: TeamSkillEvolutionRail.__init__(
//
//	skills_dir, llm, model, language, trajectory_store, team_trajectory_store,
//	trajectory_source, trajectory_sink, member_role, auto_scan, auto_save,
//	async_evolution, max_concurrent_evolution, team_id, trajectories_dir,
//	user_request_llm_policy, trajectory_issue_llm_policy, record_llm_policy,
//	evaluate_llm_policy, simplify_llm_policy, eval_interval,
//	evolution_total_timeout_secs, disabled_skills
//
//	Python: )
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
		signalTrigger:            false,
		reviewInterval:           5,
		reviewAgentMaxIterations: 40,
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
		Msg("[TeamSkillEvolutionRail] initialized")

	return r
}

// WithTeamSkillSignalTrigger 设置是否触发确定性团队信号演化。
// Python: TeamSkillEvolutionRail(signal_trigger=...)（08-13 新版 kwarg）
func WithTeamSkillSignalTrigger(v bool) TeamSkillEvolutionRailOption {
	return func(r *TeamSkillEvolutionRail) { r.signalTrigger = v }
}

// WithTeamSkillReviewTrigger 设置团队完成是否排入审查自检 follow-up。
// Python: TeamSkillEvolutionRail(review_trigger=...)
func WithTeamSkillReviewTrigger(v bool) TeamSkillEvolutionRailOption {
	return func(r *TeamSkillEvolutionRail) { r.reviewTrigger = v }
}

// WithTeamSkillReviewInterval 设置审查自检间隔。
// Python: TeamSkillEvolutionRail(review_interval=5)
func WithTeamSkillReviewInterval(v int) TeamSkillEvolutionRailOption {
	return func(r *TeamSkillEvolutionRail) { r.reviewInterval = v }
}

// WithTeamSkillReviewAgentMaxIterations 设置审查子 agent 最大迭代数。
// Python: TeamSkillEvolutionRail(review_agent_max_iterations=40)
func WithTeamSkillReviewAgentMaxIterations(v int) TeamSkillEvolutionRailOption {
	return func(r *TeamSkillEvolutionRail) { r.reviewAgentMaxIterations = v }
}

// WithTeamSkillAutoSave 设置是否自动保存。
func WithTeamSkillAutoSave(v bool) TeamSkillEvolutionRailOption {
	return func(r *TeamSkillEvolutionRail) { r.autoSave = v }
}

// WithTeamEvolutionReviewRuntime 注入共享审查运行时。
// Python: TeamSkillEvolutionRail(review_runtime=...)
func WithTeamEvolutionReviewRuntime(runtime *review.EvolutionReviewRuntime) TeamSkillEvolutionRailOption {
	return func(r *TeamSkillEvolutionRail) { r.reviewRuntime = runtime }
}

// WithTeamEvolutionTrajectorySpanProcessor 注入共享观测处理器。
// Python: TeamSkillEvolutionRail(trajectory_span_processor=...)
func WithTeamEvolutionTrajectorySpanProcessor(processor *trajectory.TrajectorySpanProcessor) TeamSkillEvolutionRailOption {
	return func(r *TeamSkillEvolutionRail) { r.trajectorySpanProcessor = processor }
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

// 【12-04 迁移注记】WithTeamSkillTrajectorySink 已删除（上游无 set_trajectory_sink，
// registry 链规则 8 删除；跨成员可见性改由 TrajectorySpanProcessor 订阅窗口承载）。

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
	// Python: _on_before_invoke（:492-497）——重置 invoke 局部被动完成状态
	r.passiveEvolutionPending = false
	r.skipSignalTriggerThisInvoke = false
	return nil
}

// OnAfterModelCall 空实现——团队演化不在 model_call 后触发。
// Python: TeamSkillEvolutionRail 无此覆写
func (r *TeamSkillEvolutionRail) OnAfterModelCall(_ context.Context, _ *agentinterfaces.AgentCallbackContext) error {
	return nil
}

// OnAfterToolCall 在每次工具调用后检测经验详情读取和团队任务完成。
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

	// Python: if not self._signal_trigger and not self._review_trigger: return（:594-596）
	if !r.signalTrigger && !r.reviewTrigger {
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
		Msg("[TeamSkillEvolutionRail] view_task intercepted")

	// Python: completed = self._all_tasks_completed(inputs.tool_result)
	completed := isCompletedTeamTaskView(inputs.ToolResult)
	logger.Debug(logComponent).
		Bool("completed", completed).
		Str("session_id", r.currentBuilderSessionID()).
		Msg("[TeamSkillEvolutionRail] view_task completion check result")

	if !completed {
		logger.Info(logComponent).Msg("[TeamSkillEvolutionRail] view_task: task still in progress, skipping")
		return nil
	}

	// Python: self._mark_team_completion_pending()（:613）
	r.markTeamCompletionPending()
	return nil
}

// OnAfterInvoke 空实现——触发由基类 AllowEvolutionTrigger 控制。
// Python: TeamSkillEvolutionRail 无此覆写
func (r *TeamSkillEvolutionRail) OnAfterInvoke(_ context.Context, _ *agentinterfaces.AgentCallbackContext) error {
	return nil
}

// OnAfterTaskIteration 团队完成时经任务循环排入活跃审查 follow-up。
// Python: _on_after_task_iteration（:632-658）。
func (r *TeamSkillEvolutionRail) OnAfterTaskIteration(ctx context.Context, cbc *agentinterfaces.AgentCallbackContext) error {
	if !r.reviewTrigger {
		return nil
	}
	if r.completionFollowupPendingSessionID == nil {
		return nil
	}
	pendingSessionID := *r.completionFollowupPendingSessionID
	sessionID := r.currentBuilderSessionID()
	if pendingSessionID == "" || pendingSessionID != sessionID {
		return nil
	}
	if !r.enqueueTaskIterationFollowup(ctx, cbc, r.buildTeamCompletionFollowupPrompt(), "team completion") {
		return nil
	}
	r.completionFollowupPendingSessionID = nil
	r.hostCompletionPendingSessionID = nil
	return nil
}

// OnAfterEvolutionTriggered 在 after_invoke 演化触发完成后消费外部完成标记。
// Python: TeamSkillEvolutionRail._on_after_evolution_triggered(trajectory, ctx)
func (r *TeamSkillEvolutionRail) OnAfterEvolutionTriggered(_ context.Context, traj *trajectory.Trajectory, _ *agentinterfaces.AgentCallbackContext) error {
	if r.hostCompletionPendingSessionID != nil && traj != nil && *r.hostCompletionPendingSessionID == sessionIDString(traj) {
		r.hostCompletionPendingSessionID = nil
	}
	return nil
}

// AllowEvolutionTrigger 返回当前触发点是否允许启动演化。
// 仅在团队任务完成时（passiveEvolutionPending 或 hostCompletionPending）允许触发。
// Python: TeamSkillEvolutionRail._allow_evolution_trigger(trigger_point, ctx)
func (r *TeamSkillEvolutionRail) AllowEvolutionTrigger(_ EvolutionTriggerPoint, _ *agentinterfaces.AgentCallbackContext) bool {
	// Python: _allow_evolution_trigger（:616-631）——skip 门 + review_trigger
	// 排除 + passive/host 完成标记判定。
	if r.skipSignalTriggerThisInvoke {
		logger.Info(logComponent).Msg("[TeamSkillEvolutionRail] active evolution activity detected, skip passive signal scan")
		return false
	}
	if r.reviewTrigger {
		return false
	}
	return r.signalTrigger && (r.passiveEvolutionPending ||
		r.hostCompletionPending())
}

// SnapshotForEvolution 同步捕获快照，供后台演化任务使用。
// Python: TeamSkillEvolutionRail._snapshot_for_evolution(trajectory, ctx)
//
//	捕获轨迹 + 消息 + skill_name="team-skill" + presentedEntries
func (r *TeamSkillEvolutionRail) SnapshotForEvolution(ctx context.Context, traj *trajectory.Trajectory, cbc *agentinterfaces.AgentCallbackContext) *EvolutionSnapshot {
	if !r.signalTrigger {
		return nil
	}

	// 收集基础快照
	messages := trajectoryToMessages(traj)
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
		SessionID:        sessionIDString(traj),
	}
}

// RunEvolution 执行团队技能演化。
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
	if !r.signalTrigger {
		logger.Info(logComponent).Msg("[TeamSkillEvolutionRail] auto_scan disabled, skipping")
		return nil
	}

	// 对齐 Python: try: ... except Exception as exc: logger.error(...); _emit_progress("failed", ...)
	defer func() {
		if rec := recover(); rec != nil {
			logger.Error(logComponent).
				Any("panic", rec).
				Msg("[TeamSkillEvolutionRail] run_evolution global exception caught")
			r.emitProgress("failed", fmt.Sprintf("团队技能演进因意外错误失败: %v", rec))
		}
	}()

	t0 := time.Now()
	defer func() {
		elapsed := time.Since(t0).Seconds()
		logger.Info(logComponent).Float64("elapsed_secs", elapsed).Msg("[TeamSkillEvolutionRail] run_evolution completed")
	}()

	// Python: emit_progress("started", ...)
	r.emitProgress("started", "team tasks completed; starting team skill evolution analysis")

	// Python: trajectory = prepared.trajectory; messages = deepcopy(...); presented_entries = deepcopy(...)
	var messages []map[string]any
	var presentedEntries []experience.PresentedRecordEntry
	if snapshot != nil {
		messages = snapshot.Messages
		presentedEntries = snapshot.PresentedEntries
	} else if traj != nil {
		messages = trajectoryToMessages(traj)
	}

	// Python: used_skill = self._detect_used_team_skill(trajectory)
	usedSkill := r.detectUsedTeamSkill(ctx, traj)
	if usedSkill == "" {
		logger.Info(logComponent).Msg("[TeamSkillEvolutionRail] no existing skill detected, skipping")
		r.emitProgress("cancelled",
			"no skill usage of a team/swarm skill detected in trajectory; cancelling team skill evolution analysis")
		r.evaluatePresentedEntries(ctx, presentedEntries)
		return nil
	}
	logger.Info(logComponent).Str("skill_name", usedSkill).Msg("[TeamSkillEvolutionRail] detected existing skill")

	// Python: signals = self._detect_rule_signals(trajectory=trajectory, skill_name=used_skill)（:761）
	signals := r.DetectRuleSignals(ctx, traj, usedSkill)
	if len(signals) == 0 {
		logger.Info(logComponent).Str("skill_name", usedSkill).Msg("[TeamSkillEvolutionRail] no signals detected")
		r.emitProgress("cancelled",
			fmt.Sprintf("no actionable evolution signals detected for '%s'; cancelling team skill evolution analysis", usedSkill),
			WithSkillName(usedSkill))
		r.evaluatePresentedEntries(ctx, presentedEntries)
		return nil
	}

	// Python: _emit_progress("detecting_signals", f"detected {len(signals)} deterministic rule signal(s) ...")
	r.emitProgress("detecting_signals",
		fmt.Sprintf("detected %d deterministic rule signal(s) for '%s'", len(signals), usedSkill),
		WithSkillName(usedSkill))

	// Python: online_result = await self._handle_evolution_from_signals_with_result(...)（:790-797）
	request, err := r.HandleEvolutionFromSignalsWithResult(ctx, usedSkill, traj, signals,
		r.autoSave, "", messages, true)
	if err != nil {
		logger.Warn(logComponent).Err(err).Str("skill_name", usedSkill).
			Msg("[TeamSkillEvolutionRail] handleEvolutionFromSignals failed")
	}

	// Python: outcome/no-request/request-ready 三分支 progress（:798-812）
	if request == nil {
		r.emitProgress("completed", "no evolution records generated")
	} else {
		r.emitProgress("completed", fmt.Sprintf("evolution request ready for '%s'", usedSkill),
			WithSkillName(usedSkill), WithRequestID(request.RequestID))
	}

	// Python: await self._evaluate_presented_entries(presented_entries, messages)
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
// 对齐 Python: TeamSkillEvolutionRail.notify_team_completed(ctx)
//
//	检查 signal/review trigger、builder 可用性，markTeamCompletionPending
func (r *TeamSkillEvolutionRail) NotifyTeamCompleted(_ context.Context) (bool, error) {
	if !r.signalTrigger {
		logger.Info(logComponent).Msg("[TeamSkillEvolutionRail] notify_team_completed ignored due to auto_scan disabled")
		return false, nil
	}

	sessionID := r.currentBuilderSessionID()
	if sessionID == "" {
		logger.Warn(logComponent).Msg("[TeamSkillEvolutionRail] notify_team_completed: no trajectory available (before_invoke may not have fired)")
		return false, nil
	}

	r.hostCompletionPendingSessionID = &sessionID
	logger.Debug(logComponent).
		Str("session_id", sessionID).
		Msg("[TeamSkillEvolutionRail] notify_team_completed marked")
	return true, nil
}

// RequestUserEvolution 用户主动触发演化入口。
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
		// 12-04：空轨迹兜底改经离线组装器（span 模型，Python 无对应字面量）。
		empty, buildErr := emptySpanTrajectoryForRails("user_triggered", "user_triggered")
		if buildErr != nil {
			return nil, buildErr
		}
		traj = empty
	}

	// Python: trajectory = self._aggregate_team_trajectory(trajectory)
	traj = r.aggregateTeamTrajectory(traj)

	// Python: messages = self._collect_messages_from_trajectory(trajectory) if trajectory.steps else []
	// span 模型：以工作 span 存在性等价「steps 非空」判定。
	messages := []map[string]any{}
	if len(trajectory.IterSpans(traj)) > 0 {
		messages = trajectoryToMessages(traj)
	}

	// Python: signals = await self._detect_active_request_signals(...) if trajectory.steps else []
	signals := []*signal.EvolutionSignal{}
	if len(trajectory.IterSpans(traj)) > 0 {
		detected, err := r.detectActiveRequestSignals(ctx, skillName, traj)
		if err != nil {
			logger.Warn(logComponent).Err(err).Str("skill_name", skillName).Msg("[TeamSkillEvolutionRail] active request signal detection failed")
		} else {
			signals = detected
		}
	}

	// Python: if user_intent: self._append_unique_signal(signals, make_team_user_intent_signal(...))
	if userIntent != "" {
		r.teamAppendUniqueSignal(&signals, signal.MakeTeamUserIntentSignal(skillName, userIntent))
	}

	if len(signals) == 0 {
		logger.Info(logComponent).Str("skill_name", skillName).Msg("[TeamSkillEvolutionRail] request_user_evolution: no evidence or user intent")
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
		logger.Warn(logComponent).Err(err).Str("skill_name", skillName).Msg("[TeamSkillEvolutionRail] request_user_evolution evolution handling failed")
		return &EvolutionRequestResult{SkillName: skillName}, nil
	}

	if request == nil {
		logger.Info(logComponent).Str("skill_name", skillName).Msg("[TeamSkillEvolutionRail] request_user_evolution: no record generated")
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
		Msg("[TeamSkillEvolutionRail] user approved records")
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
			Msg("[TeamSkillEvolutionRail] user rejected records")
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
		Msg("[TeamSkillEvolutionRail] simplify staged")

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
	logger.Info(logComponent).Str("request_id", requestID).Msg("[TeamSkillEvolutionRail] simplify approved")
	return result, nil
}

// OnRejectSimplify 丢弃精简请求。
// Python: TeamSkillEvolutionRail.on_reject_simplify(request_id)
func (r *TeamSkillEvolutionRail) OnRejectSimplify(requestID string) {
	r.manager.RejectSimplify(requestID)
	logger.Info(logComponent).Str("request_id", requestID).Msg("[TeamSkillEvolutionRail] simplify rejected")
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
	logger.Info(logComponent).Str("skill_name", skillName).Msg("[TeamSkillEvolutionRail] rebuild prompt generated")
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

// HasPendingApprovalSnapshot 检查是否有指定 requestID 的待审批快照。
// 对齐 Python: request_id in self._pending_approval_snapshots
func (r *TeamSkillEvolutionRail) HasPendingApprovalSnapshot(requestID string) bool {
	_, ok := r.pendingApprovalSnapshots[requestID]
	return ok
}

// SignalTrigger 返回是否触发确定性团队信号演化。
// Python: signal_trigger property（team_skill_evolution_rail.py:422-426）
func (r *TeamSkillEvolutionRail) SignalTrigger() bool { return r.signalTrigger }

// SetSignalTrigger 设置是否触发确定性团队信号演化。
// Python: signal_trigger.setter（:427-430）
func (r *TeamSkillEvolutionRail) SetSignalTrigger(v bool) { r.signalTrigger = v }

// ReviewTrigger 返回团队完成是否排入审查自检 follow-up。
// Python: review_trigger property（:431-435）
func (r *TeamSkillEvolutionRail) ReviewTrigger() bool { return r.reviewTrigger }

// SetReviewTrigger 设置团队完成是否排入审查自检 follow-up。
// Python: review_trigger.setter（:436-439）
func (r *TeamSkillEvolutionRail) SetReviewTrigger(v bool) { r.reviewTrigger = v }

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
// 对齐 Python: SkillEvolutionRail.set_sys_operation(sys_operation)
// TeamSkillEvolutionRail 同理
func (r *TeamSkillEvolutionRail) SetSysOperation(op sys_operation.SysOperation) {
	// Python: super().set_sys_operation(sys_operation)
	r.EvolutionRail.SetSysOperation(op)
	// Python: self._evolution_store.sys_operation = sys_operation
	r.evolutionStore.SetSysOperation(op)
}

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
		logger.Info(logComponent).Msg("[TeamSkillEvolutionRail] no existing team skill on disk")
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
			Msg("[TeamSkillEvolutionRail] no team-skill type skills found")
		return ""
	}

	best := inferTeamSkillFromTrajectory(traj, knownSkills)
	if best != "" {
		logger.Info(logComponent).Str("skill_name", best).Msg("[TeamSkillEvolutionRail] team skill detected from trajectory")
		return best
	}

	logger.Info(logComponent).Msg("[TeamSkillEvolutionRail] no SKILL.md read trace found in trajectory")
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
	// 25.2b：聚合源 = rail 自身 team scope 干净窗口（Task 3 GetTrajectory；
	// registry 死字段链按规则 8 删除）。
	teamID := r.teamID
	if teamID == "" {
		teamID = "unknown"
	}

	sessionID := sessionIDString(traj)

	// Python: team_traj = rail.get_trajectory(team_id=team_id, session_id=...)
	teamTraj, err := r.RailGetTrajectory(sessionID, teamID)
	if err != nil || teamTraj == nil || len(trajectory.IterSpans(teamTraj)) == 0 {
		return traj
	}

	// Python: emit_progress("detecting_signals", "aggregated ...")
	memberCount := 0
	if mc, ok := teamTraj.Metadata()["member_count"].(int); ok {
		memberCount = mc
	}
	r.emitProgress("detecting_signals", fmt.Sprintf("聚合 %d 个成员，%d 个协作步骤", memberCount, len(trajectory.IterSpans(teamTraj))))
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
		logger.Warn(logComponent).Err(err).Str("skill_name", skillName).Msg("[TeamSkillEvolutionRail] active request trajectory detection failed")
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
	return r.teamSkillForExperienceDetailFile(ctx, filePath)
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// extractToolContentFromInputs 从工具调用结果中提取内容文本。
// 两条演进 rail（SkillEvolutionRail / TeamSkillEvolutionRail）共用同一套提取顺序：
// result["data"] 子字段 → result 顶层 → ToolMsg 内容 → result 字符串。
func extractToolContentFromInputs(inputs *agentinterfaces.ToolCallInputs) string {
	result := inputs.ToolResult
	if result != nil {
		// Python: data = getattr(result, "data", None); if isinstance(data, dict): ...
		// 先检查 result["data"] 中间层，再直接从 result 取
		if resultMap, ok := result.(map[string]any); ok {
			// 优先从 data 子字段取
			if dataField, ok := resultMap["data"].(map[string]any); ok {
				if content, ok := dataField["skill_content"].(string); ok && content != "" {
					return content
				}
				if content, ok := dataField["content"].(string); ok && content != "" {
					return content
				}
			}
			// data 子字段不存在或未命中，回退到直接从 result 取
			if content, ok := resultMap["skill_content"].(string); ok && content != "" {
				return content
			}
			if content, ok := resultMap["content"].(string); ok && content != "" {
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

	content := extractToolContentFromInputs(inputs)
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
	traj, err := r.Builder().Build()
	if err != nil {
		logger.Warn(logComponent).Err(err).Msg("[TeamSkillEvolutionRail] trajectory build failed")
		return nil
	}
	return traj
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
// 对齐 Python: TeamSkillEvolutionRail._handle_evolution_from_signals(
//
//	skill_name, trajectory, signals, auto_approve, user_query, messages, emit_host_events
//
//	Python: )
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
				Msg("[TeamSkillEvolutionRail] signal consumed, records staged pending approval")
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
		logger.Info(logComponent).Str("skill_name", skillName).Msg("[TeamSkillEvolutionRail] signal consumed, record auto-approved")
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
	// 没有轨迹就没什么可导出的。
	if traj == nil {
		return
	}

	// 确定导出目录：优先用配置的目录，否则放到演进库父目录下的 _debug 子目录。
	debugDir := r.trajectoriesDir
	if debugDir == "" && len(r.evolutionStore.BaseDirs()) > 0 {
		debugDir = filepath.Join(filepath.Dir(r.evolutionStore.BaseDirs()[0]), "_debug")
	}
	if debugDir == "" {
		return
	}

	// 目录不存在就先建出来，建不出来只能放弃本次导出。
	if err := os.MkdirAll(debugDir, 0o755); err != nil {
		logger.Warn(logComponent).Err(err).Msg("[TeamSkillEvolutionRail] failed to create debug directory")
		return
	}

	// 文件名带时间戳和轨迹 ID 前 8 位，方便区分不同批次、不同执行。
	ts := time.Now().Format("20060102_150405")
	execID := traj.TrajectoryID()
	if len(execID) > 8 {
		execID = execID[:8]
	}
	path := filepath.Join(debugDir, fmt.Sprintf("trajectory_%s_%s.json", ts, execID))

	// 落盘用的精简结构：只留排查需要的字段（span 读面）。
	type stepEntry struct {
		Kind            string `json:"kind"`
		ToolName        string `json:"tool_name,omitempty"`
		CallArgs        string `json:"call_args,omitempty"`
		CallResult      string `json:"call_result,omitempty"`
		ResponsePreview string `json:"response_preview,omitempty"`
	}

	// 逐 span 转换：工具 span 留名字和参数/结果的前 500 字，LLM span 留最后一条回复的前 300 字。
	spans := trajectory.IterSpans(traj)
	stepsData := make([]stepEntry, 0, len(spans))
	for _, span := range spans {
		category := trajectory.SpanCategory(span)
		if category != "tool" && category != "llm" {
			continue
		}
		entry := stepEntry{Kind: category}
		if category == "tool" {
			call := trajectory.ReadToolCall(span)
			entry.ToolName = fmt.Sprintf("%v", call["name"])
			argsStr := fmt.Sprintf("%v", call["input"])
			if len(argsStr) > 500 {
				argsStr = argsStr[:500]
			}
			entry.CallArgs = argsStr
			resultStr := fmt.Sprintf("%v", call["output"])
			if len(resultStr) > 500 {
				resultStr = resultStr[:500]
			}
			entry.CallResult = resultStr
		} else {
			messages := trajectory.ReadLLMMessages(span, false)
			if len(messages) > 0 {
				respStr := fmt.Sprintf("%v", messages[len(messages)-1]["content"])
				if len(respStr) > 300 {
					respStr = respStr[:300]
				}
				entry.ResponsePreview = respStr
			}
		}
		stepsData = append(stepsData, entry)
	}

	// 组装顶层对象：轨迹标识、会话来源、span 总数以及列表。
	sessionID := sessionIDString(traj)
	dump := map[string]any{
		"trajectory_id": traj.TrajectoryID(),
		"session_id":    sessionID,
		"span_count":    len(spans),
		"spans":         stepsData,
	}

	// 序列化成带缩进的 JSON，方便人直接打开阅读。
	data, err := json.MarshalIndent(dump, "", "  ")
	if err != nil {
		logger.Warn(logComponent).Err(err).Msg("[TeamSkillEvolutionRail] trajectory JSON serialization failed")
		return
	}

	// 写入文件；无论成败都留一条日志，便于确认调试文件到底有没有生成。
	if err := os.WriteFile(path, data, 0o644); err != nil {
		logger.Warn(logComponent).Err(err).Msg("[TeamSkillEvolutionRail] trajectory write failed")
		return
	}
	logger.Info(logComponent).Str("path", path).Msg("[TeamSkillEvolutionRail] trajectory exported")
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
	if traj == nil || len(trajectory.IterSpans(traj)) == 0 || len(knownSkills) == 0 {
		return ""
	}

	var skillToolPayloads []string
	var texts []string

	for _, span := range trajectory.IterSpans(traj) {
		if trajectory.SpanCategory(span) != "tool" {
			continue
		}
		call := trajectory.ReadToolCall(span)
		toolName := fmt.Sprintf("%v", call["name"])
		if toolName == "skill_tool" {
			skillToolPayloads = append(skillToolPayloads, fmt.Sprintf("%v", call["input"]))
		}
		texts = append(texts, fmt.Sprintf("%v", call["input"]))
		texts = append(texts, fmt.Sprintf("%v", call["output"]))
	}

	return inferSkillFromTexts(knownSkills, skillToolPayloads, texts)
}

// ──────────────────────────── 评审反馈挂点（25.2b F_62） ────────────────────────────

// SetupReviewFeedbackEvolution 为标准团队 rail 附挂评审反馈演化。
// 任务反馈被翻译为结构化观察并保留到团队结束；终态聚合复用本挂载 rail 的
// 常规团队 Skill 优化器、持久化、pending 事件与审批路径。
// Python: setup_review_feedback_evolution (team_skill_evolution_rail.py L244-273)。
func (r *TeamSkillEvolutionRail) SetupReviewFeedbackEvolution(sessionID, teamID string, minConfidence float64, coordinator ReviewFeedbackCoordinator) {
	r.reviewFeedbackCoordinator = coordinator
}

// ReviewFeedbackEvolutionEnabled 本挂载 rail 是否接受调度器评审反馈。
// Python: review_feedback_evolution_enabled (team_skill_evolution_rail.py L274-276)。
func (r *TeamSkillEvolutionRail) ReviewFeedbackEvolutionEnabled() bool {
	return r.reviewFeedbackCoordinator != nil
}

// BindReviewFeedbackSkillCreateRail 绑定已挂载、用于重复模式的新建 Skill rail。
// Python: bind_review_feedback_skill_create_rail (team_skill_evolution_rail.py L277-280)。
func (r *TeamSkillEvolutionRail) BindReviewFeedbackSkillCreateRail(rail ReviewFeedbackCoordinator) {
	// Go 侧 creation rail 面与协调器同经接口注入（见 ReviewFeedbackCoordinator）。
	r.reviewFeedbackCoordinator = rail
}

// HandleReviewFeedback 把一条结算失败的评审经 Core 协调器路由。
// Python: handle_review_feedback (team_skill_evolution_rail.py L282-285)。
func (r *TeamSkillEvolutionRail) HandleReviewFeedback(ctx context.Context, payload map[string]any) error {
	if r.reviewFeedbackCoordinator != nil {
		return r.reviewFeedbackCoordinator.HandleReviewFeedback(ctx, payload)
	}
	return nil
}

// FinalizeReviewFeedback 为调度器反馈运行终态团队聚合。
// Python: finalize_review_feedback (team_skill_evolution_rail.py L287-293)。
func (r *TeamSkillEvolutionRail) FinalizeReviewFeedback(ctx context.Context, payload map[string]any) (bool, error) {
	if r.reviewFeedbackCoordinator == nil {
		return false, nil
	}
	return r.reviewFeedbackCoordinator.OnTeamCompleted(ctx, payload)
}

// ReviewLanguage rail 语言（协调器归因提示词消费）。
func (r *TeamSkillEvolutionRail) ReviewLanguage() string { return r.language }

// ReviewRuntime 返回审查运行时。
// Python: TeamSkillEvolutionRail.review_runtime property
func (r *TeamSkillEvolutionRail) ReviewRuntime() *review.EvolutionReviewRuntime {
	return r.reviewRuntime
}

// TrajectorySpanProcessor 返回共享观测处理器。
// Python: TeamSkillEvolutionRail.trajectory_span_processor
func (r *TeamSkillEvolutionRail) TrajectorySpanProcessor() *trajectory.TrajectorySpanProcessor {
	return r.trajectorySpanProcessor
}

// AsAgentRail 返回 rail 的 AgentRail 载体形态（configuration 装配用）。
func (r *TeamSkillEvolutionRail) AsAgentRail() agentinterfaces.AgentRail { return r }

// ApprovalSubmissionService 返回审批提交服务（审批中断 rail 绑定用）。
// Python: TeamSkillEvolutionRail.approval_submission_service property。
// 提交服务缝 ⤴️ 15.24b，当前返回 nil。
func (r *TeamSkillEvolutionRail) ApprovalSubmissionService() EvolutionSubmissionService {
	return nil
}

// ReviewStore 演进存储（协调器证据面消费）。
func (r *TeamSkillEvolutionRail) ReviewStore() *checkpointing.EvolutionStore { return r.evolutionStore }

// ReviewGenerator 优化器（协调器 LLM/模型名消费）。
func (r *TeamSkillEvolutionRail) ReviewGenerator() *skillopt.TeamSkillExperienceOptimizer {
	return r.generator
}

// RailGetTrajectory 会话干净窗口（协调器轨迹证据消费）。
// Python: rail.get_trajectory(session_id=..., team_id=...)。
// 偏差 D-25.2b-17：Go 侧捕获窗口恒 agent scope（rail_scope 捕获键不分区），
// 读会话窗口作为证据面；回收条件 = rail 捕获键支持 team scope。
func (r *TeamSkillEvolutionRail) RailGetTrajectory(sessionID, teamID string) (*trajectory.Trajectory, error) {
	return r.GetTrajectory(sessionID, "", "")
}

// RailDrainApprovalEvents 排干待审批事件（payload map 形态）。
// Python: rail.drain_pending_approval_events(wait=False)。
func (r *TeamSkillEvolutionRail) RailDrainApprovalEvents() []map[string]any {
	events := r.DrainPendingApprovalEvents(false, nil)
	out := make([]map[string]any, 0, len(events))
	for _, event := range events {
		if event != nil {
			if payload, ok := event.Payload.(map[string]any); ok {
				out = append(out, payload)
			}
		}
	}
	return out
}

// ──────────────────────────── 15.24a Task 11：08-13 新版对照回收 ────────────────────────────

// teamCompletionFollowUpTag 团队完成审查 follow-up 包裹标签。
// Python: _AUTO_TEAM_SKILL_EVOLUTION_FOLLOW_UP_TAG（:63）
const teamCompletionFollowUpTag = "auto_team_skill_evolution_review_followup"

// teamCompletionFollowupPromptCN 团队完成自检 follow-up 中文提示词（原文复刻）。
// Python: _TEAM_COMPLETION_FOLLOWUP_PROMPT_CN（:64-69）
const teamCompletionFollowupPromptCN = "这是运行时插入的 Team/Swarm Skill 演进自检，不是用户的新需求。\n" +
	"团队任务已完成；参考常驻“团队 Skill 演进自检”规则，只判断本轮是否存在可复用团队更新，不重新判断" +
	"运行时触发门槛。\n" +
	"如需建议，只在普通最终回复末尾追加一至两句，并同时包含可复用团队更新点和是否发起 Team/Swarm Skill " +
	"演进的确认问题；否则自然回复，不提本提醒或内部判断。"

// teamCompletionFollowupPromptEN 团队完成自检 follow-up 英文提示词（原文复刻）。
// Python: _TEAM_COMPLETION_FOLLOWUP_PROMPT_EN（:70-76）
const teamCompletionFollowupPromptEN = "This runtime-inserted Team/Swarm Skill evolution self-check is not a new user request.\n" +
	"The team task is complete. Refer to the standing \"Team Skill Evolution Self-Check\" rules and judge only whether " +
	"this round contains a reusable team update; do not re-evaluate the runtime trigger threshold.\n" +
	"If suggesting, append only one or two sentences to the normal final reply and include both the reusable team " +
	"update and the Team/Swarm Skill evolution question; otherwise reply naturally without mentioning this reminder " +
	"or internal judgment."

// markTeamCompletionPending 按模式标记团队完成（被动扫描或活跃 follow-up）。
// Python: _mark_team_completion_pending（:820-828）。
func (r *TeamSkillEvolutionRail) markTeamCompletionPending() {
	sessionID := r.currentBuilderSessionID()
	if r.reviewTrigger {
		sid := sessionID
		r.completionFollowupPendingSessionID = &sid
		return
	}
	if sessionID != "" {
		sid := sessionID
		r.hostCompletionPendingSessionID = &sid
	}
	r.markPassiveEvolutionPending()
}

// hostCompletionPending host 完成标记是否命中当前会话。
// Python: self._host_completion_pending_session_id == self._current_trajectory_session_id()（:629-630）。
func (r *TeamSkillEvolutionRail) hostCompletionPending() bool {
	sessionID := r.currentBuilderSessionID()
	if sessionID == "" || r.hostCompletionPendingSessionID == nil {
		return false
	}
	return *r.hostCompletionPendingSessionID == sessionID
}

// buildTeamCompletionFollowupPrompt 构建团队完成审查 follow-up 提示词。
// Python: _build_team_completion_followup_prompt（:829-833）。
func (r *TeamSkillEvolutionRail) buildTeamCompletionFollowupPrompt() string {
	prompt := teamCompletionFollowupPromptCN
	if r.language == "en" {
		prompt = teamCompletionFollowupPromptEN
	}
	return "<" + teamCompletionFollowUpTag + ">\n" + prompt + "\n</" + teamCompletionFollowUpTag + ">"
}

// OwnsApprovalRequest 判断请求是否由本 rail 或其 Core 子件持有。
// Python: owns_approval_request（:316-323）。
func (r *TeamSkillEvolutionRail) OwnsApprovalRequest(requestID string) bool {
	if _, ok := r.pendingApprovalSnapshots[requestID]; ok {
		return true
	}
	_, ok := r.pendingGovernance[requestID]
	return ok
}

// PopApprovalContinuation 取出并移除请求的审批后续动作。
// Python: pop_approval_continuation（:324-331）。
func (r *TeamSkillEvolutionRail) PopApprovalContinuation(requestID string) string {
	if r.reviewFeedbackApprovalContinuations == nil {
		return ""
	}
	continuation, ok := r.reviewFeedbackApprovalContinuations[requestID]
	if !ok {
		return ""
	}
	delete(r.reviewFeedbackApprovalContinuations, requestID)
	return continuation
}

// enqueueTaskIterationFollowup 从常规任务循环迭代排入 follow-up。
// Python: _enqueue_task_iteration_followup（skill_evolution_rail.py:600-622，team 继承）。
func (r *TeamSkillEvolutionRail) enqueueTaskIterationFollowup(ctx context.Context, cbc *agentinterfaces.AgentCallbackContext, prompt, logPrefix string) bool {
	if cbc == nil || cbc.Agent() == nil {
		logger.Warn(logComponent).
			Str("log_prefix", logPrefix).
			Msg("[TeamSkillEvolutionRail] follow-up dropped: no agent available")
		return false
	}
	controller := getLoopController(cbc.Agent())
	if controller == nil {
		logger.Warn(logComponent).
			Str("log_prefix", logPrefix).
			Msg("[TeamSkillEvolutionRail] follow-up dropped: no TaskLoopController available")
		return false
	}
	controller.EnqueueFollowUp(prompt)
	return true
}

// DetectRuleSignals 检测确定性执行/脚本信号并归属到团队技能。
// Python: _detect_rule_signals（:1112-1134）。
func (r *TeamSkillEvolutionRail) DetectRuleSignals(ctx context.Context, traj *trajectory.Trajectory, skillName string) []*signal.EvolutionSignal {
	detector := signal.NewConversationSignalDetector(
		signal.WithExistingSkills(map[string]bool{skillName: true}),
	)
	var detected []*signal.EvolutionSignal
	func() {
		defer func() {
			// Python: except Exception → logger.warning + []
			if rec := recover(); rec != nil {
				logger.Warn(logComponent).
					Str("skill_name", skillName).
					Any("panic", rec).
					Msg("[TeamSkillEvolutionRail] rule signal detection failed")
				detected = nil
			}
		}()
		detected = detector.DetectTrajectorySignals(traj, nil)
	}()

	signals := make([]*signal.EvolutionSignal, 0, len(detected))
	for _, sig := range detected {
		if sig.SkillName != nil && *sig.SkillName != skillName {
			continue
		}
		sig.SkillName = &skillName
		r.teamAppendUniqueSignal(&signals, sig)
	}
	return signals
}

// HandleEvolutionFromSignalsWithResult 处理演化并保留编排器状态供活动 API 使用。
// Python: _handle_evolution_from_signals_with_result（:1135-1220）——
// Go 侧既有 handleEvolutionFromSignals 已承载同款流程（staging + outcome
// 事件 + 审批事件/auto-approved），此处作 API 形态别名（Python 拆分
// with_result 与 wrapper 的目的在活动 API 返回编排结果，Go 返回值一致）。
func (r *TeamSkillEvolutionRail) HandleEvolutionFromSignalsWithResult(
	ctx context.Context,
	skillName string,
	traj *trajectory.Trajectory,
	signals []*signal.EvolutionSignal,
	autoApprove bool,
	userQuery string,
	messages []map[string]any,
	emitHostEvents bool,
) (*experience.ExperienceApprovalRequest, error) {
	return r.handleEvolutionFromSignals(ctx, skillName, traj, signals,
		autoApprove, userQuery, messages, emitHostEvents)
}

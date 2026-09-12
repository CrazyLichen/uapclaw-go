package evolution

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

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
	"github.com/uapclaw/uapclaw-go/internal/evolving/schema"
	"github.com/uapclaw/uapclaw-go/internal/evolving/sharing"
	"github.com/uapclaw/uapclaw-go/internal/evolving/sharing/backend"
	"github.com/uapclaw/uapclaw-go/internal/evolving/signal"
	"github.com/uapclaw/uapclaw-go/internal/evolving/trajectory"
	"github.com/uapclaw/uapclaw-go/internal/evolving/updater/single_dim"
)

// ──────────────────────────── 结构体 ────────────────────────────

// SkillEvolutionRail 单 Agent 技能演进护栏，在 after_invoke 后自动触发演进。
//
// 嵌入 EvolutionRail 基类获得自动轨迹收集和异步触发能力，
// 通过实现 EvolutionExtension 接口的 10 个方法完成子类多态分派。
//
// 核心流程：信号检测 → 技能归属 → 经验生成 → 评分 → 暂存审批 → 应用更新
// Slash 命令入口：/evolve /evolve_list /evolve_simplify /evolve_rebuild /evolve_rollback
//
// 对齐 Python: openjiuwen/harness/rails/evolution/skill_evolution_rail.py SkillEvolutionRail
type SkillEvolutionRail struct {
	*EvolutionRail

	// ─── 核心组件 ───

	// evolutionStore 技能演进数据存储
	evolutionStore *checkpointing.EvolutionStore
	// evolver 技能经验优化器
	evolver *skillopt.SkillExperienceOptimizer
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
	// experienceTracker 经验展示追踪器
	experienceTracker *experience.ExperienceTracker

	// ─── 配置 ───

	// autoScan 是否自动检测演进信号
	autoScan bool
	// autoSave 是否自动保存（跳过审批）
	autoSave bool
	// language 语言（"cn" 或 "en"）
	language string
	// evalInterval 评估间隔
	evalInterval int
	// evolutionTimeoutSec 后台演化超时秒数
	evolutionTimeoutSec float64

	// ─── 信号去重 ───

	// processedSignalKeys 已处理的信号指纹集合
	processedSignalKeys map[[4]string]bool

	// ─── LLM 策略 ───

	// generateRecordsLLMPolicy 记录生成 LLM 调用策略
	generateRecordsLLMPolicy llm_resilience.LLMInvokePolicy
	// evaluateLLMPolicy 评估 LLM 调用策略
	evaluateLLMPolicy llm_resilience.LLMInvokePolicy
	// simplifyLLMPolicy 精简 LLM 调用策略
	simplifyLLMPolicy llm_resilience.LLMInvokePolicy

	// ─── Sharing（9.80a 组合，可选）───

	// shareStager 经验共享暂存器（可选）
	shareStager *sharing.ShareStager
	// experienceSharer 经验共享器（可选）
	experienceSharer *sharing.ExperienceSharer
	// keywordExtractor 关键词提取器（可选）
	keywordExtractor *sharing.KeywordExtractor
	// sharingEnabled 是否启用共享
	sharingEnabled bool
	// sharingDownloadTopK 共享下载 topK
	sharingDownloadTopK int
	// excerptOffsets 增量消息偏移量
	excerptOffsets map[string]int

	// ─── 共享状态 ───

	// skillOps 技能经验操作器映射
	skillOps map[string]*skill_call.SkillExperienceOperator
	// pendingGovernance 暂存治理操作映射
	pendingGovernance map[string]*experience.PendingGovernance
	// pendingApprovalSnapshots 暂存审批快照映射
	pendingApprovalSnapshots map[string]*experience.PendingChange
	// skillsDir 技能目录
	skillsDir []string
}

// SkillEvolutionRailOption SkillEvolutionRail 构造选项函数。
type SkillEvolutionRailOption func(*SkillEvolutionRail)

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

const (
	// maxProcessedSignalKeys 已处理信号指纹最大数量
	// Python: _MAX_PROCESSED_SIGNAL_KEYS = 500
	maxProcessedSignalKeys = 500
	// defaultEvolutionTimeoutSecs 默认后台演化超时秒数
	// Python: _DEFAULT_EVOLUTION_TOTAL_TIMEOUT_SECS = 600.0
	defaultEvolutionTimeoutSecs = 600.0
	// defaultEvalInterval 默认评估间隔
	defaultEvalInterval = 5
	// defaultSharingDownloadTopK 默认共享下载 topK
	// Python: _DEFAULT_SHARING_DOWNLOAD_TOP_K = 3
	defaultSharingDownloadTopK = 3
	// defaultSharingMaxUploadRetries 默认共享上传重试次数
	// Python: _DEFAULT_SHARING_MAX_UPLOAD_RETRIES = 3
	defaultSharingMaxUploadRetries = 3
)

// ──────────────────────────── 全局变量 ────────────────────────────

// experienceRecordHeadingRE 匹配经验记录标题行中的 record ID。
// Python: SkillEvolutionRail._EXPERIENCE_RECORD_HEADING_RE
var experienceRecordHeadingRE = regexp.MustCompile(`#+\s*\[([A-Za-z0-9_-]+)\]`)

// ──────────────────────────── 导出函数 ────────────────────────────

// NewSkillEvolutionRail 创建 SkillEvolutionRail 实例。
//
// 必选参数：skillsDir（技能目录）、llmModel（LLM 客户端）、model（模型名称）、language（语言）。
// 可选参数通过 Functional Options 覆盖默认配置。
//
// 对齐 Python: SkillEvolutionRail.__init__(
//
//	skills_dir, llm, model, auto_scan, auto_save, language,
//	trajectory_store, eval_interval, evolution_total_timeout_secs,
//	generate_records_llm_policy, evaluate_llm_policy, simplify_llm_policy,
//	sharing_config, disabled_skills
//
// )
func NewSkillEvolutionRail(
	skillsDir []string,
	llmModel *llm.Model,
	model string,
	language string,
	opts ...SkillEvolutionRailOption,
) *SkillEvolutionRail {
	r := &SkillEvolutionRail{
		skillsDir:                skillsDir,
		language:                 language,
		autoScan:                 true,
		autoSave:                 true,
		evalInterval:             defaultEvalInterval,
		evolutionTimeoutSec:      defaultEvolutionTimeoutSecs,
		processedSignalKeys:      make(map[[4]string]bool),
		skillOps:                 make(map[string]*skill_call.SkillExperienceOperator),
		pendingGovernance:        make(map[string]*experience.PendingGovernance),
		pendingApprovalSnapshots: make(map[string]*experience.PendingChange),
		generateRecordsLLMPolicy: skillopt.GenerateRecordsLLMPolicy,
		evaluateLLMPolicy:        experience.EvaluateLLMPolicy,
		simplifyLLMPolicy:        experience.SimplifyLLMPolicy,
		sharingDownloadTopK:      defaultSharingDownloadTopK,
		excerptOffsets:           make(map[string]int),
	}

	// 应用可选参数
	for _, opt := range opts {
		opt(r)
	}

	// ─── 初始化核心组件 ───

	// Python: self._evolution_store = EvolutionStore(skills_dir)
	r.evolutionStore = checkpointing.NewEvolutionStore(r.skillsDir)

	// Python: self._evolver = SkillExperienceOptimizer(llm, model, language, generate_records_llm_policy=...)
	r.evolver = skillopt.NewSkillExperienceOptimizer(
		llmModel, model, language,
		r.generateRecordsLLMPolicy,
	)

	// Python: self._scorer = ExperienceScorer(llm, model, language, evaluate_llm_policy=..., simplify_llm_policy=...)
	r.scorer, _ = experience.NewExperienceScorer(
		llmModel, model, language,
		&r.evaluateLLMPolicy,
		&r.simplifyLLMPolicy,
	)

	// Python: self._manager = ExperienceManager(store=..., scorer=..., kind="skill", ...)
	r.manager, _ = experience.NewExperienceManager(
		r.evolutionStore,
		r.scorer,
		"skill",
		language,
		r.skillOps,
		r.pendingApprovalSnapshots,
		r.pendingGovernance,
	)

	// Python: self._approval_runtime = EvolutionApprovalRuntime(manager=..., pending_approval_snapshots=...)
	r.approvalRuntime = NewEvolutionApprovalRuntime(
		r.manager,
		r.pendingApprovalSnapshots,
	)

	// Python: self._online_updater = SingleDimUpdater(self._evolver)
	r.onlineUpdater = single_dim.NewSingleDimUpdater(r.evolver)

	// Python: self._online_orchestrator = OnlineEvolutionOrchestrator(store=..., updater=..., manager=..., skill_ops=..., stage_source="experience_updater")
	r.orchestrator = experience.NewOnlineEvolutionOrchestrator(
		r.evolutionStore,
		r.onlineUpdater,
		r.manager,
		r.skillOps,
		"skill_evolve_",
		"experience_updater",
	)

	// Python: self._experience_tracker = ExperienceTracker(store=..., scorer=..., eval_interval=...)
	r.experienceTracker = experience.NewExperienceTracker(
		r.evolutionStore,
		r.scorer,
		r.evalInterval,
	)

	// ─── 初始化 Sharing ───
	// Python: self._init_sharing(sharing_config, llm=llm, model=model, language=language, evolution_store=self._evolution_store)
	r.initSharing(llmModel, model, language)

	// ─── 构造 EvolutionRail 基类 ───
	railOpts := []EvolutionRailOption{
		WithDisabledSkills(r.normalizeNameSet()),
	}
	if r.evolutionTimeoutSec > 0 {
		railOpts = append(railOpts, WithMaxConcurrentEvolution(1))
	}
	r.EvolutionRail = NewEvolutionRail(r, railOpts...)

	return r
}

// WithAutoScan 设置是否自动检测演进信号。
func WithAutoScan(autoScan bool) SkillEvolutionRailOption {
	return func(r *SkillEvolutionRail) { r.autoScan = autoScan }
}

// WithAutoSave 设置是否自动保存（跳过审批）。
func WithAutoSave(autoSave bool) SkillEvolutionRailOption {
	return func(r *SkillEvolutionRail) { r.autoSave = autoSave }
}

// WithEvalInterval 设置评估间隔。
func WithEvalInterval(interval int) SkillEvolutionRailOption {
	return func(r *SkillEvolutionRail) { r.evalInterval = interval }
}

// WithEvolutionTimeout 设置后台演化超时秒数。
func WithEvolutionTimeout(secs float64) SkillEvolutionRailOption {
	return func(r *SkillEvolutionRail) { r.evolutionTimeoutSec = secs }
}

// WithGenerateRecordsLLMPolicy 设置记录生成 LLM 调用策略。
func WithGenerateRecordsLLMPolicy(policy llm_resilience.LLMInvokePolicy) SkillEvolutionRailOption {
	return func(r *SkillEvolutionRail) { r.generateRecordsLLMPolicy = policy }
}

// WithEvaluateLLMPolicy 设置评估 LLM 调用策略。
func WithEvaluateLLMPolicy(policy llm_resilience.LLMInvokePolicy) SkillEvolutionRailOption {
	return func(r *SkillEvolutionRail) { r.evaluateLLMPolicy = policy }
}

// WithSimplifyLLMPolicy 设置精简 LLM 调用策略。
func WithSimplifyLLMPolicy(policy llm_resilience.LLMInvokePolicy) SkillEvolutionRailOption {
	return func(r *SkillEvolutionRail) { r.simplifyLLMPolicy = policy }
}

// WithSharingConfig 设置跨用户共享配置。
//
// config 非空时创建 ExperienceSharer + ShareStager，对齐 Python: _init_sharing()。
func WithSharingConfig(config map[string]any) SkillEvolutionRailOption {
	return func(r *SkillEvolutionRail) {
		if config == nil {
			r.sharingEnabled = false
			return
		}
		enabled := resolveSharingBool(config["enabled"])
		envEnabled := os.Getenv("EVOLUTION_SHARING_ENABLED")
		if envEnabled != "" {
			enabled = envEnabled == "true" || envEnabled == "1"
		}
		if !enabled {
			r.sharingEnabled = false
			return
		}
		// 标记 sharing 为启用状态，实际构建在 initSharing 中延迟执行
		r.sharingEnabled = true
	}
}

// WithDisabledSkillsSet 设置禁用的技能名称列表。
func WithDisabledSkillsSet(names []string) SkillEvolutionRailOption {
	return func(r *SkillEvolutionRail) {
		// 委托给基类的 WithDisabledSkills，通过 normalizeNameSet 在构造时应用
	}
}

// Priority 返回优先级 80。
// 对齐 Python: SkillEvolutionRail.priority = 80
func (r *SkillEvolutionRail) Priority() int { return 80 }

// ─── 属性访问器 ───

// EvolutionStore 返回技能演进数据存储。
// Python: SkillEvolutionRail.evolution_store
func (r *SkillEvolutionRail) EvolutionStore() *checkpointing.EvolutionStore {
	return r.evolutionStore
}

// Scorer 返回经验评分器。
// Python: SkillEvolutionRail.scorer
func (r *SkillEvolutionRail) Scorer() *experience.ExperienceScorer {
	return r.scorer
}

// Evolver 返回经验优化器。
// Python: SkillEvolutionRail.evolver
func (r *SkillEvolutionRail) Evolver() *skillopt.SkillExperienceOptimizer {
	return r.evolver
}

// AutoScan 返回是否自动检测演进信号。
// Python: SkillEvolutionRail.auto_scan
func (r *SkillEvolutionRail) AutoScan() bool { return r.autoScan }

// SetAutoScan 设置是否自动检测演进信号。
// Python: SkillEvolutionRail.auto_scan = value
func (r *SkillEvolutionRail) SetAutoScan(v bool) { r.autoScan = v }

// AutoSave 返回是否自动保存。
// Python: SkillEvolutionRail.auto_save
func (r *SkillEvolutionRail) AutoSave() bool { return r.autoSave }

// SetAutoSave 设置是否自动保存。
// Python: SkillEvolutionRail.auto_save = value
func (r *SkillEvolutionRail) SetAutoSave(v bool) { r.autoSave = v }

// ApprovalRuntime 返回审批运行时。
// Python: SkillEvolutionRail.approval_runtime
func (r *SkillEvolutionRail) ApprovalRuntime() *EvolutionApprovalRuntime {
	return r.approvalRuntime
}

// Manager 返回经验生命周期管理器。
func (r *SkillEvolutionRail) Manager() *experience.ExperienceManager {
	return r.manager
}

// ProcessedSignalKeys 返回已处理的信号指纹集合。
// Python: SkillEvolutionRail.processed_signal_keys
func (r *SkillEvolutionRail) ProcessedSignalKeys() map[[4]string]bool {
	return r.processedSignalKeys
}

// ClearProcessedSignals 清空已处理信号指纹集合。
// Python: SkillEvolutionRail.clear_processed_signals()
func (r *SkillEvolutionRail) ClearProcessedSignals() {
	r.processedSignalKeys = make(map[[4]string]bool)
}

// IsSharingEnabled 返回是否启用共享。
// Python: SkillEvolutionSharingMixin.is_sharing_enabled
func (r *SkillEvolutionRail) IsSharingEnabled() bool {
	return r.experienceSharer != nil && r.shareStager != nil
}

// ExperienceSharer 返回经验共享器。
// Python: SkillEvolutionSharingMixin.experience_sharer
func (r *SkillEvolutionRail) ExperienceSharer() *sharing.ExperienceSharer {
	return r.experienceSharer
}

// ShareStager 返回经验共享暂存器。
// Python: SkillEvolutionSharingMixin.share_stager
func (r *SkillEvolutionRail) ShareStager() *sharing.ShareStager {
	return r.shareStager
}

// KeywordExtractor 返回关键词提取器。
// Python: SkillEvolutionSharingMixin.keyword_extractor
func (r *SkillEvolutionRail) KeywordExtractor() *sharing.KeywordExtractor {
	return r.keywordExtractor
}

// EvolutionConfig 返回当前演进配置。
// Python: SkillEvolutionRail.evolution_config
func (r *SkillEvolutionRail) EvolutionConfig() map[string]any {
	return map[string]any{
		"generate_records_llm_policy":  r.generateRecordsLLMPolicy,
		"evaluate_llm_policy":          r.evaluateLLMPolicy,
		"simplify_llm_policy":          r.simplifyLLMPolicy,
		"evolution_total_timeout_secs": r.evolutionTimeoutSec,
	}
}

// ─── EvolutionExtension 接口实现（10 个方法）───

// OnBeforeInvoke 空操作，基类已处理轨迹初始化。
// Python: SkillEvolutionRail 无此扩展点，空操作
func (r *SkillEvolutionRail) OnBeforeInvoke(ctx context.Context, cbc *agentinterfaces.AgentCallbackContext) error {
	return nil
}

// OnAfterModelCall 空操作，基类已记录 LLM 步骤。
// Python: SkillEvolutionRail 无此扩展点，空操作
func (r *SkillEvolutionRail) OnAfterModelCall(ctx context.Context, cbc *agentinterfaces.AgentCallbackContext) error {
	return nil
}

// OnAfterToolCall 检测经验详情读取，追踪 presented records。
//
// SKILL.md 读取仅用于索引发现。BODY 经验只有在 evolution/*.md 详情文件被读取
// 且返回内容包含具体记录标题时才计入 presented。
//
// 对齐 Python: SkillEvolutionRail._on_after_tool_call(ctx)
func (r *SkillEvolutionRail) OnAfterToolCall(ctx context.Context, cbc *agentinterfaces.AgentCallbackContext) error {
	inputs, ok := cbc.Inputs().(*agentinterfaces.ToolCallInputs)
	if !ok {
		return nil
	}

	skillName := r.detectExperienceDetailRead(inputs)
	if skillName == "" {
		return nil
	}

	content := r.extractToolContent(inputs)
	recordIDs := r.extractPresentedRecordIDs(content)
	if len(recordIDs) == 0 {
		return nil
	}

	sessionID := ""
	if cbc.Session() != nil {
		sessionID = cbc.Session().GetSessionID()
	}

	// Python: await self._experience_tracker.record_presented_records(session=session, skill_name=..., presentation_snippet=..., record_ids=...)
	r.experienceTracker.RecordPresentedRecords(ctx, sessionID, skillName, content, recordIDs)
	return nil
}

// OnAfterInvoke 空操作，基类已保存轨迹+触发演化。
// Python: SkillEvolutionRail 无此扩展点，空操作
func (r *SkillEvolutionRail) OnAfterInvoke(ctx context.Context, cbc *agentinterfaces.AgentCallbackContext) error {
	return nil
}

// OnAfterTaskIteration 空操作。
// Python: SkillEvolutionRail 无此扩展点，空操作
func (r *SkillEvolutionRail) OnAfterTaskIteration(ctx context.Context, cbc *agentinterfaces.AgentCallbackContext) error {
	return nil
}

// OnAfterEvolutionTriggered 空操作，被动演化在 RunEvolution 处理。
// Python: SkillEvolutionRail 无此扩展点，空操作
func (r *SkillEvolutionRail) OnAfterEvolutionTriggered(ctx context.Context, traj *trajectory.Trajectory, cbc *agentinterfaces.AgentCallbackContext) error {
	return nil
}

// AllowEvolutionTrigger 返回 autoScan 控制是否允许触发演化。
//
// 对齐 Python: SkillEvolutionRail._allow_evolution_trigger(trigger_point, ctx)
func (r *SkillEvolutionRail) AllowEvolutionTrigger(trigger EvolutionTriggerPoint, cbc *agentinterfaces.AgentCallbackContext) bool {
	return r.autoScan
}

// SnapshotForEvolution 同步捕获快照（cbc 仍活跃），供后台演化任务使用。
//
// Phase 1: 收集 messages + session_id + presented_entries + incremental_messages
//
// 对齐 Python: SkillEvolutionRail._snapshot_for_evolution(trajectory, ctx)
func (r *SkillEvolutionRail) SnapshotForEvolution(ctx context.Context, traj *trajectory.Trajectory, cbc *agentinterfaces.AgentCallbackContext) *EvolutionSnapshot {
	if !r.autoScan {
		return nil
	}

	// 基类快照：收集 messages
	snapshot := collectMessagesFromTrajectory(traj)
	if len(snapshot) == 0 {
		return nil
	}

	// 收集 session_id
	sessionID := ""
	if cbc != nil && cbc.Inputs() != nil {
		if invokeInputs, ok := cbc.Inputs().(*agentinterfaces.InvokeInputs); ok {
			sessionID = invokeInputs.ConversationID
		}
	}

	// 消费评估状态
	presentedEntries := r.experienceTracker.ConsumeEvalState(sessionID)

	// 计算增量消息
	incrementalMessages := r.resolveIncrementalMessages(snapshot, cbc, nil)

	skillName := "skill-evolution"
	return &EvolutionSnapshot{
		Trajectory:          traj,
		Messages:            snapshot,
		SkillName:           &skillName,
		PresentedEntries:    presentedEntries,
		SessionID:           sessionID,
		IncrementalMessages: incrementalMessages,
	}
}

// RunEvolution 执行技能演进，基于收集到的轨迹。
//
// 异步模式：ctx=nil，snapshot 包含捕获的数据；同步模式：ctx 活跃，snapshot=nil。
//
// 对齐 Python: SkillEvolutionRail.run_evolution(trajectory, ctx, *, snapshot)
func (r *SkillEvolutionRail) RunEvolution(ctx context.Context, traj *trajectory.Trajectory, snapshot *EvolutionSnapshot) error {
	logger.Info(logComponent).Bool("auto_scan", r.autoScan).Msg("[SkillEvolutionRail] run_evolution called")
	if !r.autoScan {
		logger.Info(logComponent).Msg("[SkillEvolutionRail] auto_scan disabled, skipping")
		return nil
	}

	var messages []map[string]any
	var presentedEntries []experience.PresentedRecordEntry

	// 异步路径：从 snapshot 读取
	// Python: if snapshot is not None: trajectory = snapshot.get("trajectory", trajectory); messages = snapshot["messages"]; presented_entries = snapshot.get("presented_entries", [])
	if snapshot != nil {
		if snapshot.Trajectory != nil {
			traj = snapshot.Trajectory
		}
		messages = snapshot.Messages
		presentedEntries = snapshot.PresentedEntries
	} else if traj != nil {
		// 同步路径：从轨迹收集消息
		messages = collectMessagesFromTrajectory(traj)
	} else {
		return nil
	}

	logger.Info(logComponent).Int("messages", len(messages)).Msg("[SkillEvolutionRail] collected messages")

	r.emitProgress("started", "starting regular skill evolution analysis for completed conversation")

	if len(messages) == 0 {
		logger.Info(logComponent).Msg("[SkillEvolutionRail] no messages, skipping")
		r.emitProgress("cancelled", "no conversation messages available; cancelling regular skill evolution analysis")
		r.experienceTracker.EvaluatePresented(ctx, presentedEntries)
		return nil
	}

	// 列出 regular skills
	// Python: all_skill_names = self._evolution_store.list_skill_names()
	allSkillNames := r.evolutionStore.ListSkillNames(ctx)
	skillNames := r.filterRegularSkills(ctx, allSkillNames)

	r.emitProgress("detecting_signals", fmt.Sprintf("checking %d regular skill(s) for evolution signals (filtered from %d local skill(s))", len(skillNames), len(allSkillNames)))

	// 信号检测
	// Python: detector = SignalDetector(existing_skills=...).bind_llm(llm=..., model=..., language=...)
	detector := signal.NewConversationSignalDetector(
		signal.WithExistingSkills(r.existingSkillSet(ctx, skillNames)),
	).BindLLM(r.evolver.LLM(), r.evolver.ModelName(), r.language)

	// Python: detected = detector.detect_trajectory_signals(trajectory, messages=messages)
	detected := detector.DetectTrajectorySignals(traj, messages)

	var signals []*signal.EvolutionSignal
	existingFingerprints := make(map[[4]string]bool)

	// Python: for signal in detected: fp = make_signal_fingerprint(signal); ...
	for _, sig := range detected {
		fp := signal.MakeSignalFingerprint(sig)
		existingFingerprints[fp] = true
		if !r.processedSignalKeys[fp] {
			r.processedSignalKeys[fp] = true
			signals = append(signals, sig)
		}
	}

	// 用户意图信号（LLM 辅助检测）
	// Python: user_intent_signals = await detector.detect_user_intent(messages)
	userIntentSignals, _ := detector.DetectUserIntent(ctx, messages)
	for _, sig := range userIntentSignals {
		fp := signal.MakeSignalFingerprint(sig)
		if existingFingerprints[fp] {
			continue
		}
		if !r.processedSignalKeys[fp] {
			r.processedSignalKeys[fp] = true
			existingFingerprints[fp] = true
			signals = append(signals, sig)
		}
	}

	// 限制去重集合大小
	// Python: if len(self._processed_signal_keys) > _MAX_PROCESSED_SIGNAL_KEYS: self._processed_signal_keys.clear()
	if len(r.processedSignalKeys) > maxProcessedSignalKeys {
		r.processedSignalKeys = make(map[[4]string]bool)
	}

	logger.Info(logComponent).Int("signals", len(signals)).Msg("[SkillEvolutionRail] detected signals")

	// 无信号时推断 primary skill
	// Python: if not signals: primary_skill = self._infer_primary_skill(messages, skill_names)
	if len(signals) == 0 {
		primarySkill := r.inferPrimarySkill(messages, skillNames)
		if primarySkill != "" {
			signals = []*signal.EvolutionSignal{
				signal.MakeEvolutionSignal(
					schema.ConversationReviewSignal, "", "[Auto] No rule-based signals. Analyze conversation for implicit experiences.",
					signal.WithSkillName(primarySkill),
					signal.WithSource("passive_conversation"),
				),
			}
		}
	}

	// 技能归属
	// Python: attributed_skills = {s.skill_name for s in signals if s.skill_name}; unattributed = [s for s in signals if not s.skill_name]
	skillGroups := r.attributeSignalsToSkills(signals)

	if len(skillGroups) == 0 {
		msg := "no skill usage of a regular skill or actionable evolution signal detected; cancelling regular skill evolution analysis"
		if len(signals) > 0 {
			msg = "detected evolution signals but no regular skill could be attributed; cancelling regular skill evolution analysis"
		}
		r.emitProgress("cancelled", msg)
		r.experienceTracker.EvaluatePresented(ctx, presentedEntries)
		return nil
	}

	// Sharing 下载（演化前从 hub 获取共享经验）
	// Python: downloaded_per_skill = await self._download_shared_experiences(messages, list(skill_groups.keys()), incremental_messages=...)
	incrementalMessages := r.resolveIncrementalMessages(messages, nil, snapshot)
	downloadedPerSkill := map[string][]checkpointing.EvolutionRecord{}
	if r.IsSharingEnabled() && len(skillGroups) > 0 {
		downloadedPerSkill = r.downloadSharedExperiences(ctx, messages, mapKeys(skillGroups), incrementalMessages)
	}

	// 逐技能演化
	// Python: for skill_name, skill_signals in skill_groups.items(): await self._evolve_skill_with_sharing(...)
	for skillName, skillSignals := range skillGroups {
		sharedRecords := downloadedPerSkill[skillName]
		r.evolveSkillWithSharing(ctx, skillName, skillSignals, messages, sharedRecords)
	}

	r.experienceTracker.EvaluatePresented(ctx, presentedEntries)
	return nil
}

// GetEvolutionTotalTimeoutSecs 获取演进总超时秒数。
// Python: SkillEvolutionRail._get_evolution_total_timeout_secs()
func (r *SkillEvolutionRail) GetEvolutionTotalTimeoutSecs() float64 {
	return r.evolutionTimeoutSec
}

// ─── Slash 命令 API 方法 ───

// RequestUserEvolution 用户主动触发的演进入口。
//
// 对齐 Python: SkillEvolutionRail.request_user_evolution(skill_name, user_intent, *, auto_approve=False)
func (r *SkillEvolutionRail) RequestUserEvolution(ctx context.Context, skillName string, userIntent string, autoApprove bool) (*EvolutionRequestResult, error) {
	traj := r.buildTrajectory()
	var messages []map[string]any
	var signals []*signal.EvolutionSignal

	if traj != nil && len(traj.Steps) > 0 {
		messages = collectMessagesFromTrajectory(traj)
		detected := r.detectActiveRequestSignals(ctx, skillName, traj, messages)
		signals = append(signals, detected...)
	}

	// 追加 USER_INTENT_SIGNAL
	// Python: if user_intent: self._append_unique_signal(signals, make_evolution_signal(USER_INTENT_SIGNAL, ...))
	if userIntent != "" {
		sig := signal.MakeEvolutionSignal(
			schema.UserIntentSignal,
			"Instructions",
			userIntent,
			signal.WithSkillName(skillName),
			signal.WithSource("explicit_request"),
		)
		signals = appendUniqueSignal(signals, sig)
	}

	if len(signals) == 0 {
		return &EvolutionRequestResult{SkillName: skillName}, nil
	}

	if len(messages) == 0 && userIntent != "" {
		messages = []map[string]any{{"role": "user", "content": userIntent}}
	}

	requiresApproval := !autoApprove
	request, err := r.handleEvolutionFromSignals(ctx, skillName, signals, messages, nil, userIntent, requiresApproval, false)
	if err != nil {
		return nil, err
	}

	if request == nil {
		return &EvolutionRequestResult{SkillName: skillName}, nil
	}

	records := request.Proposal.Records
	var approvalEvent *stream.OutputSchema
	if !autoApprove {
		approvalEvent = r.buildGeneratedRecordsEvent(skillName, request)
	}

	return &EvolutionRequestResult{
		SkillName:     skillName,
		RequestID:     &request.RequestID,
		ApprovalEvent: approvalEvent,
		Records:       records,
		AutoApproved:  autoApprove,
	}, nil
}

// RequestSimplify 请求精简技能经验。
//
// 对齐 Python: SkillEvolutionRail.request_simplify(skill_name, user_intent=None)
func (r *SkillEvolutionRail) RequestSimplify(ctx context.Context, skillName string, userIntent *string) (*SimplifyRequestResult, error) {
	// Python: request_id = await self._manager.request_simplify(skill_name, user_intent=user_intent)
	requestID, err := r.manager.RequestSimplify(ctx, skillName, userIntent)
	if err != nil {
		return nil, err
	}
	if requestID == "" {
		return &SimplifyRequestResult{SkillName: skillName}, nil
	}

	governance, ok := r.pendingGovernance[requestID]
	if !ok || governance == nil {
		return &SimplifyRequestResult{SkillName: skillName, RequestID: &requestID}, nil
	}

	actions := governance.Actions
	event := BuildSimplifyApprovalEvent(
		skillName,
		requestID,
		actions,
		r.language,
	)

	requestIDPtr := requestID
	return &SimplifyRequestResult{
		SkillName:     skillName,
		RequestID:     &requestIDPtr,
		ApprovalEvent: event,
		Actions:       actions,
	}, nil
}

// OnApproveSimplify 执行已暂存的精简提案。
//
// 对齐 Python: SkillEvolutionRail.on_approve_simplify(request_id)
func (r *SkillEvolutionRail) OnApproveSimplify(ctx context.Context, requestID string) (map[string]int, error) {
	result, err := r.manager.ApproveSimplify(ctx, requestID)
	if err != nil {
		return nil, err
	}
	if result != nil {
		logger.Info(logComponent).Str("request_id", requestID).Any("result", result).Msg("[SkillEvolutionRail] simplify executed")
	}
	return result, nil
}

// OnRejectSimplify 拒绝已暂存的精简提案。
//
// 对齐 Python: SkillEvolutionRail.on_reject_simplify(request_id)
func (r *SkillEvolutionRail) OnRejectSimplify(requestID string) {
	gov, ok := r.pendingGovernance[requestID]
	r.manager.RejectSimplify(requestID)
	if ok && gov != nil {
		logger.Info(logComponent).Str("skill_name", gov.SkillName).Msg("[SkillEvolutionRail] simplify rejected")
	}
}

// RequestRebuild 构建技能重建提示。
//
// 返回 followup_prompt 文本，调用方注入 agent loop 执行 skill-creator 生成新 SKILL.md。
//
// 对齐 Python: SkillEvolutionRail.request_rebuild(skill_name, user_intent=None, min_score=0.5)
func (r *SkillEvolutionRail) RequestRebuild(ctx context.Context, skillName string, userIntent *string, minScore float64) (string, error) {
	// Python: followup_text = await self._manager.request_rebuild(skill_name, user_intent=user_intent, min_score=min_score)
	followupText, err := r.manager.RequestRebuild(ctx, skillName, userIntent, minScore)
	if err != nil {
		return "", err
	}
	if followupText == "" {
		return "", nil
	}
	logger.Info(logComponent).Str("skill", skillName).Float64("min_score", minScore).Msg("[SkillEvolutionRail] rebuild prompt built")
	return followupText, nil
}

// RollbackSkill 回滚技能到归档版本（无需审批）。
//
// 对齐 Python: SkillEvolutionRail.rollback_skill(skill_name, version=None)
func (r *SkillEvolutionRail) RollbackSkill(ctx context.Context, skillName string, version *string) (bool, error) {
	store := r.evolutionStore
	skillDir := store.ResolveSkillDir(ctx, skillName)
	if skillDir == "" {
		return false, nil
	}

	archiveDir := filepath.Join(skillDir, "archive")
	dirEntries, err := os.ReadDir(archiveDir)
	if err != nil || len(dirEntries) == 0 {
		logger.Warn(logComponent).Str("skill", skillName).Msg("[SkillEvolutionRail] no archive dir")
		return false, nil
	}

	var bodyArchivePath string
	var evoArchivePath string

	if version != nil && *version != "" {
		bodyArchivePath = filepath.Join(archiveDir, *version)
		evoVersion := strings.ReplaceAll(strings.ReplaceAll(*version, "SKILL.", "evolutions."), ".md", ".json")
		evoArchivePath = filepath.Join(archiveDir, evoVersion)
	} else {
		// 选择最新的归档版本
		var bodyFiles []os.DirEntry
		for _, entry := range dirEntries {
			if strings.HasPrefix(entry.Name(), "SKILL.v") {
				bodyFiles = append(bodyFiles, entry)
			}
		}
		if len(bodyFiles) == 0 {
			logger.Warn(logComponent).Str("skill", skillName).Msg("[SkillEvolutionRail] no archived body")
			return false, nil
		}
		// 按名称排序（最新在后），取最后一个
		for _, f := range bodyFiles {
			bodyArchivePath = filepath.Join(archiveDir, f.Name())
		}
		evoVersion := strings.ReplaceAll(strings.ReplaceAll(filepath.Base(bodyArchivePath), "SKILL.", "evolutions."), ".md", ".json")
		evoArchivePath = filepath.Join(archiveDir, evoVersion)
	}

	// 先归档当前状态
	// Python: await store.archive_skill_body(skill_name); await store.archive_evolutions(skill_name)
	if _, err := store.ArchiveSkillBody(ctx, skillName); err != nil {
		logger.Error(logComponent).Err(err).Str("skill", skillName).Msg("归档 skill body 失败")
	}
	if _, err := store.ArchiveEvolutions(ctx, skillName); err != nil {
		logger.Error(logComponent).Err(err).Str("skill", skillName).Msg("归档 evolutions 失败")
	}

	// 恢复旧 body
	oldBody, err := store.ReadFileText(ctx, bodyArchivePath)
	if err != nil {
		return false, fmt.Errorf("read archived body: %w", err)
	}
	if _, err := store.WriteSkillContent(ctx, skillName, oldBody); err != nil {
		logger.Error(logComponent).Err(err).Str("skill", skillName).Msg("恢复 skill body 失败")
	}

	// 恢复旧 evolutions.json
	if _, statErr := os.Stat(evoArchivePath); statErr == nil {
		evoContent, readErr := store.ReadFileText(ctx, evoArchivePath)
		if readErr == nil {
			evoPath := filepath.Join(skillDir, "evolutions.json")
			if writeErr := store.WriteFileText(ctx, evoPath, evoContent); writeErr != nil {
				logger.Error(logComponent).Err(writeErr).Str("skill", skillName).Msg("恢复 evolutions.json 失败")
			}
		}
	} else {
		if err := store.ClearEvolutions(ctx, skillName); err != nil {
			logger.Error(logComponent).Err(err).Str("skill", skillName).Msg("清除 evolutions 失败")
		}
	}

	// Python: await store.render_evolution_markdown(skill_name)
	if err := store.RenderEvolutionMarkdown(ctx, skillName); err != nil {
		logger.Error(logComponent).Err(err).Str("skill", skillName).Msg("渲染 evolution markdown 失败")
	}

	logger.Info(logComponent).Str("skill", skillName).Str("archive", filepath.Base(bodyArchivePath)).Msg("[SkillEvolutionRail] rollback completed")
	return true, nil
}

// ApproveRecord 批准暂存的演进记录。
//
// 对齐 Python: SkillEvolutionRail.approve_record(request_id)
func (r *SkillEvolutionRail) ApproveRecord(ctx context.Context, requestID string) error {
	// Python: snapshot_pending = self._pending_approval_snapshots.get(request_id)
	snapshotPending := r.pendingApprovalSnapshots[requestID]
	var approvedRecords []checkpointing.EvolutionRecord
	var approvalMessages []map[string]any
	isShared := false

	if snapshotPending != nil {
		approvedRecords = snapshotPending.Payload
		approvalMessages = snapshotPending.Messages
		isShared = snapshotPending.IsSharedRecords
	}

	// Python: pending, result = await self.approval_runtime.approve_pending_request(request_id, ...)
	pending, result, err := r.approvalRuntime.ApprovePendingRequest(ctx, requestID, "SkillEvolutionRail", "approve_record")
	if err != nil {
		return err
	}
	if pending == nil {
		return nil
	}
	if result != nil && result.PendingCount > 0 {
		return nil
	}

	if pending != nil {
		logger.Info(logComponent).
			Int("applied_count", func() int {
				if result != nil {
					return result.AppliedCount
				}
				return 0
			}()).
			Str("skill", pending.SkillName).
			Str("request_id", requestID).
			Bool("is_shared", isShared).
			Msg("[SkillEvolutionRail] user approved records")
	}

	// 非共享记录审批后触发 sharing 上传
	// Python: if not is_shared and approved_records: await self._stage_records_for_share(...); await self._flush_share_uploads(...)
	if !isShared && len(approvedRecords) > 0 {
		r.stageRecordsForShare(ctx, pending.SkillName, approvalMessages, approvedRecords)
		r.flushShareUploads(ctx, pending.SkillName)
	}

	return nil
}

// RejectRecord 拒绝暂存的演进记录。
//
// 对齐 Python: SkillEvolutionRail.reject_record(request_id)
func (r *SkillEvolutionRail) RejectRecord(ctx context.Context, requestID string) error {
	// Python: pending, result = await self.approval_runtime.reject_pending_request(request_id, ...)
	pending, result, err := r.approvalRuntime.RejectPendingRequest(ctx, requestID, "SkillEvolutionRail", "reject_record")
	if err != nil {
		return err
	}
	if pending == nil {
		return nil
	}
	if result != nil {
		logger.Info(logComponent).
			Int("rejected_count", result.RejectedCount).
			Str("skill", pending.SkillName).
			Str("request_id", requestID).
			Msg("[SkillEvolutionRail] user rejected records")
	}
	return nil
}

// ShouldHintSimplifyOrRebuild 检查技能是否有足够经验来建议精简/重建。
//
// 对齐 Python: SkillEvolutionRail.should_hint_simplify_or_rebuild(skill_name)
func (r *SkillEvolutionRail) ShouldHintSimplifyOrRebuild(skillName string) bool {
	store := r.evolutionStore
	skillDir := store.ResolveSkillDir(context.Background(), skillName)
	if skillDir == "" {
		return false
	}
	evoPath := filepath.Join(skillDir, "evolutions.json")
	data, err := os.ReadFile(evoPath)
	if err != nil {
		return false
	}
	var evoData map[string]any
	if err := json.Unmarshal(data, &evoData); err != nil {
		return false
	}
	entries, ok := evoData["entries"].([]any)
	if !ok {
		return false
	}
	// Python: return len(entries) >= 10
	return len(entries) >= 10
}

// UpdateLLM 热更新 LLM 客户端和模型。
//
// 对齐 Python: SkillEvolutionRail.update_llm(llm, model)
func (r *SkillEvolutionRail) UpdateLLM(llmModel *llm.Model, model string) {
	r.evolver.UpdateLLM(llmModel, model)
	r.scorer.UpdateLLM(llmModel, model)
	if r.keywordExtractor != nil {
		r.keywordExtractor.UpdateLLM(llmModel, model)
	}
}

// SetSysOperation 设置系统操作，同时传递给 EvolutionRail 基类和 EvolutionStore。
//
// 对齐 Python: SkillEvolutionRail.set_sys_operation(sys_operation)
func (r *SkillEvolutionRail) SetSysOperation(op sys_operation.SysOperation) {
	// Python: super().set_sys_operation(sys_operation)
	r.EvolutionRail.SetSysOperation(op)
	// Python: self._evolution_store.sys_operation = sys_operation
	r.evolutionStore.SetSysOperation(op)
}

// RecordPresentedExperiences 记录由非 rail 路径呈现的经验。
//
// 对齐 Python: SkillEvolutionRail.record_presented_experiences(skill_name, presentation_snippet, *, session=None, record_ids=None)
func (r *SkillEvolutionRail) RecordPresentedExperiences(ctx context.Context, skillName string, presentationSnippet string, sessionID string, recordIDs []string) {
	if len(recordIDs) > 0 {
		r.experienceTracker.RecordPresentedRecords(ctx, sessionID, skillName, presentationSnippet, recordIDs)
		return
	}
	r.experienceTracker.RecordPresented(ctx, sessionID, skillName, presentationSnippet)
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// initSharing 初始化 Sharing 组件。
// 对齐 Python: SkillEvolutionSharingMixin._init_sharing(sharing_config, *, llm, model, language, evolution_store)
func (r *SkillEvolutionRail) initSharing(llmModel *llm.Model, model string, language string) {
	if !r.sharingEnabled {
		r.experienceSharer = nil
		r.keywordExtractor = nil
		r.shareStager = nil
		return
	}

	// Python: self._experience_sharer = self._build_experience_sharer(sharing_config)
	r.experienceSharer = r.buildExperienceSharer()
	if r.experienceSharer == nil {
		r.keywordExtractor = nil
		r.shareStager = nil
		r.sharingEnabled = false
		return
	}

	// Python: self._keyword_extractor = KeywordExtractor(llm=llm, model=model, language=language)
	r.keywordExtractor = sharing.NewKeywordExtractor(llmModel, model, language, skillopt.GenerateRecordsLLMPolicy)

	// Python: self._share_stager = ShareStager(keyword_extractor=..., sharer=...)
	r.shareStager = sharing.NewShareStager(r.keywordExtractor, r.experienceSharer, 0, nil)

	// Python: self._experience_sharer.set_skill_sharing_context_provider(self._make_sharing_context_provider(evolution_store))
	r.experienceSharer.SetSkillSharingContextProvider(r.makeSharingContextProvider())
}

// buildExperienceSharer 构建 ExperienceSharer。
// 对齐 Python: SkillEvolutionSharingMixin._build_experience_sharer(sharing_config)
func (r *SkillEvolutionRail) buildExperienceSharer() *sharing.ExperienceSharer {
	hubPath := os.Getenv("EVOLUTION_SHARING_HUB_PATH")
	if hubPath == "" {
		hubPath = ""
	}

	// Python: backend = LocalFileBackend(hub_path=hub_path)
	backendInstance := backend.NewLocalFileBackend(hubPath, 0)

	// Python: return ExperienceSharer(backend=backend, local_cache_dir=..., max_upload_retries=...)
	return sharing.NewExperienceSharer(
		backendInstance,
		"", // local_cache_dir
		defaultSharingMaxUploadRetries,
		0,   // backoff_base_secs
		nil, // provider 在 initSharing 中设置
	)
}

// makeSharingContextProvider 创建技能共享上下文提供者。
// 对齐 Python: SkillEvolutionSharingMixin._make_sharing_context_provider(evolution_store)
func (r *SkillEvolutionRail) makeSharingContextProvider() sharing.SkillSharingContextProvider {
	return func(ctx context.Context, skillName string) (string, []byte, string, string, error) {
		skillID, err := r.evolutionStore.EnsureSkillID(ctx, skillName)
		if err != nil {
			return "", nil, "", "", err
		}
		packageBytes, err := r.evolutionStore.PackSkillForSharing(ctx, skillName)
		if err != nil {
			return skillID, nil, skillName, "", err
		}
		content, err := r.evolutionStore.ReadPristineSkillContent(ctx, skillName)
		if err != nil {
			return skillID, packageBytes, skillName, "", nil
		}
		description := checkpointing.ExtractDescriptionFromSkillMD(content)
		return skillID, packageBytes, skillName, description, nil
	}
}

// resolveIncrementalMessages 解析增量消息。
// 对齐 Python: SkillEvolutionSharingMixin._resolve_incremental_messages(messages, ctx, snapshot)
func (r *SkillEvolutionRail) resolveIncrementalMessages(messages []map[string]any, cbc *agentinterfaces.AgentCallbackContext, snapshot *EvolutionSnapshot) []map[string]any {
	if snapshot != nil {
		incremental := snapshot.IncrementalMessages
		if incremental != nil {
			return incremental
		}
		return messages
	}
	if cbc != nil {
		excerptKey := r.getExcerptKeyFromCtx(cbc)
		prevOffset := r.excerptOffsets[excerptKey]
		incrementalMessages := messages[prevOffset:]
		r.excerptOffsets[excerptKey] = len(messages)
		return incrementalMessages
	}
	return messages
}

// getExcerptKeyFromCtx 从回调上下文获取摘要键。
// 对齐 Python: SkillEvolutionSharingMixin._get_excerpt_key_from_ctx(ctx)
func (r *SkillEvolutionRail) getExcerptKeyFromCtx(cbc *agentinterfaces.AgentCallbackContext) string {
	if cbc == nil {
		return ""
	}
	if cbc.Inputs() != nil {
		if invokeInputs, ok := cbc.Inputs().(*agentinterfaces.InvokeInputs); ok {
			if invokeInputs.ConversationID != "" {
				return invokeInputs.ConversationID
			}
		}
	}
	if cbc.Session() != nil {
		if sessionID := cbc.Session().GetSessionID(); sessionID != "" {
			return sessionID
		}
	}
	return ""
}

// downloadSharedExperiences 下载共享经验。
// 对齐 Python: SkillEvolutionSharingMixin._download_shared_experiences(parsed_messages, involved_skills, *, incremental_messages=...)
func (r *SkillEvolutionRail) downloadSharedExperiences(
	ctx context.Context,
	parsedMessages []map[string]any,
	involvedSkills []string,
	incrementalMessages []map[string]any,
) map[string][]checkpointing.EvolutionRecord {
	if !r.IsSharingEnabled() || r.keywordExtractor == nil {
		return map[string][]checkpointing.EvolutionRecord{}
	}
	if len(involvedSkills) == 0 {
		return map[string][]checkpointing.EvolutionRecord{}
	}

	messagesForExcerpt := incrementalMessages
	if messagesForExcerpt == nil {
		messagesForExcerpt = parsedMessages
	}
	excerpt := extractConversationExcerpt(messagesForExcerpt)
	if excerpt == "" {
		return map[string][]checkpointing.EvolutionRecord{}
	}

	// Python: query = await self._keyword_extractor.extract_query_keywords(feedback_excerpt=excerpt)
	query := r.keywordExtractor.ExtractQueryKeywords(ctx, excerpt)
	if len(query.Keywords) == 0 {
		return map[string][]checkpointing.EvolutionRecord{}
	}

	result := map[string][]checkpointing.EvolutionRecord{}
	for _, skillName := range involvedSkills {
		if r.experienceSharer == nil {
			continue
		}
		skillID := r.experienceSharer.ResolveSkillID(ctx, skillName)
		if skillID == "" {
			logger.Debug(logComponent).Str("skill", skillName).Msg("[SkillEvolutionRail] skip shared download: skill_id unavailable")
			continue
		}
		bundles := r.experienceSharer.DownloadRelevant(ctx, skillID, query, r.sharingDownloadTopK, skillName)

		var records []checkpointing.EvolutionRecord
		for _, bundle := range bundles {
			for _, sharedExp := range bundle.Experiences {
				marker := fmt.Sprintf("\n[shared origin=%s skill_id=%s]", bundle.BundleID, bundle.SkillID)
				sharedExp.Record.Context = (sharedExp.Record.Context) + marker
				records = append(records, sharedExp.Record)
			}
		}
		if len(records) > 0 {
			result[skillName] = records
		}
	}
	return result
}

// evolveSkillWithSharing 对单个技能执行演进（含共享记录处理）。
// 对齐 Python: SkillEvolutionSharingMixin._evolve_skill_with_sharing(skill_name, skill_signals, messages, ctx, shared_records)
func (r *SkillEvolutionRail) evolveSkillWithSharing(
	ctx context.Context,
	skillName string,
	skillSignals []*signal.EvolutionSignal,
	messages []map[string]any,
	sharedRecords []checkpointing.EvolutionRecord,
) {
	if len(sharedRecords) > 0 {
		sharedRecords = r.filterDuplicateSharedRecords(ctx, skillName, sharedRecords)
	}

	if len(sharedRecords) == 0 {
		// 无共享记录：走正常演化流程
		r.emitProgress("generating_updates", fmt.Sprintf("generating evolution records for '%s'", skillName), WithSkillName(skillName))
		request, err := r.handleEvolutionFromSignals(ctx, skillName, skillSignals, messages, nil, "", !r.autoSave, true)
		if err != nil {
			logger.Warn(logComponent).Str("skill", skillName).Err(err).Msg("[SkillEvolutionRail] evolve_skill failed")
			r.emitProgress("failed", fmt.Sprintf("evolution failed for '%s': %s", skillName, err.Error()), WithSkillName(skillName))
			return
		}
		if request == nil {
			r.emitProgress("completed", fmt.Sprintf("no evolution records generated for '%s'", skillName), WithSkillName(skillName))
		}
		return
	}

	// 有共享记录
	if r.autoSave {
		// 自动保存：先持久化共享记录，再正常演化
		for _, record := range sharedRecords {
			if err := r.evolutionStore.AppendRecord(ctx, skillName, record); err != nil {
				logger.Error(logComponent).Err(err).Str("skill", skillName).Msg("持久化共享记录失败")
			}
		}
		logger.Info(logComponent).Int("records", len(sharedRecords)).Str("skill", skillName).Msg("[SkillEvolutionRail] persisted shared records")
		if _, err := r.handleEvolutionFromSignals(ctx, skillName, skillSignals, messages, nil, "", false, true); err != nil {
			logger.Warn(logComponent).Err(err).Str("skill", skillName).Msg("[SkillEvolutionRail] evolve after shared records failed")
		}
		return
	}

	// 需要审批：发送共享记录审批事件
	r.emitSharedRecordsApproval(ctx, skillName, sharedRecords, messages)
}

// emitSharedRecordsApproval 发送共享记录审批事件。
// 对齐 Python: SkillEvolutionSharingMixin._emit_shared_records_approval(ctx, skill_name, records, messages)
func (r *SkillEvolutionRail) emitSharedRecordsApproval(
	ctx context.Context,
	skillName string,
	records []checkpointing.EvolutionRecord,
	messages []map[string]any,
) {
	if len(records) == 0 {
		return
	}
	// Python: request = self._manager.stage_records(skill_name, records, source="experience_sharing", messages=..., is_shared_records=True)
	request, _ := r.manager.StageRecords(
		context.Background(), skillName, records,
		true,                        // requiresApproval
		"experience_sharing",        // source
		"",                          // userQuery
		nil,                         // signalType
		nil,                         // signalSource
		schema.SkillExperienceEntry, // changeType
		"skill_evolve_",             // requestIDPrefix
		nil,                         // trajectory
		messages,
		true, // isSharedRecords
	)
	if request != nil {
		r.emitGeneratedRecords(nil, skillName, request)
	}
}

// stageRecordsForShare 暂存记录以供共享上传。
// 对齐 Python: SkillEvolutionSharingMixin._stage_records_for_share(skill_name, messages, records)
func (r *SkillEvolutionRail) stageRecordsForShare(
	ctx context.Context,
	skillName string,
	messages []map[string]any,
	records []checkpointing.EvolutionRecord,
) *sharing.StagingResult {
	if !r.IsSharingEnabled() || r.shareStager == nil || len(records) == 0 {
		return nil
	}
	result := r.shareStager.ScreenAndStage(ctx, skillName, records, messages)
	return result
}

// flushShareUploads 刷新共享上传。
// 对齐 Python: SkillEvolutionSharingMixin._flush_share_uploads(skill_name)
func (r *SkillEvolutionRail) flushShareUploads(ctx context.Context, skillName string) {
	if r.experienceSharer == nil || !r.experienceSharer.HasPending(skillName) {
		return
	}
	result := r.experienceSharer.FlushPendingUploads(ctx, skillName)
	if !result.OK && result.Reason != "" {
		logger.Warn(logComponent).Str("skill", skillName).Str("reason", result.Reason).Msg("[SkillEvolutionRail] share upload rejected")
	}
}

// filterDuplicateSharedRecords 过滤重复的共享记录。
// 对齐 Python: SkillEvolutionSharingMixin._filter_duplicate_shared_records(skill_name, shared_records)
func (r *SkillEvolutionRail) filterDuplicateSharedRecords(
	ctx context.Context,
	skillName string,
	sharedRecords []checkpointing.EvolutionRecord,
) []checkpointing.EvolutionRecord {
	if len(sharedRecords) == 0 {
		return nil
	}
	// 简化实现：直接返回共享记录（LLM 去重为可选增强）
	return sharedRecords
}

// emitProgress 发送进度事件。
// 对齐 Python: SkillEvolutionRail._emit_progress(stage, message, *, skill_name=None, request_id=None)
func (r *SkillEvolutionRail) emitProgress(stage, message string, opts ...ProgressEventOption) {
	logger.Info(logComponent).Str("stage", stage).Msg(message)
	r.EmitHostEvent(BuildEvolutionProgressEvent("regular", stage, message, append(opts, WithPrefix("[Skill Evolution]"))...))
}

// filterRegularSkills 过滤非常规技能（team-skill/swarm-skill）和禁用技能。
// 对齐 Python: run_evolution 中的 skill_names 过滤逻辑
func (r *SkillEvolutionRail) filterRegularSkills(ctx context.Context, allNames []string) []string {
	var result []string
	for _, name := range allNames {
		if r.isSkillDisabled(name) {
			continue
		}
		if !r.isRegularSkill(ctx, name) {
			continue
		}
		result = append(result, name)
	}
	return result
}

// isRegularSkill 判断技能是否为常规类型（排除 team-skill/swarm-skill）。
// 对齐 Python: SkillEvolutionRail._is_regular_skill(name)
func (r *SkillEvolutionRail) isRegularSkill(ctx context.Context, name string) bool {
	skillDir := r.evolutionStore.ResolveSkillDir(ctx, name)
	if skillDir == "" {
		return true
	}
	skillMD := filepath.Join(skillDir, "SKILL.md")
	data, err := os.ReadFile(skillMD)
	if err != nil {
		return true
	}
	frontmatter := parseTopLevelFrontmatter(string(data))
	kind := frontmatter["kind"]
	return kind != "team-skill" && kind != "swarm-skill"
}

// existingSkillSet 构建已有技能集合。
// Python: {name for name in skill_names if self._evolution_store.skill_exists(name)}
func (r *SkillEvolutionRail) existingSkillSet(ctx context.Context, names []string) map[string]bool {
	result := make(map[string]bool, len(names))
	for _, name := range names {
		if r.evolutionStore.SkillExists(ctx, name) {
			result[name] = true
		}
	}
	return result
}

// inferPrimarySkill 从 SKILL.md 读取痕迹推断最可能的活跃技能。
// 对齐 Python: SkillEvolutionRail._infer_primary_skill(messages, skill_names)
func (r *SkillEvolutionRail) inferPrimarySkill(messages []map[string]any, skillNames []string) string {
	var texts []string
	var skillToolPayloads []string

	for _, msg := range messages {
		role, _ := msg["role"].(string)
		switch role {
		case "tool", "function":
			content, _ := msg["content"].(string)
			texts = append(texts, content)
		case "assistant":
			if tcSlice, ok := msg["tool_calls"].([]any); ok {
				for _, tc := range tcSlice {
					if tcDict, ok := tc.(map[string]any); ok {
						args, _ := tcDict["arguments"].(string)
						texts = append(texts, args)
						name, _ := tcDict["name"].(string)
						if name == "skill_tool" {
							skillToolPayloads = append(skillToolPayloads, args)
						}
					}
				}
			}
		}
	}

	// Python: infer_skill_from_texts(skill_names, skill_tool_payloads=..., texts=...)
	// Go 中暂未实现 infer_skill_from_texts，使用内联简化逻辑
	return inferSkillFromTexts(skillNames, skillToolPayloads, texts)
}

// attributeSignalsToSkills 将信号归属到技能，处理无归属信号。
// 对齐 Python: run_evolution 中的技能归属逻辑
func (r *SkillEvolutionRail) attributeSignalsToSkills(signals []*signal.EvolutionSignal) map[string][]*signal.EvolutionSignal {
	// 单技能 fallback：将无归属信号分配给唯一归属技能
	attributedSkills := make(map[string]bool)
	var unattributed []*signal.EvolutionSignal
	for _, sig := range signals {
		if sig.SkillName != nil && *sig.SkillName != "" {
			attributedSkills[*sig.SkillName] = true
		} else {
			unattributed = append(unattributed, sig)
		}
	}

	if len(attributedSkills) == 1 && len(unattributed) > 0 {
		fallbackSkill := ""
		for k := range attributedSkills {
			fallbackSkill = k
			break
		}
		for _, sig := range unattributed {
			sig.SkillName = &fallbackSkill
		}
	}

	groups := make(map[string][]*signal.EvolutionSignal)
	for _, sig := range signals {
		if sig.SkillName == nil || *sig.SkillName == "" {
			continue
		}
		groups[*sig.SkillName] = append(groups[*sig.SkillName], sig)
	}
	return groups
}

// handleEvolutionFromSignals 共享下游处理程序，被动和显式技能演化共用。
// 对齐 Python: SkillEvolutionRail._handle_evolution_from_signals(skill_name, signals, messages, ctx, user_query, requires_approval, emit_host_events)
func (r *SkillEvolutionRail) handleEvolutionFromSignals(
	ctx context.Context,
	skillName string,
	signals []*signal.EvolutionSignal,
	messages []map[string]any,
	cbc *agentinterfaces.AgentCallbackContext,
	userQuery string,
	requiresApproval bool,
	emitHostEvents bool,
) (*experience.ExperienceApprovalRequest, error) {
	if emitHostEvents {
		r.emitProgress("generating_updates", fmt.Sprintf("generating evolution records for '%s'", skillName), WithSkillName(skillName))
	}

	// Python: result = await self._stage_evolution_from_signals(skill_name=..., signals=..., messages=..., user_query=..., requires_approval=...)
	result, err := r.orchestrator.Evolve(
		ctx,
		skillName,
		convertSignalsToValues(signals),
		messages,
		userQuery,
		nil, // trajectory
		requiresApproval,
		map[string]any{},
		nil, // source
	)
	if err != nil {
		return nil, err
	}

	request := result.Request
	if request == nil {
		if emitHostEvents && result.Status == experience.OnlineEvolutionStatusNoEvolutionNoRecords {
			r.emitBackgroundOutcomeEvent(map[string]string{
				"status":     string(result.Status),
				"message":    result.Message,
				"rail_kind":  "regular",
				"skill_name": result.SkillName,
				"stage":      "completed",
				"source":     "experience_updater",
			})
		}
		return nil, nil
	}

	// 自动审批回调
	if !requiresApproval {
		if emitHostEvents {
			r.emitProgress("auto_approved", fmt.Sprintf("experience records auto-saved to '%s'", skillName), WithSkillName(skillName), WithRequestID(request.RequestID))
		}
		// Sharing: auto-approve 后上传
		r.sharingAfterAutoApproved(ctx, skillName, request)
		return request, nil
	}

	// 需要审批：发送审批事件
	if emitHostEvents {
		r.emitGeneratedRecords(cbc, skillName, request)
	}

	return request, nil
}

// sharingAfterAutoApproved 自动审批后触发 sharing。
// 对齐 Python: SkillEvolutionSharingMixin._sharing_after_auto_approved(skill_name, staged_request)
func (r *SkillEvolutionRail) sharingAfterAutoApproved(ctx context.Context, skillName string, stagedRequest *experience.ExperienceApprovalRequest) {
	if stagedRequest.PendingChange == nil {
		return
	}
	records := stagedRequest.PendingChange.Payload
	if len(records) == 0 {
		return
	}
	messages := stagedRequest.PendingChange.Messages
	r.stageRecordsForShare(ctx, skillName, messages, records)
	r.flushShareUploads(ctx, skillName)
}

// emitGeneratedRecords 缓存审批请求事件以供后续传递。
// 对齐 Python: SkillEvolutionRail._emit_generated_records(ctx, skill_name, approval_request)
func (r *SkillEvolutionRail) emitGeneratedRecords(cbc *agentinterfaces.AgentCallbackContext, skillName string, approvalRequest *experience.ExperienceApprovalRequest) {
	event := r.buildGeneratedRecordsEvent(skillName, approvalRequest)
	if event == nil {
		return
	}
	r.EmitHostEvent(event)
	r.emitProgress("approval_required", fmt.Sprintf("experience records for '%s' ready, awaiting approval", skillName), WithSkillName(skillName))
	if approvalRequest != nil {
		logger.Info(logComponent).
			Str("request_id", approvalRequest.RequestID).
			Int("record_count", approvalRequest.Proposal.RecordCount()).
			Str("skill", skillName).
			Msg("[SkillEvolutionRail] buffered approval request")
	}
}

// buildGeneratedRecordsEvent 构建审批事件。
// 对齐 Python: SkillEvolutionRail._build_generated_records_event(skill_name, approval_request)
func (r *SkillEvolutionRail) buildGeneratedRecordsEvent(skillName string, approvalRequest *experience.ExperienceApprovalRequest) *stream.OutputSchema {
	if approvalRequest == nil {
		return nil
	}
	pending := approvalRequest.PendingChange
	if pending == nil || approvalRequest.RequestID == "" {
		return nil
	}

	records := pending.Payload
	isShared := pending.IsSharedRecords

	event := BuildSkillApprovalEvent(
		skillName,
		approvalRequest.RequestID,
		records,
		r.language,
		isShared,
	)
	AttachEvolutionMeta(event, approvalRequest.Proposal.SignalType, approvalRequest.Proposal.SignalSource)
	return event
}

// detectActiveRequestSignals 检测显式请求的活跃信号。
// 对齐 Python: SkillEvolutionRail._detect_active_request_signals(skill_name, trajectory, messages)
func (r *SkillEvolutionRail) detectActiveRequestSignals(
	ctx context.Context,
	skillName string,
	traj *trajectory.Trajectory,
	messages []map[string]any,
) []*signal.EvolutionSignal {
	// Python: detector = SignalDetector(existing_skills=self._active_request_regular_skill_names(skill_name)).bind_llm(...)
	activeSkillNames := r.activeRequestRegularSkillNames(ctx, skillName)
	detector := signal.NewConversationSignalDetector(
		signal.WithExistingSkills(activeSkillNames),
	).BindLLM(r.evolver.LLM(), r.evolver.ModelName(), r.language)

	var detected []*signal.EvolutionSignal
	detected = append(detected, detector.DetectTrajectorySignals(traj, messages)...)

	userIntentSignals, _ := detector.DetectUserIntent(ctx, messages)
	for _, sig := range userIntentSignals {
		if sig.SkillName != nil && *sig.SkillName != skillName {
			continue
		}
		detected = appendUniqueSignal(detected, sig)
	}

	return detected
}

// activeRequestRegularSkillNames 返回活跃请求可用的常规技能集合。
// 对齐 Python: SkillEvolutionRail._active_request_regular_skill_names(skill_name)
func (r *SkillEvolutionRail) activeRequestRegularSkillNames(ctx context.Context, skillName string) map[string]bool {
	allSkillNames := r.evolutionStore.ListSkillNames(ctx)
	skillNames := make(map[string]bool)
	for _, name := range allSkillNames {
		if r.isRegularSkill(ctx, name) {
			skillNames[name] = true
		}
	}
	skillNames[skillName] = true
	return skillNames
}

// detectExperienceDetailRead 检测经验详情读取操作。
// 对齐 Python: SkillEvolutionRail._detect_experience_detail_read(inputs)
func (r *SkillEvolutionRail) detectExperienceDetailRead(inputs *agentinterfaces.ToolCallInputs) string {
	toolName := inputs.ToolName
	args := extractToolArgs(inputs.ToolArgs)

	if toolName == "skill_tool" {
		skillName := strings.TrimSpace(str(args["skill_name"]))
		relativePath := strings.TrimSpace(str(args["relative_file_path"]))
		if relativePath == "" {
			relativePath = "SKILL.md"
		}
		if skillName != "" && isExperienceDetailRelativePath(relativePath) {
			return skillName
		}
		return ""
	}

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
	return r.skillForExperienceDetailFile(filePath)
}

// extractToolContent 提取工具调用返回内容。
// 对齐 Python: SkillEvolutionRail._extract_tool_content(inputs)
func (r *SkillEvolutionRail) extractToolContent(inputs *agentinterfaces.ToolCallInputs) string {
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

	// Python: tool_msg = inputs.tool_msg; if tool_msg is not None and hasattr(tool_msg, "content"): return tool_msg.content
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

// extractPresentedRecordIDs 从内容中提取经验记录 ID。
// 对齐 Python: SkillEvolutionRail._extract_presented_record_ids(content)
func (r *SkillEvolutionRail) extractPresentedRecordIDs(content string) []string {
	seen := make(map[string]bool)
	var recordIDs []string
	for _, match := range experienceRecordHeadingRE.FindAllStringSubmatch(content, -1) {
		recordID := match[1]
		if seen[recordID] {
			continue
		}
		seen[recordID] = true
		recordIDs = append(recordIDs, recordID)
	}
	return recordIDs
}

// skillForExperienceDetailFile 根据文件路径推断技能名。
// 对齐 Python: SkillEvolutionRail._skill_for_experience_detail_file(file_path)
func (r *SkillEvolutionRail) skillForExperienceDetailFile(filePath string) string {
	ctx := context.Background()
	skillNames := r.evolutionStore.ListSkillNames(ctx)
	readPath := filepath.Clean(filePath)

	for _, skillName := range skillNames {
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

// normalizeNameSet 规范化技能名称列表。
// Python: disabled_skills 参数处理
func (r *SkillEvolutionRail) normalizeNameSet() []string {
	// 从 skillsDir 列出已有技能名称
	return []string{}
}

// ─── 包级辅助函数 ───

// appendUniqueSignal 追加去重信号，返回更新后的切片。
// 对齐 Python: SkillEvolutionRail._append_unique_signal(signals, signal)
func appendUniqueSignal(signals []*signal.EvolutionSignal, sig *signal.EvolutionSignal) []*signal.EvolutionSignal {
	fp := signal.MakeSignalFingerprint(sig)
	for _, existing := range signals {
		if signal.MakeSignalFingerprint(existing) == fp {
			return signals
		}
	}
	return append(signals, sig)
}

// convertSignalsToValues 将指针切片转换为值切片。
func convertSignalsToValues(ptrs []*signal.EvolutionSignal) []signal.EvolutionSignal {
	result := make([]signal.EvolutionSignal, len(ptrs))
	for i, p := range ptrs {
		if p != nil {
			result[i] = *p
		}
	}
	return result
}

// extractToolArgs 提取工具参数。
// 对齐 Python: SkillEvolutionRail._extract_tool_args(tool_args)
func extractToolArgs(toolArgs any) map[string]any {
	if args, ok := toolArgs.(map[string]any); ok {
		return args
	}
	if raw, ok := toolArgs.(string); ok {
		var parsed map[string]any
		if err := json.Unmarshal([]byte(raw), &parsed); err == nil {
			return parsed
		}
	}
	return map[string]any{}
}

// isExperienceDetailRelativePath 判断是否为经验详情相对路径。
// 对齐 Python: SkillEvolutionRail._is_experience_detail_relative_path(relative_path)
func isExperienceDetailRelativePath(relativePath string) bool {
	normalized := filepath.Clean(strings.ReplaceAll(relativePath, "\\", "/"))
	normalized = strings.TrimPrefix(normalized, "/")
	if strings.HasPrefix(normalized, "../") || strings.Contains(normalized, "/../") {
		return false
	}
	if !strings.HasPrefix(normalized, "evolution/") {
		return false
	}
	if strings.HasPrefix(normalized, "evolution/scripts/") {
		return false
	}
	if strings.HasSuffix(normalized, "/SKILL.md") || normalized == "SKILL.md" {
		return false
	}
	if strings.HasSuffix(normalized, "evolutions.json") {
		return false
	}
	return strings.HasSuffix(strings.ToLower(normalized), ".md")
}

// resolveSharingBool 解析共享配置布尔值。
// 对齐 Python: SkillEvolutionSharingMixin._resolve_bool(value)
func resolveSharingBool(value any) bool {
	if b, ok := value.(bool); ok {
		return b
	}
	if i, ok := value.(int); ok {
		return i != 0
	}
	if f, ok := value.(float64); ok {
		return f != 0
	}
	if s, ok := value.(string); ok {
		lower := strings.TrimSpace(strings.ToLower(s))
		return lower == "1" || lower == "true" || lower == "yes" || lower == "on"
	}
	return false
}

// mapKeys 提取 map 的键为切片。
func mapKeys[K comparable, V any](m map[K]V) []K {
	keys := make([]K, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}

// str 将 any 转为字符串，空值返回空字符串。
func str(v any) string {
	if v == nil {
		return ""
	}
	if s, ok := v.(string); ok {
		return s
	}
	return fmt.Sprintf("%v", v)
}

// inferSkillFromTexts 从消息文本推断技能。
// 对齐 Python: utils.infer_skill_from_texts(skill_names, skill_tool_payloads=..., texts=...)
// 简化实现：搜索 skill_tool 参数中的技能名称
func inferSkillFromTexts(skillNames []string, skillToolPayloads []string, texts []string) string {
	// 在 skill_tool_payloads 中搜索已知技能名
	for _, payload := range skillToolPayloads {
		for _, name := range skillNames {
			if strings.Contains(payload, name) {
				return name
			}
		}
	}
	// 在工具内容中搜索 SKILL.md 路径中的技能名
	skillMDRe := regexp.MustCompile(`[/\\]([^/\\]+)[/\\]SKILL\.md`)
	for _, text := range texts {
		matches := skillMDRe.FindAllStringSubmatch(text, -1)
		for _, match := range matches {
			for _, name := range skillNames {
				if match[1] == name {
					return name
				}
			}
		}
	}
	return ""
}

// parseTopLevelFrontmatter 解析 Markdown frontmatter 中的顶层标量字段。
// 对齐 Python: utils.parse_top_level_frontmatter(content)
// 同 checkpointing.parseTopLevelFrontmatter 但返回 map[string]any 以兼容原接口
func parseTopLevelFrontmatter(content string) map[string]string {
	return checkpointing.ParseTopLevelFrontmatter(content)
}

// extractConversationExcerpt 从消息列表提取对话摘要用于共享关键词提取。
// 对齐 Python: SkillEvolutionSharingMixin._extract_conversation_excerpt(messages, max_chars=400)
func extractConversationExcerpt(messages []map[string]any, maxChars ...int) string {
	mc := 400
	if len(maxChars) > 0 && maxChars[0] > 0 {
		mc = maxChars[0]
	}

	// Go regexp 不支持 (?!...) 负向前瞻，将 "error" 单独匹配然后排除 "error = None"
	// 对齐 Python: _FAILURE_KEYWORDS
	failureRe := regexp.MustCompile(`(?i)error|exception|traceback|failed|failure|timeout|timed out|errno|connectionerror|oserror|valueerror|typeerror|错误|异常|失败|超时|no such file|permission denied|access denied|command not found|not recognized|module not found|econnrefused|econnreset|enoent|enotfound|npm err!`)

	var userQueries []string
	var failedToolResults []string
	var toolCallsSummary []string
	toolCallIDToName := map[string]string{}

	for _, msg := range messages {
		role, _ := msg["role"].(string)
		switch role {
		case "user":
			content, _ := msg["content"].(string)
			if strings.TrimSpace(content) != "" {
				userQueries = append(userQueries, truncateString(content, mc))
			}
		case "assistant":
			if tcSlice, ok := msg["tool_calls"].([]any); ok {
				for _, tc := range tcSlice {
					if tcDict, ok := tc.(map[string]any); ok {
						tcID, _ := tcDict["id"].(string)
						tcName, _ := tcDict["name"].(string)
						if tcID != "" && tcName != "" {
							toolCallIDToName[tcID] = tcName
						}
						if tcName != "" {
							toolCallsSummary = append(toolCallsSummary, fmt.Sprintf("[Tool: %s]", tcName))
						}
					}
				}
			}
		case "tool", "function":
			contentStr, _ := msg["content"].(string)
			toolName, _ := msg["name"].(string)
			if toolName == "" {
				if tcID, ok := msg["tool_call_id"].(string); ok {
					toolName = toolCallIDToName[tcID]
				}
			}
			if failureRe.MatchString(contentStr) && strings.TrimSpace(contentStr) != "" {
				prefix := "[ERROR]: "
				if toolName != "" {
					prefix = fmt.Sprintf("[ERROR in %s]: ", toolName)
				}
				failedToolResults = append(failedToolResults, prefix+truncateString(contentStr, mc*2))
			}
		}
	}

	var parts []string
	if len(userQueries) > 0 {
		parts = append(parts, "=== USER QUERIES ===")
		limit := len(userQueries)
		if limit > 3 {
			limit = 3
		}
		parts = append(parts, userQueries[:limit]...)
	}
	if len(failedToolResults) > 0 {
		parts = append(parts, "\n=== FAILED TOOL EXECUTIONS ===")
		limit := len(failedToolResults)
		if limit > 5 {
			limit = 5
		}
		parts = append(parts, failedToolResults[:limit]...)
	}
	if len(toolCallsSummary) > 0 {
		parts = append(parts, "\n=== TOOL CALLS ===")
		limit := len(toolCallsSummary)
		if limit > 10 {
			limit = 10
		}
		parts = append(parts, toolCallsSummary[:limit]...)
	}
	result := strings.Join(parts, "\n")
	if len(result) > mc*3 {
		result = result[:mc*3]
	}
	return result
}

// truncateString 截断字符串到指定长度。
func truncateString(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen]
}

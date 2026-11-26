package evolution

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	agentevolving "github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_evolving"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_evolving/checkpointing"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_evolving/experience"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_evolving/optimizer/llm_resilience"
	skillopt "github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_evolving/optimizer/skill_call"
	sections "github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_evolving/prompts/sections"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_evolving/schema"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_evolving/sharing"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_evolving/sharing/backend"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_evolving/signal"
	agenttools "github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_evolving/tools"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_evolving/trajectory"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_evolving/updater/single_dim"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/foundation/llm"
	toolpkg "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/foundation/tool"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/operator/skill_call"
	cb "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/runner/callback"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/session/stream"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/single_agent/interfaces"
	saprompt "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/single_agent/prompts"
	saprompts "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/single_agent/prompts"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/sys_operation"
	harnessinterfaces "github.com/uapclaw/uapclaw-go/internal/openuapclaw/harness/interfaces"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/harness/rails/evolution/review"
	reviewsubagent "github.com/uapclaw/uapclaw-go/internal/openuapclaw/harness/rails/evolution/reviewsubagent"
	harnessschema "github.com/uapclaw/uapclaw-go/internal/openuapclaw/harness/schema"
)

// ──────────────────────────── 结构体 ────────────────────────────

// SkillEvolutionRail 单 Agent 技能演进护栏，在 after_invoke 后自动触发演进。
// 嵌入 EvolutionRail 基类获得自动轨迹收集和异步触发能力，
// 通过实现 EvolutionExtension 接口的 10 个方法完成子类多态分派。
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

	// autoSave 是否自动保存（跳过审批）
	autoSave bool
	// disabledSkills 禁用的技能名称列表
	disabledSkills []string
	// language 语言（"cn" 或 "en"）
	language string
	// modelName 当前 LLM 模型名（构造与 UpdateLLM 时同步），供排障与断言
	modelName string
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
	// sharingConfig 共享配置（延迟使用）
	sharingConfig map[string]any
	// excerptOffsets 增量消息偏移量
	excerptOffsets map[string]int

	// ─── 共享状态 ───

	// skillOps 技能经验操作器映射
	skillOps map[string]*skill_call.SkillExperienceOperator
	// pendingGovernance 暂存治理操作映射
	pendingGovernance map[string]*experience.PendingGovernance
	// pendingApprovalSnapshots 暂存审批快照映射
	pendingApprovalSnapshots map[string]*experience.PendingChange
	// submissionAdapter 审批提交服务适配器（懒构造，身份稳定）
	submissionAdapter EvolutionSubmissionService
	// skillsDir 技能目录
	skillsDir []string
	// reviewRuntime 审查运行时（configure 注入，供活动演化工具共享）
	// Python: self._review_runtime
	reviewRuntime *review.EvolutionReviewRuntime
	// trajectorySpanProcessor 共享观测处理器（configure 注入；Go 消费面
	// 仍为 builder 游标增量 D-25.2b-11，字段为配置一致性校验保留）
	// Python: self._trajectory_span_processor
	trajectorySpanProcessor *trajectory.TrajectorySpanProcessor

	// ─── 15.24a Task 10 新版（08-13）字段 ───

	// signalTrigger 是否触发确定性信号驱动演化
	// Python: self._signal_trigger（__init__ :242）
	signalTrigger bool
	// twoStage 经验生成是否使用 analyzer+formatter 双段管线
	// Python: self._two_stage（:251）
	twoStage bool
	// reviewTrigger 是否周期性排入审查自检 follow-up
	// Python: self._review_trigger（:262）
	reviewTrigger bool
	// reviewInterval 非跟进任务迭代数间隔（两次审查自检之间）
	// Python: self._review_interval
	reviewInterval int
	// reviewNonFollowupCount 非跟进迭代计数
	// Python: self._review_non_followup_count
	reviewNonFollowupCount int
	// reviewAgentMaxIterations 审查子 agent 最大迭代数
	// Python: self._review_agent_max_iterations
	reviewAgentMaxIterations int
	// skipSignalTriggerThisInvoke 本 invoke 已有活动演化活动时跳过被动信号扫描
	// Python: self._skip_signal_trigger_this_invoke（:258）
	skipSignalTriggerThisInvoke bool
	// subjectKindValue 活动审查主体类别（"skill" / "swarm-skill"）
	// Python: self._subject_kind_value
	subjectKindValue string
	// evolutionTools rail 注册的演化工具（uninit 注销用；注册面 ⤴️15.24b）
	// Python: self._evolution_tools
	evolutionTools []toolpkg.Tool
	// agentRef 绑定的 agent（init 时设置）
	// Python: self._agent
	agentRef agentinterfaces.BaseAgent
	// systemPromptBuilder 系统提示词构建器（init 时抓取）
	// Python: self._system_prompt_builder
	systemPromptBuilder saprompts.SystemPromptBuilderInterface
}

// SkillEvolutionRailOption SkillEvolutionRail 构造选项函数。
type SkillEvolutionRailOption func(*SkillEvolutionRail)

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
	// sharedRecordContextMarker 共享记录上下文标记前缀。
	// 对齐 Python: _SHARED_RECORD_CONTEXT_MARKER = "[shared origin="
	sharedRecordContextMarker = "[shared origin="
	// evalSnippetMaxMessages 评估片段最大消息数
	// Python: _EVAL_SNIPPET_MAX_MESSAGES = 20
	evalSnippetMaxMessages = 20
	// evalSnippetPostPresentMaxChars 评估片段单消息最大字符数
	// Python: _EVAL_SNIPPET_POST_PRESENT_MAX_CHARS = 800
	evalSnippetPostPresentMaxChars = 800
)

// nonRegularSkillKinds 非常规技能 kind 闭集（排除出 1D 演进检测）。
// Python: _NON_REGULAR_SKILL_KINDS = {"team-skill", "swarm-skill"}
var nonRegularSkillKinds = map[string]bool{"team-skill": true, "swarm-skill": true}

// autoSkillEvolutionFollowUpTag 审查自检 follow-up 包裹标签。
// Python: _AUTO_SKILL_EVOLUTION_FOLLOW_UP_TAG（:92）
const autoSkillEvolutionFollowUpTag = "auto_skill_evolution_review_followup"

// reviewFollowupPromptCN 审查自检 follow-up 中文提示词（原文复刻，不翻译）。
// Python: _REVIEW_FOLLOWUP_PROMPT_CN（:93-98）
const reviewFollowupPromptCN = "这是运行时插入的 Skill 演进自检，不是用户的新需求。\n" +
	"参考常驻“技能演进自检”规则，只判断本轮是否存在可复用更新，不重新判断运行时触发门槛。\n" +
	"如需建议，只在普通最终回复末尾追加一至两句，并同时包含可复用更新点和是否发起 Skill 演进的确认问题；" +
	"否则自然回复，不提本提醒或内部判断。"

// reviewFollowupPromptEN 审查自检 follow-up 英文提示词（原文复刻，不翻译）。
// Python: _REVIEW_FOLLOWUP_PROMPT_EN（:99-104）
const reviewFollowupPromptEN = "This runtime-inserted Skill evolution self-check is not a new user request.\n" +
	"Refer to the standing \"Skill Evolution Self-Check\" rules and judge only whether this round contains a reusable " +
	"update; do not re-evaluate the runtime trigger threshold.\n" +
	"If suggesting, append only one or two sentences to the normal final reply and include both the reusable update " +
	"and the Skill evolution question; otherwise reply naturally without mentioning this reminder or internal judgment."

// skillMDRE 从文件路径提取技能名的正则（SKILL.md 路径形态）。
// Python: SkillEvolutionRail._SKILL_MD_RE（:154）
var skillMDRE = regexp.MustCompile(`[/\\]([^/\\]+)[/\\]SKILL\.md`)

// skillMDFileReadTools 可能按路径读到 SKILL.md 的文件读工具闭集。
// Python: SkillEvolutionRail._SKILL_MD_FILE_READ_TOOLS（:155）
var skillMDFileReadTools = map[string]bool{"read": true, "read_file": true, "read_file_stream": true}

// subjectLabels 活动审查主体展示名。
// Python: SkillEvolutionRail._SUBJECT_LABELS（:159-162）
var subjectLabels = map[string]string{
	"skill":       "skill",
	"swarm-skill": "swarm skill",
}

// experienceIndexIDRE SKILL.md Evolution Index 链接里的记录 ID 正则。
// Python: SkillEvolutionRail._EXPERIENCE_INDEX_ID_RE（:157）
var experienceIndexIDRE = regexp.MustCompile(`(?:#|/)(ev_[A-Za-z0-9_-]+)\b`)

// ──────────────────────────── 全局变量 ────────────────────────────

// experienceRecordHeadingRE 匹配经验记录标题行中的 record ID。
// Python: SkillEvolutionRail._EXPERIENCE_RECORD_HEADING_RE
var experienceRecordHeadingRE = regexp.MustCompile(`#+\s*\[([A-Za-z0-9_-]+)\]`)

// failureRe 匹配 "error" 关键词，对齐 Python _FAILURE_KEYWORDS 中的 error 部分。
// 其他失败关键词用 otherFailureRe 匹配（它们不需要负向前瞻）。
// 对齐 Python: _FAILURE_KEYWORDS = re.compile(r"error(?!\s*=\s*None)|exception|...", re.IGNORECASE)
var (
	failureRe         = regexp.MustCompile(`(?i)\berror\b`)
	otherFailureRe    = regexp.MustCompile(`(?i)\bexception\b|\btraceback\b|\bfailed\b|\bfailure\b|\btimeout\b|\btimed out\b|\berrno\b|\bconnectionerror\b|\boserror\b|\bvalueerror\b|\btypeerror\b|错误|异常|失败|超时|\bno such file\b|\bpermission denied\b|\baccess denied\b|\bcommand not found\b|\bnot recognized\b|\bmodule not found\b|\beconnrefused\b|\beconnreset\b|\benoent\b|\benotfound\b|\bnpm err!`)
	errorNoneSuffixRe = regexp.MustCompile(`^\s*=\s*[Nn]one\b`)
)

// ──────────────────────────── 导出函数 ────────────────────────────

// NewSkillEvolutionRail 创建 SkillEvolutionRail 实例。
// 必选参数：skillsDir（技能目录）、llmModel（LLM 客户端）、model（模型名称）、language（语言）。
// 可选参数通过 Functional Options 覆盖默认配置。
// 对齐 Python: SkillEvolutionRail.__init__(
//
//	skills_dir, llm, model, auto_scan, auto_save, language,
//	trajectory_store, eval_interval, evolution_total_timeout_secs,
//	generate_records_llm_policy, evaluate_llm_policy, simplify_llm_policy,
//	sharing_config, disabled_skills
//
//	Python: )
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
		modelName:                model,
		autoSave:                 true,
		subjectKindValue:         "skill",
		reviewInterval:           5,
		reviewAgentMaxIterations: 25,
		evalInterval:             defaultEvalInterval,
		evolutionTimeoutSec:      defaultEvolutionTimeoutSecs,
		processedSignalKeys:      make(map[[4]string]bool),
		skillOps:                 make(map[string]*skill_call.SkillExperienceOperator),
		pendingGovernance:        make(map[string]*experience.PendingGovernance),
		pendingApprovalSnapshots: make(map[string]*experience.PendingChange),
		generateRecordsLLMPolicy: skillopt.GenerateRecordsLLMPolicy,
		evaluateLLMPolicy:        experience.EvaluateLLMPolicy,
		simplifyLLMPolicy:        experience.SimplifyLLMPolicy,
	}

	// 初始化 EvolutionRail 基类（先于 Options，确保 Option 可安全访问基类字段）
	// Python: super().__init__(default_member_role="teammate", ...)
	railOpts := []EvolutionRailOption{
		WithDefaultMemberRole("teammate"),
	}
	if r.evolutionTimeoutSec > 0 {
		railOpts = append(railOpts, WithMaxConcurrentEvolution(1))
	}
	r.EvolutionRail = NewEvolutionRail(r, railOpts...)

	// 应用可选参数（此时基类已初始化，Option 可安全设置基类字段）
	for _, opt := range opts {
		opt(r)
	}

	// ─── 初始化核心组件 ───

	// Python: self._evolution_store = self._make_evolution_store(skills_dir)
	r.evolutionStore = r.makeEvolutionStore()

	// Python: self._evolver = self._make_skill_optimizer(llm, model, language, ...)
	r.evolver = r.makeSkillOptimizer(llmModel, model, language)

	// Python: self._scorer = ExperienceScorer(llm, model, language, evaluate_llm_policy=..., simplify_llm_policy=...)
	r.scorer, _ = experience.NewExperienceScorer(
		llmModel, model, language,
		&r.evaluateLLMPolicy,
		&r.simplifyLLMPolicy,
	)

	// Python: self._manager = self._make_experience_manager()
	r.manager, _ = r.makeExperienceManager()

	// Python: self._approval_runtime = EvolutionApprovalRuntime(manager=..., pending_approval_snapshots=...)
	r.approvalRuntime = NewEvolutionApprovalRuntime(
		r.manager,
		r.pendingApprovalSnapshots,
	)

	// Python: self._online_updater = SingleDimUpdater(self._evolver)
	r.onlineUpdater = single_dim.NewSingleDimUpdater(r.evolver)

	// Python: self._online_orchestrator = OnlineEvolutionOrchestrator(store=..., updater=..., manager=..., skill_ops=..., stage_source="experience_updater")
	// Python: self._online_orchestrator = self._make_online_orchestrator()
	r.orchestrator = r.makeOnlineOrchestrator()

	// Python: self._experience_tracker = ExperienceTracker(store=..., scorer=..., eval_interval=...)
	r.experienceTracker = experience.NewExperienceTracker(
		r.evolutionStore,
		r.scorer,
		r.evalInterval,
	)

	// ─── 初始化 Sharing ───
	// Python: self._init_sharing(sharing_config, llm=llm, model=model, language=language, evolution_store=self._evolution_store)
	r.initSharing(r.sharingConfig, llmModel, model, language)

	return r
}

// WithAutoSave 设置是否自动保存（跳过审批）。
func WithAutoSave(autoSave bool) SkillEvolutionRailOption {
	return func(r *SkillEvolutionRail) { r.autoSave = autoSave }
}

// WithEvolutionReviewRuntime 注入共享审查运行时。
// Python: SkillEvolutionRail(review_runtime=...)（必选 kwarg，Go 经选项注入）
func WithEvolutionReviewRuntime(runtime *review.EvolutionReviewRuntime) SkillEvolutionRailOption {
	return func(r *SkillEvolutionRail) { r.reviewRuntime = runtime }
}

// WithEvolutionTrajectorySpanProcessor 注入共享观测处理器。
// Python: SkillEvolutionRail(trajectory_span_processor=...)（必选 kwarg）
func WithEvolutionTrajectorySpanProcessor(processor *trajectory.TrajectorySpanProcessor) SkillEvolutionRailOption {
	return func(r *SkillEvolutionRail) { r.trajectorySpanProcessor = processor }
}

// WithEvalInterval 设置评估间隔。
func WithEvalInterval(interval int) SkillEvolutionRailOption {
	return func(r *SkillEvolutionRail) {
		if interval < 1 {
			interval = 1
		}
		r.evalInterval = interval
	}
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

// WithSharingConfigMap 设置跨用户共享配置字典。
// 仅保存配置，实际解析在 initSharing 中完成。
// 对齐 Python: SkillEvolutionRail.__init__(sharing_config=...)
func WithSharingConfigMap(config map[string]any) SkillEvolutionRailOption {
	return func(r *SkillEvolutionRail) { r.sharingConfig = config }
}

// WithDisabledSkillsSet 设置禁用的技能名称列表。
// 基类已初始化后，同时修改子类和基类的 disabledSkills。
func WithDisabledSkillsSet(names []string) SkillEvolutionRailOption {
	return func(r *SkillEvolutionRail) {
		r.disabledSkills = names
		if r.EvolutionRail != nil {
			r.EvolutionRail.disabledSkills = normalizeSkillNames(names)
		}
	}
}

// WithSignalTrigger 设置是否触发确定性信号驱动演化。
// Python: SkillEvolutionRail(signal_trigger=...)（缺省 None→False）
func WithSignalTrigger(v bool) SkillEvolutionRailOption {
	return func(r *SkillEvolutionRail) { r.signalTrigger = v }
}

// WithTwoStage 设置经验生成是否使用 analyzer+formatter 双段管线。
// Python: SkillEvolutionRail(two_stage=...)
func WithTwoStage(v bool) SkillEvolutionRailOption {
	return func(r *SkillEvolutionRail) { r.twoStage = v }
}

// WithReviewTrigger 设置是否周期性排入审查自检 follow-up。
// Python: SkillEvolutionRail(review_trigger=...)（缺省 None→False）
func WithReviewTrigger(v bool) SkillEvolutionRailOption {
	return func(r *SkillEvolutionRail) { r.reviewTrigger = v }
}

// WithReviewInterval 设置审查自检间隔（非跟进任务迭代数）。
// Python: SkillEvolutionRail(review_interval=5)
func WithReviewInterval(v int) SkillEvolutionRailOption {
	return func(r *SkillEvolutionRail) { r.reviewInterval = v }
}

// WithReviewAgentMaxIterations 设置审查子 agent 最大迭代数。
// Python: SkillEvolutionRail(review_agent_max_iterations=25)
func WithReviewAgentMaxIterations(v int) SkillEvolutionRailOption {
	return func(r *SkillEvolutionRail) { r.reviewAgentMaxIterations = v }
}

// WithSubjectKind 设置活动审查主体类别（"skill" / "swarm-skill"）。
// Python: SkillEvolutionRail(subject_kind="skill")
func WithSubjectKind(kind string) SkillEvolutionRailOption {
	return func(r *SkillEvolutionRail) { r.subjectKindValue = checkpointing.NormalizeSubjectKind(kind) }
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
func (r *SkillEvolutionRail) AutoScan() bool { return r.signalTrigger }

// SetAutoScan 设置是否自动检测演进信号。
// Python: SkillEvolutionRail.auto_scan = value
func (r *SkillEvolutionRail) SetAutoScan(v bool) { r.signalTrigger = v }

// AutoSave 返回是否自动保存。
// Python: SkillEvolutionRail.auto_save
func (r *SkillEvolutionRail) AutoSave() bool { return r.autoSave }

// ReviewRuntime 返回审查运行时。
// Python: SkillEvolutionRail.review_runtime property（skill_evolution_rail.py:678）
func (r *SkillEvolutionRail) ReviewRuntime() *review.EvolutionReviewRuntime { return r.reviewRuntime }

// TrajectorySpanProcessor 返回共享观测处理器。
// Python: SkillEvolutionRail.trajectory_span_processor（校验一致性用）
func (r *SkillEvolutionRail) TrajectorySpanProcessor() *trajectory.TrajectorySpanProcessor {
	return r.trajectorySpanProcessor
}

// ReviewLanguage 返回语言（configuration 校验用）。
// Python: _language 属性读取（configuration.py:300 getattr(existing, "_language")）
func (r *SkillEvolutionRail) ReviewLanguage() string { return r.language }

// AsAgentRail 返回 rail 的 AgentRail 载体形态（configuration 装配用）。
func (r *SkillEvolutionRail) AsAgentRail() agentinterfaces.AgentRail { return r }

// ApprovalSubmissionService 返回审批提交服务（审批中断 rail 绑定用）。
// Python: SkillEvolutionRail.approval_submission_service property（:673，
// 返回 self._manager.experience_submission_service）。
// 2026-12-24 15.24b：submission.go 真实现随批落地，替换原 nil 占位（⤴️ 销账）。
func (r *SkillEvolutionRail) ApprovalSubmissionService() EvolutionSubmissionService {
	// 对象身份稳定（Python 返回 manager 持有的同一服务实例；幂等复用按身份比较）。
	if r.submissionAdapter == nil {
		r.submissionAdapter = NewExperienceSubmissionServiceAdapter(r.evolutionStore, r.pendingApprovalSnapshots)
	}
	return r.submissionAdapter
}

// SetAutoSave 设置是否自动保存。
// Python: SkillEvolutionRail.auto_save = value
func (r *SkillEvolutionRail) SetAutoSave(v bool) { r.autoSave = v }

// ModelName 返回当前 LLM 模型名。
// 注：Python 无对应访问器，属 Go 侧可观测性补充（测试需断言就地更新是否生效）。
func (r *SkillEvolutionRail) ModelName() string { return r.modelName }

// ApprovalRuntime 返回审批运行时。
// Python: SkillEvolutionRail.approval_runtime (property) — 延迟重建
func (r *SkillEvolutionRail) ApprovalRuntime() *EvolutionApprovalRuntime {
	runtime := r.approvalRuntime
	// Python: 检查 manager 和 pendingApprovalSnapshots 是否变化，不一致时重建
	needRebuild := runtime == nil
	if runtime != nil {
		// Python 用 is 判断对象身份；Go 用接口比较（manager）和 reflect.Pointer 比较（map）
		if runtime.manager != r.manager {
			needRebuild = true
		}
		// map 只能与 nil 比较，使用 reflect 判断是否同一底层对象
		if reflect.ValueOf(runtime.pendingApprovalSnapshots).Pointer() != reflect.ValueOf(r.pendingApprovalSnapshots).Pointer() {
			needRebuild = true
		}
	}
	if needRebuild {
		runtime = NewEvolutionApprovalRuntime(r.manager, r.pendingApprovalSnapshots)
		r.approvalRuntime = runtime
	}
	return runtime
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
	// Python: _on_before_invoke（:574-577）——重置本 invoke 的活动演化状态
	r.skipSignalTriggerThisInvoke = false
	return nil
}

// OnAfterModelCall 空操作，基类已记录 LLM 步骤。
// Python: SkillEvolutionRail 无此扩展点，空操作
func (r *SkillEvolutionRail) OnAfterModelCall(ctx context.Context, cbc *agentinterfaces.AgentCallbackContext) error {
	return nil
}

// OnAfterToolCall 检测经验详情读取，追踪 presented records。
// SKILL.md 读取仅用于索引发现。BODY 经验只有在 evolution/*.md 详情文件被读取
// 且返回内容包含具体记录标题时才计入 presented。
// 对齐 Python: SkillEvolutionRail._on_after_tool_call(ctx)
func (r *SkillEvolutionRail) OnAfterToolCall(ctx context.Context, cbc *agentinterfaces.AgentCallbackContext) error {
	inputs, ok := cbc.Inputs().(*agentinterfaces.ToolCallInputs)
	if !ok {
		return nil
	}
	// Python: _on_after_tool_call（:769-785）——追踪暴露给 agent 的经验而不改写工具结果
	return r.trackPresentedFromToolCall(ctx, cbc, inputs)
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
// 对齐 Python: SkillEvolutionRail._allow_evolution_trigger(trigger_point, ctx)
func (r *SkillEvolutionRail) AllowEvolutionTrigger(trigger EvolutionTriggerPoint, cbc *agentinterfaces.AgentCallbackContext) bool {
	// Python: _allow_evolution_trigger（:560-572）——signal_trigger 门 +
	// 后台运行排除 + 活动演化活动跳过。
	if !r.signalTrigger {
		return false
	}
	if r.isBackgroundRun(cbc) {
		return false
	}
	if r.skipSignalTriggerThisInvoke {
		logger.Info(logComponent).Msg("[SkillEvolutionRail] active evolution activity detected, skip passive signal scan")
		return false
	}
	return true
}

// SnapshotForEvolution 同步捕获快照（cbc 仍活跃），供后台演化任务使用。
// Phase 1: 收集 messages + session_id + presented_entries + incremental_messages
//
// 对齐 Python: SkillEvolutionRail._snapshot_for_evolution(trajectory, ctx)
func (r *SkillEvolutionRail) SnapshotForEvolution(ctx context.Context, traj *trajectory.Trajectory, cbc *agentinterfaces.AgentCallbackContext) *EvolutionSnapshot {
	if !r.signalTrigger {
		return nil
	}

	// 基类快照：收集 messages
	snapshot := trajectoryToMessages(traj)
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
// 异步模式：ctx=nil，snapshot 包含捕获的数据；同步模式：ctx 活跃，snapshot=nil。
// 对齐 Python: SkillEvolutionRail.run_evolution(trajectory, ctx, *, snapshot)
func (r *SkillEvolutionRail) RunEvolution(ctx context.Context, traj *trajectory.Trajectory, snapshot *EvolutionSnapshot) error {
	defer func() {
		if rec := recover(); rec != nil {
			logger.Error(logComponent).Any("panic", rec).Str("method", "RunEvolution").
				Msg("[SkillEvolutionRail] RunEvolution panic")
		}
	}()
	logger.Info(logComponent).Bool("auto_scan", r.signalTrigger).Msg("[SkillEvolutionRail] run_evolution called")
	if !r.signalTrigger {
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
		messages = trajectoryToMessages(traj)
		// 对齐 Python: presented_entries = self._experience_tracker.consume_eval_state(session)
		// 同步路径无 cbc，无法获取 sessionID，传空字符串
		presentedEntries = r.experienceTracker.ConsumeEvalState("")
	} else {
		return nil
	}

	logger.Info(logComponent).Int("messages", len(messages)).Msg("[SkillEvolutionRail] messages collected")

	r.emitProgress("started", "starting regular skill evolution review for completed conversation")

	if len(messages) == 0 {
		logger.Info(logComponent).Msg("[SkillEvolutionRail] no messages, skipping")
		r.emitProgress("cancelled", "no conversation messages available; cancelling regular skill evolution review")
		r.experienceTracker.EvaluatePresented(ctx, presentedEntries)
		return nil
	}

	// 列出 regular skills
	// Python: all_skill_names = self._evolution_store.list_skill_names()
	allSkillNames := r.evolutionStore.ListSkillNames(ctx)
	skillNames := r.filterRegularSkills(ctx, allSkillNames)

	r.emitProgress("detecting_signals", fmt.Sprintf(
		"checking %d regular skill(s) for evolution signals (filtered from %d local skill(s))",
		len(skillNames), len(allSkillNames)))

	// 信号检测
	// Python: detector = SignalDetector(existing_skills=...).bind_llm(llm=..., model=..., language=...)
	detector := signal.NewConversationSignalDetector(
		signal.WithExistingSkills(r.existingSkillSet(ctx, skillNames)),
	).BindLLM(r.evolver.LLM(), r.evolver.ModelName(), r.language)

	// Python: detected = detector.detect_trajectory_signals(
	//     trajectory, signal_types={"execution_failure", "script_artifact"})（:892-896）
	detected := detector.DetectTrajectorySignals(traj, nil)

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

	// 限制去重集合大小
	// Python: if len(self._processed_signal_keys) > _MAX_PROCESSED_SIGNAL_KEYS: self._processed_signal_keys.clear()
	if len(r.processedSignalKeys) > maxProcessedSignalKeys {
		r.processedSignalKeys = make(map[[4]string]bool)
	}

	logger.Info(logComponent).Int("signals", len(signals)).Msg("[SkillEvolutionRail] signals detected")

	// 技能归属
	// Python: attributed_skills = {s.skill_name for s in signals if s.skill_name};
	//         unattributed = [s for s in signals if not s.skill_name]（:905-913）
	// 仅一个归属技能且存在未归属信号时，未归属信号回填到该技能。
	attributedSkillsSet := map[string]bool{}
	var unattributed []*signal.EvolutionSignal
	for _, sig := range signals {
		if sig.SkillName != nil && *sig.SkillName != "" {
			attributedSkillsSet[*sig.SkillName] = true
		} else {
			unattributed = append(unattributed, sig)
		}
	}
	if len(attributedSkillsSet) == 1 && len(unattributed) > 0 {
		fallbackSkill := ""
		for name := range attributedSkillsSet {
			fallbackSkill = name
		}
		for _, sig := range unattributed {
			sig.SkillName = &fallbackSkill
		}
	}
	skillGroups := map[string][]*signal.EvolutionSignal{}
	for _, sig := range signals {
		if sig.SkillName == nil || *sig.SkillName == "" {
			continue
		}
		skillGroups[*sig.SkillName] = append(skillGroups[*sig.SkillName], sig)
	}
	if len(skillGroups) == 0 {
		// Python: 两种文案（:916-927）；评估先于 cancelled 终态发出
		//（宿主 watcher 把 cancelled 当周期结束，不能与评分竞争）。
		var message string
		if len(signals) > 0 {
			message = "detected evolution signals but no regular skill could be attributed; " +
				"cancelling regular skill evolution review"
		} else {
			message = "no skill usage of a regular skill or actionable evolution signal detected; " +
				"cancelling regular skill evolution review"
		}
		r.experienceTracker.EvaluatePresented(ctx, presentedEntries)
		r.emitProgress("cancelled", message)
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
	// Python: for skill_name, skill_signals in skill_groups.items(): generated = await
	//         self._evolve_skill_with_sharing(...)；未产出记录的延迟到评分后报 cancelled（:935-955）
	deferredCancelled := []struct{ skillName, message string }{}
	for skillName, skillSignals := range skillGroups {
		sharedRecords := downloadedPerSkill[skillName]
		if generated := r.evolveSkillWithSharing(ctx, skillName, skillSignals, messages, sharedRecords); !generated {
			deferredCancelled = append(deferredCancelled, struct{ skillName, message string }{
				skillName,
				fmt.Sprintf("attributed optimizer signal for '%s' produced no reusable evolution records", skillName),
			})
		}
	}

	// 评分先于终态 cancelled（宿主 watcher 把 cancelled 当周期结束）
	r.experienceTracker.EvaluatePresented(ctx, presentedEntries)
	for _, deferred := range deferredCancelled {
		r.emitProgress("cancelled", deferred.message, WithSkillName(deferred.skillName))
	}
	return nil
}

// GetEvolutionTotalTimeoutSecs 获取演进总超时秒数。
// Python: SkillEvolutionRail._get_evolution_total_timeout_secs()
func (r *SkillEvolutionRail) GetEvolutionTotalTimeoutSecs() float64 {
	return r.evolutionTimeoutSec
}

// ─── Slash 命令 API 方法 ───

// RequestUserEvolution 用户主动触发的演进入口（兼容壳）。
// Python: request_user_evolution（skill_evolution_rail.py:1082-1100）——
// 新版主动请求 = agent_prompt 模式：只产出审查工作流 followup prompt，
// 由 host 注入 query 后在 agent 轮内走 prepare→review→submit→evolve 六工具链；
// auto_approve 等旧旗标保留调用兼容但有意忽略（del auto_approve）。
func (r *SkillEvolutionRail) RequestUserEvolution(ctx context.Context, skillName string, userIntent string, autoApprove bool) (*EvolutionRequestResult, error) {
	_ = autoApprove // Python: del auto_approve（兼容旧调用点旗标）
	subject := r.activeReviewSubject(ctx, skillName)
	if subject == nil {
		return &EvolutionRequestResult{SkillName: skillName}, nil
	}
	return buildActiveEvolutionFollowup(skillName, subject, &userIntent)
}

// activeReviewSubject 解析主动审查 subject（store 支持时用 store payload，
// 否则回退 skill 形态）。
// Python: _active_review_subject（skill_evolution_rail.py:1151-1156）。
func (r *SkillEvolutionRail) activeReviewSubject(ctx context.Context, skillName string) map[string]string {
	if payload := r.evolutionStore.ResolveSubjectPayload(ctx, skillName); payload != nil {
		return payload
	}
	return map[string]string{"kind": "skill", "name": skillName}
}

// StageEvolutionFromSignalsForTest 信号驱动的 staging 半区（仅测试用）。
// 与旧版 RequestUserEvolution 的生成编排同构（traj→detect→user_intent signal→
// handleEvolutionFromSignals，requires_approval=true、emit_host_events=false）；
// 上游删除该编排壳后，被动链由 signal 回调驱动，测试经本钩子直达真实 staging。
func (r *SkillEvolutionRail) StageEvolutionFromSignalsForTest(ctx context.Context, skillName, userIntent string) (*experience.ExperienceApprovalRequest, *stream.OutputSchema, error) {
	traj := r.buildTrajectory()
	var messages []map[string]any
	var signals []*signal.EvolutionSignal

	if traj != nil && len(trajectory.IterSpans(traj)) > 0 {
		messages = trajectoryToMessages(traj)
		detected := r.detectActiveRequestSignals(ctx, skillName, traj, messages)
		signals = append(signals, detected...)
	}
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
	if len(messages) == 0 && userIntent != "" {
		messages = []map[string]any{{"role": "user", "content": userIntent}}
	}
	request, err := r.handleEvolutionFromSignals(ctx, skillName, signals, messages, nil, userIntent, true, false)
	if err != nil || request == nil {
		return request, nil, err
	}
	return request, r.buildGeneratedRecordsEvent(skillName, request), nil
}

// RequestSimplify 请求精简技能经验。
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
// 对齐 Python: SkillEvolutionRail.on_reject_simplify(request_id)
func (r *SkillEvolutionRail) OnRejectSimplify(requestID string) {
	gov, ok := r.pendingGovernance[requestID]
	r.manager.RejectSimplify(requestID)
	if ok && gov != nil {
		logger.Info(logComponent).Str("skill_name", gov.SkillName).Msg("[SkillEvolutionRail] simplify rejected")
	}
}

// RequestRebuild 构建技能重建提示。
// 返回 followup_prompt 文本，调用方注入 agent loop 执行 skill-creator 生成新 SKILL.md。
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
		logger.Warn(logComponent).Str("skill", skillName).Msg("[SkillEvolutionRail] no archive directory")
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
			logger.Warn(logComponent).Str("skill", skillName).Msg("[SkillEvolutionRail] no archive body")
			return false, nil
		}
		// 按文件名降序排序，取最新版本
		sort.Slice(bodyFiles, func(i, j int) bool {
			return bodyFiles[i].Name() > bodyFiles[j].Name()
		})
		bodyArchivePath = filepath.Join(archiveDir, bodyFiles[0].Name())
		evoVersion := strings.ReplaceAll(strings.ReplaceAll(filepath.Base(bodyArchivePath), "SKILL.", "evolutions."), ".md", ".json")
		evoArchivePath = filepath.Join(archiveDir, evoVersion)
	}

	// 先归档当前状态
	// Python: await store.archive_skill_body(skill_name); await store.archive_evolutions(skill_name)
	if _, err := store.ArchiveSkillBody(ctx, skillName); err != nil {
		logger.Error(logComponent).Err(err).Str("skill", skillName).Msg("Failed to archive skill body")
	}
	if _, err := store.ArchiveEvolutions(ctx, skillName); err != nil {
		logger.Error(logComponent).Err(err).Str("skill", skillName).Msg("Failed to archive evolutions")
	}

	// 恢复旧 body
	oldBody, err := store.ReadFileText(ctx, bodyArchivePath)
	if err != nil {
		return false, fmt.Errorf("读取归档 body 失败: %w", err)
	}
	if _, err := store.WriteSkillContent(ctx, skillName, oldBody); err != nil {
		logger.Error(logComponent).Err(err).Str("skill", skillName).Msg("Failed to restore skill body")
	}

	// 恢复旧 evolutions.json
	if _, statErr := os.Stat(evoArchivePath); statErr == nil {
		evoContent, readErr := store.ReadFileText(ctx, evoArchivePath)
		if readErr == nil {
			evoPath := filepath.Join(skillDir, "evolutions.json")
			if writeErr := store.WriteFileText(ctx, evoPath, evoContent); writeErr != nil {
				logger.Error(logComponent).Err(writeErr).Str("skill", skillName).Msg("Failed to restore evolutions.json")
			}
		}
	} else {
		if err := store.ClearEvolutions(ctx, skillName); err != nil {
			logger.Error(logComponent).Err(err).Str("skill", skillName).Msg("Failed to clear evolutions")
		}
	}

	// Python: await store.render_evolution_markdown(skill_name)
	if err := store.RenderEvolutionMarkdown(ctx, skillName); err != nil {
		logger.Error(logComponent).Err(err).Str("skill", skillName).Msg("Failed to render evolution markdown")
	}

	logger.Info(logComponent).Str("skill", skillName).Str("archive", filepath.Base(bodyArchivePath)).Msg("[SkillEvolutionRail] rollback completed")
	return true, nil
}

// ApproveRecord 批准暂存的演进记录。
// 对齐 Python: SkillEvolutionRail.approve_record(request_id)
func (r *SkillEvolutionRail) ApproveRecord(ctx context.Context, requestID string, approvedRecordIDs ...string) error {
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

	// Python: if approved_record_ids is not None and approved_records:
	//   approved_records = [r for r in approved_records if r.id in approved_id_set]
	// （skill_evolution_rail.py:1458-1460 部分批准过滤；variadic 空参 = None 语义）
	if len(approvedRecordIDs) > 0 && len(approvedRecords) > 0 {
		approvedIDSet := make(map[string]bool, len(approvedRecordIDs))
		for _, id := range approvedRecordIDs {
			approvedIDSet[id] = true
		}
		filtered := approvedRecords[:0]
		for _, record := range approvedRecords {
			if approvedIDSet[record.ID] {
				filtered = append(filtered, record)
			}
		}
		approvedRecords = filtered
	}

	// Python: pending, result = await self.approval_runtime.approve_pending_request(request_id, ...)
	// Python: approve_kwargs["approved_record_ids"]（approval_runtime.py:19-21 转发）——
	// variadic 空参 = None（all-or-nothing）；非空时逐层转发至 commit 过滤段。
	pending, result, err := r.approvalRuntime.ApprovePendingRequest(ctx, requestID, "SkillEvolutionRail", "approve_record", approvedRecordIDs...)
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
	// Python: if not is_shared and approved_records: await self._upload_approved_records_for_sharing(pending, request_id)
	if !isShared && len(approvedRecords) > 0 {
		r.uploadApprovedRecordsForSharing(ctx, pending.SkillName, approvalMessages, approvedRecords)
	}

	return nil
}

// PendingChangeRecordIDs 返回待审批快照 payload 内的 record_id 列表；
// 全空（无任何非空 id）返回 nil——对齐 Python record_ids_from_pending_approval
// （evolution_helpers.py:454-475）「has_record_id 否则 None」语义。
// 前端对 record_id 无感知，host 层按索引映射回 record_id 后逐条回传。
func (r *SkillEvolutionRail) PendingChangeRecordIDs(requestID string) []string {
	pending := r.pendingApprovalSnapshots[requestID]
	if pending == nil {
		return nil
	}
	recordIDs := make([]string, 0, len(pending.Payload))
	hasRecordID := false
	for _, record := range pending.Payload {
		id := record.ID
		recordIDs = append(recordIDs, id)
		if id != "" {
			hasRecordID = true
		}
	}
	if !hasRecordID {
		return nil
	}
	return recordIDs
}

// RejectRecord 拒绝暂存的演进记录。
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

// OnApprove ApproveRecord 的兼容别名。
// 对齐 Python: SkillEvolutionRail.on_approve
func (r *SkillEvolutionRail) OnApprove(ctx context.Context, requestID string) error {
	return r.ApproveRecord(ctx, requestID)
}

// OnReject RejectRecord 的兼容别名。
// 对齐 Python: SkillEvolutionRail.on_reject
func (r *SkillEvolutionRail) OnReject(ctx context.Context, requestID string) error {
	return r.RejectRecord(ctx, requestID)
}

// ShouldHintSimplifyOrRebuild 检查技能是否有足够经验来建议精简/重建。
// 对齐 Python: SkillEvolutionRail.should_hint_simplify_or_rebuild(skill_name)
func (r *SkillEvolutionRail) ShouldHintSimplifyOrRebuild(skillName string) bool {
	evoLog, err := r.evolutionStore.LoadFullEvolutionLog(context.Background(), skillName)
	if err != nil || evoLog == nil {
		return false
	}
	// Python: return len(entries) >= 10
	return len(evoLog.Entries) >= 10
}

// UpdateLLM 热更新 LLM 客户端和模型。
// 对齐 Python: SkillEvolutionRail.update_llm(llm, model)
func (r *SkillEvolutionRail) UpdateLLM(llmModel *llm.Model, model string) {
	r.modelName = model
	r.evolver.UpdateLLM(llmModel, model)
	r.scorer.UpdateLLM(llmModel, model)
	if r.keywordExtractor != nil {
		r.keywordExtractor.UpdateLLM(llmModel, model)
	}
}

// SetSysOperation 设置系统操作，同时传递给 EvolutionRail 基类和 EvolutionStore。
// 对齐 Python: SkillEvolutionRail.set_sys_operation(sys_operation)
func (r *SkillEvolutionRail) SetSysOperation(op sys_operation.SysOperation) {
	// Python: super().set_sys_operation(sys_operation)
	r.EvolutionRail.SetSysOperation(op)
	// Python: self._evolution_store.sys_operation = sys_operation
	r.evolutionStore.SetSysOperation(op)
}

// RecordPresentedExperiences 记录由非 rail 路径呈现的经验。
// 对齐 Python: SkillEvolutionRail.record_presented_experiences(skill_name, presentation_snippet, *, session=None, record_ids=None)
func (r *SkillEvolutionRail) RecordPresentedExperiences(ctx context.Context, skillName string, presentationSnippet string, sessionID string, recordIDs []string) {
	if len(recordIDs) > 0 {
		r.experienceTracker.RecordPresentedRecords(ctx, sessionID, skillName, presentationSnippet, recordIDs)
		return
	}
	r.experienceTracker.RecordPresented(ctx, sessionID, skillName, presentationSnippet)
}

// initSharing 初始化 Sharing 组件。
// 对齐 Python: SkillEvolutionSharingMixin._init_sharing(sharing_config, *, llm, model, language, evolution_store)
func (r *SkillEvolutionRail) initSharing(sharingConfig map[string]any, llmModel *llm.Model, model string, language string) {
	// Python: self._sharing_download_top_k = self._resolve_download_top_k(sharing_config)
	r.sharingDownloadTopK = resolveDownloadTopK(sharingConfig)
	r.sharingConfig = sharingConfig
	r.excerptOffsets = make(map[string]int)

	// Python: self._experience_sharer = self._build_experience_sharer(sharing_config)
	// enabled 解析对齐 Python _build_experience_sharer 中的 env+config 判断
	enabled := resolveSharingBool(sharingConfig["enabled"])
	envEnabled := os.Getenv("EVOLUTION_SHARING_ENABLED")
	if envEnabled != "" {
		enabled = envEnabled == "true" || envEnabled == "1"
	}
	if !enabled || sharingConfig == nil {
		r.experienceSharer = nil
		r.keywordExtractor = nil
		r.shareStager = nil
		r.sharingEnabled = false
		return
	}

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
	r.shareStager = sharing.NewShareStager(r.keywordExtractor, r.experienceSharer, 0.6, nil)

	// Python: self._experience_sharer.set_skill_sharing_context_provider(self._make_sharing_context_provider(evolution_store))
	r.experienceSharer.SetSkillSharingContextProvider(r.makeSharingContextProvider())
}

// buildExperienceSharer 构建 ExperienceSharer。
// 对齐 Python: SkillEvolutionSharingMixin._build_experience_sharer(sharing_config)
//
// Python: try: ... except Exception: logger.warning(...); return None
// 保留 recover 兜底防意外 panic，确保异常时降级返回 nil（共享功能禁用）。
func (r *SkillEvolutionRail) buildExperienceSharer() (sharer *sharing.ExperienceSharer) {
	config := r.sharingConfig
	if config == nil {
		config = map[string]any{}
	}

	// Python: try: ... except Exception as exc: logger.warning("...%s...", exc); return None
	defer func() {
		if rec := recover(); rec != nil {
			logger.Warn(logComponent).Any("exception", rec).
				Msg("[SkillEvolutionRail] failed to build ExperienceSharer, sharing disabled")
			sharer = nil
		}
	}()

	// Python: backend_name = str(config.get("backend", "local_file") or "local_file").lower()
	// Python: if backend_name != "local_file": logger.warning(...)
	backendName := "local_file"
	if v, ok := config["backend"]; ok {
		if s, ok := v.(string); ok && s != "" {
			backendName = strings.ToLower(s)
		}
	}
	if backendName != "local_file" {
		logger.Warn(logComponent).Str("backend", backendName).Msg("[SkillEvolutionRail] unsupported backend, falling back to local_file")
	}

	hubPath := os.Getenv("EVOLUTION_SHARING_HUB_PATH")
	if hubPath == "" {
		// Python: hub_path = os.getenv("EVOLUTION_SHARING_HUB_PATH") or str(config.get("hub_path") or "").strip() or None
		if hp, ok := config["hub_path"].(string); ok {
			hp = strings.TrimSpace(hp)
			if hp != "" {
				hubPath = hp
			}
		}
	}

	localCacheDir := ""
	if lcd, ok := config["local_cache_dir"].(string); ok {
		lcd = strings.TrimSpace(lcd)
		if lcd != "" {
			localCacheDir = lcd
		}
	}

	maxUploadRetries := defaultSharingMaxUploadRetries
	if raw, ok := config["max_upload_retries"]; ok {
		switch v := raw.(type) {
		case int:
			maxUploadRetries = v
		case float64:
			maxUploadRetries = int(v)
		case string:
			if n, err := fmt.Sscanf(v, "%d", &maxUploadRetries); err != nil || n != 1 {
				maxUploadRetries = defaultSharingMaxUploadRetries
			}
		}
		if maxUploadRetries < 1 {
			maxUploadRetries = 1
		}
	}

	// Python: backend = LocalFileBackend(hub_path=hub_path)
	backendInstance := backend.NewLocalFileBackend(hubPath, 0)

	// Python: self._experience_sharer = ExperienceSharer(backend=backend, local_cache_dir=..., max_upload_retries=...)
	return sharing.NewExperienceSharer(
		backendInstance,
		localCacheDir,
		maxUploadRetries,
		0,   // 退避基础秒数
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
		prevOffset := r.getExcerptOffsetByKey(excerptKey)
		incrementalMessages := messages[prevOffset:]
		r.setExcerptOffsetByKey(excerptKey, len(messages))
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

// getExcerptOffsetByKey 获取增量消息偏移量。
// 对齐 Python: SkillEvolutionSharingMixin._get_excerpt_offset_by_key(key)
func (r *SkillEvolutionRail) getExcerptOffsetByKey(key string) int {
	if key == "" {
		return 0
	}
	return r.excerptOffsets[key]
}

// setExcerptOffsetByKey 设置增量消息偏移量。
// 对齐 Python: SkillEvolutionSharingMixin._set_excerpt_offset_by_key(key, offset)
func (r *SkillEvolutionRail) setExcerptOffsetByKey(key string, offset int) {
	if key != "" {
		r.excerptOffsets[key] = offset
	}
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

	// Python: try: query = await self._keyword_extractor.extract_query_keywords(feedback_excerpt=excerpt)
	// Python: except Exception as exc: logger.warning("[SkillEvolutionRail] keyword extraction failed: %s", exc); return {}
	// 下游 ExtractQueryKeywords 已返回 error，保留 recover 兜底防意外 panic。
	var query sharing.QueryKeywords
	func() {
		defer func() {
			if rec := recover(); rec != nil {
				logger.Warn(logComponent).Any("exception", rec).
					Msg("[SkillEvolutionRail] keyword extraction exception, degrading to empty result")
				query = sharing.QueryKeywords{}
			}
		}()
		var kwErr error
		query, kwErr = r.keywordExtractor.ExtractQueryKeywords(ctx, excerpt)
		if kwErr != nil {
			logger.Warn(logComponent).Err(kwErr).
				Msg("[SkillEvolutionRail] keyword extraction failed, degrading to empty result")
			query = sharing.QueryKeywords{}
		}
	}()
	if len(query.Keywords) == 0 {
		return map[string][]checkpointing.EvolutionRecord{}
	}

	result := map[string][]checkpointing.EvolutionRecord{}
	for _, skillName := range involvedSkills {
		func() {
			defer func() {
				if rec := recover(); rec != nil {
					// Python: except Exception as exc: logger.warning("...download_relevant failed...: %s", exc); continue
					logger.Warn(logComponent).Str("skill", skillName).Any("exception", rec).
						Msg("[SkillEvolutionRail] download_shared_experiences exception for single skill, skipping")
				}
			}()
			if r.experienceSharer == nil {
				return
			}
			skillID, resolveErr := r.experienceSharer.ResolveSkillID(ctx, skillName)
			if resolveErr != nil {
				logger.Warn(logComponent).Str("skill", skillName).Err(resolveErr).
					Msg("[SkillEvolutionRail] resolve_skill_id failed, skipping shared download")
				return
			}
			if skillID == "" {
				logger.Debug(logComponent).Str("skill", skillName).Msg("[SkillEvolutionRail] skipping shared download: skill_id unavailable")
				return
			}
			bundles, dlErr := r.experienceSharer.DownloadRelevant(ctx, skillID, query, r.sharingDownloadTopK, skillName)
			if dlErr != nil {
				logger.Warn(logComponent).Str("skill", skillName).Str("skill_id", skillID).Err(dlErr).
					Msg("[SkillEvolutionRail] download_shared_experiences failed, skipping")
				return
			}

			var records []checkpointing.EvolutionRecord
			for _, bundle := range bundles {
				for _, sharedExp := range bundle.Experiences {
					marker := "\n" + sharedRecordContextMarker + bundle.BundleID + " skill_id=" + bundle.SkillID + "]"
					sharedExp.Record.Context = (sharedExp.Record.Context) + marker
					records = append(records, sharedExp.Record)
				}
			}
			if len(records) > 0 {
				result[skillName] = records
			}
		}()
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
) bool {
	if len(sharedRecords) > 0 {
		sharedRecords = r.filterDuplicateSharedRecords(ctx, skillName, sharedRecords)
	}

	if len(sharedRecords) == 0 {
		// 无共享记录：走正常演化流程
		// Python: return request_for_online_evolution_result(result) is not None（sharing.py:227-228）
		request, err := r.handleEvolutionFromSignals(ctx, skillName, skillSignals, messages, nil, "", !r.autoSave, true)
		if err != nil {
			logger.Warn(logComponent).Str("skill", skillName).Err(err).Msg("[SkillEvolutionRail] evolve_skill failed")
			r.emitProgress("failed", fmt.Sprintf("'%s' 演进失败: %s", skillName, err.Error()), WithSkillName(skillName))
			return false
		}
		if request == nil {
			r.emitProgress("completed", fmt.Sprintf("'%s' 未生成演进记录", skillName), WithSkillName(skillName))
		}
		return request != nil
	}

	// 有共享记录
	if r.autoSave {
		// 自动保存：先持久化共享记录，再正常演化
		for _, record := range sharedRecords {
			if err := r.evolutionStore.AppendRecord(ctx, skillName, record); err != nil {
				logger.Error(logComponent).Err(err).Str("skill", skillName).Msg("Failed to persist shared records")
			}
		}
		logger.Info(logComponent).Int("records", len(sharedRecords)).Str("skill", skillName).Msg("[SkillEvolutionRail] shared records persisted")
		if _, err := r.handleEvolutionFromSignals(ctx, skillName, skillSignals, messages, nil, "", false, true); err != nil {
			logger.Warn(logComponent).Err(err).Str("skill", skillName).Msg("[SkillEvolutionRail] post-shared-record evolution failed")
		}
		return true
	}

	// 需要审批：发送共享记录审批事件
	r.emitSharedRecordsApproval(ctx, skillName, sharedRecords, messages)
	return true
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
		ctx, skillName, records,
		true,                        // requiresApproval 需要审批
		"experience_sharing",        // source 来源
		"",                          // userQuery 用户查询
		nil,                         // signalType 信号类型
		nil,                         // signalSource 信号来源
		schema.SkillExperienceEntry, // changeType 变更类型
		"skill_evolve_",             // requestIDPrefix 请求ID前缀
		nil,                         // trajectory 轨迹
		messages,
		true, // isSharedRecords 共享记录
	)
	if request != nil {
		r.emitGeneratedRecords(nil, skillName, request)
	}
}

// stageRecordsForShare 暂存记录以供共享上传。
// 对齐 Python: SkillEvolutionSharingMixin._stage_records_for_share(skill_name, messages, records)
//
// Python: try: return await self._share_stager.screen_and_stage(...) except Exception: logger.warning(...); return None
// Go: 下游 ScreenAndStage 已返回 error，保留 recover 兜底防意外 panic。
func (r *SkillEvolutionRail) stageRecordsForShare(
	ctx context.Context,
	skillName string,
	messages []map[string]any,
	records []checkpointing.EvolutionRecord,
) (result *sharing.StagingResult) {
	if !r.IsSharingEnabled() || r.shareStager == nil || len(records) == 0 {
		return nil
	}
	defer func() {
		if rec := recover(); rec != nil {
			logger.Warn(logComponent).Str("skill", skillName).Any("exception", rec).
				Msg("[SkillEvolutionRail] share staging exception, degrading to nil")
			result = nil
		}
	}()
	var stageErr error
	result, stageErr = r.shareStager.ScreenAndStage(ctx, skillName, records, messages)
	if stageErr != nil {
		logger.Warn(logComponent).Str("skill", skillName).Err(stageErr).
			Msg("[SkillEvolutionRail] share staging failed, degrading to nil")
		result = nil
	}
	return result
}

// flushShareUploads 刷新共享上传。
// 对齐 Python: SkillEvolutionSharingMixin._flush_share_uploads(skill_name)
//
// Python: try: result = await self._experience_sharer.flush_pending_uploads(skill_name)
// Python: except Exception as exc: logger.warning("...failed...: %s", exc); return
// Python: if not result.ok and result.reason: logger.warning("...rejected...: %s", result.reason)
// Go: 下游 FlushPendingUploads 已返回 error，保留 recover 兜底防意外 panic。
func (r *SkillEvolutionRail) flushShareUploads(ctx context.Context, skillName string) {
	if r.experienceSharer == nil || !r.experienceSharer.HasPending(skillName) {
		return
	}
	defer func() {
		if rec := recover(); rec != nil {
			// Python: except Exception as exc: logger.warning("..."); return
			logger.Warn(logComponent).Str("skill", skillName).Any("exception", rec).
				Msg("[SkillEvolutionRail] flush pending uploads exception, degrading and skipping")
		}
	}()
	result, flushErr := r.experienceSharer.FlushPendingUploads(ctx, skillName)
	if flushErr != nil {
		logger.Warn(logComponent).Str("skill", skillName).Err(flushErr).
			Msg("[SkillEvolutionRail] flush pending uploads failed, degrading and skipping")
		return
	}
	if !result.OK && result.Reason != "" {
		logger.Warn(logComponent).Str("skill", skillName).Str("reason", result.Reason).Msg("[SkillEvolutionRail] shared upload rejected")
	}
}

// uploadApprovedRecordsForSharing 审批后上传共享记录。
// 对齐 Python: SkillEvolutionSharingMixin._upload_approved_records_for_sharing(pending, request_id)
//
// Python: try: await self._stage_records_for_share(...); await self._flush_share_uploads(...)
// Python: except Exception as exc: logger.warning("...approve share staging failed...: %s", exc)
// Go: 下游函数已返回 error，保留 recover 兜底防意外 panic。
func (r *SkillEvolutionRail) uploadApprovedRecordsForSharing(ctx context.Context, skillName string, messages []map[string]any, records []checkpointing.EvolutionRecord) {
	if !r.IsSharingEnabled() || r.shareStager == nil || len(records) == 0 {
		return
	}
	defer func() {
		if rec := recover(); rec != nil {
			// Python: except Exception as exc: logger.warning("...approve share staging failed for skill=%s (request=%s): %s", ...)
			logger.Warn(logComponent).Str("skill", skillName).Any("exception", rec).
				Msg("[SkillEvolutionRail] approval share staging exception, degrading and skipping")
		}
	}()
	r.stageRecordsForShare(ctx, skillName, messages, records)
	r.flushShareUploads(ctx, skillName)
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

	// Python: existing_desc = await self._evolution_store.get_pending_records(skill_name, EvolutionTarget.DESCRIPTION)
	// Python: existing_body = await self._evolution_store.get_pending_records(skill_name, EvolutionTarget.BODY)
	descTarget := signal.EvolutionTargetDescription
	bodyTarget := signal.EvolutionTargetBody
	existingDesc := r.evolutionStore.GetPendingRecords(ctx, skillName, &descTarget)
	existingBody := r.evolutionStore.GetPendingRecords(ctx, skillName, &bodyTarget)
	existingRecords := append(existingDesc, existingBody...)
	if len(existingRecords) == 0 {
		return sharedRecords
	}

	return r.llmCheckDuplicates(ctx, skillName, sharedRecords, existingRecords)
}

// llmCheckDuplicates 调用 LLM 判断共享记录与已有记录是否重复。
// 对齐 Python: SkillEvolutionSharingMixin._llm_check_duplicates(skill_name, shared_records, existing_records)
func (r *SkillEvolutionRail) llmCheckDuplicates(
	ctx context.Context,
	skillName string,
	sharedRecords []checkpointing.EvolutionRecord,
	existingRecords []checkpointing.EvolutionRecord,
) []checkpointing.EvolutionRecord {
	llmModel := r.evolver.LLM()
	modelName := r.evolver.ModelName()
	if llmModel == nil || modelName == "" {
		return sharedRecords
	}

	prompt := r.buildDuplicateCheckPrompt(skillName, sharedRecords, existingRecords)
	rawResponse, err := llm_resilience.InvokeTextWithRetry(ctx, llmModel, modelName, prompt, r.generateRecordsLLMPolicy,
		llm_resilience.WithRetryPrompt(prompt),
	)
	if err != nil {
		logger.Warn(logComponent).Str("skill", skillName).Err(err).Msg("[SkillEvolutionRail] LLM dedup check failed")
		return sharedRecords
	}
	return parseDuplicateCheckResponse(sharedRecords, rawResponse)
}

// buildDuplicateCheckPrompt 构建去重提示词。
// 对齐 Python: SkillEvolutionSharingMixin._build_duplicate_check_prompt(skill_name, shared_records, existing_records)
func (r *SkillEvolutionRail) buildDuplicateCheckPrompt(
	_ string,
	sharedRecords []checkpointing.EvolutionRecord,
	existingRecords []checkpointing.EvolutionRecord,
) string {
	var existingSummary []string
	for _, record := range existingRecords {
		content := record.Change.Content
		if len(content) > 500 {
			content = content[:500]
		}
		existingSummary = append(existingSummary,
			fmt.Sprintf("- [%s] [%s] [%s]\n  %s", record.ID, record.Change.Target, record.Change.Section, content))
	}
	var sharedSummary []string
	for _, record := range sharedRecords {
		content := record.Change.Content
		if len(content) > 500 {
			content = content[:500]
		}
		sharedSummary = append(sharedSummary,
			fmt.Sprintf("- [%s] [%s] [%s]\n  %s", record.ID, record.Change.Target, record.Change.Section, content))
	}

	if (r.language != "" && r.language != "en") || r.language == "" {
		return "你是一个经验去重专家。判断下载的共享经验是否与本地已有经验重复。\n\n" +
			"## 本地已有经验\n" + strings.Join(existingSummary, "\n") + "\n\n" +
			"## 下载的共享经验\n" + strings.Join(sharedSummary, "\n") + "\n\n" +
			`输出 JSON 数组：[{"record_id": "...", "decision": "keep|duplicate", "reason": "..."}]`
	}
	return "You are an experience deduplication expert.\n\n" +
		"## Existing Local Experiences\n" + strings.Join(existingSummary, "\n") + "\n\n" +
		"## Downloaded Shared Experiences\n" + strings.Join(sharedSummary, "\n") + "\n\n" +
		`Output JSON array: [{"record_id": "...", "decision": "keep|duplicate", "reason": "..."}]`
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// parseDuplicateCheckResponse 解析 LLM 返回的去重 JSON。
// 对齐 Python: SkillEvolutionSharingMixin._parse_duplicate_check_response(shared_records, raw_response)
func parseDuplicateCheckResponse(
	sharedRecords []checkpointing.EvolutionRecord,
	rawResponse string,
) []checkpointing.EvolutionRecord {
	re := regexp.MustCompile(`\[\s*\{.*?\}\s*\]`)
	jsonMatch := re.FindString(rawResponse)
	if jsonMatch == "" {
		return sharedRecords
	}

	var decisions []struct {
		RecordID string `json:"record_id"`
		Decision string `json:"decision"`
	}
	if err := json.Unmarshal([]byte(jsonMatch), &decisions); err != nil {
		return sharedRecords
	}

	decisionMap := make(map[string]string, len(decisions))
	for _, item := range decisions {
		decisionMap[item.RecordID] = item.Decision
	}

	var result []checkpointing.EvolutionRecord
	for _, record := range sharedRecords {
		if decisionMap[record.ID] == "duplicate" {
			continue
		}
		result = append(result, record)
	}
	return result
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
	return agentevolving.InferSkillFromTexts(skillNames, skillToolPayloads, texts)
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
		r.emitProgress("generating_updates", fmt.Sprintf("为 '%s' 生成演进记录", skillName), WithSkillName(skillName))
	}

	// Python: result = await self._stage_evolution_from_signals(skill_name=..., signals=..., messages=..., user_query=..., requires_approval=...)
	result, err := r.orchestrator.Evolve(
		ctx,
		skillName,
		convertSignalsToValues(signals),
		messages,
		userQuery,
		nil, // trajectory 轨迹
		requiresApproval,
		map[string]any{},
		nil, // source 来源
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
			r.emitProgress("auto_approved", fmt.Sprintf("'%s' 的经验记录已自动保存", skillName), WithSkillName(skillName), WithRequestID(request.RequestID))
		}
	}

	// 对齐 Python: finalize_staged_evolution_request(request, requires_approval, emit_fn=..., on_auto_approved=...)
	err = r.approvalRuntime.FinalizeStagedEvolutionRequest(
		request,
		requiresApproval,
		func(req *experience.ExperienceApprovalRequest) error {
			// 需要审批：发送审批事件
			if emitHostEvents {
				r.emitGeneratedRecords(cbc, skillName, req)
			}
			return nil
		},
		func(req *experience.ExperienceApprovalRequest) error {
			// 自动审批后触发 sharing
			r.sharingAfterAutoApproved(ctx, skillName, req)
			return nil
		},
	)
	if err != nil {
		return request, err
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
	r.uploadApprovedRecordsForSharing(ctx, skillName, messages, records)
}

// emitGeneratedRecords 缓存审批请求事件以供后续传递。
// 对齐 Python: SkillEvolutionRail._emit_generated_records(ctx, skill_name, approval_request)
func (r *SkillEvolutionRail) emitGeneratedRecords(cbc *agentinterfaces.AgentCallbackContext, skillName string, approvalRequest *experience.ExperienceApprovalRequest) {
	event := r.buildGeneratedRecordsEvent(skillName, approvalRequest)
	if event == nil {
		return
	}
	r.EmitHostEvent(event)
	r.emitProgress("approval_required", fmt.Sprintf("'%s' 的经验记录已就绪，等待审批", skillName), WithSkillName(skillName))
	if approvalRequest != nil {
		logger.Info(logComponent).
			Str("request_id", approvalRequest.RequestID).
			Int("record_count", approvalRequest.Proposal.RecordCount()).
			Str("skill", skillName).
			Msg("[SkillEvolutionRail] approval request cached")
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
	// Python: try: detected.extend(detector.detect_trajectory_signals(trajectory, messages=messages))
	// Python: except Exception as exc: logger.warning("...active request trajectory detection failed: %s", exc)
	// ConversationSignalDetector.DetectTrajectorySignals 是纯规则匹配，保留 recover 兜底防意外 panic。
	func() {
		defer func() {
			if rec := recover(); rec != nil {
				logger.Warn(logComponent).Any("exception", rec).
					Msg("[SkillEvolutionRail] active request trajectory detection exception, degrading and skipping")
			}
		}()
		detected = append(detected, detector.DetectTrajectorySignals(traj, messages)...)
	}()

	// Python: try/except — DetectUserIntent 失败时优雅降级
	userIntentSignals, intentErr := detector.DetectUserIntent(ctx, messages)
	if intentErr != nil {
		logger.Warn(logComponent).Err(intentErr).
			Msg("[SkillEvolutionRail] active request user-feedback detection failed")
	}
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

// extractToolContent 提取工具调用返回内容，复用包内共享实现。
// 对齐 Python: SkillEvolutionRail._extract_tool_content(inputs)
func (r *SkillEvolutionRail) extractToolContent(inputs *agentinterfaces.ToolCallInputs) string {
	return extractToolContentFromInputs(inputs)
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
	return r.disabledSkills
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

// resolveDownloadTopK 从共享配置解析下载 topK。
// 对齐 Python: SkillEvolutionSharingMixin._resolve_download_top_k(sharing_config)
func resolveDownloadTopK(sharingConfig map[string]any) int {
	rawTopK := defaultSharingDownloadTopK
	if sharingConfig != nil {
		if v, ok := sharingConfig["download_top_k"]; ok {
			switch n := v.(type) {
			case int:
				rawTopK = n
			case float64:
				rawTopK = int(n)
			case string:
				if parsed, err := strconv.Atoi(n); err == nil {
					rawTopK = parsed
				}
			}
		}
	}
	if rawTopK < 1 {
		rawTopK = 1
	}
	return rawTopK
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

	// 失败关键词检测：error 词用 hasRealError（模拟负向前瞻排除 "error = None"），
	// 其他关键词用 otherFailureRe（不需要负向前瞻）。
	// 对齐 Python: _FAILURE_KEYWORDS = re.compile(r"error(?!\s*=\s*None)|exception|...", re.IGNORECASE)

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
			// T-08: 先查 msg["name"]，为空时回退查 msg["tool_name"]
			if toolName == "" {
				toolName, _ = msg["tool_name"].(string)
			}
			if toolName == "" {
				if tcID, ok := msg["tool_call_id"].(string); ok {
					toolName = toolCallIDToName[tcID]
				}
			}
			// S-05: 失败关键词检测：error 词用 hasRealError（排除 "error = None"），其他用 otherFailureRe
			// 对齐 Python: _FAILURE_KEYWORDS.search(content_str)
			if (hasRealError(contentStr) || otherFailureRe.MatchString(contentStr)) && strings.TrimSpace(contentStr) != "" {
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

// hasRealError 判断内容中是否包含"真实的" error 关键词（排除 "error = None" 模式）。
// 对齐 Python: _FAILURE_KEYWORDS 中的负向前瞻 error(?!\s*=\s*None)。
// Go 正则不支持负向前瞻，因此用 FindAllStringIndex 逐匹配点检查后缀。
func hasRealError(content string) bool {
	matches := failureRe.FindAllStringIndex(content, -1)
	if len(matches) == 0 {
		return false
	}
	for _, loc := range matches {
		after := content[loc[1]:]
		if !errorNoneSuffixRe.MatchString(after) {
			return true
		}
	}
	return false
}

// ──────────────────────────── 15.24a Task 10：08-13 新版对照回收 ────────────────────────────
// 以下方法对齐 Python skill_evolution_rail.py 08-13 新版（D-15.21-1 回收）。
// ⤴️ 15.24b：RegisterEvolutionTools / RegisterEvolutionReviewAgent /
// IsEvolveReviewTaskAvailable / EnsureEvolveReviewTaskAvailable /
// PrepareEvolutionReviewScope / _build_user_evolution_request 链（依赖
// create_main_evolution_tools + build_evolve_review_command_prompt +
// agent_evolving/prompts/sections/evolution.py）。

// MakeEvolutionStore 构建演化服务使用的持久化 store。
// Python: _make_evolution_store（:321-323）。
func (r *SkillEvolutionRail) makeEvolutionStore() *checkpointing.EvolutionStore {
	return checkpointing.NewEvolutionStore(r.skillsDir)
}

// makeSkillOptimizer 构建在线演化 updater 使用的优化器。
// Python: _make_skill_optimizer（:325-342）。
// 偏差：Python 透传 two_stage（optimizer 双段管线 analyzer+formatter），
// Go optimizer 侧双段管线未移植（⤴️ 登记 spec §4）。
func (r *SkillEvolutionRail) makeSkillOptimizer(llmModel *llm.Model, model, language string) *skillopt.SkillExperienceOptimizer {
	return skillopt.NewSkillExperienceOptimizer(llmModel, model, language, r.generateRecordsLLMPolicy)
}

// makeExperienceManager 构建本 rail 主体的经验管理器。
// Python: _make_experience_manager（:343-354）。
func (r *SkillEvolutionRail) makeExperienceManager() (*experience.ExperienceManager, error) {
	return experience.NewExperienceManager(
		r.evolutionStore,
		r.scorer,
		r.SubjectKind(),
		r.language,
		r.skillOps,
		r.pendingApprovalSnapshots,
		r.pendingGovernance,
	)
}

// onlineRequestIDPrefix 返回在线演化 staged 请求的 request id 前缀（regular 为空）。
// Python: _online_request_id_prefix（:355-358）。
func (r *SkillEvolutionRail) onlineRequestIDPrefix() string { return "" }

// onlineStageSource 返回在线经验生成记录的 stage source。
// Python: _online_stage_source（:359-362）。
func (r *SkillEvolutionRail) onlineStageSource() string { return "experience_updater" }

// makeOnlineOrchestrator 构建编排器：把 optimizer 生成的经验 staged。
// Python: _make_online_orchestrator（:363-373）。
func (r *SkillEvolutionRail) makeOnlineOrchestrator() *experience.OnlineEvolutionOrchestrator {
	return experience.NewOnlineEvolutionOrchestrator(
		r.evolutionStore,
		r.onlineUpdater,
		r.manager,
		r.skillOps,
		r.onlineRequestIDPrefix(),
		r.onlineStageSource(),
	)
}

// SubjectKind 返回本 rail 的活动审查主体类别。
// Python: subject_kind property（:288-291）。
func (r *SkillEvolutionRail) SubjectKind() string { return r.subjectKindValue }

// SubjectLabel 返回活动审查主体的人类可读展示名。
// Python: subject_label property（:293-296）。
func (r *SkillEvolutionRail) SubjectLabel() string {
	if label, ok := subjectLabels[r.subjectKindValue]; ok {
		return label
	}
	return r.subjectKindValue
}

// resolveStoreSubjectPayload store 支持时从 store 解析主体信封。
// Python: _resolve_store_subject_payload（:297-314）。
func (r *SkillEvolutionRail) resolveStoreSubjectPayload(ctx context.Context, skillName string) (bool, map[string]string) {
	if r.evolutionStore == nil {
		return false, nil
	}
	payload := r.evolutionStore.ResolveSubjectPayload(ctx, skillName)
	return true, payload
}

// subjectPayload 构建命名技能式主体的规范信封。
// Python: _subject_payload（:314-320）。
func (r *SkillEvolutionRail) subjectPayload(ctx context.Context, skillName string) map[string]string {
	_, payload := r.resolveStoreSubjectPayload(ctx, skillName)
	if payload != nil {
		return payload
	}
	return map[string]string{"kind": "skill", "name": skillName}
}

// EvolutionTotalTimeoutSecs 返回后台演化超时预算。
// Python: evolution_total_timeout_secs property（:532-536）。
func (r *SkillEvolutionRail) EvolutionTotalTimeoutSecs() float64 { return r.evolutionTimeoutSec }

// TwoStage 返回经验生成是否使用双段管线。
// Python: two_stage property（:537-541）。
func (r *SkillEvolutionRail) TwoStage() bool { return r.twoStage }

// SignalTrigger 返回是否启用确定性信号驱动演化。
// Python: signal_trigger property（:699-703）。
func (r *SkillEvolutionRail) SignalTrigger() bool { return r.signalTrigger }

// SetSignalTrigger 设置是否启用确定性信号驱动演化。
// Python: signal_trigger.setter（:704-707）。
func (r *SkillEvolutionRail) SetSignalTrigger(v bool) { r.signalTrigger = v }

// ReviewTrigger 返回是否启用审查自检 follow-up。
// Python: review_trigger property（:708-712）。
func (r *SkillEvolutionRail) ReviewTrigger() bool { return r.reviewTrigger }

// SetReviewTrigger 设置是否启用审查自检 follow-up。
// Python: review_trigger.setter（:713-715）。
func (r *SkillEvolutionRail) SetReviewTrigger(v bool) { r.reviewTrigger = v }

// Init 绑定 agent 并注册 rail 持有的演化工具与稳定审查子 agent。
// Python: init（:374-381）。
// ⤴️ 15.24b/24c：_register_evolution_tools / _register_evolution_review_agent
// 已随 15.24c 落地（CreateMainEvolutionTools + reviewsubagent 装配半区）。
func (r *SkillEvolutionRail) Init(ctx context.Context, agent agentinterfaces.BaseAgent) error {
	r.agentRef = agent
	// Python: self._system_prompt_builder = getattr(agent, "system_prompt_builder")
	if agent != nil {
		r.systemPromptBuilder = agent.SystemPromptBuilder()
	}
	// Python: self._evolution_tools = self._register_evolution_tools(agent)
	//        self._register_evolution_review_agent(agent)
	if agent != nil {
		if tools, err := r.registerEvolutionTools(agent); err == nil {
			r.evolutionTools = tools
		} else {
			return err
		}
		r.registerEvolutionReviewAgent(agent)
	}
	return nil
}

// Uninit 注销 rail 持有的演化工具与审查子 agent。
// Python: uninit（:382-394）。
func (r *SkillEvolutionRail) Uninit(agent agentinterfaces.BaseAgent) error {
	// Python: self._unregister_runtime_tools(agent, self._evolution_tools)
	r.unregisterRuntimeTools(agent, r.evolutionTools)
	r.evolutionTools = nil
	r.unregisterEvolutionReviewAgent(agent)
	if r.systemPromptBuilder != nil {
		// Python: remove_section(EVOLUTION_PROTOCOL) + remove_section(EVOLUTION_TEAM_PROTOCOL)
		r.systemPromptBuilder.RemoveSection(sections.SectionNameEvolutionProtocol)
		r.systemPromptBuilder.RemoveSection(sections.SectionNameEvolutionTeamProtocol)
	}
	r.systemPromptBuilder = nil
	r.agentRef = nil
	return nil
}

// registerEvolutionTools 注册 rail 持有的活动审查工具到 agent 运行时。
// Python: _register_evolution_tools（skill_evolution_rail.py:403-416）。
func (r *SkillEvolutionRail) registerEvolutionTools(agent agentinterfaces.BaseAgent) ([]toolpkg.Tool, error) {
	var agentID string
	if card := agent.Card(); card != nil {
		agentID = card.ID
	}
	evolutionTools, err := agenttools.CreateMainEvolutionTools(
		r.manager.QueryService(),
		r.manager.SubmissionService(),
		r.PrepareEvolutionReviewScope,
		r.reviewRuntime,
		r.language,
		agentID,
		deepAgentOf(agent),
	)
	if err != nil {
		return nil, err
	}
	// Python: self._register_runtime_tools(agent, evolution_tools)
	// Go 落点 = ability_manager AddAbility（card+实例成对注册）。
	am := agent.AbilityManager()
	for _, t := range evolutionTools {
		if card := t.Card(); card != nil {
			am.AddAbility(card, t)
		}
	}
	return evolutionTools, nil
}

// unregisterRuntimeTools 从 agent 运行时注销 rail 持有工具。
// Python: _unregister_runtime_tools（按工具名移除）。
func (r *SkillEvolutionRail) unregisterRuntimeTools(agent agentinterfaces.BaseAgent, tools []toolpkg.Tool) {
	if agent == nil || len(tools) == 0 {
		return
	}
	if am := agent.AbilityManager(); am != nil {
		for _, t := range tools {
			if card := t.Card(); card != nil {
				am.Remove(card.Name)
			}
		}
	}
}

// EnsureEvolutionReviewAgent 导出包装：热重载后恢复审查子 agent 配置。
// Python 侧 host adapter 直接调 rail._register_evolution_review_agent(instance)
// （interface_deep.py:10688-10694）；Go 跨包调用需导出（可见性规则，归属不变）。
func (r *SkillEvolutionRail) EnsureEvolutionReviewAgent(agent agentinterfaces.BaseAgent) {
	r.registerEvolutionReviewAgent(agent)
}

// registerEvolutionReviewAgent 在 agent.deep_config 上确保稳定审查子 agent 配置。
// Python: _register_evolution_review_agent（skill_evolution_rail.py:417-434）。
func (r *SkillEvolutionRail) registerEvolutionReviewAgent(agent agentinterfaces.BaseAgent) {
	deep := deepAgentOf(agent)
	if deep == nil {
		return
	}
	deepConfig := deep.DeepConfig()
	if deepConfig == nil {
		return
	}
	var agentID string
	if card := agent.Card(); card != nil {
		agentID = card.ID
	}
	// Python: build_evolution_review_agent_config(runtime=..., query_service=...,
	// store=..., model=None, language=..., max_iterations=..., agent_id=...)
	config, err := reviewsubagent.BuildEvolutionReviewAgentConfig(
		r.reviewRuntime,
		nil,
		r.manager.QueryService(),
		r.evolutionStore,
		r.language,
		r.reviewAgentMaxIterations,
		agentID,
	)
	if err != nil {
		logger.Error(logComponent).Err(err).Msg("build evolution review agent config failed")
		return
	}
	// Python: deep_config.subagents = ensure_evolution_review_agent_config(subagents, config)
	existing := make([]*harnessschema.SubAgentConfig, 0, len(deepConfig.Subagents))
	for _, spec := range deepConfig.Subagents {
		if cfg, ok := spec.(*harnessschema.SubAgentConfig); ok {
			existing = append(existing, cfg)
		} else {
			logger.Warn(logComponent).Msg("skip non SubAgentConfig spec in deep_config.subagents")
		}
	}
	merged := reviewsubagent.EnsureEvolutionReviewAgentConfig(existing, config)
	specs := make([]harnessschema.SubagentSpec, 0, len(merged))
	for _, cfg := range merged {
		specs = append(specs, cfg)
	}
	deepConfig.Subagents = specs
}

// deepAgentOf 断言 DeepAgent 能力面（审查子 agent 挂载需要 deep_config）。
// Python: getattr(agent, "deep_config", None) 鸭子式判空。
func deepAgentOf(agent agentinterfaces.BaseAgent) harnessinterfaces.DeepAgentInterface {
	if deep, ok := agent.(harnessinterfaces.DeepAgentInterface); ok {
		return deep
	}
	return nil
}

// unregisterEvolutionReviewAgent 从 agent.deep_config 摘除稳定审查子 agent 配置。
// Python: _unregister_evolution_review_agent（:395-401）。
func (r *SkillEvolutionRail) unregisterEvolutionReviewAgent(agent agentinterfaces.BaseAgent) {
	// Python: deep_config.subagents = remove_evolution_review_agent_config(subagents)
	deep := deepAgentOf(agent)
	if deep == nil {
		return
	}
	deepConfig := deep.DeepConfig()
	if deepConfig == nil {
		return
	}
	existing := make([]*harnessschema.SubAgentConfig, 0, len(deepConfig.Subagents))
	for _, spec := range deepConfig.Subagents {
		if cfg, ok := spec.(*harnessschema.SubAgentConfig); ok {
			existing = append(existing, cfg)
		}
	}
	removed := reviewsubagent.RemoveEvolutionReviewAgentConfig(existing)
	specs := make([]harnessschema.SubagentSpec, 0, len(removed))
	for _, cfg := range removed {
		specs = append(specs, cfg)
	}
	deepConfig.Subagents = specs
}

// BeforeModelCall 注入稳定的 Skill 演进协议段（每次模型调用前幂等 add_section）。
// Python: SkillEvolutionRail.before_model_call（skill_evolution_rail.py:733-740）：
// builder 取 inputs.system_prompt_builder 回落 agent.system_prompt_builder；
// language 取 builder.language 回落 self._language。
func (r *SkillEvolutionRail) BeforeModelCall(ctx context.Context, cbc *agentinterfaces.AgentCallbackContext) error {
	_ = ctx
	builder := r.builderFromContext(cbc)
	if builder == nil {
		return nil
	}
	language := builder.Language()
	if language == "" {
		language = r.language
	}
	builder.AddSection(sections.BuildEvolutionProtocolSection(language))
	return nil
}

// GetCallbacks 覆写基座回调表，追加 before_model_call 注入。
// Python: SkillEvolutionRail 的 before_model_call 经 rail 回调注册链挂接。
func (r *SkillEvolutionRail) GetCallbacks() map[agentinterfaces.AgentCallbackEvent]cb.PerAgentCallbackFunc {
	callbacks := r.EvolutionRail.GetCallbacks()
	callbacks[agentinterfaces.CallbackBeforeModelCall] = func(ctx context.Context, railCtx any) error {
		return r.BeforeModelCall(ctx, railCtx.(*agentinterfaces.AgentCallbackContext))
	}
	return callbacks
}

// builderFromContext 按脚本顺序取提示词构建器：inputs → agent（均缺则 nil）。
// Python: getattr(getattr(ctx, "inputs", None), "system_prompt_builder", None)
// 回落 getattr(getattr(ctx, "agent", None), "system_prompt_builder", None)。
// 偏差（语言层）：Go EventInputs 无 system_prompt_builder 属性，恒走 agent 分支。
func (r *SkillEvolutionRail) builderFromContext(cbc *agentinterfaces.AgentCallbackContext) saprompt.SystemPromptBuilderInterface {
	if cbc == nil {
		return nil
	}
	if cbc.Agent() != nil {
		if builder := cbc.Agent().SystemPromptBuilder(); builder != nil {
			return builder
		}
	}
	return r.systemPromptBuilder
}

// PrepareEvolutionReviewScope 以当前干净轨迹窗口创建审查 scope。
// Python: _prepare_evolution_review_scope（skill_evolution_rail.py:455-493）。
// 三条 RuntimeError 分支 + 成功路径置 skipSignalTriggerThisInvoke=true。
func (r *SkillEvolutionRail) PrepareEvolutionReviewScope(source string, subject map[string]any, sessionID, memberID, teamID, userIntent string) (review.EvolutionReviewLaunch, error) {
	capture := r.resolveCaptureByScope(sessionID, memberID, teamID)
	if capture == nil {
		// Python: with self._subscription_lock: has_active_capture = bool(self._active_captures)
		r.scopeMu.Lock()
		hasActiveCapture := len(r.activeCaptures) > 0
		r.scopeMu.Unlock()
		if hasActiveCapture {
			return review.EvolutionReviewLaunch{}, fmt.Errorf("review session does not match the active trajectory scope")
		}
		return review.EvolutionReviewLaunch{}, fmt.Errorf("evolution review must be prepared during an active invoke")
	}

	// Python: capture 命中后用 capture 的定位符取轨迹（session/member/team 以采集为准）
	traj, err := r.GetTrajectory(capture.sessionID, capture.memberID, capture.teamID)
	if err != nil {
		return review.EvolutionReviewLaunch{}, err
	}
	if traj == nil {
		return review.EvolutionReviewLaunch{}, fmt.Errorf("no clean trajectory is available for evolution review")
	}

	scope, err := r.reviewRuntime.CreateScope(source, subject, capture.sessionID, userIntent, traj)
	if err != nil {
		return review.EvolutionReviewLaunch{}, err
	}
	// Python: self._skip_signal_trigger_this_invoke = True
	r.skipSignalTriggerThisInvoke = true
	return scope, nil
}

// isEvolveReviewTaskAvailable 判定 agent 能力面是否已注册 evolve_review_task。
// Python: _is_evolve_review_task_available（:436-443）。
func (r *SkillEvolutionRail) isEvolveReviewTaskAvailable(agent agentinterfaces.BaseAgent) bool {
	if agent == nil {
		return false
	}
	am := agent.AbilityManager()
	if am == nil {
		return false
	}
	return am.Get("evolve_review_task") != nil
}

// EnsureEvolveReviewTaskAvailable 避免返回引用缺失工具的 follow-up 提示词。
// Python: _ensure_evolve_review_task_available（:445-452）。
func (r *SkillEvolutionRail) EnsureEvolveReviewTaskAvailable() error {
	if r.agentRef == nil {
		return nil
	}
	if r.isEvolveReviewTaskAvailable(r.agentRef) {
		return nil
	}
	return fmt.Errorf(
		"SkillEvolutionRail active review requires evolve_review_task to be registered. " +
			"Ensure SkillEvolutionRail.init() registered its rail-owned active review tools.")
}

// isBackgroundRun 判定是否后台运行（心跳/定时），后台轮不做信号触发与 follow-up。
// Python: _is_background_run（:631-648）。
func (r *SkillEvolutionRail) isBackgroundRun(cbc *agentinterfaces.AgentCallbackContext) bool {
	if cbc == nil || cbc.Inputs() == nil {
		return false
	}
	// Python: inputs.is_heartbeat()/is_cron() + run_kind + conversation_id 前缀。
	// Go 侧 RunKind 按事件类型分别落在 InvokeInputs / TaskIterationInputs。
	switch inputs := cbc.Inputs().(type) {
	case *agentinterfaces.InvokeInputs:
		switch inputs.RunKind {
		case agentinterfaces.RunKindHeartbeat, agentinterfaces.RunKindCron:
			return true
		}
		return strings.HasPrefix(inputs.ConversationID, "heartbeat") ||
			strings.HasPrefix(inputs.ConversationID, "cron")
	case *agentinterfaces.TaskIterationInputs:
		switch inputs.RunKind {
		case agentinterfaces.RunKindHeartbeat, agentinterfaces.RunKindCron:
			return true
		}
		return strings.HasPrefix(inputs.ConversationID, "heartbeat") ||
			strings.HasPrefix(inputs.ConversationID, "cron")
	default:
		return false
	}
}

// taskIterationFollowupBlocked 判定任务循环 follow-up 是否必须抑制。
// Python: _task_iteration_followup_blocked（:623-630）。
func (r *SkillEvolutionRail) taskIterationFollowupBlocked(cbc *agentinterfaces.AgentCallbackContext) bool {
	if r.isBackgroundRun(cbc) {
		return true
	}
	if inputs, ok := cbc.Inputs().(*agentinterfaces.TaskIterationInputs); ok && inputs.IsFollowUp {
		return true
	}
	return false
}

// enqueueTaskIterationFollowup 从常规任务循环迭代排入 follow-up。
// Python: _enqueue_task_iteration_followup（:600-622）。
func (r *SkillEvolutionRail) enqueueTaskIterationFollowup(ctx context.Context, cbc *agentinterfaces.AgentCallbackContext, prompt, logPrefix string) bool {
	if r.taskIterationFollowupBlocked(cbc) {
		return false
	}
	if cbc == nil || cbc.Agent() == nil {
		logger.Warn(logComponent).
			Str("log_prefix", logPrefix).
			Msg("[SkillEvolutionRail] follow-up dropped: no agent available")
		return false
	}
	controller := getLoopController(cbc.Agent())
	if controller == nil {
		logger.Warn(logComponent).
			Str("log_prefix", logPrefix).
			Msg("[SkillEvolutionRail] follow-up dropped: no TaskLoopController available")
		return false
	}
	controller.EnqueueFollowUp(prompt)
	return true
}

// buildReviewFollowupPrompt 构建活动模糊审查自检 follow-up 提示词。
// Python: _build_review_followup_prompt（:649-654）。
func (r *SkillEvolutionRail) buildReviewFollowupPrompt() string {
	prompt := reviewFollowupPromptCN
	if r.language == "en" {
		prompt = reviewFollowupPromptEN
	}
	return wrapReviewFollowupPrompt(prompt)
}

// wrapReviewFollowupPrompt 用自检标签包裹提示词。
// Python: _wrap_review_followup_prompt（:655-658）。
func wrapReviewFollowupPrompt(prompt string) string {
	return "<" + autoSkillEvolutionFollowUpTag + ">\n" + prompt + "\n</" + autoSkillEvolutionFollowUpTag + ">"
}

// trackPresentedFromToolCall 从 detail 读或 SKILL.md 索引读记录已展示经验 ID。
// Python: _track_presented_from_tool_call（:786-821）。
func (r *SkillEvolutionRail) trackPresentedFromToolCall(ctx context.Context, cbc *agentinterfaces.AgentCallbackContext, inputs *agentinterfaces.ToolCallInputs) error {
	sessionID := ""
	if cbc != nil && cbc.Session() != nil {
		sessionID = cbc.Session().GetSessionID()
	}
	content := r.extractToolContent(inputs)

	// 来源 1：evolution/*.md detail 文件读（含具体记录标题）
	if detailSkill := r.detectExperienceDetailRead(inputs); detailSkill != "" {
		recordIDs := r.extractPresentedRecordIDs(content)
		if len(recordIDs) == 0 {
			return nil
		}
		r.experienceTracker.RecordPresentedRecords(ctx, sessionID, detailSkill, "", recordIDs)
		return nil
	}

	// 来源 2：SKILL.md / skill_tool 默认入口读（含 Evolution Index 摘要）
	indexSkill := r.detectSkillIndexRead(inputs)
	if indexSkill == "" {
		return nil
	}
	recordIDs := r.extractIndexRecordIDs(content)
	if len(recordIDs) == 0 {
		return nil
	}
	r.experienceTracker.RecordPresentedRecords(ctx, sessionID, indexSkill, "", recordIDs)
	return nil
}

// evaluatePresentedEntries 用 after_invoke 时重建的对话片段评估已展示经验。
// Python: _evaluate_presented_entries（:822-833）。
func (r *SkillEvolutionRail) evaluatePresentedEntries(ctx context.Context, presentedEntries []experience.PresentedRecordEntry, messages []map[string]any) {
	r.experienceTracker.EvaluatePresented(ctx, presentedEntries)
}

// detectSignals 规则检测新信号并按指纹去重。
// Python: _detect_signals（:1252-1278）。
func (r *SkillEvolutionRail) detectSignals(ctx context.Context, messages []map[string]any, skillNames []string) []*signal.EvolutionSignal {
	existingSkills := map[string]bool{}
	for _, name := range skillNames {
		if r.evolutionStore.SkillExists(ctx, name) {
			existingSkills[name] = true
		}
	}
	detector := signal.NewConversationSignalDetector(signal.WithExistingSkills(existingSkills))
	detected := detector.Detect(messages)

	var newSignals []*signal.EvolutionSignal
	for _, sig := range detected {
		fp := signal.MakeSignalFingerprint(sig)
		if r.processedSignalKeys[fp] {
			continue
		}
		r.processedSignalKeys[fp] = true
		newSignals = append(newSignals, sig)
	}
	if len(r.processedSignalKeys) > maxProcessedSignalKeys {
		r.processedSignalKeys = make(map[[4]string]bool)
	}
	if len(newSignals) > 0 {
		logger.Info(logComponent).
			Int("new_signals", len(newSignals)).
			Int("filtered", len(detected)-len(newSignals)).
			Msg("[SkillEvolutionRail] detected new signal(s)")
	}
	return newSignals
}

// normalizeSkillRelativeFilePath 主技能入口文件的规范相对路径（SKILL.md）。
// Python: _normalize_skill_relative_file_path（:114-126，模块级）。
func normalizeSkillRelativeFilePath(relativeFilePath string) string {
	raw := strings.TrimSpace(relativeFilePath)
	if raw == "" {
		return "SKILL.md"
	}
	normalized := strings.TrimPrefix(strings.ReplaceAll(raw, "\\", "/"), "./")
	prefix, base := "", normalized
	if idx := strings.LastIndex(normalized, "/"); idx >= 0 {
		prefix, base = normalized[:idx], normalized[idx+1:]
	}
	if !strings.Contains(base, ".") && strings.EqualFold(base, "skill") {
		if prefix != "" {
			return prefix + "/SKILL.md"
		}
		return "SKILL.md"
	}
	return raw
}

// utcnowISO 当前 UTC 时刻 ISO 形态。
// Python: _utcnow_iso（:137-139，模块级）。
func utcnowISO() string {
	return time.Now().UTC().Format("2006-01-02T15:04:05.000000000Z07:00")
}

// isSkillMDPath 判定相对路径是否指向技能主入口 SKILL.md。
// Python: _is_skill_md_path（:1659-1664）。
func isSkillMDPath(relativePath string) bool {
	normalized := normalizeSkillRelativeFilePath(relativePath)
	norm := strings.ToLower(strings.TrimPrefix(strings.ReplaceAll(normalized, "\\", "/"), "./"))
	return norm == "skill.md" || strings.HasSuffix(norm, "/skill.md")
}

// isSkillMDFileReadTool 工具是否为可能按路径读到 SKILL.md 的已知文件读工具。
// Python: _is_skill_md_file_read_tool（:1692-1696）。
func isSkillMDFileReadTool(toolName string) bool {
	return skillMDFileReadTools[strings.ToLower(toolName)]
}

// extractFilePath 从工具参数取 file_path。
// Python: _extract_file_path（:1581-1585）。
func extractFilePath(toolArgs any) string {
	args := extractToolArgs(toolArgs)
	if v, ok := args["file_path"]; ok && v != nil && fmt.Sprintf("%v", v) != "" {
		return fmt.Sprintf("%v", v)
	}
	return ""
}

// detectSkillIndexRead 工具调用加载 SKILL.md（Evolution Index）时返回技能名。
// Python: _detect_skill_index_read（:1607-1629）。
func (r *SkillEvolutionRail) detectSkillIndexRead(inputs *agentinterfaces.ToolCallInputs) string {
	toolName := inputs.ToolName
	args := extractToolArgs(inputs.ToolArgs)

	if toolName == "skill_tool" {
		skillName := strings.TrimSpace(fmt.Sprintf("%v", args["skill_name"]))
		relativePath := strings.TrimSpace(fmt.Sprintf("%v", args["relative_file_path"]))
		if relativePath == "" || relativePath == "<nil>" {
			relativePath = "SKILL.md"
		}
		if skillName != "" && isSkillMDPath(relativePath) {
			return skillName
		}
		return ""
	}

	if !isSkillMDFileReadTool(toolName) {
		return ""
	}
	filePath := strings.TrimSpace(extractFilePath(args))
	if filePath == "" {
		return ""
	}
	matched := skillMDRE.FindStringSubmatch(filePath)
	if matched == nil {
		return ""
	}
	return matched[1]
}

// extractIndexRecordIDs 从 SKILL.md Evolution Index 摘要/链接提取经验 ID。
// Python: _extract_index_record_ids（:1541-1564）。
func (r *SkillEvolutionRail) extractIndexRecordIDs(content string) []string {
	if content == "" {
		return nil
	}
	section := content
	start := strings.Index(content, "<!-- evolution-index-start -->")
	end := strings.Index(content, "<!-- evolution-index-end -->")
	if start != -1 && end != -1 && end > start {
		section = content[start:end]
	} else if idx := strings.Index(content, "Evolution Experiences"); idx != -1 {
		section = content[idx:]
	}

	seen := map[string]bool{}
	var recordIDs []string
	for _, match := range experienceIndexIDRE.FindAllStringSubmatch(section, -1) {
		recordID := match[1]
		if seen[recordID] {
			continue
		}
		seen[recordID] = true
		recordIDs = append(recordIDs, recordID)
	}
	return recordIDs
}

// findSkillLoadAnchor 返回最近一次加载 skill_name 的 SKILL.md 的消息下标（-1 无）。
// Python: _find_skill_load_anchor（:1665-1691）。
func (r *SkillEvolutionRail) findSkillLoadAnchor(messages []map[string]any, skillName string) int {
	anchor := -1
	for index, msg := range messages {
		if role := fmt.Sprintf("%v", msg["role"]); role != "assistant" {
			continue
		}
		for _, toolCall := range toolCallsOf(msg) {
			tool := strings.ToLower(fmt.Sprintf("%v", trajectory.ToolCallName(toolCall)))
			arguments := ""
			if v := trajectory.ToolCallArguments(toolCall); v != nil {
				arguments = fmt.Sprintf("%v", v)
			}
			if tool == "skill_tool" {
				args := extractToolArgs(arguments)
				if strings.TrimSpace(fmt.Sprintf("%v", args["skill_name"])) != skillName {
					continue
				}
				rel := normalizeSkillRelativeFilePath(fmt.Sprintf("%v", args["relative_file_path"]))
				if isSkillMDPath(rel) {
					anchor = index
				}
			} else if isSkillMDFileReadTool(tool) {
				filePath := extractFilePath(arguments)
				matched := skillMDRE.FindStringSubmatch(filePath)
				if matched != nil && matched[1] == skillName {
					anchor = index
				}
			}
		}
	}
	return anchor
}

// formatMessagesSnippet 把对话消息格式化为评分器友好的片段。
// Python: _format_messages_snippet（:1697-1722）。
func (r *SkillEvolutionRail) formatMessagesSnippet(messages []map[string]any, start int) string {
	var lines []string
	window := messages[start:]
	if len(window) > evalSnippetMaxMessages {
		window = window[len(window)-evalSnippetMaxMessages:]
	}
	for _, msg := range window {
		role := fmt.Sprintf("%v", msg["role"])
		if role == "" || role == "<nil>" {
			role = "unknown"
		}
		content := truncateString(fmt.Sprintf("%v", msg["content"]), evalSnippetPostPresentMaxChars)
		if content != "" && content != "<nil>" {
			lines = append(lines, fmt.Sprintf("[%s] %s", role, content))
		}
		if role == "assistant" {
			for _, toolCall := range toolCallsOf(msg) {
				tool := fmt.Sprintf("%v", trajectory.ToolCallName(toolCall))
				arguments := ""
				if v := trajectory.ToolCallArguments(toolCall); v != nil {
					arguments = fmt.Sprintf("%v", v)
				}
				args := truncateString(arguments, evalSnippetPostPresentMaxChars)
				if tool != "" && tool != "<nil>" {
					lines = append(lines, fmt.Sprintf("[assistant/tool_call] %s %s", tool, args))
				}
			}
		}
	}
	return strings.Join(lines, "\n")
}

// buildEvaluationSnippet 构建展示后片段：从最后一次 SKILL.md 加载到本轮结束。
// Python: _build_evaluation_snippet（:1723-1731）。
func (r *SkillEvolutionRail) buildEvaluationSnippet(messages []map[string]any, skillName string) string {
	if len(messages) == 0 {
		return ""
	}
	anchor := r.findSkillLoadAnchor(messages, skillName)
	start := anchor
	if start < 0 {
		start = len(messages) - evalSnippetMaxMessages
		if start < 0 {
			start = 0
		}
	}
	return r.formatMessagesSnippet(messages, start)
}

// toolCallsOf 从消息提取 tool_calls 列表（signal.getToolCalls 的包内等价）。
func toolCallsOf(m map[string]any) []map[string]any {
	if m == nil {
		return nil
	}
	switch v := m["tool_calls"].(type) {
	case []map[string]any:
		return v
	case []any:
		result := make([]map[string]any, 0, len(v))
		for _, item := range v {
			if mm, ok := item.(map[string]any); ok {
				result = append(result, mm)
			}
		}
		return result
	default:
		return nil
	}
}

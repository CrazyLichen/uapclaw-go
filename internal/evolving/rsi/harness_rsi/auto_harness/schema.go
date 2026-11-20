package autoharness

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	llm "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	"github.com/uapclaw/uapclaw-go/internal/evolving/rsi/harness_rsi/auto_harness/resource"
	"gopkg.in/yaml.v3"
)

// logComponent 组件级日志常量（agentcore 域）
const logComponent = logger.ComponentAgentCore

// ──────────────────────────── 常量 ────────────────────────────

const (
	// PipelinePreferenceAuto 用户侧流水线偏好 "auto"（按目标信号选择）
	PipelinePreferenceAuto = "auto"
	// defaultRepoURL 默认远端仓库 URL（Phase 1 兼容）
	defaultRepoURL = "https://gitcode.com/openJiuwen/agent-core.git"
)

// pipelinePreferenceAliases 用户侧流水线偏好别名表
var pipelinePreferenceAliases = map[string]string{
	"":                          PipelinePreferenceAuto,
	PipelinePreferenceAuto:      PipelinePreferenceAuto,
	"meta":                      MetaEvolvePipeline,
	MetaEvolvePipeline:          MetaEvolvePipeline,
	"extended":                  ExtendedEvolvePipeline,
	ExtendedEvolvePipeline:      ExtendedEvolvePipeline,
	"extended_harness_pipeline": ExtendedEvolvePipeline,
	"pr_pipeline":               MetaEvolvePipeline,
}

// ──────────────────────────── 枚举 ────────────────────────────

// TaskStatus 优化任务状态（对齐 TaskStatus(str, Enum)）
type TaskStatus string

// TaskStatus 状态成员
const (
	TaskStatusPending  TaskStatus = "pending"
	TaskStatusRunning  TaskStatus = "running"
	TaskStatusSuccess  TaskStatus = "success"
	TaskStatusFailed   TaskStatus = "failed"
	TaskStatusTimeout  TaskStatus = "timeout"
	TaskStatusReverted TaskStatus = "reverted"
)

// ExperienceType 经验记录类型（对齐 ExperienceType(str, Enum)）
type ExperienceType string

// ExperienceType 类型成员
const (
	ExperienceTypeOptimization ExperienceType = "optimization"
	ExperienceTypeFailure      ExperienceType = "failure"
	ExperienceTypeInsight      ExperienceType = "insight"
)

// StageSlot 各流水线共享的规范 stage 阶段名（对齐 StageSlot(str, Enum)）
type StageSlot string

// StageSlot 阶段成员
const (
	StageSlotAssess    StageSlot = "assess"
	StageSlotPlan      StageSlot = "plan"
	StageSlotImplement StageSlot = "implement"
	StageSlotVerify    StageSlot = "verify"
	StageSlotActivate  StageSlot = "activate"
	StageSlotCommit    StageSlot = "commit"
	StageSlotPublish   StageSlot = "publish"
	StageSlotLearnings StageSlot = "learnings"
)

// ──────────────────────────── 结构体 ────────────────────────────

// Gap 竞品差距（对齐 dataclass Gap）
type Gap struct {
	ID                string   `yaml:"id" json:"id"`
	Competitor        string   `yaml:"competitor" json:"competitor"`
	Feature           string   `yaml:"feature" json:"feature"`
	CurrentState      string   `yaml:"current_state" json:"current_state"`
	GapDescription    string   `yaml:"gap_description" json:"gap_description"`
	Impact            float64  `yaml:"impact" json:"impact"`
	Feasibility       float64  `yaml:"feasibility" json:"feasibility"`
	SuggestedApproach string   `yaml:"suggested_approach" json:"suggested_approach"`
	TargetFiles       []string `yaml:"target_files" json:"target_files"`
}

// Priority impact x feasibility（对齐 property priority）
func (g *Gap) Priority() float64 {
	return g.Impact * g.Feasibility
}

// OptimizationTask 单个优化任务（对齐 dataclass OptimizationTask）
type OptimizationTask struct {
	Topic          string     `yaml:"topic" json:"topic"`
	Description    string     `yaml:"description" json:"description"`
	Files          []string   `yaml:"files" json:"files"`
	IssueRef       *string    `yaml:"issue_ref" json:"issue_ref"`
	ExpectedEffect string     `yaml:"expected_effect" json:"expected_effect"`
	PipelineName   string     `yaml:"pipeline_name" json:"pipeline_name"`
	Status         TaskStatus `yaml:"status" json:"status"`
}

// Experience 经验库记录（对齐 dataclass Experience）
type Experience struct {
	Type            ExperienceType `yaml:"type" json:"type"`
	Topic           string         `yaml:"topic" json:"topic"`
	Summary         string         `yaml:"summary" json:"summary"`
	Outcome         string         `yaml:"outcome" json:"outcome"`
	Details         string         `yaml:"details" json:"details"`
	PRURL           string         `yaml:"pr_url" json:"pr_url"`
	FilesChanged    []string       `yaml:"files_changed" json:"files_changed"`
	Signal          string         `yaml:"signal" json:"signal"`
	Strategy        string         `yaml:"strategy" json:"strategy"`
	CausalChain     string         `yaml:"causal_chain" json:"causal_chain"`
	SignalFrequency int            `yaml:"signal_frequency" json:"signal_frequency"`
	// ID 12 位随机 hex（对齐 default_factory=lambda: uuid.uuid4().hex[:12]）
	ID string `yaml:"id" json:"id"`
	// Timestamp 创建时间戳（秒，float；对齐 default_factory=time.time）
	Timestamp float64 `yaml:"timestamp" json:"timestamp"`
}

// ResearchContext Research 阶段收集的上下文（对齐 dataclass ResearchContext）
type ResearchContext struct {
	Experiences []Experience      `yaml:"experiences" json:"experiences"`
	SourceFiles map[string]string `yaml:"source_files" json:"source_files"`
	GapReport   *string           `yaml:"gap_report" json:"gap_report"`
}

// CycleResult 单个 task 的执行结果（对齐 dataclass CycleResult）
type CycleResult struct {
	Success  bool   `yaml:"success" json:"success"`
	Summary  string `yaml:"summary" json:"summary"`
	PRURL    string `yaml:"pr_url" json:"pr_url"`
	Error    string `yaml:"error" json:"error"`
	Reverted bool   `yaml:"reverted" json:"reverted"`
	ErrorLog string `yaml:"error_log" json:"error_log"`
}

// AssessmentArtifact Assess 阶段的结构化产物（对齐 dataclass AssessmentArtifact）
type AssessmentArtifact struct {
	Report string `yaml:"report" json:"report"`
}

// TaskPlanArtifact Plan 阶段的结构化产物（对齐 dataclass TaskPlanArtifact）
type TaskPlanArtifact struct {
	Tasks   []OptimizationTask `yaml:"tasks" json:"tasks"`
	RawPlan string             `yaml:"raw_plan" json:"raw_plan"`
}

// PipelineSelectionArtifact select_pipeline 阶段的结构化产物（对齐 dataclass PipelineSelectionArtifact）
type PipelineSelectionArtifact struct {
	PipelineName     string   `yaml:"pipeline_name" json:"pipeline_name"`
	Reason           string   `yaml:"reason" json:"reason"`
	Alternatives     []string `yaml:"alternatives" json:"alternatives"`
	Confidence       float64  `yaml:"confidence" json:"confidence"`
	RiskLevel        string   `yaml:"risk_level" json:"risk_level"`
	RequiredInputs   []string `yaml:"required_inputs" json:"required_inputs"`
	FallbackPipeline string   `yaml:"fallback_pipeline" json:"fallback_pipeline"`
}

// GapAnalysisArtifact extended evolve 流水线的差距分析输出（对齐 dataclass GapAnalysisArtifact）
type GapAnalysisArtifact struct {
	Gaps              []Gap  `yaml:"gaps" json:"gaps"`
	CompetitorSummary string `yaml:"competitor_summary" json:"competitor_summary"`
	RawAnalysis       string `yaml:"raw_analysis" json:"raw_analysis"`
}

// ExtensionDesign 一个扩展设计候选（对齐 dataclass ExtensionDesign）
type ExtensionDesign struct {
	GapID              string            `yaml:"gap_id" json:"gap_id"`
	ExtensionName      string            `yaml:"extension_name" json:"extension_name"`
	Kind               string            `yaml:"kind" json:"kind"`
	DependsOn          []string          `yaml:"depends_on" json:"depends_on"`
	AppliesTo          []string          `yaml:"applies_to" json:"applies_to"`
	Components         []string          `yaml:"components" json:"components"`
	FilePlan           map[string]string `yaml:"file_plan" json:"file_plan"`
	HarnessConfigPatch map[string]any    `yaml:"harness_config_patch" json:"harness_config_patch"`
	SkillSource        string            `yaml:"skill_source" json:"skill_source"`
}

// ExtensionDesignArtifact runtime extension 生成的设计输出（对齐 dataclass ExtensionDesignArtifact）
type ExtensionDesignArtifact struct {
	Designs     []ExtensionDesign `yaml:"designs" json:"designs"`
	PackageName string            `yaml:"package_name" json:"package_name"`
}

// ExtensionBuildArtifact task worktree 内验证通过的扩展构建输出（对齐 dataclass ExtensionBuildArtifact）
type ExtensionBuildArtifact struct {
	ExtensionName string `yaml:"extension_name" json:"extension_name"`
	ExtensionRoot string `yaml:"extension_root" json:"extension_root"`
	ConfigPath    string `yaml:"config_path" json:"config_path"`
}

// RuntimeExtensionArtifact session 内晋升的运行时扩展（对齐 dataclass RuntimeExtensionArtifact）
type RuntimeExtensionArtifact struct {
	ExtensionName string `yaml:"extension_name" json:"extension_name"`
	RuntimePath   string `yaml:"runtime_path" json:"runtime_path"`
	ConfigPath    string `yaml:"config_path" json:"config_path"`
}

// SessionResultsArtifact session 聚合结果（对齐 dataclass SessionResultsArtifact）
type SessionResultsArtifact struct {
	Results []CycleResult `yaml:"results" json:"results"`
}

// CodeChangeArtifact Implement 阶段输出（对齐 dataclass CodeChangeArtifact）
type CodeChangeArtifact struct {
	Related     []Experience `yaml:"related" json:"related"`
	EditedFiles []string     `yaml:"edited_files" json:"edited_files"`
}

// VerifyReportArtifact Verify 阶段输出（对齐 dataclass VerifyReportArtifact）
type VerifyReportArtifact struct {
	CIResult  map[string]any `yaml:"ci_result" json:"ci_result"`
	FixErrors string         `yaml:"fix_errors" json:"fix_errors"`
	Reverted  bool           `yaml:"reverted" json:"reverted"`
	Error     string         `yaml:"error" json:"error"`
}

// CommitArtifact Commit 阶段输出（对齐 dataclass CommitArtifact）
type CommitArtifact struct {
	Facts          *CommitFacts `yaml:"facts" json:"facts"`
	StatusText     string       `yaml:"status_text" json:"status_text"`
	LastCommitStat string       `yaml:"last_commit_stat" json:"last_commit_stat"`
	BranchName     string       `yaml:"branch_name" json:"branch_name"`
	Committed      bool         `yaml:"committed" json:"committed"`
	Error          string       `yaml:"error" json:"error"`
}

// PullRequestArtifact Publish 阶段输出（对齐 dataclass PullRequestArtifact）
type PullRequestArtifact struct {
	PRURL   string `yaml:"pr_url" json:"pr_url"`
	Summary string `yaml:"summary" json:"summary"`
}

// PullRequestDraft communicate agent 产出的结构化 PR 草稿（对齐 dataclass PullRequestDraft）
type PullRequestDraft struct {
	Title string `yaml:"title" json:"title"`
	Body  string `yaml:"body" json:"body"`
	Kind  string `yaml:"kind" json:"kind"`
}

// CommitFacts 提交阶段的事实快照（对齐 dataclass CommitFacts）
type CommitFacts struct {
	BranchName             string   `yaml:"branch_name" json:"branch_name"`
	TaskDeclaredFiles      []string `yaml:"task_declared_files" json:"task_declared_files"`
	PreexistingDirtyFiles  []string `yaml:"preexisting_dirty_files" json:"preexisting_dirty_files"`
	CurrentDirtyFiles      []string `yaml:"current_dirty_files" json:"current_dirty_files"`
	TrackedModifiedFiles   []string `yaml:"tracked_modified_files" json:"tracked_modified_files"`
	UntrackedFiles         []string `yaml:"untracked_files" json:"untracked_files"`
	EditedFiles            []string `yaml:"edited_files" json:"edited_files"`
	AllowedFiles           []string `yaml:"allowed_files" json:"allowed_files"`
	DerivedTestFiles       []string `yaml:"derived_test_files" json:"derived_test_files"`
	LegacyRelatedTestFiles []string `yaml:"legacy_related_test_files" json:"legacy_related_test_files"`
	VerifyRelatedFiles     []string `yaml:"verify_related_files" json:"verify_related_files"`
	DiffStat               string   `yaml:"diff_stat" json:"diff_stat"`
}

// ProjectProfile 项目画像，承载 repo-specific 默认值（对齐 dataclass ProjectProfile）
type ProjectProfile struct {
	Name               string   `yaml:"name" json:"name"`
	RepoURL            string   `yaml:"repo_url" json:"repo_url"`
	RepoSlug           string   `yaml:"repo_slug" json:"repo_slug"`
	Platform           string   `yaml:"platform" json:"platform"`
	ImmutableFiles     []string `yaml:"immutable_files" json:"immutable_files"`
	HighImpactPrefixes []string `yaml:"high_impact_prefixes" json:"high_impact_prefixes"`
	DefaultBaseBranch  string   `yaml:"default_base_branch" json:"default_base_branch"`
	DefaultCIProfile   string   `yaml:"default_ci_profile" json:"default_ci_profile"`
}

// AutoHarnessPaths 运行所需的路径派生结果（对齐 dataclass AutoHarnessPaths）
type AutoHarnessPaths struct {
	DataDir              string `yaml:"data_dir" json:"data_dir"`
	ExperienceDir        string `yaml:"experience_dir" json:"experience_dir"`
	WorktreesDir         string `yaml:"worktrees_dir" json:"worktrees_dir"`
	RunsDir              string `yaml:"runs_dir" json:"runs_dir"`
	CacheRepoDir         string `yaml:"cache_repo_dir" json:"cache_repo_dir"`
	RuntimeExtensionsDir string `yaml:"runtime_extensions_dir" json:"runtime_extensions_dir"`
}

// AutoHarnessRuntimeState 运行时状态（对齐 dataclass AutoHarnessRuntimeState）
type AutoHarnessRuntimeState struct {
	CurrentWorkspace   string `yaml:"current_workspace" json:"current_workspace"`
	SelectedPipeline   string `yaml:"selected_pipeline" json:"selected_pipeline"`
	ConfigBootstrapped bool   `yaml:"config_bootstrapped" json:"config_bootstrapped"`
	SuggestedLocalRepo string `yaml:"suggested_local_repo" json:"suggested_local_repo"`
	// SessionID 12 位随机 hex（对齐 default_factory=lambda: uuid.uuid4().hex[:12]）
	SessionID string `yaml:"session_id" json:"session_id"`
}

// StageResult 统一 stage 执行结果（对齐 dataclass StageResult）
type StageResult struct {
	Status    string         `yaml:"status" json:"status"`
	Artifacts map[string]any `yaml:"artifacts" json:"artifacts"`
	Messages  []string       `yaml:"messages" json:"messages"`
	Metrics   map[string]any `yaml:"metrics" json:"metrics"`
	Error     string         `yaml:"error" json:"error"`
}

// StageSpec Stage 的声明式元数据（对齐 dataclass StageSpec）
type StageSpec struct {
	Name        string   `yaml:"name" json:"name"`
	StageCls    any      `yaml:"stage_cls" json:"stage_cls"`
	Scope       string   `yaml:"scope" json:"scope"`
	Consumes    []string `yaml:"consumes" json:"consumes"`
	Produces    []string `yaml:"produces" json:"produces"`
	Description string   `yaml:"description" json:"description"`
	Slot        string   `yaml:"slot" json:"slot"`
}

// PipelineSpec Pipeline 的声明式模板（对齐 dataclass PipelineSpec）
type PipelineSpec struct {
	Name            string   `yaml:"name" json:"name"`
	PipelineCls     any      `yaml:"pipeline_cls" json:"pipeline_cls"`
	Description     string   `yaml:"description" json:"description"`
	ExpectedOutputs []string `yaml:"expected_outputs" json:"expected_outputs"`
}

// ActivateDecision activate stage 的用户决策结果（对齐 dataclass ActivateDecision）
type ActivateDecision struct {
	Action   string `yaml:"action" json:"action"`
	Feedback string `yaml:"feedback" json:"feedback"`
}

// AutoHarnessConfig Auto Harness Agent 配置（对齐 dataclass AutoHarnessConfig）
//
// DataDir 由宿主 CLI 传入，所有产物（经验库、运行记录、clone 缓存、
// worktree）都存放在此目录下。LocalRepo 可选，指向本地 agent-core
// 仓库路径，用于加速 worktree 创建；未配置时自动 clone 到
// {data_dir}/repo/agent-core。
type AutoHarnessConfig struct {
	// Model / PlanModel 主/计划模型（对齐 Optional[Model]，D-e2-5 指针）
	Model     *llm.Model
	PlanModel *llm.Model

	// ---- 路径 ----
	DataDir                string
	LocalRepo              string
	RepoURL                string
	SkillsDirs             []string
	CommunitySkillRepos    []string
	CommunitySkillCacheDir string
	StageRegistrars        []string
	PipelineRegistrars     []string

	// ---- 语言 ----
	Language           string
	OptimizationGoal   string
	PipelinePreference string

	// ---- 预算 ----
	SessionBudgetSecs          float64
	CostLimitUSD               float64
	TaskTimeoutSecs            float64
	ModelTimeoutSecs           float64
	MaxTasksPerSession         int
	SelfDrivenSlots            int
	ExtensionVerifyConcurrency int

	// ---- Git ----
	GitRemote     string
	GitBaseBranch string
	GitUserName   string
	GitUserEmail  string
	ForkOwner     string
	UpstreamOwner string
	UpstreamRepo  string

	// ---- GitCode ----
	GitcodeUsername string
	GitcodeToken    string
	GitcodeTokenEnv string

	// ---- CI 门控 ----
	CIGateConfig           string
	CIGatePythonExecutable string
	CIGateInstallCommand   string

	// ---- Fix Loop ----
	FixPhase1MaxRetries int
	FixPhase2MaxRetries int

	// ---- 安全 ----
	ImmutableFiles     []string
	HighImpactPrefixes []string
	AgentIterations    map[string]int

	// ---- 兼容旧字段 ----
	// Workspace 已废弃，保留以兼容旧调用方
	Workspace          string
	ConfigPath         string
	ConfigBootstrapped bool
	SuggestedLocalRepo string
	ExperienceDir      string
}

// ──────────────────────────── 导出函数 ────────────────────────────

// NormalizePipelinePreference 归一化用户侧流水线偏好值（对齐 normalize_pipeline_preference）
func NormalizePipelinePreference(value any) string {
	raw := strings.ToLower(strings.TrimSpace(toString(value)))
	if normalized, ok := pipelinePreferenceAliases[raw]; ok {
		return normalized
	}
	pipelineName := NormalizePipelineName(raw)
	if pipelineName == MetaEvolvePipeline || pipelineName == ExtendedEvolvePipeline {
		return pipelineName
	}
	logger.Warn(logComponent).
		Str("value", toString(value)).
		Msg("Invalid auto-harness pipeline preference, using auto")
	return PipelinePreferenceAuto
}

// LoadAutoHarnessConfig 从 YAML 文件加载 AutoHarnessConfig（对齐 load_auto_harness_config）。
//
// 文件不存在时自动生成模板并返回默认配置。
func LoadAutoHarnessConfig(configPath string, workspaceHint string) *AutoHarnessConfig {
	path := filepath.Clean(configPath)
	if !isRegularFile(path) {
		suggested := detectLocalRepo(workspaceHint)
		bootstrapped := bootstrapConfigFile(path, suggested)
		logger.Info(logComponent).
			Str("config_path", configPath).
			Msg("Config file not found, using defaults")
		cfg := newDefaultAutoHarnessConfig()
		cfg.ConfigPath = path
		cfg.ConfigBootstrapped = bootstrapped
		cfg.SuggestedLocalRepo = suggested
		if cfg.DataDir == "" {
			cfg.DataDir = filepath.Dir(path)
		}
		return cfg
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		logger.Warn(logComponent).
			Str("config_path", configPath).
			Err(err).
			Msg("Failed to load config, using defaults")
		cfg := newDefaultAutoHarnessConfig()
		cfg.ConfigPath = path
		cfg.SuggestedLocalRepo = detectLocalRepo(workspaceHint)
		if cfg.DataDir == "" {
			cfg.DataDir = filepath.Dir(path)
		}
		return cfg
	}
	var data map[string]any
	if err := yaml.Unmarshal(raw, &data); err != nil || data == nil {
		if err != nil {
			logger.Warn(logComponent).
				Str("config_path", configPath).
				Err(err).
				Msg("Failed to load config, using defaults")
		}
		cfg := newDefaultAutoHarnessConfig()
		cfg.ConfigPath = path
		cfg.SuggestedLocalRepo = detectLocalRepo(workspaceHint)
		if cfg.DataDir == "" {
			cfg.DataDir = filepath.Dir(path)
		}
		return cfg
	}

	cfg := AutoHarnessConfigFromDict(data)
	cfg.ConfigPath = path
	cfg.SuggestedLocalRepo = detectLocalRepo(workspaceHint)

	// data_dir 默认为配置文件所在目录
	if cfg.DataDir == "" {
		cfg.DataDir = filepath.Dir(path)
	}

	return cfg
}

// IsPlaceholderLocalRepo 判断 path 是否为明显的模板/示例值（对齐 is_placeholder_local_repo）
func IsPlaceholderLocalRepo(path string) bool {
	normalized := strings.TrimSpace(path)
	return normalized == "/home/user/code/agent-core" || normalized == "/home/user/repo"
}

// AutoHarnessConfigFromDict 从字典构建配置，支持嵌套 YAML 结构
// （对齐 AutoHarnessConfig.load_from_dict，字段读取顺序逐条照抄）
func AutoHarnessConfigFromDict(data map[string]any) *AutoHarnessConfig {
	cfg := newDefaultAutoHarnessConfig()

	// 顶层字段
	if v, ok := data["data_dir"]; ok {
		cfg.DataDir = toString(v)
	}
	if v, ok := data["local_repo"]; ok {
		cfg.LocalRepo = toString(v)
	}
	if v, ok := data["repo_url"]; ok {
		cfg.RepoURL = toString(v)
	}
	if v, ok := data["skills_dirs"]; ok {
		if list, ok := v.([]any); ok {
			cfg.SkillsDirs = toStringSlice(list)
		}
	}
	if v, ok := data["community_skill_repos"]; ok {
		if list, ok := v.([]any); ok {
			cfg.CommunitySkillRepos = toStringSlice(list)
		}
	}
	if v, ok := data["community_skill_cache_dir"]; ok {
		cfg.CommunitySkillCacheDir = toString(v)
	}
	if v, ok := data["stage_registrars"]; ok {
		if list, ok := v.([]any); ok {
			cfg.StageRegistrars = toStringSlice(list)
		}
	}
	if v, ok := data["pipeline_registrars"]; ok {
		if list, ok := v.([]any); ok {
			cfg.PipelineRegistrars = toStringSlice(list)
		}
	}
	if v, ok := data["immutable_files"]; ok {
		if list, ok := v.([]any); ok {
			cfg.ImmutableFiles = toStringSlice(list)
		}
	}
	if v, ok := data["language"]; ok {
		cfg.Language = toString(v)
	}
	if v, ok := data["pipeline"]; ok {
		cfg.PipelinePreference = NormalizePipelinePreference(v)
	}
	if v, ok := data["pipeline_preference"]; ok {
		cfg.PipelinePreference = NormalizePipelinePreference(v)
	}
	// 兼容旧字段
	if v, ok := data["workspace"]; ok {
		cfg.Workspace = toString(v)
	}
	if v, ok := data["experience_dir"]; ok {
		cfg.ExperienceDir = toString(v)
	}

	// git section
	git, ok := data["git"].(map[string]any)
	if ok {
		if v, ok := git["remote"]; ok {
			cfg.GitRemote = toString(v)
		}
		if v, ok := git["base_branch"]; ok {
			cfg.GitBaseBranch = toString(v)
		}
		if v, ok := git["user_name"]; ok {
			cfg.GitUserName = toString(v)
		}
		if v, ok := git["user_email"]; ok {
			cfg.GitUserEmail = toString(v)
		}
		if v, ok := git["fork_owner"]; ok {
			cfg.ForkOwner = toString(v)
		}
		if v, ok := git["upstream_owner"]; ok {
			cfg.UpstreamOwner = toString(v)
		}
		if v, ok := git["upstream_repo"]; ok {
			cfg.UpstreamRepo = toString(v)
		}
	}

	// gitcode section
	gc, ok := data["gitcode"].(map[string]any)
	if ok {
		if v, ok := gc["username"]; ok {
			cfg.GitcodeUsername = toString(v)
		}
		if v, ok := gc["access_token_env"]; ok {
			cfg.GitcodeTokenEnv = toString(v)
		}
		if v, ok := gc["access_token"]; ok {
			cfg.GitcodeToken = toString(v)
		}
	}

	// budget section
	budget, ok := data["budget"].(map[string]any)
	if ok {
		if v, ok := budget["session_secs"]; ok {
			cfg.SessionBudgetSecs = toFloat64(v)
		}
		if v, ok := budget["cost_limit_usd"]; ok {
			cfg.CostLimitUSD = toFloat64(v)
		}
		if v, ok := budget["task_timeout_secs"]; ok {
			cfg.TaskTimeoutSecs = toFloat64(v)
		}
		if v, ok := budget["model_timeout_secs"]; ok {
			cfg.ModelTimeoutSecs = toFloat64(v)
		}
		if v, ok := budget["max_tasks_per_session"]; ok {
			cfg.MaxTasksPerSession = int(toFloat64(v))
		}
	}

	// ci_gate section
	ci, ok := data["ci_gate"].(map[string]any)
	if ok {
		if v, ok := ci["config_path"]; ok {
			cfg.CIGateConfig = toString(v)
		}
		if v, ok := ci["python_executable"]; ok {
			cfg.CIGatePythonExecutable = toString(v)
		}
		if v, ok := ci["install_command"]; ok {
			cfg.CIGateInstallCommand = toString(v)
		}
	}

	// fix_loop section
	fl, ok := data["fix_loop"].(map[string]any)
	if ok {
		if v, ok := fl["phase1_max_retries"]; ok {
			cfg.FixPhase1MaxRetries = int(toFloat64(v))
		}
		if v, ok := fl["phase2_max_retries"]; ok {
			cfg.FixPhase2MaxRetries = int(toFloat64(v))
		}
	}

	agentCfg, ok := data["agent"].(map[string]any)
	if ok {
		for key, value := range agentCfg {
			if n := toIntOrNaN(value); n != nil {
				cfg.AgentIterations[key] = *n
			}
			// Python try/except (TypeError, ValueError): continue —— 转换失败跳过
		}
	}

	extensions, ok := data["extensions"].(map[string]any)
	if ok {
		if v, ok := extensions["stage_registrars"]; ok {
			if list, ok := v.([]any); ok {
				cfg.StageRegistrars = toStringSlice(list)
			}
		}
		if v, ok := extensions["pipeline_registrars"]; ok {
			if list, ok := v.([]any); ok {
				cfg.PipelineRegistrars = toStringSlice(list)
			}
		}
	}

	return cfg
}

// ResolvedExperienceDir 经验库目录，从 data_dir 派生（对齐 property resolved_experience_dir）
func (c *AutoHarnessConfig) ResolvedExperienceDir() string {
	if c.ExperienceDir != "" {
		return c.ExperienceDir
	}
	if c.DataDir != "" {
		return filepath.Join(c.DataDir, "experience")
	}
	return ".auto_harness/experience/"
}

// WorktreesDir Worktree 根目录，从 data_dir 派生（对齐 property worktrees_dir）
func (c *AutoHarnessConfig) WorktreesDir() string {
	if c.DataDir != "" {
		return filepath.Join(c.DataDir, "worktrees")
	}
	return ".auto_harness/worktrees/"
}

// RunsDir 运行记录目录，从 data_dir 派生（对齐 property runs_dir）
func (c *AutoHarnessConfig) RunsDir() string {
	if c.DataDir != "" {
		return filepath.Join(c.DataDir, "runs")
	}
	return ".auto_harness/runs/"
}

// CacheRepoDir clone 缓存目录，从 data_dir 派生（对齐 property cache_repo_dir）
func (c *AutoHarnessConfig) CacheRepoDir() string {
	repoName := c.ResolveRepoName()
	if c.DataDir != "" {
		return filepath.Join(c.DataDir, "repo", repoName)
	}
	return fmt.Sprintf(".auto_harness/repo/%s", repoName)
}

// RuntimeExtensionsDir session 内运行时扩展根目录（对齐 property runtime_extensions_dir）
func (c *AutoHarnessConfig) RuntimeExtensionsDir() string {
	if c.DataDir != "" {
		return filepath.Join(c.DataDir, "runtime_extensions")
	}
	return ".auto_harness/runtime_extensions/"
}

// ResolvedCommunitySkillCacheDir 社区技能仓库缓存目录（对齐 property resolved_community_skill_cache_dir）
func (c *AutoHarnessConfig) ResolvedCommunitySkillCacheDir() string {
	if c.CommunitySkillCacheDir != "" {
		return c.CommunitySkillCacheDir
	}
	if c.DataDir != "" {
		return filepath.Join(c.DataDir, "skills-cache")
	}
	return ".auto_harness/skills-cache/"
}

// ResolveRepoName 解析本地缓存使用的仓库目录名（对齐 resolve_repo_name）
func (c *AutoHarnessConfig) ResolveRepoName() string {
	candidates := []string{
		c.UpstreamRepo,
		repoURLStem(c.RepoURL),
	}
	for _, candidate := range candidates {
		name := strings.TrimSpace(candidate)
		if name != "" {
			return strings.TrimSuffix(name, ".git")
		}
	}
	return "repository"
}

// ResolveGitcodeToken 解析 GitCode token：优先使用 GitcodeToken，
// 否则从 GitcodeTokenEnv 指定的环境变量读取；未配置返回空串
// （对齐 resolve_gitcode_token）
func (c *AutoHarnessConfig) ResolveGitcodeToken() string {
	if c.GitcodeToken != "" {
		return c.GitcodeToken
	}
	return os.Getenv(c.GitcodeTokenEnv)
}

// ResolveGitcodeUsername 解析 git HTTPS 认证的 GitCode 登录用户名（对齐 resolve_gitcode_username）
func (c *AutoHarnessConfig) ResolveGitcodeUsername() string {
	if c.GitcodeUsername != "" {
		return c.GitcodeUsername
	}
	if c.ForkOwner != "" {
		return c.ForkOwner
	}
	return ""
}

// ResolveCIGatePythonExecutable 解析 CI gate 使用的 Python 可执行文件
// （对齐 resolve_ci_gate_python_executable；sys.executable 兜底在 Go 侧
// 等价取当前进程可执行文件，偏差 D-e2-9：Go 无解释器概念）
func (c *AutoHarnessConfig) ResolveCIGatePythonExecutable() string {
	if c.CIGatePythonExecutable != "" {
		return c.CIGatePythonExecutable
	}

	var candidates []string
	if c.Workspace != "" {
		candidates = append(candidates, venvPythonCandidates(c.Workspace)...)
	}
	if c.LocalRepo != "" {
		candidates = append(candidates, venvPythonCandidates(c.LocalRepo)...)
	}

	for _, candidate := range candidates {
		if isRegularFile(candidate) {
			return candidate
		}
	}

	exe, err := os.Executable()
	if err != nil {
		return ""
	}
	return exe
}

// ResolveAgentIterations 解析某个 agent stage 的 max_iterations
// （对齐 resolve_agent_iterations：转换失败返回 default）
func (c *AutoHarnessConfig) ResolveAgentIterations(stageName string, defaultValue int) int {
	raw, ok := c.AgentIterations[stageName]
	if !ok {
		return defaultValue
	}
	// Python int(self.agent_iterations.get(stage_name, default))；map 值已是 int，
	// 但字符串等非法值在 load_from_dict 已被 try/except 过滤；此处直接返回。
	_ = raw
	return raw
}

// ResolveImmutableFiles 返回配置的不可改文件或内置默认值（对齐 resolve_immutable_files）
func (c *AutoHarnessConfig) ResolveImmutableFiles() []string {
	if len(c.ImmutableFiles) > 0 {
		out := make([]string, len(c.ImmutableFiles))
		copy(out, c.ImmutableFiles)
		return out
	}
	return defaultImmutableFiles()
}

// BuildProjectProfile 构建 repo-specific 项目画像（对齐 build_project_profile）
func (c *AutoHarnessConfig) BuildProjectProfile() ProjectProfile {
	profile := ProjectProfile{
		// Name/RepoSlug/Platform 取 ProjectProfile dataclass 字段默认值
		// （Python build_project_profile 仅显式传后四项）
		Name:               "agent-core",
		RepoSlug:           "openJiuwen/agent-core",
		Platform:           "gitcode",
		RepoURL:            c.RepoURL,
		ImmutableFiles:     c.ResolveImmutableFiles(),
		HighImpactPrefixes: append([]string(nil), c.HighImpactPrefixes...),
		DefaultBaseBranch:  c.GitBaseBranch,
		DefaultCIProfile:   "default",
	}
	if profile.DefaultBaseBranch == "" {
		profile.DefaultBaseBranch = "develop"
	}
	return profile
}

// BuildPaths 构建派生运行时路径（对齐 build_paths）
func (c *AutoHarnessConfig) BuildPaths() AutoHarnessPaths {
	return AutoHarnessPaths{
		DataDir:              c.DataDir,
		ExperienceDir:        c.ResolvedExperienceDir(),
		WorktreesDir:         c.WorktreesDir(),
		RunsDir:              c.RunsDir(),
		CacheRepoDir:         c.CacheRepoDir(),
		RuntimeExtensionsDir: c.RuntimeExtensionsDir(),
	}
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// repoURLStem 取 URL 路径末段并去扩展名（对齐 Path(self.repo_url.rstrip("/")).stem）
func repoURLStem(repoURL string) string {
	trimmed := strings.TrimRight(repoURL, "/")
	if trimmed == "" {
		return ""
	}
	return strings.TrimSuffix(filepath.Base(trimmed), filepath.Ext(trimmed))
}

// newDefaultAutoHarnessConfig 构造带默认值的配置（对齐 dataclass 字段默认值）
func newDefaultAutoHarnessConfig() *AutoHarnessConfig {
	return &AutoHarnessConfig{
		RepoURL: defaultRepoURL,
		CommunitySkillRepos: []string{
			"https://github.com/anthropics/skills.git",
			"https://github.com/JimLiu/baoyu-skills.git",
		},
		Language:                   "cn",
		PipelinePreference:         PipelinePreferenceAuto,
		SessionBudgetSecs:          900000.0,
		CostLimitUSD:               10.0,
		TaskTimeoutSecs:            300000.0,
		ModelTimeoutSecs:           300000.0,
		MaxTasksPerSession:         10,
		SelfDrivenSlots:            1,
		ExtensionVerifyConcurrency: 4,
		GitBaseBranch:              "develop",
		UpstreamOwner:              "openJiuwen",
		UpstreamRepo:               "agent-core",
		GitcodeTokenEnv:            "GITCODE_ACCESS_TOKEN",
		FixPhase1MaxRetries:        10,
		FixPhase2MaxRetries:        9,
		HighImpactPrefixes:         []string{"openjiuwen/core/"},
		AgentIterations: map[string]int{
			"implement":        30,
			"assess":           30,
			"plan":             15,
			"select_pipeline":  10,
			"eval":             10,
			"pr_draft":         5,
			"learnings":        5,
			"explore_subagent": 20,
			"browser_subagent": 20,
			"merge_ext":        8,
		},
	}
}

// bootstrapConfigFile 缺失时创建起始配置文件（对齐 _bootstrap_config_file）
func bootstrapConfigFile(path string, suggestedLocalRepo string) bool {
	_ = suggestedLocalRepo
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		logger.Warn(logComponent).
			Str("path", path).
			Err(err).
			Msg("Failed to bootstrap config")
		return false
	}
	template := resource.DefaultConfigTemplate()
	if err := os.WriteFile(path, []byte(template), 0o644); err != nil {
		logger.Warn(logComponent).
			Str("path", path).
			Err(err).
			Msg("Failed to bootstrap config")
		return false
	}
	return true
}

// detectLocalRepo 尽力探测本地 agent-core 仓库（对齐 _detect_local_repo）
func detectLocalRepo(workspaceHint string) string {
	var candidates []string
	if workspaceHint != "" {
		hint := expandUser(workspaceHint)
		candidates = append(candidates, hint, filepath.Join(hint, "agent-core"))
	}
	cwd, _ := os.Getwd()
	candidates = append(candidates, cwd, filepath.Join(cwd, "agent-core"))

	seen := map[string]bool{}
	for _, candidate := range candidates {
		resolved, err := filepath.Abs(candidate)
		if err != nil {
			resolved = candidate
		}
		key := resolved
		if seen[key] {
			continue
		}
		seen[key] = true
		if looksLikeRepoRoot(resolved) {
			return key
		}
	}
	return ""
}

// looksLikeRepoRoot 判断 path 是否像本地 agent-core 仓库（对齐 _looks_like_repo_root）
func looksLikeRepoRoot(path string) bool {
	return isDir(path) &&
		pathExists(filepath.Join(path, ".git")) &&
		isRegularFile(filepath.Join(path, "pyproject.toml")) &&
		isDir(filepath.Join(path, "openjiuwen"))
}

// venvPythonCandidates 返回 base_dir 下平台感知的 venv python 路径（对齐 _venv_python_candidates）
func venvPythonCandidates(baseDir string) []string {
	venv := filepath.Join(baseDir, ".venv")
	if runtime.GOOS == "windows" {
		return []string{filepath.Join(venv, "Scripts", "python.exe")}
	}
	return []string{filepath.Join(venv, "bin", "python")}
}

// defaultImmutableFiles 内置不可改文件清单（对齐 _default_immutable_files）
func defaultImmutableFiles() []string {
	return []string{
		"openjiuwen/rsi/harness_rsi/auto_harness/prompts/identity.md",
		"openjiuwen/rsi/harness_rsi/auto_harness/resources/ci_gate.yaml",
		"openjiuwen/harness/rails/security/prompt_security_rail.py",
	}
}

// newShortID 生成 12 位随机 hex（对齐 uuid.uuid4().hex[:12]）
func newShortID() string {
	return NewShortID(12)
}

// NewShortID 生成 n 位随机 hex（uuid4().hex[:n] 等价）
func NewShortID(n int) string {
	if n <= 0 {
		return ""
	}
	buf := make([]byte, (n+1)/2)
	if _, err := rand.Read(buf); err != nil {
		s := fmt.Sprintf("%x", time.Now().UnixNano())
		return s[:n]
	}
	s := hex.EncodeToString(buf)
	return s[:n]
}

// toString 任意值转字符串（对齐 Python str(value)）
func toString(value any) string {
	switch v := value.(type) {
	case nil:
		return ""
	case string:
		return v
	case bool:
		if v {
			return "true"
		}
		return "false"
	case float64:
		if v == float64(int64(v)) {
			return fmt.Sprintf("%d", int64(v))
		}
		return fmt.Sprintf("%v", v)
	case int:
		return fmt.Sprintf("%d", v)
	default:
		return fmt.Sprintf("%v", v)
	}
}

// toStringSlice []any 转 []string（对齐 [str(v) for v in list]）
func toStringSlice(list []any) []string {
	out := make([]string, 0, len(list))
	for _, v := range list {
		out = append(out, toString(v))
	}
	return out
}

// toFloat64 任意值转 float64（对齐 Python float(value)；yaml 数值统一 float64）
func toFloat64(value any) float64 {
	switch v := value.(type) {
	case float64:
		return v
	case int:
		return float64(v)
	case int64:
		return float64(v)
	case bool:
		if v {
			return 1.0
		}
		return 0.0
	case string:
		var f float64
		if _, err := fmt.Sscanf(v, "%g", &f); err == nil {
			return f
		}
		return 0.0
	default:
		return 0.0
	}
}

// toIntOrNaN 任意值转 int；失败返回 nil（对齐 int(value) 的 try/except）
func toIntOrNaN(value any) *int {
	switch v := value.(type) {
	case float64:
		n := int(v)
		return &n
	case int:
		n := v
		return &n
	case int64:
		n := int(v)
		return &n
	case string:
		var n int
		if _, err := fmt.Sscanf(v, "%d", &n); err == nil {
			return &n
		}
		return nil
	default:
		return nil
	}
}

// pathExists 判断路径存在（对齐 Path.exists()）
func pathExists(path string) bool {
	_, err := os.Lstat(path)
	return err == nil
}

// isDir 判断路径是目录（对齐 Path.is_dir()）
func isDir(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

// isRegularFile 判断路径是常规文件（对齐 Path.is_file()）
func isRegularFile(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

// expandUser 展开 ~ 前缀（对齐 Path.expanduser()）
func expandUser(path string) string {
	if strings.HasPrefix(path, "~/") || path == "~" {
		home, err := os.UserHomeDir()
		if err == nil {
			return filepath.Join(home, strings.TrimPrefix(path, "~"))
		}
	}
	return path
}

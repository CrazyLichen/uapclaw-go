package config

import (
	"encoding/json"
	"fmt"
	"math"
	"strconv"

	harness_rsi "github.com/uapclaw/uapclaw-go/internal/evolving/rsi/harness_rsi"
)

// ──────────────────────────── 常量 ────────────────────────────

// DefaultModelCallMaxRetries 转发顶层包常量（对齐 Python 从 model_call 导入）
const DefaultModelCallMaxRetries = harness_rsi.DefaultModelCallMaxRetries

// defaultSolverIterations solver 迭代上限默认值（对齐 _solver_iteration_limit 的 300）
const defaultSolverIterations = 300

// ──────────────────────────── 结构体 ────────────────────────────

// DataLoaderConfig 既有评估数据集的载入与分批（对齐 DataLoaderConfig）
type DataLoaderConfig struct {
	// FilePattern 数据集文件通配模式
	FilePattern string `json:"file_pattern" yaml:"file_pattern"`
	// BatchSize 批次大小
	BatchSize int `json:"batch_size" yaml:"batch_size"`
	// BatchBalanceKeys 批次均衡键
	BatchBalanceKeys []string `json:"batch_balance_keys" yaml:"batch_balance_keys"`
}

// ModelConfigs 三个运行时阶段使用的模型引用（对齐 ModelConfigs）
type ModelConfigs struct {
	// Evaluation 评估模型引用
	Evaluation string `json:"evaluation" yaml:"evaluation"`
	// Analysis 分析模型引用
	Analysis string `json:"analysis" yaml:"analysis"`
	// MemberOptimization 成员优化模型引用
	MemberOptimization string `json:"member_optimization" yaml:"member_optimization"`
}

// EvaluatorConfig 单 harness 执行与显式评分选择（对齐 EvaluatorConfig）
type EvaluatorConfig struct {
	// ModelConfigRef 模型配置引用
	ModelConfigRef string `json:"model_config_ref" yaml:"model_config_ref"`
	// Backend 执行后端（single_harness）
	Backend string `json:"backend" yaml:"backend"`
	// EvaluationMethod 评分方式（script-based / llm-based）
	EvaluationMethod string `json:"evaluation_method" yaml:"evaluation_method"`
	// JudgeModelConfigRef judge 模型配置引用
	JudgeModelConfigRef string `json:"judge_model_config_ref" yaml:"judge_model_config_ref"`
	// JudgeAgentMaxIterations judge agent 最大迭代
	JudgeAgentMaxIterations int `json:"judge_agent_max_iterations" yaml:"judge_agent_max_iterations"`
	// JudgeTimeoutSec judge 超时秒数
	JudgeTimeoutSec int `json:"judge_timeout_sec" yaml:"judge_timeout_sec"`
	// JudgeMaxRetries judge 最大重试
	JudgeMaxRetries int `json:"judge_max_retries" yaml:"judge_max_retries"`
	// OutputRepair 输出修复预算
	OutputRepair harness_rsi.OutputRepairConfig `json:"output_repair" yaml:"output_repair"`
	// JudgeSuccessScore judge 判定成功分数
	JudgeSuccessScore float64 `json:"judge_success_score" yaml:"judge_success_score"`
	// ScoreMode 评分模式（binary / continuous）
	ScoreMode string `json:"score_mode" yaml:"score_mode"`
	// JudgeBaselineComparison judge 是否对比基线
	JudgeBaselineComparison bool `json:"judge_baseline_comparison" yaml:"judge_baseline_comparison"`
	// TransientCaseRetryLimit 瞬态用例重试上限
	TransientCaseRetryLimit int `json:"transient_case_retry_limit" yaml:"transient_case_retry_limit"`
	// SolverBackend 求解后端（deep_agent / uapclawswarm；上游为 jiuwenswarm，品牌映射）
	SolverBackend string `json:"solver_backend" yaml:"solver_backend"`
	// SolverMaxIterations 求解器最大迭代
	SolverMaxIterations int `json:"solver_max_iterations" yaml:"solver_max_iterations"`
	// UapclawswarmExecutable uapclawswarm 可执行文件（上游键 jiuwenswarm_executable，品牌映射）
	UapclawswarmExecutable string `json:"uapclawswarm_executable" yaml:"uapclawswarm_executable"`
	// UapclawswarmPython uapclawswarm Python 解释器
	UapclawswarmPython string `json:"uapclawswarm_python" yaml:"uapclawswarm_python"`
	// UapclawswarmExpectedVersion 期望版本
	UapclawswarmExpectedVersion string `json:"uapclawswarm_expected_version" yaml:"uapclawswarm_expected_version"`
	// UapclawswarmStartupTimeoutSec 启动超时秒数
	UapclawswarmStartupTimeoutSec int `json:"uapclawswarm_startup_timeout_sec" yaml:"uapclawswarm_startup_timeout_sec"`
	// UapclawswarmRuntimeTimeoutSec 运行超时秒数
	UapclawswarmRuntimeTimeoutSec int `json:"uapclawswarm_runtime_timeout_sec" yaml:"uapclawswarm_runtime_timeout_sec"`
	// UapclawswarmRuntimeProfile 运行 profile
	UapclawswarmRuntimeProfile string `json:"uapclawswarm_runtime_profile" yaml:"uapclawswarm_runtime_profile"`
	// SolverOnMaxIterations 求解器到上限策略
	SolverOnMaxIterations string `json:"solver_on_max_iterations" yaml:"solver_on_max_iterations"`
	// SolverMaxIterationsHardLimit 求解器迭代硬上限
	SolverMaxIterationsHardLimit int `json:"solver_max_iterations_hard_limit" yaml:"solver_max_iterations_hard_limit"`
}

// EvaluationResultAnalyzerConfig 评估后因果诊断配置（对齐 EvaluationResultAnalyzerConfig）
type EvaluationResultAnalyzerConfig struct {
	// ModelConfigRef 模型配置引用
	ModelConfigRef string `json:"model_config_ref" yaml:"model_config_ref"`
	// DiagnosisAgentModelConfigRef 诊断 agent 模型配置引用
	DiagnosisAgentModelConfigRef string `json:"diagnosis_agent_model_config_ref" yaml:"diagnosis_agent_model_config_ref"`
	// DiagnosisAgentMaxRetries 诊断 agent 最大重试
	DiagnosisAgentMaxRetries int `json:"diagnosis_agent_max_retries" yaml:"diagnosis_agent_max_retries"`
	// DiagnosisAgentMaxConcurrency 诊断 agent 最大并发
	DiagnosisAgentMaxConcurrency int `json:"diagnosis_agent_max_concurrency" yaml:"diagnosis_agent_max_concurrency"`
	// DiagnosisAgentMaxIterations 诊断 agent 最大迭代
	DiagnosisAgentMaxIterations int `json:"diagnosis_agent_max_iterations" yaml:"diagnosis_agent_max_iterations"`
	// CausalInvestigationRequired 是否强制因果调查
	CausalInvestigationRequired bool `json:"causal_investigation_required" yaml:"causal_investigation_required"`
	// MaxIssues 最大问题数
	MaxIssues int `json:"max_issues" yaml:"max_issues"`
	// EvidenceLimitPerIssue 单问题证据上限
	EvidenceLimitPerIssue int `json:"evidence_limit_per_issue" yaml:"evidence_limit_per_issue"`
	// OutputFilename 输出文件名
	OutputFilename string `json:"output_filename" yaml:"output_filename"`
	// OutputRepair 输出修复预算
	OutputRepair harness_rsi.OutputRepairConfig `json:"output_repair" yaml:"output_repair"`
}

// MemberOptimizerConfig 单个专家 harness 的候选生成与验证配置（对齐 MemberOptimizerConfig）
type MemberOptimizerConfig struct {
	// ModelConfigRef 模型配置引用
	ModelConfigRef string `json:"model_config_ref" yaml:"model_config_ref"`
	// ActionGroupConfigs 动作分组配置清单
	ActionGroupConfigs []string `json:"action_group_configs" yaml:"action_group_configs"`
	// AgentSkillsDirs agent 技能目录清单
	AgentSkillsDirs []string `json:"agent_skills_dirs" yaml:"agent_skills_dirs"`
	// Freeze 是否冻结
	Freeze bool `json:"freeze" yaml:"freeze"`
	// MaxRolesPerRun 单次运行最大角色数
	MaxRolesPerRun int `json:"max_roles_per_run" yaml:"max_roles_per_run"`
	// MinAttributionConfidence 归因最小置信度
	MinAttributionConfidence float64 `json:"min_attribution_confidence" yaml:"min_attribution_confidence"`
	// AttributionRetryLimit 归因重试上限
	AttributionRetryLimit int `json:"attribution_retry_limit" yaml:"attribution_retry_limit"`
	// StageRetryLimit 阶段重试上限
	StageRetryLimit int `json:"stage_retry_limit" yaml:"stage_retry_limit"`
	// ActionPlanningMaxAttempts 动作规划最大尝试
	ActionPlanningMaxAttempts int `json:"action_planning_max_attempts" yaml:"action_planning_max_attempts"`
	// MaxCasesPerIssue 单问题最大用例数
	MaxCasesPerIssue int `json:"max_cases_per_issue" yaml:"max_cases_per_issue"`
	// MaxTraceExcerptChars 轨迹摘录字符上限
	MaxTraceExcerptChars int `json:"max_trace_excerpt_chars" yaml:"max_trace_excerpt_chars"`
	// MaxResultExcerptChars 结果摘录字符上限
	MaxResultExcerptChars int `json:"max_result_excerpt_chars" yaml:"max_result_excerpt_chars"`
	// AllowEmptyHarnessRefsNoop 空 harness 引用时是否允许空操作
	AllowEmptyHarnessRefsNoop bool `json:"allow_empty_harness_refs_noop" yaml:"allow_empty_harness_refs_noop"`
	// ExecutionConcurrency 执行并发
	ExecutionConcurrency int `json:"execution_concurrency" yaml:"execution_concurrency"`
	// RoleExecutionConcurrency 角色执行并发
	RoleExecutionConcurrency int `json:"role_execution_concurrency" yaml:"role_execution_concurrency"`
	// ActionExecutionConcurrencyPerRole 单角色动作执行并发
	ActionExecutionConcurrencyPerRole int `json:"action_execution_concurrency_per_role" yaml:"action_execution_concurrency_per_role"`
	// AllowedActionGroups 允许的动作分组
	AllowedActionGroups []string `json:"allowed_action_groups" yaml:"allowed_action_groups"`
	// AllowedPromptSurfaces 允许的提示词表面
	AllowedPromptSurfaces []string `json:"allowed_prompt_surfaces" yaml:"allowed_prompt_surfaces"`
	// MaxActionsPerPlan 单计划最大动作数（0 表示不限制）
	MaxActionsPerPlan int `json:"max_actions_per_plan" yaml:"max_actions_per_plan"`
	// MaxIssueAttemptsPerBatch 单批次问题尝试上限（0 表示不限制）
	MaxIssueAttemptsPerBatch int `json:"max_issue_attempts_per_batch" yaml:"max_issue_attempts_per_batch"`
	// MaxRepairRoundsPerBatch 单批次修复轮数上限
	MaxRepairRoundsPerBatch int `json:"max_repair_rounds_per_batch" yaml:"max_repair_rounds_per_batch"`
	// SiblingCandidateCount 兄弟候选数
	SiblingCandidateCount int `json:"sibling_candidate_count" yaml:"sibling_candidate_count"`
	// ImproverPolicyRef 优化器策略引用
	ImproverPolicyRef string `json:"improver_policy_ref" yaml:"improver_policy_ref"`
	// CandidateMinScoreDelta 候选最小分数增量
	CandidateMinScoreDelta float64 `json:"candidate_min_score_delta" yaml:"candidate_min_score_delta"`
	// CandidateMinTargetBehaviorDelta 候选最小目标行为增量
	CandidateMinTargetBehaviorDelta float64 `json:"candidate_min_target_behavior_delta" yaml:"candidate_min_target_behavior_delta"`
	// CandidateNonTargetMaxRegression 候选非目标最大回退
	CandidateNonTargetMaxRegression float64 `json:"candidate_non_target_max_regression" yaml:"candidate_non_target_max_regression"`
	// CandidateHoldoutCases 候选保留用例数
	CandidateHoldoutCases int `json:"candidate_holdout_cases" yaml:"candidate_holdout_cases"`
	// CandidateHoldoutMaxRegression 候选保留集最大回退
	CandidateHoldoutMaxRegression float64 `json:"candidate_holdout_max_regression" yaml:"candidate_holdout_max_regression"`
	// AdaptFrozenTeamIssues 是否适配冻结团队问题
	AdaptFrozenTeamIssues bool `json:"adapt_frozen_team_issues" yaml:"adapt_frozen_team_issues"`
	// OutputRepair 输出修复预算
	OutputRepair harness_rsi.OutputRepairConfig `json:"output_repair" yaml:"output_repair"`
	// RuntimeIterationHardLimit 运行迭代硬上限
	RuntimeIterationHardLimit int `json:"runtime_iteration_hard_limit" yaml:"runtime_iteration_hard_limit"`
}

// OrchestratorSchedulingConfig 迭代单 harness 循环的选择策略（对齐 OrchestratorSchedulingConfig）
type OrchestratorSchedulingConfig struct {
	// EvaluationStrategy 评估策略（仅支持 hybrid）
	EvaluationStrategy string `json:"evaluation_strategy" yaml:"evaluation_strategy"`
	// CoordinationStrategy 协调策略（仅支持 team_first_single_pass）
	CoordinationStrategy string `json:"coordination_strategy" yaml:"coordination_strategy"`
	// FullEvaluationEnabled 是否启用全量评估
	FullEvaluationEnabled bool `json:"full_evaluation_enabled" yaml:"full_evaluation_enabled"`
	// FullEvaluationConcurrency 全量评估并发
	FullEvaluationConcurrency int `json:"full_evaluation_concurrency" yaml:"full_evaluation_concurrency"`
}

// PromotionConfig 版本化分数权威；历史任务保留原策略（对齐 PromotionConfig）
type PromotionConfig struct {
	// PolicyVersion 策略版本（legacy_epoch_v1 / continuous_two_stage_v2）
	PolicyVersion string `json:"policy_version" yaml:"policy_version"`
	// MinImprovement 最小改进幅度
	MinImprovement float64 `json:"min_improvement" yaml:"min_improvement"`
	// NonTargetMaxRegression 非目标最大回退
	NonTargetMaxRegression float64 `json:"non_target_max_regression" yaml:"non_target_max_regression"`
}

// AutoCoordinatingHarnessConfig 单 harness 自我改进顶层配置（对齐 AutoCoordinatingHarnessConfig）
type AutoCoordinatingHarnessConfig struct {
	// WorkspaceDir 工作区目录
	WorkspaceDir string `json:"workspace_dir" yaml:"workspace_dir"`
	// MaxEpochs 最大轮次
	MaxEpochs int `json:"max_epochs" yaml:"max_epochs"`
	// ModelConfigs 模型引用
	ModelConfigs ModelConfigs `json:"model_configs" yaml:"model_configs"`
	// DataLoader 数据载入配置
	DataLoader DataLoaderConfig `json:"data_loader" yaml:"data_loader"`
	// Evaluator 评估器配置
	Evaluator EvaluatorConfig `json:"evaluator" yaml:"evaluator"`
	// EvaluationResultAnalyzer 诊断分析配置
	EvaluationResultAnalyzer EvaluationResultAnalyzerConfig `json:"evaluation_result_analyzer" yaml:"evaluation_result_analyzer"`
	// MemberOptimizer 成员优化配置
	MemberOptimizer MemberOptimizerConfig `json:"member_optimizer" yaml:"member_optimizer"`
	// Scheduling 调度配置
	Scheduling OrchestratorSchedulingConfig `json:"scheduling" yaml:"scheduling"`
	// Promotion 晋级配置
	Promotion PromotionConfig `json:"promotion" yaml:"promotion"`
}

// ──────────────────────────── 导出函数 ────────────────────────────

// NewEvaluatorConfig 构造并校验评估器配置（对齐 EvaluatorConfig.__post_init__）
func NewEvaluatorConfig(cfg EvaluatorConfig) (EvaluatorConfig, error) {
	if cfg.ScoreMode != "binary" && cfg.ScoreMode != "continuous" {
		return cfg, fmt.Errorf("evaluator.score_mode must be binary or continuous")
	}
	if cfg.SolverMaxIterations < 1 {
		return cfg, fmt.Errorf("evaluator.solver_max_iterations must be a positive integer")
	}
	if cfg.SolverMaxIterationsHardLimit < cfg.SolverMaxIterations {
		return cfg, fmt.Errorf("evaluator.solver_max_iterations_hard_limit must be an integer >= solver_max_iterations")
	}
	if cfg.SolverOnMaxIterations != "error" && cfg.SolverOnMaxIterations != "return_observations" {
		return cfg, fmt.Errorf("evaluator.solver_on_max_iterations is invalid")
	}
	return cfg, nil
}

// EvaluatorConfigFromDict 从映射构造评估器配置（对齐 EvaluatorConfig.from_dict）
func EvaluatorConfigFromDict(data map[string]any) (EvaluatorConfig, error) {
	cfg := EvaluatorConfig{
		ModelConfigRef:                stringValue(data, "model_config_ref", ""),
		Backend:                       stringValue(data, "backend", "single_harness"),
		EvaluationMethod:              stringValue(data, "evaluation_method", "script-based"),
		JudgeModelConfigRef:           stringValue(data, "judge_model_config_ref", ""),
		JudgeAgentMaxIterations:       intValue(data, "judge_agent_max_iterations", 300),
		JudgeTimeoutSec:               intValue(data, "judge_timeout_sec", 900),
		JudgeMaxRetries:               intValue(data, "judge_max_retries", 2),
		JudgeSuccessScore:             floatValue(data, "judge_success_score", 0.8),
		ScoreMode:                     stringValue(data, "score_mode", "binary"),
		TransientCaseRetryLimit:       intValue(data, "transient_case_retry_limit", 2),
		SolverBackend:                 stringValue(data, "solver_backend", "deep_agent"),
		SolverOnMaxIterations:         stringValue(data, "solver_on_max_iterations", "error"),
		UapclawswarmExecutable:        stringValue(data, "uapclawswarm_executable", ""),
		UapclawswarmPython:            stringValue(data, "uapclawswarm_python", ""),
		UapclawswarmExpectedVersion:   stringValue(data, "uapclawswarm_expected_version", ""),
		UapclawswarmStartupTimeoutSec: intValue(data, "uapclawswarm_startup_timeout_sec", 120),
		UapclawswarmRuntimeTimeoutSec: intValue(data, "uapclawswarm_runtime_timeout_sec", 3600),
		UapclawswarmRuntimeProfile:    stringValue(data, "uapclawswarm_runtime_profile", "task86"),
	}
	if raw, exists := data["judge_baseline_comparison"]; exists {
		value, err := boolValue(raw, true)
		if err != nil {
			return cfg, err
		}
		cfg.JudgeBaselineComparison = value
	} else {
		cfg.JudgeBaselineComparison = true
	}
	outputRepair, err := harness_rsi.OutputRepairConfigFromDict(mappingValue(data, "output_repair"), data["output_repair"] != nil)
	if err != nil {
		return cfg, err
	}
	cfg.OutputRepair = outputRepair
	solverMax, err := solverIterationLimit(data["solver_max_iterations"])
	if err != nil {
		return cfg, err
	}
	cfg.SolverMaxIterations = solverMax
	hardLimitRaw, hasHardLimit := data["solver_max_iterations_hard_limit"]
	if !hasHardLimit || hardLimitRaw == nil {
		hardLimitRaw = maxInt(300, solverMax)
	}
	hardLimit, err := solverIterationLimit(hardLimitRaw)
	if err != nil {
		return cfg, err
	}
	cfg.SolverMaxIterationsHardLimit = hardLimit
	return NewEvaluatorConfig(cfg)
}

// EvaluatorProtocolConfig 保持历史评估指纹对 legacy binary 运行稳定
// （对齐 evaluator_protocol_config：binary 弹出 score_mode；binary 或无基线对比弹出 judge_baseline_comparison）
func EvaluatorProtocolConfig(cfg EvaluatorConfig) map[string]any {
	payload := structToMap(cfg)
	if cfg.ScoreMode == "binary" {
		delete(payload, "score_mode")
	}
	if cfg.ScoreMode == "binary" || !cfg.JudgeBaselineComparison {
		delete(payload, "judge_baseline_comparison")
	}
	return payload
}

// EvaluationResultAnalyzerConfigFromDict 从映射构造诊断分析配置
// （对齐 EvaluationResultAnalyzerConfig.from_dict）
func EvaluationResultAnalyzerConfigFromDict(data map[string]any) (EvaluationResultAnalyzerConfig, error) {
	cfg := EvaluationResultAnalyzerConfig{
		ModelConfigRef:               stringValue(data, "model_config_ref", ""),
		DiagnosisAgentModelConfigRef: stringValue(data, "diagnosis_agent_model_config_ref", ""),
		DiagnosisAgentMaxRetries:     intValue(data, "diagnosis_agent_max_retries", DefaultModelCallMaxRetries),
		DiagnosisAgentMaxConcurrency: intValue(data, "diagnosis_agent_max_concurrency", 5),
		DiagnosisAgentMaxIterations:  intValue(data, "diagnosis_agent_max_iterations", 300),
		MaxIssues:                    intValue(data, "max_issues", 20),
		EvidenceLimitPerIssue:        intValue(data, "evidence_limit_per_issue", 5),
		OutputFilename:               stringValue(data, "output_filename", "issues.yaml"),
	}
	if raw, exists := data["causal_investigation_required"]; exists {
		value, err := boolValue(raw, true)
		if err != nil {
			return cfg, err
		}
		cfg.CausalInvestigationRequired = value
	} else {
		cfg.CausalInvestigationRequired = true
	}
	outputRepair, err := harness_rsi.OutputRepairConfigFromDict(mappingValue(data, "output_repair"), data["output_repair"] != nil)
	if err != nil {
		return cfg, err
	}
	cfg.OutputRepair = outputRepair
	return cfg, nil
}

// MemberOptimizerConfigFromDict 从映射构造成员优化配置（对齐 MemberOptimizerConfig.from_dict）
func MemberOptimizerConfigFromDict(data map[string]any) (MemberOptimizerConfig, error) {
	cfg := MemberOptimizerConfig{
		ModelConfigRef:                    stringValue(data, "model_config_ref", ""),
		ActionGroupConfigs:                stringList(data, "action_group_configs"),
		AgentSkillsDirs:                   stringList(data, "agent_skills_dirs"),
		MaxRolesPerRun:                    intValue(data, "max_roles_per_run", 2),
		MinAttributionConfidence:          floatValue(data, "min_attribution_confidence", 0.5),
		AttributionRetryLimit:             intValue(data, "attribution_retry_limit", DefaultModelCallMaxRetries),
		StageRetryLimit:                   intValue(data, "stage_retry_limit", DefaultModelCallMaxRetries),
		ActionPlanningMaxAttempts:         intValue(data, "action_planning_max_attempts", 10),
		MaxCasesPerIssue:                  intValue(data, "max_cases_per_issue", 3),
		MaxTraceExcerptChars:              intValue(data, "max_trace_excerpt_chars", 4000),
		MaxResultExcerptChars:             intValue(data, "max_result_excerpt_chars", 2000),
		AllowEmptyHarnessRefsNoop:         boolValueDefault(data, "allow_empty_harness_refs_noop", true),
		ExecutionConcurrency:              intValue(data, "execution_concurrency", 2),
		RoleExecutionConcurrency:          intValue(data, "role_execution_concurrency", 2),
		ActionExecutionConcurrencyPerRole: intValue(data, "action_execution_concurrency_per_role", 2),
		AllowedActionGroups:               stringList(data, "allowed_action_groups"),
		AllowedPromptSurfaces:             stringList(data, "allowed_prompt_surfaces"),
		MaxActionsPerPlan:                 intValue(data, "max_actions_per_plan", 0),
		MaxIssueAttemptsPerBatch:          intValue(data, "max_issue_attempts_per_batch", 0),
		MaxRepairRoundsPerBatch:           intValue(data, "max_repair_rounds_per_batch", 3),
		SiblingCandidateCount:             intValue(data, "sibling_candidate_count", 1),
		ImproverPolicyRef:                 stringValue(data, "improver_policy_ref", ""),
		CandidateMinScoreDelta:            floatValue(data, "candidate_min_score_delta", 0.0),
		CandidateMinTargetBehaviorDelta:   floatValue(data, "candidate_min_target_behavior_delta", 0.0),
		CandidateNonTargetMaxRegression:   floatValue(data, "candidate_non_target_max_regression", 0.0),
		CandidateHoldoutCases:             intValue(data, "candidate_holdout_cases", 0),
		CandidateHoldoutMaxRegression:     floatValue(data, "candidate_holdout_max_regression", 0.0),
		AdaptFrozenTeamIssues:             boolValueDefault(data, "adapt_frozen_team_issues", false),
	}
	cfg.Freeze = boolValueDefault(data, "freeze", false)
	if raw, exists := data["runtime_iteration_hard_limit"]; exists && raw != nil {
		limit, err := solverIterationLimit(raw)
		if err != nil {
			return cfg, err
		}
		cfg.RuntimeIterationHardLimit = limit
	} else {
		cfg.RuntimeIterationHardLimit = 300
	}
	if cfg.ActionPlanningMaxAttempts < 1 {
		return cfg, fmt.Errorf("member_optimizer.action_planning_max_attempts must be a positive integer")
	}
	outputRepair, err := harness_rsi.OutputRepairConfigFromDict(mappingValue(data, "output_repair"), data["output_repair"] != nil)
	if err != nil {
		return cfg, err
	}
	cfg.OutputRepair = outputRepair
	return cfg, nil
}

// OrchestratorSchedulingConfigFromDict 从映射构造调度配置
// （对齐 OrchestratorSchedulingConfig.from_dict，含 legacy promotion_policy 迁移拒绝）
func OrchestratorSchedulingConfigFromDict(data map[string]any) (OrchestratorSchedulingConfig, error) {
	if _, exists := data["promotion_policy"]; exists {
		return OrchestratorSchedulingConfig{}, fmt.Errorf("scheduling.promotion_policy must be migrated to promotion.policy_version")
	}
	cfg := OrchestratorSchedulingConfig{
		EvaluationStrategy:        stringValue(data, "evaluation_strategy", "hybrid"),
		CoordinationStrategy:      stringValue(data, "coordination_strategy", "team_first_single_pass"),
		FullEvaluationEnabled:     boolValueDefault(data, "full_evaluation_enabled", true),
		FullEvaluationConcurrency: intValue(data, "full_evaluation_concurrency", 2),
	}
	if err := cfg.Validate(); err != nil {
		return cfg, err
	}
	return cfg, nil
}

// Validate 校验调度配置（对齐 OrchestratorSchedulingConfig.validate）
func (o OrchestratorSchedulingConfig) Validate() error {
	if o.FullEvaluationConcurrency < 1 {
		return fmt.Errorf("scheduling.full_evaluation_concurrency must be a positive integer")
	}
	if o.EvaluationStrategy != "hybrid" {
		return fmt.Errorf("scheduling.evaluation_strategy must be hybrid")
	}
	if o.CoordinationStrategy != "team_first_single_pass" {
		return fmt.Errorf("scheduling.coordination_strategy must be team_first_single_pass")
	}
	return nil
}

// NewPromotionConfig 构造并校验晋级配置（对齐 PromotionConfig.__post_init__）
func NewPromotionConfig(cfg PromotionConfig) (PromotionConfig, error) {
	if cfg.PolicyVersion != "legacy_epoch_v1" && cfg.PolicyVersion != "continuous_two_stage_v2" {
		return cfg, fmt.Errorf("unsupported promotion.policy_version; start a new task using continuous_two_stage_v2")
	}
	if err := requireUnitInterval(cfg.MinImprovement, "promotion.min_improvement"); err != nil {
		return cfg, err
	}
	if err := requireUnitInterval(cfg.NonTargetMaxRegression, "promotion.non_target_max_regression"); err != nil {
		return cfg, err
	}
	return cfg, nil
}

// Continuous 是否使用整包连续晋级（对齐 PromotionConfig.continuous property）
func (p PromotionConfig) Continuous() bool {
	return p.PolicyVersion == "continuous_two_stage_v2"
}

// PromotionConfigFromDict 从映射构造晋级配置（对齐 PromotionConfig.from_dict）
func PromotionConfigFromDict(data map[string]any) (PromotionConfig, error) {
	policyVersion := stringValue(data, "policy_version", "continuous_two_stage_v2")
	if policyVersion == "continuous_two_stage_v2" {
		if _, exists := data["confirmation_runs"]; exists {
			return PromotionConfig{}, fmt.Errorf("continuous_two_stage_v2 has only target and full evaluations; remove confirmation_runs")
		}
	}
	cfg := PromotionConfig{
		PolicyVersion:          policyVersion,
		MinImprovement:         floatValue(data, "min_improvement", 0.05),
		NonTargetMaxRegression: floatValue(data, "non_target_max_regression", 0.05),
	}
	return NewPromotionConfig(cfg)
}

// AutoCoordinatingHarnessConfigFromDict 从映射构造顶层配置
// （对齐 AutoCoordinatingHarnessConfig.from_dict：模型引用三级回退、迭代上限继承、
// promotion 连续模式与 score_mode 联动、validate 收口）
func AutoCoordinatingHarnessConfigFromDict(data map[string]any) (AutoCoordinatingHarnessConfig, error) {
	modelConfigs := ModelConfigsFromDict(mappingValue(data, "model_configs"))
	evaluator, err := EvaluatorConfigFromDict(mappingValue(data, "evaluator"))
	if err != nil {
		return AutoCoordinatingHarnessConfig{}, err
	}
	analyzer, err := EvaluationResultAnalyzerConfigFromDict(mappingValue(data, "evaluation_result_analyzer"))
	if err != nil {
		return AutoCoordinatingHarnessConfig{}, err
	}
	memberData := mappingValue(data, "member_optimizer")
	optimizer, err := MemberOptimizerConfigFromDict(memberData)
	if err != nil {
		return AutoCoordinatingHarnessConfig{}, err
	}
	schedulingData := copyMap(mappingValue(data, "scheduling"))
	promotionData := copyMap(mappingValue(data, "promotion"))
	// legacy scheduling.promotion_policy 迁移（对齐 from_dict 内联逻辑）
	if legacyPolicy, exists := schedulingData["promotion_policy"]; exists {
		if str, ok := legacyPolicy.(string); !ok || str != "epoch_full_evaluation" {
			return AutoCoordinatingHarnessConfig{}, fmt.Errorf("unsupported scheduling.promotion_policy; use promotion.policy_version")
		}
		current := stringValue(promotionData, "policy_version", "legacy_epoch_v1")
		if current != "legacy_epoch_v1" {
			return AutoCoordinatingHarnessConfig{}, fmt.Errorf("conflicting scheduling.promotion_policy and promotion.policy_version; remove the old selector")
		}
		promotionData["policy_version"] = "legacy_epoch_v1"
		delete(schedulingData, "promotion_policy")
	}
	promotion, err := PromotionConfigFromDict(promotionData)
	if err != nil {
		return AutoCoordinatingHarnessConfig{}, err
	}
	// 模型引用三级回退（对齐 model_configs = ModelConfigs(evaluation or evaluator...)）
	if modelConfigs.Evaluation == "" {
		modelConfigs.Evaluation = evaluator.ModelConfigRef
	}
	if modelConfigs.Analysis == "" {
		modelConfigs.Analysis = analyzer.ModelConfigRef
	}
	if modelConfigs.MemberOptimization == "" {
		modelConfigs.MemberOptimization = optimizer.ModelConfigRef
	}
	evaluator.ModelConfigRef = modelConfigs.Evaluation
	analyzer.ModelConfigRef = modelConfigs.Analysis
	optimizer.ModelConfigRef = modelConfigs.MemberOptimization
	// runtime_iteration_hard_limit 继承 evaluator 硬上限（显式配置优先）
	if _, exists := memberData["runtime_iteration_hard_limit"]; !exists {
		optimizer.RuntimeIterationHardLimit = evaluator.SolverMaxIterationsHardLimit
	}
	// promotion.continuous 强制 evaluator.score_mode=continuous（对齐 __post_init__ 的 replace）
	if promotion.Continuous() && evaluator.ScoreMode != "continuous" {
		evaluator.ScoreMode = "continuous"
	}
	cfg := AutoCoordinatingHarnessConfig{
		WorkspaceDir:             stringValue(data, "workspace_dir", ""),
		MaxEpochs:                intValue(data, "max_epochs", 5),
		ModelConfigs:             modelConfigs,
		DataLoader:               DataLoaderConfigFromDict(mappingValue(data, "data_loader")),
		Evaluator:                evaluator,
		EvaluationResultAnalyzer: analyzer,
		MemberOptimizer:          optimizer,
		Scheduling: OrchestratorSchedulingConfig{
			EvaluationStrategy:        stringValue(schedulingData, "evaluation_strategy", "hybrid"),
			CoordinationStrategy:      stringValue(schedulingData, "coordination_strategy", "team_first_single_pass"),
			FullEvaluationEnabled:     boolValueDefault(schedulingData, "full_evaluation_enabled", true),
			FullEvaluationConcurrency: intValue(schedulingData, "full_evaluation_concurrency", 2),
		},
		Promotion: promotion,
	}
	if err := cfg.Validate(); err != nil {
		return cfg, err
	}
	return cfg, nil
}

// Validate 校验顶层配置（对齐 AutoCoordinatingHarnessConfig.validate）
func (c AutoCoordinatingHarnessConfig) Validate() error {
	if c.MaxEpochs < 1 {
		return fmt.Errorf("max_epochs must be greater than or equal to 1")
	}
	if err := c.Scheduling.Validate(); err != nil {
		return err
	}
	if c.DataLoader.BatchSize < 1 {
		return fmt.Errorf("data_loader.batch_size must be greater than or equal to 1")
	}
	if len(c.DataLoader.BatchBalanceKeys) == 0 {
		return fmt.Errorf("data_loader.batch_balance_keys must not be empty")
	}
	if c.EvaluationResultAnalyzer.MaxIssues < 1 {
		return fmt.Errorf("evaluation_result_analyzer.max_issues must be greater than or equal to 1")
	}
	if c.EvaluationResultAnalyzer.EvidenceLimitPerIssue < 1 {
		return fmt.Errorf("evaluation_result_analyzer.evidence_limit_per_issue must be greater than or equal to 1")
	}
	if c.Evaluator.SolverBackend != "deep_agent" && c.Evaluator.SolverBackend != "uapclawswarm" {
		return fmt.Errorf("evaluator.solver_backend must be one of: deep_agent, uapclawswarm")
	}
	if c.Evaluator.UapclawswarmStartupTimeoutSec < 1 {
		return fmt.Errorf("evaluator.uapclawswarm_startup_timeout_sec must be greater than or equal to 1")
	}
	if c.Evaluator.UapclawswarmRuntimeTimeoutSec < 1 {
		return fmt.Errorf("evaluator.uapclawswarm_runtime_timeout_sec must be greater than or equal to 1")
	}
	if c.MemberOptimizer.MaxIssueAttemptsPerBatch < 0 {
		return fmt.Errorf("member_optimizer.max_issue_attempts_per_batch must be greater than or equal to 0")
	}
	if c.MemberOptimizer.MaxRepairRoundsPerBatch < 1 {
		return fmt.Errorf("member_optimizer.max_repair_rounds_per_batch must be greater than or equal to 1")
	}
	if c.MemberOptimizer.SiblingCandidateCount < 1 {
		return fmt.Errorf("member_optimizer.sibling_candidate_count must be greater than or equal to 1")
	}
	return nil
}

// ModelConfigsFromDict 从映射构造模型引用（对齐 ModelConfigs.from_dict）
func ModelConfigsFromDict(data map[string]any) ModelConfigs {
	return ModelConfigs{
		Evaluation:         stringValue(data, "evaluation", ""),
		Analysis:           stringValue(data, "analysis", ""),
		MemberOptimization: stringValue(data, "member_optimization", ""),
	}
}

// DataLoaderConfigFromDict 从映射构造数据载入配置（对齐 DataLoaderConfig.from_dict）
func DataLoaderConfigFromDict(data map[string]any) DataLoaderConfig {
	keys := stringList(data, "batch_balance_keys")
	if len(keys) == 0 {
		keys = []string{"dimension", "difficulty", "source", "task_type"}
	}
	return DataLoaderConfig{
		FilePattern:      stringValue(data, "file_pattern", "*.json"),
		BatchSize:        intValue(data, "batch_size", 1),
		BatchBalanceKeys: keys,
	}
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// mappingValue 取映射字段，非映射返回空映射（对齐 _mapping）
func mappingValue(data map[string]any, key string) map[string]any {
	if raw, exists := data[key]; exists {
		if mapping, ok := raw.(map[string]any); ok {
			return mapping
		}
	}
	return map[string]any{}
}

// stringValue 取字符串字段（对齐 str(data.get(key, default))）
func stringValue(data map[string]any, key, defaultValue string) string {
	if raw, exists := data[key]; exists && raw != nil {
		if str, ok := raw.(string); ok {
			return str
		}
		return fmt.Sprintf("%v", raw)
	}
	return defaultValue
}

// intValue 取整数字段（对齐 _int_value；bool 传入拒绝）
func intValue(data map[string]any, key string, defaultValue int) int {
	raw, exists := data[key]
	if !exists || raw == nil {
		return defaultValue
	}
	if _, isBool := raw.(bool); isBool {
		return defaultValue
	}
	switch typed := raw.(type) {
	case int:
		return typed
	case int64:
		return int(typed)
	case float64:
		return int(typed)
	default:
		return defaultValue
	}
}

// floatValue 取浮点字段（对齐 _float_value；bool 传入拒绝）
func floatValue(data map[string]any, key string, defaultValue float64) float64 {
	raw, exists := data[key]
	if !exists || raw == nil {
		return defaultValue
	}
	if _, isBool := raw.(bool); isBool {
		return defaultValue
	}
	switch typed := raw.(type) {
	case int:
		return float64(typed)
	case int64:
		return float64(typed)
	case float64:
		return typed
	default:
		return defaultValue
	}
}

// boolValue 归一布尔值（对齐 _bool_value 宽松解析，含 true/yes/y/1/on 与 false/no/n/0/off）
func boolValue(raw any, defaultValue bool) (bool, error) {
	if raw == nil {
		return defaultValue, nil
	}
	switch typed := raw.(type) {
	case bool:
		return typed, nil
	case int:
		if typed == 0 || typed == 1 {
			return typed == 1, nil
		}
	case int64:
		if typed == 0 || typed == 1 {
			return typed == 1, nil
		}
	case string:
		normalized := lower(trimSpaces(typed))
		switch normalized {
		case "true", "yes", "y", "1", "on":
			return true, nil
		case "false", "no", "n", "0", "off":
			return false, nil
		}
	}
	return false, fmt.Errorf("expected a boolean value, got %v", raw)
}

// boolValueDefault 取布尔字段，失败时用默认（from_dict 内 _bool_value(default=...) 语义）
func boolValueDefault(data map[string]any, key string, defaultValue bool) bool {
	raw, exists := data[key]
	if !exists {
		return defaultValue
	}
	value, err := boolValue(raw, defaultValue)
	if err != nil {
		return defaultValue
	}
	return value
}

// stringList 取字符串列表（对齐 _string_list：单字符串包装为单元素列表）
func stringList(data map[string]any, key string) []string {
	raw, exists := data[key]
	if !exists || raw == nil {
		return nil
	}
	switch typed := raw.(type) {
	case string:
		return []string{typed}
	case []any:
		out := make([]string, 0, len(typed))
		for _, item := range typed {
			if item == nil {
				continue
			}
			if str, ok := item.(string); ok {
				out = append(out, str)
			} else {
				out = append(out, fmt.Sprintf("%v", item))
			}
		}
		return out
	default:
		return nil
	}
}

// solverIterationLimit 求解迭代上限归一（对齐 _solver_iteration_limit：字符串数字解析、
// 负值/非整数拒绝、错误文案逐字对齐）
func solverIterationLimit(raw any) (int, error) {
	if raw == nil {
		return 300, nil
	}
	if str, isStr := raw.(string); isStr {
		value, ok := parsePositiveInt(str)
		if !ok {
			return 0, fmt.Errorf("evaluator.solver_max_iterations must be a positive integer")
		}
		return value, nil
	}
	value, isInt := toInt(raw)
	if !isInt || value < 1 {
		return 0, fmt.Errorf("evaluator.solver_max_iterations must be a positive integer")
	}
	return value, nil
}

// requireUnitInterval 要求有限且在 [0,1] 区间（对齐 _require_unit_interval）
func requireUnitInterval(value float64, name string) error {
	if math.IsInf(value, 0) || math.IsNaN(value) || value < 0 || value > 1 {
		return fmt.Errorf("%s must be a finite number between 0 and 1", name)
	}
	return nil
}

// structToMap 结构体转映射（evaluator_protocol_config 的 asdict 等价）
func structToMap(value any) map[string]any {
	raw, err := jsonMarshal(value)
	if err != nil {
		return map[string]any{}
	}
	var out map[string]any
	if err := jsonUnmarshal(raw, &out); err != nil {
		return map[string]any{}
	}
	return out
}

// copyMap 浅拷贝映射
func copyMap(src map[string]any) map[string]any {
	out := make(map[string]any, len(src))
	for key, value := range src {
		out[key] = value
	}
	return out
}

// maxInt 两整数取大
func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// lower 小写化
func lower(s string) string {
	buf := []byte(s)
	for i := range buf {
		if buf[i] >= 'A' && buf[i] <= 'Z' {
			buf[i] += 'a' - 'A'
		}
	}
	return string(buf)
}

// trimSpaces 去首尾空白
func trimSpaces(s string) string {
	start, end := 0, len(s)
	for start < end && (s[start] == ' ' || s[start] == '\t' || s[start] == '\n') {
		start++
	}
	for end > start && (s[end-1] == ' ' || s[end-1] == '\t' || s[end-1] == '\n') {
		end--
	}
	return s[start:end]
}

// toInt 宽松整数归一（float64 等常见 YAML 数字形态）
func toInt(raw any) (int, bool) {
	switch typed := raw.(type) {
	case int:
		return typed, true
	case int64:
		return int(typed), true
	case float64:
		return int(typed), true
	default:
		return 0, false
	}
}

// parsePositiveInt 解析正整数字符串
func parsePositiveInt(s string) (int, bool) {
	value, err := strconv.Atoi(trimSpaces(s))
	if err != nil || value < 1 {
		return 0, false
	}
	return value, true
}

// jsonMarshal / jsonUnmarshal JSON 编解码包装（集中 import）
func jsonMarshal(value any) ([]byte, error)   { return json.Marshal(value) }
func jsonUnmarshal(raw []byte, out any) error { return json.Unmarshal(raw, out) }

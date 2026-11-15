package harnessrsi

// CaseMapping 评估用例映射（对齐 Python CaseAlias: dict[str, Any]）
type CaseMapping = map[string]any

// ──────────────────────────── 结构体 ────────────────────────────

// DatasetArtifact 评估器消费的文件系统 JSON 数据集产物（对齐 DatasetArtifact）
type DatasetArtifact struct {
	// DatasetID 数据集唯一标识
	DatasetID string `json:"dataset_id" yaml:"dataset_id"`
	// DatasetDir 数据集目录
	DatasetDir string `json:"dataset_dir" yaml:"dataset_dir"`
	// DatasetFiles 数据集文件清单
	DatasetFiles []string `json:"dataset_files" yaml:"dataset_files"`
	// Cases 用例数（nil 表示未统计）
	Cases *int `json:"cases,omitempty" yaml:"cases,omitempty"`
}

// EvaluationCaseTraceRef 单个评估用例的轨迹与结果引用（对齐 EvaluationCaseTraceRef）
type EvaluationCaseTraceRef struct {
	// CaseID 用例标识
	CaseID string `json:"case_id" yaml:"case_id"`
	// CasePath 用例文件路径
	CasePath string `json:"case_path" yaml:"case_path"`
	// TracePath 轨迹文件路径
	TracePath string `json:"trace_path" yaml:"trace_path"`
	// ResultPath 结果文件路径
	ResultPath string `json:"result_path" yaml:"result_path"`
	// Status 评估状态
	Status string `json:"status" yaml:"status"`
	// Score 分数（nil 表示未评分）
	Score *float64 `json:"score,omitempty" yaml:"score,omitempty"`
	// Metadata 附加元数据
	Metadata map[string]any `json:"metadata" yaml:"metadata"`
}

// EvaluationResultAnalysisInvocation 编排器装配的分析器调用单（对齐 EvaluationResultAnalysisInvocation）
type EvaluationResultAnalysisInvocation struct {
	// EvalRefPath 评估引用路径
	EvalRefPath string `json:"eval_ref_path" yaml:"eval_ref_path"`
	// CaseResultsDir 用例结果目录
	CaseResultsDir string `json:"case_results_dir" yaml:"case_results_dir"`
	// CaseTracesDir 用例轨迹目录
	CaseTracesDir string `json:"case_traces_dir" yaml:"case_traces_dir"`
	// TeamSkillRefPath 团队技能引用路径
	TeamSkillRefPath string `json:"team_skill_ref_path" yaml:"team_skill_ref_path"`
	// HarnessRefsPath harness 引用路径
	HarnessRefsPath string `json:"harness_refs_path" yaml:"harness_refs_path"`
	// OutputDir 输出目录
	OutputDir string `json:"output_dir" yaml:"output_dir"`
	// SourceStage 来源阶段
	SourceStage string `json:"source_stage" yaml:"source_stage"`
	// PriorCandidateFeedback 先前候选反馈
	PriorCandidateFeedback map[string]any `json:"prior_candidate_feedback" yaml:"prior_candidate_feedback"`
}

// TeamIssue 当前 Team 行为的结构化问题（对齐 TeamIssue）
type TeamIssue struct {
	// IssueID 问题唯一标识
	IssueID string `json:"issue_id" yaml:"issue_id"`
	// Category 问题类别
	Category string `json:"category" yaml:"category"`
	// Severity 严重程度
	Severity string `json:"severity" yaml:"severity"`
	// Summary 摘要
	Summary string `json:"summary" yaml:"summary"`
	// AffectedCases 受影响用例清单
	AffectedCases []string `json:"affected_cases" yaml:"affected_cases"`
	// Evidence 证据清单
	Evidence []map[string]any `json:"evidence" yaml:"evidence"`
	// SuspectedTeamScope 疑似团队范围
	SuspectedTeamScope string `json:"suspected_team_scope" yaml:"suspected_team_scope"`
	// OptimizationTarget 优化目标
	OptimizationTarget string `json:"optimization_target" yaml:"optimization_target"`
	// TargetMembers 目标成员清单
	TargetMembers []string `json:"target_members" yaml:"target_members"`
	// Recommendation 建议
	Recommendation string `json:"recommendation" yaml:"recommendation"`
	// Metadata 附加元数据
	Metadata map[string]any `json:"metadata" yaml:"metadata"`
}

// EvaluationResultAnalysisArtifact 文件系统承载的 Team 问题分析产物（对齐 EvaluationResultAnalysisArtifact）
type EvaluationResultAnalysisArtifact struct {
	// AnalysisID 分析唯一标识
	AnalysisID string `json:"analysis_id" yaml:"analysis_id"`
	// AnalysisRefPath 分析引用路径
	AnalysisRefPath string `json:"analysis_ref_path" yaml:"analysis_ref_path"`
	// IssuesPath 问题清单路径
	IssuesPath string `json:"issues_path" yaml:"issues_path"`
	// Issues 结构化问题清单
	Issues []TeamIssue `json:"issues" yaml:"issues"`
	// Metadata 附加元数据
	Metadata map[string]any `json:"metadata" yaml:"metadata"`
}

// ActionDefinition 声明式成员 harness 优化动作（对齐 ActionDefinition）
type ActionDefinition struct {
	// Name 动作名
	Name string `json:"name" yaml:"name"`
	// Group 动作分组
	Group string `json:"group" yaml:"group"`
	// Operation 操作类型
	Operation string `json:"operation" yaml:"operation"`
	// Function 执行函数
	Function string `json:"function" yaml:"function"`
	// Purpose 目的说明
	Purpose string `json:"purpose" yaml:"purpose"`
	// OptimizableModules 可优化模块清单
	OptimizableModules []string `json:"optimizable_modules" yaml:"optimizable_modules"`
	// RequiresSearch 是否需要检索
	RequiresSearch bool `json:"requires_search" yaml:"requires_search"`
	// RequiresInstall 是否需要安装
	RequiresInstall bool `json:"requires_install" yaml:"requires_install"`
	// DependencyResources 依赖资源映射
	DependencyResources map[string]string `json:"dependency_resources" yaml:"dependency_resources"`
	// ParametersSchema 参数 schema
	ParametersSchema map[string]any `json:"parameters_schema" yaml:"parameters_schema"`
	// IsDestructive 是否破坏性
	IsDestructive bool `json:"is_destructive" yaml:"is_destructive"`
	// ValidationRules 校验规则清单
	ValidationRules []string `json:"validation_rules" yaml:"validation_rules"`
}

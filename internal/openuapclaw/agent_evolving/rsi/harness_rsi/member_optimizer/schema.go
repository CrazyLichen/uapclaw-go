// schema.go 提供 member 优化手术系统的数据契约。
// Python: openjiuwen/rsi/harness_rsi/member_optimizer/schema.py（全量对齐，420 行）。
// 全部契约对齐 Python frozen dataclass；YAML 字段名按 Python snake_case。

package memberoptimizer

import (
	harness "github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_evolving/rsi/harness_rsi"
)

// ──────────────────────────── 枚举 ────────────────────────────

// MechanismType 角色内部失败病根分类（封闭分类法）。
// Python: schema.py MechanismType(str, Enum)。
type MechanismType string

const (
	// MechanismTypePrompt 提示词病根
	MechanismTypePrompt MechanismType = "prompt"
	// MechanismTypeInstruction 叮嘱话术病根
	MechanismTypeInstruction MechanismType = "instruction"
	// MechanismTypeWorkflow 工作流病根
	MechanismTypeWorkflow MechanismType = "workflow"
	// MechanismTypeSkill 技能病根
	MechanismTypeSkill MechanismType = "skill"
	// MechanismTypeTool 工具病根
	MechanismTypeTool MechanismType = "tool"
	// MechanismTypeMCP MCP 病根
	MechanismTypeMCP MechanismType = "mcp"
	// MechanismTypeMemory 记忆病根
	MechanismTypeMemory MechanismType = "memory"
	// MechanismTypeContext 上下文病根
	MechanismTypeContext MechanismType = "context"
	// MechanismTypeConfig 配置病根
	MechanismTypeConfig MechanismType = "config"
	// MechanismTypeRail 护栏病根
	MechanismTypeRail MechanismType = "rail"
	// MechanismTypeKnowledge 知识病根
	MechanismTypeKnowledge MechanismType = "knowledge"
	// MechanismTypeInsufficientRoleEvidence 证据不足（不可动刀）
	MechanismTypeInsufficientRoleEvidence MechanismType = "insufficient_role_evidence"
)

// AllMechanismTypes 返回全部 MechanismType 枚举值（Python 枚举成员全集）。
func AllMechanismTypes() []MechanismType {
	return []MechanismType{
		MechanismTypePrompt, MechanismTypeInstruction, MechanismTypeWorkflow,
		MechanismTypeSkill, MechanismTypeTool, MechanismTypeMCP,
		MechanismTypeMemory, MechanismTypeContext, MechanismTypeConfig,
		MechanismTypeRail, MechanismTypeKnowledge, MechanismTypeInsufficientRoleEvidence,
	}
}

// OptimizationSurface 具体被优化的 ExpertHarness 表面。
// Python: schema.py OptimizationSurface(str, Enum)。
type OptimizationSurface string

const (
	// OptimizationSurfaceNone 无可优化表面（仅限证据不足）
	OptimizationSurfaceNone OptimizationSurface = "none"
	// OptimizationSurfaceConfig 配置表面
	OptimizationSurfaceConfig OptimizationSurface = "config"
	// OptimizationSurfacePromptSection 提示词分段表面
	OptimizationSurfacePromptSection OptimizationSurface = "prompt_section"
	// OptimizationSurfaceIdentity 身份表面
	OptimizationSurfaceIdentity OptimizationSurface = "identity"
	// OptimizationSurfaceSoul 灵魂准则表面
	OptimizationSurfaceSoul OptimizationSurface = "soul"
	// OptimizationSurfaceSkill 技能表面
	OptimizationSurfaceSkill OptimizationSurface = "skill"
	// OptimizationSurfaceTool 工具表面
	OptimizationSurfaceTool OptimizationSurface = "tool"
	// OptimizationSurfaceRail 护栏表面
	OptimizationSurfaceRail OptimizationSurface = "rail"
)

// AllOptimizationSurfaces 返回全部 OptimizationSurface 枚举值。
func AllOptimizationSurfaces() []OptimizationSurface {
	return []OptimizationSurface{
		OptimizationSurfaceNone, OptimizationSurfaceConfig, OptimizationSurfacePromptSection,
		OptimizationSurfaceIdentity, OptimizationSurfaceSoul, OptimizationSurfaceSkill,
		OptimizationSurfaceTool, OptimizationSurfaceRail,
	}
}

// FailureSignature 角色内部失败特征分类。
// Python: schema.py FailureSignature(str, Enum)。
type FailureSignature string

const (
	// FailureSignatureRepeatedFailureLoop 重复失败循环
	FailureSignatureRepeatedFailureLoop FailureSignature = "repeated_failure_loop"
	// FailureSignatureToolCallFailure 工具调用失败
	FailureSignatureToolCallFailure FailureSignature = "tool_call_failure"
	// FailureSignatureStalledObjective 目标停滞
	FailureSignatureStalledObjective FailureSignature = "stalled_objective"
	// FailureSignatureMissedExplorationOrCapability 错失探索或能力
	FailureSignatureMissedExplorationOrCapability FailureSignature = "missed_exploration_or_capability"
	// FailureSignatureStaleMemoryOrContext 记忆或上下文过期
	FailureSignatureStaleMemoryOrContext FailureSignature = "stale_memory_or_context"
	// FailureSignatureSkillCodeFailure 技能代码失败
	FailureSignatureSkillCodeFailure FailureSignature = "skill_code_failure"
	// FailureSignatureSubagentOrWorkflowHandoffFailure 子代理或工作流交接失败
	FailureSignatureSubagentOrWorkflowHandoffFailure FailureSignature = "subagent_or_workflow_handoff_failure"
	// FailureSignaturePromptInstructionMismatch 提示词与叮嘱错配
	FailureSignaturePromptInstructionMismatch FailureSignature = "prompt_instruction_mismatch"
	// FailureSignatureConfigOrRailMismatch 配置或护栏错配
	FailureSignatureConfigOrRailMismatch FailureSignature = "config_or_rail_mismatch"
	// FailureSignatureInsufficientRoleEvidence 角色证据不足
	FailureSignatureInsufficientRoleEvidence FailureSignature = "insufficient_role_evidence"
)

// AllFailureSignatures 返回全部 FailureSignature 枚举值。
func AllFailureSignatures() []FailureSignature {
	return []FailureSignature{
		FailureSignatureRepeatedFailureLoop, FailureSignatureToolCallFailure,
		FailureSignatureStalledObjective, FailureSignatureMissedExplorationOrCapability,
		FailureSignatureStaleMemoryOrContext, FailureSignatureSkillCodeFailure,
		FailureSignatureSubagentOrWorkflowHandoffFailure, FailureSignaturePromptInstructionMismatch,
		FailureSignatureConfigOrRailMismatch, FailureSignatureInsufficientRoleEvidence,
	}
}

// ──────────────────────────── 结构体 ────────────────────────────

// MemberRoleCandidate 从 harness refs 解析出的 member 优化候选角色。
// Python: schema.py:73-81 MemberRoleCandidate（frozen dataclass → Go struct，
// 偏差 D-5 见 17.4b1 spec §6）。
type MemberRoleCandidate struct {
	// Role 角色名
	Role string `yaml:"role" json:"role"`
	// HarnessRefPath harness 引用文件路径
	HarnessRefPath string `yaml:"harness_ref_path" json:"harness_ref_path"`
	// MemberName 成员名（可空，默认与 role 相同由解析方填）
	MemberName string `yaml:"member_name" json:"member_name"`
	// Description 描述
	Description string `yaml:"description" json:"description"`
	// Metadata 元数据
	Metadata map[string]any `yaml:"metadata" json:"metadata"`
}

// RoleIssueAttribution 一个 TeamIssue 到具体角色的定责结果。
// Python: schema.py RoleIssueAttribution。
type RoleIssueAttribution struct {
	// IssueID 问题 ID
	IssueID string `yaml:"issue_id" json:"issue_id"`
	// Role 归属角色
	Role string `yaml:"role" json:"role"`
	// HarnessRefPath 角色 harness 引用路径
	HarnessRefPath string `yaml:"harness_ref_path" json:"harness_ref_path"`
	// Confidence 归因置信度 [0,1]
	Confidence float64 `yaml:"confidence" json:"confidence"`
	// Evidence 证据列表
	Evidence []map[string]any `yaml:"evidence" json:"evidence"`
	// TraceRefs 轨迹引用
	TraceRefs []map[string]string `yaml:"trace_refs" json:"trace_refs"`
	// Rationale 归因理由
	Rationale string `yaml:"rationale" json:"rationale"`
	// MemberName 成员名
	MemberName string `yaml:"member_name" json:"member_name"`
	// RoleOutputRefs 定责输出引用
	RoleOutputRefs []map[string]string `yaml:"role_output_refs" json:"role_output_refs"`
}

// UnassignedMemberIssue 未能定责到具体角色的问题。
// Python: schema.py UnassignedMemberIssue。
type UnassignedMemberIssue struct {
	// IssueID 问题 ID
	IssueID string `yaml:"issue_id" json:"issue_id"`
	// Reason 未能定责的原因
	Reason string `yaml:"reason" json:"reason"`
	// Evidence 证据列表
	Evidence []map[string]any `yaml:"evidence" json:"evidence"`
	// TraceRefs 轨迹引用
	TraceRefs []map[string]string `yaml:"trace_refs" json:"trace_refs"`
	// Rationale 理由
	Rationale string `yaml:"rationale" json:"rationale"`
}

// RoleAttributionReport 第 1 步定责报告。
// Python: schema.py RoleAttributionReport。
type RoleAttributionReport struct {
	// AttributionID 归因批次 ID
	AttributionID string `yaml:"attribution_id" json:"attribution_id"`
	// SourceEvalRefPath 源 eval_ref 路径
	SourceEvalRefPath string `yaml:"source_eval_ref_path" json:"source_eval_ref_path"`
	// SourceAnalysisResultPath 源诊断报告路径
	SourceAnalysisResultPath string `yaml:"source_analysis_result_path" json:"source_analysis_result_path"`
	// HarnessRefsPath 角色档案清单路径
	HarnessRefsPath string `yaml:"harness_refs_path" json:"harness_refs_path"`
	// CandidateRoles 候选角色
	CandidateRoles []MemberRoleCandidate `yaml:"candidate_roles" json:"candidate_roles"`
	// AssignedRoleIssues 已定责问题
	AssignedRoleIssues []RoleIssueAttribution `yaml:"assigned_role_issues" json:"assigned_role_issues"`
	// UnassignedIssues 未定责问题
	UnassignedIssues []UnassignedMemberIssue `yaml:"unassigned_issues" json:"unassigned_issues"`
	// Warnings 警告
	Warnings []string `yaml:"warnings" json:"warnings"`
	// Metadata 元数据
	Metadata map[string]any `yaml:"metadata" json:"metadata"`
}

// RoleMechanismAttribution 单个问题的角色内部病根归因。
// Python: schema.py RoleMechanismAttribution。
type RoleMechanismAttribution struct {
	// IssueID 问题 ID
	IssueID string `yaml:"issue_id" json:"issue_id"`
	// Role 角色
	Role string `yaml:"role" json:"role"`
	// MechanismType 病根类型（封闭分类）
	MechanismType string `yaml:"mechanism_type" json:"mechanism_type"`
	// FailureSignature 失败特征（封闭分类）
	FailureSignature string `yaml:"failure_signature" json:"failure_signature"`
	// Confidence 置信度 [0,1]
	Confidence float64 `yaml:"confidence" json:"confidence"`
	// OptimizationSurface 优化表面（空或封闭枚举）
	OptimizationSurface string `yaml:"optimization_surface" json:"optimization_surface"`
	// Evidence 证据列表
	Evidence []map[string]any `yaml:"evidence" json:"evidence"`
	// EvidenceRefs 证据引用
	EvidenceRefs []map[string]string `yaml:"evidence_refs" json:"evidence_refs"`
	// Rationale 理由
	Rationale string `yaml:"rationale" json:"rationale"`
}

// MechanismAttributionReport 第 2 步病根归因报告。
// Python: schema.py MechanismAttributionReport。
type MechanismAttributionReport struct {
	// MechanismAttributionID 归因批次 ID
	MechanismAttributionID string `yaml:"mechanism_attribution_id" json:"mechanism_attribution_id"`
	// SourceRoleAttributionPath 源定责报告路径
	SourceRoleAttributionPath string `yaml:"source_role_attribution_path" json:"source_role_attribution_path"`
	// RoleMechanisms 按角色分组的病根归因
	RoleMechanisms map[string][]RoleMechanismAttribution `yaml:"role_mechanisms" json:"role_mechanisms"`
	// SkippedRoles 被跳过的角色
	SkippedRoles []string `yaml:"skipped_roles" json:"skipped_roles"`
	// Warnings 警告
	Warnings []string `yaml:"warnings" json:"warnings"`
	// Metadata 元数据
	Metadata map[string]any `yaml:"metadata" json:"metadata"`
}

// MemberOptimizationTarget 本轮被选中动手术的角色。
// Python: schema.py MemberOptimizationTarget。
type MemberOptimizationTarget struct {
	// Role 角色
	Role string `yaml:"role" json:"role"`
	// HarnessRefPath harness 引用路径
	HarnessRefPath string `yaml:"harness_ref_path" json:"harness_ref_path"`
	// AttributedIssueIds 关联问题 ID
	AttributedIssueIDs []string `yaml:"attributed_issue_ids" json:"attributed_issue_ids"`
	// Confidence 平均置信度
	Confidence float64 `yaml:"confidence" json:"confidence"`
	// Reason 入选理由
	Reason string `yaml:"reason" json:"reason"`
	// EvidenceRefs 证据引用
	EvidenceRefs []map[string]string `yaml:"evidence_refs" json:"evidence_refs"`
	// MechanismTypes 病根类型列表
	MechanismTypes []string `yaml:"mechanism_types" json:"mechanism_types"`
	// OptimizationSurfaces 优化表面列表
	OptimizationSurfaces []string `yaml:"optimization_surfaces" json:"optimization_surfaces"`
	// MemberName 成员名
	MemberName string `yaml:"member_name" json:"member_name"`
	// Metadata 元数据
	Metadata map[string]any `yaml:"metadata" json:"metadata"`
}

// UnselectedAttribution 已定责但本轮未入选的问题归因。
// Python: schema.py UnselectedAttribution。
type UnselectedAttribution struct {
	// IssueID 问题 ID
	IssueID string `yaml:"issue_id" json:"issue_id"`
	// Role 角色
	Role string `yaml:"role" json:"role"`
	// Reason 落选原因
	Reason string `yaml:"reason" json:"reason"`
	// Confidence 置信度
	Confidence float64 `yaml:"confidence" json:"confidence"`
}

// MemberSelectionReport 选择器输出——入选目标与落选归因。
// Python: schema.py MemberSelectionReport。
type MemberSelectionReport struct {
	// SelectionID 选择批次 ID
	SelectionID string `yaml:"selection_id" json:"selection_id"`
	// SourceRoleAttributionPath 源定责报告路径
	SourceRoleAttributionPath string `yaml:"source_role_attribution_path" json:"source_role_attribution_path"`
	// SourceMechanismAttributionPath 源病根归因报告路径
	SourceMechanismAttributionPath string `yaml:"source_mechanism_attribution_path" json:"source_mechanism_attribution_path"`
	// Targets 入选目标
	Targets []MemberOptimizationTarget `yaml:"targets" json:"targets"`
	// UnselectedAttributions 落选归因
	UnselectedAttributions []UnselectedAttribution `yaml:"unselected_attributions" json:"unselected_attributions"`
	// Metadata 元数据
	Metadata map[string]any `yaml:"metadata" json:"metadata"`
}

// MemberOptimizationAction 一条受约束的成员优化手术。
// Python: schema.py MemberOptimizationAction。
type MemberOptimizationAction struct {
	// ActionID 手术 ID
	ActionID string `yaml:"action_id" json:"action_id"`
	// Role 角色
	Role string `yaml:"role" json:"role"`
	// ActionGroup 手术分组
	ActionGroup string `yaml:"action_group" json:"action_group"`
	// Operation 操作
	Operation string `yaml:"operation" json:"operation"`
	// ActionType 动作类型
	ActionType string `yaml:"action_type" json:"action_type"`
	// TargetPath 目标路径
	TargetPath string `yaml:"target_path" json:"target_path"`
	// Description 描述
	Description string `yaml:"description" json:"description"`
	// Rationale 理由
	Rationale string `yaml:"rationale" json:"rationale"`
	// AttributedIssueIds 关联问题 ID
	AttributedIssueIDs []string `yaml:"attributed_issue_ids" json:"attributed_issue_ids"`
	// DependsOn 依赖的其他手术 ID
	DependsOn []string `yaml:"depends_on" json:"depends_on"`
	// RunIf 执行条件（默认 dependency_succeeded）
	RunIf string `yaml:"run_if" json:"run_if"`
	// AllowedSkills 允许的技能
	AllowedSkills []string `yaml:"allowed_skills" json:"allowed_skills"`
	// AllowedTools 允许的工具
	AllowedTools []string `yaml:"allowed_tools" json:"allowed_tools"`
	// CandidateQuery 候选查询
	CandidateQuery string `yaml:"candidate_query" json:"candidate_query"`
	// InstallRef 安装引用
	InstallRef string `yaml:"install_ref" json:"install_ref"`
	// ExpectedEffect 预期效果
	ExpectedEffect string `yaml:"expected_effect" json:"expected_effect"`
	// RiskNotes 风险说明
	RiskNotes []string `yaml:"risk_notes" json:"risk_notes"`
	// DeclaredWritePaths 声明写路径
	DeclaredWritePaths []string `yaml:"declared_write_paths" json:"declared_write_paths"`
	// Constraints 约束
	Constraints map[string]any `yaml:"constraints" json:"constraints"`
	// MemberName 成员名
	MemberName string `yaml:"member_name" json:"member_name"`
}

// ──────────────────────────── 导出函数 ────────────────────────────

// NewMemberOptimizationArtifact 构造工件并填 Python dataclass 默认值。
func NewMemberOptimizationArtifact() MemberOptimizationArtifact {
	return MemberOptimizationArtifact{
		PromotionStatus: "not_evaluated",
	}
}

// NewMemberOptimizationAction 构造手术并填 Python dataclass 默认值。
func NewMemberOptimizationAction(actionID, role, actionGroup, operation, actionType, targetPath string) MemberOptimizationAction {
	return MemberOptimizationAction{
		ActionID:    actionID,
		Role:        role,
		ActionGroup: actionGroup,
		Operation:   operation,
		ActionType:  actionType,
		TargetPath:  targetPath,
		RunIf:       "dependency_succeeded",
	}
}

// MemberOptimizationPlan 依赖感知的成员优化手术单。
// Python: schema.py MemberOptimizationPlan。
type MemberOptimizationPlan struct {
	// PlanID 手术单 ID
	PlanID string `yaml:"plan_id" json:"plan_id"`
	// Targets 入选目标
	Targets []MemberOptimizationTarget `yaml:"targets" json:"targets"`
	// Actions 手术列表
	Actions []MemberOptimizationAction `yaml:"actions" json:"actions"`
	// ActionWaves 波次（LLM 声明，执行前按依赖重算）
	ActionWaves [][]string `yaml:"action_waves" json:"action_waves"`
	// Metadata 元数据
	Metadata map[string]any `yaml:"metadata" json:"metadata"`
}

// MemberActionExecutionResult 单条手术的执行结果。
// Python: schema.py MemberActionExecutionResult。
type MemberActionExecutionResult struct {
	// ActionID 手术 ID
	ActionID string `yaml:"action_id" json:"action_id"`
	// Role 角色
	Role string `yaml:"role" json:"role"`
	// Status 状态
	Status string `yaml:"status" json:"status"`
	// WorktreePath 手术区路径
	WorktreePath string `yaml:"worktree_path" json:"worktree_path"`
	// ArtifactPath 产物路径
	ArtifactPath string `yaml:"artifact_path" json:"artifact_path"`
	// ChangedFiles 变更文件
	ChangedFiles []string `yaml:"changed_files" json:"changed_files"`
	// DeclaredWritePaths 声明写路径
	DeclaredWritePaths []string `yaml:"declared_write_paths" json:"declared_write_paths"`
	// MergeStatus 合并状态
	MergeStatus string `yaml:"merge_status" json:"merge_status"`
	// MergeArtifactPath 合并产物路径
	MergeArtifactPath string `yaml:"merge_artifact_path" json:"merge_artifact_path"`
	// Error 错误
	Error string `yaml:"error" json:"error"`
	// MemberName 成员名
	MemberName string `yaml:"member_name" json:"member_name"`
}

// MemberActionMergeResult 角色局部子波合并进整合区的结果。
// Python: schema.py MemberActionMergeResult。
type MemberActionMergeResult struct {
	// Role 角色
	Role string `yaml:"role" json:"role"`
	// WaveIndex 波次序号
	WaveIndex int `yaml:"wave_index" json:"wave_index"`
	// SubwaveIndex 子波序号
	SubwaveIndex int `yaml:"subwave_index" json:"subwave_index"`
	// ActionIds 参与合并的手术 ID
	ActionIDs []string `yaml:"action_ids" json:"action_ids"`
	// Status 状态
	Status string `yaml:"status" json:"status"`
	// MergedFiles 已合并文件
	MergedFiles []string `yaml:"merged_files" json:"merged_files"`
	// Conflicts 冲突
	Conflicts []string `yaml:"conflicts" json:"conflicts"`
	// ArtifactPath 产物路径
	ArtifactPath string `yaml:"artifact_path" json:"artifact_path"`
	// Error 错误
	Error string `yaml:"error" json:"error"`
}

// VerificationCheck 单项验证检查结果。
// Python: schema.py VerificationCheck。
type VerificationCheck struct {
	// Name 检查名
	Name string `yaml:"name" json:"name"`
	// Status 状态
	Status string `yaml:"status" json:"status"`
	// Notes 备注
	Notes string `yaml:"notes" json:"notes"`
	// Error 错误
	Error string `yaml:"error" json:"error"`
}

// RoleVerificationResult 单角色验证结果。
// Python: schema.py RoleVerificationResult。
type RoleVerificationResult struct {
	// Role 角色
	Role string `yaml:"role" json:"role"`
	// Status 状态
	Status string `yaml:"status" json:"status"`
	// Checks 检查列表
	Checks []VerificationCheck `yaml:"checks" json:"checks"`
	// Repairable 是否可修复
	Repairable bool `yaml:"repairable" json:"repairable"`
	// Error 错误
	Error string `yaml:"error" json:"error"`
}

// MemberVerificationResult 全角色验证结果。
// Python: schema.py MemberVerificationResult。
type MemberVerificationResult struct {
	// Status 总体状态
	Status string `yaml:"status" json:"status"`
	// CheckedRoles 已检查角色
	CheckedRoles []string `yaml:"checked_roles" json:"checked_roles"`
	// Checks 全局检查列表
	Checks []VerificationCheck `yaml:"checks" json:"checks"`
	// RoleResults 按角色验证结果
	RoleResults map[string]RoleVerificationResult `yaml:"role_results" json:"role_results"`
	// Repairable 是否可修复
	Repairable bool `yaml:"repairable" json:"repairable"`
	// Metadata 元数据
	Metadata map[string]any `yaml:"metadata" json:"metadata"`
}

// RepairItem 一次对角色整合区的补救。
// Python: schema.py RepairItem。
type RepairItem struct {
	// Role 角色
	Role string `yaml:"role" json:"role"`
	// Action 动作
	Action string `yaml:"action" json:"action"`
	// Description 描述
	Description string `yaml:"description" json:"description"`
	// Status 状态
	Status string `yaml:"status" json:"status"`
	// Error 错误
	Error string `yaml:"error" json:"error"`
}

// MemberFixResult 验证失败后的补救结果。
// Python: schema.py MemberFixResult。
type MemberFixResult struct {
	// Status 总体状态
	Status string `yaml:"status" json:"status"`
	// Repairs 补救列表
	Repairs []RepairItem `yaml:"repairs" json:"repairs"`
	// VerificationPath 验证结果路径
	VerificationPath string `yaml:"verification_path" json:"verification_path"`
	// FinalVerificationStatus 最终验证状态
	FinalVerificationStatus string `yaml:"final_verification_status" json:"final_verification_status"`
	// Metadata 元数据
	Metadata map[string]any `yaml:"metadata" json:"metadata"`
}

// RoleResult 供 artifact 与编排方消费的单角色优化结果。
// Python: schema.py RoleResult。
type RoleResult struct {
	// Status 状态
	Status string `yaml:"status" json:"status"`
	// BeforeHarnessRefPath 术前 harness 引用路径
	BeforeHarnessRefPath string `yaml:"before_harness_ref_path" json:"before_harness_ref_path"`
	// AfterHarnessRefPath 术后 harness 引用路径
	AfterHarnessRefPath string `yaml:"after_harness_ref_path" json:"after_harness_ref_path"`
	// ActionIds 执行的手术 ID
	ActionIDs []string `yaml:"action_ids" json:"action_ids"`
	// VerificationStatus 验证状态
	VerificationStatus string `yaml:"verification_status" json:"verification_status"`
	// Error 错误
	Error string `yaml:"error" json:"error"`
	// Metadata 元数据
	Metadata map[string]any `yaml:"metadata" json:"metadata"`
}

// MemberOptimizationArtifact 一次 member 优化运行的落盘工件。
// Python: schema.py MemberOptimizationArtifact（多角色全量支持，legacy role 字段保留）。
type MemberOptimizationArtifact struct {
	// OptimizationID 优化批次 ID
	OptimizationID string `yaml:"optimization_id" json:"optimization_id"`
	// OutputDir 输出目录
	OutputDir string `yaml:"output_dir" json:"output_dir"`
	// Status 状态
	Status string `yaml:"status" json:"status"`
	// OptimizedHarnessRefsPath 优化后角色档案清单路径
	OptimizedHarnessRefsPath string `yaml:"optimized_harness_refs_path" json:"optimized_harness_refs_path"`
	// Roles 涉及角色
	Roles []string `yaml:"roles" json:"roles"`
	// CandidateReadyRoles 候选就绪角色
	CandidateReadyRoles []string `yaml:"candidate_ready_roles" json:"candidate_ready_roles"`
	// PublishedRoles 已发布角色
	PublishedRoles []string `yaml:"published_roles" json:"published_roles"`
	// StagedRoles 已暂存角色
	StagedRoles []string `yaml:"staged_roles" json:"staged_roles"`
	// VerifiedRoles 已验证角色
	VerifiedRoles []string `yaml:"verified_roles" json:"verified_roles"`
	// PromotedRoles 已晋级角色
	PromotedRoles []string `yaml:"promoted_roles" json:"promoted_roles"`
	// PromotionStatus 晋级状态（默认 not_evaluated）
	PromotionStatus string `yaml:"promotion_status" json:"promotion_status"`
	// FailedRoles 失败角色
	FailedRoles []string `yaml:"failed_roles" json:"failed_roles"`
	// SkippedRoles 跳过角色
	SkippedRoles []string `yaml:"skipped_roles" json:"skipped_roles"`
	// RoleAttributionPath 定责报告路径
	RoleAttributionPath string `yaml:"role_attribution_path" json:"role_attribution_path"`
	// MechanismAttributionPath 病根归因报告路径
	MechanismAttributionPath string `yaml:"mechanism_attribution_path" json:"mechanism_attribution_path"`
	// SelectionPath 选择报告路径
	SelectionPath string `yaml:"selection_path" json:"selection_path"`
	// PlanPath 手术单路径
	PlanPath string `yaml:"plan_path" json:"plan_path"`
	// ExecutionResultPath 执行结果路径
	ExecutionResultPath string `yaml:"execution_result_path" json:"execution_result_path"`
	// VerificationPath 验证结果路径
	VerificationPath string `yaml:"verification_path" json:"verification_path"`
	// FixResultPath 补救结果路径
	FixResultPath string `yaml:"fix_result_path" json:"fix_result_path"`
	// RoleResults 按角色结果
	RoleResults map[string]RoleResult `yaml:"role_results" json:"role_results"`
	// Metadata 元数据
	Metadata map[string]any `yaml:"metadata" json:"metadata"`
	// Role legacy 兼容字段（Python 注释：prefer roles/published_roles/failed_roles）
	Role string `yaml:"role" json:"role"`
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// 编译期对齐 harness 顶层 schema 的契约引用（Python schema.py noqa F401 重导出语义）。
var (
	_ harness.TeamIssue
	_ harness.ActionDefinition
)

// ──────────────────────────── 导出函数（agents/factory 枚举注入） ────────────────────────────

// MechanismTypeValuesExcludingRail 返回除 rail 外的全部病根枚举值。
// Python: factory.py:513-515 _mechanism_type_values（if item is not MechanismType.RAIL）。
func MechanismTypeValuesExcludingRail() []string {
	out := []string{}
	for _, v := range AllMechanismTypes() {
		if v != MechanismTypeRail {
			out = append(out, string(v))
		}
	}
	return out
}

// FailureSignatureValuesExcludingConfigOrRailMismatch 返回除 config_or_rail_mismatch 外的全部失败特征。
// Python: factory.py:517-519。
func FailureSignatureValuesExcludingConfigOrRailMismatch() []string {
	out := []string{}
	for _, v := range AllFailureSignatures() {
		if v != FailureSignatureConfigOrRailMismatch {
			out = append(out, string(v))
		}
	}
	return out
}

// OptimizationSurfaceValuesExcludingRail 返回除 rail 外的全部优化面枚举值。
// Python: factory.py:521-523。
func OptimizationSurfaceValuesExcludingRail() []string {
	out := []string{}
	for _, v := range AllOptimizationSurfaces() {
		if v != OptimizationSurfaceRail {
			out = append(out, string(v))
		}
	}
	return out
}

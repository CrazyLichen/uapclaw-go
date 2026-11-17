// optimizer.go member harness 优化管线门面。
// Python: member_optimizer/optimizer.py（全量对齐，1,366 行；publish 与
// helpers 在 optimizer_publish.go）。

package memberoptimizer

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	harnessrsi "github.com/uapclaw/uapclaw-go/internal/evolving/rsi/harness_rsi"
	"github.com/uapclaw/uapclaw-go/internal/evolving/rsi/harness_rsi/member_optimizer/action_groups"
	"github.com/google/uuid"
	"gopkg.in/yaml.v3"
)

// ──────────────────────────── 结构体 ────────────────────────────

// MemberOptimizerConfig 门面配置。
// Python: optimizer.py 外部构造注入的 config（字段面按 __init__ 消费清单收拢）。
type MemberOptimizerConfig struct {
	// ModelConfigRef 模型配置引用
	ModelConfigRef string
	// ActionGroupConfigs 动作组配置路径清单
	ActionGroupConfigs []string
	// OutputRepair 输出修复配置
	OutputRepair *harnessrsi.OutputRepairConfig
	// AttributionRetryLimit 归因重试上限
	AttributionRetryLimit int
	// MaxCasesPerIssue 每问题最大案例数
	MaxCasesPerIssue int
	// MaxTraceExcerptChars 轨迹摘录字符上限
	MaxTraceExcerptChars int
	// MaxResultExcerptChars 结果摘录字符上限
	MaxResultExcerptChars int
	// ExecutionConcurrency 全局执行并发
	ExecutionConcurrency int
	// RoleExecutionConcurrency 角色执行并发
	RoleExecutionConcurrency int
	// ActionExecutionConcurrencyPerRole 角色内动作并发
	ActionExecutionConcurrencyPerRole int
	// AgentSkillsDirs agent 技能目录
	AgentSkillsDirs []string
	// RuntimeIterationHardLimit 迭代硬上限
	RuntimeIterationHardLimit int
	// StageRetryLimit 阶段重试上限
	StageRetryLimit int
	// MinAttributionConfidence 最小归因置信度
	MinAttributionConfidence float64
	// MaxRolesPerRun 每轮最大角色数
	MaxRolesPerRun int
	// ActionPlanningMaxAttempts 开单最大尝试
	ActionPlanningMaxAttempts int
	// MaxActionsPerPlan 单方案最大动作数
	MaxActionsPerPlan int
	// AllowedActionGroups 受限动作组
	AllowedActionGroups []string
	// AllowedPromptSurfaces 受限 prompt 面
	AllowedPromptSurfaces []string
	// Freeze 冻结开关（True → noop skipped）
	Freeze bool
	// AllowEmptyHarnessRefsNoop 允许空候选角色 noop
	AllowEmptyHarnessRefsNoop bool
}

// publishRequest 发布请求（Python _PublishRequest）。
type publishRequest struct {
	runDir                     string
	pathLayout                 *MemberOptimizerPathLayout
	outputRoot                 string
	selectionReport            *MemberSelectionReport
	plan                       *MemberOptimizationPlan
	executionResults           []*MemberActionExecutionResult
	verificationResult         *MemberVerificationResult
	evalRefPath                string
	analysisResultPath         string
	harnessRefsPath            string
	roleAttrPath               string
	mechanismAttrPath          string
	selectionPath              string
	planPath                   string
	executionResultPath        string
	verificationPath           string
	fixResultPath              string
	deferPublish               bool
	candidateManifestPath      string
	optimizationHypothesesPath string
}

// harnessRefsWriteRequest current_harness_refs 写入请求（Python _HarnessRefsWriteRequest）。
type harnessRefsWriteRequest struct {
	outputRoot        string
	harnessRefsPath   string
	publishedRoles    []string
	failedRoles       []string
	skippedRoles      []string
	roleResults       map[string]RoleResult
	pathLayout        *MemberOptimizerPathLayout
	refsPath          string
	publishedRefPaths map[string]string
	cumulativePublish bool
}

// MemberOptimizer member harness 优化管线编排门面。
// Python: optimizer.py:116-157 MemberOptimizer。
type MemberOptimizer struct {
	config                  *MemberOptimizerConfig
	actionGroupConfigPaths  []string
	roleAttributor          *RoleAttributor
	mechanismAttributor     *MechanismAttributor
	memberSelector          selectorLike
	actionPlanner           *MemberActionPlanner
	actionExecutor          *MemberActionExecutor
	verifier                *HarnessChangeVerifier
}

// selectorLike 选择器接口（注入替身用）。
type selectorLike interface {
	Select(roleAttributionReport *RoleAttributionReport, mechanismAttributionReport *MechanismAttributionReport, opts SelectorOptions) *MemberSelectionReport
}

// NewMemberOptimizer 构造门面（各阶段 nil 用默认真身）。
// Python: optimizer.py:123-157 __init__。
func NewMemberOptimizer(config *MemberOptimizerConfig, roleAttributor *RoleAttributor, mechanismAttributor *MechanismAttributor, memberSelector selectorLike, actionPlanner *MemberActionPlanner, actionExecutor *MemberActionExecutor, verifier *HarnessChangeVerifier) *MemberOptimizer {
	m := &MemberOptimizer{config: config, actionGroupConfigPaths: append([]string{}, config.ActionGroupConfigs...)}
	if roleAttributor != nil {
		m.roleAttributor = roleAttributor
	} else {
		m.roleAttributor = NewRoleAttributor(2, config.OutputRepair)
	}
	if mechanismAttributor != nil {
		m.mechanismAttributor = mechanismAttributor
	} else {
		m.mechanismAttributor = NewMechanismAttributor(2, config.OutputRepair)
	}
	if memberSelector != nil {
		m.memberSelector = memberSelector
	} else {
		m.memberSelector = defaultSelector{}
	}
	if actionPlanner != nil {
		m.actionPlanner = actionPlanner
	} else {
		maxAttempts := config.ActionPlanningMaxAttempts
		m.actionPlanner = NewMemberActionPlanner(nil, config.OutputRepair, &maxAttempts)
	}
	if actionExecutor != nil {
		m.actionExecutor = actionExecutor
	} else {
		m.actionExecutor = NewMemberActionExecutor(nil, nil,
			config.ExecutionConcurrency, config.RoleExecutionConcurrency,
			config.ActionExecutionConcurrencyPerRole,
			config.AgentSkillsDirs, config.OutputRepair, config.RuntimeIterationHardLimit)
	}
	if verifier != nil {
		m.verifier = verifier
	} else {
		m.verifier = NewHarnessChangeVerifier(nil, config.AgentSkillsDirs, config.OutputRepair, config.RuntimeIterationHardLimit)
	}
	return m
}

// defaultSelector 真身选择器适配（包级函数 SelectMemberTargets）。
type defaultSelector struct{}

// Select 转调包级选择函数。
func (defaultSelector) Select(roleAttributionReport *RoleAttributionReport, mechanismAttributionReport *MechanismAttributionReport, opts SelectorOptions) *MemberSelectionReport {
	return SelectMemberTargets(roleAttributionReport, mechanismAttributionReport, opts)
}

// RegisterActionGroup 登记动作组配置路径。
// Python: optimizer.py:158-161 register_action_group。
func (m *MemberOptimizer) RegisterActionGroup(actionGroupConfigPath string) {
	if actionGroupConfigPath != "" {
		m.actionGroupConfigPaths = append(m.actionGroupConfigPaths, actionGroupConfigPath)
	}
}

// OptimizeExecuteParams Optimize 执行参数（Python optimize 关键字收拢）。
type OptimizeExecuteParams struct {
	// DeferPublish 延后发布（候选态）
	DeferPublish bool
	// RejectedCapabilities 被拒能力历史
	RejectedCapabilities []map[string]any
	// SingleHarness 单 harness 直连模式
	SingleHarness bool
	// OptimizationHypothesesPath 不可变假设契约路径
	OptimizationHypothesesPath string
	// OptimizationIssueIds 单 harness 问题作用域
	OptimizationIssueIds []string
	// OptimizationExperience 优化经验
	OptimizationExperience map[string]any
}

// Optimize 执行完整 member 优化管线，返回 member_optimization_ref.yaml 路径。
// Python: optimizer.py:163-565 optimize。
func (m *MemberOptimizer) Optimize(ctx context.Context, evalRefPath, analysisResultPath, harnessRefsPath, outputDir string, params OptimizeExecuteParams) (string, error) {
	if m.config.Freeze {
		return m.writeNoopArtifact(evalRefPath, analysisResultPath, harnessRefsPath, outputDir, noopParams{
			status: "skipped", reason: "optimizer is frozen", deferPublish: params.DeferPublish,
		})
	}
	outputRoot, err := expandUser(outputDir)
	if err != nil {
		return "", err
	}
	if resolved, aerr := filepath.Abs(outputRoot); aerr == nil {
		outputRoot = resolved
	}
	if err := os.MkdirAll(outputRoot, 0o755); err != nil {
		return "", err
	}
	if params.DeferPublish {
		if reusableRef := findReusablePendingOptimization(outputRoot, evalRefPath, analysisResultPath, harnessRefsPath); reusableRef != "" {
			return reusableRef, nil
		}
	}

	analysisRef := LoadAnalysisRef(analysisResultPath)
	if err := analysisRef.RequireUsable(); err != nil {
		return "", err
	}
	if _, err := LoadMemberOptimizerModel(m.config.ModelConfigRef); err != nil {
		return "", err
	}

	pathLayout := NewMemberOptimizerPathLayout(outputRoot)
	runDir := allocateOptimizationDir(outputRoot)

	evalRef, err := LoadEvalRef(evalRefPath)
	if err != nil {
		return "", err
	}
	teamIssues := ResolveTeamIssues(analysisRef)
	candidateRoles := ResolveCandidateRoles(harnessRefsPath, &evalRef, evalRef.TeamSkillRefPath)

	if len(candidateRoles) == 0 && !m.config.AllowEmptyHarnessRefsNoop {
		return "", fmt.Errorf("No candidate roles found and allow_empty_harness_refs_noop=False. harness_refs_path=%s, eval_ref.harness_refs_path=%s",
			harnessRefsPath, evalRef.HarnessRefsPath)
	}
	if err := validateCandidateHarnessRefs(candidateRoles); err != nil {
		return "", err
	}

	var hypotheses []map[string]any
	if params.SingleHarness && params.OptimizationHypothesesPath != "" {
		hypotheses, err = LoadOptimizationHypotheses(params.OptimizationHypothesesPath)
		if err != nil {
			return "", err
		}
	}
	if params.SingleHarness {
		hypothesesByIssue := map[string]bool{}
		for _, item := range hypotheses {
			if id := pyStrOrEmpty(item["source_issue_id"]); id != "" {
				hypothesesByIssue[id] = true
			}
		}
		var missing []string
		for i := range teamIssues {
			if !hypothesesByIssue[teamIssues[i].IssueID] {
				missing = append(missing, teamIssues[i].IssueID)
			}
		}
		sort.Strings(missing)
		if len(missing) > 0 {
			return "", fmt.Errorf("single-harness analyzer issues are missing immutable hypotheses: %s", strings.Join(missing, ", "))
		}
		if params.OptimizationIssueIds != nil {
			issueScope := map[string]bool{}
			for _, id := range params.OptimizationIssueIds {
				if id != "" {
					issueScope[id] = true
				}
			}
			var unknown []string
			for id := range issueScope {
				if !hypothesesByIssue[id] {
					unknown = append(unknown, id)
				}
			}
			sort.Strings(unknown)
			if len(unknown) > 0 {
				return "", fmt.Errorf("single-harness optimization issue scope is unknown: %s", strings.Join(unknown, ", "))
			}
			var filteredIssues []harnessrsi.TeamIssue
			for i := range teamIssues {
				if issueScope[teamIssues[i].IssueID] {
					filteredIssues = append(filteredIssues, teamIssues[i])
				}
			}
			teamIssues = filteredIssues
			var filteredHypotheses []map[string]any
			for _, item := range hypotheses {
				if issueScope[pyStrOrEmpty(item["source_issue_id"])] {
					filteredHypotheses = append(filteredHypotheses, item)
				}
			}
			hypotheses = filteredHypotheses
		}
	}
	selectionPath := filepath.Join(runDir, "member_selection.yaml")
	var roleAttrReport *RoleAttributionReport
	var mechanismAttrReport *MechanismAttributionReport
	var selectionReport *MemberSelectionReport
	if params.SingleHarness {
		roleAttrReport, mechanismAttrReport, selectionReport, err = compileSingleHarnessPlanningInputs(
			teamIssues, candidateRoles, hypotheses, evalRefPath, analysisResultPath, harnessRefsPath, m.config.AllowedActionGroups)
		if err != nil {
			return "", err
		}
		if _, werr := WriteDataclassYaml(filepath.Join(runDir, "role_attribution.yaml"), roleAttrReport); werr != nil {
			return "", werr
		}
		if _, werr := WriteDataclassYaml(filepath.Join(runDir, "mechanism_attribution.yaml"), mechanismAttrReport); werr != nil {
			return "", werr
		}
		written, werr := WriteDataclassYaml(selectionPath, selectionReport)
		if werr != nil {
			return "", werr
		}
		selectionPath = written
	} else {
		// Team 模式保留 role/mechanism 归因（所有权歧义）。
		roleAttrReport, err = m.roleAttributor.Attribute(ctx, AttributeParams{
			EvalRef: &evalRef, TeamIssues: teamIssues, CandidateRoles: candidateRoles,
			ModelConfigRef: m.config.ModelConfigRef, AttributionRetryLimit: m.config.AttributionRetryLimit,
			MaxCasesPerIssue: m.config.MaxCasesPerIssue, MaxTraceChars: m.config.MaxTraceExcerptChars,
			MaxResultChars: m.config.MaxResultExcerptChars,
			AgentWorkspace: filepath.Join(runDir, "stage_workspaces", "role_attribution"),
			AgentSkillsDirs: m.config.AgentSkillsDirs,
		})
		if err != nil {
			return "", err
		}
		_, werr := WriteRoleAttributionReport(roleAttrReport, runDir)
		if werr != nil {
			return "", werr
		}
		mechanismAttrReport, err = m.mechanismAttributor.Attribute(ctx, MechanismAttributeParams{
			RoleAttributionReport: roleAttrReport, EvalRef: &evalRef, CandidateRoles: candidateRoles,
			ModelConfigRef: m.config.ModelConfigRef, AttributionRetryLimit: m.config.AttributionRetryLimit,
			MaxCasesPerIssue: m.config.MaxCasesPerIssue, MaxTraceChars: m.config.MaxTraceExcerptChars,
			MaxResultChars: m.config.MaxResultExcerptChars,
			AgentWorkspace: filepath.Join(runDir, "stage_workspaces", "mechanism_attribution"),
			AgentSkillsDirs: m.config.AgentSkillsDirs,
		})
		if err != nil {
			return "", err
		}
		if _, werr := WriteMechanismAttributionReport(mechanismAttrReport, runDir); werr != nil {
			return "", werr
		}
	}

	// No-op：无问题
	if len(teamIssues) == 0 {
		return m.writeNoopArtifact(evalRefPath, analysisResultPath, harnessRefsPath, outputDir, noopParams{
			status: "no_issues", runDir: runDir,
			roleAttrPath: filepath.Join(runDir, "role_attribution.yaml"),
			mechanismAttrPath: filepath.Join(runDir, "mechanism_attribution.yaml"),
			deferPublish: params.DeferPublish,
		})
	}

	// Phase 3: Selection（单 harness 已在上面解决所有权）
	if !params.SingleHarness {
		selectionReport = m.memberSelector.Select(roleAttrReport, mechanismAttrReport, SelectorOptions{
			MinAttributionConfidence: m.config.MinAttributionConfidence,
			MaxRolesPerRun:           m.config.MaxRolesPerRun,
		})
		written, werr := WriteMemberSelectionReport(selectionReport, runDir)
		if werr != nil {
			return "", werr
		}
		selectionPath = written
	}

	// No-op：无目标
	if len(selectionReport.Targets) == 0 {
		return m.writeNoopArtifact(evalRefPath, analysisResultPath, harnessRefsPath, outputDir, noopParams{
			status: "no_targets", runDir: runDir,
			roleAttrPath: filepath.Join(runDir, "role_attribution.yaml"),
			mechanismAttrPath: filepath.Join(runDir, "mechanism_attribution.yaml"),
			selectionPath: selectionPath,
			deferPublish: params.DeferPublish,
		})
	}

	// Phase 4: Planning
	actionDefinitions := actiongroups.LoadActionDefinitions(m.actionGroupConfigPaths)
	planningWorkspace := filepath.Join(runDir, "stage_workspaces", "action_planning")
	if params.SingleHarness {
		if err := preparePlanningEvidence(planningWorkspace, evalRefPath, hypotheses, harnessRefsPath); err != nil {
			return "", err
		}
	}
	plan, planErr := m.actionPlanner.Plan(ctx, PlanParams{
		Targets: selectionReport.Targets,
		RoleAttributionReport: roleAttrReport,
		MechanismAttributionReport: mechanismAttrReport,
		ActionDefinitions:          actionDefinitions,
		ModelConfigRef:             m.config.ModelConfigRef,
		StageRetryLimit:            m.config.StageRetryLimit,
		AgentWorkspace:             planningWorkspace,
		AgentSkillsDirs:            m.config.AgentSkillsDirs,
		RejectedCapabilities:       params.RejectedCapabilities,
		AllowedActionGroups:        m.config.AllowedActionGroups,
		AllowedPromptSurfaces:      m.config.AllowedPromptSurfaces,
		MaxActionsPerPlan:          maxActionsLimit(m.config.MaxActionsPerPlan, params.SingleHarness),
		OptimizationHypotheses:     hypotheses,
		OptimizationExperience:     params.OptimizationExperience,
	})
	if planErr != nil {
		details := map[string]any{
			"selection_path":         selectionPath,
			"role_attribution_path":  filepath.Join(runDir, "role_attribution.yaml"),
			"mechanism_attribution_path": filepath.Join(runDir, "mechanism_attribution.yaml"),
			"planning_workspace":     planningWorkspace,
			"selected_roles":         targetRoles(selectionReport),
			"selected_issue_ids":     targetIssueIDs(selectionReport),
		}
		_ = writeStageError(filepath.Join(runDir, "action_planning_error.json"), "action_planning", planErr, details)
		if isActionPlanRejection(planErr) {
			skippedRoles := targetRoles(selectionReport)
			roleResults := map[string]RoleResult{}
			for _, target := range selectionReport.Targets {
				roleResults[target.Role] = RoleResult{
					Status: "skipped",
					BeforeHarnessRefPath: target.HarnessRefPath,
					AfterHarnessRefPath:  target.HarnessRefPath,
					VerificationStatus:   "not_run",
					Error:                planErr.Error(),
				}
			}
			return m.writeNoopArtifact(evalRefPath, analysisResultPath, harnessRefsPath, outputDir, noopParams{
				status: "planning_rejected", reason: planErr.Error(), runDir: runDir,
				roleAttrPath: filepath.Join(runDir, "role_attribution.yaml"),
				mechanismAttrPath: filepath.Join(runDir, "mechanism_attribution.yaml"),
				selectionPath: selectionPath,
				roles: skippedRoles, skippedRoles: skippedRoles, roleResults: roleResults,
				deferPublish: params.DeferPublish,
			})
		}
		return "", planErr
	}
	planPath, err := WritePlan(plan, runDir)
	if err != nil {
		return "", err
	}
	candidateManifestPath := ""
	if len(hypotheses) > 0 && params.OptimizationHypothesesPath != "" {
		if written, merr := WriteCandidateManifest(filepath.Join(runDir, "candidate_manifest.yaml"),
			harnessRefsPath, params.OptimizationHypothesesPath, planToDict(plan)); merr == nil {
			candidateManifestPath = written
		}
	}
	if len(plan.Actions) == 0 {
		skippedRoles := targetRoles(selectionReport)
		roleResults := map[string]RoleResult{}
		for _, target := range selectionReport.Targets {
			roleResults[target.Role] = RoleResult{
				Status: "skipped",
				BeforeHarnessRefPath: target.HarnessRefPath,
				AfterHarnessRefPath:  target.HarnessRefPath,
				VerificationStatus:   "not_run",
				Error:                "planner returned no executable actions",
			}
		}
		return m.writeNoopArtifact(evalRefPath, analysisResultPath, harnessRefsPath, outputDir, noopParams{
			status: "no_actions", reason: "planner returned no executable actions", runDir: runDir,
			roleAttrPath: filepath.Join(runDir, "role_attribution.yaml"),
			mechanismAttrPath: filepath.Join(runDir, "mechanism_attribution.yaml"),
			selectionPath: selectionPath, planPath: planPath,
			roles: skippedRoles, skippedRoles: skippedRoles, roleResults: roleResults,
			deferPublish: params.DeferPublish,
		})
	}

	// Phase 5: Execution
	worktreesDir := pathLayout.WorktreesDir(filepath.Base(runDir))
	executionResults, err := m.actionExecutor.Execute(ctx, plan, runDir, m.config.ModelConfigRef, worktreesDir)
	if err != nil {
		return "", err
	}
	executionResultPath := filepath.Join(runDir, "execution_results.json")
	writeRunExecutionResults(executionResultPath, executionResults)
	runtimeRunDir := pathLayout.RunRuntimeDir(filepath.Base(runDir))
	if err := os.MkdirAll(runtimeRunDir, 0o755); err != nil {
		return "", err
	}
	writeRunExecutionResults(filepath.Join(runtimeRunDir, "execution_results.json"), executionResults)
	if _, statErr := os.Stat(planPath); statErr == nil {
		copyPlanFile(planPath, filepath.Join(runtimeRunDir, "plan.yaml"))
	}

	// Phase 6: Verification
	verificationPath := filepath.Join(runDir, "verification.json")
	verificationResult, err := m.verifier.Verify(plan, worktreesDir, verificationPath)
	if err != nil {
		return "", err
	}

	// Phase 7: Repair
	if _, err := m.verifier.Repair(verificationResult, worktreesDir,
		filepath.Join(runDir, "fix_result.json"), m.config.ModelConfigRef, m.config.StageRetryLimit); err != nil {
		return "", err
	}
	if verificationResult.Status != "passed" {
		verificationResult, err = m.verifier.Verify(plan, worktreesDir, verificationPath)
		if err != nil {
			return "", err
		}
	}

	// Phase 8: Publish
	m.publish(publishRequest{
		runDir: runDir, pathLayout: pathLayout, outputRoot: outputRoot,
		selectionReport: selectionReport, plan: plan, executionResults: executionResults,
		verificationResult: verificationResult,
		evalRefPath: evalRefPath, analysisResultPath: analysisResultPath, harnessRefsPath: harnessRefsPath,
		roleAttrPath: filepath.Join(runDir, "role_attribution.yaml"),
		mechanismAttrPath: filepath.Join(runDir, "mechanism_attribution.yaml"),
		selectionPath: selectionPath, planPath: planPath,
		executionResultPath: executionResultPath, verificationPath: verificationPath,
		fixResultPath: filepath.Join(runDir, "fix_result.json"),
		deferPublish:  params.DeferPublish,
		candidateManifestPath:      candidateManifestPath,
		optimizationHypothesesPath: params.OptimizationHypothesesPath,
	})

	return filepath.Join(runDir, "member_optimization_ref.yaml"), nil
}

// noopParams noop 工件参数（Python _write_noop_artifact 关键字收拢）。
type noopParams struct {
	status             string
	reason             string
	runDir             string
	roleAttrPath       string
	mechanismAttrPath  string
	selectionPath      string
	planPath           string
	executionResultPath string
	verificationPath   string
	fixResultPath      string
	roles              []string
	skippedRoles       []string
	roleResults        map[string]RoleResult
	deferPublish       bool
}

// writeNoopArtifact 无法优化时写 noop 工件。
// Python: optimizer.py:565-656 _write_noop_artifact。
func (m *MemberOptimizer) writeNoopArtifact(evalRefPath, analysisResultPath, harnessRefsPath, outputDir string, params noopParams) (string, error) {
	outputRoot, _ := filepath.Abs(outputDir)
	if err := os.MkdirAll(outputRoot, 0o755); err != nil {
		return "", err
	}
	pathLayout := NewMemberOptimizerPathLayout(outputRoot)
	runDir := params.runDir
	if runDir == "" {
		runDir = allocateOptimizationDir(outputRoot)
	}
	roleAttrPath := params.roleAttrPath
	if roleAttrPath == "" {
		emptyRoleReport := &RoleAttributionReport{
			AttributionID:            fmt.Sprintf("role_attribution_%s", shortID()),
			SourceEvalRefPath:        evalRefPath,
			SourceAnalysisResultPath: analysisResultPath,
			HarnessRefsPath:          harnessRefsPath,
			CandidateRoles:           nil,
			AssignedRoleIssues:       nil,
			UnassignedIssues:         nil,
			Warnings:                 nilIfEmpty([]string{params.reason}),
			Metadata:                 map[string]any{"status": params.status},
		}
		written, err := WriteRoleAttributionReport(emptyRoleReport, runDir)
		if err != nil {
			return "", err
		}
		roleAttrPath = written
	}
	currentRefsPath := harnessRefsPath
	if !params.deferPublish {
		currentRefsPath = writeCurrentHarnessRefs(harnessRefsWriteRequest{
			outputRoot: outputRoot, harnessRefsPath: harnessRefsPath,
			publishedRoles: nil, failedRoles: nil,
			skippedRoles: params.skippedRoles, roleResults: params.roleResults,
			pathLayout: pathLayout,
		})
	}
	roles := params.roles
	if roles == nil {
		roles = []string{}
	}
	skippedRoles := params.skippedRoles
	if skippedRoles == nil {
		skippedRoles = []string{}
	}
	roleResults := params.roleResults
	if roleResults == nil {
		roleResults = map[string]RoleResult{}
	}
	artifact := MemberOptimizationArtifact{
		OptimizationID:           filepath.Base(runDir),
		OutputDir:                runDir,
		Status:                   params.status,
		OptimizedHarnessRefsPath: currentRefsPath,
		Roles:                    roles,
		PublishedRoles:           []string{},
		StagedRoles:              roles,
		VerifiedRoles:            []string{},
		PromotedRoles:            []string{},
		FailedRoles:              []string{},
		SkippedRoles:             skippedRoles,
		RoleAttributionPath:      roleAttrPath,
		MechanismAttributionPath: params.mechanismAttrPath,
		SelectionPath:            params.selectionPath,
		PlanPath:                 params.planPath,
		ExecutionResultPath:      params.executionResultPath,
		VerificationPath:         params.verificationPath,
		FixResultPath:            params.fixResultPath,
		RoleResults:              roleResults,
		Metadata: map[string]any{
			"eval_ref_path":            evalRefPath,
			"analysis_result_path":     analysisResultPath,
			"source_harness_refs_path": harnessRefsPath,
			"runtime_root":             pathLayout.RuntimeRoot,
			"reason":                   params.reason,
		},
	}
	refPath := filepath.Join(runDir, "member_optimization_ref.yaml")
	if err := writeYamlArtifact(refPath, &artifact); err != nil {
		return "", err
	}
	return refPath, nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// maxActionsLimit 单方案动作上限（single_harness 钳 3）。
// Python: plan_kwargs["max_actions_per_plan"] = min(limit or 3, 3) if single_harness else limit。
func maxActionsLimit(limit int, singleHarness bool) int {
	if singleHarness {
		if limit == 0 {
			limit = 3
		}
		if limit > 3 {
			limit = 3
		}
		return limit
	}
	return limit
}

// targetRoles 目标角色清单。
func targetRoles(report *MemberSelectionReport) []string {
	var roles []string
	for i := range report.Targets {
		roles = append(roles, report.Targets[i].Role)
	}
	return roles
}

// targetIssueIDs 目标问题 ID 并集（排序）。
func targetIssueIDs(report *MemberSelectionReport) []string {
	seen := map[string]bool{}
	var ids []string
	for i := range report.Targets {
		for _, id := range report.Targets[i].AttributedIssueIDs {
			if !seen[id] {
				seen[id] = true
				ids = append(ids, id)
			}
		}
	}
	sort.Strings(ids)
	return ids
}

// allocateOptimizationDir 分配下一个 member_optimization_XXX 目录。
// Python: optimizer.py:1115-1124 _allocate_optimization_dir。
func allocateOptimizationDir(outputRoot string) string {
	for index := 1; ; index++ {
		candidate := filepath.Join(outputRoot, fmt.Sprintf("member_optimization_%03d", index))
		if _, err := os.Stat(candidate); err != nil {
			if err := os.MkdirAll(candidate, 0o755); err != nil {
				return candidate
			}
			return candidate
		}
	}
}

// shortID uuid hex 前 8。
// Python: optimizer.py:1126-1130 _short_id。
func shortID() string {
	return strings.ReplaceAll(uuid.NewString(), "-", "")[:8]
}

// writeRunExecutionResults 执行结果 JSON 落盘。
// Python: optimizer.py:1132-1138 _write_execution_results。
func writeRunExecutionResults(path string, results []*MemberActionExecutionResult) {
	payload := map[string]any{"results": results}
	data, _ := json.MarshalIndent(payload, "", "  ")
	_ = os.WriteFile(path, data, 0o644)
}

// writeStageError 阶段失败持久化（rerun 调试上下文）。
// Python: optimizer.py:1140-1151 _write_stage_error。
func writeStageError(path, stage string, err error, details map[string]any) error {
	payload := map[string]any{
		"stage": stage, "error_type": errorTypeName(err),
		"error": err.Error(), "details": details,
	}
	data, merr := json.MarshalIndent(payload, "", "  ")
	if merr != nil {
		return merr
	}
	return os.WriteFile(path, data, 0o644)
}

// errorTypeName 错误类型名（Python type(error).__name__ 等价）。
func errorTypeName(err error) string {
	return fmt.Sprintf("%T", err)
}



// WriteDataclassYaml dataclass 值 YAML 落盘。
// Python: optimizer.py:1041-1048 _write_dataclass_yaml。
func WriteDataclassYaml(path string, value any) (string, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return "", err
	}
	data, err := yaml.Marshal(value)
	if err != nil {
		return "", err
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return "", err
	}
	return path, nil
}

// updateCandidateManifest 候选清单状态回写。
// Python: optimizer.py:1050-1074 _update_candidate_manifest。
func updateCandidateManifest(path, status, candidateHarnessRefsPath string, verifiedRoles, failedRoles []string) error {
	expanded, err := expandUser(path)
	if err != nil {
		return err
	}
	target, _ := filepath.Abs(expanded)
	data, err := os.ReadFile(target)
	if err != nil {
		return err
	}
	var payload map[string]any
	if yaml.Unmarshal(data, &payload) != nil || payload == nil {
		return fmt.Errorf("candidate manifest must be a mapping: %s", target)
	}
	payload["status"] = status
	payload["candidate_harness_refs_path"] = candidateHarnessRefsPath
	payload["verified_roles"] = toAnySlice(verifiedRoles)
	payload["failed_roles"] = toAnySlice(failedRoles)
	out, err := yaml.Marshal(payload)
	if err != nil {
		return err
	}
	return os.WriteFile(target, out, 0o644)
}

// findReusablePendingOptimization 同输入最新待门禁候选。
// Python: optimizer.py:1076-1113 _find_reusable_pending_optimization。
func findReusablePendingOptimization(outputRoot, evalRefPath, analysisResultPath, harnessRefsPath string) string {
	abs := func(p string) string {
		expanded, _ := expandUser(p)
		resolved, _ := filepath.Abs(expanded)
		return resolved
	}
	expected := map[string]string{
		"eval_ref_path":            abs(evalRefPath),
		"analysis_result_path":     abs(analysisResultPath),
		"source_harness_refs_path": abs(harnessRefsPath),
	}
	matches, _ := filepath.Glob(filepath.Join(outputRoot, "member_optimization_*", "member_optimization_ref.yaml"))
	sort.Slice(matches, func(i, j int) bool {
		infoI, errI := os.Stat(matches[i])
		infoJ, errJ := os.Stat(matches[j])
		if errI != nil || errJ != nil {
			return false
		}
		return infoI.ModTime().After(infoJ.ModTime())
	})
	for _, refPath := range matches {
		data, err := os.ReadFile(refPath)
		if err != nil {
			continue
		}
		var payload map[string]any
		if yaml.Unmarshal(data, &payload) != nil {
			continue
		}
		status, _ := payload["status"].(string)
		if status != "success" && status != "partial_success" {
			continue
		}
		promotion, _ := payload["promotion_status"].(string)
		if promotion != "pending_gate" {
			continue
		}
		metadata, _ := payload["metadata"].(map[string]any)
		if metadata == nil {
			continue
		}
		match := true
		for key, expectedValue := range expected {
			actual := abs(fmt.Sprintf("%v", metadata[key]))
			if actual != expectedValue {
				match = false
				break
			}
		}
		if !match {
			continue
		}
		candidateRefs := fmt.Sprintf("%v", payload["optimized_harness_refs_path"])
		if info, err := os.Stat(expandUserTilde(candidateRefs)); err == nil && !info.IsDir() {
			resolved, _ := filepath.Abs(refPath)
			return resolved
		}
	}
	return ""
}

// readInputHarnessRefs 读输入 harness refs → role→path 映射。
// Python: optimizer.py:1153-1201 _read_input_harness_refs。
func readInputHarnessRefs(harnessRefsPath string) map[string]string {
	if harnessRefsPath == "" {
		return map[string]string{}
	}
	expanded, _ := expandUser(harnessRefsPath)
	p, _ := filepath.Abs(expanded)
	info, err := os.Stat(p)
	if err != nil {
		return map[string]string{}
	}
	if info.IsDir() {
		refs := map[string]string{}
		entries, _ := os.ReadDir(p)
		for _, entry := range entries {
			if entry.IsDir() {
				resolved, _ := filepath.Abs(filepath.Join(p, entry.Name()))
				refs[entry.Name()] = resolved
			}
		}
		return refs
	}
	data, err := os.ReadFile(p)
	if err != nil {
		return map[string]string{}
	}
	var raw map[string]any
	ext := strings.ToLower(filepath.Ext(p))
	if ext == ".yaml" || ext == ".yml" {
		if yaml.Unmarshal(data, &raw) != nil {
			return map[string]string{}
		}
	} else {
		if json.Unmarshal(data, &raw) != nil {
			return map[string]string{}
		}
	}
	if raw == nil {
		return map[string]string{}
	}
	refs, _ := raw["harness_refs"].(map[string]any)
	roleRefs := map[string]string{}
	if roles, ok := raw["roles"].([]any); ok {
		for _, entry := range roles {
			entryMap, isMap := entry.(map[string]any)
			if !isMap {
				continue
			}
			role := pyStrOrEmpty(entryMap["role"])
			ref := pyStrOrEmpty(entryMap["harness_ref_path"])
			if ref == "" && refs != nil {
				ref = pyStrOrEmpty(refs[role])
			}
			if role != "" && ref != "" {
				roleRefs[role] = resolveHarnessRefValue(ref, p)
			}
		}
	}
	if len(roleRefs) > 0 {
		return roleRefs
	}
	if refs != nil {
		resolvedRefs := map[string]string{}
		for key, value := range refs {
			if text, isStr := value.(string); isStr && key != "" {
				resolvedRefs[key] = resolveHarnessRefValue(text, p)
			}
		}
		return resolvedRefs
	}
	resolvedRefs := map[string]string{}
	metadataKeys := map[string]bool{"version": true, "source": true, "source_harness_refs_path": true}
	for role, ref := range raw {
		if text, isStr := ref.(string); isStr && !metadataKeys[role] {
			resolvedRefs[role] = resolveHarnessRefValue(text, p)
		}
	}
	return resolvedRefs
}

// resolveHarnessRefValue harness 引用值解析（绝对/相对 refs 文件）。
// Python: optimizer.py:1203-1208 _resolve_harness_ref_value。
func resolveHarnessRefValue(value string, refsPath string) string {
	expanded := expandUserTilde(value)
	if filepath.IsAbs(expanded) {
		resolved, _ := filepath.Abs(expanded)
		return resolved
	}
	resolved, _ := filepath.Abs(filepath.Join(filepath.Dir(refsPath), expanded))
	return resolved
}

// validateCandidateHarnessRefs 候选角色须指向已存在 harness。
// Python: optimizer.py:1210-1228 _validate_candidate_harness_refs。
func validateCandidateHarnessRefs(candidateRoles []MemberRoleCandidate) error {
	var errs []string
	for _, candidate := range candidateRoles {
		role := candidate.Role
		if role == "" {
			role = candidate.MemberName
		}
		if role == "" {
			role = "<unknown>"
		}
		ref := strings.TrimSpace(candidate.HarnessRefPath)
		if ref == "" {
			errs = append(errs, fmt.Sprintf("role '%s' has empty harness_ref_path", role))
			continue
		}
		if _, err := os.Stat(expandUserTilde(ref)); err != nil {
			errs = append(errs, fmt.Sprintf("role '%s' harness_ref_path does not exist: %s", role, ref))
		}
	}
	if len(errs) > 0 {
		return fmt.Errorf("MemberOptimizer requires pre-initialized role ExpertHarness refs. "+
			"Create the minimal role harness before optimization. %s", strings.Join(errs, "; "))
	}
	return nil
}

// isActionPlanRejection LLM 方案未过动作契约 → 拒绝候选。
// Python: optimizer.py:1354-1360 _is_action_plan_rejection。
func isActionPlanRejection(err error) bool {
	var rejected *ActionPlanRejectedError
	if errors.As(err, &rejected) {
		return true
	}
	// Python: RuntimeError 前缀兼容（注入 planner 原始错误文案）
	text := err.Error()
	return strings.HasPrefix(text, "Member action plan validation failed:") ||
		strings.HasPrefix(text, "Member action plan validation failed (")
}

// nilIfEmpty 空清单 → nil（Python [] or None 语义）。
func nilIfEmpty(list []string) []string {
	if len(list) == 0 {
		return nil
	}
	return list
}

// copyPlanFile 文件复制（shutil.copy2 等价；worktree_coordinator.go 已有
// copyFile——optimizer 侧独立命名）。
func copyPlanFile(src, dst string) {
	data, err := os.ReadFile(src)
	if err != nil {
		return
	}
	_ = os.WriteFile(dst, data, 0o644)
}

// writeYamlArtifact 工件 YAML 落盘。
func writeYamlArtifact(path string, artifact *MemberOptimizationArtifact) error {
	data, err := yaml.Marshal(artifact)
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

// planToDict plan 结构 → dict（asdict 等价，供 candidate manifest）。
func planToDict(plan *MemberOptimizationPlan) map[string]any {
	data, _ := yaml.Marshal(plan)
	var m map[string]any
	_ = yaml.Unmarshal(data, &m)
	if m == nil {
		m = map[string]any{}
	}
	return m
}

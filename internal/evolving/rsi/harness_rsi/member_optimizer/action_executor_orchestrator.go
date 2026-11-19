// action_executor_orchestrator.go 执行编排器（action_executor.py:965-1625 段）。
// 三层并行：全局波串行 / 同波角色组有界并行 / 角色内子波写路径互斥串行。

package memberoptimizer

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	harnessrsi "github.com/uapclaw/uapclaw-go/internal/evolving/rsi/harness_rsi"
	"github.com/uapclaw/uapclaw-go/internal/evolving/rsi/harness_rsi/member_optimizer/action_groups"
	agentface "github.com/uapclaw/uapclaw-go/internal/evolving/rsi/harness_rsi/member_optimizer/agentface"
	"gopkg.in/yaml.v3"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// BuildExecutorUserMessage 构造动刀用户消息。
// Python: action_executor.py:965-1070 _build_user_message（staticmethod）。
func BuildExecutorUserMessage(action MemberOptimizationAction, planSummary string, allowedSkills, allowedTools []string, actionWorktree string) string {
	// Python: resource_guidance = _action_resource_guidance(action)
	resourceGuidance := ActionResourceGuidance(action)
	// Python: declared_paths = resolve_declared_paths(action)
	declaredPaths := actiongroups.ResolveDeclaredPaths(actiongroups.SchedAction{
		TargetPath:         action.TargetPath,
		DeclaredWritePaths: action.DeclaredWritePaths,
	})
	// Python: file_context = _build_declared_file_context(action_worktree, declared_paths)
	fileContext := BuildDeclaredFileContext(actionWorktree, declaredPaths, 12000)
	// Python: runtime_contracts = _runtime_contract_projection(action)
	runtimeContracts := RuntimeContractProjection(action)
	// Python: if runtime_contracts: semantic_context = Public Runtime Contract 模板
	var semanticContext string
	if len(runtimeContracts) > 0 {
		contractsJSON, _ := json.MarshalIndent(runtimeContracts, "", "  ")
		semanticContext = "## Public Runtime Contract\n\n" +
			"The optimizer's case identifiers, trajectory pointers, evaluator identifiers,\n" +
			"rationale, and plan summary are intentionally withheld. Author the runtime\n" +
			"artifact only from this public semantic projection. The `decision_contract`\n" +
			"records one evidence-selected change in behavior; preserve its direction:\n\n" +
			string(contractsJSON)
	} else {
		// Python: else: Requested Change 模板
		riskNotes := "none"
		if len(action.RiskNotes) > 0 {
			riskNotes = strings.Join(action.RiskNotes, ", ")
		}
		semanticContext = fmt.Sprintf("## Requested Change\n\nDescription: %s\nRationale: %s\nRisk Notes: %s\n\n## Plan Summary\n\n%s",
			action.Description, action.Rationale, riskNotes, planSummary)
	}
	// Python: write_constraint
	writeConstraint := "Read current files with read_file, inspect related package dependencies with " +
		"grep/glob/list_files, then apply the change using write_file or edit_file. " +
		"Paginate long files; a truncated excerpt is not the full baseline."
	// Python: host_roots = "\n".join(f"- {root}" for root in supported_host_read_roots())
	var hostRootLines []string
	if agentsBridge.HostReadRoots != nil {
		for _, root := range agentsBridge.HostReadRoots() {
			hostRootLines = append(hostRootLines, "- "+root)
		}
	}
	hostRoots := strings.Join(hostRootLines, "\n")
	// Python: output_contract
	outputContract := "After applying edits, return ONLY JSON:\n" +
		"{\"action_id\": \"<action_id>\", \"status\": \"succeeded\", \"changed_files\": [\"<relative path>\"], \"errors\": []}\n" +
		"Actual file changes, scope, syntax, and runtime loading determine success."
	// Python: Declared Write Paths 回落 [target_path]
	declaredDisplay := declaredPaths
	if len(declaredDisplay) == 0 {
		declaredDisplay = []string{action.TargetPath}
	}
	// Python: f-string 总模板（逐字）
	return fmt.Sprintf(`## Action to Execute

You are executing the following action in your workspace.

Action ID: %s
Role: %s
Action Group: %s
Operation: %s
Action Type: %s
Target Path: %s
Declared Write Paths: %v
Allowed Skills: %v
Allowed Tools: read_file, grep, glob, list_files, write_file, edit_file

%s

## Current Declared File Contents (initial excerpts; read tools are authoritative)

%s

## Supported Host Contracts (read-only)

%s

Read core/single_agent/rail/base.py for AgentRail hooks and typed event inputs;
read core/foundation/tool/base.py for Tool and ToolCard interfaces. Package-local
harness.yaml, manifests, referenced prompts, tools, and rails may be read even
when they are not declared write paths. Writes remain declared-path-only.
The solver uses enable_task_loop=False. Task-loop-only hooks do not fire there.
ModelCallInputs and ToolCallInputs are dataclasses: use ctx.inputs.response,
ctx.inputs.react_iteration, and ctx.config.max_iterations, never inputs.get(...)
or invented ctx.extra.remaining_iterations. Check response.tool_calls before
forcing a final result. Use supported request_model_continue/push_steering callbacks;
a steering message alone does not require another model iteration.

## Action-Specific Guidance

%s

## Constraints

1. You may ONLY write files within the declared_write_paths.
2. Do NOT modify files outside the role worktree.
3. Do NOT modify current_harnesses, current_harness_refs.yaml, or orchestrator artifacts.
4. Only use the allowed_skills and allowed_tools listed above.
5. Do NOT search for external resources or install dependencies.
6. This action may only modify local ExpertHarness package files supported by current auto harness.
7. %s
8. Use the current file contents above as the source of truth. If the file
   content does not support the requested change, inspect full current files
   using read_file; return failed if the requested baseline is missing.

## Output

%s

If you cannot produce a valid change, return:

`+"```json\n"+`{
  "action_id": "%s",
  "status": "failed",
  "changed_files": [],
  "errors": ["<specific reason>"]
}
`+"```",
		action.ActionID, action.Role, action.ActionGroup, action.Operation, action.ActionType,
		action.TargetPath, declaredDisplay, allowedSkills, semanticContext, fileContext,
		hostRoots, resourceGuidance, writeConstraint, outputContract, action.ActionID)
}

// ──────────────────────────── 结构体 ────────────────────────────

// MemberActionExecutor 三层并行 + 隔离 worktree 的动作执行编排器。
// Python: action_executor.py:1073-1104 MemberActionExecutor。
type MemberActionExecutor struct {
	coordinator               *MemberWorktreeCoordinator
	executorAgent             *MemberActionExecutorAgent
	outputRepair              harnessrsi.OutputRepairConfig
	runtimeIterationHardLimit int
	agentSkillsDirs           []string
	globalSem                 chan struct{}
	roleSem                   chan struct{}
	actionSem                 chan struct{}
}

// NewMemberActionExecutor 构造（Python __init__）。
func NewMemberActionExecutor(worktreeCoordinator *MemberWorktreeCoordinator, executorAgent *MemberActionExecutorAgent, executionConcurrency, roleExecutionConcurrency, actionExecutionConcurrencyPerRole int, agentSkillsDirs []string, outputRepair *harnessrsi.OutputRepairConfig, runtimeIterationHardLimit int) *MemberActionExecutor {
	if worktreeCoordinator == nil {
		worktreeCoordinator = NewMemberWorktreeCoordinator(nil)
	}
	repair := harnessrsi.DefaultOutputRepairConfig()
	if outputRepair != nil {
		repair = *outputRepair
	}
	if runtimeIterationHardLimit <= 0 {
		runtimeIterationHardLimit = 300
	}
	if executionConcurrency <= 0 {
		executionConcurrency = 1
	}
	if roleExecutionConcurrency <= 0 {
		roleExecutionConcurrency = 1
	}
	if actionExecutionConcurrencyPerRole <= 0 {
		actionExecutionConcurrencyPerRole = 1
	}
	return &MemberActionExecutor{
		coordinator:               worktreeCoordinator,
		executorAgent:             executorAgent,
		outputRepair:              repair,
		runtimeIterationHardLimit: runtimeIterationHardLimit,
		agentSkillsDirs:           append([]string{}, agentSkillsDirs...),
		globalSem:                 make(chan struct{}, executionConcurrency),
		roleSem:                   make(chan struct{}, roleExecutionConcurrency),
		actionSem:                 make(chan struct{}, actionExecutionConcurrencyPerRole),
	}
}

// Execute 按波次执行方案（角色级与动作级并行）。
// Python: action_executor.py:1106-1310 execute。
func (e *MemberActionExecutor) Execute(ctx context.Context, plan *MemberOptimizationPlan, outputDir, modelConfigRef, worktreesDir string) ([]*MemberActionExecutionResult, error) {
	// Python: run_dir = Path(output_dir).expanduser().resolve()
	runDir, _ := filepath.Abs(outputDir)
	// Python: worktrees_root = ... or run_dir / MEMBER_WORKTREES_DIR_NAME
	worktreesRoot := worktreesDir
	if worktreesRoot == "" {
		worktreesRoot = filepath.Join(runDir, MemberWorktreesDirName)
	} else {
		worktreesRoot, _ = filepath.Abs(worktreesRoot)
	}
	// Python: action_by_id = {a.action_id: a for a in plan.actions}
	actionByID := map[string]*MemberOptimizationAction{}
	for i := range plan.Actions {
		actionByID[plan.Actions[i].ActionID] = &plan.Actions[i]
	}
	// Python: integration_worktrees = {}; for target in plan.targets: prepare_integration_worktree
	integrationWorktrees := map[string]string{}
	for i := range plan.Targets {
		role := plan.Targets[i].Role
		integrationDir, err := PrepareIntegrationWorktree(role, plan.Targets[i].HarnessRefPath, worktreesRoot)
		if err != nil {
			return nil, err
		}
		integrationWorktrees[role] = integrationDir
	}
	var results []*MemberActionExecutionResult
	var resultsMu sync.Mutex
	failedRoles := map[string]bool{}
	actionStatusByID := map[string]string{}
	// Python: execution_waves = build_action_waves(plan.actions)——持久化波次元数据不信任
	executionWaves, err := buildWavesFromActions(plan.Actions)
	if err != nil {
		return nil, err
	}
	// Python: for wave_idx, wave in enumerate(execution_waves): 波串行
	for waveIdx, wave := range executionWaves {
		var waveActions []*MemberOptimizationAction
		for _, actionID := range wave {
			if action, ok := actionByID[actionID]; ok {
				waveActions = append(waveActions, action)
			}
		}
		// Python: grouped = {}; for action in wave_actions: should_run / role_failed 跳过检查
		grouped := map[string][]*MemberOptimizationAction{}
		roleOrder := []string{}
		for _, action := range waveActions {
			shouldRun, skipReason := ShouldRunAction(*action, actionStatusByID)
			if !shouldRun {
				resultsMu.Lock()
				results = append(results, &MemberActionExecutionResult{
					ActionID: action.ActionID, Role: action.Role, Status: "skipped",
					WorktreePath: "", ArtifactPath: "", Error: skipReason,
				})
				resultsMu.Unlock()
				actionStatusByID[action.ActionID] = "skipped"
				continue
			}
			if failedRoles[action.Role] {
				resultsMu.Lock()
				results = append(results, &MemberActionExecutionResult{
					ActionID: action.ActionID, Role: action.Role, Status: "skipped",
					WorktreePath: "", ArtifactPath: "", Error: "role integration already failed",
				})
				resultsMu.Unlock()
				actionStatusByID[action.ActionID] = "skipped"
				continue
			}
			if _, exists := grouped[action.Role]; !exists {
				roleOrder = append(roleOrder, action.Role)
			}
			grouped[action.Role] = append(grouped[action.Role], action)
		}
		// Python: role_tasks = [...]; if not role_tasks: continue
		if len(roleOrder) == 0 {
			continue
		}
		// Python: wave_results = await asyncio.gather(*role_tasks)——Go 有界并行（WaitGroup）
		var wg sync.WaitGroup
		for _, role := range roleOrder {
			if failedRoles[role] {
				continue
			}
			wg.Add(1)
			go func(role string, roleActions []*MemberOptimizationAction, waveIdx int) {
				defer wg.Done()
				// Python: async with self._role_sem:
				e.roleSem <- struct{}{}
				defer func() { <-e.roleSem }()
				roleResults, roleFailed := e.executeRoleGroup(ctx, role, roleActions, waveIdx, actionByID, integrationWorktrees, runDir, worktreesRoot, modelConfigRef, plan)
				resultsMu.Lock()
				results = append(results, roleResults...)
				resultsMu.Unlock()
				for _, result := range roleResults {
					actionStatusByID[result.ActionID] = result.Status
				}
				if roleFailed {
					failedRoles[role] = true
				}
			}(role, grouped[role], waveIdx)
		}
		wg.Wait()
	}
	// Python: self._write_execution_results(run_dir, results)
	writeExecutionResults(runDir, results)
	return results, nil
}

// executeRoleGroup 角色组执行（子波串行、子波内动作并行）。
// Python: action_executor.py:1173-1189 execute_role_group 内部循环。
func (e *MemberActionExecutor) executeRoleGroup(ctx context.Context, role string, roleActions []*MemberOptimizationAction, waveIdx int, actionByID map[string]*MemberOptimizationAction, integrationWorktrees map[string]string, runDir, worktreesRoot, modelConfigRef string, plan *MemberOptimizationPlan) ([]*MemberActionExecutionResult, bool) {
	integrationDir := integrationWorktrees[role]
	roleFailed := false
	var roleResults []*MemberActionExecutionResult
	// Python: subwaves = build_role_subwaves(role_actions)
	schedActions := make([]actiongroups.SchedAction, 0, len(roleActions))
	for _, action := range roleActions {
		schedActions = append(schedActions, actiongroups.SchedAction{
			ActionID:           action.ActionID,
			TargetPath:         action.TargetPath,
			DeclaredWritePaths: action.DeclaredWritePaths,
		})
	}
	subwaves := actiongroups.BuildRoleSubwaves(schedActions)
	// Python: for subwave_idx, subwave in enumerate(subwaves): 子波串行
	for subwaveIdx, subwave := range subwaves {
		subwaveActions := make([]*MemberOptimizationAction, 0, len(subwave))
		for _, sched := range subwave {
			if action, ok := actionByID[sched.ActionID]; ok {
				subwaveActions = append(subwaveActions, action)
			}
		}
		// Python: subwave_task_results = await asyncio.gather(...)——子波内动作有界并行
		var wg sync.WaitGroup
		subwaveResults := make([]*MemberActionExecutionResult, len(subwaveActions))
		for i, action := range subwaveActions {
			wg.Add(1)
			go func(idx int, action *MemberOptimizationAction) {
				defer wg.Done()
				// Python: async with self._action_sem: async with self._global_sem:
				e.actionSem <- struct{}{}
				defer func() { <-e.actionSem }()
				e.globalSem <- struct{}{}
				defer func() { <-e.globalSem }()
				// Python: try: return await self._execute_one_action(...) except: _write_failed_action_result
				result, err := e.ExecuteOneAction(ctx, action, actionExecutionContext{
					integrationDir: integrationDir,
					runDir:         runDir,
					worktreesDir:   worktreesRoot,
					waveIndex:      waveIdx,
					modelConfigRef: modelConfigRef,
					plan:           plan,
				})
				if err != nil {
					subwaveResults[idx] = e.WriteFailedActionResult(action, runDir, fmt.Sprintf("task exception: %v", err))
					return
				}
				subwaveResults[idx] = result
			}(i, action)
		}
		wg.Wait()
		// Python: for r in subwave_task_results: Exception → failed result（Go err 已转 failed）
		// Python: failed_bundle_keys = {...非 succeeded 的束键...}
		failedBundleKeys := map[[2]string]bool{}
		for _, result := range subwaveResults {
			if result.Status == "succeeded" {
				continue
			}
			action := actionByID[result.ActionID]
			if key, ok := ActionBundleKey(action); ok {
				failedBundleKeys[key] = true
			}
		}
		// Python: atomic_results = [...原子束同伴失败则成功结果不合并...]
		for _, result := range subwaveResults {
			key, ok := ActionBundleKey(actionByID[result.ActionID])
			if result.Status == "succeeded" && ok && failedBundleKeys[key] {
				result.Status = "failed"
				result.MergeStatus = "not_merged"
				result.Error = "atomic action bundle peer failed; successful partial result was not merged"
			}
		}
		// Python: successful_results = [...]; merge or skip
		var successfulResults []*MemberActionExecutionResult
		for _, result := range subwaveResults {
			if result.Status == "succeeded" {
				successfulResults = append(successfulResults, result)
			}
		}
		var mergeResult *MemberActionMergeResult
		mergedActionIDs := map[string]bool{}
		if len(successfulResults) > 0 {
			mergeResult = MergeSubwave(subwaveMergeRequest{
				role:           role,
				waveIndex:      waveIdx,
				subwaveIndex:   subwaveIdx,
				results:        successfulResults,
				integrationDir: integrationDir,
				runDir:         runDir,
			})
			for _, id := range mergeResult.ActionIDs {
				mergedActionIDs[id] = true
			}
		}
		// Python: for r in subwave_results: merge_status 回填；role_results.append
		for _, result := range subwaveResults {
			if mergedActionIDs[result.ActionID] && result.Status == "succeeded" {
				result.MergeStatus = mergeResult.Status
				result.MergeArtifactPath = mergeResult.ArtifactPath
			}
			roleResults = append(roleResults, result)
		}
		// Python: if merge_result and merge_result.status == "failed": role_failed = True; break
		if mergeResult != nil && mergeResult.Status == "failed" {
			roleFailed = true
			break
		}
	}
	return roleResults, roleFailed
}

// ExecuteOneAction 隔离 worktree 中执行单动作。
// Python: action_executor.py:1313-1505 _execute_one_action。
func (e *MemberActionExecutor) ExecuteOneAction(ctx context.Context, action *MemberOptimizationAction, contextData actionExecutionContext) (*MemberActionExecutionResult, error) {
	integrationDir := contextData.integrationDir
	runDir := contextData.runDir
	// Python: if integration_dir is None: raise RuntimeError(...)
	if integrationDir == "" {
		return nil, fmt.Errorf("integration worktree not found for role %s", action.Role)
	}
	// Python: policy_check = validate_action_policy(action); if not valid: failed result
	policyCheck := actiongroups.ValidateActionPolicy(*action, nil)
	if !policyCheck.Valid {
		return e.WriteFailedActionResult(action, runDir, strings.Join(policyCheck.Errors, "; ")), nil
	}
	// Python: if skill/search: failed result（外部搜索当前运行时不可用）
	if action.ActionGroup == "skill" && action.Operation == "search" {
		return e.WriteFailedActionResult(action, runDir,
			"external skill search is unavailable in the current RSI runtime; "+
				"run the dependency_failed package-local skill/add fallback"), nil
	}
	// Python: worktrees_root / prepare_action_worktree
	worktreesRoot := contextData.worktreesDir
	if worktreesRoot == "" {
		worktreesRoot = filepath.Join(runDir, MemberWorktreesDirName)
	}
	worktreeDir, err := PrepareActionWorktree(*action, integrationDir, worktreesRoot, contextData.waveIndex)
	if err != nil {
		return nil, err
	}
	// Python: declared_paths = resolve_declared_paths(action)
	declaredPaths := actiongroups.ResolveDeclaredPaths(actiongroups.SchedAction{
		TargetPath:         action.TargetPath,
		DeclaredWritePaths: action.DeclaredWritePaths,
	})
	// Python: before_all / before_declared 快照
	beforeAll := SnapshotFiles(worktreeDir, nil)
	beforeDeclared := SnapshotFiles(worktreeDir, declaredPaths)
	// Python: plan_summary = f"Plan {plan_id} with {n} actions in {m} waves. Targets: {...}."
	targetRoles := []string{}
	for i := range contextData.plan.Targets {
		targetRoles = append(targetRoles, contextData.plan.Targets[i].Role)
	}
	planSummary := fmt.Sprintf("Plan %s with %d actions in %d waves. Targets: %v.",
		contextData.plan.PlanID, len(contextData.plan.Actions), len(contextData.plan.ActionWaves), targetRoles)
	// Python: try: modify 基线校验 / deterministic 分派 / agent 执行 except: failed
	execResult := map[string]any{}
	if action.Operation == "modify" {
		if _, err := os.Stat(filepath.Join(worktreeDir, action.TargetPath)); err != nil {
			execResult = map[string]any{"status": "failed", "declared_write_paths": declaredPaths,
				"error": fmt.Sprintf("modify baseline mismatch: target does not exist: %s", action.TargetPath)}
			goto afterExec
		}
	}
	{
		var preseedScaffold map[string]any
		if isAddLikeScaffoldAction(worktreeDir, *action) {
			preseedResult := executeDeterministicAddScaffold(worktreeDir, *action, declaredPaths)
			if fmt.Sprintf("%v", preseedResult["status"]) == "succeeded" {
				preseedScaffold, _ = preseedResult["scaffold"].(map[string]any)
			}
		}
		if action.Operation == "remove" {
			execResult = executeDeterministicRemove(worktreeDir, *action, declaredPaths)
		} else {
			agent := e.getExecutorAgent(contextData.modelConfigRef)
			var execErr error
			execResult, execErr = agent.ExecuteAction(ctx, worktreeDir, *action, planSummary,
				action.AllowedSkills, actiongroups.SanitizeAllowedTools(action.AllowedTools))
			if execErr == nil && preseedScaffold != nil {
				if _, has := execResult["deterministic_preseed"]; !has {
					execResult["deterministic_preseed"] = preseedScaffold
				}
			}
			if execErr != nil {
				execResult = map[string]any{"status": "failed", "declared_write_paths": declaredPaths, "error": execErr.Error()}
			}
		}
	}
afterExec:
	// Python: if succeeded: rail manifest 规范化
	if fmt.Sprintf("%v", execResult["status"]) == "succeeded" {
		if manifestFix := normalizeRailManifestRefs(worktreeDir, *action, declaredPaths); manifestFix != nil {
			execResult["rail_manifest_normalization"] = manifestFix
		}
	}
	// Python: after_declared / changed_files / actual_changed_files
	afterDeclared := SnapshotFiles(worktreeDir, declaredPaths)
	changedFiles := ChangedFiles(beforeDeclared, afterDeclared)
	actualChangedFiles := ChangedFiles(beforeAll, SnapshotFiles(worktreeDir, nil))
	// Python: if succeeded: frontmatter 规范化后重算
	if fmt.Sprintf("%v", execResult["status"]) == "succeeded" {
		changedFiles = normalizeSkillFrontmatterNameForWrittenFiles(worktreeDir, *action, changedFiles)
		actualChangedFiles = ChangedFiles(beforeAll, SnapshotFiles(worktreeDir, nil))
	}
	// Python: needs_add_scaffold = failed and not changed and operation==add and group in {prompt, skill}
	status := fmt.Sprintf("%v", execResult["status"])
	if status != "succeeded" && len(changedFiles) == 0 && action.Operation == "add" &&
		(action.ActionGroup == "prompt" || action.ActionGroup == "skill") {
		scaffoldResult := executeDeterministicAddScaffold(worktreeDir, *action, declaredPaths)
		if fmt.Sprintf("%v", scaffoldResult["status"]) == "succeeded" {
			execResult["status"] = "succeeded"
			execResult["error"] = ""
			if _, has := execResult["deterministic_scaffold"]; !has {
				execResult["deterministic_scaffold"] = scaffoldResult["scaffold"]
			}
			afterDeclared = SnapshotFiles(worktreeDir, declaredPaths)
			changedFiles = ChangedFiles(beforeDeclared, afterDeclared)
			actualChangedFiles = ChangedFiles(beforeAll, SnapshotFiles(worktreeDir, nil))
		}
	}
	// Python: if succeeded and not changed_files: 尝试脚手架
	if fmt.Sprintf("%v", execResult["status"]) == "succeeded" && len(changedFiles) == 0 {
		scaffoldResult := executeDeterministicAddScaffold(worktreeDir, *action, declaredPaths)
		if fmt.Sprintf("%v", scaffoldResult["status"]) == "succeeded" {
			if _, has := execResult["deterministic_scaffold"]; !has {
				execResult["deterministic_scaffold"] = scaffoldResult["scaffold"]
			}
			afterDeclared = SnapshotFiles(worktreeDir, declaredPaths)
			changedFiles = ChangedFiles(beforeDeclared, afterDeclared)
			actualChangedFiles = ChangedFiles(beforeAll, SnapshotFiles(worktreeDir, nil))
		}
	}
	// Python: artifact_dir = run_dir / "act" / token(role) / token(action_id)
	artifactDir := filepath.Join(runDir, "act", shortToken(action.Role), shortToken(action.ActionID))
	if err := os.MkdirAll(artifactDir, 0o755); err != nil {
		return nil, err
	}
	artifactPath := filepath.Join(artifactDir, "execution.json")
	// Python: status 降级链：越界 / 无变更 / 资源校验
	status = fmt.Sprintf("%v", execResult["status"])
	declaredPaths = actiongroups.ResolveDeclaredPaths(actiongroups.SchedAction{
		TargetPath:         action.TargetPath,
		DeclaredWritePaths: action.DeclaredWritePaths,
	})
	var outOfBounds []string
	for _, path := range actualChangedFiles {
		if !PathAllowedByDeclared(path, declaredPaths) && !IsRuntimeWorkspaceMetadata(path) {
			outOfBounds = append(outOfBounds, path)
		}
	}
	if len(outOfBounds) > 0 && status == "succeeded" {
		status = "failed"
		execResult["error"] = fmt.Sprintf("changed_files %v outside declared_write_paths %v", outOfBounds, declaredPaths)
	} else if status == "succeeded" && len(changedFiles) == 0 {
		status = "failed"
		execResult["error"] = "agent reported success but no declared_write_paths changed"
	} else if status == "succeeded" {
		validationErrors := validateGeneratedActionResources(worktreeDir, *action, changedFiles, e.runtimeIterationHardLimit)
		if len(validationErrors) > 0 {
			status = "failed"
			execResult["error"] = strings.Join(validationErrors, "; ")
		}
	}
	// Python: payload = {...action asdict...}; json.dump
	actionMap := structToMap(*action)
	payload := map[string]any{
		"action_id":            action.ActionID,
		"role":                 action.Role,
		"action":               actionMap,
		"status":               status,
		"worktree_path":        worktreeDir,
		"changed_files":        toAnySlice(changedFiles),
		"actual_changed_files": toAnySlice(actualChangedFiles),
		"declared_write_paths": toAnySlice(declaredPaths),
		"response_text":        pyStrOrEmpty(execResult["response_text"]),
		"error":                pyStrOrEmpty(execResult["error"]),
	}
	if removeValue, has := execResult["remove"]; has {
		payload["remove"] = removeValue
	}
	payloadJSON, _ := json.MarshalIndent(payload, "", "  ")
	if err := os.WriteFile(artifactPath, payloadJSON, 0o644); err != nil {
		return nil, err
	}
	return &MemberActionExecutionResult{
		ActionID:           action.ActionID,
		Role:               action.Role,
		MemberName:         action.MemberName,
		Status:             status,
		WorktreePath:       worktreeDir,
		ArtifactPath:       artifactPath,
		ChangedFiles:       changedFiles,
		DeclaredWritePaths: declaredPaths,
		MergeStatus:        "pending",
		Error:              pyStrOrEmpty(execResult["error"]),
	}, nil
}

// WriteFailedActionResult 执行前被拒动作的失败产物落盘。
// Python: action_executor.py:1507-1545 _write_failed_action_result（staticmethod）。
func (e *MemberActionExecutor) WriteFailedActionResult(action *MemberOptimizationAction, runDir, errText string) *MemberActionExecutionResult {
	declaredPaths := actiongroups.ResolveDeclaredPaths(actiongroups.SchedAction{
		TargetPath:         action.TargetPath,
		DeclaredWritePaths: action.DeclaredWritePaths,
	})
	artifactDir := filepath.Join(runDir, "act", shortToken(action.Role), shortToken(action.ActionID))
	os.MkdirAll(artifactDir, 0o755)
	artifactPath := filepath.Join(artifactDir, "execution.json")
	payload := map[string]any{
		"action_id":              action.ActionID,
		"role":                   action.Role,
		"action":                 structToMap(*action),
		"status":                 "failed",
		"worktree_path":          "",
		"changed_files":          []any{},
		"actual_changed_files":   []any{},
		"declared_write_paths":   toAnySlice(declaredPaths),
		"allowed_executor_tools": toAnySlice(sortedExecutorTools()),
		"response_text":          "",
		"error":                  errText,
	}
	payloadJSON, _ := json.MarshalIndent(payload, "", "  ")
	os.WriteFile(artifactPath, payloadJSON, 0o644)
	return &MemberActionExecutionResult{
		ActionID:           action.ActionID,
		Role:               action.Role,
		MemberName:         action.MemberName,
		Status:             "failed",
		WorktreePath:       "",
		ArtifactPath:       artifactPath,
		ChangedFiles:       []string{},
		DeclaredWritePaths: declaredPaths,
		MergeStatus:        "",
		Error:              errText,
	}
}

// MergeSubwave 成功动作 worktree 合并进整合区。
// Python: action_executor.py:1547-1611 _merge_subwave（staticmethod）。
func MergeSubwave(request subwaveMergeRequest) *MemberActionMergeResult {
	// Python: merge_dir = run_dir / "m" / token(role); merge_artifact = wave_NNN_subwave_NNN.json
	mergeDir := filepath.Join(request.runDir, "m", shortToken(request.role))
	os.MkdirAll(mergeDir, 0o755)
	mergeArtifact := filepath.Join(mergeDir, fmt.Sprintf("wave_%03d_subwave_%03d.json", request.waveIndex, request.subwaveIndex))
	var mergedFiles []string
	var conflicts []string
	// Python: for result in sorted(results, key=action_id): ...
	sortedResults := append([]*MemberActionExecutionResult{}, request.results...)
	sort.Slice(sortedResults, func(i, j int) bool { return sortedResults[i].ActionID < sortedResults[j].ActionID })
	for _, result := range sortedResults {
		if result.Status != "succeeded" {
			continue
		}
		if _, err := os.Stat(result.WorktreePath); err != nil {
			continue
		}
		for _, changedFile := range result.ChangedFiles {
			src := filepath.Join(result.WorktreePath, changedFile)
			dst := filepath.Join(request.integrationDir, changedFile)
			if info, err := os.Stat(src); err == nil && !info.IsDir() {
				os.MkdirAll(filepath.Dir(dst), 0o755)
				copyFileExec(src, dst)
				mergedFiles = append(mergedFiles, changedFile)
			} else if _, dstErr := os.Stat(dst); dstErr == nil {
				// Python: dst 存在但 src 缺失 → 删除（表达动作删除语义）
				if dstInfo, _ := os.Stat(dst); dstInfo.IsDir() {
					os.RemoveAll(dst)
				} else {
					os.Remove(dst)
				}
				removeEmptyParentDirs(filepath.Dir(dst), request.integrationDir)
				mergedFiles = append(mergedFiles, changedFile)
			}
		}
	}
	// Python: report = normalize_package_registries(integration_dir, writable_paths=merged_files)
	report := NormalizePackageRegistries(request.integrationDir, mergedFiles)
	mergedFiles = sortedUniqueStrings(append(mergedFiles, report.ChangedFiles...))
	// Python: registry_issues = report.issues or validate_package_registries(integration_dir)
	registryIssues := report.Issues
	if len(registryIssues) == 0 {
		registryIssues = ValidatePackageRegistries(request.integrationDir)
	}
	// Python: merge_status = "merged" if merged_files else "failed"
	mergeStatus := "failed"
	if len(mergedFiles) > 0 {
		mergeStatus = "merged"
	}
	if len(mergedFiles) == 0 && len(request.results) > 0 {
		conflicts = append(conflicts, "no files merged from successful actions")
	}
	var issueMsgs []any
	for _, issue := range registryIssues {
		issueMsgs = append(issueMsgs, issue.Describe())
	}
	var actionIDs []any
	for _, result := range request.results {
		actionIDs = append(actionIDs, result.ActionID)
	}
	payload := map[string]any{
		"role":            request.role,
		"wave_index":      request.waveIndex,
		"subwave_index":   request.subwaveIndex,
		"action_ids":      actionIDs,
		"status":          mergeStatus,
		"merged_files":    toAnySlice(sortedUniqueStrings(mergedFiles)),
		"conflicts":       toAnySlice(conflicts),
		"registry_issues": issueMsgs,
		"artifact_path":   mergeArtifact,
	}
	payloadJSON, _ := json.MarshalIndent(payload, "", "  ")
	os.WriteFile(mergeArtifact, payloadJSON, 0o644)
	return &MemberActionMergeResult{
		Role:         request.role,
		WaveIndex:    request.waveIndex,
		SubwaveIndex: request.subwaveIndex,
		ActionIDs: func() []string {
			var out []string
			for _, r := range request.results {
				out = append(out, r.ActionID)
			}
			return out
		}(),
		Status:       mergeStatus,
		MergedFiles:  sortedUniqueStrings(mergedFiles),
		Conflicts:    conflicts,
		ArtifactPath: mergeArtifact,
	}
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// getExecutorAgent 取预注入 agent 或按参数构造。
// Python: action_executor.py:1096-1104 _get_executor_agent。
func (e *MemberActionExecutor) getExecutorAgent(modelConfigRef string) *MemberActionExecutorAgent {
	// Python: if self._executor_agent is not None: return self._executor_agent
	if e.executorAgent != nil {
		return e.executorAgent
	}
	// Python: return MemberActionExecutorAgent(...)
	return NewMemberActionExecutorAgent(modelConfigRef, e.agentSkillsDirs, &e.outputRepair, e.runtimeIterationHardLimit)
}

// CreateExecutionAgentForExecutor 经桥构造执行 agent（adapter 返回 handle）。
func CreateExecutionAgentForExecutor(ctx context.Context, params ExecutorAgentParams) (*agentface.Agent, error) {
	if agentsBridge.CreateActionExecutionAgent == nil {
		return nil, fmt.Errorf("agents bridge not registered: import member_optimizer/agents (blank) to wire implementation")
	}
	return agentsBridge.CreateActionExecutionAgent(ctx, params)
}

// writeExecutionResults 落盘 execution_results.json。
// Python: action_executor.py:1613-1626 _write_execution_results（staticmethod）。
func writeExecutionResults(runDir string, results []*MemberActionExecutionResult) {
	path := filepath.Join(runDir, "execution_results.json")
	os.MkdirAll(filepath.Dir(path), 0o755)
	var out []any
	for _, result := range results {
		out = append(out, structToMap(*result))
	}
	payload, _ := json.MarshalIndent(map[string]any{"results": out}, "", "  ")
	os.WriteFile(path, payload, 0o644)
}

// sortedExecutorTools 排序的允许工具清单。
func sortedExecutorTools() []string {
	return []string{"edit_file", "glob", "grep", "list_files", "read_file", "write_file"}
}

// copyFileExec 合并拷贝（copy2 等价）。
func copyFileExec(src, dst string) error {
	info, err := os.Stat(src)
	if err != nil {
		return err
	}
	content, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	return os.WriteFile(dst, content, info.Mode().Perm())
}

// structToMap 结构体 → map（yaml tag 等价 asdict）。
func structToMap(value any) map[string]any {
	data, err := yaml.Marshal(value)
	if err != nil {
		return map[string]any{}
	}
	var out map[string]any
	if err := yaml.Unmarshal(data, &out); err != nil || out == nil {
		return map[string]any{}
	}
	return out
}

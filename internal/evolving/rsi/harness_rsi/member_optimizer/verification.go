// verification.go 验证与修复 agent（HarnessRepairAgent / HarnessChangeVerifier）。
// Python: member_optimizer/verification.py:1164-1664。

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
)

// ──────────────────────────── 常量 ────────────────────────────

// repairProtectedRuntimeNames 修复范围豁免的运行时文件名。
// Python: verification.py:1149-1150 _enforce_repair_scope runtime_names。
var repairProtectedRuntimeNames = map[string]bool{
	"AGENT.md": true, "SOUL.md": true, "IDENTITY.md": true,
	"USER.md": true, "HEARTBEAT.md": true,
}

// ──────────────────────────── 结构体 ────────────────────────────

// HarnessRepairAgent 修复失败静态检查的 DeepAgent 适配器。
// Python: verification.py:1164-1323 HarnessRepairAgent。
type HarnessRepairAgent struct {
	// modelConfigRef 模型配置引用
	modelConfigRef string
	// agentSkillsDirs agent 技能目录
	agentSkillsDirs []string
	// runtimeIterationHardLimit 运行时迭代硬上限
	runtimeIterationHardLimit int
}

// NewHarnessRepairAgent 构造修复 agent。
// Python: verification.py:1167-1173 __init__。
func NewHarnessRepairAgent(modelConfigRef string, agentSkillsDirs []string, runtimeIterationHardLimit int) *HarnessRepairAgent {
	return &HarnessRepairAgent{
		modelConfigRef:            modelConfigRef,
		agentSkillsDirs:           append([]string{}, agentSkillsDirs...),
		runtimeIterationHardLimit: runtimeIterationHardLimit,
	}
}

// RepairRole 调用 DeepAgent 修复单角色整合区。
// Python: verification.py:1176-1301 repair_role。
func (r *HarnessRepairAgent) RepairRole(roleIntegrationWorktree, role string, failedChecks []map[string]any) (map[string]any, error) {
	var repairableChecks []map[string]any
	for _, check := range failedChecks {
		if isWorktreeRepairableCheck(fmt.Sprintf("%v", check["name"])) {
			repairableChecks = append(repairableChecks, check)
		}
	}
	if len(repairableChecks) == 0 {
		return map[string]any{
			"status": "failed",
			"repairs": []RepairItem{{
				Role: role, Action: "deepagent_repair",
				Description: "No worktree-repairable failed checks were provided.",
				Status:      "failed", Error: "no repairable checks",
			}},
		}, nil
	}

	before := snapshotRepairFiles(roleIntegrationWorktree)
	normalization := NormalizePackageRegistries(roleIntegrationWorktree, []string{PromptRegistry, SkillRegistry})
	if len(normalization.ChangedFiles) > 0 {
		expectedTools := map[string]bool{}
		for _, check := range failedChecks {
			name := fmt.Sprintf("%v", check["name"])
			if strings.HasPrefix(name, fmt.Sprintf("tool_activation:%s:", role)) {
				parts := strings.Split(name, ":")
				expectedTools[parts[len(parts)-1]] = true
			}
		}
		currentChecks := validateRoleIntegrationWorktree(role, roleIntegrationWorktree, sortedKeys(expectedTools), r.runtimeIterationHardLimit)
		if !anyCheckFailed(currentChecks) {
			changes := strings.Join(normalization.Changes, "; ")
			return map[string]any{
				"status": "attempted", "changed_files": normalization.ChangedFiles,
				"repairs": []RepairItem{{
					Role: role, Action: "registry_normalization", Status: "attempted", Description: changes,
				}},
			}, nil
		}
		repairableChecks = nil
		for _, check := range repairableFailedChecks(currentChecks) {
			repairableChecks = append(repairableChecks, map[string]any{
				"name": check.Name, "status": check.Status, "notes": check.Notes, "error": check.Error,
			})
		}
	}
	for _, issue := range normalization.Issues {
		repairableChecks = append(repairableChecks, map[string]any{
			"name":   fmt.Sprintf("package_registry:%s:%s:%d", role, issue.Manifest, issue.Index),
			"status": "failed", "error": issue.Describe(),
		})
	}
	// Python: create_verification_repair_agent（经 agents 桥）
	createFn := agentsBridge.CreateVerificationRepairAgent
	if createFn == nil {
		return nil, fmt.Errorf("agents bridge not registered: CreateVerificationRepairAgent")
	}
	agent, err := createFn(context.Background(), ExecutorAgentParams{
		ModelConfigRef:  r.modelConfigRef,
		Workspace:       roleIntegrationWorktree,
		AgentSkillsDirs: r.agentSkillsDirs,
	})
	if err != nil {
		return nil, err
	}
	invokeFn := agentsBridge.InvokeAgent
	if invokeFn == nil {
		return nil, fmt.Errorf("agents bridge not registered: InvokeAgent")
	}
	response, invokeErr := invokeFn(context.Background(), agent, r.BuildRepairUserMessage(role, roleIntegrationWorktree, repairableChecks),
		fmt.Sprintf("member_verifier_repair_%s", role))
	if invokeErr != nil {
		return nil, invokeErr
	}
	NormalizePackageRegistries(roleIntegrationWorktree, []string{PromptRegistry, SkillRegistry})
	after := snapshotRepairFiles(roleIntegrationWorktree)
	forbidden := enforceRepairScope(roleIntegrationWorktree, before, after)
	changed := changedNames(before, after)
	errMsg := ""
	if len(forbidden) > 0 {
		errMsg = fmt.Sprintf("repair changed files outside permitted package scope: %v", forbidden)
	}
	if len(changed) == 0 {
		errMsg = "repair made no file changes"
	}
	status := "attempted"
	if errMsg != "" {
		status = "failed"
	}
	repair := RepairItem{
		Role: role, Action: "deepagent_repair",
		Description: summarizeAgentResponse(agentsBridgeExtractText(response)),
		Status:      status, Error: errMsg,
	}
	result := map[string]any{"status": repair.Status, "repairs": []RepairItem{repair}}
	if changed != nil {
		result["changed_files"] = changed
	}
	return result, nil
}

// BuildRepairUserMessage 构造修复用户消息（模板逐字）。
// Python: verification.py:1267-1323 _build_repair_user_message。
func (r *HarnessRepairAgent) BuildRepairUserMessage(role, roleIntegrationWorktree string, failedChecks []map[string]any) string {
	files := listRepairContextFiles(roleIntegrationWorktree)
	filesText := "- no files found"
	if len(files) > 0 {
		lines := make([]string, 0, len(files))
		for _, path := range files {
			lines = append(lines, "- "+path)
		}
		filesText = strings.Join(lines, "\n")
	}
	failedSummary := summarizeFailedChecks(failedChecks, repairCheckLimit)
	failedFileContext := repairFailedFileContext(roleIntegrationWorktree, failedChecks)
	hostRootLines := []string{}
	if agentsBridge.HostReadRoots != nil {
		for _, root := range agentsBridge.HostReadRoots() {
			hostRootLines = append(hostRootLines, "- "+root)
		}
	}
	return fmt.Sprintf(`## Role Expert Harness Repair

Role: %s
Integration worktree: current workspace

Repair only the failed static checks below. Do not modify audit artifacts such
as plan.yaml, execution_results.json, verification.json, fix_result.json,
current_harness_refs.yaml, or member_optimization_ref.yaml.

## Failed Checks

%s

## Registry Contract

%s

## Current Workspace Files

%s

## Failed File Contents (initial excerpts)

%s

Read full current contents with read_file before replacing files; use offset/limit
for long files and edit_file for localized edits. Use grep/glob/list_files to find
package-local dependencies. The following host contract paths are read-only:

%s

AgentRail hook inputs are typed dataclasses, not dictionaries. Read rail/base.py
for supported hooks and control callbacks. The solver has enable_task_loop=False;
task-loop-only hooks will not run. Use ctx.inputs.react_iteration and
ctx.config.max_iterations for budget decisions, not invented ctx.extra keys.
Steering is advisory; request_model_continue is needed to require another model turn.

## Output

After editing files, briefly report which files you changed, why, and any
limits you hit. The verifier will rerun deterministic checks to decide success.
`, role, failedSummary, RegistryGuidance, filesText, failedFileContext, strings.Join(hostRootLines, "\n"))
}

// HarnessChangeVerifier 验证并按需修复候选 Expert Harness 变更。
// Python: verification.py:1325-1662 HarnessChangeVerifier。
type HarnessChangeVerifier struct {
	// repairAgent 注入的修复 agent（nil 则按模型引用构造）
	repairAgent *HarnessRepairAgent
	// outputRepair 输出修复配置
	outputRepair *harnessrsi.OutputRepairConfig
	// runtimeIterationHardLimit 迭代硬上限
	runtimeIterationHardLimit int
	// agentSkillsDirs agent 技能目录
	agentSkillsDirs []string
}

// NewHarnessChangeVerifier 构造验证器。
// Python: verification.py:1328-1338 __init__。
func NewHarnessChangeVerifier(repairAgent *HarnessRepairAgent, agentSkillsDirs []string, outputRepair *harnessrsi.OutputRepairConfig, runtimeIterationHardLimit int) *HarnessChangeVerifier {
	if outputRepair == nil {
		outputRepair = &harnessrsi.OutputRepairConfig{}
	}
	return &HarnessChangeVerifier{
		repairAgent:               repairAgent,
		outputRepair:              outputRepair,
		runtimeIterationHardLimit: runtimeIterationHardLimit,
		agentSkillsDirs:           append([]string{}, agentSkillsDirs...),
	}
}

// getRepairAgent 取修复 agent（注入优先）。
// Python: verification.py:1340-1349 _get_repair_agent。
func (v *HarnessChangeVerifier) getRepairAgent(modelConfigRef string) *HarnessRepairAgent {
	if v.repairAgent != nil {
		return v.repairAgent
	}
	return NewHarnessRepairAgent(modelConfigRef, v.agentSkillsDirs, v.runtimeIterationHardLimit)
}

// Verify 验证全部入选角色整合区。
// Python: verification.py:1351-1461 verify（asyncio.gather 并发 → 有界并行）。
func (v *HarnessChangeVerifier) Verify(plan *MemberOptimizationPlan, worktreesDir, outputPath string) (*MemberVerificationResult, error) {
	output, err := PrepareOutputPath(outputPath)
	if err != nil {
		return nil, err
	}
	selectedRoles := map[string]bool{}
	for i := range plan.Targets {
		selectedRoles[plan.Targets[i].Role] = true
	}
	expectedToolNamesByRole := map[string][]string{}
	for role := range selectedRoles {
		expectedToolNamesByRole[role] = nil
	}
	for i := range plan.Actions {
		action := &plan.Actions[i]
		if action.ActionGroup != "tool" || (action.Operation != "add" && action.Operation != "modify") {
			continue
		}
		runtimeName := action.TargetPath
		if idx := strings.LastIndex(runtimeName, "/"); idx >= 0 {
			runtimeName = runtimeName[idx+1:]
		}
		runtimeName = strings.TrimSuffix(runtimeName, filepath.Ext(runtimeName))
		if runtimeName != "" {
			expectedToolNamesByRole[action.Role] = append(expectedToolNamesByRole[action.Role], runtimeName)
		}
	}
	var allChecks []VerificationCheck
	roleResults := map[string]RoleVerificationResult{}
	repairable := false

	planPath := filepath.Join(filepath.Dir(worktreesDir), "plan.yaml")
	if _, err := os.Stat(planPath); err == nil {
		allChecks = append(allChecks, validatePlanSchema(planPath)...)
	}
	execPath := filepath.Join(filepath.Dir(worktreesDir), "execution_results.json")
	actionChecksByRole := map[string][]VerificationCheck{}
	for role := range selectedRoles {
		actionChecksByRole[role] = nil
	}
	if _, err := os.Stat(execPath); err == nil {
		allChecks = append(allChecks, validateExecutionResults(execPath)...)
		actionChecksByRole = validateActionResultsByRole(plan, execPath)
	} else if len(plan.Actions) > 0 {
		missingExecCheck := VerificationCheck{
			Name: "execution_results_present", Status: "failed",
			Error: fmt.Sprintf("execution_results.json not found: %s", execPath),
		}
		allChecks = append(allChecks, missingExecCheck)
		for role := range selectedRoles {
			actionChecksByRole[role] = append(actionChecksByRole[role], missingExecCheck)
		}
	}

	// Python: asyncio.gather 并发 per-role；Go 有界并行
	type roleOutcome struct {
		role string
		rvr  RoleVerificationResult
	}
	roles := sortedKeys(selectedRoles)
	outcomes := make([]roleOutcome, len(roles))
	sem := make(chan struct{}, 4)
	var wg sync.WaitGroup
	wg.Add(len(roles))
	for i, role := range roles {
		go func(i int, role string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			integrationDir := ResolveIntegrationWorktreePath(worktreesDir, role)
			var checks []VerificationCheck
			var verifyErr error
			func() {
				defer func() {
					if rec := recover(); rec != nil {
						checks = []VerificationCheck{{Name: fmt.Sprintf("verification_exception:%s", role), Status: "failed", Error: fmt.Sprintf("%v", rec)}}
					}
				}()
				checks = validateRoleIntegrationWorktree(role, integrationDir, expectedToolNamesByRole[role], v.runtimeIterationHardLimit)
				for ai := range plan.Actions {
					action := &plan.Actions[ai]
					if action.Role != role || action.ActionGroup != "rail" {
						continue
					}
					if (action.Operation != "add" && action.Operation != "modify") || !strings.HasSuffix(action.TargetPath, ".py") {
						continue
					}
					checks = append(checks, checkRailRuntimeContract(role, integrationDir, action.TargetPath))
				}
			}()
			rvr := RoleVerificationResult{Role: role}
			if verifyErr != nil {
				checks = []VerificationCheck{{Name: fmt.Sprintf("verification_exception:%s", role), Status: "failed", Error: verifyErr.Error()}}
				rvr.Error = verifyErr.Error()
			} else {
				failedStatic := 0
				for _, check := range checks {
					if check.Status == "failed" {
						failedStatic++
					}
				}
				if failedStatic > 0 {
					rvr.Error = fmt.Sprintf("%d static check(s) failed", failedStatic)
				}
			}
			checks = append(checks, actionChecksByRole[role]...)
			failedRole := 0
			for _, check := range checks {
				if check.Status == "failed" {
					failedRole++
				}
			}
			rvr.Status = "passed"
			if failedRole > 0 {
				rvr.Status = "failed"
			}
			rvr.Checks = checks
			rvr.Repairable = roleIsRepairable(checks)
			outcomes[i] = roleOutcome{role: role, rvr: rvr}
		}(i, role)
	}
	wg.Wait()
	for _, outcome := range outcomes {
		roleResults[outcome.role] = outcome.rvr
		allChecks = append(allChecks, outcome.rvr.Checks...)
		if outcome.rvr.Repairable {
			repairable = true
		}
	}
	failedCount := 0
	for _, check := range allChecks {
		if check.Status == "failed" {
			failedCount++
		}
	}
	overall := "passed"
	if failedCount > 0 {
		overall = "failed"
	}
	result := &MemberVerificationResult{
		Status: overall, CheckedRoles: roles, Checks: allChecks,
		RoleResults: roleResults, Repairable: repairable,
		Metadata: map[string]any{"worktrees_dir": worktreesDir},
	}
	if err := writeVerificationResult(output, result); err != nil {
		return nil, err
	}
	return result, nil
}

// writeVerificationResult 落盘验证结果（JSON indent=2）。
// Python: verification.py:1462-1477 _write_verification_result。
func writeVerificationResult(output string, result *MemberVerificationResult) error {
	payload := map[string]any{
		"status": result.Status, "checked_roles": result.CheckedRoles,
		"checks": result.Checks, "role_results": result.RoleResults,
		"repairable": result.Repairable, "metadata": result.Metadata,
	}
	data, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(output, data, 0o644)
}

// Repair 尝试修复失败检查。
// Python: verification.py:1483-1658 repair。
func (v *HarnessChangeVerifier) Repair(verificationResult *MemberVerificationResult, worktreesDir, outputPath, modelConfigRef string, stageRetryLimit int) (*MemberFixResult, error) {
	output, err := PrepareOutputPath(outputPath)
	if err != nil {
		return nil, err
	}
	if verificationResult.Status == "passed" {
		fixResult := &MemberFixResult{
			Status: "not_needed",
			VerificationPath: filepath.Join(filepath.Dir(worktreesDir), "verification.json"),
			FinalVerificationStatus: "passed",
			Metadata: map[string]any{"note": "Verification already passed, no repair needed"},
		}
		if err := writeFixResult(output, fixResult); err != nil {
			return nil, err
		}
		return fixResult, nil
	}

	var repairs []RepairItem
	roleAttempts := map[string]any{}
	remainingFailedChecks := map[string]any{}
	anyRolePassed := false
	maxAttempts := stageRetryLimit
	if maxAttempts < 0 {
		maxAttempts = 0
	}
	if maxAttempts > v.outputRepair.MaxAttempts {
		maxAttempts = v.outputRepair.MaxAttempts
	}
	roleStopReasons := map[string]any{}

	// Python: for role, rvr in verification_result.role_results.items() —— map 无序；
	// Go 按 sortedKeys 确定序（D-c1-2 同款确定性化）
	for _, role := range sortedKeys(verificationResult.RoleResults) {
		rvr := verificationResult.RoleResults[role]
		if !rvr.Repairable {
			var failedNames []string
			for _, check := range rvr.Checks {
				if check.Status == "failed" {
					failedNames = append(failedNames, check.Name)
				}
			}
			if len(failedNames) > 0 {
				remainingFailedChecks[role] = failedNames
			}
			continue
		}
		integrationDir := ResolveIntegrationWorktreePath(worktreesDir, role)
		agent := v.getRepairAgent(modelConfigRef)
		failedChecks := repairableFailedChecks(rvr.Checks)
		var failed []map[string]any
		for _, check := range failedChecks {
			failed = append(failed, map[string]any{
				"name": check.Name, "status": check.Status, "notes": check.Notes, "error": check.Error})
		}
		if len(failed) == 0 || maxAttempts == 0 {
			var names []string
			for _, check := range rvr.Checks {
				if check.Status == "failed" {
					names = append(names, check.Name)
				}
			}
			remainingFailedChecks[role] = names
			continue
		}

		maxAttemptsCopy := maxAttempts
		tracker := harnessrsi.NewFeedbackTracker(*v.outputRepair, &maxAttemptsCopy)
		for attempt := 0; attempt < maxAttempts; attempt++ {
			roleAttempts[role] = roleAttempts[role].(int) + 1
			func() {
				defer func() {
					if rec := recover(); rec != nil {
						repairs = append(repairs, RepairItem{
							Role: role, Action: "deepagent_repair",
							Description: "DeepAgent repair invocation failed.",
							Status:      "failed", Error: fmt.Sprintf("%v", rec),
						})
						var names []string
						for _, check := range rvr.Checks {
							if check.Status == "failed" {
								names = append(names, check.Name)
							}
						}
						remainingFailedChecks[role] = names
						roleStopReasons[role] = "non_retryable_failure"
					}
				}()
				beforeAttempt := snapshotRepairFiles(integrationDir)
				repairResult, repairErr := agent.RepairRole(integrationDir, role, failed)
				if repairErr != nil {
					repairs = append(repairs, RepairItem{
						Role: role, Action: "deepagent_repair",
						Description: "DeepAgent repair invocation failed.",
						Status:      "failed", Error: repairErr.Error(),
					})
					var names []string
					for _, check := range rvr.Checks {
						if check.Status == "failed" {
							names = append(names, check.Name)
						}
					}
					remainingFailedChecks[role] = names
					retryable := harnessrsi.IsRetryableModelCallFailure(repairErr)
					progressed := tracker.Failed(repairErr.Error(), snapshotRepairFiles(integrationDir))
					if retryable && progressed {
						failed = append(failed, map[string]any{
							"name": failed[0]["name"], "status": "failed", "error": repairErr.Error()})
						return // continue 尝试
					}
					if tracker.StopReason != "" {
						roleStopReasons[role] = tracker.StopReason
					} else {
						roleStopReasons[role] = "non_retryable_failure"
					}
					return
				}
				rawRepairs, _ := repairResult["repairs"].([]RepairItem)
				var repairItems []RepairItem
				for _, item := range rawRepairs {
					repairItems = append(repairItems, item)
				}
				afterAttempt := snapshotRepairFiles(integrationDir)
				forbidden := enforceRepairScope(integrationDir, beforeAttempt, afterAttempt)
				if len(forbidden) > 0 {
					reason := fmt.Sprintf("repair changed files outside permitted package scope: %v", forbidden)
					for i := range repairItems {
						item := repairItems[i]
						item.Status = "failed"
						item.Error = reason
						repairs = append(repairs, item)
					}
					var names []string
					for _, check := range rvr.Checks {
						if check.Status == "failed" {
							names = append(names, check.Name)
						}
					}
					remainingFailedChecks[role] = names
					roleStopReasons[role] = "scope_violation"
					return
				}
				if sameSnapshot(beforeAttempt, afterAttempt) {
					reason := "repair made no file changes; use read/edit tools to address the failed checks"
					for i := range repairItems {
						item := repairItems[i]
						item.Status = "failed"
						item.Error = reason
						repairs = append(repairs, item)
					}
					var names []string
					for _, check := range rvr.Checks {
						if check.Status == "failed" {
							names = append(names, check.Name)
						}
					}
					remainingFailedChecks[role] = names
					tracker.Failed("no_file_changes", afterAttempt)
					roleStopReasons[role] = "no_progress"
					return
				}
				expectedToolNames := map[string]bool{}
				for _, check := range rvr.Checks {
					if strings.HasPrefix(check.Name, fmt.Sprintf("tool_activation:%s:", role)) {
						parts := strings.Split(check.Name, ":")
						expectedToolNames[parts[len(parts)-1]] = true
					}
				}
				reChecks := validateRoleIntegrationWorktree(role, integrationDir, sortedKeys(expectedToolNames), v.runtimeIterationHardLimit)
				for _, check := range rvr.Checks {
					if strings.HasPrefix(check.Name, fmt.Sprintf("rail_runtime_contract:%s:", role)) {
						if rel := failedCheckRelativePath(check.Name); rel != "" {
							reChecks = append(reChecks, checkRailRuntimeContract(role, integrationDir, rel))
						}
					}
				}
				if !anyCheckFailed(reChecks) {
					for i := range repairItems {
						if repairItems[i].Status == "attempted" {
							repairItems[i].Status = "succeeded"
						}
					}
					repairs = append(repairs, repairItems...)
					anyRolePassed = true
					roleStopReasons[role] = "validated"
					delete(remainingFailedChecks, role)
					return
				}
				lastAttempt := attempt == maxAttempts-1
				for i := range repairItems {
					if lastAttempt && repairItems[i].Status == "attempted" {
						repairItems[i].Status = "failed"
					}
				}
				repairs = append(repairs, repairItems...)
				failedChecks = repairableFailedChecks(reChecks)
				failed = nil
				for _, check := range failedChecks {
					failed = append(failed, map[string]any{
						"name": check.Name, "status": check.Status, "notes": check.Notes, "error": check.Error})
				}
				var names []string
				for _, check := range reChecks {
					if check.Status == "failed" {
						names = append(names, check.Name)
					}
				}
				remainingFailedChecks[role] = names
				if len(failed) == 0 {
					roleStopReasons[role] = "unrepairable_checks"
					return
				}
				if !tracker.Failed(failed, afterAttempt) {
					roleStopReasons[role] = tracker.StopReason
					return
				}
			}()
			// Python: continue 语义——scope_violation/no_progress/validated 等 break
			// 由 roleStopReasons 判定是否提前终止
			if roleStopReasons[role] != nil {
				break
			}
		}
	}

	// Python: 未尝试修复的角色回填失败清单
	for _, role := range sortedKeys(verificationResult.RoleResults) {
		if _, exists := remainingFailedChecks[role]; !exists {
			rvr := verificationResult.RoleResults[role]
			var failedNames []string
			for _, check := range rvr.Checks {
				if check.Status == "failed" {
					failedNames = append(failedNames, check.Name)
				}
			}
			if len(failedNames) > 0 && roleAttempts[role] == nil {
				remainingFailedChecks[role] = failedNames
			}
		}
	}
	// Python: run 级失败检查（不属于任何角色）
	roleFailedCheckNames := map[string]bool{}
	for _, roleResult := range verificationResult.RoleResults {
		for _, check := range roleResult.Checks {
			if check.Status == "failed" {
				roleFailedCheckNames[check.Name] = true
			}
		}
	}
	var runLevelFailed []string
	for _, check := range verificationResult.Checks {
		if check.Status == "failed" && !roleFailedCheckNames[check.Name] {
			runLevelFailed = append(runLevelFailed, check.Name)
		}
	}
	if len(runLevelFailed) > 0 {
		remainingFailedChecks["__run__"] = runLevelFailed
	}
	finalStatus := "passed"
	if len(remainingFailedChecks) > 0 {
		finalStatus = "failed"
	}
	status := "failed"
	if len(repairs) > 0 {
		status = "completed"
	}
	fixResult := &MemberFixResult{
		Status: status, Repairs: repairs,
		VerificationPath:        filepath.Join(filepath.Dir(worktreesDir), "verification.json"),
		FinalVerificationStatus: finalStatus,
		Metadata: map[string]any{
			"repair_attempts": len(repairs), "role_attempts": roleAttempts,
			"role_stop_reasons": roleStopReasons,
			"remaining_failed_checks": remainingFailedChecks,
			"any_role_repaired": anyRolePassed,
			"worktrees_dir": worktreesDir,
		},
	}
	if err := writeFixResult(output, fixResult); err != nil {
		return nil, err
	}
	return fixResult, nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// writeFixResult 落盘修复结果。
// Python: verification.py:1661-1664 _write_fix_result。
func writeFixResult(output string, result *MemberFixResult) error {
	data, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(output, data, 0o644)
}

// isWorktreeRepairableCheck 检查名是否可修复前缀。
// Python: verification.py:1016-1018 _is_worktree_repairable_check。
func isWorktreeRepairableCheck(name string) bool {
	for _, prefix := range repairableCheckPrefixes {
		if strings.HasPrefix(name, prefix) {
			return true
		}
	}
	return false
}

// isBlockingUnrepairableCheck 检查名是否阻断不可修复。
// Python: verification.py:1020-1024 _is_blocking_unrepairable_check。
func isBlockingUnrepairableCheck(name string) bool {
	if unrepairableCheckNames[name] {
		return true
	}
	for _, prefix := range unrepairableCheckPrefixes {
		if strings.HasPrefix(name, prefix) {
			return true
		}
	}
	return false
}

// repairableFailedChecks 失败且可修复的检查。
// Python: verification.py:1026-1030 _repairable_failed_checks。
func repairableFailedChecks(checks []VerificationCheck) []VerificationCheck {
	var out []VerificationCheck
	for _, check := range checks {
		if check.Status == "failed" && isWorktreeRepairableCheck(check.Name) {
			out = append(out, check)
		}
	}
	return out
}

// roleIsRepairable 角色检查集是否可修复。
// Python: verification.py:1032-1039 _role_is_repairable。
func roleIsRepairable(checks []VerificationCheck) bool {
	var failed []VerificationCheck
	for _, check := range checks {
		if check.Status == "failed" {
			failed = append(failed, check)
		}
	}
	if len(failed) == 0 {
		return false
	}
	for _, check := range failed {
		if isBlockingUnrepairableCheck(check.Name) {
			return false
		}
	}
	for _, check := range failed {
		if !isWorktreeRepairableCheck(check.Name) {
			return false
		}
	}
	return true
}

// anyCheckFailed 是否存在失败检查。
func anyCheckFailed(checks []VerificationCheck) bool {
	for _, check := range checks {
		if check.Status == "failed" {
			return true
		}
	}
	return false
}

// summarizeFailedChecks 失败检查摘要（限行限宽）。
// Python: verification.py:1041-1059 _summarize_failed_checks。
func summarizeFailedChecks(checks []map[string]any, limit int) string {
	var lines []string
	for index, rawCheck := range checks {
		if index >= limit {
			break
		}
		name := fmt.Sprintf("%v", rawCheck["name"])
		status := fmt.Sprintf("%v", rawCheck["status"])
		errorText := fmt.Sprintf("%v", rawCheck["error"])
		if errorText == "<nil>" || errorText == "" {
			errorText = fmt.Sprintf("%v", rawCheck["notes"])
			if errorText == "<nil>" {
				errorText = ""
			}
		}
		errorText = strings.TrimSpace(strings.ReplaceAll(errorText, "\r", " "))
		if len(errorText) > repairErrorLimit {
			errorText = strings.TrimRight(errorText[:repairErrorLimit-3], " \t") + "..."
		}
		line := fmt.Sprintf("- %s: %s", name, status)
		if errorText != "" {
			line = fmt.Sprintf("%s | %s", line, errorText)
		}
		lines = append(lines, line)
	}
	if len(checks) > limit {
		lines = append(lines, fmt.Sprintf("- ... %d more failed checks omitted", len(checks)-limit))
	}
	if len(lines) == 0 {
		return "- no failed checks"
	}
	return strings.Join(lines, "\n")
}

// listRepairContextFiles 修复上下文文件清单（限 80，排除 .pyc/__pycache__）。
// Python: verification.py:1061-1074 _list_repair_context_files。
func listRepairContextFiles(root string) []string {
	info, err := os.Stat(root)
	if err != nil || !info.IsDir() {
		return nil
	}
	var files []string
	filepath.WalkDir(root, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil || d.IsDir() {
			return nil
		}
		rel, relErr := filepath.Rel(root, path)
		if relErr != nil {
			return nil
		}
		relSlash := filepath.ToSlash(rel)
		if strings.HasSuffix(relSlash, ".pyc") || strings.Contains("/"+relSlash, "/__pycache__/") {
			return nil
		}
		files = append(files, relSlash)
		if len(files) >= repairContextFileLimit {
			files = append(files, "...")
			return filepath.SkipAll
		}
		return nil
	})
	return files
}

// repairFailedFileContext 失败检查直指文件的限量内容渲染。
// Python: verification.py:1076-1104 _repair_failed_file_context。
func repairFailedFileContext(root string, failedChecks []map[string]any) string {
	var relPaths []string
	seen := map[string]bool{}
	for _, check := range failedChecks {
		name := fmt.Sprintf("%v", check["name"])
		relPath := failedCheckRelativePath(name)
		if relPath != "" && !seen[relPath] {
			seen[relPath] = true
			relPaths = append(relPaths, relPath)
		}
	}
	if len(relPaths) == 0 {
		return "No failed check pointed to a concrete local file."
	}
	var sections []string
	rootAbs, _ := filepath.Abs(root)
	for index, relPath := range relPaths {
		if index >= 12 {
			break
		}
		path := filepath.Join(rootAbs, relPath)
		if !strings.HasPrefix(path, rootAbs+string(filepath.Separator)) && path != rootAbs {
			continue
		}
		data, err := os.ReadFile(path)
		if err != nil {
			sections = append(sections, fmt.Sprintf("### %s\n\nFile is missing.", relPath))
			continue
		}
		lines := strings.Split(string(data), "\n")
		limit := 220
		var rendered []string
		for i := 0; i < len(lines) && i < limit; i++ {
			rendered = append(rendered, fmt.Sprintf("%04d: %s", i+1, lines[i]))
		}
		if len(lines) > limit {
			rendered = append(rendered, "... content truncated ...")
		}
		sections = append(sections, fmt.Sprintf("### %s\n\n```text\n%s\n```", relPath, strings.Join(rendered, "\n")))
	}
	if len(sections) == 0 {
		return "No failed check pointed to a readable local file."
	}
	return strings.Join(sections, "\n\n")
}

// failedCheckRelativePath 从检查名提取相对路径。
// Python: verification.py:1106-1120 _failed_check_relative_path。
func failedCheckRelativePath(checkName string) string {
	for _, prefix := range []string{"python_compile:", "yaml_parse:", "json_parse:"} {
		if strings.HasPrefix(checkName, prefix) {
			value := strings.TrimPrefix(checkName, prefix)
			if idx := strings.Index(value, "/"); idx >= 0 {
				return value[idx+1:]
			}
			return value
		}
	}
	for _, prefix := range []string{"tool_file_ref:", "rail_file_ref:", "rail_runtime_contract:"} {
		if strings.HasPrefix(checkName, prefix) {
			value := strings.TrimPrefix(checkName, prefix)
			parts := strings.SplitN(value, ":", 2)
			if len(parts) == 2 {
				return strings.ReplaceAll(parts[1], "\\", "/")
			}
		}
	}
	return ""
}

// summarizeAgentResponse agent 响应摘要（前 8 行，限 800 字符）。
// Python: verification.py:1122-1128 _summarize_agent_response。
func summarizeAgentResponse(responseText string) string {
	var lines []string
	for _, line := range strings.Split(responseText, "\n") {
		if trimmed := strings.TrimSpace(line); trimmed != "" {
			lines = append(lines, trimmed)
		}
		if len(lines) >= 8 {
			break
		}
	}
	summary := strings.TrimSpace(strings.Join(lines, "\n"))
	if len(summary) > repairResponseLimit {
		return strings.TrimRight(summary[:repairResponseLimit-3], " \t") + "..."
	}
	if summary == "" {
		return "DeepAgent repair turn completed."
	}
	return summary
}

// snapshotRepairFiles 包字节快照（进度检查与保护文件回滚）。
// Python: verification.py:1130-1142 _snapshot_repair_files。
func snapshotRepairFiles(root string) map[string][]byte {
	snapshot := map[string][]byte{}
	info, err := os.Stat(root)
	if err != nil || !info.IsDir() {
		return snapshot
	}
	filepath.WalkDir(root, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil || d.IsDir() {
			return nil
		}
		if linfo, lerr := d.Info(); lerr == nil && linfo.Mode()&os.ModeSymlink != 0 {
			return nil
		}
		rel, relErr := filepath.Rel(root, path)
		if relErr != nil {
			return nil
		}
		relSlash := filepath.ToSlash(rel)
		if strings.Contains("/"+relSlash, "/__pycache__/") || strings.HasSuffix(relSlash, ".pyc") {
			return nil
		}
		data, rerr := os.ReadFile(path)
		if rerr == nil {
			snapshot[relSlash] = data
		}
		return nil
	})
	return snapshot
}

// enforceRepairScope 修复范围执行（越界回滚 + 违规清单）。
// Python: verification.py:1144-1162 _enforce_repair_scope。
func enforceRepairScope(root string, before, after map[string][]byte) []string {
	var changed []string
	for _, name := range unionKeys(before, after) {
		if !sameBytes(before[name], after[name]) {
			changed = append(changed, name)
		}
	}
	sort.Strings(changed)
	var forbidden []string
	for _, name := range changed {
		if PackageWriteAllowedCore(name, nil, true) {
			continue
		}
		if repairProtectedRuntimeNames[name] || strings.HasPrefix(name, "context/") || strings.HasPrefix(name, "memory/") {
			continue
		}
		forbidden = append(forbidden, name)
	}
	for _, name := range forbidden {
		path := filepath.Join(root, name)
		if _, exists := before[name]; exists {
			if err := os.MkdirAll(filepath.Dir(path), 0o755); err == nil {
				_ = os.WriteFile(path, before[name], 0o644)
			}
		} else {
			_ = os.Remove(path)
		}
	}
	return forbidden
}

// changedNames 快照差异键清单。
func changedNames(before, after map[string][]byte) []string {
	var changed []string
	for _, name := range unionKeys(before, after) {
		if !sameBytes(before[name], after[name]) {
			changed = append(changed, name)
		}
	}
	sort.Strings(changed)
	return changed
}

// sameSnapshot 快照全等。
func sameSnapshot(a, b map[string][]byte) bool {
	if len(a) != len(b) {
		return false
	}
	for key, value := range a {
		if !sameBytes(value, b[key]) {
			return false
		}
	}
	return true
}

// unionKeys 两快照键并集（排序）。
func unionKeys(a, b map[string][]byte) []string {
	seen := map[string]bool{}
	var out []string
	for key := range a {
		if !seen[key] {
			seen[key] = true
			out = append(out, key)
		}
	}
	for key := range b {
		if !seen[key] {
			seen[key] = true
			out = append(out, key)
		}
	}
	sort.Strings(out)
	return out
}

// sameBytes 字节等价（nil 与空等价）。
func sameBytes(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// validateActionResultsByRole 按角色校验计划动作执行/合并结果。
// Python: verification.py:908-1014 _validate_action_results_by_role。
func validateActionResultsByRole(plan *MemberOptimizationPlan, execPath string) map[string][]VerificationCheck {
	checksByRole := map[string][]VerificationCheck{}
	for i := range plan.Targets {
		checksByRole[plan.Targets[i].Role] = nil
	}
	var resultRows []map[string]any
	rows, err := loadExecutionResultData(execPath)
	if err != nil {
		loadCheck := []VerificationCheck{{Name: "execution_results_load", Status: "failed", Error: err.Error()}}
		for role := range checksByRole {
			checksByRole[role] = append(checksByRole[role], loadCheck...)
		}
		return checksByRole
	}
	resultRows = rows
	selectedRoles := map[string]bool{}
	for role := range checksByRole {
		selectedRoles[role] = true
	}
	// Python: outcomes_by_role = {role: evaluate_role_execution(...)}
	outcomesByRole := map[string]map[string]ActionExecutionOutcome{}
	for role := range checksByRole {
		outcomesByRole[role] = EvaluateRoleExecution(plan, rowsToExecutionResults(resultRows), role)
	}
	for i := range plan.Actions {
		action := &plan.Actions[i]
		roleChecks := checksByRole[action.Role]
		// Python: policy_check = validate_action_policy(action, roles)
		if policyCheck := actiongroups.ValidateActionPolicy(action, selectedRoles); !policyCheck.Valid {
			roleChecks = append(roleChecks, VerificationCheck{
				Name: fmt.Sprintf("action_policy:%s", action.ActionID), Status: "failed",
				Error: strings.Join(policyCheck.Errors, "; ")})
		}
		var matching []*MemberActionExecutionResult
		for _, row := range rowsToExecutionResults(resultRows) {
			if row.ActionID == action.ActionID {
				matching = append(matching, row)
			}
		}
		var result *MemberActionExecutionResult
		if len(matching) == 1 {
			result = matching[0]
		}
		outcome := outcomesByRole[action.Role][action.ActionID]
		if result == nil || outcome.ActionID == "" && outcome.Reason == "" && !outcome.Satisfied {
			errorText := "missing execution result"
			if outcome.Reason != "" || outcome.Satisfied {
				errorText = outcome.Reason
			}
			roleChecks = append(roleChecks, VerificationCheck{
				Name: fmt.Sprintf("action_result:%s", action.ActionID), Status: "failed", Error: errorText})
			checksByRole[action.Role] = roleChecks
			continue
		}
		// Python: result_action dict → 策略复检
		resultAction := map[string]any{
			"action_id": action.ActionID, "role": action.Role,
			"action_group": action.ActionGroup, "operation": action.Operation,
			"target_path": action.TargetPath,
			"declared_write_paths": toAnySlice(action.DeclaredWritePaths),
			"candidate_query":      action.CandidateQuery,
			"install_ref":          action.InstallRef,
			"allowed_tools":        action.AllowedTools,
			"constraints":          action.Constraints,
		}
		if result != nil {
			if dp, ok := anyToStringList(result.DeclaredWritePaths); ok {
				resultAction["declared_write_paths"] = toAnySlice(dp)
			}
		}
		if policyCheck := actiongroups.ValidateActionPolicy(resultAction, selectedRoles); !policyCheck.Valid {
			roleChecks = append(roleChecks, VerificationCheck{
				Name: fmt.Sprintf("execution_action_policy:%s", action.ActionID), Status: "failed",
				Error: strings.Join(policyCheck.Errors, "; ")})
			checksByRole[action.Role] = roleChecks
			continue
		}
		if !outcome.Satisfied {
			roleChecks = append(roleChecks, VerificationCheck{
				Name: fmt.Sprintf("action_result:%s", action.ActionID), Status: "failed", Error: outcome.Reason})
			checksByRole[action.Role] = roleChecks
			continue
		}
		roleChecks = append(roleChecks, VerificationCheck{
			Name: fmt.Sprintf("action_result:%s", action.ActionID), Status: "passed"})
		checksByRole[action.Role] = roleChecks
	}
	for role, roleChecks := range checksByRole {
		executionErrors := RoleExecutionErrors(plan, rowsToExecutionResults(resultRows), role)
		check := VerificationCheck{Name: fmt.Sprintf("role_action_bundle:%s", role), Status: "passed"}
		if len(executionErrors) > 0 {
			check.Status = "failed"
			check.Error = strings.Join(executionErrors, "; ")
		}
		checksByRole[role] = append(roleChecks, check)
	}
	return checksByRole
}

// executionResultFromRow dict 行 → MemberActionExecutionResult（JSON 键对齐）。
func executionResultFromRow(row map[string]any) *MemberActionExecutionResult {
	result := &MemberActionExecutionResult{}
	result.ActionID, _ = row["action_id"].(string)
	result.Role, _ = row["role"].(string)
	result.Status, _ = row["status"].(string)
	result.MergeStatus, _ = row["merge_status"].(string)
	result.Error, _ = row["error"].(string)
	if list, ok := row["changed_files"].([]any); ok {
		for _, item := range list {
			if s, isStr := item.(string); isStr {
				result.ChangedFiles = append(result.ChangedFiles, s)
			}
		}
	}
	if list, ok := row["declared_write_paths"].([]any); ok {
		for _, item := range list {
			if s, isStr := item.(string); isStr {
				result.DeclaredWritePaths = append(result.DeclaredWritePaths, s)
			}
		}
	}
	return result
}

// rowsToExecutionResults dict 行 → 执行结果结构。
func rowsToExecutionResults(rows []map[string]any) []*MemberActionExecutionResult {
	var out []*MemberActionExecutionResult
	for _, row := range rows {
		out = append(out, executionResultFromRow(row))
	}
	return out
}

// anyToStringList any → []string（typed-nil 装箱守卫：nil slice 装箱后
// type switch 仍命中对应 case，须显式排除——nil-interface 陷阱同族）。
func anyToStringList(value any) ([]string, bool) {
	if value == nil {
		return nil, false
	}
	switch v := value.(type) {
	case []string:
		if v == nil {
			return nil, false
		}
		return v, true
	case []any:
		var out []string
		for _, item := range v {
			out = append(out, fmt.Sprintf("%v", item))
		}
		return out, true
	}
	return nil, false
}

// sortedKeys map 键排序清单。
func sortedKeys[T any](m map[string]T) []string {
	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

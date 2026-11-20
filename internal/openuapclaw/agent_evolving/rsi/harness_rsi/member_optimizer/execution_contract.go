// execution_contract.go 依赖感知 member 优化动作的完成契约。
// Python: member_optimizer/execution_contract.py（全量对齐，120 行）。

package memberoptimizer

import (
	"fmt"
	"sort"

	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_evolving/rsi/harness_rsi/member_optimizer/action_groups"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ActionExecutionOutcome 单个既定动作是否被执行分支满足。
// Python: execution_contract.py:13-18 ActionExecutionOutcome。
type ActionExecutionOutcome struct {
	// ActionID 动作标识
	ActionID string
	// Satisfied 是否满足
	Satisfied bool
	// Reason 理由
	Reason string
}

// ──────────────────────────── 导出函数 ────────────────────────────

// ActionBundleKey 显式作用域动作束的 role/issue 键（非单 issue 返回 false）。
// Python: execution_contract.py:21-31 action_bundle_key。
func ActionBundleKey(action any) ([2]string, bool) {
	// Python: issue_ids = [str(i) for i in _list_value(_get(action, "attributed_issue_ids", [])) if str(i)]
	var issueIDs []string
	if list, ok := actionFieldValue(action, "attributed_issue_ids").([]any); ok {
		for _, item := range list {
			if text := pyStrOrEmpty(item); text != "" {
				issueIDs = append(issueIDs, text)
			}
		}
	}
	// Python: if len(issue_ids) != 1: return None; role 为空 return None
	if len(issueIDs) != 1 {
		return [2]string{}, false
	}
	role := pyStrOrEmpty(actionFieldValue(action, "role"))
	if role == "" {
		return [2]string{}, false
	}
	return [2]string{role, issueIDs[0]}, true
}

// EvaluateRoleExecution 评估角色全部既定动作（含回退分支）。
// Python: execution_contract.py:34-118 evaluate_role_execution。
func EvaluateRoleExecution(plan *MemberOptimizationPlan, executionResults []*MemberActionExecutionResult, role string) map[string]ActionExecutionOutcome {
	// Python: planned = [action for action in plan.actions if action.role == role]
	var planned []*MemberOptimizationAction
	for i := range plan.Actions {
		if plan.Actions[i].Role == role {
			planned = append(planned, &plan.Actions[i])
		}
	}
	// Python: rows_by_id = {}; for row in execution_results: ...
	rowsByID := map[string][]*MemberActionExecutionResult{}
	for _, row := range executionResults {
		if row.ActionID != "" {
			rowsByID[row.ActionID] = append(rowsByID[row.ActionID], row)
		}
	}
	// Python: actions_by_id = {...}; direct_success = {...len==1 and _is_merged_success...}
	actionsByID := map[string]*MemberOptimizationAction{}
	for _, action := range planned {
		actionsByID[action.ActionID] = action
	}
	directSuccess := map[string]bool{}
	for actionID, rows := range rowsByID {
		if len(rows) == 1 && isMergedSuccess(rows[0]) {
			directSuccess[actionID] = true
		}
	}
	// Python: successful_fallbacks_by_dependency
	successfulFallbacksByDependency := map[string][]string{}
	for _, action := range planned {
		if action.RunIf != "dependency_failed" || !directSuccess[action.ActionID] {
			continue
		}
		for _, dependencyID := range action.DependsOn {
			dependency := actionsByID[dependencyID]
			if dependency == nil || !sameBundleOrUnscoped(dependency, action) {
				continue
			}
			successfulFallbacksByDependency[dependencyID] = append(successfulFallbacksByDependency[dependencyID], action.ActionID)
		}
	}
	// Python: outcomes = {}; for action in planned: ...
	outcomes := map[string]ActionExecutionOutcome{}
	for _, action := range planned {
		rows := rowsByID[action.ActionID]
		// Python: if not rows: outcomes[...] = ActionExecutionOutcome(satisfied=False, reason="missing execution result")
		if len(rows) == 0 {
			outcomes[action.ActionID] = ActionExecutionOutcome{ActionID: action.ActionID, Satisfied: false, Reason: "missing execution result"}
			continue
		}
		// Python: if len(rows) > 1: duplicate
		if len(rows) > 1 {
			outcomes[action.ActionID] = ActionExecutionOutcome{
				ActionID: action.ActionID, Satisfied: false,
				Reason: fmt.Sprintf("duplicate execution results: %d", len(rows)),
			}
			continue
		}
		row := rows[0]
		// Python: if _is_merged_success(row): satisfied=True
		if isMergedSuccess(row) {
			outcomes[action.ActionID] = ActionExecutionOutcome{ActionID: action.ActionID, Satisfied: true}
			continue
		}
		status := pyStrOrEmpty(row.Status)
		// Python: dependency_failed_action_skipped = run_if == "dependency_failed" and status == "skipped"
		dependencyFailedActionSkipped := action.RunIf == "dependency_failed" && status == "skipped"
		allDependenciesSucceeded := len(action.DependsOn) > 0
		for _, dependencyID := range action.DependsOn {
			if !directSuccess[dependencyID] {
				allDependenciesSucceeded = false
				break
			}
		}
		// Python: if dependency_failed_action_skipped and all_dependencies_succeeded: satisfied
		if dependencyFailedActionSkipped && allDependenciesSucceeded {
			outcomes[action.ActionID] = ActionExecutionOutcome{
				ActionID: action.ActionID, Satisfied: true,
				Reason: "fallback not needed because dependencies succeeded",
			}
			continue
		}
		// Python: successful_fallbacks = ...; if status == "failed" and successful_fallbacks: satisfied
		successfulFallbacks := successfulFallbacksByDependency[action.ActionID]
		if status == "failed" && len(successfulFallbacks) > 0 {
			sorted := append([]string{}, successfulFallbacks...)
			sort.Strings(sorted)
			outcomes[action.ActionID] = ActionExecutionOutcome{
				ActionID: action.ActionID, Satisfied: true,
				Reason: fmt.Sprintf("failed primary action replaced by successful fallback(s): %v", sorted),
			}
			continue
		}
		// Python: reason = f"execution status={status!r}, merge_status={merge_status!r}"
		reason := fmt.Sprintf("execution status=%q, merge_status=%q", status, pyStrOrEmpty(row.MergeStatus))
		if row.Error != "" {
			reason += ": " + row.Error
		}
		outcomes[action.ActionID] = ActionExecutionOutcome{ActionID: action.ActionID, Satisfied: false, Reason: reason}
	}
	return outcomes
}

// actionFieldValue 字段读取（map 与 ActionGetter 双轨；Python _get 等价）。
func actionFieldValue(action any, key string) any {
	if m, ok := action.(map[string]any); ok {
		return m[key]
	}
	if g, ok := action.(actiongroups.ActionGetter); ok {
		return g.ActionFieldValue(key)
	}
	return nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// isMergedSuccess 合并成功判定。
// Python: execution_contract.py:139-140 _is_merged_success。
func isMergedSuccess(row *MemberActionExecutionResult) bool {
	// Python: status == "succeeded" and merge_status == "merged"
	return row.Status == "succeeded" && row.MergeStatus == "merged"
}

// RoleExecutionErrors 单候选角色的阻断执行错误清单。
// Python: execution_contract.py:121-131 role_execution_errors。
func RoleExecutionErrors(plan *MemberOptimizationPlan, executionResults []*MemberActionExecutionResult, role string) []string {
	// Python: planned = [...]; if not planned: return ["role has no planned actions"]
	var planned bool
	for i := range plan.Actions {
		if plan.Actions[i].Role == role {
			planned = true
			break
		}
	}
	if !planned {
		return []string{"role has no planned actions"}
	}
	// Python: return [f"{action_id}: {outcome.reason}" for ... if not outcome.satisfied]
	outcomes := EvaluateRoleExecution(plan, executionResults, role)
	ids := make([]string, 0, len(outcomes))
	for id := range outcomes {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	var errs []string
	for _, id := range ids {
		if !outcomes[id].Satisfied {
			errs = append(errs, fmt.Sprintf("%s: %s", id, outcomes[id].Reason))
		}
	}
	return errs
}

// sameBundleOrUnscoped 依赖是否同束或未作用域化。
// Python: execution_contract.py:132-142 _same_bundle_or_unscoped。
func sameBundleOrUnscoped(dependency, action *MemberOptimizationAction) bool {
	// Python: dep_key = action_bundle_key(dependency); act_key = action_bundle_key(action)
	depKey, depOK := ActionBundleKey(dependency)
	actKey, actOK := ActionBundleKey(action)
	// Python: if dep_key is None or act_key is None: return True
	if !depOK || !actOK {
		return true
	}
	return depKey == actKey
}

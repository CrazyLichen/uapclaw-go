// action_planner.go 开单——DeepAgent 出单 + 外层严审。
// Python: member_optimizer/action_planner.py（全量对齐，1,846 行）。
// planner 的 DeepAgent 是第一个能输出可执行动作的阶段；
// 外层在返回 MemberOptimizationPlan 前做确定性校验。

package memberoptimizer

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"gopkg.in/yaml.v3"

	harnessrsi "github.com/uapclaw/uapclaw-go/internal/evolving/rsi/harness_rsi"
	"github.com/uapclaw/uapclaw-go/internal/evolving/rsi/harness_rsi/member_optimizer/action_groups"
)

// ──────────────────────────── 常量 ────────────────────────────

// maxActionsPerIssue 每个问题最多关联的手术数。
// Python: action_planner.py:53 _MAX_ACTIONS_PER_ISSUE。
const maxActionsPerIssue = 3

// inactionableMechanism 证据不足机制值。
// Python: action_planner.py:1196 _INACTIONABLE_MECHANISM。
const inactionableMechanism = "insufficient_role_evidence"

// roleIdentityScopes identity.md 允许的 surface_scope。
// Python: action_planner.py:44 _ROLE_IDENTITY_SCOPES。
var roleIdentityScopes = map[string]bool{"role_identity": true, "duty_boundary": true}

// soulScopes soul.md 允许的 surface_scope。
// Python: action_planner.py:45 _SOUL_SCOPES。
var soulScopes = map[string]bool{"durable_operating_principle": true}

// promptCorePaths 核心提示词文件。
// Python: action_planner.py:46 _PROMPT_CORE_PATHS。
var promptCorePaths = map[string]bool{"identity.md": true, "soul.md": true}

// runIfValues run_if 允许值。
// Python: action_planner.py:47 _RUN_IF_VALUES。
var runIfValues = map[string]bool{
	"dependency_succeeded": true, "dependency_failed": true, "always": true,
}

// soulProceduralTerms soul.md 程序化术语检测。
// Python: action_planner.py:383-393 procedural_terms。
var soulProceduralTerms = []string{
	"checklist", "procedure", "step", "specific", "git", "merge", "rebase",
	"verification", "verify", "recover",
}

// ──────────────────────────── 结构体 ────────────────────────────

// ActionPlanRejectedError 候选方案在反馈预算内无法满足契约。
// Python: action_planner.py:56-57 ActionPlanRejectedError(RuntimeError)。
type ActionPlanRejectedError struct {
	// Msg 错误消息
	Msg string
}

// Error 实现 error 接口。
func (e *ActionPlanRejectedError) Error() string { return e.Msg }

// ──────────────────────────── 导出函数 ────────────────────────────

// ValidatePlanStructure 拒绝畸形容器（语义校验器之前）。
// Python: action_planner.py:59-94 _validate_plan_structure。
func ValidatePlanStructure(planData map[string]any) []string {
	// Python: errors = []; actions = plan_data.get("actions")
	var errs []string
	actions, isList := planData["actions"].([]any)
	if !isList {
		return []string{"actions must be a list (use [] for no actions)"}
	}
	// Python: list_fields = (...); for index, action in enumerate(actions): ...
	listFields := []string{"declared_write_paths", "attributed_issue_ids", "depends_on",
		"allowed_tools", "allowed_skills", "risk_notes"}
	for index, action := range actions {
		actionMap, isMap := action.(map[string]any)
		if !isMap {
			errs = append(errs, fmt.Sprintf("actions[%d] must be an object", index))
			continue
		}
		// Python: for key in ("action_id", "role", ...): 字符串类型校验
		for _, key := range []string{"action_id", "role", "action_group", "operation", "target_path"} {
			if value, exists := actionMap[key]; exists && value != nil {
				if _, isStr := value.(string); !isStr {
					errs = append(errs, fmt.Sprintf("actions[%d].%s must be a string", index, key))
				}
			}
		}
		// Python: for key in list_fields: 值必须为字符串列表
		for _, key := range listFields {
			value, exists := actionMap[key]
			if !exists {
				continue
			}
			list, isList := value.([]any)
			if !isList {
				errs = append(errs, fmt.Sprintf("actions[%d].%s must be a list of strings", index, key))
				continue
			}
			for _, item := range list {
				if _, isStr := item.(string); !isStr {
					errs = append(errs, fmt.Sprintf("actions[%d].%s must be a list of strings", index, key))
					break
				}
			}
		}
		// Python: if not isinstance(action.get("constraints", {}), dict): ...
		if value, exists := actionMap["constraints"]; exists && value != nil {
			if _, isMap := value.(map[string]any); !isMap {
				errs = append(errs, fmt.Sprintf("actions[%d].constraints must be an object", index))
			}
		}
	}
	// Python: waves = plan_data.get("action_waves", []); 嵌套列表字符串校验
	waves, isList := planData["action_waves"].([]any)
	if planData["action_waves"] != nil && !isList {
		return append(errs, "action_waves must be a list of lists of action IDs")
	}
	for _, wave := range waves {
		waveList, isList := wave.([]any)
		if !isList {
			errs = append(errs, "action_waves must be a list of lists of action IDs")
			continue
		}
		for _, item := range waveList {
			if _, isStr := item.(string); !isStr {
				errs = append(errs, "action_waves must be a list of lists of action IDs")
				break
			}
		}
	}
	return errs
}

// ValidateAndRepairPlanData 校验方案并仅修复机械性波次错误。
// Python: action_planner.py:372-395 _validate_and_repair_plan_data。
func ValidateAndRepairPlanData(planData map[string]any, selectedRoles map[string]bool, targets []MemberOptimizationTarget, actionDefinitions []harnessrsi.ActionDefinition) []string {
	// Python: errors = _validate_plan(...)
	errs := ValidatePlan(planData, selectedRoles, targets, actionDefinitions)
	if len(errs) == 0 {
		return nil
	}
	// Python: waves = build_action_waves_from_data(...); plan_data["action_waves"] = waves
	if waves, err := actiongroups.BuildActionWavesFromData(planActionsList(planData)); err == nil {
		planData["action_waves"] = wavesAny(waves)
	}
	// Python: re_errors = _validate_plan(...)
	reErrs := ValidatePlan(planData, selectedRoles, targets, actionDefinitions)
	if len(reErrs) == 0 {
		return nil
	}
	// Python: waves = build_waves_from_deps_only(...)
	if waves, err := actiongroups.BuildWavesFromDepsOnly(planActionsList(planData)); err == nil {
		planData["action_waves"] = wavesAny(waves)
	}
	return ValidatePlan(planData, selectedRoles, targets, actionDefinitions)
}

// ValidatePlan 校验解析后的方案 dict，返回错误清单。
// Python: action_planner.py:97-151 _validate_plan。
func ValidatePlan(planData map[string]any, selectedRoles map[string]bool, targets []MemberOptimizationTarget, actionDefinitions []harnessrsi.ActionDefinition) []string {
	// Python: errors = []; targets_by_role = {...}
	var errs []string
	targetsByRole := map[string]*MemberOptimizationTarget{}
	for i := range targets {
		targetsByRole[targets[i].Role] = &targets[i]
	}
	// Python: action_ids_seen / all_action_ids / actions_by_role
	actionIDsSeen := map[string]bool{}
	allActionIDs := map[string]bool{}
	actionsByRole := map[string][]map[string]any{}
	// Python: for action in plan_data.get("actions", []): ...
	for _, action := range planActionsList(planData) {
		actionID := pyStr(ActionMapField(action, "action_id"))
		allActionIDs[actionID] = true
		role := pyStr(ActionMapField(action, "role"))
		actionsByRole[role] = append(actionsByRole[role], action)
		// Python: errs = _validate_action(...); errors.extend(f"action {action_id}: {e}")
		for _, e := range validatePlanAction(action, selectedRoles) {
			errs = append(errs, fmt.Sprintf("action %s: %s", actionID, e))
		}
		// Python: surface_errors / alignment_errors / attribution_errors / existing_surface_errors / definition_errors
		for _, e := range validatePromptSurfaceSelection(action, targetsByRole) {
			errs = append(errs, fmt.Sprintf("action %s: %s", actionID, e))
		}
		for _, e := range validateOptimizationSurfaceAlignment(action, targetsByRole) {
			errs = append(errs, fmt.Sprintf("action %s: %s", actionID, e))
		}
		for _, e := range validateActionIssueAttribution(action, targetsByRole) {
			errs = append(errs, fmt.Sprintf("action %s: %s", actionID, e))
		}
		for _, e := range validateExistingSurfaceOperation(action, targetsByRole) {
			errs = append(errs, fmt.Sprintf("action %s: %s", actionID, e))
		}
		for _, e := range validateOfferedActionDefinition(action, actionDefinitions) {
			errs = append(errs, fmt.Sprintf("action %s: %s", actionID, e))
		}
		// Python: if action_id in action_ids_seen: duplicate
		if actionIDsSeen[actionID] {
			errs = append(errs, fmt.Sprintf("duplicate action_id: %s", actionID))
		}
		actionIDsSeen[actionID] = true
	}
	// Python: skill_search_add_fallback / bundle_cohesion / target_coverage
	errs = append(errs, validateSkillSearchAddFallback(planData)...)
	errs = append(errs, validateActionBundleCohesion(planData)...)
	errs = append(errs, validateActionableTargetCoverage(targets, actionsByRole, actionDefinitions)...)
	// Python: 波次引用校验
	waves, _ := planData["action_waves"].([]any)
	for waveIdx, wave := range waves {
		waveList, _ := wave.([]any)
		for _, item := range waveList {
			actionID := pyStr(item)
			if !allActionIDs[actionID] {
				errs = append(errs, fmt.Sprintf("wave[%d] references unknown action_id: %s", waveIdx, actionID))
			}
		}
	}
	// Python: 波次与动作集合一致性
	actionIDsFromWaves := map[string]bool{}
	for _, wave := range waves {
		waveList, _ := wave.([]any)
		for _, item := range waveList {
			actionIDsFromWaves[pyStr(item)] = true
		}
	}
	if !setsEqual(actionIDsFromWaves, allActionIDs) {
		var missing, extra []string
		for id := range allActionIDs {
			if !actionIDsFromWaves[id] {
				missing = append(missing, id)
			}
		}
		for id := range actionIDsFromWaves {
			if !allActionIDs[id] {
				extra = append(extra, id)
			}
		}
		sort.Strings(missing)
		sort.Strings(extra)
		if len(missing) > 0 {
			errs = append(errs, fmt.Sprintf("actions missing from waves: %v", missing))
		}
		if len(extra) > 0 {
			errs = append(errs, fmt.Sprintf("waves reference unknown action_ids: %v", extra))
		}
	}
	return errs
}

// ValidateActionBundleCohesion 要求相关多手术修复留在同一问题束内。
// Python: action_planner.py:154-217 _validate_action_bundle_cohesion。
func ValidateActionBundleCohesion(planData map[string]any) []string {
	return validateActionBundleCohesion(planData)
}

// NormalizeRequiredDeclaredPaths 补全所选面隐含的机械性 manifest 声明。
// Python: action_planner.py:548-583 _normalize_required_declared_paths。
func NormalizeRequiredDeclaredPaths(planData map[string]any) {
	// Python: for action in plan_data.get("actions", []): ...
	for _, action := range planActionsList(planData) {
		targetPath := normalizePlanPath(pyStr(ActionMapField(action, "target_path")))
		actionGroup := pyStr(ActionMapField(action, "action_group"))
		operation := pyStr(ActionMapField(action, "operation"))
		// Python: declared_paths 非列表回落单元素
		var declaredPaths []string
		if list, ok := action["declared_write_paths"].([]any); ok {
			for _, item := range list {
				declaredPaths = append(declaredPaths, pyStr(item))
			}
		} else if raw := ActionMapField(action, "declared_write_paths"); raw != nil {
			declaredPaths = []string{pyStr(raw)}
		}
		// Python: normalized/seen 保序去重
		var normalized []string
		seen := map[string]bool{}
		for _, path := range declaredPaths {
			rel := normalizePlanPath(path)
			if rel != "" && !seen[rel] {
				normalized = append(normalized, rel)
				seen[rel] = true
			}
		}
		// Python: required = _required_declared_paths_for_action(...)
		for _, rel := range requiredDeclaredPathsForAction(actionGroup, operation, targetPath) {
			if rel != "" && !seen[rel] {
				normalized = append(normalized, rel)
				seen[rel] = true
			}
		}
		action["declared_write_paths"] = toAnySlice(normalized)
	}
}

// BindImmutableHypotheses 方案校验后附加紧凑源契约（不经 LLM 改写）。
// Python: action_planner.py:752-821 _bind_immutable_hypotheses。
func BindImmutableHypotheses(planData map[string]any, hypotheses []map[string]any) {
	bindImmutableHypotheses(planData, hypotheses)
}

// AnnotateActionBundles 持久化每个原子束共享的问题与所需行为。
// Python: action_planner.py:824-859 _annotate_action_bundles。
func AnnotateActionBundles(planData map[string]any, hypotheses []map[string]any) {
	annotateActionBundles(planData, hypotheses)
}

// WritePlan 落盘方案（plan.yaml）。
// Python: action_planner.py:1635-1662 write_plan。
func WritePlan(plan *MemberOptimizationPlan, outputDir string) (string, error) {
	// Python: path = output_dir / "plan.yaml"; path.parent.mkdir(...)
	path := filepath.Join(outputDir, "plan.yaml")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return "", err
	}
	// Python: payload = {...targets asdict / actions asdict / waves / metadata...}
	// Go 侧结构体 yaml tag 与 Python 字段名一一对应（asdict 等价）
	payload := map[string]any{
		"plan_id":      plan.PlanID,
		"targets":      plan.Targets,
		"actions":      plan.Actions,
		"action_waves": wavesAny(plan.ActionWaves),
		"metadata":     pyOrNone(plan.Metadata),
	}
	data, err := yaml.Marshal(payload)
	if err != nil {
		return "", err
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return "", err
	}
	return path, nil
}

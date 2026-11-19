// action_planner_impl.go 开单校验器群与 agent/编排器实现（action_planner.py 后半）。
// 本文件与 action_planner.go 构成 Python action_planner.py 的一文件对齐拆分
// （声明导出面 vs 实现；Go 单文件超长拆分的例外已按规则登记——校验器群与
// 编排器分属文件不同区段，符号可对照表见 spec）。

package memberoptimizer

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/google/uuid"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm"
	modelclients "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/model_clients"
	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	harnessrsi "github.com/uapclaw/uapclaw-go/internal/evolving/rsi/harness_rsi"
	"github.com/uapclaw/uapclaw-go/internal/evolving/rsi/harness_rsi/member_optimizer/action_groups"
	agentface "github.com/uapclaw/uapclaw-go/internal/evolving/rsi/harness_rsi/member_optimizer/agentface"
	"gopkg.in/yaml.v3"
)

// ──────────────────────────── 非导出函数（校验器群） ────────────────────────────

// planActionsList plan_data["actions"] → []map（非对象跳过）。
func planActionsList(planData map[string]any) []map[string]any {
	list, _ := planData["actions"].([]any)
	var out []map[string]any
	for _, item := range list {
		if m, ok := item.(map[string]any); ok {
			out = append(out, m)
		}
	}
	return out
}

// ActionMapField map 字段读取（Python action.get(key, "") 等价）。
func ActionMapField(action map[string]any, key string) any {
	return action[key]
}

// wavesAny [][]string → []any。
func wavesAny(waves [][]string) any {
	out := make([]any, 0, len(waves))
	for _, wave := range waves {
		out = append(out, toAnySlice(wave))
	}
	return out
}

// toAnySlice []string → []any。
func toAnySlice(list []string) []any {
	out := make([]any, 0, len(list))
	for _, item := range list {
		out = append(out, item)
	}
	return out
}

// setsEqual 集合相等。
func setsEqual(a, b map[string]bool) bool {
	if len(a) != len(b) {
		return false
	}
	for key := range a {
		if !b[key] {
			return false
		}
	}
	return true
}

// validatePlanAction 单条手术校验（策略 + run_if）。
// Python: action_planner.py:96-112 _validate_action。
func validatePlanAction(action map[string]any, selectedRoles map[string]bool) []string {
	// Python: check = validate_action_policy(action, selected_roles); errors = list(check.errors)
	check := actiongroups.ValidateActionPolicy(action, selectedRoles)
	errs := append([]string{}, check.Errors...)
	// Python: run_if = str(action.get("run_if", "dependency_succeeded") or ...)
	runIf := pyStr(ActionMapField(action, "run_if"))
	if runIf == "" {
		runIf = "dependency_succeeded"
	}
	// Python: if run_if not in _RUN_IF_VALUES: errors.append(f"run_if must be one of {sorted(_RUN_IF_VALUES)}, got {run_if!r}")
	if !runIfValues[runIf] {
		keys := []string{"always", "dependency_failed", "dependency_succeeded"}
		errs = append(errs, fmt.Sprintf("run_if must be one of %v, got %q", keys, runIf))
	}
	return errs
}

// validateSkillSearchAddFallback skill/add 回退必须显式失败语义。
// Python: action_planner.py:500-527 _validate_skill_search_add_fallback。
func validateSkillSearchAddFallback(planData map[string]any) []string {
	// Python: actions = {action_id: action ...}
	actions := map[string]map[string]any{}
	for _, action := range planActionsList(planData) {
		actions[pyStr(ActionMapField(action, "action_id"))] = action
	}
	// Python: search_action_ids = {...skill/search...}
	searchActionIDs := map[string]bool{}
	for actionID, action := range actions {
		if pyStr(ActionMapField(action, "action_group")) == "skill" && pyStr(ActionMapField(action, "operation")) == "search" {
			searchActionIDs[actionID] = true
		}
	}
	var errs []string
	for actionID, action := range actions {
		if pyStr(ActionMapField(action, "action_group")) != "skill" || pyStr(ActionMapField(action, "operation")) != "add" {
			continue
		}
		// Python: depends_on = {str(dep) for dep in action.get("depends_on", [])}
		dependsOn := map[string]bool{}
		if deps, ok := ActionMapField(action, "depends_on").([]any); ok {
			for _, dep := range deps {
				dependsOn[pyStr(dep)] = true
			}
		}
		// Python: if not (depends_on & search_action_ids): continue
		overlap := false
		for dep := range dependsOn {
			if searchActionIDs[dep] {
				overlap = true
				break
			}
		}
		if !overlap {
			continue
		}
		// Python: run_if = str(action.get("run_if", "dependency_succeeded") or ...)
		runIf := pyStr(ActionMapField(action, "run_if"))
		if runIf == "" {
			runIf = "dependency_succeeded"
		}
		if runIf != "dependency_failed" {
			errs = append(errs, fmt.Sprintf("action %s: skill/add fallback after skill/search must set run_if=dependency_failed", actionID))
		}
	}
	return errs
}

// validateActionBundleCohesion 相关多手术修复必须留在同一问题束。
// Python: action_planner.py:154-217 _validate_action_bundle_cohesion。
func validateActionBundleCohesion(planData map[string]any) []string {
	// Python: actions = [...]; actions_by_id = {...}
	actions := planActionsList(planData)
	actionsByID := map[string]map[string]any{}
	for _, action := range actions {
		actionID := pyStr(ActionMapField(action, "action_id"))
		if actionID != "" {
			actionsByID[actionID] = action
		}
	}
	// Python: bundle_by_action_id / actions_by_bundle
	bundleByActionID := map[string][2]string{}
	actionsByBundle := map[[2]string][]map[string]any{}
	bundleKeys := [][2]string{}
	for _, action := range actions {
		var issueIDs []string
		if list, ok := ActionMapField(action, "attributed_issue_ids").([]any); ok {
			for _, item := range list {
				if text := pyStr(item); text != "" {
					issueIDs = append(issueIDs, text)
				}
			}
		}
		actionID := pyStr(ActionMapField(action, "action_id"))
		role := pyStr(ActionMapField(action, "role"))
		if actionID != "" && role != "" && len(issueIDs) == 1 {
			bundle := [2]string{role, issueIDs[0]}
			bundleByActionID[actionID] = bundle
			if _, exists := actionsByBundle[bundle]; !exists {
				bundleKeys = append(bundleKeys, bundle)
			}
			actionsByBundle[bundle] = append(actionsByBundle[bundle], action)
		}
	}
	var errs []string
	// Python: for bundle, bundle_actions in sorted(...): 超上限
	sort.Slice(bundleKeys, func(i, j int) bool {
		if bundleKeys[i][0] != bundleKeys[j][0] {
			return bundleKeys[i][0] < bundleKeys[j][0]
		}
		return bundleKeys[i][1] < bundleKeys[j][1]
	})
	for _, bundle := range bundleKeys {
		bundleActions := actionsByBundle[bundle]
		if len(bundleActions) > maxActionsPerIssue {
			errs = append(errs, fmt.Sprintf("role %s issue %s has %d actions; at most %d associated actions are allowed",
				bundle[0], bundle[1], len(bundleActions), maxActionsPerIssue))
		}
	}
	// Python: 依赖跨束拒绝
	for _, action := range actions {
		actionID := pyStr(ActionMapField(action, "action_id"))
		actionBundle, hasBundle := bundleByActionID[actionID]
		deps, _ := ActionMapField(action, "depends_on").([]any)
		for _, dep := range deps {
			dependencyID := pyStr(dep)
			dependency, ok := actionsByID[dependencyID]
			if !ok {
				errs = append(errs, fmt.Sprintf("action %s depends on unknown action_id: %s", actionID, dependencyID))
				continue
			}
			dependencyBundle := bundleByActionID[pyStr(ActionMapField(dependency, "action_id"))]
			if hasBundle && dependencyBundle != actionBundle {
				errs = append(errs, fmt.Sprintf("action %s depends on unrelated action %s; "+
					"dependencies must stay within the same role and attributed issue", actionID, dependencyID))
			}
		}
	}
	// Python: 束内连通性（BFS）
	for _, bundle := range bundleKeys {
		bundleActions := actionsByBundle[bundle]
		if len(bundleActions) <= 1 {
			continue
		}
		actionIDs := map[string]bool{}
		for _, action := range bundleActions {
			actionIDs[pyStr(ActionMapField(action, "action_id"))] = true
		}
		adjacency := map[string]map[string]bool{}
		for actionID := range actionIDs {
			adjacency[actionID] = map[string]bool{}
		}
		for _, action := range bundleActions {
			actionID := pyStr(ActionMapField(action, "action_id"))
			deps, _ := ActionMapField(action, "depends_on").([]any)
			for _, dep := range deps {
				dependencyID := pyStr(dep)
				if !actionIDs[dependencyID] {
					continue
				}
				adjacency[actionID][dependencyID] = true
				adjacency[dependencyID][actionID] = true
			}
		}
		// Python: visited/pending BFS（next(iter(action_ids)) 起点任一）
		visited := map[string]bool{}
		var pending []string
		for actionID := range actionIDs {
			pending = append(pending, actionID)
			break
		}
		for len(pending) > 0 {
			current := pending[len(pending)-1]
			pending = pending[:len(pending)-1]
			if visited[current] {
				continue
			}
			visited[current] = true
			for neighbor := range adjacency[current] {
				if !visited[neighbor] {
					pending = append(pending, neighbor)
				}
			}
		}
		if len(visited) != len(actionIDs) {
			errs = append(errs, fmt.Sprintf("role %s issue %s actions are not one connected "+
				"dependency bundle; split unrelated changes into separate candidates", bundle[0], bundle[1]))
		}
	}
	return errs
}

// validateActionIssueAttribution 手术只绑定其要修复的已诊断问题。
// Python: action_planner.py:220-254 _validate_action_issue_attribution。
func validateActionIssueAttribution(action map[string]any, targetsByRole map[string]*MemberOptimizationTarget) []string {
	// Python: role = ...; target = targets_by_role.get(role); if target is None: return []
	role := pyStr(ActionMapField(action, "role"))
	target := targetsByRole[role]
	if target == nil {
		return []string{}
	}
	// Python: target_issue_ids = {...}
	targetIssueIDs := map[string]bool{}
	for _, issueID := range target.AttributedIssueIDs {
		if issueID != "" {
			targetIssueIDs[issueID] = true
		}
	}
	// Python: raw_issue_ids = action.get(...); if None and len(target)==1: 回落 sorted(target)
	rawIssueIDs := ActionMapField(action, "attributed_issue_ids")
	if rawIssueIDs == nil && len(targetIssueIDs) == 1 {
		var only []string
		for id := range targetIssueIDs {
			only = append(only, id)
		}
		sort.Strings(only)
		action["attributed_issue_ids"] = toAnySlice(only)
		rawIssueIDs = action["attributed_issue_ids"]
	}
	// Python: if not isinstance(raw_issue_ids, list): return ["attributed_issue_ids must be a list"]
	rawList, isList := rawIssueIDs.([]any)
	if rawIssueIDs != nil && !isList {
		return []string{"attributed_issue_ids must be a list"}
	}
	actionIssueIDs := map[string]bool{}
	if isList {
		for _, item := range rawList {
			if text := pyStr(item); text != "" {
				actionIssueIDs[text] = true
			}
		}
	}
	// Python: if target_issue_ids and not action_issue_ids: return [...]
	if len(targetIssueIDs) > 0 && len(actionIssueIDs) == 0 {
		return []string{"attributed_issue_ids must identify the diagnosed issue(s) this action fixes"}
	}
	// Python: unknown_issue_ids = action - target
	var unknown []string
	for id := range actionIssueIDs {
		if !targetIssueIDs[id] {
			unknown = append(unknown, id)
		}
	}
	if len(unknown) > 0 {
		sort.Strings(unknown)
		return []string{fmt.Sprintf("attributed_issue_ids contains issues not assigned to the action role: %v", unknown)}
	}
	// Python: if len(action_issue_ids) > 1: return [...]
	if len(actionIssueIDs) > 1 {
		return []string{"each optimization action must attribute exactly one diagnosed issue; " +
			"separate causal mechanisms require separate actions"}
	}
	// Python: action["attributed_issue_ids"] = sorted(action_issue_ids)
	sortedIDs := setToSortedStrings(actionIssueIDs)
	action["attributed_issue_ids"] = toAnySlice(sortedIDs)
	return []string{}
}

// validateOfferedActionDefinition 拒绝被运行级动作契约移除的动作。
// Python: action_planner.py:257-274 _validate_offered_action_definition。
func validateOfferedActionDefinition(action map[string]any, actionDefinitions []harnessrsi.ActionDefinition) []string {
	// Python: if action_definitions is None: return []
	if actionDefinitions == nil {
		return []string{}
	}
	// Python: offered = {(group, operation) for definition in filter_action_definitions(...)}
	offered := map[[2]string]bool{}
	for _, definition := range actionDefinitions {
		offered[[2]string{strings.TrimSpace(definition.Group), strings.TrimSpace(definition.Operation)}] = true
	}
	// Python: selected = (group, operation)
	selected := [2]string{
		strings.TrimSpace(pyStr(ActionMapField(action, "action_group"))),
		strings.TrimSpace(pyStr(ActionMapField(action, "operation"))),
	}
	if offered[selected] {
		return []string{}
	}
	return []string{fmt.Sprintf("action is not present in the run-specific action contract: %s/%s", selected[0], selected[1])}
}

// validateExistingSurfaceOperation 目标面已存在时拒绝重复 add。
// Python: action_planner.py:277-294 _validate_existing_surface_operation。
func validateExistingSurfaceOperation(action map[string]any, targetsByRole map[string]*MemberOptimizationTarget) []string {
	// Python: if operation != "add": return []
	if strings.ToLower(strings.TrimSpace(pyStr(ActionMapField(action, "operation")))) != "add" {
		return []string{}
	}
	role := pyStr(ActionMapField(action, "role"))
	target := targetsByRole[role]
	targetPath := strings.TrimSpace(pyStr(ActionMapField(action, "target_path")))
	if target == nil || targetPath == "" {
		return []string{}
	}
	// Python: harness_root = Path(target.harness_ref_path).expanduser(); candidate = root / target_path
	harnessRoot := target.HarnessRefPath
	if strings.HasPrefix(harnessRoot, "~") {
		if home, err := os.UserHomeDir(); err == nil {
			harnessRoot = filepath.Join(home, strings.TrimPrefix(harnessRoot, "~"))
		}
	}
	candidate := filepath.Join(harnessRoot, targetPath)
	if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
		return []string{fmt.Sprintf("target_path already exists: %s; use operation=modify instead of adding a duplicate capability", targetPath)}
	}
	if info, err := os.Stat(candidate); err == nil && info.IsDir() {
		return []string{fmt.Sprintf("target_path already exists: %s; use operation=modify instead of adding a duplicate capability", targetPath)}
	}
	return []string{}
}

// validateActionableTargetCoverage 支持面不被静默丢弃。
// Python: action_planner.py:405-428 _validate_actionable_target_coverage。
func validateActionableTargetCoverage(targets []MemberOptimizationTarget, actionsByRole map[string][]map[string]any, actionDefinitions []harnessrsi.ActionDefinition) []string {
	// Python: supported_surfaces = _supported_optimization_surfaces(...); if not: return []
	supportedSurfaces := supportedOptimizationSurfaces(actionDefinitions)
	if len(supportedSurfaces) == 0 {
		return []string{}
	}
	var errs []string
	for i := range targets {
		target := &targets[i]
		// Python: target_surfaces = {normalize(s) for s in target.optimization_surfaces if s in supported}
		targetSurfaces := map[string]bool{}
		for _, surface := range target.OptimizationSurfaces {
			normalized := normalizeOptimizationSurface(surface)
			if normalized != "" && supportedSurfaces[normalized] {
				targetSurfaces[normalized] = true
			}
		}
		// Python: if target_surfaces and not actions_by_role.get(target.role): errors.append(...)
		if len(targetSurfaces) > 0 && len(actionsByRole[target.Role]) == 0 {
			keys := setToSortedStrings(targetSurfaces)
			errs = append(errs, fmt.Sprintf("target %s has actionable optimization_surfaces %v but no executable action",
				target.Role, keys))
		}
	}
	return errs
}

// supportedOptimizationSurfaces 支持的优化面集合。
// Python: action_planner.py:431-445 _supported_optimization_surfaces。
func supportedOptimizationSurfaces(actionDefinitions []harnessrsi.ActionDefinition) map[string]bool {
	// Python: surfaces = set(); for definition in filter_action_definitions(...)
	surfaces := map[string]bool{}
	for _, definition := range actionDefinitions {
		group := strings.TrimSpace(definition.Group)
		switch {
		case group == "prompt":
			// Python: surfaces.update({"prompt", "prompt_section", "identity", "soul"})
			surfaces["prompt"] = true
			surfaces["prompt_section"] = true
			surfaces["identity"] = true
			surfaces["soul"] = true
		case group == "skill" || group == "tool" || group == "rail" || group == "config":
			surfaces[group] = true
		}
	}
	return surfaces
}

// normalizeOptimizationSurface 优化面归一（prompt_file/prompt_section_file → prompt_section）。
// Python: action_planner.py:448-453 _normalize_optimization_surface。
func normalizeOptimizationSurface(surface any) string {
	value := strings.TrimSpace(pyStr(surface))
	if value == "prompt_file" || value == "prompt_section_file" {
		return "prompt_section"
	}
	return value
}

// validatePromptSurfaceSelection 核心提示词文件的 surface 契约。
// Python: action_planner.py:625-674 _validate_prompt_surface_selection。
func validatePromptSurfaceSelection(action map[string]any, targetsByRole map[string]*MemberOptimizationTarget) []string {
	// Python: if action.get("action_group") != "prompt": return []
	if pyStr(ActionMapField(action, "action_group")) != "prompt" {
		return []string{}
	}
	targetPath := normalizePlanPath(pyStr(ActionMapField(action, "target_path")))
	// Python: if target_path not in _PROMPT_CORE_PATHS: return []
	if !promptCorePaths[targetPath] {
		return []string{}
	}
	// Python: constraints = action.get("constraints") if isinstance dict else {}
	constraints := map[string]any{}
	if m, ok := ActionMapField(action, "constraints").(map[string]any); ok {
		constraints = m
	}
	surfaceScope := strings.TrimSpace(pyStr(constraints["surface_scope"]))
	// Python: identity.md 分支
	if targetPath == "identity.md" {
		if !roleIdentityScopes[surfaceScope] {
			return []string{"identity.md can only be used for role identity or " +
				"duty-boundary changes; set constraints.surface_scope to " +
				"`role_identity` or `duty_boundary`, otherwise use a " +
				"prompt section"}
		}
		return []string{}
	}
	// Python: soul.md 分支
	if targetPath == "soul.md" {
		if !soulScopes[surfaceScope] {
			return []string{"soul.md can only be used for durable operating principles; " +
				"set constraints.surface_scope to " +
				"`durable_operating_principle`, otherwise use a prompt section"}
		}
		role := pyStr(ActionMapField(action, "role"))
		target := targetsByRole[role]
		// Python: mechanism_types = set(target.mechanism_types if target else [])
		mechanismTypes := map[string]bool{}
		if target != nil {
			for _, mt := range target.MechanismTypes {
				mechanismTypes[mt] = true
			}
		}
		// Python: if mechanism_types & {"workflow", "instruction"}: 程序化术语检测
		if mechanismTypes["workflow"] || mechanismTypes["instruction"] {
			var fields []string
			for _, field := range []string{"description", "rationale", "expected_effect"} {
				fields = append(fields, pyStr(ActionMapField(action, field)))
			}
			rationale := strings.ToLower(strings.Join(fields, " "))
			for _, term := range soulProceduralTerms {
				if strings.Contains(rationale, term) {
					return []string{"specific workflows, checklists, verification procedures, " +
						"and task recovery routines must use " +
						"prompt_sections/files/*.md plus " +
						"prompt_sections/sections.yaml"}
				}
			}
		}
		return []string{}
	}
	return []string{}
}

// validateOptimizationSurfaceAlignment 手术必须落在已诊断优化面上。
// Python: action_planner.py:677-698 _validate_optimization_surface_alignment。
func validateOptimizationSurfaceAlignment(action map[string]any, targetsByRole map[string]*MemberOptimizationTarget) []string {
	role := pyStr(ActionMapField(action, "role"))
	target := targetsByRole[role]
	if target == nil || len(target.OptimizationSurfaces) == 0 {
		return []string{}
	}
	// Python: allowed_surfaces = {normalize(s) for s in ... if str(s).strip()}
	allowedSurfaces := map[string]bool{}
	for _, surface := range target.OptimizationSurfaces {
		if strings.TrimSpace(surface) != "" {
			allowedSurfaces[normalizeOptimizationSurface(surface)] = true
		}
	}
	if len(allowedSurfaces) == 0 {
		return []string{}
	}
	// Python: actual_surface = _action_optimization_surface(action); if in allowed: return []
	actualSurface := actionOptimizationSurface(action)
	if allowedSurfaces[actualSurface] {
		return []string{}
	}
	// Python: return [f"action surface does not match ..."]
	keys := setToSortedStrings(allowedSurfaces)
	return []string{fmt.Sprintf("action surface does not match diagnosed optimization_surface: "+
		"actual=%s, expected_one_of=%v", orUnknown(actualSurface), keys)}
}

// actionOptimizationSurface 从动作推导实际优化面。
// Python: action_planner.py:701-713 _action_optimization_surface。
func actionOptimizationSurface(action map[string]any) string {
	actionGroup := pyStr(ActionMapField(action, "action_group"))
	targetPath := normalizePlanPath(pyStr(ActionMapField(action, "target_path")))
	// Python: if action_group in {"skill", "tool", "rail", "config"}: return action_group
	if actionGroup == "skill" || actionGroup == "tool" || actionGroup == "rail" || actionGroup == "config" {
		return actionGroup
	}
	// Python: if action_group != "prompt": return ""
	if actionGroup != "prompt" {
		return ""
	}
	if targetPath == "identity.md" {
		return "identity"
	}
	if targetPath == "soul.md" {
		return "soul"
	}
	// Python: if target_path.startswith("prompt_sections/files/"): return "prompt_section"
	if strings.HasPrefix(targetPath, "prompt_sections/files/") {
		return "prompt_section"
	}
	return "prompt_section"
}

// validateAllowedPromptSurfaces 受限 prompt 面校验。
// Python: action_planner.py:716-731 _validate_allowed_prompt_surfaces。
func validateAllowedPromptSurfaces(planData map[string]any, allowedPromptSurfaces map[string]bool) []string {
	if len(allowedPromptSurfaces) == 0 {
		return []string{}
	}
	var errs []string
	for _, action := range planActionsList(planData) {
		if pyStr(ActionMapField(action, "action_group")) != "prompt" {
			continue
		}
		actualSurface := actionOptimizationSurface(action)
		if !allowedPromptSurfaces[actualSurface] {
			keys := setToSortedStrings(allowedPromptSurfaces)
			errs = append(errs, fmt.Sprintf("restricted prompt optimization allows only %v, got %s for action %s",
				keys, orUnknown(actualSurface), pyStr(ActionMapField(action, "action_id"))))
		}
	}
	return errs
}

// normalizePlanPath 方案路径归一。
// Python: action_planner.py:734-735 _normalize_plan_path。
func normalizePlanPath(path string) string {
	return strings.Trim(filepath.ToSlash(filepath.Clean(strings.ReplaceAll(path, "\\", "/"))), "/")
}

// orUnknown 空串回落 unknown（Python f"{actual or 'unknown'}"）。
func orUnknown(value string) string {
	if value == "" {
		return "unknown"
	}
	return value
}

// setToSortedStrings 集合 → 排序列表。
func setToSortedStrings(set map[string]bool) []string {
	out := make([]string, 0, len(set))
	for key := range set {
		out = append(out, key)
	}
	sort.Strings(out)
	return out
}

// summarizeEvidenceItems 证据条目摘要（前 4 条四字段优先回落全键）。
// Python: action_planner.py:738-752 _summarize_evidence_items。
func summarizeEvidenceItems(items []map[string]any, limit int) string {
	if limit <= 0 {
		limit = 4
	}
	var parts []string
	for _, item := range items[:intMin(len(items), limit)] {
		var fields []string
		for _, key := range []string{"summary", "recommendation", "candidate_query", "failure_mode"} {
			value := item[key]
			if value != nil && fmt.Sprintf("%v", value) != "" {
				fields = append(fields, fmt.Sprintf("%s=%v", key, value))
			}
		}
		if len(fields) == 0 {
			// Python: fields.append(", ".join(f"{k}={v}" for k, v in item.items() if k and v not in (None, "")))
			keys := make([]string, 0, len(item))
			for key := range item {
				keys = append(keys, key)
			}
			sort.Strings(keys)
			var all []string
			for _, key := range keys {
				value := item[key]
				if key != "" && value != nil && fmt.Sprintf("%v", value) != "" {
					all = append(all, fmt.Sprintf("%s=%v", key, value))
				}
			}
			fields = append(fields, strings.Join(all, ", "))
		}
		summary := strings.Join(fields, "; ")
		if summary != "" {
			parts = append(parts, summary)
		}
	}
	return strings.Join(parts, " | ")
}

// bindImmutableHypotheses 校验后附加紧凑源契约。
// Python: action_planner.py:752-821 _bind_immutable_hypotheses。
func bindImmutableHypotheses(planData map[string]any, hypotheses []map[string]any) {
	// Python: if not hypotheses: return
	if len(hypotheses) == 0 {
		return
	}
	// Python: by_issue = {source_issue_id: item}
	byIssue := map[string]map[string]any{}
	for _, item := range hypotheses {
		if issueID := pyStr(item["source_issue_id"]); issueID != "" {
			byIssue[issueID] = item
		}
	}
	// Python: selected_hypothesis_ids = []
	var selectedHypothesisIDs []string
	seenHypothesisIDs := map[string]bool{}
	for _, action := range planActionsList(planData) {
		// Python: selected = [by_issue[issue_id] for issue_id in action.attributed_issue_ids if in by_issue]
		var selected []map[string]any
		if list, ok := ActionMapField(action, "attributed_issue_ids").([]any); ok {
			for _, item := range list {
				issueID := pyStr(item)
				if h, inByIssue := byIssue[issueID]; inByIssue {
					selected = append(selected, h)
				}
			}
		}
		if len(selected) == 0 {
			continue
		}
		// Python: contracts = []; for item in selected: contract = {...八字段...}
		var contracts []any
		for _, item := range selected {
			contract := map[string]any{
				"hypothesis_id":      pyStrOrEmpty(item["hypothesis_id"]),
				"content_sha256":     pyStrOrEmpty(item["content_sha256"]),
				"source_issue_id":    pyStrOrEmpty(item["source_issue_id"]),
				"required_behavior":  pyStrOrEmpty(item["required_behavior"]),
				"forbidden_behavior": pyOrNone(item["forbidden_behavior"]),
				"public_trigger":     pyOrNone(item["public_trigger"]),
				"decisive_probe":     pyOrNone(item["decisive_probe"]),
			}
			// Python: if isinstance(item.get("decision_contract"), dict) and ...: contract["decision_contract"] = ...
			if dc, ok := item["decision_contract"].(map[string]any); ok && len(dc) > 0 {
				contract["decision_contract"] = dc
			}
			// Python: if isinstance(item.get("lever_policy"), dict) and ...: contract.update({...三项...})
			if lp, ok := item["lever_policy"].(map[string]any); ok && len(lp) > 0 {
				contract["diagnostic_lens"] = pyStrOrEmpty(item["diagnostic_lens"])
				if contract["diagnostic_lens"] == "" {
					contract["diagnostic_lens"] = "failure"
				}
				contract["intent"] = pyStrOrEmpty(item["intent"])
				if contract["intent"] == "" {
					contract["intent"] = "corrective"
				}
				contract["lever_policy"] = lp
			}
			contracts = append(contracts, contract)
		}
		// Python: constraints = dict(action.get("constraints") or {}); constraints["optimization_contracts"] = contracts
		constraints := map[string]any{}
		if m, ok := ActionMapField(action, "constraints").(map[string]any); ok {
			constraints = m
		}
		constraints["optimization_contracts"] = contracts
		// Python: policies = [dict(item["lever_policy"]) for item in selected if dict and non-empty]
		var policies []map[string]any
		for _, item := range selected {
			if lp, ok := item["lever_policy"].(map[string]any); ok && len(lp) > 0 {
				policies = append(policies, lp)
			}
		}
		// Python: lever_decision = build_action_lever_decision(action_group=..., selected_surface=..., policies=...)
		leverDecision := BuildActionLeverDecision(
			pyStrOrEmpty(ActionMapField(action, "action_group")),
			actionOptimizationSurface(action),
			policies,
		)
		// Python: if policies: constraints["lever_decision"] = lever_decision
		if len(policies) > 0 {
			constraints["lever_decision"] = leverDecision
		}
		action["constraints"] = constraints
		// Python: action["expected_effect"] = "\n".join(str(item["required_behavior"]) for item in selected if ...)
		var behaviors []string
		for _, item := range selected {
			if text := pyStr(item["required_behavior"]); text != "" {
				behaviors = append(behaviors, text)
			}
		}
		action["expected_effect"] = strings.Join(behaviors, "\n")
		// Python: selected_hypothesis_ids.extend(... 保序去重)
		for _, item := range selected {
			if id := pyStr(item["hypothesis_id"]); id != "" && !seenHypothesisIDs[id] {
				seenHypothesisIDs[id] = true
				selectedHypothesisIDs = append(selectedHypothesisIDs, id)
			}
		}
	}
	// Python: metadata = plan_data.setdefault("metadata", {}); if dict: metadata[...] = list(dict.fromkeys(...))
	metadata, _ := planData["metadata"].(map[string]any)
	if metadata == nil {
		metadata = map[string]any{}
		planData["metadata"] = metadata
	}
	metadata["optimization_hypothesis_ids"] = toAnySlice(selectedHypothesisIDs)
	metadata["semantic_authority"] = "immutable_optimization_hypotheses"
}

// annotateActionBundles 持久化束级问题与所需行为。
// Python: action_planner.py:824-859 _annotate_action_bundles。
func annotateActionBundles(planData map[string]any, hypotheses []map[string]any) {
	// Python: required_behavior_by_issue = {...}
	requiredBehaviorByIssue := map[string]string{}
	for _, item := range hypotheses {
		if issueID := pyStr(item["source_issue_id"]); issueID != "" {
			requiredBehaviorByIssue[issueID] = pyStrOrEmpty(item["required_behavior"])
		}
	}
	// Python: grouped: dict[tuple[role, issue_id], list[action_id]]
	grouped := map[[2]string][]string{}
	groupKeys := [][2]string{}
	for _, action := range planActionsList(planData) {
		issueIDs := []string{}
		if list, ok := ActionMapField(action, "attributed_issue_ids").([]any); ok {
			for _, item := range list {
				if text := pyStr(item); text != "" {
					issueIDs = append(issueIDs, text)
				}
			}
		}
		if len(issueIDs) != 1 {
			continue
		}
		key := [2]string{pyStrOrEmpty(ActionMapField(action, "role")), issueIDs[0]}
		if _, exists := grouped[key]; !exists {
			groupKeys = append(groupKeys, key)
		}
		grouped[key] = append(grouped[key], pyStrOrEmpty(ActionMapField(action, "action_id")))
	}
	metadata, _ := planData["metadata"].(map[string]any)
	if metadata == nil {
		metadata = map[string]any{}
		planData["metadata"] = metadata
	}
	// Python: metadata["action_bundles"] = [{...} for (role, issue_id), action_ids in sorted(grouped.items())]
	sort.Slice(groupKeys, func(i, j int) bool {
		if groupKeys[i][0] != groupKeys[j][0] {
			return groupKeys[i][0] < groupKeys[j][0]
		}
		return groupKeys[i][1] < groupKeys[j][1]
	})
	bundles := []any{}
	for _, key := range groupKeys {
		bundles = append(bundles, map[string]any{
			"bundle_id":         key[0] + ":" + key[1],
			"role":              key[0],
			"issue_id":          key[1],
			"required_behavior": requiredBehaviorByIssue[key[1]],
			"action_ids":        toAnySlice(grouped[key]),
			"atomic":            true,
		})
	}
	metadata["action_bundles"] = bundles
}

// buildActionContractText 渲染运行级动作策略（系统提示词之外）。
// Python: action_planner.py:862-884 _build_action_contract_text。
func buildActionContractText(actionDefinitions []harnessrsi.ActionDefinition) string {
	// Python: parts = [action_policy_prompt().strip()]
	parts := []string{strings.TrimSpace(actiongroups.ActionPolicyPrompt())}
	// Python: filtered_definitions = filter_action_definitions(...)
	filteredDefinitions := actionDefinitions
	// Python: strict_lines = [...三行...]
	strictLines := []string{
		"## Run-Specific Strict Allowlist",
		"Only the exact action_group/operation pairs listed under Action Definitions are allowed.",
		"Globally documented actions that are absent from Action Definitions are disabled for this run.",
	}
	// Python: if not any(skill/search): append disabled
	hasSearch := false
	for _, definition := range filteredDefinitions {
		if definition.Group == "skill" && definition.Operation == "search" {
			hasSearch = true
			break
		}
	}
	if !hasSearch {
		strictLines = append(strictLines, "skill/search is disabled for this run; use an offered local skill action instead.")
	}
	parts = append(parts, strings.Join(strictLines, "\n"))
	// Python: if filtered_definitions: lines = [...]; else: "## Action Definitions\n(none)"
	if len(filteredDefinitions) > 0 {
		lines := []string{"## Action Definitions"}
		for _, definition := range filteredDefinitions {
			lines = append(lines, fmt.Sprintf("- %s/%s: %s (function=%s)",
				definition.Group, definition.Operation, definition.Purpose, definition.Function))
		}
		parts = append(parts, strings.Join(lines, "\n"))
	} else {
		parts = append(parts, "## Action Definitions\n(none)")
	}
	// Python: return "\n\n".join(part for part in parts if part)
	var nonEmpty []string
	for _, part := range parts {
		if part != "" {
			nonEmpty = append(nonEmpty, part)
		}
	}
	return strings.Join(nonEmpty, "\n\n")
}

// attachImprovementBriefs 附加 analyzer 改进简报到 prompt 手术。
// Python: action_planner.py:887-931 _attach_improvement_briefs。
func attachImprovementBriefs(planData map[string]any, targets []MemberOptimizationTarget, roleAttributionReport *RoleAttributionReport) {
	// Python: brief_by_issue = {}; for issue in assigned_role_issues: for evidence: brief = evidence.get("improvement_brief")
	briefByIssue := map[string]map[string]any{}
	for _, issue := range roleAttributionReport.AssignedRoleIssues {
		for _, evidence := range issue.Evidence {
			brief, ok := evidence["improvement_brief"].(map[string]any)
			if ok && len(brief) > 0 {
				briefByIssue[issue.IssueID] = brief
				break
			}
		}
	}
	// Python: if not brief_by_issue: return
	if len(briefByIssue) == 0 {
		return
	}
	// Python: issue_ids_by_role = {target.role: set(target.attributed_issue_ids) for target in targets}
	issueIDsByRole := map[string]map[string]bool{}
	for i := range targets {
		issueIDsByRole[targets[i].Role] = map[string]bool{}
		for _, id := range targets[i].AttributedIssueIDs {
			issueIDsByRole[targets[i].Role][id] = true
		}
	}
	for _, action := range planActionsList(planData) {
		// Python: if action.get("action_group") != "prompt": continue
		if pyStr(ActionMapField(action, "action_group")) != "prompt" {
			continue
		}
		// Python: if isinstance(constraints, dict) and constraints: continue
		if constraints, ok := ActionMapField(action, "constraints").(map[string]any); ok && len(constraints) > 0 {
			continue
		}
		role := pyStr(ActionMapField(action, "role"))
		var attributedIssueIDs []string
		if list, ok := ActionMapField(action, "attributed_issue_ids").([]any); ok {
			for _, item := range list {
				if text := pyStr(item); text != "" {
					attributedIssueIDs = append(attributedIssueIDs, text)
				}
			}
		}
		// Python: candidate_issue_ids = attributed if attributed else sorted(issue_ids_by_role.get(role, set()))
		candidateIssueIDs := attributedIssueIDs
		if len(candidateIssueIDs) == 0 {
			candidateIssueIDs = setToSortedStrings(issueIDsByRole[role])
		}
		for _, issueID := range candidateIssueIDs {
			if brief, ok := briefByIssue[issueID]; ok {
				action["constraints"] = brief
				break
			}
		}
	}
}

// filterActionablePlanningInputs 剔除明确证据不足的问题。
// Python: action_planner.py:934-1060 _filter_actionable_planning_inputs。
func filterActionablePlanningInputs(targets []MemberOptimizationTarget, roleAttributionReport *RoleAttributionReport, mechanismReport *MechanismAttributionReport) ([]MemberOptimizationTarget, *RoleAttributionReport, *MechanismAttributionReport, []string) {
	// Python: inactionable_issue_ids = {}; for mechanisms in role_mechanisms.values(): ...
	inactionableIssueIDs := map[string]bool{}
	for _, mechanisms := range mechanismReport.RoleMechanisms {
		byIssue := map[string][]RoleMechanismAttribution{}
		for _, mechanism := range mechanisms {
			byIssue[mechanism.IssueID] = append(byIssue[mechanism.IssueID], mechanism)
		}
		for issueID, issueMechanisms := range byIssue {
			if len(issueMechanisms) > 0 {
				allInactionable := true
				for _, m := range issueMechanisms {
					if !isInactionableMechanism(m) {
						allInactionable = false
						break
					}
				}
				if allInactionable {
					inactionableIssueIDs[issueID] = true
				}
			}
		}
	}
	// Python: if not inactionable_issue_ids: return (targets, reports, [])
	if len(inactionableIssueIDs) == 0 {
		return targets, roleAttributionReport, mechanismReport, []string{}
	}
	// Python: actionable_surfaces_by_role
	actionableSurfacesByRole := map[string][]string{}
	for role, mechanisms := range mechanismReport.RoleMechanisms {
		var surfaces []string
		seen := map[string]bool{}
		for _, mechanism := range mechanisms {
			if inactionableIssueIDs[mechanism.IssueID] {
				continue
			}
			surface := strings.TrimSpace(mechanism.OptimizationSurface)
			if surface != "" && !seen[surface] {
				surfaces = append(surfaces, surface)
				seen[surface] = true
			}
		}
		if len(surfaces) > 0 {
			actionableSurfacesByRole[role] = surfaces
		}
	}
	// Python: for target in targets: 剔除后构建 actionable_targets
	var actionableTargets []MemberOptimizationTarget
	actionableRoles := map[string]bool{}
	for i := range targets {
		target := targets[i]
		var issueIDs []string
		for _, issueID := range target.AttributedIssueIDs {
			if !inactionableIssueIDs[issueID] {
				issueIDs = append(issueIDs, issueID)
			}
		}
		// Python: if target.attributed_issue_ids and not issue_ids: continue
		if len(target.AttributedIssueIDs) > 0 && len(issueIDs) == 0 {
			continue
		}
		// Python: mechanism_types = [mt for mt in target.mechanism_types if mt != _INACTIONABLE]
		var mechanismTypes []string
		for _, mt := range target.MechanismTypes {
			if mt != inactionableMechanism {
				mechanismTypes = append(mechanismTypes, mt)
			}
		}
		// Python: replace(target, attributed_issue_ids=..., mechanism_types=..., optimization_surfaces=...)
		surfaces := target.OptimizationSurfaces
		if s, ok := actionableSurfacesByRole[target.Role]; ok {
			surfaces = s
		}
		actionableTargets = append(actionableTargets, MemberOptimizationTarget{
			Role:                 target.Role,
			HarnessRefPath:       target.HarnessRefPath,
			AttributedIssueIDs:   issueIDs,
			Confidence:           target.Confidence,
			Reason:               target.Reason,
			EvidenceRefs:         target.EvidenceRefs,
			MechanismTypes:       mechanismTypes,
			OptimizationSurfaces: surfaces,
			MemberName:           target.MemberName,
			Metadata:             target.Metadata,
		})
		actionableRoles[target.Role] = true
	}
	// Python: actionable_role_report = replace(role_attribution_report, assigned_role_issues=[...])
	var keptIssues []RoleIssueAttribution
	for _, issue := range roleAttributionReport.AssignedRoleIssues {
		if actionableRoles[issue.Role] && !inactionableIssueIDs[issue.IssueID] {
			keptIssues = append(keptIssues, issue)
		}
	}
	actionableRoleReport := &RoleAttributionReport{
		AttributionID:            roleAttributionReport.AttributionID,
		SourceEvalRefPath:        roleAttributionReport.SourceEvalRefPath,
		SourceAnalysisResultPath: roleAttributionReport.SourceAnalysisResultPath,
		HarnessRefsPath:          roleAttributionReport.HarnessRefsPath,
		CandidateRoles:           roleAttributionReport.CandidateRoles,
		AssignedRoleIssues:       keptIssues,
		UnassignedIssues:         roleAttributionReport.UnassignedIssues,
		Warnings:                 roleAttributionReport.Warnings,
		Metadata:                 roleAttributionReport.Metadata,
	}
	// Python: actionable_mechanisms / actionable_mechanism_report
	actionableMechanisms := map[string][]RoleMechanismAttribution{}
	for role, mechanisms := range mechanismReport.RoleMechanisms {
		if !actionableRoles[role] {
			continue
		}
		var kept []RoleMechanismAttribution
		for _, mechanism := range mechanisms {
			if !inactionableIssueIDs[mechanism.IssueID] && !isInactionableMechanism(mechanism) {
				kept = append(kept, mechanism)
			}
		}
		if len(kept) > 0 {
			actionableMechanisms[role] = kept
		}
	}
	actionableMechanismReport := &MechanismAttributionReport{
		MechanismAttributionID:    mechanismReport.MechanismAttributionID,
		SourceRoleAttributionPath: mechanismReport.SourceRoleAttributionPath,
		RoleMechanisms:            actionableMechanisms,
		SkippedRoles:              mechanismReport.SkippedRoles,
		Warnings:                  mechanismReport.Warnings,
		Metadata:                  mechanismReport.Metadata,
	}
	// Python: sorted(inactionable_issue_ids)
	inactionable := setToSortedStrings(inactionableIssueIDs)
	return actionableTargets, actionableRoleReport, actionableMechanismReport, inactionable
}

// isInactionableMechanism 机制类型或失败签名为证据不足。
// Python: action_planner.py:1063-1065 _is_inactionable_mechanism。
func isInactionableMechanism(mechanism RoleMechanismAttribution) bool {
	return mechanism.MechanismType == inactionableMechanism || mechanism.FailureSignature == inactionableMechanism
}

// compactExperimentForPlanner 保留因果候选反馈，排除臃肿产物负载。
// Python: action_planner.py:1068-1083 _compact_experiment_for_planner。
func compactExperimentForPlanner(record map[string]any) map[string]any {
	// Python: experiment_fields 元组; return {key: record.get(key) for key in fields if key in record}
	experimentFields := []string{
		"experiment_id", "surface", "lever", "target_case_ids", "status",
		"reason", "failure_class", "outcome", "verifier_deltas_by_case",
		"candidate_failure_diagnoses", "epoch_checkpoint",
	}
	out := map[string]any{}
	for _, key := range experimentFields {
		if value, exists := record[key]; exists {
			out[key] = value
		}
	}
	return out
}

// adaptPromptSurfaceWithinInstructionLever 不改杠杆收窄 prompt 面。
// Python: action_planner.py:1086-1133 _adapt_prompt_surface_within_instruction_lever。
func adaptPromptSurfaceWithinInstructionLever(targets []MemberOptimizationTarget, mechanismReport *MechanismAttributionReport, allowedPromptSurfaces map[string]bool) ([]MemberOptimizationTarget, *MechanismAttributionReport) {
	// Python: if not allowed_prompt_surfaces: return
	if len(allowedPromptSurfaces) == 0 {
		return targets, mechanismReport
	}
	// Python: prompt_surfaces = {"identity", "soul", "prompt_section"}
	promptSurfaces := map[string]bool{"identity": true, "soul": true, "prompt_section": true}
	// Python: preferred_surface = next((s for s in ("prompt_section", "identity", "soul") if s in allowed), "")
	preferredSurface := ""
	for _, candidate := range []string{"prompt_section", "identity", "soul"} {
		if allowedPromptSurfaces[candidate] {
			preferredSurface = candidate
			break
		}
	}
	if preferredSurface == "" {
		return targets, mechanismReport
	}
	// Python: adapted_issue_ids = set(); adapted_targets = []
	adaptedIssueIDs := map[string]bool{}
	var adaptedTargets []MemberOptimizationTarget
	for i := range targets {
		target := targets[i]
		// Python: normalized = {...}; if normalized and normalized <= prompt_surfaces and not normalized & allowed
		normalized := map[string]bool{}
		for _, surface := range target.OptimizationSurfaces {
			if strings.TrimSpace(surface) != "" {
				normalized[normalizeOptimizationSurface(surface)] = true
			}
		}
		withinPrompt := len(normalized) > 0
		allInPrompt := true
		noOverlap := true
		for surface := range normalized {
			if !promptSurfaces[surface] {
				allInPrompt = false
			}
			if allowedPromptSurfaces[surface] {
				noOverlap = false
			}
		}
		if withinPrompt && allInPrompt && noOverlap {
			// Python: metadata["within_lever_surface_adaptation"] = {...四字段...}
			metadata := map[string]any{}
			for key, value := range target.Metadata {
				metadata[key] = value
			}
			sourceSurfaces := setToSortedStrings(normalized)
			metadata["within_lever_surface_adaptation"] = map[string]any{
				"lever":           "instruction",
				"source_surfaces": toAnySlice(sourceSurfaces),
				"adapted_surface": preferredSurface,
				"reason":          "restricted_prompt_delivery_surface",
			}
			adaptedTargets = append(adaptedTargets, MemberOptimizationTarget{
				Role:                 target.Role,
				HarnessRefPath:       target.HarnessRefPath,
				AttributedIssueIDs:   target.AttributedIssueIDs,
				Confidence:           target.Confidence,
				Reason:               target.Reason,
				EvidenceRefs:         target.EvidenceRefs,
				MechanismTypes:       target.MechanismTypes,
				OptimizationSurfaces: []string{preferredSurface},
				MemberName:           target.MemberName,
				Metadata:             metadata,
			})
			for _, id := range target.AttributedIssueIDs {
				adaptedIssueIDs[id] = true
			}
		} else {
			adaptedTargets = append(adaptedTargets, target)
		}
	}
	// Python: if not adapted_issue_ids: return targets, mechanism_report
	if len(adaptedIssueIDs) == 0 {
		return targets, mechanismReport
	}
	// Python: role_mechanisms = {role: [replace(mechanism, optimization_surface=preferred) ...]}
	roleMechanisms := map[string][]RoleMechanismAttribution{}
	for role, mechanisms := range mechanismReport.RoleMechanisms {
		var adapted []RoleMechanismAttribution
		for _, mechanism := range mechanisms {
			if adaptedIssueIDs[mechanism.IssueID] {
				mechanism.OptimizationSurface = preferredSurface
			}
			adapted = append(adapted, mechanism)
		}
		roleMechanisms[role] = adapted
	}
	return adaptedTargets, &MechanismAttributionReport{
		MechanismAttributionID:    mechanismReport.MechanismAttributionID,
		SourceRoleAttributionPath: mechanismReport.SourceRoleAttributionPath,
		RoleMechanisms:            roleMechanisms,
		SkippedRoles:              mechanismReport.SkippedRoles,
		Warnings:                  mechanismReport.Warnings,
		Metadata:                  mechanismReport.Metadata,
	}
}

// buildImproverPolicyContext 冻结改进器策略上下文。
// Python: action_planner.py:1669-1699 _build_improver_policy_context。
func buildImproverPolicyContext(optimizationExperience map[string]any) string {
	// Python: if not isinstance(optimization_experience, dict): return ""
	if optimizationExperience == nil {
		return ""
	}
	// Python: raw_policy = optimization_experience.get("improver_policy"); if not dict or no version_id: return ""
	rawPolicy, ok := optimizationExperience["improver_policy"].(map[string]any)
	if !ok || pyStrOrEmpty(rawPolicy["version_id"]) == "" {
		return ""
	}
	// Python: policy = {...四字段（dict 回落空）...}
	policy := map[string]any{
		"version_id":    pyStrOrEmpty(rawPolicy["version_id"]),
		"policy_digest": pyStrOrEmpty(rawPolicy["policy_digest"]),
	}
	if m, ok := rawPolicy["generation_directives"].(map[string]any); ok {
		policy["generation_directives"] = m
	} else {
		policy["generation_directives"] = map[string]any{}
	}
	if m, ok := rawPolicy["budget_policy"].(map[string]any); ok {
		policy["budget_policy"] = m
	} else {
		policy["budget_policy"] = map[string]any{}
	}
	// Python: f-string 模板（json.dumps indent=2）
	policyJSON, _ := json.MarshalIndent(policy, "", "  ")
	return "\n## Frozen Improver Policy\n\n" + string(policyJSON) + "\n\n" +
		"This is the versioned pre-execution policy for the current Improver. It is not\n" +
		"sibling execution feedback and contains no result for the current cohort.\n" +
		"Apply its generation directives within the frozen failure facts, behavioral goal,\n" +
		"run-specific action contract, and available Harness surfaces. The diagnosed lever\n" +
		"is advisory, not a component restriction. An activation-evidence\n" +
		"directive requires concrete current execution or Harness-path evidence before\n" +
		"choosing that surface. Do not invent evidence merely to satisfy the policy.\n"
}

// buildSiblingGenerationContext 同胞候选生成上下文（仅执行前静态方案）。
// Python: action_planner.py:1702-1746 _build_sibling_generation_context。
func buildSiblingGenerationContext(optimizationExperience map[string]any) string {
	// Python: sibling_generation = _sibling_generation(...)
	siblingGeneration := siblingGeneration(optimizationExperience)
	// Python: candidate_id = str(...).strip(); if not: return ""
	candidateID := strings.TrimSpace(pyStr(siblingGeneration["candidate_id"]))
	if candidateID == "" {
		return ""
	}
	// Python: prior_proposals = ...; compact_proposals = [...]
	var compactProposals []any
	if priorProposals, ok := siblingGeneration["prior_proposals"].([]any); ok {
		for _, proposal := range priorProposals {
			if m, isMap := proposal.(map[string]any); isMap {
				if compact := compactSiblingProposal(m); len(compact) > 0 {
					compactProposals = append(compactProposals, compact)
				}
			}
		}
	}
	// Python: generation_position = {...三字段...}
	generationIndex := siblingGeneration["generation_index"]
	if generationIndex == nil {
		generationIndex = siblingGeneration["candidate_index"]
	}
	generationPosition := map[string]any{
		"candidate_id":     candidateID,
		"generation_index": pyOrNone(generationIndex),
		"candidate_count":  pyOrNone(siblingGeneration["candidate_count"]),
	}
	positionJSON, _ := json.MarshalIndent(generationPosition, "", "  ")
	proposalsJSON, _ := json.MarshalIndent(compactProposals, "", "  ")
	return "\n## Sibling Candidate Generation (Pre-Execution Plans Only)\n\n" +
		"Current candidate position:\n" + string(positionJSON) + "\n\n" +
		"Prior sibling proposal summaries:\n" + string(proposalsJSON) + "\n\n" +
		"This section contains static plans produced before candidate execution. It is\n" +
		"not execution feedback, rejection history, verifier evidence, a score outcome,\n" +
		"or evidence that any sibling works. `generation_index` is only a generation\n" +
		"slot; it is not a quality prediction. Predicted ranks are computed and frozen\n" +
		"only after every sibling proposal exists.\n\n" +
		"Generate the proposal assigned to the current generation slot. Make it\n" +
		"materially different from the prior proposal summaries in its intervention,\n" +
		"not merely in action IDs, wording, or file names. Preserve the frozen failure\n" +
		"facts and behavioral goal. Do not change components merely to manufacture\n" +
		"diversity. If no evidence-backed materially distinct proposal exists, keep the\n" +
		"evidence contract even if the resulting proposal is a duplicate; the cohort\n" +
		"ranker will identify that duplicate without pretending it is a new strategy.\n"
}

// compactSiblingProposal 静态键压缩同胞提案。
// Python: action_planner.py:1749-1793 _compact_sibling_proposal。
func compactSiblingProposal(proposal map[string]any) map[string]any {
	// Python: static_keys 元组
	staticKeys := []string{
		"candidate_id", "index", "candidate_index", "generation_order", "plan_id",
		"summary", "proposal_summary", "intervention_summary", "intervention_strategy",
		"diversity_signature", "candidate_fingerprint", "role", "action_group",
		"operation", "action_type", "target_path", "runtime_name", "description",
		"rationale", "expected_effect", "selected_lever", "selected_surface",
		"action_groups", "operations", "target_paths", "hypothesis_ids",
		"source_issue_ids", "target_case_ids", "action_count",
	}
	// Python: compact = {key: proposal[key] for key in static_keys if key in proposal and proposal[key] not in (None, "", [], {})}
	compact := map[string]any{}
	for _, key := range staticKeys {
		if value, exists := proposal[key]; exists && !isEmptyPyValue(value) {
			compact[key] = value
		}
	}
	// Python: raw_actions = proposal.get("actions", []); actions = [_compact_sibling_action(...)]
	if rawActions, ok := proposal["actions"].([]any); ok {
		var actions []any
		for _, action := range rawActions {
			if m, isMap := action.(map[string]any); isMap {
				if compactAction := compactSiblingAction(m); len(compactAction) > 0 {
					actions = append(actions, compactAction)
				}
			}
		}
		if len(actions) > 0 {
			compact["actions"] = actions
		}
	}
	// Python: raw_capabilities 同构
	if rawCapabilities, ok := proposal["capabilities"].([]any); ok {
		var capabilities []any
		for _, capability := range rawCapabilities {
			if m, isMap := capability.(map[string]any); isMap {
				if compactAction := compactSiblingAction(m); len(compactAction) > 0 {
					capabilities = append(capabilities, compactAction)
				}
			}
		}
		if len(capabilities) > 0 {
			compact["capabilities"] = capabilities
		}
	}
	return compact
}

// compactSiblingAction 静态键压缩同胞动作。
// Python: action_planner.py:1808-1846 _compact_sibling_action。
func compactSiblingAction(action map[string]any) map[string]any {
	// Python: static_keys 元组
	staticKeys := []string{
		"role", "action_group", "operation", "action_type", "target_path",
		"runtime_name", "description", "rationale", "expected_effect",
		"attributed_issue_ids", "declared_write_paths", "optimization_hypothesis_ids",
		"target_case_ids",
	}
	compact := map[string]any{}
	for _, key := range staticKeys {
		if value, exists := action[key]; exists && !isEmptyPyValue(value) {
			compact[key] = value
		}
	}
	// Python: lever_decision 回落链
	leverDecision, ok := action["lever_decision"].(map[string]any)
	if !ok {
		if constraints, isMap := action["constraints"].(map[string]any); isMap {
			leverDecision, _ = constraints["lever_decision"].(map[string]any)
		}
	}
	// Python: compact_lever = {...四键过滤...}
	if leverDecision != nil {
		compactLever := map[string]any{}
		for _, key := range []string{"selected_lever", "selected_surface", "recommended_levers", "predicted_affected_case_ids"} {
			if value, exists := leverDecision[key]; exists && !isEmptyPyValue(value) {
				compactLever[key] = value
			}
		}
		if len(compactLever) > 0 {
			compact["lever_decision"] = compactLever
		}
	}
	return compact
}

// isEmptyPyValue 等价 Python value not in (None, "", [], {})。
func isEmptyPyValue(value any) bool {
	switch v := value.(type) {
	case nil:
		return true
	case string:
		return v == ""
	case []any:
		return len(v) == 0
	case map[string]any:
		return len(v) == 0
	default:
		return false
	}
}

// siblingGeneration 提取同胞生成配置。
// Python: action_planner.py:1796-1801 _sibling_generation。
func siblingGeneration(optimizationExperience map[string]any) map[string]any {
	// Python: if not isinstance(optimization_experience, dict): return {}
	if optimizationExperience == nil {
		return map[string]any{}
	}
	// Python: sibling_generation = optimization_experience.get("sibling_generation")
	if sg, ok := optimizationExperience["sibling_generation"].(map[string]any); ok {
		return sg
	}
	return map[string]any{}
}

// plannerSessionID 规划会话 ID（同胞候选参与命名）。
// Python: action_planner.py:1804-1812 _planner_session_id。
func plannerSessionID(optimizationExperience map[string]any) string {
	// Python: sibling_generation = _sibling_generation(...); candidate_id = ...
	candidateID := strings.TrimSpace(pyStr(siblingGeneration(optimizationExperience)["candidate_id"]))
	if candidateID == "" {
		return "member_action_planner"
	}
	// Python: safe_candidate_id = re.sub(r"[^A-Za-z0-9_-]+", "_", candidate_id).strip("_")
	safeRE := regexp.MustCompile(`[^A-Za-z0-9_-]+`)
	safeCandidateID := strings.Trim(safeRE.ReplaceAllString(candidateID, "_"), "_")
	// Python: digest = hashlib.sha256(candidate_id.encode()).hexdigest()[:10]
	sum := sha256.Sum256([]byte(candidateID))
	digest := hex.EncodeToString(sum[:])[:10]
	// Python: f"member_action_planner_{safe_candidate_id[:32] or 'candidate'}_{digest}"
	short := safeCandidateID
	if len(short) > 32 {
		short = short[:32]
	}
	if short == "" {
		short = "candidate"
	}
	return fmt.Sprintf("member_action_planner_%s_%s", short, digest)
}

// ──────────────────────────── 结构体（agent 与编排器） ────────────────────────────

// MemberActionPlannerAgent 生成 MemberOptimizationPlan 草稿的 DeepAgent 规划器。
// Python: action_planner.py:738-930 MemberActionPlannerAgent。
type MemberActionPlannerAgent struct {
	// modelConfigRef 模型配置引用
	modelConfigRef string
	// retryLimit 阶段重试上限
	retryLimit int
	// workspace 工作空间路径（空串等价 None）
	workspace string
	// agentSkillsDirs agent 技能目录
	agentSkillsDirs []string
	// lastEvidenceHolder 最近一次 CreatePlan 的证据护栏（adapter 返回后缓存）
	lastEvidenceHolder agentface.EvidenceFinalizer
	// prebuiltAgent 预构建 agent（测试注入点，等价 Python monkeypatch）
	prebuiltAgent *agentface.Agent
}

// MemberActionPlannerAgentBuildRetryMessage 构造重试消息。
// Python: action_planner.py:833-852 _build_retry_message。
func MemberActionPlannerAgentBuildRetryMessage(original string, errText string) string {
	return "Return ONLY one valid JSON or YAML mapping.\n" +
		"Do not include reasoning or markdown outside the final object.\n\n" +
		"Original task:\n" + original + "\n\n" +
		"Last error:\n" + errText + "\n\n" +
		"If the last error is only schema or formatting, keep the same " +
		"chosen surface and fix only the invalid fields. If you are " +
		"uncertain which harness surface should change, return an empty " +
		"plan with no actions instead of guessing an existing prompt file. " +
		"If the last error says a target has actionable optimization_surfaces " +
		"but no executable action, keep the diagnosed surface and add one " +
		"matching action for that target."
}

// BuildPlannerHarnessSummaries 构建各角色 harness 结构摘要。
// Python: action_planner.py:855-899 _build_harness_summaries。
func BuildPlannerHarnessSummaries(targets []MemberOptimizationTarget) map[string]string {
	summaries := map[string]string{}
	for i := range targets {
		target := &targets[i]
		// Python: try: harness_path = Path(target.harness_ref_path).expanduser().resolve()
		harnessPath := target.HarnessRefPath
		if strings.HasPrefix(harnessPath, "~") {
			if home, err := os.UserHomeDir(); err == nil {
				harnessPath = filepath.Join(home, strings.TrimPrefix(harnessPath, "~"))
			}
		}
		if abs, err := filepath.Abs(harnessPath); err == nil {
			harnessPath = abs
		}
		// Python: summary_parts = []; if is_dir(): ...
		var summaryParts []string
		info, statErr := os.Stat(harnessPath)
		if statErr == nil && info.IsDir() {
			// Python: harness_yaml / identity_md / soul_md 检测
			if fileExists(filepath.Join(harnessPath, "harness.yaml")) {
				summaryParts = append(summaryParts, "files: harness.yaml")
			}
			if fileExists(filepath.Join(harnessPath, "identity.md")) {
				summaryParts = append(summaryParts, "files: identity.md")
			}
			if fileExists(filepath.Join(harnessPath, "soul.md")) {
				summaryParts = append(summaryParts, "files: soul.md")
			}
			// Python: for folder_name in ("prompt_sections", "skills", "tools", "rails"): ...
			for _, folderName := range []string{"prompt_sections", "skills", "tools", "rails"} {
				if folderInfo, err := os.Stat(filepath.Join(harnessPath, folderName)); err == nil && folderInfo.IsDir() {
					summaryParts = append(summaryParts, "surface: "+folderName+"/")
				}
			}
			// Python: if harness_yaml.is_file(): try: data = yaml.safe_load(...); config 计数
			if fileExists(filepath.Join(harnessPath, "harness.yaml")) {
				if content, err := os.ReadFile(filepath.Join(harnessPath, "harness.yaml")); err == nil {
					var data map[string]any
					if yamlErr := yaml.Unmarshal(content, &data); yamlErr == nil && data != nil {
						toolsLen := listLenOrZero(data["tools"])
						skillsLen := listLenOrZero(data["skills"])
						sectionsLen := listLenOrZero(data["prompt_sections"])
						railsLen := listLenOrZero(data["rails"])
						summaryParts = append(summaryParts, fmt.Sprintf(
							"config: tools=%d, skills=%d, prompt_sections=%d, rails=%d",
							toolsLen, skillsLen, sectionsLen, railsLen))
					} else {
						summaryParts = append(summaryParts, "config: unreadable")
					}
				} else {
					summaryParts = append(summaryParts, "config: unreadable")
				}
			}
		}
		// Python: summaries[target.role] = "; ".join(...) if parts else "no structure summary available"
		if len(summaryParts) > 0 {
			summaries[target.Role] = strings.Join(summaryParts, "; ")
		} else {
			summaries[target.Role] = "no structure summary available"
		}
	}
	return summaries
}

// CreatePlan 经 DeepAgent 生成方案草稿（未校验）。
// Python: action_planner.py:664-831 create_plan。
func (a *MemberActionPlannerAgent) CreatePlan(ctx context.Context, params PlannerCreateParams) (map[string]any, error) {
	// Python: harness_summaries = harness_summaries or self._build_harness_summaries(targets)
	harnessSummaries := params.HarnessSummaries
	if harnessSummaries == nil {
		harnessSummaries = BuildPlannerHarnessSummaries(params.Targets)
	}
	// Python: evidence_rail = PlannerEvidenceRail(ACTION_PLANNING.max_iterations); agent = create_action_planning_agent(...)
	agent, err := a.createDeepAgent(ctx, params, harnessSummaries)
	if err != nil {
		return nil, err
	}
	// Python: evidence_rail 在 create_plan 内创建并被 finalize 闭包持有；
	// Go 经 adapter 返回的 Finalizer 直接缓存（D-c2-4 桥接时序等价）
	a.lastEvidenceHolder = agent.Finalizer
	// Python: user_message = self._build_user_message(...)
	userMessage := a.BuildUserMessage(params)
	// Python: retry_limit = feedback_tracker.max_attempts - 1 if tracker else min(self._retry_limit, 1)
	retryLimit := params.RetryLimit
	// Python: return await invoke_member_optimizer_agent_structured(...)
	return agentsBridge.InvokeStructured(ctx, StructuredInvokeParams{
		Agent:            agent,
		AgentName:        "MemberActionPlannerAgent",
		UserMessage:      userMessage,
		SessionID:        plannerSessionID(params.OptimizationExperience),
		RetryLimit:       retryLimit,
		ParseResponse:    agentsBridge.ParseYamlOrJSONObject,
		ValidateResponse: ValidatePlanStructure,
		FeedbackTracker:  params.FeedbackTracker,
		BuildRetryMessage: func(_ map[string]any, errText string) string {
			// Python: lambda _previous, error: self._build_retry_message(user_message, error)
			return MemberActionPlannerAgentBuildRetryMessage(userMessage, errText)
		},
		RecoverResponse: func(message, previous string) (map[string]any, error) {
			// Python: finalize(message, previous)：model.invoke(messages=evidence_rail.finalization_messages(...), tools=None)
			rawText, err := a.finalize(ctx, params, message, previous)
			if err != nil {
				return nil, err
			}
			parsed, parseErr := agentsBridge.ParseYamlOrJSONObject(rawText)
			if parseErr != nil {
				return nil, parseErr
			}
			return parsed, nil
		},
	})
}

// finalize 经 BudgetedRsiModel 直连模型的收尾请求。
// Python: action_planner.py:787-795 finalize。
func (a *MemberActionPlannerAgent) finalize(ctx context.Context, params PlannerCreateParams, message, previous string) (string, error) {
	// Python: model = load_member_optimizer_model(self._model_config_ref)
	model, err := LoadMemberOptimizerModel(a.modelConfigRef)
	if err != nil {
		return "", err
	}
	// Python: response = await model.invoke(messages=evidence_rail.finalization_messages(message, previous), tools=None)
	if a.lastEvidenceHolder == nil {
		return "", errors.New("planner evidence rail holder is required for finalization")
	}
	messages := a.lastEvidenceHolder.FinalizationMessages(message, previous)
	// Python: return response.content or ""
	response, err := model.Invoke(ctx, modelclients.NewMessagesParam(wrapBaseMessages(messages)...))
	if err != nil {
		return "", err
	}
	content := ""
	if response != nil {
		content = fmt.Sprintf("%v", response.Content)
	}
	return content, nil
}

// PlannerCreateParams CreatePlan 关键字参数收拢。
// Python: create_plan(...) 关键字参数。
type PlannerCreateParams struct {
	// Targets 选中的优化目标
	Targets []MemberOptimizationTarget
	// RoleAttributionReport 定责报告
	RoleAttributionReport *RoleAttributionReport
	// MechanismAttributionReport 病根归因报告
	MechanismAttributionReport *MechanismAttributionReport
	// ActionDefinitions 动作定义
	ActionDefinitions []harnessrsi.ActionDefinition
	// HarnessSummaries 结构摘要（nil 则构建）
	HarnessSummaries map[string]string
	// ValidationErrors 上轮校验错误
	ValidationErrors []string
	// RejectedCapabilities 被拒能力历史
	RejectedCapabilities []map[string]any
	// OptimizationHypotheses 不可变假设契约
	OptimizationHypotheses []map[string]any
	// OptimizationExperience 优化经验
	OptimizationExperience map[string]any
	// FeedbackTracker 反馈追踪器
	FeedbackTracker *harnessrsi.FeedbackTracker
	// RetryLimit 重试上限（tracker 非空时为 max_attempts-1）
	RetryLimit int
	// EvidenceRailHolder 证据护栏持有者（收尾消息构造；agents 侧 adapter 填充）
}

// PlannerEvidenceHolder 证据护栏收尾消息构造接口（agentface.EvidenceFinalizer 别名保留消费点语义）。

// BuildPlannerUserMessage 构造规划用户消息。
// Python: action_planner.py:902-1042 _build_user_message（staticmethod）。
func (a *MemberActionPlannerAgent) BuildUserMessage(params PlannerCreateParams) string {
	return buildPlannerUserMessage(params)
}

// createDeepAgent 创建规划 DeepAgent。
// Python: create_plan 内联 create_action_planning_agent(...)。
func (a *MemberActionPlannerAgent) createDeepAgent(ctx context.Context, params PlannerCreateParams, harnessSummaries map[string]string) (*agentface.Agent, error) {
	// 测试注入点
	if a.prebuiltAgent != nil {
		return a.prebuiltAgent, nil
	}
	// Python: workspace=self._workspace or "."
	workspace := a.workspace
	if workspace == "" {
		workspace = "."
	}
	if agentsBridge.CreateActionPlanningAgent == nil {
		return nil, errors.New("agents bridge not registered: import member_optimizer/agents (blank) to wire implementation")
	}
	return agentsBridge.CreateActionPlanningAgent(ctx, PlannerAgentParams{
		ModelConfigRef:    a.modelConfigRef,
		Workspace:         workspace,
		AgentSkillsDirs:   a.agentSkillsDirs,
		ActionDefinitions: params.ActionDefinitions,
		HarnessSummaries:  harnessSummaries,
	})
}

// ──────────────────────────── 结构体（编排器） ────────────────────────────

// MemberActionPlanner 构建依赖感知优化方案（DeepAgent + 校验）。
// Python: action_planner.py:1136-1634 MemberActionPlanner。
type MemberActionPlanner struct {
	// agent 预注入 agent（nil 则按参数构造）
	agent *MemberActionPlannerAgent
	// outputRepair 输出修复配置（默认 max_attempts=10）
	outputRepair harnessrsi.OutputRepairConfig
}

// PlanParams Plan 关键字参数收拢。
// Python: plan(...) 关键字参数。
type PlanParams struct {
	// Targets 优化目标
	Targets []MemberOptimizationTarget
	// RoleAttributionReport 定责报告
	RoleAttributionReport *RoleAttributionReport
	// MechanismAttributionReport 病根归因报告
	MechanismAttributionReport *MechanismAttributionReport
	// ActionDefinitions 动作定义
	ActionDefinitions []harnessrsi.ActionDefinition
	// ModelConfigRef 模型配置引用
	ModelConfigRef string
	// StageRetryLimit 阶段重试上限（默认 2）
	StageRetryLimit int
	// HarnessSummaries 结构摘要
	HarnessSummaries map[string]string
	// AgentWorkspace agent 工作空间
	AgentWorkspace string
	// AgentSkillsDirs agent 技能目录
	AgentSkillsDirs []string
	// RejectedCapabilities 被拒能力历史
	RejectedCapabilities []map[string]any
	// AllowedActionGroups 受限动作组
	AllowedActionGroups []string
	// AllowedPromptSurfaces 受限 prompt 面
	AllowedPromptSurfaces []string
	// MaxActionsPerPlan 单方案最大动作数（0 = 不限）
	MaxActionsPerPlan int
	// OptimizationHypotheses 不可变假设契约
	OptimizationHypotheses []map[string]any
	// OptimizationExperience 优化经验
	OptimizationExperience map[string]any
}

// NewMemberActionPlanner 构造规划器（Python __init__）。
func NewMemberActionPlanner(plannerAgent *MemberActionPlannerAgent, outputRepair *harnessrsi.OutputRepairConfig, maxAttempts *int) *MemberActionPlanner {
	// Python: self._output_repair = output_repair or OutputRepairConfig(max_attempts=10)
	repair := harnessrsi.DefaultOutputRepairConfig()
	repair.MaxAttempts = 10
	if outputRepair != nil {
		repair = *outputRepair
	}
	// Python: if max_attempts is not None: replace(..., max_attempts=max_attempts)
	if maxAttempts != nil {
		repair.MaxAttempts = *maxAttempts
	}
	return &MemberActionPlanner{agent: plannerAgent, outputRepair: repair}
}

// Plan 生成并校验 member 优化方案。
// Python: action_planner.py:1185-1520 plan。
func (p *MemberActionPlanner) Plan(ctx context.Context, params PlanParams) (*MemberOptimizationPlan, error) {
	// Python 默认值对齐
	if params.StageRetryLimit == 0 {
		params.StageRetryLimit = 2
	}
	// Python: (actionable_targets, actionable_role_report, actionable_mechanism_report, filtered_issue_ids) = _filter_actionable_planning_inputs(...)
	actionableTargets, actionableRoleReport, actionableMechanismReport, filteredIssueIDs := filterActionablePlanningInputs(
		params.Targets, params.RoleAttributionReport, params.MechanismAttributionReport)
	// Python: restricted_groups = {str(group).strip() for group in (allowed_action_groups or []) if ...}
	restrictedGroups := map[string]bool{}
	for _, group := range params.AllowedActionGroups {
		if strings.TrimSpace(group) != "" {
			restrictedGroups[strings.TrimSpace(group)] = true
		}
	}
	// Python: restricted_prompt_surfaces = {...normalize(surface) ...}
	restrictedPromptSurfaces := map[string]bool{}
	for _, surface := range params.AllowedPromptSurfaces {
		if strings.TrimSpace(surface) != "" {
			restrictedPromptSurfaces[normalizeOptimizationSurface(surface)] = true
		}
	}
	// Python: unsupported_prompt_surfaces = restricted - {"prompt_section", "identity", "soul"}; if any: raise ValueError
	unsupported := []string{}
	for surface := range restrictedPromptSurfaces {
		if surface != "prompt_section" && surface != "identity" && surface != "soul" {
			unsupported = append(unsupported, surface)
		}
	}
	if len(unsupported) > 0 {
		sort.Strings(unsupported)
		return nil, fmt.Errorf("unsupported allowed_prompt_surfaces: %v", unsupported)
	}
	// Python: (actionable_targets, actionable_mechanism_report) = _adapt_prompt_surface_within_instruction_lever(...)
	actionableTargets, actionableMechanismReport = adaptPromptSurfaceWithinInstructionLever(
		actionableTargets, actionableMechanismReport, restrictedPromptSurfaces)
	// Python: deferred_capability_requests = []; if restricted_groups: ...
	var deferredCapabilityRequests []any
	if len(restrictedGroups) > 0 {
		// Python: allowed_surfaces = set(restricted_groups); if "prompt" in allowed: update(prompt surfaces)
		allowedSurfaces := map[string]bool{}
		for group := range restrictedGroups {
			allowedSurfaces[group] = true
		}
		if restrictedGroups["prompt"] {
			if len(restrictedPromptSurfaces) > 0 {
				for surface := range restrictedPromptSurfaces {
					allowedSurfaces[surface] = true
				}
			} else {
				allowedSurfaces["prompt_section"] = true
				allowedSurfaces["identity"] = true
				allowedSurfaces["soul"] = true
			}
		}
		// Python: allowed_surfaces &= _supported_optimization_surfaces(action_definitions)
		supported := supportedOptimizationSurfaces(params.ActionDefinitions)
		for surface := range allowedSurfaces {
			if !supported[surface] {
				delete(allowedSurfaces, surface)
			}
		}
		// Python: retained_targets / deferred_capability_requests
		var retainedTargets []MemberOptimizationTarget
		for i := range actionableTargets {
			target := &actionableTargets[i]
			targetSurfaces := map[string]bool{}
			for _, surface := range target.OptimizationSurfaces {
				targetSurfaces[strings.TrimSpace(surface)] = true
			}
			// Python: if not target_surfaces or target_surfaces & allowed_surfaces: retained.append(target); continue
			overlap := false
			for surface := range targetSurfaces {
				if allowedSurfaces[surface] {
					overlap = true
					break
				}
			}
			if len(targetSurfaces) == 0 || overlap {
				retainedTargets = append(retainedTargets, *target)
				continue
			}
			// Python: deferred_capability_requests.append({...五字段...})
			deferredCapabilityRequests = append(deferredCapabilityRequests, map[string]any{
				"role":              target.Role,
				"issue_ids":         toAnySlice(target.AttributedIssueIDs),
				"required_surfaces": toAnySlice(setToSortedStrings(targetSurfaces)),
				"status":            "unsupported_capability_request",
				"reason":            "surface_not_available_in_restricted_action_contract; cross_lever_compensation_is_forbidden",
			})
		}
		actionableTargets = retainedTargets
	}
	// Python: if not actionable_targets: return 空方案（metadata 全量）
	if len(actionableTargets) == 0 {
		return &MemberOptimizationPlan{
			PlanID:      "member_plan_" + shortHex8(),
			Targets:     []MemberOptimizationTarget{},
			Actions:     []MemberOptimizationAction{},
			ActionWaves: [][]string{},
			Metadata: map[string]any{
				"planner":                         "member_action_planner_agent",
				"validated":                       true,
				"action_count":                    0,
				"wave_count":                      0,
				"filtered_inactionable_issue_ids": toAnySlice(filteredIssueIDs),
				"allowed_action_groups":           toAnySlice(setToSortedStrings(restrictedGroups)),
				"allowed_prompt_surfaces":         toAnySlice(setToSortedStrings(restrictedPromptSurfaces)),
				"capability_requests":             deferredCapabilityRequests,
			},
		}, nil
	}
	// Python: agent = self._get_agent(...)
	agent := p.getAgent(params.ModelConfigRef, params.StageRetryLimit, params.AgentWorkspace, params.AgentSkillsDirs)
	// Python: selected_roles = {t.role for t in actionable_targets}
	selectedRoles := map[string]bool{}
	for i := range actionableTargets {
		selectedRoles[actionableTargets[i].Role] = true
	}
	// Python: action_definitions = filter_action_definitions(action_definitions)
	actionDefinitions := filterHarnessDefinitions(params.ActionDefinitions)
	// Python: if restricted_groups: [d for d in ... if d.group in restricted_groups]
	if len(restrictedGroups) > 0 {
		var restricted []harnessrsi.ActionDefinition
		for _, definition := range actionDefinitions {
			if restrictedGroups[definition.Group] {
				restricted = append(restricted, definition)
			}
		}
		actionDefinitions = restricted
	}
	// Python: search_disabled_for_single_action = max_actions_per_plan == 1; if: 过滤 skill/search
	searchDisabledForSingleAction := params.MaxActionsPerPlan == 1
	if searchDisabledForSingleAction {
		var filtered []harnessrsi.ActionDefinition
		for _, definition := range actionDefinitions {
			if !(definition.Group == "skill" && definition.Operation == "search") {
				filtered = append(filtered, definition)
			}
		}
		actionDefinitions = filtered
	}
	// Python: plan_data = {}; validation_errors = None; final_errors = []
	planData := map[string]any{}
	var validationErrors []string
	var finalErrors []string
	// Python: tracker = FeedbackTracker(self._output_repair); for _attempt in range(tracker.max_attempts): ...
	tracker := harnessrsi.NewFeedbackTracker(p.outputRepair, nil)
	for attempt := 0; attempt < tracker.MaxAttempts; attempt++ {
		// Python: if tracker.stop_reason or tracker.attempts >= tracker.max_attempts: break
		if tracker.StopReason != "" || tracker.Attempts >= tracker.MaxAttempts {
			break
		}
		// Python: validation_errors 回落列表 + untrusted data 前缀
		var createValidationErrors []string
		if validationErrors != nil {
			planJSON, _ := json.Marshal(planData)
			truncated := string(planJSON)
			if len(truncated) > 16000 {
				truncated = truncated[:16000]
			}
			createValidationErrors = append(append([]string{}, validationErrors...),
				"Previous plan (untrusted data): "+truncated)
		}
		// Python: plan_data = await agent.create_plan(**create_plan_kwargs)
		createParams := PlannerCreateParams{
			Targets:                    actionableTargets,
			RoleAttributionReport:      actionableRoleReport,
			MechanismAttributionReport: actionableMechanismReport,
			ActionDefinitions:          actionDefinitions,
			HarnessSummaries:           params.HarnessSummaries,
			ValidationErrors:           createValidationErrors,
			RejectedCapabilities:       params.RejectedCapabilities,
			OptimizationHypotheses:     params.OptimizationHypotheses,
			OptimizationExperience:     params.OptimizationExperience,
			FeedbackTracker:            tracker,
			RetryLimit:                 tracker.MaxAttempts - 1,
		}
		var planErr error
		planData, planErr = agent.CreatePlan(ctx, createParams)
		if planErr != nil {
			// Python: except StructuredOutputExhaustedError: raise ActionPlanRejectedError(str(exc))
			var exhausted *StructuredOutputExhaustedError
			if errors.As(planErr, &exhausted) {
				return nil, &ActionPlanRejectedError{Msg: exhausted.Msg}
			}
			return nil, planErr
		}
		// Python: structure_errors = _validate_plan_structure(plan_data); if: continue
		structureErrors := ValidatePlanStructure(planData)
		if len(structureErrors) > 0 {
			validationErrors = structureErrors
			tracker.Failed(strings.Join(structureErrors, "; "), "")
			continue
		}
		// Python: try: _attach_improvement_briefs(...); _normalize_required_declared_paths(...)
		attachImprovementBriefs(planData, actionableTargets, actionableRoleReport)
		NormalizeRequiredDeclaredPaths(planData)
		// Python: prompt_surface_errors = _validate_allowed_prompt_surfaces(...); if: continue
		promptSurfaceErrors := validateAllowedPromptSurfaces(planData, restrictedPromptSurfaces)
		if len(promptSurfaceErrors) > 0 {
			validationErrors = promptSurfaceErrors
			tracker.Failed(strings.Join(promptSurfaceErrors, "; "), "")
			continue
		}
		// Python: if max_actions_per_plan > 0 and len(actions) > max: limit_error; continue
		if params.MaxActionsPerPlan > 0 && len(planActionsList(planData)) > params.MaxActionsPerPlan {
			limitError := fmt.Sprintf("restricted optimization mode allows at most %d action(s)", params.MaxActionsPerPlan)
			validationErrors = []string{limitError}
			tracker.Failed(limitError, "")
			continue
		}
		// Python: final_errors = _validate_and_repair_plan_data(...)
		finalErrors = ValidateAndRepairPlanData(planData, selectedRoles, actionableTargets, actionDefinitions)
		// Python: except (ValueError, ...): final_errors = [f"Invalid plan field structure: ..."]
		// Go 侧 nil/类型 panic 由 ValidatePlan 内部类型断言防住（无 recover 等价需求——偏差 D-c2-2）
		// Python: if not final_errors: break
		if len(finalErrors) == 0 {
			break
		}
		validationErrors = finalErrors
		tracker.Failed(strings.Join(finalErrors, "; "), "")
	}
	// Python: if validation_errors and (stop_reason or attempts >= max): raise ActionPlanRejectedError(...)
	if validationErrors != nil && (tracker.StopReason != "" || tracker.Attempts >= tracker.MaxAttempts) {
		reportedErrors := validationErrors
		if len(reportedErrors) == 0 {
			reportedErrors = finalErrors
		}
		stopReason := tracker.StopReason
		if stopReason == "" {
			stopReason = "attempt_limit_reached"
		}
		return nil, &ActionPlanRejectedError{
			Msg: fmt.Sprintf("Member action plan validation failed (%s): %s", stopReason, strings.Join(reportedErrors, "; ")),
		}
	}
	// Python: _bind_immutable_hypotheses(...); _annotate_action_bundles(...)
	bindImmutableHypotheses(planData, params.OptimizationHypotheses)
	annotateActionBundles(planData, params.OptimizationHypotheses)
	// Python: plan_id = plan_data.get("plan_id", f"member_plan_{uuid4}")
	planID := pyStr(ActionMapField(planData, "plan_id"))
	if planID == "" {
		planID = "member_plan_" + shortHex8()
	}
	// Python: actions = []; for action_data: MemberOptimizationAction(...)
	var actions []MemberOptimizationAction
	for _, actionData := range planActionsList(planData) {
		actions = append(actions, MemberOptimizationAction{
			ActionID:           pyStrOrEmpty(ActionMapField(actionData, "action_id")),
			Role:               pyStrOrEmpty(ActionMapField(actionData, "role")),
			MemberName:         pyStrOrEmpty(ActionMapField(actionData, "member_name")),
			ActionGroup:        pyStrOrEmpty(ActionMapField(actionData, "action_group")),
			Operation:          pyStrOrEmpty(ActionMapField(actionData, "operation")),
			ActionType:         pyStrOrEmpty(ActionMapField(actionData, "action_type")),
			TargetPath:         pyStrOrEmpty(ActionMapField(actionData, "target_path")),
			Description:        pyStrOrEmpty(ActionMapField(actionData, "description")),
			Rationale:          pyStrOrEmpty(ActionMapField(actionData, "rationale")),
			AttributedIssueIDs: strSliceFromAny(ActionMapField(actionData, "attributed_issue_ids")),
			DependsOn:          strSliceFromAny(ActionMapField(actionData, "depends_on")),
			RunIf:              pyStrOrEmpty(ActionMapField(actionData, "run_if")),
			AllowedSkills:      strSliceFromAny(ActionMapField(actionData, "allowed_skills")),
			AllowedTools:       actiongroups.SanitizeAllowedTools(strSliceFromAny(ActionMapField(actionData, "allowed_tools"))),
			CandidateQuery:     pyStrOrEmpty(ActionMapField(actionData, "candidate_query")),
			InstallRef:         pyStrOrEmpty(ActionMapField(actionData, "install_ref")),
			ExpectedEffect:     pyStrOrEmpty(ActionMapField(actionData, "expected_effect")),
			RiskNotes:          strSliceFromAny(ActionMapField(actionData, "risk_notes")),
			DeclaredWritePaths: strSliceFromAny(ActionMapField(actionData, "declared_write_paths")),
			Constraints:        mapOrEmpty(ActionMapField(actionData, "constraints")),
		})
	}
	// Python: action_waves = build_action_waves(actions)
	actionWaves, err := buildWavesFromActions(actions)
	if err != nil {
		return nil, err
	}
	// Python: metadata 全量
	planMetadata := map[string]any{}
	if m, ok := planData["metadata"].(map[string]any); ok {
		planMetadata = m
	}
	hypothesisIDs, _ := planMetadata["optimization_hypothesis_ids"].([]any)
	if hypothesisIDs == nil {
		hypothesisIDs = []any{}
	}
	actionBundles, _ := planMetadata["action_bundles"].([]any)
	if actionBundles == nil {
		actionBundles = []any{}
	}
	semanticAuthority := "attribution_pipeline"
	if len(params.OptimizationHypotheses) > 0 {
		semanticAuthority = "immutable_optimization_hypotheses"
	}
	allowedGroupsSorted := setToSortedStrings(restrictedGroups)
	allowedSurfacesSorted := setToSortedStrings(restrictedPromptSurfaces)
	return &MemberOptimizationPlan{
		PlanID:      planID,
		Targets:     actionableTargets,
		Actions:     actions,
		ActionWaves: actionWaves,
		Metadata: map[string]any{
			"planner":                                 "member_action_planner_agent",
			"validated":                               true,
			"action_count":                            len(actions),
			"wave_count":                              len(actionWaves),
			"filtered_inactionable_issue_ids":         toAnySlice(filteredIssueIDs),
			"allowed_action_groups":                   toAnySlice(allowedGroupsSorted),
			"allowed_prompt_surfaces":                 toAnySlice(allowedSurfacesSorted),
			"max_actions_per_plan":                    params.MaxActionsPerPlan,
			"skill_search_disabled_for_single_action": searchDisabledForSingleAction,
			"capability_requests":                     deferredCapabilityRequests,
			"optimization_hypothesis_ids":             hypothesisIDs,
			"action_bundles":                          actionBundles,
			"semantic_authority":                      semanticAuthority,
		},
	}, nil
}

// getAgent 取预注入 agent 或按参数构造。
// Python: action_planner.py:1156-1173 _get_agent。
func (p *MemberActionPlanner) getAgent(modelConfigRef string, stageRetryLimit int, agentWorkspace string, agentSkillsDirs []string) *MemberActionPlannerAgent {
	// Python: if self._agent is not None: return self._agent
	if p.agent != nil {
		return p.agent
	}
	// Python: return MemberActionPlannerAgent(...)
	return NewMemberActionPlannerAgent(modelConfigRef, stageRetryLimit, agentWorkspace, agentSkillsDirs)
}

// NewMemberActionPlannerAgent 构造规划 agent（Python __init__）。
func NewMemberActionPlannerAgent(modelConfigRef string, stageRetryLimit int, workspace string, agentSkillsDirs []string) *MemberActionPlannerAgent {
	// Python: self._harness_structure_rail = HarnessStructureRail()
	agent := &MemberActionPlannerAgent{
		modelConfigRef:  modelConfigRef,
		retryLimit:      stageRetryLimit,
		workspace:       workspace,
		agentSkillsDirs: append([]string{}, agentSkillsDirs...),
	}
	return agent
}

// buildPlannerUserMessage 构造规划用户消息。
// Python: action_planner.py:902-1042 _build_user_message。
func buildPlannerUserMessage(params PlannerCreateParams) string {
	// Python: targets_text = "\n".join(f"  - role: ..., member: ..., ...")
	var targetLines []string
	for i := range params.Targets {
		t := &params.Targets[i]
		targetLines = append(targetLines, fmt.Sprintf(
			"  - role: %s, member: %s, harness_ref: %s, issue_ids: %v, confidence: %.2f, mechanism_types: %v, optimization_surfaces: %v, reason: %s",
			t.Role, t.MemberName, t.HarnessRefPath, t.AttributedIssueIDs, t.Confidence, t.MechanismTypes, t.OptimizationSurfaces, t.Reason))
	}
	targetsText := strings.Join(targetLines, "\n")
	// Python: selected_roles = {target.role ...}
	selectedRoles := map[string]bool{}
	for i := range params.Targets {
		selectedRoles[params.Targets[i].Role] = true
	}
	// Python: role_issues_text（assigned_role_issues 过滤 selected_roles）
	roleIssuesText := ""
	for _, issue := range params.RoleAttributionReport.AssignedRoleIssues {
		if !selectedRoles[issue.Role] {
			continue
		}
		roleIssuesText += fmt.Sprintf("\n  Role %s:\n    issue=%s, confidence=%.2f, rationale=%s"+
			"\n    evidence=%s\n    trace_refs=%s",
			issue.Role, issue.IssueID, issue.Confidence, issue.Rationale,
			orNoneText(summarizeEvidenceItems(issue.Evidence, 0)),
			orNoneText(summarizeEvidenceItems(traceRefsToMaps(issue.TraceRefs), 0)))
	}
	// Python: role_mechanisms_text
	roleMechanismsText := ""
	for _, role := range sortedRoleKeys(params.MechanismAttributionReport.RoleMechanisms) {
		if !selectedRoles[role] {
			continue
		}
		roleMechanismsText += "\n  Role " + role + ":"
		for _, m := range params.MechanismAttributionReport.RoleMechanisms[role] {
			roleMechanismsText += fmt.Sprintf("\n    issue=%s, mechanism=%s, signature=%s, optimization_surface=%s, confidence=%.2f, rationale=%s"+
				"\n    evidence=%s\n    evidence_refs=%s",
				m.IssueID, m.MechanismType, m.FailureSignature, m.OptimizationSurface, m.Confidence, m.Rationale,
				orNoneText(summarizeEvidenceItems(m.Evidence, 0)),
				orNoneText(summarizeEvidenceItems(evidenceRefsToMaps(m.EvidenceRefs), 0)))
		}
	}
	// Python: action_contract = _build_action_contract_text(action_definitions or [])
	actionContract := buildActionContractText(params.ActionDefinitions)
	// Python: validation_feedback（f-string 模板）
	validationFeedback := ""
	if len(params.ValidationErrors) > 0 {
		var lines []string
		for _, errText := range params.ValidationErrors {
			lines = append(lines, "- "+errText)
		}
		validationFeedback = "\n## Previous Plan Validation Errors\n\nThe previous plan was rejected by deterministic validation:\n" +
			strings.Join(lines, "\n") + "\n\nGenerate a corrected plan for the same selected roles and diagnoses. Preserve\n" +
			"the diagnosed optimization surfaces. For surface mismatch errors, choose the\n" +
			"action_group and target_path that match the diagnosed optimization_surface.\n"
	}
	// Python: rejected_feedback（最后 8 条）
	rejectedFeedback := ""
	if len(params.RejectedCapabilities) > 0 {
		var lines []string
		start := len(params.RejectedCapabilities) - 8
		if start < 0 {
			start = 0
		}
		for _, item := range params.RejectedCapabilities[start:] {
			lines = append(lines, fmt.Sprintf(
				"- role=%v, group=%v, operation=%v, runtime_name=%v, expected_effect=%v, rejection_reason=%v, failure_class=%v, candidate_behavior_by_case=%v, target_case_ids=%v, verifier_deltas_by_case=%v, candidate_failure_diagnoses=%v, target_confirmation=%v, epoch_checkpoint_outcome=%v",
				pyOrDash(item["role"]), pyOrDash(item["action_group"]), pyOrDash(item["operation"]), pyOrDash(item["runtime_name"]),
				pyOrDash(item["expected_effect"]), pyOrDash(item["rejection_reason"]), pyOrDash(item["failure_class"]),
				pyOrDash(item["candidate_behavior_by_case"]), pyOrDash(item["target_case_ids"]), pyOrDash(item["verifier_deltas_by_case"]),
				pyOrDash(item["candidate_failure_diagnoses"]), pyOrDash(item["target_confirmation"]), pyOrDash(item["epoch_checkpoint_outcome"])))
		}
		rejectedFeedback = "\n## Rejected Capability History\n\n" + strings.Join(lines, "\n") + "\n\n" +
			"Do not add the same capability under a different file name. If the prior\n" +
			"capability was not invoked, repair activation/discoverability through a modify\n" +
			"action; do not create another tool/add candidate with equivalent intent.\n" +
			"Use the recorded behavior checks to separate availability, action, and outcome.\n" +
			"Missing evidence is unknown. If the method was available but not followed,\n" +
			"inspect executable control; if behavior changed but remained wrong, repair the\n" +
			"implementation or return counterevidence. Do not default failed Skills to Prompt.\n"
	}
	// Python: hypothesis_contract
	hypothesisContract := ""
	if len(params.OptimizationHypotheses) > 0 {
		// Python: compact_hypotheses = [{...十二字段...}]
		var compact []any
		for _, item := range params.OptimizationHypotheses {
			summary := ""
			if obs, ok := item["authoritative_observations"].(map[string]any); ok {
				summary = pyStrOrEmpty(obs["summary"])
			}
			compact = append(compact, map[string]any{
				"hypothesis_id":      pyStrOrEmpty(item["hypothesis_id"]),
				"content_sha256":     pyStrOrEmpty(item["content_sha256"]),
				"source_issue_id":    pyStrOrEmpty(item["source_issue_id"]),
				"target_case_ids":    pyOrNone(item["target_case_ids"]),
				"summary":            summary,
				"required_behavior":  pyStrOrEmpty(item["required_behavior"]),
				"forbidden_behavior": pyOrNone(item["forbidden_behavior"]),
				"public_trigger":     pyOrNone(item["public_trigger"]),
				"decisive_probe":     pyOrNone(item["decisive_probe"]),
				"decision_contract":  pyOrNone(item["decision_contract"]),
				"evidence_refs":      pyOrNone(item["evidence_refs"]),
				"lever_policy":       pyOrNone(item["lever_policy"]),
			})
		}
		compactJSON, _ := json.MarshalIndent(compact, "", "  ")
		hypothesisContract = "\n## Immutable Optimization Hypotheses\n\n" + string(compactJSON) + "\n\n" +
			"Freeze observed failure facts, the behavior objective and check expectations,\n" +
			"not an unproven cause or carrier preference. Read evidence/ when needed.\n" +
			"At most three necessary connected actions may implement one issue. If the cause\n" +
			"is contradicted, return counterevidence in an empty plan for reanalysis; do not\n" +
			"silently change the objective or the independent check.\n"
	}
	// Python: experience_context（journal/lever_scoreboard 缺一即空）
	experienceContext := ""
	if params.OptimizationExperience != nil {
		if _, hasJournal := params.OptimizationExperience["journal"]; hasJournal {
			experienceContext = buildExperienceContext(params.OptimizationExperience)
		} else if _, hasScoreboard := params.OptimizationExperience["lever_scoreboard"]; hasScoreboard {
			experienceContext = buildExperienceContext(params.OptimizationExperience)
		}
	}
	// Python: policy_context / sibling_context
	policyContext := buildImproverPolicyContext(params.OptimizationExperience)
	siblingContext := buildSiblingGenerationContext(params.OptimizationExperience)
	// Python: f-string 总模板
	return "## Current Action Contract\n\n" + actionContract + "\n\n" +
		"## Selected Optimization Targets\n\n" + targetsText + "\n\n" +
		"## Attributed Role Issues\n" + roleIssuesText + "\n\n" +
		"## Mechanism Attribution Summary\n" + roleMechanismsText + "\n" +
		hypothesisContract + "\n" + experienceContext + "\n" +
		policyContext + siblingContext + validationFeedback + "\n" + rejectedFeedback + "\n\n" +
		"## Output\n\nReturn ONLY a JSON plan object as specified in the system prompt.\n"
}

// buildExperienceContext 优化经验上下文。
// Python: action_planner.py:916-941（experience_context 组装段）。
func buildExperienceContext(optimizationExperience map[string]any) string {
	// Python: scoreboard = ...get("lever_scoreboard", {}); journal = ...get("journal", [])
	scoreboard := optimizationExperience["lever_scoreboard"]
	if scoreboard == nil {
		scoreboard = map[string]any{}
	}
	var recent []any
	if journal, ok := optimizationExperience["journal"].([]any); ok {
		for i := len(journal) - 8; i < len(journal); i++ {
			if i < 0 {
				continue
			}
			if m, isMap := journal[i].(map[string]any); isMap {
				recent = append(recent, compactExperimentForPlanner(m))
			}
		}
	}
	scoreboardJSON, _ := json.MarshalIndent(scoreboard, "", "  ")
	recentJSON, _ := json.MarshalIndent(recent, "", "  ")
	return "\n## Optimization Experience\n\nLever scoreboard:\n" + string(scoreboardJSON) + "\n\n" +
		"Recent experiments:\n" + string(recentJSON) + "\n\n" +
		"Use observed behavior and outcomes to choose supported components, including a\n" +
		"different carrier. Do not rename an equivalent rejected capability. Do not\n" +
		"disguise an unavailable environment or configuration change as instructions.\n" +
		"When verifier_deltas_by_case shows newly passing FAIL_TO_PASS operations, keep\n" +
		"that behavior and narrow the next action to remaining_failed_fail_to_pass.\n" +
		"Do not describe a binary 0-to-0 case score as \"no effect\" when the official\n" +
		"per-test delta records partial contract progress.\n"
}

// ──────────────────────────── 非导出函数（辅助） ────────────────────────────

// fileExists 文件存在判定。
func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

// listLenOrZero 列表长度（nil → 0）。
func listLenOrZero(value any) int {
	if list, ok := value.([]any); ok {
		return len(list)
	}
	return 0
}

// intMin 整数下界。
func intMin(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// orNoneText 空串 → none（Python `_summarize(...) or 'none'`）。
func orNoneText(text string) string {
	if text == "" {
		return "none"
	}
	return text
}

// traceRefsToMaps []map[string]string → []map[string]any。
func traceRefsToMaps(refs []map[string]string) []map[string]any {
	out := make([]map[string]any, 0, len(refs))
	for _, ref := range refs {
		m := map[string]any{}
		for key, value := range ref {
			m[key] = value
		}
		out = append(out, m)
	}
	return out
}

// evidenceRefsToMaps []map[string]string → []map[string]any（EvidenceRefs 同形）。
func evidenceRefsToMaps(refs []map[string]string) []map[string]any {
	return traceRefsToMaps(refs)
}

// pyOrDash Python f-string 中 None → "None"、缺失 → "None"；Go 统一 %v（nil → <nil> 偏差）。
func pyOrDash(value any) string {
	if value == nil {
		return "None"
	}
	return fmt.Sprintf("%v", value)
}

// strSliceFromAny any → []string（nil → nil）。
func strSliceFromAny(value any) []string {
	list, ok := value.([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(list))
	for _, item := range list {
		out = append(out, pyStrOrEmpty(item))
	}
	return out
}

// mapOrEmpty any → map[string]any。
func mapOrEmpty(value any) map[string]any {
	if m, ok := value.(map[string]any); ok {
		return m
	}
	return map[string]any{}
}

// sortedRoleKeys 角色键排序。
func sortedRoleKeys(roleMechanisms map[string][]RoleMechanismAttribution) []string {
	keys := make([]string, 0, len(roleMechanisms))
	for role := range roleMechanisms {
		keys = append(keys, role)
	}
	sort.Strings(keys)
	return keys
}

// buildWavesFromActions 由动作结构体构建波次。
// Python: action_waves = build_action_waves(actions)。
func buildWavesFromActions(actions []MemberOptimizationAction) ([][]string, error) {
	schedActions := make([]actiongroups.SchedAction, 0, len(actions))
	for i := range actions {
		schedActions = append(schedActions, actiongroups.SchedAction{
			ActionID:   actions[i].ActionID,
			DependsOn:  actions[i].DependsOn,
			TargetPath: actions[i].TargetPath,
		})
	}
	return actiongroups.BuildActionWaves(schedActions)
}

// filterHarnessDefinitions 过滤后的 harnessrsi.ActionDefinition 视图。
func filterHarnessDefinitions(actionDefinitions []harnessrsi.ActionDefinition) []harnessrsi.ActionDefinition {
	filtered := actionDefinitions
	var out []harnessrsi.ActionDefinition
	for _, definition := range filtered {
		out = append(out, harnessrsi.ActionDefinition{
			Name:          definition.Name,
			Group:         definition.Group,
			Operation:     definition.Operation,
			Function:      definition.Function,
			Purpose:       definition.Purpose,
			IsDestructive: definition.IsDestructive,
		})
	}
	return out
}

// wrapBaseMessages BaseMessage 包装（rail 消息直传）。
func wrapBaseMessages(messages []llmschema.BaseMessage) []llmschema.BaseMessage {
	return messages
}

// targetToMap 方案目标 asdict 等价（yaml tag 序列化）。
func targetToMap(t *MemberOptimizationTarget) map[string]any {
	data, _ := yaml.Marshal(t)
	var out map[string]any
	_ = yaml.Unmarshal(data, &out)
	return out
}

// actionToMap 方案动作 asdict 等价。
func actionToMap(a *MemberOptimizationAction) map[string]any {
	data, _ := yaml.Marshal(a)
	var out map[string]any
	_ = yaml.Unmarshal(data, &out)
	return out
}

// unused imports guard
var (
	_ = llm.Model{}
	_ = uuid.NewString
)

// requiredDeclaredPathsForAction 推导面隐含的必声明路径。
// Python: action_planner.py:586-600 _required_declared_paths_for_action。
func requiredDeclaredPathsForAction(actionGroup, operation, targetPath string) []string {
	// Python: if prompt and files/: [target, sections.yaml]
	if actionGroup == "prompt" && strings.HasPrefix(targetPath, "prompt_sections/files/") {
		return []string{targetPath, "prompt_sections/sections.yaml"}
	}
	// Python: if skill and search: ["skills", "skills/skills.yaml"]
	if actionGroup == "skill" && operation == "search" {
		return []string{"skills", "skills/skills.yaml"}
	}
	// Python: if skill and skills/: [target, skills.yaml]
	if actionGroup == "skill" && strings.HasPrefix(targetPath, "skills/") {
		return []string{targetPath, "skills/skills.yaml"}
	}
	// Python: if tool and tools/ and != tools.yaml: [target, tools.yaml]
	if actionGroup == "tool" && strings.HasPrefix(targetPath, "tools/") && targetPath != "tools/tools.yaml" {
		return []string{targetPath, "tools/tools.yaml"}
	}
	return []string{}
}

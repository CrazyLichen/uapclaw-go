// policy.go 当前 Auto Harness 的可执行动作策略。
// Python: action_groups/policy.py（全量对齐，412 行）。
// MemberOptimizer 只在 ExpertHarness 装载器可读的包面上规划和执行本地文件变更。

package actiongroups

import (
	"fmt"
	"path"
	"sort"
	"strconv"
	"strings"
)

// ──────────────────────────── 枚举 ────────────────────────────

// allowedActionGroups 允许的动作组集合。
// Python: policy.py:18-24 ALLOWED_ACTION_GROUPS。
var allowedActionGroups = map[string]bool{
	"prompt": true, "config": true, "rail": true, "skill": true, "tool": true,
}

// allowedOperations 允许的操作集合。
// Python: policy.py:26-31 ALLOWED_OPERATIONS。
var allowedOperations = map[string]bool{
	"add": true, "modify": true, "remove": true, "search": true,
}

// allowedExecutorTools 允许的执行器工具集合。
// Python: policy.py:33-40 ALLOWED_EXECUTOR_TOOLS。
var allowedExecutorTools = map[string]bool{
	"read_file": true, "grep": true, "glob": true,
	"list_files": true, "write_file": true, "edit_file": true,
}

// groupPathPrefixes 各动作组的允许路径前缀。
// Python: policy.py:42-49 GROUP_PATH_PREFIXES。
var groupPathPrefixes = map[string][]string{
	"config": {"runtime.yaml"},
	"prompt": {"identity.md", "soul.md", "prompt_sections/sections.yaml", "prompt_sections/files/"},
	"tool":   {"tools/"},
	"rail":   {"rails/"},
	"skill":  {"skills/"},
}

// ActionPolicyCheck 单条动作的策略校验结果。
// Python: policy.py:52-56 ActionPolicyCheck。
type ActionPolicyCheck struct {
	// Valid 是否通过校验
	Valid bool
	// Errors 错误清单
	Errors []string
}

// ──────────────────────────── 导出函数 ────────────────────────────

// ActionPolicyPrompt 返回当前 Auto Harness 可执行动作策略提示词。
// Python: policy.py:59-160 action_policy_prompt（f-string 模板逐字对齐）。
func ActionPolicyPrompt() string {
	// Python: path_lines = "\n".join(f"- {group}: {', '.join(prefixes)}" ...) sorted
	groups := make([]string, 0, len(groupPathPrefixes))
	for group := range groupPathPrefixes {
		groups = append(groups, group)
	}
	sort.Strings(groups)
	var pathLines []string
	for _, group := range groups {
		pathLines = append(pathLines, "- "+group+": "+strings.Join(groupPathPrefixes[group], ", "))
	}
	// Python: sorted(ALLOWED_ACTION_GROUPS) / sorted(ALLOWED_OPERATIONS)
	groupsList := make([]string, 0, len(allowedActionGroups))
	for group := range allowedActionGroups {
		groupsList = append(groupsList, group)
	}
	sort.Strings(groupsList)
	operationsList := make([]string, 0, len(allowedOperations))
	for operation := range allowedOperations {
		operationsList = append(operationsList, operation)
	}
	sort.Strings(operationsList)
	// Python: f-string 模板（{{ }} 转义为字面 { }）
	return "## Current Auto Harness Action Policy\n\n" +
		"Allowed action_group values:\n" + bulletList(groupsList) + "\n\n" +
		"Allowed operation values:\n" + bulletList(operationsList) + "\n\n" +
		"Allowed target_path and declared_write_paths by action_group:\n" +
		strings.Join(pathLines, "\n") + "\n\n" +
		"Current member optimizer actions are limited to local ExpertHarness package file\n" +
		"changes on prompt, skill, tool, rail, and whitelisted runtime config surfaces.\n\n" +
		"Operation rules:\n" +
		"- `search` is allowed only for `skill/search`.\n" +
		"- `skill/search` must set a non-empty English `candidate_query`, keep\n" +
		"  `install_ref` empty, target `skills/`, and declare `skills/` plus\n" +
		"  `skills/skills.yaml`. If search is unavailable or fails, use an explicit\n" +
		"  dependency-failed `skill/add` fallback.\n" +
		"- `skill/add` must target `skills/<snake_name>/SKILL.md`, declare that\n" +
		"  `SKILL.md` plus `skills/skills.yaml`, and keep `skills/skills.yaml`\n" +
		"  mounting the parent `skills` directory.\n" +
		"- For every non-search action, `candidate_query` and `install_ref` must be empty.\n" +
		"- `config` actions may only add/modify/remove `runtime.yaml`, with exactly that\n" +
		"  declared write path. Its only supported keys are `max_iterations` (positive\n" +
		"  integer under the host ceiling) and `on_max_iterations` (error,\n" +
		"  return_observations). The effective host limit is available in runtime evidence.\n" +
		"- Do not output arbitrary model config, mcp, dependency, test, documentation, memory,\n" +
		"  knowledge, context, workflow, install, or global environment actions.\n" +
		"Prompt rules:\n" +
		"- `identity.md` maps to the `identity` section with priority 10.\n" +
		"- `soul.md` maps to the `soul` section with priority 20.\n" +
		"- Extension sections must be declared in `prompt_sections/sections.yaml` and\n" +
		"  backed by `prompt_sections/files/*.md`.\n" +
		"- `prompt/remove` may delete only extension sections, never `identity.md` or\n" +
		"  `soul.md`.\n" +
		"- Surface selection:\n" +
		"  - `identity.md` is only for role identity and duty-boundary changes.\n" +
		"  - `soul.md` is only for a small number of durable operating principles.\n" +
		"  - Prompt sections clarify interpretation, priority, or a local constraint\n" +
		"    when the necessary capability already exists.\n" +
		"  - Skills organize reusable multi-step methods using existing capabilities.\n" +
		"    One case may motivate a provisional method; cross-case evidence tests its\n" +
		"    scope later and is not a Skill-only creation prerequisite.\n" +
		"  - Tools implement repeatable calculation, conversion, retrieval, or checks.\n" +
		"    Reuse existing execution tools or a Skill-local script when sufficient.\n" +
		"  - Choose from the missing operation, not the number of cases or target_ref\n" +
		"    label. One intervention may need connected actions across component types.\n" +
		"    Do not create a quota of types. Case IDs, fixed answers, or private rubric\n" +
		"    literals are not reusable content on any surface.\n\n" +
		"Tool rules:\n" +
		"- `tool/add` must target a loadable Python file under `tools/*.py`, declare\n" +
		"  both that Python file and `tools/tools.yaml`, and set\n" +
		"  `constraints.class_name` to the Tool subclass name.\n" +
		"- `tools/tools.yaml` must register package-local tools as mappings like\n" +
		"  `{{\"file\": \"tools/<name>.py\", \"class_name\": \"<ToolClass>\"}}`.\n\n" +
		"Rail rules:\n" +
		"- `rail/add` must target a loadable Python file under `rails/*.py`, declare\n" +
		"  both that Python file and `rails/rails.yaml`, and set\n" +
		"  `constraints.class_name` to the AgentRail subclass name.\n" +
		"- `rails/rails.yaml` must register package-local rails as mappings like\n" +
		"  `{{\"file\": \"rails/<name>.py\", \"class_name\": \"<AgentRailClass>\"}}`.\n" +
		"- Rails are for lifecycle and action-transition control. Do not use them as a\n" +
		"  static Prompt or Skill hidden inside Python source.\n"
}

// FilterActionDefinitions 保留符合当前动作策略的配置定义。
// Python: policy.py:163-172 filter_action_definitions。
func FilterActionDefinitions(actionDefinitions []PolicyActionDefinition) []PolicyActionDefinition {
	// Python: filtered = []; for definition: group in ALLOWED and _operation_allowed
	var filtered []PolicyActionDefinition
	for _, definition := range actionDefinitions {
		if !allowedActionGroups[definition.Group] {
			continue
		}
		if operationAllowed(definition.Group, definition.Operation) {
			filtered = append(filtered, definition)
		}
	}
	return filtered
}

// SanitizeAllowedTools 仅保留当前执行支持的文件读写编辑工具。
// Python: policy.py:175-177 sanitize_allowed_tools。
func SanitizeAllowedTools(tools []string) []string {
	// Python: return [tool for tool in tools or [] if tool in ALLOWED_EXECUTOR_TOOLS]
	var out []string
	for _, toolName := range tools {
		if allowedExecutorTools[toolName] {
			out = append(out, toolName)
		}
	}
	return out
}

// ValidateActionPolicy 按当前策略校验一条动作（dict 或 MemberOptimizationAction）。
// Python: policy.py:180-333 validate_action_policy。
func ValidateActionPolicy(action any, selectedRoles map[string]bool) ActionPolicyCheck {
	// Python: errors: list[str] = []; selected_roles = selected_roles or set()
	var errs []string
	// Python: 字段读取（dict 或对象双轨）
	actionID := pyStr(ActionField(action, "action_id"))
	role := pyStr(ActionField(action, "role"))
	actionGroup := pyStr(ActionField(action, "action_group"))
	operation := pyStr(ActionField(action, "operation"))
	targetPath := pyStr(ActionField(action, "target_path"))
	declaredPaths := listValue(ActionField(action, "declared_write_paths"))
	candidateQuery := pyStr(ActionField(action, "candidate_query"))
	installRef := pyStr(ActionField(action, "install_ref"))
	allowedTools := listValue(ActionField(action, "allowed_tools"))
	constraints := dictValue(ActionField(action, "constraints"))

	// Python: if not action_id / role 校验
	if actionID == "" {
		errs = append(errs, "missing action_id")
	}
	if role == "" {
		errs = append(errs, "missing role")
	} else if len(selectedRoles) > 0 && !selectedRoles[role] {
		errs = append(errs, fmt.Sprintf("role '%s' not in selected roles", role))
	}

	// Python: action_group / operation 白名单
	if !allowedActionGroups[actionGroup] {
		errs = append(errs, fmt.Sprintf("unsupported action_group '%s'", actionGroup))
	}
	if !allowedOperations[operation] {
		errs = append(errs, fmt.Sprintf("unsupported operation '%s'", operation))
	} else if actionGroup != "" && !operationAllowed(actionGroup, operation) {
		errs = append(errs, fmt.Sprintf("operation '%s' is not allowed for action_group '%s'", operation, actionGroup))
	}

	// Python: candidate_query / install_ref 规则
	if candidateQuery != "" && !(actionGroup == "skill" && operation == "search") {
		errs = append(errs, "candidate_query must be empty")
	}
	if installRef != "" {
		errs = append(errs, "install_ref must be empty")
	}

	// Python: skill/search 专项校验
	if actionGroup == "skill" && operation == "search" {
		if strings.TrimSpace(candidateQuery) == "" {
			errs = append(errs, "skill/search requires a non-empty candidate_query")
		}
		if normalizePolicyPath(targetPath) != "skills" {
			errs = append(errs, "skill/search target_path must be skills/")
		}
		declaredSet := map[string]bool{}
		for _, declaredPath := range declaredPaths {
			declaredSet[normalizePolicyPath(declaredPath)] = true
		}
		if !declaredSet["skills"] {
			errs = append(errs, "skill/search must declare skills/")
		}
		if !declaredSet["skills/skills.yaml"] {
			errs = append(errs, "skill/search must declare skills/skills.yaml")
		}
	}

	// Python: unsupported_tools
	var unsupportedTools []string
	for _, toolName := range allowedTools {
		if !allowedExecutorTools[toolName] {
			unsupportedTools = append(unsupportedTools, toolName)
		}
	}
	if len(unsupportedTools) > 0 {
		errs = append(errs, fmt.Sprintf("unsupported allowed_tools %v", unsupportedTools))
	}

	// Python: target_path 校验
	normalizedDeclaredPaths := map[string]bool{}
	if targetPath == "" {
		errs = append(errs, "target_path is empty")
	} else {
		errs = append(errs, validatePolicyPath(targetPath, actionGroup, "target_path")...)
	}
	for _, declaredPath := range declaredPaths {
		normalizedDeclaredPaths[normalizePolicyPath(declaredPath)] = true
		errs = append(errs, validatePolicyPath(declaredPath, actionGroup, "declared_write_path")...)
	}
	normalizedTargetPath := normalizePolicyPath(targetPath)

	// Python: config 专项
	if actionGroup == "config" {
		if normalizedTargetPath != "runtime.yaml" || len(normalizedDeclaredPaths) != 1 || !normalizedDeclaredPaths["runtime.yaml"] {
			errs = append(errs, "config actions may only target and declare runtime.yaml")
		}
	}
	// Python: prompt 专项
	if actionGroup == "prompt" {
		if operation == "add" && (normalizedTargetPath == "identity.md" || normalizedTargetPath == "soul.md") {
			errs = append(errs, "prompt/add may only create extension prompt sections")
		}
		if operation == "remove" && (normalizedTargetPath == "identity.md" || normalizedTargetPath == "soul.md") {
			errs = append(errs, "prompt/remove cannot delete identity.md or soul.md")
		}
		if strings.HasPrefix(normalizedTargetPath, "prompt_sections/files/") {
			sectionName := strings.TrimSpace(pyStr(constraints["section_name"]))
			if sectionName == "" {
				errs = append(errs, "prompt section actions require constraints.section_name")
			}
			if _, hasPriority := constraints["priority"]; hasPriority {
				if _, convErr := strconv.Atoi(pyStr(constraints["priority"])); convErr != nil {
					errs = append(errs, "constraints.priority must be an integer when provided")
				}
			}
		}
	}
	// Python: skill/add 专项
	if actionGroup == "skill" && operation == "add" {
		if !isSkillEntryPath(normalizedTargetPath) {
			errs = append(errs, "skill/add target_path must be skills/<snake_name>/SKILL.md")
		}
		if !normalizedDeclaredPaths[normalizedTargetPath] {
			errs = append(errs, "skill/add must declare its target SKILL.md file")
		}
		if !normalizedDeclaredPaths["skills/skills.yaml"] {
			errs = append(errs, "skill/add must declare skills/skills.yaml")
		}
	}
	// Python: prompt manifest 联锁
	promptManifestMissing := strings.HasPrefix(normalizedTargetPath, "prompt_sections/files/") &&
		!normalizedDeclaredPaths["prompt_sections/sections.yaml"]
	if actionGroup == "prompt" && promptManifestMissing {
		errs = append(errs, "prompt section file changes must also declare prompt_sections/sections.yaml so the file can be mounted")
	}
	// Python: skill manifest 联锁
	skillManifestMissing := strings.HasPrefix(normalizedTargetPath, "skills/") &&
		normalizedTargetPath != "skills/skills.yaml" &&
		!normalizedDeclaredPaths["skills/skills.yaml"]
	if actionGroup == "skill" && skillManifestMissing {
		errs = append(errs, "skill file changes must also declare skills/skills.yaml so the skill can be mounted")
	}
	// Python: tool manifest 联锁
	toolManifestMissing := strings.HasPrefix(normalizedTargetPath, "tools/") &&
		normalizedTargetPath != "tools/tools.yaml" &&
		!normalizedDeclaredPaths["tools/tools.yaml"]
	if actionGroup == "tool" && toolManifestMissing {
		errs = append(errs, "tool implementation changes must also declare tools/tools.yaml so the tool can be mounted")
	}
	// Python: tool/add 专项
	if actionGroup == "tool" && operation == "add" {
		if !isPythonResourcePath(normalizedTargetPath, "tools") {
			errs = append(errs, "tool/add target_path must be a package-local tools/*.py file")
		}
		if !normalizedDeclaredPaths[normalizedTargetPath] {
			errs = append(errs, "tool/add must declare its target Python file")
		}
		if !normalizedDeclaredPaths["tools/tools.yaml"] {
			errs = append(errs, "tool/add must declare tools/tools.yaml")
		}
		if strings.TrimSpace(pyStr(constraints["class_name"])) == "" {
			errs = append(errs, "tool/add requires constraints.class_name")
		}
	}
	// Python: tool/remove 保护 manifest
	if actionGroup == "tool" && operation == "remove" && normalizedTargetPath == "tools/tools.yaml" {
		errs = append(errs, "tool/remove must target a package-local tool, not tools/tools.yaml")
	}
	// Python: rail manifest 联锁
	railManifestMissing := strings.HasPrefix(normalizedTargetPath, "rails/") &&
		normalizedTargetPath != "rails/rails.yaml" &&
		!normalizedDeclaredPaths["rails/rails.yaml"]
	if actionGroup == "rail" && railManifestMissing {
		errs = append(errs, "rail implementation changes must also declare rails/rails.yaml so the rail can be mounted")
	}
	// Python: rail/add 专项
	if actionGroup == "rail" && operation == "add" {
		if !isPythonResourcePath(normalizedTargetPath, "rails") {
			errs = append(errs, "rail/add target_path must be a package-local rails/*.py file")
		}
		if !normalizedDeclaredPaths[normalizedTargetPath] {
			errs = append(errs, "rail/add must declare its target Python file")
		}
		if !normalizedDeclaredPaths["rails/rails.yaml"] {
			errs = append(errs, "rail/add must declare rails/rails.yaml")
		}
		if strings.TrimSpace(pyStr(constraints["class_name"])) == "" {
			errs = append(errs, "rail/add requires constraints.class_name")
		}
	}
	// Python: rail/remove 保护 manifest
	if actionGroup == "rail" && operation == "remove" && normalizedTargetPath == "rails/rails.yaml" {
		errs = append(errs, "rail/remove must target a package-local rail, not rails/rails.yaml")
	}
	return ActionPolicyCheck{Valid: len(errs) == 0, Errors: errs}
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// operationAllowed 组-操组合白名单。
// Python: policy.py:336-340 _operation_allowed。
func operationAllowed(actionGroup, operation string) bool {
	// Python: if operation == "search": return action_group == "skill"
	if operation == "search" {
		return actionGroup == "skill"
	}
	// Python: return operation in {"add", "modify", "remove"}
	return operation == "add" || operation == "modify" || operation == "remove"
}

// validatePolicyPath 路径策略校验。
// Python: policy.py:361-388 _validate_policy_path。
func validatePolicyPath(pathStr, actionGroup, fieldName string) []string {
	// Python: errors: list[str] = []; normalized = _normalize_policy_path(path)
	var errs []string
	normalized := normalizePolicyPath(pathStr)
	if normalized == "" {
		return []string{fieldName + " is empty"}
	}
	// Python: if Path(path).is_absolute(): ...
	if strings.HasPrefix(pathStr, "/") || pathAbsLike(pathStr) {
		errs = append(errs, fmt.Sprintf("%s is absolute: %s", fieldName, pathStr))
	}
	// Python: if ".." in Path(path).parts: ...
	if containsDotDotPath(pathStr) {
		errs = append(errs, fmt.Sprintf("%s contains '..': %s", fieldName, pathStr))
	}
	// Python: allowed_prefixes = GROUP_PATH_PREFIXES.get(action_group); ...
	if prefixes, ok := groupPathPrefixes[actionGroup]; ok && len(prefixes) > 0 {
		if !matchesAllowedPrefix(normalized, prefixes) {
			errs = append(errs, fmt.Sprintf("%s '%s' is not allowed for action_group '%s'", fieldName, pathStr, actionGroup))
		}
	}
	// Python: prompt prompt_sections 细分
	if actionGroup == "prompt" && strings.HasPrefix(normalized, "prompt_sections/") {
		if normalized != "prompt_sections/sections.yaml" && !strings.HasPrefix(normalized, "prompt_sections/files/") {
			errs = append(errs, fmt.Sprintf("%s '%s' is not loadable by ExpertHarness; use "+
				"prompt_sections/sections.yaml or prompt_sections/files/<name>.md", fieldName, pathStr))
		}
	}
	return errs
}

// normalizePolicyPath 路径规范化（反斜杠转正斜杠、去首尾斜杠）。
// Python: policy.py:391-392 _normalize_policy_path。
func normalizePolicyPath(pathStr string) string {
	return strings.Trim(path.Clean(strings.ReplaceAll(pathStr, "\\", "/")), "/")
}

// matchesAllowedPrefix 前缀匹配（带 / 的前缀按目录前缀语义）。
// Python: policy.py:395-405 _matches_allowed_prefix。
func matchesAllowedPrefix(pathStr string, allowedPrefixes []string) bool {
	for _, prefix := range allowedPrefixes {
		if strings.HasSuffix(prefix, "/") {
			normalizedPrefix := strings.Trim(prefix, "/")
			if pathStr == normalizedPrefix || strings.HasPrefix(pathStr, prefix) {
				return true
			}
			continue
		}
		if pathStr == prefix {
			return true
		}
	}
	return false
}

// isPythonResourcePath 是否 <folder>/<name>.py 两段路径。
// Python: policy.py:408-410 _is_python_resource_path。
func isPythonResourcePath(pathStr, folder string) bool {
	parts := strings.Split(pathStr, "/")
	return len(parts) == 2 && parts[0] == folder && strings.HasSuffix(parts[1], ".py")
}

// isSkillEntryPath 是否 skills/<snake_name>/SKILL.md 三段路径。
// Python: policy.py:413-416 _is_skill_entry_path。
func isSkillEntryPath(pathStr string) bool {
	parts := strings.Split(pathStr, "/")
	if len(parts) != 3 {
		return false
	}
	return parts[0] == "skills" && isSnakeName(parts[1]) && parts[2] == "SKILL.md"
}

// isSnakeName 是否合法 snake_case 名。
// Python: policy.py:419-426 _is_snake_name。
func isSnakeName(name string) bool {
	// Python: if not name or name != name.lower(): return False
	if name == "" || name != strings.ToLower(name) {
		return false
	}
	// Python: if name.startswith("_") or name.endswith("_") or "__" in name: return False
	if strings.HasPrefix(name, "_") || strings.HasSuffix(name, "_") || strings.Contains(name, "__") {
		return false
	}
	// Python: compact = name.replace("_", ""); return compact.isalnum() and not compact[0].isdigit()
	compact := strings.ReplaceAll(name, "_", "")
	if compact == "" {
		return false
	}
	for i, r := range compact {
		isAlnum := (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9')
		if !isAlnum {
			return false
		}
		if i == 0 && r >= '0' && r <= '9' {
			return false
		}
	}
	return true
}

// pyStr 等价 Python str(value)（nil → ""）。
func pyStr(value any) string {
	if value == nil {
		return ""
	}
	if s, ok := value.(string); ok {
		return s
	}
	return fmt.Sprintf("%v", value)
}

// listValue 等价 Python _list_value。
func listValue(value any) []string {
	if value == nil {
		return []string{}
	}
	if list, ok := value.([]string); ok {
		return list
	}
	if list, ok := value.([]any); ok {
		var out []string
		for _, item := range list {
			out = append(out, pyStr(item))
		}
		return out
	}
	return []string{pyStr(value)}
}

// dictValue 等价 Python _dict_value。
func dictValue(value any) map[string]any {
	if m, ok := value.(map[string]any); ok {
		return m
	}
	return map[string]any{}
}

// pathAbsLike Windows 盘符绝对路径判定（Python Path.is_absolute 的补集分支）。
func pathAbsLike(pathStr string) bool {
	if len(pathStr) >= 3 && pathStr[1] == ':' && (pathStr[2] == '/' || pathStr[2] == '\\') {
		return true
	}
	return strings.HasPrefix(pathStr, "\\\\")
}

// containsDotDotPath 路径是否含 ".." 段。
func containsDotDotPath(pathStr string) bool {
	for _, part := range strings.Split(strings.ReplaceAll(pathStr, "\\", "/"), "/") {
		if part == ".." {
			return true
		}
	}
	return false
}

// bulletList "- x" 行列表。
func bulletList(items []string) string {
	var out []string
	for _, item := range items {
		out = append(out, "- "+item)
	}
	return strings.Join(out, "\n")
}

// ──────────────────────────── 结构体（策略输入适配） ────────────────────────────

// ActionGetter 字段读取适配接口（Python _get 对 dict|对象 双轨的 Go 等价）。
// MemberOptimizationAction 等动作类型实现本接口即可参与策略校验。
type ActionGetter interface {
	// ActionFieldValue 返回动作字段值（Python getattr 等价）。
	ActionFieldValue(key string) any
}

// ActionField 读取动作字段（map 或 ActionGetter 双轨）。
// Python: policy.py:343-347 _get。
func ActionField(action any, key string) any {
	if m, ok := action.(map[string]any); ok {
		return m[key]
	}
	if g, ok := action.(ActionGetter); ok {
		return g.ActionFieldValue(key)
	}
	return nil
}

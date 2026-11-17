// action_executor_impl.go 执行 agent、编排器与确定性手术实现（action_executor.py 中后段）。
// 与 action_executor.go 构成 Python action_executor.py 的声明/实现分区（符号对照表见 spec）。

package memberoptimizer

import (
	"context"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/tamnd/gopapy/v2/parser2"
	agentface "github.com/uapclaw/uapclaw-go/internal/evolving/rsi/harness_rsi/member_optimizer/agentface"
	harnessrsi "github.com/uapclaw/uapclaw-go/internal/evolving/rsi/harness_rsi"
	"github.com/uapclaw/uapclaw-go/internal/evolving/rsi/harness_rsi/member_optimizer/action_groups"
	"gopkg.in/yaml.v3"
)

// ──────────────────────────── 非导出函数（工具群补齐） ────────────────────────────

// runtimeDecisionContractProjection 决策合同运行时投影。
// Python: action_executor.py:609-625 _runtime_decision_contract_projection。
func runtimeDecisionContractProjection(value any) map[string]any {
	raw := map[string]any{}
	if m, ok := value.(map[string]any); ok {
		raw = m
	}
	// Python: boundaries 归一（str → [str]；非 list → []）
	boundaries := raw["scope_boundary"]
	if s, isStr := boundaries.(string); isStr {
		boundaries = []any{s}
	}
	boundaryList, isList := boundaries.([]any)
	if !isList {
		boundaryList = []any{}
	}
	return map[string]any{
		"wrong_decision":        SanitizeRuntimeSemanticText(raw["wrong_decision"]),
		"causal_distinction":    SanitizeRuntimeSemanticText(raw["causal_distinction"]),
		"required_action":       SanitizeRuntimeSemanticText(raw["required_action"]),
		"acceptance_observable": SanitizeRuntimeSemanticText(raw["acceptance_observable"]),
		"scope_boundary":        toAnySlice(sanitizedRuntimeSemanticItems(boundaryList)),
		"activation_phase":      pyStrOrEmpty(raw["activation_phase"]),
	}
}

// sanitizedRuntimeSemanticItems 非空运行时安全语义串清单。
// Python: action_executor.py:627-635 _sanitized_runtime_semantic_items。
func sanitizedRuntimeSemanticItems(values []any) []string {
	var items []string
	for _, value := range values {
		cleaned := SanitizeRuntimeSemanticText(value)
		if cleaned != "" {
			items = append(items, cleaned)
		}
	}
	return items
}

// hashFile 文件 sha256。
// Python: action_executor.py:139-145 _hash_file。
func hashFile(path string) (string, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(content)
	return hexEncode(sum[:]), nil
}

// hexEncode 十六进制编码。
func hexEncode(data []byte) string {
	const hexDigits = "0123456789abcdef"
	out := make([]byte, len(data)*2)
	for i, b := range data {
		out[i*2] = hexDigits[b>>4]
		out[i*2+1] = hexDigits[b&0x0f]
	}
	return string(out)
}

// shortToken 值的短哈希（sha1 前 8 位）。
// Python: action_executor.py:111-113 _short_token。
func shortToken(value string) string {
	if value == "" {
		value = "default"
	}
	sum := sha1.Sum([]byte(value))
	return hexEncode(sum[:])[:8]
}

// renderFileContext 单文件上下文渲染（截断标记）。
// Python: action_executor.py:680-689 _render_file_context。
func renderFileContext(path, relPath string, maxChars int) string {
	content, err := os.ReadFile(path)
	if err != nil {
		return fmt.Sprintf("### %s\n\nCould not read file: %v", relPath, err)
	}
	text := string(content)
	truncated := text
	if len(text) > maxChars {
		truncated = text[:maxChars]
	}
	suffix := ""
	if len(text) > maxChars {
		suffix = "\n\n[content truncated]"
	}
	return fmt.Sprintf("### %s\n\n```text\n%s%s\n```", relPath, truncated, suffix)
}

// normalizeSkillFrontmatterName frontmatter 规范化（name 强制、description 兜底）。
// Python: action_executor.py:229-270 _normalize_skill_frontmatter_name。
func normalizeSkillFrontmatterName(content, skillName, fallbackDescription string) string {
	// Python: canonical_name = _canonical_skill_identifier(skill_name)
	canonicalName := CanonicalSkillIdentifier(skillName)
	description := strings.TrimSpace(fallbackDescription)
	if description == "" {
		description = "Reusable member harness skill."
	}
	lines := splitLinesPy(content)
	bodyLines := lines
	frontmatter := map[string]any{}
	// Python: frontmatter 解析（畸形回落 salvage）
	if len(lines) > 0 && strings.TrimSpace(lines[0]) == "---" {
		endIndex := -1
		for index, line := range lines[1:] {
			if strings.TrimSpace(line) == "---" {
				endIndex = index + 1
				break
			}
		}
		if endIndex > 0 {
			rawFrontmatter := strings.Join(lines[1:endIndex], "\n")
			var loaded map[string]any
			if strings.TrimSpace(rawFrontmatter) != "" {
				if err := yaml.Unmarshal([]byte(rawFrontmatter), &loaded); err != nil {
					for key, value := range SalvageSkillFrontmatter(rawFrontmatter) {
						loaded[key] = value
					}
				}
				if loaded != nil {
					frontmatter = loaded
				}
			}
			bodyLines = lines[endIndex+1:]
		}
	}
	// Python: frontmatter["name"] = canonical_name; description 兜底
	frontmatter["name"] = canonicalName
	if strings.TrimSpace(pyStrOrEmpty(frontmatter["description"])) == "" {
		frontmatter["description"] = description
	}
	// Python: yaml.safe_dump(frontmatter, sort_keys=False, allow_unicode=True).strip()
	frontmatterText, err := yaml.Marshal(frontmatter)
	if err != nil {
		return content
	}
	// Python: body = "\n".join(body_lines).strip("\n")
	body := strings.Trim(strings.Join(bodyLines, "\n"), "\n")
	if body != "" {
		return fmt.Sprintf("---\n%s---\n\n%s\n", string(frontmatterText), body)
	}
	return fmt.Sprintf("---\n%s---\n", string(frontmatterText))
}

// skillTriggerDescription 从决策合同推导技能触发描述。
// Python: action_executor.py:2056-2080 _skill_trigger_description。
func skillTriggerDescription(action MemberOptimizationAction) string {
	// Python: contracts = action.constraints.get("optimization_contracts", [])
	contracts, _ := action.Constraints["optimization_contracts"].([]any)
	// Python: 第一轮：decision_contract.causal_distinction
	for _, contract := range contracts {
		contractMap, isMap := contract.(map[string]any)
		if !isMap {
			continue
		}
		if decision, ok := contractMap["decision_contract"].(map[string]any); ok {
			distinction := SanitizeRuntimeSemanticText(decision["causal_distinction"])
			if distinction != "" {
				return fmt.Sprintf("Use when a task requires deciding whether %s.", strings.TrimSuffix(distinction, "."))
			}
		}
	}
	// Python: 第二轮：public_trigger 第一条非空
	for _, contract := range contracts {
		contractMap, isMap := contract.(map[string]any)
		if !isMap {
			continue
		}
		triggers := contractMap["public_trigger"]
		if s, isStr := triggers.(string); isStr {
			triggers = []any{s}
		}
		if list, isList := triggers.([]any); isList {
			for _, trigger := range list {
				cleaned := SanitizeRuntimeSemanticText(trigger)
				if cleaned != "" {
					return fmt.Sprintf("Use when %s.", strings.TrimSuffix(cleaned, "."))
				}
			}
		}
	}
	// Python: expected_effect 兜底
	expected := SanitizeRuntimeSemanticText(action.ExpectedEffect)
	if expected != "" {
		return fmt.Sprintf("Use when a task requires %s.", strings.TrimSuffix(expected, "."))
	}
	return "Use when the current task matches this reusable procedure."
}

// syncSkillRegistryForWrittenFiles 技能登记簿同步（写后挂载）。
// Python: action_executor.py:152-196 _sync_skill_registry_for_written_files。
func syncSkillRegistryForWrittenFiles(actionWorktree string, action MemberOptimizationAction, declaredPaths, writtenFiles []string) ([]string, error) {
	// Python: if action.action_group != "skill": return written_files
	if action.ActionGroup != "skill" {
		return writtenFiles, nil
	}
	var additions []string
	for _, path := range writtenFiles {
		normalized := NormalizeRelPath(path)
		parts := strings.Split(normalized, "/")
		// Python: if len(parts) >= 3 and parts[0] == "skills" and parts[-1] == "SKILL.md": 加父目录
		if len(parts) >= 3 && parts[0] == "skills" && parts[len(parts)-1] == "SKILL.md" {
			additions = append(additions, strings.Join(parts[:len(parts)-1], "/"))
		}
	}
	if len(additions) == 0 {
		return writtenFiles, nil
	}
	registryRel := "skills/skills.yaml"
	// Python: if not _path_allowed_by_declared(registry_rel, declared_paths): raise ValueError(...)
	if !PathAllowedByDeclared(registryRel, declaredPaths) {
		return nil, fmt.Errorf("skill action must declare %s so the skill can be mounted", registryRel)
	}
	registryPathAbs := filepath.Join(actionWorktree, registryRel)
	// Python: report = normalize_package_registries(...); if report.issues: raise
	report := NormalizePackageRegistries(actionWorktree, []string{registryRel})
	if len(report.Issues) > 0 {
		var msgs []string
		for _, issue := range report.Issues {
			msgs = append(msgs, issue.Describe())
		}
		return nil, fmt.Errorf("%s", strings.Join(msgs, "; "))
	}
	// Python: registry = _load_yaml_manifest(...) if is_file else []
	var existing []any
	if info, err := os.Stat(registryPathAbs); err == nil && !info.IsDir() {
		content, _ := os.ReadFile(registryPathAbs)
		var data any
		if err := yaml.Unmarshal(content, &data); err == nil && data != nil {
			if m, isMap := data.(map[string]any); isMap {
				existing, _ = m["skills"].([]any)
			} else if list, isList := data.([]any); isList {
				existing = list
			}
		}
	}
	// Python: if not isinstance(existing, list): raise ValueError("skills registry must contain a list")
	// Go: 非列表形态在上方回落 nil（空列表语义）
	merged := append([]any{}, existing...)
	mounted := map[string]bool{}
	for _, value := range existing {
		dir := ""
		if m, isMap := value.(map[string]any); isMap {
			dir = pyStrOrEmpty(m["dir"])
		} else {
			dir = pyStrOrEmpty(value)
		}
		mounted[strings.TrimPrefix(dir, "./")] = true
	}
	for _, addition := range additions {
		if !mounted[addition] {
			merged = append(merged, addition)
			mounted[addition] = true
		}
	}
	// Python: registry_path.write_text(yaml.safe_dump({"skills": merged}, ...))
	if err := os.MkdirAll(filepath.Dir(registryPathAbs), 0o755); err != nil {
		return nil, err
	}
	data, err := yaml.Marshal(map[string]any{"skills": merged})
	if err != nil {
		return nil, err
	}
	if err := os.WriteFile(registryPathAbs, data, 0o644); err != nil {
		return nil, err
	}
	// Python: return sorted(set([*written_files, registry_rel]))
	return sortedUniqueStrings(append(writtenFiles, registryRel)), nil
}

// validateGeneratedSkillContractErr 校验（错误包装同 Python）。
func validateGeneratedSkillContractErr(content string) error {
	return ValidateGeneratedSkillContract(content)
}

// validateGeneratedActionResources 生成资源合并前的廉价本地检查。
// Python: action_executor.py:1711-1771 _validate_generated_action_resources。
func validateGeneratedActionResources(worktreeDir string, action MemberOptimizationAction, changedFiles []string, runtimeIterationHardLimit int) []string {
	// Python: errors = [issue.describe() for issue in validate_package_registries(worktree_dir)]
	var errs []string
	for _, issue := range ValidatePackageRegistries(worktreeDir) {
		errs = append(errs, issue.Describe())
	}
	// Python: if config and != remove: read_runtime_policy 校验
	if action.ActionGroup == "config" && action.Operation != "remove" {
		if _, err := harnessrsi.ReadRuntimePolicy(worktreeDir, intMin(300, runtimeIterationHardLimit), "error", runtimeIterationHardLimit); err != nil {
			errs = append(errs, fmt.Sprintf("runtime_policy: %v", err))
		}
	}
	// Python: if skill: SKILL.md 契约 + frontmatter name/description 校验
	if action.ActionGroup == "skill" {
		for _, relPath := range changedFiles {
			normalized := NormalizeRelPath(relPath)
			expectedName := SkillNameFromSkillMdPath(normalized)
			if expectedName == "" {
				continue
			}
			path := filepath.Join(worktreeDir, normalized)
			if info, err := os.Stat(path); err != nil || info.IsDir() {
				continue
			}
			content, err := os.ReadFile(path)
			if err != nil {
				continue
			}
			if contractErr := ValidateGeneratedSkillContract(string(content)); contractErr != nil {
				errs = append(errs, fmt.Sprintf("skill_frontmatter:%s: %v", normalized, contractErr))
				continue
			}
			frontmatter := readSkillFrontmatter(path)
			actualName := strings.TrimSpace(pyStrOrEmpty(frontmatter["name"]))
			if actualName != expectedName {
				errs = append(errs, fmt.Sprintf("skill_frontmatter:%s: name must be %q, got %q", normalized, expectedName, actualName))
			}
			if strings.TrimSpace(pyStrOrEmpty(frontmatter["description"])) == "" {
				errs = append(errs, fmt.Sprintf("skill_frontmatter:%s: description is required", normalized))
			}
		}
	}
	// Python: for rel_path in changed_files: py 编译 / yaml / json 解析
	for _, relPath := range changedFiles {
		normalized := NormalizeRelPath(relPath)
		path := filepath.Join(worktreeDir, normalized)
		info, err := os.Stat(path)
		if err != nil || info.IsDir() {
			continue
		}
		content, _ := os.ReadFile(path)
		switch {
		case strings.HasSuffix(normalized, ".py"):
			// Python: compile(source, str(path), "exec") → gopapy ParseString（D-c3-1）
			if _, parseErr := parser2.ParseString(path, string(content)); parseErr != nil {
				errs = append(errs, fmt.Sprintf("python_compile:%s: %v", normalized, parseErr))
				continue
			}
			safetyErrors := validatePackagePythonSource(string(content), normalized)
			errs = append(errs, safetyErrors...)
		case strings.HasSuffix(strings.ToLower(normalized), ".yaml") || strings.HasSuffix(strings.ToLower(normalized), ".yml"):
			var parsed any
			if err := yaml.Unmarshal(content, &parsed); err != nil {
				errs = append(errs, fmt.Sprintf("yaml_parse:%s: %v", normalized, err))
			}
		case strings.HasSuffix(normalized, ".json"):
			var parsed any
			if err := json.Unmarshal(content, &parsed); err != nil {
				errs = append(errs, fmt.Sprintf("json_parse:%s: %v", normalized, err))
			}
		}
	}
	return errs
}

// readSkillFrontmatter 读取 SKILL.md frontmatter。
// Python: action_executor.py:1773-1785 _read_skill_frontmatter。
func readSkillFrontmatter(path string) map[string]any {
	content, err := os.ReadFile(path)
	if err != nil {
		return map[string]any{}
	}
	lines := splitLinesPy(string(content))
	if len(lines) == 0 || strings.TrimSpace(lines[0]) != "---" {
		return map[string]any{}
	}
	endIndex := -1
	for index, line := range lines[1:] {
		if strings.TrimSpace(line) == "---" {
			endIndex = index + 1
			break
		}
	}
	if endIndex <= 0 {
		return map[string]any{}
	}
	var loaded map[string]any
	if err := yaml.Unmarshal([]byte(strings.Join(lines[1:endIndex], "\n")), &loaded); err != nil || loaded == nil {
		return map[string]any{}
	}
	return loaded
}

// isAddLikeScaffoldAction add 形态脚手架动作判定。
// Python: action_executor.py:1922-1936 _is_add_like_scaffold_action。
func isAddLikeScaffoldAction(worktreeDir string, action MemberOptimizationAction) bool {
	// Python: if action.action_group not in {"tool", "rail"}: return False
	if action.ActionGroup != "tool" && action.ActionGroup != "rail" {
		return false
	}
	if action.Operation == "add" {
		return true
	}
	// Python: if action.operation != "modify" or action.action_group != "tool": return False
	if action.Operation != "modify" || action.ActionGroup != "tool" {
		return false
	}
	// Python: return not (worktree_dir / target_rel).exists()
	target := filepath.Join(worktreeDir, NormalizeRelPath(action.TargetPath))
	if _, err := os.Stat(target); err != nil {
		return true
	}
	return false
}

// executeDeterministicRemove 确定性删除。
// Python: action_executor.py:1628-1709 _execute_deterministic_remove。
func executeDeterministicRemove(worktreeDir string, action MemberOptimizationAction, declaredPaths []string) map[string]any {
	targetRel := NormalizeRelPath(action.TargetPath)
	// Python: if not _path_allowed_by_declared: return failed
	if !PathAllowedByDeclared(targetRel, declaredPaths) {
		return map[string]any{"status": "failed", "declared_write_paths": declaredPaths,
			"error": fmt.Sprintf("target_path %q is outside declared_write_paths", targetRel)}
	}
	var removedPaths []string
	var manifestUpdates []string
	switch action.ActionGroup {
	case "prompt":
		// Python: section_name = constraints.get("section_name", "")
		sectionName := pyStrOrEmpty(action.Constraints["section_name"])
		removedPaths = append(removedPaths, removePath(worktreeDir, targetRel)...)
		manifest := filepath.Join(worktreeDir, "prompt_sections", "sections.yaml")
		if info, err := os.Stat(manifest); err == nil && !info.IsDir() {
			if removePromptSectionEntry(manifest, targetRel, sectionName) {
				manifestUpdates = append(manifestUpdates, "prompt_sections/sections.yaml")
			}
		}
	case "skill":
		skillRootRel := skillRootFromTarget(targetRel)
		removedPaths = append(removedPaths, removePath(worktreeDir, skillRootRel)...)
		manifest := filepath.Join(worktreeDir, "skills", "skills.yaml")
		if info, err := os.Stat(manifest); err == nil && !info.IsDir() {
			if removeManifestEntries(manifest, "skills", map[string]bool{skillRootRel: true}, true) {
				manifestUpdates = append(manifestUpdates, "skills/skills.yaml")
			}
		}
	case "tool":
		removedPaths = append(removedPaths, removePath(worktreeDir, targetRel)...)
		manifest := filepath.Join(worktreeDir, "tools", "tools.yaml")
		if info, err := os.Stat(manifest); err == nil && !info.IsDir() {
			if removeManifestEntries(manifest, "tools", map[string]bool{targetRel: true}, false) {
				manifestUpdates = append(manifestUpdates, "tools/tools.yaml")
			}
		}
	case "config":
		removedPaths = append(removedPaths, removePath(worktreeDir, "runtime.yaml")...)
	case "rail":
		removedPaths = append(removedPaths, removePath(worktreeDir, targetRel)...)
		manifest := filepath.Join(worktreeDir, "rails", "rails.yaml")
		if info, err := os.Stat(manifest); err == nil && !info.IsDir() {
			if removeManifestEntries(manifest, "rails", map[string]bool{targetRel: true}, false) {
				manifestUpdates = append(manifestUpdates, "rails/rails.yaml")
			}
		}
	default:
		return map[string]any{"status": "failed", "declared_write_paths": declaredPaths,
			"error": fmt.Sprintf("deterministic remove unsupported for %s", action.ActionGroup)}
	}
	if len(removedPaths) == 0 && len(manifestUpdates) == 0 {
		return map[string]any{"status": "failed", "declared_write_paths": declaredPaths,
			"error": fmt.Sprintf("nothing removed for target_path %s", targetRel)}
	}
	return map[string]any{
		"status":               "succeeded",
		"declared_write_paths": declaredPaths,
		"error":                "",
		"remove": map[string]any{
			"target_path":      targetRel,
			"removed_paths":    toAnySlice(removedPaths),
			"manifest_updates": toAnySlice(manifestUpdates),
		},
	}
}

// executeDeterministicAddScaffold 确定性 add 脚手架。
// Python: action_executor.py:1787-1854 _execute_deterministic_add_scaffold。
func executeDeterministicAddScaffold(worktreeDir string, action MemberOptimizationAction, declaredPaths []string) map[string]any {
	targetRel := NormalizeRelPath(action.TargetPath)
	if !isAddLikeScaffoldAction(worktreeDir, action) {
		return map[string]any{"status": "skipped", "scaffold": map[string]any{}}
	}
	if !PathAllowedByDeclared(targetRel, declaredPaths) {
		return map[string]any{"status": "failed", "scaffold": map[string]any{},
			"error": fmt.Sprintf("target_path %q is outside declared_write_paths", targetRel)}
	}
	className := strings.TrimSpace(pyStrOrEmpty(action.Constraints["class_name"]))
	if className == "" {
		return map[string]any{"status": "failed", "scaffold": map[string]any{},
			"error": "add scaffold requires constraints.class_name"}
	}
	var manifestRel, listKey, content string
	if action.ActionGroup == "tool" {
		// Python: if not tools/ or not .py: return failed
		if !strings.HasPrefix(targetRel, "tools/") || !strings.HasSuffix(targetRel, ".py") {
			return map[string]any{"status": "failed", "scaffold": map[string]any{}, "error": "tool/add scaffold target must be tools/*.py"}
		}
		manifestRel = "tools/tools.yaml"
		listKey = "tools"
		content = toolScaffold(className, action.Description)
	} else {
		if !strings.HasPrefix(targetRel, "rails/") || !strings.HasSuffix(targetRel, ".py") {
			return map[string]any{"status": "failed", "scaffold": map[string]any{}, "error": "rail/add scaffold target must be rails/*.py"}
		}
		manifestRel = "rails/rails.yaml"
		listKey = "rails"
		content = railScaffold(className, action.Description)
	}
	// Python: if manifest not in declared: return failed
	declaredSet := map[string]bool{}
	for _, path := range declaredPaths {
		declaredSet[NormalizeRelPath(path)] = true
	}
	if !declaredSet[manifestRel] {
		return map[string]any{"status": "failed", "scaffold": map[string]any{},
			"error": fmt.Sprintf("add scaffold requires declared manifest %s", manifestRel)}
	}
	target := filepath.Join(worktreeDir, targetRel)
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return map[string]any{"status": "failed", "scaffold": map[string]any{}, "error": err.Error()}
	}
	if err := os.WriteFile(target, []byte(content), 0o644); err != nil {
		return map[string]any{"status": "failed", "scaffold": map[string]any{}, "error": err.Error()}
	}
	manifest := filepath.Join(worktreeDir, manifestRel)
	if err := os.MkdirAll(filepath.Dir(manifest), 0o755); err != nil {
		return map[string]any{"status": "failed", "scaffold": map[string]any{}, "error": err.Error()}
	}
	appendManifestEntry(manifest, listKey, map[string]any{"file": targetRel, "class_name": className})
	return map[string]any{
		"status": "succeeded",
		"scaffold": map[string]any{
			"target_path":   targetRel,
			"manifest_path": manifestRel,
			"class_name":    className,
		},
	}
}

// toolScaffold 工具脚手架源码。
// Python: action_executor.py:1938-1976 _tool_scaffold。
func toolScaffold(className, description string) string {
	toolName := snakeCase(className)
	desc := strings.ReplaceAll(description, "'", "\\'")
	if description == "" {
		desc = strings.ReplaceAll("Package-local member optimizer tool.", "'", "\\'")
	}
	return strings.Join([]string{
		"from openjiuwen.core.foundation.tool import Tool, ToolCard",
		"",
		"",
		fmt.Sprintf("class %s(Tool):", className),
		"    def __init__(self):",
		"        super().__init__(",
		"            ToolCard(",
		fmt.Sprintf("                id='%s',", toolName),
		fmt.Sprintf("                name='%s',", toolName),
		fmt.Sprintf("                description='%s',", desc),
		"                input_params={",
		"                    'type': 'object',",
		"                    'properties': {},",
		"                    'required': [],",
		"                },",
		"            )",
		"        )",
		"",
		"    async def invoke(self, inputs, **kwargs):",
		"        return {",
		"            'checklist': [",
		"                'confirm bounded concurrency before launching tasks',",
		"                'let already-started tasks run cleanup on cancellation',",
		"                'verify cancellation paths with focused tests',",
		"            ]",
		"        }",
		"",
		"    async def stream(self, inputs, **kwargs):",
		"        if False:",
		"            yield inputs",
		"",
	}, "\n")
}

// railScaffold 护栏脚手架源码。
// Python: action_executor.py:1978-1985 _rail_scaffold。
func railScaffold(className, description string) string {
	_ = description
	return strings.Join([]string{
		"from openjiuwen.core.single_agent.rail.base import AgentRail",
		"",
		"",
		fmt.Sprintf("class %s(AgentRail):", className),
		"    priority = 90",
		"",
		"    async def before_model_call(self, ctx):",
		"        return None",
		"",
	}, "\n")
}

// normalizeRailManifestRefs 规范化 rail manifest 引用到包内 rails/*.py。
// Python: action_executor.py:1856-1920 _normalize_rail_manifest_refs。
func normalizeRailManifestRefs(worktreeDir string, action MemberOptimizationAction, declaredPaths []string) map[string]any {
	// Python: if action.action_group != "rail": return None
	if action.ActionGroup != "rail" {
		return nil
	}
	normalizedDeclared := map[string]bool{}
	for _, path := range declaredPaths {
		normalizedDeclared[NormalizeRelPath(path)] = true
	}
	if !normalizedDeclared["rails/rails.yaml"] {
		return nil
	}
	manifest := filepath.Join(worktreeDir, "rails", "rails.yaml")
	if info, err := os.Stat(manifest); err != nil || info.IsDir() {
		return nil
	}
	data := loadYAMLManifestAny(manifest)
	var entries []any
	key := "rails"
	if m, isMap := data.(map[string]any); isMap {
		if e, ok := m["rails"].([]any); ok {
			entries = e
		}
	} else if list, isList := data.([]any); isList {
		entries = list
		data = map[string]any{"rails": entries}
	} else {
		return nil
	}
	// Python: if not isinstance(entries, list): entries = [entries]
	changed := false
	var normalizedEntries []any
	for _, entry := range entries {
		entryMap, isMap := entry.(map[string]any)
		if !isMap {
			normalizedEntries = append(normalizedEntries, entry)
			continue
		}
		fileValue := pyStrOrEmpty(entryMap["file"])
		if fileValue == "" {
			fileValue = pyStrOrEmpty(entryMap["file_path"])
		}
		if fileValue == "" {
			fileValue = pyStrOrEmpty(entryMap["path"])
		}
		if fileValue == "" {
			normalizedEntries = append(normalizedEntries, entry)
			continue
		}
		normalizedFile := canonicalRailFileRef(fileValue)
		if normalizedFile != fileValue {
			newEntry := map[string]any{}
			for k, v := range entryMap {
				newEntry[k] = v
			}
			newEntry["file"] = normalizedFile
			delete(newEntry, "file_path")
			delete(newEntry, "path")
			entry = newEntry
			changed = true
		}
		normalizedEntries = append(normalizedEntries, entry)
	}
	if !changed {
		return nil
	}
	if m, isMap := data.(map[string]any); isMap {
		m[key] = normalizedEntries
		data = m
	}
	writeYAMLManifest(manifest, data)
	var refs []any
	for _, entry := range normalizedEntries {
		if m, isMap := entry.(map[string]any); isMap && m["file"] != nil {
			refs = append(refs, m["file"])
		}
	}
	return map[string]any{
		"manifest_path":        "rails/rails.yaml",
		"normalized_file_refs": refs,
	}
}

// canonicalRailFileRef rail 引用规范化。
// Python: action_executor.py:1915-1920 _canonical_rail_file_ref。
func canonicalRailFileRef(rawPath string) string {
	normalized := NormalizeRelPath(rawPath)
	if strings.HasPrefix(normalized, "rails/") {
		return normalized
	}
	return "rails/" + filepath.Base(normalized)
}

// snakeCase 驼峰转 snake_case。
// Python: action_executor.py:2093-2101 _snake_case。
func snakeCase(value string) string {
	var chars []string
	for idx, char := range value {
		if char >= 'A' && char <= 'Z' && idx > 0 {
			chars = append(chars, "_")
		}
		chars = append(chars, strings.ToLower(string(char)))
	}
	out := strings.Trim(strings.Join(chars, ""), "_")
	if out == "" {
		return "member_optimizer_tool"
	}
	return out
}

// removePath 删除文件或目录（返回相对路径清单；空父目录回收）。
// Python: action_executor.py:2104-2116 _remove_path。
func removePath(worktreeDir, relPath string) []string {
	target := filepath.Join(worktreeDir, relPath)
	info, err := os.Stat(target)
	if err != nil {
		return []string{}
	}
	if info.IsDir() {
		var removed []string
		filepath.Walk(target, func(path string, f os.FileInfo, e error) error {
			if e == nil && !f.IsDir() {
				rel, _ := filepath.Rel(worktreeDir, path)
				removed = append(removed, NormalizeRelPath(rel))
			}
			return nil
		})
		sort.Strings(removed)
		os.RemoveAll(target)
		return removed
	}
	if err := os.Remove(target); err != nil {
		return []string{}
	}
	removeEmptyParentDirs(filepath.Dir(target), worktreeDir)
	return []string{relPath}
}

// removeEmptyParentDirs 回收至 stopAt 的空父目录。
// Python: action_executor.py:2119-2128 _remove_empty_parent_dirs。
func removeEmptyParentDirs(path, stopAt string) {
	stopAbs, _ := filepath.Abs(stopAt)
	current := path
	for {
		curAbs, _ := filepath.Abs(current)
		if curAbs == stopAbs {
			return
		}
		if err := os.Remove(current); err != nil {
			return
		}
		current = filepath.Dir(current)
	}
}

// skillRootFromTarget 技能目标 → skills/<name> 根。
// Python: action_executor.py:2131-2136 _skill_root_from_target。
func skillRootFromTarget(targetRel string) string {
	parts := strings.Split(targetRel, "/")
	if len(parts) >= 2 && parts[0] == "skills" {
		return parts[0] + "/" + parts[1]
	}
	return targetRel
}

// removePromptSectionEntry 移除 prompt 段条目（返回是否变更）。
// Python: action_executor.py:2139-2172 _remove_prompt_section_entry。
func removePromptSectionEntry(manifest, targetRel, sectionName string) bool {
	data := loadYAMLManifestAny(manifest)
	var entries []any
	key := "sections"
	if m, isMap := data.(map[string]any); isMap {
		if e, ok := m["sections"].([]any); ok {
			entries = e
		} else if e, ok := m["prompt_sections"].([]any); ok {
			entries = e
			key = "prompt_sections"
		}
	} else if list, isList := data.([]any); isList {
		entries = list
		data = map[string]any{"sections": entries}
	} else {
		entries = []any{}
		data = map[string]any{"sections": entries}
	}
	// Python: if not isinstance(entries, list): entries = [entries]
	var kept []any
	removed := false
	for _, entry := range entries {
		if promptEntryMatches(entry, targetRel, sectionName) {
			removed = true
			continue
		}
		kept = append(kept, entry)
	}
	if !removed {
		return false
	}
	if m, isMap := data.(map[string]any); isMap {
		m[key] = kept
		data = m
	}
	writeYAMLManifest(manifest, data)
	return true
}

// promptEntryMatches prompt 条目匹配（name 精确或 file 路径匹配）。
// Python: action_executor.py:2175-2187 _prompt_entry_matches。
func promptEntryMatches(entry any, targetRel, sectionName string) bool {
	entryMap, isMap := entry.(map[string]any)
	if !isMap {
		return false
	}
	name := pyStrOrEmpty(entryMap["name"])
	if sectionName != "" && name == sectionName {
		return true
	}
	fileValue := pyStrOrEmpty(entryMap["file"])
	if fileValue == "" {
		fileValue = pyStrOrEmpty(entryMap["path"])
	}
	if fileValue == "" {
		return false
	}
	return manifestPathMatches(fileValue, targetRel, false)
}

// removeManifestEntries 移除 manifest 条目。
// Python: action_executor.py:2190-2216 _remove_manifest_entries。
func removeManifestEntries(manifest, listKey string, targetPaths map[string]bool, exactOnly bool) bool {
	data := loadYAMLManifestAny(manifest)
	var entries []any
	if m, isMap := data.(map[string]any); isMap {
		if e, ok := m[listKey].([]any); ok {
			entries = e
		}
	} else if list, isList := data.([]any); isList {
		entries = list
		data = map[string]any{listKey: entries}
	} else {
		entries = []any{}
		data = map[string]any{listKey: entries}
	}
	var kept []any
	removed := false
	for _, entry := range entries {
		if manifestEntryMatches(entry, targetPaths, exactOnly) {
			removed = true
			continue
		}
		kept = append(kept, entry)
	}
	if !removed {
		return false
	}
	if m, isMap := data.(map[string]any); isMap {
		m[listKey] = kept
		data = m
	}
	writeYAMLManifest(manifest, data)
	return true
}

// manifestEntryMatches 条目匹配（str 或 dict 的 file/file_path/path）。
// Python: action_executor.py:2219-2232 _manifest_entry_matches。
func manifestEntryMatches(entry any, targetPaths map[string]bool, exactOnly bool) bool {
	if s, isStr := entry.(string); isStr {
		for target := range targetPaths {
			if manifestPathMatches(s, target, exactOnly) {
				return true
			}
		}
		return false
	}
	entryMap, isMap := entry.(map[string]any)
	if !isMap {
		return false
	}
	for _, key := range []string{"file", "file_path", "path"} {
		value := pyStrOrEmpty(entryMap[key])
		if value != "" {
			for target := range targetPaths {
				if manifestPathMatches(value, target, exactOnly) {
					return true
				}
			}
		}
	}
	return false
}

// manifestPathMatches 路径匹配（相等 / prompt_sections/files 前缀 / 目录包含）。
// Python: action_executor.py:2235-2243 _manifest_path_matches。
func manifestPathMatches(rawPath, targetRel string, exactOnly bool) bool {
	normalized := NormalizeRelPath(rawPath)
	target := NormalizeRelPath(targetRel)
	if normalized == target {
		return true
	}
	if exactOnly {
		return false
	}
	// Python: if not normalized.startswith(("skills/", "tools/", "rails/", "prompt_sections/")):
	//   prefixed = f"prompt_sections/files/{normalized}"; if prefixed == target: return True
	if !strings.HasPrefix(normalized, "skills/") && !strings.HasPrefix(normalized, "tools/") &&
		!strings.HasPrefix(normalized, "rails/") && !strings.HasPrefix(normalized, "prompt_sections/") {
		if "prompt_sections/files/"+normalized == target {
			return true
		}
	}
	return strings.HasPrefix(target, normalized+"/")
}

// loadYAMLManifestAny YAML manifest 装载（nil → 空 map）。
// Python: action_executor.py:2246-2249 _load_yaml_manifest。
func loadYAMLManifestAny(path string) any {
	content, err := os.ReadFile(path)
	if err != nil {
		return map[string]any{}
	}
	var data any
	if err := yaml.Unmarshal(content, &data); err != nil || data == nil {
		return map[string]any{}
	}
	return data
}

// writeYAMLManifest YAML manifest 写回。
// Python: action_executor.py:2252-2256 _write_yaml_manifest。
func writeYAMLManifest(path string, data any) {
	os.MkdirAll(filepath.Dir(path), 0o755)
	out, err := yaml.Marshal(data)
	if err != nil {
		return
	}
	os.WriteFile(path, out, 0o644)
}

// appendManifestEntry 追加/替换 manifest 条目。
// Python: action_executor.py:2083-2090 _append_manifest_entry。
func appendManifestEntry(manifest, listKey string, entry map[string]any) {
	var data map[string]any
	if info, err := os.Stat(manifest); err == nil && !info.IsDir() {
		if loaded := loadYAMLManifestAny(manifest); loaded != nil {
			if m, isMap := loaded.(map[string]any); isMap {
				data = m
			}
		}
	}
	if data == nil {
		data = map[string]any{}
	}
	var entries []any
	if e, ok := data[listKey].([]any); ok {
		entries = e
	}
	// Python: normalized_entry_file 去重（file/file_path 任一匹配即移除旧条目）
	normalizedEntryFile := NormalizeRelPath(pyStrOrEmpty(entry["file"]))
	var kept []any
	for _, item := range entries {
		itemMap, isMap := item.(map[string]any)
		if isMap {
			itemFile := pyStrOrEmpty(itemMap["file"])
			if itemFile == "" {
				itemFile = pyStrOrEmpty(itemMap["file_path"])
			}
			if NormalizeRelPath(itemFile) == normalizedEntryFile {
				continue
			}
		}
		kept = append(kept, item)
	}
	kept = append(kept, entry)
	data[listKey] = kept
	writeYAMLManifest(manifest, data)
}

// intOrDefault 整数转换（bool 拒绝；失败回落默认）。
// Python: action_executor.py:2083-2090 _int_or_default。
func intOrDefault(value any, defaultVal int) int {
	if _, isBool := value.(bool); isBool {
		return defaultVal
	}
	if f, ok := value.(float64); ok && f == float64(int(f)) {
		return int(f)
	}
	if n, ok := value.(int); ok {
		return n
	}
	if parsed, err := strconvPy(pyStrOrEmpty(value)); err == nil {
		return parsed
	}
	return defaultVal
}

// strconvPy 简易整数解析。
func strconvPy(text string) (int, error) {
	n := 0
	negative := false
	for i, c := range text {
		if i == 0 && c == '-' {
			negative = true
			continue
		}
		if c < '0' || c > '9' {
			return 0, fmt.Errorf("not an int")
		}
		n = n*10 + int(c-'0')
	}
	if negative {
		n = -n
	}
	return n, nil
}

// splitLinesPy Python splitlines 等价。
func splitLinesPy(content string) []string {
	content = strings.ReplaceAll(content, "\r\n", "\n")
	content = strings.ReplaceAll(content, "\r", "\n")
	return strings.Split(content, "\n")
}

// ──────────────────────────── 结构体（agent） ────────────────────────────

// MemberActionExecutorAgent 隔离 worktree 中单动作的 DeepAgent 执行器。
// Python: action_executor.py:691-1070 MemberActionExecutorAgent。
type MemberActionExecutorAgent struct {
	modelConfigRef            string
	agentSkillsDirs           []string
	outputRepair              harnessrsi.OutputRepairConfig
	runtimeIterationHardLimit int
	prebuiltAgent             *agentface.Agent
}

// NewMemberActionExecutorAgent 构造（Python __init__）。
func NewMemberActionExecutorAgent(modelConfigRef string, agentSkillsDirs []string, outputRepair *harnessrsi.OutputRepairConfig, runtimeIterationHardLimit int) *MemberActionExecutorAgent {
	repair := harnessrsi.DefaultOutputRepairConfig()
	if outputRepair != nil {
		repair = *outputRepair
	}
	if runtimeIterationHardLimit <= 0 {
		runtimeIterationHardLimit = 300
	}
	return &MemberActionExecutorAgent{
		modelConfigRef:            modelConfigRef,
		agentSkillsDirs:           append([]string{}, agentSkillsDirs...),
		outputRepair:              repair,
		runtimeIterationHardLimit: runtimeIterationHardLimit,
	}
}

// ExecuteAction 在给定 worktree 中执行单动作。
// Python: action_executor.py:706-815 execute_action。
func (a *MemberActionExecutorAgent) ExecuteAction(ctx context.Context, actionWorktree string, action MemberOptimizationAction, planSummary string, allowedSkills, allowedTools []string) (map[string]any, error) {
	// Python: declared_paths = resolve_declared_paths(action)
	declaredPaths := actiongroups.ResolveDeclaredPaths(actiongroups.SchedAction{
		ActionID:           action.ActionID,
		TargetPath:         action.TargetPath,
		DeclaredWritePaths: action.DeclaredWritePaths,
	})
	// Python: target = (action_worktree / action.target_path).resolve(); modify 基线校验
	if action.Operation == "modify" {
		target := filepath.Join(actionWorktree, action.TargetPath)
		if _, err := os.Stat(target); err != nil {
			return map[string]any{
				"status": "failed", "declared_write_paths": declaredPaths,
				"error": fmt.Sprintf("modify baseline mismatch: target does not exist: %s", action.TargetPath),
			}, nil
		}
	}
	// Python: before = _snapshot_files(action_worktree)
	before := SnapshotFiles(actionWorktree, nil)
	// Python: user_message = self._build_user_message(...)
	userMessage := BuildExecutorUserMessage(action, planSummary, allowedSkills, allowedTools, actionWorktree)
	// Python: ceiling / REGISTRY_GUIDANCE 追加
	userMessage += fmt.Sprintf("\nHost runtime iteration ceiling: %d; runtime.yaml cannot change it.", a.runtimeIterationHardLimit)
	userMessage += "\n\n" + RegistryGuidance
	message := userMessage
	lastResponseText := ""
	lastError := ""
	// Python: tracker = FeedbackTracker(self._output_repair)
	tracker := harnessrsi.NewFeedbackTracker(a.outputRepair, nil)
	// Python: agent = create_action_execution_agent(...); async with optimizer_agent_resources(agent):
	agent, err := CreateExecutionAgentForExecutor(ctx, ExecutorAgentParams{
		ModelConfigRef:     a.modelConfigRef,
		Workspace:          actionWorktree,
		AgentSkillsDirs:    a.agentSkillsDirs,
		DeclaredWritePaths: declaredPaths,
	})
	if err != nil {
		return map[string]any{"status": "failed", "declared_write_paths": declaredPaths, "error": err.Error()}, nil
	}
	defer func() {
		if agent.Release != nil {
			_ = agent.Release()
		}
	}()
	// Python: for attempt in range(tracker.max_attempts):
	for attempt := 0; attempt < tracker.MaxAttempts; attempt++ {
		// Python: session = Session(session_id=f"action_exec_{action_id}_{attempt}", card=...)
		// Go: 经 bridge invokeFn（BaseAgent 通道，session_id 传入）
		response, invokeErr := bridgeInvokeAgent(ctx, agent, message, fmt.Sprintf("action_exec_%s_%d", action.ActionID, attempt))
		if invokeErr != nil {
			// Python: except Exception: is_retryable_model_call_failure → 重试；否则失败返回
			if harnessrsi.IsRetryableModelCallFailure(invokeErr) {
				lastError = invokeErr.Error()
				if !tracker.Failed(lastError, snapshotRelPaths(actionWorktree, declaredPaths)) {
					break
				}
				message = BuildExecutorRetryMessage(userMessage, lastResponseText, lastError)
				continue
			}
			return map[string]any{
				"status": "failed", "declared_write_paths": declaredPaths,
				"response_text": lastResponseText, "error": invokeErr.Error(),
			}, nil
		}
		responseText := agentsBridgeExtractText(response)
		lastResponseText = responseText
		parsed, parseErr := parseExecutorActionResponse(responseText)
		if parseErr != nil {
			lastError = parseErr.Error()
			if !tracker.Failed(lastError, snapshotRelPaths(actionWorktree, declaredPaths)) {
				break
			}
			message = BuildExecutorRetryMessage(userMessage, lastResponseText, lastError)
			continue
		}
		errors := []string{}
		if list, ok := parsed["errors"].([]any); ok {
			for _, item := range list {
				if text := pyStrOrEmpty(item); strings.TrimSpace(text) != "" {
					errors = append(errors, text)
				}
			}
		}
		if pyStrOrEmpty(parsed["status"]) != "succeeded" {
			// Python: message = "; ".join(errors) or "agent reported failed status..."
			reason := strings.Join(errors, "; ")
			if reason == "" {
				reason = "agent reported failed status; inspect and correct the action"
			}
			lastError = reason
			if !tracker.Failed(lastError, snapshotRelPaths(actionWorktree, declaredPaths)) {
				break
			}
			message = BuildExecutorRetryMessage(userMessage, lastResponseText, lastError)
			continue
		}
		// Python: if parsed.get("file_writes"): _apply_structured_file_writes(...)
		if parsed["file_writes"] != nil {
			if _, writeErr := applyStructuredFileWrites(actionWorktree, action, declaredPaths, parsed["file_writes"]); writeErr != nil {
				lastError = writeErr.Error()
				if !tracker.Failed(lastError, snapshotRelPaths(actionWorktree, declaredPaths)) {
					break
				}
				message = BuildExecutorRetryMessage(userMessage, lastResponseText, lastError)
				continue
			}
		}
		// Python: changed = _changed_files(before, _snapshot_files(action_worktree))
		changed := ChangedFiles(before, SnapshotFiles(actionWorktree, nil))
		// Python: outside = [...]; if outside: raise ValueError(...)
		var outside []string
		for _, path := range changed {
			if !actionGroupsPackageWriteAllowed(path, declaredPaths) && !IsRuntimeWorkspaceMetadata(path) {
				outside = append(outside, path)
			}
		}
		if len(outside) > 0 {
			lastError = fmt.Sprintf("changed files outside declared_write_paths: %v", outside)
			if !tracker.Failed(lastError, snapshotRelPaths(actionWorktree, declaredPaths)) {
				break
			}
			message = BuildExecutorRetryMessage(userMessage, lastResponseText, lastError)
			continue
		}
		// Python: written_files = [...]; if not written_files: raise ValueError(...)
		var writtenFiles []string
		for _, path := range changed {
			if actionGroupsPackageWriteAllowed(path, declaredPaths) {
				writtenFiles = append(writtenFiles, path)
			}
		}
		if len(writtenFiles) == 0 {
			lastError = "agent reported success but no declared_write_paths changed"
			if !tracker.Failed(lastError, snapshotRelPaths(actionWorktree, declaredPaths)) {
				break
			}
			message = BuildExecutorRetryMessage(userMessage, lastResponseText, lastError)
			continue
		}
		// Python: written_files = self._sync_action_registries(...)
		writtenFiles, syncErr := syncActionRegistries(actionWorktree, action, declaredPaths, writtenFiles)
		if syncErr != nil {
			lastError = syncErr.Error()
			if !tracker.Failed(lastError, snapshotRelPaths(actionWorktree, declaredPaths)) {
				break
			}
			message = BuildExecutorRetryMessage(userMessage, lastResponseText, lastError)
			continue
		}
		// Python: validation_errors = _validate_generated_action_resources(...); if: raise
		validationErrors := validateGeneratedActionResources(actionWorktree, action, writtenFiles, a.runtimeIterationHardLimit)
		if len(validationErrors) > 0 {
			lastError = strings.Join(validationErrors, "; ")
			if !tracker.Failed(lastError, snapshotRelPaths(actionWorktree, declaredPaths)) {
				break
			}
			message = BuildExecutorRetryMessage(userMessage, lastResponseText, lastError)
			continue
		}
		// Python: return {...succeeded...}
		return map[string]any{
			"status":               "succeeded",
			"declared_write_paths": declaredPaths,
			"changed_files":        toAnySlice(writtenFiles),
			"response_text":        responseText,
			"error":                "",
		}, nil
	}
	// Python: return {...failed 兜底（含 output_repair 元数据）...}
	return map[string]any{
		"status":               "failed",
		"declared_write_paths": declaredPaths,
		"response_text":        lastResponseText,
		"error":                lastError,
		"output_repair": map[string]any{
			"attempts":    tracker.Attempts,
			"stop_reason": tracker.StopReason,
		},
	}, nil
}

// parseExecutorActionResponse 动作响应解析。
// Python: action_executor.py:817-831 _parse_action_response。
func parseExecutorActionResponse(responseText string) (map[string]any, error) {
	parseFn := agentsBridge.ParseJSONObject
	if parseFn == nil {
		// 测试进程未 import agents 子包（blank import 成环）——本地 JSON 解析兜底
		parseFn = parseJSONObjectLocal
	}
	parsed, err := parseFn(responseText)
	if err != nil {
		return nil, fmt.Errorf("agent response must be a structured file_writes JSON object: %v", err)
	}
	// Python: if "status" not in parsed: raise
	if _, has := parsed["status"]; !has {
		return nil, fmt.Errorf("agent response must be a structured file_writes JSON object")
	}
	// Python: if parsed["status"] not in ("succeeded", "failed"): raise
	if parsed["status"] != "succeeded" && parsed["status"] != "failed" {
		return nil, fmt.Errorf("status must be 'succeeded' or 'failed'")
	}
	// Python: if not isinstance(parsed.get("errors", []), list): raise
	if errors, has := parsed["errors"]; has {
		if _, isList := errors.([]any); !isList {
			return nil, fmt.Errorf("errors must be a list of strings (use [] for no errors)")
		}
		// Python: if any(not isinstance(item, str) for item in ...): raise
		for _, item := range errors.([]any) {
			if _, isStr := item.(string); !isStr {
				return nil, fmt.Errorf("errors must contain strings")
			}
		}
	}
	return parsed, nil
}

// applyStructuredFileWrites 结构化文件写入（legacy 通道）。
// Python: action_executor.py:833-887 _apply_structured_file_writes。
func applyStructuredFileWrites(actionWorktree string, action MemberOptimizationAction, declaredPaths []string, fileWrites any) ([]string, error) {
	writes, isList := fileWrites.([]any)
	if !isList {
		return nil, fmt.Errorf("file_writes must be a list")
	}
	var writtenFiles []string
	root, _ := filepath.Abs(actionWorktree)
	for index, item := range writes {
		itemMap, isMap := item.(map[string]any)
		if !isMap {
			return nil, fmt.Errorf("file_writes[%d] must be an object", index)
		}
		// Python: rel_path = str(item.get("path", "")).strip(); if not: raise
		relPath := strings.TrimSpace(pyStrOrEmpty(itemMap["path"]))
		if relPath == "" {
			return nil, fmt.Errorf("file_writes[%d].path is required", index)
		}
		normalized := NormalizeRelPath(relPath)
		// Python: if is_absolute or ".." in parts: raise
		if strings.HasPrefix(relPath, "/") || containsDotDotPath(normalized) {
			return nil, fmt.Errorf("file_writes[%d].path must be a safe relative path", index)
		}
		// Python: if not package_write_allowed(normalized, declared_paths): raise
		if !actionGroupsPackageWriteAllowed(normalized, declaredPaths) {
			return nil, fmt.Errorf("file_writes[%d].path %s outside declared_write_paths %v", index, normalized, declaredPaths)
		}
		// Python: if "content" not in item: raise
		contentValue, hasContent := itemMap["content"]
		if !hasContent {
			return nil, fmt.Errorf("file_writes[%d].content is required", index)
		}
		content, isStr := contentValue.(string)
		if !isStr {
			return nil, fmt.Errorf("file_writes[%d].content must be a string", index)
		}
		// Python: skill frontmatter 规范化与契约校验
		skillName := SkillNameFromSkillMdPath(normalized)
		if action.ActionGroup == "skill" && skillName != "" {
			content = normalizeSkillFrontmatterName(content, skillName, skillTriggerDescription(action))
			if action.Operation == "add" {
				if err := ValidateGeneratedSkillContract(content); err != nil {
					return nil, err
				}
			}
		}
		target := filepath.Join(root, normalized)
		absTarget, _ := filepath.Abs(target)
		// Python: try: target.relative_to(root) except: raise escapes
		if !withinDir(root, absTarget) {
			return nil, fmt.Errorf("file_writes[%d].path %s escapes action worktree", index, normalized)
		}
		// Python: if target.is_file() and len(read) > 12000: raise
		if info, err := os.Stat(absTarget); err == nil && !info.IsDir() {
			if existing, readErr := os.ReadFile(absTarget); readErr == nil && len(existing) > 12000 {
				return nil, fmt.Errorf("legacy file_writes cannot replace a truncated baseline; use native read/edit tools")
			}
		}
		if err := os.MkdirAll(filepath.Dir(absTarget), 0o755); err != nil {
			return nil, err
		}
		if err := os.WriteFile(absTarget, []byte(content), 0o644); err != nil {
			return nil, err
		}
		writtenFiles = append(writtenFiles, normalized)
	}
	// Python: return sorted(set(written_files))
	return sortedUniqueStrings(writtenFiles), nil
}

// syncActionRegistries 动作登记簿同步。
// Python: action_executor.py:889-930 _sync_action_registries。
func syncActionRegistries(actionWorktree string, action MemberOptimizationAction, declaredPaths []string, writtenFiles []string) ([]string, error) {
	root := actionWorktree
	// Python: if prompt and operation in {add, modify}: upsert_prompt_section
	if action.ActionGroup == "prompt" && (action.Operation == "add" || action.Operation == "modify") {
		targetRel := NormalizeRelPath(action.TargetPath)
		manifestRel := "prompt_sections/sections.yaml"
		declaredSet := map[string]bool{}
		for _, path := range writtenFiles {
			declaredSet[path] = true
		}
		if declaredSet[targetRel] && strings.HasPrefix(targetRel, "prompt_sections/files/") {
			if !PathAllowedByDeclared(manifestRel, declaredPaths) {
				return nil, fmt.Errorf("prompt/add requires declared manifest %s", manifestRel)
			}
			name := pyStrOrEmpty(action.Constraints["section_name"])
			if name == "" {
				name = trimExt(filepath.Base(targetRel))
			}
			var priority *int
			if _, has := action.Constraints["priority"]; has {
				p := intOrDefault(action.Constraints["priority"], 30)
				priority = &p
			}
			if err := UpsertPromptSection(root, name, targetRel, priority, declaredPaths, action.Operation == "modify"); err != nil {
				return nil, err
			}
			writtenFiles = sortedUniqueStrings(append(writtenFiles, manifestRel))
		}
	}
	// Python: written_files = _normalize_skill_frontmatter_name_for_written_files(...)
	writtenFiles = normalizeSkillFrontmatterNameForWrittenFiles(root, action, writtenFiles)
	// Python: written_files = _sync_skill_registry_for_written_files(...)
	writtenFiles, err := syncSkillRegistryForWrittenFiles(root, action, declaredPaths, writtenFiles)
	if err != nil {
		return nil, err
	}
	// Python: report = normalize_package_registries(root, writable_paths=declared_paths); if issues: raise
	report := NormalizePackageRegistries(root, declaredPaths)
	if len(report.Issues) > 0 {
		var msgs []string
		for _, issue := range report.Issues {
			msgs = append(msgs, issue.Describe())
		}
		return nil, fmt.Errorf("%s", strings.Join(msgs, "; "))
	}
	// Python: return sorted(set([*written_files, *report.changed_files]))
	return sortedUniqueStrings(append(writtenFiles, report.ChangedFiles...)), nil
}

// BuildExecutorRetryMessage 构造重试消息。
// Python: action_executor.py:943-963 _build_retry_message。
func BuildExecutorRetryMessage(originalMessage, previousResponse, errText string) string {
	return fmt.Sprintf("%s\n\n## Previous Invalid Output\n\n%s\n\n## Validation Error\n\n%s\n\n"+
		"Use the native tools to inspect the current files and correct the validation\n"+
		"failure. Read remaining lines before replacing any existing file. Then return\n"+
		"JSON with status, changed_files, and errors; the controller verifies real writes.\n",
		originalMessage, previousResponse, errText)
}

// ──────────────────────────── 非导出函数（桥接辅助） ────────────────────────────

// parseJSONObjectLocal 本地 JSON 对象解析（桥未注册时的兜底）。
func parseJSONObjectLocal(text string) (map[string]any, error) {
	var m map[string]any
	if err := json.Unmarshal([]byte(strings.TrimSpace(text)), &m); err != nil {
		return nil, err
	}
	return m, nil
}

// bridgeInvokeAgent 经 agents 桥的 invokeFn 通道调用执行 agent。
// Python: agent.invoke(inputs={"query": message}, session=Session(session_id, card))。
func bridgeInvokeAgent(ctx context.Context, agent *agentface.Agent, message, sessionID string) (map[string]any, error) {
	return agentsBridge.InvokeAgent(ctx, agent, message, sessionID)
}

// agentsBridgeExtractText 提取 agent 响应文本。
func agentsBridgeExtractText(response map[string]any) string {
	if agentsBridge.ExtractText != nil {
		return agentsBridge.ExtractText(response)
	}
	// 桥未注册时的兜底：text 字段直取（fake InvokeAgent 的响应形态）
	if text, ok := response["text"].(string); ok {
		return text
	}
	return ""
}

// actionGroupsPackageWriteAllowed 父包侧包写允许（D-c2-5：核心落父包，agents 包装转调）。
func actionGroupsPackageWriteAllowed(path string, declaredPaths []string) bool {
	return PackageWriteAllowedCore(path, declaredPaths, false)
}

// snapshotRelPaths 声明路径子集快照。
func snapshotRelPaths(root string, declaredPaths []string) map[string]string {
	return SnapshotFiles(root, declaredPaths)
}

// mutexGuard 编排器并发保护占位（结果收集经索引切片无需锁）。
var mutexGuard sync.Mutex

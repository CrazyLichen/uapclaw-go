// action_executor.go member 动作执行——三层并行 + 隔离 worktree + 串行合并。
// Python: member_optimizer/action_executor.py（全量对齐，2,243 行）。
// 全局波串行 / 同波角色 worker 有界并行 / 角色内子波按写路径互斥串行。
// feat_009 rule.md §3.7 与 design.md §4.9。

package memberoptimizer

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// ──────────────────────────── 常量 ────────────────────────────

// skillSuffixForbidden 禁用技能脚本后缀。
// Python: verification.py:104 _FORBIDDEN_SKILL_SUFFIXES（executor 资源校验复用）。
var skillSuffixForbidden = map[string]bool{
	".bat": true, ".cmd": true, ".dll": true, ".dylib": true,
	".exe": true, ".ps1": true, ".sh": true, ".so": true,
}

// ──────────────────────────── 结构体 ────────────────────────────

// actionExecutionContext 单动作执行上下文。
// Python: action_executor.py:69-77 _ActionExecutionContext。
type actionExecutionContext struct {
	integrationDir string
	runDir         string
	worktreesDir   string
	waveIndex      int
	modelConfigRef string
	plan           *MemberOptimizationPlan
}

// subwaveMergeRequest 子波合并请求。
// Python: action_executor.py:79-86 _SubwaveMergeRequest。
type subwaveMergeRequest struct {
	role           string
	waveIndex      int
	subwaveIndex   int
	results        []*MemberActionExecutionResult
	integrationDir string
	runDir         string
}

// ──────────────────────────── 导出函数 ────────────────────────────

// NormalizeRelPath 相对路径归一（posix、去首尾斜杠）。
// Python: action_executor.py:88-90 _normalize_rel_path。
func NormalizeRelPath(path string) string {
	return strings.Trim(filepath.ToSlash(filepath.Clean(strings.ReplaceAll(path, "\\", "/"))), "/")
}

// SnapshotFiles 快照 root 下或选定相对路径下的文件（sha256）。
// Python: action_executor.py:92-109 _snapshot_files。
func SnapshotFiles(root string, relPaths []string) map[string]string {
	// Python: roots = rel_paths or ["."]
	roots := relPaths
	if len(roots) == 0 {
		roots = []string{"."}
	}
	snapshot := map[string]string{}
	for _, relPath := range roots {
		candidate := filepath.Join(root, relPath)
		info, err := os.Stat(candidate)
		if err != nil {
			continue
		}
		// Python: if candidate.is_file(): snapshot[rel] = hash
		if !info.IsDir() {
			rel, relErr := filepath.Rel(root, candidate)
			if relErr != nil {
				continue
			}
			if hash, err := hashFile(candidate); err == nil {
				snapshot[NormalizeRelPath(rel)] = hash
			}
			continue
		}
		// Python: if candidate.is_dir(): rglob sorted
		var files []string
		filepath.Walk(candidate, func(path string, info os.FileInfo, err error) error {
			if err == nil && !info.IsDir() {
				files = append(files, path)
			}
			return nil
		})
		sort.Strings(files)
		for _, filePath := range files {
			rel, relErr := filepath.Rel(root, filePath)
			if relErr != nil {
				continue
			}
			if hash, err := hashFile(filePath); err == nil {
				snapshot[NormalizeRelPath(rel)] = hash
			}
		}
	}
	return snapshot
}

// CanonicalSkillIdentifier 包内技能标识符（目录名与 frontmatter 共用）。
// Python: action_executor.py:115-129 _canonical_skill_identifier。
func CanonicalSkillIdentifier(value string) string {
	// Python: text = str(value or "").replace("\\", "/").strip().split("/")[-1]
	text := strings.ReplaceAll(value, "\\", "/")
	parts := strings.Split(strings.TrimSpace(text), "/")
	text = parts[len(parts)-1]
	// Python: for char: alnum → lower；_-. → 单下划线
	var chars []string
	previousUnderscore := false
	for _, char := range text {
		isAlnum := (char >= 'a' && char <= 'z') || (char >= 'A' && char <= 'Z') || (char >= '0' && char <= '9')
		if isAlnum {
			chars = append(chars, strings.ToLower(string(char)))
			previousUnderscore = false
			continue
		}
		if (char == '_' || char == '-' || char == ' ' || char == '.') && !previousUnderscore {
			chars = append(chars, "_")
			previousUnderscore = true
		}
	}
	// Python: return "".join(chars).strip("_") or "member_skill"
	out := strings.Trim(strings.Join(chars, ""), "_")
	if out == "" {
		return "member_skill"
	}
	return out
}

// SkillNameFromSkillMdPath 从 SKILL.md 路径解析技能名。
// Python: action_executor.py:131-137 _skill_name_from_skill_md_path。
func SkillNameFromSkillMdPath(path string) string {
	normalized := NormalizeRelPath(path)
	parts := strings.Split(normalized, "/")
	// Python: if len(parts) >= 3 and parts[0] == "skills" and parts[-1] == "SKILL.md"
	if len(parts) >= 3 && parts[0] == "skills" && parts[len(parts)-1] == "SKILL.md" {
		return CanonicalSkillIdentifier(parts[len(parts)-2])
	}
	return ""
}

// ChangedFiles 前后快照差异清单（排序）。
// Python: action_executor.py:147-150 _changed_files。
func ChangedFiles(before, after map[string]string) []string {
	// Python: all_paths = set(before) | set(after); sorted(diff)
	all := map[string]bool{}
	for path := range before {
		all[path] = true
	}
	for path := range after {
		all[path] = true
	}
	var out []string
	for path := range all {
		if before[path] != after[path] {
			out = append(out, path)
		}
	}
	sort.Strings(out)
	return out
}

// ShouldRunAction 依赖条件下动作是否运行。
// Python: action_executor.py:317-339 _should_run_action。
func ShouldRunAction(action MemberOptimizationAction, actionStatusByID map[string]string) (bool, string) {
	// Python: if not action.depends_on or action.run_if == "always": return True, ""
	if len(action.DependsOn) == 0 || action.RunIf == "always" {
		return true, ""
	}
	// Python: dependency_statuses = {...}; missing = [...]
	dependencyStatuses := map[string]string{}
	for _, dep := range action.DependsOn {
		dependencyStatuses[dep] = actionStatusByID[dep]
	}
	var missing []string
	for dep, status := range dependencyStatuses {
		if status == "" {
			missing = append(missing, dep)
		}
	}
	if len(missing) > 0 {
		sort.Strings(missing)
		return false, fmt.Sprintf("dependency status unavailable: %v", missing)
	}
	// Python: failed = [...]
	var failed []string
	for dep, status := range dependencyStatuses {
		if status == "failed" {
			failed = append(failed, dep)
		}
	}
	// Python: if action.run_if == "dependency_failed": ...
	if action.RunIf == "dependency_failed" {
		if len(failed) > 0 {
			return true, ""
		}
		return false, "dependency did not fail"
	}
	if len(failed) > 0 {
		sort.Strings(failed)
		return false, fmt.Sprintf("dependency failed: %v", failed)
	}
	return true, ""
}

// PathAllowedByDeclared 路径是否被声明写路径覆盖。
// Python: action_executor.py:341-350 _path_allowed_by_declared。
func PathAllowedByDeclared(path string, declaredPaths []string) bool {
	normalized := NormalizeRelPath(path)
	for _, declared := range declaredPaths {
		declaredNorm := NormalizeRelPath(declared)
		if declaredNorm == "" {
			continue
		}
		if normalized == declaredNorm || strings.HasPrefix(normalized, declaredNorm+"/") {
			return true
		}
	}
	return false
}

// IsRuntimeWorkspaceMetadata 运行时工作区元数据判定（不计越界）。
// Python: action_executor.py:352-361 _is_runtime_workspace_metadata。
func IsRuntimeWorkspaceMetadata(path string) bool {
	normalized := NormalizeRelPath(path)
	// Python: if normalized in {...五文件...}: return True
	switch normalized {
	case "AGENT.md", "SOUL.md", "IDENTITY.md", "USER.md", "HEARTBEAT.md":
		return true
	}
	parts := strings.Split(normalized, "/")
	// Python: parts[0] in {...} and parts[-1] == ".workspace"
	first := map[string]bool{"agents": true, "context": true, "memory": true, "messages": true, "skills": true, "todo": true}
	if len(parts) > 0 && first[parts[0]] && parts[len(parts)-1] == ".workspace" {
		return true
	}
	// Python: parts[0] in {"context", "memory"} and len(parts) > 1
	if len(parts) > 1 && (parts[0] == "context" || parts[0] == "memory") {
		return true
	}
	return false
}

// SanitizeRuntimeSemanticText 去除评估器标识符、保留语义教训。
// Python: action_executor.py:555-606 _sanitize_runtime_semantic_text。
func SanitizeRuntimeSemanticText(value any) string {
	text := strings.TrimSpace(pyStrOrEmpty(value))
	if text == "" {
		return ""
	}
	// Python: ^(member_harness|team_skill)\.[^. ]+\.[^:]+:\s* 前缀剥离
	prefixRE := regexp.MustCompile(`(?i)^(?:member_harness|team_skill)\.[^.\s]+\.[^:]+:\s*`)
	text = prefixRE.ReplaceAllString(text, "")
	// Python: _TRACE_PROVENANCE_SUFFIX
	traceSuffixRE := regexp.MustCompile(`(?i)\s*\((?:trace_id|role|message_index|step(?:_pointer)?)=.*?\)\. ?`)
	text = traceSuffixRE.ReplaceAllString(text, ". ")
	// Python: pattern[0]: module.py::qualifier → "the observed check"
	marker0 := regexp.MustCompile(`\b[A-Za-z0-9_./-]+\.py::[A-Za-z0-9_.:\[\]-]+\b`)
	text = marker0.ReplaceAllString(text, "the observed check")
	// Python: verifier (test|check) test_xxx → "the observed contract"
	text = regexp.MustCompile(`(?i)\bverifier\s+(?:test|check)\s+test_[A-Za-z0-9_]+\b`).ReplaceAllString(text, "the observed contract")
	// Python: (test|check) test_xxx → "the observed check"
	text = regexp.MustCompile(`(?i)\b(?:test|check)\s+test_[A-Za-z0-9_]+\b`).ReplaceAllString(text, "the observed check")
	// Python: test[_ -]patch → "supplied acceptance-test contract"
	text = regexp.MustCompile(`(?i)\btest[_ -]patch\b`).ReplaceAllString(text, "supplied acceptance-test contract")
	// Python: pattern[1]: test_xxx → "the observed check"
	marker1 := regexp.MustCompile(`\btest_[A-Za-z0-9_]+\b`)
	text = marker1.ReplaceAllString(text, "the observed check")
	// Python: pattern[2]: xx__yy-123 → "the task"
	marker2 := regexp.MustCompile(`\b[A-Za-z0-9_.-]+__[A-Za-z0-9_.-]+-\d+\b`)
	text = marker2.ReplaceAllString(text, "the task")
	// Python: pattern[3]: (step|trace)_\d+ → ""
	marker3 := regexp.MustCompile(`\b(?:step|trace)_\d+\b`)
	text = marker3.ReplaceAllString(text, "")
	// Python: replacements 三项
	for _, pair := range [][2]string{
		{"SWE-bench", "authoritative evaluation"},
		{"FAIL_TO_PASS", "required checks"},
		{"PASS_TO_PASS", "regression checks"},
	} {
		text = regexp.MustCompile(`(?i)`+regexp.QuoteMeta(pair[0])).ReplaceAllString(text, pair[1])
	}
	// Python: \b(hyp|issue)_[A-Za-z0-9_]+\b → ""
	text = regexp.MustCompile(`(?i)\b(?:hyp|issue)_[A-Za-z0-9_]+\b`).ReplaceAllString(text, "")
	// Python: hidden[- ]tests? → "unseen checks"
	text = regexp.MustCompile(`(?i)\bhidden[- ]tests?\b`).ReplaceAllString(text, "unseen checks")
	// Python: verifier[_ -]result → "evaluation result"
	text = regexp.MustCompile(`(?i)\bverifier[_ -]result\b`).ReplaceAllString(text, "evaluation result")
	// Python: verifier (test|check) → "observed contract"
	text = regexp.MustCompile(`(?i)\bverifier\s+(?:test|check)\b`).ReplaceAllString(text, "observed contract")
	// Python: (official|authoritative )?verifier → "acceptance evaluation"
	text = regexp.MustCompile(`(?i)\b(?:(?:official|authoritative)\s+)?verifier\b`).ReplaceAllString(text, "acceptance evaluation")
	// Python: " ".join(text.split()).strip(" ,;.")
	return strings.Trim(strings.Join(strings.Fields(text), " "), " ,;.")
}

// RuntimeContractProjection 优化假设 → 公共运行时创作边界投影。
// Python: action_executor.py:478-542 _runtime_contract_projection。
func RuntimeContractProjection(action MemberOptimizationAction) []map[string]any {
	// Python: raw_contracts = action.constraints.get("optimization_contracts", [])
	rawContracts, _ := action.Constraints["optimization_contracts"].([]any)
	if action.Constraints["optimization_contracts"] != nil && rawContracts == nil {
		return []map[string]any{}
	}
	var projected []map[string]any
	for _, raw := range rawContracts {
		rawMap, isMap := raw.(map[string]any)
		if !isMap {
			continue
		}
		var tasks []string
		// Python: for trigger in raw.get("public_trigger", []): cleaned = sanitize(task)
		if triggers, ok := rawMap["public_trigger"].([]any); ok {
			for _, trigger := range triggers {
				triggerMap, isMap := trigger.(map[string]any)
				if !isMap {
					continue
				}
				cleaned := SanitizeRuntimeSemanticText(triggerMap["task"])
				if cleaned != "" {
					tasks = append(tasks, cleaned)
				}
			}
		}
		// Python: forbidden 归一
		forbidden := rawMap["forbidden_behavior"]
		if s, isStr := forbidden.(string); isStr {
			forbidden = []any{s}
		}
		forbiddenList, isList := forbidden.([]any)
		if !isList {
			forbiddenList = []any{}
		}
		requiredBehavior := SanitizeRuntimeSemanticText(rawMap["required_behavior"])
		var forbiddenBehavior []string
		for _, item := range forbiddenList {
			cleaned := SanitizeRuntimeSemanticText(item)
			if cleaned != "" {
				forbiddenBehavior = append(forbiddenBehavior, cleaned)
			}
		}
		decisionContract := runtimeDecisionContractProjection(rawMap["decision_contract"])
		projectedContract := map[string]any{
			"public_tasks":       toAnySlice(tasks),
			"required_behavior":  requiredBehavior,
			"forbidden_behavior": toAnySlice(forbiddenBehavior),
		}
		// Python: has_decision_contract = any(value not in (None, "", []))
		hasDecisionContract := false
		for _, value := range decisionContract {
			if !isEmptyPyValue(value) {
				hasDecisionContract = true
				break
			}
		}
		if hasDecisionContract {
			projectedContract["decision_contract"] = decisionContract
		}
		projected = append(projected, projectedContract)
	}
	return projected
}

// BuildDeclaredFileContext 渲染声明文件的当前内容（执行模型上下文）。
// Python: action_executor.py:637-678 _build_declared_file_context。
func BuildDeclaredFileContext(actionWorktree string, declaredPaths []string, maxCharsPerFile int) string {
	// Python: if action_worktree is None: return "No action worktree was provided."
	if actionWorktree == "" {
		return "No action worktree was provided."
	}
	if maxCharsPerFile <= 0 {
		maxCharsPerFile = 12000
	}
	root, _ := filepath.Abs(actionWorktree)
	var sections []string
	for _, relPath := range declaredPaths {
		normalized := NormalizeRelPath(relPath)
		if normalized == "" {
			continue
		}
		candidate := filepath.Join(root, normalized)
		absCandidate, _ := filepath.Abs(candidate)
		// Python: try: candidate.relative_to(root) except ValueError: 越界提示
		if !withinDir(root, absCandidate) {
			sections = append(sections, "### "+normalized+"\n\nPath is outside the action worktree and cannot be read.")
			continue
		}
		info, err := os.Stat(absCandidate)
		if err != nil {
			sections = append(sections, "### "+normalized+"\n\nFile does not exist.")
			continue
		}
		// Python: if candidate.is_dir(): 目录遍历（前 20 文件截断）
		if info.IsDir() {
			var files []string
			filepath.Walk(absCandidate, func(path string, f os.FileInfo, e error) error {
				if e == nil && !f.IsDir() {
					files = append(files, path)
				}
				return nil
			})
			sort.Strings(files)
			if len(files) == 0 {
				sections = append(sections, "### "+normalized+"/\n\nDirectory exists and is empty.")
				continue
			}
			for _, filePath := range files[:intMin(len(files), 20)] {
				rel, _ := filepath.Rel(root, filePath)
				sections = append(sections, renderFileContext(filePath, NormalizeRelPath(rel), maxCharsPerFile))
			}
			if len(files) > 20 {
				sections = append(sections, "### "+normalized+"/\n\nDirectory listing truncated at 20 files.")
			}
			continue
		}
		sections = append(sections, renderFileContext(absCandidate, normalized, maxCharsPerFile))
	}
	if len(sections) == 0 {
		return "No declared files were found."
	}
	return strings.Join(sections, "\n\n")
}

// normalizeSkillFrontmatterNameForWrittenFiles 规范化已写 SKILL.md 的 frontmatter 名。
// Python: action_executor.py:199-227 _normalize_skill_frontmatter_name_for_written_files。
func normalizeSkillFrontmatterNameForWrittenFiles(actionWorktree string, action MemberOptimizationAction, writtenFiles []string) []string {
	// Python: if action.action_group != "skill": return written_files
	if action.ActionGroup != "skill" {
		return writtenFiles
	}
	changed := append([]string{}, writtenFiles...)
	for _, relPath := range writtenFiles {
		normalized := NormalizeRelPath(relPath)
		skillName := SkillNameFromSkillMdPath(normalized)
		if skillName == "" {
			continue
		}
		path := filepath.Join(actionWorktree, normalized)
		if info, err := os.Stat(path); err != nil || info.IsDir() {
			continue
		}
		content, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		// Python: updated = _normalize_skill_frontmatter_name(current, skill_name=..., fallback_description=...)
		updated := normalizeSkillFrontmatterName(string(content), skillName, skillTriggerDescription(action))
		if updated != string(content) {
			if err := os.WriteFile(path, []byte(updated), 0o644); err == nil {
				changed = append(changed, normalized)
			}
		}
	}
	// Python: return sorted(set(changed))
	return sortedUniqueStrings(changed)
}

// NormalizeSkillFrontmatterName 规范化 SKILL.md frontmatter（name 强制、description 兜底）。
// Python: action_executor.py:229-270 _normalize_skill_frontmatter_name。
func NormalizeSkillFrontmatterName(content, skillName, fallbackDescription string) string {
	return normalizeSkillFrontmatterName(content, skillName, fallbackDescription)
}

// ValidateGeneratedSkillContract 校验原生 SKILL.md 的传输无关属性。
// Python: action_executor.py:272-299 _validate_generated_skill_contract。
func ValidateGeneratedSkillContract(content string) error {
	lines := splitLinesPy(content)
	bodyStart := 0
	frontmatter := map[string]any{}
	// Python: if lines and lines[0].strip() == "---": 解析 frontmatter
	if len(lines) > 0 && strings.TrimSpace(lines[0]) == "---" {
		endIndex := -1
		for index, line := range lines[1:] {
			if strings.TrimSpace(line) == "---" {
				endIndex = index + 1 // 绝对行号（Python enumerate(lines[1:], start=1) 语义）
				break
			}
		}
		if endIndex < 0 {
			endIndex = len(lines)
		}
		bodyStart = endIndex + 1 // Python: body_start = end_index + 1
		rawFrontmatter := strings.Join(lines[1:endIndex], "\n")
		if strings.TrimSpace(rawFrontmatter) != "" {
			var loaded map[string]any
			if err := yaml.Unmarshal([]byte(rawFrontmatter), &loaded); err == nil && loaded != nil {
				frontmatter = loaded
			}
		}
	}
	if len(frontmatter) > 0 {
		// Python: description 必填
		description := strings.TrimSpace(pyStrOrEmpty(frontmatter["description"]))
		if description == "" {
			return fmt.Errorf("generated SKILL.md frontmatter description is required")
		}
	}
	// Python: body 必须非空
	body := strings.TrimSpace(strings.Join(lines[bodyStart:], "\n"))
	if body == "" {
		return fmt.Errorf("generated SKILL.md must contain runtime instructions")
	}
	return nil
}

// SalvageSkillFrontmatter 修复畸形 YAML 前的标量元数据恢复。
// Python: action_executor.py:301-315 _salvage_skill_frontmatter。
func SalvageSkillFrontmatter(rawFrontmatter string) map[string]string {
	salvaged := map[string]string{}
	for _, line := range splitLinesPy(rawFrontmatter) {
		// Python: if line[:1].isspace() or ":" not in line: continue
		if len(line) > 0 && (line[0] == ' ' || line[0] == '\t') {
			continue
		}
		colon := strings.Index(line, ":")
		if colon < 0 {
			continue
		}
		key := strings.TrimSpace(line[:colon])
		if key != "name" && key != "description" {
			continue
		}
		value := strings.TrimSpace(line[colon+1:])
		value = strings.Trim(value, "\"'")
		if value != "" && value != ">" && value != ">-" && value != "|" && value != "|-" {
			salvaged[key] = value
		}
	}
	return salvaged
}

// ActionResourceGuidance 动作专属资源指引。
// Python: action_executor.py:364-476 _action_resource_guidance。
func ActionResourceGuidance(action MemberOptimizationAction) string {
	className := strings.TrimSpace(pyStrOrEmpty(action.Constraints["class_name"]))
	targetPath := NormalizeRelPath(action.TargetPath)
	if action.ActionGroup == "config" {
		return "Edit only runtime.yaml. Allowed keys: max_iterations (positive integer under the host ceiling), " +
			"on_max_iterations (error/return_observations). Preserve unrelated current values. " +
			"Do not add model, credentials, host ceiling, or grading settings."
	}
	if action.ActionGroup == "tool" && (action.Operation == "add" || strings.Contains(pyStrOrEmpty(action.ActionType), "add")) {
		classHint := className
		if classHint == "" {
			classHint = "a descriptive Tool subclass"
		}
		return fmt.Sprintf("Create `%s` with a loadable `%s` class that "+
			"inherits from `openjiuwen.core.foundation.tool.Tool`. Also update "+
			"`tools/tools.yaml` with a `tools` entry containing `file` and "+
			"`class_name`. The ToolCard must define `input_params` as a JSON "+
			"Schema with top-level `type: object`. Keep the implementation "+
			"compact and package-local, with small helper methods and explicit "+
			"structured results. The generated tool must pass package-local "+
			"Python safety verification: do not import httpx, os, pathlib, "+
			"requests, shutil, socket, subprocess, sys, or urllib, and do not "+
			"call __import__, compile, eval, exec, input, or open. Design tools "+
			"to operate on explicit input payloads supplied by the agent, such "+
			"as file contents, selectors, rules, or structured observations, "+
			"rather than reading files, launching processes, installing "+
			"packages, or contacting external services. Give the ToolCard a "+
			"specific `name` and `description` that state when the role should "+
			"search for or load this tool, the concrete defect it detects or "+
			"repairs, and the kind of input payload it expects. Implement "+
			"`invoke` and `stream` so the tool can be mounted, discovered by "+
			"progressive tool search, and called by the role. It must return a "+
			"machine-checkable result that changes the caller's next action. Do not "+
			"approximate an available parser, compiler, linter, or test command "+
			"with a weaker string heuristic, and do not create a passive validator.", targetPath, classHint)
	}
	if action.ActionGroup == "rail" && action.Operation == "add" {
		classHint := className
		if classHint == "" {
			classHint = "a descriptive AgentRail subclass"
		}
		return fmt.Sprintf("Create `%s` with a loadable `%s` class that "+
			"inherits from `openjiuwen.core.single_agent.rail.base.AgentRail`. "+
			"Also update `rails/rails.yaml` with a `rails` entry containing "+
			"`file` and `class_name`. Keep the rail lightweight and dependency-free.", targetPath, classHint)
	}
	if action.ActionGroup == "skill" && action.Operation == "add" {
		return fmt.Sprintf("Create `%s` as a package-local skill. It must be a "+
			"`SKILL.md` file with YAML frontmatter containing a non-empty "+
			"`description`; frontmatter `name` must exactly equal the "+
			"`skills/<snake_name>` directory name and use underscores, not "+
			"hyphens. Base the description on the decision contract's causal "+
			"distinction, using the public task only for broad task-area vocabulary. "+
			"The trigger must not broaden to a sibling mechanism listed only as a "+
			"scope boundary. "+
			"Write the description as a direct runtime trigger such as `Use when ...`; "+
			"never say to create, add, update, generate, or write a Skill. "+
			"Author a native reusable Skill, not an "+
			"optimizer audit report. Teach only the procedural knowledge needed to "+
			"preserve required_behavior and avoid forbidden_behavior. Treat the "+
			"decision_contract as directional causal knowledge: teach its "+
			"required_action, use its acceptance_observable to stop investigation, "+
			"and do not reintroduce the wrong_decision as an optional branch under "+
			"the same trigger. Preserve the meaning and direction of the causal "+
			"distinction, required action, acceptance observable, and scope boundary, "+
			"but rewrite, combine, or shorten them into the clearest native Skill. "+
			"Case-specific code and assertions in the diagnosis are evidence, not "+
			"runtime constants. Express the causal relation using the next task's "+
			"inputs and requirements. Do not promote syntax from the failed patch "+
			"into a general rule or prescribe the literal task patch. Include the "+
			"decisive distinction, the observable that ends investigation, the smallest "+
			"justified action, and a concrete non-tautological acceptance probe that "+
			"checks the positive case and nearest boundary. Merely avoiding an exception "+
			"does not establish value, ownership, ordering, or lifecycle semantics. "+
			"Use any Markdown "+
			"structure and amount of detail that makes that procedure effective; do not "+
			"pad the Skill for a fixed template or optimize for character count. Keep "+
			"case ids, optimizer provenance, evaluator-only names, and manifest fields "+
			"out of runtime instructions. Move optional long reference material into Skill "+
			"resources only when it is genuinely needed. Write the canonical "+
			"`SKILL.md` using native file tools; the executor updates "+
			"`skills/skills.yaml` after validating it. Do not clone, "+
			"search, or install external resources for skill/add.", targetPath)
	}
	if action.ActionGroup == "skill" {
		return fmt.Sprintf("Keep `%s` a valid package-local skill resource. If "+
			"`skills/skills.yaml` is declared, keep it valid YAML and mount the "+
			"actual skill directory (for example skills/foo), or its parent skills directory; "+
			"never mount the SKILL.md file.", targetPath)
	}
	if action.ActionGroup == "subagent" && action.Operation == "add" {
		return "This is a `subagent/add` action, not a `tool/add` action. " +
			fmt.Sprintf("Create the custom subagent package under `%s`. ", targetPath) +
			"Write the subagent system prompt and any package-local resource " +
			"files declared in `constraints.local_resources`. " +
			"Use `constraints.inherited_tools` to choose parent tools that the " +
			"subagent inherits. For subagent-local tools, create the Python " +
			"Tool classes declared under `constraints.local_resources.tools`; " +
			"ToolCard input_params must be a JSON Schema with top-level " +
			"`type: object`. For subagent-local rails, create AgentRail classes " +
			"with hooks such as `before_model_call`. For local skills, create " +
			"`SKILL.md` files with YAML frontmatter. The executor will write " +
			"the package manifests and config from `constraints.local_resources`."
	}
	if action.ActionGroup == "tool" || action.ActionGroup == "rail" {
		manifest := "tools/tools.yaml"
		if action.ActionGroup == "rail" {
			manifest = "rails/rails.yaml"
		}
		return fmt.Sprintf("Keep `%s` loadable and keep `%s` valid YAML "+
			"if the manifest is one of the declared write paths.", targetPath, manifest)
	}
	return "Apply the requested local ExpertHarness package change directly."
}

// SkillTriggerDescription 从决策合同推导可发现的运行时元数据。
// Python: action_executor.py:2056-2080 _skill_trigger_description。
func SkillTriggerDescription(action MemberOptimizationAction) string {
	return skillTriggerDescription(action)
}

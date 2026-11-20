// package_registry.go 可写 RSI 候选包的共享登记规则。
// Python: package_registry.py（全量 308 行）。17.4c2 按前置件切片落地
// plugin_manifest.go 所需闭包（_relative / resolve_registry_resource /
// _skill_entry / expand_skill_mounts）；其余（PROMPT_REGISTRY /
// SKILL_REGISTRY / REGISTRY_GUIDANCE / RegistryIssue / _registry_path /
// _read / _prompt_entry / _document 等验证归一面）⤴️ 17.4c3。

package memberoptimizer

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// ──────────────────────────── 常量 ────────────────────────────

// PromptRegistry 提示段登记簿路径。
// Python: package_registry.py:19 PROMPT_REGISTRY。
const PromptRegistry = "prompt_sections/sections.yaml"

// SkillRegistry 技能登记簿路径。
// Python: package_registry.py:20 SKILL_REGISTRY。
const SkillRegistry = "skills/skills.yaml"

// RegistryGuidance 登记簿写作指引（喂给 LLM 的文案逐字）。
// Python: package_registry.py:22-23 REGISTRY_GUIDANCE。
const RegistryGuidance = `Package paths are relative to the integration package root.
Prompt example: sections: [{name: foo, file: prompt_sections/files/foo.md, priority: 30}].
Use file, not path. Each effective prompt section name must be unique.
Skill example: skills: [{dir: skills/foo}]; dir mounts a directory containing
SKILL.md, or a parent directory of skills, never the SKILL.md file itself.
Equivalent aliases may be collapsed. Conflicting content, names, priorities or
mount options require an explicit repair; do not discard unrelated entries.`

// ──────────────────────────── 非导出函数 ────────────────────────────

// registryRelative 等价 Python _relative：解析已知别名，绝不按 basename 搜索。
// Python: package_registry.py:31-42 _relative。
func registryRelative(root, raw string) (string, error) {
	// Python: if not isinstance(raw, str) or not raw.strip(): raise ValueError(...)
	if strings.TrimSpace(raw) == "" {
		return "", fmt.Errorf("resource reference must be a non-empty string")
	}
	// Python: value = raw.strip().replace("\\", "/"); path = Path(value)
	value := strings.TrimSpace(strings.ReplaceAll(raw, "\\", "/"))
	// Python: if path.is_absolute() or PureWindowsPath(value).drive or ".." in path.parts: raise
	if strings.HasPrefix(value, "/") || windowsDriveLike(value) || containsDotDotPath(value) {
		return "", fmt.Errorf("unsafe package-relative reference: %q", raw)
	}
	// Python: resolved = (root / path).resolve(); if not resolved.is_relative_to(root): raise
	resolved := filepath.Join(root, filepath.FromSlash(value))
	if abs, err := filepath.Abs(resolved); err == nil {
		resolved = abs
	}
	rootAbs, _ := filepath.Abs(root)
	if !withinDir(rootAbs, resolved) {
		return "", fmt.Errorf("resource reference escapes package: %q", raw)
	}
	return resolved, nil
}

// resolveRegistryResource 仅解析已知别名。
// Python: package_registry.py:45-83 resolve_registry_resource。
func resolveRegistryResource(root, raw string, skill bool) (string, error) {
	// Python: root = root.resolve(); direct = _relative(root, raw)
	rootAbs, _ := filepath.Abs(root)
	direct, err := registryRelative(rootAbs, raw)
	if err != nil {
		return "", err
	}
	// Python: parts = list(Path(raw.strip().replace("\\", "/")).parts)
	parts := strings.Split(strings.TrimSpace(strings.ReplaceAll(raw, "\\", "/")), "/")
	// Python: prefix = "skills" if skill else "prompt_sections"; if parts[0] == prefix: pop
	prefix := "prompt_sections"
	if skill {
		prefix = "skills"
	}
	if len(parts) > 0 && parts[0] == prefix {
		parts = parts[1:]
	}
	// Python: repeated = "skills" if skill else "files"; while parts[0] == repeated: pop
	repeated := "files"
	if skill {
		repeated = "skills"
	}
	for len(parts) > 0 && parts[0] == repeated {
		parts = parts[1:]
	}
	// Python: tail = Path(*parts) if parts else Path(".")
	tail := ""
	if len(parts) > 0 {
		tail = strings.Join(parts, "/")
	}
	// Python: mount = Path("skills") if skill else Path("prompt_sections/files")
	mount := "prompt_sections/files"
	if skill {
		mount = "skills"
	}
	// Python: refs = [mount / tail]; if not skill: refs.append(mount / Path(raw...))
	refs := []string{mount}
	if tail != "" && tail != "." {
		refs = []string{mount + "/" + tail}
	} else {
		refs = []string{mount}
	}
	if !skill {
		refs = append(refs, mount+"/"+strings.ReplaceAll(raw, "\\", "/"))
	}
	// Python: candidates = {_relative(root, str(ref)) for ref in refs if exists}
	candidates := map[string]bool{}
	for _, ref := range refs {
		resolved, err := registryRelative(rootAbs, ref)
		if err != nil {
			continue
		}
		if _, statErr := os.Stat(resolved); statErr == nil {
			candidates[resolved] = true
		}
	}
	// Python: if direct.exists(): candidates.add(direct)
	if _, statErr := os.Stat(direct); statErr == nil {
		candidates[direct] = true
	}
	// Python: if len(candidates) != 1: raise ValueError(...)
	if len(candidates) != 1 {
		return "", fmt.Errorf("reference %q has %d existing targets; expected one", raw, len(candidates))
	}
	resolved := ""
	for candidate := range candidates {
		resolved = candidate
	}
	// Python: if skill and resolved.is_file() and resolved.name == "SKILL.md": resolved = resolved.parent
	if skill {
		if info, statErr := os.Stat(resolved); statErr == nil && !info.IsDir() && filepath.Base(resolved) == "SKILL.md" {
			resolved = filepath.Dir(resolved)
		}
		// Python: if not resolved.is_dir(): raise
		if info, statErr := os.Stat(resolved); statErr != nil || !info.IsDir() {
			return "", fmt.Errorf("skill dir must be a directory: %q", raw)
		}
		// Python: definitions = [resolved/SKILL.md, *resolved.glob("*/SKILL.md")]; present = [...]
		definitions := []string{filepath.Join(resolved, "SKILL.md")}
		leaves, _ := filepath.Glob(filepath.Join(resolved, "*", "SKILL.md"))
		definitions = append(definitions, leaves...)
		var present []string
		for _, def := range definitions {
			if _, statErr := os.Stat(def); statErr == nil {
				present = append(present, def)
			}
		}
		// Python: if any(not p.resolve().is_relative_to(root) ...): raise
		for _, def := range present {
			if abs, err := filepath.Abs(def); err == nil && !withinDir(rootAbs, abs) {
				return "", fmt.Errorf("skill mount contains a package-escaping definition: %q", raw)
			}
		}
		// Python: if not present: raise
		if len(present) == 0 {
			return "", fmt.Errorf("no package-local SKILL.md in skill mount: %q", raw)
		}
	} else {
		// Python: elif not resolved.is_file(): raise
		if info, statErr := os.Stat(resolved); statErr != nil || info.IsDir() {
			return "", fmt.Errorf("prompt reference must be a file: %q", raw)
		}
	}
	// Python: return resolved.relative_to(root).as_posix()
	rel, err := filepath.Rel(rootAbs, resolved)
	if err != nil {
		return "", err
	}
	return filepath.ToSlash(rel), nil
}

// skillEntry 等价 Python _skill_entry：raw（str|dict）→ {"dir": resolved}。
// Python: package_registry.py:138-142 _skill_entry。
func skillEntry(root string, raw any) (map[string]any, error) {
	// Python: item = {"dir": raw} if isinstance(raw, str) else dict(raw) if isinstance(raw, dict) else {}
	item := map[string]any{}
	switch v := raw.(type) {
	case string:
		item = map[string]any{"dir": v}
	case map[string]any:
		item = v
	}
	// Python: item["dir"] = resolve_registry_resource(root, item.get("dir"), skill=True)
	resolved, err := resolveRegistryResource(root, fmt.Sprintf("%v", item["dir"]), true)
	if err != nil {
		return nil, err
	}
	item["dir"] = resolved
	return item, nil
}

// expandSkillMounts 把父目录挂载编译为原生 JSON 叶子目录（保留选项）。
// Python: package_registry.py:153-174 expand_skill_mounts。
func expandSkillMounts(root string, entries []any) ([]map[string]any, error) {
	// Python: root = root.resolve(); mounted = {}
	rootAbs, _ := filepath.Abs(root)
	mounted := map[string]map[string]any{}
	// Python: for raw in entries: ...
	for _, raw := range entries {
		// Python: expanded = [raw]; if dict and dirs list: 展开 per dir
		expanded := []any{raw}
		if m, ok := raw.(map[string]any); ok {
			if dirs, ok := m["dirs"].([]any); ok {
				expanded = []any{}
				for _, d := range dirs {
					item := map[string]any{}
					for key, value := range m {
						if key != "dirs" {
							item[key] = value
						}
					}
					item["dir"] = d
					expanded = append(expanded, item)
				}
			}
		}
		for _, value := range expanded {
			// Python: item = _skill_entry(root, value); directory = root / item["dir"]
			item, err := skillEntry(rootAbs, value)
			if err != nil {
				return nil, err
			}
			directory := filepath.Join(rootAbs, fmt.Sprintf("%v", item["dir"]))
			// Python: leaves = [directory] if (directory/"SKILL.md").is_file() else sorted(...)
			var leaves []string
			if _, statErr := os.Stat(filepath.Join(directory, "SKILL.md")); statErr == nil {
				leaves = []string{directory}
			} else {
				matches, _ := filepath.Glob(filepath.Join(directory, "*", "SKILL.md"))
				for _, match := range matches {
					leaves = append(leaves, filepath.Dir(match))
				}
				sort.Strings(leaves)
			}
			for _, leaf := range leaves {
				// Python: relative = leaf.resolve().relative_to(root).as_posix()
				relative, err := filepath.Rel(rootAbs, leaf)
				if err != nil {
					return nil, err
				}
				relative = filepath.ToSlash(relative)
				// Python: compiled = {**item, "dir": relative}
				compiled := map[string]any{}
				for key, value := range item {
					compiled[key] = value
				}
				compiled["dir"] = relative
				// Python: previous = mounted.get(relative); if previous != compiled: raise
				if previous, ok := mounted[relative]; ok && fmt.Sprintf("%v", previous) != fmt.Sprintf("%v", compiled) {
					return nil, fmt.Errorf("overlapping skill mounts have conflicting options: %v; %v", previous, compiled)
				}
				mounted[relative] = compiled
			}
		}
	}
	// Python: return list(mounted.values())（dict 值序；Go 排序保证确定性——键序偏差）
	keys := make([]string, 0, len(mounted))
	for key := range mounted {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	out := make([]map[string]any, 0, len(keys))
	for _, key := range keys {
		out = append(out, mounted[key])
	}
	return out, nil
}

// withinDir 判定 candidate 是否在 dir 内（is_relative_to 等价）。
func withinDir(dir, candidate string) bool {
	rel, err := filepath.Rel(dir, candidate)
	if err != nil {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
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

// windowsDriveLike Windows 盘符前缀判定（PureWindowsPath.drive 等价）。
func windowsDriveLike(value string) bool {
	if len(value) >= 2 && value[1] == ':' {
		return true
	}
	return strings.HasPrefix(value, "\\\\")
}

// ──────────────────────────── c3 前置件补齐（executor 依赖闭包） ────────────────────────────

// RegistryIssue 登记簿问题条目。
// Python: package_registry.py:24-30 RegistryIssue。
type RegistryIssue struct {
	// Code 问题码
	Code string
	// Manifest 登记簿相对路径
	Manifest string
	// Index 条目索引（-1 = 文件级）
	Index int
	// Message 消息
	Message string
}

// Describe 渲染问题（Python describe() 文案逐字）。
func (i RegistryIssue) Describe() string {
	return fmt.Sprintf("%s: %s[%d]: %s", i.Code, i.Manifest, i.Index, i.Message)
}

// RegistryNormalizationReport 归一报告。
// Python: package_registry.py:33-37 RegistryNormalizationReport。
type RegistryNormalizationReport struct {
	// ChangedFiles 变更文件清单
	ChangedFiles []string
	// Changes 变更描述清单
	Changes []string
	// Issues 问题清单
	Issues []RegistryIssue
}

// registryPath 逐段拼接并拒绝符号链接。
// Python: package_registry.py:88-95 _registry_path。
func registryPath(root, manifest string) (string, error) {
	path := root
	for _, part := range strings.Split(manifest, "/") {
		path = filepath.Join(path, part)
		if info, err := os.Lstat(path); err == nil && info.Mode()&os.ModeSymlink != 0 {
			return "", fmt.Errorf("registry writes cannot follow symbolic links: %q", manifest)
		}
	}
	return registryRelative(root, manifest)
}

// registryRead 装载登记簿（list 回落 {key: list}；sections/prompt_sections 别名）。
// Python: package_registry.py:98-116 _read。
func registryRead(root, manifest, key string) (map[string]any, []any, error) {
	path, err := registryPath(root, manifest)
	if err != nil {
		return nil, nil, err
	}
	content, err := os.ReadFile(path)
	if err != nil {
		return nil, nil, err
	}
	var raw any
	if err := yaml.Unmarshal(content, &raw); err != nil {
		return nil, nil, err
	}
	if raw == nil {
		raw = map[string]any{}
	}
	if list, isList := raw.([]any); isList {
		raw = map[string]any{key: list}
	}
	data, isMap := raw.(map[string]any)
	if !isMap {
		return nil, nil, fmt.Errorf("registry must be a mapping or list")
	}
	out := map[string]any{}
	for key2, value := range data {
		out[key2] = value
	}
	// Python: if key == "sections" and "prompt_sections" in data: 别名合并
	if key == "sections" {
		if promptValue, has := out["prompt_sections"]; has {
			if existing, also := out["sections"]; also && fmt.Sprintf("%v", existing) != fmt.Sprintf("%v", promptValue) {
				return nil, nil, fmt.Errorf("sections and prompt_sections conflict")
			}
			out["sections"] = promptValue
			delete(out, "prompt_sections")
		}
	}
	entries, _ := out[key].([]any)
	if out[key] != nil && entries == nil {
		return nil, nil, fmt.Errorf("%s must be a list", key)
	}
	if entries == nil {
		entries = []any{}
	}
	return out, entries, nil
}

// promptEntry 规范化 prompt 段条目（path/file_path 别名、file|content 互斥、priority 整数）。
// Python: package_registry.py:119-136 _prompt_entry。
func promptEntry(root string, raw any) (map[string]any, error) {
	entryMap, isMap := raw.(map[string]any)
	if !isMap {
		return nil, fmt.Errorf("prompt section must be an object")
	}
	item := map[string]any{}
	for key, value := range entryMap {
		item[key] = value
	}
	// Python: if not isinstance(item.get("name"), str) or not item["name"].strip(): raise
	name, isStr := item["name"].(string)
	if !isStr || strings.TrimSpace(name) == "" {
		return nil, fmt.Errorf("prompt section name must be a non-empty string")
	}
	// Python: for alias in ("path", "file_path"): ...
	for _, alias := range []string{"path", "file_path"} {
		aliasValue, has := item[alias]
		if !has {
			continue
		}
		target, err := resolveRegistryResource(root, fmt.Sprintf("%v", aliasValue), false)
		if err != nil {
			return nil, err
		}
		delete(item, alias)
		if fileValue, hasFile := item["file"]; hasFile {
			fileTarget, err := resolveRegistryResource(root, fmt.Sprintf("%v", fileValue), false)
			if err != nil {
				return nil, err
			}
			if fileTarget != target {
				return nil, fmt.Errorf("file and %s refer to different resources", alias)
			}
		}
		item["file"] = target
	}
	if _, hasFile := item["file"]; hasFile {
		resolved, err := resolveRegistryResource(root, fmt.Sprintf("%v", item["file"]), false)
		if err != nil {
			return nil, err
		}
		item["file"] = resolved
		if _, hasContent := item["content"]; hasContent {
			return nil, fmt.Errorf("prompt section must declare file or content, not both")
		}
	} else if _, isStr := item["content"].(string); !isStr {
		if _, isMap := item["content"].(map[string]any); !isMap {
			return nil, fmt.Errorf("prompt section requires file or content")
		}
	}
	// Python: priority = item.get("priority", 100); if type(priority) is not int: raise
	priority := any(100)
	if v, has := item["priority"]; has {
		priority = v
	}
	switch p := priority.(type) {
	case int:
		item["priority"] = p
	case int64:
		item["priority"] = int(p)
	case float64:
		if p != float64(int(p)) {
			return nil, fmt.Errorf("prompt priority must be an integer")
		}
		item["priority"] = int(p)
	default:
		return nil, fmt.Errorf("prompt priority must be an integer")
	}
	return item, nil
}

// registryDocument 规范化文档并收集问题。
// Python: package_registry.py:177-211 _document。
func registryDocument(root, manifest string) (map[string]any, []RegistryIssue, error) {
	prompt := manifest == PromptRegistry
	key := "skills"
	if prompt {
		key = "sections"
	}
	data, entries, err := registryRead(root, manifest, key)
	if err != nil {
		return nil, nil, err
	}
	var result []any
	seen := map[string]map[string]any{}
	var issues []RegistryIssue
	for index, raw := range entries {
		// Python: expanded = [raw]; if not prompt and dirs list: 展开
		expanded := []any{raw}
		if !prompt {
			if m, isMap := raw.(map[string]any); isMap {
				if dirs, isList := m["dirs"].([]any); isList {
					expanded = []any{}
					for _, d := range dirs {
						item := map[string]any{}
						for k, v := range m {
							if k != "dirs" {
								item[k] = v
							}
						}
						item["dir"] = d
						expanded = append(expanded, item)
					}
				}
			}
		}
		for _, value := range expanded {
			var item map[string]any
			var itemErr error
			if prompt {
				item, itemErr = promptEntry(root, value)
			} else {
				item, itemErr = skillEntry(root, value)
			}
			if itemErr != nil {
				issues = append(issues, RegistryIssue{Code: "resource_path_invalid", Manifest: manifest, Index: index, Message: itemErr.Error()})
				continue
			}
			// Python: identity = item["name"] if prompt else item["dir"]
			identity := fmt.Sprintf("%v", item["dir"])
			if prompt {
				identity = fmt.Sprintf("%v", item["name"])
			}
			// Python: previous = seen.get(identity); if previous is not None: 冲突或去重
			if previous, exists := seen[identity]; exists {
				if fmt.Sprintf("%v", previous) != fmt.Sprintf("%v", item) {
					code := "registry_conflict"
					if prompt {
						code = "prompt_section_duplicate"
					}
					issues = append(issues, RegistryIssue{
						Code: code, Manifest: manifest, Index: index,
						Message: fmt.Sprintf("conflicting registrations: %v; %v", previous, item),
					})
				}
				continue
			}
			seen[identity] = item
			result = append(result, item)
		}
	}
	data[key] = result
	// Python: if not prompt and not issues: expand_skill_mounts 校验
	if !prompt && len(issues) == 0 {
		if _, err := expandSkillMounts(root, result); err != nil {
			issues = append(issues, RegistryIssue{Code: "registry_conflict", Manifest: manifest, Index: -1, Message: err.Error()})
		}
	}
	return data, issues, nil
}

// ValidatePackageRegistries 校验别名与冲突（不改包文件）。
// Python: package_registry.py:213-223 validate_package_registries。
func ValidatePackageRegistries(root string) []RegistryIssue {
	rootAbs, _ := filepath.Abs(root)
	var issues []RegistryIssue
	for _, manifest := range []string{PromptRegistry, SkillRegistry} {
		if _, err := os.Stat(filepath.Join(rootAbs, manifest)); err != nil {
			continue
		}
		_, docIssues, err := registryDocument(rootAbs, manifest)
		if err != nil {
			issues = append(issues, RegistryIssue{Code: "registry_schema_invalid", Manifest: manifest, Index: -1, Message: err.Error()})
			continue
		}
		issues = append(issues, docIssues...)
	}
	return issues
}

// registryWritable 路径是否在可写清单内。
// Python: package_registry.py:226-228 _writable。
func registryWritable(path string, writablePaths []string) bool {
	for _, value := range writablePaths {
		trimmed := strings.TrimSuffix(value, "/")
		if path == trimmed || strings.HasPrefix(path, trimmed+"/") {
			return true
		}
	}
	return false
}

// registryReferences 引用报告（不复制任意挂载元数据进反馈）。
// Python: package_registry.py:231-239 _references。
func registryReferences(entries []any) []any {
	var out []any
	for _, item := range entries {
		if m, isMap := item.(map[string]any); isMap {
			ref := map[string]any{}
			for _, key := range []string{"name", "file", "file_path", "path", "dir", "dirs"} {
				if value, has := m[key]; has {
					ref[key] = value
				}
			}
			out = append(out, ref)
			continue
		}
		out = append(out, item)
	}
	return out
}

// NormalizePackageRegistries 在许可登记簿上做无歧义事务性修复。
// Python: package_registry.py:242-268 normalize_package_registries。
func NormalizePackageRegistries(root string, writablePaths []string) RegistryNormalizationReport {
	rootAbs, _ := filepath.Abs(root)
	report := RegistryNormalizationReport{}
	type pendingEntry struct {
		path     string
		manifest string
		updated  string
		change   string
	}
	var pending []pendingEntry
	for _, manifest := range []string{PromptRegistry, SkillRegistry} {
		if !registryWritable(manifest, writablePaths) {
			continue
		}
		if _, err := os.Stat(filepath.Join(rootAbs, manifest)); err != nil {
			continue
		}
		path, err := registryPath(rootAbs, manifest)
		if err != nil {
			report.Issues = append(report.Issues, RegistryIssue{Code: "registry_schema_invalid", Manifest: manifest, Index: -1, Message: err.Error()})
			continue
		}
		data, docIssues, docErr := registryDocument(rootAbs, manifest)
		report.Issues = append(report.Issues, docIssues...)
		if docErr != nil {
			report.Issues = append(report.Issues, RegistryIssue{Code: "registry_schema_invalid", Manifest: manifest, Index: -1, Message: docErr.Error()})
			continue
		}
		original, err := os.ReadFile(path)
		if err != nil {
			report.Issues = append(report.Issues, RegistryIssue{Code: "registry_schema_invalid", Manifest: manifest, Index: -1, Message: err.Error()})
			continue
		}
		updated, err := yaml.Marshal(data)
		if err != nil {
			report.Issues = append(report.Issues, RegistryIssue{Code: "registry_schema_invalid", Manifest: manifest, Index: -1, Message: err.Error()})
			continue
		}
		if string(original) != string(updated) {
			key := "skills"
			if manifest == PromptRegistry {
				key = "sections"
			}
			_, originalEntries, readErr := registryRead(rootAbs, manifest, key)
			var before []any
			if readErr == nil {
				before = originalEntries
			}
			after, _ := data[key].([]any)
			change := fmt.Sprintf("%s: %v -> %v", manifest, registryReferences(before), registryReferences(after))
			pending = append(pending, pendingEntry{path: path, manifest: manifest, updated: string(updated), change: change})
		}
	}
	if len(report.Issues) > 0 {
		return report
	}
	for _, entry := range pending {
		if err := os.WriteFile(entry.path, []byte(entry.updated), 0o644); err != nil {
			report.Issues = append(report.Issues, RegistryIssue{Code: "registry_schema_invalid", Manifest: entry.manifest, Index: -1, Message: err.Error()})
			return report
		}
		report.ChangedFiles = append(report.ChangedFiles, entry.manifest)
		report.Changes = append(report.Changes, entry.change)
	}
	return report
}

// UpsertPromptSection 注册动作的既有文件（不重复等价别名）。
// Python: package_registry.py:271-307 upsert_prompt_section。
func UpsertPromptSection(root string, name, fileRef string, priority *int, writablePaths []string, replaceExisting bool) error {
	rootAbs, _ := filepath.Abs(root)
	// Python: if not _writable(PROMPT_REGISTRY, writable_paths): raise
	if !registryWritable(PromptRegistry, writablePaths) {
		return fmt.Errorf("prompt action must declare %s", PromptRegistry)
	}
	// Python: target = resolve_registry_resource(root, file_ref)
	target, err := resolveRegistryResource(rootAbs, fileRef, false)
	if err != nil {
		return err
	}
	var data map[string]any
	var entries []any
	if _, statErr := os.Stat(filepath.Join(rootAbs, PromptRegistry)); statErr == nil {
		data, entries, err = registryRead(rootAbs, PromptRegistry, "sections")
		if err != nil {
			return err
		}
	} else {
		data = map[string]any{}
		entries = []any{}
	}
	var kept []any
	var selected map[string]any
	for _, raw := range entries {
		item, itemErr := promptEntry(rootAbs, raw)
		if itemErr != nil {
			return itemErr
		}
		// Python: if item["name"] != name: kept.append(item); continue
		if fmt.Sprintf("%v", item["name"]) != name {
			kept = append(kept, item)
			continue
		}
		// Python: if item.get("file") != target: ...
		if fmt.Sprintf("%v", item["file"]) != target {
			if !replaceExisting {
				existing := "inline content"
				if v, has := item["file"]; has {
					existing = fmt.Sprintf("%v", v)
				}
				return fmt.Errorf("prompt_section_duplicate: %q already refers to %q", name, existing)
			}
			delete(item, "content")
			item["file"] = target
		}
		// Python: if selected is not None: 冲突校验
		if selected != nil {
			left := map[string]any{}
			for k, v := range selected {
				left[k] = v
			}
			right := map[string]any{}
			for k, v := range item {
				right[k] = v
			}
			if priority != nil {
				delete(left, "priority")
				delete(right, "priority")
			}
			if fmt.Sprintf("%v", left) != fmt.Sprintf("%v", right) {
				return fmt.Errorf("prompt_section_duplicate: conflicting metadata for %q", name)
			}
		}
		selected = item
	}
	// Python: entry = selected or {"name": name, "file": target, "priority": 30}; priority 覆盖
	entry := selected
	if entry == nil {
		entry = map[string]any{"name": name, "file": target, "priority": 30}
	}
	if priority != nil {
		entry["priority"] = *priority
	}
	// Python: data["sections"] = [*kept, entry]; write
	data["sections"] = append(kept, entry)
	path, err := registryPath(rootAbs, PromptRegistry)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	out, err := yaml.Marshal(data)
	if err != nil {
		return err
	}
	return os.WriteFile(path, out, 0o644)
}

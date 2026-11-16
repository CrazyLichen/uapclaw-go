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
)

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

// plugin_manifest.go RSI 可编辑登记簿与原生 Plugin manifest 的同步。
// Python: plugin_manifest.py（全量对齐，89 行）。

package memberoptimizer

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/resources"
	"gopkg.in/yaml.v3"
)

// registries 四类登记簿：manifest 字段 → (sidecar 相对路径, YAML 键)。
// Python: plugin_manifest.py:17-22 _REGISTRIES（dict 插入序；Go 遍历按固定切片序——键序偏差登记）。
var registries = []struct {
	Field    string
	Relative string
	Key      string
}{
	{"prompt_sections", "prompt_sections/sections.yaml", "sections"},
	{"skills", "skills/skills.yaml", "skills"},
	{"tools", "tools/tools.yaml", "tools"},
	{"rails", "rails/rails.yaml", "rails"},
}

// ──────────────────────────── 导出函数 ────────────────────────────

// PreparePluginRegistries 仅在优化器私有 worktree 中播种动作登记簿。
// Python: plugin_manifest.py:23-53 prepare_plugin_registries。
func PreparePluginRegistries(pkg string) error {
	// Python: try: manifest = find_plugin_manifest(package) except FileNotFoundError: return
	manifestPath, err := resources.FindPluginManifest(pkg)
	if err != nil {
		return nil
	}
	// Python: is_json = manifest.name == "manifest.json"
	isJSON := filepath.Base(manifestPath) == "manifest.json"
	content, err := os.ReadFile(manifestPath)
	if err != nil {
		return err
	}
	var payload map[string]any
	if isJSON {
		if err := json.Unmarshal(content, &payload); err != nil {
			return err
		}
	} else {
		if err := yaml.Unmarshal(content, &payload); err != nil {
			return err
		}
	}
	// Python: if not is_json and payload.get("schema_version") != "expert_harness.v1": return
	if !isJSON && fmt.Sprintf("%v", payload["schema_version"]) != "expert_harness.v1" {
		return nil
	}
	for _, registry := range registries {
		// Python: entries = payload.get(field, []); if not isinstance(list): raise
		rawEntries, ok := payload[registry.Field].([]any)
		if payload[registry.Field] != nil && !ok {
			return fmt.Errorf("Plugin %s must be a list", registry.Field)
		}
		if payload[registry.Field] == nil {
			rawEntries = []any{}
		}
		// Python: if field == "prompt_sections": entries = [{...name 回落 file stem...}]
		if registry.Field == "prompt_sections" {
			for i, entry := range rawEntries {
				entryMap, isMap := entry.(map[string]any)
				if !isMap {
					continue
				}
				if entryMap["name"] == nil || entryMap["name"] == "" {
					file := fmt.Sprintf("%v", entryMap["file"])
					entryMap["name"] = trimExt(filepath.Base(strings2Slash(file)))
					rawEntries[i] = entryMap
				}
			}
		}
		// Python: path = package / relative; path.parent.mkdir(...); write yaml {key: entries}
		path := filepath.Join(pkg, registry.Relative)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return err
		}
		data, err := yaml.Marshal(map[string]any{registry.Key: rawEntries})
		if err != nil {
			return err
		}
		if err := os.WriteFile(path, data, 0o644); err != nil {
			return err
		}
	}
	// Python: if not is_json: 规范声明迁至可编辑 sidecar（防双主复活已删资源）
	if !isJSON {
		payload["schema_version"] = "1.0"
		for _, registry := range registries {
			delete(payload, registry.Field)
		}
		data, err := yaml.Marshal(payload)
		if err != nil {
			return err
		}
		if err := os.WriteFile(manifestPath, data, 0o644); err != nil {
			return err
		}
	}
	return nil
}

// SynchronizePluginManifest 把编辑后的登记簿编译回 JSON（验证/发布前）。
// 根声明是替换不是追加：否则移除的资源会复活、更新资源会被双载。
// Python: plugin_manifest.py:56-78 synchronize_plugin_manifest。
func SynchronizePluginManifest(pkg string) error {
	// Python: manifest = package / "manifest.json"; if not is_file(): return
	manifestPath := filepath.Join(pkg, "manifest.json")
	if info, err := os.Stat(manifestPath); err != nil || info.IsDir() {
		return nil
	}
	content, err := os.ReadFile(manifestPath)
	if err != nil {
		return err
	}
	var payload map[string]any
	if err := json.Unmarshal(content, &payload); err != nil {
		return err
	}
	for _, registry := range registries {
		// Python: path = package / relative; if not is_file(): continue
		path := filepath.Join(pkg, registry.Relative)
		if info, err := os.Stat(path); err != nil || info.IsDir() {
			continue
		}
		registryContent, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		var registryData map[string]any
		if err := yaml.Unmarshal(registryContent, &registryData); err != nil {
			return err
		}
		// Python: entries = registry.get(key) if isinstance(dict) else registry
		var entries any
		if registryData != nil {
			entries = registryData[registry.Key]
		}
		// Python: if not isinstance(entries, list): raise
		entryList, isList := entries.([]any)
		if entries != nil && !isList {
			return fmt.Errorf("Plugin registry %s must contain a %s list", registry.Relative, registry.Key)
		}
		if entries == nil {
			entryList = []any{}
		}
		// Python: if field == "prompt_sections": entries = [_prompt_entry(...)]; elif skills: expand_skill_mounts
		switch registry.Field {
		case "prompt_sections":
			compiled := []any{}
			for _, entry := range entryList {
				compiledEntry, err := promptEntry(pkg, entry)
				if err != nil {
					return err
				}
				compiled = append(compiled, compiledEntry)
			}
			entryList = compiled
		case "skills":
			compiled, err := expandSkillMounts(pkg, entryList)
			if err != nil {
				return err
			}
			entryList = []any{}
			for _, item := range compiled {
				entryList = append(entryList, item)
			}
		}
		payload[registry.Field] = entryList
	}
	// Python: manifest.write_text(json.dumps(payload, ensure_ascii=False, indent=2) + "\n")
	data, err := jsonIndentPy(payload)
	if err != nil {
		return err
	}
	return os.WriteFile(manifestPath, []byte(data+"\n"), 0o644)
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// promptEntry 提示段条目规范化（file 别名解析 / content 包装）。
// Python: plugin_manifest.py:81-89 _prompt_entry。
func promptEntry(pkg string, raw any) (map[string]any, error) {
	// Python: entry = dict(item)
	entryMap, ok := raw.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("prompt section must be an object")
	}
	entry := map[string]any{}
	for key, value := range entryMap {
		entry[key] = value
	}
	// Python: if "file" in entry: entry["file"] = resolve_registry_resource(...)
	if _, hasFile := entry["file"]; hasFile {
		resolved, err := resolveRegistryResource(pkg, fmt.Sprintf("%v", entry["file"]), false)
		if err != nil {
			return nil, err
		}
		entry["file"] = resolved
	} else if content, hasContent := entry["content"]; hasContent {
		// Python: elif isinstance(entry.get("content"), str): entry["content"] = {"en": ...}
		if _, isStr := content.(string); isStr {
			entry["content"] = map[string]any{"en": content}
		}
	}
	return entry, nil
}

// jsonIndentPy 等价 Python json.dumps(payload, ensure_ascii=False, indent=2)。
func jsonIndentPy(payload map[string]any) (string, error) {
	data, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// strings2Slash 路径斜杠归一。
func strings2Slash(path string) string {
	out := ""
	for _, r := range path {
		if r == '\\' {
			out += "/"
		} else {
			out += string(r)
		}
	}
	return out
}

// trimExt 去扩展名（Path(...).stem 等价）。
func trimExt(name string) string {
	ext := filepath.Ext(name)
	return name[:len(name)-len(ext)]
}

package equipment

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	"github.com/uapclaw/uapclaw-go/internal/common/workspace"
)

// ──────────────────────────── 表单参数校验 ────────────────────────────
// Python: _require_* 助手（:1732-1890）。

// requireNonEmptyStr 必填非空字符串。Python: _require_nonempty_str (:1732-1738)。
func requireNonEmptyStr(params map[string]any, key string) (string, error) {
	value, ok := params[key].(string)
	if !ok || strings.TrimSpace(value) == "" {
		return "", &PackageConflictError{Message: "missing or invalid " + key}
	}
	return strings.TrimSpace(value), nil
}

// requireSkillNames 必须是存在的工作区技能目录名列表。
// Python: _require_skill_names (:1740-1756)。
func requireSkillNames(params map[string]any) ([]string, error) {
	items, ok := params["skills"].([]any)
	if !ok {
		return nil, &PackageConflictError{Message: "missing or invalid skills"}
	}
	names := []string{}
	skillsRoot := workspace.AgentSkillsDir()
	for _, item := range items {
		s, ok := item.(string)
		if !ok || strings.TrimSpace(s) == "" {
			return nil, &PackageConflictError{Message: "invalid skill name: empty"}
		}
		skillName, err := rejectPackageNameAny(strings.TrimSpace(s), "skill")
		if err != nil {
			return nil, err
		}
		if !isDir(filepath.Join(skillsRoot, skillName)) {
			return nil, &PackageConflictError{Message: "skill not found: " + skillName}
		}
		names = append(names, skillName)
	}
	return names, nil
}

// requireMcpNames 可选 connector 名列表（去重）。Python: _require_mcp_names (:1758-1776)。
func requireMcpNames(params map[string]any) ([]string, error) {
	raw, present := params["mcps"]
	if !present || raw == nil {
		return []string{}, nil
	}
	items, ok := raw.([]any)
	if !ok {
		return nil, &PackageConflictError{Message: "missing or invalid mcps"}
	}
	names := []string{}
	seen := map[string]bool{}
	for _, item := range items {
		s, ok := item.(string)
		if !ok || strings.TrimSpace(s) == "" {
			return nil, &PackageConflictError{Message: "invalid mcp name: empty"}
		}
		name, err := rejectPackageNameAny(strings.TrimSpace(s), "mcp")
		if err != nil {
			return nil, err
		}
		if seen[name] {
			continue
		}
		names = append(names, name)
		seen[name] = true
	}
	return names, nil
}

// requireQuickInputs 可选快捷输入本地化。Python: _require_quick_inputs (:1778-1792)。
func requireQuickInputs(params map[string]any) ([]map[string]any, error) {
	raw, present := params["quickInputs"]
	if !present || raw == nil {
		return []map[string]any{}, nil
	}
	items, ok := raw.([]any)
	if !ok {
		return nil, &PackageConflictError{Message: "missing or invalid quickInputs"}
	}
	entries := []map[string]any{}
	for _, item := range items {
		s, ok := item.(string)
		if !ok || strings.TrimSpace(s) == "" {
			return nil, &PackageConflictError{Message: "invalid quick input: empty"}
		}
		prompt := strings.TrimSpace(s)
		entries = append(entries, map[string]any{"zh": prompt, "en": prompt})
	}
	return entries, nil
}

// requireTags 可选双语标签（去重）。Python: _require_tags (:1794-1819)。
func requireTags(params map[string]any) ([]map[string]any, error) {
	raw, present := params["tags"]
	if !present || raw == nil {
		return []map[string]any{}, nil
	}
	items, ok := raw.([]any)
	if !ok {
		return nil, &PackageConflictError{Message: "missing or invalid tags"}
	}
	entries := []map[string]any{}
	seen := map[string]bool{}
	for _, item := range items {
		m, ok := item.(map[string]any)
		if !ok {
			return nil, &PackageConflictError{Message: "invalid tag"}
		}
		zh, _ := m["zh"].(string)
		en, _ := m["en"].(string)
		if strings.TrimSpace(zh) == "" || strings.TrimSpace(en) == "" {
			return nil, &PackageConflictError{Message: "invalid tag: zh/en must be non-empty strings"}
		}
		entry := map[string]any{"zh": strings.TrimSpace(zh), "en": strings.TrimSpace(en)}
		key := entry["zh"].(string) + "|" + entry["en"].(string)
		if seen[key] {
			continue
		}
		seen[key] = true
		entries = append(entries, entry)
	}
	return entries, nil
}

// requireFormRuntime 表单 runtime 解析；关闭时 nil。Python: _require_form_runtime (:1821-1841)。
func requireFormRuntime(params map[string]any) (map[string]any, error) {
	raw, present := params["runtime"]
	if !present || raw == nil {
		return nil, nil
	}
	m, ok := raw.(map[string]any)
	if !ok {
		return nil, &PackageConflictError{Message: "missing or invalid runtime"}
	}
	providerName, _ := m["provider_name"].(string)
	allowed := map[string]bool{"claudecode": true, "codex": true}
	if !allowed[providerName] {
		return nil, &PackageConflictError{Message: fmt.Sprintf(
			"runtime.provider_name must be one of: claudecode, codex; got %q", providerName)}
	}
	config := map[string]any{}
	if c, ok := m["config"].(map[string]any); ok {
		config = c
	}
	return map[string]any{"provider_name": providerName, "config": config}, nil
}

// applyFormRuntime 外接 runtime 固定标签 + 拒绝 connector。
// Python: _apply_form_runtime (:1842-1870)。
func applyFormRuntime(tags []map[string]any, mcpNames []string, runtime map[string]any) ([]map[string]any, []string, error) {
	if runtime == nil {
		return tags, mcpNames, nil
	}
	if len(mcpNames) > 0 {
		return nil, nil, &PackageConflictError{Message: "external harness agent_template does not support connectors"}
	}
	newTags := make([]map[string]any, 0, len(tags)+1)
	for _, t := range tags {
		newTags = append(newTags, t)
	}
	newTags = append(newTags, map[string]any{"zh": "外部CLI", "en": "external"})
	return newTags, []string{}, nil
}

// ──────────────────────────── 可用性断言 ────────────────────────────

// assertPackageIDAvailable 拒绝 local/built_in/resources 已存在的 id。
// Python: _assert_package_id_available (:1891-1910)。
func assertPackageIDAvailable(packageID, kind string) error {
	localRoot := equipmentLocalRoot(kind, "")
	builtInRoot := equipmentBuiltInRoot(kind, "")
	for _, pair := range []struct {
		root  string
		label string
	}{{localRoot, "local"}, {builtInRoot, "built_in"}} {
		if _, err := os.Stat(filepath.Join(pair.root, packageID)); err == nil {
			return &PackageConflictError{Message: kind + " package already exists in " + pair.label + ": " + packageID}
		}
	}
	if res := resourcesKindRoot(kind); res != "" {
		if isDir(filepath.Join(res, packageID)) {
			return &PackageConflictError{Message: kind + " package already exists in resources: " + packageID}
		}
	}
	return nil
}

// ──────────────────────────── 包写入 ────────────────────────────

// skillsManifestEntries 装载器形状的 skills 条目（mode 固定 all）。
// Python: _skills_manifest_entries (:1955-1958)。
func skillsManifestEntries(skillNames []string) []map[string]any {
	entries := make([]map[string]any, 0, len(skillNames))
	for _, name := range skillNames {
		entries = append(entries, map[string]any{"dir": "./skills/" + name, "mode": "all"})
	}
	return entries
}

// copyWorkspaceSkills 拷贝工作区技能到包 skills/ 树（原子：临时目录 + 替换；
// 空 skillNames 清空既有 skills/）。Python: _copy_workspace_skills (:1960-2010)。
func copyWorkspaceSkills(pkgDir string, skillNames []string) error {
	skillsDir := filepath.Join(pkgDir, "skills")
	if len(skillNames) == 0 {
		if isDir(skillsDir) {
			return os.RemoveAll(skillsDir)
		}
		return nil
	}
	skillsRoot := workspace.AgentSkillsDir()
	tmpDir := filepath.Join(pkgDir, ".skills.tmp")
	if isDir(tmpDir) {
		_ = os.RemoveAll(tmpDir)
	}
	if err := os.Mkdir(tmpDir, 0o755); err != nil {
		return err
	}
	for _, name := range skillNames {
		if err := copyTreeHelper(filepath.Join(skillsRoot, name), filepath.Join(tmpDir, name)); err != nil {
			_ = os.RemoveAll(tmpDir)
			return err
		}
	}
	if isDir(skillsDir) {
		if err := os.RemoveAll(skillsDir); err != nil {
			return err
		}
	}
	if err := os.Rename(tmpDir, skillsDir); err != nil {
		// Windows 文件占用等：保留 tmp 为 .skills.bak 供排查（对齐 Python rename 失败分支）
		bakDir := filepath.Join(pkgDir, ".skills.bak")
		if isDir(bakDir) {
			_ = os.RemoveAll(bakDir)
		}
		if renameErr := os.Rename(tmpDir, bakDir); renameErr != nil {
			logger.Warn(logger.ComponentAgentServer).Err(renameErr).
				Str("tmp", tmpDir).Msg("[equipment] skills 替换失败且备份失败")
		}
		return err
	}
	return nil
}

// writeJSONFile UTF-8 缩进 JSON + 尾换行。Python: _write_json (:2012-2015)。
func writeJSONFile(path string, payload map[string]any) error {
	data, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0o644)
}

// writeAgentTemplatePackage 原地写模板包（persona + skills + manifest）。
// Python: _write_agent_template_package (:3696-3733 段)。
func writeAgentTemplatePackage(pkgDir string, packageID, name, description, persona string,
	skillNames []string, mcpNames []string, quickInputs []map[string]any,
	tags []map[string]any, runtime map[string]any) error {
	personaDir := filepath.Join(pkgDir, "persona")
	if err := os.MkdirAll(personaDir, 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(personaDir, packageID+".md"), []byte(persona), 0o644); err != nil {
		return err
	}
	if err := copyWorkspaceSkills(pkgDir, skillNames); err != nil {
		return err
	}
	manifest := map[string]any{
		"package_type":        "agent_template",
		"name":                name,
		"description":         description,
		"persona":             map[string]any{"dir": "./persona"},
		"display_name":        map[string]any{"zh": name, "en": name},
		"display_description": map[string]any{"zh": description, "en": description},
		"skills":              skillsManifestEntries(skillNames),
	}
	if len(mcpNames) > 0 {
		mcps := make([]any, 0, len(mcpNames))
		for _, n := range mcpNames {
			mcps = append(mcps, map[string]any{"connector": n})
		}
		manifest["mcps"] = mcps
	}
	if len(quickInputs) > 0 {
		manifest["quick_inputs"] = quickInputsToAny(quickInputs)
	}
	if len(tags) > 0 {
		manifest["tags"] = tagsToAny(tags)
	}
	if runtime != nil {
		manifest["runtime"] = runtime
	}
	return writeJSONFile(filepath.Join(pkgDir, "manifest.json"), manifest)
}

// quickInputsToAny / tagsToAny 载荷转换。
func quickInputsToAny(items []map[string]any) []any {
	out := make([]any, 0, len(items))
	for _, m := range items {
		out = append(out, m)
	}
	return out
}

func tagsToAny(items []map[string]any) []any {
	out := make([]any, 0, len(items))
	for _, m := range items {
		out = append(out, m)
	}
	return out
}

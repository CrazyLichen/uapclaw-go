package resources

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"

	hschema "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/schema"
	saschema "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/schema"
	cmschema "github.com/uapclaw/uapclaw-go/internal/common/schema"
)

// 扩展包加载器：manifest.json 与 legacy YAML（harness_config.yaml /
// expert_harness.yaml / harness.yaml）三形态定位与装载，逐符号 1:1 对齐上游。
// Python: openjiuwen/harness/resources/extension_loader.py

// ──────────────────────────── 常量 ────────────────────────────

const (
	// packageManifestName 模板清单文件名。
	// Python: extension_loader.py 的 _PACKAGE_MANIFEST_NAME
	packageManifestName = "manifest.json"
	// agentTemplatePackageType 模板包的 package_type 取值。
	// Python: extension_loader.py:204 的 "agent_template"
	agentTemplatePackageType = "agent_template"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// FindAgentTemplateManifest 定位 load_agent_template 用的 manifest.json。
// Python: extension_loader.py:165-180 find_agent_template_manifest
//
// 只接受 manifest.json，无 YAML 回退。调用方传该文件本身或包含它的目录。
func FindAgentTemplateManifest(path string) (string, error) {
	expanded, err := expandUserPath(path)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(expanded)
	if err != nil {
		return "", err
	}
	if info.IsDir() {
		candidate := filepath.Join(expanded, packageManifestName)
		fileInfo, statErr := os.Stat(candidate)
		if statErr != nil || fileInfo.IsDir() {
			return "", fmt.Errorf("%s not found: %s", packageManifestName, expanded)
		}
		return filepath.Abs(candidate)
	}
	if filepath.Base(expanded) != packageManifestName {
		return "", fmt.Errorf("%s not found: %s", packageManifestName, expanded)
	}
	return filepath.Abs(expanded)
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// expandUserPath 展开前导 ~（对应 Python 的 Path.expanduser()）。
func expandUserPath(path string) (string, error) {
	if !strings.HasPrefix(path, "~") {
		return path, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	if path == "~" {
		return home, nil
	}
	if strings.HasPrefix(path, "~/") {
		return filepath.Join(home, path[2:]), nil
	}
	return path, nil
}

// ──────────────────────────── 15.15 manifest 定位（extension_loader.py:104-180）────────────────────────────

// manifestName 常量集。Python: extension_loader.py:46-48
const (
	// manifestNames legacy YAML 三形态查找序。
	// Python: _MANIFEST_NAMES = ("harness_config.yaml", "expert_harness.yaml", "harness.yaml")
	manifestNames = "harness_config.yaml,expert_harness.yaml,harness.yaml"
)

// findLegacyYamlManifest 定位 legacy harness_config.yaml / expert_harness.yaml / harness.yaml。
// Python: extension_loader.py:104-123 _find_legacy_yaml_manifest
//
// find_plugin_manifest 的 YAML 回退内部助手；不属于公开 loader 表面
// （没有独立的 legacy-only 入口点）。
func findLegacyYamlManifest(path string) (string, error) {
	requestedPath, err := expandUserPath(path)
	if err != nil {
		return "", err
	}
	info, statErr := os.Stat(requestedPath)
	if statErr == nil && info.IsDir() {
		for _, manifestName := range manifestNameList() {
			manifest := filepath.Join(requestedPath, manifestName)
			if fileInfo, fileErr := os.Stat(manifest); fileErr == nil && !fileInfo.IsDir() {
				return filepath.Abs(manifest)
			}
		}
		return "", fmt.Errorf("%s not found: %s", strings.Join(manifestNameList(), " or "), requestedPath)
	}

	manifest := requestedPath
	if !containsString(manifestNameList(), filepath.Base(manifest)) {
		return "", fmt.Errorf("%s not found: %s", strings.Join(manifestNameList(), " or "), manifest)
	}
	if _, statErr := os.Stat(manifest); statErr != nil {
		return "", fmt.Errorf("%s not found: %s", strings.Join(manifestNameList(), " or "), manifest)
	}
	return filepath.Abs(manifest)
}

// FindPluginManifest 定位 load_plugin(path) 用的 manifest。
// Python: extension_loader.py:140-162 find_plugin_manifest
//
// 显式文件按原样解析（不做兄弟目录搜索）。目录先查 manifest.json；
// 一旦存在即使用，无视其 package_type（类型拒绝交给 LoadPluginPackage——
// manifest.json 存在时不再尝试 legacy YAML 回退）。仅当 manifest.json
// 缺席时才做 legacy harness_config.yaml / expert_harness.yaml / harness.yaml 探测。
func FindPluginManifest(path string) (string, error) {
	requestedPath, err := expandUserPath(path)
	if err != nil {
		return "", err
	}
	info, statErr := os.Stat(requestedPath)
	if statErr == nil && info.IsDir() {
		manifestJSON := filepath.Join(requestedPath, packageManifestName)
		if fileInfo, fileErr := os.Stat(manifestJSON); fileErr == nil && !fileInfo.IsDir() {
			return filepath.Abs(manifestJSON)
		}
		return findLegacyYamlManifest(requestedPath)
	}

	if filepath.Base(requestedPath) == packageManifestName {
		if info, statErr := os.Stat(requestedPath); statErr != nil || info.IsDir() {
			return "", fmt.Errorf("Plugin manifest not found: %s", requestedPath)
		}
		return filepath.Abs(requestedPath)
	}
	return findLegacyYamlManifest(requestedPath)
}

// manifestNameList 返回 legacy 三形态切片。
func manifestNameList() []string {
	return []string{"harness_config.yaml", "expert_harness.yaml", "harness.yaml"}
}

// containsString 复用 resources.go 的同名助手（语义一致）。

// ──────────────────────────── 15.15 常量（extension_loader.py:30-101）────────────────────────────

const (
	// subagentManifestName 子模板清单文件名。Python: _SUBAGENT_MANIFEST_NAME
	subagentManifestName = ".subagent.json"
	// mcpDirManifestNames MCP 目录清单候选名。Python: _MCP_DIR_MANIFEST_NAMES :53
	mcpDirManifestNames = "mcp.json,mcps.json,mcps.yaml"
)

// legacyPackageManifestNames 需要 sidecar 聚合 + 归一化的 legacy YAML 包清单。
// Python: _LEGACY_PACKAGE_MANIFEST_NAMES :52（优先级 harness_config.yaml 先于 harness.yaml）
var legacyPackageManifestNames = []string{"harness_config.yaml", "harness.yaml"}

// canonicalPluginYamlFields legacy YAML 转 PluginSpec 时允许的顶层键。
// Python: _CANONICAL_PLUGIN_YAML_FIELDS :30-44
var canonicalPluginYamlFields = map[string]struct{}{
	"schema_version": {}, "id": {}, "source": {}, "name": {}, "description": {},
	"tools": {}, "mcps": {}, "rails": {}, "prompt_sections": {}, "skills": {}, "metadata": {},
}

// mcpTransportAliases 传输别名 → 规范 type。
// Python: _MCP_TRANSPORT_ALIASES :54-60
var mcpTransportAliases = map[string]string{
	"stdio": "stdio", "sse": "sse", "http": "streamable_http",
	"streamable-http": "streamable_http", "streamable_http": "streamable_http",
}

// legacyAgentControlKeys legacy YAML 的 agent 控制字段，归一化时丢弃。
// Python: _LEGACY_AGENT_CONTROL_KEYS :61-79
var legacyAgentControlKeys = []string{
	"add_general_purpose_agent", "completion_timeout", "context", "default_mode",
	"enable_async_subagent", "enable_subagent_runtime", "enable_task_loop",
	"enable_task_planning", "language", "max_iterations", "meta", "permissions",
	"progressive_tool_enabled", "prompt_mode", "restrict_to_work_dir",
	"stop_eval_conditions", "workspace",
}

// legacyToolToRail legacy tools 中的 rail provider 别名 → rail type。
// Python: _LEGACY_TOOL_TO_RAIL :82-101
var legacyToolToRail = map[string]string{
	"todo": "core.task_planning", "core.todo": "core.task_planning",
	"lsp": "core.lsp", "core.lsp": "core.lsp",
	"ask_user": "core.ask_user", "core.ask_user": "core.ask_user",
	"ask_user_tool": "core.ask_user", "core.ask_user_tool": "core.ask_user",
	"filesystem": "core.sys_operation", "core.filesystem": "core.sys_operation",
	"shell": "core.sys_operation", "core.shell": "core.sys_operation",
	"bash": "core.sys_operation", "core.bash": "core.sys_operation",
	"powershell": "core.sys_operation", "core.powershell": "core.sys_operation",
	"code": "core.sys_operation", "core.code": "core.sys_operation",
}

// ──────────────────────────── 15.15 导出函数 ────────────────────────────

// LoadPluginPackage 把 Plugin manifest.json 或 legacy YAML manifest 解析成 PluginSpec。
// Python: extension_loader.py:183-193 load_plugin_package
//
// manifestPath 必须是 find_plugin_manifest 返回的绝对 manifest 路径；
// 返回的 PluginSpec 上所有资源路径均为归一化后的绝对路径。
func LoadPluginPackage(manifestPath string) (hschema.PluginSpec, error) {
	expanded, err := expandUserPath(manifestPath)
	if err != nil {
		return hschema.PluginSpec{}, err
	}
	absolute, err := filepath.Abs(expanded)
	if err != nil {
		return hschema.PluginSpec{}, err
	}
	if filepath.Base(absolute) == packageManifestName {
		return loadPluginManifestJSON(absolute)
	}
	return loadPluginLegacyYAML(absolute)
}

// NormalizePackageMcps 展开 {file: .../mcp.json} / {dir: ...} 引用并归一化 MCP 条目。
// Python: extension_loader.py:126-132 normalize_package_mcps
//
// Plugin 包装载与 AgentTemplate 包的 manifest.json 指向 MCP 文件/目录时共用。
func NormalizePackageMcps(mcps any, packageDir string) []hschema.McpServerSpec {
	entries := normalizeMcps(mcps, packageDir)
	return entries
}

// loadPluginManifestJSON 新形态 manifest.json → PluginSpec。
// Python: extension_loader.py:253-280 _load_plugin_manifest_json
func loadPluginManifestJSON(manifest string) (hschema.PluginSpec, error) {
	payload, err := readJSONMapping(manifest)
	if err != nil {
		return hschema.PluginSpec{}, err
	}
	packageType, _ := payload["package_type"].(string)
	if packageType != "plugin" {
		return hschema.PluginSpec{}, fmt.Errorf("%s declares package_type=%v, expected 'plugin'", manifest, payload["package_type"])
	}
	for _, forbiddenKey := range []string{"persona", "agent_card", "model", "subagents", "memories", "rubrics"} {
		if _, ok := payload[forbiddenKey]; ok {
			return hschema.PluginSpec{}, fmt.Errorf("Plugin manifest %s must not declare %q", manifest, forbiddenKey)
		}
	}

	pluginID, _ := payload["id"].(string)
	if pluginID == "" {
		return hschema.PluginSpec{}, fmt.Errorf("Plugin manifest %s is missing required 'id'", manifest)
	}

	baseDir := filepath.Dir(manifest)
	packageRoot := baseDir
	promptSections, err := buildPromptSectionSpecs(payload["prompt_sections"], baseDir, packageRoot)
	if err != nil {
		return hschema.PluginSpec{}, err
	}
	tools, err := buildToolSpecs(payload["tools"], baseDir, packageRoot)
	if err != nil {
		return hschema.PluginSpec{}, err
	}
	mcps, err := buildMcpSpecs(payload["mcps"], baseDir, packageRoot)
	if err != nil {
		return hschema.PluginSpec{}, err
	}
	rails, err := buildRailSpecs(payload["rails"], baseDir, packageRoot)
	if err != nil {
		return hschema.PluginSpec{}, err
	}
	skills, err := buildSkillSpecs(payload["skills"], baseDir, packageRoot)
	if err != nil {
		return hschema.PluginSpec{}, err
	}
	metadata, _ := payload["metadata"].(map[string]any)
	if metadata == nil {
		metadata = map[string]any{}
	}
	spec := hschema.PluginSpec{
		ID:             pluginID,
		Name:           stringOrEmpty(payload["name"]),
		Description:    stringOrEmpty(payload["description"]),
		PromptSections: promptSections,
		Tools:          tools,
		Mcps:           mcps,
		Rails:          rails,
		Skills:         skills,
		Metadata:       metadata,
	}
	if err := hschema.ValidatePluginSpec(spec); err != nil {
		return hschema.PluginSpec{}, err
	}
	return spec, nil
}

// loadPluginLegacyYAML legacy YAML → PluginSpec。
// Python: extension_loader.py:283-310 _load_plugin_legacy_yaml
func loadPluginLegacyYAML(manifest string) (hschema.PluginSpec, error) {
	payload, err := normalizeLegacyPluginYAML(manifest)
	if err != nil {
		return hschema.PluginSpec{}, err
	}
	remapLegacyToolsToRails(payload)
	packageDir := filepath.Dir(manifest)
	pluginID := stringOrEmpty(payload["id"])
	if pluginID == "" {
		pluginID = filepath.Base(packageDir)
	}

	toolsRaw, _ := payload["tools"].([]any)
	tools := make([]hschema.BuiltinToolSpec, 0, len(toolsRaw))
	for _, item := range toolsRaw {
		itemMap, ok := item.(map[string]any)
		if !ok {
			continue
		}
		tools = append(tools, absolutizeLegacyResourceTool(itemMap, packageDir))
	}
	railsRaw, _ := payload["rails"].([]any)
	rails := make([]hschema.RailSpec, 0, len(railsRaw))
	for _, item := range railsRaw {
		itemMap, ok := item.(map[string]any)
		if !ok {
			continue
		}
		rails = append(rails, absolutizeLegacyResourceRail(itemMap, packageDir))
	}
	mcps := absolutizeLegacyMcps(payload["mcps"], packageDir)
	skillsRaw, _ := payload["skills"].([]any)
	skills := make([]hschema.SkillSpec, 0, len(skillsRaw))
	for _, item := range skillsRaw {
		itemMap, ok := item.(map[string]any)
		if !ok {
			continue
		}
		skills = append(skills, absolutizeLegacySkill(itemMap, packageDir))
	}

	metadata, _ := payload["metadata"].(map[string]any)
	if metadata == nil {
		metadata = map[string]any{}
	}
	spec := hschema.PluginSpec{
		ID:             pluginID,
		Name:           stringOrEmpty(payload["name"]),
		Description:    stringOrEmpty(payload["description"]),
		PromptSections: promptSectionSpecsFromLegacy(payload["prompt_sections"]),
		Tools:          tools,
		Mcps:           mcps,
		Rails:          rails,
		Skills:         skills,
		Metadata:       metadata,
	}
	if err := hschema.ValidatePluginSpec(spec); err != nil {
		return hschema.PluginSpec{}, err
	}
	return spec, nil
}

// LoadAgentTemplatePackage 把 package_type=agent_template 的 manifest.json 解析成 AgentTemplateSpec。
// Python: extension_loader.py:196-245 load_agent_template_package（15.5 桩替换为全量实现）
func LoadAgentTemplatePackage(manifestPath string) (hschema.AgentTemplateSpec, error) {
	expanded, err := expandUserPath(manifestPath)
	if err != nil {
		return hschema.AgentTemplateSpec{}, err
	}
	manifest, err := filepath.Abs(expanded)
	if err != nil {
		return hschema.AgentTemplateSpec{}, err
	}
	// Python: if manifest.name != _PACKAGE_MANIFEST_NAME（:200）
	if filepath.Base(manifest) != packageManifestName {
		return hschema.AgentTemplateSpec{}, fmt.Errorf(
			"AgentTemplate manifest must be named %q: %s", packageManifestName, manifest)
	}
	packageRoot := filepath.Dir(manifest)
	payload, err := readJSONMapping(manifest)
	if err != nil {
		return hschema.AgentTemplateSpec{}, err
	}
	// Python: if payload.get("package_type") != "agent_template"（:204）
	if pt, _ := payload["package_type"].(string); pt != agentTemplatePackageType {
		return hschema.AgentTemplateSpec{}, fmt.Errorf(
			"%s declares package_type=%v, expected 'agent_template'", manifest, payload["package_type"])
	}

	templateName, _ := payload["name"].(string)
	if templateName == "" || strings.TrimSpace(templateName) == "" {
		return hschema.AgentTemplateSpec{}, fmt.Errorf(
			"AgentTemplate manifest %s is missing required non-empty 'name'", manifest)
	}
	description, _ := payload["description"].(string)
	if description == "" || strings.TrimSpace(description) == "" {
		return hschema.AgentTemplateSpec{}, fmt.Errorf(
			"AgentTemplate manifest %s is missing required non-empty 'description'", manifest)
	}

	baseDir := filepath.Dir(manifest)
	// Python: subagents 列表推导（:217-225）
	subagents := []hschema.AgentTemplateSpec{}
	for _, item := range asList(payload["subagents"]) {
		itemMap, ok := item.(map[string]any)
		if !ok {
			return hschema.AgentTemplateSpec{}, fmt.Errorf("subagents entry must be a mapping: %v", item)
		}
		subDir, _ := itemMap["dir"].(string)
		resolved, err := resolveNewManifestPath(subDir, baseDir, packageRoot, true)
		if err != nil {
			return hschema.AgentTemplateSpec{}, err
		}
		sub, err := loadAgentSubtemplate(resolved, packageRoot)
		if err != nil {
			return hschema.AgentTemplateSpec{}, err
		}
		subagents = append(subagents, sub)
	}

	model, err := buildModelSpec(payload["model"], baseDir, packageRoot)
	if err != nil {
		return hschema.AgentTemplateSpec{}, err
	}
	runtime, err := buildRuntimeSpec(payload["runtime"], baseDir, packageRoot)
	if err != nil {
		return hschema.AgentTemplateSpec{}, err
	}
	promptSections, err := buildPersonaPromptSections(payload["persona"], baseDir, packageRoot)
	if err != nil {
		return hschema.AgentTemplateSpec{}, err
	}
	tools, err := buildToolSpecs(payload["tools"], baseDir, packageRoot)
	if err != nil {
		return hschema.AgentTemplateSpec{}, err
	}
	mcps, err := buildMcpSpecs(payload["mcps"], baseDir, packageRoot)
	if err != nil {
		return hschema.AgentTemplateSpec{}, err
	}
	rails, err := buildRailSpecs(payload["rails"], baseDir, packageRoot)
	if err != nil {
		return hschema.AgentTemplateSpec{}, err
	}
	skills, err := buildSkillSpecs(payload["skills"], baseDir, packageRoot)
	if err != nil {
		return hschema.AgentTemplateSpec{}, err
	}

	memories := []hschema.MemorySpec{}
	for _, item := range asList(payload["memories"]) {
		if m, ok := item.(map[string]any); ok {
			memories = append(memories, hschema.MemorySpec(m))
		}
	}
	rubrics := []hschema.RubricSpec{}
	for _, item := range asList(payload["rubrics"]) {
		if r, ok := item.(map[string]any); ok {
			rubrics = append(rubrics, hschema.RubricSpec(r))
		}
	}
	metadata, _ := payload["metadata"].(map[string]any)
	if metadata == nil {
		metadata = map[string]any{}
	}
	// Python: agent_card=AgentCard(id=name.strip(), name=name.strip(), description=description.strip())
	return hschema.AgentTemplateSpec{
		AgentCard:      saschema.AgentCard{BaseCard: cmschema.BaseCard{ID: strings.TrimSpace(templateName), Name: strings.TrimSpace(templateName), Description: strings.TrimSpace(description)}},
		Model:          model,
		Runtime:        runtime,
		PromptSections: promptSections,
		Tools:          tools,
		Mcps:           mcps,
		Rails:          rails,
		Skills:         skills,
		Memories:       memories,
		Rubrics:        rubrics,
		Subagents:      subagents,
		Metadata:       metadata,
	}, nil
}

// AloadAgentTemplatePackage LoadAgentTemplatePackage 的异步入口。
// Python: extension_loader.py:248-250 aload_agent_template_package（asyncio.to_thread）
// 已知偏差 D-15b：Go 文件 IO 无 async 语义，签名保持同步调用等价。
func AloadAgentTemplatePackage(manifestPath string) (hschema.AgentTemplateSpec, error) {
	return LoadAgentTemplatePackage(manifestPath)
}

// loadAgentSubtemplate 递归装载子模板。Python: extension_loader.py:313-348
func loadAgentSubtemplate(subagentDir, packageRoot string) (hschema.AgentTemplateSpec, error) {
	manifest := filepath.Join(subagentDir, subagentManifestName)
	if info, err := os.Stat(manifest); err != nil || info.IsDir() {
		return hschema.AgentTemplateSpec{}, fmt.Errorf(
			"%s not found for subagent template: %s", subagentManifestName, subagentDir)
	}
	payload, err := readJSONMapping(manifest)
	if err != nil {
		return hschema.AgentTemplateSpec{}, err
	}
	if _, ok := payload["subagents"]; ok {
		return hschema.AgentTemplateSpec{}, fmt.Errorf(
			"Subagent template %s must not declare 'subagents' (only the root agent_template may declare direct subagents)", manifest)
	}
	if _, ok := payload["runtime"]; ok {
		return hschema.AgentTemplateSpec{}, fmt.Errorf(
			"Subagent template %s must not declare 'runtime' (only the root agent_template may select an external runtime)", manifest)
	}
	agentName := stringOrEmpty(payload["agent_name"])
	if agentName == "" {
		return hschema.AgentTemplateSpec{}, fmt.Errorf(
			"Subagent template %s is missing required 'agent_name'", manifest)
	}

	baseDir := filepath.Dir(manifest)
	model, err := buildModelSpec(payload["model"], baseDir, packageRoot)
	if err != nil {
		return hschema.AgentTemplateSpec{}, err
	}
	promptSections, err := buildPersonaPromptSections(payload["persona"], baseDir, packageRoot)
	if err != nil {
		return hschema.AgentTemplateSpec{}, err
	}
	tools, err := buildToolSpecs(payload["tools"], baseDir, packageRoot)
	if err != nil {
		return hschema.AgentTemplateSpec{}, err
	}
	mcps, err := buildMcpSpecs(payload["mcps"], baseDir, packageRoot)
	if err != nil {
		return hschema.AgentTemplateSpec{}, err
	}
	rails, err := buildRailSpecs(payload["rails"], baseDir, packageRoot)
	if err != nil {
		return hschema.AgentTemplateSpec{}, err
	}
	skills, err := buildSkillSpecs(payload["skills"], baseDir, packageRoot)
	if err != nil {
		return hschema.AgentTemplateSpec{}, err
	}
	return hschema.AgentTemplateSpec{
		AgentCard:      saschema.AgentCard{BaseCard: cmschema.BaseCard{ID: agentName, Name: agentName, Description: firstDisplayDescription(payload["display_description"])}},
		Model:          model,
		PromptSections: promptSections,
		Tools:          tools,
		Mcps:           mcps,
		Rails:          rails,
		Skills:         skills,
	}, nil
}

// ──────────────────────────── 15.15 非导出 helpers（extension_loader.py:351 起）────────────────────────────

// firstDisplayDescription 展示描述取 en → zh → 第一个非空。Python: :351-361
func firstDisplayDescription(value any) string {
	m, ok := value.(map[string]any)
	if !ok {
		return ""
	}
	if text := stringOrEmpty(m["en"]); text != "" {
		return text
	}
	if text := stringOrEmpty(m["zh"]); text != "" {
		return text
	}
	for _, text := range m {
		if stringOrEmpty(text) != "" {
			return stringOrEmpty(text)
		}
	}
	return ""
}

// readJSONMapping 读 JSON manifest（容忍 UTF-8 BOM）。Python: :364-370 _read_json_mapping
func readJSONMapping(path string) (map[string]any, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	text := strings.TrimPrefix(string(content), "\ufeff")
	var payload map[string]any
	if err := json.Unmarshal([]byte(text), &payload); err != nil {
		return nil, err
	}
	if payload == nil {
		return nil, fmt.Errorf("Manifest must contain a mapping: %s", path)
	}
	return payload, nil
}

// resolveNewManifestPath 新形态 manifest 路径解析：拒绝绝对输入、限定包根内、
// 校验存在性。Python: extension_loader.py:373-394 _resolve_new_manifest_path
func resolveNewManifestPath(rawPath, baseDir, packageRoot string, mustBeDir bool) (string, error) {
	if filepath.IsAbs(rawPath) || strings.HasPrefix(rawPath, "~") {
		return "", fmt.Errorf("manifest path must be package-relative, got absolute path: %q", rawPath)
	}
	candidate := filepath.Join(baseDir, rawPath)
	absCandidate, err := filepath.Abs(candidate)
	if err != nil {
		return "", err
	}
	if _, err := os.Stat(absCandidate); err != nil {
		return "", fmt.Errorf("manifest path %q not found: %s", rawPath, absCandidate)
	}
	absRoot, _ := filepath.Abs(packageRoot)
	if !withinDir(absRoot, absCandidate) {
		return "", fmt.Errorf("manifest path %q escapes package root %s", rawPath, packageRoot)
	}
	info, _ := os.Stat(absCandidate)
	if mustBeDir && !info.IsDir() {
		return "", fmt.Errorf("manifest path %q must be a directory: %s", rawPath, absCandidate)
	}
	if !mustBeDir && info.IsDir() {
		return "", fmt.Errorf("manifest path %q must be a file: %s", rawPath, absCandidate)
	}
	return absCandidate, nil
}

// buildPersonaPromptSections persona 目录 *.md 合并为 identity 段。
// Python: extension_loader.py:397-414 _build_persona_prompt_sections
func buildPersonaPromptSections(persona any, baseDir, packageRoot string) ([]hschema.PromptSectionSpec, error) {
	if persona == nil {
		return []hschema.PromptSectionSpec{}, nil
	}
	personaMap, ok := persona.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("persona must be a mapping with 'dir': %v", persona)
	}
	dirRaw, ok := personaMap["dir"].(string)
	if !ok {
		return nil, fmt.Errorf("persona must be a mapping with 'dir': %v", persona)
	}
	personaDir, err := resolveNewManifestPath(dirRaw, baseDir, packageRoot, true)
	if err != nil {
		return nil, err
	}
	var markdownFiles []string
	walkErr := filepath.WalkDir(personaDir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() && strings.EqualFold(filepath.Ext(path), ".md") {
			markdownFiles = append(markdownFiles, path)
		}
		return nil
	})
	if walkErr != nil {
		return nil, walkErr
	}
	// Python: sorted by relative posix path
	sort.Strings(markdownFiles)
	if len(markdownFiles) == 0 {
		return nil, fmt.Errorf("persona dir %s has no markdown files", personaDir)
	}
	parts := make([]string, 0, len(markdownFiles))
	for _, mdFile := range markdownFiles {
		content, err := os.ReadFile(mdFile)
		if err != nil {
			return nil, err
		}
		parts = append(parts, string(content))
	}
	text := strings.Join(parts, "\n\n")
	return []hschema.PromptSectionSpec{
		{Name: "identity", Content: map[string]string{"cn": text, "en": text}, Priority: 10},
	}, nil
}

// buildModelSpec model.json 引用 → ModelSpec。Python: extension_loader.py:417-430
func buildModelSpec(modelRef any, baseDir, packageRoot string) (*hschema.ModelSpec, error) {
	if modelRef == nil {
		return nil, nil
	}
	refMap, ok := modelRef.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("model must be a mapping with 'file': %v", modelRef)
	}
	fileRaw, ok := refMap["file"].(string)
	if !ok {
		return nil, fmt.Errorf("model must be a mapping with 'file': %v", modelRef)
	}
	modelPath, err := resolveNewManifestPath(fileRaw, baseDir, packageRoot, false)
	if err != nil {
		return nil, err
	}
	payload, err := readJSONMapping(modelPath)
	if err != nil {
		return nil, err
	}
	modelPayload, ok := payload["model"].(map[string]any)
	if !ok {
		return nil, fmt.Errorf("model file %s is missing the outer 'model' key", modelPath)
	}
	var spec hschema.ModelSpec
	if err := decodeInto(modelPayload, &spec); err != nil {
		return nil, err
	}
	return &spec, nil
}

// buildRuntimeSpec runtime 声明 → AgentRuntimeSpec（sdk_paths 绝对化）。
// Python: extension_loader.py:433-453 _build_runtime_spec
func buildRuntimeSpec(runtime any, baseDir, packageRoot string) (*hschema.AgentRuntimeSpec, error) {
	if runtime == nil {
		return nil, nil
	}
	var spec hschema.AgentRuntimeSpec
	if err := decodeInto(runtime, &spec); err != nil {
		return nil, err
	}
	// Python: provider_name 非空 strip 校验（extension_spec.py:92-98）
	if strings.TrimSpace(spec.ProviderName) == "" {
		return nil, fmt.Errorf("provider_name must be a non-empty string")
	}
	absPaths := make([]string, 0, len(spec.SdkPaths))
	for _, rawPath := range spec.SdkPaths {
		resolved, err := resolveNewManifestPath(rawPath, baseDir, packageRoot, false)
		if err != nil {
			return nil, err
		}
		absPaths = append(absPaths, resolved)
	}
	spec.SdkPaths = absPaths
	return &spec, nil
}

// buildPromptSectionSpecs prompt_sections 装配：file 条目从 prompt_sections/*.md 装载，
// name 取 markdown 词干；内联 name+content 条目保持有效。
// Python: extension_loader.py:456-485 _build_prompt_section_specs
func buildPromptSectionSpecs(items any, baseDir, packageRoot string) ([]hschema.PromptSectionSpec, error) {
	specs := []hschema.PromptSectionSpec{}
	sectionsRoot := filepath.Join(packageRoot, "prompt_sections")
	for _, item := range asList(items) {
		itemMap, ok := item.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("prompt_sections entry must be a mapping: %v", item)
		}
		fileRaw, hasFile := itemMap["file"].(string)
		if !hasFile {
			var spec hschema.PromptSectionSpec
			if err := decodeInto(itemMap, &spec); err != nil {
				return nil, err
			}
			specs = append(specs, spec)
			continue
		}
		filePath, err := resolveNewManifestPath(fileRaw, baseDir, packageRoot, false)
		if err != nil {
			return nil, err
		}
		absRoot, _ := filepath.Abs(sectionsRoot)
		if !withinDir(absRoot, filePath) || !strings.EqualFold(filepath.Ext(filePath), ".md") {
			return nil, fmt.Errorf("prompt_sections file must be a .md under prompt_sections/: %q", fileRaw)
		}
		content, err := os.ReadFile(filePath)
		if err != nil {
			return nil, err
		}
		payload := map[string]any{
			"name":    strings.TrimSuffix(filepath.Base(filePath), filepath.Ext(filePath)),
			"content": sectionContent(string(content)),
		}
		if priority, ok := itemMap["priority"]; ok {
			payload["priority"] = priority
		}
		if renderParams, ok := itemMap["render_params"]; ok {
			payload["render_params"] = renderParams
		}
		var spec hschema.PromptSectionSpec
		if err := decodeInto(payload, &spec); err != nil {
			return nil, err
		}
		specs = append(specs, spec)
	}
	return specs, nil
}

// buildToolSpecs tools 装配。Python: extension_loader.py:488-503 _build_tool_specs
func buildToolSpecs(items any, baseDir, packageRoot string) ([]hschema.BuiltinToolSpec, error) {
	specs := []hschema.BuiltinToolSpec{}
	for _, item := range asList(items) {
		itemMap, ok := item.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("tool entry must be a mapping with 'file': %v", item)
		}
		fileRaw, ok := itemMap["file"].(string)
		if !ok {
			return nil, fmt.Errorf("tool entry must be a mapping with 'file': %v", item)
		}
		filePath, err := resolveNewManifestPath(fileRaw, baseDir, packageRoot, false)
		if err != nil {
			return nil, err
		}
		className := stringOrEmpty(itemMap["class"])
		if className == "" {
			className = stringOrEmpty(itemMap["class_name"])
		}
		specs = append(specs, hschema.BuiltinToolSpec{
			Type: "harness.tool.file",
			Params: map[string]any{
				"file_path":  filePath,
				"class_name": className,
			},
		})
	}
	return specs, nil
}

// buildRailSpecs rails 装配。Python: extension_loader.py:506-521 _build_rail_specs
func buildRailSpecs(items any, baseDir, packageRoot string) ([]hschema.RailSpec, error) {
	specs := []hschema.RailSpec{}
	for _, item := range asList(items) {
		itemMap, ok := item.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("rail entry must be a mapping with 'file': %v", item)
		}
		fileRaw, ok := itemMap["file"].(string)
		if !ok {
			return nil, fmt.Errorf("rail entry must be a mapping with 'file': %v", item)
		}
		filePath, err := resolveNewManifestPath(fileRaw, baseDir, packageRoot, false)
		if err != nil {
			return nil, err
		}
		className := stringOrEmpty(itemMap["class"])
		if className == "" {
			className = stringOrEmpty(itemMap["class_name"])
		}
		specs = append(specs, hschema.RailSpec{
			Type: "harness.rail.file",
			Params: map[string]any{
				"file_path":  filePath,
				"class_name": className,
			},
		})
	}
	return specs, nil
}

// buildSkillSpecs skills 装配（目录去重、SKILL.md 存在校验）。
// Python: extension_loader.py:524-547 _build_skill_specs
func buildSkillSpecs(items any, baseDir, packageRoot string) ([]hschema.SkillSpec, error) {
	specs := []hschema.SkillSpec{}
	seen := map[string]struct{}{}
	for _, rawItem := range asList(items) {
		var itemMap map[string]any
		if rawStr, ok := rawItem.(string); ok {
			itemMap = map[string]any{"dir": rawStr}
		} else if m, ok := rawItem.(map[string]any); ok {
			itemMap = m
		} else {
			return nil, fmt.Errorf("skill entry must be a mapping with 'dir': %v", rawItem)
		}
		dirRaw, ok := itemMap["dir"].(string)
		if !ok {
			return nil, fmt.Errorf("skill entry must be a mapping with 'dir': %v", rawItem)
		}
		directory, err := resolveNewManifestPath(dirRaw, baseDir, packageRoot, true)
		if err != nil {
			return nil, err
		}
		// Python: 校验 SKILL.md 存在（:534-536）
		if _, err := os.Stat(filepath.Join(directory, "SKILL.md")); err != nil {
			return nil, fmt.Errorf("manifest path %q must be a file: %s", "SKILL.md", filepath.Join(directory, "SKILL.md"))
		}
		if _, dup := seen[directory]; dup {
			return nil, fmt.Errorf("duplicate skill directory: %s", directory)
		}
		seen[directory] = struct{}{}
		mode := stringOrEmpty(itemMap["mode"])
		if mode == "" {
			mode = "all"
		}
		mode, modeErr := skillMode(mode)
		if modeErr != nil {
			return nil, modeErr
		}
		enabledSkills, err := skillEnabledList(itemMap["enabled_skills"])
		if err != nil {
			return nil, err
		}
		specs = append(specs, hschema.SkillSpec{Dir: directory, Mode: mode, EnabledSkills: enabledSkills})
	}
	return specs, nil
}

// skillMode 模式白名单。Python: extension_loader.py:550-553 _skill_mode
func skillMode(value string) (string, error) {
	if value == "all" || value == "auto_list" {
		return value, nil
	}
	return "", fmt.Errorf("skill mode must be 'all' or 'auto_list', got %q", value)
}

// skillEnabledList 启用列表校验。Python: extension_loader.py:556-561 _skill_enabled_list
func skillEnabledList(value any) ([]string, error) {
	if value == nil {
		return nil, nil
	}
	list, ok := value.([]any)
	if !ok {
		return nil, fmt.Errorf("enabled_skills must be a list, got %v", value)
	}
	result := make([]string, 0, len(list))
	for _, item := range list {
		result = append(result, fmt.Sprint(item))
	}
	return result, nil
}

// buildMcpSpecs mcps 装配：file/dir 引用展开、connector 跳过、内联条目直建。
// Python: extension_loader.py:564-593 _build_mcp_specs
func buildMcpSpecs(items any, baseDir, packageRoot string) ([]hschema.McpServerSpec, error) {
	specs := []hschema.McpServerSpec{}
	for _, item := range asList(items) {
		itemMap, ok := item.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("mcp entry must be a mapping: %v", item)
		}
		_, hasFile := itemMap["file"]
		_, hasDir := itemMap["dir"]
		if hasFile && !looksLikeMcpServerEntry(itemMap) {
			mcpFile, err := resolveNewManifestPath(fmt.Sprint(itemMap["file"]), baseDir, packageRoot, false)
			if err != nil {
				return nil, err
			}
			entries, err := mcpPayloadEntries(loadDataFile(mcpFile))
			if err != nil {
				return nil, err
			}
			for _, rawEntry := range entries {
				spec, err := buildMcpServerSpec(rawEntry, filepath.Dir(mcpFile), packageRoot)
				if err != nil {
					return nil, err
				}
				specs = append(specs, spec)
			}
			continue
		}
		if hasDir && !looksLikeMcpServerEntry(itemMap) {
			mcpDir, err := resolveNewManifestPath(fmt.Sprint(itemMap["dir"]), baseDir, packageRoot, true)
			if err != nil {
				return nil, err
			}
			entries, err := loadMcpDirEntries(mcpDir)
			if err != nil {
				return nil, err
			}
			for _, rawEntry := range entries {
				spec, err := buildMcpServerSpec(rawEntry, mcpDir, packageRoot)
				if err != nil {
					return nil, err
				}
				specs = append(specs, spec)
			}
			continue
		}
		if connector, ok := itemMap["connector"]; ok {
			// 宿主管理的外部 MCP 引用：仅 connector 一个键且非空字符串，跳过展开。
			// Python: :584-591
			if len(itemMap) != 1 {
				return nil, fmt.Errorf("mcp connector entry must be {'connector': <non-empty str>}: %v", item)
			}
			text, isStr := connector.(string)
			if !isStr || text == "" {
				return nil, fmt.Errorf("mcp connector entry must be {'connector': <non-empty str>}: %v", item)
			}
			continue
		}
		spec, err := buildMcpServerSpec(itemMap, baseDir, packageRoot)
		if err != nil {
			return nil, err
		}
		specs = append(specs, spec)
	}
	return specs, nil
}

// mcpPayloadEntries 解析市场 mcpServers 或 legacy servers/mcps 列表。
// Python: extension_loader.py:596-610 _mcp_payload_entries
func mcpPayloadEntries(payload any) ([]map[string]any, error) {
	if payloadMap, ok := payload.(map[string]any); ok {
		if servers, ok := payloadMap["mcpServers"].(map[string]any); ok {
			entries := []map[string]any{}
			for name, cfg := range servers {
				cfgMap, ok := cfg.(map[string]any)
				if !ok {
					continue
				}
				entry := copyMap(cfgMap)
				if _, has := entry["name"]; !has {
					entry["name"] = name
				}
				entries = append(entries, entry)
			}
			return entries, nil
		}
		servers, hasServers := payloadMap["servers"]
		if !hasServers {
			servers = payloadMap["mcps"]
		}
		entries := []map[string]any{}
		for _, item := range asList(servers) {
			if m, ok := item.(map[string]any); ok {
				entries = append(entries, m)
			}
		}
		return entries, nil
	}
	entries := []map[string]any{}
	for _, item := range asList(payload) {
		if m, ok := item.(map[string]any); ok {
			entries = append(entries, m)
		}
	}
	return entries, nil
}

// loadMcpDirEntries 从 MCP 目录按 mcp.json → mcps.json → mcps.yaml 顺序读条目。
// Python: extension_loader.py:613-619 _load_mcp_dir_entries
func loadMcpDirEntries(mcpDir string) ([]map[string]any, error) {
	for _, filename := range []string{"mcp.json", "mcps.json", "mcps.yaml"} {
		path := filepath.Join(mcpDir, filename)
		if info, err := os.Stat(path); err != nil || info.IsDir() {
			continue
		}
		return mcpPayloadEntries(loadDataFile(path))
	}
	return nil, fmt.Errorf("MCP dir %s must contain one of: %s", mcpDir, mcpDirManifestNames)
}

// buildMcpServerSpec 单个 MCP 条目 → McpServerSpec（cwd 包根内限定 + 缺省取 baseDir）。
// Python: extension_loader.py:622-641 _build_mcp_server_spec
func buildMcpServerSpec(item map[string]any, baseDir, packageRoot string) (hschema.McpServerSpec, error) {
	entry := normalizeMcpServerEntry(item)
	if entry == nil {
		return hschema.McpServerSpec{}, fmt.Errorf("mcp entry is disabled and cannot be used: %v", item)
	}
	// MCP cwd 是 stdio 子进程工作目录，不是可执行资源：保留包根限定 + 拒绝绝对路径
	// 但不做 strict-resolve / is_dir —— 目录只在子进程启动时需要存在。
	rawCwd := stringOrEmpty(entry["cwd"])
	if rawCwd != "" {
		if filepath.IsAbs(rawCwd) || strings.HasPrefix(rawCwd, "~") {
			return hschema.McpServerSpec{}, fmt.Errorf("mcp cwd must be package-relative, got absolute path: %q", rawCwd)
		}
		candidate, _ := filepath.Abs(filepath.Join(baseDir, rawCwd))
		absRoot, _ := filepath.Abs(packageRoot)
		if !withinDir(absRoot, candidate) {
			return hschema.McpServerSpec{}, fmt.Errorf("mcp cwd %q escapes package root %s", rawCwd, packageRoot)
		}
		entry["cwd"] = candidate
	} else {
		entry["cwd"] = baseDir
	}
	var spec hschema.McpServerSpec
	if err := decodeInto(entry, &spec); err != nil {
		return hschema.McpServerSpec{}, err
	}
	return spec, nil
}

// absolutizeLegacyResourceTool legacy tool 条目的 file_path 绝对化。
// Python: extension_loader.py:644-651 _absolutize_legacy_resource(kind="tool")
func absolutizeLegacyResourceTool(item map[string]any, packageDir string) hschema.BuiltinToolSpec {
	normalized := absolutizeLegacyResource(item, packageDir, "tool")
	var spec hschema.BuiltinToolSpec
	if err := decodeInto(normalized, &spec); err != nil {
		return hschema.BuiltinToolSpec{Type: stringOrEmpty(item["type"])}
	}
	return spec
}

// absolutizeLegacyResourceRail legacy rail 条目的 file_path 绝对化。
// Python: extension_loader.py:644-651 _absolutize_legacy_resource(kind="rail")
func absolutizeLegacyResourceRail(item map[string]any, packageDir string) hschema.RailSpec {
	normalized := absolutizeLegacyResource(item, packageDir, "rail")
	var spec hschema.RailSpec
	if err := decodeInto(normalized, &spec); err != nil {
		return hschema.RailSpec{Type: stringOrEmpty(item["type"])}
	}
	return spec
}

// absolutizeLegacyResource legacy file 型资源条目 file_path 绝对化。
// Python: extension_loader.py:644-651 _absolutize_legacy_resource
func absolutizeLegacyResource(item map[string]any, packageDir, kind string) map[string]any {
	if item["type"] != "harness."+kind+".file" {
		return item
	}
	params := copyMap(mapOrEmpty(item["params"]))
	if filePath := stringOrEmpty(params["file_path"]); filePath != "" {
		resolved, err := resolveLegacyPath(filePath, packageDir)
		if err == nil {
			params["file_path"] = resolved
		}
	}
	result := copyMap(item)
	result["params"] = params
	return result
}

// absolutizeLegacySkill legacy skill 条目 dir 绝对化。Python: :654-658
func absolutizeLegacySkill(item map[string]any, packageDir string) hschema.SkillSpec {
	directory := stringOrEmpty(item["dir"])
	if directory != "" {
		resolved, err := resolveLegacyPath(directory, packageDir)
		if err == nil {
			cloned := copyMap(item)
			cloned["dir"] = resolved
			item = cloned
			directory = resolved
		}
	}
	var spec hschema.SkillSpec
	if err := decodeInto(item, &spec); err != nil {
		return hschema.SkillSpec{Dir: directory}
	}
	return spec
}

// absolutizeLegacyMcps legacy mcps 条目 cwd 绝对化（缺省取包目录）。
// Python: extension_loader.py:661-668 _absolutize_legacy_mcp
func absolutizeLegacyMcps(items any, packageDir string) []hschema.McpServerSpec {
	result := []hschema.McpServerSpec{}
	for _, item := range asList(items) {
		itemMap, ok := item.(map[string]any)
		if !ok {
			continue
		}
		cloned := copyMap(itemMap)
		cwd := stringOrEmpty(cloned["cwd"])
		if cwd == "" {
			// 绝对路径，运行时永不收到未设置的 cwd。
			absDir, _ := filepath.Abs(packageDir)
			cloned["cwd"] = absDir
		} else if resolved, err := resolveLegacyPath(cwd, packageDir); err == nil {
			cloned["cwd"] = resolved
		}
		entry := normalizeMcpServerEntry(cloned)
		if entry == nil {
			continue
		}
		var spec hschema.McpServerSpec
		if err := decodeInto(entry, &spec); err == nil {
			result = append(result, spec)
		}
	}
	return result
}

// resolveLegacyPath legacy 相对路径按包目录解析（strict 存在校验）。
// Python: extension_loader.py:671-675 _resolve_legacy_path
func resolveLegacyPath(rawPath, packageDir string) (string, error) {
	expanded, err := expandUserPath(rawPath)
	if err != nil {
		return "", err
	}
	if !filepath.IsAbs(expanded) {
		expanded = filepath.Join(packageDir, expanded)
	}
	abs, err := filepath.Abs(expanded)
	if err != nil {
		return "", err
	}
	if _, err := os.Stat(abs); err != nil {
		return "", err
	}
	return abs, nil
}

// normalizeLegacyPluginYAML legacy YAML 归一化为 canonical 字段集。
// Python: extension_loader.py:678-723 _normalize_legacy_plugin_yaml
func normalizeLegacyPluginYAML(manifest string) (map[string]any, error) {
	payload, err := loadYamlMapping(manifest)
	if err != nil {
		return nil, err
	}
	packageDir := filepath.Dir(manifest)
	// Python: expert_harness.v1 分支只归一化 mcps（:681-684）
	if schemaVersion, _ := payload["schema_version"].(string); schemaVersion == "expert_harness.v1" {
		normalized := copyMap(payload)
		normalized["mcps"] = mcpsToAny(normalizeMcps(normalized["mcps"], packageDir))
		return normalized, nil
	}
	// Python: 非 legacy 包清单名（expert_harness.yaml 等无聚合，只归一化 mcps）（:685-689）
	if !containsString(legacyPackageManifestNames, filepath.Base(manifest)) {
		normalized := copyMap(payload)
		if _, ok := normalized["mcps"]; ok {
			normalized["mcps"] = mcpsToAny(normalizeMcps(normalized["mcps"], packageDir))
		}
		return normalized, nil
	}

	normalized := copyMap(payload)
	// Python: resources 段聚合进顶层（:692-696）
	resources, _ := normalized["resources"].(map[string]any)
	delete(normalized, "resources")
	if resources != nil {
		for _, key := range []string{"tools", "mcps", "rails", "prompt_sections", "skills"} {
			mergeItems(normalized, key, resources[key])
		}
	}

	mergeLegacyPromptSections(normalized)
	dropLegacyAgentControlFields(normalized)
	normalized["schema_version"] = "expert_harness.v1"
	if _, ok := normalized["id"]; !ok {
		name := stringOrEmpty(normalized["name"])
		if name == "" {
			name = filepath.Base(packageDir)
		}
		normalized["id"] = name
	}
	if _, ok := normalized["name"]; !ok {
		normalized["name"] = filepath.Base(packageDir)
	}
	mergeSidecarPromptSections(normalized, packageDir)
	mergeListFile(normalized, filepath.Join(packageDir, "tools", "tools.yaml"), "tools")
	mergeListFile(normalized, filepath.Join(packageDir, "mcps", "mcps.yaml"), "mcps")
	mergeListFile(normalized, filepath.Join(packageDir, "mcps", "mcps.json"), "mcps")
	mergeListFile(normalized, filepath.Join(packageDir, "rails", "rails.yaml"), "rails")
	mergeListFile(normalized, filepath.Join(packageDir, "skills", "skills.yaml"), "skills")
	normalized["skills"] = anyList(normalizeSkills(normalized["skills"]))
	normalized["mcps"] = mcpsToAny(normalizeMcps(normalized["mcps"], packageDir))
	normalized["tools"] = anyList(normalizeResourceItems(normalized["tools"], "tool", packageDir))
	normalized["rails"] = anyList(normalizeResourceItems(normalized["rails"], "rail", packageDir))
	// 丢弃残留 legacy 顶层键（旧 build_from_expert_harness_pkg 只读已知键，
	// 镜像该行为避免 canonical spec 的 extra=forbid 拒绝 legacy 包）。Python: :720-723
	result := map[string]any{}
	for key, value := range normalized {
		if _, ok := canonicalPluginYamlFields[key]; ok {
			result[key] = value
		}
	}
	return result, nil
}

// remapLegacyToolsToRails legacy tools 中的 rail 别名移入 rails 并从 tools 丢弃。
// Python: extension_loader.py:726-754 _remap_legacy_tools_to_rails
func remapLegacyToolsToRails(payload map[string]any) {
	tools := anyList(payload["tools"])
	rails := anyList(payload["rails"])
	railTypes := map[string]struct{}{}
	for _, rail := range rails {
		if railMap, ok := rail.(map[string]any); ok {
			if railType := stringOrEmpty(railMap["type"]); railType != "" {
				railTypes[railType] = struct{}{}
			}
		}
	}

	keptTools := []any{}
	for _, tool := range tools {
		var rawType string
		if text, ok := tool.(string); ok {
			rawType = text
		} else if toolMap, ok := tool.(map[string]any); ok {
			rawType = stringOrEmpty(toolMap["type"])
			if rawType == "" {
				keptTools = append(keptTools, tool)
				continue
			}
		} else {
			keptTools = append(keptTools, tool)
			continue
		}

		railType, mapped := legacyToolToRail[rawType]
		if !mapped {
			keptTools = append(keptTools, tool)
			continue
		}
		if _, exists := railTypes[railType]; !exists {
			rails = append(rails, map[string]any{"type": railType, "params": map[string]any{}})
			railTypes[railType] = struct{}{}
		}
	}

	payload["tools"] = keptTools
	payload["rails"] = rails
}

// dropLegacyAgentControlFields 丢弃 legacy agent 控制字段。Python: :757-759
func dropLegacyAgentControlFields(payload map[string]any) {
	for _, key := range legacyAgentControlKeys {
		delete(payload, key)
	}
}

// mergeLegacyPromptSections legacy prompts.sections 合并进 payload。
// Python: extension_loader.py:762-774 _merge_legacy_prompt_sections
func mergeLegacyPromptSections(payload map[string]any) {
	prompts, _ := payload["prompts"].(map[string]any)
	delete(payload, "prompts")
	if prompts == nil {
		return
	}
	for _, section := range asList(prompts["sections"]) {
		sectionMap, ok := section.(map[string]any)
		if !ok {
			continue
		}
		normalized := normalizeLegacyPromptSection(sectionMap)
		if _, hasFilename := normalized["filename"]; hasFilename {
			mergeItems(payload, "file_sections", normalized)
			continue
		}
		mergeItems(payload, "prompt_sections", normalized)
	}
}

// normalizeLegacyPromptSection legacy 段落归一化。Python: :777-797
func normalizeLegacyPromptSection(section map[string]any) map[string]any {
	content := normalizeLegacySectionContent(section["content"])
	renderParams := copyMap(mapOrEmpty(section["render_params"]))
	fileName := section["file"]
	if fileName != nil {
		return map[string]any{
			"filename":      fmt.Sprint(fileName),
			"content":       content,
			"render_params": renderParams,
		}
	}
	name := fmt.Sprint(section["name"])
	priority := 30
	if name == "identity" {
		priority = 10
	}
	if rawPriority, ok := section["priority"]; ok && rawPriority != nil {
		priority = toInt(rawPriority)
	}
	return map[string]any{
		"name":          name,
		"content":       content,
		"priority":      priority,
		"render_params": renderParams,
	}
}

// normalizeLegacySectionContent legacy 段落正文归一化。Python: :800-807
func normalizeLegacySectionContent(content any) map[string]string {
	if content == nil {
		return map[string]string{}
	}
	if text, ok := content.(string); ok {
		return sectionContent(text)
	}
	if m, ok := content.(map[string]any); ok {
		result := map[string]string{}
		for language, text := range m {
			result[language] = fmt.Sprint(text)
		}
		return result
	}
	return map[string]string{}
}

// promptSectionSpecsFromLegacy legacy prompt_sections 列表 → PromptSectionSpec。
// Python: :292 [PromptSectionSpec.model_validate(item) for item in payload.get("prompt_sections", [])]
func promptSectionSpecsFromLegacy(items any) []hschema.PromptSectionSpec {
	specs := []hschema.PromptSectionSpec{}
	for _, item := range asList(items) {
		itemMap, ok := item.(map[string]any)
		if !ok {
			continue
		}
		var spec hschema.PromptSectionSpec
		if err := decodeInto(itemMap, &spec); err != nil {
			continue
		}
		specs = append(specs, spec)
	}
	return specs
}

// loadYamlMapping 读 YAML manifest。Python: :810-814 _load_yaml_mapping
func loadYamlMapping(path string) (map[string]any, error) {
	payload := loadDataFile(path)
	m, ok := payload.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("Harness manifest must contain a mapping: %s", path)
	}
	return m, nil
}

// loadDataFile 按扩展名 JSON/YAML 读文件（YAML 结果归一化为 string 键 map）。
// Python: :817-821 _load_data_file
func loadDataFile(path string) any {
	content, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	if strings.EqualFold(filepath.Ext(path), ".json") {
		var payload any
		if jsonErr := json.Unmarshal(content, &payload); jsonErr != nil {
			return nil
		}
		return normalizeYAMLValue(payload)
	}
	var payload any
	if yamlErr := yaml.Unmarshal(content, &payload); yamlErr != nil {
		return nil
	}
	// Python: yaml.safe_load(text) or {}
	return normalizeYAMLValue(payload)
}

// normalizeYAMLValue 把 yaml.v3 产出的 map[any]any 递归转为 map[string]any。
// （Python yaml.safe_load 恒为 str 键，Go yaml.v3 对复合键产出 any 键。）
func normalizeYAMLValue(value any) any {
	switch v := value.(type) {
	case map[any]any:
		result := map[string]any{}
		for key, item := range v {
			result[fmt.Sprint(key)] = normalizeYAMLValue(item)
		}
		return result
	case map[string]any:
		result := map[string]any{}
		for key, item := range v {
			result[key] = normalizeYAMLValue(item)
		}
		return result
	case []any:
		result := make([]any, len(v))
		for i, item := range v {
			result[i] = normalizeYAMLValue(item)
		}
		return result
	default:
		return value
	}
}

// normalizeMcps 展开 {file}/{dir} 引用并归一化 MCP 条目。
// Python: extension_loader.py:824-849 _normalize_mcps
func normalizeMcps(items any, packageDir string) []hschema.McpServerSpec {
	normalized := []hschema.McpServerSpec{}
	for _, item := range asList(items) {
		itemMap, ok := item.(map[string]any)
		if !ok {
			continue
		}
		_, hasFile := itemMap["file"]
		_, hasDir := itemMap["dir"]
		if hasFile && !looksLikeMcpServerEntry(itemMap) {
			mcpFile := fmt.Sprint(itemMap["file"])
			if !filepath.IsAbs(mcpFile) {
				mcpFile = filepath.Join(packageDir, mcpFile)
			}
			absFile, _ := filepath.Abs(mcpFile)
			entries, _ := mcpPayloadEntries(loadDataFile(absFile))
			for _, raw := range entries {
				entry := normalizeMcpServerEntry(raw)
				if entry != nil {
					if stringOrEmpty(entry["cwd"]) == "" {
						entry["cwd"] = filepath.Dir(absFile)
					}
					var spec hschema.McpServerSpec
					if err := decodeInto(entry, &spec); err == nil {
						normalized = append(normalized, spec)
					}
				}
			}
			continue
		}
		if hasDir && !looksLikeMcpServerEntry(itemMap) {
			normalized = append(normalized, loadMcpsFromDir(packageDir, fmt.Sprint(itemMap["dir"]))...)
			continue
		}
		entry := normalizeMcpServerEntry(itemMap)
		if entry != nil {
			var spec hschema.McpServerSpec
			if err := decodeInto(entry, &spec); err == nil {
				normalized = append(normalized, spec)
			}
		}
	}
	return normalized
}

// looksLikeMcpServerEntry 判断条目是否本身是 server 定义（而非 file/dir 引用）。
// Python: extension_loader.py:852-864 _looks_like_mcp_server_entry
func looksLikeMcpServerEntry(item map[string]any) bool {
	for _, key := range []string{"type", "transport", "server_name", "name", "command", "url", "server_id"} {
		if _, ok := item[key]; ok {
			return true
		}
	}
	return false
}

// loadMcpsFromDir 从包相对 MCP 目录读条目（cwd 缺省取该目录）。
// Python: extension_loader.py:867-887 _load_mcps_from_dir
func loadMcpsFromDir(packageDir, relativeDir string) []hschema.McpServerSpec {
	mcpDir := relativeDir
	if !filepath.IsAbs(mcpDir) {
		mcpDir = filepath.Join(packageDir, mcpDir)
	}
	absDir, _ := filepath.Abs(mcpDir)
	for _, filename := range []string{"mcp.json", "mcps.json", "mcps.yaml"} {
		path := filepath.Join(absDir, filename)
		if info, err := os.Stat(path); err != nil || info.IsDir() {
			continue
		}
		normalized := []hschema.McpServerSpec{}
		entries, _ := mcpPayloadEntries(loadDataFile(path))
		for _, item := range entries {
			entry := normalizeMcpServerEntry(item)
			if entry != nil {
				if stringOrEmpty(entry["cwd"]) == "" {
					entry["cwd"] = absDir
				}
				var spec hschema.McpServerSpec
				if err := decodeInto(entry, &spec); err == nil {
					normalized = append(normalized, spec)
				}
			}
		}
		return normalized
	}
	return nil
}

// normalizeMcpServerEntry jiuwenswarm mcp.servers 字段 → McpServerSpec 形状 map。
// Python: extension_loader.py:890-935 _normalize_mcp_server_entry
// 返回 nil 表示条目被 enabled=false 禁用。
func normalizeMcpServerEntry(item map[string]any) map[string]any {
	raw := copyMap(item)
	if enabled, has := raw["enabled"]; has {
		delete(raw, "enabled")
		if !toBool(enabled) {
			return nil
		}
	}

	transportRaw, hasTransport := raw["transport"]
	delete(raw, "transport")
	if !hasTransport {
		transportRaw = raw["type"]
	}
	delete(raw, "type")
	transport := strings.ToLower(strings.TrimSpace(stringOrEmpty(transportRaw)))
	if transport == "" {
		transport = "stdio"
	}
	clientType, known := mcpTransportAliases[transport]
	if !known {
		// Python: raise ValueError（:899）——异常上抛由调用方转译
		return map[string]any{"__error__": fmt.Sprintf("Unsupported MCP transport/type: %q", transport)}
	}

	serverName := popStringOrEmpty(raw, "server_name")
	if serverName == "" {
		serverName = popStringOrEmpty(raw, "name")
	}
	serverID := popStringOrEmpty(raw, "server_id")
	url := popStringOrEmpty(raw, "url")
	command := strings.TrimSpace(popStringOrEmpty(raw, "command"))
	args := anyList(popValue(raw, "args"))
	env := mapOrEmpty(popValue(raw, "env"))
	cwd := popStringOrEmpty(raw, "cwd")
	params := copyMap(mapOrEmpty(popValue(raw, "params")))
	authHeaders := copyMap(mapOrEmpty(popValue(raw, "auth_headers")))
	if len(authHeaders) == 0 {
		authHeaders = copyMap(mapOrEmpty(popValue(raw, "headers")))
	}
	authQueryParams := copyMap(mapOrEmpty(popValue(raw, "auth_query_params")))
	timeoutS := popValue(raw, "timeout_s")
	if timeoutS != nil {
		if _, has := params["timeout_s"]; !has {
			params["timeout_s"] = toInt(timeoutS)
		}
	}
	// 包元数据的未知残留键忽略（如 description）。
	delete(raw, "description")
	delete(raw, "dir")

	entry := map[string]any{
		"type":              clientType,
		"server_name":       strings.TrimSpace(serverName),
		"command":           command,
		"args":              stringList(args),
		"env":               stringMap(env),
		"params":            params,
		"auth_headers":      stringMap(authHeaders),
		"auth_query_params": stringMap(authQueryParams),
	}
	if serverID != "" {
		entry["server_id"] = strings.TrimSpace(serverID)
	}
	if url != "" {
		entry["url"] = strings.TrimSpace(url)
	}
	if cwd != "" {
		entry["cwd"] = strings.TrimSpace(cwd)
	}
	return entry
}

// mergeSidecarPromptSections identity.md / soul.md / sections.yaml sidecar 合并。
// Python: extension_loader.py:938-961 _merge_sidecar_prompt_sections
func mergeSidecarPromptSections(payload map[string]any, packageDir string) {
	for _, sidecar := range []struct {
		filename string
		name     string
		priority int
	}{{"identity.md", "identity", 10}, {"soul.md", "soul", 20}} {
		path := filepath.Join(packageDir, sidecar.filename)
		if info, err := os.Stat(path); err == nil && !info.IsDir() {
			content, err := os.ReadFile(path)
			if err != nil {
				continue
			}
			mergeItems(payload, "prompt_sections", map[string]any{
				"name":     sidecar.name,
				"content":  sectionContent(string(content)),
				"priority": sidecar.priority,
			})
		}
	}

	sectionsPath := filepath.Join(packageDir, "prompt_sections", "sections.yaml")
	if info, err := os.Stat(sectionsPath); err != nil || info.IsDir() {
		return
	}
	sectionsMap, err := loadYamlMapping(sectionsPath)
	if err != nil {
		return
	}
	for _, section := range asList(sectionsMap["sections"]) {
		if sectionMap, ok := section.(map[string]any); ok {
			mergeItems(payload, "prompt_sections", normalizePromptSection(sectionMap, packageDir))
		}
	}
}

// normalizePromptSection sections.yaml 段落归一化（file 引用展开）。
// Python: extension_loader.py:964-972 _normalize_prompt_section
func normalizePromptSection(section map[string]any, packageDir string) map[string]any {
	normalized := copyMap(section)
	fileName, hasFile := normalized["file"]
	delete(normalized, "file")
	if hasFile && fileName != nil {
		sectionPath := resolveSectionFile(packageDir, fmt.Sprint(fileName))
		content, err := os.ReadFile(sectionPath)
		if err == nil {
			normalized["content"] = sectionContent(string(content))
		}
	} else if text, ok := normalized["content"].(string); ok {
		normalized["content"] = sectionContent(text)
	}
	return normalized
}

// resolveSectionFile 段落文件定位：绝对 / 包根直下 / prompt_sections/files。
// Python: extension_loader.py:975-982 _resolve_section_file
func resolveSectionFile(packageDir, fileName string) string {
	if filepath.IsAbs(fileName) {
		return fileName
	}
	direct := filepath.Join(packageDir, fileName)
	if info, err := os.Stat(direct); err == nil && !info.IsDir() {
		return direct
	}
	return filepath.Join(packageDir, "prompt_sections", "files", fileName)
}

// mergeListFile 列表文件合并进 payload 键。Python: :985-991 _merge_list_file
func mergeListFile(payload map[string]any, path, key string) {
	if info, err := os.Stat(path); err != nil || info.IsDir() {
		return
	}
	data := loadDataFile(path)
	if dataMap, ok := data.(map[string]any); ok {
		value, has := dataMap[key]
		if !has {
			value = dataMap["servers"]
		}
		data = value
	}
	mergeItems(payload, key, data)
}

// mergeItems 列表合并（None 跳过）。Python: :994-998 _merge_items
func mergeItems(payload map[string]any, key string, value any) {
	if value == nil {
		return
	}
	existing, _ := payload[key].([]any)
	payload[key] = append(existing, asList(value)...)
}

// asList 标量包装为单元素列表。Python: :1001-1006 _as_list
func asList(value any) []any {
	if value == nil {
		return nil
	}
	if list, ok := value.([]any); ok {
		return list
	}
	return []any{value}
}

// anyList 保证 []any 形状（nil → 空切片）。
func anyList(value any) []any {
	if list, ok := value.([]any); ok {
		return list
	}
	return []any{}
}

// mcpsToAny McpServerSpec 列表转回 []any（归一化中间态在 payload 中流转）。
func mcpsToAny(specs []hschema.McpServerSpec) []any {
	result := make([]any, 0, len(specs))
	for _, spec := range specs {
		encoded, err := json.Marshal(spec)
		if err != nil {
			continue
		}
		var m map[string]any
		if json.Unmarshal(encoded, &m) == nil {
			result = append(result, m)
		}
	}
	return result
}

// sectionContent 双语同值正文。Python: :1009-1010 _section_content
func sectionContent(content string) map[string]string {
	return map[string]string{"cn": content, "en": content}
}

// normalizeSkills legacy skills 归一化（dirs 展开、字符串包装）。Python: :1013-1026
func normalizeSkills(items any) []any {
	normalized := []any{}
	for _, item := range asList(items) {
		if itemMap, ok := item.(map[string]any); ok {
			if dirs, ok := itemMap["dirs"].([]any); ok {
				for _, value := range dirs {
					normalized = append(normalized, map[string]any{"dir": value})
				}
				continue
			}
		}
		normalized = append(normalized, normalizeSkill(item))
	}
	return normalized
}

// normalizeSkill 字符串 skill 包装为 {dir}。Python: :1013-1016
func normalizeSkill(item any) any {
	if text, ok := item.(string); ok {
		return map[string]any{"dir": text}
	}
	return item
}

// normalizeResourceItems 资源条目归一化列表。Python: :1029-1033
func normalizeResourceItems(items any, kind, packageDir string) []any {
	normalized := []any{}
	for _, item := range asList(items) {
		normalized = append(normalized, asList(normalizeResourceItem(item, kind, packageDir))...)
	}
	return normalized
}

// hasModuleClassResourceSpec 判断 module+class 形态。Python: :1036-1040
func hasModuleClassResourceSpec(normalized map[string]any, params map[string]any) bool {
	if _, ok := normalized["module"].(string); !ok {
		return false
	}
	if _, hasClass := normalized["class"]; hasClass {
		return true
	}
	if _, hasClassName := normalized["class_name"]; hasClassName {
		return true
	}
	return params["class_name"] != nil
}

// normalizeResourceItem 单个 legacy 资源条目归一化。Python: :1043-1082
func normalizeResourceItem(item any, kind, packageDir string) any {
	if text, ok := item.(string); ok {
		return map[string]any{"type": "core." + text, "params": map[string]any{}}
	}
	itemMap, ok := item.(map[string]any)
	if !ok {
		return item
	}

	normalized := copyMap(itemMap)
	if typeValue, _ := normalized["type"].(string); typeValue == "builtin" {
		return normalizeBuiltinResourceItem(normalized)
	}
	if typeValue, _ := normalized["type"].(string); typeValue == "entry_point" {
		return normalizeEntryPointResourceItem(normalized, kind)
	}
	if typeValue, hasType := normalized["type"]; hasType && fmt.Sprint(typeValue) != "package" {
		return normalizeParams(normalized)
	}

	params := popLegacyParams(normalized)
	fileName, hasFile := normalized["file"]
	delete(normalized, "file")
	if hasFile && fileName != nil {
		if _, has := params["file_path"]; !has {
			params["file_path"] = fileName
		}
		if classValue, hasClass := normalized["class"]; hasClass {
			delete(normalized, "class")
			if _, has := params["class_name"]; !has {
				params["class_name"] = classValue
			}
		}
		if classNameValue, hasClassName := normalized["class_name"]; hasClassName {
			delete(normalized, "class_name")
			if _, has := params["class_name"]; !has {
				params["class_name"] = classNameValue
			}
		}
		return map[string]any{"type": "harness." + kind + ".file", "params": params}
	}

	if hasModuleClassResourceSpec(normalized, params) {
		moduleName, _ := normalized["module"].(string)
		delete(normalized, "module")
		className := popStringOrEmpty(normalized, "class_name")
		if className == "" {
			className = popStringOrEmpty(normalized, "class")
		}
		if className == "" {
			className = popStringOrEmpty(params, "class_name")
		}
		filePath := moduleToPackageFile(moduleName, packageDir)
		if filePath == "" {
			if _, has := params["import_path"]; !has {
				params["import_path"] = moduleName + "." + className
			}
			return map[string]any{"type": "harness." + kind + ".import", "params": params}
		}
		if _, has := params["file_path"]; !has {
			params["file_path"] = filePath
		}
		if _, has := params["class_name"]; !has {
			params["class_name"] = className
		}
		return map[string]any{"type": "harness." + kind + ".file", "params": params}
	}

	return item
}

// normalizeBuiltinResourceItem builtin 条目展开为多个 core.<name>。Python: :1085-1094
func normalizeBuiltinResourceItem(item map[string]any) []any {
	params := popLegacyParams(item)
	names, hasNames := item["names"]
	delete(item, "names")
	if !hasNames && item["name"] != nil {
		names = item["name"]
		delete(item, "name")
	}
	normalized := []any{}
	for _, name := range asList(names) {
		normalized = append(normalized, map[string]any{"type": "core." + fmt.Sprint(name), "params": copyMap(params)})
	}
	return normalized
}

// normalizeEntryPointResourceItem entry_point 条目。Python: :1097-1102
func normalizeEntryPointResourceItem(item map[string]any, kind string) map[string]any {
	params := popLegacyParams(item)
	name, hasName := item["name"]
	delete(item, "name")
	if hasName && name != nil {
		if _, has := params["name"]; !has {
			params["name"] = name
		}
	}
	return map[string]any{"type": "harness." + kind + ".entry_point", "params": params}
}

// normalizeParams 非package已知type条目的 params 收拢。Python: :1105-1108
func normalizeParams(item map[string]any) map[string]any {
	normalized := copyMap(item)
	normalized["params"] = popLegacyParams(normalized)
	return normalized
}

// popLegacyParams params + kwargs 合并收拢。Python: :1111-1114
func popLegacyParams(item map[string]any) map[string]any {
	params := copyMap(mapOrEmpty(item["params"]))
	delete(item, "params")
	kwargs := mapOrEmpty(item["kwargs"])
	delete(item, "kwargs")
	for key, value := range kwargs {
		params[key] = value
	}
	return params
}

// moduleToPackageFile 模块名 → 包内 .py 相对路径（非包模块返回 ""）。
// Python: extension_loader.py:1117-1122 _module_to_package_file
func moduleToPackageFile(moduleName, packageDir string) string {
	prefix := "openjiuwen.extensions.harness." + filepath.Base(packageDir) + "."
	if !strings.HasPrefix(moduleName, prefix) {
		return ""
	}
	relativeModule := moduleName[len(prefix):]
	return strings.ReplaceAll(relativeModule, ".", "/") + ".py"
}

// ──────────────────────────── 15.15 通用 helpers ────────────────────────────

// decodeInto map → 结构体（JSON round-trip，pydantic model_validate 等价）。
// 已知偏差：pydantic extra="forbid" 严格性在 JSON round-trip 中不生效
// （未知键静默丢弃），登记 spec 已知偏差节。
func decodeInto(source any, target any) error {
	encoded, err := json.Marshal(source)
	if err != nil {
		return err
	}
	decoder := json.NewDecoder(bytes.NewReader(encoded))
	decoder.UseNumber()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	return nil
}

// withinDir 判断 target 是否位于 dir 之内（含相等）。
func withinDir(dir, target string) bool {
	rel, err := filepath.Rel(dir, target)
	if err != nil {
		return false
	}
	return rel == "." || (!strings.HasPrefix(rel, "..") && !filepath.IsAbs(rel))
}

// stringOrEmpty any → 非nil 字符串，nil/非字符串返回 ""。
func stringOrEmpty(value any) string {
	if text, ok := value.(string); ok {
		return text
	}
	return ""
}

// toInt any → int（容忍 float64/int64/string 数字形态）。
func toInt(value any) int {
	switch v := value.(type) {
	case int:
		return v
	case int64:
		return int(v)
	case float64:
		return int(v)
	case json.Number:
		i, _ := v.Int64()
		return int(i)
	case string:
		i, _ := strconv.Atoi(v)
		return i
	default:
		return 0
	}
}

// toBool any → bool。
func toBool(value any) bool {
	switch v := value.(type) {
	case bool:
		return v
	case string:
		return v == "true" || v == "True" || v == "1"
	default:
		return false
	}
}

// stringList []any → []string。
func stringList(list []any) []string {
	result := make([]string, 0, len(list))
	for _, item := range list {
		result = append(result, fmt.Sprint(item))
	}
	return result
}

// stringMap map[string]any → map[string]string。
func stringMap(source map[string]any) map[string]string {
	result := map[string]string{}
	for key, value := range source {
		result[key] = fmt.Sprint(value)
	}
	return result
}

// mapOrEmpty any → map[string]any（非 map 返回空 map）。
func mapOrEmpty(value any) map[string]any {
	if m, ok := value.(map[string]any); ok {
		return m
	}
	return map[string]any{}
}

// copyMap 浅拷贝 map[string]any。
func copyMap(source map[string]any) map[string]any {
	result := map[string]any{}
	for key, value := range source {
		result[key] = value
	}
	return result
}

// popStringOrEmpty 弹出字符串键值。
func popStringOrEmpty(source map[string]any, key string) string {
	value := source[key]
	delete(source, key)
	return stringOrEmpty(value)
}

// popValue 弹出任意键值。
func popValue(source map[string]any, key string) any {
	value := source[key]
	delete(source, key)
	return value
}

package equipment

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	pathutil "github.com/uapclaw/uapclaw-go/internal/common/utils/path"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_teams/experts"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_teams/external"
	"github.com/uapclaw/uapclaw-go/internal/swarm/common"
	"gopkg.in/yaml.v3"
)

// ──────────────────────────── 非导出函数 ────────────────────────────

// resolveShowPackageDir show 解析顺序 local → built_in → resources；冲突报错。
// Python: _resolve_show_package_dir (:973-1010)。非法名（reject 失败）返回 nil。
func resolveShowPackageDir(name, kindLabel string) (string, string, bool) {
	safeName, err := rejectPackageName(name, kindLabel)
	if err != nil {
		return "", "", false
	}
	localDir := filepath.Join(equipmentLocalRoot(kindForLabel(kindLabel), ""), safeName)
	builtInDir := filepath.Join(equipmentBuiltInRoot(kindForLabel(kindLabel), ""), safeName)
	resourcesRoot := resourcesKindRoot(kindForLabel(kindLabel))
	resourceDir := ""
	if resourcesRoot != "" {
		resourceDir = filepath.Join(resourcesRoot, safeName)
	}
	localExists := isDir(localDir)
	builtInExists := isDir(builtInDir)
	resourceExists := resourceDir != "" && isDir(resourceDir)
	if localExists && builtInExists {
		logger.Warn(logger.ComponentAgentServer).Str("package", safeName).
			Msg(kindLabel + " package conflict: exists in both local and built_in")
		return "", "", false
	}
	if localExists && resourceExists {
		logger.Warn(logger.ComponentAgentServer).Str("package", safeName).
			Msg(kindLabel + " package conflict: exists in both local and resources")
		return "", "", false
	}
	if localExists {
		return localDir, "local", true
	}
	if builtInExists {
		return builtInDir, "builtin", true
	}
	if resourceExists {
		return resourceDir, "builtin", true
	}
	return "", "", false
}

// kindForLabel kind 标签 → 目录名。
func kindForLabel(kindLabel string) string {
	switch kindLabel {
	case "agent_template":
		return agentTemplateKind
	case "agent_group":
		return agentGroupKind
	default:
		return pluginPackagesKind
	}
}

// isDir 目录存在判断。
func isDir(p string) bool {
	st, err := os.Stat(p)
	return err == nil && st.IsDir()
}

// mapSkills manifest skills → 能力卡。Python: _map_skills/:485 _map_skills_with_reader。
func mapSkills(pkgDir string, manifest map[string]any) []map[string]any {
	specs, _ := manifest["skills"].([]any)
	cards := []map[string]any{}
	for _, spec := range specs {
		skillID := skillIDFromSpec(spec)
		if skillID == "" {
			continue
		}
		text := readPackageText(pkgDir, "skills/"+skillID+"/SKILL.md")
		meta := map[string]any{}
		if text != "" {
			meta = parseSkillFrontmatterText(text)
		}
		name := skillID
		if n, ok := meta["name"].(string); ok && n != "" {
			name = n
		}
		desc, _ := meta["description"].(string)
		cards = append(cards, map[string]any{
			"id":                 skillID,
			"displayName":        i18nOf(name, skillID),
			"displayDescription": i18nOf(desc, ""),
			"avatar":             "",
		})
	}
	return cards
}

// skillIDFromSpec skills 条目的 skill id（目录名）。
// Python: _skill_id_from_spec。
func skillIDFromSpec(spec any) string {
	switch v := spec.(type) {
	case string:
		if strings.TrimSpace(v) != "" {
			return filepath.Base(strings.TrimSpace(v))
		}
	case map[string]any:
		raw := ""
		for _, key := range []string{"dir", "id"} {
			if s, ok := v[key].(string); ok && s != "" {
				raw = s
				break
			}
		}
		if raw != "" {
			return filepath.Base(strings.TrimSpace(raw))
		}
	}
	return ""
}

// mapClassEntries manifest tools/rails 条目 → 能力卡。
// Python: _map_class_entries (:519-541)。
func mapClassEntries(manifest map[string]any, key string) []map[string]any {
	specs, _ := manifest[key].([]any)
	cards := []map[string]any{}
	for _, spec := range specs {
		m, ok := spec.(map[string]any)
		if !ok {
			continue
		}
		entryID := ""
		if c, ok := m["class"].(string); ok && c != "" {
			entryID = c
		} else if id, ok := m["id"].(string); ok && id != "" {
			entryID = id
		}
		if strings.TrimSpace(entryID) == "" {
			continue
		}
		cards = append(cards, map[string]any{
			"id":                 strings.TrimSpace(entryID),
			"displayName":        i18nOf(m["display_name"], strings.TrimSpace(entryID)),
			"displayDescription": i18nOf(m["display_description"], ""),
		})
	}
	return cards
}

// mapMcps manifest mcps → 卡片（connector 显示名走 catalog——Go 侧无 connector
// catalog 注册表，按 Python except 分支回退 _i18n(name, name)；D-4）。
// Python: _map_mcps_with_reader (:556-650)。
func mapMcps(pkgDir string, manifest map[string]any) []map[string]any {
	cards := []map[string]any{}
	seen := map[string]bool{}
	add := func(mcpID string, displayName, displayDesc any, kind string) {
		if mcpID == "" || seen[mcpID] {
			return
		}
		seen[mcpID] = true
		cards = append(cards, map[string]any{
			"id":                 mcpID,
			"displayName":        i18nOf(displayName, mcpID),
			"displayDescription": i18nOf(displayDesc, ""),
			"kind":               kind,
		})
	}
	readMcp := func(rel string) string { return readPackageText(pkgDir, rel) }
	addFromMcpText := func(text string) {
		if text == "" {
			return
		}
		var data map[string]any
		if jsonUnmarshalStrict(text, &data) != nil || data == nil {
			return
		}
		if servers, ok := data["mcpServers"].(map[string]any); ok {
			for name := range servers {
				add(name, name, nil, "package")
			}
			return
		}
		if serverList, ok := data["servers"].([]any); ok {
			for _, server := range serverList {
				m, ok := server.(map[string]any)
				if !ok {
					continue
				}
				id := ""
				for _, key := range []string{"server_id", "name"} {
					if s, ok := m[key].(string); ok && s != "" {
						id = s
						break
					}
				}
				if id != "" {
					add(id, id, m["description"], "package")
				}
			}
		}
	}
	specs, _ := manifest["mcps"].([]any)
	for _, spec := range specs {
		m, ok := spec.(map[string]any)
		if !ok {
			continue
		}
		if connector, ok := m["connector"].(string); ok && connector != "" {
			// Python _connector_display 经 catalog 取显示名；catalog 不可用时回退
			add(connector, connector, nil, "connector")
			continue
		}
		id := ""
		for _, key := range []string{"id", "server_id", "name"} {
			if s, ok := m[key].(string); ok && s != "" {
				id = s
				break
			}
		}
		if id != "" {
			add(id, m["display_name"], m["display_description"], "package")
			continue
		}
		// Python :636-650：spec.id 为路径形态 → 读包内配置文件提 servers
		if pathVal, ok := m["path"].(string); ok && pathVal != "" {
			addFromMcpText(readMcp(strings.TrimPrefix(pathVal, "./")))
		}
	}
	_ = addFromMcpText
	return cards
}

// readPackageText 包相对文本读取（缺失返回空串）。Python: _read_package_text。
func readPackageText(pkgDir, rel string) string {
	data, err := os.ReadFile(filepath.Join(pkgDir, filepath.FromSlash(rel)))
	if err != nil {
		return ""
	}
	return string(data)
}

// readAgentTemplatePersona persona 目录首个 markdown 全文。Python: :418-430。
func readAgentTemplatePersona(pkgDir string) string {
	personaDir := filepath.Join(pkgDir, "persona")
	entries, err := os.ReadDir(personaDir)
	if err != nil {
		return ""
	}
	var names []string
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		if strings.ToLower(filepath.Ext(entry.Name())) == ".md" {
			names = append(names, entry.Name())
		}
	}
	// sorted(persona_dir.iterdir(), key=p.name)
	for i := 1; i < len(names); i++ {
		for j := i; j > 0 && names[j] < names[j-1]; j-- {
			names[j], names[j-1] = names[j-1], names[j]
		}
	}
	for _, name := range names {
		data, err := os.ReadFile(filepath.Join(personaDir, name))
		if err != nil {
			return ""
		}
		return string(data)
	}
	return ""
}

// ──────────────────────────── 导出函数 ────────────────────────────

// ShowAgentTemplate 模板详情卡；缺失/损坏返回 nil。
// Python: show_agent_template (:2752-2769) + _build_show_card (:830-881)。
func ShowAgentTemplate(name string) map[string]any {
	pkgDir, source, ok := resolveShowPackageDir(name, "agent_template")
	if !ok {
		return nil
	}
	market := marketplaceIndex(ReadAgentTemplateMarketplaceEntries())
	return buildShowCard(pkgDir, "agent_template", market[filepath.Base(pkgDir)], source)
}

// ShowAgentGroup 分组详情卡；缺失/损坏返回 nil（列表同等严格）。
// Python: show_agent_group (:2667-2693) + _build_agent_group_card(include_details=True)
// + disabledRuntimes 附加段。
func ShowAgentGroup(name string) map[string]any {
	pkgDir, source, ok := resolveShowPackageDir(name, "agent_group")
	if !ok {
		return nil
	}
	market := marketplaceIndex(ReadAgentGroupMarketplaceEntries())
	card, err := BuildAgentGroupCard(pkgDir, source, market[filepath.Base(pkgDir)], experts.LoadAgentGroupPackageBundle, true)
	if err != nil {
		return nil
	}
	card["disabledRuntimes"] = disabledAgentGroupRuntimes(pkgDir)
	return card
}

// loadGroupBundle show 卡的 bundle 装载（Python load_agent_group_package_bundle）。
func loadGroupBundle(pkgDir string) (*experts.LoadedAgentGroupPackage, error) {
	return experts.LoadAgentGroupPackageBundle(pkgDir)
}

// disabledAgentGroupRuntimes 配置开关未启用的外接 runtime 名集合。
// Python: disabled_agent_group_runtimes (:2652-2665)。
func disabledAgentGroupRuntimes(pkgDir string) []string {
	switches := externalCliSwitches()
	disabled := []string{}
	bundle, err := loadGroupBundle(pkgDir)
	if err != nil {
		return disabled
	}
	for _, template := range bundle.Templates {
		t := template
		externalSpec, extErr := external.ExternalCliAgentSpecFromTemplate(&t)
		if extErr != nil || externalSpec == nil {
			continue
		}
		name := externalSpec.CLIAgent
		if switches["external_cli_agent_"+name+"_enabled"] != "true" && !sliceContainsStr(disabled, name) {
			disabled = append(disabled, name)
		}
	}
	return disabled
}

// ──────────────────────────── buildShowCard ────────────────────────────

// buildShowCard 详情卡（无 components；skills/tools/rails/mcps 投影）。
// Python: _build_show_card (:830-881)。
func buildShowCard(pkgDir, packageType string, marketplace map[string]any, source string) map[string]any {
	manifest := readPackageManifestForShow(pkgDir)
	if manifest == nil || manifestType(manifest) != packageType {
		return nil
	}
	resolvedSource := source
	if resolvedSource == "" {
		resolvedSource = sourceFromPkgDir(pkgDir)
	}
	version, _ := manifest["version"].(string)
	tags := anyListOrEmpty(manifest["tags"])
	details := readReadmeDetails(pkgDir)
	if packageType == "agent_template" {
		details = stringOrEmpty(manifest["description"])
	}
	var displayDescription any
	if packageType == "agent_template" {
		description, _ := manifest["description"].(string)
		displayDescription = i18nOf(manifest["display_description"], strings.TrimSpace(description))
	} else {
		displayDescription = i18nOf(manifest["display_description"], "")
	}
	card := map[string]any{
		"id":                 filepath.Base(pkgDir),
		"displayName":        orDefault(stringOrEmpty(manifest["display_name"]), filepath.Base(pkgDir)),
		"displayDescription": displayDescription,
		"source":             resolvedSource,
		"avatar":             resolvePackageAvatar(pkgDir, manifest),
		"version":            version,
		"details":            details,
		"tags":               tags,
		"skills":             mapSkills(pkgDir, manifest),
		"tools":              mapClassEntries(manifest, "tools"),
		"rails":              mapClassEntries(manifest, "rails"),
		"mcps":               mapMcps(pkgDir, manifest),
	}
	if packageType == "agent_template" {
		card["persona"] = readAgentTemplatePersona(pkgDir)
	}
	installed := false
	if marketplace != nil {
		installed, _ = marketplace["installed"].(bool)
	}
	card["installed"] = installed
	card["connection_state"] = packageConnectionState(manifest, installed)
	card["pending_connectors"] = unreadyConnectors(connectorNamesFromManifest(manifest))
	quick, _ := manifest["quick_inputs"].([]any)
	if quick == nil {
		quick = []any{}
	}
	card["quickInputs"] = quick
	if packageType == "agent_template" {
		if runtime, ok := manifest["runtime"].(map[string]any); ok {
			card["runtime"] = runtime
		}
	}
	return card
}

// ──────────────────────────── 非导出助手 ────────────────────────────

// readPackageManifestForShow show 卡读 manifest（nil 透传）。
func readPackageManifestForShow(pkgDir string) map[string]any {
	manifest, _ := readPackageManifest(pkgDir)
	return manifest
}

// manifestType manifest.package_type。
func manifestType(manifest map[string]any) string {
	t, _ := manifest["package_type"].(string)
	return t
}

// sliceContainsStr 字符串集合包含判断（equipment 包内无 slices 泛型先例时使用）。
func sliceContainsStr(set []string, value string) bool {
	for _, item := range set {
		if item == value {
			return true
		}
	}
	return false
}

// configPathForEquipment config.yaml 路径（Python get_config() 同源）。
func configPathForEquipment() string {
	return pathutil.ConfigFile()
}

// externalCliSwitches 配置面板的 external_cli_agents 开关扁平表。
// Python: flatten_external_cli_agents_for_config_panel(get_config())——
// get_config 为全局配置；Go 侧读同一 config.yaml（control.readRawConfig 同口径）。
func externalCliSwitches() map[string]string {
	data, err := os.ReadFile(configPathForEquipment())
	if err != nil {
		return map[string]string{}
	}
	var loaded map[string]any
	if err := yaml.Unmarshal(data, &loaded); err != nil || loaded == nil {
		return map[string]string{}
	}
	kinds := common.ExternalCliAgentKinds()
	uploadedCLIPaths := map[string]string{}
	for _, kind := range kinds {
		if agent := common.GetCustomAgent(kind); agent != nil && agent.CLIPath != "" {
			uploadedCLIPaths[kind] = agent.CLIPath
		}
	}
	return common.FlattenExternalCliAgentsForConfigPanel(loaded, kinds, uploadedCLIPaths)
}

// jsonUnmarshalStrict JSON 严格解析（map 形态）。
func jsonUnmarshalStrict(text string, out *map[string]any) error {
	return json.Unmarshal([]byte(text), out)
}

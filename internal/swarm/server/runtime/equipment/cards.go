package equipment

import (
	"encoding/base64"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_teams/experts"
	hresources "github.com/uapclaw/uapclaw-go/internal/openuapclaw/harness/resources"
	hschema "github.com/uapclaw/uapclaw-go/internal/openuapclaw/harness/schema"
	"github.com/uapclaw/uapclaw-go/internal/swarm/server/runtime/mcp"
)

// ──────────────────────────── 常量 ────────────────────────────

// avatarMIME 头像扩展名 → MIME。Python: _AVATAR_MIME。
var avatarMIME = map[string]string{
	".png": "image/png", ".jpg": "image/jpeg", ".jpeg": "image/jpeg",
	".gif": "image/gif", ".webp": "image/webp", ".svg": "image/svg+xml",
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// i18nOf 展示值归一为 {zh, en}。Python: _i18n (:319-337)。
func i18nOf(value any, fallback string) map[string]any {
	if m, ok := value.(map[string]any); ok {
		zh, _ := m["zh"].(string)
		en, _ := m["en"].(string)
		zhOut := zh
		if zhOut == "" {
			zhOut = en
		}
		if zhOut == "" {
			zhOut = fallback
		}
		enOut := en
		if enOut == "" {
			enOut = zh
		}
		if enOut == "" {
			enOut = fallback
		}
		return map[string]any{"zh": zhOut, "en": enOut}
	}
	if s, ok := value.(string); ok && s != "" {
		return map[string]any{"zh": s, "en": s}
	}
	return map[string]any{"zh": fallback, "en": fallback}
}

// connectorNamesFromManifest manifest 托管 connector 名集合。
// Python: _connector_names_from_manifest (:652-662)。
func connectorNamesFromManifest(manifest map[string]any) []string {
	specs, _ := manifest["mcps"].([]any)
	names := []string{}
	for _, spec := range specs {
		m, ok := spec.(map[string]any)
		if !ok {
			continue
		}
		if c, ok := m["connector"].(string); ok && c != "" {
			names = append(names, c)
		}
	}
	return names
}

// connectionState 聚合包级 MCP connection_state。
// Python: _connection_state (:680-700)：无 connectors → 镜像 installed；
// 有 connectors → AND（任一 connecting=connecting，全 connected=connected，
// 否则 disconnected）。
func connectionState(names []string, installed bool) string {
	if len(names) == 0 {
		if installed {
			return "connected"
		}
		return "disconnected"
	}
	anyConnecting := false
	allConnected := true
	for _, name := range names {
		rec := mcp.GetMcpRecord(name)
		state := ""
		if rec != nil {
			state, _ = rec["state"].(string)
		}
		if state == "connecting" {
			anyConnecting = true
			allConnected = false
		} else if state != "connected" {
			allConnected = false
		}
	}
	if anyConnecting {
		return "connecting"
	}
	if allConnected {
		return "connected"
	}
	return "disconnected"
}

// packageConnectionState list/show 卡共用的包级状态。
// Python: _package_connection_state (:675-678)。
func packageConnectionState(manifest map[string]any, installed bool) string {
	return connectionState(connectorNamesFromManifest(manifest), installed)
}

// unreadyConnectors 未完全连接的 connector 名（只读）。
// Python: unready_connectors (:3078-3093)：ready 仅指 state=="connected"。
func unreadyConnectors(names []string) []string {
	unready := []string{}
	for _, raw := range names {
		name := strings.TrimSpace(raw)
		if name == "" {
			continue
		}
		rec := mcp.GetMcpRecord(name)
		if rec == nil {
			unready = append(unready, name)
			continue
		}
		if state, _ := rec["state"].(string); state != "connected" {
			unready = append(unready, name)
		}
	}
	return unready
}

// resolvePackageAvatar manifest 头像内联为 data URL。
// Python: _resolve_package_avatar (:706-736)：拒绝绝对路径/..；按扩展名取 MIME；
// 包内文件读取 base64。
func resolvePackageAvatar(pkgDir string, manifest map[string]any) string {
	raw, _ := manifest["avatar"].(string)
	rel := strings.TrimSpace(strings.ReplaceAll(raw, "\\", "/"))
	if rel == "" || strings.HasPrefix(rel, "/") || strings.Contains(rel, "..") {
		return ""
	}
	mime, ok := avatarMIME[strings.ToLower(filepath.Ext(rel))]
	if !ok {
		return ""
	}
	root, err := filepath.Abs(pkgDir)
	if err != nil {
		return ""
	}
	full, err := filepath.Abs(filepath.Join(pkgDir, rel))
	if err != nil || !subpathOf(root, full) {
		return ""
	}
	data, err := os.ReadFile(full)
	if err != nil {
		return ""
	}
	return "data:" + mime + ";base64," + base64.StdEncoding.EncodeToString(data)
}

// sourceFromPkgDir 由归属目录推导 source（built_in → builtin）。
// Python: _source_from_pkg_dir (:311-316)。
func sourceFromPkgDir(pkgDir string) string {
	if filepath.Base(filepath.Dir(pkgDir)) == "built_in" {
		return "builtin"
	}
	return "local"
}

// safeAvatarReference 成员/分组头像：data:image 放行、http(s) 放行、
// 其余按包相对路径解析。Python: _safe_avatar_reference (:1356-1372)。
func safeAvatarReference(raw any, packageDir string) string {
	value, ok := raw.(string)
	if !ok {
		return ""
	}
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	lower := strings.ToLower(value)
	if strings.HasPrefix(lower, "data:image/") {
		return value
	}
	if strings.HasPrefix(lower, "http://") || strings.HasPrefix(lower, "https://") {
		return value
	}
	return resolvePackageAvatar(packageDir, map[string]any{"avatar": value})
}

// agentTemplateTeamCompatibility 团队兼容性检查（leader 恒可、member 拒带 AGENT.md）。
// Python: _agent_template_team_compatibility (:812-825)。
func agentTemplateTeamCompatibility(pkgDir string) map[string]any {
	if _, err := hresources.LoadAgentTemplatePackage(filepath.Join(pkgDir, "manifest.json")); err != nil {
		return map[string]any{"leader": false, "member": false}
	}
	member := true
	if _, err := os.Stat(filepath.Join(pkgDir, "AGENT.md")); err == nil {
		member = false
	}
	return map[string]any{"leader": true, "member": member}
}

// ──────────────────────────── 导出函数 ────────────────────────────

// BuildAgentTemplateListCard 单个模板的列表卡（读路径）。
// Python: _build_agent_template_list_card (:771-810)。manifest 卡非法（package_type
// 不符等）返回 error；调用方跳过。
func BuildAgentTemplateListCard(pkgDir, source string, marketplace map[string]any, includeTeamCompatibility bool) (map[string]any, error) {
	manifestCard, err := hresources.ReadAgentTemplateCard(filepath.Join(pkgDir, "manifest.json"))
	if err != nil {
		return nil, err
	}
	description := strings.TrimSpace(manifestCard.Description)
	// displayDescription 已由卡片解析层做四语言回退（D-15.14-D-5）；i18n 形状对齐
	// _i18n(manifest_card.display_description, card_description)
	displayDescription := i18nOf(localizedWire(manifestCard.DisplayDescription), description)
	card := map[string]any{
		"id":                 filepath.Base(pkgDir),
		"displayName":        i18nOf(localizedWire(manifestCard.DisplayName), filepath.Base(pkgDir)),
		"displayDescription": displayDescription,
		"category":           manifestCard.Category,
		"tags":               tagListToAny(manifestCard.Tags),
		"source":             source,
		"avatar":             resolvePackageAvatar(pkgDir, map[string]any{"avatar": manifestCard.Avatar}),
	}
	if marketplace != nil {
		installed, _ := marketplace["installed"].(bool)
		card["installed"] = installed
	} else {
		card["installed"] = false
	}
	card["connection_state"] = packageConnectionState(manifestCardToManifest(&manifestCard), manifestCardBool(card["installed"]))
	if includeTeamCompatibility {
		card["teamCompatible"] = agentTemplateTeamCompatibility(pkgDir)
	}
	if manifestCard.RuntimeProvider != nil && *manifestCard.RuntimeProvider != "" {
		card["runtime"] = map[string]any{"provider_name": *manifestCard.RuntimeProvider}
	}
	return card, nil
}

// ListAgentTemplates 模板列表（本地三目录 + filter；Hub 合并段跳过，D-2）。
// Python: list_agent_templates (:2073-2098) + list_agent_templates_with_hub 本地段
// (:2299-2312：_omit_enterprise_hidden_cards + _filter_allowed_cards——D-4 保留判断结构、
// 个人版恒放行)。
func ListAgentTemplates(params map[string]any) ([]map[string]any, error) {
	includeTeamCompatibility := false
	if params != nil {
		if v, ok := params["include_team_compatibility"].(bool); ok {
			includeTeamCompatibility = v
		}
	}
	packageDirs, err := EquipmentPackageDirs(agentTemplateKind)
	if err != nil {
		return nil, err
	}
	market := marketplaceIndex(ReadAgentTemplateMarketplaceEntries())
	cards := []map[string]any{}
	for _, entry := range packageDirs {
		card, cardErr := BuildAgentTemplateListCard(
			entry.Dir, entry.Source, market[filepath.Base(entry.Dir)], includeTeamCompatibility)
		if cardErr != nil {
			logger.Warn(logger.ComponentAgentServer).
				Str("package", filepath.Base(entry.Dir)).
				Msg("[equipment] skipping unloadable agent_template package")
			continue
		}
		cards = append(cards, card)
	}
	cards = applyListSourceFilter(cards, params)
	// Python list_agent_templates_with_hub 尾段：_omit_enterprise_hidden_cards +
	// _filter_allowed_cards——个人版放行（D-4）
	return filterAllowedCards("agent_template", cards), nil
}

// BuildAgentGroupCard 分组公共卡（不泄露路径与提示词）。
// Python: _build_agent_group_card (:2529-2651)。includeDetails=true 时附
// version/updatedAt/details/persona/leaderId/quickInputs（show 用）。
func BuildAgentGroupCard(pkgDir, source string, marketplace map[string]any, loadPackage func(string) (*experts.LoadedAgentGroupPackage, error), includeDetails bool) (map[string]any, error) {
	manifest := readPackageManifestOrError(pkgDir, "agent_group")
	templates, loadErr := loadPackage(pkgDir)
	if loadErr != nil {
		return nil, loadErr
	}
	memberTemplates, _ := manifest["member_templates"].(map[string]any)
	members := []map[string]any{}
	memberIDs := sortedMemberIDs(templates.Templates)
	for _, memberID := range memberIDs {
		template := templates.Templates[memberID]
		memberCard := agentGroupMemberCard(pkgDir, memberID)
		rawTemplateID, _ := memberTemplates[memberID].(string)
		agentTemplateID := strings.TrimSpace(rawTemplateID)
		if agentTemplateID == "" {
			agentTemplateID = memberID
		}
		rawDisplayName := any("")
		if memberCard != nil {
			rawDisplayName = localizedWire(memberCard.DisplayName)
		}
		fallback := memberID
		if template.AgentCard.Name != "" {
			fallback = template.AgentCard.Name
		}
		avatar := ""
		if memberCard != nil {
			avatar = safeAvatarReference(memberCard.Avatar, filepath.Join(pkgDir, "agents", memberID))
		}
		members = append(members, map[string]any{
			"id":                 memberID,
			"agentTemplateId":    agentTemplateID,
			"displayName":        i18nOf(rawDisplayName, fallback),
			"displayDescription": i18nOf(template.AgentCard.Description, ""),
			"role":               map[bool]string{true: "leader", false: "member"}[memberID == "leader"],
			"avatar":             avatar,
		})
	}
	// 组共享 skills：按目录名去重（模板 spec 顺序；Go spec.Skills 为目录名字符串）
	skillDirs := map[string]string{}
	for _, template := range templates.Templates {
		for _, skill := range template.Skills {
			if _, exists := skillDirs[filepath.Base(skill.Dir)]; !exists {
				skillDirs[filepath.Base(skill.Dir)] = skill.Dir
			}
		}
	}
	skillIDs := make([]string, 0, len(skillDirs))
	for id := range skillDirs {
		skillIDs = append(skillIDs, id)
	}
	sort.Strings(skillIDs)
	skills := []map[string]any{}
	for _, skillID := range skillIDs {
		meta := parseSkillFrontmatter(filepath.Join(skillDirs[skillID], "SKILL.md"))
		name := skillID
		if n, ok := meta["name"].(string); ok && n != "" {
			name = n
		}
		desc, _ := meta["description"].(string)
		skills = append(skills, map[string]any{
			"id":                 skillID,
			"displayName":        i18nOf(name, skillID),
			"displayDescription": i18nOf(desc, ""),
			"avatar":             "",
		})
	}

	packageID := filepath.Base(pkgDir)
	installed := false
	if marketplace != nil {
		installed, _ = marketplace["installed"].(bool)
	}
	category, _ := manifest["category"].(string)
	card := map[string]any{
		"id":                 packageID,
		"name":               packageID,
		"displayName":        i18nOf(manifest["display_name"], packageID),
		"displayDescription": i18nOf(manifest["display_description"], stringOrEmpty(manifest["description"])),
		"category":           category,
		"tags":               anyListOrEmpty(manifest["tags"]),
		"source":             source,
		"installed":          installed,
		"avatar":             safeAvatarReference(manifest["avatar"], pkgDir),
		"memberCount":        len(members),
		"members":            members,
		"skills":             skills,
		"capabilities": map[string]any{
			"canUse":          installed,
			"canInstall":      !installed,
			"canUninstall":    installed || source == "local",
			"canPreviewFiles": true,
			"canEdit":         false,
			"canPublish":      installed,
		},
	}
	if includeDetails {
		version, _ := manifest["version"].(string)
		quickInputs := anyListOrEmpty(manifest["quick_inputs"])
		instruction, _ := manifest["instruction"].(string)
		updatedAt := manifestModTimeISO(pkgDir)
		details := readReadmeDetails(pkgDir)
		if details == "" {
			details = stringOrEmpty(manifest["description"])
		}
		card["version"] = version
		card["updatedAt"] = updatedAt
		card["details"] = details
		card["persona"] = instruction
		card["leaderId"] = "leader"
		card["quickInputs"] = quickInputs
	}
	return card, nil
}

// ListAgentGroups 分组列表（本地三目录 + filter；hub 安装状态改写段在无 Hub 记录时
// 天然跳过，D-2）。Python: list_agent_groups (:2387-2450 本地段)。
func ListAgentGroups(params map[string]any) ([]map[string]any, error) {
	// 冲突校验（Python :2396-2410：local 与 resources/built_in 同名报错）
	if _, err := EquipmentPackageDirs(agentGroupKind); err != nil {
		return nil, err
	}
	resourceDirs := iterResourcePackageDirs(agentGroupKind)
	builtInDirs := iterLocalPackageDirs(equipmentBuiltInRoot(agentGroupKind, ""))
	localDirs := iterLocalPackageDirs(equipmentLocalRoot(agentGroupKind, ""))
	// 同名校验
	localIDs := map[string]bool{}
	for _, d := range localDirs {
		localIDs[filepath.Base(d)] = true
	}
	for _, d := range resourceDirs {
		if localIDs[filepath.Base(d)] {
			return nil, &PackageConflictError{Message: "agent_group package conflict: " +
				filepath.Base(d) + " exists in local and resources"}
		}
	}
	builtInIDs := map[string]bool{}
	for _, d := range builtInDirs {
		builtInIDs[filepath.Base(d)] = true
	}
	for _, d := range resourceDirs {
		if builtInIDs[filepath.Base(d)] {
			return nil, &PackageConflictError{Message: "agent_group package conflict: " +
				filepath.Base(d) + " exists in local and built_in"}
		}
	}
	// 候选排序：按 marketplace updated_at_ns → manifest mtime 降序，再按名（Python newest_first）
	market := marketplaceIndex(ReadAgentGroupMarketplaceEntries())
	type candidate struct {
		source string
		dir    string
	}
	var candidates []candidate
	for _, d := range resourceDirs {
		candidates = append(candidates, candidate{source: "builtin", dir: d})
	}
	for _, d := range builtInDirs {
		if !resourceIDs(filepath.Base(d), resourceDirs) {
			candidates = append(candidates, candidate{source: "builtin", dir: d})
		}
	}
	for _, d := range localDirs {
		candidates = append(candidates, candidate{source: "local", dir: d})
	}
	sort.Slice(candidates, func(i, j int) bool {
		return groupNewestFirst(candidates[i].dir, market) > groupNewestFirst(candidates[j].dir, market)
	})
	cards := []map[string]any{}
	for _, cand := range candidates {
		card, err := BuildAgentGroupCard(cand.dir, cand.source, market[filepath.Base(cand.dir)],
			experts.LoadAgentGroupPackageBundle, false)
		if err != nil {
			logger.Warn(logger.ComponentAgentServer).
				Str("package", filepath.Base(cand.dir)).
				Err(err).
				Msg("Skipping unloadable agent_group package")
			continue
		}
		cards = append(cards, card)
	}
	cards = applyListSourceFilter(cards, params)
	return filterAllowedCards("agent_group", cards), nil
}

// ──────────────────────────── 非导出函数（投影助手） ────────────────────────────

// applyListSourceFilter 目录/我的 来源过滤。
// Python: _apply_list_source_filter (:2041-2071)：
// builtin：source==builtin；builtin+hub：builtin/hub（D-2 下恒空集）；
// mine：source==local 或 installed；local：local 或（builtin/hub 且 installed）。
func applyListSourceFilter(cards []map[string]any, params map[string]any) []map[string]any {
	if params == nil {
		return cards
	}
	raw, ok := params["filter"].(string)
	if !ok {
		return cards
	}
	switch raw {
	case "builtin":
		out := []map[string]any{}
		for _, card := range cards {
			if s, _ := card["source"].(string); s == "builtin" {
				out = append(out, card)
			}
		}
		return out
	case "builtin+hub":
		out := []map[string]any{}
		for _, card := range cards {
			s, _ := card["source"].(string)
			if s == "builtin" || s == "hub" {
				out = append(out, card)
			}
		}
		return out
	case "mine":
		out := []map[string]any{}
		for _, card := range cards {
			s, _ := card["source"].(string)
			installed, _ := card["installed"].(bool)
			if s == "local" || installed {
				out = append(out, card)
			}
		}
		return out
	case "local":
		out := []map[string]any{}
		for _, card := range cards {
			s, _ := card["source"].(string)
			installed, _ := card["installed"].(bool)
			if s == "local" || ((s == "builtin" || s == "hub") && installed) {
				out = append(out, card)
			}
		}
		return out
	}
	return cards
}

// filterAllowedCards 企业版允许名单过滤——个人版恒放行（D-4：保留判断结构）。
// Python: _filter_allowed_cards (:2504-2516)。
func filterAllowedCards(kind string, cards []map[string]any) []map[string]any {
	return cards
}

// tagListToAny 卡片 tags → payload。
func tagListToAny(tags []hresources.LocalizedText) []any {
	out := make([]any, 0, len(tags))
	for _, t := range tags {
		out = append(out, t)
	}
	return out
}

// manifestCardToManifest 卡片回填 manifest 形状（connection_state 消费用）。
func manifestCardToManifest(card *hresources.AgentTemplateCard) map[string]any {
	manifest := map[string]any{}
	if len(card.Connectors) > 0 {
		specs := make([]any, 0, len(card.Connectors))
		for _, c := range card.Connectors {
			specs = append(specs, map[string]any{"connector": c})
		}
		manifest["mcps"] = specs
	}
	return manifest
}

// manifestCardBool any bool 安全读取。
func manifestCardBool(v any) bool {
	b, _ := v.(bool)
	return b
}

// anyListOrEmpty any 列表安全读取。
func anyListOrEmpty(v any) []any {
	items, _ := v.([]any)
	if items == nil {
		return []any{}
	}
	return items
}

// stringOrEmpty any 字符串安全读取。
func stringOrEmpty(v any) string {
	s, _ := v.(string)
	return s
}

// readPackageManifestOrError manifest 缺失/损坏 → error。
// Python: _build_agent_group_card 头部（:2546-2550）。
func readPackageManifestOrError(pkgDir, kindLabel string) map[string]any {
	manifest, _ := readPackageManifest(pkgDir)
	if manifest == nil {
		// Python: raise ValueError(f"agent_group package missing/corrupt manifest.json")
		// Go 侧以空 manifest 继续会导致下游 panic——这里回传空表并由调用方
		// load_package 失败兜底（LoadAgentGroupPackageBundle 对缺失 manifest 报错）。
		return map[string]any{}
	}
	return manifest
}

// sortedMemberIDs 模板成员名排序（leader 优先展示由 role 字段承载）。
func sortedMemberIDs(templates map[string]hschema.AgentTemplateSpec) []string {
	ids := make([]string, 0, len(templates))
	for id := range templates {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

// resourceIDs 目录名集合助手。
func resourceIDs(name string, dirs []string) bool {
	for _, d := range dirs {
		if filepath.Base(d) == name {
			return true
		}
	}
	return false
}

// groupNewestFirst 排序键：marketplace updated_at_ns → manifest mtime。
// Python: newest_first (:2440-2450)。
func groupNewestFirst(pkgDir string, market map[string]map[string]any) int64 {
	if entry := market[filepath.Base(pkgDir)]; entry != nil {
		if ns, ok := toInt64(entry["updated_at_ns"]); ok {
			return ns
		}
	}
	info, err := os.Stat(filepath.Join(pkgDir, "manifest.json"))
	if err != nil {
		return 0
	}
	return info.ModTime().UnixNano()
}

// toInt64 数值转换（JSON 解码 float64）。
func toInt64(v any) (int64, bool) {
	switch n := v.(type) {
	case float64:
		return int64(n), true
	case int64:
		return n, true
	case int:
		return int64(n), true
	}
	return 0, false
}

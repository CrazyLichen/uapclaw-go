package equipment

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/uapclaw/uapclaw-go/internal/common/workspace"
)

// rejectPackageNameAny any → 包名校验（对齐 Python _reject_package_name(name: Any)
// 的 str() 强转语义 + 绝对路径拒绝段；agent_group 错误包装 AGENT_GROUP_NAME_INVALID）。
func rejectPackageNameAny(name any, kind string) (string, error) {
	var raw string
	switch v := name.(type) {
	case string:
		raw = strings.TrimSpace(v)
	case nil:
		raw = ""
	default:
		raw = strings.TrimSpace(fmt.Sprint(v))
	}
	// Python :133-136：绝对路径段（Go 原切片函数未覆盖，随本批对齐补齐）
	if err := rejectAbsolutePackageName(raw, kind); err != nil {
		return "", err
	}
	got, err := rejectPackageName(raw, kind)
	if err != nil {
		if kind == "agent_group" {
			return "", &AgentGroupPackageError{Message: err.Error(), Code: "AGENT_GROUP_NAME_INVALID"}
		}
		return "", err
	}
	return got, nil
}

// rejectAbsolutePackageName 绝对路径包名拒绝（Windows 盘符 + unix 绝对路径）。
// Python: _reject_package_name :133-136。
func rejectAbsolutePackageName(raw, kind string) error {
	if strings.HasPrefix(raw, "/") {
		return absoluteNameError(raw, kind)
	}
	// Windows 盘符形态（C:/ 或 C:\）
	if len(raw) >= 2 && raw[1] == ':' {
		return absoluteNameError(raw, kind)
	}
	return nil
}

// absoluteNameError 绝对路径错误（agent_group 包装）。
func absoluteNameError(raw, kind string) error {
	message := "invalid " + kind + " name (absolute): " + raw
	if kind == "agent_group" {
		return &AgentGroupPackageError{Message: message, Code: "AGENT_GROUP_NAME_INVALID"}
	}
	return &PackageConflictError{Message: message}
}

// ──────────────────────────── 类型 ────────────────────────────

// UnloadLiveFunc 运行会话已加载设备的卸载缝（Task 8 换真实现）。
// Python: interface.py:6016 _unload_live_equipment 属性探测语义。
type UnloadLiveFunc func(kind, packageID string) error

// noopUnloadLive 空实现（adapter 缺失 no-op 语义）。
func noopUnloadLive(kind, packageID string) error { return nil }

// ──────────────────────────── 导出函数 ────────────────────────────

// CreateAgentTemplate 创建本地专家包。
// Python: create_agent_template (:3735-3784)。
func CreateAgentTemplate(params map[string]any) error {
	packageID, err := rejectPackageNameAny(params["id"], "agent_template")
	if err != nil {
		return err
	}
	name, err := requireNonEmptyStr(params, "name")
	if err != nil {
		return err
	}
	description, err := requireNonEmptyStr(params, "description")
	if err != nil {
		return err
	}
	persona, err := requireNonEmptyStr(params, "persona")
	if err != nil {
		return err
	}
	skillNames, err := requireSkillNames(params)
	if err != nil {
		return err
	}
	mcpNames, err := requireMcpNames(params)
	if err != nil {
		return err
	}
	quickInputs, err := requireQuickInputs(params)
	if err != nil {
		return err
	}
	tags, err := requireTags(params)
	if err != nil {
		return err
	}
	runtime, err := requireFormRuntime(params)
	if err != nil {
		return err
	}
	tags, mcpNames, err = applyFormRuntime(tags, mcpNames, runtime)
	if err != nil {
		return err
	}
	if err := assertPackageIDAvailable(packageID, agentTemplateKind); err != nil {
		return err
	}
	pkgDir := filepath.Join(equipmentLocalRoot(agentTemplateKind, ""), packageID)
	if err := os.MkdirAll(equipmentLocalRoot(agentTemplateKind, ""), 0o755); err != nil {
		return err
	}
	// mkdir(exist_ok=False) 语义：目录必须不存在
	if err := os.Mkdir(pkgDir, 0o755); err != nil {
		return err
	}
	if writeErr := writeAgentTemplatePackage(pkgDir, packageID, name, description,
		persona, skillNames, mcpNames, quickInputs, tags, runtime); writeErr != nil {
		_ = os.RemoveAll(pkgDir) // Python except: rmtree(pkg_dir, ignore_errors=True)
		return writeErr
	}
	return UpsertAgentTemplateMarketplaceEntry(packageID, false, "local")
}

// UpdateAgentTemplate 原地更新 local 模板（builtin/resources/hub 拒绝）。
// Python: update_agent_template (:3785-3846)。
func UpdateAgentTemplate(params map[string]any, unloadLive UnloadLiveFunc) error {
	packageID, err := rejectPackageNameAny(params["id"], "agent_template")
	if err != nil {
		return err
	}
	name, err := requireNonEmptyStr(params, "name")
	if err != nil {
		return err
	}
	description, err := requireNonEmptyStr(params, "description")
	if err != nil {
		return err
	}
	persona, err := requireNonEmptyStr(params, "persona")
	if err != nil {
		return err
	}
	skillNames, err := requireSkillNames(params)
	if err != nil {
		return err
	}
	mcpNames, err := requireMcpNames(params)
	if err != nil {
		return err
	}
	quickInputs, err := requireQuickInputs(params)
	if err != nil {
		return err
	}
	tags, err := requireTags(params)
	if err != nil {
		return err
	}
	runtime, err := requireFormRuntime(params)
	if err != nil {
		return err
	}
	tags, mcpNames, err = applyFormRuntime(tags, mcpNames, runtime)
	if err != nil {
		return err
	}
	pkgDir := filepath.Join(equipmentLocalRoot(agentTemplateKind, ""), packageID)
	if !isDir(pkgDir) {
		return &PackageConflictError{Message: "agent_template not found: " + packageID}
	}
	// 来源校验：仅 local 可编辑（:3817-3827）
	existing := findMarketplaceEntry(ReadAgentTemplateMarketplaceEntries(), packageID)
	if existing != nil {
		if source, _ := (*existing)["source"].(string); source != "local" {
			return &PackageConflictError{Message: fmt.Sprintf(
				"only local packages can be edited: %s (source=%q)", packageID, source)}
		}
	}
	// 先卸载运行会话已加载的模板（Python :3106-3112 分派层前置；此处对齐调用时机）
	if err := unloadLive(agentTemplateKind, packageID); err != nil {
		return err
	}
	// 原地重写失败无部分状态（Python :3832-3834 注释）
	if writeErr := writeAgentTemplatePackage(pkgDir, packageID, name, description,
		persona, skillNames, mcpNames, quickInputs, tags, runtime); writeErr != nil {
		return writeErr
	}
	installed := false
	if existing != nil {
		installed, _ = (*existing)["installed"].(bool)
	}
	return UpsertAgentTemplateMarketplaceEntry(packageID, installed, "local")
}

// DeleteAgentTemplate 删除 local 模板（含 marketplace 条目；builtin/hub 拒绝）。
// Python: delete_agent_template (:3847-3885)。
func DeleteAgentTemplate(params map[string]any, unloadLive UnloadLiveFunc) error {
	packageID, err := rejectPackageNameAny(params["id"], "agent_template")
	if err != nil {
		return err
	}
	pkgDir := filepath.Join(equipmentLocalRoot(agentTemplateKind, ""), packageID)
	if !isDir(pkgDir) {
		return &PackageConflictError{Message: "agent_template not found: " + packageID}
	}
	existing := findMarketplaceEntry(ReadAgentTemplateMarketplaceEntries(), packageID)
	if existing != nil {
		if source, _ := (*existing)["source"].(string); source != "local" {
			return &PackageConflictError{Message: fmt.Sprintf(
				"only local packages can be deleted: %s (source=%q)", packageID, source)}
		}
	}
	if err := unloadLive(agentTemplateKind, packageID); err != nil {
		return err
	}
	if err := os.RemoveAll(pkgDir); err != nil {
		return err
	}
	return RemoveAgentTemplateMarketplaceEntry(packageID)
}

// findMarketplaceEntry 按包名找条目。
func findMarketplaceEntry(entries []map[string]any, packageID string) *map[string]any {
	for i := range entries {
		if id, _ := entries[i]["id"].(string); id == packageID {
			return &entries[i]
		}
	}
	return nil
}

// ──────────────────────────── 分组创建 ────────────────────────────

// AgentGroupPackageError 分组建包专有错误（Python AgentGroupPackageError）。
type AgentGroupPackageError struct {
	// Message 信息
	Message string
	// Code 机器码（AGENT_GROUP_*）
	Code string
}

// Error 实现 error 接口。
func (e *AgentGroupPackageError) Error() string { return e.Message }

// ErrorCode 机器码读取（分派层分类用）。
func (e *AgentGroupPackageError) ErrorCode() string { return e.Code }

// requireAgentGroupMembers 校验 leader/成员专家 ID。
// Python: _require_agent_group_members (:3888-3931)。
func requireAgentGroupMembers(params map[string]any) (string, []string, error) {
	rawLeader, _ := params["leaderId"].(string)
	if strings.TrimSpace(rawLeader) == "" {
		return "", nil, &AgentGroupPackageError{Message: "missing or invalid leaderId", Code: "AGENT_GROUP_LEADER_REQUIRED"}
	}
	leaderID, err := rejectPackageNameAny(rawLeader, "leaderId")
	if err != nil {
		return "", nil, &AgentGroupPackageError{Message: err.Error(), Code: "AGENT_GROUP_LEADER_REQUIRED"}
	}
	rawMembers, ok := params["memberIds"].([]any)
	if !ok {
		return "", nil, &AgentGroupPackageError{Message: "missing or invalid memberIds", Code: "AGENT_GROUP_MEMBERS_REQUIRED"}
	}
	members := []string{}
	seen := map[string]bool{}
	for _, rawMember := range rawMembers {
		s, ok := rawMember.(string)
		if !ok || strings.TrimSpace(s) == "" {
			return "", nil, &AgentGroupPackageError{Message: "invalid memberId", Code: "AGENT_GROUP_MEMBER_INVALID"}
		}
		memberID, mErr := rejectPackageNameAny(strings.TrimSpace(s), "memberId")
		if mErr != nil {
			return "", nil, &AgentGroupPackageError{Message: mErr.Error(), Code: "AGENT_GROUP_MEMBER_INVALID"}
		}
		if memberID == leaderID {
			return "", nil, &AgentGroupPackageError{Message: "leaderId must not appear in memberIds", Code: "AGENT_GROUP_LEADER_MEMBER_CONFLICT"}
		}
		if memberID == "leader" {
			return "", nil, &AgentGroupPackageError{Message: "memberId 'leader' is reserved", Code: "AGENT_GROUP_MEMBER_RESERVED"}
		}
		if seen[memberID] {
			return "", nil, &AgentGroupPackageError{Message: "duplicate memberId: " + memberID, Code: "AGENT_GROUP_MEMBER_DUPLICATE"}
		}
		seen[memberID] = true
		members = append(members, memberID)
	}
	return leaderID, members, nil
}

// agentTemplateSource 解析并校验分组选中的一个专家定义。
// Python: _agent_template_source (:3945-3990)。
func agentTemplateSource(packageID, role string) (string, error) {
	resolved, source, ok := resolveShowPackageDir(packageID, "agent_template")
	if !ok {
		// Hub asset id 兼容回退（Python :3957-3964；D-1 下 record 恒 nil）
		return "", &AgentGroupPackageError{
			Message: role + " agent_template not found: " + packageID,
			Code:    "AGENT_GROUP_MEMBER_NOT_FOUND"}
	}
	_ = source
	manifest := readPackageManifestForShow(resolved)
	if manifest == nil || manifestType(manifest) != "agent_template" {
		return "", &AgentGroupPackageError{
			Message: role + " agent_template is invalid: " + packageID,
			Code:    "AGENT_GROUP_TEMPLATE_INVALID"}
	}
	if _, loadErr := loadTemplateSpec(resolved); loadErr != nil {
		return "", &AgentGroupPackageError{
			Message: role + " agent_template is not Team-compatible: " + packageID,
			Code:    "AGENT_GROUP_MEMBER_INCOMPATIBLE"}
	}
	if role == "member" {
		if _, err := os.Stat(filepath.Join(resolved, "AGENT.md")); err == nil {
			return "", &AgentGroupPackageError{
				Message: "member agent_template contains unsupported AGENT.md: " + packageID,
				Code:    "AGENT_GROUP_MEMBER_AGENT_MD"}
		}
	}
	return resolved, nil
}

// assertAgentGroupNameAvailable 拒绝同名显示名（casefold）。
// Python: _assert_agent_group_name_available (:1912-1933)。
func assertAgentGroupNameAvailable(name string) error {
	normalizedName := strings.ToLower(name)
	var packageDirs []string
	packageDirs = append(packageDirs, iterResourcePackageDirs(agentGroupKind)...)
	packageDirs = append(packageDirs, iterLocalPackageDirs(equipmentBuiltInRoot(agentGroupKind, ""))...)
	packageDirs = append(packageDirs, iterLocalPackageDirs(equipmentLocalRoot(agentGroupKind, ""))...)
	for _, packageDir := range packageDirs {
		manifest, _ := readPackageManifest(packageDir)
		if manifest == nil {
			continue
		}
		displayNames := i18nOf(manifest["display_name"], filepath.Base(packageDir))
		for _, value := range displayNames {
			if s, ok := value.(string); ok && strings.ToLower(strings.TrimSpace(s)) == normalizedName {
				return &AgentGroupPackageError{
					Message: "agent_group display name already exists: " + name + " (" + filepath.Base(packageDir) + ")",
					Code:    "AGENT_GROUP_DUPLICATE"}
			}
		}
	}
	return nil
}

// copyTreeWithoutSymlinks 拒绝符号链接后整树拷贝。
// Python: _copytree_without_symlinks (:2028-2033) + _assert_no_symlinks (:2016-2026)。
func copyTreeWithoutSymlinks(src, dst string) error {
	if err := assertNoSymlinks(src); err != nil {
		return err
	}
	return copyTreeHelper(src, dst)
}

// assertNoSymlinks 包子树内拒绝任何符号链接。
// Python: _assert_no_symlinks (:2016-2026)。
func assertNoSymlinks(src string) error {
	if isSymlink(src) {
		return &PackageConflictError{Message: "symbolic links are not allowed in package: " + filepath.Base(src)}
	}
	return filepath.Walk(src, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return &PackageConflictError{Message: "failed to inspect package: " + filepath.Base(src)}
		}
		if isSymlink(p) {
			return &PackageConflictError{Message: "symbolic links are not allowed in package: " + filepath.Base(src)}
		}
		return nil
	})
}

// CreateAgentGroup 在 .agent_teams 下创建未安装的分组定义。
// Python: create_agent_group (:3933-4043)。
func CreateAgentGroup(params map[string]any) (map[string]any, error) {
	for _, forbidden := range []string{"model", "mcps", "pluginNames", "apiKey", "apiBase", "path"} {
		if _, exists := params[forbidden]; exists {
			return nil, &PackageConflictError{Message: "unsupported agent_group field: " + forbidden}
		}
	}
	packageID, err := rejectPackageNameAny(params["id"], "agent_group")
	if err != nil {
		return nil, err
	}
	name, err := requireNonEmptyStr(params, "name")
	if err != nil {
		return nil, err
	}
	description, err := requireNonEmptyStr(params, "description")
	if err != nil {
		return nil, err
	}
	persona, err := requireNonEmptyStr(params, "persona")
	if err != nil {
		return nil, err
	}
	leaderID, memberIDs, err := requireAgentGroupMembers(params)
	if err != nil {
		return nil, err
	}
	skillItems, err := requireSkillNames(params)
	if err != nil {
		return nil, err
	}
	// Python :3941 dict.fromkeys 去重保序
	skillNames := dedupe(skillItems)
	quickInputs, err := requireQuickInputs(params)
	if err != nil {
		return nil, err
	}
	tags, err := requireAgentGroupTags(params)
	if err != nil {
		return nil, err
	}
	category := ""
	if c, ok := params["category"].(string); ok {
		category = strings.TrimSpace(c)
	} else if _, exists := params["category"]; exists && params["category"] != nil {
		if _, isStr := params["category"].(string); !isStr {
			return nil, &PackageConflictError{Message: "invalid category"}
		}
	}

	sources := map[string]string{"leader": ""}
	resolved, srcErr := agentTemplateSource(leaderID, "leader")
	if srcErr != nil {
		return nil, srcErr
	}
	sources["leader"] = resolved
	for _, memberID := range memberIDs {
		resolved, srcErr := agentTemplateSource(memberID, "member")
		if srcErr != nil {
			return nil, srcErr
		}
		sources[memberID] = resolved
	}

	if err := assertPackageIDAvailable(packageID, agentGroupKind); err != nil {
		return nil, err
	}
	if err := assertAgentGroupNameAvailable(name); err != nil {
		return nil, err
	}

	localRoot := equipmentLocalRoot(agentGroupKind, "")
	if err := os.MkdirAll(localRoot, 0o755); err != nil {
		return nil, err
	}
	// staging：临时容器 + 阶段目录 + 校验加载 + 原子落位（Python :4016-4041）
	container, mkErr := os.MkdirTemp(localRoot, ".agent_group_create_")
	if mkErr != nil {
		return nil, mkErr
	}
	defer os.RemoveAll(container)
	stage := filepath.Join(container, packageID)
	destination := filepath.Join(localRoot, packageID)
	if err := os.Mkdir(stage, 0o755); err != nil {
		return nil, err
	}
	agentsDir := filepath.Join(stage, "agents")
	if err := os.Mkdir(agentsDir, 0o755); err != nil {
		return nil, err
	}
	// Python :4012-4015：leader 先拷（花名册顺序 leader + members）
	if err := copyTreeWithoutSymlinks(sources["leader"], filepath.Join(agentsDir, "leader")); err != nil {
		return nil, err
	}
	for _, memberID := range memberIDs {
		if err := copyTreeWithoutSymlinks(sources[memberID], filepath.Join(agentsDir, memberID)); err != nil {
			return nil, err
		}
	}
	leaderRules := "# " + name + "\n\n" +
		"你是专家团 Leader。负责理解目标、拆解任务、协调成员、核验结论，" +
		"并向用户提交完整的综合结果。\n"
	leaderRulesPath := filepath.Join(agentsDir, "leader", "AGENT.md")
	if _, err := os.Stat(leaderRulesPath); err != nil {
		if err := os.WriteFile(leaderRulesPath, []byte(leaderRules), 0o644); err != nil {
			return nil, err
		}
	}
	if len(skillNames) > 0 {
		if err := os.Mkdir(filepath.Join(stage, "skills"), 0o755); err != nil {
			return nil, err
		}
		for _, skillName := range skillNames {
			if err := copyTreeWithoutSymlinks(
				filepath.Join(workspace.AgentSkillsDir(), skillName),
				filepath.Join(stage, "skills", skillName)); err != nil {
				return nil, err
			}
		}
	}
	memberTemplates := map[string]any{"leader": leaderID}
	for _, memberID := range memberIDs {
		memberTemplates[memberID] = memberID
	}
	manifest := map[string]any{
		"name":                packageID,
		"package_type":        "agent_group",
		"description":         description,
		"display_name":        map[string]any{"zh": name, "en": name},
		"display_description": map[string]any{"zh": description, "en": description},
		"instruction":         persona,
		"agents":              listAnyFromStrings(append([]string{"leader"}, memberIDs...)),
		"member_templates":    memberTemplates,
		"skills":              listAnyFromStrings(skillNames),
	}
	if category != "" {
		manifest["category"] = category
	}
	if len(tags) > 0 {
		manifest["tags"] = tagsToAny(tags)
	}
	if len(quickInputs) > 0 {
		manifest["quick_inputs"] = quickInputsToAny(quickInputs)
	}
	if err := writeJSONFile(filepath.Join(stage, "manifest.json"), manifest); err != nil {
		return nil, err
	}
	if err := os.WriteFile(filepath.Join(stage, "README.md"),
		[]byte("# "+name+"\n\n"+description+"\n"), 0o644); err != nil {
		return nil, err
	}
	// Python :4034：load_agent_group_package(stage) 严格校验
	if _, err := loadGroupBundleStrict(stage); err != nil {
		return nil, err
	}
	if err := os.Rename(stage, destination); err != nil {
		return nil, err
	}
	if err := UpsertAgentGroupMarketplaceEntry(packageID, false, "local"); err != nil {
		_ = os.RemoveAll(destination) // Python :4037-4040 回滚
		return nil, err
	}
	return map[string]any{"id": packageID}, nil
}

// requireAgentGroupTags 分组标签（保留前端 id）。Python: _require_agent_group_tags (:1857-1890)。
func requireAgentGroupTags(params map[string]any) ([]map[string]any, error) {
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
		tagID, _ := m["id"].(string)
		zh, _ := m["zh"].(string)
		en, _ := m["en"].(string)
		if strings.TrimSpace(zh) == "" || strings.TrimSpace(en) == "" {
			return nil, &PackageConflictError{Message: "invalid tag: zh/en must be non-empty strings"}
		}
		entry := map[string]any{"zh": strings.TrimSpace(zh), "en": strings.TrimSpace(en)}
		if strings.TrimSpace(tagID) != "" {
			entry["id"] = strings.TrimSpace(tagID)
		}
		key := entry["id"].(string) + "|" + entry["zh"].(string) + "|" + entry["en"].(string)
		if seen[key] {
			continue
		}
		seen[key] = true
		entries = append(entries, entry)
	}
	return entries, nil
}

// dedupe 保序去重。
func dedupe(items []string) []string {
	seen := map[string]bool{}
	out := []string{}
	for _, item := range items {
		if !seen[item] {
			seen[item] = true
			out = append(out, item)
		}
	}
	return out
}

// listAnyFromStrings []string → []any。
func listAnyFromStrings(items []string) []any {
	out := make([]any, 0, len(items))
	for _, s := range items {
		out = append(out, s)
	}
	return out
}

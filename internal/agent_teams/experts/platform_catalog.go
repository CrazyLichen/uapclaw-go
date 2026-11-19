// Python 源：jiuwenswarm/agents/harness/team/experts/catalog.py（133 行 1:1）
//          + extension_package_manager.py 的 scoped 路径/校验符号
//          （expert_package_roots:1028-1035 / _kind_root:1017-1027 /
//          _marketplace_path:1046 / _local_root:1042 / _built_in_root:1038 /
//          _reject_package_name:111-140；AgentGroupPackageError 简化为
//          ExternalCliError 同款 error 类型——偏差 D-15.16-e12）。
//
// 定位已安装的 agent_template / agent_group 包。marketplace.json 的 plugins
// 条目 + local/built_in 双根。aiofiles 异步沿 D-15b 同步化。

package experts

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/resources"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	"github.com/uapclaw/uapclaw-go/internal/common/utils/path"
)

// ──────────────────────────── 常量 ────────────────────────────

const (
	// expertCatalogLogComponent 平台 catalog 日志。
	expertCatalogLogComponent = logger.ComponentAgentServer

	// agentTemplateKind Python: _AGENT_TEMPLATE_KIND (extension_package_manager.py:93)
	agentTemplateKind = "agent_templates"
	// agentGroupKind Python: _AGENT_GROUP_KIND (extension_package_manager.py:94)
	agentGroupKind = "agent_groups"
)

// expertSources 合法安装来源闭集。Python: _SOURCES (catalog.py:38)
var expertSources = map[string]bool{"builtin": true, "local": true, "hub": true}

// ──────────────────────────── 结构体 ────────────────────────────

// JiuwenExpertCatalog 定位已安装的 agent_template / agent_group 包。
// Python: JiuwenExpertCatalog (catalog.py:41-54)
type JiuwenExpertCatalog struct{}

// ListExpertPackages Python: list_expert_packages (:44-46)
func (JiuwenExpertCatalog) ListExpertPackages() []PackageLocation {
	locations, _ := listInstalled(agentTemplateKind)
	return locations
}

// LocateExpert Python: locate_expert (:48-50)
func (JiuwenExpertCatalog) LocateExpert(name string) (PackageLocation, error) {
	return locateInstalled(agentTemplateKind, name)
}

// ListGroupPackages Python: list_group_packages (:52-54)
func (JiuwenExpertCatalog) ListGroupPackages() []PackageLocation {
	locations, _ := listInstalled(agentGroupKind)
	return locations
}

// LocateGroup Python: locate_group (:56-58)
func (JiuwenExpertCatalog) LocateGroup(name string) (PackageLocation, error) {
	return locateInstalled(agentGroupKind, name)
}

// ──────────────────────────── 导出函数 ────────────────────────────

// InstallExpertCatalog 把 catalog 注入进程级专家缝（leader 得到专家工具）。
// Python: install_expert_catalog (catalog.py:118-125)——经
// TeamRuntimeManager.set_expert_catalog 注入；Go 侧注入点即
// SetProcessExpertCatalog（工具注册消费见 T5）。
func InstallExpertCatalog() {
	SetProcessExpertCatalog(JiuwenExpertCatalog{})
}

// ExpertPackageRoots 返回 (marketplace.json, local/, built_in/)。
// Python: expert_package_roots (extension_package_manager.py:1028-1035)
func ExpertPackageRoots(packageType string) (string, string, string) {
	kind := map[string]string{"agent_template": agentTemplateKind, "agent_group": agentGroupKind}[packageType]
	root := kindRoot(kind)
	return filepath.Join(root, "marketplace.json"), filepath.Join(root, "local"), filepath.Join(root, "built_in")
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// kindRoot 能力种类的根目录。
// Python: _kind_root (extension_package_manager.py:1017-1027)
// agent_workspace（<home>/agent/workspace）非空时 home 取 parent.parent；
// 否则 get_user_workspace_dir()（→ Go path.WorkspaceDir()）。组包在
// <home>/.agent_groups；模板在 equipment workspace 的 plugins/<kind>。
func kindRoot(kind string) string {
	if kind == agentGroupKind {
		home := path.WorkspaceDir()
		return filepath.Join(home, ".agent_teams", agentGroupKind)
	}
	// Python: _equipment_workspace(agent_workspace) / "plugins" / kind——
	// equipment workspace = get_agent_workspace_dir()（<home>/agent/workspace）。
	return filepath.Join(path.WorkspaceDir(), "agent", "workspace", "plugins", kind)
}

// marketplaceEntries marketplace 文件的 plugins 条目；缺失或损坏读作空。
// Python: _marketplace_entries (catalog.py:59-73)
func marketplaceEntries(marketplacePath string) []map[string]any {
	data, err := os.ReadFile(marketplacePath)
	if err != nil {
		return nil
	}
	var payload map[string]any
	if err := json.Unmarshal(data, &payload); err != nil {
		logger.Warn(expertCatalogLogComponent).
			Str("path", marketplacePath).
			Msg("[expert-catalog] unreadable marketplace")
		return nil
	}
	plugins, _ := payload["plugins"].([]any)
	entries := []map[string]any{}
	for _, entry := range plugins {
		if m, ok := entry.(map[string]any); ok {
			entries = append(entries, m)
		}
	}
	return entries
}

// packageDir 已解析包目录（与 resolve_agent_group_dir /
// resolve_agent_template_dir 同款判定）。
// Python: _package_dir (catalog.py:76-82)
func packageDir(root string, name string) (string, bool) {
	candidate := filepath.Join(root, name)
	if stat, err := os.Stat(candidate); err != nil || !stat.IsDir() {
		return "", false
	}
	resolved, err := filepath.EvalSymlinks(candidate)
	if err != nil {
		return candidate, true
	}
	return resolved, true
}

// location 解析一个已安装 marketplace 条目；目录不可用时报错。
// Python: _location (catalog.py:85-105)
func location(packageType string, entry map[string]any) (PackageLocation, error) {
	name, err := rejectPackageName(entry["id"], packageType)
	if err != nil {
		return PackageLocation{}, err
	}
	_, localRoot, builtInRoot := ExpertPackageRoots(packageType)
	localDir, hasLocal := packageDir(localRoot, name)
	builtInDir, hasBuiltIn := packageDir(builtInRoot, name)
	if hasLocal && hasBuiltIn {
		return PackageLocation{}, fmt.Errorf("%s package conflict: %s exists in both local and built_in", packageType, name)
	}
	pkgPath := localDir
	if !hasLocal {
		pkgPath = builtInDir
	}
	if pkgPath == "" {
		return PackageLocation{}, fmt.Errorf("%s package directory missing: %s", packageType, name)
	}
	source, _ := entry["source"].(string)
	if !expertSources[source] {
		if hasLocal {
			source = "local"
		} else {
			source = "builtin"
		}
	}
	return PackageLocation{Name: name, Path: pkgPath, Source: source}, nil
}

// listInstalled 每个已安装（installed=true）且目录可用的包。
// Python: _list_installed (catalog.py:108-120)
func listInstalled(packageType string) ([]PackageLocation, error) {
	marketplace, _, _ := ExpertPackageRoots(packageType)
	locations := []PackageLocation{}
	seen := map[string]bool{}
	for _, entry := range marketplaceEntries(marketplace) {
		id, _ := entry["id"].(string)
		if entry["installed"] != true || seen[id] {
			continue
		}
		seen[id] = true
		loc, err := location(packageType, entry)
		if err != nil {
			logger.Warn(expertCatalogLogComponent).
				Str("package_type", packageType).
				Str("id", id).
				Msg("[expert-catalog] skip")
			continue
		}
		locations = append(locations, loc)
	}
	return locations, nil
}

// locateInstalled 按名定位已安装包；未安装报错。
// Python: _locate_installed (catalog.py:123-131)
func locateInstalled(packageType string, name string) (PackageLocation, error) {
	marketplace, _, _ := ExpertPackageRoots(packageType)
	for _, entry := range marketplaceEntries(marketplace) {
		id, _ := entry["id"].(string)
		if id == name && entry["installed"] == true {
			return location(packageType, entry)
		}
	}
	return PackageLocation{}, fmt.Errorf("%s package not installed: %s", packageType, name)
}

// rejectPackageName 拒绝空、点、分隔符、绝对路径的包名
// （AgentGroup 名错误以 AGENT_GROUP_NAME_INVALID 语义包装——Go 统一 error
// 消息承载，D-15.16-e12）。
// Python: _reject_package_name (extension_package_manager.py:111-140)
func rejectPackageName(name any, kind string) (string, error) {
	raw := strings.TrimSpace(stringOr(name))
	if raw == "" {
		return "", fmt.Errorf("invalid %s name: empty", kind)
	}
	if raw == "." || raw == ".." {
		return "", fmt.Errorf("invalid %s name: %s", kind, raw)
	}
	if strings.HasPrefix(raw, ".") {
		return "", fmt.Errorf("invalid %s name (hidden): %s", kind, raw)
	}
	if strings.ContainsAny(raw, "/\\") {
		return "", fmt.Errorf("invalid %s name (path separator): %s", kind, raw)
	}
	if filepath.IsAbs(raw) || isWindowsAbs(raw) {
		return "", fmt.Errorf("invalid %s name (absolute): %s", kind, raw)
	}
	return raw, nil
}

// ──────────────────────────── 装配链 scoped 符号（D-15.16-e5） ────────────────────────────

// ResolveAgentGroupDir 解析一个已安装的 AgentGroup 包目录（Team 装配用）：
// 名校验 → marketplace installed 查询 → 目录定位 → manifest 校验。
// Python: resolve_agent_group_dir (extension_package_manager.py:1321-1355)；
// scoped 取用（D-15.16-e12：extension_package_manager 主体 ⤴️扩展包管理章节）。
func ResolveAgentGroupDir(name string) (string, error) {
	safeName, err := rejectPackageName(name, "agent_group")
	if err != nil {
		return "", err
	}
	loc, locErr := locateInstalled(agentGroupKind, safeName)
	if locErr != nil {
		// Python: AgentGroupPackageError("agent_group package not installed", ...)
		return "", fmt.Errorf("agent_group package not installed: %s", safeName)
	}
	manifest, mErr := ReadManifestFile(filepath.Join(loc.Path, "manifest.json"))
	if mErr != nil {
		return "", fmt.Errorf("agent_group package missing/corrupt manifest.json: %s", safeName)
	}
	if manifest["package_type"] != "agent_group" {
		return "", fmt.Errorf("agent_group package wrong package_type: %s (expected 'agent_group', got %v)", safeName, manifest["package_type"])
	}
	if manifestName, _ := manifest["name"].(string); manifestName != safeName {
		return "", fmt.Errorf("agent_group package name mismatch: directory=%q, manifest=%q", safeName, manifestName)
	}
	return loc.Path, nil
}

// ReadManifestFile 读 JSON 映射文件的导出包装（供同链跨文件校验）。
func ReadManifestFile(path string) (map[string]any, error) {
	return readMapping(path, "agent_group manifest")
}

// ResolveAgentGroupMemberDisplayName 解析成员的中文兼容展示名：
// display_name 优先 → 成员名 → fallback；取 zh 值。
// 完整 i18n 面（多语言 helper）⤴️扩展包管理章节（D-15.16-e5）。
// Python: resolve_agent_group_member_display_name
// (extension_package_manager.py:376-388 → :339-360)
func ResolveAgentGroupMemberDisplayName(packageDir string, memberID string, fallback string) string {
	card, err := ReadAgentGroupCard(packageDir)
	if err != nil {
		return strings.TrimSpace(fallback)
	}
	member := card.Member(memberID)
	if member == nil {
		return strings.TrimSpace(fallback)
	}
	// Python: localized zh 取值链（display_name → name → fallback）
	zh := resources.LocalizedTextOf(member.Card.DisplayName, "zh")
	if strings.TrimSpace(zh) == "" {
		zh = member.Card.Name
	}
	if strings.TrimSpace(zh) == "" {
		zh = fallback
	}
	return strings.TrimSpace(zh)
}

package equipment

import (
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	"github.com/uapclaw/uapclaw-go/internal/common/utils/path"
	"github.com/uapclaw/uapclaw-go/internal/common/workspace"
)

// ──────────────────────────── 常量 ────────────────────────────

// agentGroupKind agent_group 包目录名。
// Python: extension_package_manager.py:95 _AGENT_GROUP_KIND
const agentGroupKind = "agent_groups"

// pluginPackagesKind plugin_packages 包目录名。
// Python: extension_package_manager.py:97 同段 _PLUGIN_PACKAGE_KIND
const pluginPackagesKind = "plugin_packages"

// ──────────────────────────── 结构体 ────────────────────────────

// PackageDirEntry 三目录扫描的一个包条目。
// Python: _equipment_package_dirs 返回的 (package dir, source) 元组。
type PackageDirEntry struct {
	// Dir 包目录绝对路径
	Dir string
	// Source 来源标记："builtin"（resources/built_in）| "local"
	Source string
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// agentGroupKindRoot 分组 kind 根：<home>/.agent_teams/agent_groups。
// Python: _kind_root (:1017-1023) agent_groups 分支——agent_workspace 非空时
// home 取 parent.parent，否则 get_user_workspace_dir()。
func agentGroupKindRoot(agentWorkspace string) string {
	if agentWorkspace != "" {
		home := filepath.Dir(filepath.Dir(agentWorkspace))
		return filepath.Join(home, ".agent_teams", agentGroupKind)
	}
	return filepath.Join(path.WorkspaceDir(), ".agent_teams", agentGroupKind)
}

// equipmentKindRoot kind 对应的设备根目录。
// Python: _kind_root (:1017-1025)：agent_groups → .agent_teams；
// 其余（agent_templates/plugin_packages）→ <agent_workspace>/plugins/<kind>。
// 注：experts/platform_catalog.go 的 kindRoot 是本函数在专家目录缝的副本
// （Python catalog.py 亦自行持有；Go 侧避免 experts→equipment 反向环，D-15.16-e12 既有口径）。
func equipmentKindRoot(kind, agentWorkspace string) string {
	if kind == agentGroupKind {
		return agentGroupKindRoot(agentWorkspace)
	}
	if agentWorkspace != "" {
		return filepath.Join(agentWorkspace, "plugins", kind)
	}
	return filepath.Join(workspace.AgentWorkspaceDir(), "plugins", kind)
}

// equipmentBuiltInRoot built_in 子目录。Python: _built_in_root (:1038-1040)。
func equipmentBuiltInRoot(kind, agentWorkspace string) string {
	return filepath.Join(equipmentKindRoot(kind, agentWorkspace), "built_in")
}

// equipmentLocalRoot local 子目录。Python: _local_root (:1042-1044)。
func equipmentLocalRoot(kind, agentWorkspace string) string {
	return filepath.Join(equipmentKindRoot(kind, agentWorkspace), "local")
}

// equipmentMarketplacePath marketplace.json 路径。
// Python: _marketplace_path (:1046-1048)。
func equipmentMarketplacePath(kind, agentWorkspace string) string {
	return filepath.Join(equipmentKindRoot(kind, agentWorkspace), "marketplace.json")
}

// resourcesPluginsRoot 发行包内 resources/agent/workspace/plugins；不存在返回空串。
// Python: _resources_plugins_root (:142-152)——沿源码树向上找 resources/agent/workspace/plugins；
// Go 侧对应可执行文件工作目录下的 resources（与打包布局一致，对齐 15.18a 既有 resources 定位口径）。
func resourcesPluginsRoot() string {
	candidates := []string{
		filepath.Join("resources", "agent", "workspace", "plugins"),
		filepath.Join("bin", "resources", "agent", "workspace", "plugins"),
	}
	for _, candidate := range candidates {
		if st, err := os.Stat(candidate); err == nil && st.IsDir() {
			abs, absErr := filepath.Abs(candidate)
			if absErr != nil {
				return ""
			}
			return abs
		}
	}
	return ""
}

// resourcesKindRoot 发行包 resources 货架的 kind 子目录；不存在返回空串。
// Python: _resources_root (:1050) / get_equipment_resources_*_dir (:152-175)。
func resourcesKindRoot(kind string) string {
	plugins := resourcesPluginsRoot()
	if plugins == "" {
		return ""
	}
	p := filepath.Join(plugins, kind)
	if st, err := os.Stat(p); err != nil || !st.IsDir() {
		return ""
	}
	return p
}

// iterResourcePackageDirs 发行包货架上的合法包目录（按名排序；跳过 dot 开头、
// built_in/local 保留名、无 manifest.json 的目录）。
// Python: _iter_resource_package_dirs (:881-906)。
func iterResourcePackageDirs(kind string) []string {
	root := resourcesKindRoot(kind)
	if root == "" {
		return []string{}
	}
	return iterPackageDirsFiltered(root, true)
}

// iterLocalPackageDirs 用户 local/ 下的包目录（按名排序；跳过 dot 开头；
// 不要求 manifest.json）。Python: _iter_local_package_dirs (:908-922)。
func iterLocalPackageDirs(localRoot string) []string {
	return iterPackageDirsFiltered(localRoot, false)
}

// iterPackageDirsFiltered 目录扫描公共段（Python 两个 iter 的循环体）。
func iterPackageDirsFiltered(root string, requireManifest bool) []string {
	entries, err := os.ReadDir(root)
	if err != nil {
		return []string{}
	}
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			names = append(names, entry.Name())
		}
	}
	sort.Strings(names)
	out := []string{}
	for _, name := range names {
		if strings.HasPrefix(name, ".") {
			continue
		}
		if requireManifest && (name == "built_in" || name == "local") {
			continue
		}
		dir := filepath.Join(root, name)
		if requireManifest {
			if _, err := os.Stat(filepath.Join(dir, "manifest.json")); err != nil {
				continue
			}
		}
		out = append(out, dir)
	}
	return out
}

// ──────────────────────────── 导出函数 ────────────────────────────

// EquipmentPackageDirs 三目录扫描合并（resources → built_in → local）+ 冲突校验。
// Python: _equipment_package_dirs (:923-958)：
//   - local 与 resources 同名 → "package conflict: %s exists in both local and resources"
//   - local 与 built_in 同名 → "package conflict: %s exists in both local and built_in"
//   - resources 与 built_in 同名 → 不报错（resources 优先）
func EquipmentPackageDirs(kind string) ([]PackageDirEntry, error) {
	resourceDirs := iterResourcePackageDirs(kind)
	resourceIDs := map[string]bool{}
	for _, pkgDir := range resourceDirs {
		resourceIDs[filepath.Base(pkgDir)] = true
	}
	// Python :933-943：仅 resources + local 入列；built_in 只作冲突校验对象
	// （built_in 的列表化由 list_agent_groups 等调用方单独处理）
	found := make([]PackageDirEntry, 0, len(resourceDirs)+2)
	for _, pkgDir := range resourceDirs {
		found = append(found, PackageDirEntry{Dir: pkgDir, Source: "builtin"})
	}
	localDirs := iterLocalPackageDirs(equipmentLocalRoot(kind, ""))
	for _, pkgDir := range localDirs {
		name := filepath.Base(pkgDir)
		if resourceIDs[name] {
			logger.Warn(logger.ComponentAgentServer).Str("package", name).
				Msg("package conflict: exists in both local and resources")
			return nil, &PackageConflictError{
				Message: "package conflict: " + name + " exists in both local and resources"}
		}
		if _, err := os.Stat(filepath.Join(equipmentBuiltInRoot(kind, ""), name)); err == nil {
			logger.Warn(logger.ComponentAgentServer).Str("package", name).
				Msg("package conflict: exists in both local and built_in")
			return nil, &PackageConflictError{
				Message: "package conflict: " + name + " exists in both local and built_in"}
		}
		found = append(found, PackageDirEntry{Dir: pkgDir, Source: "local"})
	}
	return found, nil
}

// ──────────────────────────── 结构体（错误） ────────────────────────────

// PackageConflictError 包目录冲突（Python ValueError 语义）。
type PackageConflictError struct {
	// Message 错误信息
	Message string
}

// Error 实现 error 接口。
func (e *PackageConflictError) Error() string { return e.Message }

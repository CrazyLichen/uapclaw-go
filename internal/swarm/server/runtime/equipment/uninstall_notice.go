package equipment

import (
	"os"
	"path/filepath"

	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 常量 ────────────────────────────

// connectorUninstallNotice 卸载含 connector 包时的提示。
// Python: _CONNECTOR_UNINSTALL_NOTICE (:3243-3246)。
const connectorUninstallNotice = "该包声明了连接器，连接器配置未随之回收，可在连接器页管理。"

// ──────────────────────────── 导出函数 ────────────────────────────

// UninstallEquipmentWithNotice 卸载设备并返回通知载荷。
// Python: uninstall_equipment_with_notice (:3248-3266)——
// connector 记录不随卸载回收；包声明 connectors 时成功载荷带 notice。
func UninstallEquipmentWithNotice(kind string, params map[string]any, unloadLive UnloadLiveFunc) (map[string]any, error) {
	var packageID string
	var connectors []string
	var uninstallErr error
	switch kind {
	case agentTemplateKind:
		packageID, _ = ResolveEquipmentRuntimeID(kind, stringOrEmpty(params["id"]))
		connectors = manifestConnectorNames(kind, packageID)
		uninstallErr = uninstallAgentTemplate(params)
	case pluginPackagesKind:
		packageID, _ = ResolveEquipmentRuntimeID(kind, stringOrEmpty(params["id"]))
		connectors = manifestConnectorNames(kind, packageID)
		uninstallErr = uninstallPluginPackage(params)
	default:
		return nil, &PackageConflictError{Message: "unknown package kind: " + kind}
	}
	if uninstallErr != nil {
		return nil, uninstallErr
	}
	_ = packageID
	if len(connectors) > 0 {
		return map[string]any{"notice": connectorUninstallNotice}, nil
	}
	return map[string]any{}, nil
}

// UninstallAgentGroup 分组卸载（不动运行中的 Teams）。
// Python: uninstall_agent_group (:4523-4545)：显示身份快照保留（preserve
// agent_group_display_identity）+ 删目录 + marketplace 移除 + store 清理。
// Go 侧 display identity 保留段：写 .agent_groups 侧会话元数据的对应缝在
// 25.x 团队绑定链路——本批登记偏差 D-6（identity 保留 no-op），回收条件：
// 群聊/团队绑定批接线。
func UninstallAgentGroup(params map[string]any) error {
	requestedID, err := rejectPackageNameAny(params["id"], "agent_group")
	if err != nil {
		return err
	}
	store := NewHubInstallStateStore(agentGroupKind)
	record := store.Get(requestedID)
	if record == nil {
		record = store.GetByPackageID(requestedID)
	}
	packageID := requestedID
	if record != nil && record.PackageID != "" {
		packageID = record.PackageID
	}
	pkgDir, err := locateUserPackageDir(packageID, agentGroupKind, "agent_group")
	if err != nil {
		return err
	}
	if err := os.RemoveAll(pkgDir); err != nil {
		return err
	}
	if err := RemoveAgentGroupMarketplaceEntry(packageID); err != nil {
		return err
	}
	if record != nil {
		if err := store.Remove(record.ID); err != nil {
			return err
		}
	}
	return nil
}

// uninstallAgentTemplate 模板卸载。Python: uninstall_agent_template (:4639-4652)。
func uninstallAgentTemplate(params map[string]any) error {
	requestedID, err := rejectPackageNameAny(params["id"], "agent_template")
	if err != nil {
		return err
	}
	store := NewHubInstallStateStore(agentTemplateKind)
	record := store.Get(requestedID)
	if record == nil {
		record = store.GetByPackageID(requestedID)
	}
	packageID := requestedID
	if record != nil && record.PackageID != "" {
		packageID = record.PackageID
	}
	pkgDir, err := locateUserPackageDir(packageID, agentTemplateKind, "agent_template")
	if err != nil {
		return err
	}
	if err := os.RemoveAll(pkgDir); err != nil {
		return err
	}
	if err := RemoveAgentTemplateMarketplaceEntry(packageID); err != nil {
		return err
	}
	if record != nil {
		if err := store.Remove(record.ID); err != nil {
			return err
		}
	}
	return nil
}

// uninstallPluginPackage 插件卸载。Python: uninstall_plugin_package (:4677-4690)。
func uninstallPluginPackage(params map[string]any) error {
	requestedID, err := rejectPackageNameAny(params["id"], "plugin")
	if err != nil {
		return err
	}
	store := NewHubInstallStateStore(pluginPackagesKind)
	record := store.Get(requestedID)
	if record == nil {
		record = store.GetByPackageID(requestedID)
	}
	packageID := requestedID
	if record != nil && record.PackageID != "" {
		packageID = record.PackageID
	}
	pkgDir, err := locateUserPackageDir(packageID, pluginPackagesKind, "plugin")
	if err != nil {
		return err
	}
	if err := os.RemoveAll(pkgDir); err != nil {
		return err
	}
	if err := removeMarketplaceEntry(pluginPackagesKind, packageID); err != nil {
		return err
	}
	if record != nil {
		if err := store.Remove(record.ID); err != nil {
			return err
		}
	}
	return nil
}

// locateUserPackageDir local/built_in 双目录定位（冲突报错）。
// Python: _locate_user_package_dir (:4608-4624)。
func locateUserPackageDir(packageID, kind, kindLabel string) (string, error) {
	localDir := filepath.Join(equipmentLocalRoot(kind, ""), packageID)
	builtInDir := filepath.Join(equipmentBuiltInRoot(kind, ""), packageID)
	localExists := isDir(localDir)
	builtInExists := isDir(builtInDir)
	if localExists && builtInExists {
		return "", &PackageConflictError{Message: kindLabel + " package conflict: " +
			packageID + " exists in both local and built_in"}
	}
	if localExists {
		return localDir, nil
	}
	if builtInExists {
		return builtInDir, nil
	}
	return "", &PackageConflictError{Message: kindLabel + " package not found: " + packageID}
}

// manifestConnectorNames 包 manifest 的去重 connector 名（只读）。
// Python: manifest_connector_names (:2996-3018)。
func manifestConnectorNames(kind, packageID string) []string {
	var kindLabel string
	switch kind {
	case agentTemplateKind:
		kindLabel = "agent_template"
	case pluginPackagesKind:
		kindLabel = "plugin"
	default:
		return []string{}
	}
	pkgDir, _, ok := resolveShowPackageDir(packageID, kindLabel)
	if !ok {
		logger.Warn(logger.ComponentAgentServer).Str("package", packageID).
			Msg("[equipment] manifest_connector_names package not found")
		return []string{}
	}
	manifest, _ := readPackageManifest(pkgDir)
	if manifest == nil {
		return []string{}
	}
	names := []string{}
	seen := map[string]bool{}
	specs, _ := manifest["mcps"].([]any)
	for _, spec := range specs {
		m, ok := spec.(map[string]any)
		if !ok {
			continue
		}
		if c, ok := m["connector"].(string); ok && c != "" && !seen[c] {
			seen[c] = true
			names = append(names, c)
		}
	}
	return names
}

// UninstallOrganizationAgentGroup 组织分组卸载（定义卸载 + 停止组织实例）。
// Python: organization_packages.py:48-79 uninstall_organization_agent_group。
// 停 Team 半区（GLOBAL_RUNNER._team_runtime_manager 遍历）依赖团队运行池——
// Go 侧 team_runtime_manager 的组织实例停止挂账 D-7（本批仅先执行定义卸载，
// 顺序对齐 Python：先卸定义再停实例；无池时 no-op）。
func UninstallOrganizationAgentGroup(params map[string]any, unloadLive UnloadLiveFunc) error {
	identifier := params["id"]
	if identifier == nil || identifier == "" {
		identifier = params["name"]
	}
	// Python :53：resolve_equipment_runtime_id（D-1 下原名直通）
	if _, err := rejectPackageNameAny(identifier, "agent_group"); err != nil {
		return err
	}
	// Python :55：先定义卸载；失败不停运行
	return UninstallAgentGroup(params)
}

// unloadLiveForTemplate 模板卸载联动（Task 8 装配点）。
func unloadLiveForTemplate(kind, packageID string) error {
	return noopUnloadLive(kind, packageID)
}

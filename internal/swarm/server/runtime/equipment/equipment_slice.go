// Python 源：jiuwenswarm/server/runtime/extension_package_manager.py（15.18a 最小切片）。
// 仅移植三个纯本地函数（agent_template_runtime :244-283 / resolve_agent_template_dir
// :1302-1305 / resolve_equipment_runtime_id :3269-3274）及其直接依赖的路径助手；
// Hub 市场/下载/安装/卸载/预览半区（~4600 行）不移植，挂账 ⤴️扩展包管理章节回收
// （15.18a 等待回填）。专家包来源=内置目录或手工放置 local 目录，无在线下载。
//
// 偏差（登记 D-18-a-4）：Python 切片函数位于 server/runtime 包内，Go 侧因 runtime 包
// 反向依赖 adapter（image_modality_warmup）构成 import cycle，下沉为叶子子包 equipment。
package equipment

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/uapclaw/uapclaw-go/internal/common/workspace"
	hresources "github.com/uapclaw/uapclaw-go/internal/openuapclaw/harness/resources"
	hschema "github.com/uapclaw/uapclaw-go/internal/openuapclaw/harness/schema"
)

// ──────────────────────────── 常量 ────────────────────────────

// agentTemplateKind agent_template 包目录名。
// Python: extension_package_manager.py:93 _AGENT_TEMPLATE_KIND
const agentTemplateKind = "agent_templates"

// agentTemplateKindLabel 包名错误文案的 kind 标签。
// Python: resolve_agent_template_dir :1304 kind_label="agent_template"
const agentTemplateKindLabel = "agent_template"

// agentTemplateRuntimeProviders External Harness 允许的 provider 闭集。
// Python: extension_package_manager.py:97 _EXTERNAL_HARNESS_PROVIDERS = {claudecode, codex, dsh}；
// 偏差 D-18-1：codebuddy 为 Go 侧新增（Python 无对应物），对齐 15.22 收编口径（用户 12-07 拍板）。
var agentTemplateRuntimeProviders = []string{"claudecode", "codex", "dsh", "codebuddy"}

// SetAgentTemplateRuntimeProvidersForTest 仅供测试注入 provider 闭集变体
// （集成测试以 fake provider 名走全链路时使用）；生产代码不得调用。
// 对齐 Python 侧测试对 _EXTERNAL_HARNESS_PROVIDERS 的 monkeypatch 语义。
func SetAgentTemplateRuntimeProvidersForTest(names []string) func() {
	previous := agentTemplateRuntimeProviders
	agentTemplateRuntimeProviders = names
	return func() { agentTemplateRuntimeProviders = previous }
}

// ──────────────────────────── 导出函数 ────────────────────────────

// AgentTemplateRuntime 校验专家卡片的 runtime 声明并规范化。
// Python: extension_package_manager.py:244-283 agent_template_runtime（卡片形态分支
// :258-263 + 尾部 provider 闭集门卫 :271-276）。
// 偏差 D-18-a-1：入参收窄为已加载卡片（Python 另收 raw dict / AgentTemplateSpec；
// dict 形态在 Go 侧不可达——15.14 卡片已结构化，spec 形态见 AgentTemplateRuntimeFromSpec）。
func AgentTemplateRuntime(card hresources.AgentTemplateCard) (*hschema.AgentRuntimeSpec, error) {
	// 卡片形态：runtime 为 "native"（非空 dict 未声明）→ 无外部 runtime；
	// 否则以 manifest.runtime.provider_name + manifest.runtime.config 构造。
	var runtime *hschema.AgentRuntimeSpec
	if card.Runtime != "native" {
		provider := ""
		if card.RuntimeProvider != nil {
			provider = *card.RuntimeProvider
		}
		spec, err := validateRuntimeSpec(&hschema.AgentRuntimeSpec{
			ProviderName: provider,
			Config:       card.RuntimeConfig,
		})
		if err != nil {
			return nil, err
		}
		runtime = spec
	}
	return gateRuntimeProviders(runtime)
}

// AgentTemplateRuntimeFromSpec 校验已加载 spec 的 runtime 声明。
// Python: extension_package_manager.py:255-257（AgentTemplateSpec 形态直取
// manifest.runtime，模型层已校验）+ :271-276 尾部闭集门卫。
// 偏差 D-18-a-1 同上：Go 侧拆出独立入口以区分两种已加载形态。
func AgentTemplateRuntimeFromSpec(spec *hschema.AgentTemplateSpec) (*hschema.AgentRuntimeSpec, error) {
	return gateRuntimeProviders(spec.Runtime)
}

// ResolveAgentTemplateDir 解析已安装专家包目录（默认 agent workspace）。
// Python: extension_package_manager.py:1302-1305 resolve_agent_template_dir。
func ResolveAgentTemplateDir(name string) (string, error) {
	return resolvePackageDir(name, agentTemplateWorkspaceRoot(""), agentTemplateKindLabel, agentTemplateKindLabel)
}

// ResolveAgentTemplateDirIn 指定 agent workspace 根的解析入口（测试注入用）。
// Python 无对应形参（Python 经全局 get_agent_workspace_dir()；Go 侧 workspace根
// 经参数注入以便测试隔离，行为等价）。
func ResolveAgentTemplateDirIn(name, agentWorkspace string) (string, error) {
	return resolvePackageDir(name, agentTemplateWorkspaceRoot(agentWorkspace), agentTemplateKindLabel, agentTemplateKindLabel)
}

// ResolveEquipmentRuntimeID 把 Hub 资产 UUID 翻译成本地运行时包标识。
// Python: extension_package_manager.py:3269-3274 resolve_equipment_runtime_id。
// 偏差 D-18-a-2：hub 安装状态存储（_hub_install_state_store）属下载/市场半区未移植，
// 本实现恒走「查表 miss 原样返回」分支；回收点 ⤴️扩展包管理章节（15.18a）。
func ResolveEquipmentRuntimeID(kind, identifier string) (string, error) {
	// Python: _lifecycle_package_id({"id": identifier}, _hub_asset_kind(kind)) :2034-2038
	// ——取 params.id 并按 rejectPackageName 校验（hub asset kind 作错误文案 kind）。
	return rejectPackageName(identifier, hubAssetKind(kind))
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// validateRuntimeSpec 复刻 RuntimeSpec.model_validate 的两个 field_validator。
// Python: harness/schema/extension_spec.py:94-103（provider_name strip 非空；
// config 仅 plain data——Go 侧 RuntimeConfig 已是 map[string]any，无转换步）。
func validateRuntimeSpec(spec *hschema.AgentRuntimeSpec) (*hschema.AgentRuntimeSpec, error) {
	if strings.TrimSpace(spec.ProviderName) == "" {
		return nil, fmt.Errorf("provider_name must be a non-empty string")
	}
	spec.ProviderName = strings.TrimSpace(spec.ProviderName)
	return spec, nil
}

// gateRuntimeProviders 尾部 provider 闭集门卫（三种形态共用）。
// Python: extension_package_manager.py:271-276。
func gateRuntimeProviders(runtime *hschema.AgentRuntimeSpec) (*hschema.AgentRuntimeSpec, error) {
	if runtime == nil {
		return nil, nil
	}
	if !sliceContains(agentTemplateRuntimeProviders, runtime.ProviderName) {
		return nil, fmt.Errorf(
			"runtime.provider_name must be one of: %s; got %q",
			strings.Join(agentTemplateRuntimeProviders, ", "), runtime.ProviderName)
	}
	return runtime, nil
}

// sliceContains 有序集合包含判断（frozenset 成员测试等价）。
func sliceContains(set []string, value string) bool {
	for _, item := range set {
		if item == value {
			return true
		}
	}
	return false
}

// rejectPackageName 拒绝空、点、隐藏、路径分隔符包名。
// Python: extension_package_manager.py:111-133 _reject_package_name。
// 偏差（登记 D-18-a-3）：agent_group 分支的 AgentGroupPackageError 类型属 agent_group
// 半区未移植，切片只涉 agent_template，非法名一律 ValueError 等价 error；回收点补。
func rejectPackageName(name, kind string) (string, error) {
	raw := strings.TrimSpace(name)
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
	return raw, nil
}

// hubAssetKind runtime 包 kind → Hub 资产 kind 标签（仅错误文案用）。
// Python: extension_package_manager.py:2108-2116 _hub_asset_kind；
// plugin 分支属挂账半区不迁，未知 kind 报错对齐。
func hubAssetKind(kind string) string {
	switch kind {
	case agentTemplateKind:
		return "agent_template"
	default:
		return kind
	}
}

// agentTemplateWorkspaceRoot 计算 agent_templates 的设备工作区根。
// Python: _equipment_workspace :1012-1014（agent_workspace 为空取 get_agent_workspace_dir）
// + _kind_root :1017-1026 的 agent_template 分支（<equipment_ws>/plugins/agent_templates）；
// agent_group 分支（:1018-1023）属 agent_group 半区不迁。
func agentTemplateWorkspaceRoot(agentWorkspace string) string {
	if agentWorkspace != "" {
		return filepath.Join(agentWorkspace, "plugins", agentTemplateKind)
	}
	// Python: get_agent_workspace_dir()；Go 等价 workspace.AgentWorkspaceDir()
	// （15.16c projectless_workspace.go 已按此口径接线）。
	return filepath.Join(workspace.AgentWorkspaceDir(), "plugins", agentTemplateKind)
}

// readPackageManifest 解析包目录下 manifest.json；缺失/损坏返回 nil。
// Python: extension_package_manager.py:230-241 _read_package_manifest。
func readPackageManifest(pkgDir string) (map[string]any, error) {
	manifestPath := filepath.Join(pkgDir, "manifest.json")
	data, err := os.ReadFile(manifestPath)
	if err != nil {
		return nil, nil // 缺失 → None（调用方判 missing/corrupt）
	}
	var parsed map[string]any
	if err := json.Unmarshal(data, &parsed); err != nil {
		return nil, nil // 损坏 → None
	}
	return parsed, nil
}

// packageDirIfPresent 根目录存在候选包目录时返回其绝对路径；不存在返回空。
// Python: extension_package_manager.py:1215-1224 _package_dir_if_present（相对逃逸守卫保留）。
func packageDirIfPresent(root, safeName string) (string, error) {
	info, err := os.Stat(root)
	if err != nil || !info.IsDir() {
		return "", nil
	}
	rootResolved, err := filepath.EvalSymlinks(root)
	if err != nil {
		return "", nil
	}
	candidate := filepath.Join(root, safeName)
	candidateResolved, err := filepath.EvalSymlinks(candidate)
	if err != nil {
		// 目录不存在：Python resolve() 不要求存在，is_relative_to 仍成立后 is_dir() 为假
		// → 返回 None。Go 用 Abs 近似（safeName 已被 rejectPackageName 拦掉分隔符）。
		candidateResolved, _ = filepath.Abs(candidate)
	} else if !subpathOf(rootResolved, candidateResolved) {
		return "", fmt.Errorf("invalid package path (escapes root): %s", safeName)
	}
	if info, err := os.Stat(candidateResolved); err == nil && info.IsDir() {
		return candidateResolved, nil
	}
	return "", nil
}

// subpathOf 判断 child 是否位于 parent 之下（is_relative_to 等价）。
func subpathOf(parent, child string) bool {
	rel, err := filepath.Rel(parent, child)
	if err != nil {
		return false
	}
	return rel == "." || (!strings.HasPrefix(rel, "..") && !filepath.IsAbs(rel))
}

// validatePackageManifest 校验候选包目录的 manifest 存在且 package_type 匹配。
// Python: extension_package_manager.py:1227-1240 _validate_package_manifest。
func validatePackageManifest(candidate, kindLabel, packageType string) (string, error) {
	manifest, _ := readPackageManifest(candidate)
	if manifest == nil {
		return "", fmt.Errorf("%s package missing/corrupt manifest.json: %s",
			kindLabel, filepath.Base(candidate))
	}
	if declared, _ := manifest["package_type"].(string); declared != packageType {
		return "", fmt.Errorf("%s package wrong package_type: %s (expected %s, got %q)",
			kindLabel, filepath.Base(candidate), packageType, declared)
	}
	return candidate, nil
}

// resolvePackageDir 解析 local/built_in 两目录中的已安装包目录。
// Python: extension_package_manager.py:1285-1300 _resolve_package_dir。
// 步骤对齐：reject → local 探测 → built_in 探测 → 双处冲突 → 依次校验 → not found。
func resolvePackageDir(name, kindRoot, kind, kindLabel string) (string, error) {
	safeName, err := rejectPackageName(name, kindLabel)
	if err != nil {
		return "", err
	}
	localDir, err := packageDirIfPresent(filepath.Join(kindRoot, "local"), safeName)
	if err != nil {
		return "", err
	}
	builtInDir, err := packageDirIfPresent(filepath.Join(kindRoot, "built_in"), safeName)
	if err != nil {
		return "", err
	}
	if localDir != "" && builtInDir != "" {
		return "", fmt.Errorf("%s package conflict: %s exists in both local and built_in",
			kindLabel, safeName)
	}
	if localDir != "" {
		return validatePackageManifest(localDir, kindLabel, kind)
	}
	if builtInDir != "" {
		return validatePackageManifest(builtInDir, kindLabel, kind)
	}
	return "", fmt.Errorf("%s package not found: %s", kindLabel, safeName)
}

package resources

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	hschema "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/schema"
)

// ⏵ 15.14：本文件为最小加载器桩。
//
// 完整的包加载（资源解压、相对路径归一化、prompt_sections / memories /
// rubrics / skills 装配、插件能力覆盖）在 15.14 落地后替换。
// 此处只提供 15.5 的 factory 需要的两步：定位 manifest、读出 AgentTemplateSpec
// 并做上游同款的必填校验。
//
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

// LoadAgentTemplatePackage 把 package_type=agent_template 的 manifest.json
// 解析成 AgentTemplateSpec。
// Python: extension_loader.py:196-... load_agent_template_package
//
// 四步校验（顺序对齐上游）：
//  1. 文件名必须是 manifest.json
//  2. package_type 必须是 agent_template
//  3. name 必填非空
//  4. description 必填非空
func LoadAgentTemplatePackage(manifestPath string) (hschema.AgentTemplateSpec, error) {
	expanded, err := expandUserPath(manifestPath)
	if err != nil {
		return hschema.AgentTemplateSpec{}, err
	}
	absolute, err := filepath.Abs(expanded)
	if err != nil {
		return hschema.AgentTemplateSpec{}, err
	}
	// 步骤 1
	if filepath.Base(absolute) != packageManifestName {
		return hschema.AgentTemplateSpec{}, fmt.Errorf(
			"AgentTemplate manifest must be named %q: %s", packageManifestName, absolute)
	}
	content, err := os.ReadFile(absolute)
	if err != nil {
		return hschema.AgentTemplateSpec{}, err
	}
	var payload map[string]any
	if err := json.Unmarshal(content, &payload); err != nil {
		return hschema.AgentTemplateSpec{}, fmt.Errorf("parse %s: %w", absolute, err)
	}
	// 步骤 2
	packageType, _ := payload["package_type"].(string)
	if packageType != agentTemplatePackageType {
		return hschema.AgentTemplateSpec{}, fmt.Errorf(
			"%s declares package_type=%v, expected 'agent_template'", absolute, payload["package_type"])
	}
	// 步骤 3
	name, _ := payload["name"].(string)
	if strings.TrimSpace(name) == "" {
		return hschema.AgentTemplateSpec{}, fmt.Errorf(
			"AgentTemplate manifest %s is missing required non-empty 'name'", absolute)
	}
	// 步骤 4
	description, _ := payload["description"].(string)
	if strings.TrimSpace(description) == "" {
		return hschema.AgentTemplateSpec{}, fmt.Errorf(
			"AgentTemplate manifest %s is missing required non-empty 'description'", absolute)
	}

	// 解析成结构体（agent_card 可能由扁平 name / description 归一化而来）
	var spec hschema.AgentTemplateSpec
	if err := json.Unmarshal(content, &spec); err != nil {
		return hschema.AgentTemplateSpec{}, fmt.Errorf("decode %s: %w", absolute, err)
	}
	if spec.AgentCard.Name == "" {
		spec.AgentCard.Name = name
	}
	if spec.AgentCard.Description == "" {
		spec.AgentCard.Description = description
	}
	return spec, nil
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

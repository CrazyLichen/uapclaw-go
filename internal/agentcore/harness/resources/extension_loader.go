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

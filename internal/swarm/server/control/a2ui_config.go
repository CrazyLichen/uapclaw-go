package control

import (
	"fmt"
	"os"
	"strings"
)

// ──────────────────────────── 结构体 ────────────────────────────

// A2UIConfig 可选 A2UI 特性的开关（Python: a2ui_config.py:15 A2UIConfig）。
type A2UIConfig struct {
	// Enabled 是否启用 A2UI
	Enabled bool
	// ProtocolVersion A2UI 协议版本
	ProtocolVersion string
	// StreamValidationEnabled 流式校验开关
	StreamValidationEnabled bool
	// NonWebFallbackEnabled 非 Web 回退开关
	NonWebFallbackEnabled bool
	// DevSmokeToolsEnabled 开发冒烟工具开关
	DevSmokeToolsEnabled bool
}

// ──────────────────────────── 常量 ────────────────────────────

// SupportedA2UIProtocolVersions 支持的 A2UI 协议版本集合
// （Python: a2ui_config.py:12 SUPPORTED_A2UI_PROTOCOL_VERSIONS）。
var SupportedA2UIProtocolVersions = map[string]bool{"0.8": true}

// ──────────────────────────── 非导出函数 ────────────────────────────

// toBool 三态布尔解析（Python: a2ui_config.py:24 _to_bool）。
// None 回默认；bool 原样；字符串按 1/true/yes/on 与 0/false/no/off 判定；
// 其余回默认。
func toBool(value any, defaultVal bool) bool {
	if value == nil {
		return defaultVal
	}
	if b, ok := value.(bool); ok {
		return b
	}
	text := strings.ToLower(strings.TrimSpace(fmt.Sprintf("%v", value)))
	switch text {
	case "1", "true", "yes", "on":
		return true
	case "0", "false", "no", "off":
		return false
	}
	return defaultVal
}

// ──────────────────────────── 导出函数 ────────────────────────────

// GetA2UIConfig 从 config 字典加环境变量覆盖构建 A2UI 配置
// （Python: a2ui_config.py:36 get_a2ui_config）。
// 步骤：取 a2ui 段（非 dict 回空）→ enabled（段值 + 环境变量覆盖）→
// protocol_version（环境变量优先，校验支持集合，不支持则报错）→
// dev_smoke_tools_enabled（段值 + 环境变量覆盖）→ 组装。
func GetA2UIConfig(config map[string]any) (A2UIConfig, error) {
	raw := config
	if raw == nil {
		raw = map[string]any{}
	}
	sectionAny, ok := raw["a2ui"]
	section := map[string]any{}
	if ok {
		if s, isMap := sectionAny.(map[string]any); isMap {
			section = s
		}
	}

	enabled := toBool(section["enabled"], false)
	envEnabled := os.Getenv("JIUWENSWARM_A2UI_ENABLED")
	if envEnabled != "" {
		enabled = toBool(envEnabled, enabled)
	}

	protocolVersion := strings.TrimSpace(firstNonEmpty(
		os.Getenv("JIUWENSWARM_A2UI_PROTOCOL_VERSION"),
		stringOrEmpty(section["protocol_version"]),
		"0.8",
	))
	if !SupportedA2UIProtocolVersions[protocolVersion] {
		return A2UIConfig{}, fmt.Errorf("Unsupported A2UI protocol version: %s", protocolVersion)
	}

	devSmokeToolsEnabled := toBool(section["dev_smoke_tools_enabled"], false)
	envSmoke := os.Getenv("JIUWENSWARM_A2UI_SMOKE_TOOLS")
	if envSmoke != "" {
		devSmokeToolsEnabled = toBool(envSmoke, devSmokeToolsEnabled)
	}

	return A2UIConfig{
		Enabled:                 enabled,
		ProtocolVersion:         protocolVersion,
		StreamValidationEnabled: toBool(section["stream_validation_enabled"], true),
		NonWebFallbackEnabled:   toBool(section["non_web_fallback_enabled"], false),
		DevSmokeToolsEnabled:    devSmokeToolsEnabled,
	}, nil
}

// IsA2UIEnabled 便捷判断（Python: a2ui_config.py:70 is_a2ui_enabled）。
func IsA2UIEnabled(config map[string]any) bool {
	cfg, err := GetA2UIConfig(config)
	return err == nil && cfg.Enabled
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// firstNonEmpty 返回第一个非空字符串。
func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}

// stringOrEmpty 将任意值转字符串（None 转空串）。
func stringOrEmpty(value any) string {
	if value == nil {
		return ""
	}
	return fmt.Sprintf("%v", value)
}

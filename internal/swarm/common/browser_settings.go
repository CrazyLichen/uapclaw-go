// 前端与 Runtime 共享的轻量浏览器设置投影。
// 对应 Python 代码：jiuwenswarm/common/browser_settings.py（88 行）。
package common

import (
	"fmt"
	"net/url"
	"os"
	"strings"
)

// ──────────────────────────── 常量 ────────────────────────────

// browserDecisionModes 浏览器决策模式集合（Python: browser_settings.py:9 BROWSER_DECISION_MODES）。
var browserDecisionModes = map[string]bool{"llm": true, "shadow": true, "hybrid": true}

// jevProviderDefaults Jev 供应商默认连接参数（Python: browser_settings.py:10 JEV_PROVIDER_DEFAULTS）。
var jevProviderDefaults = map[string]map[string]string{
	"openrouter": {
		"model":       "typesafe/jev-1.13",
		"api_base":    "https://openrouter.ai/api/alpha",
		"api_key_env": "OPENROUTER_API_KEY",
	},
	"typesafe": {
		"model":       "jev-1.13.0",
		"api_base":    "https://api.typesafe.ai/v1",
		"api_key_env": "TYPESAFE_API_KEY",
	},
	"custom": {
		"model":       "jev-1.13.0",
		"api_base":    "",
		"api_key_env": "JEV_CUSTOM_API_KEY",
	},
}

// ──────────────────────────── 导出函数 ────────────────────────────

// ResolveJevConnection 读共享 Jev 连接；浏览器与协作模式相互独立
// （Python: browser_settings.py:22 resolve_jev_connection）。
// 步骤：browser/decision 段校验 → resolve_value 展开钩子 → provider 校验 →
// 默认值合并 → 三字段非空校验 → HTTPS URL 校验（禁凭据/查询/片段）→
// 密钥环境变量非空校验 → 组装返回。
func ResolveJevConnection(config map[string]any, resolveValue func(map[string]any) map[string]any) (map[string]string, error) {
	browser, ok := config["browser"].(map[string]any)
	if !ok {
		return nil, fmt.Errorf("browser must be a mapping")
	}
	sectionAny := browser["decision"]
	section, ok := sectionAny.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("browser.decision must be a mapping")
	}
	if resolveValue != nil {
		section = resolveValue(section)
	}
	provider := "typesafe"
	if p, ok := section["provider"].(string); ok {
		provider = p
	}
	defaults, validProvider := jevProviderDefaults[provider]
	if !validProvider {
		return nil, fmt.Errorf("Jev provider must be openrouter, typesafe or custom")
	}
	settings := map[string]string{}
	for k, v := range defaults {
		settings[k] = v
	}
	for _, name := range []string{"api_base", "model", "api_key_env"} {
		if v, ok := section[name].(string); ok {
			settings[name] = v
		}
	}
	for _, name := range []string{"api_base", "model", "api_key_env"} {
		value := strings.TrimSpace(settings[name])
		if value == "" {
			return nil, fmt.Errorf("Jev %s is required in Models > Jev settings", name)
		}
		settings[name] = value
	}
	apiBase := settings["api_base"]
	parsed, err := url.Parse(apiBase)
	validURL := err == nil && parsed.Scheme == "https" && parsed.Hostname() != ""
	validURL = validURL && parsed.User == nil && parsed.RawQuery == "" && parsed.Fragment == ""
	if !validURL {
		return nil, fmt.Errorf("Jev API base must be an HTTPS URL without credentials, query or fragment")
	}
	secret := strings.TrimSpace(os.Getenv(settings["api_key_env"]))
	if secret == "" {
		return nil, fmt.Errorf("Jev API key is required in Models > Jev settings")
	}
	endpointPath := "/systemone"
	if provider == "openrouter" {
		endpointPath = "/decisions"
	}
	return map[string]string{
		"api_base":      strings.TrimRight(apiBase, "/"),
		"model_name":    settings["model"],
		"api_key":       secret,
		"endpoint_path": endpointPath,
	}, nil
}

// ProjectBrowserDecision 投影已解析设置，不暴露凭据
// （Python: browser_settings.py:70 project_browser_decision）。
// provider 非法回 typesafe；mode 非法回 llm；键配置态看环境变量是否非空。
func ProjectBrowserDecision(section map[string]any, environment map[string]string) map[string]any {
	provider := "typesafe"
	if p, ok := section["provider"].(string); ok {
		provider = p
	}
	if _, valid := jevProviderDefaults[provider]; !valid {
		provider = "typesafe"
	}
	defaults := jevProviderDefaults[provider]
	mode := "llm"
	if m, ok := section["mode"].(string); ok && browserDecisionModes[m] {
		mode = m
	}
	keyEnv := defaults["api_key_env"]
	if ke, ok := section["api_key_env"].(string); ok && ke != "" {
		keyEnv = ke
	}
	decisionModel := defaults["model"]
	if m, ok := section["model"].(string); ok && m != "" {
		decisionModel = m
	}
	decisionAPIBase := defaults["api_base"]
	if b, ok := section["api_base"].(string); ok && b != "" {
		decisionAPIBase = b
	}
	displayName := ""
	if d, ok := section["display_name"].(string); ok {
		displayName = d
	}
	jevKeyConfigured := false
	if v, ok := environment[keyEnv]; ok && strings.TrimSpace(v) != "" {
		jevKeyConfigured = true
	}
	return map[string]any{
		"decision_mode":         mode,
		"decision_provider":     provider,
		"decision_model":        decisionModel,
		"decision_display_name": displayName,
		"decision_api_base":     decisionAPIBase,
		"jev_key_configured":    jevKeyConfigured,
	}
}

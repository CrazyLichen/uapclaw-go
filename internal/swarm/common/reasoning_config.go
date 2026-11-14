// 思考档位归一、档位映射与采样覆盖。
// 对应 Python 代码：gateway/common/reasoning_config.py:1-184（该文件无双副本，gateway 侧为唯一源）。
// 裁决 A：原挂 20.3 ⤴️，随 23.4d 并入本批（BuildReasoningModelRequestKwargs 的直接依赖）。
package common

import (
	"fmt"
	"net/url"
	"strings"

	"github.com/uapclaw/uapclaw-go/internal/common/config"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/reasoning"
)

// ──────────────────────────── 常量 ────────────────────────────

// SupportedReasoningLevels 全部合法档位。
// Python: reasoning_config.py:8 SUPPORTED_REASONING_LEVELS。
var SupportedReasoningLevels = []string{"off", "on", "minimal", "low", "medium", "high", "xhigh", "max"}

// SamplingOverrideRules Kimi 系模型拒绝任何其它采样值，强制覆盖。
// Python: reasoning_config.py:29 SAMPLING_OVERRIDE_RULES。
// 同时匹配官方端点与模型 ID——DashScope 等聚合方也暴露 Kimi 模型。
var SamplingOverrideRules = map[string]map[string]float64{
	"api.moonshot.cn": {"temperature": 1.0, "top_p": 0.95},
	"api.kimi.com":    {"temperature": 1.0, "top_p": 0.95},
}

// ──────────────────────────── 导出函数 ────────────────────────────

// ParseAPIBase 解析 api_base，缺 scheme 时补 https://。
// Python: reasoning_config.py:20 _parse_api_base。
func ParseAPIBase(apiBase string) *url.URL {
	value := strings.TrimSpace(apiBase)
	if value != "" && !strings.Contains(value, "://") {
		value = "https://" + value
	}
	parsed, err := url.Parse(value)
	if err != nil {
		// Python urlparse 对非法输入宽松返回空结果，Go 侧对齐为空 URL
		return &url.URL{}
	}
	return parsed
}

// ResolveEndpointProfileOverride 返回用户配置网关 host 隐含的 endpoint_profile。
// Python: reasoning_config.py:43 resolve_endpoint_profile_override。
// 覆盖表来源是 config.yaml 顶层 endpoint_profile_overrides(host -> profile)，
// 源码不内置任何 host；显式配置的 endpoint_profile 始终优先，本表只补空缺。
func ResolveEndpointProfileOverride(apiBase string) string {
	host := strings.ToLower(ParseAPIBase(apiBase).Hostname())
	if host == "" {
		return ""
	}
	// Python 局部 import：轻量上下文不加载用户工作区配置也能导入本模块。
	// Go 侧对齐为失败即返回空串（Python: reasoning_config.py:50-55 except → None）。
	overrides := config.GetEndpointProfileOverrides()
	return overrides[host]
}

// EffectiveEndpointProfile 显式配置优先，否则按已知 api_base host 推断。
// Python: reasoning_config.py:59 effective_endpoint_profile。
func EffectiveEndpointProfile(apiBase string, endpointProfile string) string {
	explicit := strings.TrimSpace(endpointProfile)
	if explicit != "" {
		return explicit
	}
	return ResolveEndpointProfileOverride(apiBase)
}

// ResolveSamplingOverride 返回 Kimi 端点/模型要求的采样参数。
// Python: reasoning_config.py:67 resolve_sampling_override。
// 返回 nil 表示无需覆盖（Python None）。
func ResolveSamplingOverride(apiBase string, modelName string) map[string]float64 {
	host := strings.ToLower(ParseAPIBase(apiBase).Hostname())
	// Python: reasoning_config.py:73 去空白、小写、取最后一个 / 后的段
	normalizedModel := strings.ToLower(strings.TrimSpace(modelName))
	if idx := strings.LastIndex(normalizedModel, "/"); idx >= 0 {
		normalizedModel = normalizedModel[idx+1:]
	}
	if _, ok := SamplingOverrideRules[host]; ok || strings.HasPrefix(normalizedModel, "kimi-") {
		return map[string]float64{"temperature": 1.0, "top_p": 0.95}
	}
	return nil
}

// NormalizeReasoningLevel 归一化思考档位原始值。
// Python: reasoning_config.py:79 normalize_reasoning_level。
// 返回空串表示 None（未设置或无法识别）。
func NormalizeReasoningLevel(raw string) string {
	// Python: reasoning_config.py:80-85 None 与空串 → None
	key := strings.ToLower(strings.TrimSpace(raw))
	if key == "" {
		return ""
	}
	// Python: reasoning_config.py:86-101 分支顺序照抄
	if key == "off" || key == "none" || key == "false" || key == "disable" || key == "disabled" {
		return "off"
	}
	if key == "on" || key == "true" || key == "enable" || key == "enabled" {
		return "on"
	}
	if key == "minimum" || key == "minimal" {
		return "minimal"
	}
	if key == "low" {
		return "low"
	}
	if key == "medium" || key == "med" || key == "mid" {
		return "medium"
	}
	if key == "high" {
		return "high"
	}
	if key == "xhigh" || key == "extra_high" {
		return "xhigh"
	}
	if key == "max" || key == "maximum" || key == "ultra" {
		return "max"
	}
	return ""
}

// ReasoningConfigForLevel 把 UI 兼容档位转成 core 的厂商中立配置。
// Python: reasoning_config.py:105 reasoning_config_for_level。
func ReasoningConfigForLevel(level string) map[string]string {
	if level == "off" {
		return map[string]string{"mode": "disabled"}
	}
	if level == "on" {
		return map[string]string{"mode": "enabled"}
	}
	return map[string]string{"mode": "enabled", "effort": level}
}

// ReasoningLevelOptions 返回 core ReasoningCapability 的合法持久化值。
// Python: reasoning_config.py:114 reasoning_level_options（getattr(capability, "options", ())）。
func ReasoningLevelOptions(capability reasoning.ReasoningCapability) []string {
	options := capability.Options
	if options == nil {
		return []string{}
	}
	return options
}

// ValidateReasoningLevelForModel 归一化 reasoning_level 并按模型能力校验。
// Python: reasoning_config.py:119 validate_reasoning_level_for_model。
// 返回规范档位；原始值为空返回 ""（= 用厂商默认）；未知或不支持值返回 error（Python ValueError）。
func ValidateReasoningLevelForModel(rawLevel string, modelName string, modelProvider string, apiBase string, endpointProfile string) (string, error) {
	// Python: reasoning_config.py:134-139 仅 None/空白视为"未设置"；不能按 falsy 短路
	// （YAML 1.1 会把裸 off 读成 False），空串入参即未设置语义。
	raw := strings.TrimSpace(rawLevel)
	if raw == "" {
		return "", nil
	}
	level := NormalizeReasoningLevel(raw)
	if level == "" {
		return "", fmt.Errorf("reasoning_level is invalid; canonical values: %s", strings.Join(SupportedReasoningLevels, ", "))
	}
	// Python: reasoning_config.py:145-146 provider 取 value 后判 anthropic
	providerName := strings.TrimSpace(modelProvider)
	protocol := "openai"
	if strings.ToLower(providerName) == "anthropic" {
		protocol = "anthropic"
	}
	// Python: reasoning_config.py:147-165 能力查询是静态表 lookup；一旦异常转成
	// 可读的校验错误（BAD_REQUEST）而不是 500。Go 侧 GetReasoningCapability 不返回
	// error（签名见 profiles.go:338），故无对应 except 分支——注释记录该差异。
	capability := reasoning.GetReasoningCapability(providerName, modelName, protocol, apiBase, EffectiveEndpointProfile(apiBase, endpointProfile))
	allowed := ReasoningLevelOptions(capability)
	for _, option := range allowed {
		if option == level {
			return level, nil
		}
	}
	allowedDisplay := strings.Join(allowed, ", ")
	if allowedDisplay == "" {
		allowedDisplay = "default only"
	}
	return "", fmt.Errorf("reasoning_level must be one of: %s", allowedDisplay)
}

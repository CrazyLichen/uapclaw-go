package schema

import (
	"net/url"
	"strings"

	"github.com/uapclaw/uapclaw-go/internal/common/config"
)

// 本文件是 gateway/common/reasoning_config.py 的 Go 对应物。
//
// Python: gateway/common/reasoning_config.py（184 行）
//
// 职责：思考强度的「配置期归一」——把 UI 的档位字段翻译成 core 的
// provider 中立配置，以及端点相关的采样强制与 endpoint_profile 推断。
// 与 agentcore/foundation/llm/reasoning/ 的分工：
//   - reasoning/（core 层）        —— 请求期按 provider 方言生成线上参数
//   - 本文件（gateway/common 层）  —— 配置期档位归一与采样覆盖

// ──────────────────────────── 枚举 ────────────────────────────

// ReasoningLevel 思考强度档位。
// Python: gateway/common/reasoning_config.py:7 ReasoningLevel（Literal 8 值）
type ReasoningLevel string

const (
	// ReasoningLevelOff 关闭思考
	ReasoningLevelOff ReasoningLevel = "off"
	// ReasoningLevelOn 开启思考（不指定档位）
	ReasoningLevelOn ReasoningLevel = "on"
	// ReasoningLevelMinimal 最低档
	ReasoningLevelMinimal ReasoningLevel = "minimal"
	// ReasoningLevelLow 低档
	ReasoningLevelLow ReasoningLevel = "low"
	// ReasoningLevelMedium 中档
	ReasoningLevelMedium ReasoningLevel = "medium"
	// ReasoningLevelHigh 高档
	ReasoningLevelHigh ReasoningLevel = "high"
	// ReasoningLevelXHigh 极高档
	ReasoningLevelXHigh ReasoningLevel = "xhigh"
	// ReasoningLevelMax 最高档
	ReasoningLevelMax ReasoningLevel = "max"
)

// ──────────────────────────── 常量 ────────────────────────────

// kimiModelPrefix Kimi 模型名前缀（去除 provider 前缀后匹配）。
// Python: reasoning_config.py:73（normalized_model.startswith("kimi-")）
const kimiModelPrefix = "kimi-"

// ──────────────────────────── 全局变量 ────────────────────────────

// SupportedReasoningLevels 全部合法档位（顺序对齐上游）。
// Python: reasoning_config.py:8-17 SUPPORTED_REASONING_LEVELS
var SupportedReasoningLevels = []ReasoningLevel{
	ReasoningLevelOff, ReasoningLevelOn, ReasoningLevelMinimal,
	ReasoningLevelLow, ReasoningLevelMedium, ReasoningLevelHigh,
	ReasoningLevelXHigh, ReasoningLevelMax,
}

// reasoningLevelAliases 档位别名 → 规范值。
// Python: reasoning_config.py:92-118 normalize_reasoning_level
var reasoningLevelAliases = map[string]ReasoningLevel{
	"off": ReasoningLevelOff, "none": ReasoningLevelOff,
	"false": ReasoningLevelOff, "disable": ReasoningLevelOff,
	"disabled": ReasoningLevelOff,
	"on":       ReasoningLevelOn, "true": ReasoningLevelOn,
	"enable": ReasoningLevelOn, "enabled": ReasoningLevelOn,
	"minimal": ReasoningLevelMinimal, "minimum": ReasoningLevelMinimal,
	"low":    ReasoningLevelLow,
	"medium": ReasoningLevelMedium, "med": ReasoningLevelMedium, "mid": ReasoningLevelMedium,
	"high":  ReasoningLevelHigh,
	"xhigh": ReasoningLevelXHigh, "extra_high": ReasoningLevelXHigh,
	"max": ReasoningLevelMax, "maximum": ReasoningLevelMax, "ultra": ReasoningLevelMax,
}

// samplingOverrideRules host → 强制采样参数。
// Python: reasoning_config.py:28-32 SAMPLING_OVERRIDE_RULES
//
// Kimi 模型拒绝其它采样值。同时匹配官方域名与模型 ID，因为 DashScope 等
// 聚合平台也会暴露 Kimi 模型。
var samplingOverrideRules = map[string]map[string]float64{
	"api.moonshot.cn": {"temperature": 1.0, "top_p": 0.95},
	"api.kimi.com":    {"temperature": 1.0, "top_p": 0.95},
}

// ──────────────────────────── 导出函数 ────────────────────────────

// NormalizeReasoningLevel 把任意来源的 reasoning_level 归一到合法档位。
// Python: reasoning_config.py:92-118 normalize_reasoning_level
//
// 无法识别的取值（含 nil、非字符串、空串）一律返回空字符串，表示「未指定」。
func NormalizeReasoningLevel(raw any) ReasoningLevel {
	s, ok := raw.(string)
	if !ok {
		return ""
	}
	return reasoningLevelAliases[strings.ToLower(strings.TrimSpace(s))]
}

// ReasoningConfigForLevel 把 UI 兼容档位转成 core 的 provider 中立配置。
// Python: reasoning_config.py:120-126 reasoning_config_for_level
//
// off → {"mode":"disabled"}；on → {"mode":"enabled"}；
// 其余档位 → {"mode":"enabled","effort":<level>}。
func ReasoningConfigForLevel(level ReasoningLevel) map[string]string {
	if level == ReasoningLevelOff {
		return map[string]string{"mode": "disabled"}
	}
	if level == ReasoningLevelOn {
		return map[string]string{"mode": "enabled"}
	}
	return map[string]string{"mode": "enabled", "effort": string(level)}
}

// ResolveSamplingOverride Kimi 端点/模型必须的采样参数。
// Python: reasoning_config.py:67-76 resolve_sampling_override
//
// host ∈ SAMPLING_OVERRIDE_RULES，或模型名（去 provider 前缀后）以 kimi- 开头，
// 返回 {"temperature":1.0,"top_p":0.95}；否则返回 nil。
func ResolveSamplingOverride(apiBase, modelName string) map[string]float64 {
	if rule, ok := samplingOverrideRules[apiBaseHost(apiBase)]; ok {
		return cloneSampling(rule)
	}
	normalizedModel := strings.ToLower(strings.TrimSpace(modelName))
	if index := strings.LastIndex(normalizedModel, "/"); index >= 0 {
		normalizedModel = normalizedModel[index+1:]
	}
	if strings.HasPrefix(normalizedModel, kimiModelPrefix) {
		return map[string]float64{"temperature": 1.0, "top_p": 0.95}
	}
	return nil
}

// ResolveEndpointProfileOverride 由用户配置的网关 host 推断 endpoint_profile。
// Python: reasoning_config.py:43-56 resolve_endpoint_profile_override
//
// 数据源是 config.yaml 顶层 endpoint_profile_overrides（host → profile），
// 源码不内置任何 host。未配置时返回空串。
func ResolveEndpointProfileOverride(apiBase string) string {
	host := apiBaseHost(apiBase)
	if host == "" {
		return ""
	}
	return config.GetEndpointProfileOverrides()[host]
}

// EffectiveEndpointProfile 显式 endpoint_profile 优先，否则从 api_base 推断。
// Python: reasoning_config.py:59-64 effective_endpoint_profile
func EffectiveEndpointProfile(apiBase string, explicit any) string {
	if s, ok := explicit.(string); ok {
		if trimmed := strings.TrimSpace(s); trimmed != "" {
			return trimmed
		}
	}
	return ResolveEndpointProfileOverride(apiBase)
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// apiBaseHost 提取 api_base 的主机名（小写）；缺 scheme 时补 https。
// Python: reasoning_config.py:20-24 _parse_api_base
func apiBaseHost(apiBase string) string {
	value := strings.TrimSpace(apiBase)
	if value == "" {
		return ""
	}
	if !strings.Contains(value, "://") {
		value = "https://" + value
	}
	parsed, err := url.Parse(value)
	if err != nil {
		return ""
	}
	return strings.ToLower(parsed.Hostname())
}

// cloneSampling 复制采样规则，避免调用方改写共享表。
func cloneSampling(rule map[string]float64) map[string]float64 {
	out := make(map[string]float64, len(rule))
	for k, v := range rule {
		out[k] = v
	}
	return out
}

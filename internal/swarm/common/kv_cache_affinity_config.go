// Python 源：jiuwenswarm/common/kv_cache_affinity_config.py（314 行 1:1）
//
// KV cache 亲和的规范配置与 provider 判定规则。build_kv_cache_affinity_config
// 是 Agent/Team 共用的 KVC 策略构建入口（mode 正式声明 + 旧 AscendAffinity
// 兼容）；get_kv_cache_control_model_* 系为装配层（config_specs）取权威控制
// 模型配置；validate/normalize 两函数被 web 写链消费（随批落全文件，一文件
// 对一文件）。

package common

import (
	"os"
	"strconv"
	"strings"

	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/kv_cache"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 常量 ────────────────────────────

const (
	// kvCacheLogComponent KV 亲和配置组件日志。
	kvCacheLogComponent = logger.ComponentCommon

	// AscendAffinityProvider 旧 provider 名（亲和能力的 legacy 配置别名）。
	// Python: ASCEND_AFFINITY_PROVIDER (kv_cache_affinity_config.py:13)
	AscendAffinityProvider = "AscendAffinity"
	// ApplicationKvCacheConfigKey 应用级 KVC 策略键。Python: :14
	ApplicationKvCacheConfigKey = "kv_cache_affinity_config"
	// KvCacheAffinityEnabledKey 启用键。Python: KV_CACHE_AFFINITY_ENABLED_KEY :15
	KvCacheAffinityEnabledKey = "enable_kv_cache_affinity"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// IsKvCacheAffinityConfig 新式判别：extensions.kv_cache.mode == "affinity"。
// Python: is_kv_cache_affinity_config (kv_cache_affinity_config.py:44-46)
func IsKvCacheAffinityConfig(configLike any) bool {
	return kvCacheMode(configLike) == "affinity"
}

// NormalizeProvider 返回稳定 provider 名（enum.value / 字符串 / 缺省）。
// Python: normalize_provider (kv_cache_affinity_config.py:49-53)
func NormalizeProvider(provider any) string {
	// Python: getattr(provider, "value", provider)——Go 侧枚举以字符串承载，
	// 直接取值；非字符串类型经 config.String 归一。
	if s, ok := provider.(string); ok {
		return strings.TrimSpace(s)
	}
	return strings.TrimSpace(stringify(provider))
}

// HasKvCacheAffinityCapability 单端点是否声明亲和能力：
// extensions.kv_cache.mode=affinity 为正式声明；AscendAffinity 保留为
// 展示与旧配置别名。
// Python: has_kv_cache_affinity_capability (kv_cache_affinity_config.py:78-95)
func HasKvCacheAffinityCapability(modelClientConfig any, provider any) bool {
	effectiveProvider := NormalizeProvider(provider)
	if effectiveProvider == "" {
		effectiveProvider = modelClientProvider(modelClientConfig)
	}
	return IsKvCacheAffinityConfig(modelClientConfig) || effectiveProvider == AscendAffinityProvider
}

// ModelProvider 从 OpenJiuwen Model 或其 client 解析生效 provider。
// Python: model_provider (kv_cache_affinity_config.py:98-105)
// Go 侧 Model 无 _client 属性链；仅消费 model 本身的 model_client_config。
// （偏差 D-15.16-e9：Python 的 getattr(model, "_client") 回退分支——Go Model
// 结构无 _client 概念，分支不可达，不落。）
func ModelProvider(model any) string {
	if model == nil {
		return ""
	}
	if owner, ok := model.(interface{ GetModelClientConfig() any }); ok {
		if provider := modelClientProvider(owner.GetModelClientConfig()); provider != "" {
			return provider
		}
	}
	if owner, ok := model.(interface{ ModelClientConfig() any }); ok {
		if provider := modelClientProvider(owner.ModelClientConfig()); provider != "" {
			return provider
		}
	}
	return ""
}

// BuildKvCacheAffinityConfig 构建共享的 Agent/Team KVC 策略，按 mode fail-closed。
// 新声明下 KV 亲和由 extensions.kv_cache.mode=affinity 表达；本函数以 mode 为
// 正式能力声明，同时兼容旧 provider 名 AscendAffinity。
// Python: build_kv_cache_affinity_config (kv_cache_affinity_config.py:108-137)
func BuildKvCacheAffinityConfig(applicationConfig map[string]any, provider string, modelClientConfig any) kv_cache.KVCacheAffinityConfig {
	affinityEnabled := IsAffinityEnabled(applicationConfig)

	normalizedProvider := NormalizeProvider(provider)
	if affinityEnabled && !HasKvCacheAffinityCapability(modelClientConfig, normalizedProvider) {
		logger.Warn(kvCacheLogComponent).
			Str("model_provider", orEmptyStr(normalizedProvider)).
			Str("mode", orEmptyStr(kvCacheMode(modelClientConfig))).
			Msg("KV cache affinity failed closed: model provider/mode requires extensions.kv_cache.mode=affinity (or legacy AscendAffinity)")
		affinityEnabled = false
	}
	return kv_cache.KVCacheAffinityConfig{EnableKVCacheAffinity: affinityEnabled}
}

// ParseBool 宽松布尔解析。Python: parse_bool (kv_cache_affinity_config.py:140-141)
func ParseBool(value any) bool {
	switch v := value.(type) {
	case string:
		s := strings.ToLower(strings.TrimSpace(v))
		return s == "true" || s == "1" || s == "yes"
	case bool:
		return v
	}
	return false
}

// SelectDefaultModelEntry 取默认模型条目（is_default 为真的第一条；否则首个
// dict 条目）。Python: select_default_model_entry (kv_cache_affinity_config.py:144-150)
func SelectDefaultModelEntry(models []any) map[string]any {
	for _, entry := range models {
		if m, ok := entry.(map[string]any); ok && m["is_default"] == true {
			return m
		}
	}
	for _, entry := range models {
		if m, ok := entry.(map[string]any); ok {
			return m
		}
	}
	return nil
}

// DefaultModelClientConfigFromEntries 默认条目的 model_client_config（非 dict 得 nil）。
// Python: default_model_client_config_from_entries (kv_cache_affinity_config.py:153-160)
func DefaultModelClientConfigFromEntries(models []any) map[string]any {
	entry := SelectDefaultModelEntry(models)
	if entry == nil {
		return nil
	}
	if mcc, ok := entry["model_client_config"].(map[string]any); ok {
		return mcc
	}
	return nil
}

// SetDefaultModelProviderInEntries 写链：把 provider 写进默认条目
// model_client_config.client_provider（已同值返回 false）。
// Python: set_default_model_provider_in_entries (kv_cache_affinity_config.py:163-177)
func SetDefaultModelProviderInEntries(models []any, provider string) bool {
	entry := SelectDefaultModelEntry(models)
	if entry == nil {
		return false
	}
	mcc, ok := entry["model_client_config"].(map[string]any)
	if !ok {
		mcc = map[string]any{}
		entry["model_client_config"] = mcc
	}
	if NormalizeProvider(mcc["client_provider"]) == provider {
		return false
	}
	mcc["client_provider"] = provider
	return true
}

// GetDefaultModelClientConfig 返回生效的默认 ModelClientConfig（不创建 Model）。
// 优先级：models.defaults 列表 → models.default.model_client_config →
// react.model_client_config → env MODEL_PROVIDER 兜底。
// Python: get_default_model_client_config (kv_cache_affinity_config.py:180-203)
func GetDefaultModelClientConfig(config map[string]any) map[string]any {
	if config == nil {
		config = map[string]any{}
	}
	models, _ := config["models"].(map[string]any)
	if entries, ok := models["defaults"].([]any); ok && len(entries) > 0 {
		if mcc := DefaultModelClientConfigFromEntries(entries); mcc != nil {
			return mcc
		}
		return map[string]any{}
	}
	if target, ok := models["default"].(map[string]any); ok {
		if mcc, ok := target["model_client_config"].(map[string]any); ok {
			return mcc
		}
	}
	react, _ := config["react"].(map[string]any)
	if mcc, ok := react["model_client_config"].(map[string]any); ok {
		return mcc
	}
	return map[string]any{"client_provider": strings.TrimSpace(os.Getenv("MODEL_PROVIDER"))}
}

// GetDefaultModelProvider 返回生效默认 provider（不构造 Model）。
// Python: get_default_model_provider (kv_cache_affinity_config.py:206-209)
func GetDefaultModelProvider(config map[string]any) string {
	return modelClientProvider(GetDefaultModelClientConfig(config))
}

// GetKvCacheControlModelClientConfigs 返回 KVC 控制策略的权威模型配置集：
// models.agentos 列表整体优先；无有效 AgentOS 条目才用通用默认。
// Python: get_kv_cache_control_model_client_configs (kv_cache_affinity_config.py:232-243)
func GetKvCacheControlModelClientConfigs(config map[string]any) []map[string]any {
	agentosConfigs := getAgentosModelClientConfigs(config)
	if len(agentosConfigs) > 0 {
		return agentosConfigs
	}
	return []map[string]any{GetDefaultModelClientConfig(config)}
}

// GetKvCacheControlModelClientConfig 返回首个权威 KVC 控制模型配置。
// Python: get_kv_cache_control_model_client_config (kv_cache_affinity_config.py:245-250)
func GetKvCacheControlModelClientConfig(config map[string]any) map[string]any {
	return GetKvCacheControlModelClientConfigs(config)[0]
}

// GetKvCacheControlModelProvider 返回构建应用级 KVC 策略的 provider。
// Python: get_kv_cache_control_model_provider (kv_cache_affinity_config.py:253-256)
func GetKvCacheControlModelProvider(config map[string]any) string {
	return modelClientProvider(GetKvCacheControlModelClientConfig(config))
}

// IsAffinityEnabled 应用级 KVC 开关。
// Python: is_affinity_enabled (kv_cache_affinity_config.py:259-261)
func IsAffinityEnabled(config map[string]any) bool {
	kvConfig := GetKvCacheAffinityApplicationConfig(config)
	enabled, _ := kvConfig[KvCacheAffinityEnabledKey].(bool)
	return enabled
}

// GetKvCacheAffinityApplicationConfig 应用级 KVC 策略（旧 react 路径回落）。
// Python: get_kv_cache_affinity_application_config (kv_cache_affinity_config.py:264-276)
func GetKvCacheAffinityApplicationConfig(config map[string]any) map[string]any {
	if config == nil {
		config = map[string]any{}
	}
	if canonical, ok := config[ApplicationKvCacheConfigKey].(map[string]any); ok {
		if _, present := canonical[KvCacheAffinityEnabledKey]; present {
			return canonical
		}
	}
	react, _ := config["react"].(map[string]any)
	if legacy, ok := react[ApplicationKvCacheConfigKey].(map[string]any); ok {
		return legacy
	}
	return map[string]any{}
}

// ValidateAffinityInvariant 校验亲和开关与 provider 声明的一致性。
// Python: validate_affinity_invariant (kv_cache_affinity_config.py:279-301)
func ValidateAffinityInvariant(config map[string]any) (bool, []string) {
	if config == nil {
		config = map[string]any{}
	}
	if !IsAffinityEnabled(config) {
		return true, nil
	}
	failures := []string{}
	agentosConfigs := getAgentosModelClientConfigs(config)
	modelClientConfigs := agentosConfigs
	if len(modelClientConfigs) == 0 {
		modelClientConfigs = []map[string]any{GetDefaultModelClientConfig(config)}
	}
	label := "default model"
	if len(agentosConfigs) > 0 {
		label = "AgentOS model"
	}
	for _, mcc := range modelClientConfigs {
		if HasKvCacheAffinityCapability(mcc, nil) {
			continue
		}
		failures = append(failures,
			label+" must declare extensions.kv_cache.mode=affinity "+
				"or use legacy provider "+AscendAffinityProvider+"; "+
				"got provider="+orEmptyStr(modelClientProvider(mcc))+" "+
				"mode="+orEmptyStr(kvCacheMode(mcc)))
	}
	return len(failures) == 0, failures
}

// NormalizeAffinityRequest 对单个可变请求 payload 强制开关/provider 一致性。
// Python: normalize_affinity_request (kv_cache_affinity_config.py:304-314)
func NormalizeAffinityRequest(params map[string]any) {
	affinityEnabled := ParseBool(params["kv_cache_affinity_enabled"])
	requestedProvider := strings.TrimSpace(stringify(params["model_provider"]))
	if affinityEnabled {
		if requestedProvider != "" && requestedProvider != AscendAffinityProvider {
			params["kv_cache_affinity_enabled"] = "false"
		} else {
			params["model_provider"] = AscendAffinityProvider
		}
	} else if requestedProvider != "" && requestedProvider != AscendAffinityProvider {
		if _, ok := params["kv_cache_affinity_enabled"]; !ok {
			params["kv_cache_affinity_enabled"] = "false"
		}
	}
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// kvCacheMode 读 model_client_config 的 extensions.kv_cache.mode（归一小写）。
// dict 取 extensions 子键；对象取属性链。亲和正式声明是 extensions.kv_cache.mode
// （affinity/none）；旧 client_provider=AscendAffinity 由统一能力判定兼容。
// Python: _kv_cache_mode (kv_cache_affinity_config.py:25-41)
func kvCacheMode(configLike any) string {
	if configLike == nil {
		return ""
	}
	if m, ok := configLike.(map[string]any); ok {
		extensions, _ := m["extensions"].(map[string]any)
		if extensions == nil {
			return ""
		}
		kv, _ := extensions["kv_cache"].(map[string]any)
		if kv == nil {
			return ""
		}
		return strings.ToLower(strings.TrimSpace(stringify(kv["mode"])))
	}
	// 对象形态：属性链访问（Python getattr）。Go 侧 model client config 以
	// map 承载为主；对象分支经接口探测兜底。
	if owner, ok := configLike.(interface{ Extensions() any }); ok {
		if ext, ok := owner.Extensions().(map[string]any); ok {
			if kv, ok := ext["kv_cache"].(map[string]any); ok {
				return strings.ToLower(strings.TrimSpace(stringify(kv["mode"])))
			}
		}
	}
	return ""
}

// modelClientProvider 传输归一前解析服务身份：agent-core 可把 DeepSeek/
// OpenRouter 等旧 provider 归一到 OpenAI 兼容传输，同时把原值留在
// legacy_client_provider——策略与诊断需要的是原始 provider。
// Python: _model_client_provider (kv_cache_affinity_config.py:56-75)
func modelClientProvider(config any) string {
	var legacyProvider, provider any
	if m, ok := config.(map[string]any); ok {
		legacyProvider = m["legacy_client_provider"]
		provider = m["client_provider"]
	} else if config != nil {
		if owner, ok := config.(interface{ GetLegacyClientProvider() any }); ok {
			legacyProvider = owner.GetLegacyClientProvider()
		}
		if owner, ok := config.(interface{ GetClientProvider() any }); ok {
			provider = owner.GetClientProvider()
		}
	}
	if legacyProvider != nil {
		if normalized := NormalizeProvider(legacyProvider); normalized != "" {
			return normalized
		}
	}
	return NormalizeProvider(provider)
}

// getAgentosModelClientConfigs 收集 models.agentos 列表里带 model_name 的
// model_client_config。Python: _get_agentos_model_client_configs (:212-229)
func getAgentosModelClientConfigs(config map[string]any) []map[string]any {
	if config == nil {
		config = map[string]any{}
	}
	models, _ := config["models"].(map[string]any)
	agentosEntries, _ := models["agentos"].([]any)
	configs := []map[string]any{}
	for _, entry := range agentosEntries {
		m, ok := entry.(map[string]any)
		if !ok {
			continue
		}
		mcc, ok := m["model_client_config"].(map[string]any)
		if ok && stringify(mcc["model_name"]) != "" {
			configs = append(configs, mcc)
		}
	}
	return configs
}

// stringify 任意值的安全字符串化（Python str() 的保守对应：非字符串得空串，
// 数字按 JSON 语义转换）。Python 侧 str() 对 None 得 "None"，但调用点均
// 先判 None，行为等价。
func stringify(v any) string {
	switch t := v.(type) {
	case nil:
		return ""
	case string:
		return t
	case bool:
		if t {
			return "true"
		}
		return "false"
	case float64:
		return strconv.FormatFloat(t, 'g', -1, 64)
	case int:
		return strconv.Itoa(t)
	case int64:
		return strconv.FormatInt(t, 10)
	}
	return ""
}

// orEmptyStr 空串显示占位（Python f-string 的 or "<empty>" 语义）。
func orEmptyStr(s string) string {
	if s == "" {
		return "<empty>"
	}
	return s
}

package schema

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/uapclaw/uapclaw-go/internal/common/exception"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ProviderValidator 服务商验证器接口。
// 已注册返回规范化名称，未注册返回空字符串。
type ProviderValidator interface {
	// ValidateProvider 验证 provider 是否已注册。
	// 已注册返回规范化名称，未注册返回空字符串。
	ValidateProvider(provider string) string
}

// ModelClientConfig 模型客户端配置。
type ModelClientConfig struct {
	// ClientID 客户端唯一标识，用于 Runner 注册，默认自动生成 UUID
	ClientID string `json:"client_id"`
	// ClientProvider 服务商标识，支持 ProviderType 枚举值或自定义字符串
	ClientProvider string `json:"client_provider"`
	// APIKey API 密钥
	APIKey string `json:"api_key"`
	// APIBase API 基础 URL
	APIBase string `json:"api_base"`
	// Timeout 请求超时时间（秒），必须 > 0，默认 360
	Timeout float64 `json:"timeout"`
	// AuthMode 鉴权模式，默认 api_key；none/custom_headers 时不强制 api_key
	// Python: schema/config.py:186 auth_mode: LLMAuthMode = ApiKey
	AuthMode AuthMode `json:"auth_mode,omitempty"`
	// APIMode 接口协议模式，默认（空值）Chat Completions。
	// Python: schema/config.py api_mode: LLMApiMode | None = None
	APIMode APIMode `json:"api_mode,omitempty"`
	// EndpointProfile 端点配置档名（ollama/lmstudio 等），缺 base 时回填默认地址
	// Python: schema/config.py endpoint_profile
	EndpointProfile string `json:"endpoint_profile,omitempty"`
	// StreamFirstChunkTimeout 首个携带模型输出的流式分片的最长等待（秒）。
	// nil 表示禁用；默认 300。只有携带输出（content/reasoning/tool_call）的分片
	// 才算首帧，role-only/heartbeat/usage-only 帧不算。
	// Python: schema/config.py stream_first_chunk_timeout: Optional[float] = 300.0
	StreamFirstChunkTimeout *float64 `json:"stream_first_chunk_timeout,omitempty"`
	// StreamIdleTimeout 相邻两个携带输出分片之间的最长等待（秒）。nil 表示禁用；默认 60。
	// Python: schema/config.py stream_idle_timeout: Optional[float] = 60.0
	StreamIdleTimeout *float64 `json:"stream_idle_timeout,omitempty"`
	// MaxRetries 最大重试次数，默认 3
	MaxRetries int `json:"max_retries"`
	// VerifySSL 是否验证 SSL 证书，默认 true
	VerifySSL bool `json:"verify_ssl"`
	// SSLCert SSL 证书文件路径（可选）
	SSLCert string `json:"ssl_cert,omitempty"`
	// CustomHeaders 开发者自定义请求头，每次 LLM 调用时合并
	CustomHeaders map[string]string `json:"custom_headers,omitempty"`
	// Extensions 模型客户端扩展配置（KV cache / prompt cache 等）。
	// Python: schema/config.py ModelClientConfig.extensions: Optional[LLMExtensionsConfig]
	Extensions *LLMExtensionsConfig `json:"extensions,omitempty"`
	// Extra 额外字段（对应 Python model_config extra="allow"），不直接参与 JSON 序列化
	Extra map[string]any `json:"-"`
}

// KVCacheExtensionConfig KV cache 扩展配置。
// Python: schema/config.py:47-49 KVCacheExtensionConfig（mode 是 Literal["none","affinity"]）
type KVCacheExtensionConfig struct {
	// Mode KV cache 模式，none/affinity
	Mode string `json:"mode,omitempty"`
	// AffinityField 亲和字段名，默认 agent_hint
	AffinityField string `json:"affinity_field,omitempty"`
}

// LLMExtensionsConfig 模型客户端扩展配置。
// Python: schema/config.py:52-56 LLMExtensionsConfig
type LLMExtensionsConfig struct {
	// PromptCache 提示词缓存配置
	PromptCache map[string]any `json:"prompt_cache,omitempty"`
	// KVCache KV cache 配置
	KVCache KVCacheExtensionConfig `json:"kv_cache,omitempty"`
	// ResponseFields 响应字段配置
	ResponseFields map[string]any `json:"response_fields,omitempty"`
	// RequestExtraBody 请求额外体
	RequestExtraBody map[string]any `json:"request_extra_body,omitempty"`
}

// ModelRequestConfig 模型请求配置。
type ModelRequestConfig struct {
	// ModelName 模型名称，如 "gpt-4"，JSON 键名为 "model"
	ModelName string `json:"model"`
	// Temperature 温度参数，控制输出随机性；nil = 未设置（不下发给 provider）
	// Python: schema/config.py:262-264 temperature: Optional[float] = None
	Temperature *float64 `json:"temperature,omitempty"`
	// TopP Top-p 采样参数，默认 0.1，对齐 Python: top_p=0.1
	TopP *float64 `json:"top_p,omitempty"`
	// MaxTokens 最大生成 token 数（可选）
	MaxTokens *int `json:"max_tokens,omitempty"`
	// Stop 停止序列（可选）
	Stop *string `json:"stop,omitempty"`
	// ResponseFormat 响应格式约束（如 {"type":"json_object"}）。
	// nil 表示未设置（不下发）。Python: model_config.response_format
	ResponseFormat map[string]any `json:"response_format,omitempty"`
	// ContextWindow 模型支持的上下文窗口总长（token），内部元数据，不参与厂商请求
	ContextWindow *int `json:"context_window,omitempty"`
	// Reasoning 思考/推理请求意图。
	// 可能是 *ReasoningConfig（代码内构造，对应 Python isinstance 分支），
	// 也可能是 map[string]any（从 JSON/YAML 反序列化，键集合决定新式或遗留）。
	// Python: ModelRequestConfig.reasoning: Optional[Union[ReasoningConfig, dict[str, Any]]]
	Reasoning any `json:"reasoning,omitempty"`
	// Extra 额外字段（对应 Python model_config extra="allow"），不直接参与 JSON 序列化
	Extra map[string]any `json:"-"`
}

// ModelClientConfigOption 模型客户端配置可选参数函数。
type ModelClientConfigOption func(*ModelClientConfig)

// ModelRequestConfigOption 模型请求配置可选参数函数。
type ModelRequestConfigOption func(*ModelRequestConfig)

// ──────────────────────────── 枚举 ────────────────────────────

// ProviderType 服务商类型枚举。
type ProviderType int

// ──────────────────────────── 常量 ────────────────────────────

const (
	// ProviderTypeOpenAI OpenAI 服务商
	ProviderTypeOpenAI ProviderType = iota
	// ProviderTypeOpenRouter OpenRouter 服务商
	ProviderTypeOpenRouter
	// ProviderTypeSiliconFlow SiliconFlow 服务商
	ProviderTypeSiliconFlow
	// ProviderTypeDashScope DashScope（阿里云百炼）服务商
	ProviderTypeDashScope
	// ProviderTypeDeepSeek DeepSeek 服务商
	ProviderTypeDeepSeek
	// ProviderTypeInferenceAffinity InferenceAffinity 服务商
	ProviderTypeInferenceAffinity
	// ProviderTypeIntelliRouter IntelliRouter 服务商
	ProviderTypeIntelliRouter

	// ── 以下 10 个值为 23.3 补齐（对齐上游 schema/config.py:13-31 共 17 个）──
	// 新值一律追加末尾、不按上游顺序重排：providerTypeStrings 按下标索引，
	// 中间插入会让既有常量数值整体位移。Go 侧 MarshalJSON 输出字符串，
	// 数值顺序与上游不一致无外部可见影响。

	// ProviderTypeOpenAIAccount OpenAI 账号 OAuth 服务商
	ProviderTypeOpenAIAccount
	// ProviderTypeAnthropic Anthropic 服务商
	ProviderTypeAnthropic
	// ProviderTypeMoonshot 月之暗面服务商
	ProviderTypeMoonshot
	// ProviderTypeMiniMax MiniMax 服务商
	ProviderTypeMiniMax
	// ProviderTypeModelArts 华为云 ModelArts 服务商
	ProviderTypeModelArts
	// ProviderTypeVolcEngine 火山引擎服务商
	ProviderTypeVolcEngine
	// ProviderTypeQianfan 百度千帆服务商
	ProviderTypeQianfan
	// ProviderTypeZhipu 智谱服务商
	ProviderTypeZhipu
	// ProviderTypeMiMo 小米 MiMo 服务商
	ProviderTypeMiMo
	// ProviderTypeAscendAffinity 昇腾亲和服务商（上游有枚举、无客户端、不在 api_key 白名单）
	ProviderTypeAscendAffinity
)

// kvCacheModeNone 不启用 KV cache。
const kvCacheModeNone = "none"

// kvCacheModeAffinity KV cache 亲和模式。
const kvCacheModeAffinity = "affinity"

// defaultClientTimeoutSeconds 默认请求超时（秒）。
// Python: schema/config.py:145 timeout default=360.0, gt=0——注释明文约定
// timeout >= stream_first_chunk_timeout(300)，传输层永不先于应用层流超时触发。
const defaultClientTimeoutSeconds = 360.0

// ──────────────────────────── 枚举（鉴权模式）────────────────────────────

// AuthMode LLM 鉴权模式。
// Python: schema/config.py:40-44 LLMAuthMode
// （openai_account_oauth 分支暂不生效——openai_account_model_client.py 未迁移 ⤵️）
type AuthMode string

const (
	// AuthModeAPIKey Bearer API key 鉴权（默认）
	AuthModeAPIKey AuthMode = "api_key"
	// AuthModeNone 无鉴权（自建网关 / auth_mode: none）
	AuthModeNone AuthMode = "none"
	// AuthModeCustomHeaders 靠自定义请求头携带凭据
	AuthModeCustomHeaders AuthMode = "custom_headers"
	// AuthModeOpenAIAccountOAuth OpenAI 账号 OAuth（未迁移，占位）
	AuthModeOpenAIAccountOAuth AuthMode = "openai_account_oauth"
)

// ──────────────────────────── 枚举（API 模式）────────────────────────────

// APIMode LLM 接口协议模式。
// Python: schema/config.py LLMApiMode
// 零值 "" 等价 Python 的 None（表示用 provider 默认协议，即 Chat Completions）。
type APIMode string

const (
	// APIModeChatCompletions Chat Completions 协议（/v1/chat/completions）
	APIModeChatCompletions APIMode = "chat_completions"
	// APIModeResponses Responses API 协议（/v1/responses）
	APIModeResponses APIMode = "responses"
	// APIModeAnthropicMessages Anthropic Messages 协议（/v1/messages）。
	// Python: schema/config.py:37 LLMApiMode.AnthropicMessages
	APIModeAnthropicMessages APIMode = "anthropic_messages"
)

// topLevelAPIKeyProviders 要求顶层 api_key 的 provider 白名单。
// Python: schema/config.py:111-126 _TOP_LEVEL_API_KEY_PROVIDERS
var topLevelAPIKeyProviders = map[string]struct{}{
	"OpenAI": {}, "OpenRouter": {}, "Anthropic": {}, "SiliconFlow": {},
	"DashScope": {}, "DeepSeek": {}, "Moonshot": {}, "MiniMax": {},
	"ModelArts": {}, "VolcEngine": {}, "Qianfan": {}, "Zhipu": {},
	"MiMo": {}, "InferenceAffinity": {},
}

// topLevelAPIBaseProviders 要求顶层 api_base 的 provider 白名单。
// Python: schema/config.py:127 _TOP_LEVEL_API_BASE_PROVIDERS
// （= key 白名单 ∪ {OpenAIAccount}；OpenAIAccount 客户端未迁移，白名单先对齐）
var topLevelAPIBaseProviders = map[string]struct{}{
	"OpenAI": {}, "OpenRouter": {}, "Anthropic": {}, "SiliconFlow": {},
	"DashScope": {}, "DeepSeek": {}, "Moonshot": {}, "MiniMax": {},
	"ModelArts": {}, "VolcEngine": {}, "Qianfan": {}, "Zhipu": {},
	"MiMo": {}, "InferenceAffinity": {}, "OpenAIAccount": {},
}

// defaultAPIBaseByEndpointProfile endpoint_profile 缺省 base 回填表。
// Python: schema/config.py:128-131 _DEFAULT_API_BASE_BY_ENDPOINT_PROFILE
var defaultAPIBaseByEndpointProfile = map[string]string{
	"ollama":   "http://localhost:11434/v1",
	"lmstudio": "http://localhost:1234/v1",
}

// providerTypeStrings 服务商类型名称数组，按枚举值索引。
var providerTypeStrings = [...]string{
	"OpenAI",
	"OpenRouter",
	"SiliconFlow",
	"DashScope",
	"DeepSeek",
	"InferenceAffinity",
	"intelli_router",
	// 23.3 补齐（顺序与上面常量一一对应）
	"OpenAIAccount",
	"Anthropic",
	"Moonshot",
	"MiniMax",
	"ModelArts",
	"VolcEngine",
	"Qianfan",
	"Zhipu",
	"MiMo",
	"AscendAffinity",
}

// modelClientConfigKnownKeys ModelClientConfig 已知 JSON 键名集合。
var modelClientConfigKnownKeys map[string]struct{}

// modelRequestConfigKnownKeys ModelRequestConfig 已知 JSON 键名集合。
var modelRequestConfigKnownKeys map[string]struct{}

// providerTypeMap 服务商名称到枚举值的映射表（精确匹配+小写匹配）。
var providerTypeMap map[string]ProviderType

// kvCacheModeSet KV cache 合法模式集合。
// Python: schema/config.py:48 Literal["none","affinity"]
var kvCacheModeSet = map[string]struct{}{
	kvCacheModeNone:     {},
	kvCacheModeAffinity: {},
}

// globalProviderValidator 全局服务商验证器。
var globalProviderValidator ProviderValidator

// ──────────────────────────── 导出函数 ────────────────────────────

// String 返回服务商类型的字符串表示。
func (p ProviderType) String() string {
	if int(p) >= 0 && int(p) < len(providerTypeStrings) {
		return providerTypeStrings[p]
	}
	return fmt.Sprintf("ProviderType(%d)", int(p))
}

// MarshalJSON 将 ProviderType 序列化为 JSON 字符串。
func (p ProviderType) MarshalJSON() ([]byte, error) {
	return json.Marshal(p.String())
}

// UnmarshalJSON 将 JSON 字符串反序列化为 ProviderType。
func (p *ProviderType) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		return fmt.Errorf("ProviderType 反序列化失败: %w", err)
	}
	pt, ok := ParseProviderType(s)
	if !ok {
		return fmt.Errorf("未知的 ProviderType: %q", s)
	}
	*p = pt
	return nil
}

// ParseProviderType 解析服务商类型字符串，支持精确匹配和大小写不敏感匹配。
func ParseProviderType(s string) (ProviderType, bool) {
	// 精确匹配
	if pt, ok := providerTypeMap[s]; ok {
		return pt, true
	}
	// 大小写不敏感匹配
	if pt, ok := providerTypeMap[strings.ToLower(s)]; ok {
		return pt, true
	}
	return 0, false
}

// ValidateKVCacheMode 校验 KV cache 模式是否合法。
// 上游 LEGACY_PROVIDER_ALIASES 给 InferenceAffinity 注入 mode="release"，但
// KVCacheExtensionConfig 的 Literal 不含它、下游也无 release 分支——Go 跟随
// 上游拒绝（偏差 D-10），不自行放宽。
// Python: schema/config.py:48 Literal["none","affinity"]
func ValidateKVCacheMode(mode string) error {
	if mode == "" {
		return nil
	}
	if _, ok := kvCacheModeSet[mode]; !ok {
		return fmt.Errorf("kv_cache.mode 必须是 none 或 affinity，收到 %q", mode)
	}
	return nil
}

// ValidateAndNormalizeProvider 验证并规范化服务商名称，依次尝试枚举匹配、大小写不敏感匹配和全局验证器。
// Python: validate_client_provider — 未知 provider 返回 MODEL_PROVIDER_INVALID 错误。
func ValidateAndNormalizeProvider(provider string) (string, error) {
	provider = strings.TrimSpace(provider)

	// 1. 精确匹配枚举
	if pt, ok := providerTypeMap[provider]; ok {
		return pt.String(), nil
	}

	// 2. 大小写不敏感匹配
	if pt, ok := providerTypeMap[strings.ToLower(provider)]; ok {
		return pt.String(), nil
	}

	// 3. 查询 ProviderValidator
	if globalProviderValidator != nil {
		if normalized := globalProviderValidator.ValidateProvider(provider); normalized != "" {
			return normalized, nil
		}
	}

	// 4. 对齐 Python: raise build_error(MODEL_PROVIDER_INVALID) — 严格拒绝未知 provider
	supportedTypes := make([]string, 0, len(providerTypeMap))
	for k := range providerTypeMap {
		supportedTypes = append(supportedTypes, k)
	}
	return "", exception.NewBaseError(
		exception.StatusModelProviderInvalid,
		exception.WithMsg(fmt.Sprintf("unavailable model provider: %s, and available providers are: %v", provider, supportedTypes)),
	)
}

// ProviderRequiresAPIKey 判 provider 是否要求顶层 api_key（白名单）。
// Python: schema/config.py:111-126 _TOP_LEVEL_API_KEY_PROVIDERS
func ProviderRequiresAPIKey(provider string) bool {
	_, ok := topLevelAPIKeyProviders[provider]
	return ok
}

// ProviderRequiresAPIBase 判 provider 是否要求顶层 api_base（白名单）。
// Python: schema/config.py:127 _TOP_LEVEL_API_BASE_PROVIDERS
func ProviderRequiresAPIBase(provider string) bool {
	_, ok := topLevelAPIBaseProviders[provider]
	return ok
}

// SetProviderValidator 设置全局服务商验证器。
func SetProviderValidator(v ProviderValidator) {
	globalProviderValidator = v
}

// WithClientID 设置客户端唯一标识。
func WithClientID(id string) ModelClientConfigOption {
	return func(c *ModelClientConfig) { c.ClientID = id }
}

// WithAuthMode 设置鉴权模式。
func WithAuthMode(mode AuthMode) ModelClientConfigOption {
	return func(c *ModelClientConfig) { c.AuthMode = mode }
}

// WithEndpointProfile 设置端点配置档名（ollama/lmstudio 等）。
func WithEndpointProfile(profile string) ModelClientConfigOption {
	return func(c *ModelClientConfig) { c.EndpointProfile = profile }
}

// WithTimeout 设置请求超时时间（秒）。
func WithTimeout(timeout float64) ModelClientConfigOption {
	return func(c *ModelClientConfig) { c.Timeout = timeout }
}

// WithStreamFirstChunkTimeout 设置首帧流超时（秒）；nil 表示禁用。
func WithStreamFirstChunkTimeout(seconds *float64) ModelClientConfigOption {
	return func(c *ModelClientConfig) { c.StreamFirstChunkTimeout = seconds }
}

// WithStreamIdleTimeout 设置空闲流超时（秒）；nil 表示禁用。
func WithStreamIdleTimeout(seconds *float64) ModelClientConfigOption {
	return func(c *ModelClientConfig) { c.StreamIdleTimeout = seconds }
}

// WithMaxRetries 设置最大重试次数。
func WithMaxRetries(maxRetries int) ModelClientConfigOption {
	return func(c *ModelClientConfig) { c.MaxRetries = maxRetries }
}

// WithVerifySSL 设置是否验证 SSL 证书。
func WithVerifySSL(verify bool) ModelClientConfigOption {
	return func(c *ModelClientConfig) { c.VerifySSL = verify }
}

// WithSSLCert 设置 SSL 证书文件路径。
func WithSSLCert(cert string) ModelClientConfigOption {
	return func(c *ModelClientConfig) { c.SSLCert = cert }
}

// WithCustomHeaders 设置自定义请求头。
func WithCustomHeaders(headers map[string]string) ModelClientConfigOption {
	return func(c *ModelClientConfig) { c.CustomHeaders = headers }
}

// WithConfigExtra 设置客户端配置的额外字段。
func WithConfigExtra(extra map[string]any) ModelClientConfigOption {
	return func(c *ModelClientConfig) { c.Extra = extra }
}

// NewModelClientConfig 创建模型客户端配置，默认超时 60s、重试 3 次、验证 SSL、
// 流帧首帧 300s / 空闲 60s（对齐 Python 字段默认值）。
// 构造完成后自动调用 Validate，校验必填字段并规范化 provider。
func NewModelClientConfig(provider, apiKey, apiBase string, opts ...ModelClientConfigOption) (*ModelClientConfig, error) {
	defaultFirstChunk := 300.0
	defaultIdle := 60.0
	cfg := &ModelClientConfig{
		ClientID:                uuid.New().String(),
		ClientProvider:          provider,
		APIKey:                  apiKey,
		APIBase:                 apiBase,
		Timeout:                 defaultClientTimeoutSeconds,
		StreamFirstChunkTimeout: &defaultFirstChunk,
		StreamIdleTimeout:       &defaultIdle,
		MaxRetries:              3,
		VerifySSL:               true,
	}
	for _, opt := range opts {
		opt(cfg)
	}
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return cfg, nil
}

// Validate 校验模型客户端配置必填字段并规范化 provider。
// normalize 就地归一 provider / auth_mode / api_base，不做必填校验。
//
// 与 Validate 共用，保证「只归一不校验」的调用方（如
// NormalizeModelClientConfig）行为一致。返回 provider 归一错误供调用方决定。
func (c *ModelClientConfig) normalize() error {
	normalized, err := ValidateAndNormalizeProvider(c.ClientProvider)
	if err != nil {
		return err
	}
	c.ClientProvider = normalized

	// auth_mode 归一（Go string 零值是 ""，Python 默认 ApiKey）
	if c.AuthMode == "" {
		c.AuthMode = AuthModeAPIKey
	}

	// endpoint_profile 缺省 base 回填
	// Python: schema/config.py:236-242 _validate_top_level_provider_config
	if strings.TrimSpace(c.APIBase) == "" && c.EndpointProfile != "" {
		if def, ok := defaultAPIBaseByEndpointProfile[strings.ToLower(strings.TrimSpace(c.EndpointProfile))]; ok {
			c.APIBase = def
		}
	}
	return nil
}

// Validate 就地归一配置后校验必填项，返回第一个不合法的原因。
func (c *ModelClientConfig) Validate() error {
	// 归一 provider / auth_mode / api_base（与 NormalizeModelClientConfig 共用）
	if err := c.normalize(); err != nil {
		return err
	}

	if c.ClientProvider == "" {
		return fmt.Errorf("client_provider 不能为空")
	}

	// 凭据必填判定（对齐 Python:225-256）
	// Python 语义：auth_mode != api_key → 不要求 key；否则 provider 在 key 白名单才要求
	requiresAPIKey := false
	if c.AuthMode == AuthModeAPIKey {
		_, requiresAPIKey = topLevelAPIKeyProviders[c.ClientProvider]
	}
	if requiresAPIKey && strings.TrimSpace(c.APIKey) == "" {
		return fmt.Errorf("api_key is required for provider %s", c.ClientProvider)
	}
	// base 白名单：provider 在白名单内才要求 api_base
	if _, requiresBase := topLevelAPIBaseProviders[c.ClientProvider]; requiresBase && strings.TrimSpace(c.APIBase) == "" {
		return fmt.Errorf("api_base is required for provider %s", c.ClientProvider)
	}

	if c.Timeout <= 0 {
		return fmt.Errorf("timeout 必须 > 0，当前值: %f", c.Timeout)
	}
	return nil
}

// MarshalJSON 序列化 ModelClientConfig，合并 Extra 字段到 JSON 输出。
func (c *ModelClientConfig) MarshalJSON() ([]byte, error) {
	// 使用别名避免无限递归
	type Alias ModelClientConfig
	alias := (*Alias)(c)

	// 序列化标准字段
	data, err := json.Marshal(alias)
	if err != nil {
		return nil, fmt.Errorf("ModelClientConfig 序列化失败: %w", err)
	}

	// 无 Extra 字段，直接返回
	if len(c.Extra) == 0 {
		return data, nil
	}

	// 合并 Extra 字段
	var baseMap map[string]any
	if err := json.Unmarshal(data, &baseMap); err != nil {
		return nil, fmt.Errorf("ModelClientConfig 合并 Extra 失败: %w", err)
	}
	for k, v := range c.Extra {
		baseMap[k] = v
	}
	return json.Marshal(baseMap)
}

// UnmarshalJSON 反序列化 ModelClientConfig，拆分已知字段和 Extra 字段。
func (c *ModelClientConfig) UnmarshalJSON(data []byte) error {
	// 先解析为通用 map
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		return fmt.Errorf("ModelClientConfig 反序列化失败: %w", err)
	}

	// 拆分已知字段和 Extra
	known := make(map[string]any)
	extra := make(map[string]any)
	for k, v := range raw {
		if _, ok := modelClientConfigKnownKeys[k]; ok {
			known[k] = v
		} else {
			extra[k] = v
		}
	}

	// 重新序列化已知字段并解析到结构体
	knownData, err := json.Marshal(known)
	if err != nil {
		return fmt.Errorf("ModelClientConfig 格式转换失败: %w", err)
	}

	type Alias ModelClientConfig
	var alias Alias
	if err := json.Unmarshal(knownData, &alias); err != nil {
		return fmt.Errorf("ModelClientConfig 反序列化失败: %w", err)
	}
	*c = ModelClientConfig(alias)

	// 存储 Extra 字段
	if len(extra) > 0 {
		c.Extra = extra
	}

	return nil
}

// WithModelName 设置模型名称。
func WithModelName(name string) ModelRequestConfigOption {
	return func(c *ModelRequestConfig) { c.ModelName = name }
}

// WithTemperature 设置温度参数（指针形，nil 表示不设置）。
func WithTemperature(temp *float64) ModelRequestConfigOption {
	return func(c *ModelRequestConfig) { c.Temperature = temp }
}

// WithTemperatureValue 设置温度参数（值便捷形，内部取址）。
func WithTemperatureValue(temp float64) ModelRequestConfigOption {
	return func(c *ModelRequestConfig) { c.Temperature = &temp }
}

// WithTopP 设置 Top-p 采样参数。
func WithTopP(topP float64) ModelRequestConfigOption {
	return func(c *ModelRequestConfig) { c.TopP = &topP }
}

// WithMaxTokens 设置最大生成 token 数。
func WithMaxTokens(maxTokens int) ModelRequestConfigOption {
	return func(c *ModelRequestConfig) { c.MaxTokens = &maxTokens }
}

// WithContextWindow 设置模型上下文窗口（内部元数据，不随请求下发）。
// 对齐上游 ModelRequestConfig.context_window：请求参数组装时显式排除。
// 本仓请求参数由 base_client 以 map 拼装且只合并 Extra，该字段不进 Extra 即天然不外发
// （由 config_window_test.go 固化）。
func WithContextWindow(tokens int) ModelRequestConfigOption {
	return func(c *ModelRequestConfig) {
		if tokens > 0 {
			c.ContextWindow = &tokens
		}
	}
}

// WithStop 设置停止序列。
func WithStop(stop string) ModelRequestConfigOption {
	return func(c *ModelRequestConfig) { c.Stop = &stop }
}

// WithRequestExtra 设置模型请求配置的额外字段。
func WithRequestExtra(extra map[string]any) ModelRequestConfigOption {
	return func(c *ModelRequestConfig) { c.Extra = extra }
}

// WithReasoning 设置思考/推理请求意图。
// Python: ModelRequestConfig.reasoning: Optional[Union[ReasoningConfig, dict[str, Any]]]
func WithReasoning(reasoning any) ModelRequestConfigOption {
	return func(c *ModelRequestConfig) { c.Reasoning = reasoning }
}

// NewModelRequestConfig 创建模型请求配置，默认温度 0.95、TopP 0.1（对齐 Python）。
func NewModelRequestConfig(opts ...ModelRequestConfigOption) *ModelRequestConfig {
	// 采样参数默认 nil（未设置不下发）——Python: schema/config.py:262-266
	// 应用层需要 0.95 的地方自行显式设置（deep_adapter / default_models / runtime_inheritance）
	cfg := &ModelRequestConfig{}
	for _, opt := range opts {
		opt(cfg)
	}
	return cfg
}

// MarshalJSON 序列化 ModelRequestConfig，合并 Extra 字段到 JSON 输出。
func (c *ModelRequestConfig) MarshalJSON() ([]byte, error) {
	type Alias ModelRequestConfig
	alias := (*Alias)(c)

	data, err := json.Marshal(alias)
	if err != nil {
		return nil, fmt.Errorf("ModelRequestConfig 序列化失败: %w", err)
	}

	if len(c.Extra) == 0 {
		return data, nil
	}

	var baseMap map[string]any
	if err := json.Unmarshal(data, &baseMap); err != nil {
		return nil, fmt.Errorf("ModelRequestConfig 合并 Extra 失败: %w", err)
	}
	for k, v := range c.Extra {
		baseMap[k] = v
	}
	return json.Marshal(baseMap)
}

// UnmarshalJSON 反序列化 ModelRequestConfig，拆分已知字段和 Extra 字段。
func (c *ModelRequestConfig) UnmarshalJSON(data []byte) error {
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		return fmt.Errorf("ModelRequestConfig 反序列化失败: %w", err)
	}

	known := make(map[string]any)
	extra := make(map[string]any)
	for k, v := range raw {
		if _, ok := modelRequestConfigKnownKeys[k]; ok {
			known[k] = v
		} else {
			extra[k] = v
		}
	}

	knownData, err := json.Marshal(known)
	if err != nil {
		return fmt.Errorf("ModelRequestConfig 格式转换失败: %w", err)
	}

	type Alias ModelRequestConfig
	var alias Alias
	if err := json.Unmarshal(knownData, &alias); err != nil {
		return fmt.Errorf("ModelRequestConfig 反序列化失败: %w", err)
	}
	*c = ModelRequestConfig(alias)

	if len(extra) > 0 {
		c.Extra = extra
	}

	return nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

func init() {
	// 初始化 providerTypeMap：精确匹配 + 大小写不敏感匹配
	providerTypeMap = make(map[string]ProviderType, len(providerTypeStrings)*2)
	for i, s := range providerTypeStrings {
		// 精确匹配
		providerTypeMap[s] = ProviderType(i)
		// 大小写不敏感匹配（小写形式）
		providerTypeMap[strings.ToLower(s)] = ProviderType(i)
	}

	// 初始化 ModelClientConfig 已知 JSON 键名集合
	modelClientConfigKnownKeys = map[string]struct{}{
		"client_id":                  {},
		"client_provider":            {},
		"api_key":                    {},
		"api_base":                   {},
		"timeout":                    {},
		"auth_mode":                  {},
		"api_mode":                   {},
		"endpoint_profile":           {},
		"stream_first_chunk_timeout": {},
		"stream_idle_timeout":        {},
		"max_retries":                {},
		"verify_ssl":                 {},
		"ssl_cert":                   {},
		"custom_headers":             {},
		"extensions":                 {},
	}

	// 初始化 ModelRequestConfig 已知 JSON 键名集合
	modelRequestConfigKnownKeys = map[string]struct{}{
		"model":           {},
		"temperature":     {},
		"top_p":           {},
		"max_tokens":      {},
		"stop":            {},
		"response_format": {},
		// context_window 必须是已知键：它是内部上下文元数据，由 context engine
		// 消费，绝不能作为厂商请求参数下发。若落进 Extra，会被
		// BaseClientEmbed.BuildRequestParams 平铺进请求体误发给厂商。
		// Python: base_model_client.py:631-633 model_dump(exclude={"context_window"})
		"context_window": {},
		// reasoning 必须是已知键：否则会落进 Extra，被 BuildRequestParams
		// 平铺进请求参数误发给厂商（Python 侧 model_dump 显式 exclude 它）。
		"reasoning": {},
	}
}

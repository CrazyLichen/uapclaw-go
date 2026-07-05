package schema

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/google/uuid"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ProviderValidator 自定义 Provider 验证器接口。
//
// 当 ProviderType 枚举无法匹配时，通过此接口查询外部注册的 provider。
// 后续领域（2.6+ 实现 ClientRegistry 后）注入具体实现，
// 当前阶段默认不做额外验证（宽松策略）。
type ProviderValidator interface {
	// ValidateProvider 验证 provider 是否已注册。
	// 已注册返回规范化名称，未注册返回空字符串。
	ValidateProvider(provider string) string
}

// ModelClientConfig 模型客户端配置，用于注册和创建 ModelClient 实例。
//
// Extra 字段实现 Python Pydantic model_config extra="allow" 的等价语义：
//   - 序列化时：标准字段与 Extra 合并输出为平级 JSON 对象
//   - 反序列化时：已知 key 映射到标准字段，未知 key 存入 Extra
//
// 对应 Python: openjiuwen/core/foundation/llm/schema/config.py (ModelClientConfig)
type ModelClientConfig struct {
	// ClientID 客户端唯一标识，用于 Runner 注册，默认自动生成 UUID
	ClientID string `json:"client_id"`
	// ClientProvider 服务商标识，支持 ProviderType 枚举值或自定义字符串
	ClientProvider string `json:"client_provider"`
	// APIKey API 密钥
	APIKey string `json:"api_key"`
	// APIBase API 基础 URL
	APIBase string `json:"api_base"`
	// Timeout 请求超时时间（秒），必须 > 0，默认 60
	Timeout float64 `json:"timeout"`
	// MaxRetries 最大重试次数，默认 3
	MaxRetries int `json:"max_retries"`
	// VerifySSL 是否验证 SSL 证书，默认 true
	VerifySSL bool `json:"verify_ssl"`
	// SSLCert SSL 证书文件路径（可选）
	SSLCert string `json:"ssl_cert,omitempty"`
	// CustomHeaders 开发者自定义请求头，每次 LLM 调用时合并
	CustomHeaders map[string]string `json:"custom_headers,omitempty"`
	// Extra 额外字段（对应 Python model_config extra="allow"），不直接参与 JSON 序列化
	Extra map[string]any `json:"-"`
}

// ModelRequestConfig 模型请求配置，每次 LLM 调用时的参数。
//
// ModelName 字段在 JSON 中使用 "model" 键名（对应 Python 的 alias="model"）。
// Extra 字段实现 Python Pydantic model_config extra="allow" 的等价语义。
//
// 对应 Python: openjiuwen/core/foundation/llm/schema/config.py (ModelRequestConfig)
type ModelRequestConfig struct {
	// ModelName 模型名称，如 "gpt-4"，JSON 键名为 "model"
	ModelName string `json:"model"`
	// Temperature 温度参数，控制输出随机性，默认 0.95
	Temperature float64 `json:"temperature"`
	// TopP Top-p 采样参数，默认 0.1
	TopP float64 `json:"top_p"`
	// MaxTokens 最大生成 token 数（可选）
	MaxTokens *int `json:"max_tokens,omitempty"`
	// Stop 停止序列（可选）
	Stop *string `json:"stop,omitempty"`
	// Extra 额外字段（对应 Python model_config extra="allow"），不直接参与 JSON 序列化
	Extra map[string]any `json:"-"`
}

// ──────────────────────────── 枚举 ────────────────────────────

// ProviderType 模型服务提供商标识枚举，用于标识 LLM API 的服务提供商。
//
// 枚举值序列化为字符串（与 Python ProviderType 一致），
// 例如 ProviderTypeOpenAI 序列化为 "OpenAI"。
//
// 对应 Python: openjiuwen/core/foundation/llm/schema/config.py (ProviderType)
type ProviderType int

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
)

// ModelClientConfigOption ModelClientConfig 构造选项函数。
type ModelClientConfigOption func(*ModelClientConfig)

// ModelRequestConfigOption ModelRequestConfig 构造选项函数。
type ModelRequestConfigOption func(*ModelRequestConfig)

// ──────────────────────────── 全局变量 ────────────────────────────

// providerTypeStrings ProviderType 枚举值对应的字符串表示，与 Python 端保持一致。
var providerTypeStrings = [...]string{
	"OpenAI",
	"OpenRouter",
	"SiliconFlow",
	"DashScope",
	"DeepSeek",
	"InferenceAffinity",
	"intelli_router",
}

// modelClientConfigKnownKeys ModelClientConfig 已知的 JSON 键名集合，用于 Extra 字段拆分。
var modelClientConfigKnownKeys map[string]struct{}

// modelRequestConfigKnownKeys ModelRequestConfig 已知的 JSON 键名集合，用于 Extra 字段拆分。
var modelRequestConfigKnownKeys map[string]struct{}

// providerTypeMap 字符串到 ProviderType 的映射，用于 JSON 反序列化。
var providerTypeMap map[string]ProviderType

// globalProviderValidator 全局 Provider 验证器，默认 nil（不做额外验证）。
var globalProviderValidator ProviderValidator

// ──────────────────────────── 导出函数 ────────────────────────────

// String 实现 fmt.Stringer 接口，返回 ProviderType 的字符串表示。
func (p ProviderType) String() string {
	if int(p) >= 0 && int(p) < len(providerTypeStrings) {
		return providerTypeStrings[p]
	}
	return fmt.Sprintf("ProviderType(%d)", int(p))
}

// MarshalJSON 实现 json.Marshaler 接口，将 ProviderType 序列化为字符串。
func (p ProviderType) MarshalJSON() ([]byte, error) {
	return json.Marshal(p.String())
}

// UnmarshalJSON 实现 json.Unmarshaler 接口，将字符串反序列化为 ProviderType。
// 支持精确匹配和大小写不敏感匹配。
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

// ParseProviderType 从字符串解析 ProviderType。
//
// 匹配策略：
//  1. 精确匹配枚举字符串值
//  2. 大小写不敏感匹配
//
// 匹配成功返回 (ProviderType, true)，否则返回 (0, false)。
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

// ValidateAndNormalizeProvider 验证并规范化 provider 字符串。
//
// 匹配策略（按优先级）：
//  1. 精确匹配 ProviderType 枚举 → 返回规范化字符串
//  2. 大小写不敏感匹配 → 返回规范化字符串
//  3. 查询 ProviderValidator（如已注入）→ 返回验证后的名称
//  4. 都不匹配 → 保留原字符串不报错（宽松策略）
//
// 对应 Python: ModelClientConfig.validate_client_provider()
func ValidateAndNormalizeProvider(provider string) string {
	provider = strings.TrimSpace(provider)

	// 1. 精确匹配枚举
	if pt, ok := providerTypeMap[provider]; ok {
		return pt.String()
	}

	// 2. 大小写不敏感匹配
	if pt, ok := providerTypeMap[strings.ToLower(provider)]; ok {
		return pt.String()
	}

	// 3. 查询 ProviderValidator
	if globalProviderValidator != nil {
		if normalized := globalProviderValidator.ValidateProvider(provider); normalized != "" {
			return normalized
		}
	}

	// 4. 宽松策略：保留原字符串
	return provider
}

// SetProviderValidator 设置全局 Provider 验证器。
//
// 后续领域（2.6+ 实现 ClientRegistry 后）调用此函数注入具体实现。
// 传入 nil 可重置为默认行为（不做额外验证）。
func SetProviderValidator(v ProviderValidator) {
	globalProviderValidator = v
}

// WithClientID 设置客户端唯一标识。
func WithClientID(id string) ModelClientConfigOption {
	return func(c *ModelClientConfig) { c.ClientID = id }
}

// WithTimeout 设置请求超时时间（秒）。
func WithTimeout(timeout float64) ModelClientConfigOption {
	return func(c *ModelClientConfig) { c.Timeout = timeout }
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

// WithConfigExtra 设置额外字段。
func WithConfigExtra(extra map[string]any) ModelClientConfigOption {
	return func(c *ModelClientConfig) { c.Extra = extra }
}

// NewModelClientConfig 创建 ModelClientConfig 实例。
//
// 默认值：
//   - ClientID: 自动生成 UUID
//   - Timeout: 60.0
//   - MaxRetries: 3
//   - VerifySSL: true
//
// provider, apiKey, apiBase 为必填参数。
//
// 对应 Python: ModelClientConfig(client_provider=..., api_key=..., api_base=..., ...)
func NewModelClientConfig(provider, apiKey, apiBase string, opts ...ModelClientConfigOption) *ModelClientConfig {
	cfg := &ModelClientConfig{
		ClientID:       uuid.New().String(),
		ClientProvider: provider,
		APIKey:         apiKey,
		APIBase:        apiBase,
		Timeout:        60.0,
		MaxRetries:     3,
		VerifySSL:      true,
	}
	for _, opt := range opts {
		opt(cfg)
	}
	return cfg
}

// Validate 校验 ModelClientConfig 的必填字段和参数有效性。
//
// 校验规则：
//   - ClientProvider: 规范化（ValidateAndNormalizeProvider）
//   - APIKey: 必填
//   - APIBase: 必填
//   - Timeout: 必须 > 0
func (c *ModelClientConfig) Validate() error {
	// 规范化 provider
	c.ClientProvider = ValidateAndNormalizeProvider(c.ClientProvider)

	if c.ClientProvider == "" {
		return fmt.Errorf("client_provider 不能为空")
	}
	if c.APIKey == "" {
		return fmt.Errorf("api_key 不能为空")
	}
	if c.APIBase == "" {
		return fmt.Errorf("api_base 不能为空")
	}
	if c.Timeout <= 0 {
		return fmt.Errorf("timeout 必须 > 0，当前值: %f", c.Timeout)
	}
	return nil
}

// MarshalJSON 实现 json.Marshaler 接口。
// 标准字段与 Extra 合并输出为平级 JSON 对象，与 Python extra="allow" 行为一致。
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

// UnmarshalJSON 实现 json.Unmarshaler 接口。
// 已知 key 映射到标准字段，未知 key 存入 Extra，与 Python extra="allow" 行为一致。
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

// WithTemperature 设置温度参数。
func WithTemperature(temp float64) ModelRequestConfigOption {
	return func(c *ModelRequestConfig) { c.Temperature = temp }
}

// WithTopP 设置 Top-p 采样参数。
func WithTopP(topP float64) ModelRequestConfigOption {
	return func(c *ModelRequestConfig) { c.TopP = topP }
}

// WithMaxTokens 设置最大生成 token 数。
func WithMaxTokens(maxTokens int) ModelRequestConfigOption {
	return func(c *ModelRequestConfig) { c.MaxTokens = &maxTokens }
}

// WithStop 设置停止序列。
func WithStop(stop string) ModelRequestConfigOption {
	return func(c *ModelRequestConfig) { c.Stop = &stop }
}

// WithRequestExtra 设置额外字段。
func WithRequestExtra(extra map[string]any) ModelRequestConfigOption {
	return func(c *ModelRequestConfig) { c.Extra = extra }
}

// NewModelRequestConfig 创建 ModelRequestConfig 实例。
//
// 默认值：
//   - ModelName: ""
//   - Temperature: 0.95
//   - TopP: 0.1
//   - MaxTokens: nil
//   - Stop: nil
//
// 对应 Python: ModelRequestConfig(model=..., temperature=..., top_p=..., ...)
func NewModelRequestConfig(opts ...ModelRequestConfigOption) *ModelRequestConfig {
	cfg := &ModelRequestConfig{
		Temperature: 0.95,
		TopP:        0.1,
	}
	for _, opt := range opts {
		opt(cfg)
	}
	return cfg
}

// MarshalJSON 实现 json.Marshaler 接口。
// 标准字段与 Extra 合并输出为平级 JSON 对象，与 Python extra="allow" 行为一致。
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

// UnmarshalJSON 实现 json.Unmarshaler 接口。
// 已知 key 映射到标准字段，未知 key 存入 Extra，与 Python extra="allow" 行为一致。
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
		"client_id":       {},
		"client_provider": {},
		"api_key":         {},
		"api_base":        {},
		"timeout":         {},
		"max_retries":     {},
		"verify_ssl":      {},
		"ssl_cert":        {},
		"custom_headers":  {},
	}

	// 初始化 ModelRequestConfig 已知 JSON 键名集合
	modelRequestConfigKnownKeys = map[string]struct{}{
		"model":       {},
		"temperature": {},
		"top_p":       {},
		"max_tokens":  {},
		"stop":        {},
	}
}

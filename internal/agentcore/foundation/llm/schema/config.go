package schema

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/google/uuid"
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

// ModelRequestConfig 模型请求配置。
type ModelRequestConfig struct {
	// ModelName 模型名称，如 "gpt-4"，JSON 键名为 "model"
	ModelName string `json:"model"`
	// Temperature 温度参数，控制输出随机性，默认 0.95
	Temperature float64 `json:"temperature"`
	// TopP Top-p 采样参数（可选），对齐 Python: 不设置时为 nil，不传给模型
	TopP *float64 `json:"top_p,omitempty"`
	// MaxTokens 最大生成 token 数（可选）
	MaxTokens *int `json:"max_tokens,omitempty"`
	// Stop 停止序列（可选）
	Stop *string `json:"stop,omitempty"`
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
)

// ──────────────────────────── 全局变量 ────────────────────────────

// providerTypeStrings 服务商类型名称数组，按枚举值索引。
var providerTypeStrings = [...]string{
	"OpenAI",
	"OpenRouter",
	"SiliconFlow",
	"DashScope",
	"DeepSeek",
	"InferenceAffinity",
	"intelli_router",
}

// modelClientConfigKnownKeys ModelClientConfig 已知 JSON 键名集合。
var modelClientConfigKnownKeys map[string]struct{}

// modelRequestConfigKnownKeys ModelRequestConfig 已知 JSON 键名集合。
var modelRequestConfigKnownKeys map[string]struct{}

// providerTypeMap 服务商名称到枚举值的映射表（精确匹配+小写匹配）。
var providerTypeMap map[string]ProviderType

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

// ValidateAndNormalizeProvider 验证并规范化服务商名称，依次尝试枚举匹配、大小写不敏感匹配和全局验证器。
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

// SetProviderValidator 设置全局服务商验证器。
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

// WithConfigExtra 设置客户端配置的额外字段。
func WithConfigExtra(extra map[string]any) ModelClientConfigOption {
	return func(c *ModelClientConfig) { c.Extra = extra }
}

// NewModelClientConfig 创建模型客户端配置，默认超时 60s、重试 3 次、验证 SSL。
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

// Validate 校验模型客户端配置必填字段并规范化 provider。
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

// WithTemperature 设置温度参数。
func WithTemperature(temp float64) ModelRequestConfigOption {
	return func(c *ModelRequestConfig) { c.Temperature = temp }
}

// WithTopP 设置 Top-p 采样参数。
func WithTopP(topP float64) ModelRequestConfigOption {
	return func(c *ModelRequestConfig) { c.TopP = &topP }
}

// WithMaxTokens 设置最大生成 token 数。
func WithMaxTokens(maxTokens int) ModelRequestConfigOption {
	return func(c *ModelRequestConfig) { c.MaxTokens = &maxTokens }
}

// WithStop 设置停止序列。
func WithStop(stop string) ModelRequestConfigOption {
	return func(c *ModelRequestConfig) { c.Stop = &stop }
}

// WithRequestExtra 设置模型请求配置的额外字段。
func WithRequestExtra(extra map[string]any) ModelRequestConfigOption {
	return func(c *ModelRequestConfig) { c.Extra = extra }
}

// NewModelRequestConfig 创建模型请求配置，默认温度 0.95。
func NewModelRequestConfig(opts ...ModelRequestConfigOption) *ModelRequestConfig {
	cfg := &ModelRequestConfig{
		Temperature: 0.95,
	}
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

package schema

import (
	"encoding/json"
	"fmt"
)

// ──────────────────────────── 结构体 ────────────────────────────

// BaseModelInfo 模型基础信息，将 provider 配置和请求参数合并为单一结构体。
//
// 用于 BaseModelClient 的初始化，包含 API 连接信息和模型参数。
// ModelName 字段在 JSON 中使用 "model" 键名（对应 Python 的 alias="model"）。
// Streaming 字段在 JSON 中使用 "stream" 键名（对应 Python 的 alias="stream"）。
//
// Extra 字段实现 Python Pydantic model_config extra="allow" 的等价语义：
//   - 序列化时：标准字段与 Extra 合并输出为平级 JSON 对象
//   - 反序列化时：已知 key 映射到标准字段，未知 key 存入 Extra
//
// 对应 Python: openjiuwen/core/foundation/llm/schema/mode_info.py (BaseModelInfo)
type BaseModelInfo struct {
	// APIKey API 密钥，默认空
	APIKey string `json:"api_key"`
	// APIBase API 基础 URL，必填
	APIBase string `json:"api_base"`
	// ModelName 模型名称，JSON 键名为 "model"
	ModelName string `json:"model"`
	// Temperature 温度参数，默认 0.95
	Temperature float64 `json:"temperature"`
	// TopP Top-p 采样参数，默认 0.1
	TopP float64 `json:"top_p"`
	// Streaming 是否流式输出，JSON 键名为 "stream"，默认 false
	Streaming bool `json:"stream"`
	// Timeout 请求超时时间（秒），必须 > 0，默认 60
	Timeout int `json:"timeout"`
	// CustomHeaders 开发者自定义请求头
	CustomHeaders map[string]string `json:"custom_headers,omitempty"`
	// Extra 额外字段（对应 Python model_config extra="allow"），不直接参与 JSON 序列化
	Extra map[string]any `json:"-"`
}

// ModelConfig 模型配置，组合 provider 名称和模型信息。
//
// 对应 Python: openjiuwen/core/foundation/llm/schema/mode_info.py (ModelConfig)
type ModelConfig struct {
	// ModelProvider 模型服务提供商标识
	ModelProvider string `json:"model_provider"`
	// ModelInfo 模型基础信息
	ModelInfo BaseModelInfo `json:"model_info"`
}

// ──────────────────────────── 常量 ────────────────────────────

// baseModelInfoKnownKeys BaseModelInfo 已知的 JSON 键名集合，用于 Extra 字段拆分。
var baseModelInfoKnownKeys map[string]struct{}

// ──────────────────────────── 导出函数 ────────────────────────────

// BaseModelInfoOption BaseModelInfo 构造选项函数。
type BaseModelInfoOption func(*BaseModelInfo)

// WithAPIKey 设置 API 密钥。
func WithAPIKey(key string) BaseModelInfoOption {
	return func(info *BaseModelInfo) { info.APIKey = key }
}

// WithModelNameForInfo 设置模型名称。
func WithModelNameForInfo(name string) BaseModelInfoOption {
	return func(info *BaseModelInfo) { info.ModelName = name }
}

// WithTemperatureForInfo 设置温度参数。
func WithTemperatureForInfo(temp float64) BaseModelInfoOption {
	return func(info *BaseModelInfo) { info.Temperature = temp }
}

// WithTopPForInfo 设置 Top-p 采样参数。
func WithTopPForInfo(topP float64) BaseModelInfoOption {
	return func(info *BaseModelInfo) { info.TopP = topP }
}

// WithStreaming 设置是否流式输出。
func WithStreaming(streaming bool) BaseModelInfoOption {
	return func(info *BaseModelInfo) { info.Streaming = streaming }
}

// WithTimeoutForInfo 设置请求超时时间（秒）。
func WithTimeoutForInfo(timeout int) BaseModelInfoOption {
	return func(info *BaseModelInfo) { info.Timeout = timeout }
}

// WithCustomHeadersForInfo 设置自定义请求头。
func WithCustomHeadersForInfo(headers map[string]string) BaseModelInfoOption {
	return func(info *BaseModelInfo) { info.CustomHeaders = headers }
}

// WithInfoExtra 设置额外字段。
func WithInfoExtra(extra map[string]any) BaseModelInfoOption {
	return func(info *BaseModelInfo) { info.Extra = extra }
}

// NewBaseModelInfo 创建 BaseModelInfo 实例，apiBase 为必填参数。
//
// 默认值：
//   - APIKey: ""
//   - ModelName: ""
//   - Temperature: 0.95
//   - TopP: 0.1
//   - Streaming: false
//   - Timeout: 60
//
// 对应 Python: BaseModelInfo(api_base=..., model=..., temperature=..., ...)
func NewBaseModelInfo(apiBase string, opts ...BaseModelInfoOption) *BaseModelInfo {
	info := &BaseModelInfo{
		APIBase:     apiBase,
		Temperature: 0.95,
		TopP:        0.1,
		Timeout:     60,
	}
	for _, opt := range opts {
		opt(info)
	}
	return info
}

// Validate 校验 BaseModelInfo 的参数有效性。
//
// 校验规则：
//   - APIBase: 必填（非空）
//   - Timeout: 必须 > 0
func (info *BaseModelInfo) Validate() error {
	if info.APIBase == "" {
		return fmt.Errorf("api_base 不能为空")
	}
	if info.Timeout <= 0 {
		return fmt.Errorf("timeout 必须 > 0，当前值: %d", info.Timeout)
	}
	return nil
}

// MarshalJSON 实现 json.Marshaler 接口。
// 标准字段与 Extra 合并输出为平级 JSON 对象，与 Python extra="allow" 行为一致。
func (info *BaseModelInfo) MarshalJSON() ([]byte, error) {
	type Alias BaseModelInfo
	alias := (*Alias)(info)

	data, err := json.Marshal(alias)
	if err != nil {
		return nil, fmt.Errorf("BaseModelInfo 序列化失败: %w", err)
	}

	if len(info.Extra) == 0 {
		return data, nil
	}

	var baseMap map[string]any
	if err := json.Unmarshal(data, &baseMap); err != nil {
		return nil, fmt.Errorf("BaseModelInfo 合并 Extra 失败: %w", err)
	}
	for k, v := range info.Extra {
		baseMap[k] = v
	}
	return json.Marshal(baseMap)
}

// UnmarshalJSON 实现 json.Unmarshaler 接口。
// 已知 key 映射到标准字段，未知 key 存入 Extra，与 Python extra="allow" 行为一致。
func (info *BaseModelInfo) UnmarshalJSON(data []byte) error {
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		return fmt.Errorf("BaseModelInfo 反序列化失败: %w", err)
	}

	known := make(map[string]any)
	extra := make(map[string]any)
	for k, v := range raw {
		if _, ok := baseModelInfoKnownKeys[k]; ok {
			known[k] = v
		} else {
			extra[k] = v
		}
	}

	knownData, err := json.Marshal(known)
	if err != nil {
		return fmt.Errorf("BaseModelInfo 格式转换失败: %w", err)
	}

	type Alias BaseModelInfo
	var alias Alias
	if err := json.Unmarshal(knownData, &alias); err != nil {
		return fmt.Errorf("BaseModelInfo 反序列化失败: %w", err)
	}
	*info = BaseModelInfo(alias)

	if len(extra) > 0 {
		info.Extra = extra
	}

	return nil
}

// NewModelConfig 创建 ModelConfig 实例。
//
// 对应 Python: ModelConfig(model_provider=..., model_info=...)
func NewModelConfig(provider string, info BaseModelInfo) *ModelConfig {
	return &ModelConfig{
		ModelProvider: provider,
		ModelInfo:     info,
	}
}

// ──────────────────────────── 非导出函数 ────────────────────────────

func init() {
	// 初始化 BaseModelInfo 已知 JSON 键名集合
	baseModelInfoKnownKeys = map[string]struct{}{
		"api_key":        {},
		"api_base":       {},
		"model":          {},
		"temperature":    {},
		"top_p":          {},
		"stream":         {},
		"timeout":        {},
		"custom_headers": {},
	}
}

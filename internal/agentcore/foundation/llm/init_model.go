package llm

import (
	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
)

// ──────────────────────────── 结构体 ────────────────────────────

// initModelConfig InitModel 工厂函数的内部配置。
type initModelConfig struct {
	temperature   float64
	topP          float64
	maxTokens     *int
	timeout       float64
	maxRetries    int
	verifySSL     bool
	customHeaders map[string]string
}

// ──────────────────────────── 常量 ────────────────────────────

// 默认值与 Python init_model() 保持一致
const (
	defaultInitTemperature = 0.95
	defaultInitTopP        = 0.1
	defaultInitTimeout     = 60.0
	defaultInitMaxRetries  = 3
	defaultInitVerifySSL   = false
)

// ──────────────────────────── 导出函数 ────────────────────────────

// InitModelOption InitModel 工厂选项函数。
type InitModelOption func(*initModelConfig)

// InitModel 便捷创建 Model 实例的工厂函数。
//
// 对应 Python: openjiuwen/core/foundation/llm/model.py (init_model)
//
// 扁平参数 → ModelClientConfig + ModelRequestConfig → NewModel
//
// 默认值对齐 Python：
//   - temperature: 0.95
//   - top_p: 0.1
//   - timeout: 60.0
//   - max_retries: 3
//   - verify_ssl: false
//
// 注意：verify_ssl 默认 false 与 ModelClientConfig 的默认 true 不同，
// 这与 Python init_model() 的便利性设计一致（开发环境通常不需要 SSL 验证）。
func InitModel(
	provider string,
	modelName string,
	apiKey string,
	apiBase string,
	opts ...InitModelOption,
) (*Model, error) {
	cfg := &initModelConfig{
		temperature: defaultInitTemperature,
		topP:        defaultInitTopP,
		timeout:     defaultInitTimeout,
		maxRetries:  defaultInitMaxRetries,
		verifySSL:   defaultInitVerifySSL,
	}

	for _, opt := range opts {
		opt(cfg)
	}

	// 构建 ModelClientConfig
	customHeadersAny := make(map[string]any, len(cfg.customHeaders))
	for k, v := range cfg.customHeaders {
		customHeadersAny[k] = v
	}

	clientConfig := llmschema.NewModelClientConfig(
		provider,
		apiKey,
		apiBase,
		llmschema.WithTimeout(cfg.timeout),
		llmschema.WithMaxRetries(cfg.maxRetries),
		llmschema.WithVerifySSL(cfg.verifySSL),
		llmschema.WithCustomHeaders(customHeadersAny),
	)

	// 构建 ModelRequestConfig
	requestConfigOpts := []llmschema.ModelRequestConfigOption{
		llmschema.WithModelName(modelName),
		llmschema.WithTemperature(cfg.temperature),
		llmschema.WithTopP(cfg.topP),
	}
	if cfg.maxTokens != nil {
		requestConfigOpts = append(requestConfigOpts, llmschema.WithMaxTokens(*cfg.maxTokens))
	}
	requestConfig := llmschema.NewModelRequestConfig(requestConfigOpts...)

	return NewModel(clientConfig, requestConfig)
}

// WithInitTemperature 设置采样温度。
func WithInitTemperature(t float64) InitModelOption {
	return func(c *initModelConfig) { c.temperature = t }
}

// WithInitTopP 设置 top_p 采样参数。
func WithInitTopP(p float64) InitModelOption {
	return func(c *initModelConfig) { c.topP = p }
}

// WithInitMaxTokens 设置最大 token 数。
func WithInitMaxTokens(n int) InitModelOption {
	return func(c *initModelConfig) { c.maxTokens = &n }
}

// WithInitTimeout 设置请求超时（秒）。
func WithInitTimeout(t float64) InitModelOption {
	return func(c *initModelConfig) { c.timeout = t }
}

// WithInitMaxRetries 设置最大重试次数。
func WithInitMaxRetries(n int) InitModelOption {
	return func(c *initModelConfig) { c.maxRetries = n }
}

// WithInitVerifySSL 设置 SSL 验证。
func WithInitVerifySSL(v bool) InitModelOption {
	return func(c *initModelConfig) { c.verifySSL = v }
}

// WithInitCustomHeaders 设置自定义请求头。
func WithInitCustomHeaders(h map[string]string) InitModelOption {
	return func(c *initModelConfig) { c.customHeaders = h }
}

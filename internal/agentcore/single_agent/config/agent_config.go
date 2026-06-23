package config

import (
	"fmt"

	ceschema "github.com/uapclaw/uap-claw-go/internal/agentcore/context_engine/schema"
	ceiface "github.com/uapclaw/uap-claw-go/internal/agentcore/context_engine/interface"
	llmschema "github.com/uapclaw/uap-claw-go/internal/agentcore/foundation/llm/schema"
	"github.com/uapclaw/uap-claw-go/internal/agentcore/single_agent/interfaces"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ReActAgentConfig ReAct Agent 配置，聚合模型、上下文、提示词等子配置。
//
// 对应 Python: openjiuwen/core/single_agent/agents/react_agent.py (ReActAgentConfig)
type ReActAgentConfig struct {
	// MemScopeID 内存作用域标识
	MemScopeID string `json:"mem_scope_id"`
	// ModelName 模型名称
	ModelName string `json:"model_name"`
	// ModelProvider 模型提供商
	ModelProvider string `json:"model_provider"`
	// APIKey API 密钥
	APIKey string `json:"api_key"`
	// APIBase API 基础 URL
	APIBase string `json:"api_base"`
	// CustomHeaders 自定义请求头
	CustomHeaders map[string]string `json:"custom_headers,omitempty"`
	// PromptTemplateName 提示词模板名称
	PromptTemplateName string `json:"prompt_template_name"`
	// PromptTemplate 提示词模板列表
	PromptTemplate []map[string]any `json:"prompt_template,omitempty"`
	// MaxIterations ReAct 循环最大迭代次数
	MaxIterations int `json:"max_iterations"`
	// LLMReturnTokenIDs 是否请求 token IDs（RL 用）
	LLMReturnTokenIDs bool `json:"llm_return_token_ids"`
	// LLMLogprobs 是否请求 logprobs
	LLMLogprobs bool `json:"llm_logprobs"`
	// LLMTopLogprobs top_logprobs 数量 (0-20)
	LLMTopLogprobs int `json:"llm_top_logprobs"`
	// ModelClientConfig 模型客户端配置
	ModelClientConfig *llmschema.ModelClientConfig `json:"model_client_config,omitempty"`
	// ModelRequestConfig 模型请求配置（对应 Python model_config_obj）
	ModelRequestConfig *llmschema.ModelRequestConfig `json:"model_config_obj,omitempty"`
	// SysOperationID 系统操作标识
	SysOperationID string `json:"sys_operation_id,omitempty"`
	// ContextEngineConfig 上下文引擎配置
	ContextEngineConfig ceschema.ContextEngineConfig `json:"context_engine_config,omitempty"`
	// ContextProcessors 上下文处理器规格列表
	ContextProcessors []ceiface.ProcessorSpec `json:"context_processors,omitempty"`
	// Workspace 工作区实例
	// ⤵️ 回填：Workspace 接口定义后改为具体类型
	Workspace any `json:"-"`
}

// modelClientExtra WithModelClient 复合 Option 的扩展参数容器
type modelClientExtra struct {
	// verifySSL 是否验证 SSL 证书
	verifySSL bool
	// customHeaders 自定义请求头
	customHeaders map[string]string
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// 编译期接口检查：ReActAgentConfig 必须实现 AgentConfig 接口
var _ interfaces.AgentConfig = (*ReActAgentConfig)(nil)

// ──────────────────────────── 导出函数 ────────────────────────────

// ReActAgentConfigOption ReActAgentConfig 构造选项函数
type ReActAgentConfigOption func(*ReActAgentConfig)

// ModelClientExtraOption WithModelClient 复合 Option 的扩展参数函数
type ModelClientExtraOption func(*modelClientExtra)

// NewReActAgentConfig 创建 ReActAgentConfig 实例，所有字段使用默认值。
//
// 默认值：
//   - ModelProvider: "openai"
//   - MaxIterations: 5
//   - LLMTopLogprobs: 1
//   - ContextEngineConfig: ceschema.NewContextEngineConfig() 并设置 MaxContextMessageNum=200, DefaultWindowRoundNum=10
//
// 对应 Python: ReActAgentConfig()
func NewReActAgentConfig(opts ...ReActAgentConfigOption) *ReActAgentConfig {
	defaultCECfg := ceschema.NewContextEngineConfig()
	defaultCECfg.MaxContextMessageNum = 200
	defaultCECfg.DefaultWindowRoundNum = 10

	cfg := &ReActAgentConfig{
		ModelProvider:       "openai",
		MaxIterations:      5,
		LLMTopLogprobs:     1,
		ContextEngineConfig: defaultCECfg,
	}
	for _, opt := range opts {
		opt(cfg)
	}
	return cfg
}

// WithMemScopeID 设置内存作用域标识
func WithMemScopeID(id string) ReActAgentConfigOption {
	return func(c *ReActAgentConfig) { c.MemScopeID = id }
}

// WithModelName 设置模型名称
func WithModelName(name string) ReActAgentConfigOption {
	return func(c *ReActAgentConfig) { c.ModelName = name }
}

// WithModelProvider 设置模型提供商
func WithModelProvider(provider string) ReActAgentConfigOption {
	return func(c *ReActAgentConfig) { c.ModelProvider = provider }
}

// WithAPIKey 设置 API 密钥
func WithAPIKey(key string) ReActAgentConfigOption {
	return func(c *ReActAgentConfig) { c.APIKey = key }
}

// WithAPIBase 设置 API 基础 URL
func WithAPIBase(base string) ReActAgentConfigOption {
	return func(c *ReActAgentConfig) { c.APIBase = base }
}

// WithCustomHeaders 设置自定义请求头
func WithCustomHeaders(headers map[string]string) ReActAgentConfigOption {
	return func(c *ReActAgentConfig) { c.CustomHeaders = headers }
}

// WithPromptTemplateName 设置提示词模板名称
func WithPromptTemplateName(name string) ReActAgentConfigOption {
	return func(c *ReActAgentConfig) { c.PromptTemplateName = name }
}

// WithPromptTemplate 设置提示词模板列表
func WithPromptTemplate(template []map[string]any) ReActAgentConfigOption {
	return func(c *ReActAgentConfig) { c.PromptTemplate = template }
}

// WithMaxIterations 设置 ReAct 循环最大迭代次数
func WithMaxIterations(n int) ReActAgentConfigOption {
	return func(c *ReActAgentConfig) { c.MaxIterations = n }
}

// WithLLMReturnTokenIDs 设置是否请求 token IDs
func WithLLMReturnTokenIDs(b bool) ReActAgentConfigOption {
	return func(c *ReActAgentConfig) { c.LLMReturnTokenIDs = b }
}

// WithLLMLogprobs 设置是否请求 logprobs
func WithLLMLogprobs(b bool) ReActAgentConfigOption {
	return func(c *ReActAgentConfig) { c.LLMLogprobs = b }
}

// WithLLMTopLogprobs 设置 top_logprobs 数量
func WithLLMTopLogprobs(n int) ReActAgentConfigOption {
	return func(c *ReActAgentConfig) { c.LLMTopLogprobs = n }
}

// WithModelClientConfig 设置模型客户端配置
func WithModelClientConfig(cfg *llmschema.ModelClientConfig) ReActAgentConfigOption {
	return func(c *ReActAgentConfig) { c.ModelClientConfig = cfg }
}

// WithModelRequestConfig 设置模型请求配置
func WithModelRequestConfig(cfg *llmschema.ModelRequestConfig) ReActAgentConfigOption {
	return func(c *ReActAgentConfig) { c.ModelRequestConfig = cfg }
}

// WithSysOperationID 设置系统操作标识
func WithSysOperationID(id string) ReActAgentConfigOption {
	return func(c *ReActAgentConfig) { c.SysOperationID = id }
}

// WithContextEngineConfig 设置上下文引擎配置
func WithContextEngineConfig(cfg ceschema.ContextEngineConfig) ReActAgentConfigOption {
	return func(c *ReActAgentConfig) { c.ContextEngineConfig = cfg }
}

// WithContextProcessors 设置上下文处理器规格列表
func WithContextProcessors(procs []ceiface.ProcessorSpec) ReActAgentConfigOption {
	return func(c *ReActAgentConfig) { c.ContextProcessors = procs }
}

// WithWorkspace 设置工作区实例
func WithWorkspace(ws any) ReActAgentConfigOption {
	return func(c *ReActAgentConfig) { c.Workspace = ws }
}

// WithModelClient 设置模型客户端（复合 Option）。
// 同时设置 ModelProvider/APIKey/APIBase/ModelName，
// 并创建 ModelClientConfig + ModelRequestConfig。
// 对应 Python: ReActAgentConfig.configure_model_client()
func WithModelClient(provider, apiKey, apiBase, modelName string, opts ...ModelClientExtraOption) ReActAgentConfigOption {
	return func(c *ReActAgentConfig) {
		c.ModelProvider = provider
		c.APIKey = apiKey
		c.APIBase = apiBase
		c.ModelName = modelName

		// 构建扩展参数
		extra := &modelClientExtra{verifySSL: true}
		for _, opt := range opts {
			opt(extra)
		}

		// 创建 ModelClientConfig
		c.ModelClientConfig = llmschema.NewModelClientConfig(
			provider, apiKey, apiBase,
			llmschema.WithVerifySSL(extra.verifySSL),
		)
		if len(extra.customHeaders) > 0 {
			c.ModelClientConfig.CustomHeaders = extra.customHeaders
		}

		// 创建 ModelRequestConfig
		c.ModelRequestConfig = llmschema.NewModelRequestConfig(
			llmschema.WithModelName(modelName),
		)
	}
}

// WithModelProviderDetails 设置模型提供商详情（复合 Option）。
// 同时设置 ModelProvider/APIKey/APIBase，不创建子配置。
// 对应 Python: ReActAgentConfig.configure_model_provider()
func WithModelProviderDetails(provider, apiKey, apiBase string) ReActAgentConfigOption {
	return func(c *ReActAgentConfig) {
		c.ModelProvider = provider
		c.APIKey = apiKey
		c.APIBase = apiBase
	}
}

// WithContextEngine 构建并设置上下文引擎配置（复合 Option）。
// 对应 Python: ReActAgentConfig.configure_context_engine()
func WithContextEngine(maxMsgNum, windowRoundNum int, enableReload, enableKVCacheRelease bool) ReActAgentConfigOption {
	return func(c *ReActAgentConfig) {
		c.ContextEngineConfig = ceschema.ContextEngineConfig{
			MaxContextMessageNum:  maxMsgNum,
			DefaultWindowRoundNum: windowRoundNum,
			EnableReload:         enableReload,
			EnableKVCacheRelease: enableKVCacheRelease,
			ModelContextWindowTokens: make(map[string]int),
		}
	}
}

// WithCustomHeadersSync 设置自定义请求头并同步到已有 ModelClientConfig（复合 Option）。
// 对应 Python: ReActAgentConfig.configure_custom_headers()
func WithCustomHeadersSync(headers map[string]string) ReActAgentConfigOption {
	return func(c *ReActAgentConfig) {
		c.CustomHeaders = headers
		if c.ModelClientConfig != nil {
			c.ModelClientConfig.CustomHeaders = headers
		}
	}
}

// WithVerifySSL 设置是否验证 SSL 证书（ModelClientExtraOption）
func WithVerifySSL(verify bool) ModelClientExtraOption {
	return func(e *modelClientExtra) { e.verifySSL = verify }
}

// WithExtraCustomHeaders 设置扩展自定义请求头（ModelClientExtraOption）
func WithExtraCustomHeaders(headers map[string]string) ModelClientExtraOption {
	return func(e *modelClientExtra) { e.customHeaders = headers }
}

// ModelName 返回模型名称（实现 AgentConfig 接口）
func (c *ReActAgentConfig) ModelName() string {
	return c.ModelName
}

// MemScopeID 返回内存作用域标识（实现 AgentConfig 接口）
func (c *ReActAgentConfig) MemScopeID() string {
	return c.MemScopeID
}

// GetModelClientConfig 返回模型客户端配置（便捷方法，非接口方法）
func (c *ReActAgentConfig) GetModelClientConfig() *llmschema.ModelClientConfig {
	return c.ModelClientConfig
}

// GetContextEngineConfig 返回上下文引擎配置（便捷方法，非接口方法）
func (c *ReActAgentConfig) GetContextEngineConfig() *ceschema.ContextEngineConfig {
	return &c.ContextEngineConfig
}

// Validate 校验 ReActAgentConfig 的字段合法性。
//
// 校验规则：
//   - LLMTopLogprobs 范围 [0, 20]
//   - MaxIterations > 0
//   - 子配置非 nil 时递归校验
func (c *ReActAgentConfig) Validate() error {
	if c.MaxIterations <= 0 {
		return fmt.Errorf("max_iterations 必须 > 0，当前值: %d", c.MaxIterations)
	}
	if c.LLMTopLogprobs < 0 || c.LLMTopLogprobs > 20 {
		return fmt.Errorf("llm_top_logprobs 范围 [0, 20]，当前值: %d", c.LLMTopLogprobs)
	}
	if c.ModelClientConfig != nil {
		if err := c.ModelClientConfig.Validate(); err != nil {
			return fmt.Errorf("model_client_config 校验失败: %w", err)
		}
	}
	// ContextEngineConfig 递归校验
	if err := c.ContextEngineConfig.Validate(); err != nil {
		return fmt.Errorf("context_engine_config 校验失败: %w", err)
	}
	return nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

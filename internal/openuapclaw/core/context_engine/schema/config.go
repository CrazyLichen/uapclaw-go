package schema

import "fmt"

// ──────────────────────────── 结构体 ────────────────────────────

// ContextEngineConfig 上下文引擎全局配置，控制消息上限、窗口策略、KV 缓存释放、
// 卸载重载、Token 预算等核心行为。
// 配置在 ContextEngine 构造时传入，并在每次 CreateContext 创建 SessionModelContext 时被消费。
// int 字段使用 0 表示"不限"（与 Python None 语义对齐），设置时必须 > 0。
// Python: openjiuwen/core/context_engine/schema/config.py (ContextEngineConfig)
type ContextEngineConfig struct {
	// MaxContextMessageNum 上下文消息数硬上限，0 表示不限
	MaxContextMessageNum int `json:"max_context_message_num"`
	// DefaultWindowMessageNum 滑动窗口默认保留消息数，0 表示不限
	DefaultWindowMessageNum int `json:"default_window_message_num"`
	// DefaultWindowRoundNum 滑动窗口默认保留对话轮数，优先于 DefaultWindowMessageNum，0 表示不限
	DefaultWindowRoundNum int `json:"default_window_round_num"`
	// EnableKVCacheRelease 是否释放卸载消息的 KV-cache 以减少 GPU 显存压力
	EnableKVCacheRelease bool `json:"enable_kv_cache_release"`
	// EnableReload 是否启用自动重载卸载消息（通过 reload 提示符如 [[HANDLE:xxx]]）
	EnableReload bool `json:"enable_reload"`
	// EnableTiktokenCounter 是否启用 tiktoken 计数器
	EnableTiktokenCounter bool `json:"enable_tiktoken_counter"`
	// ContextWindowTokens 模型上下文窗口 Token 数（含输入输出），用于压缩遥测，0 表示不限
	ContextWindowTokens int `json:"context_window_tokens"`
	// ModelName LLM 模型名称，用于从 ModelContextWindowTokens 查找默认上下文窗口大小
	ModelName string `json:"model_name"`
	// ModelContextWindowTokens 模型名称到上下文窗口 Token 数的映射表（最佳努力 fallback），
	// 显式运行时值和 ContextWindowTokens 优先
	ModelContextWindowTokens map[string]int `json:"model_context_window_tokens"`
	// ModelContextWindowTokensOverride 当前选中模型配置声明的上下文窗口（低优先于 ContextWindowTokens）
	ModelContextWindowTokensOverride int `json:"model_context_window_tokens_override"`
	// EnableOpenRouterModelContextWindowTokens 是否从 OpenRouter 拉取模型窗口表（默认关闭）
	EnableOpenRouterModelContextWindowTokens bool `json:"enable_openrouter_model_context_window_tokens"`
	// OpenRouterRequestTimeout OpenRouter 拉取超时（秒），<=0 使用默认 3 秒
	OpenRouterRequestTimeout float64 `json:"openrouter_request_timeout"`
	// CompressionRecall 压缩归档与召回的全局配置（Python: schema/config.py:11）
	// nil 时按"未配置"处理，消费方回退默认值
	CompressionRecall *CompressionRecallConfig `json:"compression_recall_config"`
}

// CompressionRecallConfig 压缩归档与召回的全局配置，控制被压缩消息的
// 归档切分粒度（forked 压缩器的 recall 半区消费）。
// Python: openjiuwen/core/context_engine/schema/config.py:11 (CompressionRecallConfig)
type CompressionRecallConfig struct {
	// Enabled 是否启用压缩归档/召回（默认关）
	Enabled bool `json:"enabled"`
	// ChunkSizeTokens 归档切分的单块 token 上限，设置时必须 > 0
	ChunkSizeTokens int `json:"chunk_size_tokens"`
	// ChunkOverlapTokens 相邻块的 token 重叠，必须小于 ChunkSizeTokens
	ChunkOverlapTokens int `json:"chunk_overlap_tokens"`
}

// NewCompressionRecallConfig 创建压缩召回配置，字段使用 Python 默认值。
func NewCompressionRecallConfig() *CompressionRecallConfig {
	return &CompressionRecallConfig{
		Enabled:            false,
		ChunkSizeTokens:    3000,
		ChunkOverlapTokens: 300,
	}
}

// Validate 校验召回配置字段合法性。
// Python: CompressionRecallConfig._validate_chunk_tokens (config.py:18)
func (c CompressionRecallConfig) Validate() error {
	if c.ChunkSizeTokens <= 0 {
		return fmt.Errorf("chunk_size_tokens 必须 > 0，当前值: %d", c.ChunkSizeTokens)
	}
	if c.ChunkOverlapTokens >= c.ChunkSizeTokens {
		return fmt.Errorf("chunk_overlap_tokens 必须小于 chunk_size_tokens，当前值: %d >= %d", c.ChunkOverlapTokens, c.ChunkSizeTokens)
	}
	return nil
}

// ──────────────────────────── 导出函数 ────────────────────────────

// NewContextEngineConfig 创建上下文引擎配置，所有字段使用默认值。
// ModelContextWindowTokens 初始化为空 map（非 nil），避免后续使用时需要 nil 检查。
func NewContextEngineConfig() ContextEngineConfig {
	return ContextEngineConfig{
		ModelContextWindowTokens: make(map[string]int),
		CompressionRecall:        NewCompressionRecallConfig(),
	}
}

// Validate 校验配置字段合法性。
// int 字段设置时必须 > 0（0 表示不限，不校验）；
// ModelContextWindowTokens 中每个 value 必须 > 0。
func (c ContextEngineConfig) Validate() error {
	if c.MaxContextMessageNum < 0 {
		return fmt.Errorf("max_context_message_num 不能为负数，当前值: %d", c.MaxContextMessageNum)
	}
	if c.DefaultWindowMessageNum < 0 {
		return fmt.Errorf("default_window_message_num 不能为负数，当前值: %d", c.DefaultWindowMessageNum)
	}
	if c.DefaultWindowRoundNum < 0 {
		return fmt.Errorf("default_window_round_num 不能为负数，当前值: %d", c.DefaultWindowRoundNum)
	}
	if c.ContextWindowTokens < 0 {
		return fmt.Errorf("context_window_tokens 不能为负数，当前值: %d", c.ContextWindowTokens)
	}
	if c.ModelContextWindowTokensOverride < 0 {
		return fmt.Errorf("model_context_window_tokens_override 不能为负数，当前值: %d", c.ModelContextWindowTokensOverride)
	}
	if c.OpenRouterRequestTimeout < 0 {
		return fmt.Errorf("openrouter_request_timeout 不能为负数，当前值: %v", c.OpenRouterRequestTimeout)
	}
	if c.CompressionRecall != nil {
		if err := c.CompressionRecall.Validate(); err != nil {
			return fmt.Errorf("compression_recall_config: %w", err)
		}
	}
	for model, tokens := range c.ModelContextWindowTokens {
		if tokens <= 0 {
			return fmt.Errorf("model_context_window_tokens[%q] 必须 > 0，当前值: %d", model, tokens)
		}
	}
	return nil
}

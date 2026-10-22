//go:build integration

package suite

import (
	"fmt"
	"os"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm"
	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	retrievalembedding "github.com/uapclaw/uapclaw-go/internal/agentcore/retrieval/embedding"
)

// ──────────────────────────── 结构体 ────────────────────────────

// RealLLMSuite 包含真实 LLM API 调用能力的集成测试套件。
// 对齐 Python system_tests 中 @pytest.mark.skipif(not API_KEY) 模式：
// 环境变量 LLM_API_KEY 存在时使用真实 LLM，否则 Skip。
//
// 环境变量：
//   - LLM_API_KEY: API 密钥（必填，为空则 Skip）
//   - LLM_API_BASE: API 基础 URL（默认 https://ark.cn-beijing.volces.com/api/coding/v3）
//   - LLM_MODEL_NAME: 模型名称（默认 minimax-m3）
//   - LLM_MODEL_PROVIDER: 服务商标识（默认 OpenAI）
//   - EMBED_API_KEY: embedding 密钥（缺省回落 LLM_API_KEY）
//   - EMBED_API_BASE: embedding 完整端点 URL（默认 https://ark.cn-beijing.volces.com/api/v3/embeddings）
//   - EMBED_MODEL_NAME: embedding 模型名（默认 Doubao-Embedding-Vision）
//
// 运行方式: go test -tags="sqlite_fts5 test integration llm" ./tests/integration/real_llm/...
type RealLLMSuite struct {
	RunnerSuite
	// RealClientConfig 真实 LLM 的客户端配置
	RealClientConfig *llmschema.ModelClientConfig
	// RealModelConfig 真实 LLM 的模型请求配置
	RealModelConfig *llmschema.ModelRequestConfig
	// RealEmbeddingConfig 真实 embedding 的配置（EMBED_API_KEY 或 LLM_API_KEY 存在时非 nil）
	RealEmbeddingConfig *retrievalembedding.EmbeddingConfig
}

// ──────────────────────────── 导出函数 ────────────────────────────

// SetupSuite 初始化真实 LLM 测试环境。
// 读取环境变量构建真实 LLM 配置，同时保留 MockLLM 作为兜底。
//
// embedding 配置对齐 Python test_memory_rail_e2e.py 的 fixture 模式：
// EMBED_API_KEY/EMBED_API_BASE 独立设置，缺省时回落到 LLM_API_KEY/LLM_API_BASE
// （复用同一账号 key），模型默认 Doubao-Embedding-Vision。
func (s *RealLLMSuite) SetupSuite() {
	s.RunnerSuite.SetupSuite()

	apiKey := os.Getenv("LLM_API_KEY")
	apiBase := envWithDefault("LLM_API_BASE", "https://ark.cn-beijing.volces.com/api/coding/v3")
	modelName := envWithDefault("LLM_MODEL_NAME", "minimax-m3")
	provider := envWithDefault("LLM_MODEL_PROVIDER", "OpenAI")

	if apiKey != "" {
		s.RealClientConfig = &llmschema.ModelClientConfig{
			ClientProvider: provider,
			ClientID:       "real_llm_test",
			APIKey:         apiKey,
			APIBase:        apiBase,
			VerifySSL:      false,
			Timeout:        120,
		}
		s.RealModelConfig = &llmschema.ModelRequestConfig{
			ModelName: modelName,
		}
	}

	// embedding：key 缺省回落 LLM_API_KEY（用户决策：复用现有 key）。
	// 注意 EMBED_API_BASE 是完整端点 URL——Go 的 APIEmbedding 直接 POST 到
	// BaseURL 原样（不同于 Python lite 路径会自动拼 /embeddings），
	// 因此默认值必须带 /api/v3/embeddings 完整路径。
	embedAPIKey := envWithDefault("EMBED_API_KEY", apiKey)
	embedAPIBase := envWithDefault(
		"EMBED_API_BASE",
		"https://ark.cn-beijing.volces.com/api/v3/embeddings",
	)
	embedModelName := envWithDefault("EMBED_MODEL_NAME", "Doubao-Embedding-Vision")

	if embedAPIKey != "" {
		s.RealEmbeddingConfig = &retrievalembedding.EmbeddingConfig{
			ModelName: embedModelName,
			BaseURL:   embedAPIBase,
			APIKey:    embedAPIKey,
		}
	}
}

// IsRealLLMAvailable 检查真实 LLM 是否可用（环境变量已设置）。
func (s *RealLLMSuite) IsRealLLMAvailable() bool {
	return s.RealClientConfig != nil
}

// SkipIfNoRealLLM 在真实 LLM 不可用时跳过测试。
// 每个测试函数开头调用：s.SkipIfNoRealLLM()
func (s *RealLLMSuite) SkipIfNoRealLLM() {
	if !s.IsRealLLMAvailable() {
		s.T().Skip("需要设置 LLM_API_KEY 环境变量，跳过真实 LLM 测试")
	}
}

// NewRealModel 创建真实 LLM 的 Model 实例。
// 调用前应先调用 SkipIfNoRealLLM() 确保环境变量已设置。
// 对齐 Python: create_model() → Model(model_client_config=..., model_config=...)
func (s *RealLLMSuite) NewRealModel() (*llm.Model, error) {
	if s.RealClientConfig == nil {
		return nil, fmt.Errorf("真实 LLM 配置不可用，请设置 LLM_API_KEY 环境变量")
	}
	return llm.NewModel(s.RealClientConfig, s.RealModelConfig)
}

// IsEmbeddingAvailable 检查真实 embedding 是否可用（EMBED_API_KEY 或 LLM_API_KEY 已设置）。
func (s *RealLLMSuite) IsEmbeddingAvailable() bool {
	return s.RealEmbeddingConfig != nil
}

// SkipIfNoEmbedding 在真实 embedding 不可用时跳过测试。
// 每个依赖 embedding 的测试函数开头调用：s.SkipIfNoEmbedding()
// 对齐 Python: test_memory_rail_e2e.py 的 _require_api_config 三重检查
// （LLM_API_KEY/LLM_API_BASE/EMBED_API_KEY），Go 侧 EMBED_API_KEY 缺省
// 回落 LLM_API_KEY，故只需检查最终配置是否就绪。
func (s *RealLLMSuite) SkipIfNoEmbedding() {
	if !s.IsEmbeddingAvailable() {
		s.T().Skip("需要设置 EMBED_API_KEY 或 LLM_API_KEY 环境变量，跳过真实 embedding 测试")
	}
}

// NewRealEmbeddingConfig 返回真实 embedding 配置。
// 调用前应先调用 SkipIfNoEmbedding() 确保配置已就绪。
// 对齐 Python: _create_embedding_config() → EmbeddingConfig(model_name=, base_url=, api_key=)
func (s *RealLLMSuite) NewRealEmbeddingConfig() *retrievalembedding.EmbeddingConfig {
	return s.RealEmbeddingConfig
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// envWithDefault 读取环境变量，为空时返回默认值。
func envWithDefault(key, defaultVal string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return defaultVal
}

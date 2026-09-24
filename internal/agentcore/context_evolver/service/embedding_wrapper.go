package service

import (
	"context"
	"fmt"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/store/embedding"
	retrievalembedding "github.com/uapclaw/uapclaw-go/internal/agentcore/retrieval/embedding"
	cecontext "github.com/uapclaw/uapclaw-go/internal/agentcore/context_evolver/core/context"
	"github.com/uapclaw/uapclaw-go/internal/common/exception"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// OpenAIEmbeddingWrapper 包装 OpenAIEmbedding 提供 EmbeddingService 接口。
// 对齐 Python OpenAIEmbeddingWrapper。
//
// 设计决策：
//   - 内部用 Go 已有的 OpenAIEmbedding 构造，复用 HTTP 客户端和 API 调用逻辑
//   - Embed → EmbedQuery，EmbedBatch → EmbedDocuments 的方法名映射
//
// Python: openjiuwen/extensions/context_evolver/service/task_memory_service.py (OpenAIEmbeddingWrapper)
type OpenAIEmbeddingWrapper struct {
	// modelName 模型名称
	modelName string
	// client 底层 embedding 客户端
	client embedding.BaseEmbedding
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// 编译期接口断言：OpenAIEmbeddingWrapper 实现 cecontext.EmbeddingService
var _ cecontext.EmbeddingService = (*OpenAIEmbeddingWrapper)(nil)

// ──────────────────────────── 导出函数 ────────────────────────────

// NewOpenAIEmbeddingWrapper 创建 OpenAI Embedding 适配器。
// 对齐 Python OpenAIEmbeddingWrapper.__init__(model_name, api_key, base_url)。
// 内部用 Go 已有的 OpenAIEmbedding 构造，复用 HTTP 客户端和 API 调用逻辑。
func NewOpenAIEmbeddingWrapper(modelName string, apiKey string, baseURL string) (*OpenAIEmbeddingWrapper, error) {
	// 对齐 Python：api_key = api_key or config.get("API_KEY")
	if apiKey == "" {
		return nil, exception.NewBaseError(
			exception.StatusToolchainEvolvingMemoryConfigInvalid,
			exception.WithMsg("API key not provided and API_KEY not set in config"),
		)
	}

	// 对齐 Python：base_url = base_url or "https://api.openai.com/v1"
	if baseURL == "" {
		baseURL = "https://api.openai.com/v1"
	}

	// 对齐 Python：embedding_config = EmbeddingConfig(model_name, base_url, api_key)
	embConfig := retrievalembedding.EmbeddingConfig{
		ModelName: modelName,
		BaseURL:   baseURL,
		APIKey:    apiKey,
	}

	// 对齐 Python：self.client = CoreOpenAIEmbedding(config=embedding_config, verify=False)
	client := retrievalembedding.NewOpenAIEmbedding(embConfig)

	// 对齐 Python：logger.info("Initialized OpenAI Embedding with model: %s", model_name)
	logger.Info(logComponent).
		Str("model_name", modelName).
		Msg("Initialized OpenAI Embedding")

	return &OpenAIEmbeddingWrapper{
		modelName: modelName,
		client:    client,
	}, nil
}

// NewOpenAIEmbeddingWrapperWithClient 使用已有 BaseEmbedding 创建适配器。
// 方便测试时注入 mock。
func NewOpenAIEmbeddingWrapperWithClient(modelName string, client embedding.BaseEmbedding) *OpenAIEmbeddingWrapper {
	return &OpenAIEmbeddingWrapper{
		modelName: modelName,
		client:    client,
	}
}

// Embed 生成单文本的向量嵌入。
// 对齐 Python OpenAIEmbeddingWrapper.async_embed(text)。
// 实现 cecontext.EmbeddingService 接口。
func (w *OpenAIEmbeddingWrapper) Embed(ctx context.Context, text string) ([]float64, error) {
	// 对齐 Python：embedding = await self.client.embed_query(text)
	vec, err := w.client.EmbedQuery(ctx, text)
	if err != nil {
		// 对齐 Python：except ToolchainError: raise
		if _, ok := err.(*exception.BaseError); ok {
			return nil, err
		}
		// 对齐 Python：except Exception as e: logger.error("Embedding generation failed: %s", e)
		logger.Error(logComponent).Err(err).Msg("Embedding generation failed")
		return nil, exception.NewBaseError(
			exception.StatusToolchainEvolvingMemoryEmbeddingExecutionError,
			exception.WithMsg(err.Error()),
		)
	}

	// 对齐 Python：logger.debug("Generated embedding of dimension %s", len(embedding))
	logger.Debug(logComponent).
		Int("dimension", len(vec)).
		Msg("Generated embedding")

	return vec, nil
}

// EmbedBatch 批量生成文本的向量嵌入。
// 对齐 Python OpenAIEmbeddingWrapper.async_embed_batch(texts)。
// 实现 cecontext.EmbeddingService 接口。
func (w *OpenAIEmbeddingWrapper) EmbedBatch(ctx context.Context, texts []string) ([][]float64, error) {
	// 对齐 Python：if not texts: return []
	if len(texts) == 0 {
		return [][]float64{}, nil
	}

	// 对齐 Python：embeddings = await self.client.embed_documents(texts)
	vecs, err := w.client.EmbedDocuments(ctx, texts)
	if err != nil {
		if _, ok := err.(*exception.BaseError); ok {
			return nil, err
		}
		// 对齐 Python：logger.error("Batch embedding generation failed: %s", e)
		logger.Error(logComponent).Err(err).Msg("Batch embedding generation failed")
		return nil, exception.NewBaseError(
			exception.StatusToolchainEvolvingMemoryEmbeddingExecutionError,
			exception.WithMsg(err.Error()),
		)
	}

	// 对齐 Python：logger.debug("Generated %s embeddings", len(embeddings))
	logger.Debug(logComponent).
		Int("count", len(vecs)).
		Msg("Generated embeddings")

	return vecs, nil
}

// String 实现 Stringer 接口。
// 对齐 Python OpenAIEmbeddingWrapper.__repr__()。
func (w *OpenAIEmbeddingWrapper) String() string {
	return fmt.Sprintf("OpenAIEmbeddingWrapper(model=%s)", w.modelName)
}

// ──────────────────────────── 非导出函数 ────────────────────────────

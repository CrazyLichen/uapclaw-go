package embedding

import (
	"context"
	"fmt"

	"github.com/openai/openai-go"
	"github.com/openai/openai-go/option"
	"github.com/openai/openai-go/packages/param"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/store/embedding"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/retrieval/common"
	"github.com/uapclaw/uapclaw-go/internal/common/exception"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// VLLMEmbedding vLLM 向量嵌入客户端。
//
// 组合 OpenAIEmbedding 实例，添加多模态指令注入。
// vLLM 兼容 OpenAI API 格式，但多模态嵌入需要通过 extra_body.messages 传入内容。
//
// 对应 Python: openjiuwen/core/retrieval/embedding/vllm_embedding.py
type VLLMEmbedding struct {
	// openAI 委托的 OpenAI 客户端实例
	openAI *OpenAIEmbedding
}

// ──────────────────────────── 常量 ────────────────────────────
const (
	// defaultInstruction 默认多模态嵌入指令
	defaultInstruction = "Represent the user's input."
)

// ──────────────────────────── 导出函数 ────────────────────────────

// NewVLLMEmbedding 创建 vLLM 向量嵌入客户端。
//
// 接收已配置的 OpenAIEmbedding 实例（baseURL 应指向 vLLM 服务地址）。
func NewVLLMEmbedding(openAI *OpenAIEmbedding) *VLLMEmbedding {
	return &VLLMEmbedding{openAI: openAI}
}

// EmbedQuery 将单条查询文本转换为向量，委托给 OpenAIEmbedding。
func (v *VLLMEmbedding) EmbedQuery(ctx context.Context, text string, opts ...embedding.EmbedOption) ([]float64, error) {
	return v.openAI.EmbedQuery(ctx, text, opts...)
}

// EmbedDocuments 将多条文档文本批量转换为向量，委托给 OpenAIEmbedding。
func (v *VLLMEmbedding) EmbedDocuments(ctx context.Context, texts []string, opts ...embedding.EmbedOption) ([][]float64, error) {
	return v.openAI.EmbedDocuments(ctx, texts, opts...)
}

// EmbedMultimodal 将多模态文档转换为向量。
//
// 注入 instruction → 构造 messages → 委托 OpenAI SDK 调用。
// 对应 Python: VLLMEmbedding.embed_multimodal
func (v *VLLMEmbedding) EmbedMultimodal(ctx context.Context, doc *common.MultimodalDocument, opts ...MultimodalOption) ([]float64, error) {
	if doc == nil {
		return nil, exception.BuildError(
			exception.StatusRetrievalEmbeddingInputInvalid,
			exception.WithParam("error_msg", "多模态文档为 nil"),
		)
	}

	// 获取指令
	instruction := defaultInstruction
	for _, opt := range opts {
		if opt.Instruction != "" {
			instruction = opt.Instruction
		}
	}

	// 构造 messages
	content := doc.Content()
	messages := []map[string]any{
		{
			"role": "system",
			"content": []map[string]any{
				{"type": "text", "text": instruction},
			},
		},
		{
			"role":    "user",
			"content": content,
		},
	}

	// 通过 extra_body 传入 messages，input 设为 nil
	// vLLM 多模态模式下 input 由 messages 提供
	return v.callWithMessages(ctx, messages)
}

// Dimension 返回嵌入向量维度，委托给 OpenAIEmbedding。
func (v *VLLMEmbedding) Dimension() int {
	return v.openAI.Dimension()
}

// DimensionWithContext 返回嵌入向量维度，支持 context 取消，委托给 OpenAIEmbedding。
// 对齐 T-04 修复。
func (v *VLLMEmbedding) DimensionWithContext(ctx context.Context) (int, error) {
	return v.openAI.DimensionWithContext(ctx)
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// callWithMessages 使用 extra_body.messages 调用 vLLM 多模态嵌入 API。
func (v *VLLMEmbedding) callWithMessages(ctx context.Context, messages []map[string]any) ([]float64, error) {
	return retryVLLMWithBackoff(ctx, v.openAI.maxRetries, func(attempt int) ([]float64, error) {
		params := openai.EmbeddingNewParams{
			Model: v.openAI.config.ModelName,
			Input: openai.EmbeddingNewParamsInputUnion{
				OfString: param.Null[string](), // 对齐 Python input=None，vLLM 多模态模式下 input 由 messages 提供
			},
		}

		// Matryoshka 维度截断
		if v.openAI.matryoshkaDimension && v.openAI.dimension > 0 {
			params.Dimensions = param.Opt[int64]{Value: int64(v.openAI.dimension)}
		}

		// 通过 extra_body 传入 messages
		resp, err := v.openAI.client.Embeddings.New(ctx, params,
			option.WithJSONSet("messages", messages),
		)
		if err != nil {
			logger.Warn(logComponent).
				Str("event_type", "embedding_request_failed").
				Str("model_provider", "vllm").
				Int("attempt", attempt+1).
				Int("max_retries", v.openAI.maxRetries).
				Err(err).
				Msg("vLLM 嵌入请求失败")
			return nil, err
		}

		if len(resp.Data) == 0 {
			return nil, exception.BuildError(
				exception.StatusRetrievalEmbeddingResponseInvalid,
				exception.WithParam("error_msg", "vLLM 响应中无嵌入数据"),
			)
		}

		// 按 index 排序后取第一个
		embeddings := make([][]float64, len(resp.Data))
		for _, item := range resp.Data {
			if int(item.Index) < len(embeddings) {
				embeddings[item.Index] = item.Embedding
			}
		}

		if len(embeddings) == 0 || len(embeddings[0]) == 0 {
			return nil, exception.BuildError(
				exception.StatusRetrievalEmbeddingResponseInvalid,
				exception.WithParam("error_msg", "vLLM 响应中无有效嵌入向量"),
			)
		}

		return embeddings[0], nil
	})
}

// retryVLLMWithBackoff VLLM 专用重试 + 指数退避（返回单条向量）。
// 对齐 Python: 只重试可恢复错误（5xx/网络错误/429），不重试客户端错误（4xx）。
func retryVLLMWithBackoff(ctx context.Context, maxRetries int, fn func(attempt int) ([]float64, error)) ([]float64, error) {
	var lastErr error
	for attempt := 0; attempt < maxRetries; attempt++ {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}

		result, err := fn(attempt)
		if err == nil {
			return result, nil
		}
		lastErr = err

		// 检查是否可重试：4xx 客户端错误不可重试（429 除外），5xx 和网络错误可重试
		if !vllmIsRetryable(err) {
			return nil, err
		}

		if attempt < maxRetries-1 {
			logger.Warn(logComponent).
				Str("event_type", "embedding_retry").
				Int("attempt", attempt+1).
				Int("max_retries", maxRetries).
				Err(err).
				Msg("vLLM 嵌入请求失败，准备重试")
		}
	}

	return nil, exception.BuildError(
		exception.StatusRetrievalEmbeddingRequestCallFailed,
		exception.WithParam("error_msg", fmt.Sprintf("vLLM 重试 %d 次后仍失败: %s", maxRetries, lastErr)),
		exception.WithCause(lastErr),
	)
}

// vllmIsRetryable 判断 VLLM 错误是否可重试。
// 4xx 客户端错误不可重试（429 Rate Limit 除外），5xx 服务端错误和网络错误可重试。
func vllmIsRetryable(err error) bool {
	if baseErr, ok := err.(*exception.BaseError); ok && !baseErr.IsRecoverable() {
		return false
	}
	return true
}

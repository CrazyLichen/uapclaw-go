package rb

import (
	"context"
	"fmt"

	cecontext "github.com/uapclaw/uapclaw-go/internal/agentcore/context_evolver/core/context"
	op "github.com/uapclaw/uapclaw-go/internal/agentcore/context_evolver/core/op"
	ceschema "github.com/uapclaw/uapclaw-go/internal/agentcore/context_evolver/schema"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// RBRecallMemoryOp ReasoningBank 记忆检索操作。
// 从向量库中检索与 query 相似的推理策略记忆。
// 对齐 Python RecallMemoryOp (retrieve/task/reasoning_bank/run.py)。
type RBRecallMemoryOp struct {
	op.OpBase
	// topK 检索返回数量，默认 1
	topK int
}

// ──────────────────────────── 导出函数 ────────────────────────────

// NewRBRecallMemoryOp 创建 ReasoningBank 记忆检索操作。
// 对齐 Python RecallMemoryOp(service_context, top_k=1)。
func NewRBRecallMemoryOp(sc *cecontext.ServiceContext, topK int) *RBRecallMemoryOp {
	if topK <= 0 {
		topK = 1
	}
	return &RBRecallMemoryOp{
		OpBase: *op.NewOpBase(sc),
		topK:   topK,
	}
}

// Execute 执行 ReasoningBank 记忆检索。
// 对齐 Python RecallMemoryOp.async_execute()：
// 从 RuntimeContext 获取 query + user_id → Embed → Search → 转为 []ReasoningBankRetrievedMemory。
func (o *RBRecallMemoryOp) Execute(ctx context.Context, rc *cecontext.RuntimeContext) error {
	embeddingModel := o.EmbeddingModel()
	if embeddingModel == nil {
		return fmt.Errorf("embedding model not configured in ServiceContext")
	}

	vectorStore := o.VectorStore()
	if vectorStore == nil {
		return fmt.Errorf("vector store not configured in ServiceContext")
	}

	// 从 RuntimeContext 获取 query 和 user_id
	query, _ := cecontext.GetTyped[string](rc, "query")
	userID, _ := cecontext.GetTyped[string](rc, "user_id")
	if userID == "" {
		userID = "default"
	}

	// 生成 query 的 embedding
	embedding, err := embeddingModel.Embed(ctx, query)
	if err != nil {
		logger.Error(logComponent).
			Err(err).
			Str("query", query).
			Msg("生成 query embedding 失败")
		return fmt.Errorf("embedding query failed: %w", err)
	}

	logger.Debug(logComponent).
		Int("embedding_len", len(embedding)).
		Str("query", query).
		Msg("生成 query embedding 成功")

	// 构建元数据过滤条件，对齐 Python: metadata_filter={"workspace_id": user_id, "type": "reasoning_bank_memory"}
	metadataFilter := map[string]any{
		"type":         "reasoning_bank_memory",
		"workspace_id": userID,
	}

	// 向量相似度搜索
	nodes, err := vectorStore.Search(ctx, embedding, o.topK, metadataFilter)
	if err != nil {
		logger.Error(logComponent).
			Err(err).
			Str("query", query).
			Msg("向量搜索失败")
		return fmt.Errorf("vector search failed: %w", err)
	}

	logger.Debug(logComponent).
		Int("result_count", len(nodes)).
		Str("query", query).
		Msg("向量搜索完成")

	// 转换为 []ReasoningBankRetrievedMemory
	// 对齐 Python: 遍历 VectorNode → NewReasoningBankMemoryFromVectorNode → 取 Memory 字段 → 逐条构建 ReasoningBankRetrievedMemory
	retrieved := make([]ceschema.ReasoningBankRetrievedMemory, 0)
	for _, node := range nodes {
		rbMemory := ceschema.NewReasoningBankMemoryFromVectorNode(node)
		if rbMemory == nil || len(rbMemory.Memory) == 0 {
			logger.Warn(logComponent).
				Str("node_id", node.ID).
				Msg("向量节点转换为 ReasoningBankMemory 失败或 Memory 为空，跳过")
			continue
		}
		for _, item := range rbMemory.Memory {
			retrieved = append(retrieved, ceschema.ReasoningBankRetrievedMemory(item))
		}
	}

	// 写入 RuntimeContext
	rc.Set("retrieved_memories", retrieved)

	logger.Info(logComponent).
		Str("query", query).
		Int("retrieved_count", len(retrieved)).
		Msg("ReasoningBank 记忆检索完成")

	return nil
}

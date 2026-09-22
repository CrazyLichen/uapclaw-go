package reme

import (
	"context"
	"fmt"
	"strings"

	cecontext "github.com/uapclaw/uapclaw-go/internal/agentcore/context_evolver/core/context"
	op "github.com/uapclaw/uapclaw-go/internal/agentcore/context_evolver/core/op"
	ceschema "github.com/uapclaw/uapclaw-go/internal/agentcore/context_evolver/schema"
	logger "github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// RecallMemoryOp 向量检索操作。
// 对齐 Python RecallMemoryOp：
// 从 RuntimeContext 获取 query + user_id → Embed → Search → 转为 []ReMeRetrievedMemory。
//
// Python: openjiuwen/extensions/context_evolver/retrieve/task/reme/run.py
type RecallMemoryOp struct {
	op.OpBase
	// topK 初始检索数量
	topK int
}

// RerankMemoryOp LLM 重排序操作。
// 对齐 Python RerankMemoryOp：
// llmRerank=false 时跳过，否则用 LLM 重排候选项。
//
// Python: openjiuwen/extensions/context_evolver/retrieve/task/reme/run.py
type RerankMemoryOp struct {
	op.OpBase
	// llmRerank 是否启用 LLM 重排序
	llmRerank bool
	// topKRerank 重排序后保留数量
	topKRerank int
	// prompts 提示词配置
	prompts ReMeRetrievePrompts
}

// RewriteMemoryOp LLM 改写操作。
// 对齐 Python RewriteMemoryOp：
// llmRewrite=false 时用格式化原文，否则用 LLM 改写。
//
// Python: openjiuwen/extensions/context_evolver/retrieve/task/reme/run.py
type RewriteMemoryOp struct {
	op.OpBase
	// llmRewrite 是否启用 LLM 改写
	llmRewrite bool
	// prompts 提示词配置
	prompts ReMeRetrievePrompts
}

// ──────────────────────────── 导出函数 ────────────────────────────

// NewRecallMemoryOp 创建向量检索操作。
// 对齐 Python RecallMemoryOp(service_context, top_k)。
func NewRecallMemoryOp(sc *cecontext.ServiceContext, topK int) *RecallMemoryOp {
	return &RecallMemoryOp{
		OpBase: *op.NewOpBase(sc),
		topK:   topK,
	}
}

// NewRerankMemoryOp 创建 LLM 重排序操作。
// 对齐 Python RerankMemoryOp(service_context, llm_rerank, topk_rerank)。
func NewRerankMemoryOp(sc *cecontext.ServiceContext, llmRerank bool, topKRerank int) *RerankMemoryOp {
	return &RerankMemoryOp{
		OpBase:      *op.NewOpBase(sc),
		llmRerank:   llmRerank,
		topKRerank:  topKRerank,
		prompts:     ReMeRetrieveDefaultPrompts,
	}
}

// NewRewriteMemoryOp 创建 LLM 改写操作。
// 对齐 Python RewriteMemoryOp(service_context, llm_rewrite)。
func NewRewriteMemoryOp(sc *cecontext.ServiceContext, llmRewrite bool) *RewriteMemoryOp {
	return &RewriteMemoryOp{
		OpBase:      *op.NewOpBase(sc),
		llmRewrite:  llmRewrite,
		prompts:     ReMeRetrieveDefaultPrompts,
	}
}

// ──────────────────────────── 导出方法 ────────────────────────────

// Execute 执行向量检索。
// 对齐 Python RecallMemoryOp.__call__。
func (o *RecallMemoryOp) Execute(ctx context.Context, rc *cecontext.RuntimeContext) error {
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

	// 生成 query 的 embedding
	embedding, err := embeddingModel.Embed(ctx, query)
	if err != nil {
		logger.Error(logComponent).
			Err(err).
			Str("query", query).
			Msg("生成 query embedding 失败")
		return fmt.Errorf("embedding query failed: %w", err)
	}

	// 构建元数据过滤条件，对齐 Python: metadata_filter={"workspace_id": user_id, "type": "reme_memory"}
	metadataFilter := map[string]any{"type": "reme_memory"}
	if userID != "" {
		metadataFilter["workspace_id"] = userID
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

	// 转换为 []ReMeRetrievedMemory
	retrieved := make([]ceschema.ReMeRetrievedMemory, 0, len(nodes))
	for _, node := range nodes {
		whenToUse, _ := node.Metadata["when_to_use"].(string)
		content, _ := node.Metadata["content"].(string)
		retrieved = append(retrieved, ceschema.ReMeRetrievedMemory{
			WhenToUse: whenToUse,
			Content:   content,
		})
	}

	// 写入 RuntimeContext
	rc.Set("retrieved_memories", retrieved)

	logger.Info(logComponent).
		Str("query", query).
		Int("retrieved_count", len(retrieved)).
		Msg("向量检索完成")

	return nil
}

// Execute 执行 LLM 重排序。
// 对齐 Python RerankMemoryOp.__call__。
func (o *RerankMemoryOp) Execute(ctx context.Context, rc *cecontext.RuntimeContext) error {
	// llmRerank=false 时跳过重排序
	if !o.llmRerank {
		return nil
	}

	// 从 RuntimeContext 获取 query 和 retrieved_memories
	query, _ := cecontext.GetTyped[string](rc, "query")
	retrieved, ok := cecontext.GetTyped[[]ceschema.ReMeRetrievedMemory](rc, "retrieved_memories")
	if !ok || len(retrieved) == 0 {
		// 无记忆，跳过
		return nil
	}

	llm := o.LLM()
	if llm == nil {
		return fmt.Errorf("LLM not configured in ServiceContext")
	}

	// 格式化候选项，对齐 Python _format_candidates_for_rerank
	candidates := formatCandidatesForRerank(retrieved)

	// 构建提示词，使用 strings.ReplaceAll 链式替换占位符
	prompt := o.prompts.RerankPrompt
	prompt = strings.ReplaceAll(prompt, "{query}", query)
	prompt = strings.ReplaceAll(prompt, "{num_candidates}", fmt.Sprintf("%d", len(retrieved)))
	prompt = strings.ReplaceAll(prompt, "{candidates}", candidates)

	// 调用 LLM
	response, err := llm.Generate(ctx, prompt)
	if err != nil {
		logger.Error(logComponent).
			Err(err).
			Msg("LLM 重排序调用失败")
		return fmt.Errorf("LLM rerank failed: %w", err)
	}

	// 解析排序索引
	indices := ParseJSONListResponse(response, "ranked_indices")
	if len(indices) == 0 {
		logger.Warn(logComponent).
			Msg("LLM 重排序解析失败，保留原顺序")
		// 解析失败时保留原顺序
		return nil
	}

	// 按索引重排
	reranked := make([]ceschema.ReMeRetrievedMemory, 0, len(retrieved))
	for _, idx := range indices {
		if idx >= 0 && idx < len(retrieved) {
			reranked = append(reranked, retrieved[idx])
		}
	}
	// 补充未在索引中出现的候选项
	indexSet := make(map[int]bool, len(indices))
	for _, idx := range indices {
		indexSet[idx] = true
	}
	for i, mem := range retrieved {
		if !indexSet[i] {
			reranked = append(reranked, mem)
		}
	}

	// 截断到 topKRerank
	if o.topKRerank > 0 && len(reranked) > o.topKRerank {
		reranked = reranked[:o.topKRerank]
	}

	// 写回 RuntimeContext
	rc.Set("retrieved_memories", reranked)

	logger.Info(logComponent).
		Int("original_count", len(retrieved)).
		Int("reranked_count", len(reranked)).
		Msg("LLM 重排序完成")

	return nil
}

// Execute 执行 LLM 改写。
// 对齐 Python RewriteMemoryOp.__call__。
func (o *RewriteMemoryOp) Execute(ctx context.Context, rc *cecontext.RuntimeContext) error {
	// 从 RuntimeContext 获取 query 和 retrieved_memories
	query, _ := cecontext.GetTyped[string](rc, "query")
	retrieved, ok := cecontext.GetTyped[[]ceschema.ReMeRetrievedMemory](rc, "retrieved_memories")

	// 无记忆时设置空字符串
	if !ok || len(retrieved) == 0 {
		rc.Set("memory_string", "")
		return nil
	}

	// llmRewrite=false 时用格式化原文
	if !o.llmRewrite {
		rc.Set("memory_string", formatMemoriesForContext(retrieved))
		return nil
	}

	llm := o.LLM()
	if llm == nil {
		return fmt.Errorf("LLM not configured in ServiceContext")
	}

	// 格式化记忆原文
	originalContext := formatMemoriesForContext(retrieved)

	// 构建提示词，使用 strings.ReplaceAll 链式替换占位符
	prompt := o.prompts.RewritePrompt
	prompt = strings.ReplaceAll(prompt, "{current_query}", query)
	prompt = strings.ReplaceAll(prompt, "{original_context}", originalContext)

	// 调用 LLM
	response, err := llm.Generate(ctx, prompt)
	if err != nil {
		logger.Error(logComponent).
			Err(err).
			Msg("LLM 改写调用失败")
		return fmt.Errorf("LLM rewrite failed: %w", err)
	}

	// 解析改写结果
	rewritten := ParseJSONField(response, "rewritten_context")
	if rewritten == "" {
		logger.Warn(logComponent).
			Msg("LLM 改写解析失败，降级为格式化原文")
		rc.Set("memory_string", originalContext)
		return nil
	}

	// 写入 RuntimeContext
	rc.Set("memory_string", rewritten)

	logger.Info(logComponent).
		Int("memory_count", len(retrieved)).
		Msg("LLM 改写完成")

	return nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// formatCandidatesForRerank 格式化候选项供重排序提示词使用。
// 对齐 Python RerankMemoryOp._format_candidates_for_rerank：
// f"Candidate {i}:\nCondition: {condition}\nExperience: {content}\n"
// 候选项之间用 "\n---\n" 连接。
func formatCandidatesForRerank(candidates []ceschema.ReMeRetrievedMemory) string {
	formatted := make([]string, 0, len(candidates))
	for i, mem := range candidates {
		text := fmt.Sprintf("Candidate %d:\nCondition: %s\nExperience: %s\n", i, mem.WhenToUse, mem.Content)
		formatted = append(formatted, text)
	}
	return strings.Join(formatted, "\n---\n")
}

// formatMemoriesForContext 格式化记忆供上下文使用。
// 对齐 Python RewriteMemoryOp._format_memories_for_context：
// f"Memory {i}:\n  When to use: {condition}\n  Content: {memory_content}\n"
// 记忆之间用 "\n" 连接。
func formatMemoriesForContext(memories []ceschema.ReMeRetrievedMemory) string {
	formatted := make([]string, 0, len(memories))
	for i, mem := range memories {
		text := fmt.Sprintf("Memory %d:\n  When to use: %s\n  Content: %s\n", i+1, mem.WhenToUse, mem.Content)
		formatted = append(formatted, text)
	}
	return strings.Join(formatted, "\n")
}

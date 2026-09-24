package rb

import (
	"bytes"
	"context"
	"fmt"
	"text/template"

	cecontext "github.com/uapclaw/uapclaw-go/internal/agentcore/context_evolver/core/context"
	op "github.com/uapclaw/uapclaw-go/internal/agentcore/context_evolver/core/op"
	cepersistence "github.com/uapclaw/uapclaw-go/internal/agentcore/context_evolver/core/persistence"
	ceschema "github.com/uapclaw/uapclaw-go/internal/agentcore/context_evolver/schema"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// SummarizeMemoryOp 单轨迹推理策略提取操作（matts="none"/"sequential"）。
// 对齐 Python SummarizeMemoryOp。
type SummarizeMemoryOp struct {
	op.OpBase
	// parser 记忆项解析器
	parser *MemoryItemParser
	// prompts 提示词配置
	prompts *ReasoningBankSummaryPrompt
}

// SummarizeMemoryParallelOp 多轨迹推理策略提取操作（matts="parallel"/"combined"）。
// 对齐 Python SummarizeMemoryParallelOp。
type SummarizeMemoryParallelOp struct {
	op.OpBase
	// parser 记忆项解析器
	parser *MemoryItemParser
	// prompts 提示词配置
	prompts *ReasoningBankSummaryPrompt
}

// UpdateVectorStoreOp 记忆向量存储更新操作。
// 对齐 Python UpdateVectorStoreOp。
type UpdateVectorStoreOp struct {
	op.OpBase
}

// PersistMemoryOp 记忆持久化操作。
// algoName 固定为 "rb"。
// 对齐 Python PersistMemoryOp。
type PersistMemoryOp struct {
	op.OpBase
	// helper 持久化助手
	helper *cepersistence.MemoryPersistenceHelper
}

// ──────────────────────────── 导出函数 ────────────────────────────

// NewSummarizeMemoryOp 创建单轨迹推理策略提取操作。
func NewSummarizeMemoryOp(sc *cecontext.ServiceContext) *SummarizeMemoryOp {
	return &SummarizeMemoryOp{
		OpBase:  *op.NewOpBase(sc),
		parser:  NewMemoryItemParser(),
		prompts: defaultReasoningBankSummaryPrompt,
	}
}

// NewSummarizeMemoryParallelOp 创建多轨迹推理策略提取操作。
func NewSummarizeMemoryParallelOp(sc *cecontext.ServiceContext) *SummarizeMemoryParallelOp {
	return &SummarizeMemoryParallelOp{
		OpBase:  *op.NewOpBase(sc),
		parser:  NewMemoryItemParser(),
		prompts: defaultReasoningBankSummaryPrompt,
	}
}

// NewUpdateVectorStoreOp 创建记忆向量存储更新操作。
func NewUpdateVectorStoreOp(sc *cecontext.ServiceContext) *UpdateVectorStoreOp {
	return &UpdateVectorStoreOp{OpBase: *op.NewOpBase(sc)}
}

// NewPersistMemoryOp 创建记忆持久化操作。
func NewPersistMemoryOp(sc *cecontext.ServiceContext, helper *cepersistence.MemoryPersistenceHelper) *PersistMemoryOp {
	return &PersistMemoryOp{
		OpBase: *op.NewOpBase(sc),
		helper: helper,
	}
}

// Execute 执行单轨迹推理策略提取。
// 对齐 Python SummarizeMemoryOp.async_execute。
func (o *SummarizeMemoryOp) Execute(ctx context.Context, rc *cecontext.RuntimeContext) error {
	// 检查 matts 参数
	matts, _ := cecontext.GetTyped[string](rc, "matts")
	if matts != "none" && matts != "sequential" {
		return nil
	}

	// 获取 query
	query, _ := cecontext.GetTyped[string](rc, "query")
	if query == "" {
		return nil
	}

	// 获取 trajectories
	trajectories, _ := cecontext.GetTyped[[]string](rc, "trajectories")
	if len(trajectories) == 0 {
		return nil
	}

	// 获取或判定 label
	// 对齐 Python context.label: Optional[List[bool]]，多轨迹场景每条轨迹各有一个 label，
	// 因此用 []bool 而非单值 bool。此处取第一个元素用于单轨迹提取。
	var label bool
	labelVal, ok := cecontext.GetTyped[[]bool](rc, "label")
	if ok && len(labelVal) > 0 {
		label = labelVal[0]
	} else {
		// 调用 LabelDeterminator 判定
		det := NewLabelDeterminator(o.ServiceContext())
		determinedLabel, err := det.DetermineLabel(ctx, query, trajectories[0])
		if err != nil {
			logger.Warn(logComponent).Err(err).Msg("LabelDeterminator failed, using default false")
			label = false
		} else {
			label = determinedLabel
		}
		// 对齐 Python: context.label = [is_success] — 判定后写回 RuntimeContext
		rc.Set("label", []bool{label})
	}

	// 选择系统提示词
	var systemPrompt *template.Template
	if label {
		systemPrompt = o.prompts.ExtractSuccessTrajSystemPrompt
	} else {
		systemPrompt = o.prompts.ExtractFailTrajSystemPrompt
	}

	// 渲染系统提示词
	var sysBuf bytes.Buffer
	if err := systemPrompt.Execute(&sysBuf, nil); err != nil {
		return fmt.Errorf("SummarizeMemoryOp: 渲染系统提示词失败: %w", err)
	}

	// 渲染用户提示词
	var userBuf bytes.Buffer
	if err := o.prompts.ExtractTrajUserPrompt.Execute(&userBuf, map[string]string{
		"Query":      query,
		"Trajectory": trajectories[0],
	}); err != nil {
		return fmt.Errorf("SummarizeMemoryOp: 渲染用户提示词失败: %w", err)
	}

	// 调用 LLM
	llm := o.LLM()
	if llm == nil {
		return fmt.Errorf("LLM not configured in ServiceContext")
	}

	response, err := llm.Generate(ctx, userBuf.String(), cecontext.WithSystemPrompt(sysBuf.String()))
	if err != nil {
		return fmt.Errorf("SummarizeMemoryOp: LLM 调用失败: %w", err)
	}

	// 解析响应
	memories := o.parser.Parse(response, query, &label)
	rc.Set("memories", memories)

	logger.Info(logComponent).
		Int("count", len(memories)).
		Str("matts", matts).
		Bool("label", label).
		Msg("SummarizeMemoryOp: completed single trajectory strategy extraction")

	return nil
}

// Execute 执行多轨迹推理策略提取。
// 对齐 Python SummarizeMemoryParallelOp.async_execute。
func (o *SummarizeMemoryParallelOp) Execute(ctx context.Context, rc *cecontext.RuntimeContext) error {
	// 检查 matts 参数
	matts, _ := cecontext.GetTyped[string](rc, "matts")
	if matts != "parallel" && matts != "combined" {
		return nil
	}

	// 获取 query 和 trajectories
	query, _ := cecontext.GetTyped[string](rc, "query")
	trajectories, _ := cecontext.GetTyped[[]string](rc, "trajectories")
	if len(trajectories) < 2 {
		return nil
	}

	// 构建多轨迹文本
	var trajBuf bytes.Buffer
	for i, t := range trajectories {
		fmt.Fprintf(&trajBuf, "<Trajectory %d>\n%s\n\n", i+1, t)
	}

	// 渲染系统提示词
	var sysBuf bytes.Buffer
	if err := o.prompts.ParallelScalingSystemPrompt.Execute(&sysBuf, nil); err != nil {
		return fmt.Errorf("SummarizeMemoryParallelOp: 渲染系统提示词失败: %w", err)
	}

	// 渲染用户提示词
	var userBuf bytes.Buffer
	if err := o.prompts.ParallelScalingUserPrompt.Execute(&userBuf, map[string]string{
		"Query":        query,
		"Trajectories": trajBuf.String(),
	}); err != nil {
		return fmt.Errorf("SummarizeMemoryParallelOp: 渲染用户提示词失败: %w", err)
	}

	// 调用 LLM
	llm := o.LLM()
	if llm == nil {
		return fmt.Errorf("LLM not configured in ServiceContext")
	}

	response, err := llm.Generate(ctx, userBuf.String(), cecontext.WithSystemPrompt(sysBuf.String()))
	if err != nil {
		return fmt.Errorf("SummarizeMemoryParallelOp: LLM 调用失败: %w", err)
	}

	// 解析响应，parallel 模式 label=nil
	memories := o.parser.Parse(response, query, nil)
	rc.Set("memories", memories)

	logger.Info(logComponent).
		Int("count", len(memories)).
		Str("matts", matts).
		Int("trajectory_count", len(trajectories)).
		Msg("SummarizeMemoryParallelOp: completed multi-trajectory strategy extraction")

	return nil
}

// Execute 执行记忆向量存储更新。
// 对齐 Python UpdateVectorStoreOp.async_execute。
func (o *UpdateVectorStoreOp) Execute(ctx context.Context, rc *cecontext.RuntimeContext) error {
	memories, _ := cecontext.GetTyped[[]*ceschema.ReasoningBankMemory](rc, "memories")
	userID, _ := cecontext.GetTyped[string](rc, "user_id")
	if userID == "" {
		userID = "default"
	}

	if len(memories) == 0 {
		rc.Set("stored_count", 0)
		rc.Set("memory_ids", []string{})
		return nil
	}

	embeddingModel := o.EmbeddingModel()
	if embeddingModel == nil {
		return fmt.Errorf("EmbeddingModel not configured in ServiceContext")
	}

	vectorStore := o.VectorStore()
	if vectorStore == nil {
		return fmt.Errorf("VectorStore not configured in ServiceContext")
	}

	// 设置 workspace_id 并收集 Query 字段用于 embedding
	queries := make([]string, len(memories))
	for i, memory := range memories {
		memory.WorkspaceID = userID
		queries[i] = memory.Query
	}

	// 批量生成 embedding
	embeddings, err := embeddingModel.EmbedBatch(ctx, queries)
	if err != nil {
		return fmt.Errorf("failed to generate embeddings: %w", err)
	}

	// 设置 embedding 并逐条 upsert
	memoryIDs := make([]string, 0, len(memories))
	storedCount := 0
	for i, memory := range memories {
		node := memory.ToVectorNode()
		if i < len(embeddings) {
			node.Embedding = embeddings[i]
		}
		if err := vectorStore.Upsert(ctx, node); err != nil {
			logger.Warn(logComponent).Err(err).Str("node_id", node.ID).Msg("Failed to upsert vector node")
			continue
		}
		memoryIDs = append(memoryIDs, node.ID)
		storedCount++
	}

	rc.Set("stored_count", storedCount)
	rc.Set("memory_ids", memoryIDs)
	rc.Set("memories", memories)

	logger.Info(logComponent).
		Int("stored_count", storedCount).
		Str("user_id", userID).
		Msg("UpdateVectorStoreOp (RB): completed memory vector store update")

	return nil
}

// Execute 执行记忆持久化。
// algoName 固定为 "rb"。
// 对齐 Python PersistMemoryOp.async_execute。
func (o *PersistMemoryOp) Execute(ctx context.Context, rc *cecontext.RuntimeContext) error {
	userID, _ := cecontext.GetTyped[string](rc, "user_id")
	if userID == "" {
		userID = "default"
	}

	vectorStore := o.VectorStore()
	if vectorStore == nil {
		return fmt.Errorf("VectorStore not configured in ServiceContext")
	}

	allNodes := vectorStore.GetAll(map[string]any{"workspace_id": userID, "type": "reasoning_bank_memory"})

	// 构建 nodesDict: {node.ID: node.ToDict()}
	nodesDict := make(map[string]any, len(allNodes))
	for _, node := range allNodes {
		nodesDict[node.ID] = node.ToDict()
	}

	if o.helper != nil {
		if err := o.helper.Save(userID, "rb", nodesDict); err != nil {
			return fmt.Errorf("failed to persist memories: %w", err)
		}
	}

	rc.Set("persist_count", len(nodesDict))

	logger.Info(logComponent).
		Int("count", len(nodesDict)).
		Str("user_id", userID).
		Str("helper_type", o.persistType()).
		Msg("PersistMemoryOp (RB): completed memory persistence")

	return nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// persistType 获取持久化助手的类型描述。
func (o *PersistMemoryOp) persistType() string {
	if o.helper != nil {
		return o.helper.PersistType()
	}
	return "none"
}

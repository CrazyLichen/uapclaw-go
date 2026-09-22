package reme

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	cecontext "github.com/uapclaw/uapclaw-go/internal/agentcore/context_evolver/core/context"
	op "github.com/uapclaw/uapclaw-go/internal/agentcore/context_evolver/core/op"
	cepersistence "github.com/uapclaw/uapclaw-go/internal/agentcore/context_evolver/core/persistence"
	ceschema "github.com/uapclaw/uapclaw-go/internal/agentcore/context_evolver/schema"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// TrajectoryPreprocessOp 轨迹预处理操作，按分数将轨迹分为成功/失败组。
// 对齐 Python TrajectoryPreprocessOp.async_execute。
type TrajectoryPreprocessOp struct {
	op.OpBase
}

// SuccessExtractionOp 成功轨迹经验提取操作。
// 对齐 Python SuccessExtractionOp.async_execute。
type SuccessExtractionOp struct {
	op.OpBase
	// useExtraction 是否执行提取
	useExtraction bool
	// prompts 提示词集合
	prompts ReMeSummaryPrompts
}

// FailureExtractionOp 失败轨迹经验提取操作。
// 对齐 Python FailureExtractionOp.async_execute。
type FailureExtractionOp struct {
	op.OpBase
	// useExtraction 是否执行提取
	useExtraction bool
	// prompts 提示词集合
	prompts ReMeSummaryPrompts
}

// ComparativeExtractionOp 对比经验提取操作（高低分对比）。
// 对齐 Python ComparativeExtractionOp.async_execute。
type ComparativeExtractionOp struct {
	op.OpBase
	// useExtraction 是否执行提取
	useExtraction bool
	// prompts 提示词集合
	prompts ReMeSummaryPrompts
}

// ComparativeAllExtractionOp 全量对比经验提取操作。
// 对齐 Python ComparativeAllExtractionOp.async_execute。
type ComparativeAllExtractionOp struct {
	op.OpBase
	// useExtraction 是否执行提取
	useExtraction bool
	// prompts 提示词集合
	prompts ReMeSummaryPrompts
}

// MemoryValidationOp 记忆校验操作，过滤低质量记忆。
// 对齐 Python MemoryValidationOp.async_execute。
type MemoryValidationOp struct {
	op.OpBase
	// useValidation 是否执行校验
	useValidation bool
	// prompts 提示词集合
	prompts ReMeSummaryPrompts
}

// MemoryDeduplicationOp 记忆去重操作，基于 embedding 相似度。
// 对齐 Python MemoryDeduplicationOp.async_execute。
type MemoryDeduplicationOp struct {
	op.OpBase
	// useDeduplication 是否执行去重
	useDeduplication bool
	// similarityThreshold 相似度阈值
	similarityThreshold float64
}

// UpdateVectorStoreOp 向量存储更新操作，将记忆写入向量库。
// 对齐 Python UpdateVectorStoreOp.async_execute。
type UpdateVectorStoreOp struct {
	op.OpBase
}

// PersistMemoryOp 记忆持久化操作，将向量节点保存到外部存储。
// 对齐 Python PersistMemoryOp.async_execute。
type PersistMemoryOp struct {
	op.OpBase
	// helper 持久化助手
	helper *cepersistence.MemoryPersistenceHelper
}

// ──────────────────────────── 导出函数 ────────────────────────────

// NewTrajectoryPreprocessOp 创建轨迹预处理操作。
func NewTrajectoryPreprocessOp(sc *cecontext.ServiceContext) *TrajectoryPreprocessOp {
	return &TrajectoryPreprocessOp{OpBase: *op.NewOpBase(sc)}
}

// NewSuccessExtractionOp 创建成功轨迹经验提取操作。
func NewSuccessExtractionOp(sc *cecontext.ServiceContext, useExtraction bool) *SuccessExtractionOp {
	return &SuccessExtractionOp{
		OpBase:        *op.NewOpBase(sc),
		useExtraction: useExtraction,
		prompts:       ReMeSummaryDefaultPrompts,
	}
}

// NewFailureExtractionOp 创建失败轨迹经验提取操作。
func NewFailureExtractionOp(sc *cecontext.ServiceContext, useExtraction bool) *FailureExtractionOp {
	return &FailureExtractionOp{
		OpBase:        *op.NewOpBase(sc),
		useExtraction: useExtraction,
		prompts:       ReMeSummaryDefaultPrompts,
	}
}

// NewComparativeExtractionOp 创建对比经验提取操作。
func NewComparativeExtractionOp(sc *cecontext.ServiceContext, useExtraction bool) *ComparativeExtractionOp {
	return &ComparativeExtractionOp{
		OpBase:        *op.NewOpBase(sc),
		useExtraction: useExtraction,
		prompts:       ReMeSummaryDefaultPrompts,
	}
}

// NewComparativeAllExtractionOp 创建全量对比经验提取操作。
func NewComparativeAllExtractionOp(sc *cecontext.ServiceContext, useExtraction bool) *ComparativeAllExtractionOp {
	return &ComparativeAllExtractionOp{
		OpBase:        *op.NewOpBase(sc),
		useExtraction: useExtraction,
		prompts:       ReMeSummaryDefaultPrompts,
	}
}

// NewMemoryValidationOp 创建记忆校验操作。
func NewMemoryValidationOp(sc *cecontext.ServiceContext, useValidation bool) *MemoryValidationOp {
	return &MemoryValidationOp{
		OpBase:        *op.NewOpBase(sc),
		useValidation: useValidation,
		prompts:       ReMeSummaryDefaultPrompts,
	}
}

// NewMemoryDeduplicationOp 创建记忆去重操作。
func NewMemoryDeduplicationOp(sc *cecontext.ServiceContext, useDeduplication bool, similarityThreshold float64) *MemoryDeduplicationOp {
	return &MemoryDeduplicationOp{
		OpBase:              *op.NewOpBase(sc),
		useDeduplication:    useDeduplication,
		similarityThreshold: similarityThreshold,
	}
}

// NewUpdateVectorStoreOp 创建向量存储更新操作。
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

// ──────────────────────────── 导出函数 ────────────────────────────

// Execute 执行轨迹预处理，按分数分组。
// 对齐 Python TrajectoryPreprocessOp.async_execute。
func (op *TrajectoryPreprocessOp) Execute(ctx context.Context, rc *cecontext.RuntimeContext) error {
	trajectories, _ := cecontext.GetTyped[[]string](rc, "trajectories")
	score, _ := cecontext.GetTyped[[]float64](rc, "score")
	threshold, _ := cecontext.GetTyped[float64](rc, "threshold")
	if threshold == 0 {
		threshold = 1
	}

	if len(trajectories) == 0 {
		logger.Warn(logComponent).Msg("No trajectories to preprocess")
		rc.Set("success_trajectories", []string{})
		rc.Set("failure_trajectories", []string{})
		rc.Set("all_trajectories", []string{})
		return nil
	}

	var success, failure []string
	for i, t := range trajectories {
		if i < len(score) && score[i] >= threshold {
			success = append(success, t)
		} else {
			failure = append(failure, t)
		}
	}

	all := make([]string, len(trajectories))
	copy(all, trajectories)

	rc.Set("success_trajectories", success)
	rc.Set("failure_trajectories", failure)
	rc.Set("all_trajectories", all)

	logger.Info(logComponent).
		Int("total", len(trajectories)).
		Int("success", len(success)).
		Int("failure", len(failure)).
		Msg("Preprocessed trajectories")

	return nil
}

// Execute 执行成功轨迹经验提取。
// 对齐 Python SuccessExtractionOp.async_execute。
func (op *SuccessExtractionOp) Execute(ctx context.Context, rc *cecontext.RuntimeContext) error {
	if !op.useExtraction {
		rc.Set("success_memories", []*ceschema.ReMeMemory{})
		return nil
	}

	successTrajectories, _ := cecontext.GetTyped[[]string](rc, "success_trajectories")
	if len(successTrajectories) == 0 {
		logger.Info(logComponent).Msg("No success trajectories to extract from")
		rc.Set("success_memories", []*ceschema.ReMeMemory{})
		return nil
	}

	llm := op.LLM()
	if llm == nil {
		return fmt.Errorf("LLM not configured in ServiceContext")
	}

	userID, _ := cecontext.GetTyped[string](rc, "user_id")
	if userID == "" {
		userID = "default"
	}
	query, _ := cecontext.GetTyped[string](rc, "query")

	var memories []*ceschema.ReMeMemory
	for _, trajectory := range successTrajectories {
		// 使用 strings.ReplaceAll 链式替换占位符，对齐 Python prompt.format()
		userPrompt := op.prompts.SuccessMemoryPrompt
		userPrompt = strings.ReplaceAll(userPrompt, "{query}", query)
		userPrompt = strings.ReplaceAll(userPrompt, "{step_sequence}", trajectory)
		userPrompt = strings.ReplaceAll(userPrompt, "{outcome}", "successful")
		response, err := llm.Generate(ctx, userPrompt)
		if err != nil {
			logger.Warn(logComponent).Err(err).Msg("Failed to generate success memory")
			continue
		}

		expList := ParseJSONExperienceResponse(response)
		for _, expData := range expList {
			memory := &ceschema.ReMeMemory{
				BaseMemory: ceschema.BaseMemory{WorkspaceID: userID},
				WhenToUse:  getStringFromMap(expData, "when_to_use", ""),
				Content:    getStringFromMap(expData, "experience", ""),
				Score:      1.0,
				CreatedAt:  time.Now().UTC(),
				UpdatedAt:  time.Now().UTC(),
				Metadata: ceschema.ReMeMemoryMetadata{
					Tags:       getStringSliceFromMap(expData, "tags"),
					StepType:   getStringFromMap(expData, "step_type", ""),
					ToolsUsed:  getStringSliceFromMap(expData, "tools_used"),
					Confidence: getFloatFromMap(expData, "confidence", 1.0),
					Freq:       0,
					Utility:    0,
				},
			}
			if memory.WhenToUse == "" || memory.Content == "" {
				logger.Warn(logComponent).Msg("Failed to create ReMeMemory from parsed data")
				continue
			}
			memories = append(memories, memory)
		}
	}

	rc.Set("success_memories", memories)
	logger.Info(logComponent).
		Int("count", len(memories)).
		Msg("Extracted insights from success trajectories")

	return nil
}

// Execute 执行失败轨迹经验提取。
// 对齐 Python FailureExtractionOp.async_execute。
func (op *FailureExtractionOp) Execute(ctx context.Context, rc *cecontext.RuntimeContext) error {
	if !op.useExtraction {
		rc.Set("failure_memories", []*ceschema.ReMeMemory{})
		return nil
	}

	failureTrajectories, _ := cecontext.GetTyped[[]string](rc, "failure_trajectories")
	if len(failureTrajectories) == 0 {
		logger.Info(logComponent).Msg("No failure trajectories to extract from")
		rc.Set("failure_memories", []*ceschema.ReMeMemory{})
		return nil
	}

	llm := op.LLM()
	if llm == nil {
		return fmt.Errorf("LLM not configured in ServiceContext")
	}

	userID, _ := cecontext.GetTyped[string](rc, "user_id")
	if userID == "" {
		userID = "default"
	}
	query, _ := cecontext.GetTyped[string](rc, "query")

	var memories []*ceschema.ReMeMemory
	for _, trajectory := range failureTrajectories {
		// 使用 strings.ReplaceAll 链式替换占位符，对齐 Python prompt.format()
		userPrompt := op.prompts.FailureMemoryPrompt
		userPrompt = strings.ReplaceAll(userPrompt, "{query}", query)
		userPrompt = strings.ReplaceAll(userPrompt, "{step_sequence}", trajectory)
		userPrompt = strings.ReplaceAll(userPrompt, "{outcome}", "failed")
		response, err := llm.Generate(ctx, userPrompt)
		if err != nil {
			logger.Warn(logComponent).Err(err).Msg("Failed to generate failure memory")
			continue
		}

		expList := ParseJSONExperienceResponse(response)
		for _, expData := range expList {
			memory := &ceschema.ReMeMemory{
				BaseMemory: ceschema.BaseMemory{WorkspaceID: userID},
				WhenToUse:  getStringFromMap(expData, "when_to_use", ""),
				Content:    getStringFromMap(expData, "experience", ""),
				Score:      1.0,
				CreatedAt:  time.Now().UTC(),
				UpdatedAt:  time.Now().UTC(),
				Metadata: ceschema.ReMeMemoryMetadata{
					Tags:       getStringSliceFromMap(expData, "tags"),
					StepType:   getStringFromMap(expData, "step_type", ""),
					ToolsUsed:  getStringSliceFromMap(expData, "tools_used"),
					Confidence: getFloatFromMap(expData, "confidence", 1.0),
					Freq:       0,
					Utility:    0,
				},
			}
			if memory.WhenToUse == "" || memory.Content == "" {
				logger.Warn(logComponent).Msg("Failed to create ReMeMemory from parsed data")
				continue
			}
			memories = append(memories, memory)
		}
	}

	rc.Set("failure_memories", memories)
	logger.Info(logComponent).
		Int("count", len(memories)).
		Msg("Extracted insights from failure trajectories")

	return nil
}

// Execute 执行对比经验提取（高低分轨迹对比）。
// 对齐 Python ComparativeExtractionOp.async_execute。
func (op *ComparativeExtractionOp) Execute(ctx context.Context, rc *cecontext.RuntimeContext) error {
	if !op.useExtraction {
		rc.Set("comparative_memories", []*ceschema.ReMeMemory{})
		return nil
	}

	allTrajectories, _ := cecontext.GetTyped[[]string](rc, "all_trajectories")
	if len(allTrajectories) < 2 {
		logger.Info(logComponent).Msg("Not enough trajectories for comparative extraction")
		rc.Set("comparative_memories", []*ceschema.ReMeMemory{})
		return nil
	}

	score, _ := cecontext.GetTyped[[]float64](rc, "score")
	if len(score) == 0 {
		logger.Info(logComponent).Msg("No scores for comparative extraction")
		rc.Set("comparative_memories", []*ceschema.ReMeMemory{})
		return nil
	}

	maxScore := score[0]
	minScore := score[0]
	maxIdx := 0
	minIdx := 0
	for i, s := range score {
		if s > maxScore {
			maxScore = s
			maxIdx = i
		}
		if s < minScore {
			minScore = s
			minIdx = i
		}
	}

	if maxScore == minScore {
		logger.Info(logComponent).Msg("All scores are equal, cannot compare")
		rc.Set("comparative_memories", []*ceschema.ReMeMemory{})
		return nil
	}

	llm := op.LLM()
	if llm == nil {
		return fmt.Errorf("LLM not configured in ServiceContext")
	}

	userID, _ := cecontext.GetTyped[string](rc, "user_id")
	if userID == "" {
		userID = "default"
	}

	higherSteps := ""
	lowerSteps := ""
	if maxIdx < len(allTrajectories) {
		higherSteps = allTrajectories[maxIdx]
	}
	if minIdx < len(allTrajectories) {
		lowerSteps = allTrajectories[minIdx]
	}

	// 使用 strings.ReplaceAll 链式替换占位符，对齐 Python prompt.format()
	userPrompt := op.prompts.ComparativeMemoryPrompt
	userPrompt = strings.ReplaceAll(userPrompt, "{higher_score}", fmt.Sprintf("%v", maxScore))
	userPrompt = strings.ReplaceAll(userPrompt, "{higher_steps}", higherSteps)
	userPrompt = strings.ReplaceAll(userPrompt, "{lower_score}", fmt.Sprintf("%v", minScore))
	userPrompt = strings.ReplaceAll(userPrompt, "{lower_steps}", lowerSteps)
	response, err := llm.Generate(ctx, userPrompt)
	if err != nil {
		logger.Warn(logComponent).Err(err).Msg("Failed to generate comparative memory")
		return err
	}

	expList := ParseJSONExperienceResponse(response)
	var memories []*ceschema.ReMeMemory
	for _, expData := range expList {
		memory := &ceschema.ReMeMemory{
			BaseMemory: ceschema.BaseMemory{WorkspaceID: userID},
			WhenToUse:  getStringFromMap(expData, "when_to_use", ""),
			Content:    getStringFromMap(expData, "experience", ""),
			Score:      1.0,
			CreatedAt:  time.Now().UTC(),
			UpdatedAt:  time.Now().UTC(),
			Metadata: ceschema.ReMeMemoryMetadata{
				Tags:       getStringSliceFromMap(expData, "tags"),
				StepType:   getStringFromMap(expData, "step_type", ""),
				ToolsUsed:  getStringSliceFromMap(expData, "tools_used"),
				Confidence: getFloatFromMap(expData, "confidence", 1.0),
				Freq:       0,
				Utility:    0,
			},
		}
		if memory.WhenToUse == "" || memory.Content == "" {
			logger.Warn(logComponent).Msg("Failed to create ReMeMemory from parsed data")
			continue
		}
		memories = append(memories, memory)
	}

	rc.Set("comparative_memories", memories)
	return nil
}

// Execute 执行全量对比经验提取。
// 对齐 Python ComparativeAllExtractionOp.async_execute。
func (op *ComparativeAllExtractionOp) Execute(ctx context.Context, rc *cecontext.RuntimeContext) error {
	if !op.useExtraction {
		rc.Set("comparative_memories", []*ceschema.ReMeMemory{})
		return nil
	}

	allTrajectories, _ := cecontext.GetTyped[[]string](rc, "all_trajectories")
	if len(allTrajectories) < 2 {
		logger.Info(logComponent).Msg("Not enough trajectories for comparative all extraction")
		rc.Set("comparative_memories", []*ceschema.ReMeMemory{})
		return nil
	}

	llm := op.LLM()
	if llm == nil {
		return fmt.Errorf("LLM not configured in ServiceContext")
	}

	userID, _ := cecontext.GetTyped[string](rc, "user_id")
	if userID == "" {
		userID = "default"
	}

	// 对齐 Python: "\n\n".join(f"# Trajectory {i+1}\n{t}" for i,t in enumerate(all_trajectories))
	var parts []string
	for i, t := range allTrajectories {
		parts = append(parts, fmt.Sprintf("# Trajectory %d\n%s", i+1, t))
	}
	trajectoriesStr := strings.Join(parts, "\n\n")

	// 使用 strings.ReplaceAll 链式替换占位符，对齐 Python prompt.format()
	userPrompt := op.prompts.ComparativeAllMemoryPrompt
	userPrompt = strings.ReplaceAll(userPrompt, "{trajectory}", trajectoriesStr)
	response, err := llm.Generate(ctx, userPrompt)
	if err != nil {
		logger.Warn(logComponent).Err(err).Msg("Failed to generate comparative all memory")
		return err
	}

	expList := ParseJSONExperienceResponse(response)
	var memories []*ceschema.ReMeMemory
	for _, expData := range expList {
		memory := &ceschema.ReMeMemory{
			BaseMemory: ceschema.BaseMemory{WorkspaceID: userID},
			WhenToUse:  getStringFromMap(expData, "when_to_use", ""),
			Content:    getStringFromMap(expData, "experience", ""),
			Score:      1.0,
			CreatedAt:  time.Now().UTC(),
			UpdatedAt:  time.Now().UTC(),
			Metadata: ceschema.ReMeMemoryMetadata{
				Tags:       getStringSliceFromMap(expData, "tags"),
				StepType:   getStringFromMap(expData, "step_type", ""),
				ToolsUsed:  getStringSliceFromMap(expData, "tools_used"),
				Confidence: getFloatFromMap(expData, "confidence", 1.0),
				Freq:       0,
				Utility:    0,
			},
		}
		if memory.WhenToUse == "" || memory.Content == "" {
			logger.Warn(logComponent).Msg("Failed to create ReMeMemory from parsed data")
			continue
		}
		memories = append(memories, memory)
	}

	rc.Set("comparative_memories", memories)
	return nil
}

// Execute 执行记忆校验，过滤低质量记忆。
// 对齐 Python MemoryValidationOp.async_execute。
func (op *MemoryValidationOp) Execute(ctx context.Context, rc *cecontext.RuntimeContext) error {
	successMemories, _ := cecontext.GetTyped[[]*ceschema.ReMeMemory](rc, "success_memories")
	failureMemories, _ := cecontext.GetTyped[[]*ceschema.ReMeMemory](rc, "failure_memories")
	comparativeMemories, _ := cecontext.GetTyped[[]*ceschema.ReMeMemory](rc, "comparative_memories")

	var allMemories []*ceschema.ReMeMemory
	allMemories = append(allMemories, successMemories...)
	allMemories = append(allMemories, failureMemories...)
	allMemories = append(allMemories, comparativeMemories...)

	if len(allMemories) == 0 {
		logger.Info(logComponent).Msg("No memories to validate")
		rc.Set("validated_memories", []*ceschema.ReMeMemory{})
		return nil
	}

	if !op.useValidation {
		rc.Set("validated_memories", allMemories)
		return nil
	}

	llm := op.LLM()
	if llm == nil {
		return fmt.Errorf("LLM not configured in ServiceContext")
	}

	var validated []*ceschema.ReMeMemory
	for _, memory := range allMemories {
		valid, score, reason := op.validateMemory(ctx, llm, memory)
		if valid {
			memory.Score = score
			validated = append(validated, memory)
		} else {
			logger.Warn(logComponent).
				Str("reason", reason).
				Msg("Memory validation failed")
		}
	}

	rc.Set("validated_memories", validated)
	return nil
}

// Execute 执行记忆去重，基于 embedding 相似度。
// 对齐 Python MemoryDeduplicationOp.async_execute。
func (op *MemoryDeduplicationOp) Execute(ctx context.Context, rc *cecontext.RuntimeContext) error {
	memories, _ := cecontext.GetTyped[[]*ceschema.ReMeMemory](rc, "validated_memories")
	userID, _ := cecontext.GetTyped[string](rc, "user_id")
	if userID == "" {
		userID = "default"
	}

	if len(memories) == 0 {
		logger.Info(logComponent).Msg("No memories to deduplicate")
		rc.Set("deduplicated_memories", []*ceschema.ReMeMemory{})
		rc.Set("duplicate_count", 0)
		return nil
	}

	if !op.useDeduplication {
		rc.Set("deduplicated_memories", memories)
		rc.Set("duplicate_count", 0)
		return nil
	}

	// 对齐 Python: if not self.embedding_model: raise ValueError(...)
	embeddingModel := op.EmbeddingModel()
	if embeddingModel == nil {
		return fmt.Errorf("EmbeddingModel not configured in ServiceContext")
	}
	vectorStore := op.VectorStore()

	// 获取已有记忆的 embeddings
	var existingEmbeddings [][]float64
	if vectorStore != nil {
		existingNodes := vectorStore.GetAll(map[string]any{"workspace_id": userID, "type": "reme_memory"})
		for _, node := range existingNodes {
			if node.Embedding != nil {
				existingEmbeddings = append(existingEmbeddings, node.Embedding)
			}
		}
	}

	var uniqueMemories []*ceschema.ReMeMemory
	var newEmbeddings [][]float64
	duplicateCount := 0

	for _, memory := range memories {
		embedContent := memory.WhenToUse + " " + memory.Content
		var emb []float64
		var err error
		if embeddingModel != nil {
			emb, err = embeddingModel.Embed(ctx, embedContent)
			if err != nil {
				logger.Warn(logComponent).Err(err).Msg("Failed to generate embedding for deduplication")
			}
		}

		if emb == nil {
			logger.Warn(logComponent).Msg("Embedding is nil, skipping deduplication check")
			uniqueMemories = append(uniqueMemories, memory)
			continue
		}

		// 与已有 embedding 比较
		isDuplicate := false
		for _, existing := range existingEmbeddings {
			if CalculateCosineSimilarity(emb, existing) > op.similarityThreshold {
				isDuplicate = true
				break
			}
		}

		// 与当前批次 unique_memories 比较
		if !isDuplicate {
			for _, newEmb := range newEmbeddings {
				if CalculateCosineSimilarity(emb, newEmb) > op.similarityThreshold {
					isDuplicate = true
					break
				}
			}
		}

		if isDuplicate {
			duplicateCount++
		} else {
			uniqueMemories = append(uniqueMemories, memory)
			newEmbeddings = append(newEmbeddings, emb)
		}
	}

	rc.Set("deduplicated_memories", uniqueMemories)
	rc.Set("duplicate_count", duplicateCount)
	return nil
}

// Execute 执行向量存储更新，将记忆写入向量库。
// 对齐 Python UpdateVectorStoreOp.async_execute。
func (op *UpdateVectorStoreOp) Execute(ctx context.Context, rc *cecontext.RuntimeContext) error {
	memories, _ := cecontext.GetTyped[[]*ceschema.ReMeMemory](rc, "deduplicated_memories")
	userID, _ := cecontext.GetTyped[string](rc, "user_id")
	if userID == "" {
		userID = "default"
	}

	if len(memories) == 0 {
		logger.Info(logComponent).Msg("No memories to store in vector store")
		rc.Set("stored_count", 0)
		rc.Set("memory_ids", []string{})
		return nil
	}

	embeddingModel := op.EmbeddingModel()
	if embeddingModel == nil {
		return fmt.Errorf("EmbeddingModel not configured in ServiceContext")
	}

	vectorStore := op.VectorStore()
	if vectorStore == nil {
		return fmt.Errorf("VectorStore not configured in ServiceContext")
	}

	// 批量生成 embedding
	contents := make([]string, len(memories))
	for i, memory := range memories {
		memory.WorkspaceID = userID
		contents[i] = memory.WhenToUse
	}

	embeddings, err := embeddingModel.EmbedBatch(ctx, contents)
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
	return nil
}

// Execute 执行记忆持久化，将向量节点保存到外部存储。
// 对齐 Python PersistMemoryOp.async_execute。
func (op *PersistMemoryOp) Execute(ctx context.Context, rc *cecontext.RuntimeContext) error {
	userID, _ := cecontext.GetTyped[string](rc, "user_id")
	if userID == "" {
		userID = "default"
	}

	vectorStore := op.VectorStore()
	if vectorStore == nil {
		return fmt.Errorf("VectorStore not configured in ServiceContext")
	}

	allNodes := vectorStore.GetAll(map[string]any{"workspace_id": userID, "type": "reme_memory"})
	if len(allNodes) == 0 {
		logger.Info(logComponent).
			Str("user_id", userID).
			Msg("No reme_memory nodes to persist")
		rc.Set("persist_count", 0)
		return nil
	}

	// 对齐 Python: nodes_dict = {node.id: node.to_dict() for node in all_nodes}
	nodesDict := make(map[string]any, len(allNodes))
	for _, node := range allNodes {
		nodesDict[node.ID] = node.ToDict()
	}

	if op.helper != nil {
		if err := op.helper.Save(userID, "reme", nodesDict); err != nil {
			return fmt.Errorf("failed to persist memories: %w", err)
		}
	}

	rc.Set("persist_count", len(nodesDict))
	logger.Info(logComponent).
		Int("count", len(nodesDict)).
		Str("user_id", userID).
		Str("helper_type", op.persistType()).
		Msg("PersistMemoryOp (ReMe): persisted memories for user")

	return nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// validateMemory 校验单条记忆质量。
// 对齐 Python MemoryValidationOp._validate_memory。
func (op *MemoryValidationOp) validateMemory(ctx context.Context, llm cecontext.LLMService, memory *ceschema.ReMeMemory) (bool, float64, string) {
	// 使用 strings.ReplaceAll 链式替换占位符，对齐 Python prompt.format()
	userPrompt := op.prompts.MemoryValidationPrompt
	userPrompt = strings.ReplaceAll(userPrompt, "{condition}", memory.WhenToUse)
	userPrompt = strings.ReplaceAll(userPrompt, "{task_memory_content}", memory.Content)
	response, err := llm.Generate(ctx, userPrompt)
	if err != nil {
		logger.Error(logComponent).Err(err).Msg("LLM 校验失败")
		return false, 0, fmt.Sprintf("LLM generate failed: %v", err)
	}

	// 提取 ```json 代码块
	jsonBlocks := jsonBlockPattern.FindAllStringSubmatch(response, -1)
	jsonStr := ""
	if len(jsonBlocks) > 0 {
		jsonStr = jsonBlocks[0][1]
	} else {
		// fallback: 尝试直接解析整个响应
		jsonStr = response
	}

	var result map[string]any
	if err := json.Unmarshal([]byte(jsonStr), &result); err != nil {
		return false, 0, fmt.Sprintf("JSON parse failed: %v", err)
	}

	isValid, _ := result["is_valid"].(bool)
	score := getFloatFromMap(result, "score", 0)
	reason := getStringFromMap(result, "reason", "")

	if !isValid || score < 0.5 {
		if reason == "" {
			reason = getStringFromMap(result, "feedback", "score too low or invalid")
		}
		return false, score, reason
	}

	return true, score, ""
}

// persistType 获取持久化助手的类型描述。
func (op *PersistMemoryOp) persistType() string {
	if op.helper != nil {
		return op.helper.PersistType()
	}
	return "none"
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// getStringFromMap 从 map[string]any 中安全获取字符串字段。
func getStringFromMap(m map[string]any, key, defaultVal string) string {
	if v, ok := m[key]; ok && v != nil {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return defaultVal
}

// getFloatFromMap 从 map[string]any 中安全获取 float64 字段。
func getFloatFromMap(m map[string]any, key string, defaultVal float64) float64 {
	if v, ok := m[key]; ok && v != nil {
		switch n := v.(type) {
		case float64:
			return n
		case int:
			return float64(n)
		}
	}
	return defaultVal
}

// getStringSliceFromMap 从 map[string]any 中安全获取 []string 字段。
func getStringSliceFromMap(m map[string]any, key string) []string {
	if v, ok := m[key]; ok && v != nil {
		switch arr := v.(type) {
		case []string:
			return arr
		case []any:
			result := make([]string, 0, len(arr))
			for _, item := range arr {
				if s, ok := item.(string); ok {
					result = append(result, s)
				}
			}
			return result
		}
	}
	return nil
}

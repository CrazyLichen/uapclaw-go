package rb

import (
	"bytes"
	"context"
	"fmt"
	"regexp"
	"strings"

	cecontext "github.com/uapclaw/uapclaw-go/internal/agentcore/context_evolver/core/context"
	op "github.com/uapclaw/uapclaw-go/internal/agentcore/context_evolver/core/op"
	ceschema "github.com/uapclaw/uapclaw-go/internal/agentcore/context_evolver/schema"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ParallelScalingOp 并行缩放操作：生成 k 条多样轨迹，交由 BestOfNOp 选优。
//
// 对齐 Python ParallelScalingOp：
// 为同一 query 生成 k 条不同温度的轨迹，允许 Agent 探索不同解法。
// 最佳轨迹由后续 BestOfNOp 评估选出。
//
// Python: openjiuwen/extensions/context_evolver/retrieve/task/reasoning_bank/matts.py
type ParallelScalingOp struct {
	op.OpBase
	// k 并行轨迹数量（缩放因子）
	k int
	// temperature 采样温度，用于轨迹多样性
	temperature float64
	// prompts 提示词配置
	prompts *ReasoningBankPrompt
}

// SequentialScalingOp 串行缩放操作：对单条轨迹进行 k 轮自检精炼。
//
// 对齐 Python SequentialScalingOp：
// 迭代精炼一条轨迹，每轮 Agent 重新审视并修正推理。
//
// Python: openjiuwen/extensions/context_evolver/retrieve/task/reasoning_bank/matts.py
type SequentialScalingOp struct {
	op.OpBase
	// k 精炼轮数（缩放因子）
	k int
	// prompts 提示词配置
	prompts *ReasoningBankPrompt
}

// BestOfNOp Best-of-N 选择操作：用 LLM 评估器从 N 条候选轨迹中选最优。
//
// 对齐 Python BestOfNOp：
// 基于 Progress/efficiency/error/quality 四维度评估，返回最佳轨迹索引。
//
// Python: openjiuwen/extensions/context_evolver/retrieve/task/reasoning_bank/matts.py
type BestOfNOp struct {
	op.OpBase
	// prompts 提示词配置
	prompts *ReasoningBankPrompt
}

// SelfContrastMemoryOp 自对比记忆提取操作：对比成功/失败轨迹提取推理策略。
//
// 对齐 Python SelfContrastMemoryOp：
// 比较成功与失败轨迹，识别致胜模式和失败原因，提取可迁移的记忆。
//
// Python: openjiuwen/extensions/context_evolver/retrieve/task/reasoning_bank/matts.py
type SelfContrastMemoryOp struct {
	op.OpBase
	// prompts 提示词配置
	prompts *ReasoningBankPrompt
}

// trajectoryData 轨迹数据，存入 RuntimeContext。
type trajectoryData struct {
	// Index 轨迹序号
	Index int
	// Answer Agent 回答
	Answer string
	// Steps 执行步骤
	Steps []any
	// Success 是否成功
	Success bool
}

// refinementRecord 精炼记录。
type refinementRecord struct {
	// Round 轮次
	Round int
	// Prompt 精炼提示词
	Prompt string
	// Response LLM 响应
	Response string
}

// ──────────────────────────── 常量 ────────────────────────────

const (
	// logComponent 日志组件标识
	logComponent = logger.ComponentAgentCore
)

// ──────────────────────────── 导出函数 ────────────────────────────

// NewParallelScalingOp 创建并行缩放操作。
// 对齐 Python ParallelScalingOp(k=3, temperature=0.9)。
func NewParallelScalingOp(sc *cecontext.ServiceContext, k int, temperature float64) *ParallelScalingOp {
	return &ParallelScalingOp{
		OpBase:      *op.NewOpBase(sc),
		k:           k,
		temperature: temperature,
		prompts:     defaultReasoningBankPrompt,
	}
}

// NewSequentialScalingOp 创建串行缩放操作。
// 对齐 Python SequentialScalingOp(k=3)。
func NewSequentialScalingOp(sc *cecontext.ServiceContext, k int) *SequentialScalingOp {
	return &SequentialScalingOp{
		OpBase:  *op.NewOpBase(sc),
		k:       k,
		prompts: defaultReasoningBankPrompt,
	}
}

// NewBestOfNOp 创建 Best-of-N 选择操作。
// 对齐 Python BestOfNOp()。
func NewBestOfNOp(sc *cecontext.ServiceContext) *BestOfNOp {
	return &BestOfNOp{
		OpBase:  *op.NewOpBase(sc),
		prompts: defaultReasoningBankPrompt,
	}
}

// NewSelfContrastMemoryOp 创建自对比记忆提取操作。
// 对齐 Python SelfContrastMemoryOp()。
func NewSelfContrastMemoryOp(sc *cecontext.ServiceContext) *SelfContrastMemoryOp {
	return &SelfContrastMemoryOp{
		OpBase:  *op.NewOpBase(sc),
		prompts: defaultReasoningBankPrompt,
	}
}

// Execute 执行并行缩放，生成 k 条轨迹。
// 对齐 Python ParallelScalingOp.async_execute。
func (o *ParallelScalingOp) Execute(ctx context.Context, rc *cecontext.RuntimeContext) error {
	logger.Info(logComponent).Int("k", o.k).Msg("Executing parallel scaling")

	query, _ := cecontext.GetTyped[string](rc, "query")

	agentFlow := o.AgentFlow()
	if agentFlow == nil {
		logger.Warn(logComponent).Msg("AgentFlow not configured, skipping parallel scaling")
		return nil
	}

	// 对齐 Python: llm.temperature = self.temperature
	// Go: 通过 AgentFlow 传递 llm_temperature，Agent 内部 LLM 调用时读取
	llmTempKey := "llm_temperature"
	originalTemp := rc.Get(llmTempKey)
	rc.Set(llmTempKey, o.temperature)

	trajectories := make([]*trajectoryData, 0, o.k)
	for i := 0; i < o.k; i++ {
		logger.Info(logComponent).Int("trajectory", i+1).Int("k", o.k).Msg("Generating trajectory")

		// 对齐 Python：sessionID = parallel_{i}
		sessionID := fmt.Sprintf("parallel_%d", i)

		// 执行 Agent 轨迹
		// 对齐 Python: llm.temperature = self.temperature + retrieval_query=query
		result, err := agentFlow.Execute(ctx, query, sessionID,
			cecontext.WithRetrievalQuery(query),
			cecontext.WithLLMTemperature(o.temperature),
		)
		if err != nil {
			logger.Warn(logComponent).Int("index", i).Err(err).Msg("Parallel trajectory failed, skipping")
			trajectories = append(trajectories, &trajectoryData{
				Index:   i,
				Answer:  "",
				Steps:   nil,
				Success: false,
			})
			continue
		}

		trajectories = append(trajectories, &trajectoryData{
			Index:   i,
			Answer:  result.Answer,
			Steps:   result.Steps,
			Success: result.Success,
		})
	}

	// 存入 RuntimeContext
	rc.Set("parallel_trajectories", trajectories)
	rc.Set("scaling_factor", o.k)

	// 对齐 Python: llm.temperature = original_temp
	// Go: 恢复原始温度设置
	rc.Set(llmTempKey, originalTemp)

	logger.Info(logComponent).Int("count", len(trajectories)).Msg("Generated trajectories")

	return nil
}

// Execute 执行串行缩放，迭代精炼轨迹。
// 对齐 Python SequentialScalingOp.async_execute。
func (o *SequentialScalingOp) Execute(ctx context.Context, rc *cecontext.RuntimeContext) error {
	logger.Info(logComponent).Int("k", o.k).Msg("Executing sequential scaling")

	query, _ := cecontext.GetTyped[string](rc, "query")
	currentAnswer, _ := cecontext.GetTyped[string](rc, "answer")

	llm := o.LLM()
	if llm == nil {
		return fmt.Errorf("SequentialScalingOp: LLM not configured")
	}

	refinementHistory := make([]*refinementRecord, 0, o.k)

	for roundIdx := 0; roundIdx < o.k; roundIdx++ {
		logger.Info(logComponent).Int("round", roundIdx+1).Int("k", o.k).Msg("Refinement round")

		// 对齐 Python：首轮用首次精炼提示词，后续轮用简化提示词
		var promptBuf bytes.Buffer
		var tmplErr error
		if roundIdx == 0 {
			tmplErr = o.prompts.SequentialFirstRefinePrompt.Execute(&promptBuf, map[string]any{
				"CurrentAnswer": currentAnswer,
				"Query":         query,
			})
		} else {
			tmplErr = o.prompts.SequentialFollowUpRefinePrompt.Execute(&promptBuf, map[string]any{
				"CurrentAnswer": currentAnswer,
				"Query":         query,
			})
		}
		if tmplErr != nil {
			return fmt.Errorf("SequentialScalingOp: 构建精炼提示词失败: %w", tmplErr)
		}
		prompt := promptBuf.String()

		// 调用 LLM
		response, err := llm.Generate(ctx, prompt)
		if err != nil {
			logger.Error(logComponent).Int("round", roundIdx+1).Err(err).Msg("LLM refinement call failed")
			return fmt.Errorf("SequentialScalingOp: LLM 精炼调用失败: %w", err)
		}

		refinementHistory = append(refinementHistory, &refinementRecord{
			Round:    roundIdx,
			Prompt:   prompt,
			Response: response,
		})

		currentAnswer = response
	}

	// 存入 RuntimeContext
	rc.Set("refinement_history", refinementHistory)
	rc.Set("refined_answer", currentAnswer)
	rc.Set("answer", currentAnswer)
	rc.Set("scaling_factor", o.k)

	logger.Info(logComponent).Int("rounds", o.k).Msg("Completed refinement rounds")

	return nil
}

// Execute 执行 Best-of-N 评估，选择最优轨迹。
// 对齐 Python BestOfNOp.async_execute。
func (o *BestOfNOp) Execute(ctx context.Context, rc *cecontext.RuntimeContext) error {
	trajectories, ok := cecontext.GetTyped[[]*trajectoryData](rc, "parallel_trajectories")
	if !ok || len(trajectories) == 0 {
		return fmt.Errorf("BestOfNOp: parallel_trajectories not set or empty")
	}

	logger.Info(logComponent).Int("candidates", len(trajectories)).Msg("Selecting best trajectory")

	query, _ := cecontext.GetTyped[string](rc, "query")

	llm := o.LLM()
	if llm == nil {
		return fmt.Errorf("BestOfNOp: LLM not configured")
	}

	// 对齐 Python：构建轨迹描述列表
	var trajDescs []string
	for i, traj := range trajectories {
		desc := fmt.Sprintf("Trajectory %d:\nAnswer: %s\nSuccess: %v\nSteps: %d",
			i+1, traj.Answer, traj.Success, len(traj.Steps))
		trajDescs = append(trajDescs, desc)
	}

	// 构建评估提示词
	var buf bytes.Buffer
	if err := o.prompts.BestOfNEvalPrompt.Execute(&buf, map[string]any{
		"NumTrajectories":  len(trajectories),
		"Query":            query,
		"TrajDescriptions": strings.Join(trajDescs, "\n\n"),
		"MaxIndex":         len(trajectories) - 1,
	}); err != nil {
		return fmt.Errorf("BestOfNOp: 构建评估提示词失败: %w", err)
	}

	// 调用 LLM 评估
	response, err := llm.Generate(ctx, buf.String(), cecontext.WithTemperature(0.0))
	if err != nil {
		logger.Error(logComponent).Err(err).Msg("BestOfN LLM evaluation failed")
		return fmt.Errorf("BestOfNOp: LLM 评估调用失败: %w", err)
	}

	// 对齐 Python：使用正则 \b\d+\b 解析索引
	bestIdx := parseBestIndex(response, len(trajectories))
	if bestIdx < 0 {
		logger.Warn(logComponent).Str("response", response).Msg("Failed to parse best index, falling back to 0")
		bestIdx = 0
	}

	logger.Info(logComponent).Int("best_index", bestIdx).Msg("Selected best trajectory")

	bestTraj := trajectories[bestIdx]
	rc.Set("answer", bestTraj.Answer)
	rc.Set("best_trajectory_index", bestIdx)
	rc.Set("best_trajectory", bestTraj)

	// 对齐 Python：计算 Pass@k
	successCount := 0
	for _, t := range trajectories {
		if t.Success {
			successCount++
		}
	}
	passAtK := float64(successCount) / float64(len(trajectories))
	rc.Set("pass_at_k", passAtK)

	return nil
}

// Execute 执行自对比记忆提取。
// 对齐 Python SelfContrastMemoryOp.async_execute。
func (o *SelfContrastMemoryOp) Execute(ctx context.Context, rc *cecontext.RuntimeContext) error {
	trajectories, ok := cecontext.GetTyped[[]*trajectoryData](rc, "parallel_trajectories")
	if !ok || len(trajectories) == 0 {
		return fmt.Errorf("SelfContrastMemoryOp: parallel_trajectories not set or empty")
	}

	logger.Info(logComponent).Msg("Extracting memories using self-contrast")

	query, _ := cecontext.GetTyped[string](rc, "query")
	userID, _ := cecontext.GetTyped[string](rc, "user_id")

	llm := o.LLM()
	if llm == nil {
		return fmt.Errorf("SelfContrastMemoryOp: LLM not configured")
	}

	// 对齐 Python：分离成功和失败轨迹
	var successful, failed []*trajectoryData
	for _, t := range trajectories {
		if t.Success {
			successful = append(successful, t)
		} else {
			failed = append(failed, t)
		}
	}

	logger.Info(logComponent).
		Int("successful", len(successful)).
		Int("failed", len(failed)).
		Msg("Trajectory split results")

	// 对齐 Python：构建轨迹描述
	successfulDescs := make([]string, 0, len(successful))
	for _, t := range successful {
		answer := t.Answer
		if len(answer) > 200 {
			answer = answer[:200] + "..."
		}
		successfulDescs = append(successfulDescs, fmt.Sprintf("Trajectory %d: %s", t.Index, answer))
	}
	failedDescs := make([]string, 0, len(failed))
	for _, t := range failed {
		answer := t.Answer
		if len(answer) > 200 {
			answer = answer[:200] + "..."
		}
		failedDescs = append(failedDescs, fmt.Sprintf("Trajectory %d: %s", t.Index, answer))
	}

	// 构建提取提示词
	var buf bytes.Buffer
	if err := o.prompts.SelfContrastPrompt.Execute(&buf, map[string]any{
		"Query":                  query,
		"NumSuccessful":          len(successful),
		"SuccessfulTrajectories": strings.Join(successfulDescs, "\n"),
		"NumFailed":              len(failed),
		"FailedTrajectories":     strings.Join(failedDescs, "\n"),
	}); err != nil {
		return fmt.Errorf("SelfContrastMemoryOp: 构建提取提示词失败: %w", err)
	}

	// 调用 LLM 提取
	response, err := llm.Generate(ctx, buf.String(), cecontext.WithTemperature(1.0))
	if err != nil {
		logger.Error(logComponent).Err(err).Msg("Self-contrast LLM call failed")
		return fmt.Errorf("SelfContrastMemoryOp: LLM 调用失败: %w", err)
	}

	// 对齐 Python：解析记忆项
	memories := parseContrastiveMemories(response, query, userID)

	rc.Set("contrastive_memories", memories)
	logger.Info(logComponent).Int("count", len(memories)).Msg("Extracted contrastive memories")

	return nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// parseBestIndex 从 LLM 响应中解析最优轨迹索引。
// 使用正则 \b\d+\b 匹配第一个小于 total 的数字索引。
// 对齐 Python BestOfNOp.async_execute 中的索引提取逻辑。
func parseBestIndex(response string, total int) int {
	re := regexp.MustCompile(`\b\d+\b`)
	matches := re.FindAllString(response, -1)
	for _, m := range matches {
		var idx int
		if _, err := fmt.Sscanf(m, "%d", &idx); err == nil && idx < total {
			return idx
		}
	}
	return -1
}

// parseContrastiveMemories 从 LLM 响应中解析对比记忆。
// 按 "# Memory Item" 分割，提取 ## Title、## Description、## Content，
// 构建 ReasoningBankMemory 对象。
// 对齐 Python SelfContrastMemoryOp.async_execute 中的记忆解析逻辑。
func parseContrastiveMemories(llmResponse string, query string, userID string) []*ceschema.ReasoningBankMemory {
	// 按 "# Memory Item" 分割
	sections := strings.Split(llmResponse, "# Memory Item")

	var memories []*ceschema.ReasoningBankMemory

	for _, section := range sections {
		section = strings.TrimSpace(section)
		if section == "" {
			continue
		}

		title := extractMarkdownSection(section, "## Title")
		description := extractMarkdownSection(section, "## Description")
		content := extractMarkdownSection(section, "## Content")

		if title == "" && description == "" && content == "" {
			continue
		}

		mem := &ceschema.ReasoningBankMemory{
			BaseMemory: ceschema.BaseMemory{
				WorkspaceID: userID,
			},
			Query: query,
			Memory: []ceschema.ReasoningBankMemoryItem{
				{
					Title:       title,
					Description: description,
					Content:     content,
				},
			},
		}
		memories = append(memories, mem)
	}

	return memories
}

// extractMarkdownSection 从 Markdown 文本中提取指定标题下的内容。
func extractMarkdownSection(text string, heading string) string {
	idx := strings.Index(text, heading)
	if idx == -1 {
		return ""
	}
	// 跳过标题行
	content := text[idx+len(heading):]
	content = strings.TrimSpace(content)

	// 找到下一个 ## 标题或文本末尾
	nextHeading := strings.Index(content, "\n## ")
	if nextHeading == -1 {
		return strings.TrimSpace(content)
	}
	return strings.TrimSpace(content[:nextHeading])
}

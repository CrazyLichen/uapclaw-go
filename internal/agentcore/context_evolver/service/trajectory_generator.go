package service

import (
	"context"
	"fmt"
	"strings"

	ceconfig "github.com/uapclaw/uapclaw-go/internal/agentcore/context_evolver/core/config"
	cecontext "github.com/uapclaw/uapclaw-go/internal/agentcore/context_evolver/core/context"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// TrialOutput 单次试验执行结果。对齐 Python TrialOutput。
type TrialOutput struct {
	// Trajectory 格式化后的轨迹文本
	Trajectory string
	// Feedback 评估反馈
	Feedback string
	// Score 评估分数
	Score int
}

// RunTrialsInput MaTTS 试验运行参数。对齐 Python RunTrialsInput。
type RunTrialsInput struct {
	// Agent Agent 执行服务
	Agent cecontext.AgentFlowService
	// UserID 用户标识
	UserID string
	// Question 问题
	Question string
	// GroundTruth 参考答案
	GroundTruth string
	// MattsK 缩放因子
	MattsK int
	// MattsMode 缩放模式：none/parallel/sequential/combined
	MattsMode string
	// MemoryService 记忆服务，对齐 Python memory_service 参数
	MemoryService *TaskMemoryService
	// PersistType 持久化类型，对齐 Python persist_type
	PersistType *string
	// PersistPath JSON 持久化路径，对齐 Python persist_path
	PersistPath string
	// MilvusHost Milvus 主机，对齐 Python milvus_host
	MilvusHost string
	// MilvusPort Milvus 端口，对齐 Python milvus_port
	MilvusPort int
	// MilvusCollection Milvus 集合名，对齐 Python milvus_collection
	MilvusCollection string
}

// SummarizeTrajectoriesInput 轨迹摘要参数。对齐 Python SummarizeTrajectoriesInput。
type SummarizeTrajectoriesInput struct {
	// Query 查询
	Query string
	// Trajectory 轨迹列表
	Trajectory []string
	// MattsMode MaTTS 模式
	MattsMode string
	// GroundTruth 参考答案
	GroundTruth *string
	// Feedback 反馈列表
	Feedback []string
	// Score 分数列表
	Score []int
}

// Message 轨迹消息。
type Message struct {
	// Role 消息角色：user/assistant/tool
	Role string
	// Content 消息内容
	Content string
	// ToolName 工具名称（仅 tool 角色）
	ToolName string
	// ToolArgs 工具参数（仅 tool 角色）
	ToolArgs string
}

// ──────────────────────────── 常量 ────────────────────────────

const (
	// logComponent 日志组件
	logComponent = logger.ComponentAgentCore

	// selfRefinePrompt 自纠正提示词。对齐 Python _SELF_REFINE_PROMPT。
	selfRefinePrompt = "Let's carefully re-examine the previous trajectory, including your reasoning " +
		"steps and action taken. Pay special attention to whether you used the best " +
		"search sequence and whether you used the tool correctly. If you find " +
		"inconsistencies, correct them. If everything seems correct, make it more " +
		"efficient. Now, solve the same problem again from scratch.\n\n"

	// selfDiversityPrompt 多样性提示词。对齐 Python _SELF_DIVERSITY_PROMPT。
	selfDiversityPrompt = "Let's carefully re-examine the previous trajectory, including your reasoning " +
		"steps and action taken. The solution might be correct or wrong. Now, solve the " +
		"same problem again from scratch using DIFFERENT reasoning approach. " +
		"Focus on exploring alternative strategies.\n\n"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// EvaluateTrial 评估单次试验结果。对齐 Python evaluate_trial()。
func EvaluateTrial(question string, output string, groundTruth string) (string, int) {
	if groundTruth != "" {
		isCorrect := strings.Contains(strings.ToLower(output), strings.ToLower(groundTruth))
		if isCorrect {
			return "success", 1
		}
		return "failure", 0
	}
	return "success", 1
}

// RunTrials 运行 MaTTS 试验并返回轨迹结果。对齐 Python run_trials()。
func RunTrials(ctx context.Context, agent cecontext.AgentFlowService, params RunTrialsInput) ([]TrialOutput, error) {
	var mattsK int
	var selfRefine bool

	if params.MattsMode == "none" {
		mattsK = 1
		selfRefine = false
	} else {
		mattsK = params.MattsK
		if mattsK <= 0 {
			mattsK = 3
		}
		selfRefine = (params.MattsMode == "sequential" || params.MattsMode == "combined")
	}

	results, err := runTrialsInner(ctx, agent, params.Question, params.GroundTruth, mattsK, selfRefine)
	if err != nil {
		return nil, err
	}

	// 对齐 Python：运行后调用 SummarizeTrajectories 摘要记忆
	if params.MemoryService != nil {
		trajectories := make([]string, 0, len(results))
		for _, r := range results {
			if r.Trajectory != "" {
				trajectories = append(trajectories, r.Trajectory)
			}
		}
		feedbacks := make([]string, 0, len(results))
		scores := make([]int, 0, len(results))
		for _, r := range results {
			feedbacks = append(feedbacks, r.Feedback)
			scores = append(scores, r.Score)
		}

		sumParams := SummarizeTrajectoriesInput{
			Query:      params.Question,
			Trajectory: trajectories,
			MattsMode:  params.MattsMode,
			Feedback:   feedbacks,
			Score:      scores,
		}
		if params.GroundTruth != "" {
			gt := params.GroundTruth
			sumParams.GroundTruth = &gt
		}

		if _, summarizeErr := SummarizeTrajectories(ctx, params.MemoryService, params.UserID, sumParams); summarizeErr != nil {
			logger.Error(logComponent).Err(summarizeErr).Msg("Failed to summarize trajectories after run_trials")
		}
	}

	return results, nil
}

// SummarizeTrajectories 将轨迹总结为记忆。对齐 Python summarize_trajectories()。
// P6 阶段实现：委托 TaskMemoryService.Summarize()，含序列截断和算法特定 kwargs 过滤。
func SummarizeTrajectories(ctx context.Context, memoryService *TaskMemoryService, userID string, params SummarizeTrajectoriesInput) (*SummarizeResult, error) {
	// 对齐 Python：matts_mode == "sequential" 时只保留最后一条轨迹
	trajectories := params.Trajectory
	feedbacks := params.Feedback
	scores := params.Score

	if params.MattsMode == "sequential" && len(trajectories) > 1 {
		trajectories = trajectories[len(trajectories)-1:]
		if len(feedbacks) > 1 {
			feedbacks = feedbacks[len(feedbacks)-1:]
		}
		if len(scores) > 1 {
			scores = scores[len(scores)-1:]
		}
	}

	// 对齐 Python：算法特定 kwargs 过滤
	extraKwargs := make(map[string]any)

	// ReMe 系列：传 score
	if isReMeFamily(memoryService.summaryAlgorithm) {
		extraKwargs["score"] = scores
	}

	// ReasoningBank + USE_GOLDLABEL：从 score 推导 label
	if memoryService.summaryAlgorithm == "ReasoningBank" {
		if getUseGoldLabel() {
			labels := make([]bool, len(scores))
			for i, s := range scores {
				labels[i] = (s == 1)
			}
			extraKwargs["label"] = labels
		}
	}

	// ACE + USE_GROUNDTRUTH：传 feedback + ground_truth
	if memoryService.summaryAlgorithm == "ACE" {
		if getUseGroundTruth() {
			extraKwargs["feedback"] = feedbacks
			if params.GroundTruth != nil {
				extraKwargs["ground_truth"] = *params.GroundTruth
			}
		}
	}

	// 对齐 Python：委托 TaskMemoryService.Summarize()
	result, err := memoryService.Summarize(ctx, userID, params.MattsMode, params.Query, trajectories, extraKwargs)
	if err != nil {
		// 对齐 Python：logger.error("Failed to summarize trajectories: %s", exc)
		logger.Error(logComponent).Err(err).Msg("Failed to summarize trajectories")
		return nil, err
	}

	return result, nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// formatTrajectory 将消息列表格式化为轨迹文本。对齐 Python format_trajectory()。
func formatTrajectory(messages []Message) string {
	var transcript []string

	for _, msg := range messages {
		switch msg.Role {
		case "user":
			content := msg.Content
			// 去除 "Task:" 前缀
			taskMarker := "Task:"
			if strings.HasPrefix(content, taskMarker) {
				content = strings.TrimLeft(content[len(taskMarker):], "\n ")
				content = strings.TrimSpace(content)
			}
			// 去除注入的记忆块及其之后的内容
			relatedExpMarker := "Some Related Experience to help you complete the task"
			if idx := strings.Index(content, relatedExpMarker); idx != -1 {
				content = strings.TrimSpace(content[:idx])
			}
			// 若存在 "Question: " 标记，仅保留最后一次出现之后的内容
			questionMarker := "Question: "
			if lastIdx := strings.LastIndex(content, questionMarker); lastIdx != -1 {
				content = content[lastIdx+len(questionMarker):]
			}
			transcript = append(transcript, fmt.Sprintf("USER: %s", strings.TrimSpace(content)))
		case "assistant":
			if msg.Content != "" {
				transcript = append(transcript, fmt.Sprintf("THOUGHT: %s", msg.Content))
			}
			if msg.ToolName != "" {
				transcript = append(transcript, fmt.Sprintf("ACTION: %s(%s)", msg.ToolName, msg.ToolArgs))
			}
		case "tool":
			transcript = append(transcript, fmt.Sprintf("OBSERVATION: %s", msg.Content))
		}
	}

	return strings.Join(transcript, "\n")
}

// runTrialsInner 内部试验循环。对齐 Python _run_trials_inner()。
func runTrialsInner(ctx context.Context, agent cecontext.AgentFlowService, question string, groundTruth string, mattsK int, selfRefine bool) ([]TrialOutput, error) {
	var results []TrialOutput

	for runID := 0; runID < mattsK; runID++ {
		logger.Info(logComponent).
			Int("run_id", runID+1).
			Int("matts_k", mattsK).
			Msg("Running trial")

		sessionID := fmt.Sprintf("trial_%d", runID)

		var currentQuery string
		// 自纠正模式：第 2 次起拼入上一轮轨迹
		if selfRefine && runID > 0 {
			prevTrajectories := make([]string, 0, len(results))
			for _, r := range results {
				if r.Trajectory != "" {
					prevTrajectories = append(prevTrajectories, r.Trajectory)
				}
			}
			if len(prevTrajectories) > 0 {
				prevTraj := prevTrajectories[len(prevTrajectories)-1]
				// 对齐 Python：COMBINED_MATTS_PROMPT 控制使用 refine 还是 diversity
				combinedPrompt := ceconfig.GetString("COMBINED_MATTS_PROMPT", "refine")
				var prompt string
				if combinedPrompt == "diversity" {
					prompt = selfDiversityPrompt
				} else {
					prompt = selfRefinePrompt
				}
				currentQuery = fmt.Sprintf("Previous attempt:\n%s\n\n%sQuestion: %s", prevTraj, prompt, question)
			} else {
				currentQuery = fmt.Sprintf("Question: %s", question)
			}
		} else {
			currentQuery = fmt.Sprintf("Question: %s", question)
		}

		// 对齐 Python: self_refine 模式下传入 retrieval_query=question
		// 确保记忆检索使用原始问题而非精炼后的查询
		var result *cecontext.TrajectoryResult
		var err error
		if selfRefine {
			result, err = agent.Execute(ctx, currentQuery, sessionID, cecontext.WithRetrievalQuery(question))
		} else {
			result, err = agent.Execute(ctx, currentQuery, sessionID)
		}
		if err != nil {
			logger.Error(logComponent).
				Int("run_id", runID+1).
				Err(err).
				Msg("Trial failed")
			results = append(results, TrialOutput{})
			continue
		}

		answer := result.Answer
		feedback, score := EvaluateTrial(question, answer, groundTruth)

		status := "FAILURE"
		if score == 1 {
			status = "SUCCESS"
		}
		logger.Info(logComponent).
			Str("status", status).
			Str("output", truncate(answer, 50)).
			Msg("Trial result")

		trajectory := result.Trajectory
		if trajectory == "" {
			trajectory = fmt.Sprintf("USER: %s\nASSISTANT: %s", question, answer)
		}

		results = append(results, TrialOutput{
			Trajectory: trajectory,
			Feedback:   feedback,
			Score:      score,
		})
	}

	return results, nil
}

// truncate 截断字符串到指定长度。
func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}

// isReMeFamily 判断算法是否属于 ReMe 系列（ReMe/RefCon/DivCon）。
func isReMeFamily(algo string) bool {
	return algo == "ReMe" || algo == "RefCon" || algo == "DivCon"
}

// getUseGoldLabel 获取 USE_GOLDLABEL 配置。对齐 Python config.get("USE_GOLDLABEL", False)。
func getUseGoldLabel() bool {
	return ceconfig.GetBool("USE_GOLDLABEL", false)
}

// getUseGroundTruth 获取 USE_GROUNDTRUTH 配置。对齐 Python config.get("USE_GROUNDTRUTH", False)。
func getUseGroundTruth() bool {
	return ceconfig.GetBool("USE_GROUNDTRUTH", false)
}

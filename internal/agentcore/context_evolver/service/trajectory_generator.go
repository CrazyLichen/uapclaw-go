package service

import (
	"context"
	"fmt"
	"strings"

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

	return runTrialsInner(ctx, agent, params.Question, params.GroundTruth, mattsK, selfRefine)
}

// SummarizeTrajectories 将轨迹总结为记忆。对齐 Python summarize_trajectories()。
// 注：依赖 TaskMemoryService（P6），P4 阶段先定义签名，实现留 P6。
func SummarizeTrajectories(ctx context.Context, memoryService any, userID string, params SummarizeTrajectoriesInput) (map[string]any, error) {
	return nil, fmt.Errorf("not implemented: depends on TaskMemoryService (P6)")
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
				currentQuery = fmt.Sprintf("Previous attempt:\n%s\n\n%sQuestion: %s", prevTraj, selfRefinePrompt, question)
			} else {
				currentQuery = fmt.Sprintf("Question: %s", question)
			}
		} else {
			currentQuery = fmt.Sprintf("Question: %s", question)
		}

		result, err := agent.Execute(ctx, currentQuery, sessionID)
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

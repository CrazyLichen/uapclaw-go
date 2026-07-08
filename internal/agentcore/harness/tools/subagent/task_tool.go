package subagent

import (
	"context"
	"fmt"
	"strings"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/interfaces"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/prompts/tools"
	sessioninterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/session/interfaces"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// TaskToolInput task_tool 工具输入参数。
// 对齐 Python: TaskTool.invoke() L57-121
type TaskToolInput struct {
	SubagentType    string `json:"subagent_type"`
	TaskDescription string `json:"task_description"`
}

// ──────────────────────────── 导出函数 ────────────────────────────

// NewTaskTool 创建 task_tool 工具实例。
//
// 对齐 Python: TaskTool.__init__() L31-47 + invoke() L57-121
func NewTaskTool(parentAgent interfaces.DeepAgentInterface, availableAgents, language, agentID string) tool.Tool {
	lang := "cn"
	if language == "en" {
		lang = "en"
	}
	card, _ := tools.BuildToolCard("task_tool", "task_tool", lang, map[string]string{
		"available_agents": availableAgents,
	}, agentID)

	fn := func(ctx context.Context, input TaskToolInput, opts ...tool.ToolOption) (map[string]any, error) {
		// 对齐 Python L87-91: 校验必填参数
		if input.SubagentType == "" || input.TaskDescription == "" {
			return nil, fmt.Errorf("subagent_type 和 task_description 都是必填参数")
		}

		// 提取 session
		sess := extractTaskToolSession(opts)
		if sess == nil {
			return nil, fmt.Errorf("task_tool 需要 session")
		}

		parentSessionID := sess.GetSessionID()
		subSessionID := buildSubSessionID(parentSessionID, input.SubagentType)

		logger.Info(logger.ComponentAgentCore).
			Str("event_type", "task_tool_create_subagent").
			Str("subagent_type", input.SubagentType).
			Str("parent_session_id", parentSessionID).
			Str("sub_session_id", subSessionID).
			Msg("TaskTool 创建子代理")

		// 对齐 Python L100-107: 创建子代理
		subagent, err := parentAgent.CreateSubagent(input.SubagentType, subSessionID)
		if err != nil {
			logger.Error(logger.ComponentAgentCore).
				Str("event_type", "LLM_CALL_ERROR").
				Str("subagent_type", input.SubagentType).
				Err(err).
				Msg("TaskTool 子代理创建失败")
			return nil, fmt.Errorf("子代理 %s 创建失败: %w", input.SubagentType, err)
		}

		// 对齐 Python L111-115: 调用子代理
		result, err := subagent.Invoke(ctx, map[string]any{
			"query":           input.TaskDescription,
			"conversation_id": subSessionID,
		})
		if err != nil {
			logger.Error(logger.ComponentAgentCore).
				Str("event_type", "LLM_CALL_ERROR").
				Str("subagent_type", input.SubagentType).
				Err(err).
				Msg("TaskTool 子代理执行失败")
			return nil, fmt.Errorf("子代理 %s 执行失败: %w", input.SubagentType, err)
		}

		output := ""
		if v, ok := result["output"]; ok {
			output = fmt.Sprintf("%v", v)
		}
		subAgentID := ""
		if subagentCard := subagent.Card(); subagentCard != nil {
			subAgentID = subagentCard.ID
		}

		return map[string]any{
			"output":    output,
			"agent_id":  subAgentID,
		}, nil
	}

	invokeFn, _ := tool.NewTool(fn, tool.WithToolCard(card), tool.WithToolInputParams(card.InputParams))
	return invokeFn
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// buildSubSessionID 构建子会话 ID。
//
// 对齐 Python: TaskTool._build_sub_session_id() L49-55
func buildSubSessionID(parentSessionID, subagentType string) string {
	normalized := strings.TrimSpace(subagentType)
	if normalized == "browser_agent" || normalized == "verification_agent" {
		return fmt.Sprintf("%s_sub_%s", parentSessionID, normalized)
	}
	return fmt.Sprintf("%s_sub_%s_%s", parentSessionID, normalized, generateTokenHex(8))
}

// extractTaskToolSession 从 ToolOption 提取 SessionFacade。
func extractTaskToolSession(opts []tool.ToolOption) sessioninterfaces.SessionFacade {
	callOpts := tool.NewToolCallOptions(opts...)
	session := callOpts.Session
	if session == nil {
		return nil
	}
	if sess, ok := session.(sessioninterfaces.SessionFacade); ok {
		return sess
	}
	return nil
}

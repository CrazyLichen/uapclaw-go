package rails

import (
	"context"
	"fmt"

	"github.com/uapclaw/uapclaw-go/internal/agent_teams/messager"
	"github.com/uapclaw/uapclaw-go/internal/agent_teams/tools"
	"github.com/uapclaw/uapclaw-go/internal/agent_teams/tools/database"
	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/rails/interrupt"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	saschema "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/schema"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// TeamToolApprovalRail Teammate 工具调用审批 Rail。
// Python: TeamToolApprovalRail(ConfirmInterruptRail) (rails/tool_approval_rail.py)
//
// 当 teammate 调用工具时，向 leader 发送审批请求消息，
// leader 收到消息后使用 approve_tool 工具进行回复。
//
// 审批流程：
//  1. Teammate 调用工具 → Rail 拦截
//  2. 检查 auto_confirm_config，若已配置则直接批准
//  3. 未配置自动批准：发送审批请求消息给 leader
//  4. 中断等待 leader 的审批响应
//  5. Leader 通过 approve_tool 工具回复 → resume → 批准/拒绝
type TeamToolApprovalRail struct {
	interrupt.BaseInterruptRail
	// teamName 团队名
	teamName string
	// memberName 成员名
	memberName string
	// leaderMemberName Leader 成员名
	leaderMemberName string
	// messageManager 消息管理器
	messageManager *tools.TeamMessageManager
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

const (
	// approvalLogComponent 日志组件标识
	approvalLogComponent = logger.ComponentTeam
)

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// NewTeamToolApprovalRail 创建 TeamToolApprovalRail 实例。
// Python: TeamToolApprovalRail.__init__(team_name, member_name, db, messager, leader_member_name, tool_names)
func NewTeamToolApprovalRail(
	teamName string,
	memberName string,
	db database.TeamDatabase,
	messager messager.Messager,
	leaderMemberName string,
	toolNames []string,
) *TeamToolApprovalRail {
	r := &TeamToolApprovalRail{
		teamName:         teamName,
		memberName:       memberName,
		leaderMemberName: leaderMemberName,
	}

	// Python: super().__init__(tool_names=tool_names)
	r.BaseInterruptRail = *interrupt.NewBaseInterruptRail(toolNames...)

	// Python: self.message_manager = TeamMessageManager(...)
	// 当 db 和 messager 都不为 nil 时创建 messageManager
	if db != nil && messager != nil {
		r.messageManager = tools.NewTeamMessageManager(db, teamName, memberName, messager)
	}

	// 覆盖 ResolveInterruptFn
	r.ResolveInterruptFn = r.resolveInterrupt
	return r
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// resolveInterrupt 解析工具审批中断。
// Python: TeamToolApprovalRail.resolve_interrupt(ctx, tool_call, user_input, auto_confirm_config)
//
// 流程：
//   - 首次调用（userInput nil）：检查 auto_confirm → 发消息给 leader → 中断
//   - 恢复调用（userInput 有值）：解析 ConfirmPayload → 批准/拒绝/重新中断
func (r *TeamToolApprovalRail) resolveInterrupt(
	ctx context.Context,
	_ *agentinterfaces.AgentCallbackContext,
	toolCall *llmschema.ToolCall,
	userInput any,
	autoConfirmConfig map[string]any,
) interrupt.InterruptDecision {
	// Python: if tool_call: tool_name = tool_call.name
	var toolName string
	if toolCall != nil {
		toolName = toolCall.Name
	} else {
		logger.Error(approvalLogComponent).
			Str("member_name", r.memberName).
			Msg("tool_call 未提供")
		return r.Reject("Invalid tool call")
	}

	// Python: if user_input is None: 首次调用
	if userInput == nil {
		// Python: 检查 auto_confirm
		autoConfirmKey := r.getAutoConfirmKey(toolCall)
		if interrupt.IsTruthyValue(autoConfirmConfig[autoConfirmKey]) {
			logger.Debug(approvalLogComponent).
				Str("tool_name", toolName).
				Str("member_name", r.memberName).
				Msg("工具自动批准")
			return r.Approve(nil)
		}

		// Python: 构造审批请求消息
		toolCallID := r.resolveToolCallID(toolCall)
		argsStr := "{}"
		if toolCall.Arguments != "" {
			argsStr = toolCall.Arguments
		}
		message := fmt.Sprintf(
			"Teammate tool approval request.\n"+
				"Member: %s\n"+
				"Tool: %s\n"+
				"Tool Call ID: %s\n"+
				"Arguments: %s\n"+
				"Please review and call approve_tool.\n\n",
			r.memberName, toolName, toolCallID, argsStr,
		)

		// Python: 发送消息给 leader
		logger.Info(approvalLogComponent).
			Str("tool_name", toolName).
			Str("tool_call_id", toolCallID).
			Msg("发送工具审批请求给 Leader")

		if r.messageManager != nil {
			messageID, err := r.messageManager.SendMessage(ctx, message, r.leaderMemberName, "")
			if err != nil || messageID == "" {
				logger.Error(approvalLogComponent).
					Str("tool_name", toolName).
					Err(err).
					Msg("发送审批请求失败")
				return r.Reject("Failed to send approval request to leader")
			}
		}

		// Python: 创建中断请求
		return r.Interrupt(&saschema.InterruptRequest{
			Message:        fmt.Sprintf("Awaiting leader approval for tool: %s", toolName),
			PayloadSchema:  confirmPayloadSchemaForApproval(),
			AutoConfirmKey: autoConfirmKey,
		})
	}

	// Python: 恢复调用（userInput 有值）→ 解析 ConfirmPayload
	payload, ok := r.parseApprovalInput(userInput)
	if !ok {
		// 解析失败，重新中断
		return r.Interrupt(&saschema.InterruptRequest{
			Message:        fmt.Sprintf("Invalid approval response format for tool: %s", toolName),
			PayloadSchema:  confirmPayloadSchemaForApproval(),
			AutoConfirmKey: r.getAutoConfirmKey(toolCall),
		})
	}

	// Python: if payload.approved
	if payload.Approved {
		logger.Info(approvalLogComponent).
			Str("tool_name", toolName).
			Str("member_name", r.memberName).
			Msg("工具已被 Leader 批准")
		return r.Approve(nil)
	}

	// Python: feedback = payload.feedback or "Tool call rejected by leader"
	feedback := payload.Feedback
	if feedback == "" {
		feedback = "Tool call rejected by leader"
	}
	logger.Info(approvalLogComponent).
		Str("tool_name", toolName).
		Str("member_name", r.memberName).
		Str("feedback", feedback).
		Msg("工具已被 Leader 拒绝")
	return r.Reject(feedback)
}

// getAutoConfirmKey 返回 auto_confirm 配置键。
// Python: TeamToolApprovalRail._get_auto_confirm_key(tool_call)
func (r *TeamToolApprovalRail) getAutoConfirmKey(toolCall *llmschema.ToolCall) string {
	if toolCall == nil {
		return ""
	}
	return toolCall.Name
}

// resolveToolCallID 从 ToolCall 提取 ID。
func (r *TeamToolApprovalRail) resolveToolCallID(toolCall *llmschema.ToolCall) string {
	if toolCall == nil {
		return ""
	}
	return toolCall.ID
}

// parseApprovalInput 解析用户输入为 ConfirmPayload。
// 对齐 Python ConfirmPayload.model_validate 行为。
func (r *TeamToolApprovalRail) parseApprovalInput(userInput any) (*interrupt.ConfirmPayload, bool) {
	switch input := userInput.(type) {
	case *interrupt.ConfirmPayload:
		return input, true
	case map[string]any:
		payload := &interrupt.ConfirmPayload{}
		v, hasApproved := input["approved"]
		if !hasApproved {
			return nil, false
		}
		b, ok := v.(bool)
		if !ok {
			return nil, false
		}
		payload.Approved = b
		if v, ok := input["feedback"]; ok {
			if s, ok := v.(string); ok {
				payload.Feedback = s
			}
		}
		if v, ok := input["auto_confirm"]; ok {
			if b, ok := v.(bool); ok {
				payload.AutoConfirm = b
			}
		}
		return payload, true
	default:
		return nil, false
	}
}

// confirmPayloadSchemaForApproval 返回审批请求的 ConfirmPayload JSON Schema。
func confirmPayloadSchemaForApproval() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"approved": map[string]any{
				"title": "Approved",
				"type":  "boolean",
			},
			"feedback": map[string]any{
				"default": "",
				"title":   "Feedback",
				"type":    "string",
			},
			"auto_confirm": map[string]any{
				"default": false,
				"title":   "Auto Confirm",
				"type":    "boolean",
			},
		},
		"required": []string{"approved"},
		"title":    "ConfirmPayload",
	}
}

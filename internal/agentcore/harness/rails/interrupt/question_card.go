// Package interrupt 的审批问题卡片渲染。
//
// 本文件对齐 Python interrupt_helpers.py 的 _format_question_from_interaction
// （L1801-1863）与 build_verified_permission_ask_user_question（L1332-1355）。
//
// # 为什么需要「队列验证过」的卡片
//
// 中断投影有两条路：普通投影直接信中断里的内容；权限路径要求卡片必须是
// 队列里仍处于 pending 的那一张（buildVerified 会校验 request_id 与队列卡片的
// tool_call_id 一致）。后者是唯一能保证「用户看到的审批就是当前真在等待的审批」
// 的路径——否则中断快照可能已经过期，用户批准了一个已失效的请求。
package interrupt

import (
	"fmt"
	"strings"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/rails/planapproval"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/rails/redaction"
)

// ──────────────────────────── 常量 ────────────────────────────

const (
	// permissionInterruptSource 权限类中断的来源标记
	permissionInterruptSource = "permission_interrupt"
	// genericConfirmMessage 通用的无信息确认问句
	//
	// Python: {"", "Please approve or reject?"}
	genericConfirmMessage = "Please approve or reject?"
	// fallbackPermissionMessage 无文案时按工具名兜底的中文提示
	//
	// Python: f"工具 `{tool_name}` 需要授权才能执行"
	fallbackPermissionMessageFormat = "工具 `%s` 需要授权才能执行"
	// permissionApprovalHeaderFormat 权限审批卡片标题
	//
	// Python: f"权限审批: {tool_name}" / "权限审批"
	permissionApprovalHeaderFormat = "权限审批: %s"
	// permissionApprovalHeaderDefault 无工具名时的权限审批标题
	permissionApprovalHeaderDefault = "权限审批"
	// confirmApprovalHeaderFormat 操作确认卡片标题
	//
	// Python: f"操作确认: {tool_name}" / "操作确认"
	confirmApprovalHeaderFormat = "操作确认: %s"
	// confirmApprovalHeaderDefault 无工具名时的操作确认标题
	confirmApprovalHeaderDefault = "操作确认"
	// planApprovalHeader 计划审批卡片标题
	//
	// Python: header = "Exit Plan and Execute"
	planApprovalHeader = "Exit Plan and Execute"
)

// ──────────────────────────── 结构体 ────────────────────────────

// QuestionCardInput 渲染一张审批问题卡片所需的输入。
//
// Python: _format_question_from_interaction(payload, value_obj, *,
// source, invocation_record) 的关键字参数
type QuestionCardInput struct {
	// Interaction 审批交互（含 metadata 与 id）
	Interaction map[string]any
	// Value 中断值对象
	Value any
	// Source 来源标记（permission_interrupt / confirm_interrupt）
	Source string
	// ToolName 工具名
	ToolName string
	// Message 中断文案
	Message string
	// InvocationID 队列卡片的调用标识，空串表示无记录
	InvocationID string
}

// ──────────────────────────── 导出函数 ────────────────────────────

// FormatQuestionFromInteraction 在队列身份校验之后渲染一条问题。
//
// Python: _format_question_from_interaction(payload, value_obj, *,
// source, invocation_record) (interrupt_helpers.py L1801-1863)
//
// 五段：
//  1. 取工具名/文案/参数，文案缺失时按来源补默认文案
//  2. 计划审批：专用标题 + 剥离文字选项
//  3. 确认类：操作确认标题
//  4. 权限类：权限审批标题（可被 ask_title 覆盖）
//  5. 组装 options；权限类额外附带 card_id / reviewer_metadata / tool_payload
func FormatQuestionFromInteraction(input QuestionCardInput) map[string]any {
	toolName := input.ToolName
	message := input.Message

	reviewerMetadata := reviewerUIMetadataFromInteraction(input.Interaction, input.Value)
	needsMessage := message == "" ||
		(input.Source == confirmInterruptSource && isGenericConfirmMessage(message))

	if toolName != "" && needsMessage {
		if input.Source == confirmInterruptSource {
			var toolArgs map[string]any
			if typed, ok := input.Value.(map[string]any); ok {
				if args, ok := typed["tool_args"].(map[string]any); ok {
					toolArgs = args
				}
			}
			message = planapproval.BuildConfirmInterruptMessage(toolName, toolArgs)
		} else if message == "" {
			message = fmt.Sprintf(fallbackPermissionMessageFormat, toolName)
		}
	}

	var header string
	var question string

	if options := planApprovalInterruptOptions(input.Source, toolName, message); options != nil {
		header = planApprovalHeader
		question = strings.TrimRight(planapproval.StripInlinePlanApprovalChoices(message), " \t\r\n")
	} else if input.Source == confirmInterruptSource {
		if toolName != "" {
			header = fmt.Sprintf(confirmApprovalHeaderFormat, toolName)
		} else {
			header = confirmApprovalHeaderDefault
		}
		question = message
	} else {
		metadata := extractInterruptMetadata(input.Value)
		askTitle := strings.TrimSpace(anyToString(metadata["ask_title"]))
		switch {
		case askTitle != "":
			header = askTitle
		case toolName != "":
			header = fmt.Sprintf(permissionApprovalHeaderFormat, toolName)
		default:
			header = permissionApprovalHeaderDefault
		}
		question = message
		if IsEnterprise() {
			question = stripPermanentRememberHint(question)
		}
	}

	questionData := map[string]any{
		"question":     question,
		"header":       header,
		"options":      questionOptionsFromUIOptions(input.Value, input.Source, toolName, message),
		"multi_select": false,
	}

	if input.Source == permissionInterruptSource {
		if IsEnterprise() {
			metadata := extractInteractionMetadata(input.Interaction)
			for key, value := range extractInterruptMetadata(input.Value) {
				metadata[key] = value
			}
			if matchedRule, ok := metadata["matched_rule"].(string); ok && strings.TrimSpace(matchedRule) != "" {
				questionData["matched_rule"] = redaction.RedactSecretValuesPreserveFormatNoLimit(matchedRule)
			}
		}
		if input.InvocationID != "" {
			questionData["card_id"] = input.InvocationID
		}
		if len(reviewerMetadata) > 0 {
			questionData["reviewer_metadata"] = reviewerMetadata
		}
		if toolPayload, ok := input.Value.(map[string]any); ok {
			if _, hasArgs := toolPayload["tool_args"]; hasArgs {
				questionData["tool_payload"] = redaction.SanitizePermissionUIPayload(toolPayload["tool_args"])
			}
		}
	}
	return questionData
}

// BuildVerifiedPermissionAskUserQuestion 渲染一张队列验证过的权限卡片。
//
// Python: build_verified_permission_ask_user_question(interaction, card)
// (interrupt_helpers.py L1332-1355)
//
// 校验 request_id 必须等于队列卡片的 tool_call_id：不一致说明中断快照与队列
// 对不上（用户看到的不是当前真在等待的那一条），返回 nil 让调用方撤销快照。
func BuildVerifiedPermissionAskUserQuestion(
	interaction map[string]any,
	cardToolCallID string,
	cardInvocationID string,
) map[string]any {
	requestID, valueObj := extractInteractionParts(interaction)
	if requestID != cardToolCallID {
		return nil
	}

	toolName, message, _ := readInterruptFields(valueObj)
	question := FormatQuestionFromInteraction(QuestionCardInput{
		Interaction:  interaction,
		Value:        valueObj,
		Source:       permissionInterruptSource,
		ToolName:     toolName,
		Message:      message,
		InvocationID: cardInvocationID,
	})

	payload := map[string]any{
		"event_type": "chat.ask_user_question",
		"request_id": requestID,
		"questions":  []any{question},
		"source":     permissionInterruptSource,
	}
	for key, value := range interruptIdentity(valueObj) {
		payload[key] = value
	}
	return payload
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// isGenericConfirmMessage 判断文案是否为无信息的通用确认问句。
//
// Python: message.strip() in {"", "Please approve or reject?"}
func isGenericConfirmMessage(message string) bool {
	normalized := strings.TrimSpace(message)
	return normalized == "" || normalized == genericConfirmMessage
}

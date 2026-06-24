package ability

import (
	"fmt"

	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	"github.com/uapclaw/uapclaw-go/internal/common/exception"
)

// ──────────────────────────── 结构体 ────────────────────────────

// AddAbilityResult 添加能力的返回结果。
//
// 对应 Python: AddAbilityResult
type AddAbilityResult struct {
	// Name 能力名称
	Name string
	// Added 是否成功添加
	Added bool
	// Reason 未添加的原因（如 "duplicate_tool"、"added_tool"）
	Reason string
}

// AbilityExecutionError 能力执行统一异常，嵌入 BaseError 并关联 ToolMessage。
//
// 对应 Python: AbilityExecutionError
type AbilityExecutionError struct {
	*exception.BaseError
	// ToolMessage 关联的工具返回消息
	ToolMessage *llmschema.ToolMessage
}

// ExecuteResult 单个工具调用的执行结果。
type ExecuteResult struct {
	// Result 执行结果
	Result any
	// ToolMsg 返回给 LLM 的 ToolMessage
	ToolMsg *llmschema.ToolMessage
	// Err 执行错误（如有）
	Err error
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// NewAbilityExecutionError 创建能力执行错误。
//
// 对应 Python: AbilityExecutionError(status=..., msg=..., tool_message=...)
func NewAbilityExecutionError(
	status exception.StatusCode,
	toolCallID string,
	msg string,
	opts ...exception.ErrorOption,
) *AbilityExecutionError {
	allOpts := append([]exception.ErrorOption{exception.WithMsg(msg)}, opts...)
	return &AbilityExecutionError{
		BaseError:   exception.NewBaseError(status, allOpts...),
		ToolMessage: llmschema.NewToolMessage(toolCallID, msg),
	}
}

// BuildToolMessageContent 从执行结果中提取 ToolMessage 的 content 字段。
//
// 提取逻辑（对齐 Python _build_tool_message_content）：
//  1. 结果有 data.content 字段 → 返回 content
//  2. 结果 success=false 且有 error → 返回 error
//  3. 其他 → 字符串化结果
func BuildToolMessageContent(result any) string {
	if m, ok := result.(map[string]any); ok {
		if data, ok := m["data"].(map[string]any); ok {
			if content, ok := data["content"]; ok {
				s := fmt.Sprintf("%v", content)
				if s != "" {
					return s
				}
			}
		}
		if success, ok := m["success"].(bool); ok && !success {
			if errVal, ok := m["error"]; ok {
				return fmt.Sprintf("%v", errVal)
			}
		}
	}
	return fmt.Sprintf("%v", result)
}

// ──────────────────────────── 非导出函数 ────────────────────────────

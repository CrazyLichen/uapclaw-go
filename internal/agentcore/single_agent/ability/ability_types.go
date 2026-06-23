package ability

import (
	"context"
	"fmt"

	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/rail"
	"github.com/uapclaw/uapclaw-go/internal/common/exception"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ToolRail 工具调用生命周期钩子接口（3.13 只定义，6.4-6.10 实现）。
type ToolRail interface {
	// BeforeToolCall 工具调用前触发
	BeforeToolCall(ctx context.Context, callCtx *ToolCallContext) (*ToolCallContext, error)
	// AfterToolCall 工具调用后触发
	AfterToolCall(ctx context.Context, callCtx *ToolCallContext, result *ToolCallResult) (*ToolCallResult, error)
	// OnToolException 工具调用异常时触发
	OnToolException(ctx context.Context, callCtx *ToolCallContext, err error) error
}

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

// ToolCallContext 工具调用上下文（6.5 回填）。
type ToolCallContext struct {
	// ToolCall 工具调用信息
	ToolCall *llmschema.ToolCall
	// ToolName 工具名称
	ToolName string
	// ToolArgs 工具参数
	ToolArgs map[string]any
	// ToolResult 工具执行结果
	ToolResult any
	// ToolMsg 工具返回消息
	ToolMsg *llmschema.ToolMessage
	// callbackCtx 所属 AgentCallbackContext（6.5 回填）
	// 用于在 ToolRail 钩子中访问 retry/force_finish/steering 等控制机制
	callbackCtx *rail.AgentCallbackContext
	// ⤵️ 预留字段：force_finish / steering_queue / skip_tool
}

// ToolCallResult 工具调用结果（预留）。
type ToolCallResult struct {
	// Result 执行结果
	Result any
	// ToolMsg 返回给 LLM 的 ToolMessage
	ToolMsg *llmschema.ToolMessage
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

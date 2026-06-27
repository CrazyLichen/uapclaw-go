package ability

import (
	"encoding/json"
	"errors"
	"fmt"
	"reflect"

	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/state"
	saschema "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/schema"
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
	// Result 执行结果。
	// 正常返回: map[string]any 或具体类型
	// 工具中断: *saschema.ToolInterruptException
	// 工作流中断: *workflow.WorkflowOutput (state=INPUT_REQUIRED)
	// 执行错误: *AbilityExecutionError
	Result any
	// ToolMsg 返回给 LLM 的 ToolMessage
	ToolMsg *llmschema.ToolMessage
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// InterruptAutoConfirmKey 中断自动确认状态键。
// 对齐 Python: openjiuwen/core/single_agent/interrupt/state.py INTERRUPT_AUTO_CONFIRM_KEY
var InterruptAutoConfirmKey = state.StringKey("__interrupt_auto_confirm__")

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
//
//	路径 1: map[string]any — 按 key 提取
//	  1a. data.content 提取
//	  1b. success=false + error 提取
//	  1c. structToMap 的 {"result": v} 包装 — 解包后递归处理
//	  1d. 普通 map — JSON 序列化
//	路径 2: 反射提取（对齐 Python getattr(result, "data", None)）
//	路径 3: 最终 fallback — fmt.Sprintf("%v", result)
func BuildToolMessageContent(result any) string {
	// 路径 1：map[string]any — 按 key 提取
	if m, ok := result.(map[string]any); ok {
		// 1a. data.content 提取
		if data, ok := m["data"].(map[string]any); ok {
			if content, ok := data["content"]; ok {
				if s := fmt.Sprintf("%v", content); s != "" {
					return s
				}
			}
		}
		// 1b. success=false + error 提取
		if success, ok := m["success"].(bool); ok && !success {
			if errVal, ok := m["error"]; ok {
				return fmt.Sprintf("%v", errVal)
			}
		}
		// 1c. structToMap 的 {"result": v} 包装 — 解包后递归处理
		// 对齐 Python: LocalFunction 返回 string 时，Go 包装为 {"result": v}，
		// 需解包后递归，使 "search..." 走到路径 3 的 fmt.Sprintf("%v", result) 返回原值。
		if v, ok := m["result"]; ok && len(m) == 1 {
			return BuildToolMessageContent(v)
		}
		// 1d. 普通 map — JSON 序列化（对齐 Python str(dict)）
		if jsonBytes, err := json.Marshal(m); err == nil {
			return string(jsonBytes)
		}
	}

	// 路径 2：反射提取（对齐 Python getattr(result, "data", None)）
	v := reflect.ValueOf(result)
	if v.Kind() == 22 /* reflect.Ptr 指针类型 */ {
		v = v.Elem()
	}
	if v.Kind() == reflect.Struct {
		if f := v.FieldByName("Data"); f.IsValid() {
			if dataMap, ok := f.Interface().(map[string]any); ok {
				if content, ok := dataMap["content"]; ok {
					if s := fmt.Sprintf("%v", content); s != "" {
						return s
					}
				}
			}
		}
		if f := v.FieldByName("Success"); f.IsValid() && f.Kind() == reflect.Bool && !f.Bool() {
			if ef := v.FieldByName("Error"); ef.IsValid() {
				return fmt.Sprintf("%v", ef.Interface())
			}
		}
	}

	// 路径 3：最终 fallback
	return fmt.Sprintf("%v", result)
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// errorToExecuteResult 将 error 转换为 ExecuteResult。
// 用于降级路径（cbc==nil）和 railedExecuteSingleToolCall 的异常处理。
//
// 转换规则：
//   - ToolInterruptException → Result=tie, ToolMsg=nil
//   - AbilityExecutionError  → Result=err, ToolMsg=aee.ToolMessage
//   - 其他 error             → Result=err, ToolMsg=兜底构建
func errorToExecuteResult(err error, toolCallID string) ExecuteResult {
	var tie *saschema.ToolInterruptException
	if errors.As(err, &tie) {
		return ExecuteResult{Result: tie}
	}
	var toolMsg *llmschema.ToolMessage
	var execErr *AbilityExecutionError
	if errors.As(err, &execErr) {
		toolMsg = execErr.ToolMessage
	}
	if toolMsg == nil {
		toolMsg = llmschema.NewToolMessage(toolCallID, err.Error())
	}
	return ExecuteResult{Result: err, ToolMsg: toolMsg}
}

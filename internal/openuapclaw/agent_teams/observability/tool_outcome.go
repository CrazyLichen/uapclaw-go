package observability

import (
	"fmt"
	"strings"
)

// tool_outcome.go 工具调用真实结局的统一读取。
// Python: extensions/observability/tool_outcome.py（Go 落位 agent_teams/observability，
// 与 semconv 同包——extensions 层独立包位偏差登记 spec §4）。
// 工具调用以两种不相关方式失败，span 必须以同一种方式记录两者，否则轨迹与
// 模型看到的对话不一致：
//   - 抛异常——ability manager 捕获后仍给模型 ToolMessage，span 也需要输出；
//   - 返回 success=false——内置工具（bash/grep/glob/read_file/write_file）的
//     常见形态，什么都不抛，仅从异常推导状态会把失败调用记成 OK。
// harness rail（权威工具 span 属主）与低层 callback handler（team 模式属主）
// 都经本模块读结局，两条路径不会漂移。

// ToolOutcomeDefaultFailureReason 默认失败原因。
// Python: _DEFAULT_FAILURE_REASON = "tool reported failure"。
const ToolOutcomeDefaultFailureReason = "tool reported failure"

// toolOutcomeExceptionResultPrefix 异常结果前缀。
// Python: _EXCEPTION_RESULT_PREFIX = "Ability execution error: "。
const toolOutcomeExceptionResultPrefix = "Ability execution error: "

// ToolFailureReason 返回工具在自身结果里报告的失败。
// 只有显式 success=false 才算；结果不含 success 字段（workflow 输出、裸
// MCP payload、纯字符串）不猜测。Python: tool_failure_reason（tool_outcome.py:36-59）。
func ToolFailureReason(output any) string {
	m, ok := output.(map[string]any)
	if !ok {
		return ""
	}
	if success, present := m["success"]; !present {
		return ""
	} else if b, isBool := success.(bool); !isBool || b {
		return ""
	}
	reason := strings.TrimSpace(valueToString(m["error"]))
	if reason == "" {
		return ToolOutcomeDefaultFailureReason
	}
	return reason
}

// ToolResultForException 重建抛异常调用时模型收到的工具结果。
// Python: tool_result_for_exception（tool_outcome.py:61-80）——AbilityManager
// 把抛出的调用转成 ToolMessage 再给模型（优先异常自带的 tool_message content），
// 这里镜像同款，使记录的输出与对话内容相等而非仅相似。
func ToolResultForException(err error) string {
	if err == nil {
		return ""
	}
	if tm, ok := err.(interface{ GetToolMessage() any }); ok {
		if msg := tm.GetToolMessage(); msg != nil {
			if m, ok := msg.(interface{ GetContent() any }); ok {
				if content := m.GetContent(); content != nil {
					return valueToString(content)
				}
			}
		}
	}
	return fmt.Sprintf("%s%v", toolOutcomeExceptionResultPrefix, err)
}

// valueToString 任意值的字符串化。
func valueToString(v any) string {
	if v == nil {
		return ""
	}
	if s, ok := v.(string); ok {
		return s
	}
	return fmt.Sprintf("%v", v)
}

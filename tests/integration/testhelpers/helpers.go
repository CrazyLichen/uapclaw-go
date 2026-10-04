package testhelpers

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	sessioninteraction "github.com/uapclaw/uapclaw-go/internal/agentcore/session/interaction"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// AssertInterruptResult 验证 result_type=="interrupt" 并提取中断信息。
// 对齐 Python assert_interrupt_result(result, expected_count)。
// expectedCount < 0 时跳过数量断言（并发工具数量不确定的场景）。
func AssertInterruptResult(t testing.TB, result map[string]any, expectedCount int) (interruptIDs []string, stateList []any) {
	t.Helper()
	require.NotNil(t, result, "结果不应为 nil")

	resultType, ok := result["result_type"].(string)
	require.True(t, ok, "应包含 result_type 字段")
	assert.Equal(t, "interrupt", resultType, "result_type 应为 interrupt")

	ids, ok := result["interrupt_ids"]
	if ok && ids != nil {
		interruptIDs, _ = ids.([]string)
	}
	if expectedCount >= 0 {
		assert.Equal(t, expectedCount, len(interruptIDs), "interrupt_ids 数量")
	}

	state, ok := result["state"]
	if ok && state != nil {
		stateList, _ = state.([]any)
	}
	if expectedCount >= 0 {
		assert.Equal(t, expectedCount, len(stateList), "state 列表数量")
	}

	return interruptIDs, stateList
}

// AssertAnswerResult 验证 result_type=="answer"。
// 对齐 Python assert_answer_result(result)。
func AssertAnswerResult(t testing.TB, result map[string]any) {
	t.Helper()
	require.NotNil(t, result, "结果不应为 nil")

	resultType, ok := result["result_type"].(string)
	require.True(t, ok, "应包含 result_type 字段")
	assert.Equal(t, "answer", resultType, "result_type 应为 answer")
}

// ConfirmInterrupt 构造确认中断的 InteractiveInput。
// 对齐 Python confirm_interrupt(tool_call_id, auto_confirm=False)。
func ConfirmInterrupt(toolCallID string, autoConfirm bool) *sessioninteraction.InteractiveInput {
	return &sessioninteraction.InteractiveInput{
		UserInputs: map[string]any{
			toolCallID: map[string]any{
				"approved":     true,
				"feedback":     "Confirm",
				"auto_confirm": autoConfirm,
			},
		},
	}
}

// RejectInterrupt 构造拒绝中断的 InteractiveInput。
// 对齐 Python reject_interrupt(tool_call_id, feedback="Reject")。
func RejectInterrupt(toolCallID string, feedback string) *sessioninteraction.InteractiveInput {
	if feedback == "" {
		feedback = "Reject"
	}
	return &sessioninteraction.InteractiveInput{
		UserInputs: map[string]any{
			toolCallID: map[string]any{
				"approved": false,
				"feedback": feedback,
			},
		},
	}
}

// GetToolNameFromState 从 state payload 提取 tool_name。
// 对齐 Python get_tool_name_from_state(state_item)。
func GetToolNameFromState(stateItem any) string {
	if stateItem == nil {
		return ""
	}
	// 尝试 OutputSchema 格式
	if schema, ok := stateItem.(interface{ GetPayload() any }); ok {
		payload := schema.GetPayload()
		return extractToolNameFromPayload(payload)
	}
	// 尝试 map 格式
	if m, ok := stateItem.(map[string]any); ok {
		if payload, ok := m["payload"]; ok {
			return extractToolNameFromPayload(payload)
		}
	}
	return ""
}

// GetFilePathFromState 从 state payload 提取 filepath。
// 对齐 Python get_filepath_from_state(state_item)。
func GetFilePathFromState(stateItem any) string {
	if stateItem == nil {
		return ""
	}
	var payload any
	if schema, ok := stateItem.(interface{ GetPayload() any }); ok {
		payload = schema.GetPayload()
	} else if m, ok := stateItem.(map[string]any); ok {
		payload = m["payload"]
	}
	if payload == nil {
		return ""
	}
	return extractFilePathFromPayload(payload)
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// extractToolNameFromPayload 从 payload 中提取 tool_name。
func extractToolNameFromPayload(payload any) string {
	if payload == nil {
		return ""
	}
	// 尝试 InteractionOutput 格式
	if output, ok := payload.(*sessioninteraction.InteractionOutput); ok {
		if output.Value != nil {
			if m, ok := output.Value.(map[string]any); ok {
				if name, ok := m["tool_name"].(string); ok {
					return name
				}
			}
		}
	}
	// 尝试 map 格式
	if m, ok := payload.(map[string]any); ok {
		if v, ok := m["tool_name"]; ok {
			if name, ok := v.(string); ok {
				return name
			}
		}
		// 嵌套 value
		if v, ok := m["value"]; ok {
			return extractToolNameFromPayload(v)
		}
	}
	return ""
}

// extractFilePathFromPayload 从 payload 中提取 filepath。
func extractFilePathFromPayload(payload any) string {
	if payload == nil {
		return ""
	}
	if m, ok := payload.(map[string]any); ok {
		if v, ok := m["tool_args"]; ok {
			if args, ok := v.(map[string]any); ok {
				if fp, ok := args["filepath"].(string); ok {
					return fp
				}
			}
		}
		if v, ok := m["value"]; ok {
			return extractFilePathFromPayload(v)
		}
	}
	return ""
}

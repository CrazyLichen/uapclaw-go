package security

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	saschema "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/schema"
)

// ──────────────────────────── 结构体 ────────────────────────────

// mockBaseSecurityRail 用于测试的 BaseSecurityRail 子类
type mockBaseSecurityRail struct {
	BaseSecurityRail
	decision SecurityDecision
	err      error
}

func newMockBaseSecurityRail(decision SecurityDecision) *mockBaseSecurityRail {
	return &mockBaseSecurityRail{
		BaseSecurityRail: *NewBaseSecurityRail(
			WithSupportedEvents(agentinterfaces.CallbackBeforeToolCall, agentinterfaces.CallbackBeforeModelCall),
		),
		decision: decision,
	}
}

func (m *mockBaseSecurityRail) runSecurityCheck(_ context.Context, _ *SecurityCheckContext) (SecurityDecision, error) {
	return m.decision, m.err
}

// ──────────────────────────── 导出函数 ────────────────────────────

// ──────────────────────────── 非导出函数 ────────────────────────────

// TestNewBaseSecurityRail 测试创建 BaseSecurityRail
func TestNewBaseSecurityRail(t *testing.T) {
	r := NewBaseSecurityRail(
		WithSupportedEvents(agentinterfaces.CallbackBeforeModelCall),
		WithSecurityToolNames("bash"),
	)
	assert.Equal(t, baseSecurityRailPriority, r.Priority())
	assert.True(t, r.supportedEvents[agentinterfaces.CallbackBeforeModelCall])
	assert.Contains(t, r.toolNames, "bash")
}

// TestSecurityAllow 测试允许决策
func TestSecurityAllow(t *testing.T) {
	r := NewBaseSecurityRail()
	allow := r.Allow("")
	assert.NotNil(t, allow)
	assert.Equal(t, "", allow.NewArgs)

	allowWithArgs := r.Allow(`{"command": "ls"}`)
	assert.Equal(t, `{"command": "ls"}`, allowWithArgs.NewArgs)
}

// TestSecurityApprove 测试批准决策（Allow 别名）
func TestSecurityApprove(t *testing.T) {
	r := NewBaseSecurityRail()
	approve := r.Approve("")
	assert.NotNil(t, approve)
	assert.Equal(t, "", approve.NewArgs)
}

// TestSecurityReject 测试拒绝决策
func TestSecurityReject(t *testing.T) {
	r := NewBaseSecurityRail()
	reject := r.Reject("危险操作", "已拒绝", nil)
	assert.Equal(t, "危险操作", reject.Message)
	assert.Equal(t, "已拒绝", reject.Result)

	// 对齐 Python: tool_result 参数兼容
	rejectWithMsg := r.Reject("消息", nil, llmschema.NewToolMessage("tc-1", "拒绝"))
	assert.Equal(t, "消息", rejectWithMsg.Message)
	assert.NotNil(t, rejectWithMsg.ToolMessage)
}

// TestSecurityInterrupt 测试中断决策
func TestSecurityInterrupt(t *testing.T) {
	r := NewBaseSecurityRail()
	req := &saschema.InterruptRequest{Message: "确认执行?"}
	interrupt := r.Interrupt(req, "tool-123")
	assert.Equal(t, req, interrupt.Request)
	assert.Equal(t, "tool-123", interrupt.SubjectID)
}

// TestSecurityAlert 测试告警决策
func TestSecurityAlert(t *testing.T) {
	r := NewBaseSecurityRail()
	alert := r.Alert("可疑操作", SecurityAlertLevelWarning, "security", "popup")
	assert.Equal(t, "可疑操作", alert.Message)
	assert.Equal(t, SecurityAlertLevelWarning, alert.Level)
	assert.Equal(t, "security", alert.AlertType)
	assert.Equal(t, "popup", alert.DisplayMode)
}

// TestSecurityAlertLevel_String 测试告警级别字符串表示
func TestSecurityAlertLevel_String(t *testing.T) {
	assert.Equal(t, "info", SecurityAlertLevelInfo.String())
	assert.Equal(t, "warning", SecurityAlertLevelWarning.String())
	assert.Equal(t, "error", SecurityAlertLevelError.String())
	assert.Equal(t, "critical", SecurityAlertLevelCritical.String())
}

// TestBuildForceFinishResult 测试构建 forceFinish 结果
// 对齐 Python: BaseSecurityRail._build_force_finish_result
func TestBuildForceFinishResult(t *testing.T) {
	r := NewBaseSecurityRail()

	// 对齐 Python: isinstance(decision.result, dict) → 直接返回
	reject := r.Reject("", map[string]any{"status": "denied"}, nil)
	result := r.buildForceFinishResult(reject)
	assert.Equal(t, map[string]any{"status": "denied"}, result)

	// 对齐 Python: {"output": message, "result_type": "error"}
	rejectNoResult := r.Reject("操作被拒绝", nil, nil)
	result2 := r.buildForceFinishResult(rejectNoResult)
	assert.Equal(t, "操作被拒绝", result2["output"])
	assert.Equal(t, "error", result2["result_type"])

	// 无消息无结果 → 默认消息
	rejectEmpty := r.Reject("", nil, nil)
	result3 := r.buildForceFinishResult(rejectEmpty)
	assert.Equal(t, "Rejected by security rail.", result3["output"])
}

// TestHandleInterruptResume_首次调用 测试中断恢复首次调用
// 对齐 Python: BaseSecurityRail._handle_interrupt_resume — user_input is None → return None
func TestHandleInterruptResume_首次调用(t *testing.T) {
	r := NewBaseSecurityRail()
	ctx := &SecurityCheckContext{
		UserInput:         nil,
		AutoConfirmConfig: nil,
	}
	decision := r.handleInterruptResume(ctx, "bash")
	assert.Nil(t, decision, "首次调用应返回 nil，走 Interrupt 流程")
}

// TestHandleInterruptResume_autoConfirm 测试中断恢复已自动确认
// 对齐 Python: BaseSecurityRail._handle_interrupt_resume — auto_confirm → Allow
func TestHandleInterruptResume_autoConfirm(t *testing.T) {
	r := NewBaseSecurityRail()
	ctx := &SecurityCheckContext{
		UserInput: nil,
		AutoConfirmConfig: map[string]any{
			"bash": true,
		},
	}
	decision := r.handleInterruptResume(ctx, "bash")
	require.NotNil(t, decision)
	_, ok := decision.(*SecurityAllow)
	require.True(t, ok, "auto_confirm 应返回 SecurityAllow")
}

// TestHandleInterruptResume_用户批准 测试中断恢复用户批准
// 对齐 Python: BaseSecurityRail._handle_interrupt_resume — approved=True → Allow
func TestHandleInterruptResume_用户批准(t *testing.T) {
	r := NewBaseSecurityRail()
	ctx := &SecurityCheckContext{
		UserInput: map[string]any{
			"approved":     true,
			"auto_confirm": false,
			"feedback":     "",
		},
		AutoConfirmConfig: nil,
	}
	decision := r.handleInterruptResume(ctx, "bash")
	require.NotNil(t, decision)
	_, ok := decision.(*SecurityAllow)
	require.True(t, ok, "用户批准应返回 SecurityAllow")
}

// TestHandleInterruptResume_用户批准加AutoConfirm 测试中断恢复用户批准+记住
// 对齐 Python: BaseSecurityRail._handle_interrupt_resume — approved=True + auto_confirm=True → store + Allow
func TestHandleInterruptResume_用户批准加AutoConfirm(t *testing.T) {
	r := NewBaseSecurityRail()
	ctx := &SecurityCheckContext{
		UserInput: map[string]any{
			"approved":     true,
			"auto_confirm": true,
		},
		AutoConfirmConfig: nil,
		// 注意：storeAutoConfirm 需要 CallbackCtx.Session()，此测试中为 nil，不会 panic
	}
	decision := r.handleInterruptResume(ctx, "bash")
	require.NotNil(t, decision)
	_, ok := decision.(*SecurityAllow)
	require.True(t, ok)
}

// TestHandleInterruptResume_用户拒绝 测试中断恢复用户拒绝
// 对齐 Python: BaseSecurityRail._handle_interrupt_resume — approved=False → Reject
func TestHandleInterruptResume_用户拒绝(t *testing.T) {
	r := NewBaseSecurityRail()
	ctx := &SecurityCheckContext{
		UserInput: map[string]any{
			"approved": false,
			"feedback": "太危险",
		},
		AutoConfirmConfig: nil,
	}
	decision := r.handleInterruptResume(ctx, "bash")
	require.NotNil(t, decision)
	_, ok := decision.(*SecurityReject)
	require.True(t, ok, "用户拒绝应返回 SecurityReject")
}

// TestIsAutoConfirmed 测试自动确认检查
// 对齐 Python: BaseSecurityRail._is_auto_confirmed
func TestIsAutoConfirmed(t *testing.T) {
	r := NewBaseSecurityRail()

	assert.False(t, r.isAutoConfirmed(nil, "bash"), "nil config → false")
	assert.False(t, r.isAutoConfirmed(map[string]any{}, "bash"), "空 config → false")
	assert.False(t, r.isAutoConfirmed(map[string]any{"bash": false}, "bash"), "false 值 → false")
	assert.True(t, r.isAutoConfirmed(map[string]any{"bash": true}, "bash"), "true 值 → true")
	assert.False(t, r.isAutoConfirmed(map[string]any{}, ""), "空 key → false")
	assert.False(t, r.isAutoConfirmed(nil, ""), "nil config + 空 key → false")
}

// TestAddTool 测试添加工具名
func TestAddTool(t *testing.T) {
	r := NewBaseSecurityRail()
	r.AddTool("bash")
	assert.Contains(t, r.toolNames, "bash")
	r.AddTools([]string{"read_file", "write_file"})
	assert.Contains(t, r.toolNames, "read_file")
	assert.Contains(t, r.toolNames, "write_file")
}

// TestSecurityDecisionInterface 测试决策类型实现 SecurityDecision 接口
func TestSecurityDecisionInterface(t *testing.T) {
	var _ SecurityDecision = &SecurityAllow{}
	var _ SecurityDecision = &SecurityReject{}
	var _ SecurityDecision = &SecurityInterrupt{}
	var _ SecurityDecision = &SecurityAlert{}
}

// TestIsSecurityTruthy 测试宽松真值判断
func TestIsSecurityTruthy(t *testing.T) {
	assert.False(t, isSecurityTruthy(nil))
	assert.False(t, isSecurityTruthy(false))
	assert.True(t, isSecurityTruthy(true))
	assert.False(t, isSecurityTruthy("yes"))  // 非 bool → false（对齐 Python bool()，仅 bool 类型）
	assert.False(t, isSecurityTruthy(1))      // 非 bool → false
	assert.False(t, isSecurityTruthy("true")) // 非 bool → false
}

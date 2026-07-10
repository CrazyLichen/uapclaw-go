package interrupt

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"

	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	saschema "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/schema"
)

// ──────────────────────────── 构造函数 ────────────────────────────

// TestNewConfirmInterruptRail 验证构造函数和优先级
func TestNewConfirmInterruptRail(t *testing.T) {
	r := NewConfirmInterruptRail("write_file", "edit_file")
	assert.Equal(t, 90, r.Priority())
	assert.Contains(t, r.toolNames, "write_file")
	assert.Contains(t, r.toolNames, "edit_file")
	assert.Equal(t, "请确认或拒绝?", r.request.Message)
}

// ──────────────────────────── resolveInterrupt ────────────────────────────

// TestConfirmInterruptRail_resolveInterrupt_无输入无AutoConfirm 验证无输入无auto_confirm → InterruptResult
func TestConfirmInterruptRail_resolveInterrupt_无输入无AutoConfirm(t *testing.T) {
	r := NewConfirmInterruptRail("write_file")
	toolCall := &llmschema.ToolCall{ID: "tc1", Name: "write_file", Arguments: `{}`}

	decision := r.resolveConfirmInterrupt(context.TODO(), nil, toolCall, nil, nil)
	assert.IsType(t, &InterruptResult{}, decision)
	interruptResult := decision.(*InterruptResult)
	assert.Equal(t, "请确认或拒绝?", interruptResult.Request.Message)
	assert.Equal(t, "write_file", interruptResult.Request.AutoConfirmKey)
}

// TestConfirmInterruptRail_resolveInterrupt_无输入有AutoConfirm 验证无输入有auto_confirm → ApproveResult
func TestConfirmInterruptRail_resolveInterrupt_无输入有AutoConfirm(t *testing.T) {
	r := NewConfirmInterruptRail("write_file")
	toolCall := &llmschema.ToolCall{ID: "tc1", Name: "write_file", Arguments: `{}`}
	autoConfirmConfig := map[string]any{"write_file": true}

	decision := r.resolveConfirmInterrupt(context.TODO(), nil, toolCall, nil, autoConfirmConfig)
	assert.IsType(t, &ApproveResult{}, decision)
}

// TestConfirmInterruptRail_resolveInterrupt_批准 验证 approved → ApproveResult
func TestConfirmInterruptRail_resolveInterrupt_批准(t *testing.T) {
	r := NewConfirmInterruptRail("write_file")
	toolCall := &llmschema.ToolCall{ID: "tc1", Name: "write_file", Arguments: `{}`}
	userInput := &ConfirmPayload{Approved: true}

	decision := r.resolveConfirmInterrupt(context.TODO(), nil, toolCall, userInput, nil)
	assert.IsType(t, &ApproveResult{}, decision)
}

// TestConfirmInterruptRail_resolveInterrupt_拒绝 验证 !approved → RejectResult 含 feedback
func TestConfirmInterruptRail_resolveInterrupt_拒绝(t *testing.T) {
	r := NewConfirmInterruptRail("write_file")
	toolCall := &llmschema.ToolCall{ID: "tc1", Name: "write_file", Arguments: `{}`}
	userInput := &ConfirmPayload{Approved: false, Feedback: "文件过大"}

	decision := r.resolveConfirmInterrupt(context.TODO(), nil, toolCall, userInput, nil)
	assert.IsType(t, &RejectResult{}, decision)
	rejectResult := decision.(*RejectResult)
	assert.Equal(t, "文件过大", rejectResult.ToolResult)
}

// TestConfirmInterruptRail_resolveInterrupt_拒绝无Feedback 验证 !approved 无 feedback → 默认消息
func TestConfirmInterruptRail_resolveInterrupt_拒绝无Feedback(t *testing.T) {
	r := NewConfirmInterruptRail("write_file")
	toolCall := &llmschema.ToolCall{ID: "tc1", Name: "write_file", Arguments: `{}`}
	userInput := &ConfirmPayload{Approved: false}

	decision := r.resolveConfirmInterrupt(context.TODO(), nil, toolCall, userInput, nil)
	assert.IsType(t, &RejectResult{}, decision)
	rejectResult := decision.(*RejectResult)
	assert.Equal(t, "用户反馈: 拒绝操作", rejectResult.ToolResult)
}

// TestConfirmInterruptRail_resolveInterrupt_无效输入 验证无效输入 → InterruptResult
func TestConfirmInterruptRail_resolveInterrupt_无效输入(t *testing.T) {
	r := NewConfirmInterruptRail("write_file")
	toolCall := &llmschema.ToolCall{ID: "tc1", Name: "write_file", Arguments: `{}`}
	// 不支持的输入类型
	userInput := 42

	decision := r.resolveConfirmInterrupt(context.TODO(), nil, toolCall, userInput, nil)
	assert.IsType(t, &InterruptResult{}, decision)
}

// TestConfirmInterruptRail_resolveInterrupt_Dict批准 验证 map 输入 approved=true
func TestConfirmInterruptRail_resolveInterrupt_Dict批准(t *testing.T) {
	r := NewConfirmInterruptRail("write_file")
	toolCall := &llmschema.ToolCall{ID: "tc1", Name: "write_file", Arguments: `{}`}
	userInput := map[string]any{"approved": true, "feedback": ""}

	decision := r.resolveConfirmInterrupt(context.TODO(), nil, toolCall, userInput, nil)
	assert.IsType(t, &ApproveResult{}, decision)
}

// TestConfirmInterruptRail_resolveInterrupt_Dict拒绝 验证 map 输入 approved=false
func TestConfirmInterruptRail_resolveInterrupt_Dict拒绝(t *testing.T) {
	r := NewConfirmInterruptRail("write_file")
	toolCall := &llmschema.ToolCall{ID: "tc1", Name: "write_file", Arguments: `{}`}
	userInput := map[string]any{"approved": false, "feedback": "不安全"}

	decision := r.resolveConfirmInterrupt(context.TODO(), nil, toolCall, userInput, nil)
	assert.IsType(t, &RejectResult{}, decision)
	rejectResult := decision.(*RejectResult)
	assert.Equal(t, "不安全", rejectResult.ToolResult)
}

// ──────────────────────────── isAutoConfirmed ────────────────────────────

// TestIsAutoConfirmed 验证 auto_confirm 查找逻辑
func TestIsAutoConfirmed(t *testing.T) {
	// nil config
	assert.False(t, isAutoConfirmed(nil, "write_file"))

	// key 不存在
	assert.False(t, isAutoConfirmed(map[string]any{}, "write_file"))

	// key 存在但值非 bool
	assert.False(t, isAutoConfirmed(map[string]any{"write_file": "yes"}, "write_file"))

	// key 存在且为 true
	assert.True(t, isAutoConfirmed(map[string]any{"write_file": true}, "write_file"))

	// key 存在且为 false
	assert.False(t, isAutoConfirmed(map[string]any{"write_file": false}, "write_file"))
}

// ──────────────────────────── getAutoConfirmKey ────────────────────────────

// TestConfirmInterruptRail_getAutoConfirmKey 验证 key 生成
func TestConfirmInterruptRail_getAutoConfirmKey(t *testing.T) {
	r := NewConfirmInterruptRail("write_file")

	// 正常 ToolCall
	toolCall := &llmschema.ToolCall{ID: "tc1", Name: "write_file", Arguments: `{}`}
	assert.Equal(t, "write_file", r.getAutoConfirmKey(toolCall))

	// nil ToolCall
	assert.Equal(t, "", r.getAutoConfirmKey(nil))
}

// ──────────────────────────── parseConfirmInput ────────────────────────────

// TestConfirmInterruptRail_parseConfirmInput_Dict 验证 map 输入解析
func TestConfirmInterruptRail_parseConfirmInput_Dict(t *testing.T) {
	r := NewConfirmInterruptRail()

	userInput := map[string]any{
		"approved":     false,
		"feedback":     "拒绝原因",
		"auto_confirm": false,
	}
	payload, ok := r.parseConfirmInput(userInput)
	assert.True(t, ok)
	assert.False(t, payload.Approved)
	assert.Equal(t, "拒绝原因", payload.Feedback)
	assert.False(t, payload.AutoConfirm)
}

// TestConfirmInterruptRail_parseConfirmInput_ConfirmPayload 验证 ConfirmPayload 直接传入
func TestConfirmInterruptRail_parseConfirmInput_ConfirmPayload(t *testing.T) {
	r := NewConfirmInterruptRail()
	original := &ConfirmPayload{Approved: true, Feedback: "好的", AutoConfirm: true}
	payload, ok := r.parseConfirmInput(original)
	assert.True(t, ok)
	assert.Equal(t, original, payload)
}

// ──────────────────────────── ConfirmPayload Schema ────────────────────────────

// TestConfirmPayloadSchema 验证 ConfirmPayload 的 JSON Schema
func TestConfirmPayloadSchema(t *testing.T) {
	schema := confirmPayloadSchema()
	assert.Equal(t, "object", schema["type"])
	props, ok := schema["properties"].(map[string]any)
	assert.True(t, ok)
	assert.Contains(t, props, "approved")
	assert.Contains(t, props, "feedback")
	assert.Contains(t, props, "auto_confirm")
}

// ──────────────────────────── InterruptRequester 接口 ────────────────────────────

// TestConfirmInterruptRail_InterruptRequester接口 验证 InterruptResult.Request 满足 InterruptRequester
func TestConfirmInterruptRail_InterruptRequester接口(t *testing.T) {
	r := NewConfirmInterruptRail("write_file")
	toolCall := &llmschema.ToolCall{ID: "tc1", Name: "write_file", Arguments: `{}`}
	decision := r.resolveConfirmInterrupt(context.TODO(), nil, toolCall, nil, nil)
	interruptResult, ok := decision.(*InterruptResult)
	assert.True(t, ok)
	// InterruptRequest 满足 InterruptRequester 接口
	var _ saschema.InterruptRequester = interruptResult.Request
}

// TestConfirmInterruptRail_resolveInterrupt_无效类型 验证不支持的输入类型 → InterruptResult
func TestConfirmInterruptRail_resolveInterrupt_无效类型(t *testing.T) {
	r := NewConfirmInterruptRail("write_file")
	toolCall := &llmschema.ToolCall{ID: "tc1", Name: "write_file", Arguments: `{}`}

	decision := r.resolveConfirmInterrupt(context.TODO(), nil, toolCall, 42, nil)
	assert.IsType(t, &InterruptResult{}, decision)
}

// TestConfirmInterruptRail_resolveInterrupt_AutoConfirmKeyNilToolCall 验证 nil ToolCall 的 auto_confirm_key
func TestConfirmInterruptRail_resolveInterrupt_AutoConfirmKeyNilToolCall(t *testing.T) {
	r := NewConfirmInterruptRail("write_file")
	autoConfirmConfig := map[string]any{"": true}

	// nil toolCall → autoConfirmKey="" → config[""]=true → Approve
	decision := r.resolveConfirmInterrupt(context.TODO(), nil, nil, nil, autoConfirmConfig)
	assert.IsType(t, &ApproveResult{}, decision)
}

// TestConfirmPayload_EdgeCases 验证 ConfirmPayload 边界情况
func TestConfirmPayload_EdgeCases(t *testing.T) {
	// 空 ConfirmPayload
	payload := &ConfirmPayload{}
	assert.False(t, payload.Approved)
	assert.Empty(t, payload.Feedback)
	assert.False(t, payload.AutoConfirm)
}

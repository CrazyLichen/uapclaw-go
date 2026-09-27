package rails

import (
	"testing"

	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/rails/interrupt"
)

// TestNewTeamToolApprovalRail_默认构造 测试默认构造
func TestNewTeamToolApprovalRail_默认构造(t *testing.T) {
	r := NewTeamToolApprovalRail("team1", "member1", nil, nil, "leader1", []string{"delete_file"})
	if r == nil {
		t.Fatal("NewTeamToolApprovalRail returned nil")
	}
	if r.teamName != "team1" {
		t.Fatalf("expected teamName=team1, got %s", r.teamName)
	}
	if r.memberName != "member1" {
		t.Fatalf("expected memberName=member1, got %s", r.memberName)
	}
	if r.leaderMemberName != "leader1" {
		t.Fatalf("expected leaderMemberName=leader1, got %s", r.leaderMemberName)
	}
}

// TestTeamToolApprovalRail_ResolveInterrupt_自动批准 测试 auto_confirm 自动批准
func TestTeamToolApprovalRail_ResolveInterrupt_自动批准(t *testing.T) {
	r := NewTeamToolApprovalRail("team1", "member1", nil, nil, "leader1", []string{"delete_file"})

	toolCall := &llmschema.ToolCall{
		ID:   "call_1",
		Name: "delete_file",
	}
	autoConfig := map[string]any{"delete_file": true}
	decision := r.ResolveInterruptFn(nil, nil, toolCall, nil, autoConfig)

	if _, ok := decision.(*interrupt.ApproveResult); !ok {
		t.Fatalf("expected ApproveResult, got %T", decision)
	}
}

// TestTeamToolApprovalRail_ResolveInterrupt_无ToolCall拒绝 测试无 tool_call 时拒绝
func TestTeamToolApprovalRail_ResolveInterrupt_无ToolCall拒绝(t *testing.T) {
	r := NewTeamToolApprovalRail("team1", "member1", nil, nil, "leader1", []string{"delete_file"})

	decision := r.ResolveInterruptFn(nil, nil, nil, nil, nil)

	if _, ok := decision.(*interrupt.RejectResult); !ok {
		t.Fatalf("expected RejectResult, got %T", decision)
	}
}

// TestTeamToolApprovalRail_ResolveInterrupt_首次中断 测试首次调用返回中断
func TestTeamToolApprovalRail_ResolveInterrupt_首次中断(t *testing.T) {
	r := NewTeamToolApprovalRail("team1", "member1", nil, nil, "leader1", []string{"delete_file"})

	toolCall := &llmschema.ToolCall{
		ID:   "call_1",
		Name: "delete_file",
	}
	decision := r.ResolveInterruptFn(nil, nil, toolCall, nil, nil)

	if _, ok := decision.(*interrupt.InterruptResult); !ok {
		t.Fatalf("expected InterruptResult, got %T", decision)
	}
}

// TestTeamToolApprovalRail_ResolveInterrupt_批准恢复 测试恢复时批准
func TestTeamToolApprovalRail_ResolveInterrupt_批准恢复(t *testing.T) {
	r := NewTeamToolApprovalRail("team1", "member1", nil, nil, "leader1", []string{"delete_file"})

	toolCall := &llmschema.ToolCall{
		ID:   "call_1",
		Name: "delete_file",
	}
	userInput := &interrupt.ConfirmPayload{
		Approved: true,
		Feedback: "ok",
	}
	decision := r.ResolveInterruptFn(nil, nil, toolCall, userInput, nil)

	if _, ok := decision.(*interrupt.ApproveResult); !ok {
		t.Fatalf("expected ApproveResult, got %T", decision)
	}
}

// TestTeamToolApprovalRail_ResolveInterrupt_拒绝恢复 测试恢复时拒绝
func TestTeamToolApprovalRail_ResolveInterrupt_拒绝恢复(t *testing.T) {
	r := NewTeamToolApprovalRail("team1", "member1", nil, nil, "leader1", []string{"delete_file"})

	toolCall := &llmschema.ToolCall{
		ID:   "call_1",
		Name: "delete_file",
	}
	userInput := &interrupt.ConfirmPayload{
		Approved: false,
		Feedback: "not allowed",
	}
	decision := r.ResolveInterruptFn(nil, nil, toolCall, userInput, nil)

	reject, ok := decision.(*interrupt.RejectResult)
	if !ok {
		t.Fatalf("expected RejectResult, got %T", decision)
	}
	if reject.ToolResult != "not allowed" {
		t.Fatalf("expected feedback 'not allowed', got %v", reject.ToolResult)
	}
}

// TestTeamToolApprovalRail_ResolveInterrupt_MapInput 测试 map 输入格式
func TestTeamToolApprovalRail_ResolveInterrupt_MapInput(t *testing.T) {
	r := NewTeamToolApprovalRail("team1", "member1", nil, nil, "leader1", []string{"delete_file"})

	toolCall := &llmschema.ToolCall{
		ID:   "call_1",
		Name: "delete_file",
	}
	userInput := map[string]any{
		"approved": true,
		"feedback": "ok",
	}
	decision := r.ResolveInterruptFn(nil, nil, toolCall, userInput, nil)

	if _, ok := decision.(*interrupt.ApproveResult); !ok {
		t.Fatalf("expected ApproveResult for map input, got %T", decision)
	}
}

// TestTeamToolApprovalRail_ResolveInterrupt_无效输入重新中断 测试无效输入重新中断
func TestTeamToolApprovalRail_ResolveInterrupt_无效输入重新中断(t *testing.T) {
	r := NewTeamToolApprovalRail("team1", "member1", nil, nil, "leader1", []string{"delete_file"})

	toolCall := &llmschema.ToolCall{
		ID:   "call_1",
		Name: "delete_file",
	}
	// 无法解析的输入类型
	userInput := "invalid string"
	decision := r.ResolveInterruptFn(nil, nil, toolCall, userInput, nil)

	if _, ok := decision.(*interrupt.InterruptResult); !ok {
		t.Fatalf("expected InterruptResult for invalid input, got %T", decision)
	}
}

// TestTeamToolApprovalRail_NoAutoConfirm 测试无 auto_confirm 配置
func TestTeamToolApprovalRail_NoAutoConfirm(t *testing.T) {
	r := NewTeamToolApprovalRail("team1", "member1", nil, nil, "leader1", []string{"delete_file"})

	toolCall := &llmschema.ToolCall{
		ID:   "call_1",
		Name: "delete_file",
	}
	// autoConfirmConfig 为 nil
	decision := r.ResolveInterruptFn(nil, nil, toolCall, nil, nil)

	if _, ok := decision.(*interrupt.InterruptResult); !ok {
		t.Fatalf("expected InterruptResult when no auto_confirm, got %T", decision)
	}
}

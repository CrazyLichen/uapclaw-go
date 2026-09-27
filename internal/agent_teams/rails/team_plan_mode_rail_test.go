package rails

import (
	"context"
	"testing"
)

// TestNewTeamPlanModeRail_默认构造 测试默认构造
func TestNewTeamPlanModeRail_默认构造(t *testing.T) {
	r := NewTeamPlanModeRail()
	if r == nil {
		t.Fatal("NewTeamPlanModeRail returned nil")
	}
}

// TestNewTeamPlanModeRail_WithOptions 测试选项构造
func TestNewTeamPlanModeRail_WithOptions(t *testing.T) {
	r := NewTeamPlanModeRail(
		WithLanguageOverride("en"),
	)
	if r.languageOverride != "en" {
		t.Fatalf("expected languageOverride=en, got %s", r.languageOverride)
	}
}

// TestTeamPlanModeRail_Init 测试 Init 缓存
func TestTeamPlanModeRail_Init(t *testing.T) {
	r := NewTeamPlanModeRail()
	agent := newFakeBaseAgentTeam()
	err := r.Init(context.Background(), agent)
	if err != nil {
		t.Fatalf("Init returned error: %v", err)
	}
	if r.agent == nil {
		t.Fatal("expected agent reference after Init")
	}
}

// TestTeamPlanModeRail_Uninit 测试 Uninit 清理
func TestTeamPlanModeRail_Uninit(t *testing.T) {
	r := NewTeamPlanModeRail()
	agent := newFakeBaseAgentTeam()
	_ = r.Init(context.Background(), agent)
	_ = r.Uninit(agent)
	if r.agent != nil {
		t.Fatal("expected nil agent after Uninit")
	}
	if r.systemPromptBuilder != nil {
		t.Fatal("expected nil systemPromptBuilder after Uninit")
	}
}

// TestTeamPlanModeRail_ResolveLanguage 测试语言解析
func TestTeamPlanModeRail_ResolveLanguage(t *testing.T) {
	// 有语言覆盖
	r := NewTeamPlanModeRail(WithLanguageOverride("en"))
	if r.resolveLanguage() != "en" {
		t.Fatalf("expected en, got %s", r.resolveLanguage())
	}

	// 无语言覆盖，默认 cn
	r2 := NewTeamPlanModeRail()
	if r2.resolveLanguage() != "cn" {
		t.Fatalf("expected cn, got %s", r2.resolveLanguage())
	}
}

// TestTeamPlanModeRail_BeforeModelCall 测试 BeforeModelCall
func TestTeamPlanModeRail_BeforeModelCall(t *testing.T) {
	r := NewTeamPlanModeRail()
	agent := newFakeBaseAgentTeam()
	_ = r.Init(context.Background(), agent)
	err := r.BeforeModelCall(context.Background(), nil)
	if err != nil {
		t.Fatalf("BeforeModelCall returned error: %v", err)
	}
}

// TestTeamPlanModeRail_BeforeModelCall_无Agent 测试无 agent 时不 panic
func TestTeamPlanModeRail_BeforeModelCall_无Agent(t *testing.T) {
	r := NewTeamPlanModeRail()
	err := r.BeforeModelCall(context.Background(), nil)
	if err != nil {
		t.Fatalf("BeforeModelCall should not fail without agent: %v", err)
	}
}

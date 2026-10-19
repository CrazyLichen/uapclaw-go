package team

import (
	"context"
	"strings"
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	saprompt "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/prompts"
	agentschema "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/schema"
	cb "github.com/uapclaw/uapclaw-go/internal/agentcore/runner/callback"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/stream"
)

// ──────────────────────────── 测试辅助 ────────────────────────────

// fakePromptBuilder 测试用 SystemPromptBuilder。
type fakePromptBuilder struct {
	sections map[string]*saprompt.PromptSection
}

func newFakePromptBuilder() *fakePromptBuilder {
	return &fakePromptBuilder{sections: map[string]*saprompt.PromptSection{}}
}

func (f *fakePromptBuilder) AddSection(s saprompt.PromptSection) *saprompt.SystemPromptBuilder {
	copied := s
	f.sections[s.Name] = &copied
	return nil
}
func (f *fakePromptBuilder) RemoveSection(name string) *saprompt.SystemPromptBuilder {
	delete(f.sections, name)
	return nil
}
func (f *fakePromptBuilder) Language() string   { return "cn" }
func (f *fakePromptBuilder) SetLanguage(string) {}
func (f *fakePromptBuilder) GetSection(name string) *saprompt.PromptSection {
	return f.sections[name]
}
func (f *fakePromptBuilder) HasSection(name string) bool {
	_, ok := f.sections[name]
	return ok
}

// fakeAgentForRails 实现 BaseAgent（promptBuilder 路径真实）。
type fakeAgentForRails struct {
	promptBuilder *fakePromptBuilder
	abilityMgr    *fakeAbilityMgrForFilter
	card          *agentschema.AgentCard
}

func newFakeAgentWithPromptBuilder() *fakeAgentForRails {
	return &fakeAgentForRails{
		promptBuilder: newFakePromptBuilder(),
		abilityMgr:    &fakeAbilityMgrForFilter{},
		card:          &agentschema.AgentCard{},
	}
}

func newFakeAgentWithAbilityMgr() *fakeAgentForRails {
	return &fakeAgentForRails{
		promptBuilder: newFakePromptBuilder(),
		abilityMgr:    &fakeAbilityMgrForFilter{},
		card:          &agentschema.AgentCard{},
	}
}

func (f *fakeAgentForRails) Card() *agentschema.AgentCard { return f.card }
func (f *fakeAgentForRails) Config() agentinterfaces.AgentConfig {
	return nil
}
func (f *fakeAgentForRails) AbilityManager() agentinterfaces.AbilityManagerInterface {
	return f.abilityMgr
}
func (f *fakeAgentForRails) SystemPromptBuilder() saprompt.SystemPromptBuilderInterface {
	return f.promptBuilder
}
func (f *fakeAgentForRails) Stream(ctx context.Context, inputs map[string]any, opts ...agentinterfaces.AgentOption) (<-chan stream.Schema, error) {
	return nil, nil
}
func (f *fakeAgentForRails) Invoke(ctx context.Context, inputs map[string]any, opts ...agentinterfaces.AgentOption) (map[string]any, error) {
	return nil, nil
}
func (f *fakeAgentForRails) Configure(ctx context.Context, config agentinterfaces.AgentConfig) error {
	return nil
}
func (f *fakeAgentForRails) CallbackManager() *agentinterfaces.AgentCallbackManager {
	return nil
}
func (f *fakeAgentForRails) RegisterCallback(ctx context.Context, event agentinterfaces.AgentCallbackEvent, fn cb.PerAgentCallbackFunc, opts ...cb.CallbackOption) error {
	return nil
}
func (f *fakeAgentForRails) RegisterRail(ctx context.Context, r agentinterfaces.AgentRail, opts ...cb.CallbackOption) error {
	return nil
}
func (f *fakeAgentForRails) UnregisterRail(ctx context.Context, r agentinterfaces.AgentRail) error {
	return nil
}

// 编译时验证
var _ agentinterfaces.BaseAgent = (*fakeAgentForRails)(nil)

// containsAll 检查 content 是否包含全部子串。
func containsAll(content string, subs ...string) bool {
	for _, s := range subs {
		if !strings.Contains(content, s) {
			return false
		}
	}
	return true
}

// ──────────────────────────── 测试 ────────────────────────────

// TestTeamWorkspaceReportPathRail_内容构建 验证 BeforeModelCall 注入 section。
func TestTeamWorkspaceReportPathRail_内容构建(t *testing.T) {
	root := t.TempDir()
	rail := NewTeamWorkspaceReportPathRail(root, "t1", "cn")
	if rail.Priority() != 5 {
		t.Fatalf("priority = %d, want 5", rail.Priority())
	}

	agent := newFakeAgentWithPromptBuilder()
	if err := rail.Init(context.Background(), agent); err != nil {
		t.Fatalf("Init failed: %v", err)
	}
	if err := rail.BeforeModelCall(context.Background(), nil); err != nil {
		t.Fatalf("BeforeModelCall failed: %v", err)
	}

	section := agent.promptBuilder.GetSection("team_workspace_report_paths")
	if section == nil {
		t.Fatalf("section not injected")
	}
	if section.Priority != 67 {
		t.Fatalf("section priority = %d, want 67", section.Priority)
	}
	content := section.Content["en"]
	if !containsAll(content, ".team/t1/", root, "prd-review-memo.md") {
		t.Fatalf("content missing key parts: %q", content)
	}

	// Uninit 移除 section
	if err := rail.Uninit(agent); err != nil {
		t.Fatalf("Uninit failed: %v", err)
	}
	if agent.promptBuilder.HasSection("team_workspace_report_paths") {
		t.Fatalf("section should be removed after Uninit")
	}
}

// TestMemberSkillToolkitRail_InitUninit 工具注册与反注册。
func TestMemberSkillToolkitRail_InitUninit(t *testing.T) {
	wsDir := t.TempDir()
	rail := NewMemberSkillToolkitRail(wsDir)
	if rail.Priority() != 95 {
		t.Fatalf("priority = %d, want 95", rail.Priority())
	}

	agent := newFakeAgentWithAbilityMgr()
	if err := rail.Init(context.Background(), agent); err != nil {
		t.Fatalf("Init failed: %v", err)
	}
	// SkillToolkit 提供 3 个工具（search/install/uninstall skill）
	abilities := agent.abilityMgr.List()
	if len(abilities) != 3 {
		t.Fatalf("abilities = %d, want 3", len(abilities))
	}
	// 工具 ID 带 agentID 后缀
	for _, a := range abilities {
		card := a.(*tool.ToolCard)
		if card.ID == "" {
			t.Fatalf("card.ID should be qualified with agent id suffix")
		}
	}

	if err := rail.Uninit(agent); err != nil {
		t.Fatalf("Uninit failed: %v", err)
	}
	if len(agent.abilityMgr.List()) != 0 {
		t.Fatalf("abilities should be empty after Uninit")
	}
}

package team

import (
	"context"
	"strings"
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/foundation/tool"
	cb "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/runner/callback"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/session/stream"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/single_agent/interfaces"
	saprompt "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/single_agent/prompts"
	agentschema "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/single_agent/schema"
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
	rail := NewTeamWorkspaceReportPathRail(root, "", "", "t1", "cn")
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
	if !containsAll(content, "# Project and Team Workspace File Policy", root, "deliverables") {
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

// TeardownTools 测试替身空实现（对齐 AbilityManagerInterface 扩张）。
func (f *fakeAbilityMgrForFilter) TeardownTools() {}

// TestTeamWorkspaceReportPathRail_项目与Projectless分支 对齐 Python deliverables 三分支。
func TestTeamWorkspaceReportPathRail_项目与Projectless分支(t *testing.T) {
	root := t.TempDir()
	// 绑定项目 → deliverables 留在项目内
	rail := NewTeamWorkspaceReportPathRail(root, "/proj", "", "t1", "cn")
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
	if !strings.Contains(section.Content["en"], "User project root: `/proj`") {
		t.Fatalf("project 分支缺失: %s", section.Content["en"])
	}

	// projectless → 共享 outputs 目录
	rail2 := NewTeamWorkspaceReportPathRail(root, "", "/team/outputs", "t1", "en")
	agent2 := newFakeAgentWithPromptBuilder()
	_ = rail2.Init(context.Background(), agent2)
	_ = rail2.BeforeModelCall(context.Background(), nil)
	section2 := agent2.promptBuilder.GetSection("team_workspace_report_paths")
	if section2 == nil || !strings.Contains(section2.Content["en"], "Final deliverables directory: `/team/outputs`") {
		t.Fatalf("projectless 分支缺失")
	}

	// 双缺 → 需先确认目的地
	rail3 := NewTeamWorkspaceReportPathRail(root, "", "", "t1", "en")
	agent3 := newFakeAgentWithPromptBuilder()
	_ = rail3.Init(context.Background(), agent3)
	_ = rail3.BeforeModelCall(context.Background(), nil)
	section3 := agent3.promptBuilder.GetSection("team_workspace_report_paths")
	if section3 == nil || !strings.Contains(section3.Content["en"], "do not silently drop them") {
		t.Fatalf("双缺分支缺失")
	}
}

// TestTeamWorkspaceReportPathRail_BindSwarmContext 对齐 bind_swarm_context 捕获。
func TestTeamWorkspaceReportPathRail_BindSwarmContext(t *testing.T) {
	rail := NewTeamWorkspaceReportPathRail(t.TempDir(), "", "", "t1", "cn")
	if rail.swarmContext != nil {
		t.Fatalf("初始 swarmContext 应为 nil")
	}
	rail.BindSwarmContext(map[string]any{"role": "leader"})
	if rail.swarmContext == nil {
		t.Fatalf("BindSwarmContext 后应捕获上下文")
	}
	// Uninit 移除节
	agent := newFakeAgentWithPromptBuilder()
	_ = rail.Init(context.Background(), agent)
	_ = rail.BeforeModelCall(context.Background(), nil)
	_ = rail.Uninit(agent)
	if agent.promptBuilder.GetSection("team_workspace_report_paths") != nil {
		t.Fatalf("Uninit 后应移除 section")
	}
}

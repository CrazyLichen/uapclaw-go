package prompts

import (
	"strings"
	"testing"

	atschema "github.com/uapclaw/uapclaw-go/internal/agent_teams/schema"
	saprompt "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/prompts"
)

// TestBuildTeamRoleSection_Leader 测试 Leader 角色构建
func TestBuildTeamRoleSection_Leader(t *testing.T) {
	section := mustBuildRoleSection(t, atschema.TeamRoleLeader, "build_mode", "native", "cn")
	if section == nil {
		t.Fatal("BuildTeamRoleSection returned nil")
	}
	if section.Name != SectionRole {
		t.Fatalf("expected section name %s, got %s", SectionRole, section.Name)
	}
	if section.Priority != 11 {
		t.Fatalf("expected priority 11, got %d", section.Priority)
	}
	cnContent := section.Content["cn"]
	if cnContent == "" {
		t.Fatal("cn content is empty")
	}
	// 上游刻意不渲染 member_name（它住 team_identity 状态车道），
	// 否则同角色成员前缀不再逐字节相同、KV 缓存前缀失效。
	if strings.Contains(cnContent, "member_name") {
		t.Fatal("role section 不应渲染 member_name")
	}
	// 应包含 build_mode 标签
	if !strings.Contains(cnContent, "build_mode") {
		t.Fatal("cn content should contain build_mode")
	}
}

// TestBuildTeamRoleSection_LeaderPlanMode 测试 Leader plan_mode
func TestBuildTeamRoleSection_LeaderPlanMode(t *testing.T) {
	section := mustBuildRoleSection(t, atschema.TeamRoleLeader, "plan_mode", "native", "cn")
	if section == nil {
		t.Fatal("BuildTeamRoleSection returned nil")
	}
	cnContent := section.Content["cn"]
	if !strings.Contains(cnContent, "plan_mode") {
		t.Fatal("cn content should contain plan_mode")
	}
	if !strings.Contains(cnContent, "approve_plan") {
		t.Fatal("cn content should contain approve_plan in plan_mode label")
	}
}

// TestBuildTeamRoleSection_TeammateBuildMode 测试 Teammate build_mode
func TestBuildTeamRoleSection_TeammateBuildMode(t *testing.T) {
	section := mustBuildRoleSection(t, atschema.TeamRoleTeammate, "build_mode", "native", "en")
	if section == nil {
		t.Fatal("BuildTeamRoleSection returned nil")
	}
	enContent := section.Content["en"]
	if enContent == "" {
		t.Fatal("en content is empty")
	}
	if strings.Contains(enContent, "member_name") {
		t.Fatal("role section 不应渲染 member_name（上游刻意排除）")
	}
	if !strings.Contains(enContent, "build_mode") {
		t.Fatal("en content should contain build_mode")
	}
}

// TestBuildTeamRoleSection_TeammatePlanMode 测试 Teammate plan_mode
func TestBuildTeamRoleSection_TeammatePlanMode(t *testing.T) {
	section := mustBuildRoleSection(t, atschema.TeamRoleTeammate, "plan_mode", "native", "cn")
	if section == nil {
		t.Fatal("BuildTeamRoleSection returned nil")
	}
	cnContent := section.Content["cn"]
	if cnContent == "" {
		t.Fatal("cn content is empty")
	}
	// plan_mode 下 teammate 应包含 submit_plan 提示
	if !strings.Contains(cnContent, "submit_plan") {
		t.Fatal("cn content should contain submit_plan in plan_mode label")
	}
}

// TestBuildTeamRoleSection_English 测试英文 Leader 角色
func TestBuildTeamRoleSection_English(t *testing.T) {
	section := mustBuildRoleSection(t, atschema.TeamRoleLeader, "plan_mode", "native", "en")
	if section == nil {
		t.Fatal("BuildTeamRoleSection returned nil")
	}
	enContent := section.Content["en"]
	if !strings.Contains(enContent, "plan_mode") {
		t.Fatal("en content should contain plan_mode")
	}
	if !strings.Contains(enContent, "approve_plan") {
		t.Fatal("en content should contain approve_plan in leader plan_mode label")
	}
}

// TestBuildTeamWorkflowSection_Leader 测试 Leader 工作流
func TestBuildTeamWorkflowSection_Leader(t *testing.T) {
	section := mustBuildWorkflowSection(t, atschema.TeamRoleLeader, "default", "cn")
	if section == nil {
		t.Fatal("BuildTeamWorkflowSection returned nil for leader")
	}
	if section.Priority != 13 {
		t.Fatalf("expected priority 13, got %d", section.Priority)
	}
	if section.Name != SectionWorkflow {
		t.Fatalf("expected section name %s, got %s", SectionWorkflow, section.Name)
	}
}

// TestBuildTeamWorkflowSection_Teammate返回nil 测试 Teammate 无工作流
func TestBuildTeamWorkflowSection_Teammate返回nil(t *testing.T) {
	section := mustBuildWorkflowSection(t, atschema.TeamRoleTeammate, "default", "cn")
	if section != nil {
		t.Fatal("expected nil for teammate workflow")
	}
}

// TestBuildTeamWorkflowSection_三种模式 测试 default/predefined/hybrid 三种模式
func TestBuildTeamWorkflowSection_三种模式(t *testing.T) {
	for _, mode := range []string{"default", "predefined", "hybrid"} {
		section := mustBuildWorkflowSection(t, atschema.TeamRoleLeader, mode, "cn")
		if section == nil {
			t.Fatalf("BuildTeamWorkflowSection returned nil for mode=%s", mode)
		}
		cnContent := section.Content["cn"]
		if cnContent == "" {
			t.Fatalf("cn content empty for mode=%s", mode)
		}
	}
}

// TestBuildTeamWorkflowSection_English 测试英文工作流
func TestBuildTeamWorkflowSection_English(t *testing.T) {
	section := mustBuildWorkflowSection(t, atschema.TeamRoleLeader, "default", "en")
	if section == nil {
		t.Fatal("BuildTeamWorkflowSection returned nil for English leader")
	}
	enContent := section.Content["en"]
	if !strings.Contains(enContent, "# Workflow") {
		t.Fatal("English workflow should contain '# Workflow' heading")
	}
}

// TestBuildTeamLifecycleSection_Leader 测试 Leader 生命周期
func TestBuildTeamLifecycleSection_Leader(t *testing.T) {
	section := mustBuildLifecycleSection(t, atschema.TeamRoleLeader, "temporary", "cn")
	if section == nil {
		t.Fatal("BuildTeamLifecycleSection returned nil for leader")
	}
	if section.Priority != 14 {
		t.Fatalf("expected priority 14, got %d", section.Priority)
	}
	if section.Name != SectionLifecycle {
		t.Fatalf("expected section name %s, got %s", SectionLifecycle, section.Name)
	}
}

// TestBuildTeamLifecycleSection_Teammate返回nil 测试 Teammate 无生命周期
func TestBuildTeamLifecycleSection_Teammate返回nil(t *testing.T) {
	section := mustBuildLifecycleSection(t, atschema.TeamRoleTeammate, "temporary", "cn")
	if section != nil {
		t.Fatal("expected nil for teammate lifecycle")
	}
}

// TestBuildTeamLifecycleSection_Persistent 测试 persistent 生命周期模板
func TestBuildTeamLifecycleSection_Persistent(t *testing.T) {
	section := mustBuildLifecycleSection(t, atschema.TeamRoleLeader, "persistent", "cn")
	if section == nil {
		t.Fatal("BuildTeamLifecycleSection returned nil for persistent lifecycle")
	}
	cnContent := section.Content["cn"]
	if cnContent == "" {
		t.Fatal("cn content is empty for persistent lifecycle")
	}
}

// TestBuildTeamExtraSection 测试额外提示
func TestBuildTeamExtraSection(t *testing.T) {
	section := BuildTeamExtraSection("custom instructions", "cn")
	if section == nil {
		t.Fatal("BuildTeamExtraSection returned nil")
	}
	if section.Priority != 16 {
		t.Fatalf("expected priority 16, got %d", section.Priority)
	}
	if section.Name != SectionExtra {
		t.Fatalf("expected section name %s, got %s", SectionExtra, section.Name)
	}
	cnContent := section.Content["cn"]
	if !strings.Contains(cnContent, "custom instructions") {
		t.Fatal("cn content should contain base prompt text")
	}
}

// TestBuildTeamExtraSection_空白返回nil 测试空白 base prompt 返回 nil
func TestBuildTeamExtraSection_空白返回nil(t *testing.T) {
	section := BuildTeamExtraSection("", "cn")
	if section != nil {
		t.Fatal("expected nil for empty base prompt")
	}
	section = BuildTeamExtraSection("   ", "cn")
	if section != nil {
		t.Fatal("expected nil for whitespace-only base prompt")
	}
}

// TestBuildTeamExtraSection_无标题头 测试 Extra section 不加标题头
func TestBuildTeamExtraSection_无标题头(t *testing.T) {
	section := BuildTeamExtraSection("custom prompt", "cn")
	if section == nil {
		t.Fatal("BuildTeamExtraSection returned nil")
	}
	cnContent := section.Content["cn"]
	if strings.HasPrefix(cnContent, "#") {
		t.Fatal("Extra section should NOT have a heading")
	}
}

// TestSectionName常量 测试 Section 名称常量与 Python 对齐
func TestSectionName常量(t *testing.T) {
	if SectionRole != "team_role" {
		t.Fatalf("expected team_role, got %s", SectionRole)
	}
	if SectionHITT != "team_hitt" {
		t.Fatalf("expected team_hitt, got %s", SectionHITT)
	}
	if SectionWorkflow != "team_workflow" {
		t.Fatalf("expected team_workflow, got %s", SectionWorkflow)
	}
	if SectionLifecycle != "team_lifecycle" {
		t.Fatalf("expected team_lifecycle, got %s", SectionLifecycle)
	}
	if SectionExtra != "team_extra" {
		t.Fatalf("expected team_extra, got %s", SectionExtra)
	}
}

// TestLabelsFor 测试标签回退
func TestLabelsFor(t *testing.T) {
	// cn 存在
	lbl := sectionsLabelsFor("cn")
	if lbl["role_heading"] == "" {
		t.Fatal("cn role_heading should not be empty")
	}
	// en 存在
	lbl = sectionsLabelsFor("en")
	if lbl["role_heading"] == "" {
		t.Fatal("en role_heading should not be empty")
	}
	// 未知语言回退到 cn
	lbl = sectionsLabelsFor("fr")
	if lbl["role_heading"] != labels["cn"]["role_heading"] {
		t.Fatal("unknown language should fallback to cn")
	}
}

// TestAllBuilders 返回类型验证
func TestAllBuilders(t *testing.T) {
	// 验证所有 builder 返回正确的 PromptSection 类型
	var _ = mustBuildRoleSection(t, atschema.TeamRoleLeader, "build_mode", "native", "cn")
	var _ = mustBuildWorkflowSection(t, atschema.TeamRoleLeader, "default", "cn")
	var _ = mustBuildLifecycleSection(t, atschema.TeamRoleLeader, "temporary", "cn")
	var _ = BuildTeamExtraSection("e", "cn")
	var _ = mustBuildHITTSection(t, atschema.TeamRoleLeader, "cn", "", false)
}

// mustBuildHITTSection 断言 BuildTeamHITTSection 无错误。
func mustBuildHITTSection(t *testing.T, role atschema.TeamRole, language, selfName string, expose bool) *saprompt.PromptSection {
	t.Helper()
	section, err := BuildTeamHITTSection(role, true, language, selfName, expose)
	if err != nil {
		t.Fatalf("BuildTeamHITTSection(%v, %s) 返回错误: %v", role, language, err)
	}
	return section
}

// mustBuildRoleSection 断言 BuildTeamRoleSection 无错误。
func mustBuildRoleSection(t *testing.T, role atschema.TeamRole, teammateMode, workspaceVariant, language string) *saprompt.PromptSection {
	t.Helper()
	section, err := BuildTeamRoleSection(role, teammateMode, workspaceVariant, language)
	if err != nil {
		t.Fatalf("BuildTeamRoleSection(%v, %s, %s, %s) 返回错误: %v", role, teammateMode, workspaceVariant, language, err)
	}
	return section
}

// mustBuildWorkflowSection 断言 BuildTeamWorkflowSection 无错误。
func mustBuildWorkflowSection(t *testing.T, role atschema.TeamRole, teamMode, language string) *saprompt.PromptSection {
	t.Helper()
	section, err := BuildTeamWorkflowSection(role, teamMode, language)
	if err != nil {
		t.Fatalf("BuildTeamWorkflowSection(%v, %s, %s) 返回错误: %v", role, teamMode, language, err)
	}
	return section
}

// mustBuildLifecycleSection 断言 BuildTeamLifecycleSection 无错误。
func mustBuildLifecycleSection(t *testing.T, role atschema.TeamRole, lifecycle, language string) *saprompt.PromptSection {
	t.Helper()
	section, err := BuildTeamLifecycleSection(role, lifecycle, language)
	if err != nil {
		t.Fatalf("BuildTeamLifecycleSection(%v, %s, %s) 返回错误: %v", role, lifecycle, language, err)
	}
	return section
}

// TestBuildTeamInboundTagsSection_cn 验证中文 section 内容等于模板全文。
func TestBuildTeamInboundTagsSection_cn(t *testing.T) {
	sec, err := BuildTeamInboundTagsSection("cn")
	if err != nil {
		t.Fatal(err)
	}
	if sec == nil {
		t.Fatal("不应返回 nil")
	}
	want, err := LoadTemplate("inbound_tags", "cn")
	if err != nil {
		t.Fatal(err)
	}
	if sec.Content["cn"] != want.Content() {
		t.Errorf("cn 内容与模板不一致：得到 %d 字节，模板 %d 字节",
			len(sec.Content["cn"]), len(want.Content()))
	}
}

// TestBuildTeamInboundTagsSection_en 验证英文 section 内容等于模板全文。
func TestBuildTeamInboundTagsSection_en(t *testing.T) {
	sec, err := BuildTeamInboundTagsSection("en")
	if err != nil {
		t.Fatal(err)
	}
	want, err := LoadTemplate("inbound_tags", "en")
	if err != nil {
		t.Fatal(err)
	}
	if sec.Content["en"] != want.Content() {
		t.Errorf("en 内容与模板不一致")
	}
}

// TestBuildTeamInboundTagsSection_优先级 验证 section 名与优先级 P:18。
// Python: SECTION_PRIORITY[INBOUND_TAGS] = 18 (sections.py L89)
func TestBuildTeamInboundTagsSection_优先级(t *testing.T) {
	sec, err := BuildTeamInboundTagsSection("cn")
	if err != nil {
		t.Fatal(err)
	}
	if sec.Name != SectionInboundTags {
		t.Errorf("Name = %q，期望 %q", sec.Name, SectionInboundTags)
	}
	if sec.Priority != 18 {
		t.Errorf("Priority = %d，期望 18", sec.Priority)
	}
	if SectionInboundTags != "team_inbound_tags" {
		t.Errorf("SectionInboundTags = %q，期望 team_inbound_tags", SectionInboundTags)
	}
}

// TestBuildTeamRoleSection_不渲染MemberName 验证 role section 不含成员名。
// Python: build_team_role_section 无 member_name 参数
// （sections.py L254-263 的签名里没有它；docstring 明说
// "The member's own member_name is deliberately NOT rendered here"）。
func TestBuildTeamRoleSection_不渲染MemberName(t *testing.T) {
	section, err := BuildTeamRoleSection(atschema.TeamRoleTeammate, "build_mode", "native", "cn")
	if err != nil {
		t.Fatalf("BuildTeamRoleSection 返回错误: %v", err)
	}
	body := section.Render("cn")
	if strings.Contains(body, "member_name") {
		t.Fatalf("role section 不应渲染 member_name，实际包含：%q", body)
	}
}

// TestBuildTeamRoleSection_HumanAgent 验证 HUMAN_AGENT 用独立策略且无执行模式行。
// Python: build_team_role_section —— HUMAN_AGENT 取 human_agent_policy，
// 且 mode_line = ""（avatar 不自己规划任务）。
func TestBuildTeamRoleSection_HumanAgent(t *testing.T) {
	section, err := BuildTeamRoleSection(atschema.TeamRoleHumanAgent, "build_mode", "native", "cn")
	if err != nil {
		t.Fatalf("BuildTeamRoleSection 返回错误: %v", err)
	}
	body := section.Render("cn")
	if strings.Contains(body, "你的执行模式:") {
		t.Fatalf("HUMAN_AGENT 不应有执行模式行，实际：%q", body)
	}
}

// TestBuildTeamRoleSection_ExternalWorkspace 验证 external 变体取另一份策略模板。
// Python: build_team_role_section —— workspace_prompt_variant="external"
// 时 policy_name = "teammate_policy_external"。
func TestBuildTeamRoleSection_ExternalWorkspace(t *testing.T) {
	native, err := BuildTeamRoleSection(atschema.TeamRoleTeammate, "build_mode", "native", "cn")
	if err != nil {
		t.Fatalf("native 返回错误: %v", err)
	}
	external, err := BuildTeamRoleSection(atschema.TeamRoleTeammate, "build_mode", "external", "cn")
	if err != nil {
		t.Fatalf("external 返回错误: %v", err)
	}
	if native.Render("cn") == external.Render("cn") {
		t.Fatal("native 与 external 变体应取不同策略模板")
	}
}

// TestBuildTeamHITTSection_HITT关闭返回nil 验证门控在 hittEnabled 开关上。
// Python: build_team_hitt_section —— `if not hitt_enabled: return None`，
// docstring 明说门控在能力开关而非实时名册上，这样人类 agent 还没 spawn
// 时规则就已就绪。
func TestBuildTeamHITTSection_HITT关闭返回nil(t *testing.T) {
	if s, err := BuildTeamHITTSection(atschema.TeamRoleLeader, false, "cn", "", false); err != nil || s != nil {
		t.Fatalf("hittEnabled=false 应返回 nil,nil，实际 %v,%v", s, err)
	}
}

// TestBuildTeamHITTSection_HITT开启即有内容 验证不需要人类名册。
func TestBuildTeamHITTSection_HITT开启即有内容(t *testing.T) {
	s, err := BuildTeamHITTSection(atschema.TeamRoleLeader, true, "cn", "", false)
	if err != nil {
		t.Fatalf("返回错误: %v", err)
	}
	if s == nil {
		t.Fatal("hittEnabled=true 应返回 section（不依赖人类名册）")
	}
	if s.Priority != 12 {
		t.Fatalf("HITT priority 应为 12，实际 %d", s.Priority)
	}
	if s.Render("cn") == "" {
		t.Fatal("HITT section 内容为空")
	}
}

// TestBuildTeamHITTSection_HumanAgent含自身名 验证 avatar 契约注入 self_line。
// Python: _hitt_contract_body —— 只有 HUMAN_AGENT 走 format({self_line: ...})。
func TestBuildTeamHITTSection_HumanAgent含自身名(t *testing.T) {
	s, err := BuildTeamHITTSection(atschema.TeamRoleHumanAgent, true, "cn", "human_1", false)
	if err != nil {
		t.Fatalf("返回错误: %v", err)
	}
	if s == nil {
		t.Fatal("HUMAN_AGENT + hittEnabled 应返回 section")
	}
	if !strings.Contains(s.Render("cn"), "human_1") {
		t.Fatal("human_agent 契约应含自身 member_name")
	}
}

// TestBuildTeamHITTSection_Teammate匿名与暴露 验证变体切换。
// Python: _hitt_template_name —— expose=True 取 hitt_teammate，否则 anonymous。
func TestBuildTeamHITTSection_Teammate匿名与暴露(t *testing.T) {
	anon, err := BuildTeamHITTSection(atschema.TeamRoleTeammate, true, "cn", "bob", false)
	if err != nil {
		t.Fatalf("返回错误: %v", err)
	}
	exposed, err := BuildTeamHITTSection(atschema.TeamRoleTeammate, true, "cn", "bob", true)
	if err != nil {
		t.Fatalf("返回错误: %v", err)
	}
	if anon == nil || exposed == nil {
		t.Fatal("teammate + hittEnabled 应返回 section")
	}
	if anon.Render("cn") == exposed.Render("cn") {
		t.Fatal("匿名与暴露两个变体应不同")
	}
	if strings.Contains(anon.Render("cn"), "bob") {
		t.Fatal("匿名变体不应出现成员名")
	}
}

// TestBuildTeamHITTSection_Bridge用队友策略 验证 EXTERNAL_CLI 归入 teammate 模板。
// Python: _hitt_template_name —— TEAMMATE 与 EXTERNAL_CLI 同一分支。
func TestBuildTeamHITTSection_Bridge用队友策略(t *testing.T) {
	ext, err := BuildTeamHITTSection(atschema.TeamRoleExternalCLI, true, "cn", "", false)
	if err != nil {
		t.Fatalf("返回错误: %v", err)
	}
	tm, err := BuildTeamHITTSection(atschema.TeamRoleTeammate, true, "cn", "", false)
	if err != nil {
		t.Fatalf("返回错误: %v", err)
	}
	if ext == nil || tm == nil {
		t.Fatal("应返回 section")
	}
	if ext.Render("cn") != tm.Render("cn") {
		t.Fatal("EXTERNAL_CLI 与 TEAMMATE 应取同一份 HITT 模板")
	}
}

// TestBuildTeamHITTSection_未知角色返回nil 验证无模板角色返回 nil。
func TestBuildTeamHITTSection_未知角色返回nil(t *testing.T) {
	if s, err := BuildTeamHITTSection(atschema.TeamRole("unknown_role"), true, "cn", "", false); err != nil || s != nil {
		t.Fatalf("未知角色应返回 nil,nil，实际 %v,%v", s, err)
	}
}

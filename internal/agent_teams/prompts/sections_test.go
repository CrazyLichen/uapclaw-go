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

// ──────────────────────────── dispatch ────────────────────────────

// TestBuildTeamDispatchSection_角色分派 验证只有 leader/teammate/external_cli 有。
// Python: _DISPATCH_ROLE_SLUGS (sections.py L370-375)
func TestBuildTeamDispatchSection_角色分派(t *testing.T) {
	cases := []struct {
		role atschema.TeamRole
		want bool
	}{
		{atschema.TeamRoleLeader, true},
		{atschema.TeamRoleTeammate, true},
		{atschema.TeamRoleExternalCLI, true},
		{atschema.TeamRoleHumanAgent, false},
		{atschema.TeamRoleBridgeAgent, false},
	}
	for _, c := range cases {
		s, err := BuildTeamDispatchSection(c.role, "autonomous", false, false, "cn")
		if err != nil {
			t.Fatalf("%s 返回错误: %v", c.role, err)
		}
		if (s != nil) != c.want {
			t.Fatalf("%s: 期望有 section=%v，实际 %v", c.role, c.want, s != nil)
		}
	}
}

// TestBuildTeamDispatchSection_优先级 验证 P:15。
func TestBuildTeamDispatchSection_优先级(t *testing.T) {
	s, err := BuildTeamDispatchSection(atschema.TeamRoleLeader, "autonomous", false, false, "cn")
	if err != nil {
		t.Fatalf("返回错误: %v", err)
	}
	if s.Name != SectionDispatch {
		t.Fatalf("section 名应为 %s，实际 %s", SectionDispatch, s.Name)
	}
	if s.Priority != 15 {
		t.Fatalf("priority 应为 15，实际 %d", s.Priority)
	}
}

// TestBuildTeamDispatchSection_两种模式内容不同 验证 autonomous/scheduled 取不同模板。
func TestBuildTeamDispatchSection_两种模式内容不同(t *testing.T) {
	auto, err := BuildTeamDispatchSection(atschema.TeamRoleLeader, "autonomous", false, false, "cn")
	if err != nil {
		t.Fatalf("autonomous 返回错误: %v", err)
	}
	sched, err := BuildTeamDispatchSection(atschema.TeamRoleLeader, "scheduled", false, false, "cn")
	if err != nil {
		t.Fatalf("scheduled 返回错误: %v", err)
	}
	if auto.Render("cn") == sched.Render("cn") {
		t.Fatal("两种调度模式应取不同模板")
	}
}

// TestBuildTeamDispatchSection_未知模式回落autonomous 验证非法值归一。
// Python: `mode = dispatch_mode if dispatch_mode in _DISPATCH_MODES else "autonomous"`
func TestBuildTeamDispatchSection_未知模式回落autonomous(t *testing.T) {
	auto, err := BuildTeamDispatchSection(atschema.TeamRoleLeader, "autonomous", false, false, "cn")
	if err != nil {
		t.Fatalf("返回错误: %v", err)
	}
	unknown, err := BuildTeamDispatchSection(atschema.TeamRoleLeader, "bogus", false, false, "cn")
	if err != nil {
		t.Fatalf("返回错误: %v", err)
	}
	if auto.Render("cn") != unknown.Render("cn") {
		t.Fatal("未知模式应回落 autonomous")
	}
}

// TestBuildTeamDispatchSection_messaging槽位 验证 member_messaging 开闭。
// Python: _scheduled_member_messaging (sections.py L448-461) ——
// leader 两种变体都渲染；teammate 关闭时空串。
func TestBuildTeamDispatchSection_messaging槽位(t *testing.T) {
	leaderOn, err := BuildTeamDispatchSection(atschema.TeamRoleLeader, "scheduled", true, false, "cn")
	if err != nil {
		t.Fatalf("返回错误: %v", err)
	}
	leaderOff, err := BuildTeamDispatchSection(atschema.TeamRoleLeader, "scheduled", false, false, "cn")
	if err != nil {
		t.Fatalf("返回错误: %v", err)
	}
	if leaderOn.Render("cn") == leaderOff.Render("cn") {
		t.Fatal("leader 的 messaging 开闭应产生不同文本")
	}
	if strings.Contains(leaderOn.Render("cn"), "{{member_messaging}}") {
		t.Fatal("槽位应已被替换，不应残留占位符")
	}
	if strings.Contains(leaderOff.Render("cn"), "{{member_messaging}}") {
		t.Fatal("槽位应已被替换，不应残留占位符")
	}
}

// TestBuildTeamDispatchSection_subtasks槽位 验证 member_subtasks 开闭。
func TestBuildTeamDispatchSection_subtasks槽位(t *testing.T) {
	on, err := BuildTeamDispatchSection(atschema.TeamRoleLeader, "scheduled", false, true, "cn")
	if err != nil {
		t.Fatalf("返回错误: %v", err)
	}
	off, err := BuildTeamDispatchSection(atschema.TeamRoleLeader, "scheduled", false, false, "cn")
	if err != nil {
		t.Fatalf("返回错误: %v", err)
	}
	if on.Render("cn") == off.Render("cn") {
		t.Fatal("subtasks 开闭应产生不同文本")
	}
	if strings.Contains(on.Render("cn"), "{{member_subtasks}}") {
		t.Fatal("槽位应已被替换")
	}
}

// TestBuildTeamDispatchSection_inlineMember槽位恒空 验证硬开关关闭。
// Python: constants.INLINE_TASK_MEMBER_ENABLED = False —— 槽位塌成空串。
func TestBuildTeamDispatchSection_inlineMember槽位恒空(t *testing.T) {
	s, err := BuildTeamDispatchSection(atschema.TeamRoleLeader, "scheduled", false, false, "cn")
	if err != nil {
		t.Fatalf("返回错误: %v", err)
	}
	if strings.Contains(s.Render("cn"), "{{inline_member}}") {
		t.Fatal("inline_member 槽位应被替换为空串，不应残留占位符")
	}
}

// TestBuildTeamDispatchSection_英文 验证 en 取英文模板。
func TestBuildTeamDispatchSection_英文(t *testing.T) {
	s, err := BuildTeamDispatchSection(atschema.TeamRoleLeader, "autonomous", false, false, "en")
	if err != nil {
		t.Fatalf("返回错误: %v", err)
	}
	if strings.Contains(s.Render("en"), "任务下发") {
		t.Fatal("en 不应含中文标题")
	}
}

// ──────────────────────────── task_state ────────────────────────────

// TestBuildTeamTaskStateSection_仅Leader 验证非 leader 返回 nil。
func TestBuildTeamTaskStateSection_仅Leader(t *testing.T) {
	if s, err := BuildTeamTaskStateSection(atschema.TeamRoleTeammate, "autonomous", "cn"); err != nil || s != nil {
		t.Fatalf("teammate 应返回 nil,nil，实际 %v,%v", s, err)
	}
	s, err := BuildTeamTaskStateSection(atschema.TeamRoleLeader, "autonomous", "cn")
	if err != nil {
		t.Fatalf("leader 返回错误: %v", err)
	}
	if s == nil {
		t.Fatal("leader 应返回 section")
	}
	if s.Name != SectionTaskState {
		t.Fatalf("section 名应为 %s，实际 %s", SectionTaskState, s.Name)
	}
	if s.Priority != 16 {
		t.Fatalf("priority 应为 16（硬编码，不进 SECTION_PRIORITY 表），实际 %d", s.Priority)
	}
}

// TestBuildTeamTaskStateSection_两种模式 验证 autonomous 无 in_review。
func TestBuildTeamTaskStateSection_两种模式(t *testing.T) {
	auto, err := BuildTeamTaskStateSection(atschema.TeamRoleLeader, "autonomous", "cn")
	if err != nil {
		t.Fatalf("返回错误: %v", err)
	}
	sched, err := BuildTeamTaskStateSection(atschema.TeamRoleLeader, "scheduled", "cn")
	if err != nil {
		t.Fatalf("返回错误: %v", err)
	}
	if strings.Contains(auto.Render("cn"), "in_review") {
		t.Fatal("autonomous 状态机不应出现 in_review（该模式无验证闸）")
	}
	if !strings.Contains(sched.Render("cn"), "in_review") {
		t.Fatal("scheduled 状态机应含 in_review")
	}
}

// TestBuildTeamTaskStateSection_未知模式回落 验证非法值归一。
func TestBuildTeamTaskStateSection_未知模式回落(t *testing.T) {
	auto, err := BuildTeamTaskStateSection(atschema.TeamRoleLeader, "autonomous", "cn")
	if err != nil {
		t.Fatalf("返回错误: %v", err)
	}
	unknown, err := BuildTeamTaskStateSection(atschema.TeamRoleLeader, "bogus", "cn")
	if err != nil {
		t.Fatalf("返回错误: %v", err)
	}
	if auto.Render("cn") != unknown.Render("cn") {
		t.Fatal("未知模式应回落 autonomous")
	}
}

// ──────────────────────────── bridge ────────────────────────────

// TestBuildTeamBridgeSection_仅BridgeAgent 验证角色门控。
func TestBuildTeamBridgeSection_仅BridgeAgent(t *testing.T) {
	for _, role := range []atschema.TeamRole{atschema.TeamRoleLeader, atschema.TeamRoleTeammate, atschema.TeamRoleHumanAgent} {
		if s, err := BuildTeamBridgeSection(role, "cn", ""); err != nil || s != nil {
			t.Fatalf("%s 应返回 nil,nil，实际 %v,%v", role, s, err)
		}
	}
	s, err := BuildTeamBridgeSection(atschema.TeamRoleBridgeAgent, "cn", "bridge_1")
	if err != nil {
		t.Fatalf("bridge 返回错误: %v", err)
	}
	if s == nil {
		t.Fatal("BRIDGE_AGENT 应返回 section")
	}
	if s.Name != SectionBridge {
		t.Fatalf("section 名应为 %s，实际 %s", SectionBridge, s.Name)
	}
	if s.Priority != 12 {
		t.Fatalf("priority 应为 12，实际 %d", s.Priority)
	}
}

// TestBuildTeamBridgeSection_selfLine 验证自身名注入与空值。
func TestBuildTeamBridgeSection_selfLine(t *testing.T) {
	withName, err := BuildTeamBridgeSection(atschema.TeamRoleBridgeAgent, "cn", "bridge_1")
	if err != nil {
		t.Fatalf("返回错误: %v", err)
	}
	if !strings.Contains(withName.Render("cn"), "bridge_1") {
		t.Fatal("应含自身 member_name")
	}
	empty, err := BuildTeamBridgeSection(atschema.TeamRoleBridgeAgent, "cn", "")
	if err != nil {
		t.Fatalf("返回错误: %v", err)
	}
	if strings.Contains(empty.Render("cn"), "你的 member_name 是") {
		t.Fatal("空名不应渲染 self_line")
	}
}

// ──────────────────────────── 统一入口 ────────────────────────────

// staticSectionsTestArgs 统一入口测试的通用参数。
func staticSectionsTestArgs(role atschema.TeamRole) (
	atschema.TeamRole, string, string, string, string, string, bool, bool, string, string, bool, bool, string,
) {
	return role, "bob", "temporary", "build_mode", "default", "autonomous",
		false, false, "", "cn", true, false, "native"
}

// TestBuildTeamStaticSections_Teammate顺序 验证组装顺序与 inbound_tags 收尾。
// Python: build_team_static_sections (sections.py L722-835) —— builders 列表
// 顺序 role→hitt→bridge→workflow→dispatch→task_state→lifecycle→extra，
// 最后无条件 append inbound_tags。
func TestBuildTeamStaticSections_Teammate顺序(t *testing.T) {
	role, mn, lc, tm, teamMode, dm, mm, ms, bp, lang, hitt, expose, wsv := staticSectionsTestArgs(atschema.TeamRoleTeammate)
	got, err := BuildTeamStaticSections(role, mn, lc, tm, teamMode, dm, mm, ms, bp, lang, hitt, expose, wsv)
	if err != nil {
		t.Fatalf("返回错误: %v", err)
	}
	var names []string
	for _, s := range got {
		names = append(names, s.Name)
	}
	want := []string{SectionRole, SectionHITT, SectionDispatch, SectionInboundTags}
	if len(names) != len(want) {
		t.Fatalf("section 数量不符：got=%v want=%v", names, want)
	}
	for i := range want {
		if names[i] != want[i] {
			t.Fatalf("第 %d 个 section 应为 %s，实际 %s（全序 %v）", i, want[i], names[i], names)
		}
	}
}

// TestBuildTeamStaticSections_HITT关闭时无HITT 验证 hittEnabled=false 跳过。
func TestBuildTeamStaticSections_HITT关闭时无HITT(t *testing.T) {
	role, mn, lc, tm, teamMode, dm, mm, ms, bp, lang, _, expose, wsv := staticSectionsTestArgs(atschema.TeamRoleTeammate)
	got, err := BuildTeamStaticSections(role, mn, lc, tm, teamMode, dm, mm, ms, bp, lang, false, expose, wsv)
	if err != nil {
		t.Fatalf("返回错误: %v", err)
	}
	for _, s := range got {
		if s.Name == SectionHITT {
			t.Fatal("hittEnabled=false 时不应有 HITT section")
		}
	}
}

// TestBuildTeamStaticSections_Extra为空时跳过 验证 basePrompt 空串不产生 section。
func TestBuildTeamStaticSections_Extra为空时跳过(t *testing.T) {
	role, mn, lc, tm, teamMode, dm, mm, ms, _, lang, hitt, expose, wsv := staticSectionsTestArgs(atschema.TeamRoleTeammate)
	got, err := BuildTeamStaticSections(role, mn, lc, tm, teamMode, dm, mm, ms, "", lang, hitt, expose, wsv)
	if err != nil {
		t.Fatalf("返回错误: %v", err)
	}
	for _, s := range got {
		if s.Name == SectionExtra {
			t.Fatal("basePrompt 为空时不应有 extra section")
		}
	}
}

// TestBuildTeamStaticSections_Leader全套 验证 leader 调统一入口时拿到全套
// （这是 policy disclosure 的输入，不是 leader 的系统提示词）。
func TestBuildTeamStaticSections_Leader全套(t *testing.T) {
	role, mn, lc, tm, teamMode, dm, mm, ms, bp, lang, hitt, expose, wsv := staticSectionsTestArgs(atschema.TeamRoleLeader)
	got, err := BuildTeamStaticSections(role, mn, lc, tm, teamMode, dm, mm, ms, bp, lang, hitt, expose, wsv)
	if err != nil {
		t.Fatalf("返回错误: %v", err)
	}
	var names []string
	for _, s := range got {
		names = append(names, s.Name)
	}
	// leader: role(11) hitt(12) workflow(13) dispatch(15) task_state(16) lifecycle(14) inbound(18)
	want := []string{SectionRole, SectionHITT, SectionWorkflow, SectionDispatch, SectionTaskState, SectionLifecycle, SectionInboundTags}
	if len(names) != len(want) {
		t.Fatalf("section 数量不符：got=%v want=%v", names, want)
	}
	for i := range want {
		if names[i] != want[i] {
			t.Fatalf("第 %d 个应为 %s，实际 %s（全序 %v）", i, want[i], names[i], names)
		}
	}
}

// ──────────────────────────── policy disclosure ────────────────────────────

// TestBuildLeaderPolicyDisclosure_含关键章节 验证披露含 dispatch/task_state。
// Python: build_leader_policy_disclosure (sections.py L837-903)
func TestBuildLeaderPolicyDisclosure_含关键章节(t *testing.T) {
	got, err := BuildLeaderPolicyDisclosure("temporary", "build_mode", "default", "autonomous", false, false, "cn", true)
	if err != nil {
		t.Fatalf("返回错误: %v", err)
	}
	if got == "" {
		t.Fatal("policy 不应为空")
	}
	for _, want := range []string{"任务下发", "任务状态流转", "工作流程", "团队生命周期"} {
		if !strings.Contains(got, want) {
			t.Fatalf("policy 应含 %q", want)
		}
	}
}

// TestBuildLeaderPolicyDisclosure_不含extra与bootstrap 验证三项排除。
// Python: docstring 明说排除 team_identity / team_extra / team_bootstrap。
func TestBuildLeaderPolicyDisclosure_不含extra与bootstrap(t *testing.T) {
	got, err := BuildLeaderPolicyDisclosure("temporary", "build_mode", "default", "autonomous", false, false, "cn", true)
	if err != nil {
		t.Fatalf("返回错误: %v", err)
	}
	if strings.Contains(got, "第一步就调用") {
		t.Fatal("不应含 bootstrap 的起手指令")
	}
}

// TestBuildLeaderPolicyDisclosure_scheduled含验证闸 验证模式切换生效。
func TestBuildLeaderPolicyDisclosure_scheduled含验证闸(t *testing.T) {
	auto, err := BuildLeaderPolicyDisclosure("temporary", "build_mode", "default", "autonomous", false, false, "cn", false)
	if err != nil {
		t.Fatalf("返回错误: %v", err)
	}
	sched, err := BuildLeaderPolicyDisclosure("temporary", "build_mode", "default", "scheduled", false, true, "cn", false)
	if err != nil {
		t.Fatalf("返回错误: %v", err)
	}
	if strings.Contains(auto, "in_review") {
		t.Fatal("autonomous 的披露不应出现 in_review")
	}
	if !strings.Contains(sched, "in_review") {
		t.Fatal("scheduled 的披露应出现 in_review")
	}
}

package prompts

import (
	"strings"
	"testing"

	atschema "github.com/uapclaw/uapclaw-go/internal/agent_teams/schema"
)

// TestBuildTeamRoleSection_Leader 测试 Leader 角色构建
func TestBuildTeamRoleSection_Leader(t *testing.T) {
	section := BuildTeamRoleSection(atschema.TeamRoleLeader, "alice", "build_mode", "cn")
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
	// 应包含 member_name
	if !strings.Contains(cnContent, "alice") {
		t.Fatal("cn content should contain member_name 'alice'")
	}
	// 应包含 build_mode 标签
	if !strings.Contains(cnContent, "build_mode") {
		t.Fatal("cn content should contain build_mode")
	}
}

// TestBuildTeamRoleSection_LeaderPlanMode 测试 Leader plan_mode
func TestBuildTeamRoleSection_LeaderPlanMode(t *testing.T) {
	section := BuildTeamRoleSection(atschema.TeamRoleLeader, "alice", "plan_mode", "cn")
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
	section := BuildTeamRoleSection(atschema.TeamRoleTeammate, "bob", "build_mode", "en")
	if section == nil {
		t.Fatal("BuildTeamRoleSection returned nil")
	}
	enContent := section.Content["en"]
	if enContent == "" {
		t.Fatal("en content is empty")
	}
	if !strings.Contains(enContent, "bob") {
		t.Fatal("en content should contain member_name 'bob'")
	}
	if !strings.Contains(enContent, "build_mode") {
		t.Fatal("en content should contain build_mode")
	}
}

// TestBuildTeamRoleSection_TeammatePlanMode 测试 Teammate plan_mode
func TestBuildTeamRoleSection_TeammatePlanMode(t *testing.T) {
	section := BuildTeamRoleSection(atschema.TeamRoleTeammate, "bob", "plan_mode", "cn")
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

// TestBuildTeamRoleSection_无MemberName 测试无 member_name 时不显示
func TestBuildTeamRoleSection_无MemberName(t *testing.T) {
	section := BuildTeamRoleSection(atschema.TeamRoleLeader, "", "build_mode", "cn")
	if section == nil {
		t.Fatal("BuildTeamRoleSection returned nil")
	}
	cnContent := section.Content["cn"]
	// 无 member_name 时不应该有 "你的 member_name:" 行
	if strings.Contains(cnContent, "你的 member_name: \n\n") {
		t.Fatal("cn content should not have empty member_name line")
	}
}

// TestBuildTeamRoleSection_English 测试英文 Leader 角色
func TestBuildTeamRoleSection_English(t *testing.T) {
	section := BuildTeamRoleSection(atschema.TeamRoleLeader, "leader1", "plan_mode", "en")
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

// TestBuildTeamHITTSection_Leader 测试 Leader HITT section
func TestBuildTeamHITTSection_Leader(t *testing.T) {
	section := BuildTeamHITTSection(atschema.TeamRoleLeader, []string{"human_1"}, "cn", "alice", false)
	if section == nil {
		t.Fatal("BuildTeamHITTSection returned nil for leader with human agents")
	}
	if section.Priority != 12 {
		t.Fatalf("expected priority 12, got %d", section.Priority)
	}
	if section.Name != SectionHITT {
		t.Fatalf("expected section name %s, got %s", SectionHITT, section.Name)
	}
}

// TestBuildTeamHITTSection_无人类成员返回nil 测试无人类成员时返回 nil
func TestBuildTeamHITTSection_无人类成员返回nil(t *testing.T) {
	section := BuildTeamHITTSection(atschema.TeamRoleLeader, nil, "cn", "alice", false)
	if section != nil {
		t.Fatal("expected nil when no human agents")
	}
	section = BuildTeamHITTSection(atschema.TeamRoleLeader, []string{}, "cn", "alice", false)
	if section != nil {
		t.Fatal("expected nil when empty human agents")
	}
}

// TestBuildTeamHITTSection_Teammate匿名 测试 Teammate 默认匿名变体
func TestBuildTeamHITTSection_Teammate匿名(t *testing.T) {
	section := BuildTeamHITTSection(atschema.TeamRoleTeammate, []string{"human_1"}, "cn", "bob", false)
	if section == nil {
		t.Fatal("BuildTeamHITTSection returned nil for teammate with human agents")
	}
	cnContent := section.Content["cn"]
	// 匿名变体不应包含具体人类成员名
	if strings.Contains(cnContent, "human_1") {
		t.Fatal("anonymous variant should not contain human member name")
	}
	// 匿名变体应包含 "Peer 协作"
	if !strings.Contains(cnContent, "Peer") {
		t.Fatal("anonymous variant should contain 'Peer' heading")
	}
}

// TestBuildTeamHITTSection_Teammate暴露 测试 Teammate 暴露人类成员
func TestBuildTeamHITTSection_Teammate暴露(t *testing.T) {
	section := BuildTeamHITTSection(atschema.TeamRoleTeammate, []string{"human_1"}, "cn", "bob", true)
	if section == nil {
		t.Fatal("BuildTeamHITTSection returned nil for teammate with expose=true")
	}
	cnContent := section.Content["cn"]
	// 暴露变体应包含具体人类成员名
	if !strings.Contains(cnContent, "human_1") {
		t.Fatal("exposed variant should contain human member name")
	}
}

// TestBuildTeamHITTSection_HumanAgent 测试 Human-Agent HITT section
func TestBuildTeamHITTSection_HumanAgent(t *testing.T) {
	section := BuildTeamHITTSection(atschema.TeamRoleHumanAgent, []string{"human_1", "human_2"}, "cn", "human_1", false)
	if section == nil {
		t.Fatal("BuildTeamHITTSection returned nil for human_agent")
	}
	cnContent := section.Content["cn"]
	// 应包含控制者描述
	if !strings.Contains(cnContent, "控制者") {
		t.Fatal("human_agent HITT should contain '控制者'")
	}
	// 应包含自身 member_name
	if !strings.Contains(cnContent, "human_1") {
		t.Fatal("human_agent HITT should contain self member_name")
	}
}

// TestBuildTeamHITTSection_EnglishLeader 测试英文 Leader HITT
func TestBuildTeamHITTSection_EnglishLeader(t *testing.T) {
	section := BuildTeamHITTSection(atschema.TeamRoleLeader, []string{"human_1"}, "en", "leader1", false)
	if section == nil {
		t.Fatal("BuildTeamHITTSection returned nil for English leader")
	}
	enContent := section.Content["en"]
	if !strings.Contains(enContent, "Collaborating with Human Members") {
		t.Fatal("English leader HITT should contain correct heading")
	}
}

// TestBuildTeamHITTSection_EnglishTeammateAnonymous 测试英文 Teammate 匿名 HITT
func TestBuildTeamHITTSection_EnglishTeammateAnonymous(t *testing.T) {
	section := BuildTeamHITTSection(atschema.TeamRoleTeammate, []string{"human_1"}, "en", "bob", false)
	if section == nil {
		t.Fatal("BuildTeamHITTSection returned nil for English teammate")
	}
	enContent := section.Content["en"]
	if !strings.Contains(enContent, "Peer Collaboration") {
		t.Fatal("English anonymous teammate HITT should contain 'Peer Collaboration'")
	}
}

// TestBuildTeamHITTSection_EnglishHumanAgent 测试英文 Human-Agent HITT
func TestBuildTeamHITTSection_EnglishHumanAgent(t *testing.T) {
	section := BuildTeamHITTSection(atschema.TeamRoleHumanAgent, []string{"human_1"}, "en", "human_1", false)
	if section == nil {
		t.Fatal("BuildTeamHITTSection returned nil for English human_agent")
	}
	enContent := section.Content["en"]
	if !strings.Contains(enContent, "controller") {
		t.Fatal("English human_agent HITT should contain 'controller'")
	}
	if !strings.Contains(enContent, "human_1") {
		t.Fatal("English human_agent HITT should contain self member_name")
	}
}

// TestBuildTeamHITTSection_EnglishTeammateExposed 测试英文 Teammate 暴露变体
func TestBuildTeamHITTSection_EnglishTeammateExposed(t *testing.T) {
	section := BuildTeamHITTSection(atschema.TeamRoleTeammate, []string{"human_1"}, "en", "bob", true)
	if section == nil {
		t.Fatal("BuildTeamHITTSection returned nil for English teammate exposed")
	}
	enContent := section.Content["en"]
	if !strings.Contains(enContent, "human_1") {
		t.Fatal("English exposed teammate HITT should contain human member name")
	}
	if !strings.Contains(enContent, "Human Members") {
		t.Fatal("English exposed teammate HITT should contain 'Human Members' heading")
	}
}

// TestBuildTeamHITTSection_未知角色返回nil 测试未知角色返回 nil
func TestBuildTeamHITTSection_未知角色返回nil(t *testing.T) {
	section := BuildTeamHITTSection("unknown_role", []string{"human_1"}, "cn", "alice", false)
	if section != nil {
		t.Fatal("expected nil for unknown role")
	}
}

// TestBuildTeamHITTSection_名字排序 测试人类成员名排序
func TestBuildTeamHITTSection_名字排序(t *testing.T) {
	section := BuildTeamHITTSection(atschema.TeamRoleLeader, []string{"zeta", "alpha"}, "cn", "leader", false)
	if section == nil {
		t.Fatal("BuildTeamHITTSection returned nil")
	}
	cnContent := section.Content["cn"]
	// alpha 应排在 zeta 之前
	alphaIdx := strings.Index(cnContent, "alpha")
	zetaIdx := strings.Index(cnContent, "zeta")
	if alphaIdx >= zetaIdx {
		t.Fatal("human agent names should be sorted alphabetically")
	}
}

// TestBuildTeamWorkflowSection_Leader 测试 Leader 工作流
func TestBuildTeamWorkflowSection_Leader(t *testing.T) {
	section := BuildTeamWorkflowSection(atschema.TeamRoleLeader, "default", "cn")
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
	section := BuildTeamWorkflowSection(atschema.TeamRoleTeammate, "default", "cn")
	if section != nil {
		t.Fatal("expected nil for teammate workflow")
	}
}

// TestBuildTeamWorkflowSection_三种模式 测试 default/predefined/hybrid 三种模式
func TestBuildTeamWorkflowSection_三种模式(t *testing.T) {
	for _, mode := range []string{"default", "predefined", "hybrid"} {
		section := BuildTeamWorkflowSection(atschema.TeamRoleLeader, mode, "cn")
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
	section := BuildTeamWorkflowSection(atschema.TeamRoleLeader, "default", "en")
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
	section := BuildTeamLifecycleSection(atschema.TeamRoleLeader, "temporary", "cn")
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
	section := BuildTeamLifecycleSection(atschema.TeamRoleTeammate, "temporary", "cn")
	if section != nil {
		t.Fatal("expected nil for teammate lifecycle")
	}
}

// TestBuildTeamLifecycleSection_Persistent 测试 persistent 生命周期模板
func TestBuildTeamLifecycleSection_Persistent(t *testing.T) {
	section := BuildTeamLifecycleSection(atschema.TeamRoleLeader, "persistent", "cn")
	if section == nil {
		t.Fatal("BuildTeamLifecycleSection returned nil for persistent lifecycle")
	}
	cnContent := section.Content["cn"]
	if cnContent == "" {
		t.Fatal("cn content is empty for persistent lifecycle")
	}
}

// TestBuildTeamPersonaSection_有persona 测试有 persona 时构建 section
func TestBuildTeamPersonaSection_有persona(t *testing.T) {
	section := BuildTeamPersonaSection("高级工程师", "cn")
	if section == nil {
		t.Fatal("BuildTeamPersonaSection returned nil when persona is set")
	}
	if section.Priority != 15 {
		t.Fatalf("expected priority 15, got %d", section.Priority)
	}
	cnContent := section.Content["cn"]
	if !strings.Contains(cnContent, "高级工程师") {
		t.Fatal("cn content should contain persona text")
	}
}

// TestBuildTeamPersonaSection_空persona返回nil 测试空 persona 返回 nil
func TestBuildTeamPersonaSection_空persona返回nil(t *testing.T) {
	section := BuildTeamPersonaSection("", "cn")
	if section != nil {
		t.Fatal("expected nil when persona is empty")
	}
}

// TestBuildTeamPersonaSection_English 测试英文 persona
func TestBuildTeamPersonaSection_English(t *testing.T) {
	section := BuildTeamPersonaSection("Senior Engineer", "en")
	if section == nil {
		t.Fatal("BuildTeamPersonaSection returned nil for English persona")
	}
	enContent := section.Content["en"]
	if !strings.Contains(enContent, "Senior Engineer") {
		t.Fatal("en content should contain persona text")
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

// TestBuildTeamInfoSection 测试团队信息 section
func TestBuildTeamInfoSection(t *testing.T) {
	info := &TeamInfo{
		TeamName:    "my-team",
		DisplayName: "My Team",
		Description: "A test team",
	}
	section := BuildTeamInfoSection(info, "/.team/workspace", "/abs/path", "cn")
	if section == nil {
		t.Fatal("BuildTeamInfoSection returned nil")
	}
	if section.Priority != 65 {
		t.Fatalf("expected priority 65, got %d", section.Priority)
	}
	cnContent := section.Content["cn"]
	if !strings.Contains(cnContent, "my-team") {
		t.Fatal("cn content should contain team_name")
	}
	if !strings.Contains(cnContent, "My Team") {
		t.Fatal("cn content should contain display_name")
	}
	if !strings.Contains(cnContent, "A test team") {
		t.Fatal("cn content should contain description")
	}
	if !strings.Contains(cnContent, "/.team/workspace") {
		t.Fatal("cn content should contain workspace mount")
	}
	if !strings.Contains(cnContent, "/abs/path") {
		t.Fatal("cn content should contain workspace absolute path")
	}
}

// TestBuildTeamInfoSection_无workspace 测试无工作空间挂载
func TestBuildTeamInfoSection_无workspace(t *testing.T) {
	info := &TeamInfo{TeamName: "test"}
	section := BuildTeamInfoSection(info, "", "", "cn")
	if section == nil {
		t.Fatal("BuildTeamInfoSection returned nil when team_name is set")
	}
	cnContent := section.Content["cn"]
	if strings.Contains(cnContent, "团队共享工作空间") {
		t.Fatal("cn content should not contain workspace section when mount is empty")
	}
}

// TestBuildTeamInfoSection_全空返回nil 测试全空时返回 nil
func TestBuildTeamInfoSection_全空返回nil(t *testing.T) {
	section := BuildTeamInfoSection(nil, "", "", "cn")
	if section != nil {
		t.Fatal("expected nil when all fields are empty")
	}
	section = BuildTeamInfoSection(&TeamInfo{}, "", "", "cn")
	if section != nil {
		t.Fatal("expected nil when TeamInfo is zero-value and no workspace")
	}
}

// TestBuildTeamInfoSection_English 测试英文团队信息
func TestBuildTeamInfoSection_English(t *testing.T) {
	info := &TeamInfo{TeamName: "test", DisplayName: "Test"}
	section := BuildTeamInfoSection(info, "/mount", "", "en")
	if section == nil {
		t.Fatal("BuildTeamInfoSection returned nil")
	}
	enContent := section.Content["en"]
	if !strings.Contains(enContent, "Team Info") {
		t.Fatal("English section should contain 'Team Info' heading")
	}
}

// TestBuildTeamMembersSection 测试成员关系 section
func TestBuildTeamMembersSection(t *testing.T) {
	members := []TeamMember{
		{MemberName: "bob", DisplayName: "Bob", Description: "Engineer"},
		{MemberName: "alice", DisplayName: "Alice", Description: "Leader"},
	}
	section := BuildTeamMembersSection(members, "alice", "cn")
	if section == nil {
		t.Fatal("BuildTeamMembersSection returned nil")
	}
	if section.Priority != 66 {
		t.Fatalf("expected priority 66, got %d", section.Priority)
	}
	if section.Name != SectionMembers {
		t.Fatalf("expected section name %s, got %s", SectionMembers, section.Name)
	}
	cnContent := section.Content["cn"]
	// 应排除自身 alice
	if strings.Contains(cnContent, "member_name=alice") {
		t.Fatal("cn content should exclude self member_name 'alice'")
	}
	// 应包含 bob
	if !strings.Contains(cnContent, "member_name=bob") {
		t.Fatal("cn content should contain member 'bob'")
	}
}

// TestBuildTeamMembersSection_空列表返回nil 测试空成员列表返回 nil
func TestBuildTeamMembersSection_空列表返回nil(t *testing.T) {
	section := BuildTeamMembersSection(nil, "alice", "cn")
	if section != nil {
		t.Fatal("expected nil for nil member list")
	}
	section = BuildTeamMembersSection([]TeamMember{}, "alice", "cn")
	if section != nil {
		t.Fatal("expected nil for empty member list")
	}
}

// TestBuildTeamMembersSection_全排除后返回nil 测试全部排除后返回 nil
func TestBuildTeamMembersSection_全排除后返回nil(t *testing.T) {
	members := []TeamMember{{MemberName: "alice", DisplayName: "Alice"}}
	section := BuildTeamMembersSection(members, "alice", "cn")
	if section != nil {
		t.Fatal("expected nil when only self member in list")
	}
}

// TestBuildTeamMembersSection_无Description 测试成员无描述
func TestBuildTeamMembersSection_无Description(t *testing.T) {
	members := []TeamMember{{MemberName: "bob", DisplayName: "Bob", Description: ""}}
	section := BuildTeamMembersSection(members, "alice", "cn")
	if section == nil {
		t.Fatal("BuildTeamMembersSection returned nil")
	}
	cnContent := section.Content["cn"]
	// 无描述时行尾不应有 " :: "
	if strings.Contains(cnContent, " :: ") {
		t.Fatal("member without description should not have ' :: ' separator")
	}
}

// TestBuildTeamMembersSection_无DisplayName 测试成员无展示名时默认 unknown
func TestBuildTeamMembersSection_无DisplayName(t *testing.T) {
	members := []TeamMember{{MemberName: "bob", DisplayName: "", Description: ""}}
	section := BuildTeamMembersSection(members, "alice", "cn")
	if section == nil {
		t.Fatal("BuildTeamMembersSection returned nil")
	}
	cnContent := section.Content["cn"]
	if !strings.Contains(cnContent, "unknown") {
		t.Fatal("member without display_name should default to 'unknown'")
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
	if SectionPersona != "team_persona" {
		t.Fatalf("expected team_persona, got %s", SectionPersona)
	}
	if SectionExtra != "team_extra" {
		t.Fatalf("expected team_extra, got %s", SectionExtra)
	}
	if SectionInfo != "team_info" {
		t.Fatalf("expected team_info, got %s", SectionInfo)
	}
	if SectionMembers != "team_members" {
		t.Fatalf("expected team_members, got %s", SectionMembers)
	}
}

// TestLabelsFor 测试标签回退
func TestLabelsFor(t *testing.T) {
	// cn 存在
	lbl := labelsFor("cn")
	if lbl["role_heading"] == "" {
		t.Fatal("cn role_heading should not be empty")
	}
	// en 存在
	lbl = labelsFor("en")
	if lbl["role_heading"] == "" {
		t.Fatal("en role_heading should not be empty")
	}
	// 未知语言回退到 cn
	lbl = labelsFor("fr")
	if lbl["role_heading"] != labels["cn"]["role_heading"] {
		t.Fatal("unknown language should fallback to cn")
	}
}

// TestFormatHumanAgentRoster 测试格式化人类成员名列表
func TestFormatHumanAgentRoster(t *testing.T) {
	result := formatHumanAgentRoster([]string{"alice", "bob"}, "cn")
	if !strings.Contains(result, "注册的人类成员") {
		t.Fatal("cn roster should contain '注册的人类成员'")
	}
	if !strings.Contains(result, "`alice`") {
		t.Fatal("cn roster should contain backtick-quoted alice")
	}

	result = formatHumanAgentRoster([]string{"alice"}, "en")
	if !strings.Contains(result, "Registered human members") {
		t.Fatal("en roster should contain 'Registered human members'")
	}
}

// TestAllBuilders 返回类型验证
func TestAllBuilders(t *testing.T) {
	// 验证所有 builder 返回正确的 PromptSection 类型
	var _ = BuildTeamRoleSection(atschema.TeamRoleLeader, "a", "build_mode", "cn")
	var _ = BuildTeamWorkflowSection(atschema.TeamRoleLeader, "default", "cn")
	var _ = BuildTeamLifecycleSection(atschema.TeamRoleLeader, "temporary", "cn")
	var _ = BuildTeamPersonaSection("p", "cn")
	var _ = BuildTeamExtraSection("e", "cn")
	var _ = BuildTeamInfoSection(&TeamInfo{TeamName: "t"}, "", "", "cn")
	var _ = BuildTeamMembersSection([]TeamMember{{MemberName: "b"}}, "", "cn")
	var _ = BuildTeamHITTSection(atschema.TeamRoleLeader, []string{"h"}, "cn", "", false)
}

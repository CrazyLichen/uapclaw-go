package prompts

import (
	"os"
	"path/filepath"
	"testing"

	atschema "github.com/uapclaw/uapclaw-go/internal/agent_teams/schema"
)

// goldenPath 返回 messages golden 的绝对路径。
//
// golden 随 inbound_render 一起下沉到 inboundtags 子包（Task 5b：渲染层搬到
// 零依赖叶子包），此处路径同步指向新位置。
func goldenPath(t *testing.T, name string) string {
	t.Helper()
	return filepath.Join("..", "inboundtags", "testdata", "inbound_render", name+".golden")
}

// assertGolden 逐字节比对代码输出与上游 golden。
func assertGolden(t *testing.T, name, got string) {
	t.Helper()
	want, err := os.ReadFile(goldenPath(t, name))
	if err != nil {
		t.Fatalf("读取 golden %s 失败：%v", name, err)
	}
	if got != string(want) {
		t.Errorf("golden %s 不一致\n得到: %q\n期望: %q", name, got, string(want))
	}
}

// assertGoldenNil 断言返回 nil，且对应 golden 是空文件（上游返回 None）。
func assertGoldenNil(t *testing.T, name string, got *string) {
	t.Helper()
	if got != nil {
		t.Errorf("期望 nil，得到 %q", *got)
	}
	info, err := os.Stat(goldenPath(t, name))
	if err != nil {
		t.Fatalf("读取 golden %s 失败：%v", name, err)
	}
	if info.Size() != 0 {
		t.Errorf("golden %s 应为空文件（上游返回 None），实际 %d 字节", name, info.Size())
	}
}

// TestDiffRoster_全新增保序 验证 joined 按 new 列表顺序。
func TestDiffRoster_全新增保序(t *testing.T) {
	got := DiffRoster(nil, []RosterMember{
		{MemberName: "b", DisplayName: "B"},
		{MemberName: "a", DisplayName: "A"},
		{MemberName: "c", DisplayName: "C"},
	})
	if len(got.Joined) != 3 {
		t.Fatalf("joined 应为 3 条，得到 %d", len(got.Joined))
	}
	want := []string{"b", "a", "c"}
	for i, name := range want {
		if got.Joined[i].MemberName != name {
			t.Errorf("joined[%d] = %q，期望 %q（须保 new 列表顺序）", i, got.Joined[i].MemberName, name)
		}
	}
}

// TestDiffRoster_全退出保序 验证 left 按 old 列表顺序。
func TestDiffRoster_全退出保序(t *testing.T) {
	got := DiffRoster([]RosterMember{
		{MemberName: "x"}, {MemberName: "y"},
	}, nil)
	if len(got.Left) != 2 {
		t.Fatalf("left 应为 2 条，得到 %d", len(got.Left))
	}
	if got.Left[0].MemberName != "x" || got.Left[1].MemberName != "y" {
		t.Errorf("left 顺序错误：%v", got.Left)
	}
}

// TestDiffRoster_四个追踪字段 验证 display_name/desc/role/models_used 任一变化都算 changed。
func TestDiffRoster_四个追踪字段(t *testing.T) {
	base := RosterMember{MemberName: "a", DisplayName: "A", Desc: "d", Role: atschema.TeamRoleTeammate, ModelsUsed: "m1"}
	cases := []struct {
		name string
		mod  func(RosterMember) RosterMember
	}{
		{"display_name", func(m RosterMember) RosterMember { m.DisplayName = "A2"; return m }},
		{"desc", func(m RosterMember) RosterMember { m.Desc = "d2"; return m }},
		{"role", func(m RosterMember) RosterMember { m.Role = atschema.TeamRoleLeader; return m }},
		{"models_used", func(m RosterMember) RosterMember { m.ModelsUsed = "m2"; return m }},
	}
	for _, c := range cases {
		got := DiffRoster([]RosterMember{base}, []RosterMember{c.mod(base)})
		if len(got.Changed) != 1 {
			t.Errorf("%s 变化应产生 1 条 changed，得到 %d", c.name, len(got.Changed))
		}
	}
}

// TestDiffRoster_未追踪字段不算变更 验证 model_group 变化不触发 changed。
func TestDiffRoster_未追踪字段不算变更(t *testing.T) {
	old := RosterMember{MemberName: "a", ModelGroup: "g1"}
	newer := RosterMember{MemberName: "a", ModelGroup: "g2"}
	got := DiffRoster([]RosterMember{old}, []RosterMember{newer})
	if !got.IsEmpty() {
		t.Errorf("model_group 不在追踪字段内，应无变更：%+v", got)
	}
}

// TestDiffRoster_无变更时IsEmpty为真 验证完全相同的名册 diff 为空。
func TestDiffRoster_无变更时IsEmpty为真(t *testing.T) {
	same := []RosterMember{{MemberName: "a", DisplayName: "A", Desc: "d", Role: atschema.TeamRoleTeammate}}
	got := DiffRoster(same, same)
	if !got.IsEmpty() {
		t.Errorf("完全相同的名册应无变更：%+v", got)
	}
	if len(got.Joined) != 0 || len(got.Left) != 0 || len(got.Changed) != 0 {
		t.Errorf("三个列表都应为空：%+v", got)
	}
}

// TestDiffRoster_重复memberName去重 验证与 Python dict 语义一致：
// 值取后写覆盖、位置取首次插入，重复名只出现一次。
func TestDiffRoster_重复memberName去重(t *testing.T) {
	got := DiffRoster(nil, []RosterMember{
		{MemberName: "a", DisplayName: "A1"},
		{MemberName: "a", DisplayName: "A2"},
	})
	if len(got.Joined) != 1 {
		t.Fatalf("重复 member_name 应只出现一次，得到 %d：%+v", len(got.Joined), got.Joined)
	}
	if got.Joined[0].DisplayName != "A2" {
		t.Errorf("值应取后写覆盖：得到 %q，期望 A2", got.Joined[0].DisplayName)
	}
}

// TestDiffRoster_nil与空切片等价 验证两边 nil 与空切片都当空。
func TestDiffRoster_nil与空切片等价(t *testing.T) {
	if !DiffRoster(nil, nil).IsEmpty() {
		t.Error("nil/nil 应为空")
	}
	if !DiffRoster([]RosterMember{}, []RosterMember{}).IsEmpty() {
		t.Error("空切片/空切片 应为空")
	}
	if !DiffRoster(nil, []RosterMember{}).IsEmpty() {
		t.Error("nil/空切片 应为空")
	}
}

// TestFormatMemberLine_基础 验证基础行格式。
func TestFormatMemberLine_基础(t *testing.T) {
	got := FormatMemberLine(RosterMember{MemberName: "dev-1", DisplayName: "开发一", Desc: "后端"}, false, "")
	want := "- member_name=dev-1 display_name=开发一 :: 后端"
	if got != want {
		t.Errorf("得到 %q\n期望 %q", got, want)
	}
}

// TestFormatMemberLine_带desc 验证 desc 渲染为 " :: " 后缀。
func TestFormatMemberLine_带desc(t *testing.T) {
	got := FormatMemberLine(RosterMember{MemberName: "dev-1", DisplayName: "开发一", Desc: "后端"}, false, "")
	if !stringsContains(got, " :: 后端") {
		t.Errorf("desc 应渲染为 :: 后缀：%q", got)
	}
}

// TestFormatMemberLine_人类标记 验证两种人类角色都加 [human]，其余不加。
func TestFormatMemberLine_人类标记(t *testing.T) {
	cases := []struct {
		role atschema.TeamRole
		want bool
	}{
		{atschema.TeamRoleHumanAgent, true},
		{atschema.TeamRolePassiveHuman, true},
		{atschema.TeamRoleLeader, false},
		{atschema.TeamRoleTeammate, false},
		{atschema.TeamRoleExternalCLI, false},
	}
	for _, c := range cases {
		got := FormatMemberLine(RosterMember{MemberName: "m", DisplayName: "M", Role: c.role}, true, "")
		has := stringsContains(got, "[human]")
		if has != c.want {
			t.Errorf("role=%s markHumans=true：得到 %q，[human] 期望 %v", c.role, got, c.want)
		}
	}
}

// TestFormatMemberLine_markHumans为假 验证人类角色也不加标记。
func TestFormatMemberLine_markHumans为假(t *testing.T) {
	got := FormatMemberLine(RosterMember{MemberName: "h1", DisplayName: "张三", Role: atschema.TeamRolePassiveHuman}, false, "")
	if stringsContains(got, "[human]") {
		t.Errorf("markHumans=false 不应加 [human]：%q", got)
	}
}

// TestFormatMemberLine_字段顺序 验证 model_group → models_used → [human] → desc。
func TestFormatMemberLine_字段顺序(t *testing.T) {
	got := FormatMemberLine(RosterMember{
		MemberName: "dev-1", DisplayName: "开发一", Desc: "后端",
		Role: atschema.TeamRoleHumanAgent, ModelGroup: "g1", ModelsUsed: "a,b",
	}, true, "")
	want := "- member_name=dev-1 display_name=开发一 model_group=g1 models_used=a,b [human] :: 后端"
	if got != want {
		t.Errorf("得到 %q\n期望 %q", got, want)
	}
}

// TestFormatMemberLine_空displayName 验证空展示名渲染为 unknown。
func TestFormatMemberLine_空displayName(t *testing.T) {
	got := FormatMemberLine(RosterMember{MemberName: "dev-1"}, false, "")
	want := "- member_name=dev-1 display_name=unknown"
	if got != want {
		t.Errorf("得到 %q\n期望 %q", got, want)
	}
}

// TestFormatMemberLine_带prefix 验证前缀渲染为 [prefix]。
func TestFormatMemberLine_带prefix(t *testing.T) {
	got := FormatMemberLine(RosterMember{MemberName: "dev-1", DisplayName: "开发一"}, false, "加入")
	want := "- [加入] member_name=dev-1 display_name=开发一"
	if got != want {
		t.Errorf("得到 %q\n期望 %q", got, want)
	}
}

// TestBuildIdentityText_全字段 验证完整身份正文（golden）。
func TestBuildIdentityText_全字段(t *testing.T) {
	got := BuildIdentityText("dev-1", "开发一", "/ws/dev-1", "只改 internal/", "cn", false)
	if got == nil {
		t.Fatal("不应返回 nil")
	}
	assertGolden(t, "identity_text_full", *got)
}

// TestBuildIdentityText_最少字段 验证只有 member_name 时的正文。
func TestBuildIdentityText_最少字段(t *testing.T) {
	got := BuildIdentityText("dev-1", "", "", "", "cn", false)
	if got == nil {
		t.Fatal("不应返回 nil")
	}
	assertGolden(t, "identity_text_minimal", *got)
}

// TestBuildIdentityText_中文全角括号 验证中文用全角括号且不带前导空格。
func TestBuildIdentityText_中文全角括号(t *testing.T) {
	got := BuildIdentityText("dev-1", "", "/ws/dev-1", "", "cn", false)
	if got == nil {
		t.Fatal("不应返回 nil")
	}
	if !stringsContains(*got, "`（存放你自己的产物") {
		t.Errorf("中文应用全角括号且紧贴反引号：%q", *got)
	}
}

// TestBuildIdentityText_forkCapable 验证能力声明前置于身份块。
func TestBuildIdentityText_forkCapable(t *testing.T) {
	got := BuildIdentityText("dev-1", "开发一", "", "", "cn", true)
	if got == nil {
		t.Fatal("不应返回 nil")
	}
	assertGolden(t, "identity_text_fork_capable", *got)
}

// TestBuildIdentityText_英文全角半角括号 验证英文用半角括号且带前导空格。
func TestBuildIdentityText_英文全角半角括号(t *testing.T) {
	got := BuildIdentityText("dev-1", "Dev One", "/ws/dev-1", "internal only", "en", false)
	if got == nil {
		t.Fatal("不应返回 nil")
	}
	assertGolden(t, "identity_text_en_full", *got)
}

// TestBuildIdentityText_全空 验证所有字段为空时返回 nil。
func TestBuildIdentityText_全空(t *testing.T) {
	assertGoldenNil(t, "identity_text_all_empty", BuildIdentityText("", "", "", "", "cn", false))
}

// TestBuildIdentityPromptDelta 验证只渲染私有工作约定小节，首字符为换行。
func TestBuildIdentityPromptDelta(t *testing.T) {
	got := BuildIdentityPromptDelta("只改 internal/", "cn")
	if got == nil {
		t.Fatal("不应返回 nil")
	}
	assertGolden(t, "identity_prompt_delta", *got)
}

// TestBuildIdentityPromptDelta_空prompt 验证空白 prompt 返回 nil。
func TestBuildIdentityPromptDelta_空prompt(t *testing.T) {
	assertGoldenNil(t, "identity_prompt_delta_empty", BuildIdentityPromptDelta("   ", "cn"))
}

// TestBuildIdentityConversion 验证 fork 转换通知的变量替换。
func TestBuildIdentityConversion(t *testing.T) {
	assertGolden(t, "identity_conversion", BuildIdentityConversion("dev-0", "dev-1", "cn"))
}

// TestBuildIdentityConversion_空memberName 验证空名填 "?"。
func TestBuildIdentityConversion_空memberName(t *testing.T) {
	got := BuildIdentityConversion("dev-0", "", "cn")
	if !stringsContains(got, "你的身份现在是 ?。") {
		t.Errorf("空 member_name 应填 ?：%q", got)
	}
}

// TestBuildTeamInfoText_全字段 验证完整团队信息正文（含 workspace 两条子项）。
func TestBuildTeamInfoText_全字段(t *testing.T) {
	got := BuildTeamInfoText(map[string]string{
		"team_name": "t1", "display_name": "团队一", "desc": "做后端",
	}, "/ws/team", "/ws/team/out", "cn")
	if got == nil {
		t.Fatal("不应返回 nil")
	}
	assertGolden(t, "team_info_text_full", *got)
}

// TestBuildTeamInfoText_全空 验证无可用字段时返回 nil。
func TestBuildTeamInfoText_全空(t *testing.T) {
	assertGoldenNil(t, "team_info_text_all_empty", BuildTeamInfoText(nil, "", "", "cn"))
}

// TestBuildRosterSnapshotText 验证名册全量正文（golden）。
func TestBuildRosterSnapshotText(t *testing.T) {
	got := BuildRosterSnapshotText([]RosterMember{
		{MemberName: "dev-1", DisplayName: "开发一", Desc: "后端", Role: atschema.TeamRoleTeammate},
	}, false, "cn")
	if got == nil {
		t.Fatal("不应返回 nil")
	}
	assertGolden(t, "roster_snapshot", *got)
}

// TestBuildRosterSnapshotText_空列表 验证无同伴时返回 nil。
func TestBuildRosterSnapshotText_空列表(t *testing.T) {
	assertGoldenNil(t, "roster_snapshot_empty", BuildRosterSnapshotText(nil, false, "cn"))
}

// TestBuildRosterDeltaText 验证三段顺序 joined → left → changed（golden）。
func TestBuildRosterDeltaText(t *testing.T) {
	delta := DiffRoster(
		[]RosterMember{{MemberName: "a", DisplayName: "A", Role: atschema.TeamRoleTeammate}},
		[]RosterMember{
			{MemberName: "a", DisplayName: "A2", Role: atschema.TeamRoleTeammate},
			{MemberName: "b", DisplayName: "B", Role: atschema.TeamRoleTeammate},
		},
	)
	got := BuildRosterDeltaText(delta, false, "cn")
	if got == nil {
		t.Fatal("不应返回 nil")
	}
	assertGolden(t, "roster_delta", *got)
}

// TestBuildRosterDeltaText_空delta 验证无变更时返回 nil。
func TestBuildRosterDeltaText_空delta(t *testing.T) {
	if got := BuildRosterDeltaText(RosterDelta{}, false, "cn"); got != nil {
		t.Errorf("空 delta 应返回 nil，得到 %q", *got)
	}
}

// TestBuildRosterDeltaText_英文 验证英文标签（joined/left/updated）。
func TestBuildRosterDeltaText_英文(t *testing.T) {
	delta := DiffRoster(
		[]RosterMember{{MemberName: "a", DisplayName: "A", Role: atschema.TeamRoleTeammate}},
		[]RosterMember{{MemberName: "b", DisplayName: "B", Role: atschema.TeamRoleTeammate}},
	)
	got := BuildRosterDeltaText(delta, false, "en")
	if got == nil {
		t.Fatal("不应返回 nil")
	}
	assertGolden(t, "roster_delta_en", *got)
}

// stringsContains 避免为断言引入 strings 导入
func stringsContains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

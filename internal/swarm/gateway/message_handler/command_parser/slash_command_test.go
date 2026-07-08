package command_parser

import (
	"testing"
)

// ──────────────────────────── 导出函数测试 ────────────────────────────

// TestParseChannelControlText_空文本 测试空文本返回 NONE
func TestParseChannelControlText_空文本(t *testing.T) {
	result := ParseChannelControlText("")
	if result.Action != ActionNone {
		t.Errorf("空文本应返回 ActionNone，实际：%v", result.Action)
	}
}

// TestParseChannelControlText_含换行 测试含换行返回 NONE
func TestParseChannelControlText_含换行(t *testing.T) {
	result := ParseChannelControlText("/new_session\n")
	if result.Action != ActionNone {
		t.Errorf("含换行应返回 ActionNone，实际：%v", result.Action)
	}
	result = ParseChannelControlText("/mode agent\nmore")
	if result.Action != ActionNone {
		t.Errorf("含换行应返回 ActionNone，实际：%v", result.Action)
	}
}

// TestParseChannelControlText_NewSession 测试 /new_session 解析
func TestParseChannelControlText_NewSession(t *testing.T) {
	// 精确匹配 → OK
	result := ParseChannelControlText("/new_session")
	if result.Action != ActionNewSessionOK {
		t.Errorf("/new_session 应返回 ActionNewSessionOK，实际：%v", result.Action)
	}
	// 带后缀 → BAD
	result = ParseChannelControlText("/new_session extra")
	if result.Action != ActionNewSessionBad {
		t.Errorf("/new_session extra 应返回 ActionNewSessionBad，实际：%v", result.Action)
	}
	// 前缀部分匹配 → BAD
	result = ParseChannelControlText("/new_sessionx")
	if result.Action != ActionNewSessionBad {
		t.Errorf("/new_sessionx 应返回 ActionNewSessionBad，实际：%v", result.Action)
	}
}

// TestParseChannelControlText_SkillsList 测试 /skills list 解析
func TestParseChannelControlText_SkillsList(t *testing.T) {
	// 精确匹配 → OK
	result := ParseChannelControlText("/skills list")
	if result.Action != ActionSkillsOK {
		t.Errorf("/skills list 应返回 ActionSkillsOK，实际：%v", result.Action)
	}
	// 折叠空白匹配
	result = ParseChannelControlText("/skills  list")
	if result.Action != ActionSkillsOK {
		t.Errorf("/skills  list 应返回 ActionSkillsOK（折叠空白），实际：%v", result.Action)
	}
	// 仅 /skills → NONE
	result = ParseChannelControlText("/skills")
	if result.Action != ActionNone {
		t.Errorf("/skills 应返回 ActionNone，实际：%v", result.Action)
	}
}

// TestParseChannelControlText_Mode 测试 /mode 解析
func TestParseChannelControlText_Mode(t *testing.T) {
	// 合法 /mode 行
	validModes := []string{
		"/mode agent", "/mode code", "/mode team",
		"/mode agent.plan", "/mode agent.fast",
		"/mode code.plan", "/mode code.normal", "/mode code.team",
	}
	for _, text := range validModes {
		result := ParseChannelControlText(text)
		if result.Action != ActionModeOK {
			t.Errorf("%s 应返回 ActionModeOK，实际：%v", text, result.Action)
		}
		// 验证子命令提取
		expected := text[len("/mode "):]
		if result.ModeSubcommand != expected {
			t.Errorf("%s 的 ModeSubcommand 应为 %q，实际：%q", text, expected, result.ModeSubcommand)
		}
	}

	// 非法 /mode 行
	invalidModes := []string{
		"/mode", "/mode foo", "/mode agent.slow", "/mode code.fast",
	}
	for _, text := range invalidModes {
		result := ParseChannelControlText(text)
		if result.Action != ActionModeBad {
			t.Errorf("%s 应返回 ActionModeBad，实际：%v", text, result.Action)
		}
	}
}

// TestParseChannelControlText_Switch 测试 /switch 解析
func TestParseChannelControlText_Switch(t *testing.T) {
	// 合法 /switch 行
	validSwitches := []string{
		"/switch plan", "/switch fast", "/switch normal", "/switch team",
	}
	for _, text := range validSwitches {
		result := ParseChannelControlText(text)
		if result.Action != ActionSwitchOK {
			t.Errorf("%s 应返回 ActionSwitchOK，实际：%v", text, result.Action)
		}
		expected := text[len("/switch "):]
		if result.SwitchSubcommand != expected {
			t.Errorf("%s 的 SwitchSubcommand 应为 %q，实际：%q", text, expected, result.SwitchSubcommand)
		}
	}

	// 非法 /switch 行
	invalidSwitches := []string{
		"/switch", "/switch foo", "/switch agent",
	}
	for _, text := range invalidSwitches {
		result := ParseChannelControlText(text)
		if result.Action != ActionSwitchBad {
			t.Errorf("%s 应返回 ActionSwitchBad，实际：%v", text, result.Action)
		}
	}
}

// TestParseChannelControlText_Branch 测试 /branch 解析
func TestParseChannelControlText_Branch(t *testing.T) {
	// 无名称
	result := ParseChannelControlText("/branch")
	if result.Action != ActionBranchOK {
		t.Errorf("/branch 应返回 ActionBranchOK，实际：%v", result.Action)
	}
	if result.BranchName != "" {
		t.Errorf("/branch 的 BranchName 应为空字符串，实际：%q", result.BranchName)
	}

	// 带名称
	result = ParseChannelControlText("/branch my-feature")
	if result.Action != ActionBranchOK {
		t.Errorf("/branch my-feature 应返回 ActionBranchOK，实际：%v", result.Action)
	}
	if result.BranchName != "my-feature" {
		t.Errorf("/branch my-feature 的 BranchName 应为 %q，实际：%q", "my-feature", result.BranchName)
	}

	// 带空格的名称
	result = ParseChannelControlText("/branch fix bug 123")
	if result.Action != ActionBranchOK {
		t.Errorf("/branch fix bug 123 应返回 ActionBranchOK，实际：%v", result.Action)
	}
	if result.BranchName != "fix bug 123" {
		t.Errorf("/branch fix bug 123 的 BranchName 应为 %q，实际：%q", "fix bug 123", result.BranchName)
	}
}

// TestParseChannelControlText_Rewind 测试 /rewind 解析
func TestParseChannelControlText_Rewind(t *testing.T) {
	// 无参数 → BAD
	result := ParseChannelControlText("/rewind")
	if result.Action != ActionRewindBad {
		t.Errorf("/rewind 应返回 ActionRewindBad，实际：%v", result.Action)
	}

	// 合法数字
	result = ParseChannelControlText("/rewind 3")
	if result.Action != ActionRewindOK {
		t.Errorf("/rewind 3 应返回 ActionRewindOK，实际：%v", result.Action)
	}
	if result.RewindTurn != 3 {
		t.Errorf("/rewind 3 的 RewindTurn 应为 3，实际：%d", result.RewindTurn)
	}

	// 非法：负数
	result = ParseChannelControlText("/rewind -1")
	if result.Action != ActionRewindBad {
		t.Errorf("/rewind -1 应返回 ActionRewindBad，实际：%v", result.Action)
	}

	// 非法：零
	result = ParseChannelControlText("/rewind 0")
	if result.Action != ActionRewindBad {
		t.Errorf("/rewind 0 应返回 ActionRewindBad，实际：%v", result.Action)
	}

	// 非法：非数字
	result = ParseChannelControlText("/rewind abc")
	if result.Action != ActionRewindBad {
		t.Errorf("/rewind abc 应返回 ActionRewindBad，实际：%v", result.Action)
	}

	// /rewind cancel
	result = ParseChannelControlText("/rewind cancel")
	if result.Action != ActionRewindCancel {
		t.Errorf("/rewind cancel 应返回 ActionRewindCancel，实际：%v", result.Action)
	}

	// /rewind confirm N
	result = ParseChannelControlText("/rewind confirm 5")
	if result.Action != ActionRewindConfirm {
		t.Errorf("/rewind confirm 5 应返回 ActionRewindConfirm，实际：%v", result.Action)
	}
	if result.RewindTurn != 5 {
		t.Errorf("/rewind confirm 5 的 RewindTurn 应为 5，实际：%d", result.RewindTurn)
	}

	// /rewind confirm 非法数字
	result = ParseChannelControlText("/rewind confirm 0")
	if result.Action != ActionRewindBad {
		t.Errorf("/rewind confirm 0 应返回 ActionRewindBad，实际：%v", result.Action)
	}

	// /rewind confirm 非数字
	result = ParseChannelControlText("/rewind confirm abc")
	if result.Action != ActionRewindBad {
		t.Errorf("/rewind confirm abc 应返回 ActionRewindBad，实际：%v", result.Action)
	}
}

// TestParseChannelControlText_非控制文本 测试非控制文本
func TestParseChannelControlText_非控制文本(t *testing.T) {
	nonControl := []string{
		"hello", "你好", "请帮我写代码", "/help", "/unknown",
	}
	for _, text := range nonControl {
		result := ParseChannelControlText(text)
		if result.Action != ActionNone {
			t.Errorf("%q 应返回 ActionNone，实际：%v", text, result.Action)
		}
	}
}

// TestParseChannelControlText_前后空白 测试前后空白处理
func TestParseChannelControlText_前后空白(t *testing.T) {
	result := ParseChannelControlText("  /new_session  ")
	if result.Action != ActionNewSessionOK {
		t.Errorf("前后空白的 /new_session 应返回 ActionNewSessionOK，实际：%v", result.Action)
	}
	result = ParseChannelControlText("\t/mode agent\t")
	if result.Action != ActionModeOK {
		t.Errorf("前后 tab 的 /mode agent 应返回 ActionModeOK，实际：%v", result.Action)
	}
}

// TestIsControlLikeForIMBatching 测试 IM 批处理控制判断
func TestIsControlLikeForIMBatching(t *testing.T) {
	// 空文本
	if IsControlLikeForIMBatching("") {
		t.Error("空文本应返回 false")
	}

	// 含换行
	if IsControlLikeForIMBatching("/new_session\n") {
		t.Error("含换行应返回 false")
	}

	// 精确控制消息
	controlTexts := []string{
		"/new_session", "/mode agent", "/switch plan",
		"/skills list", "/branch", "/rewind",
	}
	for _, text := range controlTexts {
		if !IsControlLikeForIMBatching(text) {
			t.Errorf("%q 应返回 true", text)
		}
	}

	// 前缀匹配也应返回 true
	prefixTexts := []string{
		"/mode foo", "/switch bar", "/new_session extra",
		"/branch my-branch", "/rewind 5",
		"/switch", // 仅 /switch 本身
	}
	for _, text := range prefixTexts {
		if !IsControlLikeForIMBatching(text) {
			t.Errorf("%q 应返回 true（前缀匹配）", text)
		}
	}

	// 非控制文本
	nonControl := []string{"hello", "写代码", "/help"}
	for _, text := range nonControl {
		if IsControlLikeForIMBatching(text) {
			t.Errorf("%q 应返回 false", text)
		}
	}

	// 折叠空白的 /skills list
	if !IsControlLikeForIMBatching("/skills  list") {
		t.Error("/skills  list 应返回 true（折叠空白）")
	}
}

// TestFormatSkillsListForNotice_空payload 测试空 payload
func TestFormatSkillsListForNotice_空payload(t *testing.T) {
	result := FormatSkillsListForNotice(nil, 50)
	if result != "暂无技能数据。" {
		t.Errorf("nil payload 应返回 '暂无技能数据。'，实际：%q", result)
	}
}

// TestFormatSkillsListForNotice_错误 测试错误响应
func TestFormatSkillsListForNotice_错误(t *testing.T) {
	result := FormatSkillsListForNotice(map[string]any{"error": "timeout"}, 50)
	expected := "获取技能列表失败：timeout"
	if result != expected {
		t.Errorf("错误 payload 应返回 %q，实际：%q", expected, result)
	}
}

// TestFormatSkillsListForNotice_空列表 测试空技能列表
func TestFormatSkillsListForNotice_空列表(t *testing.T) {
	result := FormatSkillsListForNotice(map[string]any{"skills": []any{}}, 50)
	if result != "当前无可用技能。" {
		t.Errorf("空列表应返回 '当前无可用技能。'，实际：%q", result)
	}
}

// TestFormatSkillsListForNotice_正常列表 测试正常技能列表
func TestFormatSkillsListForNotice_正常列表(t *testing.T) {
	payload := map[string]any{
		"skills": []any{
			map[string]any{"name": "搜索", "description": "网页搜索技能", "source": "builtin"},
			map[string]any{"name": "代码", "description": "代码生成技能"},
		},
	}
	result := FormatSkillsListForNotice(payload, 50)
	if result == "" {
		t.Error("正常列表应返回非空字符串")
	}
	// 验证包含技能名称
	if !containsSubstring(result, "搜索") {
		t.Errorf("结果应包含技能名称 '搜索'，实际：%q", result)
	}
	if !containsSubstring(result, "代码") {
		t.Errorf("结果应包含技能名称 '代码'，实际：%q", result)
	}
}

// TestFormatSkillsListForNotice_截断描述 测试描述超过 200 字符截断
func TestFormatSkillsListForNotice_截断描述(t *testing.T) {
	longDesc := ""
	for i := 0; i < 300; i++ {
		longDesc += "a"
	}
	payload := map[string]any{
		"skills": []any{
			map[string]any{"name": "长描述技能", "description": longDesc},
		},
	}
	result := FormatSkillsListForNotice(payload, 50)
	if !containsSubstring(result, "…") {
		t.Errorf("长描述应被截断并带省略号，实际：%q", result)
	}
}

// TestFormatSkillsListForNotice_超出maxItems 测试超出 maxItems 截断
func TestFormatSkillsListForNotice_超出maxItems(t *testing.T) {
	skills := make([]any, 5)
	for i := 0; i < 5; i++ {
		skills[i] = map[string]any{"name": string(rune('A' + i))}
	}
	payload := map[string]any{"skills": skills}
	result := FormatSkillsListForNotice(payload, 3)
	if !containsSubstring(result, "共 5 项") {
		t.Errorf("超出 maxItems 应显示总数提示，实际：%q", result)
	}
	if !containsSubstring(result, "仅显示前 3 项") {
		t.Errorf("超出 maxItems 应显示截断提示，实际：%q", result)
	}
}

// TestValidModeLines 测试合法 mode 行集合
func TestValidModeLines(t *testing.T) {
	expected := []string{
		"/mode agent", "/mode code", "/mode team",
		"/mode agent.plan", "/mode agent.fast",
		"/mode code.plan", "/mode code.normal", "/mode code.team",
	}
	for _, line := range expected {
		if !ValidModeLines[line] {
			t.Errorf("%q 应在 ValidModeLines 中", line)
		}
	}
	if len(ValidModeLines) != len(expected) {
		t.Errorf("ValidModeLines 应有 %d 项，实际：%d", len(expected), len(ValidModeLines))
	}
}

// TestValidSwitchLines 测试合法 switch 行集合
func TestValidSwitchLines(t *testing.T) {
	expected := []string{
		"/switch plan", "/switch fast", "/switch normal", "/switch team",
	}
	for _, line := range expected {
		if !ValidSwitchLines[line] {
			t.Errorf("%q 应在 ValidSwitchLines 中", line)
		}
	}
	if len(ValidSwitchLines) != len(expected) {
		t.Errorf("ValidSwitchLines 应有 %d 项，实际：%d", len(expected), len(ValidSwitchLines))
	}
}

// TestControlMessageTexts 测试控制消息全集
func TestControlMessageTexts(t *testing.T) {
	// 应包含所有合法控制消息
	mustContain := []string{
		"/new_session", "/mode agent", "/switch plan",
		"/skills list", "/branch", "/rewind",
	}
	for _, text := range mustContain {
		if !ControlMessageTexts[text] {
			t.Errorf("%q 应在 ControlMessageTexts 中", text)
		}
	}
}

// TestFirstBatchRegistry 测试第一批命令注册表
func TestFirstBatchRegistry(t *testing.T) {
	if len(FirstBatchRegistry) != 10 {
		t.Errorf("FirstBatchRegistry 应有 10 条，实际：%d", len(FirstBatchRegistry))
	}
	// 验证第一条
	if FirstBatchRegistry[0].ID != "new_session" {
		t.Errorf("第一条 ID 应为 new_session，实际：%q", FirstBatchRegistry[0].ID)
	}
	if FirstBatchRegistry[0].Scope != "gateway" {
		t.Errorf("第一条 Scope 应为 gateway，实际：%q", FirstBatchRegistry[0].Scope)
	}
}

// TestGatewaySlashCommandString 测试枚举字符串转换
func TestGatewaySlashCommandString(t *testing.T) {
	tests := map[GatewaySlashCommand]string{
		SlashNewSession: "/new_session",
		SlashMode:       "/mode",
		SlashSwitch:     "/switch",
		SlashSkills:     "/skills",
		SlashSkillsList: "/skills list",
		SlashBranch:     "/branch",
		SlashRewind:     "/rewind",
	}
	for cmd, expected := range tests {
		if got := GatewaySlashCommandString(cmd); got != expected {
			t.Errorf("GatewaySlashCommandString(%v) = %q, want %q", cmd, got, expected)
		}
	}
}

// TestModeSubcommandString 测试 ModeSubcommand 字符串转换
func TestModeSubcommandString(t *testing.T) {
	if got := ModeSubcommandString(ModeAgent); got != "agent" {
		t.Errorf("ModeSubcommandString(ModeAgent) = %q, want %q", got, "agent")
	}
	if got := ModeSubcommandString(ModeCodePlan); got != "code.plan" {
		t.Errorf("ModeSubcommandString(ModeCodePlan) = %q, want %q", got, "code.plan")
	}
}

// TestSwitchSubcommandString 测试 SwitchSubcommand 字符串转换
func TestSwitchSubcommandString(t *testing.T) {
	if got := SwitchSubcommandString(SwitchPlan); got != "plan" {
		t.Errorf("SwitchSubcommandString(SwitchPlan) = %q, want %q", got, "plan")
	}
	if got := SwitchSubcommandString(SwitchTeam); got != "team" {
		t.Errorf("SwitchSubcommandString(SwitchTeam) = %q, want %q", got, "team")
	}
}

// ──────────────────────────── 非导出函数测试 ────────────────────────────

// TestNormalizeSpaces 测试空白折叠
func TestNormalizeSpaces(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"hello", "hello"},
		{"hello  world", "hello world"},
		{"  hello  world  ", "hello world"},
		{"a\tb\tc", "a b c"},
		{"", ""},
	}
	for _, tc := range tests {
		got := normalizeSpaces(tc.input)
		if got != tc.expected {
			t.Errorf("normalizeSpaces(%q) = %q, want %q", tc.input, got, tc.expected)
		}
	}
}

// containsSubstring 检查字符串是否包含子串
func containsSubstring(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

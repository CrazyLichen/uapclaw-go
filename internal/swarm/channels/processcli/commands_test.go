package processcli

import "testing"

// TestParseSlashCommand_别名归一 /continue→/resume、/fork→/branch、/quit→/exit。
// Python: commands.py:113-126
func TestParseSlashCommand_别名归一(t *testing.T) {
	cases := map[string]string{
		"/continue": "/resume",
		"/fork":     "/branch",
		"/quit":     "/exit",
		"/help":     "/help",
	}
	for in, want := range cases {
		parsed := ParseSlashCommand(in)
		if parsed == nil || parsed.Name != want {
			t.Errorf("ParseSlashCommand(%q) = %v, 期望 %q", in, parsed, want)
		}
	}
}

// TestParseSlashCommand_大小写 /HELP → /help。
func TestParseSlashCommand_大小写(t *testing.T) {
	parsed := ParseSlashCommand("/HELP")
	if parsed == nil || parsed.Name != "/help" {
		t.Errorf("/HELP 得 %v", parsed)
	}
}

// TestParseSlashCommand_参数 参数保留原文。
func TestParseSlashCommand_参数(t *testing.T) {
	parsed := ParseSlashCommand("/mode agent.code")
	if parsed == nil || parsed.Name != "/mode" || parsed.Arguments != "agent.code" {
		t.Errorf("/mode agent.code 得 %v", parsed)
	}
	parsed = ParseSlashCommand("/new --persist")
	if parsed == nil || parsed.Arguments != "--persist" {
		t.Errorf("/new --persist 得 %v", parsed)
	}
}

// TestParseSlashCommand_未知 未知命令与空串返回 nil。
func TestParseSlashCommand_未知(t *testing.T) {
	if ParseSlashCommand("/bogus") != nil {
		t.Error("未知命令应返回 nil")
	}
	if ParseSlashCommand("") != nil {
		t.Error("空串应返回 nil")
	}
	if ParseSlashCommand("hello") != nil {
		t.Error("非斜杠输入应返回 nil")
	}
}

// TestResolveSlashCommand 精确解析。
func TestResolveSlashCommand(t *testing.T) {
	// 前后空白会被 trim，中间空白不是合法命令
	if name, ok := ResolveSlashCommand("  /fork  "); !ok || name != "/branch" {
		t.Errorf("别名解析失败：%q/%v", name, ok)
	}
	if _, ok := ResolveSlashCommand("/nope"); ok {
		t.Error("未知命令应返回 false")
	}
}

// TestResolveModeTarget 非 plan 模式映射逐条。
func TestResolveModeTarget(t *testing.T) {
	cases := map[string]string{
		"agent":             "agent.work.normal",
		"code":              "agent.code.normal",
		"team":              "team.work.normal",
		"team.normal":       "team.work.normal",
		"code.normal":       "agent.code.normal",
		"code.team":         "team.code.normal",
		"agent.work":        "agent.work.normal",
		"agent.code":        "agent.code.normal",
		"team.work":         "team.work.normal",
		"team.code":         "team.code.normal",
		"agent.code.normal": "agent.code.normal",
	}
	for in, want := range cases {
		got, ok := ResolveModeTarget(in)
		if !ok || got != want {
			t.Errorf("ResolveModeTarget(%q) = %q/%v, 期望 %q", in, got, ok, want)
		}
	}
	if _, ok := ResolveModeTarget("bogus"); ok {
		t.Error("未知值应返回 false")
	}
}

// TestMatchingSlashCommands_前缀 "/se" 补出 /sessions 与 /session。
func TestMatchingSlashCommands_前缀(t *testing.T) {
	got := MatchingSlashCommands("/se")
	if len(got) < 2 {
		t.Fatalf("/se 应补出至少 2 项：%v", got)
	}
	names := make(map[string]bool, len(got))
	for _, c := range got {
		names[c.Name] = true
	}
	if !names["/sessions"] || !names["/session"] {
		t.Errorf("/se 未补出 /sessions 与 /session：%v", got)
	}
	if len(MatchingSlashCommands("hello")) != 0 {
		t.Error("非 / 开头不应补全")
	}
	if len(MatchingSlashCommands("/mode ")) != 0 {
		t.Error("含空格不应补命令")
	}
}

// TestMatchingSlashArguments_参数补全 各命令的参数候选。
func TestMatchingSlashArguments_参数补全(t *testing.T) {
	// "ag" 同时命中 agent.work 与 agent.code
	if got := MatchingSlashArguments("/mode ag"); len(got) != 2 {
		t.Errorf("/mode ag 应有 2 项：%v", got)
	}
	// "agent.w" 只命中 agent.work
	if got := MatchingSlashArguments("/mode agent.w"); len(got) != 1 || got[0].Value != "agent.work" {
		t.Errorf("/mode agent.w = %v", got)
	}
	// "o" 同时命中 on 与 off
	if got := MatchingSlashArguments("/plan of"); len(got) != 1 || got[0].Value != "off" {
		t.Errorf("/plan of = %v", got)
	}
	if got := MatchingSlashArguments("/skills l"); len(got) != 1 {
		t.Errorf("/skills l = %v", got)
	}
	if got := MatchingSlashArguments("/new --p"); len(got) != 2 {
		t.Errorf("/new --p 应有 2 项：%v", got)
	}
	// 不支持的命令无参数补全
	if got := MatchingSlashArguments("/help x"); got != nil {
		t.Errorf("/help x 应无补全：%v", got)
	}
	// 无空格无补全
	if got := MatchingSlashArguments("/mode"); got != nil {
		t.Errorf("/mode 应无参数补全：%v", got)
	}
}

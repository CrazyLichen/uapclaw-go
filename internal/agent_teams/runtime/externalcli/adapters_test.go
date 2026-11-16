package externalcli

import (
	"slices"
	"strings"
	"testing"
)

// mustAdapter 测试助手：解析失败即失败。
func mustAdapter(t *testing.T, name string, commandOverride []string) *CliAgentAdapter {
	t.Helper()
	a, err := BuildAdapter(name, commandOverride)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	return a
}

// TestAdapterGemini_全字段断言 对齐 adapters.py gemini 条目（:419-433）。
func TestAdapterGemini_全字段断言(t *testing.T) {
	a := mustAdapter(t, "gemini", nil)
	if a == nil {
		t.Fatal("gemini adapter 应存在")
	}
	if !slices.Equal(a.Command, []string{"gemini", "-o", "stream-json", "-y"}) {
		t.Fatalf("command: %v", a.Command)
	}
	if a.SupportsStdinInjection {
		t.Fatal("gemini 是 one-shot CLI，不支持 stdin 注入")
	}
	if a.PromptFlag != "-p" || a.SessionFlag != "--session-id" || a.ResumeFlag != "--resume" {
		t.Fatalf("flags: %q %q %q", a.PromptFlag, a.SessionFlag, a.ResumeFlag)
	}
	if a.MCPInject != MCPInjectGeminiAdd {
		t.Fatalf("mcp_inject: %q", a.MCPInject)
	}
	if !a.StructuredOutput {
		t.Fatal("gemini 输出为结构化 JSON 流")
	}
	if a.InjectsSystemPromptViaArg() {
		t.Fatal("gemini 无 system-prompt flag，prompt 应拼进首条用户消息")
	}
}

// TestAdapterGemini_buildTurnCommand_首轮session后续resume 对齐 build_turn_command
// 与 gemini 注释（首轮 --session-id，后续 --resume）。
func TestAdapterGemini_buildTurnCommand_首轮session后续resume(t *testing.T) {
	a := mustAdapter(t, "gemini", nil)
	first := a.BuildTurnCommand("hello", "sid-1", true, []string{"--mcp"})
	wantFirst := []string{"gemini", "-o", "stream-json", "-y", "--session-id", "sid-1", "--mcp", "-p", "hello"}
	if !slices.Equal(first, wantFirst) {
		t.Fatalf("first: %v", first)
	}
	next := a.BuildTurnCommand("again", "sid-1", false, nil)
	wantNext := []string{"gemini", "-o", "stream-json", "-y", "--resume", "sid-1", "-p", "again"}
	if !slices.Equal(next, wantNext) {
		t.Fatalf("next: %v", next)
	}
}

// TestAdapterHermes_mcpRegisterCommand带外注册 对齐 adapters.py hermes 条目与
// mcp_register_command 的 hermes 分支（:297-299）。
func TestAdapterHermes_mcpRegisterCommand带外注册(t *testing.T) {
	a := mustAdapter(t, "hermes", nil)
	if a == nil {
		t.Fatal("hermes adapter 应存在")
	}
	cmd := a.MCPRegisterCommand("team", []string{"openjiuwen-team-mcp"})
	want := []string{"hermes", "mcp", "add", "team", "--command", "openjiuwen-team-mcp"}
	if !slices.Equal(cmd, want) {
		t.Fatalf("got %v want %v", cmd, want)
	}
	if a.MCPLaunchArgs("team", []string{"openjiuwen-team-mcp"}) != nil &&
		len(a.MCPLaunchArgs("team", []string{"openjiuwen-team-mcp"})) != 0 {
		t.Fatal("hermes 无 launch 注入，mcp_launch_args 必须为空")
	}
	if !slices.Equal(a.ContinueArgs, []string{"--continue"}) {
		t.Fatalf("continue_args: %v", a.ContinueArgs)
	}
}

// TestAdapterOpenclaw_oneShot字段 对齐 adapters.py openclaw 条目（:434-444）。
func TestAdapterOpenclaw_oneShot字段(t *testing.T) {
	a := mustAdapter(t, "openclaw", nil)
	if a.SupportsStdinInjection {
		t.Fatal("openclaw 是 one-shot CLI")
	}
	if a.PromptFlag != "--message" {
		t.Fatalf("prompt_flag: %q", a.PromptFlag)
	}
	if a.SessionFlag != "--session-id" {
		t.Fatalf("session_flag: %q", a.SessionFlag)
	}
	cmd := a.BuildTurnCommand("hi", "s-1", true, nil)
	want := []string{"openclaw", "--local", "--session-id", "s-1", "--message", "hi"}
	if !slices.Equal(cmd, want) {
		t.Fatalf("got %v want %v", cmd, want)
	}
}

// TestAdapterCodexExec_mcpOverride与developerPrompt 对齐 adapters.py codex-exec
// 条目与 mcp_launch_args/system_prompt_args（:396-410、:258-277、:322-325）。
func TestAdapterCodexExec_mcpOverride与developerPrompt(t *testing.T) {
	a := mustAdapter(t, "codex-exec", nil)
	args := a.MCPLaunchArgs("team-mcp", []string{"openjiuwen-team-mcp", "--x"})
	joined := strings.Join(args, " ")
	for _, want := range []string{
		`mcp_servers.team_mcp.command="openjiuwen-team-mcp"`,
		`mcp_servers.team_mcp.args=["--x"]`,
		`mcp_servers.team_mcp.env_vars=["OPENJIUWEN_TEAM_JOIN","OPENJIUWEN_HOME"]`,
		"startup_timeout_sec=120",
		"required=true",
	} {
		if !strings.Contains(joined, want) {
			t.Fatalf("缺少 %q in %s", want, joined)
		}
	}
	sp := a.SystemPromptArgs("be nice")
	if len(sp) != 2 || sp[0] != "-c" || !strings.HasPrefix(sp[1], "developer_instructions=") {
		t.Fatalf("system_prompt_args: %v", sp)
	}
	if !strings.Contains(sp[1], `"be nice"`) {
		t.Fatalf("prompt 须 JSON 编码: %q", sp[1])
	}
	if !a.IsTurnComplete(`{"type":"turn.completed"}`) {
		t.Fatal("codex-exec 完成判定")
	}
	if a.IsTurnComplete(`{"type":"turn.started"}`) {
		t.Fatal("非完成事件不得误判")
	}
}

// TestAdapterGeneric_marker完成判定 对齐 adapters.py generic 条目。
func TestAdapterGeneric_marker完成判定(t *testing.T) {
	a := mustAdapter(t, "generic", nil)
	if a.IsTurnComplete("plain text") {
		t.Fatal("无标记行不应判定完成")
	}
	if !a.IsTurnComplete("done <<END_OF_TURN>>") {
		t.Fatal("含标记行应判定完成")
	}
}

// TestBuildAdapter_未知报错 对齐 build_adapter 未知名分支（:523-526）。
func TestBuildAdapter_未知报错(t *testing.T) {
	_, err := BuildAdapter("nope", nil)
	if err == nil || !strings.Contains(err.Error(), "unknown cli agent adapter 'nope'") {
		t.Fatalf("got %v", err)
	}
}

// TestBuildAdapter_commandOverride 对齐 command_override 分支（:528-530）。
func TestBuildAdapter_commandOverride(t *testing.T) {
	a := mustAdapter(t, "openclaw", []string{"/abs/openclaw", "--local", "--extra"})
	if !slices.Equal(a.Command, []string{"/abs/openclaw", "--local", "--extra"}) {
		t.Fatalf("command: %v", a.Command)
	}
	if a.PromptFlag != "--message" {
		t.Fatal("override 只替换 command，其余字段保持")
	}
}

// TestAvailableAdapters_五内置 对齐 adapters.py _BUILTIN 五条目。
func TestAvailableAdapters_五内置(t *testing.T) {
	names := AvailableAdapters()
	for _, want := range []string{"codex-exec", "gemini", "openclaw", "hermes", "generic"} {
		if !slices.Contains(names, want) {
			t.Fatalf("缺少内置 adapter %q: %v", want, names)
		}
	}
}

// TestFormatInput_codexProto 对齐 format_input 的 codex_proto 分支（:174-184）。
func TestFormatInput_codexProto(t *testing.T) {
	a := mustAdapter(t, "generic", nil)
	a.InputFormat = InputCodexProto
	out := a.FormatInput("hi")
	if !strings.Contains(out, `"type":"user_input"`) || !strings.Contains(out, `"text":"hi"`) {
		t.Fatalf("codex proto 帧: %s", out)
	}
	if !strings.Contains(out, `"id":`) {
		t.Fatal("须带 uuid id")
	}
}

// TestSummarizeOutputLine_空行 对齐 summarize_output_line 开头的空行短路。
func TestSummarizeOutputLine_空行(t *testing.T) {
	a := mustAdapter(t, "gemini", nil)
	if got := a.SummarizeOutputLine(""); got != "" {
		t.Fatalf("空行返回空: %q", got)
	}
}

// TestSummarizeOutputLine_结构化提取 对齐 _summarize_event 三个提取层。
func TestSummarizeOutputLine_结构化提取(t *testing.T) {
	a := mustAdapter(t, "gemini", nil)
	// 生命周期事件跳过
	if got := a.SummarizeOutputLine(`{"type":"turn.completed"}`); got != "" {
		t.Fatalf("生命周期事件应跳过: %q", got)
	}
	// 顶层 text 字段
	if got := a.SummarizeOutputLine(`{"type":"message","text":"hello world"}`); got != "hello world" {
		t.Fatalf("顶层 text 提取: %q", got)
	}
	// content 块
	if got := a.SummarizeOutputLine(`{"type":"assistant","message":{"content":[{"type":"text","text":"part1"},{"type":"tool_use","name":"read"}]}}`); got != "part1 → read" {
		t.Fatalf("content 块提取: %q", got)
	}
	// 非 JSON 行
	if got := a.SummarizeOutputLine(`not-json`); got != "" {
		t.Fatalf("非 JSON 行应返回空: %q", got)
	}
}

// TestSummarizeOutputLine_纯文本CLI 对齐 structured_output=false 分支。
func TestSummarizeOutputLine_纯文本CLI(t *testing.T) {
	a := mustAdapter(t, "openclaw", nil)
	if got := a.SummarizeOutputLine("narration line"); got != "narration line" {
		t.Fatalf("纯文本 CLI 行即叙述: %q", got)
	}
}

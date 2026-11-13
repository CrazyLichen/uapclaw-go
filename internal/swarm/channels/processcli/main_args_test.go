package processcli

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// ──────────────────────────── parseArgs 测试分组 ────────────────────────────

// TestParseArgs_位置参数 单个位置参数进 Prompt。
// Python: main.py:174-178
func TestParseArgs_位置参数(t *testing.T) {
	args, err := parseArgs([]string{"hello world"})
	if err != nil {
		t.Fatalf("parseArgs: %v", err)
	}
	if args.Prompt != "hello world" {
		t.Errorf("Prompt = %q", args.Prompt)
	}
}

// TestParseArgs_多个位置参数报错 第二个位置参数视为无法识别。
func TestParseArgs_多个位置参数报错(t *testing.T) {
	if _, err := parseArgs([]string{"first", "second"}); err == nil {
		t.Error("多个位置参数应报错")
	}
}

// TestParseArgs_默认值 未指定时 mode=code.normal、work_mode=code、output=human。
// Python: main.py:242-253
func TestParseArgs_默认值(t *testing.T) {
	args, err := parseArgs(nil)
	if err != nil {
		t.Fatalf("parseArgs: %v", err)
	}
	if args.Mode != "code.normal" {
		t.Errorf("Mode = %q, 期望 code.normal", args.Mode)
	}
	if args.WorkMode != "code" {
		t.Errorf("WorkMode = %q, 期望 code", args.WorkMode)
	}
	if args.Output != "human" {
		t.Errorf("Output = %q, 期望 human", args.Output)
	}
	if args.Operation != "chat" {
		t.Errorf("Operation = %q, 期望 chat", args.Operation)
	}
}

// TestParseArgs_会话查询参数 list-sessions/latest-session/search/limit/offset。
// Python: main.py:182-202
func TestParseArgs_会话查询参数(t *testing.T) {
	args, err := parseArgs([]string{
		"--list-sessions", "--search", "kw", "--limit", "5", "--offset", "10",
	})
	if err != nil {
		t.Fatalf("parseArgs: %v", err)
	}
	if !args.ListSessions {
		t.Error("ListSessions 未置位")
	}
	if args.Search != "kw" || !args.SearchSet {
		t.Errorf("Search = %q / set=%v", args.Search, args.SearchSet)
	}
	if args.Limit != 5 || !args.LimitSet {
		t.Errorf("Limit = %d / set=%v", args.Limit, args.LimitSet)
	}
	if args.Offset != 10 || !args.OffsetSet {
		t.Errorf("Offset = %d / set=%v", args.Offset, args.OffsetSet)
	}
}

// TestParseArgs_latestSession latest-session 布尔位。
func TestParseArgs_latestSession(t *testing.T) {
	args, err := parseArgs([]string{"--latest-session"})
	if err != nil {
		t.Fatalf("parseArgs: %v", err)
	}
	if !args.LatestSession {
		t.Error("LatestSession 未置位")
	}
}

// TestParseArgs_模型参数 model/api-base/api-key-env/model-provider。
// Python: main.py:203-218
func TestParseArgs_模型参数(t *testing.T) {
	args, err := parseArgs([]string{
		"task", "--model", "gpt-4", "--api-base", "https://x/v1",
		"--api-key-env", "MY_KEY", "--model-provider", "OpenAI",
	})
	if err != nil {
		t.Fatalf("parseArgs: %v", err)
	}
	if args.Model != "gpt-4" {
		t.Errorf("Model = %q", args.Model)
	}
	if args.APIBase != "https://x/v1" {
		t.Errorf("APIBase = %q", args.APIBase)
	}
	if args.APIKeyEnv != "MY_KEY" {
		t.Errorf("APIKeyEnv = %q", args.APIKeyEnv)
	}
	if args.ModelProvider != "OpenAI" {
		t.Errorf("ModelProvider = %q", args.ModelProvider)
	}
}

// TestParseArgs_短选项m -m 是 --model 的短形式。
func TestParseArgs_短选项m(t *testing.T) {
	args, err := parseArgs([]string{"task", "-m", "gpt-4"})
	if err != nil {
		t.Fatalf("parseArgs: %v", err)
	}
	if args.Model != "gpt-4" {
		t.Errorf("Model = %q", args.Model)
	}
}

// TestParseArgs_机器协议参数 run-json/run-jsonl/query-json。
// Python: main.py:224-233
func TestParseArgs_机器协议参数(t *testing.T) {
	args, err := parseArgs([]string{"--run-json", "/tmp/in.json"})
	if err != nil {
		t.Fatalf("parseArgs: %v", err)
	}
	if args.RunJSON != "/tmp/in.json" {
		t.Errorf("RunJSON = %q", args.RunJSON)
	}

	args, err = parseArgs([]string{"--run-jsonl"})
	if err != nil {
		t.Fatalf("parseArgs: %v", err)
	}
	if !args.RunJSONL {
		t.Error("RunJSONL 未置位")
	}

	args, err = parseArgs([]string{"--query-json", "-"})
	if err != nil {
		t.Fatalf("parseArgs: %v", err)
	}
	if args.QueryJSON != "-" {
		t.Errorf("QueryJSON = %q", args.QueryJSON)
	}
}

// TestParseArgs_工作区参数 cwd/project-dir/trusted-dir/mode。
// Python: main.py:234-246
func TestParseArgs_工作区参数(t *testing.T) {
	args, err := parseArgs([]string{
		"--cwd", "/work", "--project-dir", "/proj",
		"--trusted-dir", "/a", "--trusted-dir", "/b",
		"--mode", "agent.work.normal",
	})
	if err != nil {
		t.Fatalf("parseArgs: %v", err)
	}
	if args.Cwd != "/work" || args.ProjectDir != "/proj" {
		t.Errorf("cwd/project = %q/%q", args.Cwd, args.ProjectDir)
	}
	if len(args.TrustedDirs) != 2 || args.TrustedDirs[0] != "/a" || args.TrustedDirs[1] != "/b" {
		t.Errorf("TrustedDirs = %v", args.TrustedDirs)
	}
	if args.Mode != "agent.work.normal" {
		t.Errorf("Mode = %q", args.Mode)
	}
}

// TestParseArgs_输出参数 output/timeout/显示开关。
// Python: main.py:249-263
func TestParseArgs_输出参数(t *testing.T) {
	args, err := parseArgs([]string{
		"task", "--output", "json", "--timeout", "60",
		"--show-reasoning", "--show-tools", "--debug",
	})
	if err != nil {
		t.Fatalf("parseArgs: %v", err)
	}
	if args.Output != "json" || !args.OutputExplicit {
		t.Errorf("Output = %q / explicit=%v", args.Output, args.OutputExplicit)
	}
	if args.Timeout != 60 || !args.TimeoutSet {
		t.Errorf("Timeout = %v / set=%v", args.Timeout, args.TimeoutSet)
	}
	if !args.ShowReasoning || !args.ShowTools || !args.Debug {
		t.Errorf("显示开关 = %v/%v/%v", args.ShowReasoning, args.ShowTools, args.Debug)
	}
}

// TestParseArgs_隐藏参数 内部协议参数全部可解析。
// Python: main.py:264-301
func TestParseArgs_隐藏参数(t *testing.T) {
	args, err := parseArgs([]string{
		"--_interactive-worker", "--_forwarded-live-input",
		"--_session-result-file", "/tmp/s.txt",
		"--_worker-result-file", "/tmp/w.json",
		"--_model-selection", "gpt-4#0",
		"--_prompt-file", "/tmp/p.txt",
		"--_operation", "session.create",
	})
	if err != nil {
		t.Fatalf("parseArgs: %v", err)
	}
	if !args.InteractiveWorker || !args.ForwardedLiveInput {
		t.Errorf("worker/forwarded = %v/%v", args.InteractiveWorker, args.ForwardedLiveInput)
	}
	if args.SessionResultFile != "/tmp/s.txt" || args.WorkerResultFile != "/tmp/w.json" {
		t.Errorf("结果文件 = %q/%q", args.SessionResultFile, args.WorkerResultFile)
	}
	if args.ModelSelection != "gpt-4#0" {
		t.Errorf("ModelSelection = %q", args.ModelSelection)
	}
	if args.PromptFile != "/tmp/p.txt" {
		t.Errorf("PromptFile = %q", args.PromptFile)
	}
	if args.Operation != "session.create" {
		t.Errorf("Operation = %q", args.Operation)
	}
}

// TestParseArgs_help返回哨兵 --help 返回 errHelpRequested 而非错误。
// Python: main.py:172 action="help"
func TestParseArgs_help返回哨兵(t *testing.T) {
	for _, argv := range [][]string{{"--help"}, {"-h"}} {
		if _, err := parseArgs(argv); !errors.Is(err, errHelpRequested) {
			t.Errorf("%v → err=%v, 期望 errHelpRequested", argv, err)
		}
	}
}

// TestParseArgs_未知参数报错 未知 flag 返回错误。
func TestParseArgs_未知参数报错(t *testing.T) {
	if _, err := parseArgs([]string{"--bogus-flag"}); err == nil {
		t.Error("未知参数应报错")
	}
}

// ──────────────────────────── activateRequestedCwd 测试分组 ────────────────────────────

// TestActivateRequestedCwd_空值不动作 空 cwd 直接返回 nil。
// Python: main.py:310-312
func TestActivateRequestedCwd_空值不动作(t *testing.T) {
	original, _ := os.Getwd()
	args := &AppArgs{Cwd: ""}
	if err := activateRequestedCwd(args); err != nil {
		t.Fatalf("空 cwd 应无错：%v", err)
	}
	current, _ := os.Getwd()
	if current != original {
		t.Errorf("空 cwd 不应切目录：%q → %q", original, current)
	}
}

// TestActivateRequestedCwd_切到临时目录 绝对路径可切。
// Python: main.py:313-320
func TestActivateRequestedCwd_切到临时目录(t *testing.T) {
	original, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd: %v", err)
	}
	defer func() { _ = os.Chdir(original) }()

	target := t.TempDir()
	resolved, err := filepath.EvalSymlinks(target)
	if err != nil {
		t.Fatalf("EvalSymlinks: %v", err)
	}
	args := &AppArgs{Cwd: target}
	if err := activateRequestedCwd(args); err != nil {
		t.Fatalf("activateRequestedCwd: %v", err)
	}
	current, _ := os.Getwd()
	if current != resolved {
		t.Errorf("cwd = %q, 期望 %q", current, resolved)
	}
	if args.Cwd != resolved {
		t.Errorf("args.Cwd 未回写解析值：%q", args.Cwd)
	}
}

// TestActivateRequestedCwd_不存在报错 不存在的目录报「无法访问」。
func TestActivateRequestedCwd_不存在报错(t *testing.T) {
	args := &AppArgs{Cwd: filepath.Join(t.TempDir(), "no-such-dir")}
	err := activateRequestedCwd(args)
	if err == nil {
		t.Fatal("不存在的目录应报错")
	}
	if !containsSubstring(err.Error(), "无法访问") {
		t.Errorf("错误文案 = %q, 期望含「无法访问」", err.Error())
	}
}

// TestActivateRequestedCwd_指向文件报错 目标不是目录时报错。
func TestActivateRequestedCwd_指向文件报错(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "a.txt")
	if err := os.WriteFile(file, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	args := &AppArgs{Cwd: file}
	if err := activateRequestedCwd(args); err == nil {
		t.Error("指向文件应报错")
	}
}

// ──────────────────────────── expandHome 测试分组 ────────────────────────────

// TestExpandHome_无波浪号 原样返回。
func TestExpandHome_无波浪号(t *testing.T) {
	if got := expandHome("/abs/path"); got != "/abs/path" {
		t.Errorf("got %q", got)
	}
	if got := expandHome("rel/path"); got != "rel/path" {
		t.Errorf("got %q", got)
	}
}

// TestExpandHome_单个波浪号 展开为 home。
func TestExpandHome_单个波浪号(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("无 home 目录")
	}
	if got := expandHome("~"); got != home {
		t.Errorf("got %q, 期望 %q", got, home)
	}
}

// TestExpandHome_子路径 展开为 home + 子路径。
func TestExpandHome_子路径(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("无 home 目录")
	}
	want := filepath.Join(home, "sub/dir")
	if got := expandHome("~/sub/dir"); got != want {
		t.Errorf("got %q, 期望 %q", got, want)
	}
}

// TestExpandHome_用户前缀不展开 ~user 形式原样返回（Go 无 pwd 展开）。
func TestExpandHome_用户前缀不展开(t *testing.T) {
	if got := expandHome("~otheruser/x"); got != "~otheruser/x" {
		t.Errorf("got %q, 期望原样", got)
	}
}

// ──────────────────────────── hasLegacyArguments / standalone 测试分组 ────────────────────────────

// TestHasLegacyArguments_默认值不算混用 纯默认参数返回 false。
// Python: main.py:353-356 standalone 判定
func TestHasLegacyArguments_默认值不算混用(t *testing.T) {
	args := &AppArgs{Mode: defaultMode, Output: defaultOutput, WorkMode: "code"}
	if args.hasLegacyArguments() {
		t.Error("纯默认值不应算混用")
	}
}

// TestHasLegacyArguments_各字段触发 任一显式字段都算混用。
func TestHasLegacyArguments_各字段触发(t *testing.T) {
	cases := []*AppArgs{
		{Prompt: "x", Mode: defaultMode, Output: defaultOutput},
		{Session: "s", Mode: defaultMode, Output: defaultOutput},
		{ListSessions: true, Mode: defaultMode, Output: defaultOutput},
		{Model: "m", Mode: defaultMode, Output: defaultOutput},
		{Cwd: "/w", Mode: defaultMode, Output: defaultOutput},
		{Mode: "agent.work.normal", Output: defaultOutput},
		{Output: defaultOutput, OutputExplicit: true, Mode: defaultMode},
		{TimeoutSet: true, Mode: defaultMode, Output: defaultOutput},
		{Debug: true, Mode: defaultMode, Output: defaultOutput},
		{ShowTools: true, Mode: defaultMode, Output: defaultOutput},
		{TrustedDirs: []string{"/a"}, Mode: defaultMode, Output: defaultOutput},
	}
	for i, args := range cases {
		if !args.hasLegacyArguments() {
			t.Errorf("case %d 应判为混用：%+v", i, args)
		}
	}
}

// TestHasLegacyArguments_机器协议独立 只带 --run-json 时不算混用（standalone）。
func TestHasLegacyArguments_机器协议独立(t *testing.T) {
	args := &AppArgs{RunJSON: "/tmp/x.json", Mode: defaultMode, Output: defaultOutput}
	if args.hasLegacyArguments() {
		t.Error("只带 --run-json 应 standalone（不算混用）")
	}
	args.Prompt = "extra"
	if !args.hasLegacyArguments() {
		t.Error("混用 prompt 后应算混用")
	}
}

// containsSubstring 子串断言助手（避免额外引入 strings 依赖到用例顶部）。
func containsSubstring(haystack, needle string) bool {
	return len(haystack) >= len(needle) && (func() bool {
		for i := 0; i+len(needle) <= len(haystack); i++ {
			if haystack[i:i+len(needle)] == needle {
				return true
			}
		}
		return false
	})()
}

package processcli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestWorkerCommand_含operation 有状态操作带 --_operation。
// Python: repl.py:106-154
func TestWorkerCommand_含operation(t *testing.T) {
	args := &AppArgs{Mode: "agent.code.normal", WorkMode: "code"}
	command, err := workerCommand(args, workerCommandOptions{
		PromptFile:        "/tmp/p.txt",
		SessionResultFile: "/tmp/s.txt",
		WorkerResultFile:  "/tmp/w.json",
		Operation:         sessionCreateOperation,
	})
	if err != nil {
		t.Fatalf("workerCommand: %v", err)
	}
	joined := strings.Join(command, " ")
	for _, want := range []string{
		"--_interactive-worker", "--_session-result-file", "--_prompt-file",
		"--_worker-result-file", "--_operation " + sessionCreateOperation,
		"--output human",
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("命令缺 %q：%s", want, joined)
		}
	}
	if strings.Contains(joined, "--_forwarded-live-input") {
		t.Error("有状态操作不应带 --_forwarded-live-input")
	}
}

// TestWorkerCommand_chat带forwarded chat 操作带 forwarded 与 model selection。
func TestWorkerCommand_chat带forwarded(t *testing.T) {
	args := &AppArgs{
		Mode: "agent.code.normal", WorkMode: "code",
		ModelSelection: "gpt-4#0",
	}
	command, err := workerCommand(args, workerCommandOptions{
		PromptFile:        "/tmp/p.txt",
		SessionResultFile: "/tmp/s.txt",
		Operation:         operationChat,
	})
	if err != nil {
		t.Fatalf("workerCommand: %v", err)
	}
	joined := strings.Join(command, " ")
	if !strings.Contains(joined, "--_forwarded-live-input") {
		t.Errorf("chat 应带 forwarded：%s", joined)
	}
	if !strings.Contains(joined, "--_model-selection gpt-4#0") {
		t.Errorf("应带 model selection：%s", joined)
	}
}

// TestWorkerCommand_透传可选参数。
func TestWorkerCommand_透传可选参数(t *testing.T) {
	args := &AppArgs{
		Mode: "agent.code.normal", WorkMode: "code",
		Cwd: "/work", ProjectDir: "/proj", TrustedDirs: []string{"/a", "/b"},
		Timeout: 60, TimeoutSet: true, ShowReasoning: true, ShowTools: true,
	}
	command, _ := workerCommand(args, workerCommandOptions{
		PromptFile: "/tmp/p.txt", SessionResultFile: "/tmp/s.txt", Operation: operationChat,
	})
	joined := strings.Join(command, " ")
	for _, want := range []string{
		"--cwd /work", "--project-dir /proj",
		"--trusted-dir /a", "--trusted-dir /b",
		"--timeout 60", "--show-reasoning", "--show-tools",
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("命令缺 %q：%s", want, joined)
		}
	}
}

// TestWorkerCommand_mode先Deprecate 展示态再 deprecate（P-1 第二调用者）。
// Python: repl.py:115-116
func TestWorkerCommand_mode先Deprecate(t *testing.T) {
	args := &AppArgs{Mode: "agent.code.normal", WorkMode: "code"}
	command, _ := workerCommand(args, workerCommandOptions{
		PromptFile: "/tmp/p.txt", SessionResultFile: "/tmp/s.txt", Operation: operationChat,
	})
	joined := strings.Join(command, " ")
	// ResolveDisplayMode 去 .normal 得 agent.code，DeprecateMode 不变
	if !strings.Contains(joined, "--mode agent.code") {
		t.Errorf("mode 非展示态：%s", joined)
	}
}

// TestDrainRuntimeLogs_回执不进缓冲 回执行被识别而非进环形缓冲。
// Python: repl.py:157-186
func TestDrainRuntimeLogs_回执不进缓冲(t *testing.T) {
	raw := ForwardedReceiptPrefix + `{"status":"accepted","message":"ok"}` + "\nplain-log\n"
	var statuses []string
	tail := drainRuntimeLogsFrom(strings.NewReader(raw), func(status string) {
		statuses = append(statuses, status)
	})
	if len(statuses) != 1 || statuses[0] != "accepted" {
		t.Errorf("回执状态 = %v, 期望 [accepted]", statuses)
	}
	if len(tail) != 1 || tail[0] != "plain-log" {
		t.Errorf("环形缓冲 = %v, 期望 [plain-log]", tail)
	}
}

// TestDrainRuntimeLogs_环形缓冲上限 超过 20 行只留尾部。
func TestDrainRuntimeLogs_环形缓冲上限(t *testing.T) {
	var builder strings.Builder
	for index := 0; index < 30; index++ {
		builder.WriteString("line-")
		builder.WriteString(string(rune('0' + index%10)))
		builder.WriteString("\n")
	}
	tail := drainRuntimeLogsFrom(strings.NewReader(builder.String()), nil)
	if len(tail) != replLogTailLines {
		t.Errorf("缓冲行数 = %d, 期望 %d", len(tail), replLogTailLines)
	}
	if tail[len(tail)-1] != "line-9" {
		t.Errorf("末行 = %q, 期望 line-9", tail[len(tail)-1])
	}
}

// TestDrainRuntimeLogs_非法回执转日志 前缀匹配但 JSON 非法时进缓冲。
func TestDrainRuntimeLogs_非法回执转日志(t *testing.T) {
	raw := ForwardedReceiptPrefix + "not-json\n"
	tail := drainRuntimeLogsFrom(strings.NewReader(raw), nil)
	if len(tail) != 1 {
		t.Errorf("非法回执应进缓冲：%v", tail)
	}
}

// TestWorkerDelivered_operation匹配。
// Python: repl.py:532-534
func TestWorkerDelivered_operation匹配(t *testing.T) {
	args := &AppArgs{lastWorkerResult: map[string]any{"operation": sessionCreateOperation}}
	if !workerDelivered(args, sessionCreateOperation) {
		t.Error("operation 匹配应为 true")
	}
	if workerDelivered(args, sessionSwitchOperation) {
		t.Error("operation 不匹配应为 false")
	}
	if workerDelivered(&AppArgs{}, sessionCreateOperation) {
		t.Error("无回执应为 false")
	}
	if workerDelivered(nil, sessionCreateOperation) {
		t.Error("nil args 应为 false")
	}
}

// TestReadWorkerResult_校验 operation 文件 operation 不符返回 false。
// Python: repl.py:454-473
func TestReadWorkerResult_校验operation(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "worker-result.json")
	if err := os.WriteFile(path,
		[]byte(`{"operation":"session.create","session_id":"s1"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	loaded, ok := readWorkerResult(path, sessionCreateOperation)
	if !ok || stringOr(loaded["session_id"]) != "s1" {
		t.Errorf("读取失败：%v/%v", loaded, ok)
	}
	if _, ok := readWorkerResult(path, sessionSwitchOperation); ok {
		t.Error("operation 不符应返回 false")
	}
	if _, ok := readWorkerResult(filepath.Join(dir, "missing.json"), operationChat); ok {
		t.Error("文件缺失应返回 false")
	}
}

// TestHandleSlashCommand_六个操作路由。
// Python: repl.py:864-960
func TestHandleSlashCommand_六个操作路由(t *testing.T) {
	var out strings.Builder
	ui := NewProcessCliUI(&out, 80)
	state := &replState{SessionID: "s1", DisplayMode: "agent.code", ModelName: "m", Cwd: "/tmp"}
	args := &AppArgs{Mode: "agent.code.normal", WorkMode: "code"}

	// /exit 应返回 true
	if !handleSlashCommand(args, &ParsedSlashCommand{Name: "/exit"}, ui, state, nil) {
		t.Error("/exit 应返回 true")
	}
	// /help 不退出
	out.Reset()
	if handleSlashCommand(args, &ParsedSlashCommand{Name: "/help"}, ui, state, nil) {
		t.Error("/help 不应退出")
	}
	if !strings.Contains(out.String(), "/help") {
		t.Errorf("/help 未渲染命令列表：%q", out.String())
	}
	// /session 渲染当前会话
	out.Reset()
	handleSlashCommand(args, &ParsedSlashCommand{Name: "/session"}, ui, state, nil)
	if !strings.Contains(out.String(), "s1") {
		t.Errorf("/session 未渲染会话 ID：%q", out.String())
	}
	// /cancel 无运行中任务
	out.Reset()
	handleSlashCommand(args, &ParsedSlashCommand{Name: "/cancel"}, ui, state, nil)
	if !strings.Contains(out.String(), "当前没有运行中的任务") {
		t.Errorf("/cancel 提示缺失：%q", out.String())
	}
	// /mode 带参数切模式
	out.Reset()
	handleSlashCommand(args, &ParsedSlashCommand{Name: "/mode", Arguments: "agent.work"}, ui, state, nil)
	if !strings.Contains(out.String(), "已切换模式") {
		t.Errorf("/mode 未切换：%q", out.String())
	}
	// 带参数的命令提示用法
	out.Reset()
	handleSlashCommand(args, &ParsedSlashCommand{Name: "/help", Arguments: "extra"}, ui, state, nil)
	if !strings.Contains(out.String(), "用法：/help") {
		t.Errorf("带参数应提示用法：%q", out.String())
	}
}

// TestHandleControlCommand_plan只改mode /plan 不查 Runtime。
// Python: repl.py:779-798
func TestHandleControlCommand_plan只改mode(t *testing.T) {
	var out strings.Builder
	ui := NewProcessCliUI(&out, 80)
	state := &replState{DisplayMode: "agent.code", ModelName: "m", Cwd: "/tmp"}
	args := &AppArgs{Mode: "agent.code.normal", WorkMode: "code"}

	if !handleControlCommand(args, &ParsedSlashCommand{Name: "/plan", Arguments: "status"}, ui, state) {
		t.Error("/plan status 应被处理")
	}
	if !strings.Contains(out.String(), "当前规划模式：关闭") {
		t.Errorf("status 文案缺失：%q", out.String())
	}

	out.Reset()
	handleControlCommand(args, &ParsedSlashCommand{Name: "/plan", Arguments: "on"}, ui, state)
	if state.DisplayMode != "agent.code.plan" {
		t.Errorf("切 plan 后 DisplayMode = %q, 期望 agent.code.plan", state.DisplayMode)
	}

	// 非 agent 模式拒绝
	out.Reset()
	teamState := &replState{DisplayMode: "team.work"}
	handleControlCommand(args, &ParsedSlashCommand{Name: "/plan", Arguments: "on"}, ui, teamState)
	if !strings.Contains(out.String(), "仅支持单 Agent 模式") {
		t.Errorf("团队模式应拒绝：%q", out.String())
	}

	// 非控制命令不处理
	if handleControlCommand(args, &ParsedSlashCommand{Name: "/help"}, ui, state) {
		t.Error("/help 不属控制命令")
	}
}

// TestHandleModeCommand_非法参数 提示用法。
// Python: repl.py:491-509
func TestHandleModeCommand_非法参数(t *testing.T) {
	var out strings.Builder
	ui := NewProcessCliUI(&out, 80)
	args := &AppArgs{Mode: "agent.code.normal", WorkMode: "code"}

	got := handleModeCommand(args, "", "agent.code", ui)
	if got != "agent.code" {
		t.Errorf("空参数应返回当前模式，得 %q", got)
	}
	if !strings.Contains(out.String(), "当前模式：agent.code") {
		t.Errorf("当前模式提示缺失：%q", out.String())
	}

	out.Reset()
	got = handleModeCommand(args, "bogus", "agent.code", ui)
	if got != "agent.code" {
		t.Errorf("非法参数应保持原模式，得 %q", got)
	}
	if !strings.Contains(out.String(), "用法：/mode") {
		t.Errorf("用法提示缺失：%q", out.String())
	}
}

// TestFirstToken 取首个 token。
func TestFirstToken(t *testing.T) {
	if got := firstToken("/bogus arg1 arg2"); got != "/bogus" {
		t.Errorf("firstToken = %q", got)
	}
	if got := firstToken(""); got != "" {
		t.Errorf("空输入 = %q", got)
	}
}

// TestAllDecimal 全数字判定。
func TestAllDecimal(t *testing.T) {
	if !allDecimal([]string{"1", "20"}) {
		t.Error("全数字应为 true")
	}
	if allDecimal([]string{"1", "x"}) {
		t.Error("含非数字应为 false")
	}
	// 空切片无元素可判，Python all() 语义为真空真 → true
	if !allDecimal(nil) {
		t.Error("空切片按 all() 语义应为 true")
	}
}

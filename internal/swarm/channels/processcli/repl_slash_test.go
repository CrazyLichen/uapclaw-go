package processcli

import (
	"strings"
	"testing"
)

// newTestUI 构造写入 builder 的 UI（测试用）。
func newTestUI() (*ProcessCliUI, *strings.Builder) {
	out := &strings.Builder{}
	return NewProcessCliUI(out, 100), out
}

// ──────────────────────────── handleSimpleSlashCommand 测试分组 ────────────────────────────

// TestHandleSimpleSlashCommand_exit 退出命令返回 true。
// Python: repl.py:943-944
func TestHandleSimpleSlashCommand_exit(t *testing.T) {
	ui, _ := newTestUI()
	state := &replState{}
	if !handleSimpleSlashCommand(&ParsedSlashCommand{Name: "/exit"}, ui, state) {
		t.Error("/exit 应返回 true（退出）")
	}
}

// TestHandleSimpleSlashCommand_cancel 无运行任务时提示。
// Python: repl.py:945-947
func TestHandleSimpleSlashCommand_cancel(t *testing.T) {
	ui, out := newTestUI()
	handleSimpleSlashCommand(&ParsedSlashCommand{Name: "/cancel"}, ui, &replState{})
	if !strings.Contains(out.String(), "当前没有运行中的任务") {
		t.Errorf("cancel 提示 = %q", out.String())
	}
}

// TestHandleSimpleSlashCommand_status 状态行含模型与模式。
// Python: repl.py:948-954
func TestHandleSimpleSlashCommand_status(t *testing.T) {
	ui, out := newTestUI()
	state := &replState{ModelName: "gpt-4", DisplayMode: "agent.code", Cwd: "/w"}
	handleSimpleSlashCommand(&ParsedSlashCommand{Name: "/status"}, ui, state)
	if !strings.Contains(out.String(), "gpt-4") {
		t.Errorf("状态 = %q", out.String())
	}
}

// TestHandleSimpleSlashCommand_help 帮助列出命令。
// Python: repl.py:955-956
func TestHandleSimpleSlashCommand_help(t *testing.T) {
	ui, out := newTestUI()
	handleSimpleSlashCommand(&ParsedSlashCommand{Name: "/help"}, ui, &replState{})
	if !strings.Contains(out.String(), "/help") {
		t.Errorf("帮助 = %q", out.String())
	}
}

// TestHandleSimpleSlashCommand_session 当前会话输出。
// Python: repl.py:957-958
func TestHandleSimpleSlashCommand_session(t *testing.T) {
	ui, out := newTestUI()
	handleSimpleSlashCommand(&ParsedSlashCommand{Name: "/session"}, ui, &replState{SessionID: "s-1"})
	if !strings.Contains(out.String(), "s-1") {
		t.Errorf("会话 = %q", out.String())
	}
}

// TestHandleSimpleSlashCommand_带参数报用法 带参数时只提示用法。
func TestHandleSimpleSlashCommand_带参数报用法(t *testing.T) {
	ui, out := newTestUI()
	handleSimpleSlashCommand(&ParsedSlashCommand{Name: "/help", Arguments: "x"}, ui, &replState{})
	if !strings.Contains(out.String(), "用法") {
		t.Errorf("应提示用法：%q", out.String())
	}
}

// ──────────────────────────── handleModeCommand 测试分组 ────────────────────────────

// TestHandleModeCommand_无参数查当前 空参数只展示当前模式。
// Python: repl.py:498-500
func TestHandleModeCommand_无参数查当前(t *testing.T) {
	ui, out := newTestUI()
	args := &AppArgs{Mode: "agent.code.normal", WorkMode: "code"}
	got := handleModeCommand(args, "", "agent.code", ui)
	if got != "agent.code" {
		t.Errorf("got %q, 期望原值", got)
	}
	if !strings.Contains(out.String(), "当前模式") {
		t.Errorf("提示 = %q", out.String())
	}
}

// TestHandleModeCommand_切换成功 合法目标切换并回写 args。
// Python: repl.py:501-509
func TestHandleModeCommand_切换成功(t *testing.T) {
	ui, _ := newTestUI()
	args := &AppArgs{Mode: "agent.code.normal", WorkMode: "code"}
	got := handleModeCommand(args, "team.work", "agent.code", ui)
	if args.Mode != "team.work.normal" {
		t.Errorf("args.Mode = %q, 期望 team.work.normal", args.Mode)
	}
	if args.WorkMode != "work" {
		t.Errorf("args.WorkMode = %q, 期望 work（team.work 自带 work profile）", args.WorkMode)
	}
	if got != "team.work" {
		t.Errorf("展示模式 = %q, 期望 team.work", got)
	}
}

// TestHandleModeCommand_非法目标 非法参数只提示用法，不改状态。
// Python: repl.py:502-504
func TestHandleModeCommand_非法目标(t *testing.T) {
	ui, out := newTestUI()
	args := &AppArgs{Mode: "agent.code.normal", WorkMode: "code"}
	got := handleModeCommand(args, "bogus", "agent.code", ui)
	if got != "agent.code" {
		t.Errorf("非法目标不应改模式，得 %q", got)
	}
	if args.Mode != "agent.code.normal" {
		t.Errorf("args.Mode 被改：%q", args.Mode)
	}
	if !strings.Contains(out.String(), "用法") {
		t.Errorf("应提示用法：%q", out.String())
	}
}

// ──────────────────────────── handleSlashCommand 分派 ────────────────────────────

// TestHandleSlashCommand_mode分派 /mode 走模式切换分支。
// Python: repl.py:883-890
func TestHandleSlashCommand_mode分派(t *testing.T) {
	ui, _ := newTestUI()
	args := &AppArgs{Mode: "agent.code.normal", WorkMode: "code"}
	state := &replState{DisplayMode: "agent.code"}
	exit := handleSlashCommand(args, &ParsedSlashCommand{Name: "/mode", Arguments: "agent.work"}, ui, state, nil)
	if exit {
		t.Error("/mode 不应退出 REPL")
	}
	if state.DisplayMode != "agent.work" {
		t.Errorf("DisplayMode = %q, 期望 agent.work", state.DisplayMode)
	}
}

// TestHandleSlashCommand_exit分派 /exit 分派到 simple 分支并返回 true。
func TestHandleSlashCommand_exit分派(t *testing.T) {
	ui, _ := newTestUI()
	exit := handleSlashCommand(&AppArgs{}, &ParsedSlashCommand{Name: "/exit"}, ui, &replState{}, nil)
	if !exit {
		t.Error("/exit 应返回 true")
	}
}

// TestHandleSlashCommand_plan分派 /plan on 切到 plan 模式。
// Python: repl.py:779-798
func TestHandleSlashCommand_plan分派(t *testing.T) {
	ui, _ := newTestUI()
	args := &AppArgs{Mode: "agent.code.normal", WorkMode: "code"}
	state := &replState{DisplayMode: "agent.code"}
	handleSlashCommand(args, &ParsedSlashCommand{Name: "/plan", Arguments: "on"}, ui, state, nil)
	if !strings.HasSuffix(state.DisplayMode, ".plan") {
		t.Errorf("DisplayMode = %q, 期望 .plan 结尾", state.DisplayMode)
	}
}

// TestHandleSlashCommand_plan非单Agent 团队模式下拒绝。
// Python: repl.py:783-784
func TestHandleSlashCommand_plan非单Agent(t *testing.T) {
	ui, out := newTestUI()
	state := &replState{DisplayMode: "team.work"}
	handleSlashCommand(&AppArgs{}, &ParsedSlashCommand{Name: "/plan", Arguments: "on"}, ui, state, nil)
	if !strings.Contains(out.String(), "仅支持单 Agent 模式") {
		t.Errorf("提示 = %q", out.String())
	}
}

// TestHandleSlashCommand_plan非法参数 参数不在白名单时报用法。
func TestHandleSlashCommand_plan非法参数(t *testing.T) {
	ui, out := newTestUI()
	state := &replState{DisplayMode: "agent.code"}
	handleSlashCommand(&AppArgs{}, &ParsedSlashCommand{Name: "/plan", Arguments: "bogus"}, ui, state, nil)
	if !strings.Contains(out.String(), "用法") {
		t.Errorf("提示 = %q", out.String())
	}
}

// TestHandleSlashCommand_plan查询状态 status 只报告不改。
func TestHandleSlashCommand_plan查询状态(t *testing.T) {
	ui, out := newTestUI()
	state := &replState{DisplayMode: "agent.code.plan"}
	handleSlashCommand(&AppArgs{}, &ParsedSlashCommand{Name: "/plan", Arguments: "status"}, ui, state, nil)
	if !strings.Contains(out.String(), "开启") {
		t.Errorf("应报告已开启：%q", out.String())
	}
	if state.DisplayMode != "agent.code.plan" {
		t.Errorf("status 不应改模式：%q", state.DisplayMode)
	}
}

// TestHandleSlashCommand_skills非法参数 /skills 只接受空或 list。
// Python: repl.py:519-521
func TestHandleSlashCommand_skills非法参数(t *testing.T) {
	ui, out := newTestUI()
	handleSlashCommand(&AppArgs{}, &ParsedSlashCommand{Name: "/skills", Arguments: "bogus"}, ui, &replState{}, nil)
	if !strings.Contains(out.String(), "用法") {
		t.Errorf("提示 = %q", out.String())
	}
}

// TestHandleSlashCommand_branch无会话 /branch 无当前会话时提示。
// Python: repl.py:716-718
func TestHandleSlashCommand_branch无会话(t *testing.T) {
	ui, out := newTestUI()
	handleSlashCommand(&AppArgs{}, &ParsedSlashCommand{Name: "/branch"}, ui, &replState{}, nil)
	if !strings.Contains(out.String(), "没有可创建分支的会话") {
		t.Errorf("提示 = %q", out.String())
	}
}

// TestHandleSlashCommand_delete用法 /delete 参数个数必须为 1。
// Python: repl.py:746-749
func TestHandleSlashCommand_delete用法(t *testing.T) {
	ui, out := newTestUI()
	handleSlashCommand(&AppArgs{}, &ParsedSlashCommand{Name: "/delete", Arguments: "a b"}, ui, &replState{}, nil)
	if !strings.Contains(out.String(), "用法") {
		t.Errorf("提示 = %q", out.String())
	}
}

// TestHandleSlashCommand_resume用法 /resume 最多一个参数。
// Python: repl.py:653-656
func TestHandleSlashCommand_resume用法(t *testing.T) {
	ui, out := newTestUI()
	handleSlashCommand(&AppArgs{}, &ParsedSlashCommand{Name: "/resume", Arguments: "a b"}, ui, &replState{}, nil)
	if !strings.Contains(out.String(), "用法") {
		t.Errorf("提示 = %q", out.String())
	}
}

// TestHandleSlashCommand_sessions用法 /sessions 分页参数校验。
// Python: repl.py:677-686
func TestHandleSlashCommand_sessions用法(t *testing.T) {
	cases := []string{"0", "201", "5 3 x"}
	for _, arguments := range cases {
		ui, out := newTestUI()
		handleSlashCommand(&AppArgs{}, &ParsedSlashCommand{Name: "/sessions", Arguments: arguments}, ui, &replState{}, nil)
		if !strings.Contains(out.String(), "用法") && !strings.Contains(out.String(), "limit") {
			t.Errorf("arguments=%q 提示 = %q", arguments, out.String())
		}
	}
}

// TestHandleSlashCommand_sessions搜索超长 搜索词超 200 字符报错。
func TestHandleSlashCommand_sessions搜索超长(t *testing.T) {
	ui, out := newTestUI()
	long := strings.Repeat("x", 201)
	handleSlashCommand(&AppArgs{}, &ParsedSlashCommand{Name: "/sessions", Arguments: long}, ui, &replState{}, nil)
	if !strings.Contains(out.String(), "200") {
		t.Errorf("提示 = %q", out.String())
	}
}

// ──────────────────────────── replState 行为 ────────────────────────────

// TestReplState_初始零值 零值状态各字段为空。
func TestReplState_初始零值(t *testing.T) {
	state := &replState{}
	if state.SessionID != "" || state.DisplayMode != "" || state.ModelSelection != "" {
		t.Errorf("零值状态 = %+v", state)
	}
}

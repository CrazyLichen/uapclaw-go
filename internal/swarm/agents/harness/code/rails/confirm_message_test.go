package rails

import (
	"strings"
	"testing"
)

// ──────────────────────────── formatArgsPreview ────────────────────────────

// TestFormatArgsPreview_空参数返回空串 测试空 map。
func TestFormatArgsPreview_空参数返回空串(t *testing.T) {
	if got := formatArgsPreview(nil); got != "" {
		t.Errorf("got %q, want 空串", got)
	}
	if got := formatArgsPreview(map[string]any{}); got != "" {
		t.Errorf("空 map got %q, want 空串", got)
	}
}

// TestFormatArgsPreview_JSON缩进 测试 indent=2 输出。
func TestFormatArgsPreview_JSON缩进(t *testing.T) {
	got := formatArgsPreview(map[string]any{"path": "/tmp/a"})
	if !strings.Contains(got, "\n  \"path\": \"/tmp/a\"") {
		t.Errorf("got %q，应为 indent=2 的 JSON", got)
	}
}

// TestFormatArgsPreview_中文不转义 测试 ensure_ascii=False。
func TestFormatArgsPreview_中文不转义(t *testing.T) {
	got := formatArgsPreview(map[string]any{"path": "/tmp/中文.txt"})
	if !strings.Contains(got, "中文") {
		t.Errorf("got %q，中文应保留不转义", got)
	}
}

// TestFormatArgsPreview_截断到800 测试超长截断。
func TestFormatArgsPreview_截断到800(t *testing.T) {
	large := strings.Repeat("x", 2000)
	got := formatArgsPreview(map[string]any{"data": large})
	if len(got) > 800 {
		t.Errorf("len = %d, want <= 800", len(got))
	}
}

// ──────────────────────────── switch_mode 分支 ────────────────────────────

// TestBuildConfirmInterruptMessage_switch到plan 测试进入 plan 模式文案。
func TestBuildConfirmInterruptMessage_switch到plan(t *testing.T) {
	got := BuildConfirmInterruptMessage("switch_mode", map[string]any{"mode": "plan"})
	if !strings.Contains(got, "enter plan mode (read-only planning phase)") {
		t.Errorf("got %q，应说明进入 plan 模式", got)
	}
	if !strings.Contains(got, "Target mode: `plan`") {
		t.Errorf("got %q，应含目标模式", got)
	}
}

// TestBuildConfirmInterruptMessage_switch到normal 测试退出 plan 模式文案。
func TestBuildConfirmInterruptMessage_switch到normal(t *testing.T) {
	got := BuildConfirmInterruptMessage("switch_mode", map[string]any{"mode": "normal"})
	if !strings.Contains(got, "exit plan mode and return to execution") {
		t.Errorf("got %q", got)
	}
}

// TestBuildConfirmInterruptMessage_switch到auto 测试 auto 与 normal 同文案。
func TestBuildConfirmInterruptMessage_switch到auto(t *testing.T) {
	got := BuildConfirmInterruptMessage("switch_mode", map[string]any{"mode": "auto"})
	if !strings.Contains(got, "exit plan mode and return to execution") {
		t.Errorf("got %q，auto 应与 normal 同文案", got)
	}
}

// TestBuildConfirmInterruptMessage_switch未知模式 测试未知目标模式。
func TestBuildConfirmInterruptMessage_switch未知模式(t *testing.T) {
	got := BuildConfirmInterruptMessage("switch_mode", map[string]any{"mode": "weird"})
	if !strings.Contains(got, "switch agent mode to `weird`") {
		t.Errorf("got %q", got)
	}
}

// TestBuildConfirmInterruptMessage_switch无模式 测试缺 mode 时 unknown。
func TestBuildConfirmInterruptMessage_switch无模式(t *testing.T) {
	got := BuildConfirmInterruptMessage("switch_mode", nil)
	if !strings.Contains(got, "`unknown`") {
		t.Errorf("got %q，缺模式应显示 unknown", got)
	}
}

// TestBuildConfirmInterruptMessage_switch无目标模式行 测试 target_mode 为空时不加该行。
func TestBuildConfirmInterruptMessage_switch无目标模式行(t *testing.T) {
	got := BuildConfirmInterruptMessage("switch_mode", map[string]any{"mode": ""})
	if strings.Contains(got, "Target mode:") {
		t.Errorf("got %q，无目标模式时不应输出 Target mode 行", got)
	}
}

// TestBuildConfirmInterruptMessage_switch用target_mode键 测试 target_mode 别名。
func TestBuildConfirmInterruptMessage_switch用target_mode键(t *testing.T) {
	got := BuildConfirmInterruptMessage("switch_mode", map[string]any{"target_mode": "plan"})
	if !strings.Contains(got, "enter plan mode") {
		t.Errorf("got %q，应支持 target_mode 键", got)
	}
}

// ──────────────────────────── 通用分支 ────────────────────────────

// TestBuildConfirmInterruptMessage_通用工具 测试普通工具文案。
func TestBuildConfirmInterruptMessage_通用工具(t *testing.T) {
	got := BuildConfirmInterruptMessage("bash", map[string]any{"command": "ls"})
	if !strings.Contains(got, "**Tool `bash` requires your approval**") {
		t.Errorf("got %q，应含工具审批标题", got)
	}
	if !strings.Contains(got, "Arguments:") {
		t.Errorf("got %q，应含参数区", got)
	}
}

// TestBuildConfirmInterruptMessage_无参数不显示参数区 测试空参数省略。
func TestBuildConfirmInterruptMessage_无参数不显示参数区(t *testing.T) {
	got := BuildConfirmInterruptMessage("bash", nil)
	if strings.Contains(got, "Arguments:") {
		t.Errorf("got %q，无参数时不应显示参数区", got)
	}
}

// TestFormatArgsPreview_空dict返回空串 测试空 dict 走 `if not tool_args` 短路。
//
// Python: `if not tool_args: return ""` —— 空 dict 在序列化前就返回了，
// 所以 build_confirm_interrupt_message 里 `preview != "{}"` 实际永不触发。
func TestFormatArgsPreview_空dict返回空串(t *testing.T) {
	if got := formatArgsPreview(map[string]any{}); got != "" {
		t.Errorf("got %q, want 空串（空 dict 短路）", got)
	}
}

// ──────────────────────────── 与 Python 逐字对照 ────────────────────────────

// TestBuildConfirmInterruptMessage_逐字对齐Python 测试输出与 Python 完全一致。
//
// 期望值由 Python 源码真实跑出（见 code_confirm_interrupt_rail.py L37-76），
// 不是推断。
func TestBuildConfirmInterruptMessage_逐字对齐Python(t *testing.T) {
	switchPlanWant := "**Agent wants to enter plan mode (read-only planning phase)**\n\n" +
		"Tool: `switch_mode`\n" +
		"Target mode: `plan`\n\n" +
		"Approve to let the agent continue with this mode change."
	if got := BuildConfirmInterruptMessage("switch_mode", map[string]any{"mode": "plan"}); got != switchPlanWant {
		t.Errorf("switch_mode/plan 不一致\n got: %q\nwant: %q", got, switchPlanWant)
	}

	bashWant := "**Tool `bash` requires your approval**\n\n" +
		"The agent is waiting for you to allow or reject this action.\n\n" +
		"Arguments:\n```json\n{\n  \"command\": \"ls\",\n  \"path\": \"/tmp/中文\"\n}\n```"
	if got := BuildConfirmInterruptMessage("bash", map[string]any{
		"command": "ls",
		"path":    "/tmp/中文",
	}); got != bashWant {
		t.Errorf("bash 不一致\n got: %q\nwant: %q", got, bashWant)
	}
}

package processcli

import (
	"strings"
	"testing"
)

// TestReadPromptFrom_非TTY降级 管道输入时逐行读，不建会话。
func TestReadPromptFrom_非TTY降级(t *testing.T) {
	got, err := readPromptFrom(nil, promptText, strings.NewReader("hello\n"))
	if err != nil {
		t.Fatalf("读取失败: %v", err)
	}
	if got != "hello" {
		t.Errorf("got %q, 期望 hello", got)
	}
}

// TestReadPromptFrom_EOF 空输入返回 io.EOF。
func TestReadPromptFrom_EOF(t *testing.T) {
	if _, err := readPromptFrom(nil, promptText, strings.NewReader("")); err == nil {
		t.Error("空输入应返回 EOF")
	}
}

// TestReadPromptFrom_去空白 首尾空白被 trim。
func TestReadPromptFrom_去空白(t *testing.T) {
	got, err := readPromptFrom(nil, promptText, strings.NewReader("  spaced  \n"))
	if err != nil {
		t.Fatalf("读取失败: %v", err)
	}
	if got != "spaced" {
		t.Errorf("got %q, 期望 spaced", got)
	}
}

// TestReadPromptFrom_无换行结尾 最后一行无换行也能读出。
func TestReadPromptFrom_无换行结尾(t *testing.T) {
	got, err := readPromptFrom(nil, promptText, strings.NewReader("last"))
	if err != nil {
		t.Fatalf("读取失败: %v", err)
	}
	if got != "last" {
		t.Errorf("got %q, 期望 last", got)
	}
}

// TestSlashCompleterText_首token "/se" 补出 /sessions 与 /session。
func TestSlashCompleterText_首token(t *testing.T) {
	suggests, start, end := slashCompleterText("/se")
	if len(suggests) < 2 {
		t.Fatalf("/se 应有至少 2 项补全：%v", suggests)
	}
	found := false
	for _, s := range suggests {
		if s.Text == "/sessions" {
			found = true
		}
	}
	if !found {
		t.Errorf("/se 未补出 /sessions：%v", suggests)
	}
	if start != 0 || end != 3 {
		t.Errorf("替换范围 = %d..%d, 期望 0..3", start, end)
	}
}

// TestSlashCompleterText_参数补全 "/mode agent.w" 只补 agent.work。
func TestSlashCompleterText_参数补全(t *testing.T) {
	suggests, start, end := slashCompleterText("/mode agent.w")
	if len(suggests) != 1 || suggests[0].Text != "agent.work" {
		t.Fatalf("/mode agent.w = %v", suggests)
	}
	// 替换范围从最后一个空格之后开始
	if start != 6 {
		t.Errorf("start = %d, 期望 6", start)
	}
	// "/mode agent.w" 共 13 个 rune（/mode=5 + 空格=1 + agent.w=7）
	if end != 13 {
		t.Errorf("end = %d, 期望 13", end)
	}
}

// TestSlashCompleterText_非命令 非 / 开头无补全。
func TestSlashCompleterText_非命令(t *testing.T) {
	if suggests, _, _ := slashCompleterText("hello"); len(suggests) != 0 {
		t.Errorf("非命令输入不应补全：%v", suggests)
	}
}

// TestPadDescription_列宽 描述左侧补齐到 22 列。
// Python: prompt.py:36-43 display=command.name.ljust(22)
func TestPadDescription_列宽(t *testing.T) {
	got := padDescription("描述")
	if !strings.HasPrefix(got, strings.Repeat(" ", commandColumnWidth)) {
		t.Errorf("未补齐到 %d 列：%q", commandColumnWidth, got)
	}
	if !strings.HasSuffix(got, "描述") {
		t.Errorf("描述丢失：%q", got)
	}
}

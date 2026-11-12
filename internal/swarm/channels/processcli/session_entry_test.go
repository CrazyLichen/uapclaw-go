package processcli

import (
	"strings"
	"testing"
)

// TestValidateSessionQuery_六类拒绝。
func TestValidateSessionQuery_六类拒绝(t *testing.T) {
	// 无查询触发条件但给了 --search
	if err := ValidateSessionQuery(&AppArgs{SearchSet: true, Limit: 20}); err == nil {
		t.Error("无 --list-sessions 却带 --search 应拒绝")
	}
	// 查询模式混用任务参数
	if err := ValidateSessionQuery(&AppArgs{ListSessions: true, Prompt: "task"}); err == nil {
		t.Error("会话查询 + prompt 应拒绝")
	}
	// --latest-session 带分页
	if err := ValidateSessionQuery(&AppArgs{LatestSession: true, LimitSet: true, Limit: 20}); err == nil {
		t.Error("--latest-session 带 --limit 应拒绝")
	}
	// --limit 越界
	if err := ValidateSessionQuery(&AppArgs{ListSessions: true, LimitSet: true, Limit: 0}); err == nil {
		t.Error("--limit=0 应拒绝")
	}
	if err := ValidateSessionQuery(&AppArgs{ListSessions: true, LimitSet: true, Limit: 201}); err == nil {
		t.Error("--limit=201 应拒绝")
	}
	// --offset 负数
	if err := ValidateSessionQuery(&AppArgs{ListSessions: true, OffsetSet: true, Offset: -1}); err == nil {
		t.Error("--offset=-1 应拒绝")
	}
	// --search 超长
	if err := ValidateSessionQuery(&AppArgs{
		ListSessions: true, SearchSet: true, Search: strings.Repeat("x", 201),
	}); err == nil {
		t.Error("--search 超 200 字符应拒绝")
	}
	// 合法
	if err := ValidateSessionQuery(&AppArgs{ListSessions: true, LimitSet: true, Limit: 20}); err != nil {
		t.Errorf("合法组合报错：%v", err)
	}
}

// TestDisplayText_控制字符 控制字符替换为空格。
// Python: session_entry.py:52-55 _display_text
func TestDisplayText_控制字符(t *testing.T) {
	if got := displayText("a\x00b"); got != "a b" {
		t.Errorf("控制字符未替换：%q", got)
	}
	if got := displayText("正常"); got != "正常" {
		t.Errorf("普通文本被改：%q", got)
	}
}

// TestDisplayTime_非法值 非正数返回 "-"。
// Python: session_entry.py:58-66 _display_time
func TestDisplayTime_非法值(t *testing.T) {
	for _, value := range []float64{0, -1, -100} {
		if got := displayTime(value); got != "-" {
			t.Errorf("displayTime(%v) = %q, 期望 -", value, got)
		}
	}
	if got := displayTime(1700000000); got == "-" {
		t.Error("合法时间戳不应返回 -")
	}
}

// TestExecuteSessionQuery_latest退出码语义
// 上游 session_entry.py:117-120：--latest-session 有匹配时输出完整 ID 并退出 0，
// 无匹配时退出 1。本机可能已有真实会话，故按结果动态断言两条互斥分支。
func TestExecuteSessionQuery_latest退出码语义(t *testing.T) {
	var stdout, stderr strings.Builder
	code := executeSessionQueryWith(&AppArgs{LatestSession: true}, &stdout, &stderr)

	switch code {
	case 0:
		// 有匹配：stdout 恰好一行完整会话 ID，stderr 为空
		if stderr.Len() != 0 {
			t.Errorf("成功时 stderr 应为空，得 %q", stderr.String())
		}
		lines := strings.Split(strings.TrimRight(stdout.String(), "\n"), "\n")
		if len(lines) != 1 {
			t.Fatalf("应只输出一行 ID，得 %d 行：%q", len(lines), stdout.String())
		}
		if strings.TrimSpace(lines[0]) == "" {
			t.Error("ID 不应为空")
		}
	case 1:
		// 无匹配：stderr 提示，stdout 为空
		if !strings.Contains(stderr.String(), "未找到匹配") {
			t.Errorf("stderr = %q, 期望含「未找到匹配」", stderr.String())
		}
		if stdout.Len() != 0 {
			t.Errorf("无匹配时 stdout 应为空，得 %q", stdout.String())
		}
	default:
		t.Errorf("退出码 = %d, 期望 0 或 1", code)
	}
}

// TestExecuteSessionQuery_list退出码恒为0
// 上游 session_entry.py:122-131：--list-sessions 无论有无结果都退出 0。
func TestExecuteSessionQuery_list退出码恒为0(t *testing.T) {
	var stdout, stderr strings.Builder
	code := executeSessionQueryWith(&AppArgs{ListSessions: true}, &stdout, &stderr)
	if code != 0 {
		t.Errorf("退出码 = %d, 期望 0", code)
	}
	if stderr.Len() != 0 {
		t.Errorf("stderr 应为空，得 %q", stderr.String())
	}
	text := stdout.String()
	if !strings.Contains(text, "会话ID") && !strings.Contains(text, "没有匹配") {
		t.Errorf("stdout 应为表格表头或空结果提示，得 %q", text)
	}
}

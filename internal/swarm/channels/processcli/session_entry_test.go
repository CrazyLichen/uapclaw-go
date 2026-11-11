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

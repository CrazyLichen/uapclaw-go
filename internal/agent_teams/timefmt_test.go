package agent_teams

import (
	"testing"
	"time"
)

// fixedCST 测试固定时区（与上游 gen_golden.py 的 TZ=Asia/Shanghai 对齐）
var fixedCST = time.FixedZone("CST", 8*3600)

// ts20260528 2026-05-28 14:30:05 +08:00 的毫秒 epoch
const ts20260528 int64 = 1779949805000

// TestFormatAbsoluteTime_固定时区 验证绝对时间格式为 "2006-01-02 15:04:05 +08:00"。
// golden 由上游 format_absolute_time(ts20260528) 生成。
func TestFormatAbsoluteTime_固定时区(t *testing.T) {
	got := formatAbsolute(fixedCST, ts20260528)
	want := "2026-05-28 14:30:05 +08:00"
	if got != want {
		t.Errorf("得到 %q\n期望 %q", got, want)
	}
}

// TestFormatAbsoluteTime_UTC 验证零时区偏移渲染为 +00:00。
func TestFormatAbsoluteTime_UTC(t *testing.T) {
	got := formatAbsolute(time.UTC, ts20260528)
	want := "2026-05-28 06:30:05 +00:00"
	if got != want {
		t.Errorf("得到 %q\n期望 %q", got, want)
	}
}

// TestFormatTimeContext_刚刚 验证 10 秒内（含未来时间）都归「刚刚」。
func TestFormatTimeContext_刚刚(t *testing.T) {
	cases := []struct {
		name  string
		ts    int64
		nowMs int64
	}{
		{"同时刻", ts20260528, ts20260528},
		{"+3秒", ts20260528, ts20260528 + 3000},
		{"+9秒", ts20260528, ts20260528 + 9000},
		{"未来时间(负delta)", ts20260528, ts20260528 - 60000},
	}
	for _, c := range cases {
		ts := c.ts
		got := formatTimeContextAt(fixedCST, &ts, c.nowMs)
		want := "2026-05-28 14:30:05 +08:00 (刚刚)"
		if got != want {
			t.Errorf("%s：得到 %q\n期望 %q", c.name, got, want)
		}
	}
}

// TestFormatTimeContext_四个分桶 验证秒/分/时/天四档文案。
func TestFormatTimeContext_四个分桶(t *testing.T) {
	cases := []struct {
		name  string
		delta int64
		want  string
	}{
		{"30秒", 30000, "2026-05-28 14:30:05 +08:00 (30 秒前)"},
		{"3分钟", 180000, "2026-05-28 14:30:05 +08:00 (3 分钟前)"},
		{"2小时", 7200000, "2026-05-28 14:30:05 +08:00 (2 小时前)"},
		{"2天", 172800000, "2026-05-28 14:30:05 +08:00 (2 天前)"},
	}
	for _, c := range cases {
		ts := ts20260528
		got := formatTimeContextAt(fixedCST, &ts, ts20260528+c.delta)
		if got != c.want {
			t.Errorf("%s：得到 %q\n期望 %q", c.name, got, c.want)
		}
	}
}

// TestFormatTimeContext_nil时间 验证 nil 返回 time.unknown 文案。
func TestFormatTimeContext_nil时间(t *testing.T) {
	got := formatTimeContextAt(fixedCST, nil, ts20260528)
	if got != "时间未知" {
		t.Errorf("得到 %q，期望 %q", got, "时间未知")
	}
}

// TestFormatAbsoluteTime_对外无参变体 验证 FormatAbsoluteTime 走本地时区且不 panic。
func TestFormatAbsoluteTime_对外无参变体(t *testing.T) {
	got := FormatAbsoluteTime(ts20260528)
	if got == "" {
		t.Fatal("FormatAbsoluteTime 不应返回空串")
	}
	// 只断言格式骨架，不断言具体时区（依赖运行环境 time.Local）
	if len(got) != len("2026-05-28 14:30:05 +08:00") {
		t.Errorf("格式长度不符：%q", got)
	}
}

// TestFormatTimeContext_对外变体 验证对外变体与内部变体在固定时区下一致。
func TestFormatTimeContext_对外变体(t *testing.T) {
	ts := ts20260528
	got := FormatTimeContext(&ts, ts20260528+180000)
	if !stringsHasSuffix(got, "(3 分钟前)") {
		t.Errorf("得到 %q，应以 (3 分钟前) 结尾", got)
	}
}

// stringsHasSuffix 避免为一个断言引入 strings 导入（保持测试文件依赖最小）
func stringsHasSuffix(s, suffix string) bool {
	return len(s) >= len(suffix) && s[len(s)-len(suffix):] == suffix
}

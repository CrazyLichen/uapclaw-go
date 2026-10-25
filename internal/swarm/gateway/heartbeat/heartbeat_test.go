package heartbeat

import (
	"testing"
)

// TestHeartbeatConstants 常量逐字对齐 Python（提示词含半角逗号空格，逐字校验）。
func TestHeartbeatConstants(t *testing.T) {
	if HeartbeatChannelID != "__heartbeat__" {
		t.Fatalf("HeartbeatChannelID = %q", HeartbeatChannelID)
	}
	if HeartbeatOK != "HEARTBEAT_OK" {
		t.Fatalf("HeartbeatOK = %q", HeartbeatOK)
	}
	want := "如果你的workspace目录存在HEARTBEAT.md文件, 读取文件内容并且根据文件内容执行任务. 如果没有HEARTBEAT.md文件, 仅回复HEARTBEAT_OK"
	if HeartbeatPrompt != want {
		t.Fatalf("HeartbeatPrompt 不逐字对齐:\n got=%q\nwant=%q", HeartbeatPrompt, want)
	}
}

// TestNormalizeActiveHours_数字回转 1320 → "22:00"（对齐 Python L38-41 divmod）。
func TestNormalizeActiveHours_数字回转(t *testing.T) {
	in := map[string]any{"start": int64(1320), "end": float64(360)}
	got := NormalizeActiveHours(in)
	if got["start"] != "22:00" || got["end"] != "06:00" {
		t.Fatalf("got %v", got)
	}
}

// TestNormalizeActiveHours_字符串直通 字符串值原样保留。
func TestNormalizeActiveHours_字符串直通(t *testing.T) {
	in := map[string]any{"start": "08:00", "end": "22:00"}
	got := NormalizeActiveHours(in)
	if got["start"] != "08:00" || got["end"] != "22:00" {
		t.Fatalf("got %v", got)
	}
}

// TestNormalizeActiveHours_非startend键数字 非白名单键的数字走 str 化分支。
func TestNormalizeActiveHours_非startend键数字(t *testing.T) {
	in := map[string]any{"note": 1320}
	got := NormalizeActiveHours(in)
	if got["note"] != "1320" {
		t.Fatalf("got %v", got)
	}
}

// TestNormalizeActiveHours_nil与空 nil 原样返回、空 map 原样返回空 map
// （对齐 Python `if not active_hours: return active_hours` 原样语义——空 dict 进空 dict 出）。
func TestNormalizeActiveHours_nil与空(t *testing.T) {
	if got := NormalizeActiveHours(nil); got != nil {
		t.Fatalf("nil input should return nil, got %v", got)
	}
	if got := NormalizeActiveHours(map[string]any{}); got == nil || len(got) != 0 {
		t.Fatalf("empty map should return empty non-nil map, got %v", got)
	}
}

// TestNormalizeActiveHours_其他类型 bool → "true"（对齐 Python str(v)）。
func TestNormalizeActiveHours_其他类型(t *testing.T) {
	in := map[string]any{"start": true}
	got := NormalizeActiveHours(in)
	if got["start"] != "true" {
		t.Fatalf("got %v", got)
	}
}

// TestNormalizeActiveHours_int32类型不匹配 Python int 是任意精度；Go YAML 出 int/int64/float64，
// int32 等其他整型走 str 化分支（1320 不会数字回转，与 Python isinstance(int) 有意差异，归档偏差②）。
func TestNormalizeActiveHours_int32类型不匹配(t *testing.T) {
	in := map[string]any{"start": int32(1320)}
	got := NormalizeActiveHours(in)
	if got["start"] != "1320" {
		t.Fatalf("got %v", got)
	}
}

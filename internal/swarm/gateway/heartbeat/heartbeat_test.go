package heartbeat

import (
	"testing"
	"time"
)

// mkTime 构造固定 Unix 秒的假时钟。
func mkTime(sec int64) func() float64 {
	return func() float64 { return float64(sec) }
}

// TestIsActiveNow_无配置 ActiveHours 为空 → true。
func TestIsActiveNow_无配置(t *testing.T) {
	s := NewGatewayHeartbeatService(nil, &HeartbeatConfig{}, nil, mkTime(0))
	if !s.isActiveNow() {
		t.Fatal("no active_hours should be active")
	}
}

// TestIsActiveNow_普通区间内 15:00 在 08:00-22:00 内。
func TestIsActiveNow_普通区间内(t *testing.T) {
	// 2026-10-27 15:00 本地时区的 Unix 秒（测试机时区无关构造见下）
	inRange := time.Date(2026, 10, 27, 15, 0, 0, 0, time.Local).Unix()
	s := NewGatewayHeartbeatService(nil, &HeartbeatConfig{
		ActiveHours: map[string]any{"start": "08:00", "end": "22:00"},
	}, nil, mkTime(inRange))
	if !s.isActiveNow() {
		t.Fatal("15:00 should be in 08:00-22:00")
	}
}

// TestIsActiveNow_普通区间外 23:00 在 08:00-22:00 外（含边界 22:00 不含）。
func TestIsActiveNow_普通区间外(t *testing.T) {
	outRange := time.Date(2026, 10, 27, 23, 0, 0, 0, time.Local).Unix()
	s := NewGatewayHeartbeatService(nil, &HeartbeatConfig{
		ActiveHours: map[string]any{"start": "08:00", "end": "22:00"},
	}, nil, mkTime(outRange))
	if s.isActiveNow() {
		t.Fatal("23:00 should be out of 08:00-22:00")
	}
	// 边界：end 时刻本身不生效（now < end 语义）
	atEnd := time.Date(2026, 10, 27, 22, 0, 0, 0, time.Local).Unix()
	s2 := NewGatewayHeartbeatService(nil, &HeartbeatConfig{
		ActiveHours: map[string]any{"start": "08:00", "end": "22:00"},
	}, nil, mkTime(atEnd))
	if s2.isActiveNow() {
		t.Fatal("22:00 boundary should be out (start<=now<end)")
	}
}

// TestIsActiveNow_跨午夜区间 22:00-06:00：23:30 与 05:00 生效，15:00 不生效。
func TestIsActiveNow_跨午夜区间(t *testing.T) {
	cfg := &HeartbeatConfig{ActiveHours: map[string]any{"start": "22:00", "end": "06:00"}}
	for _, hour := range []int{23, 5} {
		ts := time.Date(2026, 10, 27, hour, 30, 0, 0, time.Local).Unix()
		s := NewGatewayHeartbeatService(nil, cfg, nil, mkTime(ts))
		if !s.isActiveNow() {
			t.Fatalf("%d:30 should be in 22:00-06:00", hour)
		}
	}
	ts15 := time.Date(2026, 10, 27, 15, 0, 0, 0, time.Local).Unix()
	s15 := NewGatewayHeartbeatService(nil, cfg, nil, mkTime(ts15))
	if s15.isActiveNow() {
		t.Fatal("15:00 should be out of 22:00-06:00")
	}
}

// TestIsActiveNow_非法格式 Warn+true（配置非法时始终生效，避免误停心跳）。
func TestIsActiveNow_非法格式(t *testing.T) {
	s := NewGatewayHeartbeatService(nil, &HeartbeatConfig{
		ActiveHours: map[string]any{"start": "0800", "end": "22:00"}, // 缺冒号
	}, nil, mkTime(time.Date(2026, 10, 27, 15, 0, 0, 0, time.Local).Unix()))
	if !s.isActiveNow() {
		t.Fatal("invalid format should fallback to true")
	}
}

// TestIsActiveNow_数字经normalize回转 start/end 数字经回转后走正常区间判断。
func TestIsActiveNow_数字经normalize回转(t *testing.T) {
	// 1320→"22:00"、360→"06:00"：跨午夜区间 22:00-06:00
	// 本地 00:00（时区无关构造）→ nowMinutes=0 < 360 → 生效
	localMidnight := time.Date(2026, 10, 27, 0, 0, 0, 0, time.Local).Unix()
	s := NewGatewayHeartbeatService(nil, &HeartbeatConfig{
		ActiveHours: map[string]any{"start": 1320, "end": 360},
	}, nil, mkTime(localMidnight))
	if !s.isActiveNow() {
		t.Fatal("numeric hours should be normalized and evaluated as cross-midnight")
	}
}

// TestIsActiveNow_缺start或end键 normalize 后缺键 → true（对齐 Python isinstance 检查）。
func TestIsActiveNow_缺start或end键(t *testing.T) {
	s := NewGatewayHeartbeatService(nil, &HeartbeatConfig{
		ActiveHours: map[string]any{"start": "08:00"},
	}, nil, mkTime(0))
	if !s.isActiveNow() {
		t.Fatal("missing end key should fallback to true")
	}
}

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

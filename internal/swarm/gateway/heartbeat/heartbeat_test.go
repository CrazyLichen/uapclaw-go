package heartbeat

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/uapclaw/uapclaw-go/internal/swarm/e2a"
	"github.com/uapclaw/uapclaw-go/internal/swarm/schema"
)

// mkTime 构造固定 Unix 秒的假时钟。
func mkTime(sec int64) func() float64 {
	return func() float64 { return float64(sec) }
}

// fakeAgentClient 记录调用的测试桩（对齐 cron scheduler_test 模式；
// respond 收 ctx 参数——timeout 用例需挂起等待 ctx 取消，避免裸读共享字段竞态）。
type fakeAgentClient struct {
	mu      sync.Mutex
	calls   int
	respond func(ctx context.Context, call int) (*schema.AgentResponse, error)
	lastEnv *e2a.E2AEnvelope
	lastCtx context.Context
}

func (f *fakeAgentClient) SendRequest(ctx context.Context, env *e2a.E2AEnvelope) (*schema.AgentResponse, error) {
	f.mu.Lock()
	f.calls++
	n := f.calls
	f.lastEnv = env
	f.lastCtx = ctx
	fn := f.respond
	f.mu.Unlock()
	if fn == nil {
		return &schema.AgentResponse{OK: true, Payload: map[string]any{"heartbeat": "HEARTBEAT_OK"}}, nil
	}
	return fn(ctx, n)
}

func (f *fakeAgentClient) Calls() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

// fakePublisher 收集 relay 消息的测试桩。
type fakePublisher struct {
	mu   sync.Mutex
	msgs []*schema.Message
}

func (f *fakePublisher) PublishRobotMessages(msg *schema.Message) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.msgs = append(f.msgs, msg)
}

func (f *fakePublisher) Messages() []*schema.Message {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]*schema.Message{}, f.msgs...)
}

// waitFor 轮询等待条件成立（对齐 cron 测试辅助）。
func waitFor(t *testing.T, cond func() bool, timeout time.Duration) bool {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return true
		}
		time.Sleep(5 * time.Millisecond)
	}
	return false
}

// TestStart_先睡后跳 启动后 interval 内不 tick，interval 后首跳。
func TestStart_先睡后跳(t *testing.T) {
	fc := &fakeAgentClient{}
	iv := 0.05 // 50ms
	s := NewGatewayHeartbeatService(fc, &HeartbeatConfig{IntervalSeconds: iv}, nil, nil)
	if err := s.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer s.Stop()
	// 30ms（< interval 50ms）时不应有调用——time.After(50ms) 不会提前触发
	time.Sleep(30 * time.Millisecond)
	if fc.Calls() != 0 {
		t.Fatalf("should not tick before first interval, got %d calls", fc.Calls())
	}
	// interval 过后应有首次调用
	waitFor(t, func() bool { return fc.Calls() >= 1 }, 2*time.Second)
}

// TestStart_幂等 重复 Start 不产生第二个循环。
func TestStart_幂等(t *testing.T) {
	fc := &fakeAgentClient{}
	s := NewGatewayHeartbeatService(fc, &HeartbeatConfig{IntervalSeconds: 10}, nil, nil)
	if err := s.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer s.Stop()
	if err := s.Start(context.Background()); err != nil {
		t.Fatalf("second Start: %v", err)
	}
	if !s.IsRunning() {
		t.Fatal("should be running")
	}
}

// TestStop 停止后不再 tick；二次 Stop 安全；IsRunning=false。
func TestStop(t *testing.T) {
	fc := &fakeAgentClient{}
	s := NewGatewayHeartbeatService(fc, &HeartbeatConfig{IntervalSeconds: 0.02}, nil, nil)
	if err := s.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	waitFor(t, func() bool { return fc.Calls() >= 1 }, 2*time.Second)
	s.Stop()
	if s.IsRunning() {
		t.Fatal("should not be running after Stop")
	}
	calls := fc.Calls()
	time.Sleep(80 * time.Millisecond)
	if fc.Calls() != calls {
		t.Fatalf("should not tick after Stop: %d -> %d", calls, fc.Calls())
	}
	s.Stop() // 二次 Stop 安全
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

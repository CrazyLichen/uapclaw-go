package heartbeat

import (
	"context"
	"errors"
	"regexp"
	"strings"
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

// ──────────────────────────── Task 4: tick 六步 ────────────────────────────

// relayCfg 构造带 relay 的服务配置。
func relayCfg() *HeartbeatConfig {
	target := "web"
	return &HeartbeatConfig{
		IntervalSeconds: 10,
		ChannelID:       HeartbeatChannelID,
		RelayChannelID:  &target,
	}
}

// TestTick_envelope字段断言 请求 ID/session ID 正则格式、params 三键、channel_id。
func TestTick_envelope字段断言(t *testing.T) {
	fc := &fakeAgentClient{}
	s := NewGatewayHeartbeatService(fc, relayCfg(), nil, nil)
	s.tick(context.Background())

	env := fc.lastEnv
	if env == nil {
		t.Fatal("envelope not captured")
	}
	// request_id: heartbeat-{hex毫秒}_{6hex}
	reID := regexp.MustCompile(`^heartbeat-[0-9a-f]+_[0-9a-f]{6}$`)
	if !reID.MatchString(env.RequestID) {
		t.Fatalf("request_id format: %q", env.RequestID)
	}
	// session_id: heartbeat_{hex毫秒}_{6hex}
	reSID := regexp.MustCompile(`^heartbeat_[0-9a-f]+_[0-9a-f]{6}$`)
	if !reSID.MatchString(env.SessionID) {
		t.Fatalf("session_id format: %q", env.SessionID)
	}
	if env.Channel != "__heartbeat__" {
		t.Fatalf("channel: %q", env.Channel)
	}
	if env.Params["heartbeat"] != HeartbeatPrompt {
		t.Fatalf("params.heartbeat not verbatim prompt: %q", env.Params["heartbeat"])
	}
	runMap, ok := env.Params["run"].(map[string]any)
	if !ok {
		t.Fatalf("params.run type: %T", env.Params["run"])
	}
	if runMap["kind"] != "heartbeat" {
		t.Fatalf("run.kind: %v", runMap["kind"])
	}
	runCtx, ok := runMap["context"].(map[string]any)
	if !ok {
		t.Fatalf("run.context type: %T", runMap["context"])
	}
	if runCtx["reason"] != "interval" {
		t.Fatalf("run.context.reason: %v", runCtx["reason"])
	}
	if runCtx["session_id"] != env.SessionID {
		t.Fatalf("run.context.session_id mismatch: %v", runCtx["session_id"])
	}
	// request_id 与 session_id 共享时间戳与后缀
	if !strings.HasSuffix(env.RequestID, strings.SplitN(env.SessionID, "_", 2)[1]) {
		t.Fatalf("request_id/session_id suffix mismatch: %q vs %q", env.RequestID, env.SessionID)
	}
}

// TestTick_心跳OK直接取 payload.heartbeat 字符串。
func TestTick_心跳OK直接取(t *testing.T) {
	fc := &fakeAgentClient{
		respond: func(context.Context, int) (*schema.AgentResponse, error) {
			return &schema.AgentResponse{OK: true, Payload: map[string]any{"heartbeat": "HEARTBEAT_OK"}}, nil
		},
	}
	fp := &fakePublisher{}
	s := NewGatewayHeartbeatService(fc, relayCfg(), fp, nil)
	s.tick(context.Background())
	ok, at := s.LastTickOK(), s.LastTickAt()
	if ok == nil || *ok != true {
		t.Fatalf("lastTickOK should be true, got %v", ok)
	}
	if at == nil || *at <= 0 {
		t.Fatalf("lastTickAt should be set, got %v", at)
	}
	if len(fp.Messages()) != 1 {
		t.Fatalf("relay msgs: %d", len(fp.Messages()))
	}
	msg := fp.Messages()[0]
	if msg.Payload["heartbeat"] != "HEARTBEAT_OK" {
		t.Fatalf("relay payload: %v", msg.Payload)
	}
}

// TestTick_contentOutput三级提取 payload.content.output 提取。
func TestTick_contentOutput三级提取(t *testing.T) {
	fc := &fakeAgentClient{
		respond: func(context.Context, int) (*schema.AgentResponse, error) {
			return &schema.AgentResponse{OK: true, Payload: map[string]any{
				"content": map[string]any{"output": "任务执行完成", "result_type": "answer"},
			}}, nil
		},
	}
	fp := &fakePublisher{}
	s := NewGatewayHeartbeatService(fc, relayCfg(), fp, nil)
	s.tick(context.Background())
	msgs := fp.Messages()
	if len(msgs) != 1 || msgs[0].Payload["heartbeat"] != "任务执行完成" {
		t.Fatalf("relay payload: %v", msgs)
	}
}

// TestTick_content字符串提取 payload.content 为字符串直接取。
func TestTick_content字符串提取(t *testing.T) {
	fc := &fakeAgentClient{
		respond: func(context.Context, int) (*schema.AgentResponse, error) {
			return &schema.AgentResponse{OK: true, Payload: map[string]any{"content": "纯文本响应"}}, nil
		},
	}
	fp := &fakePublisher{}
	s := NewGatewayHeartbeatService(fc, relayCfg(), fp, nil)
	s.tick(context.Background())
	msgs := fp.Messages()
	if len(msgs) != 1 || msgs[0].Payload["heartbeat"] != "纯文本响应" {
		t.Fatalf("relay payload: %v", msgs)
	}
}

// TestTick_皆不命中空串 payload 无可提取字段 → heartbeat 空串仍 relay。
func TestTick_皆不命中空串(t *testing.T) {
	fc := &fakeAgentClient{
		respond: func(context.Context, int) (*schema.AgentResponse, error) {
			return &schema.AgentResponse{OK: true, Payload: map[string]any{"other": 1}}, nil
		},
	}
	fp := &fakePublisher{}
	s := NewGatewayHeartbeatService(fc, relayCfg(), fp, nil)
	s.tick(context.Background())
	msgs := fp.Messages()
	if len(msgs) != 1 || msgs[0].Payload["heartbeat"] != "" {
		t.Fatalf("relay payload should be empty string: %v", msgs)
	}
}

// TestTick_relay消息字段 ID 前缀/EventType/Payload/OK/ChannelID/SessionID/Timestamp。
func TestTick_relay消息字段(t *testing.T) {
	fc := &fakeAgentClient{}
	fp := &fakePublisher{}
	s := NewGatewayHeartbeatService(fc, relayCfg(), fp, nil)
	s.tick(context.Background())
	msg := fp.Messages()[0]
	if !strings.HasPrefix(msg.ID, "heartbeat-relay-heartbeat-") {
		t.Fatalf("relay ID: %q", msg.ID)
	}
	if msg.Type != schema.MessageTypeEvent {
		t.Fatalf("relay Type: %v", msg.Type)
	}
	if msg.EventType != schema.EventTypeHeartbeatRelay {
		t.Fatalf("relay EventType: %v", msg.EventType)
	}
	if msg.ChannelID != "web" {
		t.Fatalf("relay ChannelID: %q", msg.ChannelID)
	}
	if msg.SessionID == "" || !strings.HasPrefix(msg.SessionID, "heartbeat_") {
		t.Fatalf("relay SessionID: %q", msg.SessionID)
	}
	if !msg.OK {
		t.Fatal("relay OK should be true")
	}
	if msg.Timestamp <= 0 {
		t.Fatalf("relay Timestamp: %v", msg.Timestamp)
	}
	if string(msg.Params) != "{}" {
		t.Fatalf("relay Params should be {}: %q", string(msg.Params))
	}
}

// TestTick_OK大小写不敏感 "heartbeat_ok" 小写也判定 OK。
func TestTick_OK大小写不敏感(t *testing.T) {
	fc := &fakeAgentClient{
		respond: func(context.Context, int) (*schema.AgentResponse, error) {
			return &schema.AgentResponse{OK: true, Payload: map[string]any{"heartbeat": " heartbeat_ok "}}, nil
		},
	}
	s := NewGatewayHeartbeatService(fc, relayCfg(), nil, nil)
	s.tick(context.Background())
	// 大小写不敏感判定仅影响日志分支，行为断言经 lastTickOK
	if ok := s.LastTickOK(); ok == nil || !*ok {
		t.Fatal("lastTickOK should be true")
	}
}

// TestTick_relay关闭 RelayChannelID nil 或 publisher nil → 不发消息。
func TestTick_relay关闭(t *testing.T) {
	// RelayChannelID nil（无 relay 配置）
	fc := &fakeAgentClient{}
	fp := &fakePublisher{}
	s := NewGatewayHeartbeatService(fc, &HeartbeatConfig{IntervalSeconds: 10}, fp, nil)
	s.tick(context.Background())
	if len(fp.Messages()) != 0 {
		t.Fatalf("no relay expected, got %d", len(fp.Messages()))
	}
	// publisher nil（无 panic 即通过）
	s2 := NewGatewayHeartbeatService(fc, relayCfg(), nil, nil)
	s2.tick(context.Background())
}

// TestTick_非活跃时段 isActiveNow false → fake 无调用。
func TestTick_非活跃时段(t *testing.T) {
	fc := &fakeAgentClient{}
	// 假时钟固定 12:00（本地），活跃时段 00:00-00:01 → 必然跳过
	ts := time.Date(2026, 10, 27, 12, 0, 0, 0, time.Local).Unix()
	s := NewGatewayHeartbeatService(fc, &HeartbeatConfig{
		IntervalSeconds: 10,
		ActiveHours:     map[string]any{"start": "00:00", "end": "00:01"},
	}, nil, mkTime(ts))
	s.tick(context.Background())
	if fc.Calls() != 0 {
		t.Fatalf("should skip due to inactive hours, got %d calls", fc.Calls())
	}
}

// TestTick_nil客户端 nil AgentRequester 守卫 → lastTickOK=false 不 panic。
// 对齐 Python agent_client=None 时 send_request AttributeError → except 置 failed。
func TestTick_nil客户端(t *testing.T) {
	s := NewGatewayHeartbeatService(nil, relayCfg(), nil, nil)
	s.tick(context.Background()) // 不 panic 即通过
	if ok := s.LastTickOK(); ok == nil || *ok {
		t.Fatal("lastTickOK should be false when agentClient is nil")
	}
}

// TestTick_timeout超时 fake 挂起 + TimeoutSeconds=50ms → ctx deadline。
func TestTick_timeout超时(t *testing.T) {
	fc := &fakeAgentClient{
		respond: func(ctx context.Context, _ int) (*schema.AgentResponse, error) {
			<-ctx.Done() // 挂起直到 ctx 超时/取消
			return nil, ctx.Err()
		},
	}
	to := 0.05
	s := NewGatewayHeartbeatService(fc, &HeartbeatConfig{
		IntervalSeconds: 10,
		TimeoutSeconds:  &to,
	}, nil, nil)
	s.tick(context.Background())
	if ok := s.LastTickOK(); ok == nil || *ok {
		t.Fatal("lastTickOK should be false on timeout")
	}
}

// TestTick_无timeout配置 ctx 直传无 deadline。
func TestTick_无timeout配置(t *testing.T) {
	fc := &fakeAgentClient{}
	s := NewGatewayHeartbeatService(fc, &HeartbeatConfig{IntervalSeconds: 10}, nil, nil)
	s.tick(context.Background())
	if _, hasDeadline := fc.lastCtx.Deadline(); hasDeadline {
		t.Fatal("ctx should have no deadline when timeout not configured")
	}
}

// TestTick_错误分支 fake 返回 error → lastTickOK=false。
func TestTick_错误分支(t *testing.T) {
	fc := &fakeAgentClient{
		respond: func(context.Context, int) (*schema.AgentResponse, error) {
			return nil, errors.New("connection refused")
		},
	}
	s := NewGatewayHeartbeatService(fc, relayCfg(), nil, nil)
	s.tick(context.Background())
	if ok := s.LastTickOK(); ok == nil || *ok {
		t.Fatal("lastTickOK should be false on error")
	}
	if at := s.LastTickAt(); at == nil {
		t.Fatal("lastTickAt should be set on error")
	}
}

// TestTick_停机取消豁免 ctx 取消 → lastTick 状态保持不变（对齐 Python
// CancelledError 不进 except Exception 语义，审查修复方案A）。
func TestTick_停机取消豁免(t *testing.T) {
	fc := &fakeAgentClient{
		respond: func(ctx context.Context, call int) (*schema.AgentResponse, error) {
			if call == 1 {
				// 首次调用：成功
				return &schema.AgentResponse{OK: true, Payload: map[string]any{"heartbeat": "HEARTBEAT_OK"}}, nil
			}
			// 二次调用：挂起直到 ctx 取消（模拟 Stop 时 in-flight）
			<-ctx.Done()
			return nil, ctx.Err()
		},
	}
	s := NewGatewayHeartbeatService(fc, relayCfg(), nil, mkTime(1000))

	// 先成功一次 → lastTickOK=true / lastTickAt=1000
	s.tick(context.Background())
	if ok := s.LastTickOK(); ok == nil || !*ok {
		t.Fatal("precondition: lastTickOK should be true after successful tick")
	}
	if at := s.LastTickAt(); at == nil || *at != 1000 {
		t.Fatalf("precondition: lastTickAt should be 1000, got %v", at)
	}

	// 取消 ctx 后 tick → 豁免失败分支，lastTick 状态保持成功值
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	s.tick(ctx)
	if ok := s.LastTickOK(); ok == nil || !*ok {
		t.Fatal("lastTickOK should stay true on context cancel")
	}
	if at := s.LastTickAt(); at == nil || *at != 1000 {
		t.Fatalf("lastTickAt should stay 1000 on context cancel, got %v", at)
	}
}

// TestTick_父ctx超时归requestFailed 无 TimeoutSeconds 配置但父 ctx 带
// deadline 超时 → 不走 timeout 日志分支（归类收紧，审查修复）。
func TestTick_父ctx超时归requestFailed(t *testing.T) {
	fc := &fakeAgentClient{
		respond: func(ctx context.Context, _ int) (*schema.AgentResponse, error) {
			<-ctx.Done() // 挂起直到父 ctx deadline 超时
			return nil, ctx.Err()
		},
	}
	s := NewGatewayHeartbeatService(fc, &HeartbeatConfig{
		IntervalSeconds: 10, // 无 TimeoutSeconds
	}, nil, nil)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	s.tick(ctx)
	// lastTickOK=false（仍是失败），但归类为 request failed（无 Warn timeout）
	if ok := s.LastTickOK(); ok == nil || *ok {
		t.Fatal("lastTickOK should be false on parent ctx deadline exceeded")
	}
}

// ──────────────────────────── Task 5: get/set_conf ────────────────────────────

// TestGetHeartbeatConf 三键断言。
func TestGetHeartbeatConf(t *testing.T) {
	target := "web"
	s := NewGatewayHeartbeatService(nil, &HeartbeatConfig{
		IntervalSeconds: 30,
		RelayChannelID:  &target,
		ActiveHours:     map[string]any{"start": "08:00", "end": "22:00"},
	}, nil, nil)
	conf := s.GetHeartbeatConf()
	if conf["every"] != 30.0 {
		t.Fatalf("every: %v", conf["every"])
	}
	if conf["target"] != "web" {
		t.Fatalf("target: %v", conf["target"])
	}
	ah, ok := conf["active_hours"].(map[string]string)
	if !ok || ah["start"] != "08:00" || ah["end"] != "22:00" {
		t.Fatalf("active_hours: %v", conf["active_hours"])
	}
}

// TestGetHeartbeatConf_nil值 target/active_hours 为 nil 时键仍存在。
func TestGetHeartbeatConf_nil值(t *testing.T) {
	s := NewGatewayHeartbeatService(nil, &HeartbeatConfig{IntervalSeconds: 60}, nil, nil)
	conf := s.GetHeartbeatConf()
	if _, exists := conf["target"]; !exists {
		t.Fatal("target key must exist (nil value)")
	}
	if conf["target"] != nil {
		t.Fatalf("target should be nil, got %v", conf["target"])
	}
	if conf["active_hours"] != nil {
		t.Fatalf("active_hours should be nil, got %v", conf["active_hours"])
	}
}

// TestSetHeartbeatConf_every非法 every<=0 → error（对齐 ValueError）。
func TestSetHeartbeatConf_every非法(t *testing.T) {
	s := NewGatewayHeartbeatService(nil, &HeartbeatConfig{IntervalSeconds: 60}, nil, nil)
	zero := 0.0
	if err := s.SetHeartbeatConf(&zero, nil, nil); err == nil {
		t.Fatal("every<=0 should error")
	}
	if err := s.SetHeartbeatConf(nil, nil, nil); err != nil {
		t.Fatalf("all-nil no-op should pass, got %v", err)
	}
}

// TestSetHeartbeatConf_热重启 running→set→stop+start，间隔下轮生效。
func TestSetHeartbeatConf_热重启(t *testing.T) {
	fc := &fakeAgentClient{}
	s := NewGatewayHeartbeatService(fc, &HeartbeatConfig{IntervalSeconds: 30}, nil, nil)
	if err := s.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	newEvery := 0.03
	if err := s.SetHeartbeatConf(&newEvery, nil, nil); err != nil {
		t.Fatalf("SetHeartbeatConf: %v", err)
	}
	defer s.Stop()
	if !s.IsRunning() {
		t.Fatal("should restart and be running")
	}
	// 新间隔生效：30s 间隔在旧配置下永不 tick，新配置 30ms 内应 tick
	waitFor(t, func() bool { return fc.Calls() >= 1 }, 2*time.Second)
}

// TestSetHeartbeatConf_lastTick重置 set 后 lastTickOK/At 归 nil。
func TestSetHeartbeatConf_lastTick重置(t *testing.T) {
	s := NewGatewayHeartbeatService(nil, &HeartbeatConfig{IntervalSeconds: 60}, nil, nil)
	// 模拟已执行过一次
	s.mu.Lock()
	s.lastTickOK = ptrBool(true)
	s.lastTickAt = ptrFloat64(100.0)
	s.mu.Unlock()
	newEvery := 120.0
	if err := s.SetHeartbeatConf(&newEvery, nil, nil); err != nil {
		t.Fatalf("SetHeartbeatConf: %v", err)
	}
	if s.LastTickOK() != nil || s.LastTickAt() != nil {
		t.Fatalf("lastTick should reset to nil, got %v %v", s.LastTickOK(), s.LastTickAt())
	}
	// 验证配置已更新
	if conf := s.GetHeartbeatConf(); conf["every"] != 120.0 {
		t.Fatalf("every after set: %v", conf["every"])
	}
}

// TestSetHeartbeatConf_target与activeHours 两者更新生效（含数值回转链）。
func TestSetHeartbeatConf_target与activeHours(t *testing.T) {
	s := NewGatewayHeartbeatService(nil, &HeartbeatConfig{IntervalSeconds: 60}, nil, nil)
	newTarget := "tui"
	if err := s.SetHeartbeatConf(nil, &newTarget, map[string]any{"start": "09:00", "end": "18:00"}); err != nil {
		t.Fatalf("SetHeartbeatConf: %v", err)
	}
	conf := s.GetHeartbeatConf()
	if conf["target"] != "tui" {
		t.Fatalf("target: %v", conf["target"])
	}
	ah, _ := conf["active_hours"].(map[string]string)
	if ah["start"] != "09:00" {
		t.Fatalf("active_hours: %v", conf["active_hours"])
	}
}

// TestSetHeartbeatConf_activeHours数值原样存储 数值 1320 存储后经 GetHeartbeatConf 回转 "22:00"
// （对齐 Python set_conf 不做类型转换、get_conf normalize 的回转链）。
func TestSetHeartbeatConf_activeHours数值原样存储(t *testing.T) {
	s := NewGatewayHeartbeatService(nil, &HeartbeatConfig{IntervalSeconds: 60}, nil, nil)
	if err := s.SetHeartbeatConf(nil, nil, map[string]any{"start": 1320, "end": "06:00"}); err != nil {
		t.Fatalf("SetHeartbeatConf: %v", err)
	}
	conf := s.GetHeartbeatConf()
	ah, _ := conf["active_hours"].(map[string]string)
	if ah["start"] != "22:00" || ah["end"] != "06:00" {
		t.Fatalf("numeric passthrough + normalize failed: %v", conf["active_hours"])
	}
}

// TestSetHeartbeatConf_activeHours清除 空传 {} → 空 map（生效中清空语义）
// 语义校准：非 nil 空 map 也被接受存储（GetHeartbeatConf 后 normalize 为空 map）。
func TestSetHeartbeatConf_activeHours清除(t *testing.T) {
	s := NewGatewayHeartbeatService(nil, &HeartbeatConfig{
		IntervalSeconds: 60,
		ActiveHours:     map[string]any{"start": "08:00", "end": "22:00"},
	}, nil, nil)
	if err := s.SetHeartbeatConf(nil, nil, map[string]any{}); err != nil {
		t.Fatalf("SetHeartbeatConf: %v", err)
	}
	// 空 map 存储 → GetHeartbeatConf normalize 为空 map（len 0 非空配置——始终生效）
	conf := s.GetHeartbeatConf()
	if ah, _ := conf["active_hours"].(map[string]string); len(ah) != 0 {
		t.Fatalf("empty active_hours should store as empty, got %v", conf["active_hours"])
	}
	// isActiveNow 对空 map 返回 true（len==0 → 始终生效，Task 2 已测）
	if !s.isActiveNow() {
		t.Fatal("empty active_hours should be always-active")
	}
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

package cron

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/uapclaw/uapclaw-go/internal/swarm/e2a"
	"github.com/uapclaw/uapclaw-go/internal/swarm/schema"
)

// ──────────────────────────── 测试辅助 ────────────────────────────

// fakeClock 可手动推进的假时钟
type fakeClock struct {
	mu  sync.Mutex
	now float64
}

func (c *fakeClock) Now() float64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *fakeClock) Advance(sec float64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now += sec
}

// fakeAgentClient 可编程 agent 客户端
type fakeAgentClient struct {
	mu      sync.Mutex
	calls   int
	respond func(call int) (*schema.AgentResponse, error)
}

func (f *fakeAgentClient) SendRequest(ctx context.Context, env *e2a.E2AEnvelope) (*schema.AgentResponse, error) {
	f.mu.Lock()
	f.calls++
	n := f.calls
	fn := f.respond
	f.mu.Unlock()
	if fn == nil {
		return &schema.AgentResponse{OK: true, Payload: map[string]any{"content": map[string]any{"output": "done"}}}, nil
	}
	return fn(n)
}

func (f *fakeAgentClient) Calls() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

// fakePublisher 收集推送消息
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

// ptrInt int 指针辅助
func ptrInt(v int) *int { return &v }

// ptrBool bool 指针辅助
func ptrBool(v bool) *bool { return &v }

// waitFor 轮询等待条件成立（最长 timeout）
func waitFor(t *testing.T, cond func() bool, timeout time.Duration) bool {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return true
		}
		time.Sleep(20 * time.Millisecond)
	}
	return cond()
}

// ──────────────────────────── 调度器测试 ────────────────────────────

// TestSchedulerWakePushUpdateOrder 假时钟验证事件三分支顺序
func TestSchedulerWakePushUpdateOrder(t *testing.T) {
	clock := &fakeClock{now: float64(time.Date(2026, 12, 25, 8, 0, 0, 0, time.UTC).Unix())}
	// 启动后 next fire 9:00（表达式 0 9 * * *），wake_offset=300 → wake 8:55
	store := NewCronJobStore(filepath.Join(t.TempDir(), "cron_jobs.json"))
	_, err := store.CreateJob(CreateJobParams{
		Name: "t", CronExpr: "0 9 * * *", Timezone: "UTC",
		Description: "d", Targets: "web", Enabled: true,
		WakeOffsetSeconds: ptrInt(300),
	})
	if err != nil {
		t.Fatal(err)
	}

	agent := &fakeAgentClient{
		respond: func(call int) (*schema.AgentResponse, error) {
			// agent 慢响应：真实等待 500ms，确保 push 时刻（推时钟后 ~100-300ms 处理）时尚未完成
			time.Sleep(500 * time.Millisecond)
			return &schema.AgentResponse{OK: true, Payload: map[string]any{"content": map[string]any{"output": "final-result"}}}, nil
		},
	}
	pub := &fakePublisher{}
	svc := NewCronSchedulerService(store, agent, pub, clock.Now)
	// 缩短轮询间隔加速测试（对齐 Python 实例属性 self._store_poll_interval 可调）
	svc.storePollInterval = 0.1

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := svc.Start(ctx); err != nil {
		t.Fatal(err)
	}
	defer svc.Stop()

	// 推进到 8:55（wake 触发，agent 开始执行）
	clock.Advance(3300) // 8:00 → 8:55
	time.Sleep(100 * time.Millisecond)
	// 推进到 9:00（push 触发，agent 未完成 → 占位符）
	clock.Advance(300)
	waitFor(t, func() bool { return len(pub.Messages()) >= 1 }, 2*time.Second)

	msgs := pub.Messages()
	if len(msgs) < 1 {
		t.Fatal("placeholder should be pushed")
	}
	first := msgs[0]
	content, _ := first.Payload["content"].(string)
	if !strings.Contains(content, "正在执行中") {
		t.Errorf("first push should be placeholder, got %q", content)
	}

	// 等待 agent 完成触发 push_update（agent 真实阻塞 500ms）
	waitFor(t, func() bool { return len(pub.Messages()) >= 2 }, 3*time.Second)
	msgs = pub.Messages()
	if len(msgs) < 2 {
		t.Fatalf("push_update should fire after agent done, got %d messages", len(msgs))
	}
	second := msgs[1]
	content, _ = second.Payload["content"].(string)
	if !strings.Contains(content, "final-result") {
		t.Errorf("second push should be final result, got %q", content)
	}
	// 第二条为终稿：is_placeholder=false
	cronMeta, _ := second.Payload["cron"].(map[string]any)
	if cronMeta != nil {
		if ph, ok := cronMeta["is_placeholder"].(bool); ok && ph {
			t.Error("second push should not be placeholder")
		}
	}
}

// TestSchedulerDirectFinalPush agent 完成后 push 直推终稿（无占位符）
func TestSchedulerDirectFinalPush(t *testing.T) {
	clock := &fakeClock{now: float64(time.Date(2026, 12, 25, 8, 0, 0, 0, time.UTC).Unix())}
	store := NewCronJobStore(filepath.Join(t.TempDir(), "cron_jobs.json"))
	_, err := store.CreateJob(CreateJobParams{
		Name: "t", CronExpr: "0 9 * * *", Timezone: "UTC",
		Description: "d", Targets: "web", Enabled: true,
		WakeOffsetSeconds: ptrInt(300),
	})
	if err != nil {
		t.Fatal(err)
	}

	agent := &fakeAgentClient{
		respond: func(call int) (*schema.AgentResponse, error) {
			// agent 立即完成
			return &schema.AgentResponse{OK: true, Payload: map[string]any{"content": map[string]any{"output": "fast"}}}, nil
		},
	}
	pub := &fakePublisher{}
	svc := NewCronSchedulerService(store, agent, pub, clock.Now)
	// 缩短轮询间隔加速测试（对齐 Python 实例属性 self._store_poll_interval 可调）
	svc.storePollInterval = 0.1

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	svc.Start(ctx)
	defer svc.Stop()

	clock.Advance(3300) // wake at 8:55
	time.Sleep(50 * time.Millisecond)
	clock.Advance(300) // push at 9:00
	waitFor(t, func() bool { return len(pub.Messages()) >= 1 }, 2*time.Second)
	msgs := pub.Messages()
	if len(msgs) == 0 {
		t.Fatal("final should be pushed directly")
	}
	content, _ := msgs[0].Payload["content"].(string)
	if !strings.Contains(content, "fast") {
		t.Errorf("direct final push expected, got %q", content)
	}
	// 只推一次终稿，无占位符
	if len(msgs) != 1 {
		t.Errorf("expected exactly 1 message, got %d", len(msgs))
	}
}

// TestSchedulerDeleteAfterRun 单次任务 push 后删除
func TestSchedulerDeleteAfterRun(t *testing.T) {
	clock := &fakeClock{now: float64(time.Date(2026, 12, 25, 8, 0, 0, 0, time.UTC).Unix())}
	store := NewCronJobStore(filepath.Join(t.TempDir(), "cron_jobs.json"))
	job, err := store.CreateJob(CreateJobParams{
		Name: "once", CronExpr: "0 9 * * *", Timezone: "UTC",
		Description: "d", Targets: "web", Enabled: true,
		DeleteAfterRun:    ptrBool(true),
		WakeOffsetSeconds: ptrInt(0),
	})
	if err != nil {
		t.Fatal(err)
	}
	pub := &fakePublisher{}
	svc := NewCronSchedulerService(store, &fakeAgentClient{}, pub, clock.Now)
	// 缩短轮询间隔加速测试
	svc.storePollInterval = 0.1
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	svc.Start(ctx)
	defer svc.Stop()

	clock.Advance(3600) // 直接到 push
	waitFor(t, func() bool {
		got, _ := store.GetJob(job.ID)
		return got == nil
	}, 2*time.Second)
	if got, _ := store.GetJob(job.ID); got != nil {
		t.Fatal("delete_after_run job should be deleted after push")
	}
}

// TestScheduler过期标记 过期 one-shot 在 Reload 时标记 expired
func TestScheduler过期标记(t *testing.T) {
	store := NewCronJobStore(filepath.Join(t.TempDir(), "cron_jobs.json"))
	job, err := store.CreateJob(CreateJobParams{
		Name: "old", CronExpr: "0 0 17 28 12 * 2020", Timezone: "UTC",
		Description: "d", Targets: "web", Enabled: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	pub := &fakePublisher{}
	svc := NewCronSchedulerService(store, &fakeAgentClient{}, pub, nil)
	if err := svc.Reload(); err != nil {
		t.Fatal(err)
	}
	got, _ := store.GetJob(job.ID)
	if got == nil || got.Enabled || !got.Expired {
		t.Fatal("expired one-shot should be marked disabled+expired")
	}
}

// TestSchedulerMtime轮询外部变更 外部写文件触发热重载
func TestSchedulerMtime轮询外部变更(t *testing.T) {
	clock := &fakeClock{now: float64(time.Date(2026, 12, 25, 0, 0, 0, 0, time.UTC).Unix())}
	dir := t.TempDir()
	p := filepath.Join(dir, "cron_jobs.json")
	store := NewCronJobStore(p)
	// 先落一个初始 job：Start 时文件已存在，last_store_mtime 非 0
	// （对齐 Python 语义：last==0 时永不触发变更检测）
	_, err := store.CreateJob(CreateJobParams{
		Name: "seed", CronExpr: "0 9 * * *", Timezone: "UTC",
		Description: "d", Targets: "web", Enabled: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	// 拨回 mtime 避免与后续写入同刻（文件系统 mtime 精度兜底）
	old := time.Now().Add(-10 * time.Second)
	if err := os.Chtimes(p, old, old); err != nil {
		t.Fatal(err)
	}
	pub := &fakePublisher{}
	svc := NewCronSchedulerService(store, &fakeAgentClient{}, pub, clock.Now)
	// 缩短轮询间隔加速测试（对齐 Python 实例属性 self._store_poll_interval 可调）
	svc.storePollInterval = 0.2
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	svc.Start(ctx)
	defer svc.Stop()

	// 外部进程写新 job（bump mtime）
	external := NewCronJobStore(p)
	extJob, err := external.CreateJob(CreateJobParams{
		Name: "ext", CronExpr: "0 9 * * *", Timezone: "UTC",
		Description: "d", Targets: "web", Enabled: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	// 等待轮询周期（预留 5s 上限；jobs map 的 key 是 job.ID）
	waitFor(t, func() bool {
		svc.mu.Lock()
		defer svc.mu.Unlock()
		_, has := svc.jobs[extJob.ID]
		return has
	}, 5*time.Second)
	svc.mu.Lock()
	_, has := svc.jobs[extJob.ID]
	svc.mu.Unlock()
	if !has {
		t.Fatal("mtime polling should detect external change")
	}
}

// TestProcessScheduler注册表 进程级注册表基本行为
func TestProcessScheduler注册表(t *testing.T) {
	ResetProcessSchedulerForTest()
	if GetProcessScheduler() != nil {
		t.Fatal("initial registry should be nil")
	}
	svc := NewCronSchedulerService(nil, nil, nil, nil)
	SetProcessScheduler(svc)
	if GetProcessScheduler() != svc {
		t.Fatal("registry should return set instance")
	}
	ResetProcessSchedulerForTest()
	if GetProcessScheduler() != nil {
		t.Fatal("reset should clear registry")
	}
}

// TestSchedulerReload并发 并发 Reload 无死锁无竞争
func TestSchedulerReload并发(t *testing.T) {
	store := NewCronJobStore(filepath.Join(t.TempDir(), "cron_jobs.json"))
	svc := NewCronSchedulerService(store, &fakeAgentClient{}, &fakePublisher{}, nil)
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = svc.Reload()
		}()
	}
	wg.Wait() // -race 下无死锁无竞争即通过
}

// TestSchedulerPushToTargets渠道元数据 feishu 渠道 metadata + 群聊路由
func TestSchedulerPushToTargets渠道元数据(t *testing.T) {
	svc := NewCronSchedulerService(nil, nil, nil, nil)
	// 注入 config loader
	svc.SetConfigLoader(func() map[string]any {
		return map[string]any{
			"channels": map[string]any{
				"feishu": map[string]any{
					"last_chat_id": "oc_123", "last_open_id": "ou_456",
					"group_digital_avatar": true, "my_user_id": "u_me",
				},
			},
		}
	})
	pub := &fakePublisher{}
	svc.publisher = pub
	job := &CronJob{ID: "j", Name: "n", Targets: "feishu", ChatType: "group", Timezone: "UTC"}
	state := &CronRunState{RunID: "j:100", JobID: "j", PushAtISO: "2026-12-25T09:00:00Z", WakeAtISO: "2026-12-25T08:55:00Z"}
	svc.pushToTargets(job, state, "hello", false)
	msgs := pub.Messages()
	if len(msgs) != 1 {
		t.Fatalf("expected 1 message, got %d", len(msgs))
	}
	m := msgs[0]
	if m.ChannelID != "feishu" {
		t.Errorf("channel = %q", m.ChannelID)
	}
	md := m.Metadata
	if md["feishu_chat_id"] != "oc_123" || md["feishu_open_id"] != "ou_456" {
		t.Errorf("feishu metadata missing: %v", md)
	}
	if md["chat_type"] != "group" || md["reply_candidate_feishu_open_id"] != "u_me" || md["is_cron_job"] != true {
		t.Errorf("group routing metadata missing: %v", md)
	}
	if !m.GroupDigitalAvatar {
		t.Error("group_digital_avatar should propagate")
	}
	// 消息 ID 对齐 cron-push-{run_id}-{channel_id}
	if !strings.HasPrefix(m.ID, "cron-push-j:100-feishu") {
		t.Errorf("message id = %q", m.ID)
	}
}

// TestSchedulerPushToTargets企业飞书路由 SessionMap session_id 路由
func TestSchedulerPushToTargets企业飞书路由(t *testing.T) {
	svc := NewCronSchedulerService(nil, nil, nil, nil)
	pub := &fakePublisher{}
	svc.publisher = pub
	job := &CronJob{
		ID: "j", Name: "n", Targets: "feishu_enterprise:cli_9",
		SessionID: "feishu::oc_chat::bot_1::ou_open::x::y", Timezone: "UTC",
	}
	state := &CronRunState{RunID: "j:100", JobID: "j", PushAtISO: "2026-12-25T09:00:00Z", WakeAtISO: "2026-12-25T08:55:00Z"}
	svc.pushToTargets(job, state, "hi", false)
	msgs := pub.Messages()
	if len(msgs) != 1 {
		t.Fatal("expected message")
	}
	m := msgs[0]
	if m.SessionID != "oc_chat" {
		t.Errorf("session = %q, want oc_chat", m.SessionID)
	}
	if m.Metadata["feishu_chat_id"] != "oc_chat" || m.Metadata["feishu_open_id"] != "ou_open" {
		t.Errorf("enterprise metadata: %v", m.Metadata)
	}
}

// TestSchedulerPushToTargetsWeb无session web 渠道不绑定 session_id
func TestSchedulerPushToTargetsWeb无session(t *testing.T) {
	svc := NewCronSchedulerService(nil, nil, nil, nil)
	pub := &fakePublisher{}
	svc.publisher = pub
	job := &CronJob{ID: "j", Name: "n", Targets: "web", SessionID: "some-sid", Timezone: "UTC"}
	state := &CronRunState{RunID: "j:100", JobID: "j", PushAtISO: "2026-12-25T09:00:00Z", WakeAtISO: "2026-12-25T08:55:00Z"}
	svc.pushToTargets(job, state, "x", false)
	m := pub.Messages()[0]
	if m.SessionID != "" {
		t.Errorf("web should not bind session_id, got %q", m.SessionID)
	}
}

// TestSchedulerPushToTargets空targets 空 targets 不推送
func TestSchedulerPushToTargets空targets(t *testing.T) {
	svc := NewCronSchedulerService(nil, nil, nil, nil)
	pub := &fakePublisher{}
	svc.publisher = pub
	job := &CronJob{ID: "j", Name: "n", Targets: "", Timezone: "UTC"}
	state := &CronRunState{RunID: "j:100", JobID: "j", PushAtISO: "2026-12-25T09:00:00Z", WakeAtISO: "2026-12-25T08:55:00Z"}
	svc.pushToTargets(job, state, "x", false)
	if len(pub.Messages()) != 0 {
		t.Error("empty targets should not push")
	}
}

// TestSchedulerPushToTargets钉钉兜底 dingtalk 渠道 routing_sid 兜底与 conversation_type 默认
func TestSchedulerPushToTargets钉钉兜底(t *testing.T) {
	svc := NewCronSchedulerService(nil, nil, nil, nil)
	pub := &fakePublisher{}
	svc.publisher = pub
	// 无 config loader：metadata 走空 map，dingtalk 兜底补 sender_id/type
	job := &CronJob{ID: "j", Name: "n", Targets: "dingtalk", SessionID: "sender-1", Timezone: "UTC"}
	state := &CronRunState{RunID: "j:100", JobID: "j", PushAtISO: "2026-12-25T09:00:00Z", WakeAtISO: "2026-12-25T08:55:00Z"}
	svc.pushToTargets(job, state, "x", false)
	msgs := pub.Messages()
	if len(msgs) != 1 {
		t.Fatal("expected message")
	}
	md := msgs[0].Metadata
	if md["dingtalk_sender_id"] != "sender-1" {
		t.Errorf("dingtalk_sender_id = %v, want sender-1", md["dingtalk_sender_id"])
	}
	if md["conversation_type"] != "1" {
		t.Errorf("conversation_type = %v, want 1", md["conversation_type"])
	}
}

// TestSchedulerPushToTargets私聊跳过路由 私聊创建任务跳过 IMOutboundPipeline 路由
func TestSchedulerPushToTargets私聊跳过路由(t *testing.T) {
	svc := NewCronSchedulerService(nil, nil, nil, nil)
	svc.SetConfigLoader(func() map[string]any {
		return map[string]any{
			"channels": map[string]any{
				"feishu": map[string]any{
					"group_digital_avatar": true, "my_user_id": "u_me",
				},
			},
		}
	})
	pub := &fakePublisher{}
	svc.publisher = pub
	job := &CronJob{ID: "j", Name: "n", Targets: "feishu", ChatType: "p2p", Timezone: "UTC"}
	state := &CronRunState{RunID: "j:100", JobID: "j", PushAtISO: "2026-12-25T09:00:00Z", WakeAtISO: "2026-12-25T08:55:00Z"}
	svc.pushToTargets(job, state, "x", false)
	msgs := pub.Messages()
	if len(msgs) != 1 {
		t.Fatal("expected message")
	}
	md := msgs[0].Metadata
	if _, ok := md["chat_type"]; ok {
		t.Errorf("p2p chat should not set chat_type metadata: %v", md)
	}
	if _, ok := md["is_cron_job"]; ok {
		t.Errorf("p2p chat should not set is_cron_job: %v", md)
	}
}

// TestExtractTextFromAgentPayload agent 响应文本提取矩阵
func TestExtractTextFromAgentPayload(t *testing.T) {
	cases := []struct {
		name    string
		payload map[string]any
		want    string
	}{
		{"nil", nil, ""},
		{"content dict output str", map[string]any{"content": map[string]any{"output": "hi"}}, "hi"},
		{"content dict output non-str", map[string]any{"content": map[string]any{"output": 42}}, "42"},
		{"content dict no output", map[string]any{"content": map[string]any{"x": 1}}, "map[x:1]"},
		{"content str", map[string]any{"content": "txt"}, "txt"},
		{"heartbeat fallback", map[string]any{"heartbeat": "hb"}, "hb"},
		{"text fallback", map[string]any{"text": "tt"}, "tt"},
		{"empty heartbeat then text", map[string]any{"heartbeat": "", "text": "tt"}, "tt"},
		{"nothing", map[string]any{"other": 1}, ""},
	}
	for _, c := range cases {
		if got := extractTextFromAgentPayload(c.payload); got != c.want {
			t.Errorf("%s: got %q, want %q", c.name, got, c.want)
		}
	}
}

// TestEventHeap 事件堆排序（AtTS 优先、Seq 稳定序）
func TestEventHeap(t *testing.T) {
	var h eventHeap
	// 同刻不同 seq
	heapPush(&h, &cronEvent{AtTS: 100, Seq: 3, Kind: "push"})
	heapPush(&h, &cronEvent{AtTS: 100, Seq: 1, Kind: "wake"})
	heapPush(&h, &cronEvent{AtTS: 50, Seq: 5, Kind: "push_update"})
	heapPush(&h, &cronEvent{AtTS: 100, Seq: 2, Kind: "push"})

	first := heapPop(&h)
	if first.AtTS != 50 || first.Seq != 5 {
		t.Errorf("first = AtTS %v Seq %v, want 50/5", first.AtTS, first.Seq)
	}
	second := heapPop(&h)
	if second.AtTS != 100 || second.Seq != 1 {
		t.Errorf("second = AtTS %v Seq %v, want 100/1", second.AtTS, second.Seq)
	}
	third := heapPop(&h)
	if third.Seq != 2 {
		t.Errorf("third Seq = %v, want 2", third.Seq)
	}
	fourth := heapPop(&h)
	if fourth.Seq != 3 {
		t.Errorf("fourth Seq = %v, want 3", fourth.Seq)
	}
	if got := heapPop(&h); got != nil {
		t.Error("empty heap pop should be nil")
	}
}

// TestGetOrCreateRun 状态创建与复用
func TestGetOrCreateRun(t *testing.T) {
	svc := NewCronSchedulerService(nil, nil, nil, nil)
	job := &CronJob{ID: "j", Name: "n", Timezone: "UTC", WakeOffsetSeconds: 300, Targets: "web"}

	s1 := svc.getOrCreateRun(job, "j:100")
	if s1 == nil {
		t.Fatal("state should be created")
	}
	if s1.PushAtISO == "" || s1.WakeAtISO == "" {
		t.Errorf("ISO fields should be set: %v", s1)
	}
	// 复用：同 runID 返回同一实例
	s2 := svc.getOrCreateRun(job, "j:100")
	if s1 != s2 {
		t.Error("same runID should return same state instance")
	}
	// run_id 尾段解析 push_ts
	if s1.PushAtISO != "1970-01-01T00:01:40Z" {
		t.Errorf("push_at = %q, want 1970-01-01T00:01:40Z", s1.PushAtISO)
	}
	// wake = push - 300s
	if s1.WakeAtISO != "1969-12-31T23:56:40Z" {
		t.Errorf("wake_at = %q, want 1969-12-31T23:56:40Z", s1.WakeAtISO)
	}
	// wake_offset 为 300 时 wake 应早于 push 300 秒
	wake, _ := time.Parse(time.RFC3339, s1.WakeAtISO)
	push, _ := time.Parse(time.RFC3339, s1.PushAtISO)
	if push.Sub(wake) != 300*time.Second {
		t.Errorf("push-wake = %v, want 300s", push.Sub(wake))
	}
}

// TestHandleEventPushUpdate重建任务 push_update 无 job 时从 state 重建
func TestHandleEventPushUpdate重建任务(t *testing.T) {
	svc := NewCronSchedulerService(nil, nil, nil, nil)
	pub := &fakePublisher{}
	svc.publisher = pub

	// 手动构造 runs 状态（job 已删除场景）
	svc.mu.Lock()
	svc.runs["gone:100"] = &CronRunState{
		RunID: "gone:100", JobID: "gone", JobName: "old",
		ResultText: "late-result", Targets: "web", Timezone: "UTC",
		Status: "succeeded", PushedFinal: false,
	}
	svc.mu.Unlock()

	// push_update 事件：job 不在 jobs 表中
	svc.handleEvent(context.Background(), &cronEvent{
		AtTS: svc.nowFn(), Kind: eventKindPushUpdate, JobID: "gone", RunID: "gone:100",
	})

	msgs := pub.Messages()
	if len(msgs) != 1 {
		t.Fatalf("rebuilt job should still push, got %d messages", len(msgs))
	}
	content, _ := msgs[0].Payload["content"].(string)
	if content != "late-result" {
		t.Errorf("content = %q, want late-result", content)
	}
	// pushed_final 标记
	svc.mu.Lock()
	pushed := svc.runs["gone:100"].PushedFinal
	svc.mu.Unlock()
	if !pushed {
		t.Error("pushed_final should be true after push_update")
	}
}

// TestHandleEvent无状态跳过 push_update 无 state 无 job 时跳过
func TestHandleEvent无状态跳过(t *testing.T) {
	svc := NewCronSchedulerService(nil, nil, nil, nil)
	pub := &fakePublisher{}
	svc.publisher = pub

	svc.handleEvent(context.Background(), &cronEvent{
		AtTS: svc.nowFn(), Kind: eventKindPushUpdate, JobID: "ghost", RunID: "ghost:1",
	})
	if len(pub.Messages()) != 0 {
		t.Error("no state and no job should skip push_update")
	}
}

// TestHandleEvent停用任务跳过 wake/push 停用任务事件跳过
func TestHandleEvent停用任务跳过(t *testing.T) {
	svc := NewCronSchedulerService(nil, nil, nil, nil)
	agent := &fakeAgentClient{}
	svc.agentClient = agent
	pub := &fakePublisher{}
	svc.publisher = pub

	svc.mu.Lock()
	svc.jobs["dis"] = &CronJob{ID: "dis", Name: "n", Timezone: "UTC", Targets: "web", Enabled: false}
	svc.mu.Unlock()

	// wake 事件对停用任务：直接跳过（不触发 agent 调用）
	svc.handleEvent(context.Background(), &cronEvent{AtTS: svc.nowFn(), Kind: eventKindWake, JobID: "dis", RunID: "dis:1"})
	time.Sleep(50 * time.Millisecond)
	if agent.Calls() != 0 {
		t.Errorf("disabled job wake should not invoke agent, got %d calls", agent.Calls())
	}
	if len(pub.Messages()) != 0 {
		t.Error("disabled job should not push")
	}
}

// TestOnPushUpdate跳过矩阵 pushed_final/空文本跳过
func TestOnPushUpdate跳过矩阵(t *testing.T) {
	svc := NewCronSchedulerService(nil, nil, nil, nil)
	pub := &fakePublisher{}
	svc.publisher = pub
	job := &CronJob{ID: "j", Name: "n", Timezone: "UTC", Targets: "web", Enabled: false}

	// 已 pushed_final
	svc.mu.Lock()
	svc.runs["r1"] = &CronRunState{RunID: "r1", PushedFinal: true, ResultText: "x"}
	svc.mu.Unlock()
	svc.onPushUpdate(job, "r1")
	if len(pub.Messages()) != 0 {
		t.Error("pushed_final should skip")
	}
	// 空文本
	svc.mu.Lock()
	svc.runs["r2"] = &CronRunState{RunID: "r2", ResultText: ""}
	svc.mu.Unlock()
	svc.onPushUpdate(job, "r2")
	if len(pub.Messages()) != 0 {
		t.Error("empty result_text should skip")
	}
	// 无状态
	svc.onPushUpdate(job, "ghost")
	if len(pub.Messages()) != 0 {
		t.Error("no state should skip")
	}
}

// TestRunAgent失败路径 agent 调用失败时状态与补发文本
func TestRunAgent失败路径(t *testing.T) {
	svc := NewCronSchedulerService(nil, nil, nil, nil)
	agent := &fakeAgentClient{
		respond: func(call int) (*schema.AgentResponse, error) {
			return nil, context.DeadlineExceeded
		},
	}
	svc.agentClient = agent
	pub := &fakePublisher{}
	svc.publisher = pub

	job := &CronJob{ID: "j", Name: "n", Description: "d", Mode: "agent", Timezone: "UTC"}
	state := &CronRunState{RunID: "j:100", JobID: "j"}

	svc.runAgent(context.Background(), job, state)

	if state.Status != "failed" {
		t.Errorf("status = %q, want failed", state.Status)
	}
	if state.Error == "" {
		t.Error("error should be recorded")
	}
	if state.FinishedAt == 0 {
		t.Error("finished_at should be set")
	}
}

// TestRunAgent响应非OK resp.OK=false 时状态 failed
func TestRunAgent响应非OK(t *testing.T) {
	svc := NewCronSchedulerService(nil, nil, nil, nil)
	agent := &fakeAgentClient{
		respond: func(call int) (*schema.AgentResponse, error) {
			return &schema.AgentResponse{OK: false, Payload: map[string]any{"content": map[string]any{"output": "err-text"}}}, nil
		},
	}
	svc.agentClient = agent

	job := &CronJob{ID: "j", Name: "n", Description: "d", Mode: "", Timezone: "UTC"}
	state := &CronRunState{RunID: "j:100", JobID: "j"}

	svc.runAgent(context.Background(), job, state)

	if state.Status != "failed" {
		t.Errorf("status = %q, want failed", state.Status)
	}
	if state.ResultText != "err-text" {
		t.Errorf("result_text = %q, want err-text", state.ResultText)
	}
}

// TestRunAgent空文本兜底 agent 返回无文本时的兜底文案
func TestRunAgent空文本兜底(t *testing.T) {
	svc := NewCronSchedulerService(nil, nil, nil, nil)
	agent := &fakeAgentClient{
		respond: func(call int) (*schema.AgentResponse, error) {
			return &schema.AgentResponse{OK: true, Payload: map[string]any{}}, nil
		},
	}
	svc.agentClient = agent

	job := &CronJob{ID: "j", Name: "n", Description: "d", Timezone: "UTC"}
	state := &CronRunState{RunID: "j:100", JobID: "j"}

	svc.runAgent(context.Background(), job, state)

	if state.ResultText != "[cron] 任务完成，但未返回可展示文本" {
		t.Errorf("result_text = %q", state.ResultText)
	}
	if state.Status != "succeeded" {
		t.Errorf("status = %q, want succeeded", state.Status)
	}
}

// TestSchedulerStartStop 启停幂等与 ctx 取消
func TestSchedulerStartStop(t *testing.T) {
	store := NewCronJobStore(filepath.Join(t.TempDir(), "cron_jobs.json"))
	svc := NewCronSchedulerService(store, &fakeAgentClient{}, &fakePublisher{}, nil)

	if svc.IsRunning() {
		t.Error("should not be running before start")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := svc.Start(ctx); err != nil {
		t.Fatal(err)
	}
	if !svc.IsRunning() {
		t.Error("should be running after start")
	}
	// 重复 Start 幂等
	if err := svc.Start(ctx); err != nil {
		t.Fatal(err)
	}
	svc.Stop()
	if svc.IsRunning() {
		t.Error("should not be running after stop")
	}
}

// TestSchedulerTriggerRunNow 立即触发
func TestSchedulerTriggerRunNow(t *testing.T) {
	store := NewCronJobStore(filepath.Join(t.TempDir(), "cron_jobs.json"))
	job, err := store.CreateJob(CreateJobParams{
		Name: "t", CronExpr: "0 9 * * *", Timezone: "UTC",
		Description: "d", Targets: "web", Enabled: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	pub := &fakePublisher{}
	svc := NewCronSchedulerService(store, &fakeAgentClient{}, pub, nil)

	// 未启动：jobs 表为空，走 store.GetJob 路径
	runID, err := svc.TriggerRunNow(job.ID)
	if err != nil {
		t.Fatal(err)
	}
	if runID == "" || !strings.HasPrefix(runID, job.ID+":") {
		t.Errorf("runID = %q", runID)
	}
	// 不存在的任务
	if _, err := svc.TriggerRunNow("ghost"); err == nil {
		t.Error("nonexistent job should error")
	}
}

// TestSchedulerReload保留PushUpdate reload 保留飞行中 push_update 事件
func TestSchedulerReload保留PushUpdate(t *testing.T) {
	store := NewCronJobStore(filepath.Join(t.TempDir(), "cron_jobs.json"))
	svc := NewCronSchedulerService(store, &fakeAgentClient{}, &fakePublisher{}, nil)

	// 手动塞入 wake/push/push_update 三类事件
	svc.mu.Lock()
	svc.scheduleEventLocked(time.Now().Add(time.Hour), eventKindWake, "a", "a:1")
	svc.scheduleEventLocked(time.Now().Add(2*time.Hour), eventKindPush, "a", "a:1")
	svc.scheduleEventLocked(time.Now().Add(3*time.Hour), eventKindPushUpdate, "b", "b:2")
	svc.mu.Unlock()

	if err := svc.Reload(); err != nil {
		t.Fatal(err)
	}

	svc.mu.Lock()
	kinds := map[string]int{}
	for _, ev := range svc.events {
		kinds[ev.Kind]++
	}
	svc.mu.Unlock()

	if kinds[eventKindPushUpdate] != 1 {
		t.Errorf("push_update should be preserved, got %d", kinds[eventKindPushUpdate])
	}
	if kinds[eventKindWake] != 0 || kinds[eventKindPush] != 0 {
		t.Errorf("wake/push from old schedule should be cleared, got %v", kinds)
	}
}

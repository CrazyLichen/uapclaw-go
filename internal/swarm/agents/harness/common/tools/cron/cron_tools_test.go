package cronbridge

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/swarm/e2a"
	gwcron "github.com/uapclaw/uapclaw-go/internal/swarm/gateway/cron"
	"github.com/uapclaw/uapclaw-go/internal/swarm/schema"
)

// ──────────────────────────── 测试辅助 ────────────────────────────

// pushRecorder 记录 E2A 推送（对齐 Python WebSocketGatewayPushTransport mock）
type pushRecorder struct {
	payloads []map[string]any
	err      error
}

func (p *pushRecorder) push(ctx context.Context, payload map[string]any) error {
	p.payloads = append(p.payloads, payload)
	return p.err
}

// fakeAgentClientForBridge 空实现 SchedulerAgentClient（EnsureScheduler fallback 用）
type fakeAgentClientForBridge struct {
	called int
}

func (f *fakeAgentClientForBridge) SendRequest(ctx context.Context, envelope *e2a.E2AEnvelope) (*schema.AgentResponse, error) {
	f.called++
	return nil, errors.New("not implemented in fake")
}

// fakePublisherForBridge 测试 publisher
type fakePublisherForBridge struct {
	msgs []*schema.Message
}

func (f *fakePublisherForBridge) PublishRobotMessages(msg *schema.Message) {
	f.msgs = append(f.msgs, msg)
}

// newTestTools 构造指向临时目录的 CronTools
func newTestTools(t *testing.T, push func(ctx context.Context, payload map[string]any) error) *CronTools {
	t.Helper()
	tools := NewCronTools(push)
	tools.localStore = gwcron.NewCronJobStore(filepath.Join(t.TempDir(), "cron_jobs.json"))
	return tools
}

// ──────────────────────────── EnsureScheduler ────────────────────────────

// TestEnsureScheduler_分支1内嵌运行中 确保 running 内嵌调度器直接返回
func TestEnsureScheduler_分支1内嵌运行中(t *testing.T) {
	gwcron.ResetProcessSchedulerForTest()
	defer gwcron.ResetProcessSchedulerForTest()

	tools := newTestTools(t, nil)
	tools.SetSchedulerDeps(&fakeAgentClientForBridge{}, &fakePublisherForBridge{}, nil)

	// 第一次调用启动 fallback 内嵌调度器（分支 4）
	got := tools.EnsureScheduler(context.Background())
	if got == nil {
		t.Fatal("fallback should start embedded scheduler when agentClient provided")
	}
	if !got.IsRunning() {
		t.Fatal("started scheduler should be running")
	}
	// 第二次调用走分支 1（已运行 → 直接返回）
	got2 := tools.EnsureScheduler(context.Background())
	if got2 != got {
		t.Fatalf("branch 1 should return same scheduler, got %v want %v", got2, got)
	}
	got.Stop()
}

// TestEnsureScheduler_分支2已尝试启动 标记已尝试后不再重复启动
func TestEnsureScheduler_分支2已尝试启动(t *testing.T) {
	gwcron.ResetProcessSchedulerForTest()
	defer gwcron.ResetProcessSchedulerForTest()

	tools := newTestTools(t, nil)
	// 无 agentClient → 分支 4 失败路径标记 schedulerStarted
	if got := tools.EnsureScheduler(context.Background()); got != nil {
		t.Fatal("no agentClient should return nil")
	}
	if !tools.schedulerStarted {
		t.Fatal("should mark as tried")
	}
	// 分支 2：已尝试 → 即使后来注入 agentClient 也不再启动
	tools.SetSchedulerDeps(&fakeAgentClientForBridge{}, &fakePublisherForBridge{}, nil)
	if got := tools.EnsureScheduler(context.Background()); got != nil {
		t.Fatal("already-tried flag should prevent restart")
	}
}

// TestEnsureScheduler_分支3委托Gateway 注册表有运行中 Gateway 调度器时委托 Reload
func TestEnsureScheduler_分支3委托Gateway(t *testing.T) {
	gwcron.ResetProcessSchedulerForTest()
	defer gwcron.ResetProcessSchedulerForTest()

	// 注册运行中的 Gateway 调度器（无 agentClient 也可 Start）
	gwStore := gwcron.NewCronJobStore(filepath.Join(t.TempDir(), "gw.json"))
	gwSvc := gwcron.NewCronSchedulerService(gwStore, nil, nil, nil)
	if err := gwSvc.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer gwSvc.Stop()
	gwcron.SetProcessScheduler(gwSvc)

	tools := newTestTools(t, nil)
	// 即使提供 agentClient 也应委托（分支 3 优先于分支 4）
	tools.SetSchedulerDeps(&fakeAgentClientForBridge{}, &fakePublisherForBridge{}, nil)

	got := tools.EnsureScheduler(context.Background())
	if got != nil {
		t.Fatalf("delegated path should return nil, got %v", got)
	}
	if !tools.schedulerStarted {
		t.Fatal("delegated path should mark as tried")
	}
	if tools.scheduler != nil {
		t.Fatal("delegated path should not start embedded scheduler")
	}
}

// TestEnsureScheduler_分支4fallback agentClient 提供时启动内嵌调度器
func TestEnsureScheduler_分支4fallback(t *testing.T) {
	gwcron.ResetProcessSchedulerForTest()
	defer gwcron.ResetProcessSchedulerForTest()

	tools := newTestTools(t, nil)
	tools.SetSchedulerDeps(&fakeAgentClientForBridge{}, &fakePublisherForBridge{}, nil)

	got := tools.EnsureScheduler(context.Background())
	if got == nil {
		t.Fatal("fallback should start embedded scheduler")
	}
	defer got.Stop()
	if got.IsRunning() != true {
		t.Fatal("embedded scheduler should be running")
	}
	if tools.scheduler != got {
		t.Fatal("scheduler field should be set")
	}
}

// TestEnsureScheduler_分支4无AgentClient 无 agentClient 时 Warn 并标记
func TestEnsureScheduler_分支4无AgentClient(t *testing.T) {
	gwcron.ResetProcessSchedulerForTest()
	defer gwcron.ResetProcessSchedulerForTest()

	tools := newTestTools(t, nil)
	got := tools.EnsureScheduler(context.Background())
	if got != nil {
		t.Fatal("no agentClient fallback should return nil")
	}
	if !tools.schedulerStarted {
		t.Fatal("should mark as tried")
	}
}

// TestEnsureScheduler_分支3需运行中 注册但未运行的调度器不触发委托
func TestEnsureScheduler_分支3需运行中(t *testing.T) {
	gwcron.ResetProcessSchedulerForTest()
	defer gwcron.ResetProcessSchedulerForTest()

	// 注册但未 Start 的调度器
	gwStore := gwcron.NewCronJobStore(filepath.Join(t.TempDir(), "gw.json"))
	gwSvc := gwcron.NewCronSchedulerService(gwStore, nil, nil, nil)
	gwcron.SetProcessScheduler(gwSvc)

	tools := newTestTools(t, nil)
	tools.SetSchedulerDeps(&fakeAgentClientForBridge{}, &fakePublisherForBridge{}, nil)
	// 未运行 → 走分支 4 启动内嵌
	got := tools.EnsureScheduler(context.Background())
	if got == nil {
		t.Fatal("non-running registry scheduler should fall through to embedded fallback")
	}
	defer got.Stop()
}

// ──────────────────────────── 路由上下文 ────────────────────────────

// TestPushCronRoute ctx 传递路由
func TestPushCronRoute(t *testing.T) {
	ctx := PushCronRoute(context.Background(), &CronToolRoute{
		RequestID: "req-1", ChannelID: "feishu", SessionID: "sess-1", ChatType: "group",
	})
	r := route(ctx)
	if r.RequestID != "req-1" || r.ChannelID != "feishu" || r.SessionID != "sess-1" || r.ChatType != "group" {
		t.Errorf("route: %+v", r)
	}
}

// TestRoute默认值 无绑定 ctx 返回默认 web 路由
func TestRoute默认值(t *testing.T) {
	r := route(context.Background())
	if r.ChannelID != gwcron.TargetChannelWeb {
		t.Errorf("default channel = %q, want web", r.ChannelID)
	}
	if r.RequestID != "" || r.SessionID != "" || r.ChatType != "" {
		t.Errorf("default route should be zero: %+v", r)
	}
}

// ──────────────────────────── SendSplit ────────────────────────────

// TestSendSplitPayload 断言 E2A payload 结构
func TestSendSplitPayload(t *testing.T) {
	rec := &pushRecorder{}
	tools := newTestTools(t, rec.push)
	ctx := PushCronRoute(context.Background(), &CronToolRoute{
		RequestID: "req-1", ChannelID: "feishu", SessionID: "sess-1",
	})
	result, err := tools.SendSplit(ctx, "create", map[string]any{"name": "n"})
	if err != nil {
		t.Fatal(err)
	}
	if result["status"] != "forwarded" {
		t.Errorf("status = %v", result["status"])
	}
	if result["action"] != "create" {
		t.Errorf("action = %v", result["action"])
	}
	if result["data"] != nil {
		t.Errorf("data should be nil, got %v", result["data"])
	}
	if result["message"] != "cron request forwarded to gateway" {
		t.Errorf("message = %v", result["message"])
	}
	if len(rec.payloads) != 1 {
		t.Fatalf("push count = %d", len(rec.payloads))
	}
	p := rec.payloads[0]
	if p["request_id"] != "req-1" || p["channel_id"] != "feishu" || p["session_id"] != "sess-1" {
		t.Errorf("route fields: %v", p)
	}
	if p["response_kind"] != "cron" {
		t.Errorf("response_kind = %v", p["response_kind"])
	}
	body := p["body"].(map[string]any)
	if body["action"] != "create" || body["status"] != "ok" {
		t.Errorf("body: %v", body)
	}
	if body["message"] != "" {
		t.Errorf("body message should be empty: %v", body["message"])
	}
	data := body["data"].(map[string]any)
	if data["name"] != "n" {
		t.Errorf("body data: %v", data)
	}
}

// TestSendSplit无推送函数 gatewayPush 为 nil 时静默成功
func TestSendSplit无推送函数(t *testing.T) {
	tools := newTestTools(t, nil)
	result, err := tools.SendSplit(context.Background(), "list", nil)
	if err != nil {
		t.Fatal(err)
	}
	if result["status"] != "forwarded" {
		t.Errorf("nil gatewayPush should degrade silently: %v", result)
	}
}

// TestSendSplit推送失败 gatewayPush 报错时返回错误
func TestSendSplit推送失败(t *testing.T) {
	rec := &pushRecorder{err: errors.New("connection closed")}
	tools := newTestTools(t, rec.push)
	if _, err := tools.SendSplit(context.Background(), "list", nil); err == nil {
		t.Fatal("push error should propagate")
	}
}

// TestSendSplit参数浅拷贝 params 修改不影响已发送 payload
func TestSendSplit参数浅拷贝(t *testing.T) {
	rec := &pushRecorder{}
	tools := newTestTools(t, rec.push)
	params := map[string]any{"name": "n"}
	if _, err := tools.SendSplit(context.Background(), "create", params); err != nil {
		t.Fatal(err)
	}
	params["name"] = "mutated"
	data := rec.payloads[0]["body"].(map[string]any)["data"].(map[string]any)
	if data["name"] != "n" {
		t.Errorf("payload should be isolated from later mutation: %v", data)
	}
}

// ──────────────────────────── normalizeTargetsParam / defaultTargetFromChannel ────────────────────────────

// TestDefaultTargetFromChannel 前缀矩阵
func TestDefaultTargetFromChannel(t *testing.T) {
	tools := newTestTools(t, nil)
	cases := map[string]string{
		"feishu":                "feishu",
		"feishu_enterprise:app": "feishu_enterprise:app",
		"wecom":                 "wecom",
		"xiaoyi":                "xiaoyi",
		"whatsapp":              "whatsapp",
		"wechat":                "wechat",
		"dingtalk":              "dingtalk",
		"tui":                   "tui",
		"web":                   "web",
		"":                      "web",
		"unknown":               "web",
		// 大写归一化为小写（Python channel.lower() 前缀匹配）
		"FEISHU":    "feishu",
		"WECOM":     "wecom",
		"feishu123": "feishu", // 前缀匹配即命中（Python startswith 语义）
	}
	for in, want := range cases {
		ctx := PushCronRoute(context.Background(), &CronToolRoute{ChannelID: in})
		if got := tools.defaultTargetFromChannel(ctx); got != want {
			t.Errorf("defaultTarget(%q) = %q, want %q", in, got, want)
		}
	}
}

// TestNormalizeTargetsParam显式值 显式合法渠道直接归一化
func TestNormalizeTargetsParam显式值(t *testing.T) {
	tools := newTestTools(t, nil)
	ctx := PushCronRoute(context.Background(), &CronToolRoute{ChannelID: "web"})
	got, err := tools.normalizeTargetsParam(ctx, "feishu")
	if err != nil {
		t.Fatal(err)
	}
	if got != "feishu" {
		t.Errorf("explicit = %q, want feishu", got)
	}
}

// TestNormalizeTargetsParam空值回退 空值走 fallback 推导
func TestNormalizeTargetsParam空值回退(t *testing.T) {
	tools := newTestTools(t, nil)
	ctx := PushCronRoute(context.Background(), &CronToolRoute{ChannelID: "feishu"})
	got, err := tools.normalizeTargetsParam(ctx, "")
	if err != nil {
		t.Fatal(err)
	}
	if got != "feishu" {
		t.Errorf("fallback = %q, want feishu", got)
	}
}

// TestNormalizeTargetsParam非法值回退 非法值走 fallback
func TestNormalizeTargetsParam非法值回退(t *testing.T) {
	tools := newTestTools(t, nil)
	ctx := PushCronRoute(context.Background(), &CronToolRoute{ChannelID: "dingtalk"})
	got, err := tools.normalizeTargetsParam(ctx, "not-a-channel")
	if err != nil {
		t.Fatal(err)
	}
	if got != "dingtalk" {
		t.Errorf("fallback = %q, want dingtalk", got)
	}
}

// TestNormalizeTargetsParam企业飞书显式值 feishu_enterprise 前缀形态归一化
func TestNormalizeTargetsParam企业飞书显式值(t *testing.T) {
	tools := newTestTools(t, nil)
	ctx := PushCronRoute(context.Background(), &CronToolRoute{ChannelID: "web"})
	got, err := tools.normalizeTargetsParam(ctx, "feishu_enterprise:app123:chat")
	if err != nil {
		t.Fatal(err)
	}
	// 归一化截断 :chat 等后缀只保留 app_id
	if got != "feishu_enterprise:app123" {
		t.Errorf("normalized = %q, want feishu_enterprise:app123", got)
	}
}

// TestResolveChannelID从RequestID channel_id 空时从 request_id 推导
func TestResolveChannelID从RequestID(t *testing.T) {
	tools := newTestTools(t, nil)
	// rsplit(":", 1)[0] 语义：最后一个冒号前的完整前缀
	cases := map[string]string{
		"feishu:sess-1:req-123": "feishu:sess-1",
		"wecom:abc":             "wecom",
		"no-colon":              "",
	}
	for reqID, want := range cases {
		ctx := PushCronRoute(context.Background(), &CronToolRoute{RequestID: reqID})
		if got := tools.resolveChannelID(ctx); got != want {
			t.Errorf("resolveChannelID(%q) = %q, want %q", reqID, got, want)
		}
	}
}

// TestResolveChannelID优先路由值 channel_id 非空时优先
func TestResolveChannelID优先路由值(t *testing.T) {
	tools := newTestTools(t, nil)
	ctx := PushCronRoute(context.Background(), &CronToolRoute{
		ChannelID: "feishu", RequestID: "wecom:sess:123",
	})
	if got := tools.resolveChannelID(ctx); got != "feishu" {
		t.Errorf("channel_id should win, got %q", got)
	}
}

// ──────────────────────────── ListJobs / GetJob ────────────────────────────

// TestListAndGetJobs 本地 store 读写
func TestListAndGetJobs(t *testing.T) {
	tools := newTestTools(t, nil)
	created, err := tools.localStore.CreateJob(gwcron.CreateJobParams{
		Name: "n1", CronExpr: "0 9 * * *", Timezone: "Asia/Shanghai",
		Description: "d", Targets: "web", Enabled: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	jobs, err := tools.ListJobs()
	if err != nil {
		t.Fatal(err)
	}
	if len(jobs) != 1 {
		t.Fatalf("list = %d, want 1", len(jobs))
	}
	got, err := tools.GetJob(created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got["name"] != "n1" {
		t.Errorf("get name = %v", got["name"])
	}
	// 不存在 → nil
	missing, err := tools.GetJob("no-such")
	if err != nil {
		t.Fatal(err)
	}
	if missing != nil {
		t.Errorf("missing job should be nil, got %v", missing)
	}
}

// ──────────────────────────── CreateJob ────────────────────────────

// TestCreateJob本地写推送 本地写 + E2A 推送 + 路由上下文传播
func TestCreateJob本地写推送(t *testing.T) {
	rec := &pushRecorder{}
	tools := newTestTools(t, rec.push)

	ctx := PushCronRoute(context.Background(), &CronToolRoute{
		RequestID: "r", ChannelID: "web", SessionID: "s1", ChatType: "group",
	})
	d, err := tools.CreateJob(ctx, map[string]any{
		"name": "n", "cron_expr": "0 9 * * *", "timezone": "Asia/Shanghai",
		"description": "d", "targets": "web", "enabled": true,
	})
	if err != nil {
		t.Fatal(err)
	}
	// 路由 session/chat_type 传播到本地 job
	if d["session_id"] != "s1" {
		t.Errorf("session_id = %v, want s1", d["session_id"])
	}
	if d["chat_type"] != "group" {
		t.Errorf("chat_type = %v, want group", d["chat_type"])
	}
	// cron_expr 归一化（5 段 → 7 段）
	if d["cron_expr"] != "0 0 9 * * * *" {
		t.Errorf("cron_expr = %v, want 0 0 9 * * * *", d["cron_expr"])
	}
	// E2A 推送一次，action=create
	if len(rec.payloads) != 1 {
		t.Fatalf("E2A push count = %d, want 1", len(rec.payloads))
	}
	body := rec.payloads[0]["body"].(map[string]any)
	if body["action"] != "create" {
		t.Errorf("push action = %v", body["action"])
	}
	pushedData := body["data"].(map[string]any)
	if pushedData["id"] != d["id"] {
		t.Errorf("pushed job id mismatch: %v vs %v", pushedData["id"], d["id"])
	}
	// 本地可读
	got, _ := tools.GetJob(strVal(d["id"]))
	if got == nil {
		t.Fatal("local write should persist")
	}
}

// TestCreateJob会话剥离入参 session_id 入参被剥离，改用路由值
func TestCreateJob会话剥离入参(t *testing.T) {
	rec := &pushRecorder{}
	tools := newTestTools(t, rec.push)

	ctx := PushCronRoute(context.Background(), &CronToolRoute{
		ChannelID: "web", SessionID: "route-sess",
	})
	d, err := tools.CreateJob(ctx, map[string]any{
		"name": "n", "cron_expr": "0 9 * * *", "timezone": "Asia/Shanghai",
		"description": "d", "targets": "web",
		"session_id": "param-sess", // 应被剥离
	})
	if err != nil {
		t.Fatal(err)
	}
	if d["session_id"] != "route-sess" {
		t.Errorf("session_id = %v, want route-sess（param session_id 应剥离）", d["session_id"])
	}
}

// TestCreateJob推送失败不阻断 E2A 推送失败仅 Warn，本地写已生效
func TestCreateJob推送失败不阻断(t *testing.T) {
	rec := &pushRecorder{err: errors.New("gateway down")}
	tools := newTestTools(t, rec.push)

	d, err := tools.CreateJob(context.Background(), map[string]any{
		"name": "n", "cron_expr": "0 9 * * *", "timezone": "Asia/Shanghai",
		"description": "d", "targets": "web",
	})
	if err != nil {
		t.Fatalf("push failure should not fail create: %v", err)
	}
	// 本地仍可读
	got, _ := tools.GetJob(strVal(d["id"]))
	if got == nil {
		t.Fatal("local write should persist despite push failure")
	}
}

// TestCreateJob非法表达式 非法 cron 表达式报错
func TestCreateJob非法表达式(t *testing.T) {
	tools := newTestTools(t, nil)
	_, err := tools.CreateJob(context.Background(), map[string]any{
		"name": "n", "cron_expr": "not-a-cron", "timezone": "Asia/Shanghai",
		"description": "d", "targets": "web",
	})
	if err == nil {
		t.Fatal("invalid cron expr should error")
	}
}

// TestCreateJob委托GatewayReload Gateway 调度器运行中时 create 触发委托 reload
func TestCreateJob委托GatewayReload(t *testing.T) {
	gwcron.ResetProcessSchedulerForTest()
	defer gwcron.ResetProcessSchedulerForTest()

	gwStore := gwcron.NewCronJobStore(filepath.Join(t.TempDir(), "gw.json"))
	gwSvc := gwcron.NewCronSchedulerService(gwStore, nil, nil, nil)
	if err := gwSvc.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer gwSvc.Stop()
	gwcron.SetProcessScheduler(gwSvc)

	// CronTools 用同一 store 文件（模拟 Gateway/Agent 共路径）
	tools := newTestTools(t, nil)
	tools.localStore = gwStore

	d, err := tools.CreateJob(context.Background(), map[string]any{
		"name": "n", "cron_expr": "0 9 * * *", "timezone": "Asia/Shanghai",
		"description": "d", "targets": "web",
	})
	if err != nil {
		t.Fatal(err)
	}
	if d == nil {
		t.Fatal("unreachable")
	}
	// 委托 reload 后 Gateway 调度器应看到新任务：jobs map 重建（Reload 保留 push_update
	// 清空 wake/push），行为级验证——共路径 store 再 ListJobs 可见即 reload 已执行
	got, err := gwStore.GetJob(d["id"].(string))
	if err != nil || got == nil {
		t.Fatalf("gateway store should see job after delegated reload, err=%v", err)
	}
	if !got.Enabled {
		t.Fatal("job should be enabled")
	}
}

// ──────────────────────────── UpdateJob ────────────────────────────

// TestUpdateJob基本字段 本地更新 + E2A 推送
func TestUpdateJob基本字段(t *testing.T) {
	rec := &pushRecorder{}
	tools := newTestTools(t, rec.push)
	created, err := tools.localStore.CreateJob(gwcron.CreateJobParams{
		Name: "n", CronExpr: "0 9 * * *", Timezone: "Asia/Shanghai",
		Description: "d", Targets: "web", Enabled: true,
	})
	if err != nil {
		t.Fatal(err)
	}

	ctx := PushCronRoute(context.Background(), &CronToolRoute{ChannelID: "web"})
	updated, err := tools.UpdateJob(ctx, created.ID, map[string]any{
		"cron_expr": "30 8 * * *", "description": "new-d",
	})
	if err != nil {
		t.Fatal(err)
	}
	if updated["cron_expr"] != "0 30 8 * * * *" {
		t.Errorf("cron_expr = %v, want 0 30 8 * * * *", updated["cron_expr"])
	}
	if updated["description"] != "new-d" {
		t.Errorf("description = %v", updated["description"])
	}
	// E2A 推送 update
	if len(rec.payloads) != 1 {
		t.Fatalf("push count = %d, want 1", len(rec.payloads))
	}
	body := rec.payloads[0]["body"].(map[string]any)
	if body["action"] != "update" {
		t.Errorf("action = %v", body["action"])
	}
	data := body["data"].(map[string]any)
	if data["job_id"] != created.ID {
		t.Errorf("push job_id = %v", data["job_id"])
	}
	patch := data["patch"].(map[string]any)
	if patch["description"] != "new-d" {
		t.Errorf("push patch: %v", patch)
	}
}

// TestUpdateJob企业飞书Session注入 targets 为 feishu_enterprise 时注入路由 session_id
func TestUpdateJob企业飞书Session注入(t *testing.T) {
	rec := &pushRecorder{}
	tools := newTestTools(t, rec.push)
	created, err := tools.localStore.CreateJob(gwcron.CreateJobParams{
		Name: "n", CronExpr: "0 9 * * *", Timezone: "Asia/Shanghai",
		Description: "d", Targets: "web", Enabled: true,
	})
	if err != nil {
		t.Fatal(err)
	}

	ctx := PushCronRoute(context.Background(), &CronToolRoute{
		ChannelID: "feishu_enterprise:app1", SessionID: "feishu::chat1::bot1::open1::x::y",
	})
	updated, err := tools.UpdateJob(ctx, created.ID, map[string]any{
		"targets": "feishu_enterprise:app1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if updated["session_id"] != "feishu::chat1::bot1::open1::x::y" {
		t.Errorf("session_id = %v, want SessionMap 注入", updated["session_id"])
	}
}

// TestUpdateJob普通渠道Session清除 targets 非 feishu_enterprise 时 session_id 置 nil
func TestUpdateJob普通渠道Session清除(t *testing.T) {
	tools := newTestTools(t, nil)
	created, err := tools.localStore.CreateJob(gwcron.CreateJobParams{
		Name: "n", CronExpr: "0 9 * * *", Timezone: "Asia/Shanghai",
		Description: "d", Targets: "feishu_enterprise:app1", Enabled: true,
		SessionID: "feishu::chat1::bot1::open1::x::y",
	})
	if err != nil {
		t.Fatal(err)
	}

	ctx := PushCronRoute(context.Background(), &CronToolRoute{ChannelID: "web"})
	updated, err := tools.UpdateJob(ctx, created.ID, map[string]any{
		"targets": "web",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, has := updated["session_id"]; has {
		t.Errorf("session_id should be cleared, got %v", updated["session_id"])
	}
}

// TestUpdateJob不存在 任务不存在时报错
func TestUpdateJob不存在(t *testing.T) {
	tools := newTestTools(t, nil)
	if _, err := tools.UpdateJob(context.Background(), "no-such", map[string]any{"name": "x"}); err == nil {
		t.Fatal("update missing job should error")
	}
}

// ──────────────────────────── DeleteJob / ToggleJob ────────────────────────────

// TestDeleteJob本地删除推送 删除 + E2A 转发
func TestDeleteJob本地删除推送(t *testing.T) {
	rec := &pushRecorder{}
	tools := newTestTools(t, rec.push)
	created, err := tools.localStore.CreateJob(gwcron.CreateJobParams{
		Name: "n", CronExpr: "0 9 * * *", Timezone: "Asia/Shanghai",
		Description: "d", Targets: "web", Enabled: true,
	})
	if err != nil {
		t.Fatal(err)
	}

	deleted, err := tools.DeleteJob(context.Background(), created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !deleted {
		t.Error("should report deleted")
	}
	if len(rec.payloads) != 1 {
		t.Fatalf("push count = %d", len(rec.payloads))
	}
	body := rec.payloads[0]["body"].(map[string]any)
	if body["action"] != "delete" {
		t.Errorf("action = %v", body["action"])
	}
	// 本地已删
	got, _ := tools.GetJob(created.ID)
	if got != nil {
		t.Error("job should be gone locally")
	}
}

// TestToggleJob本地启停推送 启停 + E2A 转发
func TestToggleJob本地启停推送(t *testing.T) {
	rec := &pushRecorder{}
	tools := newTestTools(t, rec.push)
	created, err := tools.localStore.CreateJob(gwcron.CreateJobParams{
		Name: "n", CronExpr: "0 9 * * *", Timezone: "Asia/Shanghai",
		Description: "d", Targets: "web", Enabled: true,
	})
	if err != nil {
		t.Fatal(err)
	}

	toggled, err := tools.ToggleJob(context.Background(), created.ID, false)
	if err != nil {
		t.Fatal(err)
	}
	if toggled["enabled"] != false {
		t.Errorf("enabled = %v, want false", toggled["enabled"])
	}
	if len(rec.payloads) != 1 {
		t.Fatalf("push count = %d", len(rec.payloads))
	}
	body := rec.payloads[0]["body"].(map[string]any)
	if body["action"] != "toggle" {
		t.Errorf("action = %v", body["action"])
	}
	data := body["data"].(map[string]any)
	if data["enabled"] != false {
		t.Errorf("push enabled = %v", data["enabled"])
	}
}

// ──────────────────────────── PreviewJob ────────────────────────────

// TestPreviewJob本地计算 本地计算下次执行（不转发 Gateway）
func TestPreviewJob本地计算(t *testing.T) {
	rec := &pushRecorder{}
	tools := newTestTools(t, rec.push)
	created, err := tools.localStore.CreateJob(gwcron.CreateJobParams{
		Name: "n", CronExpr: "0 9 * * *", Timezone: "Asia/Shanghai",
		Description: "d", Targets: "web", Enabled: true,
	})
	if err != nil {
		t.Fatal(err)
	}

	rows, err := tools.PreviewJob(created.ID, 3)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 3 {
		t.Fatalf("preview rows = %d, want 3", len(rows))
	}
	for _, row := range rows {
		pushAt := strVal(row["push_at"])
		if !strings.Contains(pushAt, "T09:00:00") {
			t.Errorf("push_at should be 09:00 daily: %v", pushAt)
		}
		if _, has := row["wake_at"]; !has {
			t.Errorf("row missing wake_at: %v", row)
		}
	}
	// wake_at = push_at - 300s（默认 wake_offset）
	firstWake := strVal(rows[0]["wake_at"])
	firstPush := strVal(rows[0]["push_at"])
	if firstWake >= firstPush {
		t.Errorf("wake_at (%s) should be before push_at (%s)", firstWake, firstPush)
	}
	// 预览不推送
	if len(rec.payloads) != 0 {
		t.Errorf("preview should not push, got %d", len(rec.payloads))
	}
}

// TestPreviewJob不存在 任务不存在时报错
func TestPreviewJob不存在(t *testing.T) {
	tools := newTestTools(t, nil)
	if _, err := tools.PreviewJob("no-such", 5); err == nil {
		t.Fatal("preview missing job should error")
	}
}

// TestPreviewJob计数钳制 count 边界钳制 [1,50]
func TestPreviewJob计数钳制(t *testing.T) {
	tools := newTestTools(t, nil)
	created, err := tools.localStore.CreateJob(gwcron.CreateJobParams{
		Name: "n", CronExpr: "0 9 * * *", Timezone: "Asia/Shanghai",
		Description: "d", Targets: "web", Enabled: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	rows, err := tools.PreviewJob(created.ID, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 {
		t.Errorf("count=0 should clamp to 1, got %d", len(rows))
	}
}

// ──────────────────────────── RunNow ────────────────────────────

// TestRunNow纯转发 run_now 纯转发 Gateway，本地不执行
func TestRunNow纯转发(t *testing.T) {
	rec := &pushRecorder{}
	tools := newTestTools(t, rec.push)
	ctx := PushCronRoute(context.Background(), &CronToolRoute{RequestID: "r1", ChannelID: "web"})

	result, err := tools.RunNow(ctx, "job-1")
	if err != nil {
		t.Fatal(err)
	}
	if result["status"] != "forwarded" {
		t.Errorf("status = %v, want forwarded", result["status"])
	}
	if len(rec.payloads) != 1 {
		t.Fatalf("push count = %d, want 1", len(rec.payloads))
	}
	body := rec.payloads[0]["body"].(map[string]any)
	if body["action"] != "run_now" {
		t.Errorf("action = %v, want run_now", body["action"])
	}
	data := body["data"].(map[string]any)
	if data["job_id"] != "job-1" {
		t.Errorf("job_id = %v", data["job_id"])
	}
}

// ──────────────────────────── 辅助函数 ────────────────────────────

// TestCopyMap 浅拷贝语义
func TestCopyMap(t *testing.T) {
	src := map[string]any{"a": "x", "n": 1}
	dst := copyMap(src)
	if len(dst) != 2 || dst["a"] != "x" {
		t.Fatalf("copy: %v", dst)
	}
	// nil map → 空 map
	if got := copyMap(nil); len(got) != 0 {
		t.Errorf("nil copy should be empty map, got %v", got)
	}
	// 修改副本不影响原
	dst["a"] = "y"
	if src["a"] != "x" {
		t.Error("copy should isolate from source")
	}
}

// TestStrVal 安全取字符串
func TestStrVal(t *testing.T) {
	if strVal("abc") != "abc" {
		t.Error("string passthrough")
	}
	if strVal(nil) != "" {
		t.Error("nil → empty")
	}
	if strVal(42) != "" {
		t.Error("non-string → empty")
	}
}

// TestBoolVal 安全取布尔
func TestBoolVal(t *testing.T) {
	if !boolVal(true, false) {
		t.Error("bool true")
	}
	if !boolVal(nil, true) {
		t.Error("nil → default（def=true）")
	}
	if boolVal(nil, false) {
		t.Error("nil → default（def=false）")
	}
	if boolVal("x", false) {
		t.Error("non-bool → default")
	}
}

// TestIntPtrVal 安全取 int 指针
func TestIntPtrVal(t *testing.T) {
	if intPtrVal(nil) != nil {
		t.Error("nil → nil")
	}
	n := intPtrVal(300)
	if n == nil || *n != 300 {
		t.Error("int 300")
	}
	f := intPtrVal(300.0)
	if f == nil || *f != 300 {
		t.Error("float 300")
	}
	if intPtrVal("x") != nil {
		t.Error("non-numeric → nil")
	}
}

// TestBoolPtrVal 安全取 bool 指针
func TestBoolPtrVal(t *testing.T) {
	if boolPtrVal(nil) != nil {
		t.Error("nil → nil")
	}
	b := boolPtrVal(true)
	if b == nil || !*b {
		t.Error("bool true")
	}
	if boolPtrVal("x") != nil {
		t.Error("non-bool → nil")
	}
}

// TestTimezoneOr timezone 兜底
func TestTimezoneOr(t *testing.T) {
	if timezoneOr("UTC", "Asia/Shanghai") != "UTC" {
		t.Error("explicit value")
	}
	if timezoneOr("", "Asia/Shanghai") != "Asia/Shanghai" {
		t.Error("empty → default")
	}
	if timezoneOr("  ", "Asia/Shanghai") != "Asia/Shanghai" {
		t.Error("whitespace → default")
	}
	if timezoneOr(nil, "Asia/Shanghai") != "Asia/Shanghai" {
		t.Error("nil → default")
	}
}

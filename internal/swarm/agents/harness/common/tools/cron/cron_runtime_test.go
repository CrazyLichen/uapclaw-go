package cronbridge

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	agentcron "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/tools/cron"
	gwcron "github.com/uapclaw/uapclaw-go/internal/swarm/gateway/cron"
	"github.com/uapclaw/uapclaw-go/internal/swarm/schema"
)

// ──────────────────────────── 测试辅助 ────────────────────────────

// fakeUserPublisher 测试 wake 发布器
type fakeUserPublisher struct {
	msgs []*schema.Message
}

func (f *fakeUserPublisher) PublishUserMessagesNowait(msg *schema.Message) {
	f.msgs = append(f.msgs, msg)
}

// newBackendWithTmp 构造独立 store 的 backend
func newBackendWithTmp(t *testing.T) *cronToolsBackend {
	t.Helper()
	gwcron.ResetProcessSchedulerForTest()
	t.Cleanup(gwcron.ResetProcessSchedulerForTest)
	tools := newTestTools(t, nil)
	return &cronToolsBackend{tools: tools}
}

// ──────────────────────────── toBackendJob 测试 ────────────────────────────

// TestToBackendJob默认字段 五组默认注入
func TestToBackendJob默认字段(t *testing.T) {
	row := map[string]any{"id": "j", "cron_expr": "0 0 9 * * * *", "timezone": "Asia/Shanghai",
		"description": "d", "targets": "web"}
	out := toBackendJob(row)
	schedule := out["schedule"].(map[string]any)
	if schedule["kind"] != "cron" || schedule["expr"] != "0 0 9 * * * *" || schedule["tz"] != "Asia/Shanghai" {
		t.Errorf("schedule: %v", schedule)
	}
	payload := out["payload"].(map[string]any)
	if payload["kind"] != "agentTurn" || payload["message"] != "d" {
		t.Errorf("payload: %v", payload)
	}
	delivery := out["delivery"].(map[string]any)
	if delivery["mode"] != "announce" || delivery["channel"] != "web" {
		t.Errorf("delivery: %v", delivery)
	}
	if out["session_target"] != "isolated" || out["compat_mode"] != "legacy" {
		t.Errorf("defaults: session_target=%v compat_mode=%v", out["session_target"], out["compat_mode"])
	}
}

// TestToBackendJob已有嵌套不覆盖 setdefault 语义
func TestToBackendJob已有嵌套不覆盖(t *testing.T) {
	row := map[string]any{
		"cron_expr": "0 0 9 * * * *", "timezone": "UTC", "description": "d", "targets": "feishu",
		"schedule":       map[string]any{"custom": true},
		"payload":        map[string]any{"custom": true},
		"delivery":       map[string]any{"custom": true},
		"session_target": "shared", "compat_mode": "v2",
	}
	out := toBackendJob(row)
	if _, ok := out["schedule"].(map[string]any); !ok || !out["schedule"].(map[string]any)["custom"].(bool) {
		t.Error("existing schedule should not be overwritten")
	}
	if out["session_target"] != "shared" || out["compat_mode"] != "v2" {
		t.Error("existing defaults should be kept")
	}
}

// TestToBackendJob空targets兜底 空目标兜底 web
func TestToBackendJob空targets兜底(t *testing.T) {
	row := map[string]any{"cron_expr": "0 0 9 * * * *", "timezone": "UTC"}
	out := toBackendJob(row)
	delivery := out["delivery"].(map[string]any)
	if delivery["channel"] != "web" {
		t.Errorf("empty targets should default web, got %v", delivery["channel"])
	}
}

// ──────────────────────────── extractLegacyParams 测试 ────────────────────────────

// TestExtractLegacyParams嵌套全量 嵌套结构全字段映射
func TestExtractLegacyParams嵌套全量(t *testing.T) {
	data := map[string]any{
		"schedule": map[string]any{"kind": "cron", "expr": "0 0 9 * * * *", "tz": "Asia/Shanghai"},
		"payload":  map[string]any{"kind": "agentTurn", "message": "task"},
		"delivery": map[string]any{"mode": "announce", "channel": "web"},
		"name":     "n", "id": "j1",
	}
	out, err := extractLegacyParams(data, nil, true)
	if err != nil {
		t.Fatal(err)
	}
	if out["cron_expr"] != "0 0 9 * * * *" || out["timezone"] != "Asia/Shanghai" {
		t.Errorf("schedule mapping: %v", out)
	}
	if out["description"] != "task" || out["targets"] != "web" {
		t.Errorf("payload/delivery mapping: %v", out)
	}
	if out["mode"] != "agent.fast" {
		t.Errorf("default mode = %v, want agent.fast", out["mode"])
	}
	if out["name"] != "n" || out["id"] != "j1" {
		t.Errorf("name/id mapping: %v", out)
	}
}

// TestExtractLegacyParams无嵌套原样 无嵌套字段直接返回
func TestExtractLegacyParams无嵌套原样(t *testing.T) {
	data := map[string]any{"name": "n", "cron_expr": "0 0 9 * * * *"}
	out, err := extractLegacyParams(data, nil, true)
	if err != nil {
		t.Fatal(err)
	}
	if out["name"] != "n" {
		t.Errorf("flat params should pass through: %v", out)
	}
}

// TestExtractLegacyParams非法Kind schedule.kind 非 cron 报错
func TestExtractLegacyParams非法Kind(t *testing.T) {
	data := map[string]any{"schedule": map[string]any{"kind": "interval"}}
	if _, err := extractLegacyParams(data, nil, true); err == nil {
		t.Error("non-cron kind should error")
	}
}

// TestExtractLegacyParams非法PayloadKind payload.kind 非 agentTurn 报错
func TestExtractLegacyParams非法PayloadKind(t *testing.T) {
	data := map[string]any{"payload": map[string]any{"kind": "webhook"}}
	if _, err := extractLegacyParams(data, nil, true); err == nil {
		t.Error("non-agentTurn payload should error")
	}
}

// TestExtractLegacyParams隐式web映射 隐式 web target 映射 context channel
func TestExtractLegacyParams隐式web映射(t *testing.T) {
	data := map[string]any{
		"schedule": map[string]any{"expr": "0 0 9 * * * *"},
		"delivery": map[string]any{"channel": "web"},
	}
	cronCtx := &agentcron.CronToolContext{ChannelID: "feishu", SessionID: "s"}
	out, err := extractLegacyParams(data, cronCtx, true)
	if err != nil {
		t.Fatal(err)
	}
	if out["targets"] != "feishu" {
		t.Errorf("implicit web should map to context channel, got %v", out["targets"])
	}
	if out["session_id"] != "s" {
		t.Errorf("context session should inject: %v", out["session_id"])
	}
}

// TestExtractLegacyParams显式targets存在 显式 targets 存在时禁用隐式替换
// （对齐 Python：delivery.channel 优先于 data.targets，但 hasExplicitTargets
// 阻断「隐式 web → context」替换，targets 保持 delivery.channel=web）
func TestExtractLegacyParams显式targets存在(t *testing.T) {
	data := map[string]any{
		"schedule": map[string]any{"expr": "0 0 9 * * * *"},
		"delivery": map[string]any{"channel": "web"},
		"targets":  "dingtalk",
	}
	cronCtx := &agentcron.CronToolContext{ChannelID: "feishu"}
	out, err := extractLegacyParams(data, cronCtx, true)
	if err != nil {
		t.Fatal(err)
	}
	if out["targets"] != "web" {
		t.Errorf("delivery.channel should win and no implicit swap, got %v", out["targets"])
	}
}

// TestExtractLegacyParamsMode三级解析 mode 解析链
func TestExtractLegacyParamsMode三级解析(t *testing.T) {
	// context.mode 优先
	out, _ := extractLegacyParams(map[string]any{
		"schedule": map[string]any{"expr": "0 0 9 * * * *"},
	}, &agentcron.CronToolContext{Mode: "agent.plan"}, true)
	if out["mode"] != "agent.plan" {
		t.Errorf("context mode should win, got %v", out["mode"])
	}
	// data.mode 次之
	out, _ = extractLegacyParams(map[string]any{
		"schedule": map[string]any{"expr": "0 0 9 * * * *"},
		"mode":     "agent.fast",
	}, nil, true)
	if out["mode"] != "agent.fast" {
		t.Errorf("data mode should be used, got %v", out["mode"])
	}
	// 兜底 agent.fast
	out, _ = extractLegacyParams(map[string]any{
		"schedule": map[string]any{"expr": "0 0 9 * * * *"},
	}, nil, true)
	if out["mode"] != "agent.fast" {
		t.Errorf("default mode = %v", out["mode"])
	}
}

// TestExtractLegacyParamsdeleteAfterRun 驼峰映射
func TestExtractLegacyParamsdeleteAfterRun(t *testing.T) {
	out, _ := extractLegacyParams(map[string]any{
		"schedule":       map[string]any{"expr": "0 0 9 * * * *"},
		"deleteAfterRun": true,
		"enabled":        false,
	}, nil, true)
	if out["delete_after_run"] != true {
		t.Errorf("delete_after_run = %v", out["delete_after_run"])
	}
	if out["enabled"] != false {
		t.Errorf("enabled = %v", out["enabled"])
	}
}

// TestExtractLegacyParams企业飞书上下文 feishu_enterprise 上下文归一
func TestExtractLegacyParams企业飞书上下文(t *testing.T) {
	data := map[string]any{
		"schedule": map[string]any{"expr": "0 0 9 * * * *"},
		"delivery": map[string]any{"channel": "web"},
	}
	cronCtx := &agentcron.CronToolContext{ChannelID: "feishu_enterprise:cli_9"}
	out, err := extractLegacyParams(data, cronCtx, true)
	if err != nil {
		t.Fatal(err)
	}
	if out["targets"] != "feishu_enterprise:cli_9" {
		t.Errorf("enterprise context should map, got %v", out["targets"])
	}
}

// ──────────────────────────── routeFromContext 测试 ────────────────────────────

// TestRouteFromContext 上下文提取矩阵
func TestRouteFromContext(t *testing.T) {
	// nil 上下文 → 缺省 web
	r := routeFromContext(nil)
	if r.ChannelID != "web" || r.RequestID != "" {
		t.Errorf("nil ctx route = %+v", r)
	}
	// 完整上下文
	cronCtx := &agentcron.CronToolContext{
		ChannelID: "feishu", SessionID: "sess-1",
		Metadata: map[string]any{"request_id": "req-9", "chat_type": "group"},
	}
	r = routeFromContext(cronCtx)
	if r.RequestID != "req-9" || r.ChannelID != "feishu" || r.SessionID != "sess-1" || r.ChatType != "group" {
		t.Errorf("route = %+v", r)
	}
	// 空 channel 兜底 web
	r = routeFromContext(&agentcron.CronToolContext{})
	if r.ChannelID != "web" {
		t.Errorf("empty channel should default web, got %q", r.ChannelID)
	}
}

// ──────────────────────────── Wake 测试 ────────────────────────────

// TestWake校验 三重前置校验
func TestWake校验(t *testing.T) {
	backend := newBackendWithTmp(t)
	if _, err := backend.Wake(context.Background(), "  ", nil, ""); err == nil {
		t.Error("empty text should error")
	}
	if _, err := backend.Wake(context.Background(), "hi", nil, ""); err == nil {
		t.Error("nil context should error")
	}
	ctx := &agentcron.CronToolContext{ChannelID: "web", SessionID: "s"}
	if _, err := backend.Wake(context.Background(), "hi", ctx, ""); err == nil {
		t.Error("nil publisher should error")
	}
	// 空 channel 上下文也报错
	backend2 := &cronToolsBackend{tools: newTestTools(t, nil), userPublisher: &fakeUserPublisher{}}
	if _, err := backend2.Wake(context.Background(), "hi", &agentcron.CronToolContext{}, ""); err == nil {
		t.Error("empty channel context should error")
	}
}

// TestWake正常路径 消息构造与发布
func TestWake正常路径(t *testing.T) {
	pub := &fakeUserPublisher{}
	backend := &cronToolsBackend{tools: newTestTools(t, nil), userPublisher: pub}
	cronCtx := &agentcron.CronToolContext{ChannelID: "feishu", SessionID: "s", Mode: "agent.plan"}
	result, err := backend.Wake(context.Background(), "hello", cronCtx, "")
	if err != nil {
		t.Fatal(err)
	}
	if result["queued"] != true {
		t.Error("should return queued=true")
	}
	if len(pub.msgs) != 1 {
		t.Fatal("publisher should receive 1 message")
	}
	msg := pub.msgs[0]
	if msg.ChannelID != "feishu" || msg.SessionID != "s" {
		t.Errorf("message routing: %v", msg)
	}
	if !strings.HasPrefix(msg.ID, "cron-wake-") {
		t.Errorf("message ID prefix: %q", msg.ID)
	}
	// params 含 query/content/mode
	var params map[string]any
	if err := json.Unmarshal(msg.Params, &params); err != nil {
		t.Fatal(err)
	}
	if params["query"] != "hello" || params["mode"] != "agent.plan" {
		t.Errorf("params: %v", params)
	}
}

// TestWakeMode参数 显式 mode 覆盖 context mode
func TestWakeMode参数(t *testing.T) {
	pub := &fakeUserPublisher{}
	backend := &cronToolsBackend{tools: newTestTools(t, nil), userPublisher: pub}
	cronCtx := &agentcron.CronToolContext{ChannelID: "web", SessionID: "s", Mode: "agent.plan"}
	if _, err := backend.Wake(context.Background(), "hi", cronCtx, "agent.deep"); err != nil {
		t.Fatal(err)
	}
	var params map[string]any
	if err := json.Unmarshal(pub.msgs[0].Params, &params); err != nil {
		t.Fatal(err)
	}
	if params["mode"] != "agent.deep" {
		t.Errorf("explicit mode should win, got %v", params["mode"])
	}
}

// ──────────────────────────── backend CRUD 测试 ────────────────────────────

// TestBackendListJobs含停用 includeDisabled 过滤
func TestBackendListJobs含停用(t *testing.T) {
	backend := newBackendWithTmp(t)
	ctx := context.Background()
	// 建两个任务，一个停用
	d1, err := backend.CreateJob(ctx, map[string]any{
		"name": "a", "cron_expr": "0 9 * * *", "timezone": "UTC",
		"description": "d", "targets": "web", "enabled": true,
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	d2, err := backend.CreateJob(ctx, map[string]any{
		"name": "b", "cron_expr": "0 8 * * *", "timezone": "UTC",
		"description": "d", "targets": "web", "enabled": false,
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	// includeDisabled=true 全量
	rows, err := backend.ListJobs(ctx, true)
	if err != nil || len(rows) != 2 {
		t.Fatalf("all rows = %d, err=%v", len(rows), err)
	}
	// includeDisabled=false 只留启用
	rows, err = backend.ListJobs(ctx, false)
	if err != nil || len(rows) != 1 {
		t.Fatalf("enabled rows = %d, err=%v", len(rows), err)
	}
	if rows[0]["id"] != d1["id"] {
		t.Errorf("remaining job = %v, want %v", rows[0]["id"], d1["id"])
	}
	_ = d2
}

// TestBackendGetJob 单查与后端形态
func TestBackendGetJob(t *testing.T) {
	backend := newBackendWithTmp(t)
	ctx := context.Background()
	d, err := backend.CreateJob(ctx, map[string]any{
		"name": "n", "cron_expr": "0 9 * * *", "timezone": "UTC",
		"description": "d", "targets": "web",
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	jobID, _ := d["id"].(string)
	got, err := backend.GetJob(ctx, jobID)
	if err != nil || got == nil {
		t.Fatalf("get = %v, err=%v", got, err)
	}
	// 后端形态：schedule 嵌套已注入
	if _, ok := got["schedule"].(map[string]any); !ok {
		t.Error("backend job should have schedule block")
	}
	missing, err := backend.GetJob(ctx, "ghost")
	if err != nil || missing != nil {
		t.Errorf("missing = %v, err=%v", missing, err)
	}
}

// TestBackendUpdateJob legacy patch 映射
func TestBackendUpdateJob(t *testing.T) {
	backend := newBackendWithTmp(t)
	ctx := context.Background()
	d, err := backend.CreateJob(ctx, map[string]any{
		"name": "n", "cron_expr": "0 9 * * *", "timezone": "UTC",
		"description": "d", "targets": "web",
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	jobID, _ := d["id"].(string)

	// 嵌套 schedule patch → cron_expr 扁平映射
	d2, err := backend.UpdateJob(ctx, jobID, map[string]any{
		"schedule": map[string]any{"kind": "cron", "expr": "30 8 * * *", "tz": "Asia/Shanghai"},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if d2["cron_expr"] != "0 30 8 * * * *" {
		t.Errorf("updated cron_expr = %v", d2["cron_expr"])
	}
	if d2["timezone"] != "Asia/Shanghai" {
		t.Errorf("updated timezone = %v", d2["timezone"])
	}
}

// TestBackendDeleteToggle 删除与启停
func TestBackendDeleteToggle(t *testing.T) {
	backend := newBackendWithTmp(t)
	ctx := context.Background()
	d, err := backend.CreateJob(ctx, map[string]any{
		"name": "n", "cron_expr": "0 9 * * *", "timezone": "UTC",
		"description": "d", "targets": "web",
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	jobID, _ := d["id"].(string)

	d2, err := backend.ToggleJob(ctx, jobID, false)
	if err != nil || d2["enabled"] != false {
		t.Fatalf("toggle = %v, err=%v", d2, err)
	}
	deleted, err := backend.DeleteJob(ctx, jobID)
	if err != nil || !deleted {
		t.Fatalf("delete = %v, err=%v", deleted, err)
	}
}

// TestBackendPreviewRunNowStatusGetRuns 其余四方法
func TestBackendPreviewRunNowStatusGetRuns(t *testing.T) {
	backend := newBackendWithTmp(t)
	ctx := context.Background()
	d, err := backend.CreateJob(ctx, map[string]any{
		"name": "n", "cron_expr": "0 9 * * *", "timezone": "UTC",
		"description": "d", "targets": "web",
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	jobID, _ := d["id"].(string)

	rows, err := backend.PreviewJob(ctx, jobID, 2)
	if err != nil || len(rows) != 2 {
		t.Fatalf("preview = %d, err=%v", len(rows), err)
	}

	// RunNow 走纯转发路径（无 gatewayPush 时返回 forwarded 结构 → run_id 空）
	_, err = backend.RunNow(ctx, jobID)
	if err != nil {
		t.Fatalf("run_now = %v", err)
	}

	st, err := backend.Status(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if st["job_count"] != 1 || st["running"] != false {
		t.Errorf("status = %v", st)
	}

	runs, err := backend.GetRuns(ctx, jobID, 10)
	if err != nil || len(runs) != 0 {
		t.Errorf("runs = %v, err=%v", runs, err)
	}
}

// ──────────────────────────── Bridge 测试 ────────────────────────────

// TestBridgeSetBackend 显式覆盖优先
func TestBridgeSetBackend(t *testing.T) {
	bridge := NewCronRuntimeBridge()
	if bridge.GetBackend() == nil {
		t.Fatal("default backend should resolve")
	}
	// 覆盖后优先返回
	override := newBackendWithTmp(t)
	bridge.SetBackend(override)
	if bridge.GetBackend() != agentcron.CronToolBackend(override) {
		t.Error("override backend should be returned")
	}
}

// TestBridgeEnsureSchedulerStarted 无崩溃
func TestBridgeEnsureSchedulerStarted(t *testing.T) {
	bridge := NewCronRuntimeBridge()
	bridge.EnsureSchedulerStarted() // 不应 panic
}

// TestBridgeBuildTools 工具构建
func TestBridgeBuildTools(t *testing.T) {
	bridge := NewCronRuntimeBridge()
	tools := bridge.BuildTools(&agentcron.CronToolContext{ChannelID: "web", SessionID: "s"}, "agent-1", "cn")
	if len(tools) == 0 {
		t.Fatal("cron tools should be built")
	}
}

// TestGlobal注入 全局注入传递到解析的 backend
func TestGlobal注入(t *testing.T) {
	globalMu.Lock()
	oldPush, oldPub, oldCfg, oldAgent := globalGatewayPushFunc, globalUserPublisher, globalConfigLoader, globalAgentClient
	globalGatewayPushFunc, globalUserPublisher, globalConfigLoader, globalAgentClient = nil, nil, nil, nil
	globalMu.Unlock()
	defer func() {
		globalMu.Lock()
		globalGatewayPushFunc, globalUserPublisher, globalConfigLoader, globalAgentClient = oldPush, oldPub, oldCfg, oldAgent
		globalMu.Unlock()
	}()

	pub := &fakeUserPublisher{}
	SetGlobalUserPublisher(pub)
	bridge := NewCronRuntimeBridge()
	backend := bridge.GetBackend()
	cronBackend, ok := backend.(*cronToolsBackend)
	if !ok {
		t.Fatal("resolved backend should be cronToolsBackend")
	}
	if cronBackend.userPublisher != pub {
		t.Error("global user publisher should propagate")
	}
}

// TestBindRuntimeCronContext 运行时上下文绑定（对齐 Python 4 个 ContextVar）
func TestBindRuntimeCronContext(t *testing.T) {
	ctx := BindRuntimeCronContext(context.Background(), "feishu", "sess-1",
		map[string]any{"chat_type": "group"}, "req-9", "agent.plan")

	rc, ok := ctx.Value(runtimeCronCtxKey).(*RuntimeCronContext)
	if !ok || rc == nil {
		t.Fatal("runtime cron context should be bound")
	}
	if rc.ChannelID != "feishu" || rc.SessionID != "sess-1" || rc.Mode != "agent.plan" {
		t.Errorf("context = %+v", rc)
	}
	// metadata 注入 request_id（原 metadata 保留）
	if rc.Metadata["request_id"] != "req-9" || rc.Metadata["chat_type"] != "group" {
		t.Errorf("metadata = %v", rc.Metadata)
	}
}

// TestBindRuntimeCronContext归一化 空 channel 默认 web、空 mode 保留空
func TestBindRuntimeCronContext归一化(t *testing.T) {
	ctx := BindRuntimeCronContext(context.Background(), "  ", "", nil, "", "")
	rc := ctx.Value(runtimeCronCtxKey).(*RuntimeCronContext)
	if rc.ChannelID != "web" {
		t.Errorf("empty channel should default web, got %q", rc.ChannelID)
	}
	if rc.Mode != "" {
		t.Errorf("empty mode should stay empty, got %q", rc.Mode)
	}
	if rc.Metadata == nil || len(rc.Metadata) != 0 {
		t.Errorf("nil metadata should be empty map, got %v", rc.Metadata)
	}
}

// TestGlobalSetters 四个全局注入 setter
func TestGlobalSetters(t *testing.T) {
	globalMu.Lock()
	oldPush, oldPub, oldCfg, oldAgent := globalGatewayPushFunc, globalUserPublisher, globalConfigLoader, globalAgentClient
	globalGatewayPushFunc, globalUserPublisher, globalConfigLoader, globalAgentClient = nil, nil, nil, nil
	globalMu.Unlock()
	defer func() {
		globalMu.Lock()
		globalGatewayPushFunc, globalUserPublisher, globalConfigLoader, globalAgentClient = oldPush, oldPub, oldCfg, oldAgent
		globalMu.Unlock()
	}()

	called := false
	SetGlobalGatewayPushFunc(func(ctx context.Context, payload map[string]any) error {
		called = true
		return nil
	})
	SetGlobalUserPublisher(&fakeUserPublisher{})
	SetGlobalConfigLoader(func() map[string]any { return map[string]any{"x": 1} })
	SetGlobalAgentClient(nil)

	globalMu.RLock()
	fn := globalGatewayPushFunc
	globalMu.RUnlock()
	if fn == nil {
		t.Fatal("SetGlobalGatewayPushFunc should set global fn")
	}
	if fn(context.Background(), nil) != nil || !called {
		t.Error("push fn should be invocable")
	}

	globalMu.RLock()
	cfg := globalConfigLoader
	globalMu.RUnlock()
	if cfg == nil || cfg()["x"] != 1 {
		t.Error("SetGlobalConfigLoader should set global loader")
	}
}

// TestBridgeSetToolsOverrides tools 覆盖传播
func TestBridgeSetToolsOverrides(t *testing.T) {
	pushRec := &pushRecorder{}
	tools := newTestTools(t, pushRec.push)
	pub := &fakeUserPublisher{}
	bridge := NewCronRuntimeBridge()
	bridge.SetToolsOverrides(tools, pub)

	backend := bridge.GetBackend()
	cronBackend, ok := backend.(*cronToolsBackend)
	if !ok {
		t.Fatal("resolved backend should be cronToolsBackend")
	}
	if cronBackend.tools != tools {
		t.Error("toolsOverride should propagate")
	}
	if cronBackend.userPublisher != pub {
		t.Error("userPublisherOverride should propagate")
	}
}

// TestBridgeGetBackendNilPush 全局无注入时 backend 仍可解析
func TestBridgeGetBackendNilPush(t *testing.T) {
	globalMu.Lock()
	oldPush, oldPub, oldCfg, oldAgent := globalGatewayPushFunc, globalUserPublisher, globalConfigLoader, globalAgentClient
	globalGatewayPushFunc, globalUserPublisher, globalConfigLoader, globalAgentClient = nil, nil, nil, nil
	globalMu.Unlock()
	defer func() {
		globalMu.Lock()
		globalGatewayPushFunc, globalUserPublisher, globalConfigLoader, globalAgentClient = oldPush, oldPub, oldCfg, oldAgent
		globalMu.Unlock()
	}()

	bridge := NewCronRuntimeBridge()
	backend := bridge.GetBackend()
	if backend == nil {
		t.Fatal("backend should resolve without global injections")
	}
	// 二次调用返回缓存
	if bridge.GetBackend() != backend {
		t.Error("second GetBackend should return cached backend")
	}
}

// TestBackendCRUD错误路径 store 错误传播
func TestBackendCRUD错误路径(t *testing.T) {
	backend := newBackendWithTmp(t)
	ctx := context.Background()

	// GetJob 不存在 → nil
	got, err := backend.GetJob(ctx, "ghost")
	if err != nil || got != nil {
		t.Errorf("ghost get = %v, err=%v", got, err)
	}
	// DeleteJob 不存在 → false
	deleted, err := backend.DeleteJob(ctx, "ghost")
	if err != nil || deleted {
		t.Errorf("ghost delete = %v, err=%v", deleted, err)
	}
	// PreviewJob 不存在 → 错误
	if _, err := backend.PreviewJob(ctx, "ghost", 5); err == nil {
		t.Error("ghost preview should error")
	}
	// UpdateJob 不存在 → 错误
	if _, err := backend.UpdateJob(ctx, "ghost", map[string]any{"name": "x"}, nil); err == nil {
		t.Error("ghost update should error")
	}
	// ToggleJob 不存在 → 错误
	if _, err := backend.ToggleJob(ctx, "ghost", true); err == nil {
		t.Error("ghost toggle should error")
	}
	// CreateJob 非法表达式 → 错误
	if _, err := backend.CreateJob(ctx, map[string]any{
		"schedule": map[string]any{"kind": "cron", "expr": "bad-expr"},
	}, nil); err == nil {
		t.Error("invalid cron expr should error")
	}
}

// TestDeepCopyMetadata 深拷贝语义
func TestDeepCopyMetadata(t *testing.T) {
	if deepCopyMetadata(nil) != nil {
		t.Error("nil → nil")
	}
	src := map[string]any{"a": "x"}
	dst := deepCopyMetadata(src)
	dst["a"] = "y"
	if src["a"] != "x" {
		t.Error("copy should isolate from source")
	}
}

// TestContextChannelID nil 上下文安全
func TestContextChannelID(t *testing.T) {
	if contextChannelID(nil) != "" {
		t.Error("nil ctx → empty channel")
	}
	if contextSessionID(nil) != "" {
		t.Error("nil ctx → empty session")
	}
}

// TestJsonRawMessage 序列化降级
func TestJsonRawMessage(t *testing.T) {
	raw := jsonRawMessage(map[string]any{"a": "b"})
	if string(raw) != `{"a":"b"}` {
		t.Errorf("raw = %s", raw)
	}
	// 无法序列化的值降级 {}（chan 不可 marshal）
	raw = jsonRawMessage(map[string]any{"ch": make(chan int)})
	if string(raw) != `{}` {
		t.Errorf("degraded = %s", raw)
	}
}

// TestBackendRunNow转发生命周期 RunNow 空 route 转发
func TestBackendRunNow转发生命周期(t *testing.T) {
	pushRec := &pushRecorder{}
	tools := newTestTools(t, pushRec.push)
	backend := &cronToolsBackend{tools: tools}
	runID, err := backend.RunNow(context.Background(), "job-1")
	if err != nil {
		t.Fatal(err)
	}
	if runID != "" {
		t.Errorf("forwarded result has no run_id, got %q", runID)
	}
	if len(pushRec.payloads) != 1 {
		t.Fatalf("push count = %d", len(pushRec.payloads))
	}
	if pushRec.payloads[0]["body"].(map[string]any)["action"] != "run_now" {
		t.Error("action should be run_now")
	}
}

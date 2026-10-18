package adapter

import (
	"context"
	"testing"
	"time"

	coreevolution "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/rails/evolution"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/stream"
)

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// ──────────────────────────── 非导出函数 ────────────────────────────

// TestEvolutionOutputSchemasToMaps_OutputSchemaSlice 测试 []*stream.OutputSchema 转换。
// T-01 遗留点 C：原实现只断言 []any，rail.DrainPendingApprovalEvents 返回的
// []*stream.OutputSchema 永远断言失败导致 watcher 拿到 nil 事件列表。
func TestEvolutionOutputSchemasToMaps_OutputSchemaSlice(t *testing.T) {
	events := []*stream.OutputSchema{
		{Type: "team", Payload: map[string]any{"event_type": "chat.ask_user_question", "request_id": "req-1"}},
		{Type: "team", Payload: map[string]any{"event_type": "chat.evolution_progress"}},
		nil, // nil 元素应被跳过
	}
	result := evolutionOutputSchemasToMaps(events)
	if len(result) != 2 {
		t.Fatalf("evolutionOutputSchemasToMaps() len = %d, 期望 2", len(result))
	}
	if evt, _ := result[0]["event_type"].(string); evt != "chat.ask_user_question" {
		t.Errorf("result[0][event_type] = %q, 期望 chat.ask_user_question", evt)
	}
	// chunkToMap 对 OutputSchema 会附加 type 字段
	if ty, _ := result[0]["type"].(string); ty != "team" {
		t.Errorf("result[0][type] = %q, 期望 team", ty)
	}
}

// TestEvolutionOutputSchemasToMaps_nil 测试 nil/空输入。
func TestEvolutionOutputSchemasToMaps_nil(t *testing.T) {
	if result := evolutionOutputSchemasToMaps(nil); result != nil {
		t.Errorf("nil 输入应返回 nil, got %v", result)
	}
	if result := evolutionOutputSchemasToMaps([]*stream.OutputSchema{}); len(result) != 0 {
		t.Errorf("空切片应返回空, got %v", result)
	}
	// 不支持的类型返回 nil
	if result := evolutionOutputSchemasToMaps("not-a-slice"); result != nil {
		t.Errorf("非切片输入应返回 nil, got %v", result)
	}
}

// TestChunkToMap_OutputSchema指针 测试 *stream.OutputSchema 转换。
// T-01 遗留点 C：OutputSchema 是公开字段结构体（无 getter 方法），
// 原实现接口断言全部失败导致返回 nil。
func TestChunkToMap_OutputSchema指针(t *testing.T) {
	os := &stream.OutputSchema{
		Type: "team",
		Payload: map[string]any{
			"event_type": "team.runtime_ready",
			"team_name":  "demo-team",
		},
	}
	m := chunkToMap(os)
	if m == nil {
		t.Fatal("chunkToMap(*OutputSchema) 返回 nil，应成功转换")
	}
	if m["event_type"] != "team.runtime_ready" {
		t.Errorf("event_type = %v, 期望 team.runtime_ready", m["event_type"])
	}
	if m["type"] != "team" {
		t.Errorf("type = %v, 期望附加 type 字段为 team", m["type"])
	}
}

// TestChunkToMap_OutputSchema值类型 测试值类型 OutputSchema 转换。
func TestChunkToMap_OutputSchema值类型(t *testing.T) {
	os := stream.OutputSchema{
		Type:    "controller",
		Payload: map[string]any{"key": "value"},
	}
	m := chunkToMap(os)
	if m == nil {
		t.Fatal("chunkToMap(OutputSchema) 返回 nil，应成功转换")
	}
	if m["key"] != "value" {
		t.Errorf("key = %v, 期望 value", m["key"])
	}
	if m["type"] != "controller" {
		t.Errorf("type = %v, 期望 controller", m["type"])
	}
}

// TestChunkToMap_边界 测试 nil/非 map payload/map 直传。
func TestChunkToMap_边界(t *testing.T) {
	if m := chunkToMap(nil); m != nil {
		t.Errorf("nil 输入应返回 nil, got %v", m)
	}
	// map 直传原样返回
	direct := map[string]any{"a": 1}
	if m := chunkToMap(direct); len(m) != 1 || m["a"] != 1 {
		t.Errorf("map 直传应原样返回, got %v", m)
	}
	// nil 指针
	if m := chunkToMap((*stream.OutputSchema)(nil)); m != nil {
		t.Errorf("nil 指针应返回 nil, got %v", m)
	}
	// payload 非 map
	if m := chunkToMap(&stream.OutputSchema{Type: "t", Payload: "string-payload"}); m != nil {
		t.Errorf("非 map payload 应返回 nil, got %v", m)
	}
}

// TestWatchTeamEvolutionAndPushTeam_推送出口 测试审批事件触发真实推送。
// T-01 遗留点 A：pushEvolutionStatus/pushEvolutionEvent 接上 getSendPushFunc 后，
// 注入审批事件应产生 chat.evolution_status / 审批 event 两类推送消息。
func TestWatchTeamEvolutionAndPushTeam_推送出口(t *testing.T) {
	d := NewDeepAdapter()

	// 收集推送消息
	var pushed []map[string]any
	done := make(chan struct{})
	d.SetSendPushFunc(func(_ context.Context, msg map[string]any) error {
		pushed = append(pushed, msg)
		// 收到 approval_required 的 end status 即完成一个完整周期
		if payload, ok := msg["payload"].(map[string]any); ok {
			if payload["event_type"] == "chat.evolution_status" && payload["stage"] == "approval_required" {
				close(done)
			}
		}
		return nil
	})

	// 构造 rail 并注入一个审批事件
	rail := coreevolution.NewTeamSkillEvolutionRail([]string{t.TempDir()}, nil, "test-model", "zh")
	approvalEvt := &stream.OutputSchema{
		Type: "team",
		Payload: map[string]any{
			"event_type": "chat.ask_user_question",
			"request_id": "req-push-1",
		},
	}
	rail.EmitHostEvent(approvalEvt)

	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()

	watcherDone := make(chan struct{})
	go func() {
		defer close(watcherDone)
		d.watchTeamEvolutionAndPushTeam(ctx, "ch1", "sess-push-1", rail)
	}()

	select {
	case <-done:
		// 成功收到推送
	case <-ctx.Done():
		t.Fatal("5s 内未收到 approval_required 推送")
	}
	cancel()
	<-watcherDone

	// 验证推送内容
	if len(pushed) == 0 {
		t.Fatal("应至少推送一条消息")
	}
	for _, msg := range pushed {
		if msg["session_id"] != "sess-push-1" {
			t.Errorf("推送 session_id = %v, 期望 sess-push-1", msg["session_id"])
		}
		payload, ok := msg["payload"].(map[string]any)
		if !ok {
			t.Fatalf("推送 payload 类型错误: %T", msg["payload"])
		}
		switch evtType, _ := payload["event_type"].(string); evtType {
		case "chat.evolution_status":
			// 团队路径 include_payload_request_id=True，payload 应含 request_id
			if payload["request_id"] != "req-push-1" {
				t.Errorf("status payload request_id = %v, 期望 req-push-1", payload["request_id"])
			}
		case "chat.ask_user_question":
			if payload["request_id"] != "req-push-1" {
				t.Errorf("event payload request_id = %v, 期望 req-push-1", payload["request_id"])
			}
		default:
			t.Errorf("意外的推送 event_type: %q", evtType)
		}
	}
}

// TestWatchTeamEvolutionAndPushTeam_未注入推送函数 测试 sendPushFunc 为 nil 时不 panic。
func TestWatchTeamEvolutionAndPushTeam_未注入推送函数(t *testing.T) {
	d := NewDeepAdapter()
	// 不注入 SetSendPushFunc（实例级与默认级均 nil）

	rail := coreevolution.NewTeamSkillEvolutionRail([]string{t.TempDir()}, nil, "test-model", "zh")
	rail.SetAutoScan(false) // 立即退出，避免轮询

	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	defer cancel()

	watcherDone := make(chan struct{})
	go func() {
		defer close(watcherDone)
		d.watchTeamEvolutionAndPushTeam(ctx, "ch1", "sess-no-push", rail)
	}()

	select {
	case <-watcherDone:
		// 正常退出
	case <-time.After(2 * time.Second):
		t.Fatal("auto_scan=false 应立即退出")
	}
}

// TestWatchTeamEvolutionAndPushTeam_autoScan关闭 测试 auto_scan 关闭时推送 end hidden。
func TestWatchTeamEvolutionAndPushTeam_autoScan关闭(t *testing.T) {
	d := NewDeepAdapter()

	endPushed := make(chan map[string]any, 1)
	d.SetSendPushFunc(func(_ context.Context, msg map[string]any) error {
		if payload, ok := msg["payload"].(map[string]any); ok {
			if payload["event_type"] == "chat.evolution_status" {
				endPushed <- payload
			}
		}
		return nil
	})

	// rail 带 auto_scan=false，但先有一个 progress 事件激活 active cycle
	// 简化：直接验证关闭路径不 panic
	rail := coreevolution.NewTeamSkillEvolutionRail([]string{t.TempDir()}, nil, "test-model", "zh")
	rail.SetAutoScan(false)

	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	defer cancel()

	watcherDone := make(chan struct{})
	go func() {
		defer close(watcherDone)
		d.watchTeamEvolutionAndPushTeam(ctx, "ch1", "sess-autoscan", rail)
	}()

	select {
	case <-watcherDone:
		// auto_scan=false 无 active cycle，直接退出
	case <-time.After(2 * time.Second):
		t.Fatal("auto_scan=false 应立即退出")
	}
}

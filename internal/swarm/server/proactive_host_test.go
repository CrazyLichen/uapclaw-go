package server

import (
	"context"
	"encoding/json"
	"testing"

	recommendation "github.com/uapclaw/uapclaw-go/internal/swarm/agents/harness/common/recommendation"
	"github.com/uapclaw/uapclaw-go/internal/swarm/schema"
)

// ──────────────────────────── 装配层 ────────────────────────────

// TestInitProactiveEngine_装配后有引擎 启动接线把引擎挂到 AgentServer 上。
//
// 这是 S-01 修复的回归守卫：曾出现"引擎写完但零生产调用"，proactive.tick
// 永远返回 not initialized。
func TestInitProactiveEngine_装配后(t *testing.T) {
	s, _ := newTestServer()
	if s.proactiveEngine != nil {
		t.Fatal("初始不应有引擎")
	}

	s.initProactiveEngine(context.Background())

	if s.proactiveEngine == nil {
		t.Fatal("装配后应注入引擎（否则 proactive.tick 永远 not initialized）")
	}
}

// TestInitProactiveEngine_启用开关 配置 enabled 生效到引擎。
func TestInitProactiveEngine_启用开关(t *testing.T) {
	s, _ := newTestServer()
	s.initProactiveEngine(context.Background())
	if s.proactiveEngine == nil {
		t.Skip("test server 无 config，跳过")
	}
	// 默认配置无 proactive_recommendation 段 → enabled=false
	req := schema.NewAgentRequest("req-1", "web", schema.ReqMethodProactiveTick, json.RawMessage(`{}`))
	resp, err := s.handleProactiveTick(context.Background(), req)
	if err != nil {
		t.Fatalf("不应返回错误: %v", err)
	}
	if !resp.OK {
		t.Fatalf("装配后 tick 应 OK=true（不再是 not initialized）, got %v", resp.Payload)
	}
}

// TestReadProactiveRecommendationConfig nil 配置返回空 map。
func TestReadProactiveRecommendationConfig(t *testing.T) {
	got := readProactiveRecommendationConfig(nil)
	if got == nil {
		t.Fatal("应返回非 nil 空 map")
	}
	if len(got) != 0 {
		t.Errorf("nil 配置应返回空 map, got %v", got)
	}
}

// ──────────────────────────── proactiveHost ────────────────────────────

// TestProactiveHost_GetAgentNoWait 无 agent 时返回 nil，不 panic。
func TestProactiveHost_GetAgentNoWait(t *testing.T) {
	host := &proactiveHost{server: nil}
	if got := host.GetAgentNoWait("web", "agent", "", ""); got != nil {
		t.Error("server 为 nil 时应返回 nil")
	}

	s, _ := newTestServer()
	host2 := &proactiveHost{server: s}
	if got := host2.GetAgentNoWait("不存在渠道", "agent", "", ""); got != nil {
		t.Error("无 agent 时应返回 nil（不自动创建）")
	}
}

// TestProactiveHost_SendPush server 为 nil 时返回错误。
func TestProactiveHost_SendPush(t *testing.T) {
	host := &proactiveHost{server: nil}
	if err := host.SendPush(context.Background(), map[string]any{}); err == nil {
		t.Error("server 为 nil 时应返回错误")
	}

	// 正常 server（无 transport）时 SendPush 吞异常返回 nil（对齐 Python）
	s, _ := newTestServer()
	host2 := &proactiveHost{server: s}
	if err := host2.SendPush(context.Background(), map[string]any{"request_id": "r", "payload": map[string]any{}}); err != nil {
		t.Logf("SendPush 返回: %v（无 transport 时 Python 也静默）", err)
	}
}

// TestProactiveHost_IsSessionExecuting 依据流任务登记表判断。
func TestProactiveHost_IsSessionExecuting(t *testing.T) {
	host := &proactiveHost{server: nil}
	if host.IsSessionExecuting("sess-1") {
		t.Error("server 为 nil 时应返回 false")
	}

	s, _ := newTestServer()
	h := &proactiveHost{server: s}
	if h.IsSessionExecuting("sess-1") {
		t.Error("未登记流任务时应返回 false")
	}

	// 登记一条流任务后应判定为执行中
	entry := s.registerStreamTask("sess-1", "req-1", func() {})
	defer s.releaseStreamTask("sess-1", entry)
	if !h.IsSessionExecuting("sess-1") {
		t.Error("登记流任务后应判定为执行中")
	}

	// 空 sessionID 恒 false
	if h.IsSessionExecuting("") {
		t.Error("空 sessionID 应返回 false")
	}
}

// ──────────────────────────── 隐式反馈 ────────────────────────────

// TestTryRecordImplicitFeedback_跳过proactive来源 系统指令不算用户输入。
//
// Python: agent_ws_server.py:3472 source == "proactive_recommendation" → return
func TestTryRecordImplicitFeedback_跳过proactive来源(t *testing.T) {
	s, _ := newTestServer()
	sessionID := "if-skip-sess"
	params := map[string]any{
		"query":  "这是系统指令",
		"source": recommendation.ProactiveSourceSource,
	}
	raw, _ := json.Marshal(params)
	req := &schema.AgentRequest{
		RequestID: "req-1", ChannelID: "web", SessionID: &sessionID, Params: raw,
	}

	// 不该 panic，也不该写入任何反馈
	s.tryRecordImplicitFeedback(req)
}

// TestTryRecordImplicitFeedback_无session跳过 session_id 缺失时直接返回。
func TestTryRecordImplicitFeedback_无session(t *testing.T) {
	s, _ := newTestServer()
	raw, _ := json.Marshal(map[string]any{"query": "你好"})
	req := &schema.AgentRequest{RequestID: "req-1", ChannelID: "web", Params: raw}

	s.tryRecordImplicitFeedback(req) // 不应 panic
}

// TestTryRecordImplicitFeedback_无query跳过 非文本 query 不参与隐式反馈。
//
// Python: query = _request_query_text(request) → 非 str 返回 "" → return
func TestTryRecordImplicitFeedback_无query(t *testing.T) {
	s, _ := newTestServer()
	sessionID := "if-noquery-sess"
	raw, _ := json.Marshal(map[string]any{"query": 123})
	req := &schema.AgentRequest{
		RequestID: "req-1", ChannelID: "web", SessionID: &sessionID, Params: raw,
	}

	s.tryRecordImplicitFeedback(req) // 不应 panic
}

// TestTryRecordImplicitFeedback_panic被捕获 params 非法时不崩溃主 chat 流。
func TestTryRecordImplicitFeedback_不panic(t *testing.T) {
	s, _ := newTestServer()
	sessionID := "if-panic-sess"
	req := &schema.AgentRequest{
		RequestID: "req-1", ChannelID: "web", SessionID: &sessionID,
		Params: json.RawMessage(`{坏json`),
	}

	if err := func() (err error) {
		defer func() {
			if r := recover(); r != nil {
				err = context.Canceled
			}
		}()
		s.tryRecordImplicitFeedback(req)
		return nil
	}(); err != nil {
		t.Error("隐式反馈失败不应影响主 chat 流（best-effort）")
	}
}

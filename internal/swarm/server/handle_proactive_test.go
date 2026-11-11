package server

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	recommendation "github.com/uapclaw/uapclaw-go/internal/swarm/agents/harness/common/recommendation"
	"github.com/uapclaw/uapclaw-go/internal/swarm/schema"
)

// ──────────────────────────── proactive.tick ────────────────────────────

// TestHandleProactiveTick_引擎未初始化 未注入引擎时返回 error，不 panic。
// Python: handlers/ops.py:61-67
func TestHandleProactiveTick_引擎未初始化(t *testing.T) {
	s, _ := newTestServer()
	req := schema.NewAgentRequest("req-1", "web", schema.ReqMethodProactiveTick, json.RawMessage(`{}`))

	resp, err := s.handleProactiveTick(context.Background(), req)
	if err != nil {
		t.Fatalf("不应返回错误: %v", err)
	}
	if resp.OK {
		t.Error("未初始化时应 OK=false")
	}
	if resp.RequestID != "req-1" {
		t.Errorf("RequestID = %q, 期望 req-1", resp.RequestID)
	}
	payload := payloadOf(t, resp)
	if payload["error"] != "ProactiveEngine not initialized" {
		t.Errorf("error = %v, 期望「ProactiveEngine not initialized」", payload["error"])
	}
}

// TestHandleProactiveTick_未启用 disabled 引擎返回 no_recommendation。
// Python: handlers/ops.py:73-82
func TestHandleProactiveTick_未启用(t *testing.T) {
	s, _ := newTestServer()
	s.SetProactiveEngine(recommendation.NewProactiveEngine(map[string]any{"enabled": false}))

	req := schema.NewAgentRequest("req-2", "web", schema.ReqMethodProactiveTick, json.RawMessage(`{}`))
	resp, err := s.handleProactiveTick(context.Background(), req)
	if err != nil {
		t.Fatalf("不应返回错误: %v", err)
	}
	if !resp.OK {
		t.Fatalf("引擎已注入时应 OK=true, got %v", resp.Payload)
	}
	payload := payloadOf(t, resp)
	if payload["success"] != false {
		t.Errorf("success = %v, 期望 false", payload["success"])
	}
	status, _ := payload["status"].(string)
	if status == "" {
		t.Error("status 不应为空")
	}
}

// TestHandleProactiveTick_带target_channel 透传 target_channel。
func TestHandleProactiveTick_带target_channel(t *testing.T) {
	s, _ := newTestServer()
	s.SetProactiveEngine(recommendation.NewProactiveEngine(map[string]any{"enabled": false}))

	req := schema.NewAgentRequest("req-3", "web", schema.ReqMethodProactiveTick,
		json.RawMessage(`{"target_channel":"tui"}`))
	resp, err := s.handleProactiveTick(context.Background(), req)
	if err != nil {
		t.Fatalf("不应返回错误: %v", err)
	}
	if !resp.OK {
		t.Fatal("应 OK=true")
	}
}

// TestHandleProactiveTick_参数非法 params 不是合法 JSON 时返回 error。
func TestHandleProactiveTick_参数非法(t *testing.T) {
	s, _ := newTestServer()
	s.SetProactiveEngine(recommendation.NewProactiveEngine(map[string]any{"enabled": true}))

	req := schema.NewAgentRequest("req-4", "web", schema.ReqMethodProactiveTick, json.RawMessage(`{不是json`))
	resp, err := s.handleProactiveTick(context.Background(), req)
	if err != nil {
		t.Fatalf("不应返回 error（错误应放 payload）: %v", err)
	}
	if resp.OK {
		t.Error("参数非法时应 OK=false")
	}
	payload := payloadOf(t, resp)
	if payload["error"] == nil || payload["error"] == "" {
		t.Error("应带 error 说明")
	}
}

// TestHandleProactiveTick_无params params 缺失按空 map 处理。
func TestHandleProactiveTick_无params(t *testing.T) {
	s, _ := newTestServer()
	s.SetProactiveEngine(recommendation.NewProactiveEngine(map[string]any{"enabled": false}))

	req := schema.NewAgentRequest("req-5", "web", schema.ReqMethodProactiveTick, nil)
	resp, err := s.handleProactiveTick(context.Background(), req)
	if err != nil {
		t.Fatalf("不应返回错误: %v", err)
	}
	if !resp.OK {
		t.Error("缺 params 应按空 map 处理")
	}
}

// ──────────────────────────── proactive.feedback ────────────────────────────

// TestHandleProactiveFeedback_缺rec_id rec_id 缺失时返回 Invalid params。
// Python: handlers/ops.py:103-116
func TestHandleProactiveFeedback_缺rec_id(t *testing.T) {
	s, _ := newTestServer()
	req := schema.NewAgentRequest("req-1", "web", schema.ReqMethodProactiveFeedback,
		json.RawMessage(`{"feedback_type":"explicit_like"}`))

	resp, err := s.handleProactiveFeedback(req)
	if err != nil {
		t.Fatalf("不应返回错误: %v", err)
	}
	if resp.OK {
		t.Error("缺 rec_id 时应 OK=false")
	}
	payload := payloadOf(t, resp)
	errMsg, _ := payload["error"].(string)
	if errMsg == "" {
		t.Fatal("应带 error")
	}
	if !strings.Contains(errMsg, "rec_id") || !strings.Contains(errMsg, "feedback_type") {
		t.Errorf("error = %q, 应说明 rec_id 与 feedback_type 要求", errMsg)
	}
}

// TestHandleProactiveFeedback_非法feedback_type 只接受 explicit_like/explicit_dislike。
func TestHandleProactiveFeedback_非法类型(t *testing.T) {
	s, _ := newTestServer()
	for _, feedbackType := range []string{"implicit", "like", "", "explicit"} {
		req := schema.NewAgentRequest("req-1", "web", schema.ReqMethodProactiveFeedback,
			json.RawMessage(`{"rec_id":"rec_1","feedback_type":"`+feedbackType+`"}`))
		resp, err := s.handleProactiveFeedback(req)
		if err != nil {
			t.Fatalf("feedback_type=%q 不应返回 error", feedbackType)
		}
		if resp.OK {
			t.Errorf("feedback_type=%q 应被拒绝", feedbackType)
		}
	}
}

// TestHandleProactiveFeedback_正常 合法参数返回 feedback_recorded。
// Python: handlers/ops.py:126-135
func TestHandleProactiveFeedback_正常(t *testing.T) {
	s, _ := newTestServer()
	req := schema.NewAgentRequest("req-1", "web", schema.ReqMethodProactiveFeedback,
		json.RawMessage(`{"rec_id":"rec_abc","feedback_type":"explicit_like","rec_type":"skill_recommend","rec_target":"翻译技能"}`))

	resp, err := s.handleProactiveFeedback(req)
	if err != nil {
		t.Fatalf("不应返回错误: %v", err)
	}
	if !resp.OK {
		t.Fatalf("应 OK=true, got %v", resp.Payload)
	}
	payload := payloadOf(t, resp)
	if payload["status"] != "feedback_recorded" {
		t.Errorf("status = %v, 期望 feedback_recorded", payload["status"])
	}
	if payload["rec_id"] != "rec_abc" {
		t.Errorf("rec_id = %v, 期望 rec_abc", payload["rec_id"])
	}
}

// TestHandleProactiveFeedback_proactive前缀回退 rec_type 缺失时回退 proactive_type。
// Python: handlers/ops.py:100-101
func TestHandleProactiveFeedback_proactive前缀回退(t *testing.T) {
	s, _ := newTestServer()
	req := schema.NewAgentRequest("req-1", "web", schema.ReqMethodProactiveFeedback,
		json.RawMessage(`{"rec_id":"rec_x","feedback_type":"explicit_dislike","proactive_type":"task_reminder","proactive_target":"整理报表"}`))

	resp, err := s.handleProactiveFeedback(req)
	if err != nil {
		t.Fatalf("不应返回错误: %v", err)
	}
	if !resp.OK {
		t.Fatalf("应 OK=true, got %v", resp.Payload)
	}
}

// TestHandleProactiveFeedback_参数非法 params 不是合法 JSON。
func TestHandleProactiveFeedback_参数非法(t *testing.T) {
	s, _ := newTestServer()
	req := schema.NewAgentRequest("req-1", "web", schema.ReqMethodProactiveFeedback,
		json.RawMessage(`{坏json`))

	resp, err := s.handleProactiveFeedback(req)
	if err != nil {
		t.Fatalf("不应返回 error（错误应放 payload）: %v", err)
	}
	if resp.OK {
		t.Error("参数非法时应 OK=false")
	}
}

// ──────────────────────────── 辅助 ────────────────────────────

// payloadOf 取响应 payload。
func payloadOf(t *testing.T, resp *schema.AgentResponse) map[string]any {
	t.Helper()
	if resp == nil {
		t.Fatal("响应不应为 nil")
	}
	if resp.Payload == nil {
		t.Fatal("payload 不应为 nil")
	}
	return resp.Payload
}

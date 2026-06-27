package e2a

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/uapclaw/uapclaw-go/internal/swarm/schema"
)

// ──────────────────────────── E2AToAgentRequest ────────────────────────────

// TestE2AToAgentRequest_正常转换 验证 E2AEnvelope → AgentRequest 正常转换
func TestE2AToAgentRequest_正常转换(t *testing.T) {
	env := &E2AEnvelope{
		ProtocolVersion: E2AProtocolVersion,
		RequestID:       "req-1",
		Channel:         "web",
		SessionID:       "sess-1",
		ChatID:          "chat-1",
		Method:          "chat.send",
		Params:          map[string]any{"query": "hello"},
		IsStream:        true,
		Timestamp:       time.Now().UTC().Format(time.RFC3339),
		ChannelContext:  map[string]any{"trace_id": "abc"},
	}

	req, err := E2AToAgentRequest(env)
	if err != nil {
		t.Fatalf("转换失败: %v", err)
	}
	if req.RequestID != "req-1" {
		t.Errorf("request_id 期望 req-1，实际 %s", req.RequestID)
	}
	if req.ChannelID != "web" {
		t.Errorf("channel_id 期望 web，实际 %s", req.ChannelID)
	}
	if req.SessionID == nil || *req.SessionID != "sess-1" {
		t.Errorf("session_id 期望 sess-1，实际 %v", req.SessionID)
	}
	if req.ChatID == nil || *req.ChatID != "chat-1" {
		t.Errorf("chat_id 期望 chat-1，实际 %v", req.ChatID)
	}
	if req.ReqMethod != schema.ReqMethodChatSend {
		t.Errorf("req_method 期望 chat.send，实际 %s", req.ReqMethod)
	}
	if req.IsStream != true {
		t.Error("is_stream 期望 true")
	}

	var params map[string]any
	if err := json.Unmarshal(req.Params, &params); err != nil {
		t.Fatalf("params 反序列化失败: %v", err)
	}
	if params["query"] != "hello" {
		t.Errorf("params.query 期望 hello，实际 %v", params["query"])
	}
}

// TestE2AToAgentRequest_默认Channel 验证 channel 为空时默认为 web
func TestE2AToAgentRequest_默认Channel(t *testing.T) {
	env := &E2AEnvelope{
		RequestID: "req-2",
		Channel:   "",
		Method:    "chat.send",
	}

	req, err := E2AToAgentRequest(env)
	if err != nil {
		t.Fatalf("转换失败: %v", err)
	}
	if req.ChannelID != "web" {
		t.Errorf("channel_id 期望 web，实际 %s", req.ChannelID)
	}
}

// TestE2AToAgentRequest_未知方法 验证未知方法返回 error
func TestE2AToAgentRequest_未知方法(t *testing.T) {
	env := &E2AEnvelope{
		RequestID: "req-3",
		Channel:   "web",
		Method:    "nonexistent.method",
	}

	_, err := E2AToAgentRequest(env)
	if err == nil {
		t.Error("未知方法应返回 error")
	}
}

// TestE2AToAgentRequest_FallbackEnvelope 验证含 normalize_failed 的 envelope 返回 error
func TestE2AToAgentRequest_FallbackEnvelope(t *testing.T) {
	env := &E2AEnvelope{
		RequestID: "req-4",
		Channel:   "web",
		Method:    "chat.send",
		ChannelContext: map[string]any{
			e2aInternalContextKey: map[string]any{
				e2aFallbackFailedKey: true,
			},
		},
	}

	_, err := E2AToAgentRequest(env)
	if err == nil {
		t.Error("fallback envelope 应返回 error")
	}
}

// TestE2AToAgentRequest_空方法 验证空方法不报错，ReqMethod 为零值
func TestE2AToAgentRequest_空方法(t *testing.T) {
	env := &E2AEnvelope{
		RequestID: "req-5",
		Channel:   "web",
		Method:    "",
	}

	req, err := E2AToAgentRequest(env)
	if err != nil {
		t.Fatalf("空方法不应报错: %v", err)
	}
	if req.ReqMethod != "" {
		t.Errorf("空方法应为零值 ReqMethod，实际 %s", req.ReqMethod)
	}
}

// TestE2AToAgentRequest_无ChannelContext 验证无 ChannelContext 时 metadata 为 nil
func TestE2AToAgentRequest_无ChannelContext(t *testing.T) {
	env := &E2AEnvelope{
		RequestID: "req-6",
		Channel:   "web",
		Method:    "chat.send",
	}

	req, err := E2AToAgentRequest(env)
	if err != nil {
		t.Fatalf("转换失败: %v", err)
	}
	if req.Metadata != nil {
		t.Errorf("无 ChannelContext 时 metadata 应为 nil，实际 %v", req.Metadata)
	}
}

// TestE2AToAgentRequest_ChannelContext去掉Internal 验证 _jiuwenswarm 被移除但其它键保留
func TestE2AToAgentRequest_ChannelContext去掉Internal(t *testing.T) {
	env := &E2AEnvelope{
		RequestID: "req-7",
		Channel:   "web",
		Method:    "chat.send",
		ChannelContext: map[string]any{
			"trace_id": "abc",
			e2aInternalContextKey: map[string]any{
				"some_key": "some_val",
			},
		},
	}

	req, err := E2AToAgentRequest(env)
	if err != nil {
		t.Fatalf("转换失败: %v", err)
	}
	if req.Metadata == nil {
		t.Fatal("metadata 不应为 nil")
	}
	if _, ok := req.Metadata[e2aInternalContextKey]; ok {
		t.Error("_jiuwenswarm 应被移除")
	}
	if req.Metadata["trace_id"] != "abc" {
		t.Errorf("trace_id 期望 abc，实际 %v", req.Metadata["trace_id"])
	}
}

// ──────────────────────────── e2aTimestampToFloat ────────────────────────────

// TestE2aTimestampToFloat_ISO8601 验证 ISO 8601 格式转换
func TestE2aTimestampToFloat_ISO8601(t *testing.T) {
	now := time.Date(2025, 1, 15, 10, 30, 0, 0, time.UTC)
	ts := now.Format(time.RFC3339)
	result := e2aTimestampToFloat(ts)
	expected := float64(now.Unix())
	if result != expected {
		t.Errorf("时间戳转换期望 %f，实际 %f", expected, result)
	}
}

// TestE2aTimestampToFloat_带Z后缀 验证 Z 后缀处理
func TestE2aTimestampToFloat_带Z后缀(t *testing.T) {
	now := time.Date(2025, 6, 1, 0, 0, 0, 0, time.UTC)
	ts := now.Format(time.RFC3339) // 2025-06-01T00:00:00Z
	result := e2aTimestampToFloat(ts)
	expected := float64(now.Unix())
	if result != expected {
		t.Errorf("Z 后缀时间戳期望 %f，实际 %f", expected, result)
	}
}

// TestE2aTimestampToFloat_空串 验证空串返回 0.0
func TestE2aTimestampToFloat_空串(t *testing.T) {
	result := e2aTimestampToFloat("")
	if result != 0.0 {
		t.Errorf("空串期望 0.0，实际 %f", result)
	}
}

// TestE2aTimestampToFloat_无效格式 验证无效格式返回 0.0
func TestE2aTimestampToFloat_无效格式(t *testing.T) {
	result := e2aTimestampToFloat("not-a-date")
	if result != 0.0 {
		t.Errorf("无效格式期望 0.0，实际 %f", result)
	}
}

// TestE2aTimestampToFloat_带纳秒 验证纳秒精度保留
func TestE2aTimestampToFloat_带纳秒(t *testing.T) {
	now := time.Date(2025, 1, 15, 10, 30, 0, 123456789, time.UTC)
	ts := now.Format(time.RFC3339Nano)
	result := e2aTimestampToFloat(ts)
	expected := float64(now.UnixNano()) / 1e9
	diff := result - expected
	if diff < 0 {
		diff = -diff
	}
	if diff > 1e-6 {
		t.Errorf("纳秒精度期望 %f，实际 %f", expected, result)
	}
}

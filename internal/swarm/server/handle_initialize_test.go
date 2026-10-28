package server

import (
	"context"
	"encoding/json"
	"testing"

	swarmtools "github.com/uapclaw/uapclaw-go/internal/swarm/agents/harness/common/tools"
	"github.com/uapclaw/uapclaw-go/internal/swarm/schema"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// ──────────────────────────── 非导出函数 ────────────────────────────

// TestHandleInitialize 验证 initialize 返回 capabilities 和 protocol_version。
func TestHandleInitialize(t *testing.T) {
	s, _ := newTestServer()
	req := schema.NewAgentRequest("req-1", "web", schema.ReqMethodInitialize, json.RawMessage(`{}`))

	resp, err := s.handleInitialize(context.Background(), req)
	if err != nil {
		t.Fatalf("handleInitialize 返回错误: %v", err)
	}
	if !resp.OK {
		t.Error("resp.OK 应为 true")
	}
	if _, ok := resp.Payload["capabilities"]; !ok {
		t.Error("payload 应包含 capabilities")
	}
	if pv, ok := resp.Payload["protocol_version"]; !ok || pv != "1.0" {
		t.Errorf("payload.protocol_version 应为 1.0, 实际: %v", pv)
	}
}

// TestHandleACPToolResponse 验证 acp.tool_response 完成未知 ID 时回 ignored。
// Python L4684-4694: complete 失败 → accepted=false + ignored=true + reason。
func TestHandleACPToolResponse(t *testing.T) {
	s, _ := newTestServer()
	mgr := swarmtools.GetAcpOutputManager()
	mgr.ResetState()
	t.Cleanup(mgr.ResetState)
	req := schema.NewAgentRequest("req-1", "web", schema.ReqMethodACPToolResponse,
		json.RawMessage(`{"jsonrpc_id":"nope","response":{"result":"x"}}`))

	resp, err := s.handleACPToolResponse(context.Background(), req)
	if err != nil {
		t.Fatalf("handleACPToolResponse 返回错误: %v", err)
	}
	if resp.Payload["accepted"] != false {
		t.Errorf("payload.accepted 应为 false, 实际: %v", resp.Payload["accepted"])
	}
	if resp.Payload["ignored"] != true {
		t.Errorf("payload.ignored 应为 true, 实际: %v", resp.Payload["ignored"])
	}
	if resp.Payload["reason"] != "unknown_or_late_response" {
		t.Errorf("payload.reason = %v", resp.Payload["reason"])
	}
	if resp.Payload["jsonrpc_id"] != "nope" {
		t.Errorf("payload.jsonrpc_id = %v", resp.Payload["jsonrpc_id"])
	}
}

// TestHandleACPToolResponse_完成pending 验证完成 pending 请求回 accepted=true。
func TestHandleACPToolResponse_完成pending(t *testing.T) {
	s, _ := newTestServer()
	mgr := swarmtools.GetAcpOutputManager()
	mgr.ResetState()
	t.Cleanup(mgr.ResetState)

	// 登记 pending 请求
	done := make(chan swarmtools.AcpOutputResult, 1)
	mgr.AddPendingRequest(&swarmtools.AcpOutputRequest{
		JsonrpcID: "42",
		Method:    "fs/read_text_file",
		Done:      done,
	})

	req := schema.NewAgentRequest("req-2", "acp", schema.ReqMethodACPToolResponse,
		json.RawMessage(`{"jsonrpc_id":42,"response":{"result":{"content":"hi"}}}`))
	resp, err := s.handleACPToolResponse(context.Background(), req)
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if resp.Payload["accepted"] != true {
		t.Errorf("payload.accepted = %v", resp.Payload["accepted"])
	}
	// done 通道应收到响应
	select {
	case res := <-done:
		if res.Err != nil {
			t.Errorf("res.Err = %v", res.Err)
		}
		content, _ := res.Response["result"].(map[string]any)
		if content["content"] != "hi" {
			t.Errorf("res.Response = %v", res.Response)
		}
	default:
		t.Fatal("done 通道应收到完成结果")
	}
}

// TestHandleACPToolResponse_非dict响应 验证 response 非 dict 归空后仍走 complete。
func TestHandleACPToolResponse_非dict响应(t *testing.T) {
	s, _ := newTestServer()
	mgr := swarmtools.GetAcpOutputManager()
	mgr.ResetState()
	t.Cleanup(mgr.ResetState)

	req := schema.NewAgentRequest("req-3", "acp", schema.ReqMethodACPToolResponse,
		json.RawMessage(`{"jsonrpc_id":"zz","response":"not-a-map"}`))
	resp, err := s.handleACPToolResponse(context.Background(), req)
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	// 未知 ID + 空 payload → complete 失败 → ignored
	if resp.Payload["accepted"] != false {
		t.Errorf("payload.accepted = %v", resp.Payload["accepted"])
	}
}

// TestHandleACPToolResponse_空params 验证空 params 不 panic。
func TestHandleACPToolResponse_空params(t *testing.T) {
	s, _ := newTestServer()
	req := schema.NewAgentRequest("req-4", "web", schema.ReqMethodACPToolResponse, json.RawMessage(`{}`))
	resp, err := s.handleACPToolResponse(context.Background(), req)
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if resp.Payload["accepted"] != false {
		t.Errorf("payload.accepted = %v", resp.Payload["accepted"])
	}
	// nil params → jsonrpc_id 为 nil
	if resp.Payload["jsonrpc_id"] != nil {
		t.Errorf("payload.jsonrpc_id = %v", resp.Payload["jsonrpc_id"])
	}
}

// TestJsonToStr 验证 jsonToStr 各类型。
func TestJsonToStr(t *testing.T) {
	if got := jsonToStr(nil); got != "<nil>" {
		t.Errorf("nil = %q", got)
	}
	if got := jsonToStr("abc"); got != "abc" {
		t.Errorf("string = %q", got)
	}
	if got := jsonToStr(float64(42)); got != "42" {
		t.Errorf("number = %q", got)
	}
}

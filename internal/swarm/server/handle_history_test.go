package server

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/swarm/schema"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// ──────────────────────────── 非导出函数 ────────────────────────────

// TestHandleHistoryGet_无SessionID 验证 history.get 无 session_id 时返回空历史。
func TestHandleHistoryGet_无SessionID(t *testing.T) {
	s, _ := newTestServer()
	req := schema.NewAgentRequest("req-1", "web", schema.ReqMethodHistoryGet, json.RawMessage(`{}`))

	resp, err := s.handleHistoryGet(context.Background(), req)
	if err != nil {
		t.Fatalf("handleHistoryGet 返回错误: %v", err)
	}
	if !resp.OK {
		t.Error("resp.OK 应为 true")
	}
	if history, ok := resp.Payload["history"]; !ok {
		t.Error("payload 应包含 history")
	} else if arr, ok := history.([]any); !ok || len(arr) != 0 {
		t.Errorf("payload.history 应为空数组, 实际: %v", history)
	}
}

// TestHandleHistoryGet_不存在的Session 验证 history.get 不存在的 session 返回空。
func TestHandleHistoryGet_不存在的Session(t *testing.T) {
	s, _ := newTestServer()
	req := schema.NewAgentRequest("req-1", "web", schema.ReqMethodHistoryGet,
		json.RawMessage(`{"session_id": "nonexistent-session"}`))

	resp, err := s.handleHistoryGet(context.Background(), req)
	if err != nil {
		t.Fatalf("handleHistoryGet 返回错误: %v", err)
	}
	if !resp.OK {
		t.Error("resp.OK 应为 true")
	}
	if total, ok := resp.Payload["total"]; !ok || total != 0 {
		t.Errorf("payload.total 应为 0, 实际: %v", total)
	}
}

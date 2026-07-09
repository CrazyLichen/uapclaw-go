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

// TestHandleBrowserStart 验证 browser.start 返回 NOT_IMPLEMENTED。
func TestHandleBrowserStart(t *testing.T) {
	s, _ := newTestServer()
	req := schema.NewAgentRequest("req-1", "web", schema.ReqMethodBrowserStart, json.RawMessage(`{}`))

	resp, err := s.handleBrowserStart(context.Background(), req)
	if err != nil {
		t.Fatalf("handleBrowserStart 返回错误: %v", err)
	}
	if resp.OK {
		t.Error("resp.OK 应为 false（NOT_IMPLEMENTED）")
	}
}

// TestHandleBrowserRuntimeRestart 验证 browser.runtime_restart 返回 NOT_IMPLEMENTED。
func TestHandleBrowserRuntimeRestart(t *testing.T) {
	s, _ := newTestServer()
	req := schema.NewAgentRequest("req-1", "web", schema.ReqMethodBrowserRuntimeRestart, json.RawMessage(`{}`))

	resp, err := s.handleBrowserRuntimeRestart(context.Background(), req)
	if err != nil {
		t.Fatalf("handleBrowserRuntimeRestart 返回错误: %v", err)
	}
	if resp.OK {
		t.Error("resp.OK 应为 false（NOT_IMPLEMENTED）")
	}
}

package server

import (
	"context"

	"github.com/uapclaw/uapclaw-go/internal/swarm/schema"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// ──────────────────────────── 非导出函数 ────────────────────────────

// handleBrowserStart 处理 browser.start 请求。stub：返回 NOT_IMPLEMENTED。
func (s *AgentServer) handleBrowserStart(_ context.Context, request *schema.AgentRequest) (*schema.AgentResponse, error) {
	return notImplementedResponse(request)
}

// handleBrowserRuntimeRestart 处理 browser.runtime_restart 请求。stub：返回 NOT_IMPLEMENTED。
func (s *AgentServer) handleBrowserRuntimeRestart(_ context.Context, request *schema.AgentRequest) (*schema.AgentResponse, error) {
	return notImplementedResponse(request)
}

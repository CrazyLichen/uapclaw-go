package server

import (
	"context"

	permrpc "github.com/uapclaw/uapclaw-go/internal/swarm/agents/harness/common/rails/permissions"
	"github.com/uapclaw/uapclaw-go/internal/swarm/schema"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// ──────────────────────────── 非导出函数 ────────────────────────────

// handlePermissionsConfig 处理 permissions.* 请求。统一入口，按 req_method 二次分发。
//
// 对齐 Python: dispatch_permissions_config_request(request) (permissions_config_rpc.py L57-156)
func (s *AgentServer) handlePermissionsConfig(_ context.Context, request *schema.AgentRequest) (*schema.AgentResponse, error) {
	params := permrpc.ParseParams(request.Params)
	ok, payload := permrpc.DispatchPermissionsConfigRequest(string(request.ReqMethod), params)

	respOpts := []schema.AgentResponseOption{schema.WithPayload(payload)}
	if ok {
		respOpts = append(respOpts, schema.WithResponseOK(true))
	} else {
		respOpts = append(respOpts, schema.WithResponseOK(false))
	}
	return schema.NewAgentResponse(request.RequestID, request.ChannelID, respOpts...), nil
}

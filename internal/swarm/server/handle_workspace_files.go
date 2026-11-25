package server

import (
	"github.com/uapclaw/uapclaw-go/internal/swarm/schema"
	"github.com/uapclaw/uapclaw-go/internal/swarm/server/adapter"
)

// handleWorkspaceFiles 文件域请求短路分发：先于 agentRuntime.Invoke 承接
// media/document/path/file 十 method（对齐 Python WorkspaceFileAdapter 在
// GatewayAdapter 链中先于 agent 调用的短路位置，workspace_file_adapter.py:129 handle）。
// 返回 (payload, ok, code, err)：ok=false 时 code 为错误码（BAD_REQUEST 等）。
// Python: gateway_adapter/workspace_file_adapter.py:140-197 _handle 分发顺序。
func (s *AgentServer) handleWorkspaceFiles(method schema.ReqMethod, params map[string]any, sessionID string) (
	map[string]any, bool, string, error,
) {
	if !adapter.HandlesWorkspaceFileMethod(method) {
		return nil, false, "BAD_REQUEST", nil
	}
	return adapter.HandleWorkspaceFileRequest(method, params, sessionID)
}

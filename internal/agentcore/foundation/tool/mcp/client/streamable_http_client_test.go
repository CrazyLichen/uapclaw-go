//go:build llm

package client

import (
	"testing"
)

// TestStreamableHttpClient_真实调用 测试 StreamableHTTP 客户端真实调用。
// 运行方式: go test -tags=llm ./internal/agentcore/foundation/tool/mcp/client/...
func TestStreamableHttpClient_真实调用(t *testing.T) {
	t.Skip("需要真实 MCP 服务器，跳过单元测试")
}

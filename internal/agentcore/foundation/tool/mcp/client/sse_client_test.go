//go:build llm

package client

import (
	"context"
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool/mcp/types"
)

// TestSseClient_真实调用 测试 SSE 客户端真实连接。
// 运行方式: go test -tags=llm ./internal/agentcore/foundation/tool/mcp/client/...
func TestSseClient_真实调用(t *testing.T) {
	t.Skip("需要真实 MCP SSE 服务器，跳过")

	config := types.NewMcpServerConfig("test-server", "http://localhost:8080/sse", "sse")
	client := NewSseClient(config)

	if err := client.Connect(context.Background()); err != nil {
		t.Fatalf("连接失败: %v", err)
	}
	defer client.Close()

	tools, err := client.ListTools(context.Background())
	if err != nil {
		t.Fatalf("列出工具失败: %v", err)
	}
	t.Logf("工具数量: %d", len(tools))
}

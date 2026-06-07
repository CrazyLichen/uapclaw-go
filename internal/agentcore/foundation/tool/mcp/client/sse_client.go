package client

import (
	"context"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool/mcp/types"
)

// ──────────────────────────── 结构体 ────────────────────────────

// SseClient SSE (Server-Sent Events) 传输的 MCP 客户端。
//
// 对应 Python: openjiuwen/core/foundation/tool/mcp/client/sse_client.py (SseClient)
type SseClient struct {
	config      *types.McpServerConfig
	serverName  string
	isConnected bool
}

// ──────────────────────────── 导出函数 ────────────────────────────

// NewSseClient 创建 SSE 客户端。
func NewSseClient(config *types.McpServerConfig) *SseClient {
	return &SseClient{
		config:     config,
		serverName: config.ServerName,
	}
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// Compile-time check: SseClient implements McpClient.
var _ types.McpClient = (*SseClient)(nil)

func (c *SseClient) Connect(_ context.Context, _ ...types.ConnectOption) error {
	// TODO: 实现 SSE 连接
	return nil
}

func (c *SseClient) Disconnect(_ context.Context) error {
	// TODO: 实现 SSE 断开连接
	return nil
}

func (c *SseClient) ListTools(_ context.Context) ([]*types.McpToolCard, error) {
	// TODO: 实现 SSE 列出工具
	return nil, nil
}

func (c *SseClient) CallTool(_ context.Context, _ string, _ map[string]any) (any, error) {
	// TODO: 实现 SSE 调用工具
	return nil, nil
}

func (c *SseClient) GetToolInfo(_ context.Context, _ string) (*types.McpToolCard, error) {
	// TODO: 实现 SSE 获取工具信息
	return nil, nil
}

func (c *SseClient) ListResources(_ context.Context) ([]any, error) {
	// TODO: 实现 SSE 列出资源
	return nil, nil
}

func (c *SseClient) ReadResource(_ context.Context, _ string) (any, error) {
	// TODO: 实现 SSE 读取资源
	return nil, nil
}

func (c *SseClient) Close() error {
	return c.Disconnect(context.Background())
}

package client

import (
	"context"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool/mcp/types"
)

// ──────────────────────────── 结构体 ────────────────────────────

// StreamableHttpClient Streamable HTTP 传输的 MCP 客户端。
//
// 对应 Python: openjiuwen/core/foundation/tool/mcp/client/streamable_http_client.py (StreamableHttpClient)
type StreamableHttpClient struct {
	config      *types.McpServerConfig
	serverName  string
	isConnected bool
}

// ──────────────────────────── 导出函数 ────────────────────────────

// NewStreamableHttpClient 创建 Streamable HTTP 客户端。
func NewStreamableHttpClient(config *types.McpServerConfig) *StreamableHttpClient {
	return &StreamableHttpClient{
		config:     config,
		serverName: config.ServerName,
	}
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// Compile-time check: StreamableHttpClient implements McpClient.
var _ types.McpClient = (*StreamableHttpClient)(nil)

func (c *StreamableHttpClient) Connect(_ context.Context, _ ...types.ConnectOption) error {
	// TODO: 实现 Streamable HTTP 连接
	return nil
}

func (c *StreamableHttpClient) Disconnect(_ context.Context) error {
	// TODO: 实现 Streamable HTTP 断开连接
	return nil
}

func (c *StreamableHttpClient) ListTools(_ context.Context) ([]*types.McpToolCard, error) {
	// TODO: 实现 Streamable HTTP 列出工具
	return nil, nil
}

func (c *StreamableHttpClient) CallTool(_ context.Context, _ string, _ map[string]any) (any, error) {
	// TODO: 实现 Streamable HTTP 调用工具
	return nil, nil
}

func (c *StreamableHttpClient) GetToolInfo(_ context.Context, _ string) (*types.McpToolCard, error) {
	// TODO: 实现 Streamable HTTP 获取工具信息
	return nil, nil
}

func (c *StreamableHttpClient) ListResources(_ context.Context) ([]any, error) {
	// TODO: 实现 Streamable HTTP 列出资源
	return nil, nil
}

func (c *StreamableHttpClient) ReadResource(_ context.Context, _ string) (any, error) {
	// TODO: 实现 Streamable HTTP 读取资源
	return nil, nil
}

func (c *StreamableHttpClient) Close() error {
	return c.Disconnect(context.Background())
}

package client

import (
	"context"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool/mcp/types"
)

// ──────────────────────────── 结构体 ────────────────────────────

// PlaywrightClient Playwright 浏览器工具的 MCP 客户端（SSE/stdio 双传输）。
//
// 对应 Python: openjiuwen/core/foundation/tool/mcp/client/playwright_client.py (PlaywrightClient)
type PlaywrightClient struct {
	config      *types.McpServerConfig
	serverName  string
	isConnected bool
}

// ──────────────────────────── 导出函数 ────────────────────────────

// NewPlaywrightClient 创建 Playwright 客户端。
func NewPlaywrightClient(config *types.McpServerConfig) *PlaywrightClient {
	return &PlaywrightClient{
		config:     config,
		serverName: config.ServerName,
	}
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// Compile-time check: PlaywrightClient implements McpClient.
var _ types.McpClient = (*PlaywrightClient)(nil)

func (c *PlaywrightClient) Connect(_ context.Context, _ ...types.ConnectOption) error {
	// TODO: 实现 Playwright 连接
	return nil
}

func (c *PlaywrightClient) Disconnect(_ context.Context) error {
	// TODO: 实现 Playwright 断开连接
	return nil
}

func (c *PlaywrightClient) ListTools(_ context.Context) ([]*types.McpToolCard, error) {
	// TODO: 实现 Playwright 列出工具
	return nil, nil
}

func (c *PlaywrightClient) CallTool(_ context.Context, _ string, _ map[string]any) (any, error) {
	// TODO: 实现 Playwright 调用工具
	return nil, nil
}

func (c *PlaywrightClient) GetToolInfo(_ context.Context, _ string) (*types.McpToolCard, error) {
	// TODO: 实现 Playwright 获取工具信息
	return nil, nil
}

func (c *PlaywrightClient) ListResources(_ context.Context) ([]any, error) {
	// TODO: 实现 Playwright 列出资源
	return nil, nil
}

func (c *PlaywrightClient) ReadResource(_ context.Context, _ string) (any, error) {
	// TODO: 实现 Playwright 读取资源
	return nil, nil
}

func (c *PlaywrightClient) Close() error {
	return c.Disconnect(context.Background())
}

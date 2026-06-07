package client

import (
	"context"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool/mcp/types"
)

// ──────────────────────────── 结构体 ────────────────────────────

// StdioClient Stdio 子进程传输的 MCP 客户端。
//
// 对应 Python: openjiuwen/core/foundation/tool/mcp/client/stdio_client.py (StdioClient)
type StdioClient struct {
	config      *types.McpServerConfig
	serverName  string
	isConnected bool
}

// ──────────────────────────── 导出函数 ────────────────────────────

// NewStdioClient 创建 Stdio 客户端。
func NewStdioClient(config *types.McpServerConfig) *StdioClient {
	return &StdioClient{
		config:     config,
		serverName: config.ServerName,
	}
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// Compile-time check: StdioClient implements McpClient.
var _ types.McpClient = (*StdioClient)(nil)

func (c *StdioClient) Connect(_ context.Context, _ ...types.ConnectOption) error {
	// TODO: 实现 Stdio 连接
	return nil
}

func (c *StdioClient) Disconnect(_ context.Context) error {
	// TODO: 实现 Stdio 断开连接
	return nil
}

func (c *StdioClient) ListTools(_ context.Context) ([]*types.McpToolCard, error) {
	// TODO: 实现 Stdio 列出工具
	return nil, nil
}

func (c *StdioClient) CallTool(_ context.Context, _ string, _ map[string]any) (any, error) {
	// TODO: 实现 Stdio 调用工具
	return nil, nil
}

func (c *StdioClient) GetToolInfo(_ context.Context, _ string) (*types.McpToolCard, error) {
	// TODO: 实现 Stdio 获取工具信息
	return nil, nil
}

func (c *StdioClient) ListResources(_ context.Context) ([]any, error) {
	// TODO: 实现 Stdio 列出资源
	return nil, nil
}

func (c *StdioClient) ReadResource(_ context.Context, _ string) (any, error) {
	// TODO: 实现 Stdio 读取资源
	return nil, nil
}

func (c *StdioClient) Close() error {
	return c.Disconnect(context.Background())
}

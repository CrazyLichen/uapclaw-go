package client

import (
	"context"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool/mcp/types"
)

// ──────────────────────────── 结构体 ────────────────────────────

// OpenApiClient OpenAPI 规格解析的 MCP 客户端。
//
// 对应 Python: openjiuwen/core/foundation/tool/mcp/client/openapi_client.py (OpenApiClient)
type OpenApiClient struct {
	config      *types.McpServerConfig
	serverName  string
	isConnected bool
}

// ──────────────────────────── 导出函数 ────────────────────────────

// NewOpenApiClient 创建 OpenAPI 客户端。
func NewOpenApiClient(config *types.McpServerConfig) *OpenApiClient {
	return &OpenApiClient{
		config:     config,
		serverName: config.ServerName,
	}
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// Compile-time check: OpenApiClient implements McpClient.
var _ types.McpClient = (*OpenApiClient)(nil)

func (c *OpenApiClient) Connect(_ context.Context, _ ...types.ConnectOption) error {
	// TODO: 实现 OpenAPI 连接
	return nil
}

func (c *OpenApiClient) Disconnect(_ context.Context) error {
	// TODO: 实现 OpenAPI 断开连接
	return nil
}

func (c *OpenApiClient) ListTools(_ context.Context) ([]*types.McpToolCard, error) {
	// TODO: 实现 OpenAPI 列出工具
	return nil, nil
}

func (c *OpenApiClient) CallTool(_ context.Context, _ string, _ map[string]any) (any, error) {
	// TODO: 实现 OpenAPI 调用工具
	return nil, nil
}

func (c *OpenApiClient) GetToolInfo(_ context.Context, _ string) (*types.McpToolCard, error) {
	// TODO: 实现 OpenAPI 获取工具信息
	return nil, nil
}

func (c *OpenApiClient) ListResources(_ context.Context) ([]any, error) {
	// TODO: 实现 OpenAPI 列出资源
	return nil, nil
}

func (c *OpenApiClient) ReadResource(_ context.Context, _ string) (any, error) {
	// TODO: 实现 OpenAPI 读取资源
	return nil, nil
}

func (c *OpenApiClient) Close() error {
	return c.Disconnect(context.Background())
}

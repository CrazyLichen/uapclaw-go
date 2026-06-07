package client

import (
	"context"
	"fmt"
	"strings"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool/mcp/types"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// PlaywrightClient Playwright 浏览器工具的 MCP 客户端（SSE/Stdio 双传输）。
//
// 根据 ServerPath 类型委托给 SseClient 或 StdioClient：
//   - http:// 或 https:// 开头 → SseClient
//   - 其他 → StdioClient
//
// 对应 Python: openjiuwen/core/foundation/tool/mcp/client/playwright_client.py (PlaywrightClient)
type PlaywrightClient struct {
	config     *types.McpServerConfig
	serverName string
	delegate   types.McpClient // SSE 或 Stdio 客户端
}

// ──────────────────────────── 全局变量 ────────────────────────────

// 编译期检查：PlaywrightClient 实现 McpClient 接口
var _ types.McpClient = (*PlaywrightClient)(nil)

// ──────────────────────────── 导出函数 ────────────────────────────

// NewPlaywrightClient 创建 Playwright 客户端。
func NewPlaywrightClient(config *types.McpServerConfig) *PlaywrightClient {
	return &PlaywrightClient{
		config:     config,
		serverName: config.ServerName,
	}
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// Connect 根据传输类型创建委托客户端并建立连接。
func (c *PlaywrightClient) Connect(ctx context.Context, opts ...types.ConnectOption) error {
	if strings.HasPrefix(c.config.ServerPath, "http://") || strings.HasPrefix(c.config.ServerPath, "https://") {
		logger.Info(logger.ComponentAgentCore).
			Str("server_name", c.serverName).
			Str("server_path", c.config.ServerPath).
			Msg("Playwright 选择 SSE 传输")
		c.delegate = NewSseClient(c.config)
	} else {
		logger.Info(logger.ComponentAgentCore).
			Str("server_name", c.serverName).
			Str("server_path", c.config.ServerPath).
			Msg("Playwright 选择 Stdio 传输")
		c.delegate = NewStdioClient(c.config)
	}

	if err := c.delegate.Connect(ctx, opts...); err != nil {
		logger.Error(logger.ComponentAgentCore).
			Str("server_name", c.serverName).
			Err(err).
			Msg("Playwright 连接失败")
		return err
	}

	logger.Info(logger.ComponentAgentCore).
		Str("server_name", c.serverName).
		Msg("Playwright 客户端连接成功")
	return nil
}

// Disconnect 断开 Playwright 连接（委托给 delegate）。
func (c *PlaywrightClient) Disconnect(ctx context.Context) error {
	if c.delegate == nil {
		return nil
	}
	if err := c.delegate.Disconnect(ctx); err != nil {
		logger.Error(logger.ComponentAgentCore).
			Str("server_name", c.serverName).
			Err(err).
			Msg("Playwright 断开连接失败")
		return err
	}
	logger.Info(logger.ComponentAgentCore).
		Str("server_name", c.serverName).
		Msg("Playwright 客户端已断开连接")
	return nil
}

// ListTools 列出 Playwright 服务器提供的工具（委托给 delegate）。
func (c *PlaywrightClient) ListTools(ctx context.Context) ([]*types.McpToolCard, error) {
	if c.delegate == nil {
		return nil, fmt.Errorf("playwright client not initialized for server %q", c.serverName)
	}
	return c.delegate.ListTools(ctx)
}

// CallTool 调用指定工具（委托给 delegate）。
func (c *PlaywrightClient) CallTool(ctx context.Context, toolName string, arguments map[string]any) (any, error) {
	if c.delegate == nil {
		return nil, fmt.Errorf("playwright client not initialized for server %q", c.serverName)
	}
	return c.delegate.CallTool(ctx, toolName, arguments)
}

// GetToolInfo 获取指定工具信息（委托给 delegate）。
func (c *PlaywrightClient) GetToolInfo(ctx context.Context, toolName string) (*types.McpToolCard, error) {
	if c.delegate == nil {
		return nil, fmt.Errorf("playwright client not initialized for server %q", c.serverName)
	}
	return c.delegate.GetToolInfo(ctx, toolName)
}

// ListResources 列出资源（委托给 delegate）。
func (c *PlaywrightClient) ListResources(ctx context.Context) ([]any, error) {
	if c.delegate == nil {
		return nil, fmt.Errorf("playwright client not initialized for server %q", c.serverName)
	}
	return c.delegate.ListResources(ctx)
}

// ReadResource 读取资源（委托给 delegate）。
func (c *PlaywrightClient) ReadResource(ctx context.Context, uri string) (any, error) {
	if c.delegate == nil {
		return nil, fmt.Errorf("playwright client not initialized for server %q", c.serverName)
	}
	return c.delegate.ReadResource(ctx, uri)
}

// Close 关闭客户端（等价于 Disconnect）。
func (c *PlaywrightClient) Close() error {
	return c.Disconnect(context.Background())
}

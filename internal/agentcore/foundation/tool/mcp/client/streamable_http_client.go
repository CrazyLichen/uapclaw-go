package client

import (
	"context"
	"fmt"

	mcpclient "github.com/mark3labs/mcp-go/client"
	mcptransport "github.com/mark3labs/mcp-go/client/transport"
	mcpcore "github.com/mark3labs/mcp-go/mcp"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool/mcp/types"
	"github.com/uapclaw/uapclaw-go/internal/common/exception"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// StreamableHttpClient Streamable HTTP 传输的 MCP 客户端。
//
// 对应 Python: openjiuwen/core/foundation/tool/mcp/client/streamable_http_client.py (StreamableHttpClient)
type StreamableHttpClient struct {
	config      *types.McpServerConfig
	serverName  string
	client      *mcpclient.Client
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

// Connect 建立 Streamable HTTP 连接。
func (c *StreamableHttpClient) Connect(ctx context.Context, opts ...types.ConnectOption) error {
	_ = types.NewConnectOptions(opts...)

	// 构建 transport 选项
	transportOpts := make([]mcptransport.StreamableHTTPCOption, 0)
	if len(c.config.AuthHeaders) > 0 {
		transportOpts = append(transportOpts, mcptransport.WithHTTPHeaders(c.config.AuthHeaders))
	}
	// ⤵️ 3.11 回填 TOOL_AUTH 回调：此处预留动态认证头注入

	// 创建 Streamable HTTP 客户端
	mcpCli, err := mcpclient.NewStreamableHttpClient(c.config.ServerPath, transportOpts...)
	if err != nil {
		logger.Error(logger.ComponentAgentCore).
			Str("event_type", "LLM_CALL_ERROR").
			Str("server_name", c.serverName).
			Str("server_path", c.config.ServerPath).
			Err(err).
			Msg("StreamableHTTP 客户端创建失败")
		return exception.BuildError(
			exception.StatusToolMcpNotConnected,
			exception.WithParam("server_name", c.serverName),
		)
	}

	// 启动连接
	if err := mcpCli.Start(ctx); err != nil {
		logger.Error(logger.ComponentAgentCore).
			Str("event_type", "LLM_CALL_ERROR").
			Str("server_name", c.serverName).
			Str("server_path", c.config.ServerPath).
			Err(err).
			Msg("StreamableHTTP 启动连接失败")
		return exception.BuildError(
			exception.StatusToolMcpNotConnected,
			exception.WithParam("server_name", c.serverName),
		)
	}

	// 初始化会话
	initReq := mcpcore.InitializeRequest{
		Params: mcpcore.InitializeParams{
			ProtocolVersion: mcpProtocolVersion,
			ClientInfo: mcpcore.Implementation{
				Name:    mcpClientName,
				Version: mcpClientVersion,
			},
		},
	}

	if _, err := mcpCli.Initialize(ctx, initReq); err != nil {
		logger.Error(logger.ComponentAgentCore).
			Str("event_type", "LLM_CALL_ERROR").
			Str("server_name", c.serverName).
			Str("server_path", c.config.ServerPath).
			Err(err).
			Msg("StreamableHTTP 初始化会话失败")
		_ = mcpCli.Close()
		return exception.BuildError(
			exception.StatusToolMcpNotConnected,
			exception.WithParam("server_name", c.serverName),
		)
	}

	c.client = mcpCli
	c.isConnected = true
	logger.Info(logger.ComponentAgentCore).
		Str("server_name", c.serverName).
		Str("server_path", c.config.ServerPath).
		Msg("StreamableHTTP 客户端连接成功")
	return nil
}

// Disconnect 断开 Streamable HTTP 连接。
func (c *StreamableHttpClient) Disconnect(_ context.Context) error {
	if c.client != nil {
		if err := c.client.Close(); err != nil {
			logger.Error(logger.ComponentAgentCore).
				Str("server_name", c.serverName).
				Err(err).
				Msg("StreamableHTTP 断开连接失败")
			return err
		}
		c.client = nil
	}
	c.isConnected = false
	logger.Info(logger.ComponentAgentCore).
		Str("server_name", c.serverName).
		Msg("StreamableHTTP 客户端已断开连接")
	return nil
}

// ListTools 列出 MCP 服务器提供的工具。
func (c *StreamableHttpClient) ListTools(ctx context.Context) ([]*types.McpToolCard, error) {
	if !c.isConnected || c.client == nil {
		return nil, exception.BuildError(
			exception.StatusToolMcpNotConnected,
			exception.WithParam("server_name", c.serverName),
		)
	}

	result, err := c.client.ListTools(ctx, mcpcore.ListToolsRequest{})
	if err != nil {
		logger.Error(logger.ComponentAgentCore).
			Str("event_type", "LLM_CALL_ERROR").
			Str("server_name", c.serverName).
			Err(err).
			Msg("StreamableHTTP 列出工具失败")
		return nil, err
	}

	cards := make([]*types.McpToolCard, 0, len(result.Tools))
	for _, tool := range result.Tools {
		inputParams := jsonSchemaToParams(tool.InputSchema)
		card := types.NewMcpToolCard(
			tool.Name,
			tool.Description,
			c.serverName,
			inputParams,
			nil,
			types.WithMcpToolCardServerID(c.config.ServerID),
		)
		cards = append(cards, card)
	}
	logger.Info(logger.ComponentAgentCore).
		Str("server_name", c.serverName).
		Int("tool_count", len(cards)).
		Msg("StreamableHTTP 获取工具列表成功")
	return cards, nil
}

// CallTool 调用指定工具。
func (c *StreamableHttpClient) CallTool(ctx context.Context, toolName string, arguments map[string]any) (any, error) {
	if !c.isConnected || c.client == nil {
		return nil, exception.BuildError(
			exception.StatusToolMcpNotConnected,
			exception.WithParam("server_name", c.serverName),
		)
	}

	logger.Info(logger.ComponentAgentCore).
		Str("server_name", c.serverName).
		Str("tool_name", toolName).
		Msg("StreamableHTTP 调用工具")

	result, err := c.client.CallTool(ctx, mcpcore.CallToolRequest{
		Params: mcpcore.CallToolParams{
			Name:      toolName,
			Arguments: arguments,
		},
	})
	if err != nil {
		logger.Error(logger.ComponentAgentCore).
			Str("event_type", "LLM_CALL_ERROR").
			Str("server_name", c.serverName).
			Str("tool_name", toolName).
			Err(err).
			Msg("StreamableHTTP 调用工具失败")
		return nil, err
	}

	logger.Info(logger.ComponentAgentCore).
		Str("server_name", c.serverName).
		Str("tool_name", toolName).
		Msg("StreamableHTTP 工具调用完成")
	return callToolResultToMap(result), nil
}

// GetToolInfo 获取指定工具信息。
func (c *StreamableHttpClient) GetToolInfo(ctx context.Context, toolName string) (*types.McpToolCard, error) {
	tools, err := c.ListTools(ctx)
	if err != nil {
		return nil, err
	}
	for _, card := range tools {
		if card.Name == toolName {
			logger.Debug(logger.ComponentAgentCore).
				Str("server_name", c.serverName).
				Str("tool_name", toolName).
				Msg("StreamableHTTP 找到工具信息")
			return card, nil
		}
	}
	logger.Warn(logger.ComponentAgentCore).
		Str("server_name", c.serverName).
		Str("tool_name", toolName).
		Msg("StreamableHTTP 未找到工具")
	return nil, fmt.Errorf("tool %q not found in server %q", toolName, c.serverName)
}

// ListResources 列出 MCP 服务器提供的资源。
func (c *StreamableHttpClient) ListResources(ctx context.Context) ([]any, error) {
	if !c.isConnected || c.client == nil {
		return nil, exception.BuildError(
			exception.StatusToolMcpNotConnected,
			exception.WithParam("server_name", c.serverName),
		)
	}

	result, err := c.client.ListResources(ctx, mcpcore.ListResourcesRequest{})
	if err != nil {
		logger.Error(logger.ComponentAgentCore).
			Str("event_type", "LLM_CALL_ERROR").
			Str("server_name", c.serverName).
			Err(err).
			Msg("StreamableHTTP 列出资源失败")
		return nil, err
	}

	resources := make([]any, 0, len(result.Resources))
	for _, res := range result.Resources {
		resources = append(resources, res)
	}
	return resources, nil
}

// ReadResource 读取指定资源。
func (c *StreamableHttpClient) ReadResource(ctx context.Context, uri string) (any, error) {
	if !c.isConnected || c.client == nil {
		return nil, exception.BuildError(
			exception.StatusToolMcpNotConnected,
			exception.WithParam("server_name", c.serverName),
		)
	}

	result, err := c.client.ReadResource(ctx, mcpcore.ReadResourceRequest{
		Params: mcpcore.ReadResourceParams{
			URI: uri,
		},
	})
	if err != nil {
		logger.Error(logger.ComponentAgentCore).
			Str("event_type", "LLM_CALL_ERROR").
			Str("server_name", c.serverName).
			Str("uri", uri).
			Err(err).
			Msg("StreamableHTTP 读取资源失败")
		return nil, err
	}
	return result, nil
}

// Close 关闭客户端（等价于 Disconnect）。
func (c *StreamableHttpClient) Close() error {
	return c.Disconnect(context.Background())
}

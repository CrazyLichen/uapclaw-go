package client

import (
	"context"
	"crypto/tls"
	"fmt"
	"net/http"
	"time"

	mcpclient "github.com/mark3labs/mcp-go/client"
	mcptransport "github.com/mark3labs/mcp-go/client/transport"
	mcpcore "github.com/mark3labs/mcp-go/mcp"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool/auth"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool/mcp/types"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/runner/callback"
	"github.com/uapclaw/uapclaw-go/internal/common/exception"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// StreamableHttpClient Streamable HTTP 传输的 MCP 客户端。
//
// 对应 Python: openjiuwen/core/foundation/tool/mcp/client/streamable_http_client.py (StreamableHttpClient)
type StreamableHttpClient struct {
	// config MCP 服务器配置
	config *types.McpServerConfig
	// serverName 服务器名称
	serverName string
	// client mcp-go 的客户端实例
	client *mcpclient.Client
	// isConnected 连接状态
	isConnected bool
}

// ──────────────────────────── 全局变量 ────────────────────────────

// 编译期检查：StreamableHttpClient 实现 McpClient 接口
var _ types.McpClient = (*StreamableHttpClient)(nil)

// ──────────────────────────── 导出函数 ────────────────────────────

// NewStreamableHttpClient 创建 Streamable HTTP 客户端。
func NewStreamableHttpClient(config *types.McpServerConfig) *StreamableHttpClient {
	return &StreamableHttpClient{
		config:     config,
		serverName: config.ServerName,
	}
}

// Connect 建立 Streamable HTTP 连接。
func (c *StreamableHttpClient) Connect(ctx context.Context, opts ...types.ConnectOption) error {
	connectOpts := types.NewConnectOptions(opts...)

	// 如果设置了超时，创建带超时的 context
	if connectOpts.Timeout > 0 && connectOpts.Timeout != types.NoTimeout {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, time.Duration(connectOpts.Timeout*float64(time.Second)))
		defer cancel()
	}

	// 构建 transport 选项
	transportOpts := make([]mcptransport.StreamableHTTPCOption, 0)
	if len(c.config.AuthHeaders) > 0 {
		transportOpts = append(transportOpts, mcptransport.WithHTTPHeaders(c.config.AuthHeaders))
	}

	// 触发 TOOL_AUTH 回调获取认证信息（3.11 回填）
	results := callback.GetCallbackFramework().TriggerTool(ctx, &callback.ToolCallEventData{
		Event:    callback.ToolAuth,
		ToolName: c.serverName,
		ToolID:   c.config.ServerID,
		Extra: map[string]any{
			"auth_config": &auth.ToolAuthConfig{
				AuthType: auth.AuthTypeHeaderAndQuery,
				Config: map[string]any{
					"auth_headers":      c.config.AuthHeaders,
					"auth_query_params": c.config.AuthQueryParams,
				},
				ToolType: c.serverName,
				ToolID:   c.config.ServerID,
			},
		},
	})

	// 从 results 中提取 *auth.ToolAuthResult → auth_provider 和 tls_config
	// 对照 Python: 逆序遍历，取最后一个 Success=true 的结果
	var provider *auth.HeaderQueryProvider
	var tlsConfig *tls.Config
	var authSuccessCount int
	var authFailCount int
	for i := len(results) - 1; i >= 0; i-- {
		authResult, ok := results[i].(*auth.ToolAuthResult)
		if !ok {
			continue
		}
		if !authResult.Success {
			authFailCount++
			continue
		}
		authSuccessCount++
		if p, ok := authResult.AuthData["auth_provider"].(*auth.HeaderQueryProvider); ok && provider == nil {
			provider = p
		}
		if tc, ok := authResult.AuthData["tls_config"].(*tls.Config); ok && tc != nil && tlsConfig == nil {
			tlsConfig = tc
		}
	}
	// 所有认证结果均失败时记录 Warn 日志
	if authSuccessCount == 0 && authFailCount > 0 {
		logger.Warn(logger.ComponentAgentCore).
			Str("server_name", c.serverName).
			Int("auth_fail_count", authFailCount).
			Msg("StreamableHTTP 客户端所有认证结果均失败，将以无认证模式连接")
	}

	// 如果有 TLS 配置，构建自定义 HTTP 客户端
	if tlsConfig != nil {
		transportOpts = append(transportOpts, mcptransport.WithHTTPBasicClient(&http.Client{
			Transport: &http.Transport{
				TLSClientConfig: tlsConfig,
			},
		}))
	}

	// 如果设置了超时，传递到传输层
	if connectOpts.Timeout > 0 && connectOpts.Timeout != types.NoTimeout {
		timeoutDur := time.Duration(connectOpts.Timeout * float64(time.Second))
		transportOpts = append(transportOpts, mcptransport.WithHTTPTimeout(timeoutDur))
	}

	// 将 provider 的 headers 合并到 config.AuthHeaders，一次性传入 transport 选项
	if provider != nil && len(provider.Headers) > 0 {
		mergedHeaders := make(map[string]string)
		for k, v := range c.config.AuthHeaders {
			mergedHeaders[k] = v
		}
		for k, v := range provider.Headers {
			mergedHeaders[k] = v
		}
		transportOpts = append(transportOpts, mcptransport.WithHTTPHeaders(mergedHeaders))
	}

	// 将 provider 的 QueryParams 合并到 ServerPath URL 中
	// 对照 Python: AuthHeaderAndQueryProvider.async_auth_flow 中 copy_merge_params
	effectivePath := c.config.ServerPath
	if provider != nil && len(provider.QueryParams) > 0 {
		mergedQueryParams := make(map[string]string)
		for k, v := range c.config.AuthQueryParams {
			mergedQueryParams[k] = v
		}
		for k, v := range provider.QueryParams {
			mergedQueryParams[k] = v
		}
		var mergeErr error
		effectivePath, mergeErr = mergeQueryParams(effectivePath, mergedQueryParams)
		if mergeErr != nil {
			return exception.BuildError(
				exception.StatusToolMcpExecutionError,
				exception.WithParam("method", "Connect"),
				exception.WithParam("reason", fmt.Sprintf("合并查询参数失败: %v", mergeErr)),
				exception.WithParam("card", c.serverName),
			)
		}
		logger.Debug(logger.ComponentAgentCore).
			Str("server_name", c.serverName).
			Int("query_param_count", len(mergedQueryParams)).
			Msg("StreamableHTTP 客户端注入认证查询参数")
	}

	// 创建 Streamable HTTP 客户端
	mcpCli, err := mcpclient.NewStreamableHttpClient(effectivePath, transportOpts...)
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
		// 启动失败时关闭底层连接，防止资源泄漏
		_ = mcpCli.Close()
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
	if !c.isConnected {
		return nil
	}
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
	if !c.isConnected || c.client == nil {
		return nil, exception.BuildError(
			exception.StatusToolMcpNotConnected,
			exception.WithParam("server_name", c.serverName),
		)
	}
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
	return nil, exception.BuildError(
		exception.StatusToolMcpExecutionError,
		exception.WithParam("method", "GetToolInfo"),
		exception.WithParam("reason", fmt.Sprintf("tool %q not found in server %q", toolName, c.serverName)),
		exception.WithParam("card", c.serverName),
	)
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

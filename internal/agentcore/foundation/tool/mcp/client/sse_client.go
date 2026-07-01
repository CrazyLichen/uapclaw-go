package client

import (
	"context"
	"crypto/tls"
	"fmt"
	"net/http"
	"time"

	mcpclient "github.com/mark3labs/mcp-go/client"
	mcptransport "github.com/mark3labs/mcp-go/client/transport"
	mcp "github.com/mark3labs/mcp-go/mcp"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool/auth"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool/mcp/types"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/runner/callback"
	"github.com/uapclaw/uapclaw-go/internal/common/exception"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// SseClient SSE (Server-Sent Events) 传输的 MCP 客户端。
//
// 内部组合 mcp-go 的 SSE 客户端，实现 McpClient 接口。
//
// 对应 Python: openjiuwen/core/foundation/tool/mcp/client/sse_client.py (SseClient)
type SseClient struct {
	// config MCP 服务器配置
	config *types.McpServerConfig
	// serverName 服务器名称
	serverName string
	// isConnected 连接状态
	isConnected bool
	// mcpClient mcp-go 的 SSE 客户端
	mcpClient *mcpclient.Client
}

// ──────────────────────────── 常量 ────────────────────────────
const (
	// mcpProtocolVersion MCP 协议版本
	mcpProtocolVersion = "2025-03-26"
	// mcpClientName MCP 客户端名称
	mcpClientName = "uapclaw-go"
	// mcpClientVersion MCP 客户端版本
	mcpClientVersion = "1.0.0"
)

// ──────────────────────────── 全局变量 ────────────────────────────

// 编译期检查：SseClient 实现 McpClient 接口
var _ types.McpClient = (*SseClient)(nil)

// ──────────────────────────── 导出函数 ────────────────────────────

// NewSseClient 创建 SSE 客户端。
func NewSseClient(config *types.McpServerConfig) *SseClient {
	return &SseClient{
		config:     config,
		serverName: config.ServerName,
	}
}

// Connect 建立 SSE 连接，启动传输并初始化会话。
func (c *SseClient) Connect(ctx context.Context, opts ...types.ConnectOption) error {
	connectOpts := types.NewConnectOptions(opts...)

	// 如果设置了超时，创建带超时的 context
	if connectOpts.Timeout > 0 && connectOpts.Timeout != types.NoTimeout {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, time.Duration(connectOpts.Timeout*float64(time.Second)))
		defer cancel()
	}

	// 构建传输选项
	var transportOpts []mcptransport.ClientOption
	if len(c.config.AuthHeaders) > 0 {
		transportOpts = append(transportOpts, mcptransport.WithHeaders(c.config.AuthHeaders))
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
			Msg("SSE 客户端所有认证结果均失败，将以无认证模式连接")
	}

	// 判断是否需要构建自定义 HTTP 客户端（TLS 配置和超时合并到同一个客户端）
	hasTimeout := connectOpts.Timeout > 0 && connectOpts.Timeout != types.NoTimeout
	if tlsConfig != nil {
		// 有 TLS 配置，构建自定义 HTTP 客户端
		httpClient := &http.Client{
			Transport: &http.Transport{
				TLSClientConfig: tlsConfig,
			},
		}
		if hasTimeout {
			// 同时有超时，设置到 HTTP 客户端
			httpClient.Timeout = time.Duration(connectOpts.Timeout * float64(time.Second))
		}
		transportOpts = append(transportOpts, mcptransport.WithHTTPClient(httpClient))
		// TLS 场景下也传递 endpoint/response timeout，让传输层内部超时逻辑生效
		if hasTimeout {
			timeoutDur := time.Duration(connectOpts.Timeout * float64(time.Second))
			transportOpts = append(transportOpts, mcptransport.WithEndpointTimeout(timeoutDur))
			transportOpts = append(transportOpts, mcptransport.WithResponseTimeout(timeoutDur))
		}
	} else if hasTimeout {
		// 仅超时，无 TLS，使用 endpoint/response timeout 传递到传输层
		timeoutDur := time.Duration(connectOpts.Timeout * float64(time.Second))
		transportOpts = append(transportOpts, mcptransport.WithEndpointTimeout(timeoutDur))
		transportOpts = append(transportOpts, mcptransport.WithResponseTimeout(timeoutDur))
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
		transportOpts = append(transportOpts, mcptransport.WithHeaders(mergedHeaders))
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
			Msg("SSE 客户端注入认证查询参数")
	}

	// 创建 SSE 客户端
	client, err := mcpclient.NewSSEMCPClient(effectivePath, transportOpts...)
	if err != nil {
		logger.Error(logger.ComponentAgentCore).
			Err(err).
			Str("server_path", c.config.ServerPath).
			Str("server_name", c.serverName).
			Msg("SSE 客户端创建失败")
		return exception.BuildError(
			exception.StatusToolMcpExecutionError,
			exception.WithParam("method", "Connect"),
			exception.WithParam("reason", fmt.Sprintf("创建 SSE 客户端失败: %v", err)),
			exception.WithParam("card", c.serverName),
		)
	}

	// 启动传输
	if err := client.Start(ctx); err != nil {
		logger.Error(logger.ComponentAgentCore).
			Err(err).
			Str("server_path", c.config.ServerPath).
			Str("server_name", c.serverName).
			Msg("SSE 客户端启动失败")
		// 启动失败时关闭底层连接，防止资源泄漏
		_ = client.Close()
		return exception.BuildError(
			exception.StatusToolMcpExecutionError,
			exception.WithParam("method", "Connect"),
			exception.WithParam("reason", fmt.Sprintf("启动 SSE 连接失败: %v", err)),
			exception.WithParam("card", c.serverName),
		)
	}

	// 初始化会话
	initReq := mcp.InitializeRequest{
		Params: mcp.InitializeParams{
			ProtocolVersion: mcpProtocolVersion,
			ClientInfo: mcp.Implementation{
				Name:    mcpClientName,
				Version: mcpClientVersion,
			},
		},
	}
	if _, err := client.Initialize(ctx, initReq); err != nil {
		logger.Error(logger.ComponentAgentCore).
			Err(err).
			Str("server_path", c.config.ServerPath).
			Str("server_name", c.serverName).
			Msg("SSE 客户端初始化会话失败")
		// 初始化失败时关闭连接
		_ = client.Close()
		return exception.BuildError(
			exception.StatusToolMcpExecutionError,
			exception.WithParam("method", "Connect"),
			exception.WithParam("reason", fmt.Sprintf("初始化会话失败: %v", err)),
			exception.WithParam("card", c.serverName),
		)
	}

	c.mcpClient = client
	c.isConnected = true

	logger.Info(logger.ComponentAgentCore).
		Str("server_path", c.config.ServerPath).
		Str("server_name", c.serverName).
		Msg("SSE 客户端连接成功")

	return nil
}

// Disconnect 断开 SSE 连接。
func (c *SseClient) Disconnect(_ context.Context) error {
	if !c.isConnected {
		return nil
	}
	if c.mcpClient != nil {
		if err := c.mcpClient.Close(); err != nil {
			logger.Error(logger.ComponentAgentCore).
				Err(err).
				Str("server_name", c.serverName).
				Msg("SSE 客户端断开连接失败")
			return err
		}
	}
	c.isConnected = false
	c.mcpClient = nil
	logger.Info(logger.ComponentAgentCore).
		Str("server_name", c.serverName).
		Msg("SSE 客户端已断开连接")
	return nil
}

// ListTools 列出服务器提供的工具。
func (c *SseClient) ListTools(ctx context.Context) ([]*types.McpToolCard, error) {
	if !c.isConnected {
		return nil, exception.BuildError(
			exception.StatusToolMcpNotConnected,
			exception.WithParam("server_name", c.serverName),
		)
	}

	resp, err := c.mcpClient.ListTools(ctx, mcp.ListToolsRequest{})
	if err != nil {
		logger.Error(logger.ComponentAgentCore).
			Err(err).
			Str("server_name", c.serverName).
			Msg("SSE 客户端列出工具失败")
		return nil, exception.BuildError(
			exception.StatusToolMcpExecutionError,
			exception.WithParam("method", "ListTools"),
			exception.WithParam("reason", err.Error()),
			exception.WithParam("card", c.serverName),
		)
	}

	tools := make([]*types.McpToolCard, 0, len(resp.Tools))
	for _, t := range resp.Tools {
		tools = append(tools, types.NewMcpToolCard(
			t.Name,
			t.Description,
			c.serverName,
			jsonSchemaToParams(t.InputSchema),
			types.WithMcpToolCardServerID(c.config.ServerID),
		))
	}
	return tools, nil
}

// CallTool 调用指定工具。
func (c *SseClient) CallTool(ctx context.Context, toolName string, arguments map[string]any) (any, error) {
	if !c.isConnected {
		return nil, exception.BuildError(
			exception.StatusToolMcpNotConnected,
			exception.WithParam("server_name", c.serverName),
		)
	}

	resp, err := c.mcpClient.CallTool(ctx, mcp.CallToolRequest{
		Params: mcp.CallToolParams{
			Name:      toolName,
			Arguments: arguments,
		},
	})
	if err != nil {
		logger.Error(logger.ComponentAgentCore).
			Err(err).
			Str("server_name", c.serverName).
			Str("tool_name", toolName).
			Msg("SSE 客户端调用工具失败")
		return nil, exception.BuildError(
			exception.StatusToolMcpExecutionError,
			exception.WithParam("method", "CallTool"),
			exception.WithParam("reason", err.Error()),
			exception.WithParam("card", toolName),
		)
	}

	return callToolResultToMap(resp), nil
}

// GetToolInfo 获取指定工具信息。
func (c *SseClient) GetToolInfo(ctx context.Context, toolName string) (*types.McpToolCard, error) {
	if !c.isConnected {
		return nil, exception.BuildError(
			exception.StatusToolMcpNotConnected,
			exception.WithParam("server_name", c.serverName),
		)
	}

	tools, err := c.ListTools(ctx)
	if err != nil {
		return nil, err
	}

	for _, t := range tools {
		if t.Name == toolName {
			return t, nil
		}
	}

	return nil, exception.BuildError(
		exception.StatusToolMcpExecutionError,
		exception.WithParam("method", "GetToolInfo"),
		exception.WithParam("reason", fmt.Sprintf("工具 %s 不存在", toolName)),
		exception.WithParam("card", toolName),
	)
}

// ListResources 列出服务器提供的资源。
func (c *SseClient) ListResources(ctx context.Context) ([]any, error) {
	if !c.isConnected {
		return nil, exception.BuildError(
			exception.StatusToolMcpNotConnected,
			exception.WithParam("server_name", c.serverName),
		)
	}

	resp, err := c.mcpClient.ListResources(ctx, mcp.ListResourcesRequest{})
	if err != nil {
		logger.Error(logger.ComponentAgentCore).
			Err(err).
			Str("server_name", c.serverName).
			Msg("SSE 客户端列出资源失败")
		return nil, exception.BuildError(
			exception.StatusToolMcpExecutionError,
			exception.WithParam("method", "ListResources"),
			exception.WithParam("reason", err.Error()),
			exception.WithParam("card", c.serverName),
		)
	}

	result := make([]any, 0, len(resp.Resources))
	for _, r := range resp.Resources {
		result = append(result, r)
	}
	return result, nil
}

// ReadResource 读取指定资源。
func (c *SseClient) ReadResource(ctx context.Context, uri string) (any, error) {
	if !c.isConnected {
		return nil, exception.BuildError(
			exception.StatusToolMcpNotConnected,
			exception.WithParam("server_name", c.serverName),
		)
	}

	resp, err := c.mcpClient.ReadResource(ctx, mcp.ReadResourceRequest{
		Params: mcp.ReadResourceParams{
			URI: uri,
		},
	})
	if err != nil {
		logger.Error(logger.ComponentAgentCore).
			Err(err).
			Str("server_name", c.serverName).
			Str("uri", uri).
			Msg("SSE 客户端读取资源失败")
		return nil, exception.BuildError(
			exception.StatusToolMcpExecutionError,
			exception.WithParam("method", "ReadResource"),
			exception.WithParam("reason", err.Error()),
			exception.WithParam("card", c.serverName),
		)
	}

	return resp, nil
}

// Close 关闭客户端（等价于 Disconnect）。
func (c *SseClient) Close() error {
	return c.Disconnect(context.Background())
}

package client

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"time"

	mcpclient "github.com/mark3labs/mcp-go/client"
	mcptransport "github.com/mark3labs/mcp-go/client/transport"
	mcp "github.com/mark3labs/mcp-go/mcp"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool/mcp/types"
	"github.com/uapclaw/uapclaw-go/internal/common/exception"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// StdioClient Stdio 子进程传输的 MCP 客户端。
// 通过 stdin/stdout 与子进程通信，组合 mcp-go 的 Client 实现 MCP 协议交互。
//
// 对应 Python: openjiuwen/core/foundation/tool/mcp/client/stdio_client.py (StdioClient)
type StdioClient struct {
	// config MCP 服务器配置
	config *types.McpServerConfig
	// serverName 服务器名称
	serverName string
	// isConnected 连接状态
	isConnected bool
	// mcpClient mcp-go 底层客户端
	mcpClient *mcpclient.Client
}

// ──────────────────────────── 全局变量 ────────────────────────────

// 编译期检查：StdioClient 实现 McpClient 接口
var _ types.McpClient = (*StdioClient)(nil)

// ──────────────────────────── 导出函数 ────────────────────────────

// NewStdioClient 创建 Stdio 客户端。
func NewStdioClient(config *types.McpServerConfig) *StdioClient {
	return &StdioClient{
		config:     config,
		serverName: config.ServerName,
	}
}

// Connect 建立 Stdio 连接，启动子进程并初始化会话。
// 从 config.Params 提取 command/args/env，创建子进程客户端并完成 MCP 握手。
//
// 与 Python 的差异：Python 支持 encoding_error_handler 参数指定子进程 stdin/stdout 的
// 编码错误处理策略，Go 不需要此参数。Go 的 stdin/stdout 是字节流，JSON 编解码在
// json.Unmarshal/json.Marshal 层统一处理 UTF-8，不涉及 Python 的 str↔bytes 编码转换问题。
// 支持 ConnectOption 中的 Timeout 参数，设置超时后 Start 和 Initialize 均受其约束。
func (c *StdioClient) Connect(ctx context.Context, opts ...types.ConnectOption) error {
	connectOpts := types.NewConnectOptions(opts...)

	// 如果设置了超时，创建带超时的 context
	if connectOpts.Timeout > 0 && connectOpts.Timeout != types.NoTimeout {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, time.Duration(connectOpts.Timeout*float64(time.Second)))
		defer cancel()
	}

	// 从 config.Params 提取 command、args、env、cwd
	command := extractStringParam(c.config.Params, "command")
	if command == "" {
		command = c.config.ServerPath
	}
	args := extractStringSliceParam(c.config.Params, "args")
	env := extractEnvSlice(c.config.Params)
	cwd := extractStringParam(c.config.Params, "cwd")

	logger.Info(logger.ComponentAgentCore).
		Str("server_name", c.serverName).
		Str("command", command).
		Strs("args", args).
		Int("env_count", len(env)).
		Str("cwd", cwd).
		Msg("正在创建 Stdio MCP 客户端")

	// 统一使用 NewStdioMCPClientWithOptions 创建客户端
	// 如果指定了 cwd，通过 WithCommandFunc 设置子进程工作目录
	var stdioOpts []mcptransport.StdioOption
	if cwd != "" {
		stdioOpts = append(stdioOpts, mcptransport.WithCommandFunc(
			func(_ context.Context, cmd string, env []string, args []string) (*exec.Cmd, error) {
				c := exec.Command(cmd, args...)
				c.Env = append(os.Environ(), env...)
				c.Dir = cwd
				return c, nil
			},
		))
	}

	client, err := mcpclient.NewStdioMCPClientWithOptions(command, env, args, stdioOpts...)
	if err != nil {
		logger.Error(logger.ComponentAgentCore).
			Err(err).
			Str("command", command).
			Str("server_name", c.serverName).
			Msg("Stdio 客户端创建失败")
		return exception.BuildError(
			exception.StatusToolMcpExecutionError,
			exception.WithParam("method", "Connect"),
			exception.WithParam("reason", fmt.Sprintf("创建 Stdio 客户端失败: %v", err)),
			exception.WithParam("card", c.serverName),
		)
	}

	// 启动客户端（设置通知处理器等，传输层已由 NewStdioMCPClient 启动）
	if err := client.Start(ctx); err != nil {
		logger.Error(logger.ComponentAgentCore).
			Err(err).
			Str("server_name", c.serverName).
			Msg("Stdio 客户端启动失败")
		return exception.BuildError(
			exception.StatusToolMcpExecutionError,
			exception.WithParam("method", "Connect"),
			exception.WithParam("reason", fmt.Sprintf("启动 Stdio 连接失败: %v", err)),
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
			Str("server_name", c.serverName).
			Msg("Stdio 客户端初始化会话失败")
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
		Str("server_name", c.serverName).
		Msg("Stdio 客户端连接成功")

	return nil
}

// Disconnect 断开 Stdio 连接。
func (c *StdioClient) Disconnect(_ context.Context) error {
	if !c.isConnected {
		return nil
	}
	if c.mcpClient != nil {
		if err := c.mcpClient.Close(); err != nil {
			logger.Error(logger.ComponentAgentCore).
				Err(err).
				Str("server_name", c.serverName).
				Msg("Stdio 客户端断开连接失败")
			return err
		}
	}
	c.isConnected = false
	c.mcpClient = nil
	logger.Info(logger.ComponentAgentCore).
		Str("server_name", c.serverName).
		Msg("Stdio 客户端已断开连接")
	return nil
}

// ListTools 列出服务器提供的工具。
func (c *StdioClient) ListTools(ctx context.Context) ([]*types.McpToolCard, error) {
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
			Msg("Stdio 客户端列出工具失败")
		return nil, exception.BuildError(
			exception.StatusToolMcpExecutionError,
			exception.WithParam("method", "ListTools"),
			exception.WithParam("reason", err.Error()),
			exception.WithParam("card", c.serverName),
		)
	}

	tools := make([]*types.McpToolCard, 0, len(resp.Tools))
	for _, t := range resp.Tools {
		inputParams := jsonSchemaToParams(t.InputSchema)
		tools = append(tools, types.NewMcpToolCard(
			t.Name,
			t.Description,
			c.serverName,
			inputParams,
			types.WithMcpToolCardServerID(c.config.ServerID),
		))
	}

	logger.Info(logger.ComponentAgentCore).
		Str("server_name", c.serverName).
		Int("tool_count", len(tools)).
		Msg("Stdio 客户端列出工具成功")

	return tools, nil
}

// CallTool 调用指定工具。
func (c *StdioClient) CallTool(ctx context.Context, toolName string, arguments map[string]any) (any, error) {
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
			Msg("Stdio 客户端调用工具失败")
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
func (c *StdioClient) GetToolInfo(ctx context.Context, toolName string) (*types.McpToolCard, error) {
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
func (c *StdioClient) ListResources(ctx context.Context) ([]map[string]any, error) {
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
			Msg("Stdio 客户端列出资源失败")
		return nil, exception.BuildError(
			exception.StatusToolMcpExecutionError,
			exception.WithParam("method", "ListResources"),
			exception.WithParam("reason", err.Error()),
			exception.WithParam("card", c.serverName),
		)
	}

	result := make([]map[string]any, 0, len(resp.Resources))
	for _, r := range resp.Resources {
		result = append(result, resourceToMap(r))
	}
	return result, nil
}

// ReadResource 读取指定资源。
func (c *StdioClient) ReadResource(ctx context.Context, uri string) ([]map[string]any, error) {
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
			Msg("Stdio 客户端读取资源失败")
		return nil, exception.BuildError(
			exception.StatusToolMcpExecutionError,
			exception.WithParam("method", "ReadResource"),
			exception.WithParam("reason", err.Error()),
			exception.WithParam("card", c.serverName),
		)
	}

	return readResourceResultToMap(resp), nil
}

// Close 关闭客户端（等价于 Disconnect）。
func (c *StdioClient) Close() error {
	return c.Disconnect(context.Background())
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// extractStringParam 从 Params 中提取字符串参数。
func extractStringParam(params map[string]any, key string) string {
	if params == nil {
		return ""
	}
	val, ok := params[key]
	if !ok {
		return ""
	}
	str, ok := val.(string)
	if !ok {
		return ""
	}
	return str
}

// extractStringSliceParam 从 Params 中提取字符串切片参数。
// Params 中的值类型为 []any，需要逐个转换为 string。
func extractStringSliceParam(params map[string]any, key string) []string {
	if params == nil {
		return nil
	}
	val, ok := params[key]
	if !ok {
		return nil
	}
	slice, ok := val.([]any)
	if !ok {
		return nil
	}
	result := make([]string, 0, len(slice))
	for _, item := range slice {
		if s, ok := item.(string); ok {
			result = append(result, s)
		}
	}
	return result
}

// extractEnvSlice 从 Params 中提取环境变量切片。
// Params 中的 env 类型为 map[string]any，转换为 "key=value" 格式的字符串切片。
func extractEnvSlice(params map[string]any) []string {
	if params == nil {
		return nil
	}
	val, ok := params["env"]
	if !ok {
		return nil
	}
	envMap, ok := val.(map[string]any)
	if !ok {
		return nil
	}
	result := make([]string, 0, len(envMap))
	for k, v := range envMap {
		result = append(result, fmt.Sprintf("%s=%v", k, v))
	}
	return result
}

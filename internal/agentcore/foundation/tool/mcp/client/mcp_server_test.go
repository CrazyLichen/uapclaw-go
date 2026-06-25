package client

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	mcpclient "github.com/mark3labs/mcp-go/client"
	mcp "github.com/mark3labs/mcp-go/mcp"
	mcpserver "github.com/mark3labs/mcp-go/server"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool/mcp/types"
)

// ──────────────────────────── 辅助函数 ────────────────────────────

// newTestMCPServer 创建带有一个测试工具的 MCP 服务器。
func newTestMCPServer() *mcpserver.MCPServer {
	server := mcpserver.NewMCPServer("test-server", "1.0.0")
	server.AddTool(mcp.NewTool("echo",
		mcp.WithDescription("回显输入文本"),
		mcp.WithString("text", mcp.Required(), mcp.Description("输入文本")),
	), func(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		text := ""
		if args, ok := req.Params.Arguments.(map[string]any); ok {
			if v, ok2 := args["text"].(string); ok2 {
				text = v
			}
		}
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				mcp.TextContent{Type: "text", Text: text},
			},
		}, nil
	})
	return server
}

// newSSETestServer 创建基于 httptest 的 SSE MCP 服务器，返回 (httpServer, sseURL)。
func newSSETestServer(t *testing.T) (*httptest.Server, string) {
	t.Helper()
	mcpSrv := newTestMCPServer()
	sseSrv := mcpserver.NewSSEServer(mcpSrv)
	httpServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/sse":
			sseSrv.SSEHandler().ServeHTTP(w, r)
		case "/message":
			sseSrv.MessageHandler().ServeHTTP(w, r)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	sseURL := httpServer.URL + "/sse"
	return httpServer, sseURL
}

// newStreamableHTTPTestServer 创建基于 httptest 的 StreamableHTTP MCP 服务器。
func newStreamableHTTPTestServer(t *testing.T) (*httptest.Server, string) {
	t.Helper()
	mcpSrv := newTestMCPServer()
	httpSrv := mcpserver.NewStreamableHTTPServer(mcpSrv)
	server := httptest.NewServer(httpSrv)
	mcpURL := server.URL + "/mcp"
	return server, mcpURL
}

// ──────────────────────────── SseClient 集成测试 ────────────────────────────

// TestSseClient_连接和调用 测试 SSE 客户端连接、列出工具、调用工具、断开连接。
func TestSseClient_连接和调用(t *testing.T) {
	sseServer, sseURL := newSSETestServer(t)
	defer sseServer.Close()

	config := types.NewMcpServerConfig("test-sse", sseURL, "sse")
	client := NewSseClient(config)

	// 连接
	err := client.Connect(context.Background())
	require.NoError(t, err)
	assert.True(t, client.isConnected)

	// 列出工具
	tools, err := client.ListTools(context.Background())
	require.NoError(t, err)
	assert.Len(t, tools, 1)
	assert.Equal(t, "echo", tools[0].Name)

	// 调用工具
	result, err := client.CallTool(context.Background(), "echo", map[string]any{"text": "hello"})
	require.NoError(t, err)
	assert.NotNil(t, result)
	resultMap := result.(map[string]any)
	contents := resultMap["content"].([]any)
	assert.Len(t, contents, 1)

	// 获取工具信息
	toolInfo, err := client.GetToolInfo(context.Background(), "echo")
	require.NoError(t, err)
	assert.Equal(t, "echo", toolInfo.Name)

	// 获取不存在的工具信息
	_, err = client.GetToolInfo(context.Background(), "nonexistent")
	assert.Error(t, err)

	// 列出资源（MCP 服务器可能不支持资源，不要求成功）
	resources, err := client.ListResources(context.Background())
	_ = resources
	_ = err

	// 断开连接
	err = client.Disconnect(context.Background())
	require.NoError(t, err)
	assert.False(t, client.isConnected)
}

// TestSseClient_Close已连接 测试已连接时 Close 断开连接。
func TestSseClient_Close已连接(t *testing.T) {
	sseServer, sseURL := newSSETestServer(t)
	defer sseServer.Close()

	config := types.NewMcpServerConfig("test-sse", sseURL, "sse")
	client := NewSseClient(config)

	err := client.Connect(context.Background())
	require.NoError(t, err)

	err = client.Close()
	assert.NoError(t, err)
	assert.False(t, client.isConnected)
}

// TestSseClient_Disconnect已连接 测试已连接时断开连接。
func TestSseClient_Disconnect已连接(t *testing.T) {
	sseServer, sseURL := newSSETestServer(t)
	defer sseServer.Close()

	config := types.NewMcpServerConfig("test-sse", sseURL, "sse")
	client := NewSseClient(config)

	err := client.Connect(context.Background())
	require.NoError(t, err)

	err = client.Disconnect(context.Background())
	require.NoError(t, err)
	assert.False(t, client.isConnected)
	assert.Nil(t, client.mcpClient)
}

// TestSseClient_读取资源 测试 SSE 客户端读取资源。
func TestSseClient_读取资源(t *testing.T) {
	sseServer, sseURL := newSSETestServer(t)
	defer sseServer.Close()

	config := types.NewMcpServerConfig("test-sse", sseURL, "sse")
	client := NewSseClient(config)

	err := client.Connect(context.Background())
	require.NoError(t, err)
	defer func() { _ = client.Disconnect(context.Background()) }()

	// 读取不存在的资源可能返回错误，但不应 panic
	_, err = client.ReadResource(context.Background(), "test://nonexistent")
	_ = err
}

// TestSseClient_连接带AuthHeaders 测试 SSE 客户端带认证头连接。
func TestSseClient_连接带AuthHeaders(t *testing.T) {
	sseServer, sseURL := newSSETestServer(t)
	defer sseServer.Close()

	config := types.NewMcpServerConfig("test-sse", sseURL, "sse")
	config.AuthHeaders = map[string]string{
		"Authorization": "Bearer test-token",
	}
	client := NewSseClient(config)

	err := client.Connect(context.Background())
	require.NoError(t, err)
	defer func() { _ = client.Disconnect(context.Background()) }()

	tools, err := client.ListTools(context.Background())
	require.NoError(t, err)
	assert.Len(t, tools, 1)
}

// TestSseClient_带超时连接成功 测试带超时选项连接成功（使用 context 超时）。
func TestSseClient_带超时连接成功(t *testing.T) {
	sseServer, sseURL := newSSETestServer(t)
	defer sseServer.Close()

	config := types.NewMcpServerConfig("test-sse", sseURL, "sse")
	client := NewSseClient(config)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	err := client.Connect(ctx)
	require.NoError(t, err)
	defer func() { _ = client.Disconnect(context.Background()) }()

	tools, err := client.ListTools(context.Background())
	require.NoError(t, err)
	assert.Len(t, tools, 1)
}

// ──────────────────────────── StreamableHttpClient 集成测试 ────────────────────────────

// TestStreamableHttpClient_连接和调用 测试 StreamableHTTP 客户端连接和调用。
func TestStreamableHttpClient_连接和调用(t *testing.T) {
	httpServer, mcpURL := newStreamableHTTPTestServer(t)
	defer httpServer.Close()

	config := types.NewMcpServerConfig("test-http", mcpURL, "streamable-http")
	client := NewStreamableHttpClient(config)

	// 连接
	err := client.Connect(context.Background())
	require.NoError(t, err)
	assert.True(t, client.isConnected)

	// 列出工具
	tools, err := client.ListTools(context.Background())
	require.NoError(t, err)
	assert.Len(t, tools, 1)
	assert.Equal(t, "echo", tools[0].Name)

	// 调用工具
	result, err := client.CallTool(context.Background(), "echo", map[string]any{"text": "hello"})
	require.NoError(t, err)
	assert.NotNil(t, result)

	// 获取工具信息
	toolInfo, err := client.GetToolInfo(context.Background(), "echo")
	require.NoError(t, err)
	assert.Equal(t, "echo", toolInfo.Name)

	// 获取不存在的工具信息
	_, err = client.GetToolInfo(context.Background(), "nonexistent")
	assert.Error(t, err)

	// 列出资源（MCP 服务器可能不支持资源，不要求成功）
	resources, err := client.ListResources(context.Background())
	_ = resources
	_ = err

	// 读取资源
	_, err = client.ReadResource(context.Background(), "test://resource")
	_ = err

	// 断开连接
	err = client.Disconnect(context.Background())
	require.NoError(t, err)
	assert.False(t, client.isConnected)
}

// TestStreamableHttpClient_Close已连接 测试已连接时 Close 断开连接。
func TestStreamableHttpClient_Close已连接(t *testing.T) {
	httpServer, mcpURL := newStreamableHTTPTestServer(t)
	defer httpServer.Close()

	config := types.NewMcpServerConfig("test-http", mcpURL, "streamable-http")
	client := NewStreamableHttpClient(config)

	err := client.Connect(context.Background())
	require.NoError(t, err)

	err = client.Close()
	assert.NoError(t, err)
	assert.False(t, client.isConnected)
}

// TestStreamableHttpClient_Disconnect已连接 测试已连接时断开连接。
func TestStreamableHttpClient_Disconnect已连接(t *testing.T) {
	httpServer, mcpURL := newStreamableHTTPTestServer(t)
	defer httpServer.Close()

	config := types.NewMcpServerConfig("test-http", mcpURL, "streamable-http")
	client := NewStreamableHttpClient(config)

	err := client.Connect(context.Background())
	require.NoError(t, err)

	err = client.Disconnect(context.Background())
	require.NoError(t, err)
	assert.False(t, client.isConnected)
}

// TestStreamableHttpClient_连接带AuthHeaders 测试 StreamableHTTP 客户端带认证头连接。
func TestStreamableHttpClient_连接带AuthHeaders(t *testing.T) {
	httpServer, mcpURL := newStreamableHTTPTestServer(t)
	defer httpServer.Close()

	config := types.NewMcpServerConfig("test-http", mcpURL, "streamable-http")
	config.AuthHeaders = map[string]string{
		"Authorization": "Bearer test-token",
	}
	client := NewStreamableHttpClient(config)

	err := client.Connect(context.Background())
	require.NoError(t, err)
	defer func() { _ = client.Disconnect(context.Background()) }()

	tools, err := client.ListTools(context.Background())
	require.NoError(t, err)
	assert.Len(t, tools, 1)
}

// TestStreamableHttpClient_带超时连接成功 测试带超时选项连接成功。
func TestStreamableHttpClient_带超时连接成功(t *testing.T) {
	httpServer, mcpURL := newStreamableHTTPTestServer(t)
	defer httpServer.Close()

	config := types.NewMcpServerConfig("test-http", mcpURL, "streamable-http")
	client := NewStreamableHttpClient(config)

	err := client.Connect(context.Background(), types.WithConnectTimeout(30))
	require.NoError(t, err)
	defer func() { _ = client.Disconnect(context.Background()) }()

	tools, err := client.ListTools(context.Background())
	require.NoError(t, err)
	assert.Len(t, tools, 1)
}

// ──────────────────────────── PlaywrightClient 集成测试 ────────────────────────────

// TestPlaywrightClient_SSE传输 测试 Playwright 客户端选择 SSE 传输。
func TestPlaywrightClient_SSE传输(t *testing.T) {
	sseServer, sseURL := newSSETestServer(t)
	defer sseServer.Close()

	config := types.NewMcpServerConfig("test-pw", sseURL, "playwright")
	client := NewPlaywrightClient(config)

	// 连接（应选择 SSE 传输）
	err := client.Connect(context.Background())
	require.NoError(t, err)

	// 列出工具
	tools, err := client.ListTools(context.Background())
	require.NoError(t, err)
	assert.Len(t, tools, 1)

	// 调用工具
	result, err := client.CallTool(context.Background(), "echo", map[string]any{"text": "hello"})
	require.NoError(t, err)
	assert.NotNil(t, result)

	// 获取工具信息
	toolInfo, err := client.GetToolInfo(context.Background(), "echo")
	require.NoError(t, err)
	assert.Equal(t, "echo", toolInfo.Name)

	// 列出资源（MCP 服务器可能不支持资源，不要求成功）
	resources, err := client.ListResources(context.Background())
	_ = resources
	_ = err

	// 读取资源
	_, err = client.ReadResource(context.Background(), "test://resource")
	_ = err

	// 断开连接
	err = client.Disconnect(context.Background())
	require.NoError(t, err)
}

// TestPlaywrightClient_Close已连接 测试已连接时 Close 断开连接。
func TestPlaywrightClient_Close已连接(t *testing.T) {
	sseServer, sseURL := newSSETestServer(t)
	defer sseServer.Close()

	config := types.NewMcpServerConfig("test-pw", sseURL, "playwright")
	client := NewPlaywrightClient(config)

	err := client.Connect(context.Background())
	require.NoError(t, err)

	err = client.Close()
	assert.NoError(t, err)
}

// ──────────────────────────── SseClient 错误路径测试 ────────────────────────────

// TestSseClient_连接失败 测试 SSE 客户端连接到不存在的服务器返回错误。
func TestSseClient_连接失败(t *testing.T) {
	config := types.NewMcpServerConfig("test-sse", "http://localhost:99999/sse", "sse")
	client := NewSseClient(config)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	err := client.Connect(ctx)
	assert.Error(t, err)
}

// TestStreamableHttpClient_连接失败 测试 StreamableHTTP 客户端连接到不存在的服务器返回错误。
func TestStreamableHttpClient_连接失败(t *testing.T) {
	config := types.NewMcpServerConfig("test-http", "http://localhost:99999/mcp", "streamable-http")
	client := NewStreamableHttpClient(config)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	err := client.Connect(ctx)
	assert.Error(t, err)
}

// ──────────────────────────── StdioClient 扩展测试 ────────────────────────────

// TestStdioClient_连接失败 测试 Stdio 客户端连接到不存在的命令返回错误。
func TestStdioClient_连接失败(t *testing.T) {
	config := types.NewMcpServerConfig("test-stdio", "nonexistent_command_that_does_not_exist", "stdio")
	client := NewStdioClient(config)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	err := client.Connect(ctx)
	assert.Error(t, err)
}

// TestStdioClient_带Params连接 测试 Stdio 客户端从 Params 提取 command/args/env。
func TestStdioClient_带Params连接(t *testing.T) {
	config := types.NewMcpServerConfig("test-stdio", "fallback", "stdio")
	config.Params = map[string]any{
		"command": "nonexistent_command",
		"args":    []any{"--flag"},
		"env":     map[string]any{"KEY": "value"},
	}
	_ = NewStdioClient(config) // 仅验证创建不 panic

	// 验证参数提取
	cmd := extractStringParam(config.Params, "command")
	assert.Equal(t, "nonexistent_command", cmd)
	args := extractStringSliceParam(config.Params, "args")
	assert.Equal(t, []string{"--flag"}, args)
	env := extractEnvSlice(config.Params)
	assert.Contains(t, env, "KEY=value")
}

// ──────────────────────────── mcpclient 直接使用测试 ────────────────────────────

// TestMcpClient_直接创建 测试直接创建 mcp-go 客户端。
func TestMcpClient_直接创建(t *testing.T) {
	sseServer, sseURL := newSSETestServer(t)
	defer sseServer.Close()

	client, err := mcpclient.NewSSEMCPClient(sseURL)
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err = client.Start(ctx)
	require.NoError(t, err)
	defer client.Close()

	initReq := mcp.InitializeRequest{
		Params: mcp.InitializeParams{
			ProtocolVersion: "2025-03-26",
			ClientInfo: mcp.Implementation{
				Name:    "test",
				Version: "1.0.0",
			},
		},
	}
	_, err = client.Initialize(ctx, initReq)
	require.NoError(t, err)

	// 列出工具
	resp, err := client.ListTools(ctx, mcp.ListToolsRequest{})
	require.NoError(t, err)
	assert.Len(t, resp.Tools, 1)
}

// ──────────────────────────── SseClient 连接超时测试 ────────────────────────────

// TestSseClient_连接超时 测试 SSE 客户端连接超时。
func TestSseClient_连接超时(t *testing.T) {
	config := types.NewMcpServerConfig("test-sse", "http://localhost:99999/sse", "sse")
	client := NewSseClient(config)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	err := client.Connect(ctx, types.WithConnectTimeout(1))
	assert.Error(t, err)
}

// TestStreamableHttpClient_连接超时 测试 StreamableHTTP 客户端连接超时。
func TestStreamableHttpClient_连接超时(t *testing.T) {
	config := types.NewMcpServerConfig("test-http", "http://localhost:99999/mcp", "streamable-http")
	client := NewStreamableHttpClient(config)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	err := client.Connect(ctx, types.WithConnectTimeout(1))
	assert.Error(t, err)
}

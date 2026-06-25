package client

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	mcp "github.com/mark3labs/mcp-go/mcp"
	mcpserver "github.com/mark3labs/mcp-go/server"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool/mcp/types"
)

// ──────────────────────────── SseClient 扩展测试 ────────────────────────────

// TestSseClient_连接带AuthQueryParams 测试 SSE 客户端带认证查询参数连接。
func TestSseClient_连接带AuthQueryParams(t *testing.T) {
	sseServer, sseURL := newSSETestServer(t)
	defer sseServer.Close()

	config := types.NewMcpServerConfig("test-sse", sseURL, "sse")
	config.AuthQueryParams = map[string]string{
		"token": "abc123",
	}
	client := NewSseClient(config)

	err := client.Connect(context.Background())
	require.NoError(t, err)
	defer client.Disconnect(context.Background())

	tools, err := client.ListTools(context.Background())
	require.NoError(t, err)
	assert.Len(t, tools, 1)
}

// TestSseClient_连接带超时选项 测试 SSE 客户端带 ConnectTimeout 选项连接。
func TestSseClient_连接带超时选项(t *testing.T) {
	sseServer, sseURL := newSSETestServer(t)
	defer sseServer.Close()

	config := types.NewMcpServerConfig("test-sse", sseURL, "sse")
	client := NewSseClient(config)

	err := client.Connect(context.Background(), types.WithConnectTimeout(30))
	require.NoError(t, err)
	defer client.Disconnect(context.Background())
}

// TestSseClient_断开连接已连接 测试已连接状态下断开连接。
func TestSseClient_断开连接已连接(t *testing.T) {
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

// ──────────────────────────── StreamableHttpClient 扩展测试 ────────────────────────────

// TestStreamableHttpClient_连接带AuthQueryParams 测试 StreamableHTTP 客户端带认证查询参数连接。
func TestStreamableHttpClient_连接带AuthQueryParams(t *testing.T) {
	httpServer, mcpURL := newStreamableHTTPTestServer(t)
	defer httpServer.Close()

	config := types.NewMcpServerConfig("test-http", mcpURL, "streamable-http")
	config.AuthQueryParams = map[string]string{
		"token": "abc123",
	}
	client := NewStreamableHttpClient(config)

	err := client.Connect(context.Background())
	require.NoError(t, err)
	defer client.Disconnect(context.Background())
}

// TestStreamableHttpClient_断开连接已连接 测试已连接状态下断开连接。
func TestStreamableHttpClient_断开连接已连接(t *testing.T) {
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

// TestStreamableHttpClient_连接带超时选项 测试 StreamableHTTP 客户端带超时选项连接。
func TestStreamableHttpClient_连接带超时选项(t *testing.T) {
	httpServer, mcpURL := newStreamableHTTPTestServer(t)
	defer httpServer.Close()

	config := types.NewMcpServerConfig("test-http", mcpURL, "streamable-http")
	client := NewStreamableHttpClient(config)

	err := client.Connect(context.Background(), types.WithConnectTimeout(30))
	require.NoError(t, err)
	defer client.Disconnect(context.Background())
}

// ──────────────────────────── PlaywrightClient 扩展测试 ────────────────────────────

// TestPlaywrightClient_Stdio回退 测试 Playwright 客户端非 HTTP URL 时回退到 Stdio。
func TestPlaywrightClient_Stdio回退(t *testing.T) {
	config := types.NewMcpServerConfig("test-pw", "nonexistent_command", "playwright")
	client := NewPlaywrightClient(config)

	// 连接不存在的命令应返回错误
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	err := client.Connect(ctx)
	assert.Error(t, err)
}

// TestPlaywrightClient_SSE传输完整流程 测试 Playwright 客户端 SSE 传输完整流程。
func TestPlaywrightClient_SSE传输完整流程(t *testing.T) {
	sseServer, sseURL := newSSETestServer(t)
	defer sseServer.Close()

	config := types.NewMcpServerConfig("test-pw", sseURL, "playwright")
	client := NewPlaywrightClient(config)

	err := client.Connect(context.Background())
	require.NoError(t, err)
	defer client.Disconnect(context.Background())

	// CallTool
	result, err := client.CallTool(context.Background(), "echo", map[string]any{"text": "hello"})
	require.NoError(t, err)
	assert.NotNil(t, result)

	// ListResources
	_, err = client.ListResources(context.Background())
	_ = err

	// ReadResource
	_, err = client.ReadResource(context.Background(), "test://resource")
	_ = err
}

// TestPlaywrightClient_GetToolInfo工具不存在 测试 Playwright 客户端 GetToolInfo 工具不存在。
func TestPlaywrightClient_GetToolInfo工具不存在(t *testing.T) {
	sseServer, sseURL := newSSETestServer(t)
	defer sseServer.Close()

	config := types.NewMcpServerConfig("test-pw", sseURL, "playwright")
	client := NewPlaywrightClient(config)

	err := client.Connect(context.Background())
	require.NoError(t, err)
	defer client.Disconnect(context.Background())

	_, err = client.GetToolInfo(context.Background(), "nonexistent")
	assert.Error(t, err)
}

// ──────────────────────────── StdioClient 扩展测试 ────────────────────────────

// TestStdioClient_连接带Cwd 测试 Stdio 客户端带 cwd 参数。
func TestStdioClient_连接带Cwd(t *testing.T) {
	tmpDir := t.TempDir()
	config := types.NewMcpServerConfig("test-stdio", "nonexistent_command", "stdio")
	config.Params = map[string]any{
		"cwd": tmpDir,
	}
	client := NewStdioClient(config)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	err := client.Connect(ctx)
	// 连接不存在的命令应返回错误
	assert.Error(t, err)
}

// TestStdioClient_连接带AuthHeaders 测试 Stdio 客户端带认证头创建。
func TestStdioClient_连接带AuthHeaders(t *testing.T) {
	config := types.NewMcpServerConfig("test-stdio", "nonexistent_command", "stdio")
	config.AuthHeaders = map[string]string{
		"Authorization": "Bearer test-token",
	}
	client := NewStdioClient(config)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	err := client.Connect(ctx)
	// 连接不存在的命令应返回错误
	assert.Error(t, err)
}

// ──────────────────────────── 资源服务器测试 ────────────────────────────

// newTestMCPServerWithResource 创建带有一个测试资源和工具的 MCP 服务器。
func newTestMCPServerWithResource() *mcpserver.MCPServer {
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
	server.AddResource(mcp.NewResource("test://resource", "测试资源",
		mcp.WithResourceDescription("一个测试资源"),
	), func(_ context.Context, _ mcp.ReadResourceRequest) ([]mcp.ResourceContents, error) {
		return []mcp.ResourceContents{
			mcp.TextResourceContents{
				URI:      "test://resource",
				MIMEType: "text/plain",
				Text:     "资源内容",
			},
		}, nil
	})
	return server
}

// TestSseClient_资源操作 测试 SSE 客户端资源和工具操作。
func TestSseClient_资源操作(t *testing.T) {
	mcpSrv := newTestMCPServerWithResource()
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
	defer httpServer.Close()

	sseURL := httpServer.URL + "/sse"
	config := types.NewMcpServerConfig("test-sse", sseURL, "sse")
	client := NewSseClient(config)

	err := client.Connect(context.Background())
	require.NoError(t, err)
	defer client.Disconnect(context.Background())

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

	// 列出资源
	resources, err := client.ListResources(context.Background())
	require.NoError(t, err)
	assert.Len(t, resources, 1)

	// 读取资源
	readResult, err := client.ReadResource(context.Background(), "test://resource")
	require.NoError(t, err)
	assert.NotNil(t, readResult)
}

// TestStreamableHttpClient_资源操作 测试 StreamableHTTP 客户端资源和工具操作。
func TestStreamableHttpClient_资源操作(t *testing.T) {
	mcpSrv := newTestMCPServerWithResource()
	httpSrv := mcpserver.NewStreamableHTTPServer(mcpSrv)
	server := httptest.NewServer(httpSrv)
	defer server.Close()

	mcpURL := server.URL + "/mcp"
	config := types.NewMcpServerConfig("test-http", mcpURL, "streamable-http")
	client := NewStreamableHttpClient(config)

	err := client.Connect(context.Background())
	require.NoError(t, err)
	defer client.Disconnect(context.Background())

	// 列出工具
	tools, err := client.ListTools(context.Background())
	require.NoError(t, err)
	assert.Len(t, tools, 1)

	// 调用工具
	result, err := client.CallTool(context.Background(), "echo", map[string]any{"text": "hello"})
	require.NoError(t, err)
	assert.NotNil(t, result)

	// 列出资源
	resources, err := client.ListResources(context.Background())
	require.NoError(t, err)
	assert.Len(t, resources, 1)

	// 读取资源
	readResult, err := client.ReadResource(context.Background(), "test://resource")
	require.NoError(t, err)
	assert.NotNil(t, readResult)
}

// ──────────────────────────── OpenApiClient 扩展测试 ────────────────────────────

// TestOpenApiClient_CallTool带QueryParams 测试 OpenAPI 客户端带查询参数调用。
func TestOpenApiClient_CallTool带QueryParams(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"method": r.Method,
			"query":  r.URL.Query(),
		})
	}))
	defer server.Close()

	tmpDir := t.TempDir()
	specPath := tmpDir + "/openapi.json"

	spec := map[string]any{
		"openapi": "3.0.0",
		"info":    map[string]any{"title": "Test", "version": "1.0.0"},
		"servers": []map[string]any{{"url": server.URL}},
		"paths": map[string]any{
			"/search": map[string]any{
				"get": map[string]any{
					"operationId": "search",
					"summary":     "搜索",
					"parameters": []any{
						map[string]any{
							"name":     "q",
							"in":       "query",
							"required": true,
							"schema":   map[string]any{"type": "string"},
						},
						map[string]any{
							"name":   "limit",
							"in":     "query",
							"schema": map[string]any{"type": "integer"},
						},
					},
					"responses": map[string]any{
						"200": map[string]any{"description": "ok"},
					},
				},
			},
		},
	}
	specJSON, err := json.Marshal(spec)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(specPath, specJSON, 0644))

	config := types.NewMcpServerConfig("test-api", specPath, "openapi")
	client := NewOpenApiClient(config)

	err = client.Connect(context.Background())
	require.NoError(t, err)

	result, err := client.CallTool(context.Background(), "search", map[string]any{"q": "test", "limit": 10})
	require.NoError(t, err)
	assert.NotNil(t, result)
}

// TestOpenApiClient_无效JSON内容 测试 OpenAPI 文件含无效 JSON 内容。
func TestOpenApiClient_无效JSON内容(t *testing.T) {
	tmpDir := t.TempDir()
	specPath := tmpDir + "/openapi.json"
	require.NoError(t, os.WriteFile(specPath, []byte("{invalid}"), 0644))

	config := types.NewMcpServerConfig("test", specPath, "openapi")
	client := NewOpenApiClient(config)

	err := client.Connect(context.Background())
	assert.Error(t, err)
}

// TestOpenApiClient_空YAML 测试加载空 YAML 文件。
func TestOpenApiClient_空YAML(t *testing.T) {
	tmpDir := t.TempDir()
	specPath := tmpDir + "/openapi.yaml"
	require.NoError(t, os.WriteFile(specPath, []byte("openapi: \"3.0.0\"\ninfo:\n  title: Test\n  version: \"1.0.0\"\npaths: {}\n"), 0644))

	config := types.NewMcpServerConfig("test", specPath, "openapi")
	client := NewOpenApiClient(config)

	err := client.Connect(context.Background())
	require.NoError(t, err)

	tools, err := client.ListTools(context.Background())
	require.NoError(t, err)
	assert.Empty(t, tools)
}

// ──────────────────────────── OpenApiClient CallTool 错误路径测试 ────────────────────────────

// TestOpenApiClient_CallTool服务端关闭 测试 CallTool 时服务端关闭返回错误。
func TestOpenApiClient_CallTool服务端关闭(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
	}))

	tmpDir := t.TempDir()
	specPath := tmpDir + "/openapi.json"

	spec := map[string]any{
		"openapi": "3.0.0",
		"info":    map[string]any{"title": "Test", "version": "1.0.0"},
		"servers": []map[string]any{{"url": server.URL}},
		"paths": map[string]any{
			"/test": map[string]any{
				"get": map[string]any{
					"operationId": "testCall",
					"responses":   map[string]any{"200": map[string]any{"description": "ok"}},
				},
			},
		},
	}
	specJSON, err := json.Marshal(spec)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(specPath, specJSON, 0644))

	config := types.NewMcpServerConfig("test", specPath, "openapi")
	client := NewOpenApiClient(config)

	err = client.Connect(context.Background())
	require.NoError(t, err)

	// 先成功调用一次
	_, err = client.CallTool(context.Background(), "testCall", map[string]any{})
	require.NoError(t, err)

	// 关闭服务端后再调用应返回错误
	server.Close()
	_, err = client.CallTool(context.Background(), "testCall", map[string]any{})
	assert.Error(t, err)
}

// TestOpenApiClient_带Header参数的CallTool 测试 OpenAPI 客户端带 header 参数调用。
func TestOpenApiClient_带Header参数的CallTool(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"x-custom": r.Header.Get("X-Custom"),
		})
	}))
	defer server.Close()

	tmpDir := t.TempDir()
	specPath := tmpDir + "/openapi.json"

	spec := map[string]any{
		"openapi": "3.0.0",
		"info":    map[string]any{"title": "Test", "version": "1.0.0"},
		"servers": []map[string]any{{"url": server.URL}},
		"paths": map[string]any{
			"/test": map[string]any{
				"get": map[string]any{
					"operationId": "testWithHeader",
					"parameters": []any{
						map[string]any{
							"name":     "X-Custom",
							"in":       "header",
							"required": false,
							"schema":   map[string]any{"type": "string"},
						},
					},
					"responses": map[string]any{"200": map[string]any{"description": "ok"}},
				},
			},
		},
	}
	specJSON, err := json.Marshal(spec)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(specPath, specJSON, 0644))

	config := types.NewMcpServerConfig("test-api", specPath, "openapi")
	client := NewOpenApiClient(config)

	err = client.Connect(context.Background())
	require.NoError(t, err)

	result, err := client.CallTool(context.Background(), "testWithHeader", map[string]any{"X-Custom": "my-value"})
	require.NoError(t, err)
	assert.NotNil(t, result)
}

// TestOpenApiClient_带路径参数的CallTool 测试 OpenAPI 客户端带路径参数调用。
func TestOpenApiClient_带路径参数的CallTool(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"path": r.URL.Path,
		})
	}))
	defer server.Close()

	tmpDir := t.TempDir()
	specPath := tmpDir + "/openapi.json"

	spec := map[string]any{
		"openapi": "3.0.0",
		"info":    map[string]any{"title": "Test", "version": "1.0.0"},
		"servers": []map[string]any{{"url": server.URL}},
		"paths": map[string]any{
			"/users/{userId}": map[string]any{
				"get": map[string]any{
					"operationId": "getUser",
					"parameters": []any{
						map[string]any{
							"name":     "userId",
							"in":       "path",
							"required": true,
							"schema":   map[string]any{"type": "string"},
						},
					},
					"responses": map[string]any{"200": map[string]any{"description": "ok"}},
				},
			},
		},
	}
	specJSON, err := json.Marshal(spec)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(specPath, specJSON, 0644))

	config := types.NewMcpServerConfig("test-api", specPath, "openapi")
	client := NewOpenApiClient(config)

	err = client.Connect(context.Background())
	require.NoError(t, err)

	result, err := client.CallTool(context.Background(), "getUser", map[string]any{"userId": "42"})
	require.NoError(t, err)
	assert.NotNil(t, result)
}

// TestOpenApiClient_描述从Summary回退 测试 OpenAPI 工具描述从 summary 回退。
func TestOpenApiClient_描述从Summary回退(t *testing.T) {
	tmpDir := t.TempDir()
	specPath := tmpDir + "/openapi.json"

	// 不设置 description，只设置 summary
	spec := map[string]any{
		"openapi": "3.0.0",
		"info":    map[string]any{"title": "Test", "version": "1.0.0"},
		"paths": map[string]any{
			"/test": map[string]any{
				"get": map[string]any{
					"operationId": "testOp",
					"summary":     "测试操作",
					"responses":   map[string]any{"200": map[string]any{"description": "ok"}},
				},
			},
		},
	}
	specJSON, err := json.Marshal(spec)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(specPath, specJSON, 0644))

	config := types.NewMcpServerConfig("test", specPath, "openapi")
	client := NewOpenApiClient(config)

	err = client.Connect(context.Background())
	require.NoError(t, err)

	tools, err := client.ListTools(context.Background())
	require.NoError(t, err)
	assert.Len(t, tools, 1)
	assert.Equal(t, "testOp", tools[0].Name)
}

// TestOpenApiClient_无OperationId和Summary 测试工具名从 method+path 生成。
func TestOpenApiClient_无OperationId和Summary(t *testing.T) {
	tmpDir := t.TempDir()
	specPath := tmpDir + "/openapi.json"

	spec := map[string]any{
		"openapi": "3.0.0",
		"info":    map[string]any{"title": "Test", "version": "1.0.0"},
		"paths": map[string]any{
			"/users": map[string]any{
				"get": map[string]any{
					"responses": map[string]any{"200": map[string]any{"description": "ok"}},
				},
			},
		},
	}
	specJSON, err := json.Marshal(spec)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(specPath, specJSON, 0644))

	config := types.NewMcpServerConfig("test", specPath, "openapi")
	client := NewOpenApiClient(config)

	err = client.Connect(context.Background())
	require.NoError(t, err)

	tools, err := client.ListTools(context.Background())
	require.NoError(t, err)
	assert.Len(t, tools, 1)
	// 名称应从 method_path 生成
	assert.Contains(t, tools[0].Name, "users")
}

// ──────────────────────────── PlaywrightClient Connect 错误路径测试 ────────────────────────────

// TestPlaywrightClient_SSE连接错误 测试 Playwright 客户端 SSE 连接错误。
func TestPlaywrightClient_SSE连接错误(t *testing.T) {
	config := types.NewMcpServerConfig("test-pw", "http://localhost:99999/sse", "playwright")
	client := NewPlaywrightClient(config)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	err := client.Connect(ctx)
	assert.Error(t, err)
}

// ──────────────────────────── StdioClient Connect 更多路径测试 ────────────────────────────

// TestStdioClient_连接带CommandFromParams 测试 Stdio 客户端从 Params 提取 command。
func TestStdioClient_连接带CommandFromParams(t *testing.T) {
	config := types.NewMcpServerConfig("test-stdio", "fallback_command", "stdio")
	config.Params = map[string]any{
		"command": "nonexistent_command_from_params",
	}
	client := NewStdioClient(config)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	err := client.Connect(ctx)
	// 连接不存在的命令应返回错误
	assert.Error(t, err)
}

// TestStdioClient_连接带超时 测试 Stdio 客户端带超时选项。
func TestStdioClient_连接带超时(t *testing.T) {
	config := types.NewMcpServerConfig("test-stdio", "nonexistent_command", "stdio")
	client := NewStdioClient(config)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	err := client.Connect(ctx, types.WithConnectTimeout(1))
	assert.Error(t, err)
}

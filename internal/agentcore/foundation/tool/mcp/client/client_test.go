package client

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool/mcp/types"
)

// ──────────────────────────── SseClient 测试 ────────────────────────────

func TestNewSseClient(t *testing.T) {
	config := types.NewMcpServerConfig("test", "http://localhost:8080/sse", "sse")
	client := NewSseClient(config)
	assert.NotNil(t, client)
	assert.Equal(t, "test", client.serverName)
	assert.False(t, client.isConnected)
}

func TestSseClient_未连接时调用返回错误(t *testing.T) {
	config := types.NewMcpServerConfig("test", "http://localhost:8080/sse", "sse")
	client := NewSseClient(config)

	_, err := client.ListTools(context.Background())
	assert.Error(t, err)

	_, err = client.CallTool(context.Background(), "tool", nil)
	assert.Error(t, err)
}

func TestSseClient_Disconnect未连接不报错(t *testing.T) {
	config := types.NewMcpServerConfig("test", "http://localhost:8080/sse", "sse")
	client := NewSseClient(config)
	err := client.Disconnect(context.Background())
	assert.NoError(t, err)
}

// ──────────────────────────── StdioClient 测试 ────────────────────────────

func TestNewStdioClient(t *testing.T) {
	config := types.NewMcpServerConfig("test", "npx", "stdio")
	client := NewStdioClient(config)
	assert.NotNil(t, client)
	assert.Equal(t, "test", client.serverName)
	assert.False(t, client.isConnected)
}

func TestStdioClient_未连接时调用返回错误(t *testing.T) {
	config := types.NewMcpServerConfig("test", "npx", "stdio")
	client := NewStdioClient(config)

	_, err := client.ListTools(context.Background())
	assert.Error(t, err)
}

func TestStdioClient_Disconnect未连接不报错(t *testing.T) {
	config := types.NewMcpServerConfig("test", "npx", "stdio")
	client := NewStdioClient(config)
	err := client.Disconnect(context.Background())
	assert.NoError(t, err)
}

// ──────────────────────────── StreamableHttpClient 测试 ────────────────────────────

func TestNewStreamableHttpClient(t *testing.T) {
	config := types.NewMcpServerConfig("test", "http://localhost:8080/mcp", "streamable-http")
	client := NewStreamableHttpClient(config)
	assert.NotNil(t, client)
	assert.Equal(t, "test", client.serverName)
	assert.False(t, client.isConnected)
}

func TestStreamableHttpClient_未连接时调用返回错误(t *testing.T) {
	config := types.NewMcpServerConfig("test", "http://localhost:8080/mcp", "streamable-http")
	client := NewStreamableHttpClient(config)

	_, err := client.ListTools(context.Background())
	assert.Error(t, err)
}

func TestStreamableHttpClient_Disconnect未连接不报错(t *testing.T) {
	config := types.NewMcpServerConfig("test", "http://localhost:8080/mcp", "streamable-http")
	client := NewStreamableHttpClient(config)
	err := client.Disconnect(context.Background())
	assert.NoError(t, err)
}

// ──────────────────────────── PlaywrightClient 测试 ────────────────────────────

func TestNewPlaywrightClient(t *testing.T) {
	config := types.NewMcpServerConfig("test", "npx @playwright/mcp", "playwright")
	client := NewPlaywrightClient(config)
	assert.NotNil(t, client)
	assert.Equal(t, "test", client.serverName)
}

func TestPlaywrightClient_未初始化时调用返回错误(t *testing.T) {
	config := types.NewMcpServerConfig("test", "npx @playwright/mcp", "playwright")
	client := NewPlaywrightClient(config)

	_, err := client.ListTools(context.Background())
	assert.Error(t, err)
}

func TestPlaywrightClient_Disconnect未初始化不报错(t *testing.T) {
	config := types.NewMcpServerConfig("test", "npx @playwright/mcp", "playwright")
	client := NewPlaywrightClient(config)

	err := client.Disconnect(context.Background())
	assert.NoError(t, err)
}

// ──────────────────────────── OpenApiClient 测试 ────────────────────────────

func TestNewOpenApiClient(t *testing.T) {
	config := types.NewMcpServerConfig("test", "openapi.json", "openapi")
	client := NewOpenApiClient(config)
	assert.NotNil(t, client)
	assert.Equal(t, "test", client.serverName)
	assert.False(t, client.isConnected)
}

func TestOpenApiClient_未连接时调用返回错误(t *testing.T) {
	config := types.NewMcpServerConfig("test", "openapi.json", "openapi")
	client := NewOpenApiClient(config)

	_, err := client.ListTools(context.Background())
	assert.Error(t, err)

	_, err = client.CallTool(context.Background(), "tool", nil)
	assert.Error(t, err)
}

func TestOpenApiClient_ListResources返回空(t *testing.T) {
	config := types.NewMcpServerConfig("test", "openapi.json", "openapi")
	client := NewOpenApiClient(config)

	resources, err := client.ListResources(context.Background())
	assert.NoError(t, err)
	assert.Nil(t, resources)
}

func TestOpenApiClient_ReadResource返回nil(t *testing.T) {
	config := types.NewMcpServerConfig("test", "openapi.json", "openapi")
	client := NewOpenApiClient(config)

	result, err := client.ReadResource(context.Background(), "uri")
	assert.NoError(t, err)
	assert.Nil(t, result)
}

func TestOpenApiClient_加载OpenAPI规格文件(t *testing.T) {
	tmpDir := t.TempDir()
	specPath := filepath.Join(tmpDir, "openapi.json")

	spec := map[string]any{
		"openapi": "3.0.0",
		"info": map[string]any{
			"title":   "Test API",
			"version": "1.0.0",
		},
		"servers": []any{
			map[string]any{"url": "http://localhost:9090"},
		},
		"paths": map[string]any{
			"/items": map[string]any{
				"get": map[string]any{
					"operationId": "listItems",
					"summary":     "列出所有项目",
					"responses": map[string]any{
						"200": map[string]any{"description": "成功"},
					},
				},
				"post": map[string]any{
					"operationId": "createItem",
					"summary":     "创建项目",
					"requestBody": map[string]any{
						"required": true,
						"content": map[string]any{
							"application/json": map[string]any{
								"schema": map[string]any{
									"type": "object",
									"properties": map[string]any{
										"name": map[string]any{
											"type":        "string",
											"description": "项目名称",
										},
									},
									"required": []any{"name"},
								},
							},
						},
					},
					"responses": map[string]any{
						"201": map[string]any{"description": "创建成功"},
					},
				},
			},
			"/items/{id}": map[string]any{
				"get": map[string]any{
					"operationId": "getItem",
					"summary":     "获取项目详情",
					"parameters": []any{
						map[string]any{
							"name":     "id",
							"in":       "path",
							"required": true,
							"schema":   map[string]any{"type": "string"},
						},
					},
					"responses": map[string]any{
						"200": map[string]any{"description": "成功"},
					},
				},
			},
		},
	}

	specJSON, err := json.Marshal(spec)
	require.NoError(t, err)
	err = os.WriteFile(specPath, specJSON, 0644)
	require.NoError(t, err)

	config := types.NewMcpServerConfig("test-api", specPath, "openapi",
		types.WithServerID("test-server-id"),
	)
	client := NewOpenApiClient(config)

	// 连接（加载规格）
	err = client.Connect(context.Background())
	require.NoError(t, err)
	assert.True(t, client.isConnected)
	assert.Equal(t, "http://localhost:9090", client.baseURL)

	// 列出工具
	tools, err := client.ListTools(context.Background())
	require.NoError(t, err)
	assert.Len(t, tools, 3)

	// 验证工具名称
	toolNames := make([]string, len(tools))
	for i, tool := range tools {
		toolNames[i] = tool.Name
		assert.Equal(t, "test-api", tool.ServerName)
		assert.Equal(t, "test-server-id", tool.ServerID)
	}
	assert.Contains(t, toolNames, "listItems")
	assert.Contains(t, toolNames, "createItem")
	assert.Contains(t, toolNames, "getItem")

	// 获取工具信息
	toolInfo, err := client.GetToolInfo(context.Background(), "listItems")
	require.NoError(t, err)
	assert.Equal(t, "listItems", toolInfo.Name)

	// 断开连接
	err = client.Disconnect(context.Background())
	require.NoError(t, err)
	assert.False(t, client.isConnected)
}

func TestOpenApiClient_加载YAML规格(t *testing.T) {
	tmpDir := t.TempDir()
	specPath := filepath.Join(tmpDir, "openapi.yaml")

	yamlContent := `openapi: "3.0.0"
info:
  title: Test API
  version: "1.0.0"
paths:
  /hello:
    get:
      operationId: sayHello
      summary: 问候
      responses:
        "200":
          description: 成功
`
	err := os.WriteFile(specPath, []byte(yamlContent), 0644)
	require.NoError(t, err)

	config := types.NewMcpServerConfig("test-yaml", specPath, "openapi")
	client := NewOpenApiClient(config)

	err = client.Connect(context.Background())
	require.NoError(t, err)

	tools, err := client.ListTools(context.Background())
	require.NoError(t, err)
	assert.Len(t, tools, 1)
	assert.Equal(t, "sayHello", tools[0].Name)
}

func TestOpenApiClient_文件不存在(t *testing.T) {
	config := types.NewMcpServerConfig("test", "/nonexistent/openapi.json", "openapi")
	client := NewOpenApiClient(config)

	err := client.Connect(context.Background())
	assert.Error(t, err)
}

func TestOpenApiClient_无效扩展名(t *testing.T) {
	tmpDir := t.TempDir()
	specPath := filepath.Join(tmpDir, "openapi.txt")
	err := os.WriteFile(specPath, []byte("hello"), 0644)
	require.NoError(t, err)

	config := types.NewMcpServerConfig("test", specPath, "openapi")
	client := NewOpenApiClient(config)

	err = client.Connect(context.Background())
	assert.Error(t, err)
}

func TestOpenApiClient_工具名称去重(t *testing.T) {
	tmpDir := t.TempDir()
	specPath := filepath.Join(tmpDir, "openapi.json")

	// 两个操作没有 operationId，使用相同的 summary 触发名称去重
	spec := map[string]any{
		"openapi": "3.0.0",
		"info": map[string]any{
			"title":   "Test API",
			"version": "1.0.0",
		},
		"paths": map[string]any{
			"/a": map[string]any{
				"get": map[string]any{
					"summary":   "duplicate",
					"responses": map[string]any{"200": map[string]any{"description": "ok"}},
				},
			},
			"/b": map[string]any{
				"get": map[string]any{
					"summary":   "duplicate",
					"responses": map[string]any{"200": map[string]any{"description": "ok"}},
				},
			},
		},
	}

	specJSON, err := json.Marshal(spec)
	require.NoError(t, err)
	err = os.WriteFile(specPath, specJSON, 0644)
	require.NoError(t, err)

	config := types.NewMcpServerConfig("test", specPath, "openapi")
	client := NewOpenApiClient(config)

	err = client.Connect(context.Background())
	require.NoError(t, err)

	tools, err := client.ListTools(context.Background())
	require.NoError(t, err)
	assert.Len(t, tools, 2)

	toolNames := make([]string, len(tools))
	for i, tool := range tools {
		toolNames[i] = tool.Name
	}
	assert.Contains(t, toolNames, "duplicate")
	assert.Contains(t, toolNames, "duplicate_2")
}

func TestOpenApiClient_重复连接重置状态(t *testing.T) {
	tmpDir := t.TempDir()
	specPath := filepath.Join(tmpDir, "openapi.json")

	spec := map[string]any{
		"openapi": "3.0.0",
		"info":    map[string]any{"title": "Test", "version": "1.0.0"},
		"paths":   map[string]any{},
	}
	specJSON, _ := json.Marshal(spec)
	if err := os.WriteFile(specPath, specJSON, 0644); err != nil {
		t.Fatalf("写入 spec 文件失败: %v", err)
	}

	config := types.NewMcpServerConfig("test", specPath, "openapi")
	client := NewOpenApiClient(config)

	err := client.Connect(context.Background())
	require.NoError(t, err)

	// 第二次连接应重置状态
	err = client.Connect(context.Background())
	require.NoError(t, err)
}

func TestOpenApiClient_CallTool工具不存在(t *testing.T) {
	tmpDir := t.TempDir()
	specPath := filepath.Join(tmpDir, "openapi.json")

	spec := map[string]any{
		"openapi": "3.0.0",
		"info":    map[string]any{"title": "Test", "version": "1.0.0"},
		"paths":   map[string]any{},
	}
	specJSON, _ := json.Marshal(spec)
	if err := os.WriteFile(specPath, specJSON, 0644); err != nil {
		t.Fatalf("写入 spec 文件失败: %v", err)
	}

	config := types.NewMcpServerConfig("test", specPath, "openapi")
	client := NewOpenApiClient(config)

	err := client.Connect(context.Background())
	require.NoError(t, err)

	_, err = client.CallTool(context.Background(), "nonexistent", nil)
	assert.Error(t, err)
}

func TestOpenApiClient_CallTool执行HTTP请求(t *testing.T) {
	// 启动测试 HTTP 服务器
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == "GET" && r.URL.Path == "/items":
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode([]map[string]any{
				{"id": "1", "name": "item1"},
			})
		case r.Method == "POST" && r.URL.Path == "/items":
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(map[string]any{"id": "2", "name": "new"})
		case r.Method == "GET" && r.URL.Path == "/items/42":
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{"id": "42", "name": "item42"})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	tmpDir := t.TempDir()
	specPath := filepath.Join(tmpDir, "openapi.json")

	spec := map[string]any{
		"openapi": "3.0.0",
		"info": map[string]any{
			"title":   "Test API",
			"version": "1.0.0",
		},
		"servers": []map[string]any{
			{"url": server.URL},
		},
		"paths": map[string]any{
			"/items": map[string]any{
				"get": map[string]any{
					"operationId": "listItems",
					"summary":     "列出项目",
					"responses": map[string]any{
						"200": map[string]any{"description": "成功"},
					},
				},
				"post": map[string]any{
					"operationId": "createItem",
					"summary":     "创建项目",
					"requestBody": map[string]any{
						"required": true,
						"content": map[string]any{
							"application/json": map[string]any{
								"schema": map[string]any{
									"type": "object",
									"properties": map[string]any{
										"name": map[string]any{"type": "string"},
									},
									"required": []any{"name"},
								},
							},
						},
					},
					"responses": map[string]any{
						"201": map[string]any{"description": "创建成功"},
					},
				},
			},
			"/items/{id}": map[string]any{
				"get": map[string]any{
					"operationId": "getItem",
					"summary":     "获取项目",
					"parameters": []any{
						map[string]any{
							"name":     "id",
							"in":       "path",
							"required": true,
							"schema":   map[string]any{"type": "string"},
						},
					},
					"responses": map[string]any{
						"200": map[string]any{"description": "成功"},
					},
				},
			},
		},
	}

	specJSON, err := json.Marshal(spec)
	require.NoError(t, err)
	err = os.WriteFile(specPath, specJSON, 0644)
	require.NoError(t, err)

	config := types.NewMcpServerConfig("test-api", specPath, "openapi")
	client := NewOpenApiClient(config)

	err = client.Connect(context.Background())
	require.NoError(t, err)

	// 测试 GET 请求（无参数）
	result, err := client.CallTool(context.Background(), "listItems", map[string]any{})
	require.NoError(t, err)
	resultMap, ok := result.(map[string]any)
	require.True(t, ok)
	assert.Contains(t, resultMap, "content")

	// 测试 GET 请求（路径参数）
	result, err = client.CallTool(context.Background(), "getItem", map[string]any{"id": "42"})
	require.NoError(t, err)
	resultMap, ok = result.(map[string]any)
	require.True(t, ok)
	assert.Contains(t, resultMap, "content")

	// 测试 POST 请求（请求体）
	result, err = client.CallTool(context.Background(), "createItem", map[string]any{"name": "new"})
	require.NoError(t, err)
	resultMap, ok = result.(map[string]any)
	require.True(t, ok)
	assert.Contains(t, resultMap, "content")
}

func TestOpenApiClient_多文件规格(t *testing.T) {
	tmpDir := t.TempDir()

	// 第一个规格文件
	spec1 := map[string]any{
		"openapi": "3.0.0",
		"info":    map[string]any{"title": "API 1", "version": "1.0.0"},
		"paths": map[string]any{
			"/users": map[string]any{
				"get": map[string]any{
					"operationId": "listUsers",
					"responses":   map[string]any{"200": map[string]any{"description": "ok"}},
				},
			},
		},
	}

	// 第二个规格文件
	spec2 := map[string]any{
		"openapi": "3.0.0",
		"info":    map[string]any{"title": "API 2", "version": "1.0.0"},
		"paths": map[string]any{
			"/orders": map[string]any{
				"get": map[string]any{
					"operationId": "listOrders",
					"responses":   map[string]any{"200": map[string]any{"description": "ok"}},
				},
			},
		},
	}

	spec1JSON, _ := json.Marshal(spec1)
	spec2JSON, _ := json.Marshal(spec2)
	spec1Path := filepath.Join(tmpDir, "users.json")
	spec2Path := filepath.Join(tmpDir, "orders.json")
	require.NoError(t, os.WriteFile(spec1Path, spec1JSON, 0644))
	require.NoError(t, os.WriteFile(spec2Path, spec2JSON, 0644))

	combinedPath := fmt.Sprintf("%s,%s", spec1Path, spec2Path)
	config := types.NewMcpServerConfig("multi-api", combinedPath, "openapi")
	client := NewOpenApiClient(config)

	err := client.Connect(context.Background())
	require.NoError(t, err)

	tools, err := client.ListTools(context.Background())
	require.NoError(t, err)
	assert.Len(t, tools, 2)
}

// ──────────────────────────── generateToolName 测试 ────────────────────────────

func TestGenerateToolName(t *testing.T) {
	// 测试 operationID
	opWithID := &openapi3.Operation{OperationID: "listItems__v1"}
	name := generateToolName("GET", "/items", opWithID)
	assert.Equal(t, "listItems", name)

	// 测试 summary（无 operationID）
	opWithSummary := &openapi3.Operation{Summary: "List items"}
	name = generateToolName("GET", "/items", opWithSummary)
	assert.Equal(t, "List items", name)

	// 测试 method_path fallback（路径 /items → _items，与方法名之间产生双下划线）
	opFallback := &openapi3.Operation{}
	name = generateToolName("GET", "/items", opFallback)
	assert.Contains(t, name, "items")

	// 测试截断
	longID := ""
	for i := 0; i < 100; i++ {
		longID += "x"
	}
	opLongID := &openapi3.Operation{OperationID: longID}
	name = generateToolName("GET", "/items", opLongID)
	assert.LessOrEqual(t, len(name), 64)
}

// ──────────────────────────── getUniqueName 测试 ────────────────────────────

func TestGetUniqueName(t *testing.T) {
	// 创建客户端实例并测试 getUniqueName 方法
	c := NewOpenApiClient(&types.McpServerConfig{ServerName: "test"})

	// 首次使用，原样返回
	name1 := c.getUniqueName("listItems")
	assert.Equal(t, "listItems", name1)

	// 第二次使用，追加后缀
	name2 := c.getUniqueName("listItems")
	assert.Equal(t, "listItems_2", name2)

	// 第三次使用，追加后缀
	name3 := c.getUniqueName("listItems")
	assert.Equal(t, "listItems_3", name3)

	// 不同名称首次使用，原样返回
	name4 := c.getUniqueName("createItem")
	assert.Equal(t, "createItem", name4)
}

// ──────────────────────────── 辅助函数测试 ────────────────────────────

func TestExtractStringParam(t *testing.T) {
	tests := []struct {
		name   string
		params map[string]any
		key    string
		want   string
	}{
		{"nil参数", nil, "key", ""},
		{"键不存在", map[string]any{}, "key", ""},
		{"值非字符串", map[string]any{"key": 123}, "key", ""},
		{"正常值", map[string]any{"key": "value"}, "key", "value"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractStringParam(tt.params, tt.key)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestExtractStringSliceParam(t *testing.T) {
	tests := []struct {
		name   string
		params map[string]any
		key    string
		want   []string
	}{
		{"nil参数", nil, "args", nil},
		{"键不存在", map[string]any{}, "args", nil},
		{"值非切片", map[string]any{"args": "not-slice"}, "args", nil},
		{"混合类型切片", map[string]any{"args": []any{"hello", 123, "world"}}, "args", []string{"hello", "world"}},
		{"纯字符串切片", map[string]any{"args": []any{"a", "b"}}, "args", []string{"a", "b"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractStringSliceParam(tt.params, tt.key)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestExtractEnvSlice(t *testing.T) {
	tests := []struct {
		name   string
		params map[string]any
		want   []string
	}{
		{"nil参数", nil, nil},
		{"env不存在", map[string]any{}, nil},
		{"env非map", map[string]any{"env": "not-map"}, nil},
		{"正常env", map[string]any{"env": map[string]any{"KEY": "val"}}, []string{"KEY=val"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractEnvSlice(tt.params)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestLoadOpenAPISpec_目录路径报错(t *testing.T) {
	tmpDir := t.TempDir()
	_, err := loadOpenAPISpec(tmpDir)
	assert.Error(t, err)
}

func TestLoadOpenAPISpec_路径不存在(t *testing.T) {
	_, err := loadOpenAPISpec("/nonexistent/path/openapi.json")
	assert.Error(t, err)
}

// ──────────────────────────── SseClient 扩展测试 ────────────────────────────

// TestSseClient_未连接时GetToolInfo返回错误 测试未连接时 GetToolInfo 返回错误。
func TestSseClient_未连接时GetToolInfo返回错误(t *testing.T) {
	config := types.NewMcpServerConfig("test", "http://localhost:8080/sse", "sse")
	client := NewSseClient(config)
	_, err := client.GetToolInfo(context.Background(), "tool")
	assert.Error(t, err)
}

// TestSseClient_未连接时ListResources返回错误 测试未连接时 ListResources 返回错误。
func TestSseClient_未连接时ListResources返回错误(t *testing.T) {
	config := types.NewMcpServerConfig("test", "http://localhost:8080/sse", "sse")
	client := NewSseClient(config)
	_, err := client.ListResources(context.Background())
	assert.Error(t, err)
}

// TestSseClient_未连接时ReadResource返回错误 测试未连接时 ReadResource 返回错误。
func TestSseClient_未连接时ReadResource返回错误(t *testing.T) {
	config := types.NewMcpServerConfig("test", "http://localhost:8080/sse", "sse")
	client := NewSseClient(config)
	_, err := client.ReadResource(context.Background(), "uri")
	assert.Error(t, err)
}

// TestSseClient_Close 测试 Close 方法。
func TestSseClient_Close(t *testing.T) {
	config := types.NewMcpServerConfig("test", "http://localhost:8080/sse", "sse")
	client := NewSseClient(config)
	err := client.Close()
	assert.NoError(t, err)
}

// ──────────────────────────── StdioClient 扩展测试 ────────────────────────────

// TestStdioClient_未连接时GetToolInfo返回错误 测试未连接时 GetToolInfo 返回错误。
func TestStdioClient_未连接时GetToolInfo返回错误(t *testing.T) {
	config := types.NewMcpServerConfig("test", "npx", "stdio")
	client := NewStdioClient(config)
	_, err := client.GetToolInfo(context.Background(), "tool")
	assert.Error(t, err)
}

// TestStdioClient_未连接时ListResources返回错误 测试未连接时 ListResources 返回错误。
func TestStdioClient_未连接时ListResources返回错误(t *testing.T) {
	config := types.NewMcpServerConfig("test", "npx", "stdio")
	client := NewStdioClient(config)
	_, err := client.ListResources(context.Background())
	assert.Error(t, err)
}

// TestStdioClient_未连接时ReadResource返回错误 测试未连接时 ReadResource 返回错误。
func TestStdioClient_未连接时ReadResource返回错误(t *testing.T) {
	config := types.NewMcpServerConfig("test", "npx", "stdio")
	client := NewStdioClient(config)
	_, err := client.ReadResource(context.Background(), "uri")
	assert.Error(t, err)
}

// TestStdioClient_Close 测试 Close 方法。
func TestStdioClient_Close(t *testing.T) {
	config := types.NewMcpServerConfig("test", "npx", "stdio")
	client := NewStdioClient(config)
	err := client.Close()
	assert.NoError(t, err)
}

// ──────────────────────────── StreamableHttpClient 扩展测试 ────────────────────────────

// TestStreamableHttpClient_未连接时CallTool返回错误 测试未连接时 CallTool 返回错误。
func TestStreamableHttpClient_未连接时CallTool返回错误(t *testing.T) {
	config := types.NewMcpServerConfig("test", "http://localhost:8080/mcp", "streamable-http")
	client := NewStreamableHttpClient(config)
	_, err := client.CallTool(context.Background(), "tool", nil)
	assert.Error(t, err)
}

// TestStreamableHttpClient_未连接时GetToolInfo返回错误 测试未连接时 GetToolInfo 返回错误。
func TestStreamableHttpClient_未连接时GetToolInfo返回错误(t *testing.T) {
	config := types.NewMcpServerConfig("test", "http://localhost:8080/mcp", "streamable-http")
	client := NewStreamableHttpClient(config)
	_, err := client.GetToolInfo(context.Background(), "tool")
	assert.Error(t, err)
}

// TestStreamableHttpClient_未连接时ListResources返回错误 测试未连接时 ListResources 返回错误。
func TestStreamableHttpClient_未连接时ListResources返回错误(t *testing.T) {
	config := types.NewMcpServerConfig("test", "http://localhost:8080/mcp", "streamable-http")
	client := NewStreamableHttpClient(config)
	_, err := client.ListResources(context.Background())
	assert.Error(t, err)
}

// TestStreamableHttpClient_未连接时ReadResource返回错误 测试未连接时 ReadResource 返回错误。
func TestStreamableHttpClient_未连接时ReadResource返回错误(t *testing.T) {
	config := types.NewMcpServerConfig("test", "http://localhost:8080/mcp", "streamable-http")
	client := NewStreamableHttpClient(config)
	_, err := client.ReadResource(context.Background(), "uri")
	assert.Error(t, err)
}

// TestStreamableHttpClient_Close 测试 Close 方法。
func TestStreamableHttpClient_Close(t *testing.T) {
	config := types.NewMcpServerConfig("test", "http://localhost:8080/mcp", "streamable-http")
	client := NewStreamableHttpClient(config)
	err := client.Close()
	assert.NoError(t, err)
}

// ──────────────────────────── PlaywrightClient 扩展测试 ────────────────────────────

// TestPlaywrightClient_未初始化时CallTool返回错误 测试未初始化时 CallTool 返回错误。
func TestPlaywrightClient_未初始化时CallTool返回错误(t *testing.T) {
	config := types.NewMcpServerConfig("test", "npx", "playwright")
	client := NewPlaywrightClient(config)
	_, err := client.CallTool(context.Background(), "tool", nil)
	assert.Error(t, err)
}

// TestPlaywrightClient_未初始化时GetToolInfo返回错误 测试未初始化时 GetToolInfo 返回错误。
func TestPlaywrightClient_未初始化时GetToolInfo返回错误(t *testing.T) {
	config := types.NewMcpServerConfig("test", "npx", "playwright")
	client := NewPlaywrightClient(config)
	_, err := client.GetToolInfo(context.Background(), "tool")
	assert.Error(t, err)
}

// TestPlaywrightClient_未初始化时ListResources返回错误 测试未初始化时 ListResources 返回错误。
func TestPlaywrightClient_未初始化时ListResources返回错误(t *testing.T) {
	config := types.NewMcpServerConfig("test", "npx", "playwright")
	client := NewPlaywrightClient(config)
	_, err := client.ListResources(context.Background())
	assert.Error(t, err)
}

// TestPlaywrightClient_未初始化时ReadResource返回错误 测试未初始化时 ReadResource 返回错误。
func TestPlaywrightClient_未初始化时ReadResource返回错误(t *testing.T) {
	config := types.NewMcpServerConfig("test", "npx", "playwright")
	client := NewPlaywrightClient(config)
	_, err := client.ReadResource(context.Background(), "uri")
	assert.Error(t, err)
}

// TestPlaywrightClient_Close 测试 Close 方法。
func TestPlaywrightClient_Close(t *testing.T) {
	config := types.NewMcpServerConfig("test", "npx", "playwright")
	client := NewPlaywrightClient(config)
	err := client.Close()
	assert.NoError(t, err)
}

// ──────────────────────────── PlaywrightClient 委托测试 ────────────────────────────

// fakeMcpClient 用于测试的模拟 MCP 客户端。
type fakeMcpClient struct {
	tools         []*types.McpToolCard
	callResult    any
	callErr       error
	listToolsErr  error
	disconnectErr error
}

func (f *fakeMcpClient) Connect(_ context.Context, _ ...types.ConnectOption) error {
	return nil
}

func (f *fakeMcpClient) Disconnect(_ context.Context) error {
	return f.disconnectErr
}

func (f *fakeMcpClient) ListTools(_ context.Context) ([]*types.McpToolCard, error) {
	return f.tools, f.listToolsErr
}

func (f *fakeMcpClient) CallTool(_ context.Context, _ string, _ map[string]any) (any, error) {
	return f.callResult, f.callErr
}

func (f *fakeMcpClient) GetToolInfo(_ context.Context, toolName string) (*types.McpToolCard, error) {
	for _, card := range f.tools {
		if card.Name == toolName {
			return card, nil
		}
	}
	return nil, fmt.Errorf("tool %q not found", toolName)
}

func (f *fakeMcpClient) ListResources(_ context.Context) ([]map[string]any, error) {
	return nil, nil
}

func (f *fakeMcpClient) ReadResource(_ context.Context, _ string) ([]map[string]any, error) {
	return nil, nil
}

func (f *fakeMcpClient) Close() error {
	return f.disconnectErr
}

// TestPlaywrightClient_委托ListTools 测试通过委托调用 ListTools。
func TestPlaywrightClient_委托ListTools(t *testing.T) {
	config := types.NewMcpServerConfig("test", "npx", "playwright")
	client := NewPlaywrightClient(config)
	client.delegate = &fakeMcpClient{
		tools: []*types.McpToolCard{
			types.NewMcpToolCard("tool1", "desc1", "test", nil),
		},
	}

	tools, err := client.ListTools(context.Background())
	assert.NoError(t, err)
	assert.Len(t, tools, 1)
	assert.Equal(t, "tool1", tools[0].Name)
}

// TestPlaywrightClient_委托CallTool 测试通过委托调用 CallTool。
func TestPlaywrightClient_委托CallTool(t *testing.T) {
	config := types.NewMcpServerConfig("test", "npx", "playwright")
	client := NewPlaywrightClient(config)
	client.delegate = &fakeMcpClient{
		callResult: map[string]any{"result": "ok"},
	}

	result, err := client.CallTool(context.Background(), "tool1", map[string]any{"key": "val"})
	assert.NoError(t, err)
	assert.NotNil(t, result)
}

// TestPlaywrightClient_委托CallTool错误 测试委托 CallTool 返回错误。
func TestPlaywrightClient_委托CallTool错误(t *testing.T) {
	config := types.NewMcpServerConfig("test", "npx", "playwright")
	client := NewPlaywrightClient(config)
	client.delegate = &fakeMcpClient{
		callErr: fmt.Errorf("调用失败"),
	}

	_, err := client.CallTool(context.Background(), "tool1", nil)
	assert.Error(t, err)
}

// TestPlaywrightClient_委托GetToolInfo 测试通过委托调用 GetToolInfo。
func TestPlaywrightClient_委托GetToolInfo(t *testing.T) {
	config := types.NewMcpServerConfig("test", "npx", "playwright")
	client := NewPlaywrightClient(config)
	client.delegate = &fakeMcpClient{
		tools: []*types.McpToolCard{
			types.NewMcpToolCard("tool1", "desc1", "test", nil),
		},
	}

	toolInfo, err := client.GetToolInfo(context.Background(), "tool1")
	assert.NoError(t, err)
	assert.Equal(t, "tool1", toolInfo.Name)
}

// TestPlaywrightClient_委托GetToolInfo工具不存在 测试委托 GetToolInfo 工具不存在。
func TestPlaywrightClient_委托GetToolInfo工具不存在(t *testing.T) {
	config := types.NewMcpServerConfig("test", "npx", "playwright")
	client := NewPlaywrightClient(config)
	client.delegate = &fakeMcpClient{tools: []*types.McpToolCard{}}

	_, err := client.GetToolInfo(context.Background(), "nonexistent")
	assert.Error(t, err)
}

// TestPlaywrightClient_委托ListResources 测试通过委托调用 ListResources。
func TestPlaywrightClient_委托ListResources(t *testing.T) {
	config := types.NewMcpServerConfig("test", "npx", "playwright")
	client := NewPlaywrightClient(config)
	client.delegate = &fakeMcpClient{}

	resources, err := client.ListResources(context.Background())
	assert.NoError(t, err)
	assert.Nil(t, resources)
}

// TestPlaywrightClient_委托ReadResource 测试通过委托调用 ReadResource。
func TestPlaywrightClient_委托ReadResource(t *testing.T) {
	config := types.NewMcpServerConfig("test", "npx", "playwright")
	client := NewPlaywrightClient(config)
	client.delegate = &fakeMcpClient{}

	result, err := client.ReadResource(context.Background(), "uri")
	assert.NoError(t, err)
	assert.Nil(t, result)
}

// TestPlaywrightClient_委托Disconnect 测试通过委托断开连接。
func TestPlaywrightClient_委托Disconnect(t *testing.T) {
	config := types.NewMcpServerConfig("test", "npx", "playwright")
	client := NewPlaywrightClient(config)
	client.delegate = &fakeMcpClient{}

	err := client.Disconnect(context.Background())
	assert.NoError(t, err)
}

// TestPlaywrightClient_委托Disconnect错误 测试委托 Disconnect 返回错误。
func TestPlaywrightClient_委托Disconnect错误(t *testing.T) {
	config := types.NewMcpServerConfig("test", "npx", "playwright")
	client := NewPlaywrightClient(config)
	client.delegate = &fakeMcpClient{disconnectErr: fmt.Errorf("断开失败")}

	err := client.Disconnect(context.Background())
	assert.Error(t, err)
}

// TestPlaywrightClient_委托ListTools错误 测试委托 ListTools 返回错误。
func TestPlaywrightClient_委托ListTools错误(t *testing.T) {
	config := types.NewMcpServerConfig("test", "npx", "playwright")
	client := NewPlaywrightClient(config)
	client.delegate = &fakeMcpClient{listToolsErr: fmt.Errorf("列出工具失败")}

	_, err := client.ListTools(context.Background())
	assert.Error(t, err)
}

// ──────────────────────────── OpenApiClient 扩展测试 ────────────────────────────

// TestOpenApiClient_未连接时GetToolInfo返回错误 测试未连接时 GetToolInfo 返回错误。
func TestOpenApiClient_未连接时GetToolInfo返回错误(t *testing.T) {
	config := types.NewMcpServerConfig("test", "openapi.json", "openapi")
	client := NewOpenApiClient(config)
	_, err := client.GetToolInfo(context.Background(), "tool")
	assert.Error(t, err)
}

// TestOpenApiClient_Close 测试 Close 方法。
func TestOpenApiClient_Close(t *testing.T) {
	config := types.NewMcpServerConfig("test", "openapi.json", "openapi")
	client := NewOpenApiClient(config)
	// 未连接时 Close 应无错误
	err := client.Close()
	assert.NoError(t, err)
}

// TestOpenApiClient_Close已连接 测试已连接时 Close 断开连接。
func TestOpenApiClient_Close已连接(t *testing.T) {
	tmpDir := t.TempDir()
	specPath := filepath.Join(tmpDir, "openapi.json")

	spec := map[string]any{
		"openapi": "3.0.0",
		"info":    map[string]any{"title": "Test", "version": "1.0.0"},
		"paths":   map[string]any{},
	}
	specJSON, _ := json.Marshal(spec)
	require.NoError(t, os.WriteFile(specPath, specJSON, 0644))

	config := types.NewMcpServerConfig("test", specPath, "openapi")
	client := NewOpenApiClient(config)

	err := client.Connect(context.Background())
	require.NoError(t, err)
	assert.True(t, client.isConnected)

	err = client.Close()
	assert.NoError(t, err)
	assert.False(t, client.isConnected)
}

// TestOpenApiClient_GetToolInfo工具不存在 测试 GetToolInfo 工具不存在返回错误。
func TestOpenApiClient_GetToolInfo工具不存在(t *testing.T) {
	tmpDir := t.TempDir()
	specPath := filepath.Join(tmpDir, "openapi.json")

	spec := map[string]any{
		"openapi": "3.0.0",
		"info":    map[string]any{"title": "Test", "version": "1.0.0"},
		"paths":   map[string]any{},
	}
	specJSON, _ := json.Marshal(spec)
	require.NoError(t, os.WriteFile(specPath, specJSON, 0644))

	config := types.NewMcpServerConfig("test", specPath, "openapi")
	client := NewOpenApiClient(config)

	err := client.Connect(context.Background())
	require.NoError(t, err)

	_, err = client.GetToolInfo(context.Background(), "nonexistent")
	assert.Error(t, err)
}

// TestOpenApiClient_CallToolHTTP错误 测试 CallTool 返回 HTTP 错误响应。
func TestOpenApiClient_CallToolHTTP错误(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]any{"error": "internal error"})
	}))
	defer server.Close()

	tmpDir := t.TempDir()
	specPath := filepath.Join(tmpDir, "openapi.json")

	spec := map[string]any{
		"openapi": "3.0.0",
		"info":    map[string]any{"title": "Test", "version": "1.0.0"},
		"servers": []map[string]any{{"url": server.URL}},
		"paths": map[string]any{
			"/fail": map[string]any{
				"get": map[string]any{
					"operationId": "failCall",
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

	_, err = client.CallTool(context.Background(), "failCall", map[string]any{})
	assert.Error(t, err)
}

// TestOpenApiClient_CallTool带OutputSchema 测试 CallTool 带输出 Schema 时返回 structuredContent。
func TestOpenApiClient_CallTool带OutputSchema(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"id": "1", "name": "test"})
	}))
	defer server.Close()

	tmpDir := t.TempDir()
	specPath := filepath.Join(tmpDir, "openapi.json")

	spec := map[string]any{
		"openapi": "3.0.0",
		"info":    map[string]any{"title": "Test", "version": "1.0.0"},
		"servers": []map[string]any{{"url": server.URL}},
		"paths": map[string]any{
			"/items": map[string]any{
				"get": map[string]any{
					"operationId": "getItems",
					"summary":     "获取项目",
					"responses": map[string]any{
						"200": map[string]any{
							"description": "成功",
							"content": map[string]any{
								"application/json": map[string]any{
									"schema": map[string]any{
										"type": "object",
										"properties": map[string]any{
											"id":   map[string]any{"type": "string"},
											"name": map[string]any{"type": "string"},
										},
									},
								},
							},
						},
					},
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

	result, err := client.CallTool(context.Background(), "getItems", map[string]any{})
	require.NoError(t, err)
	resultMap := result.(map[string]any)
	assert.Contains(t, resultMap, "content")
	// object 类型响应应有 structuredContent
	assert.Contains(t, resultMap, "structuredContent")
}

// TestOpenApiClient_空ServerPath 测试空 ServerPath 连接不报错。
func TestOpenApiClient_空ServerPath(t *testing.T) {
	config := types.NewMcpServerConfig("test", "", "openapi")
	client := NewOpenApiClient(config)

	err := client.Connect(context.Background())
	// 空 ServerPath 不会加载文件，直接成功
	assert.NoError(t, err)
}

// TestOpenApiClient_CallToolHTTP4xx错误 测试 CallTool 返回 HTTP 4xx 错误响应。
func TestOpenApiClient_CallToolHTTP4xx错误(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]any{"error": "bad request"})
	}))
	defer server.Close()

	tmpDir := t.TempDir()
	specPath := filepath.Join(tmpDir, "openapi.json")

	spec := map[string]any{
		"openapi": "3.0.0",
		"info":    map[string]any{"title": "Test", "version": "1.0.0"},
		"servers": []map[string]any{{"url": server.URL}},
		"paths": map[string]any{
			"/bad": map[string]any{
				"post": map[string]any{
					"operationId": "badCall",
					"requestBody": map[string]any{
						"required": true,
						"content": map[string]any{
							"application/json": map[string]any{
								"schema": map[string]any{
									"type": "object",
									"properties": map[string]any{
										"name": map[string]any{"type": "string"},
									},
								},
							},
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

	config := types.NewMcpServerConfig("test", specPath, "openapi")
	client := NewOpenApiClient(config)

	err = client.Connect(context.Background())
	require.NoError(t, err)

	_, err = client.CallTool(context.Background(), "badCall", map[string]any{"name": "test"})
	assert.Error(t, err)
}

// TestOpenApiClient_CallToolWrapResult 测试 CallTool 非 object 类型响应使用 x-fastmcp-wrap-result。
func TestOpenApiClient_CallToolWrapResult(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode("hello world")
	}))
	defer server.Close()

	tmpDir := t.TempDir()
	specPath := filepath.Join(tmpDir, "openapi.json")

	spec := map[string]any{
		"openapi": "3.0.0",
		"info":    map[string]any{"title": "Test", "version": "1.0.0"},
		"servers": []map[string]any{{"url": server.URL}},
		"paths": map[string]any{
			"/greeting": map[string]any{
				"get": map[string]any{
					"operationId": "getGreeting",
					"responses": map[string]any{
						"200": map[string]any{
							"description": "成功",
							"content": map[string]any{
								"application/json": map[string]any{
									"schema": map[string]any{
										"type": "string",
									},
								},
							},
						},
					},
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

	result, err := client.CallTool(context.Background(), "getGreeting", map[string]any{})
	require.NoError(t, err)
	resultMap := result.(map[string]any)
	assert.Contains(t, resultMap, "content")
	// string 类型响应应有 structuredContent（wrap result）
	assert.Contains(t, resultMap, "structuredContent")
	structuredContent := resultMap["structuredContent"].(map[string]any)
	assert.Contains(t, structuredContent, "result")
}

// TestLoadOpenAPISpec_无效JSON 测试无效 JSON 内容返回错误。
func TestLoadOpenAPISpec_无效JSON(t *testing.T) {
	tmpDir := t.TempDir()
	specPath := filepath.Join(tmpDir, "openapi.json")
	require.NoError(t, os.WriteFile(specPath, []byte("{invalid json}"), 0644))

	_, err := loadOpenAPISpec(specPath)
	assert.Error(t, err)
}

// TestLoadOpenAPISpec_无效YAML 测试无效 YAML 内容返回错误。
func TestLoadOpenAPISpec_无效YAML(t *testing.T) {
	tmpDir := t.TempDir()
	specPath := filepath.Join(tmpDir, "openapi.yaml")
	require.NoError(t, os.WriteFile(specPath, []byte(":\n  :\n    - [invalid"), 0644))

	// 重点是 loadOpenAPISpec 不 panic
	assert.NotPanics(t, func() {
		_, _ = loadOpenAPISpec(specPath)
	})
}

// TestLoadOpenAPISpec_符号链接 测试符号链接文件返回错误。
func TestLoadOpenAPISpec_符号链接(t *testing.T) {
	tmpDir := t.TempDir()
	realPath := filepath.Join(tmpDir, "real_openapi.json")
	linkPath := filepath.Join(tmpDir, "openapi.json")

	spec := map[string]any{
		"openapi": "3.0.0",
		"info":    map[string]any{"title": "Test", "version": "1.0.0"},
		"paths":   map[string]any{},
	}
	specJSON, err := json.Marshal(spec)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(realPath, specJSON, 0644))
	require.NoError(t, os.Symlink(realPath, linkPath))

	_, err = loadOpenAPISpec(linkPath)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "符号链接")
}

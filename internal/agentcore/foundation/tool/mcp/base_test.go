package mcp

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool/mcp/types"
	"github.com/uapclaw/uapclaw-go/internal/common/schema"
)

func TestNewMcpServerConfig_默认值(t *testing.T) {
	config := types.NewMcpServerConfig("test-server", "http://localhost:8080/sse", "sse")
	assert.Equal(t, "test-server", config.ServerName)
	assert.Equal(t, "http://localhost:8080/sse", config.ServerPath)
	assert.Equal(t, "sse", config.ClientType)
	assert.NotEmpty(t, config.ServerID)
	assert.Nil(t, config.Params)
	assert.Nil(t, config.AuthHeaders)
	assert.Nil(t, config.AuthQueryParams)
}

func TestNewMcpServerConfig_ClientType默认为SSE(t *testing.T) {
	config := types.NewMcpServerConfig("test", "http://localhost:8080/sse", "")
	assert.Equal(t, "sse", config.ClientType)
}

func TestWithServerID(t *testing.T) {
	config := types.NewMcpServerConfig("test", "http://localhost:8080/sse", "sse",
		types.WithServerID("my-id"),
	)
	assert.Equal(t, "my-id", config.ServerID)
}

func TestWithParams(t *testing.T) {
	params := map[string]any{"command": "npx", "args": []any{"@playwright/mcp"}}
	config := types.NewMcpServerConfig("test", "npx", "stdio",
		types.WithParams(params),
	)
	assert.Equal(t, params, config.Params)
}

func TestWithAuthHeaders(t *testing.T) {
	headers := map[string]string{"Authorization": "Bearer xxx"}
	config := types.NewMcpServerConfig("test", "http://localhost:8080/sse", "sse",
		types.WithAuthHeaders(headers),
	)
	assert.Equal(t, headers, config.AuthHeaders)
}

func TestWithAuthQueryParams(t *testing.T) {
	params := map[string]string{"token": "abc"}
	config := types.NewMcpServerConfig("test", "http://localhost:8080/sse", "sse",
		types.WithAuthQueryParams(params),
	)
	assert.Equal(t, params, config.AuthQueryParams)
}

func TestNewMcpToolCard_基本构造(t *testing.T) {
	params := []*schema.Param{
		{Name: "query", Type: schema.ParamTypeString, Required: true, Description: "搜索关键词"},
	}
	card := types.NewMcpToolCard("web_search", "搜索网页", "search-server", params, nil)
	assert.Equal(t, "web_search", card.Name)
	assert.Equal(t, "搜索网页", card.Description)
	assert.Equal(t, "search-server", card.ServerName)
	assert.Equal(t, "", card.ServerID)
	assert.Equal(t, params, card.InputParams)
}

func TestMcpToolCard_ToolInfo(t *testing.T) {
	params := []*schema.Param{
		{Name: "query", Type: schema.ParamTypeString, Required: true, Description: "搜索关键词"},
	}
	card := types.NewMcpToolCard("web_search", "搜索网页", "search-server", params, nil)
	info := card.ToolInfo()
	assert.Equal(t, "web_search", info.Name)
	assert.Equal(t, "搜索网页", info.Description)
	assert.Equal(t, "search-server", info.ServerName)
	assert.Equal(t, "function", info.Type)
	assert.NotNil(t, info.Parameters)
}

func TestNewMcpToolCard_WithServerID(t *testing.T) {
	card := types.NewMcpToolCard("tool", "desc", "server", nil, nil,
		types.WithMcpToolCardServerID("my-server-id"),
	)
	assert.Equal(t, "my-server-id", card.ServerID)
}

func TestExtractMCPToolResultContent_文本内容(t *testing.T) {
	result := map[string]any{
		"content": []any{
			map[string]any{"type": "text", "text": "hello world"},
		},
	}
	got := ExtractMCPToolResultContent(result)
	assert.Equal(t, "hello world", got)
}

func TestExtractMCPToolResultContent_图片内容(t *testing.T) {
	result := map[string]any{
		"content": []any{
			map[string]any{"type": "image", "mimeType": "image/png", "data": "iVBORw0KGgo="},
		},
	}
	got := ExtractMCPToolResultContent(result)
	assert.Contains(t, got, "[image content: image/png")
	assert.Contains(t, got, "base64 chars]")
}

func TestExtractMCPToolResultContent_非图片Data(t *testing.T) {
	result := map[string]any{
		"content": []any{
			map[string]any{"type": "resource", "data": "raw-data-here"},
		},
	}
	got := ExtractMCPToolResultContent(result)
	assert.Equal(t, "raw-data-here", got)
}

func TestExtractMCPToolResultContent_空Content(t *testing.T) {
	result := map[string]any{"content": []any{}}
	got := ExtractMCPToolResultContent(result)
	assert.Nil(t, got)
}

func TestExtractMCPToolResultContent_无Content字段(t *testing.T) {
	result := map[string]any{}
	got := ExtractMCPToolResultContent(result)
	assert.Nil(t, got)
}

func TestExtractMCPToolResultContent_取最后一个Content(t *testing.T) {
	result := map[string]any{
		"content": []any{
			map[string]any{"type": "text", "text": "first"},
			map[string]any{"type": "text", "text": "last"},
		},
	}
	got := ExtractMCPToolResultContent(result)
	assert.Equal(t, "last", got)
}

func TestNewMCPTool_客户端为nil时返回错误(t *testing.T) {
	card := types.NewMcpToolCard("tool", "desc", "server", nil, nil)
	_, err := NewMCPTool(nil, card)
	assert.Error(t, err)
}

func TestMCPTool_Card(t *testing.T) {
	card := types.NewMcpToolCard("tool", "desc", "server", nil, nil)
	fake := &fakeMcpClient{}
	mcpTool, _ := NewMCPTool(fake, card)
	assert.NotNil(t, mcpTool.Card())
}

func TestMCPTool_Invoke_直接传参(t *testing.T) {
	card := types.NewMcpToolCard("tool", "desc", "server", nil, nil) // InputParams 为 nil
	fake := &fakeMcpClient{
		callToolFunc: func(_ context.Context, toolName string, arguments map[string]any) (any, error) {
			assert.Equal(t, "tool", toolName)
			assert.Equal(t, map[string]any{"key": "val"}, arguments)
			return map[string]any{"content": []any{
				map[string]any{"type": "text", "text": "result"},
			}}, nil
		},
	}
	mcpTool, _ := NewMCPTool(fake, card)
	result, err := mcpTool.Invoke(context.Background(), map[string]any{"key": "val"})
	assert.NoError(t, err)
	assert.Equal(t, "result", result["result"])
}

func TestMCPTool_Invoke_参数格式化(t *testing.T) {
	params := []*schema.Param{
		{Name: "query", Type: schema.ParamTypeString, Required: true},
	}
	card := types.NewMcpToolCard("tool", "desc", "server", params, nil)
	fake := &fakeMcpClient{
		callToolFunc: func(_ context.Context, _ string, _ map[string]any) (any, error) {
			return map[string]any{"content": []any{
				map[string]any{"type": "text", "text": "ok"},
			}}, nil
		},
	}
	mcpTool, _ := NewMCPTool(fake, card)
	result, err := mcpTool.Invoke(context.Background(), map[string]any{"query": "test"})
	assert.NoError(t, err)
	assert.Equal(t, "ok", result["result"])
}

func TestMCPTool_Invoke_客户端调用失败(t *testing.T) {
	card := types.NewMcpToolCard("tool", "desc", "server", nil, nil)
	fake := &fakeMcpClient{
		callToolFunc: func(_ context.Context, _ string, _ map[string]any) (any, error) {
			return nil, fmt.Errorf("connection lost")
		},
	}
	mcpTool, _ := NewMCPTool(fake, card)
	_, err := mcpTool.Invoke(context.Background(), map[string]any{})
	assert.Error(t, err)
}

func TestMCPTool_Stream_不支持(t *testing.T) {
	card := types.NewMcpToolCard("tool", "desc", "server", nil, nil)
	fake := &fakeMcpClient{}
	mcpTool, _ := NewMCPTool(fake, card)
	_, err := mcpTool.Stream(context.Background(), map[string]any{})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "stream is not support")
}

// ──────────────────────────── 测试辅助 ────────────────────────────

// fakeMcpClient 用于 MCPTool 单元测试的模拟客户端
type fakeMcpClient struct {
	callToolFunc  func(ctx context.Context, toolName string, arguments map[string]any) (any, error)
	listToolsFunc func(ctx context.Context) ([]*types.McpToolCard, error)
}

func (f *fakeMcpClient) Connect(_ context.Context, _ ...types.ConnectOption) error { return nil }
func (f *fakeMcpClient) Disconnect(_ context.Context) error                        { return nil }
func (f *fakeMcpClient) ListTools(ctx context.Context) ([]*types.McpToolCard, error) {
	if f.listToolsFunc != nil {
		return f.listToolsFunc(ctx)
	}
	return nil, nil
}
func (f *fakeMcpClient) CallTool(ctx context.Context, toolName string, arguments map[string]any) (any, error) {
	if f.callToolFunc != nil {
		return f.callToolFunc(ctx, toolName, arguments)
	}
	return nil, nil
}
func (f *fakeMcpClient) GetToolInfo(_ context.Context, _ string) (*types.McpToolCard, error) {
	return nil, nil
}
func (f *fakeMcpClient) ListResources(_ context.Context) ([]any, error) { return nil, nil }
func (f *fakeMcpClient) ReadResource(_ context.Context, _ string) (any, error) {
	return nil, nil
}
func (f *fakeMcpClient) Close() error { return nil }

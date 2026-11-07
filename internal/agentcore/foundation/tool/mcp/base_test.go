package mcp

import (
	"context"
	"fmt"
	"strings"
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
	card := types.NewMcpToolCard("web_search", "搜索网页", "search-server", params)
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
	card := types.NewMcpToolCard("web_search", "搜索网页", "search-server", params)
	info := card.ToolInfo()
	assert.Equal(t, "web_search", info.GetName())
	assert.Equal(t, "搜索网页", info.GetDescription())
	assert.Equal(t, "function", info.GetType())
	assert.NotNil(t, info.GetParameters())
	// McpToolInfo 特有字段通过类型断言获取
	if mcpInfo, ok := info.(*schema.McpToolInfo); ok {
		assert.Equal(t, "search-server", mcpInfo.ServerName)
	}
}

func TestNewMcpToolCard_WithServerID(t *testing.T) {
	card := types.NewMcpToolCard("tool", "desc", "server", nil,
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
	got := ExtractMCPToolResultContent(result, false, "t")
	assert.Equal(t, "hello world", got)
}

func TestExtractMCPToolResultContent_图片内容(t *testing.T) {
	result := map[string]any{
		"content": []any{
			map[string]any{"type": "image", "mimeType": "image/png", "data": "iVBORw0KGgo="},
		},
	}
	got := ExtractMCPToolResultContent(result, false, "t")
	assert.Contains(t, got, "[image content: image/png")
	assert.Contains(t, got, "base64 chars]")
}

func TestExtractMCPToolResultContent_非图片Data(t *testing.T) {
	result := map[string]any{
		"content": []any{
			map[string]any{"type": "resource", "data": "raw-data-here"},
		},
	}
	got := ExtractMCPToolResultContent(result, false, "t")
	assert.Equal(t, "raw-data-here", got)
}

func TestExtractMCPToolResultContent_空Content(t *testing.T) {
	result := map[string]any{"content": []any{}}
	got := ExtractMCPToolResultContent(result, false, "t")
	assert.Nil(t, got)
}

func TestExtractMCPToolResultContent_无Content字段(t *testing.T) {
	result := map[string]any{}
	got := ExtractMCPToolResultContent(result, false, "t")
	assert.Nil(t, got)
}

// TestExtractMCPToolResultContent_保留全部Content 验证多块文本按序拼接。
//
// 语义变更（D-15）：旧实现只取最后一块，多块结果的文本部分全部丢失。
// 上游改为按顺序 join，块间以空行分隔。
func TestExtractMCPToolResultContent_保留全部Content(t *testing.T) {
	result := map[string]any{
		"content": []any{
			map[string]any{"type": "text", "text": "first"},
			map[string]any{"type": "text", "text": "last"},
		},
	}
	got := ExtractMCPToolResultContent(result, false, "t")
	assert.Equal(t, "first\n\nlast", got)
}

func TestNewMCPTool_客户端为nil时返回错误(t *testing.T) {
	card := types.NewMcpToolCard("tool", "desc", "server", nil)
	_, err := NewMCPTool(nil, card, false)
	assert.Error(t, err)
}

func TestMCPTool_Card(t *testing.T) {
	card := types.NewMcpToolCard("tool", "desc", "server", nil)
	fake := &fakeMcpClient{}
	mcpTool, _ := NewMCPTool(fake, card, false)
	assert.NotNil(t, mcpTool.Card())
}

func TestMCPTool_Invoke_直接传参(t *testing.T) {
	card := types.NewMcpToolCard("tool", "desc", "server", nil) // InputParams 为 nil
	fake := &fakeMcpClient{
		callToolFunc: func(_ context.Context, toolName string, arguments map[string]any) (any, error) {
			assert.Equal(t, "tool", toolName)
			assert.Equal(t, map[string]any{"key": "val"}, arguments)
			return map[string]any{"content": []any{
				map[string]any{"type": "text", "text": "result"},
			}}, nil
		},
	}
	mcpTool, _ := NewMCPTool(fake, card, false)
	result, err := mcpTool.Invoke(context.Background(), map[string]any{"key": "val"})
	assert.NoError(t, err)
	assert.Equal(t, "result", result["result"])
}

func TestMCPTool_Invoke_参数格式化(t *testing.T) {
	params := []*schema.Param{
		{Name: "query", Type: schema.ParamTypeString, Required: true},
	}
	card := types.NewMcpToolCard("tool", "desc", "server", params)
	fake := &fakeMcpClient{
		callToolFunc: func(_ context.Context, _ string, _ map[string]any) (any, error) {
			return map[string]any{"content": []any{
				map[string]any{"type": "text", "text": "ok"},
			}}, nil
		},
	}
	mcpTool, _ := NewMCPTool(fake, card, false)
	result, err := mcpTool.Invoke(context.Background(), map[string]any{"query": "test"})
	assert.NoError(t, err)
	assert.Equal(t, "ok", result["result"])
}

func TestMCPTool_Invoke_客户端调用失败(t *testing.T) {
	card := types.NewMcpToolCard("tool", "desc", "server", nil)
	fake := &fakeMcpClient{
		callToolFunc: func(_ context.Context, _ string, _ map[string]any) (any, error) {
			return nil, fmt.Errorf("connection lost")
		},
	}
	mcpTool, _ := NewMCPTool(fake, card, false)
	_, err := mcpTool.Invoke(context.Background(), map[string]any{})
	assert.Error(t, err)
}

func TestMCPTool_Stream_不支持(t *testing.T) {
	card := types.NewMcpToolCard("tool", "desc", "server", nil)
	fake := &fakeMcpClient{}
	mcpTool, _ := NewMCPTool(fake, card, false)
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
func (f *fakeMcpClient) ListResources(_ context.Context) ([]map[string]any, error) { return nil, nil }
func (f *fakeMcpClient) ReadResource(_ context.Context, _ string) ([]map[string]any, error) {
	return nil, nil
}
func (f *fakeMcpClient) Close() error { return nil }

// ──────────────────────────── D-15/D-16：多块抽取与状态保留 ────────────────────────────

// TestExtractMCPToolResultContent_多文本块拼接 验证不再只取最后一块。
//
// 回归：旧实现 item := contentList[len-1] 只保留最后一个块，
// "动作确认文本 + 截图" 这类结果会把前面的文本全丢掉。
func TestExtractMCPToolResultContent_多文本块拼接(t *testing.T) {
	raw := map[string]any{
		"isError": false,
		"content": []any{
			map[string]any{"type": "text", "text": "ok"},
			map[string]any{"type": "text", "text": "detail"},
		},
	}
	got := ExtractMCPToolResultContent(raw, false, "t")
	if s, _ := got.(string); s != "ok\n\ndetail" {
		t.Fatalf("应为 ok\\n\\ndetail，实际 %#v", got)
	}
}

// TestExtractMCPToolResultContent_图片占位符 验证默认把图片转成文本占位符
func TestExtractMCPToolResultContent_图片占位符(t *testing.T) {
	raw := map[string]any{
		"isError": false,
		"content": []any{
			map[string]any{"type": "text", "text": "clicked"},
			map[string]any{"type": "image", "mimeType": "image/png", "data": "AAAA"},
		},
	}
	got := ExtractMCPToolResultContent(raw, false, "t")
	s, _ := got.(string)
	if !strings.Contains(s, "clicked") || !strings.Contains(s, "[image content: image/png, 4 base64 chars]") {
		t.Fatalf("应含文本与图片占位符，实际 %q", s)
	}
}

// TestExtractMCPToolResultContent_图片多模态 验证开启开关后产出 multimodal 结构
func TestExtractMCPToolResultContent_图片多模态(t *testing.T) {
	raw := map[string]any{
		"isError": false,
		"content": []any{
			map[string]any{"type": "text", "text": "shot"},
			map[string]any{"type": "image", "mimeType": "image/png", "data": "AAAA"},
		},
	}
	got := ExtractMCPToolResultContent(raw, true, "srv")
	m, ok := got.(*MCPToolResult)
	if !ok {
		t.Fatalf("应返回 *MCPToolResult，实际 %T", got)
	}
	if !m.Success {
		t.Fatal("成功结果 Success 应为 true")
	}
	if content, _ := m.Data["content"].(string); !strings.Contains(content, "1 image(s) attached as multimodal input.") {
		t.Fatalf("content 应含附加说明，实际 %q", content)
	}
	items, _ := m.Data["multimodal"].([]any)
	if len(items) != 1 {
		t.Fatalf("应有 1 个 multimodal 项，实际 %d", len(items))
	}
	item := items[0].(map[string]any)
	if item["source"] != "mcp" || item["source_path"] != "srv" {
		t.Fatalf("来源字段不符，实际 %v", item)
	}
	if item["data_url"] != "data:image/png;base64,AAAA" {
		t.Fatalf("data_url 不符，实际 %v", item["data_url"])
	}
}

// TestExtractMCPToolResultContent_单块保持类型 验证单块 data 直接返回而非字符串化
func TestExtractMCPToolResultContent_单块保持类型(t *testing.T) {
	raw := map[string]any{
		"isError": false,
		"content": []any{map[string]any{"type": "resource", "data": map[string]any{"k": "v"}}},
	}
	got := ExtractMCPToolResultContent(raw, false, "t")
	if _, ok := got.(map[string]any); !ok {
		t.Fatalf("单块 data 应原样返回 map，实际 %#v", got)
	}
}

// TestExtractMCPToolResultContent_无content 验证缺 content 时返回 nil（成功）
func TestExtractMCPToolResultContent_无content(t *testing.T) {
	if got := ExtractMCPToolResultContent(map[string]any{"isError": false}, false, "t"); got != nil {
		t.Fatalf("无 content 应返回 nil，实际 %#v", got)
	}
}

// TestPreserveMCPToolResultStatus_成功值不改 验证 isError=false 时内容不变
func TestPreserveMCPToolResultStatus_成功值不改(t *testing.T) {
	got := preserveMCPToolResultStatus(map[string]any{"isError": false}, "ok")
	if got != "ok" {
		t.Fatalf("成功值应原样返回，实际 %#v", got)
	}
}

// TestPreserveMCPToolResultStatus_失败转错误 验证 isError=true 时产出失败结构
func TestPreserveMCPToolResultStatus_失败转错误(t *testing.T) {
	raw := map[string]any{
		"isError": true,
		"content": []any{map[string]any{"text": "Permission denied"}},
	}
	got := preserveMCPToolResultStatus(raw, "placeholder")
	m, ok := got.(*MCPToolResult)
	if !ok {
		t.Fatalf("应返回 *MCPToolResult，实际 %T", got)
	}
	if m.Success {
		t.Fatal("isError=true 时 Success 应为 false")
	}
	if !strings.Contains(m.Err, "Permission denied") {
		t.Fatalf("Err 应保留原文，实际 %q", m.Err)
	}
}

// TestPreserveMCPToolResultStatus_空内容兜底 验证无文本时给兜底文案
func TestPreserveMCPToolResultStatus_空内容兜底(t *testing.T) {
	got := preserveMCPToolResultStatus(map[string]any{"isError": true}, nil)
	m, ok := got.(*MCPToolResult)
	if !ok || !strings.Contains(m.Err, "MCP tool reported an error.") {
		t.Fatalf("空内容应给兜底文案，实际 %#v", got)
	}
}

// TestExtractMCPToolResultContent_各返回点都过状态 验证每个分支都保留 isError
func TestExtractMCPToolResultContent_各返回点都过状态(t *testing.T) {
	cases := map[string]map[string]any{
		"无content": map[string]any{"isError": true},
		"单块data":   map[string]any{"isError": true, "content": []any{map[string]any{"data": "raw"}}},
		"单块other":  map[string]any{"isError": true, "content": []any{map[string]any{"type": "x", "foo": "bar"}}},
		"多块文本":     map[string]any{"isError": true, "content": []any{map[string]any{"text": "a"}, map[string]any{"text": "b"}}},
		"单块文本":     map[string]any{"isError": true, "content": []any{map[string]any{"text": "only"}}},
	}
	for name, raw := range cases {
		t.Run(name, func(t *testing.T) {
			got := ExtractMCPToolResultContent(raw, false, "t")
			m, ok := got.(*MCPToolResult)
			if !ok || m.Success {
				t.Fatalf("isError=true 应返回失败结构，实际 %#v", got)
			}
		})
	}
}

package mcp

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// TestNewListMcpResourcesTool_创建 测试创建 ListMcpResourcesTool。
func TestNewListMcpResourcesTool_创建(t *testing.T) {
	tl := NewListMcpResourcesTool("cn", "test-agent")
	require.NotNil(t, tl)
	assert.Equal(t, "list_mcp_resources", tl.Card().Name)
}

// TestNewListMcpResourcesTool_创建英文 测试创建 ListMcpResourcesTool（英文）。
func TestNewListMcpResourcesTool_创建英文(t *testing.T) {
	tl := NewListMcpResourcesTool("en", "test-agent")
	require.NotNil(t, tl)
	assert.Equal(t, "list_mcp_resources", tl.Card().Name)
}

// TestNewListMcpResourcesTool_缺少ServerID 测试空 server_id 时返回错误。
func TestNewListMcpResourcesTool_缺少ServerID(t *testing.T) {
	tl := NewListMcpResourcesTool("cn", "test-agent")
	require.NotNil(t, tl)

	result, err := tl.Invoke(context.Background(), map[string]any{"server_id": ""})
	require.NoError(t, err)

	assert.Equal(t, false, result["success"])
	assert.Equal(t, "server_id is required", result["error"])
}

// TestNewReadMcpResourceTool_创建 测试创建 ReadMcpResourceTool。
func TestNewReadMcpResourceTool_创建(t *testing.T) {
	tl := NewReadMcpResourceTool("cn", "test-agent")
	require.NotNil(t, tl)
	assert.Equal(t, "read_mcp_resource", tl.Card().Name)
}

// TestNewReadMcpResourceTool_创建英文 测试创建 ReadMcpResourceTool（英文）。
func TestNewReadMcpResourceTool_创建英文(t *testing.T) {
	tl := NewReadMcpResourceTool("en", "test-agent")
	require.NotNil(t, tl)
	assert.Equal(t, "read_mcp_resource", tl.Card().Name)
}

// TestNewReadMcpResourceTool_缺少ServerID 测试空 server_id 时返回错误。
func TestNewReadMcpResourceTool_缺少ServerID(t *testing.T) {
	tl := NewReadMcpResourceTool("cn", "test-agent")
	require.NotNil(t, tl)

	result, err := tl.Invoke(context.Background(), map[string]any{"server_id": "", "uri": "test://resource"})
	require.NoError(t, err)

	assert.Equal(t, false, result["success"])
	assert.Equal(t, "server_id is required", result["error"])
}

// TestNewReadMcpResourceTool_缺少URI 测试空 uri 时返回错误。
func TestNewReadMcpResourceTool_缺少URI(t *testing.T) {
	tl := NewReadMcpResourceTool("cn", "test-agent")
	require.NotNil(t, tl)

	result, err := tl.Invoke(context.Background(), map[string]any{"server_id": "test-server", "uri": ""})
	require.NoError(t, err)

	assert.Equal(t, false, result["success"])
	assert.Equal(t, "uri is required", result["error"])
}

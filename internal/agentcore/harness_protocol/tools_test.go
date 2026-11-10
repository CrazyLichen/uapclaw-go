package harness_protocol

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestNewToolDefinition_name非空且冻结schema 验证 name 与非空 schema。
// Python: tools.py:16-27
func TestNewToolDefinition_name非空且冻结schema(t *testing.T) {
	_, err := NewToolDefinition("", "d", nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "name must not be empty")

	schema := map[string]any{"type": "object"}
	d, err := NewToolDefinition("read_file", "读文件", schema)
	require.NoError(t, err)
	schema["type"] = "mutated"
	assert.Equal(t, "object", d.InputSchema["type"])
}

// TestNewToolInvocation_两字段非空 验证 call_id / name 必填。
// Python: tools.py:30-41
func TestNewToolInvocation_两字段非空(t *testing.T) {
	_, err := NewToolInvocation("", "n", nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "call_id and name must not be empty")

	_, err = NewToolInvocation("c1", "", nil)
	require.Error(t, err)

	inv, err := NewToolInvocation("c1", "read_file", map[string]any{"p": "/tmp"})
	require.NoError(t, err)
	assert.Equal(t, "c1", inv.CallID)
}

// TestNewToolExecutionResult_冻结content 验证内容冻结。
// Python: tools.py:44-52
func TestNewToolExecutionResult_冻结content(t *testing.T) {
	src := []any{1}
	r, err := NewToolExecutionResult(src, false)
	require.NoError(t, err)
	src[0] = 999
	assert.Equal(t, []any{1}, r.Content)
	assert.False(t, r.IsError)
}

// TestNewMcpServerConfig_stdio需要command 验证 stdio 分支。
// Python: tools.py:103-104 "stdio MCP server requires a non-empty command"
func TestNewMcpServerConfig_stdio需要command(t *testing.T) {
	_, err := NewMcpServerConfig("srv", McpTransportStdio, nil, nil, nil, nil, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "stdio MCP server requires a non-empty command")

	c, err := NewMcpServerConfig("srv", McpTransportStdio, []string{"npx", "x"}, nil, nil,
		map[string]string{"K": "V"}, nil)
	require.NoError(t, err)
	assert.Equal(t, []string{"npx", "x"}, c.Command)
	assert.Equal(t, "V", c.Env["K"])
}

// TestNewMcpServerConfig_http需要url 验证 http 分支。
// Python: tools.py:105-106 "HTTP MCP server requires url"
func TestNewMcpServerConfig_http需要url(t *testing.T) {
	_, err := NewMcpServerConfig("srv", McpTransportHTTP, nil, nil, nil, nil, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "HTTP MCP server requires url")

	url := "https://mcp.example"
	c, err := NewMcpServerConfig("srv", McpTransportHTTP, nil, &url, nil, nil,
		map[string]string{"Authorization": "Bearer x"})
	require.NoError(t, err)
	assert.Equal(t, "https://mcp.example", *c.URL)
}

// TestNewMcpServerConfig_inProcess需要instance 验证 in_process 分支。
// Python: tools.py:107-108 "in-process MCP server requires instance"
func TestNewMcpServerConfig_inProcess需要instance(t *testing.T) {
	_, err := NewMcpServerConfig("srv", McpTransportInProcess, nil, nil, nil, nil, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "in-process MCP server requires instance")

	inst := struct{ Name string }{Name: "x"}
	c, err := NewMcpServerConfig("srv", McpTransportInProcess, nil, nil, inst, nil, nil)
	require.NoError(t, err)
	assert.NotNil(t, c.Instance)
}

// TestNewMcpServerConfig_拒绝多target 验证恰好一个 transport target。
// Python: tools.py:113-115 "MCP server requires exactly one of command, url, or instance"
func TestNewMcpServerConfig_拒绝多target(t *testing.T) {
	url := "https://x"
	_, err := NewMcpServerConfig("srv", McpTransportStdio, []string{"a"}, &url, nil, nil, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "exactly one of command, url, or instance")
}

// TestNewMcpServerConfig_env仅stdio合法 验证 env 误用。
// Python: tools.py:109-110 "MCP env is only valid for stdio transport"
func TestNewMcpServerConfig_env仅stdio合法(t *testing.T) {
	url := "https://x"
	_, err := NewMcpServerConfig("srv", McpTransportHTTP, nil, &url, nil,
		map[string]string{"K": "V"}, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "MCP env is only valid for stdio transport")
}

// TestNewMcpServerConfig_headers仅http合法 验证 headers 误用。
// Python: tools.py:111-112 "MCP headers are only valid for HTTP transport"
func TestNewMcpServerConfig_headers仅http合法(t *testing.T) {
	_, err := NewMcpServerConfig("srv", McpTransportStdio, []string{"a"}, nil, nil, nil,
		map[string]string{"K": "V"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "MCP headers are only valid for HTTP transport")
}

// TestNewMcpServerConfig_name非空 验证 name 必填。
// Python: tools.py:94-95
func TestNewMcpServerConfig_name非空(t *testing.T) {
	_, err := NewMcpServerConfig("", McpTransportStdio, []string{"a"}, nil, nil, nil, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "MCP server name must not be empty")
}

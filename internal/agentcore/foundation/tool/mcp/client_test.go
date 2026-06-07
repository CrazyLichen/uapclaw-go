package mcp

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool/mcp/types"
)

func TestNewMcpClient_SSE(t *testing.T) {
	config := types.NewMcpServerConfig("test", "http://localhost:8080/sse", "sse")
	client, err := NewMcpClient(config)
	assert.NoError(t, err)
	assert.NotNil(t, client)
}

func TestNewMcpClient_Stdio(t *testing.T) {
	config := types.NewMcpServerConfig("test", "npx", "stdio")
	client, err := NewMcpClient(config)
	assert.NoError(t, err)
	assert.NotNil(t, client)
}

func TestNewMcpClient_StreamableHTTP(t *testing.T) {
	config := types.NewMcpServerConfig("test", "http://localhost:8080/mcp", "streamable-http")
	client, err := NewMcpClient(config)
	assert.NoError(t, err)
	assert.NotNil(t, client)
}

func TestNewMcpClient_StreamableHTTP下划线(t *testing.T) {
	config := types.NewMcpServerConfig("test", "http://localhost:8080/mcp", "streamable_http")
	client, err := NewMcpClient(config)
	assert.NoError(t, err)
	assert.NotNil(t, client)
}

func TestNewMcpClient_OpenAPI(t *testing.T) {
	config := types.NewMcpServerConfig("test", "openapi.json", "openapi")
	client, err := NewMcpClient(config)
	assert.NoError(t, err)
	assert.NotNil(t, client)
}

func TestNewMcpClient_Playwright(t *testing.T) {
	config := types.NewMcpServerConfig("test", "npx @playwright/mcp", "playwright")
	client, err := NewMcpClient(config)
	assert.NoError(t, err)
	assert.NotNil(t, client)
}

func TestNewMcpClient_未知类型(t *testing.T) {
	config := types.NewMcpServerConfig("test", "http://localhost:8080", "unknown")
	_, err := NewMcpClient(config)
	assert.Error(t, err)
}

func TestNewMcpClient_nil配置(t *testing.T) {
	_, err := NewMcpClient(nil)
	assert.Error(t, err)
}

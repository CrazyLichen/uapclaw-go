package mcp

import (
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool/mcp/client"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool/mcp/types"
	"github.com/uapclaw/uapclaw-go/internal/common/exception"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// NewMcpClient 根据配置创建对应类型的 MCP 客户端。
//
// 支持 clientType: sse / stdio / streamable-http / streamable_http / openapi / playwright
// 未知类型返回 StatusToolMcpClientTypeUnknown 错误。
//
// 对应 Python: 各客户端的构造逻辑
func NewMcpClient(config *types.McpServerConfig) (types.McpClient, error) {
	if config == nil {
		return nil, exception.BuildError(
			exception.StatusToolMcpClientTypeUnknown,
			exception.WithParam("client_type", "nil config"),
		)
	}

	switch config.ClientType {
	case "sse":
		return client.NewSseClient(config), nil
	case "stdio":
		return client.NewStdioClient(config), nil
	case "streamable-http", "streamable_http":
		return client.NewStreamableHttpClient(config), nil
	case "openapi":
		return client.NewOpenApiClient(config), nil
	case "playwright":
		return client.NewPlaywrightClient(config), nil
	default:
		logger.Error(logger.ComponentAgentCore).
			Str("client_type", config.ClientType).
			Msg("未知的 MCP 客户端类型")
		return nil, exception.BuildError(
			exception.StatusToolMcpClientTypeUnknown,
			exception.WithParam("client_type", config.ClientType),
		)
	}
}

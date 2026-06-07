//go:build integration

package client

import (
	"testing"
)

// TestOpenApiClient_集成测试 测试 OpenAPI 客户端集成调用。
// 运行方式: go test -tags=integration ./internal/agentcore/foundation/tool/mcp/client/...
func TestOpenApiClient_集成测试(t *testing.T) {
	t.Skip("需要真实 OpenAPI 服务，跳过单元测试")
}

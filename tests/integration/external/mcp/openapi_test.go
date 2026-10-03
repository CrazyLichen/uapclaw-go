//go:build integration

package openapi_test

import (
	"testing"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// TestOpenApiClient_集成测试 测试 OpenAPI 客户端集成调用。
// 运行方式: go test -tags=integration ./tests/integration/external/mcp/...
func TestOpenApiClient_集成测试(t *testing.T) {
	t.Skip("需要真实 OpenAPI 服务，跳过集成测试")
}

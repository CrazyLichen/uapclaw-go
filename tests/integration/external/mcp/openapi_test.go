//go:build integration

package openapi_test

import (
	"testing"

	"github.com/stretchr/testify/suite"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/runner"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session"
	isuite "github.com/uapclaw/uapclaw-go/tests/integration/suite"
)

// ──────────────────────────── 结构体 ────────────────────────────

// OpenApiSuite OpenAPI 客户端集成测试套件
type OpenApiSuite struct {
	isuite.BaseIntegrationSuite
}

// ──────────────────────────── 导出函数 ────────────────────────────

// TestOpenApiSuite 运行 OpenAPI 集成测试套件
// 运行方式: go test -tags=integration ./tests/integration/external/mcp/...
func TestOpenApiSuite(t *testing.T) {
	suite.Run(t, &OpenApiSuite{})
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// TestOpenApiClient_集成测试 测试 OpenAPI 客户端集成调用
func (s *OpenApiSuite) TestOpenApiClient_集成测试() {
	s.T().Skip("需要真实 OpenAPI 服务，跳过集成测试")
}

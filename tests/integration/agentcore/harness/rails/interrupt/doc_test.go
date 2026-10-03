//go:build integration

// Package interrupt_test 提供 AskUserRail 集成测试。
package interrupt_test

import (
	"testing"

	"github.com/stretchr/testify/suite"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/rails/interrupt"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/runner"
	isuite "github.com/uapclaw/uapclaw-go/tests/integration/suite"
)

// ──────────────────────────── 结构体 ────────────────────────────

// InterruptDocSuite interrupt 包文档占位套件
type InterruptDocSuite struct {
	isuite.BaseIntegrationSuite
}

// ──────────────────────────── 导出函数 ────────────────────────────

// TestInterruptDocSuite 运行 interrupt 包文档占位套件
func TestInterruptDocSuite(t *testing.T) {
	suite.Run(t, new(InterruptDocSuite))
}

// TestInterruptDoc_占位 interrupt 包文档占位测试
func (s *InterruptDocSuite) TestInterruptDoc_占位() {
	// 验证 interrupt 包和 runner 包可正常导入
	s.NotNil(interrupt.NewAskUserRail)
	s.NotNil(runner.GetResourceMgr)
}

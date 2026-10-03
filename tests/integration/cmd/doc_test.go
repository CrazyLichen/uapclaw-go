//go:build integration

// Package cmd_test 提供 CLI 命令的集成测试。
package cmd_test

import (
	"testing"

	"github.com/stretchr/testify/suite"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/runner"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session"
	isuite "github.com/uapclaw/uapclaw-go/tests/integration/suite"
)

// ──────────────────────────── 结构体 ────────────────────────────

// CmdDocSuite cmd 包文档占位套件
type CmdDocSuite struct {
	isuite.BaseIntegrationSuite
}

// ──────────────────────────── 导出函数 ────────────────────────────

// TestCmdDocSuite 运行 cmd 包文档占位套件
func TestCmdDocSuite(t *testing.T) {
	suite.Run(t, new(CmdDocSuite))
}

// TestCmdDoc_占位 cmd 包文档占位测试
func (s *CmdDocSuite) TestCmdDoc_占位() {
	// 验证 runner 和 session 包可正常导入
	s.NotNil(runner.GetResourceMgr)
	s.NotNil(session.NewSession)
}

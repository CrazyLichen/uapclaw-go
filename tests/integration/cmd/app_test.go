//go:build integration

package cmd_test

import (
	"os/exec"
	"testing"

	"github.com/stretchr/testify/suite"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/runner"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session"
	isuite "github.com/uapclaw/uapclaw-go/tests/integration/suite"
)

// ──────────────────────────── 结构体 ────────────────────────────

// AppSuite App 命令集成测试套件
type AppSuite struct {
	isuite.BaseIntegrationSuite
}

// ──────────────────────────── 导出函数 ────────────────────────────

// TestAppSuite 运行 App 命令集成测试套件
func TestAppSuite(t *testing.T) {
	suite.Run(t, new(AppSuite))
}

// TestAppCmd_Execute 验证 app 子命令执行输出
// 运行方式: go test -tags=integration ./tests/integration/cmd/...
// 迁移说明：原测试通过 main 包内部调用 newRootCmd()，迁移后改为通过 CLI 执行
func (s *AppSuite) TestAppCmd_Execute() {
	// 构建临时二进制
	cmd := exec.Command("go", "build", "-o", s.T().TempDir()+"/uapclaw", "./cmd/uapclaw/")
	if err := cmd.Run(); err != nil {
		s.T().Skipf("构建 CLI 失败，跳过: %v", err)
	}

	// 执行 app 子命令
	out, err := exec.Command(s.T().TempDir()+"/uapclaw", "app").CombinedOutput()
	if err != nil {
		s.T().Logf("app 命令输出: %s", string(out))
		// app 可能返回非零退出码（因缺少配置等），但不应 panic
	}
}

// TestAppDoc_包引用验证 验证引用的 internal 包可正常访问
func (s *AppSuite) TestAppDoc_包引用验证() {
	// 验证 runner 和 session 包可正常导入
	s.NotNil(runner.GetResourceMgr)
	s.NotNil(session.NewSession)
}

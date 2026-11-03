//go:build integration

package shell

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/suite"
	shelltools "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/tools/shell"
	// 空导入注册 local 模式的 fs/shell/code 操作到 GlobalRegistry
	"github.com/uapclaw/uapclaw-go/internal/agentcore/sys_operation"
	_ "github.com/uapclaw/uapclaw-go/internal/agentcore/sys_operation/local"
	isuite "github.com/uapclaw/uapclaw-go/tests/integration/suite"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ShellToolsSuite bash/powershell 工具集成测试套件。
//
// 通过 LocalSysOperation 驱动真实子进程，验证 timeout 默认值解析
// （回归：未传 timeout 曾被钳成 1 秒导致所有命令立即超时）。
type ShellToolsSuite struct {
	isuite.AgentSuite
	// op 本地系统操作实例
	op sys_operation.SysOperation
	// permCfg 权限配置（BYPASS，不拦截测试命令）
	permCfg shelltools.PermissionConfig
}

// ──────────────────────────── 导出函数 ────────────────────────────

func TestShellToolsSuite(t *testing.T) {
	suite.Run(t, new(ShellToolsSuite))
}

// SetupSuite 初始化测试套件。
func (s *ShellToolsSuite) SetupSuite() {
	s.AgentSuite.SetupSuite()
	op, err := sys_operation.NewSysOperation(nil)
	s.Require().NoError(err)
	s.op = op
	s.permCfg = shelltools.NewPermissionConfig(shelltools.PermissionModeBypass, nil, nil)
}

// TestBash_未传Timeout_回落300秒并正常执行 未传 timeout 的命令应正常完成。
// 回归：Timeout 缺失 key 解码为 0，曾被钳成 1 秒，echo 命令 1 秒内大概率
// 能完成、不可靠；此处用 sleep 2 制造与 1 秒钳位的可判差距——若仍被钳成
// 1 秒，命令会以「执行超时 1 秒」失败。
func (s *ShellToolsSuite) TestBash_未传Timeout_回落300秒并正常执行() {
	bashTool := shelltools.NewBashTool(s.op, "cn", "itest-agent", s.permCfg)

	res, err := bashTool.Invoke(context.Background(), map[string]any{
		// allowlist 只检查首词（echo 在默认白名单内），后接 sleep 制造与
		// 1 秒钳位的可判差距
		"command":     "echo itest_timeout_default_ok && sleep 2",
		"description": "集成测试：未传 timeout 应回落 300 秒",
	})
	s.Require().NoError(err)
	s.Require().Equal(true, res["success"],
		"未传 timeout 的命令应在默认 300 秒内完成，实际结果: %v", res)

	data, ok := res["data"].(map[string]any)
	s.Require().True(ok)
	content, _ := data["content"].(string)
	s.Contains(content, "itest_timeout_default_ok")
}

// TestBash_显式Timeout1秒_按1秒超时 显式传 timeout=1 时命令应按 1 秒超时。
func (s *ShellToolsSuite) TestBash_显式Timeout1秒_按1秒超时() {
	bashTool := shelltools.NewBashTool(s.op, "cn", "itest-agent", s.permCfg)

	res, err := bashTool.Invoke(context.Background(), map[string]any{
		"command":     "echo hi && sleep 5",
		"description": "集成测试：显式 timeout=1 应 1 秒超时",
		// 真实链路中 LLM 的 JSON 数字解码为 float64，与生产一致
		"timeout": float64(1),
	})
	s.Require().NoError(err)
	s.Equal(false, res["success"])

	errMsg, _ := res["error"].(string)
	s.Contains(errMsg, "执行超时 1 秒")
}

// TestPowerShell_未传Timeout_不报1秒超时 未传 timeout 时不得产生 1 秒超时错误。
// CI（linux）环境可能没有 pwsh，此时工具返回「启动失败」类错误；
// 无论哪种结果，都不应出现「执行超时 1 秒」。
func (s *ShellToolsSuite) TestPowerShell_未传Timeout_不报1秒超时() {
	psTool := shelltools.NewPowerShellTool(s.op, "cn", "itest-agent", s.permCfg)

	res, err := psTool.Invoke(context.Background(), map[string]any{
		"command":     "Write-Output itest_ps_ok",
		"description": "集成测试：powershell 未传 timeout",
	})
	s.Require().NoError(err)

	errMsg, _ := res["error"].(string)
	s.NotContains(errMsg, "执行超时 1 秒",
		"未传 timeout 不应触发 1 秒超时，实际结果: %v", res)

	// 有 pwsh 的环境下应成功执行（不强制，取决于环境是否安装 pwsh）
	if success, ok := res["success"].(bool); ok && success {
		data, _ := res["data"].(map[string]any)
		content, _ := data["content"].(string)
		s.True(strings.Contains(content, "itest_ps_ok"))
	}
}

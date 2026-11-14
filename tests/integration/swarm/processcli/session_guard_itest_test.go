//go:build integration

package processcli

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/suite"

	processcli "github.com/uapclaw/uapclaw-go/internal/swarm/channels/processcli"
	isuite "github.com/uapclaw/uapclaw-go/tests/integration/suite"
)

// ──────────────────────────── 结构体 ────────────────────────────

// SessionGuardSuite 同会话并发互斥（20.5 SessionLease）的链路级验证。
//
// 确定性来源：显式 session_id 时租约在 client.start 之前获取
// （Python machine.py:234-236，步骤 3），因此用例不需要第一个进程真的
// 在跑模型——测试进程直接持有同一把 OS 锁即可模拟"另一 CLI 实例"。
type SessionGuardSuite struct {
	isuite.BaseIntegrationSuite
	// binPath 构建好的 uapclaw-process 二进制路径
	binPath string
	// dataDir 本套件专用的数据目录
	dataDir string
}

// ──────────────────────────── 导出函数 ────────────────────────────

// TestSessionGuardSuite 运行本套件。
func TestSessionGuardSuite(t *testing.T) {
	suite.Run(t, new(SessionGuardSuite))
}

// SetupSuite 构建二进制并准备隔离的数据目录。
func (s *SessionGuardSuite) SetupSuite() {
	s.BaseIntegrationSuite.SetupSuite()
	s.binPath = filepath.Join(s.T().TempDir(), "uapclaw-process")
	build := exec.Command("go", "build", "-o", s.binPath, "./cmd/uapclaw-process/")
	build.Dir = repoRoot(s.T())
	if out, err := build.CombinedOutput(); err != nil {
		s.T().Fatalf("构建 uapclaw-process 失败: %v\n%s", err, out)
	}
	s.dataDir = s.T().TempDir()
}

// TestSessionBusy_并发拒绝 同一 session_id 已被他方持锁时，子进程以
// SESSION_BUSY 失败（退出 1）；锁释放后同一会话可重新进入。
// Python: session_guard.py:44-53 + machine.py:234-236。
func (s *SessionGuardSuite) TestSessionBusy_并发拒绝() {
	sessionID := "itest_busy_session"

	// 测试进程与子进程共用同一数据目录 ⇒ 同一锁文件。
	// 不设这个 env，测试进程的 lease 会落到 ~/.uapclaw，锁不住子进程。
	s.T().Setenv("UAPCLAW_DATA_DIR", s.dataDir)

	// 测试进程模拟另一 CLI 实例持有租约（同数据目录 ⇒ 同锁文件）
	lease := processcli.NewSessionLease(sessionID)
	s.Require().NoError(lease.Acquire())

	input := `{"schema_version":"0.1","type":"run","input":"hi","session_id":"` + sessionID + `"}`
	res := s.run(strings.NewReader(input), "--run-json", "-")
	s.Equal(1, res.exitCode, "会话被占用应退出 1")
	records := decodeJSONLines(s.T(), res.stdout)
	s.Require().NotEmpty(records)
	s.Equal("SESSION_BUSY", digString(records[len(records)-1], "error", "code"),
		"终态 result 应带 SESSION_BUSY 错误码")

	// 释放锁后同一会话可重新进入（锁文件被复用而非残留拒绝）
	lease.Release()
	res2 := s.run(strings.NewReader(input), "--run-json", "-")
	// 未配模型的确定性失败仍在，但不得再是 SESSION_BUSY
	records2 := decodeJSONLines(s.T(), res2.stdout)
	s.Require().NotEmpty(records2)
	s.NotEqual("SESSION_BUSY", digString(records2[len(records2)-1], "error", "code"))
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// run 以真实子进程执行命令（env 与 CLISuite.run 同款：隔离数据目录 + 清模型配置）。
func (s *SessionGuardSuite) run(stdin *strings.Reader, args ...string) cliResult {
	cmd := exec.Command(s.binPath, args...)
	cmd.Env = append(os.Environ(),
		"UAPCLAW_DATA_DIR="+s.dataDir,
		"UAPCLAW_CONFIG=",
	)
	if stdin != nil {
		cmd.Stdin = stdin
	} else {
		cmd.Stdin = strings.NewReader("")
	}
	var outBuf, errBuf strings.Builder
	cmd.Stdout = &outBuf
	cmd.Stderr = &errBuf

	err := cmd.Run()
	code := 0
	if err != nil {
		exitErr, ok := err.(*exec.ExitError)
		s.Require().True(ok, "非退出码错误: %v", err)
		code = exitErr.ExitCode()
	}
	return cliResult{stdout: outBuf.String(), stderr: errBuf.String(), exitCode: code}
}

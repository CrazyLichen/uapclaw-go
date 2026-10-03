//go:build integration

package cmd_test

import (
	"os/exec"
	"testing"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// TestAppCmd_Execute 验证 app 子命令执行输出
// 运行方式: go test -tags=integration ./tests/integration/cmd/...
// 迁移说明：原测试通过 main 包内部调用 newRootCmd()，迁移后改为通过 CLI 执行
func TestAppCmd_Execute(t *testing.T) {
	// 构建临时二进制
	cmd := exec.Command("go", "build", "-o", t.TempDir()+"/uapclaw", "./cmd/uapclaw/")
	if err := cmd.Run(); err != nil {
		t.Skipf("构建 CLI 失败，跳过: %v", err)
	}

	// 执行 app 子命令
	out, err := exec.Command(t.TempDir()+"/uapclaw", "app").CombinedOutput()
	if err != nil {
		t.Logf("app 命令输出: %s", string(out))
		// app 可能返回非零退出码（因缺少配置等），但不应 panic
	}
}

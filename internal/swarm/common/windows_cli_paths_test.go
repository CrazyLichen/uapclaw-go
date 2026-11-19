// Python 源：windows_cli_paths.py 解析半区用例（非 Windows 平台恒原样返回；
// Windows 分支逻辑经 preserved 前缀注入路径验证的等价行为——os.name 守卫对齐）

package common

import (
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestResolvePreservedCLIPath(t *testing.T) {
	if runtime.GOOS == "windows" {
		// Windows 下的真实行为由集成环境验证；CI 为 Linux。
		t.Skip("Windows 专属路径解析，非 Windows 平台跳过")
	}
	t.Run("非Windows平台恒原样返回", func(t *testing.T) {
		assert.Equal(t, "C:\\missing\\cli.cmd", ResolvePreservedCLIPath("C:\\missing\\cli.cmd"))
		assert.Equal(t, "", ResolvePreservedCLIPath(""))
	})
}

func TestWindowsNPMRoot(t *testing.T) {
	// Python: get_user_workspace_dir() / "runtime" / "npm"
	// → Go path.WorkspaceDir() / runtime / npm（尾部两段契约）
	root := windowsNPMRoot()
	assert.Contains(t, root, "runtime")
	assert.True(t, len(root) > len("/runtime/npm"))
}

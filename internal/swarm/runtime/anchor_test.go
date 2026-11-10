package runtime

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// repoRoot 从测试目录回溯到仓库根。
func repoRoot(t *testing.T) string {
	t.Helper()
	// 当前文件位于 internal/swarm/runtime/
	root, err := filepath.Abs(filepath.Join("..", "..", ".."))
	require.NoError(t, err)
	return root
}

// grepGoFiles 在 internal/tests/cmd 下搜索符号，返回命中行。
func grepGoFiles(t *testing.T, symbol string) []string {
	t.Helper()
	root := repoRoot(t)
	hits := make([]string, 0)
	for _, dir := range []string{"internal", "tests", "cmd"} {
		base := filepath.Join(root, dir)
		err := filepath.Walk(base, func(path string, info os.FileInfo, walkErr error) error {
			if walkErr != nil {
				return nil
			}
			if info.IsDir() || !strings.HasSuffix(path, ".go") {
				return nil
			}
			content, readErr := os.ReadFile(path)
			if readErr != nil {
				return nil
			}
			if strings.Contains(string(content), symbol) {
				hits = append(hits, path)
			}
			return nil
		})
		require.NoError(t, err)
	}
	return hits
}

// TestAnchor_旧清理适配器已删 规则 8：被替代的旧实现必须同改动删除，0 命中。
func TestAnchor_旧清理适配器已删(t *testing.T) {
	hits := grepGoFiles(t, "session"+"CleanerAdapter")
	require.Empty(t, hits, "旧适配器符号仍有命中: %v", hits)
}

// TestAnchor_门面不再直接调用账本 账本调用一律经 Runtime（或兜底访问器）。
func TestAnchor_门面不再直接调用账本(t *testing.T) {
	root := repoRoot(t)
	content, err := os.ReadFile(filepath.Join(root, "internal", "swarm", "server", "runtime", "interface.go"))
	require.NoError(t, err)
	text := string(content)

	// 门面内不再出现 coordinator.Close / hasActiveExecutions(uc.coordinator) 这类直连
	require.NotContains(t, text, "hasActiveExecutions(uc.coordinator)")
	require.NotContains(t, text, "uc.coordinator.Close(")
	require.NotContains(t, text, "uc.coordinator.CancelExecution(")
	require.NotContains(t, text, "uc.coordinator.RunUnary(")
	require.NotContains(t, text, "uc.coordinator.RegisterSession(")
}

// TestAnchor_清理走四步编排 provisioner 的 SessionCleaner 必须委托 Runtime。
func TestAnchor_清理走四步编排(t *testing.T) {
	root := repoRoot(t)
	content, err := os.ReadFile(filepath.Join(root, "internal", "swarm", "server", "session_stubs.go"))
	require.NoError(t, err)
	text := string(content)

	require.Contains(t, text, "CleanupSession(ctx, channelID, sessionID, false)",
		"删除事务必须以 resetPlanState=false 走 Runtime 四步清理")
}

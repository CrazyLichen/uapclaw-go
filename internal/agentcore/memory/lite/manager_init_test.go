package lite

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/workspace"
	sysop "github.com/uapclaw/uapclaw-go/internal/agentcore/sys_operation"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/sys_operation/cwd"
	_ "github.com/uapclaw/uapclaw-go/internal/agentcore/sys_operation/local"
)

// TestInitMemoryManagerAsync_透传ctx并建索引 回归 P0：
// Initialize 会立刻全量索引，文件读取走沙箱校验（沙箱根来自 ctx 的 CwdState）。
// 若初始化用 Background，索引永远为空（线上 memory_search 恒 results: []）。
func TestInitMemoryManagerAsync_透传ctx并建索引(t *testing.T) {
	root := t.TempDir()
	ws := workspace.NewWorkspace(root, "cn")
	nodePath := ws.GetNodePath("memory")
	if nodePath == nil {
		t.Fatal("Workspace 应包含 memory 节点")
	}
	memDir := *nodePath
	if err := os.MkdirAll(memDir, 0o755); err != nil {
		t.Fatalf("建目录失败: %v", err)
	}
	if err := os.WriteFile(filepath.Join(memDir, "MEMORY.md"),
		[]byte("- VDS15800 支持手动组网模式\n"), 0o644); err != nil {
		t.Fatalf("写记忆文件失败: %v", err)
	}

	op, err := sysop.NewSysOperation(sysop.NewSysOperationCard(
		sysop.WithSysOpMode(sysop.OperationModeLocal),
		sysop.WithSysOpWorkConfig(&sysop.LocalWorkConfig{RestrictToSandbox: true}),
	))
	if err != nil {
		t.Fatalf("创建 SysOperation 失败: %v", err)
	}

	runCtx := cwd.WithCwdState(context.Background(), cwd.InitCwd(root, cwd.WithWorkspace(root)))

	mgr, err := InitMemoryManagerAsync(runCtx, ws, "unit_tools_ctx", nil, op)
	if err != nil {
		t.Fatalf("初始化 manager 失败: %v", err)
	}
	if mgr == nil {
		t.Fatal("memory 未启用时才会返回 nil")
	}
	defer func() {
		_ = mgr.Close()
		ClearMemoryManagerCache()
	}()

	// 索引有内容 == 沙箱读取成功 == ctx 确实透传到了 Initialize
	status := mgr.Status()
	if status.Files == 0 {
		t.Fatal("files=0：初始化 ctx 未透传，沙箱拒绝读取记忆文件（P0 回归）")
	}
}

// TestIndexChunk_分词降级写原文 分词器不可用时索引侧写原文、查询侧退回「正则 + OR」，
// 记忆检索整体仍可用。
//
// 断言刻意避开 FTS 分词细节：unicode61 对不同编译产物的 CJK 切分行为不完全一致
// （本地按整段、CI 上按字），因此这里只断言「降级后仍能命中英文/数字」——
// 中文在降级下本就只能靠向量召回，不属于本用例的验证目标。
func TestIndexChunk_分词降级写原文(t *testing.T) {
	t.Setenv("UAPCLAW_DISABLE_SEGMENTER", "1")

	m, cleanup := newReindexTestManager(t)
	defer cleanup()

	dir := filepath.Dir(m.dbPath)
	if err := os.WriteFile(filepath.Join(dir, "MEMORY.md"),
		[]byte("- VDS15800 支持手动组网模式\n"), 0o644); err != nil {
		t.Fatalf("写记忆文件失败: %v", err)
	}
	if err := m.Sync(context.Background(), "unit", true); err != nil {
		t.Fatalf("同步失败: %v", err)
	}

	var chunks int
	if err := m.db.QueryRow("SELECT COUNT(*) FROM chunks").Scan(&chunks); err != nil {
		t.Fatalf("统计 chunks 失败: %v", err)
	}
	if chunks == 0 {
		t.Fatal("降级时也必须建立索引")
	}

	// 走 Search（而非直接 MATCH）：验证「降级写原文 + 正则切分 + OR」整条链路仍可用
	results, err := m.Search(context.Background(), "VDS15800", SearchOpts{MinScore: 0.01})
	if err != nil {
		t.Fatalf("检索失败: %v", err)
	}
	if len(results) == 0 {
		t.Fatal("降级后英文/数字 token 仍应命中")
	}
}

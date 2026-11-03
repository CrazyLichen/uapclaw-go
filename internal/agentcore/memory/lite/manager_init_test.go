package lite

import (
	"context"
	"database/sql"
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

// TestIndexChunk_分词降级写原文 分词器不可用时 FTS 文本退回原文：
// unicode61 会把「支持手动组网模式」当成一个整词，因此 MATCH "组网" 命中 0 条，
// 而 MATCH "vds15800"（英文整词）仍能命中。
//
// 注：FTS 表是 contentless（content=”），无法 SELECT 出列值，只能靠 MATCH 行为判定。
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

	db, err := sql.Open("sqlite3", m.dbPath)
	if err != nil {
		t.Fatalf("打开 db 失败: %v", err)
	}
	defer db.Close()

	countMatch := func(expr string) int {
		var n int
		if err := db.QueryRow("SELECT COUNT(*) FROM chunks_fts WHERE chunks_fts MATCH ?", expr).Scan(&n); err != nil {
			t.Fatalf("MATCH %s 失败: %v", expr, err)
		}
		return n
	}

	if n := countMatch(`"vds15800"`); n == 0 {
		t.Error("英文整词应命中（降级写原文）")
	}
	if n := countMatch(`"组网"`); n != 0 {
		t.Errorf("降级时不应有中文整词 token，MATCH 组网 命中 %d 条", n)
	}
	if n := countMatch(`"支持手动组网模式"`); n == 0 {
		t.Error("原文整串应命中（证明写的是原文而不是分词串）")
	}
}

package lite

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/workspace"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

const (
	// deadlockGuardTimeout 同步操作超时上限（正常耗时 < 100ms，超时即判定自锁）
	deadlockGuardTimeout = 15 * time.Second
)

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// TestManagerSync_索引文件不自锁 回归：indexFile 在事务内不得再向连接池取连接。
//
// 背景：SQLite 连接池上限为 1（对齐 Python 单连接语义），indexFile 曾用
// BeginTx 占住唯一连接后再通过 m.db.Exec / m.db.BeginTx 取第二条连接，
// 导致 Sync 永久阻塞。修复后事务贯穿（indexChunkWithTx / getEmbeddingWithTx）。
func TestManagerSync_索引文件不自锁(t *testing.T) {
	mgr, cmDir, cleanup := newDeadlockGuardManager(t)
	defer cleanup()

	memFile := filepath.Join(cmDir, "note.md")
	body := "---\nname: Note\ndescription: guard\ntype: user\n---\n\ncontent for indexing\n"
	if err := os.WriteFile(memFile, []byte(body), 0o644); err != nil {
		t.Fatalf("写入记忆文件失败: %v", err)
	}

	runWithTimeout(t, "Sync(force)", func() error {
		return mgr.Sync(context.Background(), "unit-guard", true)
	})

	status := mgr.Status()
	if status == nil || status.Files < 1 {
		t.Fatalf("索引后 files 应 ≥ 1，实际 %+v", status)
	}
	if status.Chunks < 1 {
		t.Fatalf("索引后 chunks 应 ≥ 1，实际 %+v", status)
	}
}

// TestManagerSync_删除文件不自锁 回归：清理失效索引时不得持有 rows 再开事务。
//
// 背景：syncMemoryFiles 曾在 rows 未关闭（占用唯一连接）时调用
// removeFileFromIndex（内部 BeginTx），导致 Sync 永久阻塞。修复后先
// collectStalePaths 读完并关闭 rows，再逐个删除。
func TestManagerSync_删除文件不自锁(t *testing.T) {
	mgr, cmDir, cleanup := newDeadlockGuardManager(t)
	defer cleanup()

	memFile := filepath.Join(cmDir, "note.md")
	body := "---\nname: Note\ndescription: guard\ntype: user\n---\n\ncontent for indexing\n"
	if err := os.WriteFile(memFile, []byte(body), 0o644); err != nil {
		t.Fatalf("写入记忆文件失败: %v", err)
	}
	runWithTimeout(t, "Sync(force) 首次索引", func() error {
		return mgr.Sync(context.Background(), "unit-guard-add", true)
	})

	if err := os.Remove(memFile); err != nil {
		t.Fatalf("删除记忆文件失败: %v", err)
	}
	runWithTimeout(t, "Sync(force) 清理失效索引", func() error {
		return mgr.Sync(context.Background(), "unit-guard-remove", true)
	})

	status := mgr.Status()
	if status != nil && status.Files != 0 {
		t.Fatalf("删除后 files 应清零，实际 %+v", status)
	}
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// newDeadlockGuardManager 构造临时目录下的真实 manager（关闭 watcher 与定时同步，
// 保证 Sync 只由测试显式驱动）。
func newDeadlockGuardManager(t *testing.T) (MemoryIndexManager, string, func()) {
	t.Helper()
	root := t.TempDir()
	ws := workspace.NewWorkspace(root, "cn")
	nodePath := ws.GetNodePath("coding_memory")
	if nodePath == nil {
		t.Fatal("Workspace 应包含 coding_memory 节点")
	}
	cmDir := *nodePath
	if err := os.MkdirAll(cmDir, 0o755); err != nil {
		t.Fatalf("创建 coding_memory 目录失败: %v", err)
	}

	settings := CreateMemorySettings(cmDir, nil)
	settings.Sync.Watch = false
	settings.Sync.IntervalMinutes = 0

	mgr, err := GetMemoryIndexManager(MemoryManagerParams{
		AgentID:   "unit_deadlock_guard",
		Workspace: ws,
		Settings:  settings,
		NodeName:  "coding_memory",
	})
	if err != nil {
		t.Fatalf("初始化 manager 失败: %v", err)
	}
	return mgr, cmDir, func() {
		_ = mgr.Close()
		ClearMemoryManagerCache()
	}
}

// runWithTimeout 在 goroutine 中执行同步操作，超时即判定自锁并失败。
func runWithTimeout(t *testing.T, name string, fn func() error) {
	t.Helper()
	done := make(chan error, 1)
	go func() { done <- fn() }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("%s 返回错误: %v", name, err)
		}
	case <-time.After(deadlockGuardTimeout):
		t.Fatalf("%s 超时 %v，疑似在事务内二次获取连接导致自锁", name, deadlockGuardTimeout)
	}
}

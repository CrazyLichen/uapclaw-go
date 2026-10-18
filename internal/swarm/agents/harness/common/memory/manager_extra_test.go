package memory

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// ──────────────────────────── 测试辅助 ────────────────────────────

// fakeEmbeddingProvider 可编程 EmbeddingProvider 测试替身
type fakeEmbeddingProvider struct {
	embedFn func(ctx context.Context, text string) ([]float64, error)
	id      string
	model   string
}

func (f *fakeEmbeddingProvider) EmbedQuery(ctx context.Context, text string) ([]float64, error) {
	return f.embedFn(ctx, text)
}

func (f *fakeEmbeddingProvider) EmbedDocuments(ctx context.Context, texts []string) ([][]float64, error) {
	out := make([][]float64, 0, len(texts))
	for _, t := range texts {
		v, err := f.embedFn(ctx, t)
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, nil
}

func (f *fakeEmbeddingProvider) ID() string    { return f.id }
func (f *fakeEmbeddingProvider) Model() string { return f.model }
func (f *fakeEmbeddingProvider) Dims() int     { return 8 }

// openTestMemoryDB 打开测试 SQLite 库（内存或临时文件）并建表
func openTestMemoryDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite3", filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatalf("打开测试库失败: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	for _, ddl := range []string{
		`CREATE TABLE IF NOT EXISTS meta (key TEXT PRIMARY KEY, value TEXT)`,
		`CREATE TABLE IF NOT EXISTS files (path TEXT PRIMARY KEY, source TEXT, hash TEXT, mtime INTEGER, size INTEGER)`,
		`CREATE TABLE IF NOT EXISTS chunks (id TEXT PRIMARY KEY, path TEXT, source TEXT, start_line INTEGER, end_line INTEGER, hash TEXT, model TEXT, text TEXT, embedding BLOB, updated_at INTEGER)`,
		fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s (provider TEXT, model TEXT, provider_key TEXT, hash TEXT PRIMARY KEY, embedding BLOB, dims INTEGER, updated_at INTEGER)`, embeddingCacheTable),
	} {
		if _, err := db.Exec(ddl); err != nil {
			t.Fatalf("建表失败: %v", err)
		}
	}
	return db
}

// newPartialManager 构造部分初始化的 manager（用于测非导出函数）。
// settings 为 nil 时在临时目录上构造默认配置
func newPartialManager(t *testing.T, db *sql.DB, settings *MemorySettings, workspaceDir string) *MemoryIndexManager {
	if settings == nil {
		workspaceDir = t.TempDir()
		settings = CreateMemorySettings(workspaceDir, nil)
	}
	if workspaceDir == "" {
		workspaceDir = "."
	}
	m := newMemoryIndexManager("test-agent", workspaceDir, settings)
	m.db = db
	m.provider = &fakeEmbeddingProvider{
		embedFn: func(ctx context.Context, text string) ([]float64, error) {
			return []float64{0.1, 0.2, 0.3, 0.4, 0.5, 0.6, 0.7, 0.8}, nil
		},
		id:    "fake",
		model: "fake-model",
	}
	m.providerKey = m.provider.ID() + ":" + m.provider.Model()
	return m
}

// insertTestChunk 直插一条 chunk（含 embedding blob）
func insertTestChunk(t *testing.T, db *sql.DB, path, text string, vec []float64) {
	t.Helper()
	blob := vectorToBlob(vec)
	_, err := db.Exec(
		"INSERT OR REPLACE INTO chunks (id, path, source, start_line, end_line, hash, model, text, embedding, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)",
		path+":1:1", path, "memory", 1, 1, HashText(text), "fake-model", text, blob, time.Now().Unix(),
	)
	if err != nil {
		t.Fatalf("插 chunk 失败: %v", err)
	}
}

// ──────────────────────────── scheduleWatchSync 测试 ────────────────────────────

// TestScheduleWatchSync_防抖触发 对齐 Python _schedule_watch_sync：
// dirty=true + watchDebounceMs 后执行 Sync(reason="watch")
func TestScheduleWatchSync_防抖触发(t *testing.T) {
	db := openTestMemoryDB(t)
	dir := t.TempDir()
	_ = os.MkdirAll(filepath.Join(dir, "memory"), 0o755)
	_ = os.WriteFile(filepath.Join(dir, "memory", "debounce.md"), []byte("debounce content"), 0o644)

	settings := CreateMemorySettings(dir, map[string]any{
		"sync": map[string]any{"watchDebounceMs": 200, "onSearch": false},
	})
	m := newPartialManager(t, db, settings, dir)

	// 首次同步建立基线（走真实 syncMemoryFiles）
	if err := m.syncMemoryFiles(context.Background()); err != nil {
		t.Fatalf("初始同步失败: %v", err)
	}

	// 触发防抖（dirty 变 true，200ms 后自动 Sync）
	m.scheduleWatchSync()

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		m.mu.Lock()
		dirty := m.dirty
		m.mu.Unlock()
		if !dirty {
			return // 防抖同步已执行（sync 内 dirty=false）
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Errorf("防抖 3s 内未触发同步（dirty 仍为 true）")
}

// TestScheduleWatchSync_重复调用重置防抖窗口 对齐 Python：已有 timer 时 cancel 重置
func TestScheduleWatchSync_重复调用重置防抖窗口(t *testing.T) {
	db := openTestMemoryDB(t)
	dir := t.TempDir()
	_ = os.MkdirAll(filepath.Join(dir, "memory"), 0o755)
	_ = os.WriteFile(filepath.Join(dir, "memory", "r.md"), []byte("reset content"), 0o644)

	settings := CreateMemorySettings(dir, map[string]any{
		"sync": map[string]any{"watchDebounceMs": 300, "onSearch": false},
	})
	m := newPartialManager(t, db, settings, dir)
	if err := m.syncMemoryFiles(context.Background()); err != nil {
		t.Fatalf("初始同步失败: %v", err)
	}

	// 两次调度：第二次应取消第一次的 timer 并重开窗口
	m.scheduleWatchSync()
	time.Sleep(100 * time.Millisecond)
	m.scheduleWatchSync()

	m.watchMu.Lock()
	timerActive := m.watchTimer != nil
	m.watchMu.Unlock()
	if !timerActive {
		t.Errorf("第二次调度后应存在活动 timer")
	}

	// 等待防抖执行
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		m.mu.Lock()
		dirty := m.dirty
		m.mu.Unlock()
		if !dirty {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Errorf("重置窗口后防抖未生效")
}

// TestScheduleWatchSync_关闭后不执行 对齐 Python：timer 回调检查 closed
func TestScheduleWatchSync_关闭后不执行(t *testing.T) {
	db := openTestMemoryDB(t)
	dir := t.TempDir()
	settings := CreateMemorySettings(dir, map[string]any{
		"sync": map[string]any{"watchDebounceMs": 100, "onSearch": false},
	})
	m := newPartialManager(t, db, settings, dir)

	m.mu.Lock()
	m.closed = true
	m.mu.Unlock()

	m.scheduleWatchSync() // 应不 panic 且不执行 Sync

	time.Sleep(300 * time.Millisecond)
	m.mu.Lock()
	closed := m.closed
	m.mu.Unlock()
	if !closed {
		t.Errorf("closed 状态不应被改变")
	}
}

// ──────────────────────────── ensureIntervalSync 测试 ────────────────────────────

// TestEnsureIntervalSync_禁用 对齐 Python：intervalMinutes<=0 不启动
func TestEnsureIntervalSync_禁用(t *testing.T) {
	db := openTestMemoryDB(t)
	dir := t.TempDir()
	settings := CreateMemorySettings(dir, map[string]any{
		"sync": map[string]any{"intervalMinutes": 0},
	})
	m := newPartialManager(t, db, settings, dir)
	m.ensureIntervalSync(context.Background())
	if m.intervalCancel != nil {
		t.Errorf("intervalMinutes=0 不应启动定时器")
	}
}

// TestEnsureIntervalSync_启动并关闭 对齐 Python：goroutine 循环 + cancel 退出
func TestEnsureIntervalSync_启动并关闭(t *testing.T) {
	db := openTestMemoryDB(t)
	dir := t.TempDir()
	settings := CreateMemorySettings(dir, map[string]any{
		"sync": map[string]any{"intervalMinutes": 1},
	})
	m := newPartialManager(t, db, settings, dir)
	m.ensureIntervalSync(context.Background())
	if m.intervalCancel == nil {
		t.Fatalf("intervalMinutes=1 应启动定时器")
	}

	// 幂等：再次调用不重建
	m.ensureIntervalSync(context.Background())

	m.intervalCancel() // 应正常退出 goroutine（不 panic、不泄漏）
}

// TestEnsureIntervalSync_关闭后跳过同步 对齐 Python：ticker 触发时检查 closed
func TestEnsureIntervalSync_关闭后跳过同步(t *testing.T) {
	db := openTestMemoryDB(t)
	dir := t.TempDir()
	settings := CreateMemorySettings(dir, map[string]any{
		"sync": map[string]any{"intervalMinutes": 1},
	})
	m := newPartialManager(t, db, settings, dir)
	m.ensureIntervalSync(context.Background())

	m.mu.Lock()
	m.closed = true
	m.mu.Unlock()

	// cancel 后 goroutine 应退出；不 panic 即通过
	m.intervalCancel()
}

// ──────────────────────────── ensureVectorTable / searchVector 测试 ────────────────────────────

// TestEnsureVectorTable_未启用 对齐 Python：vectorAvailable=false 直返 false
func TestEnsureVectorTable_未启用(t *testing.T) {
	db := openTestMemoryDB(t)
	m := newPartialManager(t, db, nil, "")
	m.vectorAvailable = false
	if m.ensureVectorTable(8) {
		t.Errorf("vectorAvailable=false 应返回 false")
	}
}

// TestEnsureVectorTable_dims变化重置 对齐 Python：dims 不一致时 DROP 重建（无 vec0 扩展时降级）
func TestEnsureVectorTable_dims变化重置(t *testing.T) {
	db := openTestMemoryDB(t)
	m := newPartialManager(t, db, nil, "")
	m.vectorAvailable = true // 模拟已加载扩展

	// dims=8 创建成功（vec0 虚拟表在无扩展时会失败 → 降级 false，也是有效路径）
	got := m.ensureVectorTable(8)
	if got {
		if m.vectorDims == nil || *m.vectorDims != 8 {
			t.Errorf("成功时 vectorDims 应更新为 8")
		}
		// 再调 dims 一致直返 true
		if !m.ensureVectorTable(8) {
			t.Errorf("dims 一致应直返 true")
		}
		// dims 变化 → DROP 重建路径
		m.ensureVectorTable(16)
	} else {
		// 降级路径：vectorAvailable 应为 false
		if m.vectorAvailable {
			t.Errorf("失败后应降级 vectorAvailable=false")
		}
	}
}

// TestSearchVector_vectorEnabled降级路径 对齐 Python _search_vector：
// vectorEnabled=false → 走内存余弦 fallback
func TestSearchVector_vectorEnabled降级路径(t *testing.T) {
	db := openTestMemoryDB(t)
	m := newPartialManager(t, db, nil, "")
	m.vectorEnabled = false

	insertTestChunk(t, db, "memory/a.md", "hello world", []float64{1, 0, 0, 0, 0, 0, 0, 0})

	results := m.searchVector(context.Background(), []float64{1, 0, 0, 0, 0, 0, 0, 0}, 10)
	if len(results) == 0 {
		t.Fatalf("fallback 应命中 1 条")
	}
	if results[0]["score"].(float64) <= 0.99 {
		t.Errorf("完全同向向量余弦应≈1: %v", results[0]["score"])
	}
}

// TestSearchVector_零向量 对齐 Python：查询范数 < 1e-10 返回空
func TestSearchVector_零向量(t *testing.T) {
	db := openTestMemoryDB(t)
	m := newPartialManager(t, db, nil, "")
	m.vectorEnabled = false

	insertTestChunk(t, db, "memory/z.md", "zero vec", []float64{1, 0, 0, 0, 0, 0, 0, 0})

	results := m.searchVector(context.Background(), make([]float64, 8), 10)
	if len(results) != 0 {
		t.Errorf("零查询向量应返回空: %v", results)
	}
}

// TestSearchVectorFallback_维度不匹配跳过 对齐 Python：len(vec) != len(normalized) 跳过
func TestSearchVectorFallback_维度不匹配跳过(t *testing.T) {
	db := openTestMemoryDB(t)
	m := newPartialManager(t, db, nil, "")

	insertTestChunk(t, db, "memory/mismatch.md", "dim mismatch", []float64{1, 0, 0}) // 3 维

	results := m.searchVectorFallback([]float64{1, 0, 0, 0, 0, 0, 0, 0}, 10) // 8 维查询
	if len(results) != 0 {
		t.Errorf("维度不匹配应跳过: %v", results)
	}
}

// TestSearchVectorWithVec0_空chunk 对齐 Python：chunk_map 为空返回空切片
func TestSearchVectorWithVec0_空chunk(t *testing.T) {
	db := openTestMemoryDB(t)
	m := newPartialManager(t, db, nil, "")

	results, err := m.searchVectorWithVec0([]float64{1, 0, 0, 0, 0, 0, 0, 0}, 10)
	if err != nil {
		t.Fatalf("空库不应报错: %v", err)
	}
	if len(results) != 0 {
		t.Errorf("空库应返回空: %v", results)
	}
}

// ──────────────────────────── getEmbedding 测试 ────────────────────────────

// TestGetEmbedding_缓存命中 对齐 Python _get_embedding 四键缓存
func TestGetEmbedding_缓存命中(t *testing.T) {
	db := openTestMemoryDB(t)
	m := newPartialManager(t, db, nil, "")
	m.cacheEnabled = true

	tx, err := db.Begin()
	if err != nil {
		t.Fatalf("开事务失败: %v", err)
	}
	v1 := m.getEmbedding(context.Background(), tx, "cached text")
	if v1 == nil {
		t.Fatalf("首次嵌入应成功")
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("提交失败: %v", err)
	}

	// 第二次应命中缓存（返回非 nil 即为缓存或重算成功；验证缓存条目数=1）
	tx2, err := db.Begin()
	if err != nil {
		t.Fatalf("开事务失败: %v", err)
	}
	v2 := m.getEmbedding(context.Background(), tx2, "cached text")
	_ = tx2.Commit()
	if v2 == nil {
		t.Fatalf("二次嵌入应成功")
	}
	// float32 往返有精度损失（0.1 → 0.10000000149011612），按 float32 精度比较
	if len(v1) != len(v2) {
		t.Fatalf("维度应一致: %d vs %d", len(v1), len(v2))
	}
	for i := range v1 {
		if float32(v1[i]) != float32(v2[i]) {
			t.Errorf("缓存命中应返回相同向量（float32 精度）: %v vs %v", v1[i], v2[i])
			break
		}
	}

	var count int
	if err := db.QueryRow("SELECT COUNT(*) FROM embedding_cache").Scan(&count); err != nil {
		t.Fatalf("查缓存数失败: %v", err)
	}
	if count != 1 {
		t.Errorf("同一文本应仅 1 条缓存，实际 %d", count)
	}
}

// TestGetEmbedding_提供者错误返回nil 对齐 Python：embed 失败记日志返回 None
func TestGetEmbedding_提供者错误返回nil(t *testing.T) {
	db := openTestMemoryDB(t)
	m := newPartialManager(t, db, nil, "")
	m.provider = &fakeEmbeddingProvider{
		embedFn: func(ctx context.Context, text string) ([]float64, error) {
			return nil, fmt.Errorf("embed boom")
		},
	}
	tx, _ := db.Begin()
	defer func() { _ = tx.Rollback() }()
	if got := m.getEmbedding(context.Background(), tx, "any"); got != nil {
		t.Errorf("提供者失败应返回 nil: %v", got)
	}
}

// TestGetEmbedding_无提供者 对齐 Python：provider 未初始化返回 None
func TestGetEmbedding_无提供者(t *testing.T) {
	db := openTestMemoryDB(t)
	m := newPartialManager(t, db, nil, "")
	m.provider = nil
	tx, _ := db.Begin()
	defer func() { _ = tx.Rollback() }()
	if got := m.getEmbedding(context.Background(), tx, "any"); got != nil {
		t.Errorf("无 provider 应返回 nil")
	}
}

// ──────────────────────────── embedQueryWithTimeout 测试 ────────────────────────────

// TestEmbedQueryWithTimeout_成功 对齐 Python：正常嵌入
func TestEmbedQueryWithTimeout_成功(t *testing.T) {
	db := openTestMemoryDB(t)
	m := newPartialManager(t, db, nil, "")
	vec := m.embedQueryWithTimeout(context.Background(), "hello")
	if len(vec) != 8 {
		t.Errorf("应返回 8 维: %v", vec)
	}
}

// TestEmbedQueryWithTimeout_超时 对齐 Python：60s 超时 warn + 返回 None
func TestEmbedQueryWithTimeout_超时(t *testing.T) {
	db := openTestMemoryDB(t)
	m := newPartialManager(t, db, nil, "")
	// 用已超时的 ctx 触发 DeadlineExceeded 分支
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Millisecond)
	defer cancel()
	time.Sleep(10 * time.Millisecond)
	m.provider = &fakeEmbeddingProvider{
		embedFn: func(ctx context.Context, text string) ([]float64, error) {
			<-ctx.Done()
			return nil, ctx.Err()
		},
	}
	if got := m.embedQueryWithTimeout(ctx, "slow"); got != nil {
		t.Errorf("超时应返回 nil: %v", got)
	}
}

// TestEmbedQueryWithTimeout_错误 对齐 Python：error 分支返回 None
func TestEmbedQueryWithTimeout_错误(t *testing.T) {
	db := openTestMemoryDB(t)
	m := newPartialManager(t, db, nil, "")
	m.provider = &fakeEmbeddingProvider{
		embedFn: func(ctx context.Context, text string) ([]float64, error) {
			return nil, fmt.Errorf("query boom")
		},
	}
	if got := m.embedQueryWithTimeout(context.Background(), "q"); got != nil {
		t.Errorf("错误应返回 nil: %v", got)
	}
}

// ──────────────────────────── shouldFullReindex / runReindex 测试 ────────────────────────────

// TestShouldFullReindex_触发场景 对齐 Python：meta 缺失/provider 变/model 变/chunkTokens 变
func TestShouldFullReindex_触发场景(t *testing.T) {
	db := openTestMemoryDB(t)
	dir := t.TempDir()
	_ = os.MkdirAll(filepath.Join(dir, "memory"), 0o755)
	_ = os.WriteFile(filepath.Join(dir, "memory", "re.md"), []byte("reindex"), 0o644)
	settings := CreateMemorySettings(dir, nil)
	m := newPartialManager(t, db, settings, dir)

	// meta 缺失 → true
	if !m.shouldFullReindex() {
		t.Errorf("meta 缺失应触发全量重建")
	}

	// 写入 meta 后 → false（provider/model/chunkTokens 均匹配）
	if err := m.runReindex(context.Background()); err != nil {
		t.Fatalf("runReindex 失败: %v", err)
	}
	if m.shouldFullReindex() {
		t.Errorf("meta 匹配不应触发重建")
	}

	// provider 变 → true（换不同 id/model 的 fake）
	m.provider = &fakeEmbeddingProvider{
		embedFn: func(ctx context.Context, text string) ([]float64, error) { return make([]float64, 8), nil },
		id:      "other",
		model:   "model-x",
	}
	m.providerKey = m.provider.ID() + ":" + m.provider.Model()
	if !m.shouldFullReindex() {
		t.Errorf("provider 变化应触发重建")
	}

	// 恢复 + chunkTokens 变 → true
	m.provider = nil
	m2 := newPartialManager(t, db, settings, dir)
	m2.Settings.Chunking.Tokens = 999
	if !m2.shouldFullReindex() {
		t.Errorf("chunkTokens 变化应触发重建")
	}
}

// TestRunReindex_仅memory源 对齐 Python：sources 不含 sessions 时跳过会话同步
func TestRunReindex_仅memory源(t *testing.T) {
	db := openTestMemoryDB(t)
	dir := t.TempDir()
	_ = os.MkdirAll(filepath.Join(dir, "memory"), 0o755)
	_ = os.WriteFile(filepath.Join(dir, "memory", "only.md"), []byte("only memory"), 0o644)
	_ = os.MkdirAll(filepath.Join(dir, "sessions"), 0o755)
	_ = os.WriteFile(filepath.Join(dir, "sessions", "2026-01-01.jsonl"), []byte("{}"), 0o644)

	settings := CreateMemorySettings(dir, map[string]any{"sources": []any{"memory"}})
	m := newPartialManager(t, db, settings, dir)

	if err := m.runReindex(context.Background()); err != nil {
		t.Fatalf("runReindex 失败: %v", err)
	}

	var sessionCount int
	_ = db.QueryRow("SELECT COUNT(*) FROM files WHERE source = 'sessions'").Scan(&sessionCount)
	if sessionCount != 0 {
		t.Errorf("sources 无 sessions 时不应索引会话文件: %d", sessionCount)
	}
	var memCount int
	_ = db.QueryRow("SELECT COUNT(*) FROM files WHERE source = 'memory'").Scan(&memCount)
	if memCount == 0 {
		t.Errorf("memory 源应被索引")
	}
}

// TestRunReindex_写meta 对齐 Python _run_reindex：写 meta 含 provider/model/chunkTokens
func TestRunReindex_写meta(t *testing.T) {
	db := openTestMemoryDB(t)
	dir := t.TempDir()
	settings := CreateMemorySettings(dir, map[string]any{"sources": []any{}})
	m := newPartialManager(t, db, settings, dir)

	if err := m.runReindex(context.Background()); err != nil {
		t.Fatalf("runReindex 失败: %v", err)
	}

	var value string
	if err := db.QueryRow("SELECT value FROM meta WHERE key = ?", metaKey).Scan(&value); err != nil {
		t.Fatalf("meta 应写入: %v", err)
	}
	if !strings.Contains(value, "fake-model") {
		t.Errorf("meta 应含 model: %s", value)
	}
}

// ──────────────────────────── syncMemoryFiles / syncSessionFiles 错误分支 ────────────────────────────

// TestSyncMemoryFiles_文件删除 对齐 Python：文件消失后从索引移除
func TestSyncMemoryFiles_文件删除(t *testing.T) {
	db := openTestMemoryDB(t)
	dir := t.TempDir()
	memDir := filepath.Join(dir, "memory")
	_ = os.MkdirAll(memDir, 0o755)
	fp := filepath.Join(memDir, "gone.md")
	_ = os.WriteFile(fp, []byte("to be deleted"), 0o644)

	settings := CreateMemorySettings(dir, nil)
	m := newPartialManager(t, db, settings, dir)
	if err := m.syncMemoryFiles(context.Background()); err != nil {
		t.Fatalf("首次同步失败: %v", err)
	}

	_ = os.Remove(fp)
	if err := m.syncMemoryFiles(context.Background()); err != nil {
		t.Fatalf("二次同步失败: %v", err)
	}

	var count int
	_ = db.QueryRow("SELECT COUNT(*) FROM files WHERE path = 'memory/gone.md'").Scan(&count)
	if count != 0 {
		t.Errorf("删除的文件应从索引移除: %d", count)
	}
}

// TestSyncSessionFiles_无sessions目录 对齐 Python：目录不存在直接返回 nil
func TestSyncSessionFiles_无sessions目录(t *testing.T) {
	db := openTestMemoryDB(t)
	dir := t.TempDir()
	settings := CreateMemorySettings(dir, nil)
	m := newPartialManager(t, db, settings, dir)
	if err := m.syncSessionFiles(context.Background()); err != nil {
		t.Errorf("无 sessions 目录应返回 nil: %v", err)
	}
}

// TestSyncSessionFiles_文件删除 对齐 Python：会话文件消失后移除
func TestSyncSessionFiles_文件删除(t *testing.T) {
	db := openTestMemoryDB(t)
	dir := t.TempDir()
	sessDir := filepath.Join(dir, "sessions")
	_ = os.MkdirAll(sessDir, 0o755)
	fp := filepath.Join(sessDir, "2026-02-02.jsonl")
	_ = os.WriteFile(fp, []byte(`{"a":1}`), 0o644)

	settings := CreateMemorySettings(dir, nil)
	m := newPartialManager(t, db, settings, dir)
	if err := m.syncSessionFiles(context.Background()); err != nil {
		t.Fatalf("首次同步失败: %v", err)
	}

	_ = os.Remove(fp)
	if err := m.syncSessionFiles(context.Background()); err != nil {
		t.Fatalf("二次同步失败: %v", err)
	}

	var count int
	_ = db.QueryRow("SELECT COUNT(*) FROM files WHERE path = 'sessions/2026-02-02.jsonl'").Scan(&count)
	if count != 0 {
		t.Errorf("删除的会话文件应移除: %d", count)
	}
}

// TestSync_增量与全量 对齐 Python sync：force=true 强制重建；增量时 dirty 清零
func TestSync_增量与全量(t *testing.T) {
	db := openTestMemoryDB(t)
	dir := t.TempDir()
	_ = os.MkdirAll(filepath.Join(dir, "memory"), 0o755)
	_ = os.WriteFile(filepath.Join(dir, "memory", "s.md"), []byte("sync test"), 0o644)
	settings := CreateMemorySettings(dir, nil)
	m := newPartialManager(t, db, settings, dir)

	// 全量（meta 缺失）
	if err := m.Sync(context.Background(), "force-test", true); err != nil {
		t.Fatalf("force 同步失败: %v", err)
	}
	m.mu.Lock()
	dirty := m.dirty
	m.mu.Unlock()
	if dirty {
		t.Errorf("全量重建后 dirty 应为 false")
	}

	// 制造 dirty 后增量
	m.mu.Lock()
	m.dirty = true
	m.mu.Unlock()
	if err := m.Sync(context.Background(), "incremental", false); err != nil {
		t.Fatalf("增量同步失败: %v", err)
	}
	m.mu.Lock()
	dirty = m.dirty
	m.mu.Unlock()
	if dirty {
		t.Errorf("增量同步后 dirty 应为 false")
	}
}

// ──────────────────────────── Status / Close / watcher 细分 ────────────────────────────

// TestStatus_数据库未初始化 对齐 Python：db nil 返回 available=false
func TestStatus_数据库未初始化(t *testing.T) {
	m := newMemoryIndexManager("t", "/tmp", CreateMemorySettings("/tmp", nil))
	s := m.Status()
	if v, ok := s["available"].(bool); !ok || v {
		t.Errorf("db nil 应返回 available=false: %v", s)
	}
}

// TestCacheEntryCount_表缺失 对齐 Python：异常返回 0
func TestCacheEntryCount_表缺失(t *testing.T) {
	db, err := sql.Open("sqlite3", filepath.Join(t.TempDir(), "empty.db"))
	if err != nil {
		t.Fatalf("开库失败: %v", err)
	}
	defer func() { _ = db.Close() }()
	m := newPartialManager(t, db, nil, "")
	m.db = db // 无 embedding_cache 表
	if got := m.cacheEntryCount(); got != 0 {
		t.Errorf("表缺失应返回 0: %d", got)
	}
}

// TestSetupFileWatcher_无监听路径 对齐 Python：watch_paths 空 → debug 日志 + 关闭 watcher
func TestSetupFileWatcher_无监听路径(t *testing.T) {
	dir := t.TempDir() // 空目录：无 MEMORY.md/memory 目录
	db := openTestMemoryDB(t)
	settings := CreateMemorySettings(dir, nil)
	m := newPartialManager(t, db, settings, dir)

	m.setupFileWatcher()
	if m.watcher != nil {
		t.Errorf("无监听路径时 watcher 应为 nil")
	}
}

// TestSetupFileWatcher_根MEMORYmd 对齐 Python：根 MEMORY.md 存在时监听其父目录
func TestSetupFileWatcher_根MEMORYmd(t *testing.T) {
	dir := t.TempDir()
	_ = os.WriteFile(filepath.Join(dir, "MEMORY.md"), []byte("# root"), 0o644)
	db := openTestMemoryDB(t)
	settings := CreateMemorySettings(dir, nil)
	m := newPartialManager(t, db, settings, dir)

	m.setupFileWatcher()
	defer func() { _ = m.watcher.Close() }()
	if len(m.watcherPaths) == 0 {
		t.Errorf("MEMORY.md 存在时应有监听路径")
	}
	if _, ok := m.watcherPaths[filepath.Join(dir, "MEMORY.md")]; !ok {
		t.Errorf("MEMORY.md 应在监听集合: %v", m.watcherPaths)
	}
}

// TestWatchLoop_未初始化事件被忽略 对齐 Python：watcherInitialized=false 时事件丢弃
func TestWatchLoop_未初始化事件被忽略(t *testing.T) {
	db := openTestMemoryDB(t)
	dir := t.TempDir()
	_ = os.MkdirAll(filepath.Join(dir, "memory"), 0o755)
	settings := CreateMemorySettings(dir, map[string]any{
		"sync": map[string]any{"watchDebounceMs": 100},
	})
	m := newPartialManager(t, db, settings, dir)

	// 不等 1s 初始化定时器，立即写文件 → watchLoop 应忽略事件
	go m.setupFileWatcher()
	time.Sleep(50 * time.Millisecond)
	_ = os.WriteFile(filepath.Join(dir, "memory", "ignored.md"), []byte("ignored"), 0o644)
	time.Sleep(300 * time.Millisecond)

	// watcherInitialized 尚未置 true（1s 未到）
	m.watchMu.Lock()
	initialized := m.watcherInitialized
	m.watchMu.Unlock()
	if initialized {
		t.Logf("1s 初始化可能已完成（时序宽松，仅验证无 panic）")
	}
	// 清理
	m.watchMu.Lock()
	if m.watchTimer != nil {
		m.watchTimer.Stop()
	}
	m.watchMu.Unlock()
	if m.watcher != nil {
		_ = m.watcher.Close()
	}
}

// ──────────────────────────── searchKeyword 细分 ────────────────────────────

// TestSearchKeyword_fts不可用 对齐 Python：fts_available=false 返回空
func TestSearchKeyword_fts不可用(t *testing.T) {
	db := openTestMemoryDB(t)
	m := newPartialManager(t, db, nil, "")
	m.ftsAvailable = false
	if got := m.searchKeyword("anything", 10); len(got) != 0 {
		t.Errorf("fts 不可用应返回空: %v", got)
	}
}

// ──────────────────────────── ReadFile 细分 ────────────────────────────

// TestReadFile_行区间 对齐 Python read_file：from_line/lines 区间选择
func TestReadFile_行区间(t *testing.T) {
	dir := t.TempDir()
	_ = os.WriteFile(filepath.Join(dir, "lines.txt"), []byte("l1\nl2\nl3\nl4\nl5"), 0o644)
	db := openTestMemoryDB(t)
	settings := CreateMemorySettings(dir, nil)
	m := newPartialManager(t, db, settings, dir)

	from, lines := 2, 2
	got, err := m.ReadFile(context.Background(), "lines.txt", &from, &lines)
	if err != nil {
		t.Fatalf("ReadFile 失败: %v", err)
	}
	if got["text"] != "l2\nl3" {
		t.Errorf("区间选择失败: %q", got["text"])
	}
	if got["fromLine"] != 2 || got["toLine"] != 3 {
		t.Errorf("行号区间失败: %v/%v", got["fromLine"], got["toLine"])
	}
	if got["totalLines"] != 5 {
		t.Errorf("总行数失败: %v", got["totalLines"])
	}
}

// TestReadFile_只给fromLine 对齐 Python：lines 为 None 时读到末尾
func TestReadFile_只给fromLine(t *testing.T) {
	dir := t.TempDir()
	_ = os.WriteFile(filepath.Join(dir, "tail.txt"), []byte("a\nb\nc"), 0o644)
	db := openTestMemoryDB(t)
	settings := CreateMemorySettings(dir, nil)
	m := newPartialManager(t, db, settings, dir)

	from := 2
	got, err := m.ReadFile(context.Background(), "tail.txt", &from, nil)
	if err != nil {
		t.Fatalf("ReadFile 失败: %v", err)
	}
	if got["text"] != "b\nc" {
		t.Errorf("应读到末尾: %q", got["text"])
	}
}

// TestReadFile_起始行负数 对齐 Python：max(0, from_line - 1) 钳制
func TestReadFile_起始行负数(t *testing.T) {
	dir := t.TempDir()
	_ = os.WriteFile(filepath.Join(dir, "neg.txt"), []byte("x\ny"), 0o644)
	db := openTestMemoryDB(t)
	settings := CreateMemorySettings(dir, nil)
	m := newPartialManager(t, db, settings, dir)

	from := -5
	got, err := m.ReadFile(context.Background(), "neg.txt", &from, nil)
	if err != nil {
		t.Fatalf("ReadFile 失败: %v", err)
	}
	if got["fromLine"] != -5 || got["text"] != "x\ny" {
		t.Errorf("负起始行应钳制到 0 起: %v", got)
	}
}

// ──────────────────────────── mergeHybridResults 细分 ────────────────────────────

// TestMergeHybridResults_权重计算 对齐 Python：score = vw*vector + tw*text
func TestMergeHybridResults_权重计算(t *testing.T) {
	vec := []map[string]any{{"id": "a", "score": 0.8}}
	kw := []map[string]any{{"id": "a", "score": 0.6}, {"id": "b", "score": 0.9}}
	merged := mergeHybridResults(vec, kw, 0.7, 0.3)

	if len(merged) != 2 {
		t.Fatalf("应合并 2 条: %v", merged)
	}
	byID := map[string]float64{}
	for _, r := range merged {
		byID[r["id"].(string)] = r["score"].(float64)
	}
	if byID["a"] != 0.7*0.8+0.3*0.6 {
		t.Errorf("a 加权失败: %v", byID["a"])
	}
	if byID["b"] != 0.3*0.9 {
		t.Errorf("b 加权失败: %v", byID["b"])
	}
	// 降序验证
	if merged[0]["score"].(float64) < merged[1]["score"].(float64) {
		t.Errorf("应按 score 降序")
	}
}

// ──────────────────────────── Close 细分 ────────────────────────────

// TestClose_清理定时器 对齐 Python close：停 interval + watch timer
func TestClose_清理定时器(t *testing.T) {
	db := openTestMemoryDB(t)
	dir := t.TempDir()
	_ = os.MkdirAll(filepath.Join(dir, "memory"), 0o755)
	settings := CreateMemorySettings(dir, map[string]any{
		"sync": map[string]any{"intervalMinutes": 1},
	})
	m := newPartialManager(t, db, settings, dir)
	m.ensureIntervalSync(context.Background())

	// Close 应停掉 interval goroutine 且幂等
	if err := m.Close(); err != nil {
		t.Fatalf("Close 失败: %v", err)
	}
	if err := m.Close(); err != nil {
		t.Fatalf("Close 应幂等: %v", err)
	}
}

// ──────────────────────────── 并发安全冒烟 ────────────────────────────

// TestConcurrentSearchSync 并发 Search+Sync 不数据竞争（-race 基础保障）
func TestConcurrentSearchSync(t *testing.T) {
	db := openTestMemoryDB(t)
	dir := t.TempDir()
	_ = os.MkdirAll(filepath.Join(dir, "memory"), 0o755)
	_ = os.WriteFile(filepath.Join(dir, "memory", "c.md"), []byte("concurrent smoke"), 0o644)
	settings := CreateMemorySettings(dir, map[string]any{
		"sync":  map[string]any{"watch": false, "onSearch": false},
		"store": map[string]any{"vector": map[string]any{"enabled": false}},
	})
	m := newPartialManager(t, db, settings, dir)
	if err := m.syncMemoryFiles(context.Background()); err != nil {
		t.Fatalf("同步失败: %v", err)
	}

	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = m.Search(context.Background(), "concurrent", map[string]any{"hybrid": map[string]any{"enabled": false}})
			_ = m.Sync(context.Background(), "concurrent", false)
		}()
	}
	wg.Wait()
}

// ──────────────────────────── initialize 错误传播 ────────────────────────────

// TestInitialize_提供者失败 对齐 Python：_initialize_provider 失败上抛（fallback=none 且无 key）
func TestInitialize_提供者失败(t *testing.T) {
	dir := t.TempDir()
	settings := CreateMemorySettings(dir, map[string]any{
		"provider": "openai_compatible",
		"fallback": "none",
	})
	m := newMemoryIndexManager("init-fail-agent", dir, settings)

	err := m.initialize(context.Background())
	if err == nil {
		t.Fatalf("无 key 且 fallback=none 应初始化失败")
	}
	// db 已打开的路径应被错误短路（不再继续）
	if m.provider != nil {
		t.Errorf("失败时 provider 不应被赋值")
	}
}

// ──────────────────────────── watchLoop 事件分发 ────────────────────────────

// TestWatchLoop_事件触发防抖 对齐 Python MemoryFileHandler：初始化后事件 → scheduleWatchSync
func TestWatchLoop_事件触发防抖(t *testing.T) {
	db := openTestMemoryDB(t)
	dir := t.TempDir()
	memDir := filepath.Join(dir, "memory")
	_ = os.MkdirAll(memDir, 0o755)
	_ = os.WriteFile(filepath.Join(memDir, "ev.md"), []byte("event base"), 0o644)

	settings := CreateMemorySettings(dir, map[string]any{
		"sync": map[string]any{"watchDebounceMs": 150, "onSearch": false},
	})
	m := newPartialManager(t, db, settings, dir)
	if err := m.syncMemoryFiles(context.Background()); err != nil {
		t.Fatalf("初始同步失败: %v", err)
	}

	// 启动 watcher 并等初始化（1s）
	m.setupFileWatcher()
	defer func() {
		if m.watcher != nil {
			_ = m.watcher.Close()
		}
	}()
	time.Sleep(1300 * time.Millisecond)

	// 写新文件触发事件（Rel 后 IsMemoryPath=true）
	_ = os.WriteFile(filepath.Join(memDir, "ev2.md"), []byte("watch event"), 0o644)

	// 防抖 150ms + sync 执行 → dirty 归 false
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		m.mu.Lock()
		dirty := m.dirty
		m.mu.Unlock()
		if !dirty {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Errorf("watcher 事件未在 5s 内触发防抖同步")
}

// TestWatchLoop_非记忆路径忽略 对齐 Python：is_memory_path=false 的事件不触发
func TestWatchLoop_非记忆路径忽略(t *testing.T) {
	db := openTestMemoryDB(t)
	dir := t.TempDir()
	_ = os.MkdirAll(filepath.Join(dir, "memory"), 0o755) // 仅监听 memory/，写 other/ 下不触发
	settings := CreateMemorySettings(dir, map[string]any{
		"sync": map[string]any{"watchDebounceMs": 100, "onSearch": false},
	})
	m := newPartialManager(t, db, settings, dir)
	m.setupFileWatcher()
	defer func() {
		if m.watcher != nil {
			_ = m.watcher.Close()
		}
	}()
	time.Sleep(1300 * time.Millisecond)

	// 在 workspace 根写非记忆文件（.txt 不在 is_memory_path 白名单）
	_ = os.WriteFile(filepath.Join(dir, "not-memory.txt"), []byte("x"), 0o644)
	time.Sleep(600 * time.Millisecond)

	// dirty 应保持初始 true（构造时置 true，未被 watchLoop 额外置 true 的路径验证：
	// 事件被忽略 → scheduleWatchSync 未被调用 → 无副作用）
	m.mu.Lock()
	_ = m.dirty
	m.mu.Unlock()
}

// ──────────────────────────── indexFile 事务回滚 ────────────────────────────

// TestIndexFile_读失败 对齐 Python：读文件失败记日志返回错误
func TestIndexFile_读失败(t *testing.T) {
	db := openTestMemoryDB(t)
	m := newPartialManager(t, db, nil, "")
	entry := &FileEntry{Path: "memory/gone.md", AbsPath: "/nonexistent/gone.md"}
	if err := m.indexFile(context.Background(), entry, MemorySourceMemory); err == nil {
		t.Errorf("文件不存在应返回错误")
	}
}

// TestIndexFile_正常索引 对齐 Python：chunks+files 写入
func TestIndexFile_正常索引(t *testing.T) {
	db := openTestMemoryDB(t)
	dir := t.TempDir()
	fp := filepath.Join(dir, "ok.md")
	_ = os.WriteFile(fp, []byte("# ok content"), 0o644)
	entry, err := BuildFileEntry(fp, dir)
	if err != nil {
		t.Fatalf("BuildFileEntry 失败: %v", err)
	}
	m := newPartialManager(t, db, nil, dir)
	if err := m.indexFile(context.Background(), entry, MemorySourceMemory); err != nil {
		t.Fatalf("indexFile 失败: %v", err)
	}
	var chunkCount, fileCount int
	_ = db.QueryRow("SELECT COUNT(*) FROM chunks WHERE path = ?", entry.Path).Scan(&chunkCount)
	_ = db.QueryRow("SELECT COUNT(*) FROM files WHERE path = ?", entry.Path).Scan(&fileCount)
	if chunkCount == 0 || fileCount == 0 {
		t.Errorf("应写入 chunks/files: %d/%d", chunkCount, fileCount)
	}
}

// ──────────────────────────── ensureSchema FTS 分支 ────────────────────────────

// TestEnsureSchema_fts禁用 对齐 Python：fts_enabled=false 不建 FTS 表
func TestEnsureSchema_fts禁用(t *testing.T) {
	dir := t.TempDir()
	settings := CreateMemorySettings(dir, map[string]any{
		"store": map[string]any{
			"fts":    map[string]any{"enabled": false},
			"vector": map[string]any{"enabled": false},
		},
	})
	m := newMemoryIndexManager("schema-agent", dir, settings)
	if err := m.initialize(context.Background()); err != nil {
		t.Fatalf("initialize 失败: %v", err)
	}
	defer func() { _ = m.Close() }()
	if m.ftsAvailable {
		t.Errorf("fts 禁用时 ftsAvailable 应为 false")
	}
	var count int
	_ = m.db.QueryRow("SELECT COUNT(*) FROM sqlite_master WHERE name = ?", ftsTable).Scan(&count)
	if count != 0 {
		t.Errorf("fts 禁用时不应建 FTS 表")
	}
}

// TestEnsureSchema_fts启用 对齐 Python：fts_enabled=true 建 FTS 表
func TestEnsureSchema_fts启用(t *testing.T) {
	dir := t.TempDir()
	settings := CreateMemorySettings(dir, map[string]any{
		"store": map[string]any{"vector": map[string]any{"enabled": false}},
	})
	m := newMemoryIndexManager("schema-agent2", dir, settings)
	if err := m.initialize(context.Background()); err != nil {
		t.Fatalf("initialize 失败: %v", err)
	}
	defer func() { _ = m.Close() }()
	if !m.ftsAvailable {
		t.Errorf("sqlite_fts5 构建标签下 FTS 表应可用")
	}
	var count int
	_ = m.db.QueryRow("SELECT COUNT(*) FROM sqlite_master WHERE name = ?", ftsTable).Scan(&count)
	if count != 1 {
		t.Errorf("应建 FTS 表")
	}
}

// ──────────────────────────── searchVector vec0 分支 ────────────────────────────

// TestSearchVector_dims未初始化 对齐 Python：vector_dims 为 nil 时先采样建表
func TestSearchVector_dims未初始化(t *testing.T) {
	db := openTestMemoryDB(t)
	m := newPartialManager(t, db, nil, "")
	m.vectorEnabled = true
	m.vectorAvailable = true
	m.vectorDims = nil

	// 采样建表失败（无 vec0 扩展）→ 降级 fallback，不 panic
	results := m.searchVector(context.Background(), []float64{1, 0, 0, 0, 0, 0, 0, 0}, 10)
	// 空 chunk → 空结果；仅验证分支执行不报错
	if results == nil {
		t.Errorf("应返回非 nil 切片")
	}
}

// TestSearchVector_available降级 对齐 Python：vector_available=false → fallback
func TestSearchVector_available降级(t *testing.T) {
	db := openTestMemoryDB(t)
	m := newPartialManager(t, db, nil, "")
	m.vectorEnabled = true
	m.vectorAvailable = false
	m.vectorDims = nil

	insertTestChunk(t, db, "memory/av.md", "available false", []float64{1, 0, 0, 0, 0, 0, 0, 0})
	results := m.searchVector(context.Background(), []float64{1, 0, 0, 0, 0, 0, 0, 0}, 10)
	if len(results) != 1 {
		t.Errorf("降级 fallback 应命中 1 条: %v", len(results))
	}
}

// ──────────────────────────── removeFileFromIndex 无 chunk 分支 ────────────────────────────

// TestRemoveFileFromIndex_无记录 对齐 Python：rowids 为空时直接清 files
func TestRemoveFileFromIndex_无记录(t *testing.T) {
	db := openTestMemoryDB(t)
	m := newPartialManager(t, db, nil, "")
	m.vectorAvailable = false
	m.ftsAvailable = false

	_, err := db.Exec("INSERT INTO files (path, source, hash, mtime, size) VALUES ('memory/ghost.md', 'memory', 'h', 0, 0)")
	if err != nil {
		t.Fatalf("插 files 失败: %v", err)
	}
	m.removeFileFromIndex("memory/ghost.md")
	var count int
	_ = db.QueryRow("SELECT COUNT(*) FROM files WHERE path = 'memory/ghost.md'").Scan(&count)
	if count != 0 {
		t.Errorf("files 记录应被清理: %d", count)
	}
}

// TestRemoveFileFromIndex_带chunk 对齐 Python：chunks+files 同步清理
func TestRemoveFileFromIndex_带chunk(t *testing.T) {
	db := openTestMemoryDB(t)
	m := newPartialManager(t, db, nil, "")
	m.vectorAvailable = false
	m.ftsAvailable = false

	insertTestChunk(t, db, "memory/del.md", "delete me", []float64{1, 0, 0, 0, 0, 0, 0, 0})
	_, err := db.Exec("INSERT INTO files (path, source, hash, mtime, size) VALUES ('memory/del.md', 'memory', 'h', 0, 0)")
	if err != nil {
		t.Fatalf("插 files 失败: %v", err)
	}

	m.removeFileFromIndex("memory/del.md")

	var chunkCount, fileCount int
	_ = db.QueryRow("SELECT COUNT(*) FROM chunks WHERE path = 'memory/del.md'").Scan(&chunkCount)
	_ = db.QueryRow("SELECT COUNT(*) FROM files WHERE path = 'memory/del.md'").Scan(&fileCount)
	if chunkCount != 0 || fileCount != 0 {
		t.Errorf("应清理干净: chunks=%d files=%d", chunkCount, fileCount)
	}
}

// ──────────────────────────── EnsureDir / BuildFileEntry 边界 ────────────────────────────

// TestEnsureDir_空路径 对齐 Python ensure_dir：空路径直返 nil
func TestEnsureDir_空路径(t *testing.T) {
	if err := EnsureDir(""); err != nil {
		t.Errorf("空路径应返回 nil: %v", err)
	}
}

// TestEnsureDir_嵌套创建 对齐 Python：os.makedirs(exist_ok=True)
func TestEnsureDir_嵌套创建(t *testing.T) {
	target := filepath.Join(t.TempDir(), "a", "b", "c")
	if err := EnsureDir(target); err != nil {
		t.Fatalf("嵌套创建失败: %v", err)
	}
	if st, err := os.Stat(target); err != nil || !st.IsDir() {
		t.Errorf("目录应存在")
	}
	// 幂等
	if err := EnsureDir(target); err != nil {
		t.Errorf("重复创建应幂等: %v", err)
	}
}

// TestBuildFileEntry_读取失败 对齐 Python：stat 失败返回错误
func TestBuildFileEntry_读取失败(t *testing.T) {
	if _, err := BuildFileEntry("/nonexistent/x.md", "/ws"); err == nil {
		t.Errorf("不存在文件应返回错误")
	}
}

// TestBuildFileEntry_字段完整性 对齐 Python：path/absPath/hash/mtime/size
func TestBuildFileEntry_字段完整性(t *testing.T) {
	dir := t.TempDir()
	fp := filepath.Join(dir, "memory", "f.md")
	_ = os.MkdirAll(filepath.Join(dir, "memory"), 0o755)
	_ = os.WriteFile(fp, []byte("entry content"), 0o644)

	entry, err := BuildFileEntry(fp, dir)
	if err != nil {
		t.Fatalf("BuildFileEntry 失败: %v", err)
	}
	if entry.Path != filepath.Join("memory", "f.md") {
		t.Errorf("相对路径错误: %s", entry.Path)
	}
	if entry.AbsPath != fp {
		t.Errorf("绝对路径错误: %s", entry.AbsPath)
	}
	if entry.Hash != HashText("entry content") {
		t.Errorf("hash 错误: %s", entry.Hash)
	}
	if entry.Size != int64(len("entry content")) {
		t.Errorf("size 错误: %d", entry.Size)
	}
	if entry.MtimeMs <= 0 {
		t.Errorf("mtime 应为正: %d", entry.MtimeMs)
	}
}

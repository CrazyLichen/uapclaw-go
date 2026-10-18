package memory

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// newTestManager 构造测试 manager（临时目录 + 显式 settings）
func newTestManager(t *testing.T, agentID string) *MemoryIndexManager {
	t.Helper()
	dir := t.TempDir()
	settings := CreateMemorySettings(dir, nil)
	mgr, err := GetMemoryManager(context.Background(), agentID, dir, settings)
	if err != nil {
		t.Fatalf("GetMemoryManager 失败: %v", err)
	}
	if mgr == nil {
		t.Fatal("mgr 不应为 nil")
	}
	t.Cleanup(func() { _ = mgr.Close() })
	return mgr
}

// TestGetMemoryManager_缓存幂等 对齐 Python INDEX_CACHE 语义
func TestGetMemoryManager_缓存幂等(t *testing.T) {
	dir := t.TempDir()
	m1, err := GetMemoryManager(context.Background(), "agent-cache-test", dir, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = m1.Close() }()
	m2, err := GetMemoryManager(context.Background(), "agent-cache-test", dir, nil)
	if err != nil {
		t.Fatal(err)
	}
	if m1 != m2 {
		t.Errorf("同 key 应返回缓存实例")
	}
}

// TestGetMemoryManager_ClearCache 对齐 Python clear_memory_manager_cache
func TestGetMemoryManager_ClearCache(t *testing.T) {
	dir := t.TempDir()
	m1, err := GetMemoryManager(context.Background(), "agent-clear-test", dir, nil)
	if err != nil {
		t.Fatal(err)
	}
	ClearMemoryManagerCache()
	m2, err := GetMemoryManager(context.Background(), "agent-clear-test", dir, nil)
	if err != nil {
		t.Fatal(err)
	}
	if m1 == m2 {
		t.Errorf("清缓存后应新建实例")
	}
	_ = m1.Close()
	_ = m2.Close()
}

// TestResolveDBPath 对齐 Python _resolve_db_path：
// 绝对路径直返 / workspace_name 前缀去重 / memory/ 子目录保证
func TestResolveDBPath(t *testing.T) {
	m := &MemoryIndexManager{WorkspaceDir: "/ws/myproj"}
	cases := []struct {
		storePath, want string
	}{
		{"/abs/path/memory.db", "/abs/path/memory.db"},        // 绝对路径
		{"myproj/memory.db", "/ws/myproj/memory/memory.db"},    // 前缀去重后补 memory/
		{"memory.db", "/ws/myproj/memory/memory.db"},           // 补 memory/
		{"memory/custom.db", "/ws/myproj/memory/custom.db"},    // 已含 memory/ 不重复
	}
	for _, c := range cases {
		m.Settings = &MemorySettings{Store: StoreConfig{Path: c.storePath}}
		if got := m.resolveDBPath(); got != c.want {
			t.Errorf("resolveDBPath(%q)=%q want %q", c.storePath, got, c.want)
		}
	}
}

// TestInitialize_SchemaCreation 对齐 Python _ensure_schema：四表创建
func TestInitialize_SchemaCreation(t *testing.T) {
	mgr := newTestManager(t, "agent-schema")
	s := mgr.Status()
	if !s["available"].(bool) {
		t.Fatalf("应 available")
	}
	if s["provider"] != "mock" && s["provider"] != "openai_compatible" {
		t.Errorf("provider 异常: %v", s["provider"])
	}
}

// TestSyncMemoryFiles_增量跳过 对齐 Python：hash 未变跳过重新索引
func TestSyncMemoryFiles_增量跳过(t *testing.T) {
	dir := t.TempDir()
	_ = os.MkdirAll(filepath.Join(dir, "memory"), 0o755)
	_ = os.WriteFile(filepath.Join(dir, "memory", "MEMORY.md"), []byte("# hello memory"), 0o644)

	mgr, err := GetMemoryManager(context.Background(), "agent-sync-test", dir, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = mgr.Close() }()

	s1 := mgr.Status()
	if s1["files"].(int) != 1 {
		t.Fatalf("初始同步应索引 1 个文件: %v", s1["files"])
	}
	if s1["chunks"].(int) < 1 {
		t.Fatalf("应有至少 1 个 chunk")
	}

	// 再 Sync：hash 未变，文件数不变
	if err := mgr.Sync(context.Background(), "manual", false); err != nil {
		t.Fatal(err)
	}
	s2 := mgr.Status()
	if s2["files"].(int) != 1 {
		t.Errorf("增量同步不应改变文件数")
	}
}

// TestSyncMemoryFiles_删除消失文件 对齐 Python _remove_file_from_index
func TestSyncMemoryFiles_删除消失文件(t *testing.T) {
	dir := t.TempDir()
	_ = os.MkdirAll(filepath.Join(dir, "memory"), 0o755)
	p := filepath.Join(dir, "memory", "temp.md")
	_ = os.WriteFile(p, []byte("temp content"), 0o644)

	mgr, err := GetMemoryManager(context.Background(), "agent-del-test", dir, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = mgr.Close() }()
	if mgr.Status()["files"].(int) != 1 {
		t.Fatalf("初始应 1 文件")
	}

	_ = os.Remove(p)
	if err := mgr.Sync(context.Background(), "manual", true); err != nil { // force 触发全量
		t.Fatal(err)
	}
	if mgr.Status()["files"].(int) != 0 {
		t.Errorf("消失文件应被清理")
	}
}

// TestSearch_混合检索 对齐 Python search：FTS 关键词命中
func TestSearch_混合检索(t *testing.T) {
	dir := t.TempDir()
	_ = os.MkdirAll(filepath.Join(dir, "memory"), 0o755)
	_ = os.WriteFile(filepath.Join(dir, "memory", "notes.md"), []byte("# Project Alpha\n\nWe decided to use golang for the gateway service."), 0o644)

	mgr, err := GetMemoryManager(context.Background(), "agent-search-test", dir, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = mgr.Close() }()

	results, err := mgr.Search(context.Background(), "golang gateway", map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) == 0 {
		t.Fatalf("FTS 应命中 'golang'/'gateway'")
	}
	r := results[0]
	if r["path"] != "memory/notes.md" && r["path"] != filepath.Join("memory", "notes.md") {
		t.Errorf("path 错误: %v", r["path"])
	}
	if r["snippet"] == "" {
		t.Errorf("snippet 不应为空")
	}
}

// TestSearch_minScore_过滤 对齐 Python：minScore 过滤行为
func TestSearch_minScore_过滤(t *testing.T) {
	dir := t.TempDir()
	_ = os.MkdirAll(filepath.Join(dir, "memory"), 0o755)
	_ = os.WriteFile(filepath.Join(dir, "memory", "a.md"), []byte("apple banana"), 0o644)
	_ = os.WriteFile(filepath.Join(dir, "memory", "b.md"), []byte("zebra xyz"), 0o644)

	mgr, err := GetMemoryManager(context.Background(), "agent-minscore-test", dir, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = mgr.Close() }()

	// minScore=0.99：过滤掉低分结果
	results, _ := mgr.Search(context.Background(), "apple", map[string]any{"minScore": 0.99, "hybrid": map[string]any{"enabled": false}})
	for _, r := range results {
		if r["score"].(float64) < 0.99 {
			t.Errorf("应全部 >= 0.99: %v", r["score"])
		}
	}
}

// TestSearch_空查询 对齐 Python：cleaned 为空返回 []
func TestSearch_空查询(t *testing.T) {
	mgr := newTestManager(t, "agent-emptyquery-test")
	results, err := mgr.Search(context.Background(), "   ", map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 0 {
		t.Errorf("空查询应返回空: %v", results)
	}
}

// TestEmbeddingCache 对齐 Python _get_embedding 四键缓存
func TestEmbeddingCache(t *testing.T) {
	dir := t.TempDir()
	_ = os.MkdirAll(filepath.Join(dir, "memory"), 0o755)
	_ = os.WriteFile(filepath.Join(dir, "memory", "x.md"), []byte("cache test content"), 0o644)

	mgr, err := GetMemoryManager(context.Background(), "agent-embedcache-test", dir, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = mgr.Close() }()
	if _, err := mgr.Search(context.Background(), "cache test", map[string]any{}); err != nil {
		t.Fatal(err)
	}
	// 二次搜索应命中缓存（不报错即通过；entries 计数 > 0 验证缓存写入）
	s := mgr.Status()
	cacheInfo, _ := s["cache"].(map[string]any)
	if cacheInfo == nil || cacheInfo["entries"].(int) < 1 {
		t.Errorf("缓存应至少 1 条: %v", s["cache"])
	}
}

// TestSyncSessionFiles 对齐 Python _sync_session_files：sessions/*.jsonl 索引
func TestSyncSessionFiles(t *testing.T) {
	dir := t.TempDir()
	_ = os.MkdirAll(filepath.Join(dir, "sessions"), 0o755)
	_ = os.WriteFile(filepath.Join(dir, "sessions", "2026-01-01.jsonl"), []byte(`{"role":"user","content":"hello session"}`), 0o644)

	mgr, err := GetMemoryManager(context.Background(), "agent-session-test", dir, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = mgr.Close() }()
	s := mgr.Status()
	// sources 默认含 sessions
	found := false
	if counts, ok := s["sourceCounts"].([]map[string]any); ok {
		for _, c := range counts {
			if c["source"] == "sessions" && c["files"].(int) >= 1 {
				found = true
			}
		}
	}
	if !found {
		t.Errorf("sessions 应被索引: %v", s["sourceCounts"])
	}
}

// TestFileWatcher_防抖同步 对齐 Python _setup_file_watcher + _schedule_watch_sync
func TestFileWatcher_防抖同步(t *testing.T) {
	dir := t.TempDir()
	_ = os.MkdirAll(filepath.Join(dir, "memory"), 0o755)
	_ = os.WriteFile(filepath.Join(dir, "memory", "watch.md"), []byte("initial"), 0o644)

	mgr, err := GetMemoryManager(context.Background(), "agent-watch-test", dir, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = mgr.Close() }()

	// watcher 1s 后初始化 + 2s 防抖，等待后验证
	_ = os.WriteFile(filepath.Join(dir, "memory", "watch.md"), []byte("updated content"), 0o644)

	deadline := time.Now().Add(8 * time.Second)
	for time.Now().Before(deadline) {
		results, _ := mgr.Search(context.Background(), "updated content", map[string]any{"minScore": 0.0, "hybrid": map[string]any{"enabled": false}})
		if len(results) > 0 {
			return // watcher 触发同步成功
		}
		time.Sleep(300 * time.Millisecond)
	}
	t.Errorf("watcher 未在 8s 内触发同步")
}

// TestStatus_结构完整 对齐 Python status() 字段
func TestStatus_结构完整(t *testing.T) {
	mgr := newTestManager(t, "agent-status")
	s := mgr.Status()
	for _, key := range []string{"available", "provider", "model", "files", "chunks", "sourceCounts", "dirty", "fts", "vector", "cache"} {
		if _, ok := s[key]; !ok {
			t.Errorf("status 缺少 %s", key)
		}
	}
}

// TestReadFile 对齐 Python read_file：行区间读取（readlines 保留行尾）
func TestReadFile(t *testing.T) {
	dir := t.TempDir()
	_ = os.MkdirAll(filepath.Join(dir, "memory"), 0o755)
	_ = os.WriteFile(filepath.Join(dir, "memory", "lines.md"), []byte("l1\nl2\nl3\nl4\nl5"), 0o644)

	mgr, err := GetMemoryManager(context.Background(), "agent-readfile-test", dir, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = mgr.Close() }()

	res, err := mgr.ReadFile(context.Background(), filepath.Join("memory", "lines.md"), nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if res["totalLines"] != 5 {
		t.Errorf("totalLines got %v", res["totalLines"])
	}

	from := 2
	lines := 2
	res, err = mgr.ReadFile(context.Background(), filepath.Join("memory", "lines.md"), &from, &lines)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(res["text"].(string), "l2") || !strings.Contains(res["text"].(string), "l3") {
		t.Errorf("行区间读取错误: %v", res["text"])
	}
	if strings.Contains(res["text"].(string), "l1") || strings.Contains(res["text"].(string), "l4") {
		t.Errorf("越界行: %v", res["text"])
	}
}

// TestReadFile_不存在 对齐 Python：FileNotFoundError
func TestReadFile_不存在(t *testing.T) {
	mgr := newTestManager(t, "agent-readfile-none")
	_, err := mgr.ReadFile(context.Background(), "no/such.md", nil, nil)
	if err == nil {
		t.Errorf("文件不存在应报错")
	}
}

// TestClose_幂等 对齐 Python close：closed 标记 + 缓存清理
func TestClose_幂等(t *testing.T) {
	dir := t.TempDir()
	mgr, err := GetMemoryManager(context.Background(), "agent-close-test", dir, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := mgr.Close(); err != nil {
		t.Fatal(err)
	}
	if err := mgr.Close(); err != nil {
		t.Errorf("二次 Close 应幂等")
	}
}

// TestSync_关闭后 对齐 Python：closed 时 sync 直接返回
func TestSync_关闭后(t *testing.T) {
	dir := t.TempDir()
	mgr, err := GetMemoryManager(context.Background(), "agent-closedsync-test", dir, nil)
	if err != nil {
		t.Fatal(err)
	}
	_ = mgr.Close()
	if err := mgr.Sync(context.Background(), "after-close", false); err != nil {
		t.Errorf("closed 后 sync 应直接返回 nil: %v", err)
	}
}

// TestVectorBlobRoundTrip 向量序列化往返（对齐 Python struct.pack/unpack float32）
func TestVectorBlobRoundTrip(t *testing.T) {
	vec := []float64{0.5, -0.25, 3.14159, 0}
	blob := vectorToBlob(vec)
	if len(blob) != 16 {
		t.Fatalf("4 元素应 16 字节，got %d", len(blob))
	}
	back := blobToVector(blob)
	if len(back) != 4 {
		t.Fatalf("往返应 4 元素，got %d", len(back))
	}
	for i := range vec {
		if back[i] != float64(float32(vec[i])) {
			t.Errorf("元素 %d 往返不一致: %v vs %v", i, back[i], vec[i])
		}
	}
}

// TestBuildSourceFilter 对齐 Python _build_source_filter：repr 单引号 IN
func TestBuildSourceFilter(t *testing.T) {
	m := &MemoryIndexManager{Settings: &MemorySettings{Sources: []string{"memory", "sessions"}}}
	got := m.buildSourceFilter()
	if got != "source IN ('memory', 'sessions')" {
		t.Errorf("got %s", got)
	}
	m.Settings.Sources = []string{"memory"}
	if got := m.buildSourceFilter(); got != "source = 'memory'" {
		t.Errorf("单 source got %s", got)
	}
	m.Settings.Sources = nil
	if got := m.buildSourceFilter(); got != "1=0" {
		t.Errorf("空 sources got %s", got)
	}
}

// TestIsRecentSessionFile 对齐 Python _is_recent_session_file（定义未调用，1:1 同步）：
// 仅今天/昨天的 YYYY-MM-DD.md 返回 true
func TestIsRecentSessionFile(t *testing.T) {
	m := &MemoryIndexManager{}
	if m.isRecentSessionFile("not-a-date.md") {
		t.Errorf("非日期格式应为 false")
	}
	if m.isRecentSessionFile("2026-13-99.md") {
		t.Errorf("非法日期应为 false")
	}
	if m.isRecentSessionFile("2020-01-01.md") {
		t.Errorf("历史日期应为 false（仅今天/昨天）")
	}
	today := time.Now().Format("2006-01-02")
	if !m.isRecentSessionFile(today + ".md") {
		t.Errorf("今天应为 true")
	}
	yesterday := time.Now().AddDate(0, 0, -1).Format("2006-01-02")
	if !m.isRecentSessionFile(yesterday + ".md") {
		t.Errorf("昨天应为 true")
	}
}

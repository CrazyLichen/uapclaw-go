package indexer

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/retrieval/common"
)

// chdirRepoRoot 把测试进程 cwd 切到仓库根（vec0.so 相对路径 third_party/... 按
// cwd 解析；对齐 sqlite_store_test.go 同名助手）。
func chdirRepoRootForIndexer(t *testing.T) {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("无法定位测试文件路径")
	}
	dir := filepath.Dir(file)
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			break
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("未找到仓库根（go.mod）")
		}
		dir = parent
	}
	old, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(old) })
}

// newTestSQLiteIndexer 构造测试用 SQLiteIndexer（vec0 依赖 third_party 扩展，
// 缺失时跳过——对齐 sqlite_store_test.go 门槛）。
func newTestSQLiteIndexer(t *testing.T) *SQLiteIndexer {
	t.Helper()
	chdirRepoRootForIndexer(t)
	if _, err := os.Stat(filepath.Join("third_party", "sqlite-vec", runtime.GOOS+"-"+runtime.GOARCH)); err != nil {
		t.Skipf("third_party/sqlite-vec 不存在，vec0 依赖用例跳过")
	}
	cfg := common.VectorStoreConfig{
		StoreProvider:  common.StoreTypeSqlite,
		CollectionName: "kb_it-sqlite-ix",
		DistanceMetric: common.DistanceMetricCosine,
		DatabaseName:   "kb_it-sqlite-ix",
	}
	idx, err := NewSQLiteIndexer(cfg, filepath.Join(t.TempDir(), "kbvector.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = idx.Close() })
	return idx
}

// TestSQLiteIndexer_BuildIndex幂等重建 同 id 二次 BuildIndex 覆写不报错
// （UPSERT 幂等，对照 chroma 查重跳过语义的 sqlite 等价物，spec §9）。
func TestSQLiteIndexer_BuildIndex幂等重建(t *testing.T) {
	idx := newTestSQLiteIndexer(t)
	ctx := context.Background()
	chunks := []common.TextChunk{
		{ID: "c1", Text: "alpha document text", DocID: "d1", Metadata: map[string]any{"chunk_id": "c1"}},
		{ID: "c2", Text: "beta document text", DocID: "d1", Metadata: map[string]any{"chunk_id": "c2"}},
	}
	cfg := common.IndexConfig{IndexName: "kb_it-sqlite-ix_chunks", IndexType: "vector"}
	ok, err := idx.BuildIndex(ctx, chunks, cfg, nil) // 无嵌入模型：bm25-only 合法
	if err != nil || !ok {
		t.Fatalf("BuildIndex 失败: ok=%v err=%v", ok, err)
	}
	// 同 id 二次写入（含内容变更）→ UPSERT 覆写成功
	chunks[0].Text = "alpha updated text"
	ok, err = idx.BuildIndex(ctx, chunks, cfg, nil)
	if err != nil || !ok {
		t.Fatalf("二次 BuildIndex（UPSERT）失败: ok=%v err=%v", ok, err)
	}
}

// TestSQLiteIndexer_DeleteIndex按docID 写入两文档 → 删 d1 → 仅剩 d2 可检。
func TestSQLiteIndexer_DeleteIndex按docID(t *testing.T) {
	idx := newTestSQLiteIndexer(t)
	ctx := context.Background()
	cfg := common.IndexConfig{IndexName: "kb_it-sqlite-ix_chunks", IndexType: "vector"}
	_ , err := idx.BuildIndex(ctx, []common.TextChunk{
		{ID: "c1", Text: "doc one content", DocID: "d1", Metadata: map[string]any{"chunk_id": "c1"}},
		{ID: "c2", Text: "doc two content", DocID: "d2", Metadata: map[string]any{"chunk_id": "c2"}},
	}, cfg, nil)
	if err != nil {
		t.Fatal(err)
	}
	deleted, err := idx.DeleteIndex(ctx, "d1", "kb_it-sqlite-ix_chunks")
	if err != nil || !deleted {
		t.Fatalf("DeleteIndex 失败: deleted=%v err=%v", deleted, err)
	}
	ids, err := idx.Store().IDsByDoc(ctx, "d1")
	if err != nil || len(ids) != 0 {
		t.Fatalf("d1 删除后应为空: ids=%v err=%v", ids, err)
	}
	// 幂等：再删返回 false
	deleted, err = idx.DeleteIndex(ctx, "d1", "kb_it-sqlite-ix_chunks")
	if err != nil || deleted {
		t.Fatalf("幂等删除应 deleted=false: %v %v", deleted, err)
	}
}

// TestSQLiteIndexer_IndexExists与元信息 建表后 IndexExists/GetIndexInfo 生效。
func TestSQLiteIndexer_IndexExists与元信息(t *testing.T) {
	idx := newTestSQLiteIndexer(t)
	ctx := context.Background()
	name := "kb_it-sqlite-ix_chunks"
	ok, err := idx.IndexExists(ctx, name)
	if err != nil {
		t.Fatal(err)
	}
	// 构造期即建表（与 chroma GetOrCreate 构造期真建语义一致，30.1 拍板）
	if !ok {
		t.Fatal("构造期 IndexExists 应 true")
	}
	_, err = idx.BuildIndex(ctx, []common.TextChunk{
		{ID: "c1", Text: "x", DocID: "d1", Metadata: map[string]any{"chunk_id": "c1"}},
	}, common.IndexConfig{IndexName: name, IndexType: "vector"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	ok, err = idx.IndexExists(ctx, name)
	if err != nil || !ok {
		t.Fatalf("建表后 IndexExists 应 true: %v %v", ok, err)
	}
	info, err := idx.GetIndexInfo(ctx, name)
	if err != nil {
		t.Fatal(err)
	}
	if info["provider"] != "sqlite" || info["database_name"] != "kb_it-sqlite-ix" {
		t.Fatalf("元信息不符: %v", info)
	}
	// UpdateIndex = 先删后建
	upd, err := idx.UpdateIndex(ctx, []common.TextChunk{
		{ID: "c1b", Text: "doc one rebuilt", DocID: "d1", Metadata: map[string]any{"chunk_id": "c1b"}},
	}, "d1", common.IndexConfig{IndexName: name, IndexType: "vector"}, nil)
	if err != nil || !upd {
		t.Fatalf("UpdateIndex 失败: %v %v", upd, err)
	}
	ids, _ := idx.Store().IDsByDoc(ctx, "d1")
	if len(ids) != 1 || ids[0] != "c1b" {
		t.Fatalf("UpdateIndex 应先删后建: ids=%v", ids)
	}
}

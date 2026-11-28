package kbadmin

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/retrieval/indexing/indexer"
)

// newTestService 构造真实链 Service（真实 SQLite + 真实 retrieval Chroma
// 本地持久化向量库，与 13.27 simple_kb 真链同款组件，禁替身）
func newTestService(t *testing.T) *KBService {
	t.Helper()
	st := newTestStore(t)
	svc, err := NewKBService(st, NewRetrievalKBFactory(t.TempDir()))
	require.NoError(t, err)
	return svc
}

// TestKBService_Create_真实编排 测试 create 写 DAO 并经 retrieval 建 collection
func TestKBService_Create_真实编排(t *testing.T) {
	svc := newTestService(t)
	kb, err := svc.Create(context.Background(), "kb1", "测试库", "document", "{}", "/")
	require.NoError(t, err)
	require.Equal(t, "kb1", kb.ID)

	// DAO 侧已落
	got, err := svc.store.KB().Get("kb1")
	require.NoError(t, err)
	require.Equal(t, "测试库", got.Name)

	// 向量库侧 collection 已建（工厂登记在册，本地 chroma 持久目录存在）
	require.True(t, svc.kbFactory.Has("kb1"))
}

// TestKBService_Create_向量库失败回滚 测试 collection 创建失败时 DAO 行回滚
func TestKBService_Create_向量库失败回滚(t *testing.T) {
	st := newTestStore(t)
	// 空数据目录路径 → chroma 构造期真实失败
	svc, err := NewKBService(st, NewRetrievalKBFactory(""))
	require.NoError(t, err)
	_, err = svc.Create(context.Background(), "kb1", "测试库", "document", "{}", "/")
	require.Error(t, err)
	_, err = st.KB().Get("kb1")
	require.ErrorIs(t, err, ErrKBNotFound)
}

// TestKBService_Delete_双向清理 测试 delete 同时清 collection 与 DAO
func TestKBService_Delete_双向清理(t *testing.T) {
	svc := newTestService(t)
	_, err := svc.Create(context.Background(), "kb1", "测试库", "document", "{}", "/")
	require.NoError(t, err)
	require.NoError(t, svc.Delete(context.Background(), "kb1"))
	_, err = svc.store.KB().Get("kb1")
	require.ErrorIs(t, err, ErrKBNotFound)
	require.False(t, svc.kbFactory.Has("kb1"))
}

// TestKBService_UpdateAndList 测试 update/list 纯 DAO
func TestKBService_UpdateAndList(t *testing.T) {
	svc := newTestService(t)
	_, err := svc.Create(context.Background(), "kb1", "测试库", "document", "{}", "/")
	require.NoError(t, err)
	kb, err := svc.Get("kb1")
	require.NoError(t, err)
	kb.Pinned = true
	updated, err := svc.Update(kb)
	require.NoError(t, err)
	require.True(t, updated.Pinned)
	list, err := svc.List()
	require.NoError(t, err)
	require.Len(t, list, 1)
}

// TestKBService_GetNotFound 测试不存在返回 ErrKBNotFound
func TestKBService_GetNotFound(t *testing.T) {
	svc := newTestService(t)
	_, err := svc.Get("nope")
	require.ErrorIs(t, err, ErrKBNotFound)
}

// TestKBService_Create_graph类型 graph 库同源建 collection（triple 索引在解析期建）
func TestKBService_Create_graph类型(t *testing.T) {
	svc := newTestService(t)
	kb, err := svc.Create(context.Background(), "kg-1", "图库", "graph", "{}", "/")
	require.NoError(t, err)
	require.Equal(t, "graph", kb.Type)
	require.True(t, svc.kbFactory.Has("kg-1"))
}

// TestKBService_Create_未知类型拒单 30.2 只收 document/graph/multi
func TestKBService_Create_未知类型拒单(t *testing.T) {
	svc := newTestService(t)
	_, err := svc.Create(context.Background(), "x", "n", "faq", "{}", "/")
	require.Error(t, err)
	_, err = svc.store.KB().Get("x")
	require.ErrorIs(t, err, ErrKBNotFound)
}

// TestKBService_Create_MultiKB聚合配置 multi 只落库不建 collection，config 须可解析
func TestKBService_Create_MultiKB聚合配置(t *testing.T) {
	svc := newTestService(t)
	_, err := svc.Create(context.Background(), "multi-1", "聚合", "multi", `{"sub_kb_ids":["sub-1"]}`, "/")
	require.NoError(t, err)
	require.False(t, svc.kbFactory.Has("multi-1"))
	// 坏 JSON 拒单
	_, err = svc.Create(context.Background(), "multi-2", "聚合", "multi", "{bad", "/")
	require.Error(t, err)
}

// TestKBService_Delete_MultiKB级联引用检查 被聚合引用的子库不得直接删除
func TestKBService_Delete_MultiKB级联引用检查(t *testing.T) {
	svc := newTestService(t)
	_, err := svc.Create(context.Background(), "sub-1", "子库", "document", "{}", "/")
	require.NoError(t, err)
	_, err = svc.Create(context.Background(), "multi-1", "聚合", "multi", `{"sub_kb_ids":["sub-1"]}`, "/")
	require.NoError(t, err)
	// 引用中 → 拒删
	require.Error(t, svc.Delete(context.Background(), "sub-1"))
	// 聚合库可删
	require.NoError(t, svc.Delete(context.Background(), "multi-1"))
	// 引用解除后子库可删
	require.NoError(t, svc.Delete(context.Background(), "sub-1"))
}

// TestCloseAll_缓存逐出后可重建 域停机清理链回归：CloseAll 后同 kbID 可重新
// CreateIndexer（新 client），且旧实例已 Close
func TestCloseAll_缓存逐出后可重建(t *testing.T) {
	dir := t.TempDir()
	f := NewRetrievalKBFactory(dir)
	if err := f.CreateCollection(context.Background(), "kb-ca", "{}"); err != nil {
		t.Fatal(err)
	}
	idx1, err := f.CreateIndexer(context.Background(), "kb-ca")
	if err != nil {
		t.Fatal(err)
	}
	f.CloseAll()
	f.mu.Lock()
	n := len(f.indexers)
	f.mu.Unlock()
	if n != 0 {
		t.Fatalf("CloseAll 后缓存应空, got %d", n)
	}
	idx2, err := f.CreateIndexer(context.Background(), "kb-ca")
	if err != nil {
		t.Fatal(err)
	}
	if idx1 == idx2 {
		t.Fatal("CloseAll 后应构造新实例")
	}
	_ = idx2.Close()
}

// TestCreateCollection_sqliteProvider sqlite provider 分派（spec §9）：
// collection 登记 + CreateIndexer 返回 SQLiteIndexer（真构造，vec 缺失时该分支
// 由 SQLiteVectorStore 构造期报错——用 unknown provider 分支兜覆盖）。
func TestCreateCollection_sqliteProvider(t *testing.T) {
	dir := t.TempDir()
	f := NewRetrievalKBFactory(dir)
	if err := f.CreateCollection(context.Background(), "kb-sq",
		`{"store":{"provider":"sqlite"}}`); err != nil {
		t.Fatal(err)
	}
	idx, err := f.CreateIndexer(context.Background(), "kb-sq")
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := idx.(interface {
		GetIndexInfo(context.Context, string) (map[string]any, error)
	}); !ok {
		t.Fatal("indexer 应具备 GetIndexInfo 契约")
	}
	info, err := idx.GetIndexInfo(context.Background(), "kb_sq_chunks")
	if err != nil {
		t.Fatal(err)
	}
	if info["provider"] != "sqlite" {
		t.Fatalf("provider 应 sqlite: %v", info["provider"])
	}
	_ = idx.Close()
}

// TestCreateCollection_未知Provider拒单 未支持 provider 建库拒单（DAO 回滚）。
func TestCreateCollection_未知Provider拒单(t *testing.T) {
	f := NewRetrievalKBFactory(t.TempDir())
	if err := f.CreateCollection(context.Background(), "kb-bad",
		`{"store":{"provider":"pgvector"}}`); err == nil {
		t.Fatal("pgvector 未实现入库面，应拒单")
	}
	if f.Has("kb-bad") {
		t.Fatal("拒单后不得登记 store")
	}
}

// TestCreateIndexer_缺省回退chroma 未登记 config 时 provider 缺省回退 chroma
// （域重建后直接 reparse 场景）。
func TestCreateIndexer_缺省回退chroma(t *testing.T) {
	f := NewRetrievalKBFactory(t.TempDir())
	idx, err := f.CreateIndexer(context.Background(), "kb-fallback")
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := idx.(*indexer.ChromaIndexer); !ok {
		t.Fatalf("缺省应回退 ChromaIndexer, got %T", idx)
	}
}

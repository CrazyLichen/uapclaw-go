package kbadmin

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
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

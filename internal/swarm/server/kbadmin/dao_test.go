package kbadmin

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// newTestStore 每用例独立文件库（CGO sqlite 内存库跨连接不共享，用临时文件）
func newTestStore(t *testing.T) *Store {
	t.Helper()
	st, err := OpenStore(t.TempDir() + "/test.db")
	require.NoError(t, err)
	t.Cleanup(func() { _ = st.Close() })
	return st
}

// TestKBDAO_CRUD 测试 kb_info 增删改查
func TestKBDAO_CRUD(t *testing.T) {
	st := newTestStore(t)
	dao := NewKBDAO(st.DB())
	kb := &KBInfo{ID: "kb1", Name: "测试库", Type: "document", CreatedAt: 1, UpdatedAt: 1}
	require.NoError(t, dao.Create(kb))
	got, err := dao.Get("kb1")
	require.NoError(t, err)
	require.Equal(t, "测试库", got.Name)
	got.Name = "改名"
	require.NoError(t, dao.Update(got))
	got2, _ := dao.Get("kb1")
	require.Equal(t, "改名", got2.Name)
	list, err := dao.List()
	require.NoError(t, err)
	require.Len(t, list, 1)
	require.NoError(t, dao.Delete("kb1"))
	_, err = dao.Get("kb1")
	require.ErrorIs(t, err, ErrKBNotFound)
}

// TestKBDAO_List_置顶排序 测试置顶优先、更新时间倒序
func TestKBDAO_List_置顶排序(t *testing.T) {
	st := newTestStore(t)
	dao := NewKBDAO(st.DB())
	require.NoError(t, dao.Create(&KBInfo{ID: "a", Name: "a", CreatedAt: 1, UpdatedAt: 10}))
	require.NoError(t, dao.Create(&KBInfo{ID: "b", Name: "b", Pinned: true, CreatedAt: 1, UpdatedAt: 1}))
	require.NoError(t, dao.Create(&KBInfo{ID: "c", Name: "c", CreatedAt: 1, UpdatedAt: 20}))
	list, err := dao.List()
	require.NoError(t, err)
	require.Equal(t, []string{"b", "c", "a"}, []string{list[0].ID, list[1].ID, list[2].ID})
}

// TestDocDAO_CRUD 测试 kb_doc 增删改查与按状态/库过滤
func TestDocDAO_CRUD(t *testing.T) {
	st := newTestStore(t)
	dao := NewDocDAO(st.DB())
	d1 := &KBDoc{ID: "d1", KBID: "kb1", FileName: "a.txt", ParseStatus: "pending", CreatedAt: 1, UpdatedAt: 1}
	d2 := &KBDoc{ID: "d2", KBID: "kb1", FileName: "b.txt", ParseStatus: "completed", CreatedAt: 2, UpdatedAt: 2}
	require.NoError(t, dao.Create(d1))
	require.NoError(t, dao.Create(d2))
	got, err := dao.Get("d1")
	require.NoError(t, err)
	require.Equal(t, "a.txt", got.FileName)
	byStatus, err := dao.ListByKBAndStatus("kb1", "pending")
	require.NoError(t, err)
	require.Len(t, byStatus, 1)
	all, err := dao.ListByKB("kb1")
	require.NoError(t, err)
	require.Len(t, all, 2)
	d1.ParseStatus = "processing"
	require.NoError(t, dao.Update(d1))
	require.NoError(t, dao.Delete("d2"))
	_, err = dao.Get("d2")
	require.ErrorIs(t, err, ErrDocNotFound)
}

// TestChunkDAO_CRUD_Revisions 测试分块元数据 CRUD 与版本历史
func TestChunkDAO_CRUD_Revisions(t *testing.T) {
	st := newTestStore(t)
	dao := NewChunkDAO(st.DB())
	c := &KBChunkMeta{ID: "c1", DocID: "d1", KBID: "kb1", Revision: 1, Enabled: true}
	require.NoError(t, dao.CreateMeta(c))
	got, err := dao.GetMeta("c1")
	require.NoError(t, err)
	require.True(t, got.Enabled)
	got.Revision = 2
	require.NoError(t, dao.UpdateMeta(got))
	list, err := dao.ListByDoc("d1")
	require.NoError(t, err)
	require.Len(t, list, 1)
	rev := &KBChunkRevision{ID: "r1", ChunkID: "c1", Revision: 1, ContentJSON: "{}", CreatedAt: 1}
	require.NoError(t, dao.CreateRevision(rev))
	revs, err := dao.ListRevisions("c1")
	require.NoError(t, err)
	require.Len(t, revs, 1)
	require.NoError(t, dao.DeleteMeta("c1"))
	_, err = dao.GetMeta("c1")
	require.ErrorIs(t, err, ErrChunkNotFound)
}

// TestTaskDAO_CRUD 测试任务表 CRUD 与按状态扫描
func TestTaskDAO_CRUD(t *testing.T) {
	st := newTestStore(t)
	dao := NewTaskDAO(st.DB())
	task := &KBTask{ID: "t1", DocID: "d1", KBID: "kb1", Status: "pending", CreatedAt: 1}
	require.NoError(t, dao.Create(task))
	got, err := dao.Get("t1")
	require.NoError(t, err)
	require.Equal(t, "pending", got.Status)
	got.Status = "processing"
	require.NoError(t, dao.Update(got))
	processing, err := dao.ListByStatus("processing")
	require.NoError(t, err)
	require.Len(t, processing, 1)
	_, err = dao.Get("t2")
	require.ErrorIs(t, err, ErrTaskNotFound)
}

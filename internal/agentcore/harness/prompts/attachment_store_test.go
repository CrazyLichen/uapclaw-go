package prompts

import (
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// TestSafeIDPart_边界 验证 id 归一化。
//
// Python: _safe_id_part
func TestSafeIDPart_边界(t *testing.T) {
	require.Equal(t, "session_1", safeIDPart("session 1", "x"))
	require.Len(t, safeIDPart(strings.Repeat("a", 200), "x"), 80)
	require.Len(t, safeIDPart("中文", "x"), 12, "全非法字符时回落 sha256[:12]")
	require.Equal(t, "fallback", safeIDPart("   ", "fallback"))
}

// TestManager_AddSection_ID与归一化 验证 id 规则与写入归一化。
func TestManager_AddSection_ID与归一化(t *testing.T) {
	m := NewManager()
	a, err := m.AddSection("s1", "mode_instructions", "内容", AttachmentRuntime, "src.a")
	require.NoError(t, err)
	require.Equal(t, "session.s1.mode_instructions", a.ID)
	require.Equal(t, DefaultAttachmentPriority, a.Priority)
	require.Equal(t, DefaultAttachmentContentKind, a.ContentKind)
	require.Equal(t, "mode_instructions", a.Metadata["section"])
	require.Equal(t, "src.a", a.Metadata["source"])
	require.NotEmpty(t, a.CreatedAt)
	require.NotEmpty(t, a.UpdatedAt)
	require.Equal(t, sha256Hex(a.Content), a.ContentSHA256)

	_, err = m.AddSection("", "x", "c", AttachmentRuntime, "s")
	require.Error(t, err, "缺 session 必须报错")
	_, err = m.AddSection("s1", "", "c", AttachmentRuntime, "s")
	require.Error(t, err, "缺 section 必须报错")
}

// TestManager_稳定排序与覆盖 验证 (priority, source, section) 排序与同名覆盖。
func TestManager_稳定排序与覆盖(t *testing.T) {
	m := NewManager()
	_, _ = m.AddSection("s1", "b", "B", AttachmentRuntime, "src")
	_, _ = m.AddSection("s1", "a", "A", AttachmentRuntime, "src")
	_, _ = m.AddSection("s1", "c", "C", AttachmentRuntime, "src", WithAttachmentPriority(1))
	items, err := m.Collect("s1")
	require.NoError(t, err)
	require.Equal(t, []string{"c", "a", "b"}, []string{items[0].Section, items[1].Section, items[2].Section})

	_, _ = m.AddSection("s1", "a", "A2", AttachmentRuntime, "src")
	items, _ = m.Collect("s1")
	require.Len(t, items, 3)
	require.Equal(t, "A2", items[1].Content)
	require.Equal(t, "a", items[1].Section, "同名覆盖保留 section")
}

// TestManager_过期剔除 验证惰性过期清理。
func TestManager_过期剔除(t *testing.T) {
	m := NewManager()
	_, _ = m.AddSection("s1", "old", "x", AttachmentRuntime, "src",
		WithAttachmentExpiresAt(time.Now().UTC().Add(-time.Minute).Format(time.RFC3339)))
	_, _ = m.AddSection("s1", "new", "y", AttachmentRuntime, "src")
	items, err := m.Collect("s1")
	require.NoError(t, err)
	require.Len(t, items, 1)
	require.Equal(t, "new", items[0].Section)

	items, _ = m.Collect("s1")
	require.Len(t, items, 1, "过期项应被真正删除")
}

// TestManager_查询与清空 覆盖 GetByID/ListByFilter/ClearSection/ClearSession/ClearAll。
func TestManager_查询与清空(t *testing.T) {
	m := NewManager()
	a, _ := m.AddSection("s1", "a", "A", AttachmentRuntime, "src")
	_, _ = m.AddSection("s1", "b", "B", AttachmentText, "src")
	_, _ = m.AddSection("s2", "c", "C", AttachmentRuntime, "other")

	got, err := m.GetByID(a.ID, "")
	require.NoError(t, err)
	require.NotNil(t, got)
	require.Equal(t, "a", got.Section)
	missing, err := m.GetByID("session.s1.zzz", "")
	require.NoError(t, err)
	require.Nil(t, missing)

	byKind, err := m.ListByFilter(AttachmentFilter{SessionID: "s1", Kind: AttachmentText})
	require.NoError(t, err)
	require.Len(t, byKind, 1)
	require.Equal(t, "b", byKind[0].Section)

	n, err := m.ClearSection("s1", "a")
	require.NoError(t, err)
	require.Equal(t, 1, n)

	n, err = m.ClearSession("s1")
	require.NoError(t, err)
	require.Equal(t, 1, n)

	n, err = m.ClearAll()
	require.NoError(t, err)
	require.Equal(t, 1, n)
	items, _ := m.Collect("s2")
	require.Empty(t, items)
}

// TestManager_并发安全 并发读写（配合 -race）。
func TestManager_并发安全(t *testing.T) {
	m := NewManager()
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(3)
		go func() { defer wg.Done(); _, _ = m.AddSection("s1", "sec", "v", AttachmentRuntime, "src") }()
		go func() { defer wg.Done(); _, _ = m.Collect("s1") }()
		go func() { defer wg.Done(); _, _ = m.ListByFilter(AttachmentFilter{Source: "src"}) }()
	}
	wg.Wait()
}

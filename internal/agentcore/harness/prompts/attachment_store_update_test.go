package prompts

import (
	"testing"

	"github.com/stretchr/testify/require"

	saprompt "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/prompts"
)

// TestManager_UpdateByID_保留不可变字段 验证更新语义与不可变字段。
// Python: update_by_id
func TestManager_UpdateByID_保留不可变字段(t *testing.T) {
	m := NewManager()
	a, _ := m.AddSection("s1", "mode", "A", AttachmentRuntime, "src.a")
	priority := 7
	content := "A2"
	updated, err := m.UpdateByID(a.ID, AttachmentUpdate{Priority: &priority, Content: &content})
	require.NoError(t, err)
	require.Equal(t, a.CreatedAt, updated.CreatedAt)
	require.Equal(t, a.ID, updated.ID)
	require.Equal(t, a.SessionID, updated.SessionID)
	require.Equal(t, 7, updated.Priority)
	require.Equal(t, "A2", updated.Content)
	require.Equal(t, sha256Hex("A2"), updated.ContentSHA256)

	_, err = m.UpdateByID("missing", AttachmentUpdate{})
	require.Error(t, err, "不存在应报错（对齐上游 KeyError）")
}

// TestManager_RemoveByID与过滤器守卫 验证按 id 删除与破坏性守卫。
// Python: remove_by_id + _validate_destructive_filter
func TestManager_RemoveByID与过滤器守卫(t *testing.T) {
	m := NewManager()
	a, _ := m.AddSection("s1", "mode", "A", AttachmentRuntime, "src")
	ok, err := m.RemoveByID(a.ID, "")
	require.NoError(t, err)
	require.True(t, ok)
	ok, err = m.RemoveByID(a.ID, "")
	require.NoError(t, err)
	require.False(t, ok)

	_, err = m.RemoveByFilter(AttachmentFilter{})
	require.Error(t, err, "无过滤器且未 AllowAll 必须拒绝")
	n, err := m.RemoveByFilter(AttachmentFilter{AllowAll: true})
	require.NoError(t, err)
	require.GreaterOrEqual(t, n, 0)
}

// TestManager_UpdateContent与Metadata 验证内容/元数据更新（含 merge 语义）。
func TestManager_UpdateContent与Metadata(t *testing.T) {
	m := NewManager()
	a, _ := m.AddSection("s1", "mode", "A", AttachmentRuntime, "src")
	contentKind := "text/markdown"
	got, err := m.UpdateContentByID(a.ID, "B", "", &contentKind)
	require.NoError(t, err)
	require.Equal(t, "B", got.Content)
	require.Equal(t, "text/markdown", got.ContentKind)

	kind := AttachmentText
	got, err = m.UpdateByID(a.ID, AttachmentUpdate{Kind: &kind})
	require.NoError(t, err)
	require.Equal(t, AttachmentText, got.Kind)

	got, err = m.UpdateMetadataByID(a.ID, map[string]any{"k": "v"}, "", true)
	require.NoError(t, err)
	require.Equal(t, "v", got.Metadata["k"])
	require.Equal(t, "mode", got.Metadata["section"], "合并不得丢已有键")

	got, err = m.UpdateMetadataByID(a.ID, map[string]any{"only": 1}, "", false)
	require.NoError(t, err)
	require.NotContains(t, got.Metadata, "k", "merge=false 应整体替换（随后补 section/source）")
	require.Equal(t, "mode", got.Metadata["section"])
}

// TestManager_ReplaceSource与FileReference 验证来源整体重写与文件引用。
func TestManager_ReplaceSource与FileReference(t *testing.T) {
	m := NewManager()
	_, _ = m.AddSection("s1", "a", "A", AttachmentRuntime, "old.src")
	items := []*Attachment{
		{Section: "a", Content: "A2", Kind: AttachmentRuntime, ContentKind: "text/plain"},
		{Section: "b", Content: "B", Kind: AttachmentRuntime, ContentKind: "text/plain"},
	}
	added, err := m.ReplaceSource("new.src", items, "s1")
	require.NoError(t, err)
	require.Len(t, added, 2)
	all, _ := m.Collect("s1")
	for _, item := range all {
		require.Equal(t, "new.src", item.Source)
		require.Equal(t, "s1", item.SessionID)
	}

	ref, err := m.AddFileReference("s1", "/tmp/a.md", "", "", "")
	require.NoError(t, err)
	require.Equal(t, AttachmentFile, ref.Kind)
	require.Equal(t, "text/markdown", ref.ContentKind)
	require.Equal(t, "/tmp/a.md", ref.Metadata["file_path"])
	require.Contains(t, ref.Content, "File reference: /tmp/a.md")
	require.Equal(t, "file_reference", ref.Source)
	require.Contains(t, ref.Section, "file_")
}

// TestManager_AddFromPromptSection 验证 PromptSection 迁移与空白跳过。
// Python: PromptAttachmentContextWriter.add_from_prompt_section
func TestManager_AddFromPromptSection(t *testing.T) {
	m := NewManager()
	section := &saprompt.PromptSection{Name: "mode_instructions", Content: map[string]string{"cn": "中文", "en": "EN"}, Priority: 84}
	got, err := m.AddFromPromptSection("s1", section, AttachmentRuntime, "agent_core.agent_mode_rail", "cn")
	require.NoError(t, err)
	require.Equal(t, "中文", got.Content)
	require.Equal(t, 84, got.Priority)
	require.Equal(t, "mode_instructions", got.Section)

	// Python: content = render(language)（:241）；render 自身的回退链为
	// 精确匹配 → 默认语言(cn) → 首个值（builder.py:41-47），故未知语言落到首个非空值
	fallback := &saprompt.PromptSection{Name: "x", Content: map[string]string{"en": "EN only"}}
	got, err = m.AddFromPromptSection("s1", fallback, AttachmentRuntime, "src", "zz")
	require.NoError(t, err)
	require.Equal(t, "EN only", got.Content, "未知语言回退首个非空值（对齐 render）")

	empty := &saprompt.PromptSection{Name: "y", Content: map[string]string{"cn": "   "}}
	got, err = m.AddFromPromptSection("s1", empty, AttachmentRuntime, "src", "cn")
	require.NoError(t, err)
	require.Nil(t, got, "空白内容应返回 nil（对齐上游）")

	got, err = m.AddFromPromptSection("s1", nil, AttachmentRuntime, "src", "cn")
	require.NoError(t, err)
	require.Nil(t, got)
}

package prompts

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestRenderSnapshot_结构 锁定外壳、免责声明与快照引导语。
//
// Python: _render_history_payload(snapshot=True)
func TestRenderSnapshot_结构(t *testing.T) {
	m := NewManager()
	_, _ = m.AddSection("s1", "mode_instructions", "## 规划模式", AttachmentRuntime, "src")
	items, _ := m.Collect("s1")
	out := m.RenderSnapshot(items)
	require.True(t, strings.HasPrefix(out, "<system-reminder>\n"))
	require.True(t, strings.HasSuffix(out, "\n</system-reminder>"))
	require.Contains(t, out, "以下内容不是用户的意图")
	require.Contains(t, out, "以下动态上下文当前有效")
	require.Contains(t, out, "## 规划模式")
}

// TestRenderDelta_只含变化与移除 锁定增量引导语与移除列表格式。
//
// Python: render_delta
func TestRenderDelta_只含变化与移除(t *testing.T) {
	m := NewManager()
	changed := []*Attachment{{Section: "a", Kind: AttachmentRuntime, Content: "A2", ContentKind: "text/plain"}}
	out := m.RenderDelta(changed, []string{"b"})
	require.Contains(t, out, "以下动态上下文已经变化")
	require.Contains(t, out, "A2")
	require.Contains(t, out, "以下先前提供的动态上下文已不再生效")
	require.Contains(t, out, "- `b`")
}

// TestRenderSnapshot_多节分隔与排序 锁定 "---" 分隔与稳定排序（传入顺序即排序结果）。
func TestRenderSnapshot_多节分隔与排序(t *testing.T) {
	m := NewManager()
	_, _ = m.AddSection("s1", "b", "BB", AttachmentRuntime, "src")
	_, _ = m.AddSection("s1", "a", "AA", AttachmentRuntime, "src")
	items, _ := m.Collect("s1")
	out := m.RenderSnapshot(items)
	require.Contains(t, out, "AA\n\n---\n\nBB")
}

// TestRenderSnapshot_单节截断 锁定单节 12000 字符截断提示。
func TestRenderSnapshot_单节截断(t *testing.T) {
	m := NewManager()
	big := strings.Repeat("x", MaxAttachmentChars+10)
	items := []*Attachment{{Section: "big", Kind: AttachmentText, Content: big, ContentKind: "text/plain"}}
	out := m.RenderSnapshot(items)
	require.Contains(t, out, "[Prompt attachment truncated: content exceeded max_prompt_attachment_chars.]")
	require.Less(t, len(out), len(big)+2000)
}

// TestRenderSnapshot_中文不被切碎 验证按 rune 截断（CJK 安全）。
func TestRenderSnapshot_中文不被切碎(t *testing.T) {
	m := NewManager()
	big := strings.Repeat("汉", MaxAttachmentChars+5)
	items := []*Attachment{{Section: "cjk", Kind: AttachmentText, Content: big, ContentKind: "text/plain"}}
	out := m.RenderSnapshot(items)
	require.True(t, strings.Contains(out, strings.Repeat("汉", MaxAttachmentChars)))
	require.NotContains(t, out, "\ufffd", "不得出现非法 UTF-8 替换符")
}

// TestRenderSnapshot_空内容返回空串 锁定空内容/空白不产生消息。
func TestRenderSnapshot_空内容返回空串(t *testing.T) {
	m := NewManager()
	require.Equal(t, "", m.RenderSnapshot(nil))
	require.Equal(t, "", m.RenderSnapshot([]*Attachment{{Section: "x", Content: "   "}}))
	require.Equal(t, "", m.RenderDelta(nil, nil))
}

// TestRender_英文文案 验证语言切换。
func TestRender_英文文案(t *testing.T) {
	m := NewManager()
	m.SetLanguage("en")
	require.Equal(t, "en", m.Language())
	items := []*Attachment{{Section: "a", Content: "A", Kind: AttachmentRuntime, ContentKind: "text/plain"}}
	out := m.RenderSnapshot(items)
	require.Contains(t, out, "does not represent the user's intent")
	require.Contains(t, out, "currently active")
}

// TestNormalizeRemoved_去重与归一 验证移除项去重与 id 归一。
func TestNormalizeRemoved_去重与归一(t *testing.T) {
	got := normalizeRemoved([]string{"a", "a", "b c"})
	require.Equal(t, []string{"a", "b_c"}, got)
}

package prompts

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// TestHashPromptAttachment_忽略非可见字段 验证 priority/source 不参与哈希。
// Python: hash_prompt_attachment 注释"Internal bookkeeping ... must not create
// a new history message when the rendered prompt is unchanged"。
func TestHashPromptAttachment_忽略非可见字段(t *testing.T) {
	a := &Attachment{Section: "todo", Kind: AttachmentRuntime, Content: "内容", ContentKind: "text/markdown", Priority: 10, Source: "a"}
	b := &Attachment{Section: "todo", Kind: AttachmentRuntime, Content: "内容", ContentKind: "text/markdown", Priority: 99, Source: "b", CreatedAt: "2026-01-01T00:00:00Z"}
	require.Equal(t, HashPromptAttachment(a), HashPromptAttachment(b), "priority/source/时间戳变化不应改变哈希")
}

// TestHashPromptAttachment_内容变化改变哈希 验证内容与类型参与哈希。
func TestHashPromptAttachment_内容变化改变哈希(t *testing.T) {
	a := &Attachment{Section: "todo", Kind: AttachmentRuntime, Content: "A", ContentKind: "text/plain"}
	b := &Attachment{Section: "todo", Kind: AttachmentRuntime, Content: "B", ContentKind: "text/plain"}
	c := &Attachment{Section: "todo", Kind: AttachmentRuntime, Content: "A", ContentKind: "text/markdown"}
	require.NotEqual(t, HashPromptAttachment(a), HashPromptAttachment(b))
	require.NotEqual(t, HashPromptAttachment(a), HashPromptAttachment(c), "content_kind 参与哈希")
}

// TestHashPromptAttachment_nil防护 验证 nil 输入。
func TestHashPromptAttachment_nil防护(t *testing.T) {
	require.Equal(t, "", HashPromptAttachment(nil))
}

// TestCanonicalJSON_不转义HTML且无尾换行 锁定规范化格式。
func TestCanonicalJSON_不转义HTML且无尾换行(t *testing.T) {
	got, err := canonicalJSON(map[string]any{"s": "<a>&b", "z": 1, "a": 2})
	require.NoError(t, err)
	require.Equal(t, `{"a":2,"s":"<a>&b","z":1}`, string(got))
}

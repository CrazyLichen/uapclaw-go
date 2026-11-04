package prompts

import (
	"testing"

	"github.com/stretchr/testify/require"

	saprompt "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/prompts"
)

// fakeRailProvider 提供固定管理器的 RailAttachmentProvider 实现。
type fakeRailProvider struct {
	// m 被返回的管理器（可为 nil）
	m *Manager
}

// PromptAttachments 返回管理器。
func (f *fakeRailProvider) PromptAttachments() *Manager { return f.m }

// TestManagerFor_非Provider返回nil 验证类型断言失败时安全返回。
func TestManagerFor_非Provider返回nil(t *testing.T) {
	require.Nil(t, ManagerFor(nil))
	require.Nil(t, ManagerFor(struct{}{}))
}

// TestDeliver_无管理器返回false 验证回落语义（调用方 AddSection）。
func TestDeliver_无管理器返回false(t *testing.T) {
	section := &saprompt.PromptSection{Name: "mode", Content: map[string]string{"cn": "内容"}}
	require.False(t, Deliver(&fakeRailProvider{}, "s1", section, AttachmentRuntime, "test.rail", "cn"))
	require.False(t, Deliver(&fakeRailProvider{m: NewManager()}, "", section, AttachmentRuntime, "test.rail", "cn"))
	require.False(t, Deliver(&fakeRailProvider{m: NewManager()}, "s1", nil, AttachmentRuntime, "test.rail", "cn"))
}

// TestDeliver_有管理器写入附件 验证 section 内容与优先级落到管理器。
func TestDeliver_有管理器写入附件(t *testing.T) {
	m := NewManager()
	provider := &fakeRailProvider{m: m}
	section := &saprompt.PromptSection{
		Name:     "coding",
		Content:  map[string]string{"cn": "行为指令"},
		Priority: 85,
	}
	require.True(t, Deliver(provider, "s1", section, AttachmentMemory, "agent_core.coding_memory_rail", "cn"))

	items, err := m.ListByFilter(AttachmentFilter{SessionID: "s1"})
	require.NoError(t, err)
	require.Len(t, items, 1)
	require.Equal(t, "coding", items[0].Section)
	require.Equal(t, AttachmentMemory, items[0].Kind)
	require.Equal(t, 85, items[0].Priority)
	require.Equal(t, "text/markdown", items[0].ContentKind)
}

// TestDeliverText_覆盖写与清除 验证同名 section 覆盖 + 清除。
func TestDeliverText_覆盖写与清除(t *testing.T) {
	m := NewManager()
	provider := &fakeRailProvider{m: m}

	require.True(t, DeliverText(provider, "s1", "coding_memory_context", "第一版",
		AttachmentMemory, "agent_core.coding_memory_rail", 85))
	items, err := m.ListByFilter(AttachmentFilter{SessionID: "s1"})
	require.NoError(t, err)
	require.Len(t, items, 1)

	require.True(t, DeliverText(provider, "s1", "coding_memory_context", "第二版",
		AttachmentMemory, "agent_core.coding_memory_rail", 85))
	items, err = m.ListByFilter(AttachmentFilter{SessionID: "s1"})
	require.NoError(t, err)
	require.Len(t, items, 1, "同名 section 应覆盖而非追加")
	require.Contains(t, items[0].Content, "第二版")

	require.True(t, ClearDeliveredSection(provider, "s1", "coding_memory_context"))
	items, err = m.ListByFilter(AttachmentFilter{SessionID: "s1"})
	require.NoError(t, err)
	require.Empty(t, items)
}

// TestDeliverText_入参不完整返回false 验证空会话/空内容/无管理器。
func TestDeliverText_入参不完整返回false(t *testing.T) {
	provider := &fakeRailProvider{m: NewManager()}
	require.False(t, DeliverText(provider, "", "sec", "内容", AttachmentMemory, "src", 0))
	require.False(t, DeliverText(provider, "s1", "", "内容", AttachmentMemory, "src", 0))
	require.False(t, DeliverText(provider, "s1", "sec", "", AttachmentMemory, "src", 0))
	require.False(t, DeliverText(&fakeRailProvider{}, "s1", "sec", "内容", AttachmentMemory, "src", 0))
	require.False(t, ClearDeliveredSection(&fakeRailProvider{}, "s1", "sec"))
	require.False(t, ClearDeliveredSection(provider, "", "sec"))
}

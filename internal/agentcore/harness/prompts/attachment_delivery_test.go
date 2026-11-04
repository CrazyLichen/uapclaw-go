package prompts

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	ceiface "github.com/uapclaw/uapclaw-go/internal/agentcore/context_engine/interface"
	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
)

// fakeHistory 极简 HistoryContext 实现（3 方法）。
type fakeHistory struct {
	messages []llmschema.BaseMessage
	session  string
}

func (f *fakeHistory) SessionID() string {
	if f.session == "" {
		return "s1"
	}
	return f.session
}

func (f *fakeHistory) GetMessages(_ int, _ bool) ([]llmschema.BaseMessage, error) {
	return f.messages, nil
}

func (f *fakeHistory) AddMessages(_ context.Context, msgs []llmschema.BaseMessage, _ ...ceiface.Option) ([]llmschema.BaseMessage, error) {
	f.messages = append(f.messages, msgs...)
	return msgs, nil
}

// TestSync_首次快照之后增量 全链路：快照 → 无变化 → 增量 → 移除。
//
// Python: sync_to_context（snapshot → delta → removed）
func TestSync_首次快照之后增量(t *testing.T) {
	m := NewManager()
	h := &fakeHistory{}
	_, _ = m.AddSection("s1", "mode", "A", AttachmentRuntime, "src")
	require.NoError(t, m.Sync(context.Background(), h))
	require.Len(t, h.messages, 1)
	require.Equal(t, ModeSnapshot, h.messages[0].GetMetadata()[MetadataKeyMode])
	require.Equal(t, "s1", h.messages[0].GetMetadata()[MetadataKeySessionID])

	require.NoError(t, m.Sync(context.Background(), h))
	require.Len(t, h.messages, 1, "内容未变不应新增消息")

	_, _ = m.AddSection("s1", "mode", "B", AttachmentRuntime, "src")
	require.NoError(t, m.Sync(context.Background(), h))
	require.Len(t, h.messages, 2)
	require.Equal(t, ModeDelta, h.messages[1].GetMetadata()[MetadataKeyMode])
	require.Contains(t, h.messages[1].GetContent().Text(), "B")
	require.NotContains(t, h.messages[1].GetContent().Text(), "A", "增量只含变更节内容")

	_, _ = m.ClearSection("s1", "mode")
	require.NoError(t, m.Sync(context.Background(), h))
	require.Len(t, h.messages, 3)
	require.Contains(t, h.messages[2].GetContent().Text(), "- `mode`")
}

// TestSync_新管理器不重复投递 模拟进程重启：账本从历史恢复。
func TestSync_新管理器不重复投递(t *testing.T) {
	h := &fakeHistory{}
	m1 := NewManager()
	_, _ = m1.AddSection("s1", "mode", "A", AttachmentRuntime, "src")
	require.NoError(t, m1.Sync(context.Background(), h))

	m2 := NewManager()
	_, _ = m2.AddSection("s1", "mode", "A", AttachmentRuntime, "src")
	require.NoError(t, m2.Sync(context.Background(), h))
	require.Len(t, h.messages, 1, "重建后同内容不应重复投递")
}

// TestSync_基线丢失回退快照 模拟压缩把 snapshot 挤出窗口。
func TestSync_基线丢失回退快照(t *testing.T) {
	h := &fakeHistory{}
	m := NewManager()
	_, _ = m.AddSection("s1", "mode", "A", AttachmentRuntime, "src")
	require.NoError(t, m.Sync(context.Background(), h))
	h.messages = nil
	_, _ = m.AddSection("s1", "mode", "B", AttachmentRuntime, "src")
	require.NoError(t, m.Sync(context.Background(), h))
	require.Len(t, h.messages, 1)
	require.Equal(t, ModeSnapshot, h.messages[0].GetMetadata()[MetadataKeyMode])
	require.Contains(t, h.messages[0].GetContent().Text(), "B")
}

// TestSync_JSON反序列化后的state可读 验证 map[string]any 形态（重启后从磁盘恢复）。
func TestSync_JSON反序列化后的state可读(t *testing.T) {
	m := NewManager()
	_, _ = m.AddSection("s1", "mode", "A", AttachmentRuntime, "src")
	current := stateBySection(mustCollect(t, m, "s1"))

	h := &fakeHistory{messages: []llmschema.BaseMessage{
		llmschema.NewUserMessage("<system-reminder>old</system-reminder>", llmschema.WithMetadata(map[string]any{
			MetadataKeyAttachmentHistory: true,
			MetadataKeyMode:              ModeSnapshot,
			MetadataKeySessionID:         "s1",
			MetadataKeyState:             map[string]any{"mode": current["mode"]},
		})),
	}}
	require.NoError(t, m.Sync(context.Background(), h))
	require.Len(t, h.messages, 1, "历史 state 与本轮一致时不得重复投递")
}

// TestSync_其他会话消息不参与账本 验证 session 过滤。
func TestSync_其他会话消息不参与账本(t *testing.T) {
	m := NewManager()
	_, _ = m.AddSection("s1", "mode", "A", AttachmentRuntime, "src")
	h := &fakeHistory{messages: []llmschema.BaseMessage{
		llmschema.NewUserMessage("<system-reminder>other</system-reminder>", llmschema.WithMetadata(map[string]any{
			MetadataKeyAttachmentHistory: true,
			MetadataKeyMode:              ModeSnapshot,
			MetadataKeySessionID:         "s2",
			MetadataKeyState:             map[string]string{"mode": "whatever"},
		})),
	}}
	require.NoError(t, m.Sync(context.Background(), h))
	require.Len(t, h.messages, 2, "s2 的账本不得影响 s1 首次快照")
	require.Equal(t, ModeSnapshot, h.messages[1].GetMetadata()[MetadataKeyMode])
}

// TestSync_nil防护 验证 nil 上下文直接返回。
func TestSync_nil防护(t *testing.T) {
	require.NoError(t, NewManager().Sync(context.Background(), nil))
}

// mustCollect 测试辅助：取会话附件。
func mustCollect(t *testing.T, m *Manager, sessionID string) []*Attachment {
	t.Helper()
	items, err := m.Collect(sessionID)
	require.NoError(t, err)
	return items
}

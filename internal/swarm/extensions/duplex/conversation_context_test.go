package duplex

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// fakeContextAgent 会话上下文桩。
type fakeContextAgent struct {
	ctx    DuplexConversationContext
	err    error
	called string
}

func (f *fakeContextAgent) GetContext(_ context.Context, sessionID string) (DuplexConversationContext, error) {
	f.called = sessionID
	if f.err != nil {
		return nil, f.err
	}
	return f.ctx, nil
}

// fakeConversation 上下文桩。
type fakeConversation struct {
	messages []map[string]any
	added    []map[string]any
	ids      map[string]bool
	addErr   error
}

func (f *fakeConversation) Messages(_ context.Context) []map[string]any { return f.messages }

func (f *fakeConversation) AddMessages(_ context.Context, messages []map[string]any) error {
	if f.addErr != nil {
		return f.addErr
	}
	f.added = append(f.added, messages...)
	return nil
}

func (f *fakeConversation) DuplexHistoryIDs() map[string]bool { return f.ids }

func (f *fakeConversation) SetDuplexHistoryIDs(seen map[string]bool) { f.ids = seen }

// fakeHistory 历史加载桩。
type fakeHistory struct {
	flushErr error
	loadErr  error
	records  []ConversationRecord
	flushed  bool
}

func (f *fakeHistory) FlushPendingWrites(_ context.Context) error {
	f.flushed = true
	return f.flushErr
}

func (f *fakeHistory) LoadHistoryRecords(_ context.Context, _ string) ([]ConversationRecord, error) {
	return f.records, f.loadErr
}

func TestConversationRecordID_身份判定(t *testing.T) {
	// 非 video_duplex 渠道不导入
	if got := ConversationRecordID(map[string]any{"channel_id": "web", "role": "user"}); got != "" {
		t.Fatalf("非双工渠道应返回空串，得到 %q", got)
	}
	// user 取 id→request_id 链
	if got := ConversationRecordID(map[string]any{"channel_id": "video_duplex", "role": "user", "id": "r1"}); got != "r1" {
		t.Fatalf("user 应取 id，得到 %q", got)
	}
	if got := ConversationRecordID(map[string]any{"channel_id": "video_duplex", "role": "user", "request_id": "q1"}); got != "q1" {
		t.Fatalf("user 缺 id 应回退 request_id，得到 %q", got)
	}
	// assistant 须 chat.final 且非 tool_result
	ok := map[string]any{"channel_id": "video_duplex", "role": "assistant", "event_type": "chat.final", "id": "a1"}
	if got := ConversationRecordID(ok); got != "a1" {
		t.Fatalf("assistant chat.final 应取 id，得到 %q", got)
	}
	tool := map[string]any{"channel_id": "video_duplex", "role": "assistant", "event_type": "chat.final", "presentation": "tool_result", "id": "a2"}
	if got := ConversationRecordID(tool); got != "" {
		t.Fatalf("tool_result 呈现不导入，得到 %q", got)
	}
	other := map[string]any{"channel_id": "video_duplex", "role": "assistant", "event_type": "chat.delta", "id": "a3"}
	if got := ConversationRecordID(other); got != "" {
		t.Fatalf("chat.delta 不导入，得到 %q", got)
	}
}

func TestSyncConversationContext_全链(t *testing.T) {
	ctx := context.Background()

	// agent 缺失即跳过
	if err := SyncConversationContext(ctx, nil, "s", &fakeHistory{}); err != nil {
		t.Fatalf("nil agent 应跳过，得到 %v", err)
	}
	// GetContext 报错透传
	agentErr := &fakeContextAgent{err: errors.New("boom")}
	if err := SyncConversationContext(ctx, agentErr, "s", &fakeHistory{}); err == nil {
		t.Fatal("GetContext 错误应透传")
	}
	if agentErr.called != "s" {
		t.Fatalf("应传 sessionID，得到 %q", agentErr.called)
	}
	// 冷创建跳过（nil context）
	if err := SyncConversationContext(ctx, &fakeContextAgent{}, "s", &fakeHistory{}); err != nil {
		t.Fatalf("nil context 应跳过，得到 %v", err)
	}
	// Flush 失败 → 固定文案
	conv := &fakeConversation{}
	flushFail := &fakeHistory{flushErr: errors.New("busy")}
	err := SyncConversationContext(ctx, &fakeContextAgent{ctx: conv}, "s", flushFail)
	if err == nil || !strings.Contains(err.Error(), "语音对话历史尚未保存") {
		t.Fatalf("Flush 失败应报固定文案，得到 %v", err)
	}

	// 正常导入：新 user 记录追加 + assistant 回执导入 + 空内容跳过 + 已见跳过
	seen := map[string]bool{"old": true}
	conv2 := &fakeConversation{ids: seen}
	hist := &fakeHistory{records: []ConversationRecord{
		{ChannelID: "video_duplex", Role: "user", ID: "u1", Content: "你好"},
		{ChannelID: "video_duplex", Role: "assistant", EventType: "chat.final", ID: "a1", Content: "回答"},
		{ChannelID: "video_duplex", Role: "user", ID: "u0", Content: ""},                                 // 空内容跳过
		{ChannelID: "video_duplex", Role: "user", ID: "old", Content: "已消费"},                             // 已见跳过
		{ChannelID: "video_duplex", Role: "assistant", EventType: "chat.delta", ID: "a9", Content: "片段"}, // 不可导入
	}}
	if err := SyncConversationContext(ctx, &fakeContextAgent{ctx: conv2}, "s", hist); err != nil {
		t.Fatalf("正常导入不应报错，得到 %v", err)
	}
	if !hist.flushed {
		t.Fatal("应先 FlushPendingWrites")
	}
	if len(conv2.added) != 2 {
		t.Fatalf("应追加 2 条消息，得到 %d", len(conv2.added))
	}
	userMsg := conv2.added[0]
	if userMsg["message_type"] != "user" {
		t.Fatalf("user 消息 message_type 应为 user，得到 %v", userMsg["message_type"])
	}
	meta := userMsg["metadata"].(map[string]any)
	if meta[MessageOriginMetadata] != MessageOriginExternalUser || meta[MessageSourceKindMetadata] != MessageSourceKindVideoDuplex {
		t.Fatalf("user 消息应带双工来源元数据，得到 %v", meta)
	}
	if conv2.added[1]["message_type"] != "assistant" {
		t.Fatalf("assistant 消息 message_type 应为 assistant，得到 %v", conv2.added[1]["message_type"])
	}
	if !seen["u1"] || !seen["a1"] {
		t.Fatal("导入后应写回已消费 ID 集合")
	}

	// seen 为空时从现有消息元数据重建
	conv3 := &fakeConversation{messages: []map[string]any{
		{"metadata": map[string]any{HistoryID: "m1"}},
	}}
	hist3 := &fakeHistory{records: []ConversationRecord{
		{ChannelID: "video_duplex", Role: "user", ID: "m1", Content: "重复"},
	}}
	if err := SyncConversationContext(ctx, &fakeContextAgent{ctx: conv3}, "s", hist3); err != nil {
		t.Fatalf("重建 seen 不应报错，得到 %v", err)
	}
	if len(conv3.added) != 0 {
		t.Fatalf("从消息重建 seen 后 m1 应视为已消费，得到 %d 条追加", len(conv3.added))
	}
}

package db

import (
	"context"
	"testing"
	"time"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
)

// fakeMessageStore 用于测试的 BaseMessageStore 模拟实现
type fakeMessageStore struct{}

func (f *fakeMessageStore) AddMessage(_ context.Context, _ *MessageAdd) (string, error) {
	return "msg_test", nil
}

func (f *fakeMessageStore) AddMessages(_ context.Context, adds []*MessageAdd) ([]string, error) {
	ids := make([]string, len(adds))
	for i := range adds {
		ids[i] = "msg_test"
	}
	return ids, nil
}

func (f *fakeMessageStore) GetMessageByID(_ context.Context, _ string) (schema.BaseMessage, *MessageMetadata, error) {
	return nil, nil, nil
}

func (f *fakeMessageStore) GetMessages(_ context.Context, _ *MessageFilter, _ int, _, _ string) ([]*MessageAndMeta, error) {
	return nil, nil
}

func (f *fakeMessageStore) UpdateMessage(_ context.Context, _ string, _ schema.MessageContent) error {
	return nil
}

func (f *fakeMessageStore) DeleteMessageByID(_ context.Context, _ string) error {
	return nil
}

func (f *fakeMessageStore) DeleteMessages(_ context.Context, _ *MessageFilter) (int64, error) {
	return 0, nil
}

func (f *fakeMessageStore) CountMessages(_ context.Context, _ *MessageFilter) (int64, error) {
	return 0, nil
}

func (f *fakeMessageStore) GetSchemaVersion(_ context.Context) (int32, error) {
	return 0, nil
}

func (f *fakeMessageStore) SetSchemaVersion(_ context.Context, _ int32) error {
	return nil
}

// TestBaseMessageStore_接口契约 验证 fakeMessageStore 满足 BaseMessageStore 接口
func TestBaseMessageStore_接口契约(t *testing.T) {
	var _ BaseMessageStore = (*fakeMessageStore)(nil)
}

// TestMessageFilter_字段验证 验证 MessageFilter 字段可正确设置
func TestMessageFilter_字段验证(t *testing.T) {
	now := time.Now()
	sessionID := "session1"
	filter := &MessageFilter{
		UserID:    "user1",
		ScopeID:   "scope1",
		SessionID: &sessionID,
		StartTime: &now,
		EndTime:   &now,
	}
	if filter.UserID != "user1" {
		t.Errorf("UserID = %q, want %q", filter.UserID, "user1")
	}
	if filter.StartTime == nil {
		t.Error("StartTime should not be nil")
	}
}

// TestMessageAdd_字段验证 验证 MessageAdd 字段可正确设置
func TestMessageAdd_字段验证(t *testing.T) {
	msg := schema.NewDefaultMessage(schema.RoleTypeUser, "hello")
	add := &MessageAdd{
		Message:   msg,
		UserID:    "user1",
		ScopeID:   "scope1",
		SessionID: "session1",
		Timestamp: time.Now(),
	}
	if add.Message.GetRole() != schema.RoleTypeUser {
		t.Errorf("Message.Role = %v, want %v", add.Message.GetRole(), schema.RoleTypeUser)
	}
}

// TestMessageMetadata_字段验证 验证 MessageMetadata 字段可正确设置
func TestMessageMetadata_字段验证(t *testing.T) {
	meta := &MessageMetadata{
		MessageID:   "msg_abc123_1700000000000",
		UserID:      "user1",
		ScopeID:     "scope1",
		SessionID:   "session1",
		Timestamp:   time.Now(),
		MessageType: "user",
	}
	if meta.MessageID != "msg_abc123_1700000000000" {
		t.Errorf("MessageID = %q, want %q", meta.MessageID, "msg_abc123_1700000000000")
	}
}

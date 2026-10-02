package migrator

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/store/db"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/memory/migration/operation"
)

// ──────────────────────────── 辅助结构体 ────────────────────────────

// fakeMessageStore 测试用假消息存储
type fakeMessageStore struct {
	// messages 存储的消息
	messages []*db.MessageAndMeta
	// schemaVersion 当前 schema 版本
	schemaVersion int32
	// addMessageCalls AddMessage 调用次数
	addMessageCalls int
}

func newFakeMessageStore() *fakeMessageStore {
	return &fakeMessageStore{}
}

func (s *fakeMessageStore) AddMessage(_ context.Context, msgAdd *db.MessageAdd) (string, error) {
	s.addMessageCalls++
	s.messages = append(s.messages, &db.MessageAndMeta{
		Message: msgAdd.Message,
		Metadata: &db.MessageMetadata{
			UserID:    msgAdd.UserID,
			ScopeID:   msgAdd.ScopeID,
			SessionID: msgAdd.SessionID,
			Timestamp: msgAdd.Timestamp,
		},
	})
	return fmt.Sprintf("msg_%d", s.addMessageCalls), nil
}

func (s *fakeMessageStore) AddMessages(_ context.Context, adds []*db.MessageAdd) ([]string, error) {
	ids := make([]string, len(adds))
	for i, add := range adds {
		id, err := s.AddMessage(context.Background(), add)
		if err != nil {
			return nil, err
		}
		ids[i] = id
	}
	return ids, nil
}

func (s *fakeMessageStore) GetMessages(_ context.Context, _ *db.MessageFilter, limit int, _, _ string) ([]*db.MessageAndMeta, error) {
	if limit > 0 && len(s.messages) > limit {
		return s.messages[:limit], nil
	}
	return s.messages, nil
}

func (s *fakeMessageStore) DeleteMessageByID(_ context.Context, _ string) error {
	return nil
}

func (s *fakeMessageStore) CountMessages(_ context.Context, _ *db.MessageFilter) (int64, error) {
	return int64(len(s.messages)), nil
}

func (s *fakeMessageStore) GetMessageByID(_ context.Context, _ string) (schema.BaseMessage, *db.MessageMetadata, error) {
	return nil, nil, nil
}

func (s *fakeMessageStore) UpdateMessage(_ context.Context, _ string, _ schema.MessageContent) error {
	return nil
}

func (s *fakeMessageStore) DeleteMessages(_ context.Context, _ *db.MessageFilter) (int64, error) {
	count := int64(len(s.messages))
	s.messages = nil
	return count, nil
}

func (s *fakeMessageStore) GetSchemaVersion(_ context.Context) (int32, error) {
	return s.schemaVersion, nil
}

func (s *fakeMessageStore) SetSchemaVersion(_ context.Context, version int32) error {
	s.schemaVersion = version
	return nil
}

// ──────────────────────────── MessageMigrator.TryMigrate 测试 ────────────────────────────

// TestMessageMigrator_TryMigrate_空操作 测试空操作列表直接返回
func TestMessageMigrator_TryMigrate_空操作(t *testing.T) {
	store := newFakeMessageStore()
	m := NewMessageMigrator(store)

	err := m.TryMigrate(context.Background(), MessageEntityKey, nil)
	if err != nil {
		t.Errorf("空操作列表应返回 nil, got %v", err)
	}
}

// TestMessageMigrator_TryMigrate_错误EntityKey 测试错误的 entityKey
func TestMessageMigrator_TryMigrate_错误EntityKey(t *testing.T) {
	store := newFakeMessageStore()
	m := NewMessageMigrator(store)

	ops := []operation.Operation{
		&operation.UpdateMessageOperation{BaseOperation: operation.BaseOperation{Metadata: operation.OperationMetadata{SchemaVersion: 1}}},
	}
	err := m.TryMigrate(context.Background(), "wrong_key", ops)
	if err == nil {
		t.Error("错误的 entityKey 应返回错误")
	}
}

// TestMessageMigrator_TryMigrate_执行操作 测试执行消息迁移操作
func TestMessageMigrator_TryMigrate_执行操作(t *testing.T) {
	store := newFakeMessageStore()
	m := NewMessageMigrator(store)

	executed := false
	ops := []operation.Operation{
		&operation.UpdateMessageOperation{
			BaseOperation: operation.BaseOperation{Metadata: operation.OperationMetadata{SchemaVersion: 1}},
			UpdateFunc: func(ctx context.Context, messageStore db.BaseMessageStore) error {
				executed = true
				return nil
			},
		},
		&operation.UpdateMessageOperation{
			BaseOperation: operation.BaseOperation{Metadata: operation.OperationMetadata{SchemaVersion: 2}},
			UpdateFunc: func(ctx context.Context, messageStore db.BaseMessageStore) error {
				return messageStore.SetSchemaVersion(ctx, 2)
			},
		},
	}

	err := m.TryMigrate(context.Background(), MessageEntityKey, ops)
	if err != nil {
		t.Fatalf("TryMigrate 失败: %v", err)
	}

	if !executed {
		t.Error("操作未被执行")
	}

	if store.schemaVersion != 2 {
		t.Errorf("schemaVersion = %d, want 2", store.schemaVersion)
	}
}

// TestMessageMigrator_TryMigrate_版本已是最新 测试当前版本已是最新的情况
func TestMessageMigrator_TryMigrate_版本已是最新(t *testing.T) {
	store := newFakeMessageStore()
	store.schemaVersion = 5
	m := NewMessageMigrator(store)

	ops := []operation.Operation{
		&operation.UpdateMessageOperation{
			BaseOperation: operation.BaseOperation{Metadata: operation.OperationMetadata{SchemaVersion: 3}},
		},
	}

	err := m.TryMigrate(context.Background(), MessageEntityKey, ops)
	if err != nil {
		t.Errorf("版本已是最新时应返回 nil, got %v", err)
	}
}

// TestMessageMigrator_TryMigrate_操作失败恢复备份 测试操作失败时恢复备份
func TestMessageMigrator_TryMigrate_操作失败恢复备份(t *testing.T) {
	store := newFakeMessageStore()

	// 添加初始消息
	store.messages = []*db.MessageAndMeta{
		{
			Message: schema.NewUserMessage("hello"),
			Metadata: &db.MessageMetadata{
				UserID:    "user1",
				ScopeID:   "scope1",
				SessionID: "session1",
				Timestamp: time.Now(),
			},
		},
	}
	store.schemaVersion = 1

	m := NewMessageMigrator(store)

	ops := []operation.Operation{
		&operation.UpdateMessageOperation{
			BaseOperation: operation.BaseOperation{Metadata: operation.OperationMetadata{SchemaVersion: 2}},
			UpdateFunc: func(ctx context.Context, messageStore db.BaseMessageStore) error {
				// 先删除消息（模拟操作修改数据）
				messageStore.DeleteMessages(ctx, nil)
				return fmt.Errorf("模拟操作失败")
			},
		},
	}

	err := m.TryMigrate(context.Background(), MessageEntityKey, ops)
	if err == nil {
		t.Error("操作失败应返回错误")
	}

	// 验证消息已恢复
	if len(store.messages) != 1 {
		t.Errorf("恢复后应有 1 条消息, 实际 %d", len(store.messages))
	}

	// 验证版本已恢复
	if store.schemaVersion != 1 {
		t.Errorf("恢复后 schemaVersion = %d, want 1", store.schemaVersion)
	}
}

// TestMessageMigrator_TryMigrate_部分操作待执行 测试只执行高版本操作
func TestMessageMigrator_TryMigrate_部分操作待执行(t *testing.T) {
	store := newFakeMessageStore()
	store.schemaVersion = 2
	m := NewMessageMigrator(store)

	executed := make(map[int]bool)
	ops := []operation.Operation{
		&operation.UpdateMessageOperation{
			BaseOperation: operation.BaseOperation{Metadata: operation.OperationMetadata{SchemaVersion: 1}},
			UpdateFunc: func(ctx context.Context, messageStore db.BaseMessageStore) error {
				executed[1] = true
				return nil
			},
		},
		&operation.UpdateMessageOperation{
			BaseOperation: operation.BaseOperation{Metadata: operation.OperationMetadata{SchemaVersion: 2}},
			UpdateFunc: func(ctx context.Context, messageStore db.BaseMessageStore) error {
				executed[2] = true
				return nil
			},
		},
		&operation.UpdateMessageOperation{
			BaseOperation: operation.BaseOperation{Metadata: operation.OperationMetadata{SchemaVersion: 3}},
			UpdateFunc: func(ctx context.Context, messageStore db.BaseMessageStore) error {
				executed[3] = true
				return nil
			},
		},
	}

	err := m.TryMigrate(context.Background(), MessageEntityKey, ops)
	if err != nil {
		t.Fatalf("TryMigrate 失败: %v", err)
	}

	if executed[1] || executed[2] {
		t.Error("schema_version<=2 的操作不应执行")
	}
	if !executed[3] {
		t.Error("schema_version=3 的操作应执行")
	}
}

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

// truncatingStore 每次 GetMessages 最多返回固定条数的存储，用于构造"备份取不全"。
//
// 真实 store 不存在这种截断，但备份不足是必须防的失败模式：一旦以不完整的备份
// 走恢复流程，那些没被备份的记录会被 DeleteMessages 永久删掉。
type truncatingStore struct {
	*fakeMessageStore
	// maxPerGet 单次 GetMessages 最多返回条数
	maxPerGet int
}

// GetMessages 忽略 limit 与分页参数，恒定只返回前 maxPerGet 条。
func (s *truncatingStore) GetMessages(_ context.Context, _ *db.MessageFilter, _ int, _, _ string) ([]*db.MessageAndMeta, error) {
	if s.maxPerGet < len(s.messages) {
		return s.messages[:s.maxPerGet], nil
	}
	return s.messages, nil
}

// TestCreateBackup_返回备份总数 备份必须同时返回"库中应有条数"，作为恢复阶段的判据。
func TestCreateBackup_返回备份总数(t *testing.T) {
	store := newFakeMessageStore()
	for i := 0; i < 3; i++ {
		_, _ = store.AddMessage(context.Background(), &db.MessageAdd{
			Message: schema.NewUserMessage(fmt.Sprintf("msg-%d", i)),
			UserID:  "u1", ScopeID: "s1",
		})
	}

	m := NewMessageMigrator(store)
	records, expected, err := m.createBackup(context.Background())
	if err != nil {
		t.Fatalf("createBackup 失败: %v", err)
	}
	if len(records) != 3 {
		t.Errorf("备份条数 = %d, want 3", len(records))
	}
	if expected != 3 {
		t.Errorf("expected = %d, want 3", expected)
	}
}

// TestRestoreFromBackup_备份不足时拒绝删除 这是 E-08 的核心：备份不完整时宁可恢复
// 失败，也不能删掉备份里没有的记录。
func TestRestoreFromBackup_备份不足时拒绝删除(t *testing.T) {
	store := newFakeMessageStore()
	for i := 0; i < 5; i++ {
		_, _ = store.AddMessage(context.Background(), &db.MessageAdd{
			Message: schema.NewUserMessage(fmt.Sprintf("msg-%d", i)),
			UserID:  "u1", ScopeID: "s1",
		})
	}

	m := NewMessageMigrator(store)
	// 备份只有 2 条，但库里有 5 条
	partial := []messageBackupRecord{
		{MessageContent: "msg-0", MessageRole: "user", UserID: "u1", ScopeID: "s1"},
		{MessageContent: "msg-1", MessageRole: "user", UserID: "u1", ScopeID: "s1"},
	}

	err := m.restoreFromBackup(context.Background(), partial, 5, nil)
	if err == nil {
		t.Fatal("备份不足时应返回错误并跳过恢复")
	}
	// 关键断言：一条都不能删
	if len(store.messages) != 5 {
		t.Errorf("备份不足时不得删除消息，剩余 %d 条，want 5", len(store.messages))
	}
}

// TestRestoreFromBackup_备份完整时正常恢复 条数一致时按备份重建。
func TestRestoreFromBackup_备份完整时正常恢复(t *testing.T) {
	store := newFakeMessageStore()
	for i := 0; i < 2; i++ {
		_, _ = store.AddMessage(context.Background(), &db.MessageAdd{
			Message: schema.NewUserMessage(fmt.Sprintf("old-%d", i)),
			UserID:  "u1", ScopeID: "s1",
		})
	}

	m := NewMessageMigrator(store)
	full := []messageBackupRecord{
		{MessageContent: "new-0", MessageRole: "user", UserID: "u1", ScopeID: "s1"},
		{MessageContent: "new-1", MessageRole: "user", UserID: "u1", ScopeID: "s1"},
	}

	if err := m.restoreFromBackup(context.Background(), full, 2, nil); err != nil {
		t.Fatalf("完整备份应能正常恢复: %v", err)
	}
	if len(store.messages) != 2 {
		t.Errorf("恢复后条数 = %d, want 2", len(store.messages))
	}
}

// TestTryMigrate_备份被截断时不丢数据 端到端回归：store 只能取回部分消息时，
// 迁移失败后的恢复流程必须整体放弃删除，原有记录一条不少。
func TestTryMigrate_备份被截断时不丢数据(t *testing.T) {
	base := newFakeMessageStore()
	for i := 0; i < 5; i++ {
		_, _ = base.AddMessage(context.Background(), &db.MessageAdd{
			Message: schema.NewUserMessage(fmt.Sprintf("msg-%d", i)),
			UserID:  "u1", ScopeID: "s1",
		})
	}
	// 只取回 2 条，模拟备份不完整
	store := &truncatingStore{fakeMessageStore: base, maxPerGet: 2}

	m := NewMessageMigrator(store)
	ops := []operation.Operation{
		&operation.UpdateMessageOperation{
			BaseOperation: operation.BaseOperation{Metadata: operation.OperationMetadata{SchemaVersion: 1}},
			UpdateFunc: func(ctx context.Context, messageStore db.BaseMessageStore) error {
				return fmt.Errorf("迁移故意失败以触发恢复")
			},
		},
	}

	_ = m.TryMigrate(context.Background(), MessageEntityKey, ops)

	if len(base.messages) != 5 {
		t.Errorf("备份不完整时不得删除消息，剩余 %d 条，want 5", len(base.messages))
	}
}

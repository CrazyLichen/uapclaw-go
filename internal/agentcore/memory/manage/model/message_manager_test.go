package model

import (
	"context"
	"fmt"
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/store/db"
)

// mockMessageStore 用于测试的 BaseMessageStore mock
type mockMessageStore struct {
	messages map[string]*db.MessageAndMeta
	nextID   int
}

func newMockMessageStore() *mockMessageStore {
	return &mockMessageStore{
		messages: make(map[string]*db.MessageAndMeta),
		nextID:   1,
	}
}

func (m *mockMessageStore) AddMessage(_ context.Context, messageAdd *db.MessageAdd) (string, error) {
	id := fmt.Sprintf("msg_mock_%d", m.nextID)
	m.nextID++
	m.messages[id] = &db.MessageAndMeta{
		Message: messageAdd.Message,
		Metadata: &db.MessageMetadata{
			MessageID:   id,
			UserID:      messageAdd.UserID,
			ScopeID:     messageAdd.ScopeID,
			SessionID:   messageAdd.SessionID,
			Timestamp:   messageAdd.Timestamp,
			MessageType: messageAdd.Message.Role.String(),
		},
	}
	return id, nil
}

func (m *mockMessageStore) AddMessages(_ context.Context, adds []*db.MessageAdd) ([]string, error) {
	ids := make([]string, len(adds))
	for i, add := range adds {
		id, _ := m.AddMessage(context.Background(), add)
		ids[i] = id
	}
	return ids, nil
}

func (m *mockMessageStore) GetMessageByID(_ context.Context, messageID string) (*schema.BaseMessage, *db.MessageMetadata, error) {
	if item, ok := m.messages[messageID]; ok {
		return item.Message, item.Metadata, nil
	}
	return nil, nil, db.ErrMessageNotFound
}

func (m *mockMessageStore) GetMessages(_ context.Context, filter *db.MessageFilter, limit int, orderBy string, orderDirection string) ([]*db.MessageAndMeta, error) {
	result := make([]*db.MessageAndMeta, 0)
	for _, item := range m.messages {
		if filter.UserID != "" && item.Metadata.UserID != filter.UserID {
			continue
		}
		if filter.ScopeID != "" && item.Metadata.ScopeID != filter.ScopeID {
			continue
		}
		result = append(result, item)
		if len(result) >= limit {
			break
		}
	}
	return result, nil
}

func (m *mockMessageStore) UpdateMessage(_ context.Context, _ string, _ schema.MessageContent) error {
	return nil
}

func (m *mockMessageStore) DeleteMessageByID(_ context.Context, _ string) error {
	return nil
}

func (m *mockMessageStore) DeleteMessages(_ context.Context, filter *db.MessageFilter) (int64, error) {
	count := int64(0)
	for id, item := range m.messages {
		if filter.UserID != "" && item.Metadata.UserID != filter.UserID {
			continue
		}
		if filter.ScopeID != "" && item.Metadata.ScopeID != filter.ScopeID {
			continue
		}
		delete(m.messages, id)
		count++
	}
	return count, nil
}

func (m *mockMessageStore) CountMessages(_ context.Context, filter *db.MessageFilter) (int64, error) {
	count := int64(0)
	for _, item := range m.messages {
		if filter.UserID != "" && item.Metadata.UserID != filter.UserID {
			continue
		}
		if filter.ScopeID != "" && item.Metadata.ScopeID != filter.ScopeID {
			continue
		}
		count++
	}
	return count, nil
}

func (m *mockMessageStore) GetSchemaVersion(_ context.Context) (int32, error) {
	return 0, nil
}

func (m *mockMessageStore) SetSchemaVersion(_ context.Context, _ int32) error {
	return nil
}

// TestMessageManager_Add_验证必填字段 验证 Add 方法的必填字段校验
func TestMessageManager_Add_验证必填字段(t *testing.T) {
	mgr := NewMessageManager(newMockMessageStore())
	ctx := context.Background()

	// 缺少 UserID
	_, err := mgr.Add(ctx, &MessageAddRequest{ScopeID: "scope1", Content: "hello"})
	if err == nil {
		t.Error("期望缺少 UserID 时返回错误")
	}

	// 缺少 ScopeID
	_, err = mgr.Add(ctx, &MessageAddRequest{UserID: "user1", Content: "hello"})
	if err == nil {
		t.Error("期望缺少 ScopeID 时返回错误")
	}

	// 缺少 Content
	_, err = mgr.Add(ctx, &MessageAddRequest{UserID: "user1", ScopeID: "scope1"})
	if err == nil {
		t.Error("期望缺少 Content 时返回错误")
	}
}

// TestMessageManager_Add_正常添加 验证正常添加消息
func TestMessageManager_Add_正常添加(t *testing.T) {
	mgr := NewMessageManager(newMockMessageStore())
	ctx := context.Background()

	id, err := mgr.Add(ctx, &MessageAddRequest{
		UserID:  "user1",
		ScopeID: "scope1",
		Content: "hello",
		Role:    "user",
	})
	if err != nil {
		t.Fatalf("Add 失败: %v", err)
	}
	if id == "" {
		t.Error("ID 不应为空")
	}
}

// TestMessageManager_Get_消息长度校验 验证 messageLen <= 0 时返回错误
func TestMessageManager_Get_消息长度校验(t *testing.T) {
	mgr := NewMessageManager(newMockMessageStore())
	ctx := context.Background()

	_, err := mgr.Get(ctx, "user1", "scope1", "", 0)
	if err == nil {
		t.Error("期望 messageLen <= 0 时返回错误")
	}
}

// TestMessageManager_DeleteByUserAndScope 按用户和作用域删除
func TestMessageManager_DeleteByUserAndScope(t *testing.T) {
	store := newMockMessageStore()
	mgr := NewMessageManager(store)
	ctx := context.Background()

	// 先添加消息
	_, _ = mgr.Add(ctx, &MessageAddRequest{
		UserID:  "user1",
		ScopeID: "scope1",
		Content: "hello",
	})

	count, err := mgr.DeleteByUserAndScope(ctx, "user1", "scope1")
	if err != nil {
		t.Fatalf("DeleteByUserAndScope 失败: %v", err)
	}
	if count != 1 {
		t.Errorf("删除数量 = %d, 期望 1", count)
	}
}

// TestMessageManager_Get_正常获取 验证 Get 正常获取消息
func TestMessageManager_Get_正常获取(t *testing.T) {
	store := newMockMessageStore()
	mgr := NewMessageManager(store)
	ctx := context.Background()

	// 添加 3 条消息
	for i := 0; i < 3; i++ {
		_, _ = mgr.Add(ctx, &MessageAddRequest{
			UserID:  "user1",
			ScopeID: "scope1",
			Content: fmt.Sprintf("msg_%d", i),
		})
	}

	results, err := mgr.Get(ctx, "user1", "scope1", "", 10)
	if err != nil {
		t.Fatalf("Get 失败: %v", err)
	}
	if len(results) != 3 {
		t.Errorf("Get 返回 %d 条, 期望 3 条", len(results))
	}
}

// TestMessageManager_GetByID_存在 验证 GetByID 消息存在时返回
func TestMessageManager_GetByID_存在(t *testing.T) {
	store := newMockMessageStore()
	mgr := NewMessageManager(store)
	ctx := context.Background()

	id, _ := mgr.Add(ctx, &MessageAddRequest{
		UserID:  "user1",
		ScopeID: "scope1",
		Content: "hello",
	})

	result, err := mgr.GetByID(ctx, id)
	if err != nil {
		t.Fatalf("GetByID 失败: %v", err)
	}
	if result == nil {
		t.Fatal("结果不应为 nil")
	}
	if result.Message.Content.Text() != "hello" {
		t.Errorf("Content = %q, want %q", result.Message.Content.Text(), "hello")
	}
}

// TestMessageManager_GetByID_不存在 验证 GetByID 消息不存在时返回 nil
func TestMessageManager_GetByID_不存在(t *testing.T) {
	store := newMockMessageStore()
	mgr := NewMessageManager(store)
	ctx := context.Background()

	result, err := mgr.GetByID(ctx, "nonexistent_id")
	if err != nil {
		t.Fatalf("GetByID 不应返回错误: %v", err)
	}
	if result != nil {
		t.Error("消息不存在时结果应为 nil")
	}
}

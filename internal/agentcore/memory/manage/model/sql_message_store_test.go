package model

import (
	"context"
	"fmt"
	"testing"
	"time"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/store/db"
)

// newTestSqlMessageStore 创建测试用的 SqlMessageStore 实例
func newTestSqlMessageStore(t *testing.T) (*SqlMessageStore, *SqlDbStore, *gorm.DB) {
	t.Helper()
	gormDB, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("打开测试数据库失败: %v", err)
	}
	if err := CreateTables(gormDB); err != nil {
		t.Fatalf("CreateTables 失败: %v", err)
	}
	dbStore := db.NewDefaultDbStore(gormDB)
	sqlDbStore := NewSqlDbStore(dbStore)
	store, err := NewSqlMessageStore(nil, sqlDbStore, "")
	if err != nil {
		t.Fatalf("NewSqlMessageStore 失败: %v", err)
	}
	return store, sqlDbStore, gormDB
}

// TestGenerateMessageID_确定性 验证相同输入生成相同 ID
func TestGenerateMessageID_确定性(t *testing.T) {
	ts := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	id1 := generateMessageID("hello", ts)
	id2 := generateMessageID("hello", ts)
	if id1 != id2 {
		t.Errorf("相同输入应生成相同 ID, got %q and %q", id1, id2)
	}
}

// TestGenerateMessageID_格式 验证 ID 格式为 msg_{hash16}_{timestamp_ms}
func TestGenerateMessageID_格式(t *testing.T) {
	ts := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	id := generateMessageID("hello", ts)
	if len(id) < 4 || id[:4] != "msg_" {
		t.Errorf("ID 应以 msg_ 开头, got %q", id)
	}
}

// TestSqlMessageStore_AddMessage 添加单条消息
func TestSqlMessageStore_AddMessage(t *testing.T) {
	store, _, _ := newTestSqlMessageStore(t)

	msg := schema.NewBaseMessage(schema.RoleTypeUser, "hello")
	add := &db.MessageAdd{
		Message:   msg,
		UserID:    "user1",
		ScopeID:   "scope1",
		SessionID: "session1",
		Timestamp: time.Now(),
	}

	messageID, err := store.AddMessage(context.Background(), add)
	if err != nil {
		t.Fatalf("AddMessage 失败: %v", err)
	}
	if messageID == "" {
		t.Error("messageID 不应为空")
	}
}

// TestSqlMessageStore_GetMessageByID 按 ID 获取消息
func TestSqlMessageStore_GetMessageByID(t *testing.T) {
	store, _, _ := newTestSqlMessageStore(t)

	ts := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	msg := schema.NewBaseMessage(schema.RoleTypeUser, "hello")
	add := &db.MessageAdd{
		Message:   msg,
		UserID:    "user1",
		ScopeID:   "scope1",
		SessionID: "session1",
		Timestamp: ts,
	}

	messageID, err := store.AddMessage(context.Background(), add)
	if err != nil {
		t.Fatalf("AddMessage 失败: %v", err)
	}

	gotMsg, gotMeta, err := store.GetMessageByID(context.Background(), messageID)
	if err != nil {
		t.Fatalf("GetMessageByID 失败: %v", err)
	}
	if gotMsg.Content.Text() != "hello" {
		t.Errorf("Content = %q, want %q", gotMsg.Content.Text(), "hello")
	}
	if gotMeta.UserID != "user1" {
		t.Errorf("UserID = %q, want %q", gotMeta.UserID, "user1")
	}
	if gotMeta.ScopeID != "scope1" {
		t.Errorf("ScopeID = %q, want %q", gotMeta.ScopeID, "scope1")
	}
}

// TestSqlMessageStore_GetMessageByID_不存在 验证不存在的 ID 返回错误
func TestSqlMessageStore_GetMessageByID_不存在(t *testing.T) {
	store, _, _ := newTestSqlMessageStore(t)

	_, _, err := store.GetMessageByID(context.Background(), "nonexistent_id")
	if err == nil {
		t.Error("期望返回错误，但返回 nil")
	}
}

// TestSqlMessageStore_AddMessages 批量添加消息
func TestSqlMessageStore_AddMessages(t *testing.T) {
	store, _, _ := newTestSqlMessageStore(t)

	adds := make([]*db.MessageAdd, 3)
	for i := 0; i < 3; i++ {
		msg := schema.NewBaseMessage(schema.RoleTypeUser, fmt.Sprintf("msg_%d", i))
		adds[i] = &db.MessageAdd{
			Message:   msg,
			UserID:    "user1",
			ScopeID:   "scope1",
			SessionID: "session1",
			Timestamp: time.Date(2024, 1, i+1, 0, 0, 0, 0, time.UTC),
		}
	}

	ids, err := store.AddMessages(context.Background(), adds)
	if err != nil {
		t.Fatalf("AddMessages 失败: %v", err)
	}
	if len(ids) != 3 {
		t.Errorf("返回 %d 个 ID, 期望 3", len(ids))
	}
}

// TestSqlMessageStore_GetMessages 过滤查询消息
func TestSqlMessageStore_GetMessages(t *testing.T) {
	store, _, _ := newTestSqlMessageStore(t)

	ts := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	msg := schema.NewBaseMessage(schema.RoleTypeUser, "hello")
	add := &db.MessageAdd{
		Message:   msg,
		UserID:    "user1",
		ScopeID:   "scope1",
		SessionID: "session1",
		Timestamp: ts,
	}
	if _, err := store.AddMessage(context.Background(), add); err != nil {
		t.Fatalf("AddMessage 失败: %v", err)
	}

	filter := &db.MessageFilter{
		UserID:  "user1",
		ScopeID: "scope1",
	}
	results, err := store.GetMessages(context.Background(), filter, 10, "timestamp", "desc")
	if err != nil {
		t.Fatalf("GetMessages 失败: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("GetMessages 返回 %d 条, 期望 1 条", len(results))
	}
	if results[0].Message.Content.Text() != "hello" {
		t.Errorf("Content = %q, want %q", results[0].Message.Content.Text(), "hello")
	}
}

// TestSqlMessageStore_GetMessages_时间范围 验证时间范围过滤
func TestSqlMessageStore_GetMessages_时间范围(t *testing.T) {
	store, _, _ := newTestSqlMessageStore(t)

	for i := 1; i <= 3; i++ {
		ts := time.Date(2024, 1, i, 0, 0, 0, 0, time.UTC)
		msg := schema.NewBaseMessage(schema.RoleTypeUser, fmt.Sprintf("msg_day_%d", i))
		add := &db.MessageAdd{
			Message:   msg,
			UserID:    "user1",
			ScopeID:   "scope1",
			Timestamp: ts,
		}
		if _, err := store.AddMessage(context.Background(), add); err != nil {
			t.Fatalf("AddMessage 失败: %v", err)
		}
	}

	start := time.Date(2024, 1, 2, 0, 0, 0, 0, time.UTC)
	end := time.Date(2024, 1, 3, 23, 59, 59, 0, time.UTC)
	filter := &db.MessageFilter{
		UserID:    "user1",
		ScopeID:   "scope1",
		StartTime: &start,
		EndTime:   &end,
	}
	results, err := store.GetMessages(context.Background(), filter, 10, "timestamp", "asc")
	if err != nil {
		t.Fatalf("GetMessages 失败: %v", err)
	}
	if len(results) != 2 {
		t.Errorf("时间范围过滤返回 %d 条, 期望 2 条", len(results))
	}
}

// TestSqlMessageStore_UpdateMessage 更新消息内容
func TestSqlMessageStore_UpdateMessage(t *testing.T) {
	store, _, _ := newTestSqlMessageStore(t)

	ts := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	msg := schema.NewBaseMessage(schema.RoleTypeUser, "old_content")
	add := &db.MessageAdd{
		Message:   msg,
		UserID:    "user1",
		ScopeID:   "scope1",
		Timestamp: ts,
	}
	messageID, err := store.AddMessage(context.Background(), add)
	if err != nil {
		t.Fatalf("AddMessage 失败: %v", err)
	}

	if err := store.UpdateMessage(context.Background(), messageID, schema.NewTextContent("new_content")); err != nil {
		t.Fatalf("UpdateMessage 失败: %v", err)
	}

	gotMsg, _, err := store.GetMessageByID(context.Background(), messageID)
	if err != nil {
		t.Fatalf("GetMessageByID 失败: %v", err)
	}
	if gotMsg.Content.Text() != "new_content" {
		t.Errorf("Content = %q, want %q", gotMsg.Content.Text(), "new_content")
	}
}

// TestSqlMessageStore_DeleteMessageByID 删除单条消息
func TestSqlMessageStore_DeleteMessageByID(t *testing.T) {
	store, _, _ := newTestSqlMessageStore(t)

	ts := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	msg := schema.NewBaseMessage(schema.RoleTypeUser, "to_delete")
	add := &db.MessageAdd{
		Message:   msg,
		UserID:    "user1",
		ScopeID:   "scope1",
		Timestamp: ts,
	}
	messageID, err := store.AddMessage(context.Background(), add)
	if err != nil {
		t.Fatalf("AddMessage 失败: %v", err)
	}

	if err := store.DeleteMessageByID(context.Background(), messageID); err != nil {
		t.Fatalf("DeleteMessageByID 失败: %v", err)
	}

	_, _, err = store.GetMessageByID(context.Background(), messageID)
	if err == nil {
		t.Error("期望消息已删除（返回错误），但返回 nil")
	}
}

// TestSqlMessageStore_DeleteMessages 按条件删除消息
func TestSqlMessageStore_DeleteMessages(t *testing.T) {
	store, _, _ := newTestSqlMessageStore(t)

	for i := 0; i < 3; i++ {
		ts := time.Date(2024, 1, i+1, 0, 0, 0, 0, time.UTC)
		msg := schema.NewBaseMessage(schema.RoleTypeUser, fmt.Sprintf("msg_%d", i))
		add := &db.MessageAdd{
			Message:   msg,
			UserID:    "user1",
			ScopeID:   "scope1",
			Timestamp: ts,
		}
		if _, err := store.AddMessage(context.Background(), add); err != nil {
			t.Fatalf("AddMessage 失败: %v", err)
		}
	}

	filter := &db.MessageFilter{UserID: "user1", ScopeID: "scope1"}
	count, err := store.DeleteMessages(context.Background(), filter)
	if err != nil {
		t.Fatalf("DeleteMessages 失败: %v", err)
	}
	if count != 3 {
		t.Errorf("删除数量 = %d, 期望 3", count)
	}
}

// TestSqlMessageStore_CountMessages 统计消息数量
func TestSqlMessageStore_CountMessages(t *testing.T) {
	store, _, _ := newTestSqlMessageStore(t)

	for i := 0; i < 3; i++ {
		ts := time.Date(2024, 1, i+1, 0, 0, 0, 0, time.UTC)
		msg := schema.NewBaseMessage(schema.RoleTypeUser, fmt.Sprintf("msg_%d", i))
		add := &db.MessageAdd{
			Message:   msg,
			UserID:    "user1",
			ScopeID:   "scope1",
			Timestamp: ts,
		}
		if _, err := store.AddMessage(context.Background(), add); err != nil {
			t.Fatalf("AddMessage 失败: %v", err)
		}
	}

	filter := &db.MessageFilter{UserID: "user1", ScopeID: "scope1"}
	count, err := store.CountMessages(context.Background(), filter)
	if err != nil {
		t.Fatalf("CountMessages 失败: %v", err)
	}
	if count != 3 {
		t.Errorf("Count = %d, want 3", count)
	}
}

// TestSqlMessageStore_SchemaVersion schema 版本管理
func TestSqlMessageStore_SchemaVersion(t *testing.T) {
	store, _, _ := newTestSqlMessageStore(t)

	// 初始版本应为 -1（未设置，对齐 Python 返回 None 的语义）
	version, err := store.GetSchemaVersion(context.Background())
	if err != nil {
		t.Fatalf("GetSchemaVersion 失败: %v", err)
	}
	if version != -1 {
		t.Errorf("初始版本 = %d, want -1（未设置）", version)
	}

	// 设置版本
	if err := store.SetSchemaVersion(context.Background(), 2); err != nil {
		t.Fatalf("SetSchemaVersion 失败: %v", err)
	}

	// 验证版本
	version, err = store.GetSchemaVersion(context.Background())
	if err != nil {
		t.Fatalf("GetSchemaVersion 失败: %v", err)
	}
	if version != 2 {
		t.Errorf("版本 = %d, want 2", version)
	}
}

// TestSqlMessageStore_加解密往返 验证加密存储和解密读取
func TestSqlMessageStore_加解密往返(t *testing.T) {
	gormDB, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("打开测试数据库失败: %v", err)
	}
	if err := CreateTables(gormDB); err != nil {
		t.Fatalf("CreateTables 失败: %v", err)
	}

	// 使用 32 字节 AES 密钥
	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i)
	}

	dbStore := db.NewDefaultDbStore(gormDB)
	sqlDbStore := NewSqlDbStore(dbStore)
	store, err := NewSqlMessageStore(key, sqlDbStore, "")
	if err != nil {
		t.Fatalf("NewSqlMessageStore 失败: %v", err)
	}

	ts := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	msg := schema.NewBaseMessage(schema.RoleTypeUser, "secret message")
	add := &db.MessageAdd{
		Message:   msg,
		UserID:    "user1",
		ScopeID:   "scope1",
		SessionID: "session1",
		Timestamp: ts,
	}

	messageID, err := store.AddMessage(context.Background(), add)
	if err != nil {
		t.Fatalf("AddMessage 失败: %v", err)
	}

	gotMsg, _, err := store.GetMessageByID(context.Background(), messageID)
	if err != nil {
		t.Fatalf("GetMessageByID 失败: %v", err)
	}
	if gotMsg.Content.Text() != "secret message" {
		t.Errorf("Content = %q, want %q (解密后应还原原文)", gotMsg.Content.Text(), "secret message")
	}
}

// TestSqlMessageStore_自动时间戳 验证 Timestamp 零值时自动生成
func TestSqlMessageStore_自动时间戳(t *testing.T) {
	store, _, _ := newTestSqlMessageStore(t)

	msg := schema.NewBaseMessage(schema.RoleTypeUser, "auto_ts")
	add := &db.MessageAdd{
		Message: msg,
		UserID:  "user1",
		ScopeID: "scope1",
		// Timestamp 不设置，应自动生成
	}

	messageID, err := store.AddMessage(context.Background(), add)
	if err != nil {
		t.Fatalf("AddMessage 失败: %v", err)
	}
	if messageID == "" {
		t.Error("messageID 不应为空")
	}
}

// TestSqlMessageStore_DefaultTableName 验证默认表名
func TestSqlMessageStore_DefaultTableName(t *testing.T) {
	gormDB, _ := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	dbStore := db.NewDefaultDbStore(gormDB)
	sqlDbStore := NewSqlDbStore(dbStore)
	store, err := NewSqlMessageStore(nil, sqlDbStore, "")
	if err != nil {
		t.Fatalf("NewSqlMessageStore 失败: %v", err)
	}
	if store.tableName != "user_message" {
		t.Errorf("默认表名 = %q, want %q", store.tableName, "user_message")
	}
}

// TestSqlMessageStore_AddMessages_自动时间戳 验证批量添加时自动生成时间戳
func TestSqlMessageStore_AddMessages_自动时间戳(t *testing.T) {
	store, _, _ := newTestSqlMessageStore(t)

	adds := make([]*db.MessageAdd, 2)
	for i := 0; i < 2; i++ {
		msg := schema.NewBaseMessage(schema.RoleTypeUser, fmt.Sprintf("msg_%d", i))
		adds[i] = &db.MessageAdd{
			Message: msg,
			UserID:  "user1",
			ScopeID: "scope1",
			// Timestamp 不设置
		}
	}

	ids, err := store.AddMessages(context.Background(), adds)
	if err != nil {
		t.Fatalf("AddMessages 失败: %v", err)
	}
	if len(ids) != 2 {
		t.Errorf("返回 %d 个 ID, 期望 2", len(ids))
	}
}

// TestRoleTypeFromString 验证 role 字符串解析
func TestRoleTypeFromString(t *testing.T) {
	tests := []struct {
		input string
		want  schema.RoleType
	}{
		{"system", schema.RoleTypeSystem},
		{"user", schema.RoleTypeUser},
		{"assistant", schema.RoleTypeAssistant},
		{"tool", schema.RoleTypeTool},
		{"unknown", schema.RoleTypeUser},
		{"", schema.RoleTypeUser},
	}
	for _, tt := range tests {
		got := roleTypeFromString(tt.input)
		if got != tt.want {
			t.Errorf("roleTypeFromString(%q) = %v, want %v", tt.input, got, tt.want)
		}
	}
}

// TestSqlMessageStore_GetMessages_空过滤 验证空过滤条件返回所有消息
func TestSqlMessageStore_GetMessages_空过滤(t *testing.T) {
	store, _, _ := newTestSqlMessageStore(t)

	// 添加消息
	ts := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	msg := schema.NewBaseMessage(schema.RoleTypeUser, "hello")
	add := &db.MessageAdd{
		Message:   msg,
		UserID:    "user1",
		ScopeID:   "scope1",
		Timestamp: ts,
	}
	if _, err := store.AddMessage(context.Background(), add); err != nil {
		t.Fatalf("AddMessage 失败: %v", err)
	}

	// 空过滤
	filter := &db.MessageFilter{}
	results, err := store.GetMessages(context.Background(), filter, 10, "timestamp", "desc")
	if err != nil {
		t.Fatalf("GetMessages 失败: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("空过滤返回 %d 条, 期望 1 条", len(results))
	}
}

// TestNewSqlMessageStore_key长度错误 验证非 32 字节 key 返回 error
func TestNewSqlMessageStore_key长度错误(t *testing.T) {
	gormDB, _ := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	dbStore := db.NewDefaultDbStore(gormDB)
	sqlDbStore := NewSqlDbStore(dbStore)

	_, err := NewSqlMessageStore([]byte{1, 2, 3}, sqlDbStore, "")
	if err == nil {
		t.Error("非 32 字节 key 应返回 error")
	}
}

// TestSqlMessageStore_多模态消息存储 验证多模态消息写入读取
func TestSqlMessageStore_多模态消息存储(t *testing.T) {
	gormDB, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("打开测试数据库失败: %v", err)
	}
	if err := CreateTables(gormDB); err != nil {
		t.Fatalf("CreateTables 失败: %v", err)
	}

	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i + 1)
	}
	dbStore := db.NewDefaultDbStore(gormDB)
	sqlDbStore := NewSqlDbStore(dbStore)
	store, err := NewSqlMessageStore(key, sqlDbStore, "")
	if err != nil {
		t.Fatalf("NewSqlMessageStore 失败: %v", err)
	}

	// 创建多模态消息
	textPart := schema.ContentPart{Type: "text", Text: "hello"}
	imagePart := schema.ContentPart{Type: "image_url", ImageURL: &schema.ImageURL{URL: "https://example.com/img.png"}}
	content := schema.NewMultiModalContent(textPart, imagePart)
	msg := &schema.BaseMessage{Role: schema.RoleTypeUser, Content: content}

	add := &db.MessageAdd{
		Message:   msg,
		UserID:    "user1",
		ScopeID:   "scope1",
		SessionID: "session1",
		Timestamp: time.Now(),
	}

	messageID, err := store.AddMessage(context.Background(), add)
	if err != nil {
		t.Fatalf("AddMessage 失败: %v", err)
	}

	gotMsg, _, err := store.GetMessageByID(context.Background(), messageID)
	if err != nil {
		t.Fatalf("GetMessageByID 失败: %v", err)
	}

	if gotMsg.Content.IsText() {
		t.Error("多模态消息不应是纯文本")
	}
	parts := gotMsg.Content.Parts()
	if len(parts) != 2 {
		t.Errorf("多模态消息应有 2 个分片, got %d", len(parts))
	}
	if parts[0].Type != "text" || parts[0].Text != "hello" {
		t.Errorf("第一个分片 text 不匹配, got type=%q text=%q", parts[0].Type, parts[0].Text)
	}
	if parts[1].Type != "image_url" || parts[1].ImageURL == nil || parts[1].ImageURL.URL != "https://example.com/img.png" {
		t.Errorf("第二个分片 image_url 不匹配, got type=%q", parts[1].Type)
	}
}

// TestSqlMessageStore_多模态消息更新 验证 UpdateMessage 更新多模态内容
func TestSqlMessageStore_多模态消息更新(t *testing.T) {
	store, _, _ := newTestSqlMessageStore(t)

	ts := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	msg := schema.NewBaseMessage(schema.RoleTypeUser, "old_text")
	add := &db.MessageAdd{
		Message:   msg,
		UserID:    "user1",
		ScopeID:   "scope1",
		Timestamp: ts,
	}
	messageID, _ := store.AddMessage(context.Background(), add)

	// 更新为多模态内容
	textPart := schema.ContentPart{Type: "text", Text: "new_text"}
	newContent := schema.NewMultiModalContent(textPart)
	if err := store.UpdateMessage(context.Background(), messageID, newContent); err != nil {
		t.Fatalf("UpdateMessage 失败: %v", err)
	}

	gotMsg, _, err := store.GetMessageByID(context.Background(), messageID)
	if err != nil {
		t.Fatalf("GetMessageByID 失败: %v", err)
	}
	if gotMsg.Content.IsText() {
		t.Error("更新后应为多模态内容")
	}
	if gotMsg.Content.Parts()[0].Text != "new_text" {
		t.Errorf("更新后文本 = %q, want %q", gotMsg.Content.Parts()[0].Text, "new_text")
	}
}

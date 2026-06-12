package model

import (
	"testing"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

// newTestDB 创建 SQLite 内存数据库用于测试
func newTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("打开测试数据库失败: %v", err)
	}
	return db
}

// TestCreateTables_建表 验证 AutoMigrate 能成功创建所有表
func TestCreateTables_建表(t *testing.T) {
	db := newTestDB(t)

	if err := CreateTables(db); err != nil {
		t.Fatalf("CreateTables 失败: %v", err)
	}

	// 验证表存在
	if !db.Migrator().HasTable(&UserMessage{}) {
		t.Error("user_message 表未创建")
	}
	if !db.Migrator().HasTable(&ScopeUserMapping{}) {
		t.Error("scope_user_mapping 表未创建")
	}
	if !db.Migrator().HasTable(&MemoryMeta{}) {
		t.Error("memory_meta 表未创建")
	}
}

// TestUserMessage_表名 验证表名正确
func TestUserMessage_表名(t *testing.T) {
	u := UserMessage{}
	if u.TableName() != "user_message" {
		t.Errorf("UserMessage 表名 = %q, want %q", u.TableName(), "user_message")
	}
}

// TestScopeUserMapping_表名 验证表名正确
func TestScopeUserMapping_表名(t *testing.T) {
	s := ScopeUserMapping{}
	if s.TableName() != "scope_user_mapping" {
		t.Errorf("ScopeUserMapping 表名 = %q, want %q", s.TableName(), "scope_user_mapping")
	}
}

// TestMemoryMeta_TableName 验证表名正确
func TestMemoryMeta_TableName(t *testing.T) {
	m := MemoryMeta{}
	if m.TableName() != "memory_meta" {
		t.Errorf("MemoryMeta TableName = %q, want %q", m.TableName(), "memory_meta")
	}
}

// TestUserMessage_字段写入读取 验证 UserMessage 可正常写入和读取
func TestUserMessage_字段写入读取(t *testing.T) {
	db := newTestDB(t)
	if err := CreateTables(db); err != nil {
		t.Fatalf("CreateTables 失败: %v", err)
	}

	msg := UserMessage{
		MessageID: "msg_abc123_1700000000000",
		UserID:    "user1",
		ScopeID:   "scope1",
		Content:   "encrypted_content",
		SessionID: "session1",
		Role:      "user",
		Timestamp: "2024-01-01T00:00:00Z",
	}

	if err := db.Create(&msg).Error; err != nil {
		t.Fatalf("写入失败: %v", err)
	}

	var got UserMessage
	if err := db.First(&got, "message_id = ?", "msg_abc123_1700000000000").Error; err != nil {
		t.Fatalf("读取失败: %v", err)
	}

	if got.UserID != "user1" {
		t.Errorf("UserID = %q, want %q", got.UserID, "user1")
	}
	if got.Role != "user" {
		t.Errorf("Role = %q, want %q", got.Role, "user")
	}
}

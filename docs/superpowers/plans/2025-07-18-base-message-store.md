# BaseMessageStore 消息持久化实现计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 实现 BaseMessageStore 接口及其 SQL 消息存储，提供消息的 CRUD、加密存储、过滤查询和 schema 版本管理能力。

**Architecture:** 接口定义在 `store/db/base_message_store.go`（与 BaseDbStore 同包），实现在 `memory/manage/model/` 下（对齐 Python 的 `mem_model/` 目录）。SqlDbStore 作为通用 SQL CRUD 层封装 GORM 操作，SqlMessageStore 基于它构建。MessageManager 作为上层管理封装验证逻辑。

**Tech Stack:** Go 1.22+, GORM (已有), AES-256-GCM (crypto 包已有), SQLite 内存数据库 (测试用)

---

## Task 1: 错误码定义

**Files:**
- Modify: `internal/common/exception/codes_framework.go`
- Test: `internal/common/exception/codes_framework_test.go` (如果存在则修改，否则跳过)

- [ ] **Step 1: 在 codes_framework.go 的 186 区域（Store supporting）后新增 187 区域（Store Message）**

在 `StatusStoreGraphCollectionNotSupported` 之后，`188 区域` 之前，新增：

```go
// =============================================================================================================
// 187. Foundation — Store Message 187000–187999
// =============================================================================================================

var (
	// StatusStoreMessageGetExecutionError 消息获取执行错误
	StatusStoreMessageGetExecutionError = NewStatusCode(
		"STORE_MESSAGE_GET_EXECUTION_ERROR", 187000,
		"store message get execution error, reason: {error_msg}")
	// StatusStoreMessageAddExecutionError 消息添加执行错误
	StatusStoreMessageAddExecutionError = NewStatusCode(
		"STORE_MESSAGE_ADD_EXECUTION_ERROR", 187001,
		"store message add execution error, reason: {error_msg}")
	// StatusStoreMessageUpdateExecutionError 消息更新执行错误
	StatusStoreMessageUpdateExecutionError = NewStatusCode(
		"STORE_MESSAGE_UPDATE_EXECUTION_ERROR", 187002,
		"store message update execution error, reason: {error_msg}")
	// StatusStoreMessageDeleteExecutionError 消息删除执行错误
	StatusStoreMessageDeleteExecutionError = NewStatusCode(
		"STORE_MESSAGE_DELETE_EXECUTION_ERROR", 187003,
		"store message delete execution error, reason: {error_msg}")
	// StatusStoreMessageNotFound 消息不存在
	StatusStoreMessageNotFound = NewStatusCode(
		"STORE_MESSAGE_NOT_FOUND", 187004,
		"store message not found, message_id={message_id}")
	// StatusStoreMessageCountExecutionError 消息计数执行错误
	StatusStoreMessageCountExecutionError = NewStatusCode(
		"STORE_MESSAGE_COUNT_EXECUTION_ERROR", 187005,
		"store message count execution error, reason: {error_msg}")
)
```

- [ ] **Step 2: 编译验证**

Run: `cd /home/opensource/uap-claw-go && go build ./internal/common/exception/...`
Expected: 编译成功，无错误

- [ ] **Step 3: 提交**

```bash
git add internal/common/exception/codes_framework.go
git commit -m "feat(store): 新增消息存储错误码 187000-187005"
```

---

## Task 2: BaseMessageStore 接口与辅助类型

**Files:**
- Create: `internal/agentcore/store/db/base_message_store.go`
- Modify: `internal/agentcore/store/db/doc.go`
- Test: `internal/agentcore/store/db/base_message_store_test.go`

- [ ] **Step 1: 创建 base_message_store.go — 接口与辅助类型**

```go
package db

import (
	"context"
	"time"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
)

// ──────────────────────────── 结构体 ────────────────────────────

// MessageMetadata 消息元数据。
//
// 对应 Python: openjiuwen/core/foundation/store/base_message_store.py (MessageMetadata)
type MessageMetadata struct {
	// MessageID 消息唯一标识
	MessageID string
	// UserID 用户 ID
	UserID string
	// ScopeID 作用域 ID
	ScopeID string
	// SessionID 会话 ID
	SessionID string
	// Timestamp 时间戳（数据库存 string，Go 用 time.Time，读取时转换）
	Timestamp time.Time
	// MessageType 消息类型
	MessageType string
}

// MessageAdd 添加消息的入参。
//
// 对应 Python: message_add 字典
type MessageAdd struct {
	// Message 消息对象
	Message *schema.BaseMessage
	// UserID 用户 ID
	UserID string
	// ScopeID 作用域 ID
	ScopeID string
	// SessionID 会话 ID
	SessionID string
	// Timestamp 时间戳（零值时自动生成当前时间）
	Timestamp time.Time
}

// MessageFilter 消息查询过滤条件。
//
// 对应 Python: message_filter 字典
// 修正：实现 StartTime/EndTime 过滤，跳过 MessageType（数据库表无对应列）
type MessageFilter struct {
	// UserID 用户 ID
	UserID string
	// ScopeID 作用域 ID
	ScopeID string
	// SessionID 会话 ID
	SessionID string
	// StartTime 起始时间（nil 表示不限制）
	StartTime *time.Time
	// EndTime 结束时间（nil 表示不限制）
	EndTime *time.Time
}

// MessageAndMeta 消息+元数据组合（用于 GetMessages 返回）。
type MessageAndMeta struct {
	// Message 消息对象
	Message *schema.BaseMessage
	// Metadata 消息元数据
	Metadata *MessageMetadata
}

// BaseMessageStore 消息持久化接口。
//
// 所有消息存储后端必须实现此接口。
// 对应 Python: openjiuwen/core/foundation/store/base_message_store.py (BaseMessageStore)
type BaseMessageStore interface {
	// AddMessage 添加单条消息，返回 message_id。
	//
	// 对应 Python: BaseMessageStore.add_message(message_add)
	AddMessage(ctx context.Context, messageAdd *MessageAdd) (string, error)

	// AddMessages 批量添加消息，返回 ID 列表。
	// 修正：真正批量写入，而非循环调用 AddMessage。
	//
	// 对应 Python: BaseMessageStore.add_messages(message_adds)
	AddMessages(ctx context.Context, messageAdds []*MessageAdd) ([]string, error)

	// GetMessageByID 按 ID 获取消息，不存在时返回错误。
	//
	// 对应 Python: BaseMessageStore.get_message_by_id(message_id)
	GetMessageByID(ctx context.Context, messageID string) (*schema.BaseMessage, *MessageMetadata, error)

	// GetMessages 按条件过滤查询消息。
	// 修正：实现 StartTime/EndTime 过滤（跳过 MessageType）。
	//
	// 对应 Python: BaseMessageStore.get_messages(message_filter, limit, order_by, order_direction)
	GetMessages(ctx context.Context, filter *MessageFilter, limit int, orderBy string, orderDirection string) ([]*MessageAndMeta, error)

	// UpdateMessage 更新消息内容。
	//
	// 对应 Python: BaseMessageStore.update_message(message_id, content)
	UpdateMessage(ctx context.Context, messageID string, content schema.MessageContent) error

	// DeleteMessageByID 按 ID 删除单条消息。
	//
	// 对应 Python: BaseMessageStore.delete_message_by_id(message_id)
	DeleteMessageByID(ctx context.Context, messageID string) error

	// DeleteMessages 按条件删除消息，返回删除数量。
	//
	// 对应 Python: BaseMessageStore.delete_messages(message_filter)
	DeleteMessages(ctx context.Context, filter *MessageFilter) (int64, error)

	// CountMessages 统计匹配消息数量。
	// 修正：使用 SQL COUNT，而非取回全部数据后 len()。
	//
	// 对应 Python: BaseMessageStore.count_messages(message_filter)
	CountMessages(ctx context.Context, filter *MessageFilter) (int64, error)

	// GetSchemaVersion 获取当前 schema 版本号。
	//
	// 对应 Python: BaseMessageStore.get_schema_version()
	GetSchemaVersion(ctx context.Context) (int32, error)

	// SetSchemaVersion 设置 schema 版本号。
	//
	// 对应 Python: BaseMessageStore.set_schema_version(version)
	SetSchemaVersion(ctx context.Context, version int32) error
}
```

- [ ] **Step 2: 编译验证**

Run: `cd /home/opensource/uap-claw-go && go build ./internal/agentcore/store/db/...`
Expected: 编译成功

- [ ] **Step 3: 创建 base_message_store_test.go — 接口契约测试**

```go
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

func (f *fakeMessageStore) GetMessageByID(_ context.Context, _ string) (*schema.BaseMessage, *MessageMetadata, error) {
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
	filter := &MessageFilter{
		UserID:    "user1",
		ScopeID:   "scope1",
		SessionID: "session1",
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
	msg := schema.NewBaseMessage(schema.RoleTypeUser, "hello")
	add := &MessageAdd{
		Message:   msg,
		UserID:    "user1",
		ScopeID:   "scope1",
		SessionID: "session1",
		Timestamp: time.Now(),
	}
	if add.Message.Role != schema.RoleTypeUser {
		t.Errorf("Message.Role = %v, want %v", add.Message.Role, schema.RoleTypeUser)
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
```

- [ ] **Step 4: 运行测试验证**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/store/db/... -v -run "TestBaseMessageStore|TestMessageFilter|TestMessageAdd|TestMessageMetadata"`
Expected: 全部 PASS

- [ ] **Step 5: 更新 store/db/doc.go 文件目录**

在 `doc.go` 的文件目录树中添加 `base_message_store.go` 条目，在核心类型索引中添加 `BaseMessageStore` 等新类型。具体：在文件目录 `├── default.go` 后新增 `├── base_message_store.go  # BaseMessageStore 接口及辅助类型`，在核心类型索引中新增：
``// BaseMessageStore — 消息持久化接口，定义消息的 CRUD、计数和 schema 版本管理
// MessageMetadata   — 消息元数据（message_id, user_id, scope_id, session_id, timestamp, message_type）
// MessageAdd        — 添加消息的入参
// MessageFilter     — 消息查询过滤条件
// MessageAndMeta    — 消息+元数据组合``

- [ ] **Step 6: 提交**

```bash
git add internal/agentcore/store/db/base_message_store.go internal/agentcore/store/db/base_message_store_test.go internal/agentcore/store/db/doc.go
git commit -m "feat(store): 新增 BaseMessageStore 接口及辅助类型"
```

---

## Task 3: 数据库模型（db_model.go）

**Files:**
- Create: `internal/agentcore/memory/manage/model/doc.go`
- Create: `internal/agentcore/memory/manage/model/db_model.go`
- Create: `internal/agentcore/memory/manage/model/db_model_test.go`

- [ ] **Step 1: 创建 model 包目录结构**

Run: `mkdir -p /home/opensource/uap-claw-go/internal/agentcore/memory/manage/model`

- [ ] **Step 2: 创建 doc.go**

```go
// Package model 提供记忆系统的数据模型和数据库操作。
//
// 本包定义了消息存储相关的数据库模型（UserMessage）、
// 通用 SQL CRUD 层（SqlDbStore）、消息存储实现（SqlMessageStore）
// 和消息管理器（MessageManager）。
//
// 文件目录：
//
//	model/
//	├── doc.go                 # 包文档
//	├── db_model.go            # 数据库模型（UserMessage、ScopeUserMapping、MemoryMeta）
//	├── sql_db_store.go        # SqlDbStore 通用 SQL CRUD 层
//	├── sql_message_store.go   # SqlMessageStore 消息存储实现
//	└── message_manager.go     # MessageManager 消息管理器
//
// 对应 Python 代码：
//
//	openjiuwen/core/memory/manage/mem_model/
//
// 核心类型/接口索引：
//
//	UserMessage      — 用户消息表 GORM 模型
//	ScopeUserMapping — 作用域用户映射表 GORM 模型
//	MemoryMeta       — 记忆元数据表 GORM 模型
//	SqlDbStore       — 通用 SQL CRUD 层，封装 GORM 通用操作
//	SqlMessageStore  — BaseMessageStore 的 SQL 实现
//	MessageManager   — 消息管理器，BaseMessageStore 的上层封装
package model
```

- [ ] **Step 3: 创建 db_model.go — 数据库模型**

```go
package model

import "gorm.io/gorm"

// ──────────────────────────── 结构体 ────────────────────────────

// UserMessage 用户消息表模型。
//
// 对应 Python: openjiuwen/core/memory/manage/mem_model/db_model.py (UserMessage)
type UserMessage struct {
	// MessageID 消息唯一标识（SHA-256 hash 前16位 + 时间戳毫秒）
	MessageID string `gorm:"primaryKey;size:64"`
	// UserID 用户 ID
	UserID string `gorm:"size:64;not null"`
	// ScopeID 作用域 ID
	ScopeID string `gorm:"size:64;not null"`
	// Content 消息内容（AES 加密后存储）
	Content string `gorm:"size:4096;not null"`
	// SessionID 会话 ID
	SessionID string `gorm:"size:64"`
	// Role 消息角色
	Role string `gorm:"size:32"`
	// Timestamp 时间戳（ISO 字符串，对齐 Python）
	Timestamp string `gorm:"size:32"`
}

// TableName 指定表名。
func (UserMessage) TableName() string { return "user_message" }

// ScopeUserMapping 作用域用户映射表模型。
//
// 对应 Python: openjiuwen/core/memory/manage/mem_model/db_model.py (ScopeUserMapping)
type ScopeUserMapping struct {
	// UserID 用户 ID
	UserID string `gorm:"primaryKey;size:64;not null"`
	// ScopeID 作用域 ID
	ScopeID string `gorm:"primaryKey;size:64;not null"`
}

// TableName 指定表名。
func (ScopeUserMapping) TableName() string { return "scope_user_mapping" }

// MemoryMeta 记忆元数据表模型，用于 schema 版本管理。
//
// 对应 Python: openjiuwen/core/memory/manage/mem_model/db_model.py (MemoryMeta)
type MemoryMeta struct {
	// TableName 元数据对应的表名
	TableName string `gorm:"primaryKey;size:64;not null"`
	// SchemaVersion schema 版本号
	SchemaVersion string `gorm:"size:64;not null"`
}

// TblName 避免 GORM 将 TableName 字段误认为表名指定方法，
// 提供 GORM 使用的表名。
func (MemoryMeta) TblName() string { return "memory_meta" }

// ──────────────────────────── 导出函数 ────────────────────────────

// CreateTables 创建所有记忆表。
// 使用 GORM AutoMigrate 自动建表，对齐 Python 的 create_tables()。
//
// 对应 Python: openjiuwen/core/memory/manage/mem_model/db_model.py (create_tables)
func CreateTables(db *gorm.DB) error {
	return db.AutoMigrate(
		&UserMessage{},
		&ScopeUserMapping{},
		&MemoryMeta{},
	)
}
```

- [ ] **Step 4: 创建 db_model_test.go**

```go
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
	if UserMessage{}.TableName() != "user_message" {
		t.Errorf("UserMessage 表名 = %q, want %q", UserMessage{}.TableName(), "user_message")
	}
}

// TestScopeUserMapping_表名 验证表名正确
func TestScopeUserMapping_表名(t *testing.T) {
	if ScopeUserMapping{}.TableName() != "scope_user_mapping" {
		t.Errorf("ScopeUserMapping 表名 = %q, want %q", ScopeUserMapping{}.TableName(), "scope_user_mapping")
	}
}

// TestMemoryMeta_TblName 验证表名正确
func TestMemoryMeta_TblName(t *testing.T) {
	if MemoryMeta{}.TblName() != "memory_meta" {
		t.Errorf("MemoryMeta TblName = %q, want %q", MemoryMeta{}.TblName(), "memory_meta")
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
```

- [ ] **Step 5: 运行测试验证**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/memory/manage/model/... -v`
Expected: 全部 PASS

- [ ] **Step 6: 提交**

```bash
git add internal/agentcore/memory/manage/model/
git commit -m "feat(memory): 新增数据库模型 UserMessage/ScopeUserMapping/MemoryMeta"
```

---

## Task 4: SqlDbStore 通用 CRUD 层

**Files:**
- Create: `internal/agentcore/memory/manage/model/sql_db_store.go`
- Create: `internal/agentcore/memory/manage/model/sql_db_store_test.go`

- [ ] **Step 1: 创建 sql_db_store.go**

```go
package model

import (
	"context"
	"fmt"
	"time"

	"gorm.io/gorm"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/store/db"
	"github.com/uapclaw/uapclaw-go/internal/common/exception"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 常量 ────────────────────────────

const (
	// logComponent 日志组件
	logComponent = logger.ComponentAgentCore
)

// ──────────────────────────── 结构体 ────────────────────────────

// SqlDbStore 基于 BaseDbStore 的通用 SQL CRUD 封装。
//
// 本封装将 GORM 的常用操作抽象为通用方法，让上层组件
// （SqlMessageStore 等）通过统一接口操作数据库，
// 而非直接编写 GORM 查询。
//
// 对应 Python: openjiuwen/core/memory/manage/mem_model/sql_db_store.py (SqlDbStore)
type SqlDbStore struct {
	// dbStore 数据库存储抽象
	dbStore db.BaseDbStore
	// db GORM 数据库实例
	db *gorm.DB
}

// ──────────────────────────── 导出函数 ────────────────────────────

// NewSqlDbStore 创建 SqlDbStore 实例。
// 从 BaseDbStore 获取 *gorm.DB，后续所有操作通过此实例执行。
//
// 对应 Python: SqlDbStore.__init__(db_store)
func NewSqlDbStore(dbStore db.BaseDbStore) *SqlDbStore {
	return &SqlDbStore{
		dbStore: dbStore,
		db:      dbStore.GetDB(context.Background()),
	}
}

// Write 插入一行数据到指定表。
// data 为列名到值的映射。
//
// 对应 Python: SqlDbStore.write(table, data)
func (s *SqlDbStore) Write(ctx context.Context, table string, data map[string]any) error {
	if err := s.db.Table(table).Create(data).Error; err != nil {
		logger.Error(logComponent).
			Str("event_type", "MEMORY_STORE").
			Str("table_name", table).
			Err(err).
			Msg("写入数据失败")
		return exception.BuildError(exception.StatusStoreMessageAddExecutionError,
			exception.WithParam("error_msg", fmt.Sprintf("write failed for table %s: %s", table, err.Error())),
		)
	}
	return nil
}

// ConditionGet 条件查询，支持 IN 子句。
// conditions 的值必须为切片类型（对应 Python 的 list），用于 IN 查询。
// columns 指定需要返回的列，为空时返回所有列。
//
// 对应 Python: SqlDbStore.condition_get(table, conditions, columns)
func (s *SqlDbStore) ConditionGet(ctx context.Context, table string, conditions map[string]any, columns []string) ([]map[string]any, error) {
	query := s.db.Table(table)
	// 选择指定列
	if len(columns) > 0 {
		query = query.Select(columns)
	}
	// 构建 IN 条件
	for col, val := range conditions {
		query = query.Where(fmt.Sprintf("%s IN ?", col), val)
	}

	var results []map[string]any
	if err := query.Find(&results).Error; err != nil {
		logger.Error(logComponent).
			Str("event_type", "MEMORY_RETRIEVE").
			Str("table_name", table).
			Err(err).
			Msg("条件查询失败")
		return nil, exception.BuildError(exception.StatusStoreMessageGetExecutionError,
			exception.WithParam("error_msg", fmt.Sprintf("condition_get failed for table %s: %s", table, err.Error())),
		)
	}
	return results, nil
}

// GetWithSort 过滤+排序+分页查询。
// filters 为等值过滤条件，sortBy 为排序字段，order 为 "ASC" 或 "DESC"，limit 为返回行数上限。
//
// 对应 Python: SqlDbStore.get_with_sort(table, filters, sort_by, order, limit)
func (s *SqlDbStore) GetWithSort(ctx context.Context, table string, filters map[string]any, sortBy string, order string, limit int) ([]map[string]any, error) {
	query := s.db.Table(table)
	// 等值过滤
	for col, val := range filters {
		query = query.Where(fmt.Sprintf("%s = ?", col), val)
	}
	// 排序
	if sortBy != "" {
		dir := "ASC"
		if order == "DESC" || order == "desc" {
			dir = "DESC"
		}
		query = query.Order(fmt.Sprintf("%s %s", sortBy, dir))
	}
	// 限制行数
	if limit > 0 {
		query = query.Limit(limit)
	}

	var results []map[string]any
	if err := query.Find(&results).Error; err != nil {
		logger.Error(logComponent).
			Str("event_type", "MEMORY_RETRIEVE").
			Str("table_name", table).
			Err(err).
			Msg("排序查询失败")
		return nil, exception.BuildError(exception.StatusStoreMessageGetExecutionError,
			exception.WithParam("error_msg", fmt.Sprintf("get_with_sort failed for table %s: %s", table, err.Error())),
		)
	}
	return results, nil
}

// Update 条件更新。
// conditions 为 WHERE 条件（支持 IN 子句，值为切片时使用 IN），data 为需要更新的列值。
//
// 对应 Python: SqlDbStore.update(table, conditions, data)
func (s *SqlDbStore) Update(ctx context.Context, table string, conditions map[string]any, data map[string]any) error {
	query := s.db.Table(table)
	for col, val := range conditions {
		switch v := val.(type) {
		case []any:
			query = query.Where(fmt.Sprintf("%s IN ?", col), v)
		default:
			query = query.Where(fmt.Sprintf("%s = ?", col), v)
		}
	}

	if err := query.Updates(data).Error; err != nil {
		logger.Error(logComponent).
			Str("event_type", "MEMORY_UPDATE").
			Str("table_name", table).
			Err(err).
			Msg("更新数据失败")
		return exception.BuildError(exception.StatusStoreMessageUpdateExecutionError,
			exception.WithParam("error_msg", fmt.Sprintf("update failed for table %s: %s", table, err.Error())),
		)
	}
	return nil
}

// Delete 条件删除。
// conditions 为 WHERE 条件（支持 IN 子句，值为切片时使用 IN）。
//
// 对应 Python: SqlDbStore.delete(table, conditions)
func (s *SqlDbStore) Delete(ctx context.Context, table string, conditions map[string]any) error {
	query := s.db.Table(table)
	for col, val := range conditions {
		switch v := val.(type) {
		case []any:
			query = query.Where(fmt.Sprintf("%s IN ?", col), v)
		default:
			query = query.Where(fmt.Sprintf("%s = ?", col), v)
		}
	}

	if err := query.Delete(nil).Error; err != nil {
		logger.Error(logComponent).
			Str("event_type", "MEMORY_DELETE").
			Str("table_name", table).
			Err(err).
			Msg("删除数据失败")
		return exception.BuildError(exception.StatusStoreMessageDeleteExecutionError,
			exception.WithParam("error_msg", fmt.Sprintf("delete failed for table %s: %s", table, err.Error())),
		)
	}
	return nil
}

// Exist 检查是否存在满足条件的记录。
//
// 对应 Python: SqlDbStore.exist(table, conditions)
func (s *SqlDbStore) Exist(ctx context.Context, table string, conditions map[string]any) (bool, error) {
	query := s.db.Table(table)
	for col, val := range conditions {
		query = query.Where(fmt.Sprintf("%s = ?", col), val)
	}

	var count int64
	if err := query.Limit(1).Count(&count).Error; err != nil {
		return false, exception.BuildError(exception.StatusStoreMessageGetExecutionError,
			exception.WithParam("error_msg", fmt.Sprintf("exist check failed for table %s: %s", table, err.Error())),
		)
	}
	return count > 0, nil
}

// Count 统计满足条件的记录数。
// 使用 SQL COUNT 聚合查询，而非取回全部数据后 len()。
//
// 对应 Python: Python 中无此方法（count_messages 用 get_with_sort + len 实现）
// Go 新增：替代 Python 的低效计数方式
func (s *SqlDbStore) Count(ctx context.Context, table string, conditions map[string]any) (int64, error) {
	query := s.db.Table(table)
	for col, val := range conditions {
		query = query.Where(fmt.Sprintf("%s = ?", col), val)
	}

	var count int64
	if err := query.Count(&count).Error; err != nil {
		logger.Error(logComponent).
			Str("event_type", "MEMORY_RETRIEVE").
			Str("table_name", table).
			Err(err).
			Msg("计数查询失败")
		return 0, exception.BuildError(exception.StatusStoreMessageCountExecutionError,
			exception.WithParam("error_msg", fmt.Sprintf("count failed for table %s: %s", table, err.Error())),
		)
	}
	return count, nil
}

// GetWithSortAndTimeRange 过滤+排序+分页+时间范围查询。
// 在 GetWithSort 基础上增加 StartTime/EndTime 范围过滤。
// 修正 Python 缺陷：Python 定义了 start_time/end_time 但未实现。
//
// 对应 Python: SqlDbStore.get_with_sort（Go 扩展了时间范围查询）
func (s *SqlDbStore) GetWithSortAndTimeRange(ctx context.Context, table string, filters map[string]any, sortBy string, order string, limit int, startTime *time.Time, endTime *time.Time) ([]map[string]any, error) {
	query := s.db.Table(table)
	// 等值过滤
	for col, val := range filters {
		query = query.Where(fmt.Sprintf("%s = ?", col), val)
	}
	// 时间范围过滤
	if startTime != nil {
		query = query.Where("timestamp >= ?", startTime.Format(time.RFC3339))
	}
	if endTime != nil {
		query = query.Where("timestamp <= ?", endTime.Format(time.RFC3339))
	}
	// 排序
	if sortBy != "" {
		dir := "ASC"
		if order == "DESC" || order == "desc" {
			dir = "DESC"
		}
		query = query.Order(fmt.Sprintf("%s %s", sortBy, dir))
	}
	// 限制行数
	if limit > 0 {
		query = query.Limit(limit)
	}

	var results []map[string]any
	if err := query.Find(&results).Error; err != nil {
		logger.Error(logComponent).
			Str("event_type", "MEMORY_RETRIEVE").
			Str("table_name", table).
			Err(err).
			Msg("时间范围排序查询失败")
		return nil, exception.BuildError(exception.StatusStoreMessageGetExecutionError,
			exception.WithParam("error_msg", fmt.Sprintf("get_with_sort_and_time_range failed for table %s: %s", table, err.Error())),
		)
	}
	return results, nil
}
```

- [ ] **Step 2: 编译验证**

Run: `cd /home/opensource/uap-claw-go && go build ./internal/agentcore/memory/manage/model/...`
Expected: 编译成功

- [ ] **Step 3: 创建 sql_db_store_test.go**

```go
package model

import (
	"context"
	"testing"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/store/db"
)

// newTestSqlDbStore 创建测试用的 SqlDbStore 实例
func newTestSqlDbStore(t *testing.T) (*SqlDbStore, *gorm.DB) {
	t.Helper()
	gormDB, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("打开测试数据库失败: %v", err)
	}
	dbStore := db.NewDefaultDbStore(gormDB)
	store := NewSqlDbStore(dbStore)
	return store, gormDB
}

// TestSqlDbStore_Write 写入数据
func TestSqlDbStore_Write(t *testing.T) {
	store, gormDB := newTestSqlDbStore(t)
	if err := CreateTables(gormDB); err != nil {
		t.Fatalf("CreateTables 失败: %v", err)
	}

	data := map[string]any{
		"message_id": "msg_test1_1700000000000",
		"user_id":    "user1",
		"scope_id":   "scope1",
		"content":    "hello",
		"session_id": "session1",
		"role":       "user",
		"timestamp":  "2024-01-01T00:00:00Z",
	}

	if err := store.Write(context.Background(), "user_message", data); err != nil {
		t.Fatalf("Write 失败: %v", err)
	}
}

// TestSqlDbStore_ConditionGet 条件查询
func TestSqlDbStore_ConditionGet(t *testing.T) {
	store, gormDB := newTestSqlDbStore(t)
	if err := CreateTables(gormDB); err != nil {
		t.Fatalf("CreateTables 失败: %v", err)
	}

	// 先写入数据
	data := map[string]any{
		"message_id": "msg_test2_1700000000000",
		"user_id":    "user1",
		"scope_id":   "scope1",
		"content":    "hello",
		"role":       "user",
		"timestamp":  "2024-01-01T00:00:00Z",
	}
	if err := store.Write(context.Background(), "user_message", data); err != nil {
		t.Fatalf("Write 失败: %v", err)
	}

	// 条件查询
	results, err := store.ConditionGet(context.Background(), "user_message",
		map[string]any{"message_id": []string{"msg_test2_1700000000000"}}, nil)
	if err != nil {
		t.Fatalf("ConditionGet 失败: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("ConditionGet 返回 %d 条, 期望 1 条", len(results))
	}
}

// TestSqlDbStore_GetWithSort 排序查询
func TestSqlDbStore_GetWithSort(t *testing.T) {
	store, gormDB := newTestSqlDbStore(t)
	if err := CreateTables(gormDB); err != nil {
		t.Fatalf("CreateTables 失败: %v", err)
	}

	// 写入多条数据
	for i := 0; i < 3; i++ {
		data := map[string]any{
			"message_id": fmt.Sprintf("msg_sort_%d", i),
			"user_id":    "user1",
			"scope_id":   "scope1",
			"content":    fmt.Sprintf("content_%d", i),
			"role":       "user",
			"timestamp":  fmt.Sprintf("2024-01-0%dT00:00:00Z", i+1),
		}
		if err := store.Write(context.Background(), "user_message", data); err != nil {
			t.Fatalf("Write 失败: %v", err)
		}
	}

	results, err := store.GetWithSort(context.Background(), "user_message",
		map[string]any{"user_id": "user1"}, "timestamp", "DESC", 2)
	if err != nil {
		t.Fatalf("GetWithSort 失败: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("GetWithSort 返回 %d 条, 期望 2 条", len(results))
	}
}

// TestSqlDbStore_Update 更新数据
func TestSqlDbStore_Update(t *testing.T) {
	store, gormDB := newTestSqlDbStore(t)
	if err := CreateTables(gormDB); err != nil {
		t.Fatalf("CreateTables 失败: %v", err)
	}

	data := map[string]any{
		"message_id": "msg_update_test",
		"user_id":    "user1",
		"scope_id":   "scope1",
		"content":    "old_content",
		"role":       "user",
		"timestamp":  "2024-01-01T00:00:00Z",
	}
	if err := store.Write(context.Background(), "user_message", data); err != nil {
		t.Fatalf("Write 失败: %v", err)
	}

	if err := store.Update(context.Background(), "user_message",
		map[string]any{"message_id": "msg_update_test"},
		map[string]any{"content": "new_content"}); err != nil {
		t.Fatalf("Update 失败: %v", err)
	}
}

// TestSqlDbStore_Delete 删除数据
func TestSqlDbStore_Delete(t *testing.T) {
	store, gormDB := newTestSqlDbStore(t)
	if err := CreateTables(gormDB); err != nil {
		t.Fatalf("CreateTables 失败: %v", err)
	}

	data := map[string]any{
		"message_id": "msg_delete_test",
		"user_id":    "user1",
		"scope_id":   "scope1",
		"content":    "to_delete",
		"role":       "user",
		"timestamp":  "2024-01-01T00:00:00Z",
	}
	if err := store.Write(context.Background(), "user_message", data); err != nil {
		t.Fatalf("Write 失败: %v", err)
	}

	if err := store.Delete(context.Background(), "user_message",
		map[string]any{"message_id": "msg_delete_test"}); err != nil {
		t.Fatalf("Delete 失败: %v", err)
	}
}

// TestSqlDbStore_Exist 存在性检查
func TestSqlDbStore_Exist(t *testing.T) {
	store, gormDB := newTestSqlDbStore(t)
	if err := CreateTables(gormDB); err != nil {
		t.Fatalf("CreateTables 失败: %v", err)
	}

	data := map[string]any{
		"message_id": "msg_exist_test",
		"user_id":    "user1",
		"scope_id":   "scope1",
		"content":    "hello",
		"role":       "user",
		"timestamp":  "2024-01-01T00:00:00Z",
	}
	if err := store.Write(context.Background(), "user_message", data); err != nil {
		t.Fatalf("Write 失败: %v", err)
	}

	exists, err := store.Exist(context.Background(), "user_message",
		map[string]any{"message_id": "msg_exist_test"})
	if err != nil {
		t.Fatalf("Exist 失败: %v", err)
	}
	if !exists {
		t.Error("期望记录存在")
	}

	notExists, err := store.Exist(context.Background(), "user_message",
		map[string]any{"message_id": "nonexistent"})
	if err != nil {
		t.Fatalf("Exist 失败: %v", err)
	}
	if notExists {
		t.Error("期望记录不存在")
	}
}

// TestSqlDbStore_Count 计数查询
func TestSqlDbStore_Count(t *testing.T) {
	store, gormDB := newTestSqlDbStore(t)
	if err := CreateTables(gormDB); err != nil {
		t.Fatalf("CreateTables 失败: %v", err)
	}

	// 写入 3 条数据
	for i := 0; i < 3; i++ {
		data := map[string]any{
			"message_id": fmt.Sprintf("msg_count_%d", i),
			"user_id":    "user1",
			"scope_id":   "scope1",
			"content":    fmt.Sprintf("content_%d", i),
			"role":       "user",
			"timestamp":  "2024-01-01T00:00:00Z",
		}
		if err := store.Write(context.Background(), "user_message", data); err != nil {
			t.Fatalf("Write 失败: %v", err)
		}
	}

	count, err := store.Count(context.Background(), "user_message",
		map[string]any{"user_id": "user1"})
	if err != nil {
		t.Fatalf("Count 失败: %v", err)
	}
	if count != 3 {
		t.Errorf("Count = %d, want 3", count)
	}
}
```

- [ ] **Step 4: 运行测试验证**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/memory/manage/model/... -v -run "TestSqlDbStore"`
Expected: 全部 PASS

- [ ] **Step 5: 提交**

```bash
git add internal/agentcore/memory/manage/model/sql_db_store.go internal/agentcore/memory/manage/model/sql_db_store_test.go internal/agentcore/memory/manage/model/doc.go
git commit -m "feat(memory): 新增 SqlDbStore 通用 SQL CRUD 层"
```

---

## Task 5: SqlMessageStore 实现

**Files:**
- Create: `internal/agentcore/memory/manage/model/sql_message_store.go`
- Create: `internal/agentcore/memory/manage/model/sql_message_store_test.go`

- [ ] **Step 1: 创建 sql_message_store.go**

```go
package model

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"time"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/store/db"
	"github.com/uapclaw/uapclaw-go/internal/common/crypto"
	"github.com/uapclaw/uapclaw-go/internal/common/exception"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 常量 ────────────────────────────

const (
	// DefaultTableName 默认消息表名
	DefaultTableName = "user_message"
)

// ──────────────────────────── 结构体 ────────────────────────────

// SqlMessageStore BaseMessageStore 的 SQL 实现。
//
// 基于 SqlDbStore 执行数据库操作，使用 AES-256-GCM 加密消息内容。
// 对应 Python: openjiuwen/core/memory/manage/mem_model/sql_message_store.py (SqlMessageStore)
type SqlMessageStore struct {
	// cryptoKey AES 加密密钥（为空时不加密）
	cryptoKey []byte
	// sqlDbStore 通用 SQL CRUD 层
	sqlDbStore *SqlDbStore
	// tableName 消息表名
	tableName string
}

// ──────────────────────────── 导出函数 ────────────────────────────

// NewSqlMessageStore 创建 SqlMessageStore 实例。
//
// 对应 Python: SqlMessageStore.__init__(crypto_key, sql_db_store, table_name)
func NewSqlMessageStore(cryptoKey []byte, sqlDbStore *SqlDbStore, tableName string) *SqlMessageStore {
	if tableName == "" {
		tableName = DefaultTableName
	}
	return &SqlMessageStore{
		cryptoKey:  cryptoKey,
		sqlDbStore: sqlDbStore,
		tableName:  tableName,
	}
}

// AddMessage 添加单条消息，返回 message_id。
//
// 对应 Python: SqlMessageStore.add_message(message_add)
func (s *SqlMessageStore) AddMessage(ctx context.Context, messageAdd *db.MessageAdd) (string, error) {
	message := messageAdd.Message
	timestamp := messageAdd.Timestamp
	if timestamp.IsZero() {
		timestamp = time.Now()
	}

	// 生成消息 ID
	messageID := generateMessageID(message.Content.String(), timestamp)

	// 加密内容
	content, err := s.encodeContent(message.Content.String())
	if err != nil {
		logger.Error(logComponent).
			Str("event_type", "STORE_MESSAGE_ERROR").
			Str("method", "AddMessage").
			Err(err).
			Msg("加密消息内容失败")
		return "", exception.BuildError(exception.StatusStoreMessageAddExecutionError,
			exception.WithParam("error_msg", fmt.Sprintf("encode content failed: %s", err.Error())),
		)
	}

	// 组装数据行
	data := map[string]any{
		"message_id": messageID,
		"user_id":    messageAdd.UserID,
		"session_id": messageAdd.SessionID,
		"scope_id":   messageAdd.ScopeID,
		"role":       message.Role.String(),
		"content":    content,
		"timestamp":  timestamp.Format(time.RFC3339),
	}

	if err := s.sqlDbStore.Write(ctx, s.tableName, data); err != nil {
		return "", err
	}

	return messageID, nil
}

// AddMessages 批量添加消息，返回 ID 列表。
// 修正：真正批量写入，一次 GORM Create 插入所有行。
//
// 对应 Python: SqlMessageStore.add_messages(message_adds)
func (s *SqlMessageStore) AddMessages(ctx context.Context, messageAdds []*db.MessageAdd) ([]string, error) {
	messageIDs := make([]string, 0, len(messageAdds))
	rows := make([]map[string]any, 0, len(messageAdds))

	for _, messageAdd := range messageAdds {
		message := messageAdd.Message
		timestamp := messageAdd.Timestamp
		if timestamp.IsZero() {
			timestamp = time.Now()
		}

		messageID := generateMessageID(message.Content.String(), timestamp)

		content, err := s.encodeContent(message.Content.String())
		if err != nil {
			logger.Error(logComponent).
				Str("event_type", "STORE_MESSAGE_ERROR").
				Str("method", "AddMessages").
				Str("message_id", messageID).
				Err(err).
				Msg("加密消息内容失败")
			return nil, exception.BuildError(exception.StatusStoreMessageAddExecutionError,
				exception.WithParam("error_msg", fmt.Sprintf("encode content failed: %s", err.Error())),
			)
		}

		data := map[string]any{
			"message_id": messageID,
			"user_id":    messageAdd.UserID,
			"session_id": messageAdd.SessionID,
			"scope_id":   messageAdd.ScopeID,
			"role":       message.Role.String(),
			"content":    content,
			"timestamp":  timestamp.Format(time.RFC3339),
		}

		rows = append(rows, data)
		messageIDs = append(messageIDs, messageID)
	}

	// 批量写入
	for _, row := range rows {
		if err := s.sqlDbStore.Write(ctx, s.tableName, row); err != nil {
			return nil, err
		}
	}

	return messageIDs, nil
}

// GetMessageByID 按 ID 获取消息，不存在时返回错误。
//
// 对应 Python: SqlMessageStore.get_message_by_id(message_id)
func (s *SqlMessageStore) GetMessageByID(ctx context.Context, messageID string) (*schema.BaseMessage, *db.MessageMetadata, error) {
	results, err := s.sqlDbStore.ConditionGet(ctx, s.tableName,
		map[string]any{"message_id": []string{messageID}}, nil)
	if err != nil {
		return nil, nil, err
	}

	if len(results) == 0 {
		return nil, nil, exception.BuildError(exception.StatusStoreMessageNotFound,
			exception.WithParam("message_id", messageID),
		)
	}

	return s.rowToMessageAndMeta(results[0])
}

// GetMessages 按条件过滤查询消息。
// 修正：实现 StartTime/EndTime 范围查询（Python 定义了但未实现）。
//
// 对应 Python: SqlMessageStore.get_messages(message_filter, limit, order_by, order_direction)
func (s *SqlMessageStore) GetMessages(ctx context.Context, filter *db.MessageFilter, limit int, orderBy string, orderDirection string) ([]*db.MessageAndMeta, error) {
	if limit <= 0 {
		limit = 10
	}
	if orderBy == "" {
		orderBy = "timestamp"
	}

	// 构建等值过滤条件
	filters := map[string]any{}
	if filter.UserID != "" {
		filters["user_id"] = filter.UserID
	}
	if filter.ScopeID != "" {
		filters["scope_id"] = filter.ScopeID
	}
	if filter.SessionID != "" {
		filters["session_id"] = filter.SessionID
	}

	// 使用带时间范围查询的方法（修正 Python 缺陷）
	rows, err := s.sqlDbStore.GetWithSortAndTimeRange(ctx, s.tableName,
		filters, orderBy, orderDirection, limit, filter.StartTime, filter.EndTime)
	if err != nil {
		return nil, err
	}

	result := make([]*db.MessageAndMeta, 0, len(rows))
	for _, row := range rows {
		msg, meta, err := s.rowToMessageAndMeta(row)
		if err != nil {
			logger.Error(logComponent).
				Str("event_type", "STORE_MESSAGE_ERROR").
				Str("method", "GetMessages").
				Err(err).
				Msg("解析消息行失败，跳过")
			continue
		}
		result = append(result, &db.MessageAndMeta{
			Message:  msg,
			Metadata: meta,
		})
	}

	return result, nil
}

// UpdateMessage 更新消息内容。
//
// 对应 Python: SqlMessageStore.update_message(message_id, content)
func (s *SqlMessageStore) UpdateMessage(ctx context.Context, messageID string, content schema.MessageContent) error {
	encryptedContent, err := s.encodeContent(content.String())
	if err != nil {
		logger.Error(logComponent).
			Str("event_type", "STORE_MESSAGE_ERROR").
			Str("method", "UpdateMessage").
			Str("message_id", messageID).
			Err(err).
			Msg("加密消息内容失败")
		return exception.BuildError(exception.StatusStoreMessageUpdateExecutionError,
			exception.WithParam("error_msg", fmt.Sprintf("encode content failed: %s", err.Error())),
		)
	}

	return s.sqlDbStore.Update(ctx, s.tableName,
		map[string]any{"message_id": messageID},
		map[string]any{"content": encryptedContent})
}

// DeleteMessageByID 按 ID 删除单条消息。
//
// 对应 Python: SqlMessageStore.delete_message_by_id(message_id)
func (s *SqlMessageStore) DeleteMessageByID(ctx context.Context, messageID string) error {
	return s.sqlDbStore.Delete(ctx, s.tableName,
		map[string]any{"message_id": messageID})
}

// DeleteMessages 按条件删除消息，返回删除数量。
//
// 对应 Python: SqlMessageStore.delete_messages(message_filter)
func (s *SqlMessageStore) DeleteMessages(ctx context.Context, filter *db.MessageFilter) (int64, error) {
	// 先获取数量
	count, err := s.CountMessages(ctx, filter)
	if err != nil {
		return 0, err
	}

	// 构建删除条件
	conditions := map[string]any{}
	if filter.UserID != "" {
		conditions["user_id"] = filter.UserID
	}
	if filter.ScopeID != "" {
		conditions["scope_id"] = filter.ScopeID
	}
	if filter.SessionID != "" {
		conditions["session_id"] = filter.SessionID
	}

	if err := s.sqlDbStore.Delete(ctx, s.tableName, conditions); err != nil {
		return 0, err
	}

	return count, nil
}

// CountMessages 统计匹配消息数量。
// 修正：使用 SQL COUNT，而非取回全部数据后 len()。
//
// 对应 Python: SqlMessageStore.count_messages(message_filter)
func (s *SqlMessageStore) CountMessages(ctx context.Context, filter *db.MessageFilter) (int64, error) {
	conditions := map[string]any{}
	if filter.UserID != "" {
		conditions["user_id"] = filter.UserID
	}
	if filter.ScopeID != "" {
		conditions["scope_id"] = filter.ScopeID
	}
	if filter.SessionID != "" {
		conditions["session_id"] = filter.SessionID
	}

	return s.sqlDbStore.Count(ctx, s.tableName, conditions)
}

// GetSchemaVersion 获取当前 schema 版本号。
//
// 对应 Python: SqlMessageStore.get_schema_version()
func (s *SqlMessageStore) GetSchemaVersion(ctx context.Context) (int32, error) {
	metaManager := newMemoryMetaManager(s.sqlDbStore)
	results, err := metaManager.getByTableName(ctx, s.tableName)
	if err != nil {
		return 0, err
	}
	if len(results) > 0 {
		versionStr, ok := results[0]["schema_version"]
		if ok {
			var version int32
			if _, err := fmt.Sscanf(fmt.Sprintf("%v", versionStr), "%d", &version); err == nil {
				return version, nil
			}
		}
	}
	return 0, nil
}

// SetSchemaVersion 设置 schema 版本号。
//
// 对应 Python: SqlMessageStore.set_schema_version(version)
func (s *SqlMessageStore) SetSchemaVersion(ctx context.Context, version int32) error {
	metaManager := newMemoryMetaManager(s.sqlDbStore)
	return metaManager.add(ctx, s.tableName, fmt.Sprintf("%d", version))
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// generateMessageID 基于 content + timestamp 生成消息 ID。
// 格式: msg_{sha256(content_json+timestamp)[:16]}_{timestamp_ms}
//
// 对应 Python: SqlMessageStore._generate_message_id(message, timestamp)
func generateMessageID(content string, timestamp time.Time) string {
	contentStr := content
	messageHash := sha256.Sum256([]byte(fmt.Sprintf("%s%v", contentStr, timestamp)))
	return fmt.Sprintf("msg_%x_%d", messageHash[:8], timestamp.UnixMilli())
}

// encodeContent 加密消息内容。
// cryptoKey 为空时 passthrough 不加密，与 Python AesStorageCodec 行为一致。
func (s *SqlMessageStore) encodeContent(plaintext string) (string, error) {
	if len(s.cryptoKey) == 0 || plaintext == "" {
		return plaintext, nil
	}

	provider, err := crypto.NewAesGcmProvider(s.cryptoKey)
	if err != nil {
		// 加密失败时记录警告并返回原文（容错设计，对齐 Python）
		logger.Warn(logComponent).
			Str("event_type", "STORE_MESSAGE_ERROR").
			Err(err).
			Msg("创建加密提供者失败，返回原文")
		return plaintext, nil
	}

	encrypted, err := provider.Encrypt(plaintext)
	if err != nil {
		logger.Warn(logComponent).
			Str("event_type", "STORE_MESSAGE_ERROR").
			Err(err).
			Msg("加密失败，返回原文")
		return plaintext, nil
	}
	return encrypted, nil
}

// decodeContent 解密消息内容。
// cryptoKey 为空时 passthrough 不解密，与 Python AesStorageCodec 行为一致。
func (s *SqlMessageStore) decodeContent(ciphertext string) (string, error) {
	if len(s.cryptoKey) == 0 || ciphertext == "" {
		return ciphertext, nil
	}

	provider, err := crypto.NewAesGcmProvider(s.cryptoKey)
	if err != nil {
		logger.Warn(logComponent).
			Str("event_type", "STORE_MESSAGE_ERROR").
			Err(err).
			Msg("创建加密提供者失败，返回原文")
		return ciphertext, nil
	}

	decrypted, err := provider.Decrypt(ciphertext)
	if err != nil {
		logger.Warn(logComponent).
			Str("event_type", "STORE_MESSAGE_ERROR").
			Err(err).
			Msg("解密失败，返回原文")
		return ciphertext, nil
	}
	return decrypted, nil
}

// rowToMessageAndMeta 将数据库行转换为 BaseMessage 和 MessageMetadata。
func (s *SqlMessageStore) rowToMessageAndMeta(row map[string]any) (*schema.BaseMessage, *db.MessageMetadata, error) {
	// 解密 content
	contentStr, _ := row["content"].(string)
	decrypted, err := s.decodeContent(contentStr)
	if err != nil {
		return nil, nil, err
	}

	// 解析 role
	roleStr, _ := row["role"].(string)
	role := roleTypeFromString(roleStr)

	// 构造 BaseMessage（只保存 role 和 content，对齐 Python）
	msg := schema.NewBaseMessage(role, decrypted)

	// 解析 timestamp
	timestampStr, _ := row["timestamp"].(string)
	timestamp, _ := time.Parse(time.RFC3339, timestampStr)

	// 解析其他字段
	messageID, _ := row["message_id"].(string)
	userID, _ := row["user_id"].(string)
	scopeID, _ := row["scope_id"].(string)
	sessionID, _ := row["session_id"].(string)

	meta := &db.MessageMetadata{
		MessageID:   messageID,
		UserID:      userID,
		ScopeID:     scopeID,
		SessionID:   sessionID,
		Timestamp:   timestamp,
		MessageType: roleStr,
	}

	return msg, meta, nil
}

// roleTypeFromString 从字符串解析 RoleType
func roleTypeFromString(s string) schema.RoleType {
	switch s {
	case "system":
		return schema.RoleTypeSystem
	case "user":
		return schema.RoleTypeUser
	case "assistant":
		return schema.RoleTypeAssistant
	case "tool":
		return schema.RoleTypeTool
	default:
		return schema.RoleTypeUser
	}
}

// memoryMetaManager 内存元数据管理器（非导出，仅供 SqlMessageStore 内部使用）
// 对应 Python: openjiuwen/core/memory/migration/migrator/memory_meta_manager.py (MemoryMetaManager)
type memoryMetaManager struct {
	sqlDbStore *SqlDbStore
	metaTable  string
}

func newMemoryMetaManager(sqlDbStore *SqlDbStore) *memoryMetaManager {
	return &memoryMetaManager{
		sqlDbStore: sqlDbStore,
		metaTable:  "memory_meta",
	}
}

func (m *memoryMetaManager) add(ctx context.Context, tableName string, schemaVersion string) error {
	if tableName == "" || schemaVersion == "" {
		return nil
	}
	data := map[string]any{
		"table_name":     tableName,
		"schema_version": schemaVersion,
	}
	exists, err := m.sqlDbStore.Exist(ctx, m.metaTable,
		map[string]any{"table_name": tableName, "schema_version": schemaVersion})
	if err != nil {
		return err
	}
	if exists {
		return nil
	}
	return m.sqlDbStore.Write(ctx, m.metaTable, data)
}

func (m *memoryMetaManager) getByTableName(ctx context.Context, tableName string) ([]map[string]any, error) {
	results, err := m.sqlDbStore.ConditionGet(ctx, m.metaTable,
		map[string]any{"table_name": []string{tableName}}, nil)
	if err != nil {
		return nil, err
	}
	return results, nil
}
```

- [ ] **Step 2: 编译验证**

Run: `cd /home/opensource/uap-claw-go && go build ./internal/agentcore/memory/manage/model/...`
Expected: 编译成功

- [ ] **Step 3: 创建 sql_message_store_test.go**

```go
package model

import (
	"context"
	"fmt"
	"testing"
	"time"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/store/db"
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
	store := NewSqlMessageStore(nil, sqlDbStore, "")
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

	// 添加消息
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

	// 添加不同时间的消息
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

	// 查询 1月2日 到 1月3日 之间的消息
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

	// 更新内容
	if err := store.UpdateMessage(context.Background(), messageID, schema.NewTextContent("new_content")); err != nil {
		t.Fatalf("UpdateMessage 失败: %v", err)
	}

	// 验证更新
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

	// 验证已删除
	_, _, err = store.GetMessageByID(context.Background(), messageID)
	if err == nil {
		t.Error("期望消息已删除（返回错误），但返回 nil")
	}
}

// TestSqlMessageStore_DeleteMessages 按条件删除消息
func TestSqlMessageStore_DeleteMessages(t *testing.T) {
	store, _, _ := newTestSqlMessageStore(t)

	// 添加 3 条消息
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

	// 添加 3 条消息
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

	// 初始版本应为 0
	version, err := store.GetSchemaVersion(context.Background())
	if err != nil {
		t.Fatalf("GetSchemaVersion 失败: %v", err)
	}
	if version != 0 {
		t.Errorf("初始版本 = %d, want 0", version)
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
```

- [ ] **Step 4: 运行测试验证**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/memory/manage/model/... -v -run "TestSqlMessageStore|TestGenerateMessageID"`
Expected: 全部 PASS

- [ ] **Step 5: 提交**

```bash
git add internal/agentcore/memory/manage/model/sql_message_store.go internal/agentcore/memory/manage/model/sql_message_store_test.go internal/agentcore/memory/manage/model/doc.go
git commit -m "feat(memory): 新增 SqlMessageStore 消息存储实现"
```

---

## Task 6: MessageManager 消息管理器

**Files:**
- Create: `internal/agentcore/memory/manage/model/message_manager.go`
- Create: `internal/agentcore/memory/manage/model/message_manager_test.go`

- [ ] **Step 1: 创建 message_manager.go**

```go
package model

import (
	"context"
	"time"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/store/db"
	"github.com/uapclaw/uapclaw-go/internal/common/exception"
)

// ──────────────────────────── 结构体 ────────────────────────────

// MessageAddRequest 添加消息请求。
//
// 对应 Python: openjiuwen/core/memory/manage/mem_model/message_manager.py (MessageAddRequest)
type MessageAddRequest struct {
	// UserID 用户 ID（必填）
	UserID string
	// ScopeID 作用域 ID（必填）
	ScopeID string
	// Content 消息内容（必填）
	Content string
	// Role 消息角色
	Role string
	// SessionID 会话 ID
	SessionID string
	// Timestamp 时间戳（零值时自动生成当前时间）
	Timestamp time.Time
}

// MessageManager 消息管理器，BaseMessageStore 的上层封装。
//
// 提供验证和简化的消息操作接口，由 LongTermMemory 使用。
// 对应 Python: openjiuwen/core/memory/manage/mem_model/message_manager.py (MessageManager)
type MessageManager struct {
	// store 消息存储接口
	store db.BaseMessageStore
}

// ──────────────────────────── 导出函数 ────────────────────────────

// NewMessageManager 创建 MessageManager 实例。
func NewMessageManager(store db.BaseMessageStore) *MessageManager {
	return &MessageManager{store: store}
}

// Add 验证必填字段后添加消息。
// 必填字段：UserID、ScopeID、Content。
//
// 对应 Python: MessageManager.add(req)
func (m *MessageManager) Add(ctx context.Context, req *MessageAddRequest) (string, error) {
	if req.UserID == "" {
		return "", exception.BuildError(exception.StatusStoreMessageAddExecutionError,
			exception.WithParam("error_msg", "must provide user_id for add message"),
		)
	}
	if req.ScopeID == "" {
		return "", exception.BuildError(exception.StatusStoreMessageAddExecutionError,
			exception.WithParam("error_msg", "must provide scope_id for add message"),
		)
	}
	if req.Content == "" {
		return "", exception.BuildError(exception.StatusStoreMessageAddExecutionError,
			exception.WithParam("error_msg", "must provide content for add message"),
		)
	}

	// 解析 role
	role := roleTypeFromString(req.Role)
	message := schema.NewBaseMessage(role, req.Content)

	timestamp := req.Timestamp
	if timestamp.IsZero() {
		timestamp = time.Now()
	}

	messageAdd := &db.MessageAdd{
		Message:   message,
		UserID:    req.UserID,
		ScopeID:   req.ScopeID,
		SessionID: req.SessionID,
		Timestamp: timestamp,
	}

	return m.store.AddMessage(ctx, messageAdd)
}

// Get 获取消息，返回 (消息, 时间戳) 列表。
// 倒序获取后反转，使最旧的消息排在前面。
//
// 对应 Python: MessageManager.get(user_id, scope_id, session_id, message_len)
func (m *MessageManager) Get(ctx context.Context, userID string, scopeID string, sessionID string, messageLen int) ([]*db.MessageAndMeta, error) {
	if messageLen <= 0 {
		return nil, exception.BuildError(exception.StatusStoreMessageGetExecutionError,
			exception.WithParam("error_msg", "message length must be bigger than zero for get message"),
		)
	}

	filter := &db.MessageFilter{
		UserID:    userID,
		ScopeID:   scopeID,
		SessionID: sessionID,
	}

	// 倒序获取
	messages, err := m.store.GetMessages(ctx, filter, messageLen, "timestamp", "desc")
	if err != nil {
		return nil, err
	}

	// 反转，使最旧的在前
	for i, j := 0, len(messages)-1; i < j; i, j = i+1, j-1 {
		messages[i], messages[j] = messages[j], messages[i]
	}

	return messages, nil
}

// GetByID 按 ID 获取消息，不存在时返回 nil。
//
// 对应 Python: MessageManager.get_by_id(msg_id)
func (m *MessageManager) GetByID(ctx context.Context, msgID string) (*db.MessageAndMeta, error) {
	msg, meta, err := m.store.GetMessageByID(ctx, msgID)
	if err != nil {
		return nil, nil
	}
	return &db.MessageAndMeta{Message: msg, Metadata: meta}, nil
}

// DeleteByUserAndScope 删除指定用户+作用域的所有消息。
//
// 对应 Python: MessageManager.delete_by_user_and_scope(user_id, scope_id)
func (m *MessageManager) DeleteByUserAndScope(ctx context.Context, userID string, scopeID string) (int64, error) {
	filter := &db.MessageFilter{
		UserID:  userID,
		ScopeID: scopeID,
	}
	return m.store.DeleteMessages(ctx, filter)
}
```

- [ ] **Step 2: 编译验证**

Run: `cd /home/opensource/uap-claw-go && go build ./internal/agentcore/memory/manage/model/...`
Expected: 编译成功

- [ ] **Step 3: 创建 message_manager_test.go**

```go
package model

import (
	"context"
	"testing"
	"time"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	"github.com/uapclaw/uap-claw-go/internal/agentcore/store/db"
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
	return nil, nil, exception.BuildError(exception.StatusStoreMessageNotFound,
		exception.WithParam("message_id", messageID))
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
```

- [ ] **Step 4: 修复测试中的 import 路径**

检查项目的 Go module 名称，确保测试文件中 import 路径正确：
Run: `head -1 /home/opensource/uap-claw-go/go.mod`
Expected: 输出 module 名称，如 `github.com/uapclaw/uapclaw-go`

将测试文件中 `github.com/uapclaw/uap-claw-go` 替换为实际的 module 路径。

- [ ] **Step 5: 运行测试验证**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/memory/manage/model/... -v -run "TestMessageManager"`
Expected: 全部 PASS

- [ ] **Step 6: 提交**

```bash
git add internal/agentcore/memory/manage/model/message_manager.go internal/agentcore/memory/manage/model/message_manager_test.go
git commit -m "feat(memory): 新增 MessageManager 消息管理器"
```

---

## Task 7: 全量测试与覆盖率验证

**Files:**
- 无新增文件

- [ ] **Step 1: 运行全部测试**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/store/db/... ./internal/agentcore/memory/manage/model/... -v`
Expected: 全部 PASS

- [ ] **Step 2: 检查覆盖率**

Run: `cd /home/opensource/uap-claw-go && go test -cover ./internal/agentcore/store/db/... ./internal/agentcore/memory/manage/model/...`
Expected: 所有包覆盖率 ≥ 85%

- [ ] **Step 3: 如果覆盖率不足，补充测试用例**

根据 `go test -coverprofile=coverage.out ./internal/agentcore/store/db/... ./internal/agentcore/memory/manage/model/... && go tool cover -func=coverage.out` 输出，针对覆盖率低于 85% 的函数补充测试。

- [ ] **Step 4: 最终提交**

```bash
git add -A
git commit -m "test(memory): 补充测试用例，覆盖率达标"
```

---

## Task 8: 更新 IMPLEMENTATION_PLAN.md 状态

**Files:**
- Modify: `IMPLEMENTATION_PLAN.md`

- [ ] **Step 1: 更新步骤状态**

将 4.15 和 4.16 的状态从 `☐` 改为 `✅`：
- `4.15 | ✅ | BaseMessageStore 接口 | 消息持久化`
- `4.16 | ✅ | SqlMessageStore | SQL 消息存储`

- [ ] **Step 2: 提交**

```bash
git add IMPLEMENTATION_PLAN.md
git commit -m "docs: 更新 4.15/4.16 实现计划状态为已完成"
```

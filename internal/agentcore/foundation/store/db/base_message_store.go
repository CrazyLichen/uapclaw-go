package db

import (
	"context"
	"time"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
)

// ──────────────────────────── 结构体 ────────────────────────────

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
	GetMessageByID(ctx context.Context, messageID string) (schema.BaseMessage, *MessageMetadata, error)

	// GetMessages 按条件过滤查询消息。
	// limit 为 0 时使用默认值 DefaultMessageLimit，orderBy 为空时使用 DefaultMessageOrderBy，
	// orderDirection 为空时使用 DefaultMessageOrderDirection。
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
	// 返回 -1 表示版本未设置（对齐 Python 返回 None 的语义），
	// 0 表示无迁移操作，1+ 表示实际版本号。
	//
	// 对应 Python: BaseMessageStore.get_schema_version() -> int | None
	GetSchemaVersion(ctx context.Context) (int32, error)

	// SetSchemaVersion 设置 schema 版本号。
	//
	// 对应 Python: BaseMessageStore.set_schema_version(version)
	SetSchemaVersion(ctx context.Context, version int32) error
}

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
	Message schema.BaseMessage
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
// 修正：实现 StartTime/EndTime 过滤。
// MessageType：Python 接口定义了此字段，但实际 SQL 表无对应列，暂未实现过滤。
// 如果未来数据库表增加 message_type 列，需要补回过滤逻辑。
// SessionID 使用 *string 指针类型：nil 表示查找 NULL 值，空字符串表示不加过滤，非空表示精确匹配。
type MessageFilter struct {
	// UserID 用户 ID
	UserID string
	// ScopeID 作用域 ID
	ScopeID string
	// SessionID 会话 ID（nil 表示不加过滤，空字符串指针表示查找 IS NULL，非空指针表示精确匹配）
	// 对齐 Python 行为差异：Python session_id=None 时走 WHERE session_id IS NULL，
	// Go 中需要显式设置 &"" 来表达 IS NULL 语义，nil 表示不加过滤。
	SessionID *string
	// MessageType 消息类型（暂未实现过滤，Python 接口定义了此字段但 SQL 表无对应列）
	MessageType string
	// StartTime 起始时间（nil 表示不限制）
	StartTime *time.Time
	// EndTime 结束时间（nil 表示不限制）
	EndTime *time.Time
}

// MessageAndMeta 消息+元数据组合（用于 GetMessages 返回）。
type MessageAndMeta struct {
	// Message 消息对象
	Message schema.BaseMessage
	// Metadata 消息元数据
	Metadata *MessageMetadata
}

// ──────────────────────────── 常量 ────────────────────────────

const (
	// DefaultMessageLimit GetMessages 默认返回条数，对齐 Python limit=10
	DefaultMessageLimit = 10
	// DefaultMessageOrderBy GetMessages 默认排序字段，对齐 Python order_by="timestamp"
	DefaultMessageOrderBy = "timestamp"
	// DefaultMessageOrderDirection GetMessages 默认排序方向，对齐 Python order_direction="desc"
	DefaultMessageOrderDirection = "desc"
)

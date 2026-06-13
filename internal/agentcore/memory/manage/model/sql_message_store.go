package model

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"time"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	storedb "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/store/db"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/memory/codec"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/memory/migration/migrator"
	"github.com/uapclaw/uapclaw-go/internal/common/exception"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// SqlMessageStore BaseMessageStore 的 SQL 实现。
//
// 基于 SqlDbStore 执行数据库操作，使用 AesStorageCodec 加密消息内容。
// 容错模式：key 为空时 passthrough 不加密，key 非空时加解密失败返回原文（对齐 Python 行为）。
// 对应 Python: openjiuwen/core/memory/manage/mem_model/sql_message_store.py (SqlMessageStore)
type SqlMessageStore struct {
	// codec AES 存储编解码器
	codec *codec.AesStorageCodec
	// sqlDbStore 通用 SQL CRUD 层
	sqlDbStore *SqlDbStore
	// tableName 消息表名
	tableName string
	// metaMgr schema 版本管理器
	metaMgr *migrator.MemoryMetaManager
}

// ──────────────────────────── 常量 ────────────────────────────

const (
	// DefaultTableName 默认消息表名
	DefaultTableName = "user_message"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// NewSqlMessageStore 创建 SqlMessageStore 实例。
// cryptoKey 为空时 passthrough 模式，非空时必须为 32 字节。
//
// 对应 Python: SqlMessageStore.__init__(crypto_key, sql_db_store, table_name)
func NewSqlMessageStore(cryptoKey []byte, sqlDbStore *SqlDbStore, tableName string) (*SqlMessageStore, error) {
	if tableName == "" {
		tableName = DefaultTableName
	}

	c, err := codec.NewAesStorageCodec(cryptoKey)
	if err != nil {
		return nil, exception.BuildError(exception.StatusStoreMessageAddExecutionError,
			exception.WithParam("error_msg", fmt.Sprintf("create codec failed: %s", err.Error())),
		)
	}

	return &SqlMessageStore{
		codec:      c,
		sqlDbStore: sqlDbStore,
		tableName:  tableName,
		metaMgr:    migrator.NewMemoryMetaManager(sqlDbStore),
	}, nil
}

// AddMessage 添加单条消息，返回 message_id。
//
// 对应 Python: SqlMessageStore.add_message(message_add)
func (s *SqlMessageStore) AddMessage(ctx context.Context, messageAdd *storedb.MessageAdd) (string, error) {
	message := messageAdd.Message
	timestamp := messageAdd.Timestamp
	if timestamp.IsZero() {
		timestamp = time.Now()
	}

	// 序列化并生成消息 ID
	contentStr, err := marshalContent(message.Content)
	if err != nil {
		return "", err
	}
	messageID := generateMessageID(contentStr, timestamp)

	// 加密内容（容错模式：加密失败时返回原文，与 Python 对齐）
	encrypted, _ := s.codec.Encode(contentStr)

	// 组装数据行
	data := map[string]any{
		"message_id": messageID,
		"user_id":    messageAdd.UserID,
		"session_id": messageAdd.SessionID,
		"scope_id":   messageAdd.ScopeID,
		"role":       message.Role.String(),
		"content":    encrypted,
		"timestamp":  timestamp.Format(time.RFC3339),
	}

	if err := s.sqlDbStore.Write(ctx, s.tableName, data); err != nil {
		return "", err
	}

	return messageID, nil
}

// AddMessages 批量添加消息，返回 ID 列表。
// 使用 CreateBatch 一次 GORM Create 插入所有行（一个事务）。
//
// 注意：Python 逐条调用 add_message（每条独立事务），Go 使用批量事务。
// Go 方式更优（原子性更好、性能更高）：要么全写入要么全不写。
// Python 逐条方式可能导致部分成功部分失败，Go 不存在此问题。
//
// 对应 Python: SqlMessageStore.add_messages(message_adds)
func (s *SqlMessageStore) AddMessages(ctx context.Context, messageAdds []*storedb.MessageAdd) ([]string, error) {
	messageIDs := make([]string, 0, len(messageAdds))
	rows := make([]map[string]any, 0, len(messageAdds))

	for _, messageAdd := range messageAdds {
		message := messageAdd.Message
		timestamp := messageAdd.Timestamp
		if timestamp.IsZero() {
			timestamp = time.Now()
		}

		// 序列化并生成消息 ID
		contentStr, err := marshalContent(message.Content)
		if err != nil {
			return nil, err
		}
		messageID := generateMessageID(contentStr, timestamp)

		// 加密内容（容错模式：加密失败时返回原文，与 Python 对齐）
		encrypted, _ := s.codec.Encode(contentStr)

		data := map[string]any{
			"message_id": messageID,
			"user_id":    messageAdd.UserID,
			"session_id": messageAdd.SessionID,
			"scope_id":   messageAdd.ScopeID,
			"role":       message.Role.String(),
			"content":    encrypted,
			"timestamp":  timestamp.Format(time.RFC3339),
		}

		rows = append(rows, data)
		messageIDs = append(messageIDs, messageID)
	}

	// 批量写入
	if err := s.sqlDbStore.CreateBatch(ctx, s.tableName, rows); err != nil {
		return nil, err
	}

	return messageIDs, nil
}

// GetMessageByID 按 ID 获取消息，不存在时返回错误。
//
// 对应 Python: SqlMessageStore.get_message_by_id(message_id)
func (s *SqlMessageStore) GetMessageByID(ctx context.Context, messageID string) (*schema.BaseMessage, *storedb.MessageMetadata, error) {
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
// 实现 StartTime/EndTime 范围查询（Python 定义了但未实现）。
//
// 对应 Python: SqlMessageStore.get_messages(message_filter, limit, order_by, order_direction)
func (s *SqlMessageStore) GetMessages(ctx context.Context, filter *storedb.MessageFilter, limit int, orderBy string, orderDirection string) ([]*storedb.MessageAndMeta, error) {
	if limit <= 0 {
		limit = storedb.DefaultMessageLimit
	}
	if orderBy == "" {
		orderBy = storedb.DefaultMessageOrderBy
	}
	if orderDirection == "" {
		orderDirection = storedb.DefaultMessageOrderDirection
	}

	// 构建等值过滤条件
	filters := map[string]any{}
	if filter.UserID != "" {
		filters["user_id"] = filter.UserID
	}
	if filter.ScopeID != "" {
		filters["scope_id"] = filter.ScopeID
	}
	// SessionID：nil 不加过滤，&"" 表示查找 IS NULL，&"value" 表示精确匹配
	if filter.SessionID != nil {
		if *filter.SessionID == "" {
			// 空字符串指针 → 查找 session_id IS NULL（对齐 Python session_id=None）
			filters["session_id"] = nil
		} else {
			filters["session_id"] = *filter.SessionID
		}
	}
	// filter.SessionID == nil → 不加过滤

	// 使用带时间范围查询的方法
	rows, err := s.sqlDbStore.GetWithSortAndTimeRange(ctx, s.tableName,
		filters, orderBy, orderDirection, limit, filter.StartTime, filter.EndTime)
	if err != nil {
		return nil, err
	}

	result := make([]*storedb.MessageAndMeta, 0, len(rows))
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
		result = append(result, &storedb.MessageAndMeta{
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
	contentStr, err := marshalContent(content)
	if err != nil {
		return err
	}

	// 加密内容（容错模式：加密失败时返回原文，与 Python 对齐）
	encrypted, _ := s.codec.Encode(contentStr)

	return s.sqlDbStore.Update(ctx, s.tableName,
		map[string]any{"message_id": messageID},
		map[string]any{"content": encrypted})
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
func (s *SqlMessageStore) DeleteMessages(ctx context.Context, filter *storedb.MessageFilter) (int64, error) {
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
	// SessionID：nil 不加过滤，&"" 表示查找 IS NULL，&"value" 表示精确匹配
	if filter.SessionID != nil {
		if *filter.SessionID == "" {
			conditions["session_id"] = nil
		} else {
			conditions["session_id"] = *filter.SessionID
		}
	}

	if err := s.sqlDbStore.Delete(ctx, s.tableName, conditions); err != nil {
		return 0, err
	}

	return count, nil
}

// CountMessages 统计匹配消息数量。
// 修正 Python 缺陷：支持 StartTime/EndTime 时间范围过滤（Python 定义了但未实现）。
//
// 对应 Python: SqlMessageStore.count_messages(message_filter)
func (s *SqlMessageStore) CountMessages(ctx context.Context, filter *storedb.MessageFilter) (int64, error) {
	conditions := map[string]any{}
	if filter.UserID != "" {
		conditions["user_id"] = filter.UserID
	}
	if filter.ScopeID != "" {
		conditions["scope_id"] = filter.ScopeID
	}
	// SessionID：nil 不加过滤，&"" 表示查找 IS NULL，&"value" 表示精确匹配
	if filter.SessionID != nil {
		if *filter.SessionID == "" {
			conditions["session_id"] = nil
		} else {
			conditions["session_id"] = *filter.SessionID
		}
	}

	// 有时间范围条件时走 CountWithTimeRange，否则走普通 Count
	if filter.StartTime != nil || filter.EndTime != nil {
		return s.sqlDbStore.CountWithTimeRange(ctx, s.tableName, conditions, filter.StartTime, filter.EndTime)
	}
	return s.sqlDbStore.Count(ctx, s.tableName, conditions)
}

// GetSchemaVersion 获取当前 schema 版本号。
// 返回 -1 表示版本未设置（对齐 Python 返回 None 的语义），
// 0 表示无迁移操作，1+ 表示实际版本号。
//
// 对应 Python: SqlMessageStore.get_schema_version() -> int | None
func (s *SqlMessageStore) GetSchemaVersion(ctx context.Context) (int32, error) {
	results, err := s.metaMgr.GetByTableName(ctx, s.tableName)
	if err != nil {
		return -1, err
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
	return -1, nil
}

// SetSchemaVersion 设置 schema 版本号。
//
// 对应 Python: SqlMessageStore.set_schema_version(version)
func (s *SqlMessageStore) SetSchemaVersion(ctx context.Context, version int32) error {
	return s.metaMgr.Add(ctx, s.tableName, fmt.Sprintf("%d", version))
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// generateMessageID 基于 content + timestamp 生成消息 ID。
// 格式: msg_{sha256(content_json+timestamp)[:16]}_{timestamp_ms}
//
// 对应 Python: SqlMessageStore._generate_message_id(message, timestamp)
// 时间格式使用 "2006-01-02 15:04:05-07:00" 与 Python 的 timestamp.__str__() 对齐，
// 确保相同数据在 Go/Python 生成相同的 message_id。
func generateMessageID(content string, timestamp time.Time) string {
	messageHash := sha256.Sum256([]byte(fmt.Sprintf("%s%s", content, timestamp.Format("2006-01-02 15:04:05-07:00"))))
	return fmt.Sprintf("msg_%x_%d", messageHash[:8], timestamp.UnixMilli())
}

// marshalContent 将 MessageContent 序列化为 JSON 字符串。
// 纯文本 → "hello"（JSON string），多模态 → [{"type":"text",...}]（JSON array）。
func marshalContent(content schema.MessageContent) (string, error) {
	bytes, err := json.Marshal(content)
	if err != nil {
		return "", exception.BuildError(exception.StatusStoreMessageAddExecutionError,
			exception.WithParam("error_msg", fmt.Sprintf("marshal content failed: %s", err.Error())),
		)
	}
	return string(bytes), nil
}

// unmarshalContent 将 JSON 字符串反序列化为 MessageContent。
// 兼容旧数据：纯文本字符串也可被 MessageContent.UnmarshalJSON 处理。
func unmarshalContent(data string) (schema.MessageContent, error) {
	var content schema.MessageContent
	if err := json.Unmarshal([]byte(data), &content); err != nil {
		return schema.MessageContent{}, exception.BuildError(exception.StatusStoreMessageGetExecutionError,
			exception.WithParam("error_msg", fmt.Sprintf("unmarshal content failed: %s", err.Error())),
		)
	}
	return content, nil
}

// rowToMessageAndMeta 将数据库行转换为 BaseMessage 和 MessageMetadata。
// 使用安全类型断言，断言失败时返回 error。
func (s *SqlMessageStore) rowToMessageAndMeta(row map[string]any) (*schema.BaseMessage, *storedb.MessageMetadata, error) {
	// 安全类型断言辅助
	getStr := func(key string) (string, error) {
		v, ok := row[key]
		if !ok {
			return "", fmt.Errorf("行数据缺少字段 %q", key)
		}
		s, ok := v.(string)
		if !ok {
			return "", fmt.Errorf("字段 %q 类型错误: 期望 string, 实际 %T", key, v)
		}
		return s, nil
	}

	// 解密 content（容错模式：解密失败时返回原文，与 Python 对齐）
	contentStr, err := getStr("content")
	if err != nil {
		return nil, nil, err
	}
	decrypted, _ := s.codec.Decode(contentStr)

	// 反序列化 content
	content, err := unmarshalContent(decrypted)
	if err != nil {
		return nil, nil, err
	}

	// 解析 role
	roleStr, err := getStr("role")
	if err != nil {
		return nil, nil, err
	}
	role := roleTypeFromString(roleStr)

	// 构造 BaseMessage
	msg := &schema.BaseMessage{
		Role:    role,
		Content: content,
	}

	// 解析 timestamp
	timestampStr, err := getStr("timestamp")
	if err != nil {
		return nil, nil, err
	}
	timestamp, parseErr := time.Parse(time.RFC3339, timestampStr)
	if parseErr != nil {
		logger.Warn(logComponent).Err(parseErr).
			Str("timestamp_str", timestampStr).
			Msg("解析 timestamp 失败，使用零值")
	}

	// 解析其他字段
	messageID, err := getStr("message_id")
	if err != nil {
		return nil, nil, err
	}
	userID, err := getStr("user_id")
	if err != nil {
		return nil, nil, err
	}
	scopeID, err := getStr("scope_id")
	if err != nil {
		return nil, nil, err
	}
	sessionID, _ := getStr("session_id") // session_id 允许缺失

	meta := &storedb.MessageMetadata{
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

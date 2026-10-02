package migrator

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/store/db"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/memory/migration/operation"
	"github.com/uapclaw/uapclaw-go/internal/common/exception"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// MessageMigrator 消息存储迁移执行器。
// 版本跟踪通过 BaseMessageStore.GetSchemaVersion/SetSchemaVersion。
// 迁移前创建备份，失败时从备份恢复。
//
// Python: openjiuwen/core/memory/migration/migrator/message_migrator.py (MessageMigrator)
type MessageMigrator struct {
	// messageStore 消息存储实例
	messageStore db.BaseMessageStore
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

const (
	// MessageEntityKey 消息迁移实体键
	// Python: MESSAGE_ENTITY_KEY = "message_global"
	MessageEntityKey = "message_global"
	// backupPageSize 备份分页大小
	// Python: _BACKUP_PAGE_SIZE = 1000
	backupPageSize = 1000
)

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// NewMessageMigrator 创建 MessageMigrator 实例。
//
// Python: MessageMigrator(message_store)
func NewMessageMigrator(messageStore db.BaseMessageStore) *MessageMigrator {
	return &MessageMigrator{messageStore: messageStore}
}

// TryMigrate 尝试执行消息存储迁移。
// 校验 entityKey + 版本比较 + 备份+执行+恢复。
//
// Python: MessageMigrator.try_migrate(entity_key, operations)
func (m *MessageMigrator) TryMigrate(ctx context.Context, entityKey string, operations []operation.Operation) error {
	if entityKey != MessageEntityKey {
		logger.Error(logComponent).Str("entity_key", entityKey).Str("expected_key", MessageEntityKey).
			Msg("不支持的 entity_key")
		return exception.BuildError(exception.StatusMemoryMigrateMemoryExecutionError,
			exception.WithParam("entity_key", entityKey),
			exception.WithParam("error_msg", fmt.Sprintf("不支持的 entity_key: '%s', 期望: '%s'", entityKey, MessageEntityKey)),
		)
	}

	if len(operations) == 0 {
		return nil
	}

	if !validateOperationsOrder(operations) {
		logger.Error(logComponent).Msg("操作的 schema_version 不是升序排列")
		return exception.BuildError(exception.StatusMemoryMigrateMemoryExecutionError,
			exception.WithParam("error_msg", "操作的 schema_version 不是升序排列"),
		)
	}

	currentVersion, err := m.messageStore.GetSchemaVersion(ctx)
	if err != nil {
		return err
	}

	// -1 表示版本未设置（对齐 Python 返回 None）
	var currentVersionPtr *int
	if currentVersion >= 0 {
		v := int(currentVersion)
		currentVersionPtr = &v
	}

	lastOpVersion := operations[len(operations)-1].SchemaVersion()
	if currentVersionPtr != nil && *currentVersionPtr >= lastOpVersion {
		logger.Info(logComponent).Int("current_version", *currentVersionPtr).Int("last_op_version", lastOpVersion).
			Msg("当前版本已 >= 最新操作版本，无需迁移")
		return nil
	}

	pendingOps := filterPendingOperations(operations, currentVersionPtr)
	if len(pendingOps) == 0 {
		return nil
	}

	logger.Info(logComponent).Int("pending_count", len(pendingOps)).Msg("发现待执行操作")

	backupData, err := m.createBackup(ctx)
	if err != nil {
		logger.Error(logComponent).Err(err).Msg("创建消息备份失败")
		return err
	}

	lastVersion := currentVersionPtr
	for idx, op := range pendingOps {
		logger.Info(logComponent).Int("idx", idx+1).Int("total", len(pendingOps)).
			Str("op_type", op.TypeName()).Int("schema_version", op.SchemaVersion()).
			Msg("执行操作")

		if err := m.executeOperation(ctx, op); err != nil {
			logger.Error(logComponent).Err(err).Str("op_type", op.TypeName()).
				Int("schema_version", op.SchemaVersion()).Msg("操作执行失败")

			if restoreErr := m.restoreFromBackup(ctx, backupData, currentVersionPtr); restoreErr != nil {
				logger.Error(logComponent).Err(restoreErr).Msg("从备份恢复失败")
			}
			return err
		}

		v := op.SchemaVersion()
		lastVersion = &v
		logger.Info(logComponent).Str("op_type", op.TypeName()).Int("schema_version", op.SchemaVersion()).
			Msg("操作执行成功")
	}

	if lastVersion != nil && (currentVersionPtr == nil || *lastVersion != *currentVersionPtr) {
		if err := m.messageStore.SetSchemaVersion(ctx, int32(*lastVersion)); err != nil {
			return err
		}
		logger.Info(logComponent).Int("new_version", *lastVersion).Msg("消息 schema 版本已更新")
	}

	return nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// executeOperation 执行单个迁移操作。
//
// Python: MessageMigrator._execute_operation
func (m *MessageMigrator) executeOperation(ctx context.Context, op operation.Operation) error {
	switch o := op.(type) {
	case *operation.UpdateMessageOperation:
		return o.UpdateFunc(ctx, m.messageStore)
	default:
		return exception.BuildError(exception.StatusMemoryMigrateMemoryExecutionError,
			exception.WithParam("op_type", op.TypeName()),
			exception.WithParam("error_msg", fmt.Sprintf("不支持的操作类型: %s", op.TypeName())),
		)
	}
}

// messageBackupRecord 备份记录，用于序列化/反序列化
type messageBackupRecord struct {
	// MessageContent 消息内容
	MessageContent string `json:"message_content"`
	// MessageRole 消息角色
	MessageRole string `json:"message_role"`
	// UserID 用户 ID
	UserID string `json:"user_id"`
	// ScopeID 作用域 ID
	ScopeID string `json:"scope_id"`
	// SessionID 会话 ID
	SessionID string `json:"session_id"`
	// Timestamp 时间戳
	Timestamp string `json:"timestamp"`
}

// createBackup 创建消息备份。
//
// Python: MessageMigrator._create_backup
func (m *MessageMigrator) createBackup(ctx context.Context) ([]messageBackupRecord, error) {
	var backupData []messageBackupRecord

	messages, err := m.messageStore.GetMessages(ctx, nil, backupPageSize, "timestamp", "asc")
	if err != nil {
		return nil, err
	}

	for _, msg := range messages {
		record := messageBackupRecord{
			MessageContent: msg.Message.GetContent().String(),
			MessageRole:    msg.Message.GetRole().String(),
		}
		if msg.Metadata != nil {
			record.UserID = msg.Metadata.UserID
			record.ScopeID = msg.Metadata.ScopeID
			record.SessionID = msg.Metadata.SessionID
			record.Timestamp = msg.Metadata.Timestamp.Format("2006-01-02T15:04:05Z07:00")
		}
		backupData = append(backupData, record)
	}

	logger.Info(logComponent).Int("record_count", len(backupData)).Msg("已创建消息备份")
	return backupData, nil
}

// restoreFromBackup 从备份恢复消息。
//
// Python: MessageMigrator._restore_from_backup
func (m *MessageMigrator) restoreFromBackup(ctx context.Context, backupData []messageBackupRecord, preMigrationVersion *int) error {
	// 删除所有消息
	if _, err := m.messageStore.DeleteMessages(ctx, nil); err != nil {
		logger.Error(logComponent).Err(err).Msg("删除消息失败")
		return err
	}

	// 重新插入备份数据
	for _, record := range backupData {
		// 根据 role 构造对应的消息类型
		var msg schema.BaseMessage
		switch record.MessageRole {
		case "user":
			msg = schema.NewUserMessage(record.MessageContent)
		case "system":
			msg = schema.NewSystemMessage(record.MessageContent)
		case "assistant":
			msg = schema.NewAssistantMessage(record.MessageContent)
		case "tool":
			msg = schema.NewToolMessage(record.MessageContent, "")
		default:
			msg = schema.NewDefaultMessage(schema.RoleTypeUser, record.MessageContent)
		}

		msgAdd := &db.MessageAdd{
			Message:   msg,
			UserID:    record.UserID,
			ScopeID:   record.ScopeID,
			SessionID: record.SessionID,
		}
		if record.Timestamp != "" {
			if t, err := time.Parse(time.RFC3339, record.Timestamp); err == nil {
				msgAdd.Timestamp = t
			}
		}
		if _, err := m.messageStore.AddMessage(ctx, msgAdd); err != nil {
			logger.Error(logComponent).Err(err).Msg("恢复消息失败")
			return err
		}
	}

	logger.Info(logComponent).Int("record_count", len(backupData)).Msg("已从备份恢复消息")

	// 重置 schema 版本
	if preMigrationVersion != nil {
		if err := m.messageStore.SetSchemaVersion(ctx, int32(*preMigrationVersion)); err != nil {
			logger.Error(logComponent).Err(err).Int("version", *preMigrationVersion).
				Msg("重置 schema 版本失败")
			return err
		}
		logger.Info(logComponent).Int("version", *preMigrationVersion).Msg("已重置 schema 版本")
	}

	return nil
}

// MarshalJSON 实现 json.Marshaler
func (r messageBackupRecord) MarshalJSON() ([]byte, error) {
	type Alias messageBackupRecord
	return json.Marshal(&struct{ Alias }{Alias: Alias(r)})
}

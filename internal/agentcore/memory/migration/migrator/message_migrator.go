package migrator

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/store/db"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/memory/migration/operation"
	"github.com/uapclaw/uapclaw-go/internal/common/exception"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// MessageMigrator 消息存储迁移执行器。
// 版本跟踪通过 BaseMessageStore.GetSchemaVersion/SetSchemaVersion。
// 迁移前创建备份，失败时从备份恢复。
// Python: openjiuwen/core/memory/migration/migrator/message_migrator.py (MessageMigrator)
type MessageMigrator struct {
	// messageStore 消息存储实例
	messageStore db.BaseMessageStore
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

// ──────────────────────────── 常量 ────────────────────────────

const (
	// MessageEntityKey 消息迁移实体键
	// Python: MESSAGE_ENTITY_KEY = "message_global"
	MessageEntityKey = "message_global"
	// backupPageSize 备份分页大小
	// Python: _BACKUP_PAGE_SIZE = 1000
	backupPageSize = 1000
)

// ──────────────────────────── 导出函数 ────────────────────────────

// NewMessageMigrator 创建 MessageMigrator 实例。
// Python: MessageMigrator(message_store)
func NewMessageMigrator(messageStore db.BaseMessageStore) *MessageMigrator {
	return &MessageMigrator{messageStore: messageStore}
}

// TryMigrate 尝试执行消息存储迁移。
// 校验 entityKey + 版本比较 + 备份+执行+恢复。
// Python: MessageMigrator.try_migrate(entity_key, operations)
func (m *MessageMigrator) TryMigrate(ctx context.Context, entityKey string, operations []operation.Operation) error {
	if entityKey != MessageEntityKey {
		logger.Error(logComponent).Str("entity_key", entityKey).Str("expected_key", MessageEntityKey).
			Msg("unsupported entity_key")
		return exception.BuildError(exception.StatusMemoryMigrateMemoryExecutionError,
			exception.WithParam("entity_key", entityKey),
			exception.WithParam("error_msg", fmt.Sprintf("不支持的 entity_key: '%s', 期望: '%s'", entityKey, MessageEntityKey)),
		)
	}

	// 没有待执行的操作，直接返回
	if len(operations) == 0 {
		return nil
	}

	// 操作必须按 schema_version 升序排列，否则执行顺序不可预期
	if !validateOperationsOrder(operations) {
		logger.Error(logComponent).Msg("schema_version is not in ascending order")
		return exception.BuildError(exception.StatusMemoryMigrateMemoryExecutionError,
			exception.WithParam("error_msg", "操作的 schema_version 不是升序排列"),
		)
	}

	// 先取当前库里的 schema 版本，决定哪些操作还需要跑
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
			Msg("current version >= latest operation version, no migration needed")
		return nil
	}

	// 筛出比当前版本新的操作；已经做过迁移的库在这里就结束了
	pendingOps := filterPendingOperations(operations, currentVersionPtr)
	if len(pendingOps) == 0 {
		return nil
	}

	logger.Info(logComponent).Int("pending_count", len(pendingOps)).Msg("found pending operations")

	// 迁移前先备份，失败时按备份回滚
	backupData, backupExpected, err := m.createBackup(ctx)
	if err != nil {
		logger.Error(logComponent).Err(err).Msg("failed to create message backup")
		return err
	}

	// 逐个执行操作；任一失败就回滚，并保留原版本号
	lastVersion := currentVersionPtr
	for idx, op := range pendingOps {
		logger.Info(logComponent).Int("idx", idx+1).Int("total", len(pendingOps)).
			Str("op_type", op.TypeName()).Int("schema_version", op.SchemaVersion()).
			Msg("executing operation")

		if err := m.executeOperation(ctx, op); err != nil {
			logger.Error(logComponent).Err(err).Str("op_type", op.TypeName()).
				Int("schema_version", op.SchemaVersion()).Msg("operation execution failed")

			if restoreErr := m.restoreFromBackup(ctx, backupData, backupExpected, currentVersionPtr); restoreErr != nil {
				logger.Error(logComponent).Err(restoreErr).Msg("failed to restore from backup")
			}
			return err
		}

		v := op.SchemaVersion()
		lastVersion = &v
		logger.Info(logComponent).Str("op_type", op.TypeName()).Int("schema_version", op.SchemaVersion()).
			Msg("operation executed successfully")
	}

	// 全部成功后才落新的 schema 版本
	if lastVersion != nil && (currentVersionPtr == nil || *lastVersion != *currentVersionPtr) {
		if err := m.messageStore.SetSchemaVersion(ctx, int32(*lastVersion)); err != nil {
			return err
		}
		logger.Info(logComponent).Int("new_version", *lastVersion).Msg("message schema version updated")
	}

	return nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// executeOperation 执行单个迁移操作。
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

// createBackup 创建消息备份。
// S-03: 实现基于游标的分页循环，对齐 Python 遍历所有消息而非只取前 backupPageSize 条。
// GetMessages 接口不支持 offset 参数，使用 StartTime 游标分页：每页取完后用最后一条消息的
// createBackup 创建消息备份，返回备份记录与备份时库中的消息总数。
//
// 第二个返回值 expected 是"备份是否完整"的判据：恢复阶段据此决定能否删除现有数据。
// 取法是先 CountMessages 拿真实条数，再以 max(expected, backupPageSize) 一次取全——
// GetMessages 没有 offset，固定分页必然漏掉最新消息，而漏掉的记录一旦在恢复前被删
// 就再也补不回来。
//
// 对齐 Python MessageMigrator._create_backup（message_migrator.py:134-163）。
func (m *MessageMigrator) createBackup(ctx context.Context) ([]messageBackupRecord, int, error) {
	expected, err := m.messageStore.CountMessages(ctx, nil)
	if err != nil {
		return nil, 0, err
	}

	limit := int(expected)
	if limit < backupPageSize {
		limit = backupPageSize
	}

	messages, err := m.messageStore.GetMessages(ctx, nil, limit, "timestamp", "asc")
	if err != nil {
		return nil, 0, err
	}

	backupData := make([]messageBackupRecord, 0, len(messages))
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

	logger.Info(logComponent).
		Int("record_count", len(backupData)).
		Int64("expected", expected).
		Msg("message backup created")
	return backupData, int(expected), nil
}

// restoreFromBackup 从备份恢复消息。
//
// backupExpected 是备份时库中的消息总数。备份条数少于它时**拒绝删除**——宁可让恢复
// 失败，也不能删掉备份里没有、删了就补不回来的记录。
// 对齐 Python MessageMigrator._restore_from_backup（message_migrator.py:165-225）。
func (m *MessageMigrator) restoreFromBackup(ctx context.Context, backupData []messageBackupRecord, backupExpected int, preMigrationVersion *int) error {
	if len(backupData) < backupExpected {
		return exception.BuildError(exception.StatusMemoryMigrateMemoryExecutionError,
			exception.WithParam("error_msg", fmt.Sprintf(
				"backup holds %d of %d messages; skipping restore so messages missing from the backup are not deleted",
				len(backupData), backupExpected)),
		)
	}

	// 删除所有消息
	if _, err := m.messageStore.DeleteMessages(ctx, nil); err != nil {
		logger.Error(logComponent).Err(err).Msg("failed to delete messages")
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
			logger.Error(logComponent).Err(err).Msg("failed to restore message")
			return err
		}
	}

	// 恢复后复核条数：不等说明写入过程中有丢失，必须留下明确线索。
	// 对齐 Python _restore_from_backup 的 count_messages 复核（message_migrator.py:216-225）。
	restored, countErr := m.messageStore.CountMessages(ctx, nil)
	if countErr != nil {
		logger.Error(logComponent).Err(countErr).Msg("failed to verify restored message count")
	} else if int(restored) != len(backupData) {
		logger.Error(logComponent).
			Int64("restored", restored).
			Int("backup_count", len(backupData)).
			Msg("restored message count does not match backup")
	} else {
		logger.Info(logComponent).Int("record_count", len(backupData)).Msg("messages restored from backup")
	}

	// 重置 schema 版本（M-05: 对齐 Python，版本重置失败只记录日志，不影响恢复流程）
	if preMigrationVersion != nil {
		if err := m.messageStore.SetSchemaVersion(ctx, int32(*preMigrationVersion)); err != nil {
			logger.Error(logComponent).Err(err).Int("version", *preMigrationVersion).
				Msg("failed to reset schema version")
			// 对齐 Python: except 后只记录日志，不返回错误
		} else {
			logger.Info(logComponent).Int("version", *preMigrationVersion).Msg("schema version reset")
		}
	}

	return nil
}

// MarshalJSON 实现 json.Marshaler
func (r messageBackupRecord) MarshalJSON() ([]byte, error) {
	type Alias messageBackupRecord
	return json.Marshal(&struct{ Alias }{Alias: Alias(r)})
}

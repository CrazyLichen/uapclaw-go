package migrator

import (
	"context"
	"fmt"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/store/index"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/memory/migration/operation"
	"github.com/uapclaw/uapclaw-go/internal/common/exception"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// IndexVersionMigrator 记忆索引版本迁移执行器。
// 在同一 BaseMemoryIndex 实例内做 MemoryDoc 字段变换。
// 迁移前创建备份，失败时恢复。
//
// Python: openjiuwen/core/memory/migration/migrator/index_version_migrator.py (IndexVersionMigrator)
type IndexVersionMigrator struct{}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

const (
	// indexBatchSize 批量处理文档的大小
	// Python: batch_size = 100
	indexBatchSize = 100
)

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// NewIndexVersionMigrator 创建 IndexVersionMigrator 实例。
func NewIndexVersionMigrator() *IndexVersionMigrator {
	return &IndexVersionMigrator{}
}

// TryMigrate 尝试执行索引版本迁移。
// 注意：此方法签名与其他 Migrator 不同，不接受 entityKey，直接接收 index 实例。
//
// Python: IndexVersionMigrator.try_migrate(index, operations)
func (m *IndexVersionMigrator) TryMigrate(ctx context.Context, idx index.BaseMemoryIndex, operations []operation.Operation) error {
	currentVersion := idx.GetSchemaVersion()

	// 过滤待执行操作
	var opsToApply []operation.Operation
	for _, op := range operations {
		if op.SchemaVersion() > currentVersion {
			opsToApply = append(opsToApply, op)
		}
	}

	if len(opsToApply) == 0 {
		return nil
	}

	// 创建备份
	backupID, err := idx.CreateBackup(ctx)
	if err != nil {
		logger.Error(logComponent).Err(err).Msg("创建索引备份失败")
		return err
	}

	// 执行每个操作
	for _, op := range opsToApply {
		if err := m.applyOperation(ctx, idx, op); err != nil {
			// 迁移失败，恢复备份
			logger.Error(logComponent).Err(err).Str("op_type", op.TypeName()).
				Int("schema_version", op.SchemaVersion()).Msg("索引迁移操作失败")

			if restoreErr := idx.RestoreBackup(ctx, backupID); restoreErr != nil {
				logger.Error(logComponent).Err(restoreErr).Str("backup_id", backupID).Msg("恢复索引备份失败")
			}
			if cleanupErr := idx.CleanupBackup(ctx, backupID); cleanupErr != nil {
				logger.Warn(logComponent).Err(cleanupErr).Str("backup_id", backupID).Msg("清理索引备份失败")
			}
			idx.UpdateSchemaVersion(currentVersion)

			return err
		}
		// 更新版本号
		idx.UpdateSchemaVersion(op.SchemaVersion())
	}

	// 清理备份
	if err := idx.CleanupBackup(ctx, backupID); err != nil {
		logger.Warn(logComponent).Err(err).Str("backup_id", backupID).Msg("清理索引备份失败")
	}

	return nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// applyOperation 应用单个迁移操作。
//
// Python: IndexVersionMigrator._apply_operation
func (m *IndexVersionMigrator) applyOperation(ctx context.Context, idx index.BaseMemoryIndex, op operation.Operation) error {
	switch o := op.(type) {
	case *operation.RenameMemoryDocFieldOperation:
		return m.applyRenameField(ctx, idx, o)
	case *operation.TransformMemoryDocFieldOperation:
		return m.applyTransformField(ctx, idx, o)
	case *operation.AddMemoryDocFieldOperation:
		return m.applyAddField(ctx, idx, o)
	case *operation.RemoveMemoryDocFieldOperation:
		return m.applyRemoveField(ctx, idx, o)
	default:
		return exception.BuildError(exception.StatusMemoryMigrateMemoryExecutionError,
			exception.WithParam("op_type", op.TypeName()),
			exception.WithParam("error_msg", fmt.Sprintf("不支持的索引操作类型: %s", op.TypeName())),
		)
	}
}

// applyRenameField 应用字段重命名操作。
//
// Python: IndexVersionMigrator._apply_rename_field
func (m *IndexVersionMigrator) applyRenameField(ctx context.Context, idx index.BaseMemoryIndex, op *operation.RenameMemoryDocFieldOperation) error {
	scopes, err := idx.ListUserScopes(ctx)
	if err != nil {
		return err
	}

	for _, scope := range scopes {
		offset := 0
		for {
			docs, err := idx.ListMemories(ctx, scope.UserID, scope.ScopeID, offset, indexBatchSize, nil)
			if err != nil {
				return err
			}
			if len(docs) == 0 {
				break
			}

			for _, doc := range docs {
				if _, exists := doc.Fields[op.OldFieldName]; exists {
					doc.Fields[op.NewFieldName] = doc.Fields[op.OldFieldName]
					delete(doc.Fields, op.OldFieldName)
				}
			}

			docIDs := make([]string, len(docs))
			for i, doc := range docs {
				docIDs[i] = doc.ID
			}
			if err := idx.DeleteMemories(ctx, scope.UserID, scope.ScopeID, docIDs); err != nil {
				return err
			}
			if err := idx.AddMemories(ctx, scope.UserID, scope.ScopeID, docs); err != nil {
				return err
			}
			offset += indexBatchSize
		}
	}
	return nil
}

// applyTransformField 应用字段值变换操作。
//
// Python: IndexVersionMigrator._apply_transform_field
func (m *IndexVersionMigrator) applyTransformField(ctx context.Context, idx index.BaseMemoryIndex, op *operation.TransformMemoryDocFieldOperation) error {
	scopes, err := idx.ListUserScopes(ctx)
	if err != nil {
		return err
	}

	for _, scope := range scopes {
		offset := 0
		for {
			docs, err := idx.ListMemories(ctx, scope.UserID, scope.ScopeID, offset, indexBatchSize, nil)
			if err != nil {
				return err
			}
			if len(docs) == 0 {
				break
			}

			for _, doc := range docs {
				if val, exists := doc.Fields[op.FieldName]; exists {
					if op.TransformFunc != nil {
						doc.Fields[op.FieldName] = op.TransformFunc(val)
					}
				}
			}

			docIDs := make([]string, len(docs))
			for i, doc := range docs {
				docIDs[i] = doc.ID
			}
			if err := idx.DeleteMemories(ctx, scope.UserID, scope.ScopeID, docIDs); err != nil {
				return err
			}
			if err := idx.AddMemories(ctx, scope.UserID, scope.ScopeID, docs); err != nil {
				return err
			}
			offset += indexBatchSize
		}
	}
	return nil
}

// applyAddField 应用添加字段操作。
//
// Python: IndexVersionMigrator._apply_add_field
func (m *IndexVersionMigrator) applyAddField(ctx context.Context, idx index.BaseMemoryIndex, op *operation.AddMemoryDocFieldOperation) error {
	scopes, err := idx.ListUserScopes(ctx)
	if err != nil {
		return err
	}

	for _, scope := range scopes {
		offset := 0
		for {
			docs, err := idx.ListMemories(ctx, scope.UserID, scope.ScopeID, offset, indexBatchSize, nil)
			if err != nil {
				return err
			}
			if len(docs) == 0 {
				break
			}

			for _, doc := range docs {
				if _, exists := doc.Fields[op.FieldName]; !exists {
					var defaultVal any
					switch fn := op.DefaultValueOrFunc.(type) {
					case func() any:
						defaultVal = fn()
					default:
						defaultVal = op.DefaultValueOrFunc
					}
					doc.Fields[op.FieldName] = defaultVal
				}
			}

			docIDs := make([]string, len(docs))
			for i, doc := range docs {
				docIDs[i] = doc.ID
			}
			if err := idx.DeleteMemories(ctx, scope.UserID, scope.ScopeID, docIDs); err != nil {
				return err
			}
			if err := idx.AddMemories(ctx, scope.UserID, scope.ScopeID, docs); err != nil {
				return err
			}
			offset += indexBatchSize
		}
	}
	return nil
}

// applyRemoveField 应用删除字段操作。
//
// Python: IndexVersionMigrator._apply_remove_field
func (m *IndexVersionMigrator) applyRemoveField(ctx context.Context, idx index.BaseMemoryIndex, op *operation.RemoveMemoryDocFieldOperation) error {
	scopes, err := idx.ListUserScopes(ctx)
	if err != nil {
		return err
	}

	for _, scope := range scopes {
		offset := 0
		for {
			docs, err := idx.ListMemories(ctx, scope.UserID, scope.ScopeID, offset, indexBatchSize, nil)
			if err != nil {
				return err
			}
			if len(docs) == 0 {
				break
			}

			for _, doc := range docs {
				delete(doc.Fields, op.FieldName)
			}

			docIDs := make([]string, len(docs))
			for i, doc := range docs {
				docIDs[i] = doc.ID
			}
			if err := idx.DeleteMemories(ctx, scope.UserID, scope.ScopeID, docIDs); err != nil {
				return err
			}
			if err := idx.AddMemories(ctx, scope.UserID, scope.ScopeID, docs); err != nil {
				return err
			}
			offset += indexBatchSize
		}
	}
	return nil
}

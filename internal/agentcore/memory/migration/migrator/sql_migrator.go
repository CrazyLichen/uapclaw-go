package migrator

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/memory/migration/operation"
	"github.com/uapclaw/uapclaw-go/internal/common/exception"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	"gorm.io/gorm"
)

// ──────────────────────────── 结构体 ────────────────────────────

// SQLMigrator SQL 表迁移执行器。
// 使用 GORM Migrator 接口执行 DDL 变更，版本跟踪通过 MemoryMetaManager。
//
// Python: openjiuwen/core/memory/migration/migrator/sql_migrator.py (SQLMigrator)
type SQLMigrator struct {
	// db GORM 数据库实例
	db *gorm.DB
	// metaManager 版本跟踪管理器
	metaManager *MemoryMetaManager
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// NewSQLMigrator 创建 SQLMigrator 实例。
//
// Python: SQLMigrator(sql_db_store)
func NewSQLMigrator(db *gorm.DB, metaManager *MemoryMetaManager) *SQLMigrator {
	return &SQLMigrator{db: db, metaManager: metaManager}
}

// TryMigrate 尝试执行 SQL 表迁移。
// 获取当前版本→过滤待执行操作→事务内执行 DDL→更新版本。
//
// Python: SQLMigrator.try_migrate(entity_key, operations)
func (m *SQLMigrator) TryMigrate(ctx context.Context, entityKey string, operations []operation.Operation) error {
	if len(operations) == 0 {
		return nil
	}

	tableName := entityKey
	currentVersion, err := m.getCurrentVersion(ctx, tableName)
	if err != nil {
		return err
	}

	// 过滤待执行操作
	pendingOps := filterPendingOperations(operations, currentVersion)
	if len(pendingOps) == 0 {
		return nil
	}

	logger.Info(logComponent).Str("table_name", tableName).
		Int("pending_count", len(pendingOps)).Msg("开始 SQL 表迁移")

	// 在事务内执行迁移
	err = m.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		for _, op := range pendingOps {
			if err := m.executeOperation(tx, tableName, op); err != nil {
				logger.Error(logComponent).Err(err).Str("op_type", op.TypeName()).
					Int("schema_version", op.SchemaVersion()).Msg("SQL 操作执行失败")
				return err
			}
			logger.Info(logComponent).Str("op_type", op.TypeName()).
				Int("schema_version", op.SchemaVersion()).Msg("SQL 操作执行成功")
		}
		return nil
	})

	if err != nil {
		logger.Error(logComponent).Err(err).Str("table_name", tableName).Msg("SQL 表迁移失败")
		return exception.BuildError(exception.StatusMemoryMigrateMemoryExecutionError,
			exception.WithParam("table_name", tableName),
			exception.WithParam("error_msg", fmt.Sprintf("SQL 表迁移失败: %v", err)),
		)
	}

	// 更新版本
	lastVersion := pendingOps[len(pendingOps)-1].SchemaVersion()
	if err := m.updateVersion(ctx, tableName, lastVersion); err != nil {
		return err
	}

	logger.Info(logComponent).Str("table_name", tableName).
		Int("new_version", lastVersion).Msg("SQL 表迁移完成")
	return nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// getCurrentVersion 获取当前表的 schema 版本。
//
// Python: SQLMigrator.try_migrate 中获取 current_meta
func (m *SQLMigrator) getCurrentVersion(ctx context.Context, tableName string) (*int, error) {
	currentMeta, err := m.metaManager.GetByTableName(ctx, tableName)
	if err != nil {
		return nil, err
	}
	if len(currentMeta) > 0 {
		versionStr, ok := currentMeta[0]["schema_version"].(string)
		if ok {
			version, err := strconv.Atoi(versionStr)
			if err != nil {
				return nil, exception.BuildError(exception.StatusMemoryMigrateMemoryExecutionError,
					exception.WithParam("error_msg", fmt.Sprintf("无效的 schema_version: '%s'", versionStr)),
				)
			}
			return &version, nil
		}
	}
	return nil, nil
}

// updateVersion 更新表的 schema 版本。
//
// Python: SQLMigrator.try_migrate 中 update/insert memory_meta
func (m *SQLMigrator) updateVersion(ctx context.Context, tableName string, version int) error {
	versionStr := strconv.Itoa(version)

	// 先尝试 Add（幂等），若已存在则跳过
	if err := m.metaManager.Add(ctx, tableName, versionStr); err != nil {
		return err
	}

	// 更新已有记录的 schema_version
	result := m.db.WithContext(ctx).Table("memory_meta").
		Where("table_name = ?", tableName).
		Update("schema_version", versionStr)
	if result.Error != nil {
		logger.Error(logComponent).Err(result.Error).Str("table_name", tableName).
			Int("version", version).Msg("更新 schema 版本失败")
		return result.Error
	}

	return nil
}

// executeOperation 执行单个 SQL 迁移操作。
//
// Python: SQLMigrator._migrate_add_column / _migrate_rename_column / _migrate_update_column_type
func (m *SQLMigrator) executeOperation(tx *gorm.DB, tableName string, op operation.Operation) error {
	switch o := op.(type) {
	case *operation.AddColumnOperation:
		return m.migrateAddColumn(tx, o)
	case *operation.RenameColumnOperation:
		return m.migrateRenameColumn(tx, o)
	case *operation.UpdateColumnTypeOperation:
		return m.migrateUpdateColumnType(tx, o)
	default:
		return exception.BuildError(exception.StatusMemoryMigrateMemoryExecutionError,
			exception.WithParam("op_type", op.TypeName()),
			exception.WithParam("error_msg", fmt.Sprintf("不支持的 SQL 操作类型: %s", op.TypeName())),
		)
	}
}

// migrateAddColumn 执行添加列操作。
//
// Python: SQLMigrator._migrate_add_column
func (m *SQLMigrator) migrateAddColumn(tx *gorm.DB, op *operation.AddColumnOperation) error {
	tableName := op.Table
	columnName := op.ColumnName
	columnType := getGORMColumnType(op.ColumnType)

	// 使用原生 SQL 添加列，支持 DEFAULT 和 NULL/NOT NULL
	var nullStr string
	if op.Nullable {
		nullStr = "NULL"
	} else {
		nullStr = "NOT NULL"
	}

	sql := fmt.Sprintf("ALTER TABLE %s ADD COLUMN %s %s %s", tableName, columnName, columnType, nullStr)

	// 添加默认值
	if op.Default != nil {
		sql += fmt.Sprintf(" DEFAULT %v", op.Default)
	}

	if err := tx.Exec(sql).Error; err != nil {
		// SQLite 的 ALTER TABLE ADD COLUMN 不支持 NOT NULL 无 DEFAULT
		// 回退到 GORM Migrator
		logger.Warn(logComponent).Err(err).Str("table", tableName).Str("column", columnName).
			Msg("原生 SQL 添加列失败，尝试 GORM Migrator")
		return tx.Migrator().AddColumn(tableName, columnName)
	}

	logger.Info(logComponent).Str("table", tableName).Str("column", columnName).
		Str("column_type", columnType).Bool("nullable", op.Nullable).Msg("已添加列")
	return nil
}

// migrateRenameColumn 执行重命名列操作。
//
// Python: SQLMigrator._migrate_rename_column
func (m *SQLMigrator) migrateRenameColumn(tx *gorm.DB, op *operation.RenameColumnOperation) error {
	tableName := op.Table

	if err := tx.Migrator().RenameColumn(tableName, op.OldColumnName, op.NewColumnName); err != nil {
		logger.Error(logComponent).Err(err).Str("table", tableName).
			Str("old_column", op.OldColumnName).Str("new_column", op.NewColumnName).
			Msg("重命名列失败")
		return err
	}

	logger.Info(logComponent).Str("table", tableName).
		Str("old_column", op.OldColumnName).Str("new_column", op.NewColumnName).
		Msg("已重命名列")
	return nil
}

// migrateUpdateColumnType 执行修改列类型操作。
//
// Python: SQLMigrator._migrate_update_column_type
func (m *SQLMigrator) migrateUpdateColumnType(tx *gorm.DB, op *operation.UpdateColumnTypeOperation) error {
	tableName := op.Table
	columnName := op.ColumnName
	newColumnType := getGORMColumnType(op.NewColumnType)

	// 检测方言名
	dialectName := tx.Dialector.Name()

	if dialectName == "sqlite" {
		// SQLite 不支持 ALTER COLUMN TYPE，使用 GORM Migrator().AlterColumn
		// 注意：GORM 的 SQLite driver 会通过重建表来实现 AlterColumn
		if err := tx.Migrator().AlterColumn(tableName, columnName); err != nil {
			logger.Error(logComponent).Err(err).Str("table", tableName).
				Str("column", columnName).Str("new_type", newColumnType).
				Msg("SQLite 修改列类型失败")
			return err
		}
	} else {
		// MySQL / PostgreSQL：使用 ALTER TABLE ALTER COLUMN
		sql := fmt.Sprintf("ALTER TABLE %s ALTER COLUMN %s TYPE %s", tableName, columnName, newColumnType)
		if err := tx.Exec(sql).Error; err != nil {
			// 回退到 GORM Migrator
			logger.Warn(logComponent).Err(err).Str("table", tableName).Str("column", columnName).
				Msg("原生 SQL 修改列类型失败，尝试 GORM Migrator")
			if alterErr := tx.Migrator().AlterColumn(tableName, columnName); alterErr != nil {
				return alterErr
			}
		}
	}

	logger.Info(logComponent).Str("table", tableName).
		Str("column", columnName).Str("new_type", newColumnType).
		Msg("已修改列类型")
	return nil
}

// getGORMColumnType 将字符串类型映射为 SQL 类型字符串。
//
// Python: SQLMigrator.get_sqlalchemy_type
func getGORMColumnType(typeString string) string {
	typeString = strings.TrimSpace(typeString)
	upperType := strings.ToUpper(typeString)

	// 处理带长度的类型：VARCHAR(255)、STRING(128) 等
	if idx := strings.Index(upperType, "("); idx > 0 {
		baseType := upperType[:idx]
		params := typeString[idx:] // 保留括号及参数
		switch baseType {
		case "STRING", "VARCHAR":
			return "VARCHAR" + params
		default:
			return typeString
		}
	}

	// 无参数的类型映射
	typeMap := map[string]string{
		"STRING":   "VARCHAR(255)",
		"VARCHAR":  "VARCHAR(255)",
		"INTEGER":  "INTEGER",
		"INT":      "INTEGER",
		"DATETIME": "DATETIME",
		"BOOLEAN":  "BOOLEAN",
		"BOOL":     "BOOLEAN",
		"TEXT":     "TEXT",
		"FLOAT":    "FLOAT",
	}

	if mapped, ok := typeMap[upperType]; ok {
		return mapped
	}

	// 默认 TEXT
	return "TEXT"
}

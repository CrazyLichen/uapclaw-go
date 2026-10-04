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
	// allowedTables S-01: 支持迁移的表名白名单（nil 时使用默认白名单）
	allowedTables map[string]bool
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

const (
	// memoryTablesConfig S-01: 支持迁移的表名白名单。
	// 对齐 Python: MEMORY_TABLES_CONFIG (openjiuwen/core/memory/manage/mem_model/db_model.py)
	// 只允许对已知表执行 DDL 迁移操作，防止对任意表的操作。
	memoryTablesConfig = "user_messages,scope_user_mapping"
)

// ──────────────────────────── 全局变量 ────────────────────────────

var (
	// memoryTablesSet S-01: 支持迁移的表名集合（从 memoryTablesConfig 解析）
	memoryTablesSet map[string]bool
)

func init() {
	memoryTablesSet = make(map[string]bool)
	for _, name := range strings.Split(memoryTablesConfig, ",") {
		memoryTablesSet[strings.TrimSpace(name)] = true
	}
}

// ──────────────────────────── 导出函数 ────────────────────────────

// NewSQLMigrator 创建 SQLMigrator 实例。
//
// Python: SQLMigrator(sql_db_store)
func NewSQLMigrator(db *gorm.DB, metaManager *MemoryMetaManager) *SQLMigrator {
	return &SQLMigrator{
		db:            db,
		metaManager:   metaManager,
		allowedTables: memoryTablesSet,
	}
}

// SetAllowedTables 设置允许迁移的表名白名单。
// S-01: 测试或扩展时可用此方法覆盖默认白名单。
func (m *SQLMigrator) SetAllowedTables(tables map[string]bool) {
	m.allowedTables = tables
}

// BatchMigrate 批量执行表 schema 迁移。
// 对齐 Python: SQLMigrator.batch_migrate(migrations) -> Dict[str, bool]
//
// 参数 migrations 中每项包含 table_name 和 operations，
// 返回每个表的迁移结果（true=成功, false=失败）。
//
// Python: SQLMigrator.batch_migrate(migrations: List[Dict[str, Any]]) -> Dict[str, bool]
func (m *SQLMigrator) BatchMigrate(ctx context.Context, migrations []BatchMigrationItem) map[string]bool {
	results := make(map[string]bool, len(migrations))
	for _, mg := range migrations {
		err := m.TryMigrate(ctx, mg.TableName, mg.Operations)
		results[mg.TableName] = err == nil
		if err != nil {
			logger.Error(logComponent).Err(err).Str("table_name", mg.TableName).
				Msg("BatchMigrate: 表迁移失败")
		}
	}
	return results
}

// BatchMigrationItem 批量迁移的单项。
// 对齐 Python: batch_migrate 参数列表中的单个 dict，含 table_name 和 operations。
type BatchMigrationItem struct {
	// TableName 表名
	TableName string
	// Operations 迁移操作列表
	Operations []operation.Operation
}

// TryMigrate 尝试执行 SQL 表迁移。
// 获取当前版本→过滤待执行操作→事务内执行 DDL→更新版本。
//
// Python: SQLMigrator.try_migrate(entity_key, operations)
func (m *SQLMigrator) TryMigrate(ctx context.Context, entityKey string, operations []operation.Operation) error {
	if len(operations) == 0 {
		return nil
	}

	// S-01: 校验表名是否在白名单
	tableName := entityKey
	if !m.validateTableName(tableName) {
		return exception.BuildError(exception.StatusMemoryMigrateMemoryExecutionError,
			exception.WithParam("table_name", tableName),
			exception.WithParam("error_msg", fmt.Sprintf("unsupported table name: %s (allowed: %s)", tableName, memoryTablesConfig)),
		)
	}
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

	// S-02: 对齐 Python upsert 模式 — 先 Update，RowsAffected==0 再 Insert
	result := m.db.WithContext(ctx).Table("memory_meta").
		Where("table_name = ?", tableName).
		Update("schema_version", versionStr)
	if result.Error != nil {
		logger.Error(logComponent).Err(result.Error).Str("table_name", tableName).
			Int("version", version).Msg("更新 schema 版本失败")
		return result.Error
	}
	if result.RowsAffected == 0 {
		// 不存在记录，插入新行
		if err := m.metaManager.Add(ctx, tableName, versionStr); err != nil {
			logger.Error(logComponent).Err(err).Str("table_name", tableName).
				Int("version", version).Msg("插入 schema 版本失败")
			return err
		}
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

	// S-04: SQL 注入防护 — 校验表名/列名只含合法字符
	if !isValidIdentifier(tableName) {
		return fmt.Errorf("invalid table name: %s", tableName)
	}
	if !isValidIdentifier(columnName) {
		return fmt.Errorf("invalid column name: %s", columnName)
	}

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

	// S-04: SQL 注入防护
	if !isValidIdentifier(tableName) {
		return fmt.Errorf("invalid table name: %s", tableName)
	}
	if !isValidIdentifier(columnName) {
		return fmt.Errorf("invalid column name: %s", columnName)
	}

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
		// S-05: 按方言区分 SQL 语法
		// MySQL 使用 MODIFY COLUMN，PostgreSQL 使用 ALTER COLUMN TYPE
		var sql string
		if dialectName == "mysql" {
			sql = fmt.Sprintf("ALTER TABLE %s MODIFY COLUMN %s %s", tableName, columnName, newColumnType)
		} else {
			sql = fmt.Sprintf("ALTER TABLE %s ALTER COLUMN %s TYPE %s", tableName, columnName, newColumnType)
		}
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

// isValidIdentifier 校验 SQL 标识符（表名/列名）只含合法字符，防止 SQL 注入。
// 合法字符：字母、数字、下划线，且不以数字开头。
func isValidIdentifier(name string) bool {
	if name == "" {
		return false
	}
	for i, ch := range name {
		if ch == '_' {
			continue
		}
		if ch >= 'a' && ch <= 'z' || ch >= 'A' && ch <= 'Z' {
			continue
		}
		if ch >= '0' && ch <= '9' {
			// 数字不能出现在首位
			if i == 0 {
				return false
			}
			continue
		}
		return false
	}
	return true
}

// validateTableName S-01: 校验表名是否在白名单。
// 对齐 Python: SQLMigrator._validate_table(table_name)
func (m *SQLMigrator) validateTableName(tableName string) bool {
	allowed := m.allowedTables
	if allowed == nil {
		allowed = memoryTablesSet
	}
	return allowed[tableName]
}

package migrator

import (
	"context"
)

// ──────────────────────────── 结构体 ────────────────────────────

// SqlDbQuerier SqlDbStore 的最小接口，用于解耦 migrator 和 model 包。
// model.SqlDbStore 隐式实现此接口。
type SqlDbQuerier interface {
	// Write 插入一行数据
	Write(ctx context.Context, table string, data map[string]any) error
	// ConditionGet 条件查询
	ConditionGet(ctx context.Context, table string, conditions map[string]any, columns []string) ([]map[string]any, error)
	// Exist 检查记录是否存在
	Exist(ctx context.Context, table string, conditions map[string]any) (bool, error)
	// Delete 条件删除
	Delete(ctx context.Context, table string, conditions map[string]any) error
}

// MemoryMetaManager 内存元数据管理器，基于 SqlDbQuerier 操作 memory_meta 表。
//
// 对应 Python: openjiuwen/core/memory/migration/migrator/memory_meta_manager.py (MemoryMetaManager)
type MemoryMetaManager struct {
	// db 数据库查询接口
	db SqlDbQuerier
	// metaTable 元数据表名
	metaTable string
}

// ──────────────────────────── 导出函数 ────────────────────────────

// NewMemoryMetaManager 创建 MemoryMetaManager 实例。
func NewMemoryMetaManager(db SqlDbQuerier) *MemoryMetaManager {
	return &MemoryMetaManager{
		db:        db,
		metaTable: "memory_meta",
	}
}

// Add 添加 schema 版本记录。
// tableName 或 schemaVersion 为空时静默返回 nil。
// 若记录已存在则跳过（幂等）。
//
// 对应 Python: MemoryMetaManager.add(table_name, schema_version)
func (m *MemoryMetaManager) Add(ctx context.Context, tableName string, schemaVersion string) error {
	if tableName == "" || schemaVersion == "" {
		return nil
	}
	data := map[string]any{
		"table_name":     tableName,
		"schema_version": schemaVersion,
	}
	exists, err := m.db.Exist(ctx, m.metaTable,
		map[string]any{"table_name": tableName, "schema_version": schemaVersion})
	if err != nil {
		return err
	}
	if exists {
		return nil
	}
	return m.db.Write(ctx, m.metaTable, data)
}

// GetByTableName 按 table_name 查询 schema 版本记录。
//
// 对应 Python: MemoryMetaManager.get_by_table_name(table_name)
func (m *MemoryMetaManager) GetByTableName(ctx context.Context, tableName string) ([]map[string]any, error) {
	results, err := m.db.ConditionGet(ctx, m.metaTable,
		map[string]any{"table_name": []string{tableName}}, nil)
	if err != nil {
		return nil, err
	}
	return results, nil
}

// DeleteByTableName 按 table_name 删除 schema 版本记录。
// 补齐 Python 中存在但 Go 之前缺失的方法。
//
// 对应 Python: MemoryMetaManager.delete_by_table_name(table_name)
func (m *MemoryMetaManager) DeleteByTableName(ctx context.Context, tableName string) error {
	return m.db.Delete(ctx, m.metaTable,
		map[string]any{"table_name": tableName})
}

package migrator

import (
	"context"
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/memory/migration/operation"
	"github.com/uapclaw/uapclaw-go/internal/common/exception"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

// ──────────────────────────── 辅助函数 ────────────────────────────

// newTestDB 创建 SQLite 内存数据库用于测试
func newTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("无法创建测试数据库: %v", err)
	}
	return db
}

// newTestMetaManager 创建使用测试数据库的 MemoryMetaManager
func newTestMetaManager(t *testing.T, db *gorm.DB) *MemoryMetaManager {
	t.Helper()
	// 创建 memory_meta 表
	if err := db.Exec("CREATE TABLE IF NOT EXISTS memory_meta (table_name TEXT, schema_version TEXT)").Error; err != nil {
		t.Fatalf("无法创建 memory_meta 表: %v", err)
	}
	// 使用 GORM 的 db 实现一个最小 SqlDbQuerier
	return NewMemoryMetaManager(&testSqlDbQuerier{db: db})
}

// testSqlDbQuerier 最小 SqlDbQuerier 实现，用于测试
type testSqlDbQuerier struct {
	db *gorm.DB
}

func (q *testSqlDbQuerier) Write(ctx context.Context, table string, data map[string]any) error {
	return q.db.WithContext(ctx).Table(table).Create(data).Error
}

func (q *testSqlDbQuerier) ConditionGet(ctx context.Context, table string, conditions map[string]any, columns []string) ([]map[string]any, error) {
	query := q.db.WithContext(ctx).Table(table)
	for k, v := range conditions {
		query = query.Where(k+" = ?", v)
	}
	var results []map[string]any
	if err := query.Find(&results).Error; err != nil {
		return nil, err
	}
	return results, nil
}

func (q *testSqlDbQuerier) Exist(ctx context.Context, table string, conditions map[string]any) (bool, error) {
	query := q.db.WithContext(ctx).Table(table)
	for k, v := range conditions {
		query = query.Where(k+" = ?", v)
	}
	var count int64
	if err := query.Count(&count).Error; err != nil {
		return false, err
	}
	return count > 0, nil
}

func (q *testSqlDbQuerier) Delete(ctx context.Context, table string, conditions map[string]any) error {
	query := q.db.WithContext(ctx).Table(table)
	for k, v := range conditions {
		query = query.Where(k+" = ?", v)
	}
	return query.Delete(nil).Error
}

// ──────────────────────────── getGORMColumnType 测试 ────────────────────────────

// TestGetGORMColumnType_基本类型 测试基本类型映射
func TestGetGORMColumnType_基本类型(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"string", "VARCHAR(255)"},
		{"STRING", "VARCHAR(255)"},
		{"varchar", "VARCHAR(255)"},
		{"integer", "INTEGER"},
		{"INT", "INTEGER"},
		{"datetime", "DATETIME"},
		{"boolean", "BOOLEAN"},
		{"BOOL", "BOOLEAN"},
		{"text", "TEXT"},
		{"float", "FLOAT"},
	}

	for _, tt := range tests {
		result := getGORMColumnType(tt.input)
		if result != tt.expected {
			t.Errorf("getGORMColumnType(%q) = %q, want %q", tt.input, result, tt.expected)
		}
	}
}

// TestGetGORMColumnType_带长度 测试带长度参数的类型
func TestGetGORMColumnType_带长度(t *testing.T) {
	result := getGORMColumnType("varchar(128)")
	if result != "VARCHAR(128)" {
		t.Errorf("getGORMColumnType('varchar(128)') = %q, want 'VARCHAR(128)'", result)
	}
}

// TestGetGORMColumnType_未知类型 测试未知类型默认返回 TEXT
func TestGetGORMColumnType_未知类型(t *testing.T) {
	result := getGORMColumnType("custom_type")
	if result != "TEXT" {
		t.Errorf("getGORMColumnType('custom_type') = %q, want 'TEXT'", result)
	}
}

// ──────────────────────────── SQLMigrator.TryMigrate 测试 ────────────────────────────

// TestSQLMigrator_TryMigrate_空操作 测试空操作列表直接返回
func TestSQLMigrator_TryMigrate_空操作(t *testing.T) {
	db := newTestDB(t)
	metaManager := newTestMetaManager(t, db)
	m := NewSQLMigrator(db, metaManager)
	m.SetAllowedTables(map[string]bool{"test_table": true})

	err := m.TryMigrate(context.Background(), "test_table", nil)
	if err != nil {
		t.Errorf("空操作列表应返回 nil, got %v", err)
	}
}

// TestSQLMigrator_TryMigrate_添加列 测试添加列操作
func TestSQLMigrator_TryMigrate_添加列(t *testing.T) {
	db := newTestDB(t)
	metaManager := newTestMetaManager(t, db)

	// 创建测试表
	if err := db.Exec("CREATE TABLE test_table (id INTEGER PRIMARY KEY, name TEXT)").Error; err != nil {
		t.Fatalf("无法创建测试表: %v", err)
	}

	ops := []operation.Operation{
		&operation.AddColumnOperation{
			BaseOperation: operation.BaseOperation{Metadata: operation.OperationMetadata{SchemaVersion: 1}},
			Table:         "test_table",
			ColumnName:    "age",
			ColumnType:    "integer",
			Nullable:      true,
		},
	}

	m := NewSQLMigrator(db, metaManager)
	m.SetAllowedTables(map[string]bool{"test_table": true})
	err := m.TryMigrate(context.Background(), "test_table", ops)
	if err != nil {
		t.Fatalf("TryMigrate 失败: %v", err)
	}

	// 验证列已添加
	if !db.Migrator().HasColumn("test_table", "age") {
		t.Error("列 'age' 未添加")
	}

	// 验证版本已更新
	currentVersion, err := metaManager.GetByTableName(context.Background(), "test_table")
	if err != nil {
		t.Fatalf("获取版本失败: %v", err)
	}
	if len(currentVersion) == 0 {
		t.Error("版本记录不存在")
	}
}

// TestSQLMigrator_TryMigrate_重命名列 测试重命名列操作
func TestSQLMigrator_TryMigrate_重命名列(t *testing.T) {
	db := newTestDB(t)
	metaManager := newTestMetaManager(t, db)

	// 创建测试表
	if err := db.Exec("CREATE TABLE test_table (id INTEGER PRIMARY KEY, old_name TEXT)").Error; err != nil {
		t.Fatalf("无法创建测试表: %v", err)
	}

	ops := []operation.Operation{
		&operation.RenameColumnOperation{
			BaseOperation: operation.BaseOperation{Metadata: operation.OperationMetadata{SchemaVersion: 1}},
			Table:         "test_table",
			OldColumnName: "old_name",
			NewColumnName: "new_name",
		},
	}

	m := NewSQLMigrator(db, metaManager)
	m.SetAllowedTables(map[string]bool{"test_table": true})
	err := m.TryMigrate(context.Background(), "test_table", ops)
	if err != nil {
		t.Fatalf("TryMigrate 失败: %v", err)
	}

	// 验证列已重命名
	if !db.Migrator().HasColumn("test_table", "new_name") {
		t.Error("列 'new_name' 不存在")
	}
	if db.Migrator().HasColumn("test_table", "old_name") {
		t.Error("列 'old_name' 应已被重命名")
	}
}

// TestSQLMigrator_TryMigrate_部分操作待执行 测试只执行高版本操作
func TestSQLMigrator_TryMigrate_部分操作待执行(t *testing.T) {
	db := newTestDB(t)
	metaManager := newTestMetaManager(t, db)

	// 创建测试表和版本记录
	if err := db.Exec("CREATE TABLE test_table (id INTEGER PRIMARY KEY, name TEXT)").Error; err != nil {
		t.Fatalf("无法创建测试表: %v", err)
	}
	metaManager.Add(context.Background(), "test_table", "1")

	ops := []operation.Operation{
		&operation.AddColumnOperation{
			BaseOperation: operation.BaseOperation{Metadata: operation.OperationMetadata{SchemaVersion: 1}},
			Table:         "test_table",
			ColumnName:    "col_v1",
			ColumnType:    "text",
			Nullable:      true,
		},
		&operation.AddColumnOperation{
			BaseOperation: operation.BaseOperation{Metadata: operation.OperationMetadata{SchemaVersion: 2}},
			Table:         "test_table",
			ColumnName:    "col_v2",
			ColumnType:    "text",
			Nullable:      true,
		},
	}

	m := NewSQLMigrator(db, metaManager)
	m.SetAllowedTables(map[string]bool{"test_table": true})
	err := m.TryMigrate(context.Background(), "test_table", ops)
	if err != nil {
		t.Fatalf("TryMigrate 失败: %v", err)
	}

	// 验证只有 v2 操作执行
	if db.Migrator().HasColumn("test_table", "col_v1") {
		t.Error("col_v1 (schema_version=1) 不应被添加")
	}
	if !db.Migrator().HasColumn("test_table", "col_v2") {
		t.Error("col_v2 (schema_version=2) 应被添加")
	}
}

// TestSQLMigrator_TryMigrate_不支持的操作类型 测试不支持的操作类型
func TestSQLMigrator_TryMigrate_不支持的操作类型(t *testing.T) {
	db := newTestDB(t)
	metaManager := newTestMetaManager(t, db)

	// 创建测试表
	if err := db.Exec("CREATE TABLE test_table (id INTEGER PRIMARY KEY)").Error; err != nil {
		t.Fatalf("无法创建测试表: %v", err)
	}

	ops := []operation.Operation{
		&operation.UpdateKVOperation{
			BaseOperation: operation.BaseOperation{Metadata: operation.OperationMetadata{SchemaVersion: 1}},
		},
	}

	m := NewSQLMigrator(db, metaManager)
	m.SetAllowedTables(map[string]bool{"test_table": true})
	err := m.TryMigrate(context.Background(), "test_table", ops)
	if err == nil {
		t.Error("不支持的 SQL 操作类型应返回错误")
	}
}

// TestSQLMigrator_TryMigrate_事务回滚 测试迁移失败时事务回滚
func TestSQLMigrator_TryMigrate_事务回滚(t *testing.T) {
	db := newTestDB(t)
	metaManager := newTestMetaManager(t, db)

	// 创建测试表
	if err := db.Exec("CREATE TABLE test_table (id INTEGER PRIMARY KEY)").Error; err != nil {
		t.Fatalf("无法创建测试表: %v", err)
	}

	ops := []operation.Operation{
		&operation.AddColumnOperation{
			BaseOperation: operation.BaseOperation{Metadata: operation.OperationMetadata{SchemaVersion: 1}},
			Table:         "test_table",
			ColumnName:    "age",
			ColumnType:    "integer",
			Nullable:      true,
		},
		&operation.RenameColumnOperation{
			BaseOperation: operation.BaseOperation{Metadata: operation.OperationMetadata{SchemaVersion: 2}},
			Table:         "test_table",
			OldColumnName: "nonexist_col",
			NewColumnName: "renamed_col",
		},
	}

	m := NewSQLMigrator(db, metaManager)
	m.SetAllowedTables(map[string]bool{"test_table": true})
	err := m.TryMigrate(context.Background(), "test_table", ops)
	if err == nil {
		t.Error("迁移失败应返回错误")
	}

	// 验证事务回滚——age 列不应存在（因为第 2 个操作失败导致整个事务回滚）
	if db.Migrator().HasColumn("test_table", "age") {
		t.Error("事务回滚后 age 列不应存在")
	}
}

// TestSQLMigrator_TryMigrate_版本已是最新 测试当前版本已是最新的情况
func TestSQLMigrator_TryMigrate_版本已是最新(t *testing.T) {
	db := newTestDB(t)
	metaManager := newTestMetaManager(t, db)

	if err := db.Exec("CREATE TABLE test_table (id INTEGER PRIMARY KEY)").Error; err != nil {
		t.Fatalf("无法创建测试表: %v", err)
	}

	// 设置当前版本为 5
	metaManager.Add(context.Background(), "test_table", "5")

	ops := []operation.Operation{
		&operation.AddColumnOperation{
			BaseOperation: operation.BaseOperation{Metadata: operation.OperationMetadata{SchemaVersion: 3}},
			Table:         "test_table",
			ColumnName:    "col_v3",
			ColumnType:    "text",
			Nullable:      true,
		},
	}

	m := NewSQLMigrator(db, metaManager)
	m.SetAllowedTables(map[string]bool{"test_table": true})
	err := m.TryMigrate(context.Background(), "test_table", ops)
	if err != nil {
		t.Errorf("版本已是最新时应返回 nil, got %v", err)
	}
}

// TestSQLMigrator_TryMigrate_错误码 测试返回正确的错误码
func TestSQLMigrator_TryMigrate_错误码(t *testing.T) {
	db := newTestDB(t)
	metaManager := newTestMetaManager(t, db)

	if err := db.Exec("CREATE TABLE test_table (id INTEGER PRIMARY KEY)").Error; err != nil {
		t.Fatalf("无法创建测试表: %v", err)
	}

	ops := []operation.Operation{
		&operation.UpdateKVOperation{
			BaseOperation: operation.BaseOperation{Metadata: operation.OperationMetadata{SchemaVersion: 1}},
		},
	}

	m := NewSQLMigrator(db, metaManager)
	m.SetAllowedTables(map[string]bool{"test_table": true})
	err := m.TryMigrate(context.Background(), "test_table", ops)
	if err == nil {
		t.Fatal("应返回错误")
	}

	// 验证错误码
	baseErr, ok := err.(*exception.BaseError)
	if !ok {
		t.Fatalf("错误类型应为 *exception.BaseError, got %T", err)
	}
	if baseErr.Code() != exception.StatusMemoryMigrateMemoryExecutionError.Code() {
		t.Errorf("错误码 = %d, want %d", baseErr.Code(), exception.StatusMemoryMigrateMemoryExecutionError.Code())
	}
}

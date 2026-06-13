package gaussdb

import (
	"testing"

	"gorm.io/driver/postgres"
	"gorm.io/gorm/migrator"
)

// ──────────────────────────── GaussMigrator 测试 ────────────────────────────

// TestGaussMigrator_嵌入PostgresMigrator 验证 GaussMigrator 嵌入 postgres.Migrator。
func TestGaussMigrator_嵌入PostgresMigrator(t *testing.T) {
	m := GaussMigrator{}
	// 验证 GaussMigrator 嵌入了 postgres.Migrator
	_ = m.Migrator
	// 验证 postgres.Migrator 嵌入了 migrator.Migrator
	_ = m.Migrator.Migrator
}

// TestGaussMigrator_继承MigratorConfig 验证 GaussMigrator 可正确初始化 migrator.Config。
func TestGaussMigrator_继承MigratorConfig(t *testing.T) {
	m := GaussMigrator{
		Migrator: postgres.Migrator{
			Migrator: migrator.Migrator{
				Config: migrator.Config{
					CreateIndexAfterCreateTable: true,
				},
			},
		},
	}
	if !m.CreateIndexAfterCreateTable {
		t.Error("GaussMigrator 未正确继承 CreateIndexAfterCreateTable 配置")
	}
}

package database

import (
	"encoding/json"
	"testing"
)

// TestNewDatabaseConfig 验证默认配置值
func TestNewDatabaseConfig(t *testing.T) {
	cfg := NewDatabaseConfig()

	if cfg.DBType != DatabaseTypeSQLite {
		t.Errorf("DBType 期望 %s, 实际 %s", DatabaseTypeSQLite, cfg.DBType)
	}
	if cfg.ConnectionString != "" {
		t.Errorf("ConnectionString 期望空, 实际 %s", cfg.ConnectionString)
	}
	if cfg.DBTimeout != 30 {
		t.Errorf("DBTimeout 期望 30, 实际 %d", cfg.DBTimeout)
	}
	if cfg.DBEnableWAL != true {
		t.Errorf("DBEnableWAL 期望 true, 实际 %v", cfg.DBEnableWAL)
	}
}

// TestNewMemoryDatabaseConfig 验证默认内存数据库配置值
func TestNewMemoryDatabaseConfig(t *testing.T) {
	cfg := NewMemoryDatabaseConfig()

	if cfg.DBType != DatabaseTypeMemory {
		t.Errorf("DBType 期望 %s, 实际 %s", DatabaseTypeMemory, cfg.DBType)
	}
	if cfg.ConnectionString != "" {
		t.Errorf("ConnectionString 期望空, 实际 %s", cfg.ConnectionString)
	}
}

// TestDatabaseConfig_JSON序列化往返 验证 JSON 序列化往返正确性
func TestDatabaseConfig_JSON序列化往返(t *testing.T) {
	original := NewDatabaseConfig()
	original.ConnectionString = "sqlite:///test.db"

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("序列化失败: %v", err)
	}

	var restored DatabaseConfig
	if err := json.Unmarshal(data, &restored); err != nil {
		t.Fatalf("反序列化失败: %v", err)
	}

	if restored.DBType != original.DBType {
		t.Errorf("DBType 期望 %s, 实际 %s", original.DBType, restored.DBType)
	}
	if restored.ConnectionString != original.ConnectionString {
		t.Errorf("ConnectionString 期望 %s, 实际 %s", original.ConnectionString, restored.ConnectionString)
	}
	if restored.DBTimeout != original.DBTimeout {
		t.Errorf("DBTimeout 期望 %d, 实际 %d", original.DBTimeout, restored.DBTimeout)
	}
	if restored.DBEnableWAL != original.DBEnableWAL {
		t.Errorf("DBEnableWAL 期望 %v, 实际 %v", original.DBEnableWAL, restored.DBEnableWAL)
	}
}

// TestDatabaseType 枚举值验证
func TestDatabaseType(t *testing.T) {
	types := map[DatabaseType]string{
		DatabaseTypeSQLite:     "sqlite",
		DatabaseTypePostgreSQL: "postgresql",
		DatabaseTypeMySQL:      "mysql",
		DatabaseTypeMemory:     "memory",
	}
	for typ, expected := range types {
		if string(typ) != expected {
			t.Errorf("DatabaseType 期望 %s, 实际 %s", expected, string(typ))
		}
	}
}

// TestMemoryDatabaseConfig_JSON序列化往返 验证内存数据库配置 JSON 往返
func TestMemoryDatabaseConfig_JSON序列化往返(t *testing.T) {
	original := NewMemoryDatabaseConfig()
	original.ConnectionString = "memory://test"

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("序列化失败: %v", err)
	}

	var restored MemoryDatabaseConfig
	if err := json.Unmarshal(data, &restored); err != nil {
		t.Fatalf("反序列化失败: %v", err)
	}

	if restored.DBType != original.DBType {
		t.Errorf("DBType 期望 %s, 实际 %s", original.DBType, restored.DBType)
	}
	if restored.ConnectionString != original.ConnectionString {
		t.Errorf("ConnectionString 期望 %s, 实际 %s", original.ConnectionString, restored.ConnectionString)
	}
}

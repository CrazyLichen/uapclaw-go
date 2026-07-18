package database

// ──────────────────────────── 结构体 ────────────────────────────

// DBConfigProvider 数据库配置提供者接口。
// 对齐 Python: DatabaseConfig | MemoryDatabaseConfig 联合类型，
// 两种配置通过此接口统一访问。
type DBConfigProvider interface {
	// GetDBType 返回数据库类型
	GetDBType() DatabaseType
	// GetConnectionString 返回连接字符串
	GetConnectionString() string
}

// DatabaseConfig 数据库配置，对齐 Python DatabaseConfig
type DatabaseConfig struct {
	// DBType 数据库类型
	DBType DatabaseType `json:"db_type"`
	// ConnectionString 连接字符串
	ConnectionString string `json:"connection_string"`
	// DBTimeout 数据库超时秒数
	DBTimeout int `json:"db_timeout"`
	// DBEnableWAL 是否启用 WAL 模式
	DBEnableWAL bool `json:"db_enable_wal"`
}

// MemoryDatabaseConfig 内存数据库配置
type MemoryDatabaseConfig struct {
	// DBType 数据库类型
	DBType DatabaseType `json:"db_type"`
	// ConnectionString 连接字符串
	ConnectionString string `json:"connection_string"`
}

// ──────────────────────────── 枚举 ────────────────────────────

// DatabaseType 数据库类型枚举
type DatabaseType string

const (
	// DatabaseTypeSQLite SQLite 数据库
	DatabaseTypeSQLite DatabaseType = "sqlite"
	// DatabaseTypePostgreSQL PostgreSQL 数据库
	DatabaseTypePostgreSQL DatabaseType = "postgresql"
	// DatabaseTypeMySQL MySQL 数据库
	DatabaseTypeMySQL DatabaseType = "mysql"
	// DatabaseTypeMemory 内存数据库
	DatabaseTypeMemory DatabaseType = "memory"
)

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// NewDatabaseConfig 创建默认数据库配置。
// 默认值：db_type=sqlite, connection_string="", db_timeout=30, db_enable_wal=true
func NewDatabaseConfig() DatabaseConfig {
	return DatabaseConfig{
		DBType:      DatabaseTypeSQLite,
		DBTimeout:   30,
		DBEnableWAL: true,
	}
}

// NewMemoryDatabaseConfig 创建默认内存数据库配置。
// 默认值：db_type=memory, connection_string=""
func NewMemoryDatabaseConfig() MemoryDatabaseConfig {
	return MemoryDatabaseConfig{
		DBType: DatabaseTypeMemory,
	}
}

// GetDBType 返回数据库类型。实现 DBConfigProvider 接口。
func (c DatabaseConfig) GetDBType() DatabaseType { return c.DBType }

// GetConnectionString 返回连接字符串。实现 DBConfigProvider 接口。
func (c DatabaseConfig) GetConnectionString() string { return c.ConnectionString }

// GetDBType 返回数据库类型。实现 DBConfigProvider 接口。
func (c MemoryDatabaseConfig) GetDBType() DatabaseType { return c.DBType }

// GetConnectionString 返回连接字符串。实现 DBConfigProvider 接口。
func (c MemoryDatabaseConfig) GetConnectionString() string { return c.ConnectionString }

// ──────────────────────────── 非导出函数 ────────────────────────────

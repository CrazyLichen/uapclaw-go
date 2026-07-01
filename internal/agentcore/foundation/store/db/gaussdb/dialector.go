package gaussdb

import (
	"strings"

	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"gorm.io/gorm/migrator"
	"gorm.io/gorm/schema"
)

// ──────────────────────────── 结构体 ────────────────────────────

// GaussDialector GaussDB 数据库方言，基于 PostgreSQL 方言扩展。
//
// 对应 Python: openjiuwen/extensions/store/db/gauss_dialect.py (GaussDialectAsyncpg)
//
// GaussDB 与 PostgreSQL 的主要差异：
//   - 不支持 NOWAIT / SKIP LOCKED 锁选项
//   - 不支持原生 ENUM / UUID 类型
//   - 非 string 值绑定到 string 列时需要自动转换
//
// 注意：postgres.Dialector 的所有方法均使用值接收者，
// 因此 GaussDialector 的覆写方法也必须使用值接收者。
//
// Python 独有概念（GORM 无对应，已跳过）：
//   - driver = 'async_gaussdb'：Python 显式声明驱动名，Go 使用 pgx 驱动替代，
//     通过 Name() 返回 "gaussdb" 标识方言
//   - supports_statement_cache / use_insertmanyvalues：SQLAlchemy 连接池配置项，
//     GORM 无对应概念，由底层 pgx 驱动自行管理
//   - _get_server_version_info：Python 检测数据库版本用于条件性 SQL 生成，
//     GORM 无版本检测机制，依赖 postgres.Dialector 的默认行为
type GaussDialector struct {
	postgres.Dialector // 值嵌入；所有方法使用值接收者，与 postgres.Dialector 一致
}

// ──────────────────────────── 常量 ────────────────────────────
const (
	// gaussLogComponent 日志组件
	gaussLogComponent = logger.ComponentAgentCore
)

// ──────────────────────────── 导出函数 ────────────────────────────

// GaussOpen 使用 DSN 创建 GaussDialector。
// 对标 Python: dialect 注册入口 "gaussdb"
func GaussOpen(dsn string) gorm.Dialector {
	return GaussDialector{Dialector: postgres.Dialector{Config: &postgres.Config{DSN: dsn}}}
}

// GaussNew 使用配置创建 GaussDialector。
func GaussNew(config postgres.Config) gorm.Dialector {
	return GaussDialector{Dialector: postgres.Dialector{Config: &config}}
}

// Name 返回方言名称 "gaussdb"。
// 对标 Python: GaussDialectAsyncpg.name = 'gaussdb'
// 注意：必须使用值接收者，否则嵌入的 postgres.Dialector.Name() 会被优先调用。
func (dialector GaussDialector) Name() string {
	return "gaussdb"
}

// Initialize 初始化 GaussDB 方言。
// 对标 Python: GaussDialectAsyncpg.import_dbapi() + GaussCompiler 注册
//
// 流程：
//  1. 调用 postgres.Dialector.Initialize(db) 完成基础 PG 初始化
//  2. 注册 "FOR" ClauseBuilder → 覆写 LOCKING 子句（忽略 NOWAIT/SKIP LOCKED）
//  3. 注册 gauss_string Serializer → 自动转换非 string 绑定值
//  4. 日志记录初始化完成
func (dialector GaussDialector) Initialize(db *gorm.DB) error {
	// 第 1 步：委托 postgres 初始化（连接池、回调注册等）
	if err := dialector.Dialector.Initialize(db); err != nil {
		logger.Error(gaussLogComponent).Err(err).Msg("GaussDB 方言初始化失败")
		return err
	}

	// 第 2 步：注册 LOCKING 子句构建器
	// 对标 Python: GaussCompiler.for_update_clause() 忽略 NOWAIT/SKIP LOCKED
	if db.ClauseBuilders == nil {
		db.ClauseBuilders = make(map[string]clause.ClauseBuilder)
	}
	db.ClauseBuilders["FOR"] = gaussLockingClauseBuilder

	// 第 3 步：注册 gauss_string 序列化器
	// 对标 Python: GaussString.bind_processor()
	schema.RegisterSerializer("gauss_string", gaussStringSerializer{})

	// 第 4 步：日志记录
	logger.Info(gaussLogComponent).Str("dialect", "gaussdb").Msg("GaussDB 方言初始化完成")

	return nil
}

// DataTypeOf 返回 GaussDB 中的字段类型映射。
// 对标 Python: supports_native_uuid = False, supports_native_enum = False
//
// 覆写规则：
//   - UUID 类型 → varchar(36)（GaussDB 不支持原生 UUID）
//   - ENUM 类型 → varchar（GaussDB 不支持原生 ENUM）
//   - 其他 → 委托 postgres.Dialector.DataTypeOf()
func (dialector GaussDialector) DataTypeOf(field *schema.Field) string {
	dataType := strings.ToLower(string(field.DataType))
	switch {
	case strings.Contains(dataType, "uuid"):
		return "varchar(36)"
	case strings.Contains(dataType, "enum"):
		return "varchar"
	default:
		return dialector.Dialector.DataTypeOf(field)
	}
}

// Migrator 返回 GaussMigrator 实例。
// 对标 Python: _domain_query / _enum_query → SELECT 1 WHERE FALSE
func (dialector GaussDialector) Migrator(db *gorm.DB) gorm.Migrator {
	return GaussMigrator{Migrator: postgres.Migrator{
		Migrator: migrator.Migrator{
			Config: migrator.Config{
				DB:                          db,
				Dialector:                   dialector,
				CreateIndexAfterCreateTable: true,
			},
		},
	}}
}

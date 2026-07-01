package query

import (
	"sync"

	"github.com/uapclaw/uapclaw-go/internal/common/exception"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// QueryLanguageDefinition 数据库查询语言定义//
// 每个数据库后端需注册一个 QueryLanguageDefinition 实例，
// 包含 8 个转换回调函数，分别处理不同类型的查询表达式。
// 对应 Python: QueryLanguageDefinition
type QueryLanguageDefinition struct {
	// Comparison 比较表达式转换
	Comparison func(QueryExpr) (any, error)
	// Range 范围表达式转换
	Range func(QueryExpr) (any, error)
	// Arithmetic 算术表达式转换
	Arithmetic func(QueryExpr) (any, error)
	// Null 空值表达式转换
	Null func(QueryExpr) (any, error)
	// JSONFilter JSON 字段表达式转换
	JSONFilter func(QueryExpr) (any, error)
	// Array 数组表达式转换
	Array func(QueryExpr) (any, error)
	// Logical 逻辑表达式转换
	Logical func(QueryExpr) (any, error)
	// TextMatch 文本匹配表达式转换
	TextMatch func(QueryExpr) (any, error)
}

// ──────────────────────────── 全局变量 ────────────────────────────
var (
	// queryExprFunctions 查询语言注册表
	queryExprFunctions = make(map[string]QueryLanguageDefinition)
	// queryExprMu 注册表读写锁
	queryExprMu sync.RWMutex
	// logComponent 日志组件
	logComponent = logger.ComponentAgentCore
)

// ──────────────────────────── 导出函数 ────────────────────────────

// RegisterDatabaseQueryLanguage 注册数据库查询语言定义
//
// name 为后端名称（如 "milvus"、"chroma"），def 为查询语言定义，
// force 为 true 时允许覆盖已注册的同名后端。
// 对应 Python: register_database_query_language()
func RegisterDatabaseQueryLanguage(name string, def QueryLanguageDefinition, force bool) error {
	queryExprMu.Lock()
	defer queryExprMu.Unlock()

	err := registerDatabaseQueryLanguage(queryExprFunctions, name, def, force)
	if err != nil {
		return err
	}

	logger.Info(logComponent).
		Str("database", name).
		Msg("注册查询表达式支持")
	return nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// registerDatabaseQueryLanguage 内部注册实现（便于测试）
func registerDatabaseQueryLanguage(m map[string]QueryLanguageDefinition, name string, def QueryLanguageDefinition, force bool) error {
	if _, ok := m[name]; ok && !force {
		return exception.BuildError(exception.StatusRetrievalVectorStoreQueryInvalid,
			exception.WithParam("error_msg", "Database query language for "+name+" already registered"))
	}
	m[name] = def
	return nil
}

// validateLanguageRegistered 校验后端查询语言已注册
//
// 对应 Python: validate_language_registered()
func validateLanguageRegistered(name string) error {
	queryExprMu.RLock()
	_, ok := queryExprFunctions[name]
	queryExprMu.RUnlock()

	if !ok {
		return exception.BuildError(exception.StatusRetrievalVectorStoreQueryInvalid,
			exception.WithParam("error_msg", "Database query language "+name+" not registered via RegisterDatabaseQueryLanguage method"))
	}
	return nil
}

// getQueryLanguageDefinition 获取已注册的查询语言定义
func getQueryLanguageDefinition(name string) (QueryLanguageDefinition, bool) {
	queryExprMu.RLock()
	defer queryExprMu.RUnlock()
	def, ok := queryExprFunctions[name]
	return def, ok
}

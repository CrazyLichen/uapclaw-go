package query

import (
	"fmt"
	"strings"

	"github.com/uapclaw/uapclaw-go/internal/common/exception"
)

// ──────────────────────────── 结构体 ────────────────────────────

// QueryExpr 查询过滤表达式接口
//
// 所有查询表达式（Comparison/Range/Arithmetic/Null/JSON/Array/Logical/Match/Custom）
// 均实现此接口。通过 ToExpr 方法将表达式转换为数据库特定格式。
// Milvus 后端返回 string，Chroma 后端返回 map[string]any。
// 对应 Python: QueryExpr
type QueryExpr interface {
	// ToExpr 将过滤表达式转换为后端特定格式
	ToExpr(backend string) (any, error)
}

// ComparisonExpr 比较表达式（==, !=, >, <, >=, <=）
//
// 对应 Python: ComparisonExpr
type ComparisonExpr struct {
	// Field 字段名
	Field string
	// Operator 比较运算符
	Operator string
	// Value 比较值
	Value any
}

// RangeExpr 范围表达式（in, like）
//
// 对应 Python: RangeExpr
type RangeExpr struct {
	// Field 字段名
	Field string
	// Operator 范围运算符（in 或 like）
	Operator string
	// Value 范围值（in 操作为列表，like 操作为模式字符串）
	Value any
}

// ArithmeticExpr 算术表达式（字段 + - * / % ** 比较值）
//
// 对应 Python: ArithmeticExpr
type ArithmeticExpr struct {
	// Field 字段名
	Field string
	// ArithmeticOperator 算术运算符（+, -, *, /, %, **）
	ArithmeticOperator string
	// ArithmeticValue 算术操作值
	ArithmeticValue float64
	// ComparisonOperator 算术后的比较运算符
	ComparisonOperator string
	// ComparisonValue 比较值
	ComparisonValue float64
}

// NullExpr 空值检查表达式（is null / is not null）
//
// 对应 Python: NullExpr
type NullExpr struct {
	// Field 字段名
	Field string
	// IsNull true 为 is null，false 为 is not null
	IsNull bool
}

// JSONExpr JSON 字段查询表达式
//
// 对应 Python: JSONExpr
type JSONExpr struct {
	// Field JSON 字段名
	Field string
	// Key JSON 键名
	Key string
	// Operator 比较运算符
	Operator string
	// Value 比较值
	Value any
}

// ArrayExpr 数组字段查询表达式
//
// 对应 Python: ArrayExpr
type ArrayExpr struct {
	// Field 数组字段名
	Field string
	// Index 数组索引（nil 表示操作整个数组字段）
	Index *int
	// Operator 比较运算符
	Operator string
	// Value 比较值
	Value any
}

// LogicalExpr 逻辑组合表达式（and, or, not, xor）
//
// 对应 Python: LogicalExpr
type LogicalExpr struct {
	// Operator 逻辑运算符（and, or, not, xor）
	Operator string
	// Left 左操作数
	Left QueryExpr
	// Right 右操作数（not 运算符不需要右操作数）
	Right QueryExpr
}

// MatchExpr 文本匹配表达式（prefix, suffix, infix, exact）
//
// 对应 Python: MatchExpr
type MatchExpr struct {
	// Field 字段名
	Field string
	// Value 文本值
	Value string
	// MatchMode 匹配模式（prefix, suffix, infix, exact）
	MatchMode string
}

// CustomExpr 自定义原始表达式
//
// 对应 Python: CustomExpr
type CustomExpr struct {
	// Expr 自定义表达式（可为任意类型）
	Expr any
}

// ──────────────────────────── 常量 ────────────────────────────
const (
	// MatchModeExact 精确匹配
	MatchModeExact = "exact"
	// MatchModePrefix 前缀匹配
	MatchModePrefix = "prefix"
	// MatchModeSuffix 后缀匹配
	MatchModeSuffix = "suffix"
	// MatchModeInfix 包含匹配
	MatchModeInfix = "infix"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// SanitizeStr 转义字符串值中的双引号并加上引号包裹
//
// 对应 Python: QueryExpr.sanitize_str()
func SanitizeStr(value any) string {
	s := fmt.Sprintf("%v", value)
	if strings.Contains(s, `"`) {
		escaped := strings.ReplaceAll(s, `"`, `\"`)
		return `"` + escaped + `"`
	}
	return `"` + s + `"`
}

// ToExpr 实现 QueryExpr 接口 — CustomExpr
func (e *CustomExpr) ToExpr(_ string) (any, error) {
	return e.Expr, nil
}

// ToExpr 实现 QueryExpr 接口 — ComparisonExpr
func (e *ComparisonExpr) ToExpr(backend string) (any, error) {
	if err := validateLanguageRegistered(backend); err != nil {
		return nil, err
	}
	def, _ := getQueryLanguageDefinition(backend)
	return def.Comparison(e)
}

// ToExpr 实现 QueryExpr 接口 — RangeExpr
func (e *RangeExpr) ToExpr(backend string) (any, error) {
	if err := validateLanguageRegistered(backend); err != nil {
		return nil, err
	}
	def, _ := getQueryLanguageDefinition(backend)
	return def.Range(e)
}

// ToExpr 实现 QueryExpr 接口 — ArithmeticExpr
func (e *ArithmeticExpr) ToExpr(backend string) (any, error) {
	if err := validateLanguageRegistered(backend); err != nil {
		return nil, err
	}
	def, _ := getQueryLanguageDefinition(backend)
	return def.Arithmetic(e)
}

// ToExpr 实现 QueryExpr 接口 — NullExpr
func (e *NullExpr) ToExpr(backend string) (any, error) {
	if err := validateLanguageRegistered(backend); err != nil {
		return nil, err
	}
	def, _ := getQueryLanguageDefinition(backend)
	return def.Null(e)
}

// ToExpr 实现 QueryExpr 接口 — JSONExpr
func (e *JSONExpr) ToExpr(backend string) (any, error) {
	if err := validateLanguageRegistered(backend); err != nil {
		return nil, err
	}
	def, _ := getQueryLanguageDefinition(backend)
	return def.JSONFilter(e)
}

// ToExpr 实现 QueryExpr 接口 — ArrayExpr
func (e *ArrayExpr) ToExpr(backend string) (any, error) {
	if err := validateLanguageRegistered(backend); err != nil {
		return nil, err
	}
	def, _ := getQueryLanguageDefinition(backend)
	return def.Array(e)
}

// ToExpr 实现 QueryExpr 接口 — LogicalExpr
func (e *LogicalExpr) ToExpr(backend string) (any, error) {
	if err := validateLanguageRegistered(backend); err != nil {
		return nil, err
	}
	def, _ := getQueryLanguageDefinition(backend)
	return def.Logical(e)
}

// ToExpr 实现 QueryExpr 接口 — MatchExpr
func (e *MatchExpr) ToExpr(backend string) (any, error) {
	if err := validateLanguageRegistered(backend); err != nil {
		return nil, err
	}
	def, _ := getQueryLanguageDefinition(backend)
	return def.TextMatch(e)
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// raiseQueryError 构造查询错误
//
// 对应 Python: raise_query_error()
func raiseQueryError(reason string) error {
	return exception.BuildError(exception.StatusRetrievalVectorStoreQueryInvalid,
		exception.WithParam("error_msg", reason))
}

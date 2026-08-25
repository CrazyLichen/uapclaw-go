package query

import (
	"fmt"
	"strings"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// chromaOperatorMap Chroma 比较运算符映射
//
// 对应 Python: OPERATOR_MAP
var chromaOperatorMap = map[string]string{
	"==": "$eq",
	"!=": "$nin",
	">":  "$gt",
	">=": "$gte",
	"<":  "$lt",
	"<=": "$lte",
}

// chromaDef Chroma 查询语言定义
//
// 对应 Python: chroma_def
var chromaDef = QueryLanguageDefinition{
	Comparison: chromaComparisonFilter,
	Range:      chromaRangeFilter,
	Arithmetic: chromaArithmeticFilter,
	Null:       chromaNullFilter,
	JSONFilter: chromaJSONFilter,
	Array:      chromaArrayFilter,
	Logical:    chromaLogicalFilter,
	TextMatch:  chromaTextMatchFilter,
}

// ──────────────────────────── 导出函数 ────────────────────────────

// ──────────────────────────── 非导出函数 ────────────────────────────

// chromaComparisonFilter 将比较表达式转换为 Chroma where 过滤字典
//
// 对应 Python: chroma_comparison_filter()
func chromaComparisonFilter(expr QueryExpr) (any, error) {
	e := expr.(*ComparisonExpr)
	chromaOp, ok := chromaOperatorMap[e.Operator]
	if !ok {
		return nil, raiseQueryError(fmt.Sprintf("不支持的比较运算符: %s", e.Operator))
	}

	whereFilter := make(map[string]any)
	switch chromaOp {
	case "$eq":
		whereFilter[e.Field] = e.Value
	case "$nin":
		whereFilter[e.Field] = map[string]any{chromaOp: []any{e.Value}}
	default:
		whereFilter[e.Field] = map[string]any{chromaOp: e.Value}
	}

	return map[string]any{
		"where":          whereFilter,
		"where_document": map[string]any{},
	}, nil
}

// chromaRangeFilter 将范围表达式转换为 Chroma where 过滤字典
//
// 对应 Python: chroma_range_filter()
func chromaRangeFilter(expr QueryExpr) (any, error) {
	e := expr.(*RangeExpr)
	whereFilter := make(map[string]any)

	switch strings.ToLower(e.Operator) {
	case "in":
		values, ok := toSlice(e.Value)
		if !ok {
			return nil, raiseQueryError("in 操作符需要序列或集合值")
		}
		whereFilter[e.Field] = map[string]any{"$in": values}
	default:
		return nil, raiseQueryError(fmt.Sprintf("不支持的范围运算符: %s", e.Operator))
	}

	return map[string]any{
		"where":          whereFilter,
		"where_document": map[string]any{},
	}, nil
}

// chromaArithmeticFilter Chroma 不支持算术操作
//
// 对应 Python: chroma_arithmetic_filter()
func chromaArithmeticFilter(_ QueryExpr) (any, error) {
	return nil, raiseQueryError(
		"Chroma 不支持元数据过滤中的算术操作。" +
			"请考虑预计算算术结果并将其存储为元数据字段。")
}

// chromaNullFilter Chroma 不支持 null 操作
//
// 对应 Python: chroma_null_filter()
func chromaNullFilter(_ QueryExpr) (any, error) {
	return nil, raiseQueryError(
		"Chroma 不支持元数据中的嵌套 JSON 字段。" +
			"Chroma 仅支持扁平元数据（str, int, float, bool, None）。" +
			"请考虑扁平化元数据结构（如 'user.name' → 'user_name'）。")
}

// chromaJSONFilter Chroma 不支持 JSON 字段操作
//
// 对应 Python: chroma_json_filter()
func chromaJSONFilter(_ QueryExpr) (any, error) {
	return nil, raiseQueryError(
		"Chroma 不支持元数据中的嵌套 JSON 字段。" +
			"Chroma 仅支持扁平元数据（str, int, float, bool, None）。" +
			"请考虑扁平化元数据结构（如 'user.name' → 'user_name'）。")
}

// chromaArrayFilter Chroma 不支持数组索引操作
//
// 对应 Python: chroma_array_filter()
func chromaArrayFilter(_ QueryExpr) (any, error) {
	return nil, raiseQueryError(
		"Chroma 不支持元数据中的数组索引。" +
			"Chroma 仅支持扁平元数据（str, int, float, bool, None）。" +
			"请考虑扁平化数组结构（如 'tags[0]' → 'tag_0'）。")
}

// chromaLogicalFilter 将逻辑表达式转换为 Chroma where/where_document 过滤字典
//
// 对应 Python: chroma_logical_filter()
func chromaLogicalFilter(expr QueryExpr) (any, error) {
	e := expr.(*LogicalExpr)

	// 对齐 Python: "not" 操作符不被 Chroma 支持
	if strings.ToLower(e.Operator) == "not" {
		return nil, raiseQueryError("不支持逻辑操作符: not")
	}

	if e.Right == nil {
		return nil, raiseQueryError(fmt.Sprintf("%s 操作符需要左右两个操作数", strings.ToLower(e.Operator)))
	}

	leftResult, err := e.Left.ToExpr("chroma")
	if err != nil {
		return nil, err
	}
	rightResult, err := e.Right.ToExpr("chroma")
	if err != nil {
		return nil, err
	}

	leftMap := leftResult.(map[string]any)
	rightMap := rightResult.(map[string]any)

	leftWhere := toMapAny(leftMap["where"])
	leftWhereDoc := toMapAny(leftMap["where_document"])
	rightWhere := toMapAny(rightMap["where"])
	rightWhereDoc := toMapAny(rightMap["where_document"])

	var whereFilter, whereDocFilter map[string]any

	switch strings.ToLower(e.Operator) {
	case "and":
		whereFilter = combineFilters(leftWhere, rightWhere, "$and")
		whereDocFilter = combineFilters(leftWhereDoc, rightWhereDoc, "$and")
	case "or":
		whereFilter = combineFilters(leftWhere, rightWhere, "$or")
		whereDocFilter = combineFilters(leftWhereDoc, rightWhereDoc, "$or")
	default:
		return nil, raiseQueryError(fmt.Sprintf("不支持的逻辑操作符: %s", e.Operator))
	}

	return map[string]any{
		"where":          whereFilter,
		"where_document": whereDocFilter,
	}, nil
}

// chromaTextMatchFilter 将文本匹配表达式转换为 Chroma where_document 过滤字典
//
// 对应 Python: chroma_text_match_filter()
func chromaTextMatchFilter(expr QueryExpr) (any, error) {
	e := expr.(*MatchExpr)
	whereDocFilter := make(map[string]any)

	switch strings.ToLower(e.MatchMode) {
	case MatchModeExact:
		whereDocFilter["$contains"] = e.Value
	case MatchModePrefix:
		whereDocFilter["$regex"] = "^" + e.Value
	case MatchModeSuffix:
		whereDocFilter["$regex"] = e.Value + "$"
	case MatchModeInfix:
		whereDocFilter["$contains"] = e.Value
	default:
		return nil, raiseQueryError(fmt.Sprintf("未知匹配模式: %s", e.MatchMode))
	}

	return map[string]any{
		"where":          map[string]any{},
		"where_document": whereDocFilter,
	}, nil
}

// combineFilters 组合两个过滤字典
func combineFilters(left, right map[string]any, op string) map[string]any {
	if len(left) > 0 && len(right) > 0 {
		return map[string]any{op: []any{left, right}}
	}
	if len(left) > 0 {
		return left
	}
	if len(right) > 0 {
		return right
	}
	return map[string]any{}
}

// toMapAny 将 any 转为 map[string]any
func toMapAny(v any) map[string]any {
	if v == nil {
		return map[string]any{}
	}
	if m, ok := v.(map[string]any); ok {
		return m
	}
	return map[string]any{}
}

// init 注册 Chroma 查询语言
func init() {
	if err := RegisterDatabaseQueryLanguage("chroma", chromaDef, false); err != nil {
		panic(fmt.Sprintf("注册 Chroma 查询语言失败: %v", err))
	}
}

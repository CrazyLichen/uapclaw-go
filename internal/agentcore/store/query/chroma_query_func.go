package query

import (
	"fmt"
	"strings"
)

// ──────────────────────────── 常量 ────────────────────────────

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

// ──────────────────────────── 非导出函数 ────────────────────────────

// chromaComparisonFilter 将比较表达式转换为 Chroma where 过滤字典
//
// 对应 Python: chroma_comparison_filter()
func chromaComparisonFilter(expr QueryExpr) (any, error) {
	e := expr.(*ComparisonExpr)
	chromaOp, ok := chromaOperatorMap[e.Operator]
	if !ok {
		return nil, raiseQueryError(fmt.Sprintf("Unsupported comparison operator: %s", e.Operator))
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
			return nil, raiseQueryError("in operator requires a sequence or set value")
		}
		whereFilter[e.Field] = map[string]any{"$in": values}
	default:
		return nil, raiseQueryError(fmt.Sprintf("Unsupported range operator: %s", e.Operator))
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
		"Chroma does not support arithmetic operations in metadata filters. " +
			"Consider pre-computing the arithmetic result and storing it as a metadata field.")
}

// chromaNullFilter Chroma 不支持 null 操作
//
// 对应 Python: chroma_null_filter()
func chromaNullFilter(_ QueryExpr) (any, error) {
	return nil, raiseQueryError(
		"Chroma does not support nested JSON fields in metadata. " +
			"Chroma only supports flat metadata (str, int, float, bool, None). " +
			"Consider flattening your metadata structure (e.g., 'user.name' -> 'user_name').")
}

// chromaJSONFilter Chroma 不支持 JSON 字段操作
//
// 对应 Python: chroma_json_filter()
func chromaJSONFilter(_ QueryExpr) (any, error) {
	return nil, raiseQueryError(
		"Chroma does not support nested JSON fields in metadata. " +
			"Chroma only supports flat metadata (str, int, float, bool, None). " +
			"Consider flattening your metadata structure (e.g., 'user.name' -> 'user_name').")
}

// chromaArrayFilter Chroma 不支持数组索引操作
//
// 对应 Python: chroma_array_filter()
func chromaArrayFilter(_ QueryExpr) (any, error) {
	return nil, raiseQueryError(
		"Chroma does not support array indexing in metadata. " +
			"Chroma only supports flat metadata (str, int, float, bool, None). " +
			"Consider flattening your array structure (e.g., 'tags[0]' -> 'tag_0').")
}

// chromaLogicalFilter 将逻辑表达式转换为 Chroma where/where_document 过滤字典
//
// 对应 Python: chroma_logical_filter()
func chromaLogicalFilter(expr QueryExpr) (any, error) {
	e := expr.(*LogicalExpr)

	if e.Right == nil {
		return nil, raiseQueryError(fmt.Sprintf("%s operator requires both left and right operands", strings.ToLower(e.Operator)))
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
		return nil, raiseQueryError(fmt.Sprintf("Unsupported logical operator: %s", e.Operator))
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
		return nil, raiseQueryError(fmt.Sprintf("Unknown match mode: %s", e.MatchMode))
	}

	return map[string]any{
		"where":          map[string]any{},
		"where_document": whereDocFilter,
	}, nil
}

// chromaDef Chroma 查询语言定义
//
// 对应 Python: chroma_def
var chromaDef = QueryLanguageDefinition{
	Comparison:  chromaComparisonFilter,
	Range:       chromaRangeFilter,
	Arithmetic:  chromaArithmeticFilter,
	Null:        chromaNullFilter,
	JSONFilter:  chromaJSONFilter,
	Array:       chromaArrayFilter,
	Logical:     chromaLogicalFilter,
	TextMatch:   chromaTextMatchFilter,
}

// ──────────────────────────── 辅助函数 ────────────────────────────

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

func init() {
	_ = RegisterDatabaseQueryLanguage("chroma", chromaDef, false)
}

package query

import (
	"fmt"
	"strings"
)

// ──────────────────────────── 全局变量 ────────────────────────────

// milvusDef Milvus 查询语言定义
//
// 对应 Python: milvus_def
var milvusDef = QueryLanguageDefinition{
	Comparison:  milvusComparisonFilter,
	Range:       milvusRangeFilter,
	Arithmetic:  milvusArithmeticFilter,
	Null:        milvusNullFilter,
	JSONFilter:  milvusJSONFilter,
	Array:       milvusArrayFilter,
	Logical:     milvusLogicalFilter,
	TextMatch:   milvusTextMatchFilter,
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// milvusComparisonFilter 将比较表达式转换为 Milvus 过滤字符串
//
// 对应 Python: milvus_comparison_filter()
func milvusComparisonFilter(expr QueryExpr) (any, error) {
	e := expr.(*ComparisonExpr)
	if _, ok := e.Value.(string); ok {
		return fmt.Sprintf("%s %s %s", e.Field, e.Operator, SanitizeStr(e.Value)), nil
	}
	return fmt.Sprintf("%s %s %v", e.Field, e.Operator, e.Value), nil
}

// milvusRangeFilter 将范围表达式转换为 Milvus 过滤字符串
//
// 对应 Python: milvus_range_filter()
func milvusRangeFilter(expr QueryExpr) (any, error) {
	e := expr.(*RangeExpr)
	switch strings.ToLower(e.Operator) {
	case "in":
		values, ok := toSlice(e.Value)
		if !ok {
			return nil, raiseQueryError("in operator requires a sequence or set value")
		}
		if allStrings(values) {
			parts := make([]string, len(values))
			for i, v := range values {
				parts[i] = SanitizeStr(v)
			}
			return fmt.Sprintf("%s in [%s]", e.Field, strings.Join(parts, ",")), nil
		}
		parts := make([]string, len(values))
		for i, v := range values {
			parts[i] = fmt.Sprintf("%v", v)
		}
		return fmt.Sprintf("%s in [%s]", e.Field, strings.Join(parts, ",")), nil
	case "like":
		s, ok := e.Value.(string)
		if !ok {
			return nil, raiseQueryError("like operator requires a string value")
		}
		if !strings.Contains(s, "%") {
			return nil, raiseQueryError("Milvus's like operator uses % for wildcard matching")
		}
		return fmt.Sprintf("%s like %s", e.Field, SanitizeStr(s)), nil
	default:
		return nil, raiseQueryError(fmt.Sprintf("Unsupported range operator: %s", e.Operator))
	}
}

// milvusArithmeticFilter 将算术表达式转换为 Milvus 过滤字符串
//
// 对应 Python: milvus_arithmetic_filter()
func milvusArithmeticFilter(expr QueryExpr) (any, error) {
	e := expr.(*ArithmeticExpr)
	return fmt.Sprintf("%s %s %v%s %v",
		e.Field, e.ArithmeticOperator, e.ArithmeticValue,
		e.ComparisonOperator, e.ComparisonValue), nil
}

// milvusNullFilter 将空值表达式转换为 Milvus 过滤字符串
//
// 对应 Python: milvus_null_filter()
func milvusNullFilter(expr QueryExpr) (any, error) {
	e := expr.(*NullExpr)
	if e.IsNull {
		return fmt.Sprintf("%s is null", e.Field), nil
	}
	return fmt.Sprintf("%s is not null", e.Field), nil
}

// milvusJSONFilter 将 JSON 表达式转换为 Milvus 过滤字符串
//
// 对应 Python: milvus_json_filter()
func milvusJSONFilter(expr QueryExpr) (any, error) {
	e := expr.(*JSONExpr)
	if _, ok := e.Value.(string); ok {
		return fmt.Sprintf("%s[%s] %s %s",
			e.Field, SanitizeStr(e.Key), e.Operator, SanitizeStr(e.Value)), nil
	}
	return fmt.Sprintf("%s[%s] %s %v",
		e.Field, SanitizeStr(e.Key), e.Operator, e.Value), nil
}

// milvusArrayFilter 将数组表达式转换为 Milvus 过滤字符串
//
// 对应 Python: milvus_array_filter()
func milvusArrayFilter(expr QueryExpr) (any, error) {
	e := expr.(*ArrayExpr)
	if e.Index != nil {
		if _, ok := e.Value.(string); ok {
			return fmt.Sprintf("%s[%d] %s %s",
				e.Field, *e.Index, e.Operator, SanitizeStr(e.Value)), nil
		}
		return fmt.Sprintf("%s[%d] %s %v",
			e.Field, *e.Index, e.Operator, e.Value), nil
	}
	if _, ok := e.Value.(string); ok {
		return fmt.Sprintf("%s %s %s",
			e.Field, e.Operator, SanitizeStr(e.Value)), nil
	}
	return fmt.Sprintf("%s %s %v",
		e.Field, e.Operator, e.Value), nil
}

// milvusLogicalFilter 将逻辑表达式转换为 Milvus 过滤字符串
//
// 对应 Python: milvus_logical_filter()
func milvusLogicalFilter(expr QueryExpr) (any, error) {
	e := expr.(*LogicalExpr)
	switch strings.ToLower(e.Operator) {
	case "not":
		if e.Right != nil {
			return nil, raiseQueryError("not operator should not have a right operand")
		}
		left, err := e.Left.ToExpr("milvus")
		if err != nil {
			return nil, err
		}
		return fmt.Sprintf("not (%s)", left), nil
	case "and", "or":
		if e.Right == nil {
			return nil, raiseQueryError(fmt.Sprintf("%s operator requires both left and right operands", e.Operator))
		}
		left, err := e.Left.ToExpr("milvus")
		if err != nil {
			return nil, err
		}
		right, err := e.Right.ToExpr("milvus")
		if err != nil {
			return nil, err
		}
		return fmt.Sprintf("(%s) %s (%s)", left, strings.ToLower(e.Operator), right), nil
	default:
		return nil, raiseQueryError(fmt.Sprintf("Unsupported logical operator: %s", e.Operator))
	}
}

// milvusTextMatchFilter 将文本匹配表达式转换为 Milvus 过滤字符串
//
// 对应 Python: milvus_text_match_filter()
func milvusTextMatchFilter(expr QueryExpr) (any, error) {
	e := expr.(*MatchExpr)
	pattern := e.Value
	switch strings.ToLower(e.MatchMode) {
	case MatchModeExact:
		return fmt.Sprintf("TEXT_MATCH(%s, %s)", e.Field, SanitizeStr(pattern)), nil
	case MatchModePrefix:
		return fmt.Sprintf("%s like %s", e.Field, SanitizeStr(pattern+"%")), nil
	case MatchModeSuffix:
		return fmt.Sprintf("%s like %s", e.Field, SanitizeStr("%"+pattern)), nil
	case MatchModeInfix:
		return fmt.Sprintf("%s like %s", e.Field, SanitizeStr("%"+pattern+"%")), nil
	default:
		return nil, raiseQueryError(fmt.Sprintf("Unknown match mode: %s", e.MatchMode))
	}
}

// toSlice 尝试将 any 转为 []any 切片
func toSlice(v any) ([]any, bool) {
	switch val := v.(type) {
	case []any:
		return val, true
	case []string:
		result := make([]any, len(val))
		for i, s := range val {
			result[i] = s
		}
		return result, true
	case []int:
		result := make([]any, len(val))
		for i, n := range val {
			result[i] = n
		}
		return result, true
	case []float64:
		result := make([]any, len(val))
		for i, f := range val {
			result[i] = f
		}
		return result, true
	default:
		return nil, false
	}
}

// allStrings 检查切片中所有元素是否为字符串
func allStrings(values []any) bool {
	for _, v := range values {
		if _, ok := v.(string); !ok {
			return false
		}
	}
	return true
}

// init 注册 Milvus 查询语言
func init() {
	_ = RegisterDatabaseQueryLanguage("milvus", milvusDef, false)
}

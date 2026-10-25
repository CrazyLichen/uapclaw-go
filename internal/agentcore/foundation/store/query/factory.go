package query

import (
	"sort"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// EqMap 将 dict 全等值条件折叠为 And(Eq...) 链。
//
// 对齐 Python filters 直传 dict 的等值过滤语义（{key: value} → key == value）：
// 单条件输出单个 ComparisonExpr，多条件按 key 排序折叠为 And 链保证确定性。
// 空 map 或 nil 返回 nil（无过滤条件）。
func EqMap(m map[string]any) QueryExpr {
	if len(m) == 0 {
		return nil
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var result QueryExpr
	for _, k := range keys {
		eq := Eq(k, m[k])
		if result == nil {
			result = eq
		} else {
			result = And(result, eq)
		}
	}
	return result
}

// Eq 创建等值比较表达式
//
// Python: eq()
func Eq(field string, value any) *ComparisonExpr {
	return &ComparisonExpr{Field: field, Operator: "==", Value: value}
}

// Ne 创建不等比较表达式
//
// Python: ne()
func Ne(field string, value any) *ComparisonExpr {
	return &ComparisonExpr{Field: field, Operator: "!=", Value: value}
}

// Gt 创建大于比较表达式
//
// Python: gt()
func Gt(field string, value float64) *ComparisonExpr {
	return &ComparisonExpr{Field: field, Operator: ">", Value: value}
}

// Lt 创建小于比较表达式
//
// Python: lt()
func Lt(field string, value float64) *ComparisonExpr {
	return &ComparisonExpr{Field: field, Operator: "<", Value: value}
}

// Gte 创建大于等于比较表达式
//
// Python: gte()
func Gte(field string, value float64) *ComparisonExpr {
	return &ComparisonExpr{Field: field, Operator: ">=", Value: value}
}

// Lte 创建小于等于比较表达式
//
// Python: lte()
func Lte(field string, value float64) *ComparisonExpr {
	return &ComparisonExpr{Field: field, Operator: "<=", Value: value}
}

// InList 创建 in 范围表达式，单值时退化为等值比较
//
// Python: in_list()
func InList(field string, values []any) QueryExpr {
	if len(values) == 1 {
		return &ComparisonExpr{Field: field, Operator: "==", Value: values[0]}
	}
	return &RangeExpr{Field: field, Operator: "in", Value: values}
}

// WildcardMatch 创建通配符匹配范围表达式
//
// Python: wildcard_match(field, pattern, operator="wildcard")
// operator 参数默认为 "wildcard"，对齐 Python 签名。
func WildcardMatch(field string, pattern string, operator ...string) *RangeExpr {
	op := "wildcard"
	if len(operator) > 0 && operator[0] != "" {
		op = operator[0]
	}
	return &RangeExpr{Field: field, Operator: op, Value: pattern}
}

// IsNull 创建 IS NULL 表达式
//
// Python: is_null()
func IsNull(field string) *NullExpr {
	return &NullExpr{Field: field, IsNull: true}
}

// IsNotNull 创建 IS NOT NULL 表达式
//
// Python: is_not_null()
func IsNotNull(field string) *NullExpr {
	return &NullExpr{Field: field, IsNull: false}
}

// JSONKey 创建 JSON 键过滤表达式
//
// Python: json_key()
func JSONKey(field string, key string, operator string, value any) *JSONExpr {
	return &JSONExpr{Field: field, Key: key, Operator: operator, Value: value}
}

// ArrayIndex 创建数组索引过滤表达式
//
// Python: array_index()
func ArrayIndex(field string, index int, operator string, value any) *ArrayExpr {
	return &ArrayExpr{Field: field, Index: &index, Operator: operator, Value: value}
}

// FilterUser 创建单用户 ID 过滤表达式
//
// userID 为单个用户 ID，userIDField 为字段名，默认 "user_id"。
// Python: filter_user(str)
func FilterUser(userID string, userIDField string) QueryExpr {
	if userIDField == "" {
		userIDField = "user_id"
	}
	return Eq(userIDField, userID)
}

// FilterUsers 创建多用户 ID 过滤表达式
//
// userIDs 为用户 ID 列表，userIDField 为字段名，默认 "user_id"。
// Python: filter_user(List[str])
func FilterUsers(userIDs []string, userIDField string) QueryExpr {
	if userIDField == "" {
		userIDField = "user_id"
	}
	userIDAny := make([]any, len(userIDs))
	for i, s := range userIDs {
		userIDAny[i] = s
	}
	return InList(userIDField, userIDAny)
}

// ChainFilters 用 AND 链接多个表达式，空输入返回 nil
//
// Python: chain_filters()
func ChainFilters(filters []QueryExpr) QueryExpr {
	if len(filters) == 0 {
		return nil
	}
	result := filters[0]
	for i := 1; i < len(filters); i++ {
		result = And(result, filters[i])
	}
	return result
}

// And 创建逻辑与表达式
//
// Python: expr1 & expr2
func And(left, right QueryExpr) *LogicalExpr {
	return &LogicalExpr{Operator: "and", Left: left, Right: right}
}

// Or 创建逻辑或表达式
//
// Python: expr1 | expr2
func Or(left, right QueryExpr) *LogicalExpr {
	return &LogicalExpr{Operator: "or", Left: left, Right: right}
}

// Not 创建逻辑非表达式
//
// Python: ~expr
func Not(expr QueryExpr) *LogicalExpr {
	return &LogicalExpr{Operator: "not", Left: expr, Right: nil}
}

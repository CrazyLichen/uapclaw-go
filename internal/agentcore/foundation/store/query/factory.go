package query

// ──────────────────────────── 导出函数 ────────────────────────────

// Eq 创建等值比较表达式
//
// 对应 Python: eq()
func Eq(field string, value any) *ComparisonExpr {
	return &ComparisonExpr{Field: field, Operator: "==", Value: value}
}

// Ne 创建不等比较表达式
//
// 对应 Python: ne()
func Ne(field string, value any) *ComparisonExpr {
	return &ComparisonExpr{Field: field, Operator: "!=", Value: value}
}

// Gt 创建大于比较表达式
//
// 对应 Python: gt()
func Gt(field string, value float64) *ComparisonExpr {
	return &ComparisonExpr{Field: field, Operator: ">", Value: value}
}

// Lt 创建小于比较表达式
//
// 对应 Python: lt()
func Lt(field string, value float64) *ComparisonExpr {
	return &ComparisonExpr{Field: field, Operator: "<", Value: value}
}

// Gte 创建大于等于比较表达式
//
// 对应 Python: gte()
func Gte(field string, value float64) *ComparisonExpr {
	return &ComparisonExpr{Field: field, Operator: ">=", Value: value}
}

// Lte 创建小于等于比较表达式
//
// 对应 Python: lte()
func Lte(field string, value float64) *ComparisonExpr {
	return &ComparisonExpr{Field: field, Operator: "<=", Value: value}
}

// InList 创建 in 范围表达式，单值时退化为等值比较
//
// 对应 Python: in_list()
func InList(field string, values []any) QueryExpr {
	if len(values) == 1 {
		return &ComparisonExpr{Field: field, Operator: "==", Value: values[0]}
	}
	return &RangeExpr{Field: field, Operator: "in", Value: values}
}

// WildcardMatch 创建通配符匹配范围表达式
//
// 对应 Python: wildcard_match()
func WildcardMatch(field string, pattern string) *RangeExpr {
	return &RangeExpr{Field: field, Operator: "wildcard", Value: pattern}
}

// IsNull 创建 IS NULL 表达式
//
// 对应 Python: is_null()
func IsNull(field string) *NullExpr {
	return &NullExpr{Field: field, IsNull: true}
}

// IsNotNull 创建 IS NOT NULL 表达式
//
// 对应 Python: is_not_null()
func IsNotNull(field string) *NullExpr {
	return &NullExpr{Field: field, IsNull: false}
}

// JSONKey 创建 JSON 键过滤表达式
//
// 对应 Python: json_key()
func JSONKey(field string, key string, operator string, value any) *JSONExpr {
	return &JSONExpr{Field: field, Key: key, Operator: operator, Value: value}
}

// ArrayIndex 创建数组索引过滤表达式
//
// 对应 Python: array_index()
func ArrayIndex(field string, index int, operator string, value any) *ArrayExpr {
	return &ArrayExpr{Field: field, Index: &index, Operator: operator, Value: value}
}

// FilterUser 创建单用户 ID 过滤表达式
//
// userID 为单个用户 ID，userIDField 为字段名，默认 "user_id"。
// 对应 Python: filter_user(str)
func FilterUser(userID string, userIDField string) QueryExpr {
	if userIDField == "" {
		userIDField = "user_id"
	}
	return Eq(userIDField, userID)
}

// FilterUsers 创建多用户 ID 过滤表达式
//
// userIDs 为用户 ID 列表，userIDField 为字段名，默认 "user_id"。
// 对应 Python: filter_user(List[str])
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
// 对应 Python: chain_filters()
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
// 对应 Python: expr1 & expr2
func And(left, right QueryExpr) *LogicalExpr {
	return &LogicalExpr{Operator: "and", Left: left, Right: right}
}

// Or 创建逻辑或表达式
//
// 对应 Python: expr1 | expr2
func Or(left, right QueryExpr) *LogicalExpr {
	return &LogicalExpr{Operator: "or", Left: left, Right: right}
}

// Not 创建逻辑非表达式
//
// 对应 Python: ~expr
func Not(expr QueryExpr) *LogicalExpr {
	return &LogicalExpr{Operator: "not", Left: expr, Right: nil}
}

// Xor 创建逻辑异或表达式
//
// 对应 Python: expr1 ^ expr2
func Xor(left, right QueryExpr) *LogicalExpr {
	return &LogicalExpr{Operator: "xor", Left: left, Right: right}
}

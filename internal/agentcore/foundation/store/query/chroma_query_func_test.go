package query

import (
	"testing"
)

func setupChromaTest(t *testing.T) {
	t.Helper()
	queryExprMu.Lock()
	delete(queryExprFunctions, "chroma")
	queryExprMu.Unlock()
	err := RegisterDatabaseQueryLanguage("chroma", chromaDef, false)
	if err != nil {
		t.Fatalf("注册 chroma 后端失败: %v", err)
	}
}

// TestChromaComparison_Eq 测试 Chroma 等值比较
func TestChromaComparison_Eq(t *testing.T) {
	setupChromaTest(t)
	expr := Eq("name", "test")
	result, err := expr.ToExpr("chroma")
	if err != nil {
		t.Fatalf("不应返回错误: %v", err)
	}
	m, ok := result.(map[string]any)
	if !ok {
		t.Fatalf("Chroma 后端应返回 map[string]any，实际 %T", result)
	}
	where := m["where"].(map[string]any)
	if where["name"] != "test" {
		t.Errorf("where[name] = %v, want test", where["name"])
	}
}

// TestChromaComparison_Gt 测试 Chroma 大于比较
func TestChromaComparison_Gt(t *testing.T) {
	setupChromaTest(t)
	expr := Gt("age", 25)
	result, err := expr.ToExpr("chroma")
	if err != nil {
		t.Fatalf("不应返回错误: %v", err)
	}
	m := result.(map[string]any)
	where := m["where"].(map[string]any)
	fieldFilter := where["age"].(map[string]any)
	if fieldFilter["$gt"] != 25.0 {
		t.Errorf("where[age][$gt] = %v, want 25", fieldFilter["$gt"])
	}
}

// TestChromaComparison_Ne 测试 Chroma 不等比较
func TestChromaComparison_Ne(t *testing.T) {
	setupChromaTest(t)
	expr := Ne("status", "active")
	result, err := expr.ToExpr("chroma")
	if err != nil {
		t.Fatalf("不应返回错误: %v", err)
	}
	m := result.(map[string]any)
	where := m["where"].(map[string]any)
	fieldFilter := where["status"].(map[string]any)
	ninList, ok := fieldFilter["$nin"].([]any)
	if !ok {
		t.Fatalf("where[status][$nin] 应为 []any，实际 %T", fieldFilter["$nin"])
	}
	if ninList[0] != "active" {
		t.Errorf("$nin[0] = %v, want active", ninList[0])
	}
}

// TestChromaRange_In 测试 Chroma in 操作
func TestChromaRange_In(t *testing.T) {
	setupChromaTest(t)
	expr := InList("status", []any{"active", "pending"})
	result, err := expr.ToExpr("chroma")
	if err != nil {
		t.Fatalf("不应返回错误: %v", err)
	}
	m := result.(map[string]any)
	where := m["where"].(map[string]any)
	fieldFilter := where["status"].(map[string]any)
	inList, ok := fieldFilter["$in"].([]any)
	if !ok {
		t.Fatalf("where[status][$in] 应为 []any，实际 %T", fieldFilter["$in"])
	}
	if len(inList) != 2 {
		t.Errorf("$in 长度 = %d, want 2", len(inList))
	}
}

// TestChromaArithmetic_不支持 测试 Chroma 不支持算术
func TestChromaArithmetic_不支持(t *testing.T) {
	setupChromaTest(t)
	expr := &ArithmeticExpr{Field: "price", ArithmeticOperator: "+", ArithmeticValue: 10, ComparisonOperator: ">", ComparisonValue: 100}
	_, err := expr.ToExpr("chroma")
	if err == nil {
		t.Error("Chroma 不支持算术操作应返回错误")
	}
}

// TestChromaNull_不支持 测试 Chroma 不支持 null
func TestChromaNull_不支持(t *testing.T) {
	setupChromaTest(t)
	expr := IsNull("name")
	_, err := expr.ToExpr("chroma")
	if err == nil {
		t.Error("Chroma 不支持 null 操作应返回错误")
	}
}

// TestChromaJSON_不支持 测试 Chroma 不支持 JSON
func TestChromaJSON_不支持(t *testing.T) {
	setupChromaTest(t)
	expr := JSONKey("meta", "status", "==", "active")
	_, err := expr.ToExpr("chroma")
	if err == nil {
		t.Error("Chroma 不支持 JSON 操作应返回错误")
	}
}

// TestChromaArray_不支持 测试 Chroma 不支持数组
func TestChromaArray_不支持(t *testing.T) {
	setupChromaTest(t)
	expr := ArrayIndex("tags", 0, "==", "go")
	_, err := expr.ToExpr("chroma")
	if err == nil {
		t.Error("Chroma 不支持数组操作应返回错误")
	}
}

// TestChromaLogical_And 测试 Chroma and 逻辑
func TestChromaLogical_And(t *testing.T) {
	setupChromaTest(t)
	expr := And(Eq("a", 1), Gt("b", 2))
	result, err := expr.ToExpr("chroma")
	if err != nil {
		t.Fatalf("不应返回错误: %v", err)
	}
	m := result.(map[string]any)
	where := m["where"].(map[string]any)
	andList, ok := where["$and"].([]any)
	if !ok {
		t.Fatalf("where[$and] 应为 []any，实际 %T", where["$and"])
	}
	if len(andList) != 2 {
		t.Errorf("$and 列表长度 = %d, want 2", len(andList))
	}
}

// TestChromaLogical_Or 测试 Chroma or 逻辑
func TestChromaLogical_Or(t *testing.T) {
	setupChromaTest(t)
	expr := Or(Eq("a", 1), Gt("b", 2))
	result, err := expr.ToExpr("chroma")
	if err != nil {
		t.Fatalf("不应返回错误: %v", err)
	}
	m := result.(map[string]any)
	where := m["where"].(map[string]any)
	orList, ok := where["$or"].([]any)
	if !ok {
		t.Fatalf("where[$or] 应为 []any，实际 %T", where["$or"])
	}
	if len(orList) != 2 {
		t.Errorf("$or 列表长度 = %d, want 2", len(orList))
	}
}

// TestChromaTextMatch_Exact 测试 Chroma 精确匹配
func TestChromaTextMatch_Exact(t *testing.T) {
	setupChromaTest(t)
	expr := &MatchExpr{Field: "content", Value: "hello", MatchMode: MatchModeExact}
	result, err := expr.ToExpr("chroma")
	if err != nil {
		t.Fatalf("不应返回错误: %v", err)
	}
	m := result.(map[string]any)
	whereDoc := m["where_document"].(map[string]any)
	if whereDoc["$contains"] != "hello" {
		t.Errorf("where_document[$contains] = %v, want hello", whereDoc["$contains"])
	}
}

// TestChromaTextMatch_Prefix 测试 Chroma 前缀匹配
func TestChromaTextMatch_Prefix(t *testing.T) {
	setupChromaTest(t)
	expr := &MatchExpr{Field: "content", Value: "hello", MatchMode: MatchModePrefix}
	result, err := expr.ToExpr("chroma")
	if err != nil {
		t.Fatalf("不应返回错误: %v", err)
	}
	m := result.(map[string]any)
	whereDoc := m["where_document"].(map[string]any)
	if whereDoc["$regex"] != "^hello" {
		t.Errorf("where_document[$regex] = %v, want ^hello", whereDoc["$regex"])
	}
}

// TestChromaTextMatch_Suffix 测试 Chroma 后缀匹配
func TestChromaTextMatch_Suffix(t *testing.T) {
	setupChromaTest(t)
	expr := &MatchExpr{Field: "content", Value: "hello", MatchMode: MatchModeSuffix}
	result, err := expr.ToExpr("chroma")
	if err != nil {
		t.Fatalf("不应返回错误: %v", err)
	}
	m := result.(map[string]any)
	whereDoc := m["where_document"].(map[string]any)
	if whereDoc["$regex"] != "hello$" {
		t.Errorf("where_document[$regex] = %v, want hello$", whereDoc["$regex"])
	}
}

// TestChromaTextMatch_Infix 测试 Chroma 包含匹配
func TestChromaTextMatch_Infix(t *testing.T) {
	setupChromaTest(t)
	expr := &MatchExpr{Field: "content", Value: "hello", MatchMode: MatchModeInfix}
	result, err := expr.ToExpr("chroma")
	if err != nil {
		t.Fatalf("不应返回错误: %v", err)
	}
	m := result.(map[string]any)
	whereDoc := m["where_document"].(map[string]any)
	if whereDoc["$contains"] != "hello" {
		t.Errorf("where_document[$contains] = %v, want hello", whereDoc["$contains"])
	}
}

// TestChromaTextMatch_未知模式 测试 Chroma 未知匹配模式
func TestChromaTextMatch_未知模式(t *testing.T) {
	setupChromaTest(t)
	expr := &MatchExpr{Field: "content", Value: "hello", MatchMode: "unknown"}
	_, err := expr.ToExpr("chroma")
	if err == nil {
		t.Error("未知匹配模式应返回错误")
	}
}

// TestChromaRange_不支持的操作符 测试 Chroma 不支持的 range 操作符
func TestChromaRange_不支持的操作符(t *testing.T) {
	setupChromaTest(t)
	expr := &RangeExpr{Field: "name", Operator: "like", Value: "%test%"}
	_, err := expr.ToExpr("chroma")
	if err == nil {
		t.Error("Chroma 不支持 like range 操作符应返回错误")
	}
}

// TestChromaLogical_缺少右操作数 测试 Chroma 逻辑操作缺少右操作数
func TestChromaLogical_缺少右操作数(t *testing.T) {
	setupChromaTest(t)
	expr := &LogicalExpr{Operator: "and", Left: Eq("a", 1), Right: nil}
	_, err := expr.ToExpr("chroma")
	if err == nil {
		t.Error("Chroma 逻辑操作缺少右操作数应返回错误")
	}
}

// TestChromaLogical_不支持的操作符 测试 Chroma 不支持的逻辑操作符
func TestChromaLogical_不支持的操作符(t *testing.T) {
	setupChromaTest(t)
	expr := &LogicalExpr{Operator: "xor", Left: Eq("a", 1), Right: Eq("b", 2)}
	_, err := expr.ToExpr("chroma")
	if err == nil {
		t.Error("Chroma 不支持 xor 逻辑操作符应返回错误")
	}
}

// TestChromaRange_In_非切片值 测试 Chroma in 操作非切片值
func TestChromaRange_In_非切片值(t *testing.T) {
	setupChromaTest(t)
	expr := &RangeExpr{Field: "age", Operator: "in", Value: 42}
	_, err := expr.ToExpr("chroma")
	if err == nil {
		t.Error("Chroma in 操作非切片值应返回错误")
	}
}

// TestChromaComparison_不支持的运算符 测试 Chroma 不支持的比较运算符
func TestChromaComparison_不支持的运算符(t *testing.T) {
	setupChromaTest(t)
	expr := &ComparisonExpr{Field: "name", Operator: "<>", Value: "test"}
	_, err := expr.ToExpr("chroma")
	if err == nil {
		t.Error("Chroma 不支持的比较运算符应返回错误")
	}
}

// TestChromaLogical_And_混合WhereAndWhereDocument 测试 and 组合 where 和 where_document
func TestChromaLogical_And_混合WhereAndWhereDocument(t *testing.T) {
	setupChromaTest(t)
	left := Eq("name", "test")
	right := &MatchExpr{Field: "content", Value: "hello", MatchMode: MatchModeExact}
	expr := And(left, right)
	result, err := expr.ToExpr("chroma")
	if err != nil {
		t.Fatalf("不应返回错误: %v", err)
	}
	m := result.(map[string]any)
	where := m["where"].(map[string]any)
	whereDoc := m["where_document"].(map[string]any)
	if len(where) == 0 {
		t.Error("where 不应为空")
	}
	if len(whereDoc) == 0 {
		t.Error("where_document 不应为空")
	}
}

// TestToMapAny 测试 toMapAny 辅助函数
func TestToMapAny(t *testing.T) {
	// nil 输入
	result := toMapAny(nil)
	if len(result) != 0 {
		t.Errorf("nil 输入应返回空 map，实际: %v", result)
	}
	// 非 map 输入
	result = toMapAny("not a map")
	if len(result) != 0 {
		t.Errorf("非 map 输入应返回空 map，实际: %v", result)
	}
	// 正常 map 输入
	input := map[string]any{"key": "value"}
	result = toMapAny(input)
	if result["key"] != "value" {
		t.Errorf("map 输入应返回原 map，实际: %v", result)
	}
}

// TestCombineFilters 测试 combineFilters 辅助函数
func TestCombineFilters(t *testing.T) {
	left := map[string]any{"a": 1}
	right := map[string]any{"b": 2}

	// 两边都有内容
	result := combineFilters(left, right, "$and")
	if _, ok := result["$and"]; !ok {
		t.Error("两边都有内容时应使用 $and 组合")
	}

	// 只有左边有内容
	result = combineFilters(left, map[string]any{}, "$and")
	if result["a"] != 1 {
		t.Error("只有左边有内容时应返回左边")
	}

	// 只有右边有内容
	result = combineFilters(map[string]any{}, right, "$and")
	if result["b"] != 2 {
		t.Error("只有右边有内容时应返回右边")
	}

	// 两边都为空
	result = combineFilters(map[string]any{}, map[string]any{}, "$and")
	if len(result) != 0 {
		t.Error("两边都为空时应返回空 map")
	}
}

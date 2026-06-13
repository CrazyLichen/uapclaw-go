package query

import (
	"testing"
)

// 注册测试用的 milvus 后端（与 init() 注册的冲突，需要先清除）
func setupMilvusTest(t *testing.T) {
	t.Helper()
	// 先移除 init() 注册的 milvus 定义，避免重复注册
	queryExprMu.Lock()
	delete(queryExprFunctions, "milvus")
	queryExprMu.Unlock()
	// 重新注册
	err := RegisterDatabaseQueryLanguage("milvus", milvusDef, false)
	if err != nil {
		t.Fatalf("注册 milvus 后端失败: %v", err)
	}
}

// TestMilvusComparison_字符串值 测试 Milvus 比较表达式（字符串值）
func TestMilvusComparison_字符串值(t *testing.T) {
	setupMilvusTest(t)
	expr := Eq("name", "test")
	result, err := expr.ToExpr("milvus")
	if err != nil {
		t.Fatalf("不应返回错误: %v", err)
	}
	str, ok := result.(string)
	if !ok {
		t.Fatalf("Milvus 后端应返回 string，实际 %T", result)
	}
	if str != `name == "test"` {
		t.Errorf("结果 = %q, want %q", str, `name == "test"`)
	}
}

// TestMilvusComparison_数值 测试 Milvus 比较表达式（数值）
func TestMilvusComparison_数值(t *testing.T) {
	setupMilvusTest(t)
	expr := Gt("age", 25)
	result, err := expr.ToExpr("milvus")
	if err != nil {
		t.Fatalf("不应返回错误: %v", err)
	}
	str := result.(string)
	if str != "age > 25" {
		t.Errorf("结果 = %q, want %q", str, "age > 25")
	}
}

// TestMilvusRange_In 测试 Milvus in 操作
func TestMilvusRange_In(t *testing.T) {
	setupMilvusTest(t)
	expr := InList("status", []any{"active", "pending"})
	result, err := expr.ToExpr("milvus")
	if err != nil {
		t.Fatalf("不应返回错误: %v", err)
	}
	str := result.(string)
	if str != `status in ["active","pending"]` {
		t.Errorf("结果 = %q, want %q", str, `status in ["active","pending"]`)
	}
}

// TestMilvusRange_In_数值列表 测试 Milvus in 操作（数值列表）
func TestMilvusRange_In_数值列表(t *testing.T) {
	setupMilvusTest(t)
	expr := InList("age", []any{1, 2, 3})
	result, err := expr.ToExpr("milvus")
	if err != nil {
		t.Fatalf("不应返回错误: %v", err)
	}
	str := result.(string)
	if str != "age in [1,2,3]" {
		t.Errorf("结果 = %q, want %q", str, "age in [1,2,3]")
	}
}

// TestMilvusRange_Like 测试 Milvus like 操作
func TestMilvusRange_Like(t *testing.T) {
	setupMilvusTest(t)
	expr := &RangeExpr{Field: "name", Operator: "like", Value: "%test%"}
	result, err := expr.ToExpr("milvus")
	if err != nil {
		t.Fatalf("不应返回错误: %v", err)
	}
	str := result.(string)
	if str != `name like "%test%"` {
		t.Errorf("结果 = %q, want %q", str, `name like "%test%"`)
	}
}

// TestMilvusRange_Like_无百分号 测试 Milvus like 操作无百分号
func TestMilvusRange_Like_无百分号(t *testing.T) {
	setupMilvusTest(t)
	expr := &RangeExpr{Field: "name", Operator: "like", Value: "test"}
	_, err := expr.ToExpr("milvus")
	if err == nil {
		t.Error("like 操作无百分号应返回错误")
	}
}

// TestMilvusRange_不支持的操作符 测试不支持的 range 操作符
func TestMilvusRange_不支持的操作符(t *testing.T) {
	setupMilvusTest(t)
	expr := &RangeExpr{Field: "name", Operator: "between", Value: "test"}
	_, err := expr.ToExpr("milvus")
	if err == nil {
		t.Error("不支持的 range 操作符应返回错误")
	}
}

// TestMilvusArithmetic 测试 Milvus 算术表达式
func TestMilvusArithmetic(t *testing.T) {
	setupMilvusTest(t)
	expr := &ArithmeticExpr{Field: "price", ArithmeticOperator: "+", ArithmeticValue: 10, ComparisonOperator: ">", ComparisonValue: 100}
	result, err := expr.ToExpr("milvus")
	if err != nil {
		t.Fatalf("不应返回错误: %v", err)
	}
	str := result.(string)
	if str != "price + 10> 100" {
		t.Errorf("结果 = %q, want %q", str, "price + 10> 100")
	}
}

// TestMilvusNull_IsNull 测试 Milvus is null
func TestMilvusNull_IsNull(t *testing.T) {
	setupMilvusTest(t)
	expr := IsNull("name")
	result, err := expr.ToExpr("milvus")
	if err != nil {
		t.Fatalf("不应返回错误: %v", err)
	}
	str := result.(string)
	if str != "name is null" {
		t.Errorf("结果 = %q, want %q", str, "name is null")
	}
}

// TestMilvusNull_IsNotNull 测试 Milvus is not null
func TestMilvusNull_IsNotNull(t *testing.T) {
	setupMilvusTest(t)
	expr := IsNotNull("name")
	result, err := expr.ToExpr("milvus")
	if err != nil {
		t.Fatalf("不应返回错误: %v", err)
	}
	str := result.(string)
	if str != "name is not null" {
		t.Errorf("结果 = %q, want %q", str, "name is not null")
	}
}

// TestMilvusJSON_字符串值 测试 Milvus JSON 过滤（字符串值）
func TestMilvusJSON_字符串值(t *testing.T) {
	setupMilvusTest(t)
	expr := JSONKey("meta", "status", "==", "active")
	result, err := expr.ToExpr("milvus")
	if err != nil {
		t.Fatalf("不应返回错误: %v", err)
	}
	str := result.(string)
	if str != `meta["status"] == "active"` {
		t.Errorf("结果 = %q, want %q", str, `meta["status"] == "active"`)
	}
}

// TestMilvusJSON_数值 测试 Milvus JSON 过滤（数值）
func TestMilvusJSON_数值(t *testing.T) {
	setupMilvusTest(t)
	expr := JSONKey("meta", "count", ">", 5)
	result, err := expr.ToExpr("milvus")
	if err != nil {
		t.Fatalf("不应返回错误: %v", err)
	}
	str := result.(string)
	if str != `meta["count"] > 5` {
		t.Errorf("结果 = %q, want %q", str, `meta["count"] > 5`)
	}
}

// TestMilvusArray_有索引 测试 Milvus 数组过滤（有索引）
func TestMilvusArray_有索引(t *testing.T) {
	setupMilvusTest(t)
	expr := ArrayIndex("tags", 0, "==", "go")
	result, err := expr.ToExpr("milvus")
	if err != nil {
		t.Fatalf("不应返回错误: %v", err)
	}
	str := result.(string)
	if str != `tags[0] == "go"` {
		t.Errorf("结果 = %q, want %q", str, `tags[0] == "go"`)
	}
}

// TestMilvusArray_无索引 测试 Milvus 数组过滤（无索引）
func TestMilvusArray_无索引(t *testing.T) {
	setupMilvusTest(t)
	expr := &ArrayExpr{Field: "tags", Index: nil, Operator: "==", Value: "go"}
	result, err := expr.ToExpr("milvus")
	if err != nil {
		t.Fatalf("不应返回错误: %v", err)
	}
	str := result.(string)
	if str != `tags == "go"` {
		t.Errorf("结果 = %q, want %q", str, `tags == "go"`)
	}
}

// TestMilvusLogical_And 测试 Milvus and 逻辑
func TestMilvusLogical_And(t *testing.T) {
	setupMilvusTest(t)
	expr := And(Eq("a", 1), Gt("b", 2))
	result, err := expr.ToExpr("milvus")
	if err != nil {
		t.Fatalf("不应返回错误: %v", err)
	}
	str := result.(string)
	if str != `(a == 1) and (b > 2)` {
		t.Errorf("结果 = %q, want %q", str, `(a == 1) and (b > 2)`)
	}
}

// TestMilvusLogical_Or 测试 Milvus or 逻辑
func TestMilvusLogical_Or(t *testing.T) {
	setupMilvusTest(t)
	expr := Or(Eq("a", 1), Gt("b", 2))
	result, err := expr.ToExpr("milvus")
	if err != nil {
		t.Fatalf("不应返回错误: %v", err)
	}
	str := result.(string)
	if str != `(a == 1) or (b > 2)` {
		t.Errorf("结果 = %q, want %q", str, `(a == 1) or (b > 2)`)
	}
}

// TestMilvusLogical_Not 测试 Milvus not 逻辑
func TestMilvusLogical_Not(t *testing.T) {
	setupMilvusTest(t)
	expr := Not(Eq("a", 1))
	result, err := expr.ToExpr("milvus")
	if err != nil {
		t.Fatalf("不应返回错误: %v", err)
	}
	str := result.(string)
	if str != `not (a == 1)` {
		t.Errorf("结果 = %q, want %q", str, `not (a == 1)`)
	}
}

// TestMilvusLogical_Not_有右操作数 测试 not 不应有右操作数
func TestMilvusLogical_Not_有右操作数(t *testing.T) {
	setupMilvusTest(t)
	expr := &LogicalExpr{Operator: "not", Left: Eq("a", 1), Right: Eq("b", 2)}
	_, err := expr.ToExpr("milvus")
	if err == nil {
		t.Error("not 操作符不应有右操作数")
	}
}

// TestMilvusLogical_And_缺少右操作数 测试 and 缺少右操作数
func TestMilvusLogical_And_缺少右操作数(t *testing.T) {
	setupMilvusTest(t)
	expr := &LogicalExpr{Operator: "and", Left: Eq("a", 1), Right: nil}
	_, err := expr.ToExpr("milvus")
	if err == nil {
		t.Error("and 操作符需要左右操作数")
	}
}

// TestMilvusLogical_不支持的操作符 测试不支持的逻辑操作符
func TestMilvusLogical_不支持的操作符(t *testing.T) {
	setupMilvusTest(t)
	expr := &LogicalExpr{Operator: "nand", Left: Eq("a", 1), Right: Eq("b", 2)}
	_, err := expr.ToExpr("milvus")
	if err == nil {
		t.Error("不支持的逻辑操作符应返回错误")
	}
}

// TestMilvusTextMatch_Exact 测试 Milvus 精确匹配
func TestMilvusTextMatch_Exact(t *testing.T) {
	setupMilvusTest(t)
	expr := &MatchExpr{Field: "content", Value: "hello", MatchMode: MatchModeExact}
	result, err := expr.ToExpr("milvus")
	if err != nil {
		t.Fatalf("不应返回错误: %v", err)
	}
	str := result.(string)
	if str != `TEXT_MATCH(content, "hello")` {
		t.Errorf("结果 = %q, want %q", str, `TEXT_MATCH(content, "hello")`)
	}
}

// TestMilvusTextMatch_Prefix 测试 Milvus 前缀匹配
func TestMilvusTextMatch_Prefix(t *testing.T) {
	setupMilvusTest(t)
	expr := &MatchExpr{Field: "content", Value: "hello", MatchMode: MatchModePrefix}
	result, err := expr.ToExpr("milvus")
	if err != nil {
		t.Fatalf("不应返回错误: %v", err)
	}
	str := result.(string)
	if str != `content like "hello%"` {
		t.Errorf("结果 = %q, want %q", str, `content like "hello%"`)
	}
}

// TestMilvusTextMatch_Suffix 测试 Milvus 后缀匹配
func TestMilvusTextMatch_Suffix(t *testing.T) {
	setupMilvusTest(t)
	expr := &MatchExpr{Field: "content", Value: "hello", MatchMode: MatchModeSuffix}
	result, err := expr.ToExpr("milvus")
	if err != nil {
		t.Fatalf("不应返回错误: %v", err)
	}
	str := result.(string)
	if str != `content like "%hello"` {
		t.Errorf("结果 = %q, want %q", str, `content like "%hello"`)
	}
}

// TestMilvusTextMatch_Infix 测试 Milvus 包含匹配
func TestMilvusTextMatch_Infix(t *testing.T) {
	setupMilvusTest(t)
	expr := &MatchExpr{Field: "content", Value: "hello", MatchMode: MatchModeInfix}
	result, err := expr.ToExpr("milvus")
	if err != nil {
		t.Fatalf("不应返回错误: %v", err)
	}
	str := result.(string)
	if str != `content like "%hello%"` {
		t.Errorf("结果 = %q, want %q", str, `content like "%hello%"`)
	}
}

// TestMilvusTextMatch_未知模式 测试未知匹配模式
func TestMilvusTextMatch_未知模式(t *testing.T) {
	setupMilvusTest(t)
	expr := &MatchExpr{Field: "content", Value: "hello", MatchMode: "unknown"}
	_, err := expr.ToExpr("milvus")
	if err == nil {
		t.Error("未知匹配模式应返回错误")
	}
}

// TestMilvusRange_In_字符串切片 测试 in 操作使用 []string 类型
func TestMilvusRange_In_字符串切片(t *testing.T) {
	setupMilvusTest(t)
	expr := &RangeExpr{Field: "status", Operator: "in", Value: []string{"active", "pending"}}
	result, err := expr.ToExpr("milvus")
	if err != nil {
		t.Fatalf("不应返回错误: %v", err)
	}
	str := result.(string)
	if str != `status in ["active","pending"]` {
		t.Errorf("结果 = %q, want %q", str, `status in ["active","pending"]`)
	}
}

// TestMilvusRange_In_整型切片 测试 in 操作使用 []int 类型
func TestMilvusRange_In_整型切片(t *testing.T) {
	setupMilvusTest(t)
	expr := &RangeExpr{Field: "age", Operator: "in", Value: []int{1, 2, 3}}
	result, err := expr.ToExpr("milvus")
	if err != nil {
		t.Fatalf("不应返回错误: %v", err)
	}
	str := result.(string)
	if str != "age in [1,2,3]" {
		t.Errorf("结果 = %q, want %q", str, "age in [1,2,3]")
	}
}

// TestMilvusRange_In_float64切片 测试 in 操作使用 []float64 类型
func TestMilvusRange_In_float64切片(t *testing.T) {
	setupMilvusTest(t)
	expr := &RangeExpr{Field: "score", Operator: "in", Value: []float64{1.5, 2.5}}
	result, err := expr.ToExpr("milvus")
	if err != nil {
		t.Fatalf("不应返回错误: %v", err)
	}
	str := result.(string)
	if str != "score in [1.5,2.5]" {
		t.Errorf("结果 = %q, want %q", str, "score in [1.5,2.5]")
	}
}

// TestMilvusRange_In_非切片值 测试 in 操作非切片值
func TestMilvusRange_In_非切片值(t *testing.T) {
	setupMilvusTest(t)
	expr := &RangeExpr{Field: "age", Operator: "in", Value: 42}
	_, err := expr.ToExpr("milvus")
	if err == nil {
		t.Error("in 操作非切片值应返回错误")
	}
}

// TestMilvusArray_有索引_数值 测试 Milvus 数组过滤（有索引，数值）
func TestMilvusArray_有索引_数值(t *testing.T) {
	setupMilvusTest(t)
	expr := &ArrayExpr{Field: "scores", Index: intPtr(0), Operator: ">", Value: 90}
	result, err := expr.ToExpr("milvus")
	if err != nil {
		t.Fatalf("不应返回错误: %v", err)
	}
	str := result.(string)
	if str != "scores[0] > 90" {
		t.Errorf("结果 = %q, want %q", str, "scores[0] > 90")
	}
}

// TestMilvusArray_无索引_数值 测试 Milvus 数组过滤（无索引，数值）
func TestMilvusArray_无索引_数值(t *testing.T) {
	setupMilvusTest(t)
	expr := &ArrayExpr{Field: "scores", Index: nil, Operator: ">", Value: 90}
	result, err := expr.ToExpr("milvus")
	if err != nil {
		t.Fatalf("不应返回错误: %v", err)
	}
	str := result.(string)
	if str != "scores > 90" {
		t.Errorf("结果 = %q, want %q", str, "scores > 90")
	}
}

// TestMilvusRange_Wildcard_不支持 测试 Milvus 不支持 wildcard 操作符
func TestMilvusRange_Wildcard_不支持(t *testing.T) {
	setupMilvusTest(t)
	expr := WildcardMatch("name", "test*")
	_, err := expr.ToExpr("milvus")
	if err == nil {
		t.Error("Milvus 不支持 wildcard 操作符应返回错误")
	}
}

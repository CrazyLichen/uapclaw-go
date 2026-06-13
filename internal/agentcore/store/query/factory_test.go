package query

import (
	"testing"
)

// TestEq 测试等值工厂函数
func TestEq(t *testing.T) {
	ce := Eq("name", "test")
	if ce.Field != "name" || ce.Operator != "==" || ce.Value != "test" {
		t.Errorf("Eq 字段不正确: %+v", ce)
	}
}

// TestNe 测试不等工厂函数
func TestNe(t *testing.T) {
	ce := Ne("status", "active")
	if ce.Operator != "!=" {
		t.Errorf("Ne 运算符应为 !=，实际 %q", ce.Operator)
	}
}

// TestGt 测试大于工厂函数
func TestGt(t *testing.T) {
	ce := Gt("age", 18)
	if ce.Operator != ">" || ce.Value != float64(18) {
		t.Errorf("Gt 字段不正确: %+v", ce)
	}
}

// TestLt 测试小于工厂函数
func TestLt(t *testing.T) {
	ce := Lt("score", 100)
	if ce.Operator != "<" {
		t.Errorf("Lt 运算符应为 <，实际 %q", ce.Operator)
	}
}

// TestGte 测试大于等于工厂函数
func TestGte(t *testing.T) {
	ce := Gte("age", 18)
	if ce.Operator != ">=" {
		t.Errorf("Gte 运算符应为 >=，实际 %q", ce.Operator)
	}
}

// TestLte 测试小于等于工厂函数
func TestLte(t *testing.T) {
	ce := Lte("score", 100)
	if ce.Operator != "<=" {
		t.Errorf("Lte 运算符应为 <=，实际 %q", ce.Operator)
	}
}

// TestInList_多值 测试 in_list 多值
func TestInList_多值(t *testing.T) {
	expr := InList("status", []any{"active", "pending"})
	re, ok := expr.(*RangeExpr)
	if !ok {
		t.Fatal("InList 多值应返回 *RangeExpr")
	}
	if re.Operator != "in" {
		t.Errorf("InList 运算符应为 in，实际 %q", re.Operator)
	}
}

// TestInList_单值 测试 in_list 单值退化为 ComparisonExpr
func TestInList_单值(t *testing.T) {
	expr := InList("status", []any{"active"})
	ce, ok := expr.(*ComparisonExpr)
	if !ok {
		t.Fatal("InList 单值应退化为 *ComparisonExpr")
	}
	if ce.Operator != "==" || ce.Value != "active" {
		t.Errorf("InList 单值字段不正确: %+v", ce)
	}
}

// TestWildcardMatch 测试通配符匹配工厂函数
func TestWildcardMatch(t *testing.T) {
	re := WildcardMatch("name", "test*")
	if re.Operator != "wildcard" || re.Value != "test*" {
		t.Errorf("WildcardMatch 字段不正确: %+v", re)
	}
}

// TestIsNull 测试 is null 工厂函数
func TestIsNull(t *testing.T) {
	ne := IsNull("name")
	if !ne.IsNull {
		t.Error("IsNull 应设置 IsNull=true")
	}
}

// TestIsNotNull 测试 is not null 工厂函数
func TestIsNotNull(t *testing.T) {
	ne := IsNotNull("name")
	if ne.IsNull {
		t.Error("IsNotNull 应设置 IsNull=false")
	}
}

// TestJSONKey 测试 JSON key 工厂函数
func TestJSONKey(t *testing.T) {
	je := JSONKey("meta", "status", "==", "active")
	if je.Field != "meta" || je.Key != "status" || je.Operator != "==" || je.Value != "active" {
		t.Errorf("JSONKey 字段不正确: %+v", je)
	}
}

// TestArrayIndex 测试数组索引工厂函数
func TestArrayIndex(t *testing.T) {
	ae := ArrayIndex("tags", 0, "==", "go")
	if ae.Index == nil || *ae.Index != 0 || ae.Value != "go" {
		t.Errorf("ArrayIndex 字段不正确: %+v", ae)
	}
}

// TestFilterUser_单用户 测试单用户过滤
func TestFilterUser_单用户(t *testing.T) {
	expr := FilterUser("user123", "")
	// 单用户 → InList 单值 → 退化为 ComparisonExpr
	ce, ok := expr.(*ComparisonExpr)
	if !ok {
		t.Fatal("FilterUser 单用户应退化为 *ComparisonExpr")
	}
	if ce.Field != "user_id" || ce.Value != "user123" {
		t.Errorf("FilterUser 字段不正确: %+v", ce)
	}
}

// TestFilterUser_多用户 测试多用户过滤
func TestFilterUser_多用户(t *testing.T) {
	expr := FilterUser([]string{"user1", "user2"}, "")
	re, ok := expr.(*RangeExpr)
	if !ok {
		t.Fatal("FilterUser 多用户应返回 *RangeExpr")
	}
	if re.Operator != "in" {
		t.Errorf("FilterUser 运算符应为 in，实际 %q", re.Operator)
	}
}

// TestFilterUser_自定义字段 测试自定义字段名
func TestFilterUser_自定义字段(t *testing.T) {
	expr := FilterUser("user123", "owner")
	ce, ok := expr.(*ComparisonExpr)
	if !ok {
		t.Fatal("FilterUser 应退化为 *ComparisonExpr")
	}
	if ce.Field != "owner" {
		t.Errorf("FilterUser 字段应为 owner，实际 %q", ce.Field)
	}
}

// TestChainFilters 测试链式过滤
func TestChainFilters(t *testing.T) {
	f1 := Eq("a", 1)
	f2 := Gt("b", 2)
	result := ChainFilters([]QueryExpr{f1, f2})
	if result == nil {
		t.Fatal("ChainFilters 不应返回 nil")
	}
	le, ok := result.(*LogicalExpr)
	if !ok {
		t.Fatal("ChainFilters 应返回 *LogicalExpr")
	}
	if le.Operator != "and" {
		t.Errorf("ChainFilters 运算符应为 and，实际 %q", le.Operator)
	}
}

// TestChainFilters_空列表 测试空列表
func TestChainFilters_空列表(t *testing.T) {
	result := ChainFilters(nil)
	if result != nil {
		t.Error("ChainFilters 空列表应返回 nil")
	}
}

// TestChainFilters_单个过滤 测试单个过滤
func TestChainFilters_单个过滤(t *testing.T) {
	f1 := Eq("a", 1)
	result := ChainFilters([]QueryExpr{f1})
	// 单个过滤器不包裹 LogicalExpr，直接返回
	if result != f1 {
		t.Error("ChainFilters 单个过滤应直接返回该表达式")
	}
}

// TestAnd 测试 And 组合函数
func TestAnd(t *testing.T) {
	left := Eq("a", 1)
	right := Gt("b", 2)
	le := And(left, right)
	if le.Operator != "and" || le.Left != left || le.Right != right {
		t.Errorf("And 字段不正确: %+v", le)
	}
}

// TestOr 测试 Or 组合函数
func TestOr(t *testing.T) {
	left := Eq("a", 1)
	right := Gt("b", 2)
	le := Or(left, right)
	if le.Operator != "or" {
		t.Errorf("Or 运算符应为 or，实际 %q", le.Operator)
	}
}

// TestNot 测试 Not 组合函数
func TestNot(t *testing.T) {
	inner := Eq("a", 1)
	le := Not(inner)
	if le.Operator != "not" || le.Left != inner || le.Right != nil {
		t.Errorf("Not 字段不正确: %+v", le)
	}
}

// TestXor 测试 Xor 组合函数
func TestXor(t *testing.T) {
	left := Eq("a", 1)
	right := Gt("b", 2)
	le := Xor(left, right)
	if le.Operator != "xor" {
		t.Errorf("Xor 运算符应为 xor，实际 %q", le.Operator)
	}
}

// TestFilterUser_非字符串非切片 测试 FilterUser 使用非 string 非 []string 类型
func TestFilterUser_非字符串非切片(t *testing.T) {
	expr := FilterUser(42, "")
	ce, ok := expr.(*ComparisonExpr)
	if !ok {
		t.Fatal("FilterUser 非字符串非切片应退化为 *ComparisonExpr")
	}
	if ce.Value != 42 {
		t.Errorf("FilterUser 字段不正确: %+v", ce)
	}
}

// TestToSlice 辅助函数测试
func TestToSlice(t *testing.T) {
	tests := []struct {
		name    string
		input   any
		wantOK  bool
		wantLen int
	}{
		{"[]any", []any{1, 2, 3}, true, 3},
		{"[]string", []string{"a", "b"}, true, 2},
		{"[]int", []int{1, 2}, true, 2},
		{"[]float64", []float64{1.5, 2.5}, true, 2},
		{"int", 42, false, 0},
		{"string", "hello", false, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, ok := toSlice(tt.input)
			if ok != tt.wantOK {
				t.Errorf("toSlice(%v) ok = %v, want %v", tt.input, ok, tt.wantOK)
			}
			if ok && len(result) != tt.wantLen {
				t.Errorf("toSlice(%v) len = %d, want %d", tt.input, len(result), tt.wantLen)
			}
		})
	}
}

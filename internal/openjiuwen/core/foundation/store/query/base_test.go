package query

import (
	"errors"
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/common/exception"
)

// TestSanitizeStr_普通字符串 测试普通字符串转义
func TestSanitizeStr_普通字符串(t *testing.T) {
	result := SanitizeStr("hello")
	if result != `"hello"` {
		t.Errorf("SanitizeStr(hello) = %q, want %q", result, `"hello"`)
	}
}

// TestSanitizeStr_含双引号 测试含双引号转义
func TestSanitizeStr_含双引号(t *testing.T) {
	result := SanitizeStr(`say "hi"`)
	if result != `"say \"hi\""` {
		t.Errorf("SanitizeStr(say \"hi\") = %q, want %q", result, `"say \"hi\""`)
	}
}

// TestSanitizeStr_非字符串值 测试非字符串值
func TestSanitizeStr_非字符串值(t *testing.T) {
	result := SanitizeStr(42)
	if result != `"42"` {
		t.Errorf("SanitizeStr(42) = %q, want %q", result, `"42"`)
	}
}

// TestRaiseQueryError 测试构造查询错误
func TestRaiseQueryError(t *testing.T) {
	err := raiseQueryError("测试原因")
	if err == nil {
		t.Error("raiseQueryError 应返回错误")
	}
	var baseErr *exception.BaseError
	if !errors.As(err, &baseErr) || baseErr.Status() != exception.StatusRetrievalVectorStoreQueryInvalid {
		t.Errorf("错误码应为 StatusRetrievalVectorStoreQueryInvalid，实际: %v", err)
	}
}

// TestCustomExpr_ToExpr 测试自定义表达式转换
func TestCustomExpr_ToExpr(t *testing.T) {
	expr := &CustomExpr{Expr: "raw expression"}
	result, err := expr.ToExpr("any_db")
	if err != nil {
		t.Fatalf("CustomExpr.ToExpr 不应返回错误: %v", err)
	}
	if result != "raw expression" {
		t.Errorf("CustomExpr.ToExpr = %v, want %q", result, "raw expression")
	}
}

// TestCustomExpr_ToExpr_非字符串 测试自定义表达式非字符串值
func TestCustomExpr_ToExpr_非字符串(t *testing.T) {
	expr := &CustomExpr{Expr: 123}
	result, err := expr.ToExpr("any_db")
	if err != nil {
		t.Fatalf("CustomExpr.ToExpr 不应返回错误: %v", err)
	}
	if result != 123 {
		t.Errorf("CustomExpr.ToExpr = %v, want 123", result)
	}
}

// TestComparisonExpr_ToExpr_未注册后端 测试未注册后端
func TestComparisonExpr_ToExpr_未注册后端(t *testing.T) {
	origFunctions := queryExprFunctions
	queryExprFunctions = make(map[string]QueryLanguageDefinition)
	defer func() { queryExprFunctions = origFunctions }()

	expr := &ComparisonExpr{Field: "age", Operator: "==", Value: 25}
	_, err := expr.ToExpr("nonexistent")
	if err == nil {
		t.Error("未注册后端应返回错误")
	}
}

// TestComparisonExpr_ToExpr_已注册后端 测试已注册后端
func TestComparisonExpr_ToExpr_已注册后端(t *testing.T) {
	origFunctions := queryExprFunctions
	queryExprFunctions = map[string]QueryLanguageDefinition{
		"test_db": {
			Comparison: func(e QueryExpr) (any, error) {
				ce := e.(*ComparisonExpr)
				return ce.Field + " " + ce.Operator + " " + SanitizeStr(ce.Value), nil
			},
		},
	}
	defer func() { queryExprFunctions = origFunctions }()

	expr := &ComparisonExpr{Field: "name", Operator: "==", Value: "test"}
	result, err := expr.ToExpr("test_db")
	if err != nil {
		t.Fatalf("ComparisonExpr.ToExpr 不应返回错误: %v", err)
	}
	if result != `name == "test"` {
		t.Errorf("ComparisonExpr.ToExpr = %v, want %q", result, `name == "test"`)
	}
}

// TestRangeExpr_ToExpr 测试范围表达式转换
func TestRangeExpr_ToExpr(t *testing.T) {
	origFunctions := queryExprFunctions
	queryExprFunctions = map[string]QueryLanguageDefinition{
		"test_db": {
			Range: func(e QueryExpr) (any, error) {
				re := e.(*RangeExpr)
				return re.Field + " " + re.Operator, nil
			},
		},
	}
	defer func() { queryExprFunctions = origFunctions }()

	expr := &RangeExpr{Field: "age", Operator: "in", Value: []any{1, 2, 3}}
	result, err := expr.ToExpr("test_db")
	if err != nil {
		t.Fatalf("RangeExpr.ToExpr 不应返回错误: %v", err)
	}
	if result != "age in" {
		t.Errorf("RangeExpr.ToExpr = %v, want %q", result, "age in")
	}
}

// TestArithmeticExpr_ToExpr 测试算术表达式转换
func TestArithmeticExpr_ToExpr(t *testing.T) {
	origFunctions := queryExprFunctions
	queryExprFunctions = map[string]QueryLanguageDefinition{
		"test_db": {
			Arithmetic: func(e QueryExpr) (any, error) {
				ae := e.(*ArithmeticExpr)
				return ae.Field + ae.ArithmeticOperator, nil
			},
		},
	}
	defer func() { queryExprFunctions = origFunctions }()

	expr := &ArithmeticExpr{Field: "price", ArithmeticOperator: "+", ArithmeticValue: 10, ComparisonOperator: ">", ComparisonValue: 100}
	result, err := expr.ToExpr("test_db")
	if err != nil {
		t.Fatalf("ArithmeticExpr.ToExpr 不应返回错误: %v", err)
	}
	if result != "price+" {
		t.Errorf("ArithmeticExpr.ToExpr = %v, want %q", result, "price+")
	}
}

// TestNullExpr_ToExpr 测试空值表达式转换
func TestNullExpr_ToExpr(t *testing.T) {
	origFunctions := queryExprFunctions
	queryExprFunctions = map[string]QueryLanguageDefinition{
		"test_db": {
			Null: func(e QueryExpr) (any, error) {
				ne := e.(*NullExpr)
				if ne.IsNull {
					return ne.Field + " is null", nil
				}
				return ne.Field + " is not null", nil
			},
		},
	}
	defer func() { queryExprFunctions = origFunctions }()

	expr := &NullExpr{Field: "name", IsNull: true}
	result, err := expr.ToExpr("test_db")
	if err != nil {
		t.Fatalf("NullExpr.ToExpr 不应返回错误: %v", err)
	}
	if result != "name is null" {
		t.Errorf("NullExpr.ToExpr = %v, want %q", result, "name is null")
	}
}

// TestJSONExpr_ToExpr 测试 JSON 表达式转换
func TestJSONExpr_ToExpr(t *testing.T) {
	origFunctions := queryExprFunctions
	queryExprFunctions = map[string]QueryLanguageDefinition{
		"test_db": {
			JSONFilter: func(e QueryExpr) (any, error) {
				je := e.(*JSONExpr)
				return je.Field + "[" + je.Key + "]", nil
			},
		},
	}
	defer func() { queryExprFunctions = origFunctions }()

	expr := &JSONExpr{Field: "meta", Key: "status", Operator: "==", Value: "active"}
	result, err := expr.ToExpr("test_db")
	if err != nil {
		t.Fatalf("JSONExpr.ToExpr 不应返回错误: %v", err)
	}
	if result != "meta[status]" {
		t.Errorf("JSONExpr.ToExpr = %v, want %q", result, "meta[status]")
	}
}

// TestArrayExpr_ToExpr 测试数组表达式转换
func TestArrayExpr_ToExpr(t *testing.T) {
	origFunctions := queryExprFunctions
	queryExprFunctions = map[string]QueryLanguageDefinition{
		"test_db": {
			Array: func(e QueryExpr) (any, error) {
				ae := e.(*ArrayExpr)
				return ae.Field + "[]", nil
			},
		},
	}
	defer func() { queryExprFunctions = origFunctions }()

	expr := &ArrayExpr{Field: "tags", Index: intPtr(0), Operator: "==", Value: "go"}
	result, err := expr.ToExpr("test_db")
	if err != nil {
		t.Fatalf("ArrayExpr.ToExpr 不应返回错误: %v", err)
	}
	if result != "tags[]" {
		t.Errorf("ArrayExpr.ToExpr = %v, want %q", result, "tags[]")
	}
}

// TestLogicalExpr_ToExpr 测试逻辑表达式转换
func TestLogicalExpr_ToExpr(t *testing.T) {
	origFunctions := queryExprFunctions
	queryExprFunctions = map[string]QueryLanguageDefinition{
		"test_db": {
			Logical: func(e QueryExpr) (any, error) {
				le := e.(*LogicalExpr)
				return le.Operator, nil
			},
		},
	}
	defer func() { queryExprFunctions = origFunctions }()

	expr := &LogicalExpr{Operator: "and", Left: &CustomExpr{Expr: "a"}, Right: &CustomExpr{Expr: "b"}}
	result, err := expr.ToExpr("test_db")
	if err != nil {
		t.Fatalf("LogicalExpr.ToExpr 不应返回错误: %v", err)
	}
	if result != "and" {
		t.Errorf("LogicalExpr.ToExpr = %v, want %q", result, "and")
	}
}

// TestMatchExpr_ToExpr 测试匹配表达式转换
func TestMatchExpr_ToExpr(t *testing.T) {
	origFunctions := queryExprFunctions
	queryExprFunctions = map[string]QueryLanguageDefinition{
		"test_db": {
			TextMatch: func(e QueryExpr) (any, error) {
				me := e.(*MatchExpr)
				return me.Field + ":" + me.MatchMode, nil
			},
		},
	}
	defer func() { queryExprFunctions = origFunctions }()

	expr := &MatchExpr{Field: "content", Value: "hello", MatchMode: "prefix"}
	result, err := expr.ToExpr("test_db")
	if err != nil {
		t.Fatalf("MatchExpr.ToExpr 不应返回错误: %v", err)
	}
	if result != "content:prefix" {
		t.Errorf("MatchExpr.ToExpr = %v, want %q", result, "content:prefix")
	}
}

// intPtr 辅助函数，返回 int 指针
func intPtr(v int) *int {
	return &v
}

// TestToExpr_回调返回错误 测试表达式转换回调返回错误
func TestToExpr_回调返回错误(t *testing.T) {
	errTest := raiseQueryError("test error")
	origFunctions := queryExprFunctions
	queryExprFunctions = map[string]QueryLanguageDefinition{
		"test_db": {
			Range:      func(e QueryExpr) (any, error) { return nil, errTest },
			Arithmetic: func(e QueryExpr) (any, error) { return nil, errTest },
			Null:       func(e QueryExpr) (any, error) { return nil, errTest },
			JSONFilter: func(e QueryExpr) (any, error) { return nil, errTest },
			Array:      func(e QueryExpr) (any, error) { return nil, errTest },
			Logical:    func(e QueryExpr) (any, error) { return nil, errTest },
			TextMatch:  func(e QueryExpr) (any, error) { return nil, errTest },
		},
	}
	defer func() { queryExprFunctions = origFunctions }()

	tests := []struct {
		name string
		expr QueryExpr
	}{
		{"RangeExpr", &RangeExpr{Field: "x", Operator: "in", Value: []any{1}}},
		{"ArithmeticExpr", &ArithmeticExpr{Field: "x", ArithmeticOperator: "+", ArithmeticValue: 1, ComparisonOperator: ">", ComparisonValue: 1}},
		{"NullExpr", &NullExpr{Field: "x", IsNull: true}},
		{"JSONExpr", &JSONExpr{Field: "x", Key: "k", Operator: "==", Value: "v"}},
		{"ArrayExpr", &ArrayExpr{Field: "x", Operator: "==", Value: "v"}},
		{"LogicalExpr", &LogicalExpr{Operator: "and", Left: &CustomExpr{Expr: "a"}, Right: &CustomExpr{Expr: "b"}}},
		{"MatchExpr", &MatchExpr{Field: "x", Value: "v", MatchMode: "exact"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := tt.expr.ToExpr("test_db")
			if err == nil {
				t.Errorf("%s.ToExpr 应返回错误", tt.name)
			}
		})
	}
}

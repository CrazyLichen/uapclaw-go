package query

import (
	"errors"
	"sync/atomic"
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/common/exception"
)

// TestRegisterDatabaseQueryLanguage_正常注册 测试正常注册
func TestRegisterDatabaseQueryLanguage_正常注册(t *testing.T) {
	// 使用独立注册表避免污染全局
	m := make(map[string]QueryLanguageDefinition)
	called := atomic.Int32{}
	def := QueryLanguageDefinition{
		Comparison: func(expr QueryExpr) (any, error) {
			called.Add(1)
			return "test", nil
		},
	}
	err := registerDatabaseQueryLanguage(m, "test_db", def, false)
	if err != nil {
		t.Fatalf("注册不应失败: %v", err)
	}
	if m["test_db"].Comparison == nil {
		t.Error("注册后应能找到定义")
	}
}

// TestRegisterDatabaseQueryLanguage_重复注册 测试重复注册
func TestRegisterDatabaseQueryLanguage_重复注册(t *testing.T) {
	m := map[string]QueryLanguageDefinition{
		"existing": {Comparison: func(expr QueryExpr) (any, error) { return nil, nil }},
	}
	def := QueryLanguageDefinition{
		Comparison: func(expr QueryExpr) (any, error) { return nil, nil },
	}
	err := registerDatabaseQueryLanguage(m, "existing", def, false)
	if err == nil {
		t.Error("重复注册应返回错误")
	}
}

// TestRegisterDatabaseQueryLanguage_强制覆盖 测试强制覆盖
func TestRegisterDatabaseQueryLanguage_强制覆盖(t *testing.T) {
	m := map[string]QueryLanguageDefinition{
		"existing": {Comparison: func(expr QueryExpr) (any, error) { return "old", nil }},
	}
	def := QueryLanguageDefinition{
		Comparison: func(expr QueryExpr) (any, error) { return "new", nil },
	}
	err := registerDatabaseQueryLanguage(m, "existing", def, true)
	if err != nil {
		t.Errorf("强制覆盖不应失败: %v", err)
	}
}

// TestValidateLanguageRegistered_已注册 测试已注册后端校验
func TestValidateLanguageRegistered_已注册(t *testing.T) {
	origFunctions := queryExprFunctions
	queryExprFunctions = map[string]QueryLanguageDefinition{
		"test_db": {},
	}
	defer func() { queryExprFunctions = origFunctions }()

	err := validateLanguageRegistered("test_db")
	if err != nil {
		t.Errorf("已注册的后端不应返回错误: %v", err)
	}
}

// TestValidateLanguageRegistered_未注册 测试未注册后端校验
func TestValidateLanguageRegistered_未注册(t *testing.T) {
	origFunctions := queryExprFunctions
	queryExprFunctions = map[string]QueryLanguageDefinition{}
	defer func() { queryExprFunctions = origFunctions }()

	err := validateLanguageRegistered("nonexistent")
	if err == nil {
		t.Error("未注册的后端应返回错误")
	}
	// 验证错误码
	var baseErr *exception.BaseError
	if !errors.As(err, &baseErr) || baseErr.Status() != exception.StatusRetrievalVectorStoreQueryInvalid {
		t.Errorf("错误码应为 StatusRetrievalVectorStoreQueryInvalid，实际: %v", err)
	}
}

// TestRegisterDatabaseQueryLanguage_全局注册 测试全局注册（暴露的导出函数）
func TestRegisterDatabaseQueryLanguage_全局注册(t *testing.T) {
	origFunctions := queryExprFunctions
	queryExprFunctions = make(map[string]QueryLanguageDefinition)
	defer func() { queryExprFunctions = origFunctions }()

	def := QueryLanguageDefinition{
		Comparison: func(expr QueryExpr) (any, error) { return nil, nil },
	}
	err := RegisterDatabaseQueryLanguage("global_test", def, false)
	if err != nil {
		t.Fatalf("全局注册不应失败: %v", err)
	}
	if _, ok := queryExprFunctions["global_test"]; !ok {
		t.Error("全局注册后应能找到定义")
	}
}

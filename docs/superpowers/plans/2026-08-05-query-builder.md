# Query Builder 实现计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 实现 4.28 Query Builder 查询构建器，提供统一的查询表达式抽象，支持 Milvus 和 Chroma 两个后端的表达式转换，并回填 graph 包的最小 QueryExpr 定义。

**Architecture:** 新建 `store/query/` 包，定义 `QueryExpr` 接口（`ToExpr` 返回 `any`）和 9 种表达式结构体；提供 13 个便捷工厂函数 + 4 个逻辑组合函数；通过注册表 + `init()` 自动注册 Milvus/Chroma 两个后端转换函数。完成后删除 `graph/base.go` 中的最小 `QueryExpr` 定义，改为引用 `query.QueryExpr`。

**Tech Stack:** Go 1.22+, sync.RWMutex, 项目内 exception 包

---

## Task 1: 创建 doc.go 包文档

**Files:**
- Create: `internal/agentcore/store/query/doc.go`

- [ ] **Step 1: 创建 query 目录和 doc.go**

```go
// Package query 提供查询过滤表达式的构建与转换框架。
//
// 本包定义了统一的查询表达式抽象（QueryExpr 接口），支持 9 种表达式类型
// （Comparison/Range/Arithmetic/Null/JSON/Array/Logical/Match/Custom）
// 和便捷的工厂函数（Eq/Gt/Lt 等）及逻辑组合函数（And/Or/Not/Xor），
// 使得上层可以用类型安全的方式组合过滤条件。
// 通过注册表机制，表达式可按后端名称自动转换为数据库特定格式
// （如 Milvus 过滤字符串、Chroma where 字典）。
//
// 文件目录：
//
//	query/
//	├── doc.go                  # 包文档
//	├── base.go                 # QueryExpr 接口 + 9 种表达式结构体 + 辅助函数
//	├── factory.go              # 便捷工厂函数 + 逻辑组合函数
//	├── registry.go             # 注册表 + RegisterDatabaseQueryLanguage()
//	├── milvus_query_func.go    # Milvus 后端转换函数 + init() 注册
//	└── chroma_query_func.go    # Chroma 后端转换函数 + init() 注册
//
// 对应 Python 代码：
//
//	openjiuwen/core/foundation/store/query/
//
// 核心类型/接口索引：
//
//	QueryExpr                — 查询过滤表达式接口
//	QueryLanguageDefinition  — 数据库查询语言定义（8 个转换回调）
//	ComparisonExpr           — 比较表达式（==, !=, >, <, >=, <=）
//	RangeExpr                — 范围表达式（in, like）
//	ArithmeticExpr           — 算术表达式（字段 + - * / % ** 比较值）
//	NullExpr                 — 空值检查表达式（is null / is not null）
//	JSONExpr                 — JSON 字段查询表达式
//	ArrayExpr                — 数组字段查询表达式
//	LogicalExpr              — 逻辑组合表达式（and, or, not, xor）
//	MatchExpr                — 文本匹配表达式（prefix, suffix, infix, exact）
//	CustomExpr               — 自定义原始表达式
package query
```

- [ ] **Step 2: 验证编译通过**

Run: `cd /home/opensource/uap-claw-go && go build ./internal/agentcore/store/query/`
Expected: 编译错误（缺少其他文件），但 package 声明正确

- [ ] **Step 3: Commit**

```bash
git add internal/agentcore/store/query/doc.go
git commit -m "feat(query): 添加 query 包 doc.go 包文档"
```

---

## Task 2: 实现 registry.go 注册表

**Files:**
- Create: `internal/agentcore/store/query/registry.go`
- Create: `internal/agentcore/store/query/registry_test.go`

- [ ] **Step 1: 编写 registry_test.go 失败测试**

```go
package query

import (
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
	if !exception.IsErrorCode(err, exception.StatusRetrievalVectorStoreQueryInvalid) {
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
```

- [ ] **Step 2: 运行测试验证失败**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/store/query/ -v -run "TestRegister|TestValidate"`
Expected: 编译失败（registry.go 不存在）

- [ ] **Step 3: 实现 registry.go**

```go
package query

import (
	"sync"

	"github.com/uapclaw/uapclaw-go/internal/common/exception"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// QueryLanguageDefinition 数据库查询语言定义
//
// 每个数据库后端需注册一个 QueryLanguageDefinition 实例，
// 包含 8 个转换回调函数，分别处理不同类型的查询表达式。
// 对应 Python: QueryLanguageDefinition
type QueryLanguageDefinition struct {
	// Comparison 比较表达式转换
	Comparison func(QueryExpr) (any, error)
	// Range 范围表达式转换
	Range func(QueryExpr) (any, error)
	// Arithmetic 算术表达式转换
	Arithmetic func(QueryExpr) (any, error)
	// Null 空值表达式转换
	Null func(QueryExpr) (any, error)
	// JSONFilter JSON 字段表达式转换
	JSONFilter func(QueryExpr) (any, error)
	// Array 数组表达式转换
	Array func(QueryExpr) (any, error)
	// Logical 逻辑表达式转换
	Logical func(QueryExpr) (any, error)
	// TextMatch 文本匹配表达式转换
	TextMatch func(QueryExpr) (any, error)
}

// ──────────────────────────── 全局变量 ────────────────────────────

var (
	// queryExprFunctions 查询语言注册表
	queryExprFunctions = make(map[string]QueryLanguageDefinition)
	// queryExprMu 注册表读写锁
	queryExprMu sync.RWMutex
	// logComponent 日志组件
	logComponent = logger.ComponentAgentCore
)

// ──────────────────────────── 导出函数 ────────────────────────────

// RegisterDatabaseQueryLanguage 注册数据库查询语言定义
//
// name 为后端名称（如 "milvus"、"chroma"），def 为查询语言定义，
// force 为 true 时允许覆盖已注册的同名后端。
// 对应 Python: register_database_query_language()
func RegisterDatabaseQueryLanguage(name string, def QueryLanguageDefinition, force bool) error {
	queryExprMu.Lock()
	defer queryExprMu.Unlock()

	err := registerDatabaseQueryLanguage(queryExprFunctions, name, def, force)
	if err != nil {
		return err
	}

	logger.Info(logComponent).
		Str("database", name).
		Msg("注册查询表达式支持")
	return nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// registerDatabaseQueryLanguage 内部注册实现（便于测试）
func registerDatabaseQueryLanguage(m map[string]QueryLanguageDefinition, name string, def QueryLanguageDefinition, force bool) error {
	if _, ok := m[name]; ok && !force {
		return exception.BuildError(exception.StatusRetrievalVectorStoreQueryInvalid,
			exception.WithParam("error_msg", "Database query language for "+name+" already registered"))
	}
	m[name] = def
	return nil
}

// validateLanguageRegistered 校验后端查询语言已注册
//
// 对应 Python: validate_language_registered()
func validateLanguageRegistered(name string) error {
	queryExprMu.RLock()
	_, ok := queryExprFunctions[name]
	queryExprMu.RUnlock()

	if !ok {
		return exception.BuildError(exception.StatusRetrievalVectorStoreQueryInvalid,
			exception.WithParam("error_msg", "Database query language "+name+" not registered via RegisterDatabaseQueryLanguage method"))
	}
	return nil
}

// getQueryLanguageDefinition 获取已注册的查询语言定义
func getQueryLanguageDefinition(name string) (QueryLanguageDefinition, bool) {
	queryExprMu.RLock()
	defer queryExprMu.RUnlock()
	def, ok := queryExprFunctions[name]
	return def, ok
}
```

- [ ] **Step 4: 运行测试验证通过**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/store/query/ -v -run "TestRegister|TestValidate"`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/agentcore/store/query/registry.go internal/agentcore/store/query/registry_test.go
git commit -m "feat(query): 实现注册表 registry.go + 单元测试"
```

---

## Task 3: 实现 base.go 核心表达式类型

**Files:**
- Create: `internal/agentcore/store/query/base.go`
- Create: `internal/agentcore/store/query/base_test.go`

- [ ] **Step 1: 编写 base_test.go 失败测试（QueryExpr 接口 + 辅助函数）**

```go
package query

import (
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
	if !exception.IsErrorCode(err, exception.StatusRetrievalVectorStoreQueryInvalid) {
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
```

- [ ] **Step 2: 运行测试验证失败**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/store/query/ -v -run "TestSanitize|TestRaise|TestCustomExpr|TestComparisonExpr|TestRangeExpr|TestArithmeticExpr|TestNullExpr|TestJSONExpr|TestArrayExpr|TestLogicalExpr|TestMatchExpr"`
Expected: 编译失败（base.go 不存在）

- [ ] **Step 3: 实现 base.go**

```go
package query

import (
	"fmt"

	"github.com/uapclaw/uapclaw-go/internal/common/exception"
)

// ──────────────────────────── 接口 ────────────────────────────

// QueryExpr 查询过滤表达式接口
//
// 所有查询表达式（Comparison/Range/Arithmetic/Null/JSON/Array/Logical/Match/Custom）
// 均实现此接口。通过 ToExpr 方法将表达式转换为数据库特定格式。
// Milvus 后端返回 string，Chroma 后端返回 map[string]any。
// 对应 Python: QueryExpr
type QueryExpr interface {
	// ToExpr 将过滤表达式转换为后端特定格式
	ToExpr(backend string) (any, error)
}

// ──────────────────────────── 结构体 ────────────────────────────

// ComparisonExpr 比较表达式（==, !=, >, <, >=, <=）
//
// 对应 Python: ComparisonExpr
type ComparisonExpr struct {
	// Field 字段名
	Field string
	// Operator 比较运算符
	Operator string
	// Value 比较值
	Value any
}

// RangeExpr 范围表达式（in, like）
//
// 对应 Python: RangeExpr
type RangeExpr struct {
	// Field 字段名
	Field string
	// Operator 范围运算符（in 或 like）
	Operator string
	// Value 范围值（in 操作为列表，like 操作为模式字符串）
	Value any
}

// ArithmeticExpr 算术表达式（字段 + - * / % ** 比较值）
//
// 对应 Python: ArithmeticExpr
type ArithmeticExpr struct {
	// Field 字段名
	Field string
	// ArithmeticOperator 算术运算符（+, -, *, /, %, **）
	ArithmeticOperator string
	// ArithmeticValue 算术操作值
	ArithmeticValue float64
	// ComparisonOperator 算术后的比较运算符
	ComparisonOperator string
	// ComparisonValue 比较值
	ComparisonValue float64
}

// NullExpr 空值检查表达式（is null / is not null）
//
// 对应 Python: NullExpr
type NullExpr struct {
	// Field 字段名
	Field string
	// IsNull true 为 is null，false 为 is not null
	IsNull bool
}

// JSONExpr JSON 字段查询表达式
//
// 对应 Python: JSONExpr
type JSONExpr struct {
	// Field JSON 字段名
	Field string
	// Key JSON 键名
	Key string
	// Operator 比较运算符
	Operator string
	// Value 比较值
	Value any
}

// ArrayExpr 数组字段查询表达式
//
// 对应 Python: ArrayExpr
type ArrayExpr struct {
	// Field 数组字段名
	Field string
	// Index 数组索引（nil 表示操作整个数组字段）
	Index *int
	// Operator 比较运算符
	Operator string
	// Value 比较值
	Value any
}

// LogicalExpr 逻辑组合表达式（and, or, not, xor）
//
// 对应 Python: LogicalExpr
type LogicalExpr struct {
	// Operator 逻辑运算符（and, or, not, xor）
	Operator string
	// Left 左操作数
	Left QueryExpr
	// Right 右操作数（not 运算符不需要右操作数）
	Right QueryExpr
}

// MatchExpr 文本匹配表达式（prefix, suffix, infix, exact）
//
// 对应 Python: MatchExpr
type MatchExpr struct {
	// Field 字段名
	Field string
	// Value 文本值
	Value string
	// MatchMode 匹配模式（prefix, suffix, infix, exact）
	MatchMode string
}

// CustomExpr 自定义原始表达式
//
// 对应 Python: CustomExpr
type CustomExpr struct {
	// Expr 自定义表达式（可为任意类型）
	Expr any
}

// ──────────────────────────── 常量 ────────────────────────────

const (
	// MatchModeExact 精确匹配
	MatchModeExact = "exact"
	// MatchModePrefix 前缀匹配
	MatchModePrefix = "prefix"
	// MatchModeSuffix 后缀匹配
	MatchModeSuffix = "suffix"
	// MatchModeInfix 包含匹配
	MatchModeInfix = "infix"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// SanitizeStr 转义字符串值中的双引号并加上引号包裹
//
// 对应 Python: QueryExpr.sanitize_str()
func SanitizeStr(value any) string {
	s := fmt.Sprintf("%v", value)
	if contains := false; !contains {
		// 检查是否包含双引号
		for _, r := range s {
			if r == '"' {
				contains = true
				break
			}
		}
		if contains {
			escaped := ""
			for _, r := range s {
				if r == '"' {
					escaped += `\"`
				} else {
					escaped += string(r)
				}
			}
			return `"` + escaped + `"`
		}
	}
	return `"` + s + `"`
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// raiseQueryError 构造查询错误
//
// 对应 Python: raise_query_error()
func raiseQueryError(reason string) error {
	return exception.BuildError(exception.StatusRetrievalVectorStoreQueryInvalid,
		exception.WithParam("error_msg", reason))
}

// ToExpr 实现 QueryExpr 接口 — CustomExpr
func (e *CustomExpr) ToExpr(_ string) (any, error) {
	return e.Expr, nil
}

// ToExpr 实现 QueryExpr 接口 — ComparisonExpr
func (e *ComparisonExpr) ToExpr(backend string) (any, error) {
	if err := validateLanguageRegistered(backend); err != nil {
		return nil, err
	}
	def, _ := getQueryLanguageDefinition(backend)
	return def.Comparison(e)
}

// ToExpr 实现 QueryExpr 接口 — RangeExpr
func (e *RangeExpr) ToExpr(backend string) (any, error) {
	if err := validateLanguageRegistered(backend); err != nil {
		return nil, err
	}
	def, _ := getQueryLanguageDefinition(backend)
	return def.Range(e)
}

// ToExpr 实现 QueryExpr 接口 — ArithmeticExpr
func (e *ArithmeticExpr) ToExpr(backend string) (any, error) {
	if err := validateLanguageRegistered(backend); err != nil {
		return nil, err
	}
	def, _ := getQueryLanguageDefinition(backend)
	return def.Arithmetic(e)
}

// ToExpr 实现 QueryExpr 接口 — NullExpr
func (e *NullExpr) ToExpr(backend string) (any, error) {
	if err := validateLanguageRegistered(backend); err != nil {
		return nil, err
	}
	def, _ := getQueryLanguageDefinition(backend)
	return def.Null(e)
}

// ToExpr 实现 QueryExpr 接口 — JSONExpr
func (e *JSONExpr) ToExpr(backend string) (any, error) {
	if err := validateLanguageRegistered(backend); err != nil {
		return nil, err
	}
	def, _ := getQueryLanguageDefinition(backend)
	return def.JSONFilter(e)
}

// ToExpr 实现 QueryExpr 接口 — ArrayExpr
func (e *ArrayExpr) ToExpr(backend string) (any, error) {
	if err := validateLanguageRegistered(backend); err != nil {
		return nil, err
	}
	def, _ := getQueryLanguageDefinition(backend)
	return def.Array(e)
}

// ToExpr 实现 QueryExpr 接口 — LogicalExpr
func (e *LogicalExpr) ToExpr(backend string) (any, error) {
	if err := validateLanguageRegistered(backend); err != nil {
		return nil, err
	}
	def, _ := getQueryLanguageDefinition(backend)
	return def.Logical(e)
}

// ToExpr 实现 QueryExpr 接口 — MatchExpr
func (e *MatchExpr) ToExpr(backend string) (any, error) {
	if err := validateLanguageRegistered(backend); err != nil {
		return nil, err
	}
	def, _ := getQueryLanguageDefinition(backend)
	return def.TextMatch(e)
}
```

- [ ] **Step 4: 运行测试验证通过**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/store/query/ -v -run "TestSanitize|TestRaise|TestCustomExpr|TestComparisonExpr|TestRangeExpr|TestArithmeticExpr|TestNullExpr|TestJSONExpr|TestArrayExpr|TestLogicalExpr|TestMatchExpr"`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/agentcore/store/query/base.go internal/agentcore/store/query/base_test.go
git commit -m "feat(query): 实现核心表达式类型 base.go + 单元测试"
```

---

## Task 4: 实现 factory.go 便捷工厂函数

**Files:**
- Create: `internal/agentcore/store/query/factory.go`
- Create: `internal/agentcore/store/query/factory_test.go`

- [ ] **Step 1: 编写 factory_test.go 失败测试**

```go
package query

import (
	"testing"
)

// TestEq 测试等值工厂函数
func TestEq(t *testing.T) {
	expr := Eq("name", "test")
	ce, ok := expr.(*ComparisonExpr)
	if !ok {
		t.Fatal("Eq 应返回 *ComparisonExpr")
	}
	if ce.Field != "name" || ce.Operator != "==" || ce.Value != "test" {
		t.Errorf("Eq 字段不正确: %+v", ce)
	}
}

// TestNe 测试不等工厂函数
func TestNe(t *testing.T) {
	expr := Ne("status", "active")
	ce, ok := expr.(*ComparisonExpr)
	if !ok {
		t.Fatal("Ne 应返回 *ComparisonExpr")
	}
	if ce.Operator != "!=" {
		t.Errorf("Ne 运算符应为 !=，实际 %q", ce.Operator)
	}
}

// TestGt 测试大于工厂函数
func TestGt(t *testing.T) {
	expr := Gt("age", 18)
	ce, ok := expr.(*ComparisonExpr)
	if !ok {
		t.Fatal("Gt 应返回 *ComparisonExpr")
	}
	if ce.Operator != ">" || ce.Value != 18 {
		t.Errorf("Gt 字段不正确: %+v", ce)
	}
}

// TestLt 测试小于工厂函数
func TestLt(t *testing.T) {
	expr := Lt("score", 100)
	ce, ok := expr.(*ComparisonExpr)
	if !ok {
		t.Fatal("Lt 应返回 *ComparisonExpr")
	}
	if ce.Operator != "<" {
		t.Errorf("Lt 运算符应为 <，实际 %q", ce.Operator)
	}
}

// TestGte 测试大于等于工厂函数
func TestGte(t *testing.T) {
	expr := Gte("age", 18)
	ce, ok := expr.(*ComparisonExpr)
	if !ok {
		t.Fatal("Gte 应返回 *ComparisonExpr")
	}
	if ce.Operator != ">=" {
		t.Errorf("Gte 运算符应为 >=，实际 %q", ce.Operator)
	}
}

// TestLte 测试小于等于工厂函数
func TestLte(t *testing.T) {
	expr := Lte("score", 100)
	ce, ok := expr.(*ComparisonExpr)
	if !ok {
		t.Fatal("Lte 应返回 *ComparisonExpr")
	}
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
	expr := WildcardMatch("name", "test*")
	re, ok := expr.(*RangeExpr)
	if !ok {
		t.Fatal("WildcardMatch 应返回 *RangeExpr")
	}
	if re.Operator != "wildcard" || re.Value != "test*" {
		t.Errorf("WildcardMatch 字段不正确: %+v", re)
	}
}

// TestIsNull 测试 is null 工厂函数
func TestIsNull(t *testing.T) {
	expr := IsNull("name")
	ne, ok := expr.(*NullExpr)
	if !ok {
		t.Fatal("IsNull 应返回 *NullExpr")
	}
	if !ne.IsNull {
		t.Error("IsNull 应设置 IsNull=true")
	}
}

// TestIsNotNull 测试 is not null 工厂函数
func TestIsNotNull(t *testing.T) {
	expr := IsNotNull("name")
	ne, ok := expr.(*NullExpr)
	if !ok {
		t.Fatal("IsNotNull 应返回 *NullExpr")
	}
	if ne.IsNull {
		t.Error("IsNotNull 应设置 IsNull=false")
	}
}

// TestJSONKey 测试 JSON key 工厂函数
func TestJSONKey(t *testing.T) {
	expr := JSONKey("meta", "status", "==", "active")
	je, ok := expr.(*JSONExpr)
	if !ok {
		t.Fatal("JSONKey 应返回 *JSONExpr")
	}
	if je.Field != "meta" || je.Key != "status" || je.Operator != "==" || je.Value != "active" {
		t.Errorf("JSONKey 字段不正确: %+v", je)
	}
}

// TestArrayIndex 测试数组索引工厂函数
func TestArrayIndex(t *testing.T) {
	expr := ArrayIndex("tags", 0, "==", "go")
	ae, ok := expr.(*ArrayExpr)
	if !ok {
		t.Fatal("ArrayIndex 应返回 *ArrayExpr")
	}
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
	expr := And(left, right)
	le, ok := expr.(*LogicalExpr)
	if !ok {
		t.Fatal("And 应返回 *LogicalExpr")
	}
	if le.Operator != "and" || le.Left != left || le.Right != right {
		t.Errorf("And 字段不正确: %+v", le)
	}
}

// TestOr 测试 Or 组合函数
func TestOr(t *testing.T) {
	left := Eq("a", 1)
	right := Gt("b", 2)
	expr := Or(left, right)
	le, ok := expr.(*LogicalExpr)
	if !ok {
		t.Fatal("Or 应返回 *LogicalExpr")
	}
	if le.Operator != "or" {
		t.Errorf("Or 运算符应为 or，实际 %q", le.Operator)
	}
}

// TestNot 测试 Not 组合函数
func TestNot(t *testing.T) {
	inner := Eq("a", 1)
	expr := Not(inner)
	le, ok := expr.(*LogicalExpr)
	if !ok {
		t.Fatal("Not 应返回 *LogicalExpr")
	}
	if le.Operator != "not" || le.Left != inner || le.Right != nil {
		t.Errorf("Not 字段不正确: %+v", le)
	}
}

// TestXor 测试 Xor 组合函数
func TestXor(t *testing.T) {
	left := Eq("a", 1)
	right := Gt("b", 2)
	expr := Xor(left, right)
	le, ok := expr.(*LogicalExpr)
	if !ok {
		t.Fatal("Xor 应返回 *LogicalExpr")
	}
	if le.Operator != "xor" {
		t.Errorf("Xor 运算符应为 xor，实际 %q", le.Operator)
	}
}
```

- [ ] **Step 2: 运行测试验证失败**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/store/query/ -v -run "TestEq|TestNe|TestGt|TestLt|TestGte|TestLte|TestInList|TestWildcardMatch|TestIsNull|TestIsNotNull|TestJSONKey|TestArrayIndex|TestFilterUser|TestChainFilters|TestAnd|TestOr|TestNot|TestXor"`
Expected: 编译失败（factory.go 不存在）

- [ ] **Step 3: 实现 factory.go**

```go
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

// FilterUser 创建用户 ID 过滤表达式
//
// users 可为 string 或 []string，userIDField 为字段名，默认 "user_id"。
// 对应 Python: filter_user()
func FilterUser(users any, userIDField string) QueryExpr {
	if userIDField == "" {
		userIDField = "user_id"
	}

	var userIDs []any
	switch v := users.(type) {
	case string:
		userIDs = []any{v}
	case []string:
		userIDs = make([]any, len(v))
		for i, s := range v {
			userIDs[i] = s
		}
	default:
		userIDs = []any{users}
	}

	return InList(userIDField, userIDs)
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
```

- [ ] **Step 4: 运行测试验证通过**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/store/query/ -v -run "TestEq|TestNe|TestGt|TestLt|TestGte|TestLte|TestInList|TestWildcardMatch|TestIsNull|TestIsNotNull|TestJSONKey|TestArrayIndex|TestFilterUser|TestChainFilters|TestAnd|TestOr|TestNot|TestXor"`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/agentcore/store/query/factory.go internal/agentcore/store/query/factory_test.go
git commit -m "feat(query): 实现便捷工厂函数 factory.go + 单元测试"
```

---

## Task 5: 实现 milvus_query_func.go Milvus 后端

**Files:**
- Create: `internal/agentcore/store/query/milvus_query_func.go`
- Create: `internal/agentcore/store/query/milvus_query_func_test.go`

- [ ] **Step 1: 编写 milvus_query_func_test.go 失败测试**

```go
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
	if str != `status in ["active", "pending"]` {
		t.Errorf("结果 = %q, want %q", str, `status in ["active", "pending"]`)
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
```

- [ ] **Step 2: 运行测试验证失败**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/store/query/ -v -run "TestMilvus"`
Expected: 编译失败（milvus_query_func.go 不存在）

- [ ] **Step 3: 实现 milvus_query_func.go**

```go
package query

import (
	"fmt"
	"strings"
)

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

// ──────────────────────────── 辅助函数 ────────────────────────────

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

func init() {
	_ = RegisterDatabaseQueryLanguage("milvus", milvusDef, false)
}
```

- [ ] **Step 4: 运行测试验证通过**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/store/query/ -v -run "TestMilvus"`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/agentcore/store/query/milvus_query_func.go internal/agentcore/store/query/milvus_query_func_test.go
git commit -m "feat(query): 实现 Milvus 后端转换函数 + 单元测试"
```

---

## Task 6: 实现 chroma_query_func.go Chroma 后端

**Files:**
- Create: `internal/agentcore/store/query/chroma_query_func.go`
- Create: `internal/agentcore/store/query/chroma_query_func_test.go`

- [ ] **Step 1: 编写 chroma_query_func_test.go 失败测试**

```go
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
```

- [ ] **Step 2: 运行测试验证失败**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/store/query/ -v -run "TestChroma"`
Expected: 编译失败（chroma_query_func.go 不存在）

- [ ] **Step 3: 实现 chroma_query_func.go**

```go
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
```

- [ ] **Step 4: 运行测试验证通过**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/store/query/ -v -run "TestChroma"`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/agentcore/store/query/chroma_query_func.go internal/agentcore/store/query/chroma_query_func_test.go
git commit -m "feat(query): 实现 Chroma 后端转换函数 + 单元测试"
```

---

## Task 7: 回填 graph 包 — 替换 QueryExpr 定义

**Files:**
- Modify: `internal/agentcore/store/graph/base.go:48-52` — 删除最小 QueryExpr 定义，import query 包
- Modify: `internal/agentcore/store/graph/base.go:65,75` — Options 字段类型改为 query.QueryExpr
- Modify: `internal/agentcore/store/graph/base.go:147,182` — WithExpr/WithFilterExpr 参数类型改为 query.QueryExpr
- Modify: `internal/agentcore/store/graph/doc.go:12` — 更新文件目录说明
- Modify: `internal/agentcore/store/graph/base_test.go:165,193,300-303` — fakeQueryExpr 适配 any 返回值

- [ ] **Step 1: 修改 graph/base.go — 删除最小 QueryExpr，import query 包**

在 import 中添加 `"github.com/uapclaw/uapclaw-go/internal/agentcore/store/query"`，
删除第 48-52 行的最小 QueryExpr 接口定义：
```go
// 删除以下代码：
// QueryExpr 查询过滤表达式接口（最小定义，4.28 完善后替换）
type QueryExpr interface {
	// ToExpr 将过滤表达式转换为后端特定格式
	ToExpr(backend string) (string, error)
}
```

Options 结构体中 `Expr QueryExpr` → `Expr query.QueryExpr`，`FilterExpr QueryExpr` → `FilterExpr query.QueryExpr`。

WithExpr 参数 `expr QueryExpr` → `expr query.QueryExpr`。
WithFilterExpr 参数 `expr QueryExpr` → `expr query.QueryExpr`。

- [ ] **Step 2: 修改 graph/base_test.go — fakeQueryExpr 适配 any 返回值**

```go
// 改动前
func (f *fakeQueryExpr) ToExpr(backend string) (string, error) { return "", nil }

// 改动后
func (f *fakeQueryExpr) ToExpr(backend string) (any, error) { return "", nil }
```

- [ ] **Step 3: 修改 graph/doc.go — 更新文件目录**

base.go 描述从 `BaseGraphStore 接口 / Options / Option / QueryExpr / 常量 / 工厂` 改为 `BaseGraphStore 接口 / Options / Option / 常量 / 工厂`（删除 QueryExpr，因为它现在来自 query 包）。

- [ ] **Step 4: 运行 graph 包测试验证通过**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/store/graph/ -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/agentcore/store/graph/base.go internal/agentcore/store/graph/base_test.go internal/agentcore/store/graph/doc.go
git commit -m "refactor(graph): 删除最小 QueryExpr 定义，改用 query.QueryExpr"
```

---

## Task 8: 回填 graph/milvus — 适配 ToExpr 返回 any

**Files:**
- Modify: `internal/agentcore/store/graph/milvus/milvus.go:171` — 类型断言
- Modify: `internal/agentcore/store/graph/milvus/milvus_writer.go:106` — 类型断言
- Modify: `internal/agentcore/store/graph/milvus/milvus_searcher.go:178,571` — 类型断言 + buildSearchFilterExpr
- Modify: `internal/agentcore/store/graph/milvus/milvus_writer_test.go:655-669` — mock 适配 any 返回值
- Modify: `internal/agentcore/store/graph/milvus/milvus_searcher_test.go` — 使用 query 包的工厂函数

- [ ] **Step 1: 修改 milvus.go:171**

```go
// 改动前
expr, err = o.Expr.ToExpr("milvus")

// 改动后
var exprVal any
exprVal, err = o.Expr.ToExpr("milvus")
if err != nil {
    return nil, fmt.Errorf("构建查询表达式失败: %w", err)
}
expr, ok := exprVal.(string)
if !ok {
    return nil, fmt.Errorf("Milvus 后端应返回 string 类型的表达式")
}
```

注意：需要将 `expr` 变量的声明提前，确保类型为 `string`。

- [ ] **Step 2: 修改 milvus_writer.go:106**

```go
// 改动前
expr, err = o.Expr.ToExpr("milvus")

// 改动后
var exprVal any
exprVal, err = o.Expr.ToExpr("milvus")
if err != nil {
    return fmt.Errorf("构建删除表达式失败: %w", err)
}
exprStr, ok := exprVal.(string)
if !ok {
    return fmt.Errorf("Milvus 后端应返回 string 类型的表达式")
}
expr = exprStr
```

- [ ] **Step 3: 修改 milvus_searcher.go:178**

```go
// 改动前
expr, err = o.FilterExpr.ToExpr("milvus")

// 改动后
var exprVal any
exprVal, err = o.FilterExpr.ToExpr("milvus")
if err != nil {
    return nil, fmt.Errorf("构建过滤表达式失败: %w", err)
}
exprStr, ok := exprVal.(string)
if !ok {
    return nil, fmt.Errorf("Milvus 后端应返回 string 类型的表达式")
}
expr = exprStr
```

- [ ] **Step 4: 修改 milvus_searcher.go:561-577 buildSearchFilterExpr**

```go
// 改动前
func buildSearchFilterExpr(o graph.Options) string {
    if len(o.IDs) > 0 {
        ids := make([]string, 0, len(o.IDs))
        for _, id := range o.IDs {
            ids = append(ids, fmt.Sprintf("%v", id))
        }
        return buildIDFilterExpr(ids)
    }
    if o.FilterExpr != nil {
        expr, err := o.FilterExpr.ToExpr("milvus")
        if err == nil && expr != "" {
            return expr
        }
    }
    return ""
}

// 改动后
func buildSearchFilterExpr(o graph.Options) string {
    if len(o.IDs) > 0 {
        ids := make([]string, 0, len(o.IDs))
        for _, id := range o.IDs {
            ids = append(ids, fmt.Sprintf("%v", id))
        }
        return buildIDFilterExpr(ids)
    }
    if o.FilterExpr != nil {
        exprVal, err := o.FilterExpr.ToExpr("milvus")
        if err == nil {
            if expr, ok := exprVal.(string); ok && expr != "" {
                return expr
            }
        }
    }
    return ""
}
```

- [ ] **Step 5: 修改 milvus_writer_test.go mock — 适配 any 返回值**

```go
// 改动前
type testQueryExpr struct {
    expr string
}
func (e *testQueryExpr) ToExpr(backend string) (string, error) {
    return e.expr, nil
}
type errorQueryExpr struct{}
func (e *errorQueryExpr) ToExpr(backend string) (string, error) {
    return "", fmt.Errorf("expr error")
}

// 改动后
type testQueryExpr struct {
    expr string
}
func (e *testQueryExpr) ToExpr(backend string) (any, error) {
    return e.expr, nil
}
type errorQueryExpr struct{}
func (e *errorQueryExpr) ToExpr(backend string) (any, error) {
    return nil, fmt.Errorf("expr error")
}
```

- [ ] **Step 6: 运行 graph/milvus 包测试验证通过**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/store/graph/milvus/ -v`
Expected: PASS

- [ ] **Step 7: Commit**

```bash
git add internal/agentcore/store/graph/milvus/milvus.go internal/agentcore/store/graph/milvus/milvus_writer.go internal/agentcore/store/graph/milvus/milvus_searcher.go internal/agentcore/store/graph/milvus/milvus_writer_test.go
git commit -m "refactor(graph/milvus): 适配 QueryExpr.ToExpr 返回 any"
```

---

## Task 9: 全量测试验证 + 覆盖率检查

**Files:**
- 无新增/修改

- [ ] **Step 1: 运行 query 包完整测试**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/store/query/ -v -cover`
Expected: PASS，覆盖率 ≥ 85%

- [ ] **Step 2: 运行 graph 包完整测试**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/store/graph/... -v -cover`
Expected: PASS

- [ ] **Step 3: 运行 store 包全量测试**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/store/... -v -cover`
Expected: PASS

- [ ] **Step 4: 检查覆盖率**

Run: `cd /home/opensource/uap-claw-go && go test -coverprofile=coverage.out ./internal/agentcore/store/query/ && go tool cover -func=coverage.out | tail -1`
Expected: query 包总覆盖率 ≥ 85%

- [ ] **Step 5: Commit（如有覆盖率修复）**

```bash
git add -A
git commit -m "test(query): 补充覆盖率测试用例"
```

---

## Task 10: 更新 IMPLEMENTATION_PLAN.md 状态

**Files:**
- Modify: `IMPLEMENTATION_PLAN.md` — 更新 4.28 状态

- [ ] **Step 1: 将 4.28 状态从 ☐ 改为 ✅**

- [ ] **Step 2: Commit**

```bash
git add IMPLEMENTATION_PLAN.md
git commit -m "docs: 更新 4.28 Query Builder 状态为已完成"
```

# Query Builder 设计文档

> 日期：2026-08-05
> 对应 Python：`openjiuwen/core/foundation/store/query/`
> 实现位置：`internal/agentcore/store/query/`
> 实现计划步骤：4.28

## 1. 概述

Query Builder 是查询过滤表达式的构建与转换框架，提供统一的查询表达式抽象，
使得上层（如 GraphStore）可以使用类型安全的表达式组合过滤条件，
再由注册的后端转换函数将表达式转为数据库特定的查询格式（如 Milvus 过滤字符串、Chroma where 字典）。

Python 端在 `openjiuwen/core/foundation/store/query/` 中实现了完整的查询表达式体系，
包含 9 种表达式类型、13 个便捷工厂函数、线程安全的注册机制，
以及 Milvus 和 Chroma 两个后端的转换函数。

Go 端对齐此设计，提供等价的查询表达式构建能力。

## 2. 设计决策

| 决策项 | 选择 | 理由 |
|--------|------|------|
| ToExpr 返回类型 | `any` | 与 Python 的 `Any` 对应；Milvus 返回 string，Chroma 返回 map，无法统一为 string |
| 表达式组合方式 | 便捷函数（And/Or/Not/Xor） | Go 不支持运算符重载，顶层函数是最惯用的 Go 风格 |
| 回调签名 | `func(QueryExpr) (any, error)` | 统一签名，简单直接，与 ToExpr 返回 any 保持一致 |
| 注册机制 | `init()` 自动注册 | 与 Python `__init__.py` 行为一致，import 即注册 |
| vector 包改造 | 不改，保持 `map[string]any` | Python 原版 vector store 也用 dict，两者各司其职 |
| 后端覆盖范围 | 仅 milvus + chroma | 与 Python 一致，YAGNI 原则；ES/Gauss 后续按需补充 |
| graph 包回填 | 删除 graph/base.go 的 QueryExpr，改为引用 query 包 | 单一来源，避免重复定义 |

## 3. 包结构

```
internal/agentcore/store/query/
├── doc.go                    # 包文档
├── base.go                   # QueryExpr 接口 + 9 种表达式结构体 + 辅助函数
├── factory.go                # 13 个便捷工厂函数 + And/Or/Not/Xor 组合函数
├── registry.go               # 注册表 + RegisterDatabaseQueryLanguage()
├── milvus_query_func.go      # Milvus 后端转换函数 + init() 注册
└── chroma_query_func.go      # Chroma 后端转换函数 + init() 注册
```

对应 Python 文件映射：

| Go 文件 | Python 文件 | 职责 |
|---------|-------------|------|
| `base.go` | `base.py` | QueryExpr 接口 + 表达式结构体 + 辅助函数 |
| `factory.go` | `base.py`（便捷函数部分） | 工厂函数 + 组合函数 |
| `registry.go` | `registry.py` | 注册表 |
| `milvus_query_func.go` | `milvus_query_func.py` | Milvus 后端 |
| `chroma_query_func.go` | `chroma_query_func.py` | Chroma 后端 |

## 4. 核心类型设计

### 4.1 QueryExpr 接口

```go
// QueryExpr 查询过滤表达式接口
type QueryExpr interface {
    // ToExpr 将过滤表达式转换为后端特定格式
    // Milvus 后端返回 string，Chroma 后端返回 map[string]any
    ToExpr(backend string) (any, error)
}
```

此接口将替换 `graph/base.go` 中的最小化定义：
```go
// 删除 graph/base.go 中的：
// type QueryExpr interface {
//     ToExpr(backend string) (string, error)
// }
```

### 4.2 表达式结构体

9 种表达式结构体，字段严格对齐 Python base.py：

| 结构体 | Python 类 | 字段 |
|--------|-----------|------|
| `ComparisonExpr` | `ComparisonExpr` | Field, Operator, Value |
| `RangeExpr` | `RangeExpr` | Field, Operator, Value |
| `ArithmeticExpr` | `ArithmeticExpr` | Field, ArithmeticOperator, ArithmeticValue, ComparisonOperator, ComparisonValue |
| `NullExpr` | `NullExpr` | Field, IsNull |
| `JSONExpr` | `JSONExpr` | Field, Key, Operator, Value |
| `ArrayExpr` | `ArrayExpr` | Field, Index, Operator, Value |
| `LogicalExpr` | `LogicalExpr` | Operator, Left, Right |
| `MatchExpr` | `MatchExpr` | Field, Value, MatchMode |
| `CustomExpr` | `CustomExpr` | Expr |

每种表达式的 `ToExpr` 方法逻辑：校验后端已注册 → 从注册表获取 `QueryLanguageDefinition` → 调用对应的转换回调。

### 4.3 QueryLanguageDefinition

```go
// QueryLanguageDefinition 数据库查询语言定义
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
```

### 4.4 辅助函数

```go
// SanitizeStr 转义字符串值中的双引号并加上引号包裹
func SanitizeStr(value any) string

// RaiseQueryError 构造查询错误并 panic（对齐 Python 的 raise_query_error）
func RaiseQueryError(reason string) error

// ValidateLanguageRegistered 校验后端查询语言已注册
func ValidateLanguageRegistered(name string) error
```

Python 中 `raise_query_error` 通过 `raise` 抛异常，Go 中改为返回 error，
各表达式的 `ToExpr` 方法在转换回调返回错误时直接传播。

## 5. 便捷工厂函数

### 5.1 比较类

| Go 函数 | Python 函数 | 产出 |
|---------|-------------|------|
| `Eq(field, value)` | `eq(field, value)` | `ComparisonExpr{Operator: "=="}` |
| `Ne(field, value)` | `ne(field, value)` | `ComparisonExpr{Operator: "!="}` |
| `Gt(field, value)` | `gt(field, value)` | `ComparisonExpr{Operator: ">"}` |
| `Lt(field, value)` | `lt(field, value)` | `ComparisonExpr{Operator: "<"}` |
| `Gte(field, value)` | `gte(field, value)` | `ComparisonExpr{Operator: ">="}` |
| `Lte(field, value)` | `lte(field, value)` | `ComparisonExpr{Operator: "<="}` |

### 5.2 范围/匹配类

| Go 函数 | Python 函数 | 产出 |
|---------|-------------|------|
| `InList(field, values)` | `in_list(field, values)` | `RangeExpr{Operator: "in"}` 或单值时退化为 `ComparisonExpr` |
| `WildcardMatch(field, pattern)` | `wildcard_match(field, pattern)` | `RangeExpr{Operator: "wildcard"}` |
| `IsNull(field)` | `is_null(field)` | `NullExpr{IsNull: true}` |
| `IsNotNull(field)` | `is_not_null(field)` | `NullExpr{IsNull: false}` |

### 5.3 结构化字段类

| Go 函数 | Python 函数 | 产出 |
|---------|-------------|------|
| `JSONKey(field, key, operator, value)` | `json_key(field, key, operator, value)` | `JSONExpr` |
| `ArrayIndex(field, index, operator, value)` | `array_index(field, index, operator, value)` | `ArrayExpr` |

### 5.4 业务便捷类

| Go 函数 | Python 函数 | 产出 |
|---------|-------------|------|
| `FilterUser(users, field)` | `filter_user(users, field)` | 对 `InList` 的封装，默认 field="user_id" |
| `ChainFilters(filters)` | `chain_filters(filters)` | 用 AND 链接多个表达式 |

### 5.5 逻辑组合函数

| Go 函数 | Python 等价 | 说明 |
|---------|-------------|------|
| `And(left, right)` | `expr1 & expr2` | 逻辑与 |
| `Or(left, right)` | `expr1 \| expr2` | 逻辑或 |
| `Not(expr)` | `~expr` | 逻辑非 |
| `Xor(left, right)` | `expr1 ^ expr2` | 逻辑异或 |

## 6. 注册机制

```go
// registry.go

var (
    queryExprFunctions = make(map[string]QueryLanguageDefinition)
    queryExprMu        sync.RWMutex
)

// RegisterDatabaseQueryLanguage 注册数据库查询语言定义
func RegisterDatabaseQueryLanguage(name string, def QueryLanguageDefinition, force bool) error
```

- `sync.RWMutex` 保护 `queryExprFunctions` 全局 map
- `force=true` 时允许覆盖已注册的后端
- 注册失败返回 error

## 7. 后端实现

### 7.1 Milvus 后端

8 个转换函数，输出过滤表达式字符串：

| 函数 | 输出示例 |
|------|---------|
| `milvusComparisonFilter` | `field == "value"` |
| `milvusRangeFilter` | `field in ["a","b"]` / `field like "%pattern%"` |
| `milvusArithmeticFilter` | `field + 10 > 100` |
| `milvusNullFilter` | `field is null` / `field is not null` |
| `milvusJSONFilter` | `field["key"] == "value"` |
| `milvusArrayFilter` | `field[0] == "value"` |
| `milvusLogicalFilter` | `(expr1) and (expr2)` / `not (expr1)` |
| `milvusTextMatchFilter` | `TEXT_MATCH(field, "pattern")` / `field like "prefix%"` |

`init()` 中自动注册：
```go
func init() {
    _ = RegisterDatabaseQueryLanguage("milvus", milvusDef, false)
}
```

### 7.2 Chroma 后端

8 个转换函数，输出 `map[string]any`（含 `where` 和 `where_document` 两个 key）：

| 函数 | 说明 |
|------|------|
| `chromaComparisonFilter` | 转为 Chroma where 字典，如 `{"where": {"field": {"$gt": 5}}}` |
| `chromaRangeFilter` | 转为 Chroma `$in` 过滤 |
| `chromaArithmeticFilter` | 返回 error（Chroma 不支持） |
| `chromaNullFilter` | 返回 error（Chroma 不支持） |
| `chromaJSONFilter` | 返回 error（Chroma 不支持） |
| `chromaArrayFilter` | 返回 error（Chroma 不支持） |
| `chromaLogicalFilter` | 组合为 `$and`/`$or` 结构 |
| `chromaTextMatchFilter` | 转为 `where_document` 的 `$contains`/`$regex` |

`init()` 中自动注册：
```go
func init() {
    _ = RegisterDatabaseQueryLanguage("chroma", chromaDef, false)
}
```

## 8. 回填影响

### 8.1 graph/base.go

- **删除** 最小化 `QueryExpr` 接口定义（第 48-52 行）
- **删除** `fakeQueryExpr` mock（移到 graph 包自己的测试中，引用 `query.QueryExpr`）
- graph 包 import `store/query`，使用 `query.QueryExpr`、`query.Eq` 等

### 8.2 graph/base.go Options 字段

```go
// 改动前
type Options struct {
    Expr       QueryExpr  // → 改为 query.QueryExpr
    FilterExpr QueryExpr  // → 改为 query.QueryExpr
}

// 改动后（类型不变，只是来源变了）
type Options struct {
    Expr       query.QueryExpr
    FilterExpr query.QueryExpr
}
```

### 8.3 graph/milvus

`ToExpr` 返回值从 `(string, error)` 改为 `(any, error)`，调用处需类型断言：

```go
// 改动前
expr, err := o.FilterExpr.ToExpr("milvus")

// 改动后
exprVal, err := o.FilterExpr.ToExpr("milvus")
expr, ok := exprVal.(string)
```

### 8.4 graph 包测试

测试中的 mock QueryExpr 实现需适配 `ToExpr` 返回 `any`：

```go
// 改动前
func (f fakeQueryExpr) ToExpr(backend string) (string, error) { ... }

// 改动后
func (f fakeQueryExpr) ToExpr(backend string) (any, error) { ... }
```

### 8.5 vector 包

**不改动**。vector 包的 `filters map[string]any` 保持不变，与 Python 原版一致。

## 9. 测试策略

| 测试对象 | 测试方式 | 覆盖率 |
|----------|---------|--------|
| 表达式结构体 | 构造 + ToExpr 调用 | 纳入基线 |
| 工厂函数 | 验证产出表达式类型和字段 | 纳入基线 |
| 组合函数（And/Or/Not/Xor） | 验证 LogicalExpr 结构 | 纳入基线 |
| 注册表 | 注册/查询/重复注册/force 覆盖 | 纳入基线 |
| Milvus 转换 | 8 种表达式各用例 | 纳入基线 |
| Chroma 转换 | 支持的表达式用例 + 不支持的表达式返回 error | 纳入基线 |
| SanitizeStr | 双引号转义、非字符串值 | 纳入基线 |
| graph 包回填 | 原有 graph 测试 + milvus 搜索过滤测试 | 纳入基线 |

所有测试均可通过构造表达式 + 调用 ToExpr 验证输出，无需外部依赖，不使用 build tag。

## 10. 不做的事

- ❌ 不为 ES/Gauss 后端实现转换函数（后续按需补充）
- ❌ 不改造 vector 包的 `filters map[string]any`
- ❌ 不实现运算符重载（Go 不支持，用便捷函数替代）
- ❌ 不实现泛型 ToExpr（Go 接口方法不支持类型参数）

# GaussDbStore 实现计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 实现 Go 版 GaussDbStore，完整对标 Python 的 `gauss_db_store.py` + `gauss_dialect.py`，通过自定义 GORM Dialector 提供 GaussDB 特有的方言适配。

**Architecture:** 在 `internal/agentcore/store/db/gaussdb/` 独立包中实现 GaussDbStore。核心是 `GaussDialector`——嵌入 `postgres.Dialector` 并覆写 `Name()`、`Initialize()`、`DataTypeOf()`、`Migrator()` 四个方法。配套实现 `gaussLockingClauseBuilder`（对标 GaussCompiler）、`gaussStringSerializer`（对标 GaussString）和 `GaussMigrator`（预留扩展点）。GaussDbStore 实现 `db.BaseDbStore` 接口。

**Tech Stack:** Go 1.22+, gorm.io/gorm v1.31.1, gorm.io/driver/postgres v1.6.0, github.com/jackc/pgx/v5

**Design Spec:** `docs/superpowers/specs/2025-07-30-gaussdb-store-design.md`

---

## 文件结构

| 操作 | 文件路径 | 职责 |
|------|---------|------|
| 创建 | `internal/agentcore/store/db/gaussdb/doc.go` | 包文档 |
| 创建 | `internal/agentcore/store/db/gaussdb/clause.go` | gaussLockingClauseBuilder |
| 创建 | `internal/agentcore/store/db/gaussdb/clause_test.go` | clause 测试 |
| 创建 | `internal/agentcore/store/db/gaussdb/serializer.go` | gaussStringSerializer |
| 创建 | `internal/agentcore/store/db/gaussdb/serializer_test.go` | serializer 测试 |
| 创建 | `internal/agentcore/store/db/gaussdb/migrator.go` | GaussMigrator |
| 创建 | `internal/agentcore/store/db/gaussdb/migrator_test.go` | migrator 测试 |
| 创建 | `internal/agentcore/store/db/gaussdb/dialector.go` | GaussDialector + 工厂函数 |
| 创建 | `internal/agentcore/store/db/gaussdb/dialector_test.go` | dialector 测试 |
| 创建 | `internal/agentcore/store/db/gaussdb/store.go` | GaussDbStore 结构体 |
| 创建 | `internal/agentcore/store/db/gaussdb/store_test.go` | store 测试 |
| 修改 | `internal/agentcore/store/db/doc.go` | 更新文件目录 |
| 修改 | `IMPLEMENTATION_PLAN.md` | 4.14 状态 ☐ → ✅ |

---

### Task 1: 创建 gaussdb 包目录和 doc.go

**Files:**
- Create: `internal/agentcore/store/db/gaussdb/doc.go`

- [ ] **Step 1: 创建包目录**

Run: `mkdir -p internal/agentcore/store/db/gaussdb`

- [ ] **Step 2: 编写 doc.go**

```go
// Package gaussdb 提供 GaussDB 数据库的方言适配和存储实现。
//
// 本包完整对标 Python 的 openjiuwen/extensions/store/db/gauss_dialect.py，
// 通过自定义 GORM Dialector 处理 GaussDB 与 PostgreSQL 的不兼容点：
//   - 不支持 NOWAIT / SKIP LOCKED 锁选项
//   - 不支持原生 ENUM / UUID 类型
//   - 非 string 值绑定到 string 列时需要自动转换
//
// GaussDbStore 实现 db.BaseDbStore 接口，通过 GaussDialector 创建
// *gorm.DB 实例，供上层组件（SqlDbStore、SqlMessageStore 等）使用。
//
// 文件目录：
//
//	gaussdb/
//	├── doc.go          # 包文档
//	├── clause.go       # GaussDB LOCKING 子句构建器
//	├── serializer.go   # GaussDB 字符串序列化器
//	├── migrator.go     # GaussDB 迁移器
//	├── dialector.go    # GaussDB 方言定义
//	└── store.go        # GaussDbStore 存储实现
//
// 对应 Python 代码：
//
//	openjiuwen/extensions/store/db/gauss_db_store.py
//	openjiuwen/extensions/store/db/gauss_dialect.py
//
// 核心类型/接口索引：
//
//	GaussDialector  — GaussDB 数据库方言，基于 PostgreSQL 方言扩展
//	GaussMigrator   — GaussDB 迁移器，基于 PostgreSQL 迁移器扩展
//	GaussDbStore    — GaussDB 数据库存储，实现 db.BaseDbStore 接口
package gaussdb
```

- [ ] **Step 3: 验证编译通过**

Run: `cd /home/opensource/uap-claw-go && go build ./internal/agentcore/store/db/gaussdb/...`
Expected: 编译成功，无错误

- [ ] **Step 4: 提交**

```bash
git add internal/agentcore/store/db/gaussdb/doc.go
git commit -m "feat(store/gaussdb): 添加 gaussdb 包 doc.go 包文档"
```

---

### Task 2: 实现 gaussLockingClauseBuilder（TDD）

**Files:**
- Create: `internal/agentcore/store/db/gaussdb/clause.go`
- Create: `internal/agentcore/store/db/gaussdb/clause_test.go`

对标 Python: `GaussCompiler.for_update_clause()` — 忽略 NOWAIT/SKIP LOCKED，始终输出 `FOR <strength>`

- [ ] **Step 1: 编写 clause_test.go 测试**

```go
package gaussdb

import (
	"bytes"
	"testing"

	"gorm.io/gorm/clause"
)

// ──────────────────────────── gaussLockingClauseBuilder 测试 ────────────────────────────

// TestGaussLockingClauseBuilder_ForUpdate 验证 FOR UPDATE 不带选项时输出正确。
func TestGaussLockingClauseBuilder_ForUpdate(t *testing.T) {
	var buf bytes.Buffer
	builder := &testBuilder{writer: &buf}

	c := clause.Clause{
		Name: "FOR",
		Expression: clause.Locking{
			Strength: "UPDATE",
		},
	}

	gaussLockingClauseBuilder(c, builder)

	got := buf.String()
	want := "FOR UPDATE"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

// TestGaussLockingClauseBuilder_ForUpdateNowait 验证 FOR UPDATE NOWAIT 忽略 NOWAIT 选项。
func TestGaussLockingClauseBuilder_ForUpdateNowait(t *testing.T) {
	var buf bytes.Buffer
	builder := &testBuilder{writer: &buf}

	c := clause.Clause{
		Name: "FOR",
		Expression: clause.Locking{
			Strength: "UPDATE",
			Options:  "NOWAIT",
		},
	}

	gaussLockingClauseBuilder(c, builder)

	got := buf.String()
	want := "FOR UPDATE"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

// TestGaussLockingClauseBuilder_ForUpdateSkipLocked 验证 FOR UPDATE SKIP LOCKED 忽略 SKIP LOCKED 选项。
func TestGaussLockingClauseBuilder_ForUpdateSkipLocked(t *testing.T) {
	var buf bytes.Buffer
	builder := &testBuilder{writer: &buf}

	c := clause.Clause{
		Name: "FOR",
		Expression: clause.Locking{
			Strength: "UPDATE",
			Options:  "SKIP LOCKED",
		},
	}

	gaussLockingClauseBuilder(c, builder)

	got := buf.String()
	want := "FOR UPDATE"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

// TestGaussLockingClauseBuilder_ForShare 验证 FOR SHARE 输出正确。
func TestGaussLockingClauseBuilder_ForShare(t *testing.T) {
	var buf bytes.Buffer
	builder := &testBuilder{writer: &buf}

	c := clause.Clause{
		Name: "FOR",
		Expression: clause.Locking{
			Strength: "SHARE",
		},
	}

	gaussLockingClauseBuilder(c, builder)

	got := buf.String()
	want := "FOR SHARE"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

// TestGaussLockingClauseBuilder_ForUpdateOfTable 验证 FOR UPDATE OF table 忽略 OF table 子句。
func TestGaussLockingClauseBuilder_ForUpdateOfTable(t *testing.T) {
	var buf bytes.Buffer
	builder := &testBuilder{writer: &buf}

	c := clause.Clause{
		Name: "FOR",
		Expression: clause.Locking{
			Strength: "UPDATE",
			Table:    clause.Table{Name: "users"},
		},
	}

	gaussLockingClauseBuilder(c, builder)

	got := buf.String()
	want := "FOR UPDATE"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

// TestGaussLockingClauseBuilder_NonLockingExpression 验证非 Locking 表达式时回退到默认 Build。
func TestGaussLockingClauseBuilder_NonLockingExpression(t *testing.T) {
	var buf bytes.Buffer
	builder := &testBuilder{writer: &buf}

	c := clause.Clause{
		Name:       "FOR",
		Expression: clause.Expr{SQL: "SOMETHING"},
	}

	gaussLockingClauseBuilder(c, builder)

	got := buf.String()
	 // 非 Locking 表达式，回退到 Clause.Build()，输出 Name + Expression
	if got == "" {
		t.Error("期望非空输出，但得到空字符串")
	}
}

// ──────────────────────────── 测试辅助 ────────────────────────────

// testBuilder 实现 clause.Builder 接口，用于捕获 SQL 输出。
type testBuilder struct {
	writer *bytes.Buffer
}

func (b *testBuilder) WriteByte(c byte) error {
	return b.writer.WriteByte(c)
}

func (b *testBuilder) WriteString(s string) (int, error) {
	return b.writer.WriteString(s)
}

func (b *testBuilder) WriteQuoted(field interface{}) {
	b.writer.WriteString(`"`)
	b.writer.WriteString(field.(string))
	b.writer.WriteString(`"`)
}

func (b *testBuilder) AddVar(writer clause.Writer, vars ...interface{}) {
	for i, v := range vars {
		if i > 0 {
			writer.WriteByte(',')
		}
		writer.WriteByte('$')
		switch val := v.(type) {
		case int:
			writer.WriteString(rune('0' + val))
		case string:
			writer.WriteString(val)
		}
	}
}

func (b *testBuilder) AddError(err error) error {
	return err
}
```

- [ ] **Step 2: 运行测试验证失败**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/store/db/gaussdb/... -run TestGaussLockingClauseBuilder -v`
Expected: 编译失败（gaussLockingClauseBuilder 未定义）

- [ ] **Step 3: 实现 clause.go**

```go
package gaussdb

import (
	"gorm.io/gorm/clause"
)

// ──────────────────────────── 非导出函数 ────────────────────────────

// gaussLockingClauseBuilder GaussDB LOCKING 子句构建器。
//
// 对应 Python: GaussCompiler.for_update_clause()
//
// GaussDB 不支持 NOWAIT / SKIP LOCKED 锁选项和 OF table 语法，
// 此构建器忽略 Locking.Options 和 Locking.Table，仅输出 "FOR <strength>"。
// 当表达式不是 clause.Locking 类型时，回退到默认的 Clause.Build()。
func gaussLockingClauseBuilder(c clause.Clause, builder clause.Builder) {
	if locking, ok := c.Expression.(clause.Locking); ok {
		// 对标 Python: GaussCompiler.for_update_clause() 始终返回 " FOR UPDATE"
		// 忽略 locking.Table 和 locking.Options
		builder.WriteString("FOR ")
		builder.WriteString(locking.Strength)
		return
	}
	// 非 Locking 表达式，回退到默认构建
	c.Build(builder)
}
```

- [ ] **Step 4: 运行测试验证通过**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/store/db/gaussdb/... -run TestGaussLockingClauseBuilder -v`
Expected: 所有测试通过

- [ ] **Step 5: 提交**

```bash
git add internal/agentcore/store/db/gaussdb/clause.go internal/agentcore/store/db/gaussdb/clause_test.go
git commit -m "feat(store/gaussdb): 实现 gaussLockingClauseBuilder"
```

---

### Task 3: 实现 gaussStringSerializer（TDD）

**Files:**
- Create: `internal/agentcore/store/db/gaussdb/serializer.go`
- Create: `internal/agentcore/store/db/gaussdb/serializer_test.go`

对标 Python: `GaussString.bind_processor()` — 非 string 值绑定到 string 列时自动转换，datetime 特殊格式化

- [ ] **Step 1: 编写 serializer_test.go 测试**

```go
package gaussdb

import (
	"context"
	"reflect"
	"testing"
	"time"

	"gorm.io/gorm/schema"
)

// ──────────────────────────── gaussStringSerializer 测试 ────────────────────────────

// TestGaussStringSerializer_Value_String 验证 string 值直接返回。
func TestGaussStringSerializer_Value_String(t *testing.T) {
	s := gaussStringSerializer{}
	val, err := s.Value(context.Background(), nil, reflect.Value{}, "hello")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if val != "hello" {
		t.Errorf("got %v, want %q", val, "hello")
	}
}

// TestGaussStringSerializer_Value_Time 验证 time.Time 值转换为指定格式字符串。
func TestGaussStringSerializer_Value_Time(t *testing.T) {
	s := gaussStringSerializer{}
	ts := time.Date(2025, 7, 30, 15, 4, 5, 123456000, time.UTC)
	val, err := s.Value(context.Background(), nil, reflect.Value{}, ts)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := "2025-07-30 15:04:05.123456"
	if val != want {
		t.Errorf("got %q, want %q", val, want)
	}
}

// TestGaussStringSerializer_Value_Nil 验证 nil 值返回 nil。
func TestGaussStringSerializer_Value_Nil(t *testing.T) {
	s := gaussStringSerializer{}
	val, err := s.Value(context.Background(), nil, reflect.Value{}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if val != nil {
		t.Errorf("got %v, want nil", val)
	}
}

// TestGaussStringSerializer_Value_Int 验证 int 值通过 fmt.Sprintf("%v") 转换。
func TestGaussStringSerializer_Value_Int(t *testing.T) {
	s := gaussStringSerializer{}
	val, err := s.Value(context.Background(), nil, reflect.Value{}, 42)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if val != "42" {
		t.Errorf("got %v, want %q", val, "42")
	}
}

// TestGaussStringSerializer_Value_Float 验证 float64 值通过 fmt.Sprintf("%v") 转换。
func TestGaussStringSerializer_Value_Float(t *testing.T) {
	s := gaussStringSerializer{}
	val, err := s.Value(context.Background(), nil, reflect.Value{}, 3.14)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if val != "3.14" {
		t.Errorf("got %v, want %q", val, "3.14")
	}
}

// TestGaussStringSerializer_Value_Bool 验证 bool 值通过 fmt.Sprintf("%v") 转换。
func TestGaussStringSerializer_Value_Bool(t *testing.T) {
	s := gaussStringSerializer{}
	val, err := s.Value(context.Background(), nil, reflect.Value{}, true)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if val != "true" {
		t.Errorf("got %v, want %q", val, "true")
	}
}

// TestGaussStringSerializer_Scan_String 验证从数据库扫描 string 值。
func TestGaussStringSerializer_Scan_String(t *testing.T) {
	s := gaussStringSerializer{}
	field := &testField{}
	dst := reflect.ValueOf(&testDst{}).Elem()

	err := s.Scan(context.Background(), field, dst, "hello")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if field.setVal != "hello" {
		t.Errorf("got %v, want %q", field.setVal, "hello")
	}
}

// TestGaussStringSerializer_Scan_Bytes 验证从数据库扫描 []byte 值转为 string。
func TestGaussStringSerializer_Scan_Bytes(t *testing.T) {
	s := gaussStringSerializer{}
	field := &testField{}
	dst := reflect.ValueOf(&testDst{}).Elem()

	err := s.Scan(context.Background(), field, dst, []byte("world"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if field.setVal != "world" {
		t.Errorf("got %v, want %q", field.setVal, "world")
	}
}

// TestGaussStringSerializer_Scan_Nil 验证从数据库扫描 nil 值不报错。
func TestGaussStringSerializer_Scan_Nil(t *testing.T) {
	s := gaussStringSerializer{}
	field := &testField{}
	dst := reflect.ValueOf(&testDst{}).Elem()

	err := s.Scan(context.Background(), field, dst, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// TestGaussStringSerializer_Scan_Int 验证从数据库扫描非 string/[]byte 值通过 fmt.Sprintf 转换。
func TestGaussStringSerializer_Scan_Int(t *testing.T) {
	s := gaussStringSerializer{}
	field := &testField{}
	dst := reflect.ValueOf(&testDst{}).Elem()

	err := s.Scan(context.Background(), field, dst, 42)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if field.setVal != "42" {
		t.Errorf("got %v, want %q", field.setVal, "42")
	}
}

// ──────────────────────────── 测试辅助 ────────────────────────────

// testField 实现 schema.Field 的 Set 方法所需接口。
type testField struct {
	setVal interface{}
}

func (f *testField) Set(ctx context.Context, dst reflect.Value, val interface{}) error {
	f.setVal = val
	return nil
}

// testDst 用作 Scan 的目标值。
type testDst struct{}
```

- [ ] **Step 2: 运行测试验证失败**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/store/db/gaussdb/... -run TestGaussStringSerializer -v`
Expected: 编译失败（gaussStringSerializer 未定义）

- [ ] **Step 3: 实现 serializer.go**

```go
package gaussdb

import (
	"context"
	"fmt"
	"reflect"
	"time"

	"gorm.io/gorm/schema"
)

// ──────────────────────────── 结构体 ────────────────────────────

// gaussStringSerializer GaussDB 字符串序列化器。
//
// 对应 Python: GaussString.bind_processor()
//
// 确保所有绑定到 string 列的非 string 值在进入驱动前被转换为 string。
// 特别处理 time.Time → "2006-01-02 15:04:05.000000" 格式，
// 对标 Python 的 datetime.strftime('%Y-%m-%d %H:%M:%S.%f')。
type gaussStringSerializer struct{}

// ──────────────────────────── 非导出函数 ────────────────────────────

// Value 实现 schema.SerializerInterface，将 Go 值转换为数据库值。
// 对标 Python GaussString.bind_processor().process(value)：
//   - string → 直接返回
//   - time.Time → "2006-01-02 15:04:05.000000"
//   - nil → nil
//   - 其他 → fmt.Sprintf("%v", v)
func (s gaussStringSerializer) Value(_ context.Context, _ *schema.Field, _ reflect.Value, fieldValue interface{}) (interface{}, error) {
	switch v := fieldValue.(type) {
	case string:
		return v, nil
	case time.Time:
		return v.Format("2006-01-02 15:04:05.000000"), nil
	case nil:
		return nil, nil
	default:
		return fmt.Sprintf("%v", v), nil
	}
}

// Scan 实现 schema.SerializerInterface，从数据库值扫描到 Go 值。
// 对标 Python GaussString 的 result_processor 行为：
//   - string → 直接设置
//   - []byte → string(v)
//   - nil → 不设置
//   - 其他 → fmt.Sprintf("%v", v)
func (s gaussStringSerializer) Scan(_ context.Context, field *schema.Field, _ reflect.Value, dbValue interface{}) error {
	if dbValue == nil {
		return nil
	}
	switch v := dbValue.(type) {
	case string:
		return field.Set(context.Background(), reflect.Value{}, v)
	case []byte:
		return field.Set(context.Background(), reflect.Value{}, string(v))
	default:
		return field.Set(context.Background(), reflect.Value{}, fmt.Sprintf("%v", v))
	}
}
```

- [ ] **Step 4: 运行测试验证通过**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/store/db/gaussdb/... -run TestGaussStringSerializer -v`
Expected: 所有测试通过

- [ ] **Step 5: 提交**

```bash
git add internal/agentcore/store/db/gaussdb/serializer.go internal/agentcore/store/db/gaussdb/serializer_test.go
git commit -m "feat(store/gaussdb): 实现 gaussStringSerializer"
```

---

### Task 4: 实现 GaussMigrator（TDD）

**Files:**
- Create: `internal/agentcore/store/db/gaussdb/migrator.go`
- Create: `internal/agentcore/store/db/gaussdb/migrator_test.go`

对标 Python: `_domain_query` / `_enum_query` → `SELECT 1 WHERE FALSE` — 当前 GORM postgres Migrator 的 SQL 天然兼容，GaussMigrator 仅预留覆写点

- [ ] **Step 1: 编写 migrator_test.go 测试**

```go
package gaussdb

import (
	"testing"

	"gorm.io/driver/postgres"
	"gorm.io/gorm/migrator"
)

// ──────────────────────────── GaussMigrator 测试 ────────────────────────────

// TestGaussMigrator_嵌入PostgresMigrator 验证 GaussMigrator 嵌入 postgres.Migrator。
func TestGaussMigrator_嵌入PostgresMigrator(t *testing.T) {
	m := GaussMigrator{}
	// 验证 GaussMigrator 嵌入了 postgres.Migrator
	_ = m.Migrator
	// 验证 postgres.Migrator 嵌入了 migrator.Migrator
	_ = m.Migrator.Migrator
}

// TestGaussMigrator_继承MigratorConfig 验证 GaussMigrator 可正确初始化 migrator.Config。
func TestGaussMigrator_继承MigratorConfig(t *testing.T) {
	m := GaussMigrator{
		Migrator: postgres.Migrator{
			Migrator: migrator.Migrator{
				Config: migrator.Config{
					CreateIndexAfterCreateTable: true,
				},
			},
		},
	}
	if !m.CreateIndexAfterCreateTable {
		t.Error("GaussMigrator 未正确继承 CreateIndexAfterCreateTable 配置")
	}
}
```

- [ ] **Step 2: 运行测试验证失败**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/store/db/gaussdb/... -run TestGaussMigrator -v`
Expected: 编译失败（GaussMigrator 未定义）

- [ ] **Step 3: 实现 migrator.go**

```go
package gaussdb

import (
	"gorm.io/driver/postgres"
)

// ──────────────────────────── 结构体 ────────────────────────────

// GaussMigrator GaussDB 迁移器，基于 PostgreSQL 迁移器扩展。
//
// 对应 Python: GaussDialectAsyncpg._domain_query / _enum_query / _get_server_version_info
//
// 当前 GORM postgres Migrator 的 SQL 不查询 pg_type.typcollation，
// 也不做 domain/enum 内省，因此 GaussDB 天然兼容。
// 本 Migrator 预留覆写点，以便未来 GORM 版本变更时快速适配。
//
// 如果未来 GORM 版本的 postgres Migrator 引入了 GaussDB 不兼容的 SQL
// （如查询 pg_type.typcollation 或 pg_enum），应在此处覆写
// ColumnTypes() 方法进行改写。
type GaussMigrator struct {
	postgres.Migrator
}
```

- [ ] **Step 4: 运行测试验证通过**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/store/db/gaussdb/... -run TestGaussMigrator -v`
Expected: 所有测试通过

- [ ] **Step 5: 提交**

```bash
git add internal/agentcore/store/db/gaussdb/migrator.go internal/agentcore/store/db/gaussdb/migrator_test.go
git commit -m "feat(store/gaussdb): 实现 GaussMigrator 预留扩展点"
```

---

### Task 5: 实现 GaussDialector（TDD）

**Files:**
- Create: `internal/agentcore/store/db/gaussdb/dialector.go`
- Create: `internal/agentcore/store/db/gaussdb/dialector_test.go`

对标 Python: `GaussDialectAsyncpg` — 嵌入 PGDialect_asyncpg，覆写 Name/Initialize/DataTypeOf/Migrator

- [ ] **Step 1: 编写 dialector_test.go 测试**

```go
package gaussdb

import (
	"strings"
	"testing"

	"gorm.io/driver/postgres"
	"gorm.io/gorm/schema"
)

// ──────────────────────────── GaussDialector 测试 ────────────────────────────

// TestGaussDialector_Name 验证 Name 返回 "gaussdb"。
func TestGaussDialector_Name(t *testing.T) {
	d := GaussDialector{}
	if got := d.Name(); got != "gaussdb" {
		t.Errorf("Name() = %q, want %q", got, "gaussdb")
	}
}

// TestGaussDialector_Name_非Postgres 验证 Name 不返回 "postgres"。
func TestGaussDialector_Name_非Postgres(t *testing.T) {
	d := GaussDialector{}
	if d.Name() == "postgres" {
		t.Error("Name() 返回了 'postgres'，应该返回 'gaussdb'")
	}
}

// TestGaussDialector_Migrator_返回GaussMigrator 验证 Migrator 返回 GaussMigrator 类型。
func TestGaussDialector_Migrator_返回GaussMigrator(t *testing.T) {
	d := GaussDialector{}
	// 不能调用 Migrator(db) 因为需要真实 *gorm.DB，
	// 这里仅验证方法存在且签名正确
	_ = d.Migrator
}

// TestGaussOpen_返回GaussDialector 验证 GaussOpen 返回正确类型。
func TestGaussOpen_返回GaussDialector(t *testing.T) {
	d := GaussOpen("host=localhost")
	if _, ok := d.(GaussDialector); !ok {
		t.Error("GaussOpen 未返回 GaussDialector 类型")
	}
}

// TestGaussNew_返回GaussDialector 验证 GaussNew 返回正确类型。
func TestGaussNew_返回GaussDialector(t *testing.T) {
	d := GaussNew(postgres.Config{DSN: "host=localhost"})
	if _, ok := d.(GaussDialector); !ok {
		t.Error("GaussNew 未返回 GaussDialector 类型")
	}
}

// TestGaussDialector_DataTypeOf_UUID 验证 UUID 类型映射为 varchar(36)。
func TestGaussDialector_DataTypeOf_UUID(t *testing.T) {
	d := GaussDialector{}
	field := &schema.Field{
		DataType: "uuid",
	}
	got := d.DataTypeOf(field)
	want := "varchar(36)"
	if got != want {
		t.Errorf("DataTypeOf(uuid) = %q, want %q", got, want)
	}
}

// TestGaussDialector_DataTypeOf_Enum 验证 ENUM 类型映射为 varchar。
func TestGaussDialector_DataTypeOf_Enum(t *testing.T) {
	d := GaussDialector{}
	field := &schema.Field{
		DataType: "enum",
	}
	got := d.DataTypeOf(field)
	want := "varchar"
	if got != want {
		t.Errorf("DataTypeOf(enum) = %q, want %q", got, want)
	}
}

// TestGaussDialector_DataTypeOf_String 验证 String 类型委托给 postgres。
func TestGaussDialector_DataTypeOf_String(t *testing.T) {
	d := GaussDialector{}
	field := &schema.Field{
		DataType: schema.String,
	}
	got := d.DataTypeOf(field)
	// postgres 对 schema.String 返回 "text" 或 "varchar(n)"
	if !strings.HasPrefix(got, "text") && !strings.HasPrefix(got, "varchar") {
		t.Errorf("DataTypeOf(String) = %q, 期望 text 或 varchar 前缀", got)
	}
}

// TestGaussDialector_DataTypeOf_Int 验证 Int 类型委托给 postgres。
func TestGaussDialector_DataTypeOf_Int(t *testing.T) {
	d := GaussDialector{}
	field := &schema.Field{
		DataType: schema.Int,
	}
	got := d.DataTypeOf(field)
	// postgres 对 schema.Int 返回 "integer" 或 "bigint" 等
	if got == "" {
		t.Error("DataTypeOf(Int) 返回空字符串")
	}
}

// TestGaussDialector_DataTypeOf_Bool 验证 Bool 类型委托给 postgres。
func TestGaussDialector_DataTypeOf_Bool(t *testing.T) {
	d := GaussDialector{}
	field := &schema.Field{
		DataType: schema.Bool,
	}
	got := d.DataTypeOf(field)
	if got != "boolean" {
		t.Errorf("DataTypeOf(Bool) = %q, want %q", got, "boolean")
	}
}

// TestGaussDialector_DataTypeOf_UUIDMixedCase 验证 UUID 类型（大写）也能映射。
func TestGaussDialector_DataTypeOf_UUIDMixedCase(t *testing.T) {
	d := GaussDialector{}
	field := &schema.Field{
		DataType: "Uuid",
	}
	got := d.DataTypeOf(field)
	want := "varchar(36)"
	if got != want {
		t.Errorf("DataTypeOf(Uuid) = %q, want %q", got, want)
	}
}

// TestGaussDialector_DataTypeOf_EnumMixedCase 验证 ENUM 类型（大写）也能映射。
func TestGaussDialector_DataTypeOf_EnumMixedCase(t *testing.T) {
	d := GaussDialector{}
	field := &schema.Field{
		DataType: "ENUM",
	}
	got := d.DataTypeOf(field)
	want := "varchar"
	if got != want {
		t.Errorf("DataTypeOf(ENUM) = %q, want %q", got, want)
	}
}
```

- [ ] **Step 2: 运行测试验证失败**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/store/db/gaussdb/... -run TestGaussDialector -v`
Expected: 编译失败（GaussDialector 未定义）

- [ ] **Step 3: 添加 postgres driver 依赖**

Run: `cd /home/opensource/uap-claw-go && go get gorm.io/driver/postgres@v1.6.0`

- [ ] **Step 4: 实现 dialector.go**

```go
package gaussdb

import (
	"strings"

	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"gorm.io/gorm/migrator"
	"gorm.io/gorm/schema"
)

// ──────────────────────────── 常量 ────────────────────────────

const (
	// gaussLogComponent 日志组件
	gaussLogComponent = logger.ComponentAgentCore
)

// ──────────────────────────── 结构体 ────────────────────────────

// GaussDialector GaussDB 数据库方言，基于 PostgreSQL 方言扩展。
//
// 对应 Python: openjiuwen/extensions/store/db/gauss_dialect.py (GaussDialectAsyncpg)
//
// GaussDB 与 PostgreSQL 的主要差异：
//   - 不支持 NOWAIT / SKIP LOCKED 锁选项
//   - 不支持原生 ENUM / UUID 类型
//   - 非 string 值绑定到 string 列时需要自动转换
//
// 注意：postgres.Dialector 的所有方法均使用值接收者，
// 因此 GaussDialector 的覆写方法也必须使用值接收者。
type GaussDialector struct {
	postgres.Dialector // 值嵌入；所有方法使用值接收者，与 postgres.Dialector 一致
}

// ──────────────────────────── 导出函数 ────────────────────────────

// GaussOpen 使用 DSN 创建 GaussDialector。
// 对标 Python: dialect 注册入口 "gaussdb"
func GaussOpen(dsn string) gorm.Dialector {
	return GaussDialector{Dialector: postgres.Dialector{Config: &postgres.Config{DSN: dsn}}}
}

// GaussNew 使用配置创建 GaussDialector。
func GaussNew(config postgres.Config) gorm.Dialector {
	return GaussDialector{Dialector: postgres.Dialector{Config: &config}}
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// Name 返回方言名称 "gaussdb"。
// 对标 Python: GaussDialectAsyncpg.name = 'gaussdb'
// 注意：必须使用值接收者，否则嵌入的 postgres.Dialector.Name() 会被优先调用。
func (dialector GaussDialector) Name() string {
	return "gaussdb"
}

// Initialize 初始化 GaussDB 方言。
// 对标 Python: GaussDialectAsyncpg.import_dbapi() + GaussCompiler 注册
//
// 流程：
//  1. 调用 postgres.Dialector.Initialize(db) 完成基础 PG 初始化
//  2. 注册 "FOR" ClauseBuilder → 覆写 LOCKING 子句（忽略 NOWAIT/SKIP LOCKED）
//  3. 注册 gauss_string Serializer → 自动转换非 string 绑定值
//  4. 日志记录初始化完成
func (dialector GaussDialector) Initialize(db *gorm.DB) error {
	// 第 1 步：委托 postgres 初始化（连接池、回调注册等）
	if err := dialector.Dialector.Initialize(db); err != nil {
		logger.Error(gaussLogComponent).Err(err).Msg("GaussDB 方言初始化失败")
		return err
	}

	// 第 2 步：注册 LOCKING 子句构建器
	// 对标 Python: GaussCompiler.for_update_clause() 忽略 NOWAIT/SKIP LOCKED
	if db.ClauseBuilders == nil {
		db.ClauseBuilders = make(map[string]clause.ClauseBuilder)
	}
	db.ClauseBuilders["FOR"] = gaussLockingClauseBuilder

	// 第 3 步：注册 gauss_string 序列化器
	// 对标 Python: GaussString.bind_processor()
	_ = schema.RegisterSerializer("gauss_string", gaussStringSerializer{})

	// 第 4 步：日志记录
	logger.Info(gaussLogComponent).Str("dialect", "gaussdb").Msg("GaussDB 方言初始化完成")

	return nil
}

// DataTypeOf 返回 GaussDB 中的字段类型映射。
// 对标 Python: supports_native_uuid = False, supports_native_enum = False
//
// 覆写规则：
//   - UUID 类型 → varchar(36)（GaussDB 不支持原生 UUID）
//   - ENUM 类型 → varchar（GaussDB 不支持原生 ENUM）
//   - 其他 → 委托 postgres.Dialector.DataTypeOf()
func (dialector GaussDialector) DataTypeOf(field *schema.Field) string {
	dataType := strings.ToLower(string(field.DataType))
	switch {
	case strings.Contains(dataType, "uuid"):
		return "varchar(36)"
	case strings.Contains(dataType, "enum"):
		return "varchar"
	default:
		return dialector.Dialector.DataTypeOf(field)
	}
}

// Migrator 返回 GaussMigrator 实例。
// 对标 Python: _domain_query / _enum_query → SELECT 1 WHERE FALSE
func (dialector GaussDialector) Migrator(db *gorm.DB) gorm.Migrator {
	return GaussMigrator{Migrator: postgres.Migrator{
		Migrator: migrator.Migrator{
			Config: migrator.Config{
				DB:                          db,
				Dialector:                   dialector,
				CreateIndexAfterCreateTable: true,
			},
		},
	}}
}
```

- [ ] **Step 5: 运行测试验证通过**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/store/db/gaussdb/... -run TestGaussDialector -v`
Expected: 所有测试通过

- [ ] **Step 6: 提交**

```bash
git add internal/agentcore/store/db/gaussdb/dialector.go internal/agentcore/store/db/gaussdb/dialector_test.go go.mod go.sum
git commit -m "feat(store/gaussdb): 实现 GaussDialector 完整方言"
```

---

### Task 6: 实现 GaussDbStore（TDD）

**Files:**
- Create: `internal/agentcore/store/db/gaussdb/store.go`
- Create: `internal/agentcore/store/db/gaussdb/store_test.go`

对标 Python: `GaussDbStore(async_conn)` + `get_async_engine()`

- [ ] **Step 1: 编写 store_test.go 测试**

```go
package gaussdb

import (
	"context"
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/store/db"
	"gorm.io/gorm"
)

// ──────────────────────────── GaussDbStore 测试 ────────────────────────────

// TestGaussDbStore_接口满足 编译期验证 *GaussDbStore 满足 db.BaseDbStore 接口。
func TestGaussDbStore_接口满足(t *testing.T) {
	var _ db.BaseDbStore = (*GaussDbStore)(nil)
}

// TestNewGaussDbStoreWithDB 验证从已有 *gorm.DB 构造 GaussDbStore。
func TestNewGaussDbStoreWithDB(t *testing.T) {
	gormDB, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("创建测试数据库失败: %v", err)
	}

	store := NewGaussDbStoreWithDB(gormDB)
	if store == nil {
		t.Fatal("NewGaussDbStoreWithDB 返回 nil")
	}
}

// TestGaussDbStore_GetDB 验证 GetDB 返回正确的 *gorm.DB 实例。
func TestGaussDbStore_GetDB(t *testing.T) {
	gormDB, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("创建测试数据库失败: %v", err)
	}

	store := NewGaussDbStoreWithDB(gormDB)
	result := store.GetDB(context.Background())
	if result != gormDB {
		t.Error("GetDB 返回的 *gorm.DB 与构造时传入的不一致")
	}
}

// TestGaussDbStore_GetDB_NilDB 验证持有 nil *gorm.DB 时 GetDB 返回 nil。
func TestGaussDbStore_GetDB_NilDB(t *testing.T) {
	store := NewGaussDbStoreWithDB(nil)
	result := store.GetDB(context.Background())
	if result != nil {
		t.Errorf("GetDB 返回 %v, 期望 nil", result)
	}
}

// TestGaussDbStore_GetDB_忽略Context 验证 GetDB 忽略 context 参数。
func TestGaussDbStore_GetDB_忽略Context(t *testing.T) {
	gormDB, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("创建测试数据库失败: %v", err)
	}

	store := NewGaussDbStoreWithDB(gormDB)

	ctx1 := context.Background()
	ctx2 := context.TODO()

	result1 := store.GetDB(ctx1)
	result2 := store.GetDB(ctx2)

	if result1 != result2 {
		t.Error("不同 context 调用 GetDB 返回了不同的 *gorm.DB 实例")
	}
	if result1 != gormDB {
		t.Error("GetDB 返回的 *gorm.DB 与构造时传入的不一致")
	}
}

// TestGaussDbStore_Close 验证 Close 正常关闭底层连接。
func TestGaussDbStore_Close(t *testing.T) {
	gormDB, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("创建测试数据库失败: %v", err)
	}

	store := NewGaussDbStoreWithDB(gormDB)
	if err := store.Close(); err != nil {
		t.Errorf("Close 返回错误: %v", err)
	}
}

// TestGaussDbStore_Close_NilDB 验证持有 nil *gorm.DB 时 Close 返回错误。
func TestGaussDbStore_Close_NilDB(t *testing.T) {
	store := NewGaussDbStoreWithDB(nil)
	err := store.Close()
	if err == nil {
		t.Error("期望 Close 对 nil *gorm.DB 返回错误，但得到 nil")
	}
}
```

- [ ] **Step 2: 运行测试验证失败**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/store/db/gaussdb/... -run TestGaussDbStore -v`
Expected: 编译失败（GaussDbStore 未定义）

- [ ] **Step 3: 实现 store.go**

```go
package gaussdb

import (
	"context"
	"fmt"

	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	"gorm.io/gorm"
)

// ──────────────────────────── 结构体 ────────────────────────────

// GaussDbStore GaussDB 数据库存储，实现 db.BaseDbStore 接口。
//
// 对应 Python: openjiuwen/extensions/store/db/gauss_db_store.py
//
// 本实现通过 GaussDialector 创建 *gorm.DB 实例，
// 并提供 GaussDB 特有的方言适配（LOCKING 子句简化、
// 字符串序列化、UUID/ENUM 类型映射等）。
type GaussDbStore struct {
	// db GORM 数据库实例（通过 GaussDialector 创建）
	db *gorm.DB
}

// ──────────────────────────── 导出函数 ────────────────────────────

// NewGaussDbStore 从 DSN 创建 GaussDbStore。
// 内部使用 GaussDialector 连接 GaussDB，对等 Python 的 GaussDbStore(async_conn)。
// dsn 为 GaussDB 连接串（如 "host=localhost port=5432 dbname=mydb"），
// opts 为可选的 GORM 配置项。
func NewGaussDbStore(dsn string, opts ...gorm.Option) (*GaussDbStore, error) {
	dialector := GaussOpen(dsn)
	db, err := gorm.Open(dialector, opts...)
	if err != nil {
		logger.Error(gaussLogComponent).Err(err).Str("dsn", dsn).Msg("GaussDB 连接创建失败")
		return nil, fmt.Errorf("GaussDB 连接创建失败: %w", err)
	}
	logger.Info(gaussLogComponent).Str("dialect", "gaussdb").Msg("GaussDB 数据库存储创建成功")
	return &GaussDbStore{db: db}, nil
}

// NewGaussDbStoreWithDB 从已有的 *gorm.DB 创建 GaussDbStore。
// 调用方需确保该 DB 使用了 GaussDialector。
func NewGaussDbStoreWithDB(db *gorm.DB) *GaussDbStore {
	return &GaussDbStore{db: db}
}

// GetDB 实现 db.BaseDbStore 接口，返回持有的 *gorm.DB 实例。
// 对标 Python: GaussDbStore.get_async_engine() -> AsyncEngine
func (s *GaussDbStore) GetDB(_ context.Context) *gorm.DB {
	return s.db
}

// Close 关闭数据库连接池。
// Python 中由 AsyncEngine 管理生命周期，Go 中需要显式关闭。
func (s *GaussDbStore) Close() error {
	if s.db == nil {
		return fmt.Errorf("GaussDbStore 未初始化，无法关闭")
	}
	sqlDB, err := s.db.DB()
	if err != nil {
		logger.Error(gaussLogComponent).Err(err).Msg("获取底层数据库连接失败")
		return fmt.Errorf("获取底层数据库连接失败: %w", err)
	}
	if err := sqlDB.Close(); err != nil {
		logger.Error(gaussLogComponent).Err(err).Msg("关闭 GaussDB 连接失败")
		return fmt.Errorf("关闭 GaussDB 连接失败: %w", err)
	}
	logger.Info(gaussLogComponent).Msg("GaussDB 数据库连接已关闭")
	return nil
}
```

- [ ] **Step 4: 运行测试验证通过**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/store/db/gaussdb/... -run TestGaussDbStore -v`
Expected: 所有测试通过

- [ ] **Step 5: 提交**

```bash
git add internal/agentcore/store/db/gaussdb/store.go internal/agentcore/store/db/gaussdb/store_test.go
git commit -m "feat(store/gaussdb): 实现 GaussDbStore"
```

---

### Task 7: 运行完整测试套件并修复

**Files:**
- 可能修改: 上述所有测试/实现文件

- [ ] **Step 1: 运行 gaussdb 包完整测试**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/store/db/gaussdb/... -v -cover`
Expected: 所有测试通过，覆盖率达标

- [ ] **Step 2: 运行 db 包完整测试（确保无回归）**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/store/db/... -v`
Expected: 所有测试通过

- [ ] **Step 3: 运行项目整体编译检查**

Run: `cd /home/opensource/uap-claw-go && go build ./...`
Expected: 编译成功，无错误

- [ ] **Step 4: 如有测试失败或编译错误，修复后重新运行**

---

### Task 8: 更新 db 包 doc.go 文件目录

**Files:**
- Modify: `internal/agentcore/store/db/doc.go`

- [ ] **Step 1: 读取现有 doc.go**

Run: `cat internal/agentcore/store/db/doc.go`

- [ ] **Step 2: 更新 doc.go 文件目录，添加 gaussdb/ 子目录**

将文件目录从：
```
//	db/
//	├── doc.go      # 包文档
//	├── base.go     # BaseDbStore 接口定义
//	└── default.go  # DefaultDbStore 默认实现
```

更新为：
```
//	db/
//	├── doc.go          # 包文档
//	├── base.go         # BaseDbStore 接口定义
//	├── default.go      # DefaultDbStore 默认实现
//	└── gaussdb/        # GaussDB 数据库扩展
//	    ├── doc.go      # 包文档
//	    ├── clause.go   # GaussDB LOCKING 子句构建器
//	    ├── serializer.go # GaussDB 字符串序列化器
//	    ├── migrator.go # GaussDB 迁移器
//	    ├── dialector.go # GaussDB 方言定义
//	    └── store.go    # GaussDbStore 存储实现
```

同时更新核心类型索引，添加：
```
//	GaussDbStore   — BaseDbStore 的 GaussDB 实现，提供 GaussDB 特有方言适配（在 gaussdb 子包中）
```

- [ ] **Step 3: 验证编译通过**

Run: `cd /home/opensource/uap-claw-go && go build ./internal/agentcore/store/db/...`
Expected: 编译成功

- [ ] **Step 4: 提交**

```bash
git add internal/agentcore/store/db/doc.go
git commit -m "docs(store/db): 更新 doc.go 文件目录，添加 gaussdb 子包"
```

---

### Task 9: 更新 IMPLEMENTATION_PLAN.md

**Files:**
- Modify: `IMPLEMENTATION_PLAN.md`

- [ ] **Step 1: 将 4.14 状态从 ☐ 更新为 ✅**

找到行：
```
| 4.14 | ☐ | GaussDbStore | GaussDB 数据库实现 | `openjiuwen/extensions/store/gauss_db_store.py` |
```

替换为：
```
| 4.14 | ✅ | GaussDbStore | GaussDB 数据库实现 | `openjiuwen/extensions/store/gauss_db_store.py` |
```

- [ ] **Step 2: 提交**

```bash
git add IMPLEMENTATION_PLAN.md
git commit -m "chore: 更新 IMPLEMENTATION_PLAN.md 4.14 状态为已完成"
```

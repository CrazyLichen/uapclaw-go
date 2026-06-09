# vector_fields 通用框架实现计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 实现 `vector_fields` 包的通用框架——VectorField 基类、枚举、vf 标签反射机制和 ToDict 方法，为后续后端子类（Milvus/Chroma/PG）提供扩展基础。

**Architecture:** 新建独立子包 `internal/agentcore/store/vector_fields/`，与已有的 `vector` 包互不导入。VectorField 基类通过 `vf` 结构体标签实现 stage 过滤，ToDict 方法通过反射统一处理基类和子类字段，支持 Extra 字段合并和 `keepzero` 修饰符。子类通过嵌入 VectorField 并在字段上添加 `vf` 标签即可自动获得 ToDict 能力。

**Tech Stack:** Go 1.25 + reflect 标准库

---

### Task 1: 创建包目录与 doc.go

**Files:**
- Create: `internal/agentcore/store/vector_fields/doc.go`

- [ ] **Step 1: 创建目录并编写 doc.go**

```go
// Package vector_fields 提供向量索引配置的通用框架。
//
// 本包定义了 VectorField 基类及其配套的枚举类型（DatabaseType、IndexType），
// 通过 vf 结构体标签实现 stage 过滤机制，支持子类扩展。
//
// 核心设计：
//   - 子类通过嵌入 VectorField 并在字段上添加 `vf:"construct"` 或 `vf:"search"` 标签
//     来标记字段所属阶段
//   - ToDict(stage) 通过反射读取标签，只输出匹配阶段的字段
//   - 内部字段用 `vf:"-"` 标记，始终过滤
//   - 支持 `vf:"construct,keepzero"` 修饰符保留零值
//   - 支持 Extra 字段合并（字段名以 Extra 开头且类型为 map[string]any）
//
// 与 vector 包的关系：
//   - vector 包定义 FieldSchema/CollectionSchema（数据描述层）
//   - vector_fields 包定义 VectorField 层次结构（索引配置层）
//   - 两者互不导入，后续 Store 实现同时导入两者
//
// 文件目录：
//
//	vector_fields/
//	├── doc.go        # 包文档
//	└── base.go       # VectorField 基类 + DatabaseType/IndexType 枚举 + vf 标签反射机制
//
// 对应 Python 代码：openjiuwen/core/foundation/store/vector_fields/
//
// 核心类型/接口索引：
//
//	DatabaseType    — 向量数据库类型枚举（Milvus, Chroma, PG, Gauss, ES）
//	IndexType       — 索引类型枚举（AUTO, HNSW, FLAT, IVF, SCANN, IVFFlat）
//	VectorField     — 向量索引配置基类，提供 ToDict(stage) 和 Validate() 方法
package vector_fields
```

- [ ] **Step 2: 验证包可编译**

Run: `cd /home/opensource/uap-claw-go && go build ./internal/agentcore/store/vector_fields/`
Expected: 编译成功，无输出

- [ ] **Step 3: 提交**

```bash
cd /home/opensource/uap-claw-go && git add internal/agentcore/store/vector_fields/doc.go && git commit -m "feat(store): 添加 vector_fields 包 doc.go"
```

---

### Task 2: 实现 DatabaseType 和 IndexType 枚举

**Files:**
- Create: `internal/agentcore/store/vector_fields/base.go`
- Test: `internal/agentcore/store/vector_fields/base_test.go`

- [ ] **Step 1: 编写 DatabaseType 和 IndexType 枚举的失败测试**

在 `base_test.go` 中编写：

```go
package vector_fields

import (
	"fmt"
	"testing"
)

// ──── DatabaseType 枚举测试 ────

// TestDatabaseType_String 验证枚举值与 Python 字符串一致
func TestDatabaseType_String(t *testing.T) {
	tests := []struct {
		dt       DatabaseType
		expected string
	}{
		{DatabaseTypeMilvus, "milvus"},
		{DatabaseTypeChroma, "chroma"},
		{DatabaseTypePG, "pg"},
		{DatabaseTypeGauss, "gauss"},
		{DatabaseTypeES, "es"},
	}
	for _, tt := range tests {
		if got := tt.dt.String(); got != tt.expected {
			t.Errorf("DatabaseType(%d).String() = %q, 期望 %q", tt.dt, got, tt.expected)
		}
	}
}

// TestDatabaseType_无效值 验证未定义枚举值的字符串输出
func TestDatabaseType_无效值(t *testing.T) {
	dt := DatabaseType(999)
	if got := dt.String(); got != "UNKNOWN(999)" {
		t.Errorf("未知枚举值 String() = %q, 期望 %q", got, "UNKNOWN(999)")
	}
}

// TestDatabaseType_所有枚举值 验证所有枚举值在有效范围内
func TestDatabaseType_所有枚举值(t *testing.T) {
	for i := DatabaseTypeMilvus; i <= DatabaseTypeES; i++ {
		s := i.String()
		if s == "" || s == fmt.Sprintf("UNKNOWN(%d)", i) {
			t.Errorf("DatabaseType(%d) 缺少字符串表示", i)
		}
	}
}

// ──── IndexType 枚举测试 ────

// TestIndexType_String 验证枚举值与 Python 字符串一致
func TestIndexType_String(t *testing.T) {
	tests := []struct {
		it       IndexType
		expected string
	}{
		{IndexTypeAUTO, "auto"},
		{IndexTypeHNSW, "hnsw"},
		{IndexTypeFLAT, "flat"},
		{IndexTypeIVF, "ivf"},
		{IndexTypeSCANN, "scann"},
		{IndexTypeIVFFlat, "ivfflat"},
	}
	for _, tt := range tests {
		if got := tt.it.String(); got != tt.expected {
			t.Errorf("IndexType(%d).String() = %q, 期望 %q", tt.it, got, tt.expected)
		}
	}
}

// TestIndexType_无效值 验证未定义枚举值的字符串输出
func TestIndexType_无效值(t *testing.T) {
	it := IndexType(999)
	if got := it.String(); got != "UNKNOWN(999)" {
		t.Errorf("未知枚举值 String() = %q, 期望 %q", got, "UNKNOWN(999)")
	}
}

// TestIndexType_所有枚举值 验证所有枚举值在有效范围内
func TestIndexType_所有枚举值(t *testing.T) {
	for i := IndexTypeAUTO; i <= IndexTypeIVFFlat; i++ {
		s := i.String()
		if s == "" || s == fmt.Sprintf("UNKNOWN(%d)", i) {
			t.Errorf("IndexType(%d) 缺少字符串表示", i)
		}
	}
}
```

- [ ] **Step 2: 运行测试验证失败**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/store/vector_fields/ -run "TestDatabaseType_String|TestIndexType_String" -v`
Expected: 编译失败，DatabaseType 和 IndexType 未定义

- [ ] **Step 3: 实现 DatabaseType 和 IndexType 枚举**

在 `base.go` 中编写：

```go
package vector_fields

import (
	"fmt"
	"reflect"
	"strings"
)

// ──────────────────────────── 枚举 ────────────────────────────

// DatabaseType 向量数据库类型。
//
// 对应 Python: vector_fields/base.py (VectorField.database_type)
type DatabaseType int

const (
	// DatabaseTypeMilvus Milvus 向量数据库
	DatabaseTypeMilvus DatabaseType = iota
	// DatabaseTypeChroma ChromaDB 向量数据库
	DatabaseTypeChroma
	// DatabaseTypePG PostgreSQL + pgvector 向量数据库
	DatabaseTypePG
	// DatabaseTypeGauss Gauss 向量数据库
	DatabaseTypeGauss
	// DatabaseTypeES Elasticsearch 向量数据库
	DatabaseTypeES
)

// databaseTypeStrings DatabaseType 枚举值对应的字符串表示，与 Python 枚举值保持一致。
var databaseTypeStrings = [...]string{
	"milvus",
	"chroma",
	"pg",
	"gauss",
	"es",
}

// String 返回 DatabaseType 的字符串表示，与 Python 枚举值一致。
func (dt DatabaseType) String() string {
	if dt >= 0 && int(dt) < len(databaseTypeStrings) {
		return databaseTypeStrings[dt]
	}
	return fmt.Sprintf("UNKNOWN(%d)", dt)
}

// IndexType 向量索引类型。
//
// 对应 Python: vector_fields/base.py (VectorField.index_type)
type IndexType int

const (
	// IndexTypeAUTO 自动选择索引类型
	IndexTypeAUTO IndexType = iota
	// IndexTypeHNSW HNSW 索引
	IndexTypeHNSW
	// IndexTypeFLAT FLAT 索引（精确搜索）
	IndexTypeFLAT
	// IndexTypeIVF IVF 索引
	IndexTypeIVF
	// IndexTypeSCANN SCANN 索引
	IndexTypeSCANN
	// IndexTypeIVFFlat IVFFlat 索引（PG）
	IndexTypeIVFFlat
)

// indexTypeStrings IndexType 枚举值对应的字符串表示，与 Python 枚举值保持一致。
var indexTypeStrings = [...]string{
	"auto",
	"hnsw",
	"flat",
	"ivf",
	"scann",
	"ivfflat",
}

// String 返回 IndexType 的字符串表示，与 Python 枚举值一致。
func (it IndexType) String() string {
	if it >= 0 && int(it) < len(indexTypeStrings) {
		return indexTypeStrings[it]
	}
	return fmt.Sprintf("UNKNOWN(%d)", it)
}
```

- [ ] **Step 4: 运行测试验证通过**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/store/vector_fields/ -run "TestDatabaseType|TestIndexType" -v`
Expected: 所有测试通过

- [ ] **Step 5: 提交**

```bash
cd /home/opensource/uap-claw-go && git add internal/agentcore/store/vector_fields/base.go internal/agentcore/store/vector_fields/base_test.go && git commit -m "feat(store): 实现 DatabaseType 和 IndexType 枚举"
```

---

### Task 3: 实现 VectorField 结构体与构造函数

**Files:**
- Modify: `internal/agentcore/store/vector_fields/base.go`
- Modify: `internal/agentcore/store/vector_fields/base_test.go`

- [ ] **Step 1: 编写 VectorField 构造函数和 Validate 的失败测试**

在 `base_test.go` 中追加：

```go
// ──── VectorField 构造测试 ────

// TestNewVectorField_基本创建 验证基本创建
func TestNewVectorField_基本创建(t *testing.T) {
	vf := NewVectorField(DatabaseTypeMilvus, IndexTypeHNSW, "embedding")
	if vf.DatabaseType != DatabaseTypeMilvus {
		t.Errorf("DatabaseType = %v, 期望 %v", vf.DatabaseType, DatabaseTypeMilvus)
	}
	if vf.IndexType != IndexTypeHNSW {
		t.Errorf("IndexType = %v, 期望 %v", vf.IndexType, IndexTypeHNSW)
	}
	if vf.VectorFieldName != "embedding" {
		t.Errorf("VectorFieldName = %q, 期望 %q", vf.VectorFieldName, "embedding")
	}
}

// TestVectorField_Validate_基类默认 验证基类 Validate 返回 nil
func TestVectorField_Validate_基类默认(t *testing.T) {
	vf := NewVectorField(DatabaseTypeMilvus, IndexTypeAUTO, "embedding")
	if err := vf.Validate(); err != nil {
		t.Errorf("基类 Validate() 应返回 nil, 实际: %v", err)
	}
}
```

- [ ] **Step 2: 运行测试验证失败**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/store/vector_fields/ -run "TestNewVectorField_基本创建" -v`
Expected: 编译失败，NewVectorField 未定义

- [ ] **Step 3: 实现 VectorField 结构体和构造函数**

在 `base.go` 的枚举区块之后、常量区块中追加常量，然后添加结构体区块和导出函数区块：

```go
// ──────────────────────────── 常量 ────────────────────────────

const (
	// StageConstruct 构建阶段（建索引时的参数）
	StageConstruct = "construct"
	// StageSearch 搜索阶段（查询时的参数）
	StageSearch = "search"
)

// ──────────────────────────── 结构体 ────────────────────────────

// VectorField 向量索引配置基类。
//
// 子类通过嵌入 VectorField 并在字段上添加 `vf:"construct"` 或 `vf:"search"`
// 结构体标签来标记字段所属阶段。ToDict(stage) 通过反射读取标签，
// 只输出匹配阶段的字段。
//
// 内部字段（DatabaseType、IndexType、VectorFieldName）用 `vf:"-"` 标记，
// 始终被过滤，不会出现在 ToDict 输出中。
//
// 对应 Python: vector_fields/base.py (VectorField)
type VectorField struct {
	// DatabaseType 向量数据库类型
	DatabaseType DatabaseType `vf:"-"`
	// IndexType 索引类型
	IndexType IndexType `vf:"-"`
	// VectorFieldName 向量字段名
	VectorFieldName string `vf:"-"`
}

// ──────────────────────────── 导出函数 ────────────────────────────

// NewVectorField 创建向量索引配置基类实例。
//
// 对应 Python: VectorField(vector_field=..., database_type=..., index_type=...)
func NewVectorField(dbType DatabaseType, indexType IndexType, fieldName string) *VectorField {
	return &VectorField{
		DatabaseType:    dbType,
		IndexType:       indexType,
		VectorFieldName: fieldName,
	}
}

// Validate 校验配置参数，子类可覆盖实现自定义校验逻辑。
//
// 基类默认返回 nil。对应 Python: VectorField 子类的 @model_validator 逻辑。
func (vf *VectorField) Validate() error {
	return nil
}
```

- [ ] **Step 4: 运行测试验证通过**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/store/vector_fields/ -run "TestNewVectorField|TestVectorField_Validate" -v`
Expected: 所有测试通过

- [ ] **Step 5: 提交**

```bash
cd /home/opensource/uap-claw-go && git add internal/agentcore/store/vector_fields/base.go internal/agentcore/store/vector_fields/base_test.go && git commit -m "feat(store): 实现 VectorField 基类与构造函数"
```

---

### Task 4: 实现 parseVFTag 标签解析

**Files:**
- Modify: `internal/agentcore/store/vector_fields/base.go`
- Modify: `internal/agentcore/store/vector_fields/base_test.go`

- [ ] **Step 1: 编写 parseVFTag 的失败测试**

在 `base_test.go` 中追加：

```go
// ──── parseVFTag 标签解析测试 ────

// TestParseVFTag_各格式 验证各种标签格式的解析结果
func TestParseVFTag_各格式(t *testing.T) {
	tests := []struct {
		name        string
		tag         string
		wantStage   string
		wantKeepZero bool
	}{
		{"construct", "construct", "construct", false},
		{"search", "search", "search", false},
		{"dash", "-", "-", false},
		{"construct_keepzero", "construct,keepzero", "construct", true},
		{"search_keepzero", "search,keepzero", "search", true},
		{"空字符串", "", "", false},
		{"unknown", "unknown", "unknown", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stage, keepZero := parseVFTag(tt.tag)
			if stage != tt.wantStage {
				t.Errorf("stage = %q, 期望 %q", stage, tt.wantStage)
			}
			if keepZero != tt.wantKeepZero {
				t.Errorf("keepZero = %v, 期望 %v", keepZero, tt.wantKeepZero)
			}
		})
	}
}
```

- [ ] **Step 2: 运行测试验证失败**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/store/vector_fields/ -run "TestParseVFTag" -v`
Expected: 编译失败，parseVFTag 未定义

- [ ] **Step 3: 实现 parseVFTag 函数**

在 `base.go` 的非导出函数区块中追加：

```go
// ──────────────────────────── 非导出函数 ────────────────────────────

// parseVFTag 解析 vf 结构体标签，返回 stage 和是否 keepzero。
//
// 标签格式："<stage>" 或 "<stage>,keepzero"
// 示例："construct" → ("construct", false)
//
//	"search,keepzero" → ("search", true)
//	"-" → ("-", false)
//	"" → ("", false)
func parseVFTag(tag string) (stage string, keepZero bool) {
	if tag == "" {
		return "", false
	}
	parts := strings.Split(tag, ",")
	stage = parts[0]
	for _, p := range parts[1:] {
		if strings.TrimSpace(p) == "keepzero" {
			keepZero = true
		}
	}
	return stage, keepZero
}
```

- [ ] **Step 4: 运行测试验证通过**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/store/vector_fields/ -run "TestParseVFTag" -v`
Expected: 所有测试通过

- [ ] **Step 5: 提交**

```bash
cd /home/opensource/uap-claw-go && git add internal/agentcore/store/vector_fields/base.go internal/agentcore/store/vector_fields/base_test.go && git commit -m "feat(store): 实现 parseVFTag 标签解析"
```

---

### Task 5: 实现 ToDict 反射机制

**Files:**
- Modify: `internal/agentcore/store/vector_fields/base.go`
- Modify: `internal/agentcore/store/vector_fields/base_test.go`

- [ ] **Step 1: 编写测试用子类和 ToDict 失败测试**

在 `base_test.go` 中追加测试用子类和测试函数：

```go
// ──── 测试用子类 ────

// testVectorField 测试用子类，模拟未来子类的字段标记方式
type testVectorField struct {
	VectorField
	// ConstructParam 构建阶段参数
	ConstructParam int `vf:"construct"`
	// SearchParam 搜索阶段参数
	SearchParam float64 `vf:"search"`
	// KeepZeroParam 带零值保留的搜索参数
	KeepZeroParam int `vf:"search,keepzero"`
	// IgnoredField 无 vf 标签，应被过滤
	IgnoredField string
	// ExtraConstruct 构建阶段额外参数
	ExtraConstruct map[string]any `vf:"construct"`
	// ExtraSearch 搜索阶段额外参数
	ExtraSearch map[string]any `vf:"search"`
}

// ──── ToDict 反射机制测试 ────

// TestToDict_Construct阶段 验证只输出 construct 阶段字段
func TestToDict_Construct阶段(t *testing.T) {
	vf := &testVectorField{
		VectorField:    VectorField{DatabaseType: DatabaseTypeMilvus, IndexType: IndexTypeHNSW, VectorFieldName: "embedding"},
		ConstructParam: 30,
		SearchParam:    100.0,
	}
	got := vf.ToDict(StageConstruct)
	if _, ok := got["ConstructParam"]; !ok {
		t.Error("construct 阶段应包含 ConstructParam")
	}
	if got["ConstructParam"] != 30 {
		t.Errorf("ConstructParam = %v, 期望 30", got["ConstructParam"])
	}
	if _, ok := got["SearchParam"]; ok {
		t.Error("construct 阶段不应包含 SearchParam")
	}
}

// TestToDict_Search阶段 验证只输出 search 阶段字段
func TestToDict_Search阶段(t *testing.T) {
	vf := &testVectorField{
		VectorField:    VectorField{DatabaseType: DatabaseTypeMilvus, IndexType: IndexTypeHNSW, VectorFieldName: "embedding"},
		ConstructParam: 30,
		SearchParam:    100.0,
	}
	got := vf.ToDict(StageSearch)
	if _, ok := got["SearchParam"]; !ok {
		t.Error("search 阶段应包含 SearchParam")
	}
	if got["SearchParam"] != 100.0 {
		t.Errorf("SearchParam = %v, 期望 100.0", got["SearchParam"])
	}
	if _, ok := got["ConstructParam"]; ok {
		t.Error("search 阶段不应包含 ConstructParam")
	}
}

// TestToDict_零值过滤 验证默认行为下零值不输出
func TestToDict_零值过滤(t *testing.T) {
	vf := &testVectorField{
		VectorField:    VectorField{DatabaseType: DatabaseTypeMilvus, IndexType: IndexTypeHNSW, VectorFieldName: "embedding"},
		ConstructParam: 0, // 零值，应被过滤
		SearchParam:    0.0,
	}
	got := vf.ToDict(StageConstruct)
	if _, ok := got["ConstructParam"]; ok {
		t.Error("零值 ConstructParam 应被过滤")
	}
	got = vf.ToDict(StageSearch)
	if _, ok := got["SearchParam"]; ok {
		t.Error("零值 SearchParam 应被过滤")
	}
}

// TestToDict_KeepZero保留零值 验证 keepzero 修饰符保留零值
func TestToDict_KeepZero保留零值(t *testing.T) {
	vf := &testVectorField{
		VectorField:   VectorField{DatabaseType: DatabaseTypePG, IndexType: IndexTypeHNSW, VectorFieldName: "embedding"},
		KeepZeroParam: 0, // 零值但有 keepzero，应保留
	}
	got := vf.ToDict(StageSearch)
	if _, ok := got["KeepZeroParam"]; !ok {
		t.Error("keepzero 字段零值应保留")
	}
	if got["KeepZeroParam"] != 0 {
		t.Errorf("KeepZeroParam = %v, 期望 0", got["KeepZeroParam"])
	}
}

// TestToDict_内部字段过滤 验证 vf:"-" 内部字段不输出
func TestToDict_内部字段过滤(t *testing.T) {
	vf := &testVectorField{
		VectorField:    VectorField{DatabaseType: DatabaseTypeMilvus, IndexType: IndexTypeHNSW, VectorFieldName: "embedding"},
		ConstructParam: 30,
	}
	// 两个阶段都不应包含内部字段
	for _, stage := range []string{StageConstruct, StageSearch} {
		got := vf.ToDict(stage)
		if _, ok := got["DatabaseType"]; ok {
			t.Errorf("%s 阶段不应包含 DatabaseType", stage)
		}
		if _, ok := got["IndexType"]; ok {
			t.Errorf("%s 阶段不应包含 IndexType", stage)
		}
		if _, ok := got["VectorFieldName"]; ok {
			t.Errorf("%s 阶段不应包含 VectorFieldName", stage)
		}
	}
}

// TestToDict_无标签字段过滤 验证无 vf 标签字段不输出
func TestToDict_无标签字段过滤(t *testing.T) {
	vf := &testVectorField{
		VectorField:   VectorField{DatabaseType: DatabaseTypeMilvus, IndexType: IndexTypeHNSW, VectorFieldName: "embedding"},
		IgnoredField:  "should_not_appear",
		ConstructParam: 30,
	}
	got := vf.ToDict(StageConstruct)
	if _, ok := got["IgnoredField"]; ok {
		t.Error("无 vf 标签字段不应输出")
	}
}

// TestToDict_Extra合并 验证 ExtraConstruct/ExtraSearch 展开合并
func TestToDict_Extra合并(t *testing.T) {
	vf := &testVectorField{
		VectorField:    VectorField{DatabaseType: DatabaseTypeMilvus, IndexType: IndexTypeHNSW, VectorFieldName: "embedding"},
		ConstructParam: 30,
		ExtraConstruct: map[string]any{"custom_key": "custom_value"},
		ExtraSearch:    map[string]any{"search_key": 42},
	}
	got := vf.ToDict(StageConstruct)
	if got["custom_key"] != "custom_value" {
		t.Errorf("ExtraConstruct 合并后 custom_key = %v, 期望 %q", got["custom_key"], "custom_value")
	}
	// ExtraConstruct 本身不应作为 key 出现
	if _, ok := got["ExtraConstruct"]; ok {
		t.Error("ExtraConstruct 不应作为 key 出现在结果中")
	}
	got = vf.ToDict(StageSearch)
	if got["search_key"] != 42 {
		t.Errorf("ExtraSearch 合并后 search_key = %v, 期望 42", got["search_key"])
	}
}

// TestToDict_ExtraNil和空 验证 Extra 为 nil 或空时不影响输出
func TestToDict_ExtraNil和空(t *testing.T) {
	vf := &testVectorField{
		VectorField:    VectorField{DatabaseType: DatabaseTypeMilvus, IndexType: IndexTypeHNSW, VectorFieldName: "embedding"},
		ConstructParam: 30,
		ExtraConstruct: nil,
		ExtraSearch:    map[string]any{},
	}
	got := vf.ToDict(StageConstruct)
	if got["ConstructParam"] != 30 {
		t.Errorf("ConstructParam = %v, 期望 30", got["ConstructParam"])
	}
	got = vf.ToDict(StageSearch)
	if len(got) != 0 {
		t.Errorf("空 ExtraSearch + 零值 SearchParam 应无输出, 实际: %v", got)
	}
}

// TestToDict_Extra覆盖 验证 Extra 字段 key 覆盖普通字段 key
func TestToDict_Extra覆盖(t *testing.T) {
	vf := &testVectorField{
		VectorField:    VectorField{DatabaseType: DatabaseTypeMilvus, IndexType: IndexTypeHNSW, VectorFieldName: "embedding"},
		ConstructParam: 30,
		ExtraConstruct: map[string]any{"ConstructParam": 999}, // 覆盖同名字段
	}
	got := vf.ToDict(StageConstruct)
	// Extra 字段覆盖普通字段（与 Python `|` 运算符行为一致）
	if got["ConstructParam"] != 999 {
		t.Errorf("Extra 覆盖后 ConstructParam = %v, 期望 999", got["ConstructParam"])
	}
}

// TestToDict_嵌入子类 验证子类嵌入 VectorField 后字段正确读取
func TestToDict_嵌入子类(t *testing.T) {
	vf := &testVectorField{
		VectorField:    VectorField{DatabaseType: DatabaseTypeChroma, IndexType: IndexTypeHNSW, VectorFieldName: "embedding"},
		ConstructParam: 16,
		SearchParam:    100.0,
		KeepZeroParam:  0,
	}
	got := vf.ToDict(StageConstruct)
	if got["ConstructParam"] != 16 {
		t.Errorf("ConstructParam = %v, 期望 16", got["ConstructParam"])
	}
	got = vf.ToDict(StageSearch)
	if got["SearchParam"] != 100.0 {
		t.Errorf("SearchParam = %v, 期望 100.0", got["SearchParam"])
	}
	if got["KeepZeroParam"] != 0 {
		t.Errorf("KeepZeroParam = %v, 期望 0", got["KeepZeroParam"])
	}
}

// TestToDict_基类实例 验证纯基类实例 ToDict 返回空 map（所有字段都是 vf:"-"）
func TestToDict_基类实例(t *testing.T) {
	vf := NewVectorField(DatabaseTypeMilvus, IndexTypeHNSW, "embedding")
	got := vf.ToDict(StageConstruct)
	if len(got) != 0 {
		t.Errorf("纯基类 ToDict 应返回空 map, 实际: %v", got)
	}
	got = vf.ToDict(StageSearch)
	if len(got) != 0 {
		t.Errorf("纯基类 ToDict 应返回空 map, 实际: %v", got)
	}
}
```

- [ ] **Step 2: 运行测试验证失败**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/store/vector_fields/ -run "TestToDict" -v`
Expected: 编译失败，ToDict 方法未定义

- [ ] **Step 3: 实现 ToDict 方法**

在 `base.go` 的导出函数区块中（Validate 之后）追加：

```go
// ToDict 将向量索引配置转为字典格式，只输出指定阶段的字段。
//
// 通过反射读取 vf 结构体标签，过滤规则：
//   - vf:"-" 或无标签 → 跳过（内部字段）
//   - vf 标签 stage 与参数不匹配 → 跳过
//   - vf 标签 stage 匹配 → 检查零值和 Extra 合并
//   - 无 keepzero 修饰且为零值 → 跳过
//   - 字段名以 Extra 开头且类型为 map[string]any → 展开合并到结果
//
// 对应 Python: VectorField.to_dict(stage)
func (vf *VectorField) ToDict(stage string) map[string]any {
	result := make(map[string]any)
	collectFields(reflect.ValueOf(vf).Elem(), stage, result)
	return result
}
```

在非导出函数区块中追加：

```go
// collectFields 递归收集匹配 stage 的字段到 result 中。
// 处理嵌入结构体的提升字段，跳过嵌入字段本身。
func collectFields(v reflect.Value, stage string, result map[string]any) {
	t := v.Type()
	for i := 0; i < t.NumField(); i++ {
		field := t.Field(i)
		fieldValue := v.Field(i)

		// 匿名嵌入字段：递归处理其子字段
		if field.Anonymous && fieldValue.Kind() == reflect.Struct {
			collectFields(fieldValue, stage, result)
			continue
		}

		// 解析 vf 标签
		tagStr, ok := field.Tag.Lookup("vf")
		if !ok {
			continue // 无 vf 标签，当作内部字段跳过
		}
		fieldStage, keepZero := parseVFTag(tagStr)

		// 过滤内部字段和阶段不匹配的字段
		if fieldStage == "-" || fieldStage != stage {
			continue
		}

		// Extra 字段合并逻辑：字段名以 Extra 开头且类型为 map[string]any
		if strings.HasPrefix(field.Name, "Extra") && fieldValue.Type() == reflect.TypeOf(map[string]any{}) {
			if fieldValue.IsNil() {
				continue
			}
			extraMap := fieldValue.Interface().(map[string]any)
			for k, v := range extraMap {
				result[k] = v
			}
			continue
		}

		// 零值过滤（除非 keepzero）
		if !keepZero && fieldValue.IsZero() {
			continue
		}

		result[field.Name] = fieldValue.Interface()
	}
}
```

- [ ] **Step 4: 运行测试验证通过**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/store/vector_fields/ -run "TestToDict" -v`
Expected: 所有测试通过

- [ ] **Step 5: 提交**

```bash
cd /home/opensource/uap-claw-go && git add internal/agentcore/store/vector_fields/base.go internal/agentcore/store/vector_fields/base_test.go && git commit -m "feat(store): 实现 VectorField.ToDict 反射机制"
```

---

### Task 6: 运行完整测试并检查覆盖率

**Files:**
- No new files

- [ ] **Step 1: 运行全部测试**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/store/vector_fields/ -v`
Expected: 所有测试通过

- [ ] **Step 2: 检查覆盖率**

Run: `cd /home/opensource/uap-claw-go && go test -cover ./internal/agentcore/store/vector_fields/`
Expected: 覆盖率 ≥ 85%

- [ ] **Step 3: 如覆盖率不足，补充测试**

如果覆盖率 < 85%，运行 `go test -coverprofile=coverage.out ./internal/agentcore/store/vector_fields/ && go tool cover -func=coverage.out` 查看详情，补充缺失测试直到达标。

- [ ] **Step 4: 运行全项目测试确认无回归**

Run: `cd /home/opensource/uap-claw-go && go test ./...`
Expected: 所有测试通过，无回归

---

### Task 7: 更新 IMPLEMENTATION_PLAN.md

**Files:**
- Modify: `IMPLEMENTATION_PLAN.md`

- [ ] **Step 1: 更新步骤 4.7 状态**

找到步骤 4.7 相关行，确认状态已正确标记。如果当前标记为 ✅ 但缺少 vector_fields 部分，在状态行或备注中补充说明"VectorField 基类 + vf 标签反射机制已实现，具体后端子类留到 4.8-4.11"。

- [ ] **Step 2: 提交**

```bash
cd /home/opensource/uap-claw-go && git add IMPLEMENTATION_PLAN.md && git commit -m "docs: 更新实现计划 4.7 状态"
```

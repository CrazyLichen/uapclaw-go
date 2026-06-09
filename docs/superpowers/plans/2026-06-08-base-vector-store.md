# BaseVectorStore 接口实现计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 实现 4.6 BaseVectorStore 接口及配套数据模型（VectorDataType / FieldSchema / CollectionSchema / VectorSearchResult / Option），对照 Python 源码 `base_vector_store.py` 迁移到 Go。

**Architecture:** 在 `internal/agentcore/store/vector/` 包下创建 `doc.go` 和 `base.go`，定义向量存储的抽象接口和配套 Schema 类型。接口采用同步风格（ctx + 阻塞调用），Schema 通过构造函数校验，CollectionSchema 指针可变支持链式调用。9 个核心方法先行，3 个迁移方法延后。

**Tech Stack:** Go 1.22+, 项目自定义 exception 包（`StatusStoreVectorSchemaInvalid` 等）

**设计文档:** `docs/superpowers/specs/2026-06-08-base-vector-store-design.md`

---

## 文件结构

```
internal/agentcore/store/vector/
├── doc.go        # 包文档（创建）
├── base.go       # 全部类型 + 接口 + Option（创建）
└── base_test.go  # 单元测试（创建）
```

---

### Task 1: 创建 doc.go 包文档

**Files:**
- Create: `internal/agentcore/store/vector/doc.go`

- [ ] **Step 1: 创建 vector 目录并写入 doc.go**

```go
// Package vector 提供向量存储的抽象接口定义和配套 Schema 类型。
//
// 本包定义了所有向量存储后端必须满足的 BaseVectorStore 接口，
// 以及用于描述集合结构的 CollectionSchema、FieldSchema 等数据模型。
// 当前仅有接口和类型定义，具体后端实现（Milvus、Chroma 等）在后续步骤中添加。
//
// 文件目录：
//
//	vector/
//	├── doc.go        # 包文档
//	└── base.go       # VectorDataType + FieldSchema + CollectionSchema + VectorSearchResult + BaseVectorStore + Option
//
// 对应 Python 代码：openjiuwen/core/foundation/store/base_vector_store.py
//
// 核心类型/接口索引：
//
//	VectorDataType      — 字段数据类型枚举（VARCHAR, FLOAT_VECTOR, INT64 等）
//	FieldSchema         — 集合字段 Schema 定义，通过 NewFieldSchema 构造并校验
//	CollectionSchema    — 集合 Schema 定义，通过 NewCollectionSchema 构造并校验
//	VectorSearchResult  — 向量搜索结果，包含 Score 和 Fields
//	BaseVectorStore     — 向量存储基础接口，定义集合 CRUD 和向量搜索
//	Option              — 操作可选参数的函数选项模式
package vector
```

- [ ] **Step 2: 验证编译**

Run: `cd /home/opensource/uap-claw-go && go build ./internal/agentcore/store/vector/`
Expected: 编译成功（无输出）

- [ ] **Step 3: 提交**

```bash
cd /home/opensource/uap-claw-go && git add internal/agentcore/store/vector/doc.go && git commit -m "feat(store/vector): 添加包文档 doc.go"
```

---

### Task 2: 实现 VectorDataType 枚举 + FieldSchema 结构体 + FieldOption

**Files:**
- Create: `internal/agentcore/store/vector/base.go`（初始版本，逐步扩展）

- [ ] **Step 1: 写 VectorDataType 枚举的失败测试**

在 `internal/agentcore/store/vector/base_test.go` 中：

```go
package vector

import (
	"testing"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// TestVectorDataType_String 验证枚举值与 Python 字符串一致
func TestVectorDataType_String(t *testing.T) {
	tests := []struct {
		dt       VectorDataType
		expected string
	}{
		{VectorDataTypeVarchar, "VARCHAR"},
		{VectorDataTypeFloatVector, "FLOAT_VECTOR"},
		{VectorDataTypeInt64, "INT64"},
		{VectorDataTypeInt32, "INT32"},
		{VectorDataTypeInt16, "INT16"},
		{VectorDataTypeInt8, "INT8"},
		{VectorDataTypeFloat, "FLOAT"},
		{VectorDataTypeDouble, "DOUBLE"},
		{VectorDataTypeBool, "BOOL"},
		{VectorDataTypeJSON, "JSON"},
		{VectorDataTypeArray, "ARRAY"},
	}
	for _, tt := range tests {
		if got := tt.dt.String(); got != tt.expected {
			t.Errorf("VectorDataType(%d).String() = %q, 期望 %q", tt.dt, got, tt.expected)
		}
	}
}

// TestVectorDataType_无效值 验证未定义枚举值的字符串输出
func TestVectorDataType_无效值(t *testing.T) {
	dt := VectorDataType(999)
	if got := dt.String(); got != "UNKNOWN(999)" {
		t.Errorf("未知枚举值 String() = %q, 期望 %q", got, "UNKNOWN(999)")
	}
}
```

- [ ] **Step 2: 运行测试验证失败**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/store/vector/ -run TestVectorDataType -v`
Expected: 编译失败（VectorDataType 未定义）

- [ ] **Step 3: 实现 VectorDataType 枚举**

在 `internal/agentcore/store/vector/base.go` 中：

```go
package vector

import (
	"context"
	"fmt"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ──────────────────────────── 枚举 ────────────────────────────

// VectorDataType 向量存储支持的字段数据类型。
//
// 对应 Python: openjiuwen/core/foundation/store/base_vector_store.py (VectorDataType)
type VectorDataType int

const (
	// VectorDataTypeVarchar 变长字符串
	VectorDataTypeVarchar VectorDataType = iota
	// VectorDataTypeFloatVector 浮点向量
	VectorDataTypeFloatVector
	// VectorDataTypeInt64 64位整数
	VectorDataTypeInt64
	// VectorDataTypeInt32 32位整数
	VectorDataTypeInt32
	// VectorDataTypeInt16 16位整数
	VectorDataTypeInt16
	// VectorDataTypeInt8 8位整数
	VectorDataTypeInt8
	// VectorDataTypeFloat 浮点数
	VectorDataTypeFloat
	// VectorDataTypeDouble 双精度浮点数
	VectorDataTypeDouble
	// VectorDataTypeBool 布尔值
	VectorDataTypeBool
	// VectorDataTypeJSON JSON 对象
	VectorDataTypeJSON
	// VectorDataTypeArray 数组
	VectorDataTypeArray
)

// vectorDataTypeStrings VectorDataType 枚举值对应的字符串表示，与 Python VectorDataType 枚举值保持一致。
var vectorDataTypeStrings = [...]string{
	"VARCHAR",
	"FLOAT_VECTOR",
	"INT64",
	"INT32",
	"INT16",
	"INT8",
	"FLOAT",
	"DOUBLE",
	"BOOL",
	"JSON",
	"ARRAY",
}

// String 返回 VectorDataType 的字符串表示，与 Python 枚举值一致。
func (dt VectorDataType) String() string {
	if dt >= 0 && int(dt) < len(vectorDataTypeStrings) {
		return vectorDataTypeStrings[dt]
	}
	return fmt.Sprintf("UNKNOWN(%d)", dt)
}
```

- [ ] **Step 4: 运行测试验证通过**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/store/vector/ -run TestVectorDataType -v`
Expected: PASS

- [ ] **Step 5: 写 FieldSchema 构造和校验的失败测试**

在 `base_test.go` 中追加：

```go
// TestNewFieldSchema_基本创建 验证基本字段创建
func TestNewFieldSchema_基本创建(t *testing.T) {
	field, err := NewFieldSchema("id", VectorDataTypeVarchar)
	if err != nil {
		t.Fatalf("NewFieldSchema() 返回错误: %v", err)
	}
	if field.Name != "id" {
		t.Errorf("Name = %q, 期望 %q", field.Name, "id")
	}
	if field.DType != VectorDataTypeVarchar {
		t.Errorf("DType = %v, 期望 %v", field.DType, VectorDataTypeVarchar)
	}
}

// TestNewFieldSchema_FloatVector必须提供Dim 验证 FLOAT_VECTOR 类型缺少 dim 时返回错误
func TestNewFieldSchema_FloatVector必须提供Dim(t *testing.T) {
	_, err := NewFieldSchema("embedding", VectorDataTypeFloatVector)
	if err == nil {
		t.Error("FLOAT_VECTOR 缺少 dim 应返回错误")
	}
}

// TestNewFieldSchema_FloatVectorDim为零 验证 dim=0 时返回错误
func TestNewFieldSchema_FloatVectorDim为零(t *testing.T) {
	_, err := NewFieldSchema("embedding", VectorDataTypeFloatVector, WithDim(0))
	if err == nil {
		t.Error("FLOAT_VECTOR dim=0 应返回错误")
	}
}

// TestNewFieldSchema_FloatVectorDim为负 验证 dim<0 时返回错误
func TestNewFieldSchema_FloatVectorDim为负(t *testing.T) {
	_, err := NewFieldSchema("embedding", VectorDataTypeFloatVector, WithDim(-1))
	if err == nil {
		t.Error("FLOAT_VECTOR dim<0 应返回错误")
	}
}

// TestNewFieldSchema_FloatVectorDim合法 验证合法 dim 创建成功
func TestNewFieldSchema_FloatVectorDim合法(t *testing.T) {
	field, err := NewFieldSchema("embedding", VectorDataTypeFloatVector, WithDim(768))
	if err != nil {
		t.Fatalf("NewFieldSchema() 返回错误: %v", err)
	}
	if field.Dim != 768 {
		t.Errorf("Dim = %d, 期望 %d", field.Dim, 768)
	}
}

// TestNewFieldSchema_WithOptions 验证 FieldOption 全部生效
func TestNewFieldSchema_WithOptions(t *testing.T) {
	field, err := NewFieldSchema("id", VectorDataTypeVarchar,
		WithPrimary(),
		WithAutoID(),
		WithMaxLength(256),
		WithFieldDescription("主键字段"),
	)
	if err != nil {
		t.Fatalf("NewFieldSchema() 返回错误: %v", err)
	}
	if !field.IsPrimary {
		t.Error("IsPrimary 应为 true")
	}
	if !field.AutoID {
		t.Error("AutoID 应为 true")
	}
	if field.MaxLength != 256 {
		t.Errorf("MaxLength = %d, 期望 %d", field.MaxLength, 256)
	}
	if field.Description != "主键字段" {
		t.Errorf("Description = %q, 期望 %q", field.Description, "主键字段")
	}
}

// TestNewFieldSchema_名字为空 验证名字为空时返回错误
func TestNewFieldSchema_名字为空(t *testing.T) {
	_, err := NewFieldSchema("", VectorDataTypeVarchar)
	if err == nil {
		t.Error("名字为空应返回错误")
	}
}
```

- [ ] **Step 6: 运行测试验证失败**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/store/vector/ -run TestNewFieldSchema -v`
Expected: 编译失败（NewFieldSchema / FieldOption 未定义）

- [ ] **Step 7: 实现 FieldSchema + FieldOption**

在 `base.go` 的 `// ──────────────────────────── 结构体 ────────────────────────────` 区块下追加：

```go

// FieldSchema 集合中单个字段的 Schema 定义。
//
// 类似 Milvus FieldSchema，支持各种数据类型和字段属性。
// 通过 NewFieldSchema 构造，构造时自动校验字段合法性。
//
// 对应 Python: openjiuwen/core/foundation/store/base_vector_store.py (FieldSchema)
type FieldSchema struct {
	// Name 字段名
	Name string
	// DType 字段数据类型
	DType VectorDataType
	// IsPrimary 是否为主键字段
	IsPrimary bool
	// AutoID 是否自动生成 ID
	AutoID bool
	// MaxLength VARCHAR 字段最大长度，0 表示使用默认值 65535
	MaxLength int
	// Dim FLOAT_VECTOR 字段的向量维度，0 表示未设置
	Dim int
	// ElementType ARRAY 字段的元素类型，0 表示未设置
	ElementType VectorDataType
	// MaxCapacity ARRAY 字段的最大容量，0 表示未设置
	MaxCapacity int
	// Description 字段描述
	Description string
	// DefaultValue 字段默认值
	DefaultValue any
}
```

在 `// ──────────────────────────── 常量 ────────────────────────────` 区块下追加：

```go

// defaultMaxLength VARCHAR 字段默认最大长度，对齐 Python FieldSchema.max_length 默认值
const defaultMaxLength = 65535
```

在 `// ──────────────────────────── 导出函数 ────────────────────────────` 区块下追加：

```go

// NewFieldSchema 创建并校验 FieldSchema。
//
// 校验规则：
//   - 名字不能为空
//   - DType 为 FloatVector 时 Dim 必须大于 0
//   - Dim 大于 0 时（非 FloatVector 类型传入了 Dim）不报错，但语义上无意义
//
// 对应 Python: FieldSchema(name=..., dtype=..., ...) 的 Pydantic 校验
func NewFieldSchema(name string, dtype VectorDataType, opts ...FieldOption) (*FieldSchema, error) {
	if name == "" {
		return nil, exception.BuildError(exception.StatusStoreVectorSchemaInvalid,
			exception.WithErrorMessage(fmt.Sprintf("field name is empty")),
		)
	}
	f := &FieldSchema{
		Name:  name,
		DType: dtype,
	}
	for _, opt := range opts {
		opt(f)
	}
	// 校验 dim：FLOAT_VECTOR 必须提供 dim，dim 必须大于 0
	if f.Dim < 0 {
		return nil, exception.BuildError(exception.StatusStoreVectorSchemaInvalid,
			exception.WithErrorMessage(fmt.Sprintf("dim of vector field is invalid, field=%s, dim=%d", f.Name, f.Dim)),
		)
	}
	if f.DType == VectorDataTypeFloatVector && f.Dim == 0 {
		return nil, exception.BuildError(exception.StatusStoreVectorSchemaInvalid,
			exception.WithErrorMessage(fmt.Sprintf("dim of vector field is missing, field=%s, dim=%d", f.Name, f.Dim)),
		)
	}
	return f, nil
}
```

在文件中添加 FieldOption 类型定义和选项函数。在 `// ──────────────────────────── 枚举 ────────────────────────────` 和 `// ──────────────────────────── 常量 ────────────────────────────` 之间添加：

```go

// ──────────────────────────── 函数选项 ────────────────────────────

// FieldOption FieldSchema 构造选项
type FieldOption func(*FieldSchema)

// WithPrimary 设置为主键字段
func WithPrimary() FieldOption {
	return func(f *FieldSchema) { f.IsPrimary = true }
}

// WithAutoID 设置自动生成 ID
func WithAutoID() FieldOption {
	return func(f *FieldSchema) { f.AutoID = true }
}

// WithMaxLength 设置 VARCHAR 字段最大长度
func WithMaxLength(maxLen int) FieldOption {
	return func(f *FieldSchema) { f.MaxLength = maxLen }
}

// WithDim 设置向量维度
func WithDim(dim int) FieldOption {
	return func(f *FieldSchema) { f.Dim = dim }
}

// WithElementType 设置 ARRAY 元素类型
func WithElementType(dt VectorDataType) FieldOption {
	return func(f *FieldSchema) { f.ElementType = dt }
}

// WithMaxCapacity 设置 ARRAY 最大容量
func WithMaxCapacity(cap int) FieldOption {
	return func(f *FieldSchema) { f.MaxCapacity = cap }
}

// WithFieldDescription 设置字段描述
func WithFieldDescription(desc string) FieldOption {
	return func(f *FieldSchema) { f.Description = desc }
}

// WithDefaultValue 设置字段默认值
func WithDefaultValue(val any) FieldOption {
	return func(f *FieldSchema) { f.DefaultValue = val }
}

// CollectionOption CollectionSchema 构造选项
type CollectionOption func(*CollectionSchema)

// WithCollectionDescription 设置集合描述
func WithCollectionDescription(desc string) CollectionOption {
	return func(s *CollectionSchema) { s.Description = desc }
}

// WithEnableDynamicField 启用动态字段
func WithEnableDynamicField() CollectionOption {
	return func(s *CollectionSchema) { s.EnableDynamicField = true }
}

// Option 向量存储操作的通用可选参数
type Option func(*Options)

// Options 向量存储操作的可选参数集合
type Options struct {
	// DistanceMetric 距离度量方式（如 "COSINE"、"L2"、"IP"）
	DistanceMetric string
	// BatchSize 批量操作的批次大小
	BatchSize int
	// MetricType 搜索时的距离度量类型
	MetricType string
	// OutputFields 搜索结果中需要返回的字段列表
	OutputFields []string
}

// WithDistanceMetric 设置距离度量方式
func WithDistanceMetric(metric string) Option {
	return func(o *Options) { o.DistanceMetric = metric }
}

// WithBatchSize 设置批量操作的批次大小
func WithBatchSize(size int) Option {
	return func(o *Options) { o.BatchSize = size }
}

// WithMetricType 设置搜索时的距离度量类型
func WithMetricType(metricType string) Option {
	return func(o *Options) { o.MetricType = metricType }
}

// WithOutputFields 设置搜索结果中需要返回的字段
func WithOutputFields(fields ...string) Option {
	return func(o *Options) { o.OutputFields = fields }
}

// newOptions 从选项列表构造 Options
func newOptions(opts ...Option) Options {
	var o Options
	for _, opt := range opts {
		opt(&o)
	}
	return o
}
```

同时在 import 中添加 `"github.com/opensource-uap/uap-claw-go/internal/common/exception"`。

- [ ] **Step 8: 运行测试验证通过**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/store/vector/ -run TestNewFieldSchema -v`
Expected: PASS

- [ ] **Step 9: 提交**

```bash
cd /home/opensource/uap-claw-go && git add internal/agentcore/store/vector/base.go internal/agentcore/store/vector/base_test.go && git commit -m "feat(store/vector): 实现 VectorDataType 枚举 + FieldSchema + FieldOption"
```

---

### Task 3: 实现 FieldSchema 序列化方法（ToDict / FieldFromDict）

**Files:**
- Modify: `internal/agentcore/store/vector/base.go`
- Modify: `internal/agentcore/store/vector/base_test.go`

- [ ] **Step 1: 写 FieldSchema 序列化的失败测试**

在 `base_test.go` 中追加：

```go
// TestFieldSchema_ToDict_完整字段 验证所有非零值字段序列化
func TestFieldSchema_ToDict_完整字段(t *testing.T) {
	field, _ := NewFieldSchema("embedding", VectorDataTypeFloatVector,
		WithDim(768), WithFieldDescription("向量字段"))
	got := field.ToDict()

	if got["name"] != "embedding" {
		t.Errorf("name = %v, 期望 %q", got["name"], "embedding")
	}
	if got["type"] != "FLOAT_VECTOR" {
		t.Errorf("type = %v, 期望 %q", got["type"], "FLOAT_VECTOR")
	}
	if got["dim"] != 768 {
		t.Errorf("dim = %v, 期望 %v", got["dim"], 768)
	}
	if got["description"] != "向量字段" {
		t.Errorf("description = %v, 期望 %q", got["description"], "向量字段")
	}
}

// TestFieldSchema_ToDict_主键字段 验证主键和 auto_id 序列化
func TestFieldSchema_ToDict_主键字段(t *testing.T) {
	field, _ := NewFieldSchema("id", VectorDataTypeVarchar,
		WithPrimary(), WithAutoID(), WithMaxLength(256))
	got := field.ToDict()

	if got["is_primary"] != true {
		t.Error("is_primary 应为 true")
	}
	if got["auto_id"] != true {
		t.Error("auto_id 应为 true")
	}
	if got["max_length"] != 256 {
		t.Errorf("max_length = %v, 期望 %v", got["max_length"], 256)
	}
}

// TestFieldSchema_ToDict_零值省略 验证零值字段不序列化
func TestFieldSchema_ToDict_零值省略(t *testing.T) {
	field, _ := NewFieldSchema("text", VectorDataTypeVarchar)
	got := field.ToDict()

	if _, ok := got["is_primary"]; ok {
		t.Error("IsPrimary=false 不应序列化")
	}
	if _, ok := got["auto_id"]; ok {
		t.Error("AutoID=false 不应序列化")
	}
	if _, ok := got["dim"]; ok {
		t.Error("Dim=0 不应序列化")
	}
}

// TestFieldSchema_ToDict_MaxLength默认值 验证未设置 MaxLength 时序列化为默认 65535
func TestFieldSchema_ToDict_MaxLength默认值(t *testing.T) {
	field, _ := NewFieldSchema("content", VectorDataTypeVarchar)
	got := field.ToDict()

	if got["max_length"] != defaultMaxLength {
		t.Errorf("max_length = %v, 期望 %v", got["max_length"], defaultMaxLength)
	}
}

// TestFieldFromDict_基本反序列化 验证从字典创建 FieldSchema
func TestFieldFromDict_基本反序列化(t *testing.T) {
	data := map[string]any{
		"name":       "embedding",
		"type":       "FLOAT_VECTOR",
		"dim":        768,
		"is_primary": false,
	}
	field, err := FieldFromDict(data)
	if err != nil {
		t.Fatalf("FieldFromDict() 返回错误: %v", err)
	}
	if field.Name != "embedding" {
		t.Errorf("Name = %q, 期望 %q", field.Name, "embedding")
	}
	if field.DType != VectorDataTypeFloatVector {
		t.Errorf("DType = %v, 期望 %v", field.DType, VectorDataTypeFloatVector)
	}
	if field.Dim != 768 {
		t.Errorf("Dim = %d, 期望 %d", field.Dim, 768)
	}
}

// TestFieldFromDict_主键字段 验证主键字段反序列化
func TestFieldFromDict_主键字段(t *testing.T) {
	data := map[string]any{
		"name":       "id",
		"type":       "VARCHAR",
		"is_primary": true,
		"auto_id":    true,
		"max_length": 256,
	}
	field, err := FieldFromDict(data)
	if err != nil {
		t.Fatalf("FieldFromDict() 返回错误: %v", err)
	}
	if !field.IsPrimary {
		t.Error("IsPrimary 应为 true")
	}
	if !field.AutoID {
		t.Error("AutoID 应为 true")
	}
	if field.MaxLength != 256 {
		t.Errorf("MaxLength = %d, 期望 %d", field.MaxLength, 256)
	}
}

// TestFieldFromDict_dtype兼容 验证 "dtype" 键也能解析（Python 兼容）
func TestFieldFromDict_dtype兼容(t *testing.T) {
	data := map[string]any{
		"name":  "text",
		"dtype": "VARCHAR",
	}
	field, err := FieldFromDict(data)
	if err != nil {
		t.Fatalf("FieldFromDict() 返回错误: %v", err)
	}
	if field.DType != VectorDataTypeVarchar {
		t.Errorf("DType = %v, 期望 %v", field.DType, VectorDataTypeVarchar)
	}
}

// TestFieldFromDict_缺少名字 验证缺少 name 时返回错误
func TestFieldFromDict_缺少名字(t *testing.T) {
	data := map[string]any{
		"type": "VARCHAR",
	}
	_, err := FieldFromDict(data)
	if err == nil {
		t.Error("缺少 name 应返回错误")
	}
}

// TestFieldSchema_序列化往返 验证 ToDict → FieldFromDict 往返一致性
func TestFieldSchema_序列化往返(t *testing.T) {
	original, _ := NewFieldSchema("embedding", VectorDataTypeFloatVector,
		WithDim(768), WithFieldDescription("测试"))
	dict := original.ToDict()
	restored, err := FieldFromDict(dict)
	if err != nil {
		t.Fatalf("FieldFromDict() 返回错误: %v", err)
	}
	if restored.Name != original.Name {
		t.Errorf("Name 往返不一致: %q vs %q", restored.Name, original.Name)
	}
	if restored.DType != original.DType {
		t.Errorf("DType 往返不一致: %v vs %v", restored.DType, original.DType)
	}
	if restored.Dim != original.Dim {
		t.Errorf("Dim 往返不一致: %d vs %d", restored.Dim, original.Dim)
	}
	if restored.Description != original.Description {
		t.Errorf("Description 往返不一致: %q vs %q", restored.Description, original.Description)
	}
}
```

- [ ] **Step 2: 运行测试验证失败**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/store/vector/ -run "TestFieldSchema_ToDict|TestFieldFromDict|TestFieldSchema_序列化往返" -v`
Expected: 编译失败（ToDict / FieldFromDict 未定义）

- [ ] **Step 3: 实现 ToDict 和 FieldFromDict**

在 `base.go` 的 `// ──────────────────────────── 导出函数 ────────────────────────────` 区块中 `NewFieldSchema` 之后追加：

```go

// ToDict 将字段 Schema 转为字典格式。
//
// 只包含非零值字段。MaxLength 未显式设置时输出默认值 65535（对齐 Python 序列化行为）。
// 字段 type 使用 Python 兼容的字符串值（如 "VARCHAR"、"FLOAT_VECTOR"）。
//
// 对应 Python: FieldSchema.to_dict()
func (f *FieldSchema) ToDict() map[string]any {
	result := map[string]any{
		"name": f.Name,
		"type": f.DType.String(),
	}
	if f.IsPrimary {
		result["is_primary"] = true
	}
	if f.AutoID {
		result["auto_id"] = true
	}
	// MaxLength: 0 表示未设置，输出默认值 65535（对齐 Python max_length 默认值）
	if f.DType == VectorDataTypeVarchar {
		ml := f.MaxLength
		if ml == 0 {
			ml = defaultMaxLength
		}
		result["max_length"] = ml
	} else if f.MaxLength > 0 {
		result["max_length"] = f.MaxLength
	}
	if f.Dim > 0 {
		result["dim"] = f.Dim
	}
	if f.ElementType != VectorDataTypeVarchar || f.ElementType != 0 {
		// ElementType 为 0 (Varchar) 是零值，只有非零值才序列化
		if int(f.ElementType) != 0 {
			result["element_type"] = f.ElementType.String()
		}
	}
	if f.MaxCapacity > 0 {
		result["max_capacity"] = f.MaxCapacity
	}
	if f.Description != "" {
		result["description"] = f.Description
	}
	if f.DefaultValue != nil {
		result["default_value"] = f.DefaultValue
	}
	return result
}

// FieldFromDict 从字典创建 FieldSchema。
//
// 字典中字段类型键名支持 "type" 或 "dtype"（兼容 Python 两种写法）。
// 枚举值不区分大小写，如 "varchar" 和 "VARCHAR" 均可。
//
// 对应 Python: FieldSchema.from_dict()
func FieldFromDict(data map[string]any) (*FieldSchema, error) {
	name, _ := data["name"].(string)
	if name == "" {
		return nil, exception.BuildError(exception.StatusStoreVectorSchemaInvalid,
			exception.WithErrorMessage("field name is missing in dict"),
		)
	}
	// 兼容 "type" 和 "dtype" 两种键名
	dtypeStr := ""
	if v, ok := data["type"]; ok {
		dtypeStr, _ = v.(string)
	} else if v, ok := data["dtype"]; ok {
		dtypeStr, _ = v.(string)
	}
	dtype := vectorDataTypeFromString(dtypeStr)

	opts := make([]FieldOption, 0)
	if v, ok := data["is_primary"].(bool); ok && v {
		opts = append(opts, WithPrimary())
	}
	if v, ok := data["auto_id"].(bool); ok && v {
		opts = append(opts, WithAutoID())
	}
	if v, ok := data["max_length"]; ok {
		switch n := v.(type) {
		case int:
			opts = append(opts, WithMaxLength(n))
		case float64:
			opts = append(opts, WithMaxLength(int(n)))
		}
	}
	if v, ok := data["dim"]; ok {
		switch n := v.(type) {
		case int:
			opts = append(opts, WithDim(n))
		case float64:
			opts = append(opts, WithDim(int(n)))
		}
	}
	if v, ok := data["element_type"]; ok {
		if s, ok := v.(string); ok {
			opts = append(opts, WithElementType(vectorDataTypeFromString(s)))
		}
	}
	if v, ok := data["max_capacity"]; ok {
		switch n := v.(type) {
		case int:
			opts = append(opts, WithMaxCapacity(n))
		case float64:
			opts = append(opts, WithMaxCapacity(int(n)))
		}
	}
	if v, ok := data["description"].(string); ok && v != "" {
		opts = append(opts, WithFieldDescription(v))
	}
	if v, ok := data["default_value"]; ok {
		opts = append(opts, WithDefaultValue(v))
	}

	return NewFieldSchema(name, dtype, opts...)
}
```

在 `// ──────────────────────────── 非导出函数 ────────────────────────────` 区块中追加：

```go

// vectorDataTypeFromString 从字符串解析 VectorDataType，不区分大小写。
func vectorDataTypeFromString(s string) VectorDataType {
	upper := strings.ToUpper(s)
	for i, v := range vectorDataTypeStrings {
		if v == upper {
			return VectorDataType(i)
		}
	}
	return VectorDataTypeVarchar // 默认值，对齐 Python from_dict 的 fallback
}
```

同时在 import 中添加 `"strings"`。

- [ ] **Step 4: 运行测试验证通过**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/store/vector/ -run "TestFieldSchema_ToDict|TestFieldFromDict|TestFieldSchema_序列化往返" -v`
Expected: PASS

- [ ] **Step 5: 提交**

```bash
cd /home/opensource/uap-claw-go && git add internal/agentcore/store/vector/base.go internal/agentcore/store/vector/base_test.go && git commit -m "feat(store/vector): 实现 FieldSchema.ToDict / FieldFromDict 序列化"
```

---

### Task 4: 实现 CollectionSchema + CollectionOption

**Files:**
- Modify: `internal/agentcore/store/vector/base.go`
- Modify: `internal/agentcore/store/vector/base_test.go`

- [ ] **Step 1: 写 CollectionSchema 的失败测试**

在 `base_test.go` 中追加：

```go
// TestNewCollectionSchema_空创建 验证空 Schema 创建成功
func TestNewCollectionSchema_空创建(t *testing.T) {
	schema, err := NewCollectionSchema()
	if err != nil {
		t.Fatalf("NewCollectionSchema() 返回错误: %v", err)
	}
	if len(schema.fields) != 0 {
		t.Errorf("fields 长度 = %d, 期望 0", len(schema.fields))
	}
}

// TestNewCollectionSchema_带选项 验证 CollectionOption 生效
func TestNewCollectionSchema_带选项(t *testing.T) {
	schema, err := NewCollectionSchema(
		WithCollectionDescription("测试集合"),
		WithEnableDynamicField(),
	)
	if err != nil {
		t.Fatalf("NewCollectionSchema() 返回错误: %v", err)
	}
	if schema.Description != "测试集合" {
		t.Errorf("Description = %q, 期望 %q", schema.Description, "测试集合")
	}
	if !schema.EnableDynamicField {
		t.Error("EnableDynamicField 应为 true")
	}
}

// TestCollectionSchema_AddField_正常添加 验证字段添加成功
func TestCollectionSchema_AddField_正常添加(t *testing.T) {
	schema, _ := NewCollectionSchema()
	idField, _ := NewFieldSchema("id", VectorDataTypeVarchar, WithPrimary())
	_, err := schema.AddField(idField)
	if err != nil {
		t.Fatalf("AddField() 返回错误: %v", err)
	}
	if len(schema.fields) != 1 {
		t.Errorf("fields 长度 = %d, 期望 1", len(schema.fields))
	}
}

// TestCollectionSchema_AddField_链式调用 验证 AddField 返回自身支持链式调用
func TestCollectionSchema_AddField_链式调用(t *testing.T) {
	schema, _ := NewCollectionSchema()
	idField, _ := NewFieldSchema("id", VectorDataTypeVarchar, WithPrimary(), WithMaxLength(256))
	embedField, _ := NewFieldSchema("embedding", VectorDataTypeFloatVector, WithDim(768))
	_, err := schema.AddField(idField).AddField(embedField)
	if err != nil {
		t.Fatalf("链式 AddField() 返回错误: %v", err)
	}
	if len(schema.fields) != 2 {
		t.Errorf("fields 长度 = %d, 期望 2", len(schema.fields))
	}
}

// TestCollectionSchema_AddField_重复名字 验证重复字段名返回错误
func TestCollectionSchema_AddField_重复名字(t *testing.T) {
	schema, _ := NewCollectionSchema()
	field1, _ := NewFieldSchema("id", VectorDataTypeVarchar, WithPrimary())
	field2, _ := NewFieldSchema("id", VectorDataTypeVarchar)
	schema.AddField(field1)
	_, err := schema.AddField(field2)
	if err == nil {
		t.Error("重复字段名应返回错误")
	}
}

// TestCollectionSchema_AddField_重复主键 验证添加第二个主键返回错误
func TestCollectionSchema_AddField_重复主键(t *testing.T) {
	schema, _ := NewCollectionSchema()
	pk1, _ := NewFieldSchema("id", VectorDataTypeVarchar, WithPrimary())
	pk2, _ := NewFieldSchema("id2", VectorDataTypeVarchar, WithPrimary())
	schema.AddField(pk1)
	_, err := schema.AddField(pk2)
	if err == nil {
		t.Error("第二个主键应返回错误")
	}
}

// TestCollectionSchema_RemoveField 验证字段移除
func TestCollectionSchema_RemoveField(t *testing.T) {
	schema, _ := NewCollectionSchema()
	field, _ := NewFieldSchema("id", VectorDataTypeVarchar, WithPrimary())
	schema.AddField(field)
	schema.RemoveField("id")
	if schema.HasField("id") {
		t.Error("移除后 HasField 应返回 false")
	}
}

// TestCollectionSchema_GetField 验证字段获取
func TestCollectionSchema_GetField(t *testing.T) {
	schema, _ := NewCollectionSchema()
	field, _ := NewFieldSchema("embedding", VectorDataTypeFloatVector, WithDim(768))
	schema.AddField(field)
	got := schema.GetField("embedding")
	if got == nil {
		t.Fatal("GetField 返回 nil")
	}
	if got.Name != "embedding" {
		t.Errorf("Name = %q, 期望 %q", got.Name, "embedding")
	}
}

// TestCollectionSchema_GetField_不存在 验证不存在字段返回 nil
func TestCollectionSchema_GetField_不存在(t *testing.T) {
	schema, _ := NewCollectionSchema()
	if got := schema.GetField("no_such"); got != nil {
		t.Error("不存在字段应返回 nil")
	}
}

// TestCollectionSchema_GetPrimaryKeyField 验证获取主键
func TestCollectionSchema_GetPrimaryKeyField(t *testing.T) {
	schema, _ := NewCollectionSchema()
	field, _ := NewFieldSchema("id", VectorDataTypeVarchar, WithPrimary())
	schema.AddField(field)
	pk := schema.GetPrimaryKeyField()
	if pk == nil {
		t.Fatal("主键字段不应为 nil")
	}
	if pk.Name != "id" {
		t.Errorf("主键 Name = %q, 期望 %q", pk.Name, "id")
	}
}

// TestCollectionSchema_GetPrimaryKeyField_无主键 验证无主键时返回 nil
func TestCollectionSchema_GetPrimaryKeyField_无主键(t *testing.T) {
	schema, _ := NewCollectionSchema()
	if pk := schema.GetPrimaryKeyField(); pk != nil {
		t.Error("无主键时应返回 nil")
	}
}

// TestCollectionSchema_GetVectorFields 验证获取向量字段
func TestCollectionSchema_GetVectorFields(t *testing.T) {
	schema, _ := NewCollectionSchema()
	idField, _ := NewFieldSchema("id", VectorDataTypeVarchar, WithPrimary())
	embedField, _ := NewFieldSchema("embedding", VectorDataTypeFloatVector, WithDim(768))
	schema.AddField(idField).AddField(embedField)
	vf := schema.GetVectorFields()
	if len(vf) != 1 {
		t.Fatalf("向量字段数量 = %d, 期望 1", len(vf))
	}
	if vf[0].Name != "embedding" {
		t.Errorf("向量字段 Name = %q, 期望 %q", vf[0].Name, "embedding")
	}
}

// TestCollectionSchema_初始带主键冲突 验证 NewCollectionSchema 传入多个主键字段返回错误
func TestCollectionSchema_初始带主键冲突(t *testing.T) {
	pk1, _ := NewFieldSchema("id1", VectorDataTypeVarchar, WithPrimary())
	pk2, _ := NewFieldSchema("id2", VectorDataTypeVarchar, WithPrimary())
	_, err := NewCollectionSchemaFromFields([]*FieldSchema{pk1, pk2})
	if err == nil {
		t.Error("多个主键字段应返回错误")
	}
}
```

- [ ] **Step 2: 运行测试验证失败**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/store/vector/ -run "TestNewCollectionSchema|TestCollectionSchema" -v`
Expected: 编译失败（CollectionSchema 未定义）

- [ ] **Step 3: 实现 CollectionSchema**

在 `base.go` 的 `// ──────────────────────────── 结构体 ────────────────────────────` 区块中 FieldSchema 之后追加：

```go

// CollectionSchema 向量集合的 Schema 定义。
//
// 类似 Milvus CollectionSchema，支持动态字段。
// fields 为未导出切片，通过方法访问和修改，保证校验逻辑不被绕过。
//
// 对应 Python: openjiuwen/core/foundation/store/base_vector_store.py (CollectionSchema)
type CollectionSchema struct {
	// fields 字段定义列表（未导出，通过方法访问）
	fields []*FieldSchema
	// Description 集合描述
	Description string
	// EnableDynamicField 是否启用动态字段
	EnableDynamicField bool
}
```

在 `// ──────────────────────────── 导出函数 ────────────────────────────` 区块中追加：

```go

// NewCollectionSchema 创建并校验 CollectionSchema。
//
// 校验规则：最多只能有一个主键字段。
//
// 对应 Python: CollectionSchema(fields=[], ...)
func NewCollectionSchema(opts ...CollectionOption) (*CollectionSchema, error) {
	s := &CollectionSchema{}
	for _, opt := range opts {
		opt(s)
	}
	return s, nil
}

// NewCollectionSchemaFromFields 从字段列表创建 Schema。
//
// 逐个添加字段并校验，任一字段冲突则返回错误。
//
// 对应 Python: CollectionSchema.from_fields(fields, **kwargs)
func NewCollectionSchemaFromFields(fields []*FieldSchema, opts ...CollectionOption) (*CollectionSchema, error) {
	schema, err := NewCollectionSchema(opts...)
	if err != nil {
		return nil, err
	}
	for _, f := range fields {
		if _, err := schema.AddField(f); err != nil {
			return nil, err
		}
	}
	return schema, nil
}

// AddField 添加字段到 Schema（原地修改，返回自身以支持链式调用）。
//
// 校验规则：
//   - 字段名不能重复
//   - 不能添加第二个主键字段
//
// 对应 Python: CollectionSchema.add_field(field)
func (s *CollectionSchema) AddField(field *FieldSchema) (*CollectionSchema, error) {
	// 检查重名
	for _, f := range s.fields {
		if f.Name == field.Name {
			return nil, exception.BuildError(exception.StatusStoreVectorSchemaInvalid,
				exception.WithErrorMessage(fmt.Sprintf("field name already exists, field=%s", field.Name)),
			)
		}
	}
	// 检查主键冲突
	if field.IsPrimary {
		for _, f := range s.fields {
			if f.IsPrimary {
				return nil, exception.BuildError(exception.StatusStoreVectorSchemaInvalid,
					exception.WithErrorMessage(fmt.Sprintf(
						"collection can have at most one primary key field, primary_field=%s, field=%s",
						f.Name, field.Name)),
				)
			}
		}
	}
	s.fields = append(s.fields, field)
	return s, nil
}

// RemoveField 按名称移除字段（原地修改，返回自身以支持链式调用）。
//
// 对应 Python: CollectionSchema.remove_field(field_name)
func (s *CollectionSchema) RemoveField(fieldName string) *CollectionSchema {
	filtered := make([]*FieldSchema, 0, len(s.fields))
	for _, f := range s.fields {
		if f.Name != fieldName {
			filtered = append(filtered, f)
		}
	}
	s.fields = filtered
	return s
}

// GetField 按名称获取字段，不存在返回 nil。
//
// 对应 Python: CollectionSchema.get_field(field_name)
func (s *CollectionSchema) GetField(fieldName string) *FieldSchema {
	for _, f := range s.fields {
		if f.Name == fieldName {
			return f
		}
	}
	return nil
}

// HasField 检查字段是否存在。
//
// 对应 Python: CollectionSchema.has_field(field_name)
func (s *CollectionSchema) HasField(fieldName string) bool {
	return s.GetField(fieldName) != nil
}

// GetPrimaryKeyField 获取主键字段，不存在返回 nil。
//
// 对应 Python: CollectionSchema.get_primary_key_field()
func (s *CollectionSchema) GetPrimaryKeyField() *FieldSchema {
	for _, f := range s.fields {
		if f.IsPrimary {
			return f
		}
	}
	return nil
}

// GetVectorFields 获取所有 FLOAT_VECTOR 类型的字段。
//
// 对应 Python: CollectionSchema.get_vector_fields()
func (s *CollectionSchema) GetVectorFields() []*FieldSchema {
	var result []*FieldSchema
	for _, f := range s.fields {
		if f.DType == VectorDataTypeFloatVector {
			result = append(result, f)
		}
	}
	return result
}

// Fields 返回字段列表的副本（防止外部直接修改内部切片）。
func (s *CollectionSchema) Fields() []*FieldSchema {
	result := make([]*FieldSchema, len(s.fields))
	copy(result, s.fields)
	return result
}
```

- [ ] **Step 4: 运行测试验证通过**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/store/vector/ -run "TestNewCollectionSchema|TestCollectionSchema" -v`
Expected: PASS

- [ ] **Step 5: 提交**

```bash
cd /home/opensource/uap-claw-go && git add internal/agentcore/store/vector/base.go internal/agentcore/store/vector/base_test.go && git commit -m "feat(store/vector): 实现 CollectionSchema + CollectionOption"
```

---

### Task 5: 实现 CollectionSchema 序列化 + VectorSearchResult + BaseVectorStore 接口

**Files:**
- Modify: `internal/agentcore/store/vector/base.go`
- Modify: `internal/agentcore/store/vector/base_test.go`

- [ ] **Step 1: 写 CollectionSchema 序列化和 BaseVectorStore 接口的失败测试**

在 `base_test.go` 中追加：

```go
// TestCollectionSchema_ToDict 验证序列化输出
func TestCollectionSchema_ToDict(t *testing.T) {
	schema, _ := NewCollectionSchema(
		WithCollectionDescription("测试"),
		WithEnableDynamicField(),
	)
	idField, _ := NewFieldSchema("id", VectorDataTypeVarchar, WithPrimary(), WithMaxLength(256))
	embedField, _ := NewFieldSchema("embedding", VectorDataTypeFloatVector, WithDim(768))
	schema.AddField(idField).AddField(embedField)

	got := schema.ToDict()
	if got["description"] != "测试" {
		t.Errorf("description = %v, 期望 %q", got["description"], "测试")
	}
	if got["enable_dynamic_field"] != true {
		t.Error("enable_dynamic_field 应为 true")
	}
	// ToDict 返回 []map[string]any，存入 map[string]any 后运行时类型不变
	fieldsRaw, ok := got["fields"].([]map[string]any)
	if !ok {
		t.Fatalf("fields 类型不正确: %T", got["fields"])
	}
	if len(fieldsRaw) != 2 {
		t.Errorf("fields 长度 = %d, 期望 2", len(fieldsRaw))
	}
}

// TestCollectionFromDict_基本反序列化 验证从字典创建 CollectionSchema
func TestCollectionFromDict_基本反序列化(t *testing.T) {
	data := map[string]any{
		"fields": []map[string]any{
			{"name": "id", "type": "VARCHAR", "is_primary": true, "max_length": 256},
			{"name": "embedding", "type": "FLOAT_VECTOR", "dim": 768},
		},
		"description":           "测试集合",
		"enable_dynamic_field":  true,
	}
	schema, err := CollectionFromDict(data)
	if err != nil {
		t.Fatalf("CollectionFromDict() 返回错误: %v", err)
	}
	if schema.Description != "测试集合" {
		t.Errorf("Description = %q, 期望 %q", schema.Description, "测试集合")
	}
	if !schema.EnableDynamicField {
		t.Error("EnableDynamicField 应为 true")
	}
	if len(schema.fields) != 2 {
		t.Errorf("fields 长度 = %d, 期望 2", len(schema.fields))
	}
	pk := schema.GetPrimaryKeyField()
	if pk == nil || pk.Name != "id" {
		t.Error("主键字段应为 id")
	}
}

// TestCollectionSchema_序列化往返 验证 ToDict → CollectionFromDict 往返一致性
func TestCollectionSchema_序列化往返(t *testing.T) {
	original, _ := NewCollectionSchema(
		WithCollectionDescription("往返测试"),
		WithEnableDynamicField(),
	)
	idField, _ := NewFieldSchema("id", VectorDataTypeVarchar, WithPrimary(), WithMaxLength(256))
	embedField, _ := NewFieldSchema("embedding", VectorDataTypeFloatVector, WithDim(768))
	original.AddField(idField).AddField(embedField)

	dict := original.ToDict()
	restored, err := CollectionFromDict(dict)
	if err != nil {
		t.Fatalf("CollectionFromDict() 返回错误: %v", err)
	}
	if restored.Description != original.Description {
		t.Errorf("Description 往返不一致: %q vs %q", restored.Description, original.Description)
	}
	if restored.EnableDynamicField != original.EnableDynamicField {
		t.Errorf("EnableDynamicField 往返不一致: %v vs %v", restored.EnableDynamicField, original.EnableDynamicField)
	}
	if len(restored.fields) != len(original.fields) {
		t.Errorf("fields 长度往返不一致: %d vs %d", len(restored.fields), len(original.fields))
	}
}

// TestVectorSearchResult_字段 验证搜索结果字段赋值
func TestVectorSearchResult_字段(t *testing.T) {
	result := VectorSearchResult{
		Score: 0.95,
		Fields: map[string]any{
			"id":   "doc_1",
			"text": "测试文档",
		},
	}
	if result.Score != 0.95 {
		t.Errorf("Score = %f, 期望 0.95", result.Score)
	}
	if result.Fields["id"] != "doc_1" {
		t.Errorf("Fields[id] = %v, 期望 %q", result.Fields["id"], "doc_1")
	}
}

// fakeVectorStore 用于验证 BaseVectorStore 接口可被实现
type fakeVectorStore struct{}

func (f *fakeVectorStore) CreateCollection(_ context.Context, _ string, _ *CollectionSchema, _ ...Option) error {
	return nil
}
func (f *fakeVectorStore) DeleteCollection(_ context.Context, _ string, _ ...Option) error {
	return nil
}
func (f *fakeVectorStore) CollectionExists(_ context.Context, _ string, _ ...Option) (bool, error) {
	return false, nil
}
func (f *fakeVectorStore) GetSchema(_ context.Context, _ string, _ ...Option) (*CollectionSchema, error) {
	return nil, nil
}
func (f *fakeVectorStore) AddDocs(_ context.Context, _ string, _ []map[string]any, _ ...Option) error {
	return nil
}
func (f *fakeVectorStore) Search(_ context.Context, _ string, _ []float64, _ string, _ int, _ map[string]any, _ ...Option) ([]VectorSearchResult, error) {
	return nil, nil
}
func (f *fakeVectorStore) DeleteDocsByIDs(_ context.Context, _ string, _ []string, _ ...Option) error {
	return nil
}
func (f *fakeVectorStore) DeleteDocsByFilters(_ context.Context, _ string, _ map[string]any, _ ...Option) error {
	return nil
}
func (f *fakeVectorStore) ListCollectionNames(_ context.Context) ([]string, error) {
	return nil, nil
}

// TestBaseVectorStore_接口满足 验证 fakeVectorStore 满足 BaseVectorStore 接口
func TestBaseVectorStore_接口满足(t *testing.T) {
	var _ BaseVectorStore = (*fakeVectorStore)(nil)
}
```

- [ ] **Step 2: 运行测试验证失败**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/store/vector/ -run "TestCollectionSchema_ToDict|TestCollectionFromDict|TestCollectionSchema_序列化往返|TestVectorSearchResult|TestBaseVectorStore" -v`
Expected: 编译失败（ToDict / CollectionFromDict / VectorSearchResult / BaseVectorStore 未定义）

- [ ] **Step 3: 实现 CollectionSchema 序列化 + VectorSearchResult + BaseVectorStore**

在 `base.go` 的结构体区块中 CollectionSchema 之后追加：

```go

// VectorSearchResult 向量搜索结果。
//
// 对应 Python: openjiuwen/core/foundation/store/base_vector_store.py (VectorSearchResult)
type VectorSearchResult struct {
	// Score 相关度分数（越高越相关）
	Score float64
	// Fields 匹配文档的所有字段值（包括 id, text, metadata 等）
	Fields map[string]any
}

// BaseVectorStore 向量存储后端的抽象接口。
//
// 所有向量存储后端（Chroma、Milvus、Gauss 等）必须实现此接口。
// 方法全部为同步风格，调用者可按需通过 goroutine 实现并发。
//
// 对应 Python: openjiuwen/core/foundation/store/base_vector_store.py (BaseVectorStore)
type BaseVectorStore interface {
	// CreateCollection 创建集合，schema 定义字段结构。
	//
	// 对应 Python: BaseVectorStore.create_collection(collection_name, schema, **kwargs)
	CreateCollection(ctx context.Context, collectionName string, schema *CollectionSchema, opts ...Option) error

	// DeleteCollection 删除集合。
	//
	// 对应 Python: BaseVectorStore.delete_collection(collection_name, **kwargs)
	DeleteCollection(ctx context.Context, collectionName string, opts ...Option) error

	// CollectionExists 检查集合是否存在。
	//
	// 对应 Python: BaseVectorStore.collection_exists(collection_name, **kwargs)
	CollectionExists(ctx context.Context, collectionName string, opts ...Option) (bool, error)

	// GetSchema 获取集合的 Schema。
	//
	// 对应 Python: BaseVectorStore.get_schema(collection_name, **kwargs)
	GetSchema(ctx context.Context, collectionName string, opts ...Option) (*CollectionSchema, error)

	// AddDocs 添加文档到集合。
	// 每个文档是包含 id/embedding/text/metadata 等字段的 map。
	//
	// 对应 Python: BaseVectorStore.add_docs(collection_name, docs, **kwargs)
	AddDocs(ctx context.Context, collectionName string, docs []map[string]any, opts ...Option) error

	// Search 向量相似度搜索。
	// queryVector: 查询向量
	// vectorField: 搜索的向量字段名（如 "embedding"）
	// topK: 返回结果数量，0 使用默认值 5
	// filters: 标量字段过滤条件，nil 表示无过滤
	//
	// 对应 Python: BaseVectorStore.search(collection_name, query_vector, vector_field, top_k=5, filters=None, **kwargs)
	Search(ctx context.Context, collectionName string, queryVector []float64, vectorField string, topK int, filters map[string]any, opts ...Option) ([]VectorSearchResult, error)

	// DeleteDocsByIDs 按 ID 删除文档。
	//
	// 对应 Python: BaseVectorStore.delete_docs_by_ids(collection_name, ids, **kwargs)
	DeleteDocsByIDs(ctx context.Context, collectionName string, ids []string, opts ...Option) error

	// DeleteDocsByFilters 按标量字段过滤条件删除文档。
	//
	// 对应 Python: BaseVectorStore.delete_docs_by_filters(collection_name, filters, **kwargs)
	DeleteDocsByFilters(ctx context.Context, collectionName string, filters map[string]any, opts ...Option) error

	// ListCollectionNames 列出所有集合名称。
	//
	// 对应 Python: BaseVectorStore.list_collection_names()
	ListCollectionNames(ctx context.Context) ([]string, error)
}
```

在导出函数区块中 CollectionSchema 的 Fields() 之后追加：

```go

// ToDict 将 Schema 转为字典格式（序列化用）。
//
// 对应 Python: CollectionSchema.to_dict()
func (s *CollectionSchema) ToDict() map[string]any {
	fields := make([]map[string]any, len(s.fields))
	for i, f := range s.fields {
		fields[i] = f.ToDict()
	}
	return map[string]any{
		"fields":               fields,
		"description":          s.Description,
		"enable_dynamic_field": s.EnableDynamicField,
	}
}

// CollectionFromDict 从字典创建 CollectionSchema。
//
// 对应 Python: CollectionSchema.from_dict(data)
func CollectionFromDict(data map[string]any) (*CollectionSchema, error) {
	opts := make([]CollectionOption, 0)
	if v, ok := data["description"].(string); ok {
		opts = append(opts, WithCollectionDescription(v))
	}
	if v, ok := data["enable_dynamic_field"].(bool); ok && v {
		opts = append(opts, WithEnableDynamicField())
	}

	schema, err := NewCollectionSchema(opts...)
	if err != nil {
		return nil, err
	}

	fieldsRaw, ok := data["fields"]
	if !ok {
		return schema, nil
	}

	// 支持两种格式：[]map[string]any 或 []any（每个元素为 map[string]any）
	switch fields := fieldsRaw.(type) {
	case []map[string]any:
		for _, fd := range fields {
			f, err := FieldFromDict(fd)
			if err != nil {
				return nil, err
			}
			if _, err := schema.AddField(f); err != nil {
				return nil, err
			}
		}
	case []any:
		for _, item := range fields {
			fd, ok := item.(map[string]any)
			if !ok {
				continue
			}
			f, err := FieldFromDict(fd)
			if err != nil {
				return nil, err
			}
			if _, err := schema.AddField(f); err != nil {
				return nil, err
			}
		}
	}

	return schema, nil
}
```

- [ ] **Step 4: 运行测试验证通过**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/store/vector/ -v`
Expected: ALL PASS

- [ ] **Step 5: 提交**

```bash
cd /home/opensource/uap-claw-go && git add internal/agentcore/store/vector/base.go internal/agentcore/store/vector/base_test.go && git commit -m "feat(store/vector): 实现 CollectionSchema 序列化 + VectorSearchResult + BaseVectorStore 接口"
```

---

### Task 6: 补充边界测试 + Options 测试 + 覆盖率检查

**Files:**
- Modify: `internal/agentcore/store/vector/base_test.go`

- [ ] **Step 1: 写 Options 和边界条件测试**

在 `base_test.go` 中追加：

```go
// TestOptions_全部选项 验证 Options 函数选项全部生效
func TestOptions_全部选项(t *testing.T) {
	o := newOptions(
		WithDistanceMetric("COSINE"),
		WithBatchSize(100),
		WithMetricType("L2"),
		WithOutputFields("id", "text"),
	)
	if o.DistanceMetric != "COSINE" {
		t.Errorf("DistanceMetric = %q, 期望 %q", o.DistanceMetric, "COSINE")
	}
	if o.BatchSize != 100 {
		t.Errorf("BatchSize = %d, 期望 %d", o.BatchSize, 100)
	}
	if o.MetricType != "L2" {
		t.Errorf("MetricType = %q, 期望 %q", o.MetricType, "L2")
	}
	if len(o.OutputFields) != 2 {
		t.Errorf("OutputFields 长度 = %d, 期望 2", len(o.OutputFields))
	}
}

// TestOptions_空选项 验证零值 Options
func TestOptions_空选项(t *testing.T) {
	o := newOptions()
	if o.DistanceMetric != "" {
		t.Errorf("DistanceMetric = %q, 期望空字符串", o.DistanceMetric)
	}
	if o.BatchSize != 0 {
		t.Errorf("BatchSize = %d, 期望 0", o.BatchSize)
	}
	if o.MetricType != "" {
		t.Errorf("MetricType = %q, 期望空字符串", o.MetricType)
	}
	if o.OutputFields != nil {
		t.Errorf("OutputFields = %v, 期望 nil", o.OutputFields)
	}
}

// TestVectorDataType_所有枚举值 验证所有枚举值在有效范围内
func TestVectorDataType_所有枚举值(t *testing.T) {
	for i := VectorDataTypeVarchar; i <= VectorDataTypeArray; i++ {
		s := i.String()
		if s == "" || s == fmt.Sprintf("UNKNOWN(%d)", i) {
			t.Errorf("VectorDataType(%d) 缺少字符串表示", i)
		}
	}
}

// TestNewFieldSchema_ArrayWithElementType 验证 ARRAY 类型带元素类型
func TestNewFieldSchema_ArrayWithElementType(t *testing.T) {
	field, err := NewFieldSchema("tags", VectorDataTypeArray,
		WithElementType(VectorDataTypeVarchar),
		WithMaxCapacity(10),
	)
	if err != nil {
		t.Fatalf("NewFieldSchema() 返回错误: %v", err)
	}
	if field.ElementType != VectorDataTypeVarchar {
		t.Errorf("ElementType = %v, 期望 %v", field.ElementType, VectorDataTypeVarchar)
	}
	if field.MaxCapacity != 10 {
		t.Errorf("MaxCapacity = %d, 期望 %d", field.MaxCapacity, 10)
	}
}

// TestFieldSchema_ToDict_Array字段 验证 ARRAY 字段序列化包含 element_type 和 max_capacity
func TestFieldSchema_ToDict_Array字段(t *testing.T) {
	field, _ := NewFieldSchema("tags", VectorDataTypeArray,
		WithElementType(VectorDataTypeInt64),
		WithMaxCapacity(20),
	)
	got := field.ToDict()
	if got["element_type"] != "INT64" {
		t.Errorf("element_type = %v, 期望 %q", got["element_type"], "INT64")
	}
	if got["max_capacity"] != 20 {
		t.Errorf("max_capacity = %v, 期望 %d", got["max_capacity"], 20)
	}
}

// TestFieldFromDict_Array字段 验证 ARRAY 字段反序列化
func TestFieldFromDict_Array字段(t *testing.T) {
	data := map[string]any{
		"name":          "tags",
		"type":          "ARRAY",
		"element_type":  "INT64",
		"max_capacity":  20,
	}
	field, err := FieldFromDict(data)
	if err != nil {
		t.Fatalf("FieldFromDict() 返回错误: %v", err)
	}
	if field.DType != VectorDataTypeArray {
		t.Errorf("DType = %v, 期望 %v", field.DType, VectorDataTypeArray)
	}
	if field.ElementType != VectorDataTypeInt64 {
		t.Errorf("ElementType = %v, 期望 %v", field.ElementType, VectorDataTypeInt64)
	}
	if field.MaxCapacity != 20 {
		t.Errorf("MaxCapacity = %d, 期望 %d", field.MaxCapacity, 20)
	}
}

// TestFieldSchema_ToDict_DefaultValue 验证 DefaultValue 序列化
func TestFieldSchema_ToDict_DefaultValue(t *testing.T) {
	field, _ := NewFieldSchema("status", VectorDataTypeVarchar,
		WithDefaultValue("active"),
	)
	got := field.ToDict()
	if got["default_value"] != "active" {
		t.Errorf("default_value = %v, 期望 %q", got["default_value"], "active")
	}
}

// TestNewFieldSchema_NonFloatVectorDim 验证非 FLOAT_VECTOR 类型设置 dim 不报错
func TestNewFieldSchema_NonFloatVectorDim(t *testing.T) {
	field, err := NewFieldSchema("count", VectorDataTypeInt32, WithDim(100))
	if err != nil {
		t.Fatalf("非 FLOAT_VECTOR 带 dim 不应报错: %v", err)
	}
	if field.Dim != 100 {
		t.Errorf("Dim = %d, 期望 %d", field.Dim, 100)
	}
}

// TestCollectionSchema_RemoveField_链式 验证 RemoveField 支持链式调用
func TestCollectionSchema_RemoveField_链式(t *testing.T) {
	schema, _ := NewCollectionSchema()
	f1, _ := NewFieldSchema("a", VectorDataTypeVarchar)
	f2, _ := NewFieldSchema("b", VectorDataTypeVarchar)
	schema.AddField(f1).AddField(f2)
	result := schema.RemoveField("a")
	if result != schema {
		t.Error("RemoveField 应返回自身")
	}
	if len(schema.fields) != 1 {
		t.Errorf("fields 长度 = %d, 期望 1", len(schema.fields))
	}
}

// TestCollectionFromDict_空字典 验证空字典创建空 Schema
func TestCollectionFromDict_空字典(t *testing.T) {
	schema, err := CollectionFromDict(map[string]any{})
	if err != nil {
		t.Fatalf("CollectionFromDict() 返回错误: %v", err)
	}
	if len(schema.fields) != 0 {
		t.Errorf("fields 长度 = %d, 期望 0", len(schema.fields))
	}
}
```

需要在 base_test.go 的 import 中添加 `"fmt"`。

- [ ] **Step 2: 运行全部测试**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/store/vector/ -v`
Expected: ALL PASS

- [ ] **Step 3: 检查覆盖率**

Run: `cd /home/opensource/uap-claw-go && go test -cover ./internal/agentcore/store/vector/`
Expected: 覆盖率 ≥ 85%

- [ ] **Step 4: 提交**

```bash
cd /home/opensource/uap-claw-go && git add internal/agentcore/store/vector/base_test.go && git commit -m "test(store/vector): 补充边界测试 + Options 测试，覆盖率 ≥ 85%"
```

---

### Task 7: 更新 IMPLEMENTATION_PLAN.md 状态

**Files:**
- Modify: `IMPLEMENTATION_PLAN.md`

- [ ] **Step 1: 更新 4.6 步骤状态为 ✅**

找到 `4.6` 对应的行，将 `☐` 改为 `✅`。

- [ ] **Step 2: 提交**

```bash
cd /home/opensource/uap-claw-go && git add IMPLEMENTATION_PLAN.md && git commit -m "docs: 更新实现计划 4.6 状态为已完成"
```

---

## 自查清单

| 规格要求 | 对应任务 |
|---------|---------|
| VectorDataType 枚举 + String() | Task 2 |
| FieldSchema + NewFieldSchema() 校验 | Task 2 |
| FieldSchema.ToDict() / FieldFromDict() | Task 3 |
| CollectionSchema + NewCollectionSchema() 校验 | Task 4 |
| CollectionSchema.AddField/RemoveField/GetField 等方法 | Task 4 |
| CollectionSchema.ToDict() / CollectionFromDict() | Task 5 |
| NewCollectionSchemaFromFields | Task 4 |
| VectorSearchResult | Task 5 |
| Option / FieldOption / CollectionOption | Task 2 |
| BaseVectorStore 接口（9 方法） | Task 5 |
| doc.go 包文档 | Task 1 |
| 单元测试覆盖率 ≥ 85% | Task 6 |
| 迁移方法延后 | 未实现（按设计） |
| 所有注释使用中文 | 全部任务 |
| 声明排列顺序按规范 | 全部任务 |

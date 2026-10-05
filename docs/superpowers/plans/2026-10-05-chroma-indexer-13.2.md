# 13.2 ChromaIndexer + 前置依赖 实现计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 实现 ChromaIndexer（ChromaDB 文档索引管理器）及其全部前置依赖，对齐 Python `retrieval/indexing/indexer/chroma_indexer.py`

**Architecture:** 按 5 个子步骤递进实现：ChromaVectorField（HNSW 配置）→ DocIndexCallback（进度回调）→ ComputeChunkEmbeddings 签名回填 → Retrieval 层 ChromaVectorStore（含 SearchResult/RRFFusion）→ ChromaIndexer。每步完成后可独立测试。

**Tech Stack:** Go 1.22+，chroma-go v2 SDK，stretchr/testify

**Design Spec:** `docs/superpowers/specs/2026-10-05-chroma-indexer-13.2-design.md`

---

## Task 1: VectorField 基类 + ToDict 反射机制

**Files:**
- Create: `internal/agentcore/foundation/store/vector_fields/doc.go`
- Create: `internal/agentcore/foundation/store/vector_fields/base.go`
- Create: `internal/agentcore/foundation/store/vector_fields/base_test.go`

- [ ] **Step 1: 创建 doc.go 包文档**

```go
// Package vectorfields 提供向量字段索引配置类型。
//
// 定义跨数据库后端（Chroma/Milvus/PGVector）的向量字段索引统一接口，
// 支持构建/搜索阶段过滤（stage-based field filtering），
// 通过 struct tag 标记字段阶段，ToDict 按阶段返回配置字典。
//
// 对应 Python 代码：openjiuwen/core/foundation/store/vector_fields/
//
// 文件目录：
//
//	vectorfields/
//	├── doc.go           # 包文档
//	├── base.go          # VectorField 基类 + ToDict + ShouldKeep
//	└── chroma.go        # ChromaVectorField HNSW 配置
package vectorfields
```

- [ ] **Step 2: 写 VectorField ToDict 反射机制的失败测试**

在 `base_test.go` 中写测试，覆盖 `ShouldKeep` 和 `ToDict` 的核心逻辑：

```go
package vectorfields

import (
	"reflect"
	"testing"

	"github.com/stretchr/testify/assert"
)

// testStruct 用于测试 ToDict 的模拟结构体
type testStruct struct {
	Name     string         `json:"name" stage:"-"`
	Build    int            `json:"build" stage:"construct"`
	Search   int            `json:"search" stage:"search"`
	NoTag    string         `json:"no_tag"`
	Extra    map[string]any `json:"extra" stage:"search"`
}

func TestShouldKeep(t *testing.T) {
	ft := reflect.TypeOf(testStruct{})

	tests := []struct {
		fieldName string
		stage     string
		expected  bool
	}{
		{"Name", "construct", false},    // stage:"-" → 始终移除
		{"Name", "search", false},       // stage:"-" → 始终移除
		{"Build", "construct", true},    // stage:"construct" → construct 阶段保留
		{"Build", "search", false},      // stage:"construct" → search 阶段移除
		{"Search", "search", true},      // stage:"search" → search 阶段保留
		{"Search", "construct", false},  // stage:"search" → construct 阶段移除
		{"NoTag", "construct", true},    // 无 tag → 始终保留
		{"NoTag", "search", true},       // 无 tag → 始终保留
	}

	for _, tt := range tests {
		t.Run(tt.fieldName+"_"+tt.stage, func(t *testing.T) {
			field, ok := ft.FieldByName(tt.fieldName)
			assert.True(t, ok)
			result := ShouldKeep(field, tt.stage)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestToDict_Construct(t *testing.T) {
	v := testStruct{
		Name:   "test",
		Build:  16,
		Search: 100,
		NoTag:  "always",
		Extra:  map[string]any{"resize_factor": 1.5, "num_threads": 4},
	}
	result := ToDict(v, "construct")

	// stage:"-" 的 Name 应被移除
	assert.NotContains(t, result, "vector_field")
	assert.NotContains(t, result, "database_type")
	assert.NotContains(t, result, "index_type")
	assert.NotContains(t, result, "name")
	// stage:"construct" 的 Build 应保留
	assert.Equal(t, 16, result["build"])
	// stage:"search" 的 Search 应被移除
	assert.NotContains(t, result, "search")
	// 无 tag 的 NoTag 应保留
	assert.Equal(t, "always", result["no_tag"])
	// Extra 是 stage:"search"，construct 阶段不应展开
	assert.NotContains(t, result, "extra")
	assert.NotContains(t, result, "resize_factor")
}

func TestToDict_Search(t *testing.T) {
	v := testStruct{
		Name:   "test",
		Build:  16,
		Search: 100,
		NoTag:  "always",
		Extra:  map[string]any{"resize_factor": 1.5, "num_threads": 4},
	}
	result := ToDict(v, "search")

	// stage:"-" 的 Name 应被移除
	assert.NotContains(t, result, "name")
	// stage:"construct" 的 Build 应被移除
	assert.NotContains(t, result, "build")
	// stage:"search" 的 Search 应保留
	assert.Equal(t, 100, result["search"])
	// 无 tag 的 NoTag 应保留
	assert.Equal(t, "always", result["no_tag"])
	// Extra 应被展开（键值对合并），原 extra 键移除
	assert.NotContains(t, result, "extra")
	assert.Equal(t, 1.5, result["resize_factor"])
	assert.Equal(t, 4, result["num_threads"])
}
```

- [ ] **Step 3: 运行测试确认失败**

Run: `cd /home/opensource/uapclaw-gateway && go test ./internal/agentcore/foundation/store/vector_fields/... -v -run "TestShouldKeep|TestToDict"`
Expected: FAIL — 包不存在

- [ ] **Step 4: 实现 base.go**

```go
package vectorfields

import (
	"reflect"
	"strings"
)

// ──────────────────────────── 结构体 ────────────────────────────

// VectorField 向量字段索引配置基类。
//
// 提供跨数据库后端的向量字段索引统一接口，支持构建/搜索阶段过滤。
// 通过 struct tag `stage:"construct"` / `stage:"search"` / `stage:"-"` 标记字段阶段，
// ToDict(stage) 通过反射读取 tag 过滤字段，对齐 Python should_keep(finfo, stage)。
//
// Python: foundation/store/vector_fields/base.py (VectorField)
type VectorField struct {
	// VectorFieldName 向量字段名，默认 "embedding"
	// 对齐 Python vector_field
	VectorFieldName string `json:"vector_field" stage:"-"`
	// DatabaseType 数据库类型，"chroma" | "milvus" | "pg"
	// 对齐 Python database_type
	DatabaseType string `json:"database_type" stage:"-"`
	// IndexType 索引类型，"auto" | "hnsw" | "flat" | "ivf" | "scann"
	// 对齐 Python index_type
	IndexType string `json:"index_type" stage:"-"`
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// NewVectorField 创建默认 VectorField。
func NewVectorField(vectorFieldName, databaseType, indexType string) VectorField {
	return VectorField{
		VectorFieldName: vectorFieldName,
		DatabaseType:    databaseType,
		IndexType:       indexType,
	}
}

// ShouldKeep 判断字段是否应在指定阶段保留。
//
// 通过反射读取 struct tag `stage` 值判断：
//   - stage:"-" → 始终移除（内部字段：database_type/index_type/vector_field/variant）
//   - stage:"construct" → 仅 construct 阶段保留
//   - stage:"search" → 仅 search 阶段保留
//   - 无 tag → 始终保留
//
// 对齐 Python VectorField.should_keep(finfo, stage)。
func ShouldKeep(field reflect.StructField, stage string) bool {
	tag := field.Tag.Get("stage")
	if tag == "-" {
		return false
	}
	if tag == "" {
		return true
	}
	return tag == stage
}

// ToDict 按阶段过滤字段并返回字典。
//
// 通过反射遍历 struct 字段，读取 stage tag 过滤：
//   - stage:"construct" → 仅 construct 阶段返回
//   - stage:"search" → 仅 search 阶段返回
//   - stage:"-" → 始终移除（内部字段）
//   - 无 tag → 始终包含
//
// map[string]any 类型的字段（如 ExtraSearch/ExtraConstruct）被展开合并到返回字典中
// （键值对合并，原键移除），对齐 Python `model_content | extra`。
//
// 对齐 Python VectorField.to_dict(stage)。
func ToDict(v any, stage string) map[string]any {
	result := make(map[string]any)
	rv := reflect.ValueOf(v)
	if rv.Kind() == reflect.Pointer {
		rv = rv.Elem()
	}
	rt := rv.Type()

	for i := 0; i < rt.NumField(); i++ {
		field := rt.Field(i)
		if !ShouldKeep(field, stage) {
			continue
		}

		fieldValue := rv.Field(i)
		jsonKey := field.Tag.Get("json")
		if jsonKey == "" || jsonKey == "-" {
			continue
		}
		// 去除 json tag 中的 omitempty 等选项
		if idx := strings.Index(jsonKey, ","); idx != -1 {
			jsonKey = jsonKey[:idx]
		}

		// map[string]any 类型的字段：展开合并
		if fieldValue.Kind() == reflect.Map && fieldValue.Type().Key().Kind() == reflect.String {
			iter := fieldValue.MapRange()
			for iter.Next() {
				key := iter.Key().String()
				val := iter.Value().Interface()
				if val != nil {
					result[key] = val
				}
			}
			continue
		}

		// 零值跳过，对齐 Python `if val is not None`
		if fieldValue.IsZero() {
			continue
		}

		result[jsonKey] = fieldValue.Interface()
	}
	return result
}

// ──────────────────────────── 非导出函数 ────────────────────────────
```

- [ ] **Step 5: 运行测试确认通过**

Run: `cd /home/opensource/uapclaw-gateway && go test ./internal/agentcore/foundation/store/vector_fields/... -v -run "TestShouldKeep|TestToDict"`
Expected: PASS

- [ ] **Step 6: 补充 VectorField 自身的 ToDict 测试**

在 `base_test.go` 追加：

```go
func TestVectorField_ToDict(t *testing.T) {
	vf := NewVectorField("embedding", "chroma", "hnsw")

	// VectorField 的字段全是 stage:"-"，ToDict 应返回空
	constructResult := ToDict(vf, "construct")
	assert.Empty(t, constructResult)

	searchResult := ToDict(vf, "search")
	assert.Empty(t, searchResult)
}

func TestVectorField_ToDict_Pointer(t *testing.T) {
	vf := NewVectorField("embedding", "chroma", "hnsw")

	// 指针也应该正常工作
	constructResult := ToDict(&vf, "construct")
	assert.Empty(t, constructResult)
}
```

Run: `cd /home/opensource/uapclaw-gateway && go test ./internal/agentcore/foundation/store/vector_fields/... -v`
Expected: PASS

- [ ] **Step 7: 提交**

```bash
git add internal/agentcore/foundation/store/vector_fields/
git commit -m "feat(vector_fields): 添加 VectorField 基类 + ToDict 反射阶段过滤机制"
```

---

## Task 2: ChromaVectorField struct

**Files:**
- Create: `internal/agentcore/foundation/store/vector_fields/chroma.go`
- Create: `internal/agentcore/foundation/store/vector_fields/chroma_test.go`
- Modify: `internal/agentcore/foundation/store/vector_fields/doc.go` — 添加 chroma.go 条目

- [ ] **Step 1: 写 ChromaVectorField 构造 + ToConstructDict/ToSearchDict 的失败测试**

```go
package vectorfields

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewChromaVectorField_默认值(t *testing.T) {
	f := NewChromaVectorField()
	assert.Equal(t, "embedding", f.VectorFieldName)
	assert.Equal(t, "chroma", f.DatabaseType)
	assert.Equal(t, "hnsw", f.IndexType)
	assert.Equal(t, 16, f.MaxNeighbors)
	assert.Equal(t, 100, f.EfConstruction)
	assert.Equal(t, 100, f.EfSearch)
}

func TestNewChromaVectorFieldFromName(t *testing.T) {
	f := NewChromaVectorFieldFromName("my_vector")
	assert.Equal(t, "my_vector", f.VectorFieldName)
	assert.Equal(t, "chroma", f.DatabaseType)
	assert.Equal(t, "hnsw", f.IndexType)
	assert.Equal(t, 16, f.MaxNeighbors)
}

func TestChromaVectorField_ToConstructDict(t *testing.T) {
	f := NewChromaVectorField()
	result := f.ToConstructDict()

	// stage:"-" 的字段应被移除
	assert.NotContains(t, result, "vector_field")
	assert.NotContains(t, result, "database_type")
	assert.NotContains(t, result, "index_type")

	// stage:"construct" 的字段应保留
	assert.Equal(t, 16, result["max_neighbors"])
	assert.Equal(t, 100, result["ef_construction"])
	assert.Equal(t, 100, result["ef_search"])

	// stage:"search" 的字段应被移除
	assert.NotContains(t, result, "extra_search")
	assert.NotContains(t, result, "resize_factor")
}

func TestChromaVectorField_ToSearchDict_无额外参数(t *testing.T) {
	f := NewChromaVectorField()
	result := f.ToSearchDict()

	// 无 ExtraSearch 时，search 阶段应返回空字典
	// 因为 ChromaVectorField 的所有非内部字段都是 stage:"construct"
	// ExtraSearch 是 stage:"search" 但为零值（nil map → 跳过）
	assert.Empty(t, result)
}

func TestChromaVectorField_ToSearchDict_有额外参数(t *testing.T) {
	f := NewChromaVectorField()
	f.ExtraSearch = map[string]any{
		"resize_factor": 1.5,
		"num_threads":   4,
	}
	result := f.ToSearchDict()

	// ExtraSearch 应被展开
	assert.NotContains(t, result, "extra_search")
	assert.Equal(t, 1.5, result["resize_factor"])
	assert.Equal(t, 4, result["num_threads"])

	// construct 阶段字段不应出现
	assert.NotContains(t, result, "max_neighbors")
	assert.NotContains(t, result, "ef_construction")
}

func TestChromaVectorField_ValidateExtraSearch_合法(t *testing.T) {
	f := NewChromaVectorField()
	f.ExtraSearch = map[string]any{
		"resize_factor":  1.5,
		"num_threads":    4,
		"batch_size":     100,
		"sync_threshold": 1000,
	}
	err := f.ValidateExtraSearch()
	assert.NoError(t, err)
}

func TestChromaVectorField_ValidateExtraSearch_resizeFactor类型错误(t *testing.T) {
	f := NewChromaVectorField()
	f.ExtraSearch = map[string]any{
		"resize_factor": "invalid",
	}
	err := f.ValidateExtraSearch()
	assert.Error(t, err)
}

func TestChromaVectorField_ValidateExtraSearch_numThreads类型错误(t *testing.T) {
	f := NewChromaVectorField()
	f.ExtraSearch = map[string]any{
		"num_threads": 3.14,
	}
	err := f.ValidateExtraSearch()
	assert.Error(t, err)
}

func TestChromaVectorField_ValidateExtraSearch_空字典(t *testing.T) {
	f := NewChromaVectorField()
	f.ExtraSearch = map[string]any{}
	err := f.ValidateExtraSearch()
	assert.NoError(t, err)
}

func TestChromaVectorField_ValidateExtraSearch_nil(t *testing.T) {
	f := NewChromaVectorField()
	// ExtraSearch 为 nil 时不校验
	err := f.ValidateExtraSearch()
	assert.NoError(t, err)
}
```

- [ ] **Step 2: 运行测试确认失败**

Run: `cd /home/opensource/uapclaw-gateway && go test ./internal/agentcore/foundation/store/vector_fields/... -v -run TestNewChroma`
Expected: FAIL — 函数未定义

- [ ] **Step 3: 实现 chroma.go**

```go
package vectorfields

import (
	"fmt"

	"github.com/uapclaw/uapclaw-go/internal/common/exception"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ChromaVectorField ChromaDB HNSW 索引配置。
//
// 对齐 Python ChromaVectorField，含 HNSW 图参数和搜索参数。
// database_type 固定为 "chroma"，index_type 固定为 "hnsw"。
//
// Python: foundation/store/vector_fields/chroma_fields.py (ChromaVectorField)
type ChromaVectorField struct {
	VectorField                                                          // 嵌入基类
	// MaxNeighbors HNSW 图每个节点的最大边数。默认 16，范围 [2, 2048]
	MaxNeighbors int `json:"max_neighbors" stage:"construct"`
	// EfConstruction 构建时候选邻居数。默认 100，范围 [1, ∞)
	EfConstruction int `json:"ef_construction" stage:"construct"`
	// EfSearch 搜索时探索候选数。默认 100，范围 [1, ∞)
	// 注意：Python 中 ef_search 类型为 float，但实际使用为整数，Go 中使用 int
	EfSearch int `json:"ef_search" stage:"construct"`
	// ExtraSearch 额外搜索参数（resize_factor/num_threads/batch_size/sync_threshold）
	// TODO: 待 chroma-go SDK 支持后启用
	ExtraSearch map[string]any `json:"extra_search" stage:"search"`
}

// ChromaVectorFieldOption ChromaVectorField 的可选参数函数。
type ChromaVectorFieldOption func(*ChromaVectorField)

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

const (
	// defaultMaxNeighbors 默认最大邻居数，对齐 Python max_neighbors=16
	defaultMaxNeighbors = 16
	// defaultEfConstruction 默认构建候选数，对齐 Python ef_construction=100
	defaultEfConstruction = 100
	// defaultEfSearch 默认搜索候选数，对齐 Python ef_search=100
	defaultEfSearch = 100
)

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// NewChromaVectorField 创建默认 ChromaVectorField。
// 对齐 Python ChromaVectorField() 默认值。
func NewChromaVectorField(opts ...ChromaVectorFieldOption) *ChromaVectorField {
	f := &ChromaVectorField{
		VectorField:    NewVectorField("embedding", "chroma", "hnsw"),
		MaxNeighbors:   defaultMaxNeighbors,
		EfConstruction: defaultEfConstruction,
		EfSearch:       defaultEfSearch,
	}
	for _, opt := range opts {
		opt(f)
	}
	return f
}

// NewChromaVectorFieldFromName 从向量字段名创建默认 ChromaVectorField。
// 对齐 Python ChromaVectorField(vector_field=name)。
// Python 中 vector_field 参数支持 str，自动包装为 ChromaVectorField。
func NewChromaVectorFieldFromName(name string) *ChromaVectorField {
	return NewChromaVectorField(WithChromaVectorFieldName(name))
}

// WithChromaVectorFieldName 设置向量字段名。
func WithChromaVectorFieldName(name string) ChromaVectorFieldOption {
	return func(f *ChromaVectorField) {
		f.VectorFieldName = name
	}
}

// WithMaxNeighbors 设置最大邻居数。
func WithMaxNeighbors(n int) ChromaVectorFieldOption {
	return func(f *ChromaVectorField) {
		f.MaxNeighbors = n
	}
}

// WithEfConstruction 设置构建候选数。
func WithEfConstruction(n int) ChromaVectorFieldOption {
	return func(f *ChromaVectorField) {
		f.EfConstruction = n
	}
}

// WithEfSearch 设置搜索候选数。
func WithEfSearch(n int) ChromaVectorFieldOption {
	return func(f *ChromaVectorField) {
		f.EfSearch = n
	}
}

// WithExtraSearch 设置额外搜索参数。
func WithExtraSearch(extra map[string]any) ChromaVectorFieldOption {
	return func(f *ChromaVectorField) {
		f.ExtraSearch = extra
	}
}

// ToConstructDict 返回构建阶段配置。
// 对齐 Python vector_field.to_dict(stage="construct")。
func (f *ChromaVectorField) ToConstructDict() map[string]any {
	return ToDict(f, "construct")
}

// ToSearchDict 返回搜索阶段配置。
// 对齐 Python vector_field.to_dict(stage="search")。
func (f *ChromaVectorField) ToSearchDict() map[string]any {
	return ToDict(f, "search")
}

// ValidateExtraSearch 校验 ExtraSearch 字段类型。
//
// 对齐 Python ChromaVectorField.validate_kwargs。
//   - resize_factor 必须为 int 或 float
//   - num_threads/batch_size/sync_threshold 必须为 int
func (f *ChromaVectorField) ValidateExtraSearch() error {
	if f.ExtraSearch == nil {
		return nil
	}

	// 校验 resize_factor
	if v, ok := f.ExtraSearch["resize_factor"]; ok {
		switch v.(type) {
		case int, int64, float32, float64:
			// 合法
		default:
			return exception.BuildError(
				exception.StatusRetrievalIndexingVectorFieldInvalid,
				exception.WithParam("error_msg",
					fmt.Sprintf("ChromaVectorField.extra_search 字段 resize_factor 类型无效，期望 int 或 float，实际 %T", v)),
			)
		}
	}

	// 校验 num_threads / batch_size / sync_threshold
	intAttrs := []string{"num_threads", "batch_size", "sync_threshold"}
	for _, attr := range intAttrs {
		if v, ok := f.ExtraSearch[attr]; ok {
			switch v.(type) {
			case int, int64:
				// 合法
			default:
				return exception.BuildError(
					exception.StatusRetrievalIndexingVectorFieldInvalid,
					exception.WithParam("error_msg",
						fmt.Sprintf("ChromaVectorField.extra_search 字段 %s 类型无效，期望 int，实际 %T", attr, v)),
				)
			}
		}
	}
	return nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────
```

- [ ] **Step 4: 运行测试确认通过**

Run: `cd /home/opensource/uapclaw-gateway && go test ./internal/agentcore/foundation/store/vector_fields/... -v`
Expected: PASS

- [ ] **Step 5: 更新 doc.go 文件目录**

在 doc.go 中添加 `chroma.go` 条目到文件目录树。

- [ ] **Step 6: 提交**

```bash
git add internal/agentcore/foundation/store/vector_fields/
git commit -m "feat(vector_fields): 添加 ChromaVectorField HNSW 索引配置 + 校验"
```

---

## Task 3: DocIndexCallback + NoOp + Logging

**Files:**
- Create: `internal/agentcore/retrieval/common/callbacks.go`
- Create: `internal/agentcore/retrieval/common/callbacks_test.go`

- [ ] **Step 1: 写 DocIndexCallback 的失败测试**

```go
package common

import (
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	storeEmbedding "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/store/embedding"
)

func TestDocIndexCallback_类型别名(t *testing.T) {
	// DocIndexCallback 应该是 embedding.Callback 的类型别名
	// NoOpDocIndexCallback 应实现 DocIndexCallback 接口
	var _ DocIndexCallback = (*NoOpDocIndexCallback)(nil)
	var _ storeEmbedding.Callback = (*NoOpDocIndexCallback)(nil)
}

func TestNoOpDocIndexCallback_调用计数(t *testing.T) {
	cb := &NoOpDocIndexCallback{}
	cb.OnBatchComplete(0, 10, []string{"a", "b"})
	cb.OnBatchComplete(10, 20, []string{"c", "d"})
	assert.Equal(t, 2, cb.CallCounter())
}

func TestNoOpDocIndexCallback_并发安全(t *testing.T) {
	cb := &NoOpDocIndexCallback{}
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			cb.OnBatchComplete(0, 1, nil)
		}()
	}
	wg.Wait()
	assert.Equal(t, 100, cb.CallCounter())
}

func TestLoggingDocIndexCallback_总数小于100不打日志(t *testing.T) {
	// total=5, 打 5 次 OnBatchComplete
	// 最后一批（processed >= total）会打日志
	cb := NewLoggingDocIndexCallback(5)
	assert.NotNil(t, cb)
	// 不 panic 即可
	for i := 0; i < 5; i++ {
		cb.OnBatchComplete(i*10, (i+1)*10, []string{"text"})
	}
}

func TestLoggingDocIndexCallback_总数大于100每100批打日志(t *testing.T) {
	cb := NewLoggingDocIndexCallback(250)
	assert.NotNil(t, cb)
	for i := 0; i < 250; i++ {
		cb.OnBatchComplete(i*10, (i+1)*10, []string{"text"})
	}
}
```

- [ ] **Step 2: 运行测试确认失败**

Run: `cd /home/opensource/uapclaw-gateway && go test ./internal/agentcore/retrieval/common/... -v -run "TestDocIndex|TestNoOp|TestLogging"`
Expected: FAIL — 类型未定义

- [ ] **Step 3: 实现 callbacks.go**

```go
package common

import (
	"sync"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/store/embedding"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 类型别名 ────────────────────────────

// DocIndexCallback 文档索引进度回调接口。
//
// 用于追踪嵌入批次的处理进度。对齐 Python BaseCallback。
// 类型别名指向 foundation 层 embedding.Callback，保持编译期类型安全。
//
// Python: retrieval/common/callbacks.py (BaseCallback)
type DocIndexCallback = embedding.Callback

// ──────────────────────────── 结构体 ────────────────────────────

// NoOpDocIndexCallback 空操作回调，默认使用。
//
// 对齐 Python BaseCallback 的空操作语义（仅计数器+1，无进度条）。
// 实现 embedding.Callback 接口。
type NoOpDocIndexCallback struct {
	// callCounter 调用计数器，对齐 Python BaseCallback._call_counter
	callCounter int
	// mu 线程锁，对齐 Python BaseCallback._thread_lock
	mu sync.Mutex
}

// LoggingDocIndexCallback 日志进度回调。
//
// 对齐 Python TqdmCallback 的进度追踪语义。
// 每 100 批或最后一批打 Info 日志，对齐 Python ChromaVectorStore.add 中
// `if processed % 100 == 0: logger.info("Written %d/%d records")` 的逻辑。
type LoggingDocIndexCallback struct {
	// total 总批次数
	total int
	// processed 已处理批次数
	processed int
	// mu 线程锁
	mu sync.Mutex
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// NewLoggingDocIndexCallback 创建日志进度回调。
func NewLoggingDocIndexCallback(total int) *LoggingDocIndexCallback {
	return &LoggingDocIndexCallback{total: total}
}

// OnBatchComplete 实现 embedding.Callback 接口 — NoOpDocIndexCallback。
func (c *NoOpDocIndexCallback) OnBatchComplete(_ int, _ int, _ []string) {
	c.mu.Lock()
	c.callCounter++
	c.mu.Unlock()
}

// CallCounter 返回累计调用次数，对齐 Python BaseCallback.call_counter。
func (c *NoOpDocIndexCallback) CallCounter() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.callCounter
}

// OnBatchComplete 实现 embedding.Callback 接口 — LoggingDocIndexCallback。
func (c *LoggingDocIndexCallback) OnBatchComplete(startIdx, endIdx int, _ []string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.processed++
	if c.processed%100 == 0 || c.processed >= c.total {
		logger.Info(logComponent).
			Int("processed", endIdx).
			Int("total", c.total).
			Msg("嵌入进度")
	}
}

// ──────────────────────────── 非导出函数 ────────────────────────────
```

- [ ] **Step 4: 运行测试确认通过**

Run: `cd /home/opensource/uapclaw-gateway && go test ./internal/agentcore/retrieval/common/... -v -run "TestDocIndex|TestNoOp|TestLogging"`
Expected: PASS

- [ ] **Step 5: 提交**

```bash
git add internal/agentcore/retrieval/common/callbacks.go internal/agentcore/retrieval/common/callbacks_test.go
git commit -m "feat(retrieval): 添加 DocIndexCallback + NoOpDocIndexCallback + LoggingDocIndexCallback"
```

---

## Task 4: 回填 ComputeChunkEmbeddings 签名

**Files:**
- Modify: `internal/agentcore/retrieval/indexing/indexer/embed_chunks.go`
- Modify: `internal/agentcore/retrieval/indexing/indexer/embed_chunks_test.go`

- [ ] **Step 1: 写 ChunkEmbedOptions 的测试**

在 `embed_chunks_test.go` 中添加：

```go
func TestWithUseCaptionForImages(t *testing.T) {
	opts := NewChunkEmbedOptions(WithUseCaptionForImages(true))
	assert.True(t, opts.UseCaptionForImages)
}

func TestWithDocIndexCallback(t *testing.T) {
	cb := &NoOpDocIndexCallback{}
	opts := NewChunkEmbedOptions(WithDocIndexCallback(cb))
	assert.NotNil(t, opts.DocIndexCallback)
}

func TestNewChunkEmbedOptions_默认值(t *testing.T) {
	opts := NewChunkEmbedOptions()
	assert.False(t, opts.UseCaptionForImages)
	assert.Nil(t, opts.DocIndexCallback)
	assert.Nil(t, opts.EmbedOpts)
}

func TestComputeChunkEmbeddings_新签名_纯文本(t *testing.T) {
	fake := newFakeBaseEmbedding(3)
	chunks := makeTextChunks(3)

	err := ComputeChunkEmbeddings(context.Background(), chunks, fake,
		WithUseCaptionForImages(false),
	)

	assert.NoError(t, err)
	assert.Equal(t, 1, fake.embedDocsCalled)
	for _, c := range chunks {
		assert.NotNil(t, c.Embedding)
	}
}

func TestComputeChunkEmbeddings_新签名_带回调(t *testing.T) {
	fake := newFakeBaseEmbedding(2)
	cb := &NoOpDocIndexCallback{}
	chunks := makeTextChunks(2)

	err := ComputeChunkEmbeddings(context.Background(), chunks, fake,
		WithUseCaptionForImages(false),
		WithDocIndexCallback(cb),
	)

	assert.NoError(t, err)
	assert.Equal(t, 1, fake.embedDocsCalled)
	// NoOpDocIndexCallback 不影响嵌入结果
	for _, c := range chunks {
		assert.NotNil(t, c.Embedding)
	}
}
```

- [ ] **Step 2: 修改 embed_chunks.go 签名**

将现有签名的 `useCaptionForImages bool, opts ...storeEmbedding.EmbedOption` 改为 `opts ...ChunkEmbedOption`，新增 `ChunkEmbedOptions` struct 和相关 option 函数。内部逻辑适配新选项读取方式，并在调 `EmbedDocuments` 时通过 `WithCallback(docIndexCallback)` 传入回调。

具体改动点：
1. 新增 `ChunkEmbedOptions` struct、`ChunkEmbedOption` func 类型
2. 新增 `NewChunkEmbedOptions`、`WithUseCaptionForImages`、`WithDocIndexCallback`、`WithEmbedOpts` 函数
3. `ComputeChunkEmbeddings` 签名改为 `opts ...ChunkEmbedOption`
4. 函数体从 `ChunkEmbedOptions` 读取 `UseCaptionForImages` 和 `DocIndexCallback`
5. `EmbedDocuments` 调用改为 `EmbedDocuments(ctx, texts, append(embedOpts, storeEmbedding.WithCallback(docIndexCallback))...)`

- [ ] **Step 3: 适配所有已有测试调用点**

11 个 `ComputeChunkEmbeddings` 调用从 `ComputeChunkEmbeddings(ctx, chunks, fake, false)` 改为 `ComputeChunkEmbeddings(ctx, chunks, fake, WithUseCaptionForImages(false))`。

- [ ] **Step 4: 运行测试确认通过**

Run: `cd /home/opensource/uapclaw-gateway && go test ./internal/agentcore/retrieval/indexing/indexer/... -v`
Expected: PASS

- [ ] **Step 5: 全项目编译确认无遗漏**

Run: `cd /home/opensource/uapclaw-gateway && go build ./...`
Expected: 无编译错误

- [ ] **Step 6: 提交**

```bash
git add internal/agentcore/retrieval/indexing/indexer/
git commit -m "refactor(indexer): ComputeChunkEmbeddings 改为 option 模式，加 DocIndexCallback 参数"
```

---

## Task 5: SearchResult + RetrievalResult

**Files:**
- Create: `internal/agentcore/retrieval/common/result.go`
- Create: `internal/agentcore/retrieval/common/result_test.go`

- [ ] **Step 1: 写 SearchResult/RetrievalResult 测试**

```go
package common

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestSearchResult_JSON序列化(t *testing.T) {
	sr := SearchResult{
		ID:       "chunk-1",
		Text:     "测试文本",
		Score:    0.95,
		Metadata: map[string]any{"doc_id": "doc-1"},
	}
	data, err := json.Marshal(sr)
	assert.NoError(t, err)
	assert.Contains(t, string(data), `"id"`)
	assert.Contains(t, string(data), `"score"`)
}

func TestRetrievalResult_JSON序列化(t *testing.T) {
	rr := RetrievalResult{
		Text:     "测试文本",
		Score:    0.88,
		Metadata: map[string]any{"source": "web"},
		DocID:    "doc-1",
		ChunkID:  "chunk-1",
	}
	data, err := json.Marshal(rr)
	assert.NoError(t, err)
	assert.Contains(t, string(data), `"doc_id"`)
	assert.Contains(t, string(data), `"chunk_id"`)
}
```

- [ ] **Step 2: 实现 result.go**

```go
package common

// ──────────────────────────── 结构体 ────────────────────────────

// SearchResult 搜索结果数据模型。
//
// Python: retrieval/common/retrieval_result.py (SearchResult)
type SearchResult struct {
	// ID 结果 ID
	ID string `json:"id"`
	// Text 文本内容
	Text string `json:"text"`
	// Score 相关性分数
	Score float64 `json:"score"`
	// Metadata 元数据
	Metadata map[string]any `json:"metadata"`
}

// RetrievalResult 检索结果数据模型。
//
// Python: retrieval/common/retrieval_result.py (RetrievalResult)
type RetrievalResult struct {
	// Text 文本内容
	Text string `json:"text"`
	// Score 相关性分数
	Score float64 `json:"score"`
	// Metadata 元数据
	Metadata map[string]any `json:"metadata"`
	// DocID 文档 ID
	DocID string `json:"doc_id,omitempty"`
	// ChunkID 分块 ID
	ChunkID string `json:"chunk_id,omitempty"`
}
```

- [ ] **Step 3: 运行测试确认通过**

Run: `cd /home/opensource/uapclaw-gateway && go test ./internal/agentcore/retrieval/common/... -v -run "TestSearchResult|TestRetrievalResult"`
Expected: PASS

- [ ] **Step 4: 提交**

```bash
git add internal/agentcore/retrieval/common/result.go internal/agentcore/retrieval/common/result_test.go
git commit -m "feat(retrieval): 添加 SearchResult + RetrievalResult 数据模型"
```

---

## Task 6: RRFFusion

**Files:**
- Create: `internal/agentcore/retrieval/utils/fusion.go`
- Create: `internal/agentcore/retrieval/utils/fusion_test.go`

- [ ] **Step 1: 写 RRFFusion 测试**

```go
package utils

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/retrieval/common"
)

func TestRRFFusion_单路结果(t *testing.T) {
	list := [][]common.RetrievalResult{
		{
			{Text: "a", Score: 0.9, Metadata: map[string]any{}},
			{Text: "b", Score: 0.8, Metadata: map[string]any{}},
		},
	}
	result := RRFFusion(list, 60)
	assert.Len(t, result, 2)
	// 单路结果顺序不变
	assert.Equal(t, "a", result[0].Text)
	assert.Equal(t, "b", result[1].Text)
}

func TestRRFFusion_两路融合(t *testing.T) {
	list := [][]common.RetrievalResult{
		{
			{Text: "a", Score: 0.9, Metadata: map[string]any{"source": "vec"}},
			{Text: "b", Score: 0.8, Metadata: map[string]any{"source": "vec"}},
		},
		{
			{Text: "b", Score: 0.95, Metadata: map[string]any{"source": "text"}},
			{Text: "c", Score: 0.7, Metadata: map[string]any{"source": "text"}},
		},
	}
	result := RRFFusion(list, 60)
	assert.Len(t, result, 3)
	// "b" 在两路都出现，RRF 分数最高
	assert.Equal(t, "b", result[0].Text)
	// "b" 保留首次出现的 metadata（来自 vec）
	assert.Equal(t, "vec", result[0].Metadata["source"])
}

func TestRRFFusion_空列表(t *testing.T) {
	result := RRFFusion(nil, 60)
	assert.Empty(t, result)
}

func TestRRFFusion_空子列表(t *testing.T) {
	list := [][]common.RetrievalResult{
		{},
		{{Text: "a", Score: 0.9, Metadata: map[string]any{}}},
	}
	result := RRFFusion(list, 60)
	assert.Len(t, result, 1)
}
```

- [ ] **Step 2: 实现 fusion.go**

```go
package utils

import (
	"sort"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/retrieval/common"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// RRFFusion 倒数秩融合（Reciprocal Rank Fusion）。
//
// 将多路检索结果融合为单一排序结果。
// 对齐 Python retrieval/utils/fusion.py (rrf_fusion)。
//   - 用 text 作为唯一键做去重
//   - RRF 分数 = Σ 1/(k + rank)
//   - 保留首次出现的 metadata
//   - 按分数降序排列
func RRFFusion(resultsList [][]common.RetrievalResult, k int) []common.RetrievalResult {
	if len(resultsList) == 0 {
		return nil
	}

	scoreDict := make(map[string]float64)
	resultDict := make(map[string]*common.RetrievalResult)

	for _, results := range resultsList {
		for rank, result := range results {
			key := result.Text
			scoreDict[key] += 1.0 / float64(k+rank+1) // rank 从 0 开始，对齐 Python enumerate(start=1)
			if _, exists := resultDict[key]; !exists {
				resultDict[key] = &result
			}
		}
	}

	// 按分数降序排列
	type kv struct {
		key   string
		score float64
	}
	var sorted []kv
	for key, score := range scoreDict {
		sorted = append(sorted, kv{key: key, score: score})
	}
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].score > sorted[j].score
	})

	// 构建融合结果
	fusedResults := make([]common.RetrievalResult, 0, len(sorted))
	for _, item := range sorted {
		result := *resultDict[item.key]
		result.Score = item.score
		fusedResults = append(fusedResults, result)
	}
	return fusedResults
}
```

- [ ] **Step 3: 运行测试确认通过**

Run: `cd /home/opensource/uapclaw-gateway && go test ./internal/agentcore/retrieval/utils/... -v -run TestRRFFusion`
Expected: PASS

- [ ] **Step 4: 提交**

```bash
git add internal/agentcore/retrieval/utils/fusion.go internal/agentcore/retrieval/utils/fusion_test.go
git commit -m "feat(retrieval): 添加 RRFFusion 倒数秩融合"
```

---

## Task 7: VectorStore 接口（retrieval 层）

**Files:**
- Create: `internal/agentcore/retrieval/vector_store/doc.go`
- Create: `internal/agentcore/retrieval/vector_store/base.go`
- Create: `internal/agentcore/retrieval/vector_store/base_test.go`

- [ ] **Step 1: 创建 doc.go + base.go**

`doc.go` 包文档 + `base.go` 定义 `VectorStore` 接口、`StoreOptions`、`StoreOption`、`CheckConfigsMatching`。

关键内容：
- `VectorStore` 接口（CreateClient/CheckVectorField/Add/Search/SparseSearch/HybridSearch/Delete/TableExists/DeleteTable/Close）
- `StoreOptions` struct（BatchSize）
- `StoreOption` func 类型
- `NewStoreOptions`、`WithStoreBatchSize` 函数
- `CheckConfigsMatching` 静态方法对齐 Python `_check_configs_matching`

- [ ] **Step 2: 写 CheckConfigsMatching 测试**

```go
func TestCheckConfigsMatching_匹配(t *testing.T) {
	configured := map[string]any{"space": "cosine", "max_neighbors": 16}
	actual := map[string]any{"space": "cosine", "max_neighbors": 16, "ef_search": 100}
	err := CheckConfigsMatching(configured, actual)
	assert.NoError(t, err)
}

func TestCheckConfigsMatching_不匹配(t *testing.T) {
	configured := map[string]any{"space": "cosine"}
	actual := map[string]any{"space": "l2"}
	err := CheckConfigsMatching(configured, actual)
	assert.Error(t, err)
}

func TestCheckConfigsMatching_数值近似匹配(t *testing.T) {
	configured := map[string]any{"max_neighbors": 16.0}
	actual := map[string]any{"max_neighbors": 16}
	err := CheckConfigsMatching(configured, actual)
	assert.NoError(t, err)
}
```

- [ ] **Step 3: 运行测试确认通过**

Run: `cd /home/opensource/uapclaw-gateway && go test ./internal/agentcore/retrieval/vector_store/... -v`
Expected: PASS

- [ ] **Step 4: 提交**

```bash
git add internal/agentcore/retrieval/vector_store/
git commit -m "feat(vector_store): 添加 retrieval 层 VectorStore 接口 + CheckConfigsMatching"
```

---

## Task 8: ChromaVectorStore（retrieval 层）

**Files:**
- Create: `internal/agentcore/retrieval/vector_store/chroma.go`
- Create: `internal/agentcore/retrieval/vector_store/chroma_test.go`

这是最大的实现任务。按 TDD 逐步实现每个方法。

- [ ] **Step 1: 写构造函数测试**

```go
func TestNewChromaVectorStore_默认参数(t *testing.T) {
	dir := t.TempDir()
	config := common.NewDefaultVectorStoreConfig("test-collection")

	vs, err := NewChromaVectorStore(config, dir,
		"content",           // textField
		"embedding",         // vectorField (string)
		"sparse_vector",     // sparseVectorField
		"metadata",          // metadataField
		"document_id",       // docIDField
	)

	assert.NoError(t, err)
	assert.NotNil(t, vs)
	assert.Equal(t, "cosine", vs.DistanceMetric())
}

func TestNewChromaVectorStore_chromaPath为空(t *testing.T) {
	config := common.NewDefaultVectorStoreConfig("test-collection")

	_, err := NewChromaVectorStore(config, "",
		"content", "embedding", "sparse_vector", "metadata", "document_id",
	)

	assert.Error(t, err)
}

func TestNewChromaVectorStore_vectorField为ChromaVectorField(t *testing.T) {
	dir := t.TempDir()
	config := common.NewDefaultVectorStoreConfig("test-collection")
	vf := vectorfields.NewChromaVectorField()

	vs, err := NewChromaVectorStore(config, dir,
		"content", vf, "sparse_vector", "metadata", "document_id",
	)

	assert.NoError(t, err)
	assert.NotNil(t, vs)
}
```

注意：需要先在 `common/config.go` 中确认 `NewDefaultVectorStoreConfig` 是否存在，如果不存在需添加。

- [ ] **Step 2: 实现构造函数 + Add + Search + SparseSearch + HybridSearch + Delete + Close + TableExists + DeleteTable**

完整实现 `ChromaVectorStore` struct，对齐 Python `chroma_store.py`。每个方法内部组合 foundation 层 `ChromaVectorStore` 或直接使用 chroma-go SDK。

核心实现要点：
1. 构造时校验 chromaPath、vectorField 类型断言（string 或 *ChromaVectorField）
2. 距离度量归一化（dot→ip，euclidean→l2）
3. constructConfig/searchConfig 通过 ChromaVectorField.ToConstructDict()/ToSearchDict() 生成
4. Add 按字段名映射提取 ids/embeddings/documents/metadatas
5. Search 调 collection.Query，结果用距离转换函数
6. SparseSearch 调 collection.Query(query_texts)
7. HybridSearch 并发 Search+SparseSearch，RRF 融合
8. 结果转换函数 chromaResultToSearchResults

- [ ] **Step 3: 写 Add + Search 方法测试**

使用 fake collection mock 测试 Add 和 Search 的字段映射和结果转换逻辑。

- [ ] **Step 4: 写 HybridSearch 测试**

mock Search 和 SparseSearch 的返回结果，验证 RRF 融合逻辑。

- [ ] **Step 5: 运行全量测试**

Run: `cd /home/opensource/uapclaw-gateway && go test ./internal/agentcore/retrieval/vector_store/... -v`
Expected: PASS

- [ ] **Step 6: 提交**

```bash
git add internal/agentcore/retrieval/vector_store/
git commit -m "feat(vector_store): 添加 retrieval 层 ChromaVectorStore（Add/Search/SparseSearch/HybridSearch）"
```

---

## Task 9: ChromaIndexer

**Files:**
- Create: `internal/agentcore/retrieval/indexing/indexer/chroma.go`
- Create: `internal/agentcore/retrieval/indexing/indexer/chroma_test.go`
- Modify: `internal/agentcore/retrieval/indexing/indexer/doc.go` — 添加 chroma.go 条目

- [ ] **Step 1: 写构造函数测试**

```go
func TestNewChromaIndexer_默认参数(t *testing.T) {
	dir := t.TempDir()
	config := common.VectorStoreConfig{
		StoreProvider:  common.StoreTypeChroma,
		CollectionName: "test-collection",
		DistanceMetric: common.DistanceMetricCosine,
	}

	indexer, err := NewChromaIndexer(config, dir)

	assert.NoError(t, err)
	assert.NotNil(t, indexer)
	assert.Equal(t, "cosine", indexer.DistanceMetric())
}

func TestNewChromaIndexer_chromaPath为空(t *testing.T) {
	config := common.VectorStoreConfig{
		StoreProvider:  common.StoreTypeChroma,
		CollectionName: "test-collection",
	}
	_, err := NewChromaIndexer(config, "")
	assert.Error(t, err)
}

func TestNewChromaIndexer_距离度量归一化(t *testing.T) {
	dir := t.TempDir()

	tests := []struct {
		input    common.DistanceMetricKind
		expected string
	}{
		{common.DistanceMetricCosine, "cosine"},
		{common.DistanceMetricEuclidean, "l2"},
		{common.DistanceMetricDot, "ip"},
	}

	for _, tt := range tests {
		t.Run(string(tt.input), func(t *testing.T) {
			config := common.VectorStoreConfig{
				StoreProvider:  common.StoreTypeChroma,
				CollectionName: "test-collection",
				DistanceMetric: tt.input,
			}
			indexer, err := NewChromaIndexer(config, dir)
			require.NoError(t, err)
			assert.Equal(t, tt.expected, indexer.DistanceMetric())
		})
	}
}
```

- [ ] **Step 2: 实现 ChromaIndexer struct + 构造函数**

- [ ] **Step 3: 写 BuildIndex 测试（使用 mock）**

```go
func TestChromaIndexer_BuildIndex_查重docID(t *testing.T) {
	// 测试重复 doc_id 时返回错误
}

func TestChromaIndexer_BuildIndex_vector类型需要embedModel(t *testing.T) {
	// 测试 index_type=vector 但 embedModel 为 nil 时返回错误
}

func TestChromaIndexer_BuildIndex_成功(t *testing.T) {
	// 测试正常构建索引流程
}
```

- [ ] **Step 4: 实现 BuildIndex**

4 步流程：查重 doc_id → 嵌入 → 转换字段 → 写入。需要 mock `retrieval.ChromaVectorStore` 的 Add 方法和 collection.Get 方法。

- [ ] **Step 5: 写 UpdateIndex/DeleteIndex/IndexExists/GetIndexInfo 测试**

- [ ] **Step 6: 实现 UpdateIndex/DeleteIndex/IndexExists/GetIndexInfo**

- [ ] **Step 7: 运行全量测试**

Run: `cd /home/opensource/uapclaw-gateway && go test ./internal/agentcore/retrieval/indexing/indexer/... -v`
Expected: PASS

- [ ] **Step 8: 更新 doc.go 文件目录**

在 `indexer/doc.go` 中添加 `chroma.go` 条目。

- [ ] **Step 9: 提交**

```bash
git add internal/agentcore/retrieval/indexing/indexer/chroma.go internal/agentcore/retrieval/indexing/indexer/chroma_test.go internal/agentcore/retrieval/indexing/indexer/doc.go
git commit -m "feat(indexer): 添加 ChromaIndexer 实现（BuildIndex/UpdateIndex/DeleteIndex/IndexExists/GetIndexInfo）"
```

---

## Task 10: 更新 IMPLEMENTATION_PLAN.md + doc.go 全局维护

**Files:**
- Modify: `IMPLEMENTATION_PLAN.md`
- Modify: `foundation/store/vector/doc.go`
- Modify: `retrieval/common/doc.go`
- Modify: `retrieval/utils/doc.go`

- [ ] **Step 1: 更新 IMPLEMENTATION_PLAN.md**

将 13.2 从 `☐` 改为 `✅`。

- [ ] **Step 2: 更新所有 doc.go 文件目录**

确保每个新增文件都在对应包的 doc.go 中有条目。

- [ ] **Step 3: 全项目编译 + 测试**

Run: `cd /home/opensource/uapclaw-gateway && go build ./... && go test ./internal/agentcore/retrieval/... ./internal/agentcore/foundation/store/vector_fields/... -cover`
Expected: 编译通过，测试通过

- [ ] **Step 4: 提交**

```bash
git add IMPLEMENTATION_PLAN.md internal/agentcore/foundation/store/vector/doc.go internal/agentcore/retrieval/common/doc.go internal/agentcore/retrieval/utils/doc.go
git commit -m "docs: 更新 IMPLEMENTATION_PLAN.md 13.2 完成状态 + doc.go 文件目录"
```

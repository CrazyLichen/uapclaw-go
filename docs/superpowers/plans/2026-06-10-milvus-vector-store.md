# MilvusVectorStore 实现计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 实现 MilvusVectorStore，基于官方 milvus-sdk-go/v2 提供完整的向量存储 CRUD 和搜索功能。

**Architecture:** MilvusVectorStore 实现 BaseVectorStore 接口，内部通过 milvusClient 接口抽象层与 Milvus SDK 解耦，便于单元测试 mock。距离转换函数独立于 vector/utils.go，Milvus 索引子类型在 vector_fields/milvus_fields.go 中实现。UpdateSchema 等迁移方法预留签名，逻辑待 7.22/7.23 回填。

**Tech Stack:** Go, milvus-sdk-go/v2, sync.RWMutex（缓存并发保护）

---

## 文件结构

| 文件 | 操作 | 职责 |
|------|------|------|
| `internal/agentcore/store/vector/utils.go` | 新增 | 距离/相似度转换纯函数 |
| `internal/agentcore/store/vector/utils_test.go` | 新增 | 距离转换单元测试 |
| `internal/agentcore/store/vector_fields/milvus_fields.go` | 新增 | Milvus 索引子类型（HNSW/IVF/FLAT/AUTO/SCANN） |
| `internal/agentcore/store/vector_fields/milvus_fields_test.go` | 新增 | Milvus 索引子类型测试 |
| `internal/agentcore/store/vector/base.go` | 修改 | BaseVectorStore 接口补充 3 个方法 + Options 补充 VectorField 字段 |
| `internal/agentcore/store/vector/base_test.go` | 修改 | 补充新接口方法的 mock 测试 |
| `internal/agentcore/store/vector/milvus.go` | 新增 | MilvusVectorStore 结构体 + 全部方法实现 |
| `internal/agentcore/store/vector/milvus_test.go` | 新增 | MilvusVectorStore 单元测试（fakeMilvusClient） |
| `internal/agentcore/store/vector/milvus_integration_test.go` | 新增 | 集成测试（//go:build integration） |
| `internal/agentcore/store/vector/doc.go` | 修改 | 文件目录树更新 |
| `internal/agentcore/store/vector_fields/doc.go` | 修改 | 文件目录树更新 |

---

### Task 1: 距离转换函数（vector/utils.go）

**Files:**
- Create: `internal/agentcore/store/vector/utils.go`
- Test: `internal/agentcore/store/vector/utils_test.go`

- [ ] **Step 1: 编写距离转换测试**

创建 `internal/agentcore/store/vector/utils_test.go`：

```go
package vector

import "testing"

// ──────────────────────────── 导出函数 ────────────────────────────

func TestConvertL2Squared(t *testing.T) {
	tests := []struct {
		name     string
		rawScore float64
		maxDist  float64
		want     float64
	}{
		{"零距离返回1", 0, 4.0, 1.0},
		{"最大距离返回0", 4.0, 4.0, 0.0},
		{"中间值", 2.0, 4.0, 0.5},
		{"超过最大距离截断为0", 5.0, 4.0, 0.0},
		{"负值距离截断为0", -1.0, 4.0, 0.0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ConvertL2Squared(tt.rawScore, tt.maxDist); got != tt.want {
				t.Errorf("ConvertL2Squared(%v, %v) = %v, want %v", tt.rawScore, tt.maxDist, got, tt.want)
			}
		})
	}
}

func TestConvertL2Squared_默认最大距离(t *testing.T) {
	// 默认 maxDist=4.0
	if got := ConvertL2Squared(0, 4.0); got != 1.0 {
		t.Errorf("ConvertL2Squared(0, 4.0) = %v, want 1.0", got)
	}
}

func TestConvertCosineSimilarity(t *testing.T) {
	tests := []struct {
		name     string
		rawScore float64
		want     float64
	}{
		{"完全相似", 1.0, 1.0},
		{"完全相反", -1.0, 0.0},
		{"正交", 0.0, 0.5},
		{"0.6映射", 0.6, 0.8},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ConvertCosineSimilarity(tt.rawScore); got != tt.want {
				t.Errorf("ConvertCosineSimilarity(%v) = %v, want %v", tt.rawScore, got, tt.want)
			}
		})
	}
}

func TestConvertCosineDistance(t *testing.T) {
	tests := []struct {
		name     string
		rawScore float64
		want     float64
	}{
		{"距离0完全相似", 0.0, 1.0},
		{"距离2完全相反", 2.0, 0.0},
		{"距离1正交", 1.0, 0.5},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ConvertCosineDistance(tt.rawScore); got != tt.want {
				t.Errorf("ConvertCosineDistance(%v) = %v, want %v", tt.rawScore, got, tt.want)
			}
		})
	}
}

func TestConvertIPSimilarity(t *testing.T) {
	tests := []struct {
		name     string
		rawScore float64
		want     float64
	}{
		{"高内积1", 1.0, 1.0},
		{"内积-1", -1.0, 0.0},
		{"内积0", 0.0, 0.5},
		{"超高内积截断1", 3.0, 1.0},
		{"超低内积截断0", -3.0, 0.0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ConvertIPSimilarity(tt.rawScore); got != tt.want {
				t.Errorf("ConvertIPSimilarity(%v) = %v, want %v", tt.rawScore, got, tt.want)
			}
		})
	}
}

func TestConvertIPDistance(t *testing.T) {
	tests := []struct {
		name     string
		rawScore float64
		want     float64
	}{
		{"距离0完全相似", 0.0, 1.0},
		{"距离2完全相反", 2.0, 0.0},
		{"距离1", 1.0, 0.5},
		{"超低距离截断0", -1.0, 0.0},
		{"超高距离截断0", 3.0, 0.0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ConvertIPDistance(tt.rawScore); got != tt.want {
				t.Errorf("ConvertIPDistance(%v) = %v, want %v", tt.rawScore, got, tt.want)
			}
		})
	}
}
```

- [ ] **Step 2: 运行测试确认失败**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/store/vector/ -run "TestConvert" -v 2>&1 | head -20`
Expected: 编译失败，函数未定义

- [ ] **Step 3: 实现距离转换函数**

创建 `internal/agentcore/store/vector/utils.go`：

```go
package vector

import "math"

// ──────────────────────────── 导出函数 ────────────────────────────

// ConvertL2Squared 将 L2 平方距离转换为归一化相似度 [0, 1]。
// 公式: max(0, (maxDist - rawScore) / maxDist)
// 默认 maxDist=4.0（单位向量假设下 L2 平方距离上限）。
//
// 对应 Python: vector/utils.py (convert_l2_squared)
func ConvertL2Squared(rawScore, maxDist float64) float64 {
	return math.Max(0, (maxDist-rawScore)/maxDist)
}

// ConvertCosineSimilarity 将余弦相似度 [-1, 1] 转换为归一化相似度 [0, 1]。
// 公式: (rawScore + 1) / 2
//
// 对应 Python: vector/utils.py (convert_cosine_similarity)
func ConvertCosineSimilarity(rawScore float64) float64 {
	return (rawScore + 1.0) / 2.0
}

// ConvertCosineDistance 将余弦距离 [0, 2] 转换为归一化余弦相似度 [0, 1]。
// 公式: (2 - rawScore) / 2
// Chroma 使用余弦距离。
//
// 对应 Python: vector/utils.py (convert_cosine_distance)
func ConvertCosineDistance(rawScore float64) float64 {
	return (2.0 - rawScore) / 2.0
}

// ConvertIPSimilarity 将原始内积转换为归一化相似度 [0, 1]。
// 公式: clamp((rawScore + 1) / 2, 0, 1)
// Milvus 使用内积。
//
// 对应 Python: vector/utils.py (convert_ip_similarity)
func ConvertIPSimilarity(rawScore float64) float64 {
	return math.Max(0, math.Min(1, (rawScore+1.0)/2.0))
}

// ConvertIPDistance 将 Chroma 的内积距离 [0, 2] 转换为归一化相似度 [0, 1]。
// 公式: clamp((2 - rawScore) / 2, 0, 1)
//
// 对应 Python: vector/utils.py (convert_ip_distance)
func ConvertIPDistance(rawScore float64) float64 {
	return math.Max(0, math.Min(1, (2.0-rawScore)/2.0))
}
```

- [ ] **Step 4: 运行测试确认通过**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/store/vector/ -run "TestConvert" -v`
Expected: PASS

- [ ] **Step 5: 提交**

```bash
cd /home/opensource/uap-claw-go && git add internal/agentcore/store/vector/utils.go internal/agentcore/store/vector/utils_test.go && git commit -m "feat(store/vector): 添加距离/相似度转换函数"
```

---

### Task 2: Milvus 索引子类型（vector_fields/milvus_fields.go）

**Files:**
- Create: `internal/agentcore/store/vector_fields/milvus_fields.go`
- Test: `internal/agentcore/store/vector_fields/milvus_fields_test.go`

- [ ] **Step 1: 编写 Milvus 索引子类型测试**

创建 `internal/agentcore/store/vector_fields/milvus_fields_test.go`：

```go
package vector_fields

import "testing"

// ──────────────────────────── 导出函数 ────────────────────────────

func TestMilvusAUTO(t *testing.T) {
	f := NewMilvusAUTO("embedding")
	if f.DatabaseType != DatabaseTypeMilvus {
		t.Errorf("DatabaseType = %v, want %v", f.DatabaseType, DatabaseTypeMilvus)
	}
	if f.IndexType != IndexTypeAUTO {
		t.Errorf("IndexType = %v, want %v", f.IndexType, IndexTypeAUTO)
	}
	if f.VectorFieldName != "embedding" {
		t.Errorf("VectorFieldName = %v, want embedding", f.VectorFieldName)
	}
	if err := f.Validate(); err != nil {
		t.Errorf("Validate() = %v, want nil", err)
	}
	// AUTO 无额外字段，construct/search 输出均为空
	constructDict := ToDict(f, StageConstruct)
	if len(constructDict) != 0 {
		t.Errorf("ToDict(construct) = %v, want empty", constructDict)
	}
	searchDict := ToDict(f, StageSearch)
	if len(searchDict) != 0 {
		t.Errorf("ToDict(search) = %v, want empty", searchDict)
	}
}

func TestMilvusFLAT(t *testing.T) {
	f := NewMilvusFLAT("embedding")
	if f.IndexType != IndexTypeFLAT {
		t.Errorf("IndexType = %v, want %v", f.IndexType, IndexTypeFLAT)
	}
	if err := f.Validate(); err != nil {
		t.Errorf("Validate() = %v, want nil", err)
	}
}

func TestMilvusHNSW(t *testing.T) {
	f := NewMilvusHNSW("embedding", 30, 360, 2.0)
	if f.IndexType != IndexTypeHNSW {
		t.Errorf("IndexType = %v, want %v", f.IndexType, IndexTypeHNSW)
	}
	if err := f.Validate(); err != nil {
		t.Errorf("Validate() = %v, want nil", err)
	}

	// construct 阶段应输出 M 和 EfConstruction
	constructDict := ToDict(f, StageConstruct)
	if constructDict["M"] != 30 {
		t.Errorf("construct M = %v, want 30", constructDict["M"])
	}
	if constructDict["EfConstruction"] != 360 {
		t.Errorf("construct EfConstruction = %v, want 360", constructDict["EfConstruction"])
	}

	// search 阶段应输出 EfSearchFactor
	searchDict := ToDict(f, StageSearch)
	if searchDict["EfSearchFactor"] != 2.0 {
		t.Errorf("search EfSearchFactor = %v, want 2.0", searchDict["EfSearchFactor"])
	}
}

func TestMilvusHNSW_校验失败(t *testing.T) {
	tests := []struct {
		name    string
		m       int
		efc     int
		efsf    float64
		wantErr bool
	}{
		{"M太小", 1, 360, 2.0, true},
		{"M太大", 2049, 360, 2.0, true},
		{"EfConstruction为零", 30, 0, 2.0, true},
		{"EfSearchFactor为零", 30, 360, 0.0, true},
		{"合法值", 30, 360, 2.0, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := NewMilvusHNSW("embedding", tt.m, tt.efc, tt.efsf)
			err := f.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestMilvusIVF(t *testing.T) {
	f := NewMilvusIVF("embedding", 128, 8)
	if f.IndexType != IndexTypeIVF {
		t.Errorf("IndexType = %v, want %v", f.IndexType, IndexTypeIVF)
	}
	if err := f.Validate(); err != nil {
		t.Errorf("Validate() = %v, want nil", err)
	}

	// construct 阶段应输出 Nlist
	constructDict := ToDict(f, StageConstruct)
	if constructDict["Nlist"] != 128 {
		t.Errorf("construct Nlist = %v, want 128", constructDict["Nlist"])
	}

	// search 阶段应输出 Nprobe
	searchDict := ToDict(f, StageSearch)
	if searchDict["Nprobe"] != 8 {
		t.Errorf("search Nprobe = %v, want 8", searchDict["Nprobe"])
	}
}

func TestMilvusIVF_校验失败(t *testing.T) {
	tests := []struct {
		name    string
		nlist   int
		nprobe  int
		wantErr bool
	}{
		{"nlist为零", 0, 8, true},
		{"nprobe为零", 128, 0, true},
		{"nprobe大于nlist", 8, 128, true},
		{"合法值", 128, 8, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := NewMilvusIVF("embedding", tt.nlist, tt.nprobe)
			err := f.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestMilvusSCANN(t *testing.T) {
	f := NewMilvusSCANN("embedding", 128, 8, true, 200)
	if f.IndexType != IndexTypeSCANN {
		t.Errorf("IndexType = %v, want %v", f.IndexType, IndexTypeSCANN)
	}
	if err := f.Validate(); err != nil {
		t.Errorf("Validate() = %v, want nil", err)
	}

	// construct 阶段应输出 Nlist 和 WithRawData
	constructDict := ToDict(f, StageConstruct)
	if constructDict["Nlist"] != 128 {
		t.Errorf("construct Nlist = %v, want 128", constructDict["Nlist"])
	}
	if constructDict["WithRawData"] != true {
		t.Errorf("construct WithRawData = %v, want true", constructDict["WithRawData"])
	}

	// search 阶段应输出 Nprobe 和 ReorderK
	searchDict := ToDict(f, StageSearch)
	if searchDict["Nprobe"] != 8 {
		t.Errorf("search Nprobe = %v, want 8", searchDict["Nprobe"])
	}
	if searchDict["ReorderK"] != 200 {
		t.Errorf("search ReorderK = %v, want 200", searchDict["ReorderK"])
	}
}

func TestMilvusSCANN_校验失败(t *testing.T) {
	tests := []struct {
		name      string
		nlist     int
		nprobe    int
		reorderK  int
		wantErr   bool
	}{
		{"ReorderK为负数", 128, 8, -1, true},
		{"nprobe大于nlist", 8, 128, 200, true},
		{"合法值", 128, 8, 200, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := NewMilvusSCANN("embedding", tt.nlist, tt.nprobe, true, tt.reorderK)
			err := f.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
```

- [ ] **Step 2: 运行测试确认失败**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/store/vector_fields/ -run "TestMilvus" -v 2>&1 | head -20`
Expected: 编译失败，类型未定义

- [ ] **Step 3: 实现 Milvus 索引子类型**

创建 `internal/agentcore/store/vector_fields/milvus_fields.go`：

```go
package vector_fields

import (
	"fmt"

	"github.com/uapclaw/uapclaw-go/internal/common/exception"
)

// ──────────────────────────── 结构体 ────────────────────────────

// MilvusAUTO Milvus AUTOINDEX 配置。
// AUTOINDEX 是 Milvus 默认索引类型，在 milvus.yaml 中可配置。
// 默认参数：M=18, efConstruction=240, index_type=HNSW, metric_type=COSINE。
//
// 对应 Python: vector_fields/milvus_fields.py (MilvusAUTO)
type MilvusAUTO struct {
	VectorField
}

// MilvusFLAT Milvus FLAT 索引配置。
// FLAT 执行精确最近邻搜索，无近似。精度最高但内存占用大、搜索速度慢。
// 适用于中小规模数据集。
//
// 对应 Python: vector_fields/milvus_fields.py (MilvusFLAT)
type MilvusFLAT struct {
	VectorField
}

// MilvusHNSW Milvus HNSW 索引配置。
// HNSW 构建多层图结构进行近似最近邻搜索，搜索性能和精度优秀。
// 支持可选量化变体（SQ/PQ/PRQ）以降低内存占用。
//
// 对应 Python: vector_fields/milvus_fields.py (MilvusHNSW)
type MilvusHNSW struct {
	VectorField
	// M 图中每个节点的最大边数，越高精度越高但内存和构建时间增加
	M int `vf:"construct"`
	// EfConstruction 构建索引时考虑的候选邻居数，越高图质量越好但构建越慢
	EfConstruction int `vf:"construct"`
	// EfSearchFactor 搜索广度乘数，top_k * EfSearchFactor = Milvus 中的 ef
	EfSearchFactor float64 `vf:"search"`
}

// baseIVF IVF 系列索引的公共基类（非导出）。
// IVF 使用 k-means 将向量空间划分为簇，搜索时只查最相关的簇。
//
// 对应 Python: vector_fields/milvus_fields.py (_BaseIVF)
type baseIVF struct {
	VectorField
	// Nlist 构建索引时创建的簇数
	Nlist int `vf:"construct"`
	// Nprobe 搜索时查询的簇数，必须 <= Nlist
	Nprobe int `vf:"search"`
}

// MilvusIVF Milvus IVF 索引配置。
// 支持多种量化变体：FLAT、SQ8、PQ、RABITQ。
//
// 对应 Python: vector_fields/milvus_fields.py (MilvusIVF)
type MilvusIVF struct {
	baseIVF
}

// MilvusSCANN Milvus SCANN 索引配置。
// SCANN 是基于 IVF 的索引，使用乘积量化进行压缩。
// 继承 IVF 的簇参数（Nlist、Nprobe）。
//
// 对应 Python: vector_fields/milvus_fields.py (MilvusSCANN)
type MilvusSCANN struct {
	baseIVF
	// WithRawData 是否存储原始向量，True 提高精度但增加存储
	WithRawData bool `vf:"construct,keepzero"`
	// ReorderK 搜索时使用高精度向量重排序的结果数，仅 WithRawData=True 时有效
	ReorderK int `vf:"search"`
}

// ──────────────────────────── 导出函数 ────────────────────────────

// NewMilvusAUTO 创建 Milvus AUTOINDEX 配置。
func NewMilvusAUTO(fieldName string) *MilvusAUTO {
	return &MilvusAUTO{
		VectorField: *NewVectorField(DatabaseTypeMilvus, IndexTypeAUTO, fieldName),
	}
}

// NewMilvusFLAT 创建 Milvus FLAT 索引配置。
func NewMilvusFLAT(fieldName string) *MilvusFLAT {
	return &MilvusFLAT{
		VectorField: *NewVectorField(DatabaseTypeMilvus, IndexTypeFLAT, fieldName),
	}
}

// NewMilvusHNSW 创建 Milvus HNSW 索引配置。
// M: 图中每个节点最大边数，范围 [2, 2048]，默认 30
// efConstruction: 构建时候选邻居数，范围 [1, +∞)，默认 360
// efSearchFactor: 搜索广度乘数，范围 (0, +∞)，默认 2.0
func NewMilvusHNSW(fieldName string, m, efConstruction int, efSearchFactor float64) *MilvusHNSW {
	return &MilvusHNSW{
		VectorField:    *NewVectorField(DatabaseTypeMilvus, IndexTypeHNSW, fieldName),
		M:             m,
		EfConstruction: efConstruction,
		EfSearchFactor: efSearchFactor,
	}
}

// Validate 校验 HNSW 参数：M 范围 [2, 2048]，EfConstruction > 0，EfSearchFactor > 0。
func (h *MilvusHNSW) Validate() error {
	if h.M < 2 || h.M > 2048 {
		return exception.BuildError(exception.StatusStoreVectorSchemaInvalid,
			exception.WithParam("error_msg", fmt.Sprintf("HNSW M must be in range [2, 2048], got %d", h.M)),
		)
	}
	if h.EfConstruction < 1 {
		return exception.BuildError(exception.StatusStoreVectorSchemaInvalid,
			exception.WithParam("error_msg", fmt.Sprintf("HNSW EfConstruction must be >= 1, got %d", h.EfConstruction)),
		)
	}
	if h.EfSearchFactor <= 0 {
		return exception.BuildError(exception.StatusStoreVectorSchemaInvalid,
			exception.WithParam("error_msg", fmt.Sprintf("HNSW EfSearchFactor must be > 0, got %f", h.EfSearchFactor)),
		)
	}
	return nil
}

// NewMilvusIVF 创建 Milvus IVF 索引配置。
// nlist: 构建时的簇数，范围 [1, 65536]，默认 128
// nprobe: 搜索时的查询簇数，范围 [1, 65536] 且 <= nlist，默认 8
func NewMilvusIVF(fieldName string, nlist, nprobe int) *MilvusIVF {
	return &MilvusIVF{
		baseIVF: baseIVF{
			VectorField: *NewVectorField(DatabaseTypeMilvus, IndexTypeIVF, fieldName),
			Nlist:       nlist,
			Nprobe:      nprobe,
		},
	}
}

// Validate 校验 IVF 参数：Nlist > 0，Nprobe > 0，Nprobe <= Nlist。
func (iv *MilvusIVF) Validate() error {
	if iv.Nlist < 1 {
		return exception.BuildError(exception.StatusStoreVectorSchemaInvalid,
			exception.WithParam("error_msg", fmt.Sprintf("IVF Nlist must be >= 1, got %d", iv.Nlist)),
		)
	}
	if iv.Nprobe < 1 {
		return exception.BuildError(exception.StatusStoreVectorSchemaInvalid,
			exception.WithParam("error_msg", fmt.Sprintf("IVF Nprobe must be >= 1, got %d", iv.Nprobe)),
		)
	}
	if iv.Nprobe > iv.Nlist {
		return exception.BuildError(exception.StatusStoreVectorSchemaInvalid,
			exception.WithParam("error_msg", fmt.Sprintf("IVF Nprobe must be <= Nlist, got nprobe=%d, nlist=%d", iv.Nprobe, iv.Nlist)),
		)
	}
	return nil
}

// NewMilvusSCANN 创建 Milvus SCANN 索引配置。
// nlist: 构建时的簇数，默认 128
// nprobe: 搜索时的查询簇数，默认 8
// withRawData: 是否存储原始向量，默认 true
// reorderK: 搜索时重排序结果数，0 表示不重排序
func NewMilvusSCANN(fieldName string, nlist, nprobe int, withRawData bool, reorderK int) *MilvusSCANN {
	return &MilvusSCANN{
		baseIVF: baseIVF{
			VectorField: *NewVectorField(DatabaseTypeMilvus, IndexTypeSCANN, fieldName),
			Nlist:       nlist,
			Nprobe:      nprobe,
		},
		WithRawData: withRawData,
		ReorderK:    reorderK,
	}
}

// Validate 校验 SCANN 参数：继承 IVF 校验 + ReorderK 不能为负数。
func (s *MilvusSCANN) Validate() error {
	// 先校验 IVF 基类参数
	if s.Nlist < 1 {
		return exception.BuildError(exception.StatusStoreVectorSchemaInvalid,
			exception.WithParam("error_msg", fmt.Sprintf("SCANN Nlist must be >= 1, got %d", s.Nlist)),
		)
	}
	if s.Nprobe < 1 {
		return exception.BuildError(exception.StatusStoreVectorSchemaInvalid,
			exception.WithParam("error_msg", fmt.Sprintf("SCANN Nprobe must be >= 1, got %d", s.Nprobe)),
		)
	}
	if s.Nprobe > s.Nlist {
		return exception.BuildError(exception.StatusStoreVectorSchemaInvalid,
			exception.WithParam("error_msg", fmt.Sprintf("SCANN Nprobe must be <= Nlist, got nprobe=%d, nlist=%d", s.Nprobe, s.Nlist)),
		)
	}
	if s.ReorderK < 0 {
		return exception.BuildError(exception.StatusStoreVectorSchemaInvalid,
			exception.WithParam("error_msg", fmt.Sprintf("SCANN ReorderK must be >= 0, got %d", s.ReorderK)),
		)
	}
	return nil
}
```

- [ ] **Step 4: 运行测试确认通过**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/store/vector_fields/ -run "TestMilvus" -v`
Expected: PASS

- [ ] **Step 5: 提交**

```bash
cd /home/opensource/uap-claw-go && git add internal/agentcore/store/vector_fields/milvus_fields.go internal/agentcore/store/vector_fields/milvus_fields_test.go && git commit -m "feat(store/vector_fields): 添加 Milvus 索引子类型（AUTO/FLAT/HNSW/IVF/SCANN）"
```

---

### Task 3: BaseVectorStore 接口补充 3 个方法

**Files:**
- Modify: `internal/agentcore/store/vector/base.go`
- Modify: `internal/agentcore/store/vector/base_test.go`

- [ ] **Step 1: 在 base.go 中补充接口方法和 Option**

在 `BaseVectorStore` 接口末尾（`ListCollectionNames` 方法之后）添加 3 个方法：

```go
	// UpdateSchema 执行 schema 迁移操作。
	// ⤵️ 预留：实际迁移逻辑待 7.22/7.23 实现后回填。
	//
	// 对应 Python: BaseVectorStore.update_schema(collection_name, operations)
	UpdateSchema(ctx context.Context, collectionName string, operations []any, opts ...Option) error

	// UpdateCollectionMetadata 更新集合元数据。
	//
	// 对应 Python: BaseVectorStore.update_collection_metadata(collection_name, metadata)
	UpdateCollectionMetadata(ctx context.Context, collectionName string, metadata map[string]any, opts ...Option) error

	// GetCollectionMetadata 获取集合元数据。
	//
	// 对应 Python: BaseVectorStore.get_collection_metadata(collection_name)
	GetCollectionMetadata(ctx context.Context, collectionName string, opts ...Option) (map[string]any, error)
```

在 `Options` 结构体中添加 `VectorField` 字段：

```go
	// VectorField 向量索引配置，用于 CreateCollection 时指定索引参数
	VectorField any
```

添加对应的 Option 构造函数：

```go
// WithVectorField 设置向量索引配置
func WithVectorField(vf any) Option {
	return func(o *Options) { o.VectorField = vf }
}
```

- [ ] **Step 2: 在 base_test.go 中补充新接口方法的 mock**

在 `base_test.go` 中找到已有的 mock 实现并添加新方法。搜索文件中的 mock 结构体，添加：

```go
func (m *mockVectorStore) UpdateSchema(ctx context.Context, collectionName string, operations []any, opts ...Option) error {
	return nil
}

func (m *mockVectorStore) UpdateCollectionMetadata(ctx context.Context, collectionName string, metadata map[string]any, opts ...Option) error {
	return nil
}

func (m *mockVectorStore) GetCollectionMetadata(ctx context.Context, collectionName string, opts ...Option) (map[string]any, error) {
	return nil, nil
}
```

- [ ] **Step 3: 运行测试确认通过**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/store/vector/ -v -run "Test" 2>&1 | tail -20`
Expected: PASS

- [ ] **Step 4: 提交**

```bash
cd /home/opensource/uap-claw-go && git add internal/agentcore/store/vector/base.go internal/agentcore/store/vector/base_test.go && git commit -m "feat(store/vector): BaseVectorStore 接口补充 UpdateSchema/UpdateCollectionMetadata/GetCollectionMetadata"
```

---

### Task 4: MilvusVectorStore 核心实现（vector/milvus.go）

**Files:**
- Create: `internal/agentcore/store/vector/milvus.go`
- Create: `internal/agentcore/store/vector/milvus_test.go`
- Create: `internal/agentcore/store/vector/milvus_integration_test.go`

这是最大的任务，按方法分步实现。

- [ ] **Step 4.1: 添加 milvus-sdk-go/v2 依赖**

Run: `cd /home/opensource/uap-claw-go && go get github.com/milvus-io/milvus-sdk-go/v2`

- [ ] **Step 4.2: 编写 MilvusVectorStore 结构体、构造函数和 milvusClient 接口**

创建 `internal/agentcore/store/vector/milvus.go`，先写结构体和接口骨架：

```go
package vector

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"sync"

	"github.com/milvus-io/milvus-sdk-go/v2/client"
	"github.com/milvus-io/milvus-sdk-go/v2/entity"

	"github.com/uapclaw/uapclaw-go/internal/common/exception"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/store/vector_fields"
)

// ──────────────────────────── 常量 ────────────────────────────

const (
	// defaultDistanceMetric 默认距离度量方式
	defaultDistanceMetric = "COSINE"
	// defaultIndexType 默认索引类型
	defaultIndexType = "AUTOINDEX"
	// defaultBatchSize 默认批量插入大小
	defaultBatchSize = 128
)

// ──────────────────────────── 全局变量 ────────────────────────────

// logComponent 日志组件，agentcore 下统一使用 ComponentAgentCore
var logComponent = logger.ComponentAgentCore

// ──────────────────────────── 结构体 ────────────────────────────

// collMeta 集合元数据缓存
type collMeta struct {
	// DistanceMetric 距离度量类型
	DistanceMetric string
	// VectorField 向量字段名
	VectorField string
	// VectorDim 向量维度
	VectorDim int
	// SchemaVersion schema 版本
	SchemaVersion string
}

// MilvusVectorStore 基于 Milvus 的向量存储实现。
//
// 实现 BaseVectorStore 接口，使用 milvus-sdk-go/v2 作为客户端。
// 客户端惰性创建，初始化时不需要 Milvus 可用。
//
// 对应 Python: vector/milvus_vector_store.py (MilvusVectorStore)
type MilvusVectorStore struct {
	client             milvusClient
	milvusURI          string
	milvusToken        string
	dbName             string
	collectionMetadata map[string]*collMeta
	collectionsLoaded  map[string]bool
	mu                 sync.RWMutex
}

// milvusClient Milvus 客户端操作接口（用于解耦和测试）。
// 生产代码使用真实 milvus.Client，测试代码注入 fakeMilvusClient。
type milvusClient interface {
	CreateCollection(ctx context.Context, coll *entity.Collection, shardsNum int32) error
	DropCollection(ctx context.Context, collName string) error
	HasCollection(ctx context.Context, collName string) (bool, error)
	DescribeCollection(ctx context.Context, collName string) (*entity.Collection, error)
	Insert(ctx context.Context, collName string, partitionName string, columns ...entity.Column) (entity.Column, error)
	Search(ctx context.Context, collName string, partitions []string, expr string, fields []string, vectors []entity.Vector, vectorField string, metricType entity.MetricType, topK int, sp entity.SearchParam, opts ...client.SearchQueryOptionFunc) ([]client.SearchResult, error)
	Delete(ctx context.Context, collName string, partitionName string, expr string) error
	ShowCollections(ctx context.Context) ([]*entity.Collection, error)
	LoadCollection(ctx context.Context, collName string, async bool) error
	AlterCollectionProperties(ctx context.Context, collName string, props ...entity.CollectionProperty) error
	ReleaseCollection(ctx context.Context, collName string) error
	RenameCollection(ctx context.Context, oldName, newName string) error
	Flush(ctx context.Context, collName string, async bool) error
	CreateIndex(ctx context.Context, collName string, fieldName string, idx entity.Index, async bool) error
	DescribeIndex(ctx context.Context, collName string, fieldName string) ([]entity.Index, error)
	Query(ctx context.Context, collName string, partitions []string, expr string, outputFields []string, opts ...client.SearchQueryOptionFunc) ([]client.ResultSet, error)
}
```

- [ ] **Step 4.3: 实现构造函数和客户端惰性创建**

```go
// ──────────────────────────── 导出函数 ────────────────────────────

// NewMilvusVectorStore 创建 MilvusVectorStore 实例。
// 客户端惰性创建，初始化时不需要 Milvus 可用。
//
// 对应 Python: MilvusVectorStore.__init__(milvus_uri, milvus_token, database_name)
func NewMilvusVectorStore(milvusURI, milvusToken, dbName string) *MilvusVectorStore {
	if dbName == "" {
		dbName = "default"
	}
	return &MilvusVectorStore{
		milvusURI:          milvusURI,
		milvusToken:        milvusToken,
		dbName:             dbName,
		collectionMetadata: make(map[string]*collMeta),
		collectionsLoaded:  make(map[string]bool),
	}
}

// Close 关闭 Milvus 客户端连接。
// 关闭后客户端将在下次操作时重新创建。
//
// 对应 Python: MilvusVectorStore.close()
func (s *MilvusVectorStore) Close() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.client != nil {
		s.client = nil
		logger.Info(logComponent).Str("action", "close").Msg("Milvus 客户端连接已关闭")
	}
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// getClient 惰性获取或创建 Milvus 客户端。
func (s *MilvusVectorStore) getClient(ctx context.Context) (milvusClient, error) {
	s.mu.RLock()
	if s.client != nil {
		s.mu.RUnlock()
		return s.client, nil
	}
	s.mu.RUnlock()

	s.mu.Lock()
	defer s.mu.Unlock()
	// 双重检查
	if s.client != nil {
		return s.client, nil
	}

	c, err := client.NewClient(ctx, client.Config{
		Address: s.milvusURI,
		// Token:   s.milvusToken,  // 根据SDK版本调整
	})
	if err != nil {
		logger.Error(logComponent).Err(err).Str("milvus_uri", s.milvusURI).Msg("连接 Milvus 失败")
		return nil, exception.BuildError(exception.StatusStoreVectorCollectionNotFound,
			exception.WithParam("error_msg", fmt.Sprintf("failed to connect to Milvus: %v", err)),
		)
	}
	s.client = c
	logger.Info(logComponent).Str("milvus_uri", s.milvusURI).Msg("成功连接 Milvus")
	return s.client, nil
}

// ensureLoaded 确保集合已加载到内存，使用缓存避免重复加载。
//
// 对应 Python: MilvusVectorStore._ensure_loaded(collection)
func (s *MilvusVectorStore) ensureLoaded(ctx context.Context, collectionName string) error {
	s.mu.RLock()
	loaded := s.collectionsLoaded[collectionName]
	s.mu.RUnlock()
	if loaded {
		return nil
	}

	c, err := s.getClient(ctx)
	if err != nil {
		return err
	}

	has, err := c.HasCollection(ctx, collectionName)
	if err != nil {
		return err
	}
	if !has {
		return nil
	}

	logger.Info(logComponent).Str("collection_name", collectionName).Msg("正在加载集合")
	if err := c.LoadCollection(ctx, collectionName, false); err != nil {
		logger.Error(logComponent).Err(err).Str("collection_name", collectionName).Msg("加载集合失败")
		return err
	}

	s.mu.Lock()
	s.collectionsLoaded[collectionName] = true
	s.mu.Unlock()

	logger.Info(logComponent).Str("collection_name", collectionName).Msg("集合加载完成")
	return nil
}

// buildFilterExpr 从过滤条件字典构建 Milvus 过滤表达式（仅支持等值过滤）。
//
// 对应 Python: MilvusVectorStore._build_filter_expr(filters)
func buildFilterExpr(filters map[string]any) string {
	if len(filters) == 0 {
		return ""
	}
	parts := make([]string, 0, len(filters))
	for key, value := range filters {
		switch v := value.(type) {
		case string:
			parts = append(parts, fmt.Sprintf(`%s == "%s"`, key, v))
		default:
			parts = append(parts, fmt.Sprintf("%s == %v", key, v))
		}
	}
	expr := ""
	for i, p := range parts {
		if i > 0 {
			expr += " && "
		}
		expr += p
	}
	return expr
}

// mapFieldType 将 VectorDataType 映射为 Milvus DataType。
//
// 对应 Python: MilvusVectorStore._map_field_type(field_type)
func mapFieldType(dt VectorDataType) (entity.FieldType, error) {
	mapping := map[VectorDataType]entity.FieldType{
		VectorDataTypeVarchar:     entity.FieldTypeVarChar,
		VectorDataTypeFloatVector: entity.FieldTypeFloatVector,
		VectorDataTypeInt64:       entity.FieldTypeInt64,
		VectorDataTypeInt32:       entity.FieldTypeInt32,
		VectorDataTypeFloat:       entity.FieldTypeFloat,
		VectorDataTypeDouble:      entity.FieldTypeDouble,
		VectorDataTypeBool:        entity.FieldTypeBool,
		VectorDataTypeJSON:        entity.FieldTypeJSON,
	}
	milvusType, ok := mapping[dt]
	if !ok {
		return 0, exception.BuildError(exception.StatusStoreVectorSchemaInvalid,
			exception.WithParam("error_msg", fmt.Sprintf("unsupported field type: %v", dt)),
		)
	}
	return milvusType, nil
}

// mapMilvusTypeToOurType 将 Milvus DataType 映射回 VectorDataType。
//
// 对应 Python: MilvusVectorStore._map_milvus_type_to_our_type(milvus_type)
func mapMilvusTypeToOurType(milvusType entity.FieldType) VectorDataType {
	mapping := map[entity.FieldType]VectorDataType{
		entity.FieldTypeVarChar:     VectorDataTypeVarchar,
		entity.FieldTypeFloatVector: VectorDataTypeFloatVector,
		entity.FieldTypeInt64:       VectorDataTypeInt64,
		entity.FieldTypeInt32:       VectorDataTypeInt32,
		entity.FieldTypeFloat:       VectorDataTypeFloat,
		entity.FieldTypeDouble:      VectorDataTypeDouble,
		entity.FieldTypeBool:        VectorDataTypeBool,
		entity.FieldTypeJSON:        VectorDataTypeJSON,
	}
	ourType, ok := mapping[milvusType]
	if !ok {
		logger.Warn(logComponent).Str("milvus_type", milvusType.String()).Msg("不支持的 Milvus 类型，回退为 VARCHAR")
		return VectorDataTypeVarchar
	}
	return ourType
}

// getDistanceMetricType 从 Options 和缓存中获取距离度量类型。
func (s *MilvusVectorStore) getDistanceMetricType(collectionName string, opts Options) entity.MetricType {
	metricStr := opts.DistanceMetric
	if metricStr == "" {
		s.mu.RLock()
		if meta, ok := s.collectionMetadata[collectionName]; ok && meta.DistanceMetric != "" {
			metricStr = meta.DistanceMetric
		}
		s.mu.RUnlock()
	}
	if metricStr == "" {
		metricStr = defaultDistanceMetric
	}
	return mapMetricType(metricStr)
}

// mapMetricType 将字符串度量类型映射为 entity.MetricType。
func mapMetricType(metric string) entity.MetricType {
	switch metric {
	case "L2":
		return entity.L2
	case "IP":
		return entity.IP
	case "COSINE":
		return entity.COSINE
	default:
		return entity.COSINE
	}
}
```

- [ ] **Step 4.4: 实现 CreateCollection**

```go
// CreateCollection 创建集合。
// 如果集合已存在则跳过创建。schema 定义字段结构，opts 可指定 DistanceMetric 和 VectorField 索引配置。
//
// 对应 Python: MilvusVectorStore.create_collection(collection_name, schema, **kwargs)
func (s *MilvusVectorStore) CreateCollection(ctx context.Context, collectionName string, schema *CollectionSchema, opts ...Option) error {
	o := newOptions(opts...)
	c, err := s.getClient(ctx)
	if err != nil {
		return err
	}

	// 检查集合是否已存在
	has, err := c.HasCollection(ctx, collectionName)
	if err != nil {
		return err
	}
	if has {
		logger.Info(logComponent).Str("collection_name", collectionName).Msg("集合已存在，跳过创建")
		return nil
	}

	distanceMetric := o.DistanceMetric
	if distanceMetric == "" {
		distanceMetric = defaultDistanceMetric
	}
	distanceMetric = strings.ToUpper(distanceMetric)

	// 构建 Milvus schema
	milvusSchema := &entity.Schema{
		CollectionName:   collectionName,
		Description:      schema.Description,
		EnableDynamicField: schema.EnableDynamicField,
	}

	var vectorFieldName string
	var vectorDim int

	for _, field := range schema.Fields() {
		milvusType, err := mapFieldType(field.DType)
		if err != nil {
			return err
		}

		var milvusField *entity.Field
		if milvusType == entity.FieldTypeFloatVector {
			vectorFieldName = field.Name
			vectorDim = field.Dim
			if vectorDim == 0 {
				return exception.BuildError(exception.StatusStoreVectorSchemaInvalid,
					exception.WithParam("error_msg", fmt.Sprintf("dim of vector field is missing, field=%s", field.Name)),
				)
			}
			milvusField = entity.NewVectorField(field.Name, vectorDim, nil)
		} else if milvusType == entity.FieldTypeVarChar {
			maxLen := field.MaxLength
			if maxLen == 0 {
				maxLen = defaultMaxLength
			}
			milvusField = entity.NewField(field.Name, milvusType, field.IsPrimary, field.AutoID,
				entity.WithMaxLength(maxLen))
		} else if milvusType == entity.FieldTypeJSON {
			milvusField = entity.NewField(field.Name, milvusType, field.IsPrimary, field.AutoID)
		} else {
			milvusField = entity.NewField(field.Name, milvusType, field.IsPrimary, field.AutoID)
		}
		milvusSchema.Fields = append(milvusSchema.Fields, milvusField)
	}

	if vectorFieldName == "" {
		return exception.BuildError(exception.StatusStoreVectorSchemaInvalid,
			exception.WithParam("error_msg", "schema must contain at least one FLOAT_VECTOR field"),
		)
	}

	// 创建集合
	if err := c.CreateCollection(ctx, milvusSchema, 1); err != nil {
		logger.Error(logComponent).Err(err).Str("collection_name", collectionName).Msg("创建集合失败")
		return err
	}

	// 创建向量索引
	idx, err := s.buildIndexParams(vectorFieldName, distanceMetric, o)
	if err != nil {
		return err
	}
	if err := c.CreateIndex(ctx, collectionName, vectorFieldName, idx, false); err != nil {
		logger.Error(logComponent).Err(err).Str("collection_name", collectionName).Msg("创建索引失败")
		return err
	}

	// 为 VARCHAR 和 INT 标量字段创建倒排索引（用于过滤）
	for _, field := range schema.Fields() {
		if field.IsPrimary {
			continue
		}
		if field.DType == VectorDataTypeVarchar || field.DType == VectorDataTypeInt64 || field.DType == VectorDataTypeInt32 {
			scalarIdx := entity.NewScalarIndexWithType(entity.IndexTypeInverted)
			_ = c.CreateIndex(ctx, collectionName, field.Name, scalarIdx, false)
		}
	}

	// 加载集合
	if err := c.LoadCollection(ctx, collectionName, false); err != nil {
		logger.Error(logComponent).Err(err).Str("collection_name", collectionName).Msg("加载集合失败")
		return err
	}

	// 缓存集合元数据
	s.mu.Lock()
	s.collectionMetadata[collectionName] = &collMeta{
		DistanceMetric: distanceMetric,
		VectorField:    vectorFieldName,
		VectorDim:      vectorDim,
	}
	s.collectionsLoaded[collectionName] = true
	s.mu.Unlock()

	logger.Info(logComponent).Str("collection_name", collectionName).
		Int("field_count", len(schema.Fields())).
		Msg("成功创建集合并加载")
	return nil
}

// buildIndexParams 根据 VectorField 配置构建索引参数。
func (s *MilvusVectorStore) buildIndexParams(vectorFieldName, distanceMetric string, o Options) (entity.Index, error) {
	metricType := mapMetricType(distanceMetric)

	// 如果有 VectorField 配置，使用其参数
	if o.VectorField != nil {
		if vf, ok := o.VectorField.(*vector_fields.MilvusAUTO); ok {
			_ = vf
			return entity.NewGenericIndex(vectorFieldName, entity.MAP{}, metricType), nil
		}
		if vf, ok := o.VectorField.(*vector_fields.MilvusFLAT); ok {
			_ = vf
			return entity.NewFlatIndex(vectorFieldName, metricType), nil
		}
		if vf, ok := o.VectorField.(*vector_fields.MilvusHNSW); ok {
			constructParams := vector_fields.ToDict(vf, vector_fields.StageConstruct)
			return entity.NewHNSWIndex(entity.MAP{
				"M":             constructParams["M"],
				"efConstruction": constructParams["EfConstruction"],
			}, metricType), nil
		}
		if vf, ok := o.VectorField.(*vector_fields.MilvusIVF); ok {
			constructParams := vector_fields.ToDict(vf, vector_fields.StageConstruct)
			return entity.NewIvfFlatIndex(entity.MAP{
				"nlist": constructParams["Nlist"],
			}, metricType), nil
		}
		if vf, ok := o.VectorField.(*vector_fields.MilvusSCANN); ok {
			constructParams := vector_fields.ToDict(vf, vector_fields.StageConstruct)
			return entity.NewIvfFlatIndex(entity.MAP{
				"nlist": constructParams["Nlist"],
			}, metricType), nil
		}
	}

	// 默认使用 AUTOINDEX
	return entity.NewGenericIndex(vectorFieldName, entity.MAP{}, metricType), nil
}
```

- [ ] **Step 4.5: 实现 DeleteCollection、CollectionExists、ListCollectionNames**

```go
// DeleteCollection 删除集合。
//
// 对应 Python: MilvusVectorStore.delete_collection(collection_name)
func (s *MilvusVectorStore) DeleteCollection(ctx context.Context, collectionName string, opts ...Option) error {
	c, err := s.getClient(ctx)
	if err != nil {
		return err
	}

	has, err := c.HasCollection(ctx, collectionName)
	if err != nil {
		return err
	}
	if !has {
		logger.Warn(logComponent).Str("collection_name", collectionName).Msg("集合不存在，跳过删除")
		return nil
	}

	if err := c.DropCollection(ctx, collectionName); err != nil {
		logger.Error(logComponent).Err(err).Str("collection_name", collectionName).Msg("删除集合失败")
		return err
	}

	// 清除缓存
	s.mu.Lock()
	delete(s.collectionMetadata, collectionName)
	delete(s.collectionsLoaded, collectionName)
	s.mu.Unlock()

	logger.Info(logComponent).Str("collection_name", collectionName).Msg("成功删除集合")
	return nil
}

// CollectionExists 检查集合是否存在。
//
// 对应 Python: MilvusVectorStore.collection_exists(collection_name)
func (s *MilvusVectorStore) CollectionExists(ctx context.Context, collectionName string, opts ...Option) (bool, error) {
	c, err := s.getClient(ctx)
	if err != nil {
		return false, err
	}
	return c.HasCollection(ctx, collectionName)
}

// ListCollectionNames 列出所有集合名称。
//
// 对应 Python: MilvusVectorStore.list_collection_names()
func (s *MilvusVectorStore) ListCollectionNames(ctx context.Context) ([]string, error) {
	c, err := s.getClient(ctx)
	if err != nil {
		return nil, err
	}
	colls, err := c.ShowCollections(ctx)
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, len(colls))
	for _, coll := range colls {
		names = append(names, coll.Name)
	}
	return names, nil
}
```

- [ ] **Step 4.6: 实现 GetSchema**

```go
// GetSchema 获取集合的 Schema。
//
// 对应 Python: MilvusVectorStore.get_schema(collection_name)
func (s *MilvusVectorStore) GetSchema(ctx context.Context, collectionName string, opts ...Option) (*CollectionSchema, error) {
	c, err := s.getClient(ctx)
	if err != nil {
		return nil, err
	}

	has, err := c.HasCollection(ctx, collectionName)
	if err != nil {
		return nil, err
	}
	if !has {
		return nil, exception.BuildError(exception.StatusStoreVectorCollectionNotFound,
			exception.WithParam("collection_name", collectionName),
		)
	}

	collInfo, err := c.DescribeCollection(ctx, collectionName)
	if err != nil {
		logger.Error(logComponent).Err(err).Str("collection_name", collectionName).Msg("获取集合 Schema 失败")
		return nil, err
	}

	schema, err := NewCollectionSchema(
		WithCollectionDescription(collInfo.Schema.Description),
	)
	if err != nil {
		return nil, err
	}
	if collInfo.Schema.EnableDynamicField {
		schema.EnableDynamicField = true
	}

	for _, milvusField := range collInfo.Schema.Fields {
		ourType := mapMilvusTypeToOurType(milvusField.DataType)
		fieldOpts := []FieldOption{}
		if milvusField.PrimaryKey {
			fieldOpts = append(fieldOpts, WithPrimary())
		}
		if milvusField.AutoID {
			fieldOpts = append(fieldOpts, WithAutoID())
		}
		if milvusField.TypeParams != nil {
			if dim, ok := milvusField.TypeParams["dim"]; ok {
				if d, err := strconv.Atoi(dim); err == nil && d > 0 {
					fieldOpts = append(fieldOpts, WithDim(d))
				}
			}
			if maxLen, ok := milvusField.TypeParams["max_length"]; ok {
				if ml, err := strconv.Atoi(maxLen); err == nil {
					fieldOpts = append(fieldOpts, WithMaxLength(ml))
				}
			}
		}
		if milvusField.Name != "" {
			fieldOpts = append(fieldOpts, WithFieldDescription(milvusField.Name))
		}

		f, err := NewFieldSchema(milvusField.Name, ourType, fieldOpts...)
		if err != nil {
			return nil, err
		}
		if _, err := schema.AddField(f); err != nil {
			return nil, err
		}
	}

	return schema, nil
}
```

> 注：需在 milvus.go 顶部 import 中添加 `"strconv"` 和 `"strings"`。

- [ ] **Step 4.7: 实现 AddDocs**

```go
// AddDocs 添加文档到集合。支持批量插入，通过 BatchSize 控制批次大小。
//
// 对应 Python: MilvusVectorStore.add_docs(collection_name, docs, **kwargs)
func (s *MilvusVectorStore) AddDocs(ctx context.Context, collectionName string, docs []map[string]any, opts ...Option) error {
	if len(docs) == 0 {
		return nil
	}

	o := newOptions(opts...)
	batchSize := o.BatchSize
	if batchSize <= 0 {
		batchSize = defaultBatchSize
	}

	if err := s.ensureLoaded(ctx, collectionName); err != nil {
		return err
	}

	c, err := s.getClient(ctx)
	if err != nil {
		return err
	}

	total := len(docs)
	processed := 0

	for i := 0; i < total; i += batchSize {
		end := i + batchSize
		if end > total {
			end = total
		}
		batch := docs[i:end]

		// 将 map 转为 entity.Column 列表
		columns, err := s.docsToColumns(batch, collectionName)
		if err != nil {
			return err
		}

		_, err = c.Insert(ctx, collectionName, "", columns...)
		if err != nil {
			logger.Error(logComponent).Err(err).Str("collection_name", collectionName).
				Int("batch_start", i).Int("batch_size", len(batch)).Msg("插入文档批次失败")
			return err
		}

		processed += len(batch)
		if processed%100 == 0 {
			logger.Info(logComponent).Str("collection_name", collectionName).
				Int("processed", processed).Int("total", total).Msg("已添加文档到集合")
		}
	}

	// 刷新确保持久化
	if err := c.Flush(ctx, collectionName, false); err != nil {
		logger.Warn(logComponent).Err(err).Str("collection_name", collectionName).Msg("Flush 失败")
	}

	logger.Info(logComponent).Str("collection_name", collectionName).
		Int("total", total).Msg("成功添加文档到集合")
	return nil
}

// docsToColumns 将文档列表转换为 Milvus 列格式。
// 根据集合 schema 推断列类型。
func (s *MilvusVectorStore) docsToColumns(docs []map[string]any, collectionName string) ([]entity.Column, error) {
	if len(docs) == 0 {
		return nil, nil
	}

	// 从缓存或 schema 获取字段信息
	schema, err := s.getCachedSchema(collectionName)
	if err != nil {
		// fallback：直接从文档推断字段
		return s.docsToColumnsFromDocs(docs)
	}

	columnsMap := make(map[string][]any)
	fieldTypes := make(map[string]VectorDataType)

	// 初始化列
	for _, f := range schema.Fields() {
		columnsMap[f.Name] = make([]any, 0, len(docs))
		fieldTypes[f.Name] = f.DType
	}

	// 收集数据
	for _, doc := range docs {
		for fieldName := range columnsMap {
			val, ok := doc[fieldName]
			if ok {
				columnsMap[fieldName] = append(columnsMap[fieldName], val)
			} else {
				columnsMap[fieldName] = append(columnsMap[fieldName], nil)
			}
		}
	}

	// 转换为 entity.Column
	result := make([]entity.Column, 0, len(columnsMap))
	for fieldName, values := range columnsMap {
		col, err := s.valuesToColumn(fieldName, fieldTypes[fieldName], values)
		if err != nil {
			return nil, err
		}
		if col != nil {
			result = append(result, col)
		}
	}

	return result, nil
}

// getCachedSchema 尝试从缓存获取 schema，缓存未命中则从 Milvus 获取。
func (s *MilvusVectorStore) getCachedSchema(collectionName string) (*CollectionSchema, error) {
	// 这里简化处理，实际可以加 schema 缓存
	// 暂时返回 nil 让调用者 fallback
	return nil, exception.BuildError(exception.StatusStoreVectorCollectionNotFound,
		exception.WithParam("collection_name", collectionName),
	)
}

// docsToColumnsFromDocs 从文档内容推断列类型并转换（fallback 方式）。
func (s *MilvusVectorStore) docsToColumnsFromDocs(docs []map[string]any) ([]entity.Column, error) {
	if len(docs) == 0 {
		return nil, nil
	}

	// 收集所有字段名
	fieldNames := make(map[string]bool)
	for _, doc := range docs {
		for k := range doc {
			fieldNames[k] = true
		}
	}

	columnsMap := make(map[string][]any)
	for name := range fieldNames {
		columnsMap[name] = make([]any, 0, len(docs))
	}

	for _, doc := range docs {
		for name := range fieldNames {
			val, ok := doc[name]
			if ok {
				columnsMap[name] = append(columnsMap[name], val)
			} else {
				columnsMap[name] = append(columnsMap[name], nil)
			}
		}
	}

	result := make([]entity.Column, 0, len(columnsMap))
	for fieldName, values := range columnsMap {
		col := s.inferColumn(fieldName, values)
		if col != nil {
			result = append(result, col)
		}
	}
	return result, nil
}

// inferColumn 从值推断列类型并创建 entity.Column。
func (s *MilvusVectorStore) inferColumn(fieldName string, values []any) entity.Column {
	if len(values) == 0 {
		return nil
	}

	// 检查第一个非 nil 值的类型
	for _, v := range values {
		if v == nil {
			continue
		}
		switch v.(type) {
		case []float64:
			// 向量字段
			vecs := make([][]float32, 0, len(values))
			for _, val := range values {
				if f64, ok := val.([]float64); ok {
					f32 := make([]float32, len(f64))
					for i, f := range f64 {
						f32[i] = float32(f)
					}
					vecs = append(vecs, f32)
				}
			}
			col, err := entity.NewColumnFloatVector(fieldName, len(vecs[0]), vecs)
			if err != nil {
				return nil
			}
			return col
		case string:
			strs := make([]string, 0, len(values))
			for _, val := range values {
				if s, ok := val.(string); ok {
					strs = append(strs, s)
				} else {
					strs = append(strs, "")
				}
			}
			return entity.NewColumnVarChar(fieldName, strs)
		case int64:
			ints := make([]int64, 0, len(values))
			for _, val := range values {
				if i, ok := val.(int64); ok {
					ints = append(ints, i)
				} else {
					ints = append(ints, 0)
				}
			}
			return entity.NewColumnInt64(fieldName, ints)
		case int:
			ints := make([]int64, 0, len(values))
			for _, val := range values {
				if i, ok := val.(int); ok {
					ints = append(ints, int64(i))
				} else {
					ints = append(ints, 0)
				}
			}
			return entity.NewColumnInt64(fieldName, ints)
		}
	}
	return nil
}

// valuesToColumn 按指定类型将值列表转为 entity.Column。
func (s *MilvusVectorStore) valuesToColumn(fieldName string, dt VectorDataType, values []any) (entity.Column, error) {
	switch dt {
	case VectorDataTypeFloatVector:
		vecs := make([][]float32, 0, len(values))
		dim := 0
		for _, v := range values {
			if v == nil {
				if dim > 0 {
					vecs = append(vecs, make([]float32, dim))
				}
				continue
			}
			if f64, ok := v.([]float64); ok {
				dim = len(f64)
				f32 := make([]float32, len(f64))
				for i, f := range f64 {
					f32[i] = float32(f)
				}
				vecs = append(vecs, f32)
			}
		}
		if dim == 0 || len(vecs) == 0 {
			return nil, nil
		}
		return entity.NewColumnFloatVector(fieldName, dim, vecs)
	case VectorDataTypeVarchar:
		strs := make([]string, 0, len(values))
		for _, v := range values {
			if s, ok := v.(string); ok {
				strs = append(strs, s)
			} else {
				strs = append(strs, "")
			}
		}
		return entity.NewColumnVarChar(fieldName, strs), nil
	case VectorDataTypeInt64:
		ints := make([]int64, 0, len(values))
		for _, v := range values {
			switch n := v.(type) {
			case int64:
				ints = append(ints, n)
			case int:
				ints = append(ints, int64(n))
			case float64:
				ints = append(ints, int64(n))
			default:
				ints = append(ints, 0)
			}
		}
		return entity.NewColumnInt64(fieldName, ints), nil
	default:
		// 其他类型 fallback
		return s.inferColumn(fieldName, values), nil
	}
}
```

> 注：`entity.NewColumnFloatVector` 返回 `*entity.ColumnFloatVector, error`，`entity.NewColumnVarChar` 返回 `*entity.ColumnVarChar`，`entity.NewColumnInt64` 返回 `*entity.ColumnInt64`，需要根据 SDK 实际 API 调整返回签名。

- [ ] **Step 4.8: 实现 Search**

```go
// Search 向量相似度搜索。
// 返回按相似度排序的搜索结果列表，原始距离/分数已根据度量类型转换为 [0, 1] 归一化相似度。
//
// 对应 Python: MilvusVectorStore.search(collection_name, query_vector, vector_field, top_k, filters, **kwargs)
func (s *MilvusVectorStore) Search(ctx context.Context, collectionName string, queryVector []float64, vectorField string, topK int, filters map[string]any, opts ...Option) ([]VectorSearchResult, error) {
	o := newOptions(opts...)
	if topK <= 0 {
		topK = 5
	}

	if err := s.ensureLoaded(ctx, collectionName); err != nil {
		return nil, err
	}

	c, err := s.getClient(ctx)
	if err != nil {
		return nil, err
	}

	metricType := s.getDistanceMetricType(collectionName, o)

	// 构建过滤表达式
	expr := buildFilterExpr(filters)

	// 构建查询向量
	vecFloat32 := make([]float32, len(queryVector))
	for i, v := range queryVector {
		vecFloat32[i] = float32(v)
	}
	vectors := []entity.Vector{entity.FloatVector(vecFloat32)}

	// 确定输出字段
	outputFields := o.OutputFields

	// 构建搜索参数
	sp, err := s.buildSearchParams(collectionName, o)
	if err != nil {
		return nil, err
	}

	results, err := c.Search(ctx, collectionName, nil, expr, outputFields, vectors, vectorField, metricType, topK, sp)
	if err != nil {
		logger.Error(logComponent).Err(err).Str("collection_name", collectionName).Msg("向量搜索失败")
		return nil, err
	}

	// 转换结果
	searchResults := make([]VectorSearchResult, 0)
	for _, result := range results {
		for j := 0; j < result.ResultCount; j++ {
			score := result.Scores[j]
			// 根据度量类型转换分数
			normalizedScore := s.normalizeScore(score, metricType)

			fields := make(map[string]any)
			for _, col := range result.Fields {
				fields[col.Name()] = col.FieldData().GetScalars().GetData()
			}

			searchResults = append(searchResults, VectorSearchResult{
				Score:  normalizedScore,
				Fields: fields,
			})
		}
	}

	logger.Info(logComponent).Str("collection_name", collectionName).
		Int("result_count", len(searchResults)).Msg("向量搜索完成")
	return searchResults, nil
}

// normalizeScore 根据度量类型将原始分数转换为归一化相似度 [0, 1]。
func (s *MilvusVectorStore) normalizeScore(rawScore float64, metricType entity.MetricType) float64 {
	switch metricType {
	case entity.COSINE:
		return ConvertCosineSimilarity(rawScore)
	case entity.L2:
		return ConvertL2Squared(rawScore, 4.0)
	case entity.IP:
		return ConvertIPSimilarity(rawScore)
	default:
		return ConvertCosineSimilarity(rawScore)
	}
}

// buildSearchParams 根据配置构建搜索参数。
func (s *MilvusVectorStore) buildSearchParams(collectionName string, o Options) (entity.SearchParam, error) {
	if o.VectorField != nil {
		if vf, ok := o.VectorField.(*vector_fields.MilvusHNSW); ok {
			searchParams := vector_fields.ToDict(vf, vector_fields.StageSearch)
			ef := 0
			if efVal, ok := searchParams["EfSearchFactor"]; ok {
				if efFloat, ok := efVal.(float64); ok {
					ef = int(efFloat)
				}
			}
			return entity.NewIndexHNSWSearchParam(ef), nil
		}
		if vf, ok := o.VectorField.(*vector_fields.MilvusIVF); ok {
			searchParams := vector_fields.ToDict(vf, vector_fields.StageSearch)
			nprobe := 8
			if nprobeVal, ok := searchParams["Nprobe"]; ok {
				if nprobeInt, ok := nprobeVal.(int); ok {
					nprobe = nprobeInt
				}
			}
			return entity.NewIndexIvfSQ8SearchParam(nprobe), nil
		}
		if vf, ok := o.VectorField.(*vector_fields.MilvusSCANN); ok {
			searchParams := vector_fields.ToDict(vf, vector_fields.StageSearch)
			nprobe := 8
			if nprobeVal, ok := searchParams["Nprobe"]; ok {
				if nprobeInt, ok := nprobeVal.(int); ok {
					nprobe = nprobeInt
				}
			}
			return entity.NewIndexIvfSQ8SearchParam(nprobe), nil
		}
	}
	// 默认搜索参数
	return entity.NewIndexFlatSearchParam(), nil
}
```

- [ ] **Step 4.9: 实现 DeleteDocsByIDs 和 DeleteDocsByFilters**

```go
// DeleteDocsByIDs 按 ID 删除文档。
//
// 对应 Python: MilvusVectorStore.delete_docs_by_ids(collection_name, ids)
func (s *MilvusVectorStore) DeleteDocsByIDs(ctx context.Context, collectionName string, ids []string, opts ...Option) error {
	if len(ids) == 0 {
		logger.Warn(logComponent).Str("collection_name", collectionName).Msg("未提供删除 ID")
		return nil
	}

	if err := s.ensureLoaded(ctx, collectionName); err != nil {
		return err
	}

	c, err := s.getClient(ctx)
	if err != nil {
		return err
	}

	// 构建 ID 过滤表达式
	expr := fmt.Sprintf("id in [%s]", s.joinIDs(ids))

	if err := c.Delete(ctx, collectionName, "", expr); err != nil {
		logger.Error(logComponent).Err(err).Str("collection_name", collectionName).
			Int("id_count", len(ids)).Msg("按 ID 删除文档失败")
		return err
	}

	if err := c.Flush(ctx, collectionName, false); err != nil {
		logger.Warn(logComponent).Err(err).Str("collection_name", collectionName).Msg("Flush 失败")
	}

	logger.Info(logComponent).Str("collection_name", collectionName).
		Int("id_count", len(ids)).Msg("成功按 ID 删除文档")
	return nil
}

// DeleteDocsByFilters 按标量字段过滤条件删除文档。
//
// 对应 Python: MilvusVectorStore.delete_docs_by_filters(collection_name, filters)
func (s *MilvusVectorStore) DeleteDocsByFilters(ctx context.Context, collectionName string, filters map[string]any, opts ...Option) error {
	if len(filters) == 0 {
		logger.Warn(logComponent).Str("collection_name", collectionName).Msg("未提供过滤条件")
		return nil
	}

	if err := s.ensureLoaded(ctx, collectionName); err != nil {
		return err
	}

	c, err := s.getClient(ctx)
	if err != nil {
		return err
	}

	expr := buildFilterExpr(filters)
	if expr == "" {
		return nil
	}

	if err := c.Delete(ctx, collectionName, "", expr); err != nil {
		logger.Error(logComponent).Err(err).Str("collection_name", collectionName).
			Str("filter_expr", expr).Msg("按过滤条件删除文档失败")
		return err
	}

	if err := c.Flush(ctx, collectionName, false); err != nil {
		logger.Warn(logComponent).Err(err).Str("collection_name", collectionName).Msg("Flush 失败")
	}

	logger.Info(logComponent).Str("collection_name", collectionName).
		Str("filter_expr", expr).Msg("成功按过滤条件删除文档")
	return nil
}

// joinIDs 将 ID 列表拼接为 Milvus 表达式中的 ID 字符串。
func (s *MilvusVectorStore) joinIDs(ids []string) string {
	parts := make([]string, 0, len(ids))
	for _, id := range ids {
		parts = append(parts, fmt.Sprintf(`"%s"`, id))
	}
	result := ""
	for i, p := range parts {
		if i > 0 {
			result += ", "
		}
		result += p
	}
	return result
}
```

- [ ] **Step 4.10: 实现 UpdateSchema（预留）、UpdateCollectionMetadata、GetCollectionMetadata**

```go
// UpdateSchema 执行 schema 迁移操作。
// ⤵️ 预留：实际迁移逻辑待 7.22/7.23 实现后回填。
//
// 对应 Python: MilvusVectorStore.update_schema(collection_name, operations)
func (s *MilvusVectorStore) UpdateSchema(ctx context.Context, collectionName string, operations []any, opts ...Option) error {
	// TODO: ⤵️ 回填，待 7.22/7.23 实现后补全
	logger.Warn(logComponent).Str("collection_name", collectionName).Msg("UpdateSchema 尚未实现，待 7.22/7.23 回填")
	return exception.BuildError(exception.StatusStoreVectorSchemaInvalid,
		exception.WithParam("error_msg", "UpdateSchema is not yet implemented, pending 7.22/7.23"),
	)
}

// UpdateCollectionMetadata 更新集合元数据。
// 同时更新 Milvus 集合属性和本地缓存。
//
// 对应 Python: MilvusVectorStore.update_collection_metadata(collection_name, metadata)
func (s *MilvusVectorStore) UpdateCollectionMetadata(ctx context.Context, collectionName string, metadata map[string]any, opts ...Option) error {
	if len(metadata) == 0 {
		return nil
	}

	// 校验 schema_version
	if v, ok := metadata["schema_version"]; ok {
		switch sv := v.(type) {
		case int:
			if sv < 0 {
				return exception.BuildError(exception.StatusStoreVectorSchemaInvalid,
					exception.WithParam("error_msg", fmt.Sprintf("schema_version must be non-negative, got %d", sv)),
				)
			}
		default:
			return exception.BuildError(exception.StatusStoreVectorSchemaInvalid,
				exception.WithParam("error_msg", fmt.Sprintf("schema_version must be int, got %T", v)),
			)
		}
	}

	c, err := s.getClient(ctx)
	if err != nil {
		return err
	}

	// 检查集合是否存在
	has, err := c.HasCollection(ctx, collectionName)
	if err != nil {
		return err
	}
	if !has {
		return exception.BuildError(exception.StatusStoreVectorCollectionNotFound,
			exception.WithParam("collection_name", collectionName),
		)
	}

	// 构建 collection properties
	props := make([]entity.CollectionProperty, 0, len(metadata))
	for key, value := range metadata {
		props = append(props, entity.CollectionProperty{
			Key:   key,
			Value: fmt.Sprintf("%v", value),
		})
	}

	if err := c.AlterCollectionProperties(ctx, collectionName, props...); err != nil {
		logger.Error(logComponent).Err(err).Str("collection_name", collectionName).Msg("更新集合元数据失败")
		return err
	}

	// 更新本地缓存
	s.mu.Lock()
	if meta, ok := s.collectionMetadata[collectionName]; ok {
		if v, ok := metadata["schema_version"]; ok {
			meta.SchemaVersion = fmt.Sprintf("%v", v)
		}
		if v, ok := metadata["distance_metric"]; ok {
			if str, ok := v.(string); ok {
				meta.DistanceMetric = str
			}
		}
	}
	s.mu.Unlock()

	logger.Info(logComponent).Str("collection_name", collectionName).Msg("成功更新集合元数据")
	return nil
}

// GetCollectionMetadata 获取集合元数据。
// 优先从缓存获取，缓存未命中则从 Milvus 获取。
//
// 对应 Python: MilvusVectorStore.get_collection_metadata(collection_name)
func (s *MilvusVectorStore) GetCollectionMetadata(ctx context.Context, collectionName string, opts ...Option) (map[string]any, error) {
	s.mu.RLock()
	if meta, ok := s.collectionMetadata[collectionName]; ok {
		s.mu.RUnlock()
		result := map[string]any{
			"distance_metric": meta.DistanceMetric,
			"vector_field":    meta.VectorField,
			"vector_dim":      meta.VectorDim,
			"schema_version":  meta.SchemaVersion,
		}
		return result, nil
	}
	s.mu.RUnlock()

	// 缓存未命中，从 Milvus 获取
	c, err := s.getClient(ctx)
	if err != nil {
		return nil, err
	}

	collInfo, err := c.DescribeCollection(ctx, collectionName)
	if err != nil {
		logger.Warn(logComponent).Err(err).Str("collection_name", collectionName).
			Msg("获取集合描述失败，回退默认值")
		return map[string]any{
			"distance_metric": defaultDistanceMetric,
			"schema_version":  "0",
		}, nil
	}

	// 提取向量字段名和索引信息
	var vectorFieldName string
	for _, f := range collInfo.Schema.Fields {
		if f.DataType == entity.FieldTypeFloatVector {
			vectorFieldName = f.Name
			break
		}
	}

	metadata := map[string]any{
		"distance_metric": defaultDistanceMetric,
		"vector_field":    vectorFieldName,
	}

	// 尝试获取索引信息以确定度量类型
	if vectorFieldName != "" {
		indexes, err := c.DescribeIndex(ctx, collectionName, vectorFieldName)
		if err == nil && len(indexes) > 0 {
			metadata["distance_metric"] = indexes[0].Params()["metric_type"]
		}
	}

	// 获取 schema_version
	schemaVersion := "0"
	if collInfo.Properties != nil {
		if v, ok := collInfo.Properties["schema_version"]; ok {
			schemaVersion = v
		}
	}
	metadata["schema_version"] = schemaVersion

	// 更新缓存
	s.mu.Lock()
	s.collectionMetadata[collectionName] = &collMeta{
		DistanceMetric: metadata["distance_metric"].(string),
		VectorField:    vectorFieldName,
		SchemaVersion:  schemaVersion,
	}
	s.mu.Unlock()

	return metadata, nil
}
```

- [ ] **Step 4.11: 编写 MilvusVectorStore 单元测试**

创建 `internal/agentcore/store/vector/milvus_test.go`，使用 fakeMilvusClient：

```go
package vector

import (
	"context"
	"testing"

	"github.com/milvus-io/milvus-sdk-go/v2/entity"
)

// ──────────────────────────── 结构体 ────────────────────────────

// fakeMilvusClient 用于测试的 Milvus 客户端模拟
type fakeMilvusClient struct {
	collections map[string]bool
	schemas     map[string]*entity.Schema
}

func newFakeMilvusClient() *fakeMilvusClient {
	return &fakeMilvusClient{
		collections: make(map[string]bool),
		schemas:     make(map[string]*entity.Schema),
	}
}

// 实现 milvusClient 接口的所有方法（省略，每个方法用简单逻辑模拟）
func (f *fakeMilvusClient) CreateCollection(ctx context.Context, coll *entity.Collection, shardsNum int32) error {
	f.collections[coll.Name] = true
	f.schemas[coll.Name] = coll.Schema
	return nil
}

func (f *fakeMilvusClient) DropCollection(ctx context.Context, collName string) error {
	delete(f.collections, collName)
	delete(f.schemas, collName)
	return nil
}

func (f *fakeMilvusClient) HasCollection(ctx context.Context, collName string) (bool, error) {
	return f.collections[collName], nil
}

func (f *fakeMilvusClient) DescribeCollection(ctx context.Context, collName string) (*entity.Collection, error) {
	if !f.collections[collName] {
		return nil, fmt.Errorf("collection not found")
	}
	return &entity.Collection{Name: collName, Schema: f.schemas[collName]}, nil
}

func (f *fakeMilvusClient) Insert(ctx context.Context, collName string, partitionName string, columns ...entity.Column) (entity.Column, error) {
	return nil, nil
}

func (f *fakeMilvusClient) Search(ctx context.Context, collName string, partitions []string, expr string, fields []string, vectors []entity.Vector, vectorField string, metricType entity.MetricType, topK int, sp entity.SearchParam, opts ...client.SearchQueryOptionFunc) ([]client.SearchResult, error) {
	return nil, nil
}

func (f *fakeMilvusClient) Delete(ctx context.Context, collName string, partitionName string, expr string) error {
	return nil
}

func (f *fakeMilvusClient) ShowCollections(ctx context.Context) ([]*entity.Collection, error) {
	result := make([]*entity.Collection, 0, len(f.collections))
	for name := range f.collections {
		result = append(result, &entity.Collection{Name: name})
	}
	return result, nil
}

func (f *fakeMilvusClient) LoadCollection(ctx context.Context, collName string, async bool) error {
	return nil
}

func (f *fakeMilvusClient) AlterCollectionProperties(ctx context.Context, collName string, props ...entity.CollectionProperty) error {
	return nil
}

func (f *fakeMilvusClient) ReleaseCollection(ctx context.Context, collName string) error {
	return nil
}

func (f *fakeMilvusClient) RenameCollection(ctx context.Context, oldName, newName string) error {
	return nil
}

func (f *fakeMilvusClient) Flush(ctx context.Context, collName string, async bool) error {
	return nil
}

func (f *fakeMilvusClient) CreateIndex(ctx context.Context, collName string, fieldName string, idx entity.Index, async bool) error {
	return nil
}

func (f *fakeMilvusClient) DescribeIndex(ctx context.Context, collName string, fieldName string) ([]entity.Index, error) {
	return nil, nil
}

func (f *fakeMilvusClient) Query(ctx context.Context, collName string, partitions []string, expr string, outputFields []string, opts ...client.SearchQueryOptionFunc) ([]client.ResultSet, error) {
	return nil, nil
}

// ──────────────────────────── 导出函数 ────────────────────────────

func TestNewMilvusVectorStore(t *testing.T) {
	s := NewMilvusVectorStore("http://localhost:19530", "", "default")
	if s.milvusURI != "http://localhost:19530" {
		t.Errorf("milvusURI = %v, want http://localhost:19530", s.milvusURI)
	}
	if s.dbName != "default" {
		t.Errorf("dbName = %v, want default", s.dbName)
	}
}

func TestNewMilvusVectorStore_默认数据库名(t *testing.T) {
	s := NewMilvusVectorStore("http://localhost:19530", "", "")
	if s.dbName != "default" {
		t.Errorf("dbName = %v, want default", s.dbName)
	}
}

func TestMilvusVectorStore_CreateCollection(t *testing.T) {
	s := NewMilvusVectorStore("http://localhost:19530", "", "default")
	fake := newFakeMilvusClient()
	s.client = fake

	schema := createTestSchema()
	ctx := context.Background()

	err := s.CreateCollection(ctx, "test_coll", schema, WithDistanceMetric("COSINE"))
	if err != nil {
		t.Fatalf("CreateCollection() error = %v", err)
	}

	has, _ := fake.HasCollection(ctx, "test_coll")
	if !has {
		t.Error("集合应该已创建")
	}
}

func TestMilvusVectorStore_CreateCollection_已存在(t *testing.T) {
	s := NewMilvusVectorStore("http://localhost:19530", "", "default")
	fake := newFakeMilvusClient()
	s.client = fake

	fake.collections["test_coll"] = true
	schema := createTestSchema()
	ctx := context.Background()

	err := s.CreateCollection(ctx, "test_coll", schema)
	if err != nil {
		t.Fatalf("CreateCollection() 已存在时应返回 nil, error = %v", err)
	}
}

func TestMilvusVectorStore_DeleteCollection(t *testing.T) {
	s := NewMilvusVectorStore("http://localhost:19530", "", "default")
	fake := newFakeMilvusClient()
	s.client = fake

	fake.collections["test_coll"] = true
	ctx := context.Background()

	err := s.DeleteCollection(ctx, "test_coll")
	if err != nil {
		t.Fatalf("DeleteCollection() error = %v", err)
	}

	has, _ := fake.HasCollection(ctx, "test_coll")
	if has {
		t.Error("集合应该已删除")
	}
}

func TestMilvusVectorStore_CollectionExists(t *testing.T) {
	s := NewMilvusVectorStore("http://localhost:19530", "", "default")
	fake := newFakeMilvusClient()
	s.client = fake

	fake.collections["test_coll"] = true
	ctx := context.Background()

	exists, err := s.CollectionExists(ctx, "test_coll")
	if err != nil {
		t.Fatalf("CollectionExists() error = %v", err)
	}
	if !exists {
		t.Error("集合应该存在")
	}

	exists, err = s.CollectionExists(ctx, "not_exist")
	if err != nil {
		t.Fatalf("CollectionExists() error = %v", err)
	}
	if exists {
		t.Error("集合不应该存在")
	}
}

func TestMilvusVectorStore_ListCollectionNames(t *testing.T) {
	s := NewMilvusVectorStore("http://localhost:19530", "", "default")
	fake := newFakeMilvusClient()
	s.client = fake

	fake.collections["coll1"] = true
	fake.collections["coll2"] = true
	ctx := context.Background()

	names, err := s.ListCollectionNames(ctx)
	if err != nil {
		t.Fatalf("ListCollectionNames() error = %v", err)
	}
	if len(names) != 2 {
		t.Errorf("ListCollectionNames() 返回 %d 个, want 2", len(names))
	}
}

func TestBuildFilterExpr(t *testing.T) {
	tests := []struct {
		name    string
		filters map[string]any
		want    string
	}{
		{"空过滤器", nil, ""},
		{"字符串值", map[string]any{"name": "test"}, `name == "test"`},
		{"整数值", map[string]any{"age": 30}, "age == 30"},
		{"多条件", map[string]any{"name": "test", "age": 30}, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := buildFilterExpr(tt.filters)
			if tt.name == "多条件" {
				// 多条件顺序不确定，检查包含
				if got == "" {
					t.Error("buildFilterExpr() 多条件不应返回空")
				}
			} else if got != tt.want {
				t.Errorf("buildFilterExpr() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestMilvusVectorStore_UpdateSchema_预留(t *testing.T) {
	s := NewMilvusVectorStore("http://localhost:19530", "", "default")
	fake := newFakeMilvusClient()
	s.client = fake

	ctx := context.Background()
	err := s.UpdateSchema(ctx, "test_coll", []any{})
	if err == nil {
		t.Error("UpdateSchema() 预留方法应返回错误")
	}
}

func TestMilvusVectorStore_GetCollectionMetadata_缓存命中(t *testing.T) {
	s := NewMilvusVectorStore("http://localhost:19530", "", "default")
	fake := newFakeMilvusClient()
	s.client = fake

	s.collectionMetadata["test_coll"] = &collMeta{
		DistanceMetric: "COSINE",
		VectorField:    "embedding",
		VectorDim:      128,
		SchemaVersion:  "1",
	}

	ctx := context.Background()
	meta, err := s.GetCollectionMetadata(ctx, "test_coll")
	if err != nil {
		t.Fatalf("GetCollectionMetadata() error = %v", err)
	}
	if meta["distance_metric"] != "COSINE" {
		t.Errorf("distance_metric = %v, want COSINE", meta["distance_metric"])
	}
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// createTestSchema 创建测试用的集合 Schema
func createTestSchema() *CollectionSchema {
	pk, _ := NewFieldSchema("id", VectorDataTypeVarchar, WithPrimary())
	vec, _ := NewFieldSchema("embedding", VectorDataTypeFloatVector, WithDim(128))
	text, _ := NewFieldSchema("text", VectorDataTypeVarchar)
	schema, _ := NewCollectionSchemaFromFields([]*FieldSchema{pk, vec, text})
	return schema
}
```

> 注：fakeMilvusClient 需要导入 `client` 包，需在 import 中添加。测试文件中需要 `fmt` 包。

- [ ] **Step 4.12: 编写集成测试**

创建 `internal/agentcore/store/vector/milvus_integration_test.go`：

```go
//go:build integration

package vector

import (
	"context"
	"os"
	"testing"
)

// TestMilvusVectorStore_集成测试 连接真实 Milvus 实例进行完整 CRUD + Search 测试。
// 运行方式: go test -tags=integration ./internal/agentcore/store/vector/...
func TestMilvusVectorStore_集成测试(t *testing.T) {
	uri := os.Getenv("MILVUS_URI")
	if uri == "" {
		uri = "localhost:19530"
	}
	token := os.Getenv("MILVUS_TOKEN")

	store := NewMilvusVectorStore(uri, token, "default")
	ctx := context.Background()

	collName := "test_integration_coll"

	// 清理
	_ = store.DeleteCollection(ctx, collName)

	// 创建集合
	schema := createTestSchema()
	if err := store.CreateCollection(ctx, collName, schema, WithDistanceMetric("COSINE")); err != nil {
		t.Fatalf("CreateCollection() error = %v", err)
	}

	// 检查存在
	exists, err := store.CollectionExists(ctx, collName)
	if err != nil {
		t.Fatalf("CollectionExists() error = %v", err)
	}
	if !exists {
		t.Fatal("集合应该存在")
	}

	// 获取 Schema
	gotSchema, err := store.GetSchema(ctx, collName)
	if err != nil {
		t.Fatalf("GetSchema() error = %v", err)
	}
	if gotSchema == nil {
		t.Fatal("GetSchema() 不应返回 nil")
	}

	// 删除集合
	if err := store.DeleteCollection(ctx, collName); err != nil {
		t.Fatalf("DeleteCollection() error = %v", err)
	}

	// 关闭
	store.Close()
}
```

- [ ] **Step 4.13: 运行单元测试确认通过**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/store/vector/ -v -run "TestMilvus|TestNew|TestBuild" 2>&1 | tail -30`
Expected: PASS（可能需要根据 SDK 实际 API 微调编译错误）

- [ ] **Step 4.14: 提交**

```bash
cd /home/opensource/uap-claw-go && git add internal/agentcore/store/vector/milvus.go internal/agentcore/store/vector/milvus_test.go internal/agentcore/store/vector/milvus_integration_test.go && git commit -m "feat(store/vector): 实现 MilvusVectorStore 完整 CRUD + Search + 集成测试"
```

---

### Task 5: 更新 doc.go 文件

**Files:**
- Modify: `internal/agentcore/store/vector/doc.go`
- Modify: `internal/agentcore/store/vector_fields/doc.go`

- [ ] **Step 1: 更新 vector/doc.go**

在文件目录部分添加 `utils.go` 和 `milvus.go`：

```
//	vector/
//	├── doc.go        # 包文档
//	├── base.go       # VectorDataType + FieldSchema + CollectionSchema + VectorSearchResult + BaseVectorStore + Option
//	├── utils.go      # 距离/相似度转换函数
//	└── milvus.go     # MilvusVectorStore 结构体 + BaseVectorStore 接口实现
```

更新包功能概述，添加 Milvus 实现的说明。

在核心类型/接口索引中添加：

``//	MilvusVectorStore  — Milvus 向量存储实现，包含完整 CRUD 和搜索功能``

- [ ] **Step 2: 更新 vector_fields/doc.go**

在文件目录部分添加 `milvus_fields.go`：

```
//	vector_fields/
//	├── doc.go              # 包文档
//	├── base.go             # VectorField 基类 + DatabaseType/IndexType 枚举 + vf 标签反射机制
//	└── milvus_fields.go    # Milvus 索引子类型（AUTO/FLAT/HNSW/IVF/SCANN）
```

在核心类型/接口索引中添加：

```
//	MilvusAUTO          — Milvus AUTOINDEX 配置
//	MilvusFLAT          — Milvus FLAT 索引配置
//	MilvusHNSW          — Milvus HNSW 索引配置（M, EfConstruction, EfSearchFactor）
//	MilvusIVF           — Milvus IVF 索引配置（Nlist, Nprobe）
//	MilvusSCANN         — Milvus SCANN 索引配置（Nlist, Nprobe, WithRawData, ReorderK）
```

- [ ] **Step 3: 提交**

```bash
cd /home/opensource/uap-claw-go && git add internal/agentcore/store/vector/doc.go internal/agentcore/store/vector_fields/doc.go && git commit -m "docs(store): 更新 vector 和 vector_fields 包 doc.go 文件目录"
```

---

### Task 6: 全量测试与覆盖率验证

**Files:** 无新增

- [ ] **Step 1: 运行全量测试**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/store/... -v -count=1 2>&1 | tail -40`
Expected: 全部 PASS

- [ ] **Step 2: 检查覆盖率**

Run: `cd /home/opensource/uap-claw-go && go test -cover ./internal/agentcore/store/vector/ ./internal/agentcore/store/vector_fields/`
Expected: 覆盖率 ≥ 85%

- [ ] **Step 3: 修复编译或测试问题**

根据测试结果修复代码中的编译错误（特别是 milvus-sdk-go/v2 的 API 差异），直到所有测试通过。

- [ ] **Step 4: 提交修复**

```bash
cd /home/opensource/uap-claw-go && git add -A && git commit -m "fix(store/vector): 修复测试和编译问题"
```

---

### Task 7: 更新实现计划进度

**Files:**
- Modify: `IMPLEMENTATION_PLAN.md`

- [ ] **Step 1: 更新 4.8 状态为 ✅**

将 IMPLEMENTATION_PLAN.md 中 4.8 的状态从 `☐` 改为 `✅`。

- [ ] **Step 2: 提交**

```bash
cd /home/opensource/uap-claw-go && git add IMPLEMENTATION_PLAN.md && git commit -m "docs: 更新 4.8 MilvusVectorStore 实现状态为已完成"
```

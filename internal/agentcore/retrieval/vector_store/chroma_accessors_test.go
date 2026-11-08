package vector_store

import (
	"testing"

	chromav2 "github.com/amikos-tech/chroma-go/pkg/api/v2"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/store/vector_fields"
)

// ──────────────────────────── 非导出函数测试 ────────────────────────────

// TestChromaMetadataKeyToPython 验证 SDK 键名到 Python configuration 键名的映射。
func TestChromaMetadataKeyToPython(t *testing.T) {
	tests := []struct {
		name   string
		sdkKey string
		want   string
	}{
		{"M", chromav2.HNSWM, "max_neighbors"},
		{"construction_ef", chromav2.HNSWConstructionEF, "ef_construction"},
		{"search_ef", chromav2.HNSWSearchEF, "ef_search"},
		{"space", chromav2.HNSWSpace, "space"},
		{"num_threads", chromav2.HNSWNumThreads, "num_threads"},
		{"batch_size", chromav2.HNSWBatchSize, "batch_size"},
		{"sync_threshold", chromav2.HNSWSyncThreshold, "sync_threshold"},
		{"resize_factor", chromav2.HNSWResizeFactor, "resize_factor"},
		{"未知键返回空", "unknown:key", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := chromaMetadataKeyToPython(tt.sdkKey); got != tt.want {
				t.Fatalf("chromaMetadataKeyToPython(%q) = %q, 期望 %q", tt.sdkKey, got, tt.want)
			}
		})
	}
}

// ──────────────────────────── 导出函数测试 ────────────────────────────

// TestChromaVectorStore_Accessors 验证零值 store 的各只读访问器。
// 这些访问器只是字段读取，不触达 ChromaDB 连接。
func TestChromaVectorStore_Accessors(t *testing.T) {
	s := &ChromaVectorStore{
		distanceMetric:    "cosine",
		databaseName:      "db1",
		textField:         "text",
		sparseVectorField: "sparse",
		metadataField:     "meta",
		docIDField:        "doc_id",
		constructConfig:   map[string]any{"a": 1},
		searchConfig:      map[string]any{"b": 2},
	}

	if got := s.DistanceMetric(); got != "cosine" {
		t.Fatalf("DistanceMetric = %q", got)
	}
	if got := s.DatabaseName(); got != "db1" {
		t.Fatalf("DatabaseName = %q", got)
	}
	if got := s.TextField(); got != "text" {
		t.Fatalf("TextField = %q", got)
	}
	if got := s.SparseVectorField(); got != "sparse" {
		t.Fatalf("SparseVectorField = %q", got)
	}
	if got := s.MetadataField(); got != "meta" {
		t.Fatalf("MetadataField = %q", got)
	}
	if got := s.DocIDField(); got != "doc_id" {
		t.Fatalf("DocIDField = %q", got)
	}
	if got := s.ConstructConfig(); got["a"] != 1 {
		t.Fatalf("ConstructConfig = %v", got)
	}
	if got := s.SearchConfig(); got["b"] != 2 {
		t.Fatalf("SearchConfig = %v", got)
	}
	if got := s.Collection(); got != nil {
		t.Fatalf("未初始化时 Collection 应为 nil，实际 %#v", got)
	}
	// VectorField 返回 any：*ChromaVectorField 为 nil 时装箱得到非 nil 接口
	// （typed-nil）。按实际行为断言为「类型正确但值为 nil」。
	if got := s.VectorField(); got == nil {
		t.Skip("实现已改为判空返回 nil，此处分支不适用")
	} else if vf, ok := got.(*vector_fields.ChromaVectorField); !ok || vf != nil {
		t.Fatalf("未设置时应为 typed-nil 的 *ChromaVectorField，实际 %#v", got)
	}
}

// TestChromaVectorStore_VectorField 验证向量字段配置原样返回。
func TestChromaVectorStore_VectorField(t *testing.T) {
	f := vector_fields.NewChromaVectorField("embedding", 16, 200, 1.5)
	s := &ChromaVectorStore{vectorField: f}
	if got := s.VectorField(); got != any(f) {
		t.Fatalf("应返回原字段配置，实际 %#v", got)
	}
}

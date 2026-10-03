//go:build integration

package gaussdb_test

import (
	"context"
	"os"
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/store/vector"
)

// ──────────────────────────── 非导出函数 ────────────────────────────

// newGaussTestSchema 创建 GaussDB 测试用的向量集合 schema
// 原位置：internal/agentcore/foundation/store/vector/gauss_test.go:createGaussTestSchema()
func newGaussTestSchema() *vector.CollectionSchema {
	pk, _ := vector.NewFieldSchema("id", vector.VectorDataTypeVarchar, vector.WithPrimary())
	vec, _ := vector.NewFieldSchema("embedding", vector.VectorDataTypeFloatVector, vector.WithDim(128))
	text, _ := vector.NewFieldSchema("text", vector.VectorDataTypeVarchar)
	schema, _ := vector.NewCollectionSchemaFromFields([]*vector.FieldSchema{pk, vec, text})
	return schema
}

// ──────────────────────────── 导出函数 ────────────────────────────

// TestGaussVectorStore_集成测试 GaussVectorStore 与真实 GaussDB 的集成测试
// 运行方式: go test -tags=integration ./tests/integration/external/gaussdb/...
func TestGaussVectorStore_集成测试(t *testing.T) {
	connString := os.Getenv("GAUSS_DB_CONN_STRING")
	if connString == "" {
		t.Skip("未设置 GAUSS_DB_CONN_STRING 环境变量，跳过集成测试")
	}

	s := vector.NewGaussVectorStore(connString)
	defer s.Close()
	ctx := context.Background()

	schema := newGaussTestSchema()
	err := s.CreateCollection(ctx, "integration_test_coll", schema, vector.WithDistanceMetric("COSINE"))
	if err != nil {
		t.Fatalf("CreateCollection() error = %v", err)
	}

	docs := []map[string]any{
		{"id": "doc1", "text": "hello world", "embedding": make([]float64, 128)},
	}
	err = s.AddDocs(ctx, "integration_test_coll", docs)
	if err != nil {
		t.Fatalf("AddDocs() error = %v", err)
	}

	results, err := s.Search(ctx, "integration_test_coll", make([]float64, 128), "embedding", 5, nil)
	if err != nil {
		t.Fatalf("Search() error = %v", err)
	}
	t.Logf("搜索结果数量: %d", len(results))

	err = s.DeleteCollection(ctx, "integration_test_coll")
	if err != nil {
		t.Fatalf("DeleteCollection() error = %v", err)
	}
}

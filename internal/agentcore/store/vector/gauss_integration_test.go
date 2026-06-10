//go:build integration

package vector

import (
	"context"
	"os"
	"testing"
)

// TestGaussVectorStore_集成测试 GaussVectorStore 与真实 GaussDB 的集成测试
// 运行方式: go test -tags=integration ./internal/agentcore/store/vector/...
func TestGaussVectorStore_集成测试(t *testing.T) {
	connString := os.Getenv("GAUSS_DB_CONN_STRING")
	if connString == "" {
		t.Skip("未设置 GAUSS_DB_CONN_STRING 环境变量，跳过集成测试")
	}

	s := NewGaussVectorStore(connString)
	defer s.Close()
	ctx := context.Background()

	// 创建集合
	schema := createGaussTestSchema()
	err := s.CreateCollection(ctx, "integration_test_coll", schema, WithDistanceMetric("COSINE"))
	if err != nil {
		t.Fatalf("CreateCollection() error = %v", err)
	}

	// 插入文档
	docs := []map[string]any{
		{"id": "doc1", "text": "hello world", "embedding": make([]float64, 128)},
	}
	err = s.AddDocs(ctx, "integration_test_coll", docs)
	if err != nil {
		t.Fatalf("AddDocs() error = %v", err)
	}

	// 搜索
	results, err := s.Search(ctx, "integration_test_coll", make([]float64, 128), "embedding", 5, nil)
	if err != nil {
		t.Fatalf("Search() error = %v", err)
	}
	t.Logf("搜索结果数量: %d", len(results))

	// 清理
	err = s.DeleteCollection(ctx, "integration_test_coll")
	if err != nil {
		t.Fatalf("DeleteCollection() error = %v", err)
	}
}

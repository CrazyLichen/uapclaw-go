//go:build integration

package chromadb_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/store/vector"
)

// newTestSchema 创建测试用的向量集合 schema
// 原位置：internal/agentcore/foundation/store/vector/milvus_test.go:createTestSchema()
func newTestSchema() *vector.CollectionSchema {
	pk, _ := vector.NewFieldSchema("id", vector.VectorDataTypeVarchar, vector.WithPrimary())
	vec, _ := vector.NewFieldSchema("embedding", vector.VectorDataTypeFloatVector, vector.WithDim(3))
	text, _ := vector.NewFieldSchema("text", vector.VectorDataTypeVarchar)
	schema, _ := vector.NewCollectionSchemaFromFields([]*vector.FieldSchema{pk, vec, text})
	return schema
}

// TestChromaVectorStore_集成_创建集合 测试真实创建集合
// 运行方式: go test -tags=integration ./tests/integration/external/chromadb/...
//
// 前提条件：
// - 已安装 chroma-go-local 的原生库（自动下载到 ~/.cache/chroma/local_shim/）
// - 或有可访问的 ChromaDB 服务端
func TestChromaVectorStore_集成_创建集合(t *testing.T) {
	persistPath := filepath.Join(t.TempDir(), "chroma_data")
	store := vector.NewChromaVectorStore(persistPath)
	defer store.Close()

	ctx := context.Background()
	schema := newTestSchema()

	err := store.CreateCollection(ctx, "integration_test", schema, vector.WithDistanceMetric("COSINE"))
	if err != nil {
		t.Fatalf("CreateCollection() error = %v", err)
	}

	exists, err := store.CollectionExists(ctx, "integration_test")
	if err != nil {
		t.Fatalf("CollectionExists() error = %v", err)
	}
	if !exists {
		t.Error("集合应该存在")
	}
}

// TestChromaVectorStore_集成_添加和搜索文档 测试真实添加和搜索文档
func TestChromaVectorStore_集成_添加和搜索文档(t *testing.T) {
	persistPath := filepath.Join(t.TempDir(), "chroma_data")
	store := vector.NewChromaVectorStore(persistPath)
	defer store.Close()

	ctx := context.Background()
	schema := newTestSchema()
	err := store.CreateCollection(ctx, "integration_test", schema, vector.WithDistanceMetric("COSINE"))
	if err != nil {
		t.Fatalf("CreateCollection() error = %v", err)
	}

	docs := []map[string]any{
		{"id": "doc1", "text": "hello world", "embedding": []float32{0.1, 0.2, 0.3}},
		{"id": "doc2", "text": "goodbye world", "embedding": []float32{0.4, 0.5, 0.6}},
	}
	err = store.AddDocs(ctx, "integration_test", docs)
	if err != nil {
		t.Fatalf("AddDocs() error = %v", err)
	}

	results, err := store.Search(ctx, "integration_test", []float64{0.1, 0.2, 0.3}, "embedding", 5, nil)
	if err != nil {
		t.Fatalf("Search() error = %v", err)
	}
	if len(results) == 0 {
		t.Error("Search() 应返回至少一个结果")
	}
}

// TestChromaVectorStore_集成_删除集合 测试真实删除集合
func TestChromaVectorStore_集成_删除集合(t *testing.T) {
	persistPath := filepath.Join(t.TempDir(), "chroma_data")
	store := vector.NewChromaVectorStore(persistPath)
	defer store.Close()

	ctx := context.Background()
	schema := newTestSchema()
	err := store.CreateCollection(ctx, "integration_test", schema, vector.WithDistanceMetric("COSINE"))
	if err != nil {
		t.Fatalf("CreateCollection() error = %v", err)
	}

	err = store.DeleteCollection(ctx, "integration_test")
	if err != nil {
		t.Fatalf("DeleteCollection() error = %v", err)
	}

	exists, err := store.CollectionExists(ctx, "integration_test")
	if err != nil {
		t.Fatalf("CollectionExists() error = %v", err)
	}
	if exists {
		t.Error("删除后集合不应该存在")
	}
}

// TestChromaVectorStore_集成_获取所有文档 测试真实获取所有文档
func TestChromaVectorStore_集成_获取所有文档(t *testing.T) {
	persistPath := filepath.Join(t.TempDir(), "chroma_data")
	store := vector.NewChromaVectorStore(persistPath)
	defer store.Close()

	ctx := context.Background()
	schema := newTestSchema()
	err := store.CreateCollection(ctx, "integration_test", schema, vector.WithDistanceMetric("COSINE"))
	if err != nil {
		t.Fatalf("CreateCollection() error = %v", err)
	}

	docs := []map[string]any{
		{"id": "doc1", "text": "hello", "embedding": []float32{0.1, 0.2, 0.3}},
		{"id": "doc2", "text": "world", "embedding": []float32{0.4, 0.5, 0.6}},
	}
	err = store.AddDocs(ctx, "integration_test", docs)
	if err != nil {
		t.Fatalf("AddDocs() error = %v", err)
	}

	allDocs, err := store.GetAllDocuments(ctx, "integration_test")
	if err != nil {
		t.Fatalf("GetAllDocuments() error = %v", err)
	}
	if len(allDocs) != 2 {
		t.Errorf("GetAllDocuments() 返回 %d 文档, want 2", len(allDocs))
	}
}

// TestChromaVectorStore_集成_持久化 测试数据持久化（创建、关闭、重新打开）
func TestChromaVectorStore_集成_持久化(t *testing.T) {
	persistPath := filepath.Join(t.TempDir(), "chroma_persist")

	// 第一步：创建集合并添加文档
	{
		store := vector.NewChromaVectorStore(persistPath)
		ctx := context.Background()
		schema := newTestSchema()
		err := store.CreateCollection(ctx, "persist_test", schema, vector.WithDistanceMetric("COSINE"))
		if err != nil {
			t.Fatalf("CreateCollection() error = %v", err)
		}
		docs := []map[string]any{
			{"id": "doc1", "text": "persisted", "embedding": []float32{0.1, 0.2, 0.3}},
		}
		err = store.AddDocs(ctx, "persist_test", docs)
		if err != nil {
			t.Fatalf("AddDocs() error = %v", err)
		}
		store.Close()
	}

	// 第二步：重新打开，验证数据持久化
	{
		store := vector.NewChromaVectorStore(persistPath)
		defer store.Close()
		ctx := context.Background()

		exists, err := store.CollectionExists(ctx, "persist_test")
		if err != nil {
			t.Fatalf("CollectionExists() error = %v", err)
		}
		if !exists {
			t.Error("持久化后集合应该存在")
		}

		fmt.Printf("持久化测试路径: %s\n", persistPath)
		_ = os.WriteFile("/tmp/chroma_persist_path.txt", []byte(persistPath), 0644)
	}
}

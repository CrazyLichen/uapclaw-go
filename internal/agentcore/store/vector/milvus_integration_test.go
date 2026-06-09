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

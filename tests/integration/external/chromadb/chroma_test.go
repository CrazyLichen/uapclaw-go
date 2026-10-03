//go:build integration

package chromadb_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/suite"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/store/vector"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/runner"
	isuite "github.com/uapclaw/uapclaw-go/tests/integration/suite"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ChromaSuite ChromaDB 集成测试套件
type ChromaSuite struct {
	isuite.BaseIntegrationSuite
}

// ──────────────────────────── 导出函数 ────────────────────────────

// TestChromaSuite 运行 ChromaDB 集成测试套件
func TestChromaSuite(t *testing.T) {
	suite.Run(t, new(ChromaSuite))
}

// TestChromaVectorStore_集成_创建集合 测试真实创建集合
// 运行方式: go test -tags=integration ./tests/integration/external/chromadb/...
//
// 前提条件：
// - 已安装 chroma-go-local 的原生库（自动下载到 ~/.cache/chroma/local_shim/）
// - 或有可访问的 ChromaDB 服务端
func (s *ChromaSuite) TestChromaVectorStore_集成_创建集合() {
	persistPath := filepath.Join(s.T().TempDir(), "chroma_data")
	store := vector.NewChromaVectorStore(persistPath)
	defer store.Close()

	ctx := context.Background()
	schema := newTestSchema()

	err := store.CreateCollection(ctx, "integration_test", schema, vector.WithDistanceMetric("COSINE"))
	s.Require().NoError(err)

	exists, err := store.CollectionExists(ctx, "integration_test")
	s.Require().NoError(err)
	s.True(exists, "集合应该存在")
}

// TestChromaVectorStore_集成_添加和搜索文档 测试真实添加和搜索文档
func (s *ChromaSuite) TestChromaVectorStore_集成_添加和搜索文档() {
	persistPath := filepath.Join(s.T().TempDir(), "chroma_data")
	store := vector.NewChromaVectorStore(persistPath)
	defer store.Close()

	ctx := context.Background()
	schema := newTestSchema()
	err := store.CreateCollection(ctx, "integration_test", schema, vector.WithDistanceMetric("COSINE"))
	s.Require().NoError(err)

	docs := []map[string]any{
		{"id": "doc1", "text": "hello world", "embedding": []float32{0.1, 0.2, 0.3}},
		{"id": "doc2", "text": "goodbye world", "embedding": []float32{0.4, 0.5, 0.6}},
	}
	err = store.AddDocs(ctx, "integration_test", docs)
	s.Require().NoError(err)

	results, err := store.Search(ctx, "integration_test", []float64{0.1, 0.2, 0.3}, "embedding", 5, nil)
	s.Require().NoError(err)
	s.NotEmpty(results, "Search() 应返回至少一个结果")
}

// TestChromaVectorStore_集成_删除集合 测试真实删除集合
func (s *ChromaSuite) TestChromaVectorStore_集成_删除集合() {
	persistPath := filepath.Join(s.T().TempDir(), "chroma_data")
	store := vector.NewChromaVectorStore(persistPath)
	defer store.Close()

	ctx := context.Background()
	schema := newTestSchema()
	err := store.CreateCollection(ctx, "integration_test", schema, vector.WithDistanceMetric("COSINE"))
	s.Require().NoError(err)

	err = store.DeleteCollection(ctx, "integration_test")
	s.Require().NoError(err)

	exists, err := store.CollectionExists(ctx, "integration_test")
	s.Require().NoError(err)
	s.False(exists, "删除后集合不应该存在")
}

// TestChromaVectorStore_集成_获取所有文档 测试真实获取所有文档
func (s *ChromaSuite) TestChromaVectorStore_集成_获取所有文档() {
	persistPath := filepath.Join(s.T().TempDir(), "chroma_data")
	store := vector.NewChromaVectorStore(persistPath)
	defer store.Close()

	ctx := context.Background()
	schema := newTestSchema()
	err := store.CreateCollection(ctx, "integration_test", schema, vector.WithDistanceMetric("COSINE"))
	s.Require().NoError(err)

	docs := []map[string]any{
		{"id": "doc1", "text": "hello", "embedding": []float32{0.1, 0.2, 0.3}},
		{"id": "doc2", "text": "world", "embedding": []float32{0.4, 0.5, 0.6}},
	}
	err = store.AddDocs(ctx, "integration_test", docs)
	s.Require().NoError(err)

	allDocs, err := store.GetAllDocuments(ctx, "integration_test")
	s.Require().NoError(err)
	s.Len(allDocs, 2)
}

// TestChromaVectorStore_集成_持久化 测试数据持久化（创建、关闭、重新打开）
func (s *ChromaSuite) TestChromaVectorStore_集成_持久化() {
	persistPath := filepath.Join(s.T().TempDir(), "chroma_persist")

	// 第一步：创建集合并添加文档
	{
		store := vector.NewChromaVectorStore(persistPath)
		ctx := context.Background()
		schema := newTestSchema()
		err := store.CreateCollection(ctx, "persist_test", schema, vector.WithDistanceMetric("COSINE"))
		s.Require().NoError(err)
		docs := []map[string]any{
			{"id": "doc1", "text": "persisted", "embedding": []float32{0.1, 0.2, 0.3}},
		}
		err = store.AddDocs(ctx, "persist_test", docs)
		s.Require().NoError(err)
		store.Close()
	}

	// 第二步：重新打开，验证数据持久化
	{
		store := vector.NewChromaVectorStore(persistPath)
		defer store.Close()
		ctx := context.Background()

		exists, err := store.CollectionExists(ctx, "persist_test")
		s.Require().NoError(err)
		s.True(exists, "持久化后集合应该存在")

		fmt.Printf("持久化测试路径: %s\n", persistPath)
		_ = os.WriteFile("/tmp/chroma_persist_path.txt", []byte(persistPath), 0644)
	}
}

// TestChromaDoc_包引用验证 验证引用的 internal 包可正常访问
func (s *ChromaSuite) TestChromaDoc_包引用验证() {
	// 验证 vector 和 runner 包可正常导入
	s.NotNil(vector.NewChromaVectorStore)
	s.NotNil(runner.GetResourceMgr)
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// newTestSchema 创建测试用的向量集合 schema
// 原位置：internal/agentcore/foundation/store/vector/milvus_test.go:createTestSchema()
func newTestSchema() *vector.CollectionSchema {
	pk, _ := vector.NewFieldSchema("id", vector.VectorDataTypeVarchar, vector.WithPrimary())
	vec, _ := vector.NewFieldSchema("embedding", vector.VectorDataTypeFloatVector, vector.WithDim(3))
	text, _ := vector.NewFieldSchema("text", vector.VectorDataTypeVarchar)
	schema, _ := vector.NewCollectionSchemaFromFields([]*vector.FieldSchema{pk, vec, text})
	return schema
}

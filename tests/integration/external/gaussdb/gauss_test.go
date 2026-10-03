//go:build integration

package gaussdb_test

import (
	"context"
	"os"
	"testing"

	"github.com/stretchr/testify/suite"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/store/vector"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/runner"
	isuite "github.com/uapclaw/uapclaw-go/tests/integration/suite"
)

// ──────────────────────────── 结构体 ────────────────────────────

// GaussSuite GaussDB 集成测试套件
type GaussSuite struct {
	isuite.BaseIntegrationSuite
}

// ──────────────────────────── 导出函数 ────────────────────────────

// TestGaussSuite 运行 GaussDB 集成测试套件
func TestGaussSuite(t *testing.T) {
	suite.Run(t, new(GaussSuite))
}

// TestGaussVectorStore_集成测试 GaussVectorStore 与真实 GaussDB 的集成测试
// 运行方式: go test -tags=integration ./tests/integration/external/gaussdb/...
func (s *GaussSuite) TestGaussVectorStore_集成测试() {
	connString := os.Getenv("GAUSS_DB_CONN_STRING")
	if connString == "" {
		s.T().Skip("未设置 GAUSS_DB_CONN_STRING 环境变量，跳过集成测试")
	}

	sv := vector.NewGaussVectorStore(connString)
	defer sv.Close()
	ctx := context.Background()

	schema := newGaussTestSchema()
	err := sv.CreateCollection(ctx, "integration_test_coll", schema, vector.WithDistanceMetric("COSINE"))
	s.Require().NoError(err)

	docs := []map[string]any{
		{"id": "doc1", "text": "hello world", "embedding": make([]float64, 128)},
	}
	err = sv.AddDocs(ctx, "integration_test_coll", docs)
	s.Require().NoError(err)

	results, err := sv.Search(ctx, "integration_test_coll", make([]float64, 128), "embedding", 5, nil)
	s.Require().NoError(err)
	s.T().Logf("搜索结果数量: %d", len(results))

	err = sv.DeleteCollection(ctx, "integration_test_coll")
	s.Require().NoError(err)
}

// TestGaussDoc_包引用验证 验证引用的 internal 包可正常访问
func (s *GaussSuite) TestGaussDoc_包引用验证() {
	// 验证 vector 和 runner 包可正常导入
	s.NotNil(vector.NewGaussVectorStore)
	s.NotNil(runner.GetResourceMgr)
}

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

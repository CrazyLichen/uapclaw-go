# 13.3 MilvusIndexer + MilvusVectorStore (retrieval 层) 实现计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 实现 retrieval 层的 MilvusVectorStore 和 MilvusIndexer，对齐 Python `retrieval/vector_store/milvus_store.py` + `retrieval/indexing/indexer/milvus_indexer.py`，一步到位完成 13.3。

**Architecture:** retrieval 层自建 `milvusClient` 接口（直接使用 Go Milvus SDK v2.6.5，对齐 Python 直接用 pymilvus），通过依赖注入支持测试 mock。MilvusIndexer 在构造时创建共享 client，BuildIndex 时按 collection 创建 MilvusVectorStore 完成写入；ensureCollection 负责自动建表（BM25 sparse + dense 向量 + INVERTED 标量索引）。分数转换复用 foundation 层 `vector` 包的纯函数（对齐 Python import foundation utils）。

**Tech Stack:** Go 1.x + milvus-io/milvus/client/v2 v2.6.5（SDK 已在 go.mod）+ testify（已有依赖）

**设计文档:** `docs/superpowers/specs/2025-07-21-milvus-indexer-13.3-design.md`

---

## 关键 SDK API 速查（写代码时直接参考）

```
milvusclient.New(ctx, &milvusclient.ClientConfig{Address, APIKey, DBName}) // 创建 client
milvusclient.NewHasCollectionOption(name)                                  // HasCollection
milvusclient.NewDropCollectionOption(name)                                 // DropCollection
milvusclient.NewDescribeCollectionOption(name)                             // DescribeCollection → *entity.Collection
milvusclient.NewListCollectionOption()                                     // ListCollections → []string
milvusclient.NewGetCollectionStatsOption(name)                             // GetCollectionStats → map[string]string
milvusclient.NewCreateCollectionOption(name, schema).WithIndexOptions(...) // CreateCollection
milvusclient.NewColumnBasedInsertOption(coll, columns...)                  // Insert
milvusclient.NewDeleteOption(coll).WithExpr(expr)                          // Delete → DeleteResult{DeleteCount}
milvusclient.NewFlushOption(coll)                                          // Flush
milvusclient.NewSearchOption(coll, limit, vectors).WithANNSField(...).WithFilter(...).WithOutputFields(...).WithAnnParam(...).WithSearchParam("metric_type", ...)  // Search
milvusclient.NewQueryOption(coll).WithFilter(...).WithOutputFields(...)    // Query → ResultSet
milvusclient.NewAnnRequest(field, limit, vectors...)                       // AnnRequest（HybridSearch 用）
milvusclient.NewHybridSearchOption(coll, limit, reqs...).WithOutputFields(...).WithReranker(reranker)  // HybridSearch
milvusclient.NewRRFReranker()                                              // RRF（默认 k=60，Python RRFRanker(k=60)）
milvusclient.NewLoadCollectionOption(coll)                                 // LoadCollection
milvusclient.NewGetLoadStateOption(coll)                                   // GetLoadState → entity.LoadState
milvusclient.NewReleaseCollectionOption(coll)                              // ReleaseCollection
milvusclient.NewDescribeIndexOption(coll, fieldName)                       // DescribeIndex → IndexDescription
milvusclient.NewListDatabaseOption()                                       // ListDatabase → []string
milvusclient.NewCreateDatabaseOption(db)                                   // CreateDatabase
milvusclient.NewUseDatabaseOption(db)                                      // UseDatabase

entity.NewSchema().WithAutoID(true).WithDynamicField(false)                // schema
entity.NewField().WithName(...).WithDataType(...).WithMaxLength(65535).WithDim(int64(dim)).WithIsPrimaryKey(true).WithIsAutoID(true).WithEnableAnalyzer(true).WithAnalyzerParams(map[string]any{"tokenizer": "jieba"})
entity.NewFunction().WithName("text_bm25_emb").WithType(entity.FunctionTypeBM25).WithInputFields("content").WithOutputFields("sparse_vector")
index.NewInvertedIndex()                                                   // INVERTED 标量索引
index.NewSparseInvertedIndex(entity.BM25, dropRatio)                       // BM25 sparse 索引
index.NewAutoIndex(metricType) / index.NewHNSWIndex(m, efC) / ...          // dense 索引
column.NewColumnVarChar(name, []string)                                    // 列构造
column.NewColumnFloatVector(name, dim, [][]float32)
column.NewColumnJSONBytes(name, [][]byte)
entity.FloatVector([]float32) / entity.Text("query")                       // 查询向量/文本
ResultSet: .ResultCount / .Scores []float32 / .Fields (DataSet) / .GetColumn(name) → column.Column
column.Column: .Name() / .Get(idx) (any, error) / .GetAsString(idx) / .GetAsDouble(idx)
```

**注意**：
- `InvertedIndex` 的 `dropRatio`：Python MilvusIndexer 的 SPARSE_INVERTED_INDEX 未传 drop_ratio（默认 0），graph 层用了 0.2——本处对齐 Python milvus_indexer 用默认 0
- `entity.Text` 用于 BM25 稀疏搜索的查询输入（Python `data=[query_text]`）
- Indexer 接口签名（已有）：`BuildIndex(ctx, chunks []common.TextChunk, config common.IndexConfig, embedModel embedding.BaseEmbedding, opts ...IndexOption) (bool, error)`
- Python MilvusIndexer `doc_id_field` 参数在 `__init__` 中，注意结构体字段对齐

## 任务清单

- Task 1: retrieval/utils/common.go — CreateMilvusAlias + Deduplicate（TDD）
- Task 2: retrieval/vector_store/milvus.go — milvusClient 接口 + 适配器 + 构造函数（TDD）
- Task 3: retrieval/vector_store/milvus.go — Add/Delete/TableExists/DeleteTable/Close/ensureLoaded（TDD）
- Task 4: retrieval/vector_store/milvus.go — Search/SparseSearch/HybridSearch + 结果转换（TDD）
- Task 5: retrieval/vector_store/milvus.go — CheckVectorField/getSearchParams（TDD）
- Task 6: retrieval/indexing/indexer/milvus.go — MilvusIndexer 构造 + IndexExists/DeleteIndex/GetIndexInfo/Close（TDD）
- Task 7: retrieval/indexing/indexer/milvus.go — ensureCollection（TDD）
- Task 8: retrieval/indexing/indexer/milvus.go — BuildIndex/UpdateIndex（TDD）
- Task 9: 文档更新（doc.go × 3）+ IMPLEMENTATION_PLAN.md 状态回填

---

### Task 1: retrieval/utils/common.go — CreateMilvusAlias + Deduplicate

**Files:**
- Create: `internal/agentcore/retrieval/utils/common.go`
- Test: `internal/agentcore/retrieval/utils/common_test.go`

- [ ] **Step 1.1: 写失败测试**

```go
package utils

import (
	"crypto/md5"
	"encoding/hex"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestCreateMilvusAlias_别名非空 对齐 Python: if alias: return alias
func TestCreateMilvusAlias_别名非空(t *testing.T) {
	assert.Equal(t, "my_alias", CreateMilvusAlias("my_alias", "http://localhost:19530", ""))
}

// TestCreateMilvusAlias_无token 对齐 Python: auth_info = user or "noauth"; "-".join(["kb", uri, auth_info])
func TestCreateMilvusAlias_无token(t *testing.T) {
	result := CreateMilvusAlias("", "http://localhost:19530", "")
	assert.Equal(t, "kb-http://localhost:19530-noauth", result)
}

// TestCreateMilvusAlias_有token 对齐 Python: md5(token) 作为 auth_info
func TestCreateMilvusAlias_有token(t *testing.T) {
	token := "secret-token"
	md5sum := md5.Sum([]byte(token)) // #nosec G401 — 对齐 Python usedforsecurity=False
	expected := fmt.Sprintf("kb-http://localhost:19530-%s", hex.EncodeToString(md5sum[:]))
	assert.Equal(t, expected, CreateMilvusAlias("", "http://localhost:19530", token))
}

// TestCreateMilvusAlias_用户名 对齐 Python: auth_info = user or "noauth"
func TestCreateMilvusAlias_用户名(t *testing.T) {
	// Go 签名将 user 合并进 token 参数外的路径：Python 签名 (alias, uri, user="", token=None)
	// Go 用可选 user 参数对齐
	assert.Equal(t, "kb-http://localhost:19530-root", CreateMilvusAlias("", "http://localhost:19530", "", "root"))
}

// TestDeduplicate_正常去重 对齐 Python deduplicate 保留顺序
func TestDeduplicate_正常去重(t *testing.T) {
	data := []string{"a", "b", "a", "c", "b"}
	result := Deduplicate(data, func(s string) string { return s })
	assert.Equal(t, []string{"a", "b", "c"}, result)
}

// TestDeduplicate_空切片 对齐 Python: 空输入返回空列表
func TestDeduplicate_空切片(t *testing.T) {
	result := Deduplicate([]int{}, func(i int) int { return i })
	assert.Empty(t, result)
}

// TestDeduplicate_自定义key 对齐 Python deduplicate(data, key=...)
func TestDeduplicate_自定义key(t *testing.T) {
	type item struct{ k, v string }
	data := []item{{"a", "1"}, {"b", "2"}, {"a", "3"}}
	result := Deduplicate(data, func(i item) string { return i.k })
	assert.Equal(t, []item{{"a", "1"}, {"b", "2"}}, result)
}
```

- [ ] **Step 1.2: 运行测试确认失败**

```bash
cd /home/opensource/uapclaw-gateway && go test ./internal/agentcore/retrieval/utils/ -run 'TestCreateMilvusAlias|TestDeduplicate' -v 2>&1 | head -20
```

预期：编译失败 `undefined: CreateMilvusAlias` / `undefined: Deduplicate`

- [ ] **Step 1.3: 实现 common.go**

```go
package utils

import (
	"crypto/md5"
	"encoding/hex"
	"fmt"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// CreateMilvusAlias 生成 Milvus 连接别名。
//
// 对齐 Python retrieval/utils/common.py (create_milvus_alias)：
//   - alias 非空时直接返回
//   - 否则基于 URI + 认证信息生成：token 非空用 MD5(token)，否则用 user 或 "noauth"
//   - 格式：kb-{uri}-{auth_info}，空元素跳过
func CreateMilvusAlias(alias, uri, token string, user ...string) string {
	if alias != "" {
		return alias
	}
	// 对齐 Python: auth_info = user or "noauth"
	authInfo := "noauth"
	if len(user) > 0 && user[0] != "" {
		authInfo = user[0]
	}
	// 对齐 Python: if token: md5(token) 作为 auth_info
	if token != "" {
		md5sum := md5.Sum([]byte(token)) // #nosec G401 — 对齐 Python usedforsecurity=False
		authInfo = hex.EncodeToString(md5sum[:])
	}
	// 对齐 Python: "-".join(elem for elem in ["kb", uri, auth_info] if elem)
	elems := make([]string, 0, 3)
	for _, e := range []string{"kb", uri, authInfo} {
		if e != "" {
			elems = append(elems, e)
		}
	}
	return joinNonEmpty(elems)
}

// Deduplicate 去重并保留首次出现顺序。
//
// 对齐 Python retrieval/utils/common.py (deduplicate)。
type dedupKey = string

// ──────────────────────────── 非导出函数 ────────────────────────────

// joinNonEmpty 用 "-" 连接非空元素。
// 对齐 Python: "-".join(elem for elem in [...] if elem)
func joinNonEmpty(elems []string) string {
	result := ""
	for i, e := range elems {
		if i > 0 {
			result += "-"
		}
		result += e
	}
	return result
}
```

等一下——Deduplicate 是泛型函数，需要单独写。完整文件：

```go
package utils

import (
	"crypto/md5"
	"encoding/hex"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// CreateMilvusAlias 生成 Milvus 连接别名。
//
// 对齐 Python retrieval/utils/common.py (create_milvus_alias)：
//   - alias 非空时直接返回
//   - 否则基于 URI + 认证信息生成：token 非空用 MD5(token)，否则用 user 或 "noauth"
//   - 格式：kb-{uri}-{auth_info}，空元素跳过
func CreateMilvusAlias(alias, uri, token string, user ...string) string {
	if alias != "" {
		return alias
	}
	// 对齐 Python: auth_info = user or "noauth"
	authInfo := "noauth"
	if len(user) > 0 && user[0] != "" {
		authInfo = user[0]
	}
	// 对齐 Python: if token: md5(token) 作为 auth_info
	if token != "" {
		md5sum := md5.Sum([]byte(token)) // #nosec G401 — 对齐 Python usedforsecurity=False
		authInfo = hex.EncodeToString(md5sum[:])
	}
	// 对齐 Python: "-".join(elem for elem in ["kb", uri, auth_info] if elem)
	elems := make([]string, 0, 3)
	for _, e := range []string{"kb", uri, authInfo} {
		if e != "" {
			elems = append(elems, e)
		}
	}
	result := ""
	for i, e := range elems {
		if i > 0 {
			result += "-"
		}
		result += e
	}
	return result
}

// Deduplicate 去重并保留首次出现顺序。
//
// 对齐 Python retrieval/utils/common.py (deduplicate)：
// 按 key 函数计算键，重复项只保留首次出现。
func Deduplicate[T any, K comparable](data []T, key func(T) K) []T {
	seen := make(map[K]struct{})
	result := make([]T, 0, len(data))
	for _, item := range data {
		k := key(item)
		if _, exists := seen[k]; !exists {
			seen[k] = struct{}{}
			result = append(result, item)
		}
	}
	return result
}
```

注意测试中 `Deduplicate(data, func(s string) string { return s })` 的 key 返回 string，泛型 K 推断为 string，正确。

- [ ] **Step 1.4: 运行测试确认通过**

```bash
cd /home/opensource/uapclaw-gateway && go test ./internal/agentcore/retrieval/utils/ -run 'TestCreateMilvusAlias|TestDeduplicate' -v 2>&1 | tail -15
```

预期：全部 PASS

- [ ] **Step 1.5: 提交**

```bash
cd /home/opensource/uapclaw-gateway && git add internal/agentcore/retrieval/utils/common.go internal/agentcore/retrieval/utils/common_test.go && git commit -m "feat(retrieval): 13.3 前置工具 CreateMilvusAlias + Deduplicate 对齐 Python retrieval/utils/common.py"
```

---

### Task 2: retrieval/vector_store/milvus.go — milvusClient 接口 + 适配器 + 构造函数

**Files:**
- Create: `internal/agentcore/retrieval/vector_store/milvus.go`
- Test: `internal/agentcore/retrieval/vector_store/milvus_test.go`

- [ ] **Step 2.1: 写失败测试**

```go
package vector_store

import (
	"context"
	"testing"

	"github.com/milvus-io/milvus/client/v2/entity"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/store/vector_fields"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/retrieval/common"
)

// newTestMilvusStore 构建带 fake client 的测试 store
func newTestMilvusStore(t *testing.T) *MilvusVectorStore {
	t.Helper()
	config := common.VectorStoreConfig{
		StoreProvider:  common.StoreTypeMilvus,
		CollectionName: "test-collection",
		DatabaseName:   "default",
		DistanceMetric: common.DistanceMetricCosine,
	}
	vf := vector_fields.NewMilvusAUTO("embedding")
	s, err := NewMilvusVectorStore(config, "http://localhost:19530", "",
		"content", vf, "sparse_vector", "metadata", "document_id")
	require.NoError(t, err)
	require.NotNil(t, s)
	return s
}

// TestNewMilvusVectorStore_默认参数 对齐 Python MilvusVectorStore.__init__
func TestNewMilvusVectorStore_默认参数(t *testing.T) {
	s := newTestMilvusStore(t)
	assert.Equal(t, "test-collection", s.collectionName)
	assert.Equal(t, "COSINE", s.distanceMetric) // 对齐 Python: .upper()
	assert.Equal(t, "content", s.textField)
	assert.Equal(t, "embedding", s.vectorField.VectorFieldName)
	assert.Equal(t, "sparse_vector", s.sparseVectorField)
	assert.Equal(t, "metadata", s.metadataField)
	assert.Equal(t, "document_id", s.docIDField)
}

// TestNewMilvusVectorStore_距离度量归一化 对齐 Python: replace("dot","ip").replace("euclidean","l2").upper()
func TestNewMilvusVectorStore_距离度量归一化(t *testing.T) {
	tests := []struct {
		input    common.DistanceMetricKind
		expected string
	}{
		{common.DistanceMetricCosine, "COSINE"},
		{common.DistanceMetricEuclidean, "L2"},
		{common.DistanceMetricDot, "IP"},
	}
	for _, tt := range tests {
		t.Run(string(tt.input), func(t *testing.T) {
			config := common.VectorStoreConfig{
				StoreProvider:  common.StoreTypeMilvus,
				CollectionName: "test-collection",
				DistanceMetric: tt.input,
			}
			s, err := NewMilvusVectorStore(config, "http://localhost:19530", "",
				"content", vector_fields.NewMilvusAUTO("embedding"), "sparse_vector", "metadata", "document_id")
			require.NoError(t, err)
			assert.Equal(t, tt.expected, s.distanceMetric)
		})
	}
}

// TestNewMilvusVectorStore_constructConfig 对齐 Python: auto 时 _construct_config={} 否则 to_dict(stage="construct")
func TestNewMilvusVectorStore_constructConfig(t *testing.T) {
	s := newTestMilvusStore(t) // AUTO → 空 construct config + metric_type
	assert.Equal(t, "COSINE", s.constructConfig["metric_type"])
	assert.Len(t, s.constructConfig, 1) // auto 索引无其他参数

	// HNSW → construct 参数存在
	config := common.VectorStoreConfig{
		StoreProvider:  common.StoreTypeMilvus,
		CollectionName: "test-collection",
		DistanceMetric: common.DistanceMetricCosine,
	}
	hnsw := vector_fields.NewMilvusHNSW("embedding", 30, 360, 2.0)
	s2, err := NewMilvusVectorStore(config, "http://localhost:19530", "",
		"content", hnsw, "sparse_vector", "metadata", "document_id")
	require.NoError(t, err)
	assert.Equal(t, 30, s2.constructConfig["M"])
	assert.Equal(t, 360, s2.constructConfig["EfConstruction"])
	assert.Equal(t, "COSINE", s2.constructConfig["metric_type"])
}

// TestNewMilvusVectorStore_HNSW搜索配置 对齐 Python: _search_config = to_dict(stage="search")
func TestNewMilvusVectorStore_HNSW搜索配置(t *testing.T) {
	config := common.VectorStoreConfig{
		StoreProvider:  common.StoreTypeMilvus,
		CollectionName: "test-collection",
		DistanceMetric: common.DistanceMetricCosine,
	}
	hnsw := vector_fields.NewMilvusHNSW("embedding", 30, 360, 2.0)
	s, err := NewMilvusVectorStore(config, "http://localhost:19530", "",
		"content", hnsw, "sparse_vector", "metadata", "document_id")
	require.NoError(t, err)
	assert.Equal(t, 2.0, s.searchConfig["EfSearchFactor"])
}

// TestNewMilvusVectorStore_vectorField为nil 对齐 Python: raise VECTOR_FIELD_INVALID
func TestNewMilvusVectorStore_vectorField为nil(t *testing.T) {
	config := common.VectorStoreConfig{
		StoreProvider:  common.StoreTypeMilvus,
		CollectionName: "test-collection",
	}
	_, err := NewMilvusVectorStore(config, "http://localhost:19530", "",
		"content", nil, "sparse_vector", "metadata", "document_id")
	assert.Error(t, err)
}

// TestNewMilvusVectorStore_惰性客户端 对齐 Python：client 在首次使用时才创建
func TestNewMilvusVectorStore_惰性客户端(t *testing.T) {
	s := newTestMilvusStore(t)
	assert.Nil(t, s.client) // 未注入时为 nil，通过 getClient 惰性创建
}
```

- [ ] **Step 2.2: 运行测试确认失败**

```bash
cd /home/opensource/uapclaw-gateway && go test ./internal/agentcore/retrieval/vector_store/ -run 'TestNewMilvusVectorStore' -v 2>&1 | head -10
```

预期：编译失败 `undefined: NewMilvusVectorStore`

- [ ] **Step 2.3: 实现 milvus.go 第一部分（接口 + 适配器 + 构造）**

```go
package vector_store

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/milvus-io/milvus/client/v2/column"
	"github.com/milvus-io/milvus/client/v2/entity"
	milvusclient "github.com/milvus-io/milvus/client/v2/milvusclient"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/store/vector_fields"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/retrieval/common"
	"github.com/uapclaw/uapclaw-go/internal/common/exception"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 接口 ────────────────────────────

// milvusClient retrieval 层 Milvus 客户端接口。
//
// 对齐 Python 直接使用 pymilvus.MilvusClient 的方法子集，
// 与 foundation 层的 milvusClient 接口独立（Python 两层也各自独立用 SDK）。
// 通过依赖注入支持单元测试 mock。
type milvusClient interface {
	// CreateCollection 创建集合
	CreateCollection(ctx context.Context, option milvusclient.CreateCollectionOption, callOptions ...any) error
	// DropCollection 删除集合
	DropCollection(ctx context.Context, option milvusclient.DropCollectionOption, callOptions ...any) error
	// HasCollection 检查集合是否存在
	HasCollection(ctx context.Context, option milvusclient.HasCollectionOption, callOptions ...any) (bool, error)
	// DescribeCollection 获取集合描述
	DescribeCollection(ctx context.Context, option milvusclient.DescribeCollectionOption, callOptions ...any) (*entity.Collection, error)
	// ListCollections 列出集合名称
	ListCollections(ctx context.Context, option milvusclient.ListCollectionOption, callOptions ...any) ([]string, error)
	// GetCollectionStats 获取集合统计信息（row_count）
	GetCollectionStats(ctx context.Context, option milvusclient.GetCollectionStatsOption, callOptions ...any) (map[string]string, error)
	// Insert 插入数据
	Insert(ctx context.Context, option milvusclient.InsertOption, callOptions ...any) (milvusclient.InsertResult, error)
	// Delete 删除数据
	Delete(ctx context.Context, option milvusclient.DeleteOption, callOptions ...any) (milvusclient.DeleteResult, error)
	// Flush 刷写数据
	Flush(ctx context.Context, option milvusclient.FlushOption, callOptions ...any) error
	// Search 向量搜索
	Search(ctx context.Context, option milvusclient.SearchOption, callOptions ...any) ([]milvusclient.ResultSet, error)
	// HybridSearch 混合搜索
	HybridSearch(ctx context.Context, option milvusclient.HybridSearchOption, callOptions ...any) ([]milvusclient.ResultSet, error)
	// Query 按表达式查询
	Query(ctx context.Context, option milvusclient.QueryOption, callOptions ...any) (milvusclient.ResultSet, error)
	// DescribeIndex 获取索引描述
	DescribeIndex(ctx context.Context, option milvusclient.DescribeIndexOption, callOptions ...any) (milvusclient.IndexDescription, error)
	// LoadCollection 加载集合
	LoadCollection(ctx context.Context, option milvusclient.LoadCollectionOption, callOptions ...any) error
	// GetLoadState 获取加载状态
	GetLoadState(ctx context.Context, option milvusclient.GetLoadStateOption, callOptions ...any) (entity.LoadState, error)
	// ReleaseCollection 释放集合
	ReleaseCollection(ctx context.Context, option milvusclient.ReleaseCollectionOption, callOptions ...any) error
	// ListDatabase 列出数据库
	ListDatabase(ctx context.Context, option milvusclient.ListDatabaseOption, callOptions ...any) ([]string, error)
	// CreateDatabase 创建数据库
	CreateDatabase(ctx context.Context, option milvusclient.CreateDatabaseOption, callOptions ...any) error
	// UseDatabase 切换数据库
	UseDatabase(ctx context.Context, option milvusclient.UseDatabaseOption, callOptions ...any) error
	// Close 关闭连接
	Close(ctx context.Context) error
}

// ──────────────────────────── 结构体 ────────────────────────────

// milvusClientAdapter 适配器，将 milvusclient.Client 适配到 milvusClient 接口。
// 异步 Task 方法（LoadCollection/Flush）通过 task.Await 转为同步。
// 仅在集成测试中覆盖（需要真实 Milvus 服务）。
type milvusClientAdapter struct {
	client *milvusclient.Client
}

// MilvusVectorStore retrieval 层 Milvus 向量存储实现。
//
// 支持向量搜索、BM25 稀疏搜索和混合搜索（原生 HybridSearch + RRF）。
// 对齐 Python retrieval/vector_store/milvus_store.py (MilvusVectorStore)。
type MilvusVectorStore struct {
	// config 向量存储配置
	config common.VectorStoreConfig
	// collectionName 集合名称
	collectionName string
	// milvusURI Milvus 服务地址
	milvusURI string
	// milvusToken Milvus 认证 token
	milvusToken string
	// textField 文本字段名，默认 "content"
	textField string
	// vectorField 向量字段配置
	vectorField *vector_fields.MilvusVectorFieldConfig
	// sparseVectorField 稀疏向量字段名，默认 "sparse_vector"
	sparseVectorField string
	// metadataField 元数据字段名，默认 "metadata"
	metadataField string
	// docIDField 文档 ID 字段名，默认 "document_id"
	docIDField string
	// databaseName 数据库名称
	databaseName string
	// distanceMetric 归一化距离度量：IP/L2/COSINE
	distanceMetric string
	// constructConfig 构建阶段配置
	constructConfig map[string]any
	// searchConfig 搜索阶段配置
	searchConfig map[string]any
	// client Milvus 客户端（惰性创建）
	client milvusClient
	// createClientFunc 客户端创建函数（依赖注入）
	createClientFunc func(ctx context.Context, databaseName, uri, token string) (milvusClient, error)
	// collectionLoaded 集合是否已加载
	collectionLoaded bool
	// mu 读写锁
	mu sync.RWMutex
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

const (
	// defaultMilvusBatchSize 默认批大小，对齐 Python batch_size=128
	defaultMilvusBatchSize = 128
	// logComponent 日志组件
	logComponent = logger.ComponentAgentCore
)

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// NewMilvusVectorStore 创建 retrieval 层 MilvusVectorStore。
//
// 对齐 Python MilvusVectorStore.__init__。
// vectorField 为 *vector_fields.MilvusVectorFieldConfig 完整配置（含 MilvusAUTO 等变体）。
// 客户端惰性创建：首次使用时才建立连接（对齐 Python 构造时创建，Go 便于测试改为惰性）。
func NewMilvusVectorStore(
	config common.VectorStoreConfig,
	milvusURI string,
	milvusToken string,
	textField string,
	vectorField *vector_fields.MilvusVectorFieldConfig,
	sparseVectorField string,
	metadataField string,
	docIDField string,
	opts ...MilvusStoreOption,
) (*MilvusVectorStore, error) {
	// 对齐 Python: if not milvus_uri: raise（隐性，连接失败）
	if strings.TrimSpace(milvusURI) == "" {
		return nil, exception.BuildError(
			exception.StatusRetrievalVectorStorePathNotFound,
			exception.WithParam("error_msg", "milvus_uri is required and cannot be empty"),
		)
	}

	// 对齐 Python: if isinstance(vector_field, str): MilvusAUTO(vector_field=...)
	//              elif isinstance(vector_field, MilvusVectorField): pass
	//              else: raise VECTOR_FIELD_INVALID
	if vectorField == nil {
		return nil, exception.BuildError(
			exception.StatusRetrievalIndexingVectorFieldInvalid,
			exception.WithParam("error_msg", "vector_field must not be nil"),
		)
	}

	s := &MilvusVectorStore{
		config:            config,
		collectionName:    config.CollectionName,
		milvusURI:         milvusURI,
		milvusToken:       milvusToken,
		textField:         textField,
		sparseVectorField: sparseVectorField,
		metadataField:     metadataField,
		docIDField:        docIDField,
		databaseName:      config.DatabaseName,
		vectorField:       vectorField,
		createClientFunc:  defaultMilvusCreateClient,
	}

	for _, opt := range opts {
		opt(s)
	}

	// 对齐 Python: self._distance_metric = config.distance_metric.replace("dot","ip").replace("euclidean","l2").upper()
	s.distanceMetric = strings.ToUpper(
		strings.ReplaceAll(
			strings.ReplaceAll(string(config.DistanceMetric), "dot", "ip"),
			"euclidean", "l2",
		),
	)

	// 对齐 Python: if self.vector_field.index_type == "auto": self._construct_config = {}
	//              else: self._construct_config = self.vector_field.to_dict(stage="construct")
	if vectorField.IndexType() == vector_fields.IndexTypeAUTO {
		s.constructConfig = map[string]any{}
	} else {
		s.constructConfig = vectorField.ToConstructDict()
	}
	// 对齐 Python: self._construct_config["metric_type"] = self._distance_metric
	s.constructConfig["metric_type"] = s.distanceMetric

	// 对齐 Python: self._search_config = self.vector_field.to_dict(stage="search")
	s.searchConfig = vectorField.ToSearchDict()

	return s, nil
}

// Client 返回底层 Milvus 客户端（惰性创建）。
// 对齐 Python client property。
func (s *MilvusVectorStore) Client(ctx context.Context) (milvusClient, error) {
	return s.getClient(ctx)
}

// DistanceMetric 返回归一化距离度量（IP/L2/COSINE）。
// 对齐 Python distance_metric property。
func (s *MilvusVectorStore) DistanceMetric() string { return s.distanceMetric }

// WithMilvusClient 注入已创建的客户端（测试用）。
type MilvusStoreOption func(*MilvusVectorStore)

// WithMilvusClientOption 注入 milvusClient 实现。
func WithMilvusClientOption(c milvusClient) MilvusStoreOption {
	return func(s *MilvusVectorStore) {
		s.client = c
		s.createClientFunc = func(ctx context.Context, _, _, _ string) (milvusClient, error) {
			return c, nil
		}
	}
}

// WithMilvusCreateClient 注入客户端创建函数（测试用）。
func WithMilvusCreateClient(f func(ctx context.Context, databaseName, uri, token string) (milvusClient, error)) MilvusStoreOption {
	return func(s *MilvusVectorStore) { s.createClientFunc = f }
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// defaultMilvusCreateClient 默认客户端创建函数。
// 对齐 Python MilvusVectorStore.create_client：
// 创建 client → database_name 非空且非 default 时确保数据库存在并切换。
// 仅在集成测试中覆盖。
func defaultMilvusCreateClient(ctx context.Context, databaseName, uri, token string) (milvusClient, error) {
	c, err := milvusclient.New(ctx, &milvusclient.ClientConfig{
		Address: uri,
		APIKey:  token,
		DBName:  databaseName,
	})
	if err != nil {
		return nil, err
	}
	return &milvusClientAdapter{client: c}, nil
}

// getClient 惰性获取 Milvus 客户端。
func (s *MilvusVectorStore) getClient(ctx context.Context) (milvusClient, error) {
	s.mu.RLock()
	if s.client != nil {
		s.mu.RUnlock()
		return s.client, nil
	}
	s.mu.RUnlock()

	s.mu.Lock()
	defer s.mu.Unlock()
	if s.client != nil {
		return s.client, nil
	}
	if s.createClientFunc == nil {
		return nil, fmt.Errorf("milvus client 创建函数未设置")
	}
	client, err := s.createClientFunc(ctx, s.databaseName, s.milvusURI, s.milvusToken)
	if err != nil {
		return nil, exception.BuildError(
			exception.StatusRetrievalVectorStoreProviderInvalid,
			exception.WithParam("error_msg", fmt.Sprintf("创建 Milvus 客户端失败: %s", err)),
			exception.WithCause(err),
		)
	}
	s.client = client
	return client, nil
}
```

同时需要在 `vector_fields` 包中新增 `MilvusVectorFieldConfig` 接口和 `ToConstructDict`/`ToSearchDict` 方法。在 `internal/agentcore/foundation/store/vector_fields/milvus_fields.go` 末尾追加：

```go
// MilvusVectorFieldConfig Milvus 向量字段统一接口。
//
// 对齐 Python MilvusVectorField（Union 类型的接口面），
// MilvusAUTO/MilvusFLAT/MilvusHNSW/MilvusIVF/MilvusSCANN 均实现此接口。
type MilvusVectorFieldConfig interface {
	// IndexType 返回索引类型
	IndexType() IndexType
	// VectorFieldName 返回向量字段名
	VectorFieldName() string
	// ToConstructDict 返回构建阶段配置
	ToConstructDict() map[string]any
	// ToSearchDict 返回搜索阶段配置
	ToSearchDict() map[string]any
}

// IndexType 返回 AUTO 索引类型。
func (a *MilvusAUTO) IndexType() IndexType { return a.VectorField.IndexType }

// VectorFieldName 返回向量字段名。
func (a *MilvusAUTO) VectorFieldName() string { return a.VectorField.VectorFieldName }

// ToConstructDict 返回构建阶段配置。
func (a *MilvusAUTO) ToConstructDict() map[string]any {
	return ToDict(*a, StageConstruct)
}

// ToSearchDict 返回搜索阶段配置。
func (a *MilvusAUTO) ToSearchDict() map[string]any {
	return ToDict(*a, StageSearch)
}

// IndexType 返回 FLAT 索引类型。
func (f *MilvusFLAT) IndexType() IndexType { return f.VectorField.IndexType }

// VectorFieldName 返回向量字段名。
func (f *MilvusFLAT) VectorFieldName() string { return f.VectorField.VectorFieldName }

// ToConstructDict 返回构建阶段配置。
func (f *MilvusFLAT) ToConstructDict() map[string]any {
	return ToDict(*f, StageConstruct)
}

// ToSearchDict 返回搜索阶段配置。
func (f *MilvusFLAT) ToSearchDict() map[string]any {
	return ToDict(*f, StageSearch)
}

// IndexType 返回 HNSW 索引类型。
func (h *MilvusHNSW) IndexType() IndexType { return h.VectorField.IndexType }

// VectorFieldName 返回向量字段名。
func (h *MilvusHNSW) VectorFieldName() string { return h.VectorField.VectorFieldName }

// ToConstructDict 返回构建阶段配置。
func (h *MilvusHNSW) ToConstructDict() map[string]any {
	return ToDict(*h, StageConstruct)
}

// ToSearchDict 返回搜索阶段配置。
func (h *MilvusHNSW) ToSearchDict() map[string]any {
	return ToDict(*h, StageSearch)
}

// IndexType 返回 IVF 索引类型。
func (iv *MilvusIVF) IndexType() IndexType { return iv.VectorField.IndexType }

// VectorFieldName 返回向量字段名。
func (iv *MilvusIVF) VectorFieldName() string { return iv.VectorField.VectorFieldName }

// ToConstructDict 返回构建阶段配置。
func (iv *MilvusIVF) ToConstructDict() map[string]any {
	return ToDict(*iv, StageConstruct)
}

// ToSearchDict 返回搜索阶段配置。
func (iv *MilvusIVF) ToSearchDict() map[string]any {
	return ToDict(*iv, StageSearch)
}

// IndexType 返回 SCANN 索引类型。
func (sc *MilvusSCANN) IndexType() IndexType { return sc.VectorField.IndexType }

// VectorFieldName 返回向量字段名。
func (sc *MilvusSCANN) VectorFieldName() string { return sc.VectorField.VectorFieldName }

// ToConstructDict 返回构建阶段配置。
func (sc *MilvusSCANN) ToConstructDict() map[string]any {
	return ToDict(*sc, StageConstruct)
}

// ToSearchDict 返回搜索阶段配置。
func (sc *MilvusSCANN) ToSearchDict() map[string]any {
	return ToDict(*sc, StageSearch)
}
```

注意：接口方法名 `IndexType()` 与结构体嵌入字段 `VectorField.IndexType` 同名会遮蔽——嵌入字段的 `IndexType` 是数据字段而非方法，所以 `a.VectorField.IndexType` 访问的是字段，`a.IndexType()` 是新定义的方法，无冲突。

- [ ] **Step 2.4: 运行测试确认通过**

```bash
cd /home/opensource/uapclaw-gateway && go test ./internal/agentcore/retrieval/vector_store/ -run 'TestNewMilvusVectorStore' -v 2>&1 | tail -15
```

预期：全部 PASS。注意 `logComponent` 常量已在 chroma.go 中定义，新文件不要重复定义（删掉本文件中的 logComponent 定义，直接复用）。

- [ ] **Step 2.5: 提交**

```bash
cd /home/opensource/uapclaw-gateway && git add internal/agentcore/retrieval/vector_store/milvus.go internal/agentcore/retrieval/vector_store/milvus_test.go internal/agentcore/foundation/store/vector_fields/milvus_fields.go && git commit -m "feat(retrieval): 13.3 MilvusVectorStore 接口+适配器+构造函数，vector_fields 新增 MilvusVectorFieldConfig 统一接口"
```

---

### Task 3: MilvusVectorStore — Add/Delete/TableExists/DeleteTable/Close/ensureLoaded

**Files:**
- Modify: `internal/agentcore/retrieval/vector_store/milvus.go`
- Test: `internal/agentcore/retrieval/vector_store/milvus_test.go`

- [ ] **Step 3.1: 在测试文件追加失败测试**

```go
// fakeRetrievalMilvusClient retrieval 层测试用 fake client
type fakeRetrievalMilvusClient struct {
	collections   map[string]bool
	insertedRows  int
	deleteCount   int64
	flushCalled   bool
	insertErr     error
	queryErr      error
	insertBatches [][]map[string]any // 记录每次 Insert 的列数据
}

func newFakeRetrievalMilvusClient() *fakeRetrievalMilvusClient {
	return &fakeRetrievalMilvusClient{collections: map[string]bool{}}
}

func (f *fakeRetrievalMilvusClient) CreateCollection(ctx context.Context, option milvusclient.CreateCollectionOption, callOptions ...any) error {
	return nil
}
func (f *fakeRetrievalMilvusClient) DropCollection(ctx context.Context, option milvusclient.DropCollectionOption, callOptions ...any) error {
	return nil
}
func (f *fakeRetrievalMilvusClient) HasCollection(ctx context.Context, option milvusclient.HasCollectionOption, callOptions ...any) (bool, error) {
	return true, nil
}
func (f *fakeRetrievalMilvusClient) DescribeCollection(ctx context.Context, option milvusclient.DescribeCollectionOption, callOptions ...any) (*entity.Collection, error) {
	return nil, fmt.Errorf("not found")
}
func (f *fakeRetrievalMilvusClient) ListCollections(ctx context.Context, option milvusclient.ListCollectionOption, callOptions ...any) ([]string, error) {
	return []string{}, nil
}
func (f *fakeRetrievalMilvusClient) GetCollectionStats(ctx context.Context, option milvusclient.GetCollectionStatsOption, callOptions ...any) (map[string]string, error) {
	return map[string]string{"row_count": "10"}, nil
}
func (f *fakeRetrievalMilvusClient) Insert(ctx context.Context, option milvusclient.InsertOption, callOptions ...any) (milvusclient.InsertResult, error) {
	if f.insertErr != nil {
		return milvusclient.InsertResult{}, f.insertErr
	}
	return milvusclient.InsertResult{}, nil
}
func (f *fakeRetrievalMilvusClient) Delete(ctx context.Context, option milvusclient.DeleteOption, callOptions ...any) (milvusclient.DeleteResult, error) {
	return milvusclient.DeleteResult{DeleteCount: f.deleteCount}, nil
}
func (f *fakeRetrievalMilvusClient) Flush(ctx context.Context, option milvusclient.FlushOption, callOptions ...any) error {
	f.flushCalled = true
	return nil
}
func (f *fakeRetrievalMilvusClient) Search(ctx context.Context, option milvusclient.SearchOption, callOptions ...any) ([]milvusclient.ResultSet, error) {
	return nil, nil
}
func (f *fakeRetrievalMilvusClient) HybridSearch(ctx context.Context, option milvusclient.HybridSearchOption, callOptions ...any) ([]milvusclient.ResultSet, error) {
	return nil, nil
}
func (f *fakeRetrievalMilvusClient) Query(ctx context.Context, option milvusclient.QueryOption, callOptions ...any) (milvusclient.ResultSet, error) {
	return milvusclient.ResultSet{}, f.queryErr
}
func (f *fakeRetrievalMilvusClient) DescribeIndex(ctx context.Context, option milvusclient.DescribeIndexOption, callOptions ...any) (milvusclient.IndexDescription, error) {
	return milvusclient.IndexDescription{}, nil
}
func (f *fakeRetrievalMilvusClient) LoadCollection(ctx context.Context, option milvusclient.LoadCollectionOption, callOptions ...any) error {
	return nil
}
func (f *fakeRetrievalMilvusClient) GetLoadState(ctx context.Context, option milvusclient.GetLoadStateOption, callOptions ...any) (entity.LoadState, error) {
	return entity.LoadState{State: entity.LoadStateLoaded}, nil
}
func (f *fakeRetrievalMilvusClient) ReleaseCollection(ctx context.Context, option milvusclient.ReleaseCollectionOption, callOptions ...any) error {
	return nil
}
func (f *fakeRetrievalMilvusClient) ListDatabase(ctx context.Context, option milvusclient.ListDatabaseOption, callOptions ...any) ([]string, error) {
	return []string{"default"}, nil
}
func (f *fakeRetrievalMilvusClient) CreateDatabase(ctx context.Context, option milvusclient.CreateDatabaseOption, callOptions ...any) error {
	return nil
}
func (f *fakeRetrievalMilvusClient) UseDatabase(ctx context.Context, option milvusclient.UseDatabaseOption, callOptions ...any) error {
	return nil
}
func (f *fakeRetrievalMilvusClient) Close(ctx context.Context) error {
	return nil
}

// newTestStoreWithFake 构建注入 fake client 的 store
func newTestStoreWithFake(t *testing.T, fake *fakeRetrievalMilvusClient) *MilvusVectorStore {
	t.Helper()
	config := common.VectorStoreConfig{
		StoreProvider:  common.StoreTypeMilvus,
		CollectionName: "test-collection",
		DistanceMetric: common.DistanceMetricCosine,
	}
	s, err := NewMilvusVectorStore(config, "http://localhost:19530", "",
		"content", vector_fields.NewMilvusAUTO("embedding"), "sparse_vector", "metadata", "document_id",
		WithMilvusClientOption(fake))
	require.NoError(t, err)
	return s
}

// TestMilvusVectorStore_Add 对齐 Python add：批量写入+每100条日志+flush
func TestMilvusVectorStore_Add(t *testing.T) {
	fake := newFakeRetrievalMilvusClient()
	s := newTestStoreWithFake(t, fake)

	// 数据中含向量字段
	data := []map[string]any{
		{"chunk_id": "c1", "document_id": "d1", "content": "text1", "metadata": map[string]any{"k": "v"}, "embedding": []float64{0.1, 0.2}},
		{"chunk_id": "c2", "document_id": "d1", "content": "text2", "metadata": map[string]any{}, "embedding": []float64{0.3, 0.4}},
	}
	err := s.Add(context.Background(), data)
	require.NoError(t, err)
	assert.True(t, fake.flushCalled) // 对齐 Python: 最后 flush
}

// TestMilvusVectorStore_Add_默认批大小 对齐 Python: batch_size=128
func TestMilvusVectorStore_Add_默认批大小(t *testing.T) {
	fake := newFakeRetrievalMilvusClient()
	s := newTestStoreWithFake(t, fake)

	data := make([]map[string]any, 300)
	for i := range data {
		data[i] = map[string]any{"chunk_id": fmt.Sprintf("c%d", i), "embedding": []float64{0.1}}
	}
	err := s.Add(context.Background(), data)
	require.NoError(t, err)
}

// TestMilvusVectorStore_Add_插入失败 对齐 Python: insert 抛错向上传播
func TestMilvusVectorStore_Add_插入失败(t *testing.T) {
	fake := newFakeRetrievalMilvusClient()
	fake.insertErr = fmt.Errorf("insert failed")
	s := newTestStoreWithFake(t, fake)

	err := s.Add(context.Background(), []map[string]any{{"chunk_id": "c1"}})
	assert.Error(t, err)
}

// TestMilvusVectorStore_Delete 对齐 Python delete：按 filterExpr 删除+flush
func TestMilvusVectorStore_Delete(t *testing.T) {
	fake := newFakeRetrievalMilvusClient()
	fake.deleteCount = 3
	s := newTestStoreWithFake(t, fake)

	ok, err := s.Delete(context.Background(), nil, map[string]any{"document_id": "d1"})
	require.NoError(t, err)
	assert.True(t, ok) // delete_count > 0
	assert.True(t, fake.flushCalled)
}

// TestMilvusVectorStore_Delete_零删除 对齐 Python: delete_count == 0 → False
func TestMilvusVectorStore_Delete_零删除(t *testing.T) {
	fake := newFakeRetrievalMilvusClient()
	fake.deleteCount = 0
	s := newTestStoreWithFake(t, fake)

	ok, err := s.Delete(context.Background(), nil, map[string]any{"document_id": "d1"})
	require.NoError(t, err)
	assert.False(t, ok)
}

// TestMilvusVectorStore_TableExists 对齐 Python table_exists
func TestMilvusVectorStore_TableExists(t *testing.T) {
	fake := newFakeRetrievalMilvusClient()
	s := newTestStoreWithFake(t, fake)

	ok, err := s.TableExists(context.Background(), "some_table")
	require.NoError(t, err)
	assert.True(t, ok) // fake HasCollection 返回 true
}

// TestMilvusVectorStore_DeleteTable 对齐 Python delete_table
func TestMilvusVectorStore_DeleteTable(t *testing.T) {
	fake := newFakeRetrievalMilvusClient()
	s := newTestStoreWithFake(t, fake)

	err := s.DeleteTable(context.Background(), "some_table")
	assert.NoError(t, err)
}

// TestMilvusVectorStore_ensureLoaded 对齐 Python _ensure_loaded：首次加载后缓存
func TestMilvusVectorStore_ensureLoaded(t *testing.T) {
	fake := newFakeRetrievalMilvusClient()
	s := newTestStoreWithFake(t, fake)

	err := s.ensureLoaded(context.Background())
	require.NoError(t, err)
	assert.True(t, s.collectionLoaded)
}
```

- [ ] **Step 3.2: 运行测试确认失败**

```bash
cd /home/opensource/uapclaw-gateway && go test ./internal/agentcore/retrieval/vector_store/ -run 'TestMilvusVectorStore_(Add|Delete|TableExists|DeleteTable|ensureLoaded)' -v 2>&1 | head -10
```

预期：编译失败，方法未定义

- [ ] **Step 3.3: 实现 Add/Delete/TableExists/DeleteTable/Close/ensureLoaded**

在 `milvus.go` 导出函数区追加（注意：`Add` 等方法必须满足 `VectorStore` 接口签名，参考 base.go）：

```go
// Add 添加向量数据。
//
// 对齐 Python MilvusVectorStore.add：
//   - _ensure_loaded 确保集合已加载
//   - 按 batch_size（默认128）分批 insert
//   - 每 100 条打进度日志
//   - 最后 flush
func (s *MilvusVectorStore) Add(ctx context.Context, data []map[string]any, opts ...StoreOption) error {
	if err := s.ensureLoaded(ctx); err != nil {
		return err
	}
	storeOpts := NewStoreOptions(opts...)
	batchSize := storeOpts.BatchSize
	if batchSize <= 0 {
		batchSize = defaultMilvusBatchSize
	}

	c, err := s.getClient(ctx)
	if err != nil {
		return err
	}

	processed := 0
	total := len(data)
	var cache []map[string]any
	for _, doc := range data {
		cache = append(cache, doc)
		if len(cache) >= batchSize {
			nodes := cache[:batchSize]
			cache = cache[batchSize:]
			if err := s.insertBatch(ctx, c, nodes); err != nil {
				return err
			}
			processed += len(nodes)
			// 对齐 Python: if processed % 100 == 0: logger.info("Written %d/%d records to %s", ...)
			if processed%100 == 0 {
				logger.Info(logComponent).
					Int("processed", processed).
					Int("total", total).
					Str("collection_name", s.collectionName).
					Msg("写入进度")
			}
		}
	}
	if len(cache) > 0 {
		if err := s.insertBatch(ctx, c, cache); err != nil {
			return err
		}
		processed += len(cache)
	}
	// 对齐 Python: logger.info("Writing completed, total %d/%d records to %s", ...)
	logger.Info(logComponent).
		Int("processed", processed).
		Int("total", total).
		Str("collection_name", s.collectionName).
		Msg("写入完成")

	// 对齐 Python: self._client.flush(self.collection_name)
	if err := c.Flush(ctx, milvusclient.NewFlushOption(s.collectionName)); err != nil {
		logger.Warn(logComponent).Err(err).Str("collection_name", s.collectionName).Msg("Flush 失败")
	}
	return nil
}

// Delete 删除向量。
//
// 对齐 Python MilvusVectorStore.delete：按 ids 或 filter_expr 删除 + flush。
func (s *MilvusVectorStore) Delete(ctx context.Context, ids []string, filterExpr map[string]any) (bool, error) {
	if err := s.ensureLoaded(ctx); err != nil {
		return false, err
	}
	c, err := s.getClient(ctx)
	if err != nil {
		return false, err
	}

	deleteOpt := milvusclient.NewDeleteOption(s.collectionName)
	// 对齐 Python: ids 非空时按主键删除（此处 ids 对应 pk，用 in 表达式）
	if len(ids) > 0 {
		quoted := make([]string, 0, len(ids))
		for _, id := range ids {
			quoted = append(quoted, fmt.Sprintf("%q", id))
		}
		deleteOpt = deleteOpt.WithExpr(fmt.Sprintf("pk in [%s]", strings.Join(quoted, ",")))
	} else if len(filterExpr) > 0 {
		// 对齐 Python: filter_expr 构建等值过滤
		deleteOpt = deleteOpt.WithExpr(buildMilvusFilterExpr(filterExpr))
	} else {
		return false, nil
	}

	result, err := c.Delete(ctx, deleteOpt)
	if err != nil {
		// 对齐 Python: except Exception: logger.error; return False
		logger.Error(logComponent).
			Str("event_type", "LLM_CALL_ERROR").
			Str("method", "Delete").
			Str("collection_name", s.collectionName).
			Err(err).
			Msg("删除向量失败")
		return false, err
	}
	if err := c.Flush(ctx, milvusclient.NewFlushOption(s.collectionName)); err != nil {
		logger.Warn(logComponent).Err(err).Str("collection_name", s.collectionName).Msg("Flush 失败")
	}
	// 对齐 Python: result.get("delete_count", 0) > 0
	return result.DeleteCount > 0, nil
}

// TableExists 检查集合是否存在。
// 对齐 Python MilvusVectorStore.table_exists。
func (s *MilvusVectorStore) TableExists(ctx context.Context, tableName string) (bool, error) {
	c, err := s.getClient(ctx)
	if err != nil {
		return false, err
	}
	return c.HasCollection(ctx, milvusclient.NewHasCollectionOption(tableName))
}

// DeleteTable 删除集合。
// 对齐 Python MilvusVectorStore.delete_table。
func (s *MilvusVectorStore) DeleteTable(ctx context.Context, tableName string) error {
	c, err := s.getClient(ctx)
	if err != nil {
		return err
	}
	return c.DropCollection(ctx, milvusclient.NewDropCollectionOption(tableName))
}

// Close 关闭存储。
//
// 对齐 Python MilvusVectorStore.close(release_collection=False)：
// release 时检查 load state 并 release，然后关闭 client。
func (s *MilvusVectorStore) Close(releaseCollection bool) {
	c, err := s.getClient(context.Background())
	if err != nil || c == nil {
		return
	}
	if releaseCollection {
		// 对齐 Python: if has_collection and LoadState == Loaded: release_collection
		if has, hasErr := c.HasCollection(ctx0(), milvusclient.NewHasCollectionOption(s.collectionName)); hasErr == nil && has {
			if state, stateErr := c.GetLoadState(ctx0(), milvusclient.NewGetLoadStateOption(s.collectionName)); stateErr == nil && state.State == entity.LoadStateLoaded {
				if err := c.ReleaseCollection(ctx0(), milvusclient.NewReleaseCollectionOption(s.collectionName)); err != nil {
					// 对齐 Python: logger.warning(f"Failed to release Milvus collection: {e}")
					logger.Warn(logComponent).Err(err).Str("collection_name", s.collectionName).Msg("释放集合失败")
				}
				s.mu.Lock()
				s.collectionLoaded = false
				s.mu.Unlock()
			}
		}
	}
	if err := c.Close(ctx0()); err != nil {
		// 对齐 Python: logger.warning(f"Failed to close Milvus client: {e}")
		logger.Warn(logComponent).Err(err).Msg("关闭 Milvus 客户端失败")
	}
}

// ensureLoaded 确保集合已加载。
//
// 对齐 Python MilvusVectorStore._ensure_loaded：
// 首次调用时 has_collection → load_collection，之后缓存跳过。
func (s *MilvusVectorStore) ensureLoaded(ctx context.Context) error {
	s.mu.RLock()
	loaded := s.collectionLoaded
	s.mu.RUnlock()
	if loaded {
		return nil
	}

	c, err := s.getClient(ctx)
	if err != nil {
		return err
	}

	// 对齐 Python: if self.client.has_collection(self.collection_name):
	has, err := c.HasCollection(ctx, milvusclient.NewHasCollectionOption(s.collectionName))
	if err != nil {
		return err
	}
	if !has {
		return nil
	}

	// 对齐 Python: logger.info("Retrieval Milvus Store: loading collection %s", ...)
	logger.Info(logComponent).Str("collection_name", s.collectionName).Msg("正在加载集合")
	if err := c.LoadCollection(ctx, milvusclient.NewLoadCollectionOption(s.collectionName)); err != nil {
		return err
	}
	// 对齐 Python: logger.info("Retrieval Milvus Store: %s collection loaded", ...)
	logger.Info(logComponent).Str("collection_name", s.collectionName).Msg("集合加载完成")

	s.mu.Lock()
	s.collectionLoaded = true
	s.mu.Unlock()
	return nil
}
```

在非导出函数区追加（Close 用的 ctx0 辅助 + insertBatch + buildMilvusFilterExpr）：

```go
// ctx0 返回后台上下文（Close 无 ctx 参数时使用）。
func ctx0() context.Context { return context.Background() }

// insertBatch 将一批 map 数据转为 Milvus 列并插入。
//
// 对齐 Python asyncio.to_thread(self._client.insert, collection_name=..., data=nodes)。
// 列类型推断：[]float64 → FloatVector、string → VarChar、map[string]any → JSON。
func (s *MilvusVectorStore) insertBatch(ctx context.Context, c milvusClient, nodes []map[string]any) error {
	if len(nodes) == 0 {
		return nil
	}
	columns, err := s.dataToColumns(nodes)
	if err != nil {
		return err
	}
	insertOpt := milvusclient.NewColumnBasedInsertOption(s.collectionName).WithColumns(columns...)
	if _, err := c.Insert(ctx, insertOpt); err != nil {
		return exception.BuildError(
			exception.StatusRetrievalVectorStoreProviderInvalid,
			exception.WithParam("error_msg", fmt.Sprintf("批量插入数据失败: %s", err)),
			exception.WithCause(err),
		)
	}
	return nil
}

// dataToColumns 将 map 数据列表转为 Milvus 列。
// 对齐 Python client.insert(data=[dict...]) 的 SDK 内部列转换。
func (s *MilvusVectorStore) dataToColumns(nodes []map[string]any) ([]column.Column, error) {
	// 收集所有字段名（保持确定顺序）
	fieldSet := make(map[string]bool)
	var fieldOrder []string
	for _, doc := range nodes {
		for k := range doc {
			if !fieldSet[k] {
				fieldSet[k] = true
				fieldOrder = append(fieldOrder, k)
			}
		}
	}

	var columns []column.Column
	for _, fieldName := range fieldOrder {
		// 找第一个非 nil 值判断类型
		var firstVal any
		for _, doc := range nodes {
			if v, ok := doc[fieldName]; ok && v != nil {
				firstVal = v
				break
			}
		}
		if firstVal == nil {
			continue
		}

		switch v := firstVal.(type) {
		case []float64:
			vecs := make([][]float32, 0, len(nodes))
			dim := len(v)
			for _, doc := range nodes {
				if f64, ok := doc[fieldName].([]float64); ok && len(f64) > 0 {
					f32 := make([]float32, len(f64))
					for i, f := range f64 {
						f32[i] = float32(f)
					}
					vecs = append(vecs, f32)
				} else {
					vecs = append(vecs, make([]float32, dim))
				}
			}
			if len(vecs) > 0 && dim > 0 {
				columns = append(columns, column.NewColumnFloatVector(fieldName, dim, vecs))
			}
		case string:
			strs := make([]string, 0, len(nodes))
			for _, doc := range nodes {
				if str, ok := doc[fieldName].(string); ok {
					strs = append(strs, str)
				} else {
					strs = append(strs, "")
				}
			}
			columns = append(columns, column.NewColumnVarChar(fieldName, strs))
		case map[string]any:
			// metadata 字段 → JSON 列
			jsonBytes := make([][]byte, 0, len(nodes))
			for _, doc := range nodes {
				if m, ok := doc[fieldName].(map[string]any); ok {
					bs, err := json.Marshal(m)
					if err != nil {
						return nil, fmt.Errorf("序列化字段 %s 失败: %w", fieldName, err)
					}
					jsonBytes = append(jsonBytes, bs)
				} else {
					jsonBytes = append(jsonBytes, []byte("{}"))
				}
			}
			columns = append(columns, column.NewColumnJSONBytes(fieldName, jsonBytes))
		}
	}
	return columns, nil
}

// buildMilvusFilterExpr 构建等值过滤表达式。
//
// 对齐 Python MilvusVectorStore.search 中的 filter 构建逻辑：
// 字符串值用 QueryExpr.sanitize_str 转义，非字符串直接拼接，条件用 " && " 连接。
func buildMilvusFilterExpr(filters map[string]any) string {
	if len(filters) == 0 {
		return ""
	}
	var parts []string
	for key, value := range filters {
		if str, ok := value.(string); ok {
			parts = append(parts, fmt.Sprintf("%s == %s", key, query.SanitizeStr(str)))
		} else {
			parts = append(parts, fmt.Sprintf("%s == %v", key, value))
		}
	}
	return strings.Join(parts, " && ")
}
```

**注意**：
1. `buildMilvusFilterExpr` 需要 import `"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/store/query"`，key 顺序在 Go map 遍历中不确定——Python dict 是有序的。为保证确定性，对 key 排序：

```go
func buildMilvusFilterExpr(filters map[string]any) string {
	if len(filters) == 0 {
		return ""
	}
	keys := make([]string, 0, len(filters))
	for k := range filters {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var parts []string
	for _, key := range keys {
		value := filters[key]
		if str, ok := value.(string); ok {
			parts = append(parts, fmt.Sprintf("%s == %s", key, query.SanitizeStr(str)))
		} else {
			parts = append(parts, fmt.Sprintf("%s == %v", key, value))
		}
	}
	return strings.Join(parts, " && ")
}
```

2. `dataToColumns` 需要 import `"encoding/json"`
3. Close 方法签名 `Close(releaseCollection bool)` 与 VectorStore 接口 `Close()` 不一致——**决策**：接口要求 `Close()`。查看 Python：MilvusVectorStore.close(release_collection=False)。Go 接口已定为 `Close()`。实现两个方法：`Close()` 调 `CloseWithRelease(false)`。修正：

```go
// Close 关闭存储（不释放集合）。
// 实现 VectorStore 接口。
func (s *MilvusVectorStore) Close() {
	s.CloseWithRelease(false)
}

// CloseWithRelease 关闭存储，可选释放集合。
// 对齐 Python MilvusVectorStore.close(release_collection=False)。
func (s *MilvusVectorStore) CloseWithRelease(releaseCollection bool) {
	// ... 上面的 Close 逻辑
}
```

同时测试改为 `s.Close()`。更新测试中 `TestMilvusVectorStore_ensureLoaded` 用 `s.Close()` 即可。

4. 还需要 `var _ VectorStore = (*MilvusVectorStore)(nil)` 编译期接口断言（放在全局变量区）。

- [ ] **Step 3.4: 运行测试确认通过**

```bash
cd /home/opensource/uapclaw-gateway && go test ./internal/agentcore/retrieval/vector_store/ -run 'TestMilvusVectorStore' -v 2>&1 | tail -25
```

预期：全部 PASS

- [ ] **Step 3.5: 提交**

```bash
cd /home/opensource/uapclaw-gateway && git add internal/agentcore/retrieval/vector_store/ && git commit -m "feat(retrieval): 13.3 MilvusVectorStore Add/Delete/TableExists/DeleteTable/Close/ensureLoaded"
```

---

### Task 4: MilvusVectorStore — Search/SparseSearch/HybridSearch + 结果转换

**Files:**
- Modify: `internal/agentcore/retrieval/vector_store/milvus.go`
- Test: `internal/agentcore/retrieval/vector_store/milvus_test.go`

- [ ] **Step 4.1: 在测试文件追加失败测试**

```go
// buildTestResultSet 构建测试用搜索结果集
func buildTestResultSet(fieldValues map[string][]any, scores []float32) milvusclient.ResultSet {
	// 通过真实 column 构造 Fields
	var fields milvusclient.DataSet
	for name, values := range fieldValues {
		if len(values) == 0 {
			continue
		}
		switch values[0].(type) {
		case string:
			strs := make([]string, len(values))
			for i, v := range values {
				strs[i], _ = v.(string)
			}
			fields = append(fields, column.NewColumnVarChar(name, strs))
		case map[string]any:
			jsonVals := make([][]byte, len(values))
			for i, v := range values {
				bs, _ := json.Marshal(v)
				jsonVals[i] = bs
			}
			fields = append(fields, column.NewColumnJSONBytes(name, jsonVals))
		}
	}
	return milvusclient.ResultSet{
		ResultCount: len(scores),
		Fields:      fields,
		Scores:      scores,
	}
}

// TestMilvusVectorStore_Search 对齐 Python search：向量搜索+分数归一化
func TestMilvusVectorStore_Search(t *testing.T) {
	fake := newFakeRetrievalMilvusClient()
	fake.searchResults = []milvusclient.ResultSet{buildTestResultSet(
		map[string][]any{
			"content":     {"hello"},
			"metadata":    {map[string]any{"k": "v"}},
			"document_id": {"d1"},
			"chunk_id":    {"c1"},
		},
		[]float32{0.9},
	)}
	s := newTestStoreWithFake(t, fake)

	results, err := s.Search(context.Background(), []float64{0.1, 0.2}, 5, nil)
	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.Equal(t, "hello", results[0].Text)
	assert.Equal(t, "d1", results[0].Metadata["doc_id"]) // 对齐 Python: doc_id 回填到 metadata
}

// TestMilvusVectorStore_Search_L2分数 对齐 Python: convert_l2_squared
func TestMilvusVectorStore_Search_L2分数(t *testing.T) {
	config := common.VectorStoreConfig{
		StoreProvider:  common.StoreTypeMilvus,
		CollectionName: "test-collection",
		DistanceMetric: common.DistanceMetricEuclidean,
	}
	fake := newFakeRetrievalMilvusClient()
	fake.searchResults = []milvusclient.ResultSet{buildTestResultSet(
		map[string][]any{"content": {"hello"}}, []float32{0.5})}
	s, err := NewMilvusVectorStore(config, "http://localhost:19530", "",
		"content", vector_fields.NewMilvusAUTO("embedding"), "sparse_vector", "metadata", "document_id",
		WithMilvusClientOption(fake))
	require.NoError(t, err)

	results, err := s.Search(context.Background(), []float64{0.1}, 5, nil)
	require.NoError(t, err)
	// L2: convert_l2_squared(0.5) = (maxDist - d) / maxDist，参考 foundation utils
	expected := vector.ConvertL2Squared(0.5, 0)
	assert.InDelta(t, expected, results[0].Score, 1e-9)
	assert.Equal(t, 0.5, results[0].Metadata["raw_score"]) // 对齐 Python: raw_score 写入 metadata
}

// TestMilvusVectorStore_SparseSearch 对齐 Python sparse_search（BM25）
func TestMilvusVectorStore_SparseSearch(t *testing.T) {
	fake := newFakeRetrievalMilvusClient()
	fake.searchResults = []milvusclient.ResultSet{buildTestResultSet(
		map[string][]any{"content": {"hello"}, "document_id": {"d1"}},
		[]float32{2.5})}
	s := newTestStoreWithFake(t, fake)

	results, err := s.SparseSearch(context.Background(), "query text", 5, nil)
	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.Equal(t, 2.5, results[0].Score) // sparse: final_score = raw_score
}

// TestMilvusVectorStore_SparseSearch_失败返回空 对齐 Python: except → warning + return []
func TestMilvusVectorStore_SparseSearch_失败返回空(t *testing.T) {
	fake := newFakeRetrievalMilvusClient()
	fake.searchErr = fmt.Errorf("bm25 failed")
	s := newTestStoreWithFake(t, fake)

	results, err := s.SparseSearch(context.Background(), "query", 5, nil)
	require.NoError(t, err) // 对齐 Python: 失败不抛错
	assert.Empty(t, results)
}

// TestMilvusVectorStore_HybridSearch 对齐 Python hybrid_search：原生 HybridSearch + RRF
func TestMilvusVectorStore_HybridSearch(t *testing.T) {
	fake := newFakeRetrievalMilvusClient()
	fake.hybridResults = []milvusclient.ResultSet{buildTestResultSet(
		map[string][]any{"content": {"hello"}, "document_id": {"d1"}},
		[]float32{0.8})}
	s := newTestStoreWithFake(t, fake)

	results, err := s.HybridSearch(context.Background(), "query", []float64{0.1, 0.2}, 5, 0.5, nil)
	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.Equal(t, "hello", results[0].Text)
}

// TestMilvusVectorStore_HybridSearch_降级 对齐 Python: 失败 fallback 到分别搜索
func TestMilvusVectorStore_HybridSearch_降级(t *testing.T) {
	fake := newFakeRetrievalMilvusClient()
	fake.hybridErr = fmt.Errorf("hybrid failed")
	fake.searchResults = []milvusclient.ResultSet{buildTestResultSet(
		map[string][]any{"content": {"dense result"}}, []float32{0.9})}
	s := newTestStoreWithFake(t, fake)

	results, err := s.HybridSearch(context.Background(), "query", []float64{0.1, 0.2}, 5, 0.5, nil)
	require.NoError(t, err)
	assert.NotEmpty(t, results) // fallback 生效：至少有 dense 结果
}

// TestMilvusVectorStore_HybridSearch_无向量 对齐 Python: query_vector 为 None 时只用 sparse
func TestMilvusVectorStore_HybridSearch_无向量(t *testing.T) {
	fake := newFakeRetrievalMilvusClient()
	fake.hybridResults = []milvusclient.ResultSet{buildTestResultSet(
		map[string][]any{"content": {"sparse result"}}, []float32{0.7})}
	s := newTestStoreWithFake(t, fake)

	results, err := s.HybridSearch(context.Background(), "query", nil, 5, 0.5, nil)
	require.NoError(t, err)
	assert.NotEmpty(t, results)
}

// TestMilvusVectorStore_milvusResultToSearchResults 对齐 Python _milvus_result_to_search_results
func TestMilvusVectorStore_milvusResultToSearchResults(t *testing.T) {
	s := newTestMilvusStore(t)
	rs := buildTestResultSet(
		map[string][]any{
			"content":     {"text1", "text2"},
			"metadata":    {map[string]any{"k": "v1"}, map[string]any{"k": "v2"}},
			"document_id": {"d1", "d2"},
			"chunk_id":    {"c1", "c2"},
		},
		[]float32{0.9, 0.8},
	)
	results := s.milvusResultToSearchResults(rs, "vector")
	require.Len(t, results, 2)
	assert.Equal(t, "d1", results[0].Metadata["doc_id"])
	assert.Equal(t, "c1", results[0].Metadata["chunk_id"]) // 对齐 Python: chunk_id 写入 metadata
	assert.Equal(t, 0.9, results[0].Metadata["raw_score"])
}

// TestMilvusVectorStore_getSearchParams 对齐 Python get_search_params：efSearchFactor → ef
func TestMilvusVectorStore_getSearchParams(t *testing.T) {
	config := common.VectorStoreConfig{
		StoreProvider:  common.StoreTypeMilvus,
		CollectionName: "test-collection",
		DistanceMetric: common.DistanceMetricCosine,
	}
	hnsw := vector_fields.NewMilvusHNSW("embedding", 30, 360, 2.0)
	s, err := NewMilvusVectorStore(config, "http://localhost:19530", "",
		"content", hnsw, "sparse_vector", "metadata", "document_id")
	require.NoError(t, err)

	params := s.getSearchParams(10)
	// 对齐 Python: ef = round(top_k * efSearchFactor) = round(10 * 2.0) = 20
	assert.Equal(t, float64(20), params["ef"])
	assert.NotContains(t, params, "efSearchFactor") // pop 掉
}
```

fake client 需要扩展（在 fakeRetrievalMilvusClient 中追加字段和方法实现修改）：

```go
// 在 struct 中追加：
	searchResults  []milvusclient.ResultSet
	hybridResults  []milvusclient.ResultSet
	searchErr      error
	hybridErr      error
	loadedOnce     bool

// Search 方法改为：
func (f *fakeRetrievalMilvusClient) Search(ctx context.Context, option milvusclient.SearchOption, callOptions ...any) ([]milvusclient.ResultSet, error) {
	if f.searchErr != nil {
		return nil, f.searchErr
	}
	return f.searchResults, nil
}

// HybridSearch 方法改为：
func (f *fakeRetrievalMilvusClient) HybridSearch(ctx context.Context, option milvusclient.HybridSearchOption, callOptions ...any) ([]milvusclient.ResultSet, error) {
	if f.hybridErr != nil {
		return nil, f.hybridErr
	}
	return f.hybridResults, nil
}
```

测试文件 import 需要追加 `"encoding/json"` 和 `"github.com/milvus-io/milvus/client/v2/column"` 和 `"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/store/vector"`。

注意 `newTestMilvusStore`（Task 2 中定义）现在需要返回能构建 ResultSet 的实例——它没有注入 fake，但 `milvusResultToSearchResults` 是纯函数测试不需要 client。

- [ ] **Step 4.2: 运行测试确认失败**

```bash
cd /home/opensource/uapclaw-gateway && go test ./internal/agentcore/retrieval/vector_store/ -run 'TestMilvusVectorStore_(Search|SparseSearch|HybridSearch|milvusResult|getSearchParams)' -v 2>&1 | head -10
```

- [ ] **Step 4.3: 实现 Search/SparseSearch/HybridSearch/结果转换/getSearchParams**

在导出函数区追加：

```go
// Search 向量搜索。
//
// 对齐 Python MilvusVectorStore.search：
//   - _ensure_loaded
//   - 构建 filter 表达式（dict → 等值过滤 / QueryExpr → to_expr("milvus")）
//   - client.search（anns_field + search_params 含 metric_type）
//   - 结果转 SearchResult（vector 模式分数归一化）
func (s *MilvusVectorStore) Search(ctx context.Context, queryVector []float64, topK int, filters map[string]any, _ ...StoreOption) ([]common.SearchResult, error) {
	if err := s.ensureLoaded(ctx); err != nil {
		return nil, err
	}
	c, err := s.getClient(ctx)
	if err != nil {
		return nil, err
	}

	// 对齐 Python: output_fields = [text_field, metadata_field, doc_id_field, "chunk_id"]
	outputFields := []string{s.textField, s.metadataField, s.docIDField, "chunk_id"}

	// 对齐 Python: filter_expr 构建
	filterExpr := buildMilvusFilterExpr(filters)

	// 构建查询向量
	f32 := make([]float32, len(queryVector))
	for i, v := range queryVector {
		f32[i] = float32(v)
	}
	vectors := []entity.Vector{entity.FloatVector(f32)}

	// 对齐 Python: search_params={"metric_type": self._distance_metric, "params": self.get_search_params(top_k)}
	searchOpt := milvusclient.NewSearchOption(s.collectionName, topK, vectors).
		WithANNSField(s.vectorField.VectorFieldName()).
		WithSearchParam("metric_type", s.distanceMetric).
		WithOutputFields(outputFields...)
	for k, v := range s.getSearchParams(topK) {
		searchOpt = searchOpt.WithSearchParam(k, fmt.Sprintf("%v", v))
	}
	if filterExpr != "" {
		searchOpt = searchOpt.WithFilter(filterExpr)
	}

	resultSets, err := c.Search(ctx, searchOpt)
	if err != nil {
		logger.Error(logComponent).
			Str("event_type", "LLM_CALL_ERROR").
			Str("method", "Search").
			Str("collection_name", s.collectionName).
			Err(err).
			Msg("向量搜索失败")
		return nil, err
	}

	// 对齐 Python: if results and len(results) > 0: return self._milvus_result_to_search_results(results[0], mode="vector")
	if len(resultSets) > 0 {
		return s.milvusResultToSearchResults(resultSets[0], "vector"), nil
	}
	return nil, nil
}

// SparseSearch 稀疏搜索（BM25）。
//
// 对齐 Python MilvusVectorStore.sparse_search：
// 传递查询文本（BM25 Function 自动分词），失败时 warning + 返回空。
func (s *MilvusVectorStore) SparseSearch(ctx context.Context, queryText string, topK int, filters map[string]any, _ ...StoreOption) ([]common.SearchResult, error) {
	if err := s.ensureLoaded(ctx); err != nil {
		return nil, err
	}
	c, err := s.getClient(ctx)
	if err != nil {
		return nil, err
	}

	// 对齐 Python: output_fields = [text_field, metadata_field, doc_id_field]
	outputFields := []string{s.textField, s.metadataField, s.docIDField}
	filterExpr := buildMilvusFilterExpr(filters)

	// 对齐 Python: data=[query_text] 直接传文本，BM25 function 处理分词
	vectors := []entity.Vector{entity.Text(queryText)}
	searchOpt := milvusclient.NewSearchOption(s.collectionName, topK, vectors).
		WithANNSField(s.sparseVectorField).
		WithSearchParam("metric_type", "BM25").
		WithOutputFields(outputFields...)
	if filterExpr != "" {
		searchOpt = searchOpt.WithFilter(filterExpr)
	}

	resultSets, err := c.Search(ctx, searchOpt)
	if err != nil {
		// 对齐 Python: except Exception as e: logger.warning(f"BM25 text search failed: {e}"); return []
		logger.Warn(logComponent).
			Str("method", "SparseSearch").
			Str("query_text", truncateText(queryText, 100)).
			Err(err).
			Msg("BM25 文本搜索失败")
		return nil, nil
	}

	if len(resultSets) > 0 {
		return s.milvusResultToSearchResults(resultSets[0], "sparse"), nil
	}
	return nil, nil
}

// HybridSearch 混合搜索（原生 HybridSearch + RRF）。
//
// 对齐 Python MilvusVectorStore.hybrid_search：
//   - query_vector 非 nil 时构建 dense AnnRequest
//   - 始终构建 sparse BM25 AnnRequest
//   - 原生 hybrid_search + RRFRanker(k=60)
//   - 失败时降级到分别搜索后 RRF 融合
func (s *MilvusVectorStore) HybridSearch(ctx context.Context, queryText string, queryVector []float64, topK int, alpha float64, filters map[string]any, _ ...StoreOption) ([]common.SearchResult, error) {
	if err := s.ensureLoaded(ctx); err != nil {
		return nil, err
	}
	c, err := s.getClient(ctx)
	if err != nil {
		return nil, err
	}

	// 对齐 Python: output_fields = [text_field, metadata_field, doc_id_field]
	outputFields := []string{s.textField, s.metadataField, s.docIDField}
	filterExpr := buildMilvusFilterExpr(filters)

	var annRequests []*milvusclient.AnnRequest

	// 对齐 Python: if query_vector is not None: dense_req = AnnSearchRequest(...)
	if queryVector != nil {
		f32 := make([]float32, len(queryVector))
		for i, v := range queryVector {
			f32[i] = float32(v)
		}
		denseReq := milvusclient.NewAnnRequest(s.vectorField.VectorFieldName(), topK, entity.FloatVector(f32)).
			WithSearchParam("metric_type", s.distanceMetric)
		for k, v := range s.getSearchParams(topK) {
			denseReq = denseReq.WithSearchParam(k, fmt.Sprintf("%v", v))
		}
		if filterExpr != "" {
			denseReq = denseReq.WithFilter(filterExpr)
		}
		annRequests = append(annRequests, denseReq)
	}

	// 对齐 Python: sparse_req = AnnSearchRequest(data=[query_text], anns_field=sparse_vector_field, param={"metric_type": "BM25"}, ...)
	sparseReq := milvusclient.NewAnnRequest(s.sparseVectorField, topK, entity.Text(queryText)).
		WithSearchParam("metric_type", "BM25")
	if filterExpr != "" {
		sparseReq = sparseReq.WithFilter(filterExpr)
	}
	annRequests = append(annRequests, sparseReq)

	// 对齐 Python: if not search_requests: return []
	if len(annRequests) == 0 {
		return nil, nil
	}

	// 对齐 Python: ranker=RRFRanker(k=60)
	reranker := milvusclient.NewRRFReranker().WithK(60)
	searchOpt := milvusclient.NewHybridSearchOption(s.collectionName, topK, annRequests...).
		WithOutputFields(outputFields...).
		WithReranker(reranker)

	resultSets, err := c.HybridSearch(ctx, searchOpt)
	if err != nil {
		// 对齐 Python: logger.warning(f"Hybrid search failed, falling back to separate searches: {e}")
		logger.Warn(logComponent).
			Str("method", "HybridSearch").
			Str("collection_name", s.collectionName).
			Err(err).
			Msg("混合搜索失败，降级到分别搜索")
		return s.hybridSearchFallback(ctx, queryText, queryVector, topK, filters)
	}

	if len(resultSets) > 0 {
		return s.milvusResultToSearchResults(resultSets[0], "hybrid"), nil
	}
	return nil, nil
}
```

在非导出函数区追加：

```go
// hybridSearchFallback 降级混合搜索：分别执行向量搜索和 BM25 搜索后 RRF 融合。
//
// 对齐 Python MilvusVectorStore._hybrid_search_fallback。
func (s *MilvusVectorStore) hybridSearchFallback(ctx context.Context, queryText string, queryVector []float64, topK int, filters map[string]any) ([]common.SearchResult, error) {
	// 对齐 Python: asyncio.create_task 并发两路搜索
	type searchOut struct {
		results []common.SearchResult
		err     error
	}
	outCh := make(chan searchOut, 2)

	go func() {
		if queryVector != nil {
			// 对齐 Python: self.search(query_vector, top_k, None)
			results, err := s.Search(ctx, queryVector, topK, nil)
			outCh <- searchOut{results: results, err: err}
		} else {
			outCh <- searchOut{}
		}
	}()
	go func() {
		// 对齐 Python: self.sparse_search(query_text, top_k, None)
		results, err := s.SparseSearch(ctx, queryText, topK, nil)
		outCh <- searchOut{results: results, err: err}
	}()

	var resultsList [][]common.RetrievalResult
	idMapping := make(map[string]string)
	for i := 0; i < 2; i++ {
		out := <-outCh
		if out.err != nil || len(out.results) == 0 {
			continue
		}
		var retrievalResults []common.RetrievalResult
		for _, sr := range out.results {
			idMapping[sr.Text] = sr.ID
			metadata := make(map[string]any)
			for k, v := range sr.Metadata {
				metadata[k] = v
			}
			metadata["id"] = sr.ID
			docID, _ := sr.Metadata[s.docIDField].(string)
			chunkID, _ := sr.Metadata["chunk_id"].(string)
			retrievalResults = append(retrievalResults, common.RetrievalResult{
				Text:     sr.Text,
				Score:    sr.Score,
				Metadata: metadata,
				DocID:    docID,
				ChunkID:  chunkID,
			})
		}
		resultsList = append(resultsList, retrievalResults)
	}

	if len(resultsList) == 0 {
		return nil, nil
	}

	// 对齐 Python: rrf_fusion([results_vector, results_text], k=60)
	fusedRetrievalResults := utils.RRFFusion(resultsList, 60)

	var fusedResults []common.SearchResult
	for i, rr := range fusedRetrievalResults {
		if i >= topK {
			break
		}
		resultID, _ := rr.Metadata["id"].(string)
		if resultID == "" {
			resultID = idMapping[rr.Text]
		}
		metadata := make(map[string]any)
		for k, v := range rr.Metadata {
			if k != "id" {
				metadata[k] = v
			}
		}
		fusedResults = append(fusedResults, common.SearchResult{
			ID:       resultID,
			Text:     rr.Text,
			Score:    rr.Score,
			Metadata: metadata,
		})
	}
	return fusedResults, nil
}

// milvusResultToSearchResults 将 Milvus 搜索结果转为 SearchResult 列表。
//
// 对齐 Python MilvusVectorStore._milvus_result_to_search_results：
//   - 提取 text/metadata/doc_id/chunk_id
//   - doc_id 回填到 metadata（避免重复）
//   - chunk_id 写入 metadata（供上层关联）
//   - vector 模式按距离度量归一化分数；sparse/hybrid 直接用原始分数
//   - raw_score / raw_score_scaled 写入 metadata
func (s *MilvusVectorStore) milvusResultToSearchResults(rs milvusclient.ResultSet, mode string) []common.SearchResult {
	rowCount := rs.ResultCount
	if rowCount == 0 {
		return nil
	}

	// 收集列数据
	colData := make(map[string][]any)
	for _, col := range rs.Fields {
		name := col.Name()
		values := make([]any, rowCount)
		for j := 0; j < rowCount; j++ {
			v, err := col.Get(j)
			if err != nil {
				continue
			}
			values[j] = v
		}
		colData[name] = values
	}

	var searchResults []common.SearchResult
	for j := 0; j < rowCount; j++ {
		// 对齐 Python: result_id = str(item.get("id", item.get("pk", "")))
		resultID := ""
		if ids := rs.IDs; ids != nil {
			if v, err := ids.Get(j); err == nil {
				resultID = fmt.Sprintf("%v", v)
			}
		}

		// 对齐 Python: text = item.get(self.text_field, "")
		text := ""
		if vals := colData[s.textField]; j < len(vals) {
			if str, ok := vals[j].(string); ok {
				text = str
			}
		}

		// 对齐 Python: metadata = item.get(self.metadata_field, {})；str 时 json.loads
		metadata := make(map[string]any)
		if vals := colData[s.metadataField]; j < len(vals) {
			switch m := vals[j].(type) {
			case map[string]any:
				for k, v := range m {
					metadata[k] = v
				}
			case string:
				var parsed map[string]any
				if err := json.Unmarshal([]byte(m), &parsed); err == nil {
					for k, v := range parsed {
						metadata[k] = v
					}
				}
			case []byte:
				var parsed map[string]any
				if err := json.Unmarshal(m, &parsed); err == nil {
					for k, v := range parsed {
						metadata[k] = v
					}
				}
			}
		}

		// 对齐 Python: if "doc_id" not in metadata: metadata["doc_id"] = item.pop(doc_id_field, None)
		if _, ok := metadata["doc_id"]; !ok {
			if vals := colData[s.docIDField]; j < len(vals) {
				if str, ok := vals[j].(string); ok {
					metadata["doc_id"] = str
				}
			}
		}

		// 对齐 Python: if item.get("chunk_id") is not None: metadata.setdefault("chunk_id", ...)
		if vals := colData["chunk_id"]; j < len(vals {
			if str, ok := vals[j].(string); ok && str != "" {
				metadata["chunk_id"] = str
			}
		}

		// 对齐 Python: raw_score = item.get("score", item.get("distance", None))
		var rawScoreVal *float64
		if j < len(rs.Scores) {
			v := float64(rs.Scores[j])
			rawScoreVal = &v
		}

		var finalScore float64
		var rawScoreScaled *float64

		if mode == "vector" && rawScoreVal != nil {
			// 对齐 Python: L2/COSINE/IP 三种归一化
			switch s.distanceMetric {
			case "L2":
				scaled := vector.ConvertL2Squared(*rawScoreVal, 0)
				rawScoreScaled = &scaled
				finalScore = scaled
			case "COSINE":
				scaled := vector.ConvertCosineSimilarity(*rawScoreVal)
				rawScoreScaled = &scaled
				finalScore = scaled
			default: // IP
				scaled := vector.ConvertIPSimilarity(*rawScoreVal)
				rawScoreScaled = &scaled
				finalScore = scaled
			}
		} else if rawScoreVal != nil {
			// 对齐 Python: sparse/hybrid: final_score = raw_score
			finalScore = *rawScoreVal
		}

		// 对齐 Python: metadata.setdefault("raw_score", raw_score_val)
		if rawScoreVal != nil {
			metadata["raw_score"] = *rawScoreVal
		}
		if rawScoreScaled != nil {
			metadata["raw_score_scaled"] = *rawScoreScaled
		}

		searchResults = append(searchResults, common.SearchResult{
			ID:       resultID,
			Text:     text,
			Score:    finalScore,
			Metadata: metadata,
		})
	}
	return searchResults
}

// getSearchParams 获取搜索参数。
//
// 对齐 Python MilvusVectorStore.get_search_params：
// efSearchFactor 存在时转为 ef = round(top_k * efSearchFactor)。
func (s *MilvusVectorStore) getSearchParams(topK int) map[string]any {
	// 对齐 Python: search_params = self._search_config
	searchParams := make(map[string]any)
	for k, v := range s.searchConfig {
		searchParams[k] = v
	}
	// 对齐 Python: if "efSearchFactor" in search_params: ef = round(top_k * pop("efSearchFactor"))
	if factor, ok := searchParams["efSearchFactor"]; ok {
		delete(searchParams, "efSearchFactor")
		if f, ok := factor.(float64); ok {
			searchParams["ef"] = float64(math.Round(float64(topK) * f))
		}
	}
	return searchParams
}
```

import 追加：`"math"`、`"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/store/vector"`、`"github.com/uapclaw/uapclaw-go/internal/agentcore/retrieval/utils"`。

**注意**：`truncateText` 已在 chroma.go 定义（同包），直接复用。

**注意**：Task 4 代码中有一处笔误 `if vals := colData["chunk_id"]; j < len(vals {` 少了右括号，正确代码：

```go
		if vals := colData["chunk_id"]; j < len(vals) {
```

- [ ] **Step 4.4: 运行测试确认通过**

```bash
cd /home/opensource/uapclaw-gateway && go test ./internal/agentcore/retrieval/vector_store/ -run 'TestMilvusVectorStore' -v 2>&1 | tail -30
```

预期：全部 PASS（含 Task 2/3 的测试）

- [ ] **Step 4.5: 提交**

```bash
cd /home/opensource/uapclaw-gateway && git add internal/agentcore/retrieval/vector_store/ && git commit -m "feat(retrieval): 13.3 MilvusVectorStore Search/SparseSearch/HybridSearch+RRF降级+结果转换"
```

---

### Task 5: MilvusVectorStore — CheckVectorField

**Files:**
- Modify: `internal/agentcore/retrieval/vector_store/milvus.go`
- Test: `internal/agentcore/retrieval/vector_store/milvus_test.go`

- [ ] **Step 5.1: 追加失败测试**

```go
// TestMilvusVectorStore_CheckVectorField_集合不存在 对齐 Python: not has_collection → return
func TestMilvusVectorStore_CheckVectorField_集合不存在(t *testing.T) {
	fake := newFakeRetrievalMilvusClient()
	fake.hasCollection = false
	s := newTestStoreWithFake(t, fake)

	err := s.CheckVectorField()
	assert.NoError(t, err)
}

// TestMilvusVectorStore_CheckVectorField_索引描述为空 对齐 Python: actual 为空时抛 KB_DATABASE_CONFIG_INVALID
func TestMilvusVectorStore_CheckVectorField_索引描述为空(t *testing.T) {
	fake := newFakeRetrievalMilvusClient()
	fake.hasCollection = true
	fake.describeIndexErr = fmt.Errorf("index not found")
	fake.describeCollectionFields = []*entity.Field{} // 无向量字段
	s := newTestStoreWithFake(t, fake)

	err := s.CheckVectorField()
	assert.Error(t, err) // 对齐 Python: 提示 collection 中无向量字段
}

// TestMilvusVectorStore_CheckVectorField_配置匹配 对齐 Python: index_type 匹配 + CheckConfigsMatching 通过
func TestMilvusVectorStore_CheckVectorField_配置匹配(t *testing.T) {
	fake := newFakeRetrievalMilvusClient()
	fake.hasCollection = true
	// AUTO 索引跳过 index_type 检查
	s := newTestStoreWithFake(t, fake)

	err := s.CheckVectorField()
	assert.NoError(t, err)
}
```

fake 扩展：

```go
// struct 追加：
	hasCollection          bool
	describeIndexErr       error
	describeCollectionFields []*entity.Field

// HasCollection 改为：
func (f *fakeRetrievalMilvusClient) HasCollection(ctx context.Context, option milvusclient.HasCollectionOption, callOptions ...any) (bool, error) {
	if f.hasCollection {
		return true, nil
	}
	return f.collections[""] , nil
}

// DescribeIndex 改为：
func (f *fakeRetrievalMilvusClient) DescribeIndex(ctx context.Context, option milvusclient.DescribeIndexOption, callOptions ...any) (milvusclient.IndexDescription, error) {
	if f.describeIndexErr != nil {
		return milvusclient.IndexDescription{}, f.describeIndexErr
	}
	return milvusclient.IndexDescription{}, nil
}

// DescribeCollection 改为：
func (f *fakeRetrievalMilvusClient) DescribeCollection(ctx context.Context, option milvusclient.DescribeCollectionOption, callOptions ...any) (*entity.Collection, error) {
	return &entity.Collection{Schema: entity.Schema{Fields: f.describeCollectionFields}}, nil
}
```

**注意**：`HasCollection` 的行为改动会影响 Task 3 的 `TableExists`/`ensureLoaded` 测试——fake 默认 `hasCollection=false` 会让 ensureLoaded 直接跳过 load。为保持 Task 3 测试有效，fake 默认应返回 true：`hasCollection` 字段默认 false 改为在 `newFakeRetrievalMilvusClient` 里设 `hasCollection: true`。同时 `collections` map 逻辑简化为直接返回 `f.hasCollection`。

- [ ] **Step 5.2: 运行测试确认失败**

- [ ] **Step 5.3: 实现 CheckVectorField**

```go
// CheckVectorField 校验向量字段配置是否与实际数据库一致。
//
// 对齐 Python MilvusVectorStore.check_vector_field：
//   - 集合不存在时直接返回
//   - describe_index 为空时：列出集合中的向量字段并抛 KB_DATABASE_CONFIG_INVALID
//   - index_type 非 auto 时校验前缀+variant 匹配
//   - 最后 _check_configs_matching 校验构造参数
func (s *MilvusVectorStore) CheckVectorField() error {
	c, err := s.getClient(context.Background())
	if err != nil {
		return err
	}
	// 对齐 Python: if not self._client.has_collection(...): return
	has, err := c.HasCollection(context.Background(), milvusclient.NewHasCollectionOption(s.collectionName))
	if err != nil || !has {
		return err
	}

	// 对齐 Python: actual = self._client.describe_index(collection_name, field_name) or {}
	indexType := s.vectorField.IndexType()
	fieldName := s.vectorField.VectorFieldName()
	actual, idxErr := c.DescribeIndex(context.Background(), milvusclient.NewDescribeIndexOption(s.collectionName, fieldName))
	if idxErr != nil || actual.Index == nil {
		// 对齐 Python: 索引不存在时列出向量字段并报错
		coll, collErr := c.DescribeCollection(context.Background(), milvusclient.NewDescribeCollectionOption(s.collectionName))
		var vFieldsList string
		if collErr == nil && coll != nil {
			for _, v := range coll.Schema.Fields {
				if v.DataType == entity.FieldTypeFloatVector {
					vFieldsList += fmt.Sprintf("- [%d] %s: %v\n", v.FieldID, v.Name, v.TypeParams)
				}
			}
		}
		return exception.BuildError(
			exception.StatusRetrievalKbDatabaseConfigInvalid,
			exception.WithParam("error_msg",
				fmt.Sprintf("MilvusVectorStore has vector_field at %s while actual database has "+
					"vector field(s) at:\n%sYou may want to call delete_collection method on "+
					"collection %q", fieldName, vFieldsList, s.collectionName)),
		)
	}

	// 对齐 Python: if index_type != "auto": 校验 returned_type 前缀匹配
	if indexType != vector_fields.IndexTypeAUTO {
		returnedType := actual.IndexType()
		if !strings.HasPrefix(string(returnedType), strings.ToUpper(indexType.String())) {
			return exception.BuildError(
				exception.StatusRetrievalKbDatabaseConfigInvalid,
				exception.WithParam("error_msg",
					fmt.Sprintf("MilvusVectorStore has index_type of %s while actual database has "+
						"index_type of %s, do not change index_type after Knowledge Base is constructed.",
						indexType.String(), returnedType)),
			)
		}
	}

	// 对齐 Python: self._check_configs_matching(self._construct_config, actual)
	actualParams := make(map[string]any)
	for k, v := range actual.Params() {
		actualParams[k] = v
	}
	return CheckConfigsMatching(s.constructConfig, actualParams)
}
```

**注意**：Python 的 `returned_type.startswith(index_type.upper()) and returned_type.endswith(variant)`——Go 的 vector_fields 尚无 variant 字段（TODO #通用），此对齐只做 index_type 前缀检查，variant 检查留待 variant 字段补充后回填。这一点需在代码注释中标注 `TODO(#通用): variant 检查待 vector_fields variant 字段补充后回填`。

- [ ] **Step 5.4: 运行全部 milvus 测试确认通过**

```bash
cd /home/opensource/uapclaw-gateway && go test ./internal/agentcore/retrieval/vector_store/ -v 2>&1 | tail -15
```

- [ ] **Step 5.5: 提交**

```bash
cd /home/opensource/uapclaw-gateway && git add internal/agentcore/retrieval/vector_store/ && git commit -m "feat(retrieval): 13.3 MilvusVectorStore CheckVectorField 配置一致性校验"
```

---

### Task 6: MilvusIndexer 构造 + IndexExists/DeleteIndex/GetIndexInfo/Close

**Files:**
- Create: `internal/agentcore/retrieval/indexing/indexer/milvus.go`
- Test: `internal/agentcore/retrieval/indexing/indexer/milvus_test.go`

- [ ] **Step 6.1: 写失败测试**

```go
package indexer

import (
	"context"
	"fmt"
	"testing"

	"github.com/milvus-io/milvus/client/v2/entity"
	milvusclient "github.com/milvus-io/milvus/client/v2/milvusclient"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/store/vector_fields"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/retrieval/common"
)

// fakeIndexerMilvusClient indexer 测试用 fake client（复用 vector_store 的接口定义）
type fakeIndexerMilvusClient struct {
	collections        map[string]bool
	deleteCount        int64
	statsRowCount      string
	statsInList        bool
	createCollErr      error
	hasCollectionErr   error
	deleteErr          error
	createdSchemas     map[string]*entity.Schema
	describeCollResult *entity.Collection
}

func newFakeIndexerMilvusClient() *fakeIndexerMilvusClient {
	return &fakeIndexerMilvusClient{
		collections:    map[string]bool{},
		createdSchemas: map[string]*entity.Schema{},
	}
}

func (f *fakeIndexerMilvusClient) CreateCollection(ctx context.Context, option milvusclient.CreateCollectionOption, callOptions ...any) error {
	if f.createCollErr != nil {
		return f.createCollErr
	}
	// 记录 schema 供测试断言（通过 HasCollection 判断是否已创建）
	f.collections["__last__"] = true
	return nil
}
func (f *fakeIndexerMilvusClient) DropCollection(ctx context.Context, option milvusclient.DropCollectionOption, callOptions ...any) error {
	return nil
}
func (f *fakeIndexerMilvusClient) HasCollection(ctx context.Context, option milvusclient.HasCollectionOption, callOptions ...any) (bool, error) {
	if f.hasCollectionErr != nil {
		return false, f.hasCollectionErr
	}
	return f.collections["__exists__"], nil
}
func (f *fakeIndexerMilvusClient) DescribeCollection(ctx context.Context, option milvusclient.DescribeCollectionOption, callOptions ...any) (*entity.Collection, error) {
	if f.describeCollResult != nil {
		return f.describeCollResult, nil
	}
	return &entity.Collection{}, nil
}
func (f *fakeIndexerMilvusClient) ListCollections(ctx context.Context, option milvusclient.ListCollectionOption, callOptions ...any) ([]string, error) {
	return []string{}, nil
}
func (f *fakeIndexerMilvusClient) GetCollectionStats(ctx context.Context, option milvusclient.GetCollectionStatsOption, callOptions ...any) (map[string]string, error) {
	if f.statsInList {
		// 模拟 stats 列表格式（Go SDK 返回 map[string]string）
		return map[string]string{"row_count": f.statsRowCount}, nil
	}
	return map[string]string{"row_count": f.statsRowCount}, nil
}
func (f *fakeIndexerMilvusClient) Insert(ctx context.Context, option milvusclient.InsertOption, callOptions ...any) (milvusclient.InsertResult, error) {
	return milvusclient.InsertResult{}, nil
}
func (f *fakeIndexerMilvusClient) Delete(ctx context.Context, option milvusclient.DeleteOption, callOptions ...any) (milvusclient.DeleteResult, error) {
	if f.deleteErr != nil {
		return milvusclient.DeleteResult{}, f.deleteErr
	}
	return milvusclient.DeleteResult{DeleteCount: f.deleteCount}, nil
}
func (f *fakeIndexerMilvusClient) Flush(ctx context.Context, option milvusclient.FlushOption, callOptions ...any) error {
	return nil
}
func (f *fakeIndexerMilvusClient) Search(ctx context.Context, option milvusclient.SearchOption, callOptions ...any) ([]milvusclient.ResultSet, error) {
	return nil, nil
}
func (f *fakeIndexerMilvusClient) HybridSearch(ctx context.Context, option milvusclient.HybridSearchOption, callOptions ...any) ([]milvusclient.ResultSet, error) {
	return nil, nil
}
func (f *fakeIndexerMilvusClient) Query(ctx context.Context, option milvusclient.QueryOption, callOptions ...any) (milvusclient.ResultSet, error) {
	return milvusclient.ResultSet{}, nil
}
func (f *fakeIndexerMilvusClient) DescribeIndex(ctx context.Context, option milvusclient.DescribeIndexOption, callOptions ...any) (milvusclient.IndexDescription, error) {
	return milvusclient.IndexDescription{}, nil
}
func (f *fakeIndexerMilvusClient) LoadCollection(ctx context.Context, option milvusclient.LoadCollectionOption, callOptions ...any) error {
	return nil
}
func (f *fakeIndexerMilvusClient) GetLoadState(ctx context.Context, option milvusclient.GetLoadStateOption, callOptions ...any) (entity.LoadState, error) {
	return entity.LoadState{State: entity.LoadStateLoaded}, nil
}
func (f *fakeIndexerMilvusClient) ReleaseCollection(ctx context.Context, option milvusclient.ReleaseCollectionOption, callOptions ...any) error {
	return nil
}
func (f *fakeIndexerMilvusClient) ListDatabase(ctx context.Context, option milvusclient.ListDatabaseOption, callOptions ...any) ([]string, error) {
	return []string{"default"}, nil
}
func (f *fakeIndexerMilvusClient) CreateDatabase(ctx context.Context, option milvusclient.CreateDatabaseOption, callOptions ...any) error {
	return nil
}
func (f *fakeIndexerMilvusClient) UseDatabase(ctx context.Context, option milvusclient.UseDatabaseOption, callOptions ...any) error {
	return nil
}
func (f *fakeIndexerMilvusClient) Close(ctx context.Context) error {
	return nil
}

// newTestMilvusIndexer 构建 indexer 测试实例
func newTestMilvusIndexer(t *testing.T, fake *fakeIndexerMilvusClient) *MilvusIndexer {
	t.Helper()
	config := common.VectorStoreConfig{
		StoreProvider:  common.StoreTypeMilvus,
		CollectionName: "test-collection",
		DatabaseName:   "default",
		DistanceMetric: common.DistanceMetricCosine,
	}
	idx, err := NewMilvusIndexer(config, "http://localhost:19530", "",
		WithMilvusIndexerClient(fake))
	require.NoError(t, err)
	return idx
}

// TestNewMilvusIndexer_默认参数 对齐 Python MilvusIndexer.__init__
func TestNewMilvusIndexer_默认参数(t *testing.T) {
	fake := newFakeIndexerMilvusClient()
	idx := newTestMilvusIndexer(t, fake)
	assert.Equal(t, "COSINE", idx.distanceMetric)
	assert.Equal(t, "content", idx.textField)
	assert.Equal(t, "embedding", idx.vectorField.VectorFieldName())
	assert.Equal(t, "sparse_vector", idx.sparseVectorField)
	assert.Equal(t, "metadata", idx.metadataField)
	assert.Equal(t, "document_id", idx.docIDField)
}

// TestNewMilvusIndexer_别名生成 对齐 Python: create_milvus_alias(None, uri, token)
func TestNewMilvusIndexer_别名生成(t *testing.T) {
	fake := newFakeIndexerMilvusClient()
	config := common.VectorStoreConfig{
		StoreProvider:  common.StoreTypeMilvus,
		CollectionName: "test",
		DistanceMetric: common.DistanceMetricCosine,
	}
	idx, err := NewMilvusIndexer(config, "http://localhost:19530", "token123",
		WithMilvusIndexerClient(fake))
	require.NoError(t, err)
	assert.Contains(t, idx.milvusAlias, "kb-http://localhost:19530-")
}

// TestMilvusIndexer_IndexExists 对齐 Python index_exists
func TestMilvusIndexer_IndexExists(t *testing.T) {
	fake := newFakeIndexerMilvusClient()
	fake.collections["__exists__"] = true
	idx := newTestMilvusIndexer(t, fake)

	ok, err := idx.IndexExists(context.Background(), "some_index")
	require.NoError(t, err)
	assert.True(t, ok)
}

// TestMilvusIndexer_IndexExists_异常 对齐 Python: except → return False
func TestMilvusIndexer_IndexExists_异常(t *testing.T) {
	fake := newFakeIndexerMilvusClient()
	fake.hasCollectionErr = fmt.Errorf("conn error")
	idx := newTestMilvusIndexer(t, fake)

	ok, err := idx.IndexExists(context.Background(), "some_index")
	require.NoError(t, err) // 对齐 Python: 异常返回 False 不抛错
	assert.False(t, ok)
}

// TestMilvusIndexer_DeleteIndex 对齐 Python delete_index：filter 删除
func TestMilvusIndexer_DeleteIndex(t *testing.T) {
	fake := newFakeIndexerMilvusClient()
	fake.deleteCount = 5
	idx := newTestMilvusIndexer(t, fake)

	ok, err := idx.DeleteIndex(context.Background(), "doc1", "index1")
	require.NoError(t, err)
	assert.True(t, ok) // delete_count > 0
}

// TestMilvusIndexer_DeleteIndex_零删除 对齐 Python: delete_count == 0 → False
func TestMilvusIndexer_DeleteIndex_零删除(t *testing.T) {
	fake := newFakeIndexerMilvusClient()
	fake.deleteCount = 0
	idx := newTestMilvusIndexer(t, fake)

	ok, err := idx.DeleteIndex(context.Background(), "doc1", "index1")
	require.NoError(t, err)
	assert.False(t, ok)
}

// TestMilvusIndexer_DeleteIndex_异常 对齐 Python: MilvusException → return False
func TestMilvusIndexer_DeleteIndex_异常(t *testing.T) {
	fake := newFakeIndexerMilvusClient()
	fake.deleteErr = fmt.Errorf("delete failed")
	idx := newTestMilvusIndexer(t, fake)

	ok, err := idx.DeleteIndex(context.Background(), "doc1", "index1")
	require.NoError(t, err) // 对齐 Python: 异常返回 False 不抛错
	assert.False(t, ok)
}

// TestMilvusIndexer_GetIndexInfo_存在 对齐 Python get_index_info：stats row_count + describe
func TestMilvusIndexer_GetIndexInfo_存在(t *testing.T) {
	fake := newFakeIndexerMilvusClient()
	fake.collections["__exists__"] = true
	fake.statsRowCount = "42"
	idx := newTestMilvusIndexer(t, fake)

	info, err := idx.GetIndexInfo(context.Background(), "index1")
	require.NoError(t, err)
	assert.Equal(t, true, info["exists"])
	assert.Equal(t, "index1", info["collection_name"])
	assert.Equal(t, 42, info["count"])
	assert.NotNil(t, info["info"]) // describe_collection 结果
}

// TestMilvusIndexer_GetIndexInfo_不存在 对齐 Python: return {"exists": False}
func TestMilvusIndexer_GetIndexInfo_不存在(t *testing.T) {
	fake := newFakeIndexerMilvusClient()
	idx := newTestMilvusIndexer(t, fake)

	info, err := idx.GetIndexInfo(context.Background(), "index1")
	require.NoError(t, err)
	assert.Equal(t, false, info["exists"])
}
```

- [ ] **Step 6.2: 运行测试确认失败**

- [ ] **Step 6.3: 实现 indexer/milvus.go 第一部分**

问题：indexer 包无法访问 vector_store 包的非导出 `milvusClient` 接口。**解决方案**：vector_store 包提供导出的构造入口——`NewMilvusVectorStoreWithClient` 或者将 milvusClient 接口导出。对齐 Python（MilvusIndexer 持有 client 且直接调用 pymilvus API），Go 在 indexer 包重新定义接口会导致重复。**决策**：在 vector_store 包将接口和注入选项导出：

修改 `vector_store/milvus.go`：
- `milvusClient` → `MilvusClient`（导出）
- `milvusClientAdapter` → `MilvusClientAdapter`（导出）
- `defaultMilvusCreateClient` → `DefaultMilvusCreateClient`（导出）
- `WithMilvusClientOption(c MilvusClient)`（已导出）
- `WithMilvusCreateClient`（已导出）

```go
package indexer

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/milvus-io/milvus/client/v2/entity"
	milvusclient "github.com/milvus-io/milvus/client/v2/milvusclient"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/store/vector_fields"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/retrieval/common"
	vector_store "github.com/uapclaw/uapclaw-go/internal/agentcore/retrieval/vector_store"
	"github.com/uapclaw/uapclaw-go/internal/common/exception"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// MilvusIndexer Milvus 文档索引管理器。
//
// 实现 Indexer 接口，负责将 TextChunk 写入 Milvus 向量库。
// 对齐 Python retrieval/indexing/indexer/milvus_indexer.py (MilvusIndexer)。
type MilvusIndexer struct {
	// milvusURI Milvus 服务地址
	milvusURI string
	// milvusToken Milvus 认证 token
	milvusToken string
	// textField 文本字段名，默认 "content"
	textField string
	// vectorField 向量字段配置
	vectorField vector_fields.MilvusVectorFieldConfig
	// sparseVectorField 稀疏向量字段名，默认 "sparse_vector"
	sparseVectorField string
	// metadataField 元数据字段名，默认 "metadata"
	metadataField string
	// docIDField 文档 ID 字段名，默认 "document_id"
	docIDField string
	// databaseName 数据库名称
	databaseName string
	// constructConfig 构建阶段配置
	constructConfig map[string]any
	// searchConfig 搜索阶段配置
	searchConfig map[string]any
	// distanceMetric 归一化距离度量：IP/L2/COSINE
	distanceMetric string
	// docIndexCallback 嵌入进度回调
	docIndexCallback common.DocIndexCallback
	// milvusAlias 连接别名
	milvusAlias string
	// client Milvus 客户端（构造时创建，全方法共享）
	client vector_store.MilvusClient
	// createClientFunc 客户端创建函数（依赖注入）
	createClientFunc func(ctx context.Context, databaseName, uri, token string) (vector_store.MilvusClient, error)
}

// MilvusIndexerOption MilvusIndexer 的可选参数函数。
type MilvusIndexerOption func(*MilvusIndexer)

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

const (
	// logComponent 日志组件（indexer 包已有定义，此处不重复）
	// milvusBM25FunctionName BM25 函数名，对齐 Python "text_bm25_emb"
	milvusBM25FunctionName = "text_bm25_emb"
)

// ──────────────────────────── 全局变量 ────────────────────────────

// 编译期接口断言
var _ Indexer = (*MilvusIndexer)(nil)

// ──────────────────────────── 导出函数 ────────────────────────────

// NewMilvusIndexer 创建 MilvusIndexer 实例。
//
// 对齐 Python MilvusIndexer.__init__：
//   - vectorField 字符串自动转 MilvusAUTO（Go 用构造函数显式转换）
//   - 距离度量归一化 dot→ip / euclidean→l2 / 大写
//   - constructConfig/searchConfig 初始化
//   - milvusAlias 生成
//   - client 创建
func NewMilvusIndexer(
	config common.VectorStoreConfig,
	milvusURI string,
	milvusToken string,
	opts ...MilvusIndexerOption,
) (*MilvusIndexer, error) {
	idx := &MilvusIndexer{
		milvusURI:         milvusURI,
		milvusToken:       milvusToken,
		textField:         "content",
		vectorField:       vector_fields.NewMilvusAUTO("embedding"),
		sparseVectorField: "sparse_vector",
		metadataField:     "metadata",
		docIDField:        "document_id",
		databaseName:      config.DatabaseName,
		createClientFunc:  vector_store.DefaultMilvusCreateClient,
	}

	for _, opt := range opts {
		opt(idx)
	}

	// 对齐 Python: if isinstance(vector_field, str) → MilvusAUTO（已在默认值处理）

	// 对齐 Python: self._distance_metric = ...replace("dot","ip").replace("euclidean","l2").upper()
	idx.distanceMetric = strings.ToUpper(
		strings.ReplaceAll(
			strings.ReplaceAll(string(config.DistanceMetric), "dot", "ip"),
			"euclidean", "l2",
		),
	)

	// 对齐 Python: construct_config 初始化
	if idx.vectorField.IndexType() == vector_fields.IndexTypeAUTO {
		idx.constructConfig = map[string]any{}
	} else {
		idx.constructConfig = idx.vectorField.ToConstructDict()
	}
	idx.constructConfig["metric_type"] = idx.distanceMetric

	// 对齐 Python: self._search_config = self.vector_field.to_dict(stage="search")
	idx.searchConfig = idx.vectorField.ToSearchDict()

	// 对齐 Python: self.milvus_alias = create_milvus_alias(milvus_alias, uri=..., token=...)
	// （别名在 WithMilvusIndexerAlias 未设置时惰性生成，见 ensureAlias）

	// 对齐 Python: self._client = MilvusVectorStore.create_client(...)
	if idx.client == nil {
		client, err := idx.createClientFunc(context.Background(), idx.databaseName, idx.milvusURI, idx.milvusToken)
		if err != nil {
			return nil, exception.BuildError(
				exception.StatusRetrievalVectorStoreProviderInvalid,
				exception.WithParam("error_msg", fmt.Sprintf("创建 Milvus 客户端失败: %s", err)),
				exception.WithCause(err),
			)
		}
		idx.client = client
	}

	return idx, nil
}

// WithMilvusIndexerClient 注入 Milvus 客户端（测试用，跳过默认创建）。
func WithMilvusIndexerClient(c vector_store.MilvusClient) MilvusIndexerOption {
	return func(idx *MilvusIndexer) { idx.client = c }
}

// WithMilvusIndexerCreateClient 注入客户端创建函数。
func WithMilvusIndexerCreateClient(f func(ctx context.Context, databaseName, uri, token string) (vector_store.MilvusClient, error)) MilvusIndexerOption {
	return func(idx *MilvusIndexer) { idx.createClientFunc = f }
}

// WithMilvusIndexerTextField 设置文本字段名。
func WithMilvusIndexerTextField(field string) MilvusIndexerOption {
	return func(idx *MilvusIndexer) { idx.textField = field }
}

// WithMilvusIndexerVectorField 设置向量字段配置。
func WithMilvusIndexerVectorField(vf vector_fields.MilvusVectorFieldConfig) MilvusIndexerOption {
	return func(idx *MilvusIndexer) { idx.vectorField = vf }
}

// WithMilvusIndexerSparseVectorField 设置稀疏向量字段名。
func WithMilvusIndexerSparseVectorField(field string) MilvusIndexerOption {
	return func(idx *MilvusIndexer) { idx.sparseVectorField = field }
}

// WithMilvusIndexerMetadataField 设置元数据字段名。
func WithMilvusIndexerMetadataField(field string) MilvusIndexerOption {
	return func(idx *MilvusIndexer) { idx.metadataField = field }
}

// WithMilvusIndexerDocIDField 设置文档 ID 字段名。
func WithMilvusIndexerDocIDField(field string) MilvusIndexerOption {
	return func(idx *MilvusIndexer) { idx.docIDField = field }
}

// WithMilvusIndexerDocIndexCallback 设置嵌入进度回调。
func WithMilvusIndexerDocIndexCallback(cb common.DocIndexCallback) MilvusIndexerOption {
	return func(idx *MilvusIndexer) { idx.docIndexCallback = cb }
}

// WithMilvusIndexerAlias 设置连接别名。
func WithMilvusIndexerAlias(alias string) MilvusIndexerOption {
	return func(idx *MilvusIndexer) { idx.milvusAlias = alias }
}

// Client 返回底层 Milvus 客户端。
// 对齐 Python client property。
func (mi *MilvusIndexer) Client() vector_store.MilvusClient { return mi.client }

// DistanceMetric 返回归一化距离度量。
// 对齐 Python distance_metric property。
func (mi *MilvusIndexer) DistanceMetric() string { return mi.distanceMetric }

// IndexExists 检查索引是否存在。
//
// 对齐 Python MilvusIndexer.index_exists：has_collection，异常时返回 False。
func (mi *MilvusIndexer) IndexExists(ctx context.Context, indexName string) (bool, error) {
	ok, err := mi.client.HasCollection(ctx, milvusclient.NewHasCollectionOption(indexName))
	if err != nil {
		// 对齐 Python: except Exception as e: logger.error(...); return False
		logger.Error(logComponent).
			Str("index_name", indexName).
			Err(err).
			Msg("检查索引存在性失败")
		return false, nil
	}
	return ok, nil
}

// DeleteIndex 删除索引。
//
// 对齐 Python MilvusIndexer.delete_index：
// filter_expr = 'document_id == "xxx"'，delete 后按 delete_count 判断。
func (mi *MilvusIndexer) DeleteIndex(ctx context.Context, docID string, indexName string) (bool, error) {
	// 对齐 Python: filter_expr = f'{self.doc_id_field} == "{doc_id}"'
	filterExpr := fmt.Sprintf("%s == %q", mi.docIDField, docID)

	result, err := mi.client.Delete(ctx, milvusclient.NewDeleteOption(indexName).WithExpr(filterExpr))
	if err != nil {
		// 对齐 Python: except MilvusException / except Exception → logger.error + return False
		logger.Error(logComponent).
			Str("event_type", "INDEXING_ERROR").
			Str("doc_id", docID).
			Str("index_name", indexName).
			Err(err).
			Msg("删除索引条目失败")
		return false, nil
	}

	// 对齐 Python: logger.info(f"Deleted {delete_count} entries for doc_id={doc_id}")
	logger.Info(logComponent).
		Str("doc_id", docID).
		Int64("delete_count", result.DeleteCount).
		Msg("成功删除文档条目")

	return result.DeleteCount > 0, nil
}

// GetIndexInfo 获取索引信息。
//
// 对齐 Python MilvusIndexer.get_index_info：
//   - 不存在返回 {"exists": False}
//   - get_collection_stats 获取 row_count（避免 query 分页错误）
//   - describe_collection 获取字段信息
func (mi *MilvusIndexer) GetIndexInfo(ctx context.Context, indexName string) (map[string]any, error) {
	exists, err := mi.IndexExists(ctx, indexName)
	if err != nil || !exists {
		return map[string]any{"exists": false}, nil
	}

	// 对齐 Python: stats = get_collection_stats(...)；row_count 可能是字符串
	stats, err := mi.client.GetCollectionStats(ctx, milvusclient.NewGetCollectionStatsOption(indexName))
	if err != nil {
		logger.Error(logComponent).
			Str("index_name", indexName).
			Err(err).
			Msg("获取集合统计失败")
		return map[string]any{"exists": false, "error": err.Error()}, nil
	}
	rowCount := 0
	if rc, ok := stats["row_count"]; ok {
		if n, parseErr := strconv.Atoi(rc); parseErr == nil {
			rowCount = n
		}
	}

	// 对齐 Python: collection_info = describe_collection(...)
	collectionInfo, err := mi.client.DescribeCollection(ctx, milvusclient.NewDescribeCollectionOption(indexName))
	if err != nil {
		logger.Error(logComponent).
			Str("index_name", indexName).
			Err(err).
			Msg("获取集合描述失败")
		return map[string]any{"exists": false, "error": err.Error()}, nil
	}

	return map[string]any{
		"exists":          true,
		"collection_name": indexName,
		"info":            collectionInfo,
		"count":           rowCount,
	}, nil
}

// Close 关闭索引管理器。
// 对齐 Python MilvusIndexer.close。
func (mi *MilvusIndexer) Close() {
	if mi.client == nil {
		return
	}
	if err := mi.client.Close(context.Background()); err != nil {
		// 对齐 Python: logger.warning(f"Failed to close Milvus client: {e}")
		logger.Warn(logComponent).Err(err).Msg("关闭 Milvus 客户端失败")
	}
}
```

**注意**：`milvusAlias` 生成逻辑——Python 在 `__init__` 中调用 `create_milvus_alias`。Go 在构造时如果没有注入 alias，需要生成。在 NewMilvusIndexer 中 client 创建前加：

```go
	// 对齐 Python: self.milvus_alias = create_milvus_alias(milvus_alias, uri=milvus_uri, token=milvus_token)
	if idx.milvusAlias == "" {
		idx.milvusAlias = utils.CreateMilvusAlias("", milvusURI, milvusToken)
	}
```

import 追加 `"github.com/uapclaw/uapclaw-go/internal/agentcore/retrieval/utils"`。

- [ ] **Step 6.4: 运行测试确认通过**

```bash
cd /home/opensource/uapclaw-gateway && go test ./internal/agentcore/retrieval/indexing/indexer/ -run 'TestNewMilvusIndexer|TestMilvusIndexer_(IndexExists|DeleteIndex|GetIndexInfo)' -v 2>&1 | tail -20
```

- [ ] **Step 6.5: 提交**

```bash
cd /home/opensource/uapclaw-gateway && git add internal/agentcore/retrieval/ && git commit -m "feat(retrieval): 13.3 MilvusIndexer 构造+IndexExists/DeleteIndex/GetIndexInfo/Close，vector_store 导出 MilvusClient 接口"
```

---

### Task 7: MilvusIndexer — ensureCollection

**Files:**
- Modify: `internal/agentcore/retrieval/indexing/indexer/milvus.go`
- Test: `internal/agentcore/retrieval/indexing/indexer/milvus_test.go`

- [ ] **Step 7.1: 追加失败测试**

```go
// fakeIndexerMilvusClient 需要扩展记录 schema：
// struct 追加 createdCollections map[string]*entity.Schema

// TestMilvusIndexer_ensureCollection_已存在 对齐 Python: index_exists → return
func TestMilvusIndexer_ensureCollection_已存在(t *testing.T) {
	fake := newFakeIndexerMilvusClient()
	fake.collections["existing_coll"] = true
	// HasCollection 需按集合名返回
	idx := newTestMilvusIndexer(t, fake)

	err := idx.ensureCollection(context.Background(), "existing_coll",
		common.IndexConfig{IndexName: "existing_coll", IndexType: common.IndexTypeVector}, nil)
	require.NoError(t, err)
}

// TestMilvusIndexer_ensureCollection_BM25 对齐 Python: bm25 类型含 analyzer+sparse+BM25函数
func TestMilvusIndexer_ensureCollection_BM25(t *testing.T) {
	fake := newFakeIndexerMilvusClient()
	idx := newTestMilvusIndexer(t, fake)

	err := idx.ensureCollection(context.Background(), "bm25_coll",
		common.IndexConfig{IndexName: "bm25_coll", IndexType: common.IndexTypeBM25}, nil)
	require.NoError(t, err)

	schema := fake.createdSchemas["bm25_coll"]
	require.NotNil(t, schema)
	// 断言字段
	fieldNames := map[string]bool{}
	for _, f := range schema.Fields {
		fieldNames[f.Name] = true
	}
	assert.True(t, fieldNames["document_id"])
	assert.True(t, fieldNames["pk"])
	assert.True(t, fieldNames["chunk_id"])
	assert.True(t, fieldNames["content"])
	assert.True(t, fieldNames["sparse_vector"])
	assert.True(t, fieldNames["metadata"])
	assert.False(t, fieldNames["embedding"]) // bm25 不含 dense 向量

	// 断言 BM25 Function
	require.Len(t, schema.Functions, 1)
	assert.Equal(t, "text_bm25_emb", schema.Functions[0].Name)

	// 断言 content 字段启用 analyzer
	for _, f := range schema.Fields {
		if f.Name == "content" {
			assert.Equal(t, "true", f.TypeParams["enable_analyzer"])
			assert.Equal(t, "jieba", f.TypeParams["analyzer_params"]) // 简化断言，实际为 JSON
		}
	}
}

// TestMilvusIndexer_ensureCollection_Vector 对齐 Python: vector 类型含 dense 向量
func TestMilvusIndexer_ensureCollection_Vector(t *testing.T) {
	fake := newFakeIndexerMilvusClient()
	idx := newTestMilvusIndexer(t, fake)

	err := idx.ensureCollection(context.Background(), "vec_coll",
		common.IndexConfig{IndexName: "vec_coll", IndexType: common.IndexTypeVector}, nil)
	require.Error(t, err) // 无 embedModel 时 dimension 无法推断 → 报错
}

// TestMilvusIndexer_ensureCollection_Hybrid 对齐 Python: hybrid 类型同时含 sparse + dense
func TestMilvusIndexer_ensureCollection_Hybrid(t *testing.T) {
	fake := newFakeIndexerMilvusClient()
	idx := newTestMilvusIndexer(t, fake)

	err := idx.ensureCollection(context.Background(), "hybrid_coll",
		common.IndexConfig{IndexName: "hybrid_coll", IndexType: common.IndexTypeHybrid}, nil)
	require.Error(t, err) // 同样需要 embedModel 提供 dimension
}
```

fake 的 CreateCollection 需要记录 schema。问题：`milvusclient.CreateCollectionOption` 是不透明接口，无法直接读取 schema。**解决方案**：fake client 检查 `option` 的具体类型断言——SDK 的 `createCollectionOption` 是非导出类型无法断言。**替代方案**：通过 `option.Request()`（如果接口有）……查看 SDK：`CreateCollectionOption` 接口有 `Request() *milvuspb.CreateCollectionRequest` 方法。fake 可以从 proto request 中解析 schema：

```go
func (f *fakeIndexerMilvusClient) CreateCollection(ctx context.Context, option milvusclient.CreateCollectionOption, callOptions ...any) error {
	if f.createCollErr != nil {
		return f.createCollErr
	}
	req := option.Request()
	schemaProto := req.GetSchema()
	if schemaProto != nil {
		schema := &entity.Schema{}
		_ = schema.Unmarshal(schemaProto) // entity.Schema 有 Unmarshal 方法
		f.createdSchemas[req.GetCollectionName()] = schema
	}
	return nil
}
```

需要验证 `entity.Schema.Unmarshal` 方法存在。同时 `HasCollection` 按名字查 `collections` map：

```go
func (f *fakeIndexerMilvusClient) HasCollection(ctx context.Context, option milvusclient.HasCollectionOption, callOptions ...any) (bool, error) {
	if f.hasCollectionErr != nil {
		return false, f.hasCollectionErr
	}
	name := option.Request().GetCollectionName()
	return f.collections[name], nil
}
```

**注意**：这会改变 Task 6 测试中 `__exists__` 的用法——需要把 Task 6 测试中的 `fake.collections["__exists__"] = true` 改为 `fake.collections["some_index"] = true`（按实际集合名记录）。

`ensureCollection_Vector` 测试中无 embedModel 报错——对齐 Python：`dimension is required for vector/hybrid index type`（DIMENSION_NOT_FOUND）。

- [ ] **Step 7.2: 运行测试确认失败**

- [ ] **Step 7.3: 实现 ensureCollection**

```go
// ensureCollection 确保集合存在，不存在则创建。
//
// 对齐 Python MilvusIndexer._ensure_collection：
//   - 已存在直接返回
//   - schema：document_id(VARCHAR 256+INVERTED) + pk(INT64 PK auto_id) + chunk_id(VARCHAR 256+INVERTED)
//     + content(VARCHAR 65535, BM25 时 enable_analyzer+jieba)
//     + BM25/hybrid：sparse_vector(SPARSE_FLOAT_VECTOR) + BM25 Function + SPARSE_INVERTED_INDEX
//     + vector/hybrid：embedding(FLOAT_VECTOR dim) + dense 索引
//     + metadata(JSON)
//   - dimension 推断：embedModel.Dimension → EmbedQuery("X") → 报错
func (mi *MilvusIndexer) ensureCollection(ctx context.Context, collectionName string, config common.IndexConfig, embedModel embedding.BaseEmbedding) error {
	exists, err := mi.IndexExists(ctx, collectionName)
	if err != nil {
		return err
	}
	if exists {
		return nil
	}

	// 对齐 Python: schema = self._client.create_schema(auto_id=True, enable_dynamic_field=False)
	schema := entity.NewSchema().WithAutoID(true).WithDynamicField(false)

	var indexOpts []milvusclient.CreateIndexOption

	// 对齐 Python: document_id 字段 + INVERTED 索引
	schema = schema.WithField(
		entity.NewField().WithName(mi.docIDField).
			WithDataType(entity.FieldTypeVarChar).
			WithMaxLength(256),
	)
	indexOpts = append(indexOpts, milvusclient.NewCreateIndexOption(collectionName, mi.docIDField, index.NewInvertedIndex()))

	// 对齐 Python: pk 主键字段（INT64, auto_id）
	schema = schema.WithField(
		entity.NewField().WithName("pk").
			WithDataType(entity.FieldTypeInt64).
			WithIsPrimaryKey(true).
			WithIsAutoID(true),
	)

	// 对齐 Python: chunk_id 字段 + INVERTED 索引
	schema = schema.WithField(
		entity.NewField().WithName("chunk_id").
			WithDataType(entity.FieldTypeVarChar).
			WithMaxLength(256),
	)
	indexOpts = append(indexOpts, milvusclient.NewCreateIndexOption(collectionName, "chunk_id", index.NewInvertedIndex()))

	// 对齐 Python: enable_bm25 = config.index_type in ("bm25", "hybrid")
	enableBM25 := config.IndexType == common.IndexTypeBM25 || config.IndexType == common.IndexTypeHybrid

	// 对齐 Python: content 字段（BM25 时 enable_analyzer + jieba 分词器）
	contentField := entity.NewField().WithName(mi.textField).
		WithDataType(entity.FieldTypeVarChar).
		WithMaxLength(65535)
	if enableBM25 {
		contentField = contentField.WithEnableAnalyzer(true).
			WithAnalyzerParams(map[string]any{"tokenizer": "jieba"})
	}
	schema = schema.WithField(contentField)

	// 对齐 Python: BM25 稀疏向量字段 + Function + SPARSE_INVERTED_INDEX
	if enableBM25 {
		schema = schema.WithField(
			entity.NewField().WithName(mi.sparseVectorField).
				WithDataType(entity.FieldTypeSparseVector),
		)
		schema = schema.WithFunction(
			entity.NewFunction().WithName(milvusBM25FunctionName).
				WithType(entity.FunctionTypeBM25).
				WithInputFields(mi.textField).
				WithOutputFields(mi.sparseVectorField),
		)
		// 对齐 Python: index_type="SPARSE_INVERTED_INDEX", metric_type="BM25"
		indexOpts = append(indexOpts, milvusclient.NewCreateIndexOption(collectionName, mi.sparseVectorField, index.NewSparseInvertedIndex(entity.BM25, 0)))
	}

	// 对齐 Python: vector/hybrid 时 embedding 字段 + dense 索引
	if config.IndexType == common.IndexTypeVector || config.IndexType == common.IndexTypeHybrid {
		// 对齐 Python: dimension 推断三级防御
		dimension := 0
		if embedModel != nil {
			dimension = embedModel.Dimension()
		}
		if (dimension == 0) && embedModel != nil {
			// 对齐 Python: test_embedding = await embed_model.embed_query("X")
			if testEmb, embErr := embedModel.EmbedQuery(ctx, "X"); embErr == nil {
				dimension = len(testEmb)
				// 对齐 Python: logger.debug(f"Got dimension through API call: {dimension}")
				logger.Debug(logComponent).Int("dimension", dimension).Msg("通过 API 调用获取维度")
			} else {
				// 对齐 Python: logger.warning(f"Unable to get dimension through API call: {e}")
				logger.Warn(logComponent).Err(embErr).Msg("无法通过 API 调用获取维度")
			}
		}
		if dimension == 0 {
			return exception.BuildError(
				exception.StatusRetrievalIndexingDimensionNotFound,
				exception.WithParam("error_msg", "dimension is required for vector/hybrid index type"),
			)
		}

		schema = schema.WithField(
			entity.NewField().WithName(mi.vectorField.VectorFieldName()).
				WithDataType(entity.FieldTypeFloatVector).
				WithDim(int64(dimension)),
		)

		// 对齐 Python: index_type = "AUTOINDEX" if auto else index_type.upper() (+variant)
		idx, err := mi.buildDenseIndex()
		if err != nil {
			return err
		}
		indexOpts = append(indexOpts, milvusclient.NewCreateIndexOption(collectionName, mi.vectorField.VectorFieldName(), idx))
	}

	// 对齐 Python: metadata 字段（JSON）
	schema = schema.WithField(
		entity.NewField().WithName(mi.metadataField).
			WithDataType(entity.FieldTypeJSON),
	)

	// 对齐 Python: create_collection(collection_name, schema, index_params)
	createOpt := milvusclient.NewCreateCollectionOption(collectionName, schema).
		WithIndexOptions(indexOpts...)
	if err := mi.client.CreateCollection(ctx, createOpt); err != nil {
		logger.Error(logComponent).
			Str("collection_name", collectionName).
			Err(err).
			Msg("创建集合失败")
		return err
	}

	// 对齐 Python: logger.info(f"Created collection: {collection_name}")
	logger.Info(logComponent).
		Str("collection_name", collectionName).
		Msg("集合创建成功")
	return nil
}

// buildDenseIndex 根据向量字段配置构建 dense 索引。
//
// 对齐 Python _ensure_collection 中的 index_type 计算：
// auto → AUTOINDEX；其他 → 具体索引构造（参数来自 constructConfig）。
func (mi *MilvusIndexer) buildDenseIndex() (index.Index, error) {
	metricType := mapMilvusMetricType(mi.distanceMetric)

	switch vf := mi.vectorField.(type) {
	case *vector_fields.MilvusAUTO:
		return index.NewAutoIndex(metricType), nil
	case *vector_fields.MilvusFLAT:
		// 对齐 foundation 层：FLAT 等价 nlist=1 的 IVF_FLAT
		return index.NewIvfFlatIndex(metricType, 1), nil
	case *vector_fields.MilvusHNSW:
		return index.NewHNSWIndex(metricType, vf.M, vf.EfConstruction), nil
	case *vector_fields.MilvusIVF:
		return index.NewIvfFlatIndex(metricType, vf.Nlist), nil
	case *vector_fields.MilvusSCANN:
		// 新 SDK 暂无 SCANN 构造器，用 IVF_PQ 近似（对齐 foundation 层做法）
		return index.NewIvfPQIndex(metricType, vf.Nlist, 8, 8), nil
	default:
		return index.NewAutoIndex(metricType), nil
	}
}

// mapMilvusMetricType 距离度量字符串转 SDK MetricType。
func mapMilvusMetricType(metric string) entity.MetricType {
	switch strings.ToUpper(metric) {
	case "L2":
		return entity.L2
	case "IP":
		return entity.IP
	default:
		return entity.COSINE
	}
}
```

import 追加：`"github.com/milvus-io/milvus/client/v2/index"`、`"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/store/embedding"`。

- [ ] **Step 7.4: 验证 entity.Schema.Unmarshal 存在并修正 fake**

```bash
grep -n "func (s \*Schema) Unmarshal" /root/go/pkg/mod/github.com/milvus-io/milvus/client/v2@v2.6.5/entity/schema.go
```

如果存在则 fake 用 Unmarshal；不存在则从 proto 手动解析字段名（`req.GetSchema().GetFields()` 每个有 `GetName()`）：

```go
func (f *fakeIndexerMilvusClient) CreateCollection(ctx context.Context, option milvusclient.CreateCollectionOption, callOptions ...any) error {
	if f.createCollErr != nil {
		return f.createCollErr
	}
	req := option.Request()
	schema := &entity.Schema{}
	if p := req.GetSchema(); p != nil {
		_ = schema.Unmarshal(p)
	}
	f.createdSchemas[req.GetCollectionName()] = schema
	return nil
}
```

测试断言改为通过 `schema.Fields`（Unmarshal 后填充）。

- [ ] **Step 7.5: 运行测试确认通过**

```bash
cd /home/opensource/uapclaw-gateway && go test ./internal/agentcore/retrieval/indexing/indexer/ -run 'TestMilvusIndexer_ensureCollection' -v 2>&1 | tail -15
```

- [ ] **Step 7.6: 提交**

```bash
cd /home/opensource/uapclaw-gateway && git add internal/agentcore/retrieval/ && git commit -m "feat(retrieval): 13.3 MilvusIndexer ensureCollection 自动建表（BM25+dense+INVERTED 索引）"
```

---

### Task 8: MilvusIndexer — BuildIndex/UpdateIndex

**Files:**
- Modify: `internal/agentcore/retrieval/indexing/indexer/milvus.go`
- Test: `internal/agentcore/retrieval/indexing/indexer/milvus_test.go`

- [ ] **Step 8.1: 追加失败测试**

```go
// fakeEmbedModel 测试用嵌入模型
type fakeEmbedModel struct {
	dimension int
	embedErr  error
}

func (f *fakeEmbedModel) EmbedQuery(ctx context.Context, text string, opts ...storeEmbedding.EmbedOption) ([]float64, error) {
	if f.embedErr != nil {
		return nil, f.embedErr
	}
	return make([]float64, f.dimension), nil
}
func (f *fakeEmbedModel) EmbedDocuments(ctx context.Context, texts []string, opts ...storeEmbedding.EmbedOption) ([][]float64, error) {
	if f.embedErr != nil {
		return nil, f.embedErr
	}
	result := make([][]float64, len(texts))
	for i := range result {
		result[i] = make([]float64, f.dimension)
	}
	return result, nil
}
func (f *fakeEmbedModel) Dimension() int { return f.dimension }
// ContextDimension / DimensionCtx 如果 BaseEmbedding 接口需要则补充

// TestMilvusIndexer_BuildIndex_完整流程 对齐 Python build_index
func TestMilvusIndexer_BuildIndex_完整流程(t *testing.T) {
	fake := newFakeIndexerMilvusClient()
	fake.insertOK = true
	idx := newTestMilvusIndexer(t, fake)

	chunks := []common.TextChunk{
		{ID: "c1", Text: "hello world", DocID: "doc1", Metadata: map[string]any{"chunk_id": "c1"}},
	}
	config := common.IndexConfig{IndexName: "coll1", IndexType: common.IndexTypeVector}
	embedModel := &fakeEmbedModel{dimension: 4}

	ok, err := idx.BuildIndex(context.Background(), chunks, config, embedModel)
	require.NoError(t, err)
	assert.True(t, ok)
}

// TestMilvusIndexer_BuildIndex_重复docID 对齐 Python: query 查重 → ADD_DOC_RUNTIME_ERROR
func TestMilvusIndexer_BuildIndex_重复docID(t *testing.T) {
	fake := newFakeIndexerMilvusClient()
	fake.queryReturnDocIDs = []string{"doc1"} // query 返回已有 doc1
	idx := newTestMilvusIndexer(t, fake)

	chunks := []common.TextChunk{
		{ID: "c1", Text: "text", DocID: "doc1", Metadata: map[string]any{}},
	}
	config := common.IndexConfig{IndexName: "coll1", IndexType: common.IndexTypeBM25}

	ok, err := idx.BuildIndex(context.Background(), chunks, config, nil)
	require.Error(t, err)
	assert.False(t, ok)
}

// TestMilvusIndexer_BuildIndex_缺嵌入模型 对齐 Python: EMBED_MODEL_NOT_FOUND
func TestMilvusIndexer_BuildIndex_缺嵌入模型(t *testing.T) {
	fake := newFakeIndexerMilvusClient()
	idx := newTestMilvusIndexer(t, fake)

	chunks := []common.TextChunk{{ID: "c1", Text: "t", DocID: "d1"}}
	config := common.IndexConfig{IndexName: "coll1", IndexType: common.IndexTypeVector}

	ok, err := idx.BuildIndex(context.Background(), chunks, config, nil)
	require.Error(t, err)
	assert.False(t, ok)
}

// TestMilvusIndexer_UpdateIndex 对齐 Python update_index: delete → flush → build
func TestMilvusIndexer_UpdateIndex(t *testing.T) {
	fake := newFakeIndexerMilvusClient()
	fake.insertOK = true
	fake.deleteCount = 2
	fake.flushCount = 0
	idx := newTestMilvusIndexer(t, fake)

	chunks := []common.TextChunk{
		{ID: "c1", Text: "updated", DocID: "doc1", Metadata: map[string]any{"chunk_id": "c1"}},
	}
	config := common.IndexConfig{IndexName: "coll1", IndexType: common.IndexTypeBM25}

	ok, err := idx.UpdateIndex(context.Background(), chunks, "doc1", config, nil)
	require.NoError(t, err)
	assert.True(t, ok)
	assert.Equal(t, 1, fake.flushCount) // 对齐 Python: delete 后 flush
}
```

fake 扩展（fakeIndexerMilvusClient）：

```go
// struct 追加：
	insertOK          bool
	flushCount        int
	queryReturnDocIDs []string

// Query 改为返回 doc_id 列：
func (f *fakeIndexerMilvusClient) Query(ctx context.Context, option milvusclient.QueryOption, callOptions ...any) (milvusclient.ResultSet, error) {
	if len(f.queryReturnDocIDs) == 0 {
		return milvusclient.ResultSet{}, nil
	}
	col := column.NewColumnVarChar("document_id", f.queryReturnDocIDs)
	return milvusclient.ResultSet{
		ResultCount: len(f.queryReturnDocIDs),
		Fields:      milvusclient.DataSet{col},
	}, nil
}

// Flush 改为计数：
func (f *fakeIndexerMilvusClient) Flush(ctx context.Context, option milvusclient.FlushOption, callOptions ...any) error {
	f.flushCount++
	return nil
}

// Insert 改为标记：
func (f *fakeIndexerMilvusClient) Insert(ctx context.Context, option milvusclient.InsertOption, callOptions ...any) (milvusclient.InsertResult, error) {
	return milvusclient.InsertResult{}, nil
}
```

`TestMilvusIndexer_BuildIndex_重复docID` 中 BM25 类型不需要 embedModel——但 ensureCollection 会建表（fake CreateCollection 成功），然后 query 查重返回 doc1 → 报错。

- [ ] **Step 8.2: 运行测试确认失败**

- [ ] **Step 8.3: 实现 BuildIndex/UpdateIndex**

```go
// BuildIndex 构建索引。ensureCollection → 查重 → 嵌入 → 转换 → 写入。
//
// 对齐 Python MilvusIndexer.build_index。
// 5 步流程：
//  1. _ensure_collection 确保集合存在（自动建表）
//  2. query 检查 doc_id 重复（重复抛 ADD_DOC_RUNTIME_ERROR）
//  3. vector/hybrid 类型计算 embedding
//  4. 转换 TextChunk → Milvus 字段（chunk_id/doc_id/content/metadata/embedding）
//  5. MilvusVectorStore.add 写入
func (mi *MilvusIndexer) BuildIndex(
	ctx context.Context,
	chunks []common.TextChunk,
	config common.IndexConfig,
	embedModel embedding.BaseEmbedding,
	opts ...IndexOption,
) (bool, error) {
	indexOpts := NewIndexOptions(opts...)
	collectionName := config.IndexName

	// ─── 步骤 1：确保集合存在 ───
	if err := mi.ensureCollection(ctx, collectionName, config, embedModel); err != nil {
		return false, err
	}

	// ─── 步骤 2：查重 doc_id ───
	// 对齐 Python: all_doc_ids = '["' + '","'.join({chunk.doc_id for chunk in chunks}) + '"]'
	docIDSet := make(map[string]struct{})
	for _, chunk := range chunks {
		if chunk.DocID != "" {
			docIDSet[chunk.DocID] = struct{}{}
		}
	}
	if len(docIDSet) > 0 {
		quoted := make([]string, 0, len(docIDSet))
		for docID := range docIDSet {
			quoted = append(quoted, fmt.Sprintf("%q", docID))
		}
		filterExpr := fmt.Sprintf("%s in [%s]", mi.docIDField, strings.Join(quoted, ","))
		// 对齐 Python: results = self._client.query(collection_name, filter=..., output_fields=[doc_id_field])
		resultSet, err := mi.client.Query(ctx, milvusclient.NewQueryOption(collectionName).
			WithFilter(filterExpr).
			WithOutputFields(mi.docIDField))
		if err != nil {
			return false, exception.BuildError(
				exception.StatusRetrievalIndexingAddDocRuntimeError,
				exception.WithParam("error_msg", err.Error()),
				exception.WithCause(err),
			)
		}
		// 对齐 Python: duplicate_doc_ids = sorted({...} - {None, ""})
		var duplicateDocIDs []string
		if col := resultSet.GetColumn(mi.docIDField); col != nil {
			for j := 0; j < resultSet.ResultCount; j++ {
				if v, getErr := col.GetAsString(j); getErr == nil && v != "" {
					duplicateDocIDs = append(duplicateDocIDs, v)
				}
			}
		}
		if len(duplicateDocIDs) > 0 {
			return false, exception.BuildError(
				exception.StatusRetrievalIndexingAddDocRuntimeError,
				exception.WithParam("error_msg",
					fmt.Sprintf("some documents with same doc_id already exist, if they are the same documents, "+
						"please consider updating instead of adding. duplicate_doc_ids=%v", duplicateDocIDs)),
			)
		}
	}

	// ─── 步骤 3：嵌入 ───
	// 对齐 Python: if config.index_type in ("vector", "hybrid"):
	if config.IndexType == common.IndexTypeVector || config.IndexType == common.IndexTypeHybrid {
		if embedModel == nil {
			// 对齐 Python: EMBED_MODEL_NOT_FOUND
			return false, exception.BuildError(
				exception.StatusRetrievalIndexingEmbedModelNotFound,
				exception.WithParam("error_msg", "embed_model is required for vector/hybrid index type"),
			)
		}
		embedOpts := []ChunkEmbedOption{
			WithUseCaptionForImages(config.UseCaptionForImages),
		}
		if mi.docIndexCallback != nil {
			embedOpts = append(embedOpts, WithDocIndexCallback(mi.docIndexCallback))
		}
		if err := ComputeChunkEmbeddings(ctx, chunks, embedModel, embedOpts...); err != nil {
			return false, err
		}
	}

	// ─── 步骤 4：转换 TextChunk → Milvus 字段 ───
	// 对齐 Python: data = [...{chunk_id, doc_id_field, text_field, metadata_field, [vector_field]}]
	databaseName, _ := indexOpts.Extra["database_name"].(string)
	data := make([]map[string]any, 0, len(chunks))
	for _, chunk := range chunks {
		meta := chunk.Metadata
		if meta == nil {
			meta = make(map[string]any)
		}
		// 对齐 Python: chunk_id = meta.get("chunk_id", chunk.id_)
		chunkID := chunk.ID
		if cid, ok := meta["chunk_id"].(string); ok && cid != "" {
			chunkID = cid
		}
		item := map[string]any{
			"chunk_id":       chunkID,
			mi.docIDField:    chunk.DocID,
			mi.textField:     chunk.Text,
			mi.metadataField: meta,
		}
		if chunk.Embedding != nil {
			item[mi.vectorField.VectorFieldName()] = chunk.Embedding
		}
		data = append(data, item)
	}

	// ─── 步骤 5：写入 ───
	// 对齐 Python: vector_store = MilvusVectorStore(config=..., milvus_uri=..., milvus_token=..., milvus_alias=vs_alias)
	vectorStoreConfig := common.VectorStoreConfig{
		StoreProvider:  common.StoreTypeMilvus,
		CollectionName: collectionName,
		DatabaseName:   databaseName,
	}
	// 对齐 Python: vs_alias = f"{self.milvus_alias}_{collection_name}" if self.milvus_alias else None
	vsAlias := ""
	if mi.milvusAlias != "" {
		vsAlias = mi.milvusAlias + "_" + collectionName
	}
	// Go 版直接复用 indexer 持有的 client（Python 每次新建，语义等价：同一服务）
	vectorStore, err := vector_store.NewMilvusVectorStore(vectorStoreConfig, mi.milvusURI, mi.milvusToken,
		mi.textField, mi.vectorField, mi.sparseVectorField, mi.metadataField, mi.docIDField,
		vector_store.WithMilvusClientOption(mi.client))
	if err != nil {
		return false, err
	}
	_ = vsAlias // alias 仅用于 Python 连接复用，Go 共享 client 已等价

	if err := vectorStore.Add(ctx, data); err != nil {
		return false, err
	}

	// 对齐 Python: logger.info(f"Successfully built index {collection_name} with {len(chunks)} chunks")
	logger.Info(logComponent).
		Str("index_name", collectionName).
		Int("chunk_count", len(chunks)).
		Msg("成功构建索引")

	return true, nil
}

// UpdateIndex 更新索引。先删后 flush 后重建。
//
// 对齐 Python MilvusIndexer.update_index：
// delete_index → flush（Milvus 最终一致性）→ build_index。
func (mi *MilvusIndexer) UpdateIndex(
	ctx context.Context,
	chunks []common.TextChunk,
	docID string,
	config common.IndexConfig,
	embedModel embedding.BaseEmbedding,
	opts ...IndexOption,
) (bool, error) {
	// 对齐 Python: await self.delete_index(doc_id, config.index_name)
	if _, err := mi.DeleteIndex(ctx, docID, config.IndexName); err != nil {
		return false, err
	}

	// 对齐 Python: self._client.flush(collection_name=config.index_name)
	if err := mi.client.Flush(ctx, milvusclient.NewFlushOption(config.IndexName)); err != nil {
		logger.Error(logComponent).
			Str("event_type", "INDEXING_ERROR").
			Str("doc_id", docID).
			Str("index_name", config.IndexName).
			Err(err).
			Msg("更新索引-flush失败")
		return false, err
	}

	// 对齐 Python: return await self.build_index(chunks, config, embed_model, **kwargs)
	return mi.BuildIndex(ctx, chunks, config, embedModel, opts...)
}
```

import 追加：`"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/store/embedding"`（Task 7 已加）。

- [ ] **Step 8.4: 运行全部 indexer 测试**

```bash
cd /home/opensource/uapclaw-gateway && go test ./internal/agentcore/retrieval/indexing/indexer/ -v 2>&1 | tail -25
```

预期：chroma 测试 + milvus 测试全部 PASS

- [ ] **Step 8.5: 提交**

```bash
cd /home/opensource/uapclaw-gateway && git add internal/agentcore/retrieval/ && git commit -m "feat(retrieval): 13.3 MilvusIndexer BuildIndex/UpdateIndex 完整索引构建流程"
```

---

### Task 9: 文档更新 + 状态回填

**Files:**
- Modify: `internal/agentcore/retrieval/utils/doc.go`
- Modify: `internal/agentcore/retrieval/vector_store/doc.go`
- Modify: `internal/agentcore/retrieval/indexing/indexer/doc.go`
- Modify: `IMPLEMENTATION_PLAN.md`

- [ ] **Step 9.1: 更新 utils/doc.go 文件目录**

在文件目录树中追加：

```
//	├── common.go          # Milvus 连接别名生成 + 去重工具
//	├── common_test.go     # 工具测试
```

核心类型索引追加：

```
//	CreateMilvusAlias   — 生成 Milvus 连接别名
//	Deduplicate         — 泛型去重（保留顺序）
```

- [ ] **Step 9.2: 更新 vector_store/doc.go 文件目录**

追加：

```
//	├── milvus.go          # Milvus 向量存储（retrieval 层）
//	├── milvus_test.go     # Milvus 向量存储测试
```

核心类型索引追加：

```
//	MilvusClient              — Milvus 客户端接口（依赖注入）
//	MilvusVectorStore         — Milvus 向量存储实现
//	DefaultMilvusCreateClient — 默认客户端创建函数
```

- [ ] **Step 9.3: 更新 indexer/doc.go 文件目录**

追加：

```
//	├── milvus.go          # Milvus 索引管理器
//	├── milvus_test.go     # Milvus 索引管理器测试
```

核心类型索引追加：

```
//	MilvusIndexer — Milvus 文档索引管理器
```

- [ ] **Step 9.4: 更新 IMPLEMENTATION_PLAN.md 13.3 状态**

将：

```
| 13.3 | ☐ | MilvusIndexer | ...
```

改为（保留原有内容，前置 ✅）：

```
| 13.3 | ✅ | MilvusIndexer | ...
```

同时在产出列追加实际完成内容（对齐 13.2 的风格）：

```
`MilvusIndexer + retrieval层MilvusVectorStore + vector_fields.MilvusVectorFieldConfig统一接口 + CreateMilvusAlias/Deduplicate + ensureCollection含BM25 Function+dense索引+INVERTED标量索引 + HybridSearch原生RRF+降级 + CheckVectorField`
```

- [ ] **Step 9.5: 全量编译 + 测试验证**

```bash
cd /home/opensource/uapclaw-gateway && export GOPROXY=https://goproxy.cn,direct && pgrep -f 'go (build|test)' && pkill -f 'go (build|test)'; go build ./... && go test ./internal/agentcore/retrieval/... ./internal/agentcore/foundation/store/vector_fields/... 2>&1 | tail -10
```

预期：编译通过，测试全部 PASS

- [ ] **Step 9.6: 提交**

```bash
cd /home/opensource/uapclaw-gateway && git add -A && git commit -m "docs(retrieval): 13.3 完成，doc.go 更新 + IMPLEMENTATION_PLAN.md 状态回填"
```

---

## 自查记录

**Spec coverage：**
- ✅ CreateMilvusAlias/Deduplicate → Task 1
- ✅ milvusClient 接口独立 + 适配器 + createClient → Task 2
- ✅ Add/Search/SparseSearch/HybridSearch/Delete/TableExists/DeleteTable/Close/ensureLoaded/normalizeHit/结果转换/getSearchParams → Task 3/4
- ✅ CheckVectorField → Task 5
- ✅ MilvusIndexer 构造/IndexExists/DeleteIndex/GetIndexInfo/Close → Task 6
- ✅ ensureCollection（BM25+dense+INVERTED）→ Task 7
- ✅ BuildIndex/UpdateIndex → Task 8
- ✅ doc.go × 3 + IMPLEMENTATION_PLAN.md → Task 9
- ✅ 分数转换三种度量 → Task 4 milvusResultToSearchResults
- ✅ doc_id/chunk_id metadata 回填 → Task 4
- ✅ dimension 三级推断 → Task 7

**Placeholder scan：** 无 TBD/TODO（variant 检查的 TODO 是对齐 vector_fields 已有 TODO 标记，非本计划缺失）

**Type consistency：**
- `MilvusVectorFieldConfig` 接口在 Task 2 定义，Task 6/7/8 使用一致
- `vector_store.MilvusClient`（Task 6 导出化）与 Task 2 定义一致
- `Indexer` 接口断言 `var _ Indexer = (*MilvusIndexer)(nil)`
- `VectorStore` 接口断言 `var _ VectorStore = (*MilvusVectorStore)(nil)`
- fake client 两处（vector_store 和 indexer 包）各自实现 `MilvusClient` 接口——indexer 包的 fake 用 `vector_store.MilvusClient` 类型

**遗留决策（写代码时注意）：**
1. Task 2 将 `milvusClient` 等改为导出（`MilvusClient`/`MilvusClientAdapter`/`DefaultMilvusCreateClient`）——Task 6 依赖此导出，执行 Task 2 时直接用导出名
2. Task 5 的 fake `hasCollection` 默认值改为 true（在 newFakeRetrievalMilvusClient 构造函数中设置），否则 Task 3 的 ensureLoaded 测试失效
3. Task 7 的 fake CreateCollection 用 proto Request + Schema.Unmarshal 记录 schema；Task 6 测试中 `__exists__` 占位集合名需改为实际集合名
4. Python `_ensure_collection` 中 SCANN 索引的 `index_variant` 拼接（`index_type + "_" + variant.upper()`）——Go vector_fields 无 variant 字段，已在 buildDenseIndex 中按 foundation 层等价实现（SCANN→IVF_PQ），并标注 TODO(#通用) 待 variant 字段补充后回填

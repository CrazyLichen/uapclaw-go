package retriever

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	chromav2 "github.com/amikos-tech/chroma-go/pkg/api/v2"
	milvuscol "github.com/milvus-io/milvus/client/v2/column"
	milvusclient "github.com/milvus-io/milvus/client/v2/milvusclient"
	"google.golang.org/grpc"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/retrieval/vector_store"
)

// ──────────────────────────── Milvus 桩 ────────────────────────────

// milvusQueryStub 仅覆写 Query 的 MilvusClient 桩。
// 嵌入接口：未触达方法 nil panic（测试不触达即安全）。
type milvusQueryStub struct {
	vector_store.MilvusClient
	queryRS    milvusclient.ResultSet
	queryErr   error
	queryCalls int
}

func (m *milvusQueryStub) Query(_ context.Context, _ milvusclient.QueryOption, _ ...grpc.CallOption) (milvusclient.ResultSet, error) {
	m.queryCalls++
	return m.queryRS, m.queryErr
}

// ──────────────────────────── Chroma 桩 ────────────────────────────

// chromaClientStub GetCollection 桩
type chromaClientStub struct {
	chromav2.Client
	collection chromav2.Collection
	getCollErr error
}

func (c *chromaClientStub) GetCollection(_ context.Context, _ string, _ ...chromav2.GetCollectionOption) (chromav2.Collection, error) {
	return c.collection, c.getCollErr
}

// chromaCollectionStub Get 桩
type chromaCollectionStub struct {
	chromav2.Collection
	getResult chromav2.GetResult
	getErr    error
	getCalls  int
}

func (c *chromaCollectionStub) Get(_ context.Context, _ ...chromav2.CollectionGetOption) (chromav2.GetResult, error) {
	c.getCalls++
	return c.getResult, c.getErr
}

// ──────────────────────────── 用例 ────────────────────────────

// TestQueryByFilterMilvus_结果归一化 列数据 → fetchItem
func TestQueryByFilterMilvus_结果归一化(t *testing.T) {
	// 构造 1 行三列：content / metadata(JSON 字符串) / document_id
	// 列构造复用 SDK column 包
	contentCol := milvuscol.NewColumnString("content", []string{"triple text 1"})
	metaCol := milvuscol.NewColumnString("metadata", []string{`{"chunk_id":"c1","doc_id":"d1"}`})
	docIDCol := milvuscol.NewColumnString("document_id", []string{"top-doc"})
	rs := milvusclient.ResultSet{
		ResultCount: 1,
		Fields:      milvusclient.DataSet{contentCol, metaCol, docIDCol},
	}
	stub := &milvusQueryStub{queryRS: rs}

	items, err := queryByFilterMilvus(context.Background(), stub, "kb_1_triples", `chunk_id IN ["c1"]`, 100)
	require.NoError(t, err)
	require.Len(t, items, 1)
	assert.Equal(t, "triple text 1", items[0].content)
	assert.Equal(t, "top-doc", items[0].docIDTop)
	assert.Equal(t, "c1", items[0].metadata["chunk_id"])
	assert.Equal(t, "d1", items[0].metadata["doc_id"])
	assert.Equal(t, 1, stub.queryCalls)
}

// TestQueryByFilterMilvus_空结果
func TestQueryByFilterMilvus_空结果(t *testing.T) {
	stub := &milvusQueryStub{queryRS: milvusclient.ResultSet{ResultCount: 0}}
	items, err := queryByFilterMilvus(context.Background(), stub, "coll", "x", 10)
	require.NoError(t, err)
	assert.Empty(t, items)
}

// TestQueryByFilterMilvus_错误冒泡
func TestQueryByFilterMilvus_错误冒泡(t *testing.T) {
	stub := &milvusQueryStub{queryErr: errors.New("milvus down")}
	_, err := queryByFilterMilvus(context.Background(), stub, "coll", "x", 10)
	assert.Error(t, err)
}

// TestQueryByFilterMilvus_metadata三形态 map 直装 / JSON 字符串解析
func TestQueryByFilterMilvus_metadata三形态(t *testing.T) {
	metaCol := milvuscol.NewColumnString("metadata", []string{
		`{"chunk_id":"c1"}`, // JSON 字符串形态（Python L656-661 防御）
	})
	rs := milvusclient.ResultSet{
		ResultCount: 1,
		Fields:      milvusclient.DataSet{metaCol},
	}
	stub := &milvusQueryStub{queryRS: rs}
	items, err := queryByFilterMilvus(context.Background(), stub, "coll", "x", 10)
	require.NoError(t, err)
	assert.Equal(t, "c1", items[0].metadata["chunk_id"])

	// 非 JSON 字符串 → 空元数据（解析失败吞掉）
	metaCol2 := milvuscol.NewColumnString("metadata", []string{"not-json"})
	rs2 := milvusclient.ResultSet{ResultCount: 1, Fields: milvusclient.DataSet{metaCol2}}
	stub2 := &milvusQueryStub{queryRS: rs2}
	items2, err := queryByFilterMilvus(context.Background(), stub2, "coll", "x", 10)
	require.NoError(t, err)
	assert.Empty(t, items2[0].metadata)
}

// TestQueryByFilterChroma_结果归一化 score 恒 0.0 + metadata 提取
func TestQueryByFilterChroma_结果归一化(t *testing.T) {
	meta, err := chromav2.NewDocumentMetadataFromMap(map[string]interface{}{
		"chunk_id": "c9", "doc_id": "d9",
	})
	require.NoError(t, err)
	getResult := &chromav2.GetResultImpl{
		Ids:       chromav2.DocumentIDs{"id1"},
		Documents: chromav2.Documents{&chromav2.TextDocument{Content: "chunk text"}},
		Metadatas: chromav2.DocumentMetadatas{meta},
	}
	coll := &chromaCollectionStub{getResult: getResult}
	client := &chromaClientStub{collection: coll}

	items, err := queryByFilterChroma(context.Background(), client, "kb_1_chunks", chromav2.InString("chunk_id", "c9"), 100)
	require.NoError(t, err)
	require.Len(t, items, 1)
	assert.Equal(t, "id1", items[0].id)
	assert.Equal(t, "chunk text", items[0].content)
	assert.Equal(t, "c9", items[0].metadata["chunk_id"])
	assert.Equal(t, 0.0, items[0].score) // Chroma get 无分数字面
}

// TestQueryByFilterChroma_错误冒泡 两段错误（GetCollection/Get）
func TestQueryByFilterChroma_错误冒泡(t *testing.T) {
	client := &chromaClientStub{getCollErr: errors.New("no collection")}
	_, err := queryByFilterChroma(context.Background(), client, "coll", chromav2.EqString("chunk_id", "c"), 10)
	assert.Error(t, err)

	coll := &chromaCollectionStub{getErr: errors.New("get failed")}
	client2 := &chromaClientStub{collection: coll}
	_, err = queryByFilterChroma(context.Background(), client2, "coll", chromav2.EqString("chunk_id", "c"), 10)
	assert.Error(t, err)
}

// TestQueryByFilterChroma_空结果
func TestQueryByFilterChroma_空结果(t *testing.T) {
	getResult := &chromav2.GetResultImpl{}
	coll := &chromaCollectionStub{getResult: getResult}
	client := &chromaClientStub{collection: coll}
	items, err := queryByFilterChroma(context.Background(), client, "coll", chromav2.EqString("chunk_id", "c"), 10)
	require.NoError(t, err)
	assert.Empty(t, items)
}

// TestFetchItemsToRetrievalResults doc_id 双源 + chunk_id 回落
func TestFetchItemsToRetrievalResults(t *testing.T) {
	items := []fetchItem{
		{ // 顶层 document_id 优先
			id: "i1", content: "text1", score: 0.9,
			metadata: map[string]any{"chunk_id": "c1", "doc_id": "meta-doc"},
			docIDTop: "top-doc",
		},
		{ // 顶层空回落 metadata
			id: "i2", content: "text2", score: 0.0,
			metadata: map[string]any{"chunk_id": "c2", "doc_id": "meta-doc2"},
		},
		{ // chunk_id 缺失回落 fallback
			id: "i3", content: "text3", metadata: map[string]any{},
		},
	}
	results := fetchItemsToRetrievalResults(items, "query-cid")
	require.Len(t, results, 3)
	assert.Equal(t, "top-doc", results[0].DocID)
	assert.Equal(t, "c1", results[0].ChunkID)
	assert.Equal(t, "meta-doc2", results[1].DocID)
	assert.Equal(t, "query-cid", results[2].ChunkID)
	assert.Equal(t, 0.0, results[1].Score) // Chroma get 无分数字面
	assert.Equal(t, "text1", results[0].Text)
}

// TestFetchItemsToRetrievalResults_空输入
func TestFetchItemsToRetrievalResults_空输入(t *testing.T) {
	assert.Empty(t, fetchItemsToRetrievalResults(nil, ""))
}

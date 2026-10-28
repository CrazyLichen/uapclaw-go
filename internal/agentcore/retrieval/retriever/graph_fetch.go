// 图检索 fetch 层：按 chunk_id 反查三元组/正查 chunk。
//
// Python: graph_retriever.py (_query_by_filter_milvus L537-552 /
// _query_by_filter_chroma L554-596 / 归一化 L654-672、L749-768)
//
// 文件目录：
//
//	（见 retriever/doc.go）
package retriever

import (
	"context"
	"encoding/json"
	"fmt"

	chromav2 "github.com/amikos-tech/chroma-go/pkg/api/v2"
	milvusclient "github.com/milvus-io/milvus/client/v2/milvusclient"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/retrieval/common"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/retrieval/vector_store"
)

// ──────────────────────────── 结构体 ────────────────────────────

// fetchItem Python dict {"id","content","metadata","score","document_id"}
// 的结构化对应物（spec §8-2）。
type fetchItem struct {
	// id 结果 ID
	id string
	// content 文本内容
	content string
	// metadata 元数据
	metadata map[string]any
	// score 相关性分数（Chroma get 恒 0.0）
	score float64
	// docIDTop 顶层 document_id（Milvus query 返回形态；Python L663
	// item.get("document_id") 双源优先侧）
	docIDTop string
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// queryByFilterMilvus 按过滤表达式查询 Milvus 集合。
//
// 对齐 Python _query_by_filter_milvus（L537-552）：filter 字符串 + 全字段
// 输出 + limit。asyncio.to_thread 异步包装在 Go 无对应物（同步调用）。
func queryByFilterMilvus(ctx context.Context, client vector_store.MilvusClient, collectionName, filterExpr string, limit int) ([]fetchItem, error) {
	opt := milvusclient.NewQueryOption(collectionName).
		WithFilter(filterExpr).
		WithOutputFields("*").
		WithLimit(limit)
	rs, err := client.Query(ctx, opt)
	if err != nil {
		return nil, err
	}
	return milvusResultSetToFetchItems(rs), nil
}

// milvusResultSetToFetchItems ResultSet 列数据 → fetchItem 列表
// （13.3 milvusResultToSearchResults 列遍历先例 L1196-1220）。
func milvusResultSetToFetchItems(rs milvusclient.ResultSet) []fetchItem {
	rowCount := rs.ResultCount
	if rowCount == 0 {
		return nil
	}
	colData := make(map[string][]any)
	for _, col := range rs.Fields {
		name := col.Name()
		values := make([]any, rowCount)
		for j := 0; j < rowCount; j++ {
			if v, err := col.Get(j); err == nil {
				values[j] = v
			}
		}
		colData[name] = values
	}

	items := make([]fetchItem, 0, rowCount)
	for j := 0; j < rowCount; j++ {
		item := fetchItem{metadata: map[string]any{}}
		if ids := rs.IDs; ids != nil {
			if v, err := ids.Get(j); err == nil {
				item.id = fmt.Sprintf("%v", v)
			}
		}
		if vals := colData["content"]; j < len(vals) {
			item.content, _ = vals[j].(string)
		}
		// metadata 三形态：map / JSON 字符串（Python L656-661 防御）
		if vals := colData["metadata"]; j < len(vals) {
			switch m := vals[j].(type) {
			case map[string]any:
				for k, v := range m {
					item.metadata[k] = v
				}
			case string:
				var parsed map[string]any
				if json.Unmarshal([]byte(m), &parsed) == nil {
					for k, v := range parsed {
						item.metadata[k] = v
					}
				}
			}
		}
		// document_id 顶层字段（Python L663）
		if vals := colData["document_id"]; j < len(vals) {
			item.docIDTop, _ = vals[j].(string)
		}
		items = append(items, item)
	}
	return items
}

// queryByFilterChroma 按 where 过滤查询 Chroma 集合。
//
// 对齐 Python _query_by_filter_chroma（L554-596）：get_collection +
// get(where, limit)；结果无分数（score 恒 0.0 字面复刻 Python L589）。
// Python 整体 try/except 吞错返回 []，Go 侧错误由调用方
// （fetchTriples/fetchChunk）决定吞或冒泡——异常路径语义在调用方对齐。
func queryByFilterChroma(ctx context.Context, client chromav2.Client, collectionName string, where chromav2.WhereFilter, limit int) ([]fetchItem, error) {
	collection, err := client.GetCollection(ctx, collectionName)
	if err != nil {
		return nil, err
	}
	result, err := collection.Get(ctx, chromav2.WithWhere(where), chromav2.WithLimit(limit))
	if err != nil {
		return nil, err
	}

	ids := result.GetIDs()
	docs := result.GetDocuments()
	metas := result.GetMetadatas()

	items := make([]fetchItem, 0, len(ids))
	for i, id := range ids {
		item := fetchItem{id: string(id), metadata: map[string]any{}}
		if i < len(docs) && docs[i] != nil {
			item.content = docs[i].ContentString()
		}
		if i < len(metas) && metas[i] != nil {
			meta := metas[i]
			for _, key := range chromaMetaKeysForFetch(meta) {
				if val, ok := meta.GetRaw(key); ok {
					if mv, isMv := val.(chromav2.MetadataValue); isMv {
						if rawVal, ok := mv.GetRaw(); ok {
							item.metadata[key] = rawVal
						}
					} else {
						item.metadata[key] = val
					}
				}
			}
		}
		item.score = 0.0 // Chroma get 不返分数
		items = append(items, item)
	}
	return items, nil
}

// chromaMetaKeysForFetch DocumentMetadata 键集（chroma.go chromaMetaKeys
// 同款逻辑；包私有不导出，本包复制避免公开面扩大）。
func chromaMetaKeysForFetch(meta chromav2.DocumentMetadata) []string {
	if impl, ok := meta.(*chromav2.DocumentMetadataImpl); ok {
		return impl.Keys()
	}
	return nil
}

// fetchItemsToRetrievalResults fetchItem → RetrievalResult 归一化。
//
// 对齐 Python L663-671/L758-767：doc_id 双源（docIDTop 空串回落
// metadata，Python or 语义）；chunk_id metadata 回落 fallback（_fetch_
// triples 传 ""，_fetch_chunks 传查询 chunk_id——Python 两处
// metadata.get("chunk_id", 默认值) 不同默认，由调用方区分）。
func fetchItemsToRetrievalResults(items []fetchItem, chunkIDFallback string) []common.RetrievalResult {
	results := make([]common.RetrievalResult, 0, len(items))
	for _, item := range items {
		docID := item.docIDTop
		if docID == "" {
			docID, _ = item.metadata["doc_id"].(string)
		}
		chunkID, ok := item.metadata["chunk_id"].(string)
		if !ok || chunkID == "" {
			chunkID = chunkIDFallback
		}
		results = append(results, common.RetrievalResult{
			Text:     item.content,
			Score:    item.score,
			Metadata: item.metadata,
			DocID:    docID,
			ChunkID:  chunkID,
		})
	}
	return results
}

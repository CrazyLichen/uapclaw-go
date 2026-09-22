package vector_store

import (
	"context"
	"fmt"
	"math"
	"sort"
	"sync"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/context_evolver/core/schema"
)

// ──────────────────────────── 结构体 ────────────────────────────

// MemoryVectorStore 内存向量库，使用余弦相似度搜索。
//
// Python 用 numpy 计算余弦相似度，Go 手写（点积 / 范数乘积），纯 math 包。
// Python 中 async_upsert/async_search/async_delete 是异步方法，
// Go 全部同步，通过 context.Context 传递取消信号。
//
// Python: openjiuwen/extensions/context_evolver/core/vector_store/memory_vector_store.py
type MemoryVectorStore struct {
	mu      sync.RWMutex
	vectors map[string]*schema.VectorNode
}

// ──────────────────────────── 导出函数 ────────────────────────────

// NewMemoryVectorStore 创建空的内存向量库。
func NewMemoryVectorStore() *MemoryVectorStore {
	return &MemoryVectorStore{vectors: make(map[string]*schema.VectorNode)}
}

// ──────────────────────────── 导出函数 ────────────────────────────

// Upsert 插入或更新向量节点。
// 对齐 Python MemoryVectorStore.async_upsert(node)。
// 节点必须有 embedding，否则返回 error（对齐 Python ValueError）。
func (s *MemoryVectorStore) Upsert(_ context.Context, node *schema.VectorNode) error {
	if node.Embedding == nil {
		return fmt.Errorf("node %s has no embedding", node.ID)
	}
	s.mu.Lock()
	s.vectors[node.ID] = node
	s.mu.Unlock()
	return nil
}

// Search 向量相似度搜索（余弦相似度）。
// 对齐 Python MemoryVectorStore.async_search(embedding, top_k, metadata_filter)。
// 返回与 embedding 最相似的 topK 个节点，可选按 metadata 过滤。
// 过滤使用精确匹配（对齐 Python metadata.get(k) == val）。
// 零范数向量的相似度为 0.0（对齐 Python，不除零）。
func (s *MemoryVectorStore) Search(_ context.Context, embedding []float64, topK int, metadataFilter map[string]any) ([]*schema.VectorNode, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if len(s.vectors) == 0 {
		return nil, nil
	}

	type scored struct {
		node       *schema.VectorNode
		similarity float64
	}

	var candidates []scored
	for _, node := range s.vectors {
		if !matchesMetadata(node.Metadata, metadataFilter) {
			continue
		}
		if node.Embedding == nil {
			continue // 跳过无 embedding 的节点
		}
		sim := cosineSimilarity(embedding, node.Embedding)
		candidates = append(candidates, scored{node: node, similarity: sim})
	}

	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].similarity > candidates[j].similarity
	})

	if topK > len(candidates) {
		topK = len(candidates)
	}
	results := make([]*schema.VectorNode, topK)
	for i := 0; i < topK; i++ {
		results[i] = candidates[i].node
	}
	return results, nil
}

// Delete 删除向量节点。返回是否实际删除。
// 对齐 Python MemoryVectorStore.async_delete(node_id)。
func (s *MemoryVectorStore) Delete(_ context.Context, nodeID string) (bool, error) {
	s.mu.Lock()
	_, exists := s.vectors[nodeID]
	if exists {
		delete(s.vectors, nodeID)
	}
	s.mu.Unlock()
	return exists, nil
}

// Clear 清除所有向量。
// 对齐 Python MemoryVectorStore.clear()。
func (s *MemoryVectorStore) Clear() {
	s.mu.Lock()
	s.vectors = make(map[string]*schema.VectorNode)
	s.mu.Unlock()
}

// Count 获取向量数量。
// 对齐 Python MemoryVectorStore.count 属性。
func (s *MemoryVectorStore) Count() int {
	s.mu.RLock()
	n := len(s.vectors)
	s.mu.RUnlock()
	return n
}

// GetAll 获取所有向量，可选按 metadata 过滤。
// 对齐 Python MemoryVectorStore.get_all(metadata_filter)。
func (s *MemoryVectorStore) GetAll(metadataFilter map[string]any) []*schema.VectorNode {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var result []*schema.VectorNode
	for _, node := range s.vectors {
		if matchesMetadata(node.Metadata, metadataFilter) {
			result = append(result, node)
		}
	}
	return result
}

// LoadNode 直接加载单个向量节点（用于反序列化，跳过 embedding 检查）。
// 对齐 Python MemoryVectorStore.load_node(node_id, node)。
// 与 Upsert 不同，此方法不校验 embedding 是否为 nil。
func (s *MemoryVectorStore) LoadNode(nodeID string, node *schema.VectorNode) {
	s.mu.Lock()
	s.vectors[nodeID] = node
	s.mu.Unlock()
}

// LoadFromDict 从字典加载多个向量节点。
// 对齐 Python MemoryVectorStore.async_load_from_dict(data)。
// 跳过 embedding 为 nil 的节点（对齐 Python 行为）。
func (s *MemoryVectorStore) LoadFromDict(data map[string]map[string]any) error {
	for nodeID, nodeData := range data {
		node, err := schema.VectorNodeFromDict(nodeData)
		if err != nil {
			return fmt.Errorf("load node %s: %w", nodeID, err)
		}
		// 对齐 Python：跳过无 embedding 的节点
		if node.Embedding == nil {
			continue
		}
		s.LoadNode(nodeID, node)
	}
	return nil
}

// String 实现 Stringer 接口。
// 对齐 Python MemoryVectorStore.__repr__()。
func (s *MemoryVectorStore) String() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return fmt.Sprintf("MemoryVectorStore(count=%d)", len(s.vectors))
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// cosineSimilarity 计算两个向量的余弦相似度。
// 对齐 Python numpy: dot(a,b) / (norm(a) * norm(b))。
// 零范数向量返回 0.0（避免除零）。
func cosineSimilarity(a, b []float64) float64 {
	dot := 0.0
	normA := 0.0
	normB := 0.0
	minLen := len(a)
	if len(b) < minLen {
		minLen = len(b)
	}
	for i := 0; i < minLen; i++ {
		dot += a[i] * b[i]
		normA += a[i] * a[i]
		normB += b[i] * b[i]
	}
	normA = math.Sqrt(normA)
	normB = math.Sqrt(normB)
	if normA == 0 || normB == 0 {
		return 0
	}
	return dot / (normA * normB)
}

// matchesMetadata 检查节点的 metadata 是否匹配过滤条件。
// 对齐 Python: 对每个 filter key 执行 metadata.get(k) == val。
// 缺失的 key 返回 None，与 val 不等（除非 val 也是 None）。
func matchesMetadata(metadata, filter map[string]any) bool {
	if filter == nil {
		return true
	}
	for k, v := range filter {
		// 对齐 Python metadata.get(k) == val
		metaVal, ok := metadata[k]
		if !ok {
			// Python 中 dict.get(k) 返回 None（键不存在），与 v 比较不等
			// 除非 v 也是 nil
			if v != nil {
				return false
			}
			continue
		}
		if metaVal != v {
			return false
		}
	}
	return true
}

package common

// ──────────────────────────── 结构体 ────────────────────────────

// SearchResult 搜索结果数据模型。
//
// Python: retrieval/common/retrieval_result.py (SearchResult)
type SearchResult struct {
	// ID 结果 ID
	ID string `json:"id"`
	// Text 文本内容
	Text string `json:"text"`
	// Score 相关性分数
	Score float64 `json:"score"`
	// Metadata 元数据
	Metadata map[string]any `json:"metadata"`
}

// RetrievalResult 检索结果数据模型。
//
// Python: retrieval/common/retrieval_result.py (RetrievalResult)
type RetrievalResult struct {
	// Text 文本内容
	Text string `json:"text"`
	// Score 相关性分数
	Score float64 `json:"score"`
	// Metadata 元数据
	Metadata map[string]any `json:"metadata"`
	// DocID 文档 ID
	DocID string `json:"doc_id,omitempty"`
	// ChunkID 分块 ID
	ChunkID string `json:"chunk_id,omitempty"`
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// ──────────────────────────── 非导出函数 ────────────────────────────

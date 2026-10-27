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

// MultiKBRetrievalResult 多知识库聚合检索结果数据模型。
//
// 多 KB 并行检索按 text 聚合去重后的出口契约：Score 为跨 KB 融合
// 最大值，RawScore/RawScoreScaled 保真源库分数，KbIDs 记录命中的
// 全部知识库。
//
// Python: openjiuwen/core/retrieval/common/retrieval_result.py (MultiKBRetrievalResult)
// RawScore/RawScoreScaled 用 float64 非指针——13.27 构造点默认值
// 回落使实际数据流恒非 None；metadata 显式 None 理论路径归档 spec §8-3。
type MultiKBRetrievalResult struct {
	// Text 文本内容（聚合键）
	Text string `json:"text"`
	// Score 跨 KB 融合分数（最大值）
	Score float64 `json:"score"`
	// RawScore 源库原始分数
	RawScore float64 `json:"raw_score"`
	// RawScoreScaled 缩放到 [0,1] 的原始分数
	RawScoreScaled float64 `json:"raw_score_scaled"`
	// KbIDs 命中的知识库 ID 列表（无 omitempty——default_factory 先例，
	// nil 序列化为 null 由构造方初始化保证，同 Metadata 契约）
	KbIDs []string `json:"kb_ids"`
	// Metadata 元数据（同上无 omitempty）
	Metadata map[string]any `json:"metadata"`
}

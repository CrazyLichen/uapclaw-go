// 能力接口：Python hasattr/getattr 鸭子探测的 Go 同构物。
//
// 类型断言三态语义（13.23 spec §3.1）：
//   - 未实现接口    ↔ Python hasattr False（如 SparseRetriever.embed_model）
//   - 实现返回 nil  ↔ Python 属性存在值为 None
//   - 实现返回非 nil ↔ Python 属性存在且非 None
//
// 放置约定（13.23 审查拍板 A+）：
//   - 有跨包消费方 → 本文件导出（如 IndexTypeCarrier 的 SetIndexType
//     由 13.28 GraphKB 跨包注入）
//   - 仅包内消费   → 用点处包内小接口断言（如 graph_retriever_impl.go
//     的 collectionNameSetter——Python collection_name 外部写入点全库
//     仅 graph_retriever.py L340 一处，无跨包需求）
//   - 本文件不导出仅包内消费的接口，避免公开面扩大
//     （chromaMetaKeysForFetch 同款原则）
//
// 文件目录：
//
//	（见 retriever/doc.go）
package retriever

import (
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/store/embedding"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/retrieval/vector_store"
)

// ──────────────────────────── 接口 ────────────────────────────

// EmbedModelProvider 嵌入模型提供者。
//
// Python: hasattr(retriever, "embed_model")（graph_retriever.py L48）
type EmbedModelProvider interface {
	// EmbedModel 返回嵌入模型（nil = Python None）
	EmbedModel() embedding.BaseEmbedding
}

// IndexTypeCarrier index_type 载体（上层 KnowledgeBase 注入）。
//
// Python: getattr(retriever, "index_type", None)（L193/L375）+ 动态属性
// 注入（L376）。13.25 AgenticRetriever 构造 default_mode 推导复用。
type IndexTypeCarrier interface {
	// IndexType 返回 index_type（"" = Python None 未注入）
	IndexType() string
	// SetIndexType 注入 index_type（上层 13.28 GraphKB 调用）
	SetIndexType(indexType string)
}

// VectorStoreAccessor 向量存储访问器。
//
// Python: hasattr(retriever, "vector_store") + 属性挖取（L622-631）
type VectorStoreAccessor interface {
	// VectorStore 返回底层向量存储（nil = Python 无该属性/None）
	VectorStore() vector_store.VectorStore
}

// MaxBatchSizeProvider 嵌入批大小提供者。
//
// Python: embed_model.max_batch_size（L91）——retrieval 层四实现
// （API/Dashscope/OpenAI/VLLM）的实例字段，foundation 层 Embedding ABC
// 无此属性。断言失败显式报 155202（拍板 c，spec §3.7）。
type MaxBatchSizeProvider interface {
	// MaxBatchSize 返回每批最大文档数
	MaxBatchSize() int
}

// SupportedModesProvider 支持模式声明（Python SUPPORTED_MODES 类属性
// 探测通道，L299）。Retriever 接口（13.19）无此方法；测试 stub/自定义
// 实现可选用。
type SupportedModesProvider interface {
	// SupportedModes 返回支持的 RetrievalMode 清单
	SupportedModes() []RetrievalMode
}

// collectionNameSetter 集合名注入通道（13.23 包内小接口，审查 A+ 迁入）。
//
// Python self.vector_store.collection_name = name 字段赋值（L340，全库
// 唯一外部写入点）；VectorStore 接口（13.2）无此方法，Milvus/Chroma 真
// 实现与测试桩经此断言注入。未实现的 store 跳过（宽松处理归档 spec §8-9）。
type collectionNameSetter interface {
	SetCollectionName(string)
}

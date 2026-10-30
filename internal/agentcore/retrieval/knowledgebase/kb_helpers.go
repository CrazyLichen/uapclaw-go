// 知识库包内共享辅助：两个子类（SimpleKnowledgeBase / GraphKnowledgeBase）
// 共用的默认配置、模式推导、database_name 取值与 agentic 包装。
//
// 放独立文件而非任一子类文件：这两个函数的消费方是两个平级子类，
// 挂在其一会造成"另一子类依赖该子类文件"的错觉（13.28 拍板）。
//
// 文件目录：
//
//	（见 knowledgebase/doc.go）
package knowledgebase

import (
	"fmt"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/model_clients"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/retrieval/common"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/retrieval/retriever"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/retrieval/vector_store"
	"github.com/uapclaw/uapclaw-go/internal/common/exception"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 非导出函数 ────────────────────────────

// defaultRetrievalConfig 默认检索配置（对齐 Python RetrievalConfig() 默认
// top_k=5，其余字段为零值/nil）。
func defaultRetrievalConfig() *common.RetrievalConfig {
	return &common.RetrievalConfig{TopK: 5}
}

// resolveRetrievalMode 由 index_type 推导检索模式。
//
// Python: mode = "hybrid"; index_type=="vector"→"vector"；
// index_type=="bm25"→"sparse"；其余保持 "hybrid"。
func resolveRetrievalMode(indexType common.IndexTypeKind) retriever.RetrievalMode {
	switch indexType {
	case common.IndexTypeVector:
		return retriever.RetrievalModeVector
	case common.IndexTypeBM25:
		return retriever.RetrievalModeSparse
	default:
		return retriever.RetrievalModeHybrid
	}
}

// vectorStoreDatabaseName 防御式取 vector_store 的 database_name。
//
// Python: getattr(getattr(self.vector_store, "config", None), "database_name", "")
// 且非 str 归零。Go 用接口断言表达"属性存在性"，未实现即回落空串；
// 非 str 归零由静态类型天然满足（13.27 spec §8-5）。
func vectorStoreDatabaseName(vs vector_store.VectorStore) string {
	if vs == nil {
		return ""
	}
	if p, ok := vs.(interface{ DatabaseName() string }); ok {
		return p.DatabaseName()
	}
	return ""
}

// wrapAgenticRetriever 用 AgenticRetriever 包装底层 retriever。
//
// 对齐 Python `AgenticRetriever(retriever=..., llm_client=..., **kwargs)`：
// kwargs 只认 max_iter，经 KBOptions.Extra["max_iter"] 映射。Python 的
// AgenticRetriever 不接受其他关键字参数，多传会在**构造前**抛 TypeError
// 让整条链路失败；Go 同样拒绝（155510），不静默忽略。
// max_iter 类型不符时 Python 侧 isinstance 判假 → Warn 回落默认 2，
// Go 返回同样的告警（13.27 spec §8-6）。
func wrapAgenticRetriever(llm model_clients.BaseModelClient, r retriever.Retriever,
	opts ...KBOption) (retriever.Retriever, error) {
	kbOpts := NewKBOptions(opts...)
	var agenticOpts []retriever.AgenticRetrieverOption
	for key, v := range kbOpts.Extra {
		switch key {
		case "max_iter":
			n, ok := v.(int)
			if !ok {
				// Python isinstance(max_iter, int) 判假 → Warn 回落默认 2，不报错
				logger.Warn(logComponent).
					Str("max_iter_type", fmt.Sprintf("%T", v)).
					Msg("max_iter is not an int, falling back to default")
				continue
			}
			agenticOpts = append(agenticOpts, retriever.WithAgenticMaxIter(n))
		default:
			return nil, exception.BuildError(
				exception.StatusAgenticKwargsUnexpected,
				exception.WithMsg(fmt.Sprintf(
					"AgenticRetriever() got an unexpected keyword argument '%s'", key)),
			)
		}
	}
	return retriever.NewAgenticRetriever(r, llm, agenticOpts...)
}

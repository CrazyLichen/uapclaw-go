// Package recall 提供压缩召回的检索原语。
// 当前只落地 BM25 索引与中英混排分词；archive/retriever 与 compression_recall
// 工具属"压缩后召回原文"能力，另开章节实施。
// 文件目录：
//
//	recall/
//	├── doc.go    # 包文档
//	└── bm25.go   # BM25Index + 中英混排分词
//
// 对应 Python 代码：openjiuwen/core/context_engine/processor/forked/compressor/recall/
//
// 已知偏差：Go 侧未建 forked 目录层（5.22-5.26 已按非 forked 家族对齐），
// 故本包落在 processor/compressor/recall/ 下。
package recall

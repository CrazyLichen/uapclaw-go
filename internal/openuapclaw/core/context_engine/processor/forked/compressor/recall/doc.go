// Package recall 提供压缩召回原语：BM25 索引与中英混排分词、
// 压缩归档（archive）、归档检索（retriever）。
//
// bm25.go 为 5.32 批迁入产物（Python: recall/bm25.py 123 行），24.2 批自
// processor/compressor/recall/ 搬移至此对齐 Python 目录层级（销"未建 forked
// 目录层"历史偏差）；archive/retriever 为 24.2 批整件新迁。retriever 的
// 生产消费方 compression_recall 工具属 24.15 15b 批（同 5.32 先迁原语口径）。
//
// 文件目录：
//
//	recall/
//	├── doc.go         # 包文档
//	├── bm25.go        # BM25Index + 中英混排分词（5.32 迁入，24.2 搬移）
//	├── archive.go     # 压缩归档（schema v1、chunk 切分）
//	└── retriever.go   # 归档检索（BM25 打分 + budget 截断）
//
// 对应 Python 代码：openjiuwen/core/context_engine/processor/forked/compressor/recall/
package recall

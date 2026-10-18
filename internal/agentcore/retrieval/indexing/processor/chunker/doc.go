// Package chunker 提供文本分块抽象与策略注册：将整篇 Document 切成
// 可嵌入的 TextChunk，供 Indexer.BuildIndex 消费。
//
// 知识检索入库管线：Parser 解析 → 本包分块 → ComputeChunkEmbeddings 嵌入 →
// VectorStore 入库。分块携带 doc_id/chunk_index/total_chunks/chunk_id 溯源
// 元数据，支撑按文档整体删除与结果回溯。
//
// 使用方式（对齐 Python get_chunker/register_chunker）：
//   - 按名获取内置分块器：GetChunker("char", WithChunkSize(512))
//   - 自定义分块器：实现 Chunker 接口（嵌入 BaseChunker 获得默认
//     ChunkDocuments/Process），或 RegisterChunker("yourname", factory) 注册
//
// 文件目录：
//
//	chunker/
//	├── doc.go            # 包文档
//	├── base.go           # Chunker 接口 + BaseChunker 默认适配器 + 构造校验
//	├── registry.go       # 分块器注册表 + GetChunker/RegisterChunker + hybridFactory
//	├── text_splitter.go  # TextSplitter 接口 + CharSplitter 滑窗分割（IndexSentenceSplitter 13.7）
//	└── char_chunker.go   # CharChunker 字符级分块器（"char" 注册项）
//
// 对应 Python 代码：openjiuwen/core/retrieval/indexing/processor/chunker/
package chunker

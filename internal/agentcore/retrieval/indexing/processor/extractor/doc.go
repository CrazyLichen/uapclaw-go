// Package extractor 提供知识抽取抽象与 LLM 三元组抽取实现：把非结构化
// chunk 文本抽取为结构化知识（RDF 风格三元组），供图谱索引消费。
//
// 知识检索入库管线位置：Chunker 产 TextChunk 后，本包 Extractor 把
// 文本抽成 (subject, predicate, object) 三元组（13.17 TripleExtractor，
// use_graph=true 时启用），三元组转 TextChunk 再走同一 Indexer 入库
// kb_{id}_triples——是 GraphRAG 图谱索引的写入原料。读取侧消费方为
// 13.23 GraphRetriever（beam search）与 13.28 GraphKnowledgeBase
// （add_documents 中调用 extractor.extract）。
//
// 并发模型（方案 A，对齐 Python asyncio.gather(return_exceptions=True)）：
// goroutine 预分配槽位各写 results[i] + buffered chan 信号量
// （asyncio.Semaphore 等价）+ wg 收齐后按 chunk 序扫描——全部任务跑完
// 才扫、首错按 chunk 序选、BaseError 原样返回。该 gather 模式在
// 13.23/13.25/13.27 共 5 处再现，本包实现为复用资产。
//
// JSON 解析走 jsonrepair.RepairWithOptions 一步取 result.Value（对齐
// Python repair_json(return_objects=True)）；提示词模板逐字复制 Python
// 源（{{/}} 归一为 {/}）经 strings.NewReplacer 渲染唯二命名槽。
//
// 文件目录：
//
//	extractor/
//	├── doc.go               # 包文档
//	├── base.go              # Extractor 接口 + ExtractOption（13.17）
//	└── triple_extractor.go  # TripleExtractor：LLM OpenIE 三元组抽取（13.17）
//
// 对应 Python 代码：
//
//	openjiuwen/core/retrieval/indexing/processor/extractor/
//	  （base.py + triple_extractor.py）
//	openjiuwen/core/retrieval/common/triple.py（Triple 数据模型，Go 侧在
//	retrieval/common/triple.go）
//
// 核心类型索引：
//
//	Extractor       — 抽取器抽象接口（Extract + Process 双方法）
//	ExtractOption   — Extract 可选参数（**kwargs 收纳）
//	TripleExtractor — 基于 LLM 的 OpenIE 三元组抽取器
package extractor

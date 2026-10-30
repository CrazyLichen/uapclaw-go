// Package knowledgebase 提供知识库编排层抽象与实现。
//
// KnowledgeBase 是检索管线的顶层门面：统一文档写入（解析→分块→嵌入→
// 入库）与查询读取（多策略检索）的出入口。BaseKnowledgeBase 提供共享
// 基座（组件字段 + 配置一致性防线 + 4 模板方法），SimpleKnowledgeBase
// （13.27）与 GraphKnowledgeBase（13.28）作为子类嵌入实现 5 抽象方法。
//
// 核心机制——配置一致性防线：读侧 vector_store 与写侧 index_manager
// 分开构造、各持 7 项 schema 配置，两者首次配齐（构造尾部或二次 setter）
// 时逐项比对，不一致抛 155508（防"写 db1 读 db2 → 检索静默为空"漂移）。
//
// 文件目录：
//
//	knowledgebase/
//	├── doc.go           # 包文档
//	├── base.go          # KnowledgeBase 接口 + BaseKnowledgeBase + 构造/setter/模板方法（13.26）
//	├── validate.go      # ValidateIndex 三段式 + attrEqual + isChromaType（13.26）
//	├── kb_helpers.go    # 包级复用：默认检索配置 / mode 推导 / database_name 取值 / agentic 包装（13.28）
//	├── simple_kb.go     # SimpleKnowledgeBase 完整实现（13.27，5 抽象方法 + 懒创建 + agentic 包装）
//	├── multi_kb.go      # 跨知识库聚合检索两函数（13.27）
//	└── graph_kb.go      # GraphKnowledgeBase 图增强实现（13.28，双索引写入 + 图路径检索）
//
// SimpleKnowledgeBase（13.27）实现 5 抽象方法，是检索查询管线在会话侧的
// 总入口；multi_kb.go 提供跨知识库聚合检索。
//
// GraphKnowledgeBase（13.28）是图增强实现：写侧 use_graph 时抽取三元组、
// 建 kb_{id}_triples 索引（三元组 chunk 的 metadata 承载 doc_id/chunk_id，
// 供 13.23 graph_fetch 回链原始分块）；读侧懒创建 GraphRetriever 走 beam
// search 图检索，注入 index_type 后由检索器自行校验 mode；未启用图检索
// 时回落内建 SimpleKnowledgeBase（不传 extractor）。
//
// 对应 Python 代码：
//
//	openjiuwen/core/retrieval/knowledge_base.py
//	openjiuwen/core/retrieval/simple_knowledge_base.py
//	openjiuwen/core/retrieval/graph_knowledge_base.py
//
// 核心类型/接口索引：
//
//	KnowledgeBase      — 知识库抽象接口（5 抽象 + 4 模板 + Config）
//	BaseKnowledgeBase  — 子类共享基座（嵌入）
//	SimpleKnowledgeBase — 简单知识库实现（向量/稀疏/混合 + agentic）
//	GraphKnowledgeBase  — 图增强知识库实现（双索引 + 图检索）
package knowledgebase

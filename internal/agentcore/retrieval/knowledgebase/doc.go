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
//	└── simple_kb.go     # SimpleKnowledgeBase 完整实现（13.27，5 抽象方法 + 懒创建 + agentic 包装）
//
// 13.28 graph.go 落地时补充目录条目与对应 Python 路径。
//
// SimpleKnowledgeBase（13.27）实现 5 抽象方法，是检索查询管线在会话侧的
// 总入口。
//
// 对应 Python 代码：
//
//	openjiuwen/core/retrieval/knowledge_base.py
//
// 核心类型/接口索引：
//
//	KnowledgeBase     — 知识库抽象接口（5 抽象 + 4 模板 + Config）
//	BaseKnowledgeBase — 子类共享基座（嵌入）
package knowledgebase

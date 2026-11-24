// Package resource 提供 workflow 资源类组件（知识检索/长记忆读写）。
//
// 一比一对齐 Python openjiuwen/core/workflow/components/resource/ 三组件：
// 知识库检索（KnowledgeRetrievalComp）、长记忆检索（MemoryRetrievalComp）、
// 长记忆写入（MemoryWriteComp）。均为纯 INVOKE 叶子执行节点，不涉流式路径。
// 知识检索组件消费领域十三既有检索管线（13.26-13.28）；长记忆两件直接持有
// ltm.LongTermMemory 单例（7.27），无惰性构建。
//
// 文件目录：
//
//	resource/
//	├── doc.go                        # 包文档
//	├── knowledge_retrieval_comp.go  # 知识库检索组件（Python knowledge_retrieval_comp.py）
//	├── memory_retrieval_comp.go     # 长记忆检索组件（Python memory_retrieval_comp.py）
//	└── memory_write_comp.go         # 长记忆写入组件（Python memory_write_comp.py）
//
// 对应 Python 代码：openjiuwen/core/workflow/components/resource/
package resource

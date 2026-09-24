// Package core 提供上下文记忆演化系统的核心运行时基础设施。
//
// 包含操作执行上下文（RuntimeContext/ServiceContext）、
// 可组合操作管线（BaseOp/SequentialOp/ParallelOp）、
// 向量存储格式（VectorNode）、内存向量库（MemoryVectorStore）、
// 文件持久化（JSONFileConnector）和持久化助手（MemoryPersistenceHelper）。
//
// 文件目录：
//
//	core/
//	├── doc.go                               # 包文档
//	├── config/                              # 配置管理（懒加载全局单例）
//	├── context/                             # 操作执行上下文
//	├── op/                                  # 可组合操作管线
//	├── schema/                              # 向量存储格式
//	├── vector_store/                        # 内存向量库
//	├── file_connector/                      # 文件持久化连接器
//	└── persistence/                         # 持久化助手
//
// 对应 Python 代码：openjiuwen/extensions/context_evolver/core/
package core

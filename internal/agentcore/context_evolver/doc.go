// Package contextevolver 提供上下文记忆演化系统（Context Evolver）。
//
// 上下文记忆演化系统让 Agent 具备跨会话的经验记忆能力——从任务执行轨迹中
// 提取蒸馏后的经验知识，以向量化记忆形式持久化，并在后续任务中检索注入。
// 包含 3 条独立的记忆算法管线（ACE / ReasoningBank / ReMe），
// 本包实现所有管线的共享运行时基础设施和 IO 数据模式。
//
// 文件目录：
//
//	context_evolver/
//	├── doc.go                                # 包文档
//	├── core/                                 # 核心框架子包
//	│   ├── context/                          # RuntimeContext + ServiceContext
//	│   ├── op/                               # BaseOp + SequentialOp + ParallelOp
//	│   ├── schema/                           # VectorNode
//	│   ├── vector_store/                     # MemoryVectorStore
//	│   ├── file_connector/                   # JSONFileConnector
//	│   └── persistence/                      # MemoryPersistenceHelper
//	└── schema/                               # IO Schema 子包
//	    ├── doc.go                            # 包文档
//	    ├── trajectory.go                     # FeedbackType + Trajectory + TrajectoryBatch
//	    ├── memory.go                         # BaseMemory + MemoryInterface + ACE/RB/ReMe Memory
//	    └── io_schema.go                      # ACE/RB/ReMe Request/Response + 泛型 Response
//
// 对应 Python 代码：openjiuwen/extensions/context_evolver/
package contextevolver

// Package schema 提供上下文记忆演化系统的 IO 数据模式定义。
//
// 定义三条算法管线（ACE/ReasoningBank/ReMe）共用的输入输出类型，
// 包括轨迹（Trajectory）、记忆（Memory）和请求/响应（Request/Response）。
// 所有 Memory 类型实现 MemoryInterface 接口，支持与 VectorNode 双向转换。
//
// 文件目录：
//
//	schema/
//	├── doc.go           # 包文档
//	├── trajectory.go    # FeedbackType + Trajectory + TrajectoryBatch 轨迹系列
//	├── memory.go        # BaseMemory + MemoryInterface + ACE/RB/ReMe Memory 类型
//	└── io_schema.go     # ACE/RB/ReMe Request/Response + 泛型 SummarizeResponse/RetrieveResponse
//
// 对应 Python 代码：openjiuwen/extensions/context_evolver/schema/
package schema

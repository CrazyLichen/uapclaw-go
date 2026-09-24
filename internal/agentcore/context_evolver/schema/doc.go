// Package schema 提供上下文记忆演化系统的 IO 数据模式定义。
//
// 定义三条算法管线（ACE/ReasoningBank/ReMe）共用的输入输出类型，
// 包括轨迹（Trajectory）、记忆（Memory）和请求/响应（Request/Response）。
// 所有 Memory 类型（ACEMemory/ReasoningBankMemory/ReMeMemory）实现 MemoryInterface 接口，
// 支持与 VectorNode 双向转换；MemoryInterface 方法使用值接收者，值类型和指针类型均可满足接口。
// MemoryItem 接口统一不同算法的检索结果类型，各 RetrievedMemory 类型实现 FormatMemoryString 方法。
//
// 泛型约束区分：
//   - SummarizeResponse[T MemoryInterface]：摘要响应中的 Memory 必须是可向量化的完整记忆类型
//   - RetrieveResponse[T any]：检索响应中的 RetrievedMemory 是只读快照，不需要向量化，
//     因此不实现 MemoryInterface
//
// 文件目录：
//
//	schema/
//	├── doc.go           # 包文档
//	├── trajectory.go    # FeedbackType + Trajectory + TrajectoryBatch 轨迹系列
//	├── memory.go        # BaseMemory + MemoryInterface + ACE/RB/ReMe Memory + FormatMemoryString
//	└── io_schema.go     # MemoryItem 接口 + ACE/RB/ReMe Request/Response + 泛型 SummarizeResponse/RetrieveResponse
//
// 对应 Python 代码：openjiuwen/extensions/context_evolver/schema/
package schema

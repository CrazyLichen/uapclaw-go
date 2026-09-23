// Package rb 提供 ReasoningBank 算法的摘要管线操作。
//
// 包含轨迹标签判定（LabelDeterminator）、记忆项解析（MemoryItemParser）、
// 单轨迹/多轨迹策略提取（SummarizeMemoryOp/SummarizeMemoryParallelOp）、
// 向量存储更新和持久化操作。
//
// 文件目录：
//
//	rb/
//	├── doc.go           # 包文档
//	├── update.go        # SummarizeMemoryOp + SummarizeMemoryParallelOp + UpdateVectorStoreOp + PersistMemoryOp
//	├── label.go         # LabelDeterminator 轨迹标签判定器
//	├── parser.go        # MemoryItemParser 记忆项解析器
//	└── prompt.go        # 摘要相关提示词模板
//
// 对应 Python 代码：openjiuwen/extensions/context_evolver/summary/task/reasoning_bank/
package rb

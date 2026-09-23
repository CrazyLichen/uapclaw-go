// Package rb 提供 ReasoningBank 算法的检索管线和 MaTTS 操作。
//
// ReasoningBank 通过向量搜索检索推理策略记忆，检索路径比 ReMe 更简单
// （无重排/改写步骤）。MaTTS（Memory-aware Test-Time Scaling）提供
// 并行/串行多轨迹缩放和自对比记忆提取能力。
//
// 文件目录：
//
//	rb/
//	├── doc.go           # 包文档
//	├── run.go           # RBRecallMemoryOp 检索操作
//	├── matts.go         # MaTTS 操作（ParallelScalingOp/SequentialScalingOp/BestOfNOp/SelfContrastMemoryOp）
//	└── prompt.go        # 检索和 MaTTS 相关提示词模板
//
// 对应 Python 代码：openjiuwen/extensions/context_evolver/retrieve/task/reasoning_bank/
package rb

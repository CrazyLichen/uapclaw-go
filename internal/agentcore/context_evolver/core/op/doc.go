// Package op 提供上下文记忆演化系统的可组合操作管线。
//
// BaseOp 是操作接口，SequentialOp 顺序组合（Then），ParallelOp 并行组合（With）。
// Python 用 >> 和 | 运算符重载，Go 用方法链。
//
// 文件目录：
//
//	op/
//	├── doc.go              # 包文档
//	├── base_op.go          # BaseOp 接口 + Seq/Par 工厂函数
//	├── sequential_op.go    # SequentialOp 顺序组合
//	└── parallel_op.go      # ParallelOp 并行组合
//
// 对应 Python 代码：openjiuwen/extensions/context_evolver/core/op/
package op

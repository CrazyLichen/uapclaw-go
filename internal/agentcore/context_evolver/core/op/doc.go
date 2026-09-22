// Package op 提供上下文记忆演化系统的可组合操作管线。
//
// BaseOp 是操作接口，SequentialOp 顺序组合（Then），ParallelOp 并行组合（With）。
// Python 用 >> 和 | 运算符重载，Go 用方法链。
//
// 对应 Python 代码：openjiuwen/extensions/context_evolver/core/op/
package op

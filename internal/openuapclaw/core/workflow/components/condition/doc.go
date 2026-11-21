// Package condition 提供分支组件的条件求值件：Condition 抽象 + 表达式条件。
//
// 本包承载工作流分支/循环组件的条件子域。Condition 基类嵌入 8.10 AtomicNode
// 的原子执行链（session 校验 → 回调 → CommitCmp），是 AtomicNode 落地后的
// 首个真实消费方（8.10 spec D1 回收点）。表达式条件（ExpressionCondition）
// 使用手写词法 + 递归下降求值器，对齐 Python expression.py 的 DSL 语义
// （Python ast.parse 只是上游实现载体，Go 复刻的是可观测语义，见 8.19 spec §3 拍板①）。
//
// number.py/array.py（NumberCondition/ArrayCondition）留 8.20 与 LoopComp 一起实施。
//
// 文件目录：
//
//	condition/
//	├── doc.go           # 包文档
//	├── condition.go     # Condition 接口 + BaseCondition + FuncCondition + AlwaysTrue
//	└── expression.go   # ExpressionCondition + convertCondition + parser + evaluator
//
// 对应 Python 代码：openjiuwen/core/workflow/components/condition/
package condition

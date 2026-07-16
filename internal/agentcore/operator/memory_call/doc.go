// Package memory_call 提供记忆参数句柄。
//
// MemoryCallOperator 管理记忆相关参数（enabled/max_retries），
// 不执行记忆操作。参数变更通过 onParameterUpdated 回调推送给消费者。
//
// 文件目录：
//
//	memory_call/        # 记忆调用操作器
//	├── doc.go                   # 子包文档
//	└── memory_call_operator.go  # MemoryCallOperator
//
// 对应 Python 代码：openjiuwen/core/operator/memory_call/
package memory_call

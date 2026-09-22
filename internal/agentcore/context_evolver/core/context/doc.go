// Package context 提供上下文记忆演化系统的操作执行上下文。
//
// RuntimeContext 作为操作间传递中间结果的通信总线，
// ServiceContext 管理共享服务（LLM/Embedding/VectorStore）的依赖注入。
//
// 文件目录：
//
//	context/
//	├── doc.go                # 包文档
//	├── runtime_context.go    # RuntimeContext 操作间上下文
//	└── service_context.go    # ServiceContext 共享服务上下文
//
// 对应 Python 代码：openjiuwen/extensions/context_evolver/core/context/
package context

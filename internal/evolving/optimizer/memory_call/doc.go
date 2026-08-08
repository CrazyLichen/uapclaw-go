// Package memory_call 提供记忆维度优化器基类。
//
// MemoryOptimizerBase 固定 domain="memory"，默认优化目标为 enabled 和 max_retries，
// 对齐 Python MemoryOptimizerBase 的声明式骨架。
// _backward() 是空实现（pass），_step() 返回空映射，为未来扩展预留。
//
// 文件目录：
//
//	memory_call/
//	├── doc.go           # 包文档
//	└── base.go          # MemoryOptimizerBase（记忆优化器基类） 结构体 + Domain/DefaultTargets/Backward/Step + Mixin 委托方法
//
// 对应 Python 代码：openjiuwen/agent_evolving/optimizer/memory_call/
package memory_call

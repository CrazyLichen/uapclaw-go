// Package iface 提供上下文引擎的核心抽象接口和数据结构。
//
// 本包将 ModelContext、ContextEngine、ContextProcessor 等核心接口
// 以及 ContextWindow、ContextStats、ContextEvent 等数据结构集中定义，
// 消除 context_engine ↔ processor 之间的循环依赖，
// 同时使 ProcessorFactory 和 RegisterProcessor 具备类型安全。
//
// 文件目录：
//
//	interface/
//	├── doc.go          # 包文档
//	├── types.go        # ModelContext/ContextEngine 接口 + ContextWindow/ContextStats + Option 类型 + ProcessorSpec
//	├── processor.go    # ContextProcessor/ProcessorConfig 接口 + ContextEvent/ProcessorOption + Option 类型
//	└── registry.go     # ProcessorFactory 工厂函数类型
//
// 对应 Python 代码：openjiuwen/core/context_engine/base.py + processor/base.py
package iface

// Package memory 提供记忆护栏 Rail 实现。
//
// 包含 CodingMemoryRail（编程记忆护栏，含自动召回 goroutine）、
// MemoryRail（通用记忆护栏）和 ExternalMemoryRail（外部记忆护栏，桥接 MemoryProvider）。
// 三者均嵌入 DeepAgentRail。CodingMemoryRail 和 MemoryRail 优先级 80，
// ExternalMemoryRail 优先级 75。在 Init 中注册工具，在 BeforeInvoke 中初始化管理器/Provider，
// 在 BeforeModelCall 中注入记忆 section 到系统提示词。
//
// 文件目录：
//
//	memory/
//	├── doc.go                    # 包文档
//	├── coding_memory_rail.go     # CodingMemoryRail 编程记忆护栏（自动召回+工具注册）
//	├── external_memory_rail.go   # ExternalMemoryRail 外部记忆护栏（Provider 桥接+prefetch+syncTurn+熔断器）
//	└── memory_rail.go            # MemoryRail 通用记忆护栏（工具注册+prompt 注入）
//
// 对应 Python 代码：openjiuwen/harness/rails/memory/
package memory

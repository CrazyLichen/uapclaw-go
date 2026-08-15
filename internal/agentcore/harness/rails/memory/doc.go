// Package memory 提供记忆护栏 Rail 实现。
//
// 包含 CodingMemoryRail（编程记忆护栏，含自动召回 goroutine）和 MemoryRail（通用记忆护栏）。
// 两者均嵌入 DeepAgentRail，优先级 80，在 Init 中注册工具，在 BeforeInvoke 中初始化管理器，
// 在 BeforeModelCall 中注入记忆 section 到系统提示词。
//
// 文件目录：
//
//	memory/
//	├── doc.go                    # 包文档
//	├── coding_memory_rail.go     # CodingMemoryRail 编程记忆护栏（自动召回+工具注册）
//	├── coding_memory_rail_test.go # 单元测试
//	├── memory_rail.go            # MemoryRail 通用记忆护栏（工具注册+prompt 注入）
//	└── memory_rail_test.go       # 单元测试
//
// 对应 Python 代码：openjiuwen/harness/rails/memory/
package memory

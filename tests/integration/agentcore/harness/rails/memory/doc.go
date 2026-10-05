//go:build integration

// Package memory 提供记忆护栏集成测试。
//
// 测试 CodingMemoryRail / MemoryRail 在 DeepAgent 上下文中的集成行为。
//
// 文件目录：
//
//	memory/
//	├── doc.go                          # 包文档
//	├── coding_memory_rail_test.go      # CodingMemoryRail 集成测试
//	├── coding_memory_rail_deep_test.go # CodingMemoryRail 深度 BeforeModelCall 测试
//	└── memory_rail_test.go             # MemoryRail 集成测试
//
// 对应 Python 代码：openjiuwen/harness/rails/memory/
package memory

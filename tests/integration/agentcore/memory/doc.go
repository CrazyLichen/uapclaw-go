//go:build integration

// Package memory_test 提供 agentcore/memory 模块的集成测试。
//
// 覆盖 CodingMemory（MockEmbedding + ToolContext + Settings）、
// LongTermMemory（零值创建 + RegisterStore + SetConfig 前置校验）、
// ExternalMemory（Rail Init/Uninit/Prefetch/熔断器）。
//
// 文件目录：
//
//	memory/
//	├── doc.go                          # 包文档
//	├── ltm_e2e_test.go                 # LongTermMemory E2E 集成测试（14 测试）
//	├── lite/
//	│   ├── doc.go                      # CodingMemory 集成测试包文档
//	│   └── coding_memory_test.go       # CodingMemory 集成测试（5 测试）
//	├── ltm/
//	│   ├── doc.go                      # LongTermMemory 集成测试包文档
//	│   └── ltm_test.go                 # LongTermMemory 集成测试（6 测试）
//	└── external/
//	    ├── doc.go                      # ExternalMemory 集成测试包文档
//	    └── external_memory_test.go     # ExternalMemoryRail 集成测试（5 测试）
//
// 对应 Python 代码：openjiuwen/core/memory/
package memory_test

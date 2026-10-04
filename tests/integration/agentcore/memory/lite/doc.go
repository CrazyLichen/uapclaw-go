//go:build integration

// Package lite 提供 agentcore/memory/lite 模块（CodingMemory）的集成测试。
//
// 测试覆盖：
//   - MemoryIndexManager 初始化、搜索、同步
//   - MockEmbeddingProvider 确定性向量
//   - CodingMemoryToolContext 工具创建
//   - MemorySettings 目录配置
//   - CodingMemory 写入/读取/冲突解决（WriteResult 结构验证 + 文件 I/O）
//
// 文件目录：
//
//	lite/
//	├── doc.go                        # 包文档
//	├── coding_memory_test.go         # CodingMemory 集成测试
//	└── coding_memory_conflict_test.go # CodingMemory 冲突解决集成测试
//
// 对应 Python 代码：openjiuwen/core/memory/lite/
package lite

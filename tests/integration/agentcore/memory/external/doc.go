//go:build integration

// Package external 提供 ExternalMemoryRail 集成测试。
//
// 测试覆盖：
//   - ExternalMemoryRail Init/Uninit 生命周期
//   - Prefetch 缓存机制
//   - SyncTurn 正常流程与熔断器
//   - fakeMemoryProvider Mock 实现
//
// 文件目录：
//
//	external/
//	├── doc.go                    # 包文档
//	└── external_memory_test.go   # ExternalMemoryRail 集成测试
//
// 对应 Python 代码：openjiuwen/core/memory/external/
package external

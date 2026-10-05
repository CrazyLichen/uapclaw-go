//go:build integration

// Package dreaming 提供 agentcore/memory/dreaming 模块的集成测试。
//
// 测试覆盖：
//   - DreamingOrchestrator 构造与默认值
//   - Start/Stop 生命周期（幂等性、状态一致性）
//   - IsRunning 状态查询
//   - DreamingConfig 间隔钳位
//   - OrchestratorHealth 健康检查
//   - WithBusyChecker/WithName 选项
//   - 接口满足性验证
//
// 文件目录：
//
//	dreaming/
//	├── doc.go                 # 包文档
//	└── orchestrator_test.go   # DreamingOrchestrator 集成测试
//
// 对应 Python 代码：openjiuwen/core/memory/dreaming/
package dreaming

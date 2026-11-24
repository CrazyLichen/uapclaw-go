//go:build integration

// Package agentobservability 提供 Agent 观测采集链集成测试的目录结构文档。
//
// 实际测试代码以 agentobservability_test 包（外部测试包）编写。
// 对照 Python: harness/observability/ 行为 + 15.24a W9 验证面——
// 真跑 DeepAgent 验证"埋点→provider→trajectory processor→Trajectory 重放"全链。
//
// 文件目录：
//
//	agentobservability/
//	├── doc.go            # 包文档
//	└── observability_test.go  # 采集链端到端三用例
package agentobservability

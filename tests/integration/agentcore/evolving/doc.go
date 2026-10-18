//go:build integration

// Package evolving_test 提供在线进化管道端到端集成测试。
//
// 对齐 Python: tests/system_tests/agent_evolving/test_online_evolution_e2e.py
// 测试核心管道：SignalDetector → SkillExperienceOptimizer → EvolutionStore
//
// 测试范围：
//   - 全管道信号→LLM→持久化端到端
//   - data-fetch 工具误报抑制
//   - 畸形 LLM 输出自动重试
//   - merge_target 替换已存在记录
//   - EvolutionStore 记录追加/合并/查询
//   - SignalDetector 多种信号类型检测
//
// 文件目录：
//
//	evolving/
//	├── doc.go                          # 包文档
//	└── online_evolution_e2e_test.go    # 在线进化管道 E2E 测试
//
// 对应 Python 代码：tests/system_tests/agent_evolving/test_online_evolution_e2e.py
package evolving_test

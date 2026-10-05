//go:build integration

// Package agentcore_test 提供 agentcore 模块的集成测试。
//
// 对齐 Python tests/system_tests/ 下的测试用例。
//
// 文件目录：
//
//	agentcore/
//	├── doc.go              # 包文档
//	├── controller/         # Controller 集成测试
//	├── context_engine/     # 上下文引擎集成测试
//	├── evolving/           # 进化训练集成测试
//	├── foundation/         # LLM 基础层集成测试
//	├── harness/            # Harness 集成测试
//	├── memory/             # Memory 集成测试
//	├── multi_agent/        # 多 Agent 集成测试
//	├── runner/             # Runner 集成测试
//	├── session/            # Session 集成测试
//	├── single_agent/       # 单 Agent 集成测试
//	└── workflow/           # Workflow 集成测试
package agentcore_test

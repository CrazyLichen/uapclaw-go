//go:build integration

// Package rails 提供 swarm/harness/common/rails 包下各 Rail 的集成测试。
//
// 本包重点测试各 Rail 在真实 ResourceMgr 和 Agent 上下文中的端到端行为，
// 覆盖构造、生命周期、工具注册/注销、中断决策和父类行为集成等场景。
//
// 文件目录：
//
//	rails/
//	├── doc.go                                # 包文档
//	├── project_memory_rail_test.go           # ProjectMemoryRail 集成测试
//	└── structured_ask_user_rail_test.go       # StructuredAskUserRail 集成测试
//
// 对应 Python 代码：tests/system_tests/test_project_memory.py, jiuwenswarm/agents/harness/common/rails/ask_user_rail.py
package rails

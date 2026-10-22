//go:build integration

// Package single_agent 提供 agentcore 单 Agent 层的集成测试。
//
// 覆盖 single_agent 及其依赖子系统的跨包链路：
// 技能生命周期/管理器 E2E（skills/）、模型备份护栏 failover 全链路
// （model_backup_rail_test.go，跨 agents + rail + interfaces +
// foundation/llm + model_clients 子系统）、failover 后完整 ReAct
// 工具循环（model_backup_react_loop_test.go，额外跨 foundation/tool +
// ability + common/schema 子系统，验证切换模型后工具分发/执行/结果回填）。
//
// 文件目录：
//
//	single_agent/
//	├── doc.go                         # 包文档
//	├── model_backup_rail_test.go      # ModelBackupRail failover 集成测试（对齐 Python test_model_backup_rail.py）
//	├── model_backup_react_loop_test.go # failover 后 ReAct 工具循环集成测试（对齐 Python 主用例工具链路）
//	└── skills/
//	    ├── doc.go                    # 技能测试包文档
//	    ├── skill_lifecycle_test.go   # SkillUtil 生命周期
//	    └── skill_manager_e2e_test.go # SkillManager E2E
//
// 对应 Python 代码：tests/system_tests/rail/test_model_backup_rail.py
//
//	tests/system_tests/single_agent/skills/
package single_agent

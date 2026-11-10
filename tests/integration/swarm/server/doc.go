//go:build integration

// Package server_test 提供 swarm/server 模块的集成测试。
//
// 覆盖 AgentServer 适配层与 SkillManager 两条链路。SkillManager 部分以真实临时目录
// 驱动「导入 → 落盘 → state/agent-data 索引 → 卸载」全链，并对齐 Python
// tests/unit_tests/agentserver 下 7 个单测的行为规格。
//
// 文件目录：
//
//	server/
//	├── doc.go                      # 包文档
//	├── goal_dispatch_test.go       # command.goal 分发契约（不写历史/不调模型/未知 action）
//	├── history_roundtrip_test.go   # 会话历史往返集成测试
//	├── skill_manager_test.go       # SkillManager 集成测试（路径穿越/导入卸载/marketplace/团队共享/启停可见性）
//	├── skilldev_pipeline_test.go   # SkillDev 流水线编排测试（挂起/恢复/检查点）
//	├── skilldev_stage_agent_test.go # SkillDev 阶段 Agent 装配与真实工具落盘测试
//	└── adapter/                    # AgentServer 适配层集成测试
package server_test

//go:build integration

// Package evolution 提供 Evolution Rails 集成测试。
//
// 覆盖以下模块：
//   - EvolutionRail 基类：轨迹收集生命周期、Builder 复用、GetCallbacks、DrainPendingHostEvents
//   - TrajectoryRail：纯轨迹收集，Priority=10
//   - TeamSkillCreateRail：构造选项、Priority=85、spawn_member 阈值检测
//   - EvolutionApprovalRuntime：LookupPendingApprovalSnapshot、ApprovePendingRequest、FinalizeStagedEvolutionRequest
//   - Contracts：EvolutionSnapshot、EvolutionHostEventMeta、EvolutionRequestResult 序列化/转换
//
// 对齐 Python: tests/unit_tests/harness/rails/evolution/test_evolution_rail.py,
// tests/unit_tests/harness/test_team_skill_create_rail.py,
// tests/unit_tests/harness/rails/evolution/test_evolution_approval_runtime.py
//
// 文件目录：
//
//	evolution/
//	├── doc.go                            # 包文档
//	├── evolution_rail_test.go            # EvolutionRail + TrajectoryRail 轨迹收集测试
//	├── team_skill_create_rail_test.go    # TeamSkillCreateRail 团队技能创建测试
//	└── approval_contracts_test.go        # ApprovalRuntime + Contracts 审批/契约测试
package evolution

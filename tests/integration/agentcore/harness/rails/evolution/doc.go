//go:build integration

// Package evolution 提供 Evolution Rails 集成测试。
//
// 覆盖以下模块：
//   - EvolutionRail 基类：轨迹收集生命周期、Builder 复用、GetCallbacks、DrainPendingHostEvents
//   - TrajectoryRail：纯轨迹收集，Priority=10
//   - SkillEvolutionRail：构造选项、Priority=80、AutoScan/AutoSave、LLM 策略、信号去重、共享配置
//   - SkillEvolutionRail 正例：RunEvolution 自动保存落盘、审批事件与批准/拒绝、写盘失败保留待审并可重试
//   - SkillEvolutionRail 触发链：真实 DeepAgent 一轮会话 → AfterInvoke → 异步演化 → 落盘
//   - TeamSkillEvolutionRail：构造选项、Priority=80、autoScan/autoSave(default=false)、AllowEvolutionTrigger、NotifyTeamCompleted、OnAfterToolCall view_task 检测
//   - TeamSkillCreateRail：构造选项、Priority=85、spawn_member 阈值检测
//   - ContextEvolutionRail：记忆注入/恢复、缓存、Priority=50
//   - EvolutionApprovalRuntime：LookupPendingApprovalSnapshot、ApprovePendingRequest 与 FinalizeStagedEvolutionRequest
//   - Contracts：EvolutionSnapshot、EvolutionHostEventMeta、EvolutionRequestResult 序列化/转换
//   - ApprovalEvents：BuildSkillApprovalEvent、BuildSimplifyApprovalEvent 与 BuildEvolutionProgressEvent
//
// 对齐 Python: tests/unit_tests/harness/rails/evolution/test_evolution_rail.py,
// tests/unit_tests/harness/test_team_skill_create_rail.py,
// tests/unit_tests/harness/rails/evolution/test_evolution_approval_runtime.py,
// tests/unit_tests/harness/rails/evolution/test_skill_evolution_rail.py
//
// 文件目录：
//
//	evolution/
//	├── doc.go                              # 包文档
//	├── evolution_rail_test.go              # EvolutionRail + TrajectoryRail 轨迹收集测试
//	├── skill_evolution_rail_test.go        # SkillEvolutionRail 技能演进护栏测试
//	├── skill_evolution_positive_test.go    # SkillEvolutionRail.RunEvolution 正例（落盘/审批/重试）
//	├── skill_evolution_trigger_test.go     # AfterInvoke 触发异步演化并落盘
//	├── team_skill_evolution_rail_test.go   # TeamSkillEvolutionRail 团队技能演进护栏测试
//	├── team_skill_create_rail_test.go      # TeamSkillCreateRail 团队技能创建测试
//	├── team_skill_rails_system_test.go     # 团队技能 rails 系统级测试
//	├── context_evolution_rail_test.go      # ContextEvolutionRail 记忆注入测试
//	├── approval_contracts_test.go          # ApprovalRuntime + Contracts 审批/契约测试
//	└── approval_events_test.go             # 审批事件构建测试
package evolution

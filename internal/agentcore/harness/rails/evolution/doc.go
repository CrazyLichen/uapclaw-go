// Package evolution 提供技能演化轨道（Evolution Rail）的共享契约类型、
// 审批事件构建函数、审批运行时以及 EvolutionRail 基类和 TrajectoryRail。
//
// 本包实现了 9.24 的 P1 契约层、P2（EvolutionRail 基类 + TrajectoryRail）
// 和 P3（SkillEvolutionRail 单 Agent 技能演进护栏），
// 为 P4（TeamSkillEvolutionRail）提供基础类型和基类。
//
// 核心功能：
//   - 契约类型：EvolutionHostEventMeta / EvolutionSnapshot / EvolutionRequestResult / SimplifyRequestResult
//   - 审批接口：ApprovalManager 窄接口（ExperienceManager 隐式满足）
//   - 审批事件：BuildSkillApprovalEvent / BuildSimplifyApprovalEvent / BuildTeamSkillApprovalEventFromRecords
//   - 审批运行时：EvolutionApprovalRuntime（查找/批准/拒绝/路由）
//   - EvolutionRail 基类：嵌入 DeepAgentRail，4 个 final 回调自动完成轨迹收集，10 个扩展点通过 EvolutionExtension 接口实现多态分派
//   - TrajectoryRail：纯轨迹收集轨道，使用 noOpExtension，不触发任何演化逻辑
//   - SkillEvolutionRail：单 Agent 技能演进护栏（Priority=80），信号检测→技能归属→经验生成→评分→暂存审批→应用更新
//   - 辅助函数：消息/工具/角色规范化、token 字段分离等
//
// 文件目录：
//
//	evolution/
//	├── doc.go                  # 包文档
//	├── contracts.go            # 契约类型 + ApprovalManager 接口
//	├── approval_events.go      # 审批事件构建函数（7 导出 + 1 非导出）
//	├── approval_runtime.go     # EvolutionApprovalRuntime 结构体 + 4 方法
//	├── extension.go            # EvolutionExtension 接口（10 方法）+ noOpExtension 默认实现 + EvolutionTriggerPoint 枚举
//	├── evolution_rail.go       # EvolutionRail 基类 + 构造选项 + 回调注册 + 轨迹收集 + 异步演化
//	├── trajectory_rail.go      # TrajectoryRail 纯轨迹收集轨道（Priority=10）
//	├── skill_evolution_rail.go # SkillEvolutionRail 单 Agent 技能演进护栏（Priority=80）+ Sharing 集成
//	└── helpers.go              # 辅助函数：splitResponseTokenFields / normalizeSkillNames / normalizeMemberRole 等
//
// 对应 Python 代码：openjiuwen/harness/rails/evolution/
package evolution

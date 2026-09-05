// Package evolution 提供技能演化轨道（Evolution Rail）的共享契约类型、
// 审批事件构建函数和审批运行时。
//
// 本包是 9.24 EvolutionRail 实现的 P1 契约层，为 P2（EvolutionRail 基类 + TrajectoryRail）、
// P3（SkillEvolutionRail）、P4（TeamSkillEvolutionRail）提供基础类型依赖。
//
// 核心功能：
//   - 契约类型：EvolutionHostEventMeta / EvolutionSnapshot / EvolutionRequestResult / SimplifyRequestResult
//   - 审批接口：ApprovalManager 窄接口（ExperienceManager 隐式满足）
//   - 审批事件：BuildSkillApprovalEvent / BuildSimplifyApprovalEvent / BuildTeamSkillApprovalEventFromRecords
//   - 审批运行时：EvolutionApprovalRuntime（查找/批准/拒绝/路由）
//
// 文件目录：
//
//	evolution/
//	├── doc.go                # 包文档
//	├── contracts.go          # 契约类型 + ApprovalManager 接口
//	├── approval_events.go    # 审批事件构建函数（7 导出 + 1 非导出）
//	└── approval_runtime.go   # EvolutionApprovalRuntime 结构体 + 4 方法
//
// 对应 Python 代码：openjiuwen/harness/rails/evolution/
package evolution

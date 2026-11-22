// Package rails 提供团队成员 harness 的成员侧 rail 基座。
//
// 对齐 Python: jiuwenswarm/agents/harness/team/rails/（15.20 成员 rail 基座缺口批）。
// 本包 rail 挂载在团队成员（含 leader）的 DeepAgent 上，按成员身份注入
// prompt 节、拦截工具调用或触发技能库重载。
//
// 跨域缝（15.20 批一裁决）：
//   - OrganizationModelRail / OrganizationAvailabilityRail 依赖的
//     server/runtime/organization_* 663 行（25.9 专家与组织 ☐）以
//     OrganizationModelBackend / OrganizationAvailabilityBackend 窄接口注入；
//     后端缺省为 nil，rail 按成员既有语义降级运行。
//
// 文件目录：
//
//	rails/
//	├── doc.go                                # 包文档
//	├── team_skill_storage_policy_rail.go     # 团队技能存储策略 prompt 注入
//	├── team_skill_library_reload_rail.go    # 全局技能库写入后重载成员技能视图
//	├── organization_model_rail.go           # 组织持久团队模型应用（后端注入缝）
//	├── organization_availability_rail.go    # 组织 AgentGroup 可用性快照（后端注入缝）
//	└── team_permission_policy_rail.go      # 团队权限规则 prompt 注入
//
// 对应 Python 代码：jiuwenswarm/agents/harness/team/rails/
package rails

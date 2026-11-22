// Package swarmproviderrails_test 覆盖 15.20 批一成员 rail 基座集成矩阵：
// 工厂闭合逐 rail 一用例 + 桩语义断言。
//
// 跨子系统：swarm/agents/swarm（enrich+RegisterSwarmProviders）→
// agentcore/harness（ResolveDeepAgentSpecParts+provider 注册表+物化）→
// agent_teams/schema（TeamAgentSpec/BuildContext）→
// swarm/agents/harness/team/rails（15.20 新基座）→
// swarm/agents/swarm/providers（工厂层）。
//
// 文件目录：
//
//	swarmproviderrails/
//	├── doc.go                        # 包文档
//	└── provider_rails_matrix_test.go # ProviderRailsSuite：五用例矩阵
//
// 对应 Python 代码：jiuwenswarm/agents/swarm/providers/（消费链）
package swarmproviderrails

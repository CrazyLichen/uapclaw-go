// Package swarmproviders_test 覆盖 15.16f swarm providers 集成矩阵：
// 真实团队链路逐成员按 config.yaml 加载/不加载对应 tool/rail/subagent。
//
// 跨子系统：swarm/agents/swarm（enrich+RegisterSwarmProviders）→
// agentcore/harness（ResolveDeepAgentSpecParts+provider 注册表+物化）→
// agent_teams/harness（NativeHarness 前向构造）→ agent_teams/schema（TeamAgentSpec
// 种子往返）→ agentcore/harness/schema（三张 provider 注册表）。
//
// 文件目录：
//
//	swarmproviders/
//	├── doc.go                   # 包文档
//	└── provider_matrix_test.go # ProviderMatrixSuite：七用例加载矩阵
//
// 对应 Python 代码：jiuwenswarm/agents/swarm/providers/（消费链）
package swarmproviders

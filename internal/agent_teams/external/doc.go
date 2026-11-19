// Package external 提供外部 CLI 成员的后端注册表与 AgentTemplate runtime 翻译。
//
// 文件目录：
//
//	external/
//	├── doc.go               # 包文档
//	├── backends.go         # 外部 CLI 后端注册表（cli_agent/backends.py 190 行 1:1；归属修正见文件头）
//	└── manifest.go         # AgentTemplate runtime → ExternalCliAgentSpec（manifest.py 104 行 1:1）
//
// 归属说明：backends.go 此前在 runtime/externalcli（聚合包）；Python 源文件在
// agent_teams/external/cli_agent 层，tools 包消费 IsKnownBackend/AvailableBackends
// 时不得依赖 runtime 层，按规则 9 抽回（15.16e-T5）；adapter 半区仍在
// runtime/externalcli，经缝注入（D-15.16-e14）。
//
// 对应 Python 代码：openjiuwen/agent_teams/external/{cli_agent/backends.py,manifest.py}
package external

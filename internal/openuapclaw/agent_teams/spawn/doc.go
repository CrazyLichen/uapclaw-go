// Package spawn 提供进程内生成（inprocess spawn）和进程级共享资源。
// 本包是团队 Agent 生成机制的独立子包，与 agentcore/runner/spawn/（通用子进程基础设施）
// 形成分层：agentcore/runner/spawn/ 不感知 TeamAgent，本包知道 TeamAgent 概念。
// 循环依赖处理：本包不 import agent/ 包（SpawnableAgent 最小接口，含 ForkDeep() 返回 *harness.DeepAgent 具体类型——harness 不反向 import 本包，方向合法，见 fork.go 铁律注释）；
// 和 AgentFactory 工厂函数解耦。
// 文件目录：
//
//	spawn/
//	├── doc.go              # 包文档
//	├── handle.go           # SpawnHandle 统一接口
//	├── inprocess_handle.go # InProcessSpawnHandle 进程内句柄
//	├── inprocess_spawn.go  # InProcessSpawn 函数 + SpawnableAgent 接口
//	├── external_cli_spawn.go # 外部 CLI 成员 spawn 编排主体（15.16b，D-15.16-b1 注入缝）
//	├── external_cli_glue.go   # 上述编排的 Go 侧胶合类型（卡片视图 / 句柄构造）
//	└── shared_resources.go # 进程级全局单例
//
// 对应 Python 代码：openjiuwen/agent_teams/spawn/
package spawn

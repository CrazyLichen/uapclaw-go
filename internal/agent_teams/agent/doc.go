// Package agent 提供生产级团队 Agent（TeamAgent）实现。
//
// TeamAgent 是整个多 Agent 协作系统的核心编排节点，既可充当 Leader
// （分发任务、协调成员），也可充当 Teammate（执行具体任务）。
// 采用组合式架构：内部包裹 DeepAgent 实例（而非继承），
// 委托给专职 Manager 管理配置/生成/恢复/会话/流式/协调。
//
// 四象限分解：
//   - 静态蓝图（TeamAgentBlueprint）：构造时确定、不可变
//   - 可变状态（TeamAgentState）：运行时可变值，跨 Manager 共享
//   - 进程级基础设施（TeamInfra）：每进程一份
//   - 实例级资源（PrivateAgentResources）：每实例一份
//
// 文件目录：
//
//	agent/
//	├── doc.go              # 包文档
//	├── team_agent.go       # TeamAgent 主体（9.55）
//	├── state.go            # TeamAgentState 可变状态（9.55）
//	├── member.go           # TeamMember 成员状态管理（9.55）
//	├── member_factory.go   # create_member_handle 工厂（9.55）
//	├── blueprint.go        # TeamAgentBlueprint 不可变蓝图（9.55）
//	├── infra.go            # TeamInfra 进程级基础设施（9.55）
//	├── resources.go        # PrivateAgentResources 实例级资源（9.55）
//	├── agent_configurator.go # ⤵️ 回填: 9.57 AgentConfigurator
//	├── spawn_manager.go      # ⤵️ 回填: 9.58 SpawnManager
//	├── session_manager.go    # ⤵️ 回填: 9.59 SessionManager
//	├── stream_controller.go  # ⤵️ 回填: 9.60 StreamController
//	├── recovery_manager.go   # ⤵️ 回填: 9.61 RecoveryManager
//	└── coordination/         # ⤵️ 回填: 9.62-9.63 协调子系统
//
// 对应 Python 代码：openjiuwen/agent_teams/agent/
package agent

// Package agent 提供团队成员 Agent 的核心类型。
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
//	├── doc.go                # 包文档
//	├── blueprint.go          # TeamAgentBlueprint 不可变蓝图
//	├── team_agent.go         # TeamAgent 主体
//	├── state.go              # TeamAgentState 可变状态
//	├── member.go             # TeamMember 成员状态管理
//	├── member_factory.go     # CreateMemberHandle 工厂
//	├── infra.go              # TeamInfra 进程级基础设施
//	├── resources.go          # PrivateAgentResources 实例级资源
//	├── agent_configurator.go # AgentConfigurator Agent 配置器（9.57）
//	├── payload.go            # SpawnPayloadBuilder 跨进程载荷构造器（9.57）
//	├── spawn_manager.go      # SpawnManager 子进程管理（9.58）
//	├── session_manager.go    # SessionManager 会话三态管理（9.59）
//	├── stream_controller.go  # StreamController 流式控制器（9.60）
//	└── recovery_manager.go   # TODO(#9.61) 恢复管理器
//
// 对应 Python 代码：openjiuwen/agent_teams/agent/
package agent

// Package multi_agent 提供多 Agent 团队的核心抽象与运行时基础设施。
//
// 定义 BaseTeam 接口（类型别名指向 schema 包）作为多 Agent 团队体系的根基契约，
// 具体团队实现（HandoffTeam、HierarchicalTeam）均实现此接口。
// 团队内部的 Agent 通信通过 TeamRuntime（8.30）的 P2P 和 Pub-Sub 消息机制完成。
// BaseTeam.Card() 返回 schema.TeamCardInterface，支持 TeamCard 和 EventDrivenTeamCard 多态访问。
//
// 文件目录：
//
//	multi_agent/
//	├── doc.go              # 包文档
//	├── config.go           # TeamConfig 类型别名 + NewTeamConfig 转发
//	├── team.go             # BaseTeam/AgentTeamProvider/TeamAgentProvider 类型别名
//	├── team_option.go      # TeamOption/TeamOptions 类型别名 + WithXxx/NewTeamOptions 转发
//	├── schema/
//	│   ├── doc.go              # schema 子包文档
//	│   ├── team_card.go        # TeamCardInterface + TeamCard + EventDrivenTeamCard + 构造函数 + 选项 + String
//	│   └── team_interface.go   # BaseTeam + AgentTeamProvider + TeamAgentProvider
//	│                          # + TeamOption/TeamOptions/TeamConfig + 构造函数 + With* + 链式配置方法
//	├── team_runtime/
//	│   ├── doc.go                  # team_runtime 子包文档
//	│   ├── team_runtime.go         # TeamRuntime 核心结构体 + P2P/PubSub 通信
//	│   ├── message_bus.go          # MessageBus 消息总线
//	│   ├── message_router.go       # MessageRouter 消息路由
//	│   ├── envelope.go             # Envelope 消息信封
//	│   ├── subscription_manager.go # SubscriptionManager 订阅管理
//	│   ├── runtime_bindable.go     # RuntimeBindable 运行时绑定接口
//	│   ├── communicable_agent.go   # CommunicableAgent 可通信 Agent
//	│   └── communicable_interface.go # Communicable 接口定义
//	└── teams/
//	    ├── doc.go              # teams 子包文档
//	    ├── utils.go            # 独立调用/流式上下文工具函数
//	    ├── handoff/            # HandoffTeam（单活跃 Agent 交接模式）
//	    │   ├── doc.go                  # handoff 子包文档
//	    │   ├── handoff_team.go        # HandoffTeam 实现
//	    │   ├── handoff_config.go      # HandoffTeamConfig 配置
//	    │   ├── handoff_orchestrator.go # HandoffOrchestrator 编排器
//	    │   ├── handoff_request.go     # HandoffRequest 请求
//	    │   ├── handoff_signal.go      # HandoffSignal 信号
//	    │   ├── handoff_tool.go        # HandoffTool 工具
//	    │   ├── container_agent.go     # ContainerAgent 容器 Agent
//	    │   └── interrupt.go           # 中断处理
//	    ├── hierarchical_msgbus/  # HierarchicalTeam（消息总线模式）
//	    │   ├── doc.go                  # 子包文档
//	    │   ├── hierarchical_team.go   # HierarchicalTeam 实现
//	    │   ├── hierarchical_config.go # HierarchicalTeamConfig 配置
//	    │   ├── supervisor_agent.go    # SupervisorAgent 监督 Agent
//	    │   └── p2p_ability_manager.go # P2PAbilityManager 能力管理
//	    └── hierarchical_tools/  # HierarchicalTeam（工具模式）
//	        ├── doc.go                  # 子包文档
//	        ├── hierarchical_team.go   # HierarchicalTeam 实现
//	        └── hierarchical_config.go # HierarchicalTeamConfig 配置
//
// 对应 Python 代码：openjiuwen/core/multi_agent/
package multi_agent

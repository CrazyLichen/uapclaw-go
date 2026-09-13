// Package schema 提供团队相关的数据结构定义。
//
// 包含团队角色、状态枚举、规格定义、运行时上下文、配置结构体、i18n 等。
// blueprint.go 是核心文件，定义 TeamAgentSpec（构造 TeamAgent 的完整 JSON 可序列化规格）、
// LeaderSpec、TransportSpec/StorageSpec 及其注册表、以及全部校验方法。
// deep_agent_spec.go 定义 DeepAgentSpec、SubAgentSpec 等单角色规格类型（TeamModelConfig 已迁移至 models 包）。
// 配置结构体（MessagerTransportConfig、TeamMemoryConfig）和常量/i18n 从其他包搬入，
// 打断 schema→messager/memory/agent_teams 循环依赖。
// 事件类型（BaseEventMessage、TypedEvent、TeamTopic、30+ 事件结构体及 TeamEvent 常量）
// 已提取到 schema/events 子包，打断 schema←→team_workspace 循环依赖。
//
// 文件目录：
//
//	schema/             # Schema 类型定义
//	├── doc.go               # 包文档
//	├── team.go              # TeamRole/TeamSpec/TeamRuntimeContext 等团队级类型
//	├── deep_agent_spec.go   # DeepAgentSpec/SubAgentSpec 等单角色规格定义（TeamModelConfig 已迁移至 models 包）
//	├── blueprint.go         # TeamAgentSpec/LeaderSpec/TransportSpec 等团队规格与校验
//	├── status.go            # 成员/任务状态枚举与状态转换表
//	├── stream.go            # TeamOutputSchema 团队流式输出 Schema
//	├── task.go              # 任务视图响应类型（TaskOpResult/TaskDetail/NewTaskSpec 等）
//	├── transport_config.go  # MessagerTransportConfig/MessagerPeerConfig 消息通信配置
//	├── memory_config.go     # TeamMemoryConfig 团队记忆配置
//	├── session_context.go   # SessionState/GetSessionID 委托（定义已迁移至 sessionctx 包）
//	├── constants.go         # 保留成员名常量（HumanAgentMemberName 等）
//	├── i18n.go              # 国际化字符串字典与 T() 函数
//	└── events/              # 事件类型子包（BaseEventMessage/TypedEvent/TeamTopic/30+ 事件结构体/TeamEvent 常量）
//	    ├── doc.go               # 子包文档
//	    ├── base.go              # BaseEventMessage + TypedEvent + EventMessage
//	    ├── topic.go             # TeamTopic 类型 + Build 方法
//	    ├── constants.go         # TeamEvent* 字符串常量
//	    ├── team_events.go       # 团队生命周期事件
//	    ├── member_events.go     # 成员生命周期事件
//	    ├── task_events.go       # 任务事件
//	    ├── message_events.go    # 消息事件
//	    ├── workspace_events.go  # 工作空间事件
//	    ├── worktree_events.go   # Worktree 事件
//	    └── approval_events.go   # 审批事件
//
// 对应 Python 代码：openjiuwen/agent_teams/schema/
package schema

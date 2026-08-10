// Package schema 提供团队相关的数据结构定义。
//
// 包含团队角色、状态枚举、规格定义、运行时上下文、事件类型、配置结构体、i18n 等。
// blueprint.go 是核心文件，定义 TeamAgentSpec（构造 TeamAgent 的完整 JSON 可序列化规格）、
// LeaderSpec、TransportSpec/StorageSpec 及其注册表、以及全部校验方法。
// deep_agent_spec.go 定义 DeepAgentSpec、SubAgentSpec 等单角色规格类型（TeamModelConfig 已迁移至 models 包）。
// 配置结构体（MessagerTransportConfig、TeamMemoryConfig）和常量/i18n 从其他包搬入，
// 打断 schema→messager/memory/agent_teams 循环依赖。
//
// 文件目录：
//
//	schema/             # Schema 类型定义
//	├── doc.go               # 包文档
//	├── team.go              # TeamRole/TeamSpec/TeamRuntimeContext 等团队级类型
//	├── deep_agent_spec.go   # DeepAgentSpec/SubAgentSpec 等单角色规格定义（TeamModelConfig 已迁移至 models 包）
//	├── blueprint.go         # TeamAgentSpec/LeaderSpec/TransportSpec 等团队规格与校验
//	├── status.go            # 成员/任务状态枚举与状态转换表
//	├── events.go            # 事件类型与事件消息 Schema
//	├── stream.go            # TeamOutputSchema 团队流式输出 Schema
//	├── task.go              # 任务视图响应类型（TaskOpResult/TaskDetail/NewTaskSpec 等）
//	├── transport_config.go  # MessagerTransportConfig/MessagerPeerConfig 消息通信配置
//	├── memory_config.go     # TeamMemoryConfig 团队记忆配置
//	├── session_context.go   # SessionState/GetSessionID 会话上下文管理
//	├── constants.go         # 保留成员名常量（HumanAgentMemberName 等）
//	└── i18n.go              # 国际化字符串字典与 T() 函数
//
// 对应 Python 代码：openjiuwen/agent_teams/schema/
package schema

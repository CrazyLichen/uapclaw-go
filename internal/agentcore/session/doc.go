// Package session 提供会话管理的抽象接口、代理实现和 Agent/Workflow/Node 公开会话。
//
// 本包通过类型别名引用 interfaces.BaseSession 作为所有会话类型的统一抽象。ProxySession 实现代理模式。
// Session 是 Agent 场景下的公开会话，组合内部层 AgentSession，提供 PreRun/PostRun
// 生命周期、状态读写、流写入等用户面向 API。
// WorkflowSession 是工作流场景下的公开会话，组合内部层 WorkflowSession，提供
// 环境变量管理、工作流卡片等业务功能。
// NodeSessionFacade 是工作流组件场景下的公开会话，包装内部层 NodeSession，提供
// 身份查询、状态读写、追踪、交互、流写入、环境变量等组件开发者面向 API。
//
// 本包依赖 state 子包提供的双层状态接口（StateLike/CommitStateLike 底层 + SessionState 上层）。
// Config 已于 5.12 回填为 interfaces.SessionConfig，ActorManager 暂用 any 占位待后续回填。
// Tracer 已于 5.11 回填为 *tracer.Tracer，AgentSpan 已于 5.11 回填为 *tracer.TraceAgentSpan。
// StreamWriterManager 已于 5.10 回填为 *stream.StreamWriterManager。
//
// 文件目录：
//
//	session/
//	├── doc.go              # 包文档
//	├── session.go          # BaseSession 类型别名 + ProxySession 实现
//	├── interfaces/         # 统一接口定义
//	│   ├── doc.go                           # interfaces 包文档
//	│   └── interfaces.go                    # BaseSession/Checkpointer/Storage/*Provider 接口
//	├── agent.go            # Session 公开会话（Agent 场景）+ CreateAgentSession
//	├── workflow.go         # WorkflowSession 公开会话（Workflow 场景）
//	├── node.go             # NodeSessionFacade 公开会话（工作流组件场景）
//	├── wrapper.go          # RouterSessionFacade 路由会话门面（禁写壳）
//	├── constants/          # 会话常量
//	│   ├── doc.go                           # constants 包文档
//	│   └── constants.go                     # 配置键名/环境变量键名/默认值/映射表
//	├── config/             # 会话配置
//	│   ├── doc.go                           # config 包文档
//	│   ├── config.go                        # MetadataLike/BuiltinConfigLoader/defaultSessionConfig
//	│   ├── env_loader.go                    # trySetEnv/loadEnvConfigs 环境加载
//	│   └── context.go                       # WithEnvs context 注入
//	├── interaction/        # 交互管理
//	│   ├── doc.go                           # interaction 包文档
//	│   ├── base.go                          # ExecutableIDProvider 类型别名 + BaseInteraction + GraphInterrupt/Interrupt + AgentInterrupt + 常量
//	│   ├── interaction.go                   # WorkflowInteraction + SimpleAgentInteraction + AgentInteraction + InteractionOutput
//	│   └── interactive_input.go             # InteractiveInput 用户输入容器
//	├── state/              # 状态接口与内存实现
//	│   ├── doc.go                           # state 包文档
//	│   ├── state.go                         # 双层接口 + 常量 + 兼容别名
//	│   ├── key.go                           # StateKey 类型
//	│   ├── agent_state_collection.go        # Agent 状态集合
//	│   ├── workflow_state_collection.go     # Workflow 四区状态集合
//	│   ├── workflow_commit_state.go         # Workflow 可提交状态
//	│   ├── workflow_inmemory_state.go       # InMemoryWorkflowState 构造器
//	│   ├── inmemory_state.go                # InMemoryStateLike
//	│   ├── inmemory_commit_state.go         # InMemoryCommitState
//	│   └── utils.go                         # 工具函数
//	├── tracer/             # 会话追踪
//	│   ├── doc.go                           # tracer 包文档
//	│   ├── data.go                          # InvokeType/NodeStatus/TraceEvent 枚举
//	│   ├── span.go                          # Span/TraceAgentSpan/TraceWorkflowSpan/SpanManager
//	│   ├── tracer.go                        # Tracer 核心 + TriggerParams
//	│   ├── handler.go                       # TraceAgentHandler/TraceWorkflowHandler
//	│   ├── decorator.go                     # TracedModelClient/TracedTool/TracedWorkflow
//	│   └── workflow.go                      # TracerWorkflowUtils + BaseWorkflowSession
//	└── internal/           # 内部会话实现
//	    ├── doc.go                # internal 包文档
//	    ├── agent_session.go      # AgentSession
//	    └── workflow_session.go   # WorkflowSession/NodeSession/SubWorkflowSession
//
// 对应 Python 代码：openjiuwen/core/session/agent.py + openjiuwen/core/session/session.py + openjiuwen/core/session/workflow.py + openjiuwen/core/session/node.py + openjiuwen/core/session/internal/wrapper.py
//
// 核心类型/接口索引：
//
//	BaseSession          — 会话基类接口，所有会话类型的核心抽象
//	ProxySession         — 代理会话，将调用委托给内部 stub
//	Session              — Agent 公开会话，用户面向 API
//	WorkflowSession      — Workflow 公开会话，用户面向 API
//	NodeSessionFacade    — 工作流节点会话门面，组件开发者面向 API
//	RouterSessionFacade  — 路由会话门面，禁写壳（路由函数场景）
//	SessionConfig        — 会话配置接口，环境变量/工作流配置/Agent配置的统一抽象
//	WorkflowConfigProvider — 工作流配置提供者接口（占位，⤵️ 8.15 回填）
//	AgentConfigProvider  — Agent 配置提供者接口（占位，⤵️ 6.3 回填）
//	WorkflowInteraction   — 工作流交互，通过 GraphInterrupt 暂停图执行
//	SimpleAgentInteraction — 简单 Agent 交互，无输入队列
//	AgentInteraction      — 完整 Agent 交互，含输入队列 + 检查点 + 流输出
//	InteractiveInput      — 用户交互输入容器
//	GraphInterrupt        — 图级中断异常
//	AgentInterrupt        — Agent 中断异常
package session

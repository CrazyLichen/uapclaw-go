// Package session 提供会话管理的抽象接口、代理实现和 Agent/Workflow/Node 公开会话。
//
// 本包通过类型别名引用 interfaces.InnerSession 作为所有会话类型的统一抽象。ProxySession 实现代理模式。
// Session 是 Agent 场景下的公开会话，组合内部层 AgentSession，提供 PreRun/PostRun
// 生命周期、状态读写、流写入等用户面向 API。
// WorkflowSession 是工作流场景下的公开会话，组合内部层 WorkflowSession，提供
// 环境变量管理、工作流卡片等业务功能。
// NodeSessionFacade 是工作流组件场景下的公开会话，包装内部层 NodeSession，提供
// 身份查询、状态读写、追踪、交互、流写入、环境变量等组件开发者面向 API。
//
// 本包依赖 state 子包提供的双层状态接口（StateLike/CommitStateLike 底层 + SessionState 上层）。
// Config 已于 5.12 回填为 config.SessionConfig，ActorManager 暂用 any 占位待后续回填。
// Tracer 已于 5.11 回填为 *tracer.Tracer，AgentSpan 已于 5.11 回填为 *tracer.TraceAgentSpan。
// StreamWriterManager 已于 5.10 回填为 *stream.StreamWriterManager。
//
// 文件目录：
//
//	session/
//	├── doc.go              # 包文档
//	├── session.go          # InnerSession（会话基类）/SessionFacade（门面会话） 类型别名 + ProxySession（代理会话） 实现
//	├── agent.go            # Session 公开会话（Agent 场景）+ CreateAgentSession
//	├── agent_team.go       # AgentTeamSession 公开会话（Agent 团队场景）+ CreateAgentTeamSession
//	├── workflow.go         # WorkflowSession 公开会话（Workflow 场景）
//	├── node.go             # NodeSessionFacade 公开会话（工作流组件场景）
//	├── wrapper.go          # RouterSessionFacade 路由会话门面（禁写壳）
//	├── checkpointer/       # 检查点持久化
//	│   ├── doc.go                           # checkpointer 包文档
//	│   ├── base.go                          # Checkpointer 基础接口
//	│   ├── factory.go                       # Checkpointer 工厂
//	│   ├── inmemory.go                      # InMemoryCheckpointer 实现
//	│   ├── persistence.go                   # 持久化检查点
//	│   └── serializer.go                    # 检查点序列化
//	├── config/             # 会话配置
//	│   ├── doc.go                           # config 包文档
//	│   ├── config.go                        # MetadataLike（元数据接口）/BuiltinConfigLoader（内置配置加载器）/defaultSessionConfig（默认会话配置）
//	│   ├── env_loader.go                    # trySetEnv/loadEnvConfigs 环境加载
//	│   └── context.go                       # WithEnvs context 注入
//	├── constants/          # 会话常量
//	│   ├── doc.go                           # constants 包文档
//	│   └── constants.go                     # 配置键名/环境变量键名/默认值/映射表
//	├── controller/         # 会话控制器
//	│   ├── doc.go                           # controller 包文档
//	│   ├── chain_session.go                 # 链式会话
//	│   ├── data_container.go                # 数据容器
//	│   ├── global_controller.go             # 全局控制器
//	│   ├── paths.go                         # 路径管理
//	│   ├── schema.go                        # 控制器 Schema
//	│   ├── scope.go                         # 作用域
//	│   ├── scope_factory.go                 # 作用域工厂
//	│   └── session_controller.go            # SessionController 核心控制器
//	├── interaction/        # 交互管理
//	│   ├── doc.go                           # interaction 包文档
//	│   ├── base.go                          # ExecutableIDProvider（可执行ID提供者） 类型别名 + BaseInteraction（基础交互） + GraphInterrupt/Interrupt（图级中断） + AgentInterrupt（Agent中断） + 常量
//	│   ├── interaction.go                   # WorkflowInteraction（工作流交互） + SimpleAgentInteraction（简单Agent交互） + AgentInteraction（完整Agent交互） + InteractionOutput（交互输出）
//	│   └── interactive_input.go             # InteractiveInput 用户输入容器
//	├── interfaces/         # 统一接口定义
//	│   ├── doc.go                           # interfaces 包文档
//	│   ├── facade.go                        # SessionFacade 门面会话共有接口
//	│   └── interfaces.go                    # InnerSession（会话基类）/Checkpointer（检查点）/Storage（存储）/*Provider（提供者） 接口
//	├── internal/           # 内部会话实现
//	│   ├── doc.go                # internal 包文档
//	│   ├── agent_session.go      # AgentSession（Agent内部会话）
//	│   ├── agent_team_session.go # AgentTeamSession（Agent团队内部会话）
//	│   └── workflow_session.go   # WorkflowSession（工作流内部会话）/NodeSession（节点会话）/SubWorkflowSession（子工作流会话）
//	├── state/              # 状态接口与内存实现
//	│   ├── doc.go                           # state 包文档
//	│   ├── state.go                         # 双层接口 + 常量 + 兼容别名
//	│   ├── key.go                           # StateKey 类型
//	│   ├── agent_state_collection.go        # Agent 状态集合
//	│   ├── workflow_state_collection.go     # Workflow 四区状态集合
//	│   ├── workflow_commit_state.go         # Workflow 可提交状态
//	│   ├── workflow_inmemory_state.go       # InMemoryWorkflowState（内存工作流状态） 构造器
//	│   ├── inmemory_state.go                # InMemoryStateLike（内存状态实现）
//	│   ├── inmemory_commit_state.go         # InMemoryCommitState（内存可提交状态）
//	│   └── utils.go                         # getBySchema 等 StateKey 依赖函数
//	├── stream/             # 流写入管理
//	│   ├── doc.go                           # stream 包文档
//	│   ├── base.go                          # 流写入基础接口
//	│   ├── emitter.go                       # 流发射器
//	│   ├── manager.go                       # StreamWriterManager（流写入管理器） 管理器
//	│   ├── queue.go                         # 流队列
//	│   └── writer.go                        # StreamWriter 写入器
//	├── tracer/             # 会话追踪
//	│   ├── doc.go                           # tracer 包文档
//	│   ├── data.go                          # InvokeType（调用类型）/NodeStatus（节点状态）/TraceEvent（追踪事件） 枚举
//	│   ├── span.go                          # Span（追踪段）/TraceAgentSpan（Agent追踪段）/TraceWorkflowSpan（工作流追踪段）/SpanManager（段管理器）
//	│   ├── tracer.go                        # Tracer（追踪器） 核心 + TriggerParams（触发参数）
//	│   ├── handler.go                       # TraceAgentHandler（Agent追踪处理器）/TraceWorkflowHandler（工作流追踪处理器）
//	│   ├── workflow.go                      # TracerWorkflowUtils（工作流追踪工具） + WorkflowNodeSession（工作流节点会话）
//	│   └── decorator/                       # 追踪装饰器子包
//	│       ├── doc.go                       # decorator 子包文档
//	│       └── decorator.go                # TracedModelClient（追踪模型客户端）/TracedTool（追踪工具）/TracedWorkflow（追踪工作流） 装饰器 + Decorate*WithTrace + TracerSession（追踪会话） 接口
//	└── utils/              # 通用工具函数（嵌套路径/引用路径/字典/容器操作）
//	    ├── doc.go                           # utils 包文档
//	    ├── path.go                          # SplitNestedPath（拆分嵌套路径）/GetValueByNestedPath（按嵌套路径取值）/RootToPath（根到路径）/RootToIndex（根到索引）
//	    ├── ref.go                           # IsRefPath（是否引用路径）/ExtractOriginKey（提取原始键）
//	    ├── dict.go                          # UpdateDict（更新字典）/UpdateByKey（按键更新）/DeleteByKey（按键删除）/ExpandNestedStructure（展开嵌套结构）
//	    ├── container.go                     # SafeExtendContainer（安全扩展容器）/DeepCopyMap（深拷贝Map）/Slice（切片工具）/Value（取值）/Updates（更新集合）/ConvertUpdatesFromJSON（从JSON转换更新）
//	    ├── string.go                        # ContainsChar（包含字符）/ContainsSubstring（包含子串）/SplitString（拆分字符串）/ParseListIndexes（解析列表索引）
//	    └── constants.go                     # RegexMaxLength（正则最大长度）/NestedPathSplit（嵌套路径分隔）/NestedPathListSplit（嵌套路径列表分隔）
//
// 对应 Python 代码：openjiuwen/core/session/agent.py + openjiuwen/core/session/session.py + openjiuwen/core/session/workflow.py + openjiuwen/core/session/node.py + openjiuwen/core/session/internal/wrapper.py
//
// 核心类型/接口索引：
//
//	InnerSession        — 会话基类接口，所有会话类型的核心抽象
//	BaseSession         — 已废弃的 InnerSession 别名
//	SessionFacade       — 门面会话共有接口，Agent/Node/Router 门面的统一抽象
//	ProxySession         — 代理会话，将调用委托给内部 stub
//	Session              — Agent 公开会话，用户面向 API
//	AgentTeamSession     — Agent 团队公开会话，实现 SessionFacade 接口
//	WorkflowSession      — Workflow 公开会话，用户面向 API
//	NodeSessionFacade    — 工作流节点会话门面，组件开发者面向 API
//	RouterSessionFacade  — 路由会话门面，禁写壳（路由函数场景）
//	SessionConfig        — 会话配置接口，环境变量/工作流配置/Agent配置的统一抽象
//	WorkflowConfigProvider — 工作流配置提供者接口（占位，⤵️ 8.15 回填）
//	AgentConfig         — Agent 配置接口（定义于 single_agent/interfaces，实现于 single_agent/config）
//	WorkflowInteraction   — 工作流交互，通过 GraphInterrupt 暂停图执行
//	SimpleAgentInteraction — 简单 Agent 交互，无输入队列
//	AgentInteraction      — 完整 Agent 交互，含输入队列 + 检查点 + 流输出
//	InteractiveInput      — 用户交互输入容器
//	GraphInterrupt        — 图级中断异常
//	AgentInterrupt        — Agent 中断异常
package session

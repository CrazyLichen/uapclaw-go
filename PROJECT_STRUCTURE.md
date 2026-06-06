# UapClaw Go 项目目录结构

## 架构总览

```
internal/
  common/          ← 两者共享的基础设施（领域一）
  agentcore/       ← 对应 openjiuwen（Agent SDK 库，不可独立运行）
  swarm/           ← 对应 jiuwenswarm（可运行平台，依赖 agentcore）
```

**依赖关系**：swarm 依赖 agentcore，agentcore 不依赖 swarm。agentcore 是 SDK 库不直接暴露给 CLI，所有用户可见的入口（chat/serve/app/acp）都在 swarm 层。

**运行模式**：
```
uapclaw chat  → swarm/chat/repl → 调用 agentcore（SDK 库）
uapclaw serve → swarm/chat/http_api → 调用 agentcore（SDK 库）
uapclaw app   → swarm/server + swarm/gateway → 调用 agentcore（SDK 库）
uapclaw acp   → swarm/chat/acp_stdio → 调用 agentcore（SDK 库）
```

## 目录结构详情

```
/home/opensource/uap-claw-go/
│
├── cmd/
│   ├── uapclaw/                     # 统一 CLI 入口
│   │   └── main.go                  # 子命令: chat/serve/app/agentserver/gateway/web/init/acp
│   └── jiuwenbox/                   # JiuwenBox CLI（独立入口）
│       └── main.go
│
├── internal/
│   │
│   │  ╔══════════════════════════════════════════════════════╗
│   │  ║  common: 两者共享的基础设施（领域一）                    ║
│   │  ╚══════════════════════════════════════════════════════╝
│   ├── common/
│   │   ├── schema/
│   │   │   ├── card.go              # BaseCard, BaseParam
│   │   │   └── param.go
│   │   ├── exception/
│   │   │   ├── error.go             # BaseError
│   │   │   └── codes.go             # StatusCode 枚举
│   │   ├── config/
│   │   │   ├── config.go            # YAML 配置管理
│   │   │   └── watcher.go           # fsnotify 热重载
│   │   ├── logger/
│   │   │   └── logger.go            # zerolog 分级日志
│   │   ├── crypto/
│   │   │   └── aes.go               # AES 加密/解密
│   │   ├── workspace/
│   │   │   └── workspace.go         # ~/.uapclaw 路径管理
│   │   ├── version/
│   │   │   └── version.go           # 版本号
│   │   ├── wsorigin/
│   │   │   └── origin.go            # WebSocket Origin 检查
│   │   └── utils/
│   │       ├── port.go              # 端口等待
│   │       ├── singleton.go         # 单例模式
│   │       ├── pool.go              # 连接池
│   │       └── background.go        # 后台任务
│   │
│   │  ╔══════════════════════════════════════════════════════╗
│   │  ║  agentcore: Agent SDK 库（对应 openjiuwen）            ║
│   │  ║  领域二~九，SDK 库，不可独立运行                        ║
│   │  ║  所有用户入口都通过 swarm 调用 agentcore                ║
│   │  ╚══════════════════════════════════════════════════════╝
│   ├── agentcore/
│   │   │
│   │   │  ── 领域二：LLM 基础层 ──────────────────────────────
│   │   ├── llm/
│   │   │   ├── model.go              # Model 门面
│   │   │   ├── message.go            # BaseMessage, UserMessage, SystemMessage, AssistantMessage, ToolMessage
│   │   │   ├── tool_call.go          # ToolCall
│   │   │   ├── chunk.go              # AssistantMessageChunk, 增量合并
│   │   │   ├── generation.go         # GenerationResponse, Image/Audio/Video
│   │   │   ├── config.go             # ProviderType, ModelClientConfig, ModelRequestConfig, BaseModelInfo
│   │   │   ├── base_client.go        # BaseModelClient 接口
│   │   │   ├── openai_client.go      # OpenAI 客户端
│   │   │   ├── dashscope_client.go   # DashScope 客户端
│   │   │   ├── deepseek_client.go    # DeepSeek 客户端
│   │   │   ├── siliconflow_client.go # SiliconFlow 客户端
│   │   │   ├── headers.go            # Headers Helper
│   │   │   ├── output_parsers/       # 输出解析器（2.16 节）
│   │   │   │   ├── json_output_parser.go    # JsonOutputParser
│   │   │   │   ├── markdown_output_parser.go # MarkdownOutputParser
│   │   │   │   └── markdown_types.go        # MarkdownElement, MarkdownContent
│   │   │   └── init.go               # init_model 工厂
│   │   ├── prompt/
│   │   │   ├── template.go           # Prompt 模板系统
│   │   │   └── builder.go            # Prompt 构建
│   │   │
│   │   │  ── 领域三：工具系统 ────────────────────────────────
│   │   ├── tool/
│   │   │   ├── base.go               # Tool 接口, ToolCard, ToolInfo
│   │   │   ├── local_function.go     # LocalFunction
│   │   │   ├── registry.go           # @tool 等价注册 API
│   │   │   ├── mcp/
│   │   │   │   ├── base.go           # MCPTool, McpToolCard, McpServerConfig
│   │   │   │   └── client/           # SSE/stdio/OpenAPI/Playwright/StreamableHTTP
│   │   │   ├── restful/
│   │   │   │   ├── api.go            # RestfulApi
│   │   │   │   └── param_mapper.go   # APIParamMapper
│   │   │   ├── form/
│   │   │   │   └── handler.go        # Form Handler
│   │   │   ├── auth/
│   │   │   │   └── auth.go           # ToolAuthConfig, ToolAuthResult
│   │   │   └── utils/
│   │   │       └── schema.go         # Schema 转换工具
│   │   │
│   │   │  ── 领域四：存储层 ─────────────────────────────────
│   │   ├── store/
│   │   │   ├── kv/
│   │   │   │   ├── base.go           # BaseKVStore 接口
│   │   │   │   ├── memory.go         # InMemoryKVStore
│   │   │   │   ├── file.go           # ShelveStore 等价
│   │   │   │   ├── db.go             # DbBasedKVStore
│   │   │   │   └── redis.go          # RedisStore
│   │   │   ├── vector/
│   │   │   │   ├── base.go           # BaseVectorStore 接口, CollectionSchema, FieldSchema
│   │   │   │   ├── milvus.go         # MilvusVectorStore
│   │   │   │   └── chroma.go         # ChromaVectorStore
│   │   │   ├── db/
│   │   │   │   ├── base.go           # BaseDbStore 接口
│   │   │   │   ├── sqlite.go         # SQLite
│   │   │   │   └── postgres.go       # PostgreSQL
│   │   │   ├── message/
│   │   │   │   ├── base.go           # BaseMessageStore 接口
│   │   │   │   └── sql.go            # SqlMessageStore
│   │   │   ├── memory_index/
│   │   │   │   ├── base.go           # BaseMemoryIndex 接口
│   │   │   │   └── simple.go         # SimpleMemoryIndex
│   │   │   ├── embedding/
│   │   │   │   ├── base.go           # Embedding 接口
│   │   │   │   ├── openai.go         # OpenAIEmbedding
│   │   │   │   ├── dashscope.go      # DashScopeEmbedding
│   │   │   │   ├── api.go            # APIEmbedding
│   │   │   │   └── vllm.go           # VLLMEmbedding
│   │   │   ├── reranker/
│   │   │   │   ├── base.go           # Reranker 接口
│   │   │   │   ├── standard.go       # StandardReranker
│   │   │   │   └── chat.go           # ChatReranker
│   │   │   ├── graph/                # Graph Store
│   │   │   ├── object/               # Object Store (S3/OBS)
│   │   │   └── query/                # Query Builder
│   │   │
│   │   │  ── 领域五：会话与上下文引擎 ────────────────────────
│   │   ├── session/
│   │   │   ├── session.go            # BaseSession, AgentSession
│   │   │   ├── workflow_session.go   # WorkflowSession
│   │   │   ├── node.go              # SessionNode
│   │   │   ├── state.go             # State 体系
│   │   │   ├── stream.go            # StreamMode, OutputSchema, TraceSchema
│   │   │   ├── constants.go         # 会话常量
│   │   │   ├── utils.go             # 会话工具函数
│   │   │   ├── interaction/          # 交互管理
│   │   │   ├── controller/           # SessionController
│   │   │   ├── tracer/               # Session Tracer
│   │   │   ├── config/               # Session Config
│   │   │   └── checkpointer/
│   │   │       ├── base.go          # Checkpointer 接口
│   │   │       ├── factory.go       # CheckpointerFactory
│   │   │       ├── memory.go        # InMemoryCheckpointer
│   │   │       └── redis.go         # RedisCheckpointer
│   │   ├── context_engine/
│   │   │   ├── engine.go            # ContextEngine 门面
│   │   │   ├── model_context.go     # ModelContext 接口, ContextWindow, ContextStats
│   │   │   ├── config.go            # ContextEngineConfig
│   │   │   ├── token_counter.go     # TiktokenCounter
│   │   │   ├── context/             # Context 实现
│   │   │   └── processor/
│   │   │       ├── base.go          # ContextProcessor 接口
│   │   │       ├── compressor.go    # DialogueCompressor, FullCompact, MicroCompact, CurrentRound, RoundLevel
│   │   │       └── offloader.go     # MessageOffloader, MessageSummaryOffloader, ToolResultBudget
│   │   │
│   │   │  ── 领域六：Agent 核心 ──────────────────────────────
│   │   ├── agent/
│   │   │   ├── base.go              # BaseAgent 接口
│   │   │   ├── react_agent.go       # ReActAgent
│   │   │   ├── controller_agent.go  # ControllerAgent
│   │   │   ├── ability_manager.go   # AbilityManager
│   │   │   ├── callback.go          # AgentCallbackManager, AgentCallbackContext, AgentCallbackEvent
│   │   │   ├── schema/
│   │   │   │   ├── card.go          # AgentCard
│   │   │   │   └── result.go        # AgentResult, Part, Artifact
│   │   │   ├── rail/
│   │   │   │   ├── base.go          # AgentRail 接口, 10 个生命周期钩子
│   │   │   │   ├── inputs.go        # InvokeInputs, ModelCallInputs, ToolCallInputs, TaskIterationInputs
│   │   │   │   └── decorator.go     # @rail 等价
│   │   │   ├── interrupt/
│   │   │   │   ├── handler.go       # ToolInterruptHandler
│   │   │   │   └── state.go         # InterruptionState, ToolInterruptionState, ResumeContext
│   │   │   ├── skill/
│   │   │   │   └── manager.go       # SkillManager, Skill
│   │   │   └── prompts/             # Agent 系统提示词模板
│   │   ├── controller/
│   │   │   ├── controller.go        # Controller
│   │   │   ├── task_manager.go      # TaskManager
│   │   │   ├── event_queue.go       # EventQueue
│   │   │   └── scheduler.go         # TaskScheduler, EventHandler
│   │   ├── runner/
│   │   │   ├── runner.go            # Runner 单例
│   │   │   ├── resource_manager.go  # ResourceMgr
│   │   │   ├── callback_framework.go # AsyncCallbackFramework
│   │   │   ├── config.go            # RunnerConfig
│   │   │   ├── message_queue.go     # LocalMessageQueue
│   │   │   ├── spawn/               # Spawn 子进程
│   │   │   │   ├── config.go        # SpawnAgentConfig
│   │   │   │   ├── handle.go        # SpawnedProcessHandle
│   │   │   │   └── process.go       # spawn_process()
│   │   │   └── drunner/             # 分布式 Runner
│   │   │
│   │   │  ── 领域七：记忆、安全与检索 ────────────────────────
│   │   ├── memory/
│   │   │   ├── lite/
│   │   │   │   ├── manager.go       # CodingMemoryManager
│   │   │   │   ├── tools.go         # CodingMemoryTools
│   │   │   │   ├── context.go       # CodingMemoryToolContext
│   │   │   │   ├── frontmatter.go   # Frontmatter 解析
│   │   │   │   └── config.go        # MemoryConfig
│   │   │   ├── manage/
│   │   │   │   ├── fragment.go      # FragmentMemoryManager
│   │   │   │   ├── summary.go       # SummaryManager
│   │   │   │   ├── variable.go      # VariableManager
│   │   │   │   ├── write.go         # WriteManager
│   │   │   │   ├── search.go        # SearchManager
│   │   │   │   └── model/           # MemoryUnit, DataIdManager, SemanticStore 等
│   │   │   ├── graph/
│   │   │   │   ├── memory.go        # GraphMemory
│   │   │   │   └── extraction/      # 实体抽取
│   │   │   ├── external/
│   │   │   │   ├── provider.go      # MemoryProvider 协议
│   │   │   │   ├── mem0.go          # Mem0Provider
│   │   │   │   ├── openviking.go    # OpenVikingProvider
│   │   │   │   ├── openjiuwen.go    # OpenJiuwenMemoryProvider
│   │   │   │   └── agentarts.go     # AgentArtsMemoryProvider
│   │   │   ├── process/
│   │   │   │   ├── extract.go       # LongTermMemoryExtractor
│   │   │   │   └── refine.go        # MemoryAnalyzer, Refiner
│   │   │   ├── dreaming/
│   │   │   │   └── orchestrator.go  # Dreaming Orchestrator
│   │   │   ├── migration/
│   │   │   │   ├── plan.go          # MigrationPlan
│   │   │   │   ├── operation/       # 迁移操作注册表
│   │   │   │   └── migrator/        # 各类迁移器
│   │   │   ├── codec/               # Memory Codec
│   │   │   ├── common/              # Memory 公共工具
│   │   │   ├── prompts/             # Memory 提示词
│   │   │   └── long_term.go         # LongTermMemory
│   │   ├── security/
│   │   │   ├── guardrail.go         # BaseGuardrail, GuardrailBackend, GuardrailResult, RiskAssessment
│   │   │   └── backends/
│   │   │       ├── injection.go     # PromptInjectionGuardrail
│   │   │       └── jailbreak.go     # JailbreakGuardrail
│   │   │
│   │   │  ── 领域八：工作流与图引擎 + 多 Agent 团队 ──────────
│   │   ├── graph/
│   │   │   ├── graph.go             # Graph 接口
│   │   │   ├── executable.go        # ExecutableGraph
│   │   │   ├── state.go             # GraphState
│   │   │   ├── atomic_node.go       # AtomicNode
│   │   │   ├── vertex.go            # Vertex
│   │   │   ├── pregel/
│   │   │   │   ├── node.go          # PregelNode
│   │   │   │   ├── channel.go       # Channel
│   │   │   │   ├── router.go        # IRouter
│   │   │   │   └── message.go       # Graph Message, Interrupt, GraphInterrupt
│   │   │   ├── store/
│   │   │   │   └── memory.go        # InMemoryStore
│   │   │   └── stream_actor/        # StreamActor
│   │   ├── workflow/
│   │   │   ├── workflow.go          # Workflow 类
│   │   │   ├── config.go            # WorkflowConfig, WorkflowCard
│   │   │   └── component/
│   │   │       ├── flow.go          # StartComp, EndComp, BranchComp, LoopComp
│   │   │       ├── llm.go           # LLMComp, IntentDetectionComp, QuestionerComp
│   │   │       ├── tool.go          # ToolComp
│   │   │       ├── http.go          # HTTPRequestComponent
│   │   │       ├── retrieval.go     # KnowledgeRetrievalComp
│   │   │       └── react.go         # ReactComponent
│   │   ├── multi_agent/
│   │   │   ├── team.go              # BaseTeam 接口
│   │   │   ├── config.go            # TeamConfig
│   │   │   ├── schema/
│   │   │   │   └── team_card.go     # TeamCard, EventDrivenTeamCard
│   │   │   ├── runtime/
│   │   │   │   ├── runtime.go       # TeamRuntime
│   │   │   │   ├── communicable.go  # CommunicableAgent
│   │   │   │   ├── message_bus.go   # MessageBus
│   │   │   │   ├── router.go        # MessageRouter
│   │   │   │   └── subscription.go  # SubscriptionManager
│   │   │   ├── handoff/
│   │   │   │   └── team.go          # HandoffTeam
│   │   │   └── hierarchical/
│   │   │       ├── msgbus.go        # HierarchicalTeam (msgbus)
│   │   │       └── tools.go         # HierarchicalTeam (tools)
│   │   │
│   │   │  ── 领域九：DeepAgent 应用层 (Harness) ─────────────
│   │   ├── harness/
│   │   │   ├── deep_agent.go        # DeepAgent
│   │   │   ├── factory.go           # DeepAgent Factory
│   │   │   ├── config/              # DeepAgentConfig
│   │   │   ├── task_loop/
│   │   │   │   ├── controller.go    # TaskLoopController
│   │   │   │   ├── coordinator.go   # LoopCoordinator
│   │   │   │   ├── executor.go      # TaskLoopEventExecutor
│   │   │   │   └── spawn.go         # SessionSpawnExecutor
│   │   │   ├── rails/
│   │   │   │   ├── progressive.go   # ProgressiveToolRail
│   │   │   │   ├── completion.go    # TaskCompletionRail
│   │   │   │   ├── planning.go      # TaskPlanningRail
│   │   │   │   ├── agent_mode.go    # AgentModeRail
│   │   │   │   ├── heartbeat.go     # HeartbeatRail
│   │   │   │   ├── mcp.go           # McpRail
│   │   │   │   ├── lsp.go           # LSPRail
│   │   │   │   └── sysop.go         # SysOperationRail
│   │   │   ├── security/
│   │   │   │   ├── shell_ast.go     # ShellAST 分析
│   │   │   │   └── policy.go        # TieredPolicy
│   │   │   ├── subagents/
│   │   │   │   ├── research.go      # ResearchAgent
│   │   │   │   ├── browser.go       # BrowserAgent
│   │   │   │   ├── code.go          # CodeAgent
│   │   │   │   ├── plan.go          # PlanAgent
│   │   │   │   ├── verify.go        # VerificationAgent
│   │   │   │   ├── explore.go       # ExploreAgent
│   │   │   │   └── mobile.go        # MobileGUIAgent
│   │   │   ├── tools/
│   │   │   │   ├── shell.go         # Shell 工具 (bash/powershell)
│   │   │   │   ├── filesystem.go    # 文件系统工具
│   │   │   │   ├── code.go          # 代码工具
│   │   │   │   ├── mcp.go           # MCP 工具集
│   │   │   │   ├── worktree.go      # Worktree 工具
│   │   │   │   ├── browser.go       # 浏览器工具
│   │   │   │   ├── cron.go          # Cron 工具
│   │   │   │   ├── todo.go          # TODO 工具
│   │   │   │   ├── ask_user.go      # AskUser 工具
│   │   │   │   ├── memory.go        # Memory 工具
│   │   │   │   ├── agent_mode.go    # AgentMode 工具
│   │   │   │   ├── multimodal.go    # 多模态工具
│   │   │   │   └── search.go        # 搜索工具
│   │   │   ├── workspace/
│   │   │   │   └── workspace.go     # Workspace 管理
│   │   │   ├── resources/
│   │   │   │   └── resources.go     # Harness 资源
│   │   │   ├── schema/
│   │   │   │   └── schema.go        # Harness Schema
│   │   │   ├── prompts/
│   │   │   │   └── prompts.go       # Harness 提示词
│   │   │   └── lsp/                 # LSP 集成
│   │   ├── agent_teams/
│   │   │   ├── team_agent.go        # TeamAgent
│   │   │   ├── blueprint/
│   │   │   │   └── blueprint.go     # Blueprint
│   │   │   ├── coordination/
│   │   │   │   ├── kernel.go        # CoordinationKernel
│   │   │   │   ├── event_bus.go     # EventBus
│   │   │   │   └── dispatcher.go    # Dispatcher
│   │   │   ├── memory/
│   │   │   │   └── shared.go        # SharedMemory, MemberMemoryToolkit
│   │   │   ├── messager/
│   │   │   │   └── messager.go      # Messager (inprocess/ZMQ)
│   │   │   ├── spawn/
│   │   │   │   └── spawn.go         # SpawnManager
│   │   │   ├── interaction/          # Team SessionManager
│   │   │   ├── observability/
│   │   │   │   └── otel.go          # OpenTelemetry 集成
│   │   │   ├── rails/               # Team Rails
│   │   │   ├── prompts/             # Team Prompts
│   │   │   ├── models/              # Team Models
│   │   │   ├── runtime/             # Team Runtime
│   │   │   └── team_workspace/      # Team Workspace
│   │   ├── agent_evolving/
│   │   │   ├── trainer/
│   │   │   │   └── trainer.go       # Trainer
│   │   │   ├── evaluator/
│   │   │   │   └── evaluator.go     # BaseEvaluator
│   │   │   ├── optimizer/
│   │   │   │   └── optimizer.go     # InstructionOptimizer
│   │   │   ├── signal/
│   │   │   │   └── detector.go      # SignalDetector
│   │   │   ├── agent_rl/
│   │   │   │   ├── offline/         # OfflineRLOptimizer, TrainingCoordinator
│   │   │   │   ├── online/          # OnlineRLOptimizer
│   │   │   │   ├── dataset.go       # Case, EvaluatedCase, CaseLoader
│   │   │   │   └── reward.go        # RewardRegistry, Rollout
│   │   │   ├── trajectory/
│   │   │   │   └── store.go         # TrajectoryStore
│   │   │   ├── checkpointing/
│   │   │   │   └── store.go         # EvolveCheckpoint
│   │   │   ├── experience/
│   │   │   │   └── lifecycle.go     # ExperienceLifecycle, SkillExperience
│   │   │   └── update.go            # UpdateExecution
│   │   ├── extensions/
│   │   │   ├── a2a/
│   │   │   │   ├── server.go        # A2AServer
│   │   │   │   ├── client.go        # A2AClient
│   │   │   │   ├── remote.go        # A2ARemoteClient
│   │   │   │   └── adapter.go       # A2AServerAdapter
│   │   │   ├── context_evolver/
│   │   │   │   └── evolver.go       # ContextEvolvingReActAgent
│   │   │   ├── store/
│   │   │   │   ├── gauss_db.go      # GaussDbStore
│   │   │   │   ├── gauss_vector.go  # GaussVectorStore
│   │   │   │   └── es_vector.go     # ESVectorStore
│   │   │   ├── checkpointer/
│   │   │   │   └── redis.go         # RedisCheckpointer
│   │   │   ├── message_queue/
│   │   │   │   └── pulsar.go        # Pulsar Message Queue
│   │   │   └── sys_operation/
│   │   │       ├── jiuwenbox.go     # JiuwenBoxProvider
│   │   │       └── aio.go           # AioProvider
│   │   └── sys_operation/
│   │       ├── base.go              # SysOperation 接口, SysOperationCard
│   │       ├── local.go             # LocalSysOperation
│   │       ├── sandbox.go           # SandboxSysOperation
│   │       └── shell_registry.go    # Shell Process Registry
│   │
│   │  ╔══════════════════════════════════════════════════════╗
│   │  ║  swarm: 可运行平台（对应 jiuwenswarm）                 ║
│   │  ║  依赖 agentcore，所有 CLI 入口都走 swarm               ║
│   │  ╚══════════════════════════════════════════════════════╝
│   ├── swarm/
│   │   │
│   │   │  ── 领域十：AgentServer + 独立交互入口 ────────────
│   │   ├── schema/
│   │   │   ├── method.go               # ReqMethod (~100 方法)
│   │   │   ├── event.go                # EventType
│   │   │   ├── mode.go                 # Mode (agent.plan/code.normal/team 等)
│   │   │   ├── message.go              # Message
│   │   │   ├── agent_request.go        # AgentRequest, AgentResponse, AgentResponseChunk
│   │   │   ├── permission.go           # PermissionContext
│   │   │   └── hook_event.go           # HookEventBase
│   │   ├── e2a/
│   │   │   ├── models.go               # E2AEnvelope, E2AResponse, E2AProvenance, E2AAuth, E2AFileRef
│   │   │   ├── wire_codec.go           # Wire Codec (E2A ↔ AgentResponse 编解码)
│   │   │   ├── constants.go            # 协议常量 (source protocols, response kinds, ACP methods)
│   │   │   ├── normalize.go            # gateway_normalize (Message/E2A/AgentResponse 格式互转)
│   │   │   ├── compat.go               # agent_compat (e2a_to_agent_request)
│   │   │   └── adapters/
│   │   │       ├── acp.go              # ACP JSON-RPC 适配器
│   │   │       └── a2a.go              # A2A 协议适配器
│   │   ├── server/
│   │   │   ├── ws_server.go            # AgentWebSocketServer (WS 服务端, RPC 方法分发)
│   │   │   ├── claw_facade.go          # JiuWenClaw 门面 (SDK 路由/会话队列/流式包装)
│   │   │   ├── agent_adapter.go        # AgentAdapter 接口与工厂
│   │   │   ├── adapter_agent.go        # Agent 模式适配器
│   │   │   ├── adapter_code.go         # Code 模式适配器
│   │   │   ├── adapter_deep.go         # Deep 模式适配器
│   │   │   ├── agent_manager.go        # AgentManager (多实例管理)
│   │   │   ├── agent_config.go         # AgentConfigService (配置 CRUD)
│   │   │   ├── tenant_pool.go          # TenantAgentPool (多租户)
│   │   │   ├── session/
│   │   │   │   ├── manager.go          # SessionManager (LIFO 任务队列)
│   │   │   │   ├── history.go          # SessionHistory (JSONL 持久化)
│   │   │   │   ├── metadata.go         # SessionMetadata (元数据缓存)
│   │   │   │   └── rename.go           # SessionRename
│   │   │   ├── skill/
│   │   │   │   ├── manager.go          # SkillManager (Server 端)
│   │   │   │   └── skilldev/           # 技能开发管道
│   │   │   ├── sandbox/
│   │   │   │   └── runner.go           # JiuwenBox Runner
│   │   │   ├── gateway_push/
│   │   │   │   ├── transport.go        # GatewayPush Transport
│   │   │   │   └── wire.go             # GatewayPush Wire
│   │   │   ├── hooks/                  # AgentServer Hooks
│   │   │   └── utils/                  # Diff, Stream 工具
│   │   ├── chat/
│   │   │   ├── repl.go                 # 🔥 CLI REPL 交互 (独立聊天模式)
│   │   │   ├── http_api.go             # 🔥 HTTP REST API
│   │   │   ├── sse.go                  # 🔥 SSE 流式响应
│   │   │   ├── slash_command.go        # Slash 命令处理 (/mode /new /sandbox /model)
│   │   │   └── acp_stdio.go            # ACP stdio JSON-RPC 协议
│   │   ├── extension/
│   │   │   ├── base.go                 # BaseExtension 接口
│   │   │   ├── registry.go             # ExtensionRegistry (单例)
│   │   │   ├── manager.go              # ExtensionManager (加载/卸载)
│   │   │   ├── hook_event.go           # Hook Events
│   │   │   ├── hooks_context.go        # Hooks Context
│   │   │   ├── loader.go               # Extension Loader
│   │   │   ├── types.go                # Extension Types
│   │   │   └── crypto_utility.go       # CryptoUtility
│   │   ├── agents/                     # Swarm 侧 Harness 集成
│   │   │   ├── prompt/
│   │   │   │   └── builder.go          # PromptBuilder (Agent/Code/Team 模式)
│   │   │   ├── rails/
│   │   │   │   ├── ask_user.go         # AskUserRail
│   │   │   │   ├── avatar.go           # AvatarRail
│   │   │   │   ├── permissions.go      # PermissionRails (allow/ask/deny, owner scopes)
│   │   │   │   ├── interrupt.go        # Interrupt Helpers
│   │   │   │   ├── project_memory.go   # ProjectMemoryRail
│   │   │   │   ├── response_prompt.go  # ResponsePromptRail
│   │   │   │   ├── runtime_prompt.go   # RuntimePromptRail
│   │   │   │   └── stream_event.go     # StreamEventRail
│   │   │   ├── auto_harness/
│   │   │   │   ├── service.go          # AutoHarness 服务
│   │   │   │   ├── scheduler.go        # AutoHarness 调度
│   │   │   │   ├── task_store.go       # AutoHarness 任务存储
│   │   │   │   └── validator.go        # AutoHarness 配置校验
│   │   │   ├── memory/
│   │   │   │   ├── config.go           # 记忆配置
│   │   │   │   ├── dreaming.go         # 记忆梦游整理
│   │   │   │   ├── embeddings.go       # 记忆嵌入
│   │   │   │   ├── external_builder.go # 外部记忆构建
│   │   │   │   ├── external_config.go  # 外部记忆配置
│   │   │   │   └── forbidden.go        # 禁止记忆模式
│   │   │   ├── team/
│   │   │   │   ├── manager.go          # TeamManager
│   │   │   │   ├── bootstrap.go        # Team Bootstrap
│   │   │   │   ├── distributed.go      # DistributedRuntime
│   │   │   │   ├── a2x/                # A2X 客户端
│   │   │   │   ├── rails/              # Team Rails
│   │   │   │   └── monitor.go          # MonitorHandler
│   │   │   ├── tools/                  # Swarm 内置工具集
│   │   │   │   ├── browser.go          # 浏览器工具
│   │   │   │   ├── mcp.go              # MCP 工具包
│   │   │   │   ├── search.go           # 搜索工具
│   │   │   │   ├── video.go            # 视频工具
│   │   │   │   ├── send_file.go        # 发文件
│   │   │   │   ├── todo.go             # 用户 TODO
│   │   │   │   ├── cron.go             # Cron 工具
│   │   │   │   └── xiaoyi_phone.go     # 小艺电话工具
│   │   │   ├── session_ops.go          # SessionOpsService
│   │   │   └── memory_rpc.go           # MemoryRPC
│   │   │
│   │   │  ── 领域十一：Gateway + IM 渠道 ──────────────────
│   │   ├── gateway/
│   │   │   ├── server.go               # GatewayServer (多路由 WS 服务器)
│   │   │   ├── channel_manager.go      # ChannelManager (注册/注销/分发)
│   │   │   ├── message_handler.go      # MessageHandler (入站→AS, 出站→Channel)
│   │   │   ├── agent_client.go         # WebSocketAgentServerClient (WS 客户端)
│   │   │   ├── route_binding.go        # RouteBinding
│   │   │   ├── session_map.go          # SessionMap
│   │   │   ├── interaction_context.go  # InteractionContext
│   │   │   ├── heartbeat.go            # GatewayHeartbeatService
│   │   │   ├── cron/
│   │   │   │   ├── controller.go       # CronController
│   │   │   │   ├── scheduler.go        # CronSchedulerService
│   │   │   │   ├── store.go            # CronJobStore (JSON)
│   │   │   │   ├── models.go           # CronJob 模型
│   │   │   │   └── expr.go             # Cron 表达式解析
│   │   │   ├── hooks/                  # Gateway Hooks
│   │   │   ├── pipeline/               # IM Pipeline (数字人模式)
│   │   │   └── channel/
│   │   │       ├── base.go             # BaseChannel 接口
│   │   │       ├── web/                # Web 通道 (WebSocket + HTTP RPC)
│   │   │       ├── tui/                # TUI 通道 (终端交互)
│   │   │       ├── feishu/             # 飞书 (Lark) 通道
│   │   │       ├── dingtalk/           # 钉钉通道
│   │   │       ├── telegram/           # Telegram Bot 通道
│   │   │       ├── discord/            # Discord Bot 通道
│   │   │       ├── wechat/             # 微信 (iLinkAI) 通道
│   │   │       ├── wecom/              # 企微 (WeCom) 通道
│   │   │       ├── whatsapp/           # WhatsApp 通道
│   │   │       ├── xiaoyi/             # 小艺通道
│   │   │       ├── adapter/            # IM 平台通用适配器
│   │   │       ├── acp/                # ACP 协议桥接
│   │   │       └── a2a/                # A2A 协议通道
│   │   │
│   │   │  ── 领域十二：沙箱与部署 ────────────────────────
│   │   └── jiuwenbox/
│   │       ├── policy/
│   │       │   ├── engine.go           # 策略引擎
│   │       │   └── schema.go           # YAML 策略定义
│   │       ├── sandbox/
│   │       │   ├── executor.go         # bwrap/landlock/seccomp/cgroup 进程隔离
│   │       │   └── config.go           # 沙箱配置
│   │       ├── proxy/
│   │       │   └── inference.go        # 推理隐私代理
│   │       ├── runtime/
│   │       │   ├── manager.go          # 沙箱运行时管理
│   │       │   └── file_share.go       # 文件共享
│   │       └── server/
│   │           ├── server.go           # HTTP API 服务
│   │           ├── sandbox_handler.go  # 沙箱 API 路由
│   │           ├── proxy_handler.go    # 代理 API 路由
│   │           └── policy_handler.go   # 策略 API 路由
│   │
│   └── (无其他顶层目录)
│
├── pkg/                             # 可导出公共包（暂空）
│   └── ...
│
├── resources/                       # 配置模板、静态资源
│   └── config.yaml
│
├── go.mod
├── go.sum
├── Makefile
├── Dockerfile
├── IMPLEMENTATION_PLAN.md           # 实现计划文档
└── PROJECT_STRUCTURE.md             # 本文档

// Package adapter 提供 Agent 适配器接口与工厂。
// 定义 AgentAdapter 接口——Agent SDK 后端的最小能力集，
// 以及 CreateAdapter 工厂函数按 SDK+Mode 创建适配器实例。
// 额外定义 3 个可选接口（ContextCompressor/DreamingController/GatewayDisconnectHandler），
// DeepAdapter 和 CodeAdapter 均实现，JiuWenClaw 门面通过类型断言调用。
// 三种模式适配器：
//   - DeepAdapter：Deep SDK 适配器，agent/plan/fast/team 模式均使用
//   - CodeAdapter：Code 模式适配器，组合委托 DeepAdapter，仅覆盖 CreateInstance
//   - Python 中无独立 AgentAdapter，agent 模式直接使用 DeepAdapter
//
// # 会话分身（根 router + 会话 child）
//
// 对齐 Python interface_deep.py 的双形态单类：根适配器只做路由与模板持有，
// 每个会话由 sessionChildFactory 造一个 child 独享 DeepAgent（自己的 rail 树、
// 工具表、reactAgent.context_engine）。对应上游 _session_adapters（:2126）。
//
//   - 根的 CreateInstance 在 config 段之后早退，不建自己的 DeepAgent
//     （_skip_own_instance_build :12727）；需要句柄的非 chat 路径走
//     EnsureInstance 惰建（ensure_instance :12809）。
//   - chat 与服务方法在入口分流到 child（即"分流头"），方法体本身不需要搬迁：
//     分流后 d 已是 child，d.instance / d.rails / context_engine 自然都是 child 的。
//   - ProcessInterrupt 对 cancel/supplement 只查缓存不建 child（上游 :16330，
//     建 child 会跑完整初始化把 cancel 卡住）。
//   - 6 个骨架方法（HandleSwarmflowReply / DispatchGoalControl /
//     GetContextUsageEvent / GenerateBtwAnswer 等）本批只落分流头，
//     方法体归各自章节。
//
// 详见 docs/superpowers/specs/2026-11-09-10.3.28f-session-adapter-fork-design.md。
//
// 文件目录：
//
//	adapter/
//	├── doc.go                    # 包文档
//	├── interface.go              # AgentAdapter 接口（含会话分身 5 方法）+ 3 个可选接口
//	├── factory.go                # CreateAdapter 工厂 + ResolveSDKChoice
//	├── deep_adapter.go           # DeepAdapter 核心结构体 + 8 接口方法 + 模型构建 + CWD 种子
//	├── deep_adapter_sessions.go  # 会话分身：child 工厂 + 缓存/锁/TTL 淘汰 + 清理 + 根 instance 惰建 + 分流辅助
//	├── deep_adapter_rails.go     # ~20 个 rail builder + buildAgentRails + 幂等 UpdateAgentRails
//	├── deep_adapter_plan.go      # plan 状态机接入（EnsureState/check_post_process_exit/控制事件）
//	├── deep_adapter_skills.go    # 技能 rail 热刷新（RefreshSkillRails）+ 技能扫描根汇总
//	├── deep_adapter_mcp.go       # MCP 管理 6 方法
//	├── deep_adapter_a2x.go       # A2X 客户端 5 方法 + Cron 上下文 2 方法（⤵️）
//	├── deep_adapter_tools.go     # Tool 同步 5 方法 + 多模态配置 + ToolCards 构建 + 工具名常量
//	├── multimodal_config.go      # YAML 配置映射（ApplyVideo/Vision/AudioModelConfigFromYAML + DedicatedMultimodalModelConfigured）
//	├── deep_adapter_slash.go     # Slash 命令 5 个 + governance approval（⤵️）
//	├── deep_adapter_evolution.go # EvolutionWatcher + ContextCompressor + Recap（⤵️）
//	├── deep_adapter_team.go      # TeamSkillApproval + team 分流 + ⤵️ stub 方法（⤵️）
//	├── team_helpers.go           # Team 模式运行时辅助函数（10.3.8）：常量/全局状态/纯函数/事件判断/审批/broadcast/元数据同步/slash 回调
//	├── deep_adapter_dreaming.go  # DreamingController 接口实现（⤵️）
//	├── deep_adapter_config.go    # RuntimeConfig + Profile/Prompt/Subagent + createSysOperation
//	├── deep_adapter_acp.go       # ACP 运行时工具刷新（10.6.24d：能力装卸+快照日志）
//	├── deep_adapter_session_request.go # 外层会话选择：getSessionAdapterForRequest + isHostPermissionUpdateInput（20.2 R-9）
//	├── deep_adapter_process_cli.go     # configureProcessCliRun + ProcessCliRunOptions（20.2 R-10）
//	├── deep_adapter_goal.go            # Goal 控制 RPC 分发（dispatchGoalControl + 记录 payload 组装）
//	├── deep_adapter_goal_history.go     # Goal 目标历史延迟落库（pendingGoalHistory + flush）
//	├── deep_adapter_interaction.go      # 交互会话生命周期（StartInteraction + 输出交接 handoff）
//	├── goal_events.go                   # Goal 事件常量与工具定义（59 行）
//	├── person_drove_turn.go             # 判定本轮是否由真人用户驱动（personDroveTurn + 终态归因）
//	├── evolution/                # Evolution 事件分类/状态提取/推送辅助（10.3.9）
//	│   ├── doc.go                # 包文档
//	│   └── helpers.go            # 3 结构体 + 常量/变量 + ~22 导出函数
//	├── agent_tool.go             # AgentTool 自定义 Agent 调度工具（10.3.7）
//	├── code_agent_rail.go        # CodeAgentRail Code 模式自定义 Agent 护栏 + 常量 + 辅助函数（10.3.7）
//	└── code_adapter.go           # CodeAdapter Code 模式适配器 + 可选接口委托
//
// 对应 Python 代码：jiuwenswarm/server/runtime/agent_adapter/interface_deep.py
package adapter

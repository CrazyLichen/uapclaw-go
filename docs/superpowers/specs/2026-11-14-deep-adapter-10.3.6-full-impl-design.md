# 10.3.6 DeepAdapter 完整实现设计文档

> 本文档描述 DeepAdapter 从骨架升级为完整实现的设计，全量对齐 Python `interface_deep.py`（6047 行）。
> 依赖模块未实现的部分在方法中保留并用回填注释标记。

---

## 1. 流程位置与作用

```
用户请求 → Gateway → [E2A 协议] → AgentServer
                                   ↓
                              JiuWenClaw 门面（10.3.2）
                                   ↓
                           AgentAdapter 接口（10.3.3）
                                   ↓
                        ┌──────────┴──────────┐
                  DeepAdapter(10.3.6)   CodeAdapter(10.3.5)
                  (agent/plan/fast/team)   (code 模式,组合委托DeepAdapter)
                        ↓
                  agentcore (DeepAgent/Runner/Model/Rails/...)
```

DeepAdapter 是 **AgentServer 处理链的最后一层 Go 代码边界**——Gateway→AgentServer→Adapter→agentcore 链路中，进入 agentcore SDK 之前的最后一个组件。所有 E2A 请求经过 JiuWenClaw 门面路由后，最终落到 DeepAdapter 的方法上。

### 核心方法作用

| 方法 | 作用 | 在会话流程中的位置 |
|------|------|-------------------|
| `CreateInstance` | 冷启动：创建 DeepAgent 实例（模型/rails/tools/子代理/MCP） | AgentServer 首次初始化 |
| `ReloadAgentConfig` | 热重载：不重启进程更新模型/rails/tools/MCP | 配置变更时 |
| `ProcessMessageImpl` | 非流式执行：Runner.RunAgent → 完整响应 | 单次问答 |
| `ProcessMessageStreamImpl` | 流式执行：Runner.RunAgentStreaming → chunk 通道 | 流式对话（主要使用场景） |
| `ProcessInterrupt` | 中断处理：pause/resume/cancel/supplement | 用户中断 Agent 执行 |
| `HandleUserAnswer` | 用户回答：evolution 审批 / 权限审批 / team skill 审批 | 权限确认弹窗的回调 |
| `HandleHeartbeat` | 心跳：检测 heartbeat session 前缀，注入心跳 prompt | 定时心跳保活 |
| `Cleanup` | 资源释放：关闭 A2X 客户端等 | 进程退出 |

---

## 2. 决策记录

| 决策项 | 选择 | 理由 |
|--------|------|------|
| 实现范围 | 全量 A-E，依赖模块未实现的用 ⤵️ 标记 | 完整对齐 Python 6047 行 |
| 回填标记风格 | 混合：关键方法用步骤编号+完整描述(B)，简单标记用简短(A) | 平衡可读性和回填精确度 |
| Slash/Evolution/Team 拆分 | 同包多文件（adapter 包内） | 同包访问非导出字段，无需接口注入 |
| Stream Chunk 解析 | 直接用 `OutputSchema` 真实类型 | `OutputSchema` 已实现，输入输出两端类型安全 |
| Rail builder | 写完整 ~20 个骨架，已实现的用真实类型，未实现的用 interface{} + ⤵️ | 回填时只需"填肉" |
| MCP/ToolSync/多模态 | 全部完整实现 | `McpServerConfig`/`ResourceMgr`/`AbilityManager` 都已实现 |
| DeepAgent 实例化 | 通过 `harness.CreateDeepAgent()` 工厂 | 对齐 Python `create_deep_agent()` |
| CreateInstance 组装 | 在 CreateInstance 中逐步组装 `CreateDeepAgentParams` | 步骤对齐 Python，无需额外方法 |
| instance 字段类型 | 升级为 `*harness.DeepAgent` | 编译期类型安全，直接调用方法 |
| 执行模型 | `Runner.RunAgent()` / `Runner.RunAgentStreaming()` | 对齐 Python `Runner.run_agent()` |
| 流式 goroutine 模型 | goroutine 从 stream.Schema channel 读取，parseStreamChunk 转换，写入 AgentResponseChunk channel | 对齐 Python `async for chunk in Runner.run_agent_streaming()` |
| ContextCompression | 可选接口 `ContextCompressor` | AgentAdapter 核心接口不变，扩展能力通过组合接口 |
| Dreaming | 可选接口 `DreamingController` | 门面直接调用 |
| GatewayDisconnect | 可选接口 `GatewayDisconnectHandler` | 门面直接调用 |
| EvolutionWatcher/TeamSkillApproval/Slash | DeepAdapter 导出/非导出方法，不定义接口 | 由 HandleUserAnswer/ProcessMessageStreamImpl 内部路由 |

---

## 3. 可选接口定义

### 3.1 ContextCompressor

```go
// ContextCompressor 上下文压缩可选接口。
// DeepAdapter 额外实现此接口，JiuWenClaw 门面通过类型断言调用。
//
// 对应 Python: JiuWenClawDeepAdapter.compress_context / get_context_usage / generate_recap
type ContextCompressor interface {
    // CompressContext 触发上下文压缩。
    // 对应 Python: JiuWenClawDeepAdapter.compress_context() (line 5380-5570)
    CompressContext(ctx context.Context, sessionID string, session any, returnState bool) (map[string]any, error)
    // GetContextUsage 获取上下文窗口占用率。
    // 对应 Python: JiuWenClawDeepAdapter.get_context_usage() (line 5572-5588)
    GetContextUsage(ctx context.Context, sessionID string) (map[string]any, error)
    // GenerateRecap 生成会话回顾摘要。
    // 对应 Python: JiuWenClawDeepAdapter.generate_recap() (line 5590-5663)
    GenerateRecap(ctx context.Context, sessionID string) (map[string]any, error)
}
```

### 3.2 DreamingController

```go
// DreamingController Dreaming 启停可选接口。
// DeepAdapter 额外实现此接口，JiuWenClaw 门面通过类型断言调用。
//
// 对应 Python: JiuWenClawDeepAdapter.try_start_dreaming / try_stop_dreaming
type DreamingController interface {
    // TryStartDreaming 尝试启动 dreaming 进程。
    // 对应 Python: JiuWenClawDeepAdapter.try_start_dreaming() (line 5935-5954)
    TryStartDreaming(ctx context.Context, busyChecker func() bool) error
    // TryStopDreaming 停止 dreaming 进程。
    // 对应 Python: JiuWenClawDeepAdapter.try_stop_dreaming() (line 5956-5965)
    TryStopDreaming(ctx context.Context) error
}
```

### 3.3 GatewayDisconnectHandler

```go
// GatewayDisconnectHandler Gateway 断连处理可选接口。
// DeepAdapter 额外实现此接口，JiuWenClaw 门面通过类型断言调用。
//
// 对应 Python: JiuWenClawDeepAdapter.abort_on_gateway_disconnect
type GatewayDisconnectHandler interface {
    // AbortOnGatewayDisconnect Gateway 断连时全局中止。
    // 对应 Python: JiuWenClawDeepAdapter.abort_on_gateway_disconnect() (line 3539-3578)
    AbortOnGatewayDisconnect(ctx context.Context)
}
```

---

## 4. 可完整实现 vs ⤵️ 标记

### 4.1 可完整实现（真实类型 + 真实调用）

| 模块 | 方法 | 依赖 |
|------|------|------|
| `CreateInstance` | 全部 25 步 | `harness.CreateDeepAgent` + `config` + `dotenv` + `llm` + `checkpointer` + `cwd` |
| `ReloadAgentConfig` | 全部 13 步 | `DeepAgent.ConfigureDeepConfig` + `config` + `llm` |
| `ProcessMessageImpl` | 核心执行 | `Runner.RunAgent` + `DeepAgent.Abort` |
| `ProcessMessageStreamImpl` | 核心执行 | `Runner.RunAgentStreaming` + goroutine + parseStreamChunk |
| `ProcessInterrupt` | 核心逻辑 | `DeepAgent.Abort` + session 活跃管理 |
| `HandleUserAnswer` | 前缀分发 | 已实现，slash/evolution/team 部分依赖 ⤵️ Rail |
| `HandleHeartbeat` | 全部 | 已实现 |
| `Cleanup` | A2X 关闭 | ⤵️ A2X 包未实现 |
| `_parseStreamChunk` | 15+ 种 chunk 类型 | `OutputSchema` + `ControllerOutputPayload` |
| MCP 管理 | 6 方法 | `McpServerConfig` + `ResourceMgr.AddMcpServer/RemoveMcpServer` |
| Tool 同步 | ~10 方法 | `AbilityManager.Add/Remove` |
| 多模态配置 | 5 方法 | 配置解析逻辑 |
| 已实现 Rail builder | HeartbeatRail/TaskPlanningRail/SysOperationRail/AgentModeRail/McpRail | 真实类型 |

### 4.2 仍需 ⤵️ 标记

| 模块 | ⤵️ 依赖 | 说明 |
|------|---------|------|
| 未实现 15 个 Rail | 10.6.3-10 Swarm Rails | SkillUseRail/SubagentRail/StreamEventRail/SecurityRail/MemoryRail/ExternalMemoryRail/AvatarRail/RuntimePromptRail/ResponsePromptRail/ContextAssembleRail/ContextProcessorRail/PermissionInterruptRail/LspRail/ProjectMemoryRail/CodingMemoryRail/WorktreeRail/CodeAgentRail |
| A2X 客户端管理 | A2X | `initA2xClient` / `tryInitA2xClient` / `closeA2xClient` / `syncA2xRuntimeState` |
| Cron 运行时桥接 | 11.10 | `bindRuntimeCronContext` / `resetRuntimeCronContext` |
| Slash 命令处理 | 10.6.3-10 SkillEvolutionRail | `/evolve` 等 5 个命令 + `_handleGovernanceApproval` |
| EvolutionWatcher | 10.6.3-10 SkillEvolutionRail | `_watchEvolutionAndPush` / `_onEvolutionWatcherDone` |
| EvolutionApproval | 10.6.3-10 SkillEvolutionRail | `_handleEvolutionApproval` |
| TeamSkillApproval | 10.6.3-10 TeamSkillEvolutionRail | `handleTeamSkillEvolveApproval` / `findTeamSkillRail` |
| Team 模式分流 | 10.3.7-11 TeamHelpers | `processTeamMessageStream` |
| AutoHarness 分流 | 10.6.11-12 AutoHarnessService | `autoHarnessService.run` |
| Dreaming 启停 | 10.6.13-18 Swarm Memory | `tryStartDreaming` / `tryStopDreaming` |
| Context compression | SessionHistory(JSONL) | `compressContext` / `get_contextUsage` / `generateRecap` |
| Recap 辅助 | SessionHistory(JSONL) | `_getRecentMessages` / `_callModelForRecap` |
| Permission 上下文 | 10.1.8 PermissionContext | `setupPermissionContext` / `cleanupPermissionContext` |
| `_update_rails_for_mode` | 未实现 Rail | 按模式注册/注销 Rail |
| `_update_tools_for_mode` | 未实现 Tool | 按模式注册/注销多会话工具 |
| `_update_prompt_for_mode` | RuntimePromptRail | 同步 system_prompt_builder 语言 |
| `_update_permission_rail` | PermissionInterruptRail | 原地更新或新建 PermissionRail |
| `loadUserRails` | 10.6.3-10 | 动态加载用户 Rail 扩展 |

---

## 5. 文件清单

### 5.1 adapter 包内文件

| 文件 | 操作 | 内容 | 预估行数 |
|------|------|------|---------|
| `deep_adapter.go` | 修改 | 核心结构体（instance 升级为 `*harness.DeepAgent`）+ 8 接口方法完整实现 + session 辅助 + model 构建 + CWD 种子 + params 解析 | ~1500 |
| `deep_adapter_rails.go` | 新建 | ~20 个 rail builder 方法 + `_buildAgentRails` + `_updateRailsForMode` + `_updatePromptForMode` | ~800 |
| `deep_adapter_mcp.go` | 新建 | MCP 管理 6 方法：`buildMcpServerConfig` / `extractEnabledMcpServerEntries` / `registerMcpServer` / `unregisterMcpServer` / `registerMcpServersFromConfig` / `syncMcpServersForRuntime` | ~300 |
| `deep_adapter_a2x.go` | 新建 | A2X 客户端管理 5 方法 + Cron 上下文 2 方法（大部分 ⤵️） | ~200 |
| `deep_adapter_tools.go` | 新建 | Tool 同步 ~10 方法 + 多模态配置 5 方法 + paid search | ~400 |
| `deep_adapter_slash.go` | 新建 | Slash 命令 5 个（`/evolve` `/evolve_list` `/evolve_simplify` `/evolve_rebuild` `/evolve_rollback`）+ `_handleSlashCommand` + `_handleGovernanceApproval` | ~600 |
| `deep_adapter_evolution.go` | 新建 | EvolutionWatcher 3 方法 + `_handleEvolutionApproval` + recap 3 方法 + context compression 4 方法 + `_countFullContextTokens` | ~500 |
| `deep_adapter_team.go` | 新建 | TeamSkillApproval 5 方法 + team 模式分流辅助 + `_optionMatches` | ~300 |
| `deep_adapter_stream.go` | 新建 | `_parseStreamChunk`（15+ 种 chunk 类型）+ 流式执行 goroutine 辅助 + usage 累加器 + ask_user 去重 | ~400 |
| `deep_adapter_dreaming.go` | 新建 | `tryStartDreaming` / `tryStopDreaming`（⤵️ 10.6.13-18） | ~60 |
| `deep_adapter_config.go` | 新建 | `_updateRuntimeConfig` / `_RuntimeConfig` / Profile 检查 / Prompt 解析 / Subagent 构建 / `_writeRuntimeState` / `_resolveRuntimeLanguage` | ~400 |
| `interface.go` | 修改 | 新增 `ContextCompressor` / `DreamingController` / `GatewayDisconnectHandler` 可选接口 | +50 |
| `code_adapter.go` | 修改 | 适配新可选接口，委托新方法 | +30 |
| `deep_adapter_test.go` | 修改 | 补充测试 | +300 |
| `doc.go` | 修改 | 更新文件目录 | +20 |

### 5.2 非适配器文件修改

| 文件 | 操作 | 内容 |
|------|------|------|
| `IMPLEMENTATION_PLAN.md` | 修改 | 10.3.4-6 状态保持 ✅，更新产出描述 |

---

## 6. 核心结构体变更

### 6.1 instance 字段升级

```go
// 旧：
instance interface{}  // ⤵️ agentcore.DeepAgent

// 新：
instance *harness.DeepAgent  // 对齐 Python self._instance: DeepAgent
```

### 6.2 已实现 Rail 字段升级

```go
// 旧：
heartbeatRail interface{}  // ⤵️ 10.6.3-10: HeartbeatRail

// 新（已实现）：
heartbeatRail *rails.HeartbeatRail
```

### 6.3 未实现 Rail 字段保持 interface{}

```go
// 保持：
skillRail         interface{}  // ⤵️ 10.6.3-10: SkillUseRail
streamEventRail   interface{}  // ⤵️ 10.6.3-10: JiuClawStreamEventRail
subagentRail      interface{}  // ⤵️ 10.6.3-10: SubagentRail
// ... 其余 12 个
```

---

## 7. 核心方法实现要点

### 7.1 CreateInstance

完整 25 步对齐 Python，逐步组装 `hconfig.CreateDeepAgentParams`，最终调用 `harness.CreateDeepAgent(ctx, params)`。

```go
func (d *DeepAdapter) CreateInstance(ctx context.Context, configMap map[string]any, mode string, subMode string) error {
    // 步骤 1:  setCheckpoint()
    // 步骤 2:  dreamingMode 设置
    // 步骤 3:  instanceOverrides
    // 步骤 4:  loadDotenv
    // 步骤 5:  getConfig → configBase
    // 步骤 6:  refreshMultimodalConfigs(configBase)  ⤵️ 多模态
    // 步骤 7-8: 读取 react 配置段 → configCache
    // 步骤 9:  agentName（overrides → configCache）
    // 步骤 10: projectDir（overrides → configCache）
    // 步骤 11: workspaceDir（configCache → workspace.AgentRootDir()）
    // 步骤 12: createModel(configBase)
    // 步骤 13: tryInitA2xClient(configBase)          ⤵️ A2X
    // 步骤 14: agentCard = AgentCard{Name, ID}
    // 步骤 15: toolCards = getToolCards(agentCard.ID)
    // 步骤 16: railsList = buildAgentRails(config, configBase, mode)
    // 步骤 17: sysOperation = createSysOperation()
    // 步骤 18: subagents = buildConfiguredSubagents(...)
    // 步骤 19: d.instance = harness.CreateDeepAgent(ctx, params)
    // 步骤 20: d.instance.EnsureInitialized(ctx)
    // 步骤 21: seedRuntimeCwd(projectDir or workspaceDir)
    // 步骤 22: syncA2xRuntimeState()                ⤵️ A2X
    // 步骤 23: registeredMCPServerIDs = make(...)
    // 步骤 24: registerMcpServersFromConfig(...)     真实调用 ResourceMgr
    // 步骤 25: loadUserRails()                       ⤵️ 10.6.3-10
}
```

### 7.2 ProcessMessageStreamImpl

goroutine 从 `Runner.RunAgentStreaming()` 的 `<-chan stream.Schema` 读取，按 `chunk.Type` 分支处理，经 `_parseStreamChunk` 转换后写入 `<-chan *schema.AgentResponseChunk`。

```go
func (d *DeepAdapter) ProcessMessageStreamImpl(...) (<-chan *schema.AgentResponseChunk, error) {
    // 前置校验 + 模式分流 + 上下文设置（与 ProcessMessageImpl 共享）
    
    rawCh, err := runner.RunAgentStreaming(ctx, d.instance, inputs)
    outCh := make(chan *schema.AgentResponseChunk, 64)
    
    go func() {
        defer close(outCh)
        // usage 累加器
        // accumulatedText / accumulatedReasoning
        // askUser 去重集合
        
        for raw := range rawCh {
            // 按 raw.Type 分支：
            //   "llm_usage"    → 累加 usage, yield chat.usage_metadata
            //   "llm_reasoning" → yield chat.reasoning
            //   "llm_output"   → yield chat.delta
            //   "answer"       → parseStreamChunk → yield
            //   其他           → parseStreamChunk → yield
        }
        
        // yield 累积文本/reasoning
        // 启动 evolution watcher               ⤵️
        // yield chat.usage_summary
        // yield is_complete=true
    }()
    
    // finally: 清理上下文（通过 defer 在 goroutine 内处理）
    return outCh, nil
}
```

### 7.3 _parseStreamChunk

完整对齐 Python `_parse_stream_chunk`（line 4981-5294），处理 15+ 种 chunk 类型：

| chunk.Type | 处理 | 输出 payload.event_type |
|------------|------|------------------------|
| `controller_output` | 内部 type 分发（task_completion→跳过, task_failed→error） | — / `chat.error` |
| `llm_output` | 提取 content | `chat.delta` |
| `llm_reasoning` | 提取 reasoning content | `chat.reasoning` |
| `content_chunk` | 直接透传 content | `chat.delta` |
| `answer` | 完整响应 | `chat.final` |
| `tool_call` | 工具调用开始 | `chat.tool_call` |
| `tool_update` | 工具进度更新 | `chat.tool_update` |
| `tool_result` | 工具执行结果 | `chat.tool_result` |
| `error` | 错误 | `chat.error` |
| `thinking` | 思考过程 | `chat.thinking` |
| `todo.updated` | TODO 状态变更 | `todo.updated` |
| `context.usage` | 上下文用量 | `chat.context_usage` |
| `context.compression_state` | 压缩状态 | `chat.context_compression_state` |
| `ask_user_question` | HITL 交互 | `chat.ask_user_question` |
| `__interaction__` | 交互事件 | `chat.interaction` |
| `message` / `stage_result` / `extension_ready` / `harness_session_finished` / `activate_testing_guide` | 各自处理 | 对应 event_type |

---

## 8. 回填依赖图

```
10.3.6 DeepAdapter（本步骤）
  ├── 核心执行链路（全量真实调用）     ✅
  │   ├── CreateInstance → harness.CreateDeepAgent
  │   ├── ReloadAgentConfig → DeepAgent.ConfigureDeepConfig
  │   ├── ProcessMessage → Runner.RunAgent / RunAgentStreaming
  │   ├── ProcessInterrupt → DeepAgent.Abort
  │   ├── parseStreamChunk → OutputSchema → AgentResponseChunk
  │   ├── MCP 管理 → ResourceMgr.AddMcpServer/RemoveMcpServer
  │   └── Tool 同步 → AbilityManager.Add/Remove
  │
  ├── ⤵️ 10.6.3-10 Swarm Rails
  │   ├── 15 个未实现 Rail builder（interface{} 占位）
  │   ├── _update_rails_for_mode
  │   ├── _update_tools_for_mode
  │   ├── _update_prompt_for_mode
  │   ├── _update_permission_rail
  │   ├── load_user_rails
  │   ├── Slash 命令（依赖 SkillEvolutionRail）
  │   ├── EvolutionWatcher（依赖 SkillEvolutionRail）
  │   └── TeamSkillApproval（依赖 TeamSkillEvolutionRail）
  │
  ├── ⤵️ A2X
  │   └── initA2xClient / tryInitA2xClient / closeA2xClient / syncA2xRuntimeState
  │
  ├── ⤵️ 11.10 Cron
  │   └── bindRuntimeCronContext / resetRuntimeCronContext
  │
  ├── ⤵️ 10.6.11-12 AutoHarness
  │   └── autoHarnessService 分流
  │
  ├── ⤵️ 10.6.13-18 Swarm Memory
  │   └── tryStartDreaming / tryStopDreaming
  │
  ├── ⤵️ SessionHistory(JSONL)（10.3.17）
  │   └── compressContext / getContextUsage / generateRecap / getRecentMessages
  │
  └── ⤵️ 10.1.8 PermissionContext
      └── setupPermissionContext / cleanupPermissionContext
```

---

## 9. Python 方法 ↔ Go 方法映射

### 9.1 核心接口方法

| Python | Go | 文件 |
|--------|-----|------|
| `create_instance()` | `CreateInstance()` | `deep_adapter.go` |
| `reload_agent_config()` | `ReloadAgentConfig()` | `deep_adapter.go` |
| `process_message_impl()` | `ProcessMessageImpl()` | `deep_adapter.go` |
| `process_message_stream_impl()` | `ProcessMessageStreamImpl()` | `deep_adapter.go` |
| `process_interrupt()` | `ProcessInterrupt()` | `deep_adapter.go` |
| `handle_user_answer()` | `HandleUserAnswer()` | `deep_adapter.go` |
| `handle_heartbeat()` | `HandleHeartbeat()` | `deep_adapter.go` |
| `cleanup()` | `Cleanup()` | `deep_adapter.go` |

### 9.2 Rail Builder

| Python | Go | 文件 |
|--------|-----|------|
| `_build_heartbeat_rail()` | `buildHeartbeatRail()` | `deep_adapter_rails.go` |
| `_build_task_planning_rail()` | `buildTaskPlanningRail()` | `deep_adapter_rails.go` |
| `_build_filesystem_rail()` | `buildFilesystemRail()` | `deep_adapter_rails.go` |
| `_build_skill_rail()` | `buildSkillRail()` | `deep_adapter_rails.go` |
| `_build_skill_evolution_rail()` | `buildSkillEvolutionRail()` | `deep_adapter_rails.go` |
| `_build_skill_create_rail()` | `buildSkillCreateRail()` | `deep_adapter_rails.go` |
| `_build_stream_event_rail()` | `buildStreamEventRail()` | `deep_adapter_rails.go` |
| `_build_subagent_rail()` | `buildSubagentRail()` | `deep_adapter_rails.go` |
| `_build_security_rail()` | `buildSecurityRail()` | `deep_adapter_rails.go` |
| `_build_memory_rail()` | `buildMemoryRail()` | `deep_adapter_rails.go` |
| `_build_external_memory_rail()` | `buildExternalMemoryRail()` | `deep_adapter_rails.go` |
| `_build_avatar_rail()` | `buildAvatarRail()` | `deep_adapter_rails.go` |
| `_build_runtime_prompt_rail()` | `buildRuntimePromptRail()` | `deep_adapter_rails.go` |
| `_build_response_prompt_rail()` | `buildResponsePromptRail()` | `deep_adapter_rails.go` |
| `_build_context_assemble_rail()` | `buildContextAssembleRail()` | `deep_adapter_rails.go` |
| `_build_context_processor_rail()` | `buildContextProcessorRail()` | `deep_adapter_rails.go` |
| `_build_agent_rails()` | `buildAgentRails()` | `deep_adapter_rails.go` |
| `_update_rails_for_mode()` | `updateRailsForMode()` | `deep_adapter_rails.go` |
| `_update_prompt_for_mode()` | `updatePromptForMode()` | `deep_adapter_rails.go` |

### 9.3 MCP / A2X / Tools / Config / Stream / Slash / Evolution / Team / Dreaming

（按文件对应关系，同上表格逻辑，此处省略以避免冗余。每个 Python 方法名加 Go 驼峰命名映射，一对一对应。）

---

## 10. 测试策略

### 10.1 核心接口方法测试

| 测试函数 | 覆盖内容 |
|---------|---------|
| `TestDeepAdapter_CreateInstance_完整流程` | 25 步执行，验证 instance 非空 |
| `TestDeepAdapter_CreateInstance_配置缺失` | 无 model 配置时返回错误 |
| `TestDeepAdapter_ReloadAgentConfig_完整流程` | 13 步执行，验证 instance 重新配置 |
| `TestDeepAdapter_ProcessMessageImpl_完整流程` | Runner 调用返回 AgentResponse |
| `TestDeepAdapter_ProcessMessageStreamImpl_完整流程` | goroutine 从 channel 读取 chunk |
| `TestDeepAdapter_ProcessMessageStreamImpl_多chunk类型` | llm_output/llm_reasoning/tool_call/answer 等 |
| `TestDeepAdapter_ProcessInterrupt_四种intent` | pause/resume/cancel/supplement |
| `TestDeepAdapter_HandleUserAnswer_三种前缀` | team_skill_evolve_ / evolve_simplify_ / skill_evolve_ |
| `TestDeepAdapter_ParseStreamChunk_15种类型` | 每种 chunk.Type 的转换逻辑 |

### 10.2 MCP / Tool 同步测试

| 测试函数 | 覆盖内容 |
|---------|---------|
| `TestBuildMcpServerConfig` | 从配置条目构建 McpServerConfig |
| `TestRegisterMcpServer` | 注册到 ResourceMgr |
| `TestSyncMcpServersForRuntime` | 热同步：新增/移除/不变 |
| `TestSyncToolGroup` | 工具组热更新 |
| `TestBuildVisionModelConfig` | 多模态配置构建 |

### 10.3 可选接口测试

| 测试函数 | 覆盖内容 |
|---------|---------|
| `TestDeepAdapter_实现ContextCompressor` | 编译期检查 |
| `TestDeepAdapter_实现DreamingController` | 编译期检查 |
| `TestDeepAdapter_实现GatewayDisconnectHandler` | 编译期检查 |

### 10.4 CodeAdapter 委托测试

| 测试函数 | 覆盖内容 |
|---------|---------|
| `TestCodeAdapter_新接口委托` | ContextCompressor/DreamingController/GatewayDisconnectHandler 委托 |

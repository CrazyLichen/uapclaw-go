# 10.3.6 DeepAdapter 完整实现计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 将 DeepAdapter 从骨架升级为完整实现，全量对齐 Python `interface_deep.py`（6047 行），依赖模块未实现的部分用 `⤵️` 回填注释标记。

**Architecture:** DeepAdapter 核心执行链路（CreateInstance/ReloadAgentConfig/ProcessMessage/ProcessInterrupt）通过真实 agentcore 调用实现；未实现的 Rail/A2X/Cron 等用 `interface{}` + `⤵️` 标记。按职责拆为 11 个 Go 源文件（同包 adapter），新增 3 个可选接口（ContextCompressor/DreamingController/GatewayDisconnectHandler）。

**Tech Stack:** Go 1.23+, agentcore/harness (DeepAgent/Runner), agentcore/foundation/llm (Model), agentcore/session/stream (OutputSchema), agentcore/runner/resources_manager (ResourceMgr/MCP), internal/swarm/schema (AgentRequest/AgentResponse/AgentResponseChunk)

**Spec:** `docs/superpowers/specs/2026-11-14-deep-adapter-10.3.6-full-impl-design.md`

---

## 文件结构

| 操作 | 文件路径 | 职责 |
|------|---------|------|
| 修改 | `internal/swarm/server/adapter/interface.go` | 新增 3 个可选接口 |
| 修改 | `internal/swarm/server/adapter/deep_adapter.go` | 核心结构体升级 + 8 接口方法完整实现 |
| 新建 | `internal/swarm/server/adapter/deep_adapter_rails.go` | ~20 个 rail builder + 模式切换 |
| 新建 | `internal/swarm/server/adapter/deep_adapter_mcp.go` | MCP 管理 6 方法 |
| 新建 | `internal/swarm/server/adapter/deep_adapter_a2x.go` | A2X 客户端 + Cron 上下文 |
| 新建 | `internal/swarm/server/adapter/deep_adapter_tools.go` | Tool 同步 + 多模态配置 |
| 新建 | `internal/swarm/server/adapter/deep_adapter_slash.go` | Slash 命令 5 个 + governance approval |
| 新建 | `internal/swarm/server/adapter/deep_adapter_evolution.go` | EvolutionWatcher + evolution approval + recap + context compression |
| 新建 | `internal/swarm/server/adapter/deep_adapter_team.go` | TeamSkillApproval + team 分流 |
| 新建 | `internal/swarm/server/adapter/deep_adapter_stream.go` | parseStreamChunk + 流式 goroutine 辅助 |
| 新建 | `internal/swarm/server/adapter/deep_adapter_dreaming.go` | Dreaming 启停（⤵️） |
| 新建 | `internal/swarm/server/adapter/deep_adapter_config.go` | updateRuntimeConfig + Profile/Prompt 解析 + Subagent 构建 |
| 修改 | `internal/swarm/server/adapter/code_adapter.go` | 适配新可选接口委托 |
| 修改 | `internal/swarm/server/adapter/deep_adapter_test.go` | 补充测试 |
| 修改 | `internal/swarm/server/adapter/doc.go` | 更新文件目录 |
| 修改 | `IMPLEMENTATION_PLAN.md` | 更新 10.3.4-6 产出描述 |

---

### Task 1: 新增 3 个可选接口到 interface.go

**Files:**
- Modify: `internal/swarm/server/adapter/interface.go`

- [ ] **Step 1: 在 interface.go 末尾添加 ContextCompressor / DreamingController / GatewayDisconnectHandler 接口定义**

在 `interface.go` 文件末尾（AgentAdapter 接口定义之后）添加：

```go
// ──────────────────────────── 可选接口 ────────────────────────────

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

- [ ] **Step 2: 验证编译**

Run: `cd /home/opensource/uapclaw-gateway && go build ./internal/swarm/server/adapter/...`

- [ ] **Step 3: 提交**

```bash
git add internal/swarm/server/adapter/interface.go
git commit -m "feat(adapter): 新增 ContextCompressor/DreamingController/GatewayDisconnectHandler 可选接口"
```

---

### Task 2: 升级 deep_adapter.go 结构体和核心方法

**Files:**
- Modify: `internal/swarm/server/adapter/deep_adapter.go`

这是最核心的 Task。需要：
1. 将 `instance` 字段从 `interface{}` 升级为 `*harness.DeepAgent`
2. 升级已实现 Rail 字段类型（heartbeatRail → `*rails.HeartbeatRail`）
3. 完整实现 `CreateInstance` 25 步（组装 `CreateDeepAgentParams`，调 `harness.CreateDeepAgent`）
4. 完整实现 `ReloadAgentConfig` 13 步（调 `DeepAgent.ConfigureDeepConfig`）
5. 完整实现 `ProcessMessageImpl`（调 `Runner.RunAgent`）
6. 完整实现 `ProcessMessageStreamImpl`（goroutine + `Runner.RunAgentStreaming`）
7. 完整实现 `ProcessInterrupt`（调 `DeepAgent.Abort`）
8. 实现 `AbortOnGatewayDisconnect`
9. 更新 `Cleanup` 调用 A2X 关闭

由于此 Task 代码量极大（~1500 行修改），按子步骤拆分：

- [ ] **Step 1: 升级 instance 字段类型**

将 `deep_adapter.go` 中：
```go
instance interface{}
```
改为：
```go
instance *harness.DeepAgent
```

同时在 import 中添加 `harness` 包的导入（已有部分 import，需添加 `"github.com/uapclaw/uapclaw-go/internal/agentcore/harness"`）。

更新所有 `d.instance == nil` 的 nil 检查保持不变（`*harness.DeepAgent` 也可 nil 比较）。

- [ ] **Step 2: 升级已实现 Rail 字段类型**

将 `heartbeatRail` 从 `interface{}` 改为 `*rails.HeartbeatRail`（rails 包已在 import 中）。

- [ ] **Step 3: 重写 CreateInstance — 组装 CreateDeepAgentParams 并调用工厂**

完整 25 步对齐 Python `create_instance()` (line 2527-2621)。关键变更：
- 步骤 1-12: 保持现有逻辑（setCheckpoint/loadDotenv/getConfig/createModel 等）
- 步骤 13: 调用 `tryInitA2xClient` (⤵️ A2X)
- 步骤 14: 构建 `AgentCard`
- 步骤 15: 调用 `getToolCards` (⤵️ agentcore)
- 步骤 16: 调用 `buildAgentRails`（将在 Task 3 实现，此处先调方法签名）
- 步骤 17: 调用 `createSysOperation`（复用 factory.go 的 `buildSysOperation`）
- 步骤 18: 调用 `buildConfiguredSubagents`（将在 Task 11 实现）
- **步骤 19: 组装 `CreateDeepAgentParams` 并调用 `harness.CreateDeepAgent(ctx, params)`**
- 步骤 20: `d.instance.EnsureInitialized(ctx)`
- 步骤 21-25: 保持现有逻辑

步骤 19 的核心代码：
```go
params := hconfig.CreateDeepAgentParams{
    Card:            agentCard,
    Model:           d.model,
    SystemPrompt:    systemPrompt,
    ToolCards:       toolCards,
    Rails:           railsList,
    Subagents:       subagents,
    Workspace:       workspaceObj,
    SysOperation:    sysOperation,
    EnableTaskLoop:  enableTaskLoop,
    MaxIterations:   maxIterations,
    Language:        resolvedLanguage,
    Skills:          skills,
    Mcps:            mcpConfigs,
    VisionModelConfig:  d.visionModelConfig,
    AudioModelConfig:   d.audioModelConfig,
    EnableTaskPlanning:  enableTaskPlanning,
    PromptMode:         promptMode,
}
agent, err := harness.CreateDeepAgent(ctx, params)
if err != nil {
    return fmt.Errorf("CreateDeepAgent 失败: %w", err)
}
d.instance = agent
```

- [ ] **Step 4: 重写 ReloadAgentConfig — 完整 13 步**

对齐 Python `reload_agent_config()` (line 2646-2752)。核心：
- 步骤 1-4: 重新读取配置
- 步骤 5: `refreshMultimodalConfigs`
- 步骤 6-7: 重建模型 `createModel`
- 步骤 8: `getCurrentAgentRails`
- 步骤 9: `getToolCards`
- 步骤 10: `updatePermissionRail` (⤵️)
- 步骤 11: `d.instance.ConfigureDeepConfig(ctx, newDeepConfig)` — **真实调用**
- 步骤 12-13: 重新注册 MCP

- [ ] **Step 5: 重写 ProcessMessageImpl — Runner.RunAgent**

对齐 Python `process_message_impl()` (line 4409-4512)。核心：
- 保留前置校验
- 添加 slash 命令处理 (⤵️ 10.6.3-10)
- 添加 cron/permission 上下文 (⤵️ 11.10/10.1.8)
- **`result, err := runner.RunAgent(ctx, d.instance, inputs)`** — 真实调用
- 构造 `AgentResponse` 返回

- [ ] **Step 6: 重写 ProcessMessageStreamImpl — goroutine + Runner.RunAgentStreaming**

对齐 Python `process_message_stream_impl()` (line 4514-4979)。核心：
- 前置校验 + 模式分流 (team/auto_harness/slash)
- **`rawCh, err := runner.RunAgentStreaming(ctx, d.instance, inputs)`**
- 启动 goroutine：`for raw := range rawCh { ... }` 按 chunk.Type 分支处理
- goroutine 内调 `parseStreamChunk`（Task 9 实现）转换
- 写入 `outCh chan *schema.AgentResponseChunk`
- finally 清理

- [ ] **Step 7: 增强 ProcessInterrupt — DeepAgent.Abort**

对齐 Python `process_interrupt()` (line 3268-3578)。增强：
- supplement/cancel 分支中调 `d.instance.Abort(ctx)` 当 `otherActiveSessions == 0`
- 添加 `streamEventRail` 调用 (⤵️ 10.6.3-10)

- [ ] **Step 8: 实现 AbortOnGatewayDisconnect**

对齐 Python `abort_on_gateway_disconnect()` (line 3539-3578)。遍历所有 active session，对每个执行 `d.instance.Abort(ctx)` + `unmarkSessionActive`。

- [ ] **Step 9: 更新 Cleanup**

调 `closeA2xClient()` (⤵️ A2X)，日志记录。

- [ ] **Step 10: 验证编译**

Run: `cd /home/opensource/uapclaw-gateway && go build ./internal/swarm/server/adapter/...`

- [ ] **Step 11: 提交**

```bash
git add internal/swarm/server/adapter/deep_adapter.go
git commit -m "feat(adapter): DeepAdapter 核心方法完整实现——CreateInstance/ReloadAgentConfig/ProcessMessage/ProcessInterrupt/AbortOnGatewayDisconnect"
```

---

### Task 3: 新建 deep_adapter_rails.go — Rail Builder

**Files:**
- Create: `internal/swarm/server/adapter/deep_adapter_rails.go`

对齐 Python 的 ~20 个 `_build_*_rail` 方法 (line 1632-2212)。

- [ ] **Step 1: 实现已有的 Rail builder（HeartbeatRail/TaskPlanningRail/SysOperationRail）**

这 3 个 Rail 已有 Go 实现，builder 直接返回真实类型：

```go
func (d *DeepAdapter) buildHeartbeatRail() *rails.HeartbeatRail {
    return rails.NewHeartbeatRail()
}
func (d *DeepAdapter) buildTaskPlanningRail(config map[string]any, language string) *rails.TaskPlanningRail {
    // 对齐 Python _build_task_planning_rail()
    return rails.NewTaskPlanningRail(...)
}
func (d *DeepAdapter) buildFilesystemRail() *rails.SysOperationRail {
    return rails.NewSysOperationRail()
}
```

- [ ] **Step 2: 实现未实现 Rail builder 骨架（15 个，interface{} + ⤵️ 标记）**

每个方法写完整签名和步骤注释，返回 `interface{}` + `⤵️ 10.6.3-10` 标记：
- `buildSkillRail` / `buildSkillEvolutionRail` / `buildSkillCreateRail`
- `buildStreamEventRail` / `buildSubagentRail` / `buildSecurityRail`
- `buildMemoryRail` / `buildExternalMemoryRail` / `buildAvatarRail`
- `buildRuntimePromptRail` / `buildResponsePromptRail`
- `buildContextAssembleRail` / `buildContextProcessorRail`

- [ ] **Step 3: 实现 buildAgentRails 主方法**

对齐 Python `_build_agent_rails()` (line 2116-2212)。调用步骤 1-2 的所有 builder，组装 `[]agentinterfaces.AgentRail` 列表。

- [ ] **Step 4: 实现 updateRailsForMode / updatePromptForMode**

对齐 Python `_update_rails_for_mode()` (line 2754-2896) 和 `_update_prompt_for_mode()` (line 3091-3097)。按 mode 值分支注册/注销 Rail。

- [ ] **Step 5: 验证编译并提交**

```bash
go build ./internal/swarm/server/adapter/...
git add internal/swarm/server/adapter/deep_adapter_rails.go
git commit -m "feat(adapter): DeepAdapter rail builder 完整骨架——20 个 builder + buildAgentRails + updateRailsForMode"
```

---

### Task 4: 新建 deep_adapter_mcp.go — MCP 管理

**Files:**
- Create: `internal/swarm/server/adapter/deep_adapter_mcp.go`

对齐 Python MCP 管理 (line 972-1168)。**全部可完整实现**，因为 `McpServerConfig` 和 `ResourceMgr` 都已有。

- [ ] **Step 1: 实现 buildMcpServerConfig + extractEnabledMcpServerEntries**

对齐 Python `_build_mcp_server_config()` / `_extract_enabled_mcp_server_entries()`。从 config 条目构建 `*mcptypes.McpServerConfig`。

- [ ] **Step 2: 实现 registerMcpServer + unregisterMcpServer**

对齐 Python `_register_mcp_server()` / `_unregister_mcp_server()`。调 `runner.GetResourceMgr().AddMcpServer()` / `RemoveMcpServer()`。

- [ ] **Step 3: 实现 registerMcpServersFromConfig + syncMcpServersForRuntime**

对齐 Python `_register_mcp_servers_from_config()` / `_sync_mcp_servers_for_runtime()`。热同步逻辑（to_remove/to_add/to_check）。

- [ ] **Step 4: 验证编译并提交**

```bash
go build ./internal/swarm/server/adapter/...
git add internal/swarm/server/adapter/deep_adapter_mcp.go
git commit -m "feat(adapter): DeepAdapter MCP 管理完整实现——buildMcpServerConfig/registerMcpServer/syncMcpServersForRuntime"
```

---

### Task 5: 新建 deep_adapter_a2x.go — A2X 客户端 + Cron 上下文

**Files:**
- Create: `internal/swarm/server/adapter/deep_adapter_a2x.go`

对齐 Python A2X (line 612-706) + Cron (line 2719-2752)。**大部分 ⤵️**。

- [ ] **Step 1: 实现 A2X 客户端管理骨架**

5 个方法全部用 ⤵️ 标记：`clearA2xRuntimeState` / `closeA2xClient` / `initA2xClient` / `tryInitA2xClient` / `syncA2xRuntimeState`。

- [ ] **Step 2: 实现 Cron 上下文骨架**

`bindRuntimeCronContext` / `resetRuntimeCronContext` — ⤵️ 11.10。

- [ ] **Step 3: 验证编译并提交**

```bash
go build ./internal/swarm/server/adapter/...
git add internal/swarm/server/adapter/deep_adapter_a2x.go
git commit -m "feat(adapter): DeepAdapter A2X 客户端 + Cron 上下文骨架（⤵️ A2X/11.10）"
```

---

### Task 6: 新建 deep_adapter_tools.go — Tool 同步 + 多模态配置

**Files:**
- Create: `internal/swarm/server/adapter/deep_adapter_tools.go`

对齐 Python Tool 同步 (line 1319-1476) + 多模态配置 (line 1170-1318)。**全部可完整实现**。

- [ ] **Step 1: 实现 Tool 同步方法**

`syncToolGroup` / `removeRegisteredTools` / `appendToolCard` / `prioritizePaidSearchToolCard` / `pruneToolCards` / `syncMultimodalToolsForRuntime` / `syncPaidSearchToolForRuntime`。调 `AbilityManager.Add/Remove`。

- [ ] **Step 2: 实现多模态配置方法**

`buildVisionModelConfig` / `buildAudioModelConfig` / `buildVideoModelConfig` / `buildImageGenModelConfig` / `refreshMultimodalConfigs`。从配置/环境变量构建多模态配置。

- [ ] **Step 3: 验证编译并提交**

```bash
go build ./internal/swarm/server/adapter/...
git add internal/swarm/server/adapter/deep_adapter_tools.go
git commit -m "feat(adapter): DeepAdapter Tool 同步 + 多模态配置完整实现"
```

---

### Task 7: 新建 deep_adapter_slash.go — Slash 命令

**Files:**
- Create: `internal/swarm/server/adapter/deep_adapter_slash.go`

对齐 Python Slash 命令 (line 3769-4349)。**依赖 SkillEvolutionRail，大部分 ⤵️**。

- [ ] **Step 1: 实现 handleSlashCommand 主方法骨架**

对齐 Python `_handle_slash_command()` (line 3769-3830)。按 query 前缀 `/evolve*` 分发。

- [ ] **Step 2: 实现 5 个 slash 命令处理器骨架**

`handleEvolveCommand` / `handleEvolveListCommand` / `handleEvolveSimplifyCommand` / `handleEvolveRebuildCommand` / `handleEvolveRollbackCommand`。全部 ⤵️ 10.6.3-10。

- [ ] **Step 3: 实现 handleGovernanceApproval 骨架**

对齐 Python `_handle_governance_approval()` (line 4298-4349)。⤵️ 10.6.3-10。

- [ ] **Step 4: 验证编译并提交**

```bash
go build ./internal/swarm/server/adapter/...
git add internal/swarm/server/adapter/deep_adapter_slash.go
git commit -m "feat(adapter): DeepAdapter Slash 命令骨架（⤵️ 10.6.3-10 SkillEvolutionRail）"
```

---

### Task 8: 新建 deep_adapter_evolution.go — Evolution + Recap + Context Compression

**Files:**
- Create: `internal/swarm/server/adapter/deep_adapter_evolution.go`

对齐 Python EvolutionWatcher (line 5725-5923) + EvolutionApproval (line 3626-3648) + Recap (line 5572-5663) + Context Compression (line 5380-5570) + Token Counting (line 5665-5723)。

- [ ] **Step 1: 实现 EvolutionWatcher 骨架**

`watchEvolutionAndPush` / `onEvolutionWatcherDone` / `isApprovalEvent`。⤵️ 10.6.3-10。

- [ ] **Step 2: 实现 handleEvolutionApproval 骨架**

⤵️ 10.6.3-10。

- [ ] **Step 3: 实现 ContextCompressor 接口方法**

`CompressContext` / `GetContextUsage` / `GenerateRecap`。⤵️ SessionHistory(JSONL)。

- [ ] **Step 4: 实现 recap 辅助方法**

`getRecentMessages` / `callModelForRecap` / `countFullContextTokens`。⤵️ SessionHistory(JSONL)。

- [ ] **Step 5: 验证编译并提交**

```bash
go build ./internal/swarm/server/adapter/...
git add internal/swarm/server/adapter/deep_adapter_evolution.go
git commit -m "feat(adapter): DeepAdapter EvolutionWatcher + ContextCompressor + Recap 骨架（⤵️ SkillEvolutionRail/SessionHistory）"
```

---

### Task 9: 新建 deep_adapter_stream.go — Stream Chunk 解析

**Files:**
- Create: `internal/swarm/server/adapter/deep_adapter_stream.go`

对齐 Python `_parse_stream_chunk()` (line 4981-5294)。**全部可完整实现**（OutputSchema 已有）。

- [ ] **Step 1: 实现 parseStreamChunk 核心方法**

15+ 种 chunk.Type 分支处理，返回 `map[string]any`（payload dict）。对齐 Python 逐个类型处理逻辑。

- [ ] **Step 2: 实现 usage 累加器和 askUser 去重辅助**

```go
type usageAccumulator struct {
    InputTokens  int
    OutputTokens int
    TotalTokens  int
    InputCost    float64
    OutputCost   float64
    TotalCost    float64
}
```

- [ ] **Step 3: 验证编译并提交**

```bash
go build ./internal/swarm/server/adapter/...
git add internal/swarm/server/adapter/deep_adapter_stream.go
git commit -m "feat(adapter): DeepAdapter parseStreamChunk 完整实现——15+ 种 chunk 类型 + usage 累加器"
```

---

### Task 10: 新建 deep_adapter_team.go — Team Skill Approval

**Files:**
- Create: `internal/swarm/server/adapter/deep_adapter_team.go`

对齐 Python TeamSkillApproval (line 3651-3767)。**依赖 TeamSkillEvolutionRail，大部分 ⤵️**。

- [ ] **Step 1: 实现 team skill approval 方法骨架**

`findTeamSkillRail` / `handleTeamSkillEvolveApproval` / `pushTeamSkillEvolveResolutionStatus` / `optionMatches`。⤵️ 10.6.3-10。

- [ ] **Step 2: 实现 team 模式分流辅助骨架**

对齐 Python `process_team_message_stream` 调用。⤵️ 10.3.7-11 TeamHelpers。

- [ ] **Step 3: 验证编译并提交**

```bash
go build ./internal/swarm/server/adapter/...
git add internal/swarm/server/adapter/deep_adapter_team.go
git commit -m "feat(adapter): DeepAdapter TeamSkillApproval + team 分流骨架（⤵️ 10.6.3-10/10.3.7-11）"
```

---

### Task 11: 新建 deep_adapter_config.go — RuntimeConfig + Profile/Prompt/Subagent

**Files:**
- Create: `internal/swarm/server/adapter/deep_adapter_config.go`

对齐 Python RuntimeConfig (line 3098-3266) + Profile/Prompt (line 708-970) + Subagent (line 878-970)。

- [ ] **Step 1: 实现 RuntimeConfig dataclass + updateRuntimeConfig**

对齐 Python `_RuntimeConfig` / `_update_runtime_config()`。包含 CWD 种子、language/channel 解析、runtime state 写入、rail/tool 模式切换。

- [ ] **Step 2: 实现 Profile 检查方法**

`isAcpToolProfile` / `filesystemRailEnabledForProfile` / `skillIncludeToolsForProfile`。全部可完整实现。

- [ ] **Step 3: 实现 Prompt 解析方法**

`resolvePromptChannel` / `resolvePromptLanguage` / `resolveRuntimeLanguage` / `resolveModelName` / `writeRuntimeState`。全部可完整实现。

- [ ] **Step 4: 实现 Subagent 构建方法**

`buildConfiguredSubagents` / `isSubagentEnabled` / `isSubagentDefaultEnabled`。对齐 Python `_build_configured_subagents()` (line 878-970)。

- [ ] **Step 5: 验证编译并提交**

```bash
go build ./internal/swarm/server/adapter/...
git add internal/swarm/server/adapter/deep_adapter_config.go
git commit -m "feat(adapter): DeepAdapter RuntimeConfig + Profile/Prompt/Subagent 完整实现"
```

---

### Task 12: 新建 deep_adapter_dreaming.go — Dreaming 启停

**Files:**
- Create: `internal/swarm/server/adapter/deep_adapter_dreaming.go`

对齐 Python `try_start_dreaming` / `try_stop_dreaming` (line 5935-5965)。**⤵️ 10.6.13-18**。

- [ ] **Step 1: 实现 DreamingController 接口方法**

```go
func (d *DeepAdapter) TryStartDreaming(ctx context.Context, busyChecker func() bool) error {
    if d.dreamingStarted {
        return nil
    }
    // ⤵️ 10.6.13-18: 调用 swarm memory dreaming.startDreaming(...)
    logger.Info(logComponent).Str("mode", d.dreamingMode).Msg("tryStartDreaming 等待 10.6.13-18 回填")
    return nil
}

func (d *DeepAdapter) TryStopDreaming(ctx context.Context) error {
    if !d.dreamingStarted {
        return nil
    }
    // ⤵️ 10.6.13-18: 调用 swarm memory dreaming.stopDreaming(...)
    d.dreamingStarted = false
    logger.Info(logComponent).Msg("tryStopDreaming 等待 10.6.13-18 回填")
    return nil
}
```

- [ ] **Step 2: 验证编译并提交**

```bash
go build ./internal/swarm/server/adapter/...
git add internal/swarm/server/adapter/deep_adapter_dreaming.go
git commit -m "feat(adapter): DeepAdapter DreamingController 实现（⤵️ 10.6.13-18）"
```

---

### Task 13: 适配 CodeAdapter 新可选接口委托

**Files:**
- Modify: `internal/swarm/server/adapter/code_adapter.go`

- [ ] **Step 1: 添加 ContextCompressor / DreamingController / GatewayDisconnectHandler 委托方法**

```go
func (c *CodeAdapter) CompressContext(ctx context.Context, sessionID string, session any, returnState bool) (map[string]any, error) {
    return c.deep.CompressContext(ctx, sessionID, session, returnState)
}
func (c *CodeAdapter) GetContextUsage(ctx context.Context, sessionID string) (map[string]any, error) {
    return c.deep.GetContextUsage(ctx, sessionID)
}
func (c *CodeAdapter) GenerateRecap(ctx context.Context, sessionID string) (map[string]any, error) {
    return c.deep.GenerateRecap(ctx, sessionID)
}
func (c *CodeAdapter) TryStartDreaming(ctx context.Context, busyChecker func() bool) error {
    return c.deep.TryStartDreaming(ctx, busyChecker)
}
func (c *CodeAdapter) TryStopDreaming(ctx context.Context) error {
    return c.deep.TryStopDreaming(ctx)
}
func (c *CodeAdapter) AbortOnGatewayDisconnect(ctx context.Context) {
    c.deep.AbortOnGatewayDisconnect(ctx)
}
```

- [ ] **Step 2: 验证编译并提交**

```bash
go build ./internal/swarm/server/adapter/...
git add internal/swarm/server/adapter/code_adapter.go
git commit -m "feat(adapter): CodeAdapter 适配 ContextCompressor/DreamingController/GatewayDisconnectHandler 委托"
```

---

### Task 14: 补充测试

**Files:**
- Modify: `internal/swarm/server/adapter/deep_adapter_test.go`
- Modify: `internal/swarm/server/adapter/code_adapter_test.go`

- [ ] **Step 1: 添加 DeepAdapter 可选接口编译期检查测试**

```go
func TestDeepAdapter_实现ContextCompressor(t *testing.T) {
    var _ ContextCompressor = (*DeepAdapter)(nil)
}
func TestDeepAdapter_实现DreamingController(t *testing.T) {
    var _ DreamingController = (*DeepAdapter)(nil)
}
func TestDeepAdapter_实现GatewayDisconnectHandler(t *testing.T) {
    var _ GatewayDisconnectHandler = (*DeepAdapter)(nil)
}
```

- [ ] **Step 2: 添加 CodeAdapter 可选接口编译期检查测试**

```go
func TestCodeAdapter_实现ContextCompressor(t *testing.T) {
    var _ ContextCompressor = (*CodeAdapter)(nil)
}
func TestCodeAdapter_实现DreamingController(t *testing.T) {
    var _ DreamingController = (*CodeAdapter)(nil)
}
func TestCodeAdapter_实现GatewayDisconnectHandler(t *testing.T) {
    var _ GatewayDisconnectHandler = (*CodeAdapter)(nil)
}
```

- [ ] **Step 3: 添加 MCP 管理单元测试**

`TestBuildMcpServerConfig` / `TestExtractEnabledMcpServerEntries`。

- [ ] **Step 4: 添加 parseStreamChunk 单元测试**

`TestParseStreamChunk_llmOutput` / `TestParseStreamChunk_llmReasoning` / `TestParseStreamChunk_toolCall` / `TestParseStreamChunk_error` / `TestParseStreamChunk_answer` 等。

- [ ] **Step 5: 运行测试验证**

Run: `cd /home/opensource/uapclaw-gateway && go test -cover ./internal/swarm/server/adapter/...`

- [ ] **Step 6: 提交**

```bash
git add internal/swarm/server/adapter/deep_adapter_test.go internal/swarm/server/adapter/code_adapter_test.go
git commit -m "test(adapter): DeepAdapter/CodeAdapter 可选接口 + MCP + parseStreamChunk 单元测试"
```

---

### Task 15: 更新 doc.go 和 IMPLEMENTATION_PLAN.md

**Files:**
- Modify: `internal/swarm/server/adapter/doc.go`
- Modify: `IMPLEMENTATION_PLAN.md`

- [ ] **Step 1: 更新 adapter/doc.go 文件目录**

添加所有新建文件到文件目录树。

- [ ] **Step 2: 更新 IMPLEMENTATION_PLAN.md 10.3.4-6 产出描述**

更新产出列，描述 DeepAdapter 已完整实现（含 ⤵️ 回填标记）。

- [ ] **Step 3: 提交**

```bash
git add internal/swarm/server/adapter/doc.go IMPLEMENTATION_PLAN.md
git commit -m "docs(adapter): 更新 doc.go 文件目录 + IMPLEMENTATION_PLAN 10.3.4-6 产出描述"
```

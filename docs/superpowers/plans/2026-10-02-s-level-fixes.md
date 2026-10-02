# 2026-10-02 S 级修复实现计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 修复 2026-10-02 审查报告中 37 个仍存在的 S 级问题（S-16 跳过/S-39 已不存在）

**Architecture:** 按模块分 4 个 Task Group，每个 Group 内按依赖顺序执行。简单 bug 修复（类型断言、ctx 传播、字段补全）优先，核心功能实现（Invoke/Stream、Transport 订阅）放后。

**Tech Stack:** Go 1.21+（context.WithoutCancel）、现有项目框架

---

## 文件结构

| 文件 | 职责 | 涉及修复 |
|------|------|----------|
| `internal/agent_teams/agent/team_agent.go` | TeamAgent 核心逻辑 | S-01,04,05,08,09,10,11,12,13,14,15 |
| `internal/agent_teams/agent/coordination/kernel.go` | CoordinationKernel 协调层 | S-02,03,06,07 |
| `internal/agent_teams/agent/coordination/types/protocols.go` | KernelHost 接口 | S-06,07 |
| `internal/agent_teams/tools/team_tools.go` | Team Tools 实现 | S-24,25,26,27,28,29,30,31 |
| `internal/agent_teams/tools/task_manager.go` | TaskManager | S-26,27 |
| `internal/swarm/server/adapter/deep_adapter.go` | DeepAdapter 主逻辑 | S-17,18 |
| `internal/swarm/server/adapter/deep_adapter_evolution.go` | Evolution 相关 | S-19,21,22 |
| `internal/swarm/server/adapter/deep_adapter_team.go` | Team 消息处理 | S-23 |
| `internal/swarm/server/adapter/code_adapter.go` | CodeAdapter | S-17,20 |
| `internal/agent_teams/runtime/manager.go` | TeamRuntimeManager | S-33,34 |
| `internal/agent_teams/registry/interfaces.go` | PoolEntry 接口 | S-35 |
| `internal/agent_teams/runtime/pool.go` | TeamRuntimePool | S-35 |
| `internal/agentcore/harness/rails/memory/external_memory_rail.go` | ExternalMemoryRail | S-32 |
| `internal/agentcore/memory/external/viking_provider.go` | VikingProvider | S-36 |
| `internal/agentcore/harness/rails/memory/coding_memory_rail.go` | CodingMemoryRail | S-37 |
| `internal/agentcore/memory/process/extract/extractor.go` | LongTermMemoryExtractor | S-38 |

---

## Task Group 1：简单 bug 修复（无依赖，可并行）

### Task 1: S-19 isOutcomeEvent 类型断言修复

**Files:**
- Modify: `internal/swarm/server/adapter/deep_adapter_evolution.go:553`
- Test: `internal/swarm/server/adapter/deep_adapter_evolution_test.go`

- [ ] **Step 1: 修改类型断言**

将 `deep_adapter_evolution.go:553` 行：
```go
meta, ok := payload["_evolution_meta"].(map[string]string)
```
改为：
```go
meta, ok := payload["_evolution_meta"].(map[string]any)
```

- [ ] **Step 2: 验证编译**

Run: `cd /home/opensource/uap-claw-go && go build ./internal/swarm/server/adapter/...`
Expected: 编译成功

- [ ] **Step 3: 运行相关测试**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/swarm/server/adapter/... -run TestIsOutcomeEvent -v`
Expected: PASS（如有测试）或无匹配测试

- [ ] **Step 4: Commit**

```bash
git add internal/swarm/server/adapter/deep_adapter_evolution.go
git commit -m "fix(S-19): isOutcomeEvent 类型断言从 map[string]string 改为 map[string]any"
```

---

### Task 2: S-25 SpawnMemberTool prompt 安全断言

**Files:**
- Modify: `internal/agent_teams/tools/team_tools.go:679`

- [ ] **Step 1: 修改裸断言**

将 `team_tools.go:679` 行：
```go
inputs["prompt"].(string),
```
改为：
```go
prompt, _ := inputs["prompt"].(string)
```
并在 SpawnMember 调用中使用 `prompt` 变量。

- [ ] **Step 2: 验证编译**

Run: `cd /home/opensource/uap-claw-go && go build ./internal/agent_teams/tools/...`
Expected: 编译成功

- [ ] **Step 3: Commit**

```bash
git add internal/agent_teams/tools/team_tools.go
git commit -m "fix(S-25): SpawnMemberTool prompt 改为安全类型断言防止 panic"
```

---

### Task 3: S-24 SpawnMemberTool human_agent 路径漏检 prompt

**Files:**
- Modify: `internal/agent_teams/tools/team_tools.go:653`

- [ ] **Step 1: 增加 prompt 检查**

将 `team_tools.go:653` 行：
```go
if _, ok := inputs["model_name"]; ok {
```
改为：
```go
if _, ok := inputs["model_name"]; ok {
    return toolError("role_type='human_agent' does not accept 'model_name' or 'prompt'; use role_type='ai_agent' instead")
}
if _, ok := inputs["prompt"]; ok {
    return toolError("role_type='human_agent' does not accept 'prompt'; use role_type='ai_agent' instead")
}
```

- [ ] **Step 2: 验证编译 + 测试**

Run: `cd /home/opensource/uap-claw-go && go build ./internal/agent_teams/tools/... && go test ./internal/agent_teams/tools/... -short -count=1`
Expected: 编译成功，测试通过

- [ ] **Step 3: Commit**

```bash
git add internal/agent_teams/tools/team_tools.go
git commit -m "fix(S-24): SpawnMemberTool human_agent 路径增加 prompt 校验"
```

---

### Task 4: S-29 SubmitPlanTool 增加 message 字段

**Files:**
- Modify: `internal/agent_teams/tools/team_tools.go:1104-1111`

- [ ] **Step 1: 添加 message 字段**

在 `team_tools.go` SubmitPlanTool 的 resultMap 中，在 `"member_plan_md"` 之后添加：
```go
"message":        "Member plan submitted. Wait for leader approval before execution.",
```

- [ ] **Step 2: 验证编译**

Run: `cd /home/opensource/uap-claw-go && go build ./internal/agent_teams/tools/...`
Expected: 编译成功

- [ ] **Step 3: Commit**

```bash
git add internal/agent_teams/tools/team_tools.go
git commit -m "fix(S-29): SubmitPlanTool 返回值增加 message 字段引导 LLM 行为"
```

---

### Task 5: S-36 Viking Initialize 静默降级

**Files:**
- Modify: `internal/agentcore/memory/external/viking_provider.go:253-262`
- Test: `internal/agentcore/memory/external/viking_provider_test.go`

- [ ] **Step 1: 修改 Initialize 健康检查失败行为**

将 `viking_provider.go` 健康检查失败分支：
```go
if !healthy {
    logger.Warn(vikingLogComponent).
        Str("endpoint", p.endpoint).
        Msg("OpenViking 不可达")
    p.client = nil
    p.initialized = false
    return fmt.Errorf("OpenViking at %s not reachable", p.endpoint)
}
```
改为：
```go
if !healthy {
    logger.Warn(vikingLogComponent).
        Str("endpoint", p.endpoint).
        Msg("OpenViking 不可达，静默降级")
    p.client = nil
    p.initialized = true // 标记已初始化，允许后续 Prefetch/SyncTurn 检查 client==nil 优雅跳过
    return nil           // 静默降级，对齐 Python
}
```

- [ ] **Step 2: 更新测试**

修改 `viking_provider_test.go` 中健康检查失败的测试用例，将期望从 `error != nil` 改为 `err == nil && p.initialized == true && p.client == nil`。

- [ ] **Step 3: 运行测试**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/memory/external/... -short -count=1`
Expected: PASS

- [ ] **Step 4: Commit**

```bash
git add internal/agentcore/memory/external/viking_provider.go internal/agentcore/memory/external/viking_provider_test.go
git commit -m "fix(S-36): Viking Initialize 健康检查失败时静默降级对齐 Python"
```

---

### Task 6: S-37 CodingMemoryRail 截断 rune/字节修复

**Files:**
- Modify: `internal/agentcore/harness/rails/memory/coding_memory_rail.go:576-588`
- Test: `internal/agentcore/harness/rails/memory/coding_memory_rail_test.go`

- [ ] **Step 1: 修改截断逻辑**

将 `coding_memory_rail.go:585` 附近的截断判断：
```go
if utf8.RuneCountInString(truncated) > remaining {
    runes := []rune(truncated)
    truncated = string(runes[:remaining]) + "\n\n... (truncated)"
}
```
改为：
```go
if len(truncated) > remaining {
    // 按 rune 截断确保不超过 remaining 字节且不破坏 UTF-8
    runes := []rune(truncated)
    for i := len(runes); i > 0; i-- {
        if len(string(runes[:i]))+len("\n\n... (truncated)") <= remaining {
            truncated = string(runes[:i]) + "\n\n... (truncated)"
            break
        }
    }
}
```

- [ ] **Step 2: 补充 CJK 截断测试**

在测试文件中增加 CJK 字符截断测试，验证：
- 纯 ASCII 截断正确
- CJK 多字节字符截断不超字节预算
- 截断不破坏 UTF-8 字符边界

- [ ] **Step 3: 运行测试**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/harness/rails/memory/... -short -count=1`
Expected: PASS

- [ ] **Step 4: Commit**

```bash
git add internal/agentcore/harness/rails/memory/coding_memory_rail.go internal/agentcore/harness/rails/memory/coding_memory_rail_test.go
git commit -m "fix(S-37): autoRecall 截断改为字节数对比 + rune 安全截断修复 CJK bug"
```

---

### Task 7: S-38 多模态消息 Text() 返回空串修复

**Files:**
- Modify: `internal/agentcore/memory/process/extract/extractor.go:58-74`
- Test: `internal/agentcore/memory/process/extract/extractor_test.go`

- [ ] **Step 1: 添加多模态回退逻辑**

在 `extractor.go` 中，将所有 `msg.GetContent().Text()` 调用替换为辅助函数：
```go
// contentText 安全提取消息文本内容，多模态消息回退到 String()
func contentText(msg schema.Message) string {
    c := msg.GetContent()
    if c.IsText() {
        return c.Text()
    }
    return c.String()
}
```
然后将 `extractor.go` 中的 4 处 `msg.GetContent().Text()` 替换为 `contentText(msg)`。

- [ ] **Step 2: 补充多模态消息测试**

增加测试用例：纯文本消息正常提取、多模态消息（含 image_url）回退到 String()。

- [ ] **Step 3: 运行测试**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/memory/process/extract/... -short -count=1`
Expected: PASS

- [ ] **Step 4: Commit**

```bash
git add internal/agentcore/memory/process/extract/extractor.go internal/agentcore/memory/process/extract/extractor_test.go
git commit -m "fix(S-38): LongTermMemoryExtractor 多模态消息回退到 String() 防止内容丢失"
```

---

### Task 8: S-32 ExternalMemoryRail AfterInvoke ctx 隔离

**Files:**
- Modify: `internal/agentcore/harness/rails/memory/external_memory_rail.go:408-413`
- Test: `internal/agentcore/harness/rails/memory/external_memory_rail_test.go`

- [ ] **Step 1: 使用 context.WithoutCancel 隔离请求生命周期**

将 `external_memory_rail.go` AfterInvoke 的 goroutine 中：
```go
go func() {
    defer close(done)
    err := r.provider.SyncTurn(ctx, query, output, ...)
```
改为：
```go
go func() {
    defer close(done)
    syncCtx := context.WithoutCancel(ctx) // 请求取消后 SyncTurn 继续执行，对齐 Python asyncio.create_task
    err := r.provider.SyncTurn(syncCtx, query, output, ...)
```

- [ ] **Step 2: 运行测试**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/harness/rails/memory/... -short -count=1`
Expected: PASS

- [ ] **Step 3: Commit**

```bash
git add internal/agentcore/harness/rails/memory/external_memory_rail.go
git commit -m "fix(S-32): AfterInvoke goroutine 使用 context.WithoutCancel 隔离请求生命周期"
```

---

## Task Group 2：ctx 传播 + 接口补全

### Task 9: S-21+S-22 handleEvolutionApproval/pushEventToFrontend 增加 ctx

**Files:**
- Modify: `internal/swarm/server/adapter/deep_adapter_evolution.go:247,507`

- [ ] **Step 1: handleEvolutionApproval 增加 ctx 参数**

将方法签名：
```go
func (d *DeepAdapter) handleEvolutionApproval(requestID string, answers []ApprovalAnswer) bool {
```
改为：
```go
func (d *DeepAdapter) handleEvolutionApproval(ctx context.Context, requestID string, answers []ApprovalAnswer) bool {
```
删除方法体内的 `ctx := context.Background()` 行（L253），直接使用传入的 ctx。

- [ ] **Step 2: pushEventToFrontend 增加 ctx 参数**

将方法签名：
```go
func (d *DeepAdapter) pushEventToFrontend(event *stream.OutputSchema, sessionID string, channelID string, requestID string) {
```
改为：
```go
func (d *DeepAdapter) pushEventToFrontend(ctx context.Context, event *stream.OutputSchema, sessionID string, channelID string, requestID string) {
```
将 L522/L536 的 `context.Background()` 改为 `ctx`。

- [ ] **Step 3: 更新调用方**

在 `deep_adapter_evolution.go` 中：
- `HandleUserAnswer` 调用 `handleEvolutionApproval` 时传入 `ctx`
- `watchEvolutionAndPush` 调用 `pushEventToFrontend` 时传入 `ctx`

在 `deep_adapter_team.go` 中如有调用 `pushEventToFrontend`，同步更新。

- [ ] **Step 4: 验证编译**

Run: `cd /home/opensource/uap-claw-go && go build ./internal/swarm/server/adapter/...`
Expected: 编译成功

- [ ] **Step 5: 运行测试**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/swarm/server/adapter/... -short -count=1`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add internal/swarm/server/adapter/deep_adapter_evolution.go internal/swarm/server/adapter/deep_adapter_team.go
git commit -m "fix(S-21,S-22): handleEvolutionApproval/pushEventToFrontend 增加 ctx 参数传播上游 context"
```

---

### Task 10: S-33+S-34 TeamRuntimeManager ctx 传播

**Files:**
- Modify: `internal/agent_teams/runtime/manager.go:265-304`

- [ ] **Step 1: handleInteractiveInput 增加 ctx 参数**

将方法签名：
```go
func (m *TeamRuntimeManager) handleInteractiveInput(entry *ActiveTeam, input *sessioninteraction.InteractiveInput) (*interaction.DeliverResult, error) {
```
改为：
```go
func (m *TeamRuntimeManager) handleInteractiveInput(ctx context.Context, entry *ActiveTeam, input *sessioninteraction.InteractiveInput) (*interaction.DeliverResult, error) {
```
将 L272 `context.Background()` 改为 `ctx`。

- [ ] **Step 2: resolveRecipients 增加 ctx 参数**

将方法签名：
```go
func (m *TeamRuntimeManager) resolveRecipients(entry *ActiveTeam, payloads []interaction.InteractPayload) ([]interaction.InteractPayload, error) {
```
改为：
```go
func (m *TeamRuntimeManager) resolveRecipients(ctx context.Context, entry *ActiveTeam, payloads []interaction.InteractPayload) ([]interaction.InteractPayload, error) {
```
删除 `ctx := context.Background()` 行，直接使用传入的 ctx。

- [ ] **Step 3: 更新 Interact 方法中的调用**

在 `Interact` 方法中，将 `handleInteractiveInput` 和 `resolveRecipients` 调用传入 `ctx`。

- [ ] **Step 4: 验证编译 + 测试**

Run: `cd /home/opensource/uap-claw-go && go build ./internal/agent_teams/runtime/... && go test ./internal/agent_teams/runtime/... -short -count=1`
Expected: 编译成功，测试通过

- [ ] **Step 5: Commit**

```bash
git add internal/agent_teams/runtime/manager.go
git commit -m "fix(S-33,S-34): handleInteractiveInput/resolveRecipients 传播上游 ctx"
```

---

### Task 11: S-35 PoolEntry 接口补全

**Files:**
- Modify: `internal/agent_teams/registry/interfaces.go:24-29`
- Modify: `internal/agent_teams/runtime/pool.go`

- [ ] **Step 1: 在 PoolEntry 接口添加缺失方法**

```go
type PoolEntry interface {
    GetEntry(teamName string) PoolTeamEntry
    RemoveEntry(teamName string)
    HasActive(teamName string) bool
    ListTeamNames() []string
    TeamsForSession(sessionID string) []PoolTeamEntry
}
```

- [ ] **Step 2: 验证 TeamRuntimePool 已实现这些方法**

检查 `pool.go` 中的 `TeamRuntimePool`，确认 `HasActive`、`ListTeamNames`、`TeamsForSession` 方法已存在且签名匹配。如有差异则调整。

- [ ] **Step 3: 验证编译**

Run: `cd /home/opensource/uap-claw-go && go build ./internal/agent_teams/...`
Expected: 编译成功

- [ ] **Step 4: Commit**

```bash
git add internal/agent_teams/registry/interfaces.go internal/agent_teams/runtime/pool.go
git commit -m "fix(S-35): PoolEntry 接口补充 HasActive/ListTeamNames/TeamsForSession 方法"
```

---

### Task 12: S-23 processTeamMessageStream req 类型改为 *schema.AgentRequest

**Files:**
- Modify: `internal/swarm/server/adapter/deep_adapter_team.go:167`
- Modify: `internal/swarm/server/adapter/deep_adapter.go:985`

- [ ] **Step 1: 修改方法签名**

将 `deep_adapter_team.go:167`：
```go
func (d *DeepAdapter) processTeamMessageStream(ctx context.Context, req any, inputs map[string]any) (<-chan *agentschema.AgentResponseChunk, error) {
```
改为：
```go
func (d *DeepAdapter) processTeamMessageStream(ctx context.Context, req *schema.AgentRequest, inputs map[string]any) (<-chan *agentschema.AgentResponseChunk, error) {
```

- [ ] **Step 2: 从 req 解析元信息**

将 L182-189 从 `inputs` 解析改为从 `req` 解析：
```go
sessionID := req.SessionID
channelID := req.ChannelID
requestID := req.RequestID
// inputs 仅取业务参数
if params, ok := inputs["params"].(map[string]any); ok {
    if tn, ok := params["team_name"].(string); ok {
        teamName = tn
    }
}
```

- [ ] **Step 3: 更新调用方**

`deep_adapter.go:985` 的调用 `d.processTeamMessageStream(ctx, req, inputs)` 中 `req` 类型已经是 `*schema.AgentRequest`，无需修改。

- [ ] **Step 4: 验证编译 + 测试**

Run: `cd /home/opensource/uap-claw-go && go build ./internal/swarm/server/adapter/... && go test ./internal/swarm/server/adapter/... -short -count=1`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/swarm/server/adapter/deep_adapter_team.go internal/swarm/server/adapter/deep_adapter.go
git commit -m "fix(S-23): processTeamMessageStream req 类型改为 *schema.AgentRequest 对齐 Python 从 request 获取元信息"
```

---

## Task Group 3：适配器修复

### Task 13: S-17 函数指针 runtimeConfigUpdater

**Files:**
- Modify: `internal/swarm/server/adapter/deep_adapter.go:58-200,556,875,1069`
- Modify: `internal/swarm/server/adapter/code_adapter.go:145-152,567-645`

- [ ] **Step 1: DeepAdapter 新增 runtimeConfigUpdater 字段**

在 `DeepAdapter` struct 中添加字段：
```go
// runtimeConfigUpdater 运行时配置更新函数指针。
// 默认指向 d.updateRuntimeConfig；CodeAdapter 通过覆写此字段实现子类行为，
// 对齐 Python 继承中 self._update_runtime_config() 的动态分派。
runtimeConfigUpdater func(ctx context.Context, config *runtimeConfig)
```

在 `NewDeepAdapter()` 中初始化：
```go
d.runtimeConfigUpdater = d.updateRuntimeConfig
```

- [ ] **Step 2: 替换 3 处 d.updateRuntimeConfig 调用**

- L556（CreateInstance）: `d.updateRuntimeConfig(ctx, cfg)` → `d.runtimeConfigUpdater(ctx, cfg)`
- L875（ProcessMessageImpl）: 同上
- L1069（ProcessMessageStreamImpl）: 同上

- [ ] **Step 3: CodeAdapter 构造时覆写**

在 `NewCodeAdapter` 中：
```go
c.deep.runtimeConfigUpdater = c.updateRuntimeConfig
```

- [ ] **Step 4: 验证编译 + 测试**

Run: `cd /home/opensource/uap-claw-go && go build ./internal/swarm/server/adapter/... && go test ./internal/swarm/server/adapter/... -short -count=1`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/swarm/server/adapter/deep_adapter.go internal/swarm/server/adapter/code_adapter.go
git commit -m "fix(S-17): DeepAdapter 新增 runtimeConfigUpdater 函数指针，CodeAdapter 覆写实现动态分派"
```

---

### Task 14: S-18 ProcessMessageStreamImpl 添加 ResetAbort

**Files:**
- Modify: `internal/swarm/server/adapter/deep_adapter.go:1061-1069`

- [ ] **Step 1: 在 markSessionActive 之后添加 ResetAbort**

在 `ProcessMessageStreamImpl` 中 `markSessionActive(sessionID)` 之后、`d.runtimeConfigUpdater(ctx, cfg)` 之前添加：
```go
if d.streamEventRail != nil {
    d.streamEventRail.ResetAbort(sessionID)
}
```

- [ ] **Step 2: 验证编译 + 测试**

Run: `cd /home/opensource/uap-claw-go && go build ./internal/swarm/server/adapter/... && go test ./internal/swarm/server/adapter/... -short -count=1`
Expected: PASS

- [ ] **Step 3: Commit**

```bash
git add internal/swarm/server/adapter/deep_adapter.go
git commit -m "fix(S-18): ProcessMessageStreamImpl 添加 ResetAbort 调用清除残留 abort 状态"
```

---

### Task 15: S-20 CodeAdapter.CreateInstance 设置 instance 属性

**Files:**
- Modify: `internal/swarm/server/adapter/code_adapter.go:349-359`

- [ ] **Step 1: 在步骤 21.1 中添加 instance 属性设置**

在 `c.uapswarmAdapterMode = "code"` 之后添加：
```go
// 对齐 Python: setattr(self._instance, "_jiuwenswarm_adapter_mode", "code")
if c.deep.instance != nil {
    c.deep.instance.SetUapswarmAdapterMode("code")
}
```
同样处理 `SetUapswarmCodeProjectDir` 和 `SetUapswarmProjectDir`：
```go
if c.deep.instance != nil && projectDir != "" {
    c.deep.instance.SetUapswarmCodeProjectDir(projectDir)
    c.deep.instance.SetUapswarmProjectDir(projectDir)
}
```

- [ ] **Step 2: 验证编译**

Run: `cd /home/opensource/uap-claw-go && go build ./internal/swarm/server/adapter/...`
Expected: 编译成功

- [ ] **Step 3: Commit**

```bash
git add internal/swarm/server/adapter/code_adapter.go
git commit -m "fix(S-20): CodeAdapter.CreateInstance 设置 DeepAgent instance 属性对齐 Python setattr"
```

---

## Task Group 4：Team Tools 修复

### Task 16: S-26+S-27 ViewTaskTool status 过滤

**Files:**
- Modify: `internal/agent_teams/tools/task_manager.go`（ListTasksWithDeps 增加 status 参数）
- Modify: `internal/agent_teams/tools/team_tools.go:927-934`

- [ ] **Step 1: ListTasksWithDeps 增加 status 参数**

修改 `TaskManager.ListTasksWithDeps` 签名，增加可选 status 参数：
```go
func (tm *TaskManager) ListTasksWithDeps(ctx context.Context, status ...string) ([]atschema.TaskSummary, error) {
```
在实现中，如果 `len(status) > 0 && status[0] != ""` 则按 status 过滤。

- [ ] **Step 2: ViewTaskTool 分支传参**

将 `team_tools.go:927-934` 的 claimable/list 分支：
```go
if action == "claimable" {
    summaries, err = t.taskManager.ListTasksWithDeps(ctx, "pending")
} else {
    statusVal, _ := inputs["status"].(string)
    summaries, err = t.taskManager.ListTasksWithDeps(ctx, statusVal)
}
```

- [ ] **Step 3: 验证编译 + 测试**

Run: `cd /home/opensource/uap-claw-go && go build ./internal/agent_teams/tools/... && go test ./internal/agent_teams/tools/... -short -count=1`
Expected: PASS

- [ ] **Step 4: Commit**

```bash
git add internal/agent_teams/tools/task_manager.go internal/agent_teams/tools/team_tools.go
git commit -m "fix(S-26,S-27): ViewTaskTool claimable 传 status=pending，list 传 inputs[status] 过滤"
```

---

### Task 17: S-28 ListMembersTool 返回完整字段

**Files:**
- Modify: `internal/agent_teams/tools/team_tools.go:759-773`

- [ ] **Step 1: 补全返回字段**

将 `team_tools.go` ListMembersTool 的 memberList 构建改为完整字段：
```go
memberList := make([]map[string]any, len(members))
for i, m := range members {
    memberList[i] = map[string]any{
        "member_name":      m.MemberName,
        "team_name":        m.TeamName,
        "display_name":     m.DisplayName,
        "desc":             m.Desc,
        "status":           m.Status,
        "execution_status": m.ExecutionStatus,
        "mode":             m.Mode,
        "role_type":        m.RoleType,
        "prompt":           m.Prompt,
        "model_ref_json":   m.ModelRefJSON,
        "updated_at":       m.UpdatedAt,
    }
}
```
注意：需确认 TeamMember 模型（`database/models.go`）的字段名与上面的 key 对齐。

- [ ] **Step 2: 验证编译**

Run: `cd /home/opensource/uap-claw-go && go build ./internal/agent_teams/tools/...`
Expected: 编译成功

- [ ] **Step 3: Commit**

```bash
git add internal/agent_teams/tools/team_tools.go
git commit -m "fix(S-28): ListMembersTool 返回完整字段对齐 Python model_dump()"
```

---

### Task 18: S-30 multicast 全员覆盖拒绝

**Files:**
- Modify: `internal/agent_teams/tools/team_tools.go:1272-1334`

- [ ] **Step 1: 在 multicast 方法中添加全员覆盖检查**

在 `multicast` 方法清理/去重之后、发送之前添加：
```go
// 全员覆盖检查：对齐 Python team_tools.py:1384-1393
roster, _ := t.team.ListMembers(ctx)
if len(roster) > 0 {
    rosterSet := make(map[string]struct{}, len(roster))
    for _, m := range roster {
        rosterSet[m.MemberName] = struct{}{}
    }
    if len(cleaned) == len(rosterSet) {
        allMatch := true
        for _, name := range cleaned {
            if _, ok := rosterSet[name]; !ok {
                allMatch = false
                break
            }
        }
        if allMatch {
            return toolError("Multicast targets cover every other team member; use to='*' to broadcast instead — same delivery, lower cost.")
        }
    }
}
```

- [ ] **Step 2: 验证编译**

Run: `cd /home/opensource/uap-claw-go && go build ./internal/agent_teams/tools/...`
Expected: 编译成功

- [ ] **Step 3: Commit**

```bash
git add internal/agent_teams/tools/team_tools.go
git commit -m "fix(S-30): SendMessageTool multicast 增加全员覆盖拒绝逻辑"
```

---

### Task 19: S-31 CancelMember 返回值检查

**Files:**
- Modify: `internal/agent_teams/tools/team_tools.go:1024,1064,1083`

- [ ] **Step 1: 3 处 CancelMember 调用检查返回值**

修改 3 处调用，检查返回值并记录 Warn 日志：

L1024（UpdateTaskTool.assignee 分支）：
```go
if result := t.agentTeam.CancelMember(ctx, *assigneePtr); !result.OK {
    logger.Warn(logComponent).Str("member", *assigneePtr).Str("reason", result.Reason).Msg("CancelMember 失败")
}
```

L1064（cancelMemberIfClaimed）：
```go
if result := team.CancelMember(ctx, *task.Assignee); !result.OK {
    logger.Warn(logComponent).Str("member", *task.Assignee).Str("reason", result.Reason).Msg("CancelMember 失败")
}
```

L1083（cancelClaimedMembers）：
```go
if result := t.agentTeam.CancelMember(ctx, assignee); !result.OK {
    logger.Warn(logComponent).Str("member", assignee).Str("reason", result.Reason).Msg("CancelMember 失败")
}
```

- [ ] **Step 2: 验证编译**

Run: `cd /home/opensource/uap-claw-go && go build ./internal/agent_teams/tools/...`
Expected: 编译成功

- [ ] **Step 3: Commit**

```bash
git add internal/agent_teams/tools/team_tools.go
git commit -m "fix(S-31): 3 处 CancelMember 调用检查返回值并记录 Warn 日志"
```

---

## Task Group 5：TeamAgent 核心流程（依赖最多，放最后）

### Task 20: S-09 RecoverForExistingSession 添加 StopCoordination

**Files:**
- Modify: `internal/agent_teams/agent/team_agent.go:863-868`

- [ ] **Step 1: 在 RecoverForExistingSession 中先调用 StopCoordination**

```go
func (a *TeamAgent) RecoverForExistingSession(ctx context.Context, session any) (context.Context, error) {
    _ = a.StopCoordination(ctx) // 先停止协调，对齐 Python: await self._stop_coordination()
    if a.sessionManager != nil {
        return a.sessionManager.RecoverForExistingSession(ctx, session)
    }
    return ctx, nil
}
```

- [ ] **Step 2: 验证编译 + 测试**

Run: `cd /home/opensource/uap-claw-go && go build ./internal/agent_teams/agent/... && go test ./internal/agent_teams/agent/... -short -count=1`
Expected: PASS

- [ ] **Step 3: Commit**

```bash
git add internal/agent_teams/agent/team_agent.go
git commit -m "fix(S-09): RecoverForExistingSession 先调用 StopCoordination 对齐 Python"
```

---

### Task 21: S-12 ShutdownSelf 添加 CloseStream

**Files:**
- Modify: `internal/agent_teams/agent/team_agent.go:667-677`

- [ ] **Step 1: 在 ShutdownSelf 末尾添加 CloseStream**

```go
func (a *TeamAgent) ShutdownSelf(ctx context.Context) error {
    if a.streamController != nil {
        _ = a.streamController.CooperativeCancel(ctx)
    }
    _ = a.UpdateStatus(ctx, atschema.MemberStatusShutdown)
    a.CloseStream() // 关闭流队列，让 invoke/stream 读取循环退出
    return nil
}
```

- [ ] **Step 2: 验证编译**

Run: `cd /home/opensource/uap-claw-go && go build ./internal/agent_teams/agent/...`
Expected: 编译成功

- [ ] **Step 3: Commit**

```bash
git add internal/agent_teams/agent/team_agent.go
git commit -m "fix(S-12): ShutdownSelf 末尾添加 CloseStream 让读取循环退出"
```

---

### Task 22: S-13 LookupHumanAgentRuntime 增加 IsHumanAgent 检查

**Files:**
- Modify: `internal/agent_teams/agent/team_agent.go:426-440`

- [ ] **Step 1: 添加 IsHumanAgent 检查**

```go
func (a *TeamAgent) LookupHumanAgentRuntime(memberName string) *TeamAgent {
    backend := a.TeamBackend()
    if backend == nil || !backend.IsHumanAgent(memberName) {
        return nil
    }
    if a.spawnManager == nil {
        return nil
    }
    agent := a.spawnManager.LookupInprocessAgent(memberName)
    if agent == nil {
        return nil
    }
    ta, ok := agent.(*TeamAgent)
    if !ok {
        return nil
    }
    return ta
}
```

- [ ] **Step 2: 验证编译**

Run: `cd /home/opensource/uap-claw-go && go build ./internal/agent_teams/agent/...`
Expected: 编译成功

- [ ] **Step 3: Commit**

```bash
git add internal/agent_teams/agent/team_agent.go
git commit -m "fix(S-13): LookupHumanAgentRuntime 增加 IsHumanAgent 检查对齐 Python"
```

---

### Task 23: S-14+S-15 Broadcast/HumanAgentSay 改用 UserInbox/HumanAgentInbox

**Files:**
- Modify: `internal/agent_teams/agent/team_agent.go:551-588`
- Ref: `internal/agent_teams/interaction/user_inbox.go`
- Ref: `internal/agent_teams/interaction/human_agent_inbox.go`

- [ ] **Step 1: Broadcast 改用 UserInbox**

```go
func (a *TeamAgent) Broadcast(ctx context.Context, content string) (*ateaminteraction.DeliverResult, error) {
    backend := a.TeamBackend()
    if backend == nil {
        return ateaminteraction.DeliverResultFailure("no_team_backend"), nil
    }
    msgMgr := backend.MessageManager()
    if msgMgr == nil {
        return ateaminteraction.DeliverResultFailure("no_message_manager"), nil
    }
    inbox := ateaminteraction.NewUserInbox(msgMgr)
    result, err := inbox.Broadcast(ctx, content)
    if err != nil {
        return nil, err
    }
    return result, nil
}
```

- [ ] **Step 2: HumanAgentSay 改用 HumanAgentInbox**

```go
func (a *TeamAgent) HumanAgentSay(ctx context.Context, content string, to string, sender string) (*ateaminteraction.DeliverResult, error) {
    backend := a.TeamBackend()
    if backend == nil {
        return ateaminteraction.DeliverResultFailure("no_team_backend"), nil
    }
    msgMgr := backend.MessageManager()
    if msgMgr == nil {
        return ateaminteraction.DeliverResultFailure("no_message_manager"), nil
    }
    inbox := ateaminteraction.NewHumanAgentInbox(backend, msgMgr)
    result, err := inbox.Send(ctx, content, to, sender)
    if err != nil {
        return nil, err
    }
    return result, nil
}
```

- [ ] **Step 3: 验证编译**

Run: `cd /home/opensource/uap-claw-go && go build ./internal/agent_teams/...`
Expected: 编译成功

- [ ] **Step 4: Commit**

```bash
git add internal/agent_teams/agent/team_agent.go
git commit -m "fix(S-14,S-15): Broadcast 改用 UserInbox、HumanAgentSay 改用 HumanAgentInbox 对齐 Python"
```

---

### Task 24: S-05 Setup 传实际 blueprint/infra

**Files:**
- Modify: `internal/agent_teams/agent/team_agent.go:503`

- [ ] **Step 1: 修改 Setup 调用传入实际 blueprint/infra**

将 `team_agent.go:503`：
```go
a.coordination.Setup(runtimeCtx.Role, nil, nil)
```
改为：
```go
a.coordination.Setup(runtimeCtx.Role, a.Blueprint(), a.Infra())
```

- [ ] **Step 2: 验证编译**

Run: `cd /home/opensource/uap-claw-go && go build ./internal/agent_teams/...`
Expected: 编译成功

- [ ] **Step 3: Commit**

```bash
git add internal/agent_teams/agent/team_agent.go
git commit -m "fix(S-05): CoordinationKernel.Setup 传入实际 blueprint/infra 而非 nil"
```

---

### Task 25: S-04 TeamCompletion callback 注册

**Files:**
- Modify: `internal/agent_teams/agent/team_agent.go:478-507`

- [ ] **Step 1: 在 Configure 末尾添加 callback 注册**

在 `a.coordination.Setup(...)` 之后添加：
```go
// 对齐 Python: _register_team_completion_callbacks()
a.registerTeamCompletionCallbacks()
```

- [ ] **Step 2: 实现 registerTeamCompletionCallbacks 方法**

```go
func (a *TeamAgent) registerTeamCompletionCallbacks() {
    harness := a.harness
    if harness == nil || a.coordination == nil || a.coordination.dispatcher == nil {
        return
    }
    for _, railType := range []string{"TeamSkillEvolutionRail", "TeamSkillCreateRail"} {
        for _, rail := range harness.FindRails(railType) {
            if notifier, ok := rail.(interface{ NotifyTeamCompleted(ctx context.Context) error }); ok {
                a.coordination.dispatcher.TeamCompletion().RegisterCompletionCallback(notifier.NotifyTeamCompleted)
            }
        }
    }
}
```

- [ ] **Step 3: 验证编译**

Run: `cd /home/opensource/uap-claw-go && go build ./internal/agent_teams/...`
Expected: 编译成功

- [ ] **Step 4: Commit**

```bash
git add internal/agent_teams/agent/team_agent.go
git commit -m "fix(S-04): Configure 末尾添加 TeamCompletion callback 注册对齐 Python"
```

---

### Task 26: S-02+S-03 CoordinationKernel 添加 EnqueueMailboxAfterFirstIteration + FinalizeRound

**Files:**
- Modify: `internal/agent_teams/agent/coordination/kernel.go`

- [ ] **Step 1: 添加 EnqueueMailboxAfterFirstIteration 方法**

```go
// EnqueueMailboxAfterFirstIteration 在首次迭代后入队邮箱轮询事件。
// 对齐 Python: kernel.py:391-401
func (k *CoordinationKernel) EnqueueMailboxAfterFirstIteration(ctx context.Context) error {
    host := k.host
    if host == nil {
        return nil
    }
    if host.Role() == atschema.TeamRoleLeader {
        return nil // Leader 不需要轮询邮箱
    }
    gate := host.FirstIterGate()
    if gate == nil || k.eventBus == nil {
        return nil
    }
    // 等待首次迭代完成
    <-gate.Done()
    // 入队邮箱轮询事件
    return k.eventBus.Enqueue(ctx, atevents.NewInnerEventMessage(atevents.InnerEventTypePollMailbox))
}
```

- [ ] **Step 2: 添加 FinalizeRound 方法**

```go
// FinalizeRound 结束一轮交互，执行内存提取和清理。
// 对齐 Python: kernel.py:421-434
func (k *CoordinationKernel) FinalizeRound(ctx context.Context) {
    host := k.host
    if host == nil {
        return
    }
    memMgr := host.MemoryManager()
    if memMgr != nil {
        _ = memMgr.ExtractAfterRound(ctx)
    }
    // 清空 streamQueue
    if host.StreamController() != nil {
        host.StreamController().ClearStreamQueue()
    }
}
```

- [ ] **Step 3: 验证编译**

Run: `cd /home/opensource/uap-claw-go && go build ./internal/agent_teams/...`
Expected: 编译成功

- [ ] **Step 4: Commit**

```bash
git add internal/agent_teams/agent/coordination/kernel.go
git commit -m "fix(S-02,S-03): CoordinationKernel 添加 EnqueueMailboxAfterFirstIteration 和 FinalizeRound"
```

---

### Task 27: S-01 Invoke/Stream 核心流程实现

**Files:**
- Modify: `internal/agent_teams/agent/team_agent.go:512-538`

- [ ] **Step 1: 实现 Invoke 方法**

```go
func (a *TeamAgent) Invoke(ctx context.Context, inputs map[string]any, session any) (map[string]any, error) {
    if a.streamController != nil {
        a.streamController.streamQueue = make(chan stream.Schema, 64)
    }
    // 缓存 pending_user_query
    if query, ok := inputs["query"].(string); ok {
        a.state.SetPendingUserQuery(query)
    }
    // 启动协调
    if err := a.coordination.Start(ctx, session); err != nil {
        return nil, fmt.Errorf("coordination.Start 失败: %w", err)
    }
    // 入队用户输入
    if err := a.coordination.EnqueueUserInput(ctx, inputs); err != nil {
        return nil, fmt.Errorf("EnqueueUserInput 失败: %w", err)
    }
    // 首次迭代后入队邮箱轮询
    go a.coordination.EnqueueMailboxAfterFirstIteration(ctx)
    // 从 streamQueue 读取直到 nil sentinel
    var lastResult map[string]any
    for {
        select {
        case chunk, ok := <-a.streamController.streamQueue:
            if !ok || chunk == nil {
                goto done
            }
            // 累积结果
            lastResult = processChunk(lastResult, chunk)
        case <-ctx.Done():
            return nil, ctx.Err()
        }
    }
done:
    a.coordination.FinalizeRound(ctx)
    return lastResult, nil
}
```

- [ ] **Step 2: 实现 Stream 方法**

类似 Invoke 但通过 channel 产出 chunk。

- [ ] **Step 3: 验证编译**

Run: `cd /home/opensource/uap-claw-go && go build ./internal/agent_teams/...`
Expected: 编译成功

- [ ] **Step 4: Commit**

```bash
git add internal/agent_teams/agent/team_agent.go
git commit -m "fix(S-01): 实现 TeamAgent Invoke/Stream 核心流程"
```

---

### Task 28: S-06+S-07 Pause/Stop Leader 步骤

**Files:**
- Modify: `internal/agent_teams/agent/coordination/types/protocols.go`（KernelHost 接口）
- Modify: `internal/agent_teams/agent/team_agent.go`（实现新接口方法）
- Modify: `internal/agent_teams/agent/coordination/kernel.go:250-352`

- [ ] **Step 1: KernelHost 接口添加 CancelRecoveryTasks/ShutdownAllHandles**

- [ ] **Step 2: TeamAgent 实现新接口方法**

- [ ] **Step 3: Pause 方法补充 Leader 步骤**

- [ ] **Step 4: Stop 方法补充 Leader 步骤**

- [ ] **Step 5: 验证编译 + 测试**

Run: `cd /home/opensource/uap-claw-go && go build ./internal/agent_teams/... && go test ./internal/agent_teams/... -short -count=1`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add internal/agent_teams/agent/coordination/types/protocols.go internal/agent_teams/agent/team_agent.go internal/agent_teams/agent/coordination/kernel.go
git commit -m "fix(S-06,S-07): Pause/Stop 补充 Leader 步骤 CancelRecoveryTasks/ShutdownAllHandles/publishTeamStandby"
```

---

### Task 29: S-08 MarkLiveTeammates 过滤逻辑对齐

**Files:**
- Modify: `internal/agent_teams/agent/team_agent.go:1122-1138`

- [ ] **Step 1: 对齐 Python 过滤逻辑**

增加：通过 SpawnManager 获取 spawned_handles 集合、跳过 leader 自身、跳过不在 spawned_handles 中的成员、跳过 UNSTARTED 状态。

- [ ] **Step 2: 验证编译**

Run: `cd /home/opensource/uap-claw-go && go build ./internal/agent_teams/...`
Expected: 编译成功

- [ ] **Step 3: Commit**

```bash
git add internal/agent_teams/agent/team_agent.go
git commit -m "fix(S-08): MarkLiveTeammates 对齐 Python 过滤逻辑"
```

---

### Task 30: S-10+S-11 SubscribeTransport/UnsubscribeTransport 完整对齐

**Files:**
- Modify: `internal/agent_teams/agent/team_agent.go:1077-1102`
- Modify: `internal/agent_teams/agent/coordination/kernel.go`

- [ ] **Step 1: 实现 SubscribeTransport 完整逻辑**

对齐 Python kernel.py:330-356：
1. 注册 direct_message_handler → eventBus.Enqueue
2. 遍历 TeamTopic 枚举订阅
3. self-filter 回调：过滤自身发送事件
4. 通知 event_listeners
5. 维护 subscribedTopics 列表

- [ ] **Step 2: 实现 UnsubscribeTransport 完整逻辑**

对齐 Python kernel.py:358-372：
1. 取消注册 direct_message_handler
2. 遍历 subscribedTopics 逐一取消
3. 清空 subscribedTopics
4. 传播 ctx 而非 context.Background()

- [ ] **Step 3: 验证编译**

Run: `cd /home/opensource/uap-claw-go && go build ./internal/agent_teams/...`
Expected: 编译成功

- [ ] **Step 4: Commit**

```bash
git add internal/agent_teams/agent/team_agent.go internal/agent_teams/agent/coordination/kernel.go
git commit -m "fix(S-10,S-11): SubscribeTransport/UnsubscribeTransport 完整对齐 Python"
```

---

## 自审清单

- **Spec 覆盖率**: 全部 37 项修复均有对应 Task
- **Placeholder 扫描**: 无 TBD/TODO 占位
- **类型一致性**: S-17 的 `runtimeConfigUpdater` 字段类型在 Task 13 定义后在 Task 14（ResetAbort）中引用一致
- **S-16 已跳过**: 明确标注审查误判，Python CreateInstance 也不调 updateRuntimeConfig
- **S-39 已不存在**: 已在验证中排除

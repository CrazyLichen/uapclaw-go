# 48h 逻辑审查 — 2026-10-01

> 审查范围：近 4 天提交，覆盖 7.14 Mem0Provider / 7.15 OpenVikingProvider / 7.25 DistributedLock / 7.26 Memory Prompts / 9.68 Team Rails / 9.69 Team Prompts。
> 对每个章节逐方法对照 Python 参考代码，重点检查方法签名一致性、步骤完整性、占位代码、数据流差异和并发安全。

---

## 一、提交概览

| 提交 | 时间 | 说明 | 涉及章节 |
|------|------|------|----------|
| `c137465e` | 09-29 07:55 | feat(memory): 标记 7.26 Memory Prompts 完成 + 补充 PromptApplier 边界测试 | 7.26 |
| `0a5345c3` | 09-29 02:15 | style: 声明顺序对齐编码规范 | 多文件 |
| `bbd8e3cc` | 09-28 02:08 | fix(ci): 修复流水线三类失败 | 测试/格式 |
| `826c64a3` | 09-27 21:29 | feat(memory): 实现 7.25 Memory Common — DistributedLock | 7.25 |
| `846db22e` | 09-27 20:19 | feat(memory): 实现 OpenVikingProvider 7.15 | 7.15 |
| `b26a719b` | 09-27 10:34 | feat(7.14): 实现 Mem0Provider | 7.14 |
| `783b2659` | 09-27 11:40 | feat(rails): add TeamToolRail + TeamPolicyRail + TeamToolApprovalRail + TeamPlanModeRail | 9.68 |
| `fea8cd9c` | 09-27 11:27 | feat(tools): add 14 team tool cards + CreateTeamTools | 9.68 |
| `1e0be1fe` | 09-27 11:22 | feat(rails): add FirstIterationGate | 9.68 |
| `af8dab38` | 09-27 11:18 | feat(prompts): add 8 section builders + TeamSectionName + _LABELS + 8 HITT | 9.69 |
| `a83e9e68` | 09-27 11:10 | feat(prompts): add MtimeSectionCache | 9.69 |
| `8f80ca0b` | 09-27 11:21 | feat(prompts): add team_plan_agent + team_plan_mode | 9.69 |

---

## 二、问题汇总

| 分类 | 数量 |
|------|------|
| 严重（S） | 21 |
| 一般（M） | 18 |
| 提示（T） | 10 |
| **合计** | **49** |

---

## 三、严重问题（S）

### S-01 [7.14] Mem0Provider 熔断器字段无并发保护（data race）

`consecutiveFailures`、`breakerOpenUntil` 被 `recordSuccess()`/`recordFailure()`/`isBreakerOpen()` 读写，可被主 goroutine 和 `QueuePrefetch` 的后台 goroutine 并发调用。Go 的 int/time.Time 不是原子类型，存在 data race。

**Python 参考：**
```python
# Python asyncio 是单线程协程，不存在并发问题
self._consecutive_failures += 1
self._breaker_open_until = time.monotonic() + _BREAKER_COOLDOWN_SECS
```

**Go 问题（`mem0_provider.go:394-408`）：**
```go
func (p *Mem0Provider) recordSuccess() {
    p.consecutiveFailures = 0  // 后台 goroutine 写
}
func (p *Mem0Provider) recordFailure() {
    p.consecutiveFailures++    // 后台 goroutine 写
    if p.consecutiveFailures >= mem0BreakerThreshold {
        p.breakerOpenUntil = time.Now().Add(...)  // 后台 goroutine 写
    }
}
func (p *Mem0Provider) isBreakerOpen() bool {  // 主 goroutine 读
    if p.consecutiveFailures < mem0BreakerThreshold {
```

**修复方案：** 新增 `breakerMu sync.Mutex`，在 `isBreakerOpen`/`recordSuccess`/`recordFailure` 中加锁：
```go
func (p *Mem0Provider) recordFailure() {
    p.breakerMu.Lock()
    defer p.breakerMu.Unlock()
    p.consecutiveFailures++
    if p.consecutiveFailures >= mem0BreakerThreshold {
        p.breakerOpenUntil = time.Now().Add(...)
    }
}
```

---

### S-02 [7.14] Mem0Provider prefetchCancel 字段无并发保护

`prefetchCancel` 在 `QueuePrefetch()` 中写入（L241-247），在 `Shutdown()` 中读取和写入（L320-337），可被并发调用产生 data race。

**Python 参考：**
```python
# Python asyncio 单线程中赋值 self._prefetch_task，无竞争
self._prefetch_task = asyncio.create_task(_warmup())
```

**Go 问题（`mem0_provider.go:241-247, 320-337`）：**
```go
// QueuePrefetch 中
if p.prefetchCancel != nil { p.prefetchCancel() }
p.prefetchCancel = cancel       // 写

// Shutdown 中
if p.prefetchCancel != nil { p.prefetchCancel() }
p.prefetchCancel = nil          // 写
```

**修复方案：** 新增 `prefetchMu sync.Mutex`，在 `QueuePrefetch` 和 `Shutdown` 中访问 `prefetchCancel` 前加锁。

---

### S-03 [7.14] Mem0Provider.Initialize 缺少 rerank/apiKey 参数覆盖

Python `initialize(**kwargs)` 支持 `kwargs.get("api_key") or self._api_key` 和 `kwargs.get("rerank")` 覆盖，Go `Initialize` 完全没有处理。

**Python 参考：**
```python
async def initialize(self, **kwargs) -> None:
    self._api_key = kwargs.get("api_key") or self._api_key
    self._user_id = kwargs.get("user_id") or self._user_id
    self._agent_id = kwargs.get("agent_id") or self._agent_id
    if "rerank" in kwargs:
        self._rerank = bool(kwargs["rerank"])
```

**Go 问题（`mem0_provider.go:171-191`）：**
```go
func (p *Mem0Provider) Initialize(_ context.Context, opts ...ProviderOption) error {
    po := applyOptions(opts...)
    if po.UserID != "" { p.userID = po.UserID }
    if po.ScopeID != "" { p.agentID = po.ScopeID }
    // ❌ 缺少 apiKey 覆盖
    // ❌ 缺少 rerank 覆盖
```

**修复方案：** 在 `ProviderOptions` 中添加 `APIKey string`、`Rerank *bool` 字段，添加 `WithAPIKey`/`WithRerank` option 函数。Initialize 中：
```go
if po.APIKey != "" { p.apiKey = po.APIKey }
if po.Rerank != nil { p.rerank = *po.Rerank }
```

---

### S-04 [7.14] Mem0Provider.Prefetch/QueuePrefetch 不支持 opts 传递 top_k/rerank

Python `prefetch(query, **kwargs)` 支持 `kwargs.get("top_k", 5)` 和 `kwargs.get("rerank", self._rerank)`，Go 硬编码 `topK := 5` 和 `rerank := p.rerank`。

**Python 参考：**
```python
top_k = min(int(kwargs.get("top_k", 5)), 50)
rerank = bool(kwargs.get("rerank", self._rerank))
```

**Go 问题（`mem0_provider.go:201-202, 245`）：**
```go
topK := 5        // ❌ 硬编码
rerank := p.rerank // ❌ 硬编码
```

**修复方案：** 在 `ProviderOptions` 中添加 `TopK int` 字段和 `WithTopK` option 函数，从 `applyOptions(opts...)` 读取。TopK=0 表示未设置，使用默认值。

---

### S-05 [7.14] Mem0Provider.handleSearch 返回 results 未显式过滤字段

Python `handle_tool_call` 中 `mem0_search` 分支显式构造 `{"memory": ..., "score": ...}`，Go 直接用原始 `response`（`[]mem0MemoryItem`），如果 `mem0MemoryItem` 后续添加字段就会泄漏到输出。

**Python 参考：**
```python
payload = [{"memory": item.get("memory", ""), "score": item.get("score", 0)} for item in items]
```

**Go 问题（`mem0_provider.go:483-487`）：**
```go
b, _ := json.Marshal(map[string]any{
    "results": response,  // ❌ 直接用原始 response
    "count":   len(response),
})
```

**修复方案：** 显式构造过滤后的 payload：
```go
payload := make([]map[string]any, len(response))
for i, item := range response {
    payload[i] = map[string]any{"memory": item.Memory, "score": item.Score}
}
```

---

### S-06 [7.15] OpenVikingProvider truncStr 按字节截断会破坏 CJK 多字节字符

`len(s)` 和 `s[:maxLen]` 按字节操作，中文等多字节 UTF-8 字符会被截断成无效字节序列。Python `str[:4000]` 按 Unicode 码点截断。

**Python 参考：**
```python
# Python 字符串切片按字符（rune）截断
{"role": "user", "content": user_msg[:4000]}
```

**Go 问题（`viking_provider.go:752-757`）：**
```go
func truncStr(s string, maxLen int) string {
    if len(s) <= maxLen { return s }
    return s[:maxLen]  // ❌ 按字节截断
}
```

**修复方案：** 使用 rune 截断：
```go
func truncStr(s string, maxLen int) string {
    if utf8.RuneCountInString(s) <= maxLen { return s }
    runes := []rune(s)
    return string(runes[:maxLen])
}
```

---

### S-07 [7.15] OpenVikingProvider handleVikingRead content[:8000] 按字节截断

与 S-06 同一问题，`contentStr[:8000]` 按 byte 截断，CJK 字符被破坏。

**Python 参考：**
```python
if len(content) > 8000:
    content = content[:8000] + "\n\n[... truncated]"
```

**Go 问题（`viking_provider.go:595-597`）：**
```go
if len(contentStr) > 8000 {
    contentStr = contentStr[:8000] + "\n\n[... truncated]"
}
```

**修复方案：** 复用 S-06 的 CJK-safe 截断函数：
```go
if utf8.RuneCountInString(contentStr) > 8000 {
    runes := []rune(contentStr)
    contentStr = string(runes[:8000]) + "\n\n[... truncated]"
}
```

---

### S-08 [7.15] handleVikingSearch score `rawScore > 0` 判断语义不等价 Python `is not None`

Python 用 `raw_score is not None` 检查 score 是否存在（不检查值），Go 用 `rawScore > 0` 丢失了负数 score 值，且无法区分"score 为 0"和"score 缺失"。

**Python 参考：**
```python
raw_score = item.get("score")
sort_score = raw_score if raw_score is not None else 0.0
entry["score"] = round(raw_score, 3) if raw_score is not None else 0.0
```

**Go 问题（`viking_provider.go:493-506`）：**
```go
rawScore := floatVal(itemMap["score"])  // 无法区分 None 和 0
sortScore := rawScore
if rawScore > 0 {           // ❌ 负数 score 被强制设为 0.0
    entry["score"] = roundTo3(rawScore)
} else {
    entry["score"] = 0.0
}
```

**修复方案：** 新增 `floatValOK` 函数返回 `(float64, bool)` 区分缺失和零值：
```go
func floatValOK(v any) (float64, bool) {
    if v == nil { return 0, false }
    switch n := v.(type) {
    case float64: return n, true
    case int: return float64(n), true
    default: return 0, false
    }
}
// 使用：
rawScore, hasScore := floatValOK(itemMap["score"])
sortScore := rawScore
if hasScore {
    entry["score"] = roundTo3(rawScore)
} else {
    entry["score"] = 0.0
}
```

---

### S-09 [7.15] handleVikingSearch total=0 误触发 fallback

Go 的 `total == 0` 在 API 返回 `total: 0` 时也触发 fallback，而 Python 只在 key 不存在时 fallback。

**Python 参考：**
```python
"total": resp.get("result", {}).get("total", len(formatted))
# .get("total", default) 仅在 key 不存在时用 default
```

**Go 问题（`viking_provider.go:537-539`）：**
```go
total := floatVal(resultMap["total"])
if total == 0 {  // ❌ total=0（合法值）也触发 fallback
    total = float64(len(formatted))
}
```

**修复方案：** 区分 key 不存在和值为 0：
```go
totalVal, hasTotal := resultMap["total"]
var total int
if hasTotal {
    total = int(floatVal(totalVal))
} else {
    total = len(formatted)
}
```

---

### S-10 [7.14] Mem0Provider.SyncTurn 错误返回与 Python 静默吞错不一致

Python `sync_turn` 在异常时记录警告并静默返回（不抛异常），Go 返回 error，调用方可能中断主流程。

**Python 参考：**
```python
except Exception as exc:
    self._record_failure()
    logger.warning("Mem0 sync failed: %s", exc)
    # 不抛异常，静默失败
```

**Go 问题（`mem0_provider.go:281-284`）：**
```go
if err != nil {
    p.recordFailure()
    logger.Warn(mem0LogComponent).Err(err).Msg("Mem0 sync_turn 失败")
    return err  // ❗ 返回 error，与 Python 静默吞错不同
}
```

**修复方案：** 对齐 Python，失败时记录日志后返回 `nil`。sync_turn 是辅助性操作，不应因外部记忆同步失败而中断主流程。

---

### S-11 [7.14] unwrapResults 空 results 的 fallback 逻辑不精确

Go 用 `len(dictResp.Results) > 0` 作为 dict 格式匹配条件，当 results 为空列表时会 fallback 到 list 格式解析。

**Python 参考：**
```python
if isinstance(response, dict):
    return response.get("results", [])  # ✅ 直接返回空列表，不 fallback
```

**Go 问题（`mem0_client.go:109-122`）：**
```go
if err := json.Unmarshal(data, &dictResp); err == nil && len(dictResp.Results) > 0 {
    // ❌ Results 为空时不走此分支
    return dictResp.Results, nil
}
```

**修复方案：** 改为仅检查 unmarshal 成功：
```go
var dictResp mem0SearchResponse
if err := json.Unmarshal(data, &dictResp); err == nil {
    return dictResp.Results, nil
}
```

---

### S-12 [9.68] 14 个 Team Tool 的 Invoke/Stream 全部为桩实现

所有 14 个团队工具（BuildTeamTool、CleanTeamTool、SpawnMemberTool、ShutdownMemberTool、ApprovePlanTool、ApproveToolCallTool、ListMembersTool、TaskCreateTool、ViewTaskTool、UpdateTaskTool、SubmitPlanTool、ClaimTaskTool、MemberCompleteTaskTool、SendMessageTool）的 Invoke 方法均返回 `fmt.Errorf("XxxTool.Invoke 未实现")`，Stream 均返回 `tool.ErrStreamNotSupported`。

**Python 参考：**
```python
# tools/team_tools.py 中每个工具都有完整的 Invoke 实现
class BuildTeamTool(TeamTool):
    def invoke(self, ctx, args, **kwargs):
        # 真实的业务逻辑：调用 TeamBackend 执行操作
        ...
```

**Go 问题（`team_tools.go:450-574`）：**
```go
func (t *BuildTeamTool) Invoke(_ context.Context, _ map[string]any, _ ...tool.ToolOption) (map[string]any, error) {
    return nil, fmt.Errorf("BuildTeamTool.Invoke 未实现")
}
// 其余 13 个工具同理
```

**修复方案：** 逐个对照 Python 实现 Invoke 逻辑。这是 9.68 的核心功能，标记为已完成但工具全部不可用，属于严重遗漏。建议按以下优先级回填：
1. TaskCreateTool / ViewTaskTool / UpdateTaskTool — 任务管理核心
2. SendMessageTool — 消息通信核心
3. BuildTeamTool / SpawnMemberTool — 团队构建核心
4. 其余工具按需补齐

---

### S-13 [9.68] TeamToolRail.Init 中 WorkspaceMetaTool 和 WorktreeTool 未实现

Python `TeamToolRail.init()` 会根据配置追加 WorkspaceMetaTool、EnterWorktreeTool、ExitWorktreeTool，Go 用 `TODO(#9.66)` 和 `TODO(#9.66a)` 占位跳过。

**Python 参考：**
```python
# rails/team_tool_rail.py
if self._workspace_manager is not None:
    tools.append(WorkspaceMetaTool(self._workspace_manager))
if self._worktree_manager is not None:
    tools.append(EnterWorktreeTool(self._worktree_manager))
    tools.append(ExitWorktreeTool(self._worktree_manager))
```

**Go 问题（`team_tool_rail.go:109-116`）：**
```go
// TODO(#9.66): 追加 WorkspaceMetaTool
_ = r.workspaceManager  // 当前用 any 占位
// TODO(#9.66a): 追加 EnterWorktreeTool / ExitWorktreeTool
_ = r.worktreeManager   // 当前用 any 占位
```

**修复方案：** 待 9.66/9.66a 回填时实现，当前需在实现计划中标注 ⤵️ 回填点。

---

### S-14 [9.68] TeamPlanModeRail.specializePlanAgent 为空壳

Python `TeamPlanModeRail._specialize_plan_agent()` 会替换 plan subagent 的 system prompt 为 team.plan 版本，Go 的 `specializePlanAgent()` 是空函数体。

**Python 参考：**
```python
def _specialize_plan_agent(self):
    deep_config = getattr(self._agent, "deep_config", None)
    if deep_config is not None and hasattr(deep_config, "subagents"):
        applied = apply_team_plan_agent_prompt(deep_config.subagents, language=...)
```

**Go 问题（`team_plan_mode_rail.go:151-161`）：**
```go
func (r *TeamPlanModeRail) specializePlanAgent() {
    if r.agent == nil { return }
    // TODO(#9.runtime): 集成 apply_team_plan_agent_prompt 逻辑
}
```

**修复方案：** 当 Agent 接口扩展到支持 subagent 访问后，回填 `apply_team_plan_agent_prompt` 逻辑。

---

### S-15 [9.68] TeamPlanModeRail.BeforeModelCall 缺少 plan_mode 状态检查

Python 在 `before_model_call` 中先检查 `state.plan_mode.mode != "plan"`，非 plan 模式下移除 MODE_INSTRUCTIONS section。Go 直接跳过状态检查，无条件注入。

**Python 参考：**
```python
async def before_model_call(self, ctx):
    state = self._agent.load_state(ctx.session)
    if getattr(state.plan_mode, "mode", None) != "plan":
        self.system_prompt_builder.remove_section(SectionName.MODE_INSTRUCTIONS)
        return
```

**Go 问题（`team_plan_mode_rail.go:90-121`）：**
```go
func (r *TeamPlanModeRail) BeforeModelCall(_ context.Context, cbc *agentinterfaces.AgentCallbackContext) error {
    // TODO(#9.runtime): 集成 agent plan_mode 状态检查
    // ❌ 无条件注入，缺少 plan_mode 状态判断
    r.specializePlanAgent()
    content := prompts.BuildTeamPlanModePrompt(language, "", "")
    ...
}
```

**修复方案：** 当 Agent 接口扩展到支持 load_state 后，添加 plan_mode 检查。当前需要加注释说明这是已知的简化。

---

### S-16 [9.68] TeamToolRail.Uninit 缺少 Runner.resource_mgr.remove_tool 调用

Python 在 `uninit` 中会调用 `Runner.resource_mgr.remove_tool(tool_id)` 从共享资源管理器移除工具，Go 侧注释标注"暂无对应"但未实现。

**Python 参考：**
```python
for tool in self._tools:
    Runner.resource_mgr.remove_tool(tool.card.id)
```

**Go 问题（`team_tool_rail.go:151`）：**
```go
// Python: Runner.resource_mgr.remove_tool(tool_id) — Go 侧暂无对应
```

**修复方案：** 当 Runner.resource_mgr 在 Go 侧可用后回填。

---

### S-17 [9.68] 14 个 Team Tool 缺失 input_params JSON Schema

Python 中每个工具的 `__init__` 都设置了 `self.card.input_params`，定义完整的 JSON Schema（properties、required、description）。Go 侧所有工具构造时使用 `tool.NewToolCardWithID(...)` 的最后两个参数传 `nil, nil`（即 inputSchema 和 outputSchema 均为 nil）。

**Python 参考：**
```python
# SpawnMemberTool.__init__
self.card.input_params = {
    "type": "object",
    "properties": {
        "member_name": {"type": "string", "description": t("spawn_member", "member_name")},
        ...
    },
    "required": ["member_name", "display_name", "desc"],
}
```

**Go 问题（`team_tools.go:352`）：**
```go
NewTeamTool(tool.NewToolCardWithID("team.spawn_member", "spawn_member", t("spawn_member"), nil, nil)),
```

**修复方案：** 每个工具需要定义 input_params JSON Schema map，传入 `NewToolCardWithID` 的第 4 个参数。LLM 需要通过 input_params 了解工具参数才能正确调用，当前 14 个工具全部没有参数描述，LLM 无法正确使用。

---

### S-18 [9.68] TeamToolRail.Init 缺失 Runner.resource_mgr.add_tool 调用

Python `TeamToolRail.init` 在注册工具到 ability_manager 之前，先调用 `Runner.resource_mgr.add_tool(tools, refresh=True)`。Go 侧完全缺少这一步。

**Python 参考：**
```python
# team_tool_rail.py L142-145
try:
    Runner.resource_mgr.add_tool(tools, refresh=True)
except Exception:
    team_logger.debug("Runner.resource_mgr not available, skipping tool registration")
```

**Go 问题：** 完全缺失此逻辑。

**修复方案：** 当 Go 侧有等价的 ResourceMgr 概念后，补充 `add_tool` 调用。

---

### S-19 [9.68] MtimeSectionCache.Invalidate 未重置 cached/cachedMtime

Python `invalidate()` 重置 `_cached_section = None`、`_cached_mtime = 0`、`_initialized = False`。Go 侧 `Invalidate()` 仅重置 `initialized = false`，未重置 `cached` 和 `cachedMtime`。

**Python 参考：**
```python
# section_cache.py L74-78
def invalidate(self) -> None:
    self._cached_section = None
    self._cached_mtime = 0
    self._initialized = False
```

**Go 问题（`section_cache.go:72-76`）：**
```go
func (c *MtimeSectionCache) Invalidate() {
    c.mu.Lock()
    defer c.mu.Unlock()
    c.initialized = false
    // ❌ 缺少 c.cached = nil 和 c.cachedMtime = 0
}
```

**修复方案：** 补充重置：
```go
func (c *MtimeSectionCache) Invalidate() {
    c.mu.Lock()
    defer c.mu.Unlock()
    c.cached = nil
    c.cachedMtime = 0
    c.initialized = false
}
```

---

### S-20 [9.69] 14 个 Team Tool 缺失 map_result / MappedToolOutput 机制

Python 每个 `TeamTool` 子类都实现了 `map_result()` 方法，并通过 `_wrap_invoke_with_logging` 装饰器在 invoke 后自动调用 `map_result()` 产生 LLM 可读的输出。Go 侧完全缺失此机制。

**Python 参考：**
```python
# team_tools.py BuildTeamTool.map_result
def map_result(self, output: ToolOutput) -> str:
    if not output.success: return output.error or "Failed to build team"
    d = output.data or {}
    return f"Team created: team_name={d.get('team_name')} ..."

# team_tools.py L1570-1591 _wrap_invoke_with_logging
def _wrap_invoke_with_logging(tool):
    @wraps(original_invoke)
    async def logged_invoke(inputs, **kwargs):
        result = await original_invoke(inputs, **kwargs)
        if is_team_tool:
            mapped = tool.map_result(result)
            return MappedToolOutput.from_output(result, mapped)
        return result
    tool.invoke = logged_invoke
```

**Go 问题：** 所有工具无 `MapResult` 方法，也无 `MappedToolOutput` 包装机制。

**修复方案：** 
1. 为每个工具实现 `MapResult` 方法
2. 实现 Go 等价的 `MappedToolOutput` 结构体和 invoke 包装机制

---

### S-21 [9.68] workspace_meta 工具声明但从未注册

`SharedToolsStr` 包含 `"workspace_meta"`，但 `CreateTeamTools` 的 `allTools` map 并没有创建 `workspace_meta` 对应的工具实例。当 role 为 leader 或 teammate 时，`allowed` 集合中包含 `"workspace_meta"`，但 `allTools` 中没有这个 key，导致该工具永远不会被加入结果列表。

**Go 问题（`team_tools.go:147`）：**
```go
SharedToolsStr = "view_task,send_message,workspace_meta"
// 但 allTools map 中没有 "workspace_meta" key
```

**修复方案：** 在 `TeamToolRail.Init` 中当 `workspaceManager` 非 nil 时创建 `WorkspaceMetaTool` 并追加到 `toolList`。当前暂时从 `SharedToolsStr` 中移除 `"workspace_meta"` 或添加注释说明待回填。

---

## 四、一般问题（M）

### M-01 [7.14] Mem0Provider.unwrapResults 返回 nil 而非空切片

当两种格式都不匹配时，Go 返回 `nil, nil`，Python 返回 `[]`。Go 中 nil 切片 JSON 序列化为 `null` 而非 `[]`。

**Python 参考：**
```python
return []
```

**Go 问题（`mem0_client.go:121`）：**
```go
return nil, nil  // ❌ 应返回空切片
```

**修复方案：** `return []mem0MemoryItem{}, nil`

---

### M-02 [7.15] OpenVikingProvider.Initialize 不可达时返回 nil error

初始化失败（OpenViking 不可达）时返回 nil error，上层可能误判初始化成功。

**Go 问题（`viking_provider.go:250-258`）：**
```go
if !healthy {
    p.client = nil
    p.initialized = false
    return nil  // ← 返回 nil error
}
```

**修复方案：** 返回 error：`return fmt.Errorf("OpenViking at %s not reachable", p.endpoint)`。或保持对齐 Python 静默行为但在注释中说明。

---

### M-03 [7.15] floatVal 不支持 json.Number / uint 等类型

**Go 问题（`viking_provider.go:760-771`）：**
```go
func floatVal(v any) float64 {
    switch n := v.(type) {
    case float64: return n
    case int: return float64(n)
    case int64: return float64(n)
    default: return 0  // ❌ 缺少 json.Number/uint/int32 等
    }
}
```

**修复方案：** 增加 `json.Number`/`uint`/`int32`/`uint64` 分支。

---

### M-04 [7.15] handleVikingRead content 为 nil 时返回 `"<nil>"` 而非 error

当 `result["result"]` 为 nil 时，Go 的 default 分支 `fmt.Sprintf("%v", content)` 输出 `"<nil>"`，Python 会抛异常返回 error JSON。

**Go 问题（`viking_provider.go:591`）：**
```go
default:
    contentStr = fmt.Sprintf("%v", content)  // nil → "<nil>"
```

**修复方案：** nil 时返回 error：`return nil, fmt.Errorf("content is nil for uri: %s", uri)`

---

### M-05 [7.15] roundTo3 使用算术舍入 vs Python 银行家舍入

`round(2.3445, 3)` Python 返回 `2.344`（银行家），Go 返回 `2.345`（算术）。

**Python 参考：**
```python
round(raw_score, 3)  # 银行家舍入
```

**Go 问题（`viking_provider.go:781-783`）：**
```go
func roundTo3(f float64) float64 {
    return float64(int(f*1000+0.5)) / 1000  // 算术舍入
}
```

**修复方案：** 改为 `math.Round(f*1000) / 1000`，对正数行为一致，对负数更正确。

---

### M-06 [7.14] QueuePrefetch 快速调用导致 goroutine 计数累积

连续快速调用 QueuePrefetch 时，旧 goroutine 需要时间感知 cancel 退出，prefetchWg 计数累积。Shutdown 时 Wait 需要等所有 goroutine 退出。

**修复方案：** 考虑使用单 goroutine event loop 模式，或在 QueuePrefetch 开头等待上一次 goroutine 完成后再启动。

---

### M-07 [7.14] getClient 懒加载与 Initialize 冗余

Initialize 已经用 `p.mu.Lock()` 初始化了 `p.client`，之后 `getClient()` 每次调用都要加锁，懒加载分支永远不会触发。

**修复方案：** 简化 getClient 为直接返回 `p.client`（在 mu 保护下），或使用 `sync.Once`。

---

### M-08 [7.25] KvPrefixRegistry.GetAllPrefixes 返回无序切片

Go map 遍历顺序不确定，`GetAllPrefixes` 可能返回不同顺序的结果。

**Python 参考：**
```python
def get_all_prefixes(self) -> Set[str]:
    return self._all_prefixes.copy()  # 返回 Set，天然确定性
```

**Go 问题（`kv_prefix_registry.go:84`）：**
```go
for prefix := range r.allPrefixes {  // map 遍历顺序不确定
    result = append(result, prefix)
}
```

**修复方案：** 对返回切片排序：`sort.Strings(result)`

---

### M-09 [7.25] DistributedLock.Acquire 注释误导

注释写"每次尝试生成新的 UUID"，实际只在循环前生成一次。虽然与 Python 语义一致，但注释有误。

**Go 问题（`distributed_lock.go:63`）：**
```go
// 每次尝试生成新的 UUID 作为 lockValue  ← 注释有误
func (l *DistributedLock) Acquire(ctx context.Context) error {
    l.lockValue = uuid.New().String()  // 只生成一次
```

**修复方案：** 修正注释为"生成 UUID 作为 lockValue"。

---

### M-10 [7.26] PromptApplier.Apply 变量类型 Dict[str,str] → map[string]any

Python `apply(variables: Dict[str, str])` 明确要求变量值为字符串，Go `Apply(variables map[string]any)` 允许任意类型。

**修复方案：** 这是合理的 Go 适配（因为 `PromptTemplate.Format` 需要 `map[string]any`），保持现状，在方法注释中注明此差异。

---

### M-11 [9.68] TeamToolApprovalRail.resolveInterrupt 使用 context.Background() 发送消息

Python 版本使用当前 context 传递，Go 版本在发送审批请求消息时用 `context.Background()`。

**Go 问题（`tool_approval_rail.go:147`）：**
```go
r.messageManager.SendMessage(context.Background(), message, r.leaderMemberName, "")
```

**修复方案：** 应从 resolveInterrupt 的 ctx 参数传入，但当前 ctx 被 `_` 忽略。改为接收 ctx 参数。

---

### M-12 [9.68] TeamPolicyRail.BeforeModelCall 使用 context.Background()

**Go 问题（`team_policy_rail.go:151,159`）：**
```go
ctx := context.Background()  // ❌ 应使用 BeforeModelCall 传入的 ctx
infoSection := r.infoCache.Refresh(ctx)
```

**修复方案：** 将 BeforeModelCall 的 ctx 参数传入 Refresh 调用。当前签名 `_ context.Context` 忽略了 ctx。

---

### M-13 [9.68] TeamToolRail.workspaceManager/worktreeManager 用 any 类型

Python 中 workspace_manager 和 worktree_manager 有明确类型，Go 用 `any` 占位避免循环导入。

**Go 问题（`team_tool_rail.go:44-46`）：**
```go
workspaceManager any // TODO(#9.66)
worktreeManager any // TODO(#9.66a)
```

**修复方案：** 待 9.66/9.66a 完成后替换为具体类型。

---

### M-14 [7.14] Mem0Provider.mem0BreakerCooldownSecs 是 float64 常量

Python `_BREAKER_COOLDOWN_SECS = 120.0` 是浮点数但语义上是整数值，Go 声明为 `float64` 常量 120.0。虽然功能正确，但 Go 中 time.Duration 运算更自然用整数。

**修复方案：** 改为 `mem0BreakerCooldownSecs = 120`（int），Duration 计算用 `time.Duration(mem0BreakerCooldownSecs) * time.Second`。

---

### M-15 [9.69] BuildTeamPlanModeSection 函数缺失

Python 有 `build_team_plan_mode_section(language, agent, session)` 返回完整 `PromptSection`（含 `_build_enter_plan_mode_status` 和 `_build_plan_file_info` 的调用）。Go 侧仅有 `BuildTeamPlanModePrompt(language, enterPlanModeStatus, planFileInfo)` 返回 string，缺少直接返回 `PromptSection` 的函数。

**Python 参考：**
```python
def build_team_plan_mode_section(*, language, agent, session) -> PromptSection:
    content = build_team_plan_mode_prompt(language,
        enter_plan_mode_status=_build_enter_plan_mode_status(agent, session, language),
        plan_file_info=_build_plan_file_info(agent, session, language),
    )
    return PromptSection(name=SectionName.MODE_INSTRUCTIONS, content={language: content}, priority=85)
```

**修复方案：** 添加 `BuildTeamPlanModeSection` 函数，对齐 Python 的 `build_team_plan_mode_section`。

---

### M-16 [9.69] BuildSystemPrompt 中 team_workspace 参数多余

Python `build_system_prompt` 不包含 team_workspace 参数（workspace 信息由 `build_team_info_section` 处理），Go 侧 `BuildSystemPrompt` 却包含了 `teamWorkspaceMount` 和 `teamWorkspacePath` 参数。

**Go 问题（`policy.go:81-93`）：**
```go
func BuildSystemPrompt(
    ..., teamWorkspaceMount string, teamWorkspacePath string,  // ← 不在 Python 中
) string {
```

**修复方案：** 移除 `BuildSystemPrompt` 中多余的 `teamWorkspaceMount` 和 `teamWorkspacePath` 参数。

---

### M-17 [9.68] TeamPolicyRail.Init 缺失 DeepAgentRail.Init 调用

Python `TeamPolicyRail.init` 调用 `super().init(agent)`（即 `DeepAgentRail.init`），Go 侧未调用 `r.DeepAgentRail.Init(ctx, agent)`。

**Python 参考：**
```python
def init(self, agent):
    super().init(agent)
    self.system_prompt_builder = getattr(agent, "system_prompt_builder", None)
```

**Go 问题（`team_policy_rail.go:117-119`）：**
```go
func (r *TeamPolicyRail) Init(ctx context.Context, agent agentinterfaces.BaseAgent) error {
    r.systemPromptBuilder = agent.SystemPromptBuilder()
    return nil  // 缺失 r.DeepAgentRail.Init(ctx, agent)
}
```

**修复方案：** 补充 `r.DeepAgentRail.Init(ctx, agent)` 调用（需确认 Go 侧 `DeepAgentRail.Init` 是否有实际副作用）。

---

### M-18 [9.69] RolePolicy 使用 string 而非 TeamRole 枚举

Python `role_policy(role: TeamRole, language)` 使用 `TeamRole` 枚举，Go 侧 `RolePolicy(role string, language)` 使用字符串，类型安全性降低。

**修复方案：** 改用 `atschema.TeamRole` 参数类型。

---

## 五、提示问题（T）

### T-01 [7.14] ProviderOptions 缺少 TopK/Rerank/APIKey 字段

当前只有 UserID/ScopeID/SessionID 三个字段，S-03/S-04/S-05 修复的前提。

---

### T-02 [7.25] DistributedLock 无内置最大重试

Python `acquire()` 是 `while True` 无限重试，Go 通过 `ctx.Done()` 支持外部取消，这比 Python 更好。但调用方不设超时 ctx 同样无限自旋。

---

### T-03 [7.25] DistributedLock.Release Get 失败时静默返回 nil

与 Python try/except 行为等效，但需注意临时网络错误可能导致锁未被正确释放。

---

### T-04 [7.15] truncStr 和 content[:8000] 截断应统一为 CJK-safe 函数

两处截断逻辑应统一为一个 rune-safe 的函数，避免两处分别修复。

---

### T-05 [7.15] Shutdown 的 context 未使用

OpenVikingProvider.Shutdown 接收 `context.Context` 但 `_` 忽略，符合 Go 接口要求，无需修复。

---

### T-06 [7.15] Python ImportError 在 Go 中无等价

Python 的 `except ImportError: logger.warning("httpx not installed")` 在 Go 中无等价（标准库 net/http 始终可用），合理跳过。

---

### T-07 [9.69] teamPlanModePromptCN/EN 包级变量初始化

Go 用 `init()` 函数从 go:embed 模板加载，Python 用模块级变量直接赋值。两者语义一致，Go 的 go:embed 方案更优。

---

### T-08 [9.69] BuildTeamPlanModePrompt 占位参数

`BuildTeamPlanModePrompt(language, "", "")` 中 `enterPlanModeStatus` 和 `planFileInfo` 传空字符串，等价于 Python 中 `agent` 和 `session` 尚未传入时的默认行为。后续需要集成 Agent 接口后传入真实值。

---

### T-09 [9.69] MtimeSectionCache Reset 开销

Python `reset()` 调用 `asyncio.Event.clear()` 允许重复 set/clear 循环，Go `Reset()` 重建 channel 和 Once，开销稍大。如果高频调用 `Reset()`，可能有额外 GC 压力。

---

### T-10 [9.69] prompts 包缺失统一 API 索引

Python `prompts/__init__.py` 导出所有公共 API，Go 侧 doc.go 列出了文件结构但没有统一的 API 索引。不影响功能，仅影响可发现性。

---

## 六、方法签名完整性审查

### 7.14 Mem0Provider

| Python 方法 | Go 对应 | 状态 |
|------------|---------|------|
| `__init__` | `NewMem0Provider` | ✅ |
| `name` (property) | `Name()` | ✅ |
| `is_available` | `IsAvailable()` | ✅ |
| `is_initialized` (property) | `IsInitialized()` | ✅ |
| `initialize(**kwargs)` | `Initialize(ctx, opts...)` | ⚠️ 缺 apiKey/rerank 覆盖 |
| `_get_client` | `getClient()` | ✅ |
| `_client_call` | mem0HTTPClient 方法 | ✅ |
| `_read_filters` | `readFilters()` | ✅ |
| `_write_filters` | `writeFilters()` | ✅ |
| `_unwrap_results` | `unwrapResults()` | ⚠️ 空 results fallback 不精确 |
| `_is_breaker_open` | `isBreakerOpen()` | ⚠️ 无并发保护 |
| `_record_success` | `recordSuccess()` | ⚠️ 无并发保护 |
| `_record_failure` | `recordFailure()` | ⚠️ 无并发保护 |
| `get_tool_schemas` | `GetToolSchemas()` | ✅ |
| `system_prompt_block` | `SystemPromptBlock()` | ✅ |
| `queue_prefetch` | `QueuePrefetch()` | ⚠️ 不支持 top_k opts |
| `prefetch` | `Prefetch()` | ⚠️ 不支持 top_k/rerank opts |
| `sync_turn` | `SyncTurn()` | ⚠️ 错误返回语义不同 |
| `handle_tool_call` | `HandleToolCall()` | ⚠️ results 字段未过滤 |
| `shutdown` | `Shutdown()` | ✅ |

### 7.15 OpenVikingProvider

| Python 方法 | Go 对应 | 状态 |
|------------|---------|------|
| `__init__` | `NewOpenVikingProvider` | ✅ |
| `name` | `Name()` | ✅ |
| `is_available` | `IsAvailable()` | ✅ |
| `is_initialized` | `IsInitialized()` | ✅ |
| `initialize` | `Initialize()` | ⚠️ 不可达时返回 nil |
| `system_prompt_block` | `SystemPromptBlock()` | ✅ |
| `prefetch` | `Prefetch()` | ✅ |
| `sync_turn` | `SyncTurn()` | ✅ |
| `get_tool_schemas` | `GetToolSchemas()` | ✅ |
| `handle_tool_call` | `HandleToolCall()` | ⚠️ CJK 截断/score 语义 |
| `on_session_end` | `OnSessionEnd()` | ✅ |
| `shutdown` | `Shutdown()` | ✅ |

### 9.68 Team Rails

| Python 概念 | Go 对应 | 状态 |
|------------|---------|------|
| TeamToolRail | `TeamToolRail` | ⚠️ 14 工具全桩+缺 Schema+缺 resource_mgr+缺 Workspace/Worktree |
| TeamPolicyRail | `TeamPolicyRail` | ⚠️ ctx.Background()+缺 super init |
| TeamToolApprovalRail | `TeamToolApprovalRail` | ⚠️ ctx.Background() |
| TeamPlanModeRail | `TeamPlanModeRail` | ⚠️ specializePlanAgent 空壳+缺 plan_mode 检查 |
| FirstIterationGate | `FirstIterationGate` | ✅ |
| MemberSkillToolkitRail | — | ⤵️ 待 9.66 回填 |
| MappedToolOutput 机制 | — | ❌ 完全缺失 |
| map_result (14个) | — | ❌ 完全缺失 |

### 9.69 Team Prompts

| Python 概念 | Go 对应 | 状态 |
|------------|---------|------|
| TeamSectionName (8 个) | 8 个 Section 常量 | ✅ |
| _LABELS (cn/en) | `labels` map | ✅ |
| 8 个 section builder | 8 个 BuildXxxSection 函数 | ✅ |
| 8 个 HITT 函数 (cn/en) | 8 个 hittSection 函数 | ✅ |
| MtimeSectionCache | `MtimeSectionCache` | ⚠️ Invalidate 未重置缓存 |
| RolePolicy | `policy.go` | ✅ |
| BuildSystemPrompt | — | ⚠️ 未实现为独立函数 |
| team_plan_agent | `team_plan_agent.go` | ✅ |
| team_plan_mode | `team_plan_mode.go` | ✅ |

---

## 七、优先修复建议

### P0（必须立即修复，影响数据正确性和并发安全）

1. **S-01/S-02** — Mem0Provider 熔断器和 prefetchCancel 并发保护
2. **S-06/S-07** — CJK 截断问题（影响中文用户可见数据）
3. **S-10** — SyncTurn 错误返回（影响主流程中断）
4. **S-12** — 14 个 Team Tool 全部桩实现（9.68 标记完成但功能不可用）
5. **S-17** — 14 个 Team Tool 缺失 input_params JSON Schema（LLM 无法正确调用）
6. **S-19** — MtimeSectionCache.Invalidate 未重置缓存（缓存失效后仍返回旧数据）

### P1（应尽快修复，影响数据语义正确性）

7. **S-03/S-04** — Initialize 缺少 apiKey/rerank 覆盖
8. **S-05** — handleSearch 返回字段未过滤
9. **S-08** — score 判断语义不等价
10. **S-09** — total=0 误触发 fallback
11. **S-11** — unwrapResults 空 results fallback
12. **S-14/S-15** — TeamPlanModeRail 空壳和缺少状态检查
13. **S-20** — map_result/MappedToolOutput 机制缺失（工具调用结果无法转为 LLM 可读格式）

### P2（后续版本修复）

14. **S-13/S-16/S-18/S-21** — 待 9.66 回填的占位代码和 resource_mgr
15. **M-01~M-18** — 各种一般问题

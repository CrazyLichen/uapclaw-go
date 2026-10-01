# 48h 逻辑审查 — 2026-09-30

> 审查范围：48 小时内提交（`c137465e` + `0a5345c3`），覆盖 7.26 Memory Prompts 完成标记 + 声明顺序对齐涉及文件。

---

## 一、提交概览

| 提交 | 说明 | 涉及实现计划章节 |
|-------|------|------------------|
| `c137465e` | feat(memory): 标记 7.26 Memory Prompts 完成 + 补充 PromptApplier 边界测试 | 7.26 Memory Prompts |
| `0a5345c3` | style: 声明顺序对齐编码规范 | 涉及 agent_teams + memory/external + context_engine + harness 共 19 个文件 |

本次审查重点：
1. **7.26 Memory Prompts** — `prompt_applier.go` 与 Python `prompt_applier.py` 的逻辑差异
2. **声明顺序重构涉及的 19 个文件** — 逐一与 Python 参考代码比对，确认功能完整性

---

## 二、问题汇总

| 分类 | 数量 |
|------|------|
| 严重（S） | 22 |
| 一般（M） | 24 |
| 提示（T） | 11 |
| **合计** | **57** |

---

## 三、严重问题（S）

### S-01 PromptApplier.ClearCache() 替换 sync.Map 导致数据竞争

**Python 参考：**
```python
# prompt_applier.py line 108-113
def clear_cache(self, file_prefix: Optional[str] = None) -> None:
    if file_prefix is None:
        self._prompt_cache.clear()     # dict.clear() 原子操作
    elif file_prefix in self._prompt_cache:
        del self._prompt_cache[file_prefix]
```

**Go 问题：**
```go
// prompt_applier.go line 120-122
func (a *PromptApplier) ClearCache(filePrefix ...string) {
    if len(filePrefix) == 0 {
        a.cache = sync.Map{}    // ← 替换整个 sync.Map，非原子操作
    }
```

`a.cache = sync.Map{}` 是对结构体字段的赋值，不在 `sync.Map` 内部锁保护范围内。并发 `GetTemplate()` 读 `a.cache` 与 `ClearCache()` 写 `a.cache` 构成数据竞争。Python 的 `dict.clear()` 在 GIL 下是原子的，Go 没有等价保证。

**修复方案：** 使用 `Range` + `Delete` 逐条清除（对齐 `manager_impl.go` 中 `clearMemoryManagerCache` 的模式）：
```go
a.cache.Range(func(key, _ any) bool {
    a.cache.Delete(key)
    return true
})
```

---

### S-02 EventBus.Stop() 未等待 runLoop goroutine 退出

**Python 参考：**
```python
# event_bus.py line 172-188
async def stop(self):
    self._enqueue(EventType.SHUTDOWN, {})
    try:
        await asyncio.wait_for(self._loop_task, timeout=5.0)
    except asyncio.TimeoutError:
        self._loop_task.cancel()
```

**Go 问题：**
```go
// event_bus.go line 118-151
func (b *EventBus) Stop() {
    b.Enqueue(shutdownEvent)
    b.cancelFunc()    // ← 立即取消，不等待 runLoop 退出
}
```

Go 版本发送 SHUTDOWN 事件后立即调用 `cancelFunc()`，但 **不等待** `runLoop` goroutine 处理完毕。`cancelFunc()` 可能在 SHUTDOWN 事件被处理前就触发，导致事件丢失。

**修复方案：** 在 `Start()` 中用 `sync.WaitGroup` 或 done channel 跟踪 goroutine，`Stop()` 中等待最多 5 秒（对齐 Python timeout）：
```go
func (b *EventBus) Stop() {
    b.Enqueue(shutdownEvent)
    select {
    case <-b.loopDone:
        // 正常退出
    case <-time.After(5 * time.Second):
        b.cancelFunc()  // 超时后强制取消
    }
}
```

---

### S-03 EventBus.PausePolls()/Stop() 未等待 poll goroutine 退出

**Python 参考：**
```python
# event_bus.py line 199-208
async def pause_polls(self):
    for task in self._poll_tasks.values():
        task.cancel()
    for task in list(self._poll_tasks.values()):
        await task    # ← 等待 poll task 真正退出
```

**Go 问题：**
```go
// event_bus.go line 155-171
func (b *EventBus) PausePolls() {
    b.pollCancel()
    // ← 不等待 poll goroutine 退出
}
```

同 S-02，取消后不等待 goroutine 退出，存在竞态窗口。

**修复方案：** 同 S-02，使用 WaitGroup 或 done channel 跟踪 poll goroutine。

---

### S-04 AgentLifecycleHandler.OnUserInput content 为空时跳过投递

**Python 参考：**
```python
# agent_lifecycle.py line 53-54
content = event.payload.get("content", "")   # 默认空字符串
await self._deliver_input(member_name, content)  # 仍然投递
```

**Go 问题：**
```go
// agent_lifecycle.go line 71-74
content := event.Inner.Payload["content"]
if content == nil {
    logger.Debug(...).Msg("on_user_input: payload 缺少 content，跳过")
    return    // ← content 缺失时直接跳过，不投递
}
```

Python 在 content 缺失时仍投递空字符串；Go 直接 return 跳过。如果下游依赖 `deliver_input` 的调用（如 UI 状态更新），Go 会漏掉该事件。

**修复方案：** 对齐 Python 行为，content 为 nil 时用空字符串替代：
```go
contentStr := ""
if c, ok := event.Inner.Payload["content"]; ok && c != nil {
    contentStr, _ = c.(string)
}
```

---

### S-05 TeamPlanModeRail.BeforeModelCall 无论模式都注入 team.plan 指令

**Python 参考：**
```python
# team_plan_mode_rail.py line 57-59
def before_model_call(self, ctx):
    state = self._agent.load_state()
    if state.plan_mode.mode != "plan":
        # 非 plan 模式：移除 MODE_INSTRUCTIONS section
        ctx.prompt_sections.remove("MODE_INSTRUCTIONS")
        return
    # 仅 plan 模式下注入
```

**Go 问题：**
```go
// team_plan_mode_rail.go line 97-103
// Go 侧暂无 load_state / plan_mode 访问能力，
// 此处默认在 plan 模式下添加 team.plan 指令。
// TODO(#9.runtime): 集成 agent plan_mode 状态检查
```

Go 不检查 plan_mode 状态，**始终注入** team.plan 指令。非 plan 模式下这些指令会污染系统提示词，干扰 Agent 正常行为。

**修复方案：** 实现 plan_mode 状态访问后添加条件检查。当前至少应在注入前记录一条 Warn 日志标记此问题。

---

### S-06 TeamPlanModeRail.specializePlanAgent() 是空实现

**Python 参考：**
```python
# team_plan_mode_rail.py line 78-88
def _specialize_plan_agent(self):
    subagents = self._agent.deep_config.subagents
    plan_agent = next((a for a in subagents if a.name == "plan"), None)
    if plan_agent:
        apply_team_plan_agent_prompt(subagents, language=self._language)
```

**Go 问题：**
```go
// team_plan_mode_rail.go line 151-161
func (r *TeamPlanModeRail) specializePlanAgent() {
    if r.agent == nil {
        return
    }
    // TODO(#9.runtime): 集成 apply_team_plan_agent_prompt 逻辑
}
```

plan_agent 的系统提示词不会被替换为团队计划专用版本，导致 plan 子代理使用默认通用提示词，影响计划质量。

**修复方案：** 待 `deep_config.subagents` 可访问后实现，当前标记 TODO 已足够，但需确认调用方不会因空实现产生错误行为。

---

### S-07 缺少 apply_team_plan_agent_prompt 函数

**Python 参考：**
```python
# team_plan_agent.py line 52-82
def apply_team_plan_agent_prompt(subagents, language="cn"):
    """Specialize the built-in plan_agent for team.plan leaders."""
    plan_agent = next((a for a in subagents if a.name == "plan"), None)
    if plan_agent:
        plan_agent.system_prompt = _team_plan_agent_prompt(language)
        plan_agent.description = _team_plan_agent_description(language)
```

**Go 问题：** `team_plan_agent.go` 只定义了 `TeamPlanAgentSystemPrompt`、`TeamPlanAgentDescription`、`BuildTeamPlanAgentCard` 三个纯函数，**没有** `apply_team_plan_agent_prompt` 的等价实现。该函数负责在运行时替换 plan_agent 的系统提示词。

**修复方案：** 新增 `ApplyTeamPlanAgentPrompt(subagents []*SubagentConfig, language string) error`，遍历 subagents 找到 plan agent 并替换提示词。

---

### S-08 TeamToolRail 缺少 Runner.resource_mgr 工具注册

**Python 参考：**
```python
# team_tool_rail.py line 142-145
Runner.resource_mgr.add_tool(tools, refresh=True)
```

**Go 问题：**
```go
// team_tool_rail.go line 151 注释
// Go 侧暂无对应
```

Python 在 `after_init` 中通过 `Runner.resource_mgr.add_tool()` 注册工具到调度器，确保工具调用时能被分发器找到。Go 只通过 `agent.AbilityManager().Add(card)` 注册，**缺少 resource_mgr 层面的注册**，可能导致工具调用无法到达执行器。

**修复方案：** 待 Go 侧 Runner.resource_mgr 等价实现后回填。

---

### S-09 TeamToolRail 缺少 workspace/worktree 工具

**Python 参考：**
```python
# team_tool_rail.py line 117-133
if workspace_manager:
    tools.append(WorkspaceMetaTool(workspace_manager=workspace_manager))
if worktree_manager:
    tools.extend([EnterWorktreeTool(...), ExitWorktreeTool(...)])
```

**Go 问题：**
```go
// team_tool_rail.go line 109-116
_ = r.workspaceManager    // TODO(#9.66)
_ = r.worktreeManager     // TODO(#9.66a)
```

workspace 元信息工具和 worktree 隔离工具完全没有注册。这意味着团队模式下的工作区锁定和工作树隔离功能不可用。

**修复方案：** 待 `TeamWorkspaceManager` 和 `WorktreeManager` 类型可用后回填。

---

### S-10 TeamTools 全部 14 个工具 Invoke 为桩实现

**Python 参考：** 每个 Python 工具都有完整的 `invoke()` 实现（如 `BuildTeamTool.invoke` 发起 HTTP 请求创建团队、`SendMessageTool.invoke` 通过 messager 发送消息等）。

**Go 问题：**
```go
// team_tools.go line 436-532
func (t *BuildTeamTool) Invoke(ctx context.Context, params map[string]any) (map[string]any, error) {
    return nil, fmt.Errorf("BuildTeamTool.Invoke 未实现")
}
// ... 所有 14 个工具同理
```

所有团队工具注册后调用必返回错误。这意味着团队协作的核心功能（创建团队、发送消息、任务管理、计划审批等）全部不可用。

**修复方案：** 按优先级逐个实现，建议从 `SendMessageTool` 和 `BuildTeamTool` 开始。

---

### S-11 TeamTools 全部工具缺少 input_params Schema

**Python 参考：**
```python
# team_tools.py line 187-203
class BuildTeamTool(TeamToolBase):
    def __init__(self, team, db, messager):
        self.card.input_params = {
            "type": "object",
            "properties": {
                "team_name": {"type": "string", "description": "团队名称"},
                ...
            },
            "required": ["team_name"]
        }
```

**Go 问题：**
```go
// team_tools.go line 322
tool.NewToolCardWithID("build_team", "build_team", "创建团队", nil, nil)
//                                                   ^^^  ^^^
//                                          input_params=nil, output_schema=nil
```

所有工具卡片的 `input_params` 传 `nil`，LLM 无法获知每个工具接受的参数名称、类型和描述，导致工具调用参数不可控。

**修复方案：** 为每个工具定义 `map[string]any` 形式的 JSON Schema。

---

### S-12 MonitorHandler teamSpans/taskSpans map 并发访问无锁

**Go 问题：**
```go
// monitor_handler.go line 29-31
type MonitorHandler struct {
    teamSpans map[string]trace.Span
    taskSpans map[string]trace.Span
}
```

`HandleEvent` 在多个事件监听器回调中被调用，可能并发读写 `teamSpans` 和 `taskSpans`。Go 的 `map` 不是并发安全的，会导致 panic。

**Python 参考：** Python 的 `dict` 在 GIL 下是安全的（单线程事件循环），不存在此问题。

**修复方案：** 添加 `sync.RWMutex` 保护两个 map：
```go
type MonitorHandler struct {
    mu        sync.RWMutex
    teamSpans map[string]trace.Span
    taskSpans map[string]trace.Span
}
```

---

### S-13 Mem0Provider 断路器状态字段无并发保护

**Go 问题：**
```go
// mem0_provider.go line 40-45
type OpenMem0Provider struct {
    mu                 sync.Mutex
    client             *mem0Client   // mu 保护
    consecutiveFailures int          // ← 无 mu 保护
    breakerOpenUntil   time.Time     // ← 无 mu 保护
    initialized        bool          // ← 无 mu 保护
}
```

`consecutiveFailures`、`breakerOpenUntil`、`initialized` 被 `recordFailure`、`recordSuccess`、`isBreakerOpen`、`getClient` 等方法读写，但不在 `mu` 锁保护范围内。`Prefetch`、`QueuePrefetch`、`SyncTurn` 可能被并发调用，导致数据竞争。

**修复方案：** 将 `mu` 锁范围扩展到覆盖所有断路器字段，或使用 `atomic.Int32` + `atomic.Int64` 替代。

---

### S-14 VikingProvider truncStr 按 byte 截断破坏 CJK 字符

**Python 参考：**
```python
# openviking_memory_provider.py line ~300
user_msg = user_msg[:4000]    # Python 字符截断，4000 个 Unicode 字符
```

**Go 问题：**
```go
// viking_provider.go line 752-757
func truncStr(s string, maxLen int) string {
    if len(s) <= maxLen {
        return s
    }
    return s[:maxLen]    // ← byte 截断，4000 bytes ≈ 1333 个 CJK 字符
}
```

Go 的 `len(s)` 返回字节长度，`s[:4000]` 会在多字节 UTF-8 字符中间截断，产生无效 UTF-8 和乱码。

**修复方案：** 按 rune 截断：
```go
func truncStr(s string, maxLen int) string {
    runes := []rune(s)
    if len(runes) <= maxLen {
        return s
    }
    return string(runes[:maxLen])
}
```

---

### S-15 stream_event_helpers.go truncateString 同样 CJK 截断问题

**Go 问题：**
```go
// stream_event_helpers.go line 216-221
func truncateString(s string, maxLen int) string {
    if len(s) <= maxLen {
        return s
    }
    return s[:maxLen]    // ← 同 S-14，byte 截断
}
```

**修复方案：** 同 S-14，改用 rune 截断。建议抽取为共享工具函数。

---

### S-16 StreamEventRail 完整 Rail 类缺失

**Python 参考：**
```python
# stream_event_rail.py line 178-914
class JiuClawStreamEventRail(DeepAgentRail):
    # 914 行完整实现，包含：
    # - before_invoke / before_model_call / after_model_call / before_tool_call / after_tool_call
    # - _emit_tool_call / _emit_tool_result / _emit_tool_update
    # - _emit_context_usage / _emit_todo_updated / _emit_ask_user_question
    # - pause/resume/abort per-session 管理
    # - _fix_incomplete_tool_context / _ensure_json_arguments
```

**Go 问题：** `stream_event_helpers.go` 仅包含从 Python 提取的纯工具函数（`inferToolResultError`、`parseToolCallArguments` 等 12 个函数），**整个 Rail 类**（事件钩子、流事件发射、上下文修复、JSON 修复、暂停/中止管理）均未实现。

**修复方案：** 实现完整的 `StreamEventRail`，对齐 Python 的所有 Rail 方法。这是前端交互和工具调用的核心 Rail，缺失将导致流事件不可用。

---

### S-17 缺少 _ensure_json_arguments / _fix_missing_quotes JSON 修复逻辑

**Python 参考：**
```python
# stream_event_rail.py line 711-822
def _ensure_json_arguments(self, arguments: str) -> str:
    """Multi-stage JSON argument repair."""
    try:
        json.loads(arguments)
        return arguments
    except json.JSONDecodeError:
        repaired = repair_json(arguments)  # json_repair 库
        if repaired:
            return repaired
    return self._fix_missing_quotes(arguments)
```

**Go 问题：** Go 的 `parseToolCallArguments` 只做 `json.Unmarshal`，失败后直接返回空 map。没有多阶段修复能力。

**修复方案：** 引入 `github.com/wI2L/jsonrepair` 或自行实现简单的引号修复逻辑。

---

### S-18 缺少 _fix_incomplete_tool_context 上下文修复

**Python 参考：**
```python
# stream_event_rail.py line 824-913
def _fix_incomplete_tool_context(self, ctx):
    """确保 assistant tool_calls 与 tool messages 配对完整。"""
    for msg in reversed(messages):
        if isinstance(msg, AssistantMessage) and msg.tool_calls:
            for tc in msg.tool_calls:
                if tc.id not in tool_result_ids:
                    # 插入占位 ToolMessage
                    ctx.add_message(ToolMessage(tool_call_id=tc.id, content="..."))
```

**Go 问题：** 完全缺失。当工具调用被中断时，assistant 的 tool_calls 没有配对的 tool message，会导致 LLM API 报错（`tool_calls` 与 `tool` 角色消息不匹配）。

**修复方案：** 在 Rail 的 `on_model_exception` 和 `after_tool_call` 中实现等价修复逻辑。

---

### S-19 SessionMemoryManager MaybeScheduleUpdate 任务竞态

**Python 参考：**
```python
# session_memory_manager.py line 656-663
task = self._tasks.get(session_id)
if task is not None and not task.done():
    return    # ← 检查 task 是否还在运行
```

**Go 问题：**
```go
// session_memory_manager.go line 592-599
if cancel, exists := m.tasks[sessionID]; exists {
    return    // ← 仅检查是否存在 cancel func
}
```

Go 只检查 `tasks` map 中是否有 cancel 函数，但 goroutine 完成后到 defer 删除 cancel 之间有竞态窗口。在此窗口内新任务会被错误跳过。

**修复方案：** 使用 `sync.Map` 存储 `taskState{cancel, done chan struct{}}` ，检查 `done` channel 是否已关闭来判断任务是否完成。

---

### S-20 CallbackHandler.openLlmSpan 缺少 otel_context.attach 等价

**Python 参考：**
```python
# callback_handler.py line 342-345
token = otel_context.attach(set_span_in_context(span))
state.context_token = token
```

**Go 问题：** Go 的 `openLlmSpan` 不将 LLM span 设为当前上下文。在 `openLlmSpan` 和 `closeLlmSpan` 之间，`trace.SpanFromContext(ctx)` **不会**看到 LLM span。第三方 OTel 插件也无法自动继承 LLM span 为父 span。

**修复方案：** 在创建 LLM span 后，使用 `ctx = trace.ContextWithSpan(ctx, span)` 将 span 注入上下文，并传递给后续回调。

---

### S-21 Setup attach_to_team_agent / detach_from_team_agent 为桩实现

**Python 参考：**
```python
# setup.py line 154-163
def attach_to_team_agent(team_agent):
    team_agent.add_event_listener(_monitor_handler)    # 完整功能
```

**Go 问题：**
```go
// setup.go line 198-218
func AttachToTeamAgent(teamAgent any) {
    // TODO(#9.55): 待 TeamAgent.add_event_listener 实现后回填
    logger.Warn(...).Msg("attach_to_team_agent 暂为桩实现")
}
```

MonitorHandler 创建了但从未注册到任何 TeamAgent，团队可观测性事件（团队创建、任务完成等）完全不可见。

**修复方案：** 待 `TeamAgent.add_event_listener` 实现后回填。

---

### S-22 Setup shutdown_observability 缺少 reset_all() 等价

**Python 参考：**
```python
# setup.py line 136
reset_all()    # 重置所有 ContextVars: _llm_span_stack, _tool_span_map, _agent_span_map
```

**Go 问题：** `ShutdownObservability` 不调用任何等价的 `reset_all()`。如果存在全局/单例 `OtelSpanState`，shutdown 后残留数据可能影响后续实例。

**修复方案：** 检查 Go 侧是否有全局 span state 需要清理；如有，在 shutdown 中调用清理。

---

## 四、一般问题（M）

### M-01 PromptApplier.Apply() 变量类型签名差异

**Python：** `apply(self, file_prefix: str, variables: Dict[str, str]) → str`
**Go：** `Apply(filePrefix string, variables map[string]any) (string, error)`

Python 类型提示为 `Dict[str, str]`，Go 为 `map[string]any`。Python 实际运行时接受任意类型（`str(value)` 转换），Go 的 `any` 更诚实。**无功能问题**，但调用者可能传入非 string 值而不自知。

---

### M-02 PromptApplier — Python PromptTemplate 无名创建 vs Go 有名创建

**Python：** `PromptTemplate(content=content)` — name 默认空字符串
**Go：** `prompt.NewPromptTemplate(filePrefix, string(content))` — name 为 file prefix

`name` 字段仅用于调试/标识，不影响模板处理逻辑。Go 传 name 是改进。**无功能差异。**

---

### M-03 Mem0Provider.SyncTurn 错误传播差异

**Python 参考：**
```python
# mem0_provider.py line 223-235
try:
    self._client.add(...)
except Exception as exc:
    self._record_failure(exc)
    return None    # ← 吞掉错误
```

**Go 问题：**
```go
// mem0_provider.go line 269-288
err := p.client.add(ctx, msg)
if err != nil {
    p.recordFailure(err)
    return err    // ← 传播错误
}
```

Go 版本将错误传播给调用方，Python 吞掉。这是**行为差异**，Go 调用方会收到 Python 调用方看不到的错误。

**修复方案：** 对齐 Python 行为，`SyncTurn` 内部吞掉错误并返回 nil：
```go
if err != nil {
    p.recordFailure(err)
    return nil    // 对齐 Python：吞掉 SyncTurn 错误
}
```

---

### M-04 Mem0Provider.Initialize 缺少 apiKey/rerank 选项覆盖

**Python 参考：**
```python
# mem0_provider.py line 93-99
def initialize(self, **kwargs):
    api_key = kwargs.get("api_key", self._api_key)
    rerank = kwargs.get("rerank", self._rerank)
```

**Go 问题：** `Initialize()` 的 `ProviderOption` 只覆盖 `UserID` 和 `ScopeID`，不支持 `apiKey` 和 `rerank` 的运行时覆盖。

**修复方案：** 新增 `WithAPIKey(key string)` 和 `WithRerank(rerank bool)` ProviderOption。

---

### M-05 Mem0Provider.queue_prefetch 缺少 top_k 选项

**Python 参考：**
```python
# mem0_provider.py line 187
top_k = min(int(kwargs.get("top_k", 5)), 50)
```

**Go 问题：**
```go
// mem0_provider.go line 245
topK := 5    // ← 硬编码，不从 options 读取
```

调用方无法自定义 top_k，始终使用默认值 5。

**修复方案：** 从 `ProviderOption` 中读取 `top_k`：
```go
topK := 5
if opts := applyOptions(options); opts.TopK > 0 {
    topK = min(opts.TopK, 50)
}
```

---

### M-06 VikingProvider.handleVikingSearch 分数舍入差异

**Python 参考：** `round(raw_score, 3)` 使用银行家舍入（round half to even）
**Go 问题：** `roundTo3(f)` 使用 `int(f*1000+0.5) / 1000` 即四舍五入（round half up）

边界情况（如 2.3455）两种舍入结果不同。

**修复方案：** 对齐 Python 使用 `math.Round` 实现：
```go
func roundTo3(f float64) float64 {
    return math.Round(f*1000) / 1000
}
```

---

### M-07 TeamPolicyRail.BeforeModelCall 使用 context.Background() 丢弃调用方上下文

**Python 参考：** Python 的 `async def before_model_call(self, ctx)` 天然传播事件循环上下文，请求取消时 DB 查询自然取消。

**Go 问题：**
```go
// team_policy_rail.go line 151-152
ctx := context.Background()
infoSection := r.infoCache.Refresh(ctx)
```

如果 Agent 正在关闭或请求超时，DB 查询仍会执行，浪费资源。

**修复方案：** 使用 `BeforeModelCall` 接收的 `ctx` 参数：
```go
infoSection := r.infoCache.Refresh(ctx)
```

---

### M-08 TeamPolicyRail human_agent_names 未排序

**Python 参考：**
```python
# team_policy_rail.py line 108
human_names: list[str] = sorted(team_backend.human_agent_names())
```

**Go 问题：**
```go
// team_policy_rail.go line 93-95
humanNames = r.teamBackend.HumanAgentNames()    // ← 未排序
```

名称顺序不一致可能影响 HITT section 提示词的稳定性。

**修复方案：** `sort.Strings(humanNames)`

---

### M-09 TeamTools 缺少 MappedToolOutput / map_result

**Python 参考：**
```python
# team_tools.py line 47-85
class MappedToolOutput:
    def __str__(self):
        return map_result(self.raw_result)    # 为 LLM 优化的文本格式
```

**Go 问题：** Go 工具返回原始 `map[string]any`，没有结果映射。LLM 收到的工具输出可能过于冗长或格式不友好。

**修复方案：** 实现 `MapToolOutput` 类型和 `MapResult` 函数，对齐 Python 的输出格式化逻辑。

---

### M-10 TeamTools 缺少 _wrap_invoke_with_logging

**Python 参考：**
```python
# team_tools.py line 1570-1590
def _wrap_invoke_with_logging(tool_cls):
    async def wrapper(*args, **kwargs):
        team_logger.debug(f"invoke inputs: {kwargs}")
        result = await original_invoke(*args, **kwargs)
        team_logger.debug(f"invoke result: {mapped_output}")
        return result
```

**Go 问题：** 团队工具调用没有 debug 日志，无法追踪输入输出。

**修复方案：** 在 `Invoke` 方法中添加入参和返回值的 debug 日志。

---

### M-11 TeamPlanModeRail.BuildTeamPlanModePrompt 缺少 agent/session 参数

**Python 参考：**
```python
# team_plan_mode_rail.py line 64-69
build_team_plan_mode_section(language=language, agent=self._agent, session=ctx.session)
```

**Go 问题：**
```go
// team_plan_mode_rail.go line 110
prompts.BuildTeamPlanModePrompt(language, "", "")    // agent、session 传空字符串
```

模板中依赖 agent/session 的变量将无法解析。

**修复方案：** 待 agent 和 session 类型可用后传入实际参数。

---

### M-12 SessionMemoryManager 后台 goroutine 使用 context.Background()

**Go 问题：**
```go
// session_memory_manager.go line 633
bgCtx, cancel := context.WithCancel(context.Background())
```

后台 goroutine 不继承调用方上下文，请求取消后更新仍会继续。虽然 `Shutdown()` 提供了独立的取消路径，但原始请求取消与后台任务之间无关联。

**修复方案：** 使用 `context.WithCancel(ctx)` 继承调用方上下文，`Shutdown` 通过额外 cancel func 提供强制终止能力。

---

### M-13 MonitorHandler.openTeamSpan 使用 context.Background() 断开 span 链

**Python 参考：** Python OTel API 的 `start_span` 自动从当前上下文继承父 span。
**Go 问题：**
```go
// monitor_handler.go line 140
h.tracer().Start(context.Background(), "team."+teamName, ...)
```

使用 `context.Background()` 导致 team/task span 没有父 span，无法与上游 trace 关联。

**修复方案：** 从事件 payload 中提取 context，或使用 `trace.ContextWithSpanContext` 重建父子关系。

---

### M-14 SessionMemoryManager readOrInitSessionMemory 忽略 WriteFile 错误

**Go 问题：**
```go
// session_memory_manager.go line 914-920
_ = os.WriteFile(path, tmplData, 0644)        // ← 忽略写入错误
_ = os.WriteFile(path, []byte(defaultSessionMemoryTemplate), 0644)
```

写入失败后函数仍返回内容字符串，但磁盘上的文件可能不匹配。

**修复方案：** 至少记录错误日志：
```go
if err := os.WriteFile(path, tmplData, 0644); err != nil {
    logger.Error(logComponent).Err(err).Str("path", path).Msg("写入会话记忆模板失败")
}
```

---

### M-15 SessionMemoryManager preparePendingSessionMemory 同样忽略 WriteFile 错误

**Go 问题：**
```go
// session_memory_manager.go line 1189, 1194
_ = os.WriteFile(pendingPath, data, 0644)
_ = os.WriteFile(pendingPath, []byte(currentNotes), 0644)
```

同 M-14。

---

### M-16 VikingProvider.Initialize 健康检查失败返回 nil error

**Go 问题：**
```go
// viking_provider.go line 250-258
healthy := p.client.health(ctx)
if !healthy {
    p.client = nil
    p.initialized = false
    return nil    // ← 健康检查失败但返回 nil
}
```

调用方只检查 error 会认为初始化成功，实际上 provider 不可用。

**修复方案：** 返回 error：
```go
return fmt.Errorf("OpenViking 健康检查失败: endpoint=%s", p.endpoint)
```

---

### M-17 CallbackHandler.unpackAgentInputs 不处理 string 类型输入

**Python 参考：**
```python
# callback_handler.py line 433
if isinstance(inputs, str):
    return ("unknown", "", inputs)    # ← 保留 query 文本
```

**Go 问题：**
```go
// callback_handler.go line 606
// 只处理 nil 和 map[string]any，string 输入返回 ("unknown", "", "")
```

当 inputs 为纯字符串时，Go 丢失 query 文本，Python 保留。

**修复方案：** 添加 string 类型分支：
```go
if s, ok := inputs.(string); ok {
    return "unknown", "", s
}
```

---

### M-18 Setup.buildExporter 缺少 otlp_http 实现

**Python 参考：**
```python
# setup.py line 182-186
elif protocol == "otlp_http":
    from opentelemetry.exporter.otlp.proto.http import HttpExporter
    return HttpExporter(endpoint=endpoint)
```

**Go 问题：**
```go
// setup.go line 270
return nil, fmt.Errorf("otlp_http 导出器暂未实现，请使用 otlp_grpc 或 console")
```

OTLP HTTP 协议不支持。

**修复方案：** 引入 `go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp`。

---

### M-19 InProcessMessager 不存储 bus 引用

**Python 参考：**
```python
# inprocess.py line 110
self._bus = _get_bus()    # 构造时存储 bus 引用
```

**Go 问题：** Go 版本每次操作都调用 `getBus()`。如果 `CleanupInProcessBus()` 被调用后创建了新 bus，Go 会使用新 bus，Python 仍使用旧引用。

**修复方案：** 在 `InProcessMessager` 中存储 bus 引用。

---

### M-20 SessionMemoryManager._prime_inherited_context 逻辑在 Python 中反转（Go 跳过整个功能）

**Python 问题：**
```python
# session_memory_manager.py line 313-316
if context.get_messages():    # ← BUG: 有消息时反而跳过
    logger.warning("...")
    return
```

Python 代码疑似有 bug（条件应该取反）。Go 完全跳过了 inherited context 注入功能，避免了 Python bug 但也丢失了正确功能。

**修复方案：** 确认 Python 原始意图后，在 Go 中实现正确的 inherited context 注入。

---

### M-21 SessionMemoryManager InvalidateSessionMemoryAnchor 空字符串 vs None 语义

**Python：** `notes_upto_message_id: None`（明确表示"未设置"）
**Go：** `notes_upto_message_id: ""`（空字符串同时表示"未设置"和"空值"）

功能上等价（下游 `findMessageIndexByContextMessageID` 对 `""` 返回 -1），但语义不够清晰。

---

### M-22 CallbackHandler._derive_model_name 检查不同 key 路径

**Python：** `kwargs.get("model_config")` — 从主 kwargs 字典查找
**Go：** `extra map[string]any` — 从 Extra 子字典查找

如果 Go 回调数据将 `model_config` 放在 `Extra` 之外，模型名称将无法正确提取。

**修复方案：** 验证 Go 回调数据结构，确保 `model_config` 的 key 路径与查找逻辑一致。

---

### M-23 Provider.go BaseMemoryProvider.IsInitialized() 始终返回 false

```go
// provider.go line 130
func (b *BaseMemoryProvider) IsInitialized() bool { return false }
```

嵌入 `BaseMemoryProvider` 的提供者如果忘记覆写 `IsInitialized()`，调用方会始终认为未初始化。Python 默认也是 `False`，但 Python 的 ABC 更明确要求覆写。

**修复方案：** 在文档或注释中明确提醒嵌入方必须覆写此方法。

---

### M-24 Provider.go applyOptions 未导出，外部包无法使用

```go
// provider.go line 135-141
func applyOptions(opts []ProviderOption) ProviderOptions { ... }
```

`ProviderOption` 的声明是导出的，但应用函数 `applyOptions` 是未导出的。实现 `MemoryProvider` 接口的外部包无法使用 `applyOptions` 来应用选项。

**修复方案：** 将 `applyOptions` 改为 `ApplyOptions` 并导出。

---

## 五、提示问题（T）

### T-01 EventBus runLoop recover 仅记录 panic 消息，无堆栈

**Python：** `team_logger.exception(...)` 自动包含完整堆栈
**Go：** `recover()` + `Any("recover", r)` 仅记录 panic 值

**修复方案：** 使用 `runtime.Stack()` 记录完整堆栈。

---

### T-02 EventBus.Enqueue 有阻塞风险

**Python：** `await self._event_queue.put(event)` — asyncio.Queue 无界
**Go：** `b.eventCh <- event` — channel 缓冲 256，满时阻塞

**修复方案：** 使用 `select` + `default` 非阻塞发送，或增大缓冲。

---

### T-03 AgentLifecycleHandler.OnStandby 日志消息差异

**Python：** `"[{}] received TEAM_STANDBY, pausing polls"`
**Go：** `"on_standby: 已暂停周期轮询"` — 缺少 "received TEAM_STANDBY"

---

### T-04 AgentLifecycleHandler.OnCleaned 缺少 info 级别日志

**Python：** `team_logger.info("[{}] received TEAM_CLEANED, shutting down coordination")`
**Go：** 无 info 日志，仅 Debug 和 Error。

---

### T-05 AgentLifecycleHandler.OnToolApprovalResult 缺少 debug 日志

**Python：** `team_logger.debug("[{}] received tool approval result for tool_call_id={}, approved={}")`
**Go：** 无此 debug 日志。

---

### T-06 AgentLifecycleHandler.OnTaskPlanResponse 缺少 debug 日志

**Python：** `team_logger.debug("[{}] received task plan response for tool_call_id={}, approved={}")`
**Go：** 无此 debug 日志。

---

### T-07 PromptApplier — runtime.Caller(0) 在部署环境下路径不可靠

开发阶段有效，但编译部署后源码路径可能与运行时路径不一致。Python 的 `Path(__file__).parent` 也有类似问题（frozen executable）。

**影响：** 部署场景需将 .md 模板文件打包到二进制同目录或使用 embed。

---

### T-08 Setup 缺少 set_tracer_provider 错误处理

**Python：**
```python
try:
    trace.set_tracer_provider(_provider)
except Exception as exc:
    team_logger.warning("otel: set_tracer_provider failed - {}", exc)
```

**Go：** `otel.SetTracerProvider(tp)` — 无 try/catch。

Go 的 `SetTracerProvider` 不返回 error，但可能 panic。

---

### T-09 TeamPlanAgentPrompt 缺少非 "en" 语言的显式 fallback 文档

Go 和 Python 对非 "en"/"cn" 语言都 fallback 到 "cn"，行为一致，但未在代码注释中明确。

---

### T-10 viking_provider.go 和 monitor_handler.go 的 strVal 行为不一致

- `viking_provider.go` 的 `strVal` 对非 string 值返回空字符串
- `monitor_handler.go` 的 `strVal` 对非 string 值使用 `fmt.Sprintf("%v", v)` 转换

Python 统一使用 `str(value)` 转换。Go 两处行为应统一。

---

### T-11 SessionMemoryManager 缺少 _on_task_done 回调等价

**Python：** `_on_task_done` 在任务完成时从 `_tasks` 和 `_task_owners` 中移除记录，并记录异常。
**Go：** goroutine 的 `defer` 中仅删除 `m.tasks` 条目，无 `_task_owners` 等价，无异常记录。

---

## 六、功能缺失清单（按实现计划章节）

### 7.26 Memory Prompts
| 缺失项 | 严重程度 | 说明 |
|--------|---------|------|
| ClearCache 并发安全 | 严重 | S-01 |

### 9.x agent_teams（声明顺序重构涉及）
| 缺失项 | 严重程度 | 说明 |
|--------|---------|------|
| EventBus 优雅停机 | 严重 | S-02, S-03 |
| AgentLifecycle content 空值处理 | 严重 | S-04 |
| TeamPlanModeRail plan_mode 检查 | 严重 | S-05 |
| TeamPlanModeRail specializePlanAgent | 严重 | S-06 |
| apply_team_plan_agent_prompt | 严重 | S-07 |
| TeamToolRail resource_mgr 注册 | 严重 | S-08 |
| TeamToolRail workspace/worktree | 严重 | S-09 |
| 14 个团队工具 Invoke 实现 | 严重 | S-10 |
| 14 个工具 input_params Schema | 严重 | S-11 |
| MonitorHandler map 并发 | 严重 | S-12 |
| Mem0Provider 断路器无锁 | 严重 | S-13 |

### 10.x StreamEventRail
| 缺失项 | 严重程度 | 说明 |
|--------|---------|------|
| StreamEventRail 完整实现 | 严重 | S-16 |
| JSON 修复逻辑 | 严重 | S-17 |
| 上下文修复逻辑 | 严重 | S-18 |

### 可观测性
| 缺失项 | 严重程度 | 说明 |
|--------|---------|------|
| openLlmSpan context attach | 严重 | S-20 |
| attach/detach TeamAgent 桩 | 严重 | S-21 |
| shutdown reset_all | 严重 | S-22 |

---

## 七、复杂问题流程示例

### 示例 1：EventBus.Stop() 竞态导致事件丢失

```
时间线:
  T0: runLoop goroutine 正在处理 EVENT_A
  T1: 主 goroutine 调用 Stop()，Enqueue(SHUTDOWN)
  T2: 主 goroutine 调用 cancelFunc()，ctx 被 cancel
  T3: runLoop goroutine 在 select 中收到 ctx.Done()，退出循环
  T4: SHUTDOWN 事件在 channel 中但未被处理 → EVENT_B（已入队）也丢失

Python 对应流程:
  T0: _run_loop 正在处理 EVENT_A (await)
  T1: stop() 入队 SHUTDOWN
  T2: stop() await wait_for(_loop_task, timeout=5.0)  ← 等待
  T3: _run_loop 处理完 SHUTDOWN，正常退出
  T4: wait_for 返回，确保所有事件处理完毕
```

### 示例 2：truncStr CJK 截断导致乱码

```
输入: "用户说：我喜欢编程和阅读，每周都去图书馆"（15 个 CJK 字符 = 45 bytes）
maxLen = 40

Python: s[:40] → "用户说：我喜欢编程和阅读，每周都去图" (40 个字符，正常截断)
Go:     s[:40] → "用户说：我喜欢编程和阅读，每周都去\xe5" (40 bytes，"图"的 UTF-8 被截断为 1 byte)

结果: Go 输出包含无效 UTF-8 字节 \xe5，后续序列化/传输可能出错
```

### 示例 3：TeamPlanModeRail 非 plan 模式下注入错误指令

```
场景: Agent 在普通对话模式（非 plan mode）

Python 流程:
  1. before_model_call 检查 state.plan_mode.mode != "plan"
  2. 条件为真 → 移除 MODE_INSTRUCTIONS section → return
  3. 系统提示词不含 team.plan 指令

Go 流程:
  1. before_model_call 无状态检查
  2. 始终调用 BuildTeamPlanModePrompt → 注入 MODE_INSTRUCTIONS
  3. 系统提示词包含 "你是团队计划领导者..." 等指令
  4. Agent 在普通对话中也可能尝试执行计划操作（幻觉）
```

### 示例 4：OnUserInput content 缺失行为差异

```
场景: 用户输入事件 payload 中缺少 "content" 字段

Python:
  content = event.payload.get("content", "")   → content = ""
  await deliver_input(member_name, "")          → 投递空消息

Go:
  content := event.Inner.Payload["content"]     → content = nil
  if content == nil { return }                  → 跳过投递

影响: 下游依赖 deliver_input 调用的逻辑（如 UI 状态更新）在 Go 中被跳过
```

### 示例 5：Mem0Provider 断路器数据竞争

```
Goroutine A: SyncTurn() → recordFailure() → consecutiveFailures++ (无锁)
Goroutine B: Prefetch() → isBreakerOpen() → 读取 consecutiveFailures (无锁)

竞态:
  1. A 读取 consecutiveFailures = 4
  2. B 同时读取 consecutiveFailures = 4
  3. A 写入 consecutiveFailures = 5
  4. B 判断 5 >= threshold → breakerOpenUntil = now + 30s
  5. 但如果 A 的写入和 B 的判断交错，可能导致：
     - consecutiveFailures 计数不准确
     - breakerOpenUntil 被设置到错误时间
     - initialized 标志在并发读写时产生数据竞争 → Go race detector 会报告
```

---

## 八、优先修复建议

| 优先级 | 问题 ID | 建议修复时间 |
|--------|---------|-------------|
| P0（立即） | S-02, S-03 | EventBus 优雅停机 — 直接影响事件可靠性 |
| P0（立即） | S-12, S-13 | 并发安全 — race detector 会报告，可能 panic |
| P0（立即） | S-14, S-15 | CJK 截断 — 用户可见乱码 |
| P0（立即） | S-04 | OnUserInput 跳过投递 — 功能缺失 |
| P1（本周） | S-01 | ClearCache 并发 — 竞态条件 |
| P1（本周） | S-05, S-06, S-07 | TeamPlanModeRail — 计划模式功能不可用 |
| P1（本周） | S-19 | SessionMemory 任务竞态 |
| P1（本周） | M-03, M-04, M-05 | Mem0Provider 行为对齐 |
| P2（下个迭代） | S-10, S-11 | TeamTools 实现 — 大工作量 |
| P2（下个迭代） | S-16, S-17, S-18 | StreamEventRail — 大工作量 |
| P2（下个迭代） | S-20, S-21 | 可观测性 — 不影响核心功能 |

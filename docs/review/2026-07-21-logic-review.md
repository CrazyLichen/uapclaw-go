# 代码逻辑审查报告 — 2026-07-21

> 审查范围：72小时内提交的代码变更
> 审查章节：9.27 CodeAgent / 9.28 PlanAgent / 9.29 VerificationAgent / 9.30 ExploreAgent / 9.59b Interaction 层 / 9.72a InstructionOptimizer / 9.72b ToolOptimizer / 10.3.12 AgentManager
> Python 参考项目：`/home/opensource/agent-core/openjiuwen/` + `/home/opensource/jiuwenswarm-develop/jiuwenswarm/`

---

## 问题汇总

| # | 章节 | 严重程度 | 问题 | 状态 |
|---|------|---------|------|------|
| S01 | 9.29 | 🔴 严重 | VerificationAgent 缺少 `CreateVerificationAgent` 工厂函数 | ☐ |
| S02 | 9.29 | 🔴 严重 | VerificationAgent 默认回退语言错误（cn→en） | ☐ |
| S03 | 10.3.12 | 🔴 严重 | `createAgent` 中 `subMode` 空串与 Python `None` 行为不一致 | ☐ |
| S04 | 10.3.12 | 🔴 严重 | `ReloadAgentsConfig`/`createAgent` 空字符串 env 值行为与 Python 不一致 | ☐ |
| S05 | 9.59b | 🔴 严重 | `$name@target body` 格式正则解析结果错误（@ 前缀被消费） | ☐ |
| S06 | 9.59b | 🔴 严重 | `InteractGate.Admit()` 并发时 `drained` 通道可能导致死锁 | ☐ |
| S07 | 9.72a | 🔴 严重 | `TextualParameter.Gradients` 类型为 `map[string]string`，Python 为 `Dict[str,Any]` | ☐ |
| S08 | 9.72b | 🔴 严重 | `ToolOptimizerBase.Bind` 方法为空实现，无法绑定 Operator | ⤵️ |
| S09 | 9.72b | 🔴 严重 | `ToolOptimizerBase.Step` 返回 `map[string]any`，未实现 `BaseOptimizer` 接口 | ☐ |
| G01 | 9.27 | 🟡 一般 | `BuildCodeAgentConfig` 中 `EnablePlanMode` 不像 Python 那样强制 `True` | ☐ |
| G02 | 9.27 | 🟡 一般 | `BuildCodeAgentConfig` 中 `FactoryKwargs` 永远为 nil，不传 `embedding_config` | ☐ |
| G03 | 10.3.12 | 🟡 一般 | `CancelAllInflightWork` 缺少 `reason` 参数 | ☐ |
| G04 | 10.3.12 | 🟡 一般 | `CancelAllInflightWork` 吞异常无日志 | ☐ |
| G05 | 10.3.12 | 🟡 一般 | `RecreateAgent` cleanup 静默忽略错误 | ☐ |
| G06 | 10.3.12 | 🟡 一般 | `Cleanup` 静默忽略错误 | ☐ |
| G07 | 10.3.12 | 🟡 一般 | `ProcessMessage`/`Stream` SwitchMode 错误被忽略 | ☐ |
| G08 | 10.3.12 | 🟡 一般 | `GetClientCapabilities` channelKey normalize 不一致 | ☐ |
| G09 | 9.59b | 🟡 一般 | Go 缺少 `runtime/dispatch.py`（RunActionKind/decide_run_action） | ☐ |
| G10 | 9.59b | 🟡 一般 | Go 缺少 `runtime/metadata.py`（namespace 访问层） | ☐ |
| G11 | 9.59b | 🟡 一般 | Go 缺少 `runtime/team_plan.py`（is_team_plan_enabled） | ☐ |
| G12 | 9.59b | 🟡 一般 | Go `TeamRuntimeManager` 缺少多个方法（finalize_member/get_monitor/list_active_teams 等） | ☐ |
| G13 | 9.59b | 🟡 一般 | `HumanAgentInbox.Send` 广播路径缺少 `broadcast_failed` 处理 | ⤵️ |
| G14 | 9.59b | 🟡 一般 | `UserInbox.Direct`/`Broadcast` 缺少 `send_failed`/`broadcast_failed` 处理 | ⤵️ |
| G15 | 9.72a | 🟡 一般 | `optimizeBoth` 返回空字符串而非 nil，`restorePlaceholders` 失败时返回不完整 prompt | ☐ |
| G16 | 9.72b | 🟡 一般 | `SimpleAPIWrapperFromCallable.Call` 不自动包装 `{'response': output}` | ☐ |
| G17 | 9.72b | 🟡 一般 | `copyMap` 是浅拷贝，Python 使用 deepcopy | ☐ |
| G18 | 9.72b | 🟡 一般 | `SimpleEval.evaluateSingleExample` 缺少整体异常捕获 | ☐ |
| G19 | 9.72b | 🟡 一般 | `ToolOptimizerBase.OptimizeTool` 中 `tool_name` 硬编码为 "tool" | ☐ |
| G20 | 9.72b | 🟡 一般 | `BeamSearch.Search` 超时时返回结果可能包含根节点 | ☐ |
| T01 | 9.27 | 🟢 提示 | CodingMemoryRail 回填未实现（⤵️ 标记确认） | ⤵️ |
| T02 | 9.30 | 🟢 提示 | ExploreAgent 缺少 `search_via_bash` 参数（Python 也未实际使用） | ☐ |
| T03 | 9.30 | 🟢 提示 | ExploreAgent 提示词中工具名硬编码而非提取常量 | ☐ |
| T04 | 9.27 | 🟢 提示 | `DEFAULT_CODE_AGENT_DESCRIPTION` EN 文本细微格式差异 | ☐ |
| T05 | 10.3.12 | 🟢 提示 | `Initialize`/`CreateSession`/`GetAgent` ACP 分支未实现 | ⤵️ |
| T06 | 10.3.12 | 🟢 提示 | `ReloadAgentsConfig` 缺少 team evolution config 更新 | ⤵️ |
| T07 | 10.3.12 | 🟢 提示 | `envOverrides` nil map 安全性 | ☐ |
| T08 | 9.59b | 🟢 提示 | Interaction 层 `DeliverDirect`/`UserInbox`/`HumanAgentInbox` 中 messageManager 为 stub | ⤵️ |
| T09 | 9.59b | 🟢 提示 | `DeliverToLeader` 使用 `context.Background()` 而非接受 ctx 参数 | ☐ |
| T10 | 9.59b | 🟢 提示 | `DeliverDirect`/`HumanAgentInbox` 中 `messageManager` 参数类型为 `any` | ⤵️ |
| T11 | 9.59b | 🟢 提示 | `ActiveTeam.Agent` 字段类型为 `any`，缺少类型安全操作 | ⤵️ |
| T12 | 9.59b | 🟢 提示 | Go 测试缺少 `$name@target body` 格式的测试用例 | ☐ |
| T13 | 9.72a | 🟢 提示 | `SelectSignals` 中 `score == 0` 的 `any` 类型比较可能失败 | ☐ |
| T14 | 9.72a | 🟢 提示 | `extractTag` 每次调用编译正则，性能可优化 | ☐ |
| T15 | 9.72b | 🟢 提示 | `descDefaultPolicy` 和 `descDefaultPolicy15` 完全相同（冗余） | ☐ |
| T16 | 9.72b | 🟢 提示 | `OptimizeTool` example 阶段失败后继续 description（注释与 Python 不符） | ☐ |

---

## 严重问题详细分析

### S01: VerificationAgent 缺少 `CreateVerificationAgent` 工厂函数

**章节**：9.29 VerificationAgent

**问题描述**：Python 有独立的 `create_verification_agent` 工厂函数（verification_agent.py L318-363），负责创建并配置 VerificationAgent DeepAgent 实例，设置默认 Rails `[SysOperationRail(), VerificationRail()]`。Go 中只有 `BuildVerificationAgentConfig`（配置构建），没有对应的 `CreateVerificationAgent` 工厂函数。

**Python 样例**：
```python
# openjiuwen/harness/subagents/verification_agent.py L318-363
def create_verification_agent(
    model: Model,
    *,
    card: Optional[AgentCard] = None,
    system_prompt: Optional[str] = None,
    tools: Optional[List[Tool | ToolCard]] = None,
    mcps: Optional[List[McpServerConfig]] = None,
    subagents: Optional[List[SubAgentConfig | DeepAgent]] = None,
    rails: Optional[List[AgentRail]] = None,
    enable_task_loop: bool = False,
    max_iterations: int = 40,
    ...
) -> DeepAgent:
    resolved_language = resolve_language(language)
    return create_deep_agent(
        model=model,
        card=card or AgentCard(
            name="verification_agent",
            description=VERIFICATION_AGENT_DESC.get(resolved_language, VERIFICATION_AGENT_DESC["en"]),
        ),
        system_prompt=system_prompt or (
            VERIFICATION_AGENT_SYSTEM_PROMPT_CN if resolved_language == "cn"
            else VERIFICATION_AGENT_SYSTEM_PROMPT_EN
        ),
        tools=tools,
        mcps=mcps,
        subagents=subagents,
        rails=rails if rails is not None else [SysOperationRail(), VerificationRail()],
        enable_task_loop=enable_task_loop,
        max_iterations=max_iterations,
        ...
    )
```

**Go 问题**：缺少 `internal/agentcore/harness/verification_agent_factory.go`

**修复方案**：创建 `internal/agentcore/harness/verification_agent_factory.go`，实现 `CreateVerificationAgent`：

```go
func CreateVerificationAgent(ctx context.Context, params *hschema.SubagentCreateParams) (*DeepAgent, error) {
    language := hpromts.ResolveLanguage(params.Language)

    // 默认 Rails：SysOperationRail() + VerificationRail()
    // 对齐 Python: rails=rails if rails is not None else [SysOperationRail(), VerificationRail()]
    finalRails := params.Rails
    if finalRails == nil {
        finalRails = []sainterfaces.AgentRail{
            rails.NewSysOperationRail(),
            subagent.NewVerificationRail(),
        }
    }

    // 默认 AgentCard
    card := params.Card
    if card == nil {
        desc := subagents.DefaultVerificationAgentDescription(language)
        card = agentschema.NewAgentCard(
            agentschema.WithAgentName(subagents.VerificationAgentFactoryName),
            agentschema.WithAgentDescription(desc),
        )
    }

    // 默认 SystemPrompt
    systemPrompt := params.SystemPrompt
    if systemPrompt == "" {
        systemPrompt = subagents.DefaultVerificationAgentSystemPrompt(language)
    }

    // 默认 MaxIterations = 40
    maxIterations := params.MaxIterations
    if maxIterations == 0 {
        maxIterations = 40
    }

    // RestrictToWorkDir: 默认 false
    restrictToWorkDir := false
    if params.RestrictToWorkDir != nil {
        restrictToWorkDir = *params.RestrictToWorkDir
    }

    return CreateDeepAgent(ctx, hconfig.CreateDeepAgentParams{
        Model:             params.Model,
        Card:              card,
        SystemPrompt:      systemPrompt,
        ToolCards:         params.Tools,
        ToolInstances:     params.ToolInstances,
        Mcps:              params.Mcps,
        Rails:             finalRails,
        EnableTaskLoop:    params.EnableTaskLoop,
        MaxIterations:     maxIterations,
        Workspace:         params.Workspace,
        Skills:            params.Skills,
        Backend:           params.Backend,
        SysOperation:      params.SysOperation,
        Language:          language,
        PromptMode:        params.PromptMode,
        EnableTaskPlanning: params.EnablePlanMode,
        RestrictToWorkDir: &restrictToWorkDir,
    })
}
```

---

### S02: VerificationAgent 默认回退语言错误

**章节**：9.29 VerificationAgent

**问题描述**：Python 中 VerificationAgent 的描述回退语言是 `"en"`，Go 中回退到 `"cn"`。这导致当 `resolved_language` 既不是 `"cn"` 也不是 `"en"`（如 `"ja"`、`"ko"` 等）时，Go 使用中文描述，Python 使用英文描述。

**Python 样例**：
```python
# openjiuwen/harness/subagents/verification_agent.py L298
description=VERIFICATION_AGENT_DESC.get(resolved_language, VERIFICATION_AGENT_DESC["en"]),
#                                                          ^^^ 回退到 "en"
```

**Go 问题代码**：
```go
// internal/agentcore/harness/subagents/verification_agent.go L237-238
desc := defaultVerificationAgentDescription[language]
if desc == "" {
    desc = defaultVerificationAgentDescription["cn"]  // ❌ 应该回退到 "en"
}

// L317-319
func DefaultVerificationAgentDescription(language string) string {
    if s, ok := defaultVerificationAgentDescription[language]; ok && s != "" {
        return s
    }
    return defaultVerificationAgentDescription["cn"]  // ❌ 应该回退到 "en"
}
```

**修复方案**：将 `verification_agent.go` 中所有 `"cn"` 回退改为 `"en"`，涉及 3 处：

1. `BuildVerificationAgentConfig` L238：`"cn"` → `"en"`
2. `DefaultVerificationAgentDescription` L319：`"cn"` → `"en"`
3. `DefaultVerificationAgentSystemPrompt` L310：`"cn"` → `"en"`

系统提示词的 Python 实现使用 `if resolved_language == "cn" else ... EN` 模式（非 cn 即 en），Go 的 map 回退也应使用 `"en"` 保持一致。

---

### S03: `createAgent` 中 `subMode` 空串与 Python `None` 行为不一致

**章节**：10.3.12 AgentManager

**问题描述**：Python 中 `create_instance(config, mode=mode_key, sub_mode=sub_mode_key or None)`，当 `sub_mode_key` 为空字符串时会传 `None`。Go 中 `normalizeSubMode` 返回空字符串 `""`，不会转为 `nil`/其他零值，直接传给 `CreateInstance`。

**Python 样例**：
```python
# jiuwenswarm/server/runtime/agent_manager.py L130
await agent.create_instance(config, mode=mode_key, sub_mode=sub_mode_key or None)
#                                                              ^^^^^^^^^^^^^^^^^^^^
# sub_mode_key 为空串 "" 时，Python 传 None
```

**Go 问题代码**：
```go
// internal/swarm/server/runtime/agent_manager.go L570
agent, err := factory(config, modeKey, subModeKey)
// subModeKey 为空串 "" 时，Go 直接传空串
```

**修复方案**：确认 `UapClaw.CreateInstance` 对空字符串 `subMode` 和 Python `None` 的处理是否等价。如果 `CreateInstance` 内部对空字符串和 nil 的行为不同，需要在 `defaultAgentFactory` 中将空字符串转换为等效的零值：

```go
agent, err := factory(config, modeKey, subModeKey)
// 如果 subModeKey 为空，确认 CreateInstance 行为与 Python None 一致
// 如果不一致，需要：
effectiveSubMode := subModeKey
if effectiveSubMode == "" {
    effectiveSubMode = "" // 或传 nil/特殊值
}
```

---

### S04: `ReloadAgentsConfig`/`createAgent` 空字符串 env 值行为与 Python 不一致

**章节**：10.3.12 AgentManager

**问题描述**：Python 中 `env_value=None` 时 unsetenv；非 None 时一律 `str(env_value)` 设置，空字符串 `""` 也会被设置为 `os.environ[key] = ""`。Go 额外加了 `s == ""` 条件，空字符串值也会 `os.Unsetenv(key)`，与 Python 行为不一致。

**Python 样例**：
```python
# jiuwenswarm/server/runtime/agent_manager.py L311-316
for env_key, env_value in self._latest_env_overrides.items():
    key = str(env_key)
    if env_value is None:
        os.environ.pop(key, None)
    else:
        os.environ[key] = str(env_value)  # 空字符串 "" 也走这里
```

**Go 问题代码**：
```go
// internal/swarm/server/runtime/agent_manager.go L350-360
for key, val := range envOverrides {
    s, ok := val.(string)
    if !ok && val != nil {
        s = fmt.Sprintf("%v", val)
    }
    if val == nil || s == "" {  // ❌ s == "" 导致空字符串也被 unset
        _ = os.Unsetenv(key)
    } else {
        _ = os.Setenv(key, s)
    }
}
```

**修复方案**：移除 `s == ""` 条件，空字符串应 `os.Setenv(key, "")`：

```go
for key, val := range envOverrides {
    if val == nil {
        _ = os.Unsetenv(key)
        continue
    }
    s, ok := val.(string)
    if !ok {
        s = fmt.Sprintf("%v", val)
    }
    _ = os.Setenv(key, s)
}
```

同样的问题也存在于 `createAgent` 中的 env 注入逻辑（L529-538），需一并修复。

---

### S05: `$name@target body` 格式正则解析结果错误

**章节**：9.59b Interaction 层 — `router.go`

**问题描述**：Python 的 `_HUMAN_AGENT_PREFIX_RE` 使用 lookahead `(?=@)` 匹配 `$name@target` 格式，group(2) 返回 `"@target body"`（保留 `@` 前缀），后续 recipient 循环能正确解析出 `@target`。Go 将此 regex 拆为两个正则，其中 `humanAgentPrefixAtRe` 消费了 `@` 符号，导致 recipient 循环无法匹配。

**Python 样例**：
```python
# openjiuwen/agent_teams/interaction/router.py L56
_HUMAN_AGENT_PREFIX_RE = re.compile(r"^\$([^\s@]+)(?:\s+|(?=@))([\s\S]*)$")

# 输入: "$human_agent@alice hello"
# match.groups() → ('human_agent', '@alice hello')
# rest = '@alice hello' → recipientRe 匹配 '@alice ' → recipients=['alice'], finalBody='hello'
# 结果: HumanAgentMessage(body='hello', sender='human_agent', target='alice')
```

**Go 问题代码**：
```go
// internal/agent_teams/interaction/router.go L36
humanAgentPrefixAtRe = regexp.MustCompile(r"^\$([^\s@]+)@([\s\S]*)$")

// 输入: "$human_agent@alice hello"
// match → ["human_agent", "alice hello"]
// rest = "alice hello" → recipientRe 不匹配 → recipients=[], finalBody="alice hello"
// 结果: HumanAgentMessage(body="alice hello", sender="human_agent") ← target 丢失！
```

**修复方案**：在 `ParseInteractStr` 中匹配到 `humanAgentPrefixAtRe` 后，给 rest 前缀补上 `"@"`：

```go
match := humanAgentPrefixAtRe.FindStringSubmatch(rest)
if match != nil {
    sender = match[1]
    rest = "@" + match[2]  // ← 补回 @ 前缀，使其与 Python 行为一致
    rest = trimLeadingSpaces(rest)
    isHumanAgent = true
}
```

修复后添加测试用例：
```go
func TestParseInteractStr_美元前缀At接收者(t *testing.T) {
    result := ParseInteractStr("$human_agent@alice hello")
    if len(result) != 1 {
        t.Fatalf("期望 1 个载荷，实际 %d", len(result))
    }
    msg, ok := result[0].(*HumanAgentMessage)
    if !ok {
        t.Fatal("期望 HumanAgentMessage")
    }
    if msg.Body() != "hello" {
        t.Errorf("Body = %q, want 'hello'", msg.Body())
    }
    if msg.Sender() != "human_agent" {
        t.Errorf("Sender = %q, want 'human_agent'", msg.Sender())
    }
    if msg.Target() == nil || *msg.Target() != "alice" {
        t.Errorf("Target = %v, want 'alice'", msg.Target())
    }
}
```

---

### S06: `InteractGate.Admit()` 并发时 `drained` 通道可能导致死锁

**章节**：9.59b Interaction 层 — `runtime/manager.go`

**问题描述**：每次 `Admit()` 都 `make(chan struct{})` 创建新通道。如果 `Admit()` 被多次调用，后一次的 `drained` 通道覆盖前一次。`CloseAndDrain` 可能在第一次 Admit 后拿到 `drained_1` 引用，但 `ConsumeDone` 只 close 最后的 `drained_2`，导致 `CloseAndDrain` 永远阻塞在 `drained_1` 上——**死锁**。

**Python 样例**：
```python
# openjiuwen/agent_teams/runtime/gate.py
async def admit(self) -> AdmissionTicket | None:
    async with self._lock:
        if self._closed.is_set():
            return None
        self._inflight += 1
        self._drained.clear()  # Event.clear() 只重置信号，不创建新对象
        return AdmissionTicket(gate=self)
```

Python 使用 `asyncio.Event`，`clear()`/`set()` 在同一个对象上操作，不存在引用丢失问题。

**Go 问题代码**：
```go
// internal/agent_teams/runtime/manager.go
func (g *InteractGate) Admit() *AdmissionTicket {
    g.mu.Lock()
    defer g.mu.Unlock()
    if g.closed {
        return nil
    }
    g.inflight++
    g.drained = make(chan struct{})  // ← 每次创建新通道，旧通道引用丢失
    return &AdmissionTicket{gate: g}
}
```

**死锁场景**：
```
goroutine A: Admit() → g.drained = drained_1
goroutine B: Admit() → g.drained = drained_2  (drained_1 引用丢失)
goroutine C: CloseAndDrain() → 等待 drained_1（从 Admit 前获取的引用）
goroutine A: ConsumeDone() → close(drained_2)  // close 的是 drained_2
goroutine C: 永远阻塞在 drained_1 ← 死锁！
```

**修复方案**：使用 `sync.Cond` 替代 `chan struct{}`：

```go
type InteractGate struct {
    closed   bool
    inflight int
    mu       sync.Mutex
    cond     *sync.Cond
}

func NewInteractGate() *InteractGate {
    g := &InteractGate{}
    g.cond = sync.NewCond(&g.mu)
    return g
}

func (g *InteractGate) Admit() *AdmissionTicket {
    g.mu.Lock()
    defer g.mu.Unlock()
    if g.closed {
        return nil
    }
    g.inflight++
    return &AdmissionTicket{gate: g}
}

func (g *InteractGate) ConsumeDone() {
    g.mu.Lock()
    g.inflight--
    if g.inflight == 0 {
        g.cond.Broadcast()  // 通知所有等待者
    }
    g.mu.Unlock()
}

func (g *InteractGate) CloseAndDrain() {
    g.mu.Lock()
    g.closed = true
    for g.inflight > 0 {
        g.cond.Wait()  // 等待 inflight 降为 0
    }
    g.mu.Unlock()
}
```

或者保持 chan 方案但修复逻辑——只在 inflight==0 时 drained 已关闭，Admit 时如果 drained 已关闭则重新创建；否则不创建：

```go
func (g *InteractGate) Admit() *AdmissionTicket {
    g.mu.Lock()
    defer g.mu.Unlock()
    if g.closed {
        return nil
    }
    g.inflight++
    select {
    case <-g.drained:
        // drained 已关闭（inflight 刚从 0 变 1），重新创建
        g.drained = make(chan struct{})
    default:
        // drained 未关闭（inflight > 0），不需要新通道
    }
    return &AdmissionTicket{gate: g}
}
```

---

### S07: `TextualParameter.Gradients` 类型为 `map[string]string`，Python 为 `Dict[str,Any]`

**章节**：9.72a InstructionOptimizer

**问题描述**：Python 的 `TextualParameter.gradients` 是 `Dict[str, Any]`，值可以是 `None`、`str` 或 `list`。Go 实现用了 `map[string]string`，只能存字符串。虽然当前 InstructionOptimizer 中梯度都是字符串，但 `None` 和空字符串在语义上有区别——Python 中 `None` 表示"未设置/已清除"，空字符串可能有其他含义。

**Python 样例**：
```python
# base.py TextualParameter
self.gradients: Dict[str, Any] = {}  # target -> gradient value (str or list)
param.set_gradient("system_prompt_optimized", None)  # None 值
param.set_gradient("system_prompt", textual_gradient)  # str
sys_val = param.get_gradient("system_prompt_optimized")  # 可能是 None
if sys_val:  # None 为 falsy
    updates[(op_id, "system_prompt")] = sys_val
```

**Go 问题代码**：
```go
// internal/evolving/optimizer/base.go TextualParameter
Gradients map[string]string  // 只支持 string
param.SetGradient("system_prompt_optimized", "")  // 空字符串代替 None
if sysVal := param.GetGradient("system_prompt_optimized"); sysVal != "" {
```

**修复方案**：将 `Gradients` 改为 `map[string]any`，`GetGradient` 返回 `any`，调用方自行断言。同时添加注释说明设计决策：

```go
type TextualParameter struct {
    // ...
    // Gradients 梯度容器，对齐 Python: Dict[str, Any]
    // 值可以是 nil（等价于 Python None）、string 或 []string
    Gradients map[string]any
}

func (p *TextualParameter) GetGradient(key string) any {
    return p.Gradients[key]
}

func (p *TextualParameter) SetGradient(key string, value any) {
    if p.Gradients == nil {
        p.Gradients = make(map[string]any)
    }
    p.Gradients[key] = value
}
```

---

### S08: `ToolOptimizerBase.Bind` 方法为空实现

**章节**：9.72b ToolOptimizer

**问题描述**：`ToolOptimizerBase.Bind` 方法签名使用 `map[string]any` 而非 `map[string]operator.Operator`，且方法体为空（直接返回 0）。这意味着 ToolOptimizer 无法绑定任何 Operator，无法与 Trainer 集成。已有 ⤵️ 标记。

**Go 问题代码**：
```go
// internal/evolving/optimizer/tool_call/base.go
func (b *ToolOptimizerBase) Bind(operators map[string]any, targets []string, config map[string]any) int {
    // ⤵️ 9.70: 等待 Trainer 实现后回填 Operator 类型转换
    return 0
}
```

**修复方案**：等待 9.70 Trainer 实现后回填，将签名改为 `map[string]operator.Operator`，并委托 `b.BaseOptimizerMixin.Bind()`。

---

### S09: `ToolOptimizerBase.Step` 返回类型不匹配

**章节**：9.72b ToolOptimizer

**问题描述**：Python 的 `_step` 返回 `Dict[tuple[str, str], Any]`，即 `{(op_id, target): value}` 的映射。Go 返回 `map[string]any`，而不是 `map[schema.UpdateKey]any`。与其他优化器（如 InstructionOptimizer.Step）的返回类型不一致，无法实现 BaseOptimizer 接口。

**Go 问题代码**：
```go
// internal/evolving/optimizer/tool_call/base.go
func (b *ToolOptimizerBase) Step() map[string]any {
    return map[string]any{}
}
```

**修复方案**：将 Step 返回类型改为 `map[schema.UpdateKey]any` 以实现 BaseOptimizer 接口：

```go
func (b *ToolOptimizerBase) Step() map[schema.UpdateKey]any {
    return map[schema.UpdateKey]any{}
}
```

---

## 一般问题详细分析

### G01: `BuildCodeAgentConfig` 中 `EnablePlanMode` 不像 Python 那样强制 `True`

**章节**：9.27 CodeAgent

**问题描述**：Python `create_code_agent` 显式传 `enable_task_planning=True`，强制 CodeAgent 启用任务规划。Go 的 `BuildCodeAgentConfig` 只做 `cfg.EnablePlanMode = params.EnablePlanMode`（默认 false），在 config 层不强制。不过 `CreateCodeAgent` 工厂层已正确设置 `EnableTaskPlanning: true`。

**Python 样例**：
```python
# openjiuwen/harness/subagents/code_agent.py L275
return create_deep_agent(
    model=model,
    ...
    enable_task_planning=True,  # 强制 True
    **config_kwargs,
)
```

**Go 代码**：
```go
// code_agent.go L106 — config 层不强制
cfg.EnablePlanMode = params.EnablePlanMode

// code_agent_factory.go L122 — factory 层强制
EnableTaskPlanning: true,  // 关键区别：CodeAgent 默认启用任务规划
```

**结论**：由于实际实例化通过 factory 层（`CreateCodeAgent`）完成，而 factory 层已正确强制 `true`，这不是功能 bug。但如果有人直接使用 `BuildCodeAgentConfig` 构建 config 再手动实例化，会缺少 `enable_task_planning=True` 的行为。

**修复方案**：在 `BuildCodeAgentConfig` 注释中添加明确说明，或在 `EnablePlanMode` 默认值为 true：

```go
// CodeAgent 默认启用规划模式
// 对齐 Python: create_code_agent 传 enable_task_planning=True
if !params.EnablePlanMode {
    cfg.EnablePlanMode = true  // CodeAgent 默认 true
} else {
    cfg.EnablePlanMode = params.EnablePlanMode
}
```

---

### G02: `BuildCodeAgentConfig` 中 `FactoryKwargs` 永远为 nil，不传 `embedding_config`

**章节**：9.27 CodeAgent

**问题描述**：Python `build_code_agent_config` 中，当 `embedding_config` 非空时，会将其放入 `factory_kwargs` 传递给 SubAgentConfig。Go 中 `cfg.FactoryKwargs = nil`，永远不传。

**Python 样例**：
```python
# openjiuwen/harness/subagents/code_agent.py L141-143
factory_kwargs: Dict[str, Any] = {}
if embedding_config is not None:
    factory_kwargs["embedding_config"] = embedding_config
```

**Go 问题代码**：
```go
// code_agent.go L104
cfg.FactoryKwargs = nil  // 永远为 nil
```

**修复方案**：等 CodingMemoryRail 回填时（9.19-23）一并修复。当前 CodingMemoryRail 整体未实现，FactoryKwargs 为 nil 是预期状态。

---

### G03: `CancelAllInflightWork` 缺少 `reason` 参数

**章节**：10.3.12 AgentManager

**Python 样例**：
```python
# jiuwenswarm/server/runtime/agent_manager.py L184
async def cancel_all_inflight_work(self, reason: str = "[gateway ws disconnect] ") -> None:
    for modes in list(self.agents.values()):
        for agent in list(modes.values()):
            try:
                await agent.cancel_inflight_work(reason)
            except Exception:
                logger.exception("[AgentManager] cancel_inflight_work failed")
```

**Go 问题代码**：
```go
func (am *AgentManager) CancelAllInflightWork(ctx context.Context) error {
    // 没有 reason 参数
    _ = agent.CancelInflightWork()  // 也没有 reason
}
```

**修复方案**：给 `UapClaw.CancelInflightWork` 和 `AgentManager.CancelAllInflightWork` 都加上 `reason string` 参数：

```go
func (am *AgentManager) CancelAllInflightWork(ctx context.Context, reason string) error {
    // ...
    if err := agent.CancelInflightWork(reason); err != nil {
        logger.Warn(amLogComponent).Err(err).Msg("[AgentManager] cancel_inflight_work failed")
    }
}
```

---

### G04: `CancelAllInflightWork` 吞异常无日志

**章节**：10.3.12 AgentManager

**修复方案**：在忽略错误时添加 Warn 日志（见 G03 修复方案）。

---

### G05: `RecreateAgent` cleanup 静默忽略错误

**章节**：10.3.12 AgentManager

**Python 样例**：
```python
# jiuwenswarm/server/runtime/agent_manager.py L381-390
try:
    await agent.cleanup()
except Exception as exc:
    logger.warning("[AgentManager] recreate cleanup failed (mode=%s): %s", mode_key, exc)
```

**Go 问题代码**：
```go
_ = entry.agent.Cleanup()  // 静默忽略
```

**修复方案**：
```go
if err := entry.agent.Cleanup(); err != nil {
    logger.Warn(amLogComponent).Err(err).Str("cache_key", entry.cacheKey).
        Msg("[AgentManager] recreate cleanup failed")
}
```

---

### G06: `Cleanup` 静默忽略错误

**章节**：10.3.12 AgentManager

同 G05，在 `Cleanup` 方法中也静默忽略了 cleanup 错误，需补充 Warn 日志。

---

### G07: `ProcessMessage`/`Stream` SwitchMode 错误被忽略

**章节**：10.3.12 AgentManager

**问题描述**：Go 新增的 SwitchMode 逻辑（Python 中不存在），用 `_ =` 忽略了错误。如果 SwitchMode 失败，后续 ProcessMessage 可能在错误的模式下执行。

**Go 问题代码**：
```go
if mode == "code" && subMode != "team" {
    _ = agent.SwitchMode(ctx, sid, subMode)  // 错误被忽略
}
```

**修复方案**：至少记录 Warn 日志：
```go
if mode == "code" && subMode != "team" {
    if err := agent.SwitchMode(ctx, sid, subMode); err != nil {
        logger.Warn(amLogComponent).Err(err).Str("sub_mode", subMode).
            Msg("[AgentManager] SwitchMode failed, continuing in current mode")
    }
}
```

---

### G08: `GetClientCapabilities` channelKey normalize 不一致

**章节**：10.3.12 AgentManager

**问题描述**：Python 用 `str(channel_id or "").strip()`，空字符串变为 `""`。Go 用 `strings.TrimSpace(channelID)`，空字符串仍为 `""`。但 Python 的 `or` 会让 `None` 变为 `""`，而 Go 的空字符串可能不是 `"default"`。

**修复方案**：使用 `normalizeChannelID(channelID)` 替代 `strings.TrimSpace(channelID)`。

---

### G09: Go 缺少 `runtime/dispatch.py`（RunActionKind/decide_run_action）

**章节**：9.59b Interaction 层 — `runtime/`

**问题描述**：Python 有完整的 `runtime/dispatch.py`（RunActionKind 枚举、RunAction 数据类、decide_run_action 函数），Go 完全缺失。Manager.Activate 是空 stub，但 dispatch 决策逻辑是 activate 的核心。

**Python 样例**：
```python
# openjiuwen/agent_teams/runtime/dispatch.py
class RunActionKind(str, Enum):
    CREATE = "create"
    NEW_TEAM_IN_SESSION = "new_team_in_session"
    COLD_RECOVER = "cold_recover"
    RESUME_FROM_PAUSE = "resume_from_pause"
    REJECT_RUNNING = "reject_running"
    REJECT_ORPHANED = "reject_orphaned"
    REJECT_INCONSISTENT = "reject_inconsistent"

def decide_run_action(...) -> RunAction:
    # 完整的状态机逻辑
```

**修复方案**：创建 `runtime/dispatch.go`，移植 RunActionKind、RunAction、decide_run_action。这些是纯决策逻辑，不依赖 TeamBackend 等外部接口，可以现在就实现。当前标记为 ⤵️ 待 9.62。

---

### G10: Go 缺少 `runtime/metadata.py`（namespace 访问层）

**章节**：9.59b Interaction 层 — `runtime/`

**Python 样例**：
```python
# openjiuwen/agent_teams/runtime/metadata.py
def read_teams_bucket(session) -> dict[str, dict[str, Any]]:
    teams = session.get_state(TEAMS_KEY)
    if not isinstance(teams, dict):
        return {}
    return teams

def read_team_namespace(session, team_name: str) -> dict[str, Any] | None:
    bucket = read_teams_bucket(session).get(team_name)
    ...
```

**修复方案**：创建 `runtime/metadata.go`，移植 metadata 函数。等 9.62 回填后实现。

---

### G11: Go 缺少 `runtime/team_plan.py`（is_team_plan_enabled）

**章节**：9.59b Interaction 层 — `runtime/`

**Python 样例**：
```python
# openjiuwen/agent_teams/runtime/team_plan.py
def is_team_plan_enabled(spec: Any) -> bool:
    return bool(_get_field(spec, "enable_team_plan", default=False))
```

**修复方案**：在 runtime 包中添加 `team_plan.go`，移植该函数。

---

### G12: Go `TeamRuntimeManager` 缺少多个方法

**章节**：9.59b Interaction 层 — `runtime/manager.go`

**问题描述**：Python `TeamRuntimeManager` 有大量方法，Go 只实现了 Interact + stub。完全缺失的方法：
- `FinalizeMember(agent)` — 非 leader 运行周期终结
- `GetMonitor(team_name, session_id, hide_dm)` — 返回 TeamMonitor
- `ListActiveTeams()` — 列出所有活跃团队
- `ReleaseSession(session_id, force)` — 释放 session 动态表
- `ResolveTeamSessionReleaseInfo(session_id)` — 解析 session 发布信息
- `DeleteTeam` 参数签名不一致（Go 只有 teamName+sessionID，Python 接受 session_ids 列表 + force）

**修复方案**：逐个移植缺失方法。`ListActiveTeams` 和 `GetMonitor` 不依赖 TeamBackend，可以立即实现。

---

### G13: `HumanAgentInbox.Send` 广播路径缺少 `broadcast_failed` 处理

**章节**：9.59b Interaction 层 — `interaction/human_agent_inbox.go`

**Python 样例**：
```python
# openjiuwen/agent_teams/interaction/human_agent_inbox.py
if to in BROADCAST_TARGETS:
    msg_id = await self._mm.broadcast_message(content=body, from_member_name=resolved_sender)
    if msg_id is None:
        return DeliverResult.failure("broadcast_failed")
    return DeliverResult.success(msg_id)
```

**Go 问题代码**：
```go
if BroadcastTargets[*to] {
    msgID := "stub-ha-broadcast-msg-id"  // ← stub 总是成功
    return NewDeliverResultSuccess(&msgID), nil
}
```

**修复方案**：⤵️ 待 9.55 回填 TeamMessageManager 后，需加上 `msgID == nil` 检查返回 `broadcast_failed`。

---

### G14: `UserInbox.Direct`/`Broadcast` 缺少 `send_failed`/`broadcast_failed` 处理

**章节**：9.59b Interaction 层 — `interaction/user_inbox.go`

同 G13，Python UserInbox 检查 `msg_id is None` 返回 `failure("send_failed:{target}")` 或 `failure("broadcast_failed")`，Go stub 总是成功。待 TeamMessageManager 回填时需确保检查 nil msgID。

---

### G15: `optimizeBoth` 返回空字符串而非 nil

**章节**：9.72a InstructionOptimizer

**问题描述**：Python 中 `_optimize_both` 返回 `(None, None)` 表示"无法优化"，Go 中返回 `("", "", nil)`。在 `restorePlaceholders` 失败时，Go 返回可能不完整的 prompt 而非 nil，可能导致应用了不完整的优化结果。

**修复方案**：`restorePlaceholders` 失败时，考虑返回空字符串（等价于 Python 的 None），表示优化无效。

---

### G16: `SimpleAPIWrapperFromCallable.Call` 不自动包装 `{'response': output}`

**章节**：9.72b ToolOptimizer

**Python 样例**：
```python
# customized_api.py SimpleAPIWrapperFromCallable.__call__
def __call__(self, tool, tool_input):
    fn = self.functions.get(self.fn_call_name)
    try:
        output = fn(params)  # fn 是 tool_callable
        return json.dumps({'response': output}, ensure_ascii=False), 0
    except Exception as e:
        return json.dumps({"error": ..., "response": ""}), 12
```

Go 中 `w.callable(tool, toolInput)` 直接返回 JSON 字符串和状态码，没有自动包装 `{'response': output}`。callable 的实现者必须自己负责包装输出格式。

**修复方案**：在 Call 方法内部包装 `{'response': output}`（对齐 Python），或在文档中明确 callable 的输出格式约定。

---

### G17: `copyMap` 是浅拷贝，Python 使用 deepcopy

**章节**：9.72b ToolOptimizer — `eval.go`

**Python 样例**：
```python
tool_for_opt = copy.deepcopy(tool)  # 深拷贝
```

**Go 问题代码**：
```go
func copyMap(m map[string]any) map[string]any {
    result := make(map[string]any, len(m))
    for k, v := range m {
        result[k] = v  // 浅拷贝！嵌套 map 是共享的
    }
    return result
}
```

**修复方案**：将 `copyMap` 替换为 `deepCopyMap`（example_method.go 中已有实现），或将 `deepCopyMap` 提取为公共函数统一使用。

---

### G18: `SimpleEval.evaluateSingleExample` 缺少整体异常捕获

**章节**：9.72b ToolOptimizer — `eval.go`

**Python 样例**：
```python
def _evaluate_single_example(self, example):
    try:
        # ... 所有逻辑
    except Exception as e:
        logger.error(f"Error evaluating example {example_id}: {str(e)}")
        return { 'instruction': instruction, 'fn_call_score': 0.0, ... }
```

Go 版本只在 `generateFunctionCall` 处理了错误，后续步骤（API 调用、JSON 解析）的错误处理不够全面。

**修复方案**：在 evaluateSingleExample 中添加对 API 调用异常的完整处理，或在 Eval 方法层面添加 recover。

---

### G19: `ToolOptimizerBase.OptimizeTool` 中 `tool_name` 硬编码

**章节**：9.72b ToolOptimizer — `base.go`

**Python 样例**：
```python
f"{kwargs.get('tool_name','tool')}.json"  # 可通过 kwargs 覆盖
```

Go 中硬编码为 `"tool"`，没有提供 `WithToolName` 选项。

**修复方案**：添加 `WithToolName(name string) ToolOptimizerBaseOption` 选项函数。

---

### G20: `BeamSearch.Search` 超时时返回结果可能包含根节点

**章节**：9.72b ToolOptimizer — `beam_search.go`

**问题描述**：超时时 `bestNodes` 包含 root（depth=0），可能返回未优化的根节点结果。

**修复方案**：超时时也应过滤 `depth > 0` 的节点，除非没有任何深度 > 0 的节点。

---

## 提示问题汇总

### T01: CodingMemoryRail 回填未实现

**章节**：9.27 CodeAgent ⤵️ 9.19-23

确认 CodingMemoryRail 整体未实现，`CreateCodeAgent` 中代码被注释掉并有 ⤵️ 标记。等 9.19-23 章节实现时回填。

### T02: ExploreAgent 缺少 `search_via_bash` 参数

Python `build_explore_agent_config` 和 `create_explore_agent` 中声明了 `search_via_bash: bool = False` 参数，但未传递给 `create_deep_agent`。这是 Python 端的预留参数，Go 暂不实现不影响功能。

### T03: ExploreAgent 提示词中工具名硬编码

Python 使用模块级常量 `_EXPLORE_TOOL_BASH` 等，Go 直接硬编码字符串。风格差异，不影响功能。建议后续提取常量提升可维护性。

### T04: `DEFAULT_CODE_AGENT_DESCRIPTION` EN 文本细微格式差异

Python 三引号保留换行+缩进，Go 拼接后无换行。这是三引号格式副作用，AgentCard 描述中不应包含换行+缩进，Go 处理合理。

### T05-T06: ACP 分支和 team evolution config 未实现

确认 ⤵️ 标记准确，这些是延后实现的功能。

### T07: `envOverrides` nil map 安全性

Go 中迭代 nil map 不会 panic，但建议加 nil 保护检查。

### T08: Interaction 层 messageManager 为 stub

Interaction 层中 `DeliverDirect`/`UserInbox`/`HumanAgentInbox` 中 `messageManager` 参数为 stub（返回硬编码 msg-id），等 9.55 TeamBackend 回填后实现。

---

## ⤵️ 占位标记确认汇总

| 标记 | 位置 | 确认状态 |
|------|------|---------|
| ⤵️ 9.19-23 CodingMemoryRail | `code_agent_factory.go` L65-69 | ✅ 确认未实现 |
| ⤵️ 9.55 TeamBackend | `interaction/human_agent_inbox.go` 多处 | ✅ 确认未实现 |
| ⤵️ 9.55 messageManager | `interaction/user_inbox.go` 多处 | ✅ 确认未实现 |
| ⤵️ 9.55 agentLookup | `runtime/manager.go` L284 | ✅ 确认未实现 |
| ⤵️ 9.62 CoordinationKernel | `runtime/manager.go` 生命周期方法 | ✅ 确认未实现 |
| ⤵️ 9.70 ToolOptimizerBase.Bind | `optimizer/tool_call/base.go` L264 | ✅ 确认未实现 |
| ⤵️ ACP Initialize | `agent_manager.go` L230-242 | ✅ 确认未实现 |
| ⤵️ ACP CreateSession | `agent_manager.go` L257-261 | ✅ 确认未实现 |
| ⤵️ ACP GetAgent | `agent_manager.go` L165-166 | ✅ 确认未实现 |
| ⤵️ 10.3.2 Team evolution config | `agent_manager.go` L386-389 | ✅ 确认未实现 |
| ⤵️ 9.64 EmbeddingConfig | `agent_teams/memory/config.go` | ✅ 确认未实现 |
| ⤵️ 9.65 Messager | `agent_teams/messager/base.go` | ✅ 确认未实现 |

---

## 9.72a/9.72b Optimizer 审查结论

### 9.72a InstructionOptimizer

Go 实现与 Python 高度对齐，核心方法 `backward`/`step`/`generateTextualGradient`/`optimizeBoth`/`optimizeSingle`/`formatBadCases`/`restorePlaceholders`/`extractTag` 全部实现，逻辑步骤一一对应。

**遗留问题**：
1. `TextualParameter.Gradients` 类型限制为 `map[string]string`（S07），与 Python 的 `Dict[str, Any]` 不对齐
2. `optimizeBoth`/`optimizeSingle` 返回空字符串而非 nil（G15），restorePlaceholders 失败时可能返回不完整 prompt
3. `SelectSignals` 中 `score == 0` 的 any 类型比较可能失败（T13）

### 9.72b ToolOptimizer

Go 实现完整移植了 Python `tool_call/utils/` 下的全部组件：
- `BeamSearch` — goroutine 并行替代 ThreadPoolExecutor
- `APICallToExampleMethod` — 示例阶段
- `ToolDescriptionMethod` — 描述阶段
- `ToolDescriptionReviewer` — 三步后处理（clean→cross_check→translate→format）
- `SimpleEval` — LLM Function Calling 评估
- `CustomizedPipeline` — 完整流水线
- `APIWrapper`/`MCP 封装`

设计决策已明确记录（不复刻 rits.py 复用 llm_resilience、SimpleEval 持有 Model、单包扁平）。

**遗留问题**：
1. `ToolOptimizerBase.Bind` 方法为空实现（S08），无法与 Trainer 集成
2. `ToolOptimizerBase.Step` 返回类型不匹配（S09），未实现 BaseOptimizer 接口
3. `copyMap` 浅拷贝 vs Python deepcopy（G17），可能导致并发修改问题
4. `SimpleAPIWrapperFromCallable.Call` 不自动包装输出（G16）

---

## 9.59b Interaction 层审查结论

Interaction 层 Go 实现与 Python 高度对齐：
- `payload.go` — GodViewMessage/OperatorMessage/HumanAgentMessage/DeliverResult 完整实现
- `router.go` — ParseInteractStr/ParseMention/ResolveTargets/DeliverDirect 完整实现
- `user_inbox.go` — Direct/Broadcast 方法结构完整，messageManager 为 stub
- `human_agent_inbox.go` — DeliverDirect/Broadcast/GetHumanAgentNames 结构完整，team backend 为 stub
- `runtime/` — InteractGate/TeamRuntimePool/TeamRuntimeManager 主体逻辑完整，生命周期方法为 stub

**关键缺陷**：
1. `$name@target` 正则解析 bug（S05），@ 前缀被消费导致 target 丢失
2. InteractGate 并发死锁风险（S06），drained 通道引用覆盖

**系统性限制**：所有依赖 `TeamBackend`/`TeamMessageManager` 的方法都是 stub 实现，等 9.55 回填后激活。

---

## 建议修复优先级

1. **P0 — 立即修复（功能缺陷）**：
   - S05（`$name@target` 正则 bug — @ 前缀被消费，立即可修）
   - S04（空字符串 env 行为不一致）
   - S06（InteractGate 并发死锁风险）

2. **P1 — 尽快修复**：
   - S01（缺 VerificationAgent 工厂函数）
   - S02（回退语言 cn→en）
   - S03（subMode 空串 vs None）
   - S07（TextualParameter Gradients 类型限制）
   - G04-G06（静默忽略错误补日志）

3. **P2 — 计划修复**：
   - S08/S09（ToolOptimizerBase Bind/Step — ⤵️ 等 9.70 回填）
   - G01（config 层 EnablePlanMode 说明）
   - G03（reason 参数）
   - G07-G08（AgentManager 日志/normalize）
   - G09-G12（runtime 缺失模块 — ⤵️ 等 9.62 回填）
   - G15-G20（Optimizer 细节差异）

4. **P3 — 后续优化**：所有提示级别问题

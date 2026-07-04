# 8.35 & 8.36 HierarchicalTeam Python 对齐修复设计

## 背景

对照 Python 源码（`hierarchical_msgbus/` 和 `hierarchical_tools/`）逐项检查 Go 实现，发现 7 个需要修复的差异。

## 修复清单

### 修复 #1：SupervisorAgent 添加 Configure 覆写方法

**文件**：`hierarchical_msgbus/supervisor_agent.go`

**Python**：
```python
def configure(self, config) -> "SupervisorAgent":
    if isinstance(config, ReActAgentConfig):
        ReActAgent.configure(self, config)
    return self
```

**方案**：在 `SupervisorAgent` 上添加 `Configure` 方法，覆写嵌入的 `ReActAgent.Configure`：

```go
func (s *SupervisorAgent) Configure(ctx context.Context, config agentinterfaces.AgentConfig) error {
    if reactCfg, ok := config.(*saconfig.ReActAgentConfig); ok {
        return s.ReActAgent.Configure(ctx, reactCfg)
    }
    // 非 ReActAgentConfig 类型 no-op，与 Python isinstance 判断一致
    return nil
}
```

---

### 修复 #2：SupervisorAgent 构造时 config=nil 对齐 Python 语义

**文件**：`hierarchical_msgbus/supervisor_agent.go`

**Python**：
```python
super().__init__(card=card)
if config is not None:
    ReActAgent.configure(self, config)
```

**方案**：修改 `NewSupervisorAgent`，`config == nil` 时不传入配置，`config != nil` 时创建后 Configure：

```go
func NewSupervisorAgent(card *agentschema.AgentCard, config *saconfig.ReActAgentConfig, maxParallelSubAgents int) *SupervisorAgent {
    react := agents.NewReActAgent(card, nil)  // 先用 nil 创建
    if config != nil {
        react.Configure(context.Background(), config)  // config 非空才 Configure
    }

    supervisor := &SupervisorAgent{
        CommunicableAgent: *team_runtime.NewCommunicableAgent(),
        ReActAgent:        *react,
    }

    if maxParallelSubAgents < 1 {
        maxParallelSubAgents = defaultMaxParallelSubAgents
    }

    p2pAm := NewP2PAbilityManager(supervisor, maxParallelSubAgents, defaultP2PTimeout)
    react.SetAbilityManager(p2pAm)

    return supervisor
}
```

---

### 修复 #3：P2PAbilityManager.Execute other 调用也放入 goroutine 实现完全并行

**文件**：`hierarchical_msgbus/p2p_ability_manager.go`

**Python**：`asyncio.gather(agent_gather, other_coro)` 全并行。

**方案**：将 other 调用也放入 goroutine，与 agent 调用完全并行：

```go
func (m *P2PAbilityManager) Execute(...) []agentschema.ExecuteResult {
    // ... 分区、快速路径、懒初始化信号量 ...

    results := make([]agentschema.ExecuteResult, len(toolCalls))
    var wg sync.WaitGroup

    // agent 调用 goroutine
    for _, idx := range agentIndices {
        wg.Add(1)
        go func(i int, tc *llmschema.ToolCall) {
            defer wg.Done()
            m.sem <- struct{}{}
            defer func() { <-m.sem }()
            r, err := m.executeSingleP2P(ctx, tc, sess)
            if err != nil {
                results[i] = errorToP2PResult(err, tc.ID)
            } else {
                results[i] = r
            }
        }(idx, toolCalls[idx])
    }

    // other 调用也放入 goroutine
    if len(otherIndices) > 0 {
        wg.Add(1)
        go func() {
            defer wg.Done()
            otherToolCalls := make([]*llmschema.ToolCall, len(otherIndices))
            for j, idx := range otherIndices {
                otherToolCalls[j] = toolCalls[idx]
            }
            otherResults := m.AbilityManager.Execute(ctx, cbc, otherToolCalls, sess, tag)
            for j, idx := range otherIndices {
                results[idx] = otherResults[j]
            }
        }()
    }

    wg.Wait()
    // ... 日志 ...
    return results
}
```

---

### 修复 #4：executeSingleP2P 添加非 Agent fallback 路径

**文件**：`hierarchical_msgbus/p2p_ability_manager.go`

**Python**：
```python
if tool_name not in self._agents:
    return await super()._execute_single_tool_call(tool_call, session, tag)
```

**方案**：在 `executeSingleP2P` 开头添加 IsAgent 判断，非 Agent 时委托基类：

```go
func (m *P2PAbilityManager) executeSingleP2P(...) (agentschema.ExecuteResult, error) {
    toolName := toolCall.Name

    // 非 Agent 调用 fallback：委托基类
    if !m.IsAgent(toolName) {
        singleResults := m.AbilityManager.Execute(ctx, cbc, []*llmschema.ToolCall{toolCall}, sess, tag)
        if len(singleResults) > 0 {
            return singleResults[0], nil
        }
        return agentschema.ExecuteResult{}, nil
    }

    // ... 后续 Agent P2P 派发逻辑不变 ...
}
```

**注意**：需要调整 `executeSingleP2P` 的签名，增加 `cbc *rail.AgentCallbackContext` 参数，因为基类 `Execute` 需要它。

---

### 修复 #8：AddAgent 增加 opts ...TeamOption，通过 WithParentAgentID 传递层级关系

**发现**：`WithParentAgentID` Option 和 `TeamOptions.ParentAgentID` 字段已存在于 `team_interface.go`，但 `AddAgent` 签名没有 `opts` 参数，无法使用。

**文件**：
- `multi_agent/schema/team_interface.go` — 修改 BaseTeam 接口 AddAgent 签名
- `hierarchical_tools/hierarchical_team.go` — 合并 AddAgent + AddAgentWithParent
- `hierarchical_msgbus/hierarchical_team.go` — 适配新签名
- `handoff/handoff_team.go` — 适配新签名
- `schema/team_interface_test.go` — 适配 stubTeam
- 所有调用 `AddAgent` 的测试和业务代码

**Python**：
```python
def add_agent(self, card, provider, parent_agent_id=None):
    super().add_agent(card, provider)
    if parent_agent_id:
        self._pending_children.setdefault(parent_agent_id, []).append(card)
    return self
```

**方案**：

1. **修改 BaseTeam 接口**，AddAgent 签名增加 `opts ...TeamOption`（与 Send/Publish 一致）：

```go
AddAgent(ctx context.Context, card *agentschema.AgentCard, provider TeamAgentProvider, opts ...TeamOption) error
```

2. **HierarchicalToolsTeam** 合并 AddAgent + AddAgentWithParent，通过 opts 解析 parentAgentID：

```go
func (t *HierarchicalToolsTeam) AddAgent(ctx context.Context, card *agentschema.AgentCard, provider maschema.TeamAgentProvider, opts ...maschema.TeamOption) error {
    // 注册到 runtime
    ...
    // 解析 opts 获取 parentAgentID
    teamOpts := maschema.NewTeamOptions(opts...)
    if teamOpts.ParentAgentID != "" {
        t.pendingChildren[teamOpts.ParentAgentID] = append(t.pendingChildren[teamOpts.ParentAgentID], card)
    }
    return nil
}
```

3. **删除 AddAgentWithParent 方法**

4. **HierarchicalTeam (msgbus)** 和 **HandoffTeam** 适配：AddAgent 签名增加 `opts ...TeamOption`（不使用）

5. **调用方**：现有 `.AddAgent(ctx, card, provider)` 不变（opts 为 variadic）；需层级关系时用 `.AddAgent(ctx, card, provider, maschema.WithParentAgentID("parent_id"))`

---

### 修复 #9：移除 setupHierarchy 幂等保护，对齐 Python

**文件**：`hierarchical_tools/hierarchical_team.go`

**Python**：
```python
async def _setup_hierarchy(self) -> None:
    if not self._pending_children:
        return
    for parent_id, child_cards in self._pending_children.items():
        ...
    self._pending_children.clear()
```

**方案**：
1. 移除 `hierarchySetup` 字段
2. `setupHierarchy` 中不再检查 `hierarchySetup` 标志
3. 执行完毕后 `clear(pendingChildren)`（Go 中用重新赋值空 map）
4. `AddAgent` 中不再重置 `hierarchySetup`

```go
func (t *HierarchicalToolsTeam) setupHierarchy(ctx context.Context) error {
    if len(t.pendingChildren) == 0 {
        return nil
    }

    resourceMgr := runner.GetResourceMgr()
    if resourceMgr == nil {
        // Warn 日志，跳过
        return nil
    }

    for parentID, childCards := range t.pendingChildren {
        // ... 遍历注册 ...
    }

    // 对齐 Python：clear pendingChildren
    t.pendingChildren = make(map[string][]*agentschema.AgentCard)
    return nil
}
```

---

### 修复 #10：Stream 添加错误流传播

**文件**：`hierarchical_tools/hierarchical_team.go`

**Python**：
```python
except Exception as e:
    logger.error(f"[{self.__class__.__name__}] Error during streaming: {e}")
    error_result = {"output": str(e), "result_type": "error"}
    await team_session.write_stream(error_result)
```

**方案**：在 Stream 的 chunk 循环外添加 recover 机制，捕获 panic 时写错误到流；同时检查 `agent.Stream()` 返回的 error，在流读取完成后检查是否有错误需要传播：

```go
func(t *HierarchicalToolsTeam) Stream(...) {
    return teams.StandaloneStreamContext(ctx, t.runtime, t.card, inputs, sess,
        func(teamSession *session.AgentTeamSession, sessionID string) error {
            // ... 获取 agent、构造 inputsWithSID ...

            ch, streamErr := agent.Stream(ctx, inputsWithSID)
            if streamErr != nil {
                // 错误写入流
                errorResult := map[string]any{
                    "output":      streamErr.Error(),
                    "result_type": "error",
                }
                _ = teamSession.WriteStream(ctx, errorResult)
                return streamErr
            }

            for chunk := range ch {
                if writeErr := teamSession.WriteStream(ctx, chunk); writeErr != nil {
                    logger.Warn(...)
                }
            }

            // ... 日志 ...
            return nil
        },
    )
}
```

## 不修复项

| # | 差异 | 原因 |
|---|------|------|
| 5 | executeSingleP2P P2P 失败时异常类型 | 双层包装可接受，WithCause 保留异常链 |
| 6 | Create 用 panic | 编程错误用 panic 是 Go 惯用法 |
| 7 | 缺少 _supervisor_instance | YAGNI，等实际需要再添加 |
| 11 | inputs 非 dict 分支 | Go 类型系统决定，inputs 只能是 map |
| 12 | 信号量实现方式 | 功能等价 |
| 13 | Execute 参数 Union vs List | 功能等价 |
| 14 | 初始化日志级别 | Info 更合理 |
| 15 | Invoke 返回值 map 包装 | 类型系统需要 |

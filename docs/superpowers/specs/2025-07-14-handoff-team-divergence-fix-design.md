# HandoffTeam 差异修复设计规格

> 基于差异记录 `2025-07-14-handoff-team-impl-divergence.md` 的逐项讨论结果，
> 确定修复方案、优先级和依赖关系。
> 生成时间：2025-07-14

---

## 一、修复决策汇总

| 编号 | 差异描述 | 决策 | 修复方案 |
|------|---------|------|---------|
| P1 | ContainerAgent 未嵌入 CommunicableAgent，publishHandoff 空操作 | **修复** | 嵌入 CommunicableAgent，publishHandoff 调用 Publish |
| M1 | saveAgentContext 为空操作 | **修复** | 类型断言探测 ContextEngine，删除 saveAgentContextWithCE |
| M2 | ContainerAgent 缺少 resourceMgr 字段 | **不修复** | 全局获取与 Python 一致 |
| M3 | StripHandoffMessages 可见性 | **修复** | 改为非导出 stripHandoffMessages |
| M4 | injectToolsOnce description 为空 | **修复** | 从 runtime 获取 AgentCard description |
| M5 | 消息去重键不完整 | **修复** | 补齐 tool_calls 和 tool_call_id |
| M6 | FlushTeamSession Commit 失败返回错误 | **修复** | 改为仅警告，不返回错误 |
| L1 | ExtractInterruptSignal Result 不含 message 键 | **修复** | 补充 message 键 |
| L2 | writeResultToStream 不支持 list 结果 | **修复** | 检测 []any 类型，逐个 WriteStream |
| L3 | HandoffTeam.Invoke 未复用 StandaloneInvokeContext | **修复** | 改用 StandaloneInvokeContext |
| L4 | defaultContextID 定义位置 | **不修复** | 不影响功能 |
| L5 | History 格式差异 | **修复** | 给 HandoffHistoryEntry 加 JSON tag |
| L6 | findHandoffFromSession 不支持 ast.literal_eval | **不修复** | 语言差异 |
| L7 | HandoffTool.Invoke 不支持 inputs 为 JSON 字符串 | **不修复** | 类型系统差异 |

**共 9 项修复，5 项不修复。**

---

## 二、额外添加项决策

| 编号 | 额外添加 | 决策 | 说明 |
|------|---------|------|------|
| E1 | `GetRuntime() *TeamRuntime` | **删除** | P1 后可通过 CommunicableAgent.Runtime() 获取，测试改用类型断言 |
| E2 | `wrapTeamAgentProvider` | 保留 | Go 类型转换必要 |
| E3 | `filterInterruptHistory` | 保留 | 与 Python isResume 过滤逻辑对应 |
| E4 | `writeResultToStream` | 保留 | 非导出内部方法，逻辑拆分合理 |
| E5 | `saveAgentContextWithCE` | **删除** | M1 修复后逻辑内联到 saveAgentContext |
| E6 | `msgKey` 去重函数 | 保留并修复 | M5 修复补齐字段 |
| E7 | `defaultMaxHandoffs` 常量 | 保留 | 代码清晰度 |
| E8 | `logComponent` 常量 | 保留 | 日志系统要求 |
| E9 | AbilityManager()/CallbackManager()/RegisterCallback()/RegisterRail()/UnregisterRail() | 保留 | 满足 BaseAgent 接口 |
| E10 | `handoffEndpointPrefix`/`containerTopicPrefix` 常量 | 保留 | 常量提取 |

---

## 三、修复优先级与依赖关系

### 依赖关系图

```
P1 (嵌入 CommunicableAgent)
 └──→ M4 (从 runtime 获取 description) — 依赖 P1 的 Runtime() 方法

M1, M3, M5, M6, L1, L2, L3, L5 — 全部独立，可与 P1 并行
```

### 优先级排序

| 优先级 | 差异编号 | 修复内容 | 工作量 | 依赖 |
|--------|---------|---------|--------|------|
| P0 | P1 | 嵌入 CommunicableAgent，实现 publishHandoff | 中 | — |
| P1 | M1 | saveAgentContext 类型断言探测，删除 saveAgentContextWithCE | 小 | — |
| P1 | M6 | FlushTeamSession Commit 失败改为仅警告 | 极小 | — |
| P1 | M3 | StripHandoffMessages 改为非导出 stripHandoffMessages | 极小 | — |
| P1 | L1 | ExtractInterruptSignal 补充 message 键 | 极小 | — |
| P1 | L5 | HandoffHistoryEntry 加 JSON tag | 极小 | — |
| P2 | M4 | injectToolsOnce 从 runtime 获取 AgentCard description | 小 | P1 |
| P2 | M5 | 消息去重键补齐 tool_calls 和 tool_call_id | 小 | — |
| P2 | L2 | writeResultToStream 支持 list 结果 | 小 | — |
| P2 | L3 | HandoffTeam.Invoke 改用 StandaloneInvokeContext | 小 | — |

---

## 四、各修复项详细设计

### P1：ContainerAgent 嵌入 CommunicableAgent

**文件**：`container_agent.go`

**修改内容**：

1. ContainerAgent 结构体嵌入 `teamruntime.CommunicableAgent`：
```go
type ContainerAgent struct {
    teamruntime.CommunicableAgent  // 嵌入，获得 Send/Publish/Subscribe/IsBound/Runtime 方法
    // ... 其余字段不变
}
```

2. 在 `ensureInternalAgents`（或 HandoffTeam 构造 ContainerAgent 后）调用 `BindRuntime` 注入运行时引用：
```go
c.BindRuntime(runtime, agentID)
```

3. `publishHandoff` 方法实现实际交接逻辑：
```go
func (c *ContainerAgent) publishHandoff(ctx context.Context, nextReq *HandoffRequest, sessionID string) {
    if nextReq == nil || !c.IsBound() {
        return
    }
    topicID := fmt.Sprintf("container_%s", nextReq.Target)
    if err := c.Publish(ctx, nextReq, topicID); err != nil {
        logger.Warn(logComponent).Err(err).
            Str("action", "publish_handoff").
            Str("target", nextReq.Target).
            Str("topic_id", topicID).
            Msg("发布交接请求失败")
    }
}
```

4. 删除 `GetRuntime()` 导出方法（E1），测试中改用类型断言或直接通过嵌入的 CommunicableAgent 访问。

**对应 Python**：`class ContainerAgent(CommunicableAgent, BaseAgent)` → `await self.publish(message=HandoffRequest(...), topic_id=f"container_{signal.target}", session_id=session_id)`

---

### M1：saveAgentContext 类型断言探测

**文件**：`container_agent.go`

**修改内容**：

1. `saveAgentContext` 中用类型断言探测 ContextEngine：
```go
func (c *ContainerAgent) saveAgentContext(ctx context.Context, targetAgent agentinterfaces.BaseAgent, agentSession *session.Session) {
    if targetAgent == nil || agentSession == nil {
        return
    }

    // 类型断言探测目标 Agent 是否有 ContextEngine（对应 Python getattr）
    type contextEngineHolder interface {
        ContextEngine() ceinterface.ContextEngine
    }
    if holder, ok := targetAgent.(contextEngineHolder); ok {
        ce := holder.ContextEngine()
        if ce != nil {
            if _, err := ce.SaveContexts(ctx, agentSession, nil); err != nil {
                logger.Warn(logComponent).Err(err).
                    Str("action", "save_agent_context").
                    Str("agent_id", targetAgent.Card().ID).
                    Msg("保存 Agent 上下文失败")
            }
        }
    }
}
```

2. 删除 `saveAgentContextWithCE` 方法（E5），逻辑已内联。

**对应 Python**：`context_engine = getattr(target_agent, "context_engine", None)` + `await context_engine.save_contexts(agent_session)`

---

### M3：StripHandoffMessages 改为非导出

**文件**：`container_agent.go`、`container_agent_test.go`

**修改内容**：

1. 重命名 `StripHandoffMessages` → `stripHandoffMessages`
2. 更新所有调用点（同包内，包括测试）

---

### M4：injectToolsOnce 从 runtime 获取 AgentCard description

**文件**：`container_agent.go`

**前置依赖**：P1（ContainerAgent 嵌入 CommunicableAgent 后可调用 `c.Runtime()`）

**修改内容**：

```go
// 获取目标 Agent 的描述
description := ""
if rt := c.Runtime(); rt != nil {
    if card, err := rt.GetAgentCard(targetID); err == nil && card != nil {
        description = card.Description
    }
}
```

**对应 Python**：`card = self._runtime.get_agent_card(target_id) if self._runtime else None` + `description = card.description if card else ""`

---

### M5：消息去重键补齐

**文件**：`container_agent.go`

**修改内容**：

```go
func msgKey(msg any) string {
    msgMap, ok := msg.(map[string]any)
    if !ok {
        return ""
    }

    role := ""
    if r, ok := msgMap["role"]; ok {
        if rs, ok := r.(string); ok {
            role = rs
        }
    }

    content := ""
    if c, ok := msgMap["content"]; ok {
        if cs, ok := c.(string); ok {
            content = cs
        }
    }

    // 补齐 tool_calls 字段（对应 Python str(getattr(m, "tool_calls", ""))）
    toolCallsStr := ""
    if tc, ok := msgMap["tool_calls"]; ok {
        toolCallsStr = fmt.Sprintf("%v", tc)
    }

    // 补齐 tool_call_id 字段（对应 Python getattr(m, "tool_call_id", "")）
    toolCallID := ""
    if tci, ok := msgMap["tool_call_id"]; ok {
        if s, ok := tci.(string); ok {
            toolCallID = s
        }
    }

    return role + ":" + content + ":" + toolCallsStr + ":" + toolCallID
}
```

**对应 Python**：`_msg_key(m) = (role, str(content), str(tool_calls), tool_call_id)`

---

### M6：FlushTeamSession Commit 失败仅警告

**文件**：`handoff_team.go`（或对应 FlushTeamSession 所在文件）

**修改内容**：

`Commit()` 失败时记录 Warn 日志但不返回 error，与 `CloseStream()` 失败处理一致：

```go
if err := teamSession.Commit(ctx); err != nil {
    logger.Warn(logComponent).Err(err).
        Str("action", "flush_team_session").
        Msg("提交 team session 失败")
    // 不返回错误，与 Python 行为一致
}
```

**对应 Python**：`try/except` 中 `close_stream()` + `commit()` 任何失败仅 warning

---

### L1：ExtractInterruptSignal 补充 message 键

**文件**：`handoff_signal.go`

**修改内容**：

路径2 构造 Result 时加入 message 键：

```go
Result: map[string]any{
    "result_type": "interrupt",
    "message":     message,
},
```

**对应 Python**：`result={"result_type": "interrupt", "message": message}`

---

### L2：writeResultToStream 支持 list 结果

**文件**：`container_agent.go`

**修改内容**：

```go
func (c *ContainerAgent) writeResultToStream(ctx context.Context, result any, teamSession *session.AgentTeamSession) {
    if result == nil || teamSession == nil {
        return
    }

    switch v := result.(type) {
    case map[string]any:
        _ = teamSession.WriteStream(ctx, v)
    case []any:
        for _, item := range v {
            if m, ok := item.(map[string]any); ok {
                _ = teamSession.WriteStream(ctx, m)
            }
        }
    }
}
```

**注意**：方法签名需从 `result map[string]any` 改为 `result any`，调用点需同步调整。

**对应 Python**：区分 `isinstance(result, dict)` 和 `isinstance(result, list)` 两种情况

---

### L3：HandoffTeam.Invoke 改用 StandaloneInvokeContext

**文件**：`handoff_team.go`

**修改内容**：

用 `teams/utils.go` 中的 `StandaloneInvokeContext` 替代手动会话生命周期管理（创建/PreRun/Bind/PostRun/Unbind）。

**对应 Python**：`async with standalone_invoke_context(...)` 管理会话

---

### L5：HandoffHistoryEntry 加 JSON tag

**文件**：`handoff_request.go`

**修改内容**：

```go
type HandoffHistoryEntry struct {
    // AgentID Agent 标识
    AgentID string         `json:"agent"`
    // Output Agent 输出结果
    Output  map[string]any `json:"output"`
}
```

**对应 Python**：`{"agent": ..., "output": ...}` dict 键名

---

## 五、设计规格未实现项状态更新

| 编号 | 要求 | 对应差异 | 决策 |
|------|------|---------|------|
| D1 | ContainerAgent 嵌入 CommunicableAgent | P1 | **修复** |
| D2 | ContainerAgent 持有 resourceMgr 字段 | M2 | **不修复**（全局获取与 Python 一致） |
| D3 | saveAgentContext 实现 | M1 | **修复** |
| D4 | HandoffTeam.Invoke 使用 standaloneInvokeContext | L3 | **修复** |
| D5 | injectToolsOnce 从 runtime 获取 AgentCard description | M4 | **修复** |

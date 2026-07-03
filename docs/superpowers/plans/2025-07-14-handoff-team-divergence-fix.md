# HandoffTeam 差异修复实现计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 修复 HandoffTeam 实现与 Python 参考实现之间的 9 项差异，确保多 Agent 交接流程完整工作

**Architecture:** 按 P0→P1→P2 优先级分批修复，P1（嵌入 CommunicableAgent）是核心前置，M4 依赖 P1 完成后的 Runtime() 方法；其余修复项相互独立可并行

**Tech Stack:** Go 1.22+, team_runtime.CommunicableAgent, ceinterface.ContextEngine, session.AgentTeamSession

---

## 文件结构

| 文件 | 职责 | 修改类型 |
|------|------|---------|
| `internal/agentcore/multi_agent/teams/handoff/container_agent.go` | ContainerAgent 核心逻辑 | 修改：P1/M1/M3/M4/M5/L2 |
| `internal/agentcore/multi_agent/teams/handoff/container_agent_test.go` | ContainerAgent 测试 | 修改：适配上述变更 |
| `internal/agentcore/multi_agent/teams/handoff/handoff_team.go` | HandoffTeam 核心 | 修改：L3/E1 |
| `internal/agentcore/multi_agent/teams/handoff/handoff_team_test.go` | HandoffTeam 测试 | 修改：适配 E1 删除 |
| `internal/agentcore/multi_agent/teams/handoff/handoff_request.go` | HandoffHistoryEntry 定义 | 修改：L5 |
| `internal/agentcore/multi_agent/teams/handoff/interrupt.go` | 中断信号 + FlushTeamSession | 修改：L1/M6 |
| `internal/agentcore/multi_agent/teams/handoff/interrupt_test.go` | 中断信号测试 | 修改：适配 L1/M6 |

---

### Task 1: P1 — ContainerAgent 嵌入 CommunicableAgent，实现 publishHandoff

**Files:**
- Modify: `internal/agentcore/multi_agent/teams/handoff/container_agent.go`
- Modify: `internal/agentcore/multi_agent/teams/handoff/container_agent_test.go`
- Modify: `internal/agentcore/multi_agent/teams/handoff/handoff_team.go` (makeContainerProvider 中 BindRuntime)

- [ ] **Step 1: ContainerAgent 结构体嵌入 CommunicableAgent**

在 `container_agent.go` 中：

1. 添加 import `"github.com/uapclaw/uapclaw-go/internal/agentcore/multi_agent/team_runtime"`
2. ContainerAgent 结构体嵌入 `teamruntime.CommunicableAgent`：
```go
type ContainerAgent struct {
	teamruntime.CommunicableAgent // 嵌入，获得 Send/Publish/Subscribe/IsBound/Runtime 方法
	// targetCard 目标 Agent 的身份卡片
	targetCard *agentschema.AgentCard
	// ... 其余字段不变
}
```
3. `NewContainerAgent` 初始化时无需特殊处理 CommunicableAgent（零值即可，后续 BindRuntime 注入）
4. 编译时验证：`var _ agentinterfaces.BaseAgent = (*ContainerAgent)(nil)` 应仍然通过

- [ ] **Step 2: 实现 publishHandoff 方法**

替换当前空操作：
```go
func (c *ContainerAgent) publishHandoff(
	ctx context.Context,
	inputMessage map[string]any,
	history []HandoffHistoryEntry,
	req *HandoffRequest,
	signal *HandoffSignal,
	sessionID string,
) {
	nextReq := &HandoffRequest{
		InputMessage: inputMessage,
		History:      history,
		Session:      req.Session,
	}

	if !c.IsBound() {
		logger.Warn(logComponent).
			Str("action", "publish_handoff").
			Str("target_id", signal.Target).
			Str("session_id", sessionID).
			Msg("CommunicableAgent 未绑定运行时，无法发布交接消息")
		return
	}

	topicID := containerTopicPrefix + signal.Target
	if err := c.Publish(ctx, nextReq, topicID,
		maschema.WithTeamSessionID(sessionID),
	); err != nil {
		logger.Warn(logComponent).Err(err).
			Str("action", "publish_handoff").
			Str("target_id", signal.Target).
			Str("topic_id", topicID).
			Str("session_id", sessionID).
			Msg("发布交接请求失败")
	}

	logger.Info(logComponent).
		Str("action", "publish_handoff").
		Str("target_id", signal.Target).
		Str("reason", signal.Reason).
		Str("session_id", sessionID).
		Msg("发布交接消息到下一个 ContainerAgent")
}
```

注意：`containerTopicPrefix` 已在 `handoff_team.go` 中定义为常量 `"container_"`，需确保包内可见。

- [ ] **Step 3: 在 makeContainerProvider 中 BindRuntime**

修改 `handoff_team.go` 中的 `makeContainerProvider` 方法，创建 ContainerAgent 后立即 BindRuntime：
```go
func (t *HandoffTeam) makeContainerProvider(
	card *agentschema.AgentCard,
	agentID string,
	allowedTargets map[string]struct{},
) resources_manager.AgentProvider {
	coordinatorLookup := t.lookupCoordinator
	agentProvider := t.agentProviders[agentID]
	runtime := t.runtime

	return func(ctx context.Context, _ *agentschema.AgentCard) (agentinterfaces.BaseAgent, error) {
		container := NewContainerAgent(card, agentProvider, allowedTargets, coordinatorLookup)
		container.BindRuntime(runtime, fmt.Sprintf("%s%s_%s", handoffEndpointPrefix, runtime.Config().TeamID, agentID))
		return container, nil
	}
}
```

注意：`runtime.Config().TeamID` 需确认 TeamRuntime 是否有 Config() 方法暴露 TeamID。如无，可用 `t.card.GetID()` 代替（等价）。

- [ ] **Step 4: 删除 HandoffTeam.GetRuntime()（E1）**

删除 `handoff_team.go` 中的 `GetRuntime()` 方法。修改 `handoff_team_test.go` 中使用 `team.GetRuntime()` 的测试，改为通过其他方式验证（如直接访问 `team.runtime` 字段或通过类型断言）。

- [ ] **Step 5: 运行测试确认 P1 通过**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/multi_agent/teams/handoff/... -v -count=1`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add internal/agentcore/multi_agent/teams/handoff/container_agent.go internal/agentcore/multi_agent/teams/handoff/container_agent_test.go internal/agentcore/multi_agent/teams/handoff/handoff_team.go internal/agentcore/multi_agent/teams/handoff/handoff_team_test.go
git commit -m "fix(handoff): ContainerAgent 嵌入 CommunicableAgent，实现 publishHandoff 交接 (P1, E1)"
```

---

### Task 2: M1 — saveAgentContext 类型断言探测，删除 saveAgentContextWithCE

**Files:**
- Modify: `internal/agentcore/multi_agent/teams/handoff/container_agent.go`
- Modify: `internal/agentcore/multi_agent/teams/handoff/container_agent_test.go`

- [ ] **Step 1: 重写 saveAgentContext 方法**

替换当前空操作，使用类型断言探测 ContextEngine：
```go
func (c *ContainerAgent) saveAgentContext(ctx context.Context, targetAgent agentinterfaces.BaseAgent, agentSession *session.Session) {
	if targetAgent == nil || agentSession == nil {
		return
	}

	// 类型断言探测目标 Agent 是否有 ContextEngine
	// 对应 Python: context_engine = getattr(target_agent, "context_engine", None)
	type contextEngineHolder interface {
		ContextEngine() ceinterface.ContextEngine
	}
	holder, ok := targetAgent.(contextEngineHolder)
	if !ok {
		return
	}

	ce := holder.ContextEngine()
	if ce == nil {
		return
	}

	if _, err := ce.SaveContexts(ctx, agentSession, nil); err != nil {
		logger.Warn(logComponent).Err(err).
			Str("action", "save_agent_context").
			Str("agent_id", targetAgent.Card().ID).
			Msg("保存 Agent 上下文失败")
	}
}
```

- [ ] **Step 2: 删除 saveAgentContextWithCE 方法**

从 `container_agent.go` 中删除整个 `saveAgentContextWithCE` 方法定义（第 710-720 行）。

- [ ] **Step 3: 更新测试**

在 `container_agent_test.go` 中：
- 删除对 `saveAgentContextWithCE` 的测试（如有）
- 确保 `saveAgentContext` 的测试覆盖：无 ContextEngine 的 Agent（跳过）、有 ContextEngine 的 Agent（调用 SaveContexts）

- [ ] **Step 4: 运行测试确认 M1 通过**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/multi_agent/teams/handoff/... -v -count=1`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/agentcore/multi_agent/teams/handoff/container_agent.go internal/agentcore/multi_agent/teams/handoff/container_agent_test.go
git commit -m "fix(handoff): saveAgentContext 类型断言探测 ContextEngine，删除 saveAgentContextWithCE (M1, E5)"
```

---

### Task 3: M3 — StripHandoffMessages 改为非导出

**Files:**
- Modify: `internal/agentcore/multi_agent/teams/handoff/container_agent.go`
- Modify: `internal/agentcore/multi_agent/teams/handoff/container_agent_test.go`

- [ ] **Step 1: 重命名 StripHandoffMessages → stripHandoffMessages**

在 `container_agent.go` 中：
1. 函数签名：`func StripHandoffMessages(messages []any) []any` → `func stripHandoffMessages(messages []any) []any`
2. 调用点：`cleaned := StripHandoffMessages(newMessages)` → `cleaned := stripHandoffMessages(newMessages)`（在 `saveContextToTeamSession` 中）

- [ ] **Step 2: 更新测试**

在 `container_agent_test.go` 中，所有对 `StripHandoffMessages` 的调用改为 `stripHandoffMessages`（同包内可直接访问非导出函数）。

- [ ] **Step 3: 运行测试确认 M3 通过**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/multi_agent/teams/handoff/... -v -count=1`
Expected: PASS

- [ ] **Step 4: Commit**

```bash
git add internal/agentcore/multi_agent/teams/handoff/container_agent.go internal/agentcore/multi_agent/teams/handoff/container_agent_test.go
git commit -m "fix(handoff): StripHandoffMessages 改为非导出 stripHandoffMessages (M3)"
```

---

### Task 4: M5 — 消息去重键补齐 tool_calls 和 tool_call_id

**Files:**
- Modify: `internal/agentcore/multi_agent/teams/handoff/container_agent.go`
- Modify: `internal/agentcore/multi_agent/teams/handoff/container_agent_test.go`

- [ ] **Step 1: 重写 msgKey 函数**

替换当前 2 字段实现为 4 字段：
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

	// 对应 Python: str(getattr(m, "tool_calls", ""))
	toolCallsStr := ""
	if tc, ok := msgMap["tool_calls"]; ok {
		toolCallsStr = fmt.Sprintf("%v", tc)
	}

	// 对应 Python: getattr(m, "tool_call_id", "")
	toolCallID := ""
	if tci, ok := msgMap["tool_call_id"]; ok {
		if s, ok := tci.(string); ok {
			toolCallID = s
		}
	}

	return role + ":" + content + ":" + toolCallsStr + ":" + toolCallID
}
```

- [ ] **Step 2: 添加/更新 msgKey 测试**

在 `container_agent_test.go` 中添加测试用例：
- 只有 role+content 的消息
- 有 tool_calls 的消息（应产生不同 key）
- 有 tool_call_id 的消息（应产生不同 key）
- 相同 role+content 但不同 tool_calls 的消息（应不去重）

- [ ] **Step 3: 运行测试确认 M5 通过**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/multi_agent/teams/handoff/... -v -count=1`
Expected: PASS

- [ ] **Step 4: Commit**

```bash
git add internal/agentcore/multi_agent/teams/handoff/container_agent.go internal/agentcore/multi_agent/teams/handoff/container_agent_test.go
git commit -m "fix(handoff): msgKey 补齐 tool_calls 和 tool_call_id 字段 (M5)"
```

---

### Task 5: M4 — injectToolsOnce 从 runtime 获取 AgentCard description

**Files:**
- Modify: `internal/agentcore/multi_agent/teams/handoff/container_agent.go`

**前置依赖：** Task 1 (P1) 完成后 ContainerAgent 已嵌入 CommunicableAgent

- [ ] **Step 1: 修改 injectToolsOnce 中 description 获取逻辑**

将 `description := ""` 和注释替换为：
```go
// 获取目标 Agent 的描述
// 对应 Python: card = self._runtime.get_agent_card(target_id) if self._runtime else None
//              description = card.description if card else ""
description := ""
if rt := c.Runtime(); rt != nil {
	if card, cardErr := rt.GetAgentCard(targetID); cardErr == nil && card != nil {
		description = card.Description
	}
}
```

- [ ] **Step 2: 运行测试确认 M4 通过**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/multi_agent/teams/handoff/... -v -count=1`
Expected: PASS

- [ ] **Step 3: Commit**

```bash
git add internal/agentcore/multi_agent/teams/handoff/container_agent.go
git commit -m "fix(handoff): injectToolsOnce 从 runtime 获取 AgentCard description (M4)"
```

---

### Task 6: M6 — FlushTeamSession Commit 失败改为仅警告

**Files:**
- Modify: `internal/agentcore/multi_agent/teams/handoff/interrupt.go`
- Modify: `internal/agentcore/multi_agent/teams/handoff/interrupt_test.go`

- [ ] **Step 1: 修改 FlushTeamSession Commit 错误处理**

在 `interrupt.go` 的 `FlushTeamSession` 函数中，将 Commit 失败后的 `return err` 改为仅记录警告：
```go
// 提交检查点
if err := sess.Commit(ctx); err != nil {
	logger.Warn(logger.ComponentAgentCore).
		Err(err).
		Str("action", "flush_team_session").
		Str("session_id", sess.GetSessionID()).
		Msg("FlushTeamSession Commit 失败")
	// 不返回错误，与 Python 行为一致
}

return nil
```

- [ ] **Step 2: 更新 FlushTeamSession 测试**

确保测试中 Commit 失败时 FlushTeamSession 返回 nil 而非 error。

- [ ] **Step 3: 运行测试确认 M6 通过**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/multi_agent/teams/handoff/... -v -count=1`
Expected: PASS

- [ ] **Step 4: Commit**

```bash
git add internal/agentcore/multi_agent/teams/handoff/interrupt.go internal/agentcore/multi_agent/teams/handoff/interrupt_test.go
git commit -m "fix(handoff): FlushTeamSession Commit 失败改为仅警告 (M6)"
```

---

### Task 7: L1 — ExtractInterruptSignal 补充 message 键

**Files:**
- Modify: `internal/agentcore/multi_agent/teams/handoff/interrupt.go`
- Modify: `internal/agentcore/multi_agent/teams/handoff/interrupt_test.go`

- [ ] **Step 1: 修改 ExtractInterruptSignal 路径2**

在 `interrupt.go` 的 `ExtractInterruptSignal` 函数路径2中，将：
```go
Result: map[string]any{"result_type": "interrupt"},
```
改为：
```go
Result: map[string]any{"result_type": "interrupt", "message": msg},
```

- [ ] **Step 2: 添加/更新 ExtractInterruptSignal 测试**

确保路径2的测试验证 Result 包含 `"message"` 键。

- [ ] **Step 3: 运行测试确认 L1 通过**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/multi_agent/teams/handoff/... -v -count=1`
Expected: PASS

- [ ] **Step 4: Commit**

```bash
git add internal/agentcore/multi_agent/teams/handoff/interrupt.go internal/agentcore/multi_agent/teams/handoff/interrupt_test.go
git commit -m "fix(handoff): ExtractInterruptSignal 路径2 Result 补充 message 键 (L1)"
```

---

### Task 8: L2 — writeResultToStream 支持 list 结果

**Files:**
- Modify: `internal/agentcore/multi_agent/teams/handoff/container_agent.go`
- Modify: `internal/agentcore/multi_agent/teams/handoff/container_agent_test.go`

- [ ] **Step 1: 修改 writeResultToStream 方法签名和实现**

将方法签名从 `result map[string]any` 改为 `result any`，增加 list 分支：
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

- [ ] **Step 2: 更新 writeResultToStream 测试**

添加 list 结果的测试用例，验证逐个写入行为。

- [ ] **Step 3: 运行测试确认 L2 通过**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/multi_agent/teams/handoff/... -v -count=1`
Expected: PASS

- [ ] **Step 4: Commit**

```bash
git add internal/agentcore/multi_agent/teams/handoff/container_agent.go internal/agentcore/multi_agent/teams/handoff/container_agent_test.go
git commit -m "fix(handoff): writeResultToStream 支持 list 结果逐个写入 (L2)"
```

---

### Task 9: L3 — HandoffTeam.Invoke 改用 StandaloneInvokeContext

**Files:**
- Modify: `internal/agentcore/multi_agent/teams/handoff/handoff_team.go`
- Modify: `internal/agentcore/multi_agent/teams/handoff/handoff_team_test.go`

- [ ] **Step 1: 重写 HandoffTeam.Invoke 使用 StandaloneInvokeContext**

将手动会话管理替换为 `StandaloneInvokeContext`：
```go
func (t *HandoffTeam) Invoke(ctx context.Context, inputs map[string]any, opts ...maschema.TeamOption) (any, error) {
	teamOpts := maschema.NewTeamOptions(opts...)
	sess := teamOpts.Session

	result, err := teams.StandaloneInvokeContext(ctx, t.runtime, t.card, inputs, sess,
		func(teamSession *session.AgentTeamSession, sessionID string) (map[string]any, error) {
			return t.runChain(ctx, inputs, teamSession)
		},
	)

	if err != nil {
		return nil, err
	}
	return result, nil
}
```

注意：需确认 `StandaloneInvokeContext` 的函数签名是否匹配（接收 `fn func(*session.AgentTeamSession, string) (map[string]any, error)`）。如不匹配，需调整 `runChain` 的签名或增加适配闭包。

- [ ] **Step 2: 添加 import**

在 `handoff_team.go` 中添加：
```go
teams "github.com/uapclaw/uapclaw-go/internal/agentcore/multi_agent/teams"
```

- [ ] **Step 3: 运行测试确认 L3 通过**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/multi_agent/teams/handoff/... -v -count=1`
Expected: PASS

- [ ] **Step 4: Commit**

```bash
git add internal/agentcore/multi_agent/teams/handoff/handoff_team.go internal/agentcore/multi_agent/teams/handoff/handoff_team_test.go
git commit -m "fix(handoff): HandoffTeam.Invoke 改用 StandaloneInvokeContext (L3)"
```

---

### Task 10: L5 — HandoffHistoryEntry 加 JSON tag

**Files:**
- Modify: `internal/agentcore/multi_agent/teams/handoff/handoff_request.go`

- [ ] **Step 1: 给 HandoffHistoryEntry 加 JSON tag**

```go
type HandoffHistoryEntry struct {
	// AgentID Agent 标识
	AgentID string `json:"agent"`
	// Output Agent 输出结果
	Output  map[string]any `json:"output"`
}
```

- [ ] **Step 2: 运行测试确认 L5 通过**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/multi_agent/teams/handoff/... -v -count=1`
Expected: PASS

- [ ] **Step 3: Commit**

```bash
git add internal/agentcore/multi_agent/teams/handoff/handoff_request.go
git commit -m "fix(handoff): HandoffHistoryEntry 加 JSON tag 与 Python 键名对齐 (L5)"
```

---

### Task 11: 全量测试 + 更新差异文档

**Files:**
- Modify: `docs/superpowers/specs/2025-07-14-handoff-team-impl-divergence.md`

- [ ] **Step 1: 运行全量测试**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/multi_agent/... -v -count=1`
Expected: PASS

- [ ] **Step 2: 运行覆盖率检查**

Run: `cd /home/opensource/uap-claw-go && go test -cover ./internal/agentcore/multi_agent/teams/handoff/...`
Expected: 覆盖率 ≥ 85%

- [ ] **Step 3: 更新差异文档状态**

在 `2025-07-14-handoff-team-impl-divergence.md` 中标注每项差异的修复状态：
- P1: ✅ 已修复
- M1: ✅ 已修复
- M2: — 不修复（与 Python 一致）
- M3: ✅ 已修复
- M4: ✅ 已修复
- M5: ✅ 已修复
- M6: ✅ 已修复
- L1: ✅ 已修复
- L2: ✅ 已修复
- L3: ✅ 已修复
- L4: — 不修复（不影响功能）
- L5: ✅ 已修复
- L6: — 不修复（语言差异）
- L7: — 不修复（类型系统差异）

- [ ] **Step 4: Commit**

```bash
git add docs/superpowers/specs/2025-07-14-handoff-team-impl-divergence.md
git commit -m "docs: 更新 HandoffTeam 差异修复状态"
```

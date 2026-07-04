# 8.35 & 8.36 HierarchicalTeam Python 对齐修复 实现计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 修复 7 个 Go 与 Python 的行为差异，使 8.35 (hierarchical_msgbus) 和 8.36 (hierarchical_tools) 的实现对齐 Python 源码。

**Architecture:** 按依赖关系分 3 组执行：先修改接口层（修复 #8），再修改 msgbus 包（修复 #1-#4），最后修改 tools 包（修复 #9-#10）。

**Tech Stack:** Go 1.23+, 项目内 logger/exception/schema 包

---

## 文件结构

| 文件 | 操作 | 职责 |
|------|------|------|
| `multi_agent/schema/team_interface.go` | 修改 | BaseTeam.AddAgent 签名增加 opts |
| `multi_agent/schema/team_interface_test.go` | 修改 | stubTeam 适配新签名 |
| `hierarchical_msgbus/supervisor_agent.go` | 修改 | 添加 Configure 覆写 + 构造逻辑修复 |
| `hierarchical_msgbus/supervisor_agent_test.go` | 修改 | 新增 Configure 测试用例 |
| `hierarchical_msgbus/p2p_ability_manager.go` | 修改 | other 并行 + fallback 路径 |
| `hierarchical_msgbus/p2p_ability_manager_test.go` | 修改 | 新增并行和 fallback 测试 |
| `hierarchical_msgbus/hierarchical_team.go` | 修改 | AddAgent 适配新签名 |
| `hierarchical_msgbus/hierarchical_team_test.go` | 修改 | AddAgent 调用适配 |
| `hierarchical_tools/hierarchical_team.go` | 修改 | AddAgent 合并 + 移除幂等 + Stream 错误传播 |
| `hierarchical_tools/hierarchical_team_test.go` | 修改 | 测试适配 |
| `handoff/handoff_team.go` | 修改 | AddAgent 适配新签名 |
| `handoff/handoff_team_test.go` | 修改 | AddAgent 调用适配 |
| 其他调用 AddAgent 的测试/业务代码 | 修改 | 签名适配（opts 为 variadic，不传即可） |

---

## Task 1：修改 BaseTeam 接口 AddAgent 签名（修复 #8 前置）

**Files:**
- Modify: `internal/agentcore/multi_agent/schema/team_interface.go:39-42`
- Modify: `internal/agentcore/multi_agent/schema/team_interface_test.go:40`

- [ ] **Step 1: 修改 BaseTeam 接口 AddAgent 签名**

在 `team_interface.go` 中，将：
```go
AddAgent(ctx context.Context, card *agentschema.AgentCard, provider TeamAgentProvider) error
```
改为：
```go
AddAgent(ctx context.Context, card *agentschema.AgentCard, provider TeamAgentProvider, opts ...TeamOption) error
```

同时更新注释为：
```go
// AddAgent 向团队注册 Agent。
//
// 通过 WithParentAgentID() Option 声明层级关系（仅 HierarchicalToolsTeam 使用），
// 其他 Team 实现忽略 opts。
//
// 对应 Python: BaseTeam.add_agent(card, provider, parent_agent_id=None) -> self
```

- [ ] **Step 2: 适配 stubTeam 测试**

在 `team_interface_test.go` 中，将 stubTeam 的 AddAgent 签名改为：
```go
func (t *stubTeam) AddAgent(_ context.Context, _ *agentschema.AgentCard, _ TeamAgentProvider, _ ...TeamOption) error {
```

- [ ] **Step 3: 编译验证**

Run: `cd /home/opensource/uap-claw-go && go build ./internal/agentcore/multi_agent/schema/...`
Expected: 编译失败（其他实现者未适配），记录错误文件列表

- [ ] **Step 4: 适配所有 BaseTeam 实现者的 AddAgent 签名**

对每个实现 BaseTeam 接口的结构体，AddAgent 签名增加 `opts ...maschema.TeamOption`（实现体内暂不使用 opts）：

1. `hierarchical_msgbus/hierarchical_team.go` — AddAgent 签名加 `opts ...maschema.TeamOption`
2. `hierarchical_tools/hierarchical_team.go` — AddAgent 签名加 `opts ...maschema.TeamOption`（后续 Task 5 会完整改造）
3. `handoff/handoff_team.go` — AddAgent 签名加 `opts ...maschema.TeamOption`

- [ ] **Step 5: 适配所有 AddAgent 调用方**

搜索所有 `.AddAgent(` 调用，确认现有调用 `team.AddAgent(ctx, card, provider)` 无需修改（opts 为 variadic，不传即为空切片）。仅更新测试代码中如有直接构造 stubTeam 后调用 AddAgent 的地方。

- [ ] **Step 6: 编译通过**

Run: `cd /home/opensource/uap-claw-go && go build ./internal/agentcore/...`
Expected: 编译成功

- [ ] **Step 7: 运行测试**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/multi_agent/... -count=1 -timeout 120s`
Expected: PASS

- [ ] **Step 8: Commit**

```
feat(team): BaseTeam.AddAgent 增加 opts ...TeamOption 参数，支持 WithParentAgentID 层级关系
```

---

## Task 2：SupervisorAgent 添加 Configure 覆写 + 构造逻辑修复（修复 #1 + #2）

**Files:**
- Modify: `internal/agentcore/multi_agent/teams/hierarchical_msgbus/supervisor_agent.go`
- Modify: `internal/agentcore/multi_agent/teams/hierarchical_msgbus/supervisor_agent_test.go`

- [ ] **Step 1: 修改 NewSupervisorAgent 构造逻辑（修复 #2）**

将 `supervisor_agent.go` 中 `NewSupervisorAgent` 函数体改为：

```go
func NewSupervisorAgent(
	card *agentschema.AgentCard,
	config *saconfig.ReActAgentConfig,
	maxParallelSubAgents int,
) *SupervisorAgent {
	// 对齐 Python: super().__init__(card=card)，先创建默认实例
	react := agents.NewReActAgent(card, nil)

	// 对齐 Python: if config is not None: ReActAgent.configure(self, config)
	if config != nil {
		_ = react.Configure(context.Background(), config)
	}

	supervisor := &SupervisorAgent{
		CommunicableAgent: *team_runtime.NewCommunicableAgent(),
		ReActAgent:        *react,
	}

	if maxParallelSubAgents < 1 {
		maxParallelSubAgents = defaultMaxParallelSubAgents
	}

	// 从 runtime 获取 timeout，若未绑定则使用默认值
	p2pAm := NewP2PAbilityManager(supervisor, maxParallelSubAgents, defaultP2PTimeout)
	react.SetAbilityManager(p2pAm)

	return supervisor
}
```

- [ ] **Step 2: 添加 Configure 覆写方法（修复 #1）**

在 `supervisor_agent.go` 的导出函数区块（`RegisterSubAgentCard` 之后）添加：

```go
// Configure 配置 SupervisorAgent。
//
// 对齐 Python: SupervisorAgent.configure(config) — 只对 ReActAgentConfig 类型执行配置，其他类型 no-op。
func (s *SupervisorAgent) Configure(ctx context.Context, config agentinterfaces.AgentConfig) error {
	if reactCfg, ok := config.(*saconfig.ReActAgentConfig); ok {
		return s.ReActAgent.Configure(ctx, reactCfg)
	}
	// 非 ReActAgentConfig 类型 no-op，与 Python isinstance 判断一致
	return nil
}
```

- [ ] **Step 3: 确认编译**

Run: `cd /home/opensource/uap-claw-go && go build ./internal/agentcore/multi_agent/teams/hierarchical_msgbus/...`
Expected: 编译成功

- [ ] **Step 4: 添加 Configure 测试用例**

在 `supervisor_agent_test.go` 添加：

```go
func TestSupervisorAgent_Configure(t *testing.T) {
	card := &agentschema.AgentCard{ID: "sup-1", Name: "supervisor"}
	sup := NewSupervisorAgent(card, nil, 5)

	// 测试 ReActAgentConfig 类型：应该生效
	cfg := saconfig.NewReActAgentConfig(saconfig.WithModelName("test-model"))
	err := sup.Configure(context.Background(), cfg)
	if err != nil {
		t.Fatalf("Configure 返回错误: %v", err)
	}

	// 测试非 ReActAgentConfig 类型：应该 no-op
	// 用一个实现了 AgentConfig 接口的 mock 类型
	err = sup.Configure(context.Background(), &mockAgentConfig{})
	if err != nil {
		t.Fatalf("Configure 非 ReActAgentConfig 应返回 nil，got: %v", err)
	}
}

// mockAgentConfig 用于测试 Configure 的 no-op 路径
type mockAgentConfig struct{}

func (m *mockAgentConfig) ModelName() string                                  { return "" }
func (m *mockAgentConfig) MemScopeID() string                                 { return "" }
func (m *mockAgentConfig) GetContextEngineConfig() ceschema.ContextEngineConfig { return ceschema.ContextEngineConfig{} }
func (m *mockAgentConfig) GetModelClientConfig() *llmschema.ModelClientConfig  { return nil }
```

- [ ] **Step 5: 添加 config=nil 构造测试**

在 `supervisor_agent_test.go` 添加：

```go
func TestNewSupervisorAgent_Config为nil(t *testing.T) {
	card := &agentschema.AgentCard{ID: "sup-nil", Name: "supervisor-nil"}
	// config=nil 应正常创建，不 panic
	sup := NewSupervisorAgent(card, nil, 5)
	if sup == nil {
		t.Fatal("NewSupervisorAgent(card, nil) 返回 nil")
	}
}
```

- [ ] **Step 6: 运行测试**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/multi_agent/teams/hierarchical_msgbus/... -count=1 -timeout 120s`
Expected: PASS

- [ ] **Step 7: Commit**

```
fix(hierarchical-msgbus): SupervisorAgent 添加 Configure 覆写 + config=nil 对齐 Python 语义
```

---

## Task 3：P2PAbilityManager.Execute 完全并行 + fallback（修复 #3 + #4）

**Files:**
- Modify: `internal/agentcore/multi_agent/teams/hierarchical_msgbus/p2p_ability_manager.go`
- Modify: `internal/agentcore/multi_agent/teams/hierarchical_msgbus/p2p_ability_manager_test.go`

- [ ] **Step 1: 修改 Execute 方法，other 调用放入 goroutine（修复 #3）**

将 `p2p_ability_manager.go` 中 Execute 方法的 other 调用部分从：

```go
// 其他调用委托基类
if len(otherIndices) > 0 {
	otherToolCalls := make([]*llmschema.ToolCall, len(otherIndices))
	for j, idx := range otherIndices {
		otherToolCalls[j] = toolCalls[idx]
	}
	otherResults := m.AbilityManager.Execute(ctx, cbc, otherToolCalls, sess, tag)
	for j, idx := range otherIndices {
		results[idx] = otherResults[j]
	}
}

wg.Wait()
```

改为：

```go
// 其他调用也放入 goroutine，对齐 Python asyncio.gather 全并行语义
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
```

- [ ] **Step 2: 修改 executeSingleP2P 添加 fallback 路径（修复 #4）**

将 `executeSingleP2P` 签名增加 `cbc *rail.AgentCallbackContext` 参数，并在方法开头添加 IsAgent 判断：

```go
func (m *P2PAbilityManager) executeSingleP2P(
	ctx context.Context,
	cbc *rail.AgentCallbackContext,
	toolCall *llmschema.ToolCall,
	sess sessioninterfaces.SessionFacade,
) (agentschema.ExecuteResult, error) {
	toolName := toolCall.Name

	// 非 Agent 调用 fallback：委托基类
	// 对齐 Python: if tool_name not in self._agents: return await super()._execute_single_tool_call(...)
	if !m.IsAgent(toolName) {
		singleResults := m.AbilityManager.Execute(ctx, cbc, []*llmschema.ToolCall{toolCall}, sess, "")
		if len(singleResults) > 0 {
			return singleResults[0], nil
		}
		return agentschema.ExecuteResult{}, nil
	}

	// ... 后续 P2P 派发逻辑不变 ...
```

同时更新 Execute 中调用 executeSingleP2P 的地方，传入 cbc：

```go
r, err := m.executeSingleP2P(ctx, cbc, tc, sess)
```

- [ ] **Step 3: 编译验证**

Run: `cd /home/opensource/uap-claw-go && go build ./internal/agentcore/multi_agent/teams/hierarchical_msgbus/...`
Expected: 编译成功

- [ ] **Step 4: 运行现有测试**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/multi_agent/teams/hierarchical_msgbus/... -count=1 -timeout 120s`
Expected: PASS

- [ ] **Step 5: Commit**

```
fix(hierarchical-msgbus): P2PAbilityManager.Execute agent/other 全并行 + executeSingleP2P fallback 路径
```

---

## Task 4：HierarchicalToolsTeam AddAgent 合并 + 移除幂等 + Stream 错误传播（修复 #8 + #9 + #10）

**Files:**
- Modify: `internal/agentcore/multi_agent/teams/hierarchical_tools/hierarchical_team.go`
- Modify: `internal/agentcore/multi_agent/teams/hierarchical_tools/hierarchical_team_test.go`

- [ ] **Step 1: 合并 AddAgent 和 AddAgentWithParent（修复 #8）**

将现有的 `AddAgent` 和 `AddAgentWithParent` 合并为一个 `AddAgent` 方法：

```go
func (t *HierarchicalToolsTeam) AddAgent(ctx context.Context, card *agentschema.AgentCard, provider maschema.TeamAgentProvider, opts ...maschema.TeamOption) error {
	if t.runtime.HasAgent(card.ID) {
		logger.Warn(toolsLogComponent).
			Str("action", "add_agent_skip").
			Str("agent_id", card.ID).
			Str("team_id", t.card.GetID()).
			Msg("Agent 已存在，跳过注册")
		return nil
	}

	// 注册到运行时
	wrappedProvider := resources_manager.AgentProvider(provider)
	if err := t.runtime.RegisterAgent(ctx, card, wrappedProvider); err != nil {
		logger.Error(toolsLogComponent).Err(err).
			Str("event_type", "LLM_CALL_ERROR").
			Str("method", "HierarchicalToolsTeam.AddAgent").
			Str("agent_id", card.ID).
			Msg("注册 Agent 到运行时失败")
		return err
	}

	// 识别 rootAgent
	if card.ID == t.rootAgentID {
		logger.Info(toolsLogComponent).
			Str("action", "add_agent_root").
			Str("root_agent_id", card.ID).
			Str("team_id", t.card.GetID()).
			Msg("注册 root_agent 到 HierarchicalToolsTeam")
	}

	// 对齐 Python: if parent_agent_id: self._pending_children.setdefault(parent_agent_id, []).append(card)
	teamOpts := maschema.NewTeamOptions(opts...)
	if teamOpts.ParentAgentID != "" {
		t.pendingChildren[teamOpts.ParentAgentID] = append(t.pendingChildren[teamOpts.ParentAgentID], card)
		logger.Debug(toolsLogComponent).
			Str("action", "add_agent_with_parent").
			Str("child_id", card.ID).
			Str("parent_id", teamOpts.ParentAgentID).
			Msg("记录父子关系到 pendingChildren")
	}

	logger.Info(toolsLogComponent).
		Str("action", "add_agent").
		Str("agent_id", card.ID).
		Str("team_id", t.card.GetID()).
		Msg("Agent 已注册到 HierarchicalToolsTeam")

	return nil
}
```

**删除** `AddAgentWithParent` 方法。

- [ ] **Step 2: 移除 setupHierarchy 幂等保护（修复 #9）**

1. 从 `HierarchicalToolsTeam` 结构体中**删除** `hierarchySetup bool` 字段
2. 修改 `NewHierarchicalToolsTeam`，删除 `hierarchySetup: false`
3. 修改 `setupHierarchy`：

```go
func (t *HierarchicalToolsTeam) setupHierarchy(ctx context.Context) error {
	if len(t.pendingChildren) == 0 {
		return nil
	}

	resourceMgr := runner.GetResourceMgr()
	if resourceMgr == nil {
		logger.Warn(toolsLogComponent).
			Str("action", "setup_hierarchy").
			Msg("ResourceMgr 为空，跳过层级建立")
		return nil
	}

	for parentID, childCards := range t.pendingChildren {
		parentAgents, err := resourceMgr.GetAgent(ctx, []string{parentID})
		if err != nil || len(parentAgents) == 0 {
			logger.Error(toolsLogComponent).Err(err).
				Str("event_type", "LLM_CALL_ERROR").
				Str("method", "HierarchicalToolsTeam.setupHierarchy").
				Str("parent_id", parentID).
				Msg("获取父 Agent 实例失败")
			return exception.BuildError(exception.StatusAgentTeamAgentNotFound,
				exception.WithParam("error_msg", fmt.Sprintf("父 Agent '%s' 实例未找到", parentID)),
			)
		}

		parentAgent := parentAgents[0]
		am := parentAgent.AbilityManager()
		if am == nil {
			logger.Warn(toolsLogComponent).
				Str("action", "setup_hierarchy").
				Str("parent_id", parentID).
				Msg("父 Agent 无 AbilityManager，跳过子 Agent 注册")
			continue
		}

		for _, childCard := range childCards {
			am.Add(childCard)
			logger.Debug(toolsLogComponent).
				Str("action", "setup_hierarchy_register").
				Str("child_id", childCard.ID).
				Str("parent_id", parentID).
				Msg("子 Agent 已注册到父 Agent 的 ability_manager")
		}
	}

	// 对齐 Python: self._pending_children.clear()
	t.pendingChildren = make(map[string][]*agentschema.AgentCard)
	return nil
}
```

- [ ] **Step 3: Stream 添加错误流传播（修复 #10）**

修改 Stream 方法中 callback 内部，在 `agent.Stream()` 返回 error 时写错误到流：

将 Stream callback 中：

```go
ch, streamErr := agent.Stream(ctx, inputsWithSID)
if streamErr != nil {
	logger.Error(toolsLogComponent).Err(streamErr).
		Str("event_type", "LLM_CALL_ERROR").
		Str("method", "HierarchicalToolsTeam.Stream").
		Str("root_agent_id", t.rootAgentID).
		Msg("root_agent.Stream() 调用失败")
	return streamErr
}
```

改为：

```go
ch, streamErr := agent.Stream(ctx, inputsWithSID)
if streamErr != nil {
	logger.Error(toolsLogComponent).Err(streamErr).
		Str("event_type", "LLM_CALL_ERROR").
		Str("method", "HierarchicalToolsTeam.Stream").
		Str("root_agent_id", t.rootAgentID).
		Msg("root_agent.Stream() 调用失败")
	// 对齐 Python: error_result = {"output": str(e), "result_type": "error"}
	errorResult := map[string]any{
		"output":      streamErr.Error(),
		"result_type": "error",
	}
	_ = teamSession.WriteStream(ctx, errorResult)
	return streamErr
}
```

- [ ] **Step 4: 更新测试中 AddAgentWithParent 调用**

搜索测试代码中所有 `AddAgentWithParent` 调用，替换为 `AddAgent(ctx, card, provider, maschema.WithParentAgentID(parentID))`。

- [ ] **Step 5: 编译验证**

Run: `cd /home/opensource/uap-claw-go && go build ./internal/agentcore/multi_agent/teams/hierarchical_tools/...`
Expected: 编译成功

- [ ] **Step 6: 运行测试**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/multi_agent/teams/hierarchical_tools/... -count=1 -timeout 120s`
Expected: PASS

- [ ] **Step 7: Commit**

```
fix(hierarchical-tools): AddAgent 合并 WithParent + 移除幂等保护 + Stream 错误传播
```

---

## Task 5：全量编译 + 测试 + 适配剩余调用方

**Files:**
- 所有在 Task 1-4 中未覆盖的 AddAgent 调用方

- [ ] **Step 1: 全量编译**

Run: `cd /home/opensource/uap-claw-go && go build ./...`
Expected: 编译成功（如失败，按错误逐个修复 AddAgent 签名适配）

- [ ] **Step 2: 全量测试**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/... -count=1 -timeout 300s`
Expected: PASS

- [ ] **Step 3: Commit**

```
chore: 适配 BaseTeam.AddAgent opts ...TeamOption 签名变更的剩余调用方
```

---

## Task 6：更新 IMPLEMENTATION_PLAN.md 状态

**Files:**
- Modify: `IMPLEMENTATION_PLAN.md`

- [ ] **Step 1: 确认 8.35 和 8.36 已标记为 ✅**

检查 IMPLEMENTATION_PLAN.md 中 8.35 和 8.36 的状态，确认已为 ✅（本次修复是对齐补丁，不改变完成状态）。

- [ ] **Step 2: Commit（如有变更）**

```
docs: 更新 IMPLEMENTATION_PLAN.md 8.35/8.36 对齐修复记录
```

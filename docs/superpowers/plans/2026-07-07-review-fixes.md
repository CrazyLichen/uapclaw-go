# Review 2026-07-06 已确认问题修复 实现计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 修复 2026-07-06 daily review 中已确认的14个问题，一比一还原Python逻辑

**Architecture:** 按优先级分4组：P0并发安全(5个) → P1功能缺失(3个) → G-01流式输出(1个大任务) → P2一般问题(5个)。每组内部按文件分组，减少上下文切换。

**Tech Stack:** Go 1.22+, sync.RWMutex, sync.Mutex, context.Context, channel

---

## 文件结构

| 文件 | 职责 | 修改任务 |
|------|------|---------|
| `harness/deep_agent.go` | DeepAgent 核心实现 | S-01, G-01 |
| `harness/task_loop/handler.go` | 事件处理器 | S-03 |
| `harness/task_loop/session_spawn_executor.go` | SessionSpawn执行器 | G-02, G-06 |
| `harness/schema/config.go` | 配置定义 | S-12/13/15 |
| `harness/interfaces/deep_agent.go` | 接口定义 | S-12/13/15 |
| `harness/tools/subagent/session_tools.go` | Session工具 | G-05 |
| `multi_agent/teams/handoff/handoff_team.go` | Handoff团队 | S-06, S-07 |
| `multi_agent/team_runtime/team_runtime.go` | 团队运行时 | S-05, S-16, G-08/09/10 |

---

## Task 1: S-01 — DeepAgent.runTaskLoopInvoke err 变量遮蔽

**Files:**
- Modify: `internal/agentcore/harness/deep_agent.go:1563-1687`

**问题:** `if err := ctrl.SubmitRound(...)` 用 `:=` 遮蔽了外层 err，导致 `WaitRoundCompletion` 后 `if err != nil` 永远为 false（死代码），方法永远返回 nil error。

**Python对照:** `_run_task_loop_invoke` 中异常正确传播，不会静默吞掉错误。

- [ ] **Step 1: 修复 SubmitRound 的 err 遮蔽**

将 `if err := ctrl.SubmitRound(...)` 改为赋值给外层 err：

```go
// 原来（遮蔽）:
if err := ctrl.SubmitRound(ctx, sessConcrete, string(currentQuery.PlainText()), false, modified.RunKind, modified.RunContext); err != nil {

// 修改后（统一外层err）:
if err = ctrl.SubmitRound(ctx, sessConcrete, string(currentQuery.PlainText()), false, modified.RunKind, modified.RunContext); err != nil {
```

注意：`:=` 改为 `=`，这样赋值给外层的 `err` 变量。

- [ ] **Step 2: 修复 WaitRoundCompletion 错误检查**

当前 `WaitRoundCompletion` 不返回 error，`if err != nil` 是死代码。需要：
1. 确认 `WaitRoundCompletion` 是否应该返回 error
2. 如果 WaitRoundCompletion 的 result 中包含错误信息（如 `result_type == "error"`），改为从 result 中提取错误：

```go
result := ctrl.WaitRoundCompletion(ctx, &timeout)
// 从 result 中检查错误（对齐 Python: wait_round_completion 返回的 result dict）
resultType, _ := result["result_type"].(string)
if resultType == "error" {
    errMsg, _ := result["output"].(string)
    err = fmt.Errorf("轮次完成返回错误: %s", errMsg)
    logger.Error(logComponent).Err(err).Int("round", outerRound).Msg("等待轮次完成失败")
    break
}
```

- [ ] **Step 3: 确保方法最终返回 err**

确认 `return lastResult, nil` 改为 `return lastResult, err`，让错误正确传播给调用方（对齐Python的 `raise` 行为）。

- [ ] **Step 4: 运行测试**

```bash
cd /home/opensource/uap-claw-go && export GOPROXY=https://goproxy.cn,direct && go test ./internal/agentcore/harness/... -run TestDeepAgent -count=1 -v 2>&1 | head -100
```

- [ ] **Step 5: Commit**

```bash
git add internal/agentcore/harness/deep_agent.go
git commit -m "fix(S-01): 修复 runTaskLoopInvoke err 变量遮蔽，确保错误正确传播"
```

---

## Task 2: S-03 — completeSessionSpawn 无锁读取

**Files:**
- Modify: `internal/agentcore/harness/task_loop/handler.go:475-534`

**问题:** `completeSessionSpawn` 直接读取 `h.sessionToolkit` 和 `h.interactionQueues` 未持 `h.mu`，而 `SetSessionToolkit`/`SetInteractionQueues` 在 `h.mu` 保护下写入。

**Python对照:** Python asyncio单线程无此问题，但Go需要加锁保护并发访问。

- [ ] **Step 1: 在 completeSessionSpawn 中加锁读取**

在方法开头加锁读取，复制到局部变量后释放锁：

```go
func (h *TaskLoopEventHandler) completeSessionSpawn(taskID string, input *modules.EventHandlerInput, isError bool) {
	// 加锁读取 sessionToolkit 和 interactionQueues（对齐 SetSessionToolkit/SetInteractionQueues 的锁模式）
	h.mu.Lock()
	sessionToolkit := h.sessionToolkit
	interactionQueues := h.interactionQueues
	h.mu.Unlock()

	var resultStr string
	var errorStr string
	if isError {
		errorStr = extractErrorFromEvent(input)
	} else {
		resultStr = extractResultFromEvent(input)
	}

	// 使用局部变量替代直接读取
	if sessionToolkit != nil {
		if isError {
			sessionToolkit.MarkFailed(taskID, errorStr)
		} else {
			sessionToolkit.MarkCompleted(taskID, resultStr)
		}
	}

	// ... 中间代码不变 ...

	// 使用局部变量替代
	if interactionQueues != nil {
		interactionQueues.PushSteer(steerText)
	}

	// 路径2中同样使用局部变量
	if !h.provider.IsAutoInvokeScheduled() {
		// ...
	}
}
```

将方法体中所有 `h.sessionToolkit` 替换为 `sessionToolkit`，`h.interactionQueues` 替换为 `interactionQueues`。

- [ ] **Step 2: 运行测试**

```bash
cd /home/opensource/uap-claw-go && export GOPROXY=https://goproxy.cn,direct && go test ./internal/agentcore/harness/task_loop/... -count=1 -v 2>&1 | head -100
```

- [ ] **Step 3: Commit**

```bash
git add internal/agentcore/harness/task_loop/handler.go
git commit -m "fix(S-03): completeSessionSpawn 加锁读取 sessionToolkit/interactionQueues"
```

---

## Task 3: S-06 — HandoffTeam.coordinatorRegistry 无并发保护

**Files:**
- Modify: `internal/agentcore/multi_agent/teams/handoff/handoff_team.go`

**问题:** `coordinatorRegistry` 是普通 map，被 runChain（写/删）和 lookupCoordinator 闭包（读）并发访问。

**Python对照:** Python asyncio单线程无此问题。

- [ ] **Step 1: 添加 registryMu 字段**

在 HandoffTeam 结构体中添加 `registryMu sync.RWMutex` 字段，放在 `coordinatorRegistry` 字段附近。

- [ ] **Step 2: 保护写操作**

在 `runChain` 方法中对 `coordinatorRegistry` 的写入和删除操作加写锁：

```go
// 写入
t.registryMu.Lock()
t.coordinatorRegistry[sessionID] = coordinator
t.registryMu.Unlock()

// 删除
t.registryMu.Lock()
delete(t.coordinatorRegistry, sessionID)
t.registryMu.Unlock()
```

- [ ] **Step 3: 保护读操作**

在 `lookupCoordinator` 方法中加读锁：

```go
func (t *HandoffTeam) lookupCoordinator(sessionID string) *HandoffOrchestrator {
	t.registryMu.RLock()
	defer t.registryMu.RUnlock()
	return t.coordinatorRegistry[sessionID]
}
```

- [ ] **Step 4: 运行测试**

```bash
cd /home/opensource/uap-claw-go && export GOPROXY=https://goproxy.cn,direct && go test ./internal/agentcore/multi_agent/teams/handoff/... -count=1 -v 2>&1 | head -100
```

- [ ] **Step 5: Commit**

```bash
git add internal/agentcore/multi_agent/teams/handoff/handoff_team.go
git commit -m "fix(S-06): coordinatorRegistry 加 sync.RWMutex 并发保护"
```

---

## Task 4: S-07 — resetInternalAgents sync.Once 重置竞态

**Files:**
- Modify: `internal/agentcore/multi_agent/teams/handoff/handoff_team.go`

**问题:** `ensureInternalAgents` 不获取 `initLock`，直接调用 `internalAgentsOnce.Do()`，与 `resetInternalAgents`（在 initLock 下重置 Once）存在数据竞争。

**修复方案:** 去掉 `sync.Once`，改用 `initLock` + `internalAgentsReady` bool 控制。

- [ ] **Step 1: 去掉 internalAgentsOnce 字段**

从 HandoffTeam 结构体中删除 `internalAgentsOnce sync.Once` 字段。

- [ ] **Step 2: 简化 resetInternalAgents**

```go
func (t *HandoffTeam) resetInternalAgents() {
	t.internalAgentsReady = false
	t.internalAgentsErr = nil
}
```

去掉 `t.internalAgentsOnce = sync.Once{}` 行。

- [ ] **Step 3: 重写 ensureInternalAgents**

改为在 `initLock` 保护下检查 bool 并执行初始化：

```go
func (t *HandoffTeam) ensureInternalAgents(ctx context.Context) error {
	t.initLock.Lock()
	defer t.initLock.Unlock()

	if t.internalAgentsReady {
		return t.internalAgentsErr
	}

	err := t.initInternalAgents(ctx)
	if err != nil {
		t.internalAgentsErr = err
		return err
	}
	t.internalAgentsReady = true
	return nil
}
```

注意：需确认 `initInternalAgents` 不会递归调用 `ensureInternalAgents`（否则死锁）。检查 `initInternalAgents` 实现，如果会递归则需要调整。

- [ ] **Step 4: 运行测试**

```bash
cd /home/opensource/uap-claw-go && export GOPROXY=https://goproxy.cn,direct && go test ./internal/agentcore/multi_agent/teams/handoff/... -count=1 -v 2>&1 | head -100
```

- [ ] **Step 5: Commit**

```bash
git add internal/agentcore/multi_agent/teams/handoff/handoff_team.go
git commit -m "fix(S-07): 去掉 sync.Once，改用 initLock+bool 控制 ensureInternalAgents"
```

---

## Task 5: S-05 — TeamRuntime.Send/Publish 缺少自动启动

**Files:**
- Modify: `internal/agentcore/multi_agent/team_runtime/team_runtime.go`

**问题:** Python `send()`/`publish()` 调用 `await self._ensure_started()` 自动启动runtime，Go只检查 `IsRunning()` 返回error。

**Python对照:** `team_runtime.py:153-159` — `_ensure_started` 双检锁懒启动。

- [ ] **Step 1: 添加 startMu 字段**

在 TeamRuntime 结构体中添加 `startMu sync.Mutex` 字段（用于 `_ensure_started` 双检锁）。

- [ ] **Step 2: 实现 ensureStarted 方法**

对齐 Python `_ensure_started` (line 153-159)：

```go
// ensureStarted 确保运行时已启动（懒启动）。
// 对齐 Python: TeamRuntime._ensure_started() (line 153-159)
func (tr *TeamRuntime) ensureStarted(ctx context.Context) error {
	if tr.IsRunning() {
		return nil
	}
	tr.startMu.Lock()
	defer tr.startMu.Unlock()
	if tr.IsRunning() {
		return nil
	}
	return tr.Start(ctx)
}
```

- [ ] **Step 3: 替换 Send 中的 IsRunning 检查**

将 `Send` 方法开头的 `if !tr.IsRunning() { return nil, fmt.Errorf(...) }` 替换为：

```go
if err := tr.ensureStarted(ctx); err != nil {
	return nil, err
}
```

- [ ] **Step 4: 替换 Publish 中的 IsRunning 检查**

同样替换 `Publish` 方法开头的 `if !tr.IsRunning() { return fmt.Errorf(...) }`。

- [ ] **Step 5: 运行测试**

```bash
cd /home/opensource/uap-claw-go && export GOPROXY=https://goproxy.cn,direct && go test ./internal/agentcore/multi_agent/team_runtime/... -count=1 -v 2>&1 | head -100
```

- [ ] **Step 6: Commit**

```bash
git add internal/agentcore/multi_agent/team_runtime/team_runtime.go
git commit -m "fix(S-05): 实现 ensureStarted 懒启动，对齐Python _ensure_started"
```

---

## Task 6: S-12/S-13/S-15 — Subagents 改为 SubagentSpec 接口切片

**Files:**
- Modify: `internal/agentcore/harness/schema/config.go`
- Modify: `internal/agentcore/harness/interfaces/deep_agent.go`
- Modify: `internal/agentcore/harness/deep_agent.go`

**问题:** Go的 `Subagents []SubAgentConfig` 只接受SubAgentConfig，Python支持 `SubAgentConfig | DeepAgent`。findSubagentSpec缺少DeepAgent实例搜索分支。CreateSubagent缺少 `isinstance(spec, DeepAgent)` 分支。

**Python对照:**
- `_find_subagent_spec` (line 1032-1054): 遍历 subagents，同时检查 isinstance(SubAgentConfig) 和 isinstance(DeepAgent)
- `create_subagent` (line 898-920): isinstance(spec, DeepAgent) → 直接返回spec
- `DeepAgentConfig.subagents` 类型: `Optional[List[SubAgentConfig | DeepAgent]]`

- [ ] **Step 1: 修改 DeepAgentConfig.Subagents 类型**

在 `config.go` 中，将 `Subagents []SubAgentConfig` 改为：

```go
// Subagents 子 Agent 规格列表。
// 支持放入 *SubAgentConfig 或 *DeepAgent（通过 SubagentSpec 接口统一）。
// 对齐 Python: subagents: Optional[List[SubAgentConfig | DeepAgent]]
Subagents []hinterfaces.SubagentSpec `json:"subagents,omitempty"`
```

注意：需要调整 import，`hinterfaces` 包引用 `schema` 包，可能产生循环依赖。如果出现循环依赖，则将 `SubagentSpec` 接口移到独立的小包（如 `harness/schema/subagent_spec.go`），或者不改变字段类型，而是在 `findSubagentSpec` 中增加 DeepAgent 搜索分支（更简单，避免循环依赖问题）。

**备选方案（无循环依赖风险）：** 保持 `Subagents []SubAgentConfig` 不变，在 DeepAgent 中新增 `subagentInstances` 字段存放 DeepAgent 实例，`findSubagentSpec` 两个集合都搜索。但这不符合Python的统一列表语义。

**推荐方案：** 先评估循环依赖。如果 `schema` 包已有对 `interfaces` 包的引用，直接改类型；否则在 `findSubagentSpec` 和 `CreateSubagent` 中增加 DeepAgent 实例的处理逻辑，Subagents 类型暂不改。

- [ ] **Step 2: 修改 findSubagentSpec 搜索 DeepAgent 实例**

对齐 Python `_find_subagent_spec` (line 1032-1054)：

```go
func (d *DeepAgent) findSubagentSpec(subagentType string) hinterfaces.SubagentSpec {
	d.configMu.RLock()
	cfg := d.deepConfig
	d.configMu.RUnlock()

	if cfg == nil {
		return nil
	}

	// 对齐 Python: isinstance(spec, SubAgentConfig) and spec.agent_card.name == subagent_type
	for i := range cfg.Subagents {
		spec := &cfg.Subagents[i]
		if spec.SpecName() == subagentType {
			return spec
		}
	}

	// 对齐 Python: isinstance(spec, DeepAgent)
	// 搜索 DeepAgent 实例（通过 SubagentSpec 接口或额外字段）
	// 如果 Subagents 类型改为 []SubagentSpec，此分支自然覆盖
	// 否则需要从 DeepAgent 实例列表中搜索

	return nil
}
```

- [ ] **Step 3: 修改 CreateSubagent 增加 DeepAgent 实例分支**

对齐 Python `create_subagent` (line 911-920)：

```go
func (d *DeepAgent) CreateSubagent(subagentType string, subSessionID string) (hinterfaces.DeepAgentInterface, error) {
	spec := d.findSubagentSpec(subagentType)
	if spec == nil {
		return nil, exception.BuildError(exception.StatusDeepagentCreateSubagentNotFound,
			exception.WithParam("error_msg", fmt.Sprintf("Subagent spec not found: %s", subagentType)),
		)
	}

	// 对齐 Python: isinstance(spec, DeepAgent) → 直接返回
	if da, ok := spec.(*DeepAgent); ok {
		logger.Info(logComponent).Str("subagent_type", subagentType).Msg("已获取 DeepAgent 实例，直接返回")
		return da, nil
	}

	// 原有逻辑：从 SubAgentConfig 构建新实例
	subCfg, ok := spec.(*hschema.SubAgentConfig)
	if !ok {
		return nil, exception.BuildError(exception.StatusDeepagentCreateSubagentNotFound,
			exception.WithParam("error_msg", "spec 类型不支持"),
		)
	}
	// ... 后续构建逻辑不变 ...
}
```

- [ ] **Step 4: 运行测试**

```bash
cd /home/opensource/uap-claw-go && export GOPROXY=https://goproxy.cn,direct && go test ./internal/agentcore/harness/... -count=1 -v 2>&1 | head -100
```

- [ ] **Step 5: Commit**

```bash
git add internal/agentcore/harness/schema/config.go internal/agentcore/harness/interfaces/deep_agent.go internal/agentcore/harness/deep_agent.go
git commit -m "fix(S-12/13/15): SubagentSpec 支持 DeepAgent 实例，findSubagentSpec/CreateSubagent 对齐Python"
```

---

## Task 7: S-16 — TeamRuntime.Send recipient 错误码不一致

**Files:**
- Modify: `internal/agentcore/multi_agent/team_runtime/team_runtime.go:337-365`

**问题:** sender不存在返回 `StatusAgentTeamAgentNotFound`，recipient不存在用 `fmt.Errorf`（无错误码）。

- [ ] **Step 1: 修复 recipient 不存在的错误码**

将 `Send` 方法中 recipient 不存在的错误从 `fmt.Errorf` 改为结构化错误码：

```go
// 原来:
if !tr.HasAgent(recipient) {
	return nil, fmt.Errorf("接收者 Agent %s 不存在", recipient)
}

// 修改后:
if !tr.HasAgent(recipient) {
	return nil, exception.BuildError(exception.StatusAgentTeamAgentNotFound,
		exception.WithParam("error_msg", fmt.Sprintf("接收者 Agent %s 不存在", recipient)),
	)
}
```

- [ ] **Step 2: 运行测试**

```bash
cd /home/opensource/uap-claw-go && export GOPROXY=https://goproxy.cn,direct && go test ./internal/agentcore/multi_agent/team_runtime/... -count=1 -v 2>&1 | head -100
```

- [ ] **Step 3: Commit**

```bash
git add internal/agentcore/multi_agent/team_runtime/team_runtime.go
git commit -m "fix(S-16): recipient 不存在时使用结构化错误码 StatusAgentTeamAgentNotFound"
```

---

## Task 8: G-01 — DeepAgent Stream 一比一复刻 Python 流式实现

**Files:**
- Modify: `internal/agentcore/harness/deep_agent.go`

**问题:** task-loop分支的Stream是stub，Python实现两层写入模式：底层LLM实时写入session流 + 每轮完成后写入轮结果。

**Python对照（完整数据流）:**
```
_run_task_loop_stream():
  后台 _stream_process():
    async for result in _run_task_loop(ctx, session):    ← 逐轮迭代生成器
      _write_round_result_to_stream(result, session)      ← 每轮结果写入session流
    except → 写入错误结果到流
    finally → session.close_stream()                      ← 发送END_FRAME

  _run_task_loop 内部每轮:
    controller.submit_round →
    controller.wait_round_completion →
      内部: react_agent.invoke(..., _streaming=True)       ← LLM streaming 实时写入session流
    yield result                                           ← yield给消费者

  前台:
    async for chunk in session.stream_iterator():          ← 统一读取所有chunk
      yield chunk
```

**Go端已有基础设施:**
- `session.StreamIterator()` → Python `stream_iterator()`
- `session.CloseStream()` → Python `close_stream()`
- `reactAgent.WriteInvokeResultToStream()` → Python `_write_invoke_result_to_stream()`
- `agentinterfaces.WithSession(sess)` → 传递session给Invoke

- [ ] **Step 1: 新增 runTaskLoop 方法（逐轮channel生成器）**

对齐 Python `_run_task_loop` (line 1991-2110)。这是 AsyncIterator 生成器的Go等价实现：

```go
// runTaskLoop 任务循环生成器，每轮完成后将 result 发送到 channel。
// 对齐 Python: DeepAgent._run_task_loop(ctx, session) (line 1991-2110)
// invoke 和 stream 共用此方法：
//   invoke: 从 channel 读取最后一轮结果
//   stream: 后台goroutine从channel读取，每轮写入session流
func (d *DeepAgent) runTaskLoop(ctx context.Context, cbc *rail.AgentCallbackContext, sess *session.Session) (<-chan map[string]any, error) {
	modified, ok := cbc.Inputs().(*rail.InvokeInputs)
	if !ok {
		return nil, exception.BuildError(exception.StatusDeepagentContextParamError,
			exception.WithMsg("ctx.inputs 必须为 InvokeInputs 类型"))
	}

	coord, ctrl, err := d.setupTaskLoop(ctx, sess)
	if err != nil {
		return nil, err
	}

	// 绑定会话（对齐 Python line 2013-2017）
	d.configMu.RLock()
	boundID := d.boundSessionID
	d.configMu.RUnlock()

	sessionID := sess.GetSessionID()
	if boundID != sessionID {
		if err := ctrl.BindSession(ctx, sess); err != nil {
			logger.Warn(logComponent).Err(err).Msg("绑定会话失败")
		}
		d.configMu.Lock()
		d.boundSessionID = sessionID
		d.configMu.Unlock()
	}

	// 创建输出channel
	outCh := make(chan map[string]any, 1)

	// 启动循环goroutine
	go func() {
		defer close(outCh)

		// 对齐 Python: try/finally 确保清理
		defer func() {
			// 清理逻辑（对齐 Python line 2095-2110）
			state := d.LoadState(sess)
			state.StopConditionState = nil
			d.saveState(sess, state)

			if !d.hasPendingSessionSpawn() {
				_ = ctrl.UnbindSession(ctx, sess)
				_ = ctrl.Stop(ctx)
				d.configMu.Lock()
				d.loopCoordinator = nil
				d.loopController = nil
				d.loopSession = nil
				d.boundSessionID = ""
				d.configMu.Unlock()
				logLoop("all tasks completed, controller cleaned up", "", 0)
			} else {
				logLoop("pending SESSION_SPAWN tasks, controller kept alive", "", 0)
			}
		}()

		currentQuery := modified.Query
		outerRound := 0

		d.configMu.RLock()
		timeout := hschema.DefaultCompletionTimeout
		if d.deepConfig != nil && d.deepConfig.CompletionTimeout > 0 {
			timeout = d.deepConfig.CompletionTimeout
		}
		d.configMu.RUnlock()

		for coord.ShouldContinue() {
			outerRound++

			// 排空 follow-up（对齐 Python line 2026-2040）
			_ = ctrl.DrainFollowUp()

			queryPreview := currentQuery.PlainText()
			if len(queryPreview) > 120 {
				queryPreview = queryPreview[:120]
			}
			logLoop("round=%d started", fmt.Sprintf(", query=%s", queryPreview), outerRound)

			if err = ctrl.SubmitRound(ctx, sess, string(currentQuery.PlainText()), false, modified.RunKind, modified.RunContext); err != nil {
				logger.Error(logComponent).Err(err).Int("round", outerRound).Msg("提交轮次失败")
				break
			}

			result := ctrl.WaitRoundCompletion(ctx, &timeout)

			resultType, _ := result["result_type"].(string)
			outputPreview := ""
			if output, ok := result["output"].(string); ok {
				outputPreview = output
				if len(outputPreview) > 200 {
					outputPreview = outputPreview[:200]
				}
			}
			logLoop("round=%d completed, result_type=%s", fmt.Sprintf(", output=%s", outputPreview), outerRound, resultType)

			// yield result（对齐 Python line 2063: yield result）
			select {
			case outCh <- result:
			case <-ctx.Done():
				return
			}

			coord.IncrementIteration()
			coord.SetLastResult(result)

			// 更新状态
			st := d.LoadState(sess)
			exported := coord.ExportState()
			st.StopConditionState = map[string]any{
				"iteration":        exported.Iteration,
				"token_usage":      exported.TokenUsage,
				"stop_reason":      exported.StopReason,
				"evaluator_states": exported.EvaluatorStates,
			}
			d.saveState(sess, st)

			if resultType == "interrupt" {
				logLoop("round=%d interrupted", "", outerRound)
				break
			}
			if coord.IsAborted() {
				logLoop("round=%d aborted", "", outerRound)
				break
			}

			currentQuery = modified.Query
		}
	}()

	return outCh, nil
}
```

- [ ] **Step 2: 重构 runTaskLoopInvoke 消费 runTaskLoop**

对齐 Python `_run_task_loop_invoke` (line 2112-2144)：

```go
func (d *DeepAgent) runTaskLoopInvoke(ctx context.Context, cbc *rail.AgentCallbackContext, sess sessioninterfaces.SessionFacade) (map[string]any, error) {
	sessConcrete, ok := sess.(*session.Session)
	if !ok || sessConcrete == nil {
		return nil, exception.BuildError(exception.StatusDeepagentRuntimeError,
			exception.WithMsg("任务循环模式需要 *session.Session 类型会话"))
	}

	loopCh, err := d.runTaskLoop(ctx, cbc, sessConcrete)
	if err != nil {
		return nil, err
	}

	// 对齐 Python: last_result: Dict[str, Any] = {}
	// async for result in _run_task_loop(): last_result = result
	var lastResult map[string]any
	for result := range loopCh {
		lastResult = result
	}

	// 对齐 Python: return last_result
	return lastResult, nil
}
```

- [ ] **Step 3: 新增 writeRoundResultToStream 辅助方法**

对齐 Python `_write_round_result_to_stream` (line 2214-2232)：

```go
// writeRoundResultToStream 将任务循环轮次结果写入会话流。
// 对齐 Python: DeepAgent._write_round_result_to_stream(result, session) (line 2214-2232)
func (d *DeepAgent) writeRoundResultToStream(ctx context.Context, result map[string]any, sess *session.Session) {
	d.configMu.RLock()
	reactAgent := d.reactAgent
	d.configMu.RUnlock()

	if reactAgent != nil {
		reactAgent.WriteInvokeResultToStream(ctx, result, sess)
	}
}
```

- [ ] **Step 4: 实现 runTaskLoopStream**

对齐 Python `_run_task_loop_stream` (line 2146-2212)：

```go
func (d *DeepAgent) runTaskLoopStream(ctx context.Context, invokeInputs *rail.InvokeInputs, sess sessioninterfaces.SessionFacade, streamModes []stream.StreamMode) (<-chan stream.Schema, error) {
	if sess == nil {
		return nil, exception.BuildError(exception.StatusDeepagentRuntimeError,
			exception.WithMsg("任务循环模式需要会话"))
	}

	sessConcrete, ok := sess.(*session.Session)
	if !ok || sessConcrete == nil {
		return nil, exception.BuildError(exception.StatusDeepagentRuntimeError,
			exception.WithMsg("任务循环模式需要 *session.Session 类型会话"))
	}

	// 构建 AgentCallbackContext
	cbc := rail.NewAgentCallbackContext(d, invokeInputs, sess)

	// outCh: 对外输出的流式channel
	outCh := make(chan stream.Schema, 64)

	// 后台 goroutine: 运行任务循环，每轮结果写入session流
	// 对齐 Python: _stream_process() (line 2182-2194)
	go func() {
		// 对齐 Python: finally → session.close_stream()
		defer func() {
			_ = sessConcrete.CloseStream()
		}()

		loopCh, loopErr := d.runTaskLoop(ctx, cbc, sessConcrete)
		if loopErr != nil {
			// 对齐 Python line 2187-2189: except → 写入错误结果到流
			d.writeRoundResultToStream(ctx, map[string]any{
				"output":      loopErr.Error(),
				"result_type": "error",
			}, sessConcrete)
			return
		}

		// 对齐 Python line 2184-2185: async for result in _run_task_loop(): _write_round_result_to_stream
		for result := range loopCh {
			d.writeRoundResultToStream(ctx, result, sessConcrete)
		}
	}()

	// 前台: 从 session.StreamIterator() 读取chunk转发到 outCh
	// 对齐 Python line 2199-2200: async for chunk in session.stream_iterator(): yield chunk
	go func() {
		defer close(outCh)
		for chunk := range sessConcrete.StreamIterator() {
			select {
			case outCh <- chunk:
			case <-ctx.Done():
				return
			}
		}
	}()

	return outCh, nil
}
```

- [ ] **Step 5: 运行测试**

```bash
cd /home/opensource/uap-claw-go && export GOPROXY=https://goproxy.cn,direct && go test ./internal/agentcore/harness/... -run TestDeepAgent -count=1 -v 2>&1 | head -100
```

- [ ] **Step 6: Commit**

```bash
git add internal/agentcore/harness/deep_agent.go
git commit -m "feat(G-01): 实现 runTaskLoopStream 流式输出，一比一复刻Python两层写入模式"
```

---

## Task 9: G-02 — SessionSpawnExecutor 子Agent invoke 未传 Session

**Files:**
- Modify: `internal/agentcore/harness/task_loop/session_spawn_executor.go:94-157`

**问题:** `subAgent.ReactAgent().Invoke(ctx, effective)` 未传 session。

**Python对照:** `subagent.invoke({"query": query, "conversation_id": cid})` 隐式传递session。Go的 DeepExecutor 已正确传递 `agentinterfaces.WithSession(sess)`。

- [ ] **Step 1: 添加 WithSession 选项**

在 session_spawn_executor.go 中，找到 `subAgent.ReactAgent().Invoke(ctx, effective)` 调用（约第115行），改为：

```go
result, invokeErr := subAgent.ReactAgent().Invoke(ctx, effective, agentinterfaces.WithSession(sess))
```

需要确认 `sess` 变量在作用域内可用（从 `ExecuteAbility` 方法参数获取）。

- [ ] **Step 2: 运行测试**

```bash
cd /home/opensource/uap-claw-go && export GOPROXY=https://goproxy.cn,direct && go test ./internal/agentcore/harness/task_loop/... -count=1 -v 2>&1 | head -100
```

- [ ] **Step 3: Commit**

```bash
git add internal/agentcore/harness/task_loop/session_spawn_executor.go
git commit -m "fix(G-02): SessionSpawnExecutor 子Agent invoke 传递 Session"
```

---

## Task 10: G-05 — joinLines 空切片 panic

**Files:**
- Modify: `internal/agentcore/harness/tools/subagent/session_tools.go:475-481`

- [ ] **Step 1: 加空切片 guard**

```go
func joinLines(lines []string) string {
	if len(lines) == 0 {
		return ""
	}
	result := lines[0]
	for i := 1; i < len(lines); i++ {
		result += "\n" + lines[i]
	}
	return result
}
```

- [ ] **Step 2: 运行测试**

```bash
cd /home/opensource/uap-claw-go && export GOPROXY=https://goproxy.cn,direct && go test ./internal/agentcore/harness/tools/subagent/... -count=1 -v 2>&1 | head -100
```

- [ ] **Step 3: Commit**

```bash
git add internal/agentcore/harness/tools/subagent/session_tools.go
git commit -m "fix(G-05): joinLines 空切片防御，返回空字符串而非 panic"
```

---

## Task 11: G-06 — SessionSpawnExecutor GetTask 错误返回模式不一致

**Files:**
- Modify: `internal/agentcore/harness/task_loop/session_spawn_executor.go:51-70`

**问题:** GetTask错误时返回 `(ch, nil)` 而非 `(nil, err)`，错误通过channel传递而非Go error return。

- [ ] **Step 1: 修改同步错误路径返回模式**

将 GetTask 错误和任务未找到的错误改为通过 Go error return 返回：

```go
tasks, err := e.deps.TaskManager.GetTask(ctx, MakeFilter(taskID))
if err != nil {
	logger.Error(logComponent).
		Err(err).
		Str("task_id", taskID).
		Str("event_type", "LLM_CALL_ERROR").
		Str("method", "SessionSpawnExecutor.ExecuteAbility").
		Msg("查询任务失败")
	close(ch)
	return nil, err  // 改为返回 error
}
if len(tasks) == 0 {
	logger.Warn(logComponent).
		Str("task_id", taskID).
		Msg("未找到任务")
	close(ch)
	return nil, fmt.Errorf("task %s not found", taskID)  // 改为返回 error
}
```

- [ ] **Step 2: 运行测试**

```bash
cd /home/opensource/uap-claw-go && export GOPROXY=https://goproxy.cn,direct && go test ./internal/agentcore/harness/task_loop/... -count=1 -v 2>&1 | head -100
```

- [ ] **Step 3: Commit**

```bash
git add internal/agentcore/harness/task_loop/session_spawn_executor.go
git commit -m "fix(G-06): SessionSpawnExecutor GetTask 错误通过Go error return返回"
```

---

## Task 12: G-08/G-09/G-10 — Send/Publish/Subscribe 参数校验

**Files:**
- Modify: `internal/agentcore/multi_agent/team_runtime/team_runtime.go`

**Python对照:**
- `send()`: `if not recipient: raise build_error(...)` (line 360-363)
- `publish()`: `if not topic_id: raise build_error(...)` (类似)
- `subscribe()`: `if not agent_id: raise ...` + `if not topic: raise ...`

- [ ] **Step 1: Send 添加 recipient 空字符串校验**

在 `HasAgent(recipient)` 检查之前加：

```go
if recipient == "" {
	return nil, exception.BuildError(exception.StatusAgentTeamExecutionError,
		exception.WithParam("error_msg", "recipient 不能为空"),
	)
}
```

- [ ] **Step 2: Publish 添加 topicID 空字符串校验**

```go
if topicID == "" {
	return exception.BuildError(exception.StatusAgentTeamExecutionError,
		exception.WithParam("error_msg", "topic_id 不能为空"),
	)
}
```

- [ ] **Step 3: Subscribe 添加参数校验**

```go
func (tr *TeamRuntime) Subscribe(ctx context.Context, agentID string, topic string) error {
	if agentID == "" {
		return exception.BuildError(exception.StatusAgentTeamExecutionError,
			exception.WithParam("error_msg", "agent_id 不能为空"),
		)
	}
	if topic == "" {
		return exception.BuildError(exception.StatusAgentTeamExecutionError,
			exception.WithParam("error_msg", "topic 不能为空"),
		)
	}
	if tr.messageBus != nil {
		tr.messageBus.AddSubscription(agentID, topic)
	}
	return nil
}
```

- [ ] **Step 4: Unsubscribe 添加参数校验**

同Subscribe模式，加 `agentID` 和 `topic` 空字符串校验。

- [ ] **Step 5: 运行测试**

```bash
cd /home/opensource/uap-claw-go && export GOPROXY=https://goproxy.cn,direct && go test ./internal/agentcore/multi_agent/team_runtime/... -count=1 -v 2>&1 | head -100
```

- [ ] **Step 6: Commit**

```bash
git add internal/agentcore/multi_agent/team_runtime/team_runtime.go
git commit -m "fix(G-08/09/10): Send/Publish/Subscribe/Unsubscribe 添加参数空字符串校验"
```

---

## 自查清单

- [ ] 所有14个确认问题都有对应Task
- [ ] 每个Task都有Python对照代码的行号引用
- [ ] 没有TBD/TODO占位符
- [ ] 方法签名和类型在Task之间一致
- [ ] 测试命令可执行

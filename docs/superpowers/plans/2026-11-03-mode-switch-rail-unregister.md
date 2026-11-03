# 模式切换注销失效 + 规划模式置位 实施计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 让 `UnregisterRail` 真正移除回调（修 23 处静默失效），并让 web 规划模式状态真正置位。

**Architecture:** 方案 A 在 `DeepAgent` 注册 rail 时缓存 `rail → map[event]fn`，注销复用同一批 fn（`OffPerAgent` 按 `%p` 比较，只有同一批 fn 才能命中）；方案 D1 在 `DeepAdapter.updateRailsForMode` 分派 rails 的同时调用现成的 `SwitchMode("plan"/"normal")` 置位状态。

**Tech Stack:** Go 标准 `testing`（含 `sync/atomic`）；集成测试用 MockLLM + testify suite。

**设计文档:** `docs/superpowers/specs/2026-11-03-mode-switch-rail-unregister-design.md`

---

## 文件结构

| 文件 | 责任 | 动作 |
|---|---|---|
| `internal/agentcore/harness/deep_agent.go` | DeepAgent 主体：RegisterRail/UnregisterRail/registerRailSelective | 新增 `railCallbacks` 缓存；注销改用缓存 fn |
| `internal/agentcore/harness/deep_agent_rail_unregister_test.go` | 注销生效的回归单测 | 新增 |
| `internal/swarm/server/adapter/deep_adapter_rails.go` | `updateRailsForMode` 模式分派 | 签名加 sessionID；新增 `switchModeForState` |
| `internal/swarm/server/adapter/deep_adapter_config.go:142` | updateRuntimeConfig 调用点 | 传入 `config.SessionID` |
| `tests/integration/swarm/server/adapter/mode_switch_todo_test.go` | 全链路回归 | 观测改为断言 |

---

## Task 1: 方案 A —— DeepAgent 缓存回调（TDD）

**Files:**
- Create: `internal/agentcore/harness/deep_agent_rail_unregister_test.go`
- Modify: `internal/agentcore/harness/deep_agent.go`（结构体 ~:117、`UnregisterRail` :513、`registerRailSelective`）

- [ ] **Step 1: 写失败测试**

创建 `internal/agentcore/harness/deep_agent_rail_unregister_test.go`：

```go
package harness

import (
	"context"
	"sync/atomic"
	"testing"

	cb "github.com/uapclaw/uapclaw-go/internal/agentcore/runner/callback"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	agentschema "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/schema"
)

// ──────────────────────────── 结构体 ────────────────────────────

// fakeUnregisterRail 每次调用 GetCallbacks 都返回新闭包，复现生产 rail 的构造方式。
// 回归 2026-11-03 缺陷：注销经 GetCallbacks() 新建闭包、OffPerAgent 按 %p 比较
// 永不命中，钩子留在框架里继续触发（todo 节每轮回填、提示词每轮膨胀即此因）。
type fakeUnregisterRail struct {
	calls atomic.Int64
}

// ──────────────────────────── 导出函数 ────────────────────────────

func (f *fakeUnregisterRail) Priority() int { return 50 }

func (f *fakeUnregisterRail) Init(_ context.Context, _ agentinterfaces.BaseAgent) error {
	return nil
}

func (f *fakeUnregisterRail) Uninit(_ agentinterfaces.BaseAgent) error { return nil }

func (f *fakeUnregisterRail) BeforeInvoke(_ context.Context, _ *agentinterfaces.AgentCallbackContext) error {
	return nil
}

func (f *fakeUnregisterRail) AfterInvoke(_ context.Context, _ *agentinterfaces.AgentCallbackContext) error {
	return nil
}

func (f *fakeUnregisterRail) BeforeTaskIteration(_ context.Context, _ *agentinterfaces.AgentCallbackContext) error {
	return nil
}

func (f *fakeUnregisterRail) AfterTaskIteration(_ context.Context, _ *agentinterfaces.AgentCallbackContext) error {
	return nil
}

func (f *fakeUnregisterRail) BeforeModelCall(_ context.Context, _ *agentinterfaces.AgentCallbackContext) error {
	return nil
}

func (f *fakeUnregisterRail) AfterModelCall(_ context.Context, _ *agentinterfaces.AgentCallbackContext) error {
	return nil
}

func (f *fakeUnregisterRail) OnModelException(_ context.Context, _ *agentinterfaces.AgentCallbackContext) error {
	return nil
}

func (f *fakeUnregisterRail) BeforeToolCall(_ context.Context, _ *agentinterfaces.AgentCallbackContext) error {
	return nil
}

func (f *fakeUnregisterRail) AfterToolCall(_ context.Context, _ *agentinterfaces.AgentCallbackContext) error {
	return nil
}

func (f *fakeUnregisterRail) OnToolException(_ context.Context, _ *agentinterfaces.AgentCallbackContext) error {
	return nil
}

// GetCallbacks 每次调用都构造新闭包（与 TaskPlanningRail.GetCallbacks 同型）。
// 注意必须用非桥接事件 CallbackAfterInvoke：裸 NewDeepAgent 无 reactAgent，
// 桥接事件会被 registerRailSelective 跳过，触发不到。
func (f *fakeUnregisterRail) GetCallbacks() map[agentinterfaces.AgentCallbackEvent]cb.PerAgentCallbackFunc {
	return map[agentinterfaces.AgentCallbackEvent]cb.PerAgentCallbackFunc{
		agentinterfaces.CallbackAfterInvoke: func(_ context.Context, _ any) error {
			f.calls.Add(1)
			return nil
		},
	}
}

// ──────────────────────────── 导出函数（测试） ────────────────────────────

// TestDeepAgent_UnregisterRail_注销后回调不再触发 修复后注销必须复用注册时缓存的 fn。
func TestDeepAgent_UnregisterRail_注销后回调不再触发(t *testing.T) {
	agent := NewDeepAgent(agentschema.NewAgentCard())
	rail := &fakeUnregisterRail{}
	ctx := context.Background()

	if err := agent.RegisterRail(ctx, rail); err != nil {
		t.Fatalf("RegisterRail: %v", err)
	}

	railCtx := &agentinterfaces.AgentCallbackContext{}
	if err := agent.CallbackManager().Execute(ctx, agentinterfaces.CallbackAfterInvoke, railCtx); err != nil {
		t.Fatalf("首次 Execute: %v", err)
	}
	if got := rail.calls.Load(); got != 1 {
		t.Fatalf("注册后应触发 1 次, 实际 %d", got)
	}

	if err := agent.UnregisterRail(ctx, rail); err != nil {
		t.Fatalf("UnregisterRail: %v", err)
	}

	if err := agent.CallbackManager().Execute(ctx, agentinterfaces.CallbackAfterInvoke, railCtx); err != nil {
		t.Fatalf("注销后 Execute: %v", err)
	}
	if got := rail.calls.Load(); got != 1 {
		t.Fatalf("注销后不应再触发, 实际 %d（注销静默失效）", got)
	}

	// 重复注销不应 panic
	if err := agent.UnregisterRail(ctx, rail); err != nil {
		t.Fatalf("重复 UnregisterRail: %v", err)
	}
}
```

- [ ] **Step 2: 跑测试确认失败**

Run: `CGO_ENABLED=1 go test -tags "sqlite_fts5 test" ./internal/agentcore/harness/ -run TestDeepAgent_UnregisterRail -count=1 -v`
Expected: FAIL，`注销后不应再触发, 实际 2（注销静默失效）`。

- [ ] **Step 3: 实现缓存**

3a. `internal/agentcore/harness/deep_agent.go` 结构体 `registeredRails`/`railsMu`（:116-119）后新增字段：

```go
	// railCallbacks 每个 rail 注册时缓存的事件→回调映射。
	// 注销必须复用同一批 fn：GetCallbacks() 每次现场构造新闭包，而
	// OffPerAgent 按 %p 比较——用新闭包注销永远匹配不上（2026-11-03）。
	// Python 无此问题：bound method 的 == 是结构比较，新对象也相等。
	railCallbacks map[agentinterfaces.AgentRail]map[agentinterfaces.AgentCallbackEvent]cb.PerAgentCallbackFunc
```

3b. `registerRailSelective` 开头缓存（注册逻辑不变）：

```go
func (d *DeepAgent) registerRailSelective(ctx context.Context, r agentinterfaces.AgentRail) {
	callbacks := r.GetCallbacks()

	// 缓存本次注册的 fn，供 UnregisterRail 复用（见 railCallbacks 字段注释）
	d.railsMu.Lock()
	if d.railCallbacks == nil {
		d.railCallbacks = make(map[agentinterfaces.AgentRail]map[agentinterfaces.AgentCallbackEvent]cb.PerAgentCallbackFunc)
	}
	d.railCallbacks[r] = callbacks
	d.railsMu.Unlock()

	for event, callback := range callbacks {
	// ……以下原逻辑不变……
```

3c. `UnregisterRail`（:513）整体替换为：

```go
// UnregisterRail 注销 Rail。
// Python: BaseAgent.unregister_rail(rail) (line 1198)
//
// 回调注销必须使用注册时缓存的 fn（railCallbacks）：GetCallbacks() 每次
// 现场构造新闭包，OffPerAgent 按 %p 比较，新闭包永远匹配不上——那会让
// 注销静默失效、钩子继续触发（2026-11-03 修复）。
func (d *DeepAgent) UnregisterRail(ctx context.Context, r agentinterfaces.AgentRail) error {
	d.railsMu.Lock()
	// 从 pendingRails 中移除
	d.pendingRails = removeRailByRef(d.pendingRails, r)
	// 从 registeredRails 中移除
	d.registeredRails = removeRailByRef(d.registeredRails, r)
	cached := d.railCallbacks[r]
	delete(d.railCallbacks, r)

	// 检查 TaskCompletionRail
	if tc, ok := r.(*rails.TaskCompletionRail); ok && d.taskCompletionRail == tc {
		d.taskCompletionRail = nil
	}
	d.railsMu.Unlock()

	if cached != nil {
		// 用缓存的同一批 fn 注销：同时传给内外两层管理器，
		// 未注册的一方 Off 是无害 no-op（按 fn 指针匹配不上即跳过）。
		for event, fn := range cached {
			_ = d.callbackManager.Unregister(event, fn)
			d.configMu.RLock()
			reactAgent := d.reactAgent
			d.configMu.RUnlock()
			if reactAgent != nil {
				reactAgent.CallbackManager().Unregister(event, fn)
			}
		}
	} else {
		// 无缓存（rail 未走 RegisterRail 注册）回退旧行为
		_ = d.callbackManager.UnregisterRail(ctx, r)
		d.configMu.RLock()
		reactAgent := d.reactAgent
		d.configMu.RUnlock()
		if reactAgent != nil {
			_ = reactAgent.CallbackManager().UnregisterRail(ctx, r)
		}
	}

	_ = r.Uninit(d)
	return nil
}
```

- [ ] **Step 4: 跑测试确认通过 + 包内全测**

Run:
```bash
CGO_ENABLED=1 go test -tags "sqlite_fts5 test" ./internal/agentcore/harness/ -run TestDeepAgent_UnregisterRail -count=1 -v
CGO_ENABLED=1 go test -tags "sqlite_fts5 test" ./internal/agentcore/harness/... -count=1
```
Expected: 回归用例 PASS；harness 包全部 PASS（若 harness 下其他用例受回调缓存影响而失败，逐个核查是否依赖"重复注册同一 rail"的旧行为）。

- [ ] **Step 5: 提交**

```bash
git add internal/agentcore/harness/deep_agent.go internal/agentcore/harness/deep_agent_rail_unregister_test.go
git commit -m "fix(agentcore): UnregisterRail 复用注册时缓存的回调，修注销静默失效

GetCallbacks() 每次现场构造闭包，OffPerAgent 按 %p 比较永不命中，
导致 23 处 UnregisterRail 只执行了 Uninit 副作用、钩子仍在触发——
性能模式下 todo 提示词节每轮回填、ContextAssembleRail 新旧双跑致
系统提示词每轮膨胀 7KB 均源于此。"
```

---

## Task 2: 方案 D1 —— web 切模式同步置位 plan 状态

**Files:**
- Modify: `internal/swarm/server/adapter/deep_adapter_rails.go:984`（`updateRailsForMode`）
- Modify: `internal/swarm/server/adapter/deep_adapter_config.go:142`（调用点）

- [ ] **Step 1: 改造 `updateRailsForMode`**

把：

```go
func (d *DeepAdapter) updateRailsForMode(ctx context.Context, mode string) {
	// Python: if mode == "agent.plan": await self._update_plan_mode_rails()
	// Python: else: await self._update_agent_mode_rails(mode)
	if mode == "agent.plan" {
		d.updatePlanModeRails(ctx)
	} else {
		d.updateAgentModeRails(ctx, mode)
	}
}
```

替换为：

```go
func (d *DeepAdapter) updateRailsForMode(ctx context.Context, mode, sessionID string) {
	// Python: if mode == "agent.plan": await self._update_plan_mode_rails()
	// Python: else: await self._update_agent_mode_rails(mode)
	if mode == "agent.plan" {
		d.updatePlanModeRails(ctx)
		d.switchModeForState(ctx, sessionID, "plan")
	} else {
		d.updateAgentModeRails(ctx, mode)
		d.switchModeForState(ctx, sessionID, "normal")
	}
}

// switchModeForState 把请求携带的 mode 同步进 agent 状态机
// （DeepAgentState.PlanMode.Mode），使 AgentModeRail 的 plan 分支真正生效。
//
// 注意：这是对 Python 的有意偏离——Python web 链路只在 code 模式调
// switch_mode（agent_ws_server.py:1145-1150），agent.plan 的状态从不置位，
// plan 分支实际休眠。本次按产品决策让 web 规划模式完整生效
// （隐藏 todo/session 工具 + plan 指令节 + 白名单拦截）。
//
// sessionID 为空（CreateInstance 阶段）跳过；SwitchMode 自带同模式幂等
// （deep_agent.go:975），每条消息重复调用无副作用。失败仅告警不阻断请求。
func (d *DeepAdapter) switchModeForState(ctx context.Context, sessionID, planMode string) {
	if sessionID == "" {
		return
	}
	if err := d.SwitchMode(ctx, sessionID, planMode); err != nil {
		logger.Warn(logComponent).Err(err).
			Str("session_id", sessionID).
			Str("plan_mode", planMode).
			Msg("switch plan state failed, continuing")
	}
}
```

- [ ] **Step 2: 更新调用点**

`internal/swarm/server/adapter/deep_adapter_config.go:142`：

```go
	// 步骤 7: rail/tool 模式切换（仅 evolution 分支已实现）
	// Python: await self._update_rails_for_mode(runtime_config.mode)
	d.updateRailsForMode(ctx, config.Mode, config.SessionID)
```

改完后 `grep -rn "updateRailsForMode(ctx" internal/swarm/server/adapter/ | grep -v _test` 确认 DeepAdapter 的调用点已全部更新（`code_adapter.go` 的同名方法是独立实现，不改）。

- [ ] **Step 3: 编译 + 包内测试**

Run:
```bash
CGO_ENABLED=1 go build -tags sqlite_fts5 ./internal/swarm/...
CGO_ENABLED=1 go test -tags "sqlite_fts5 test" ./internal/swarm/server/adapter/ -count=1
```
Expected: 编译通过；adapter 包全 PASS（`TestUpdateRuntimeConfig_*` 系列若因签名变更需要 mock，按编译器提示补 `config.SessionID` 即可）。

- [ ] **Step 4: 提交**

```bash
git add internal/swarm/server/adapter/deep_adapter_rails.go internal/swarm/server/adapter/deep_adapter_config.go
git commit -m "feat(adapter): web 切模式时同步置位 plan 状态，让规划模式真正生效

updateRailsForMode 分派 rails 的同时调用 SwitchMode(plan/normal)，
使 AgentModeRail 的 plan 分支（plan 指令节/隐藏 todo 工具/白名单拦截）
真正运行。对 Python 的有意偏离：Python web 链路 plan 状态从不置位，
plan 分支实际休眠（agent_ws_server.py:1145 仅 code 模式调用）。"
```

---

## Task 3: 集成用例从观测改为断言

**Files:**
- Modify: `tests/integration/swarm/server/adapter/mode_switch_todo_test.go`

- [ ] **Step 1: 把 `observe` 改为断言**

将测试方法与 `observe` 替换为（保留 `runTurn`/`lastCallMessages`/`hasEvent`/`eventTypes`/`truncateForLog`/`headings` 辅助函数与常量）：

```go
// TestPlan到性能模式_模式语义完整 断言切换前后三处状态全部一致：
// 规划模式 plan 分支生效（todo 节移除、工具隐藏但已注册）；
// 性能模式 rail 注销彻底（无 todo 节、无 todo 工具、提示词不膨胀）。
func (s *ModeSwitchTodoSuite) TestPlan到性能模式_模式语义完整() {
	s.env.WriteConfigYAML(s.T(), "")

	d := adapter.NewDeepAdapter()
	s.Require().NoError(d.CreateInstance(s.Ctx, map[string]any{}, "agent.plan", ""))
	s.T().Cleanup(func() { _ = d.Cleanup() })

	// ── 第 1 轮：规划模式 ──
	s.MockLLM.SetResponses(
		mockllm.CreateToolCallResponse("todo_create", modeSwitchTodoArgs),
		mockllm.CreateTextResponse("规划完成。"),
	)
	planEvents := s.runTurn(d, "req-plan-1", "agent.plan", "把需求拆成待办")
	s.Require().True(hasEvent(planEvents, "todo.updated"),
		"规划模式下 todo_create 应产生 todo.updated（工具已注册，只是对模型隐藏），实际: %v", eventTypes(planEvents))

	inst := d.Instance()
	s.Require().NotNil(inst)
	sb := inst.SystemPromptBuilder()
	s.Require().NotNil(sb)
	s.False(sb.HasSection("todo"), "规划模式 plan 分支应移除 todo 节")
	planSystem := s.lastSystemPrompt()
	s.NotContains(planSystem, todoSectionMarker, "规划模式实发提示词不应含 todo 指令")
	s.Equal([]string{"todo_create", "todo_get", "todo_list", "todo_modify"},
		registeredTodoTools(inst), "规划模式工具应已注册（plan 只是对模型隐藏）")

	// ── 第 2 轮：切到性能模式，模型仍尝试调用 todo_create ──
	s.MockLLM.SetResponses(
		mockllm.CreateToolCallResponse("todo_create", modeSwitchTodoArgs),
		mockllm.CreateTextResponse("执行完成。"),
	)
	agentEvents := s.runTurn(d, "req-agent-2", "agent", "继续执行")

	inst = d.Instance()
	sb = inst.SystemPromptBuilder()
	s.Require().NotNil(sb)
	s.False(sb.HasSection("todo"), "性能模式 TaskPlanningRail 已注销，todo 节不应存在")
	s.Empty(registeredTodoTools(inst), "性能模式 todo 工具应全部注销")
	agentSystem := s.lastSystemPrompt()
	s.NotContains(agentSystem, todoSectionMarker, "性能模式实发提示词不应含 todo 指令")

	// 提示词不得重复拼段（旧 ContextAssembleRail 残留会导致 +7KB 与标题重复）
	heads := headings(agentSystem)
	s.Equal(len(heads), len(uniqueStrings(heads)), "系统提示词一级标题不应重复: %v", heads)
	s.LessOrEqual(len(agentSystem), len(planSystem)+1000,
		"性能模式提示词长度相对规划模式不应膨胀（plan=%d agent=%d）", len(planSystem), len(agentSystem))

	// 性能模式 todo_create 不应再成功执行（无 todo.updated）
	s.False(hasEvent(agentEvents, "todo.updated"),
		"性能模式 todo 工具已注销，不应产生 todo.updated，实际事件: %v", eventTypes(agentEvents))
}

// lastSystemPrompt 返回最近一次模型调用实收的系统提示词。
func (s *ModeSwitchTodoSuite) lastSystemPrompt() string {
	s.T().Helper()
	rec := s.lastCallMessages()
	if rec.IsEmpty() {
		return ""
	}
	for _, m := range rec.Messages() {
		if m.GetRole() == llmschema.RoleTypeSystem {
			return m.GetContent().Text()
		}
	}
	return ""
}

// registeredTodoTools 返回 AbilityManager 中已注册的 todo 工具名（升序）。
func registeredTodoTools(inst *harness.DeepAgent) []string {
	out := make([]string, 0, 4)
	if am := inst.AbilityManager(); am != nil {
		for _, ability := range am.List() {
			if name := ability.AbilityName(); strings.HasPrefix(name, "todo_") {
				out = append(out, name)
			}
		}
	}
	sort.Strings(out)
	return out
}

// uniqueStrings 去重保序。
func uniqueStrings(in []string) []string {
	seen := make(map[string]bool, len(in))
	out := make([]string, 0, len(in))
	for _, s := range in {
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	return out
}
```

删除不再使用的旧 `TestPlan到性能模式_todo残留观测`、`observe`，以及仅被它们使用的辅助（若 `truncateForLog`/`progressReminderMarker` 不再被引用则一并删除，避免 staticcheck unused）。

- [ ] **Step 2: 跑集成用例**

Run:
```bash
CGO_ENABLED=1 go test -tags "sqlite_fts5 test integration" \
  ./tests/integration/swarm/server/adapter/ -run TestModeSwitchTodoSuite -count=1 -v 2>&1 | tail -30
```
Expected: PASS。

**失败处置对照**：
- `规划模式 plan 分支应移除 todo 节` 失败 → D1 未生效，查 `updateRailsForMode` 是否收到 sessionID（`runTurn` 的 params 里要带 session_id 对应的 `req.SessionID`，已带）。
- `性能模式 todo 节不应存在` 失败 → A 未生效，查 `railCallbacks` 缓存路径。
- `不应产生 todo.updated` 失败 → 出现 spec 预判的"执行路径残留"（ReAct 工具快照/ResourceMgr），在 plan 之外单独记录，不阻塞本任务提交，但要在汇报中列出。

- [ ] **Step 3: 提交**

```bash
git add tests/integration/swarm/server/adapter/mode_switch_todo_test.go
git commit -m "test(integration): 模式切换用例从观测改为断言

断言规划模式 plan 分支生效（todo 节移除、工具隐藏但注册）、性能模式
注销彻底（无节/无工具/提示词不膨胀/不重复拼段）、todo_create 不再成功。"
```

---

## Task 4: 本地全量门禁

- [ ] **Step 1: 单测 + 覆盖率**

```bash
CGO_ENABLED=1 go test -tags "sqlite_fts5 test" \
  ./internal/agentcore/harness/ ./internal/swarm/server/adapter/ ./internal/swarm/server/utils/ -count=1 -cover
```
Expected: 全 ok。

- [ ] **Step 2: 改动行覆盖率门禁**

```bash
CGO_ENABLED=1 go test -tags "sqlite_fts5 test" -coverprofile=/tmp/changed2.out \
  ./internal/agentcore/harness/ ./internal/swarm/server/adapter/ -count=1 >/dev/null
BASE=$(git merge-base origin/main HEAD)
bash scripts/check-changed-line-coverage.sh /tmp/changed2.out "$BASE" 85 .coverage-exempt
```
Expected: 通过（阈值 85%）。

- [ ] **Step 3: race**

```bash
CGO_ENABLED=1 go test -tags "sqlite_fts5 test" -race \
  ./internal/agentcore/harness/ ./internal/swarm/server/adapter/ -count=1
```
Expected: 无 DATA RACE（`railCallbacks` 在 `railsMu` 内读写）。

- [ ] **Step 4: 格式与静态**

```bash
"$(go env GOROOT)/bin/gofmt" -l internal/agentcore/harness/deep_agent.go \
  internal/agentcore/harness/deep_agent_rail_unregister_test.go \
  internal/swarm/server/adapter/deep_adapter_rails.go \
  internal/swarm/server/adapter/deep_adapter_config.go \
  tests/integration/swarm/server/adapter/mode_switch_todo_test.go
go vet -tags "sqlite_fts5 test" ./internal/agentcore/harness/ ./internal/swarm/server/adapter/
```
Expected: 均无输出/无告警。

- [ ] **Step 5: 集成审计**

```bash
go run ./cmd/integration-audit -verify 2>&1 | tail -3
```
Expected: 违规 0。

---

## Task 5: 推送并确认 CI 四绿

- [ ] **Step 1: fetch 对齐后推送分支**

```bash
git fetch origin && git rebase origin/main
git push -u origin fix/review-2026-09-23
```

- [ ] **Step 2: 触发 CI**

`ci.yml` 只在 push 到 main 或 PR 到 main 触发，且当前 PAT 无 `createPullRequest` 权限。二选一（按用户指示）：
- 用户在 GitHub 上开 PR：https://github.com/CrazyLichen/uapclaw-go/pull/new/fix/review-2026-09-23
- 用户授权后 `git push origin HEAD:main`

- [ ] **Step 3: 跟踪 CI**

```bash
gh run list --limit 3 --branch main
gh run watch <run-id> --exit-status
```
Expected: Build / Lint / Unit Test / Integration Test 四 job 全绿；失败按 `gh run view <run-id> --log-failed` 修复后重推。

- [ ] **Step 4: 汇报**

包含：改动清单、四条集成断言结果、CI run id、"不做"边界（TeamAgent/ContainerAgent 注销路径、进度提醒跨模式残留、ResourceMgr 执行残留若有）。

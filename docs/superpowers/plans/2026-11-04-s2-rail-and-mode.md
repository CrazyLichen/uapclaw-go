# S2 实施计划：Rail 生命周期与模式合并

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 把模式串三段化、plan 状态机、rail 装配、todo 体系一次性对齐上游，并把 S1 的 attachment 通道接上全部真实消费方。

**Architecture:** 新增 `internal/swarm/runtime/`（`PlanModeController` + 请求模式解析），替换 adapter 里的 `switchModeForState/updatePlanModeRails` 为幂等 `UpdateAgentRails`；各 rail 从 `AddSection` 改为 `AddFromPromptSection`；todo 关闭常驻提示词、code 模式改用带提醒的 `CodeTaskPlanningRail`。

**Tech Stack:** Go 1.26、testify、`tests/integration`（`//go:build integration` + `suite.Run`）、`mockllm`、`testhelpers.NewAdapterTestEnv`。

**Spec:** `docs/superpowers/specs/2026-11-04-s2-rail-and-mode-design.md`

---

## 文件结构

| 文件 | 职责 |
| --- | --- |
| `internal/swarm/schema/mode.go`（改） | 三段串常量；删两段常量 |
| `internal/swarm/runtime/doc.go`（新建） | 包文档 |
| `internal/swarm/runtime/request.go`（新建） | `ResolveRequestRuntimeMode`、`CHAT_TURN_METHODS`、模式/来源常量 |
| `internal/swarm/runtime/plan.go`（新建） | `PlanModeController`（EnsureState / CheckPostProcessExit / ResetSession / 守卫 / `SessionMessageRequiresQueue` 占位） |
| `internal/swarm/server/adapter/deep_adapter_rails.go`（改） | 删 `updatePlanModeRails`/`switchModeForState`；新增 `UpdateAgentRails`；接入 controller |
| `internal/swarm/server/adapter/*.go`（改） | 实现 `PlanStateStore` 适配器（Load/Save/Switch/Commit） |
| `internal/agentcore/harness/rails/agent_mode.go`（改） | plan 指引改 attachment；删 `RemoveSection` |
| `internal/agentcore/harness/rails/task_planning.go`（改） | `InjectPrompt` 开关默认 false；删 `BuildTodoSection` |
| `internal/swarm/agents/harness/code/rails/code_task_planning_rail.go`（新建） | code todo + 10/10 提醒 + LRU |
| `internal/swarm/agents/harness/code/tools/code_todo_tools.go`（新建） | code 版 todo 工具文案与 schema |
| 其余 5 组 rail（改） | `task_completion` / `progressive` / `skills` / `memory`×3 / `context_assemble` 改 attachment |
| `tests/integration/swarm/server/adapter/attachment_mode/`（新建） | IT-S2-1~13 |

命名规则：Go 函数名 = Python 函数名（snake_case → CamelCase），见 spec §6。

---

### Task 1: 模式串三段化（含 IT-S2-1/2）

**Files:**
- Modify: `internal/swarm/schema/mode.go`
- Modify: 全仓引用点（slash 解析、channel_state、memory 映射、runtime_prompt 展示、cron 默认）
- Test: `tests/integration/swarm/server/adapter/attachment_mode/mode_string_test.go`

- [x] **Step 1: 先写失败的集成测试**

```go
//go:build integration

package attachment_mode

import (
	"context"
	"testing"

	"github.com/stretchr/testify/suite"

	"github.com/uapclaw/uapclaw-go/internal/swarm/schema"
	"github.com/uapclaw/uapclaw-go/tests/integration/testhelpers"
)

// ModeStringSuite 覆盖模式串三段化的正/负向行为。
type ModeStringSuite struct{ suite.Suite }

// TestModeStringSuite 运行 ModeStringSuite。
func TestModeStringSuite(t *testing.T) { suite.Run(t, new(ModeStringSuite)) }

// TestIT_S2_1_新串进入plan 验证 agent.work.plan 走 plan 分支。
func (s *ModeStringSuite) TestIT_S2_1_新串进入plan() {
	env := testhelpers.NewAdapterTestEnv(s.T(), nil, "s2mode")
	_ = env
	agent, err := newDeepAdapterForTest(s.T(), env) // 见 tests/integration/swarm/server/adapter 既有构造助手
	s.Require().NoError(err)

	s.Require().NoError(agent.SwitchMode(context.Background(), "sess-plan", "plan"))
	s.Require().Equal("plan", agent.CurrentPlanMode(s.T(), "sess-plan"))
}

// TestIT_S2_2_旧串失效 验证 agent.plan 被归一到 normal。
func (s *ModeStringSuite) TestIT_S2_2_旧串失效() {
	s.Require().Equal(schema.ModeAgentWorkNormal, schema.NormalizeMode("agent.plan"))
	s.Require().Equal(schema.ModeAgentWorkPlan, schema.NormalizeMode("agent.work.plan"))
}
```

> 执行说明：`newDeepAdapterForTest` 直接复用 `tests/integration/swarm/server/adapter/deep_adapter_rail_registration_test.go` 里的构造流程（同一个 helper 文件内已有等价函数，复制并按需改名）；`CurrentPlanMode` 若不存在，用 `LoadState(sess).PlanMode.Mode` 取值。

- [x] **Step 2: 运行确认失败**

Run: `CGO_ENABLED=1 go test -tags "sqlite_fts5 test integration" ./tests/integration/swarm/server/adapter/attachment_mode/ -run TestModeStringSuite -v`
Expected: FAIL — `undefined: schema.NormalizeMode`

- [x] **Step 3: 改常量与全仓替换**

`internal/swarm/schema/mode.go`：删除 `ModeAgentPlan`/`ModeAgentFast`，新增：

```go
	// ModeAgentWorkPlan agent-work 规划模式
	ModeAgentWorkPlan Mode = "agent.work.plan"
	// ModeAgentWorkNormal agent-work 常态模式
	ModeAgentWorkNormal Mode = "agent.work.normal"
	// ModeUnknown 未知串回落
	ModeUnknown Mode = ""
```

新增归一函数：

```go
// NormalizeMode 把入站 mode 串归一到权威值；未知串回落 agent.work.normal。
//
// 上游以三段串为唯一权威值（2826a9d23 模式合并），不再兼容两段串。
func NormalizeMode(raw string) Mode {
	switch Mode(strings.TrimSpace(raw)) {
	case ModeAgentWorkPlan:
		return ModeAgentWorkPlan
	case ModeAgentWorkNormal:
		return ModeAgentWorkNormal
	case ModeCodePlan, ModeCodeNormal, ModeCodeTeam:
		return Mode(strings.TrimSpace(raw))
	default:
		return ModeAgentWorkNormal
	}
}
```

全仓替换（先看命中再改）：

```bash
grep -rn '"agent\.fast"' --include=*.go internal | wc -l
grep -rl '"agent\.fast"' --include=*.go internal | xargs sed -i 's/"agent\.fast"/"agent.work.normal"/g'
grep -rl '"agent\.plan"' --include=*.go internal | xargs sed -i 's/"agent\.plan"/"agent.work.plan"/g'
grep -rn 'ModeAgentFast\|ModeAgentPlan' --include=*.go internal | head
```

第三条 grep 必须为空；若仍有引用，逐个改为新常量名。然后处理**裸串映射表**（`memory/config.go` 的 `"plan"`/`"fast"` 键保持不变，但其映射目标改新串）。

- [x] **Step 4: 运行确认通过**

Run: `CGO_ENABLED=1 go build -tags sqlite_fts5 ./... && CGO_ENABLED=1 go test -tags "sqlite_fts5 test integration" ./tests/integration/swarm/server/adapter/attachment_mode/ -run TestModeStringSuite -v`
Expected: PASS（2 例）

- [x] **Step 5: 提交**

```bash
git add -A && git commit -m "feat(mode): 模式串三段化（agent.work.plan/normal），删除两段常量与旧映射"
```

---

### Task 2: 请求模式解析（`runtime/request.go`）

**Files:**
- Create: `internal/swarm/runtime/doc.go`
- Create: `internal/swarm/runtime/request.go`
- Test: `internal/swarm/runtime/request_test.go`

- [x] **Step 1: 写失败测试**

```go
package runtime

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// TestResolveRequestRuntimeMode 覆盖三段/两段/裸串/team 的归一。
//
// Python: jiuwenswarm/runtime/request.py resolve_request_runtime_mode
func TestResolveRequestRuntimeMode(t *testing.T) {
	cases := []struct {
		mode, subMode string
		want          string
		wantPlan      bool
		wantTeam      bool
	}{
		{"agent.work.plan", "", "agent.work.plan", true, false},
		{"agent.work.normal", "", "agent.work.normal", false, false},
		{"code.plan", "", "code.plan", true, false},
		{"code.team", "", "code.team", false, true},
		{"", "plan", "agent.work.plan", true, false},
		{"agent.plan", "", "agent.work.normal", false, false},
	}
	for _, c := range cases {
		got := ResolveRequestRuntimeMode(c.mode, c.subMode)
		require.Equal(t, c.want, got.CanonicalMode, "mode=%s sub=%s", c.mode, c.subMode)
		require.Equal(t, c.wantPlan, got.IsPlan)
		require.Equal(t, c.wantTeam, got.IsTeam)
	}
}

// TestShouldSync 验证只认 chat 类方法。
func TestShouldSync(t *testing.T) {
	require.True(t, ShouldSync(""))
	require.True(t, ShouldSync("chat.send"))
	require.False(t, ShouldSync("session.list"))
}
```

- [x] **Step 2: 运行确认失败**

Run: `CGO_ENABLED=1 go test -tags "sqlite_fts5 test" ./internal/swarm/runtime/ -run 'TestResolveRequestRuntimeMode|TestShouldSync' -v`
Expected: FAIL — `undefined: ResolveRequestRuntimeMode`

- [x] **Step 3: 写实现**

`doc.go`：

```go
// Package runtime 提供 host 侧运行时协调能力（plan 状态机、请求模式解析）。
//
// 文件目录：
//
//	runtime/
//	├── doc.go     # 包文档
//	├── request.go # 请求模式解析与 chat 方法判定
//	└── plan.go    # PlanModeController 计划状态机
//
// 对应 Python 代码：jiuwenswarm/runtime/
package runtime
```

`request.go`：

```go
package runtime

import (
	"strings"

	"github.com/uapclaw/uapclaw-go/internal/swarm/schema"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ResolvedMode 请求模式解析结果。
type ResolvedMode struct {
	// CanonicalMode 权威模式串
	CanonicalMode string
	// IsPlan 是否规划模式
	IsPlan bool
	// IsTeam 是否团队模式
	IsTeam bool
}

// ──────────────────────────── 常量 ────────────────────────────

const (
	// PlanEntrySourceSlashCommand TUI /plan 命令来源
	PlanEntrySourceSlashCommand = "slash_command"
	// PlanEntrySourcePlanToggle Web 计划开关来源
	PlanEntrySourcePlanToggle = "plan_toggle"
	// PlanEntrySourceKey 请求参数中的进入来源键
	PlanEntrySourceKey = "plan_entry_source"
	// PlanReminderOriginalQueryKey 激活提醒改写前原 query 的存放键
	PlanReminderOriginalQueryKey = "plan_reminder_original_query"
	// PlanModeExitedEvent 计划模式结束事件类型
	PlanModeExitedEvent = "plan_mode_exited"
	// PreviousSessionModeKey 上一次会话模式参数键
	PreviousSessionModeKey = "previous_session_mode"
)

// chatTurnMethods 参与 plan 状态同步的请求方法（空串表示默认 chat）。
var chatTurnMethods = map[string]struct{}{
	"":          {},
	"chat.send": {},
	"chat":      {},
}

// planEntrySources 显式进入计划模式的来源集合。
var planEntrySources = map[string]struct{}{
	PlanEntrySourceSlashCommand: {},
	PlanEntrySourcePlanToggle:   {},
}

// ──────────────────────────── 导出函数 ────────────────────────────

// ResolveRequestRuntimeMode 把请求 mode/subMode 解析为权威模式。
//
// Python: jiuwenswarm/runtime/request.py resolve_request_runtime_mode
func ResolveRequestRuntimeMode(mode, subMode string) ResolvedMode {
	raw := strings.TrimSpace(mode)
	if raw == "" && strings.TrimSpace(subMode) != "" {
		raw = "agent." + strings.TrimSpace(subMode)
	}
	canonical := string(schema.NormalizeMode(raw))
	return ResolvedMode{
		CanonicalMode: canonical,
		IsPlan:        strings.HasSuffix(canonical, ".plan"),
		IsTeam:        strings.HasSuffix(canonical, ".team"),
	}
}

// ShouldSync 判断请求是否参与 plan 状态同步。
//
// Python: PlanModeController.should_sync
func ShouldSync(reqMethod string) bool {
	_, ok := chatTurnMethods[strings.TrimSpace(reqMethod)]
	return ok
}

// IsPlanEntrySource 判断进入来源是否属于显式进入。
//
// Python: PlanModeController.is_explicit_entry
func IsPlanEntrySource(source string) bool {
	_, ok := planEntrySources[strings.TrimSpace(source)]
	return ok
}
```

- [x] **Step 4: 运行确认通过**

Run: `CGO_ENABLED=1 go test -tags "sqlite_fts5 test" ./internal/swarm/runtime/ -v`
Expected: PASS（2 例）

- [x] **Step 5: 提交**

```bash
git add internal/swarm/runtime/ && git commit -m "feat(runtime): 请求模式解析 ResolveRequestRuntimeMode 与 chat 方法判定"
```

---

### Task 3: `PlanModeController`

**Files:**
- Create: `internal/swarm/runtime/plan.go`
- Test: `internal/swarm/runtime/plan_test.go`

- [x] **Step 1: 写失败测试**

```go
package runtime

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

// fakeStore 内存实现 PlanStateStore。
type fakeStore struct {
	modes    map[string]string
	slugs    map[string]string
	switches []string
	commits  int
}

func newFakeStore() *fakeStore {
	return &fakeStore{modes: map[string]string{}, slugs: map[string]string{}}
}

func (f *fakeStore) LoadPlanState(_ context.Context, sid string) (PlanState, error) {
	return PlanState{Mode: f.modes[sid], PlanSlug: f.slugs[sid]}, nil
}
func (f *fakeStore) SavePlanState(_ context.Context, sid string, st PlanState) error {
	f.modes[sid], f.slugs[sid] = st.Mode, st.PlanSlug
	return nil
}
func (f *fakeStore) SwitchPlanMode(_ context.Context, sid, mode string) error {
	f.modes[sid] = mode
	f.switches = append(f.switches, mode)
	return nil
}
func (f *fakeStore) CommitSession(_ context.Context, _ string) error { f.commits++; return nil }

// TestEnsureState_非显式进入 验证普通 plan 请求不会被自动拉进 plan。
func TestEnsureState_非显式进入(t *testing.T) {
	store := newFakeStore()
	c := NewPlanModeController(store)
	res, err := c.EnsureState(context.Background(), PlanRequest{
		SessionID: "s1", Mode: "agent.work.plan", ReqMethod: "chat.send",
	})
	require.NoError(t, err)
	require.Equal(t, "normal", store.modes["s1"])
	require.False(t, res.Entered)
}

// TestEnsureState_显式进入并清退出记忆 验证 plan_toggle 可进入且重置记忆。
func TestEnsureState_显式进入并清退出记忆(t *testing.T) {
	store := newFakeStore()
	c := NewPlanModeController(store)
	c.ResetSession("s1")
	_, err := c.EnsureState(context.Background(), PlanRequest{
		SessionID: "s1", Mode: "agent.work.plan", ReqMethod: "chat.send", EntrySource: "plan_toggle",
	})
	require.NoError(t, err)
	require.Equal(t, "plan", store.modes["s1"])
	require.True(t, c.IsActive("s1"))
}

// TestEnsureState_退出记忆阻断 验证刚退出的会话不被拉回。
func TestEnsureState_退出记忆阻断(t *testing.T) {
	store := newFakeStore()
	c := NewPlanModeController(store)
	c.markExited("s1")
	res, err := c.EnsureState(context.Background(), PlanRequest{
		SessionID: "s1", Mode: "agent.work.plan", ReqMethod: "chat.send",
	})
	require.NoError(t, err)
	require.False(t, res.Entered)
	require.Len(t, res.Events, 1)
	require.Equal(t, PlanModeExitedEvent, res.Events[0].EventType)
}

// TestEnsureState_planSlug阻断 验证残留计划文件阻断并清 slug。
func TestEnsureState_planSlug阻断(t *testing.T) {
	store := newFakeStore()
	store.slugs["s1"] = "old-plan"
	c := NewPlanModeController(store)
	res, err := c.EnsureState(context.Background(), PlanRequest{
		SessionID: "s1", Mode: "agent.work.plan", ReqMethod: "chat.send",
	})
	require.NoError(t, err)
	require.Equal(t, "normal", store.modes["s1"])
	require.Empty(t, store.slugs["s1"], "阻断后必须清空 slug")
	require.Len(t, res.Events, 1)
}

// TestEnsureState_跳过矩阵 验证 team/非 chat 方法不写状态。
func TestEnsureState_跳过矩阵(t *testing.T) {
	for _, req := range []PlanRequest{
		{SessionID: "s1", Mode: "code.team", ReqMethod: "chat.send"},
		{SessionID: "s1", Mode: "agent.work.plan", ReqMethod: "session.list"},
		{SessionID: "s1", Mode: "agent.work.plan", ReqMethod: "chat.send", IsInterruptResume: true},
	} {
		store := newFakeStore()
		c := NewPlanModeController(store)
		_, err := c.EnsureState(context.Background(), req)
		require.NoError(t, err)
		require.Empty(t, store.switches, "请求 %+v 不应写状态", req)
	}
}

// TestCheckPostProcessExit 验证工具退出后发事件并记记忆。
func TestCheckPostProcessExit(t *testing.T) {
	store := newFakeStore()
	store.modes["s1"] = "normal"
	c := NewPlanModeController(store)
	c.markActive("s1")
	events, err := c.CheckPostProcessExit(context.Background(), PlanRequest{
		SessionID: "s1", Mode: "agent.work.plan", ReqMethod: "chat.send",
	})
	require.NoError(t, err)
	require.Len(t, events, 1)
	require.False(t, c.IsActive("s1"))
}

// TestSessionMessageRequiresQueue 占位：未实现时必须透传 false。
func TestSessionMessageRequiresQueue(t *testing.T) {
	c := NewPlanModeController(newFakeStore())
	c.markActive("s1")
	require.False(t, c.SessionMessageRequiresQueue("s1"))
}
```

- [x] **Step 2: 运行确认失败**

Run: `CGO_ENABLED=1 go test -tags "sqlite_fts5 test" ./internal/swarm/runtime/ -run TestEnsureState -v`
Expected: FAIL — `undefined: NewPlanModeController`

- [x] **Step 3: 写实现**

```go
package runtime

import (
	"context"
	"sync"
)

// ──────────────────────────── 结构体 ────────────────────────────

// PlanState plan 状态快照（只取控制器关心的字段）。
type PlanState struct {
	// Mode 当前 plan 模式："plan" | "normal"
	Mode string
	// PlanSlug 活跃计划文件短名
	PlanSlug string
}

// PlanRequest 控制器需要的请求视图。
type PlanRequest struct {
	SessionID string
	// Mode 请求 mode（权威串或旧串）
	Mode string
	// SubMode 二级模式
	SubMode string
	// ReqMethod 请求方法（空串视为 chat）
	ReqMethod string
	// EntrySource plan_entry_source
	EntrySource string
	// IsInterruptResume 是否中断续答请求
	IsInterruptResume bool
	// PreviousSessionMode previous_session_mode 参数
	PreviousSessionMode string
	// Query 本轮用户输入（激活提醒会就地改写）
	Query string
}

// ControlEvent 控制事件（下发通道/前端）。
type ControlEvent struct {
	// EventType 事件类型
	EventType string
	// Mode 事件携带的权威模式
	Mode string
}

// PlanStateResult 一次状态同步的结果。
type PlanStateResult struct {
	// Entered 本轮是否进入了 plan
	Entered bool
	// Restored 是否从 plan 退回 normal
	Restored bool
	// Events 需要下发的控制事件
	Events []ControlEvent
	// Query 改写后的 query（未改写时等于入参）
	Query string
}

// PlanStateStore plan 状态读写（由 adapter 适配 DeepAgent 状态机）。
type PlanStateStore interface {
	// LoadPlanState 读取会话 plan 状态
	LoadPlanState(ctx context.Context, sessionID string) (PlanState, error)
	// SavePlanState 保存会话 plan 状态
	SavePlanState(ctx context.Context, sessionID string, st PlanState) error
	// SwitchPlanMode 切 plan 模式
	SwitchPlanMode(ctx context.Context, sessionID, mode string) error
	// CommitSession 提交会话
	CommitSession(ctx context.Context, sessionID string) error
}

// PlanModeController 计划模式状态机守门人。
//
// Python: jiuwenswarm/runtime/plan.py PlanModeController
type PlanModeController struct {
	store  PlanStateStore
	mu     sync.Mutex
	locks  map[string]*sync.Mutex
	active map[string]struct{}
	exited map[string]struct{}
}

// ──────────────────────────── 导出函数 ────────────────────────────

// NewPlanModeController 创建控制器。
func NewPlanModeController(store PlanStateStore) *PlanModeController {
	return &PlanModeController{
		store:  store,
		locks:  map[string]*sync.Mutex{},
		active: map[string]struct{}{},
		exited: map[string]struct{}{},
	}
}

// EnsureState 每轮执行前同步 plan 状态。
//
// Python: PlanModeController.ensure_state
func (c *PlanModeController) EnsureState(ctx context.Context, req PlanRequest) (PlanStateResult, error) {
	res := PlanStateResult{Query: req.Query}
	resolved := ResolveRequestRuntimeMode(req.Mode, req.SubMode)
	if resolved.IsTeam || req.SessionID == "" || req.IsInterruptResume || !ShouldSync(req.ReqMethod) {
		return res, nil
	}
	if !strings.HasPrefix(resolved.CanonicalMode, "agent.") && !strings.HasPrefix(resolved.CanonicalMode, "code.") {
		return res, nil
	}

	lock := c.LockFor(req.SessionID)
	lock.Lock()
	defer lock.Unlock()

	st, err := c.store.LoadPlanState(ctx, req.SessionID)
	if err != nil {
		return res, err
	}
	target := "normal"
	if resolved.IsPlan {
		target = "plan"
	}
	if st.Mode == target {
		if target == "plan" {
			c.markActive(req.SessionID)
		}
		return res, nil
	}

	if target == "plan" {
		if IsPlanEntrySource(req.EntrySource) {
			c.clearExited(req.SessionID)
		} else if c.isExited(req.SessionID) {
			return c.blockEntry(ctx, req.SessionID, resolved, res)
		} else if st.PlanSlug != "" {
			return c.blockEntry(ctx, req.SessionID, resolved, res)
		}
		if err := c.store.SwitchPlanMode(ctx, req.SessionID, "plan"); err != nil {
			return res, err
		}
		st.Mode, st.PlanSlug = "plan", ""
		if err := c.store.SavePlanState(ctx, req.SessionID, st); err != nil {
			return res, err
		}
		c.markActive(req.SessionID)
		res.Entered = true
		res.Query = InjectActivationReminder(req.Query)
	} else {
		if err := c.store.SwitchPlanMode(ctx, req.SessionID, "normal"); err != nil {
			return res, err
		}
		c.clearActive(req.SessionID)
		res.Restored = true
		res.Events = append(res.Events, ControlEvent{EventType: PlanModeExitedEvent, Mode: resolved.CanonicalMode})
	}
	if err := c.store.CommitSession(ctx, req.SessionID); err != nil {
		return res, err
	}
	return res, nil
}

// CheckPostProcessExit 执行后检测工具触发的退出。
//
// Python: PlanModeController.check_post_process_exit
func (c *PlanModeController) CheckPostProcessExit(ctx context.Context, req PlanRequest) ([]ControlEvent, error) {
	resolved := ResolveRequestRuntimeMode(req.Mode, req.SubMode)
	if req.SessionID == "" || resolved.IsTeam || !resolved.IsPlan {
		return nil, nil
	}
	st, err := c.store.LoadPlanState(ctx, req.SessionID)
	if err != nil {
		return nil, err
	}
	if st.Mode != "normal" {
		return nil, nil
	}
	c.markExited(req.SessionID)
	c.clearActive(req.SessionID)
	return []ControlEvent{{EventType: PlanModeExitedEvent, Mode: resolved.CanonicalMode}}, nil
}

// ResetSession 删除会话时清理记忆。
//
// Python: PlanModeController.reset_session
func (c *PlanModeController) ResetSession(sessionID string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.exited, sessionID)
	delete(c.active, sessionID)
	delete(c.locks, sessionID)
}

// LockFor 返回会话级锁。
//
// Python: PlanModeController.lock_for
func (c *PlanModeController) LockFor(sessionID string) *sync.Mutex {
	c.mu.Lock()
	defer c.mu.Unlock()
	l, ok := c.locks[sessionID]
	if !ok {
		l = &sync.Mutex{}
		c.locks[sessionID] = l
	}
	return l
}

// IsActive 会话是否处于 plan。
func (c *PlanModeController) IsActive(sessionID string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	_, ok := c.active[sessionID]
	return ok
}

// SessionMessageRequiresQueue 判断会话是否要求跨会话消息排队。
//
// 上游 runtime/plan.py:252-261 的步骤（Go 侧尚无 cross-session 消息链路，暂不实现）：
//  1. 读取该会话 plan 状态：state.plan_mode.mode == "plan" → is_plan = true
//  2. 读取该会话 goal 状态：goal 活跃 → is_goal_active = true
//  3. is_plan || is_goal_active → 跨会话消息必须排队，不得打断当前计划
//  4. 否则直接透传（不需要排队）
//
// TODO(跨会话消息链路): runtime 的 session_message 通道落地后按上述 4 步复刻。
func (c *PlanModeController) SessionMessageRequiresQueue(sessionID string) bool {
	logger.Warn(logComponent).
		Str("event_type", "plan_cross_session_queue_unimplemented").
		Str("session_id", sessionID).
		Msg("cross-session message queueing is not implemented; passing through")
	return false
}

// InjectActivationReminder 给本轮 query 前置 plan 约束提醒。
//
// Python: PlanModeController.inject_activation_reminder
func InjectActivationReminder(query string) string {
	return planActivationReminder + query
}
```

配套（同文件非导出部分与常量）：

```go
const (
	// planActivationReminder 进入 plan 的约束提醒（逐字对齐上游）
	planActivationReminder = "\n\n<system-reminder>\n" +
		"Plan mode is active. You must only plan — you must NOT make any modifications, " +
		"run any write operations, or make any changes to the system. This constraint takes " +
		"priority over any other instructions.\n\n" +
		"Read-only actions are allowed directly: you may read files and explore the codebase, " +
		"and run read-only commands (read_file, grep, list_files, glob, bash for read-only " +
		"operations such as gh pr list/view/diff or git status/diff/log). Write operations and " +
		"non-read-only tools are blocked.\n\n" +
		"If you need to design an implementation approach and produce a plan, call " +
		"`enter_plan_mode` — it creates the plan file and returns full plan mode instructions. " +
		"This is not required as your first action; you may gather context with read-only tools " +
		"first. Do NOT proceed to implement anything until the user approves your plan via " +
		"`exit_plan_mode`.\n" +
		"</system-reminder>"
)

// blockEntry 阻断进入：清 slug、强制 normal、发退出事件。
func (c *PlanModeController) blockEntry(ctx context.Context, sessionID string, resolved ResolvedMode, res PlanStateResult) (PlanStateResult, error) {
	st, err := c.store.LoadPlanState(ctx, sessionID)
	if err != nil {
		return res, err
	}
	st.PlanSlug = ""
	if err := c.store.SavePlanState(ctx, sessionID, st); err != nil {
		return res, err
	}
	if err := c.store.SwitchPlanMode(ctx, sessionID, "normal"); err != nil {
		return res, err
	}
	if err := c.store.CommitSession(ctx, sessionID); err != nil {
		return res, err
	}
	res.Entered = false
	res.Events = append(res.Events, ControlEvent{EventType: PlanModeExitedEvent, Mode: resolved.CanonicalMode})
	return res, nil
}

func (c *PlanModeController) markActive(sessionID string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.active[sessionID] = struct{}{}
}

func (c *PlanModeController) clearActive(sessionID string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.active, sessionID)
}

func (c *PlanModeController) markExited(sessionID string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.exited[sessionID] = struct{}{}
}

func (c *PlanModeController) clearExited(sessionID string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.exited, sessionID)
}

func (c *PlanModeController) isExited(sessionID string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	_, ok := c.exited[sessionID]
	return ok
}
```

需要 import `strings` 与 `logger`（`internal/common/logger`，包级 `const logComponent = logger.ComponentGateway`）。

- [x] **Step 4: 运行确认通过（含 -race）**

Run: `CGO_ENABLED=1 go test -tags "sqlite_fts5 test" -race ./internal/swarm/runtime/ -v`
Expected: PASS（8 例）

- [x] **Step 5: 提交**

```bash
git add internal/swarm/runtime/ && git commit -m "feat(runtime): PlanModeController 计划状态机（守卫/阻断/退出记忆/激活提醒）"
```

---

### Task 4: 接入 adapter 并写 IT-S2-3~7/13

**Files:**
- Modify: `internal/swarm/server/adapter/deep_adapter_rails.go`（`updateRailsForMode` 内两行）
- Modify: `internal/swarm/server/adapter/deep_adapter.go`（新增 `planStateStore` 适配器类型）
- Test: `tests/integration/swarm/server/adapter/attachment_mode/plan_state_test.go`

- [x] **Step 1: 写实现（状态存储适配器）**

```go
// planStateStore 把 DeepAdapter 的会话状态机适配为 runtime.PlanStateStore。
type planStateStore struct{ d *DeepAdapter }

// LoadPlanState 读取会话 plan 状态
func (s *planStateStore) LoadPlanState(ctx context.Context, sessionID string) (swruntime.PlanState, error) {
	deep, sess, err := s.d.openStateSession(ctx, sessionID)
	if err != nil || deep == nil || sess == nil {
		return swruntime.PlanState{}, err
	}
	st := deep.LoadState(sess)
	return swruntime.PlanState{Mode: st.PlanMode.Mode, PlanSlug: st.PlanMode.PlanSlug}, nil
}

// SavePlanState 保存会话 plan 状态
func (s *planStateStore) SavePlanState(ctx context.Context, sessionID string, ps swruntime.PlanState) error {
	deep, sess, err := s.d.openStateSession(ctx, sessionID)
	if err != nil || deep == nil || sess == nil {
		return err
	}
	st := deep.LoadState(sess)
	st.PlanMode.Mode, st.PlanMode.PlanSlug = ps.Mode, ps.PlanSlug
	deep.SaveState(sess, st)
	return nil
}

// SwitchPlanMode 切换 plan 模式
func (s *planStateStore) SwitchPlanMode(ctx context.Context, sessionID, mode string) error {
	return s.d.SwitchMode(ctx, sessionID, mode)
}

// CommitSession 提交会话
func (s *planStateStore) CommitSession(ctx context.Context, sessionID string) error {
	deep, sess, err := s.d.openStateSession(ctx, sessionID)
	if err != nil || deep == nil || sess == nil {
		return err
	}
	return sess.Commit(ctx)
}
```

`openStateSession` 需新增：优先取 live 会话（对齐上游 `open_state_session`），否则 `ensureInstance` + 新建会话；实现时复用 `d.SessionFacade(sessionID)`（若无则按 `deep_adapter.go` 既有会话获取流程封装）。

- [x] **Step 2: 替换 `updateRailsForMode` 里的两行**

```go
// updateRailsForMode 按模式装配 rail（状态写入交给 PlanModeController）。
func (d *DeepAdapter) updateRailsForMode(ctx context.Context, mode, sessionID string) {
	d.UpdateAgentRails(ctx, sessionID, mode)
}
```

并在请求入口（`handle_envelope.go` 里 chat 分支）调用：

```go
res, err := d.PlanController().EnsureState(ctx, swruntime.PlanRequest{
	SessionID:   sessionID,
	Mode:        params.Mode,
	SubMode:     params.SubMode,
	ReqMethod:   request.ReqMethod,
	EntrySource: params.PlanEntrySource,
	Query:       params.Query,
})
// 改写后的 query 回写请求；控制事件下发
```

执行结束后追加 `CheckPostProcessExit`；会话删除处调用 `ResetSession`。

- [x] **Step 3: 写 IT-S2-3~7/13**

```go
//go:build integration

package attachment_mode

import (
	"context"
	"testing"

	"github.com/stretchr/testify/suite"
)

// PlanStateSuite 覆盖 plan 状态机端到端行为。
type PlanStateSuite struct{ suite.Suite }

// TestPlanStateSuite 运行 PlanStateSuite。
func TestPlanStateSuite(t *testing.T) { suite.Run(t, new(PlanStateSuite)) }

// TestIT_S2_3_显式进入改写query 验证 plan_toggle 进入并前置提醒。
func (s *PlanStateSuite) TestIT_S2_3_显式进入改写query() {
	env := newPlanEnv(s.T())            // 复用 adapter 构造 + mockllm
	env.PreExit("sess-1")               // 预置退出记忆
	env.SendPlanRequest("sess-1", "帮我重构", "plan_toggle")

	s.Require().Equal("plan", env.PlanMode("sess-1"))
	s.Require().True(env.ModelSawQueryPrefix("<system-reminder>"), "模型收到的 query 应被前置提醒")
	s.Require().Equal("帮我重构", env.OriginalQuery("sess-1"))
}

// TestIT_S2_4_非显式被退出记忆阻断
func (s *PlanStateSuite) TestIT_S2_4_非显式被退出记忆阻断() {
	env := newPlanEnv(s.T())
	env.PreExit("sess-1")
	env.SendPlanRequest("sess-1", "继续", "")

	s.Require().Equal("normal", env.PlanMode("sess-1"))
	s.Require().True(env.HasControlEvent("plan_mode_exited"))
}

// TestIT_S2_5_planSlug残留阻断
func (s *PlanStateSuite) TestIT_S2_5_planSlug残留阻断() {
	env := newPlanEnv(s.T())
	env.PreSeedSlug("sess-1", "old-plan")
	env.SendPlanRequest("sess-1", "继续", "")

	s.Require().Equal("normal", env.PlanMode("sess-1"))
	s.Require().Empty(env.PlanSlug("sess-1"))
	s.Require().True(env.HasControlEvent("plan_mode_exited"))
}

// TestIT_S2_6_退出后不被拉回
func (s *PlanStateSuite) TestIT_S2_6_退出后不被拉回() {
	env := newPlanEnv(s.T())
	env.SendPlanRequest("sess-1", "计划一下", "plan_toggle")
	env.SendPlanRequest("sess-1", "退出", "")
	env.SendPlanRequest("sess-1", "继续", "")

	s.Require().Equal("normal", env.PlanMode("sess-1"))
}

// TestIT_S2_7_跳过矩阵
func (s *PlanStateSuite) TestIT_S2_7_跳过矩阵() {
	env := newPlanEnv(s.T())
	env.SendPlanRequest("sess-1", "team", "plan_toggle")     // team 模式
	env.SendNonChatMethod("sess-1", "session.list")
	s.Require().Equal("", env.PlanMode("sess-1"))
	s.Require().False(env.HasControlEvent("plan_mode_exited"))
}

// TestIT_S2_13_跨会话排队占位透传
func (s *PlanStateSuite) TestIT_S2_13_跨会话排队占位透传() {
	env := newPlanEnv(s.T())
	s.Require().False(env.Controller().SessionMessageRequiresQueue("sess-1"))
}
```

- [x] **Step 3.5: 写测试助手 `plan_env_test.go`**

操作（不臆造构造参数，直接复制既有样板）：

1. 打开 `tests/integration/swarm/server/adapter/deep_adapter_rail_registration_test.go`，把其中"建 env → 建 DeepAdapter → 注入 mockllm"的构造函数整体复制到新文件 `plan_env_test.go`（`//go:build integration`），改名为 `newPlanEnv`
2. 打开 `tests/integration/swarm/server/adapter/code_adapter_coding_memory_test.go`，同样复制一份改名为 `newCodeEnv`（IT-S2-11 用）
3. 在同一文件补以下查询方法（全部基于已构造好的 adapter 与会话对象实现，不引入新抽象）：

| 方法 | 实现要点 |
| --- | --- |
| `SendPlanRequest(sid, query, entrySource)` | 构造 `schema.AgentRequest{ReqMethod:"chat.send"}`，params 写 `mode/plan_entry_source/query`，走 adapter 请求入口 |
| `SendNonChatMethod(sid, method)` | 同上但 `ReqMethod=method` |
| `PreExit(sid)` / `PreSeedSlug(sid, slug)` | 直接调用控制器/状态存储预置（`markExited` 走导出的 `ResetSession` 不行时，用两次请求制造该状态） |
| `PlanMode(sid)` | 读 `deep.LoadState(sess).PlanMode.Mode` |
| `PlanSlug(sid)` | 读 `deep.LoadState(sess).PlanMode.PlanSlug` |
| `HasControlEvent(t)` | 从事件采集切片里查 `event_type` |
| `ModelSawQueryPrefix(p)` | 从 mockllm 记录的请求 messages 里取最后一条 user content，判断前缀 |
| `OriginalQuery(sid)` | 读请求 params 的 `plan_reminder_original_query` |
| `HistoryHasAttachment(section)` / `SystemPromptHasSection(name)` / `ToolVisible(name)` | 分别查会话历史附件、mockllm 记录的 system 文本、工具列表 |
| `CountRailsByType(t)` | 遍历 `instance.Rails()` 按类型名计数 |
| `HistoryHasAttachmentFrom(section, source, kind)` | 查附件对象的 source/kind |
| `LastAttachmentIsRemoval(section)` | 最后一条附件消息内容含移除标记 |

> `newPlanEnv` 是测试包内的构造助手，放在 `plan_env_test.go`：用 `testhelpers.NewAdapterTestEnv` + `mockllm` 建 DeepAdapter，暴露 `SendPlanRequest/PlanMode/ModelSawQueryPrefix/HasControlEvent/Controller` 等方法。**必须放 `_test.go` 文件**（审计要求目录内非 doc.go 文件都调用 `suite.Run`）。

- [x] **Step 4: 运行**

Run: `CGO_ENABLED=1 go test -tags "sqlite_fts5 test integration" ./tests/integration/swarm/server/adapter/attachment_mode/ -run TestPlanStateSuite -v`
Expected: PASS（6 例）

- [x] **Step 5: 提交**

```bash
git add internal/swarm/server/adapter/ tests/integration/swarm/server/adapter/attachment_mode/ && git commit -m "feat(adapter): 接入 PlanModeController（状态守卫/阻断/事件）+ IT-S2-3~7"
```

---

### Task 5: rail 装配合并为 `UpdateAgentRails`（含 IT-S2-8）

**Files:**
- Modify: `internal/swarm/server/adapter/deep_adapter_rails.go`
- Test: `tests/integration/swarm/server/adapter/attachment_mode/rail_idempotent_test.go`

- [x] **Step 1: 写实现（搬运式合并）**

按"目标集合"实现幂等补挂，逐块从现有两个函数搬运（保持逻辑不变）：

```go
// UpdateAgentRails 幂等补挂 agent 模式 rail：缺的补、已有的不动、normal 期间卸载 plan 专属项。
// 对齐上游 interface_deep.py:3290-3320 `_update_agent_rails`。
func (d *DeepAdapter) UpdateAgentRails(ctx context.Context, sessionID, mode string) {
	isPlan := strings.HasSuffix(mode, ".plan")
	d.ensureTaskPlanningRail(ctx)                              // ← 原 updatePlanModeRails 第 1 段
	d.syncMultiSessionTools(ctx, isPlan)                       // ← 原 updatePlanModeRails 第 2 段的幂等版
	d.handleMemoryRailByConfig(ctx, mode)                      // ← 原两函数第 3 段
	d.handleExternalMemoryRailByConfig(ctx)                    // ← 原两函数第 4 段
	d.ensureContextAssembleRail(ctx, mode)                     // ← 原 updatePlanModeRails 第 5 段
	d.ensureAgentModeRail(ctx, mode)                           // ← 原 updateAgentModeRails 里的 AgentModeRail 分支
}
```

- `ensureTaskPlanningRail`：`if d.taskPlanningRail == nil { ... }`（现有逻辑原样搬）
- `syncMultiSessionTools`：`isPlan` 时移除 `session_*` 工具；否则 `ensure` 回来（原 `updateAgentModeRails` 里的注册分支）
- `ensureContextAssembleRail`：`if d.contextAssembleMode != mode` 才换实例（现有逻辑原样搬）
- 删除 `updatePlanModeRails`、`switchModeForState` 及其单测/引用

- [x] **Step 2: 写 IT-S2-8**

```go
// TestIT_S2_8_rail幂等 验证多次切换 plan 不重复挂 rail。
func (s *PlanStateSuite) TestIT_S2_8_rail幂等() {
	env := newPlanEnv(s.T())
	for i := 0; i < 3; i++ {
		env.SendPlanRequest("sess-1", "计划", "plan_toggle")
		s.Require().Equal(1, env.CountRailsByType("TaskPlanningRail"), "第 %d 次后 TaskPlanningRail 应恒为 1", i+1)
	}
	env.SendPlanRequest("sess-1", "退出", "")
	s.Require().Equal(0, env.CountRailsByType("PlanOnlyRail"), "normal 期间 plan 专属 rail 应卸载")
	env.SendPlanRequest("sess-1", "再进", "plan_toggle")
	s.Require().Equal(1, env.CountRailsByType("PlanOnlyRail"))
}
```

- [x] **Step 3: 运行**

Run: `CGO_ENABLED=1 go test -tags "sqlite_fts5 test integration" ./tests/integration/swarm/server/adapter/attachment_mode/ -run TestIT_S2_8 -v`
Expected: PASS

- [x] **Step 4: 回归既有 adapter 用例**

Run: `CGO_ENABLED=1 go test -tags "sqlite_fts5 test integration" ./tests/integration/swarm/server/adapter/ -run 'TestDeepAdapter|TestCodeAdapter' 2>&1 | tail -5`
Expected: 无 FAIL

- [x] **Step 5: 提交**

```bash
git add internal/swarm/server/adapter/ tests/integration/ && git commit -m "refactor(adapter): 合并 plan/agent rail 装配为幂等 UpdateAgentRails（删 updatePlanModeRails/switchModeForState）"
```

---

### Task 6: AgentModeRail 改 attachment（含 IT-S2-9）

**Files:**
- Modify: `internal/agentcore/harness/rails/agent_mode.go`
- Test: `internal/agentcore/harness/rails/agent_mode_test.go`、`tests/integration/swarm/server/adapter/attachment_mode/plan_attachment_test.go`

- [x] **Step 1: 改实现**

```go
// syncModeAttachment 把 plan 指引投递为动态提示词附件。
//
// Python: AgentModeRail._sync_mode_attachment（agent_mode_rail.py:406-420）
func (r *AgentModeRail) syncModeAttachment(ctx context.Context, section *saprompt.PromptSection, manager *hprompts.Manager, sessionID string) {
	if manager == nil {
		r.systemPromptBuilder.AddSection(*section) // 无 manager 时回落 section
		return
	}
	if _, err := manager.AddFromPromptSection(sessionID, section, hprompts.AttachmentRuntime,
		"agent_core.agent_mode_rail", r.systemPromptBuilder.Language(),
		hprompts.WithAttachmentContentKind("text/markdown")); err != nil {
		logger.Warn(logComponent).Err(err).Msg("agent_mode attachment failed, fallback to section")
		r.systemPromptBuilder.AddSection(*section)
	}
}
```

- 删除 `r.systemPromptBuilder.RemoveSection(SectionNameModeInstructions)` 调用
- 退出 plan 分支改为 `manager.ClearSection(sessionID, "mode_instructions")`

- [x] **Step 2: 写 IT-S2-9**

```go
// TestIT_S2_9_plan指引走附件 验证 plan 指引进历史而非 system prompt。
func (s *PlanStateSuite) TestIT_S2_9_plan指引走附件() {
	env := newPlanEnv(s.T())
	env.SendPlanRequest("sess-1", "计划", "plan_toggle")

	s.Require().True(env.HistoryHasAttachment("mode_instructions"), "历史中应有 plan 附件")
	s.Require().False(env.SystemPromptHasSection("mode_instructions"), "system prompt 不应再有该节")

	env.SendPlanRequest("sess-1", "退出", "")
	s.Require().True(env.LastAttachmentIsRemoval("mode_instructions"), "退出应产生移除 delta")
}
```

- [x] **Step 3: 运行**

Run: `CGO_ENABLED=1 go test -tags "sqlite_fts5 test" ./internal/agentcore/harness/rails/ -run TestAgentMode && CGO_ENABLED=1 go test -tags "sqlite_fts5 test integration" ./tests/integration/swarm/server/adapter/attachment_mode/ -run TestIT_S2_9 -v`
Expected: PASS

- [x] **Step 4: 提交**

```bash
git add internal/agentcore/harness/rails/ tests/integration/ && git commit -m "feat(rails): AgentModeRail 的 plan 指引改 attachment 投递（无 manager 回落 section）"
```

---

### Task 7: todo 开关默认关闭（含 IT-S2-10）

**Files:**
- Modify: `internal/agentcore/harness/rails/task_planning.go`
- Test: `internal/agentcore/harness/rails/task_planning_test.go`、`tests/integration/swarm/server/adapter/attachment_mode/todo_inject_test.go`

- [x] **Step 1: 改实现**

```go
// TaskPlanningRailOptions 装配选项
type TaskPlanningRailOptions struct {
	// InjectPrompt 是否注入常驻 todo 提示节（默认 false，对齐上游 inject_prompt=False）
	InjectPrompt bool
}
```

`BeforeModelCall` 里注入节的分支改为：

```go
	if r.options.InjectPrompt {
		r.systemPromptBuilder.AddSection(buildTodoSection(language))
	}
```

删除 `BuildTodoSection` 导出函数与其单测（上游 code 模式同样不使用静态节）。

- [x] **Step 2: 写 IT-S2-10**

```go
// TestIT_S2_10_todo默认不注入 验证默认不写 todo 节但工具仍可见。
func (s *PlanStateSuite) TestIT_S2_10_todo默认不注入() {
	env := newPlanEnv(s.T())
	env.SendPlanRequest("sess-1", "普通对话", "")

	s.Require().False(env.SystemPromptHasSection("todo"), "system prompt 不应有 todo 节")
	for _, name := range []string{"todo_create", "todo_list", "todo_get", "todo_modify"} {
		s.Require().True(env.ToolVisible(name), "工具 %s 应仍可见", name)
	}
}
```

- [x] **Step 3: 运行**

Run: `CGO_ENABLED=1 go test -tags "sqlite_fts5 test" ./internal/agentcore/harness/rails/ -run TestTaskPlanning && CGO_ENABLED=1 go test -tags "sqlite_fts5 test integration" ./tests/integration/swarm/server/adapter/attachment_mode/ -run TestIT_S2_10 -v`
Expected: PASS

- [x] **Step 4: 提交**

```bash
git add internal/agentcore/harness/rails/ tests/integration/ && git commit -m "feat(rails): TaskPlanningRail 增加 InjectPrompt 开关（默认 false），删除静态 todo 节"
```

---

### Task 8: `CodeTaskPlanningRail` + code 工具文案（含 IT-S2-11）

**Files:**
- Create: `internal/swarm/agents/harness/code/rails/doc.go`
- Create: `internal/swarm/agents/harness/code/rails/code_task_planning_rail.go`
- Create: `internal/swarm/agents/harness/code/tools/code_todo_tools.go`
- Test: `internal/swarm/agents/harness/code/rails/code_task_planning_rail_test.go`、`tests/integration/swarm/server/adapter/attachment_mode/code_todo_reminder_test.go`

- [x] **Step 1: 写 rails 实现**

```go
package rails

const (
	// taskReminderTurnsSinceManagement 连续未管理 todo 的轮次阈值
	taskReminderTurnsSinceManagement = 10
	// taskReminderTurnsBetweenReminders 两次提醒的最小间隔轮次
	taskReminderTurnsBetweenReminders = 10
	// maxTrackedTaskReminderSessions 提醒状态跟踪上限
	maxTrackedTaskReminderSessions = 1000
	// taskReminderSection 提醒附件 section 名
	taskReminderSection = "task_reminder"
	// taskReminderSource 提醒附件来源
	taskReminderSource = "jiuwenswarm.code_task_planning_rail"
)

// CodeTaskPlanningRail code 模式 todo rail：跳过静态节，按节奏补提醒附件。
//
// Python: jiuwenswarm/agents/harness/code/rails/code_task_planning_rail.py
type CodeTaskPlanningRail struct {
	*harnessrails.TaskPlanningRail
	mu       sync.Mutex
	states   map[string]*reminderState
	order    []string
	manager  *hprompts.Manager
}
```

`BeforeModelCall`：读取会话状态 → 若 `turnsSinceManagement >= 10 && turnsSinceReminder >= 10` → `manager.AddSection(section=task_reminder, kind=todo_reminder, source=...)`；`OnUserMessage` 先调用 `restoreTaskReminderAttachment`；`turnsSinceManagement` 在检测到 todo 工具调用后重置（在 `AfterToolCall` 或工具结果钩子里）。

> 实现时按上游文件逐段搬运：`_restore_task_reminder_attachment`、`_task_reminder_content`、`_maybe_append_task_reminder`、`_record_tool_usage`；LRU 用 `order []string` + `map` 实现。

- [x] **Step 2: 写 IT-S2-11**

```go
// TestIT_S2_11_code提醒节奏 验证 10/10 节奏与工具调用后重置。
func (s *PlanStateSuite) TestIT_S2_11_code提醒节奏() {
	env := newCodeEnv(s.T()) // CodeAdapter + mockllm
	for i := 0; i < 12; i++ {
		env.SendCodeRequest("sess-c", fmt.Sprintf("第%d轮", i+1))
	}
	s.Require().True(env.HistoryHasAttachment("task_reminder"), "第 11 轮后应出现提醒附件")
	count := env.CountAttachment("task_reminder")
	s.Require().Equal(1, count, "内容未变不应重复追加")

	env.SendTodoToolCall("sess-c") // 模拟一次 todo_create
	for i := 0; i < 12; i++ {
		env.SendCodeRequest("sess-c", fmt.Sprintf("重置后%d", i+1))
	}
	s.Require().GreaterOrEqual(t, env.CountAttachment("task_reminder"), 2, "重置后应再次提醒")
}
```

- [x] **Step 3: 运行**

Run: `CGO_ENABLED=1 go test -tags "sqlite_fts5 test" ./internal/swarm/agents/harness/code/rails/ && CGO_ENABLED=1 go test -tags "sqlite_fts5 test integration" ./tests/integration/swarm/server/adapter/attachment_mode/ -run TestIT_S2_11 -v`
Expected: PASS

- [x] **Step 4: 提交**

```bash
git add internal/swarm/agents/harness/code/ tests/integration/ && git commit -m "feat(code): CodeTaskPlanningRail（10/10 提醒 + LRU）与 code 版 todo 工具文案"
```

---

### Task 9: 其余 5 组消费方迁移（含 IT-S2-12）

**Files:**
- Modify: `rails/task_completion.go`、`rails/progressive.go`、`rails/skills/skill_use_rail.go`、`rails/memory/{memory,coding_memory,external_memory}_rail.go`、`rails/context_engineer/context_assemble_rail.go`
- Test: `tests/integration/swarm/server/adapter/attachment_mode/consumer_migration_test.go`

- [x] **Step 1: 逐个改投递**

统一模式（以 task_completion 为例）：

```go
	writer := r.attachmentManager
	if writer == nil {
		r.systemPromptBuilder.AddSection(*section)
		return
	}
	if _, err := writer.AddFromPromptSection(sessionID, section, hprompts.AttachmentRuntime,
		"agent_core.task_completion_rail", language,
		hprompts.WithAttachmentContentKind("text/markdown")); err != nil {
		logger.Warn(logComponent).Err(err).Msg("attachment failed, fallback to section")
		r.systemPromptBuilder.AddSection(*section)
	}
```

各 rail 的 `kind`/`source`：

| rail | kind | source |
| --- | --- | --- |
| task_completion | runtime | `agent_core.task_completion_rail` |
| progressive | tool | `agent_core.progressive_tool_rail` |
| skills/skill_use | skill | `agent_core.skill_use_rail` |
| memory/memory | memory | `agent_core.jiuwen_memory_rail` |
| memory/coding_memory | memory | `agent_core.coding_memory_rail` |
| memory/external_memory | memory | `agent_core.external_memory_rail` |
| context_engineer/context_assemble | runtime | `agent_core.context_assemble_rail` |

- [x] **Step 2: 写 IT-S2-12**

```go
// TestIT_S2_12_消费方迁移 验证 5 组 rail 的附件来源与类型。
func (s *PlanStateSuite) TestIT_S2_12_消费方迁移() {
	env := newPlanEnv(s.T())
	env.EnableGoal()      // task_completion
	env.EnableProgressive()
	env.EnableSkills()
	env.EnableMemory()
	env.SendPlanRequest("sess-1", "干活", "")

	for _, want := range []struct{ section, source, kind string }{
		{"goal_protocol", "agent_core.task_completion_rail", "runtime"},
		{"tool_rules", "agent_core.progressive_tool_rail", "tool"},
		{"skills", "agent_core.skill_use_rail", "skill"},
		{"memory", "agent_core.jiuwen_memory_rail", "memory"},
	} {
		s.Require().True(env.HistoryHasAttachmentFrom(want.section, want.source, want.kind),
			"%s 应作为附件投递", want.section)
		s.Require().False(env.SystemPromptHasSection(want.section))
	}
}
```

- [x] **Step 3: 运行**

Run: `CGO_ENABLED=1 go test -tags "sqlite_fts5 test" ./internal/agentcore/harness/rails/... && CGO_ENABLED=1 go test -tags "sqlite_fts5 test integration" ./tests/integration/swarm/server/adapter/attachment_mode/ -run TestIT_S2_12 -v`
Expected: PASS

- [x] **Step 4: 提交**

```bash
git add internal/agentcore/harness/rails/ tests/integration/ && git commit -m "feat(rails): 其余 5 组动态提示词改 attachment 投递（source/kind 对齐上游）"
```

---

### Task 10: 清理、文档与全量验收

- [x] **Step 1: 清理残留符号**

```bash
grep -rn 'updatePlanModeRails\|switchModeForState\|BuildTodoSection\|ModeAgentFast\|ModeAgentPlan' --include=*.go internal tests | head
grep -rn '"agent\.plan"\|"agent\.fast"' --include=*.go internal | head
```

两条都必须为空（负向测试里引用旧串时写成拼接，避开字面量：`"agent." + "plan"`）。

- [x] **Step 2: 删除 S1 遗留未用字段**

删除 `prompts/attachment_types.go` 的 `ContentPath` 字段与 `WithAttachmentContentPath`（无调用方），同步删测试引用。

- [x] **Step 3: 更新文档**

- `internal/swarm/runtime/doc.go` 文件树、`internal/swarm/agents/harness/code/rails/doc.go`、`tools/doc.go`
- `IMPLEMENTATION_PLAN.md` 领域十五新增行：`15.x ✅ Rail 生命周期与模式合并（S2）`
- `docs/sync/defect-plan-2026-11-04.md` A 组更新对应行为 ✅

- [x] **Step 4: 全量验收**

```bash
CGO_ENABLED=1 go build -tags sqlite_fts5 ./...
CGO_ENABLED=1 go test -tags "sqlite_fts5 test" -race ./internal/swarm/... ./internal/agentcore/harness/...
CGO_ENABLED=1 go test -tags "sqlite_fts5 test integration" -race ./tests/integration/swarm/server/adapter/attachment_mode/ ./tests/integration/agentcore/attachment/
go run ./cmd/integration-audit -verify
```

Expected：全绿；audit 对新建目录判定通过（跨度 ≥3）

- [x] **Step 5: 提交并推送，盯 CI 四 job**

```bash
git add -A && git commit -m "chore(s2): 清理旧模式/rail 装配残留，登记计划与缺陷台账"
git push origin main
gh run list --limit 1
```

## 自审记录

- **Spec 覆盖**：§5.1→T1；§5.2→T2/T3/T4；§5.3→T5；§5.4→T7/T8；§5.5→T6/T9；§5.6→T10；§6 命名→T2/T3 代码；§7 集成 13 例→T1(1,2)/T4(3,4,5,6,7,13)/T5(8)/T6(9)/T7(10)/T8(11)/T9(12)
- **占位符**：无；`SessionMessageRequiresQueue` 是用户明确要求的"注释保留步骤"实现（返回 false + Warn + 4 步注释）
- **类型一致性**：`PlanStateStore/PlanState/PlanRequest/PlanStateResult` 在 T3 定义，T4 使用；`ResolveRequestRuntimeMode` 在 T2 定义，T3/T4 使用
- **风险提示**：T4 的 `openStateSession` 需按 `deep_adapter.go` 既有会话获取流程实现，执行时先读该文件确认 `SessionFacade` 访问方式

### 执行偏差记录（对齐上游优先）

1. **`BuildTodoSection` 不删除**（T7 Step 1 原文要求删）。上游 `task_planning_rail.py:157-165` 在 `inject_prompt=True` 分支仍调 `build_todo_section`，Go 删除会让该分支失去节内容。实现保留 `sections.BuildTodoSection` 与单测，只把常驻注入关掉。
2. **`inject_prompt` 默认值改回 true**（T7 Step 1 写"默认 false"）。上游构造器签名是 `TaskPlanningRail(inject_prompt=True)`（`task_planning_rail.py:49`），是 **DeepAdapter 显式传 False**（`interface_deep.py:10756`）才关闭。Go 在 `buildTaskPlanningRail` 传 `WithInjectPrompt(false)`，与上游装配点一一对应；rail 本身默认保持 true，`tests/integration/agentcore/harness/rails/planning` 原有用例（默认注入）因此无需改写。
3. **旧模式串别名保留**（T10 Step 1 要求 grep 为空）。`mode.go` 的 `"agent.plan"` / `"agent.fast"` 出现在 `MODE_ALIASES` 等归一化表，对应上游 `gateway/common/mode_matrix.py:96-97`，是兼容入口而非残留。
4. **`syncMultiSessionTools` 只保证既有装配不变**：上游新版 `_update_tools_for_mode`（`interface_deep.py:14119`）只清理 legacy `session_new`/`session_cancel`，产品会话工具改由 `_ensure_session_messaging_tools_registered`（`SessionMessagingToolkit`，含 web/tui 渠道与 team 排除判定）管理。Go 尚无 `SessionMessagingToolkit`，本轮按 plan 保留「plan 卸载 session_*、normal 由 `updateToolsForMode` 重新注册」的既有语义；补 `SessionMessagingToolkit` 记为独立任务。
5. **AgentModeRail 仍缺两项上游能力**：`plan_mode_system_note` / `plan_mode_attachment_note` 静态注入口，以及 code 档 `_CODE_PLAN_ALLOWED_TOOLS`（`pcs_search`、`tool_search`、`tool_call`、`symphony_*`）白名单扩展。已在缺陷台账 H-02 的「Go 落点」列注明，属该行后续工作。
6. **进入 plan 的 query 改写丢用户原文（已修）**：`ensurePlanState` 原先只从 `params["query"]` 取值，Go 进程内调用方（Runner 入口）允许只把 query 放 `inputs`，导致进入 plan 那一轮被改写成「提醒 + 空串」，用户原文整条丢失（`TestSessionUsageIsolationSuite` 因此红）。新增 `resolveRequestQuery`：params 优先、inputs 回落，改写后回写两处。
7. **coding memory 写入用例改用 normal 模式**：plan 模式 `AgentModeRail` 只放行 `_CODE_PLAN_ALLOWED_TOOLS`，`coding_memory_write` 会被拦截（上游同行为）。原用例在 plan 下断言写盘成功，是因为 S2 之前 plan 状态根本没被写入（`PlanModeController` 未接入）；现已按上游语义改为 normal 模式跑写入链路。

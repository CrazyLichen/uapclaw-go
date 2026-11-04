# Plan / Runtime 层对齐 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 把 `PlanModeController` 的调用点从 adapter 上移到新增的 `AgentRuntime`（`internal/swarm/runtime/service.go`），使 query 改写先于 `BuildInputs`、控制事件真正下发，文件/类型/方法/步骤与 Python 上游同名同序。

**Architecture:** 新增 `internal/swarm/runtime/{service.go,events.go}`（`AgentRuntime` + `RuntimeEvent`），补齐 `runtime/request.py` 的 8 个函数；`AgentServer` 的 chat 入口退化为「分发 + 发帧 + keepalive + 任务登记」；`PlanModeController` 去掉构造期 store 绑定，改为运行期 `PlanStateAccessor`（由 `UapClawSwarm` 实现）；`uapclaw.go` 改名 `interface.go`、`UapClaw` 改名 `UapClawSwarm`。

**Tech Stack:** Go 1.26、CGO + `sqlite_fts5`、testify/suite、`cmd/integration-audit`。

**Spec:** `docs/superpowers/specs/2026-11-04-plan-runtime-layer-alignment-design.md`

---

## 文件结构

| 文件 | 责任 | 动作 |
|---|---|---|
| `internal/swarm/runtime/events.go` | `RuntimeEvent` 与响应/帧转换 | 新建 |
| `internal/swarm/runtime/service.go` | `AgentRuntime`：`Invoke/Stream` 五步、`prepareChatTurn`、`emitControlEvents` | 新建 |
| `internal/swarm/runtime/request.go` | 请求模式/工程目录/元数据同步/`PrepareChatTurn`/`CancelRequest` | 补 7 函数 |
| `internal/swarm/runtime/plan.go` | 控制器；`PlanStateAccessor` 运行期注入；`ActiveSessions()` | 改 |
| `internal/swarm/server/runtime/interface.go` | `UapClawSwarm`（原 `uapclaw.go`），实现 `PlanStateAccessor` | 改名+增方法 |
| `internal/swarm/server/handle_message.go` | `handleMessage` 分发 + `writeResponse/writeChunk/writeErrorResponse/sendToPayload` | 从 `handle_envelope.go` 拆出 |
| `internal/swarm/server/handle_chat.go` | `handleUnary/Impl`、`handleStream/Impl`、`handleCancel`、`sendRuntimeEvent` | 从 `handle_envelope.go` 拆出并重写入口 |
| `internal/swarm/server/keepalive.go` | `StreamKeepalive` 类 | 新建（吸收 `runKeepalive`） |
| `internal/swarm/server/handle_session_plan.go` | `handleSessionPlanStatus` | 新建 |
| `internal/swarm/schema/req_method.go` | `ReqMethodSessionPlanStatus` | 增常量 |

---

### Task 1: `RuntimeEvent`（events.go）

**Files:**
- Create: `internal/swarm/runtime/events.go`
- Test: `internal/swarm/runtime/events_test.go`

- [x] **Step 1: 写失败测试**

```go
package runtime

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/uapclaw/uapclaw-go/internal/swarm/schema"
)

func TestRuntimeEvent_Control(t *testing.T) {
	sid := "sess-1"
	req := &schema.AgentRequest{RequestID: "req-1", ChannelID: "web", SessionID: &sid}
	ev := Control(req, map[string]any{"event_type": PlanModeExitedEvent, "mode": "agent.work.normal"})

	require.Equal(t, "req-1", ev.RequestID)
	require.Equal(t, "web", ev.ChannelID)
	require.Equal(t, "sess-1", ev.SessionID)
	require.Equal(t, PlanModeExitedEvent, ev.EventType())
	require.True(t, ev.OK)

	d := ev.ToDict()
	require.Equal(t, "agent.work.normal", d["payload"].(map[string]any)["mode"])
}

func TestRuntimeEvent_FromAgentResponse(t *testing.T) {
	resp := &schema.AgentResponse{
		RequestID: "req-1", ChannelID: "web", OK: true,
		Payload: map[string]any{"event_type": "chat.final", "content": "hi"},
	}
	ev := FromAgentResponse(resp)
	require.Equal(t, "chat.final", ev.EventType())
	require.True(t, ev.IsComplete)
	require.Equal(t, "hi", ev.Payload["content"])
}
```

- [x] **Step 2: 跑测试确认失败**

Run: `CGO_ENABLED=1 go test -tags "sqlite_fts5 test" ./internal/swarm/runtime/ -run TestRuntimeEvent`
Expected: FAIL `undefined: Control` / `undefined: RuntimeEvent`

- [x] **Step 3: 实现 events.go**

```go
package runtime

import "github.com/uapclaw/uapclaw-go/internal/swarm/schema"

// ──────────────────────────── 结构体 ────────────────────────────

// RuntimeEvent Runtime 流中的一个事件（输出/线缆渲染之前的中间形态）。
//
// Python: jiuwenswarm/runtime/events.py RuntimeEvent
type RuntimeEvent struct {
	// RequestID 请求标识
	RequestID string
	// ChannelID 渠道标识
	ChannelID string
	// SessionID 会话标识
	SessionID string
	// Payload 事件负载（event_type 等）
	Payload map[string]any
	// IsComplete 是否为完成事件
	IsComplete bool
	// OK 执行是否成功
	OK bool
}

// ──────────────────────────── 导出函数 ────────────────────────────

// Control 构造控制事件。
//
// Python: RuntimeEvent.control(request_id=..., channel_id=..., session_id=..., payload=...)
func Control(req *schema.AgentRequest, payload map[string]any) *RuntimeEvent {
	sid := ""
	if req != nil && req.SessionID != nil {
		sid = *req.SessionID
	}
	ev := &RuntimeEvent{Payload: payload, OK: true, SessionID: sid}
	if req != nil {
		ev.RequestID, ev.ChannelID = req.RequestID, req.ChannelID
	}
	return ev
}

// FromAgentResponse 由 AgentResponse 包装为 RuntimeEvent。
//
// Python: RuntimeEvent.from_agent_message
func FromAgentResponse(resp *schema.AgentResponse) *RuntimeEvent {
	if resp == nil {
		return &RuntimeEvent{OK: true, IsComplete: true}
	}
	return &RuntimeEvent{
		RequestID:  resp.RequestID,
		ChannelID:  resp.ChannelID,
		Payload:    resp.Payload,
		IsComplete: true,
		OK:         resp.OK,
	}
}

// FromAgentResponseChunk 由流式 chunk 包装为 RuntimeEvent。
func FromAgentResponseChunk(chunk *schema.AgentResponseChunk) *RuntimeEvent {
	if chunk == nil {
		return &RuntimeEvent{OK: true, IsComplete: true}
	}
	return &RuntimeEvent{
		RequestID:  chunk.RequestID,
		ChannelID:  chunk.ChannelID,
		Payload:    chunk.Payload,
		IsComplete: chunk.IsComplete,
		OK:         true,
	}
}

// ToChunk 还原为流式 chunk（仅取 wire 需要的字段）。
func (e *RuntimeEvent) ToChunk() *schema.AgentResponseChunk {
	return &schema.AgentResponseChunk{
		RequestID:  e.RequestID,
		ChannelID:  e.ChannelID,
		Payload:    e.Payload,
		IsComplete: e.IsComplete,
	}
}

// ToDict 序列化为字典（对齐 Python to_dict，不含内部字段）。
func (e *RuntimeEvent) ToDict() map[string]any {
	return map[string]any{
		"request_id": e.RequestID,
		"channel_id": e.ChannelID,
		"session_id": e.SessionID,
		"payload":    e.Payload,
		"is_complete": e.IsComplete,
		"ok":          e.OK,
	}
}

// EventType 取 payload["event_type"]。
//
// Python: RuntimeEvent.event_type
func (e *RuntimeEvent) EventType() string {
	if e == nil || e.Payload == nil {
		return ""
	}
	s, _ := e.Payload["event_type"].(string)
	return s
}
```

- [x] **Step 4: 跑测试确认通过**

Run: `CGO_ENABLED=1 go test -tags "sqlite_fts5 test" ./internal/swarm/runtime/ -run TestRuntimeEvent -v`
Expected: PASS

- [x] **Step 5: 提交**

```bash
git add internal/swarm/runtime/events.go internal/swarm/runtime/events_test.go
git commit -m "feat(runtime): 新增 RuntimeEvent（Control/FromAgentResponse/ToDict/EventType）"
```

---

### Task 2: `runtime/request.go` 补 7 函数 + mode 解析三合一

> **进度（2026-11-04）**：mode 三合一与工程目录解析已完成并提交（`df4c45fd`、`ac51eeff`）；
> 剩余 `SyncChatRequestMetadata` / `PrepareChatTurn` / `CancelRequest` 待做（与 Task 4 的
> `AgentRuntime` 强耦合，随 Task 4 一起落）。


**Files:**
- Modify: `internal/swarm/runtime/request.go`
- Modify: `internal/swarm/server/handle_envelope.go:602-700`（删 `resolveMode`、`applyResolvedModeToRequest`）
- Modify: `internal/swarm/server/runtime/agent_manager.go:803-830`（删 `resolveModeFromRequest`）
- Test: `internal/swarm/runtime/request_test.go`

- [x] **Step 1: 写失败测试（解析函数）**

```go
func TestResolveAgentRequestMode(t *testing.T) {
	cases := []struct{ name, raw, work string; wantMode, wantSub, wantCanonical string }{
		{"空值默认 agent", "", "", "agent", "", "agent"},
		{"三段 plan", "agent.work.plan", "", "agent", "plan", "agent.work.plan"},
		{"code normal", "code.normal", "", "code", "normal", "code.normal"},
		{"team plan", "team.plan", "", "code", "team", "code.team.plan"},
		{"auto_harness", "auto_harness", "", "auto_harness", "auto_harness", "auto_harness"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			mode, sub, canonical := ResolveAgentRequestMode(tc.raw, tc.work)
			require.Equal(t, tc.wantMode, mode)
			require.Equal(t, tc.wantSub, sub)
			require.Equal(t, tc.wantCanonical, canonical)
		})
	}
}
```

- [x] **Step 2: 跑测试确认失败**

Run: `CGO_ENABLED=1 go test -tags "sqlite_fts5 test" ./internal/swarm/runtime/ -run TestResolveAgentRequestMode`
Expected: FAIL `undefined: ResolveAgentRequestMode`

- [x] **Step 3: 实现解析 + 应用（节选，完整实现按上游 `runtime/request.py:266-360` 逐支迁移）**

```go
// ResolveAgentRequestMode 解析 mode 为 manager mode、sub-mode 与 canonical mode。
//
// Python: jiuwenswarm/runtime/request.py resolve_agent_request_mode
func ResolveAgentRequestMode(rawMode any, workMode string) (mode, subMode, canonical string) {
	modeText := schema.CanonicalizeMode(strings.TrimSpace(fmt.Sprint(rawMode)))
	if rawMode == nil || strings.TrimSpace(fmt.Sprint(rawMode)) == "" {
		modeText = "agent"
	}
	if m, s, c, ok := schema.ResolveNewCanonicalMode(modeText); ok {
		return m, s, c
	}
	if modeText == "auto_harness" {
		return "auto_harness", "auto_harness", "auto_harness"
	}
	normalizedWork := strings.ToLower(strings.TrimSpace(workMode))
	if modeText == "plan" || modeText == "fast" {
		if normalizedWork == "code" {
			return "code", "normal", "code.normal"
		}
		return "agent", "", "agent"
	}
	if modeText == "team.plan" {
		return "code", "team", "code.team.plan"
	}
	if modeText == "agent.plan" {
		if normalizedWork == "code" {
			return "code", "plan", "code.plan"
		}
		return "agent", "plan", "agent.plan"
	}
	parts := strings.SplitN(modeText, ".", 2)
	mode = parts[0]
	if mode == "" {
		mode = "agent"
	}
	if mode == "agent" {
		if normalizedWork == "code" {
			return "code", "normal", "code.normal"
		}
		return "agent", "", "agent"
	}
	subMode = ""
	if len(parts) > 1 {
		subMode = parts[1]
	}
	if mode == "code" && subMode != "plan" && subMode != "normal" && subMode != "team" {
		subMode = "normal"
	}
	if subMode != "" {
		canonical = mode + "." + subMode
	} else {
		canonical = mode
	}
	return mode, subMode, canonical
}

// ApplyResolvedModeToRequest 解析并把 canonical mode 写回 request.Params。
//
// Python: jiuwenswarm/runtime/request.py apply_resolved_mode_to_request
func ApplyResolvedModeToRequest(req *schema.AgentRequest, workMode string) (mode, subMode string) {
	params := parseRequestParams(req)
	raw := params["mode"]
	mode, subMode, canonical := ResolveAgentRequestMode(raw, workMode)
	params["mode"] = canonical
	writeRequestParams(req, params)
	return mode, subMode
}
```

同时实现 `ResolveRequestProjectDir`、`ResolveCancelProjectDir`、`SyncChatRequestMetadata`、`PrepareChatTurn`（见 Task 3）、`CancelRequest`（见 Task 4）。

- [x] **Step 4: 三处旧实现改调用（删除旧函数体）**

`handle_envelope.go` 的 `resolveMode` / `applyResolvedModeToRequest` 与 `agent_manager.go` 的 `resolveModeFromRequest` 全部删除，调用点改为：

```go
mode, subMode := swruntime.ApplyResolvedModeToRequest(request, "")
```

`agent_manager.go`：

```go
mode, _ := swruntime.ResolveRequestRuntimeModeFromRequest(request) // 见 Step 5
```

- [x] **Step 5: 补请求视图辅助（同文件）**

```go
// ResolveRequestRuntimeModeFromRequest 从 request 解析权威模式（含 work_mode）。
//
// Python: jiuwenswarm/runtime/request.py resolve_request_runtime_mode
func ResolveRequestRuntimeModeFromRequest(req *schema.AgentRequest) ResolvedMode {
	params := parseRequestParams(req)
	workMode, _ := params["work_mode"].(string)
	mode, subMode, canonical := ResolveAgentRequestMode(params["mode"], workMode)
	return ResolvedMode{
		CanonicalMode: canonical,
		IsPlan:        schema.IsPlanMode(canonical),
		IsTeam:        schema.IsTeamMode(canonical),
		NormalMode:    baseModeWithoutPlan(canonical),
	}
}
```

- [x] **Step 6: 跑包内测试与受影响包**

Run: `CGO_ENABLED=1 go test -tags "sqlite_fts5 test" ./internal/swarm/runtime/ ./internal/swarm/server/...`
Expected: PASS（`resolveMode` 默认 `"plan"` 的旧断言同步改掉）

- [x] **Step 7: 提交**

```bash
git add internal/swarm/runtime/request.go internal/swarm/runtime/request_test.go \
        internal/swarm/server/handle_envelope.go internal/swarm/server/runtime/agent_manager.go
git commit -m "refactor(runtime): 补 request.py 函数并对齐 mode 解析（删三份私有实现）"
```

---

### Task 3: `PlanStateAccessor` 解耦 + `ActiveSessions`

> **进度（2026-11-04, commit 6e1bbd6a）**：已完成 —— controller 无参构造、运行期 accessor、
> `ActiveSessions()`、adapter 删 4 个 plan 符号 + `controlEvents` 字段并新增 `PlanStateAccessor()`、
> 删除 `resolveRequestQuery`（单通道）。**当前运行期 plan 同步处于断点**：adapter 侧已移除、
> `AgentRuntime`（Task 4）与 `handle_chat`（Task 7）尚未接线，集成用例附件模式/plan_state 预期红。


**Files:**
- Modify: `internal/swarm/runtime/plan.go:64-100`（接口）、`118-135`（构造函数）、`132-215`（EnsureState）、`225-245`（CheckPostProcessExit）
- Modify: `internal/swarm/server/adapter/deep_adapter_plan.go`
- Test: `internal/swarm/runtime/plan_test.go`

- [x] **Step 1: 写失败测试**

```go
type fakeAccessor struct {
	state PlanState
	saved []PlanState
}

func (f *fakeAccessor) LoadPlanState(context.Context, string) (PlanState, error) { return f.state, nil }
func (f *fakeAccessor) SavePlanState(_ context.Context, _ string, st PlanState) error {
	f.saved = append(f.saved, st); f.state = st; return nil
}
func (f *fakeAccessor) SwitchPlanMode(_ context.Context, _ string, mode string) error {
	f.state.Mode = mode; return nil
}
func (f *fakeAccessor) CommitSession(context.Context, string) error { return nil }

func TestEnsureState_运行期注入Accessor(t *testing.T) {
	c := NewPlanModeController()
	acc := &fakeAccessor{}
	res, err := c.EnsureState(context.Background(), PlanRequest{
		SessionID: "s1", Mode: "agent.work.plan", ReqMethod: "chat.send",
	}, acc)
	require.NoError(t, err)
	require.True(t, res.Entered)
	require.Equal(t, "plan", acc.state.Mode)
	require.Contains(t, c.ActiveSessions(), "s1")
}
```

- [x] **Step 2: 跑测试确认失败**

Run: `CGO_ENABLED=1 go test -tags "sqlite_fts5 test" ./internal/swarm/runtime/ -run TestEnsureState_运行期注入Accessor`
Expected: FAIL（`EnsureState` 参数数量不匹配 / `undefined: ActiveSessions`）

- [x] **Step 3: 改接口与方法签名**

```go
// PlanStateAccessor plan 状态读写入口（运行期由调用方传入，对齐上游 ensure_state(request, mode, sub_mode, agent)）。
type PlanStateAccessor interface {
	// LoadPlanState 读取会话 plan 状态
	LoadPlanState(ctx context.Context, sessionID string) (PlanState, error)
	// SavePlanState 保存会话 plan 状态
	SavePlanState(ctx context.Context, sessionID string, st PlanState) error
	// SwitchPlanMode 切 plan 模式
	SwitchPlanMode(ctx context.Context, sessionID, mode string) error
	// CommitSession 提交会话
	CommitSession(ctx context.Context, sessionID string) error
}

// NewPlanModeController 创建控制器（不再绑定 store）。
func NewPlanModeController() *PlanModeController {
	return &PlanModeController{
		locks:  make(map[string]*sync.Mutex),
		active: make(map[string]struct{}),
		exited: make(map[string]struct{}),
	}
}

// EnsureState 每轮同步 plan 状态。
//
// Python: PlanModeController.ensure_state(request, mode, sub_mode, agent)
func (c *PlanModeController) EnsureState(ctx context.Context, req PlanRequest, acc PlanStateAccessor) (PlanStateResult, error)

// CheckPostProcessExit 执行后检测工具触发的 plan 退出。
func (c *PlanModeController) CheckPostProcessExit(ctx context.Context, req PlanRequest, acc PlanStateAccessor) ([]ControlEvent, error)

// ActiveSessions 返回当前处于 plan 的会话集合快照。
//
// Python: get_plan_active_sessions()
func (c *PlanModeController) ActiveSessions() map[string]struct{}
```

- [x] **Step 4: adapter 侧改为 accessor 提供者**

`deep_adapter_plan.go`：`planStateStore` 保留（含 `openPlanSession`），`NewDeepAdapter` 里 `NewPlanModeController()` 不再传 store；暴露：

```go
// PlanStateAccessor 返回会话 plan 状态读写入口（由 UapClawSwarm 在调用控制器时传入）。
func (d *DeepAdapter) PlanStateAccessor() swruntime.PlanStateAccessor { return &planStateStore{d: d} }
```

同时删除 `ensurePlanState` / `checkPlanPostProcess` / `emitControlEvents` / `DrainControlEvents` / `controlEvents` 字段（见删除清单）。

- [x] **Step 5: 跑测试**

Run: `CGO_ENABLED=1 go test -tags "sqlite_fts5 test" ./internal/swarm/runtime/ ./internal/swarm/server/adapter/`
Expected: PASS

- [x] **Step 6: 提交**

```bash
git add internal/swarm/runtime/plan.go internal/swarm/runtime/plan_test.go internal/swarm/server/adapter/
git commit -m "refactor(runtime): PlanModeController 运行期注入 PlanStateAccessor + ActiveSessions"
```

---

### Task 4: `AgentRuntime`（service.go）

**Files:**
- Create: `internal/swarm/runtime/service.go`
- Test: `internal/swarm/runtime/service_test.go`

- [x] **Step 1: 写失败测试（步骤顺序）**

```go
type fakeAgent struct {
	calls []string
}

func (f *fakeAgent) ProcessMessage(context.Context, *schema.AgentRequest) (*schema.AgentResponse, error) {
	f.calls = append(f.calls, "execute")
	return &schema.AgentResponse{OK: true, Payload: map[string]any{"event_type": "chat.final"}}, nil
}

func TestAgentRuntime_Invoke_步骤顺序(t *testing.T) {
	agent := &fakeAgent{}
	acc := &fakeAccessor{}
	rt := NewAgentRuntime(AgentRuntimeDeps{
		Prepare: func(_ context.Context, req *schema.AgentRequest, _ string) (string, PlanStateAccessor, ChatAgentPort, error) {
			return "agent", acc, agent, nil
		},
		PlanController: NewPlanModeController(),
		OnControlEvent: func(ev *RuntimeEvent) { agent.calls = append(agent.calls, "event") },
	})
	events, err := rt.Invoke(context.Background(), reqWithMode("agent.work.plan"))
	require.NoError(t, err)
	require.Equal(t, []string{"execute", "event"}, agent.calls) // 执行后补退出/进入事件由 planEnabled 决定
	require.NotEmpty(t, events)
}
```

- [x] **Step 2: 跑测试确认失败**

Run: `CGO_ENABLED=1 go test -tags "sqlite_fts5 test" ./internal/swarm/runtime/ -run TestAgentRuntime_Invoke`
Expected: FAIL `undefined: AgentRuntime`

- [x] **Step 3: 实现 service.go（Invoke/Stream + 五步）**

```go
// AgentRuntime 运行时编排：选 agent、同步 plan 状态、执行、下发控制事件。
//
// Python: jiuwenswarm/runtime/service.py AgentRuntime
type AgentRuntime struct {
	mu             sync.Mutex
	started        bool
	closed         bool
	manager        AgentManagerPort
	planController *PlanModeController
	onControlEvent func(*RuntimeEvent)
	prepare        PrepareChatTurnFunc
}

// AgentRuntimeDeps AgentRuntime 依赖（构造注入）。
type AgentRuntimeDeps struct {
	Manager        AgentManagerPort
	PlanController *PlanModeController
	OnControlEvent func(*RuntimeEvent)
	Prepare        PrepareChatTurnFunc
}

// AgentManagerPort runtime 需要的 manager 能力（由 server 层实现，避免反向依赖）。
type AgentManagerPort interface {
	// GetChatAgent 按 mode/subMode/projectDir 取聊天 agent
	GetChatAgent(ctx context.Context, channelID, mode, projectDir string) (ChatAgentPort, error)
}

// ChatAgentPort runtime 需要的 agent 能力。
type ChatAgentPort interface {
	// ProcessMessage 非流式执行
	ProcessMessage(ctx context.Context, req *schema.AgentRequest) (*schema.AgentResponse, error)
	// ProcessMessageStream 流式执行
	ProcessMessageStream(ctx context.Context, req *schema.AgentRequest) (<-chan *schema.AgentResponseChunk, error)
	// PlanStateAccessor 返回 plan 状态读写入口
	PlanStateAccessor() PlanStateAccessor
}

// PrepareChatTurnFunc 选 agent 并返回 (mode, accessor, agent)，测试可注入。
type PrepareChatTurnFunc func(ctx context.Context, req *schema.AgentRequest, channelID string) (string, PlanStateAccessor, ChatAgentPort, error)

// NewAgentRuntime 创建运行时。
//
// Python: AgentRuntime.__init__
func NewAgentRuntime(deps AgentRuntimeDeps) *AgentRuntime {
	if deps.PlanController == nil {
		deps.PlanController = NewPlanModeController()
	}
	return &AgentRuntime{
		manager:        deps.Manager,
		planController: deps.PlanController,
		onControlEvent: deps.OnControlEvent,
		prepare:        deps.Prepare,
	}
}

// Start 幂等启动。
//
// Python: AgentRuntime.start（service.py:655）
func (r *AgentRuntime) Start() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return errors.New("agent runtime is closed")
	}
	if r.started {
		logger.Warn(logComponent).Msg("agent runtime already started")
		return nil
	}
	r.started = true
	return nil
}

// Close 关闭（幂等）。不接管 Runner/checkpointer 生命周期。
//
// Python: AgentRuntime.close
func (r *AgentRuntime) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return nil
	}
	r.closed = true
	r.started = false
	return nil
}

// NormalizeRequest 归一化请求（保证 Params 为可读 map）。
//
// Python: AgentRuntime.normalize_request（service.py:3790）
func NormalizeRequest(req *schema.AgentRequest) *schema.AgentRequest {
	if req == nil {
		return &schema.AgentRequest{}
	}
	params := parseRequestParams(req)
	writeRequestParams(req, params)
	return req
}

// UsesSessionRuntime 判断请求是否走 session runtime 路径。
//
// Python: AgentRuntime.uses_session_runtime（service.py:3842）—— team 与
// 显式 stateless 请求不占用会话流式任务槽。
func UsesSessionRuntime(req *schema.AgentRequest) bool {
	params := parseRequestParams(req)
	if mode, ok := params["mode"].(string); ok && schema.IsTeamMode(mode) {
		return false
	}
	if v, ok := params["stateless"].(bool); ok && v {
		return false
	}
	return true
}

// Invoke 执行非流式 chat turn。
//
// Python: AgentRuntime.invoke → _invoke_started（service.py:1694/1786）
func (r *AgentRuntime) Invoke(ctx context.Context, req *schema.AgentRequest) ([]*RuntimeEvent, error) {
	req = NormalizeRequest(req)
	mode, acc, agent, err := r.prepareTurn(ctx, req)
	if err != nil {
		return nil, err
	}
	planEnabled := r.planEnabled(req, mode)
	if planEnabled {
		res, ensureErr := r.planController.EnsureState(ctx, planRequestOf(req, mode, ""), acc)
		if ensureErr != nil {
			logger.Warn(logComponent).Err(ensureErr).Msg("ensure plan state failed")
		} else {
			r.emitControlEvents(req, res.Events)
		}
	}
	resp, execErr := agent.ProcessMessage(ctx, req)
	r.checkPlanExit(ctx, req, mode, acc, planEnabled)
	if execErr != nil {
		return nil, execErr
	}
	return []*RuntimeEvent{FromAgentResponse(resp)}, nil
}

// Stream 执行流式 chat turn，逐 chunk 回调 onChunk；返回 false 时停止消费。
//
// Python: AgentRuntime.stream → _stream_started（service.py:2183/2482）
func (r *AgentRuntime) Stream(ctx context.Context, req *schema.AgentRequest,
	onChunk func(*RuntimeEvent) bool) error {
	req = NormalizeRequest(req)
	mode, acc, agent, err := r.prepareTurn(ctx, req)
	if err != nil {
		return err
	}
	planEnabled := r.planEnabled(req, mode)
	if planEnabled {
		res, ensureErr := r.planController.EnsureState(ctx, planRequestOf(req, mode, ""), acc)
		if ensureErr != nil {
			logger.Warn(logComponent).Err(ensureErr).Msg("ensure plan state failed")
		} else {
			r.emitControlEvents(req, res.Events)
		}
	}
	ch, err := agent.ProcessMessageStream(ctx, req)
	if err != nil {
		r.checkPlanExit(ctx, req, mode, acc, planEnabled)
		return err
	}
	for chunk := range ch {
		if !onChunk(FromAgentResponseChunk(chunk)) {
			break
		}
	}
	// 流结束：退出事件在 channel 关闭前发送（对齐 Python generator finally 里的 yield）
	r.checkPlanExit(ctx, req, mode, acc, planEnabled)
	return nil
}

// prepareTurn 选 agent 并返回 plan 状态入口。
//
// Python: _prepare_chat_turn + prepare_chat_turn（service.py:1266 / request.py:361）
func (r *AgentRuntime) prepareTurn(ctx context.Context, req *schema.AgentRequest) (string, PlanStateAccessor, ChatAgentPort, error) {
	if r.prepare != nil {
		return r.prepare(ctx, req, req.ChannelID)
	}
	mode, _ := ApplyResolvedModeToRequest(req, "")
	agent, err := r.manager.GetChatAgent(ctx, req.ChannelID, mode, ResolveRequestProjectDir(req))
	if err != nil {
		return "", nil, nil, err
	}
	return mode, agent, agent, nil
}

// planEnabled 判定本轮是否参与 plan 状态同步（team/resume/非 chat 方法跳过）。
//
// Python: service.py:1859 `if not readonly_goal_get` + PlanModeController.should_sync
func (r *AgentRuntime) planEnabled(req *schema.AgentRequest, mode string) bool {
	if req == nil || r.planController == nil {
		return false
	}
	if schema.IsTeamMode(mode) {
		return false
	}
	params := parseRequestParams(req)
	if resume, ok := params["resume"].(bool); ok && resume {
		return false
	}
	return ShouldSync(req.ReqMethod.String())
}

// checkPlanExit 执行后检测工具触发的 plan 退出。
//
// Python: PlanModeController.check_post_process_exit（service.py:1911）
func (r *AgentRuntime) checkPlanExit(ctx context.Context, req *schema.AgentRequest, mode string, acc PlanStateAccessor, enabled bool) {
	if !enabled {
		return
	}
	events, err := r.planController.CheckPostProcessExit(ctx, planRequestOf(req, mode, ""), acc)
	if err != nil {
		logger.Error(logComponent).Err(err).Msg("check plan post process failed")
		return
	}
	r.emitControlEvents(req, events)
}

// emitControlEvents 把控制事件交给回调；无回调时记 Debug（对应 Python handler 为 None 的分支）。
//
// Python: AgentRuntime._emit_control_events（service.py:4405）
func (r *AgentRuntime) emitControlEvents(req *schema.AgentRequest, events []ControlEvent) {
	for _, ce := range events {
		ev := Control(req, map[string]any{"event_type": ce.EventType, "mode": ce.Mode})
		if r.onControlEvent != nil {
			r.onControlEvent(ev)
			continue
		}
		logger.Debug(logComponent).Str("event_type", ce.EventType).Str("mode", ce.Mode).
			Msg("control event without handler")
	}
}

// planRequestOf 把请求投影为控制器入参。
func planRequestOf(req *schema.AgentRequest, mode, subMode string) PlanRequest {
	params := parseRequestParams(req)
	sid := ""
	if req.SessionID != nil {
		sid = *req.SessionID
	}
	query, _ := params["query"].(string)
	entry, _ := params[PlanEntrySourceKey].(string)
	prev, _ := params[PreviousSessionModeKey].(string)
	return PlanRequest{
		SessionID:           sid,
		Mode:                mode,
		SubMode:             subMode,
		ReqMethod:           req.ReqMethod.String(),
		EntrySource:         entry,
		IsInterruptResume:   params["resume"] == true,
		PreviousSessionMode: prev,
		Query:               query,
	}
}
```

`AgentManagerPort` / `PrepareChatTurnFunc` 为 runtime 包内的窄接口，避免 runtime → server 反向依赖；`UapClawSwarm` 在 server 层实现并注入。

- [x] **Step 4: 跑测试**

Run: `CGO_ENABLED=1 go test -tags "sqlite_fts5 test" ./internal/swarm/runtime/`
Expected: PASS

- [x] **Step 5: 提交**

```bash
git add internal/swarm/runtime/service.go internal/swarm/runtime/service_test.go
git commit -m "feat(runtime): 新增 AgentRuntime（Invoke/Stream 五步编排 + 控制事件回调）"
```

---

### Task 5: `session.plan_status` RPC

**Files:**
- Modify: `internal/swarm/schema/req_method.go`（+常量）
- Create: `internal/swarm/server/handle_session_plan.go`
- Modify: `internal/swarm/server/handle_message.go`（分发）
- Test: `internal/swarm/server/handle_session_plan_test.go`

- [x] **Step 1: 写失败测试**

```go
func TestHandleSessionPlanStatus(t *testing.T) {
	s := newTestServer(t) // 复用现有测试助手
	resp, err := s.handleSessionPlanStatus(context.Background(), reqWithParams(map[string]any{"session_id": "s1"}))
	require.NoError(t, err)
	require.Equal(t, false, resp.Payload["plan_mode"])
}
```

- [x] **Step 2: 跑测试确认失败**

Run: `CGO_ENABLED=1 go test -tags "sqlite_fts5 test" ./internal/swarm/server/ -run TestHandleSessionPlanStatus`
Expected: FAIL `undefined: handleSessionPlanStatus`

- [x] **Step 3: 实现**

```go
// ReqMethodSessionPlanStatus 只读查询会话是否处于计划模式
ReqMethodSessionPlanStatus ReqMethod = "session.plan_status"
```

```go
// handleSessionPlanStatus 只读查询当前会话是否处于 plan。
//
// 不调用 EnsureState/SwitchMode，不修改活跃集合。
// Python: agent_ws_server.py:3779 _handle_session_plan_status
func (s *AgentServer) handleSessionPlanStatus(ctx context.Context, request *schema.AgentRequest) (*schema.AgentResponse, error) {
	params := parseParams(request.Params)
	sid, _ := params["session_id"].(string)
	if sid == "" && request.SessionID != nil {
		sid = *request.SessionID
	}
	planMode := false
	if agent := s.agentManager.GetAgentNoWait(request.ChannelID, "", "", ""); agent != nil {
		if mode, ok := agent.PlanModeSnapshot(ctx, sid); ok {
			planMode = schema.IsPlanMode(mode)
			return planStatusResponse(request, sid, planMode, mode), nil
		}
	}
	mode := sessionMetadataMode(sid) // 兜底：metadata.mode
	return planStatusResponse(request, sid, schema.IsPlanMode(mode), mode), nil
}
```

- [x] **Step 4: 跑测试**

Run: `CGO_ENABLED=1 go test -tags "sqlite_fts5 test" ./internal/swarm/server/ -run TestHandleSessionPlanStatus`
Expected: PASS

- [x] **Step 5: 提交**

```bash
git add internal/swarm/schema/req_method.go internal/swarm/server/handle_session_plan.go \
        internal/swarm/server/handle_session_plan_test.go internal/swarm/server/handle_message.go
git commit -m "feat(server): 补 session.plan_status 只读查询（live 优先、metadata 兜底）"
```

---

### Task 6: `StreamKeepalive` 类化

**Files:**
- Create: `internal/swarm/server/keepalive.go`
- Modify: `internal/swarm/server/handle_chat.go`（使用新类）
- Delete: `handle_envelope.go` 中的 `runKeepalive`
- Test: `internal/swarm/server/keepalive_test.go`

- [x] **Step 1: 写失败测试**

```go
func TestStreamKeepalive_NotifyActivity重置计时(t *testing.T) {
	beats := make(chan struct{}, 4)
	k := NewStreamKeepalive(func() { beats <- struct{}{} }, 20*time.Millisecond)
	k.Start()
	time.Sleep(10 * time.Millisecond)
	k.NotifyActivity(false) // 重置，不应立即发送
	select {
	case <-beats:
		t.Fatal("NotifyActivity 后不应在重置窗口内发心跳")
	case <-time.After(15 * time.Millisecond):
	}
	k.Stop()
}
```

- [x] **Step 2: 跑测试确认失败**

Run: `CGO_ENABLED=1 go test -tags "sqlite_fts5 test" ./internal/swarm/server/ -run TestStreamKeepalive`
Expected: FAIL `undefined: NewStreamKeepalive`

- [x] **Step 3: 实现 keepalive.go（吸收现有 `runKeepalive` 的 timer 语义）**

```go
// StreamKeepalive 流式心跳：空闲超时发 keepalive，收到真实 chunk 或 terminal 后重置。
//
// Python: agent_ws_server.py:699 StreamKeepalive
type StreamKeepalive struct {
	send     func()
	interval time.Duration
	trigger  chan struct{}
	stopCh   chan struct{}
	done     chan struct{}
	once     sync.Once
}

// NewStreamKeepalive 创建心跳。
func NewStreamKeepalive(send func(), interval time.Duration) *StreamKeepalive {
	if interval <= 0 {
		interval = keepaliveInterval
	}
	return &StreamKeepalive{
		send:     send,
		interval: interval,
		trigger:  make(chan struct{}, 1),
		stopCh:   make(chan struct{}),
		done:     make(chan struct{}),
	}
}

// Start 启动心跳 goroutine。
func (k *StreamKeepalive) Start() {
	go func() {
		defer close(k.done)
		timer := time.NewTimer(k.interval)
		defer timer.Stop()
		for {
			select {
			case <-k.stopCh:
				return
			case <-k.trigger:
				if !timer.Stop() {
					select {
					case <-timer.C:
					default:
					}
				}
				timer.Reset(k.interval)
			case <-timer.C:
				k.send()
				timer.Reset(k.interval)
			}
		}
	}()
}

// NotifyActivity 收到真实 chunk（terminal=true 时停止后续心跳）。
func (k *StreamKeepalive) NotifyActivity(terminal bool) {
	if terminal {
		k.SignalStop()
		return
	}
	select {
	case k.trigger <- struct{}{}:
	default:
	}
}

// SignalStop 尽早停止新心跳（runtime 清理仍先完成）。
func (k *StreamKeepalive) SignalStop() { k.once.Do(func() { close(k.stopCh) }) }

// Stop 停止并等待 goroutine 退出。
func (k *StreamKeepalive) Stop() {
	k.SignalStop()
	<-k.done
}
```

- [x] **Step 4: 跑测试**

Run: `CGO_ENABLED=1 go test -tags "sqlite_fts5 test" ./internal/swarm/server/ -run TestStreamKeepalive`
Expected: PASS

- [x] **Step 5: 提交**

```bash
git add internal/swarm/server/keepalive.go internal/swarm/server/keepalive_test.go
git commit -m "refactor(server): runKeepalive 改为 StreamKeepalive 类（Start/NotifyActivity/SignalStop/Stop）"
```

---

### Task 7: chat 入口重构（handle_chat.go + sendRuntimeEvent）

**Files:**
- Create: `internal/swarm/server/handle_chat.go`
- Modify: `internal/swarm/server/handle_envelope.go` → 改名为 `handle_message.go`（只保留分发与写出函数）
- Modify: `internal/swarm/server/agent_server.go`（持有 `*runtime.AgentRuntime`）
- Test: `internal/swarm/server/handle_chat_test.go`

- [x] **Step 1: 写失败测试（unary 走 runtime + 控制事件发 push）**

```go
func TestHandleUnary_控制事件独立发帧(t *testing.T) {
	s := newTestServerWithRuntime(t, /* runtime 返回 plan_mode_exited 事件 */)
	s.handleUnary(context.Background(), reqWithMode("agent.work.normal"))
	select {
	case frame := <-s.testPushCh:
		require.Contains(t, string(frame), "plan_mode_exited")
	case <-time.After(time.Second):
		t.Fatal("应下发 plan_mode_exited push 帧")
	}
}
```

- [x] **Step 2: 跑测试确认失败**

Run: `CGO_ENABLED=1 go test -tags "sqlite_fts5 test" ./internal/swarm/server/ -run TestHandleUnary_控制事件`
Expected: FAIL（仍走旧路径，无 push 帧）

- [x] **Step 3: 实现 handleUnaryImpl / handleStreamImpl（对齐上游步骤）**

```go
// handleUnaryImpl 非流式处理。
//
// Python: agent_ws_server.py:3941 _handle_unary_impl
func (s *AgentServer) handleUnaryImpl(ctx context.Context, request *schema.AgentRequest) {
	events, err := s.runtime.Invoke(ctx, request)
	if err != nil {
		s.writeErrorResponse(request.RequestID, request.ChannelID, err.Error(), "AGENT_PROCESS_ERROR")
		return
	}
	for _, ev := range events {
		if !s.sendRuntimeEvent(ev, false, 0) {
			logger.Warn(logComponent).Str("request_id", request.RequestID).Msg("unary send failed")
		}
	}
}

// handleStreamImpl 流式处理。
//
// Python: agent_ws_server.py:4654 _handle_stream_impl
func (s *AgentServer) handleStreamImpl(ctx context.Context, request *schema.AgentRequest) {
	sessionID := sessionIDOf(request)
	request = runtime.NormalizeRequest(request)
	if !runtime.UsesSessionRuntime(request) {
		streamCtx, finish := s.beginStreamTask(ctx, sessionID, request.RequestID)
		defer finish()
	}
	keepalive := NewStreamKeepalive(func() { s.sendKeepalive(request.RequestID, request.ChannelID) }, keepaliveInterval)
	keepalive.Start()
	chunkCount := 0
	err := s.runtime.Stream(streamCtx, request, func(ev *runtime.RuntimeEvent) bool {
		if ev.EventType() == runtime.PlanModeExitedEvent {
			keepalive.NotifyActivity(false) // 控制事件不占 sequence
			return s.sendRuntimeEvent(ev, true, chunkCount)
		}
		chunkCount++
		keepalive.NotifyActivity(ev.IsComplete)
		return s.sendRuntimeEvent(ev, true, chunkCount-1)
	})
	keepalive.SignalStop()
	if err != nil {
		logger.Error(logComponent).Err(err).Str("request_id", request.RequestID).Msg("agent stream failed")
		s.writeErrorResponse(request.RequestID, request.ChannelID, err.Error(), "AGENT_STREAM_ERROR")
	}
	keepalive.Stop()
}

// sendRuntimeEvent 按事件类型选择 push 帧或响应/流帧。
//
// Python: agent_ws_server.py:5024 _send_runtime_event
func (s *AgentServer) sendRuntimeEvent(ev *runtime.RuntimeEvent, streaming bool, sequence int) bool {
	if ev.EventType() == runtime.PlanModeExitedEvent {
		s.sendPush(map[string]any{
			"channel_id": ev.ChannelID, "session_id": ev.SessionID, "payload": ev.Payload,
		})
		return true
	}
	if streaming {
		return s.sendWireChunk(ev.ToChunk(), sequence)
	}
	return s.sendWireResponse(ev)
}
```

`sendWireChunk`/`sendWireResponse` 由 `writeChunk`/`writeResponse` 改造为返回 `bool`（`sendToGateway` 透传 `TrySendToRecvCh` 结果）。

- [x] **Step 4: 跑测试与包内回归**

Run: `CGO_ENABLED=1 go test -tags "sqlite_fts5 test" ./internal/swarm/server/...`
Expected: PASS

- [x] **Step 5: 提交**

```bash
git add internal/swarm/server/handle_chat.go internal/swarm/server/handle_message.go \
        internal/swarm/server/agent_server.go internal/swarm/server/handle_chat_test.go
git commit -m "refactor(server): chat 入口改走 AgentRuntime，控制事件走 push 帧（步骤对齐 _handle_*_impl）"
```

---

### Task 8: 改名 `uapclaw.go` → `interface.go`、`UapClaw` → `UapClawSwarm`

**Files:**
- Rename: `internal/swarm/server/runtime/uapclaw.go` → `interface.go`
- Rename: `uapclaw_test.go` → `interface_test.go`、`uapclaw_history_test.go` → `interface_history_test.go`
- Modify: 全部引用（169 处）

- [x] **Step 1: 机械改名**

```bash
git mv internal/swarm/server/runtime/uapclaw.go internal/swarm/server/runtime/interface.go
git mv internal/swarm/server/runtime/uapclaw_test.go internal/swarm/server/runtime/interface_test.go
git mv internal/swarm/server/runtime/uapclaw_history_test.go internal/swarm/server/runtime/interface_history_test.go
grep -rl 'UapClaw' --include='*.go' internal cmd tests | xargs sed -i 's/UapClawSwarm/UapClaw/g; s/UapClaw/UapClawSwarm/g'
```

> 先反向替换再到正向，避免 `UapClawSwarm` 被二次改名。执行后必须 `grep -rn 'UapClaw\b' internal tests cmd` 为 0。

- [x] **Step 2: 加 `PlanStateAccessor` 实现与 `PlanModeSnapshot`**

```go
// PlanStateAccessor 返回 plan 状态读写入口。
func (uc *UapClawSwarm) PlanStateAccessor() swruntime.PlanStateAccessor {
	provider, ok := uc.adapter.(interface {
		PlanStateAccessor() swruntime.PlanStateAccessor
	})
	if !ok {
		return nil
	}
	return provider.PlanStateAccessor()
}

// PlanModeSnapshot 只读当前会话 plan 模式。
//
// Python: DeepAgentState.plan_mode（plan_status 用）
func (uc *UapClawSwarm) PlanModeSnapshot(ctx context.Context, sessionID string) (string, bool) {
	acc := uc.PlanStateAccessor()
	if acc == nil || sessionID == "" {
		return "", false
	}
	st, err := acc.LoadPlanState(ctx, sessionID)
	if err != nil {
		return "", false
	}
	return st.Mode, true
}
```

- [x] **Step 3: 编译 + 测试**

Run: `CGO_ENABLED=1 go build -tags sqlite_fts5 ./... && CGO_ENABLED=1 go test -tags "sqlite_fts5 test" ./internal/swarm/...`
Expected: PASS

- [x] **Step 4: 提交**

```bash
git add -A internal/swarm/server/runtime internal cmd tests
git commit -m "refactor(server): uapclaw.go→interface.go、UapClaw→UapClawSwarm（对齐 interface.py:JiuWenSwarm 位置）"
```

---

### Task 9: 删除 adapter 侧 plan 接入与残留

**Files:**
- Modify: `internal/swarm/server/adapter/deep_adapter_plan.go`（删 5 个符号）
- Modify: `internal/swarm/server/adapter/deep_adapter.go`（删字段与初始化）
- Modify: `internal/swarm/server/adapter/deep_adapter_test.go` / `deep_adapter_config_test.go`（删相关用例）

- [x] **Step 1: 删除并改引用**

删除：`ensurePlanState`、`checkPlanPostProcess`、`emitControlEvents`、`DrainControlEvents`、`controlEvents`/`controlMu` 字段及初始化。
保留：`PlanStateAccessor()`、`planStateStore`（4 个方法）、`ResetPlanSession`。

```bash
grep -rn 'DrainControlEvents\|ensurePlanState\|checkPlanPostProcess\|emitControlEvents\|controlEvents' --include='*.go' internal tests
```

Expected: 仅剩 `deep_adapter_plan.go` 内的 `ResetPlanSession` 引用 `d.planController`（保留）。

- [x] **Step 2: 编译 + 包内测试**

Run: `CGO_ENABLED=1 go build -tags sqlite_fts5 ./... && CGO_ENABLED=1 go test -tags "sqlite_fts5 test" ./internal/swarm/server/adapter/`
Expected: PASS

- [x] **Step 3: 提交**

```bash
git add internal/swarm/server/adapter/
git commit -m "refactor(adapter): 删除 plan 状态机接入与控制事件缓冲（已上移 AgentRuntime）"
```

---

### Task 10: 集成用例迁移 + 文档登记

**Files:**
- Modify: `tests/integration/swarm/server/adapter/attachment_mode/*.go`（改从 server 入口驱动）
- Create: `tests/integration/swarm/server/adapter/attachment_mode/envelope_integrity_test.go`
- Create: `tests/integration/swarm/server/adapter/attachment_mode/plan_event_test.go`
- Modify: `internal/swarm/runtime/doc.go`、`internal/swarm/server/doc.go`、`internal/swarm/server/runtime/doc.go`
- Modify: `IMPLEMENTATION_PLAN.md`（24.12 行）、`docs/sync/defect-plan-2026-11-04.md`（H-02）

- [x] **Step 1: 迁移 IT-S2-3~7**

把 `SendPlanRequest` 走 `AgentServer.handleStream`（经 `testhelpers/plan_env.go` 的 env 提供 `AgentServer` 实例），断言改为「state.PlanMode + 控制事件帧」。

- [x] **Step 2: 新增信封完整性用例**

```go
// TestIT_S3_1_信封完整性 进 plan 首轮 user 消息同时含原文与信封字段。
func (s *PlanStateSuite) TestIT_S3_1_信封完整性() {
	env := newPlanEnv(s.T())
	env.SendPlanRequest("sess-1", "unique-marker 请规划", "plan_toggle")

	msg := env.LastUserMessage()
	s.Require().Contains(msg, "unique-marker")
	s.Require().Contains(msg, `"preferred_response_language"`)
}
```

- [x] **Step 3: 新增控制事件用例**

```go
// TestIT_S3_2_退出事件下发 退出 plan 后客户端收到 plan_mode_exited 帧。
func (s *PlanStateSuite) TestIT_S3_2_退出事件下发() {
	env := newPlanEnv(s.T())
	env.SendPlanRequest("sess-1", "计划", "plan_toggle")
	env.SendPlanRequest("sess-1", "退出", "")
	ev := env.LastPushPayload()
	s.Require().Equal("plan_mode_exited", ev["event_type"])
	s.Require().Equal("agent.work.normal", ev["mode"])
}
```

- [x] **Step 4: 跑集成与审计**

Run:
```bash
CGO_ENABLED=1 go test -tags "sqlite_fts5 test integration" ./tests/integration/swarm/server/... -count=1
go run ./cmd/integration-audit -verify
```
Expected: PASS，审计 0 违规

- [x] **Step 5: 文档登记**

- `doc.go` 文件树：新增 `service.go`、`events.go`；server 侧新增 `handle_message.go`/`handle_chat.go`/`keepalive.go`/`handle_session_plan.go`
- `IMPLEMENTATION_PLAN.md` 领域二十四加 `24.12 ✅ Plan/Runtime 层对齐（S3）`
- 缺陷台账 H-02 更新落点与状态

- [x] **Step 6: 提交**

```bash
git add tests/integration internal/swarm/runtime/doc.go internal/swarm/server/doc.go \
        internal/swarm/server/runtime/doc.go IMPLEMENTATION_PLAN.md docs/sync/defect-plan-2026-11-04.md
git commit -m "test(itest): IT-S2-3~7 迁移到 server 入口 + 信封完整性/plan_mode_exited 用例；文档登记"
```

---

## 删除清单（规则 8 强制）

| 对象 | 位置 | 替代 |
|---|---|---|
| `ensurePlanState` | `server/adapter/deep_adapter_plan.go:51` | `AgentRuntime.Invoke/Stream` 调 `EnsureState` |
| `checkPlanPostProcess` | 同上 `:89` | `AgentRuntime.checkPlanExit` |
| `emitControlEvents`（adapter 版） | 同上 `:112` | `AgentRuntime.emitControlEvents` |
| `DrainControlEvents` | 同上 `:28` | `AgentServer.sendRuntimeEvent` |
| `controlEvents` / `controlMu` 字段 | `server/adapter/deep_adapter.go:281`、`:406` | 同上 |
| `resolveRequestQuery` | `server/adapter/deep_adapter.go:2898` | 单通道：`params["query"]` |
| `inputs["query"] = query` 覆盖 | `server/adapter/deep_adapter.go:980`、`:1235` | 由 `BuildInputs` 在改写后生成 |
| `resolveMode` | `server/handle_envelope.go:602` | `runtime.ResolveAgentRequestMode` |
| `applyResolvedModeToRequest` | 同上 `:660` | `runtime.ApplyResolvedModeToRequest` |
| `resolveModeFromRequest` | `server/runtime/agent_manager.go:803` | `runtime.ResolveAgentRequestMode` |
| `runKeepalive` | `server/handle_envelope.go:570` | `StreamKeepalive` |
| `handle_envelope.go` 文件名 | `server/` | `handle_message.go` + `handle_chat.go` |
| `uapclaw.go` / `UapClaw` | `server/runtime/` | `interface.go` / `UapClawSwarm` |
| `TestResolveModeFromRequest` 等旧断言 | `server/runtime/agent_manager_test.go` | 迁到 `runtime/request_test.go` |

## 验收

```bash
export GOPROXY=https://goproxy.cn,direct
CGO_ENABLED=1 go build -tags sqlite_fts5 ./...
CGO_ENABLED=1 go vet -tags "sqlite_fts5 test integration" ./...
CGO_ENABLED=1 go test -tags "sqlite_fts5 test" ./internal/swarm/...
CGO_ENABLED=1 go test -tags "sqlite_fts5 test integration" ./tests/integration/swarm/... -count=1
go run ./cmd/integration-audit -verify
```
再加：`grep -rn 'UapClaw\b\|handleEnvelope\|resolveMode\|DrainControlEvents' internal tests cmd` 为 0；推送 main 后 CI 四 job 全绿。

---

## 附录 A：剩余收尾工作（2026-11-04 交接）

> 当前状态：Task 1-3、4（最小接线）、5、6、8 已完成并本地提交（未推送）。以下三项待做，
> 做完再一次性推 main 跑 CI（用户 2026-11-04 决定）。

### A.1 Task 10 脚手架清理（必须删，规则 8）

现状：`tests/integration/testhelpers/plan_env.go` 的 `syncPlanState`/`checkPlanExit`/`recordEvents`
与 `tests/integration/swarm/server/adapter/mode_switch_todo_test.go` 的 `runTurn` 里，按
`AgentRuntime` 的步骤手工补了 plan 同步（因为这两个 IT 直连 adapter）。

目标：从 **AgentServer 入口** 驱动，删掉这段模拟。

步骤：
1. testhelpers 新增 `ServerEnv`（`NewAgentServer(cfg, transport.NewChannelTransport())` +
   `Start(ctx)` + `SendPush` 捕获）：暴露 `Send(method, params, sessionID)` 与
   `Frames()`（读 transport `RecvCh`）；`Push()` 返回最近一次 push payload。
2. `plan_env.go`：删除 `syncPlanState`/`checkPlanExit`/`recordEvents`/`lastEnsureQuery`/`events` 字段，
   `Send` 改走 `ServerEnv`；`Events()` 改读 `Push()` 里的 `payload`。
3. `mode_switch_todo_test.go`：`runTurn` 删除手工 `EnsureState`/`CheckPostProcessExit`，
   改走 `ServerEnv.Send`。
4. 验收：`grep -rn "syncPlanState\|checkPlanExit\|lastEnsureQuery" tests/` 为 0；
   `go test -tags "sqlite_fts5 test integration" ./tests/integration/swarm/server/...` 全绿。

### A.2 Task 2c 欠账：`CancelRequest`

Python `jiuwenswarm/runtime/request.py:578-652`：`cancel_request(agent_manager, request, allow_create=False)`
→ 先 `get_agent_for_session_nowait`，取不到且 `allow_create` 才 `get_agent`；随后
`agent.process_message(request)`。

Go 现状：`handle_envelope.go` 的 `handleCancel` 内联三层 fallback（`GetAgentNoWait` ×2 → `GetAgent`）。
落点：`internal/swarm/runtime/request.go` 新增 `CancelRequest(ctx, mgr, req, allowCreate)`，把三层
fallback 上移；`handleCancel` 改为调用它。`AgentManagerPort` 需补 `GetAgentNoWait(channelID, mode, projectDir, subMode)`。

### A.3 Task 2c 欠账：`SyncChatRequestMetadata`

Python `jiuwenswarm/runtime/request.py:152-266`：写 `mode/model/project_dir/project_id/cron_id/
user_id/last_user_message_at/work_mode/persist_session`，并有 attach-only 只读分支。

Go 阻塞点：`session.SessionMetadataUpdate` 只有 `Mode/ChannelID/UserID/Title/ChannelMetadata/TeamName`。
需先扩字段（`ProjectDir *string`、`WorkMode *string`、`Model *string`、`LastUserMessageAt *float64`），
再实现 `SyncChatRequestMetadata` 并在 `PrepareChatTurn` 的 `syncMetadata=true` 路径调用。

### A.4 推送前必做

```bash
export GOPROXY=https://goproxy.cn,direct
CGO_ENABLED=1 go build -tags sqlite_fts5 ./...
CGO_ENABLED=1 go vet -tags "sqlite_fts5 test integration" ./...
CGO_ENABLED=1 go test -tags "sqlite_fts5 test" ./internal/...
CGO_ENABLED=1 go test -tags "sqlite_fts5 test integration" ./tests/integration/swarm/...
go run ./cmd/integration-audit -verify
grep -rn "syncPlanState\|checkPlanExit\|lastEnsureQuery\|DrainControlEvents\|resolveRequestQuery\|UapClaw\b" internal tests cmd
```
最后一条为 0 才可推送；推送后盯 CI 四 job 全绿（Build / Unit / Integration / Lint）。

---

## 附录 B：2026-11-04 收尾执行记录（inline execution）

> 执行方式：按 `superpowers:executing-plans`（inline，不派子 agent）。逐任务对照
> `jiuwenswarm/runtime/{request,plan,service,events}.py`、
> `jiuwenswarm/server/{agent_ws_server.py,handlers/session.py}`、
> `gateway/common/mode_matrix.py` 复核并落地；先审核已完成的 Task 1~9，再补 A.1~A.3。

### B.1 审核中发现并修正的偏差（Task 1~9 回看）

| # | 问题 | 上游依据 | 处置 |
|---|---|---|---|
| 1 | plan 激活提醒只算不写：`PlanStateResult.Query` 被丢弃，`params["query"]` 与 `plan_reminder_original_query` 均未回写 → 提醒整段丢失，S3 目标 4 落空 | `plan.py:151-175 inject_activation_reminder` 直接改 `request.params` | 新增 `ApplyPlanResult`，`AgentRuntime.ensurePlanState` 回写 query + 原文键 |
| 2 | 阻断进入分支未回写 `params["mode"]=exit_mode` | `plan.py:230-234` | `PlanStateResult.ModeOverride` + `ApplyPlanResult` |
| 3 | `check_post_process_exit` 未把 canonical mode 回写 params | `plan.py:270-272` | 新增 `ApplyCanonicalMode`，`AgentRuntime.checkPlanExit` 首步调用 |
| 4 | `ResolveAgentRequestMode` 用 `CanonicalizeMode` 做首步归一（含 DEPRECATION_MAP），且缺 `agent.plan` / `team.plan.normal` / `team.plan.code` 显式分支与末尾 work_mode 重折算 | `request.py:266-328`、`mode_matrix.canonicalize_mode_text` | 改用 alias-only 归一 + 三条 legacy 分支 + 末尾重折算；期望值由上游 `mode_matrix` 实算核对 |
| 5 | `ApplyResolvedModeToRequest` / `ResolveRequestRuntimeModeFromRequest` 缺少 Web 组合分支（`mode+work_mode`），如 `agent.plan + code` 会丢 plan、`team + code` 会掉出 code-team | `mode_matrix.resolve_request_mode` + `_WEB_MODE_TABLE` | schema 新增 `CanonicalizeModeText/ResolveNewCanonicalModeText/IsWebComposableMode/ComposeWebMode`；runtime 新增 `resolveRequestMode` 先组合后 legacy |
| 6 | 控制器归属与决策不符：controller 建在 adapter 上、`AgentRuntime` 无 `PlanController` | spec §6.0（AgentServer 持有、注入 runtime）；Python 模块级 `_SERVER_PLAN_CONTROLLER` | controller 上移到 `AgentServer` 并注入 `AgentRuntime`；删除 `adapter.planController/PlanController()/ResetPlanSession`、`UapClawSwarm.PlanController/ResetPlanSession`、`AgentManager.ResetSessionPlanState`（规则 8） |
| 7 | `session.plan_status` 契约键错：返回 `plan_mode/mode`，前端读的是 `payload.in_plan`；且缺 BAD_REQUEST/NOT_FOUND、team 会话会被同 session 的 DeepAgent 默认 normal 误判 | `handlers/session.py:240-283`、`_combine_session_in_plan`、上游前端 `App.tsx` 读 `in_plan` | 改为 `{session_id, in_plan}` + 两类错误码 + team 忽略 live + 活跃集合判据 |
| 8 | `session.plan_status` 常量未进 `AllReqMethods()` → `E2A_CONVERT_ERROR`（RPC 实际不可达） | — | 注册 `ReqMethodSessionPlanStatus` |
| 9 | Task 7 未落：`sendRuntimeEvent` 不存在、`writeChunk/writeResponse` 无返回值、chunk 发送失败不停止流、控制事件占 sequence、流任务无条件登记 | `agent_ws_server.py:5024 _send_runtime_event`、`:4654 _handle_stream_impl` | 拆 `handle_message.go`/`handle_chat.go`；`sendToGateway/writeResponse/writeChunk` 返回 bool；发送失败 `Warn + 停止流`；控制事件用当前 `chunkCount`；登记条件改 `!UsesSessionRuntime` |
| 10 | Task 4/5/6 无单测（覆盖率门禁风险） | 计划 Step 1 要求 | 补 `runtime/service_test.go`、`server/keepalive_test.go`、`server/handle_session_plan_test.go`、`runtime/request_test.go` 的 metadata/取消用例 |

### B.2 A.1~A.3 执行结果

- **A.1 脚手架清理**：新增 `tests/integration/testhelpers/server_env.go`（`ServerEnv`：E2A 信封投递 +
  帧分类 + `AwaitUnary/AwaitStreamEnd/Pushes/LastPushPayload/PushPayload`）；`plan_env.go` 改为
  **AgentServer 入口驱动**，删除 `syncPlanState/checkPlanExit/recordEvents/lastEnsureQuery/events`；
  `mode_switch_todo_test.go` 的 `runTurn` 删除手工 `EnsureState/CheckPostProcessExit`，改走
  `ServerEnv.SendStream`。新增 IT：`envelope_integrity_test.go`（IT-S3-1 信封完整性）、
  `plan_event_test.go`（unary/stream 两例 `plan_mode_exited` + `session.plan_status` 一致性）。
  验收：`grep -rn "syncPlanState\|checkPlanExit\|lastEnsureQuery" tests/` 为 0。
- **A.2 `CancelRequest`**：`runtime/request.go` 新增（mode 命中 → 无 mode 兜底 → `allowCreate`
  才新建），`handleCancel` 改为调它；`AgentManagerPort` 增 `GetChatAgentNoWait`（server 侧
  `AgentManager.GetChatAgentNoWait`）。偏差 ⤵️：上游首步 `get_agent_for_session_nowait` 依赖
  session→agent 注册表，Go 以 (channel,mode,subMode,projectDir) 为键、无该注册表，分支缺席。
- **A.3 `SyncChatRequestMetadata`**：`session/session_request_metadata.go` 新增
  `SyncSessionRequestMetadata`（project_dir/project_id/cron_id/user_id/channel_id/work_mode 首次锁定、
  model/mode 显式覆盖、last_user_message_at 覆盖、last_message_at 仅 chat 轮次、persist_session
  创建期锁定）；`runtime.SyncChatRequestMetadata` 负责请求级取值（attach-only 只读分支、
  `model_name`/`eternal_conversation_enabled` 解析、跨会话方法判定）；`PrepareChatTurn(..., syncMetadata)`
  在 `sync_metadata=true` 路径调用并回写 project_dir 到 params/metadata，随后做 persist_session 锁定告警。
  偏差 ⤵️：`_repair_session_project_dir`、bound project_id 绑定、`is_agentos_workspace_fallback`
  未移植（Go 无工程注册表 / AgentOS 工作区回退语义）。

### B.3 本轮新增的已知偏差（写在此处避免重复讨论）

1. **agent 缓存键含 sub_mode**：`plan` 与 `normal` 轮会命中不同 `UapClawSwarm` 实例，因此
   `mode_instructions` 附件（per-agent 管理器）不跨模式共享。plan 状态本身持久化在会话状态里，
   两轮都能读到正确状态；附件由每轮 `BeforeModelCall` 重新注入，功能不受影响。集成断言因此
   按「本轮实际承接的 agent」取实例（`PlanEnv.Inst()`）。
2. **code 族 IT 仍直连 CodeAdapter**：`TestIT_S2_11_code提醒节奏` 验证 code rail 提醒节奏，
   与 plan/runtime 层无关；其配置显式把 `modes.code.tools` 置空，走 AgentServer 会拿不到 todo 工具
   （`HasTodoTool=false` → 不再计数）。改为自建 `codeEnv` 直连，并在用例注释说明口径。
3. **流任务登记始终执行**：上游只在 `targeted_expert or not uses_session_runtime` 时登记
   `_session_stream_tasks`，取消走 adapter interrupt；Go 的取消链路依赖该登记
   （`handleCancel → cancelStreamTask`），按上游条件收窄会让 supplement/cancel 在取消后
   把 `chat.error` 写进历史（`TestSupplementInterruptSuite` 已复现），故始终登记。
4. **`handle_session_plan` 磁盘优先 + 缓存兜底**：Python `cache_bust=True` 只读盘；Go 的 metadata
   写入是异步队列，同一轮 chat 刚结束时存在短暂未落盘窗口，故磁盘为空时回退内存缓存。

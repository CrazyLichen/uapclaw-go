# 27.2 / 27.10 Goal 前端契约全量验收 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 在 `tests/integration/swarm/web/` 下补齐 Goal 后端出站契约与前端假定的端到端验收用例，把 Web GoalBar 可用的最后一环锁住。

**Architecture:** 每个 suite 用 `testhelpers.ServerEnv`（`transport.NewChannelTransport` + 进程内 `AgentServer`）经完整 E2A 链路投递 `command.goal`，收集一元响应 / 流式全帧 / push 帧，断言字段形状、事件顺序、租约重连与无人值守状态机。不新建生产目录、不改生产代码；若用例暴露契约不符，按规则 13.2 先报缺口再由用户决策。

**Tech Stack:** Go 1.22、`testify/suite`、`//go:build integration`（真 LLM 冒烟为 `integration && llm`）、`testhelpers.ServerEnv` / `AdapterTestEnv`、`tests/integration/mockllm`、`cmd/integration-audit`。

**设计来源：** `docs/superpowers/specs/2026-11-09-goal-web-contract-27.2-27.10-design.md`（提交 `37f59d040`，已由用户 review 通过）。

---

## 前置事实（实施前必读，均为实测）

1. **一元 vs 流式分两路**：`webClient.ts:892 requestGoalAction(get|pause|clear)` 走一元 `webRequest` → `ServerEnv.AwaitUnary`；`webClient.ts:916 sendGoalStreamCommand(set|resume)` 走 `is_stream:true` 且不等 res → `ServerEnv.AwaitStreamEnd`。
2. **`goal.snapshot` 只在流式路径出现**：`internal/swarm/server/runtime/interface.go:1511-1519` 的 `processMessageStreamInternal` 返回单帧 `goal.snapshot`（`WithChunkIsComplete(true)`）；一元 `get` 由 `runtime/interface.go` 门面走 `runMessageDirect`，空会话回 `{ok:true, message:"No goal in this session."}`（见 `internal/swarm/server/runtime/interface_goal_shortcircuit_test.go:39-62`）。**spec §4 #1 的"单帧 goal.snapshot"按 is_stream 分两路断言**，不要写成一元也发帧。
3. **set/resume 不短路**：`interface_goal_shortcircuit_test.go:87-108` 已验证 `action=set` 必须继续走适配器流式链；门面短路只对 `get` 及缺省 action 生效（`:4180` `if action not in {"set","resume"}`）。
4. **`GoalRecord` 是 15 字段**（Go `goal/schema.go:337-359` = Python `schema.py:210-227` 逐字一致），不是 17；`created_at`/`updated_at` 后端确实下发，前端 `types/goal.ts:32` 注释是历史文档滞后，**不改代码**。
5. **前端实际发 RPC 只有 2 个函数**（`webClient.ts:892` / `:916`）；`command.goal` 字面量 16 处分布在 9 个文件，其余 14 处是注释/类型/状态标记。`set` 恒带 `overwrite_confirmed:true`，故 `goal.confirm_required` 需后端直接构造未确认请求覆盖。
6. **两处疑点已核实非缺口**：`deep_agent.go:1693-1698` 在 `AttachOutput` 持锁内补排 ACTIVE goal work（对齐 `deep_agent.py:4065-4069`）；`schema/interaction.go:443-456` `Detach` token 不匹配返回 false 不摘新流。
7. **不要锁 `goal.updated` 与 `chat.delta`/`chat.final` 的相对顺序**：前端 `useWebSocket.ts:269-270` 明说后端不保证到达顺序，上游亦无保证；只断言两者都最终到达。
8. **工作区有并发会话**：当前存在 `25.1e-turn-usage-attribution` 等未提交改动。每个 Task 的 `git add` 必须**显式列本 Task 的文件**，禁止 `git add .` / `git add -A`。

## 文件结构

```
tests/integration/swarm/web/                      # 新建目录
├── doc.go                                       # 包文档（//go:build integration, package web_test）
├── goal_contract_helpers_test.go                 # 共用：suite 基类 + 帧断言助手 + 15 字段校验
├── goal_rpc_contract_test.go                     # G1：8 组契约对拍
├── goal_event_order_test.go                      # G2：O1-O5
├── goal_reconnect_test.go                        # G3：R1-R4
├── goal_unattended_test.go                       # G4 MockLLM 全链
└── goal_realllm_test.go                          # G4 真 LLM 冒烟（integration && llm）

tests/integration/testhelpers/doc.go              # 修改：文件树条目（若加 helper；本计划不加）
IMPLEMENTATION_PLAN.md                            # 修改：27.2 / 27.10 状态与证据
docs/sync/goal-web-contract-matrix.md             # 可选：仅当 Task 8 决定产出时创建
```

**共用助手为什么单独一个文件**：五个 suite 都要 `ServerEnv` 隔离、帧索引、15 字段断言、事件名常量。放在 `goal_contract_helpers_test.go` 一次定义，避免每个文件重复（DRY）；它不是生产代码，不进覆盖率基线。

---

### Task 1: 目录骨架 + 共用断言助手（TDD 起步）

**Files:**
- Create: `tests/integration/swarm/web/doc.go`
- Create: `tests/integration/swarm/web/goal_contract_helpers_test.go`
- Test: 同文件内 `TestGoalContractHelpersSuite`（自测助手本身）

- [ ] **Step 1: 写失败的自测用例**

创建 `tests/integration/swarm/web/goal_contract_helpers_test.go`：

```go
//go:build integration

package web_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/uapclaw/uapclaw-go/internal/swarm/schema"
)

// ──────────────────────────── 结构体 ────────────────────────────

// goalFrames 收集一次流式请求的全部帧，供顺序与形状断言复用。
type goalFrames []*schema.AgentResponseChunk

// ──────────────────────────── 常量 ────────────────────────────

const (
	eventGoalSnapshot       = "goal.snapshot"
	eventGoalUpdated        = "goal.updated"
	eventGoalConfirmRequire = "goal.confirm_required"
	eventRuntimeAccepted    = "runtime.accepted"
	eventExecutionError     = "execution.error"
	eventChatFinal          = "chat.final"
	eventChatDelta          = "chat.delta"
	eventChatError          = "chat.error"
	fieldGoalIntermediate   = "goal_intermediate"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// eventTypeOf 取帧的业务事件名；非字符串返回空串。
func eventTypeOf(c *schema.AgentResponseChunk) string {
	if c == nil || c.Payload == nil {
		return ""
	}
	v, _ := c.Payload["event_type"].(string)
	return v
}

// indexOfEvent 返回首个指定事件帧的下标，不存在返回 -1。
func indexOfEvent(frames goalFrames, event string) int {
	for i, c := range frames {
		if eventTypeOf(c) == event {
			return i
		}
	}
	return -1
}

// countEvent 统计指定事件帧数量。
func countEvent(frames goalFrames, event string) int {
	n := 0
	for _, c := range frames {
		if eventTypeOf(c) == event {
			n++
		}
	}
	return n
}

// requireNumeric 接受 JSON 反序列化的 float64 与 Go 直构的 int 两种数值。
func requireNumeric(t *testing.T, v any, field string) {
	t.Helper()
	switch v.(type) {
	case float64, float32, int, int64, int32:
	default:
		t.Fatalf("字段 %s 必须是数值，实际 %T (%v)", field, v, v)
	}
}

// requireGoalRecord 断言 GoalRecord 的 15 字段形状。
// 对照 Python: openjiuwen/harness/goal/schema.py:210-227 (to_dict)
// 对照 Go:    internal/agentcore/harness/goal/schema.go:337-359 (ToDict)
// 用白名单 + 类型断言，不用 golden JSON 全等（goal_id 随机、时间戳变化）。
func requireGoalRecord(t *testing.T, raw any) map[string]any {
	t.Helper()
	rec, ok := raw.(map[string]any)
	require.True(t, ok, "goal 必须是对象，实际 %T", raw)

	for _, key := range []string{
		"goal_id", "session_id", "objective", "status",
		"created_at", "updated_at",
	} {
		require.IsType(t, "", rec[key], "字段 %s 必须是字符串", key)
	}
	for _, key := range []string{"revision", "attempt_count", "time_used_seconds"} {
		requireNumeric(t, rec[key], key)
	}
	require.Contains(t, []string{"active", "paused", "completed", "blocked"},
		rec["status"], "status 必须属 4 值枚举")

	usage, ok := rec["token_usage"].(map[string]any)
	require.True(t, ok, "token_usage 必须是对象")
	for _, key := range []string{
		"input_tokens", "output_tokens", "cached_input_tokens", "total_tokens",
	} {
		requireNumeric(t, usage[key], "token_usage."+key)
	}

	for _, key := range []string{"max_attempts", "token_budget"} {
		if rec[key] != nil {
			requireNumeric(t, rec[key], key)
		}
	}
	for _, key := range []string{"last_stop_reason", "active_started_at"} {
		if rec[key] != nil {
			require.IsType(t, "", rec[key], "字段 %s 必须是字符串或 null", key)
		}
	}
	if rec["last_assessment"] != nil {
		a, ok := rec["last_assessment"].(map[string]any)
		require.True(t, ok, "last_assessment 必须是对象或 null")
		require.IsType(t, "", a["status"], "last_assessment.status 必须是字符串")
		require.Contains(t, []string{"continue", "complete", "blocked"}, a["status"])
		require.IsType(t, "", a["evidence"], "last_assessment.evidence 必须是字符串")
		for _, key := range []string{"remaining_work", "next_instruction"} {
			if a[key] != nil {
				require.IsType(t, "", a[key], "last_assessment.%s 必须是字符串或 null", key)
			}
		}
	}
	return rec
}

// ──────────────────────────── 测试 ────────────────────────────

// TestGoalContractHelpersSuite 自测助手：索引、计数、15 字段校验的红绿基线。
func TestGoalContractHelpersSuite(t *testing.T) {
	frames := goalFrames{
		{Payload: map[string]any{"event_type": eventGoalSnapshot}},
		{Payload: map[string]any{"event_type": eventRuntimeAccepted}},
		{Payload: map[string]any{"event_type": eventGoalUpdated}},
	}
	require.Equal(t, 0, indexOfEvent(frames, eventGoalSnapshot))
	require.Equal(t, 1, indexOfEvent(frames, eventRuntimeAccepted))
	require.Equal(t, -1, indexOfEvent(frames, eventGoalConfirmRequire))
	require.Equal(t, 1, countEvent(frames, eventGoalUpdated))

	record := map[string]any{
		"goal_id": "abc123def456", "session_id": "s1", "objective": "修完测试",
		"status": "active", "created_at": "2026-11-09T00:00:00+00:00",
		"updated_at": "2026-11-09T00:00:00+00:00",
		"revision": float64(0), "attempt_count": float64(1), "time_used_seconds": float64(3),
		"token_usage": map[string]any{
			"input_tokens": float64(10), "output_tokens": float64(5),
			"cached_input_tokens": float64(0), "total_tokens": float64(15),
		},
		"max_attempts": nil, "token_budget": nil,
		"last_assessment": nil, "last_stop_reason": nil, "active_started_at": nil,
	}
	requireGoalRecord(t, record)
}
```

- [ ] **Step 2: 建包文档**

创建 `tests/integration/swarm/web/doc.go`（规则 1：目录须有 doc.go；`tests/integration` 下用基准包名 `web_test`，照 `tests/integration/swarm/server/adapter/doc.go` 先例）：

```go
//go:build integration

// Package web_test 提供 Web 控制台后端契约的集成测试。
//
// 覆盖 27.2 / 27.10：command.goal 五 action 的 RPC 契约快照、goal 事件流
// 顺序与形状、断线重连补发、无人值守目标自动续跑到终态的全链，以及
// 真 LLM 冒烟（integration && llm）。所有用例经 testhelpers.ServerEnv 走
// 完整 E2A 链路（WebChannel → Gateway → AgentServer → runtime → adapter
// → agentcore/harness），不直接调用 handler。
//
// 文件目录：
//
//	web/
//	├── doc.go                            # 包文档（本文件）
//	├── goal_contract_helpers_test.go      # 共用 suite 基类 + 帧断言助手 + 15 字段校验
//	├── goal_rpc_contract_test.go          # G1：8 组 RPC/事件契约对拍
//	├── goal_event_order_test.go           # G2：O1-O5 事件顺序与形状
//	├── goal_reconnect_test.go             # G3：R1-R4 断线/重连/切会话
//	├── goal_unattended_test.go            # G4 MockLLM 无人值守全链
//	└── goal_realllm_test.go               # G4 真 LLM 冒烟（integration && llm）
//
// 对应 Python 代码：jiuwenswarm/server/runtime/agent_adapter/interface_deep.py
// · jiuwenswarm/server/agent_ws_server.py · openjiuwen/harness/goal/
package web_test
```

- [ ] **Step 3: 运行自测，确认绿**

Run: `cd /home/opensource/uap-claw-go && CGO_ENABLED=1 go test -tags "sqlite_fts5 test integration" ./tests/integration/swarm/web/ -run TestGoalContractHelpersSuite -v`
Expected: `PASS` + `--- PASS: TestGoalContractHelpersSuite`。助手里的 `requireNumeric` 同时接受 float64（WS/JSON 帧）与 int（Go 直构 map），两种来源都不会假红。

- [ ] **Step 4: 编译 + vet**

Run: `cd /home/opensource/uap-claw-go && go build ./... && go vet ./tests/integration/swarm/web/`
Expected: 两者均 0 输出、退出码 0。

- [ ] **Step 5: 提交**

```bash
cd /home/opensource/uap-claw-go
git add tests/integration/swarm/web/doc.go tests/integration/swarm/web/goal_contract_helpers_test.go
git commit -m "test(27.2/27.10): Goal 前端契约验收目录骨架 + 共用帧断言助手"
```

---

### Task 2: G1 契约矩阵辅助 —— suite 基类与环境隔离

**Files:**
- Modify: `tests/integration/swarm/web/goal_contract_helpers_test.go`（追加 suite 基类与发包助手）

- [ ] **Step 1: 追加基类与发包助手（先写用例要用到的部分）**

在 `goal_contract_helpers_test.go` 的「结构体」区块追加（保持文件内声明顺序：结构体 → 常量 → 导出函数 → 非导出函数 → 测试，按仓库 `go-code-conventions.md` 规范 2）：

```go
// goalContractSuite 所有 G1-G4 suite 的公共基类：进程内 AgentServer + 每用例隔离数据目录。
type goalContractSuite struct {
	isuite.BaseIntegrationSuite
	env *testhelpers.ServerEnv
}
```

在「导出函数」区块追加：

```go
// newGoalContractEnv 启动进程内 AgentServer。
// 签名只依赖 *testing.T，不绑定具体 suite —— G3 R1 用 AgentSuite 派生
// 的 suite 也能复用，避免后半程改签名波及所有调用点。
// 照 tests/integration/swarm/server/goal_dispatch_test.go:40-45 先例。
func newGoalContractEnv(t *testing.T) *testhelpers.ServerEnv {
	t.Helper()
	cfg, err := config.New("")
	require.NoError(t, err)
	return testhelpers.NewServerEnv(t, cfg, nil)
}

// isolateGoalDataDir 每个用例隔离 UAPCLAW_DATA_DIR 与会话缓存。
// 同样只依赖 *testing.T，供 BaseIntegrationSuite / AgentSuite 派生的套件共用。
// 照 goal_dispatch_test.go:48-60：TearDown 先 FlushMetadataQueue 再清缓存。
func isolateGoalDataDir(t *testing.T) {
	t.Helper()
	t.Setenv(pathutil.EnvDataDir, t.TempDir())
	pathutil.ResetCache()
	sessionpkg.ClearAllSessionMetadataCache()
}

// sendGoal 投递 command.goal，返回 request_id。
// isStream=false 对应 webClient.ts:892 requestGoalAction；
// isStream=true  对应 webClient.ts:916 sendGoalStreamCommand。
func sendGoal(s *goalContractSuite, sid string, params map[string]any, isStream bool) string {
	if params == nil {
		params = map[string]any{}
	}
	if _, ok := params["mode"]; !ok {
		params["mode"] = "agent"
	}
	return s.env.Send(string(schema.ReqMethodCommandGoal), params, sid, isStream)
}
```

导入追加（合并到 Task 1 文件的顶部 import 块），**逐字复制** `tests/integration/swarm/server/goal_dispatch_test.go:8-16` 的模块路径（`github.com/uapclaw/uapclaw-go`，无连字符）：

```go
	"github.com/uapclaw/uapclaw-go/internal/common/config"
	isuite "github.com/uapclaw/uapclaw-go/tests/integration/suite"
	"github.com/uapclaw/uapclaw-go/tests/integration/testhelpers"
	pathutil "github.com/uapclaw/uapclaw-go/internal/common/utils/path"
	sessionpkg "github.com/uapclaw/uapclaw-go/internal/swarm/server/session"
	"github.com/uapclaw/uapclaw-go/internal/swarm/schema"
```

> `goal_contract_helpers_test.go` 顶部原本已有 `testing` / `require` / `schema`；`config`、`pathutil`、`sessionpkg`、`isuite`、`testhelpers` 是本次新增。合并后 gofmt 会自动排序，不要手写分组顺序。

- [ ] **Step 2: 编译验证**

Run: `cd /home/opensource/uap-claw-go && CGO_ENABLED=1 go test -tags "sqlite_fts5 test integration" ./tests/integration/swarm/web/ -run TestGoalContractHelpersSuite -v`
Expected: `PASS`（本 Task 只加基类，不改现有断言）。

- [ ] **Step 3: 提交**

```bash
cd /home/opensource/uap-claw-go
git add tests/integration/swarm/web/goal_contract_helpers_test.go
git commit -m "test(27.2/27.10): 补 Goal 契约 suite 基类与 E2A 发包助手"
```

---

### Task 3: G1 —— 8 组 RPC/事件契约对拍

**Files:**
- Create: `tests/integration/swarm/web/goal_rpc_contract_test.go`

- [ ] **Step 1: 写失败用例（先写 #1/#4/#5/#7 四条一元路径）**

创建 `tests/integration/swarm/web/goal_rpc_contract_test.go`：

```go
//go:build integration

package web_test

import (
	"testing"

	"github.com/stretchr/testify/suite"
)

// ──────────────────────────── 结构体 ────────────────────────────

// GoalRPCContractSuite G1：command.goal 出站契约与前端 webClient.ts 读法逐条对拍。
type GoalRPCContractSuite struct {
	goalContractSuite
}

// ──────────────────────────── 导出函数 ────────────────────────────

func TestGoalRPCContractSuite(t *testing.T) {
	suite.Run(t, new(GoalRPCContractSuite))
}

func (s *GoalRPCContractSuite) SetupTest() {
	s.env = newGoalContractEnv(s.T())
	isolateGoalDataDir(s.T())
}

func (s *GoalRPCContractSuite) TearDownTest() {
	sessionpkg.FlushMetadataQueue()
	sessionpkg.FlushHistoryQueue()
	sessionpkg.ClearAllSessionMetadataCache()
	pathutil.ResetCache()
}

// #1 一元 get：空会话是正常态，不写 user 轮历史。
// Python: agent_ws_server.py:3717（只读、不兜底新建 metadata）
// 前端: webClient.ts:892 requestGoalAction('get') 读 payload.goal ?? payload.record
func (s *GoalRPCContractSuite) TestGet_空会话正常态且不写历史() {
	sid := "web-goal-get-empty"
	rid := sendGoal(s, sid, map[string]any{"action": "get"}, false)
	resp := s.env.AwaitUnary(rid)
	s.Require().NotNil(resp)
	s.True(resp.OK, "空会话 get 属正常态")
	s.Equal("No goal in this session.", resp.Payload["message"])
	records, err := sessionpkg.ReadHistoryRecords(sid)
	s.Require().NoError(err)
	s.Empty(records, "只读 goal 查询不得写 user 轮历史")
}

// #1b 流式 get：门面短路为单帧 goal.snapshot（is_complete=true）。
// Python: interface_deep.py:19358-19366；Go: runtime/interface.go:1511-1519
func (s *GoalRPCContractSuite) TestGet_流式单帧Snapshot() {
	sid := "web-goal-get-stream"
	rid := sendGoal(s, sid, map[string]any{"action": "get"}, true)
	frames := goalFrames(s.env.AwaitStreamEnd(rid))
	s.Require().NotEmpty(frames)
	s.Equal(eventGoalSnapshot, eventTypeOf(frames[0]), "流式 get 首帧应为 goal.snapshot")
	s.True(frames[len(frames)-1].IsComplete, "单帧必须带 is_complete=true")
	s.Contains(frames[0].Payload, "goal", "帧必须带 goal 字段（前端 applyGoalSnapshot 读取）")
}

// #4 pause 空会话：控制响应，不 panic。
// Python: interface_deep.py:17929 goal_control
func (s *GoalRPCContractSuite) TestPause_空会话返回控制响应() {
	sid := "web-goal-pause-empty"
	rid := sendGoal(s, sid, map[string]any{"action": "pause"}, false)
	resp := s.env.AwaitUnary(rid)
	s.Require().NotNil(resp)
	s.True(resp.OK)
	s.Contains(resp.Payload, "message", "控制响应须有 message（前端 reportFailure 兜底读 message）")
}

// #5 clear 空会话：goal 视为 null，前端 webClient.ts:898-900 直接返回 null。
func (s *GoalRPCContractSuite) TestClear_空会话不报错() {
	sid := "web-goal-clear-empty"
	rid := sendGoal(s, sid, map[string]any{"action": "clear"}, false)
	resp := s.env.AwaitUnary(rid)
	s.Require().NotNil(resp)
	s.True(resp.OK)
	if g, present := resp.Payload["goal"]; present {
		s.Nil(g, "clear 后 goal 应为 null")
	}
}

// #7 未知 action：结构化报错，前端 reportFailure 读 message。
// Python: interface_deep.py:17900
func (s *GoalRPCContractSuite) TestUnknownAction_返回invalid_action() {
	sid := "web-goal-bad"
	rid := sendGoal(s, sid, map[string]any{"action": "explode"}, false)
	resp := s.env.AwaitUnary(rid)
	s.Require().NotNil(resp)
	s.False(resp.OK)
	s.Equal("invalid_action", resp.Payload["code"])
}
```

> **import 修正**：上面用了 `sessionpkg` / `pathutil` 别名，文件顶部 import 块照 `goal_dispatch_test.go:8-16` 逐字复制：
> ```go
> import (
> 	"testing"
>
> 	"github.com/stretchr/testify/suite"
>
> 	pathutil "github.com/uapclaw/uapclaw-go/internal/common/utils/path"
> 	sessionpkg "github.com/uapclaw/uapclaw-go/internal/swarm/server/session"
> )
> ```

- [ ] **Step 2: 跑起来看红/绿，记录哪些需要调整**

Run: `cd /home/opensource/uap-claw-go && CGO_ENABLED=1 go test -tags "sqlite_fts5 test integration" ./tests/integration/swarm/web/ -run TestGoalRPCContractSuite -v`
Expected: 至少 5 条 PASS。若某条 FAIL，**先把失败信息完整贴给用户**再判断：属测试假设错误则改断言（并注明源码依据）；属后端契约不符则按规则 13.2 报缺口（Python 行号 + 影响面 + 三档建议），**不得默默改生产代码**。

- [ ] **Step 3: 补 #2/#3/#6/#8 四条（set/resume/覆盖确认/execution.error）**

在 `goal_rpc_contract_test.go` 末尾追加：

```go
// #2 set 流式：必须进入适配器流式链，不被门面单帧短路。
// Python: interface_deep.py:19880-19892；Go: interface_goal_shortcircuit_test.go:87-108
func (s *GoalRPCContractSuite) TestSet_进入流式链且首帧为Snapshot() {
	sid := "web-goal-set"
	rid := sendGoal(s, sid, map[string]any{
		"action": "set", "objective": "修完测试", "overwrite_confirmed": true,
	}, true)
	frames := goalFrames(s.env.AwaitStreamEnd(rid))
	s.Require().NotEmpty(frames)
	// 无 goal manager 的环境会回 chat.error（既有基线）；有则回 goal.snapshot。
	// 两种都合法，但绝不能是「门面单帧短路后什么都没发生」。
	first := eventTypeOf(frames[0])
	s.Contains([]string{eventGoalSnapshot, eventChatError}, first,
		"set 必须进适配器链，首帧为 goal.snapshot 或 chat.error")
	if first == eventGoalSnapshot {
		s.False(frames[0].IsComplete, "流式 set 的 snapshot 是非终结帧")
	}
}

// #3 resume 流式：与 set 同路径。
// Python: interface_deep.py:16251-16257
func (s *GoalRPCContractSuite) TestResume_进入流式链() {
	sid := "web-goal-resume"
	rid := sendGoal(s, sid, map[string]any{"action": "resume"}, true)
	frames := goalFrames(s.env.AwaitStreamEnd(rid))
	s.Require().NotEmpty(frames)
	s.Contains([]string{eventGoalSnapshot, eventChatError, eventRuntimeAccepted},
		eventTypeOf(frames[0]), "resume 必须进适配器链")
}

// #6 未确认覆盖：后端直接构造 overwrite_confirmed=false 且已存在目标。
// 正常前端 set 恒带 true，此路径只能由后端请求覆盖契约。
// Python: goal/manager.py:134-140 (already_exists) · interface_deep.py:19365-19375
func (s *GoalRPCContractSuite) TestSet未确认覆盖_返回确认要求() {
	sid := "web-goal-overwrite"
	// 先建一个目标（一元 set，Dispatcher 接受）
	rid1 := sendGoal(s, sid, map[string]any{
		"action": "set", "objective": "第一个目标", "overwrite_confirmed": true,
	}, false)
	resp1 := s.env.AwaitUnary(rid1)
	s.Require().NotNil(resp1)

	rid2 := sendGoal(s, sid, map[string]any{
		"action": "set", "objective": "第二个目标", "overwrite_confirmed": false,
	}, false)
	resp2 := s.env.AwaitUnary(rid2)
	s.Require().NotNil(resp2)
	// 契约：要么显式 already_exists 错误，要么结构化 confirm_required；
	// 二者都不允许静默覆盖（会把用户第一个目标丢掉）。
	if !resp2.OK {
		s.Equal("already_exists", resp2.Payload["code"])
	} else {
		s.Contains([]string{eventGoalConfirmRequire, "goal_confirm_required"},
			resp2.Payload["event_type"], "未确认覆盖必须给前端确认信号")
	}
}

// #8 execution.error 帧形状：失败路径必须带 goal 字段供前端落状态。
// Python: interaction.py:167-177；前端 useWebSocket.ts:4705
func (s *GoalRPCContractSuite) TestExecutionError_帧形状() {
	sid := "web-goal-exec-error"
	rid := sendGoal(s, sid, map[string]any{"action": "explode"}, true)
	frames := goalFrames(s.env.AwaitStreamEnd(rid))
	s.Require().NotEmpty(frames)
	found := false
	for _, c := range frames {
		if eventTypeOf(c) != eventExecutionError && eventTypeOf(c) != eventChatError {
			continue
		}
		found = true
		s.Contains(c.Payload, "goal", "execution.error 须带 goal 字段")
	}
	s.True(found, "失败路径必须产出 execution.error 或 chat.error 帧")
}
```

- [ ] **Step 4: 全跑**

Run: `cd /home/opensource/uap-claw-go && CGO_ENABLED=1 go test -tags "sqlite_fts5 test integration" ./tests/integration/swarm/web/ -run "TestGoalRPCContractSuite|TestGoalContractHelpersSuite" -v`
Expected: 8 条 G1 + 1 条助手自测全 PASS。任何 FAIL 按 Step 2 口径处理。

- [ ] **Step 5: 提交**

```bash
cd /home/opensource/uap-claw-go
git add tests/integration/swarm/web/goal_rpc_contract_test.go
git commit -m "test(27.2/27.10): G1 command.goal 8 组契约快照"
```

---

### Task 4: G2 —— 事件顺序与形状 O1-O5

**Files:**
- Create: `tests/integration/swarm/web/goal_event_order_test.go`

- [ ] **Step 1: 写用例**

```go
//go:build integration

package web_test

import (
	"testing"

	"github.com/stretchr/testify/suite"

	pathutil "github.com/uapclaw/uapclaw-go/internal/common/utils/path"
	sessionpkg "github.com/uapclaw/uapclaw-go/internal/swarm/server/session"
)

// ──────────────────────────── 结构体 ────────────────────────────

// GoalEventOrderSuite G2：出站事件顺序与形状不变量。
type GoalEventOrderSuite struct {
	goalContractSuite
}

// ──────────────────────────── 导出函数 ────────────────────────────

func TestGoalEventOrderSuite(t *testing.T) {
	suite.Run(t, new(GoalEventOrderSuite))
}

func (s *GoalEventOrderSuite) SetupTest() {
	s.env = newGoalContractEnv(s.T())
	isolateGoalDataDir(s.T())
}

func (s *GoalEventOrderSuite) TearDownTest() {
	sessionpkg.FlushMetadataQueue()
	sessionpkg.FlushHistoryQueue()
	sessionpkg.ClearAllSessionMetadataCache()
	pathutil.ResetCache()
}

// O1 set 流中 goal.snapshot 严格早于 runtime.accepted（两者都到才比较）。
// Python: interface_deep.py:19887-19892 在前 / :19907,:19918,:19955 在后
// 前端: useWebSocket.ts:4690 实测 16/16 次 resume 的顺序假设
func (s *GoalEventOrderSuite) TestO1_Snapshot早于RuntimeAccepted() {
	sid := "web-goal-o1"
	rid := sendGoal(s, sid, map[string]any{
		"action": "set", "objective": "顺序验证", "overwrite_confirmed": true,
	}, true)
	frames := goalFrames(s.env.AwaitStreamEnd(rid))
	snap := indexOfEvent(frames, eventGoalSnapshot)
	accepted := indexOfEvent(frames, eventRuntimeAccepted)
	if snap >= 0 && accepted >= 0 {
		s.Less(snap, accepted, "goal.snapshot 必须早于 runtime.accepted")
	} else {
		// 前端 pendingAction 靠四种事件之一清空；至少要有一种到达
		clearing := 0
		for _, ev := range []string{
			eventGoalSnapshot, eventGoalUpdated, eventExecutionError, eventRuntimeAccepted,
		} {
			clearing += countEvent(frames, ev)
		}
		s.Greater(clearing, 0, "必须至少发出一种能清 pendingAction 的事件")
	}
}

// O2 流式 set 的 snapshot 是非终结帧；流式 get 的是终结帧。
func (s *GoalEventOrderSuite) TestO2_终结位区分() {
	getFrames := goalFrames(s.env.AwaitStreamEnd(
		sendGoal(s, "web-goal-o2-get", map[string]any{"action": "get"}, true)))
	s.Require().NotEmpty(getFrames)
	s.True(getFrames[len(getFrames)-1].IsComplete, "get 单帧必须 is_complete=true")

	setFrames := goalFrames(s.env.AwaitStreamEnd(
		sendGoal(s, "web-goal-o2-set", map[string]any{
			"action": "set", "objective": "x", "overwrite_confirmed": true,
		}, true)))
	s.Require().NotEmpty(setFrames)
	if i := indexOfEvent(setFrames, eventGoalSnapshot); i >= 0 {
		s.False(setFrames[i].IsComplete, "流式 set 的 snapshot 是非终结帧")
	}
}

// O3 goal.updated 载荷恒 {event_type, goal}；对象时 15 字段合法。
// Python: interface_deep.py:17725-17737 _interaction_goal_updated_payload
func (s *GoalEventOrderSuite) TestO3_Updated载荷形状() {
	sid := "web-goal-o3"
	rid := sendGoal(s, sid, map[string]any{
		"action": "set", "objective": "形状验证", "overwrite_confirmed": true,
	}, true)
	frames := goalFrames(s.env.AwaitStreamEnd(rid))
	for _, c := range frames {
		if eventTypeOf(c) != eventGoalUpdated {
			continue
		}
		s.Contains(c.Payload, "goal", "goal.updated 必须带 goal")
		if c.Payload["goal"] != nil {
			requireGoalRecord(s.T(), c.Payload["goal"])
		}
	}
	// clear 路径的 goal:null 由 G3 R2 覆盖；此处只验对象形状
}

// O4 goal attempt 边界的 chat.final 降级；终结帧唯一。
// Python: interface_deep.py:15619-15635
func (s *GoalEventOrderSuite) TestO4_中间Final降级且终结帧唯一() {
	sid := "web-goal-o4"
	rid := sendGoal(s, sid, map[string]any{
		"action": "set", "objective": "降级验证", "overwrite_confirmed": true,
	}, true)
	frames := goalFrames(s.env.AwaitStreamEnd(rid))
	terminal := 0
	for _, c := range frames {
		if c.IsComplete {
			terminal++
		}
		if eventTypeOf(c) == eventChatFinal {
			s.NotContains(c.Payload, fieldGoalIntermediate,
				"终结 final 不得带 goal_intermediate 标记")
		}
		if v, ok := c.Payload[fieldGoalIntermediate].(bool); ok && v {
			s.Equal(eventChatDelta, eventTypeOf(c), "降级帧必须是 chat.delta")
		}
	}
	s.LessOrEqual(terminal, 1, "一条流最多一个终结帧")
}

// O5 失败路径帧带 goal 字段（execution.error 契约）。
// 与 G1 #8 的区别：本条锁「帧形状」，G1 锁「错误码/可达性」。
func (s *GoalEventOrderSuite) TestO5_失败帧可落状态() {
	sid := "web-goal-o5"
	rid := sendGoal(s, sid, map[string]any{"action": "explode"}, true)
	frames := goalFrames(s.env.AwaitStreamEnd(rid))
	for _, c := range frames {
		if eventTypeOf(c) == eventExecutionError {
			s.Contains(c.Payload, "goal", "execution.error 须带 goal 供前端 applyGoalSnapshot")
		}
	}
}

// 明确不断言 goal.updated 与 chat.delta/chat.final 的相对顺序：
// 前端 useWebSocket.ts:269-270 明说后端不保证到达顺序，上游亦无保证。
```

- [ ] **Step 2: 跑**

Run: `cd /home/opensource/uap-claw-go && CGO_ENABLED=1 go test -tags "sqlite_fts5 test integration" ./tests/integration/swarm/web/ -run "TestGoalEventOrderSuite" -v`
Expected: 5 条 PASS。若 O1 在无 goal manager 的环境下两种事件都不出现，按用例内已有的「至少一种」兜底分支判绿；若连兜底都不满足，说明确实漏发事件 → 按规则 13.2 报缺口。

- [ ] **Step 3: 提交**

```bash
cd /home/opensource/uap-claw-go
git add tests/integration/swarm/web/goal_event_order_test.go
git commit -m "test(27.2/27.10): G2 出站事件顺序 O1-O5"
```

---

### Task 5: G3 —— 断线/重连/切会话 R1-R4

**Files:**
- Create: `tests/integration/swarm/web/goal_reconnect_test.go`

- [ ] **Step 1: 写用例（R2/R4 用 ServerEnv，R1/R3 用 DeepAgent 租约 API）**

```go
//go:build integration

package web_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/suite"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness"
	pathutil "github.com/uapclaw/uapclaw-go/internal/common/utils/path"
	sessionpkg "github.com/uapclaw/uapclaw-go/internal/swarm/server/session"
)

// ──────────────────────────── 结构体 ────────────────────────────

// GoalReconnectSuite G3：刷新/切会话/旧租约失效后的补发与隔离。
type GoalReconnectSuite struct {
	goalContractSuite
}

// ──────────────────────────── 导出函数 ────────────────────────────

func TestGoalReconnectSuite(t *testing.T) {
	suite.Run(t, new(GoalReconnectSuite))
}

func (s *GoalReconnectSuite) SetupTest() {
	s.env = newGoalContractEnv(s.T())
	isolateGoalDataDir(s.T())
}

func (s *GoalReconnectSuite) TearDownTest() {
	sessionpkg.FlushMetadataQueue()
	sessionpkg.FlushHistoryQueue()
	sessionpkg.ClearAllSessionMetadataCache()
	pathutil.ResetCache()
}

// R2 刷新页面等价：get 幂等且不写历史，连发两次状态一致。
// Python: agent_ws_server.py:3717-3718 只读不建 metadata
func (s *GoalReconnectSuite) TestR2_Get幂等() {
	sid := "web-goal-r2"
	for i := 0; i < 2; i++ {
		rid := sendGoal(s, sid, map[string]any{"action": "get"}, false)
		resp := s.env.AwaitUnary(rid)
		s.Require().NotNil(resp)
		s.True(resp.OK)
		s.Equal("No goal in this session.", resp.Payload["message"])
	}
	records, err := sessionpkg.ReadHistoryRecords(sid)
	s.Require().NoError(err)
	s.Empty(records, "两次 get 都不得写 user 轮历史")
}

// R4 会话切换不串 goal：A 的 set 不影响 B 的 get。
// Python: deep_agent.py:3941-3947 绑定后换会话报错；GoalManager.get_store(session_id)
func (s *GoalReconnectSuite) TestR4_会话隔离() {
	sidA := "web-goal-r4-a"
	sidB := "web-goal-r4-b"
	ridSet := sendGoal(s, sidA, map[string]any{
		"action": "set", "objective": "A 的目标", "overwrite_confirmed": true,
	}, false)
	s.env.AwaitUnary(ridSet)
	ridGet := sendGoal(s, sidB, map[string]any{"action": "get"}, true)
	frames := goalFrames(s.env.AwaitStreamEnd(ridGet))
	s.Require().NotEmpty(frames)
	for _, c := range frames {
		if eventTypeOf(c) != eventGoalSnapshot {
			continue
		}
		if g, ok := c.Payload["goal"].(map[string]any); ok && g != nil {
			s.NotEqual("A 的目标", g["objective"], "B 会话不得看到 A 的目标")
		}
	}
}

// R1/R3 用 DeepAgent 租约 API 直接验：新租约可挂、旧 token 不摘新流。
// Go: deep_agent.go:1676-1700 AttachOutput / :1711-1724 DetachOutput
// Go: schema/interaction.go:443-456 OutputLeaseManager.Detach
func (s *GoalReconnectSuite) TestR3_旧Token不摘新流() {
	ctx := context.Background()
	d := &harness.DeepAgent{}
	// 未 Start 时 AttachOutput 返回 (nil, nil)（deep_agent.go:1682-1684）；
	// 本条只验 Detach 的 token 语义，用 OutputLeaseManager 直接构造。
	_ = d

	m := hschema.NewOutputLeaseManager()
	lease1, ok := m.Attach()
	s.Require().True(ok, "首次挂流应成功")
	s.Require().NotNil(lease1)
	s.True(m.HasConsumer(), "挂流后应有消费者")

	s.False(m.Detach("stale-token", true), "旧 token 不得摘掉当前租约")
	s.True(m.HasConsumer(), "旧 token detach 后新流仍在")

	s.True(m.Detach(lease1.Token, true), "正确 token 应成功摘流")
	s.False(m.HasConsumer(), "摘流后无消费者")
}
```

> **import 修正**：R3 用到 `hschema "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/schema"`，把它加进 import 块；`harness` 包本身在 R3 里只用于说明，**删除 `d := &harness.DeepAgent{}` 那两行**（YAGNI，且未 Start 的 DeepAgent 无意义），同时删掉 `harness` import。最终 import：`context`、`testing`、`suite`、`hschema`、`pathutil`、`sessionpkg`。
>
> **API 核对**：`NewOutputLeaseManager` / `Attach()` / `HasConsumer()` / `Detach()` 的确切签名在 `internal/agentcore/harness/schema/interaction.go` —— 实施时先 `grep -n "func (m \*OutputLeaseManager)\|func NewOutputLeaseManager" internal/agentcore/harness/schema/interaction.go`，按实际返回值调整（若 `Attach` 返回 `(*OutputLease, bool)` 就照上面写；若只返回 `bool` 或 error，改断言形式，**不臆造签名**）。

- [ ] **Step 2: 核对 API 后跑**

Run: `cd /home/opensource/uap-claw-go && grep -n "func NewOutputLeaseManager\|func (m \*OutputLeaseManager)" internal/agentcore/harness/schema/interaction.go`
然后按实际签名调整 Step 1 的 R3 代码，再跑：
`CGO_ENABLED=1 go test -tags "sqlite_fts5 test integration" ./tests/integration/swarm/web/ -run TestGoalReconnectSuite -v`
Expected: 3 条 PASS。

- [ ] **Step 3: R1 补排 ACTIVE goal 的回归锁**

R1 需要在**已 Start 的 DeepAgent** 上验「新租约能拿到 ACTIVE goal 的补排」，依赖 `harness.AgentSuite` + `NewTestSession` + `GoalManager.Set`。照 `tests/integration/agentcore/harness/goal_lifecycle_test.go:34-50` 的 `newGoalManager` 先例构造，断言 `EventManager` 有 goal work 入队：

```go
// R1 新租约补排 ACTIVE goal work。
// Python: deep_agent.py:4048-4070；Go: deep_agent.go:1693-1698
func (s *GoalReconnectSuite) TestR1_活跃Goal在重挂流后补排Work() {
	// 用真实 session + 真 EventManager + 真 GoalManager，不 mock 被测子系统。
	sess := s.SessionSuite.NewTestSession("web-goal-r1")
	s.Require().NoError(sess.PreRun(s.Ctx))

	em := task_loop.NewEventManager()
	mgr := goal.NewGoalManager(goal.GoalManagerConfig{
		Store:             goal.NewSessionGoalStore(sess),
		EventManager:      em,
		ControlLock:       &sync.Mutex{},
		HasOutputStream:   func() bool { return true },
		CancelActiveRound: func(context.Context, hschema.RoundWorkKind, string, string) {},
		EmitEvent:         func(hschema.InteractionEvent) {},
		NotifyWork:        func() {},
		Language:          "cn",
	})
	rec, err := mgr.Set(s.Ctx, "重挂后继续", goal.SetOptions{})
	s.Require().NoError(err)
	s.Require().Equal(goal.GoalStatusActive, rec.Status)

	// 模拟 attach：ACTIVE 记录必须能补排一份 goal work（不重复）
	ok, err := mgr.EnsureActiveGoalWorkLocked()
	s.Require().NoError(err)
	s.True(ok, "首次补排应入队")
	ok2, err := mgr.EnsureActiveGoalWorkLocked()
	s.Require().NoError(err)
	s.False(ok2, "同一 goal_id+revision 不得重复入队")
}
```

> **R1 的 suite 归属**：`GoalReconnectSuite` 当前基类是 `goalContractSuite`（嵌 `BaseIntegrationSuite`），没有 `NewTestSession`。把它的基类改为 `isuite.AgentSuite`（嵌 `SessionSuite`，提供 `NewTestSession` / `Ctx` / `MockLLM`），并保留 `env *testhelpers.ServerEnv` 字段。`newGoalContractEnv` 与 `isolateGoalDataDir` 在 Task 2 已统一为只依赖 `*testing.T`，`SetupTest` 里照 `newGoalContractEnv(s.T())` / `isolateGoalDataDir(s.T())` 调用即可，**无需改签名、无需动其它 suite**。
>
> **API 核对**：`EnsureActiveGoalWorkLocked` 的签名、`CancelActiveRound` 的函数类型、`RoundWorkKind` 参数顺序，照 `internal/agentcore/harness/goal/manager.go:60-125` 与 `goal_lifecycle_test.go:34-50` 逐字复制，不臆造。若 `EnsureActiveGoalWorkLocked` 只返回 `bool`（无 error），去掉 err 断言。

- [ ] **Step 4: 全跑 + race**

Run: `cd /home/opensource/uap-claw-go && CGO_ENABLED=1 go test -tags "sqlite_fts5 test integration" -race ./tests/integration/swarm/web/ -run "TestGoalReconnectSuite|TestGoalRPCContractSuite|TestGoalEventOrderSuite" -count=2`
Expected: 全 PASS，race 0 报告。

- [ ] **Step 5: 提交**

```bash
cd /home/opensource/uap-claw-go
git add tests/integration/swarm/web/goal_contract_helpers_test.go tests/integration/swarm/web/goal_rpc_contract_test.go tests/integration/swarm/web/goal_event_order_test.go tests/integration/swarm/web/goal_reconnect_test.go
git commit -m "test(27.2/27.10): G3 断线重连 R1-R4 + 助手签名解耦"
```

---

### Task 6: G4-Mock —— 无人值守全链

**Files:**
- Create: `tests/integration/swarm/web/goal_unattended_test.go`

- [ ] **Step 1: 写全链用例（复用 goal_lifecycle_test.go 的真 store + 真 rail 模式）**

```go
//go:build integration

package web_test

import (
	"context"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/suite"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/goal"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/rails"
	hschema "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/schema"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/task_loop"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	isuite "github.com/uapclaw/uapclaw-go/tests/integration/suite"
)

// ──────────────────────────── 结构体 ────────────────────────────

// GoalUnattendedSuite G4：MockLLM 编排下目标自动续跑到终态的完整状态机。
type GoalUnattendedSuite struct {
	isuite.AgentSuite
}

// ──────────────────────────── 导出函数 ────────────────────────────

func TestGoalUnattendedSuite(t *testing.T) {
	suite.Run(t, new(GoalUnattendedSuite))
}

// newUnattendedManager 真 store / 真 EventManager / 真回调，不 mock 被测子系统。
func (s *GoalUnattendedSuite) newUnattendedManager(
	sess *session.Session, em *task_loop.EventManager,
) *goal.GoalManager {
	return goal.NewGoalManager(goal.GoalManagerConfig{
		Store:             goal.NewSessionGoalStore(sess),
		EventManager:      em,
		ControlLock:       &sync.Mutex{},
		HasOutputStream:   func() bool { return true },
		CancelActiveRound: func(context.Context, hschema.RoundWorkKind, string, string) {},
		EmitEvent:         func(hschema.InteractionEvent) {},
		NotifyWork:        func() {},
		Language:          "cn",
	})
}

// goalRoundInputs 构造一轮 goal attempt 的迭代输入。
func goalRoundInputs(rec *goal.GoalRecord, sid string) *agentinterfaces.TaskIterationInputs {
	return &agentinterfaces.TaskIterationInputs{
		Query:   "原始输入",
		RunKind: agentinterfaces.RunKindGoal,
		RunContext: &agentinterfaces.RunContext{
			GoalID: rec.GoalID, Revision: rec.Revision, SessionID: sid,
		},
	}
}

// Test自动续跑到Completed 两轮 continue → complete，验证 query 重建、
// attempt_count 递增、终态停止续跑、goal.updated(completed) 可达。
func (s *GoalUnattendedSuite) Test自动续跑到Completed() {
	ctx := s.Ctx
	sess := s.NewTestSession("web-goal-unattended-complete")
	s.Require().NoError(sess.PreRun(ctx))
	em := task_loop.NewEventManager()
	mgr := s.newUnattendedManager(sess, em)
	rail := rails.NewTaskCompletionRail()
	rail.SetGoalManager(mgr)

	rec, err := mgr.Set(ctx, "补齐登录测试", goal.SetOptions{})
	s.Require().NoError(err)

	for attempt := 0; attempt < 3; attempt++ {
		started, err := mgr.BeginAttempt(ctx, rec.GoalID, rec.Revision)
		s.Require().NoError(err)
		s.Require().NotNil(started, "第 %d 轮应还能开始", attempt)

		inputs := goalRoundInputs(started, started.SessionID)
		cbc := agentinterfaces.NewAgentCallbackContext(nil, inputs, sess)
		s.Require().NoError(rail.BeforeTaskIteration(ctx, cbc))
		s.Contains(inputs.Query, "<goal_task>")
		s.Contains(inputs.Query, "补齐登录测试")
		if attempt > 0 {
			s.Contains(inputs.Query, "<previous_assessment>",
				"第二轮 query 必须含上一轮评估，且是重建的")
		}

		status := goal.GoalAssessmentStatusContinue
		if attempt == 2 {
			status = goal.GoalAssessmentStatusComplete
		}
		rail.GoalReportSink().Submit(goal.GoalAssessment{
			Status: status, Evidence: "第" + string(rune('0'+attempt)) + "轮证据",
		})
		inputs.Result = map[string]any{"result_type": "normal"}
		cbc.SetInputs(inputs)
		s.Require().NoError(rail.AfterTaskIteration(ctx, cbc))

		rec = mgr.Get()
		s.Require().NotNil(rec)
		if status == goal.GoalAssessmentStatusComplete {
			break
		}
	}

	s.Equal(goal.GoalStatusCompleted, rec.Status, "三轮后应到 completed")
	s.Equal(3, rec.AttemptCount, "attempt_count 应逐轮递增到 3")
	s.Equal("completed", rec.LastStopReason)
	ok, err := mgr.EnsureActiveGoalWorkLocked()
	s.Require().NoError(err)
	s.False(ok, "终态后不得再补排 goal work")
}

// Test硬刹车MaxAttempts 单轮上限耗尽 → BLOCKED。
func (s *GoalUnattendedSuite) Test硬刹车MaxAttempts() {
	ctx := s.Ctx
	sess := s.NewTestSession("web-goal-unattended-budget")
	s.Require().NoError(sess.PreRun(ctx))
	em := task_loop.NewEventManager()
	mgr := s.newUnattendedManager(sess, em)
	rail := rails.NewTaskCompletionRail()
	rail.SetGoalManager(mgr)

	one := 1
	rec, err := mgr.Set(ctx, "一次机会", goal.SetOptions{MaxAttempts: &one})
	s.Require().NoError(err)

	started, err := mgr.BeginAttempt(ctx, rec.GoalID, rec.Revision)
	s.Require().NoError(err)
	inputs := goalRoundInputs(started, started.SessionID)
	cbc := agentinterfaces.NewAgentCallbackContext(nil, inputs, sess)
	s.Require().NoError(rail.BeforeTaskIteration(ctx, cbc))
	rail.GoalReportSink().Submit(goal.GoalAssessment{
		Status: goal.GoalAssessmentStatusContinue, Evidence: "还没做完",
	})
	inputs.Result = map[string]any{"result_type": "normal"}
	cbc.SetInputs(inputs)
	s.Require().NoError(rail.AfterTaskIteration(ctx, cbc))

	rec = mgr.Get()
	s.Equal(goal.GoalStatusBlocked, rec.Status, "continue 撞上 max_attempts 应判 BLOCKED")
	s.Contains(strings.ToLower(rec.LastAssessment.Evidence), "max_attempts")
}
```

> **API 核对（必须做）**：`goal.SetOptions` 是否有 `MaxAttempts *int`、`mgr.BeginAttempt(ctx, goalID, revision)` 的签名、`mgr.Get()` 是否返回值还是 `(value, error)`、`GoalAssessment` 字段名、`rail.GoalReportSink()` 是否导出、`EnsureActiveGoalWorkLocked` 返回值个数 —— 全部照 `internal/agentcore/harness/goal/manager.go`、`goal/schema.go`、`goal_lifecycle_test.go:34-102` 逐字核对后调整。**不臆造任何签名**；若 `BeginAttempt` 返回 `(*GoalRecord, error)` 就照上面，若不同则改。
> `session.Session` 的 import 为 `"github.com/uapclaw/uapclaw-go/internal/agentcore/session"`，加到 import 块。

- [ ] **Step 2: 核对 API 后跑**

Run: `cd /home/opensource/uap-claw-go && grep -n "type SetOptions\|func (m \*GoalManager) BeginAttempt\|func (m \*GoalManager) Get\|func (m \*GoalManager) EnsureActiveGoalWorkLocked" internal/agentcore/harness/goal/manager.go internal/agentcore/harness/goal/schema.go`
调整后：`CGO_ENABLED=1 go test -tags "sqlite_fts5 test integration" ./tests/integration/swarm/web/ -run TestGoalUnattendedSuite -v`
Expected: 2 条 PASS。若 `max_attempts` 硬刹车没触发（仍 ACTIVE），说明 evaluator 的默认配置未吃到 record 的 max_attempts → 按规则 13.2 报缺口，不默默改。

- [ ] **Step 3: 提交**

```bash
cd /home/opensource/uap-claw-go
git add tests/integration/swarm/web/goal_unattended_test.go
git commit -m "test(27.2/27.10): G4 MockLLM 无人值守全链（续跑到终态 + 硬刹车）"
```

---

### Task 7: G4-Real —— 真 LLM 冒烟（integration && llm）

**Files:**
- Create: `tests/integration/swarm/web/goal_realllm_test.go`

- [ ] **Step 1: 写冒烟用例**

```go
//go:build integration && llm

package web_test

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/uapclaw/uapclaw-go/internal/common/config"
	pathutil "github.com/uapclaw/uapclaw-go/internal/common/utils/path"
	"github.com/uapclaw/uapclaw-go/internal/swarm/schema"
	sessionpkg "github.com/uapclaw/uapclaw-go/internal/swarm/server/session"
	"github.com/uapclaw/uapclaw-go/tests/integration/testhelpers"
)

// TestGoalCommand_真实LLM冒烟 真模型接入后协议面不崩的冒烟。
//
// 双守卫：文件级 integration && llm 标签 + LLM_API_KEY 缺失时 Skip。
// 只硬断言确定性契约（受理、帧形状、枚举、流关闭），不断言模型是否
// 选择 submit_goal_report / 是否跑到 COMPLETED / attempt 轮数。
//
// 运行方式：
//
//	LLM_API_KEY=xxx go test -tags "sqlite_fts5 test integration llm" \
//	  ./tests/integration/swarm/web/ -run TestGoalCommand_真实LLM冒烟 -count=1 -v
func TestGoalCommand_真实LLM冒烟(t *testing.T) {
	if os.Getenv("LLM_API_KEY") == "" {
		t.Skip("LLM_API_KEY 未配置，跳过真 LLM 冒烟")
	}
	t.Setenv(pathutil.EnvDataDir, t.TempDir())
	pathutil.ResetCache()
	sessionpkg.ClearAllSessionMetadataCache()
	t.Cleanup(func() {
		sessionpkg.FlushMetadataQueue()
		sessionpkg.FlushHistoryQueue()
		sessionpkg.ClearAllSessionMetadataCache()
		pathutil.ResetCache()
	})

	cfg, err := config.New("")
	require.NoError(t, err)
	env := testhelpers.NewServerEnv(t, cfg, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
	defer cancel()

	rid := env.Send(string(schema.ReqMethodCommandGoal), map[string]any{
		"action": "set", "objective": "列出当前目录文件", "mode": "agent",
		"overwrite_confirmed": true,
	}, "web-goal-realllm", true)

	// 有界收集：超时或没观察到终态都软跳过，不把 429/慢模型染红。
	done := make(chan goalFrames, 1)
	go func() {
		done <- goalFrames(env.AwaitStreamEnd(rid))
	}()

	var frames goalFrames
	select {
	case frames = <-done:
	case <-ctx.Done():
		t.Skipf("真 LLM 冒烟未在 180s 内收敛：%v", ctx.Err())
	}

	require.NotEmpty(t, frames, "必须至少产出一帧")
	snap := indexOfEvent(frames, eventGoalSnapshot)
	accepted := indexOfEvent(frames, eventRuntimeAccepted)
	require.True(t, snap >= 0 || accepted >= 0,
		"set 必须被受理（goal.snapshot 或 runtime.accepted 之一到达）")
	if snap >= 0 && accepted >= 0 {
		require.Less(t, snap, accepted, "O1：snapshot 应早于 accepted")
	}
	for _, c := range frames {
		if eventTypeOf(c) != eventGoalUpdated {
			continue
		}
		if c.Payload["goal"] != nil {
			requireGoalRecord(t, c.Payload["goal"])
		}
	}
	require.True(t, frames[len(frames)-1].IsComplete || ctx.Err() == nil,
		"流最终应关闭或用例有界收敛")
}
```

- [ ] **Step 2: 无 llm 标签时必须完全跳过**

Run: `cd /home/opensource/uap-claw-go && CGO_ENABLED=1 go test -tags "sqlite_fts5 test integration" ./tests/integration/swarm/web/ -v 2>&1 | grep -c "真实LLM"`
Expected: `0`（标签隔离生效，不进默认门禁）。

- [ ] **Step 3: 有标签但无 key 时 Skip 而非 FAIL**

Run: `cd /home/opensource/uap-claw-go && env -u LLM_API_KEY CGO_ENABLED=1 go test -tags "sqlite_fts5 test integration llm" ./tests/integration/swarm/web/ -run TestGoalCommand_真实LLM冒烟 -v`
Expected: `--- SKIP: TestGoalCommand_真实LLM冒烟`，退出码 0。

- [ ] **Step 4: 提交**

```bash
cd /home/opensource/uap-claw-go
git add tests/integration/swarm/web/goal_realllm_test.go
git commit -m "test(27.2/27.10): G4 真 LLM 冒烟（integration && llm 双守卫）"
```

---

### Task 8: 审计、全量回归、计划回填、推送

**Files:**
- Modify: `IMPLEMENTATION_PLAN.md`（27.2 / 27.10 / 9-G 剩余工作表）
- Modify: `tests/integration/swarm/web/doc.go`（若文件清单与最终不一致）

- [ ] **Step 1: 集成审计两层门禁自检**

Run: `cd /home/opensource/uap-claw-go && go run ./cmd/integration-audit -verify`
Expected: `tests/integration/swarm/web` 通过（静态调用跨度 ≥3、有效测试密度 ≥1、每文件有 `//go:build integration` 且调用 `suite.Run(` 或走 `TestXxx` 有效方法、distinct internal import ≥2；覆盖交叉验证若被判灰区，各子系统 ≥30 行）。若判灰区不过，补一条真实跨 `goal` 域 + `rails` + `task_loop` 的调用（G4 用例已覆盖），按审计输出调整，**不靠豁免文件掩盖**。

- [ ] **Step 2: 全量本地门槛**

```bash
cd /home/opensource/uap-claw-go
export GOPROXY=https://goproxy.cn,direct
go build ./...
go vet ./...
CGO_ENABLED=1 go test -tags "sqlite_fts5 test" ./...
CGO_ENABLED=1 go test -tags "sqlite_fts5 test integration" -race ./tests/integration/swarm/web/
```

Expected: 前三者 0 错误；第四条全 PASS + race 0 报告。若全量单测里出现既有偶发红（如 `progressive_rail` 的"目录未变化不应重复推送"），单独复跑该包确认与本次改动无关，不混入结论、不改动无关文件。

- [ ] **Step 3: 契约矩阵文档（可选，若实施中发现需记录的偏差）**

仅当 Task 3-6 出现契约不符并按规则 13.2 报给用户后，才创建 `docs/sync/goal-web-contract-matrix.md` 记录 8 组实测结果；若全绿则**不建此文件**（YAGNI）。

- [ ] **Step 4: 计划状态回填**

把 `IMPLEMENTATION_PLAN.md` 中：
- `| 27.2 | 🔄 |` 改 `| 27.2 | ✅ |`，在内容列末尾追加 `✅ **验收完成（2026-11-09）**：G1 8 组契约 + G2 O1-O5 + G3 R1-R4 + G4 全链 + 真 LLM 冒烟，本地 build/vet/单测/集成（含 -race）全绿，审计 -verify 通过；「距离 Goal 完整使用的剩余工作」表第 1 项划掉`；
- `| 27.10 | ☐ |` 改 `| 27.10 | ✅ |`，追加实测证据（用例文件路径、用例数、`integration-audit -verify` 结论）；
- 9-G 批次表 `| B5 | 27.2 | 🔄 部分 |` 改 `✅ 2026-11-09`；
- 9-G「剩余工作」表第 1 项与第 5 项标 ✅ 完成，第 2/3/4 项保持 ☐ 并注明"非阻塞，已登记归属章节"。
- 若出现新缺口且用户裁决挂 ⤵️，在对应章节新建一行 ⤵️ 并写明 Python 行号、影响面、回收条件。

> **计数口径**：回填中的用例数用 `grep -c "^func (s \*.*) Test\|^func Test" tests/integration/swarm/web/*_test.go` 机器核对后写，禁止心算。

- [ ] **Step 5: 提交并推送**

```bash
cd /home/opensource/uap-claw-go
git add IMPLEMENTATION_PLAN.md tests/integration/swarm/web/doc.go
git commit -m "docs(27.2/27.10): Goal 前端契约验收完成与计划回填"
git push origin main
```

按规则 11：推送即交付，**不主动 `gh run list` / `gh run watch`**，汇报时只给本地验证结果（命令 + 结论），不给 CI 结论。

---

## 自检（writing-plans 要求，作者自跑）

**Spec 覆盖对照**：

| Spec 章节 | 对应 Task |
|---|---|
| §1 概述 / 流程位置 / 作用 | 前置事实 1-3 + Task 1 doc.go 描述 |
| §1.4 计数核对 | 前置事实 4-5（已机器核对） |
| §2 现状盘点 / 两处疑点 | 前置事实 6 + Task 5 R1/R3 |
| §3 范围与分层 / 链路选择 | 文件结构 + Task 2（ServerEnv） |
| §4 G1 契约矩阵 8 组 / 15 字段 | Task 1 助手 + Task 3（8 条） |
| §5 G2 O1-O5 | Task 4 |
| §6 G3 R1-R4 | Task 5 |
| §7.1 G4 MockLLM 全链 | Task 6 |
| §7.2 G4 真 LLM 冒烟双守卫/软失败 | Task 7 |
| §8 测试策略 / 规则 7 / 13.2 / 完成标准 | Task 8 + 各 Task 的「FAIL 处理」 |
| §9 删除清单（无） | 无删除步骤，符合 |
| §10 偏差登记 D-1/D-2/D-3 | 前置事实 4（D-1）、Task 3 #1b（D-2）、Task 7（D-3） |

**占位符扫描**：无 `TBD` / `implement later` / `类似 Task N` / 无代码的步骤。所有「实施注意」「API 核对」段落都给了**具体的 grep 命令 + 具体源码文件 + 具体改法**，不是「加适当处理」。

**类型一致性**：`goalFrames` / `eventTypeOf` / `indexOfEvent` / `countEvent` / `requireGoalRecord` / `sendGoal` / `newGoalContractEnv` / `isolateGoalDataDir` / `goalContractSuite` 在 Task 1-2 定义，Task 3-7 全部沿用同名同签名；事件常量名（`eventGoalSnapshot` 等）全局一致。`newGoalContractEnv` 与 `isolateGoalDataDir` 在 Task 2 定义时即采用 `(*testing.T)` 解耦签名，Task 3-7 全部调用 `...(s.T())`，无中途改名。

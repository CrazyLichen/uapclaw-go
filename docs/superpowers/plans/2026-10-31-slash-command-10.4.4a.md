# 10.4.4a Slash 命令对账收口 + command.model 实现计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 把 Gateway slash 命令处理与 Python 逐条对齐（6 项改代码、2 项核实等价），并全量实现 `command.model`，同时把 `message_handler` 包覆盖率从 56.8% 提到 ≥85%。

**Architecture:** 不动生产架构。新增一个基于 `ChannelTransport` 的假 AgentServer 测试替身，让三个发 E2A 的函数（`skillsSlashNotice` / `branchSlashNotice` / `rewindSlashNotice`）可被完整驱动；生产侧只做「顺序纠正 + 补日志 + 补 `params={}` + `command.model` 三分支」五处增量修改。

**Tech Stack:** Go 1.x、`stretchr/testify`、`internal/swarm/transport` 的 `ChannelTransport`、`internal/swarm/e2a` wire codec。

**Spec:** `docs/superpowers/specs/2026-10-31-slash-command-10.4.4a-design.md`

---

## 文件结构

| 文件 | 动作 | 职责 |
|---|---|---|
| `internal/swarm/gateway/message_handler/fake_agent_server_test.go` | 新建 | 假 AgentServer 测试替身 + 通知等待辅助 |
| `internal/swarm/gateway/message_handler/slash_cmd.go` | 修改 | A1/A2/A5/A6/A7/A8 六项对齐 |
| `internal/swarm/gateway/message_handler/slash_cmd_test.go` | 修改 | 新增 3 个 E2A 函数用例 + A7/A8 断言 |
| `internal/swarm/gateway/message_handler/command_parser/slash_command.go` | 修改 | 新增 `ParsedControlActionString`（对齐 Python `action.value`） |
| `internal/swarm/gateway/message_handler/command_parser/slash_command_test.go` | 修改 | 新增 A7 日志名映射 + 边界用例 |
| `internal/swarm/server/handle_command.go` | 修改 | `handleCommandModel` 从 stub 换真实现 + 5 个辅助函数 |
| `internal/swarm/server/handle_command_model_test.go` | 新建 | `command.model` 四分支测试 |
| `IMPLEMENTATION_PLAN.md` | 修改 | 10.4.4 拆分 a/b 两行 + 10.3.24 ⤴️ |

无新增非测试生产文件，`doc.go` 不改。

---

### Task 1: 假 AgentServer 测试替身

**Files:**
- Create: `internal/swarm/gateway/message_handler/fake_agent_server_test.go`

- [ ] **Step 1: 写辅助文件（本文件本身不写断言，只提供基建）**

```go
package message_handler

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/uapclaw/uapclaw-go/internal/swarm/e2a"
	"github.com/uapclaw/uapclaw-go/internal/swarm/gateway/routing"
	"github.com/uapclaw/uapclaw-go/internal/swarm/schema"
	"github.com/uapclaw/uapclaw-go/internal/swarm/transport"
)

// ──────────────────────────── 结构体 ────────────────────────────

// fakeReply 假 AgentServer 的响应构造器：入参为请求的 request_id 与原请求帧。
type fakeReply func(requestID string, req map[string]any) *schema.AgentResponse

// fakeAgentServer 用 ChannelTransport 扮演 AgentServer：
// 启动时先回 connection.ack 让 AgentClient 进入 connected，之后按请求 method
// 查表把响应编码成 E2A 线帧回送。
type fakeAgentServer struct {
	// tr 进程内传输
	tr *transport.ChannelTransport
	// client 被测代码持有的 AgentClient
	client *routing.AgentClient
	// mu 保护 replies / delays / methods
	mu sync.Mutex
	// replies method → 响应构造器
	replies map[string]fakeReply
	// delays method → 回复前延迟（用于制造时序屏障）
	delays map[string]time.Duration
	// methods 已收到的请求 method 序列（按到达顺序）
	methods []string
}

// ──────────────────────────── 导出函数 ────────────────────────────

// ──────────────────────────── 非导出函数 ────────────────────────────

// newFakeAgentServer 启动假 AgentServer，返回它和已建立连接的 MessageHandler。
func newFakeAgentServer(t *testing.T, replies map[string]fakeReply) (*fakeAgentServer, *MessageHandler) {
	t.Helper()

	f := &fakeAgentServer{
		tr:      transport.NewChannelTransport(),
		replies: replies,
		delays:  make(map[string]time.Duration),
	}
	f.client = routing.NewAgentClient(f.tr)

	ack, err := json.Marshal(transport.BuildConnectionAckFrame())
	if err != nil {
		t.Fatalf("marshal connection.ack: %v", err)
	}
	// 先入队 ack：Connect 启动 receiverLoop 后立即取到，serverReady 置真
	f.tr.RecvCh() <- ack

	go f.serve()

	mh := NewMessageHandler(f.client)
	if err := f.client.Connect(context.Background()); err != nil {
		t.Fatalf("connect fake agent server: %v", err)
	}
	if !f.client.IsConnected() {
		t.Fatal("fake agent server: AgentClient 未进入 connected 状态")
	}

	t.Cleanup(func() {
		_ = f.tr.Close()
		f.client.Disconnect()
	})
	return f, mh
}

// serve 消费 Gateway 发来的请求帧，按 method 查表回送响应。
func (f *fakeAgentServer) serve() {
	for frame := range f.tr.SendCh() {
		var req map[string]any
		if err := json.Unmarshal(frame, &req); err != nil {
			continue
		}
		method, _ := req["method"].(string)
		rid := transport.WireRequestIDKey(req["request_id"])

		f.mu.Lock()
		f.methods = append(f.methods, method)
		reply := f.replies[method]
		delay := f.delays[method]
		f.mu.Unlock()

		if delay > 0 {
			time.Sleep(delay)
		}
		if reply == nil {
			continue
		}
		resp := reply(rid, req)
		if resp == nil {
			continue
		}

		wire := e2a.EncodeAgentResponseForWire(resp, rid, 0)
		// 强制回填 request_id / response_id：AgentClient 的队列按 request_id 路由，
		// 不依赖 E2A 编码器是否把 responseID 写进顶层字段。
		wire["request_id"] = rid
		wire["response_id"] = rid

		out, err := json.Marshal(wire)
		if err != nil {
			continue
		}
		f.tr.RecvCh() <- out
	}
}

// methodCount 返回指定 method 已收到的请求数。
func (f *fakeAgentServer) methodCount(method string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for _, m := range f.methods {
		if m == method {
			n++
		}
	}
	return n
}

// setDelay 为指定 method 设置回复延迟。
func (f *fakeAgentServer) setDelay(method string, d time.Duration) {
	f.mu.Lock()
	f.delays[method] = d
	f.mu.Unlock()
}

// waitNotice 从出站队列取下一条通知。
func waitNotice(t *testing.T, mh *MessageHandler) *schema.Message {
	t.Helper()
	select {
	case notice := <-mh.robotMessages:
		return notice
	case <-time.After(3 * time.Second):
		t.Fatal("timeout waiting for channel notice")
		return nil
	}
}

// waitNoticeContaining 跳过不含 substr 的通知，直到取到匹配的一条。
func waitNoticeContaining(t *testing.T, mh *MessageHandler, substr string) *schema.Message {
	t.Helper()
	deadline := time.After(3 * time.Second)
	for {
		select {
		case notice := <-mh.robotMessages:
			if content, _ := notice.Payload["content"].(string); strings.Contains(content, substr) {
				return notice
			}
		case <-deadline:
			t.Fatalf("timeout waiting for notice containing %q", substr)
			return nil
		}
	}
}
```

- [ ] **Step 2: 编译验证（此时无测试引用，只需能编译）**

Run: `go build ./internal/swarm/gateway/message_handler/ && go vet ./internal/swarm/gateway/message_handler/`
Expected: 无输出、退出码 0

- [ ] **Step 3: Commit**

```bash
git add internal/swarm/gateway/message_handler/fake_agent_server_test.go
git commit -m "test(message_handler): 新增假 AgentServer 测试替身"
```

---

### Task 2: A8 `sendChannelNotice` 补 `params={}`

**Files:**
- Modify: `internal/swarm/gateway/message_handler/slash_cmd.go:243-254`
- Test: `internal/swarm/gateway/message_handler/slash_cmd_test.go`

- [ ] **Step 1: 写失败测试**

在 `slash_cmd_test.go` 的 `TestSendChannelNotice` 之后追加：

```go
// TestSendChannelNotice_Params为空对象 A8：Python Message(params={})，
// Go 侧必须发空对象而非 null，否则下游按 params 是否为 nil 分支的 Channel 行为不同。
func TestSendChannelNotice_Params为空对象(t *testing.T) {
	mh := createTestMessageHandlerWithTransport()

	msg := schema.NewReqMessage("feishu_test", "sess-1", schema.ReqMethodChatSend, json.RawMessage(`{}`))
	mh.sendChannelNotice(msg, map[string]any{"content": "测试通知"})

	select {
	case notice := <-mh.robotMessages:
		assert.JSONEq(t, `{}`, string(notice.Params), "params 应为空对象 {}")
		assert.True(t, notice.EnableStreaming, "enable_streaming 默认 True")
		assert.Equal(t, schema.EventTypeChatFinal, notice.EventType)
	case <-time.After(2 * time.Second):
		t.Fatal("未收到通知")
	}
}
```

- [ ] **Step 2: 跑测试确认失败**

Run: `go test -run 'TestSendChannelNotice_Params为空对象' ./internal/swarm/gateway/message_handler/ -v`
Expected: FAIL，`params 应为空对象 {}`（实际为 `null` 或空字节）

- [ ] **Step 3: 改实现**

把 `slash_cmd.go` 中 `noticeMsg` 字面量改成（新增 `Params` 一行）：

```go
	noticeMsg := &schema.Message{
		ID:              msg.ID,
		Type:            schema.MessageTypeEvent,
		ChannelID:       msg.ChannelID,
		SessionID:       msg.SessionID,
		Params:          json.RawMessage(`{}`),
		Timestamp:       schema.NowTimestamp(),
		OK:              true,
		EventType:       schema.EventTypeChatFinal,
		Payload:         payload,
		Metadata:        msg.Metadata,
		EnableStreaming: true,
	}
```

- [ ] **Step 4: 跑测试确认通过**

Run: `go test -run 'TestSendChannelNotice' ./internal/swarm/gateway/message_handler/ -v`
Expected: PASS（两条用例都过）

- [ ] **Step 5: Commit**

```bash
git add internal/swarm/gateway/message_handler/slash_cmd.go internal/swarm/gateway/message_handler/slash_cmd_test.go
git commit -m "fix(message_handler): sendChannelNotice 补 params={} 对齐 Python"
```

---

### Task 3: A7 `handleChannelControl` 顺序、提前返回与日志点位

**Files:**
- Modify: `internal/swarm/gateway/message_handler/slash_cmd.go:33-117`
- Modify: `internal/swarm/gateway/message_handler/command_parser/slash_command.go`（末尾追加导出函数）
- Test: `internal/swarm/gateway/message_handler/slash_cmd_test.go`、`command_parser/slash_command_test.go`

- [ ] **Step 1: 先加 `ParsedControlActionString`（Python `parsed.action.value`）**

在 `command_parser/slash_command.go` 的「全局变量」区追加：

```go
// parsedControlActionStrings ParsedControlAction 到 Python 枚举值的映射
var parsedControlActionStrings = map[ParsedControlAction]string{
	ActionNone:           "none",
	ActionNewSessionOK:   "new_session_ok",
	ActionNewSessionBad:  "new_session_bad",
	ActionModeOK:         "mode_ok",
	ActionModeBad:        "mode_bad",
	ActionSwitchOK:       "switch_ok",
	ActionSwitchBad:      "switch_bad",
	ActionSkillsOK:       "skills_ok",
	ActionBranchOK:       "branch_ok",
	ActionRewindOK:       "rewind_ok",
	ActionRewindBad:      "rewind_bad",
	ActionRewindConfirm:  "rewind_confirm",
	ActionRewindCancel:   "rewind_cancel",
}
```

在「导出函数」区追加：

```go
// ParsedControlActionString 返回 ParsedControlAction 的 Python 枚举值字符串。
//
// Python: ParsedControlAction.value（"new_session_ok" 等），用于日志字段对齐。
func ParsedControlActionString(a ParsedControlAction) string {
	if s, ok := parsedControlActionStrings[a]; ok {
		return s
	}
	return ""
}
```

- [ ] **Step 2: 写失败测试（state 副作用）**

在 `slash_cmd_test.go` 追加：

```go
// TestHandleChannelControl_Skills不创建渠道状态 A7：Python 在 SKILLS_OK 处提前 return，
// 不调用 _get_or_create_channel_state；Go 必须在建 state 之前短路。
func TestHandleChannelControl_Skills不创建渠道状态(t *testing.T) {
	mh := createTestMessageHandlerWithTransport()

	msg := schema.NewReqMessage("feishu_test", "sess-1", schema.ReqMethodChatSend,
		json.RawMessage(`{"content":"/skills list"}`), schema.WithProvider("feishu"))

	handled := mh.handleChannelControl(msg)
	assert.True(t, handled, "/skills list 应被处理")

	mh.statesMu.RLock()
	count := len(mh.channelStates)
	mh.statesMu.RUnlock()
	assert.Equal(t, 0, count, "skills_ok 路径不得创建 channel state")
}
```

- [ ] **Step 3: 跑测试确认失败**

Run: `go test -run 'TestHandleChannelControl_Skills不创建渠道状态' ./internal/swarm/gateway/message_handler/ -v`
Expected: FAIL，`skills_ok 路径不得创建 channel state`（实际为 1）

- [ ] **Step 4: 重写 `handleChannelControl`**

把 `slash_cmd.go` 中 `handleChannelControl` 整体替换为：

```go
// handleChannelControl 处理 slash 命令，返回 true 表示已处理
//
// Python: _handle_channel_control (L615-883)，顺序严格对齐：
//   L626 渠道白名单 → L630 取 text → L634 解析 → L639 记 info
//   → L645 SKILLS_OK 提前 return（不建 state）→ L652 建 state → 分发
//
// D1（有意偏差）：保留 Type/ReqMethod 双重前置守卫。Python 的三个发布方
// （web publish_user_messages、cron、evolution autoAnswer）均发 type=req，
// slash 文本只出现在 query/content，守卫等价且省一次解析。
func (mh *MessageHandler) handleChannelControl(msg *schema.Message) bool {
	// 仅处理请求消息
	if msg.Type != schema.MessageTypeReq {
		return false
	}

	// 仅处理 chat.send 方法
	if msg.ReqMethod != schema.ReqMethodChatSend {
		return false
	}

	// Python: L626 先判渠道白名单，非受控渠道不解析
	channelType := mh.resolveControlChannelType(msg)
	if !controlChannelTypes[string(channelType)] {
		return false
	}

	// Python: L630 params.get("query") or params.get("content") or ""
	text := extractTextFromParams(msg.Params)
	if text == "" {
		return false
	}

	// Python: L634 解析
	parsed := command_parser.ParseChannelControlText(text)
	if parsed.Action == command_parser.ActionNone {
		return false
	}

	// Python: L637-642 解析成功后立即记录（分发之前）
	logger.Info(logComponent).
		Str("event_type", "channel_control_parsed").
		Str("channel_type", string(channelType)).
		Str("text", text).
		Str("action", command_parser.ParsedControlActionString(parsed.Action)).
		Msg("slash command parsed")

	// Python: L645-648 /skills list 走独立路径，且不创建 channel state
	if parsed.Action == command_parser.ActionSkillsOK {
		mh.skillsSlashNotice(msg)
		return true
	}

	// Python: L652 之后才创建 channel state
	state := mh.GetOrCreateChannelState(msg)

	// 按 action 分发
	switch parsed.Action {
	case command_parser.ActionNewSessionOK:
		// 11.7+11.13：SessionMap 轮转 + state 更新 + SessionStart hook
		// Python: L654-667（全部在入口函数体内，收尾甩子函数）
		oldSID := state.SessionID
		var newSID string
		if p, c, b, u, ok := extractIdentityTuple(msg); ok && channelIDMatchesSessionMapTypes(msg.ChannelID) {
			newSID = mh.sessionMap.GetSessionID(p, c, b, u, true) // rotate=true
		} else {
			newSID = GenerateChannelSessionID(string(channelType))
		}
		state.SessionID = newSID
		// 触发 SessionStart hook（fire-and-forget）
		// Python: if self._gateway_hook_handler: asyncio.create_task(on_session_start(new_sid, source=channel_type))
		mh.TriggerSessionStartHook(newSID, string(channelType))
		// 收尾：异步取消旧会话 + 发变更通知
		mh.newSessionCancelAndNotice(msg, oldSID, newSID)
	case command_parser.ActionNewSessionBad:
		mh.sendChannelNotice(msg, map[string]any{"content": "非法指令"})
	case command_parser.ActionModeOK:
		mh.modeChangeCancelAndNotice(msg, parsed)
	case command_parser.ActionModeBad:
		mh.sendChannelNotice(msg, map[string]any{"content": "非法指令"})
	case command_parser.ActionSwitchOK:
		mh.modeChangeCancelAndNotice(msg, parsed)
	case command_parser.ActionSwitchBad:
		mh.sendChannelNotice(msg, map[string]any{"content": "非法指令"})
	case command_parser.ActionBranchOK:
		mh.branchSlashNotice(msg, parsed)
	case command_parser.ActionRewindOK:
		mh.rewindSlashConfirmPrompt(msg, parsed)
	case command_parser.ActionRewindBad:
		mh.sendChannelNotice(msg, map[string]any{"content": "非法指令，/rewind 须带正整数轮次编号，如 /rewind 2"})
	case command_parser.ActionRewindConfirm:
		mh.rewindSlashNotice(msg, parsed)
	case command_parser.ActionRewindCancel:
		mh.sendChannelNotice(msg, map[string]any{"content": "[收到 /rewind cancel] 已取消回退操作。"})
	default:
		return false
	}

	return true
}
```

注意：原函数末尾的 `logger.Info(... channel_control_handled ...)` 整段删除（日志已上移）。

- [ ] **Step 5: 跑本包全部测试**

Run: `go test ./internal/swarm/gateway/message_handler/... 2>&1 | tail -20`
Expected: PASS。若 `TestHandleChannelControl_非控制文本` 之类因顺序变化失败，逐条核对：非受控渠道现在在解析前就返回 `false`，与 Python 一致。

- [ ] **Step 6: Commit**

```bash
git add internal/swarm/gateway/message_handler/slash_cmd.go internal/swarm/gateway/message_handler/slash_cmd_test.go internal/swarm/gateway/message_handler/command_parser/slash_command.go
git commit -m "fix(message_handler): handleChannelControl 顺序/提前返回/日志点位对齐 Python"
```

---

### Task 4: A3/A4 `/skills list` 形态锁定（不改生产代码）

**Files:**
- Test: `internal/swarm/gateway/message_handler/slash_cmd_test.go`

- [ ] **Step 1: 写测试**

追加：

```go
// skillsReplies 构造 /skills list 的假响应。
func skillsReplies(payload map[string]any, ok bool) map[string]fakeReply {
	return map[string]fakeReply{
		string(schema.ReqMethodSkillsList): func(rid string, _ map[string]any) *schema.AgentResponse {
			return schema.NewAgentResponse(rid, "feishu_test",
				schema.WithResponseOK(ok), schema.WithPayload(payload))
		},
	}
}

// TestSkillsSlashNotice_五种文案形态 A3/A4：锁定与 Python 一致的四种失败/成功形态，
// 防止后续被"统一文案"优化掉。
func TestSkillsSlashNotice_五种文案形态(t *testing.T) {
	newMsg := func() *schema.Message {
		return schema.NewReqMessage("feishu_test", "sess-1", schema.ReqMethodChatSend,
			json.RawMessage(`{"content":"/skills list"}`), schema.WithProvider("feishu"))
	}

	t.Run("未连接_全角冒号", func(t *testing.T) {
		mh := createTestMessageHandlerWithTransport()
		mh.skillsSlashNotice(newMsg())
		notice := waitNotice(t, mh)
		assert.Equal(t, "获取技能列表失败：AgentClient 未连接", notice.Payload["error"])
	})

	t.Run("成功_payload透传", func(t *testing.T) {
		_, mh := newFakeAgentServer(t, skillsReplies(map[string]any{
			"skills": []any{map[string]any{"name": "s1"}},
		}, true))
		mh.skillsSlashNotice(newMsg())
		notice := waitNotice(t, mh)
		assert.Nil(t, notice.Payload["error"])
		assert.NotNil(t, notice.Payload["skills"], "成功响应应逐键透传 payload")
	})

	t.Run("成功_payload为空_data为nil", func(t *testing.T) {
		_, mh := newFakeAgentServer(t, skillsReplies(nil, true))
		mh.skillsSlashNotice(newMsg())
		notice := waitNotice(t, mh)
		val, exists := notice.Payload["data"]
		assert.True(t, exists, "payload 为 nil 时应回 {\"data\": nil}")
		assert.Nil(t, val)
	})

	t.Run("失败_带error_ASCII冒号", func(t *testing.T) {
		_, mh := newFakeAgentServer(t, skillsReplies(map[string]any{"error": "boom"}, false))
		mh.skillsSlashNotice(newMsg())
		notice := waitNotice(t, mh)
		assert.Equal(t, "获取技能列表失败: boom", notice.Payload["error"])
	})

	t.Run("失败_无error", func(t *testing.T) {
		_, mh := newFakeAgentServer(t, skillsReplies(nil, false))
		mh.skillsSlashNotice(newMsg())
		notice := waitNotice(t, mh)
		assert.Equal(t, "获取技能列表失败", notice.Payload["error"])
	})
}
```

- [ ] **Step 2: 跑测试**

Run: `go test -run 'TestSkillsSlashNotice_五种文案形态' ./internal/swarm/gateway/message_handler/ -v`
Expected: 5 个 subtest 全 PASS。若有失败，说明 A3/A4 的「等价」结论不成立，**停下来回到 spec 修正**，不要改生产代码迁就测试。

- [ ] **Step 3: Commit**

```bash
git add internal/swarm/gateway/message_handler/slash_cmd_test.go
git commit -m "test(message_handler): 锁定 /skills list 五种文案形态（A3/A4 等价确认）"
```

---

### Task 5: A1+A2 `/branch` 取消改同步 + 补日志

**Files:**
- Modify: `internal/swarm/gateway/message_handler/slash_cmd.go:331-410`
- Test: `internal/swarm/gateway/message_handler/slash_cmd_test.go`

- [ ] **Step 1: 写失败测试（时序）**

追加：

```go
// branchReplies 构造 /branch 需要的两个假响应。
func branchReplies() map[string]fakeReply {
	return map[string]fakeReply{
		string(schema.ReqMethodChatCancel): func(rid string, _ map[string]any) *schema.AgentResponse {
			return schema.NewAgentResponse(rid, "feishu_test", schema.WithPayload(map[string]any{}))
		},
		string(schema.ReqMethodSessionFork): func(rid string, _ map[string]any) *schema.AgentResponse {
			return schema.NewAgentResponse(rid, "feishu_test", schema.WithPayload(map[string]any{
				"session_id": "fork-1",
				"title":      "分支标题",
			}))
		},
	}
}

// seedChannelSession 预置某个消息对应的渠道状态 session_id。
func seedChannelSession(mh *MessageHandler, msg *schema.Message, sid string) {
	mh.statesMu.Lock()
	defer mh.statesMu.Unlock()
	mh.channelStates[getChannelStateKey(msg.ChannelID, msg.SessionID)] = &ChannelControlState{SessionID: sid}
}

// TestBranchSlashNotice_取消先于通知 A1：Python await 取消后再发通知。
// 给 chat.cancel 加 100ms 延迟：同步实现下返回时必已收到 cancel；异步实现下为 0。
func TestBranchSlashNotice_取消先于通知(t *testing.T) {
	f, mh := newFakeAgentServer(t, branchReplies())
	f.setDelay(string(schema.ReqMethodChatCancel), 100*time.Millisecond)

	msg := schema.NewReqMessage("feishu_test", "sess-1", schema.ReqMethodChatSend,
		json.RawMessage(`{}`), schema.WithProvider("feishu"))
	seedChannelSession(mh, msg, "sess-1")

	mh.branchSlashNotice(msg, command_parser.ParsedChannelControl{
		Action:     command_parser.ActionBranchOK,
		BranchName: "my-branch",
	})

	assert.Equal(t, 1, f.methodCount(string(schema.ReqMethodChatCancel)),
		"取消必须同步完成（异步实现下此处为 0）")
	assert.Equal(t, 1, f.methodCount(string(schema.ReqMethodSessionFork)))

	notice := waitNoticeContaining(t, mh, "已分叉会话")
	content, _ := notice.Payload["content"].(string)
	assert.Contains(t, content, "分支标题")
}

// TestBranchSlashNotice_成功回填字段 验证 payload 缺字段时的回落。
func TestBranchSlashNotice_成功回填字段(t *testing.T) {
	replies := branchReplies()
	replies[string(schema.ReqMethodSessionFork)] = func(rid string, _ map[string]any) *schema.AgentResponse {
		return schema.NewAgentResponse(rid, "feishu_test", schema.WithPayload(map[string]any{}))
	}
	_, mh := newFakeAgentServer(t, replies)

	msg := schema.NewReqMessage("feishu_test", "sess-1", schema.ReqMethodChatSend,
		json.RawMessage(`{}`), schema.WithProvider("feishu"))
	seedChannelSession(mh, msg, "sess-1")

	mh.branchSlashNotice(msg, command_parser.ParsedChannelControl{
		Action:     command_parser.ActionBranchOK,
		BranchName: "named",
	})

	notice := waitNoticeContaining(t, mh, "已分叉会话")
	content, _ := notice.Payload["content"].(string)
	assert.Contains(t, content, "named")
	state := mh.GetOrCreateChannelState(msg)
	assert.NotEmpty(t, state.SessionID, "回落后应写入生成的 newSID")
}
```

- [ ] **Step 2: 跑测试确认失败**

Run: `go test -run 'TestBranchSlashNotice' ./internal/swarm/gateway/message_handler/ -v`
Expected: `TestBranchSlashNotice_取消先于通知` FAIL，`取消必须同步完成（异步实现下此处为 0）` 实际 0

- [ ] **Step 3: 改实现**

`branchSlashNotice` 末尾把异步取消改为同步，并在成功分支补日志：

```go
	oldSID := state.SessionID
	state.SessionID = forkSID

	// A1: Python L994 `await _cancel_agent_work_for_session(msg, old_sid)`（默认
	// publish_interrupt_result=True）。Go 原实现用 go 异步，通知会先于取消完成发出。
	if oldSID != "" {
		mh.CancelAgentWorkForSession(context.Background(), msg, oldSID, true)
	}

	// A2: Python L1001-1004 成功日志
	logger.Info(logComponent).
		Str("event_type", "branch_slash_done").
		Str("source_session_id", sourceSID).
		Str("fork_session_id", forkSID).
		Str("title", forkTitle).
		Msg("/branch completed")

	mh.sendChannelNotice(msg, map[string]any{"content": fmt.Sprintf("[收到 /branch 指令] 已分叉会话「%s」，当前已切换到新会话。", forkTitle)})
```

并在 `resp.OK=false` 分支补 Warn：

```go
	if !resp.OK {
		errMsg := "session.fork failed"
		if resp.Payload != nil {
			if e, ok := resp.Payload["error"].(string); ok && e != "" {
				errMsg = e
			}
		}
		// A2: Python L1005-1009 `except ValueError` 分支
		logger.Warn(logComponent).
			Str("event_type", "branch_slash_rejected").
			Str("session_id", sourceSID).
			Msgf("/branch rejected: %s", errMsg)
		mh.sendChannelNotice(msg, map[string]any{"error": fmt.Sprintf("分叉失败：%s", errMsg)})
		return
	}
```

- [ ] **Step 4: 跑测试确认通过**

Run: `go test -run 'TestBranchSlashNotice|TestHandleChannelControl_BranchOK' ./internal/swarm/gateway/message_handler/ -v`
Expected: 全 PASS

- [ ] **Step 5: Commit**

```bash
git add internal/swarm/gateway/message_handler/slash_cmd.go internal/swarm/gateway/message_handler/slash_cmd_test.go
git commit -m "fix(message_handler): /branch 取消改同步 + 补成功/拒绝日志"
```

---

### Task 6: A5+A6 `/rewind` 删 preview 兜底 + 补日志与 rewind_context

**Files:**
- Modify: `internal/swarm/gateway/message_handler/slash_cmd.go:438-519`
- Test: `internal/swarm/gateway/message_handler/slash_cmd_test.go`

- [ ] **Step 1: 写失败测试**

追加：

```go
// rewindReplies 构造 /rewind 的假响应。
func rewindReplies(ok bool) map[string]fakeReply {
	return map[string]fakeReply{
		string(schema.ReqMethodChatCancel): func(rid string, _ map[string]any) *schema.AgentResponse {
			return schema.NewAgentResponse(rid, "feishu_test", schema.WithPayload(map[string]any{}))
		},
		string(schema.ReqMethodSessionRewind): func(rid string, _ map[string]any) *schema.AgentResponse {
			if !ok {
				return schema.NewAgentResponse(rid, "feishu_test",
					schema.WithResponseOK(false),
					schema.WithPayload(map[string]any{"error": "boom"}))
			}
			return schema.NewAgentResponse(rid, "feishu_test", schema.WithPayload(map[string]any{
				"content_preview":   "hello world",
				"remaining_records": 3,
				"removed_records":   2,
				"rewind_context":    true,
			}))
		},
	}
}

// TestRewindSlashNotice_E2A成功 A5/A6：preview 只取 content_preview（无兜底），
// 且读 rewind_context。
func TestRewindSlashNotice_E2A成功(t *testing.T) {
	_, mh := newFakeAgentServer(t, rewindReplies(true))

	msg := schema.NewReqMessage("feishu_test", "sess-1", schema.ReqMethodChatSend,
		json.RawMessage(`{}`), schema.WithProvider("feishu"))
	seedChannelSession(mh, msg, "sess-1")

	mh.rewindSlashNotice(msg, command_parser.ParsedChannelControl{
		Action:     command_parser.ActionRewindConfirm,
		RewindTurn: 2,
	})

	notice := waitNoticeContaining(t, mh, "已回退到第 2 轮")
	content, _ := notice.Payload["content"].(string)
	assert.Contains(t, content, `"hello world"`)
	assert.Contains(t, content, "删除 2 条记录，剩余 3 条。")
	assert.NotContains(t, content, "上下文未同步截断", "E2A 成功路径不应带兜底提示")
}

// TestRewindSlashNotice_无活跃会话 覆盖早退分支。
func TestRewindSlashNotice_无活跃会话(t *testing.T) {
	mh := createTestMessageHandlerWithTransport()
	msg := schema.NewReqMessage("feishu_test", "sess-1", schema.ReqMethodChatSend,
		json.RawMessage(`{}`), schema.WithProvider("feishu"))

	mh.rewindSlashNotice(msg, command_parser.ParsedChannelControl{
		Action:     command_parser.ActionRewindConfirm,
		RewindTurn: 1,
	})
	notice := waitNotice(t, mh)
	assert.Equal(t, "当前无活跃会话，无法回退", notice.Payload["error"])
}

// TestRewindSlashNotice_兜底失败 覆盖 E2A 拒绝后本地回退失败的路径。
func TestRewindSlashNotice_兜底失败(t *testing.T) {
	_, mh := newFakeAgentServer(t, rewindReplies(false))

	msg := schema.NewReqMessage("feishu_test", "sess-1", schema.ReqMethodChatSend,
		json.RawMessage(`{}`), schema.WithProvider("feishu"))
	seedChannelSession(mh, msg, "sid-without-history")

	mh.rewindSlashNotice(msg, command_parser.ParsedChannelControl{
		Action:     command_parser.ActionRewindConfirm,
		RewindTurn: 1,
	})

	notice := waitNoticeContaining(t, mh, "回退失败")
	assert.Contains(t, notice.Payload["error"], "回退失败")
}
```

- [ ] **Step 2: 跑测试确认基线**

Run: `go test -run 'TestRewindSlashNotice' ./internal/swarm/gateway/message_handler/ -v`
Expected: `TestRewindSlashNotice_E2A成功` PASS（说明现状已对），另两条 PASS。这一条测试是**形态锁定**而非先失败：A5 的兜底删除在 `result.ContentPreview` 为空时才生效，E2A 成功路径本来就用 payload 值。

- [ ] **Step 3: 改实现（删兜底 + 补日志）**

把 `rewindSlashNotice` 的 E2A 成功分支替换为：

```go
		} else if resp.OK {
			// E2A 成功路径
			preview := ""
			remaining := 0
			removed := 0
			rewindContext := false
			if resp.Payload != nil {
				if p, ok := resp.Payload["content_preview"].(string); ok {
					// M-07: Python preview[:50] 按字符切，Go 按 rune 对齐口径，
					// 否则 CJK 多字节字符在字节边界截断产生非法 UTF-8
					preview = truncRunes(p, 50)
				}
				remaining = payloadToInt(resp.Payload["remaining_records"])
				removed = payloadToInt(resp.Payload["removed_records"])
				// A6: Python L1096 读取 rewind_context（仅用于日志）
				rewindContext, _ = resp.Payload["rewind_context"].(bool)
			}
			// A6: Python L1104-1107 成功日志
			logger.Info(logComponent).
				Str("event_type", "rewind_slash_done_e2a").
				Str("session_id", targetSID).
				Int("turn_index", turn).
				Bool("rewind_context", rewindContext).
				Msg("/rewind completed via E2A")
			mh.sendChannelNotice(msg, map[string]any{"content": fmt.Sprintf("[收到 /rewind 指令] 已回退到第 %d 轮（\"%s\"），删除 %d 条记录，剩余 %d 条。", turn, preview, removed, remaining)})
			return
		} else {
```

把 fallback 成功分支替换为：

```go
	// A5: Python L1093/L1117 只用 content_preview，无兜底（原实现在为空时
	// 回填 result.Content，属自造行为，删除）。
	preview := result.ContentPreview
	// A6: Python L1128-1131 fallback 成功日志
	logger.Info(logComponent).
		Str("event_type", "rewind_slash_done_local").
		Str("session_id", targetSID).
		Int("turn_index", turn).
		Int("remaining_records", result.RemainingRecords).
		Int("removed_records", result.RemovedRecords).
		Msg("/rewind completed via local fallback")
	mh.sendChannelNotice(msg, map[string]any{"content": fmt.Sprintf(
		"[收到 /rewind 指令] 已回退到第 %d 轮（\"%s\"），删除 %d 条记录，剩余 %d 条。（注意：上下文未同步截断）",
		turn, preview, result.RemovedRecords, result.RemainingRecords)})
```

- [ ] **Step 4: 跑测试确认通过**

Run: `go test -run 'TestRewindSlash' ./internal/swarm/gateway/message_handler/ -v`
Expected: 全 PASS

- [ ] **Step 5: Commit**

```bash
git add internal/swarm/gateway/message_handler/slash_cmd.go internal/swarm/gateway/message_handler/slash_cmd_test.go
git commit -m "fix(message_handler): /rewind 删 preview 兜底 + 补成功日志与 rewind_context"
```

---

### Task 7: `command.model` 全量实现

**Files:**
- Modify: `internal/swarm/server/handle_command.go:245-274`
- Create: `internal/swarm/server/handle_command_model_test.go`

- [ ] **Step 1: 写失败测试**

新建 `internal/swarm/server/handle_command_model_test.go`：

```go
package server

import (
	"context"
	"encoding/json"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/uapclaw/uapclaw-go/internal/swarm/schema"
)

// newModelRequest 构造 command.model 请求。
func newModelRequest(t *testing.T, params string) *schema.AgentRequest {
	t.Helper()
	return &schema.AgentRequest{
		RequestID: "r-model-1",
		ChannelID: "c-model",
		Params:    json.RawMessage(params),
	}
}

// TestHandleCommandModel_AddModel add_model 分支。
func TestHandleCommandModel_AddModel(t *testing.T) {
	s := &AgentServer{}
	resp, err := s.handleCommandModel(context.Background(),
		newModelRequest(t, `{"action":"add_model","target":"  qwen-max  "}`))

	require.NoError(t, err)
	assert.True(t, resp.OK)
	assert.Equal(t, "model_added", resp.Payload["type"])
	assert.Equal(t, "qwen-max", resp.Payload["name"], "target 应去首尾空格")
}

// TestHandleCommandModel_SwitchModel_空EnvUpdates 复刻 Python 的 ok=false。
func TestHandleCommandModel_SwitchModel_空EnvUpdates(t *testing.T) {
	s := &AgentServer{}
	resp, err := s.handleCommandModel(context.Background(),
		newModelRequest(t, `{"action":"switch_model","model":"m2"}`))

	require.NoError(t, err)
	assert.False(t, resp.OK, "无 env_updates 应 ok=false（Python 不是抛异常）")
	assert.Equal(t, "No env_updates provided", resp.Payload["error"])
}

// TestHandleCommandModel_SwitchModel_正常 写入环境变量并回 current。
func TestHandleCommandModel_SwitchModel_正常(t *testing.T) {
	t.Setenv("MODEL_NAME", "m2")
	s := &AgentServer{}
	resp, err := s.handleCommandModel(context.Background(),
		newModelRequest(t, `{"action":"switch_model","model":"m2","env_updates":{"MODEL_NAME":"m2"}}`))

	require.NoError(t, err)
	assert.True(t, resp.OK)
	assert.Equal(t, "m2", resp.Payload["current"])
	assert.Equal(t, "m2", resp.Payload["requested"])
	assert.Equal(t, "switched", resp.Payload["type"])
	assert.Equal(t, true, resp.Payload["applied"])
	assert.Equal(t, "m2", getenvOr("MODEL_NAME", ""), "os.environ 应已写入")
}

// TestHandleCommandModel_SwitchModel_非字符串Env 复刻 Python os.environ TypeError。
func TestHandleCommandModel_SwitchModel_非字符串Env(t *testing.T) {
	s := &AgentServer{}
	resp, err := s.handleCommandModel(context.Background(),
		newModelRequest(t, `{"action":"switch_model","model":"m2","env_updates":{"TIMEOUT":30}}`))

	require.NoError(t, err)
	assert.False(t, resp.OK, "非字符串 env 值必须失败（Python TypeError 路径）")
	errText, _ := resp.Payload["error"].(string)
	assert.Contains(t, errText, "str expected")
}

// TestHandleCommandModel_缺省Action 返回当前模型与可用列表。
func TestHandleCommandModel_缺省Action(t *testing.T) {
	t.Setenv("MODEL_NAME", "m-cur")
	s := &AgentServer{}
	resp, err := s.handleCommandModel(context.Background(), newModelRequest(t, `{}`))

	require.NoError(t, err)
	assert.True(t, resp.OK)
	assert.Equal(t, "m-cur", resp.Payload["current"])
	assert.Equal(t, []any{"default-model"}, resp.Payload["available"])
}

// TestHandleCommandModel_ModelName未设置 current 回退 unknown。
// Python os.getenv("MODEL_NAME","unknown") 的 default 只在**未设置**时生效，
// 已设为空串时返回空串，因此这里必须真正 Unsetenv。
func TestHandleCommandModel_ModelName未设置(t *testing.T) {
	prev, had := os.LookupEnv("MODEL_NAME")
	require.NoError(t, os.Unsetenv("MODEL_NAME"))
	t.Cleanup(func() {
		if had {
			_ = os.Setenv("MODEL_NAME", prev)
		}
	})

	s := &AgentServer{}
	resp, err := s.handleCommandModel(context.Background(), newModelRequest(t, `{}`))

	require.NoError(t, err)
	assert.Equal(t, "unknown", resp.Payload["current"])
}

// TestMaskModelEnvForLog API_KEY 掩码。
func TestMaskModelEnvForLog(t *testing.T) {
	in := map[string]any{"API_KEY": "sk-secret", "MODEL_NAME": "m"}
	out := maskModelEnvForLog(in)
	assert.Equal(t, "***", out["API_KEY"])
	assert.Equal(t, "m", out["MODEL_NAME"])
	assert.Equal(t, "sk-secret", in["API_KEY"], "不得修改入参")
}
```

- [ ] **Step 2: 跑测试确认失败**

Run: `go test -run 'TestHandleCommandModel|TestMaskModelEnvForLog' ./internal/swarm/server/ -v`
Expected: 编译失败或 `maskModelEnvForLog` undefined

- [ ] **Step 3: 改实现**

`handle_command.go` 顶部 import 增加（保持分组）：

```go
	"os"
	"strings"

	commmem "github.com/uapclaw/uapclaw-go/internal/swarm/agents/harness/common/memory"
```

把 `handleCommandModel`（含注释）整体替换为：

```go
// handleCommandModel 处理 command.model 请求。
//
// Python: _handle_command_model (L2277-2352) 三分支：
//   add_model    → {"type":"model_added","name":target}
//   switch_model → 写 os.environ + 清内存配置缓存 + 重载 agent 配置
//   其它/缺省    → {"current":MODEL_NAME 或 "unknown","available":["default-model"]}
//
// 失败一律以 ok=false + payload.error 返回，**不返回 error**：返回 error 会被
// handle_envelope.go 转成框架错误帧，与 Python 的 ok=false 响应不一致。
func (s *AgentServer) handleCommandModel(ctx context.Context, request *schema.AgentRequest) (*schema.AgentResponse, error) {
	params := parseModelCommandParams(request.Params)

	switch params.Action {
	case "add_model":
		target := strings.TrimSpace(params.Target)
		logger.Info(logComponent).
			Str("event_type", "command_model_add").
			Str("request_id", request.RequestID).
			Str("target", target).
			Msg("command.model add_model")
		return schema.NewAgentResponse(request.RequestID, request.ChannelID,
			schema.WithPayload(map[string]any{"type": "model_added", "name": target})), nil

	case "switch_model":
		target := strings.TrimSpace(params.Model)
		// A: Python 先掩码 API_KEY 再打日志
		logger.Info(logComponent).
			Str("event_type", "command_model_switch").
			Str("request_id", request.RequestID).
			Str("target", target).
			Any("env_updates", maskModelEnvForLog(params.EnvUpdates)).
			Msg("command.model switch_model")

		if len(params.EnvUpdates) == 0 {
			// Python: ok=False, {"error": "No env_updates provided"}（不记 error 日志）
			return schema.NewAgentResponse(request.RequestID, request.ChannelID,
				schema.WithResponseOK(false),
				schema.WithPayload(map[string]any{"error": "No env_updates provided"})), nil
		}

		for k, v := range params.EnvUpdates {
			strVal, isStr := v.(string)
			if !isStr {
				// Python: os.environ[k] = v 对非 str 抛 TypeError，被外层 except 捕获
				return modelErrorResponse(request, fmt.Sprintf("str expected, not %T", v)), nil
			}
			if err := os.Setenv(k, strVal); err != nil {
				return modelErrorResponse(request, err.Error()), nil
			}
		}
		logger.Info(logComponent).
			Str("event_type", "command_model_env_updated").
			Str("model_name", getenvOr("MODEL_NAME", "unknown")).
			Msg("command.model os.environ updated")

		// Python: clear_config_cache()（失败仅 debug）
		commmem.ClearConfigCache()
		logger.Info(logComponent).
			Str("event_type", "command_model_cache_cleared").
			Msg("command.model config cache cleared")

		// Python: await reload_agents_config(None, env_updates)（失败仅 debug）
		if s.agentManager != nil {
			if err := s.agentManager.ReloadAgentsConfig(ctx, nil, stringEnvMap(params.EnvUpdates)); err != nil {
				logger.Debug(logComponent).
					Err(err).
					Msg("command.model reload_agents_config skipped")
			} else {
				logger.Info(logComponent).
					Str("event_type", "command_model_agents_reloaded").
					Msg("command.model agent config reloaded")
			}
		}

		current := getenvOr("MODEL_NAME", "unknown")
		logger.Info(logComponent).
			Str("event_type", "command_model_switched").
			Str("current", current).
			Msg("command.model switch completed")
		return schema.NewAgentResponse(request.RequestID, request.ChannelID,
			schema.WithPayload(map[string]any{
				"current":   current,
				"requested": target,
				"type":      "switched",
				"applied":   true,
			})), nil

	default:
		return schema.NewAgentResponse(request.RequestID, request.ChannelID,
			schema.WithPayload(map[string]any{
				"current":   getenvOr("MODEL_NAME", "unknown"),
				"available": []any{"default-model"},
			})), nil
	}
}
```

在「结构体」区（`handleCommandAddDir` 之前）追加参数结构体：

```go
// modelCommandParams command.model 请求参数
//
// Python: params.get("action"/"target"/"model"/"env_updates")
type modelCommandParams struct {
	// Action 动作：add_model / switch_model / 空
	Action string
	// Target add_model 的目标模型名
	Target string
	// Model switch_model 的目标模型名
	Model string
	// EnvUpdates 需写入进程环境变量的键值（保留 any 以便复刻 Python 的类型报错）
	EnvUpdates map[string]any
}
```

在「非导出函数」区追加：

```go
// parseModelCommandParams 解析 command.model 参数。
//
// Python: params = request.params or {}；str(params.get(...) or "").strip()
// 偏差 D7：Python str(None) == "None"，Go 的 JSON null 解到 string 为空串，
// 该边界不复刻（无实际调用方会传 null）。
func parseModelCommandParams(raw json.RawMessage) modelCommandParams {
	var p modelCommandParams
	if len(raw) == 0 {
		return p
	}
	var wire struct {
		Action     string         `json:"action"`
		Target     string         `json:"target"`
		Model      string         `json:"model"`
		EnvUpdates map[string]any `json:"env_updates"`
	}
	if err := json.Unmarshal(raw, &wire); err != nil {
		logger.Warn(logComponent).
			Err(err).
			Msg("command.model failed to parse params")
		return p
	}
	return modelCommandParams{
		Action:     wire.Action,
		Target:     wire.Target,
		Model:      wire.Model,
		EnvUpdates: wire.EnvUpdates,
	}
}

// modelErrorResponse 构造 ok=false 的 command.model 响应并记录错误日志。
//
// Python: except Exception → logger.exception + ok=False, {"error": str(e)}
func modelErrorResponse(request *schema.AgentRequest, msg string) *schema.AgentResponse {
	logger.Error(logComponent).
		Str("event_type", "LLM_CALL_ERROR").
		Str("method", "command.model").
		Str("request_id", request.RequestID).
		Msgf("command.model failed: %s", msg)
	return schema.NewAgentResponse(request.RequestID, request.ChannelID,
		schema.WithResponseOK(false),
		schema.WithPayload(map[string]any{"error": msg}))
}

// maskModelEnvForLog 返回掩码 API_KEY 后的副本。
//
// Python: {k: (v if k != "API_KEY" else "***") for k, v in env_updates.items()}
func maskModelEnvForLog(env map[string]any) map[string]any {
	masked := make(map[string]any, len(env))
	for k, v := range env {
		if k == "API_KEY" {
			masked[k] = "***"
			continue
		}
		masked[k] = v
	}
	return masked
}

// stringEnvMap 把已校验为字符串的 env 映射转成 map[string]string。
func stringEnvMap(env map[string]any) map[string]string {
	out := make(map[string]string, len(env))
	for k, v := range env {
		if s, ok := v.(string); ok {
			out[k] = s
		}
	}
	return out
}

// getenvOr 读取环境变量，未设置时返回缺省值（对齐 Python os.getenv 语义：
// 已设置为空串时返回空串，不返回缺省值）。
func getenvOr(key, def string) string {
	if v, ok := os.LookupEnv(key); ok {
		return v
	}
	return def
}
```

- [ ] **Step 4: 跑测试确认通过**

Run: `go test -run 'TestHandleCommandModel|TestMaskModelEnvForLog' ./internal/swarm/server/ -v`
Expected: 全 PASS

- [ ] **Step 5: Commit**

```bash
git add internal/swarm/server/handle_command.go internal/swarm/server/handle_command_model_test.go
git commit -m "feat(server): command.model 三分支全量实现（掩码 + os.environ 类型严格性）"
```

---

### Task 8: 文档与计划同步

**Files:**
- Modify: `IMPLEMENTATION_PLAN.md:698`
- Modify: `docs/superpowers/specs/2026-10-31-slash-command-10.4.4a-design.md`（补 D7）

- [ ] **Step 1: spec 补 D7**

在 spec 第五节表格 `D6` 行后追加：

```markdown
| D7 | `command.model` 的 `target`/`model` 为 JSON `null` 时 Go 得空串，Python `str(None)` 得 `"None"` | 无实际调用方会传 null；为对齐需实现 Python `str()` 语义，收益为零 |
```

- [ ] **Step 2: 计划表插入两行并给 10.3.24 加回填标记**

在 `IMPLEMENTATION_PLAN.md` 第 698 行（`| 10.4.4 | 🔄 | ...`）**之后**插入：

```markdown
| 10.4.4a | ✅ | Slash 命令处理（对账收口 + /model） | 8 项对账（A1/A2/A5/A6/A7/A8 改代码，A3/A4 核实等价）：handleChannelControl 顺序与日志点位、/branch 取消改同步、/rewind 删 preview 兜底、sendChannelNotice 补 params={}；command.model 三分支全量实现（API_KEY 掩码 + os.environ 非字符串报错）；/evolve 5 个命令已完整实现（deep_adapter_slash.go 563 行，非骨架）；message_handler 覆盖率 56.8%→≥85% | `command_parser/slash_command.py` · `message_handler.py` L347-1145 · `agent_ws_server.py` L2277-2352 |
| 10.4.4b | ⤵️ | Slash 命令处理（/sandbox 命令面） | 9 个 sub + Linux 平台守卫 + SANDBOX_BAD_REQUEST/SANDBOX_INTERNAL 双错误码；依赖 10.3.24 Sandbox 包与 jiuwenbox（21k 行 Python 守护进程） | `agent_ws_server.py` L2853-3510 |
```

把同一行（10.4.4）的状态 `🔄` 改回按实际：若本节收口完成则改为 `✅`，并在描述末尾追加 `；已拆分为 10.4.4a/10.4.4b`（**原文其余内容一个字不改**）。

在 10.3.24 行末尾追加：`；⤴️ /sandbox 命令面来源 10.4.4b`

- [ ] **Step 3: Commit**

```bash
git add IMPLEMENTATION_PLAN.md docs/superpowers/specs/2026-10-31-slash-command-10.4.4a-design.md
git commit -m "docs(plan): 10.4.4 拆分为 10.4.4a/10.4.4b 并同步进度"
```

---

### Task 9: 覆盖率与全量回归

- [ ] **Step 1: 目标包覆盖率**

Run: `go test -cover ./internal/swarm/gateway/message_handler/ ./internal/swarm/gateway/message_handler/command_parser/ ./internal/swarm/server/`
Expected: `message_handler` ≥85%，`command_parser` ≥94%，`server` 不低于改动前

- [ ] **Step 2: 逐函数核对三个 E2A 函数**

Run: `go test -coverprofile=/tmp/cov44a.out ./internal/swarm/gateway/message_handler/ >/dev/null && go tool cover -func=/tmp/cov44a.out | grep -E "skillsSlashNotice|branchSlashNotice|rewindSlashNotice"`
Expected: 三者均 ≥80%（`skillsSlashNotice` 应接近 100%）

- [ ] **Step 3: 全量回归（必须带 CI 同款标签）**

Run: `CGO_ENABLED=1 go test -tags "sqlite_fts5 test" ./... 2>&1 | grep -E "^(FAIL|ok.*FAIL)" | head -20`
Expected: 无 FAIL 行

- [ ] **Step 4: 静态检查**

Run: `go vet ./... && gofmt -l internal/swarm/gateway/message_handler internal/swarm/server`
Expected: vet 无输出，gofmt 无文件列表

- [ ] **Step 5: Commit（若上一步有格式修复）**

```bash
git add internal/swarm/gateway/message_handler/slash_cmd.go internal/swarm/server/handle_command.go
git commit -m "style: 10.4.4a 收口后格式修正"
```

---

## 自审记录

**Spec 覆盖检查**

| Spec 条目 | 对应任务 |
|---|---|
| A1 `/branch` 同步取消 | Task 5 |
| A2 `/branch` 补日志 | Task 5 |
| A3 非 dict payload 等价确认 | Task 4（形态锁定） |
| A4 五种文案等价确认 | Task 4 |
| A5 删 preview 兜底 | Task 6 |
| A6 补日志 + `rewind_context` | Task 6 |
| A7 顺序/提前返回/日志点位 | Task 3 |
| A8 `params={}` | Task 2 |
| §6 `command.model` 全量 | Task 7 |
| §7 测试计划 | Task 1（基建）+ Task 2-7 |
| §8 计划行同步 | Task 8 |
| §9 验收标准 | Task 9 |
| D1-D7 偏差登记 | Task 8（D7）+ spec 已有 D1-D6 |

**未覆盖项（有意）**：`/rewind` fallback **成功**路径需要真实 `history.json`（`sessionops.RewindSession` 依赖 `workspace.AgentSessionsDir()` + `path.ResetCache()`，与同包其他用例存在全局缓存互扰风险）。Task 6 只覆盖 fallback **失败**路径；`rewindSlashNotice` 预期落在 80-90%，由包级 ≥85% 兜住。若 Task 9 Step 2 显示该函数 <70%，再补一个 `t.Setenv("UAPCLAW_DATA_DIR", t.TempDir()) + pathutil.ResetCache()` 的用例。

**类型一致性**：`fakeReply`、`modelCommandParams`、`maskModelEnvForLog`、`getenvOr`、`stringEnvMap`、`ParsedControlActionString` 在各任务中的签名一致，无重命名。

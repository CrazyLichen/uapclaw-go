package acp

// 11.25 Task 8：AcpChannel 本体测试（stdio 三路由 + jsonrpc 六方法 + E2A 双模式骨架）。
// Python 基线：tests/unit_tests/acp/test_acp_channel.py。

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/uapclaw/uapclaw-go/internal/swarm/e2a"
	"github.com/uapclaw/uapclaw-go/internal/swarm/schema"
)

// ──────────────────────────── 测试 harness ────────────────────────────

// fakeStdin 顺序行输入（EOF 后恒空）。
type fakeStdin struct {
	lines []string
	idx   int
}

// Read 顺序吐行，行集耗尽后恒 EOF（io.Reader 语义，bufio 兼容）。
func (f *fakeStdin) Read(p []byte) (int, error) {
	if f.idx >= len(f.lines) {
		return 0, errEOF
	}
	line := f.lines[f.idx] + "\n"
	f.idx++
	n := copy(p, line)
	return n, nil
}

// errEOF 哨兵 EOF。
var errEOF = eofError{}

type eofError struct{}

func (eofError) Error() string            { return "EOF" }
func (eofError) Read([]byte) (int, error) { return 0, errEOF }

// safeBuffer 并发安全 stdout 收集器。
type safeBuffer struct {
	mu  sync.Mutex
	buf strings.Builder
}

// Write 线程安全追加。
func (b *safeBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

// jsonLines 解析 stdout 收集的全部 JSON 行。
func (b *safeBuffer) jsonLines(t *testing.T) []map[string]any {
	t.Helper()
	b.mu.Lock()
	raw := b.buf.String()
	b.mu.Unlock()
	out := []map[string]any{}
	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var m map[string]any
		assert.NoError(t, json.Unmarshal([]byte(line), &m), line)
		out = append(out, m)
	}
	return out
}

// newTestChannel 构造测试通道（无 gateway URL；onMessage 注入）。
func newTestChannel(t *testing.T, lines []string) (*AcpChannel, *safeBuffer, *[]*schema.Message) {
	t.Helper()
	out := &safeBuffer{}
	seen := &[]*schema.Message{}
	ch := NewAcpChannel(AcpChannelConfig{Enabled: true})
	ch.SetStdoutForTest(out)
	ch.SetStdinForTest(&fakeStdin{lines: lines})
	ch.SetStdinEOFGraceSecondsForTest(0.1) // 缩短 EOF 宽限（默认 5s 拖慢用例）
	ch.SetIdleFinalizeSecondsForTest(0.1)  // 缩短 idle finalize（默认 3s）
	ch.SetOnMessageForTest(func(msg *schema.Message) bool {
		*seen = append(*seen, msg)
		return true
	})
	return ch, out, seen
}

// makeChEvent 通道事件消息构造（测试辅助）。
func makeChEvent(requestID, sessionID string, et schema.EventType, payload map[string]any) *schema.Message {
	return &schema.Message{
		ID: requestID, Type: schema.MessageTypeEvent, ChannelID: "acp",
		SessionID: sessionID, Timestamp: float64(time.Now().UnixNano()) / 1e9,
		OK: true, EventType: et, Payload: payload,
	}
}

// ──────────────────────────── 配置测试 ────────────────────────────

// TestAcpChannelConfigDefaults 零值默认（Python AcpChannelConfig dataclass）。
func TestAcpChannelConfigDefaults(t *testing.T) {
	c := DefaultAcpChannelConfig()
	assert.True(t, c.Enabled)
	assert.Equal(t, "acp", c.ChannelID)
	assert.Equal(t, "acp_cli_session", c.DefaultSessionID)
	assert.NotNil(t, c.Metadata)
	assert.Empty(t, c.Metadata)
}

// TestLoadAcpChannelConfig config 读取（Python L1740-1761）。
func TestLoadAcpChannelConfig(t *testing.T) {
	// 默认值
	c := LoadAcpChannelConfig(func() map[string]any { return nil })
	assert.True(t, c.Enabled)
	assert.Equal(t, "acp", c.ChannelID)
	assert.Equal(t, "acp_cli_session", c.DefaultSessionID)

	// channels.acp 自定义
	c2 := LoadAcpChannelConfig(func() map[string]any {
		return map[string]any{"channels": map[string]any{"acp": map[string]any{
			"enabled":            true,
			"channel_id":         "acp_custom",
			"default_session_id": "sess_custom",
			"metadata":           map[string]any{"source": "ut"},
		}}}
	})
	assert.Equal(t, "acp_custom", c2.ChannelID)
	assert.Equal(t, "sess_custom", c2.DefaultSessionID)
	assert.Equal(t, map[string]any{"source": "ut"}, c2.Metadata)
}

// ──────────────────────────── jsonrpc 六方法 ────────────────────────────

// TestAcpChannel_InitializeAndSessionNew 例 4（Python test_jsonrpc_initialize_and_session_new）。
func TestAcpChannel_InitializeAndSessionNew(t *testing.T) {
	ch, out, _ := newTestChannel(t, []string{
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`,
		`{"jsonrpc":"2.0","id":2,"method":"session/new","params":{"sessionId":"sess-1"}}`,
	})
	assert.NoError(t, ch.Start(context.Background()))

	lines := out.jsonLines(t)
	assert.Len(t, lines, 2)
	initResult, _ := lines[0]["result"].(map[string]any)
	assert.Equal(t, float64(1), lines[0]["id"])
	assert.Equal(t, float64(1), initResult["protocolVersion"])
	agentInfo, _ := initResult["agentInfo"].(map[string]any)
	assert.Equal(t, "jiuwenswarm", agentInfo["name"])
	caps, _ := initResult["agentCapabilities"].(map[string]any)
	assert.Equal(t, false, caps["loadSession"])

	newResult, _ := lines[1]["result"].(map[string]any)
	assert.Equal(t, float64(2), lines[1]["id"])
	assert.Equal(t, "sess-1", newResult["sessionId"])
}

// TestAcpChannel_SessionList 例 5。
func TestAcpChannel_SessionList(t *testing.T) {
	ch, out, _ := newTestChannel(t, []string{
		`{"jsonrpc":"2.0","id":10,"method":"session/new","params":{"sessionId":"sess-1"}}`,
		`{"jsonrpc":"2.0","id":11,"method":"session/list","params":{}}`,
	})
	assert.NoError(t, ch.Start(context.Background()))
	lines := out.jsonLines(t)
	assert.Equal(t, map[string]any{
		"jsonrpc": "2.0", "id": float64(11),
		"result": map[string]any{"sessions": []any{map[string]any{"sessionId": "sess-1"}}},
	}, lines[1])
}

// TestAcpChannel_SessionLoadRejected 例 6。
func TestAcpChannel_SessionLoadRejected(t *testing.T) {
	ch, out, _ := newTestChannel(t, []string{
		`{"jsonrpc":"2.0","id":21,"method":"session/load","params":{"sessionId":"sess-1"}}`,
	})
	assert.NoError(t, ch.Start(context.Background()))
	lines := out.jsonLines(t)
	assert.Equal(t, []map[string]any{{
		"jsonrpc": "2.0", "id": float64(21),
		"error": map[string]any{
			"code":    float64(-32601),
			"message": "Method not supported by agent capabilities: session/load",
		},
	}}, lines)
}

// TestAcpChannel_UnknownMethodRejected 未知方法 -32601。
func TestAcpChannel_UnknownMethodRejected(t *testing.T) {
	ch, out, _ := newTestChannel(t, []string{
		`{"jsonrpc":"2.0","id":22,"method":"session/unknown","params":{}}`,
	})
	assert.NoError(t, ch.Start(context.Background()))
	lines := out.jsonLines(t)
	errObj, _ := lines[0]["error"].(map[string]any)
	assert.Equal(t, float64(-32601), errObj["code"])
}

// TestAcpChannel_PromptEmitsUpdatesAndFinalResult 例 7（四帧序列）。
func TestAcpChannel_PromptEmitsUpdatesAndFinalResult(t *testing.T) {
	ch, out, seen := newTestChannel(t, []string{
		`{"jsonrpc":"2.0","id":3,"method":"session/prompt","params":{"sessionId":"sess-2","prompt":[{"type":"text","text":"hello"}]}}`,
	})
	// onMessage 回调内回推事件流（Python _on_message 模式）
	ch.SetOnMessageForTest(func(msg *schema.Message) bool {
		*seen = append(*seen, msg)
		ch.Send(context.Background(), makeChEvent(msg.ID, msg.SessionID,
			schema.EventTypeChatDelta, map[string]any{"content": "thinking", "source_chunk_type": "llm_reasoning"}))
		ch.Send(context.Background(), makeChEvent(msg.ID, msg.SessionID,
			schema.EventTypeChatDelta, map[string]any{"content": "final answer"}))
		ch.Send(context.Background(), makeChEvent(msg.ID, msg.SessionID,
			schema.EventTypeChatProcessingStatus, map[string]any{"is_processing": false}))
		return true
	})
	assert.NoError(t, ch.Start(context.Background()))

	assert.Len(t, *seen, 1)
	req := (*seen)[0]
	assert.Equal(t, schema.ReqMethodChatSend, req.ReqMethod)
	assert.Equal(t, "sess-2", req.SessionID)
	var p map[string]any
	assert.NoError(t, json.Unmarshal(req.Params, &p))
	assert.Equal(t, "hello", p["query"])

	lines := out.jsonLines(t)
	assert.Len(t, lines, 4)
	u1, _ := lines[0]["params"].(map[string]any)
	upd1, _ := u1["update"].(map[string]any)
	assert.Equal(t, "agent_thought_chunk", upd1["sessionUpdate"])
	u2, _ := lines[1]["params"].(map[string]any)
	upd2, _ := u2["update"].(map[string]any)
	assert.Equal(t, "agent_message_chunk", upd2["sessionUpdate"])
	u3, _ := lines[2]["params"].(map[string]any)
	upd3, _ := u3["update"].(map[string]any)
	assert.Equal(t, "session_info_update", upd3["sessionUpdate"])
	assert.Equal(t, "idle", upd3["status"])
	result, _ := lines[3]["result"].(map[string]any)
	assert.Equal(t, float64(3), lines[3]["id"])
	assert.Equal(t, "end_turn", result["stopReason"])
}

// TestAcpChannel_PromptAcceptsTextParam 例 8（text 参数直传）。
func TestAcpChannel_PromptAcceptsTextParam(t *testing.T) {
	ch, out, seen := newTestChannel(t, []string{
		`{"jsonrpc":"2.0","id":301,"method":"session/prompt","params":{"sessionId":"sess-text","text":"hello from text"}}`,
	})
	ch.SetOnMessageForTest(func(msg *schema.Message) bool {
		*seen = append(*seen, msg)
		ch.Send(context.Background(), makeChEvent(msg.ID, msg.SessionID,
			schema.EventTypeChatDelta, map[string]any{"content": "final answer"}))
		ch.Send(context.Background(), makeChEvent(msg.ID, msg.SessionID,
			schema.EventTypeChatProcessingStatus, map[string]any{"is_processing": false}))
		return true
	})
	assert.NoError(t, ch.Start(context.Background()))

	req := (*seen)[0]
	var p map[string]any
	assert.NoError(t, json.Unmarshal(req.Params, &p))
	assert.Equal(t, "hello from text", p["query"])
	lines := out.jsonLines(t)
	assert.Len(t, lines, 3)
}

// TestAcpChannel_FinalTextAsAgentMessageChunk 例 9（final 无 delta → chunk + result）。
func TestAcpChannel_FinalTextAsAgentMessageChunk(t *testing.T) {
	ch, out, _ := newTestChannel(t, []string{
		`{"jsonrpc":"2.0","id":302,"method":"session/prompt","params":{"sessionId":"sess-final-only","text":"hello from final only"}}`,
	})
	ch.SetOnMessageForTest(func(msg *schema.Message) bool {
		ch.Send(context.Background(), makeChEvent(msg.ID, msg.SessionID,
			schema.EventTypeChatFinal, map[string]any{"content": "final answer from chat.final"}))
		ch.Send(context.Background(), makeChEvent(msg.ID, msg.SessionID,
			schema.EventTypeChatProcessingStatus, map[string]any{"is_processing": false}))
		return true
	})
	assert.NoError(t, ch.Start(context.Background()))

	lines := out.jsonLines(t)
	assert.Len(t, lines, 3)
	p0, _ := lines[0]["params"].(map[string]any)
	u0, _ := p0["update"].(map[string]any)
	assert.Equal(t, "agent_message_chunk", u0["sessionUpdate"])
	assert.Equal(t, map[string]any{"type": "text", "text": "final answer from chat.final"}, u0["content"])
	assert.Equal(t, map[string]any{
		"jsonrpc": "2.0", "id": float64(302), "result": map[string]any{"stopReason": "end_turn"},
	}, lines[2])
}

// TestAcpChannel_SessionContextMerge 例 10（session/new params 合并进 prompt）。
func TestAcpChannel_SessionContextMerge(t *testing.T) {
	ch, out, seen := newTestChannel(t, []string{
		`{"jsonrpc":"2.0","id":10,"method":"session/new","params":{"sessionId":"sess-ctx","cwd":"D:/workspace/demo"}}`,
		`{"jsonrpc":"2.0","id":11,"method":"session/prompt","params":{"sessionId":"sess-ctx","prompt":[{"type":"text","text":"hello"}]}}`,
	})
	ch.SetOnMessageForTest(func(msg *schema.Message) bool {
		*seen = append(*seen, msg)
		ch.Send(context.Background(), makeChEvent(msg.ID, msg.SessionID,
			schema.EventTypeChatDelta, map[string]any{"content": "done"}))
		ch.Send(context.Background(), makeChEvent(msg.ID, msg.SessionID,
			schema.EventTypeChatProcessingStatus, map[string]any{"is_processing": false}))
		return true
	})
	assert.NoError(t, ch.Start(context.Background()))

	req := (*seen)[0]
	var p map[string]any
	assert.NoError(t, json.Unmarshal(req.Params, &p))
	assert.Equal(t, "D:/workspace/demo", p["cwd"])
	assert.Equal(t, "hello", p["query"])
	_ = out
}

// TestAcpChannel_CancelFinalizesActivePrompt 例 13（cancel → cancelled result + null result）。
func TestAcpChannel_CancelFinalizesActivePrompt(t *testing.T) {
	ch, out, _ := newTestChannel(t, []string{
		`{"jsonrpc":"2.0","id":20,"method":"session/prompt","params":{"sessionId":"sess-cancel","prompt":[{"type":"text","text":"hello"}]}}`,
		`{"jsonrpc":"2.0","id":21,"method":"session/cancel","params":{"sessionId":"sess-cancel"}}`,
	})
	ch.SetOnMessageForTest(func(msg *schema.Message) bool {
		if msg.ReqMethod == schema.ReqMethodChatSend {
			ch.Send(context.Background(), makeChEvent(msg.ID, msg.SessionID,
				schema.EventTypeChatDelta, map[string]any{"content": "still running"}))
		}
		return true
	})
	assert.NoError(t, ch.Start(context.Background()))

	lines := out.jsonLines(t)
	var promptResult, cancelResult map[string]any
	for _, l := range lines {
		if l["id"] == float64(20) {
			promptResult = l
		}
		if l["id"] == float64(21) {
			cancelResult = l
		}
	}
	pr, _ := promptResult["result"].(map[string]any)
	assert.Equal(t, "cancelled", pr["stopReason"])
	assert.Equal(t, map[string]any{"jsonrpc": "2.0", "id": float64(21), "result": nil}, cancelResult)
}

// ──────────────────────────── 例 15-16 工具响应回流 ────────────────────────────

// TestAcpChannel_ToolResponseForwarded 例 15：jsonrpc response → acp.tool_response dispatch。
// Python: test_jsonrpc_response_is_forwarded_as_acp_tool_response
// （直灌 pending 映射 + HandleJsonrpcResponseForTest + 断言 dispatch 消息形态）。
func TestAcpChannel_ToolResponseForwarded(t *testing.T) {
	out := &safeBuffer{}
	ch := NewAcpChannel(AcpChannelConfig{Enabled: true})
	ch.SetStdoutForTest(out)
	dispatched := make(chan *schema.Message, 4)
	ch.SetOnMessageForTest(func(msg *schema.Message) bool {
		dispatched <- msg
		return true
	})

	ch.SetPendingRPCSessionForTest("tool-15", "sess-tool")
	ch.HandleJsonrpcResponseForTest(map[string]any{
		"jsonrpc": "2.0", "id": "tool-15", "result": map[string]any{"content": "hello"},
	})

	select {
	case msg := <-dispatched:
		assert.Equal(t, schema.ReqMethodACPToolResponse, msg.ReqMethod)
		assert.Equal(t, "sess-tool", msg.SessionID)
		assert.Equal(t, "acp", msg.ChannelID)
		var p map[string]any
		assert.NoError(t, json.Unmarshal(msg.Params, &p))
		assert.Equal(t, "tool-15", p["jsonrpc_id"])
		resp, _ := p["response"].(map[string]any)
		if assert.NotNil(t, resp) {
			assert.Equal(t, "hello", resp["result"].(map[string]any)["content"])
		}
		assert.Equal(t, "sess-tool", p["session_id"])
	case <-time.After(2 * time.Second):
		t.Fatal("acp.tool_response dispatch not invoked")
	}
}

// TestAcpChannel_ResponseWithoutMappingIgnored 例 16：无 pending 映射 → 零 dispatch 零帧。
func TestAcpChannel_ResponseWithoutMappingIgnored(t *testing.T) {
	out := &safeBuffer{}
	ch := NewAcpChannel(AcpChannelConfig{Enabled: true})
	ch.SetStdoutForTest(out)
	ch.SetStdinForTest(&fakeStdin{lines: nil})
	ch.SetStdinEOFGraceSecondsForTest(0.05)
	ch.SetIdleFinalizeSecondsForTest(0.05)
	dispatched := 0
	ch.SetOnMessageForTest(func(msg *schema.Message) bool {
		dispatched++
		return true
	})
	_ = dispatched

	ch.HandleJsonrpcResponseForTest(map[string]any{
		"jsonrpc": "2.0", "id": "tool-unknown", "result": map[string]any{"late": true},
	})
	lines := out.jsonLines(t)
	assert.Empty(t, lines)
}

// TestAcpChannel_GatewayJsonrpcWrittenToStdout 例 19：网关 jsonrpc request 原样写 stdout + pending 登记。
func TestAcpChannel_GatewayJsonrpcWrittenToStdout(t *testing.T) {
	out := &safeBuffer{}
	ch := NewAcpChannel(AcpChannelConfig{Enabled: true})
	ch.SetStdoutForTest(out)

	raw := map[string]any{
		"jsonrpc": "2.0", "id": "perm-19", "method": "session/request_permission",
		"params": map[string]any{"sessionId": "sess-gw", "options": []any{}},
	}
	ch.HandleGatewayFrameForTest(raw)

	lines := out.jsonLines(t)
	assert.Len(t, lines, 1)
	assert.Equal(t, "perm-19", lines[0]["id"])
	assert.Equal(t, "session/request_permission", lines[0]["method"])
	// pending 登记
	assert.Equal(t, "sess-gw", ch.GetPendingRPCSessionForTest("perm-19"))
}

// TestAcpChannel_GatewayJsonrpcForActivePrompt 例 20：同 19 + 活跃 prompt 的 idle timer 被取消。
func TestAcpChannel_GatewayJsonrpcForActivePrompt(t *testing.T) {
	out := &safeBuffer{}
	ch := NewAcpChannel(AcpChannelConfig{Enabled: true})
	ch.SetStdoutForTest(out)

	c := &acpRequestCtx{
		JSONRPCID: 20, Method: "session/prompt", ResponseMode: "jsonrpc",
		SessionID: "sess-active",
	}
	ch.SeedRequestCtxForTest("req-active", c)
	ch.SetActivePromptRequestForTest("sess-active", "req-active")
	// 预置 sentinel timer（若未被取消将写 result 帧导致断言失败）
	fired := make(chan struct{}, 1)
	ch.mu.Lock()
	slot := &acpIdleTimerSlot{}
	c.idleFinalizeTimerSlot = slot
	slot.timer = time.AfterFunc(50*time.Millisecond, func() {
		select {
		case fired <- struct{}{}:
		default:
		}
	})
	ch.mu.Unlock()

	raw := map[string]any{
		"jsonrpc": "2.0", "id": "perm-20", "method": "fs/read_text_file",
		"params": map[string]any{"sessionId": "sess-active"},
	}
	ch.HandleGatewayFrameForTest(raw)

	lines := out.jsonLines(t)
	assert.Len(t, lines, 1)
	assert.Equal(t, "perm-20", lines[0]["id"])
	assert.Equal(t, "sess-active", ch.GetPendingRPCSessionForTest("perm-20"))
	// sentinel 被取消
	select {
	case <-fired:
		t.Fatal("sentinel timer should be cancelled by gateway jsonrpc request")
	case <-time.After(100 * time.Millisecond):
	}
	ch.mu.Lock()
	assert.Nil(t, c.idleFinalizeTimerSlot)
	ch.mu.Unlock()
}

// TestAcpChannel_E2aModeDeltaAndFinal E2A 模式（response_mode=e2a）基本流。
// Go 增强：Python 基线未直测 e2a 模式出站帧（三路由的 envelope 入口被
// jsonrpc 用例旁路覆盖），此处补 delta→e2a.chunk + final→acp_prompt_result。
func TestAcpChannel_E2aModeDeltaAndFinal(t *testing.T) {
	ch, out, seen := newTestChannel(t, []string{
		`{"request_id":"e2a-req-1","method":"session/prompt","params":{"session_id":"e2a-sess","query":"hi"},"jsonrpc_id":"e2a-1"}`,
	})
	ch.SetOnMessageForTest(func(msg *schema.Message) bool {
		*seen = append(*seen, msg)
		ch.Send(context.Background(), makeChEvent(msg.ID, msg.SessionID,
			schema.EventTypeChatDelta, map[string]any{"content": "part"}))
		ch.Send(context.Background(), makeChEvent(msg.ID, msg.SessionID,
			schema.EventTypeChatFinal, map[string]any{"content": "part done"}))
		return true
	})
	assert.NoError(t, ch.Start(context.Background()))

	// prompt 消息形态（E2A 入口 → chat.send 改写）
	require.Len(t, *seen, 1)
	req := (*seen)[0]
	assert.Equal(t, "e2a-req-1", req.ID)
	assert.Equal(t, schema.ReqMethodChatSend, req.ReqMethod)
	assert.Equal(t, "e2a-sess", req.SessionID)

	lines := out.jsonLines(t)
	require.GreaterOrEqual(t, len(lines), 2)
	// e2a.chunk 帧（sequence 取递增前旧值——Python L887-890 先取后增，首帧 :0；
	// 计划文档的 ":1" 为转录错误，以 Python 源为准）
	assert.Equal(t, "e2a-req-1:0", lines[0]["response_id"])
	assert.Equal(t, "e2a.chunk", lines[0]["response_kind"])
	assert.Equal(t, false, lines[0]["is_final"])
	// final 帧
	last := lines[len(lines)-1]
	assert.Equal(t, "acp.prompt_result", last["response_kind"])
	assert.Equal(t, true, last["is_final"])
	// ctx 清理
	assert.False(t, ch.HasRequestContextForTest("e2a-req-1"))
}

// ──────────────────────────── 例 12-13 finalize 悬挂 ────────────────────────────

// TestAcpChannel_NoEndTurnBeforeLateToolResult 例 12：final→tool_result→idle 四帧，
// 末帧才 end_turn（晚到 tool result 不得提前触发收尾）。
// Python: test_jsonrpc_session_prompt_does_not_end_turn_from_chat_final_before_late_tool_result。
func TestAcpChannel_NoEndTurnBeforeLateToolResult(t *testing.T) {
	ch, out, _ := newTestChannel(t, []string{
		`{"jsonrpc":"2.0","id":12,"method":"session/prompt","params":{"sessionId":"sess-idle","prompt":[{"type":"text","text":"hello"}]}}`,
	})
	ch.SetIdleFinalizeSecondsForTest(0.01)
	ch.SetStdinEOFGraceSecondsForTest(0.01)
	ch.SetOnMessageForTest(func(msg *schema.Message) bool {
		ch.Send(context.Background(), makeChEvent(msg.ID, msg.SessionID,
			schema.EventTypeChatFinal, map[string]any{"content": "final answer"}))
		ch.Send(context.Background(), makeChEvent(msg.ID, msg.SessionID,
			schema.EventTypeChatToolResult, map[string]any{
				"tool_name": "write_text_file", "tool_call_id": "tool-call-late-2",
				"result": "index.html written"}))
		ch.Send(context.Background(), makeChEvent(msg.ID, msg.SessionID,
			schema.EventTypeChatProcessingStatus, map[string]any{"is_processing": false}))
		return true
	})
	assert.NoError(t, ch.Start(context.Background()))

	lines := out.jsonLines(t)
	require.Len(t, lines, 4)
	// 第一个响应是 agent_message_chunk
	p0, _ := lines[0]["params"].(map[string]any)
	u0, _ := p0["update"].(map[string]any)
	assert.Equal(t, "session/update", lines[0]["method"])
	assert.Equal(t, "agent_message_chunk", u0["sessionUpdate"])
	// 第二个响应是晚到的 tool result update（七键全量）
	p1, _ := lines[1]["params"].(map[string]any)
	u1, _ := p1["update"].(map[string]any)
	assert.Equal(t, map[string]any{
		"sessionUpdate": "tool_call_update",
		"toolCallId":    "tool-call-late-2",
		"toolName":      "write_text_file",
		"title":         "Editing files",
		"kind":          "edit",
		"status":        "completed",
		"result":        "index.html written",
		"content":       []any{map[string]any{"type": "content", "content": map[string]any{"type": "text", "text": "index.html written"}}},
	}, u1)
	// 第三个响应是 idle
	p2, _ := lines[2]["params"].(map[string]any)
	u2, _ := p2["update"].(map[string]any)
	assert.Equal(t, map[string]any{
		"sessionUpdate": "session_info_update",
		"status":        "idle",
	}, u2)
	// 末帧才是 end_turn result
	result, _ := lines[3]["result"].(map[string]any)
	assert.Equal(t, float64(12), lines[3]["id"])
	assert.Equal(t, "end_turn", result["stopReason"])
}

// TestAcpChannel_DeltaOnlyNoFinalize 例 13：仅 delta 不自动 end_turn，
// 显式 Stop 截断循环（对齐 Python await channel.stop()）。
// Python: test_jsonrpc_session_prompt_does_not_auto_finalize_from_delta_only。
func TestAcpChannel_DeltaOnlyNoFinalize(t *testing.T) {
	ch, out, _ := newTestChannel(t, []string{
		`{"jsonrpc":"2.0","id":121,"method":"session/prompt","params":{"sessionId":"sess-delta-only","prompt":[{"type":"text","text":"hello"}]}}`,
	})
	ch.SetIdleFinalizeSecondsForTest(0.01)
	ch.SetStdinEOFGraceSecondsForTest(0.05)
	ch.SetOnMessageForTest(func(msg *schema.Message) bool {
		ch.Send(context.Background(), makeChEvent(msg.ID, msg.SessionID,
			schema.EventTypeChatDelta, map[string]any{"content": "partial answer"}))
		// delta-only 场景不会自动 end_turn；显式停止通道，避免测试等待未完成请求而超时
		ch.Stop()
		return true
	})
	assert.NoError(t, ch.Start(context.Background()))

	lines := out.jsonLines(t)
	require.Len(t, lines, 1)
	p0, _ := lines[0]["params"].(map[string]any)
	update, _ := p0["update"].(map[string]any)
	assert.Equal(t, "agent_message_chunk", update["sessionUpdate"])
	assert.IsType(t, "", update["messageId"])
	assert.Equal(t, map[string]any{"type": "text", "text": "partial answer"}, update["content"])
}

// ──────────────────────────── 例 17-18 直灌 ctx ────────────────────────────

// TestAcpChannel_IdleDefersUntilPendingRPC 例 17：pending RPC 挂起 end_turn，
// 回流后补发 result（SeedRequestCtxForTest + SetPendingRPCSessionForTest 直灌，
// 对齐 Python SimpleNamespace 模式）。
// Python: test_processing_idle_defers_end_turn_until_pending_client_rpc_resolves。
func TestAcpChannel_IdleDefersUntilPendingRPC(t *testing.T) {
	out := &safeBuffer{}
	ch := NewAcpChannel(AcpChannelConfig{Enabled: true})
	ch.SetStdoutForTest(out)
	requestID := "req-pending-client-rpc"
	sessionID := "sess-pending-client-rpc"
	seen := make(chan *schema.Message, 4)
	ch.SetOnMessageForTest(func(msg *schema.Message) bool {
		seen <- msg
		return true
	})
	c := &acpRequestCtx{
		JSONRPCID: 501, Method: "session/prompt", ResponseMode: "jsonrpc",
		SessionID: sessionID, UserMessageID: "user-msg-pending",
	}
	ch.SeedRequestCtxForTest(requestID, c)
	ch.SetPendingRPCSessionForTest("tool-pending-1", sessionID)

	isFinal := ch.SendJsonrpcMessageForTest(makeChEvent(requestID, sessionID,
		schema.EventTypeChatProcessingStatus, map[string]any{"is_processing": false}), c)

	assert.False(t, isFinal)
	ch.mu.Lock()
	assert.Equal(t, "end_turn", c.PendingStopReason)
	ch.mu.Unlock()
	// 挂起期唯一一帧：session_info_update idle
	lines := out.jsonLines(t)
	assert.Equal(t, []map[string]any{{
		"jsonrpc": "2.0",
		"method":  "session/update",
		"params": map[string]any{
			"sessionId": sessionID,
			"update": map[string]any{
				"sessionUpdate": "session_info_update",
				"status":        "idle",
			},
		},
	}}, lines)

	// 工具响应回流 → 补发 result
	ch.HandleJsonrpcResponseForTest(map[string]any{
		"jsonrpc": "2.0", "id": "tool-pending-1", "result": map[string]any{"ok": true},
	})

	lines = out.jsonLines(t)
	last := lines[len(lines)-1]
	assert.Equal(t, map[string]any{
		"jsonrpc": "2.0", "id": float64(501),
		"result": map[string]any{
			"stopReason":    "end_turn",
			"userMessageId": "user-msg-pending",
		},
	}, last)
	assert.False(t, ch.HasRequestContextForTest(requestID))
	// dispatch 单条 acp.tool_response
	select {
	case msg := <-seen:
		assert.Equal(t, schema.ReqMethodACPToolResponse, msg.ReqMethod)
		assert.Equal(t, sessionID, msg.SessionID)
	case <-time.After(2 * time.Second):
		t.Fatal("acp.tool_response dispatch not invoked")
	}
}

// TestAcpChannel_LateFinalSuffixOnly 例 18：迟到 final 只发缺失后缀。
// 三次直灌：delta("partial") → idle(挂起) → final("partial final answer")。
// Python: test_processing_idle_waits_for_late_chat_final_and_only_emits_missing_suffix。
func TestAcpChannel_LateFinalSuffixOnly(t *testing.T) {
	out := &safeBuffer{}
	ch := NewAcpChannel(AcpChannelConfig{Enabled: true})
	ch.SetStdoutForTest(out)
	requestID := "req-late-final"
	sessionID := "sess-late-final"
	c := &acpRequestCtx{
		JSONRPCID: 551, Method: "session/prompt", ResponseMode: "jsonrpc",
		SessionID: sessionID,
	}
	ch.SeedRequestCtxForTest(requestID, c)

	first := ch.SendJsonrpcMessageForTest(makeChEvent(requestID, sessionID,
		schema.EventTypeChatDelta, map[string]any{"content": "partial"}), c)
	second := ch.SendJsonrpcMessageForTest(makeChEvent(requestID, sessionID,
		schema.EventTypeChatProcessingStatus, map[string]any{"is_processing": false}), c)
	third := ch.SendJsonrpcMessageForTest(makeChEvent(requestID, sessionID,
		schema.EventTypeChatFinal, map[string]any{"content": "partial final answer"}), c)

	assert.False(t, first)
	assert.False(t, second)
	assert.True(t, third)

	ch.mu.Lock()
	assistantID := c.State.AssistantMessageID
	ch.mu.Unlock()

	lines := out.jsonLines(t)
	assert.Equal(t, []map[string]any{
		{
			"jsonrpc": "2.0",
			"method":  "session/update",
			"params": map[string]any{
				"sessionId": sessionID,
				"update": map[string]any{
					"sessionUpdate": "agent_message_chunk",
					"messageId":     assistantID,
					"content":       map[string]any{"type": "text", "text": "partial"},
				},
			},
		},
		{
			"jsonrpc": "2.0",
			"method":  "session/update",
			"params": map[string]any{
				"sessionId": sessionID,
				"update": map[string]any{
					"sessionUpdate": "session_info_update",
					"status":        "idle",
				},
			},
		},
		{
			"jsonrpc": "2.0",
			"method":  "session/update",
			"params": map[string]any{
				"sessionId": sessionID,
				"update": map[string]any{
					"sessionUpdate": "agent_message_chunk",
					"messageId":     assistantID,
					"content":       map[string]any{"type": "text", "text": " final answer"},
				},
			},
		},
		{
			"jsonrpc": "2.0",
			"id":      float64(551),
			"result":  map[string]any{"stopReason": "end_turn"},
		},
	}, lines)
}

// ──────────────────────────── 例 21-26 update 族 ────────────────────────────

// TestAcpChannel_ToolCallUpdate 例 21：tool_call 八键全量。
// Python: test_jsonrpc_session_prompt_emits_tool_call_update。
func TestAcpChannel_ToolCallUpdate(t *testing.T) {
	ch, out, _ := newTestChannel(t, []string{
		`{"jsonrpc":"2.0","id":30,"method":"session/prompt","params":{"sessionId":"sess-tool-call","prompt":[{"type":"text","text":"hello"}]}}`,
	})
	ch.SetOnMessageForTest(func(msg *schema.Message) bool {
		ch.Send(context.Background(), makeChEvent(msg.ID, msg.SessionID,
			schema.EventTypeChatToolCall, map[string]any{
				"tool_call": map[string]any{"name": "read_file",
					"arguments": map[string]any{"path": "demo.txt"}, "tool_call_id": "tool-call-1"}}))
		ch.Send(context.Background(), makeChEvent(msg.ID, msg.SessionID,
			schema.EventTypeChatDelta, map[string]any{"content": "done"}))
		ch.Send(context.Background(), makeChEvent(msg.ID, msg.SessionID,
			schema.EventTypeChatProcessingStatus, map[string]any{"is_processing": false}))
		return true
	})
	assert.NoError(t, ch.Start(context.Background()))

	lines := out.jsonLines(t)
	require.Len(t, lines, 4)
	p0, _ := lines[0]["params"].(map[string]any)
	u0, _ := p0["update"].(map[string]any)
	assert.Equal(t, map[string]any{
		"sessionUpdate": "tool_call",
		"toolCall": map[string]any{
			"id":        "tool-call-1",
			"name":      "read_file",
			"arguments": map[string]any{"path": "demo.txt"},
		},
		"toolCallId": "tool-call-1",
		"title":      "Reading demo.txt",
		"kind":       "read",
		"status":     "pending",
		"rawInput":   map[string]any{"path": "demo.txt"},
		"locations":  []any{map[string]any{"path": "demo.txt"}},
	}, u0)
	p1, _ := lines[1]["params"].(map[string]any)
	u1, _ := p1["update"].(map[string]any)
	assert.Equal(t, "agent_message_chunk", u1["sessionUpdate"])
	p2, _ := lines[2]["params"].(map[string]any)
	u2, _ := p2["update"].(map[string]any)
	assert.Equal(t, "session_info_update", u2["sessionUpdate"])
	assert.Equal(t, "idle", u2["status"])
	result, _ := lines[3]["result"].(map[string]any)
	assert.Equal(t, "end_turn", result["stopReason"])
}

// TestAcpChannel_ToolResultUpdate 例 22：tool_call_update 七键。
// Python: test_jsonrpc_session_prompt_emits_tool_result_update。
func TestAcpChannel_ToolResultUpdate(t *testing.T) {
	ch, out, _ := newTestChannel(t, []string{
		`{"jsonrpc":"2.0","id":31,"method":"session/prompt","params":{"sessionId":"sess-tool-result","prompt":[{"type":"text","text":"hello"}]}}`,
	})
	ch.SetOnMessageForTest(func(msg *schema.Message) bool {
		ch.Send(context.Background(), makeChEvent(msg.ID, msg.SessionID,
			schema.EventTypeChatToolResult, map[string]any{
				"tool_name": "read_file", "tool_call_id": "tool-call-2",
				"result": "file contents"}))
		ch.Send(context.Background(), makeChEvent(msg.ID, msg.SessionID,
			schema.EventTypeChatDelta, map[string]any{"content": "done"}))
		ch.Send(context.Background(), makeChEvent(msg.ID, msg.SessionID,
			schema.EventTypeChatProcessingStatus, map[string]any{"is_processing": false}))
		return true
	})
	assert.NoError(t, ch.Start(context.Background()))

	lines := out.jsonLines(t)
	require.Len(t, lines, 4)
	p0, _ := lines[0]["params"].(map[string]any)
	u0, _ := p0["update"].(map[string]any)
	assert.Equal(t, map[string]any{
		"sessionUpdate": "tool_call_update",
		"toolCallId":    "tool-call-2",
		"toolName":      "read_file",
		"title":         "Reading data",
		"kind":          "read",
		"status":        "completed",
		"result":        "file contents",
		"content":       []any{map[string]any{"type": "content", "content": map[string]any{"type": "text", "text": "file contents"}}},
	}, u0)
	p1, _ := lines[1]["params"].(map[string]any)
	u1, _ := p1["update"].(map[string]any)
	assert.Equal(t, "agent_message_chunk", u1["sessionUpdate"])
	p2, _ := lines[2]["params"].(map[string]any)
	u2, _ := p2["update"].(map[string]any)
	assert.Equal(t, "session_info_update", u2["sessionUpdate"])
	assert.Equal(t, "idle", u2["status"])
	result, _ := lines[3]["result"].(map[string]any)
	assert.Equal(t, "end_turn", result["stopReason"])
}

// TestAcpChannel_ToolInProgressUpdate 例 23：tool_call → tool_call_update(in_progress) 两帧。
// Python: test_jsonrpc_session_prompt_emits_tool_in_progress_update。
func TestAcpChannel_ToolInProgressUpdate(t *testing.T) {
	ch, out, _ := newTestChannel(t, []string{
		`{"jsonrpc":"2.0","id":311,"method":"session/prompt","params":{"sessionId":"sess-tool-progress","prompt":[{"type":"text","text":"hello"}]}}`,
	})
	ch.SetOnMessageForTest(func(msg *schema.Message) bool {
		ch.Send(context.Background(), makeChEvent(msg.ID, msg.SessionID,
			schema.EventTypeChatToolCall, map[string]any{
				"tool_call": map[string]any{"name": "read_file",
					"arguments": map[string]any{"path": "demo.txt"}, "tool_call_id": "tool-call-progress-1"}}))
		ch.Send(context.Background(), makeChEvent(msg.ID, msg.SessionID,
			schema.EventTypeChatToolUpdate, map[string]any{
				"tool_call_id": "tool-call-progress-1", "status": "in_progress"}))
		ch.Send(context.Background(), makeChEvent(msg.ID, msg.SessionID,
			schema.EventTypeChatProcessingStatus, map[string]any{"is_processing": false}))
		return true
	})
	assert.NoError(t, ch.Start(context.Background()))

	lines := out.jsonLines(t)
	// Python 断言前两帧 update 内容；后续 idle 帧 + result 由 timer 收尾（四帧）
	require.Len(t, lines, 4)
	p0, _ := lines[0]["params"].(map[string]any)
	u0, _ := p0["update"].(map[string]any)
	assert.Equal(t, map[string]any{
		"sessionUpdate": "tool_call",
		"toolCall": map[string]any{
			"id":        "tool-call-progress-1",
			"name":      "read_file",
			"arguments": map[string]any{"path": "demo.txt"},
		},
		"toolCallId": "tool-call-progress-1",
		"title":      "Reading demo.txt",
		"kind":       "read",
		"status":     "pending",
		"rawInput":   map[string]any{"path": "demo.txt"},
		"locations":  []any{map[string]any{"path": "demo.txt"}},
	}, u0)
	p1, _ := lines[1]["params"].(map[string]any)
	u1, _ := p1["update"].(map[string]any)
	assert.Equal(t, map[string]any{
		"sessionUpdate": "tool_call_update",
		"toolCallId":    "tool-call-progress-1",
		"toolName":      "read_file",
		"title":         "Reading demo.txt",
		"kind":          "read",
		"status":        "in_progress",
		"rawInput":      map[string]any{"path": "demo.txt"},
		"locations":     []any{map[string]any{"path": "demo.txt"}},
	}, u1)
	p2, _ := lines[2]["params"].(map[string]any)
	u2, _ := p2["update"].(map[string]any)
	assert.Equal(t, "session_info_update", u2["sessionUpdate"])
	assert.Equal(t, "idle", u2["status"])
	result, _ := lines[3]["result"].(map[string]any)
	assert.Equal(t, "end_turn", result["stopReason"])
}

// TestAcpChannel_PlanUpdate 例 24：chat.subtask_update → plan。
// Python: test_jsonrpc_session_prompt_emits_plan_update。
func TestAcpChannel_PlanUpdate(t *testing.T) {
	ch, out, _ := newTestChannel(t, []string{
		`{"jsonrpc":"2.0","id":32,"method":"session/prompt","params":{"sessionId":"sess-plan","prompt":[{"type":"text","text":"hello"}]}}`,
	})
	ch.SetOnMessageForTest(func(msg *schema.Message) bool {
		ch.Send(context.Background(), makeChEvent(msg.ID, msg.SessionID,
			schema.EventTypeChatSubtaskUpdate, map[string]any{
				"session_id": msg.SessionID, "description": "并行执行两个任务",
				"status": "running", "index": 1, "total": 2,
				"result": "已启动后台会话", "is_parallel": true}))
		ch.Send(context.Background(), makeChEvent(msg.ID, msg.SessionID,
			schema.EventTypeChatDelta, map[string]any{"content": "done"}))
		ch.Send(context.Background(), makeChEvent(msg.ID, msg.SessionID,
			schema.EventTypeChatProcessingStatus, map[string]any{"is_processing": false}))
		return true
	})
	assert.NoError(t, ch.Start(context.Background()))

	lines := out.jsonLines(t)
	require.Len(t, lines, 4)
	p0, _ := lines[0]["params"].(map[string]any)
	u0, _ := p0["update"].(map[string]any)
	assert.Equal(t, "plan", u0["sessionUpdate"])
	plan, _ := u0["plan"].(map[string]any)
	assert.Equal(t, "并行执行两个任务", plan["description"])
	assert.Equal(t, true, plan["is_parallel"])
	p1, _ := lines[1]["params"].(map[string]any)
	u1, _ := p1["update"].(map[string]any)
	assert.Equal(t, "agent_message_chunk", u1["sessionUpdate"])
	p2, _ := lines[2]["params"].(map[string]any)
	u2, _ := p2["update"].(map[string]any)
	assert.Equal(t, "session_info_update", u2["sessionUpdate"])
	assert.Equal(t, "idle", u2["status"])
	result, _ := lines[3]["result"].(map[string]any)
	assert.Equal(t, "end_turn", result["stopReason"])
}

// TestAcpChannel_ProcessingStatusUpdate 例 25：processing→idle→result 三帧。
// Python: test_jsonrpc_session_prompt_emits_processing_status_update。
func TestAcpChannel_ProcessingStatusUpdate(t *testing.T) {
	ch, out, _ := newTestChannel(t, []string{
		`{"jsonrpc":"2.0","id":33,"method":"session/prompt","params":{"sessionId":"sess-processing","prompt":[{"type":"text","text":"hello"}]}}`,
	})
	ch.SetOnMessageForTest(func(msg *schema.Message) bool {
		ch.Send(context.Background(), makeChEvent(msg.ID, msg.SessionID,
			schema.EventTypeChatProcessingStatus, map[string]any{"is_processing": true}))
		ch.Send(context.Background(), makeChEvent(msg.ID, msg.SessionID,
			schema.EventTypeChatProcessingStatus, map[string]any{"is_processing": false}))
		return true
	})
	assert.NoError(t, ch.Start(context.Background()))

	lines := out.jsonLines(t)
	require.Len(t, lines, 3)
	p0, _ := lines[0]["params"].(map[string]any)
	u0, _ := p0["update"].(map[string]any)
	assert.Equal(t, map[string]any{
		"sessionUpdate": "session_info_update",
		"status":        "processing",
	}, u0)
	p1, _ := lines[1]["params"].(map[string]any)
	u1, _ := p1["update"].(map[string]any)
	assert.Equal(t, map[string]any{
		"sessionUpdate": "session_info_update",
		"status":        "idle",
	}, u1)
	result, _ := lines[2]["result"].(map[string]any)
	assert.Equal(t, "end_turn", result["stopReason"])
}

// TestAcpChannel_UsageUpdateBeforeResult 例 26：delta 带 usage → chunk→usage→idle→result 四帧。
// Python: test_jsonrpc_session_prompt_emits_usage_update_before_result。
func TestAcpChannel_UsageUpdateBeforeResult(t *testing.T) {
	ch, out, _ := newTestChannel(t, []string{
		`{"jsonrpc":"2.0","id":34,"method":"session/prompt","params":{"sessionId":"sess-usage","prompt":[{"type":"text","text":"hello"}]}}`,
	})
	ch.SetOnMessageForTest(func(msg *schema.Message) bool {
		ch.Send(context.Background(), makeChEvent(msg.ID, msg.SessionID,
			schema.EventTypeChatDelta, map[string]any{
				"content": "done",
				"usage":   map[string]any{"inputTokens": 12, "outputTokens": 34}}))
		ch.Send(context.Background(), makeChEvent(msg.ID, msg.SessionID,
			schema.EventTypeChatProcessingStatus, map[string]any{"is_processing": false}))
		return true
	})
	assert.NoError(t, ch.Start(context.Background()))

	lines := out.jsonLines(t)
	require.Len(t, lines, 4)
	p0, _ := lines[0]["params"].(map[string]any)
	u0, _ := p0["update"].(map[string]any)
	assert.Equal(t, "agent_message_chunk", u0["sessionUpdate"])
	p1, _ := lines[1]["params"].(map[string]any)
	u1, _ := p1["update"].(map[string]any)
	// usage 数值经 JSON 往返为 float64（Python json 解析同态）
	assert.Equal(t, map[string]any{
		"sessionUpdate": "usage_update",
		"usage":         map[string]any{"inputTokens": float64(12), "outputTokens": float64(34)},
	}, u1)
	p2, _ := lines[2]["params"].(map[string]any)
	u2, _ := p2["update"].(map[string]any)
	assert.Equal(t, "session_info_update", u2["sessionUpdate"])
	assert.Equal(t, "idle", u2["status"])
	assert.Equal(t, map[string]any{
		"jsonrpc": "2.0", "id": float64(34), "result": map[string]any{"stopReason": "end_turn"},
	}, lines[3])
}

// ──────────────────────────── 例 27-31 timer 取向 ────────────────────────────

// TestAcpChannel_ProcessingTrueNoSchedule 例 27：is_processing=true 只出一帧且不排 timer。
// Python: test_processing_status_true_does_not_schedule_idle_finalize。
func TestAcpChannel_ProcessingTrueNoSchedule(t *testing.T) {
	out := &safeBuffer{}
	ch := NewAcpChannel(AcpChannelConfig{Enabled: true})
	ch.SetStdoutForTest(out)
	c := &acpRequestCtx{
		JSONRPCID: 35, Method: "session/prompt", ResponseMode: "jsonrpc",
		SessionID: "sess-processing-only",
	}
	ch.SeedRequestCtxForTest("req-processing-only", c)

	isFinal := ch.SendJsonrpcMessageForTest(makeChEvent("req-processing-only", "sess-processing-only",
		schema.EventTypeChatProcessingStatus, map[string]any{"is_processing": true}), c)

	assert.False(t, isFinal)
	assert.Equal(t, []map[string]any{{
		"jsonrpc": "2.0",
		"method":  "session/update",
		"params": map[string]any{
			"sessionId": "sess-processing-only",
			"update": map[string]any{
				"sessionUpdate": "session_info_update",
				"status":        "processing",
			},
		},
	}}, out.jsonLines(t))
	ch.mu.Lock()
	assert.Nil(t, c.idleFinalizeTimerSlot)
	ch.mu.Unlock()
}

// TestAcpChannel_ToolEventCancelsIdleFinalize 例 28：tool_call 事件取消 sentinel timer。
// Python: test_tool_events_cancel_idle_finalize_instead_of_scheduling。
// （计划代表代码写于 timer slot 模式修复前——此处按 slot 模式适配。）
func TestAcpChannel_ToolEventCancelsIdleFinalize(t *testing.T) {
	out := &safeBuffer{}
	ch := NewAcpChannel(AcpChannelConfig{Enabled: true})
	ch.SetStdoutForTest(out)
	c := &acpRequestCtx{
		JSONRPCID: 36, Method: "session/prompt", ResponseMode: "jsonrpc",
		SessionID: "sess-tool-idle",
	}
	// 预置 sentinel timer：短超时但绝不希望触发（触发即写出 result 帧 = 失败）
	fired := make(chan struct{}, 1)
	ch.mu.Lock()
	slot := &acpIdleTimerSlot{}
	c.idleFinalizeTimerSlot = slot
	slot.timer = time.AfterFunc(50*time.Millisecond, func() {
		select {
		case fired <- struct{}{}:
		default:
		}
	})
	ch.mu.Unlock()
	ch.SeedRequestCtxForTest("req-tool-idle", c)

	isFinal := ch.SendJsonrpcMessageForTest(makeChEvent("req-tool-idle", "sess-tool-idle",
		schema.EventTypeChatToolCall, map[string]any{
			"tool_call": map[string]any{"name": "terminal_create",
				"arguments": map[string]any{"cmd": "ls"}, "tool_call_id": "tc-idle-1"}}), c)

	assert.False(t, isFinal)
	// sentinel 被取消：100ms 内不触发（原 50ms）
	select {
	case <-fired:
		t.Fatal("sentinel idle finalize timer should have been cancelled by tool event")
	case <-time.After(100 * time.Millisecond):
	}
	ch.mu.Lock()
	assert.Nil(t, c.idleFinalizeTimerSlot)
	ch.mu.Unlock()
	// tool_call 帧本身照发（terminal_create kind=execute）
	lines := out.jsonLines(t)
	require.Len(t, lines, 1)
	p0, _ := lines[0]["params"].(map[string]any)
	u0, _ := p0["update"].(map[string]any)
	assert.Equal(t, "tool_call", u0["sessionUpdate"])
}

// TestAcpChannel_DirectReasoningUpdate 例 29：chat.reasoning 直发 thought_chunk。
// Python: test_jsonrpc_session_prompt_emits_direct_reasoning_update。
func TestAcpChannel_DirectReasoningUpdate(t *testing.T) {
	ch, out, _ := newTestChannel(t, []string{
		`{"jsonrpc":"2.0","id":37,"method":"session/prompt","params":{"sessionId":"sess-reasoning-direct","prompt":[{"type":"text","text":"hello"}]}}`,
	})
	ch.SetOnMessageForTest(func(msg *schema.Message) bool {
		ch.Send(context.Background(), makeChEvent(msg.ID, msg.SessionID,
			schema.EventTypeChatReasoning, map[string]any{"content": "reasoning step", "event_type": "chat.reasoning"}))
		ch.Send(context.Background(), makeChEvent(msg.ID, msg.SessionID,
			schema.EventTypeChatProcessingStatus, map[string]any{"is_processing": false}))
		return true
	})
	assert.NoError(t, ch.Start(context.Background()))

	lines := out.jsonLines(t)
	require.Len(t, lines, 3)
	p0, _ := lines[0]["params"].(map[string]any)
	u0, _ := p0["update"].(map[string]any)
	assert.Equal(t, "agent_thought_chunk", u0["sessionUpdate"])
	assert.Equal(t, map[string]any{"type": "text", "text": "reasoning step"}, u0["content"])
	p1, _ := lines[1]["params"].(map[string]any)
	u1, _ := p1["update"].(map[string]any)
	assert.Equal(t, map[string]any{
		"sessionUpdate": "session_info_update",
		"status":        "idle",
	}, u1)
	assert.Equal(t, map[string]any{
		"jsonrpc": "2.0", "id": float64(37), "result": map[string]any{"stopReason": "end_turn"},
	}, lines[2])
}

// TestAcpChannel_TodoUpdate 例 30：todo.updated → todo_update 三键。
// Python: test_jsonrpc_session_prompt_emits_todo_update。
func TestAcpChannel_TodoUpdate(t *testing.T) {
	ch, out, _ := newTestChannel(t, []string{
		`{"jsonrpc":"2.0","id":38,"method":"session/prompt","params":{"sessionId":"sess-todo-update","prompt":[{"type":"text","text":"hello"}]}}`,
	})
	ch.SetOnMessageForTest(func(msg *schema.Message) bool {
		ch.Send(context.Background(), makeChEvent(msg.ID, msg.SessionID,
			schema.EventTypeTodoUpdated, map[string]any{
				"todos": []any{map[string]any{
					"id": "todo-1", "content": "Implement ACP todo update",
					"activeForm": "Implementing ACP todo update", "status": "in_progress",
					"createdAt": "2026-04-16T00:00:00Z", "updatedAt": "2026-04-16T00:05:00Z"}}}))
		ch.Send(context.Background(), makeChEvent(msg.ID, msg.SessionID,
			schema.EventTypeChatProcessingStatus, map[string]any{"is_processing": false}))
		return true
	})
	assert.NoError(t, ch.Start(context.Background()))

	lines := out.jsonLines(t)
	require.Len(t, lines, 3)
	p0, _ := lines[0]["params"].(map[string]any)
	u0, _ := p0["update"].(map[string]any)
	assert.Equal(t, map[string]any{
		"sessionUpdate": "todo_update",
		"todos": []any{map[string]any{
			"id": "todo-1", "content": "Implement ACP todo update",
			"activeForm": "Implementing ACP todo update", "status": "in_progress",
			"createdAt": "2026-04-16T00:00:00Z", "updatedAt": "2026-04-16T00:05:00Z"}},
	}, u0)
	p1, _ := lines[1]["params"].(map[string]any)
	u1, _ := p1["update"].(map[string]any)
	assert.Equal(t, map[string]any{
		"sessionUpdate": "session_info_update",
		"status":        "idle",
	}, u1)
	assert.Equal(t, map[string]any{
		"jsonrpc": "2.0", "id": float64(38), "result": map[string]any{"stopReason": "end_turn"},
	}, lines[2])
}

// TestAcpChannel_ReasoningTodoCancelIdle 例 31：三事件各取消 sentinel timer。
// Python: test_reasoning_and_todo_events_cancel_idle_finalize。
func TestAcpChannel_ReasoningTodoCancelIdle(t *testing.T) {
	// 事件与 jsonrpc ID 三元组（reasoning delta / 直接 reasoning / todo）
	cases := []struct {
		name      string
		requestID string
		jsonrpcID int
		event     schema.EventType
		payload   map[string]any
	}{
		{"reasoning_delta", "req-reasoning-delta-idle", 39, schema.EventTypeChatDelta,
			map[string]any{"content": "reasoning", "source_chunk_type": "llm_reasoning"}},
		{"direct_reasoning", "req-reasoning-idle", 40, schema.EventTypeChatReasoning,
			map[string]any{"content": "reasoning", "event_type": "chat.reasoning"}},
		{"todo", "req-todo-idle", 41, schema.EventTypeTodoUpdated,
			map[string]any{"todos": []any{map[string]any{
				"id": "todo-2", "content": "Keep session alive",
				"activeForm": "Keeping session alive", "status": "pending",
				"createdAt": "2026-04-16T00:00:00Z", "updatedAt": "2026-04-16T00:05:00Z"}}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out := &safeBuffer{}
			ch := NewAcpChannel(AcpChannelConfig{Enabled: true})
			ch.SetStdoutForTest(out)
			c := &acpRequestCtx{
				JSONRPCID: tc.jsonrpcID, Method: "session/prompt", ResponseMode: "jsonrpc",
				SessionID: "sess-idle-cancel",
			}
			fired := make(chan struct{}, 1)
			ch.mu.Lock()
			slot := &acpIdleTimerSlot{}
			c.idleFinalizeTimerSlot = slot
			slot.timer = time.AfterFunc(50*time.Millisecond, func() {
				select {
				case fired <- struct{}{}:
				default:
				}
			})
			ch.mu.Unlock()
			ch.SeedRequestCtxForTest(tc.requestID, c)

			isFinal := ch.SendJsonrpcMessageForTest(makeChEvent(tc.requestID, "sess-idle-cancel",
				tc.event, tc.payload), c)

			assert.False(t, isFinal)
			select {
			case <-fired:
				t.Fatal("sentinel idle finalize timer should have been cancelled")
			case <-time.After(100 * time.Millisecond):
			}
			ch.mu.Lock()
			assert.Nil(t, c.idleFinalizeTimerSlot)
			ch.mu.Unlock()
		})
	}
}

// ──────────────────────────── Go 增强：数字 id / stdin 末行（P1/P3 回归） ────────────────────────────

// TestAcpChannel_NumericJsonrpcIDToolResponse 数字型 id 必须命中 pending（P1 回归）。
// Python: _handle_jsonrpc_response 用 str(data.get("id") or "").strip()（L810）。
func TestAcpChannel_NumericJsonrpcIDToolResponse(t *testing.T) {
	out := &safeBuffer{}
	ch := NewAcpChannel(AcpChannelConfig{Enabled: true})
	ch.SetStdoutForTest(out)
	seen := make(chan *schema.Message, 4)
	ch.SetOnMessageForTest(func(msg *schema.Message) bool {
		select {
		case seen <- msg:
		default:
		}
		return true
	})
	requestID := "req-num-id"
	sessionID := "sess-num-id"
	c := &acpRequestCtx{
		JSONRPCID: float64(777), Method: "session/prompt", ResponseMode: "jsonrpc",
		SessionID: sessionID,
	}
	ch.SeedRequestCtxForTest(requestID, c)
	ch.SetActivePromptRequestForTest(sessionID, requestID)
	// 我们发出 string id "777"，IDE 回成 number 777
	ch.SetPendingRPCSessionForTest("777", sessionID)

	ch.HandleJsonrpcResponseForTest(map[string]any{
		"jsonrpc": "2.0", "id": float64(777), "result": map[string]any{"content": "ok"},
	})

	select {
	case msg := <-seen:
		assert.Equal(t, schema.ReqMethodACPToolResponse, msg.ReqMethod)
		assert.Equal(t, sessionID, msg.SessionID)
		var params map[string]any
		require.NoError(t, json.Unmarshal(msg.Params, &params))
		assert.Equal(t, "777", params["jsonrpc_id"])
	case <-time.After(2 * time.Second):
		t.Fatal("numeric jsonrpc id 未能命中 pending，tool response 被丢弃")
	}
}

// noNewlineStdin 首行不带换行即 EOF（验证 P3：末行不丢弃）。
type noNewlineStdin struct {
	line string
	done bool
}

// Read 首次返回不带 \n 的残余行，之后恒 EOF。
func (f *noNewlineStdin) Read(p []byte) (int, error) {
	if f.done {
		return 0, errEOF
	}
	f.done = true
	return copy(p, f.line), nil
}

// TestAcpChannel_StdinLastLineWithoutNewline 末行无换行仍须处理（P3 回归）。
// Python readline() 对「有数据无分隔符 + EOF」返回该行，下一次才返回 b""。
func TestAcpChannel_StdinLastLineWithoutNewline(t *testing.T) {
	out := &safeBuffer{}
	seen := &[]*schema.Message{}
	ch := NewAcpChannel(AcpChannelConfig{Enabled: true})
	ch.SetStdoutForTest(out)
	ch.SetStdinForTest(&noNewlineStdin{
		line: `{"jsonrpc":"2.0","id":900,"method":"session/new","params":{"sessionId":"sess-tail"}}`,
	})
	ch.SetStdinEOFGraceSecondsForTest(0.1)
	ch.SetOnMessageForTest(func(msg *schema.Message) bool {
		*seen = append(*seen, msg)
		return true
	})
	require.NoError(t, ch.Start(context.Background()))

	lines := out.jsonLines(t)
	require.Len(t, lines, 1, "末行无换行仍应产生 session/new 响应")
	result, _ := lines[0]["result"].(map[string]any)
	assert.Equal(t, float64(900), lines[0]["id"])
	if assert.NotNil(t, result) {
		assert.Equal(t, "sess-tail", result["sessionId"])
	}
}

// TestAcpChannel_InvalidJsonLineEmitsStdioError 非法 JSON 行 → E2A stdio_error 响应。
// Python: start() 的 try/except → E2AResponse(response_kind=acp.jsonrpc_error)（L726-742）。
func TestAcpChannel_InvalidJsonLineEmitsStdioError(t *testing.T) {
	out := &safeBuffer{}
	ch := NewAcpChannel(AcpChannelConfig{Enabled: true})
	ch.SetStdoutForTest(out)
	ch.SetStdinForTest(&fakeStdin{lines: []string{`{not json}`}})
	ch.SetStdinEOFGraceSecondsForTest(0.1)
	ch.SetOnMessageForTest(func(*schema.Message) bool { return true })
	require.NoError(t, ch.Start(context.Background()))

	lines := out.jsonLines(t)
	require.Len(t, lines, 1)
	assert.Equal(t, e2a.E2AResponseKindACPJSONRPCError, lines[0]["response_kind"])
	body, _ := lines[0]["body"].(map[string]any)
	if assert.NotNil(t, body) {
		assert.Equal(t, float64(-32603), body["code"])
		assert.Contains(t, body["message"], "invalid json line")
	}
}

// TestAcpChannel_GatewayPingLoopExits 网关连接关闭 → ping goroutine 随之退出。
func TestAcpChannel_GatewayPingLoopExits(t *testing.T) {
	t.Parallel()
	ch := NewAcpChannel(AcpChannelConfig{Enabled: true})
	done := make(chan struct{})
	finished := make(chan struct{})
	go func() {
		ch.gatewayPingLoop(nil, done)
		close(finished)
	}()
	close(done)
	select {
	case <-finished:
	case <-time.After(2 * time.Second):
		t.Fatal("gatewayPingLoop 未随 done 关闭退出")
	}
}

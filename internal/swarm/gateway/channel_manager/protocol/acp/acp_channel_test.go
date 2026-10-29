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

func (eofError) Error() string   { return "EOF" }
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
		"result":  map[string]any{"sessions": []any{map[string]any{"sessionId": "sess-1"}}},
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

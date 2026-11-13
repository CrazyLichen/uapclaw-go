package observability

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// captureHandler 捕获事件的测试 handler（← handler.py EventHandler 实现）。
type captureHandler struct {
	name    string
	events  []RoutingEvent
	panicOn bool
}

func (h *captureHandler) HandleEvent(e RoutingEvent) {
	if h.panicOn {
		panic("boom")
	}
	h.events = append(h.events, e)
}
func (h *captureHandler) Name() string { return h.name }

// TestEventTypes_逐字对齐 事件类型字符串值与 Python 枚举一致（23.4b 桥接键）。
func TestEventTypes_逐字对齐(t *testing.T) {
	// ← events.py:9-33 全量枚举
	expected := []string{
		"request_started", "request_succeeded", "request_retried",
		"all_deployments_exhausted", "request_cancelled",
		"stream_started", "stream_route_selected", "stream_succeeded",
		"stream_cancelled", "deployments_updated",
	}
	require.Equal(t, expected, SupportedEventTypes())
}

// TestRoutingEvent_ToDict 省略 None 字段（events.py:76-97）。
func TestRoutingEvent_ToDict(t *testing.T) {
	e := NewRoutingEvent(EventTypeRequestStarted, "req-1", "m")
	d := e.ToDict()
	require.Equal(t, "request_started", d["event_type"]) // py:79 event_type.value
	require.Equal(t, "req-1", d["request_id"])
	require.Equal(t, "m", d["model"])
	_, hasLatency := d["latency"]
	require.False(t, hasLatency) // None 字段省略（py:82-91）
	_, hasAttempt := d["attempt"]
	require.False(t, hasAttempt) // attempt==0 不输出（py:92-93）

	latency := 1.5
	e.Latency = &latency
	e.Attempt = 2
	e.TotalAttempts = 3
	e.Extra = map[string]any{"k": "v"}
	d = e.ToDict()
	require.Equal(t, 1.5, d["latency"])
	require.Equal(t, 2, d["attempt"])
	require.Equal(t, 3, d["total_attempts"])
	require.Equal(t, "v", d["extra"].(map[string]any)["k"])
}

// TestEventBus_RegisterUnregisterEmit 注册/注销/分发顺序（bus.py:23-49）。
func TestEventBus_RegisterUnregisterEmit(t *testing.T) {
	b := &EventBus{}
	h1 := &captureHandler{name: "h1"}
	h2 := &captureHandler{name: "h2"}
	// 无 handler 时 emit 零开销（py:41）
	b.Emit(NewRoutingEvent(EventTypeRequestStarted, "r", "m"))

	b.Register(h1)
	b.Register(h2)
	require.Len(t, b.Handlers(), 2) // 只读副本（py:23-25）

	e := NewRoutingEvent(EventTypeRequestSucceeded, "r", "m")
	b.Emit(e)
	require.Len(t, h1.events, 1) // 按注册顺序广播
	require.Len(t, h2.events, 1)
	require.Equal(t, "request_succeeded", string(h1.events[0].EventType))

	// 单 handler panic 不影响其他 handler（py:42-47）
	h1.panicOn = true
	b.Emit(e)
	require.Len(t, h2.events, 2) // h2 仍收到

	b.Unregister(h1)
	require.Len(t, b.Handlers(), 1)
}

// TestRequestID 生成短唯一 ID（events.py:72-74）。
func TestRequestID(t *testing.T) {
	id := NewRequestID()
	require.Len(t, id, 12) // uuid hex[:12]
	require.NotEqual(t, id, NewRequestID())
}

// TestRoutingEvent_ToDict_全字段 补齐 optional 字段输出（events.py:82-97）。
func TestRoutingEvent_ToDict_全字段(t *testing.T) {
	e := NewRoutingEvent(EventTypeStreamSucceeded, "r", "m")
	depID := "d1"
	prov := "openai"
	lat := 0.5
	pt := 1
	ct := 2
	tt := 3
	et := "E"
	em := "msg"
	cc := 9
	e.DeploymentID = &depID
	e.Provider = &prov
	e.Latency = &lat
	e.PromptTokens = &pt
	e.CompletionTokens = &ct
	e.TotalTokens = &tt
	e.ErrorType = &et
	e.ErrorMessage = &em
	e.ChunkCount = &cc
	d := e.ToDict()
	require.Equal(t, "d1", d["deployment_id"])
	require.Equal(t, "openai", d["provider"])
	require.Equal(t, 0.5, d["latency"])
	require.Equal(t, 1, d["prompt_tokens"])
	require.Equal(t, 2, d["completion_tokens"])
	require.Equal(t, 3, d["total_tokens"])
	require.Equal(t, "E", d["error_type"])
	require.Equal(t, "msg", d["error_message"])
	require.Equal(t, 9, d["chunk_count"])
	// Unregister 未注册 handler 为 no-op（Go 语义登记）
	b := &EventBus{}
	b.Unregister(&captureHandler{name: "ghost"})
	require.Empty(t, b.Handlers())
}

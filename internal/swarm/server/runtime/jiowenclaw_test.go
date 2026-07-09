package runtime

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/uapclaw/uapclaw-go/internal/swarm/schema"
)

// ──────────────────────────── 结构体 ────────────────────────────

// fakeAdapter AgentAdapter mock，用于 JiuWenClaw 测试。
type fakeAdapter struct {
	mu              sync.Mutex
	createErr       error
	processResp     *schema.AgentResponse
	processErr      error
	streamCh        <-chan *schema.AgentResponseChunk
	streamErr       error
	interruptResp   *schema.AgentResponse
	interruptErr    error
	heartbeatResp   *schema.AgentResponse
	heartbeatErr    error
	userAnswerResp  *schema.AgentResponse
	userAnswerErr   error
	instanceCreated bool
}

func newFakeAdapter() *fakeAdapter {
	return &fakeAdapter{
		processResp:   schema.NewAgentResponse("fake", "fake", schema.WithResponseOK(true), schema.WithPayload(map[string]any{"content": "mock response"})),
		interruptResp: schema.NewAgentResponse("fake", "fake", schema.WithResponseOK(true)),
	}
}

func (f *fakeAdapter) CreateInstance(_ context.Context, _ map[string]any, _, _ string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.instanceCreated = true
	return f.createErr
}
func (f *fakeAdapter) ReloadAgentConfig(_ context.Context, _, _ map[string]any) error { return nil }
func (f *fakeAdapter) ProcessMessageImpl(_ context.Context, _ *schema.AgentRequest, _ map[string]any) (*schema.AgentResponse, error) {
	return f.processResp, f.processErr
}
func (f *fakeAdapter) ProcessMessageStreamImpl(_ context.Context, _ *schema.AgentRequest, _ map[string]any) (<-chan *schema.AgentResponseChunk, error) {
	return f.streamCh, f.streamErr
}
func (f *fakeAdapter) ProcessInterrupt(_ context.Context, _ *schema.AgentRequest) (*schema.AgentResponse, error) {
	return f.interruptResp, f.interruptErr
}
func (f *fakeAdapter) HandleUserAnswer(_ context.Context, _ *schema.AgentRequest) (*schema.AgentResponse, error) {
	return f.userAnswerResp, f.userAnswerErr
}
func (f *fakeAdapter) HandleHeartbeat(_ context.Context, _ *schema.AgentRequest) (*schema.AgentResponse, error) {
	return f.heartbeatResp, f.heartbeatErr
}
func (f *fakeAdapter) Cleanup() error { return nil }

// ──────────────────────────── 导出函数 ────────────────────────────

func TestNewJiuWenClaw(t *testing.T) {
	jw := NewJiuWenClaw()
	require.NotNil(t, jw)
	require.NotNil(t, jw.sessionManager)
}

func TestJiuWenClaw_ensureAdapter_幂等(t *testing.T) {
	jw := NewJiuWenClaw()
	a1, err := jw.ensureAdapter("agent")
	require.NoError(t, err)
	require.NotNil(t, a1)
	a2, err := jw.ensureAdapter("agent")
	require.NoError(t, err)
	assert.Equal(t, a1, a2, "ensureAdapter 应幂等返回同一 adapter")
}

func TestJiuWenClaw_ProcessMessage_cancel分支(t *testing.T) {
	jw := NewJiuWenClaw()
	// 注入 fakeAdapter
	fa := newFakeAdapter()
	jw.adapter = fa

	req := schema.NewAgentRequest("req-1", "web", schema.ReqMethodChatCancel, nil)
	resp, err := jw.ProcessMessage(context.Background(), req)
	require.NoError(t, err)
	assert.True(t, resp.OK)
}

func TestJiuWenClaw_ProcessMessage_heartbeat分支(t *testing.T) {
	jw := NewJiuWenClaw()
	fa := newFakeAdapter()
	fa.heartbeatResp = schema.NewAgentResponse("req-1", "web", schema.WithResponseOK(true), schema.WithPayload(map[string]any{"event_type": "heartbeat"}))
	jw.adapter = fa

	req := schema.NewAgentRequest("req-1", "web", schema.ReqMethodChatSend, nil)
	resp, err := jw.ProcessMessage(context.Background(), req)
	require.NoError(t, err)
	assert.True(t, resp.OK)
	// 应短路返回 heartbeat 响应
	assert.Equal(t, "heartbeat", resp.Payload["event_type"])
}

func TestJiuWenClaw_ProcessMessage_常规对话(t *testing.T) {
	jw := NewJiuWenClaw()
	fa := newFakeAdapter()
	jw.adapter = fa

	params := map[string]any{"query": "你好"}
	paramsJSON, _ := json.Marshal(params)
	sessID := "test-sess"
	req := schema.NewAgentRequest("req-1", "web", schema.ReqMethodChatSend, paramsJSON,
		schema.WithAgentSessionID(sessID))

	resp, err := jw.ProcessMessage(context.Background(), req)
	require.NoError(t, err)
	assert.True(t, resp.OK)
}

func TestJiuWenClaw_ProcessMessageStream_基本流程(t *testing.T) {
	jw := NewJiuWenClaw()
	fa := newFakeAdapter()

	// 构建流式 chunk channel
	chunkCh := make(chan *schema.AgentResponseChunk, 4)
	chunkCh <- schema.NewAgentResponseChunk("req-2", "web", map[string]any{
		"event_type": "chat.delta",
		"content":    "你好",
	})
	chunkCh <- schema.NewAgentResponseChunk("req-2", "web", map[string]any{
		"event_type": "chat.final",
		"content":    "完整回复",
	})
	close(chunkCh)

	fa.streamCh = chunkCh
	jw.adapter = fa

	params := map[string]any{"query": "你好"}
	paramsJSON, _ := json.Marshal(params)
	req := schema.NewAgentRequest("req-2", "web", schema.ReqMethodChatSend, paramsJSON,
		schema.WithAgentIsStream(true),
		schema.WithAgentSessionID("stream-sess"))

	resultCh, err := jw.ProcessMessageStream(context.Background(), req)
	require.NoError(t, err)

	// 消费所有 chunk
	chunkCount := 0
	var gotTerminal bool
	for chunk := range resultCh {
		chunkCount++
		if chunk.IsTerminal() {
			gotTerminal = true
		}
	}
	assert.True(t, gotTerminal, "流应以终止哨兵结束")
	assert.GreaterOrEqual(t, chunkCount, 2, "至少应有 chat.delta + terminal")
}

func TestJiuWenClaw_ProcessInterrupt_intent分支(t *testing.T) {
	jw := NewJiuWenClaw()
	fa := newFakeAdapter()
	jw.adapter = fa

	tests := []struct {
		name   string
		intent string
	}{
		{"pause", "pause"},
		{"resume", "resume"},
		{"supplement", "supplement"},
		{"cancel", "cancel"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			params, _ := json.Marshal(map[string]any{"intent": tc.intent})
			req := schema.NewAgentRequest("req", "web", schema.ReqMethodChatCancel, params)
			resp, err := jw.ProcessInterrupt(context.Background(), req)
			require.NoError(t, err)
			assert.True(t, resp.OK)
		})
	}
}

func TestJiuWenClaw_Cleanup(t *testing.T) {
	jw := NewJiuWenClaw()
	fa := newFakeAdapter()
	jw.adapter = fa
	err := jw.Cleanup()
	require.NoError(t, err)
	assert.Nil(t, jw.adapter)
}

func TestJiuWenClaw_CancelInflightWork(t *testing.T) {
	jw := NewJiuWenClaw()
	fa := newFakeAdapter()
	jw.adapter = fa
	err := jw.CancelInflightWork()
	require.NoError(t, err)
}

func TestJiuWenClaw_GetContextUsage(t *testing.T) {
	jw := NewJiuWenClaw()
	result, err := jw.GetContextUsage("sess-1")
	require.NoError(t, err)
	assert.Equal(t, 0, result["usage"])
}

func TestJiuWenClaw_CompressContext(t *testing.T) {
	jw := NewJiuWenClaw()
	result, err := jw.CompressContext("sess-1")
	require.NoError(t, err)
	assert.True(t, result["ok"].(bool))
}

func TestJiuWenClaw_GenerateRecap(t *testing.T) {
	jw := NewJiuWenClaw()
	result, err := jw.GenerateRecap("sess-1")
	require.NoError(t, err)
	assert.Equal(t, "", result["recap"])
}

func TestJiuWenClaw_SwitchMode(t *testing.T) {
	jw := NewJiuWenClaw()
	err := jw.SwitchMode("sess-1", "code.normal")
	assert.NoError(t, err)
}

func TestJiuWenClaw_CreateInstance(t *testing.T) {
	jw := NewJiuWenClaw()
	fa := newFakeAdapter()
	jw.adapter = fa

	err := jw.CreateInstance(map[string]any{}, "agent", "")
	require.NoError(t, err)

	fa.mu.Lock()
	created := fa.instanceCreated
	fa.mu.Unlock()
	assert.True(t, created, "CreateInstance 应委托给 adapter")
}

func TestJiuWenClaw_CreateInstance_错误(t *testing.T) {
	jw := NewJiuWenClaw()
	fa := newFakeAdapter()
	fa.createErr = fmt.Errorf("mock error")
	jw.adapter = fa

	err := jw.CreateInstance(map[string]any{}, "agent", "")
	assert.Error(t, err)
}

func TestJiuWenClaw_ReloadAgentConfig(t *testing.T) {
	jw := NewJiuWenClaw()
	fa := newFakeAdapter()
	jw.adapter = fa

	err := jw.ReloadAgentConfig(map[string]any{}, nil)
	require.NoError(t, err)
}

func TestJiuWenClaw_ReloadAgentConfig_无Adapter(t *testing.T) {
	jw := NewJiuWenClaw()
	// adapter 为 nil 时不报错
	err := jw.ReloadAgentConfig(map[string]any{}, nil)
	require.NoError(t, err)
}

func TestJiuWenClaw_GetInstance(t *testing.T) {
	jw := NewJiuWenClaw()
	// ⤵️ 10.3.2: 当前 stub 返回 nil
	assert.Nil(t, jw.GetInstance())
}

// ──────────────────────────── 非导出函数 ────────────────────────────

func TestNormalizeSessionID(t *testing.T) {
	assert.Equal(t, "default", normalizeSessionID(""))
	assert.Equal(t, "my-session", normalizeSessionID("my-session"))
}

func TestShouldRecordHistory(t *testing.T) {
	assert.True(t, shouldRecordHistory("chat.delta"))
	assert.True(t, shouldRecordHistory("chat.final"))
	assert.True(t, shouldRecordHistory("chat.tool_result"))
	assert.False(t, shouldRecordHistory("heartbeat"))
	assert.False(t, shouldRecordHistory("system"))
}

func TestExtractChunkContent(t *testing.T) {
	assert.Equal(t, "hello", extractChunkContent(map[string]any{"content": "hello"}))
	assert.Equal(t, "", extractChunkContent(map[string]any{}))
	assert.Equal(t, "", extractChunkContent(nil))
}

func TestParseRequestParams(t *testing.T) {
	// nil params
	req := schema.NewAgentRequest("r1", "web", schema.ReqMethodChatSend, nil)
	params := parseRequestParams(req)
	assert.Empty(t, params)

	// valid JSON params
	req = schema.NewAgentRequest("r2", "web", schema.ReqMethodChatSend, json.RawMessage(`{"query":"hi","mode":"code"}`))
	params = parseRequestParams(req)
	assert.Equal(t, "hi", params["query"])
	assert.Equal(t, "code", params["mode"])
}

func TestExtractChannelFromSessionID(t *testing.T) {
	// 有 sessionID，包含 _
	sessID := "web_my-session"
	req := schema.NewAgentRequest("r1", "web", schema.ReqMethodChatSend, nil, schema.WithAgentSessionID(sessID))
	assert.Equal(t, "web", extractChannelFromSessionID(req))

	// 无 sessionID
	req = schema.NewAgentRequest("r2", "web", schema.ReqMethodChatSend, nil)
	assert.Equal(t, "web", extractChannelFromSessionID(req))
}

func TestExtractStringWithFallback(t *testing.T) {
	// params 优先
	params := map[string]any{"project_dir": "/from/params"}
	metadata := map[string]any{"project_dir": "/from/metadata"}
	assert.Equal(t, "/from/params", extractStringWithFallback(params, "project_dir", metadata, "project_dir"))

	// metadata 兜底
	params = map[string]any{}
	assert.Equal(t, "/from/metadata", extractStringWithFallback(params, "project_dir", metadata, "project_dir"))

	// 都没有
	assert.Equal(t, "", extractStringWithFallback(params, "project_dir", nil, "project_dir"))
}

func TestAdapterModeForRequest(t *testing.T) {
	jw := NewJiuWenClaw()
	// 有 mode 参数
	params := map[string]any{"mode": "code.normal"}
	paramsJSON, _ := json.Marshal(params)
	req := schema.NewAgentRequest("r1", "web", schema.ReqMethodChatSend, paramsJSON)
	assert.Equal(t, "code", jw.adapterModeForRequest(req))

	// 无 mode 参数，默认 agent
	req = schema.NewAgentRequest("r2", "web", schema.ReqMethodChatSend, nil)
	assert.Equal(t, "agent", jw.adapterModeForRequest(req))

	// mode 为空串
	params = map[string]any{"mode": ""}
	paramsJSON, _ = json.Marshal(params)
	req = schema.NewAgentRequest("r3", "web", schema.ReqMethodChatSend, paramsJSON)
	assert.Equal(t, "agent", jw.adapterModeForRequest(req))
}

func TestExtractResponseContent(t *testing.T) {
	jw := NewJiuWenClaw()
	// 有 content
	resp := schema.NewAgentResponse("r1", "web", schema.WithPayload(map[string]any{"content": "hello"}))
	assert.Equal(t, "hello", jw.extractResponseContent(resp))

	// 无 payload
	resp = schema.NewAgentResponse("r2", "web")
	assert.Equal(t, "", jw.extractResponseContent(resp))

	// payload 无 content 键
	resp = schema.NewAgentResponse("r3", "web", schema.WithPayload(map[string]any{"other": "val"}))
	assert.Equal(t, "", jw.extractResponseContent(resp))
}

func TestExtractIntent(t *testing.T) {
	jw := NewJiuWenClaw()
	// 有 intent
	params := map[string]any{"intent": "pause"}
	paramsJSON, _ := json.Marshal(params)
	req := schema.NewAgentRequest("r1", "web", schema.ReqMethodChatCancel, paramsJSON)
	assert.Equal(t, "pause", jw.extractIntent(req))

	// 无 intent，默认 cancel
	req = schema.NewAgentRequest("r2", "web", schema.ReqMethodChatCancel, nil)
	assert.Equal(t, "cancel", jw.extractIntent(req))
}

func TestExtractQuery(t *testing.T) {
	jw := NewJiuWenClaw()
	// 有 query
	params := map[string]any{"query": "你好"}
	paramsJSON, _ := json.Marshal(params)
	req := schema.NewAgentRequest("r1", "web", schema.ReqMethodChatSend, paramsJSON)
	assert.Equal(t, "你好", jw.extractQuery(req))

	// 无 query
	req = schema.NewAgentRequest("r2", "web", schema.ReqMethodChatSend, nil)
	assert.Equal(t, "", jw.extractQuery(req))
}

func TestExtractSessionID(t *testing.T) {
	jw := NewJiuWenClaw()
	// 有 sessionID
	sessID := "my-session"
	req := schema.NewAgentRequest("r1", "web", schema.ReqMethodChatSend, nil, schema.WithAgentSessionID(sessID))
	assert.Equal(t, "my-session", jw.extractSessionID(req))

	// 无 sessionID
	req = schema.NewAgentRequest("r2", "web", schema.ReqMethodChatSend, nil)
	assert.Equal(t, "", jw.extractSessionID(req))
}

// 注意：此测试依赖真实 adapter 创建（DeepAdapter），确保编译通过即可。
// 如需更完整的 ProcessMessage 测试，使用 fakeAdapter 注入。
func TestJiuWenClaw_ensureAdapter_创建真实Adapter(t *testing.T) {
	jw := NewJiuWenClaw()
	a, err := jw.ensureAdapter("agent")
	require.NoError(t, err)
	require.NotNil(t, a)
	// 确保幂等
	a2, _ := jw.ensureAdapter("agent")
	assert.Equal(t, a, a2)
}

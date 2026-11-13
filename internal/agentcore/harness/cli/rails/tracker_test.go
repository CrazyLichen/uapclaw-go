package rails

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	harnessprotocol "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	kvcache "github.com/uapclaw/uapclaw-go/internal/agentcore/kv_cache"
	sessioninterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/session/interfaces"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/state"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/stream"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
)

// recordingSession 记录写进流的块。
type recordingSession struct {
	sessionID string
	chunks    []any
}

func newRecordingSession() *recordingSession { return &recordingSession{sessionID: "s-1"} }

func (s *recordingSession) GetSessionID() string                   { return s.sessionID }
func (s *recordingSession) UpdateState(_ map[string]any)           {}
func (s *recordingSession) GetState(_ state.StateKey) (any, error) { return nil, nil }
func (s *recordingSession) DumpState() map[string]any              { return nil }
func (s *recordingSession) WriteStream(_ context.Context, data any) error {
	s.chunks = append(s.chunks, data)
	return nil
}
func (s *recordingSession) WriteCustomStream(_ context.Context, _ any) error { return nil }
func (s *recordingSession) GetEnv(_ string, _ ...any) any                    { return nil }
func (s *recordingSession) Interact(_ context.Context, _ any) error          { return nil }
func (s *recordingSession) ClearSession(_ context.Context)                   {}
func (s *recordingSession) GetCacheIdentity() kvcache.KVCacheIdentity {
	return kvcache.KVCacheIdentity{}
}
func (s *recordingSession) GetParentSessionID() string { return "" }

var _ sessioninterfaces.SessionFacade = (*recordingSession)(nil)

// asToolCallPayload 把块断言成 tool 载荷 map。
func asChunk(t *testing.T, chunk any, chunkType string) map[string]any {
	t.Helper()
	schema, ok := chunk.(stream.OutputSchema)
	require.True(t, ok)
	require.Equal(t, chunkType, schema.Type)
	payload, ok := schema.Payload.(map[string]any)
	require.True(t, ok)
	return payload
}

// ──────────────────────────── TokenTrackingRail 测试分组 ────────────────────────────

// TestTokenTrackingRail_累计用量 对齐 token_tracker.py:30-55。
func TestTokenTrackingRail_累计用量(t *testing.T) {
	rail := NewTokenTrackingRail()
	// Python: priority = 10
	assert.Equal(t, 10, rail.Priority())

	message := harnessprotocol.NewAssistantMessage("回答",
		harnessprotocol.WithAssistantUsageMetadata(&harnessprotocol.UsageMetadata{
			InputTokens: 100, OutputTokens: 30,
		}))
	ctx := agentinterfaces.NewAgentCallbackContext(nil,
		&agentinterfaces.ModelCallInputs{Response: message}, nil)
	require.NoError(t, rail.afterModelCall(context.Background(), ctx))
	require.NoError(t, rail.afterModelCall(context.Background(), ctx))

	summary := rail.GetSummary()
	assert.Equal(t, 200, summary["input_tokens"])
	assert.Equal(t, 60, summary["output_tokens"])
	assert.Equal(t, 260, summary["total_tokens"])
	assert.Equal(t, 2, summary["model_calls"])
}

// TestTokenTrackingRail_无响应跳过 响应或 usage 缺失时只计调用数。
func TestTokenTrackingRail_无响应跳过(t *testing.T) {
	rail := NewTokenTrackingRail()
	ctx := agentinterfaces.NewAgentCallbackContext(nil, &agentinterfaces.ModelCallInputs{}, nil)
	require.NoError(t, rail.afterModelCall(context.Background(), ctx))

	summary := rail.GetSummary()
	assert.Equal(t, 0, summary["input_tokens"])
	assert.Equal(t, 1, summary["model_calls"])
}

// TestTokenTrackingRail_非ModelCall输入 忽略其它事件输入。
func TestTokenTrackingRail_非ModelCall输入(t *testing.T) {
	rail := NewTokenTrackingRail()
	ctx := agentinterfaces.NewAgentCallbackContext(nil, &agentinterfaces.MapInputs{}, nil)
	require.NoError(t, rail.afterModelCall(context.Background(), ctx))
	assert.Equal(t, 0, rail.GetSummary()["input_tokens"])
}

// ──────────────────────────── ToolTrackingRail 测试分组 ────────────────────────────

// TestToolTrackingRail_优先级最低 对齐 tool_tracker.py `priority = 5`。
func TestToolTrackingRail_优先级最低(t *testing.T) {
	rail := NewToolTrackingRail()
	assert.Equal(t, 5, rail.Priority())
}

// TestToolTrackingRail_before写toolCall块 对齐 tool_tracker.py:64-89。
func TestToolTrackingRail_before写toolCall块(t *testing.T) {
	rail := NewToolTrackingRail()
	session := newRecordingSession()
	ctx := agentinterfaces.NewAgentCallbackContext(nil,
		&agentinterfaces.ToolCallInputs{
			ToolName: "read_file",
			ToolArgs: map[string]any{"path": "/tmp/x"},
		}, session)
	require.NoError(t, rail.beforeToolCall(context.Background(), ctx))

	require.Len(t, session.chunks, 1)
	payload := asChunk(t, session.chunks[0], "tool_call")
	assert.Equal(t, "read_file", payload["tool_name"])
	require.NotNil(t, payload["tool_args"])
}

// TestToolTrackingRail_before无会话跳过 session 缺失时静默。
func TestToolTrackingRail_before无会话跳过(t *testing.T) {
	rail := NewToolTrackingRail()
	ctx := agentinterfaces.NewAgentCallbackContext(nil, &agentinterfaces.ToolCallInputs{}, nil)
	require.NoError(t, rail.beforeToolCall(context.Background(), ctx))
	require.NoError(t, rail.afterToolCall(context.Background(), ctx))
}

// TestToolTrackingRail_after写toolResult块 对齐 tool_tracker.py:91-145。
func TestToolTrackingRail_after写toolResult块(t *testing.T) {
	rail := NewToolTrackingRail()
	session := newRecordingSession()
	ctx := agentinterfaces.NewAgentCallbackContext(nil,
		&agentinterfaces.ToolCallInputs{
			ToolName:   "bash",
			ToolArgs:   map[string]any{"cmd": "ls"},
			ToolResult: map[string]any{"result": "文件列表"},
		}, session)
	require.NoError(t, rail.afterToolCall(context.Background(), ctx))

	require.Len(t, session.chunks, 1)
	payload := asChunk(t, session.chunks[0], "tool_result")
	assert.Equal(t, "bash", payload["tool_name"])
	require.Contains(t, payload, "tool_result")
	assert.Contains(t, payload["tool_result"], "文件列表")
	// rendered_result 从 ToolResult 提取或字符串投影
	require.Contains(t, payload, "rendered_result")
}

// TestBuildToolResultPayload_readFile特例 对齐 tool_tracker.py:46-61。
func TestBuildToolResultPayload_readFile特例(t *testing.T) {
	// 非 read_file：只带兼容字段
	plain := buildToolResultPayload("bash", map[string]any{"result": "x"})
	require.Contains(t, plain, "tool_result")
	assert.NotContains(t, plain, "line_count")

	// read_file + content + line_count åºæ¯
	file := buildToolResultPayload("read_file", map[string]any{
		"content": "第一行", "line_count": 2,
	})
	assert.Equal(t, "第一行", file["tool_result"])
	assert.Equal(t, 2, file["line_count"])
}

// TestBuildToolResultPayload_readFile异常值 line_count 非法时跳过。
func TestBuildToolResultPayload_readFile异常值(t *testing.T) {
	payload := buildToolResultPayload("read_file", map[string]any{
		"content": "文本", "line_count": "不是数字",
	})
	assert.NotContains(t, payload, "line_count")
}

// TestBuildToolResultPayload_空结果 tool_result 为 nil 时兼容字段为空串。
func TestBuildToolResultPayload_空结果(t *testing.T) {
	payload := buildToolResultPayload("bash", nil)
	assert.Equal(t, "", payload["tool_result"])
}

// TestStringifyResult_各形态 Python str() 的等价投影。
func TestStringifyResult_各形态(t *testing.T) {
	assert.Equal(t, "", stringifyResult(nil))
	assert.Equal(t, "文本", stringifyResult("文本"))
	assert.Contains(t, stringifyResult([]byte("字节")), "字节")
	assert.Contains(t, stringifyResult(map[string]any{"k": "v"}), "k")
	assert.Equal(t, "42", stringifyResult(42))
}

// TestToInt_各形态 宽松整数转换的各分支。
func TestToInt_各形态(t *testing.T) {
	n, ok := toInt(7)
	assert.True(t, ok)
	assert.Equal(t, 7, n)
	n, ok = toInt(int64(9))
	assert.True(t, ok)
	assert.Equal(t, 9, n)
	n, ok = toInt(2.0)
	assert.True(t, ok)
	assert.Equal(t, 2, n)
	n, ok = toInt("12")
	assert.True(t, ok)
	assert.Equal(t, 12, n)
	_, ok = toInt("abc")
	assert.False(t, ok)
	_, ok = toInt([]any{1})
	assert.False(t, ok)
}

// TestTokenTrackingRail_非cbc输入 railCtx 不是 cbc 时忽略。
func TestTokenTrackingRail_非cbc输入(t *testing.T) {
	rail := NewTokenTrackingRail()
	require.NoError(t, rail.afterModelCall(context.Background(), "not-a-cbc"))
	assert.Equal(t, 1, rail.GetSummary()["model_calls"])
}

// TestToolTrackingRail_非cbc输入 railCtx 不是 cbc 时忽略。
func TestToolTrackingRail_非cbc输入(t *testing.T) {
	rail := NewToolTrackingRail()
	require.NoError(t, rail.beforeToolCall(context.Background(), "not-a-cbc"))
	require.NoError(t, rail.afterToolCall(context.Background(), "not-a-cbc"))
}

// TestBuildToolResultPayload_非map结果 read_file 但结果非 map。
func TestBuildToolResultPayload_非map结果(t *testing.T) {
	payload := buildToolResultPayload("read_file", "纯文本结果")
	assert.Equal(t, "纯文本结果", payload["tool_result"])
	assert.NotContains(t, payload, "line_count")
}

// TestBuildToolResultPayload_contentBytes read_file 的 content 是字节。
func TestBuildToolResultPayload_contentBytes(t *testing.T) {
	payload := buildToolResultPayload("read_file", map[string]any{
		"content": []byte("字节内容"), "line_count": int64(4),
	})
	assert.Equal(t, "字节内容", payload["tool_result"])
	assert.Equal(t, 4, payload["line_count"])
}

// TestCallbacks_两rail的挂接 回调 map 装配正确。
func TestCallbacks_两rail的挂接(t *testing.T) {
	token := NewTokenTrackingRail()
	tokenCallbacks := token.GetCallbacks()
	assert.Contains(t, tokenCallbacks, agentinterfaces.CallbackAfterModelCall)

	tool := NewToolTrackingRail()
	toolCallbacks := tool.GetCallbacks()
	assert.Contains(t, toolCallbacks, agentinterfaces.CallbackBeforeToolCall)
	assert.Contains(t, toolCallbacks, agentinterfaces.CallbackAfterToolCall)
}

// TestExtractRendered_各分支 rendered 字段与字符串投影。
func TestExtractRendered_各分支(t *testing.T) {
	// 有 rendered_result 字段
	text, ok := extractRendered(map[string]any{"rendered_result": "渲染文本"})
	assert.True(t, ok)
	assert.Equal(t, "渲染文本", text)
	// rendered_result 为空串 → 回落字符串投影
	text, ok = extractRendered(map[string]any{"rendered_result": ""})
	assert.True(t, ok)
	assert.NotEqual(t, "", text)
	// nil 结果
	_, ok = extractRendered(nil)
	assert.False(t, ok)
}

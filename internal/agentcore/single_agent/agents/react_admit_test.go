package agents

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	ceiface "github.com/uapclaw/uapclaw-go/internal/agentcore/context_engine/interface"
	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	cb "github.com/uapclaw/uapclaw-go/internal/agentcore/runner/callback"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
)

// ──────────────────────────── 结构体 ────────────────────────────

// recordingRail 记录 on_user_message 时看到的 parts 并原地改写。
type recordingRail struct {
	interfaces.BaseRail
	// mutate 对 parts 的改写动作
	mutate func(parts []string) []string
	// seen 最后一次看到的 parts 快照
	seen []string
	// seenSource 最后一次看到的 source
	seenSource string
	// inputsDuringFire fire 期间 cbc.Inputs() 的种类标识
	inputsDuringFire string
}

// panicRail 在 OnUserMessage 里 panic。
type panicRail struct{}

// recordingModelContext 记录 AddMessages 写入的消息。
type recordingModelContext struct {
	ceiface.ModelContext
	// added 已写入的消息
	added []llmschema.BaseMessage
}

// ──────────────────────────── 导出函数 ────────────────────────────

// GetCallbacks 声明 OnUserMessage（记录并改写）。
func (a *recordingRail) GetCallbacks() map[interfaces.AgentCallbackEvent]cb.PerAgentCallbackFunc {
	return a.BuildCallbacks(
		a.CallbackFrom(interfaces.CallbackOnUserMessage, func(_ context.Context, railCtx any) error {
			cbc, ok := railCtx.(*interfaces.AgentCallbackContext)
			if !ok {
				return nil
			}
			in, ok := cbc.Inputs().(*interfaces.UserMessageInputs)
			if !ok {
				a.inputsDuringFire = "other"
				return nil
			}
			a.inputsDuringFire = in.EventKind()
			a.seen = append([]string(nil), in.Parts...)
			a.seenSource = in.Source
			if a.mutate != nil {
				in.Parts = a.mutate(in.Parts)
			}
			return nil
		}),
	)
}

// GetCallbacks 声明会 panic 的 OnUserMessage。
func (p *panicRail) GetCallbacks() map[interfaces.AgentCallbackEvent]cb.PerAgentCallbackFunc {
	return map[interfaces.AgentCallbackEvent]cb.PerAgentCallbackFunc{
		interfaces.CallbackOnUserMessage: func(_ context.Context, _ any) error { panic("rail panic") },
	}
}

// AddMessages 记录写入
func (m *recordingModelContext) AddMessages(_ context.Context, msgs []llmschema.BaseMessage, _ ...ceiface.Option) ([]llmschema.BaseMessage, error) {
	m.added = append(m.added, msgs...)
	return msgs, nil
}

// JustAdded 返回已记录的消息
func (m *recordingModelContext) JustAdded() []llmschema.BaseMessage { return m.added }

// SessionID 返回固定会话 id
func (m *recordingModelContext) SessionID() string { return "s1" }

// TestAdmitUserMessage_多part用换行join 验证 join 用换行。
//
// Python: body = "\n".join(parts) (react_agent.py L953)
func TestAdmitUserMessage_多part用换行join(t *testing.T) {
	ag := &recordingRail{}
	a, cbc, mc := newAgentWithRecordingRail(t, ag)

	require.NoError(t, a.admitUserMessage(context.Background(), cbc, mc, []string{"a", "b"}, "query", ""))
	require.Equal(t, []string{"a", "b"}, ag.seen)
	require.Equal(t, "query", ag.seenSource)
	require.Len(t, mc.JustAdded(), 1)
	require.Equal(t, "a\nb", mc.JustAdded()[0].GetContent().Text())
}

// TestAdmitUserMessage_前缀在join之前 验证前缀拼在整段 body 前面。
//
// Python: content=f"{prefix}{body}"
func TestAdmitUserMessage_前缀在join之前(t *testing.T) {
	ag := &recordingRail{}
	a, cbc, mc := newAgentWithRecordingRail(t, ag)

	require.NoError(t, a.admitUserMessage(context.Background(), cbc, mc, []string{"a", "b"}, "steering", "[STEERING] "))
	require.Equal(t, "[STEERING] a\nb", mc.JustAdded()[0].GetContent().Text())
}

// TestAdmitUserMessage_前插与丢弃 验证 rails 的编辑生效。
func TestAdmitUserMessage_前插与丢弃(t *testing.T) {
	ag := &recordingRail{mutate: func(parts []string) []string {
		return append([]string{"X"}, parts[:1]...)
	}}
	a, cbc, mc := newAgentWithRecordingRail(t, ag)

	require.NoError(t, a.admitUserMessage(context.Background(), cbc, mc, []string{"a", "b"}, "query", ""))
	require.Equal(t, "X\na", mc.JustAdded()[0].GetContent().Text())
}

// TestAdmitUserMessage_全丢不写历史 验证 rails 丢空后什么都不做。
//
// Python: if not parts: return（react_agent.py L948）
func TestAdmitUserMessage_全丢不写历史(t *testing.T) {
	ag := &recordingRail{mutate: func([]string) []string { return nil }}
	a, cbc, mc := newAgentWithRecordingRail(t, ag)

	require.NoError(t, a.admitUserMessage(context.Background(), cbc, mc, []string{"a"}, "query", ""))
	require.Empty(t, mc.JustAdded(), "parts 全丢后不得写历史")
}

// TestAdmitUserMessage_Inputs还原 验证事件后 inputs 复原。
//
// Python: try/finally 还原 previous_inputs（react_agent.py L940-956）
func TestAdmitUserMessage_Inputs还原(t *testing.T) {
	ag := &recordingRail{}
	a, cbc, mc := newAgentWithRecordingRail(t, ag)

	require.NoError(t, a.admitUserMessage(context.Background(), cbc, mc, []string{"a"}, "query", ""))
	require.Equal(t, "user_message_inputs", ag.inputsDuringFire)
	_, ok := cbc.Inputs().(*interfaces.InvokeInputs)
	require.True(t, ok, "事件结束后必须还原为 InvokeInputs")
}

// TestAdmitUserMessage_panic也还原 验证 fire 内 panic 时 inputs 仍复原。
//
// Go 特有：Python 的 finally 保证等价语义；Go 必须在 defer 里做。
func TestAdmitUserMessage_panic也还原(t *testing.T) {
	a, cbc, mc := newAgentWithRecordingRail(t, &panicRail{})

	require.Panics(t, func() {
		_ = a.admitUserMessage(context.Background(), cbc, mc, []string{"a"}, "query", "")
	})
	_, ok := cbc.Inputs().(*interfaces.InvokeInputs)
	require.True(t, ok, "panic 路径也必须还原 inputs")
}

// TestExtractUserParts_无extra时单元素兜底 验证缺省时 parts 为 query 单元素。
//
// Python: _extract_user_parts 无 _input_parts → [query 文本]
func TestExtractUserParts_无extra时单元素兜底(t *testing.T) {
	cbc := interfaces.NewAgentCallbackContext(nil, &interfaces.InvokeInputs{}, nil)
	parts := extractUserParts(cbc, interfaces.InvokeQueryString("hello world"))
	require.Len(t, parts, 1)
	require.Equal(t, "hello world", parts[0])
}

// TestExtractUserParts_有extra时用列表 验证 _input_parts 优先。
func TestExtractUserParts_有extra时用列表(t *testing.T) {
	cbc := interfaces.NewAgentCallbackContext(nil, &interfaces.InvokeInputs{}, nil)
	cbc.SetExtra("_input_parts", []string{"a", "b", "c"})
	parts := extractUserParts(cbc, interfaces.InvokeQueryString("a\nb\nc"))
	require.Equal(t, []string{"a", "b", "c"}, parts)
}

// TestExtractUserParts_类型不符回退 验证 []any 形态被忽略。
func TestExtractUserParts_类型不符回退(t *testing.T) {
	cbc := interfaces.NewAgentCallbackContext(nil, &interfaces.InvokeInputs{}, nil)
	cbc.SetExtra("_input_parts", []any{"a", "b"})
	parts := extractUserParts(cbc, interfaces.InvokeQueryString("a\nb"))
	require.Equal(t, []string{"a\nb"}, parts)
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// newAgentWithRecordingRail 构造挂了 rail 的 ReActAgent、回调上下文与可观察 ModelContext。
func newAgentWithRecordingRail(
	t *testing.T,
	rail interface {
		GetCallbacks() map[interfaces.AgentCallbackEvent]cb.PerAgentCallbackFunc
	},
) (*ReActAgent, *interfaces.AgentCallbackContext, *recordingModelContext) {
	t.Helper()
	a := newTestAgent("admit-test")
	for ev, fn := range rail.GetCallbacks() {
		a.CallbackManager().RegisterCallback(context.Background(), ev, fn)
	}
	mc := &recordingModelContext{}
	cbc := interfaces.NewAgentCallbackContext(a, &interfaces.InvokeInputs{Query: interfaces.InvokeQueryString("q")}, nil)
	return a, cbc, mc
}

// TestReActAgent_输入parts经DeepAgent传递 验证跨层透传闭环。
//
// Python: inputs["_input_parts"] → ctx.extra["_input_parts"] → parts
// (react_agent.py L2981-2982, L1002-1012)
func TestReActAgent_输入parts经DeepAgent传递(t *testing.T) {
	cbc := interfaces.NewAgentCallbackContext(nil, &interfaces.InvokeInputs{Query: interfaces.InvokeQueryString("a\nb")}, nil)

	applyInputPartsExtra(cbc, map[string]any{"_input_parts": []string{"a", "b"}})

	raw, ok := cbc.GetExtra("_input_parts")
	require.True(t, ok, "_input_parts 必须落到 cbc.extra")
	require.Equal(t, []string{"a", "b"}, raw)
	require.Equal(t, []string{"a", "b"}, extractUserParts(cbc, interfaces.InvokeQueryString("a\nb")))
}

// TestReActAgent_非法inputParts被忽略 验证 []any 或空列表不写入 extra。
func TestReActAgent_非法inputParts被忽略(t *testing.T) {
	cbc := interfaces.NewAgentCallbackContext(nil, &interfaces.InvokeInputs{}, nil)

	applyInputPartsExtra(cbc, map[string]any{"_input_parts": []any{"a", "b"}})
	_, ok := cbc.GetExtra("_input_parts")
	require.False(t, ok, "类型不符不得写入")

	applyInputPartsExtra(cbc, map[string]any{"_input_parts": []string{}})
	_, ok = cbc.GetExtra("_input_parts")
	require.False(t, ok, "空列表不得写入")

	applyInputPartsExtra(cbc, nil)
	applyInputPartsExtra(nil, map[string]any{"_input_parts": []string{"a"}})
}

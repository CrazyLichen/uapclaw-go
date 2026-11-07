package agents

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	ceiface "github.com/uapclaw/uapclaw-go/internal/agentcore/context_engine/interface"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm"
	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/prompts"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/state"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
)

// TestSyncPromptAttachments_无管理器时跳过 验证 nil manager 视为同步成功（无车道）。
func TestSyncPromptAttachments_无管理器时跳过(t *testing.T) {
	require.True(t, syncPromptAttachments(context.Background(), nil, nil))
}

// TestSyncPromptAttachments_有管理器无上下文时跳过 验证 nil 上下文视为同步成功（无车道）。
func TestSyncPromptAttachments_有管理器无上下文时跳过(t *testing.T) {
	require.True(t, syncPromptAttachments(context.Background(), prompts.NewManager(), nil))
}

// TestExtraString_取值语义 验证配置 Extra 读取。
func TestExtraString_取值语义(t *testing.T) {
	require.Equal(t, "", extraString(nil, "endpoint_profile"))
	require.Equal(t, "", extraString(map[string]any{"endpoint_profile": 1}, "endpoint_profile"))
	require.Equal(t, "bailian", extraString(map[string]any{"endpoint_profile": "bailian"}, "endpoint_profile"))
}

// TestSetPromptAttachmentManager 验证注入可空。
func TestSetPromptAttachmentManager(t *testing.T) {
	a := &ReActAgent{}
	a.SetPromptAttachmentManager(prompts.NewManager())
	require.NotNil(t, a.promptAttachments)
	a.SetPromptAttachmentManager(nil)
	require.Nil(t, a.promptAttachments)
}

// TestAttachmentWindowOptions_按provider挂投影 覆盖 nil 分支与百炼/Anthropic 两种投影。
func TestAttachmentWindowOptions_按provider挂投影(t *testing.T) {
	m := prompts.NewManager()

	require.Nil(t, attachmentWindowOptions(nil, nil, "s1"))
	require.Nil(t, attachmentWindowOptions(m, nil, "s1"))
	require.Nil(t, attachmentWindowOptions(m, &llm.Model{}, "s1"))

	bailian := &llm.Model{ClientConfig: &llmschema.ModelClientConfig{ClientProvider: "dashscope"}}
	opts := attachmentWindowOptions(m, bailian, "s1")
	require.Len(t, opts, 1)

	window := ceiface.NewContextWindow()
	window.ContextMessages = []llmschema.BaseMessage{
		llmschema.NewUserMessage("<system-reminder>x</system-reminder>", llmschema.WithMetadata(map[string]any{
			prompts.MetadataKeyAttachmentHistory: true,
			prompts.MetadataKeySessionID:         "s1",
		})),
	}
	projected := ceiface.WindowMutatorsFromOption(opts...)[0](window)
	require.IsType(t, &llmschema.SystemMessage{}, projected.ContextMessages[0], "dashscope 应换 System")

	anthropic := &llm.Model{ClientConfig: &llmschema.ModelClientConfig{ClientProvider: "anthropic"}}
	opts = attachmentWindowOptions(m, anthropic, "s1")
	projected = ceiface.WindowMutatorsFromOption(opts...)[0](window)
	require.IsType(t, &llmschema.UserMessage{}, projected.ContextMessages[0], "anthropic 保持 User")
}

// failHistory 故意失败的 HistoryContext，用于覆盖同步失败告警分支。
type failHistory struct{}

// SessionID 返回固定会话 id
func (f *failHistory) SessionID() string { return "s1" }

// GetMessages 故意失败
func (f *failHistory) GetMessages(int, bool) ([]llmschema.BaseMessage, error) {
	return nil, errFailHistory
}

// AddMessages 故意失败
func (f *failHistory) AddMessages(context.Context, []llmschema.BaseMessage, ...ceiface.Option) ([]llmschema.BaseMessage, error) {
	return nil, errFailHistory
}

// TestSyncPromptAttachments_同步失败只告警 验证失败不向调用方传播，但判定为未成功。
func TestSyncPromptAttachments_同步失败只告警(t *testing.T) {
	m := prompts.NewManager()
	_, err := m.AddSection("s1", "mode", "A", prompts.AttachmentRuntime, "test.rail")
	require.NoError(t, err)
	require.False(t, syncPromptAttachments(context.Background(), m, &failHistory{}),
		"同步失败必须返回 false，调用方据此不执行提交回调")
}

// errFailHistory 测试用错误
var errFailHistory = errors.New("history unavailable")

// writableHistory 可写的最小 HistoryContext，用于验证同步成功路径。
type writableHistory struct {
	// sessionID 会话标识
	sessionID string
	// msgs 已写入的消息
	msgs []llmschema.BaseMessage
}

// SessionID 返回固定会话 id
func (w *writableHistory) SessionID() string { return w.sessionID }

// GetMessages 返回已有消息
func (w *writableHistory) GetMessages(_ int, _ bool) ([]llmschema.BaseMessage, error) {
	return w.msgs, nil
}

// AddMessages 记录写入的消息
func (w *writableHistory) AddMessages(_ context.Context, msgs []llmschema.BaseMessage, _ ...ceiface.Option) ([]llmschema.BaseMessage, error) {
	w.msgs = append(w.msgs, msgs...)
	return w.msgs, nil
}

// TestSyncPromptAttachments_成功返回true 验证历史可写时判定同步成功。
func TestSyncPromptAttachments_成功返回true(t *testing.T) {
	m := prompts.NewManager()
	mc := &writableHistory{sessionID: "s1"}
	require.True(t, syncPromptAttachments(context.Background(), m, mc))
}

// TestSyncPromptAttachments_失败返回false且不取回调 验证同步失败时不消费回调。
// Python: _sync_prompt_attachments 在 sync_to_context 抛异常时直接 return，
// 回调留在 ctx.extra 里等下一次边界（react_agent.py L1160-1170）。
func TestSyncPromptAttachments_失败返回false且不取回调(t *testing.T) {
	m := prompts.NewManager()
	// 必须先注册 section，否则 Sync 在 len(items)==0 时直接返回（不碰历史）
	_, err := m.AddSection("s1", "mode", "A", prompts.AttachmentRuntime, "test.rail")
	require.NoError(t, err)

	cbc := testCallbackContext("s1")
	calls := 0
	cbc.SetExtra(prompts.CommitCallbacksExtraKey, []func() error{
		func() error { calls++; return nil },
	})
	require.False(t, syncPromptAttachments(context.Background(), m, &failHistory{}))
	_, still := cbc.GetExtra(prompts.CommitCallbacksExtraKey)
	require.True(t, still, "失败时回调不得被取出，必须留到下一个边界重试")
	require.Zero(t, calls, "失败时回调不得被执行")
}

// TestRunCommitCallbacks_逐条执行且只执行一次 验证回调取出即清空。
func TestRunCommitCallbacks_逐条执行且只执行一次(t *testing.T) {
	cbc := testCallbackContext("s1")
	var hits []int
	cbc.SetExtra(prompts.CommitCallbacksExtraKey, []func() error{
		func() error { hits = append(hits, 1); return nil },
		func() error { hits = append(hits, 2); return nil },
	})
	runCommitCallbacks(cbc)
	require.Equal(t, []int{1, 2}, hits)
	runCommitCallbacks(cbc)
	require.Equal(t, []int{1, 2}, hits, "第二次调用不得重放")
}

// testSessionFacade 最小 SessionFacade 实现，只为提供 GetSessionID。
type testSessionFacade struct {
	// id 会话标识
	id string
}

// GetSessionID 返回会话 id
func (f *testSessionFacade) GetSessionID() string { return f.id }

// UpdateState 空实现
func (f *testSessionFacade) UpdateState(map[string]any) {}

// GetState 返回空
func (f *testSessionFacade) GetState(state.StateKey) (any, error) { return nil, nil }

// DumpState 返回空快照
func (f *testSessionFacade) DumpState() map[string]any { return map[string]any{} }

// WriteStream 空实现
func (f *testSessionFacade) WriteStream(context.Context, any) error { return nil }

// WriteCustomStream 空实现
func (f *testSessionFacade) WriteCustomStream(context.Context, any) error { return nil }

// GetEnv 返回 nil
func (f *testSessionFacade) GetEnv(string, ...any) any { return nil }

// Interact 空实现
func (f *testSessionFacade) Interact(context.Context, any) error { return nil }

// ClearSession 空实现
func (f *testSessionFacade) ClearSession(context.Context) {}

// testCallbackContext 构造一个只挂 session 的回调上下文。
func testCallbackContext(sessionID string) *agentinterfaces.AgentCallbackContext {
	return agentinterfaces.NewAgentCallbackContext(nil, &agentinterfaces.InvokeInputs{}, &testSessionFacade{id: sessionID})
}

// TestRunCommitCallbacks_单条失败不影响其余 验证单条 error 只告警。
// Python: for callback in callbacks: try: ... except Exception: logger.warning
func TestRunCommitCallbacks_单条失败不影响其余(t *testing.T) {
	cbc := testCallbackContext("s1")
	var hits []int
	cbc.SetExtra(prompts.CommitCallbacksExtraKey, []func() error{
		func() error { return errors.New("boom") },
		func() error { hits = append(hits, 2); return nil },
	})
	require.NotPanics(t, func() { runCommitCallbacks(cbc) })
	require.Equal(t, []int{2}, hits)
}

// TestRunCommitCallbacks_panic不掀翻调用方 验证 panic 被隔离。
// Go 特有：Python 只捕 Exception，panic 等价物是进程级崩溃（spec D-2）。
func TestRunCommitCallbacks_panic不掀翻调用方(t *testing.T) {
	cbc := testCallbackContext("s1")
	var hits []int
	cbc.SetExtra(prompts.CommitCallbacksExtraKey, []func() error{
		func() error { panic("cb panic") },
		func() error { hits = append(hits, 2); return nil },
	})
	require.NotPanics(t, func() { runCommitCallbacks(cbc) })
	require.Equal(t, []int{2}, hits)
}

// TestRunCommitCallbacks_类型不符直接丢弃 验证非 []func() error 被忽略。
// Python: if not isinstance(callbacks, list): return
func TestRunCommitCallbacks_类型不符直接丢弃(t *testing.T) {
	cbc := testCallbackContext("s1")
	cbc.SetExtra(prompts.CommitCallbacksExtraKey, "not a list")
	require.NotPanics(t, func() { runCommitCallbacks(cbc) })
	_, still := cbc.GetExtra(prompts.CommitCallbacksExtraKey)
	require.False(t, still, "无论类型对不对，键都必须被取走")
}

// TestRunCommitCallbacks_空上下文不panic 验证 nil 守卫。
func TestRunCommitCallbacks_空上下文不panic(t *testing.T) {
	require.NotPanics(t, func() { runCommitCallbacks(nil) })
}

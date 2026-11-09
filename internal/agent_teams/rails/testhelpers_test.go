package rails

import (
	"testing"

	"context"
	"github.com/uapclaw/uapclaw-go/internal/agent_teams/tracker"
	sessioninterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/session/interfaces"
	sessionstate "github.com/uapclaw/uapclaw-go/internal/agentcore/session/state"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
)

// ──────────────────────────── 测试替身 ────────────────────────────

// fakeTaskTracker 任务通知器的测试替身。
//
// 嵌入 tracker.UnimplementedTaskEventTracker 补齐 Announce/Wakes/Release
// （规则 10：接口扩张时改替身，不改接口）。
type fakeTaskTracker struct {
	tracker.UnimplementedTaskEventTracker
	// notice 待发的通知（取一次即清空，模拟真实 tracker 的「先 claim 先得」）
	notice *tracker.ClaimedNotice
	// persistCount persist 被调用次数
	persistCount int
}

// Claim 见 tracker.TaskEventTracker。
func (f *fakeTaskTracker) Claim(_ sessioninterfaces.SessionFacade) *tracker.ClaimedNotice {
	n := f.notice
	f.notice = nil
	return n
}

// Persist 见 tracker.TaskEventTracker。
func (f *fakeTaskTracker) Persist(_ sessioninterfaces.SessionFacade) { f.persistCount++ }

// fakeSessionFacade 极简会话，只为 state 读写与 Commit。
type fakeSessionFacade struct {
	id    string
	state map[string]any
}

// newFakeSessionFacade 构造测试会话。
func newFakeSessionFacade(id string) *fakeSessionFacade {
	return &fakeSessionFacade{id: id, state: map[string]any{}}
}

// GetSessionID 见 sessioninterfaces.SessionFacade。
func (f *fakeSessionFacade) GetSessionID() string { return f.id }

// UpdateState 见 sessioninterfaces.SessionFacade。
func (f *fakeSessionFacade) UpdateState(d map[string]any) {
	for k, v := range d {
		f.state[k] = v
	}
}

// GetState 见 sessioninterfaces.SessionFacade。
func (f *fakeSessionFacade) GetState(k sessionstate.StateKey) (any, error) {
	return f.state[k.String()], nil
}

// DumpState 见 sessioninterfaces.SessionFacade。
func (f *fakeSessionFacade) DumpState() map[string]any { return f.state }

// WriteStream 见 sessioninterfaces.SessionFacade。
func (f *fakeSessionFacade) WriteStream(_ context.Context, _ any) error { return nil }

// WriteCustomStream 见 sessioninterfaces.SessionFacade。
func (f *fakeSessionFacade) WriteCustomStream(_ context.Context, _ any) error { return nil }

// GetEnv 见 sessioninterfaces.SessionFacade。
func (f *fakeSessionFacade) GetEnv(_ string, _ ...any) any { return nil }

// Interact 见 sessioninterfaces.SessionFacade。
func (f *fakeSessionFacade) Interact(_ context.Context, _ any) error { return nil }

// ClearSession 见 sessioninterfaces.SessionFacade。
func (f *fakeSessionFacade) ClearSession(_ context.Context) { f.state = map[string]any{} }

// Commit 见 *session.Session 的 flush（tracker 用类型断言调用）。
func (f *fakeSessionFacade) Commit(_ context.Context) error { return nil }

// newTestCallbackContext 构造带指定 inputs 的回调上下文（无会话、无 agent）。
//
// 多数钩子用例只验证输入改写，不需要会话。
func newTestCallbackContext(inputs agentinterfaces.EventInputs) *agentinterfaces.AgentCallbackContext {
	return agentinterfaces.NewAgentCallbackContext(nil, inputs, nil)
}

// newTestCallbackContextWithSession 构造带会话的回调上下文。
//
// 任务通知与状态折入需要 session；agent 仍为 nil（附件管理器因此为空，
// 走「历史消息车道」的兜底分支）。
func newTestCallbackContextWithSession(
	inputs agentinterfaces.EventInputs,
	sess sessioninterfaces.SessionFacade,
) *agentinterfaces.AgentCallbackContext {
	return agentinterfaces.NewAgentCallbackContext(nil, inputs, sess)
}

// 确保导入被使用。
var _ = testing.Short

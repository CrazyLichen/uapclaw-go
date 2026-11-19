package flow

import (
	"context"
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/session"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/config"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/interfaces"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/state"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/stream"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/graph/stream_actor"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/tracer"
)

// fakeInnerSession 最小 InnerSession 桩（组件基类测试依赖面）。
// 仿 graph/vertex_test.go 同名桩：满足 interfaces.InnerSession 全量方法。
type fakeInnerSession struct {
	cfg config.SessionConfig
	id  string
}

func (f *fakeInnerSession) Config() config.SessionConfig                     { return f.cfg }
func (f *fakeInnerSession) State() state.SessionState                        { return state.NewInMemoryWorkflowState() }
func (f *fakeInnerSession) Tracer() *tracer.Tracer                           { return nil }
func (f *fakeInnerSession) StreamWriterManager() *stream.StreamWriterManager { return nil }
func (f *fakeInnerSession) SessionID() string                                { return f.id }
func (f *fakeInnerSession) Checkpointer() interfaces.Checkpointer            { return nil }
func (f *fakeInnerSession) ActorManager() *stream_actor.ActorManager         { return nil }
func (f *fakeInnerSession) Close() error                                     { return nil }

// newFakeVertexSession 构造经真实 NodeSession 管线的顶点会话（供组件四钩子校验）。
func newFakeVertexSession(t *testing.T, nodeID, nodeType string) session.WorkflowVertexSession {
	t.Helper()
	parent := &fakeInnerSession{cfg: config.NewSessionConfig(context.Background()), id: "s1"}
	return session.NewWorkflowVertexSession(parent, nodeID, nodeType, false)
}

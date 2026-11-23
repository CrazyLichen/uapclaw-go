package tool

import (
	"context"
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/graph/stream_actor"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/session"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/session/config"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/session/interfaces"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/session/state"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/session/stream"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/session/tracer"
)

// fakeInnerSession 最小 InnerSession 桩（llm/testutil_test.go 同款）
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

// newTestNodeSession 构造真实 NodeSessionFacade（经 NewComponentSession 管线）
func newTestNodeSession(t *testing.T) *session.NodeSessionFacade {
	t.Helper()
	parent := &fakeInnerSession{cfg: config.NewSessionConfig(context.Background()), id: "s1"}
	vsess := session.NewWorkflowVertexSession(parent, "n1", "ToolComponent", false)
	compSess, err := session.NewComponentSession(vsess, false)
	if err != nil {
		t.Fatal(err)
	}
	return compSess
}

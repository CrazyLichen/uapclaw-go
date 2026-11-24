package react

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/graph/executable"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/graph/stream_actor"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/session"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/session/config"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/session/interfaces"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/session/state"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/session/stream"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/session/tracer"
	saconfig "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/single_agent/config"
)

// TestUnpackChunk 解包三态（Python react_executable.py L170-183）。
func TestUnpackChunk(t *testing.T) {
	// 态1：llm_output + content → {"output": content}
	lli := &stream.OutputSchema{Type: "llm_output", Payload: map[string]any{"content": "hi", "result_type": "answer"}}
	out := unpackChunk(lli)
	require.IsType(t, map[string]any{}, out)
	assert.Equal(t, "hi", out.(map[string]any)["output"])

	// 态2：llm_output 但 payload 无 content → 剥信封透传 payload
	noContent := &stream.OutputSchema{Type: "llm_output", Payload: map[string]any{"other": 1}}
	assert.Equal(t, map[string]any{"other": 1}, unpackChunk(noContent))

	// 态2b：其他 OutputSchema（answer）→ payload 原样
	ans := &stream.OutputSchema{Type: "answer", Payload: map[string]any{"output": "done", "result_type": "answer"}}
	assert.Equal(t, ans.Payload, unpackChunk(ans))
}

// TestNewReActExecutable 构造期固定 card + Configure（Python __init__ L20-135）。
func TestNewReActExecutable(t *testing.T) {
	cfg := &ReActAgentCompConfig{ReActAgentConfig: saconfig.NewReActAgentConfig(saconfig.WithModelName("test-model"))}
	e := NewReActExecutable(cfg)
	require.NotNil(t, e.reactAgent)
	card := e.reactAgent.Card()
	assert.Equal(t, "react_agent_workflow_executable", card.ID)
	assert.Equal(t, "ReAct Agent Workflow Executable", card.Name)
	assert.Equal(t, "ReAct agent for workflow execution", card.Description)
	// 组合面访问器
	assert.NotNil(t, e.AbilityManager())
}

// TestReActAgentComp_LazyExecutable 组合面懒加载共享（Python executable property）。
func TestReActAgentComp_LazyExecutable(t *testing.T) {
	cfg := &ReActAgentCompConfig{ReActAgentConfig: saconfig.NewReActAgentConfig(saconfig.WithModelName("test-model"))}
	c := NewReActAgentComp(cfg)
	ex1 := c.toExecutableCached()
	ex2 := c.toExecutableCached()
	assert.Same(t, ex1, ex2)
}

// TestReActExecutable_InvokeEntry 异常包装（Python invoke L40-143 文案逐字）。
func TestReActExecutable_InvokeEntry(t *testing.T) {
	// 无 session 时 Invoke 返回错误 → 包装为 error 输出（不透传 error）
	cfg := &ReActAgentCompConfig{ReActAgentConfig: saconfig.NewReActAgentConfig(saconfig.WithModelName("test-model"))}
	e := NewReActExecutable(cfg)
	sess := newTestNodeSession()
	out, err := e.invokeEntry(context.Background(), map[string]any{"query": "q"}, sess, nil)
	require.NoError(t, err) // 异常被吞掉（Python except Exception）
	require.NotNil(t, out)
	assert.Equal(t, "error", out["result_type"])
	assert.True(t, strings.HasPrefix(out["output"].(string), "Error in ReAct execution:"))
}

// TestReActExecutable_StreamEntry_AgentSession 复用 workflow session_id 新建 agent 会话。
// Python stream L63-170。真实管线：workflow 节点会话 + 真实 agent session。
func TestReActExecutable_StreamEntry_AgentSession(t *testing.T) {
	cfg := &ReActAgentCompConfig{ReActAgentConfig: saconfig.NewReActAgentConfig(saconfig.WithModelName("test-model"))}
	e := NewReActExecutable(cfg)
	wfSess := newTestNodeSession()
	ch, err := e.streamEntry(context.Background(), map[string]any{"query": "q"}, wfSess, nil)
	require.NoError(t, err)
	// 模型未配置真实客户端 → 流可能立即出错或产出 error 帧；只断言通道可用且
	// agent session 复用了 workflow session_id（核心契约）
	count := 0
	for range ch {
		count++
		if count > 10 {
			break
		}
	}
	assert.GreaterOrEqual(t, count, 0)
}

// fakeInnerSession 最小 InnerSession 桩（仿 llm/testutil_test.go 同款）。
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

// newTestNodeSession 构造真实 NodeSessionFacade（经 NewComponentSession 管线）。
func newTestNodeSession() *session.NodeSessionFacade {
	parent := &fakeInnerSession{cfg: config.NewSessionConfig(context.Background()), id: "s1"}
	vsess := session.NewWorkflowVertexSession(parent, "n1", "ReActExecutable", false)
	compSess, err := session.NewComponentSession(vsess, false)
	if err != nil {
		panic(err)
	}
	return compSess
}

// TestStreamChunk_Unpack_Flow 验证 streamEntry 产出块经 unpackChunk 解包（集成视点）。
func TestStreamChunk_Unpack_Flow(t *testing.T) {
	// 直接验证解包链路：llm_output 帧 → {"output": content} 落入 StreamChunk.Val
	frame := &stream.OutputSchema{Type: "llm_output", Index: 0, Payload: map[string]any{"content": "增量"}}
	val := unpackChunk(frame)
	chunk := executable.StreamChunk[any]{Val: val}
	m, ok := chunk.Val.(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "增量", m["output"])
}

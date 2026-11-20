package checkpointer

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/graph/pregel"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/graph/stream_actor"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/session/config"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/session/constants"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/session/interaction"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/session/interfaces"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/session/state"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/session/stream"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/session/tracer"
	"github.com/uapclaw/uapclaw-go/internal/common/exception"
)

// assertCheckpointerPreError 断言 err 为 CHECKPOINTER_PRE_WORKFLOW_EXECUTION_ERROR。
func assertCheckpointerPreError(t *testing.T, err error) {
	t.Helper()
	var be *exception.BaseError
	if !errors.As(err, &be) || be.Status() != exception.StatusCheckpointerPreWorkflowExecutionError {
		t.Errorf("期望 CHECKPOINTER_PRE_WORKFLOW_EXECUTION_ERROR，实际=%v", err)
	}
}

// ──────────────────────────── 命名空间常量测试 ────────────────────────────

// Test命名空间常量值 测试命名空间常量与 Python 一致
func Test命名空间常量值(t *testing.T) {
	if SessionNamespaceAgent != "agent" {
		t.Errorf("SessionNamespaceAgent 期望 'agent'，实际=%s", SessionNamespaceAgent)
	}
	if SessionNamespaceAgentTeam != "agent-team" {
		t.Errorf("SessionNamespaceAgentTeam 期望 'agent-team'，实际=%s", SessionNamespaceAgentTeam)
	}
	if SessionNamespaceWorkflow != "workflow" {
		t.Errorf("SessionNamespaceWorkflow 期望 'workflow'，实际=%s", SessionNamespaceWorkflow)
	}
	if WorkflowNamespaceGraph != "workflow-graph" {
		t.Errorf("WorkflowNamespaceGraph 期望 'workflow-graph'，实际=%s", WorkflowNamespaceGraph)
	}
}

// ──────────────────────────── BuildKey 测试 ────────────────────────────

// TestBuildKey_单部分 测试单部分键
func TestBuildKey_单部分(t *testing.T) {
	if got := BuildKey("abc"); got != "abc" {
		t.Errorf("BuildKey('abc') = %s，期望 'abc'", got)
	}
}

// TestBuildKey_多部分 测试多部分键
func TestBuildKey_多部分(t *testing.T) {
	if got := BuildKey("a", "b", "c"); got != "a:b:c" {
		t.Errorf("BuildKey('a','b','c') = %s，期望 'a:b:c'", got)
	}
}

// TestBuildKey_空 测试空输入
func TestBuildKey_空(t *testing.T) {
	if got := BuildKey(); got != "" {
		t.Errorf("BuildKey() = %s，期望 ''", got)
	}
}

// ──────────────────────────── BuildKeyWithNamespace 测试 ────────────────────────────

// TestBuildKeyWithNamespace_基本 测试基本构建
func TestBuildKeyWithNamespace_基本(t *testing.T) {
	got := BuildKeyWithNamespace("sess1", "agent", "agent1")
	expected := "sess1:agent:agent1"
	if got != expected {
		t.Errorf("期望 %s，实际=%s", expected, got)
	}
}

// TestBuildKeyWithNamespace_有后缀 测试带后缀
func TestBuildKeyWithNamespace_有后缀(t *testing.T) {
	got := BuildKeyWithNamespace("sess1", "agent", "agent1", "state", "blobs")
	expected := "sess1:agent:agent1:state:blobs"
	if got != expected {
		t.Errorf("期望 %s，实际=%s", expected, got)
	}
}

// ──────────────────────────── GetThreadID 测试 ────────────────────────────

// TestGetThreadID 测试线程 ID 构建
func TestGetThreadID(t *testing.T) {
	session := &testSessionWithWorkflowID{
		testSession: testSession{sessionID: "sess1"},
		workflowID:  "wf1",
	}
	got := GetThreadID(session)
	if got != "sess1:wf1" {
		t.Errorf("GetThreadID = %s，期望 'sess1:wf1'", got)
	}
}

// ──────────────────────────── GetAgentID/GetTeamID 类型断言测试 ────────────────────────────

// TestGetAgentID_满足接口 测试 session 满足 AgentIDProvider
func TestGetAgentID_满足接口(t *testing.T) {
	session := &testSessionWithAgentID{
		testSession: testSession{sessionID: "s1"},
		agentID:     "agent1",
	}
	got := GetAgentID(session)
	if got != "agent1" {
		t.Errorf("GetAgentID = %s，期望 'agent1'", got)
	}
}

// TestGetAgentID_不满足接口 测试 session 不满足 AgentIDProvider 返回空字符串
func TestGetAgentID_不满足接口(t *testing.T) {
	session := &testSession{sessionID: "s1"}
	got := GetAgentID(session)
	if got != "Na" {
		t.Errorf("不满足接口时应返回 Na，实际=%s", got)
	}
}

// TestGetTeamID_满足接口 测试 session 满足 TeamIDProvider
func TestGetTeamID_满足接口(t *testing.T) {
	session := &testSessionWithTeamID{
		testSession: testSession{sessionID: "s1"},
		teamID:      "team1",
	}
	got := GetTeamID(session)
	if got != "team1" {
		t.Errorf("GetTeamID = %s，期望 'team1'", got)
	}
}

// TestGetTeamID_不满足接口 测试 session 不满足 TeamIDProvider 返回空字符串
func TestGetTeamID_不满足接口(t *testing.T) {
	session := &testSession{sessionID: "s1"}
	got := GetTeamID(session)
	if got != "Na" {
		t.Errorf("不满足接口时应返回 Na，实际=%s", got)
	}
}

// ──────────────────────────── 测试辅助类型 ────────────────────────────

// testSession 最小 interfaces.InnerSession 实现
type testSession struct {
	sessionID string
}

func (s *testSession) SessionID() string                                { return s.sessionID }
func (s *testSession) State() state.SessionState                        { return nil }
func (s *testSession) Config() config.SessionConfig                     { return nil }
func (s *testSession) Tracer() *tracer.Tracer                           { return nil }
func (s *testSession) StreamWriterManager() *stream.StreamWriterManager { return nil }
func (s *testSession) ActorManager() *stream_actor.ActorManager         { return nil }
func (s *testSession) Close() error                                     { return nil }
func (s *testSession) Checkpointer() interfaces.Checkpointer            { return nil }

// testSessionWithWorkflowID 满足 WorkflowIDProvider 的 session
type testSessionWithWorkflowID struct {
	testSession
	workflowID string
}

func (s *testSessionWithWorkflowID) WorkflowID() string { return s.workflowID }

// testSessionWithAgentID 实现 AgentIDProvider 的 session
type testSessionWithAgentID struct {
	testSession
	agentID string
}

func (s *testSessionWithAgentID) AgentID() string { return s.agentID }

// testSessionWithTeamID 实现 TeamIDProvider 的 session
type testSessionWithTeamID struct {
	testSession
	teamID string
}

func (s *testSessionWithTeamID) TeamID() string { return s.teamID }

// ──────────────────────────── ValidateInteractiveResume 测试（8.14 前置件）────────────────────────────

// fakeValidateStorage ValidateInteractiveResume 用的 Storage 测试实现（仅 Exists 有意义）。
type fakeValidateStorage struct{ exists bool }

func (f *fakeValidateStorage) Save(_ context.Context, _ interfaces.InnerSession) error { return nil }
func (f *fakeValidateStorage) Recover(_ context.Context, _ interfaces.InnerSession, _ any) error {
	return nil
}
func (f *fakeValidateStorage) Clear(_ context.Context, _, _ string) error { return nil }
func (f *fakeValidateStorage) Exists(_ context.Context, _ interfaces.InnerSession) (bool, error) {
	return f.exists, nil
}

// fakeValidateGraphStore ValidateInteractiveResume 用的 pregel.Store 测试实现（仅 Get 有意义）。
type fakeValidateGraphStore struct{ state *pregel.GraphState }

func (f *fakeValidateGraphStore) Get(_ context.Context, _, _ string) (*pregel.GraphState, error) {
	return f.state, nil
}
func (f *fakeValidateGraphStore) Save(_ context.Context, _, _ string, _ *pregel.GraphState) error {
	return nil
}
func (f *fakeValidateGraphStore) Delete(_ context.Context, _, _ string) error { return nil }

// newValidateSession 构造带 env 的 Workflow 会话测试实现。
func newValidateSession(t *testing.T, env map[string]any) *testWorkflowSession {
	t.Helper()
	cfg := config.NewSessionConfig(context.Background())
	cfg.SetEnvs(env)
	return &testWorkflowSession{
		testSession: testSession{sessionID: "sess-vr"},
		workflowID:  "wf-vr",
		config:      cfg,
	}
}

// TestValidateInteractiveResume_无WorkflowCheckpoint拒绝 Python base.py:59-72：
// io 侧 workflow state 不存在 → CHECKPOINTER_PRE_WORKFLOW_EXECUTION_ERROR 显式报错。
func TestValidateInteractiveResume_无WorkflowCheckpoint拒绝(t *testing.T) {
	sess := newValidateSession(t, nil)
	err := ValidateInteractiveResume(context.Background(), sess,
		&fakeValidateStorage{exists: false},
		&fakeValidateGraphStore{state: nil}, "in_memory")
	if err == nil {
		t.Fatal("期望错误，实际为 nil")
	}
	assertCheckpointerPreError(t, err)
	if !strings.Contains(err.Error(), "no workflow checkpoint exists for session sess-vr") {
		t.Errorf("错误文案不符：%v", err)
	}
}

// TestValidateInteractiveResume_单组件直调放行 Python base.py:73-76：
// SINGLE_COMPONENT_EXECUTION env 为真 → io 侧校验通过即放行，不查图侧快照。
func TestValidateInteractiveResume_单组件直调放行(t *testing.T) {
	sess := newValidateSession(t, map[string]any{constants.SingleComponentExecutionEnvKey: true})
	err := ValidateInteractiveResume(context.Background(), sess,
		&fakeValidateStorage{exists: true},
		&fakeValidateGraphStore{state: nil}, "in_memory") // 图侧快照本就不存在
	if err != nil {
		t.Fatalf("未期望错误：%v", err)
	}
}

// TestValidateInteractiveResume_图快照可续跑放行 Python base.py:78-80。
func TestValidateInteractiveResume_图快照可续跑放行(t *testing.T) {
	sess := newValidateSession(t, nil)
	resumable := &pregel.GraphState{PendingNode: map[string]pregel.PendingNode{"n1": {NodeName: "n1"}}}
	err := ValidateInteractiveResume(context.Background(), sess,
		&fakeValidateStorage{exists: true},
		&fakeValidateGraphStore{state: resumable}, "persistence")
	if err != nil {
		t.Fatalf("未期望错误：%v", err)
	}
}

// TestValidateInteractiveResume_图快照缺失拒绝 Python base.py:81-93：
// 非单组件直调且图侧快照为 nil → 拒绝（否则引擎全新起跑）。
func TestValidateInteractiveResume_图快照缺失拒绝(t *testing.T) {
	sess := newValidateSession(t, nil)
	err := ValidateInteractiveResume(context.Background(), sess,
		&fakeValidateStorage{exists: true},
		&fakeValidateGraphStore{state: nil}, "persistence")
	if err == nil {
		t.Fatal("期望错误，实际为 nil")
	}
	assertCheckpointerPreError(t, err)
	if !strings.Contains(err.Error(), "graph checkpoint is missing or empty for session sess-vr") {
		t.Errorf("错误文案不符：%v", err)
	}
}

// TestValidateInteractiveResume_图快照不可续跑拒绝 空快照（IsResumable()==false）同样拒绝。
func TestValidateInteractiveResume_图快照不可续跑拒绝(t *testing.T) {
	sess := newValidateSession(t, nil)
	err := ValidateInteractiveResume(context.Background(), sess,
		&fakeValidateStorage{exists: true},
		&fakeValidateGraphStore{state: &pregel.GraphState{}}, "in_memory")
	if err == nil {
		t.Fatal("期望错误，实际为 nil")
	}
	assertCheckpointerPreError(t, err)
}

// TestInMemoryCheckpointer_PreWorkflowExecute_交互恢复无快照显式报错
// 后端接线：InteractiveInput 分支先校验再 recover（inmemory.py:53-63）；
// 全新会话（无任何快照）恢复交互输入必须显式报错，而非静默全新起跑。
func TestInMemoryCheckpointer_PreWorkflowExecute_交互恢复无快照显式报错(t *testing.T) {
	cp := NewInMemoryCheckpointer()
	sess := newValidateSession(t, nil)
	ii, err := interaction.NewInteractiveInput()
	if err != nil {
		t.Fatalf("未期望错误：%v", err)
	}
	err = cp.PreWorkflowExecute(context.Background(), sess, ii)
	if err == nil {
		t.Fatal("期望错误，实际为 nil")
	}
	assertCheckpointerPreError(t, err)
}

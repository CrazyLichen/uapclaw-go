package checkpointer

import (
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/state"
)

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

// testSession 最小 interfaces.BaseSession 实现
type testSession struct {
	sessionID string
}

func (s *testSession) SessionID() string         { return s.sessionID }
func (s *testSession) State() state.SessionState  { return nil }
func (s *testSession) Config() any                { return nil }
func (s *testSession) Tracer() any                { return nil }
func (s *testSession) StreamWriterManager() any     { return nil }
func (s *testSession) ActorManager() any            { return nil }
func (s *testSession) Close() error                 { return nil }
func (s *testSession) Checkpointer() Checkpointer { return nil }

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

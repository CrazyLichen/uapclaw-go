package checkpointer

import (
	"context"
	"fmt"
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/config"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/constants"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/interaction"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/interfaces"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/state"
)

// ──────────────────────────── 测试辅助类型 ────────────────────────────

// testAgentSession Agent 会话测试实现
type testAgentSession struct {
	testSession
	agentID string
	config  config.SessionConfig
	st      state.SessionState
}

func (s *testAgentSession) AgentID() string              { return s.agentID }
func (s *testAgentSession) State() state.SessionState    { return s.st }
func (s *testAgentSession) Config() config.SessionConfig { return s.config }
func (s *testAgentSession) Checkpointer() Checkpointer   { return nil }

// testTeamSession Team 会话测试实现
type testTeamSession struct {
	testSession
	teamID string
	config config.SessionConfig
	st     state.SessionState
}

func (s *testTeamSession) TeamID() string               { return s.teamID }
func (s *testTeamSession) State() state.SessionState    { return s.st }
func (s *testTeamSession) Config() config.SessionConfig { return s.config }

// testWorkflowSession Workflow 会话测试实现（含 WorkflowState）
type testWorkflowSession struct {
	testSession
	config     config.SessionConfig
	st         state.SessionState
	workflowID string
	parent     interfaces.InnerSession
}

func (s *testWorkflowSession) State() state.SessionState      { return s.st }
func (s *testWorkflowSession) Config() config.SessionConfig   { return s.config }
func (s *testWorkflowSession) WorkflowID() string             { return s.workflowID }
func (s *testWorkflowSession) Parent() interfaces.InnerSession { return s.parent }

// ──────────────────────────── NewInMemoryCheckpointer 测试 ────────────────────────────

// TestNewInMemoryCheckpointer 测试创建实例
func TestNewInMemoryCheckpointer(t *testing.T) {
	cp := NewInMemoryCheckpointer()
	if cp == nil {
		t.Fatal("NewInMemoryCheckpointer 返回 nil")
	}
	if cp.agentStores == nil {
		t.Error("agentStores 未初始化")
	}
	if cp.agentTeamStores == nil {
		t.Error("agentTeamStores 未初始化")
	}
	if cp.workflowStores == nil {
		t.Error("workflowStores 未初始化")
	}
	if cp.sessionToWorkflowIDs == nil {
		t.Error("sessionToWorkflowIDs 未初始化")
	}
}

// ──────────────────────────── PreAgentExecute / PostAgentExecute 测试 ────────────────────────────

// TestInMemoryCheckpointer_PreAgentExecute 测试 Agent 执行前恢复状态
func TestInMemoryCheckpointer_PreAgentExecute(t *testing.T) {
	cp := NewInMemoryCheckpointer()
	session := &testAgentSession{
		testSession: testSession{sessionID: "sess1"},
		agentID:     "agent1",
		st:          state.NewAgentStateCollection(),
		config:      config.NewSessionConfig(context.Background()),
	}

	ctx := context.Background()
	err := cp.PreAgentExecute(ctx, session, nil)
	if err != nil {
		t.Fatalf("PreAgentExecute 返回错误：%v", err)
	}

	// 验证 agent store 已创建
	if _, ok := cp.agentStores["sess1"]; !ok {
		t.Error("agent store 未创建")
	}
}

// TestInMemoryCheckpointer_PostAgentExecute 测试 Agent 执行后保存状态
func TestInMemoryCheckpointer_PostAgentExecute(t *testing.T) {
	cp := NewInMemoryCheckpointer()
	st := state.NewAgentStateCollection()
	session := &testAgentSession{
		testSession: testSession{sessionID: "sess1"},
		agentID:     "agent1",
		st:          st,
		config:      config.NewSessionConfig(context.Background()),
	}

	ctx := context.Background()
	// 先执行 Pre 创建 store
	if err := cp.PreAgentExecute(ctx, session, nil); err != nil {
		t.Fatalf("PreAgentExecute 返回错误：%v", err)
	}

	// 更新状态后保存
	if err := st.Update(map[string]any{"key": "value"}); err != nil {
		t.Fatalf("Update 返回错误：%v", err)
	}
	err := cp.PostAgentExecute(ctx, session)
	if err != nil {
		t.Fatalf("PostAgentExecute 返回错误：%v", err)
	}
}

// TestInMemoryCheckpointer_PrePostAgentExecute_状态恢复 测试 Agent 状态保存恢复往返
func TestInMemoryCheckpointer_PrePostAgentExecute_状态恢复(t *testing.T) {
	cp := NewInMemoryCheckpointer()
	st := state.NewAgentStateCollection()
	session := &testAgentSession{
		testSession: testSession{sessionID: "sess1"},
		agentID:     "agent1",
		st:          st,
		config:      config.NewSessionConfig(context.Background()),
	}

	ctx := context.Background()
	// 先 Pre
	if err := cp.PreAgentExecute(ctx, session, nil); err != nil {
		t.Fatalf("PreAgentExecute 返回错误：%v", err)
	}

	// 更新状态
	if err := st.Update(map[string]any{"test_key": "test_value"}); err != nil {
		t.Fatalf("Update 返回错误：%v", err)
	}

	// Post 保存
	if err := cp.PostAgentExecute(ctx, session); err != nil {
		t.Fatalf("PostAgentExecute 返回错误：%v", err)
	}

	// 创建新 session，恢复状态
	st2 := state.NewAgentStateCollection()
	session2 := &testAgentSession{
		testSession: testSession{sessionID: "sess1"},
		agentID:     "agent1",
		st:          st2,
		config:      config.NewSessionConfig(context.Background()),
	}
	if err := cp.PreAgentExecute(ctx, session2, nil); err != nil {
		t.Fatalf("第二次 PreAgentExecute 返回错误：%v", err)
	}

	// 验证状态已恢复
	recovered := st2.GetAgent(state.AllStateKey)
	if recovered == nil {
		t.Fatal("恢复后 Agent 状态为 nil")
	}
}

// TestInMemoryCheckpointer_PostAgentExecute_无Store 测试 Agent 无 store 时返回错误
func TestInMemoryCheckpointer_PostAgentExecute_无Store(t *testing.T) {
	cp := NewInMemoryCheckpointer()
	session := &testAgentSession{
		testSession: testSession{sessionID: "sess1"},
		agentID:     "agent1",
		st:          state.NewAgentStateCollection(),
		config:      config.NewSessionConfig(context.Background()),
	}

	ctx := context.Background()
	err := cp.PostAgentExecute(ctx, session)
	if err == nil {
		t.Error("无 store 时应返回错误")
	}
}

// ──────────────────────────── InterruptAgentExecute 测试 ────────────────────────────

// TestInMemoryCheckpointer_InterruptAgentExecute 测试 Agent 中断保存
func TestInMemoryCheckpointer_InterruptAgentExecute(t *testing.T) {
	cp := NewInMemoryCheckpointer()
	st := state.NewAgentStateCollection()
	session := &testAgentSession{
		testSession: testSession{sessionID: "sess1"},
		agentID:     "agent1",
		st:          st,
		config:      config.NewSessionConfig(context.Background()),
	}

	ctx := context.Background()
	// 先 Pre 创建 store
	if err := cp.PreAgentExecute(ctx, session, nil); err != nil {
		t.Fatalf("PreAgentExecute 返回错误：%v", err)
	}

	// 中断保存
	err := cp.InterruptAgentExecute(ctx, session)
	if err != nil {
		t.Fatalf("InterruptAgentExecute 返回错误：%v", err)
	}
}

// TestInMemoryCheckpointer_InterruptAgentExecute_无Store 测试中断时无 store 返回错误
func TestInMemoryCheckpointer_InterruptAgentExecute_无Store(t *testing.T) {
	cp := NewInMemoryCheckpointer()
	session := &testAgentSession{
		testSession: testSession{sessionID: "sess1"},
		agentID:     "agent1",
		st:          state.NewAgentStateCollection(),
		config:      config.NewSessionConfig(context.Background()),
	}

	ctx := context.Background()
	err := cp.InterruptAgentExecute(ctx, session)
	if err == nil {
		t.Error("无 store 时应返回错误")
	}
}

// ──────────────────────────── PreAgentTeamExecute / PostAgentTeamExecute 测试 ────────────────────────────

// TestInMemoryCheckpointer_PreAgentTeamExecute 测试 Team 执行前恢复状态
func TestInMemoryCheckpointer_PreAgentTeamExecute(t *testing.T) {
	cp := NewInMemoryCheckpointer()
	session := &testTeamSession{
		testSession: testSession{sessionID: "sess1"},
		teamID:      "team1",
		st:          state.NewAgentStateCollection(),
		config:      config.NewSessionConfig(context.Background()),
	}

	ctx := context.Background()
	err := cp.PreAgentTeamExecute(ctx, session, nil)
	if err != nil {
		t.Fatalf("PreAgentTeamExecute 返回错误：%v", err)
	}

	// 验证 team store 已创建
	if _, ok := cp.agentTeamStores["sess1"]; !ok {
		t.Error("agent team store 未创建")
	}
}

// TestInMemoryCheckpointer_PostAgentTeamExecute 测试 Team 执行后保存
func TestInMemoryCheckpointer_PostAgentTeamExecute(t *testing.T) {
	cp := NewInMemoryCheckpointer()
	st := state.NewAgentStateCollection()
	session := &testTeamSession{
		testSession: testSession{sessionID: "sess1"},
		teamID:      "team1",
		st:          st,
		config:      config.NewSessionConfig(context.Background()),
	}

	ctx := context.Background()
	// 先 Pre 创建 store
	if err := cp.PreAgentTeamExecute(ctx, session, nil); err != nil {
		t.Fatalf("PreAgentTeamExecute 返回错误：%v", err)
	}

	// Post 保存
	err := cp.PostAgentTeamExecute(ctx, session)
	if err != nil {
		t.Fatalf("PostAgentTeamExecute 返回错误：%v", err)
	}
}

// TestInMemoryCheckpointer_PostAgentTeamExecute_无Store 测试 Team 无 store 时返回错误
func TestInMemoryCheckpointer_PostAgentTeamExecute_无Store(t *testing.T) {
	cp := NewInMemoryCheckpointer()
	session := &testTeamSession{
		testSession: testSession{sessionID: "sess1"},
		teamID:      "team1",
		st:          state.NewAgentStateCollection(),
		config:      config.NewSessionConfig(context.Background()),
	}

	ctx := context.Background()
	err := cp.PostAgentTeamExecute(ctx, session)
	if err == nil {
		t.Error("无 store 时应返回错误")
	}
}

// ──────────────────────────── SessionExists 测试 ────────────────────────────

// TestInMemoryCheckpointer_SessionExists_存在 测试会话存在判断
func TestInMemoryCheckpointer_SessionExists_存在(t *testing.T) {
	cp := NewInMemoryCheckpointer()
	st := state.NewAgentStateCollection()
	session := &testAgentSession{
		testSession: testSession{sessionID: "sess1"},
		agentID:     "agent1",
		st:          st,
		config:      config.NewSessionConfig(context.Background()),
	}

	ctx := context.Background()
	if err := cp.PreAgentExecute(ctx, session, nil); err != nil {
		t.Fatalf("PreAgentExecute 返回错误：%v", err)
	}

	exists, err := cp.SessionExists(ctx, "sess1")
	if err != nil {
		t.Fatalf("SessionExists 返回错误：%v", err)
	}
	if !exists {
		t.Error("会话应存在")
	}
}

// TestInMemoryCheckpointer_SessionExists_不存在 测试会话不存在判断
func TestInMemoryCheckpointer_SessionExists_不存在(t *testing.T) {
	cp := NewInMemoryCheckpointer()
	ctx := context.Background()

	exists, err := cp.SessionExists(ctx, "nonexistent")
	if err != nil {
		t.Fatalf("SessionExists 返回错误：%v", err)
	}
	if exists {
		t.Error("不存在的会话应返回 false")
	}
}

// ──────────────────────────── Release 测试 ────────────────────────────

// TestInMemoryCheckpointer_Release 测试释放会话资源
func TestInMemoryCheckpointer_Release(t *testing.T) {
	cp := NewInMemoryCheckpointer()
	st := state.NewAgentStateCollection()
	session := &testAgentSession{
		testSession: testSession{sessionID: "sess1"},
		agentID:     "agent1",
		st:          st,
		config:      config.NewSessionConfig(context.Background()),
	}

	ctx := context.Background()
	if err := cp.PreAgentExecute(ctx, session, nil); err != nil {
		t.Fatalf("PreAgentExecute 返回错误：%v", err)
	}

	// 释放
	err := cp.Release(ctx, "sess1")
	if err != nil {
		t.Fatalf("Release 返回错误：%v", err)
	}

	// 验证已清除
	exists, _ := cp.SessionExists(ctx, "sess1")
	if exists {
		t.Error("释放后会话不应存在")
	}
}

// TestInMemoryCheckpointer_Release_不存在Session 测试释放不存在的会话
func TestInMemoryCheckpointer_Release_不存在Session(t *testing.T) {
	cp := NewInMemoryCheckpointer()
	ctx := context.Background()

	err := cp.Release(ctx, "nonexistent")
	if err != nil {
		t.Fatalf("释放不存在的会话不应返回错误：%v", err)
	}
}

// ──────────────────────────── GraphStore 测试 ────────────────────────────

// TestInMemoryCheckpointer_GraphStore 测试 Graph Store 返回 nil（8.7 回填前）
func TestInMemoryCheckpointer_GraphStore(t *testing.T) {
	cp := NewInMemoryCheckpointer()
	gs := cp.GraphStore()
	if gs != nil {
		t.Error("8.7 回填前 GraphStore 应返回 nil")
	}
}

// ──────────────────────────── AgentStorage 单独测试 ────────────────────────────

// TestAgentStorage_SaveRecover 测试 Agent 状态保存恢复
func TestAgentStorage_SaveRecover(t *testing.T) {
	storage := newAgentStorage()
	st := state.NewAgentStateCollection()
	session := &testAgentSession{
		testSession: testSession{sessionID: "sess1"},
		agentID:     "agent1",
		st:          st,
		config:      config.NewSessionConfig(context.Background()),
	}

	ctx := context.Background()

	// 更新状态
	if err := st.Update(map[string]any{"key1": "value1"}); err != nil {
		t.Fatalf("Update 返回错误：%v", err)
	}

	// 保存
	if err := storage.Save(ctx, session); err != nil {
		t.Fatalf("Save 返回错误：%v", err)
	}

	// 创建新 session，恢复
	st2 := state.NewAgentStateCollection()
	session2 := &testAgentSession{
		testSession: testSession{sessionID: "sess1"},
		agentID:     "agent1",
		st:          st2,
		config:      config.NewSessionConfig(context.Background()),
	}
	if err := storage.Recover(ctx, session2, nil); err != nil {
		t.Fatalf("Recover 返回错误：%v", err)
	}

	// 验证恢复
	recovered := st2.GetAgent(state.AllStateKey)
	if recovered == nil {
		t.Fatal("恢复后状态不应为 nil")
	}
}

// TestAgentStorage_Clear 测试清除状态
func TestAgentStorage_Clear(t *testing.T) {
	storage := newAgentStorage()
	st := state.NewAgentStateCollection()
	session := &testAgentSession{
		testSession: testSession{sessionID: "sess1"},
		agentID:     "agent1",
		st:          st,
		config:      config.NewSessionConfig(context.Background()),
	}

	ctx := context.Background()
	if err := storage.Save(ctx, session); err != nil {
		t.Fatalf("Save 返回错误：%v", err)
	}

	// 清除
	if err := storage.Clear(ctx, "agent1", "sess1"); err != nil {
		t.Fatalf("Clear 返回错误：%v", err)
	}

	// 验证已清除
	exists, err := storage.Exists(ctx, session)
	if err != nil {
		t.Fatalf("Exists 返回错误：%v", err)
	}
	if exists {
		t.Error("清除后状态不应存在")
	}
}

// TestAgentStorage_Exists 测试状态存在判断
func TestAgentStorage_Exists(t *testing.T) {
	storage := newAgentStorage()
	st := state.NewAgentStateCollection()
	session := &testAgentSession{
		testSession: testSession{sessionID: "sess1"},
		agentID:     "agent1",
		st:          st,
		config:      config.NewSessionConfig(context.Background()),
	}

	ctx := context.Background()

	// 未保存前不存在
	exists, err := storage.Exists(ctx, session)
	if err != nil {
		t.Fatalf("Exists 返回错误：%v", err)
	}
	if exists {
		t.Error("未保存前不应存在")
	}

	// 保存后存在
	if err := storage.Save(ctx, session); err != nil {
		t.Fatalf("Save 返回错误：%v", err)
	}
	exists, err = storage.Exists(ctx, session)
	if err != nil {
		t.Fatalf("Exists 返回错误：%v", err)
	}
	if !exists {
		t.Error("保存后应存在")
	}
}

// TestAgentStorage_Recover_无数据 测试恢复时无数据不报错
func TestAgentStorage_Recover_无数据(t *testing.T) {
	storage := newAgentStorage()
	st := state.NewAgentStateCollection()
	session := &testAgentSession{
		testSession: testSession{sessionID: "sess1"},
		agentID:     "agent1",
		st:          st,
		config:      config.NewSessionConfig(context.Background()),
	}

	ctx := context.Background()
	err := storage.Recover(ctx, session, nil)
	if err != nil {
		t.Fatalf("无数据时 Recover 不应返回错误：%v", err)
	}
}

// ──────────────────────────── AgentTeamStorage 单独测试 ────────────────────────────

// TestAgentTeamStorage_SaveRecover 测试 Team 状态保存恢复
func TestAgentTeamStorage_SaveRecover(t *testing.T) {
	storage := newAgentTeamStorage()
	st := state.NewAgentStateCollection()
	session := &testTeamSession{
		testSession: testSession{sessionID: "sess1"},
		teamID:      "team1",
		st:          st,
		config:      config.NewSessionConfig(context.Background()),
	}

	ctx := context.Background()

	// 更新全局状态
	st.UpdateGlobal(map[string]any{"global_key": "global_value"})

	// 保存
	if err := storage.Save(ctx, session); err != nil {
		t.Fatalf("Save 返回错误：%v", err)
	}

	// 创建新 session，恢复
	st2 := state.NewAgentStateCollection()
	session2 := &testTeamSession{
		testSession: testSession{sessionID: "sess1"},
		teamID:      "team1",
		st:          st2,
		config:      config.NewSessionConfig(context.Background()),
	}
	if err := storage.Recover(ctx, session2, nil); err != nil {
		t.Fatalf("Recover 返回错误：%v", err)
	}

	// 验证恢复
	recovered := st2.GetGlobal(state.AllStateKey)
	if recovered == nil {
		t.Fatal("恢复后全局状态不应为 nil")
	}
}

// TestAgentTeamStorage_Clear 测试清除 Team 状态
func TestAgentTeamStorage_Clear(t *testing.T) {
	storage := newAgentTeamStorage()
	st := state.NewAgentStateCollection()
	session := &testTeamSession{
		testSession: testSession{sessionID: "sess1"},
		teamID:      "team1",
		st:          st,
		config:      config.NewSessionConfig(context.Background()),
	}

	ctx := context.Background()
	if err := storage.Save(ctx, session); err != nil {
		t.Fatalf("Save 返回错误：%v", err)
	}

	if err := storage.Clear(ctx, "team1", "sess1"); err != nil {
		t.Fatalf("Clear 返回错误：%v", err)
	}

	exists, _ := storage.Exists(ctx, session)
	if exists {
		t.Error("清除后状态不应存在")
	}
}

// ──────────────────────────── WorkflowStorage 单独测试 ────────────────────────────

// TestWorkflowStorage_SaveRecover 测试 Workflow 状态保存恢复
func TestWorkflowStorage_SaveRecover(t *testing.T) {
	storage := newWorkflowStorage()

	// 创建带 WorkflowState 的 session（使用 WorkflowCommitState）
	ioState := state.NewInMemoryCommitState()
	globalState := state.NewInMemoryCommitState()
	compState := state.NewInMemoryCommitState()
	workflowState := state.NewInMemoryCommitState()
	wcs := state.NewWorkflowCommitState(ioState, globalState, compState, workflowState, nil, "parent1", "node1", true)

	session := &testWorkflowSession{
		testSession: testSession{sessionID: "sess1"},
		workflowID:  "wf1",
		config:      config.NewSessionConfig(context.Background()),
		st:          wcs,
	}

	ctx := context.Background()

	// 更新工作流状态
	if err := wcs.Update(map[string]any{"comp_key": "comp_value"}); err != nil {
		t.Fatalf("Update 返回错误：%v", err)
	}

	// 保存
	if err := storage.Save(ctx, session); err != nil {
		t.Fatalf("Save 返回错误：%v", err)
	}

	// 创建新 session，恢复
	ioState2 := state.NewInMemoryCommitState()
	globalState2 := state.NewInMemoryCommitState()
	compState2 := state.NewInMemoryCommitState()
	workflowState2 := state.NewInMemoryCommitState()
	wcs2 := state.NewWorkflowCommitState(ioState2, globalState2, compState2, workflowState2, nil, "parent1", "node1", true)
	session2 := &testWorkflowSession{
		testSession: testSession{sessionID: "sess1"},
		workflowID:  "wf1",
		config:      config.NewSessionConfig(context.Background()),
		st:          wcs2,
	}

	if err := storage.Recover(ctx, session2, nil); err != nil {
		t.Fatalf("Recover 返回错误：%v", err)
	}
}

// TestWorkflowStorage_Clear 测试清除 Workflow 状态
func TestWorkflowStorage_Clear(t *testing.T) {
	storage := newWorkflowStorage()

	ioState := state.NewInMemoryCommitState()
	globalState := state.NewInMemoryCommitState()
	compState := state.NewInMemoryCommitState()
	workflowState := state.NewInMemoryCommitState()
	wcs := state.NewWorkflowCommitState(ioState, globalState, compState, workflowState, nil, "parent1", "node1", true)

	session := &testWorkflowSession{
		testSession: testSession{sessionID: "sess1"},
		workflowID:  "wf1",
		config:      config.NewSessionConfig(context.Background()),
		st:          wcs,
	}

	ctx := context.Background()
	if err := storage.Save(ctx, session); err != nil {
		t.Fatalf("Save 返回错误：%v", err)
	}

	if err := storage.Clear(ctx, "wf1", "sess1"); err != nil {
		t.Fatalf("Clear 返回错误：%v", err)
	}

	exists, _ := storage.Exists(ctx, session)
	if exists {
		t.Error("清除后状态不应存在")
	}
}

// TestWorkflowStorage_Exists_未保存 测试未保存时不存在
func TestWorkflowStorage_Exists_未保存(t *testing.T) {
	storage := newWorkflowStorage()

	ioState := state.NewInMemoryCommitState()
	globalState := state.NewInMemoryCommitState()
	compState := state.NewInMemoryCommitState()
	workflowState := state.NewInMemoryCommitState()
	wcs := state.NewWorkflowCommitState(ioState, globalState, compState, workflowState, nil, "parent1", "node1", true)

	session := &testWorkflowSession{
		testSession: testSession{sessionID: "sess1"},
		workflowID:  "wf1",
		config:      config.NewSessionConfig(context.Background()),
		st:          wcs,
	}

	ctx := context.Background()
	exists, err := storage.Exists(ctx, session)
	if err != nil {
		t.Fatalf("Exists 返回错误：%v", err)
	}
	if exists {
		t.Error("未保存时不应存在")
	}
}

// ──────────────────────────── PreAgentExecute 带 inputs 测试 ────────────────────────────

// TestInMemoryCheckpointer_PreAgentExecute_带Inputs 测试 Pre 时带交互输入
func TestInMemoryCheckpointer_PreAgentExecute_带Inputs(t *testing.T) {
	cp := NewInMemoryCheckpointer()
	st := state.NewAgentStateCollection()
	session := &testAgentSession{
		testSession: testSession{sessionID: "sess1"},
		agentID:     "agent1",
		st:          st,
		config:      config.NewSessionConfig(context.Background()),
	}

	ctx := context.Background()
	err := cp.PreAgentExecute(ctx, session, "test_input")
	if err != nil {
		t.Fatalf("PreAgentExecute 返回错误：%v", err)
	}

	// 验证交互输入已设置到 state
	inputs := st.Get(state.StringKey(constants.InteractiveInputKey))
	if inputs == nil {
		t.Error("交互输入应已设置到 state")
	}
}

// TestInMemoryCheckpointer_PreAgentTeamExecute_带Inputs 测试 Team Pre 时带交互输入
func TestInMemoryCheckpointer_PreAgentTeamExecute_带Inputs(t *testing.T) {
	cp := NewInMemoryCheckpointer()
	st := state.NewAgentStateCollection()
	session := &testTeamSession{
		testSession: testSession{sessionID: "sess1"},
		teamID:      "team1",
		st:          st,
		config:      config.NewSessionConfig(context.Background()),
	}

	ctx := context.Background()
	err := cp.PreAgentTeamExecute(ctx, session, "team_input")
	if err != nil {
		t.Fatalf("PreAgentTeamExecute 返回错误：%v", err)
	}
}

// ──────────────────────────── PreWorkflowExecute 测试 ────────────────────────────

// TestInMemoryCheckpointer_PreWorkflowExecute_交互输入 测试工作流执行前恢复（交互输入）
func TestInMemoryCheckpointer_PreWorkflowExecute_交互输入(t *testing.T) {
	cp := NewInMemoryCheckpointer()

	ioState := state.NewInMemoryCommitState()
	globalState := state.NewInMemoryCommitState()
	compState := state.NewInMemoryCommitState()
	workflowState := state.NewInMemoryCommitState()
	wcs := state.NewWorkflowCommitState(ioState, globalState, compState, workflowState, nil, "parent1", "node1", true)

	session := &testWorkflowSession{
		testSession: testSession{sessionID: "sess1"},
		workflowID:  "wf1",
		config:      config.NewSessionConfig(context.Background()),
		st:          wcs,
	}

	ctx := context.Background()
	ii, _ := interaction.NewInteractiveInput()
	err := cp.PreWorkflowExecute(ctx, session, ii)
	if err != nil {
		t.Fatalf("PreWorkflowExecute 返回错误：%v", err)
	}

	// 验证 workflow store 已创建
	cp.mu.RLock()
	_, ok := cp.workflowStores["sess1"]
	cp.mu.RUnlock()
	if !ok {
		t.Error("workflow store 未创建")
	}
}

// TestInMemoryCheckpointer_PreWorkflowExecute_空输入无状态 测试空输入且无已存状态时正常返回
func TestInMemoryCheckpointer_PreWorkflowExecute_空输入无状态(t *testing.T) {
	cp := NewInMemoryCheckpointer()

	ioState := state.NewInMemoryCommitState()
	globalState := state.NewInMemoryCommitState()
	compState := state.NewInMemoryCommitState()
	workflowState := state.NewInMemoryCommitState()
	wcs := state.NewWorkflowCommitState(ioState, globalState, compState, workflowState, nil, "parent1", "node1", true)

	session := &testWorkflowSession{
		testSession: testSession{sessionID: "sess1"},
		workflowID:  "wf1",
		config:      config.NewSessionConfig(context.Background()),
		st:          wcs,
	}

	ctx := context.Background()
	err := cp.PreWorkflowExecute(ctx, session, nil)
	if err != nil {
		t.Fatalf("PreWorkflowExecute 返回错误：%v", err)
	}
}

// ──────────────────────────── PostWorkflowExecute 测试 ────────────────────────────

// TestInMemoryCheckpointer_PostWorkflowExecute_正常完成 测试工作流正常完成时清除状态
func TestInMemoryCheckpointer_PostWorkflowExecute_正常完成(t *testing.T) {
	cp := NewInMemoryCheckpointer()

	ioState := state.NewInMemoryCommitState()
	globalState := state.NewInMemoryCommitState()
	compState := state.NewInMemoryCommitState()
	workflowState := state.NewInMemoryCommitState()
	wcs := state.NewWorkflowCommitState(ioState, globalState, compState, workflowState, nil, "parent1", "node1", true)

	session := &testWorkflowSession{
		testSession: testSession{sessionID: "sess1"},
		workflowID:  "wf1",
		config:      config.NewSessionConfig(context.Background()),
		st:          wcs,
	}

	ctx := context.Background()
	// 先 Pre
	ii, _ := interaction.NewInteractiveInput()
	if err := cp.PreWorkflowExecute(ctx, session, ii); err != nil {
		t.Fatalf("PreWorkflowExecute 返回错误：%v", err)
	}

	// 正常完成（无中断）
	err := cp.PostWorkflowExecute(ctx, session, map[string]any{}, nil)
	if err != nil {
		t.Fatalf("PostWorkflowExecute 返回错误：%v", err)
	}
}

// TestInMemoryCheckpointer_PostWorkflowExecute_异常 测试工作流异常时保存检查点
func TestInMemoryCheckpointer_PostWorkflowExecute_异常(t *testing.T) {
	cp := NewInMemoryCheckpointer()

	ioState := state.NewInMemoryCommitState()
	globalState := state.NewInMemoryCommitState()
	compState := state.NewInMemoryCommitState()
	workflowState := state.NewInMemoryCommitState()
	wcs := state.NewWorkflowCommitState(ioState, globalState, compState, workflowState, nil, "parent1", "node1", true)

	session := &testWorkflowSession{
		testSession: testSession{sessionID: "sess1"},
		workflowID:  "wf1",
		config:      config.NewSessionConfig(context.Background()),
		st:          wcs,
	}

	ctx := context.Background()
	// 先 Pre
	ii, _ := interaction.NewInteractiveInput()
	if err := cp.PreWorkflowExecute(ctx, session, ii); err != nil {
		t.Fatalf("PreWorkflowExecute 返回错误：%v", err)
	}

	// 异常完成
	testErr := fmt.Errorf("test exception")
	err := cp.PostWorkflowExecute(ctx, session, nil, testErr)
	if err == nil {
		t.Error("异常时应返回错误")
	}
}

// ──────────────────────────── 常量测试 ────────────────────────────

// TestForceDelWorkflowStateKey 测试常量值
func TestForceDelWorkflowStateKey(t *testing.T) {
	if constants.ForceDelWorkflowStateKey != "_force_del_workflow_state" {
		t.Errorf("ForceDelWorkflowStateKey 期望 '_force_del_workflow_state'，实际=%s", constants.ForceDelWorkflowStateKey)
	}
}

// TestInteractiveInputKey 测试常量值
func TestInteractiveInputKey(t *testing.T) {
	if constants.InteractiveInputKey != "__interactive_input__" {
		t.Errorf("InteractiveInputKey 期望 '__interactive_input__'，实际=%s", constants.InteractiveInputKey)
	}
}

// ──────────────────────────── isInteractiveInput 测试 ────────────────────────────

// TestIsInteractiveInput 测试交互输入判断
// 对齐 Python: isinstance(inputs, InteractiveInput)
func TestIsInteractiveInput(t *testing.T) {
	if isInteractiveInput(nil) {
		t.Error("nil 不应视为交互输入")
	}
	if isInteractiveInput("some_input") {
		t.Error("字符串不应视为交互输入")
	}
	if isInteractiveInput(map[string]any{"key": "val"}) {
		t.Error("map 不应视为交互输入")
	}
	ii, _ := interaction.NewInteractiveInput()
	if !isInteractiveInput(ii) {
		t.Error("*InteractiveInput 应视为交互输入")
	}
}

// ──────────────────────────── isWorkflowInterrupted 测试 ────────────────────────────

// TestIsWorkflowInterrupted 测试中断判断
func TestIsWorkflowInterrupted(t *testing.T) {
	if isWorkflowInterrupted(nil) {
		t.Error("nil 不应视为中断")
	}
	if isWorkflowInterrupted(map[string]any{}) {
		t.Error("空 map 不应视为中断")
	}
	if isWorkflowInterrupted(map[string]any{"result": "ok"}) {
		t.Error("无中断标记不应视为中断")
	}
	if !isWorkflowInterrupted(map[string]any{"__interrupt__": "some_value"}) {
		t.Error("有中断标记应视为中断")
	}
}

// ──────────────────────────── PreWorkflowExecute 补充测试 ────────────────────────────

// TestInMemoryCheckpointer_PreWorkflowExecute_有状态非交互输入 测试已有状态但非交互输入时报错
func TestInMemoryCheckpointer_PreWorkflowExecute_有状态非交互输入(t *testing.T) {
	cp := NewInMemoryCheckpointer()

	ioState := state.NewInMemoryCommitState()
	globalState := state.NewInMemoryCommitState()
	compState := state.NewInMemoryCommitState()
	workflowState := state.NewInMemoryCommitState()
	wcs := state.NewWorkflowCommitState(ioState, globalState, compState, workflowState, nil, "parent1", "node1", true)

	session := &testWorkflowSession{
		testSession: testSession{sessionID: "sess1"},
		workflowID:  "wf1",
		config:      config.NewSessionConfig(context.Background()),
		st:          wcs,
	}

	ctx := context.Background()
	// 先保存一次状态
	ii, _ := interaction.NewInteractiveInput()
	if err := cp.PreWorkflowExecute(ctx, session, ii); err != nil {
		t.Fatalf("第一次 PreWorkflowExecute 返回错误：%v", err)
	}
	// 保存检查点
	if err := cp.innerSaveWorkflowCheckpoint(ctx, "wf1", "sess1", session, "test save"); err != nil {
		t.Fatalf("innerSaveWorkflowCheckpoint 返回错误：%v", err)
	}

	// 再次 Pre，nil 输入且已有状态，应报错
	err := cp.PreWorkflowExecute(ctx, session, nil)
	if err == nil {
		t.Error("已有状态但非交互输入时应返回错误")
	}
}

// TestInMemoryCheckpointer_PreWorkflowExecute_强制删除 测试 ForceDelWorkflowStateKey
func TestInMemoryCheckpointer_PreWorkflowExecute_强制删除(t *testing.T) {
	cp := NewInMemoryCheckpointer()

	ioState := state.NewInMemoryCommitState()
	globalState := state.NewInMemoryCommitState()
	compState := state.NewInMemoryCommitState()
	workflowState := state.NewInMemoryCommitState()
	wcs := state.NewWorkflowCommitState(ioState, globalState, compState, workflowState, nil, "parent1", "node1", true)

	session := &testWorkflowSession{
		testSession: testSession{sessionID: "sess1"},
		workflowID:  "wf1",
		config: func() config.SessionConfig {
			cfg := config.NewSessionConfig(context.Background())
			cfg.SetEnvs(map[string]any{constants.ForceDelWorkflowStateKey: true})
			return cfg
		}(),
		st: wcs,
	}

	ctx := context.Background()
	// 先保存一次状态
	ii, _ := interaction.NewInteractiveInput()
	if err := cp.PreWorkflowExecute(ctx, session, ii); err != nil {
		t.Fatalf("第一次 PreWorkflowExecute 返回错误：%v", err)
	}
	if err := cp.innerSaveWorkflowCheckpoint(ctx, "wf1", "sess1", session, "test save"); err != nil {
		t.Fatalf("innerSaveWorkflowCheckpoint 返回错误：%v", err)
	}

	// 再次 Pre，ForceDelWorkflowStateKey=true，应清除后正常返回
	err := cp.PreWorkflowExecute(ctx, session, nil)
	if err != nil {
		t.Fatalf("强制删除模式不应返回错误：%v", err)
	}
}

// ──────────────────────────── PostWorkflowExecute 补充测试 ────────────────────────────

// TestInMemoryCheckpointer_PostWorkflowExecute_中断 测试工作流中断时保存检查点
func TestInMemoryCheckpointer_PostWorkflowExecute_中断(t *testing.T) {
	cp := NewInMemoryCheckpointer()

	ioState := state.NewInMemoryCommitState()
	globalState := state.NewInMemoryCommitState()
	compState := state.NewInMemoryCommitState()
	workflowState := state.NewInMemoryCommitState()
	wcs := state.NewWorkflowCommitState(ioState, globalState, compState, workflowState, nil, "parent1", "node1", true)

	session := &testWorkflowSession{
		testSession: testSession{sessionID: "sess1"},
		workflowID:  "wf1",
		config:      config.NewSessionConfig(context.Background()),
		st:          wcs,
	}

	ctx := context.Background()
	ii, _ := interaction.NewInteractiveInput()
	if err := cp.PreWorkflowExecute(ctx, session, ii); err != nil {
		t.Fatalf("PreWorkflowExecute 返回错误：%v", err)
	}

	// 中断完成
	err := cp.PostWorkflowExecute(ctx, session, map[string]any{"__interrupt__": "interrupted"}, nil)
	if err != nil {
		t.Fatalf("中断时 PostWorkflowExecute 不应返回错误：%v", err)
	}
}

// TestInMemoryCheckpointer_PostWorkflowExecute_正常完成AgentSession 测试父为 AgentSession 时不清除 store
func TestInMemoryCheckpointer_PostWorkflowExecute_正常完成AgentSession(t *testing.T) {
	cp := NewInMemoryCheckpointer()

	ioState := state.NewInMemoryCommitState()
	globalState := state.NewInMemoryCommitState()
	compState := state.NewInMemoryCommitState()
	workflowState := state.NewInMemoryCommitState()
	wcs := state.NewWorkflowCommitState(ioState, globalState, compState, workflowState, nil, "parent1", "node1", true)

	// 创建一个 parent session 满足 AgentIDProvider
	parentSession := &testAgentSession{
		testSession: testSession{sessionID: "parent-sess1"},
		agentID:     "parent-agent1",
		st:          state.NewAgentStateCollection(),
		config:      config.NewSessionConfig(context.Background()),
	}

	session := &testWorkflowSession{
		testSession: testSession{sessionID: "sess1"},
		workflowID:  "wf1",
		config:      config.NewSessionConfig(context.Background()),
		st:          wcs,
		parent:      parentSession,
	}

	ctx := context.Background()
	ii, _ := interaction.NewInteractiveInput()
	if err := cp.PreWorkflowExecute(ctx, session, ii); err != nil {
		t.Fatalf("PreWorkflowExecute 返回错误：%v", err)
	}

	// 正常完成
	err := cp.PostWorkflowExecute(ctx, session, map[string]any{}, nil)
	if err != nil {
		t.Fatalf("PostWorkflowExecute 返回错误：%v", err)
	}

	// AgentSession 父会话时，workflow store 应保留
	cp.mu.RLock()
	_, exists := cp.workflowStores["sess1"]
	cp.mu.RUnlock()
	if !exists {
		t.Error("AgentSession 子会话正常完成时，workflow store 应保留")
	}
}

// ──────────────────────────── Release 补充测试 ────────────────────────────

// TestInMemoryCheckpointer_Release_全量释放 测试全量释放含 workflow 和 team
func TestInMemoryCheckpointer_Release_全量释放(t *testing.T) {
	cp := NewInMemoryCheckpointer()

	ioState := state.NewInMemoryCommitState()
	globalState := state.NewInMemoryCommitState()
	compState := state.NewInMemoryCommitState()
	workflowState := state.NewInMemoryCommitState()
	wcs := state.NewWorkflowCommitState(ioState, globalState, compState, workflowState, nil, "parent1", "node1", true)

	agentSt := state.NewAgentStateCollection()
	teamSt := state.NewAgentStateCollection()

	agentSession := &testAgentSession{
		testSession: testSession{sessionID: "sess1"},
		agentID:     "agent1",
		st:          agentSt,
		config:      config.NewSessionConfig(context.Background()),
	}
	teamSession := &testTeamSession{
		testSession: testSession{sessionID: "sess1"},
		teamID:      "team1",
		st:          teamSt,
		config:      config.NewSessionConfig(context.Background()),
	}
	workflowSession := &testWorkflowSession{
		testSession: testSession{sessionID: "sess1"},
		workflowID:  "wf1",
		config:      config.NewSessionConfig(context.Background()),
		st:          wcs,
	}

	ctx := context.Background()
	if err := cp.PreAgentExecute(ctx, agentSession, nil); err != nil {
		t.Fatalf("PreAgentExecute 返回错误：%v", err)
	}
	if err := cp.PreAgentTeamExecute(ctx, teamSession, nil); err != nil {
		t.Fatalf("PreAgentTeamExecute 返回错误：%v", err)
	}
	ii, _ := interaction.NewInteractiveInput()
	if err := cp.PreWorkflowExecute(ctx, workflowSession, ii); err != nil {
		t.Fatalf("PreWorkflowExecute 返回错误：%v", err)
	}

	// 全量释放
	err := cp.Release(ctx, "sess1")
	if err != nil {
		t.Fatalf("Release 返回错误：%v", err)
	}

	exists, _ := cp.SessionExists(ctx, "sess1")
	if exists {
		t.Error("全量释放后会话不应存在")
	}
}

// ──────────────────────────── WorkflowStorage 补充测试 ────────────────────────────

// TestWorkflowStorage_SaveRecover_带更新 测试保存恢复含状态更新
func TestWorkflowStorage_SaveRecover_带更新(t *testing.T) {
	storage := newWorkflowStorage()

	ioState := state.NewInMemoryCommitState()
	globalState := state.NewInMemoryCommitState()
	compState := state.NewInMemoryCommitState()
	workflowState := state.NewInMemoryCommitState()
	wcs := state.NewWorkflowCommitState(ioState, globalState, compState, workflowState, nil, "parent1", "node1", true)

	session := &testWorkflowSession{
		testSession: testSession{sessionID: "sess1"},
		workflowID:  "wf1",
		config:      config.NewSessionConfig(context.Background()),
		st:          wcs,
	}

	ctx := context.Background()

	// 更新工作流状态
	if err := wcs.Update(map[string]any{"comp_key": "comp_value"}); err != nil {
		t.Fatalf("Update 返回错误：%v", err)
	}

	// 保存
	if err := storage.Save(ctx, session); err != nil {
		t.Fatalf("Save 返回错误：%v", err)
	}

	// 创建新 session，恢复
	ioState2 := state.NewInMemoryCommitState()
	globalState2 := state.NewInMemoryCommitState()
	compState2 := state.NewInMemoryCommitState()
	workflowState2 := state.NewInMemoryCommitState()
	wcs2 := state.NewWorkflowCommitState(ioState2, globalState2, compState2, workflowState2, nil, "parent1", "node1", true)
	session2 := &testWorkflowSession{
		testSession: testSession{sessionID: "sess1"},
		workflowID:  "wf1",
		config:      config.NewSessionConfig(context.Background()),
		st:          wcs2,
	}

	if err := storage.Recover(ctx, session2, nil); err != nil {
		t.Fatalf("Recover 返回错误：%v", err)
	}
}

// TestWorkflowStorage_Recover_带交互输入 测试恢复时处理交互输入
func TestWorkflowStorage_Recover_带交互输入(t *testing.T) {
	storage := newWorkflowStorage()

	ioState := state.NewInMemoryCommitState()
	globalState := state.NewInMemoryCommitState()
	compState := state.NewInMemoryCommitState()
	workflowState := state.NewInMemoryCommitState()
	wcs := state.NewWorkflowCommitState(ioState, globalState, compState, workflowState, nil, "parent1", "node1", true)

	session := &testWorkflowSession{
		testSession: testSession{sessionID: "sess1"},
		workflowID:  "wf1",
		config:      config.NewSessionConfig(context.Background()),
		st:          wcs,
	}

	ctx := context.Background()

	// 保存
	if err := storage.Save(ctx, session); err != nil {
		t.Fatalf("Save 返回错误：%v", err)
	}

	// 带交互输入恢复
	ioState2 := state.NewInMemoryCommitState()
	globalState2 := state.NewInMemoryCommitState()
	compState2 := state.NewInMemoryCommitState()
	workflowState2 := state.NewInMemoryCommitState()
	wcs2 := state.NewWorkflowCommitState(ioState2, globalState2, compState2, workflowState2, nil, "parent1", "node1", true)
	session2 := &testWorkflowSession{
		testSession: testSession{sessionID: "sess1"},
		workflowID:  "wf1",
		config:      config.NewSessionConfig(context.Background()),
		st:          wcs2,
	}
	ii, _ := interaction.NewInteractiveInput()
	if err := storage.Recover(ctx, session2, ii); err != nil {
		t.Fatalf("带交互输入 Recover 返回错误：%v", err)
	}
}

// TestWorkflowStorage_Recover_单个输入 测试恢复时 InteractiveInput.RawInputs 路径
func TestWorkflowStorage_Recover_单个输入(t *testing.T) {
	storage := newWorkflowStorage()

	ioState := state.NewInMemoryCommitState()
	globalState := state.NewInMemoryCommitState()
	compState := state.NewInMemoryCommitState()
	workflowState := state.NewInMemoryCommitState()
	wcs := state.NewWorkflowCommitState(ioState, globalState, compState, workflowState, nil, "parent1", "node1", true)

	session := &testWorkflowSession{
		testSession: testSession{sessionID: "sess1"},
		workflowID:  "wf1",
		config:      config.NewSessionConfig(context.Background()),
		st:          wcs,
	}

	ctx := context.Background()

	// 先保存
	if err := storage.Save(ctx, session); err != nil {
		t.Fatalf("Save 返回错误：%v", err)
	}

	// 用 InteractiveInput.RawInputs 恢复
	ioState2 := state.NewInMemoryCommitState()
	globalState2 := state.NewInMemoryCommitState()
	compState2 := state.NewInMemoryCommitState()
	workflowState2 := state.NewInMemoryCommitState()
	wcs2 := state.NewWorkflowCommitState(ioState2, globalState2, compState2, workflowState2, nil, "parent1", "node1", true)
	session2 := &testWorkflowSession{
		testSession: testSession{sessionID: "sess1"},
		workflowID:  "wf1",
		config:      config.NewSessionConfig(context.Background()),
		st:          wcs2,
	}

	ii, _ := interaction.NewInteractiveInput("simple_input")
	if err := storage.Recover(ctx, session2, ii); err != nil {
		t.Fatalf("RawInputs 恢复返回错误：%v", err)
	}
}

// ──────────────────────────── AgentTeamStorage 补充测试 ────────────────────────────

// TestAgentTeamStorage_Exists 测试 Team 状态存在判断
func TestAgentTeamStorage_Exists(t *testing.T) {
	storage := newAgentTeamStorage()
	st := state.NewAgentStateCollection()
	session := &testTeamSession{
		testSession: testSession{sessionID: "sess1"},
		teamID:      "team1",
		st:          st,
		config:      config.NewSessionConfig(context.Background()),
	}

	ctx := context.Background()

	// 未保存前不存在
	exists, err := storage.Exists(ctx, session)
	if err != nil {
		t.Fatalf("Exists 返回错误：%v", err)
	}
	if exists {
		t.Error("未保存前不应存在")
	}

	// 保存后存在
	if err := storage.Save(ctx, session); err != nil {
		t.Fatalf("Save 返回错误：%v", err)
	}
	exists, err = storage.Exists(ctx, session)
	if err != nil {
		t.Fatalf("Exists 返回错误：%v", err)
	}
	if !exists {
		t.Error("保存后应存在")
	}
}

// ──────────────────────────── 边界条件测试 ────────────────────────────

// TestInMemoryCheckpointer_PreWorkflowExecute_空Map输入 测试空 map 输入
func TestInMemoryCheckpointer_PreWorkflowExecute_空Map输入(t *testing.T) {
	cp := NewInMemoryCheckpointer()

	ioState := state.NewInMemoryCommitState()
	globalState := state.NewInMemoryCommitState()
	compState := state.NewInMemoryCommitState()
	workflowState := state.NewInMemoryCommitState()
	wcs := state.NewWorkflowCommitState(ioState, globalState, compState, workflowState, nil, "parent1", "node1", true)

	session := &testWorkflowSession{
		testSession: testSession{sessionID: "sess1"},
		workflowID:  "wf1",
		config:      config.NewSessionConfig(context.Background()),
		st:          wcs,
	}

	ctx := context.Background()
	// 空 map 不是 *InteractiveInput，不视为交互输入
	err := cp.PreWorkflowExecute(ctx, session, map[string]any{})
	if err != nil {
		t.Fatalf("空 map 输入不应返回错误：%v", err)
	}
}

// TestInMemoryCheckpointer_PostWorkflowExecute_无Store 测试无 workflow store 时报错
func TestInMemoryCheckpointer_PostWorkflowExecute_无Store(t *testing.T) {
	cp := NewInMemoryCheckpointer()

	ioState := state.NewInMemoryCommitState()
	globalState := state.NewInMemoryCommitState()
	compState := state.NewInMemoryCommitState()
	workflowState := state.NewInMemoryCommitState()
	wcs := state.NewWorkflowCommitState(ioState, globalState, compState, workflowState, nil, "parent1", "node1", true)

	session := &testWorkflowSession{
		testSession: testSession{sessionID: "nonexist"},
		workflowID:  "wf1",
		config:      config.NewSessionConfig(context.Background()),
		st:          wcs,
	}

	ctx := context.Background()
	// 异常完成但无 store
	err := cp.PostWorkflowExecute(ctx, session, nil, fmt.Errorf("test error"))
	if err == nil {
		t.Error("异常且无 store 时应返回错误")
	}
}

// TestInMemoryCheckpointer_PostWorkflowExecute_中断无Store 测试中断且无 workflow store 时报错
func TestInMemoryCheckpointer_PostWorkflowExecute_中断无Store(t *testing.T) {
	cp := NewInMemoryCheckpointer()

	ioState := state.NewInMemoryCommitState()
	globalState := state.NewInMemoryCommitState()
	compState := state.NewInMemoryCommitState()
	workflowState := state.NewInMemoryCommitState()
	wcs := state.NewWorkflowCommitState(ioState, globalState, compState, workflowState, nil, "parent1", "node1", true)

	session := &testWorkflowSession{
		testSession: testSession{sessionID: "nonexist"},
		workflowID:  "wf1",
		config:      config.NewSessionConfig(context.Background()),
		st:          wcs,
	}

	ctx := context.Background()
	// 中断完成但无 store
	err := cp.PostWorkflowExecute(ctx, session, map[string]any{"__interrupt__": "value"}, nil)
	if err == nil {
		t.Error("中断且无 store 时应返回错误")
	}
}

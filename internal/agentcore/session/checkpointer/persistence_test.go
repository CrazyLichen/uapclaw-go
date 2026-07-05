package checkpointer

import (
	"context"
	"fmt"
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/store/kv"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/config"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/constants"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/interaction"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/interfaces"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/state"
)

// ──────────────────────────── 构造函数测试 ────────────────────────────

// TestNewPersistenceCheckpointer 测试创建实例
func TestNewPersistenceCheckpointer(t *testing.T) {
	store := kv.NewInMemoryKVStore()
	cp := NewPersistenceCheckpointer(store)
	if cp == nil {
		t.Fatal("NewPersistenceCheckpointer 返回 nil")
	}
	if cp.kvStore == nil {
		t.Error("kvStore 未初始化")
	}
	if cp.agentStorage == nil {
		t.Error("agentStorage 未初始化")
	}
	if cp.agentTeamStorage == nil {
		t.Error("agentTeamStorage 未初始化")
	}
	if cp.workflowStorage == nil {
		t.Error("workflowStorage 未初始化")
	}
}

// ──────────────────────────── PreAgentExecute / PostAgentExecute 测试 ────────────────────────────

// TestPersistenceCheckpointer_PreAgentExecute 测试 Agent 执行前恢复状态
func TestPersistenceCheckpointer_PreAgentExecute(t *testing.T) {
	store := kv.NewInMemoryKVStore()
	cp := NewPersistenceCheckpointer(store)
	session := &testAgentSession{
		testSession: testSession{sessionID: "sess1"},
		agentID:     "agent1",
		st:          state.NewAgentStateCollection(),
		config:      config.NewSessionConfig(context.Background()),
	}

	ctx := context.Background()
	// 首次恢复（空状态），不应报错
	if err := cp.PreAgentExecute(ctx, session, nil); err != nil {
		t.Fatalf("PreAgentExecute 返回错误：%v", err)
	}
}

// TestPersistenceCheckpointer_PostAgentExecute 测试 Agent 执行后保存检查点
func TestPersistenceCheckpointer_PostAgentExecute(t *testing.T) {
	store := kv.NewInMemoryKVStore()
	cp := NewPersistenceCheckpointer(store)
	st := state.NewAgentStateCollection()
	session := &testAgentSession{
		testSession: testSession{sessionID: "sess1"},
		agentID:     "agent1",
		st:          st,
		config:      config.NewSessionConfig(context.Background()),
	}

	ctx := context.Background()
	// 保存状态
	if err := st.Update(map[string]any{"key": "value"}); err != nil {
		t.Fatalf("Update 返回错误：%v", err)
	}
	if err := cp.PostAgentExecute(ctx, session); err != nil {
		t.Fatalf("PostAgentExecute 返回错误：%v", err)
	}
}

// TestPersistenceCheckpointer_Agent完整流程 测试 Agent 完整保存-恢复流程
func TestPersistenceCheckpointer_Agent完整流程(t *testing.T) {
	store := kv.NewInMemoryKVStore()
	cp := NewPersistenceCheckpointer(store)
	ctx := context.Background()

	// 1. 保存阶段
	st1 := state.NewAgentStateCollection()
	if err := st1.Update(map[string]any{"key1": "value1"}); err != nil {
		t.Fatalf("st1.Update 返回错误：%v", err)
	}
	session1 := &testAgentSession{
		testSession: testSession{sessionID: "sess1"},
		agentID:     "agent1",
		st:          st1,
		config:      config.NewSessionConfig(context.Background()),
	}
	if err := cp.PostAgentExecute(ctx, session1); err != nil {
		t.Fatalf("PostAgentExecute 返回错误：%v", err)
	}

	// 2. 恢复阶段
	st2 := state.NewAgentStateCollection()
	session2 := &testAgentSession{
		testSession: testSession{sessionID: "sess1"},
		agentID:     "agent1",
		st:          st2,
		config:      config.NewSessionConfig(context.Background()),
	}
	if err := cp.PreAgentExecute(ctx, session2, nil); err != nil {
		t.Fatalf("PreAgentExecute 返回错误：%v", err)
	}

	// 3. 验证状态恢复
	got := st2.Get(state.StringKey("key1"))
	if got != "value1" {
		t.Errorf("恢复后 key1 = %v，期望 'value1'", got)
	}
}

// ──────────────────────────── PreAgentTeamExecute / PostAgentTeamExecute 测试 ────────────────────────────

// TestPersistenceCheckpointer_PreAgentTeamExecute 测试 AgentTeam 执行前恢复状态
func TestPersistenceCheckpointer_PreAgentTeamExecute(t *testing.T) {
	store := kv.NewInMemoryKVStore()
	cp := NewPersistenceCheckpointer(store)
	session := &testTeamSession{
		testSession: testSession{sessionID: "sess1"},
		teamID:      "team1",
		st:          state.NewAgentStateCollection(),
		config:      config.NewSessionConfig(context.Background()),
	}

	ctx := context.Background()
	if err := cp.PreAgentTeamExecute(ctx, session, nil); err != nil {
		t.Fatalf("PreAgentTeamExecute 返回错误：%v", err)
	}
}

// TestPersistenceCheckpointer_PostAgentTeamExecute 测试 AgentTeam 执行后保存
func TestPersistenceCheckpointer_PostAgentTeamExecute(t *testing.T) {
	store := kv.NewInMemoryKVStore()
	cp := NewPersistenceCheckpointer(store)
	st := state.NewAgentStateCollection()
	session := &testTeamSession{
		testSession: testSession{sessionID: "sess1"},
		teamID:      "team1",
		st:          st,
		config:      config.NewSessionConfig(context.Background()),
	}

	ctx := context.Background()
	st.UpdateGlobal(map[string]any{"global_key": "global_val"})
	if err := cp.PostAgentTeamExecute(ctx, session); err != nil {
		t.Fatalf("PostAgentTeamExecute 返回错误：%v", err)
	}
}

// TestPersistenceCheckpointer_AgentTeam完整流程 测试 AgentTeam 完整保存-恢复流程
func TestPersistenceCheckpointer_AgentTeam完整流程(t *testing.T) {
	store := kv.NewInMemoryKVStore()
	cp := NewPersistenceCheckpointer(store)
	ctx := context.Background()

	// 1. 保存阶段
	st1 := state.NewAgentStateCollection()
	st1.UpdateGlobal(map[string]any{"global_key": "global_val"})
	session1 := &testTeamSession{
		testSession: testSession{sessionID: "sess1"},
		teamID:      "team1",
		st:          st1,
		config:      config.NewSessionConfig(context.Background()),
	}
	if err := cp.PostAgentTeamExecute(ctx, session1); err != nil {
		t.Fatalf("PostAgentTeamExecute 返回错误：%v", err)
	}

	// 2. 恢复阶段
	st2 := state.NewAgentStateCollection()
	session2 := &testTeamSession{
		testSession: testSession{sessionID: "sess1"},
		teamID:      "team1",
		st:          st2,
		config:      config.NewSessionConfig(context.Background()),
	}
	if err := cp.PreAgentTeamExecute(ctx, session2, nil); err != nil {
		t.Fatalf("PreAgentTeamExecute 返回错误：%v", err)
	}

	// 3. 验证状态恢复
	got := st2.GetGlobal(state.StringKey("global_key"))
	if got != "global_val" {
		t.Errorf("恢复后 global_key = %v，期望 'global_val'", got)
	}
}

// ──────────────────────────── InterruptAgentExecute 测试 ────────────────────────────

// TestPersistenceCheckpointer_InterruptAgentExecute 测试 Agent 中断时保存检查点
func TestPersistenceCheckpointer_InterruptAgentExecute(t *testing.T) {
	store := kv.NewInMemoryKVStore()
	cp := NewPersistenceCheckpointer(store)
	st := state.NewAgentStateCollection()
	session := &testAgentSession{
		testSession: testSession{sessionID: "sess1"},
		agentID:     "agent1",
		st:          st,
		config:      config.NewSessionConfig(context.Background()),
	}

	ctx := context.Background()
	if err := st.Update(map[string]any{"interrupt_key": "interrupt_val"}); err != nil {
		t.Fatalf("st.Update 返回错误：%v", err)
	}
	if err := cp.InterruptAgentExecute(ctx, session); err != nil {
		t.Fatalf("InterruptAgentExecute 返回错误：%v", err)
	}

	// 验证中断后状态可恢复
	st2 := state.NewAgentStateCollection()
	session2 := &testAgentSession{
		testSession: testSession{sessionID: "sess1"},
		agentID:     "agent1",
		st:          st2,
		config:      config.NewSessionConfig(context.Background()),
	}
	if err := cp.PreAgentExecute(ctx, session2, nil); err != nil {
		t.Fatalf("PreAgentExecute 返回错误：%v", err)
	}
	got := st2.Get(state.StringKey("interrupt_key"))
	if got != "interrupt_val" {
		t.Errorf("恢复后 interrupt_key = %v，期望 'interrupt_val'", got)
	}
}

// ──────────────────────────── PreWorkflowExecute / PostWorkflowExecute 测试 ────────────────────────────

// newTestWorkflowCommitState 创建测试用的 WorkflowCommitState
func newTestWorkflowCommitState() *state.WorkflowCommitState {
	ioState := state.NewInMemoryCommitState()
	globalState := state.NewInMemoryCommitState()
	compState := state.NewInMemoryCommitState()
	workflowState := state.NewInMemoryCommitState()
	return state.NewWorkflowCommitState(ioState, globalState, compState, workflowState, nil, "parent1", "node1", true)
}

// TestPersistenceCheckpointer_PreWorkflowExecute_新会话 测试新工作流首次执行
func TestPersistenceCheckpointer_PreWorkflowExecute_新会话(t *testing.T) {
	store := kv.NewInMemoryKVStore()
	cp := NewPersistenceCheckpointer(store)
	wcs := newTestWorkflowCommitState()
	session := &testWorkflowSession{
		testSession: testSession{sessionID: "sess1"},
		st:          wcs,
		config:      config.NewSessionConfig(context.Background()),
	}

	ctx := context.Background()
	// 新工作流，无交互输入 → 不存在检查点 → 返回 nil
	if err := cp.PreWorkflowExecute(ctx, session, nil); err != nil {
		t.Fatalf("PreWorkflowExecute 返回错误：%v", err)
	}
}

// TestPersistenceCheckpointer_PostWorkflowExecute_中断保存 测试工作流中断时保存检查点
func TestPersistenceCheckpointer_PostWorkflowExecute_中断保存(t *testing.T) {
	store := kv.NewInMemoryKVStore()
	cp := NewPersistenceCheckpointer(store)
	wcs := newTestWorkflowCommitState()
	wcs.UpdateGlobal(map[string]any{"wf_key": "wf_val"})
	session := &testWorkflowSession{
		testSession: testSession{sessionID: "sess1"},
		st:          wcs,
		config:      config.NewSessionConfig(context.Background()),
	}

	ctx := context.Background()
	// 中断结果包含 __interrupt__
	result := map[string]any{"__interrupt__": "need_input"}
	if err := cp.PostWorkflowExecute(ctx, session, result, nil); err != nil {
		t.Fatalf("PostWorkflowExecute 返回错误：%v", err)
	}
}

// TestPersistenceCheckpointer_PostWorkflowExecute_正常完成清除 测试工作流正常完成时清除检查点
func TestPersistenceCheckpointer_PostWorkflowExecute_正常完成清除(t *testing.T) {
	store := kv.NewInMemoryKVStore()
	cp := NewPersistenceCheckpointer(store)
	wcs := newTestWorkflowCommitState()
	session := &testWorkflowSession{
		testSession: testSession{sessionID: "sess1"},
		st:          wcs,
		config:      config.NewSessionConfig(context.Background()),
	}

	ctx := context.Background()
	// 先保存一次
	wcs.UpdateGlobal(map[string]any{"wf_key": "wf_val"})
	result := map[string]any{"__interrupt__": "need_input"}
	if err := cp.PostWorkflowExecute(ctx, session, result, nil); err != nil {
		t.Fatalf("PostWorkflowExecute 保存失败：%v", err)
	}

	// 正常完成 → 应清除检查点
	if err := cp.PostWorkflowExecute(ctx, session, map[string]any{"result": "ok"}, nil); err != nil {
		t.Fatalf("PostWorkflowExecute 清除失败：%v", err)
	}
}

// TestPersistenceCheckpointer_PostWorkflowExecute_异常保存 测试工作流异常时保存检查点
func TestPersistenceCheckpointer_PostWorkflowExecute_异常保存(t *testing.T) {
	store := kv.NewInMemoryKVStore()
	cp := NewPersistenceCheckpointer(store)
	wcs := newTestWorkflowCommitState()
	wcs.UpdateGlobal(map[string]any{"wf_key": "wf_val"})
	session := &testWorkflowSession{
		testSession: testSession{sessionID: "sess1"},
		st:          wcs,
		config:      config.NewSessionConfig(context.Background()),
	}

	ctx := context.Background()
	exception := fmt.Errorf("执行出错")
	if err := cp.PostWorkflowExecute(ctx, session, nil, exception); err == nil {
		t.Error("异常时应返回原始 exception 错误")
	}
}

// TestPersistenceCheckpointer_Workflow完整流程 测试工作流完整保存-恢复流程
func TestPersistenceCheckpointer_Workflow完整流程(t *testing.T) {
	store := kv.NewInMemoryKVStore()
	cp := NewPersistenceCheckpointer(store)
	ctx := context.Background()

	// 1. 保存阶段
	wcs1 := newTestWorkflowCommitState()
	wcs1.UpdateGlobal(map[string]any{"wf_key": "wf_val"})
	session1 := &testWorkflowSession{
		testSession: testSession{sessionID: "sess1"},
		st:          wcs1,
		config:      config.NewSessionConfig(context.Background()),
	}
	// 中断保存
	result := map[string]any{"__interrupt__": "need_input"}
	if err := cp.PostWorkflowExecute(ctx, session1, result, nil); err != nil {
		t.Fatalf("PostWorkflowExecute 保存失败：%v", err)
	}

	// 2. 恢复阶段（交互输入）
	wcs2 := newTestWorkflowCommitState()
	session2 := &testWorkflowSession{
		testSession: testSession{sessionID: "sess1"},
		st:          wcs2,
		config:      config.NewSessionConfig(context.Background()),
	}
	inputs, _ := interaction.NewInteractiveInput()
	if err := cp.PreWorkflowExecute(ctx, session2, inputs); err != nil {
		t.Fatalf("PreWorkflowExecute 返回错误：%v", err)
	}
}

// TestPersistenceCheckpointer_PreWorkflowExecute_强制删除 测试 ForceDelWorkflowStateKey
func TestPersistenceCheckpointer_PreWorkflowExecute_强制删除(t *testing.T) {
	store := kv.NewInMemoryKVStore()
	cp := NewPersistenceCheckpointer(store)
	ctx := context.Background()

	// 先保存工作流状态
	wcs1 := newTestWorkflowCommitState()
	wcs1.UpdateGlobal(map[string]any{"wf_key": "wf_val"})
	session1 := &testWorkflowSession{
		testSession: testSession{sessionID: "sess1"},
		st:          wcs1,
		config:      config.NewSessionConfig(context.Background()),
	}
	result := map[string]any{"__interrupt__": "need_input"}
	if err := cp.PostWorkflowExecute(ctx, session1, result, nil); err != nil {
		t.Fatalf("PostWorkflowExecute 保存失败：%v", err)
	}

	// ForceDelWorkflowStateKey=true，应清除后正常返回
	wcs2 := newTestWorkflowCommitState()
	session2 := &testWorkflowSession{
		testSession: testSession{sessionID: "sess1"},
		st:          wcs2,
		config: func() config.SessionConfig {
			cfg := config.NewSessionConfig(context.Background())
			cfg.SetEnvs(map[string]any{constants.ForceDelWorkflowStateKey: true})
			return cfg
		}(),
	}
	if err := cp.PreWorkflowExecute(ctx, session2, nil); err != nil {
		t.Fatalf("PreWorkflowExecute 强制删除失败：%v", err)
	}
}

// TestPersistenceCheckpointer_PreWorkflowExecute_状态存在非交互输入 测试状态存在但非交互输入时报错
func TestPersistenceCheckpointer_PreWorkflowExecute_状态存在非交互输入(t *testing.T) {
	store := kv.NewInMemoryKVStore()
	cp := NewPersistenceCheckpointer(store)
	ctx := context.Background()

	// 先保存工作流状态
	wcs1 := newTestWorkflowCommitState()
	wcs1.UpdateGlobal(map[string]any{"wf_key": "wf_val"})
	session1 := &testWorkflowSession{
		testSession: testSession{sessionID: "sess1"},
		st:          wcs1,
		config:      config.NewSessionConfig(context.Background()),
	}
	result := map[string]any{"__interrupt__": "need_input"}
	if err := cp.PostWorkflowExecute(ctx, session1, result, nil); err != nil {
		t.Fatalf("PostWorkflowExecute 保存失败：%v", err)
	}

	// 非交互输入 + 无 ForceDel → 应报错
	wcs2 := newTestWorkflowCommitState()
	session2 := &testWorkflowSession{
		testSession: testSession{sessionID: "sess1"},
		st:          wcs2,
		config:      config.NewSessionConfig(context.Background()),
	}
	if err := cp.PreWorkflowExecute(ctx, session2, nil); err == nil {
		t.Error("状态存在且非交互输入时应返回错误")
	}
}

// ──────────────────────────── SessionExists 测试 ────────────────────────────

// TestPersistenceCheckpointer_SessionExists_空 测试空 KVStore 不存在
func TestPersistenceCheckpointer_SessionExists_空(t *testing.T) {
	store := kv.NewInMemoryKVStore()
	cp := NewPersistenceCheckpointer(store)
	ctx := context.Background()

	exists, err := cp.SessionExists(ctx, "sess1")
	if err != nil {
		t.Fatalf("SessionExists 返回错误：%v", err)
	}
	if exists {
		t.Error("空 store 不应存在 session")
	}
}

// TestPersistenceCheckpointer_SessionExists_有数据 测试保存后存在
func TestPersistenceCheckpointer_SessionExists_有数据(t *testing.T) {
	store := kv.NewInMemoryKVStore()
	cp := NewPersistenceCheckpointer(store)
	ctx := context.Background()

	// 先保存一些状态
	st := state.NewAgentStateCollection()
	session := &testAgentSession{
		testSession: testSession{sessionID: "sess1"},
		agentID:     "agent1",
		st:          st,
		config:      config.NewSessionConfig(context.Background()),
	}
	if err := st.Update(map[string]any{"key": "value"}); err != nil {
		t.Fatalf("st.Update 返回错误：%v", err)
	}
	if err := cp.PostAgentExecute(ctx, session); err != nil {
		t.Fatalf("PostAgentExecute 返回错误：%v", err)
	}

	// 检查 session 是否存在
	exists, err := cp.SessionExists(ctx, "sess1")
	if err != nil {
		t.Fatalf("SessionExists 返回错误：%v", err)
	}
	if !exists {
		t.Error("保存后 session 应存在")
	}
}

// TestPersistenceCheckpointer_SessionExists_nilKVStore 测试 nil KVStore
func TestPersistenceCheckpointer_SessionExists_nilKVStore(t *testing.T) {
	cp := &PersistenceCheckpointer{}
	ctx := context.Background()

	exists, err := cp.SessionExists(ctx, "sess1")
	if err != nil {
		t.Fatalf("SessionExists 返回错误：%v", err)
	}
	if exists {
		t.Error("nil kvStore 不应存在 session")
	}
}

// ──────────────────────────── Release 测试 ────────────────────────────

// TestPersistenceCheckpointer_Release 测试释放会话资源
func TestPersistenceCheckpointer_Release(t *testing.T) {
	store := kv.NewInMemoryKVStore()
	cp := NewPersistenceCheckpointer(store)
	ctx := context.Background()

	// 先保存一些状态
	st := state.NewAgentStateCollection()
	session := &testAgentSession{
		testSession: testSession{sessionID: "sess1"},
		agentID:     "agent1",
		st:          st,
		config:      config.NewSessionConfig(context.Background()),
	}
	if err := st.Update(map[string]any{"key": "value"}); err != nil {
		t.Fatalf("st.Update 返回错误：%v", err)
	}
	if err := cp.PostAgentExecute(ctx, session); err != nil {
		t.Fatalf("PostAgentExecute 返回错误：%v", err)
	}

	// 验证存在
	exists, _ := cp.SessionExists(ctx, "sess1")
	if !exists {
		t.Fatal("保存后 session 应存在")
	}

	// 释放
	if err := cp.Release(ctx, "sess1"); err != nil {
		t.Fatalf("Release 返回错误：%v", err)
	}

	// 验证已清除
	exists, _ = cp.SessionExists(ctx, "sess1")
	if exists {
		t.Error("Release 后 session 不应存在")
	}
}

// TestPersistenceCheckpointer_Release_nilKVStore 测试 nil KVStore 时释放
func TestPersistenceCheckpointer_Release_nilKVStore(t *testing.T) {
	cp := &PersistenceCheckpointer{}
	ctx := context.Background()

	// nil kvStore 不应报错
	if err := cp.Release(ctx, "sess1"); err != nil {
		t.Fatalf("Release 不应返回错误：%v", err)
	}
}

// ──────────────────────────── GraphStore 测试 ────────────────────────────

// TestPersistenceCheckpointer_GraphStore 测试获取图状态存储（未实现）
func TestPersistenceCheckpointer_GraphStore(t *testing.T) {
	store := kv.NewInMemoryKVStore()
	cp := NewPersistenceCheckpointer(store)

	gs := cp.GraphStore()
	if gs != nil {
		t.Error("GraphStore 当前应为 nil（⤵️ 8.7 回填）")
	}
}

// ──────────────────────────── 交互输入测试 ────────────────────────────

// TestPersistenceCheckpointer_PreAgentExecute_交互输入 测试 Agent 交互输入
func TestPersistenceCheckpointer_PreAgentExecute_交互输入(t *testing.T) {
	store := kv.NewInMemoryKVStore()
	cp := NewPersistenceCheckpointer(store)
	st := state.NewAgentStateCollection()
	session := &testAgentSession{
		testSession: testSession{sessionID: "sess1"},
		agentID:     "agent1",
		st:          st,
		config:      config.NewSessionConfig(context.Background()),
	}

	ctx := context.Background()
	if err := cp.PreAgentExecute(ctx, session, "user_input"); err != nil {
		t.Fatalf("PreAgentExecute 返回错误：%v", err)
	}

	// 验证交互输入已设置到状态
	inputs := st.Get(state.StringKey(constants.InteractiveInputKey))
	if inputs == nil {
		t.Error("交互输入未设置到状态")
	}
}

// TestPersistenceCheckpointer_PreAgentTeamExecute_交互输入 测试 AgentTeam 交互输入
func TestPersistenceCheckpointer_PreAgentTeamExecute_交互输入(t *testing.T) {
	store := kv.NewInMemoryKVStore()
	cp := NewPersistenceCheckpointer(store)
	st := state.NewAgentStateCollection()
	session := &testTeamSession{
		testSession: testSession{sessionID: "sess1"},
		teamID:      "team1",
		st:          st,
		config:      config.NewSessionConfig(context.Background()),
	}

	ctx := context.Background()
	if err := cp.PreAgentTeamExecute(ctx, session, "user_input"); err != nil {
		t.Fatalf("PreAgentTeamExecute 返回错误：%v", err)
	}

	// 验证交互输入已设置到全局状态
	inputs := st.GetGlobal(state.StringKey(constants.InteractiveInputKey))
	if inputs == nil {
		t.Error("交互输入未设置到全局状态")
	}
}

// ──────────────────────────── 多 Agent 隔离测试 ────────────────────────────

// TestPersistenceCheckpointer_多Agent隔离 测试不同 Agent 的状态互相隔离
func TestPersistenceCheckpointer_多Agent隔离(t *testing.T) {
	store := kv.NewInMemoryKVStore()
	cp := NewPersistenceCheckpointer(store)
	ctx := context.Background()

	// Agent1 保存
	st1 := state.NewAgentStateCollection()
	if err := st1.Update(map[string]any{"agent1_key": "agent1_val"}); err != nil {
		t.Fatalf("st1.Update 返回错误：%v", err)
	}
	session1 := &testAgentSession{
		testSession: testSession{sessionID: "sess1"},
		agentID:     "agent1",
		st:          st1,
		config:      config.NewSessionConfig(context.Background()),
	}
	if err := cp.PostAgentExecute(ctx, session1); err != nil {
		t.Fatalf("PostAgentExecute agent1 返回错误：%v", err)
	}

	// Agent2 保存
	st2 := state.NewAgentStateCollection()
	if err := st2.Update(map[string]any{"agent2_key": "agent2_val"}); err != nil {
		t.Fatalf("st2.Update 返回错误：%v", err)
	}
	session2 := &testAgentSession{
		testSession: testSession{sessionID: "sess1"},
		agentID:     "agent2",
		st:          st2,
		config:      config.NewSessionConfig(context.Background()),
	}
	if err := cp.PostAgentExecute(ctx, session2); err != nil {
		t.Fatalf("PostAgentExecute agent2 返回错误：%v", err)
	}

	// 恢复 Agent1
	st1r := state.NewAgentStateCollection()
	session1r := &testAgentSession{
		testSession: testSession{sessionID: "sess1"},
		agentID:     "agent1",
		st:          st1r,
		config:      config.NewSessionConfig(context.Background()),
	}
	if err := cp.PreAgentExecute(ctx, session1r, nil); err != nil {
		t.Fatalf("PreAgentExecute agent1 返回错误：%v", err)
	}

	// 验证 Agent1 只有自己的状态
	if got := st1r.Get(state.StringKey("agent1_key")); got != "agent1_val" {
		t.Errorf("agent1_key = %v，期望 'agent1_val'", got)
	}
	if got := st1r.Get(state.StringKey("agent2_key")); got != nil {
		t.Errorf("agent2_key 应为 nil，实际=%v", got)
	}
}

// ──────────────────────────── Pipeline 辅助函数测试 ────────────────────────────

// TestPipelineGetResult 测试 pipelineGetResult 辅助函数
func TestPipelineGetResult(t *testing.T) {
	results := []kv.PipelineResult{
		{Op: "get", Key: "k1", Value: []byte("v1")},
		{Op: "get", Key: "k2", Value: nil},
	}

	// 正常获取
	val, err := pipelineGetResult(results, 0)
	if err != nil {
		t.Fatalf("pipelineGetResult 返回错误：%v", err)
	}
	if string(val) != "v1" {
		t.Errorf("pipelineGetResult[0] = %s，期望 'v1'", string(val))
	}

	// nil 值
	val, err = pipelineGetResult(results, 1)
	if err != nil {
		t.Fatalf("pipelineGetResult 返回错误：%v", err)
	}
	if val != nil {
		t.Errorf("pipelineGetResult[1] 应为 nil，实际=%v", val)
	}

	// 越界
	val, err = pipelineGetResult(results, 5)
	if err != nil {
		t.Fatalf("pipelineGetResult 越界返回错误：%v", err)
	}
	if val != nil {
		t.Errorf("pipelineGetResult 越界应为 nil，实际=%v", val)
	}

	// 错误结果
	errResults := []kv.PipelineResult{
		{Op: "get", Key: "k1", Err: fmt.Errorf("get error")},
	}
	_, err = pipelineGetResult(errResults, 0)
	if err == nil {
		t.Error("pipelineGetResult 错误结果应返回错误")
	}
}

// TestPipelineExistsResult 测试 pipelineExistsResult 辅助函数
func TestPipelineExistsResult(t *testing.T) {
	results := []kv.PipelineResult{
		{Op: "exists", Key: "k1", Exists: true},
		{Op: "exists", Key: "k2", Exists: false},
	}

	// 存在
	exists, err := pipelineExistsResult(results, 0)
	if err != nil {
		t.Fatalf("pipelineExistsResult 返回错误：%v", err)
	}
	if !exists {
		t.Error("pipelineExistsResult[0] 应为 true")
	}

	// 不存在
	exists, err = pipelineExistsResult(results, 1)
	if err != nil {
		t.Fatalf("pipelineExistsResult 返回错误：%v", err)
	}
	if exists {
		t.Error("pipelineExistsResult[1] 应为 false")
	}

	// 越界
	exists, err = pipelineExistsResult(results, 5)
	if err != nil {
		t.Fatalf("pipelineExistsResult 越界返回错误：%v", err)
	}
	if exists {
		t.Error("pipelineExistsResult 越界应为 false")
	}

	// 错误结果
	errResults := []kv.PipelineResult{
		{Op: "exists", Key: "k1", Err: fmt.Errorf("exists error")},
	}
	_, err = pipelineExistsResult(errResults, 0)
	if err == nil {
		t.Error("pipelineExistsResult 错误结果应返回错误")
	}
}

// ──────────────────────────── EntityHooks 测试 ────────────────────────────

// TestAgentEntityHooks_GetEntityID 测试 Agent 钩子获取实体 ID
func TestAgentEntityHooks_GetEntityID(t *testing.T) {
	h := &agentEntityHooks{}
	session := &testAgentSession{
		testSession: testSession{sessionID: "sess1"},
		agentID:     "agent1",
	}
	got := h.GetEntityID(session)
	if got != "agent1" {
		t.Errorf("GetEntityID = %s，期望 'agent1'", got)
	}
}

// TestAgentEntityHooks_GetStateToSave 测试 Agent 钩子获取状态
func TestAgentEntityHooks_GetStateToSave(t *testing.T) {
	h := &agentEntityHooks{}
	st := state.NewAgentStateCollection()
	if err := st.Update(map[string]any{"key": "value"}); err != nil {
		t.Fatalf("st.Update 返回错误：%v", err)
	}
	session := &testAgentSession{
		testSession: testSession{sessionID: "sess1"},
		agentID:     "agent1",
		st:          st,
	}

	savedState := h.GetStateToSave(session)
	if savedState == nil {
		t.Fatal("GetStateToSave 返回 nil")
	}
	if m, ok := savedState.(map[string]any); !ok {
		t.Errorf("GetStateToSave 期望 map[string]any，实际 %T", savedState)
	} else {
		// GetState() 返回 {"global_state": ..., "agent_state": ...}
		if _, hasAgent := m["agent_state"]; !hasAgent {
			t.Error("GetStateToSave 结果应包含 'agent_state' 键")
		}
	}
}

// TestAgentEntityHooks_GetStateToSave_nilState 测试 Agent 钩子 nil 状态
func TestAgentEntityHooks_GetStateToSave_nilState(t *testing.T) {
	h := &agentEntityHooks{}
	session := &testAgentSession{
		testSession: testSession{sessionID: "sess1"},
		agentID:     "agent1",
		st:          nil,
	}

	savedState := h.GetStateToSave(session)
	if savedState != nil {
		t.Errorf("nil state 应返回 nil，实际=%v", savedState)
	}
}

// TestAgentEntityHooks_RestoreState 测试 Agent 钩子恢复状态
func TestAgentEntityHooks_RestoreState(t *testing.T) {
	h := &agentEntityHooks{}
	st := state.NewAgentStateCollection()
	session := &testAgentSession{
		testSession: testSession{sessionID: "sess1"},
		agentID:     "agent1",
		st:          st,
	}

	// SetState 需要 {"global_state": {...}, "agent_state": {...}} 格式
	if err := h.RestoreState(session, map[string]any{
		"agent_state": map[string]any{"key": "restored"},
	}); err != nil {
		t.Fatalf("RestoreState 返回错误: %v", err)
	}
	got := st.Get(state.StringKey("key"))
	if got != "restored" {
		t.Errorf("RestoreState 后 key = %v，期望 'restored'", got)
	}
}

// TestAgentEntityHooks_RestoreState_nil 测试 Agent 钩子 nil 输入
func TestAgentEntityHooks_RestoreState_nil(t *testing.T) {
	h := &agentEntityHooks{}
	st := state.NewAgentStateCollection()
	session := &testAgentSession{
		testSession: testSession{sessionID: "sess1"},
		agentID:     "agent1",
		st:          st,
	}

	// nil savedState → 不应 panic，不应返回 error
	if err := h.RestoreState(session, nil); err != nil {
		t.Errorf("nil savedState 不应返回错误: %v", err)
	}

	// nil session state → 不应 panic，不应返回 error
	session2 := &testAgentSession{
		testSession: testSession{sessionID: "sess1"},
		agentID:     "agent1",
		st:          nil,
	}
	if err := h.RestoreState(session2, map[string]any{"key": "value"}); err != nil {
		t.Errorf("nil session state 不应返回错误: %v", err)
	}
}

// TestAgentEntityHooks_RestoreState_类型错误 测试 Agent 钩子 savedState 类型断言失败
func TestAgentEntityHooks_RestoreState_类型错误(t *testing.T) {
	h := &agentEntityHooks{}
	st := state.NewAgentStateCollection()
	session := &testAgentSession{
		testSession: testSession{sessionID: "sess1"},
		agentID:     "agent1",
		st:          st,
	}

	// 非 map[string]any 类型应返回 error
	if err := h.RestoreState(session, "invalid type"); err == nil {
		t.Error("savedState 类型错误时应返回 error，但返回 nil")
	}
}

// TestAgentTeamEntityHooks_GetEntityID 测试 AgentTeam 钩子获取实体 ID
func TestAgentTeamEntityHooks_GetEntityID(t *testing.T) {
	h := &agentTeamEntityHooks{}
	session := &testTeamSession{
		testSession: testSession{sessionID: "sess1"},
		teamID:      "team1",
	}
	got := h.GetEntityID(session)
	if got != "team1" {
		t.Errorf("GetEntityID = %s，期望 'team1'", got)
	}
}

// TestAgentTeamEntityHooks_GetStateToSave 测试 AgentTeam 钩子获取状态
func TestAgentTeamEntityHooks_GetStateToSave(t *testing.T) {
	h := &agentTeamEntityHooks{}
	st := state.NewAgentStateCollection()
	st.UpdateGlobal(map[string]any{"global_key": "global_val"})
	session := &testTeamSession{
		testSession: testSession{sessionID: "sess1"},
		teamID:      "team1",
		st:          st,
	}

	savedState := h.GetStateToSave(session)
	if savedState == nil {
		t.Fatal("GetStateToSave 返回 nil")
	}
}

// TestAgentTeamEntityHooks_RestoreState 测试 AgentTeam 钩子恢复状态
func TestAgentTeamEntityHooks_RestoreState(t *testing.T) {
	h := &agentTeamEntityHooks{}
	st := state.NewAgentStateCollection()
	session := &testTeamSession{
		testSession: testSession{sessionID: "sess1"},
		teamID:      "team1",
		st:          st,
	}

	if err := h.RestoreState(session, map[string]any{"global": map[string]any{"global_key": "restored_val"}}); err != nil {
		t.Fatalf("RestoreState 返回错误: %v", err)
	}
}

// TestAgentTeamEntityHooks_RestoreState_nilState 测试 AgentTeam 钩子 nil 输入
func TestAgentTeamEntityHooks_RestoreState_nilState(t *testing.T) {
	h := &agentTeamEntityHooks{}
	st := state.NewAgentStateCollection()
	session := &testTeamSession{
		testSession: testSession{sessionID: "sess1"},
		teamID:      "team1",
		st:          st,
	}

	// nil savedState → 不应 panic，不应返回 error
	if err := h.RestoreState(session, nil); err != nil {
		t.Errorf("nil savedState 不应返回错误: %v", err)
	}
}

// TestAgentTeamEntityHooks_RestoreState_类型错误 测试 AgentTeam 钩子 savedState 类型断言失败
func TestAgentTeamEntityHooks_RestoreState_类型错误(t *testing.T) {
	h := &agentTeamEntityHooks{}
	st := state.NewAgentStateCollection()
	session := &testTeamSession{
		testSession: testSession{sessionID: "sess1"},
		teamID:      "team1",
		st:          st,
	}

	// 非 map[string]any 类型应返回 error
	if err := h.RestoreState(session, 12345); err == nil {
		t.Error("savedState 类型错误时应返回 error，但返回 nil")
	}
}

// ──────────────────────────── basePersistenceStorage 辅助方法测试 ────────────────────────────

// TestBasePersistenceStorage_entityLogExtraKey 测试日志键名
func TestBasePersistenceStorage_entityLogExtraKey(t *testing.T) {
	agentStorage := newPersistenceAgentStorage(kv.NewInMemoryKVStore())
	if key := agentStorage.entityLogExtraKey(); key != "agent_id" {
		t.Errorf("agent entityLogExtraKey = %s，期望 'agent_id'", key)
	}

	teamStorage := newPersistenceAgentTeamStorage(kv.NewInMemoryKVStore())
	if key := teamStorage.entityLogExtraKey(); key != "workflow_id" {
		t.Errorf("team entityLogExtraKey = %s，期望 'workflow_id'", key)
	}
}

// TestBasePersistenceStorage_entityTitleLabel 测试标题标签
func TestBasePersistenceStorage_entityTitleLabel(t *testing.T) {
	agentStorage := newPersistenceAgentStorage(kv.NewInMemoryKVStore())
	if label := agentStorage.entityTitleLabel(); label != "Agent" {
		t.Errorf("agent entityTitleLabel = %s，期望 'Agent'", label)
	}

	teamStorage := newPersistenceAgentTeamStorage(kv.NewInMemoryKVStore())
	if label := teamStorage.entityTitleLabel(); label != "Agent_team" {
		t.Errorf("team entityTitleLabel = %s，期望 'Agent_team'", label)
	}
}

// TestBasePersistenceStorage_buildStateKeys 测试构建 KV 存储键
func TestBasePersistenceStorage_buildStateKeys(t *testing.T) {
	agentStorage := newPersistenceAgentStorage(kv.NewInMemoryKVStore())
	dumpTypeKey, blobKey := agentStorage.buildStateKeys("sess1", "agent1")

	expectedDumpType := "sess1:agent:agent1:agent_state_blobs_dump_type"
	expectedBlob := "sess1:agent:agent1:agent_state_blobs"
	if dumpTypeKey != expectedDumpType {
		t.Errorf("dumpTypeKey = %s，期望 %s", dumpTypeKey, expectedDumpType)
	}
	if blobKey != expectedBlob {
		t.Errorf("blobKey = %s，期望 %s", blobKey, expectedBlob)
	}
}

// ──────────────────────────── PersistenceWorkflowStorage 独立测试 ────────────────────────────

// TestPersistenceWorkflowStorage_SaveAndRecover 测试 Workflow 存储保存与恢复
func TestPersistenceWorkflowStorage_SaveAndRecover(t *testing.T) {
	store := kv.NewInMemoryKVStore()
	ws := newPersistenceWorkflowStorage(store)
	ctx := context.Background()

	wcs := newTestWorkflowCommitState()
	wcs.UpdateGlobal(map[string]any{"wf_key": "wf_val"})
	session := &testWorkflowSession{
		testSession: testSession{sessionID: "sess1"},
		st:          wcs,
		config:      config.NewSessionConfig(context.Background()),
	}

	// 保存
	if err := ws.Save(ctx, session); err != nil {
		t.Fatalf("Save 返回错误：%v", err)
	}

	// 恢复
	wcs2 := newTestWorkflowCommitState()
	session2 := &testWorkflowSession{
		testSession: testSession{sessionID: "sess1"},
		st:          wcs2,
		config:      config.NewSessionConfig(context.Background()),
	}
	if err := ws.Recover(ctx, session2, nil); err != nil {
		t.Fatalf("Recover 返回错误：%v", err)
	}
}

// TestPersistenceWorkflowStorage_Clear 测试 Workflow 存储清除
func TestPersistenceWorkflowStorage_Clear(t *testing.T) {
	store := kv.NewInMemoryKVStore()
	ws := newPersistenceWorkflowStorage(store)
	ctx := context.Background()

	wcs := newTestWorkflowCommitState()
	wcs.UpdateGlobal(map[string]any{"wf_key": "wf_val"})
	session := &testWorkflowSession{
		testSession: testSession{sessionID: "sess1"},
		st:          wcs,
		config:      config.NewSessionConfig(context.Background()),
	}

	// 保存后清除
	if err := ws.Save(ctx, session); err != nil {
		t.Fatalf("Save 返回错误：%v", err)
	}
	if err := ws.Clear(ctx, "wf1", "sess1"); err != nil {
		t.Fatalf("Clear 返回错误：%v", err)
	}
}

// TestPersistenceWorkflowStorage_Exists 测试 Workflow 存储存在检查
func TestPersistenceWorkflowStorage_Exists(t *testing.T) {
	store := kv.NewInMemoryKVStore()
	ws := newPersistenceWorkflowStorage(store)
	ctx := context.Background()

	wcs := newTestWorkflowCommitState()
	session := &testWorkflowSession{
		testSession: testSession{sessionID: "sess1"},
		st:          wcs,
		config:      config.NewSessionConfig(context.Background()),
	}

	// 未保存时不存在
	exists, err := ws.Exists(ctx, session)
	if err != nil {
		t.Fatalf("Exists 返回错误：%v", err)
	}
	if exists {
		t.Error("未保存时不应存在")
	}

	// 保存后存在
	wcs.UpdateGlobal(map[string]any{"wf_key": "wf_val"})
	if err := ws.Save(ctx, session); err != nil {
		t.Fatalf("Save 返回错误：%v", err)
	}
	exists, err = ws.Exists(ctx, session)
	if err != nil {
		t.Fatalf("Exists 返回错误：%v", err)
	}
	if !exists {
		t.Error("保存后应存在")
	}
}

// ──────────────────────────── 接口满足测试 ────────────────────────────

// Test接口满足_PersistenceCheckpointer_Checkpointer 验证 PersistenceCheckpointer 满足 Checkpointer 接口
func Test接口满足_PersistenceCheckpointer_Checkpointer(t *testing.T) {
	var _ interfaces.Checkpointer = (*PersistenceCheckpointer)(nil)
}

// Test接口满足_agentEntityHooks_EntityHooks 验证 agentEntityHooks 满足 EntityHooks 接口
func Test接口满足_agentEntityHooks_EntityHooks(t *testing.T) {
	var _ EntityHooks = (*agentEntityHooks)(nil)
}

// Test接口满足_agentTeamEntityHooks_EntityHooks 验证 agentTeamEntityHooks 满足 EntityHooks 接口
func Test接口满足_agentTeamEntityHooks_EntityHooks(t *testing.T) {
	var _ EntityHooks = (*agentTeamEntityHooks)(nil)
}

// Test接口满足_persistenceProvider_CheckpointerProvider 验证 persistenceProvider 满足 CheckpointerProvider 接口
func Test接口满足_persistenceProvider_CheckpointerProvider(t *testing.T) {
	var _ CheckpointerProvider = (*persistenceProvider)(nil)
}

// ──────────────────────────── basePersistenceStorage.Clear/Exists 测试 ────────────────────────────

// TestBasePersistenceStorage_Clear 测试 Agent 持久化存储清除
func TestBasePersistenceStorage_Clear(t *testing.T) {
	store := kv.NewInMemoryKVStore()
	agentStorage := newPersistenceAgentStorage(store)
	ctx := context.Background()

	// 先保存
	st := state.NewAgentStateCollection()
	if err := st.Update(map[string]any{"key": "value"}); err != nil {
		t.Fatalf("st.Update 返回错误：%v", err)
	}
	session := &testAgentSession{
		testSession: testSession{sessionID: "sess1"},
		agentID:     "agent1",
		st:          st,
		config:      config.NewSessionConfig(context.Background()),
	}
	if err := agentStorage.Save(ctx, session); err != nil {
		t.Fatalf("Save 返回错误：%v", err)
	}

	// 清除
	if err := agentStorage.Clear(ctx, "agent1", "sess1"); err != nil {
		t.Fatalf("Clear 返回错误：%v", err)
	}
}

// TestBasePersistenceStorage_Exists 测试 Agent 持久化存储存在检查
func TestBasePersistenceStorage_Exists(t *testing.T) {
	store := kv.NewInMemoryKVStore()
	agentStorage := newPersistenceAgentStorage(store)
	ctx := context.Background()

	session := &testAgentSession{
		testSession: testSession{sessionID: "sess1"},
		agentID:     "agent1",
		st:          state.NewAgentStateCollection(),
		config:      config.NewSessionConfig(context.Background()),
	}

	// 未保存时不存在
	exists, err := agentStorage.Exists(ctx, session)
	if err != nil {
		t.Fatalf("Exists 返回错误：%v", err)
	}
	if exists {
		t.Error("未保存时不应存在")
	}

	// 保存后存在
	if err := session.st.Update(map[string]any{"key": "value"}); err != nil {
		t.Fatalf("session.st.Update 返回错误：%v", err)
	}
	if err := agentStorage.Save(ctx, session); err != nil {
		t.Fatalf("Save 返回错误：%v", err)
	}
	exists, err = agentStorage.Exists(ctx, session)
	if err != nil {
		t.Fatalf("Exists 返回错误：%v", err)
	}
	if !exists {
		t.Error("保存后应存在")
	}
}

// TestBasePersistenceStorage_Save_序列化失败 测试序列化失败时不报错
func TestBasePersistenceStorage_Save_序列化失败(t *testing.T) {
	store := kv.NewInMemoryKVStore()
	agentStorage := newPersistenceAgentStorage(store)
	ctx := context.Background()

	// 使用不可序列化的值（channel 不能 JSON 序列化）
	st := state.NewAgentStateCollection()
	if err := st.Update(map[string]any{"ch": make(chan int)}); err != nil {
		t.Fatalf("st.Update 返回错误：%v", err)
	}
	session := &testAgentSession{
		testSession: testSession{sessionID: "sess1"},
		agentID:     "agent1",
		st:          st,
		config:      config.NewSessionConfig(context.Background()),
	}

	// 序列化失败，Save 应返回 nil（不报错，仅日志警告）
	if err := agentStorage.Save(ctx, session); err != nil {
		t.Fatalf("Save 序列化失败不应返回错误：%v", err)
	}
}

// TestBasePersistenceStorage_Recover_空数据 测试恢复空数据
func TestBasePersistenceStorage_Recover_空数据(t *testing.T) {
	store := kv.NewInMemoryKVStore()
	agentStorage := newPersistenceAgentStorage(store)
	ctx := context.Background()

	// 无保存数据时恢复
	st := state.NewAgentStateCollection()
	session := &testAgentSession{
		testSession: testSession{sessionID: "sess1"},
		agentID:     "agent1",
		st:          st,
		config:      config.NewSessionConfig(context.Background()),
	}
	if err := agentStorage.Recover(ctx, session, nil); err != nil {
		t.Fatalf("Recover 返回错误：%v", err)
	}
}

// TestBasePersistenceStorage_entityTitleLabel_空标签 测试空标签边界情况
func TestBasePersistenceStorage_entityTitleLabel_空标签(t *testing.T) {
	storage := &basePersistenceStorage{entityLabel: ""}
	if label := storage.entityTitleLabel(); label != "" {
		t.Errorf("空标签应返回空字符串，实际=%s", label)
	}
}

// TestAgentTeamEntityHooks_GetStateToSave_AgentStateCollection 测试 AgentTeam 钩子
// 对齐 Python: AgentTeamStorage._get_state_to_save → session.state().get_global(None) → 只保存 globalState
func TestAgentTeamEntityHooks_GetStateToSave_AgentStateCollection(t *testing.T) {
	h := &agentTeamEntityHooks{}
	st := state.NewAgentStateCollection()
	st.UpdateGlobal(map[string]any{"global_key": "global_val"})
	session := &testTeamSession{
		testSession: testSession{sessionID: "sess1"},
		teamID:      "team1",
		st:          st,
	}

	// GetGlobal(AllStateKey) 只返回 globalState dict，不含 agent_state
	savedState := h.GetStateToSave(session)
	if savedState == nil {
		t.Fatal("GetStateToSave 返回 nil")
	}
	m, ok := savedState.(map[string]any)
	if !ok {
		t.Fatalf("GetStateToSave 期望 map[string]any，实际 %T", savedState)
	}
	// GetGlobal(AllStateKey) 返回纯 globalState dict，应包含写入的 key
	if val, hasKey := m["global_key"]; !hasKey {
		t.Error("GetStateToSave 结果应包含 'global_key' 键")
	} else if val != "global_val" {
		t.Errorf("global_key 期望 'global_val'，实际 %v", val)
	}
	// 不应包含 agent_state（与旧行为不同，旧行为走 GetState() 返回完整状态）
	if _, hasAgent := m["agent_state"]; hasAgent {
		t.Error("GetStateToSave 结果不应包含 'agent_state' 键（只保存 globalState）")
	}
}

// TestAgentTeamEntityHooks_RestoreState_AgentStateCollection 测试 AgentTeam 恢复到 *AgentStateCollection
// 对齐 Python: AgentTeamStorage._restore_state → session.state().global_state.set_state(state)
func TestAgentTeamEntityHooks_RestoreState_AgentStateCollection(t *testing.T) {
	h := &agentTeamEntityHooks{}
	st := state.NewAgentStateCollection()
	session := &testTeamSession{
		testSession: testSession{sessionID: "sess1"},
		teamID:      "team1",
		st:          st,
	}

	// SetGlobal 只恢复 globalState，传入纯 dict（非 {global_state:..., agent_state:...} 格式）
	if err := h.RestoreState(session, map[string]any{"global_key": "restored_val"}); err != nil {
		t.Fatalf("RestoreState 返回错误: %v", err)
	}

	// 验证 globalState 已恢复
	got := st.GetGlobal(state.StringKey("global_key"))
	if got != "restored_val" {
		t.Errorf("global_key 期望 'restored_val'，实际 %v", got)
	}
}

// ──────────────────────────── PreAgentExecute 错误路径测试 ────────────────────────────

// TestPersistenceCheckpointer_PreAgentExecute_恢复失败 测试 Agent 恢复失败时返回错误
func TestPersistenceCheckpointer_PreAgentExecute_恢复失败(t *testing.T) {
	// 使用一个注入错误的 KVStore
	store := &errorKVStore{}
	cp := NewPersistenceCheckpointer(store)
	session := &testAgentSession{
		testSession: testSession{sessionID: "sess1"},
		agentID:     "agent1",
		st:          state.NewAgentStateCollection(),
		config:      config.NewSessionConfig(context.Background()),
	}

	ctx := context.Background()
	err := cp.PreAgentExecute(ctx, session, nil)
	if err == nil {
		t.Error("恢复失败时应返回错误")
	}
}

// TestPersistenceCheckpointer_PostAgentExecute_保存失败 测试 Agent 保存失败时返回错误
func TestPersistenceCheckpointer_PostAgentExecute_保存失败(t *testing.T) {
	store := &errorKVStore{}
	cp := NewPersistenceCheckpointer(store)
	session := &testAgentSession{
		testSession: testSession{sessionID: "sess1"},
		agentID:     "agent1",
		st:          state.NewAgentStateCollection(),
		config:      config.NewSessionConfig(context.Background()),
	}

	ctx := context.Background()
	err := cp.PostAgentExecute(ctx, session)
	if err == nil {
		t.Error("保存失败时应返回错误")
	}
}

// TestPersistenceCheckpointer_PreWorkflowExecute_恢复失败 测试工作流恢复失败时返回错误
func TestPersistenceCheckpointer_PreWorkflowExecute_恢复失败(t *testing.T) {
	store := &errorKVStore{}
	cp := NewPersistenceCheckpointer(store)
	wcs := newTestWorkflowCommitState()
	session := &testWorkflowSession{
		testSession: testSession{sessionID: "sess1"},
		st:          wcs,
		config:      config.NewSessionConfig(context.Background()),
	}

	ctx := context.Background()
	// 交互输入走 Recover 路径
	ii, _ := interaction.NewInteractiveInput()
	err := cp.PreWorkflowExecute(ctx, session, ii)
	if err == nil {
		t.Error("恢复失败时应返回错误")
	}
}

// TestPersistenceCheckpointer_PostWorkflowExecute_保存失败 测试工作流保存失败时返回错误
func TestPersistenceCheckpointer_PostWorkflowExecute_保存失败(t *testing.T) {
	store := &errorKVStore{}
	cp := NewPersistenceCheckpointer(store)
	wcs := newTestWorkflowCommitState()
	session := &testWorkflowSession{
		testSession: testSession{sessionID: "sess1"},
		st:          wcs,
		config:      config.NewSessionConfig(context.Background()),
	}

	ctx := context.Background()
	result := map[string]any{"__interrupt__": "need_input"}
	err := cp.PostWorkflowExecute(ctx, session, result, nil)
	if err == nil {
		t.Error("保存失败时应返回错误")
	}
}

// ──────────────────────────── 错误注入 KVStore ────────────────────────────

// errorKVStore 注入错误的 KVStore，用于测试错误路径
type errorKVStore struct{}

func (e *errorKVStore) Get(_ context.Context, _ string) ([]byte, error) {
	return nil, fmt.Errorf("injected error")
}
func (e *errorKVStore) Set(_ context.Context, _ string, _ []byte) error {
	return fmt.Errorf("injected error")
}
func (e *errorKVStore) ExclusiveSet(_ context.Context, _ string, _ []byte, _ int) (bool, error) {
	return false, fmt.Errorf("injected error")
}
func (e *errorKVStore) Exists(_ context.Context, _ string) (bool, error) {
	return false, fmt.Errorf("injected error")
}
func (e *errorKVStore) Delete(_ context.Context, _ string) error {
	return fmt.Errorf("injected error")
}
func (e *errorKVStore) GetByPrefix(_ context.Context, _ string) (map[string][]byte, error) {
	return nil, fmt.Errorf("injected error")
}
func (e *errorKVStore) DeleteByPrefix(_ context.Context, _ string, _ int) error {
	return fmt.Errorf("injected error")
}
func (e *errorKVStore) MGet(_ context.Context, _ []string) ([][]byte, error) {
	return nil, fmt.Errorf("injected error")
}
func (e *errorKVStore) BatchDelete(_ context.Context, _ []string, _ int) (int, error) {
	return 0, fmt.Errorf("injected error")
}
func (e *errorKVStore) Pipeline(_ context.Context) kv.KVPipeline {
	return &errorPipeline{}
}

// errorPipeline 注入错误的 Pipeline
type errorPipeline struct{}

func (p *errorPipeline) Set(_ context.Context, _ string, _ []byte, _ int) error { return nil }
func (p *errorPipeline) Get(_ context.Context, _ string) error                  { return nil }
func (p *errorPipeline) Exists(_ context.Context, _ string) error               { return nil }
func (p *errorPipeline) Execute(_ context.Context) ([]kv.PipelineResult, error) {
	return nil, fmt.Errorf("injected pipeline error")
}

// ──────────────────────────── T-06: PreWorkflowExecute workflowID 空值防御测试 ────────────────────────────

// TestPreWorkflowExecute_workflowID为空_强制删除 测试 workflowID 为空时跳过清理
// 对应 Python: if workflow_id is None: logger.warning(...) return
func TestPreWorkflowExecute_workflowID为空_强制删除(t *testing.T) {
	store := kv.NewInMemoryKVStore()
	cp := NewPersistenceCheckpointer(store)
	ctx := context.Background()

	// 先用有 workflowID 的 session 保存工作流状态（确保状态存在）
	wcs1 := newTestWorkflowCommitState()
	wcs1.UpdateGlobal(map[string]any{"wf_key": "wf_val"})
	session1 := &testWorkflowSession{
		testSession: testSession{sessionID: "sess-empty-wf"},
		st:          wcs1,
		config:      config.NewSessionConfig(context.Background()),
		workflowID:  "wf-1",
	}
	result := map[string]any{"__interrupt__": "need_input"}
	if err := cp.PostWorkflowExecute(ctx, session1, result, nil); err != nil {
		t.Fatalf("PostWorkflowExecute 保存失败：%v", err)
	}

	// 再用空 workflowID + ForceDel=true 的 session 尝试强制删除
	// 空 workflowID → Exists 返回 false → 直接 return nil（不进入强制删除分支）
	// 所以这个测试验证空 workflowID 不会导致异常
	wcs2 := newTestWorkflowCommitState()
	session2 := &testWorkflowSession{
		testSession: testSession{sessionID: "sess-empty-wf"},
		st:          wcs2,
		config: func() config.SessionConfig {
			cfg := config.NewSessionConfig(context.Background())
			cfg.SetEnvs(map[string]any{constants.ForceDelWorkflowStateKey: true})
			return cfg
		}(),
		workflowID: "", // 空 workflowID
	}
	err := cp.PreWorkflowExecute(ctx, session2, nil)
	if err != nil {
		t.Fatalf("空 workflowID 不应返回错误：%v", err)
	}
}

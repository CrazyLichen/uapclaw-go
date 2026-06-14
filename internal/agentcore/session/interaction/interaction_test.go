package interaction

import (
	"context"
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/state"
)

// ──────────────────────────── fake checkpointer/writer ────────────────────────────

// fakeCheckpointer 测试用检查点器
type fakeCheckpointer struct {
	interruptErr error
	interrupted  bool
}

func (f *fakeCheckpointer) InterruptAgentExecute(session any) error {
	f.interrupted = true
	return f.interruptErr
}

// fakeOutputWriter 测试用输出写入器
type fakeOutputWriter struct {
	written bool
	lastErr error
}

func (f *fakeOutputWriter) WriteInteraction(outputType string, index int, payload any) error {
	f.written = true
	return f.lastErr
}

// fakeOutputWriterProvider 测试用输出写入器提供者
type fakeOutputWriterProvider struct {
	writer *fakeOutputWriter
}

func (f *fakeOutputWriterProvider) GetOutputWriter() InteractionOutputWriter {
	return f.writer
}

// ──────────────────────────── InteractionOutput 测试 ────────────────────────────

// TestInteractionOutput 测试结构体字段
func TestInteractionOutput(t *testing.T) {
	output := InteractionOutput{ID: "node1", Value: "test_val"}
	if output.ID != "node1" {
		t.Errorf("ID 期望 'node1'，实际=%s", output.ID)
	}
	if output.Value != "test_val" {
		t.Errorf("Value 期望 'test_val'，实际=%v", output.Value)
	}
}

// ──────────────────────────── WorkflowInteraction 测试 ────────────────────────────

// TestNewWorkflowInteraction 测试构造函数
func TestNewWorkflowInteraction(t *testing.T) {
	session := newFakeBaseSession()
	wi := NewWorkflowInteraction(session)

	if wi == nil {
		t.Fatal("NewWorkflowInteraction 返回 nil")
	}
	if wi.nodeID != "test.exec.id" {
		t.Errorf("nodeID 期望 'test.exec.id'，实际=%s", wi.nodeID)
	}
}

// TestWorkflowInteraction_WaitUserInputs_队列有输入 测试恢复场景直接返回
func TestWorkflowInteraction_WaitUserInputs_队列有输入(t *testing.T) {
	session := newFakeBaseSession()
	// 预设输入到 session state（全局状态并提交）
	if cs, ok := session.State().(*state.WorkflowCommitState); ok {
		cs.UpdateGlobal(map[string]any{InteractiveInputKey: []any{"user_answer"}})
		cs.Commit()
	}

	wi := NewWorkflowInteraction(session)
	result, err := wi.WaitUserInputs(context.Background(), "question")
	if err != nil {
		t.Fatalf("不应返回错误：%v", err)
	}
	if result != "user_answer" {
		t.Errorf("期望 'user_answer'，实际=%v", result)
	}
}

// TestWorkflowInteraction_WaitUserInputs_队列空时触发GraphInterrupt 测试中断场景
func TestWorkflowInteraction_WaitUserInputs_队列空时触发GraphInterrupt(t *testing.T) {
	session := newFakeBaseSession()
	wi := NewWorkflowInteraction(session)

	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("期望 panic GraphInterrupt，但未发生")
		}
		gi, ok := r.(*GraphInterrupt)
		if !ok {
			t.Fatalf("期望 *GraphInterrupt，得到 %T", r)
		}
		if len(gi.Interrupts) != 1 {
			t.Fatalf("Interrupts 长度应为 1，实际=%d", len(gi.Interrupts))
		}
	}()

	wi.WaitUserInputs(context.Background(), "question")
}

// TestWorkflowInteraction_UserLatestInput_有缓存 测试缓存命中直接返回
func TestWorkflowInteraction_UserLatestInput_有缓存(t *testing.T) {
	session := newFakeBaseSession()
	if cs, ok := session.State().(*state.WorkflowCommitState); ok {
		cs.UpdateGlobal(map[string]any{InteractiveInputKey: []any{"latest_input"}})
		cs.Commit()
	}

	wi := NewWorkflowInteraction(session)
	result, err := wi.UserLatestInput(context.Background(), "value")
	if err != nil {
		t.Fatalf("不应返回错误：%v", err)
	}
	if result != "latest_input" {
		t.Errorf("期望 'latest_input'，实际=%v", result)
	}

	// 第二次调用应触发 GraphInterrupt（缓存已清空）
	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("缓存清空后应触发 GraphInterrupt")
		}
		if _, ok := r.(*GraphInterrupt); !ok {
			t.Fatalf("期望 *GraphInterrupt，得到 %T", r)
		}
	}()
	wi.UserLatestInput(context.Background(), "value2")
}

// TestWorkflowInteraction_UserLatestInput_无缓存触发GraphInterrupt 测试无缓存中断
func TestWorkflowInteraction_UserLatestInput_无缓存触发GraphInterrupt(t *testing.T) {
	session := newFakeBaseSession()
	wi := NewWorkflowInteraction(session)

	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("期望 panic GraphInterrupt，但未发生")
		}
		gi, ok := r.(*GraphInterrupt)
		if !ok {
			t.Fatalf("期望 *GraphInterrupt，得到 %T", r)
		}
		if len(gi.Interrupts) != 1 {
			t.Fatalf("Interrupts 长度应为 1，实际=%d", len(gi.Interrupts))
		}
		if !gi.Interrupts[0].Resumable {
			t.Error("UserLatestInput 的 GraphInterrupt 应为 Resumable")
		}
		if gi.Interrupts[0].NS != "test.exec.id" {
			t.Errorf("NS 期望 'test.exec.id'，实际=%s", gi.Interrupts[0].NS)
		}
	}()

	wi.UserLatestInput(context.Background(), "value")
}

// TestWorkflowInteraction_有StreamWriter 测试 StreamWriterManager 存在时写入交互输出
func TestWorkflowInteraction_有StreamWriter(t *testing.T) {
	session := newFakeBaseSession()
	writer := &fakeOutputWriter{}
	session.swMgrValue = &fakeOutputWriterProvider{writer: writer}

	wi := NewWorkflowInteraction(session)

	defer func() {
		recover()
		if !writer.written {
			t.Error("StreamWriterManager 存在时应写入交互输出")
		}
	}()

	wi.WaitUserInputs(context.Background(), "question")
}

// ──────────────────────────── SimpleAgentInteraction 测试 ────────────────────────────

// TestNewSimpleAgentInteraction 测试构造函数
func TestNewSimpleAgentInteraction(t *testing.T) {
	session := newFakeBaseSession()
	sai := NewSimpleAgentInteraction(session)

	if sai == nil {
		t.Fatal("NewSimpleAgentInteraction 返回 nil")
	}
}

// TestSimpleAgentInteraction_WaitUserInputs_触发AgentInterrupt 测试中断场景
func TestSimpleAgentInteraction_WaitUserInputs_触发AgentInterrupt(t *testing.T) {
	session := newFakeBaseSession()
	session.cpValue = &fakeCheckpointer{}
	sai := NewSimpleAgentInteraction(session)

	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("期望 panic AgentInterrupt，但未发生")
		}
		ai, ok := r.(*AgentInterrupt)
		if !ok {
			t.Fatalf("期望 *AgentInterrupt，得到 %T", r)
		}
		if ai.Message != "test_msg" {
			t.Errorf("Message 期望 'test_msg'，实际=%s", ai.Message)
		}
	}()

	sai.WaitUserInputs(context.Background(), "test_msg")
}

// TestSimpleAgentInteraction_WaitUserInputs_有Checkpointer 测试 checkpointer 被调用
func TestSimpleAgentInteraction_WaitUserInputs_有Checkpointer(t *testing.T) {
	session := newFakeBaseSession()
	cp := &fakeCheckpointer{}
	session.cpValue = cp
	sai := NewSimpleAgentInteraction(session)

	defer func() {
		recover()
		if !cp.interrupted {
			t.Error("checkpointer.InterruptAgentExecute 应被调用")
		}
	}()

	sai.WaitUserInputs(context.Background(), "msg")
}

// ──────────────────────────── AgentInteraction 测试 ────────────────────────────

// TestNewAgentInteraction 测试构造函数
func TestNewAgentInteraction(t *testing.T) {
	session := newFakeBaseSession()
	ai := NewAgentInteraction(session)

	if ai == nil {
		t.Fatal("NewAgentInteraction 返回 nil")
	}
}

// TestAgentInteraction_WaitUserInputs_队列有输入 测试恢复场景直接返回
func TestAgentInteraction_WaitUserInputs_队列有输入(t *testing.T) {
	session := newFakeBaseSession()
	if cs, ok := session.State().(*state.WorkflowCommitState); ok {
		cs.UpdateGlobal(map[string]any{InteractiveInputKey: []any{"agent_answer"}})
		cs.Commit()
	}

	ai := NewAgentInteraction(session)
	result, err := ai.WaitUserInputs(context.Background(), "value")
	if err != nil {
		t.Fatalf("不应返回错误：%v", err)
	}
	if result != "agent_answer" {
		t.Errorf("期望 'agent_answer'，实际=%v", result)
	}
}

// TestAgentInteraction_WaitUserInputs_队列空时触发AgentInterrupt 测试中断场景
func TestAgentInteraction_WaitUserInputs_队列空时触发AgentInterrupt(t *testing.T) {
	session := newFakeBaseSession()
	cp := &fakeCheckpointer{}
	session.cpValue = cp
	ai := NewAgentInteraction(session)

	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("期望 panic AgentInterrupt，但未发生")
		}
		ai2, ok := r.(*AgentInterrupt)
		if !ok {
			t.Fatalf("期望 *AgentInterrupt，得到 %T", r)
		}
		if ai2.Message != "" {
			t.Errorf("AgentInteraction 的 AgentInterrupt.Message 应为空，实际=%s", ai2.Message)
		}
		if !cp.interrupted {
			t.Error("checkpointer.InterruptAgentExecute 应被调用")
		}
	}()

	ai.WaitUserInputs(context.Background(), "value")
}

// TestAgentInteraction_WaitUserInputs_有StreamWriter 测试流输出写入
func TestAgentInteraction_WaitUserInputs_有StreamWriter(t *testing.T) {
	session := newFakeBaseSession()
	writer := &fakeOutputWriter{}
	session.swMgrValue = &fakeOutputWriterProvider{writer: writer}
	cp := &fakeCheckpointer{}
	session.cpValue = cp
	ai := NewAgentInteraction(session)

	defer func() {
		recover()
		if !writer.written {
			t.Error("StreamWriterManager 存在时应写入交互输出")
		}
	}()

	ai.WaitUserInputs(context.Background(), "value")
}

// ──────────────────────────── 依赖接口类型断言测试 ────────────────────────────

// TestInterruptAgentExecute_checkpointer为nil 测试 checkpointer 为 nil 不 panic
func TestInterruptAgentExecute_checkpointer为nil(t *testing.T) {
	session := newFakeBaseSession()
	err := interruptAgentExecute(session)
	if err != nil {
		t.Errorf("checkpointer 为 nil 时应返回 nil，实际=%v", err)
	}
}

// TestInterruptAgentExecute_类型不满足接口 测试 checkpointer 不满足接口时返回 nil
func TestInterruptAgentExecute_类型不满足接口(t *testing.T) {
	session := newFakeBaseSession()
	session.cpValue = "not_a_checkpointer"
	err := interruptAgentExecute(session)
	if err != nil {
		t.Errorf("类型不满足接口时应返回 nil，实际=%v", err)
	}
}

// TestWriteInteractionOutput_manager为nil 测试 StreamWriterManager 为 nil 不 panic
func TestWriteInteractionOutput_manager为nil(t *testing.T) {
	session := newFakeBaseSession()
	err := writeInteractionOutput(session, InteractionType, 0, "payload")
	if err != nil {
		t.Errorf("StreamWriterManager 为 nil 时应返回 nil，实际=%v", err)
	}
}

// TestWriteInteractionOutput_类型不满足接口 测试 manager 不满足接口时返回 nil
func TestWriteInteractionOutput_类型不满足接口(t *testing.T) {
	session := newFakeBaseSession()
	session.swMgrValue = "not_a_provider"
	err := writeInteractionOutput(session, InteractionType, 0, "payload")
	if err != nil {
		t.Errorf("类型不满足接口时应返回 nil，实际=%v", err)
	}
}

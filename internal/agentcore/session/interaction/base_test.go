package interaction

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/state"
)

// ──────────────────────────── fake 实现 ────────────────────────────

// fakeBaseSession 用于测试的最小 baseSession 实现
type fakeBaseSession struct {
	stateValue  state.SessionState
	swMgrValue  any
	cpValue     any
	execIDValue string
}

func (f *fakeBaseSession) State() state.SessionState { return f.stateValue }
func (f *fakeBaseSession) StreamWriterManager() any  { return f.swMgrValue }
func (f *fakeBaseSession) Checkpointer() any         { return f.cpValue }
func (f *fakeBaseSession) ExecutableID() string      { return f.execIDValue }

// newFakeBaseSession 创建测试用 fake session
func newFakeBaseSession() *fakeBaseSession {
	return &fakeBaseSession{
		stateValue: state.NewInMemoryWorkflowState(),
	}
}

// ──────────────────────────── NewBaseInteraction 测试 ────────────────────────────

// TestNewBaseInteraction_无默认输入 测试无 defaultInput 时队列为空
func TestNewBaseInteraction_无默认输入(t *testing.T) {
	session := newFakeBaseSession()
	bi := NewBaseInteraction(session)

	if bi == nil {
		t.Fatal("NewBaseInteraction 返回 nil")
	}
	if len(bi.interactiveInputs) != 0 {
		t.Errorf("无 defaultInput 时 interactiveInputs 应为空，实际长度=%d", len(bi.interactiveInputs))
	}
	if bi.idx != 0 {
		t.Errorf("idx 应为 0，实际=%d", bi.idx)
	}
}

// TestNewBaseInteraction_有默认输入 测试有 defaultInput 时队列包含默认值
func TestNewBaseInteraction_有默认输入(t *testing.T) {
	session := newFakeBaseSession()
	bi := NewBaseInteraction(session, "default_input")

	if len(bi.interactiveInputs) != 1 {
		t.Fatalf("interactiveInputs 长度应为 1，实际=%d", len(bi.interactiveInputs))
	}
	if bi.interactiveInputs[0] != "default_input" {
		t.Errorf("interactiveInputs[0] 期望 'default_input'，实际=%v", bi.interactiveInputs[0])
	}
	if bi.latestInteractiveInput != "default_input" {
		t.Errorf("latestInteractiveInput 期望 'default_input'，实际=%v", bi.latestInteractiveInput)
	}
}

// TestNewBaseInteraction_从SessionState读取输入 测试从 session state 合并已有输入
// 对齐 Python：state().get() 读取 agent_state/comp_state（非 global_state）
func TestNewBaseInteraction_从SessionState读取输入(t *testing.T) {
	session := newFakeBaseSession()
	// 预设 session state 中的输入（写入组件级状态并提交）
	if cs, ok := session.State().(*state.WorkflowCommitState); ok {
		require.NoError(t, cs.Update(map[string]any{InteractiveInputKey: []any{"existing_input"}}))
		cs.Commit()
	}

	bi := NewBaseInteraction(session, "default_input")

	// existing_input 在前，default_input 在后
	if len(bi.interactiveInputs) != 2 {
		t.Fatalf("interactiveInputs 长度应为 2，实际=%d", len(bi.interactiveInputs))
	}
	if bi.interactiveInputs[0] != "existing_input" {
		t.Errorf("interactiveInputs[0] 期望 'existing_input'，实际=%v", bi.interactiveInputs[0])
	}
	if bi.interactiveInputs[1] != "default_input" {
		t.Errorf("interactiveInputs[1] 期望 'default_input'，实际=%v", bi.interactiveInputs[1])
	}
}

// ──────────────────────────── getNextInteractiveInput 测试 ────────────────────────────

// TestGetNextInteractiveInput_顺序消费 测试输入队列顺序消费
func TestGetNextInteractiveInput_顺序消费(t *testing.T) {
	session := newFakeBaseSession()
	bi := NewBaseInteraction(session, "input1")

	// 第一次消费
	result := bi.getNextInteractiveInput()
	if result != "input1" {
		t.Errorf("第一次消费期望 'input1'，实际=%v", result)
	}
	if bi.idx != 1 {
		t.Errorf("消费后 idx 应为 1，实际=%d", bi.idx)
	}

	// 队列耗尽
	result2 := bi.getNextInteractiveInput()
	if result2 != nil {
		t.Errorf("队列耗尽后应返回 nil，实际=%v", result2)
	}
}

// TestGetNextInteractiveInput_队列为空 测试空队列返回 nil
func TestGetNextInteractiveInput_队列为空(t *testing.T) {
	session := newFakeBaseSession()
	bi := NewBaseInteraction(session)

	result := bi.getNextInteractiveInput()
	if result != nil {
		t.Errorf("空队列应返回 nil，实际=%v", result)
	}
}

// ──────────────────────────── GraphInterrupt 测试 ────────────────────────────

// TestPanicGraphInterrupt 测试 GraphInterrupt panic
func TestPanicGraphInterrupt(t *testing.T) {
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
		if gi.Interrupts[0].Value != "test_value" {
			t.Errorf("Interrupt.Value 期望 'test_value'，实际=%v", gi.Interrupts[0].Value)
		}
	}()
	PanicGraphInterrupt(Interrupt{Value: "test_value"})
}

// TestPanicAgentInterrupt 测试 AgentInterrupt panic
func TestPanicAgentInterrupt(t *testing.T) {
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
	PanicAgentInterrupt("test_msg")
}

// ──────────────────────────── commitCMP 测试 ────────────────────────────

// TestCommitCMP_WorkflowCommitState 测试 WorkflowCommitState 提交
func TestCommitCMP_WorkflowCommitState(t *testing.T) {
	cs := state.NewInMemoryWorkflowState()
	session := &fakeBaseSession{stateValue: cs}

	// 不应 panic
	commitCMP(session)
}

// TestCommitCMP_非WorkflowCommitState 测试非 WorkflowCommitState 会 panic（对齐 Python AttributeError）
func TestCommitCMP_非WorkflowCommitState(t *testing.T) {
	session := &fakeBaseSession{stateValue: state.NewInMemoryState()}

	defer func() {
		if r := recover(); r == nil {
			t.Error("期望 panic（对齐 Python AttributeError），实际未 panic")
		}
	}()
	commitCMP(session)
}

// ──────────────────────────── getExecutableID 测试 ────────────────────────────

// fakeSessionWithoutExecID 不实现 ExecutableIDProvider 的 session
type fakeSessionWithoutExecID struct {
	stateValue state.SessionState
	swMgrValue any
	cpValue    any
}

func (f *fakeSessionWithoutExecID) State() state.SessionState { return f.stateValue }
func (f *fakeSessionWithoutExecID) StreamWriterManager() any  { return f.swMgrValue }
func (f *fakeSessionWithoutExecID) Checkpointer() any         { return f.cpValue }

// TestGetExecutableID_不满足接口 测试 session 不满足 ExecutableIDProvider 时返回空字符串
func TestGetExecutableID_不满足接口(t *testing.T) {
	session := &fakeSessionWithoutExecID{stateValue: state.NewInMemoryWorkflowState()}
	id := getExecutableID(session)
	if id != "" {
		t.Errorf("不满足 ExecutableIDProvider 时应返回空字符串，实际=%s", id)
	}
}

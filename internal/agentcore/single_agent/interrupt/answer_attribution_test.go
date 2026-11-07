package interrupt

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/interaction"
	sessionstate "github.com/uapclaw/uapclaw-go/internal/agentcore/session/state"
	saschema "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/schema"
)

// ──────────────────────────── 测试辅助 ────────────────────────────

// attributionState 构造一个含指定中断的状态。
// entries: call_id → (owner, interrupt_id, kind)
func attributionState(entries map[string]struct {
	Owner       string
	InterruptID string
	Kind        string
}) *saschema.ToolInterruptionState {
	tools := make(map[string]*saschema.ToolInterruptEntry, len(entries))
	for callID, spec := range entries {
		tools[callID] = &saschema.ToolInterruptEntry{
			ToolCall: &llmschema.ToolCall{ID: callID},
			// 键是 inner call_id（对齐 Python state.py L66 与 Go handler.go:474）
			InterruptRequests: map[string]saschema.InterruptRequester{
				callID: &saschema.InterruptRequest{
					InterruptID:    spec.InterruptID,
					InterruptOwner: spec.Owner,
					InterruptKind:  spec.Kind,
				},
			},
		}
	}
	return &saschema.ToolInterruptionState{
		InterruptedTools:   tools,
		AutoConfirmMapping: map[string]string{},
	}
}

// attrSpec 简化 attributionState 的入参书写
type attrSpec = struct {
	Owner       string
	InterruptID string
	Kind        string
}

// ──────────────────────────── PendingInterrupts 测试 ────────────────────────────

// TestPendingInterrupts_正常 验证列出全部待决中断
func TestPendingInterrupts_正常(t *testing.T) {
	state := attributionState(map[string]attrSpec{
		"call_1": {"rail.A", "intr_1", saschema.InterruptKindPermission},
		"call_2": {"rail.B", "intr_2", saschema.InterruptKindConfirm},
	})

	pending := PendingInterrupts(state)
	require.Len(t, pending, 2)

	byCall := make(map[string]PendingInterrupt, len(pending))
	for _, item := range pending {
		byCall[item.ToolCallID] = item
	}
	assert.Equal(t, "intr_1", byCall["call_1"].InterruptID)
	assert.Equal(t, "call_1", byCall["call_1"].ToolCallID)
	assert.Equal(t, saschema.InterruptKindPermission, byCall["call_1"].Kind)
}

// TestPendingInterrupts_nil 验证 nil 状态返回空
func TestPendingInterrupts_nil(t *testing.T) {
	assert.Empty(t, PendingInterrupts(nil))
}

// TestPendingInterrupts_空条目 验证跳过 nil 条目
func TestPendingInterrupts_空条目(t *testing.T) {
	state := &saschema.ToolInterruptionState{
		InterruptedTools: map[string]*saschema.ToolInterruptEntry{"call_1": nil},
	}
	assert.Empty(t, PendingInterrupts(state))
}

// ──────────────────────────── MatchAnswers 测试 ────────────────────────────

// TestMatchAnswers_按interruptID 验证 interrupt_id 优先于 tool_call_id
func TestMatchAnswers_按interruptID(t *testing.T) {
	pending := []PendingInterrupt{
		{InterruptID: "intr_1", ToolCallID: "call_1"},
	}
	input := &interaction.InteractiveInput{
		UserInputs:       map[string]any{"call_1": "by-call"},
		InterruptAnswers: map[string]any{"intr_1": "by-interrupt"},
	}

	matched := MatchAnswers(input, pending)
	// interrupt_id 优先
	assert.Equal(t, "by-interrupt", matched.Answers["intr_1"])
	assert.Empty(t, matched.Unmatched)
}

// TestMatchAnswers_按toolCallID 验证只有 call id 时按 call 取值
func TestMatchAnswers_按toolCallID(t *testing.T) {
	pending := []PendingInterrupt{
		{InterruptID: "intr_1", ToolCallID: "call_1"},
	}
	input := &interaction.InteractiveInput{
		UserInputs:       map[string]any{"call_1": "by-call"},
		InterruptAnswers: map[string]any{},
	}

	matched := MatchAnswers(input, pending)
	assert.Equal(t, "by-call", matched.Answers["intr_1"])
}

// TestMatchAnswers_unmatched 验证对不上的键被记入 unmatched
func TestMatchAnswers_unmatched(t *testing.T) {
	pending := []PendingInterrupt{
		{InterruptID: "intr_1", ToolCallID: "call_1"},
	}
	input := &interaction.InteractiveInput{
		UserInputs:       map[string]any{"stale_call": "x"},
		InterruptAnswers: map[string]any{"stale_intr": "y"},
	}

	matched := MatchAnswers(input, pending)
	assert.Empty(t, matched.Answers)
	assert.Len(t, matched.Unmatched, 2)
	assert.Contains(t, matched.Unmatched, "stale_call")
	assert.Contains(t, matched.Unmatched, "stale_intr")
}

// TestMatchAnswers_nil 验证 nil 输入返回空答案
func TestMatchAnswers_nil(t *testing.T) {
	matched := MatchAnswers(nil, []PendingInterrupt{{InterruptID: "intr_1"}})
	assert.Empty(t, matched.Answers)
}

// ──────────────────────────── attributeAnswers 测试 ────────────────────────────

// TestAttributeAnswers_InteractiveInput 验证按 InteractiveInput 归属并带 owner/kind
func TestAttributeAnswers_InteractiveInput(t *testing.T) {
	state := attributionState(map[string]attrSpec{
		"call_1": {"rail.A", "intr_1", saschema.InterruptKindPermission},
	})
	input := &interaction.InteractiveInput{
		UserInputs:       map[string]any{"call_1": "answer"},
		InterruptAnswers: map[string]any{},
	}

	answers := attributeAnswers(state, input)
	require.Contains(t, answers, "intr_1")

	answer := answers["intr_1"]
	assert.Equal(t, "rail.A", answer.Owner)
	assert.Equal(t, saschema.InterruptKindPermission, answer.Kind)
	assert.Equal(t, "answer", answer.Value)
	assert.False(t, answer.Unaddressed, "InteractiveInput 不是纯文本")
}

// TestAttributeAnswers_键含callID的dict 验证 dict 只答键里点名的调用
func TestAttributeAnswers_键含callID的dict(t *testing.T) {
	state := attributionState(map[string]attrSpec{
		"call_1": {"rail.A", "intr_1", saschema.InterruptKindPermission},
		"call_2": {"rail.B", "intr_2", saschema.InterruptKindPermission},
	})

	answers := attributeAnswers(state, map[string]any{"call_1": "only-call-1"})
	require.Len(t, answers, 1)
	assert.Equal(t, "only-call-1", answers["intr_1"].Value)
	assert.NotContains(t, answers, "intr_2")
}

// TestAttributeAnswers_纯文本 验证纯文本提供给每个中断并标记 Unaddressed
func TestAttributeAnswers_纯文本(t *testing.T) {
	state := attributionState(map[string]attrSpec{
		"call_1": {"rail.A", "intr_1", saschema.InterruptKindPermission},
		"call_2": {"rail.B", "intr_2", saschema.InterruptKindConfirm},
	})

	answers := attributeAnswers(state, "plain text")
	require.Len(t, answers, 2, "纯文本应提供给每个待决中断")
	for _, answer := range answers {
		assert.Equal(t, "plain text", answer.Value)
		assert.True(t, answer.Unaddressed, "纯文本应标记 unaddressed")
	}
}

// TestAttributeAnswers_其它dict 验证无 call id 的 dict 同样退化
func TestAttributeAnswers_其它dict(t *testing.T) {
	state := attributionState(map[string]attrSpec{
		"call_1": {"rail.A", "intr_1", saschema.InterruptKindPermission},
	})

	answers := attributeAnswers(state, map[string]any{"unrelated": 1})
	require.Len(t, answers, 1)
	assert.False(t, answers["intr_1"].Unaddressed, "dict 不是纯文本")
}

// TestAttributeAnswers_nil 验证 nil 输入返回空
func TestAttributeAnswers_nil(t *testing.T) {
	state := attributionState(map[string]attrSpec{
		"call_1": {"rail.A", "intr_1", saschema.InterruptKindPermission},
	})
	assert.Empty(t, attributeAnswers(state, nil))
}

// ──────────────────────────── callAnswers 测试 ────────────────────────────

// TestCallAnswers_本轮答案 验证答案按 call_id → owner 组织
func TestCallAnswers_本轮答案(t *testing.T) {
	state := attributionState(map[string]attrSpec{
		"call_1": {"rail.A", "intr_1", saschema.InterruptKindPermission},
	})

	fresh := map[string]*saschema.InterruptAnswer{
		"intr_1": {Owner: "rail.A", Value: "v"},
	}
	result := callAnswers(state, fresh)

	require.Contains(t, result, "call_1")
	assert.Equal(t, "v", result["call_1"]["rail.A"].Value)
}

// TestCallAnswers_上轮铺底本轮覆盖 验证 carried 先铺底、fresh 覆盖同名 owner
func TestCallAnswers_上轮铺底本轮覆盖(t *testing.T) {
	state := attributionState(map[string]attrSpec{
		"call_1": {"rail.A", "intr_1", saschema.InterruptKindPermission},
	})
	state.InterruptedTools["call_1"].CarriedAnswers = map[string]map[string]*saschema.InterruptAnswer{
		"call_1": {
			"rail.A": {Owner: "rail.A", Value: "carried", Carried: true},
			"rail.C": {Owner: "rail.C", Value: "other", Carried: true},
		},
	}

	fresh := map[string]*saschema.InterruptAnswer{
		"intr_1": {Owner: "rail.A", Value: "fresh"},
	}
	result := callAnswers(state, fresh)

	assert.Equal(t, "fresh", result["call_1"]["rail.A"].Value, "本轮应覆盖上轮")
	assert.Equal(t, "other", result["call_1"]["rail.C"].Value, "上轮其它 owner 应保留")
}

// TestCallAnswers_子Agent只暴露outer 验证子 Agent 条目不合并本轮答案
func TestCallAnswers_子Agent只暴露outer(t *testing.T) {
	state := attributionState(map[string]attrSpec{
		"sub_1": {"rail.A", "intr_1", saschema.InterruptKindPermission},
	})
	state.InterruptedTools["sub_1"].IsSubAgent = true

	fresh := map[string]*saschema.InterruptAnswer{
		"intr_1": {Owner: "rail.A", Value: "fresh"},
	}
	result := callAnswers(state, fresh)

	// 子 Agent 不合并本轮答案
	assert.NotContains(t, result["sub_1"], "rail.A")
}

// ──────────────────────────── unansweredInterrupts 测试 ────────────────────────────

// TestUnansweredInterrupts_未答 验证未答的中断被记录
func TestUnansweredInterrupts_未答(t *testing.T) {
	state := attributionState(map[string]attrSpec{
		"call_1": {"rail.A", "intr_1", saschema.InterruptKindPermission},
		"call_2": {"rail.B", "intr_2", saschema.InterruptKindPermission},
	})

	// 只答了 intr_1
	fresh := map[string]*saschema.InterruptAnswer{"intr_1": {Owner: "rail.A"}}
	result := unansweredInterrupts(state, fresh)

	require.Len(t, result, 1)
	assert.Equal(t, "rail.B", result["call_2"].Owner)
	assert.Equal(t, "intr_2", result["call_2"].InterruptID)
}

// TestUnansweredInterrupts_跳过子Agent 验证子 Agent 条目不参与
func TestUnansweredInterrupts_跳过子Agent(t *testing.T) {
	state := attributionState(map[string]attrSpec{
		"sub_1": {"rail.A", "intr_1", saschema.InterruptKindPermission},
	})
	state.InterruptedTools["sub_1"].IsSubAgent = true

	result := unansweredInterrupts(state, map[string]*saschema.InterruptAnswer{})
	assert.Empty(t, result)
}

// ──────────────────────────── carryAnswers 测试 ────────────────────────────

// TestCarryAnswers_保留其它owner 验证非提问 owner 的答案被标记 carried 保留
func TestCarryAnswers_保留其它owner(t *testing.T) {
	entry := &saschema.ToolInterruptEntry{
		ToolCall: &llmschema.ToolCall{ID: "call_1"},
		InterruptRequests: map[string]saschema.InterruptRequester{
			"call_1": &saschema.InterruptRequest{InterruptID: "intr_2", InterruptOwner: "rail.B"},
		},
	}
	tools := map[string]*saschema.ToolInterruptEntry{"call_1": entry}

	answers := CallAnswers{
		"call_1": {
			"rail.A": {Owner: "rail.A", Value: "kept"},
			"rail.B": {Owner: "rail.B", Value: "dropped"},
		},
	}
	carryAnswers(tools, answers)

	require.Contains(t, entry.CarriedAnswers, "call_1")
	assert.Contains(t, entry.CarriedAnswers["call_1"], "rail.A", "非提问 owner 应保留")
	assert.True(t, entry.CarriedAnswers["call_1"]["rail.A"].Carried, "应标记 carried")
	assert.NotContains(t, entry.CarriedAnswers["call_1"], "rail.B", "再次提问的 owner 答案应丢弃")
}

// TestCarryAnswers_无保留时不写 验证全部丢弃时不写 CarriedAnswers
func TestCarryAnswers_无保留时不写(t *testing.T) {
	entry := &saschema.ToolInterruptEntry{
		ToolCall: &llmschema.ToolCall{ID: "call_1"},
		InterruptRequests: map[string]saschema.InterruptRequester{
			"call_1": &saschema.InterruptRequest{InterruptID: "intr_1", InterruptOwner: "rail.A"},
		},
	}
	tools := map[string]*saschema.ToolInterruptEntry{"call_1": entry}

	answers := CallAnswers{"call_1": {"rail.A": {Owner: "rail.A"}}}
	carryAnswers(tools, answers)

	assert.Nil(t, entry.CarriedAnswers)
}

// TestCarryAnswers_nil条目 验证跳过 nil 条目
func TestCarryAnswers_nil条目(t *testing.T) {
	tools := map[string]*saschema.ToolInterruptEntry{"call_1": nil}
	carryAnswers(tools, CallAnswers{})
}

// ──────────────────────────── IssueInterruptID 测试 ────────────────────────────

// TestIssueInterruptID_沿用同owner 验证同 owner 的未答中断沿用原 id
func TestIssueInterruptID_沿用同owner(t *testing.T) {
	request := &saschema.InterruptRequest{InterruptOwner: "rail.A"}
	unanswered := map[string]saschema.AnswerOwnerAndID{
		"call_1": {Owner: "rail.A", InterruptID: "intr_old"},
	}

	saschema.IssueInterruptID(request, "call_1", unanswered)
	assert.Equal(t, "intr_old", request.InterruptID)
}

// TestIssueInterruptID_不同owner重发号 验证 owner 不同时分配新 id
func TestIssueInterruptID_不同owner重发号(t *testing.T) {
	request := &saschema.InterruptRequest{InterruptOwner: "rail.B"}
	unanswered := map[string]saschema.AnswerOwnerAndID{
		"call_1": {Owner: "rail.A", InterruptID: "intr_old"},
	}

	saschema.IssueInterruptID(request, "call_1", unanswered)
	assert.NotEqual(t, "intr_old", request.InterruptID)
	assert.NotEmpty(t, request.InterruptID)
}

// TestIssueInterruptID_无未答 验证没有未答记录时分配新 id
func TestIssueInterruptID_无未答(t *testing.T) {
	request := &saschema.InterruptRequest{InterruptOwner: "rail.A"}
	saschema.IssueInterruptID(request, "call_1", nil)
	assert.NotEmpty(t, request.InterruptID)
	assert.Contains(t, request.InterruptID, "intr_")
}

// TestIssueInterruptID_nil 验证 nil 请求不 panic
func TestIssueInterruptID_nil(t *testing.T) {
	saschema.IssueInterruptID(nil, "call_1", nil)
}

// TestNewInterruptID_唯一性 验证生成的 id 唯一且带前缀
func TestNewInterruptID_唯一性(t *testing.T) {
	seen := make(map[string]struct{}, 100)
	for i := 0; i < 100; i++ {
		id := saschema.NewInterruptID()
		assert.Contains(t, id, "intr_")
		_, dup := seen[id]
		assert.False(t, dup, "id 重复: %s", id)
		seen[id] = struct{}{}
	}
}

// ──────────────────────────── 边界分支补充 ────────────────────────────

// TestRequestHelpers_非InterruptRequest 验证中间层包装类型走入 nil 分支
func TestRequestHelpers_非InterruptRequest(t *testing.T) {
	wrapped := &saschema.ToolCallInterruptRequest{
		Request: &saschema.InterruptRequest{
			InterruptID:    "intr_1",
			InterruptOwner: "rail.A",
			InterruptKind:  saschema.InterruptKindPermission,
		},
	}

	// 包装类型不是 *InterruptRequest，id 与 as-request 走短路分支
	assert.Empty(t, requestIDOf(wrapped))
	assert.Nil(t, requestAsInterruptRequest(wrapped))
	// owner 与 kind 走 GetInterruptOwner 委托，能取到
	assert.Equal(t, "rail.A", requestOwnerOf(wrapped))
	assert.Equal(t, saschema.InterruptKindPermission, requestKindOf(wrapped))

	// nil 请求
	assert.Empty(t, requestIDOf(nil))
	assert.Empty(t, requestOwnerOf(nil))
	assert.Equal(t, saschema.InterruptKindUndeclared, requestKindOf(nil))
}

// TestSaveAutoConfirm_nil答案与缺key 验证 nil 答案跳过、无 auto_confirm_key 时不通开
func TestSaveAutoConfirm_nil答案与缺key(t *testing.T) {
	sess := session.NewSession()
	answers := map[string]*saschema.InterruptAnswer{
		"intr_nil": nil,
		"intr_no_key": {
			Kind:    saschema.InterruptKindPermission,
			Value:   map[string]any{"auto_confirm": true},
			Request: &saschema.InterruptRequest{},
		},
		"intr_no_request": {
			Kind:  saschema.InterruptKindPermission,
			Value: map[string]any{"auto_confirm": true},
		},
	}

	saveAutoConfirm(answers, sess)

	configVal, _ := sess.GetState(sessionstate.StringKey(InterruptAutoConfirmKey))
	config, _ := configVal.(map[string]any)
	assert.Empty(t, config, "无 auto_confirm_key 不应写入")
}

// TestSaveAutoConfirm_保留已有配置 验证已有 auto_confirm 配置被保留
func TestSaveAutoConfirm_保留已有配置(t *testing.T) {
	sess := session.NewSession()
	sess.UpdateState(map[string]any{InterruptAutoConfirmKey: map[string]any{"existing": true}})

	answers := map[string]*saschema.InterruptAnswer{
		"intr_1": {
			Kind:    saschema.InterruptKindConfirm,
			Value:   map[string]any{"auto_confirm": true},
			Request: &saschema.InterruptRequest{AutoConfirmKey: "new_key"},
		},
	}
	saveAutoConfirm(answers, sess)

	configVal, _ := sess.GetState(sessionstate.StringKey(InterruptAutoConfirmKey))
	config, _ := configVal.(map[string]any)
	assert.Equal(t, true, config["existing"], "已有配置应保留")
	assert.Equal(t, true, config["new_key"])
}

// TestCallAnswers_entry为nil 验证 nil 条目被跳过
func TestCallAnswers_entry为nil(t *testing.T) {
	state := &saschema.ToolInterruptionState{
		InterruptedTools: map[string]*saschema.ToolInterruptEntry{"call_1": nil},
	}
	assert.Empty(t, callAnswers(state, map[string]*saschema.InterruptAnswer{}))
}

// TestAttributeAnswers_待决为空 验证无待决中断时纯文本返回空
func TestAttributeAnswers_待决为空(t *testing.T) {
	state := &saschema.ToolInterruptionState{
		InterruptedTools: map[string]*saschema.ToolInterruptEntry{},
	}
	assert.Empty(t, attributeAnswers(state, "plain"))
	assert.Empty(t, attributeAnswers(state, map[string]any{"a": 1}))
}

// TestCarryAnswers_子Agent条目 验证子 Agent 条目只处理 outer id
func TestCarryAnswers_子Agent条目(t *testing.T) {
	entry := &saschema.ToolInterruptEntry{
		ToolCall:   &llmschema.ToolCall{ID: "sub_1"},
		IsSubAgent: true,
	}
	tools := map[string]*saschema.ToolInterruptEntry{"sub_1": entry}

	answers := CallAnswers{"sub_1": {"rail.A": {Owner: "rail.A", Value: "kept"}}}
	carryAnswers(tools, answers)

	assert.Contains(t, entry.CarriedAnswers["sub_1"], "rail.A")
}

// TestSaveAutoConfirm_未声明kind也应写入 回归：AskUserRail 不声明 interrupt_kind，
// 答案的 Kind 为空串（InterruptKindUndeclared）。Python 的 APPROVED_ANSWER_KINDS
// 明确把 UNDECLARED 列入白名单（answers.py:47-52），Go 曾遗漏该取值，导致这类
// rail 的 auto_confirm 永远写不进 session（集成套件 ConcurrentMixedInterrupt
// 因此红）。
func TestSaveAutoConfirm_未声明kind也应写入(t *testing.T) {
	sess := session.NewSession()

	answers := map[string]*saschema.InterruptAnswer{
		"intr_undeclared": {
			Kind:    saschema.InterruptKindUndeclared,
			Value:   map[string]any{"approved": true, "auto_confirm": true},
			Request: &saschema.InterruptRequest{AutoConfirmKey: "write_file"},
		},
	}
	saveAutoConfirm(answers, sess)

	configVal, _ := sess.GetState(sessionstate.StringKey(InterruptAutoConfirmKey))
	config, _ := configVal.(map[string]any)
	assert.Equal(t, true, config["write_file"], "未声明 kind 的 auto_confirm 应写入 session")
}

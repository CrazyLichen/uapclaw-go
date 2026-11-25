package agents

// 补齐覆盖率用例（8.26 收口审查拍板 2026-11-25：react_workflow_interrupt.go
// 77.8% < 85% 门禁，针对 payload 双形态辅助/extractComponentIDs 分支/
// loadInterruptionState 往返重建/deepClone nil 守卫/pendingInteractionValue
// 指针形态补测）。

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/session"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/session/interaction"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/session/stream"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/single_agent/schema"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/workflow"
)

// interactionPayloadID/ComponentID 双形态辅助：值/指针/map 三形态 + 未知类型
func TestInteractionPayloadHelpers_双形态(t *testing.T) {
	// interactionPayloadID：值形态
	id, ok := interactionPayloadID(interaction.InteractionOutput{ID: "c1"})
	require.True(t, ok)
	assert.Equal(t, "c1", id)
	// interactionPayloadID：指针形态
	id2, ok2 := interactionPayloadID(&interaction.InteractionOutput{ID: "c2"})
	require.True(t, ok2)
	assert.Equal(t, "c2", id2)
	// interactionPayloadID：map 兜底
	id3, ok3 := interactionPayloadID(map[string]any{"id": "c3"})
	require.True(t, ok3)
	assert.Equal(t, "c3", id3)
	// interactionPayloadID：未知类型 → false
	_, ok4 := interactionPayloadID("plain")
	assert.False(t, ok4)
	// interactionPayloadComponentID：三形态 + 未知类型
	cid, cok := interactionPayloadComponentID(interaction.InteractionOutput{ID: "q1"})
	require.True(t, cok)
	assert.Equal(t, "q1", cid)
	cid2, cok2 := interactionPayloadComponentID(&interaction.InteractionOutput{ID: "q2"})
	require.True(t, cok2)
	assert.Equal(t, "q2", cid2)
	cid3, cok3 := interactionPayloadComponentID(map[string]any{"component_id": "q3"})
	require.True(t, cok3)
	assert.Equal(t, "q3", cid3)
	_, cok4 := interactionPayloadComponentID(42)
	assert.False(t, cok4)
}

// TestExtractComponentIDs_分支补齐：值/指针 OutputSchema 双形态混入非交互块、
// result 非 list、list 版缺键兜底
func TestExtractComponentIDs_分支补齐(t *testing.T) {
	a := &ReActAgent{}
	// WorkflowOutput 分支：指针 InteractionOutput + 非 OutputSchema 项跳过
	mixed := &workflow.WorkflowOutput{Result: []stream.Schema{
		&stream.OutputSchema{Type: "answer"}, // 非交互块跳过
		nil,                                  // nil 项守卫
		&stream.OutputSchema{Type: interaction.InteractionType,
			Payload: &interaction.InteractionOutput{ID: "b"}}, // 指针 payload
		&stream.OutputSchema{Type: interaction.InteractionType,
			Payload: map[string]any{"id": "a"}}, // map payload
		stream.OutputSchema{Type: interaction.InteractionType,
			Payload: interaction.InteractionOutput{ID: "c"}}, // 值形态帧（非指针）
	}}
	ids := a.extractComponentIDs(mixed)
	assert.Equal(t, []string{"a", "b", "c"}, ids) // sorted 稳定序
	// WorkflowOutput.result 非 list → 空列表
	assert.Empty(t, a.extractComponentIDs(&workflow.WorkflowOutput{Result: "str"}))
	// 非 WorkflowOutput 非 list → 空列表
	assert.Empty(t, a.extractComponentIDs(42))
	// list 分支：指针帧 + 值帧混排，component_id 键提取
	list := []stream.Schema{
		&stream.OutputSchema{Type: interaction.InteractionType,
			Payload: map[string]any{"component_id": "x2"}},
		stream.OutputSchema{Type: interaction.InteractionType,
			Payload: map[string]any{"component_id": "x1"}},
		nil,
	}
	assert.Equal(t, []string{"x1", "x2"}, a.extractComponentIDs(list))
	// list 分支：缺 component_id 键 → fmt.Sprint(nil) 兜底仍收集（Python
	// payload.get("component_id","") 等价：键缺失取空）
	listMissing := []stream.Schema{&stream.OutputSchema{Type: interaction.InteractionType,
		Payload: map[string]any{}}}
	assert.Equal(t, []string{"<nil>"}, a.extractComponentIDs(listMissing))
}

// TestLoadInterruptionState_往返重建：checkpoint JSON 往返把指针降级为 map
// → JSON 重建恢复语义（questioner_state Serialize/Deserialize 同款问题）
func TestLoadInterruptionState_往返重建(t *testing.T) {
	a := &ReActAgent{}
	// nil 会话 → nil
	assert.Nil(t, a.loadInterruptionState(nil))
	// 空状态会话 → nil
	assert.Nil(t, a.loadInterruptionState(session.NewSession(session.WithSessionID("empty"))))
	// map 往返形态 → JSON 重建（真实会话：UpdateState 落 map 后 GetState 读回）
	st := &schema.InterruptionState{
		InterruptedWorkflows: map[string]*schema.WorkflowInterruptEntry{
			"wf_a": {ComponentIDs: []string{"q1"}},
		},
		PendingWorkflowID:  "wf_a",
		PendingComponentID: "q1",
	}
	// 先经 JSON 往返把指针降级为 map（模拟 checkpoint 持久化往返）
	data, err := json.Marshal(st)
	require.NoError(t, err)
	var roundtrip map[string]any
	require.NoError(t, json.Unmarshal(data, &roundtrip))
	sess := session.NewSession(session.WithSessionID("rt"))
	sess.UpdateState(map[string]any{schema.InterruptionKey: roundtrip})
	got := a.loadInterruptionState(sess)
	require.NotNil(t, got)
	assert.Equal(t, "wf_a", got.PendingWorkflowID)
	assert.Equal(t, "q1", got.PendingComponentID)
	require.Len(t, got.InterruptedWorkflows, 1)
	assert.Equal(t, []string{"q1"}, got.InterruptedWorkflows["wf_a"].ComponentIDs)
}

// TestSaveClearInterruptionState_往返 save→load 原指针往返 + clear 置空
func TestSaveClearInterruptionState_往返(t *testing.T) {
	a := &ReActAgent{}
	sess := session.NewSession(session.WithSessionID("rt2"))
	st := &schema.InterruptionState{PendingWorkflowID: "wf_a", PendingComponentID: "q1"}
	a.saveInterruptionState(st, sess)
	got := a.loadInterruptionState(sess)
	require.NotNil(t, got)
	assert.Equal(t, "q1", got.PendingComponentID)
	// clear → 置空
	a.clearInterruptionState(sess)
	assert.Nil(t, a.loadInterruptionState(sess))
}

// TestDeepCloneHelpers_nil 守卫
func TestDeepCloneHelpers_nil(t *testing.T) {
	assert.Nil(t, deepCloneAssistantMessage(nil))
	assert.Nil(t, deepCloneToolCall(nil))
}

// TestPendingInteractionValue_指针帧补齐：*InteractionOutput payload 形态 +
// result 非 list 守卫
func TestPendingInteractionValue_指针帧(t *testing.T) {
	out := &workflow.WorkflowOutput{Result: []stream.Schema{&stream.OutputSchema{
		Type: interaction.InteractionType,
		Payload: &interaction.InteractionOutput{ID: "react", Value: "指针值"}}}}
	v, ok := pendingInteractionValue(map[string]any{
		"workflow_execution_state": out, "component_ids": []string{"react"}})
	require.True(t, ok)
	assert.Equal(t, "指针值", v)
	// result 非 list → false
	outStr := &workflow.WorkflowOutput{Result: "str"}
	_, ok2 := pendingInteractionValue(map[string]any{
		"workflow_execution_state": outStr, "component_ids": []string{"react"}})
	assert.False(t, ok2)
	// component_ids 空列表 → false
	_, ok3 := pendingInteractionValue(map[string]any{
		"workflow_execution_state": out, "component_ids": []string{}})
	assert.False(t, ok3)
}

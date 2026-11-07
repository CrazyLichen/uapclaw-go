package schema

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ──────────────────────────── InterruptKind 常量测试 ────────────────────────────

// TestInterruptKind_七值对齐 验证七个常量取值与 Python InterruptKind 一致
func TestInterruptKind_七值对齐(t *testing.T) {
	cases := map[string]string{
		InterruptKindUndeclared:             "",
		InterruptKindPermission:             "permission",
		InterruptKindConfirm:                "confirm",
		InterruptKindAskUser:                "ask_user",
		InterruptKindSkillEvolutionApproval: "skill_evolution_approval",
		InterruptKindTeamToolApproval:       "team_tool_approval",
		InterruptKindClientTool:             "client_tool",
	}
	for got, want := range cases {
		assert.Equal(t, want, got)
	}
}

// TestClientToolInterruptOwner_值 验证 owner 常量与 Python 一致
func TestClientToolInterruptOwner_值(t *testing.T) {
	assert.Equal(t, "openjiuwen.client_tool", ClientToolInterruptOwner)
}

// ──────────────────────────── InterruptKindOf 测试 ────────────────────────────

// TestInterruptKindOf_字段优先 验证字段有值时直接返回字段值
func TestInterruptKindOf_字段优先(t *testing.T) {
	req := &InterruptRequest{
		InterruptKind: InterruptKindPermission,
		Metadata:      map[string]any{"interrupt_kind": InterruptKindConfirm},
	}
	assert.Equal(t, InterruptKindPermission, InterruptKindOf(req))
}

// TestInterruptKindOf_字段空回退metadata 验证字段为空时回退到 metadata 里的历史形态
func TestInterruptKindOf_字段空回退metadata(t *testing.T) {
	req := &InterruptRequest{
		Metadata: map[string]any{"interrupt_kind": InterruptKindSkillEvolutionApproval},
	}
	assert.Equal(t, InterruptKindSkillEvolutionApproval, InterruptKindOf(req))
}

// TestInterruptKindOf_都未声明 验证字段与 metadata 都没有时返回空串
func TestInterruptKindOf_都未声明(t *testing.T) {
	assert.Equal(t, InterruptKindUndeclared, InterruptKindOf(&InterruptRequest{}))
	assert.Equal(t, InterruptKindUndeclared, InterruptKindOf(&InterruptRequest{
		Metadata: map[string]any{},
	}))
}

// TestInterruptKindOf_metadata非字符串 验证 metadata 里的 kind 非字符串时按未声明处理
func TestInterruptKindOf_metadata非字符串(t *testing.T) {
	for _, bad := range []any{123, true, nil, []string{"x"}} {
		req := &InterruptRequest{Metadata: map[string]any{"interrupt_kind": bad}}
		assert.Equal(t, InterruptKindUndeclared, InterruptKindOf(req), "bad=%v", bad)
	}
}

// TestInterruptKindOf_nil 验证 nil 安全
func TestInterruptKindOf_nil(t *testing.T) {
	assert.Equal(t, InterruptKindUndeclared, InterruptKindOf(nil))
}

// ──────────────────────────── 新字段测试 ────────────────────────────

// TestInterruptRequest_新字段默认零值 验证新增四个字段零值语义
func TestInterruptRequest_新字段默认零值(t *testing.T) {
	req := &InterruptRequest{}
	assert.Nil(t, req.Metadata)
	assert.Empty(t, req.InterruptID)
	assert.Empty(t, req.InterruptKind)
	assert.Empty(t, req.InterruptOwner)
}

// TestInterruptRequest_新字段赋值 验证四个字段可正常读写
func TestInterruptRequest_新字段赋值(t *testing.T) {
	req := &InterruptRequest{
		Message:        "确认执行",
		Metadata:       map[string]any{"tool_invocation_key": map[string]any{"invocation_id": "inv-1"}},
		InterruptID:    "intr_abc",
		InterruptKind:  InterruptKindPermission,
		InterruptOwner: "some.rail.Owner",
	}
	assert.Equal(t, "intr_abc", req.InterruptID)
	assert.Equal(t, InterruptKindPermission, req.InterruptKind)
	assert.Equal(t, "some.rail.Owner", req.InterruptOwner)
	assert.Equal(t, "inv-1", req.Metadata["tool_invocation_key"].(map[string]any)["invocation_id"])
}

// TestInterruptRequest_JSON序列化含新字段 验证新字段被序列化输出
func TestInterruptRequest_JSON序列化含新字段(t *testing.T) {
	req := &InterruptRequest{
		Message:        "msg",
		Metadata:       map[string]any{"k": "v"},
		InterruptID:    "intr_1",
		InterruptKind:  InterruptKindAskUser,
		InterruptOwner: "owner-1",
	}
	raw, err := json.Marshal(req)
	require.NoError(t, err)

	var back map[string]any
	require.NoError(t, json.Unmarshal(raw, &back))
	assert.Equal(t, "intr_1", back["InterruptID"])
	assert.Equal(t, "ask_user", back["InterruptKind"])
	assert.Equal(t, "owner-1", back["InterruptOwner"])
	assert.Equal(t, "v", back["Metadata"].(map[string]any)["k"])
}

// TestInterruptRequest_JSON往返 验证序列化后能无损还原
func TestInterruptRequest_JSON往返(t *testing.T) {
	orig := &InterruptRequest{
		Message:        "msg",
		PayloadSchema:  map[string]any{"type": "object"},
		AutoConfirmKey: "key-1",
		UIOptions:      []map[string]any{{"label": "ok"}},
		Metadata:       map[string]any{"tool_invocation_key": map[string]any{"invocation_id": "inv-1"}},
		InterruptID:    "intr_1",
		InterruptKind:  InterruptKindPermission,
		InterruptOwner: "owner-1",
	}
	raw, err := json.Marshal(orig)
	require.NoError(t, err)

	var back InterruptRequest
	require.NoError(t, json.Unmarshal(raw, &back))
	assert.Equal(t, orig.Message, back.Message)
	assert.Equal(t, orig.AutoConfirmKey, back.AutoConfirmKey)
	assert.Equal(t, orig.InterruptID, back.InterruptID)
	assert.Equal(t, orig.InterruptKind, back.InterruptKind)
	assert.Equal(t, orig.InterruptOwner, back.InterruptOwner)
	assert.Equal(t, orig.Metadata, back.Metadata)
	assert.Equal(t, orig.PayloadSchema, back.PayloadSchema)
	assert.Len(t, back.UIOptions, 1)
}

// TestInterruptRequest_Getter不受新字段影响 验证接口方法仍只看原字段
func TestInterruptRequest_Getter不受新字段影响(t *testing.T) {
	req := &InterruptRequest{
		Message:        "确认",
		AutoConfirmKey: "ack",
		InterruptID:    "intr_1",
		InterruptKind:  InterruptKindPermission,
		InterruptOwner: "owner-1",
	}
	assert.Equal(t, "确认", req.GetMessage())
	assert.Equal(t, "ack", req.GetAutoConfirmKey())
}

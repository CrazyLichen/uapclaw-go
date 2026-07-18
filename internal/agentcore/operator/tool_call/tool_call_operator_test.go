package tool_call

import (
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/operator"
	"github.com/uapclaw/uapclaw-go/internal/evolving/schema"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ──────────────────────────── 枚 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// ──────────────────────────── 非导出函数 ────────────────────────────

func TestNewToolCallOperator(t *testing.T) {
	t.Run("基本构造", func(t *testing.T) {
		op := NewToolCallOperator("tool_op")
		if op.OperatorID() != "tool_op" {
			t.Errorf("operatorID = %q, expected %q", op.OperatorID(), "tool_op")
		}
	})

	t.Run("带描述", func(t *testing.T) {
		descs := map[string]any{"search": "search tool", "calc": "calculator"}
		op := NewToolCallOperator("tool_op", WithDescriptions(descs))
		state := op.GetState()
		td := state[TargetToolDescription].(map[string]any)
		if td["search"] != "search tool" {
			t.Errorf("search desc = %q, expected %q", td["search"], "search tool")
		}
	})
}

func TestToolCallOperator_GetTunables(t *testing.T) {
	t.Run("空描述无tunable", func(t *testing.T) {
		op := NewToolCallOperator("tool_op")
		tunables := op.GetTunables()
		if len(tunables) != 0 {
			t.Errorf("tunables count = %d, expected 0", len(tunables))
		}
	})

	t.Run("有描述暴露tunable", func(t *testing.T) {
		op := NewToolCallOperator("tool_op", WithDescriptions(map[string]any{"search": "desc"}))
		tunables := op.GetTunables()
		spec, ok := tunables[TargetToolDescription]
		if !ok {
			t.Fatal("tool_description should be tunable")
		}
		if spec.Kind != operator.TunableKindText {
			t.Errorf("kind = %v, expected %v", spec.Kind, operator.TunableKindText)
		}
	})
}

func TestToolCallOperator_SetParameter(t *testing.T) {
	t.Run("正确更新", func(t *testing.T) {
		op := NewToolCallOperator("tool_op", WithDescriptions(map[string]any{"search": "old"}))
		op.SetParameter(TargetToolDescription, map[string]any{"search": "new desc"})
		state := op.GetState()
		td := state[TargetToolDescription].(map[string]any)
		if td["search"] != "new desc" {
			t.Errorf("desc = %q, expected %q", td["search"], "new desc")
		}
	})

	t.Run("忽略错误target", func(t *testing.T) {
		op := NewToolCallOperator("tool_op", WithDescriptions(map[string]any{"search": "old"}))
		op.SetParameter("wrong_target", map[string]any{"search": "new"})
		state := op.GetState()
		td := state[TargetToolDescription].(map[string]any)
		if td["search"] != "old" {
			t.Error("should not update for wrong target")
		}
	})

	t.Run("忽略非map值", func(t *testing.T) {
		op := NewToolCallOperator("tool_op", WithDescriptions(map[string]any{"search": "old"}))
		op.SetParameter(TargetToolDescription, "not a map")
		state := op.GetState()
		td := state[TargetToolDescription].(map[string]any)
		if td["search"] != "old" {
			t.Error("should not update for non-map value")
		}
	})

	t.Run("回调触发", func(t *testing.T) {
		var capturedTarget string
		cb := operator.ParameterUpdatedCallback(func(target string, _ any) {
			capturedTarget = target
		})
		op := NewToolCallOperator("tool_op",
			WithDescriptions(map[string]any{"search": "old"}),
			WithToolCallOnParameterUpdated(cb),
		)
		op.SetParameter(TargetToolDescription, map[string]any{"search": "new"})
		if capturedTarget != TargetToolDescription {
			t.Errorf("callback target = %q, expected %q", capturedTarget, TargetToolDescription)
		}
	})

	t.Run("保留any类型值", func(t *testing.T) {
		op := NewToolCallOperator("tool_op", WithDescriptions(map[string]any{"search": "old"}))
		op.SetParameter(TargetToolDescription, map[string]any{"search": 42})
		state := op.GetState()
		td := state[TargetToolDescription].(map[string]any)
		if td["search"] != 42 {
			t.Errorf("desc = %v, expected 42", td["search"])
		}
	})
}

func TestToolCallOperator_LoadState(t *testing.T) {
	t.Run("map[string]any恢复", func(t *testing.T) {
		op := NewToolCallOperator("tool_op")
		op.LoadState(map[string]any{
			TargetToolDescription: map[string]any{"search": "restored any"},
		})
		state := op.GetState()
		td := state[TargetToolDescription].(map[string]any)
		if td["search"] != "restored any" {
			t.Errorf("desc = %q, expected %q", td["search"], "restored any")
		}
	})

	t.Run("回调触发", func(t *testing.T) {
		called := false
		cb := operator.ParameterUpdatedCallback(func(string, any) { called = true })
		op := NewToolCallOperator("tool_op", WithToolCallOnParameterUpdated(cb))
		op.LoadState(map[string]any{
			TargetToolDescription: map[string]any{"search": "restored"},
		})
		if !called {
			t.Error("callback should have been called")
		}
	})

	t.Run("非map类型状态被忽略", func(t *testing.T) {
		op := NewToolCallOperator("tool_op", WithDescriptions(map[string]any{"search": "old"}))
		op.LoadState(map[string]any{
			TargetToolDescription: "not a map",
		})
		state := op.GetState()
		td := state[TargetToolDescription].(map[string]any)
		if td["search"] != "old" {
			t.Error("should not update for non-map state")
		}
	})
}

func TestToolCallOperator_ApplyUpdate(t *testing.T) {
	op := NewToolCallOperator("tool_op", WithDescriptions(map[string]any{"search": "old"}))
	update := schema.NewUpdateValue(map[string]any{"search": "new"})
	result := op.ApplyUpdate(TargetToolDescription, update)
	if !result.Applied {
		t.Error("should be applied")
	}
	if result.OperatorID != "tool_op" {
		t.Errorf("operatorID = %q, expected %q", result.OperatorID, "tool_op")
	}
}

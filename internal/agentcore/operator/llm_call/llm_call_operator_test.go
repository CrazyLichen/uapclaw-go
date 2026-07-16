package llm_call

import (
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/operator"
	"github.com/uapclaw/uapclaw-go/internal/evolving/schema"
)

func TestNewLLMCallOperator(t *testing.T) {
	t.Run("基本构造", func(t *testing.T) {
		op := NewLLMCallOperator("system prompt", "user prompt")
		if op.OperatorID() != "llm_call" {
			t.Errorf("operatorID = %q, expected %q", op.OperatorID(), "llm_call")
		}
		state := op.GetState()
		if state[TargetSystemPrompt] != "system prompt" {
			t.Errorf("system prompt = %v, expected %q", state[TargetSystemPrompt], "system prompt")
		}
		if state[TargetUserPrompt] != "user prompt" {
			t.Errorf("user prompt = %v, expected %q", state[TargetUserPrompt], "user prompt")
		}
	})

	t.Run("默认userPrompt", func(t *testing.T) {
		op := NewLLMCallOperator("system prompt", "")
		state := op.GetState()
		if state[TargetUserPrompt] != defaultUserPrompt {
			t.Errorf("user prompt = %v, expected %q", state[TargetUserPrompt], defaultUserPrompt)
		}
	})

	t.Run("默认冻结状态", func(t *testing.T) {
		op := NewLLMCallOperator("system", "user")
		if op.GetFreezeSystemPrompt() {
			t.Error("system prompt should not be frozen by default")
		}
		if !op.GetFreezeUserPrompt() {
			t.Error("user prompt should be frozen by default")
		}
	})

	t.Run("选项", func(t *testing.T) {
		op := NewLLMCallOperator("system", "user",
			WithFreezeSystemPrompt(true),
			WithFreezeUserPrompt(false),
			WithLLMCallOperatorID("custom_id"),
		)
		if !op.GetFreezeSystemPrompt() {
			t.Error("system prompt should be frozen")
		}
		if op.GetFreezeUserPrompt() {
			t.Error("user prompt should not be frozen")
		}
		if op.OperatorID() != "custom_id" {
			t.Errorf("operatorID = %q, expected %q", op.OperatorID(), "custom_id")
		}
	})
}

func TestLLMCallOperator_GetTunables(t *testing.T) {
	t.Run("默认只有system_prompt", func(t *testing.T) {
		op := NewLLMCallOperator("system", "user")
		tunables := op.GetTunables()
		if _, ok := tunables[TargetSystemPrompt]; !ok {
			t.Error("system_prompt should be tunable")
		}
		if _, ok := tunables[TargetUserPrompt]; ok {
			t.Error("user_prompt should not be tunable (frozen by default)")
		}
	})

	t.Run("两个都解冻", func(t *testing.T) {
		op := NewLLMCallOperator("system", "user", WithFreezeUserPrompt(false))
		tunables := op.GetTunables()
		if len(tunables) != 2 {
			t.Errorf("tunables count = %d, expected 2", len(tunables))
		}
	})

	t.Run("两个都冻结", func(t *testing.T) {
		op := NewLLMCallOperator("system", "user", WithFreezeSystemPrompt(true))
		tunables := op.GetTunables()
		if len(tunables) != 0 {
			t.Errorf("tunables count = %d, expected 0", len(tunables))
		}
	})
}

func TestLLMCallOperator_SetParameter(t *testing.T) {
	t.Run("更新未冻结的system_prompt", func(t *testing.T) {
		op := NewLLMCallOperator("old system", "user")
		op.SetParameter(TargetSystemPrompt, "new system")
		state := op.GetState()
		if state[TargetSystemPrompt] != "new system" {
			t.Errorf("system prompt = %v, expected %q", state[TargetSystemPrompt], "new system")
		}
	})

	t.Run("冻结的user_prompt不更新", func(t *testing.T) {
		op := NewLLMCallOperator("system", "old user")
		op.SetParameter(TargetUserPrompt, "new user")
		state := op.GetState()
		if state[TargetUserPrompt] != "old user" {
			t.Errorf("frozen user prompt should not change, got %v", state[TargetUserPrompt])
		}
	})

	t.Run("解冻后可更新user_prompt", func(t *testing.T) {
		op := NewLLMCallOperator("system", "old user", WithFreezeUserPrompt(false))
		op.SetParameter(TargetUserPrompt, "new user")
		state := op.GetState()
		if state[TargetUserPrompt] != "new user" {
			t.Errorf("user prompt = %v, expected %q", state[TargetUserPrompt], "new user")
		}
	})

	t.Run("回调触发", func(t *testing.T) {
		var capturedTarget string
		var capturedValue any
		cb := operator.ParameterUpdatedCallback(func(target string, value any) {
			capturedTarget = target
			capturedValue = value
		})
		op := NewLLMCallOperator("system", "user", WithLLMCallOnParameterUpdated(cb))
		op.SetParameter(TargetSystemPrompt, "new system")
		if capturedTarget != TargetSystemPrompt {
			t.Errorf("callback target = %q, expected %q", capturedTarget, TargetSystemPrompt)
		}
		if capturedValue != "new system" {
			t.Errorf("callback value = %v, expected %q", capturedValue, "new system")
		}
	})
}

func TestLLMCallOperator_LoadState(t *testing.T) {
	t.Run("完整恢复", func(t *testing.T) {
		var callbacks []string
		cb := operator.ParameterUpdatedCallback(func(target string, _ any) {
			callbacks = append(callbacks, target)
		})
		op := NewLLMCallOperator("system", "user",
			WithFreezeSystemPrompt(true), // 冻结状态
			WithLLMCallOnParameterUpdated(cb),
		)
		// LoadState 不检查冻结标记
		op.LoadState(map[string]any{
			TargetSystemPrompt: "restored system",
			TargetUserPrompt:   "restored user",
		})
		state := op.GetState()
		if state[TargetSystemPrompt] != "restored system" {
			t.Errorf("system prompt should be restored even when frozen, got %v", state[TargetSystemPrompt])
		}
		if state[TargetUserPrompt] != "restored user" {
			t.Errorf("user prompt = %v, expected %q", state[TargetUserPrompt], "restored user")
		}
		// 回调应为每个字段触发
		if len(callbacks) != 2 {
			t.Errorf("callbacks count = %d, expected 2", len(callbacks))
		}
	})

	t.Run("部分恢复", func(t *testing.T) {
		op := NewLLMCallOperator("system", "user")
		op.LoadState(map[string]any{
			TargetSystemPrompt: "restored only",
		})
		state := op.GetState()
		if state[TargetSystemPrompt] != "restored only" {
			t.Errorf("system prompt = %v, expected %q", state[TargetSystemPrompt], "restored only")
		}
	})
}

func TestLLMCallOperator_ApplyUpdate(t *testing.T) {
	op := NewLLMCallOperator("system", "user", WithFreezeUserPrompt(false))
	update := schema.NewUpdateValue("new system")
	result := op.ApplyUpdate(TargetSystemPrompt, update)
	if !result.Applied {
		t.Error("should be applied")
	}
	if result.OperatorID != "llm_call" {
		t.Errorf("operatorID = %q, expected %q", result.OperatorID, "llm_call")
	}
}

func TestLLMCallOperator_FreezeControl(t *testing.T) {
	op := NewLLMCallOperator("system", "user")
	op.SetFreezeSystemPrompt(true)
	if !op.GetFreezeSystemPrompt() {
		t.Error("system prompt should be frozen after set")
	}
	op.SetFreezeUserPrompt(false)
	if op.GetFreezeUserPrompt() {
		t.Error("user prompt should not be frozen after set")
	}
}

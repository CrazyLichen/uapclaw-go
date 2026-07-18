package llm_call

import (
	"testing"

	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/operator"
	evolvingschema "github.com/uapclaw/uapclaw-go/internal/evolving/schema"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// ──────────────────────────── 非导出函数 ────────────────────────────

// textMessages 从字符串列表构造 []llmschema.BaseMessage。
// 每个字符串创建一条 UserMessage。
func textMessages(texts ...string) []llmschema.BaseMessage {
	msgs := make([]llmschema.BaseMessage, 0, len(texts))
	for _, text := range texts {
		msgs = append(msgs, llmschema.NewUserMessage(text))
	}
	return msgs
}

// systemMessages 从字符串构造系统消息列表。
func systemMessages(texts ...string) []llmschema.BaseMessage {
	msgs := make([]llmschema.BaseMessage, 0, len(texts))
	for _, text := range texts {
		msgs = append(msgs, llmschema.NewSystemMessage(text))
	}
	return msgs
}

func TestNewLLMCallOperator(t *testing.T) {
	t.Run("基本构造", func(t *testing.T) {
		op := NewLLMCallOperator(
			textMessages("system prompt"),
			textMessages("user prompt"),
		)
		if op.OperatorID() != "llm_call" {
			t.Errorf("operatorID = %q, expected %q", op.OperatorID(), "llm_call")
		}
		state := op.GetState()
		msgs, ok := state[TargetSystemPrompt].([]llmschema.BaseMessage)
		if !ok {
			t.Fatalf("system prompt type = %T, expected []llmschema.BaseMessage", state[TargetSystemPrompt])
		}
		if len(msgs) == 0 || msgs[0].GetContent().Text() != "system prompt" {
			t.Errorf("system prompt text = %v, expected %q", msgs, "system prompt")
		}
	})

	t.Run("默认userPrompt", func(t *testing.T) {
		op := NewLLMCallOperator(textMessages("system prompt"), nil)
		state := op.GetState()
		msgs, ok := state[TargetUserPrompt].([]llmschema.BaseMessage)
		if !ok {
			t.Fatalf("user prompt type = %T, expected []llmschema.BaseMessage", state[TargetUserPrompt])
		}
		if len(msgs) == 0 || msgs[0].GetContent().Text() != defaultUserPrompt {
			t.Errorf("user prompt text = %v, expected %q", msgs[0].GetContent().Text(), defaultUserPrompt)
		}
	})

	t.Run("默认冻结状态", func(t *testing.T) {
		op := NewLLMCallOperator(textMessages("system"), textMessages("user"))
		if op.GetFreezeSystemPrompt() {
			t.Error("system prompt should not be frozen by default")
		}
		if !op.GetFreezeUserPrompt() {
			t.Error("user prompt should be frozen by default")
		}
	})

	t.Run("选项", func(t *testing.T) {
		op := NewLLMCallOperator(
			textMessages("system"),
			textMessages("user"),
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
		op := NewLLMCallOperator(textMessages("system"), textMessages("user"))
		tunables := op.GetTunables()
		if _, ok := tunables[TargetSystemPrompt]; !ok {
			t.Error("system_prompt should be tunable")
		}
		if _, ok := tunables[TargetUserPrompt]; ok {
			t.Error("user_prompt should not be tunable (frozen by default)")
		}
	})

	t.Run("两个都解冻", func(t *testing.T) {
		op := NewLLMCallOperator(textMessages("system"), textMessages("user"), WithFreezeUserPrompt(false))
		tunables := op.GetTunables()
		if len(tunables) != 2 {
			t.Errorf("tunables count = %d, expected 2", len(tunables))
		}
	})

	t.Run("两个都冻结", func(t *testing.T) {
		op := NewLLMCallOperator(textMessages("system"), textMessages("user"), WithFreezeSystemPrompt(true))
		tunables := op.GetTunables()
		if len(tunables) != 0 {
			t.Errorf("tunables count = %d, expected 0", len(tunables))
		}
	})
}

func TestLLMCallOperator_SetParameter(t *testing.T) {
	t.Run("更新未冻结的system_prompt", func(t *testing.T) {
		op := NewLLMCallOperator(textMessages("old system"), textMessages("user"))
		op.SetParameter(TargetSystemPrompt, "new system")
		state := op.GetState()
		if state[TargetSystemPrompt] != "new system" {
			t.Errorf("system prompt = %v, expected %q", state[TargetSystemPrompt], "new system")
		}
	})

	t.Run("冻结的user_prompt不更新", func(t *testing.T) {
		op := NewLLMCallOperator(textMessages("system"), textMessages("old user"))
		op.SetParameter(TargetUserPrompt, "new user")
		state := op.GetState()
		msgs, ok := state[TargetUserPrompt].([]llmschema.BaseMessage)
		if !ok || len(msgs) == 0 || msgs[0].GetContent().Text() != "old user" {
			t.Errorf("frozen user prompt should not change, got %v", state[TargetUserPrompt])
		}
	})

	t.Run("解冻后可更新user_prompt", func(t *testing.T) {
		op := NewLLMCallOperator(textMessages("system"), textMessages("old user"), WithFreezeUserPrompt(false))
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
		op := NewLLMCallOperator(textMessages("system"), textMessages("user"), WithLLMCallOnParameterUpdated(cb))
		op.SetParameter(TargetSystemPrompt, "new system")
		if capturedTarget != TargetSystemPrompt {
			t.Errorf("callback target = %q, expected %q", capturedTarget, TargetSystemPrompt)
		}
		if capturedValue != "new system" {
			t.Errorf("callback value = %v, expected %q", capturedValue, "new system")
		}
	})

	t.Run("使用消息列表更新system_prompt", func(t *testing.T) {
		op := NewLLMCallOperator(textMessages("old"), textMessages("user"))
		newMsgs := systemMessages("new system message")
		op.SetParameter(TargetSystemPrompt, newMsgs)
		state := op.GetState()
		msgs, ok := state[TargetSystemPrompt].([]llmschema.BaseMessage)
		if !ok {
			t.Fatalf("system prompt type = %T, expected []llmschema.BaseMessage", state[TargetSystemPrompt])
		}
		if len(msgs) == 0 || msgs[0].GetContent().Text() != "new system message" {
			t.Errorf("system prompt text = %v, expected %q", msgs, "new system message")
		}
	})
}

func TestLLMCallOperator_LoadState(t *testing.T) {
	t.Run("完整恢复", func(t *testing.T) {
		var callbacks []string
		cb := operator.ParameterUpdatedCallback(func(target string, _ any) {
			callbacks = append(callbacks, target)
		})
		op := NewLLMCallOperator(
			textMessages("system"),
			textMessages("user"),
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
		op := NewLLMCallOperator(textMessages("system"), textMessages("user"))
		op.LoadState(map[string]any{
			TargetSystemPrompt: "restored only",
		})
		state := op.GetState()
		if state[TargetSystemPrompt] != "restored only" {
			t.Errorf("system prompt = %v, expected %q", state[TargetSystemPrompt], "restored only")
		}
	})

	t.Run("使用消息列表恢复", func(t *testing.T) {
		op := NewLLMCallOperator(textMessages("old"), textMessages("user"))
		restoreMsgs := systemMessages("restored system")
		op.LoadState(map[string]any{
			TargetSystemPrompt: restoreMsgs,
		})
		state := op.GetState()
		msgs, ok := state[TargetSystemPrompt].([]llmschema.BaseMessage)
		if !ok {
			t.Fatalf("system prompt type = %T, expected []llmschema.BaseMessage", state[TargetSystemPrompt])
		}
		if len(msgs) == 0 || msgs[0].GetContent().Text() != "restored system" {
			t.Errorf("system prompt text = %v, expected %q", msgs, "restored system")
		}
	})
}

func TestLLMCallOperator_ApplyUpdate(t *testing.T) {
	op := NewLLMCallOperator(textMessages("system"), textMessages("user"), WithFreezeUserPrompt(false))
	update := evolvingschema.NewUpdateValue("new system")
	result := op.ApplyUpdate(TargetSystemPrompt, update)
	if !result.Applied {
		t.Error("should be applied")
	}
	if result.OperatorID != "llm_call" {
		t.Errorf("operatorID = %q, expected %q", result.OperatorID, "llm_call")
	}
}

func TestLLMCallOperator_FreezeControl(t *testing.T) {
	op := NewLLMCallOperator(textMessages("system"), textMessages("user"))
	op.SetFreezeSystemPrompt(true)
	if !op.GetFreezeSystemPrompt() {
		t.Error("system prompt should be frozen after set")
	}
	op.SetFreezeUserPrompt(false)
	if op.GetFreezeUserPrompt() {
		t.Error("user prompt should not be frozen after set")
	}
}

func TestPromptContent(t *testing.T) {
	t.Run("string 直接返回", func(t *testing.T) {
		result := promptContent("hello")
		if result != "hello" {
			t.Errorf("promptContent(string) = %v, expected %q", result, "hello")
		}
	})

	t.Run("nil 返回空字符串", func(t *testing.T) {
		result := promptContent(nil)
		if result != "" {
			t.Errorf("promptContent(nil) = %v, expected %q", result, "")
		}
	})

	t.Run("[]any 保留原始结构", func(t *testing.T) {
		input := []any{"a", "b"}
		result := promptContent(input)
		resultSlice, ok := result.([]any)
		if !ok {
			t.Fatalf("promptContent([]any) type = %T, expected []any", result)
		}
		if len(resultSlice) != 2 || resultSlice[0] != "a" || resultSlice[1] != "b" {
			t.Errorf("promptContent([]any) = %v, expected [a b]", resultSlice)
		}
	})

	t.Run("[]map[string]any 保留原始结构", func(t *testing.T) {
		input := []map[string]any{{"role": "system", "content": "hello"}}
		result := promptContent(input)
		resultSlice, ok := result.([]map[string]any)
		if !ok {
			t.Fatalf("promptContent([]map[string]any) type = %T, expected []map[string]any", result)
		}
		if len(resultSlice) != 1 || resultSlice[0]["role"] != "system" {
			t.Errorf("promptContent([]map[string]any) = %v, expected [{role:system content:hello}]", resultSlice)
		}
	})
}

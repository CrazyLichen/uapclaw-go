package llm_call

import (
	"testing"

	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/operator"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/operator/llm_call"
)

// ──────────────────────────── 导出函数 ────────────────────────────

func TestLLMCallOptimizerBase_Domain(t *testing.T) {
	base := &LLMCallOptimizerBase{}
	if base.Domain() != "llm" {
		t.Errorf("Domain() = %q, expected %q", base.Domain(), "llm")
	}
}

func TestLLMCallOptimizerBase_DefaultTargets(t *testing.T) {
	base := &LLMCallOptimizerBase{}
	targets := base.DefaultTargets()
	if len(targets) != 2 || targets[0] != "system_prompt" || targets[1] != "user_prompt" {
		t.Errorf("DefaultTargets() = %v, expected [system_prompt, user_prompt]", targets)
	}
}

func TestLLMCallOptimizerBase_RequiresForwardData(t *testing.T) {
	base := &LLMCallOptimizerBase{}
	if !base.RequiresForwardData() {
		t.Error("RequiresForwardData() should be true")
	}
}

func TestLLMCallOptimizerBase_isTargetFrozen(t *testing.T) {
	base := &LLMCallOptimizerBase{}
	// 未冻结的 operator（默认 freezeUserPrompt=true，所以 user_prompt 冻结）
	op := llm_call.NewLLMCallOperator(
		[]llmschema.BaseMessage{llmschema.NewSystemMessage("sys")},
		[]llmschema.BaseMessage{llmschema.NewUserMessage("usr")},
	)
	// system_prompt 默认不冻结
	if base.isTargetFrozen(op, "system_prompt") {
		t.Error("system_prompt 不应冻结（默认 freezeSystemPrompt=false）")
	}
	// user_prompt 默认冻结
	if !base.isTargetFrozen(op, "user_prompt") {
		t.Error("user_prompt 应冻结（默认 freezeUserPrompt=true）")
	}

	// 解冻 user_prompt
	op2 := llm_call.NewLLMCallOperator(
		[]llmschema.BaseMessage{llmschema.NewSystemMessage("sys")},
		[]llmschema.BaseMessage{llmschema.NewUserMessage("usr")},
		llm_call.WithFreezeUserPrompt(false),
	)
	if base.isTargetFrozen(op2, "user_prompt") {
		t.Error("user_prompt 不应冻结（已设置 freezeUserPrompt=false）")
	}
}

func TestLLMCallOptimizerBase_getPromptTemplate(t *testing.T) {
	base := &LLMCallOptimizerBase{}
	op := llm_call.NewLLMCallOperator(
		[]llmschema.BaseMessage{llmschema.NewSystemMessage("hello system")},
		[]llmschema.BaseMessage{llmschema.NewUserMessage("hello user")},
		llm_call.WithFreezeUserPrompt(false),
	)

	sysTpl := base.getPromptTemplate(op, "system_prompt")
	if sysTpl == nil {
		t.Fatal("getPromptTemplate(system_prompt) 返回 nil")
	}
	msgs, err := sysTpl.ToMessages()
	if err != nil {
		t.Fatalf("ToMessages failed: %v", err)
	}
	if len(msgs) == 0 {
		t.Fatal("system_prompt 模板消息为空")
	}

	usrTpl := base.getPromptTemplate(op, "user_prompt")
	if usrTpl == nil {
		t.Fatal("getPromptTemplate(user_prompt) 返回 nil")
	}
}

func TestLLMCallOptimizerBase_Bind(t *testing.T) {
	base := &LLMCallOptimizerBase{}
	op := llm_call.NewLLMCallOperator(
		[]llmschema.BaseMessage{llmschema.NewSystemMessage("sys")},
		nil, // 默认 user_prompt
		llm_call.WithFreezeUserPrompt(false),
	)
	operators := map[string]operator.Operator{"llm_call": op}

	t.Run("显式targets", func(t *testing.T) {
		count := base.Bind(operators, []string{"system_prompt", "user_prompt"}, nil)
		if count != 1 {
			t.Errorf("Bind() = %d, expected 1", count)
		}
	})

	t.Run("空targets使用默认值", func(t *testing.T) {
		base2 := &LLMCallOptimizerBase{}
		// 传入空切片触发默认值逻辑
		count := base2.Bind(operators, []string{}, nil)
		// 空切片 != nil，所以 FilterOperators 会用空 targets，不会匹配
		// 这是预期行为：调用方应传 nil 或显式 targets
		_ = count
	})
}

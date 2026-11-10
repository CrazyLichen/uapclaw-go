package rails

import (
	"encoding/json"
	"strings"
	"testing"

	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session"
	sessioninterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/session/interfaces"
	saschema "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/schema"
)

// toolCallWithQuestions 构造带 questions 参数的工具调用。
func toolCallWithQuestions(t *testing.T, questions any) *llmschema.ToolCall {
	t.Helper()
	return &llmschema.ToolCall{
		ID:        "call-1",
		Name:      "ask_user",
		Arguments: buildArgsJSON(t, map[string]any{"questions": questions}),
	}
}

// marshalJSON 序列化为 JSON 字符串。
func marshalJSON(value any) (string, error) {
	encoded, err := json.Marshal(value)
	return string(encoded), err
}

// buildArgsJSON 把参数序列化为 JSON 字符串。
func buildArgsJSON(t *testing.T, args map[string]any) string {
	t.Helper()
	encoded, err := marshalJSON(args)
	if err != nil {
		t.Fatalf("marshal args: %v", err)
	}
	return encoded
}

// TestValidateStructuredQuestions_无questions 验证直接通过。
func TestValidateStructuredQuestions_无questions(t *testing.T) {
	toolCall := &llmschema.ToolCall{ID: "c1", Name: "ask_user", Arguments: `{"query":"hi"}`}
	if got := ValidateStructuredQuestions(toolCall, true); got != "" {
		t.Errorf("no questions should pass, got %q", got)
	}
}

// TestValidateStructuredQuestions_nilToolCall 验证 nil 安全。
func TestValidateStructuredQuestions_nilToolCall(t *testing.T) {
	if got := ValidateStructuredQuestions(nil, true); got != "" {
		t.Errorf("nil toolCall should pass, got %q", got)
	}
}

// TestValidateStructuredQuestions_questions非数组 验证第一个校验分支。
func TestValidateStructuredQuestions_questions非数组(t *testing.T) {
	toolCall := toolCallWithQuestions(t, "not an array")
	got := ValidateStructuredQuestions(toolCall, true)
	if !strings.Contains(got, "questions must be an array of question objects") {
		t.Errorf("got %q", got)
	}
}

// TestValidateStructuredQuestions_超限 验证数量上限。
func TestValidateStructuredQuestions_超限(t *testing.T) {
	questions := make([]any, MaxStructuredQuestions+1)
	for index := range questions {
		questions[index] = map[string]any{"question": "q"}
	}
	toolCall := toolCallWithQuestions(t, questions)
	got := ValidateStructuredQuestions(toolCall, true)
	if !strings.Contains(got, "accepts at most") {
		t.Errorf("got %q", got)
	}
}

// TestValidateStructuredQuestions_问题非对象 验证第二个分支。
func TestValidateStructuredQuestions_问题非对象(t *testing.T) {
	toolCall := toolCallWithQuestions(t, []any{"string question"})
	got := ValidateStructuredQuestions(toolCall, true)
	if !strings.Contains(got, "questions[0] must be an object") {
		t.Errorf("got %q", got)
	}
}

// TestValidateStructuredQuestions_question缺失 验证非空校验。
func TestValidateStructuredQuestions_question缺失(t *testing.T) {
	toolCall := toolCallWithQuestions(t, []any{map[string]any{"header": "H"}})
	got := ValidateStructuredQuestions(toolCall, true)
	if !strings.Contains(got, "questions[0].question is required") {
		t.Errorf("got %q", got)
	}
}

// TestValidateStructuredQuestions_question空白 验证空白串算缺失。
func TestValidateStructuredQuestions_question空白(t *testing.T) {
	toolCall := toolCallWithQuestions(t, []any{map[string]any{"question": "   "}})
	if got := ValidateStructuredQuestions(toolCall, true); !strings.Contains(got, "non-empty string") {
		t.Errorf("got %q", got)
	}
}

// TestValidateStructuredQuestions_header非字符串 验证 header 类型校验。
func TestValidateStructuredQuestions_header非字符串(t *testing.T) {
	toolCall := toolCallWithQuestions(t, []any{
		map[string]any{"question": "q", "header": 42},
	})
	got := ValidateStructuredQuestions(toolCall, true)
	if !strings.Contains(got, "header must be a string") {
		t.Errorf("got %q", got)
	}
}

// TestValidateStructuredQuestions_options非数组 验证 options 类型校验。
func TestValidateStructuredQuestions_options非数组(t *testing.T) {
	toolCall := toolCallWithQuestions(t, []any{
		map[string]any{"question": "q", "options": "not array"},
	})
	got := ValidateStructuredQuestions(toolCall, true)
	if !strings.Contains(got, "options must be an array of option objects") {
		t.Errorf("got %q", got)
	}
}

// TestValidateStructuredQuestions_option缺label 验证 label 必填。
func TestValidateStructuredQuestions_option缺label(t *testing.T) {
	toolCall := toolCallWithQuestions(t, []any{
		map[string]any{"question": "q", "options": []any{
			map[string]any{"label": "A"}, map[string]any{},
		}},
	})
	got := ValidateStructuredQuestions(toolCall, true)
	if !strings.Contains(got, "options[1].label is required") {
		t.Errorf("got %q", got)
	}
}

// TestValidateStructuredQuestions_options数量非法 验证 2-4 约束。
func TestValidateStructuredQuestions_options数量非法(t *testing.T) {
	toolCall := toolCallWithQuestions(t, []any{
		map[string]any{"question": "q", "options": []any{
			map[string]any{"label": "A"},
		}},
	})
	got := ValidateStructuredQuestions(toolCall, true)
	if !strings.Contains(got, "either 0 or 2-4 items") {
		t.Errorf("got %q", got)
	}
}

// TestValidateStructuredQuestions_保留标签Other 验证 strict 下的 Other 检查。
func TestValidateStructuredQuestions_保留标签Other(t *testing.T) {
	toolCall := toolCallWithQuestions(t, []any{
		map[string]any{"question": "q", "options": []any{
			map[string]any{"label": "Other"}, map[string]any{"label": "B"},
		}},
	})
	got := ValidateStructuredQuestions(toolCall, true)
	if !strings.Contains(got, "reserved label 'Other'") {
		t.Errorf("got %q", got)
	}
}

// TestValidateStructuredQuestions_重复标签 验证 strict 下的唯一性检查。
func TestValidateStructuredQuestions_重复标签(t *testing.T) {
	toolCall := toolCallWithQuestions(t, []any{
		map[string]any{"question": "q", "options": []any{
			map[string]any{"label": "A"}, map[string]any{"label": "A"},
		}},
	})
	got := ValidateStructuredQuestions(toolCall, true)
	if !strings.Contains(got, "unique labels") {
		t.Errorf("got %q", got)
	}
}

// TestValidateStructuredQuestions_desc非字符串 验证 description 类型校验。
func TestValidateStructuredQuestions_desc非字符串(t *testing.T) {
	toolCall := toolCallWithQuestions(t, []any{
		map[string]any{"question": "q", "options": []any{
			map[string]any{"label": "A", "description": 1},
			map[string]any{"label": "B"},
		}},
	})
	got := ValidateStructuredQuestions(toolCall, true)
	if !strings.Contains(got, "description and preview must be strings") {
		t.Errorf("got %q", got)
	}
}

// TestValidateStructuredQuestions_非strict放宽 验证续接请求跳过 strict 检查。
func TestValidateStructuredQuestions_非strict放宽(t *testing.T) {
	toolCall := toolCallWithQuestions(t, []any{
		map[string]any{"question": "q", "options": []any{
			map[string]any{"label": "Other"}, map[string]any{"label": "Other"},
		}},
	})
	if got := ValidateStructuredQuestions(toolCall, false); got != "" {
		t.Errorf("non-strict should pass, got %q", got)
	}
}

// TestValidateStructuredQuestions_合法通过 验证正常参数不被拒。
func TestValidateStructuredQuestions_合法通过(t *testing.T) {
	toolCall := toolCallWithQuestions(t, []any{
		map[string]any{"question": "Which branch?", "options": []any{
			map[string]any{"label": "main"}, map[string]any{"label": "dev"},
		}},
	})
	if got := ValidateStructuredQuestions(toolCall, true); got != "" {
		t.Errorf("valid questions should pass, got %q", got)
	}
}

// TestIsSavedPendingToolCall_nil安全 验证 nil 输入。
func TestIsSavedPendingToolCall_nil安全(t *testing.T) {
	if IsSavedPendingToolCall(nil, nil) {
		t.Error("nil inputs should be false")
	}
	if IsSavedPendingToolCall(nil, &llmschema.ToolCall{ID: "c1"}) {
		t.Error("nil cbc should be false")
	}
}

// TestIsSavedPendingToolCall_命中持久化态 验证从会话状态识别稀疏重放。
func TestIsSavedPendingToolCall_命中持久化态(t *testing.T) {
	sess := session.NewSession()
	sess.UpdateState(map[string]any{
		saschema.InterruptionKey: &saschema.ToolInterruptionState{
			InterruptedTools: map[string]*saschema.ToolInterruptEntry{
				"c1": {ToolCall: &llmschema.ToolCall{ID: "saved-call"}},
			},
		},
	})
	carrier := &fakeCallbackCarrier{session: sess}

	if !IsSavedPendingToolCall(carrier, &llmschema.ToolCall{ID: "saved-call"}) {
		t.Error("matching saved call should be detected as replay")
	}
	if IsSavedPendingToolCall(carrier, &llmschema.ToolCall{ID: "other-call"}) {
		t.Error("non-matching call should not be replay")
	}
}

// TestIsSavedPendingToolCall_空状态 验证状态为空时返回 false。
func TestIsSavedPendingToolCall_空状态(t *testing.T) {
	sess := session.NewSession()
	carrier := &fakeCallbackCarrier{session: sess}
	if IsSavedPendingToolCall(carrier, &llmschema.ToolCall{ID: "c1"}) {
		t.Error("empty state should not be replay")
	}
}

// TestIsSavedPendingToolCall_类型不符 验证状态类型异常时返回 false。
func TestIsSavedPendingToolCall_类型不符(t *testing.T) {
	sess := session.NewSession()
	sess.UpdateState(map[string]any{saschema.InterruptionKey: "not a state"})
	carrier := &fakeCallbackCarrier{session: sess}
	if IsSavedPendingToolCall(carrier, &llmschema.ToolCall{ID: "c1"}) {
		t.Error("wrong state type should not be replay")
	}
}

// fakeCallbackCarrier 提供 Session() 的测试载体。
type fakeCallbackCarrier struct {
	// session 会话门面
	session sessioninterfaces.SessionFacade
}

// Session 实现载体接口
func (f *fakeCallbackCarrier) Session() sessioninterfaces.SessionFacade { return f.session }

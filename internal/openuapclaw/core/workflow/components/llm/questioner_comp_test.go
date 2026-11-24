package llm

import (
	"strings"

	llmschema "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/foundation/llm/schema"
	"testing"
)

// TestQuestionerDefaultTemplate 双语默认模板选择（Python :91-101）
func TestQuestionerDefaultTemplate(t *testing.T) {
	zh := QuestionerDefaultTemplate("zh")
	if len(zh) != 2 || zh[0].GetRole() != llmschema.RoleTypeSystem || zh[1].GetRole() != llmschema.RoleTypeUser {
		t.Fatalf("zh template roles wrong")
	}
	if !strings.Contains(zh[0].GetContent().String(), "信息收集助手") {
		t.Fatalf("zh system template content mismatch")
	}
	en := QuestionerDefaultTemplate("en")
	if !strings.Contains(en[0].GetContent().String(), "information collection assistant") {
		t.Fatalf("en system template content mismatch")
	}
	// 非 en 回落中文（Python :98-101 仅判 'en'）
	other := QuestionerDefaultTemplate("fr")
	if !strings.Contains(other[0].GetContent().String(), "信息收集助手") {
		t.Fatalf("non-en should fallback zh")
	}
}

// TestQuestionerConfigDefaults 配置默认值（Python dataclass :130-143）
func TestQuestionerConfigDefaults(t *testing.T) {
	cfg := newQuestionerConfigDefaults(&QuestionerConfig{})
	if cfg.ResponseType != ResponseTypeReplyDirectly || cfg.MaxResponse != 3 ||
		cfg.ChatHistoryMaxRounds != 5 || cfg.AcceptLanguage != "zh" ||
		cfg.IfExtractFields() != true {
		t.Fatalf("config defaults mismatch Python :130-143: %+v", cfg)
	}
}

// TestQuestionerDefaultConfigFromLanguage from_language（Python :150-152）
func TestQuestionerDefaultConfigFromLanguage(t *testing.T) {
	d := questionerDefaultConfigFromLanguage("en")
	if len(d.promptTemplate) != 2 || !strings.Contains(d.promptTemplate[0].GetContent().String(), "collection assistant") {
		t.Fatalf("from_language(en) mismatch")
	}
}

// TestQuestionerStateHandleEvent 状态分派（Python :187-251 四类合流）
func TestQuestionerStateHandleEvent(t *testing.T) {
	st := NewQuestionerState()
	if !st.IsFreshState() {
		t.Fatal("fresh state expected")
	}
	st2 := st.HandleEvent(QuestionerEventUserInteract)
	if st2.Status != QuestionerStatusUserInteract {
		t.Fatal("want interact")
	}
	if !st2.IsUndergoingInteraction() {
		t.Fatal("undergoing interaction")
	}
	// USER_INTERACT 只响应 END（Python :231-234）
	if got := st2.HandleEvent(QuestionerEventUserInteract); got.Status != QuestionerStatusUserInteract {
		t.Fatal("interact should stay")
	}
	// START 事件对 Interact 态无效（Python :213-217 只认 END）
	if got := st2.HandleEvent(QuestionerEventStart); got.Status != QuestionerStatusUserInteract {
		t.Fatal("interact ignores START")
	}
	st3 := st2.HandleEvent(QuestionerEventEnd)
	if st3.Status != QuestionerStatusEnd {
		t.Fatal("want end")
	}
	// END + START 回环新建（Python :249-250）
	st4 := st3.HandleEvent(QuestionerEventStart)
	if st4.Status != QuestionerStatusStart || st4.ResponseNum != 0 {
		t.Fatal("loop back to new start")
	}
	// START 态对 START 事件无变化
	if got := NewQuestionerState().HandleEvent(QuestionerEventStart); got.Status != QuestionerStatusStart {
		t.Fatal("start stays")
	}
}

// TestQuestionerStateSerializeRoundTrip 序列化往返（Python :179-185）
func TestQuestionerStateSerializeRoundTrip(t *testing.T) {
	st := NewQuestionerState()
	st.ResponseNum = 2
	st.ExtractedKeyFields = map[string]any{"name": "张三"}
	st.Question = "请您提供身份证号相关的信息"
	st = st.HandleEvent(QuestionerEventUserInteract)
	raw := st.Serialize()
	got, err := QuestionerStateDeserialize(raw)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != QuestionerStatusUserInteract || got.ResponseNum != 2 ||
		got.ExtractedKeyFields["name"] != "张三" || got.Question != "请您提供身份证号相关的信息" {
		t.Fatalf("roundtrip mismatch: %+v", got)
	}
}

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

// TestQuestionerFormatTemplate 模板渲染（Python :255-265，含 D-24-1 缺键怪癖）
func TestQuestionerFormatTemplate(t *testing.T) {
	got := questionerFormatTemplate("您好 {{name}}，{{age}}岁", map[string]any{"name": "张三", "age": 18})
	if got != "您好 张三，18岁" {
		t.Fatalf("got %q", got)
	}
	got = questionerFormatTemplate("{{missing}}", map[string]any{})
	if got != "None" {
		t.Fatalf("D-24-1 quirk: got %q", got)
	}
}

// TestQuestionerFormatContinueAsk 追问语句（Python :267-277）
func TestQuestionerFormatContinueAsk(t *testing.T) {
	fields := []QuestionerFieldInfo{
		{FieldName: "id_card", CnFieldName: "身份证号", Description: "身份证"},
		{FieldName: "addr", Description: "住址"},
	}
	if got := questionerFormatContinueAsk(fields, "zh"); got != "请您提供身份证号, 住址相关的信息" {
		t.Fatalf("zh: %q", got)
	}
	if got := questionerFormatContinueAsk(fields, "en"); got != "Please provide information related to: 身份证号, 住址" {
		t.Fatalf("en: %q", got)
	}
}

// TestQuestionerIsValidValue 有效值判定（Python :297-306）
func TestQuestionerIsValidValue(t *testing.T) {
	invalid := []any{nil, "", map[string]any{}, []any{}, "null", " NONE "}
	for _, v := range invalid {
		if questionerIsValidValue(v) {
			t.Errorf("%v should be invalid", v)
		}
	}
	valid := []any{false, int64(0), 0.0, "0", "abc", map[string]any{"k": 1}}
	for _, v := range valid {
		if !questionerIsValidValue(v) {
			t.Errorf("%v should be valid", v)
		}
	}
}

// TestQuestionerFormatOutputExcludeDefaults 输出 exclude_defaults（Python :279-284）
func TestQuestionerFormatOutputExcludeDefaults(t *testing.T) {
	cache := &QuestionerOutputCache{
		UserResponse: "",
		Question:     "q1",
		KeyFields:    map[string]any{"name": "张三"},
	}
	out := questionerFormatQuestionerOutput(cache)
	if _, ok := out["user_response"]; ok {
		t.Fatal("user_response should be excluded (default)")
	}
	if out["question"] != "q1" || out["name"] != "张三" {
		t.Fatalf("out=%v", out)
	}
	empty := questionerFormatQuestionerOutput(&QuestionerOutputCache{KeyFields: map[string]any{}})
	if len(empty) != 0 {
		t.Fatalf("want empty map, got %v", empty)
	}
	// 非 "" 的 user_response 保留（dict 形态）
	dictOut := questionerFormatQuestionerOutput(&QuestionerOutputCache{
		UserResponse: map[string]any{"k": 1}, KeyFields: map[string]any{}})
	if dictOut["user_response"] == nil {
		t.Fatal("dict user_response should be kept")
	}
}

// TestQuestionerValidateInputs 输入校验（Python :286-295，D-24-3 三态）
func TestQuestionerValidateInputs(t *testing.T) {
	// 缺 query 回落 ""
	m, err := questionerValidateInputs(map[string]any{})
	if err != nil || m["query"] != "" {
		t.Fatalf("default query: %v %v", m, err)
	}
	// str / dict / nil 均合法
	if _, err = questionerValidateInputs(map[string]any{"query": "x"}); err != nil {
		t.Fatal(err)
	}
	if _, err = questionerValidateInputs(map[string]any{"query": map[string]any{"k": 1}}); err != nil {
		t.Fatal(err)
	}
	if _, err = questionerValidateInputs(map[string]any{"query": nil}); err != nil {
		t.Fatal(err)
	}
	// 非法类型 → 101070
	_, err = questionerValidateInputs(map[string]any{"query": 42})
	if err == nil {
		t.Fatal("want 101070")
	}
}

// TestQuestionerValidateAndConvertTypeDelegation pythonsem 委托（D-24-2）
func TestQuestionerValidateAndConvertTypeDelegation(t *testing.T) {
	if v, ok := questionerValidateAndConvertType(true, "integer"); ok || v != nil {
		t.Fatal("bool≠int 边界失败")
	}
	if v, ok := questionerValidateAndConvertType("7", "integer"); !ok || v != int64(7) {
		t.Fatalf("got %v %v", v, ok)
	}
}

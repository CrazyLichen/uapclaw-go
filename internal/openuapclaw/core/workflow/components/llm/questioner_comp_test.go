package llm

import (
	"context"
	"fmt"
	"strings"
	"testing"

	modelclients "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/foundation/llm/model_clients"
	llmschema "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/foundation/llm/schema"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/session/state"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/session"
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
	empty := questionerFormatQuestionerOutput(newQuestionerOutputCache())
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

// TestQuestionerValidateConfig 配置校验三分支（Python :768-797）
func TestQuestionerValidateConfig(t *testing.T) {
	if err := validateQuestionerResponseType("bad"); err == nil {
		t.Fatal("response_type 非法应报 101071")
	}
	if err := validateQuestionerResponseType(ResponseTypeReplyDirectly); err != nil {
		t.Fatal(err)
	}
	if err := validateQuestionerExtractKeyFields(true, nil); err == nil {
		t.Fatal("extract_fields 且字段表空应报 101071")
	}
	if err := validateQuestionerExtractKeyFields(true, []QuestionerFieldInfo{{Description: "x"}}); err == nil {
		t.Fatal("字段名空应报 101071")
	}
	if err := validateQuestionerExtractKeyFields(true, []QuestionerFieldInfo{{FieldName: "q"}}); err != nil {
		t.Fatal(err)
	}
	if err := validateQuestionerExtractKeyFields(false, nil); err != nil {
		t.Fatal(err)
	}
	if err := validateQuestionerMaxResponse(0); err == nil {
		t.Fatal("max_response<=0 应报 101071")
	}
	if err := validateQuestionerMaxResponse(3); err != nil {
		t.Fatal(err)
	}
}

// TestQuestionerLoadStoreState 会话状态存取（Python :755-766）。
// Python 语义实证：state().update() 进 staged _updates，get() 只读已提交
// _state（base.py:186-189），同 invoke 内 store 后 load 不可见——恢复可见性由
// WaitUserInputs 的 commitCMP 保证（interaction.go:128）。Go 同构。
func TestQuestionerLoadStoreState(t *testing.T) {
	sess := newTestNodeSession(t)
	st := loadQuestionerStateFromSession(sess)
	if st.Status != QuestionerStatusStart {
		t.Fatal("want fresh")
	}
	st = st.HandleEvent(QuestionerEventUserInteract)
	st.Question = "q"
	storeQuestionerStateToSession(st, sess) // staged，不报错即通过
	// staged 不可见：同 invoke 内 load 回 START 新建（Python get() 同语义）
	got := loadQuestionerStateFromSession(sess)
	if got.Status != QuestionerStatusStart {
		t.Fatalf("staged should be invisible until commit: %+v", got)
	}
}

// TestQuestionerExecutableConstruction 构造校验链（Python __init__ :745-753）
func TestQuestionerExecutableConstruction(t *testing.T) {
	if _, err := NewQuestionerExecutable(&QuestionerConfig{ResponseType: "bad"}); err == nil {
		t.Fatal("want config error")
	}
	// 空配置（extract 默认 True 且字段表空）构造必失败——Python __init__ 同语义
	if _, err := NewQuestionerExecutable(&QuestionerConfig{}); err == nil {
		t.Fatal("empty config should fail 101071 (Python :777-782)")
	}
	e, err := NewQuestionerExecutable(&QuestionerConfig{ExtractFieldsFromResponse: boolPtr(false)})
	if err != nil {
		t.Fatal(err)
	}
	if e.cfg.MaxResponse != 3 || e.cfg.AcceptLanguage != "zh" || e.promptTemplate == nil {
		t.Fatalf("defaults not applied: %+v", e.cfg)
	}
	if e.initialized {
		t.Fatal("should start uninitialized")
	}
}

// TestQuestionerCreateLLMInstance 双路径（Python :831-841 + 8.23 D-23-3 防御）
func TestQuestionerCreateLLMInstance(t *testing.T) {
	// 皆缺 → 101075
	e, errC := NewQuestionerExecutable(&QuestionerConfig{ExtractFieldsFromResponse: boolPtr(false)})
	if errC != nil {
		t.Fatal(errC)
	}
	_, err := e.createLLMInstance(context.Background())
	if err == nil {
		t.Fatal("want 101075")
	}
	// ModelID 路径且 mgr==nil → 错误（8.23 D-23-3 同款防御）
	mid := "some-model"
	e3, _ := NewQuestionerExecutable(&QuestionerConfig{ModelID: &mid, ExtractFieldsFromResponse: boolPtr(false)})
	if _, err := e3.createLLMInstance(context.Background()); err == nil {
		t.Fatal("mgr==nil should error")
	}
}

// boolPtr *bool 便捷构造（D-24-9）
func boolPtr(b bool) *bool { return &b }

// fakeQuestionerLLM LLMInvoker 替身（仅网络边界，feedback-real-chain-testing 允许）
type fakeQuestionerLLM struct{ content string }

func (f *fakeQuestionerLLM) Invoke(ctx context.Context, msgs modelclients.MessagesParam,
	opts ...modelclients.InvokeOption) (*llmschema.AssistantMessage, error) {
	return llmschema.NewAssistantMessage(f.content), nil
}
func (f *fakeQuestionerLLM) Stream(ctx context.Context, msgs modelclients.MessagesParam,
	opts ...modelclients.StreamOption) (<-chan *llmschema.AssistantMessageChunk, error) {
	return nil, fmt.Errorf("stream not supported in fake")
}

// newTestHandler 组装 handler（真实 session 管线 + fake LLM 边界）
func newTestHandler(t *testing.T, cfg *QuestionerConfig, content string, st *QuestionerState) (*questionerDirectReplyHandler, *session.NodeSessionFacade) {
	t.Helper()
	e, err := NewQuestionerExecutable(cfg)
	if err != nil {
		t.Fatal(err)
	}
	e.llModel = &fakeQuestionerLLM{content: content}
	e.initialized = true
	sess := newTestNodeSession(t)
	return newQuestionerDirectReplyHandler(e.cfg, e.llModel, st, e.promptTemplate), sess
}

// TestQuestionerHandlerStartWithQuestionContent START 态 question_content 分支（Python :425-433）
func TestQuestionerHandlerStartWithQuestionContent(t *testing.T) {
	cfg := &QuestionerConfig{QuestionContent: "您好 {{name}}", ExtractFieldsFromResponse: boolPtr(false)}
	h, sess := newTestHandler(t, cfg, "", NewQuestionerState())
	result, err := h.handle(context.Background(),
		map[string]any{"query": "", "name": "张三"}, sess, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result["question"] != "您好 张三" {
		t.Fatalf("question: %v", result)
	}
	if h.state.Status != QuestionerStatusUserInteract {
		t.Fatal("should be interact (挂起)")
	}
}

// TestQuestionerHandlerExtractionComplete 抽取齐 → END（Python :435-453）
func TestQuestionerHandlerExtractionComplete(t *testing.T) {
	cfg := &QuestionerConfig{
		ExtractFieldsFromResponse: boolPtr(true),
		FieldNames: []QuestionerFieldInfo{
			{FieldName: "name", Description: "姓名", Type: "string", Required: true},
		},
	}
	h, sess := newTestHandler(t, cfg, `{"name": "张三"}`, NewQuestionerState())
	result, err := h.handle(context.Background(),
		map[string]any{"query": "我叫张三"}, sess, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result["name"] != "张三" {
		t.Fatalf("extracted: %v", result)
	}
	if h.state.Status != QuestionerStatusEnd {
		t.Fatal("want END")
	}
}

// TestQuestionerHandlerExtractionContinueAsk 必填缺失 → 追问挂起 + key_fields 清空（Python :684-699）
func TestQuestionerHandlerExtractionContinueAsk(t *testing.T) {
	cfg := &QuestionerConfig{
		ExtractFieldsFromResponse: boolPtr(true),
		FieldNames: []QuestionerFieldInfo{
			{FieldName: "name", Description: "姓名", Required: true},
			{FieldName: "id", CnFieldName: "身份证号", Description: "证件", Required: true},
		},
	}
	h, sess := newTestHandler(t, cfg, `{"name": "张三"}`, NewQuestionerState())
	result, err := h.handle(context.Background(), map[string]any{"query": "我叫张三"}, sess, nil)
	if err != nil {
		t.Fatal(err)
	}
	if h.state.Status != QuestionerStatusUserInteract {
		t.Fatal("want interact")
	}
	if result["question"] != "请您提供身份证号相关的信息" {
		t.Fatalf("ask: %v", result["question"])
	}
	if _, ok := result["name"]; ok {
		t.Fatal("key_fields should be cleared on continue-ask")
	}
}

// TestQuestionerHandlerMaxResponseExceeded 超限 → 101074（Python :693-697）
func TestQuestionerHandlerMaxResponseExceeded(t *testing.T) {
	cfg := &QuestionerConfig{
		ExtractFieldsFromResponse: boolPtr(true), MaxResponse: 1,
		FieldNames: []QuestionerFieldInfo{{FieldName: "id", CnFieldName: "身份证号", Description: "证件", Required: true}},
	}
	e, err := NewQuestionerExecutable(cfg)
	if err != nil {
		t.Fatal(err)
	}
	e.llModel = &fakeQuestionerLLM{content: `{}`}
	e.initialized = true
	// response_num=0 + 1 条待用输入：interact 消费后 increment → response_num=1
	// == max_response，再次缺必填 → 101074（Python :681-697 语义链实证）
	st := NewQuestionerState()
	st = st.HandleEvent(QuestionerEventUserInteract)
	h := newQuestionerDirectReplyHandler(e.cfg, e.llModel, st, e.promptTemplate)
	sess := newTestNodeSession(t)
	// 模拟恢复场景：注入待用交互输入（Python INTERACTIVE_INPUT 经 checkpoint recover
	// 写入；此处直写 session state 并提交以触发重放）
	ws, ok := sess.InnerNodeSession().State().(*state.WorkflowCommitState)
	if !ok {
		t.Fatalf("state type %T not WorkflowCommitState", sess.InnerNodeSession().State())
	}
	ws.UpdateAndCommitWorkflowState(map[string]any{"__interactive_input__": []any{"回答"}})
	_, err = h.handle(context.Background(), map[string]any{"query": ""}, sess, nil)
	if err == nil {
		t.Fatal("want 101074")
	}
}

// TestQuestionerHandlerJSONParseFailure 解析失败返空 dict 继续追问（Python :596-603）
func TestQuestionerHandlerJSONParseFailure(t *testing.T) {
	cfg := &QuestionerConfig{
		ExtractFieldsFromResponse: boolPtr(true),
		FieldNames: []QuestionerFieldInfo{{FieldName: "q", Description: "问题", Required: true}},
	}
	h, sess := newTestHandler(t, cfg, "这不是 JSON", NewQuestionerState())
	result, err := h.handle(context.Background(), map[string]any{"query": "x"}, sess, nil)
	if err != nil {
		t.Fatal(err)
	}
	if h.state.Status != QuestionerStatusUserInteract {
		t.Fatal("parse failure → 视为未抽取 → 追问挂起")
	}
	if result["question"] != "请您提供问题相关的信息" {
		t.Fatalf("ask: %v", result["question"])
	}
}

// TestQuestionerHandlerJSONFenceStripping 剥壳（Python :593-594）
func TestQuestionerHandlerJSONFenceStripping(t *testing.T) {
	cfg := &QuestionerConfig{
		ExtractFieldsFromResponse: boolPtr(true),
		FieldNames: []QuestionerFieldInfo{{FieldName: "name", Description: "姓名", Required: true}},
	}
	h, sess := newTestHandler(t, cfg, "```json\n{\"name\": \"张三\"}\n```", NewQuestionerState())
	result, err := h.handle(context.Background(), map[string]any{"query": "x"}, sess, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result["name"] != "张三" {
		t.Fatalf("fence stripped: %v", result)
	}
}

// TestQuestionerHandlerTypeConversionValidation 类型校验转换（Python :616-654）
func TestQuestionerHandlerTypeConversionValidation(t *testing.T) {
	cfg := &QuestionerConfig{
		ExtractFieldsFromResponse: boolPtr(true),
		FieldNames: []QuestionerFieldInfo{
			{FieldName: "age", Description: "年龄", Type: "integer", Required: false},
			{FieldName: "score", Description: "分数", Type: "number", Required: false},
			{FieldName: "active", Description: "激活", Type: "boolean", Required: false},
		},
	}
	h, sess := newTestHandler(t, cfg, `{"age": "30", "score": 3.5, "active": "true", "bad_int": "abc"}`, NewQuestionerState())
	result, err := h.handle(context.Background(), map[string]any{"query": "x"}, sess, nil)
	if err != nil {
		t.Fatal(err)
	}
	// "30"→int64(30)；3.5→float64；"true"→true；"abc" integer 转换失败剔除
	if result["age"] != int64(30) || result["score"] != 3.5 || result["active"] != true {
		t.Fatalf("converted: %v", result)
	}
	if _, ok := result["bad_int"]; ok {
		t.Fatal("bad_int should be dropped")
	}
	// bool≠int：LLM 返回 bool 给 integer 字段 → 剔除
	h2, sess2 := newTestHandler(t, cfg, `{"age": true}`, NewQuestionerState())
	result2, _ := h2.handle(context.Background(), map[string]any{"query": "x"}, sess2, nil)
	if _, ok := result2["age"]; ok {
		t.Fatal("bool for integer field should be dropped")
	}
}

// TestQuestionerHandlerDefaultValueFallback 默认值兜底（Python :668-676）
func TestQuestionerHandlerDefaultValueFallback(t *testing.T) {
	cfg := &QuestionerConfig{
		ExtractFieldsFromResponse: boolPtr(true),
		FieldNames: []QuestionerFieldInfo{
			{FieldName: "city", Description: "城市", Required: false, DefaultValue: "北京"},
		},
	}
	h, sess := newTestHandler(t, cfg, `{}`, NewQuestionerState())
	result, err := h.handle(context.Background(), map[string]any{"query": "x"}, sess, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result["city"] != "北京" {
		t.Fatalf("default: %v", result)
	}
}

// TestQuestionerHandlerUnknownFieldStringFallback 未知键回落 string（Python :635）
func TestQuestionerHandlerUnknownFieldStringFallback(t *testing.T) {
	cfg := &QuestionerConfig{
		ExtractFieldsFromResponse: boolPtr(true),
		FieldNames: []QuestionerFieldInfo{{FieldName: "name", Description: "姓名", Required: true}},
	}
	h, sess := newTestHandler(t, cfg, `{"name": "张三", "unknown_num": 42}`, NewQuestionerState())
	result, err := h.handle(context.Background(), map[string]any{"query": "x"}, sess, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result["unknown_num"] != "42" {
		t.Fatalf("unknown key should fall back to string: %v", result["unknown_num"])
	}
}

// TestQuestionerHandlerInputInvalid 两者皆缺 → 101072（Python :449-452）
func TestQuestionerHandlerInputInvalid(t *testing.T) {
	cfg := &QuestionerConfig{ExtractFieldsFromResponse: boolPtr(false)}
	h, sess := newTestHandler(t, cfg, "", NewQuestionerState())
	_, err := h.handle(context.Background(), map[string]any{"query": "x"}, sess, nil)
	if err == nil {
		t.Fatal("want 101072")
	}
}

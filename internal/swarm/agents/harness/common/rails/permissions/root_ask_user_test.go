package permissions

import (
	"strings"
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/swarm/agents/harness/common/rails"
)

// ──────────────────────────── 测试辅助 ────────────────────────────

// askUserDecisionContext 构造一个测试用决策上下文
func askUserDecisionContext(t *testing.T) *RootDecisionContext {
	t.Helper()
	turn, err := NewRootIntentTurn("req-1", RootIntentTurnKindFresh, "帮我改配置", nil)
	if err != nil {
		t.Fatalf("构造轮次失败: %v", err)
	}
	dc, err := NewRootDecisionContext("sess-1", "req-1", "web", []RootIntentTurn{turn}, "")
	if err != nil {
		t.Fatalf("构造上下文失败: %v", err)
	}
	return dc
}

// askUserArgs 构造一份合法的 ask_user 工具参数
func askUserArgs(questions ...map[string]any) map[string]any {
	list := make([]any, 0, len(questions))
	for _, question := range questions {
		list = append(list, question)
	}
	return map[string]any{"questions": list}
}

// simpleQuestion 构造一条单问题
func simpleQuestion(text string) map[string]any {
	return map[string]any{"question": text}
}

// ──────────────────────────── AskUserQuestionReference 测试 ────────────────────────────

// TestNewAskUserQuestionReference_正常 验证合法构造
func TestNewAskUserQuestionReference_正常(t *testing.T) {
	opt, _ := NewRootAskUserOption("A", "", "")
	ref, err := NewAskUserQuestionReference("问题", []RootAskUserOption{opt}, true)
	if err != nil {
		t.Fatalf("不应报错: %v", err)
	}
	if ref.Question != "问题" || !ref.MultiSelect || len(ref.Options) != 1 {
		t.Fatalf("字段不符: %+v", ref)
	}
}

// TestNewAskUserQuestionReference_空问题报错 验证空问题报错
func TestNewAskUserQuestionReference_空问题报错(t *testing.T) {
	if _, err := NewAskUserQuestionReference("  ", nil, false); err == nil {
		t.Fatalf("空问题应报错")
	}
}

// TestAskUserQuestionReference_往返 验证序列化往返一致
func TestAskUserQuestionReference_往返(t *testing.T) {
	opt, _ := NewRootAskUserOption("A", "d", "p")
	ref, _ := NewAskUserQuestionReference("问题", []RootAskUserOption{opt}, true)
	back, err := AskUserQuestionReferenceFromMapping(ref.ToMapping())
	if err != nil {
		t.Fatalf("反序列化失败: %v", err)
	}
	if back.Question != ref.Question || back.MultiSelect != ref.MultiSelect ||
		len(back.Options) != 1 || back.Options[0].Label != "A" {
		t.Fatalf("往返不一致: %+v", back)
	}
}

// TestAskUserQuestionReferenceFromMapping_异常 验证各类非法输入
func TestAskUserQuestionReferenceFromMapping_异常(t *testing.T) {
	if _, err := AskUserQuestionReferenceFromMapping(nil); err == nil {
		t.Fatalf("nil 应报错")
	}
	if _, err := AskUserQuestionReferenceFromMapping(map[string]any{"question": "q"}); err == nil {
		t.Fatalf("缺字段应报错")
	}
	// 空问题
	if _, err := AskUserQuestionReferenceFromMapping(map[string]any{
		"question": "  ", "options": []any{}, "multi_select": false,
	}); err == nil {
		t.Fatalf("空问题应报错")
	}
	// options 非列表
	if _, err := AskUserQuestionReferenceFromMapping(map[string]any{
		"question": "q", "options": "x", "multi_select": false,
	}); err == nil {
		t.Fatalf("options 非列表应报错")
	}
	// multi_select 非布尔
	if _, err := AskUserQuestionReferenceFromMapping(map[string]any{
		"question": "q", "options": []any{}, "multi_select": "yes",
	}); err == nil {
		t.Fatalf("multi_select 非布尔应报错")
	}
	// option 项非映射
	if _, err := AskUserQuestionReferenceFromMapping(map[string]any{
		"question": "q", "options": []any{"x"}, "multi_select": false,
	}); err == nil {
		t.Fatalf("option 非映射应报错")
	}
	// option 非法
	if _, err := AskUserQuestionReferenceFromMapping(map[string]any{
		"question":     "q",
		"options":      []any{map[string]any{"label": ""}},
		"multi_select": false,
	}); err == nil {
		t.Fatalf("option 非法应报错")
	}
}

// ──────────────────────────── PutAskUserResumeInInputs 测试 ────────────────────────────

// TestPutAskUserResumeInInputs_写入 验证写入 run.context.extra
func TestPutAskUserResumeInInputs_写入(t *testing.T) {
	prepared := &RootAskUserResume{CapacityReason: ""}
	out := PutAskUserResumeInInputs(map[string]any{"other": 1}, prepared)

	run := out["run"].(map[string]any)
	contextMap := run["context"].(map[string]any)
	extra := contextMap["extra"].(map[string]any)
	if extra[AskUserResumeDTOKey] != prepared {
		t.Fatalf("应写入 prepared")
	}
	if out["other"] != 1 {
		t.Fatalf("原有字段应保留")
	}
}

// TestPutAskUserResumeInInputs_nil清除 验证 prepared 为 nil 时只清除旧值
func TestPutAskUserResumeInInputs_nil清除(t *testing.T) {
	inputs := map[string]any{
		"run": map[string]any{
			"context": map[string]any{
				"extra": map[string]any{AskUserResumeDTOKey: "stale"},
			},
		},
	}
	out := PutAskUserResumeInInputs(inputs, nil)
	extra := out["run"].(map[string]any)["context"].(map[string]any)["extra"].(map[string]any)
	if _, exists := extra[AskUserResumeDTOKey]; exists {
		t.Fatalf("应已清除")
	}
}

// TestPutAskUserResumeInInputs_不修改入参 验证不修改原始映射
func TestPutAskUserResumeInInputs_不修改入参(t *testing.T) {
	inputs := map[string]any{"run": map[string]any{"context": map[string]any{"extra": map[string]any{}}}}
	_ = PutAskUserResumeInInputs(inputs, &RootAskUserResume{})
	extra := inputs["run"].(map[string]any)["context"].(map[string]any)["extra"].(map[string]any)
	if _, exists := extra[AskUserResumeDTOKey]; exists {
		t.Fatalf("不应修改入参")
	}
}

// TestPutAskUserResumeInInputs_空输入 验证空映射也能构造层级
func TestPutAskUserResumeInInputs_空输入(t *testing.T) {
	out := PutAskUserResumeInInputs(nil, &RootAskUserResume{})
	run := out["run"].(map[string]any)
	contextMap := run["context"].(map[string]any)
	extra := contextMap["extra"].(map[string]any)
	if _, exists := extra[AskUserResumeDTOKey]; !exists {
		t.Fatalf("应写入")
	}
}

// ──────────────────────────── BuildAskUserMetadata 测试 ────────────────────────────

// TestBuildAskUserMetadata_正常 验证正常构造出 metadata
func TestBuildAskUserMetadata_正常(t *testing.T) {
	got := BuildAskUserMetadata(
		askUserDecisionContext(t),
		AskUserToolName,
		"call-1",
		askUserArgs(simpleQuestion("哪个配置？")),
	)
	if got == nil {
		t.Fatalf("应返回 metadata")
	}
	if got["tool_call_id"] != "call-1" {
		t.Fatalf("tool_call_id 不符: %v", got["tool_call_id"])
	}
	questions := got["questions"].([]any)
	if len(questions) != 1 {
		t.Fatalf("问题数不符")
	}
	if _, ok := got["context"].(map[string]any); !ok {
		t.Fatalf("context 应序列化为映射")
	}
}

// TestBuildAskUserMetadata_工具名不符 验证非 ask_user 返回 nil
func TestBuildAskUserMetadata_工具名不符(t *testing.T) {
	got := BuildAskUserMetadata(askUserDecisionContext(t), "other_tool", "call-1", askUserArgs(simpleQuestion("q")))
	if got != nil {
		t.Fatalf("非 ask_user 应返回 nil")
	}
}

// TestBuildAskUserMetadata_缺callID 验证 tool_call_id 为空返回 nil
func TestBuildAskUserMetadata_缺callID(t *testing.T) {
	got := BuildAskUserMetadata(askUserDecisionContext(t), AskUserToolName, "  ", askUserArgs(simpleQuestion("q")))
	if got != nil {
		t.Fatalf("空 call id 应返回 nil")
	}
}

// TestBuildAskUserMetadata_callID超长 验证超过 512 返回 nil
func TestBuildAskUserMetadata_callID超长(t *testing.T) {
	long := strings.Repeat("a", 513)
	got := BuildAskUserMetadata(askUserDecisionContext(t), AskUserToolName, long, askUserArgs(simpleQuestion("q")))
	if got != nil {
		t.Fatalf("超长 call id 应返回 nil")
	}
}

// TestBuildAskUserMetadata_无问题 验证解析不出问题返回 nil
func TestBuildAskUserMetadata_无问题(t *testing.T) {
	got := BuildAskUserMetadata(askUserDecisionContext(t), AskUserToolName, "call-1", map[string]any{})
	if got != nil {
		t.Fatalf("无问题应返回 nil")
	}
}

// TestBuildAskUserMetadata_非字符串callID 验证非字符串 call id 返回 nil
func TestBuildAskUserMetadata_非字符串callID(t *testing.T) {
	got := BuildAskUserMetadata(askUserDecisionContext(t), AskUserToolName, 123, askUserArgs(simpleQuestion("q")))
	if got != nil {
		t.Fatalf("非字符串 call id 应返回 nil")
	}
}

// ──────────────────────────── questionsOf 测试 ────────────────────────────

// TestQuestionsOf_字符串JSON 验证字符串参数被解析
func TestQuestionsOf_字符串JSON(t *testing.T) {
	got, err := questionsOf(`{"questions":[{"question":"q1"}]}`)
	if err != nil || len(got) != 1 || got[0].Question != "q1" {
		t.Fatalf("解析失败: %v %v", got, err)
	}
}

// TestQuestionsOf_非法JSON 验证非法 JSON 返回空
func TestQuestionsOf_非法JSON(t *testing.T) {
	got, err := questionsOf("{not json")
	if err != nil || len(got) != 0 {
		t.Fatalf("非法 JSON 应返回空")
	}
}

// TestQuestionsOf_回退query 验证无 questions 时回退到 query
func TestQuestionsOf_回退query(t *testing.T) {
	got, err := questionsOf(map[string]any{"query": "问我点什么"})
	if err != nil || len(got) != 1 || got[0].Question != "问我点什么" {
		t.Fatalf("回退失败: %v", got)
	}
	// query 为空
	if got, _ := questionsOf(map[string]any{"query": "  "}); len(got) != 0 {
		t.Fatalf("空 query 应返回空")
	}
	// 无 query
	if got, _ := questionsOf(map[string]any{}); len(got) != 0 {
		t.Fatalf("无 query 应返回空")
	}
}

// TestQuestionsOf_问题数量越界 验证 0 条与超过 4 条返回空
func TestQuestionsOf_问题数量越界(t *testing.T) {
	if got, _ := questionsOf(map[string]any{"questions": []any{}}); len(got) != 0 {
		t.Fatalf("0 条应返回空")
	}
	tooMany := make([]any, rails.MaxStructuredQuestions+1)
	for i := range tooMany {
		tooMany[i] = simpleQuestion("q")
	}
	if got, _ := questionsOf(map[string]any{"questions": tooMany}); len(got) != 0 {
		t.Fatalf("超上限应返回空")
	}
	// questions 非列表
	if got, _ := questionsOf(map[string]any{"questions": "x"}); len(got) != 0 {
		t.Fatalf("非列表应返回空")
	}
}

// TestQuestionsOf_问题项非法 验证各项非法形态
func TestQuestionsOf_问题项非法(t *testing.T) {
	cases := []map[string]any{
		{"questions": []any{"not-a-map"}},
		{"questions": []any{map[string]any{"question": ""}}},
		{"questions": []any{map[string]any{"question": 123}}},
		{"questions": []any{map[string]any{"question": "q", "multi_select": "yes"}}},
		{"questions": []any{map[string]any{"question": "q", "options": "x"}}},
		{"questions": []any{map[string]any{"question": "q", "options": []any{"x"}}}},
		// 重复问题
		{"questions": []any{map[string]any{"question": "same"}, map[string]any{"question": "same"}}},
	}
	for i, c := range cases {
		if got, _ := questionsOf(c); len(got) != 0 {
			t.Fatalf("case %d 应返回空，实际 %v", i, got)
		}
	}
}

// TestQuestionsOf_非字符串非映射 验证其它类型返回空
func TestQuestionsOf_非字符串非映射(t *testing.T) {
	if got, _ := questionsOf(123); len(got) != 0 {
		t.Fatalf("数字应返回空")
	}
	if got, _ := questionsOf(nil); len(got) != 0 {
		t.Fatalf("nil 应返回空")
	}
}

// ──────────────────────────── optionsOf 测试 ────────────────────────────

// TestOptionsOf_空与nil 验证 nil 与空列表都合法
func TestOptionsOf_空与nil(t *testing.T) {
	if got, ok := optionsOf(nil); !ok || len(got) != 0 {
		t.Fatalf("nil 应合法且为空")
	}
	if got, ok := optionsOf([]any{}); !ok || len(got) != 0 {
		t.Fatalf("空列表应合法且为空")
	}
}

// TestOptionsOf_两到四 验证 2-4 个合法
func TestOptionsOf_两到四(t *testing.T) {
	for n := 2; n <= 4; n++ {
		list := make([]any, 0, n)
		for i := 0; i < n; i++ {
			list = append(list, map[string]any{"label": string(rune('A' + i))})
		}
		if _, ok := optionsOf(list); !ok {
			t.Fatalf("n=%d 应合法", n)
		}
	}
}

// TestOptionsOf_非法数量 验证 1 个与 5 个不合法
func TestOptionsOf_非法数量(t *testing.T) {
	if _, ok := optionsOf([]any{map[string]any{"label": "A"}}); ok {
		t.Fatalf("1 个应不合法")
	}
	five := make([]any, 5)
	for i := range five {
		five[i] = map[string]any{"label": string(rune('A' + i))}
	}
	if _, ok := optionsOf(five); ok {
		t.Fatalf("5 个应不合法")
	}
}

// TestOptionsOf_项非法 验证各项非法形态
func TestOptionsOf_项非法(t *testing.T) {
	cases := []any{
		"not-a-list",
		[]any{"not-a-map"},
		[]any{map[string]any{"description": "d"}},                             // 缺 label
		[]any{map[string]any{"label": "A"}, map[string]any{"label": "A"}},     // 重复
		[]any{map[string]any{"label": "A"}, map[string]any{"label": "Other"}}, // Other
		[]any{map[string]any{"label": "A", "description": 1}, map[string]any{"label": "B"}},
		[]any{map[string]any{"label": "A", "preview": 1}, map[string]any{"label": "B"}},
	}
	for i, c := range cases {
		if _, ok := optionsOf(c); ok {
			t.Fatalf("case %d 应不合法", i)
		}
	}
}

// TestOptionsOf_可选字段 验证 description/preview 可为空或缺省
func TestOptionsOf_可选字段(t *testing.T) {
	got, ok := optionsOf([]any{
		map[string]any{"label": "A", "description": "", "preview": ""},
		map[string]any{"label": "B"},
	})
	if !ok || len(got) != 2 {
		t.Fatalf("应合法: %v %v", got, ok)
	}
}

// ──────────────────────────── answerValues 测试 ────────────────────────────

// TestAnswerValues_单值 验证单值与单元素列表等价
func TestAnswerValues_单值(t *testing.T) {
	if got, ok := answerValues("a", false); !ok || len(got) != 1 || got[0] != "a" {
		t.Fatalf("单值解析失败: %v", got)
	}
	if got, ok := answerValues([]any{"a"}, false); !ok || len(got) != 1 {
		t.Fatalf("单元素列表解析失败")
	}
}

// TestAnswerValues_多选 验证多选允许多值
func TestAnswerValues_多选(t *testing.T) {
	if got, ok := answerValues([]any{"a", "b"}, true); !ok || len(got) != 2 {
		t.Fatalf("多选解析失败: %v", got)
	}
	// 单选取多值非法
	if _, ok := answerValues([]any{"a", "b"}, false); ok {
		t.Fatalf("单选取多值应非法")
	}
}

// TestAnswerValues_边界 验证空、超 8 条、空白字符串
func TestAnswerValues_边界(t *testing.T) {
	if _, ok := answerValues([]any{}, true); ok {
		t.Fatalf("空列表应非法")
	}
	nine := make([]any, 9)
	for i := range nine {
		nine[i] = "a"
	}
	if _, ok := answerValues(nine, true); ok {
		t.Fatalf("超 8 条应非法")
	}
	if _, ok := answerValues("  ", false); ok {
		t.Fatalf("空白字符串应非法")
	}
	if _, ok := answerValues(123, false); ok {
		t.Fatalf("非字符串应非法")
	}
}

// TestAnswerValues_trim 验证返回值被 trim
func TestAnswerValues_trim(t *testing.T) {
	got, ok := answerValues("  a  ", false)
	if !ok || got[0] != "a" {
		t.Fatalf("应 trim，实际 %v", got)
	}
}

// ──────────────────────────── AskUserContinuation 测试 ────────────────────────────

// fullAskUserMetadata 构造一个完整的中断请求 metadata。
// 注意结构：BuildAskUserMetadata 返回的是"续接快照"本身，
// 需要由 rail 再嵌到 request.metadata[ASK_USER_CONTINUATION_METADATA_KEY] 下
// （对齐 Python root_context_rail.py L176-179）。
func fullAskUserMetadata(t *testing.T) map[string]any {
	t.Helper()
	snapshot := BuildAskUserMetadata(
		askUserDecisionContext(t),
		AskUserToolName,
		"call-1",
		askUserArgs(simpleQuestion("哪个配置？")),
	)
	if snapshot == nil {
		t.Fatalf("构造快照失败")
	}
	return map[string]any{AskUserContinuationMetadataKey: snapshot}
}

// wrapAskUserMetadata 把续接快照嵌进中断请求 metadata
func wrapAskUserMetadata(snapshot map[string]any) map[string]any {
	if snapshot == nil {
		return nil
	}
	return map[string]any{AskUserContinuationMetadataKey: snapshot}
}

// TestAskUserContinuation_正常 验证完整还原
func TestAskUserContinuation_正常(t *testing.T) {
	got := AskUserContinuation(fullAskUserMetadata(t), "call-1")
	if got == nil {
		t.Fatalf("应还原")
	}
	if got.ToolCallID != "call-1" {
		t.Fatalf("call id 不符")
	}
	if len(got.Questions) != 1 || got.Questions[0].Question != "哪个配置？" {
		t.Fatalf("问题不符: %+v", got.Questions)
	}
	if got.Context.SessionID != "sess-1" {
		t.Fatalf("上下文不符")
	}
}

// TestAskUserContinuation_callID不符 验证期望的 call id 与快照不一致返回 nil
func TestAskUserContinuation_callID不符(t *testing.T) {
	if got := AskUserContinuation(fullAskUserMetadata(t), "other"); got != nil {
		t.Fatalf("call id 不符应返回 nil")
	}
}

// TestAskUserContinuation_元数据缺失 验证各类缺失形态
func TestAskUserContinuation_元数据缺失(t *testing.T) {
	if got := AskUserContinuation(nil, "call-1"); got != nil {
		t.Fatalf("nil metadata 应返回 nil")
	}
	if got := AskUserContinuation(map[string]any{}, "call-1"); got != nil {
		t.Fatalf("缺键应返回 nil")
	}
	if got := AskUserContinuation(map[string]any{AskUserContinuationMetadataKey: "str"}, "call-1"); got != nil {
		t.Fatalf("值非映射应返回 nil")
	}
	// 字段数不符
	if got := AskUserContinuation(map[string]any{
		AskUserContinuationMetadataKey: map[string]any{"tool_call_id": "call-1"},
	}, "call-1"); got != nil {
		t.Fatalf("字段数不符应返回 nil")
	}
}

// TestAskUserContinuation_字段类型非法 验证 call id / questions / context 类型非法
func TestAskUserContinuation_字段类型非法(t *testing.T) {
	base := func() map[string]any {
		return map[string]any{
			"tool_call_id": "call-1",
			"context":      askUserDecisionContext(t).ToMapping(),
			"questions":    []any{simpleQuestion("q")},
		}
	}

	bad1 := base()
	bad1["tool_call_id"] = 123
	if got := AskUserContinuation(map[string]any{AskUserContinuationMetadataKey: bad1}, "call-1"); got != nil {
		t.Fatalf("call id 类型非法应返回 nil")
	}

	bad2 := base()
	bad2["questions"] = "x"
	if got := AskUserContinuation(map[string]any{AskUserContinuationMetadataKey: bad2}, "call-1"); got != nil {
		t.Fatalf("questions 非列表应返回 nil")
	}

	bad3 := base()
	bad3["context"] = "x"
	if got := AskUserContinuation(map[string]any{AskUserContinuationMetadataKey: bad3}, "call-1"); got != nil {
		t.Fatalf("context 非映射应返回 nil")
	}

	bad4 := base()
	bad4["context"] = map[string]any{}
	if got := AskUserContinuation(map[string]any{AskUserContinuationMetadataKey: bad4}, "call-1"); got != nil {
		t.Fatalf("context 非法应返回 nil")
	}
}

// TestAskUserContinuation_问题非法 验证问题项非法与数量越界
func TestAskUserContinuation_问题非法(t *testing.T) {
	build := func(questions []any) map[string]any {
		return map[string]any{AskUserContinuationMetadataKey: map[string]any{
			"tool_call_id": "call-1",
			"context":      askUserDecisionContext(t).ToMapping(),
			"questions":    questions,
		}}
	}

	if got := AskUserContinuation(build([]any{"not-a-map"}), "call-1"); got != nil {
		t.Fatalf("问题非映射应返回 nil")
	}
	if got := AskUserContinuation(build([]any{map[string]any{"question": ""}}), "call-1"); got != nil {
		t.Fatalf("非法问题应返回 nil")
	}
	if got := AskUserContinuation(build([]any{}), "call-1"); got != nil {
		t.Fatalf("零问题应返回 nil")
	}
	tooMany := make([]any, rails.MaxStructuredQuestions+1)
	for i := range tooMany {
		tooMany[i] = map[string]any{
			"question": "q" + string(rune('0'+i)), "options": []any{}, "multi_select": false,
		}
	}
	if got := AskUserContinuation(build(tooMany), "call-1"); got != nil {
		t.Fatalf("超上限应返回 nil")
	}
}

// TestAskUserContinuation_重复问题 验证重复问题文本返回 nil
func TestAskUserContinuation_重复问题(t *testing.T) {
	metadata := map[string]any{AskUserContinuationMetadataKey: map[string]any{
		"tool_call_id": "call-1",
		"context":      askUserDecisionContext(t).ToMapping(),
		"questions": []any{
			map[string]any{"question": "same", "options": []any{}, "multi_select": false},
			map[string]any{"question": "same", "options": []any{}, "multi_select": false},
		},
	}}
	if got := AskUserContinuation(metadata, "call-1"); got != nil {
		t.Fatalf("重复问题应返回 nil")
	}
}

// ──────────────────────────── PrepareAskUserResume 测试 ────────────────────────────

// continuationFor 构造一个续接上下文
func continuationFor(t *testing.T, questions ...map[string]any) *RootAskUserContinuation {
	t.Helper()
	snapshot := BuildAskUserMetadata(
		askUserDecisionContext(t), AskUserToolName, "call-1", askUserArgs(questions...),
	)
	if snapshot == nil {
		t.Fatalf("构造 metadata 失败")
	}
	got := AskUserContinuation(wrapAskUserMetadata(snapshot), "call-1")
	if got == nil {
		t.Fatalf("还原失败")
	}
	return got
}

// TestPrepareAskUserResume_正常 验证单问题正常还原
func TestPrepareAskUserResume_正常(t *testing.T) {
	cont := continuationFor(t, simpleQuestion("哪个配置？"))
	prepared := PrepareAskUserResume(cont, map[string]any{
		"answers": map[string]any{"哪个配置？": "dev"},
	})
	if prepared == nil {
		t.Fatalf("应成功")
	}
	if len(prepared.Clarifications) != 1 {
		t.Fatalf("应 1 条澄清")
	}
	if prepared.Clarifications[0].Answers[0] != "dev" {
		t.Fatalf("答案不符: %v", prepared.Clarifications[0].Answers)
	}
	if prepared.CapacityReason != "" {
		t.Fatalf("不应有容量原因")
	}
}

// TestPrepareAskUserResume_多选 验证多选题允许多值
func TestPrepareAskUserResume_多选(t *testing.T) {
	cont := continuationFor(t, map[string]any{"question": "要哪些？", "multi_select": true})
	prepared := PrepareAskUserResume(cont, map[string]any{
		"answers": map[string]any{"要哪些？": []any{"a", "b"}},
	})
	if prepared == nil || len(prepared.Clarifications[0].Answers) != 2 {
		t.Fatalf("多选失败: %+v", prepared)
	}
}

// TestPrepareAskUserResume_结构不符 验证 answer 结构不符返回 nil
func TestPrepareAskUserResume_结构不符(t *testing.T) {
	cont := continuationFor(t, simpleQuestion("q"))

	if got := PrepareAskUserResume(cont, "not-a-map"); got != nil {
		t.Fatalf("非映射应返回 nil")
	}
	if got := PrepareAskUserResume(cont, map[string]any{}); got != nil {
		t.Fatalf("空映射应返回 nil")
	}
	if got := PrepareAskUserResume(cont, map[string]any{"answers": "x"}); got != nil {
		t.Fatalf("answers 非映射应返回 nil")
	}
	if got := PrepareAskUserResume(cont, map[string]any{"other": map[string]any{}}); got != nil {
		t.Fatalf("缺 answers 应返回 nil")
	}
}

// TestPrepareAskUserResume_问题集合不符 验证问题数量与名称不一致返回 nil
func TestPrepareAskUserResume_问题集合不符(t *testing.T) {
	cont := continuationFor(t, simpleQuestion("q1"), simpleQuestion("q2"))

	if got := PrepareAskUserResume(cont, map[string]any{
		"answers": map[string]any{"q1": "a"},
	}); got != nil {
		t.Fatalf("数量不符应返回 nil")
	}
	if got := PrepareAskUserResume(cont, map[string]any{
		"answers": map[string]any{"q1": "a", "other": "b"},
	}); got != nil {
		t.Fatalf("问题名不符应返回 nil")
	}
}

// TestPrepareAskUserResume_答案非法 验证答案值非法返回 nil
func TestPrepareAskUserResume_答案非法(t *testing.T) {
	cont := continuationFor(t, simpleQuestion("q"))
	if got := PrepareAskUserResume(cont, map[string]any{
		"answers": map[string]any{"q": "  "},
	}); got != nil {
		t.Fatalf("空白答案应返回 nil")
	}
}

// TestPrepareAskUserResume_容量超限 验证单条澄清超容量时返回 CapacityReason
func TestPrepareAskUserResume_容量超限(t *testing.T) {
	// 构造一个超长问题，使澄清轮次超过单轮上限
	longQuestion := strings.Repeat("q", RootIntentMaxTurnChars-100)
	cont := continuationFor(t, map[string]any{"question": longQuestion})
	prepared := PrepareAskUserResume(cont, map[string]any{
		"answers": map[string]any{longQuestion: strings.Repeat("a", 200)},
	})
	if prepared == nil {
		t.Fatalf("应返回结构")
	}
	if prepared.CapacityReason != AutoReviewBlockIntentInputTooLarge {
		t.Fatalf("应标记容量原因，实际 %q", prepared.CapacityReason)
	}
	if prepared.Clarifications != nil {
		t.Fatalf("澄清应为 nil")
	}
}

// ──────────────────────────── ApplyAskUserResume 测试 ────────────────────────────

// TestApplyAskUserResume_正常追加 验证澄清被追加到意图窗口
func TestApplyAskUserResume_正常追加(t *testing.T) {
	cont := continuationFor(t, simpleQuestion("q"))
	prepared := PrepareAskUserResume(cont, map[string]any{
		"answers": map[string]any{"q": "a"},
	})
	got := ApplyAskUserResume(prepared)
	if got == nil {
		t.Fatalf("应返回上下文")
	}
	if len(got.TrustedTurns) != 2 {
		t.Fatalf("应有 2 轮（原 1 + 澄清 1），实际 %d", len(got.TrustedTurns))
	}
	if got.TrustedTurns[1].Kind != RootIntentTurnKindAskUserClarification {
		t.Fatalf("末轮应为澄清")
	}
}

// TestApplyAskUserResume_容量原因 验证只标记阻断原因不追加澄清
func TestApplyAskUserResume_容量原因(t *testing.T) {
	cont := continuationFor(t, simpleQuestion("q"))
	prepared := &RootAskUserResume{
		Continuation:   cont,
		CapacityReason: AutoReviewBlockIntentInputTooLarge,
	}
	got := ApplyAskUserResume(prepared)
	if got.AutoReviewBlockReason != AutoReviewBlockIntentInputTooLarge {
		t.Fatalf("应标记阻断原因，实际 %q", got.AutoReviewBlockReason)
	}
	if len(got.TrustedTurns) != 1 {
		t.Fatalf("不应追加轮次，实际 %d", len(got.TrustedTurns))
	}
}

// TestApplyAskUserResume_澄清为nil 验证澄清为 nil 且无容量原因时返回原上下文
func TestApplyAskUserResume_澄清为nil(t *testing.T) {
	cont := continuationFor(t, simpleQuestion("q"))
	prepared := &RootAskUserResume{Continuation: cont, Clarifications: nil}
	got := ApplyAskUserResume(prepared)
	if got != cont.Context {
		t.Fatalf("应返回原上下文")
	}
}

// TestApplyAskUserResume_容量原因非法 验证阻断原因非法时回退原上下文
func TestApplyAskUserResume_容量原因非法(t *testing.T) {
	cont := continuationFor(t, simpleQuestion("q"))
	prepared := &RootAskUserResume{
		Continuation:   cont,
		CapacityReason: "unknown-reason",
	}
	got := ApplyAskUserResume(prepared)
	if got != cont.Context {
		t.Fatalf("非法原因应回退原上下文")
	}
}

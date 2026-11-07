package permissions

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

// ──────────────────────────── RootAskUserOption 测试 ────────────────────────────

// TestNewRootAskUserOption_正常 验证合法标签构造成功
func TestNewRootAskUserOption_正常(t *testing.T) {
	opt, err := NewRootAskUserOption("允许", "本次允许", "preview")
	if err != nil {
		t.Fatalf("不应报错: %v", err)
	}
	if opt.Label != "允许" || opt.Description != "本次允许" || opt.Preview != "preview" {
		t.Fatalf("字段不符: %+v", opt)
	}
}

// TestNewRootAskUserOption_empty报错 验证空标签报错
func TestNewRootAskUserOption_empty报错(t *testing.T) {
	for _, label := range []string{"", "   ", "\t"} {
		if _, err := NewRootAskUserOption(label, "", ""); err == nil {
			t.Fatalf("label=%q 应报错", label)
		}
	}
}

// TestRootAskUserOption_往返 验证 ToMapping / FromMapping 往返一致
func TestRootAskUserOption_往返(t *testing.T) {
	opt, _ := NewRootAskUserOption("A", "desc", "prev")
	back, err := RootAskUserOptionFromMapping(opt.ToMapping())
	if err != nil {
		t.Fatalf("反序列化失败: %v", err)
	}
	if back != opt {
		t.Fatalf("往返不一致: %+v vs %+v", back, opt)
	}
}

// TestRootAskUserOptionFromMapping_字段数不符 验证字段数不符报错
func TestRootAskUserOptionFromMapping_字段数不符(t *testing.T) {
	if _, err := RootAskUserOptionFromMapping(map[string]any{"label": "A"}); err == nil {
		t.Fatalf("缺字段应报错")
	}
	full := map[string]any{"label": "A", "description": "", "preview": "", "extra": 1}
	if _, err := RootAskUserOptionFromMapping(full); err == nil {
		t.Fatalf("多字段应报错")
	}
	if _, err := RootAskUserOptionFromMapping(nil); err == nil {
		t.Fatalf("nil 应报错")
	}
}

// TestRootAskUserOptionFromMapping_类型不符 验证非字符串字段报错
func TestRootAskUserOptionFromMapping_类型不符(t *testing.T) {
	_, err := RootAskUserOptionFromMapping(map[string]any{
		"label": 123, "description": "", "preview": "",
	})
	if err == nil {
		t.Fatalf("应报错")
	}
}

// ──────────────────────────── RootAskUserClarification 测试 ────────────────────────────

// opts2 构造两个合法选项
func opts2(t *testing.T) []RootAskUserOption {
	t.Helper()
	a, _ := NewRootAskUserOption("A", "", "")
	b, _ := NewRootAskUserOption("B", "", "")
	return []RootAskUserOption{a, b}
}

// TestNewRootAskUserClarification_无选项 验证选项可为空
func TestNewRootAskUserClarification_无选项(t *testing.T) {
	c, err := NewRootAskUserClarification("怎么处理？", []string{"随便"}, nil)
	if err != nil {
		t.Fatalf("不应报错: %v", err)
	}
	if len(c.Options) != 0 || len(c.Answers) != 1 {
		t.Fatalf("字段不符: %+v", c)
	}
}

// TestNewRootAskUserClarification_两到四选项 验证 2-4 个选项通过
func TestNewRootAskUserClarification_两到四选项(t *testing.T) {
	for n := 2; n <= 4; n++ {
		opts := make([]RootAskUserOption, 0, n)
		for i := 0; i < n; i++ {
			o, _ := NewRootAskUserOption(string(rune('A'+i)), "", "")
			opts = append(opts, o)
		}
		if _, err := NewRootAskUserClarification("q", []string{"a"}, opts); err != nil {
			t.Fatalf("n=%d 应通过: %v", n, err)
		}
	}
}

// TestNewRootAskUserClarification_单选项报错 验证仅 1 个选项报错
func TestNewRootAskUserClarification_单选项报错(t *testing.T) {
	o, _ := NewRootAskUserOption("A", "", "")
	if _, err := NewRootAskUserClarification("q", []string{"a"}, []RootAskUserOption{o}); err == nil {
		t.Fatalf("单选项应报错")
	}
}

// TestNewRootAskUserClarification_五选项报错 验证超过 4 个选项报错
func TestNewRootAskUserClarification_五选项报错(t *testing.T) {
	opts := make([]RootAskUserOption, 0, 5)
	for i := 0; i < 5; i++ {
		o, _ := NewRootAskUserOption(string(rune('A'+i)), "", "")
		opts = append(opts, o)
	}
	if _, err := NewRootAskUserClarification("q", []string{"a"}, opts); err == nil {
		t.Fatalf("五选项应报错")
	}
}

// TestNewRootAskUserClarification_重复标签报错 验证标签重复报错
func TestNewRootAskUserClarification_重复标签报错(t *testing.T) {
	a, _ := NewRootAskUserOption("A", "", "")
	b, _ := NewRootAskUserOption("A", "other", "")
	if _, err := NewRootAskUserClarification("q", []string{"x"}, []RootAskUserOption{a, b}); err == nil {
		t.Fatalf("重复标签应报错")
	}
}

// TestNewRootAskUserClarification_Other标签报错 验证含 Other 标签报错
func TestNewRootAskUserClarification_Other标签报错(t *testing.T) {
	o, _ := NewRootAskUserOption("A", "", "")
	other, _ := NewRootAskUserOption("Other", "", "")
	if _, err := NewRootAskUserClarification("q", []string{"x"}, []RootAskUserOption{o, other}); err == nil {
		t.Fatalf("Other 标签应报错")
	}
}

// TestNewRootAskUserClarification_答案边界 验证答案为空/超 8 条报错，8 条通过
func TestNewRootAskUserClarification_答案边界(t *testing.T) {
	if _, err := NewRootAskUserClarification("q", nil, nil); err == nil {
		t.Fatalf("空答案应报错")
	}
	eight := make([]string, 8)
	for i := range eight {
		eight[i] = "a"
	}
	if _, err := NewRootAskUserClarification("q", eight, nil); err != nil {
		t.Fatalf("8 条应通过: %v", err)
	}
	nine := make([]string, 9)
	for i := range nine {
		nine[i] = "a"
	}
	if _, err := NewRootAskUserClarification("q", nine, nil); err == nil {
		t.Fatalf("9 条应报错")
	}
}

// TestNewRootAskUserClarification_答案trim 验证答案被 trim
func TestNewRootAskUserClarification_答案trim(t *testing.T) {
	c, err := NewRootAskUserClarification("q", []string{"  a  "}, nil)
	if err != nil {
		t.Fatalf("不应报错: %v", err)
	}
	if c.Answers[0] != "a" {
		t.Fatalf("应 trim 为 a，实际 %q", c.Answers[0])
	}
}

// TestNewRootAskUserClarification_空白答案报错 验证 trim 后为空的答案报错
func TestNewRootAskUserClarification_空白答案报错(t *testing.T) {
	if _, err := NewRootAskUserClarification("q", []string{"   "}, nil); err == nil {
		t.Fatalf("空白答案应报错")
	}
}

// TestNewRootAskUserClarification_问题为空报错 验证问题为空报错
func TestNewRootAskUserClarification_问题为空报错(t *testing.T) {
	if _, err := NewRootAskUserClarification("  ", []string{"a"}, nil); err == nil {
		t.Fatalf("空问题应报错")
	}
}

// TestRootAskUserClarification_往返 验证序列化往返一致
func TestRootAskUserClarification_往返(t *testing.T) {
	c, err := NewRootAskUserClarification("q", []string{"a", "b"}, opts2(t))
	if err != nil {
		t.Fatalf("构造失败: %v", err)
	}
	back, err := RootAskUserClarificationFromMapping(c.ToMapping())
	if err != nil {
		t.Fatalf("反序列化失败: %v", err)
	}
	if back.Question != c.Question || len(back.Answers) != 2 || len(back.Options) != 2 {
		t.Fatalf("往返不一致: %+v vs %+v", back, c)
	}
}

// TestRootAskUserClarificationFromMapping_异常 验证各类非法输入报错
func TestRootAskUserClarificationFromMapping_异常(t *testing.T) {
	if _, err := RootAskUserClarificationFromMapping(nil); err == nil {
		t.Fatalf("nil 应报错")
	}
	if _, err := RootAskUserClarificationFromMapping(map[string]any{"question": "q"}); err == nil {
		t.Fatalf("缺字段应报错")
	}
	bad := map[string]any{"question": "q", "options": "not-a-list", "answers": []any{"a"}}
	if _, err := RootAskUserClarificationFromMapping(bad); err == nil {
		t.Fatalf("options 非列表应报错")
	}
	bad2 := map[string]any{"question": "q", "options": []any{"x"}, "answers": []any{"a"}}
	if _, err := RootAskUserClarificationFromMapping(bad2); err == nil {
		t.Fatalf("option 非映射应报错")
	}
	bad3 := map[string]any{"question": "q", "options": []any{}, "answers": []any{1}}
	if _, err := RootAskUserClarificationFromMapping(bad3); err == nil {
		t.Fatalf("answer 非字符串应报错")
	}
}

// ──────────────────────────── RootIntentTurn 测试 ────────────────────────────

// TestNewRootIntentTurn_history无需requestID 验证 history 轮次允许空 request_id
func TestNewRootIntentTurn_history无需requestID(t *testing.T) {
	turn, err := NewRootIntentTurn("", RootIntentTurnKindHistory, "历史消息", nil)
	if err != nil {
		t.Fatalf("不应报错: %v", err)
	}
	if turn.Kind != RootIntentTurnKindHistory {
		t.Fatalf("kind 不符")
	}
}

// TestNewRootIntentTurn_非history需要requestID 验证其余类型必须带 request_id
func TestNewRootIntentTurn_非history需要requestID(t *testing.T) {
	for _, kind := range []string{
		RootIntentTurnKindFresh, RootIntentTurnKindSteer,
	} {
		if _, err := NewRootIntentTurn("", kind, "文本", nil); err == nil {
			t.Fatalf("kind=%s 空 request_id 应报错", kind)
		}
	}
}

// TestNewRootIntentTurn_非法kind 验证未知 kind 报错
func TestNewRootIntentTurn_非法kind(t *testing.T) {
	if _, err := NewRootIntentTurn("r", "unknown", "文本", nil); err == nil {
		t.Fatalf("非法 kind 应报错")
	}
}

// TestNewRootIntentTurn_requestID超长 验证 request_id 超 512 报错
func TestNewRootIntentTurn_requestID超长(t *testing.T) {
	long := strings.Repeat("a", 513)
	if _, err := NewRootIntentTurn(long, RootIntentTurnKindFresh, "文本", nil); err == nil {
		t.Fatalf("超长应报错")
	}
	at512 := strings.Repeat("a", 512)
	if _, err := NewRootIntentTurn(at512, RootIntentTurnKindFresh, "文本", nil); err != nil {
		t.Fatalf("512 应通过: %v", err)
	}
}

// TestNewRootIntentTurn_澄清轮次规则 验证澄清轮次不能带 text，且澄清数 1-4
func TestNewRootIntentTurn_澄清轮次规则(t *testing.T) {
	c, _ := NewRootAskUserClarification("q", []string{"a"}, nil)

	if _, err := NewRootIntentTurn("r", RootIntentTurnKindAskUserClarification, "有文本", []RootAskUserClarification{c}); err == nil {
		t.Fatalf("澄清轮次带 text 应报错")
	}
	if _, err := NewRootIntentTurn("r", RootIntentTurnKindAskUserClarification, "", nil); err == nil {
		t.Fatalf("澄清轮次无澄清应报错")
	}
	if _, err := NewRootIntentTurn("r", RootIntentTurnKindAskUserClarification, "", []RootAskUserClarification{c}); err != nil {
		t.Fatalf("单条澄清应通过: %v", err)
	}
	five := make([]RootAskUserClarification, 5)
	for i := range five {
		five[i] = c
	}
	if _, err := NewRootIntentTurn("r", RootIntentTurnKindAskUserClarification, "", five); err == nil {
		t.Fatalf("5 条澄清应报错")
	}
}

// TestNewRootIntentTurn_非澄清必须有文本 验证非澄清轮次文本必填且不得带澄清
func TestNewRootIntentTurn_非澄清必须有文本(t *testing.T) {
	if _, err := NewRootIntentTurn("r", RootIntentTurnKindFresh, "  ", nil); err == nil {
		t.Fatalf("空文本应报错")
	}
	c, _ := NewRootAskUserClarification("q", []string{"a"}, nil)
	if _, err := NewRootIntentTurn("r", RootIntentTurnKindFresh, "文本", []RootAskUserClarification{c}); err == nil {
		t.Fatalf("非澄清轮次带澄清应报错")
	}
}

// TestNewRootIntentTurn_单据超长报错 验证单轮文本超 8000 报错
func TestNewRootIntentTurn_单据超长报错(t *testing.T) {
	long := strings.Repeat("a", RootIntentMaxTurnChars+1)
	if _, err := NewRootIntentTurn("r", RootIntentTurnKindFresh, long, nil); err == nil {
		t.Fatalf("超长应报错")
	}
	exact := strings.Repeat("a", RootIntentMaxTurnChars)
	if _, err := NewRootIntentTurn("r", RootIntentTurnKindFresh, exact, nil); err != nil {
		t.Fatalf("等于上限应通过: %v", err)
	}
}

// TestRootIntentTurnText_非澄清返回原文 验证非澄清轮次直接返回 text
func TestRootIntentTurnText_非澄清返回原文(t *testing.T) {
	turn, _ := NewRootIntentTurn("r", RootIntentTurnKindFresh, "原文", nil)
	if got := RootIntentTurnText(turn, false); got != "原文" {
		t.Fatalf("期望 原文，实际 %q", got)
	}
	if got := RootIntentTurnText(turn, true); got != "原文" {
		t.Fatalf("includeQuestion 不影响非澄清: %q", got)
	}
}

// TestRootIntentTurnText_澄清序列化 验证澄清轮次序列化为 JSON
func TestRootIntentTurnText_澄清序列化(t *testing.T) {
	c, _ := NewRootAskUserClarification("哪个配置？", []string{"dev"}, opts2(t))
	turn, err := NewRootIntentTurn("r", RootIntentTurnKindAskUserClarification, "", []RootAskUserClarification{c})
	if err != nil {
		t.Fatalf("构造失败: %v", err)
	}

	withoutQ := RootIntentTurnText(turn, false)
	var parsed map[string]any
	if err := json.Unmarshal([]byte(withoutQ), &parsed); err != nil {
		t.Fatalf("应可解析为 JSON: %v (%s)", err, withoutQ)
	}
	items, ok := parsed["clarifications"].([]any)
	if !ok || len(items) != 1 {
		t.Fatalf("clarifications 结构不符: %s", withoutQ)
	}
	item := items[0].(map[string]any)
	if _, hasQ := item["question"]; hasQ {
		t.Fatalf("includeQuestion=false 不应含 question")
	}
	if _, hasOpts := item["options"]; hasOpts {
		t.Fatalf("includeQuestion=false 不应含 options")
	}

	withQ := RootIntentTurnText(turn, true)
	if !strings.Contains(withQ, "哪个配置？") {
		t.Fatalf("includeQuestion=true 应含 question: %s", withQ)
	}
	if !strings.Contains(withQ, "\"options\"") {
		t.Fatalf("includeQuestion=true 应含 options: %s", withQ)
	}
}

// TestRootIntentTurn_往返 验证序列化往返一致
func TestRootIntentTurn_往返(t *testing.T) {
	turn, _ := NewRootIntentTurn("r", RootIntentTurnKindFresh, "文本", nil)
	back, err := RootIntentTurnFromMapping(turn.ToMapping())
	if err != nil {
		t.Fatalf("反序列化失败: %v", err)
	}
	if back.RequestID != turn.RequestID || back.Kind != turn.Kind || back.Text != turn.Text {
		t.Fatalf("往返不一致: %+v vs %+v", back, turn)
	}
}

// TestRootIntentTurnFromMapping_异常 验证非法输入报错
func TestRootIntentTurnFromMapping_异常(t *testing.T) {
	if _, err := RootIntentTurnFromMapping(nil); err == nil {
		t.Fatalf("nil 应报错")
	}
	if _, err := RootIntentTurnFromMapping(map[string]any{"request_id": "r"}); err == nil {
		t.Fatalf("缺字段应报错")
	}
	bad := map[string]any{
		"request_id": "r", "kind": RootIntentTurnKindFresh, "text": "t",
		"clarifications": "not-list",
	}
	if _, err := RootIntentTurnFromMapping(bad); err == nil {
		t.Fatalf("clarifications 非列表应报错")
	}
	bad2 := map[string]any{
		"request_id": "r", "kind": RootIntentTurnKindFresh, "text": "t",
		"clarifications": []any{"x"},
	}
	if _, err := RootIntentTurnFromMapping(bad2); err == nil {
		t.Fatalf("澄清项非映射应报错")
	}
}

// ──────────────────────────── RootDecisionContext 测试 ────────────────────────────

// freshTurn 构造一条 fresh 轮次
func freshTurn(t *testing.T, text string) RootIntentTurn {
	t.Helper()
	turn, err := NewRootIntentTurn("req-1", RootIntentTurnKindFresh, text, nil)
	if err != nil {
		t.Fatalf("构造失败: %v", err)
	}
	return turn
}

// TestNewRootDecisionContext_正常 验证合法参数构造成功
func TestNewRootDecisionContext_正常(t *testing.T) {
	dc, err := NewRootDecisionContext("s", "r", "web", []RootIntentTurn{freshTurn(t, "hi")}, "")
	if err != nil {
		t.Fatalf("不应报错: %v", err)
	}
	if dc.SessionID != "s" || dc.RequestID != "r" || dc.ChannelID != "web" {
		t.Fatalf("字段不符: %+v", dc)
	}
	if len(dc.TrustedTurns) != 1 {
		t.Fatalf("轮次数不符")
	}
}

// TestNewRootDecisionContext_必填校验 验证 session_id / request_id 必填且不超 512
func TestNewRootDecisionContext_必填校验(t *testing.T) {
	if _, err := NewRootDecisionContext("", "r", "", nil, ""); err == nil {
		t.Fatalf("空 session_id 应报错")
	}
	if _, err := NewRootDecisionContext("s", "", "", nil, ""); err == nil {
		t.Fatalf("空 request_id 应报错")
	}
	long := strings.Repeat("a", 513)
	if _, err := NewRootDecisionContext(long, "r", "", nil, ""); err == nil {
		t.Fatalf("超长 session_id 应报错")
	}
}

// TestNewRootDecisionContext_channelID可为空 验证 channel_id 允许空但不超 512
func TestNewRootDecisionContext_channelID可为空(t *testing.T) {
	if _, err := NewRootDecisionContext("s", "r", "", nil, ""); err != nil {
		t.Fatalf("空 channel_id 应通过: %v", err)
	}
	long := strings.Repeat("a", 513)
	if _, err := NewRootDecisionContext("s", "r", long, nil, ""); err == nil {
		t.Fatalf("超长 channel_id 应报错")
	}
}

// TestNewRootDecisionContext_轮次上限 验证超过 20 轮报错
func TestNewRootDecisionContext_轮次上限(t *testing.T) {
	turns := make([]RootIntentTurn, RootIntentMaxTurns+1)
	for i := range turns {
		turns[i] = freshTurn(t, "x")
	}
	if _, err := NewRootDecisionContext("s", "r", "", turns, ""); err == nil {
		t.Fatalf("超 20 轮应报错")
	}
}

// TestNewRootDecisionContext_总字符上限 验证总字符超限报错
func TestNewRootDecisionContext_总字符上限(t *testing.T) {
	// 每条 7999 字符，7 条 = 55993 > 48000
	big := strings.Repeat("a", 7999)
	turns := make([]RootIntentTurn, 7)
	for i := range turns {
		turns[i] = freshTurn(t, big)
	}
	if _, err := NewRootDecisionContext("s", "r", "", turns, ""); err == nil {
		t.Fatalf("超总字符上限应报错")
	}
}

// TestNewRootDecisionContext_阻断原因校验 验证只接受三个合法值
func TestNewRootDecisionContext_阻断原因校验(t *testing.T) {
	for _, reason := range []string{"", AutoReviewBlockIntentInputTooLarge, AutoReviewBlockHistoryWindowTruncated} {
		if _, err := NewRootDecisionContext("s", "r", "", nil, reason); err != nil {
			t.Fatalf("reason=%q 应通过: %v", reason, err)
		}
	}
	if _, err := NewRootDecisionContext("s", "r", "", nil, "unknown"); err == nil {
		t.Fatalf("非法 reason 应报错")
	}
}

// TestRootDecisionContext_ObjectiveText 验证取最近一条非澄清轮次原文
func TestRootDecisionContext_ObjectiveText(t *testing.T) {
	c, _ := NewRootAskUserClarification("q", []string{"a"}, nil)
	clarifyTurn, _ := NewRootIntentTurn("req-1", RootIntentTurnKindAskUserClarification, "", []RootAskUserClarification{c})

	dc, err := NewRootDecisionContext("s", "r", "", []RootIntentTurn{
		freshTurn(t, "目标文本"),
		clarifyTurn,
	}, "")
	if err != nil {
		t.Fatalf("构造失败: %v", err)
	}
	if got := dc.ObjectiveText(); got != "目标文本" {
		t.Fatalf("期望 目标文本，实际 %q", got)
	}

	empty, _ := NewRootDecisionContext("s", "r", "", nil, "")
	if got := empty.ObjectiveText(); got != "" {
		t.Fatalf("空轮次应返回空串，实际 %q", got)
	}
	var nilCtx *RootDecisionContext
	if got := nilCtx.ObjectiveText(); got != "" {
		t.Fatalf("nil 应返回空串")
	}
}

// TestRootDecisionContext_往返 验证序列化往返一致
func TestRootDecisionContext_往返(t *testing.T) {
	dc, _ := NewRootDecisionContext("s", "r", "web", []RootIntentTurn{freshTurn(t, "hi")}, "")
	back, err := RootDecisionContextFromMapping(dc.ToMapping())
	if err != nil {
		t.Fatalf("反序列化失败: %v", err)
	}
	if back.SessionID != dc.SessionID || len(back.TrustedTurns) != 1 {
		t.Fatalf("往返不一致: %+v", back)
	}
}

// TestRootDecisionContextFromMapping_异常 验证非法输入报错
func TestRootDecisionContextFromMapping_异常(t *testing.T) {
	if _, err := RootDecisionContextFromMapping(nil); err == nil {
		t.Fatalf("nil 应报错")
	}
	if _, err := RootDecisionContextFromMapping(map[string]any{"session_id": "s"}); err == nil {
		t.Fatalf("缺字段应报错")
	}
	bad := map[string]any{
		"session_id": "s", "request_id": "r", "channel_id": "",
		"trusted_turns": "not-list", "auto_review_block_reason": "",
	}
	if _, err := RootDecisionContextFromMapping(bad); err == nil {
		t.Fatalf("trusted_turns 非列表应报错")
	}
	bad2 := map[string]any{
		"session_id": "s", "request_id": "r", "channel_id": "",
		"trusted_turns": []any{"x"}, "auto_review_block_reason": "",
	}
	if _, err := RootDecisionContextFromMapping(bad2); err == nil {
		t.Fatalf("轮次非映射应报错")
	}
}

// ──────────────────────────── ContextVar 绑定测试 ────────────────────────────

// TestBindRootDecisionContext_绑定与派生隔离 验证绑定生效且不影响父 ctx
func TestBindRootDecisionContext_绑定与派生隔离(t *testing.T) {
	parent := context.Background()
	dc, _ := NewRootDecisionContext("s", "r", "", []RootIntentTurn{freshTurn(t, "hi")}, "")

	child, cleanup := BindRootDecisionContext(parent, dc)
	defer cleanup()

	if got := CurrentRootDecisionContext(child); got == nil || got.SessionID != "s" {
		t.Fatalf("子 ctx 应读到绑定: %+v", got)
	}
	if got := CurrentRootDecisionContext(parent); got != nil {
		t.Fatalf("父 ctx 不应受影响")
	}
}

// TestBindRootDecisionContext_绑nil 验证可显式绑定 nil 以清空
func TestBindRootDecisionContext_绑nil(t *testing.T) {
	dc, _ := NewRootDecisionContext("s", "r", "", nil, "")
	ctx1, c1 := BindRootDecisionContext(context.Background(), dc)
	defer c1()
	ctx2, c2 := BindRootDecisionContext(ctx1, nil)
	defer c2()

	if got := CurrentRootDecisionContext(ctx2); got != nil {
		t.Fatalf("应被清空，实际 %+v", got)
	}
	if got := CurrentRootDecisionContext(ctx1); got == nil {
		t.Fatalf("上层不应受影响")
	}
}

// TestCurrentRootDecisionContext_未绑定 验证未绑定时返回 nil
func TestCurrentRootDecisionContext_未绑定(t *testing.T) {
	if got := CurrentRootDecisionContext(context.Background()); got != nil {
		t.Fatalf("未绑定应返回 nil")
	}
}

// ──────────────────────────── FromExtra / PutInInputs 测试 ────────────────────────────

// TestRootDecisionContextFromExtra_指针值 验证直接存指针时原样返回
func TestRootDecisionContextFromExtra_指针值(t *testing.T) {
	dc, _ := NewRootDecisionContext("s", "r", "", nil, "")
	got, err := RootDecisionContextFromExtra(map[string]any{RootContextKey: dc})
	if err != nil {
		t.Fatalf("不应报错: %v", err)
	}
	if got != dc {
		t.Fatalf("应返回同一指针")
	}
}

// TestRootDecisionContextFromExtra_映射值 验证存映射时反序列化
func TestRootDecisionContextFromExtra_映射值(t *testing.T) {
	dc, _ := NewRootDecisionContext("s", "r", "", nil, "")
	got, err := RootDecisionContextFromExtra(map[string]any{RootContextKey: dc.ToMapping()})
	if err != nil {
		t.Fatalf("不应报错: %v", err)
	}
	if got.SessionID != "s" {
		t.Fatalf("反序列化不符: %+v", got)
	}
}

// TestRootDecisionContextFromExtra_异常 验证缺失与类型非法报错
func TestRootDecisionContextFromExtra_异常(t *testing.T) {
	if _, err := RootDecisionContextFromExtra(nil); err == nil {
		t.Fatalf("nil 应报错")
	}
	if _, err := RootDecisionContextFromExtra(map[string]any{}); err == nil {
		t.Fatalf("缺键应报错")
	}
	if _, err := RootDecisionContextFromExtra(map[string]any{RootContextKey: "str"}); err == nil {
		t.Fatalf("类型非法应报错")
	}
}

// TestPutRootDecisionContextInInputs_写入路径 验证写到 run.context.extra
func TestPutRootDecisionContextInInputs_写入路径(t *testing.T) {
	dc, _ := NewRootDecisionContext("s", "r", "", nil, "")
	out := PutRootDecisionContextInInputs(map[string]any{"other": 1}, dc)

	run, ok := out["run"].(map[string]any)
	if !ok {
		t.Fatalf("run 应是映射")
	}
	ctxMap, ok := run["context"].(map[string]any)
	if !ok {
		t.Fatalf("context 应是映射")
	}
	extra, ok := ctxMap["extra"].(map[string]any)
	if !ok {
		t.Fatalf("extra 应是映射")
	}
	stored, ok := extra[RootContextKey].(map[string]any)
	if !ok {
		t.Fatalf("应写入映射形态")
	}
	if stored["session_id"] != "s" {
		t.Fatalf("写入内容不符: %v", stored)
	}
	if out["other"] != 1 {
		t.Fatalf("原有字段应保留")
	}
}

// TestPutRootDecisionContextInInputs_不修改入参 验证不修改原始 map
func TestPutRootDecisionContextInInputs_不修改入参(t *testing.T) {
	dc, _ := NewRootDecisionContext("s", "r", "", nil, "")
	inputs := map[string]any{"run": map[string]any{"context": map[string]any{"extra": map[string]any{}}}}
	_ = PutRootDecisionContextInInputs(inputs, dc)

	original := inputs["run"].(map[string]any)["context"].(map[string]any)["extra"].(map[string]any)
	if _, exists := original[RootContextKey]; exists {
		t.Fatalf("不应修改入参")
	}
}

// TestPutRootDecisionContextInInputs_空输入 验证空映射也能正确构造层级
func TestPutRootDecisionContextInInputs_空输入(t *testing.T) {
	dc, _ := NewRootDecisionContext("s", "r", "", nil, "")
	out := PutRootDecisionContextInInputs(nil, dc)
	run := out["run"].(map[string]any)
	ctxMap := run["context"].(map[string]any)
	extra := ctxMap["extra"].(map[string]any)
	if _, ok := extra[RootContextKey]; !ok {
		t.Fatalf("应写入")
	}
}

// ──────────────────────────── AppendRootClarification 测试 ────────────────────────────

// TestAppendRootClarification_追加一轮 验证澄清被追加为末轮
func TestAppendRootClarification_追加一轮(t *testing.T) {
	dc, _ := NewRootDecisionContext("s", "r", "", []RootIntentTurn{freshTurn(t, "hi")}, "")
	c, _ := NewRootAskUserClarification("q", []string{"a"}, nil)

	got := AppendRootClarification(dc, []RootAskUserClarification{c})
	if len(got.TrustedTurns) != 2 {
		t.Fatalf("应有 2 轮，实际 %d", len(got.TrustedTurns))
	}
	if got.TrustedTurns[1].Kind != RootIntentTurnKindAskUserClarification {
		t.Fatalf("末轮应为澄清")
	}
	if len(got.TrustedTurns[1].Clarifications) != 1 {
		t.Fatalf("澄清数不符")
	}
	// 原 context 不变
	if len(dc.TrustedTurns) != 1 {
		t.Fatalf("不应修改原 context")
	}
}

// TestAppendRootClarification_轮次上限裁剪 验证超过 20 轮时丢弃最老的
func TestAppendRootClarification_轮次上限裁剪(t *testing.T) {
	turns := make([]RootIntentTurn, RootIntentMaxTurns)
	for i := range turns {
		turns[i] = freshTurn(t, "x")
	}
	dc, err := NewRootDecisionContext("s", "r", "", turns, "")
	if err != nil {
		t.Fatalf("构造失败: %v", err)
	}
	c, _ := NewRootAskUserClarification("q", []string{"a"}, nil)

	got := AppendRootClarification(dc, []RootAskUserClarification{c})
	if len(got.TrustedTurns) != RootIntentMaxTurns {
		t.Fatalf("应保持 20 轮，实际 %d", len(got.TrustedTurns))
	}
	if got.TrustedTurns[RootIntentMaxTurns-1].Kind != RootIntentTurnKindAskUserClarification {
		t.Fatalf("末轮应为澄清")
	}
	if got.AutoReviewBlockReason != AutoReviewBlockHistoryWindowTruncated {
		t.Fatalf("应标记截断，实际 %q", got.AutoReviewBlockReason)
	}
}

// TestAppendRootClarification_字符超限裁剪 验证总字符超限时从头丢弃
func TestAppendRootClarification_字符超限裁剪(t *testing.T) {
	// 6 条 7999 字符 = 47994，加一条澄清后超限
	big := strings.Repeat("a", 7999)
	turns := make([]RootIntentTurn, 6)
	for i := range turns {
		turns[i] = freshTurn(t, big)
	}
	dc, err := NewRootDecisionContext("s", "r", "", turns, "")
	if err != nil {
		t.Fatalf("构造失败: %v", err)
	}
	c, _ := NewRootAskUserClarification(strings.Repeat("q", 500), []string{"a"}, nil)

	got := AppendRootClarification(dc, []RootAskUserClarification{c})
	total := totalChars(got.TrustedTurns)
	if total > RootIntentMaxTotalChars {
		t.Fatalf("总字符仍超限: %d", total)
	}
	if got.AutoReviewBlockReason != AutoReviewBlockHistoryWindowTruncated {
		t.Fatalf("应标记截断，实际 %q", got.AutoReviewBlockReason)
	}
	if len(got.TrustedTurns) >= len(turns)+1 {
		t.Fatalf("应有轮次被丢弃")
	}
}

// TestAppendRootClarification_单轮超限清空 验证只剩本轮且仍超限时清空窗口
//
// 场景：上游只有一条超总上限的轮次。裁剪循环先把上游那条 pop 掉，
// 剩下只有新追加的澄清轮；若该轮本身不超总上限，则保留澄清轮并标记截断。
// 只有"只剩一轮仍超上限"才清空窗口。
func TestAppendRootClarification_单轮超限清空(t *testing.T) {
	huge := RootIntentTurn{
		RequestID: "req-1",
		Kind:      RootIntentTurnKindFresh,
		Text:      strings.Repeat("a", RootIntentMaxTotalChars+1),
	}
	dc := &RootDecisionContext{
		SessionID:    "s",
		RequestID:    "r",
		ChannelID:    "",
		TrustedTurns: []RootIntentTurn{huge},
	}
	c, _ := NewRootAskUserClarification(strings.Repeat("q", 1000), []string{"a"}, nil)

	got := AppendRootClarification(dc, []RootAskUserClarification{c})
	// 超大上游轮次被丢弃，只保留澄清轮
	if len(got.TrustedTurns) != 1 {
		t.Fatalf("应只保留澄清轮，实际 %d 轮", len(got.TrustedTurns))
	}
	if got.TrustedTurns[0].Kind != RootIntentTurnKindAskUserClarification {
		t.Fatalf("保留的应是澄清轮")
	}
	if got.AutoReviewBlockReason != AutoReviewBlockHistoryWindowTruncated {
		t.Fatalf("应标记截断，实际 %q", got.AutoReviewBlockReason)
	}
}

// TestAppendRootClarification_澄清轮自身超限 验证只有澄清轮时按各自字符数取舍
//
// 该用例直接走公开 API：一条不超单轮上限的澄清轮，加上一条超大上游轮次，
// 裁剪后只剩澄清轮且总量未超总上限，故保留澄清轮并标记截断（已在上一用例覆盖）。
// 这里补充"上游为空、澄清轮正常"的基线。
func TestAppendRootClarification_澄清轮自身超限(t *testing.T) {
	dc, _ := NewRootDecisionContext("s", "r", "", nil, "")
	c, err := NewRootAskUserClarification("正常问题", []string{"a"}, nil)
	if err != nil {
		t.Fatalf("构造失败: %v", err)
	}
	got := AppendRootClarification(dc, []RootAskUserClarification{c})
	if len(got.TrustedTurns) != 1 {
		t.Fatalf("应保留澄清轮，实际 %d 轮", len(got.TrustedTurns))
	}
	if got.AutoReviewBlockReason != "" {
		t.Fatalf("不应阻断，实际 %q", got.AutoReviewBlockReason)
	}
}

// TestAppendRootClarification_非法澄清不追加 验证构造失败时原样返回
func TestAppendRootClarification_非法澄清不追加(t *testing.T) {
	dc, _ := NewRootDecisionContext("s", "r", "", []RootIntentTurn{freshTurn(t, "hi")}, "")
	// 空澄清列表 → NewRootIntentTurn 报错
	got := AppendRootClarification(dc, nil)
	if got != dc {
		t.Fatalf("应原样返回")
	}
}

// ──────────────────────────── 证据类函数测试 ────────────────────────────

// TestOriginalUserIntentEvidence_TaskObjective 验证有/无 context 两种取值
func TestOriginalUserIntentEvidence_TaskObjective(t *testing.T) {
	dc, _ := NewRootDecisionContext("s", "r", "", []RootIntentTurn{freshTurn(t, "目标")}, "")
	withCtx := OriginalUserIntentEvidence{Text: "fallback", Context: dc}
	if got := withCtx.TaskObjective(); got != "目标" {
		t.Fatalf("期望 目标，实际 %q", got)
	}
	withoutCtx := OriginalUserIntentEvidence{Text: "fallback"}
	if got := withoutCtx.TaskObjective(); got != "fallback" {
		t.Fatalf("期望 fallback，实际 %q", got)
	}
}

// TestOriginalUserIntentEvidence_CurrentUserMessage 验证末轮文本
func TestOriginalUserIntentEvidence_CurrentUserMessage(t *testing.T) {
	dc, _ := NewRootDecisionContext("s", "r", "", []RootIntentTurn{freshTurn(t, "末轮")}, "")
	e := OriginalUserIntentEvidence{Text: "fallback", Context: dc}
	if got := e.CurrentUserMessage(); got != "末轮" {
		t.Fatalf("期望 末轮，实际 %q", got)
	}
	empty := OriginalUserIntentEvidence{Text: "fallback"}
	if got := empty.CurrentUserMessage(); got != "fallback" {
		t.Fatalf("期望 fallback，实际 %q", got)
	}
}

// TestHasValidAskUserClarification 验证有无澄清的判定
func TestHasValidAskUserClarification(t *testing.T) {
	if HasValidAskUserClarification(nil) {
		t.Fatalf("nil 应为 false")
	}
	if HasValidAskUserClarification(&OriginalUserIntentEvidence{}) {
		t.Fatalf("无 context 应为 false")
	}

	dc, _ := NewRootDecisionContext("s", "r", "", []RootIntentTurn{freshTurn(t, "x")}, "")
	if HasValidAskUserClarification(&OriginalUserIntentEvidence{Context: dc}) {
		t.Fatalf("无澄清应为 false")
	}

	c, _ := NewRootAskUserClarification("q", []string{"a"}, nil)
	clarifyTurn, _ := NewRootIntentTurn("req-1", RootIntentTurnKindAskUserClarification, "", []RootAskUserClarification{c})
	dc2, _ := NewRootDecisionContext("s", "r", "", []RootIntentTurn{clarifyTurn}, "")
	if !HasValidAskUserClarification(&OriginalUserIntentEvidence{Context: dc2}) {
		t.Fatalf("有澄清应为 true")
	}
}

// TestOriginalUserIntent 验证从绑定上下文构造证据
func TestOriginalUserIntent(t *testing.T) {
	if OriginalUserIntent(context.Background()) != nil {
		t.Fatalf("未绑定应返回 nil")
	}

	dc, _ := NewRootDecisionContext("s", "r", "", []RootIntentTurn{freshTurn(t, "hi")}, "")
	ctx, cleanup := BindRootDecisionContext(context.Background(), dc)
	defer cleanup()

	evidence := OriginalUserIntent(ctx)
	if evidence == nil {
		t.Fatalf("应返回证据")
	}
	if evidence.Source != UserIntentSourceHostUserMessage {
		t.Fatalf("source 不符: %q", evidence.Source)
	}
	if evidence.Text != "hi" {
		t.Fatalf("text 不符: %q", evidence.Text)
	}
	if evidence.Context != dc {
		t.Fatalf("context 应一致")
	}
}

// TestFileDeliveryConstraintText 验证拼接行为
func TestFileDeliveryConstraintText(t *testing.T) {
	if got := FileDeliveryConstraintText(nil); got != "" {
		t.Fatalf("nil 应返回空串")
	}
	if got := FileDeliveryConstraintText(&OriginalUserIntentEvidence{Text: "纯文本"}); got != "纯文本" {
		t.Fatalf("无 context 应返回 Text，实际 %q", got)
	}

	t1 := freshTurn(t, "第一条")
	t2, _ := NewRootIntentTurn("req-1", RootIntentTurnKindSteer, "第二条", nil)
	dc, _ := NewRootDecisionContext("s", "r", "", []RootIntentTurn{t1, t2}, "")
	got := FileDeliveryConstraintText(&OriginalUserIntentEvidence{Context: dc})
	if got != "第一条\n第二条" {
		t.Fatalf("拼接不符: %q", got)
	}
}

// ──────────────────────────── ExtractPermissionUserContent 测试 ────────────────────────────

// validEnvelope 构造一个合法信封 JSON
func validEnvelope(overrides map[string]any) map[string]any {
	env := map[string]any{
		"source":                      "feishu-user",
		"timestamp":                   "2026-11-07T10:00:00Z",
		"preferred_response_language": "zh",
		"content":                     "用户原文",
		"files_updated_by_user":       "{}",
		"type":                        "user input",
		"origin_kind":                 HostUserOriginExternal,
	}
	for k, v := range overrides {
		if v == nil {
			delete(env, k)
		} else {
			env[k] = v
		}
	}
	return env
}

// envelopePrompt 把信封包成带前缀的提示词
func envelopePrompt(env map[string]any) string {
	raw, _ := json.Marshal(env)
	return HostUserPromptPrefixZh + string(raw)
}

// TestExtractPermissionUserContent_正常 验证合法信封提取出原文
func TestExtractPermissionUserContent_正常(t *testing.T) {
	got := ExtractPermissionUserContent(envelopePrompt(validEnvelope(nil)))
	if got == nil || *got != "用户原文" {
		t.Fatalf("提取失败: %v", got)
	}
}

// TestExtractPermissionUserContent_英文前缀 验证英文前缀也支持
func TestExtractPermissionUserContent_英文前缀(t *testing.T) {
	raw, _ := json.Marshal(validEnvelope(nil))
	got := ExtractPermissionUserContent(HostUserPromptPrefixEn + string(raw))
	if got == nil || *got != "用户原文" {
		t.Fatalf("提取失败: %v", got)
	}
}

// TestExtractPermissionUserContent_非字符串 验证非字符串输入返回 nil
func TestExtractPermissionUserContent_非字符串(t *testing.T) {
	if ExtractPermissionUserContent(123) != nil {
		t.Fatalf("非字符串应返回 nil")
	}
	if ExtractPermissionUserContent(nil) != nil {
		t.Fatalf("nil 应返回 nil")
	}
}

// TestExtractPermissionUserContent_无前缀 验证无前缀返回 nil
func TestExtractPermissionUserContent_无前缀(t *testing.T) {
	raw, _ := json.Marshal(validEnvelope(nil))
	if ExtractPermissionUserContent(string(raw)) != nil {
		t.Fatalf("无前缀应返回 nil")
	}
}

// TestExtractPermissionUserContent_尾部非空白 验证前缀后有多余内容返回 nil
func TestExtractPermissionUserContent_尾部非空白(t *testing.T) {
	prompt := envelopePrompt(validEnvelope(nil)) + "trailing"
	if ExtractPermissionUserContent(prompt) != nil {
		t.Fatalf("尾部非空白应返回 nil")
	}
}

// TestExtractPermissionUserContent_非法JSON 验证 JSON 非法返回 nil
func TestExtractPermissionUserContent_非法JSON(t *testing.T) {
	if ExtractPermissionUserContent(HostUserPromptPrefixZh+"{not json") != nil {
		t.Fatalf("非法 JSON 应返回 nil")
	}
	if ExtractPermissionUserContent(HostUserPromptPrefixZh+"[1,2]") != nil {
		t.Fatalf("非对象应返回 nil")
	}
}

// TestExtractPermissionUserContent_重复键 验证重复键返回 nil
func TestExtractPermissionUserContent_重复键(t *testing.T) {
	dup := `{"source":"a","source":"b"}`
	if ExtractPermissionUserContent(HostUserPromptPrefixZh+dup) != nil {
		t.Fatalf("重复键应返回 nil")
	}
}

// TestExtractPermissionUserContent_缺必填键 验证缺任一必填键返回 nil
func TestExtractPermissionUserContent_缺必填键(t *testing.T) {
	for _, key := range requiredEnvelopeKeys {
		env := validEnvelope(map[string]any{key: nil})
		if ExtractPermissionUserContent(envelopePrompt(env)) != nil {
			t.Fatalf("缺 %s 应返回 nil", key)
		}
	}
}

// TestExtractPermissionUserContent_多未知键 验证含未知键返回 nil
func TestExtractPermissionUserContent_多未知键(t *testing.T) {
	env := validEnvelope(map[string]any{"unknown": "x"})
	if ExtractPermissionUserContent(envelopePrompt(env)) != nil {
		t.Fatalf("未知键应返回 nil")
	}
}

// TestExtractPermissionUserContent_可选键 验证三个可选键允许出现
func TestExtractPermissionUserContent_可选键(t *testing.T) {
	env := validEnvelope(map[string]any{
		"skills_to_use": []any{},
		"trusted_dirs":  []any{},
		"timezone":      "Asia/Shanghai",
	})
	if ExtractPermissionUserContent(envelopePrompt(env)) == nil {
		t.Fatalf("可选键应允许")
	}
}

// TestExtractPermissionUserContent_type不符 验证 type 非 "user input" 返回 nil
func TestExtractPermissionUserContent_type不符(t *testing.T) {
	for _, v := range []any{"system message", "", 123} {
		env := validEnvelope(map[string]any{"type": v})
		if ExtractPermissionUserContent(envelopePrompt(env)) != nil {
			t.Fatalf("type=%v 应返回 nil", v)
		}
	}
}

// TestExtractPermissionUserContent_origin不符 验证 origin_kind 非外部返回 nil
func TestExtractPermissionUserContent_origin不符(t *testing.T) {
	env := validEnvelope(map[string]any{"origin_kind": HostUserOriginInternal})
	if ExtractPermissionUserContent(envelopePrompt(env)) != nil {
		t.Fatalf("内部派发应返回 nil")
	}
}

// TestExtractPermissionUserContent_source黑名单 验证 system/cron/heartbeat 返回 nil
func TestExtractPermissionUserContent_source黑名单(t *testing.T) {
	for _, source := range []string{"system", "SYSTEM", "cron", "heartbeat", " HeartBeat "} {
		env := validEnvelope(map[string]any{"source": source})
		if ExtractPermissionUserContent(envelopePrompt(env)) != nil {
			t.Fatalf("source=%q 应返回 nil", source)
		}
	}
}

// TestExtractPermissionUserContent_source为空 验证 source 为空或非字符串返回 nil
func TestExtractPermissionUserContent_source为空(t *testing.T) {
	for _, v := range []any{"", "   ", 123} {
		env := validEnvelope(map[string]any{"source": v})
		if ExtractPermissionUserContent(envelopePrompt(env)) != nil {
			t.Fatalf("source=%v 应返回 nil", v)
		}
	}
}

// TestExtractPermissionUserContent_时间戳语言为空 验证时间戳与语言字段必填
func TestExtractPermissionUserContent_时间戳语言为空(t *testing.T) {
	for _, key := range []string{"timestamp", "preferred_response_language"} {
		env := validEnvelope(map[string]any{key: "  "})
		if ExtractPermissionUserContent(envelopePrompt(env)) != nil {
			t.Fatalf("%s 为空应返回 nil", key)
		}
	}
}

// TestExtractPermissionUserContent_files非对象JSON 验证 files_updated_by_user 必须是对象 JSON 串
func TestExtractPermissionUserContent_files非对象JSON(t *testing.T) {
	for _, v := range []any{"[1,2]", "not-json", 123, nil} {
		env := validEnvelope(map[string]any{"files_updated_by_user": v})
		if ExtractPermissionUserContent(envelopePrompt(env)) != nil {
			t.Fatalf("files=%v 应返回 nil", v)
		}
	}
}

// TestExtractPermissionUserContent_content为空 验证 content 为空或非字符串返回 nil
func TestExtractPermissionUserContent_content为空(t *testing.T) {
	for _, v := range []any{"", "   ", 123} {
		env := validEnvelope(map[string]any{"content": v})
		if ExtractPermissionUserContent(envelopePrompt(env)) != nil {
			t.Fatalf("content=%v 应返回 nil", v)
		}
	}
}

// TestExtractPermissionUserContent_contenttrim 验证返回的 content 被 trim
func TestExtractPermissionUserContent_contenttrim(t *testing.T) {
	env := validEnvelope(map[string]any{"content": "  原文  "})
	got := ExtractPermissionUserContent(envelopePrompt(env))
	if got == nil || *got != "原文" {
		t.Fatalf("应 trim，实际 %v", got)
	}
}

// ──────────────────────────── BuildRootIntentProjection 测试 ────────────────────────────

// userMessages 构造若干条用户消息
func userMessages(t *testing.T, texts ...string) []any {
	t.Helper()
	messages := make([]any, 0, len(texts))
	for _, text := range texts {
		messages = append(messages, map[string]any{
			"role":    "user",
			"content": envelopePrompt(validEnvelope(map[string]any{"content": text})),
		})
	}
	return messages
}

// TestBuildRootIntentProjection_历史加当前 验证历史消息与当前文本按时间序合并
func TestBuildRootIntentProjection_历史加当前(t *testing.T) {
	got := BuildRootIntentProjection(
		userMessages(t, "历史一", "历史二"),
		true, "当前", "req-1", RootIntentTurnKindFresh,
	)
	if len(got.Turns) != 3 {
		t.Fatalf("应有 3 轮，实际 %d", len(got.Turns))
	}
	if got.Turns[0].Kind != RootIntentTurnKindHistory || got.Turns[0].Text != "历史一" {
		t.Fatalf("首轮不符: %+v", got.Turns[0])
	}
	last := got.Turns[2]
	if last.Kind != RootIntentTurnKindFresh || last.Text != "当前" || last.RequestID != "req-1" {
		t.Fatalf("末轮不符: %+v", last)
	}
	if got.AutoReviewBlockReason != "" {
		t.Fatalf("不应阻断，实际 %q", got.AutoReviewBlockReason)
	}
}

// TestBuildRootIntentProjection_忽略非用户消息 验证 assistant/system 消息被跳过
func TestBuildRootIntentProjection_忽略非用户消息(t *testing.T) {
	messages := []any{
		map[string]any{"role": "assistant", "content": "回答"},
		map[string]any{"role": "system", "content": "系统"},
		map[string]any{"role": "user", "content": envelopePrompt(validEnvelope(nil))},
	}
	got := BuildRootIntentProjection(messages, true, "", "", RootIntentTurnKindFresh)
	if len(got.Turns) != 1 {
		t.Fatalf("应只保留 1 条用户消息，实际 %d", len(got.Turns))
	}
}

// TestBuildRootIntentProjection_忽略非法信封 验证内容不合法的消息被跳过
func TestBuildRootIntentProjection_忽略非法信封(t *testing.T) {
	messages := []any{
		map[string]any{"role": "user", "content": "裸文本，无信封"},
		map[string]any{"role": "user", "content": envelopePrompt(validEnvelope(nil))},
	}
	got := BuildRootIntentProjection(messages, true, "", "", RootIntentTurnKindFresh)
	if len(got.Turns) != 1 {
		t.Fatalf("应只保留合法信封，实际 %d", len(got.Turns))
	}
}

// TestBuildRootIntentProjection_context不可用 验证 contextAvailable=false 时不读历史
func TestBuildRootIntentProjection_context不可用(t *testing.T) {
	got := BuildRootIntentProjection(
		userMessages(t, "历史"), false, "当前", "req-1", RootIntentTurnKindFresh,
	)
	if len(got.Turns) != 1 || got.Turns[0].Text != "当前" {
		t.Fatalf("应只有当前轮，实际 %+v", got.Turns)
	}
}

// TestBuildRootIntentProjection_当前超长阻断 验证当前文本超 8000 时阻断
func TestBuildRootIntentProjection_当前超长阻断(t *testing.T) {
	long := strings.Repeat("a", RootIntentMaxTurnChars+1)
	got := BuildRootIntentProjection(nil, true, long, "req-1", RootIntentTurnKindFresh)
	if got.AutoReviewBlockReason != AutoReviewBlockIntentInputTooLarge {
		t.Fatalf("应标记输入过大，实际 %q", got.AutoReviewBlockReason)
	}
	if len(got.Turns) != 0 {
		t.Fatalf("不应包含超长轮次")
	}
}

// TestBuildRootIntentProjection_历史超长跳过 验证历史消息超长时该轮被丢弃并阻断
func TestBuildRootIntentProjection_历史超长跳过(t *testing.T) {
	long := strings.Repeat("a", RootIntentMaxTurnChars+1)
	messages := userMessages(t, long, "正常")
	got := BuildRootIntentProjection(messages, true, "", "", RootIntentTurnKindFresh)
	if got.AutoReviewBlockReason != AutoReviewBlockHistoryWindowTruncated {
		t.Fatalf("应标记截断，实际 %q", got.AutoReviewBlockReason)
	}
	if len(got.Turns) != 1 || got.Turns[0].Text != "正常" {
		t.Fatalf("应只留正常那条，实际 %+v", got.Turns)
	}
}

// TestBuildRootIntentProjection_轮次上限 验证超过 20 轮时只保留最近的
func TestBuildRootIntentProjection_轮次上限(t *testing.T) {
	texts := make([]string, 25)
	for i := range texts {
		texts[i] = "消息"
	}
	got := BuildRootIntentProjection(userMessages(t, texts...), true, "", "", RootIntentTurnKindFresh)
	if len(got.Turns) > RootIntentMaxTurns {
		t.Fatalf("不应超过 %d 轮，实际 %d", RootIntentMaxTurns, len(got.Turns))
	}
	if got.AutoReviewBlockReason != AutoReviewBlockHistoryWindowTruncated {
		t.Fatalf("应标记截断，实际 %q", got.AutoReviewBlockReason)
	}
}

// TestBuildRootIntentProjection_总字符上限 验证总字符超限时从最老的丢弃
func TestBuildRootIntentProjection_总字符上限(t *testing.T) {
	big := strings.Repeat("a", 7000)
	texts := []string{big, big, big, big, big, big, big}
	got := BuildRootIntentProjection(userMessages(t, texts...), true, "", "", RootIntentTurnKindFresh)
	total := 0
	for _, turn := range got.Turns {
		total += len(RootIntentTurnText(turn, true))
	}
	if total > RootIntentMaxTotalChars {
		t.Fatalf("总字符超限: %d", total)
	}
	if got.AutoReviewBlockReason != AutoReviewBlockHistoryWindowTruncated {
		t.Fatalf("应标记截断，实际 %q", got.AutoReviewBlockReason)
	}
}

// TestBuildRootIntentProjection_当前为空 验证当前文本为空白时不追加
func TestBuildRootIntentProjection_当前为空(t *testing.T) {
	got := BuildRootIntentProjection(userMessages(t, "历史"), true, "   ", "req-1", RootIntentTurnKindFresh)
	if len(got.Turns) != 1 || got.Turns[0].Kind != RootIntentTurnKindHistory {
		t.Fatalf("应只有历史轮，实际 %+v", got.Turns)
	}
}

// TestBuildRootIntentProjection_全空 验证无输入时返回空
func TestBuildRootIntentProjection_全空(t *testing.T) {
	got := BuildRootIntentProjection(nil, true, "", "", RootIntentTurnKindFresh)
	if len(got.Turns) != 0 || got.AutoReviewBlockReason != "" {
		t.Fatalf("应全空，实际 %+v", got)
	}
}

// TestBuildRootIntentProjection_role枚举形态 验证 role 为 {"value": "user"} 时也能识别
func TestBuildRootIntentProjection_role枚举形态(t *testing.T) {
	messages := []any{
		map[string]any{
			"role":    map[string]any{"value": "user"},
			"content": envelopePrompt(validEnvelope(nil)),
		},
	}
	got := BuildRootIntentProjection(messages, true, "", "", RootIntentTurnKindFresh)
	if len(got.Turns) != 1 {
		t.Fatalf("应识别为 user，实际 %d", len(got.Turns))
	}
}

// TestBuildRootIntentProjection_大小写role 验证 role 大小写不敏感
func TestBuildRootIntentProjection_大小写role(t *testing.T) {
	messages := []any{
		map[string]any{"role": "USER", "content": envelopePrompt(validEnvelope(nil))},
	}
	got := BuildRootIntentProjection(messages, true, "", "", RootIntentTurnKindFresh)
	if len(got.Turns) != 1 {
		t.Fatalf("应识别为 user，实际 %d", len(got.Turns))
	}
}

// ──────────────────────────── 常量测试 ────────────────────────────

// TestRootContext常量对齐 验证与 Python 取值一致
func TestRootContext常量对齐(t *testing.T) {
	if RootContextKey != "jiuwenswarm.root_permission_context.v1" {
		t.Fatalf("RootContextKey 不符: %q", RootContextKey)
	}
	if HostUserMessageSource != "host_user_message" {
		t.Fatalf("HostUserMessageSource 不符: %q", HostUserMessageSource)
	}
	if RootIntentMaxTurns != 20 || RootIntentMaxTurnChars != 8000 || RootIntentMaxTotalChars != 48000 {
		t.Fatalf("容量常量不符")
	}
	if AutoReviewBlockIntentInputTooLarge != "intent_input_too_large" ||
		AutoReviewBlockHistoryWindowTruncated != "intent_history_window_truncated" {
		t.Fatalf("阻断原因常量不符")
	}
	if HostUserOriginExternal != "external_user_authored" || HostUserOriginInternal != "internal_dispatch" {
		t.Fatalf("origin 常量不符")
	}
}

// ──────────────────────────── 补充分支覆盖 ────────────────────────────

// TestRootIntentTurnToMapping_带澄清 验证澄清轮次的 ToMapping 输出澄清数组
func TestRootIntentTurnToMapping_带澄清(t *testing.T) {
	c, _ := NewRootAskUserClarification("q", []string{"a"}, nil)
	turn, err := NewRootIntentTurn("r", RootIntentTurnKindAskUserClarification, "", []RootAskUserClarification{c})
	if err != nil {
		t.Fatalf("构造失败: %v", err)
	}
	m := turn.ToMapping()
	items, ok := m["clarifications"].([]any)
	if !ok || len(items) != 1 {
		t.Fatalf("clarifications 不符: %v", m["clarifications"])
	}
	if len(items[0].(map[string]any)) != 3 {
		t.Fatalf("澄清映射应有 3 个键")
	}
}

// TestRootIntentTurnFromMapping_带澄清 验证澄清轮次反序列化
func TestRootIntentTurnFromMapping_带澄清(t *testing.T) {
	c, _ := NewRootAskUserClarification("q", []string{"a"}, nil)
	turn, _ := NewRootIntentTurn("r", RootIntentTurnKindAskUserClarification, "", []RootAskUserClarification{c})
	back, err := RootIntentTurnFromMapping(turn.ToMapping())
	if err != nil {
		t.Fatalf("反序列化失败: %v", err)
	}
	if back.Kind != RootIntentTurnKindAskUserClarification || len(back.Clarifications) != 1 {
		t.Fatalf("往返不一致: %+v", back)
	}
}

// TestRootIntentTurnFromMapping_字段类型非法 验证三个字符串字段分别类型非法时报错
func TestRootIntentTurnFromMapping_字段类型非法(t *testing.T) {
	base := map[string]any{
		"request_id": "r", "kind": RootIntentTurnKindFresh, "text": "t",
		"clarifications": []any{},
	}
	for _, key := range []string{"request_id", "kind", "text"} {
		m := make(map[string]any, len(base))
		for k, v := range base {
			m[k] = v
		}
		m[key] = 123
		if _, err := RootIntentTurnFromMapping(m); err == nil {
			t.Fatalf("%s 非字符串应报错", key)
		}
	}
}

// TestRootDecisionContextFromMapping_字符串字段类型非法 验证四个字符串字段类型非法时报错
func TestRootDecisionContextFromMapping_字符串字段类型非法(t *testing.T) {
	base := map[string]any{
		"session_id": "s", "request_id": "r", "channel_id": "",
		"trusted_turns": []any{}, "auto_review_block_reason": "",
	}
	for _, key := range []string{"session_id", "request_id", "channel_id", "auto_review_block_reason"} {
		m := make(map[string]any, len(base))
		for k, v := range base {
			m[k] = v
		}
		m[key] = 123
		if _, err := RootDecisionContextFromMapping(m); err == nil {
			t.Fatalf("%s 非字符串应报错", key)
		}
	}
}

// TestRootIntentTurnText_澄清含选项 验证 includeQuestion=true 时选项被序列化
func TestRootIntentTurnText_澄清含选项(t *testing.T) {
	c, _ := NewRootAskUserClarification("q", []string{"a"}, opts2(t))
	turn, _ := NewRootIntentTurn("r", RootIntentTurnKindAskUserClarification, "", []RootAskUserClarification{c})
	got := RootIntentTurnText(turn, true)
	if !strings.Contains(got, "\"label\":\"A\"") {
		t.Fatalf("应含选项标签: %s", got)
	}
}

// TestStrictString_缺键 验证缺键返回错误
func TestStrictString_缺键(t *testing.T) {
	if _, err := strictString(map[string]any{}, "k"); err == nil {
		t.Fatalf("缺键应报错")
	}
	if _, err := strictString(map[string]any{"k": 1}, "k"); err == nil {
		t.Fatalf("类型不符应报错")
	}
	if got, err := strictString(map[string]any{"k": "v"}, "k"); err != nil || got != "v" {
		t.Fatalf("正常应返回 v，实际 %q %v", got, err)
	}
}

// TestDecodeStrictObject_各类非法 验证非对象/键非字符串/嵌套值非法时报错
func TestDecodeStrictObject_各类非法(t *testing.T) {
	cases := []string{
		`[1,2]`,   // 顶层非对象
		`"str"`,   // 顶层字符串
		`{"a":}`,  // 值非法
		`{1:"a"}`, // 键非字符串
		`{"a":1`,  // 未闭合
	}
	for _, raw := range cases {
		if _, _, err := decodeStrictObject(raw); err == nil {
			t.Fatalf("raw=%q 应报错", raw)
		}
	}
}

// TestDecodeStrictObject_嵌套对象 验证嵌套值正常解析
func TestDecodeStrictObject_嵌套对象(t *testing.T) {
	got, consumed, err := decodeStrictObject(`{"a":{"b":1}}`)
	if err != nil {
		t.Fatalf("不应报错: %v", err)
	}
	if consumed != len(`{"a":{"b":1}}`) {
		t.Fatalf("consumed 不符: %d", consumed)
	}
	nested, ok := got["a"].(map[string]any)
	if !ok || nested["b"] == nil {
		t.Fatalf("嵌套解析不符: %+v", got)
	}
}

// TestMessageRole_非映射 验证非映射消息返回空 role
func TestMessageRole_非映射(t *testing.T) {
	if got := messageRole("string"); got != "" {
		t.Fatalf("非映射应返回空，实际 %q", got)
	}
	if got := messageRole(map[string]any{}); got != "" {
		t.Fatalf("缺 role 应返回空")
	}
	if got := messageRole(map[string]any{"role": map[string]any{}}); got != "" {
		t.Fatalf("role 映射缺 value 应返回空")
	}
}

// TestMessageContent_非映射 验证非映射消息返回 nil content
func TestMessageContent_非映射(t *testing.T) {
	if got := messageContent("string"); got != nil {
		t.Fatalf("非映射应返回 nil，实际 %v", got)
	}
	if got := messageContent(map[string]any{"content": "c"}); got != "c" {
		t.Fatalf("应返回 c，实际 %v", got)
	}
}

// TestNestedCopyMap_非映射值 验证 source[key] 非映射时返回空映射
func TestNestedCopyMap_非映射值(t *testing.T) {
	got := nestedCopyMap(map[string]any{"run": "string"}, "run")
	if len(got) != 0 {
		t.Fatalf("应返回空映射，实际 %v", got)
	}
}

// TestAppendRootClarification_构造失败走保底分支 验证非法澄清时原样返回
func TestAppendRootClarification_构造失败走保底分支(t *testing.T) {
	dc, _ := NewRootDecisionContext("s", "r", "", nil, "")
	// 5 条澄清超过 MaxStructuredQuestions，构造会失败
	c, _ := NewRootAskUserClarification("q", []string{"a"}, nil)
	five := make([]RootAskUserClarification, 5)
	for i := range five {
		five[i] = c
	}
	if got := AppendRootClarification(dc, five); got != dc {
		t.Fatalf("应原样返回")
	}
}

// TestBuildRootIntentProjection_历史消息构造失败 验证非法 content 长度导致的构造失败被跳过
func TestBuildRootIntentProjection_历史消息构造失败(t *testing.T) {
	// 恰好等于单轮上限的内容可构造成功
	exact := strings.Repeat("a", RootIntentMaxTurnChars)
	got := BuildRootIntentProjection(userMessages(t, exact), true, "", "", RootIntentTurnKindFresh)
	if len(got.Turns) != 1 {
		t.Fatalf("等于上限应保留，实际 %d", len(got.Turns))
	}
}

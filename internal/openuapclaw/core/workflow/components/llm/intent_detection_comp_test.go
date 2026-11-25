package llm

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/common/exception"
	ceschema "github.com/uapclaw/uapclaw-go/internal/common/schema"
	ceengine "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/context_engine"
	iface "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/context_engine/interface"
	ceengschema "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/context_engine/schema"
	modelclients "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/foundation/llm/model_clients"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/graph"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/session"

	llmschema "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/foundation/llm/schema"
)

// TestNewIntentDetectionExecutable_分类列表派生ZH 构造期派生（Python :435-444 +
// :420-424）：分类1..N + 默认分类头部追加；名字表头部插"默认意图"
func TestNewIntentDetectionExecutable_分类列表派生ZH(t *testing.T) {
	e := NewIntentDetectionExecutable(&IntentDetectionCompConfig{
		CategoryNameList: []string{"退款申请", "物流查询"},
	})
	// 分类表 = [分类0, 分类1, 分类2]（默认头部）
	if len(e.defaultCfg.CategoryList) != 3 || e.defaultCfg.CategoryList[0] != "分类0" ||
		e.defaultCfg.CategoryList[1] != "分类1" || e.defaultCfg.CategoryList[2] != "分类2" {
		t.Fatalf("分类表派生错误: %v", e.defaultCfg.CategoryList)
	}
	// 名字表 = [默认意图, 退款申请, 物流查询]
	if len(e.cfg.CategoryNameList) != 3 || e.cfg.CategoryNameList[0] != "默认意图" ||
		e.cfg.CategoryNameList[1] != "退款申请" {
		t.Fatalf("名字表头部追加错误: %v", e.cfg.CategoryNameList)
	}
	if e.defaultCfg.DefaultClass != "分类0" {
		t.Fatalf("默认分类错误: %q", e.defaultCfg.DefaultClass)
	}
	if !e.defaultCfg.EnableInput {
		t.Fatalf("EnableInput 默认应为 true（Python dataclass enable_input: bool = True，:439 构造未显式传）")
	}
}

// TestNewIntentDetectionExecutable_英文派生 en → CategoryN / Category0 / Default intent
func TestNewIntentDetectionExecutable_英文派生(t *testing.T) {
	e := NewIntentDetectionExecutable(&IntentDetectionCompConfig{
		CategoryNameList: []string{"Refund"},
		AcceptLanguage:   "en",
	})
	if len(e.defaultCfg.CategoryList) != 2 || e.defaultCfg.CategoryList[0] != "Category0" ||
		e.defaultCfg.CategoryList[1] != "Category1" {
		t.Fatalf("英文分类表派生错误: %v", e.defaultCfg.CategoryList)
	}
	if e.cfg.CategoryNameList[0] != "Default intent" {
		t.Fatalf("英文默认名错误: %q", e.cfg.CategoryNameList[0])
	}
	if e.defaultCfg.DefaultClass != "Category0" {
		t.Fatalf("英文默认分类错误: %q", e.defaultCfg.DefaultClass)
	}
}

// TestNewIntentDetectionExecutable_空语言兜底zh accept_language 空串 → getattr 兜底 'zh'
func TestNewIntentDetectionExecutable_空语言兜底zh(t *testing.T) {
	e := NewIntentDetectionExecutable(&IntentDetectionCompConfig{
		CategoryNameList: []string{"x"},
		AcceptLanguage:   "",
	})
	if e.defaultCfg.CategoryList[0] != "分类0" {
		t.Fatalf("空语言应兜底 zh: %v", e.defaultCfg.CategoryList)
	}
}

// TestGetDefaultTemplate_双语言 zh/en 模板消息逐字（Python get_default_template :135-149）
func TestGetDefaultTemplate_双语言(t *testing.T) {
	tpl := GetDefaultTemplate("zh")
	msgs, err := tpl.ToMessages()
	if err != nil || len(msgs) != 2 {
		t.Fatalf("zh 模板应含 2 条消息: %v %v", msgs, err)
	}
	if msgs[0].GetRole() != llmschema.RoleTypeSystem ||
		msgs[0].GetContent().String() != defaultSystemPromptZH {
		t.Fatalf("zh 系统消息不符: %v", msgs[0].GetContent().String())
	}
	if msgs[1].GetContent().String() != defaultUserPromptZH {
		t.Fatalf("zh 用户消息不符（应逐字）")
	}

	enMsgs, err := GetDefaultTemplate("en").ToMessages()
	if err != nil || len(enMsgs) != 2 ||
		enMsgs[0].GetContent().String() != defaultSystemPromptEN ||
		enMsgs[1].GetContent().String() != defaultUserPromptEN {
		t.Fatalf("en 模板不符")
	}
}

// TestGetDefaultTemplate_其他语言回落zh Python :135-149 非 'en' 一律 zh 段
func TestGetDefaultTemplate_其他语言回落zh(t *testing.T) {
	msgs, _ := GetDefaultTemplate("jp").ToMessages()
	if msgs[0].GetContent().String() != defaultSystemPromptZH {
		t.Fatalf("非 en 应回落 zh 模板")
	}
}

// ──────────────────────────── Task 2 解析层 ────────────────────────────

// TestPostProcessIntentDetection_全分支 Python :377-414 逐分支（spec §5 覆盖点 4）
func TestPostProcessIntentDetection_全分支(t *testing.T) {
	e := NewIntentDetectionExecutable(&IntentDetectionCompConfig{CategoryNameList: []string{"退款"}})

	// 分支1：非 JSON 文本 → 默认分类 + jsonParseFailReason（zh）
	class, reason := e.postProcessIntentDetection("完全不是JSON")
	if class != "分类0" || !strings.Contains(reason, "格式不符合有效的JSON规范") {
		t.Fatalf("分支1错误: %q %q", class, reason)
	}

	// 分支1b：true/false/null 修复后可解析（LLM 常见输出，Python :202）
	class, _ = e.postProcessIntentDetection(`{"class": "分类1", "reason": "r", "ok": true}`)
	if class != "分类1" {
		t.Fatalf("分支1b 修复失败: %q", class)
	}

	// 分支1c：单引号 dict（ast.literal_eval 特性）
	class, _ = e.postProcessIntentDetection(`{'class': '分类1', 'reason': 'r'}`)
	if class != "分类1" {
		t.Fatalf("分支1c 单引号失败: %q", class)
	}

	// 分支2：解析成功但非 dict（如纯数字）→ jsonParseFailReason
	class, reason = e.postProcessIntentDetection("123")
	if class != "分类0" || !strings.Contains(reason, "格式不符合有效的JSON规范") {
		t.Fatalf("分支2错误: %q %q", class, reason)
	}

	// 分支3：缺 class 键 → 默认分类 + classKeyMissingReason
	class, reason = e.postProcessIntentDetection(`{"reason": "r"}`)
	if class != "分类0" || !strings.Contains(reason, "缺少必要的输出") {
		t.Fatalf("分支3错误: %q %q", class, reason)
	}

	// 分支3b：class falsy 数值 0 → 同缺键路径（审查拍板②A，Python `not 0` 语义）
	class, reason = e.postProcessIntentDetection(`{"class": 0, "reason": "r"}`)
	if class != "分类0" || !strings.Contains(reason, "缺少必要的输出") {
		t.Fatalf("分支3b falsy 数值错误: %q %q", class, reason)
	}

	// 分支3c：class 空容器 [] → 同缺键路径（Python `not []` 语义）
	class, reason = e.postProcessIntentDetection(`{"class": [], "reason": "r"}`)
	if class != "分类0" || !strings.Contains(reason, "缺少必要的输出") {
		t.Fatalf("分支3c falsy 空容器错误: %q %q", class, reason)
	}

	// 分支4：类别不在列表（带杂音被规整成 分类9，不在表内）→ 默认分类 + validationFailReason
	class, reason = e.postProcessIntentDetection(`{"class": "分类 9\n", "reason": "r"}`)
	if class != "分类0" || !strings.Contains(reason, "不在预定义的分类列表") {
		t.Fatalf("分支4错误: %q %q", class, reason)
	}

	// 分支5：规整命中——"分类 1\n" 去空白后正则捕获 分类1（Python :398-405）
	class, _ = e.postProcessIntentDetection(`{"class": "分类 1\n", "reason": "r"}`)
	if class != "分类1" {
		t.Fatalf("分支5 规整失败: %q", class)
	}
}

// TestPostProcessIntentDetection_英文分支 en 正则 CategoryN + 数字组重排（Python :383/402-405）
func TestPostProcessIntentDetection_英文分支(t *testing.T) {
	e := NewIntentDetectionExecutable(&IntentDetectionCompConfig{
		CategoryNameList: []string{"Refund"}, AcceptLanguage: "en",
	})
	// 大小写不敏感 + 杂音 → 规整 Category1
	class, _ := e.postProcessIntentDetection(`{"class": "  category 1 x\n", "reason": "r"}`)
	if class != "Category1" {
		t.Fatalf("英文规整失败: %q", class)
	}
	// 失败文案为英文
	_, reason := e.postProcessIntentDetection("bad")
	if !strings.Contains(reason, "does not conform to valid JSON format") {
		t.Fatalf("英文失败文案不符: %q", reason)
	}
}

// TestParseDetectionResult_excludeDefaults Python :329-334 model_dump(exclude_defaults=True)
func TestParseDetectionResult_excludeDefaults(t *testing.T) {
	e := NewIntentDetectionExecutable(&IntentDetectionCompConfig{CategoryNameList: []string{"退款"}})

	// 正常命中：id=1 非默认、name="退款" 非默认、reason="r" 非默认 → 三键齐
	out := e.parseDetectionResult(`{"class": "分类1", "reason": "r"}`)
	if out[classificationIDKey] != 1 || out["reason"] != "r" || out[classificationNameKey] != "退款" {
		t.Fatalf("正常命中输出错误: %v", out)
	}

	// 解析失败：class=分类0（id=0 ≠ -1 保留）、reason 被回落文案覆盖非空、
	// name=分类表[0]="分类0" 非默认 → 但 reason/name 键均非默认值
	out = e.parseDetectionResult("bad")
	if out[classificationIDKey] != 0 {
		t.Fatalf("默认分类 id 应为 0: %v", out)
	}

	// reason 为空串时剔除（= 默认 ""）：构造 class 命中但 reason 缺失
	out = e.parseDetectionResult(`{"class": "分类1"}`)
	if _, ok := out["reason"]; ok {
		t.Fatalf("reason 空串应被剔除: %v", out)
	}
}

// TestPyLiteralEval_Python字面量子集 D-23-1：str/bool/None/数字/列表/元组/字典/嵌套
func TestPyLiteralEval_Python字面量子集(t *testing.T) {
	cases := []struct {
		in   string
		want any
	}{
		{`"abc"`, "abc"}, {`'abc'`, "abc"},
		{`True`, true}, {`False`, false}, {`None`, nil},
		{`123`, int64(123)}, {`-1.5`, -1.5},
		{`[1, 'a', True]`, []any{int64(1), "a", true}},
		{`(1, 2)`, []any{int64(1), int64(2)}},
		{`{'a': 1, 'b': [True, None]}`, map[string]any{"a": int64(1), "b": []any{true, nil}}},
	}
	for _, c := range cases {
		got, err := pyLiteralEval(c.in)
		if err != nil {
			t.Fatalf("pyLiteralEval(%q) err: %v", c.in, err)
		}
		if !reflect.DeepEqual(got, c.want) {
			t.Fatalf("pyLiteralEval(%q) = %#v, want %#v", c.in, got, c.want)
		}
	}
	// 残缺输入报错（Python literal_eval 语义）
	if _, err := pyLiteralEval(`{'a': `); err == nil {
		t.Fatal("残缺输入应报错")
	}
	if _, err := pyLiteralEval(`{'a': 1}x`); err == nil {
		t.Fatal("尾随字符应报错")
	}
}

// ──────────────────────────── Task 3 模型双路径 ────────────────────────────

// TestCreateLLMInstance_双缺配置报错 Python :255-259：model_id 与双 config 皆缺 →
// COMPONENT_INTENT_DETECTION_INVOKE_CALL_FAILED
// assertIntentCode 错误码断言助手（同 llm_comp_test assertLLMCode 惯例）
func assertIntentCode(t *testing.T, err error, status exception.StatusCode) {
	t.Helper()
	be, ok := err.(*exception.BaseError)
	if !ok {
		t.Fatalf("期望 BaseError，实际 %T", err)
	}
	if be.Status() != status {
		t.Fatalf("错误码不符: got %v want %v", be.Status(), status)
	}
}

func TestCreateLLMInstance_双缺配置报错(t *testing.T) {
	e := NewIntentDetectionExecutable(&IntentDetectionCompConfig{})
	_, err := e.createLLMInstance(context.Background())
	if err == nil {
		t.Fatal("期望 IntentDetectionInvokeCallFailed")
	}
	assertIntentCode(t, err, exception.StatusComponentIntentDetectionInvokeCallFailed)
}

// TestCreateLLMInstance_直构路径 client/request config 齐备 → NewModel 非 nil
func TestCreateLLMInstance_直构路径(t *testing.T) {
	e := NewIntentDetectionExecutable(&IntentDetectionCompConfig{
		ModelClientConfig: &llmschema.ModelClientConfig{ClientID: "c1", ClientProvider: "llm_OpenAI", APIKey: "k"},
		ModelConfig:       &llmschema.ModelRequestConfig{ModelName: "gpt"},
	})
	inv, err := e.createLLMInstance(context.Background())
	if err != nil || inv == nil {
		t.Fatalf("直构路径失败: %v", err)
	}
}

// TestInitializeIfNeeded_包装错误 Python :266-276：创建异常包装 LLMInitFailed
func TestInitializeIfNeeded_包装错误(t *testing.T) {
	e := NewIntentDetectionExecutable(&IntentDetectionCompConfig{}) // 双缺 → create 报错
	err := e.initializeIfNeeded(context.Background())
	if err == nil {
		t.Fatal("期望 IntentDetectionLLMInitFailed 包装")
	}
	assertIntentCode(t, err, exception.StatusComponentIntentDetectionLLMInitFailed)
	if e.initialized {
		t.Fatal("失败不应置位 initialized")
	}
}

// ──────────────────────────── Task 4 输入组装 ────────────────────────────

// TestPrepareDetectionInputs_槽位填充 Python :278-312 十槽
func TestPrepareDetectionInputs_槽位填充(t *testing.T) {
	e := NewIntentDetectionExecutable(&IntentDetectionCompConfig{
		CategoryNameList: []string{"退款"},
		UserPrompt:       "客服语境",
		ExampleContent:   []string{"示例1", "示例2"},
	})
	inputs := map[string]any{"query": "我的快递到哪了"}
	cur, err := e.prepareDetectionInputs(inputs, nil)
	if err != nil {
		t.Fatal(err)
	}
	if cur[userPromptKey] != "客服语境" {
		t.Fatalf("user_prompt 槽错误: %v", cur[userPromptKey])
	}
	if cur[categoryInfoKey] != "0: 默认意图\n1: 退款" {
		t.Fatalf("category_info 槽错误: %q", cur[categoryInfoKey])
	}
	if cur[defaultClassKey] != "分类0" {
		t.Fatalf("default_class 槽错误: %v", cur[defaultClassKey])
	}
	if cur[exampleContentKey] != "示例1\n\n示例2" {
		t.Fatalf("example_content 槽错误: %q", cur[exampleContentKey])
	}
	if cur[chatHistoryKey] != "" {
		t.Fatalf("chat_history 槽错误: %q", cur[chatHistoryKey])
	}
	if cur[inputKey] != "我的快递到哪了" {
		t.Fatalf("input 槽错误: %v", cur[inputKey])
	}
	if cur[enableHistoryKey] != false || cur[enableInputKey] != true {
		t.Fatalf("enable 开关槽错误: %v %v", cur[enableHistoryKey], cur[enableInputKey])
	}
	if cur[chatHistoryMaxTurnKey] != 3 {
		t.Fatalf("max_turn 槽错误: %v", cur[chatHistoryMaxTurnKey])
	}
	if gi, ok := cur["global_intent_map"].([]any); !ok || len(gi) != 0 {
		t.Fatalf("global_intent_map 槽错误: %v", cur["global_intent_map"])
	}
}

// TestPrepareDetectionInputs_历史覆盖 enable_history=true 时 CHAT_HISTORY 槽被格式化覆盖
func TestPrepareDetectionInputs_历史覆盖(t *testing.T) {
	e := NewIntentDetectionExecutable(&IntentDetectionCompConfig{
		CategoryNameList: []string{"退款"}, EnableHistory: true,
	})
	history := []llmschema.BaseMessage{
		llmschema.NewUserMessage("你好"),
		llmschema.NewAssistantMessage("您好"),
	}
	cur, err := e.prepareDetectionInputs(map[string]any{"query": "q"}, history)
	if err != nil {
		t.Fatal(err)
	}
	if cur[chatHistoryKey] != "用户: 你好\n助手: 您好\n" {
		t.Fatalf("历史格式化错误: %q", cur[chatHistoryKey])
	}
}

// TestPrepareDetectionInputs_query缺失报错 Python :300-308 → InputParamError
func TestPrepareDetectionInputs_query缺失报错(t *testing.T) {
	e := NewIntentDetectionExecutable(&IntentDetectionCompConfig{CategoryNameList: []string{"x"}})
	_, err := e.prepareDetectionInputs(map[string]any{}, nil)
	assertIntentCode(t, err, exception.StatusComponentIntentDetectionInputParamError)
}

// TestFormatChatHistory_角色映射与截尾 Python :314-322：未知角色跳过 + 尾部截 N 条
func TestFormatChatHistory_角色映射与截尾(t *testing.T) {
	e := NewIntentDetectionExecutable(&IntentDetectionCompConfig{CategoryNameList: []string{"x"}})
	e.cfg.ChatHistoryMaxTurn = 2
	history := []llmschema.BaseMessage{
		llmschema.NewUserMessage("h1"),
		llmschema.NewUserMessage("h2"),
		llmschema.NewUserMessage("h3"),
		&fakeUnknownRoleMessage{content: "unknown"},
		llmschema.NewUserMessage("h4"),
	}
	got := e.formatChatHistory(history)
	// 末 2 条 = [alien, h4]，alien 角色不在表内被跳过 → 只剩 h4
	if got != "用户: h4\n" {
		t.Fatalf("截尾+角色映射错误: %q", got)
	}
}

// fakeUnknownRoleMessage 非法 role 消息（角色映射跳过分支）
type fakeUnknownRoleMessage struct{ content string }

func (f *fakeUnknownRoleMessage) GetRole() llmschema.RoleType { return llmschema.RoleType(99) }
func (f *fakeUnknownRoleMessage) GetContent() llmschema.MessageContent {
	return llmschema.NewTextContent(f.content)
}
func (f *fakeUnknownRoleMessage) SetRole(llmschema.RoleType)          {}
func (f *fakeUnknownRoleMessage) SetContent(llmschema.MessageContent) {}
func (f *fakeUnknownRoleMessage) GetName() string                     { return "" }
func (f *fakeUnknownRoleMessage) SetName(string)                      {}
func (f *fakeUnknownRoleMessage) GetMetadata() map[string]any         { return nil }
func (f *fakeUnknownRoleMessage) SetMetadata(map[string]any)          {}

// ──────────────────────────── Task 5 invokeEntry 主链 ────────────────────────────

// fakeLLMInvoker LLM 网络边界替身（feedback-real-chain-testing：仅边界允许），
// 捕获入参消息供断言
type fakeLLMInvoker struct {
	content  string
	captured []llmschema.BaseMessage
}

func (f *fakeLLMInvoker) Invoke(_ context.Context, msgs modelclients.MessagesParam,
	_ ...modelclients.InvokeOption) (*llmschema.AssistantMessage, error) {
	f.captured = msgs.Messages()
	return llmschema.NewAssistantMessage(f.content), nil
}

func (f *fakeLLMInvoker) Stream(_ context.Context, _ modelclients.MessagesParam,
	_ ...modelclients.StreamOption) (<-chan *llmschema.AssistantMessageChunk, error) {
	return nil, errors.New("意图识别组件不使用流式")
}

// newTestModelContext 真实 ModelContext（真实 ContextEngine + 真实 Session 管线）
func newTestModelContext(t *testing.T) iface.ModelContext {
	t.Helper()
	ce := ceengine.NewContextEngine(ceengschema.NewContextEngineConfig())
	mc, err := ce.CreateContext(context.Background(), "ctx-itd",
		session.NewSession(session.WithSessionID("s-itd")))
	if err != nil || mc == nil {
		t.Fatalf("真实 CreateContext 失败: %v", err)
	}
	return mc
}

// TestInvokeEntry_历史真实链 spec §5 覆盖点 3：取 N 轮 + user/assistant 写回（真实链）
func TestInvokeEntry_历史真实链(t *testing.T) {
	e := NewIntentDetectionExecutable(&IntentDetectionCompConfig{
		CategoryNameList: []string{"退款"}, EnableHistory: true, ChatHistoryMaxTurn: 1,
	})
	fake := &fakeLLMInvoker{content: `{"class": "分类1", "reason": "r"}`}
	e.llModel = fake
	e.initialized = true

	mc := newTestModelContext(t)
	// 预置 2 轮历史
	_, _ = mc.AddMessages(ctxBg(t), []llmschema.BaseMessage{
		llmschema.NewUserMessage("第一轮问"),
		llmschema.NewAssistantMessage("第一轮答"),
		llmschema.NewUserMessage("第二轮问"),
		llmschema.NewAssistantMessage("第二轮答"),
	})
	sess := newTestNodeSession(t)

	out, err := e.invokeEntry(ctxBg(t), map[string]any{"query": "我的快递到哪了"}, sess, mc)
	if err != nil {
		t.Fatalf("invokeEntry 失败: %v", err)
	}
	// 断言1：模板 chat_history 恰 1 轮（第二轮），渲染后的 user 消息含该历史文本
	if len(fake.captured) != 2 {
		t.Fatalf("应产出 system+user 双消息: %d", len(fake.captured))
	}
	// 历史窗口语义由真实 ContextEngine 决定（dialogue_round=1 → 末轮），
	// 组件忠实透传：应含末轮内容、不含首轮
	hist := fake.captured[1].GetContent().String()
	if !strings.Contains(hist, "第二轮答") || strings.Contains(hist, "第一轮问") {
		t.Fatalf("历史窗口透传错误: %q", hist)
	}
	// 断言2：ModelContext 尾部追加 user(query) + assistant(LLM 原文)
	msgs, _ := mc.GetMessages(0, true)
	if len(msgs) != 6 {
		t.Fatalf("写回后应 6 条: %d", len(msgs))
	}
	if msgs[4].GetRole() != llmschema.RoleTypeUser || msgs[4].GetContent().String() != "我的快递到哪了" {
		t.Fatalf("写回 user 错误: %v", msgs[4].GetContent().String())
	}
	if msgs[5].GetRole() != llmschema.RoleTypeAssistant ||
		msgs[5].GetContent().String() != fake.content {
		t.Fatalf("写回 assistant 错误: %v", msgs[5].GetContent().String())
	}
	// 断言3：输出 exclude_defaults 后键齐
	if out[classificationIDKey] != 1 || out[classificationNameKey] != "退款" {
		t.Fatalf("输出错误: %v", out)
	}
}

// TestInvokeEntry_未开历史 不读不写 ModelContext（消息数执行前后不变）
func TestInvokeEntry_未开历史(t *testing.T) {
	e := NewIntentDetectionExecutable(&IntentDetectionCompConfig{CategoryNameList: []string{"退款"}})
	fake := &fakeLLMInvoker{content: `{"class": "分类1", "reason": "r"}`}
	e.llModel = fake
	e.initialized = true
	mc := newTestModelContext(t)
	_, _ = mc.AddMessages(ctxBg(t), []llmschema.BaseMessage{llmschema.NewUserMessage("旧消息")})
	sess := newTestNodeSession(t)
	if _, err := e.invokeEntry(ctxBg(t), map[string]any{"query": "q"}, sess, mc); err != nil {
		t.Fatal(err)
	}
	msgs, _ := mc.GetMessages(0, true)
	if len(msgs) != 1 {
		t.Fatalf("未开历史不应读写 context: %d", len(msgs))
	}
}

// TestInvokeEntry_解析失败回落 默认分类快照输出
func TestInvokeEntry_解析失败回落(t *testing.T) {
	e := NewIntentDetectionExecutable(&IntentDetectionCompConfig{CategoryNameList: []string{"退款"}})
	e.llModel = &fakeLLMInvoker{content: "不是JSON"}
	e.initialized = true
	out, err := e.invokeEntry(ctxBg(t), map[string]any{"query": "q"}, newTestNodeSession(t), nil)
	if err != nil {
		t.Fatal(err)
	}
	if out[classificationIDKey] != 0 {
		t.Fatalf("应回落默认分类 id=0: %v", out)
	}
	if _, ok := out["reason"]; !ok {
		t.Fatalf("回落应带失败 reason: %v", out)
	}
}

// errModelContext 故障注入包装（真实链错误传播语义钉子——审查拍板①A：
// Python 无 try，GetContextWindow 失败异常直达调用方）
type errModelContext struct {
	iface.ModelContext
	err error
}

func (m *errModelContext) GetContextWindow(_ context.Context, _ []llmschema.BaseMessage,
	_ []ceschema.ToolInfoInterface, _, _ int, _ ...iface.Option) (*iface.ContextWindow, error) {
	return nil, m.err
}

// TestInvokeEntry_历史窗口失败传播 GetContextWindow 失败 → error 直达（不降级吞掉）
func TestInvokeEntry_历史窗口失败传播(t *testing.T) {
	e := NewIntentDetectionExecutable(&IntentDetectionCompConfig{
		CategoryNameList: []string{"退款"}, EnableHistory: true,
	})
	e.llModel = &fakeLLMInvoker{content: `{"class": "分类1"}`}
	e.initialized = true
	realMC := newTestModelContext(t)
	mc := &errModelContext{ModelContext: realMC, err: errors.New("窗口引擎故障")}
	_, err := e.invokeEntry(ctxBg(t), map[string]any{"query": "q"}, newTestNodeSession(t), mc)
	if err == nil || !strings.Contains(err.Error(), "窗口引擎故障") {
		t.Fatalf("窗口失败应传播: %v", err)
	}
}

// ctxBg 测试上下文
func ctxBg(t *testing.T) context.Context {
	t.Helper()
	return context.Background()
}

// ──────────────────────────── Task 6 组合面 ────────────────────────────

// TestIntentDetectionComponent_接线 wiring 级：AddComponent 注册节点+条件边、
// AddBranch 委派 router、executable 懒加载缓存、opts 注入缝
func TestIntentDetectionComponent_接线(t *testing.T) {
	c := NewIntentDetectionComponent(&IntentDetectionCompConfig{CategoryNameList: []string{"退款"}})
	if c.router == nil {
		t.Fatal("构造应创建 BranchRouter")
	}
	// AddBranch 委派
	if err := c.AddBranch("${classificationId} == 1", []string{"refund"}, "b1"); err != nil {
		t.Fatalf("AddBranch 失败: %v", err)
	}
	targets := c.router.AllTargets()
	if _, ok := targets["refund"]; !ok {
		t.Fatalf("分支未登记: %v", targets)
	}
	// AddComponent：真图注册节点 + 条件边
	g := graph.NewPregelGraph()
	c.AddComponent(g, "intent", false)
	// executable 懒加载缓存（ToExecutableFn 槽）
	e1 := c.toExecutableCached()
	e2 := c.toExecutableCached()
	if e1 != e2 {
		t.Fatal("executable 应缓存复用")
	}
	if e1.(*IntentDetectionExecutable).router != c.router {
		t.Fatal("executable 应共享组件 router")
	}
}

// TestIntentDetectionComponent_AddBranch校验 nil 条件/空 target 报错（Python :484-487 → router 校验）
func TestIntentDetectionComponent_AddBranch校验(t *testing.T) {
	c := NewIntentDetectionComponent(&IntentDetectionCompConfig{})
	if err := c.AddBranch(nil, []string{"x"}, ""); err == nil {
		t.Fatal("nil 条件应报错")
	}
	if err := c.AddBranch("${a} == 1", []string{}, ""); err == nil {
		t.Fatal("空 target 应报错")
	}
}

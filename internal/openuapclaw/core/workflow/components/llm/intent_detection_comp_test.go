package llm

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/common/exception"

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

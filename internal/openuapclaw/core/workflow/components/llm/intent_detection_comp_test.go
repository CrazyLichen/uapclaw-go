package llm

import (
	"testing"

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

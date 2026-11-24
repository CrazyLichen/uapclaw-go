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

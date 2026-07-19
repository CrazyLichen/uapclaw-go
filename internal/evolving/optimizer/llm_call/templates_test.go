package llm_call

import (
	"strings"
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/prompt"
)

// ──────────────────────────── 导出函数 ────────────────────────────

func TestPromptInstructionOptimizeTemplate(t *testing.T) {
	keywords := map[string]any{
		"prompt_instruction":       "test prompt",
		"bad_cases":                "case1",
		"reflections_on_bad_cases": "reflection1",
		"tools_description":        "tool1",
	}
	result, err := PromptInstructionOptimizeTemplate.Format(keywords)
	if err != nil {
		t.Fatalf("Format failed: %v", err)
	}
	content, ok := result.Content.(string)
	if !ok {
		t.Fatal("Content should be string")
	}
	if !strings.Contains(content, "test prompt") {
		t.Error("应包含 prompt_instruction 替换结果")
	}
	if !strings.Contains(content, "PROMPT_OPTIMIZED") {
		t.Error("应包含 PROMPT_OPTIMIZED 输出标签")
	}
}

func TestPromptInstructionOptimizeBothTemplate(t *testing.T) {
	keywords := map[string]any{
		"system_prompt":            "sys prompt",
		"user_prompt":              "usr prompt",
		"bad_cases":                "case1",
		"reflections_on_bad_cases": "reflection1",
		"tools_description":        "tool1",
	}
	result, err := PromptInstructionOptimizeBothTemplate.Format(keywords)
	if err != nil {
		t.Fatalf("Format failed: %v", err)
	}
	content, ok := result.Content.(string)
	if !ok {
		t.Fatal("Content should be string")
	}
	if !strings.Contains(content, "SYSTEM_PROMPT_OPTIMIZED") {
		t.Error("应包含 SYSTEM_PROMPT_OPTIMIZED 输出标签")
	}
	if !strings.Contains(content, "USER_PROMPT_OPTIMIZED") {
		t.Error("应包含 USER_PROMPT_OPTIMIZED 输出标签")
	}
}

func TestCreatePromptTextualGradientTemplate(t *testing.T) {
	keywords := map[string]any{
		"system_prompt":     "sys prompt",
		"user_prompt":       "usr prompt",
		"bad_cases":         "case1",
		"tools_description": "tool1",
	}
	result, err := CreatePromptTextualGradientTemplate.Format(keywords)
	if err != nil {
		t.Fatalf("Format failed: %v", err)
	}
	content, ok := result.Content.(string)
	if !ok {
		t.Fatal("Content should be string")
	}
	if !strings.Contains(content, "<INS>") {
		t.Error("应包含 INS 标签指示")
	}
}

func TestCreateBadCaseTemplate(t *testing.T) {
	keywords := map[string]any{
		"question": "q1",
		"label":    "l1",
		"answer":   "a1",
		"reason":   "r1",
	}
	result, err := CreateBadCaseTemplate.Format(keywords)
	if err != nil {
		t.Fatalf("Format failed: %v", err)
	}
	content, ok := result.Content.(string)
	if !ok {
		t.Fatal("Content should be string")
	}
	if !strings.Contains(content, "q1") || !strings.Contains(content, "l1") {
		t.Error("应包含 question 和 label 替换结果")
	}
}

func TestPlaceholderRestoreTemplate(t *testing.T) {
	keywords := map[string]any{
		"original_prompt":      "original",
		"revised_prompt":       "revised",
		"all_placeholders":     "[name, age]",
		"missing_placeholders": "[age]",
	}
	result, err := PlaceholderRestoreTemplate.Format(keywords)
	if err != nil {
		t.Fatalf("Format failed: %v", err)
	}
	content, ok := result.Content.(string)
	if !ok {
		t.Fatal("Content should be string")
	}
	if !strings.Contains(content, "original") || !strings.Contains(content, "revised") {
		t.Error("应包含 original_prompt 和 revised_prompt 替换结果")
	}
}

func TestTemplates_ToMessages(t *testing.T) {
	templates := []*prompt.PromptTemplate{
		PromptInstructionOptimizeTemplate,
		PromptInstructionOptimizeBothTemplate,
		CreatePromptTextualGradientTemplate,
		CreateBadCaseTemplate,
		PlaceholderRestoreTemplate,
	}
	for i, tpl := range templates {
		msgs, err := tpl.ToMessages()
		if err != nil {
			t.Errorf("template[%d] ToMessages failed: %v", i, err)
		}
		if len(msgs) == 0 {
			t.Errorf("template[%d] ToMessages returned empty", i)
		}
	}
}

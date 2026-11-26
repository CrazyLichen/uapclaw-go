package prompts

import (
	"strings"
	"testing"
)

// TestPromptConstants_锚点 三段提示词非空且含必备占位/结构标签。
// Python: prompts.py 三常量 + __all__。
func TestPromptConstants_锚点(t *testing.T) {
	cases := []struct {
		name     string
		body     string
		mustHave []string
	}{
		{"CurrentCompactPrompt", CurrentCompactPrompt, []string{
			"<coverage_check>", "<state_snapshot>", "</state_snapshot>",
			"### 1. Active Task", "### 11. Relevant Files & Structure",
			"Sensitive information removed.",
		}},
		{"DialogueCompactPrompt", DialogueCompactPrompt, []string{
			"<coverage_check>", "<state_snapshot>",
			"<memory_block_dialogue>", "<memory_block_current>", "<memory_block_round>",
			"### 1. Historical User Requests and Outcomes", "### 10. Historical Pending Notes",
		}},
		{"RoundCompactPrompt", RoundCompactPrompt, []string{
			"<coverage_check>", "<state_snapshot>",
			"<memory_block_current>", "<memory_block_dialogue>", "<memory_block_round>",
			"### 1. Active Task and Success Criteria", "### 14. Relevant Files",
		}},
	}
	for _, tc := range cases {
		if strings.TrimSpace(tc.body) == "" {
			t.Errorf("%s 为空", tc.name)
		}
		for _, token := range tc.mustHave {
			if !strings.Contains(tc.body, token) {
				t.Errorf("%s 缺少必备片段 %q", tc.name, token)
			}
		}
	}
}

// TestPromptConstants_尾换行对齐 Python 三引号体以换行结尾（"""  前无残字符）。
func TestPromptConstants_尾换行(t *testing.T) {
	for name, body := range map[string]string{
		"CurrentCompactPrompt":  CurrentCompactPrompt,
		"DialogueCompactPrompt": DialogueCompactPrompt,
		"RoundCompactPrompt":    RoundCompactPrompt,
	} {
		if !strings.HasSuffix(body, "\n") {
			t.Errorf("%s 应以换行结尾（对齐 Python 三引号体）", name)
		}
	}
}

package rails

import (
	"testing"

	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// TestContextUsageTokensUsed_取总量 验证 context.usage 分子口径：TotalTokens（输入+输出）
func TestContextUsageTokensUsed_取总量(t *testing.T) {
	cases := []struct {
		name  string
		usage *llmschema.UsageMetadata
		want  int
	}{
		{"取TotalTokens", &llmschema.UsageMetadata{InputTokens: 2500, OutputTokens: 300, TotalTokens: 2800}, 2800},
		{"只上报输入侧视为0", &llmschema.UsageMetadata{InputTokens: 2500}, 0},
		{"nil返回0", nil, 0},
		{"全为0返回0", &llmschema.UsageMetadata{}, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := contextUsageTokensUsed(tc.usage); got != tc.want {
				t.Errorf("want %d，实际 %d", tc.want, got)
			}
		})
	}
}

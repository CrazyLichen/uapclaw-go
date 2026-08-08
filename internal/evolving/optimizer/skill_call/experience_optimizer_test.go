package skill_call

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/uapclaw/uapclaw-go/internal/evolving/checkpointing"
	"github.com/uapclaw/uapclaw-go/internal/evolving/signal"
)

// ──────────────────────────── 导出函数 ────────────────────────────

func TestNewSkillExperienceOptimizer(t *testing.T) {
	opt := NewSkillExperienceOptimizer(nil, "qwen-max", "cn", GenerateRecordsLLMPolicy)
	assert.NotNil(t, opt)
	assert.Equal(t, "qwen-max", opt.ModelName())
	assert.Equal(t, "cn", opt.Language())
	assert.Equal(t, GenerateRecordsLLMPolicy, opt.generateRecordsLLMPolicy)
}

func TestSkillExperienceOptimizer_Domain(t *testing.T) {
	opt := NewSkillExperienceOptimizer(nil, "qwen-max", "cn", GenerateRecordsLLMPolicy)
	assert.Equal(t, "skill_experience", opt.Domain())
}

func TestSkillExperienceOptimizer_DefaultTargets(t *testing.T) {
	opt := NewSkillExperienceOptimizer(nil, "qwen-max", "cn", GenerateRecordsLLMPolicy)
	assert.Equal(t, []string{"experiences"}, opt.DefaultTargets())
}

func TestSkillExperienceOptimizer_RequiresForwardData(t *testing.T) {
	opt := NewSkillExperienceOptimizer(nil, "qwen-max", "cn", GenerateRecordsLLMPolicy)
	assert.True(t, opt.RequiresForwardData())
}

func TestSkillExperienceOptimizer_UpdateLLM(t *testing.T) {
	opt := NewSkillExperienceOptimizer(nil, "qwen-max", "cn", GenerateRecordsLLMPolicy)
	// nil llm 应被拒绝
	opt.UpdateLLM(nil, "new-model")
	assert.Nil(t, opt.llm)
}

func TestBuildConversationSnippet_正常(t *testing.T) {
	messages := []map[string]any{
		{"role": "user", "content": "Hello"},
		{"role": "assistant", "content": "Hi there"},
	}
	snippet := buildConversationSnippet(messages, 30, 300, "cn")
	assert.Contains(t, snippet, "[user] Hello")
	assert.Contains(t, snippet, "[assistant] Hi there")
}

func TestBuildConversationSnippet_空消息(t *testing.T) {
	snippet := buildConversationSnippet(nil, 30, 300, "cn")
	assert.Equal(t, "", snippet)
}

func TestBuildConversationSnippet_截断(t *testing.T) {
	// 需要 6+ 条消息，使得前几条的 budget=300（非最近5条）被截断
	longContent := strings.Repeat("a", 500)
	messages := []map[string]any{
		{"role": "user", "content": longContent}, // 第1条，非最近5条
		{"role": "assistant", "content": "ok"},   // 第2条
		{"role": "user", "content": "ok"},        // 第3条
		{"role": "assistant", "content": "ok"},   // 第4条
		{"role": "user", "content": "ok"},        // 第5条
		{"role": "assistant", "content": "ok"},   // 第6条
	}
	snippet := buildConversationSnippet(messages, 30, 300, "cn")
	assert.Contains(t, snippet, "已截断")
}

func TestBuildConversationSnippet_toolCalls(t *testing.T) {
	messages := []map[string]any{
		{"role": "assistant", "content": "result", "tool_calls": []any{
			map[string]any{"name": "search"},
		}},
	}
	snippet := buildConversationSnippet(messages, 30, 300, "en")
	assert.Contains(t, snippet, "[assistant] (tool_calls: search)")
}

func TestSummarizeSkillContent_短内容(t *testing.T) {
	short := "short content"
	result := summarizeSkillContent(short)
	assert.Equal(t, short, result)
}

func TestSummarizeSkillContent_长内容分节(t *testing.T) {
	long := "## Section 1\n" + strings.Repeat("body text ", 200) +
		"\n## Section 2\n" + strings.Repeat("more text ", 200)
	result := summarizeSkillContent(long)
	assert.True(t, len(result) <= SkillContentMaxChars+100) // 允许一些额外截断标记
}

func TestSplitIntoSections(t *testing.T) {
	text := "## Section 1\nbody1\n## Section 2\nbody2"
	sections := splitIntoSections(text)
	assert.Equal(t, 2, len(sections))
}

func TestPreviewSection(t *testing.T) {
	longSection := "## Title\n" + strings.Repeat("content ", 100)
	preview := previewSection(longSection)
	assert.Contains(t, preview, "## Title")
}

func TestBuildExistingSummary_有记录(t *testing.T) {
	records := []checkpointing.EvolutionRecord{
		{ID: "ev_001", Change: checkpointing.EvolutionPatch{Section: "Troubleshooting", Content: "test content"}},
	}
	result := buildExistingSummary(records, "description")
	assert.Contains(t, result, "[description]")
	assert.Contains(t, result, "ev_001")
}

func TestBuildExistingSummary_无记录(t *testing.T) {
	result := buildExistingSummary(nil, "body")
	assert.Equal(t, "", result)
}

func TestBuildContext_多信号(t *testing.T) {
	signals := []*signal.EvolutionSignal{
		{SignalType: "execution_failure", Excerpt: "error in step 3"},
		{SignalType: "user_correction", Excerpt: "wrong answer"},
	}
	result := buildContext(signals)
	assert.Contains(t, result, "[execution_failure]")
	assert.Contains(t, result, "[user_correction]")
}

func TestBuildContext_空信号(t *testing.T) {
	result := buildContext(nil)
	assert.Equal(t, "", result)
}

func TestLimitSummaryLines(t *testing.T) {
	summary := "line1\nline2\nline3\nline4"
	result := limitSummaryLines(summary, 2)
	assert.Equal(t, "line1\nline2", result)
}

func TestFormatTemplate(t *testing.T) {
	template := "Hello {name}, your score is {score}"
	result := formatTemplate(template, "name", "Alice", "score", "95")
	assert.Equal(t, "Hello Alice, your score is 95", result)
}

func TestSkillExperienceOptimizer_RemoveSkillPrefix(t *testing.T) {
	assert.Equal(t, "test_skill", removeSkillPrefix("skill_experience_test_skill"))
	assert.Equal(t, "other", removeSkillPrefix("other"))
}

func TestSkillExperienceOptimizer_TruncateString(t *testing.T) {
	result := truncateString("abcdefghij", 5)
	assert.Equal(t, "abcde", result)
	result = truncateString("abc", 5)
	assert.Equal(t, "abc", result)
}

func TestSkillExperienceOptimizer_OrElse(t *testing.T) {
	assert.Equal(t, "value", orDefault("value", "default"))
	assert.Equal(t, "default", orDefault("", "default"))
}

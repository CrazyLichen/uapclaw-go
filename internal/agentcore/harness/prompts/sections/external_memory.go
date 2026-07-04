package sections

import saprompt "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/prompts"

// ──────────────────────────── 导出函数 ────────────────────────────

// BuildExternalMemorySection 构建外部记忆节（Priority 55）
//
// promptBlock 为外部记忆内容；为空时返回 nil。
func BuildExternalMemorySection(promptBlock string, lang string) *saprompt.PromptSection {
	if promptBlock == "" {
		return nil
	}
	section := saprompt.PromptSection{
		Name:     SectionExternalMemory,
		Content:  map[string]string{lang: promptBlock},
		Priority: 55,
	}
	return &section
}

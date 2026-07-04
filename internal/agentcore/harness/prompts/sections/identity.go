package sections

import saprompt "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/prompts"

// ──────────────────────────── 常量 ────────────────────────────

const (
	identityCN = "你是一个通用 AI 助手。请根据用户的需求，合理使用可用工具完成任务。\n" +
		"在执行过程中保持目标聚焦，遇到问题时尝试不同策略。"

	identityEN = "You are a general-purpose AI assistant. Use available tools to complete tasks based on user needs.\n" +
		"Stay focused on the goal during execution and try different strategies when encountering problems."
)

// ──────────────────────────── 导出函数 ────────────────────────────

// BuildIdentitySection 构建身份节
func BuildIdentitySection() saprompt.PromptSection {
	return saprompt.PromptSection{
		Name:     SectionIdentity,
		Content:  map[string]string{"cn": identityCN, "en": identityEN},
		Priority: 10,
	}
}

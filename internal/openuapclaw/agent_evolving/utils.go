package evolving

import (
	"regexp"
	"strings"

	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/foundation/prompt"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// GetContentStringFromTemplate 将 PromptTemplate 转为多行文本。
// 调用模板的 ToMessages() 获取消息列表，拼接所有消息的文本内容，用换行符连接。
// Python: TuneUtils.get_content_string_from_template(template)
//
//	"\n".join(msg.content for msg in template.to_messages())
func GetContentStringFromTemplate(tpl *prompt.PromptTemplate) string {
	messages, err := tpl.ToMessages()
	if err != nil || len(messages) == 0 {
		return ""
	}

	parts := make([]string, 0, len(messages))
	for _, msg := range messages {
		content := msg.GetContent()
		if content.IsText() {
			parts = append(parts, content.Text())
		}
	}
	return strings.Join(parts, "\n")
}

// inferSkillFromTexts 从消息文本推断技能。
// 对齐 Python: utils.infer_skill_from_texts(skill_names, skill_tool_payloads=..., texts=...)
// 简化实现：搜索 skill_tool 参数中的技能名称
func InferSkillFromTexts(skillNames []string, skillToolPayloads []string, texts []string) string {
	// 在 skill_tool_payloads 中搜索已知技能名
	for _, payload := range skillToolPayloads {
		for _, name := range skillNames {
			if strings.Contains(payload, name) {
				return name
			}
		}
	}
	// 在工具内容中搜索 SKILL.md 路径中的技能名
	skillMDRe := regexp.MustCompile(`[/\\]([^/\\]+)[/\\]SKILL\.md`)
	for _, text := range texts {
		matches := skillMDRe.FindAllStringSubmatch(text, -1)
		for _, match := range matches {
			for _, name := range skillNames {
				if match[1] == name {
					return name
				}
			}
		}
	}
	return ""
}

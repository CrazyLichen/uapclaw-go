package evolving

import (
	"strings"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/prompt"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// GetContentStringFromTemplate 将 PromptTemplate 转为多行文本。
//
// 调用模板的 ToMessages() 获取消息列表，拼接所有消息的文本内容，用换行符连接。
//
// 对应 Python: TuneUtils.get_content_string_from_template(template)
//   "\n".join(msg.content for msg in template.to_messages())
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

// ──────────────────────────── 非导出函数 ────────────────────────────

package prompts

import (
	"regexp"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// prPattern 匹配 .pr.md 文件中的角色分隔符 `#role#`
//
// Python: PR_PATTERN = re.compile(r"(?s)`#((?:user)|(?:system)|(?:assistant)|(?:tool))#`")
// 注意：正则中 (?s) 使 . 匹配换行，但本正则不使用 . ，(?s) 无实际效果，仅为对齐 Python
var prPattern = regexp.MustCompile("`#((?:user)|(?:system)|(?:assistant)|(?:tool))#`")

// ──────────────────────────── 导出函数 ────────────────────────────

// ParsePRContent 解析 .pr.md 格式的提示词模板内容，返回消息列表。
//
// Python: ThreadSafePromptManager.load_pr_content(content)
//
// 解析规则（对齐 Python PR_PATTERN.split 行为）：
//   - 使用正则匹配 `#role#` 分隔符，捕获角色名
//   - Python 的 re.split 对含捕获组的正则，结果交替为 [前缀, role1, content1, role2, content2, ...]
//   - Go 用 FindAllStringSubmatchIndex 定位分隔符，手动提取角色和内容
//   - role ∈ {system, user, assistant, tool}（角色集合）
//   - 每个角色标记开始一个新的消息段落
//   - 未知角色跳过
func ParsePRContent(content string) []schema.BaseMessage {
	roles := map[string]bool{
		"user":      true,
		"system":    true,
		"assistant": true,
		"tool":      true,
	}

	// 找到所有匹配 `#role#` 的位置
	matches := prPattern.FindAllStringSubmatchIndex(content, -1)
	if len(matches) == 0 {
		return nil
	}

	var messages []schema.BaseMessage

	for i, match := range matches {
		roleStart := match[2]
		roleEnd := match[3]
		role := content[roleStart:roleEnd]

		// 内容从分隔符结束位置开始，到下一个分隔符开始位置结束
		contentStart := match[1] // 完整匹配结束位置
		var contentEnd int
		if i+1 < len(matches) {
			contentEnd = matches[i+1][0] // 下一个分隔符开始位置
		} else {
			contentEnd = len(content)
		}

		if roles[role] {
			msg := newMessageByRole(role, content[contentStart:contentEnd])
			if msg != nil {
				messages = append(messages, msg)
			}
		}
	}

	return messages
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// newMessageByRole 根据角色创建对应的 BaseMessage
func newMessageByRole(role, content string) schema.BaseMessage {
	switch role {
	case "system":
		return schema.NewSystemMessage(content)
	case "user":
		return schema.NewUserMessage(content)
	case "assistant":
		return schema.NewAssistantMessage(content)
	// case "tool" 跳过：当前 .pr.md 模板无 #tool# 角色，
	// ToolMessage 需要 tool_call_id，而模板中无此字段。
	// 待后续 #tool# 模板出现时正确实现。
	default:
		return nil
	}
}

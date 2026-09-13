package project_memory

import (
	"strings"

	saprompt "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/prompts"
)

// ──────────────────────────── 常量 ────────────────────────────

const (
	// SectionName project_memory section 名称。
	// Python: SECTION_NAME = "project_memory" (section.py L13)
	SectionName = "project_memory"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// BuildProjectMemorySection 构建 project_memory PromptSection。
// 内容为空时返回 nil。始终生成中英双语内容。
// Python: build_project_memory_section (section.py L28-51)
func BuildProjectMemorySection(content string, priority int) *saprompt.PromptSection {
	// Python: del language  # accepted for API compat, not used.
	// Python: if not content or not content.strip(): return None
	if strings.TrimSpace(content) == "" {
		return nil
	}
	// Python: body_text = content.strip()
	body := strings.TrimSpace(content)
	// Python: body_cn = f"{_HEADER_CN}\n\n{_NOTE_CN}\n\n{body_text}\n"
	cn := "## 项目记忆（ProjectMemoryRail 自动加载）\n\n以下内容来自项目根、用户目录、本地私有文件的合并。修改磁盘文件即可在下一轮对话生效。\n\n" + body + "\n"
	// Python: body_en = f"{_HEADER_EN}\n\n{_NOTE_EN}\n\n{body_text}\n"
	en := "## Project Memory (auto-loaded by ProjectMemoryRail)\n\nThe following is merged from project root, user home, and local private files. Edits take effect on the next turn.\n\n" + body + "\n"
	// Python: return PromptSection(name=SECTION_NAME, content={"cn": body_cn, "en": body_en}, priority=priority)
	section := saprompt.PromptSection{
		Name:     SectionName,
		Content:  map[string]string{"cn": cn, "en": en},
		Priority: priority,
	}
	return &section
}

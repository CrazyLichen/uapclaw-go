package sections

import (
	"fmt"

	saprompt "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/prompts"
	wscontent "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/prompts/workspace_content"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// BuildWorkspaceSection 构建工作空间节（Priority 70）
//
// rootPath 为工作目录根路径；dirTree 为目录树文本（暂未使用，预留）。
func BuildWorkspaceSection(rootPath string, dirTree string, lang string) saprompt.PromptSection {
	var header string
	var importantFiles string

	if lang == "en" {
		header = wscontent.WorkspaceHeaderEN
		importantFiles = wscontent.ImportantFilesEN
	} else {
		header = wscontent.WorkspaceHeaderCN
		importantFiles = wscontent.ImportantFilesCN
	}

	var pathLine string
	if lang == "en" {
		pathLine = fmt.Sprintf("Your working directory is: `%s`\n\n%s", rootPath, importantFiles)
	} else {
		pathLine = fmt.Sprintf("你的工作目录是：`%s`\n\n%s", rootPath, importantFiles)
	}

	content := header + pathLine

	return saprompt.PromptSection{
		Name:     SectionWorkspace,
		Content:  map[string]string{lang: content},
		Priority: 70,
	}
}

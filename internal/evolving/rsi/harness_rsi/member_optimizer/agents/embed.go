// embed.go 提供 MemberOptimizer Agent 提示词模板的内嵌资源。
// Python: factory.py _load_prompt / _render_prompt 经文件系统读取 agents/prompts/。

package agents

import "embed"

//go:embed prompts/*.md
var promptsFS embed.FS

// LoadPrompt 读取内嵌提示词模板原文。
// Python: factory.py:302-305 _load_prompt。
func LoadPrompt(filename string) (string, error) {
	data, err := promptsFS.ReadFile("prompts/" + filename)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

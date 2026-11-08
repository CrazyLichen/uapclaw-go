package rb

import (
	"regexp"
	"strings"

	ceschema "github.com/uapclaw/uapclaw-go/internal/agentcore/context_evolver/schema"
)

// ──────────────────────────── 结构体 ────────────────────────────

// MemoryItemParser 记忆项解析器。
// 将 LLM 返回的 Markdown 格式解析为结构化 ReasoningBankMemoryItem。
// 对齐 Python MemoryItemParser (summary/task/reasoning_bank/update.py)。
type MemoryItemParser struct{}

// ──────────────────────────── 全局变量 ────────────────────────────

var (
	// memoryItemSplitRegex 按段落标题 "# Memory Item N" 分割响应
	memoryItemSplitRegex = regexp.MustCompile(`\n\s*#\s*Memory\s+Item\s+\d+`)
	// titleFieldRegex 匹配 Title 字段行
	titleFieldRegex = regexp.MustCompile(`^##\s*Title\s+`)
	// descriptionFieldRegex 匹配 Description 字段行
	descriptionFieldRegex = regexp.MustCompile(`^##\s*Description\s+`)
	// contentFieldRegex 匹配 Content 字段行
	contentFieldRegex = regexp.MustCompile(`^##\s*Content\s+`)
	// sectionHeaderRegex 匹配段落标题或字段标题（用于判断字段终止）
	sectionHeaderRegex = regexp.MustCompile(`^##\s*|#\s*Memory\s+Item\s+\d+`)
)

// ──────────────────────────── 导出函数 ────────────────────────────

// NewMemoryItemParser 创建记忆项解析器。
func NewMemoryItemParser() *MemoryItemParser {
	return &MemoryItemParser{}
}

// Parse 将 LLM 响应解析为 ReasoningBankMemory 列表。
// 对齐 Python MemoryItemParser.parse()。
func (p *MemoryItemParser) Parse(llmResponse string, query string, label *bool) []*ceschema.ReasoningBankMemory {
	cleaned := p.cleanResponse(llmResponse)
	sections := p.splitIntoSections(cleaned)

	var memories []*ceschema.ReasoningBankMemory
	for _, section := range sections {
		item := p.extractMemoryItem(section)
		if item == nil {
			continue
		}
		memories = append(memories, &ceschema.ReasoningBankMemory{
			Query:  query,
			Memory: []ceschema.ReasoningBankMemoryItem{*item},
			Label:  label,
		})
	}
	return memories
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// cleanResponse 去除 Markdown 代码围栏。
// 对齐 Python MemoryItemParser._clean_response()。
func (p *MemoryItemParser) cleanResponse(response string) string {
	s := strings.TrimSpace(response)
	lines := strings.Split(s, "\n")

	// 去除开头 ``` 行
	if len(lines) > 0 && strings.HasPrefix(strings.TrimSpace(lines[0]), "```") {
		lines = lines[1:]
	}

	// 去除结尾 ``` 行
	if len(lines) > 0 && strings.TrimSpace(lines[len(lines)-1]) == "```" {
		lines = lines[:len(lines)-1]
	}

	return strings.Join(lines, "\n")
}

// splitIntoSections 按 "# Memory Item N" 分割响应。
// 对齐 Python MemoryItemParser._split_into_sections()。
func (p *MemoryItemParser) splitIntoSections(response string) []string {
	// 在分割标记前插入换行，确保 regex 能匹配行首
	normalized := memoryItemSplitRegex.ReplaceAllString(response, "\n###SPLIT###")
	parts := strings.Split(normalized, "###SPLIT###")

	var sections []string
	for _, part := range parts {
		trimmed := strings.TrimSpace(part)
		if trimmed != "" {
			sections = append(sections, trimmed)
		}
	}
	return sections
}

// extractMemoryItem 从单个段落提取 ReasoningBankMemoryItem。
// 对齐 Python MemoryItemParser._extract_memory_item()。
func (p *MemoryItemParser) extractMemoryItem(section string) *ceschema.ReasoningBankMemoryItem {
	lines := strings.Split(section, "\n")

	var title, description, content string
	var titleFound, descFound, contentFound bool

	for i := 0; i < len(lines); i++ {
		line := lines[i]

		if titleFieldRegex.MatchString(line) {
			title, _ = p.extractField(lines, i, `##\s*Title\s+`)
			titleFound = true
		} else if descriptionFieldRegex.MatchString(line) {
			description, _ = p.extractField(lines, i, `##\s*Description\s+`)
			descFound = true
		} else if contentFieldRegex.MatchString(line) {
			content, _ = p.extractField(lines, i, `##\s*Content\s+`)
			contentFound = true
		}
	}

	if !titleFound || !descFound || !contentFound {
		return nil
	}

	return &ceschema.ReasoningBankMemoryItem{
		Title:       strings.TrimSpace(title),
		Description: strings.TrimSpace(description),
		Content:     strings.TrimSpace(content),
	}
}

// extractField 从行列表中提取字段值。
// 对齐 Python MemoryItemParser._extract_field()。
// 返回字段值文本和下一个待扫描的行索引。
func (p *MemoryItemParser) extractField(lines []string, startIdx int, fieldPattern string) (string, int) {
	re := regexp.MustCompile(fieldPattern)

	// 去除字段模式，获取当前行的初始值
	initialValue := re.ReplaceAllString(lines[startIdx], "")
	parts := []string{initialValue}

	// 继续读取后续行，直到遇到 ## 或 # Memory Item 或文件末尾
	i := startIdx + 1
	for ; i < len(lines); i++ {
		line := lines[i]
		trimmed := strings.TrimSpace(line)

		// 遇到下一个字段标题或段落标题，停止
		if sectionHeaderRegex.MatchString(trimmed) {
			break
		}

		// 跳过空行和 ``` 行
		if trimmed == "" || trimmed == "```" {
			continue
		}

		parts = append(parts, trimmed)
	}

	return strings.Join(parts, " "), i
}

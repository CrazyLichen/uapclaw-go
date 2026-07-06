package prompts

import (
	"sort"
	"strconv"

	saprompt "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/prompts"
)

// ──────────────────────────── 结构体 ────────────────────────────

// SectionInfo 单个节的轻量快照，用于诊断报告。
//
// 对应 Python: SectionInfo (openjiuwen/harness/prompts/report.py)
type SectionInfo struct {
	// Name 节名称
	Name string `json:"name"`
	// Priority 优先级
	Priority int `json:"priority"`
	// CharCount 渲染后字符数
	CharCount int `json:"char_count"`
}

// PromptReport 系统提示词诊断报告，统计节信息和 token 估算。
//
// 对应 Python: PromptReport (openjiuwen/harness/prompts/report.py)
type PromptReport struct {
	// TotalChars 总字符数
	TotalChars int `json:"total_chars"`
	// EstimatedTokens 估算 token 数
	EstimatedTokens int `json:"estimated_tokens"`
	// SectionCount 节数量
	SectionCount int `json:"section_count"`
	// Sections 各节信息
	Sections []SectionInfo `json:"sections,omitempty"`
	// Mode 提示词模式
	Mode string `json:"mode"`
	// Language 语言
	Language string `json:"language"`
}

// ──────────────────────────── 常量 ────────────────────────────

const (
	// cnCharsPerToken 中文每 token 约对应 2.5 字符
	// 对应 Python: _CN_CHARS_PER_TOKEN = 2.5
	cnCharsPerToken = 2.5
	// enCharsPerToken 英文每 token 约对应 4.0 字符
	// 对应 Python: _EN_CHARS_PER_TOKEN = 4.0
	enCharsPerToken = 4.0
)

// ──────────────────────────── 导出函数 ────────────────────────────

// NewPromptReport 从构建器当前状态创建诊断报告。
//
// 对应 Python: PromptReport.from_builder(builder)
func NewPromptReport(builder *SystemPromptBuilder) *PromptReport {
	language := builder.Language()
	mode := builder.mode.String()

	allSections := builder.GetAllSections()
	sectionInfos := make([]SectionInfo, 0, len(allSections))
	totalChars := 0

	// 按优先级排序
	sorted := make([]saprompt.PromptSection, 0, len(allSections))
	for _, s := range allSections {
		sorted = append(sorted, s)
	}
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].Priority < sorted[j].Priority
	})

	for _, s := range sorted {
		chars := s.CharCount(language)
		sectionInfos = append(sectionInfos, SectionInfo{
			Name:      s.Name,
			Priority:  s.Priority,
			CharCount: chars,
		})
		totalChars += chars
	}

	// token 估算
	charsPerToken := enCharsPerToken
	if language == "cn" {
		charsPerToken = cnCharsPerToken
	}
	estimatedTokens := 0
	if totalChars > 0 {
		estimatedTokens = int(float64(totalChars) / charsPerToken)
	}

	return &PromptReport{
		TotalChars:      totalChars,
		EstimatedTokens: estimatedTokens,
		SectionCount:    len(sectionInfos),
		Sections:        sectionInfos,
		Mode:            mode,
		Language:        language,
	}
}

// ToDict 将报告序列化为字典。
//
// 对应 Python: PromptReport.to_dict()
func (r *PromptReport) ToDict() map[string]any {
	sections := make([]map[string]any, 0, len(r.Sections))
	for _, s := range r.Sections {
		sections = append(sections, map[string]any{
			"name":       s.Name,
			"priority":   s.Priority,
			"char_count": s.CharCount,
		})
	}
	return map[string]any{
		"total_chars":      r.TotalChars,
		"estimated_tokens": r.EstimatedTokens,
		"section_count":    r.SectionCount,
		"sections":         sections,
		"mode":             r.Mode,
		"language":         r.Language,
	}
}

// Summary 返回人类可读的单行摘要。
//
// 对应 Python: PromptReport.summary()
func (r *PromptReport) Summary() string {
	return "mode=" + r.Mode + " lang=" + r.Language +
		" sections=" + strconv.Itoa(r.SectionCount) +
		" chars=" + strconv.Itoa(r.TotalChars) +
		" est_tokens≈" + strconv.Itoa(r.EstimatedTokens)
}

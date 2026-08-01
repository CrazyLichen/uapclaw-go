package checkpointing

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	"github.com/uapclaw/uapclaw-go/internal/evolving/signal"
)

// ──────────────────────────── 结构体 ────────────────────────────

// StoreProjectionHelper Markdown 投影和待定记录格式化辅助。
//
// 对应 Python: openjiuwen/agent_evolving/checkpointing/store_projection.py StoreProjectionHelper
type StoreProjectionHelper struct {
	// store 所属的 EvolutionStore 实例
	store *EvolutionStore
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// RenderEvolutionMarkdown 渲染演进 Markdown。
// 对应 Python: StoreProjectionHelper.render_evolution_markdown(name)
func (h *StoreProjectionHelper) RenderEvolutionMarkdown(ctx context.Context, name string) error {
	skillDir := h.store.ResolveSkillDir(ctx, name)
	if skillDir == "" {
		return nil
	}

	evoLog := h.store.LoadFullEvolutionLog(ctx, name)
	// 对齐 Python: active_entries = [r for r in evo_log.entries if not r.change.skip_reason]
	activeEntries := make([]EvolutionRecord, 0)
	for _, r := range evoLog.Entries {
		if r.Change.SkipReason == nil || *r.Change.SkipReason == "" {
			activeEntries = append(activeEntries, r)
		}
	}
	if len(activeEntries) == 0 {
		return h.ClearRenderedOutputs(ctx, skillDir)
	}

	evoDir := filepath.Join(skillDir, "evolution")
	os.MkdirAll(evoDir, 0755)

	// 对齐 Python: section_groups / script_entries 分组
	sectionGroups := map[string][]EvolutionRecord{}
	scriptEntries := []EvolutionRecord{}
	for _, record := range activeEntries {
		if record.Change.Target == signal.EvolutionTargetScript {
			scriptEntries = append(scriptEntries, record)
		} else {
			sectionGroups[record.Change.Section] = append(sectionGroups[record.Change.Section], record)
		}
	}

	for section, records := range sectionGroups {
		if err := h.RenderSectionFile(ctx, evoDir, section, records); err != nil {
			return err
		}
	}

	if len(scriptEntries) > 0 {
		scriptsDir := filepath.Join(evoDir, "scripts")
		os.MkdirAll(scriptsDir, 0755)
		if err := h.RenderScriptIndex(ctx, scriptsDir, scriptEntries); err != nil {
			return err
		}
	}

	if err := h.UpdateSkillMDIndex(ctx, skillDir, activeEntries); err != nil {
		return err
	}
	logger.Info(logger.ComponentAgentCore).
		Str("skill", name).
		Int("entries", len(activeEntries)).
		Msg("[EvolutionStore] 渲染 markdown")
	return nil
}

// ClearRenderedOutputs 清除生成的投影文件和 SKILL.md index 块。
// 对应 Python: StoreProjectionHelper.clear_rendered_outputs(skill_dir)
func (h *StoreProjectionHelper) ClearRenderedOutputs(ctx context.Context, skillDir string) error {
	evoDir := filepath.Join(skillDir, "evolution")
	if isDir(evoDir) {
		clearDirRecursive(evoDir)
	}

	skillMDPath := h.store.FindSkillMD(ctx, skillDir)
	if skillMDPath == "" {
		return nil
	}
	content, err := h.store.ReadFileText(ctx, skillMDPath)
	if err != nil {
		return err
	}
	if content == "" || !evolutionIndexPattern.MatchString(content) {
		return nil
	}
	cleaned := evolutionIndexPattern.ReplaceAllString(content, "")
	cleaned = strings.TrimSpace(cleaned) + "\n"
	return h.store.WriteFileText(ctx, skillMDPath, cleaned)
}

// RenderSectionFile 渲染单个 section 的 Markdown 文件。
// 对应 Python: StoreProjectionHelper.render_section_file(evo_dir, section, records)
func (h *StoreProjectionHelper) RenderSectionFile(ctx context.Context, evoDir string, section string, records []EvolutionRecord) error {
	lines := []string{
		fmt.Sprintf("# %s", section),
		"",
		"> Auto-generated from evolutions.json. Do not edit directly.",
		"",
	}
	for _, record := range records {
		parts := strings.SplitN(record.Change.Content, "\n", 2)
		lines = append(lines, fmt.Sprintf(`<a id="%s"></a>`, record.ID))
		lines = append(lines, fmt.Sprintf("### [%s] %s", record.ID, recordSummary(&record)))
		if record.Summary != nil && *record.Summary != "" && strings.TrimSpace(record.Change.Content) != "" {
			lines = append(lines, strings.TrimRight(record.Change.Content, "\n"))
		} else if len(parts) > 1 && strings.TrimSpace(parts[1]) != "" {
			lines = append(lines, strings.TrimRight(parts[1], "\n"))
		}
		appliedTag := ""
		if record.Applied {
			appliedTag = " | applied"
		}
		lines = append(lines,
			"",
			fmt.Sprintf("*Source: %s | %s%s*", record.Source, record.Timestamp, appliedTag),
			"",
			"---",
			"",
		)
	}
	filename := sectionFilename(section)
	return h.store.WriteFileText(ctx, filepath.Join(evoDir, filename), strings.Join(lines, "\n"))
}

// RenderScriptIndex 渲染脚本索引文件。
// 对应 Python: StoreProjectionHelper.render_script_index(scripts_dir, entries)
func (h *StoreProjectionHelper) RenderScriptIndex(ctx context.Context, scriptsDir string, entries []EvolutionRecord) error {
	lines := []string{
		"# Script Index",
		"",
		"> Auto-generated from evolutions.json. Do not edit directly.",
		"",
		"| File | Language | Purpose | Source |",
		"|------|----------|---------|--------|",
	}
	for _, record := range entries {
		fname := record.ID
		if record.Change.ScriptFilename != nil {
			fname = *record.Change.ScriptFilename
		}
		lang := "unknown"
		if record.Change.ScriptLanguage != nil {
			lang = *record.Change.ScriptLanguage
		}
		purpose := ""
		if record.Change.ScriptPurpose != nil {
			purpose = *record.Change.ScriptPurpose
		}
		date := record.Timestamp
		if len(date) >= 10 {
			date = date[:10]
		}
		lines = append(lines, fmt.Sprintf("| [%s](%s) | %s | %s | %s |", fname, fname, lang, purpose, date))
	}
	lines = append(lines, "")
	return h.store.WriteFileText(ctx, filepath.Join(scriptsDir, "_index.md"), strings.Join(lines, "\n"))
}

// UpdateSkillMDIndex 更新 SKILL.md 的 evolution-index 块。
// 对应 Python: StoreProjectionHelper.update_skill_md_index(skill_dir, entries)
func (h *StoreProjectionHelper) UpdateSkillMDIndex(ctx context.Context, skillDir string, entries []EvolutionRecord) error {
	skillMDPath := h.store.FindSkillMD(ctx, skillDir)
	if skillMDPath == "" {
		return nil
	}

	bodyCount := 0
	descCount := 0
	scriptCount := 0
	for _, record := range entries {
		switch record.Change.Target {
		case signal.EvolutionTargetBody:
			bodyCount++
		case signal.EvolutionTargetDescription:
			descCount++
		case signal.EvolutionTargetScript:
			scriptCount++
		}
	}

	total := len(entries)
	// 对齐 Python: parts = ", ".join(f"{v} {k}" for k, v in ... if v)
	var partStrs []string
	if bodyCount > 0 {
		partStrs = append(partStrs, fmt.Sprintf("%d body", bodyCount))
	}
	if descCount > 0 {
		partStrs = append(partStrs, fmt.Sprintf("%d description", descCount))
	}
	if scriptCount > 0 {
		partStrs = append(partStrs, fmt.Sprintf("%d script", scriptCount))
	}
	parts := strings.Join(partStrs, ", ")

	narrativeEntries := []EvolutionRecord{}
	scriptEntries := []EvolutionRecord{}
	for _, record := range entries {
		if record.Change.Target != signal.EvolutionTargetScript {
			narrativeEntries = append(narrativeEntries, record)
		} else {
			scriptEntries = append(scriptEntries, record)
		}
	}

	experienceIndexLines := formatExperienceIndexTable(narrativeEntries)
	scriptTableLines := formatScriptAssetsTable(scriptEntries)

	now := time.Now().UTC().Format("2006-01-02T15:04:05Z")
	indexBlock := strings.Join([]string{
		"<!-- evolution-index-start -->",
		"## Evolution Experiences",
		"",
		"Use this section as an index of lessons learned from previous executions. " +
			"Before applying this skill, check whether the current task matches any listed experience " +
			"summary. If it matches, read the linked detail section first and use the guidance while " +
			"planning and executing the task.",
		"",
		"For narrative guidance, read the relevant `evolution/*.md#...` detail section. " +
			"For reusable helper code, first review `evolution/scripts/_index.md`, then inspect " +
			"the specific script source before adapting or running it. Scripts are implementation " +
			"aids, not mandatory steps.",
		"",
		fmt.Sprintf("This skill has accumulated **%d** evolution experiences (%s).", total, parts),
		"",
	}, "\n")
	indexBlock += "\n" + strings.Join(experienceIndexLines, "\n") + "\n" + strings.Join(scriptTableLines, "\n") +
		"\n*" + fmt.Sprintf("Last updated: %s", now) + "*\n<!-- evolution-index-end -->"

	content, err := h.store.ReadFileText(ctx, skillMDPath)
	if err != nil {
		return err
	}
	if evolutionIndexPattern.MatchString(content) {
		content = evolutionIndexPattern.ReplaceAllString(content, indexBlock)
	} else {
		content = strings.TrimRight(content, "\n") + "\n\n" + indexBlock + "\n"
	}
	return h.store.WriteFileText(ctx, skillMDPath, content)
}

// FormatDescExperienceText 格式化描述层经验文本。
// 对应 Python: StoreProjectionHelper.format_desc_experience_text(name, max_items)
func (h *StoreProjectionHelper) FormatDescExperienceText(ctx context.Context, name string, maxItems int) string {
	pending := h.store.GetPendingRecords(ctx, name, func() *signal.EvolutionTarget {
		t := signal.EvolutionTargetDescription
		return &t
	}())
	if len(pending) == 0 {
		return ""
	}
	// 对齐 Python: pending.sort(key=lambda r: r.score, reverse=True)
	sort.Slice(pending, func(i, j int) bool { return pending[i].Score > pending[j].Score })
	limit := maxItems
	if limit <= 0 {
		limit = maxInjectDesc
	}
	var lines []string
	for _, record := range pending {
		if len(lines) >= limit {
			break
		}
		lines = append(lines, fmt.Sprintf("- %s", record.Change.Content))
	}
	return strings.Join(lines, "\n")
}

// FormatAllDescExperiences 格式化所有技能的描述经验。
func (h *StoreProjectionHelper) FormatAllDescExperiences(ctx context.Context, names []string) map[string]string {
	result := map[string]string{}
	for _, name := range names {
		text := h.FormatDescExperienceText(ctx, name, maxInjectDesc)
		if text != "" {
			result[name] = text
		}
	}
	return result
}

// FormatBodyExperienceText 格式化主体层经验文本。
func (h *StoreProjectionHelper) FormatBodyExperienceText(ctx context.Context, name string) string {
	pending := h.store.GetPendingRecords(ctx, name, func() *signal.EvolutionTarget {
		t := signal.EvolutionTargetBody
		return &t
	}())
	if len(pending) == 0 {
		return ""
	}
	lines := []string{fmt.Sprintf("\n\n# Skill '%s' body 演进经验\n", name)}
	for i, record := range pending {
		lines = append(lines, fmt.Sprintf("%d. **[%s]** %s", i+1, record.Change.Section, record.Change.Content))
	}
	return strings.Join(lines, "\n")
}

// ListPendingSummary 列出待定经验摘要。
func (h *StoreProjectionHelper) ListPendingSummary(ctx context.Context, names []string) string {
	var lines []string
	count := 0
	for _, name := range names {
		descPending := h.store.GetPendingRecords(ctx, name, func() *signal.EvolutionTarget {
			t := signal.EvolutionTargetDescription
			return &t
		}())
		bodyPending := h.store.GetPendingRecords(ctx, name, func() *signal.EvolutionTarget {
			t := signal.EvolutionTargetBody
			return &t
		}())
		allPending := append(descPending, bodyPending...)
		if len(allPending) == 0 {
			continue
		}

		count++
		lines = append(lines,
			fmt.Sprintf("%d. **%s** - 共 %d 条 pending 经验（description: %d, body: %d）",
				count, name, len(allPending), len(descPending), len(bodyPending)),
		)
		for _, record := range allPending {
			targetTag := "description"
			if record.Change.Target == signal.EvolutionTargetBody {
				targetTag = "body"
			}
			content := record.Change.Content
			title := content
			if idx := strings.Index(content, "\n"); idx >= 0 {
				title = content[:idx]
			} else if len(content) > 50 {
				title = content[:50]
			}
			lines = append(lines, fmt.Sprintf("   - [%s] **%s**: ", targetTag, title))
			if strings.Contains(content, "\n") {
				bodyLines := strings.Split(content, "\n")[1:]
				if len(bodyLines) > 0 {
					var summaryParts []string
					for _, line := range bodyLines {
					 trimmed := strings.TrimSpace(line)
					 trimmed = strings.TrimPrefix(trimmed, "- ")
					 if trimmed != "" {
						 summaryParts = append(summaryParts, trimmed)
					 }
					}
					summary := strings.Join(summaryParts, " ")
					if len(summary) > 100 {
						summary = summary[:100]
					}
					summary = strings.ReplaceAll(summary, "**", "")
					lines = append(lines, fmt.Sprintf("    %s", summary))
				}
			}
		}
		lines = append(lines, "")
	}
	if len(lines) == 0 {
		return "当前所有 Skill 暂无演进信息。"
	}
	return strings.Join(lines, "\n")
}

// StoreProjectionHelperExtractDescriptionFromSkillMD 从 SKILL.md 内容提取 description。
// 对应 Python: StoreProjectionHelper.extract_description_from_skill_md(content)
func StoreProjectionHelperExtractDescriptionFromSkillMD(content string) string {
	if !strings.HasPrefix(content, "---") {
		return ""
	}
	parts := strings.SplitN(content, "---", 3)
	if len(parts) < 3 {
		return ""
	}
	frontMatter := parts[1]
	for _, line := range strings.Split(strings.TrimSpace(frontMatter), "\n") {
		if strings.HasPrefix(line, "description:") {
			value := strings.TrimSpace(strings.TrimPrefix(line, "description:"))
			value = strings.Trim(value, "\"")
			value = strings.Trim(value, "'")
			return value
		}
	}
	return ""
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// sectionFilename 根据 section 名生成文件名。
// 对应 Python: StoreProjectionHelper._section_filename(section)
func sectionFilename(section string) string {
	return strings.ToLower(strings.ReplaceAll(section, " ", "_")) + ".md"
}

// recordSummary 生成记录摘要。
// 对应 Python: StoreProjectionHelper._record_summary(record)
func recordSummary(record *EvolutionRecord) string {
	if record.Summary != nil && *record.Summary != "" {
		return normalizeSummaryText(*record.Summary, 96)
	}
	if record.Change.Target == signal.EvolutionTargetScript && record.Change.ScriptPurpose != nil {
		return normalizeSummaryText(*record.Change.ScriptPurpose, 96)
	}
	firstLine := ""
	if record.Change.Content != "" {
		lines := strings.Split(record.Change.Content, "\n")
		if len(lines) > 0 {
			firstLine = lines[0]
		}
	}
	result := normalizeSummaryText(firstLine, 96)
	if result == "" {
		return record.ID
	}
	return result
}

// normalizeSummaryText 规范化摘要文本。
// 对应 Python: StoreProjectionHelper._normalize_summary_text(text, max_chars=96)
func normalizeSummaryText(text string, maxChars int) string {
	value := strings.TrimSpace(text)
	// 对齐 Python: value = re.sub(r"^#{1,6}\s*", "", value)
	headerRe := regexp.MustCompile(`^#{1,6}\s*`)
	value = headerRe.ReplaceAllString(value, "")
	value = strings.ReplaceAll(value, "|", " ")
	// 对齐 Python: value = re.sub(r"\s+", " ", value).strip()
	spaceRe := regexp.MustCompile(`\s+`)
	value = spaceRe.ReplaceAllString(value, " ")
	value = strings.TrimSpace(value)
	if maxChars <= 0 {
		maxChars = 96
	}
	if len(value) > maxChars {
		return strings.TrimRight(value[:maxChars-3], " ") + "..."
	}
	return value
}

// formatExperienceIndexTable 格式化经验索引表。
// 对应 Python: StoreProjectionHelper._format_experience_index_table(records)
func formatExperienceIndexTable(records []EvolutionRecord) []string {
	if len(records) == 0 {
		return nil
	}
	// 对齐 Python: sorted by timestamp desc, then score desc, then section
	ordered := make([]EvolutionRecord, len(records))
	copy(ordered, records)
	sort.SliceStable(ordered, func(i, j int) bool {
		return ordered[i].Timestamp > ordered[j].Timestamp
	})
	sort.SliceStable(ordered, func(i, j int) bool {
		return ordered[i].Score > ordered[j].Score
	})
	sort.SliceStable(ordered, func(i, j int) bool {
		return ordered[i].Change.Section < ordered[j].Change.Section
	})

	lines := []string{
		"### Experience Index",
		"",
		"| Summary | Type | Score | Detail |",
		"|---------|------|-------|--------|",
	}
	for _, record := range ordered {
		detailPath := fmt.Sprintf("evolution/%s#%s", sectionFilename(record.Change.Section), record.ID)
		lines = append(lines,
			fmt.Sprintf("| %s | %s | %.2f | [%s](%s) |",
				recordSummary(&record), record.Change.Section, record.Score, detailPath, detailPath))
	}
	lines = append(lines, "")
	return lines
}

// formatScriptAssetsTable 格式化脚本资产表。
// 对应 Python: StoreProjectionHelper._format_script_assets_table(records)
func formatScriptAssetsTable(records []EvolutionRecord) []string {
	if len(records) == 0 {
		return nil
	}
	ordered := make([]EvolutionRecord, len(records))
	copy(ordered, records)
	sort.SliceStable(ordered, func(i, j int) bool {
		return ordered[i].Timestamp > ordered[j].Timestamp
	})
	sort.SliceStable(ordered, func(i, j int) bool {
		return ordered[i].Score > ordered[j].Score
	})

	lines := []string{
		"### Script Assets",
		"",
		"| Summary | Language | Score | Index | Source |",
		"|---------|----------|-------|-------|--------|",
	}
	for _, record := range ordered {
		filename := record.ID
		if record.Change.ScriptFilename != nil {
			filename = *record.Change.ScriptFilename
		}
		source := fmt.Sprintf("evolution/scripts/%s", filename)
		lang := "unknown"
		if record.Change.ScriptLanguage != nil {
			lang = *record.Change.ScriptLanguage
		}
		lines = append(lines,
			fmt.Sprintf("| %s | %s | %.2f | [evolution/scripts/_index.md](evolution/scripts/_index.md) | [%s](%s) |",
				recordSummary(&record), lang, record.Score, source, source))
	}
	lines = append(lines, "")
	return lines
}

// clearDirRecursive 递归清空目录中的文件（从叶子开始删除）。
func clearDirRecursive(dir string) {
	filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if !d.IsDir() {
			os.Remove(path)
		}
		return nil
	})
	// 重新遍历删除空子目录
	filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil || path == dir {
			return nil
		}
		if d.IsDir() {
			entries, _ := os.ReadDir(path)
			if len(entries) == 0 {
				os.Remove(path)
			}
		}
		return nil
	})
}

// Host 侧演进命令 prompt builders（/evolve 系 slash 命令的一次性提示词）。
//
// 对应 Python 代码：openjiuwen/harness/rails/evolution/commands.py（182 行 1:1）。
package evolution

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_evolving/evoschema"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// FormatRelevantIndexPreview 格式化聚焦（非全量）索引预览（evolve prompts）。
// Python: format_relevant_index_preview()（commands.py:13-21）。
func FormatRelevantIndexPreview(indexPreview map[string]any) string {
	items := indexItems(indexPreview)
	if len(items) == 0 {
		return "Relevant Experience Index: empty."
	}
	lines := []string{"Relevant Experience Index (not the full list):"}
	lines = append(lines, formatIndexLines(items)...)
	if boolOf(indexPreview["has_more"]) {
		lines = append(lines, "More records may be relevant; use list_skill_experiences for focused lookup.")
	}
	return strings.Join(lines, "\n")
}

// FormatCompleteSummaryIndex 格式化完整（或显式截断）摘要索引（simplify prompts）。
// Python: format_complete_summary_index()（commands.py:24-40）。
func FormatCompleteSummaryIndex(index map[string]any, indexComplete bool) string {
	items := indexItems(index)
	if len(items) == 0 {
		return "Complete Experience Summary Index: empty."
	}
	header := "Complete Experience Summary Index:"
	if !indexComplete {
		header = "Partial Experience Summary Index (overflow exists):"
	}
	lines := []string{header}
	lines = append(lines, formatIndexLines(items)...)
	if !indexComplete || boolOf(index["has_more"]) {
		lines = append(lines,
			"This index is incomplete. Before submitting simplify actions, call list_skill_experiences "+
				"until overflow is covered, then read selected details with read_skill_experiences(record_ids=[...]).")
	}
	return strings.Join(lines, "\n")
}

// FormatRebuildContext 格式化确定性重建上下文（rebuild prompts）。
// Python: format_rebuild_context()（commands.py:43-64）。
func FormatRebuildContext(rebuildContext map[string]any) string {
	if len(rebuildContext) == 0 {
		return "Deterministic Rebuild Context: empty."
	}
	lines := []string{"Deterministic Rebuild Context:"}
	for _, item := range mapSliceOf(rebuildContext["records"]) {
		lines = append(lines, fmt.Sprintf(
			"\n[%s] %s\ntarget=%s section=%s score=%s\n%s",
			strOf(item["record_id"]), strOf(item["summary"]), strOf(item["target"]),
			strOf(item["section"]), strOf(item["score"]), strOf(item["content"]),
		))
	}
	overflow, _ := rebuildContext["overflow_index"].(map[string]any)
	if overflow != nil && len(mapSliceOf(overflow["items"])) > 0 {
		lines = append(lines, "\nOverflow Summary Index:")
		lines = append(lines, formatIndexLines(mapSliceOf(overflow["items"]))...)
		lines = append(lines, "Read overflow details only if they are needed for the rebuild.")
	}
	return strings.Join(lines, "\n")
}

// BuildSimplifyCommandPrompt 构建主动经验简化的一次性 host prompt。
// Python: build_simplify_command_prompt()（commands.py:67-86）。
func BuildSimplifyCommandPrompt(subject map[string]any, userIntent *string, fullIndex map[string]any, indexComplete bool) (string, error) {
	normalizedSubject, err := evoschema.NormalizeSubject(subject, nil)
	if err != nil {
		return "", err
	}
	intent := "Clean up duplicated, stale, or conflicting experiences."
	if userIntent != nil {
		intent = strings.TrimSpace(*userIntent)
	}
	return fmt.Sprintf(
		"Please perform skill evolution operation `simplify` for subject %s.\n"+
			"Intent: %s\n"+
			"%s\n"+
			"Read full content only for records you intend to delete, merge, refine, or use as merge evidence.\n"+
			"Submit cleanup actions with simplify_skill_experiences. Do not edit skill files directly.",
		mustJSON(normalizedSubject.ToPayload()), intent,
		FormatCompleteSummaryIndex(fullIndex, indexComplete),
	), nil
}

// BuildEvolveReviewCommandPrompt 构建走受限审查的主动 evolve host prompt。
// Python: build_evolve_review_command_prompt()（commands.py:89-126）。
func BuildEvolveReviewCommandPrompt(subject map[string]any, userIntent *string) (string, error) {
	normalizedSubject, err := evoschema.NormalizeSubject(subject, nil)
	if err != nil {
		return "", err
	}
	intent := ""
	if userIntent != nil {
		intent = strings.TrimSpace(*userIntent)
	}
	subjectLabel := "Skill"
	if normalizedSubject.Kind == "swarm-skill" {
		subjectLabel = "Swarm Skill"
	}
	subjectJSON := mustJSON(normalizedSubject.ToPayload())
	return fmt.Sprintf(
		"This is the %s evolution review workflow.\n"+
			"/evolve means the user explicitly requested an evolution review, but it does not mean "+
			"an experience must be written.\n"+
			"First decide whether the context shows a reusable need to improve this Skill: user correction, "+
			"missing capability, outdated knowledge, a better recurring approach, or an execution failure that "+
			"reveals missing Skill instructions.\n"+
			"Use concrete execution failures only as possible evidence that the Skill lacks reusable precheck, fallback, "+
			"verification, parameter, or troubleshooting guidance.\n"+
			"If the issue is a one-off task fact, user preference, temporary environment/permission/network "+
			"problem, or otherwise has no reusable Skill instruction change, the review should submit no_evolution.\n"+
			"user_intent is review direction, not proof that an experience should be written: "+
			"%s\n"+
			"Call prepare_skill_evolution with "+
			"subject=%s, "+
			"user_confirmed=true, "+
			"user_intent=%s.\n"+
			"Then run the review with evolve_review_task(evolution_review_ref=...) using the returned ref.\n"+
			"The review agent must call submit_evolution_review before you submit a proposal selection.\n"+
			"Read the validated review JSON from evolve_review_task.data.output, then submit "+
			"proposal_selection_for_submission with evolve_skill_experiences. "+
			"You must follow prepare -> evolve_review_task -> submit. "+
			"The main agent must not directly generate, edit, or rewrite proposals or experiences, and must not "+
			"edit Skill files directly.",
		subjectLabel, mustJSON(intent), subjectJSON, mustJSON(intent),
	), nil
}

// BuildRebuildCommandPrompt 构建 Skill 重建的一次性 host prompt。
// Python: build_rebuild_command_prompt()（commands.py:129-153）。
func BuildRebuildCommandPrompt(subject map[string]any, userIntent *string, rebuildContext map[string]any) (string, error) {
	normalizedSubject, err := evoschema.NormalizeSubject(subject, nil)
	if err != nil {
		return "", err
	}
	intent := "Rebuild the skill using the deterministic context."
	if userIntent != nil {
		intent = strings.TrimSpace(*userIntent)
	}
	creator := "skill-creator"
	if normalizedSubject.Kind == "swarm-skill" {
		creator = "swarmskill-creator"
	}
	minScoreLine := ""
	if rebuildContext != nil {
		if minScore, ok := rebuildContext["min_score"]; ok && minScore != nil {
			minScoreLine = fmt.Sprintf("Min score threshold: %s\n", strOf(minScore))
		}
	}
	return fmt.Sprintf(
		"Please perform skill evolution operation `evolve_rebuild` for subject %s.\n"+
			"Intent: %s\n"+
			"%s"+
			"Old version has been archived. Create the rebuilt skill body from the deterministic context below.\n"+
			"%s\n"+
			"Use %s for the rebuild and reset evolutions.json to an empty list.\n"+
			"Use list_skill_experiences/read_skill_experiences only for overflow records that are not already inlined.\n"+
			"Do not submit evolve_skill_experiences or simplify_skill_experiences for this rebuild.",
		mustJSON(normalizedSubject.ToPayload()), intent, minScoreLine,
		FormatRebuildContext(rebuildContext), creator,
	), nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// formatIndexLines 索引条目行。
// Python: _format_index_lines()（commands.py:156-172）。
func formatIndexLines(items []map[string]any) []string {
	lines := make([]string, 0, len(items))
	for _, item := range items {
		lines = append(lines, fmt.Sprintf(
			"- %s | %s | target=%s | section=%s | score=%s | updated_at=%s",
			strOf(item["record_id"]), strOf(item["summary"]), strOf(item["target"]),
			strOf(item["section"]), strOf(item["score"]), strOf(item["updated_at"]),
		))
	}
	return lines
}

// indexItems 安全取 index["items"] 列表。
func indexItems(index map[string]any) []map[string]any {
	if index == nil {
		return nil
	}
	return mapSliceOf(index["items"])
}

// mapSliceOf 把 any 断言为 []map[string]any（非该形状返回 nil）。
func mapSliceOf(v any) []map[string]any {
	if items, ok := v.([]map[string]any); ok {
		return items
	}
	return nil
}

// boolOf 断言 bool（缺省 false）。
func boolOf(v any) bool {
	b, _ := v.(bool)
	return b
}

// strOf 任意值 → 字符串（Python str() 直出语义）。
func strOf(v any) string {
	switch t := v.(type) {
	case nil:
		return ""
	case string:
		return t
	case float64:
		return fmt.Sprintf("%v", t)
	case int:
		return fmt.Sprintf("%d", t)
	case bool:
		return fmt.Sprintf("%t", t)
	default:
		return fmt.Sprintf("%v", t)
	}
}

// mustJSON 紧凑 JSON（ensure_ascii=False 语义：Go json 直接输出 UTF-8 原文）。
func mustJSON(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		return "null"
	}
	return string(b)
}

// buildActiveEvolutionFollowup 构造主动演进的 followup 结果（agent_prompt 模式）。
// Python 归属：skill_evolution_rail._build_user_evolution_request（:1066-1080）；
// team rail 经 super() 继承同壳（team_skill_evolution_rail.py:528），Go 组合继承
// 以包级助手承载（两 rail 共用，规则 9 归属注记）。
func buildActiveEvolutionFollowup(skillName string, subject map[string]string, userIntent *string) (*EvolutionRequestResult, error) {
	prompt, err := BuildEvolveReviewCommandPrompt(subjectStringMap(subject), userIntent)
	if err != nil {
		return nil, err
	}
	return &EvolutionRequestResult{
		SkillName:      skillName,
		Mode:           "agent_prompt",
		FollowupPrompt: &prompt,
	}, nil
}

// subjectStringMap map[string]string → map[string]any（evoschema 入参形状）。
func subjectStringMap(subject map[string]string) map[string]any {
	out := make(map[string]any, len(subject))
	for k, v := range subject {
		out[k] = v
	}
	return out
}

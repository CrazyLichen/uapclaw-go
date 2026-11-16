// member_selector.go 第 3 步：成员选择——确定性优先级选择器（零 LLM）。
// Python: member_optimizer/member_selector.py（全量对齐，330 行）。
// 从第 1/2 步预计算的归因报告中纯算术打分与选择。
// 评分：severity_weight和×10 + 平均confidence×5 + min(issue数,5)/5；
// 排序：score↓ → max_severity↓ → role↑（稳定）；上限 max_roles_per_run。

package memberoptimizer

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

// severityWeights 严重度权重表（未知严重度默认 1）。
// Python: member_selector.py:20-24 _SEVERITY_WEIGHTS。
var severityWeights = map[string]int{
	"critical": 4,
	"high":     3,
	"medium":   2,
	"low":      1,
}

// insufficientRoleEvidence 证据不足机制类型值。
// Python: member_selector.py:25 _INSUFFICIENT_ROLE_EVIDENCE。
const insufficientRoleEvidence = "insufficient_role_evidence"

// ──────────────────────────── 结构体 ────────────────────────────

// SelectorOptions MemberSelector.select 关键字参数收拢。
type SelectorOptions struct {
	// MinAttributionConfidence 最小归因置信度门槛（默认 0.5）
	MinAttributionConfidence float64
	// MaxRolesPerRun 每轮最大角色数（默认 2）
	MaxRolesPerRun int
	// SeverityMap 自定义严重度权重（nil 用默认表）
	SeverityMap map[string]int
}

// ──────────────────────────── 导出函数 ────────────────────────────

// SelectMemberTargets 从归因报告确定性选择优化角色。
// Python: member_selector.py:107-306 MemberSelector.select。
func SelectMemberTargets(roleAttributionReport *RoleAttributionReport, mechanismAttributionReport *MechanismAttributionReport, opts SelectorOptions) *MemberSelectionReport {
	// Python 默认值对齐（def 签名默认）
	if opts.MinAttributionConfidence == 0 {
		opts.MinAttributionConfidence = 0.5
	}
	if opts.MaxRolesPerRun == 0 {
		opts.MaxRolesPerRun = 2
	}
	severityMap := severityWeights
	if opts.SeverityMap != nil {
		severityMap = opts.SeverityMap
	}
	// Python: role_attrs = role_attribution_report.assigned_role_issues
	roleAttrs := roleAttributionReport.AssignedRoleIssues
	// Python: grouped = {}; for ri in role_attrs: grouped.setdefault(role, []).append(ri)
	grouped := map[string][]RoleIssueAttribution{}
	for _, ri := range roleAttrs {
		grouped[ri.Role] = append(grouped[ri.Role], ri)
	}
	// Python: candidates: list[RoleScore] = []; unselected_attrs = []; ...
	var roleScores []roleScore
	var unselectedAttrs []UnselectedAttribution
	filteredInsufficientCount := 0
	var deferredContractIssueIDs []string
	// Python 稳定遍历序：Go map 无序——按 role 排序遍历（Python dict 保插入序；键序偏差登记 spec §4）
	roles := make([]string, 0, len(grouped))
	for role := range grouped {
		roles = append(roles, role)
	}
	sort.Strings(roles)
	for _, role := range roles {
		attrs := grouped[role]
		// Python: eligible = []; for attr in attrs: ...
		var eligible []RoleIssueAttribution
		for _, attr := range attrs {
			// Python: if attr.confidence < min_attribution_confidence: continue
			if attr.Confidence < opts.MinAttributionConfidence {
				continue
			}
			// Python: mechanisms = _mechanisms_for_issue(...)
			mechanisms := mechanismsForIssue(mechanismAttributionReport, attr.Role, attr.IssueID)
			// Python: if not _has_actionable_mechanism(mechanisms): ...
			if !hasActionableMechanism(mechanisms) {
				filteredInsufficientCount++
				deferredContractIssueIDs = append(deferredContractIssueIDs, attr.IssueID)
				unselectedAttrs = append(unselectedAttrs, UnselectedAttribution{
					IssueID:    attr.IssueID,
					Role:       attr.Role,
					Reason:     "Mechanism attribution is insufficient_role_evidence; not actionable as a member harness change",
					Confidence: attr.Confidence,
				})
				continue
			}
			eligible = append(eligible, attr)
		}
		// Python: if not eligible: continue
		if len(eligible) == 0 {
			continue
		}
		// Python: severity_scores = [...]; severity_score = sum(...)
		severityScore := 0
		maxSev := 0
		for _, a := range eligible {
			weight := attributionSeverityWeight(a, severityMap)
			severityScore += weight
			if weight > maxSev {
				maxSev = weight
			}
		}
		// Python: confidence_score = sum(conf) / len(eligible)
		confSum := 0.0
		for _, a := range eligible {
			confSum += a.Confidence
		}
		confidenceScore := confSum / float64(len(eligible))
		// Python: issue_score = min(len(eligible), 5) / 5.0
		issueCount := len(eligible)
		if issueCount > 5 {
			issueCount = 5
		}
		issueScore := float64(issueCount) / 5.0
		// Python: score = severity_score * 10 + confidence_score * 5 + issue_score
		score := float64(severityScore)*10 + confidenceScore*5 + issueScore

		// Python: mechanism_types / optimization_surfaces 收集（dict.fromkeys 保序去重）
		var mechanismTypes []string
		var optimizationSurfaces []string
		seenMech := map[string]bool{}
		seenSurf := map[string]bool{}
		for _, ri := range eligible {
			for _, m := range mechanismAttributionReport.RoleMechanisms[ri.Role] {
				if m.IssueID == ri.IssueID && m.MechanismType != "" && !seenMech[m.MechanismType] {
					seenMech[m.MechanismType] = true
					mechanismTypes = append(mechanismTypes, m.MechanismType)
				}
				surface := strings.TrimSpace(m.OptimizationSurface)
				if m.IssueID == ri.IssueID && surface != "" && !seenSurf[surface] {
					seenSurf[surface] = true
					optimizationSurfaces = append(optimizationSurfaces, surface)
				}
			}
		}
		// Python: harness_ref_path / member_name 回查 candidate_roles
		harnessRefPath := ""
		memberName := ""
		for _, r := range roleAttributionReport.CandidateRoles {
			if r.Role == role {
				harnessRefPath = r.HarnessRefPath
				memberName = r.MemberName
				break
			}
		}
		// Python: issue_confidences = {a.issue_id: a.confidence for a in eligible}
		issueConfidences := map[string]float64{}
		attributedIssueIDs := make([]string, 0, len(eligible))
		for _, a := range eligible {
			attributedIssueIDs = append(attributedIssueIDs, a.IssueID)
			issueConfidences[a.IssueID] = a.Confidence
		}
		roleScores = append(roleScores, roleScore{
			role:                role,
			memberName:          memberName,
			harnessRefPath:      harnessRefPath,
			score:               score,
			maxSeverity:         maxSev,
			avgConfidence:       confidenceScore,
			issueCount:          len(eligible),
			attributedIssueIDs:  attributedIssueIDs,
			mechanismTypes:      mechanismTypes,
			optimizationSurfaces: optimizationSurfaces,
			issueConfidences:    issueConfidences,
		})
	}
	// Python: candidates.sort(key=lambda c: (-c.score, -c.max_severity, c.role))（稳定）
	sort.SliceStable(roleScores, func(i, j int) bool {
		if roleScores[i].score != roleScores[j].score {
			return roleScores[i].score > roleScores[j].score
		}
		if roleScores[i].maxSeverity != roleScores[j].maxSeverity {
			return roleScores[i].maxSeverity > roleScores[j].maxSeverity
		}
		return roleScores[i].role < roleScores[j].role
	})
	// Python: selected = candidates[:max_roles_per_run]
	selectedCount := len(roleScores)
	if selectedCount > opts.MaxRolesPerRun {
		selectedCount = opts.MaxRolesPerRun
	}
	// Python: for c in candidates[max_roles_per_run:]: 逐 issue 记 unselected
	for _, c := range roleScores[selectedCount:] {
		for _, issueID := range c.attributedIssueIDs {
			conf := c.avgConfidence
			if v, ok := c.issueConfidences[issueID]; ok {
				conf = v
			}
			unselectedAttrs = append(unselectedAttrs, UnselectedAttribution{
				IssueID:    issueID,
				Role:       c.role,
				Reason:     "Lower score than selected targets for this run",
				Confidence: conf,
			})
		}
	}
	// Python: selected_targets = [...]
	var selectedTargets []MemberOptimizationTarget
	for _, c := range roleScores[:selectedCount] {
		// Python: mechanism_summary = ", ".join(...) if ... else "unknown"
		mechanismSummary := "unknown"
		if len(c.mechanismTypes) > 0 {
			mechanismSummary = strings.Join(c.mechanismTypes, ", ")
		}
		// Python: reason = f"Score={c.score:.2f}. {c.issue_count} issue(s) attributed with avg_confidence={c.avg_confidence:.2f}. Mechanisms: {mechanism_summary}."
		reason := "Score=" + pyFormat2(c.score) + ". " + itoa(c.issueCount) + " issue(s) attributed with avg_confidence=" +
			pyFormat2(c.avgConfidence) + ". Mechanisms: " + mechanismSummary + "."
		// Python: evidence_refs=[{"issue_id": iid} for iid in c.attributed_issue_ids]
		evidenceRefs := make([]map[string]string, 0, len(c.attributedIssueIDs))
		for _, iid := range c.attributedIssueIDs {
			evidenceRefs = append(evidenceRefs, map[string]string{"issue_id": iid})
		}
		selectedTargets = append(selectedTargets, MemberOptimizationTarget{
			Role:                 c.role,
			HarnessRefPath:       c.harnessRefPath,
			AttributedIssueIDs:   c.attributedIssueIDs,
			Confidence:           c.avgConfidence,
			Reason:               reason,
			EvidenceRefs:         evidenceRefs,
			MechanismTypes:       c.mechanismTypes,
			OptimizationSurfaces: c.optimizationSurfaces,
			MemberName:           c.memberName,
		})
	}
	// Python: return MemberSelectionReport(...)
	deferredRoute := ""
	if len(deferredContractIssueIDs) > 0 {
		deferredRoute = "team_skill"
	}
	return &MemberSelectionReport{
		SelectionID:                   "member_selection_" + shortHex8(),
		SourceRoleAttributionPath:     "",
		SourceMechanismAttributionPath: "",
		Targets:                       selectedTargets,
		UnselectedAttributions:        unselectedAttrs,
		Metadata: map[string]any{
			"selector":                                   "deterministic_priority_selector",
			"scoring":                                    "severity_weight*10 + confidence*5 + issue_score",
			"min_attribution_confidence":                 opts.MinAttributionConfidence,
			"max_roles_per_run":                          opts.MaxRolesPerRun,
			"total_candidates":                           len(roleScores),
			"selected_count":                             selectedCount,
			"filtered_insufficient_role_evidence_count":  filteredInsufficientCount,
			"deferred_contract_issue_ids":                pyStrListUnique(deferredContractIssueIDs),
			"deferred_contract_route":                    deferredRoute,
		},
	}
}

// WriteMemberSelectionReport 落盘选择报告（member_selection.yaml）。
// Python: member_selector.py:308-329 write_report。
func WriteMemberSelectionReport(report *MemberSelectionReport, outputDir string) (string, error) {
	// Python: path = output_dir / "member_selection.yaml"; path.parent.mkdir(...)
	path := filepath.Join(outputDir, "member_selection.yaml")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return "", err
	}
	// Python: targets_out 组装
	var targetsOut []map[string]any
	for _, t := range report.Targets {
		targetsOut = append(targetsOut, map[string]any{
			"role":                 t.Role,
			"member_name":          t.MemberName,
			"harness_ref_path":     t.HarnessRefPath,
			"attributed_issue_ids": pyStrList(t.AttributedIssueIDs),
			"confidence":           t.Confidence,
			"reason":               t.Reason,
			"evidence_refs":        pyOrNoneTraceRefs(t.EvidenceRefs),
			"mechanism_types":      pyStrList(t.MechanismTypes),
			"optimization_surfaces": pyStrList(t.OptimizationSurfaces),
		})
	}
	// Python: unselected_out 组装
	var unselectedOut []map[string]any
	for _, u := range report.UnselectedAttributions {
		unselectedOut = append(unselectedOut, map[string]any{
			"issue_id":   u.IssueID,
			"role":       u.Role,
			"reason":     u.Reason,
			"confidence": u.Confidence,
		})
	}
	// Python: payload = {...}; yaml.safe_dump(payload, allow_unicode=True, sort_keys=False)
	payload := map[string]any{
		"selection_id":                      report.SelectionID,
		"source_role_attribution_path":      report.SourceRoleAttributionPath,
		"source_mechanism_attribution_path": report.SourceMechanismAttributionPath,
		"targets":                           pyOrNoneList(targetsOut),
		"unselected_attributions":           pyOrNoneList(unselectedOut),
		"metadata":                          pyOrNone(report.Metadata),
	}
	data, err := yaml.Marshal(payload)
	if err != nil {
		return "", err
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return "", err
	}
	return path, nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// roleScore 单候选角色打分明细。
// Python: member_selector.py:47-59 RoleScore。
type roleScore struct {
	role                 string
	memberName           string
	harnessRefPath       string
	score                float64
	maxSeverity          int
	avgConfidence        float64
	issueCount           int
	attributedIssueIDs   []string
	mechanismTypes       []string
	optimizationSurfaces []string
	issueConfidences     map[string]float64
}

// mechanismsForIssue 返回一个角色问题的机制归因列表。
// Python: member_selector.py:37-45 _mechanisms_for_issue。
func mechanismsForIssue(report *MechanismAttributionReport, role, issueID string) []RoleMechanismAttribution {
	var out []RoleMechanismAttribution
	for _, item := range report.RoleMechanisms[role] {
		if item.IssueID == issueID {
			out = append(out, item)
		}
	}
	return out
}

// hasActionableMechanism 机制归因是否能支撑 harness 修改。
// Python: member_selector.py:48-60 _has_actionable_mechanism。
func hasActionableMechanism(mechanisms []RoleMechanismAttribution) bool {
	// Python: if not mechanisms: return True
	if len(mechanisms) == 0 {
		return true
	}
	// Python: for item: if mechanism_type != insufficient and failure_signature != insufficient: return True
	for _, item := range mechanisms {
		mt := strings.ToLower(strings.TrimSpace(item.MechanismType))
		fs := strings.ToLower(strings.TrimSpace(item.FailureSignature))
		if mt != insufficientRoleEvidence && fs != insufficientRoleEvidence {
			return true
		}
	}
	return false
}

// attributionSeverityWeight 读取归因证据或元数据中的 severity 权重。
// Python: member_selector.py:27-45 _attribution_severity + _severity_weight。
func attributionSeverityWeight(attr RoleIssueAttribution, severityMap map[string]int) int {
	// Python: _attribution_severity——evidence 逐项 severity → metadata severity → ""
	severity := ""
	for _, item := range attr.Evidence {
		if s, ok := item["severity"].(string); ok && strings.TrimSpace(s) != "" {
			severity = strings.TrimSpace(s)
			break
		}
	}
	// Python: _severity_weight——未知默认 1
	weight, ok := severityMap[strings.ToLower(severity)]
	if !ok {
		return 1
	}
	return weight
}

// pyFormat2 等价 Python f"{value:.2f}"。
func pyFormat2(value float64) string {
	return fmt.Sprintf("%.2f", value)
}

// itoa 整数转字符串。
func itoa(v int) string {
	return strconv.Itoa(v)
}

// pyStrListUnique 保序去重（Python list(dict.fromkeys(...))）。
func pyStrListUnique(list []string) any {
	seen := map[string]bool{}
	out := make([]any, 0, len(list))
	for _, item := range list {
		if !seen[item] {
			seen[item] = true
			out = append(out, item)
		}
	}
	return out
}

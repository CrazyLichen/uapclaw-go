// loader.go 提供 member 优化的上游工件载入（17.4b1 切片）。
// Python: openjiuwen/rsi/harness_rsi/member_optimizer/loader.py。
// 本节仅落地 evaluation_result_analyzer 实际使用的符号闭包：
// EvalRef / load_eval_ref / resolve_candidate_roles 及其私有依赖。
// load_analysis_ref / resolve_team_issues / AnalysisUnavailableError /
// BoundedEvidenceBundle / load_case_evidence 及其依赖 ⤴️ 17.4c 补齐。

package memberoptimizer

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	harnessschema "github.com/uapclaw/uapclaw-go/internal/openjiuwen/agent_evolving/rsi/harness_rsi"
	"gopkg.in/yaml.v3"
)

// ──────────────────────────── 结构体 ────────────────────────────

// EvalRef eval_ref.yaml 的解析结果。
// Python: loader.py:23-68 EvalRef（dataclass → Go struct，偏差 D-5）。
type EvalRef struct {
	// EvalID 评估 ID
	EvalID string
	// TeamName 团队名
	TeamName string
	// TeamSkillRefPath team skill 引用路径
	TeamSkillRefPath string
	// HarnessRefsPath harness refs 引用路径
	HarnessRefsPath string
	// EvalDir 评估目录
	EvalDir string
	// CaseResultsDir 案例结果目录
	CaseResultsDir string
	// CaseTracesDir 案例轨迹目录
	CaseTracesDir string
	// SummaryPath 汇总文件路径（可缺失）
	SummaryPath *string
	// Cases 案例原始条目
	Cases []map[string]any
}

// ──────────────────────────── 导出函数 ────────────────────────────

// EvalRefFromDict 从原始 mapping 构建 EvalRef。
// Python: loader.py:37-68 EvalRef.from_dict
func EvalRefFromDict(data map[string]any, basePath *string) EvalRef {
	// Python: eval_dir = Path(data.get("eval_dir", ".")); 相对则拼 base_path
	evalDir := stringField(data, "eval_dir", ".")
	evalDir = joinWithBase(evalDir, basePath)

	// Python: cases = data.get("cases", [])
	var cases []map[string]any
	if rawCases, ok := data["cases"].([]any); ok {
		for _, item := range rawCases {
			if caseMap, isMap := item.(map[string]any); isMap {
				cases = append(cases, caseMap)
			}
		}
	}

	// Python: results_dir / traces_dir 默认 results / traces，相对则拼 base_path
	resultsDir := joinWithBase(stringField(data, "case_results_dir", "results"), basePath)
	tracesDir := joinWithBase(stringField(data, "case_traces_dir", "traces"), basePath)

	// Python: summary_path 存在且非空才解析，相对则拼 base_path
	var summaryPath *string
	if raw, ok := data["summary_path"]; ok {
		if str, isStr := raw.(string); isStr && str != "" {
			resolved := joinWithBase(str, basePath)
			summaryPath = &resolved
		}
	}

	return EvalRef{
		EvalID:           stringField(data, "eval_id", ""),
		TeamName:         stringField(data, "team_name", ""),
		TeamSkillRefPath: stringField(data, "team_skill_ref_path", ""),
		HarnessRefsPath:  stringField(data, "harness_refs_path", ""),
		EvalDir:          evalDir,
		CaseResultsDir:   resultsDir,
		CaseTracesDir:    tracesDir,
		SummaryPath:      summaryPath,
		Cases:            cases,
	}
}

// LoadEvalRef 载入并解析 eval_ref.yaml。
// 相对 eval_dir 等字段基于 yaml 所在目录解析。
// Python: loader.py:147-165 load_eval_ref
func LoadEvalRef(evalRefPath string) (EvalRef, error) {
	// Python: path = Path(eval_ref_path).expanduser().resolve()
	path, err := expandUser(evalRefPath)
	if err != nil {
		return EvalRef{}, err
	}
	if path, err = filepath.Abs(path); err != nil {
		return EvalRef{}, err
	}
	// Python: if not path.is_file(): raise FileNotFoundError(...)
	if info, statErr := os.Stat(path); statErr != nil || info.IsDir() {
		return EvalRef{}, fmt.Errorf("eval_ref not found: %s", path)
	}

	// Python: data = yaml.safe_load(f) or {}
	data, err := loadYAMLMapping(path)
	if err != nil {
		return EvalRef{}, err
	}

	// Python: eval_dir 相对则基于 path.parent；from_dict 内再次解析（已绝对则保持）
	parent := dirOf(path)

	// Python: return EvalRef.from_dict(data, base_path=path.parent)
	return EvalRefFromDict(data, &parent), nil
}

// ResolveCandidateRoles 从 harness refs 与可选 Team Skill 绑定解析候选角色。
// 解析顺序（spec Section 2.6.3）：
//  1. 直接传入的 harnessRefsPath
//  2. 回落 evalRef.HarnessRefsPath
//  3. 空列表（由调用方决定 no-op 或失败）
//
// Python: loader.py:254-283 resolve_candidate_roles
func ResolveCandidateRoles(harnessRefsPath string, evalRef *EvalRef, teamSkillRefPath string) []MemberRoleCandidate {
	var candidates []MemberRoleCandidate

	// Python: refs_path = _normalize_harness_refs_path(harness_refs_path)
	refsPath := normalizeHarnessRefsPath(harnessRefsPath)
	if refsPath == "" {
		refsPath = normalizeHarnessRefsPath(evalRef.HarnessRefsPath)
	}

	// Python: if refs_path is None: return candidates
	if refsPath == "" {
		return candidates
	}

	// Python: is_file → _parse_harness_refs_file；is_dir → _parse_harness_refs_dir
	if info, err := os.Stat(refsPath); err == nil {
		if !info.IsDir() {
			candidates = parseHarnessRefsFile(refsPath)
		} else {
			candidates = parseHarnessRefsDir(refsPath)
		}
	}

	// Python: if not candidates and team_skill_ref_path: 兜底
	if len(candidates) == 0 && teamSkillRefPath != "" {
		candidates = supplementFromTeamSkill(teamSkillRefPath, evalRef.EvalDir)
	}

	return candidates
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// normalizeHarnessRefsPath 归一化 harness refs 路径：空/不存在返回空串。
// Python: loader.py:247-251 _normalize_harness_refs_path
func normalizeHarnessRefsPath(harnessRefsPath string) string {
	if harnessRefsPath == "" {
		return ""
	}
	p, err := expandUser(harnessRefsPath)
	if err != nil {
		return ""
	}
	if p, err = filepath.Abs(p); err != nil {
		return ""
	}
	// Python: return p if p.exists() else None
	if _, statErr := os.Stat(p); statErr != nil {
		return ""
	}
	return p
}

// resolveHarnessRefPath 相对 harness ref 基于 refs 文件位置解析。
// Python: loader.py:286-293 _resolve_harness_ref_path
func resolveHarnessRefPath(ref any, refsPath string) string {
	str, isStr := ref.(string)
	if !isStr || strings.TrimSpace(str) == "" {
		return ""
	}
	path, err := expandUser(str)
	if err != nil {
		return ""
	}
	if filepath.IsAbs(path) {
		return path
	}
	// Python: str((refs_path.parent / path).resolve())
	resolved, err := filepath.Abs(filepath.Join(filepath.Dir(refsPath), path))
	if err != nil {
		return ""
	}
	return resolved
}

// parseHarnessRefsFile 解析 harness_refs.yaml / .json 为候选角色列表。
// Python: loader.py:296-351 _parse_harness_refs_file
func parseHarnessRefsFile(refsPath string) []MemberRoleCandidate {
	data, err := loadMappingByExt(refsPath)
	if err != nil {
		data = map[string]any{}
	}

	var candidates []MemberRoleCandidate

	// Python: if "roles" in data and isinstance(list): ...
	if roles, ok := data["roles"].([]any); ok {
		for _, entry := range roles {
			roleEntry, isMap := entry.(map[string]any)
			if !isMap {
				continue
			}
			candidates = append(candidates, MemberRoleCandidate{
				Role:           stringField(roleEntry, "role", ""),
				MemberName:     stringField(roleEntry, "member_name", ""),
				HarnessRefPath: resolveHarnessRefPath(roleEntry["harness_ref_path"], refsPath),
				Description:    stringField(roleEntry, "description", ""),
				Metadata:       mapField(roleEntry, "metadata"),
			})
		}
		return candidates
	}

	// Python: harness_refs = data.get("harness_refs", {}); isinstance(dict)
	if harnessRefs, ok := data["harness_refs"].(map[string]any); ok {
		for role, ref := range harnessRefs {
			if _, isStr := ref.(string); !isStr {
				continue
			}
			candidates = append(candidates, MemberRoleCandidate{
				Role:           role,
				MemberName:     role,
				HarnessRefPath: resolveHarnessRefPath(ref, refsPath),
				Description:    "",
				Metadata:       map[string]any{"source": "harness_refs"},
			})
		}
		return candidates
	}

	// Python: 裸 mapping：跳过 version / source 键
	for role, ref := range data {
		if _, isStr := ref.(string); isStr && role != "version" && role != "source" {
			candidates = append(candidates, MemberRoleCandidate{
				Role:           role,
				MemberName:     role,
				HarnessRefPath: resolveHarnessRefPath(ref, refsPath),
				Description:    "",
				Metadata:       map[string]any{"source": "harness_refs"},
			})
		}
	}
	return candidates
}

// parseHarnessRefsDir 解析 harness refs 目录：子目录名成为角色（按目录名排序）。
// Python: loader.py:354-368 _parse_harness_refs_dir
func parseHarnessRefsDir(refsPath string) []MemberRoleCandidate {
	var candidates []MemberRoleCandidate
	entries, err := os.ReadDir(refsPath)
	if err != nil {
		return candidates
	}
	// Python: sorted(refs_path.iterdir())（按名字排序）
	names := make([]string, 0, len(entries))
	byName := map[string]string{}
	for _, entry := range entries {
		if entry.IsDir() {
			names = append(names, entry.Name())
			byName[entry.Name()] = filepath.Join(refsPath, entry.Name())
		}
	}
	sort.Strings(names)
	for _, name := range names {
		candidates = append(candidates, MemberRoleCandidate{
			Role:           name,
			MemberName:     name,
			HarnessRefPath: byName[name],
			Description:    "",
			Metadata:       map[string]any{"source": "directory"},
		})
	}
	return candidates
}

// supplementFromTeamSkill 从 TeamAgentSpec 绑定补充候选角色。
// Python: loader.py:371-414 _supplement_from_team_skill
func supplementFromTeamSkill(teamSkillRefPath string, basePath string) []MemberRoleCandidate {
	var candidates []MemberRoleCandidate
	// Python: path = base_path / team_skill_ref_path if not absolute
	path := teamSkillRefPath
	if !filepath.IsAbs(path) && basePath != "" {
		path = filepath.Join(basePath, path)
	}

	if info, statErr := os.Stat(path); statErr != nil || info.IsDir() {
		return candidates
	}

	data, err := loadMappingByExt(path)
	if err != nil {
		return candidates
	}

	// Python: leader = data.get("leader", {}); member_name
	if leader, ok := data["leader"].(map[string]any); ok {
		if memberName := stringField(leader, "member_name", ""); memberName != "" {
			candidates = append(candidates, MemberRoleCandidate{
				Role:           memberName,
				MemberName:     memberName,
				HarnessRefPath: "",
				Description:    "Team leader",
				Metadata:       map[string]any{"source": "team_skill_binding"},
			})
		}
	}

	// Python: for member in data.get("predefined_members", [])
	if members, ok := data["predefined_members"].([]any); ok {
		for _, entry := range members {
			member, isMap := entry.(map[string]any)
			if !isMap {
				continue
			}
			if mname := stringField(member, "member_name", ""); mname != "" {
				candidates = append(candidates, MemberRoleCandidate{
					Role:           mname,
					MemberName:     mname,
					HarnessRefPath: stringField(member, "harness_ref_path", ""),
					Description:    stringField(member, "description", ""),
					Metadata:       map[string]any{"source": "team_skill_binding"},
				})
			}
		}
	}

	return candidates
}

// loadYAMLMapping 读取 YAML 文件为 mapping（空文件得空 mapping）。
// Python: loader.py:158-159 / 183-184 / 199-200 的 yaml.safe_load(f) or {}
func loadYAMLMapping(path string) (map[string]any, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var data map[string]any
	if err := yaml.Unmarshal(content, &data); err != nil {
		return nil, err
	}
	if data == nil {
		data = map[string]any{}
	}
	return data, nil
}

// loadMappingByExt 按后缀分派 YAML/JSON 读取 mapping。
// Python: loader.py:298-302 / 382-386
func loadMappingByExt(path string) (map[string]any, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	ext := strings.ToLower(filepath.Ext(path))
	var data map[string]any
	if ext == ".yaml" || ext == ".yml" {
		if err := yaml.Unmarshal(content, &data); err != nil {
			return nil, err
		}
	} else {
		if err := json.Unmarshal(content, &data); err != nil {
			return nil, err
		}
	}
	if data == nil {
		data = map[string]any{}
	}
	return data, nil
}

// stringField 读取 mapping 的字符串字段（缺失/类型不符取默认值）。
func stringField(data map[string]any, key, fallback string) string {
	if raw, ok := data[key]; ok {
		if str, isStr := raw.(string); isStr {
			return str
		}
	}
	return fallback
}

// mapField 读取 mapping 字段（缺失/类型不符得空 map）。
func mapField(data map[string]any, key string) map[string]any {
	if raw, ok := data[key]; ok {
		if m, isMap := raw.(map[string]any); isMap {
			return m
		}
	}
	return map[string]any{}
}

// joinWithBase 相对路径拼 basePath（绝对路径或 nil basePath 保持原样）。
// Python: if base_path is not None and not p.is_absolute(): p = base_path / p
func joinWithBase(path string, basePath *string) string {
	if basePath == nil || filepath.IsAbs(path) {
		return path
	}
	return filepath.Join(*basePath, path)
}

// dirOf 返回路径的父目录。
func dirOf(path string) string {
	return filepath.Dir(path)
}

// ───────────────────────── 17.4c1 补齐段 ─────────────────────────
// 以下对齐 Python loader.py:72-245 与 417-658（AnalysisRef / AnalysisUnavailableError /
// BoundedEvidenceBundle / load_analysis_ref / resolve_team_issues / load_case_evidence 及助手）。

// AnalysisRef analysis_ref.yaml 的解析结果。
// Python: loader.py:72-99 AnalysisRef（issues 为原始 dict，由调用方转 TeamIssue）。
type AnalysisRef struct {
	// Issues 原始问题字典列表
	Issues []map[string]any
	// IssuesPath 外部问题清单路径（已解析为绝对路径；未设置为空）
	IssuesPath string
	// Metadata 元数据
	Metadata map[string]any
	// AnalysisRefPath 解析后的 analysis_ref.yaml 绝对路径（文件缺失时为空）
	AnalysisRefPath string
}

// DiagnosisIncomplete 容错旧产物错标"failed diagnosis completed"。
// Python: loader.py:81-88 diagnosis_incomplete property。
func (r *AnalysisRef) DiagnosisIncomplete() bool {
	if r.Metadata == nil {
		return false
	}
	switch status, _ := r.Metadata["analysis_status"].(string); status {
	case "failed", "unavailable", "partial", "empty_case_results":
		return true
	}
	switch v := r.Metadata["diagnosis_failed_count"].(type) {
	case bool:
		return v
	case float64:
		return v != 0
	case int:
		return v != 0
	case string:
		return v != ""
	}
	return false
}

// RequireUsable 不完整诊断且无 issues 时拒绝，避免把失败当成"无问题"。
// Python: loader.py:90-99 require_usable。
func (r *AnalysisRef) RequireUsable() error {
	if r.DiagnosisIncomplete() && len(r.Issues) == 0 {
		perCase := ""
		if r.Metadata != nil {
			perCase, _ = r.Metadata["per_case_diagnoses_path"].(string)
		}
		return &AnalysisUnavailableError{
			Msg: "Analyzer did not complete a usable diagnosis; this is not a no-issues result. " +
				"The evaluation is preserved. Inspect analysis_ref.yaml and per_case_diagnoses.json " +
				"before resuming analysis.",
			AnalysisRefPath:      r.AnalysisRefPath,
			PerCaseDiagnosesPath: perCase,
		}
	}
	return nil
}

// AnalysisUnavailableError 不完整诊断不可当作空优化结果。
// Python: loader.py:101-136 AnalysisUnavailableError(RuntimeError)。
type AnalysisUnavailableError struct {
	// Msg 错误消息
	Msg string
	// AnalysisRefPath analysis_ref.yaml 路径
	AnalysisRefPath string
	// PerCaseDiagnosesPath 逐案诊断文件路径
	PerCaseDiagnosesPath string
}

// Error 实现 error 接口。
func (e *AnalysisUnavailableError) Error() string { return e.Msg }

// Details 保留原始诊断失败信息而不伪造 issue。
// Python: loader.py:111-136 details。
func (e *AnalysisUnavailableError) Details() map[string]any {
	result := map[string]any{
		"status":                  "unavailable",
		"error_type":              "AnalysisUnavailableError",
		"message":                 e.Msg,
		"analysis_ref_path":       e.AnalysisRefPath,
		"per_case_diagnoses_path": e.PerCaseDiagnosesPath,
		"diagnosis_errors":        []map[string]any{},
	}
	path := e.PerCaseDiagnosesPath
	if path != "" && !filepath.IsAbs(path) && e.AnalysisRefPath != "" {
		path = filepath.Join(filepath.Dir(e.AnalysisRefPath), path)
	}
	if path != "" {
		content, err := os.ReadFile(path)
		if err != nil {
			result["evidence_read_error"] = err.Error()
			return result
		}
		var payload map[string]any
		if err := json.Unmarshal(content, &payload); err != nil {
			result["evidence_read_error"] = err.Error()
			return result
		}
		rows, _ := payload["per_case_diagnoses"].([]any)
		fields := []string{"case_id", "diagnosis_status", "root_cause", "error", "output_repair"}
		diagnosisErrors := []map[string]any{}
		for _, raw := range rows {
			row, ok := raw.(map[string]any)
			if !ok || row["analysis_failed"] != true {
				continue
			}
			filtered := map[string]any{}
			for _, key := range fields {
				filtered[key] = row[key]
			}
			diagnosisErrors = append(diagnosisErrors, filtered)
		}
		result["diagnosis_errors"] = diagnosisErrors
	}
	return result
}

// BoundedEvidenceBundle 单个 TeamIssue 的有界证据包（供归因使用）。
// Python: loader.py:138-145 BoundedEvidenceBundle。
type BoundedEvidenceBundle struct {
	// Issue 原始问题字典
	Issue map[string]any
	// CaseResults 案例结果（有界）
	CaseResults []map[string]any
	// CaseTraces 案例轨迹（有界）
	CaseTraces []map[string]any
	// CandidateRoles 候选角色
	CandidateRoles []MemberRoleCandidate
}

// LoadAnalysisRef 载入并解析 analysis_ref.yaml。
// Python: loader.py:168-210 load_analysis_ref（三级解析：内联 issues → issues_path 文件 → 空）。
func LoadAnalysisRef(analysisResultPath string) *AnalysisRef {
	path, err := expandUser(analysisResultPath)
	if err != nil {
		return &AnalysisRef{Issues: []map[string]any{}}
	}
	if abs, err := filepath.Abs(path); err == nil {
		path = abs
	}
	info, statErr := os.Stat(path)
	if statErr != nil || info.IsDir() {
		// Python: return AnalysisRef(issues=[], issues_path=None)
		return &AnalysisRef{Issues: []map[string]any{}}
	}
	data, err := loadYAMLMapping(path)
	if err != nil {
		return &AnalysisRef{Issues: []map[string]any{}}
	}
	metadata := mapField(data, "metadata")
	issuesPathStr, _ := data["issues_path"].(string)

	// Python: if isinstance(inline_issues, list) and inline_issues: ...
	if inlineIssues, ok := data["issues"].([]any); ok && len(inlineIssues) > 0 {
		return &AnalysisRef{
			Issues:          anyListToStringMapList(inlineIssues),
			IssuesPath:      "",
			Metadata:        metadata,
			AnalysisRefPath: path,
		}
	}
	_ = issuesPathStr
	if issuesPathStr != "" {
		issuesPath, err := expandUser(issuesPathStr)
		if err == nil {
			if !filepath.IsAbs(issuesPath) {
				issuesPath = filepath.Join(filepath.Dir(path), issuesPath)
			}
			if info, err := os.Stat(issuesPath); err == nil && !info.IsDir() {
				if issuesData, err := loadYAMLMapping(issuesPath); err == nil {
					if rawIssues, ok := issuesData["issues"].([]any); ok && len(rawIssues) > 0 {
						return &AnalysisRef{
							Issues:          anyListToStringMapList(rawIssues),
							IssuesPath:      issuesPath,
							Metadata:        metadata,
							AnalysisRefPath: path,
						}
					}
				}
			}
		}
	}
	return &AnalysisRef{Issues: []map[string]any{}, Metadata: metadata, AnalysisRefPath: path}
}

// ResolveTeamIssues 把原始问题字典转为 TeamIssue（仅保留 member_harness 目标）。
// Python: loader.py:213-245 resolve_team_issues。
func ResolveTeamIssues(analysisRef *AnalysisRef) []harnessschema.TeamIssue {
	issues := []harnessschema.TeamIssue{}
	for _, item := range analysisRef.Issues {
		// Python: if not isinstance(item, dict): continue（LoadAnalysisRef 已保证 dict）
		rawTarget, _ := item["optimization_target"].(string)
		optimizationTarget := strings.ToLower(strings.ReplaceAll(strings.TrimSpace(rawTarget), "-", "_"))
		if optimizationTarget != "member_harness" {
			continue
		}
		metadata := mapField(item, "metadata")
		metadata = copyStringAnyMap(metadata)
		for _, key := range []string{"affected_component", "component", "signals_method"} {
			if v, exists := item[key]; exists {
				if _, inMeta := metadata[key]; !inMeta {
					metadata[key] = v
				}
			}
		}
		issues = append(issues, harnessschema.TeamIssue{
			IssueID:            stringField(item, "issue_id", ""),
			Category:           stringField(item, "category", ""),
			Severity:           stringField(item, "severity", ""),
			Summary:            stringField(item, "summary", ""),
			AffectedCases:      stringSliceField(item, "affected_cases"),
			Evidence:           evidenceField(item, "evidence"),
			SuspectedTeamScope: stringField(item, "suspected_team_scope", ""),
			OptimizationTarget: optimizationTarget,
			TargetMembers:      stringSliceField(item, "target_members"),
			Recommendation:     stringField(item, "recommendation", ""),
			Metadata:           metadata,
		})
	}
	return issues
}

// LoadCaseEvidence 载入给定案例的有界结果与轨迹证据。
// Python: loader.py:427-479 load_case_evidence（返回 case_id → {result, trace}）。
func LoadCaseEvidence(evalRef *EvalRef, caseIDs []string, maxCases int, maxTraceChars int, maxResultChars int) map[string]map[string]map[string]any {
	if len(caseIDs) == 0 {
		return map[string]map[string]map[string]any{}
	}
	evidence := map[string]map[string]map[string]any{}
	if maxCases < len(caseIDs) {
		caseIDs = caseIDs[:maxCases]
	}
	for _, caseID := range caseIDs {
		var caseEntry map[string]any
		for _, c := range evalRef.Cases {
			if id, _ := c["case_id"].(string); id == caseID {
				caseEntry = c
				break
			}
		}
		resultData := map[string]any{}
		traceData := map[string]any{}
		if caseEntry != nil {
			if resultPath, ok := caseEntry["result_path"].(string); ok && resultPath != "" {
				p := resolveCaseArtifactPath(resultPath, evalRef.EvalDir)
				if info, err := os.Stat(p); err == nil && !info.IsDir() {
					resultData = loadJSONOrYAML(p)
				}
			}
			if tracePath, ok := caseEntry["trace_path"].(string); ok && tracePath != "" {
				p := resolveCaseArtifactPath(tracePath, evalRef.EvalDir)
				if info, err := os.Stat(p); err == nil && !info.IsDir() {
					traceData = loadJSONOrYAML(p)
				}
			}
		}
		if _, exists := resultData["case_id"]; !exists {
			resultData["case_id"] = caseID
		}
		if _, exists := traceData["case_id"]; !exists {
			traceData["case_id"] = caseID
		}
		resultData["response"] = excerpt(extractResponseText(resultData), maxResultChars)
		resultData["result"] = excerpt(extractResultText(resultData), maxResultChars)
		traceData["response"] = excerpt(extractResponseText(traceData), maxTraceChars)
		if behaviorTrace, ok := traceData["behavior_trace"].(map[string]any); ok {
			traceData["behavior_trace"] = boundBehaviorTrace(behaviorTrace)
		}
		evidence[caseID] = map[string]map[string]any{"result": resultData, "trace": traceData}
	}
	return evidence
}

// ───────────────────────── 非导出助手（17.4c1 补齐段） ─────────────────────────

// excerpt 截断文本并在截断时加省略号。Python: loader.py:417-424 _excerpt。
func excerpt(text string, maxChars int) string {
	if text == "" {
		return ""
	}
	if len(text) <= maxChars {
		return text
	}
	return text[:maxChars-3] + "..."
}

// resolveCaseArtifactPath 案例产物路径按 eval_dir 解析。Python: loader.py:481-486。
func resolveCaseArtifactPath(value string, evalDir string) string {
	path, err := expandUser(value)
	if err != nil {
		return value
	}
	if filepath.IsAbs(path) {
		return path
	}
	return filepath.Join(evalDir, path)
}

// extractResponseText 从正式 result/trace 字段读响应摘要，不改产物。
// Python: loader.py:488-505 _extract_response_text。
func extractResponseText(data map[string]any) string {
	for _, key := range []string{"response", "output", "answer"} {
		if value, exists := data[key]; exists && value != nil && !isEmptyValue(value) {
			return stringifyEvidenceValue(value)
		}
	}
	result, exists := data["result"]
	if !exists || result == nil || isEmptyValue(result) {
		return ""
	}
	if resultMap, ok := result.(map[string]any); ok {
		for _, key := range []string{"output", "response", "answer", "result"} {
			if value, exists := resultMap[key]; exists && value != nil && !isEmptyValue(value) {
				return stringifyEvidenceValue(value)
			}
		}
		return ""
	}
	return stringifyEvidenceValue(result)
}

// extractResultText 读 result 字段，回落响应摘要。Python: loader.py:506-511。
func extractResultText(data map[string]any) string {
	if result, exists := data["result"]; exists && result != nil && !isEmptyValue(result) {
		return stringifyEvidenceValue(result)
	}
	return extractResponseText(data)
}

// isEmptyValue 复刻 Python 真值判断（空串/空列表/空 map/False 均为假）。
func isEmptyValue(value any) bool {
	switch v := value.(type) {
	case string:
		return v == ""
	case []any:
		return len(v) == 0
	case []string:
		return len(v) == 0
	case map[string]any:
		return len(v) == 0
	case bool:
		return !v
	case nil:
		return true
	}
	return false
}

// stringifyEvidenceValue 字符串原样，其余 JSON 序列化（键排序、非 ASCII 不转义）。
// Python: loader.py:513-520 _stringify_evidence_value。
func stringifyEvidenceValue(value any) string {
	if s, ok := value.(string); ok {
		return s
	}
	// Python: json.dumps(value, ensure_ascii=False, sort_keys=True)（默认分隔符 ", "/": "）
	return pyJSONDump(value)
}

// pyJSONDump 复刻 Python json.dumps(value, ensure_ascii=False, sort_keys=True) 的默认分隔符风格。
// Go encoding/json 输出紧凑分隔符；Python 默认为 ", " 与 ": "，证据文本喂给 LLM 提示词，
// 逐字对齐要求保留 Python 风格。
func pyJSONDump(value any) string {
	var b strings.Builder
	pyJSONDumpValue(&b, value)
	return b.String()
}

// pyJSONDumpValue 递归输出 Python 风格 JSON。
func pyJSONDumpValue(b *strings.Builder, value any) {
	switch v := value.(type) {
	case nil:
		b.WriteString("null")
	case bool:
		if v {
			b.WriteString("true")
		} else {
			b.WriteString("false")
		}
	case string:
		b.WriteString(pyJSONQuote(v))
	case float64:
		b.WriteString(pyJSONNumber(v))
	case int:
		b.WriteString(strconv.Itoa(v))
	case int64:
		b.WriteString(strconv.FormatInt(v, 10))
	case []any:
		b.WriteByte('[')
		for i, item := range v {
			if i > 0 {
				b.WriteString(", ")
			}
			pyJSONDumpValue(b, item)
		}
		b.WriteByte(']')
	case []string:
		b.WriteByte('[')
		for i, item := range v {
			if i > 0 {
				b.WriteString(", ")
			}
			pyJSONDumpValue(b, item)
		}
		b.WriteByte(']')
	case []map[string]any:
		b.WriteByte('[')
		for i, item := range v {
			if i > 0 {
				b.WriteString(", ")
			}
			pyJSONDumpValue(b, item)
		}
		b.WriteByte(']')
	case map[string]any:
		keys := make([]string, 0, len(v))
		for k := range v {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		b.WriteByte('{')
		for i, k := range keys {
			if i > 0 {
				b.WriteString(", ")
			}
			b.WriteString(pyJSONQuote(k))
			b.WriteString(": ")
			pyJSONDumpValue(b, v[k])
		}
		b.WriteByte('}')
	default:
		b.WriteString(pyJSONQuote(fmt.Sprintf("%v", v)))
	}
}

// pyJSONQuote 复刻 json.dumps 字符串转义（ensure_ascii=False，保留非 ASCII）。
func pyJSONQuote(s string) string {
	var b strings.Builder
	b.WriteByte('"')
	for _, r := range s {
		switch r {
		case '"':
			b.WriteString(`\"`)
		case '\\':
			b.WriteString(`\\`)
		case '\n':
			b.WriteString(`\n`)
		case '\r':
			b.WriteString(`\r`)
		case '\t':
			b.WriteString(`\t`)
		case '\b':
			b.WriteString(`\b`)
		case '\f':
			b.WriteString(`\f`)
		default:
			if r < 0x20 {
				b.WriteString(fmt.Sprintf(`\u%04x`, r))
			} else {
				b.WriteRune(r)
			}
		}
	}
	b.WriteByte('"')
	return b.String()
}

// pyJSONNumber 复刻 Python float 的 JSON 输出（整数不带小数点）。
func pyJSONNumber(f float64) string {
	if f == float64(int64(f)) && math.Abs(f) < 1e15 {
		return strconv.FormatInt(int64(f), 10)
	}
	return strconv.FormatFloat(f, 'g', -1, 64)
}

// loadJSONOrYAML 按后缀载入 YAML 或 JSON。Python: loader.py:522-528 _load_json_or_yaml。
func loadJSONOrYAML(path string) map[string]any {
	content, err := os.ReadFile(path)
	if err != nil {
		return map[string]any{}
	}
	var data map[string]any
	ext := strings.ToLower(filepath.Ext(path))
	switch ext {
	case ".yaml", ".yml":
		if err := yaml.Unmarshal(content, &data); err != nil {
			return map[string]any{}
		}
	default:
		if err := json.Unmarshal(content, &data); err != nil {
			return map[string]any{}
		}
	}
	if data == nil {
		data = map[string]any{}
	}
	return data
}

// boundBehaviorTrace 有界化行为轨迹。Python: loader.py:530-567 _bound_behavior_trace。
func boundBehaviorTrace(behaviorTrace map[string]any) map[string]any {
	bounded := map[string]any{}
	if commandLog, ok := behaviorTrace["command_log"].([]any); ok {
		entries := []map[string]any{}
		for _, raw := range commandLog {
			if len(entries) >= 40 {
				break
			}
			item, ok := raw.(map[string]any)
			if !ok {
				continue
			}
			entries = append(entries, map[string]any{
				"command":        excerpt(stringField(item, "command", ""), 600),
				"cwd":            stringField(item, "cwd", ""),
				"exit_code":      item["exit_code"],
				"stdout_excerpt": excerpt(stringField(item, "stdout_excerpt", ""), 1200),
				"stderr_excerpt": excerpt(stringField(item, "stderr_excerpt", ""), 1200),
				"timeout_sec":    item["timeout_sec"],
				"background":     boolField(item, "background"),
			})
		}
		bounded["command_log"] = entries
	}
	if workspaceChanges, ok := behaviorTrace["workspace_changes"].(map[string]any); ok {
		bounded["workspace_changes"] = map[string]any{
			"added":    boundedStringList(workspaceChanges["added"], 80),
			"modified": boundedStringList(workspaceChanges["modified"], 80),
			"removed":  boundedStringList(workspaceChanges["removed"], 80),
		}
	}
	if v, ok := behaviorTrace["trajectory_events_path"].(string); ok {
		bounded["trajectory_events_path"] = v
	}
	if summary, ok := behaviorTrace["trajectory_window_summary"].(map[string]any); ok {
		bounded["trajectory_window_summary"] = boundTrajectoryWindowSummary(summary)
	}
	if v, ok := behaviorTrace["normalized_trace_path"].(string); ok {
		bounded["normalized_trace_path"] = v
	}
	if summary, ok := behaviorTrace["normalized_trace_summary"].(map[string]any); ok {
		bounded["normalized_trace_summary"] = boundNormalizedTraceSummary(summary)
	}
	return bounded
}

// boundTrajectoryWindowSummary 有界化轨迹窗口摘要。Python: loader.py:569-587。
func boundTrajectoryWindowSummary(summary map[string]any) map[string]any {
	bounded := map[string]any{
		"window_size":        summary["window_size"],
		"event_count":        summary["event_count"],
		"failure_signatures": boundedStringList(summary["failure_signatures"], 20),
	}
	if recentEvents, ok := summary["recent_events"].([]any); ok {
		events := []map[string]any{}
		for _, raw := range recentEvents {
			if len(events) >= 30 {
				break
			}
			item, ok := raw.(map[string]any)
			if !ok {
				continue
			}
			events = append(events, map[string]any{
				"event_index": item["event_index"],
				"event_type":  excerpt(stringField(item, "event_type", ""), 80),
				"summary":     excerpt(stringField(item, "summary", ""), 1200),
			})
		}
		bounded["recent_events"] = events
	}
	return bounded
}

// boundNormalizedTraceSummary 有界化规范化轨迹摘要。Python: loader.py:589-621。
func boundNormalizedTraceSummary(summary map[string]any) map[string]any {
	boundedTraces := []map[string]any{}
	if traces, ok := summary["traces"].([]any); ok {
		for _, raw := range traces {
			if len(boundedTraces) >= 4 {
				break
			}
			trace, ok := raw.(map[string]any)
			if !ok {
				continue
			}
			boundedMessages := []map[string]any{}
			if messages, ok := trace["messages"].([]any); ok {
				for _, rawMsg := range messages {
					if len(boundedMessages) >= 30 {
						break
					}
					message, ok := rawMsg.(map[string]any)
					if !ok {
						continue
					}
					boundedMessages = append(boundedMessages, map[string]any{
						"message_index": message["message_index"],
						"role":          excerpt(stringField(message, "role", ""), 80),
						"step_pointer":  excerpt(stringField(message, "step_pointer", ""), 120),
						"tool_calls":    boundNormalizedToolCalls(message["tool_calls"]),
					})
				}
			}
			boundedTraces = append(boundedTraces, map[string]any{
				"trace_id":    excerpt(stringField(trace, "trace_id", ""), 240),
				"member_role": excerpt(stringField(trace, "member_role", ""), 160),
				"messages":    boundedMessages,
			})
		}
	}
	return map[string]any{
		"case_id": excerpt(stringField(summary, "case_id", ""), 160),
		"traces":  boundedTraces,
	}
}

// boundNormalizedToolCalls 有界化规范化工具调用。Python: loader.py:623-640。
func boundNormalizedToolCalls(value any) []map[string]string {
	rawList, ok := value.([]any)
	if !ok {
		return []map[string]string{}
	}
	calls := []map[string]string{}
	for _, raw := range rawList {
		if len(calls) >= 8 {
			break
		}
		item, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		calls = append(calls, map[string]string{
			"name":         excerpt(stringField(item, "name", ""), 120),
			"input":        excerpt(stringField(item, "input", ""), 800),
			"output":       excerpt(stringField(item, "output", ""), 800),
			"error":        excerpt(stringField(item, "error", ""), 800),
			"step_pointer": excerpt(stringField(item, "step_pointer", ""), 120),
		})
	}
	return calls
}

// boundedStringList 列表前 limit 项转字符串列表。Python: loader.py:642-645 _string_list。
func boundedStringList(value any, limit int) []string {
	rawList, ok := value.([]any)
	if !ok {
		return []string{}
	}
	out := []string{}
	for _, item := range rawList {
		if len(out) >= limit {
			break
		}
		out = append(out, fmt.Sprintf("%v", item))
	}
	return out
}

// anyListToStringMapList 把 []any 中的 dict 项收集为 []map[string]any（非 dict 项丢弃）。
func anyListToStringMapList(values []any) []map[string]any {
	out := []map[string]any{}
	for _, raw := range values {
		if m, ok := raw.(map[string]any); ok {
			out = append(out, m)
		}
	}
	return out
}

// stringSliceField 取字符串切片字段（兼容 []any）。
func stringSliceField(data map[string]any, key string) []string {
	switch v := data[key].(type) {
	case []string:
		return v
	case []any:
		out := []string{}
		for _, item := range v {
			out = append(out, fmt.Sprintf("%v", item))
		}
		return out
	}
	return []string{}
}

// evidenceField 取证据字段（兼容 []map[string]any 与 []any）。
func evidenceField(data map[string]any, key string) []map[string]any {
	switch v := data[key].(type) {
	case []map[string]any:
		return v
	case []any:
		return anyListToStringMapList(v)
	}
	return []map[string]any{}
}

// boolField 取布尔字段（Python bool(item.get(...)) 语义）。
func boolField(data map[string]any, key string) bool {
	if v, ok := data[key].(bool); ok {
		return v
	}
	return false
}

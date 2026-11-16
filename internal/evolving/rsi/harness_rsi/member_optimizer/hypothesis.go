// hypothesis.go 不可变优化假设契约——把 analyzer 输出编译为绑定案例的合同。
// Python: member_optimizer/hypothesis.py（全量对齐，352 行）。
// 记录的病因保持为假设：下游反证据可请求重新分析，但不能悄悄改写本实验合同。

package memberoptimizer

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"

	harnessrsi "github.com/uapclaw/uapclaw-go/internal/evolving/rsi/harness_rsi"
)

// hypothesisVersion 假设文档版本。
// Python: hypothesis.py:14 _HYPOTHESIS_VERSION = 4。
const hypothesisVersion = 4

// ──────────────────────────── 导出函数 ────────────────────────────

// CompileOptimizationHypotheses 把 analyzer 输出编译为不可变、绑定案例的优化合同。
// Python: hypothesis.py:24-137 compile_optimization_hypotheses。
func CompileOptimizationHypotheses(analysisRefPath string, cases []map[string]any, outputPath string) (string, error) {
	// Python: analysis_path = Path(analysis_ref_path).expanduser().resolve()
	analysisPath, err := expandAndAbs(analysisRefPath)
	if err != nil {
		return "", err
	}
	// Python: analysis_ref = load_analysis_ref(analysis_path); analysis_ref.require_usable()
	analysisRef := LoadAnalysisRef(analysisPath)
	if err := analysisRef.RequireUsable(); err != nil {
		return "", err
	}
	// Python: case_inputs = {case_id: _public_case_input(case) for case in cases if case_id}
	caseInputs := map[string]string{}
	for _, c := range cases {
		caseID := pyStr(c["case_id"])
		if caseID != "" {
			caseInputs[caseID] = PublicCaseInput(c)
		}
	}
	// Python: hypotheses: list[dict] = []; for issue in resolve_team_issues(analysis_ref): ...
	hypotheses := []any{}
	for _, issue := range ResolveTeamIssues(analysisRef) {
		// Python: attribution = issue.metadata.get("attribution", {}) if isinstance(dict) else {}
		attribution := map[string]any{}
		if a, ok := issue.Metadata["attribution"].(map[string]any); ok {
			attribution = a
		}
		// Python: target_case_ids = sorted({affected_cases ∪ evidence case_ids})
		caseIDSet := map[string]bool{}
		for _, cid := range issue.AffectedCases {
			if cid != "" {
				caseIDSet[cid] = true
			}
		}
		for _, item := range issue.Evidence {
			if cid := pyStr(item["case_id"]); cid != "" {
				caseIDSet[cid] = true
			}
		}
		targetCaseIDs := make([]string, 0, len(caseIDSet))
		for cid := range caseIDSet {
			targetCaseIDs = append(targetCaseIDs, cid)
		}
		sort.Strings(targetCaseIDs)
		// Python: public_triggers = [{"case_id": cid, "task": case_inputs[cid]} for cid in target_case_ids if case_inputs.get(cid)]
		var publicTriggers []any
		for _, cid := range targetCaseIDs {
			if task, ok := caseInputs[cid]; ok && task != "" {
				publicTriggers = append(publicTriggers, map[string]any{"case_id": cid, "task": task})
			}
		}
		// Python: decisive_probe = _decisive_probe(issue)
		decisiveProbe := DecisiveProbe(issue)
		// Python: target_ref = str(attribution.get("target_ref", "") or "").strip()
		targetRef := strings.TrimSpace(pyStr(attribution["target_ref"]))
		// Python: decision_contract = _decision_contract(issue, attribution); _require_acceptance_observable(...)
		decisionContract := DecisionContract(issue, attribution)
		if err := RequireAcceptanceObservable(decisionContract, issue.IssueID); err != nil {
			return "", err
		}
		// Python: payload = {...}
		payload := map[string]any{
			"source_issue_id": issue.IssueID,
			"target_case_ids": pyStrList(targetCaseIDs),
			"authoritative_observations": map[string]any{
				"summary":  issue.Summary,
				"evidence": pyEvidenceListAny(issue.Evidence),
				"metadata": pyOrNone(issue.Metadata),
			},
			"required_behavior": pyStrOrEmpty(firstNonEmpty(pyStr(attribution["general_mechanism"]), issue.Recommendation)),
			"forbidden_behavior": DeduplicateStrings(
				StringList(firstNonEmptyAny(issue.Metadata["forbidden_behavior"], issue.Metadata["forbidden_behaviors"])),
			),
			"public_trigger":    pyOrNoneListAny(publicTriggers),
			"decisive_probe":    decisiveProbe,
			"decision_contract": decisionContract,
			"evidence_refs":     EvidenceRefs(issue.Evidence),
			"diagnostic_lens":   DiagnosticLens(issue),
			"intent":            "corrective",
			"deficiency_class":  deficiencyClass(targetRef),
			"lever_policy":      BuildHypothesisLeverPolicy(targetRef, targetCaseIDs, decisiveProbe),
		}
		// Python: digest = _payload_digest(payload); hypothesis_id / content_sha256
		digest := PayloadDigest(payload)
		payload["hypothesis_id"] = "hyp_" + digest[:pyMin(12, len(digest))]
		payload["content_sha256"] = digest
		hypotheses = append(hypotheses, payload)
	}
	// Python: document = {version, source_analysis_ref_path, hypotheses}
	document := map[string]any{
		"version":                  hypothesisVersion,
		"source_analysis_ref_path": analysisPath,
		"hypotheses":               hypotheses,
	}
	// Python: target.write_text(yaml.safe_dump(document, allow_unicode=True, sort_keys=False))
	target, err := expandAndAbs(outputPath)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return "", err
	}
	data, err := yaml.Marshal(document)
	if err != nil {
		return "", err
	}
	if err := os.WriteFile(target, data, 0o644); err != nil {
		return "", err
	}
	return target, nil
}

// LoadOptimizationHypotheses 载入假设并校验不可变负载未漂移。
// Python: hypothesis.py:140-163 load_optimization_hypotheses。
func LoadOptimizationHypotheses(path string) ([]map[string]any, error) {
	// Python: target = Path(path).expanduser().resolve(); if not is_file(): return []
	target, err := expandAndAbs(path)
	if err != nil {
		return nil, err
	}
	if info, statErr := os.Stat(target); statErr != nil || info.IsDir() {
		return []map[string]any{}, nil
	}
	// Python: payload = yaml.safe_load(...) or {}; records = payload.get("hypotheses", [])
	data, err := os.ReadFile(target)
	if err != nil {
		return nil, err
	}
	var payload map[string]any
	if err := yaml.Unmarshal(data, &payload); err != nil {
		return nil, err
	}
	if payload == nil {
		payload = map[string]any{}
	}
	// Python: for raw in records if isinstance(records, list) else []: ...
	records, _ := payload["hypotheses"].([]any)
	hypotheses := []map[string]any{}
	for _, record := range records {
		raw, ok := record.(map[string]any)
		if !ok {
			continue
		}
		// Python: item = dict(raw); expected = item.pop("content_sha256"); item.pop("hypothesis_id")
		item := deepCopyAny(raw).(map[string]any)
		expected := pyStr(item["content_sha256"])
		delete(item, "content_sha256")
		delete(item, "hypothesis_id")
		// Python: actual = _payload_digest(item)
		actual := PayloadDigest(item)
		// Python: if not expected or actual != expected: raise ValueError(...)
		if expected == "" || actual != expected {
			return nil, fmt.Errorf("optimization hypothesis content digest mismatch: %s",
				pyStrOrUnknown(raw["hypothesis_id"]))
		}
		// Python: _require_acceptance_observable(item.get("decision_contract"), item.get("source_issue_id", ""))
		if err := RequireAcceptanceObservable(item["decision_contract"], pyStr(item["source_issue_id"])); err != nil {
			return nil, err
		}
		// Python: hypotheses.append(dict(raw))
		hypotheses = append(hypotheses, raw)
	}
	return hypotheses, nil
}

// WriteCandidateManifest 写 optimizer 专属来源清单（不污染运行时资源）。
// Python: hypothesis.py:166-207 write_candidate_manifest。
func WriteCandidateManifest(outputPath string, sourceHarnessRefsPath string, hypothesesPath string, plan map[string]any) (string, error) {
	// Python: hypotheses = load_optimization_hypotheses(hypotheses_path)
	hypotheses, err := LoadOptimizationHypotheses(hypothesesPath)
	if err != nil {
		return "", err
	}
	// Python: selected_issue_ids = {issue_id for action in plan.actions for issue_id in action.attributed_issue_ids}
	selectedIssueIDs := map[string]bool{}
	actions, _ := plan["actions"].([]any)
	for _, action := range actions {
		if actionMap, ok := action.(map[string]any); ok {
			ids, _ := actionMap["attributed_issue_ids"].([]any)
			for _, id := range ids {
				if text := pyStr(id); text != "" {
					selectedIssueIDs[text] = true
				}
			}
		}
	}
	// Python: selected = [h for h in hypotheses if h["source_issue_id"] in selected_issue_ids]
	var hypothesisIDs []any
	for _, h := range hypotheses {
		if selectedIssueIDs[pyStr(h["source_issue_id"])] {
			hypothesisIDs = append(hypothesisIDs, h["hypothesis_id"])
		}
	}
	// Python: manifest = {version: 2, status: "planned", ..., actions: [...六字段...]}
	var manifestActions []any
	for _, action := range actions {
		actionMap, ok := action.(map[string]any)
		if !ok {
			continue
		}
		leverDecision := map[string]any{}
		if constraints, ok := actionMap["constraints"].(map[string]any); ok {
			if ld, ok := constraints["lever_decision"].(map[string]any); ok {
				leverDecision = ld
			}
		}
		manifestActions = append(manifestActions, map[string]any{
			"action_id":            pyStr(actionMap["action_id"]),
			"action_group":         pyStr(actionMap["action_group"]),
			"operation":            pyStr(actionMap["operation"]),
			"target_path":          pyStr(actionMap["target_path"]),
			"attributed_issue_ids": pyOrNone(actionMap["attributed_issue_ids"]),
			"lever_decision":       pyOrNone(leverDecision),
		})
	}
	sourceRefsPath, err := expandAndAbs(sourceHarnessRefsPath)
	if err != nil {
		return "", err
	}
	hypResolved, err := expandAndAbs(hypothesesPath)
	if err != nil {
		return "", err
	}
	manifest := map[string]any{
		"version":                      2,
		"status":                       "planned",
		"source_harness_refs_path":     sourceRefsPath,
		"optimization_hypotheses_path": hypResolved,
		"hypothesis_ids":               pyOrNoneListAny(hypothesisIDs),
		"actions":                      pyOrNoneListAny(manifestActions),
	}
	// Python: target.write_text(yaml.safe_dump(manifest, ...))
	target, err := expandAndAbs(outputPath)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return "", err
	}
	data, err := yaml.Marshal(manifest)
	if err != nil {
		return "", err
	}
	if err := os.WriteFile(target, data, 0o644); err != nil {
		return "", err
	}
	return target, nil
}

// PayloadDigest 规范化 JSON 摘要。
// Python: hypothesis.py:210-218 _payload_digest（json.dumps sort_keys separators=(",",":") + sha256）。
func PayloadDigest(payload map[string]any) string {
	// Python: canonical = json.dumps(payload, ensure_ascii=False, sort_keys=True, separators=(",", ":"))
	canonical := pyJSONDumpCompactSorted(payload)
	// Python: hashlib.sha256(canonical.encode("utf-8")).hexdigest()
	sum := sha256.Sum256([]byte(canonical))
	return hex.EncodeToString(sum[:])
}

// DiagnosticLens 诊断透镜（capability_gap / failure）。
// Python: hypothesis.py:220-227 _diagnostic_lens。
func DiagnosticLens(issue harnessrsi.TeamIssue) string {
	// Python: text = " ".join((category, summary)).lower()
	text := strings.ToLower(strings.TrimSpace(pyStr(issue.Category) + " " + pyStr(issue.Summary)))
	// Python: if "capability" in text or "missing" in text: return "capability_gap"
	if strings.Contains(text, "capability") || strings.Contains(text, "missing") {
		return "capability_gap"
	}
	return "failure"
}

// PublicCaseInput 提取案例公共输入（五键探测）。
// Python: hypothesis.py:229-241 _public_case_input。
func PublicCaseInput(c map[string]any) string {
	// Python: for key in ("input", "inputs", "task_input", "query", "prompt"): ...
	for _, key := range []string{"input", "inputs", "task_input", "query", "prompt"} {
		value, ok := c[key]
		if !ok {
			continue
		}
		// Python: if isinstance(dict) and set(value) == {"user_message"}: ...
		if m, isMap := value.(map[string]any); isMap && len(m) == 1 {
			if _, hasKey := m["user_message"]; hasKey {
				return StripBenchmarkTransport(pyStr(m["user_message"]))
			}
		}
		// Python: if isinstance(value, str): return _strip_benchmark_transport(value)
		if s, isStr := value.(string); isStr {
			return StripBenchmarkTransport(s)
		}
		// Python: return _strip_benchmark_transport(json.dumps(value, ensure_ascii=False, sort_keys=True))
		return StripBenchmarkTransport(pyJSONDumpSorted(value))
	}
	return ""
}

// StripBenchmarkTransport 剥离 harness 路由元数据、保留公共问题文本。
// Python: hypothesis.py:244-260 _strip_benchmark_transport。
func StripBenchmarkTransport(value string) string {
	// Python: transport_prefixes 元组
	transportPrefixes := []string{
		"execution environment:",
		"shell commands already run there.",
		"use `bash` for repository file inspection",
		"swe-bench lite instance:",
		"repository:",
		"base commit:",
		"work in the checked-out repository.",
		"diagnose the issue, implement the smallest correct fix",
		"do not modify tests to make them pass.",
	}
	// Python: kept = [line for line in value.splitlines() if not line.strip().lower().startswith(prefixes)]
	var kept []string
	for _, line := range splitLines(value) {
		lower := strings.ToLower(strings.TrimSpace(line))
		skip := false
		for _, prefix := range transportPrefixes {
			if strings.HasPrefix(lower, prefix) {
				skip = true
				break
			}
		}
		if !skip {
			kept = append(kept, line)
		}
	}
	// Python: return "\n".join(kept).strip()
	return strings.TrimSpace(strings.Join(kept, "\n"))
}

// StringList 宽松字符串列表（Python _string_list）。
func StringList(value any) []string {
	// Python: if isinstance(value, str): return [value] if value.strip() else []
	if s, ok := value.(string); ok {
		if strings.TrimSpace(s) != "" {
			return []string{s}
		}
		return []string{}
	}
	// Python: if not isinstance(value, list): return []
	list, ok := value.([]any)
	if !ok {
		return []string{}
	}
	// Python: return [str(item) for item in value if str(item).strip()]
	var out []string
	for _, item := range list {
		if text := pyStr(item); strings.TrimSpace(text) != "" {
			out = append(out, text)
		}
	}
	return out
}

// DeduplicateStrings 保序去重（Python _deduplicate_strings）。
func DeduplicateStrings(values []string) []any {
	// Python: return list(dict.fromkeys(value.strip() for value in values if value.strip()))
	seen := map[string]bool{}
	var out []any
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed != "" && !seen[trimmed] {
			seen[trimmed] = true
			out = append(out, trimmed)
		}
	}
	return out
}

// DecisiveProbe 决定性探针五字段。
// Python: hypothesis.py:268-283 _decisive_probe。
func DecisiveProbe(issue harnessrsi.TeamIssue) map[string]any {
	// Python: metadata = issue.metadata if isinstance(dict) else {}; attribution = metadata.get("attribution", {})
	metadata := issue.Metadata
	if metadata == nil {
		metadata = map[string]any{}
	}
	attribution := map[string]any{}
	if a, ok := metadata["attribution"].(map[string]any); ok {
		attribution = a
	}
	// Python: contract = _decision_contract(issue, attribution)（复用两次）
	contract := DecisionContract(issue, attribution)
	// Python: return {...七字段...}
	return map[string]any{
		"root_cause":              pyOrNone(attribution["root_cause"]),
		"critical_mistake":        pyOrNone(attribution["critical_mistake"]),
		"check":                   pyOrNone(contract["acceptance_observable"]),
		"boundary":                pyOrNone(contract["scope_boundary"]),
		"validation_observations": pyOrNone(metadata["validation_observations"]),
		"verifier_observations":   pyOrNone(metadata["verifier_observations"]),
	}
}

// RequireAcceptanceObservable 校验决策合同可执行性。
// Python: hypothesis.py:286-296 _require_acceptance_observable。
func RequireAcceptanceObservable(contract any, issueID string) error {
	// Python: observable = contract.get("acceptance_observable") if isinstance(dict) else None
	var observable any
	if m, ok := contract.(map[string]any); ok {
		observable = m["acceptance_observable"]
	}
	// Python: if not isinstance(observable, str) or not observable.strip(): raise AnalysisUnavailableError(...)
	text, isStr := observable.(string)
	if !isStr || strings.TrimSpace(text) == "" {
		return &AnalysisUnavailableError{
			Msg: fmt.Sprintf("Issue %q has an unexecutable decision_contract: missing non-empty "+
				"acceptance_observable. Rerun Analyzer to supply a task/verifier-grounded acceptance check; "+
				"the contract cannot enter candidate generation or acceptance. "+
				"Legacy contracts are not inferred from recommendations.", issueID),
		}
	}
	return nil
}

// DecisionContract 决策合同（保留 analyzer 的变更方向）。
// Python: hypothesis.py:299-349 _decision_contract。
func DecisionContract(issue harnessrsi.TeamIssue, attribution map[string]any) map[string]any {
	// Python: supplied = attribution.get("decision_contract", {}); supplied = dict(supplied) if isinstance(dict) else {}
	supplied := map[string]any{}
	if s, ok := attribution["decision_contract"].(map[string]any); ok {
		supplied = s
	}
	// Python: metadata = issue.metadata if isinstance(dict) else {}
	metadata := issue.Metadata
	if metadata == nil {
		metadata = map[string]any{}
	}
	// Python: boundaries = _deduplicate_strings([...scope_boundary, invalid_alternatives, forbidden_behavior(s)])
	boundaries := DeduplicateStrings(concatStrings(
		StringList(supplied["scope_boundary"]),
		StringList(supplied["invalid_alternatives"]),
		StringList(firstNonEmptyAny(metadata["forbidden_behavior"], metadata["forbidden_behaviors"])),
	))
	// Python: activation_phase 归一与推断
	activationPhase := strings.ToLower(strings.TrimSpace(pyStr(supplied["activation_phase"])))
	switch activationPhase {
	case "task_start", "during_investigation", "post_diagnosis", "pre_submission":
		// Python: if activation_phase not in {...}: 进入推断
	default:
		// Python: phase_text = " ".join((required_action, recommendation, acceptance_observable)).lower()
		phaseText := strings.ToLower(strings.Join([]string{
			pyStr(supplied["required_action"]), pyStr(issue.Recommendation), pyStr(supplied["acceptance_observable"]),
		}, " "))
		// Python: if any(token in phase_text for token in ("before submit", "pre-submit", "finalize")): pre_submission
		if strings.Contains(phaseText, "before submit") || strings.Contains(phaseText, "pre-submit") || strings.Contains(phaseText, "finalize") {
			activationPhase = "pre_submission"
		} else if strings.Contains(phaseText, "after diagnosis") || strings.Contains(phaseText, "once diagnosed") ||
			strings.Contains(phaseText, "after confirming") || strings.Contains(phaseText, "once confirmed") ||
			strings.Contains(phaseText, "edit site") {
			activationPhase = "post_diagnosis"
		} else {
			activationPhase = "task_start"
		}
	}
	// Python: return {...六字段...}
	return map[string]any{
		"wrong_decision": strings.TrimSpace(firstNonEmpty(
			pyStr(supplied["wrong_decision"]), pyStr(attribution["critical_mistake"]), issue.Summary, "")),
		"causal_distinction": strings.TrimSpace(firstNonEmpty(
			pyStr(supplied["causal_distinction"]), pyStr(attribution["general_mechanism"]), issue.Summary, "")),
		"required_action": strings.TrimSpace(firstNonEmpty(
			pyStr(supplied["required_action"]), issue.Recommendation, "")),
		"acceptance_observable": pyOrNone(supplied["acceptance_observable"]),
		"scope_boundary":        boundaries,
		"activation_phase":      activationPhase,
	}
}

// EvidenceRefs 证据引用四键过滤。
// Python: hypothesis.py:352-364 _evidence_refs。
func EvidenceRefs(evidence []map[string]any) []any {
	// Python: refs = []; for item in evidence: ref = {key: str(item[key]) for key in (...) if item.get(key) not in (None, "")}
	var refs []any
	for _, item := range evidence {
		ref := map[string]string{}
		for _, key := range []string{"case_id", "trace_id", "role", "step_pointer"} {
			if v, ok := item[key]; ok && v != nil && pyStr(v) != "" {
				ref[key] = pyStr(v)
			}
		}
		if len(ref) > 0 {
			refs = append(refs, ref)
		}
	}
	// Python: return refs（空列表保留 []）
	if refs == nil {
		return []any{}
	}
	return refs
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// deficiencyClass 缺陷分类（target_ref 非空且非 unassigned → harness_deficiency）。
// Python: payload["deficiency_class"] 内联三元。
func deficiencyClass(targetRef string) string {
	if targetRef != "" && targetRef != "unassigned" {
		return "harness_deficiency"
	}
	return "insufficient_evidence"
}

// pyStrOrUnknown 等价 Python raw.get("hypothesis_id", "<unknown>")。
func pyStrOrUnknown(value any) string {
	if value == nil {
		return "<unknown>"
	}
	return pyStr(value)
}

// pyJSONDumpCompactSorted 紧凑有序 JSON（Python json.dumps(sort_keys=True, separators=(",",":"))）。
func pyJSONDumpCompactSorted(value any) string {
	var sb strings.Builder
	pyJSONDumpCompactValue(&sb, value)
	return sb.String()
}

// pyJSONDumpCompactValue 递归输出（键排序 + 紧凑分隔符）。
func pyJSONDumpCompactValue(sb *strings.Builder, value any) {
	switch v := value.(type) {
	case nil:
		sb.WriteString("null")
	case bool:
		if v {
			sb.WriteString("true")
		} else {
			sb.WriteString("false")
		}
	case string:
		sb.WriteString(pyJSONQuote(v))
	case float64:
		sb.WriteString(pyJSONNumber(v))
	case int:
		sb.WriteString(strconv.Itoa(v))
	case int64:
		sb.WriteString(strconv.FormatInt(v, 10))
	case []any:
		sb.WriteByte('[')
		for i, item := range v {
			if i > 0 {
				sb.WriteByte(',')
			}
			pyJSONDumpCompactValue(sb, item)
		}
		sb.WriteByte(']')
	case []map[string]any:
		sb.WriteByte('[')
		for i, item := range v {
			if i > 0 {
				sb.WriteByte(',')
			}
			pyJSONDumpCompactValue(sb, item)
		}
		sb.WriteByte(']')
	case map[string]string:
		keys := make([]string, 0, len(v))
		for key := range v {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		sb.WriteByte('{')
		for i, key := range keys {
			if i > 0 {
				sb.WriteByte(',')
			}
			sb.WriteString(pyJSONQuote(key))
			sb.WriteByte(':')
			pyJSONDumpCompactValue(sb, v[key])
		}
		sb.WriteByte('}')
	case []string:
		sb.WriteByte('[')
		for i, item := range v {
			if i > 0 {
				sb.WriteByte(',')
			}
			pyJSONDumpCompactValue(sb, item)
		}
		sb.WriteByte(']')
	case map[string]any:
		keys := make([]string, 0, len(v))
		for key := range v {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		sb.WriteByte('{')
		for i, key := range keys {
			if i > 0 {
				sb.WriteByte(',')
			}
			sb.WriteString(pyJSONQuote(key))
			sb.WriteByte(':')
			pyJSONDumpCompactValue(sb, v[key])
		}
		sb.WriteByte('}')
	default:
		sb.WriteString(pyJSONQuote(fmt.Sprintf("%v", value)))
	}
}

// pyJSONDumpSorted 有序缩进风格 JSON（Python json.dumps(value, ensure_ascii=False, sort_keys=True)）。
func pyJSONDumpSorted(value any) string {
	// Python 默认分隔符 ", " ": "；复用紧凑输出后无空格差异不可接受——用 pyJSONDump 已有 ", "/": " 语义
	return pyJSONDump(value)
}

// pyEvidenceListAny []map[string]any → []any。
func pyEvidenceListAny(evidence []map[string]any) []any {
	out := make([]any, 0, len(evidence))
	for _, item := range evidence {
		out = append(out, item)
	}
	return out
}

// pyOrNoneListAny nil → []。
func pyOrNoneListAny(list []any) any {
	if list == nil {
		return []any{}
	}
	return list
}

// firstNonEmpty 首个非空字符串。
func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}

// firstNonEmptyAny 首个非 nil 值。
func firstNonEmptyAny(values ...any) any {
	for _, v := range values {
		if v != nil {
			return v
		}
	}
	return nil
}

// concatStrings 拼接字符串列表。
func concatStrings(lists ...[]string) []string {
	var out []string
	for _, list := range lists {
		out = append(out, list...)
	}
	return out
}

// expandAndAbs 等价 Python Path(path).expanduser().resolve()。
func expandAndAbs(path string) (string, error) {
	expanded, err := expandUser(path)
	if err != nil {
		return "", err
	}
	abs, err := filepath.Abs(expanded)
	if err != nil {
		return "", err
	}
	return abs, nil
}

// splitLines 等价 Python str.splitlines()（\n/\r\n/\r 均为行界）。
func splitLines(value string) []string {
	value = strings.ReplaceAll(value, "\r\n", "\n")
	value = strings.ReplaceAll(value, "\r", "\n")
	return strings.Split(value, "\n")
}

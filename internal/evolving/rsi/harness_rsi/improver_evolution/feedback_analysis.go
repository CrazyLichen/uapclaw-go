// Package improver_evolution 中 feedback_analysis 部分的确定性复盘分析：
// 校验并汇总一份或多份 Candidate Feedback Ledger v1 输入，产出候选反馈复盘报告。
//
// 对应 Python 代码：openjiuwen/rsi/harness_rsi/improver_evolution/feedback_analysis.py
package improver_evolution

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

// ──────────────────────────── 结构体 ────────────────────────────

// surfaceEvidence 单个触发面的触发/回归证据累积器
// （对应 Python class _SurfaceEvidence L234-289）
type surfaceEvidence struct {
	// triggerAvailability 触发可用性计数
	triggerAvailability map[string]int
	// triggerOpportunityCandidates 触发机会候选数（observed）
	triggerOpportunityCandidates int
	// triggerFailureCandidates 触发失败候选数（observed 且 not_triggered）
	triggerFailureCandidates int
	// triggerOpportunityCohorts 触发机会 cohort 集合
	triggerOpportunityCohorts map[string]bool
	// triggerFailureCohorts 触发失败 cohort 集合
	triggerFailureCohorts map[string]bool
	// regressionAvailability 回归可用性计数
	regressionAvailability map[string]int
	// regressionOpportunityCandidates 回归机会候选数（observed）
	regressionOpportunityCandidates int
	// regressionFailureCandidates 回归失败候选数（observed 且退步数>0）
	regressionFailureCandidates int
	// regressionOpportunityCohorts 回归机会 cohort 集合
	regressionOpportunityCohorts map[string]bool
	// regressionFailureCohorts 回归失败 cohort 集合
	regressionFailureCohorts map[string]bool
}

// newSurfaceEvidence 构造零值累积器
func newSurfaceEvidence() *surfaceEvidence {
	return &surfaceEvidence{
		triggerAvailability:          map[string]int{},
		triggerOpportunityCohorts:    map[string]bool{},
		triggerFailureCohorts:        map[string]bool{},
		regressionAvailability:       map[string]int{},
		regressionOpportunityCohorts: map[string]bool{},
		regressionFailureCohorts:     map[string]bool{},
	}
}

// observeTrigger 记录一次候选激活观察（对应 Python observe_trigger L247-256）
func (e *surfaceEvidence) observeTrigger(cohortID string, activation map[string]any) {
	availability, _ := activation["availability"].(string)
	e.triggerAvailability[availability]++
	if availability != "observed" {
		return
	}
	e.triggerOpportunityCandidates++
	e.triggerOpportunityCohorts[cohortID] = true
	if state, _ := activation["state"].(string); state == "not_triggered" {
		e.triggerFailureCandidates++
		e.triggerFailureCohorts[cohortID] = true
	}
}

// observeRegression 记录一次候选回归观察（对应 Python observe_regression L258-267）
func (e *surfaceEvidence) observeRegression(cohortID string, regression map[string]any) {
	availability, _ := regression["availability"].(string)
	e.regressionAvailability[availability]++
	if availability != "observed" {
		return
	}
	e.regressionOpportunityCandidates++
	e.regressionOpportunityCohorts[cohortID] = true
	count := 0
	if v, ok := regression["regressed_case_count"]; ok {
		if n, isInt := v.(int); isInt {
			count = n
		} else if n, ok := v.(int64); ok {
			count = int(n)
		} else if f, ok := v.(float64); ok {
			count = int(f)
		}
	}
	if count > 0 {
		e.regressionFailureCandidates++
		e.regressionFailureCohorts[cohortID] = true
	}
}

// triggerSummary 触发侧汇总（对应 Python trigger_summary L269-278）
func (e *surfaceEvidence) triggerSummary() map[string]any {
	return map[string]any{
		"availability_counts":                  counterDict(e.triggerAvailability),
		"observed_opportunity_candidate_count": e.triggerOpportunityCandidates,
		"failure_candidate_count":              e.triggerFailureCandidates,
		"rate":                                 rate(e.triggerFailureCandidates, e.triggerOpportunityCandidates),
		"opportunity_cohort_count":             len(e.triggerOpportunityCohorts),
		"support_cohort_count":                 len(e.triggerFailureCohorts),
		"evidence_cohort_ids":                  sortedSetKeys(e.triggerFailureCohorts),
	}
}

// regressionSummary 回归侧汇总（对应 Python regression_summary L280-289）
func (e *surfaceEvidence) regressionSummary() map[string]any {
	return map[string]any{
		"availability_counts":                  counterDict(e.regressionAvailability),
		"observed_opportunity_candidate_count": e.regressionOpportunityCandidates,
		"regression_candidate_count":           e.regressionFailureCandidates,
		"rate":                                 rate(e.regressionFailureCandidates, e.regressionOpportunityCandidates),
		"opportunity_cohort_count":             len(e.regressionOpportunityCohorts),
		"support_cohort_count":                 len(e.regressionFailureCohorts),
		"evidence_cohort_ids":                  sortedSetKeys(e.regressionFailureCohorts),
	}
}

// featureEvidence 单个排序特征的对齐证据累积器
// （对应 Python class _FeatureEvidence L292-342）
type featureEvidence struct {
	// comparablePairCount 可比较特征对数
	comparablePairCount int
	// comparableCohorts 可比较 cohort 集合
	comparableCohorts map[string]bool
	// alignedPairCount 预测与实际赢家一致的对数
	alignedPairCount int
	// alignedCohorts 一致 cohort 集合
	alignedCohorts map[string]bool
	// oppositePairCount 不一致的对数
	oppositePairCount int
	// oppositeCohorts 不一致 cohort 集合
	oppositeCohorts map[string]bool
	// increasePairCount 建议 increase 权重的对数
	increasePairCount int
	// increaseCohorts 建议 increase 的 cohort 集合
	increaseCohorts map[string]bool
	// decreasePairCount 建议 decrease 权重的对数
	decreasePairCount int
	// decreaseCohorts 建议 decrease 的 cohort 集合
	decreaseCohorts map[string]bool
}

// newFeatureEvidence 构造零值累积器
func newFeatureEvidence() *featureEvidence {
	return &featureEvidence{
		comparableCohorts: map[string]bool{},
		alignedCohorts:    map[string]bool{},
		oppositeCohorts:   map[string]bool{},
		increaseCohorts:   map[string]bool{},
		decreaseCohorts:   map[string]bool{},
	}
}

// observe 记录一次特征对观察（对应 Python observe L305-319）
func (e *featureEvidence) observe(cohortID string, aligned bool, adjustment string) {
	e.comparablePairCount++
	e.comparableCohorts[cohortID] = true
	if aligned {
		e.alignedPairCount++
		e.alignedCohorts[cohortID] = true
		return
	}
	e.oppositePairCount++
	e.oppositeCohorts[cohortID] = true
	switch adjustment {
	case "increase":
		e.increasePairCount++
		e.increaseCohorts[cohortID] = true
	case "decrease":
		e.decreasePairCount++
		e.decreaseCohorts[cohortID] = true
	}
}

// summary 特征证据汇总（对应 Python summary L321-342）
func (e *featureEvidence) summary() map[string]any {
	return map[string]any{
		"comparable_pair_count":   e.comparablePairCount,
		"comparable_cohort_count": len(e.comparableCohorts),
		"comparable_cohort_ids":   sortedSetKeys(e.comparableCohorts),
		"aligned_pair_count":      e.alignedPairCount,
		"aligned_cohort_count":    len(e.alignedCohorts),
		"aligned_cohort_ids":      sortedSetKeys(e.alignedCohorts),
		"opposite_pair_count":     e.oppositePairCount,
		"opposite_cohort_count":   len(e.oppositeCohorts),
		"opposite_cohort_ids":     sortedSetKeys(e.oppositeCohorts),
		"increase_weight_evidence": map[string]any{
			"pair_count":   e.increasePairCount,
			"cohort_count": len(e.increaseCohorts),
			"cohort_ids":   sortedSetKeys(e.increaseCohorts),
		},
		"decrease_weight_evidence": map[string]any{
			"pair_count":   e.decreasePairCount,
			"cohort_count": len(e.decreaseCohorts),
			"cohort_ids":   sortedSetKeys(e.decreaseCohorts),
		},
	}
}

// ──────────────────────────── 枚举 ────────────────────────────

// LedgerSource 账本输入源：内存 ledger/cohort 映射（map[string]any）或 YAML/JSON 文件路径（string）。
// 对应 Python TypeAlias `Mapping[str, Any] | str | Path`（偏差 D-e1-1：Go 无 Path 类型，路径即 string）。
type LedgerSource = any

// ──────────────────────────── 常量 ────────────────────────────

// _LEDGER_TYPE 账本级 record_type 字面量
const _LEDGER_TYPE = "single_harness_sibling_candidate_feedback"

// _COHORT_TYPE cohort 级 record_type 字面量
const _COHORT_TYPE = "candidate_feedback_cohort"

// _METRIC_NAMES 三个汇总指标名（顺序照抄 Python）
var _METRIC_NAMES = []string{"best_of_k_gain", "top_m_gain", "selection_regret"}

// _METRIC_STATUSES 指标状态合法取值
var _METRIC_STATUSES = map[string]bool{"available": true, "unavailable": true}

// _ACTIVATION_AVAILABILITIES 激活可用性合法取值
var _ACTIVATION_AVAILABILITIES = map[string]bool{
	"missing_artifact": true,
	"not_applicable":   true,
	"not_instrumented": true,
	"observed":         true,
}

// _ACTIVATION_STATES 激活状态合法取值
var _ACTIVATION_STATES = map[string]bool{
	"missing_artifact": true,
	"not_applicable":   true,
	"not_instrumented": true,
	"not_triggered":    true,
	"triggered":        true,
}

// _REGRESSION_AVAILABILITIES 回归可用性合法取值
var _REGRESSION_AVAILABILITIES = map[string]bool{
	"missing_artifact": true,
	"not_evaluated":    true,
	"not_instrumented": true,
	"observed":         true,
}

// ──────────────────────────── 导出函数 ────────────────────────────
// （AnalyzeCandidateFeedbackLedgers 于 digest 主函数阶段加入）

// ──────────────────────────── 非导出函数 ────────────────────────────

// normalizeSources 归一输入源列表（对应 Python _normalize_sources L345-353）。
// Go 入参已为序列形态，仅校验每个元素是映射或路径字符串。
func normalizeSources(sources []LedgerSource) ([]LedgerSource, error) {
	for _, source := range sources {
		switch source.(type) {
		case map[string]any, string:
		default:
			return nil, errNew("each source must be a ledger mapping or path")
		}
	}
	return sources, nil
}

// loadAndValidateSource 加载并校验单个输入源，返回其 cohort 列表
// （对应 Python _load_and_validate_source L356-371）。
func loadAndValidateSource(source LedgerSource, sourceIndex int) ([]map[string]any, error) {
	value, err := loadSource(source)
	if err != nil {
		return nil, err
	}
	location := fmt.Sprintf("source[%d]", sourceIndex)
	mapping, err := requireMapping(value, location)
	if err != nil {
		return nil, err
	}
	var rawCohorts []any
	if recordType, _ := mapping["record_type"].(string); recordType == _COHORT_TYPE {
		rawCohorts = []any{mapping}
	} else {
		if err := requireExact(mapping["schema_version"], 1, location+".schema_version"); err != nil {
			return nil, err
		}
		if err := requireExact(mapping["ledger_type"], _LEDGER_TYPE, location+".ledger_type"); err != nil {
			return nil, err
		}
		cohortsValue, present := mapping["cohorts"]
		if !present {
			cohortsValue = nil
		}
		rawList, ok := cohortsValue.([]any)
		if !ok {
			return nil, errNew(location + ".cohorts must be a list")
		}
		rawCohorts = rawList
	}
	cohorts := make([]map[string]any, 0, len(rawCohorts))
	for cohortIndex, cohort := range rawCohorts {
		validated, err := validateCohort(cohort, fmt.Sprintf("%s.cohorts[%d]", location, cohortIndex))
		if err != nil {
			return nil, err
		}
		cohorts = append(cohorts, validated)
	}
	return cohorts, nil
}

// loadSource 读取单个输入源（对应 Python _load_source L374-384）。
// 映射原样返回；字符串按文件路径读取，.json 后缀（大小写不敏感）按 JSON 解析，
// 其余按 YAML 解析。JSON 数字用 UseNumber 还原 Python 的 int/float 二分语义。
func loadSource(source LedgerSource) (any, error) {
	switch s := source.(type) {
	case map[string]any:
		// 对应 Python dict(source) 的浅拷贝：仅顶层新建，嵌套共享（本包只读不改，语义等价）
		shallow := make(map[string]any, len(s))
		for key, value := range s {
			shallow[key] = value
		}
		return shallow, nil
	case string:
		info, err := os.Stat(s)
		if err != nil || info.IsDir() {
			return nil, errNew(fmt.Sprintf("candidate feedback ledger does not exist: %s", s))
		}
		text, err := os.ReadFile(s)
		if err != nil {
			return nil, errNew(fmt.Sprintf("failed to read candidate feedback ledger %s: %s", s, err))
		}
		if strings.EqualFold(filepath.Ext(s), ".json") {
			decoded, err := decodeJSONWithInts(text)
			if err != nil {
				return nil, errNew(fmt.Sprintf("failed to read candidate feedback ledger %s: %s", s, err))
			}
			return decoded, nil
		}
		var decoded any
		if err := yamlUnmarshal(text, &decoded); err != nil {
			return nil, errNew(fmt.Sprintf("failed to read candidate feedback ledger %s: %s", s, err))
		}
		return decoded, nil
	default:
		return nil, errNew("sources must be a ledger mapping, path, or sequence of them")
	}
}

// decodeJSONWithInts 按 Python json.loads 的数字语义解码：无小数点/指数的整数还原为 int64，
// 其余为 float64（对应 json.loads 对 int/float 的区分）。
func decodeJSONWithInts(text []byte) (any, error) {
	decoder := json.NewDecoder(bytes.NewReader(text))
	decoder.UseNumber()
	var raw any
	if err := decoder.Decode(&raw); err != nil {
		return nil, err
	}
	return convertJSONNumbers(raw), nil
}

// convertJSONNumbers 递归将 json.Number 还原为 int64/float64
func convertJSONNumbers(value any) any {
	switch v := value.(type) {
	case map[string]any:
		for key, item := range v {
			v[key] = convertJSONNumbers(item)
		}
		return v
	case []any:
		for i, item := range v {
			v[i] = convertJSONNumbers(item)
		}
		return v
	case json.Number:
		text := v.String()
		if !strings.ContainsAny(text, ".eE") {
			if n, err := strconv.ParseInt(text, 10, 64); err == nil {
				return n
			}
		}
		f, err := strconv.ParseFloat(text, 64)
		if err != nil {
			return v
		}
		return f
	default:
		return value
	}
}

// yamlUnmarshal yaml.v3 解析（对应 Python yaml.safe_load 的安全语义）
func yamlUnmarshal(text []byte, out *any) error {
	return yaml.Unmarshal(text, out)
}

// validateCohort 校验单个 cohort 记录（对应 Python _validate_cohort L387-412）
func validateCohort(value any, location string) (map[string]any, error) {
	cohort, err := requireMapping(value, location)
	if err != nil {
		return nil, err
	}
	if err := requireExact(cohort["schema_version"], 1, location+".schema_version"); err != nil {
		return nil, err
	}
	if err := requireExact(cohort["record_type"], _COHORT_TYPE, location+".record_type"); err != nil {
		return nil, err
	}
	identity, err := requireMapping(mapValue(cohort, "cohort"), location+".cohort")
	if err != nil {
		return nil, err
	}
	if err := requireNonemptyText(identity["cohort_id"], location+".cohort.cohort_id"); err != nil {
		return nil, err
	}
	candidatesValue, present := cohort["candidates"]
	if !present {
		candidatesValue = nil
	}
	rawCandidates, ok := candidatesValue.([]any)
	if !ok {
		return nil, errNew(location + ".candidates must be a list")
	}
	// 对应 Python identity.get("candidate_count") != len(candidates)
	if !pyEqual(identity["candidate_count"], len(rawCandidates)) {
		return nil, errNew(location + ".cohort.candidate_count must match candidates length")
	}
	for candidateIndex, candidate := range rawCandidates {
		if _, err := validateCandidate(candidate, fmt.Sprintf("%s.candidates[%d]", location, candidateIndex)); err != nil {
			return nil, err
		}
	}
	metrics, err := requireMapping(mapValue(cohort, "metrics"), location+".metrics")
	if err != nil {
		return nil, err
	}
	for _, metricName := range _METRIC_NAMES {
		metric, err := requireMapping(mapValue(metrics, metricName), fmt.Sprintf("%s.metrics.%s", location, metricName))
		if err != nil {
			return nil, err
		}
		status, _ := metric["status"].(string)
		if !_METRIC_STATUSES[status] {
			return nil, errNew(fmt.Sprintf("%s.metrics.%s.status has unsupported value %s",
				location, metricName, pyRepr(metric["status"])))
		}
		if status == "available" {
			if !isNumber(metric["value"]) {
				return nil, errNew(location + fmt.Sprintf(".metrics.%s.value must be finite when available", metricName))
			}
		} else {
			valuePresent := metric["value"] != nil
			if valuePresent {
				return nil, errNew(location + fmt.Sprintf(".metrics.%s.value must be null when unavailable", metricName))
			}
			if err := requireNonemptyText(metric["reason"],
				fmt.Sprintf("%s.metrics.%s.reason", location, metricName)); err != nil {
				return nil, err
			}
		}
	}
	return cohort, nil
}

// validateCandidate 校验单个候选记录（对应 Python _validate_candidate L415-460）
func validateCandidate(value any, location string) (map[string]any, error) {
	candidate, err := requireMapping(value, location)
	if err != nil {
		return nil, err
	}
	if err := requireNonemptyText(candidate["candidate_id"], location+".candidate_id"); err != nil {
		return nil, err
	}
	if err := requireNonemptyText(candidate["candidate_fingerprint"], location+".candidate_fingerprint"); err != nil {
		return nil, err
	}
	identity, err := requireMapping(mapValue(candidate, "identity"), location+".identity")
	if err != nil {
		return nil, err
	}
	for _, field := range []string{"parent_harness_ref", "source_eval_ref", "evaluation_protocol"} {
		if err := requireNonemptyText(identity[field], location+".identity."+field); err != nil {
			return nil, err
		}
	}
	caseIDs, ok := identity["target_case_ids"].([]any)
	if !ok {
		return nil, errNew(location + ".identity.target_case_ids must be a list")
	}
	_ = caseIDs
	rank, err := requireMapping(mapValue(candidate, "proposal_rank"), location+".proposal_rank")
	if err != nil {
		return nil, err
	}
	if _, ok := rank["rank_frozen"].(bool); !ok {
		return nil, errNew(location + ".proposal_rank.rank_frozen must be a boolean")
	}
	if err := requireNonemptyText(rank["ranking_policy"], location+".proposal_rank.ranking_policy"); err != nil {
		return nil, err
	}
	// 对应 Python "ranking_features" in rank and not isinstance(dict)
	if features, present := rank["ranking_features"]; present {
		if _, ok := features.(map[string]any); !ok {
			return nil, errNew(location + ".proposal_rank.ranking_features must be a mapping when present")
		}
	}
	outcome, err := requireMapping(mapValue(candidate, "outcome"), location+".outcome")
	if err != nil {
		return nil, err
	}
	if gain := outcome["target_gain"]; gain != nil && !isNumber(gain) {
		return nil, errNew(location + ".outcome.target_gain must be finite or null")
	}

	activation, err := requireMapping(mapValue(candidate, "activation"), location+".activation")
	if err != nil {
		return nil, err
	}
	availability, _ := activation["availability"].(string)
	state, _ := activation["state"].(string)
	if !_ACTIVATION_AVAILABILITIES[availability] {
		return nil, errNew(fmt.Sprintf("%s.activation.availability has unsupported value %s",
			location, pyRepr(activation["availability"])))
	}
	if !_ACTIVATION_STATES[state] {
		return nil, errNew(fmt.Sprintf("%s.activation.state has unsupported value %s",
			location, pyRepr(activation["state"])))
	}
	if availability == "observed" && state != "triggered" && state != "not_triggered" {
		return nil, errNew(location + ".activation.state must be triggered or not_triggered when observed")
	}
	if availability != "observed" && state != availability {
		return nil, errNew(fmt.Sprintf("%s.activation.state must preserve unavailable state %s",
			location, pyRepr(availability)))
	}
	surfaces, ok := activation["surfaces"].([]any)
	if !ok {
		return nil, errNew(location + ".activation.surfaces must be a list of non-empty strings")
	}
	for _, surface := range surfaces {
		text, ok := surface.(string)
		if !ok || text == "" {
			return nil, errNew(location + ".activation.surfaces must be a list of non-empty strings")
		}
	}

	regression, err := requireMapping(mapValue(candidate, "regression"), location+".regression")
	if err != nil {
		return nil, err
	}
	target, err := requireMapping(mapValue(regression, "target"), location+".regression.target")
	if err != nil {
		return nil, err
	}
	targetAvailability, _ := target["availability"].(string)
	if !_REGRESSION_AVAILABILITIES[targetAvailability] {
		return nil, errNew(fmt.Sprintf("%s.regression.target.availability has unsupported value %s",
			location, pyRepr(target["availability"])))
	}
	count, hasCount := target["regressed_case_count"]
	if targetAvailability == "observed" {
		// 对应 Python isinstance(count, bool) or not isinstance(int) or count < 0
		if isBool(count) || !isInt(count) || pyInt(count) < 0 || !hasCount {
			return nil, errNew(location + ".regression.target.regressed_case_count must be non-negative")
		}
	} else if hasCount && count != nil {
		return nil, errNew(location + ".regression.target.regressed_case_count must be null when unavailable")
	}
	return candidate, nil
}

// activationStateValue 取激活 state 原值（含非字符串形态，供 pyRepr 展示）
func activationStateValue(activation map[string]any) any {
	return activation["state"]
}

// requireMapping 要求值必须是映射（对应 Python _require_mapping L783-786）
func requireMapping(value any, location string) (map[string]any, error) {
	mapping, ok := value.(map[string]any)
	if !ok {
		return nil, errNew(location + " must be a mapping")
	}
	return mapping, nil
}

// requireExact 要求值与期望严格相等（对应 Python _require_exact L789-791）。
// 数值跨类型相等复刻 Python `1 == 1.0` 语义。
func requireExact(value any, expected any, location string) error {
	if !pyEqual(value, expected) {
		return errNew(fmt.Sprintf("%s must be %s, got %s", location, pyRepr(expected), pyRepr(value)))
	}
	return nil
}

// requireNonemptyText 要求值为非空白字符串（对应 Python _require_nonempty_text L794-796）
func requireNonemptyText(value any, location string) error {
	text, ok := value.(string)
	if !ok || strings.TrimSpace(text) == "" {
		return errNew(location + " must be a non-empty string")
	}
	return nil
}

// isNumber 判断是否有限数且非布尔（对应 Python _is_number L799-800）
func isNumber(value any) bool {
	if isBool(value) {
		return false
	}
	switch v := value.(type) {
	case int:
		return true
	case int64:
		return true
	case float64:
		return !math.IsInf(v, 0) && !math.IsNaN(v)
	default:
		return false
	}
}

// isNonnegativeInt 判断是否非负整数且非布尔（对应 Python _is_nonnegative_int L803-804）
func isNonnegativeInt(value any) bool {
	if isBool(value) || !isInt(value) {
		return false
	}
	return pyInt(value) >= 0
}

// comparabilityReason 判定 cohort 不可比较的原因，可比较返回空串
// （对应 Python _comparability_reason L463-496，分支顺序照抄）
func comparabilityReason(cohort map[string]any) string {
	candidates := cohort["candidates"].([]any)
	if len(candidates) < 2 {
		return "requires_at_least_two_comparable_candidates"
	}
	identity := cohort["cohort"].(map[string]any)
	requiredFields := []string{"parent_harness_ref", "source_eval_ref", "evaluation_protocol"}
	for _, field := range requiredFields {
		if pyFalsy(identity[field]) {
			return "cohort_identity_missing"
		}
	}
	if pyFalsy(identity["target_case_ids"]) {
		return "cohort_identity_missing"
	}
	if identity["rank_frozen"] != true {
		return "rank_not_frozen_before_evaluation"
	}
	candidateIDs := make([]string, 0, len(candidates))
	for _, item := range candidates {
		candidateIDs = append(candidateIDs, item.(map[string]any)["candidate_id"].(string))
	}
	if len(candidateIDs) != len(setOf(candidateIDs)) {
		return "duplicate_candidate_id"
	}
	// 对应 Python ranks = [candidate["proposal_rank"].get("predicted_rank") for ...]
	ranks := make([]any, 0, len(candidates))
	for _, item := range candidates {
		ranks = append(ranks, item.(map[string]any)["proposal_rank"].(map[string]any)["predicted_rank"])
	}
	for _, r := range ranks {
		if isBool(r) || !isInt(r) {
			return "predicted_rank_missing"
		}
	}
	// 对应 Python set(ranks) != set(range(1, len(candidates)+1))
	expected := map[int64]bool{}
	for i := 1; i <= len(candidates); i++ {
		expected[int64(i)] = true
	}
	actual := map[int64]bool{}
	for _, r := range ranks {
		actual[pyInt(r)] = true
	}
	if len(actual) != len(expected) {
		return "predicted_ranks_not_permutation"
	}
	for k := range expected {
		if !actual[k] {
			return "predicted_ranks_not_permutation"
		}
	}
	for _, item := range candidates {
		candidate := item.(map[string]any)
		candidateIdentity := candidate["identity"].(map[string]any)
		for _, field := range requiredFields {
			if !pyEqual(candidateIdentity[field], identity[field]) {
				return field + "_mismatch"
			}
		}
		// 对应 Python set(candidate_target_case_ids) != set(identity_target_case_ids)
		if !stringSetEqual(candidateIdentity["target_case_ids"], identity["target_case_ids"]) {
			return "target_case_set_mismatch"
		}
		if candidate["proposal_rank"].(map[string]any)["rank_frozen"] != true {
			return "candidate_rank_not_frozen"
		}
		if !pyEqual(candidate["proposal_rank"].(map[string]any)["ranking_policy"], identity["ranking_policy"]) {
			return "candidate_ranking_policy_mismatch"
		}
		if !isNumber(candidate["outcome"].(map[string]any)["target_gain"]) {
			return "target_gain_missing"
		}
	}
	for _, name := range []string{"best_of_k_gain", "top_m_gain"} {
		metric := cohort["metrics"].(map[string]any)[name].(map[string]any)
		if metric["status"] != "available" {
			return "search_metric_unavailable"
		}
	}
	return ""
}

// pyFalsy 复刻 Python falsy 判定（None/空串/空列表/0）
func pyFalsy(value any) bool {
	switch v := value.(type) {
	case nil:
		return true
	case string:
		return v == ""
	case bool:
		return !v
	case int:
		return v == 0
	case int64:
		return v == 0
	case float64:
		return v == 0
	case []any:
		return len(v) == 0
	case map[string]any:
		return len(v) == 0
	default:
		return false
	}
}

// setOf 去重集合
func setOf(items []string) map[string]bool {
	out := map[string]bool{}
	for _, item := range items {
		out[item] = true
	}
	return out
}

// sortedSetKeys 集合排序为字符串切片（对应 Python sorted(set)）
func sortedSetKeys(set map[string]bool) []string {
	out := make([]string, 0, len(set))
	for key := range set {
		out = append(out, key)
	}
	sort.Strings(out)
	return out
}

// stringSetEqual 复刻 Python set(list) == set(list) 比较
func stringSetEqual(left, right any) bool {
	leftList, okL := left.([]any)
	rightList, okR := right.([]any)
	if !okL || !okR {
		return false
	}
	leftSet := map[string]bool{}
	for _, item := range leftList {
		leftSet[pyStr(item)] = true
	}
	rightSet := map[string]bool{}
	for _, item := range rightList {
		rightSet[pyStr(item)] = true
	}
	if len(leftSet) != len(rightSet) {
		return false
	}
	for key := range leftSet {
		if !rightSet[key] {
			return false
		}
	}
	return true
}

// pyStr 字符串化（对应 Python str()，仅用于集合键）
func pyStr(value any) string {
	if text, ok := value.(string); ok {
		return text
	}
	return fmt.Sprintf("%v", value)
}

// collectHighValueOutsideTopM 收集"高点候选在 Top-m 预算外"证据
// （对应 Python _collect_high_value_outside_top_m L499-520）
func collectHighValueOutsideTopM(cohort map[string]any, threshold float64, opportunityIDs, supportIDs *[]string) error {
	best := cohort["metrics"].(map[string]any)["best_of_k_gain"].(map[string]any)
	topM := cohort["metrics"].(map[string]any)["top_m_gain"].(map[string]any)
	if pyFloat(best["value"]) <= threshold {
		return nil
	}
	cohortID := cohort["cohort"].(map[string]any)["cohort_id"].(string)
	*opportunityIDs = append(*opportunityIDs, cohortID)
	candidateID := best["candidate_id"]
	var candidate map[string]any
	for _, item := range cohort["candidates"].([]any) {
		if pyEqual(item.(map[string]any)["candidate_id"], candidateID) {
			candidate = item.(map[string]any)
			break
		}
	}
	if candidate == nil {
		return errNew(fmt.Sprintf("cohort %s best_of_k_gain candidate_id is not in candidates", pyRepr(cohortID)))
	}
	effectiveM := topM["effective_m"]
	if isBool(effectiveM) || !isInt(effectiveM) || pyInt(effectiveM) < 1 {
		return errNew(fmt.Sprintf("cohort %s top_m_gain.effective_m must be a positive integer", pyRepr(cohortID)))
	}
	predictedRank := candidate["proposal_rank"].(map[string]any)["predicted_rank"]
	if pyInt(predictedRank) > pyInt(effectiveM) {
		*supportIDs = append(*supportIDs, cohortID)
	}
	return nil
}

// cohortDuplicateCounts 返回（候选指纹总数，重复指纹数）
// （对应 Python _cohort_duplicate_counts L523-525）
func cohortDuplicateCounts(cohort map[string]any) (int, int) {
	fingerprints := make([]string, 0)
	for _, item := range cohort["candidates"].([]any) {
		fingerprints = append(fingerprints, item.(map[string]any)["candidate_fingerprint"].(string))
	}
	return len(fingerprints), len(fingerprints) - len(setOf(fingerprints))
}

// collectFeatureEvidence 收集特征-结果对齐证据：候选两两组合，恰好一个数值特征不同
// 且预测/实际赢家不一致时计入（对应 Python _collect_feature_evidence L528-563）
func collectFeatureEvidence(cohort map[string]any, featureEvidence map[string]*featureEvidence) {
	cohortID := cohort["cohort"].(map[string]any)["cohort_id"].(string)
	candidates := cohort["candidates"].([]any)
	for i := 0; i < len(candidates); i++ {
		for j := i + 1; j < len(candidates); j++ {
			first := candidates[i].(map[string]any)
			second := candidates[j].(map[string]any)
			firstFeatures := numericRankingFeatures(first)
			secondFeatures := numericRankingFeatures(second)
			if len(firstFeatures) == 0 || !featureKeySetsEqual(firstFeatures, secondFeatures) {
				continue
			}
			differing := make([]string, 0)
			for feature, value := range firstFeatures {
				if value != secondFeatures[feature] {
					differing = append(differing, feature)
				}
			}
			sort.Strings(differing)
			if len(differing) != 1 {
				continue
			}
			feature := differing[0]
			firstGain := pyFloat(first["outcome"].(map[string]any)["target_gain"])
			secondGain := pyFloat(second["outcome"].(map[string]any)["target_gain"])
			firstScore := first["proposal_rank"].(map[string]any)["predicted_score"]
			secondScore := second["proposal_rank"].(map[string]any)["predicted_score"]
			if firstGain == secondGain || !isNumber(firstScore) || !isNumber(secondScore) {
				continue
			}
			if pyFloat(firstScore) == pyFloat(secondScore) {
				continue
			}
			predictedWinnerIsFirst := pyFloat(firstScore) > pyFloat(secondScore)
			realizedWinnerIsFirst := firstGain > secondGain
			aligned := predictedWinnerIsFirst == realizedWinnerIsFirst
			adjustment := ""
			if !aligned {
				higherFeatureIsFirst := firstFeatures[feature] > secondFeatures[feature]
				higherFeaturePredicted := predictedWinnerIsFirst == higherFeatureIsFirst
				if higherFeaturePredicted {
					adjustment = "decrease"
				} else {
					adjustment = "increase"
				}
			}
			evidence, present := featureEvidence[feature]
			if !present {
				evidence = newFeatureEvidence()
				featureEvidence[feature] = evidence
			}
			evidence.observe(cohortID, aligned, adjustment)
		}
	}
}

// featureKeySetsEqual 特征键集合相等（对应 Python first_features.keys() != second_features.keys()）
func featureKeySetsEqual(left, right map[string]float64) bool {
	if len(left) != len(right) {
		return false
	}
	for key := range left {
		if _, ok := right[key]; !ok {
			return false
		}
	}
	return true
}

// numericRankingFeatures 提取数值排序特征（对应 Python _numeric_ranking_features L566-570）
func numericRankingFeatures(candidate map[string]any) map[string]float64 {
	raw, ok := candidate["proposal_rank"].(map[string]any)["ranking_features"].(map[string]any)
	if !ok {
		return map[string]float64{}
	}
	out := map[string]float64{}
	for key, value := range raw {
		if isNumber(value) {
			out[key] = pyFloat(value)
		}
	}
	return out
}

// rate 计算比率，分母为 0 返回 nil（对应 Python _rate L757-758）
func rate(numerator, denominator int) any {
	if denominator == 0 {
		return nil
	}
	return float64(numerator) / float64(denominator)
}

// counterDict 计数 map 键排序输出（对应 Python _counter_dict L761-762）
func counterDict(counter map[string]int) map[string]int {
	out := make(map[string]int, len(counter))
	for key, value := range counter {
		out[key] = value
	}
	return out
}

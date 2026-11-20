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
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

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

// Package improver_evolution 提供 Harness improver 的策略层（版本化改进策略）。
//
// 本包是 17.4d 的前置件切片：Python improver_evolution/policy.py 全文件对齐。
// 17.4e 将补齐同目录的 feedback_analysis / meta_validation（见计划表 ⤴️ 登记）。
//
// 文件目录：
//
//	improver_evolution/
//	├── doc.go        # 包文档
//	└── policy.go    # FrozenDict / VersionedImproverPolicy / 策略读写与候选提议
//
// 对应 Python 代码：openjiuwen/rsi/harness_rsi/improver_evolution/policy.py
package improver_evolution

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"bytes"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// POLICY_SCHEMA_VERSION 策略文档 schema 版本（Python POLICY_SCHEMA_VERSION）。
const POLICY_SCHEMA_VERSION = 1

// STATIC_RANKING_POLICY 静态优先级打分策略名。
const STATIC_RANKING_POLICY = "static_priority_v1"

// RANKING_FEATURES 打分特征集合。
var RANKING_FEATURES = map[string]bool{"executable": true, "coverage": true, "atomicity": true, "duplicate": true}

// ──────────────────────────── 结构体 ────────────────────────────

// FrozenDict 递归不可变映射（Python FrozenDict；偏差 D-d1：Go 无 Mapping 协议，
// 以有序键表 + 只读查找表实现，构造时递归冻结值并按键排序）。
type FrozenDict struct {
	items  []frozenPair
	lookup map[string]any
}

// frozenPair 冻结映射的键值对。
type frozenPair struct {
	key   string
	value any
}

// NewFrozenDict 构造冻结映射（Python __init__）。
// value 为 nil 时等价空映射；非映射输入在 Go 侧由类型系统排除，仅保留键校验语义。
func NewFrozenDict(value map[string]any) (*FrozenDict, error) {
	source := value
	if source == nil {
		source = map[string]any{}
	}
	pairs := make([]frozenPair, 0, len(source))
	lookup := make(map[string]any, len(source))
	for key, item := range source {
		if key == "" {
			return nil, fmt.Errorf("policy mapping keys must be non-empty strings")
		}
		frozen, err := freezeJSONValue(item)
		if err != nil {
			return nil, err
		}
		pairs = append(pairs, frozenPair{key: key, value: frozen})
		lookup[key] = frozen
	}
	sort.Slice(pairs, func(i, j int) bool { return pairs[i].key < pairs[j].key })
	return &FrozenDict{items: pairs, lookup: lookup}, nil
}

// Get 取值（Python __getitem__；缺失返回 ok=false，映射缺失键的 KeyError 语义由调用方处理）。
func (f *FrozenDict) Get(key string) (any, bool) {
	v, ok := f.lookup[key]
	return v, ok
}

// MustGet 取值，缺失时 panic（对应 Python __getitem__ 的 KeyError 直抛语义）。
func (f *FrozenDict) MustGet(key string) any {
	v, ok := f.lookup[key]
	if !ok {
		panic(fmt.Sprintf("KeyError: %q", key))
	}
	return v
}

// Keys 按构造时的排序顺序返回全部键（Python __iter__）。
func (f *FrozenDict) Keys() []string {
	keys := make([]string, 0, len(f.items))
	for _, pair := range f.items {
		keys = append(keys, pair.key)
	}
	return keys
}

// Len 键数量（Python __len__）。
func (f *FrozenDict) Len() int { return len(f.items) }

// VersionedImproverPolicy Harness improver 使用的策略可序列化快照（Python VersionedImproverPolicy）。
// 偏差 D-d1：Python dataclass(frozen=True) 的 __post_init__ 校验在 Go 侧由构造函数承担。
type VersionedImproverPolicy struct {
	// VersionID 版本标识
	VersionID string
	// ParentVersionID 父版本标识（可为空）
	ParentVersionID *string
	// TrainingLedgerDigest 训练账本摘要
	TrainingLedgerDigest string
	// RankingPolicy 打分策略名
	RankingPolicy string
	// RankingWeights 打分权重
	RankingWeights *FrozenDict
	// GenerationDirectives 生成指令
	GenerationDirectives *FrozenDict
	// BudgetPolicy 预算策略
	BudgetPolicy *FrozenDict
	// EvidenceRefs 证据引用（构造时去空格、去重、排序）
	EvidenceRefs []string
}

// ──────────────────────────── 常量 ────────────────────────────

var (
	// defaultRankingWeights 默认打分权重（Python _DEFAULT_RANKING_WEIGHTS）。
	defaultRankingWeights = mustFrozenDict(map[string]any{
		"executable": 100.0,
		"coverage":   20.0,
		"atomicity":  5.0,
		"duplicate":  -30.0,
	})
	// defaultGenerationDirectives 默认生成指令（空映射，Python _DEFAULT_GENERATION_DIRECTIVES）。
	defaultGenerationDirectives = mustFrozenDict(nil)
	// defaultBudgetPolicy 默认预算策略（Python _DEFAULT_BUDGET_POLICY）。
	defaultBudgetPolicy = mustFrozenDict(map[string]any{"top_m": 1, "min_pattern_support": 2})
	// emptyLedgerDigest 空账本摘要（Python _EMPTY_LEDGER_DIGEST）。
	emptyLedgerDigest = "sha256:" + sha256Hex([]byte("[]"))
)

// ──────────────────────────── 全局变量 ────────────────────────────

// NewVersionedImproverPolicy 构造并校验策略（Python __init__ + __post_init__）。
func NewVersionedImproverPolicy(
	versionID string,
	parentVersionID *string,
	trainingLedgerDigest string,
	rankingPolicy string,
	rankingWeights *FrozenDict,
	generationDirectives *FrozenDict,
	budgetPolicy *FrozenDict,
	evidenceRefs []string,
) (*VersionedImproverPolicy, error) {
	if strings.TrimSpace(versionID) == "" {
		return nil, fmt.Errorf("version_id must be a non-empty string")
	}
	if parentVersionID != nil && strings.TrimSpace(*parentVersionID) == "" {
		return nil, fmt.Errorf("parent_version_id must be None or a non-empty string")
	}
	if strings.TrimSpace(trainingLedgerDigest) == "" {
		return nil, fmt.Errorf("training_ledger_digest must be a non-empty string")
	}
	if strings.TrimSpace(rankingPolicy) == "" {
		return nil, fmt.Errorf("ranking_policy must be a non-empty string")
	}

	if rankingWeights == nil {
		rankingWeights = defaultRankingWeights
	}
	if err := validateRankingWeights(rankingWeights); err != nil {
		return nil, err
	}
	if generationDirectives == nil {
		generationDirectives = defaultGenerationDirectives
	}
	if budgetPolicy == nil {
		budgetPolicy = defaultBudgetPolicy
	}
	if err := validateBudgetPolicy(budgetPolicy); err != nil {
		return nil, err
	}

	refs := make([]string, 0)
	seen := map[string]bool{}
	for _, ref := range evidenceRefs {
		trimmed := strings.TrimSpace(ref)
		if trimmed != "" && !seen[trimmed] {
			seen[trimmed] = true
			refs = append(refs, trimmed)
		}
	}
	sort.Strings(refs)

	return &VersionedImproverPolicy{
		VersionID:            versionID,
		ParentVersionID:      parentVersionID,
		TrainingLedgerDigest: trainingLedgerDigest,
		RankingPolicy:        rankingPolicy,
		RankingWeights:       rankingWeights,
		GenerationDirectives: generationDirectives,
		BudgetPolicy:         budgetPolicy,
		EvidenceRefs:         refs,
	}, nil
}

// ──────────────────────────── 导出函数 ────────────────────────────

// ToDict 返回写入 YAML 的纯数据表示（Python to_dict）。
func (p *VersionedImproverPolicy) ToDict() map[string]any {
	var parent any
	if p.ParentVersionID != nil {
		parent = *p.ParentVersionID
	}
	return map[string]any{
		"schema_version":         POLICY_SCHEMA_VERSION,
		"version_id":             p.VersionID,
		"parent_version_id":      parent,
		"training_ledger_digest": p.TrainingLedgerDigest,
		"ranking_policy":         p.RankingPolicy,
		"ranking_weights":        thawJSONValue(p.RankingWeights),
		"generation_directives":  thawJSONValue(p.GenerationDirectives),
		"budget_policy":          thawJSONValue(p.BudgetPolicy),
		"evidence_refs":          append([]string{}, p.EvidenceRefs...),
	}
}

// VersionedImproverPolicyFromDict 从纯解析数据校验并构造策略（Python from_dict）。
// value 须为 map[string]any（YAML/JSON 解码产物），否则报 TypeError 等价错误。
func VersionedImproverPolicyFromDict(value any) (*VersionedImproverPolicy, error) {
	mapping, ok := value.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("improver policy document must be a mapping")
	}
	schemaVersion := POLICY_SCHEMA_VERSION
	if raw, present := mapping["schema_version"]; present {
		schemaVersion = raw.(int)
	}
	if schemaVersion != POLICY_SCHEMA_VERSION {
		return nil, fmt.Errorf("unsupported improver policy schema_version: %v", schemaVersion)
	}
	rawRefs, present := mapping["evidence_refs"]
	if !present {
		rawRefs = []any{}
	}
	refs, err := anySliceToStrings(rawRefs)
	if err != nil {
		return nil, err
	}

	var parentVersionID *string
	if raw, present := mapping["parent_version_id"]; present && raw != nil {
		s := raw.(string)
		parentVersionID = &s
	}
	return NewVersionedImproverPolicy(
		stringField(mapping, "version_id"),
		parentVersionID,
		stringField(mapping, "training_ledger_digest"),
		stringField(mapping, "ranking_policy"),
		frozenField(mapping, "ranking_weights"),
		frozenField(mapping, "generation_directives"),
		frozenField(mapping, "budget_policy"),
		refs,
	)
}

// anySliceToStrings 序列字段提取：兼容 []any 与 []string。
func anySliceToStrings(raw any) ([]string, error) {
	switch v := raw.(type) {
	case []any:
		refs := make([]string, 0, len(v))
		for _, ref := range v {
			s, ok := ref.(string)
			if !ok {
				return nil, fmt.Errorf("evidence_refs must be a sequence")
			}
			refs = append(refs, s)
		}
		return refs, nil
	case []string:
		return v, nil
	}
	return nil, fmt.Errorf("evidence_refs must be a sequence")
}

// CanonicalDigest 完整序列化策略的稳定摘要（Python canonical_digest property）。
func (p *VersionedImproverPolicy) CanonicalDigest() string {
	return CanonicalPolicyDigest(p)
}

// DefaultImproverPolicy 返回等价 static_priority_v1 的不可变 I0 策略（Python default_improver_policy）。
func DefaultImproverPolicy() *VersionedImproverPolicy {
	policy, err := NewVersionedImproverPolicy("I0", nil, emptyLedgerDigest, STATIC_RANKING_POLICY, nil, nil, nil, nil)
	if err != nil {
		panic(err)
	}
	return policy
}

// CanonicalPolicyDigest 返回与映射插入顺序无关的稳定 SHA-256 摘要（Python canonical_policy_digest）。
func CanonicalPolicyDigest(policy any) string {
	var payload any
	switch v := policy.(type) {
	case *VersionedImproverPolicy:
		payload = v.ToDict()
	default:
		payload = plainJSONValue(policy)
	}
	return "sha256:" + sha256Hex([]byte(canonicalJSON(payload)))
}

// LoadImproverPolicy 从 YAML 加载并校验版本化策略（Python load_improver_policy）。
func LoadImproverPolicy(path string) (*VersionedImproverPolicy, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var payload any
	if err := yaml.Unmarshal(data, &payload); err != nil {
		return nil, err
	}
	mapping, ok := payload.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("improver policy YAML must contain a mapping")
	}
	return VersionedImproverPolicyFromDict(mapping)
}

// WriteImproverPolicy 以确定性 YAML 原子写入策略（Python write_improver_policy）。
func WriteImproverPolicy(path string, policy *VersionedImproverPolicy) (string, error) {
	if policy == nil {
		return "", fmt.Errorf("policy must be a VersionedImproverPolicy")
	}
	destination := filepath.Clean(path)
	if err := os.MkdirAll(filepath.Dir(destination), 0o755); err != nil {
		return "", err
	}
	data, err := yaml.Marshal(policy.ToDict())
	if err != nil {
		return "", err
	}
	temporary, err := os.CreateTemp(filepath.Dir(destination), "."+filepath.Base(destination)+".*.tmp")
	if err != nil {
		return "", err
	}
	temporaryPath := temporary.Name()
	if _, err := temporary.Write(data); err != nil {
		temporary.Close()
		os.Remove(temporaryPath)
		return "", err
	}
	if err := temporary.Sync(); err != nil {
		temporary.Close()
		os.Remove(temporaryPath)
		return "", err
	}
	if err := temporary.Close(); err != nil {
		os.Remove(temporaryPath)
		return "", err
	}
	if err := os.Rename(temporaryPath, destination); err != nil {
		os.Remove(temporaryPath)
		return "", err
	}
	return destination, nil
}

// ScoreStaticPriority 以策略权重为 static_priority_v1 四特征打分（Python score_static_priority）。
func ScoreStaticPriority(policy *VersionedImproverPolicy, features map[string]any) (float64, error) {
	if policy.RankingPolicy != STATIC_RANKING_POLICY {
		return 0, fmt.Errorf("unsupported ranking policy: %q", policy.RankingPolicy)
	}
	if err := validateRankingWeights(policy.RankingWeights); err != nil {
		return 0, err
	}
	var unknown, missing []string
	for name := range features {
		if !RANKING_FEATURES[name] {
			unknown = append(unknown, name)
		}
	}
	for name := range RANKING_FEATURES {
		if _, present := features[name]; !present {
			missing = append(missing, name)
		}
	}
	sort.Strings(unknown)
	sort.Strings(missing)
	if len(unknown) > 0 {
		return 0, fmt.Errorf("unknown ranking features: %v", unknown)
	}
	if len(missing) > 0 {
		return 0, fmt.Errorf("missing ranking features: %v", missing)
	}

	normalized := make(map[string]float64, len(features))
	for name, raw := range features {
		if b, isBool := raw.(bool); isBool {
			if b {
				normalized[name] = 1.0
			} else {
				normalized[name] = 0.0
			}
			continue
		}
		value := finiteNumber(raw)
		if value == nil {
			return 0, fmt.Errorf("ranking feature %q must be finite", name)
		}
		normalized[name] = *value
	}
	names := make([]string, 0, len(RANKING_FEATURES))
	for name := range RANKING_FEATURES {
		names = append(names, name)
	}
	sort.Strings(names)
	total := 0.0
	for _, name := range names {
		weight := finiteNumber(policy.RankingWeights.MustGet(name))
		total += *weight * normalized[name]
	}
	return total, nil
}

// ProposePolicyCandidates 每个受支持且具体的推荐生成一个不可变子候选（Python propose_policy_candidates）。
func ProposePolicyCandidates(parent *VersionedImproverPolicy, feedbackAnalysis map[string]any, minSupport *int) ([]*VersionedImproverPolicy, error) {
	if parent == nil {
		return nil, fmt.Errorf("parent must be a VersionedImproverPolicy")
	}
	if feedbackAnalysis == nil {
		return nil, fmt.Errorf("feedback_analysis must be a mapping")
	}
	threshold, err := minimumSupport(parent, minSupport)
	if err != nil {
		return nil, err
	}
	rawPatterns, present := feedbackAnalysis["stable_patterns"]
	if !present {
		rawPatterns = []any{}
	}
	patternList, ok := rawPatterns.([]any)
	if !ok {
		return nil, fmt.Errorf("feedback_analysis.stable_patterns must be a list")
	}
	ledgerDigest, err := feedbackLedgerDigest(feedbackAnalysis)
	if err != nil {
		return nil, err
	}

	type supportedEntry struct {
		sortSupport    float64
		sortRate       float64
		sortOpportunity float64
		patternID      string
		changeJSON     string
		pattern        map[string]any
		recommendation map[string]any
	}
	var supported []supportedEntry
	for _, raw := range patternList {
		pattern, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		support := patternMetric(pattern, "support_cohorts", "support")
		if support == nil || *support < float64(threshold) {
			continue
		}
		rawRecommendation, present := pattern["recommended_policy_change"]
		if !present {
			continue
		}
		recommendation, ok := rawRecommendation.(map[string]any)
		if !ok || !isConcreteChange(recommendation) {
			continue
		}
		rate := 0.0
		if r := finiteNumber(pattern["rate"]); r != nil {
			rate = *r
		}
		opportunity := 0.0
		if o := patternMetric(pattern, "opportunity_cohorts", "opportunity"); o != nil {
			opportunity = *o
		}
		patternID := ""
		if raw, present := pattern["pattern_id"]; present {
			patternID, _ = raw.(string)
		}
		supported = append(supported, supportedEntry{
			sortSupport:     -*support,
			sortRate:        -rate,
			sortOpportunity: -opportunity,
			patternID:       patternID,
			changeJSON:      canonicalJSON(plainJSONValue(recommendation)),
			pattern:         pattern,
			recommendation:  recommendation,
		})
	}
	sort.SliceStable(supported, func(i, j int) bool {
		a, b := supported[i], supported[j]
		if a.sortSupport != b.sortSupport {
			return a.sortSupport < b.sortSupport
		}
		if a.sortRate != b.sortRate {
			return a.sortRate < b.sortRate
		}
		if a.sortOpportunity != b.sortOpportunity {
			return a.sortOpportunity < b.sortOpportunity
		}
		if a.patternID != b.patternID {
			return a.patternID < b.patternID
		}
		return a.changeJSON < b.changeJSON
	})

	candidates := make([]*VersionedImproverPolicy, 0)
	seenChanges := map[string]bool{}
	for _, entry := range supported {
		if seenChanges[entry.changeJSON] {
			continue
		}
		seenChanges[entry.changeJSON] = true
		candidate, err := applyRecommendedChange(parent, entry.recommendation, entry.pattern, feedbackAnalysis, ledgerDigest)
		if err != nil {
			return nil, err
		}
		candidates = append(candidates, candidate)
	}
	return candidates, nil
}

// ProposePolicyUpdate 返回支持度最高的单改动候选（Python propose_policy_update）。
func ProposePolicyUpdate(parent *VersionedImproverPolicy, feedbackAnalysis map[string]any, minSupport *int) (*VersionedImproverPolicy, error) {
	candidates, err := ProposePolicyCandidates(parent, feedbackAnalysis, minSupport)
	if err != nil {
		return nil, err
	}
	if len(candidates) == 0 {
		return nil, nil
	}
	return candidates[0], nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// applyRecommendedChange 应用推荐改动生成子候选（Python _apply_recommended_change）。
func applyRecommendedChange(
	parent *VersionedImproverPolicy,
	change map[string]any,
	pattern map[string]any,
	feedbackAnalysis map[string]any,
	ledgerDigest string,
) (*VersionedImproverPolicy, error) {
	fieldName := strings.TrimSpace(change["field"].(string))
	operation := strings.ToLower(strings.TrimSpace(change["operation"].(string)))
	value := plainJSONValue(change["value"])
	rankingWeights, err := thawToMap(parent.RankingWeights)
	if err != nil {
		return nil, err
	}
	generationDirectives, err := thawToMap(parent.GenerationDirectives)
	if err != nil {
		return nil, err
	}
	budgetPolicy, err := thawToMap(parent.BudgetPolicy)
	if err != nil {
		return nil, err
	}

	parts := strings.Split(fieldName, ".")
	root, path := parts[0], parts[1:]
	var target map[string]any
	switch root {
	case "ranking_weights":
		target = rankingWeights
	case "generation_directives":
		target = generationDirectives
	case "budget_policy":
		target = budgetPolicy
	}
	if err := applyDeepChange(target, path, operation, value); err != nil {
		return nil, err
	}

	refs := map[string]bool{}
	for _, ref := range parent.EvidenceRefs {
		refs[ref] = true
	}
	rawAnalysisRefs, _ := feedbackAnalysis["evidence_refs"].([]any)
	for _, raw := range rawAnalysisRefs {
		if ref, ok := raw.(string); ok && strings.TrimSpace(ref) != "" {
			refs[strings.TrimSpace(ref)] = true
		}
	}
	rawPatternRefs, _ := pattern["evidence_cohort_ids"].([]any)
	for _, raw := range rawPatternRefs {
		if ref, ok := raw.(string); ok && strings.TrimSpace(ref) != "" {
			refs[strings.TrimSpace(ref)] = true
		}
	}
	if patternID, ok := pattern["pattern_id"].(string); ok && strings.TrimSpace(patternID) != "" {
		refs["pattern:"+strings.TrimSpace(patternID)] = true
	}
	refList := make([]string, 0, len(refs))
	for ref := range refs {
		refList = append(refList, ref)
	}
	sort.Strings(refList)

	versionPayload := map[string]any{
		"parent_version_id":      parent.VersionID,
		"training_ledger_digest": ledgerDigest,
		"change": map[string]any{
			"field":     fieldName,
			"operation": operation,
			"value":     value,
		},
		"ranking_policy":        parent.RankingPolicy,
		"ranking_weights":       rankingWeights,
		"generation_directives": generationDirectives,
		"budget_policy":         budgetPolicy,
		"evidence_refs":         refList,
	}
	versionDigest := sha256Hex([]byte(canonicalJSON(versionPayload)))
	return NewVersionedImproverPolicy(
		"I_"+versionDigest[:16],
		&parent.VersionID,
		ledgerDigest,
		parent.RankingPolicy,
		mustFrozenDict(rankingWeights),
		mustFrozenDict(generationDirectives),
		mustFrozenDict(budgetPolicy),
		refList,
	)
}

// isConcreteChange 判断推荐改动是否具体可执行（Python _is_concrete_change）。
func isConcreteChange(change map[string]any) bool {
	if len(change) != 4 {
		return false
	}
	for _, key := range []string{"field", "operation", "value", "rationale"} {
		if _, present := change[key]; !present {
			return false
		}
	}
	fieldName, _ := change["field"].(string)
	operation, _ := change["operation"].(string)
	rationale, _ := change["rationale"].(string)
	if strings.TrimSpace(fieldName) == "" || strings.TrimSpace(operation) == "" || strings.TrimSpace(rationale) == "" {
		return false
	}
	fieldName = strings.TrimSpace(fieldName)
	operation = strings.ToLower(strings.TrimSpace(operation))
	parts := strings.Split(fieldName, ".")
	if parts[0] == "ranking_weights" {
		finiteValue := finiteNumber(change["value"])
		return len(parts) == 2 &&
			RANKING_FEATURES[parts[1]] &&
			(operation == "set" || operation == "increase" || operation == "decrease") &&
			finiteValue != nil &&
			(operation == "set" || *finiteValue > 0.0)
	}
	if fieldName == "budget_policy.top_m" {
		value, isInt := change["value"].(int)
		if !isInt {
			return false
		}
		return operation == "increase" && value > 0
	}
	switch fieldName {
	case "generation_directives.require_unique_candidate_fingerprint",
		"generation_directives.require_distinct_intervention_surfaces",
		"generation_directives.preserve_partial_progress_and_target_residual":
		set, isBool := change["value"].(bool)
		return operation == "set" && isBool && set
	}
	if len(parts) == 3 {
		prefix := parts[0] + "." + parts[1]
		if prefix == "generation_directives.require_activation_evidence" || prefix == "generation_directives.avoid_target_regression" {
			set, isBool := change["value"].(bool)
			return parts[2] != "" && operation == "set" && isBool && set
		}
	}
	return false
}

// applyDeepChange 沿路径深改目标映射（Python _apply_deep_change）。
func applyDeepChange(target map[string]any, path []string, operation string, value any) error {
	if len(path) == 0 {
		return fmt.Errorf("recommended policy field must identify a leaf")
	}
	cursor := target
	for _, part := range path[:len(path)-1] {
		existing, present := cursor[part]
		if !present || existing == nil {
			existing = map[string]any{}
			cursor[part] = existing
		}
		nested, ok := existing.(map[string]any)
		if !ok {
			return fmt.Errorf("recommended policy path crosses non-mapping field: %q", part)
		}
		cursor = nested
	}
	leaf := path[len(path)-1]
	switch operation {
	case "set":
		cursor[leaf] = value
		return nil
	case "increase", "decrease":
		current, present := cursor[leaf]
		if !present {
			current = nil
		}
		currentNum, currentIsNum := numericValue(current)
		if currentIsNum == 0 {
			return fmt.Errorf("cannot %s non-numeric policy field: %q", operation, leaf)
		}
		valueNum, valueIsNum := numericValue(value)
		if valueIsNum == 0 {
			return fmt.Errorf("%s value must be numeric", operation)
		}
		var result float64
		if operation == "increase" {
			result = currentNum + valueNum
		} else {
			result = currentNum - valueNum
		}
		if math.IsInf(result, 0) || math.IsNaN(result) {
			return fmt.Errorf("%sd policy value must be finite", operation)
		}
		if currentIsNum == 1 && valueIsNum == 1 {
			cursor[leaf] = int(result)
		} else {
			cursor[leaf] = result
		}
		return nil
	}
	return fmt.Errorf("unsupported policy change operation: %q", operation)
}

// minimumSupport 解析最小支持度（Python _minimum_support）。
func minimumSupport(parent *VersionedImproverPolicy, explicit *int) (int, error) {
	var value int
	if explicit != nil {
		value = *explicit
	} else {
		raw, present := parent.BudgetPolicy.Get("min_pattern_support")
		if !present {
			value = 2
		} else {
			v, ok := raw.(int)
			if !ok {
				return 0, fmt.Errorf("min_support must be a positive integer")
			}
			value = v
		}
	}
	if value < 1 {
		return 0, fmt.Errorf("min_support must be a positive integer")
	}
	return value, nil
}

// feedbackLedgerDigest 提取反馈分析账本摘要（Python _feedback_ledger_digest）。
func feedbackLedgerDigest(feedbackAnalysis map[string]any) (string, error) {
	for _, key := range []string{"training_ledger_digest", "source_ledger_digest", "ledger_digest"} {
		if value, ok := feedbackAnalysis[key].(string); ok && strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value), nil
		}
	}
	return CanonicalPolicyDigest(plainJSONValue(feedbackAnalysis)), nil
}

// validateRankingWeights 校验打分权重（Python _validate_ranking_weights）。
func validateRankingWeights(weights *FrozenDict) error {
	var unknown, missing []string
	for _, name := range weights.Keys() {
		if !RANKING_FEATURES[name] {
			unknown = append(unknown, name)
		}
	}
	for name := range RANKING_FEATURES {
		if _, present := weights.Get(name); !present {
			missing = append(missing, name)
		}
	}
	sort.Strings(unknown)
	sort.Strings(missing)
	if len(unknown) > 0 {
		return fmt.Errorf("unknown ranking weights: %v", unknown)
	}
	if len(missing) > 0 {
		return fmt.Errorf("missing ranking weights: %v", missing)
	}
	for _, name := range weights.Keys() {
		value, _ := weights.Get(name)
		if finiteNumber(value) == nil {
			return fmt.Errorf("ranking weight %q must be finite", name)
		}
	}
	return nil
}

// validateBudgetPolicy 校验预算策略（Python _validate_budget_policy）。
func validateBudgetPolicy(policy *FrozenDict) error {
	required := []string{"top_m", "min_pattern_support"}
	var missing []string
	for _, name := range required {
		if _, present := policy.Get(name); !present {
			missing = append(missing, name)
		}
	}
	sort.Strings(missing)
	if len(missing) > 0 {
		return fmt.Errorf("missing budget policy fields: %v", missing)
	}
	for _, name := range required {
		value, _ := policy.Get(name)
		v, ok := value.(int)
		if !ok || v < 1 {
			return fmt.Errorf("budget policy %q must be a positive integer", name)
		}
	}
	return nil
}

// finiteNumber 有限数值提取（Python _finite_number）：bool 一律拒绝。
func finiteNumber(value any) *float64 {
	switch v := value.(type) {
	case bool:
		return nil
	case int:
		f := float64(v)
		return &f
	case int64:
		f := float64(v)
		return &f
	case float64:
		if math.IsInf(v, 0) || math.IsNaN(v) {
			return nil
		}
		return &v
	}
	return nil
}

// numericValue 数值归类：0 非数 / 1 整数 / 2 浮点（对齐 Python int/float 分支判断）。
func numericValue(value any) (float64, int) {
	switch v := value.(type) {
	case bool:
		return 0, 0
	case int:
		return float64(v), 1
	case int64:
		return float64(v), 1
	case float64:
		return v, 2
	}
	return 0, 0
}

// patternMetric 模式指标读取（Python _pattern_metric）：主键优先，缺省回退键。
func patternMetric(pattern map[string]any, primary, fallback string) *float64 {
	if _, present := pattern[primary]; present {
		return finiteNumber(pattern[primary])
	}
	return finiteNumber(pattern[fallback])
}

// freezeJSONValue 递归冻结策略值（Python _freeze_json_value）。
func freezeJSONValue(value any) (any, error) {
	switch v := value.(type) {
	case map[string]any:
		return NewFrozenDict(v)
	case []any:
		frozen := make([]any, 0, len(v))
		for _, item := range v {
			f, err := freezeJSONValue(item)
			if err != nil {
				return nil, err
			}
			frozen = append(frozen, f)
		}
		return frozen, nil
	case nil, string, bool, int, int64:
		return value, nil
	case float64:
		if !math.IsInf(v, 0) && !math.IsNaN(v) {
			return v, nil
		}
		return nil, fmt.Errorf("policy values must be finite")
	}
	return nil, fmt.Errorf("unsupported policy value type: %T", value)
}

// thawJSONValue 递归解冻为纯 JSON 值（Python _thaw_json_value）。
func thawJSONValue(value any) any {
	switch v := value.(type) {
	case *FrozenDict:
		if v == nil {
			return nil
		}
		plain := make(map[string]any, v.Len())
		for _, key := range v.Keys() {
			item, _ := v.Get(key)
			plain[key] = thawJSONValue(item)
		}
		return plain
	case []any:
		plain := make([]any, len(v))
		for i, item := range v {
			plain[i] = thawJSONValue(item)
		}
		return plain
	}
	return value
}

// thawToMap 解冻为顶层 map（applyRecommendedChange 使用）。
func thawToMap(frozen *FrozenDict) (map[string]any, error) {
	thawed, ok := thawJSONValue(frozen).(map[string]any)
	if !ok {
		return nil, fmt.Errorf("policy mapping fields must be mappings")
	}
	return thawed, nil
}

// plainJSONValue 冻结再解冻得到纯 JSON 值（Python _plain_json_value）。
func plainJSONValue(value any) any {
	frozen, err := freezeJSONValue(value)
	if err != nil {
		panic(err)
	}
	return thawJSONValue(frozen)
}

// canonicalJSON 规范 JSON 序列化：键排序、紧凑分隔、非 ASCII 不转义（Python _canonical_json）。
func canonicalJSON(value any) string {
	encoded, err := marshalSortedJSON(value)
	if err != nil {
		panic(err)
	}
	return encoded
}

// marshalSortedJSON 与 Python json.dumps(sort_keys=True, ensure_ascii=False, separators=(",",":")) 等价。
func marshalSortedJSON(value any) (string, error) {
	var buffer bytes.Buffer
	encoder := json.NewEncoder(&buffer)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(value); err != nil {
		return "", err
	}
	return strings.TrimSuffix(buffer.String(), "\n"), nil
}

// sha256Hex 计算 SHA-256 十六进制摘要。
func sha256Hex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// mustFrozenDict 构造冻结映射（失败 panic，仅用于包内已校验默认值）。
func mustFrozenDict(value map[string]any) *FrozenDict {
	frozen, err := NewFrozenDict(value)
	if err != nil {
		panic(err)
	}
	return frozen
}

// stringField 取字符串字段（缺失返回空串，对应 Python value.get(key, "")）。
func stringField(mapping map[string]any, key string) string {
	if raw, present := mapping[key]; present {
		s, _ := raw.(string)
		return s
	}
	return ""
}

// frozenField 取冻结映射字段（缺失返回 nil，由构造函数落默认值）。
func frozenField(mapping map[string]any, key string) *FrozenDict {
	if raw, present := mapping[key]; present {
		if m, ok := raw.(map[string]any); ok {
			return mustFrozenDict(m)
		}
	}
	return nil
}

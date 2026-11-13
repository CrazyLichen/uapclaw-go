// Package recommendation topic 选择层：效用/多样性评分与确定性列表搜索。
//
// 本文件一比一对齐 topic_selection.py（1203 行）：
// Utility/diversity scoring and deterministic list search for topic cards。
//
// 对应 Python 代码：jiuwenswarm/agents/harness/common/recommendation/topic_selection.py
package recommendation

import (
	"context"
	"fmt"
	"math"
	"sort"
	"strings"
)

// ──────────────────────────── 结构体 ────────────────────────────

// SelectionCandidate 宿主冻结投影，同时供评分器与列表搜索使用（对齐 :73）。
type SelectionCandidate struct {
	// CandidateID 候选 ID
	CandidateID string
	// ContextScope 上下文范围
	ContextScope string
	// Category 类别
	Category string
	// StableRank 稳定序
	StableRank int
	// EvidenceEligible 证据可用
	EvidenceEligible bool
	// FreshnessEligible 新鲜度可用
	FreshnessEligible bool
	// NegativePreferenceHit 负偏好命中
	NegativePreferenceHit bool
	// InterestSummaryID 兴趣摘要 ID（可空）
	InterestSummaryID string
	// IdentityAnchor 身份锚（可空）
	IdentityAnchor string
	// SourceMemoryUnitIDs 来源记忆单元 ID
	SourceMemoryUnitIDs []string
	// ScoringFeatures 评分特征
	ScoringFeatures map[string]any
}

// UtilityScore 单候选效用评分（对齐 :125）。
type UtilityScore struct {
	// CandidateID 候选 ID
	CandidateID string
	// ClickScoreNullable 点击分（可空）
	ClickScoreNullable *float64
	// DepthScoreNullable 深度分（可空）
	DepthScoreNullable *float64
	// CombinedUtilityNullable 综合效用（可空）
	CombinedUtilityNullable *float64
	// Admitted 是否准入
	Admitted bool
	// Reason 判定原因
	Reason string
	// GivenConditions 评分器读作固定的条件及其来源（仅审计）
	GivenConditions []map[string]string
}

// UtilityScoringResult 效用评分结果（对齐 :137）。
type UtilityScoringResult struct {
	// Scores 各候选评分
	Scores []UtilityScore
	// ScorerVersion 评分器版本
	ScorerVersion string
	// Receipt 组件小票
	Receipt *LearnedComponentReceipt
}

// PairwiseDiversityScore 成对多样性评分（对齐 :144）。
type PairwiseDiversityScore struct {
	// LeftCandidateID 左候选 ID
	LeftCandidateID string
	// RightCandidateID 右候选 ID
	RightCandidateID string
	// DiversityScoreNullable 多样性分（可空）
	DiversityScoreNullable *float64
	// Compatible 是否兼容
	Compatible bool
	// Reason 判定原因
	Reason string
}

// MarginalDiversityScore 边际多样性评分（对齐 :153）。
type MarginalDiversityScore struct {
	// CandidateID 候选 ID
	CandidateID string
	// MarginalDiversityNullable 边际多样性分（可空）
	MarginalDiversityNullable *float64
}

// DiversityScoringResult 多样性评分结果（对齐 :159）。
type DiversityScoringResult struct {
	// PairwiseScores 成对评分
	PairwiseScores []PairwiseDiversityScore
	// MarginalScores 边际评分
	MarginalScores []MarginalDiversityScore
	// ScorerVersion 评分器版本
	ScorerVersion string
	// Receipt 组件小票
	Receipt *LearnedComponentReceipt
}

// ListSearchConstraints 列表搜索约束（对齐 :167）。
type ListSearchConstraints struct {
	// ContextScope 上下文范围
	ContextScope string
	// BeamWidth 束宽
	BeamWidth int
	// MaxExpansions 最大扩展次数
	MaxExpansions int
	// MaxCandidates 最大候选数
	MaxCandidates int
	// UtilityWeight 效用权重
	UtilityWeight float64
	// DiversityWeight 多样性权重
	DiversityWeight float64
	// RequiredFirstWindowInterestSummaryIDs 首窗必需兴趣 ID（仅全局）
	RequiredFirstWindowInterestSummaryIDs []string
	// LeadingCards 全局新闻卡占位（排在被搜列表之前）
	LeadingCards int
}

// CandidateHostDecision 单候选宿主判定（对齐 :241）。
type CandidateHostDecision struct {
	// CandidateID 候选 ID
	CandidateID string
	// Selected 是否入选
	Selected bool
	// Reasons 判定原因
	Reasons []string
}

// BeamTraceEntry 束搜索轨迹条目（对齐 :248）。
type BeamTraceEntry struct {
	// TraceIndex 轨迹序号（从 1 起）
	TraceIndex int
	// CandidateIDs 候选 ID 列表
	CandidateIDs []string
	// Depth 深度
	Depth int
	// Status 状态
	Status string
	// RetainedInBeam 是否留在束内
	RetainedInBeam bool
	// HardConstraintsPass 硬约束通过
	HardConstraintsPass bool
	// UtilityPass 效用通过（未判定为 nil）
	UtilityPass *bool
	// DiversityPass 多样性通过（未判定为 nil）
	DiversityPass *bool
	// MeanUtilityNullable 平均效用（可空）
	MeanUtilityNullable *float64
	// MeanDiversityNullable 平均多样性（可空）
	MeanDiversityNullable *float64
	// MinimumPairwiseDiversityNullable 最小成对多样性（可空）
	MinimumPairwiseDiversityNullable *float64
	// ObjectiveNullable 目标值（可空）
	ObjectiveNullable *float64
	// FailureReasons 失败原因
	FailureReasons []string
}

// BoundedBeamTrace 有界束搜索轨迹（对齐 :265）。
type BoundedBeamTrace struct {
	// Entries 轨迹条目
	Entries []BeamTraceEntry
	// PreferredCandidateIDs 偏好候选 ID
	PreferredCandidateIDs []string
	// TargetSize 目标规模
	TargetSize int
	// ExpansionCount 扩展计数
	ExpansionCount int
	// BeamWidth 束宽
	BeamWidth int
	// MaxExpansions 最大扩展
	MaxExpansions int
	// UtilityScorerVersion 效用评分器版本
	UtilityScorerVersion string
	// DiversityScorerVersion 多样性评分器版本
	DiversityScorerVersion string
	// SearchVersion 搜索版本
	SearchVersion string
}

// ListSearchResult 列表搜索结果（对齐 :278）。
type ListSearchResult struct {
	// PreferredList 偏好列表
	PreferredList []string
	// Decisions 各候选判定
	Decisions []CandidateHostDecision
	// BoundedBeamTrace 束轨迹
	BoundedBeamTrace BoundedBeamTrace
	// FailureReason 失败原因（成功为空）
	FailureReason string
	// UtilityScorerVersion 效用评分器版本
	UtilityScorerVersion string
	// DiversityScorerVersion 多样性评分器版本
	DiversityScorerVersion string
	// UtilityReceipt 效用小票
	UtilityReceipt *LearnedComponentReceipt
	// DiversityReceipt 多样性小票（纯新闻卡时为 nil）
	DiversityReceipt *LearnedComponentReceipt
	// SearchVersion 搜索版本
	SearchVersion string
}

// AsManifestMetadata 清单元数据（对齐 :290-303）。
func (r *ListSearchResult) AsManifestMetadata() map[string]any {
	receipts := map[string]any{
		UTILITY_COMPONENT_ID: r.UtilityReceipt.AsPayload(),
	}
	if r.DiversityReceipt != nil {
		receipts[DIVERSITY_COMPONENT_ID] = r.DiversityReceipt.AsPayload()
	}
	return map[string]any{
		"utility_scorer_version":   r.UtilityScorerVersion,
		"diversity_scorer_version": r.DiversityScorerVersion,
		"list_search_version":      r.SearchVersion,
		"component_receipts":       receipts,
	}
}

// HierarchicalPreferencePair 层级偏好对（对齐 :307）。
type HierarchicalPreferencePair struct {
	// PairID 对 ID
	PairID string
	// PreferredCandidateIDs 正例候选 ID
	PreferredCandidateIDs []string
	// NegativeCandidateIDs 负例候选 ID
	NegativeCandidateIDs []string
	// NegativeType 负例类型
	NegativeType string
	// SourceTraceIndex 来源轨迹序号
	SourceTraceIndex int
	// FailureReasons 失败原因
	FailureReasons []string
	// UtilityScorerVersion 效用评分器版本
	UtilityScorerVersion string
	// DiversityScorerVersion 多样性评分器版本
	DiversityScorerVersion string
	// SearchVersion 搜索版本
	SearchVersion string
	// PairVersion 对版本
	PairVersion string
}

// AsPayload 序列化（对齐 :319-331）。
func (p *HierarchicalPreferencePair) AsPayload() map[string]any {
	return map[string]any{
		"pair_id":                p.PairID,
		"preferred_candidate_ids": copyStringSlice(p.PreferredCandidateIDs),
		"negative_candidate_ids":  copyStringSlice(p.NegativeCandidateIDs),
		"negative_type":           p.NegativeType,
		"source_trace_index":      p.SourceTraceIndex,
		"failure_reasons":         copyStringSlice(p.FailureReasons),
		"utility_scorer_version":  p.UtilityScorerVersion,
		"diversity_scorer_version": p.DiversityScorerVersion,
		"search_version":          p.SearchVersion,
		"pair_version":            p.PairVersion,
	}
}

// listMetrics 列表指标（对齐 :732 _ListMetrics）。
type listMetrics struct {
	meanUtility             *float64
	meanDiversity           *float64
	minimumPairwiseDiversity *float64
	pairwiseCount           int
	objective               *float64
}

// ──────────────────────────── 常量 ────────────────────────────

const (
	// UtilityComponentID 效用评分器组件 ID（对齐 :21）
	UTILITY_COMPONENT_ID = "interaction_utility_scorer"
	// DiversityComponentID 多样性评分器组件 ID（对齐 :30）
	DIVERSITY_COMPONENT_ID = "diversity_scorer"
	// ListSearchVersion 列表搜索版本（对齐 :31）
	LIST_SEARCH_VERSION = "topic-starter-v6-host-beam-v4"
	// PreferencePairVersion 偏好对版本（对齐 :32）
	PREFERENCE_PAIR_VERSION = "topic-starter-v6-preference-pair-v1"
	// FirstScreenCardTarget 首屏卡片数（对齐 :34）
	FIRST_SCREEN_CARD_TARGET = 4
	// GlobalPoolTargetSize 全局一轮准备的卡片数（对齐 :36）
	GLOBAL_POOL_TARGET_SIZE = 7
)

// ──────────────────────────── 全局变量 ────────────────────────────

// givenConditionSources 卡片固定条件的来源（对齐 :23-29 元组）。
var givenConditionSources = []string{
	"user",
	"assistant_estimate",
	"source_page",
	"stated_assumption",
	"unsupported",
}

// globalScope 全局范围常量（对齐 :39）。
const globalScope = "global_new_chat"

// selectionGlobalQuotas 全局配额（对齐 :40-45，偏好语义：只排序不裁剪）。
var selectionGlobalQuotas = map[string]int{
	"profile_recent_progress":      3,
	"adjacent_explanation":         2,
	"broad_timely_practical":       2,
	"profile_interest_continuation": GLOBAL_POOL_TARGET_SIZE,
}

// selectionProjectQuotas 项目配额（对齐 :46-49，上限语义）。
var selectionProjectQuotas = map[string]int{
	"project_continuation":    3,
	"project_domain_progress": 1,
}

// ──────────────────────────── 导出函数 ────────────────────────────

// NewSelectionCandidate 构造并校验（对齐 :88-112 __post_init__）。
func NewSelectionCandidate(candidateID, contextScope, category string, stableRank int,
	evidenceEligible, freshnessEligible, negativePreferenceHit bool,
	interestSummaryID, identityAnchor string, sourceMemoryUnitIDs []string,
	scoringFeatures map[string]any) (*SelectionCandidate, error) {
	c := &SelectionCandidate{
		CandidateID: candidateID, ContextScope: contextScope, Category: category,
		StableRank: stableRank, EvidenceEligible: evidenceEligible,
		FreshnessEligible: freshnessEligible, NegativePreferenceHit: negativePreferenceHit,
		InterestSummaryID: interestSummaryID, IdentityAnchor: identityAnchor,
		SourceMemoryUnitIDs: sourceMemoryUnitIDs, ScoringFeatures: scoringFeatures,
	}
	// 对齐 :89-93 身份/范围/类别必填
	if !selectionNonempty(c.CandidateID) || !selectionNonempty(c.ContextScope) || !selectionNonempty(c.Category) {
		return nil, fmt.Errorf("candidate identity, scope, and category are required")
	}
	// 对齐 :94-95
	if c.StableRank <= 0 {
		return nil, fmt.Errorf("stable_rank must be a positive integer")
	}
	// 对齐 :96-107 bool 由 Go 静态保证；features 必须是映射（nil 视为空 dict，对齐 :112 dict 拷贝）
	if c.ScoringFeatures == nil {
		c.ScoringFeatures = map[string]any{}
	}
	return c, nil
}

// AsScoringPayload 评分投影（对齐 :114-121）。
func (c *SelectionCandidate) AsScoringPayload() map[string]any {
	return map[string]any{
		"candidate_id":     c.CandidateID,
		"context_scope":    c.ContextScope,
		"category":         c.Category,
		"stable_rank":      c.StableRank,
		"scoring_features": copyAnyMap(c.ScoringFeatures),
	}
}

// NewListSearchConstraints 构造并校验（对齐 :178-215 __post_init__）。
func NewListSearchConstraints(contextScope string, requiredFirstWindowInterestSummaryIDs []string,
	leadingCards, beamWidth, maxExpansions, maxCandidates int,
	utilityWeight, diversityWeight float64) (*ListSearchConstraints, error) {
	c := &ListSearchConstraints{
		ContextScope:                          contextScope,
		BeamWidth:                             beamWidth,
		MaxExpansions:                         maxExpansions,
		MaxCandidates:                         maxCandidates,
		UtilityWeight:                         utilityWeight,
		DiversityWeight:                       diversityWeight,
		RequiredFirstWindowInterestSummaryIDs: requiredFirstWindowInterestSummaryIDs,
		LeadingCards:                          leadingCards,
	}
	// 对齐 :179-184 scope 校验（注意 Python 此处未 strip，仅判非空前缀）
	validScope := c.ContextScope == globalScope ||
		(strings.HasPrefix(c.ContextScope, "project:") && strings.TrimPrefix(c.ContextScope, "project:") != "")
	if !validScope {
		return nil, fmt.Errorf("invalid context_scope")
	}
	// 对齐 :185-193 必需兴趣仅限全局
	if len(c.RequiredFirstWindowInterestSummaryIDs) > 0 {
		if c.ContextScope != globalScope {
			return nil, fmt.Errorf("required interests must be a global ID tuple")
		}
		for _, id := range c.RequiredFirstWindowInterestSummaryIDs {
			if !selectionNonempty(id) {
				return nil, fmt.Errorf("required interests must be a global ID tuple")
			}
		}
	}
	// 对齐 :194-195 leading_cards 取值域
	allowedLeading := []int{0}
	if c.ContextScope == globalScope {
		allowedLeading = []int{0, 1}
	}
	leadingAllowed := false
	for _, v := range allowedLeading {
		if c.LeadingCards == v {
			leadingAllowed = true
		}
	}
	if !leadingAllowed {
		return nil, fmt.Errorf("only a global list has a leading card, and at most one")
	}
	// 对齐 :196-205 正整数界
	for _, v := range []int{c.BeamWidth, c.MaxExpansions, c.MaxCandidates} {
		if v <= 0 {
			return nil, fmt.Errorf("search bounds must be positive integers")
		}
	}
	// 对齐 :206-215 有限非负权重
	for _, w := range []float64{c.UtilityWeight, c.DiversityWeight} {
		if math.IsNaN(w) || math.IsInf(w, 0) {
			return nil, fmt.Errorf("must be finite")
		}
		if w < 0 {
			return nil, fmt.Errorf("search weights must be non-negative")
		}
	}
	return c, nil
}

// Quotas 配额表（对齐 :217-221 property quotas）。
func (c *ListSearchConstraints) Quotas() map[string]int {
	if c.ContextScope == globalScope {
		return copyIntMap(selectionGlobalQuotas)
	}
	return copyIntMap(selectionProjectQuotas)
}

// TargetSize 目标规模（对齐 :223-231 D10 注释）。
func (c *ListSearchConstraints) TargetSize() int {
	if c.ContextScope == globalScope {
		return GLOBAL_POOL_TARGET_SIZE - c.LeadingCards
	}
	total := 0
	for _, v := range c.Quotas() {
		total += v
	}
	return total
}

// FirstWindowSize 首窗被搜卡片数（对齐 :233-237）。
func (c *ListSearchConstraints) FirstWindowSize() int {
	return FIRST_SCREEN_CARD_TARGET - c.LeadingCards
}

// ScoreInvoker 评分器调用形态（对齐 :334）。
type ScoreInvoker = func(ctx context.Context, payload map[string]any) (any, error)

// ScoreInteractionUtility 调用一个可替换效用评分器并校验精确 ID 覆盖（对齐 :350-377）。
func ScoreInteractionUtility(userState map[string]any, candidates []*SelectionCandidate,
	invokeScorer ScoreInvoker, receipt *LearnedComponentReceipt) (*UtilityScoringResult, error) {
	if err := requireSuccessfulReceipt(receipt, UTILITY_COMPONENT_ID); err != nil {
		return nil, err
	}
	candidateIDs := make([]string, 0, len(candidates))
	for _, candidate := range candidates {
		candidateIDs = append(candidateIDs, candidate.CandidateID)
	}
	if hasStringDuplicates(candidateIDs) {
		return nil, fmt.Errorf("candidate IDs must be unique")
	}
	scoringPayloads := make([]map[string]any, 0, len(candidates))
	for _, candidate := range candidates {
		scoringPayloads = append(scoringPayloads, candidate.AsScoringPayload())
	}
	raw, err := invokeScorer(context.Background(), map[string]any{
		"input_snapshot_id": receipt.InputSnapshotID,
		"request_id":        receipt.RequestID,
		"user_state":        copyAnyMap(userState),
		"candidates":        scoringPayloads,
	})
	if err != nil {
		return nil, err
	}
	// 对齐 :371-377 provider_validation 包裹
	byID, validationErr := utilityScoresByID(raw, candidateIDs)
	if validationErr != nil {
		recordProviderValidationOn(invokeScorer, raw, "business_validation_failed")
		return nil, validationErr
	}
	recordProviderValidationOn(invokeScorer, raw, "business_validated")
	scores := make([]UtilityScore, 0, len(candidateIDs))
	for _, candidateID := range candidateIDs {
		scores = append(scores, byID[candidateID])
	}
	return &UtilityScoringResult{
		Scores:        scores,
		ScorerVersion: receipt.Binding.ConfigVersion,
		Receipt:       receipt,
	}, nil
}

// UtilityScoresByID 校验一份恰好覆盖这些候选各一次的效用响应（对齐 :380-444）。
func UtilityScoresByID(raw any, candidateIDs []string) (map[string]UtilityScore, error) {
	return utilityScoresByID(raw, candidateIDs)
}

// ScoreDiversity 调用多样性评分器并要求成对/边际全覆盖（对齐 :463-571）。
func ScoreDiversity(candidates []*SelectionCandidate, invokeScorer ScoreInvoker,
	receipt *LearnedComponentReceipt) (*DiversityScoringResult, error) {
	if err := requireSuccessfulReceipt(receipt, DIVERSITY_COMPONENT_ID); err != nil {
		return nil, err
	}
	candidateIDs := make([]string, 0, len(candidates))
	for _, candidate := range candidates {
		candidateIDs = append(candidateIDs, candidate.CandidateID)
	}
	if hasStringDuplicates(candidateIDs) {
		return nil, fmt.Errorf("candidate IDs must be unique")
	}
	scoringPayloads := make([]map[string]any, 0, len(candidates))
	for _, candidate := range candidates {
		scoringPayloads = append(scoringPayloads, candidate.AsScoringPayload())
	}
	raw, err := invokeScorer(context.Background(), map[string]any{
		"input_snapshot_id": receipt.InputSnapshotID,
		"request_id":        receipt.RequestID,
		"candidates":        scoringPayloads,
	})
	if err != nil {
		return nil, err
	}
	result, validationErr := scoreDiversityValidate(raw, candidateIDs, receipt)
	if validationErr != nil {
		recordProviderValidationOn(invokeScorer, raw, "business_validation_failed")
		return nil, validationErr
	}
	recordProviderValidationOn(invokeScorer, raw, "business_validated")
	return result, nil
}

// NewsOnlySearchResult 全局池仅剩新闻卡时的列表搜索（对齐 :827-859）。
func NewsOnlySearchResult(utility *UtilityScoringResult, contextScope string) (*ListSearchResult, error) {
	constraints, err := NewListSearchConstraints(contextScope, nil, 1, 64, 4096, 64, 1.0, 1.0)
	if err != nil {
		return nil, err
	}
	trace := BoundedBeamTrace{
		Entries:                []BeamTraceEntry{},
		PreferredCandidateIDs:  []string{},
		TargetSize:             constraints.TargetSize(),
		ExpansionCount:         0,
		BeamWidth:              constraints.BeamWidth,
		MaxExpansions:          constraints.MaxExpansions,
		UtilityScorerVersion:   utility.ScorerVersion,
		DiversityScorerVersion: "",
		SearchVersion:          LIST_SEARCH_VERSION,
	}
	return &ListSearchResult{
		PreferredList:          []string{},
		Decisions:              []CandidateHostDecision{},
		BoundedBeamTrace:       trace,
		FailureReason:          "",
		UtilityScorerVersion:   utility.ScorerVersion,
		DiversityScorerVersion: "",
		UtilityReceipt:         utility.Receipt,
		DiversityReceipt:       nil,
		SearchVersion:          LIST_SEARCH_VERSION,
	}, nil
}

// HybridListSearch 在类别配额总规模内选一条确定性列表（对齐 :862-1148）。
// 全局配额是偏好：只对等长列表排序，不因超出单一类别拒绝；项目配额保持上限，
// 且项目列表至少需要一条续聊。优先选择通过全部宿主/效用/多样性约束的最大列表。
func HybridListSearch(candidates []*SelectionCandidate, utility *UtilityScoringResult,
	diversity *DiversityScoringResult, constraints *ListSearchConstraints) (*ListSearchResult, error) {
	// 对齐 :878-882 规模与唯一性
	if len(candidates) > constraints.MaxCandidates {
		return nil, fmt.Errorf("candidate set exceeds max_candidates")
	}
	candidateIDs := make([]string, 0, len(candidates))
	for _, candidate := range candidates {
		candidateIDs = append(candidateIDs, candidate.CandidateID)
	}
	if hasStringDuplicates(candidateIDs) {
		return nil, fmt.Errorf("candidate IDs must be unique")
	}

	// 对齐 :884-905 排序与硬过滤
	ordered := append([]*SelectionCandidate{}, candidates...)
	sort.SliceStable(ordered, func(i, j int) bool {
		return candidateOrderLess(ordered[i], ordered[j])
	})
	hardReasonsByID := map[string][]string{}
	for _, candidate := range ordered {
		hardReasonsByID[candidate.CandidateID] = candidateHardReasons(candidate, constraints)
	}
	utilityDecisions := map[string]bool{}
	for _, score := range utility.Scores {
		utilityDecisions[score.CandidateID] = score.Admitted
	}
	for _, candidate := range ordered {
		if admitted, present := utilityDecisions[candidate.CandidateID]; present && !admitted {
			hardReasonsByID[candidate.CandidateID] = append(hardReasonsByID[candidate.CandidateID], "utility_rejected")
		}
	}
	incompatiblePairs := map[selectionPairKey]struct{}{}
	for i := range diversity.PairwiseScores {
		score := &diversity.PairwiseScores[i]
		if !score.Compatible {
			incompatiblePairs[pairKey(score.LeftCandidateID, score.RightCandidateID)] = struct{}{}
		}
	}
	eligible := []*SelectionCandidate{}
	for _, candidate := range ordered {
		if len(hardReasonsByID[candidate.CandidateID]) == 0 {
			eligible = append(eligible, candidate)
		}
	}
	eligibleIDs := map[string]struct{}{}
	for _, candidate := range eligible {
		eligibleIDs[candidate.CandidateID] = struct{}{}
	}
	utilityByID, pairwiseByKey, marginalByID, err := scoreMaps(ordered, eligibleIDs, utility, diversity)
	if err != nil {
		return nil, err
	}

	// 对齐 :907-926 硬过滤轨迹行
	traceRows := []selectionTraceRow{}
	for _, candidate := range ordered {
		reasons := hardReasonsByID[candidate.CandidateID]
		if len(reasons) > 0 {
			traceRows = append(traceRows, selectionTraceRow{
				candidateIDs:        []string{candidate.CandidateID},
				depth:               1,
				hardConstraintsPass: false,
				utilityPass:         nil,
				diversityPass:       nil,
				meanUtility:         nil,
				meanDiversity:       nil,
				minimumPairwise:     nil,
				objective:           nil,
				failureReasons:      reasons,
			})
		}
	}

	// 对齐 :928-946 必需兴趣不可用 → 空结果
	requiredInterests := constraints.RequiredFirstWindowInterestSummaryIDs
	if len(requiredInterests) > 0 {
		found := false
		for _, candidate := range eligible {
			if stringInSlice(candidate.InterestSummaryID, requiredInterests) {
				found = true
				break
			}
		}
		if !found {
			entries := buildTraceEntries(traceRows)
			return emptySearchResult(ordered, hardReasonsByID, entries, 0,
				"required_first_window_interest_unavailable", utility, diversity, constraints), nil
		}
	}

	// 对齐 :948-1068 束搜索主循环
	quotas := constraints.Quotas()
	targetSize := constraints.TargetSize()
	beam := [][]int{{}}
	passingByDepth := map[int][]selectionPassingItem{}
	expansionCount := 0
	budgetExhausted := false
	for depth := 1; depth <= targetSize; depth++ {
		attempts := []selectionAttemptRow{}
		feasible := []selectionPassingItem{}
		for _, positions := range beam {
			start := 0
			if len(positions) > 0 {
				start = positions[len(positions)-1] + 1
			}
			for candidatePosition := start; candidatePosition < len(eligible); candidatePosition++ {
				if expansionCount >= constraints.MaxExpansions {
					budgetExhausted = true
					break
				}
				expansionCount++
				expanded := append(append([]int{}, positions...), candidatePosition)
				selected := make([]*SelectionCandidate, 0, len(expanded))
				for _, index := range expanded {
					selected = append(selected, eligible[index])
				}
				selectedIDs := make([]string, 0, len(selected))
				for _, s := range selected {
					selectedIDs = append(selectedIDs, s.CandidateID)
				}
				remaining := eligible[candidatePosition+1:]
				hardReasons := partialHardReasons(selected, remaining, quotas,
					constraints.ContextScope == globalScope,
					constraints.RequiredFirstWindowInterestSummaryIDs,
					constraints.FirstWindowSize())
				// 对齐 :976-980 不兼容对
				for _, pair := range combinations2(selectedIDs) {
					if _, bad := incompatiblePairs[pairKey(pair[0], pair[1])]; bad {
						hardReasons = append(hardReasons, "diversity_incompatible")
						break
					}
				}
				metrics := listMetricsFor(selectedIDs, utilityByID, pairwiseByKey, marginalByID, constraints)
				var utilityPass, diversityPass *bool
				failureReasons := hardReasons
				// 对齐 :991-1004 完整判定
				if len(hardReasons) == 0 {
					requiredReasons := completeRequiredReasons(selected, constraints)
					if len(requiredReasons) > 0 {
						failureReasons = requiredReasons
					} else {
						passTrue := true
						utilityPass, diversityPass = &passTrue, &passTrue
						failureReasons = []string{}
						passingByDepth[depth] = append(passingByDepth[depth],
							selectionPassingItem{positions: expanded, metrics: metrics})
					}
				}
				attempts = append(attempts, selectionAttemptRow{
					positions: expanded, candidateIDs: selectedIDs, hardConstraintsPass: len(hardReasons) == 0,
					utilityPass: utilityPass, diversityPass: diversityPass,
					meanUtility: metrics.meanUtility, meanDiversity: metrics.meanDiversity,
					minimumPairwise: metrics.minimumPairwiseDiversity, objective: metrics.objective,
					failureReasons: failureReasons,
				})
				// 对齐 :1021-1022 硬约束通过且目标可计算 → 进入 feasible
				if len(hardReasons) == 0 && metrics.objective != nil {
					feasible = append(feasible, selectionPassingItem{positions: expanded, metrics: metrics})
				}
			}
			if budgetExhausted {
				break
			}
		}
		if budgetExhausted {
			// 对齐 :1025-1044 预算耗尽轨迹
			for _, budgetAttempt := range attempts {
				budgetFailureReasons := append([]string{}, budgetAttempt.failureReasons...)
				if !stringInSlice("search_budget_exhausted", budgetFailureReasons) {
					budgetFailureReasons = append(budgetFailureReasons, "search_budget_exhausted")
				}
				traceRows = append(traceRows, selectionTraceRow{
					candidateIDs:        budgetAttempt.candidateIDs,
					depth:               depth,
					hardConstraintsPass: budgetAttempt.hardConstraintsPass,
					utilityPass:         budgetAttempt.utilityPass,
					diversityPass:       budgetAttempt.diversityPass,
					meanUtility:         budgetAttempt.meanUtility,
					meanDiversity:       budgetAttempt.meanDiversity,
					minimumPairwise:     budgetAttempt.minimumPairwise,
					objective:           budgetAttempt.objective,
					status:              "search_budget_exhausted",
					retainedInBeam:      false,
					failureReasons:      budgetFailureReasons,
				})
			}
			break
		}

		// 对齐 :1046-1053 feasible 排序取束
		sort.SliceStable(feasible, func(i, j int) bool {
			oi := categoryOverflow(positionsToCandidates(feasible[i].positions, eligible), quotas)
			oj := categoryOverflow(positionsToCandidates(feasible[j].positions, eligible), quotas)
			if oi != oj {
				return oi < oj
			}
			objI := 0.0
			if feasible[i].metrics.objective != nil {
				objI = *feasible[i].metrics.objective
			}
			objJ := 0.0
			if feasible[j].metrics.objective != nil {
				objJ = *feasible[j].metrics.objective
			}
			if objI != objJ {
				return objI > objJ // -objective 升序
			}
			return orderTupleLess(feasible[i].positions, feasible[j].positions, eligible)
		})
		width := constraints.BeamWidth
		if len(feasible) < width {
			width = len(feasible)
		}
		beam = nil
		for _, item := range feasible[:width] {
			beam = append(beam, item.positions)
		}
		retained := map[string]struct{}{}
		for _, positions := range beam {
			retained[fmt.Sprint(positions)] = struct{}{}
		}
		// 对齐 :1055-1066 轨迹行状态
		for _, attempt := range attempts {
			isRetained := false
			if _, ok := retained[fmt.Sprint(attempt.positions)]; ok {
				isRetained = true
			}
			status := ""
			if !attempt.hardConstraintsPass {
				status = "hard_constraint_rejected"
			} else if depth == targetSize {
				if isRetained {
					status = "complete_retained"
				} else {
					status = "complete_pruned"
				}
			} else if isRetained {
				status = "partial_retained"
			} else {
				status = "partial_pruned"
			}
			traceRows = append(traceRows, selectionTraceRow{
				candidateIDs:        attempt.candidateIDs,
				depth:               depth,
				hardConstraintsPass: attempt.hardConstraintsPass,
				utilityPass:         attempt.utilityPass,
				diversityPass:       attempt.diversityPass,
				meanUtility:         attempt.meanUtility,
				meanDiversity:       attempt.meanDiversity,
				minimumPairwise:     attempt.minimumPairwise,
				objective:           attempt.objective,
				status:              status,
				retainedInBeam:      isRetained,
				failureReasons:      attempt.failureReasons,
			})
		}
		if len(beam) == 0 {
			break
		}
	}

	// 对齐 :1070-1085 偏好列表：最深通过层中按键取最小
	var preferredPositions []int
	if !budgetExhausted && len(passingByDepth) > 0 {
		maximumDepth := 0
		for depth := range passingByDepth {
			if depth > maximumDepth {
				maximumDepth = depth
			}
		}
		items := passingByDepth[maximumDepth]
		if len(items) > 0 {
			best := 0
			for i := 1; i < len(items); i++ {
				if passingLess(items[i], items[best], eligible, quotas) {
					best = i
				}
			}
			preferredPositions = items[best].positions
		}
	}
	preferredIDs := []string{}
	if preferredPositions != nil {
		for _, index := range preferredPositions {
			preferredIDs = append(preferredIDs, eligible[index].CandidateID)
		}
	}
	// 对齐 :1086-1088 偏好行标 preferred
	if len(preferredIDs) > 0 {
		for i := range traceRows {
			if stringSliceEqual(traceRows[i].candidateIDs, preferredIDs) {
				traceRows[i].status = "preferred"
			}
		}
	}

	entries := buildTraceEntries(traceRows)
	if len(preferredIDs) == 0 {
		// 对齐 :1094-1109 失败原因分支
		failureReason := "no_feasible_complete_list"
		if budgetExhausted {
			failureReason = "search_budget_exhausted"
		} else if len(utilityDecisions) > 0 && !anyTrue(utilityDecisions) {
			failureReason = "quality_abstained"
		}
		return emptySearchResult(ordered, hardReasonsByID, entries, expansionCount,
			failureReason, utility, diversity, constraints), nil
	}

	// 对齐 :1111-1148 判定与成功结果
	preferred := map[string]struct{}{}
	for _, id := range preferredIDs {
		preferred[id] = struct{}{}
	}
	decisionReasons := func(candidate *SelectionCandidate) []string {
		if _, ok := preferred[candidate.CandidateID]; ok {
			return []string{"selected_by_host_list_search"}
		}
		if len(hardReasonsByID[candidate.CandidateID]) > 0 {
			return hardReasonsByID[candidate.CandidateID]
		}
		return []string{"list_level_not_selected"}
	}
	decisions := make([]CandidateHostDecision, 0, len(ordered))
	for _, candidate := range ordered {
		_, isSelected := preferred[candidate.CandidateID]
		decisions = append(decisions, CandidateHostDecision{
			CandidateID: candidate.CandidateID,
			Selected:    isSelected,
			Reasons:     decisionReasons(candidate),
		})
	}
	trace := BoundedBeamTrace{
		Entries:                entries,
		PreferredCandidateIDs:  preferredIDs,
		TargetSize:             targetSize,
		ExpansionCount:         expansionCount,
		BeamWidth:              constraints.BeamWidth,
		MaxExpansions:          constraints.MaxExpansions,
		UtilityScorerVersion:   utility.ScorerVersion,
		DiversityScorerVersion: diversity.ScorerVersion,
		SearchVersion:          LIST_SEARCH_VERSION,
	}
	return &ListSearchResult{
		PreferredList:          preferredIDs,
		Decisions:              decisions,
		BoundedBeamTrace:       trace,
		FailureReason:          "",
		UtilityScorerVersion:   utility.ScorerVersion,
		DiversityScorerVersion: diversity.ScorerVersion,
		UtilityReceipt:         utility.Receipt,
		DiversityReceipt:       diversity.Receipt,
		SearchVersion:          LIST_SEARCH_VERSION,
	}, nil
}

// MineHierarchicalPairs 从束轨迹挖掘偏好对（对齐 :1151-1203）。
func MineHierarchicalPairs(beamTrace *BoundedBeamTrace) ([]*HierarchicalPreferencePair, error) {
	if beamTrace == nil {
		return nil, fmt.Errorf("beam_trace must be BoundedBeamTrace")
	}
	if len(beamTrace.PreferredCandidateIDs) == 0 {
		return []*HierarchicalPreferencePair{}, nil
	}
	pairs := []*HierarchicalPreferencePair{}
	type seenKey struct {
		candidateIDs string
		negativeType string
	}
	seen := map[seenKey]struct{}{}
	for i := range beamTrace.Entries {
		entry := &beamTrace.Entries[i]
		if len(entry.FailureReasons) == 0 || len(entry.CandidateIDs) == 0 {
			continue
		}
		if stringSliceEqual(entry.CandidateIDs, beamTrace.PreferredCandidateIDs) {
			continue
		}
		// 对齐 :1170-1183 负例类型
		var negativeType string
		if entry.HardConstraintsPass && entry.UtilityPass != nil && *entry.UtilityPass &&
			entry.DiversityPass != nil && !*entry.DiversityPass {
			negativeType = "diversity_neg"
		} else if entry.HardConstraintsPass && entry.UtilityPass != nil && !*entry.UtilityPass &&
			entry.DiversityPass != nil && *entry.DiversityPass {
			negativeType = "utility_neg"
		} else {
			negativeType = "joint_failure_neg"
		}
		key := seenKey{strings.Join(entry.CandidateIDs, "\x00"), negativeType}
		if _, dup := seen[key]; dup {
			continue
		}
		seen[key] = struct{}{}
		pairs = append(pairs, &HierarchicalPreferencePair{
			PairID:                 fmt.Sprintf("%s:%d:%s", PREFERENCE_PAIR_VERSION, entry.TraceIndex, negativeType),
			PreferredCandidateIDs:  copyStringSlice(beamTrace.PreferredCandidateIDs),
			NegativeCandidateIDs:   copyStringSlice(entry.CandidateIDs),
			NegativeType:           negativeType,
			SourceTraceIndex:       entry.TraceIndex,
			FailureReasons:         copyStringSlice(entry.FailureReasons),
			UtilityScorerVersion:   beamTrace.UtilityScorerVersion,
			DiversityScorerVersion: beamTrace.DiversityScorerVersion,
			SearchVersion:          beamTrace.SearchVersion,
			PairVersion:            PREFERENCE_PAIR_VERSION,
		})
	}
	return pairs, nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// selectionTraceRow hybrid_list_search 的轨迹行（对齐 :907-1066 trace_rows dict 结构）。
type selectionTraceRow struct {
	candidateIDs        []string
	depth               int
	hardConstraintsPass bool
	utilityPass         *bool
	diversityPass       *bool
	meanUtility         *float64
	meanDiversity       *float64
	minimumPairwise     *float64
	objective           *float64
	status              string
	retainedInBeam      bool
	failureReasons      []string
}

// selectionAttemptRow 束搜索单次尝试行（对齐 :955 attempts dict 结构）。
type selectionAttemptRow struct {
	positions           []int
	candidateIDs        []string
	hardConstraintsPass bool
	utilityPass         *bool
	diversityPass       *bool
	meanUtility         *float64
	meanDiversity       *float64
	minimumPairwise     *float64
	objective           *float64
	failureReasons      []string
}

// selectionPassingItem 通过项（对齐 :951 feasible 元组 (positions, metrics)）。
type selectionPassingItem struct {
	positions []int
	metrics   listMetrics
}

// selectionPairKey 成对键（对齐 :459-460 _pair_key）。
type selectionPairKey struct {
	left  string
	right string
}

// pairKey 排序对键。
func pairKey(left, right string) selectionPairKey {
	if left < right {
		return selectionPairKey{left: left, right: right}
	}
	return selectionPairKey{left: right, right: left}
}

// selectionNonempty 非空白字符串（对齐 :57-58，复用 terminalNonempty）。
func selectionNonempty(value string) bool { return terminalNonempty(value) }

// nullableScore 可空分数校验（对齐 :61-69 _nullable_score）。
func nullableScore(value any, fieldName string) (*float64, error) {
	if value == nil {
		return nil, nil
	}
	if _, isBool := value.(bool); isBool {
		return nil, fmt.Errorf("%s must be a finite number or null", fieldName)
	}
	score := num(value)
	if score == 0 && !isNumericAny(value) {
		return nil, fmt.Errorf("%s must be a finite number or null", fieldName)
	}
	if math.IsNaN(score) || math.IsInf(score, 0) {
		return nil, fmt.Errorf("%s must be a finite number or null", fieldName)
	}
	return &score, nil
}

// isNumericAny 是否数字类型（bool 不算）。
func isNumericAny(value any) bool {
	switch value.(type) {
	case float64, int, int64:
		return true
	default:
		return false
	}
}

// requireSuccessfulReceipt 小票闸（对齐 :337-347）。
func requireSuccessfulReceipt(receipt *LearnedComponentReceipt, componentID string) error {
	if receipt == nil {
		return fmt.Errorf("scoring invocation requires LearnedComponentReceipt")
	}
	if receipt.Binding.ComponentID != componentID {
		return fmt.Errorf("receipt component_id does not match scorer")
	}
	if receipt.OutputStatus != "success" {
		return fmt.Errorf("scoring receipt must be successful")
	}
	return nil
}

// validGivenConditions 固定条件合法性（对齐 :447-456）。
func validGivenConditions(value any) bool {
	conditions, ok := value.([]any)
	if !ok {
		return false
	}
	for _, raw := range conditions {
		condition, keys, isMap := payloadMap(raw)
		if !isMap || !stringSetEqual(keys, []string{"card_quote", "condition", "source", "source_quote"}) {
			return false
		}
		if !selectionNonempty(str(condition["card_quote"])) ||
			!selectionNonempty(str(condition["condition"])) ||
			!selectionNonempty(str(condition["source_quote"])) {
			return false
		}
		source := str(condition["source"])
		found := false
		for _, allowed := range givenConditionSources {
			if source == allowed {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

// utilityScoresByID 校验效用响应（对齐 :380-444）。
func utilityScoresByID(raw any, candidateIDs []string) (map[string]UtilityScore, error) {
	rawMap, rawKeys, ok := payloadMap(raw)
	scoresRaw, isList := rawMap["scores"].([]any)
	if !ok || !stringSetEqual(rawKeys, []string{"scores"}) || !isList {
		return nil, fmt.Errorf("utility scorer output schema invalid")
	}
	byID := map[string]UtilityScore{}
	expectedFields := []string{
		"candidate_id", "question_focus", "assertion_spans", "given_conditions",
		"click_score_nullable", "depth_score_nullable", "combined_utility_nullable",
		"admitted", "reason",
	}
	memberSet := map[string]struct{}{}
	for _, id := range candidateIDs {
		memberSet[id] = struct{}{}
	}
	for _, item := range scoresRaw {
		itemMap, itemKeys, itemOK := payloadMap(item)
		spansRaw, spansIsList := itemMap["assertion_spans"].([]any)
		spansValid := spansIsList && func() bool {
			for _, span := range spansRaw {
				if !selectionNonempty(str(span)) {
					return false
				}
			}
			return true
		}()
		if !itemOK || !stringSetEqual(itemKeys, expectedFields) ||
			!selectionNonempty(str(itemMap["question_focus"])) ||
			!spansValid ||
			!validGivenConditions(itemMap["given_conditions"]) {
			return nil, fmt.Errorf("utility scorer output schema invalid")
		}
		candidateID := str(itemMap["candidate_id"])
		if _, member := memberSet[candidateID]; !selectionNonempty(candidateID) || !member {
			return nil, fmt.Errorf("utility scorer candidate membership invalid")
		}
		if _, dup := byID[candidateID]; dup {
			return nil, fmt.Errorf("utility scorer candidate membership invalid")
		}
		admitted, isBool := itemMap["admitted"].(bool)
		if !isBool || !selectionNonempty(str(itemMap["reason"])) {
			return nil, fmt.Errorf("utility scorer decision invalid")
		}
		click, err := nullableScore(itemMap["click_score_nullable"], "click_score_nullable")
		if err != nil {
			return nil, err
		}
		depth, err := nullableScore(itemMap["depth_score_nullable"], "depth_score_nullable")
		if err != nil {
			return nil, err
		}
		combined, err := nullableScore(itemMap["combined_utility_nullable"], "combined_utility_nullable")
		if err != nil {
			return nil, err
		}
		conditions := []map[string]string{}
		for _, rawCondition := range itemMap["given_conditions"].([]any) {
			conditionMap, _, _ := payloadMap(rawCondition)
			conditions = append(conditions, map[string]string{
				"card_quote":   str(conditionMap["card_quote"]),
				"condition":    str(conditionMap["condition"]),
				"source":       str(conditionMap["source"]),
				"source_quote": str(conditionMap["source_quote"]),
			})
		}
		byID[candidateID] = UtilityScore{
			Admitted:                admitted,
			Reason:                  str(itemMap["reason"]),
			GivenConditions:         conditions,
			CandidateID:             candidateID,
			ClickScoreNullable:      click,
			DepthScoreNullable:      depth,
			CombinedUtilityNullable: combined,
		}
	}
	if len(byID) != len(candidateIDs) {
		return nil, fmt.Errorf("utility scorer must cover every candidate exactly once")
	}
	return byID, nil
}

// scoreDiversityValidate 校验多样性响应（对齐 :482-571 主体）。
func scoreDiversityValidate(raw any, candidateIDs []string, receipt *LearnedComponentReceipt) (*DiversityScoringResult, error) {
	rawMap, rawKeys, ok := payloadMap(raw)
	pairwiseRaw, pairwiseIsList := rawMap["pairwise_scores"].([]any)
	marginalRaw, marginalIsList := rawMap["marginal_scores"].([]any)
	if !ok || !stringSetEqual(rawKeys, []string{"pairwise_scores", "marginal_scores"}) ||
		!pairwiseIsList || !marginalIsList {
		return nil, fmt.Errorf("diversity scorer output schema invalid")
	}
	memberSet := map[string]struct{}{}
	for _, id := range candidateIDs {
		memberSet[id] = struct{}{}
	}
	// 对齐 :493-531 成对评分
	pairFields := []string{"left_candidate_id", "right_candidate_id", "diversity_score_nullable", "compatible", "reason"}
	pairwiseByKey := map[selectionPairKey]PairwiseDiversityScore{}
	for _, item := range pairwiseRaw {
		itemMap, itemKeys, itemOK := payloadMap(item)
		if !itemOK || !stringSetEqual(itemKeys, pairFields) {
			return nil, fmt.Errorf("diversity scorer output schema invalid")
		}
		left := str(itemMap["left_candidate_id"])
		right := str(itemMap["right_candidate_id"])
		leftMember, rightMember := false, false
		for _, id := range candidateIDs {
			if id == left {
				leftMember = true
			}
			if id == right {
				rightMember = true
			}
		}
		if !selectionNonempty(left) || !selectionNonempty(right) || left == right || !leftMember || !rightMember {
			return nil, fmt.Errorf("diversity scorer pair membership invalid")
		}
		key := pairKey(left, right)
		if _, dup := pairwiseByKey[key]; dup {
			return nil, fmt.Errorf("diversity scorer pair membership invalid")
		}
		compatible, isBool := itemMap["compatible"].(bool)
		if !isBool || !selectionNonempty(str(itemMap["reason"])) {
			return nil, fmt.Errorf("diversity scorer decision invalid")
		}
		score, err := nullableScore(itemMap["diversity_score_nullable"], "diversity_score_nullable")
		if err != nil {
			return nil, err
		}
		pairwiseByKey[key] = PairwiseDiversityScore{
			Compatible:             compatible,
			Reason:                 str(itemMap["reason"]),
			LeftCandidateID:        key.left,
			RightCandidateID:       key.right,
			DiversityScoreNullable: score,
		}
	}
	// 对齐 :533-551 边际评分
	marginalFields := []string{"candidate_id", "marginal_diversity_nullable"}
	marginalByID := map[string]MarginalDiversityScore{}
	for _, item := range marginalRaw {
		itemMap, itemKeys, itemOK := payloadMap(item)
		if !itemOK || !stringSetEqual(itemKeys, marginalFields) {
			return nil, fmt.Errorf("diversity scorer output schema invalid")
		}
		candidateID := str(itemMap["candidate_id"])
		if _, member := memberSet[candidateID]; !selectionNonempty(candidateID) || !member {
			return nil, fmt.Errorf("diversity scorer marginal membership invalid")
		}
		if _, dup := marginalByID[candidateID]; dup {
			return nil, fmt.Errorf("diversity scorer marginal membership invalid")
		}
		score, err := nullableScore(itemMap["marginal_diversity_nullable"], "marginal_diversity_nullable")
		if err != nil {
			return nil, err
		}
		marginalByID[candidateID] = MarginalDiversityScore{
			CandidateID:               candidateID,
			MarginalDiversityNullable: score,
		}
	}
	// 对齐 :553-559 全覆盖校验
	expectedPairs := map[selectionPairKey]struct{}{}
	for _, pair := range combinations2(candidateIDs) {
		expectedPairs[pairKey(pair[0], pair[1])] = struct{}{}
	}
	if len(pairwiseByKey) != len(expectedPairs) {
		return nil, fmt.Errorf("diversity scorer must cover every pair exactly once")
	}
	for key := range expectedPairs {
		if _, present := pairwiseByKey[key]; !present {
			return nil, fmt.Errorf("diversity scorer must cover every pair exactly once")
		}
	}
	if len(marginalByID) != len(candidateIDs) {
		return nil, fmt.Errorf("diversity scorer must cover every marginal exactly once")
	}
	// 对齐 :561-571 按输入序组织
	pairOrder := []PairwiseDiversityScore{}
	for _, pair := range combinations2(candidateIDs) {
		pairOrder = append(pairOrder, pairwiseByKey[pairKey(pair[0], pair[1])])
	}
	marginalScores := make([]MarginalDiversityScore, 0, len(candidateIDs))
	for _, id := range candidateIDs {
		marginalScores = append(marginalScores, marginalByID[id])
	}
	return &DiversityScoringResult{
		PairwiseScores: pairOrder,
		MarginalScores: marginalScores,
		ScorerVersion:  receipt.Binding.ConfigVersion,
		Receipt:        receipt,
	}, nil
}

// candidateOrderLess 候选排序（对齐 :574-575 (stable_rank, candidate_id)）。
func candidateOrderLess(a, b *SelectionCandidate) bool {
	if a.StableRank != b.StableRank {
		return a.StableRank < b.StableRank
	}
	return a.CandidateID < b.CandidateID
}

// candidateHardReasons 候选硬过滤原因（对齐 :578-598）。
func candidateHardReasons(candidate *SelectionCandidate, constraints *ListSearchConstraints) []string {
	reasons := []string{}
	if candidate.ContextScope != constraints.ContextScope {
		reasons = append(reasons, "scope_mismatch")
	}
	quotas := constraints.Quotas()
	if _, allowed := quotas[candidate.Category]; !allowed {
		reasons = append(reasons, "category_not_allowed")
	}
	if !candidate.EvidenceEligible {
		reasons = append(reasons, "evidence_ineligible")
	}
	if !candidate.FreshnessEligible {
		reasons = append(reasons, "freshness_ineligible")
	}
	if candidate.NegativePreferenceHit {
		reasons = append(reasons, "negative_preference")
	}
	if constraints.ContextScope == globalScope {
		if !selectionNonempty(candidate.InterestSummaryID) {
			reasons = append(reasons, "missing_personalized_interest")
		}
		if !selectionNonempty(candidate.IdentityAnchor) {
			reasons = append(reasons, "missing_identity_anchor")
		}
	}
	return reasons
}

// partialHardReasons 部分列表硬约束（对齐 :601-625）。
func partialHardReasons(selected, remaining []*SelectionCandidate, quotas map[string]int,
	isGlobalScope bool, requiredInterests []string, firstWindow int) []string {
	reasons := []string{}
	if !isGlobalScope && categoryOverflow(selected, quotas) > 0 {
		reasons = append(reasons, "category_quota_exceeded")
	}
	if isGlobalScope && len(requiredInterests) > 0 {
		window := selected
		if len(selected) > firstWindow {
			window = selected[:firstWindow]
		}
		inWindow := false
		for _, candidate := range window {
			if stringInSlice(candidate.InterestSummaryID, requiredInterests) {
				inWindow = true
				break
			}
		}
		if !inWindow {
			if len(selected) >= firstWindow {
				reasons = append(reasons, "required_first_window_interest_missing")
			} else {
				fillable := false
				for _, candidate := range remaining {
					if stringInSlice(candidate.InterestSummaryID, requiredInterests) {
						fillable = true
						break
					}
				}
				if !fillable {
					reasons = append(reasons, "required_first_window_interest_unfillable")
				}
			}
		}
	}
	return reasons
}

// categoryOverflow 列表超出配额类别混合的程度（对齐 :628-635）。
func categoryOverflow(selected []*SelectionCandidate, quotas map[string]int) int {
	counts := map[string]int{}
	for _, candidate := range selected {
		counts[candidate.Category]++
	}
	overflow := 0
	for category, count := range counts {
		if quota, ok := quotas[category]; ok && count > quota {
			overflow += count - quota
		}
	}
	return overflow
}

// completeRequiredReasons 完整列表必需项校验（对齐 :638-652）。
func completeRequiredReasons(selected []*SelectionCandidate, constraints *ListSearchConstraints) []string {
	if constraints.ContextScope != globalScope {
		hasContinuation := false
		for _, candidate := range selected {
			if candidate.Category == "project_continuation" {
				hasContinuation = true
				break
			}
		}
		if !hasContinuation {
			return []string{"project_continuation_required"}
		}
	}
	requiredInterests := constraints.RequiredFirstWindowInterestSummaryIDs
	if len(requiredInterests) == 0 {
		return []string{}
	}
	window := selected
	if len(selected) > constraints.FirstWindowSize() {
		window = selected[:constraints.FirstWindowSize()]
	}
	for _, candidate := range window {
		if stringInSlice(candidate.InterestSummaryID, requiredInterests) {
			return []string{}
		}
	}
	return []string{"required_first_window_interest_missing"}
}

// scoreMaps 分数映射（对齐 :655-728 _score_maps）。
func scoreMaps(candidates []*SelectionCandidate, eligibleIDs map[string]struct{},
	utility *UtilityScoringResult, diversity *DiversityScoringResult,
) (map[string]float64, map[selectionPairKey]float64, map[string]float64, error) {
	if err := requireSuccessfulReceipt(utility.Receipt, UTILITY_COMPONENT_ID); err != nil {
		return nil, nil, nil, err
	}
	if err := requireSuccessfulReceipt(diversity.Receipt, DIVERSITY_COMPONENT_ID); err != nil {
		return nil, nil, nil, err
	}
	if utility.ScorerVersion != utility.Receipt.Binding.ConfigVersion {
		return nil, nil, nil, fmt.Errorf("utility scorer_version disagrees with receipt")
	}
	if diversity.ScorerVersion != diversity.Receipt.Binding.ConfigVersion {
		return nil, nil, nil, fmt.Errorf("diversity scorer_version disagrees with receipt")
	}
	if utility.Receipt.InputSnapshotID != diversity.Receipt.InputSnapshotID {
		return nil, nil, nil, fmt.Errorf("utility and diversity snapshots must match")
	}
	inputIDs := map[string]struct{}{}
	for _, candidate := range candidates {
		inputIDs[candidate.CandidateID] = struct{}{}
	}
	// 对齐 :675-689 效用映射（`or 0.0` 语义：nil/0 → 0.0）
	utilityByID := map[string]float64{}
	for i := range utility.Scores {
		score := &utility.Scores[i]
		if _, member := inputIDs[score.CandidateID]; !member {
			return nil, nil, nil, fmt.Errorf("utility scores contain invalid candidate membership")
		}
		if _, dup := utilityByID[score.CandidateID]; dup {
			return nil, nil, nil, fmt.Errorf("utility scores contain invalid candidate membership")
		}
		if !selectionNonempty(score.Reason) {
			return nil, nil, nil, fmt.Errorf("utility scorer decision invalid")
		}
		value := 0.0
		if score.CombinedUtilityNullable != nil && *score.CombinedUtilityNullable != 0 {
			value = *score.CombinedUtilityNullable
		}
		utilityByID[score.CandidateID] = value
	}
	if len(utilityByID) != len(inputIDs) {
		return nil, nil, nil, fmt.Errorf("utility scores do not cover every eligible candidate")
	}
	// 对齐 :691-703 边际映射
	marginalByID := map[string]float64{}
	for i := range diversity.MarginalScores {
		score := &diversity.MarginalScores[i]
		if _, member := inputIDs[score.CandidateID]; !member {
			return nil, nil, nil, fmt.Errorf("marginal scores contain invalid candidate membership")
		}
		if _, dup := marginalByID[score.CandidateID]; dup {
			return nil, nil, nil, fmt.Errorf("marginal scores contain invalid candidate membership")
		}
		value := 0.0
		if score.MarginalDiversityNullable != nil && *score.MarginalDiversityNullable != 0 {
			value = *score.MarginalDiversityNullable
		}
		marginalByID[score.CandidateID] = value
	}
	if len(marginalByID) != len(inputIDs) {
		return nil, nil, nil, fmt.Errorf("marginal scores do not cover every eligible candidate")
	}
	// 对齐 :705-727 成对映射
	pairwiseByKey := map[selectionPairKey]float64{}
	for i := range diversity.PairwiseScores {
		score := &diversity.PairwiseScores[i]
		key := pairKey(score.LeftCandidateID, score.RightCandidateID)
		if score.LeftCandidateID == score.RightCandidateID {
			return nil, nil, nil, fmt.Errorf("pairwise scores contain invalid candidate membership")
		}
		for _, id := range []string{key.left, key.right} {
			if _, member := inputIDs[id]; !member {
				return nil, nil, nil, fmt.Errorf("pairwise scores contain invalid candidate membership")
			}
		}
		if _, dup := pairwiseByKey[key]; dup {
			return nil, nil, nil, fmt.Errorf("pairwise scores contain invalid candidate membership")
		}
		if !selectionNonempty(score.Reason) {
			return nil, nil, nil, fmt.Errorf("diversity scorer decision invalid")
		}
		value := 0.0
		if score.DiversityScoreNullable != nil && *score.DiversityScoreNullable != 0 {
			value = *score.DiversityScoreNullable
		}
		pairwiseByKey[key] = value
	}
	expectedEligiblePairs := 0
	for _, pair := range combinations2(mapKeys(inputIDs)) {
		expectedEligiblePairs++
		if _, present := pairwiseByKey[pairKey(pair[0], pair[1])]; !present {
			return nil, nil, nil, fmt.Errorf("pairwise scores do not cover every eligible pair")
		}
	}
	if len(pairwiseByKey) != expectedEligiblePairs {
		return nil, nil, nil, fmt.Errorf("pairwise scores do not cover every eligible pair")
	}
	return utilityByID, pairwiseByKey, marginalByID, nil
}

// listMetricsFor 列表指标（对齐 :740-783 _list_metrics）。
func listMetricsFor(candidateIDs []string, utilityByID map[string]float64,
	pairwiseByKey map[selectionPairKey]float64, marginalByID map[string]float64,
	constraints *ListSearchConstraints) listMetrics {
	utilityValues := make([]float64, 0, len(candidateIDs))
	for _, id := range candidateIDs {
		utilityValues = append(utilityValues, utilityByID[id])
	}
	meanUtility := mean(utilityValues)
	pairwiseValues := []float64{}
	for _, pair := range combinations2(candidateIDs) {
		pairwiseValues = append(pairwiseValues, pairwiseByKey[pairKey(pair[0], pair[1])])
	}
	marginalValues := make([]float64, 0, len(candidateIDs))
	for _, id := range candidateIDs {
		marginalValues = append(marginalValues, marginalByID[id])
	}
	diversityValues := append(append([]float64{}, pairwiseValues...), marginalValues...)
	meanDiversity := mean(diversityValues)
	var minimumPairwise *float64
	if len(pairwiseValues) > 0 {
		minValue := pairwiseValues[0]
		for _, v := range pairwiseValues[1:] {
			if v < minValue {
				minValue = v
			}
		}
		minimumPairwise = &minValue
	}
	var objective *float64
	if meanUtility != nil && meanDiversity != nil {
		value := constraints.UtilityWeight**meanUtility + constraints.DiversityWeight**meanDiversity
		objective = &value
	}
	return listMetrics{
		meanUtility:              meanUtility,
		meanDiversity:            meanDiversity,
		minimumPairwiseDiversity: minimumPairwise,
		pairwiseCount:            len(pairwiseValues),
		objective:                objective,
	}
}

// emptySearchResult 空结果（对齐 :786-824 _empty_search_result）。
func emptySearchResult(candidates []*SelectionCandidate, hardReasonsByID map[string][]string,
	traceEntries []BeamTraceEntry, expansionCount int, failureReason string,
	utility *UtilityScoringResult, diversity *DiversityScoringResult,
	constraints *ListSearchConstraints) *ListSearchResult {
	sortedCandidates := append([]*SelectionCandidate{}, candidates...)
	sort.SliceStable(sortedCandidates, func(i, j int) bool {
		return candidateOrderLess(sortedCandidates[i], sortedCandidates[j])
	})
	decisions := make([]CandidateHostDecision, 0, len(sortedCandidates))
	for _, candidate := range sortedCandidates {
		reasons := hardReasonsByID[candidate.CandidateID]
		if len(reasons) == 0 {
			reasons = []string{failureReason}
		}
		decisions = append(decisions, CandidateHostDecision{
			CandidateID: candidate.CandidateID,
			Selected:    false,
			Reasons:     reasons,
		})
	}
	trace := BoundedBeamTrace{
		Entries:                traceEntries,
		PreferredCandidateIDs:  []string{},
		TargetSize:             constraints.TargetSize(),
		ExpansionCount:         expansionCount,
		BeamWidth:              constraints.BeamWidth,
		MaxExpansions:          constraints.MaxExpansions,
		UtilityScorerVersion:   utility.ScorerVersion,
		DiversityScorerVersion: diversity.ScorerVersion,
		SearchVersion:          LIST_SEARCH_VERSION,
	}
	return &ListSearchResult{
		PreferredList:          []string{},
		Decisions:              decisions,
		BoundedBeamTrace:       trace,
		FailureReason:          failureReason,
		UtilityScorerVersion:   utility.ScorerVersion,
		DiversityScorerVersion: diversity.ScorerVersion,
		UtilityReceipt:         utility.Receipt,
		DiversityReceipt:       diversity.Receipt,
		SearchVersion:          LIST_SEARCH_VERSION,
	}
}

// buildTraceEntries 轨迹行 → BeamTraceEntry（对齐 :1090-1093 enumerate）。
func buildTraceEntries(rows []selectionTraceRow) []BeamTraceEntry {
	entries := make([]BeamTraceEntry, 0, len(rows))
	for i, row := range rows {
		entries = append(entries, BeamTraceEntry{
			TraceIndex:                       i + 1,
			CandidateIDs:                     row.candidateIDs,
			Depth:                            row.depth,
			Status:                           row.status,
			RetainedInBeam:                   row.retainedInBeam,
			HardConstraintsPass:              row.hardConstraintsPass,
			UtilityPass:                      row.utilityPass,
			DiversityPass:                    row.diversityPass,
			MeanUtilityNullable:              row.meanUtility,
			MeanDiversityNullable:            row.meanDiversity,
			MinimumPairwiseDiversityNullable: row.minimumPairwise,
			ObjectiveNullable:                row.objective,
			FailureReasons:                   row.failureReasons,
		})
	}
	return entries
}

// passingLess 通过项排序键（对齐 :1073-1080 min key）。
func passingLess(a, b struct {
	positions []int
	metrics   listMetrics
}, eligible []*SelectionCandidate, quotas map[string]int) bool {
	oa := categoryOverflow(positionsToCandidates(a.positions, eligible), quotas)
	ob := categoryOverflow(positionsToCandidates(b.positions, eligible), quotas)
	if oa != ob {
		return oa < ob
	}
	objA := 0.0
	if a.metrics.objective != nil {
		objA = *a.metrics.objective
	}
	objB := 0.0
	if b.metrics.objective != nil {
		objB = *b.metrics.objective
	}
	if objA != objB {
		return objA > objB
	}
	return orderTupleLess(a.positions, b.positions, eligible)
}

// positionsToCandidates 位置序列还原候选。
func positionsToCandidates(positions []int, eligible []*SelectionCandidate) []*SelectionCandidate {
	out := make([]*SelectionCandidate, 0, len(positions))
	for _, index := range positions {
		out = append(out, eligible[index])
	}
	return out
}

// orderTupleLess 位置序列的候选序字典序比较（对齐 tuple(_candidate_order(...))）。
func orderTupleLess(a, b []int, eligible []*SelectionCandidate) bool {
	for i := 0; i < len(a) && i < len(b); i++ {
		ca, cb := eligible[a[i]], eligible[b[i]]
		if ca.StableRank != cb.StableRank {
			return ca.StableRank < cb.StableRank
		}
		if ca.CandidateID != cb.CandidateID {
			return ca.CandidateID < cb.CandidateID
		}
	}
	return len(a) < len(b)
}

// combinations2 二元组合（对齐 itertools.combinations(ids, 2)）。
func combinations2(ids []string) [][2]string {
	out := [][2]string{}
	for i := 0; i < len(ids); i++ {
		for j := i + 1; j < len(ids); j++ {
			out = append(out, [2]string{ids[i], ids[j]})
		}
	}
	return out
}

// mean 均值（空或含 NaN 不会发生：输入全部有限）。
func mean(values []float64) *float64 {
	if len(values) == 0 {
		return nil
	}
	total := 0.0
	for _, v := range values {
		total += v
	}
	m := total / float64(len(values))
	return &m
}

// anyTrue map 值是否有任一 true（对齐 any(utility_decisions.values())）。
func anyTrue(decisions map[string]bool) bool {
	for _, v := range decisions {
		if v {
			return true
		}
	}
	return false
}

// mapKeys map 键切片。
func mapKeys(set map[string]struct{}) []string {
	keys := make([]string, 0, len(set))
	for k := range set {
		keys = append(keys, k)
	}
	return keys
}

// copyIntMap int map 拷贝（对齐 dict(_GLOBAL_QUOTAS)）。
func copyIntMap(values map[string]int) map[string]int {
	out := make(map[string]int, len(values))
	for k, v := range values {
		out[k] = v
	}
	return out
}

// copyStringSlice 字符串切片拷贝。
func copyStringSlice(values []string) []string {
	out := make([]string, len(values))
	copy(out, values)
	return out
}

// stringInSlice 包含判定。
func stringInSlice(value string, values []string) bool {
	for _, v := range values {
		if v == value {
			return true
		}
	}
	return false
}

// stringSliceEqual 切片逐元素相等。
func stringSliceEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

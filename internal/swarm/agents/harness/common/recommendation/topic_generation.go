package recommendation

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
)

// 本文件对齐 topic_generation.py（600 行）：I2/I4/I5 三阶段的可替换契约层——
// 排序（rank_interest_summaries）、候选生成（generate_starter_candidates）、
// 槽位选择（select_starter_slate）。Python 符号:行号逐条标注。
//
// 对应 Python 代码：jiuwenswarm/agents/harness/common/recommendation/topic_generation.py

// ──────────────────────────── 常量 ────────────────────────────

// I2PromptVersion I2 兴趣排序阶段提示词版本（topic_generation.py:13）
const I2PromptVersion = "topic-starter-v5-i2-rank-v1"

// I4PromptVersion I4 候选生成阶段提示词版本（topic_generation.py:14）
const I4PromptVersion = "topic-starter-v5-i4-generate-v1"

// I5PromptVersion I5 槽位选择阶段提示词版本（topic_generation.py:15）
const I5PromptVersion = "topic-starter-v5-i5-select-v1"

// OversampleSize 候选过采样池固定大小（topic_generation.py:17）
const OversampleSize = 12

// RetrievedIntentSlots 固定命中作为不可变前缀的槽数（topic_generation.py:18）
const RetrievedIntentSlots = 4

// GenerationConfigVersion 生成配置版本（topic_generation.py:19）
const GenerationConfigVersion = "topic-starter-v5-generation-v1"

// i2SystemPrompt I2 系统提示词（topic_generation.py:21-30，逐字复刻）
const i2SystemPrompt = "You are the independent Topic Starter I2 interest ranker (" + I2PromptVersion + ").\n" +
	"Rank every supplied snapshot interest exactly once. Use only\n" +
	"interest_summary_id values from snapshot and assign contiguous 1-based ranks.\n" +
	"api_score_nullable must be a finite number or null and is observational only.\n" +
	"Return exactly\n" +
	"{\"ranked_interests\":[{\"interest_summary_id\":\"...\",\"rank\":1,\n" +
	"\"api_score_nullable\":null}}]}. Do not add, omit, merge, rewrite, or explain\n" +
	"interests, and do not return extra fields.\n"

// i4SystemPrompt I4 系统提示词（topic_generation.py:32-47，逐字复刻）
const i4SystemPrompt = "You are the independent Topic Starter I4 candidate generator\n" +
	"(" + I4PromptVersion + "). Return exactly twelve candidates using the supplied\n" +
	"generation contract. Warm requests must contain 8 personalized-intent,\n" +
	"2 perspective-expansion, and 2 fresh-trend candidates. Cold requests must\n" +
	"contain 8 generic-intent, 2 generic-perspective, and 2 fresh-trend candidates.\n" +
	"The supplied retrieved_fixed_hits are an immutable leading prefix: copy each\n" +
	"hit into one retrieved-fixed candidate in the same order. Its source_starter_id\n" +
	"and title must be copied verbatim, without rewriting even one character; copy\n" +
	"its interest and provenance bindings too. Never invent another retrieved-fixed\n" +
	"candidate. Generated candidates must use origin generated and a null\n" +
	"source_starter_id. Every candidate must require web retrieval. Return only JSON\n" +
	"{\"candidates\":[...]} with the frozen candidate fields supplied by the\n" +
	"contract, with no explanations or extra fields.\n"

// i5SystemPrompt I5 系统提示词（topic_generation.py:49-59，逐字复刻）
const i5SystemPrompt = "You are the independent Topic Starter I5 grounded-slate selector\n" +
	"(" + I5PromptVersion + "). Select at most target_count items from the supplied\n" +
	"grounded candidates under selection_constraints. Use candidate IDs only;\n" +
	"never invent an ID. If any qualified retrieved-fixed candidate is supplied,\n" +
	"select at least one. Return contiguous 1-based selector ranks and a finite\n" +
	"observational score or null. Return exactly\n" +
	"{\"selections\":[{\"candidate_id\":\"...\",\"selector_rank\":1,\n" +
	"\"api_score_nullable\":null}}]}. Return candidate IDs only; Do not rewrite or\n" +
	"return titles, intents, evidence, queries, candidate content, explanations, or\n" +
	"extra fields.\n"

// 暖/冷启动槽位计数（topic_generation.py:61-72）
var warmLaneCounts = map[string]int{
	"personalized-intent":   8,
	"perspective-expansion": 2,
	"fresh-trend":           2,
}

var coldLaneCounts = map[string]int{
	"generic-intent":      8,
	"generic-perspective": 2,
	"fresh-trend":         2,
}

// candidateFields 候选冻结字段全集（topic_generation.py:74-94）
var candidateFields = map[string]struct{}{
	"candidate_id": {}, "lane": {}, "intent": {}, "persona": {},
	"provenance_relation": {}, "source_memory_unit_ids": {}, "source_locators": {},
	"identity_anchor": {}, "context_anchors": {}, "bridge": {},
	"primary_query": {}, "fallback_query": {}, "freshness_class": {},
	"source_requirement": {}, "title": {}, "origin": {},
	"source_starter_id": {}, "interest_summary_id": {},
}

// genericLanes 泛化通道集合（topic_generation.py:96-100）
var genericLanes = map[string]struct{}{
	"generic-intent": {}, "generic-perspective": {}, "fresh-trend": {},
}

// freshnessClasses 保鲜等级集合（topic_generation.py:101-105）
var freshnessClasses = map[string]struct{}{
	"current_event": {}, "recent_development": {}, "evergreen_corpus": {},
}

// ──────────────────────────── 结构体 ────────────────────────────

// ApiAdapter I2/I4/I5 共用的可替换 API 适配器函数类型
// （Python: ApiAdapter = Callable[[dict], Awaitable[Any]] :108；Go 侧携带 ctx））
type ApiAdapter = func(ctx context.Context, payload map[string]any) (any, error)

// StageInvokerFunc 阶段调用函数签名（Python: StageInvoker :109；
// Go 侧携带 ctx 以传递 attempt id——31c providerInvocationContext 同款映射）
type StageInvokerFunc = func(ctx context.Context, stage, systemPrompt, payload string) (any, error)

// RankedInterest I2 排序结果（Python dataclass :113-117）
type RankedInterest struct {
	// InterestSummaryId 兴趣摘要 ID
	InterestSummaryID string
	// Rank 1 起连续秩
	Rank int
	// APIScoreNullable 仅观测用的可空分数
	APIScoreNullable *float64
}

// AsPayload 对齐 Python asdict(RankedInterest)（I4 payload 中使用）
func (r RankedInterest) AsPayload() map[string]any {
	return map[string]any{
		"interest_summary_id": r.InterestSummaryID,
		"rank":                r.Rank,
		"api_score_nullable":  nullableFloatValue(r.APIScoreNullable),
	}
}

// OrderedSelection I5 选择结果（Python dataclass :120-124）
type OrderedSelection struct {
	// CandidateId 候选 ID
	CandidateID string
	// SelectorRank 1 起连续选择秩
	SelectorRank int
	// APIScoreNullable 仅观测用的可空分数
	APIScoreNullable *float64
}

// ArkInterestRankingAdapter I2 专属提示词适配器（Python :123-133）
type ArkInterestRankingAdapter struct {
	invokeStage StageInvokerFunc
}

// NewArkInterestRankingAdapter 构造（Python __init__ :125-127）
func NewArkInterestRankingAdapter(invokeStage StageInvokerFunc) *ArkInterestRankingAdapter {
	return &ArkInterestRankingAdapter{invokeStage: invokeStage}
}

// Invoke 调用 interest_ranker 阶段（Python __call__ :128-133）
func (a *ArkInterestRankingAdapter) Invoke(ctx context.Context, payload map[string]any) (any, error) {
	return a.invokeStage(ctx, "interest_ranker", i2SystemPrompt, marshalCompactPayload(payload))
}

// ArkCandidateGenerationAdapter I4 专属提示词适配器（Python :135-145）
type ArkCandidateGenerationAdapter struct {
	invokeStage StageInvokerFunc
}

// NewArkCandidateGenerationAdapter 构造（Python __init__ :137-139）
func NewArkCandidateGenerationAdapter(invokeStage StageInvokerFunc) *ArkCandidateGenerationAdapter {
	return &ArkCandidateGenerationAdapter{invokeStage: invokeStage}
}

// Invoke 调用 starter_candidate_generator 阶段（Python __call__ :140-145）
func (a *ArkCandidateGenerationAdapter) Invoke(ctx context.Context, payload map[string]any) (any, error) {
	return a.invokeStage(ctx, "starter_candidate_generator", i4SystemPrompt, marshalCompactPayload(payload))
}

// ArkSlateSelectionAdapter I5 专属提示词适配器（Python :147-157）
type ArkSlateSelectionAdapter struct {
	invokeStage StageInvokerFunc
}

// NewArkSlateSelectionAdapter 构造（Python __init__ :149-151）
func NewArkSlateSelectionAdapter(invokeStage StageInvokerFunc) *ArkSlateSelectionAdapter {
	return &ArkSlateSelectionAdapter{invokeStage: invokeStage}
}

// Invoke 调用 starter_slate_selector 阶段（Python __call__ :152-157）
func (a *ArkSlateSelectionAdapter) Invoke(ctx context.Context, payload map[string]any) (any, error) {
	return a.invokeStage(ctx, "starter_slate_selector", i5SystemPrompt, marshalCompactPayload(payload))
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// marshalCompactPayload 对齐 Python json.dumps(payload, ensure_ascii=False)
func marshalCompactPayload(payload map[string]any) string {
	data, err := json.Marshal(payload)
	if err != nil {
		return "null"
	}
	return string(data)
}

// plainInt 见 prompt_suggestion.go:340（全包共用判定，此处不重复定义）。

// finiteNullableScore 对齐 Python _finite_nullable_score（:159-165）
func finiteNullableScore(value any) (bool, *float64) {
	if value == nil {
		return true, nil
	}
	switch v := value.(type) {
	case int:
		return true, floatPtr(float64(v))
	case float64:
		if math.IsInf(v, 0) || math.IsNaN(v) {
			return false, nil
		}
		return true, floatPtr(v)
	default:
		return false, nil
	}
}

func floatPtr(v float64) *float64 { return &v }

func nullableFloatValue(p *float64) any {
	if p == nil {
		return nil
	}
	return *p
}

// ──────────────────────────── 导出函数 ────────────────────────────

// RankInterestSummaries I2：把全部冻结兴趣经可替换 API 适配器排序
// （Python rank_interest_summaries :168-231，步骤顺序逐条对应）
func RankInterestSummaries(ctx context.Context, snapshot []map[string]any, delta []map[string]any,
	feedbackContext map[string]any, invokeRanker ApiAdapter) ([]RankedInterest, string) {
	// 步骤 1：快照兴趣 ID 校验（:170-186）
	interestIDs := []string{}
	for _, item := range snapshot {
		id, ok := item["interest_summary_id"].(string)
		if !ok || id == "" {
			return nil, "interest_ranking_contract_failed"
		}
		interestIDs = append(interestIDs, id)
	}
	if len(interestIDs) != len(snapshot) {
		return nil, "interest_ranking_contract_failed"
	}
	seen := map[string]struct{}{}
	for _, id := range interestIDs {
		if _, dup := seen[id]; dup {
			return nil, "interest_ranking_contract_failed"
		}
		seen[id] = struct{}{}
	}

	// 步骤 2：调用适配器（:188-195）
	if invokeRanker == nil {
		return nil, "interest_ranking_failed"
	}
	payload := map[string]any{
		"prompt_version":   I2PromptVersion,
		"snapshot":         mapsToAny(snapshot),
		"delta":            mapsToAny(delta),
		"feedback_context": feedbackContext,
	}
	response, err := invokeRanker(ctx, payload)
	if err != nil {
		return nil, "interest_ranking_failed"
	}

	// 步骤 3：响应契约校验（:197-207）
	resp, ok := response.(map[string]any)
	if !ok || !exactKeys(resp, []string{"ranked_interests"}) {
		return nil, "interest_ranking_contract_failed"
	}
	rawList, ok := resp["ranked_interests"].([]any)
	if !ok {
		return nil, "interest_ranking_contract_failed"
	}

	// 步骤 4：逐条秩校验（:209-235）
	ranked := []RankedInterest{}
	for expectedRank, raw := range rawList {
		rawMap, ok := raw.(map[string]any)
		if !ok || !exactKeys(rawMap, []string{"interest_summary_id", "rank", "api_score_nullable"}) {
			return nil, "interest_ranking_contract_failed"
		}
		interestID, ok := rawMap["interest_summary_id"].(string)
		if !ok || interestID == "" {
			return nil, "interest_ranking_contract_failed"
		}
		if !plainInt(rawMap["rank"]) || intOf(rawMap["rank"]) != expectedRank+1 {
			return nil, "interest_ranking_contract_failed"
		}
		scoreValid, score := finiteNullableScore(rawMap["api_score_nullable"])
		if !scoreValid {
			return nil, "interest_ranking_contract_failed"
		}
		ranked = append(ranked, RankedInterest{InterestSummaryID: interestID,
			Rank: expectedRank + 1, APIScoreNullable: score})
	}

	// 步骤 5：集合一致性（:237-240）
	if len(ranked) != len(interestIDs) {
		return nil, "interest_ranking_contract_failed"
	}
	rankedSet := map[string]struct{}{}
	for _, item := range ranked {
		rankedSet[item.InterestSummaryID] = struct{}{}
	}
	for _, id := range interestIDs {
		if _, ok := rankedSet[id]; !ok {
			return nil, "interest_ranking_contract_failed"
		}
	}
	return ranked, ""
}

// GenerateStarterCandidates I4：生成冻结的 12 候选过采样池
// （Python generate_starter_candidates :309-404，步骤顺序逐条对应）
func GenerateStarterCandidates(ctx context.Context, rankedInterests []RankedInterest,
	starterHits []map[string]any, feedbackContext map[string]any, coldStart bool,
	invokeGenerator ApiAdapter, targetCount int,
	interestContextByID map[string]map[string]any) ([]map[string]any, string) {
	// 步骤 1：目标数与上下文键校验（:313-319）
	if targetCount != OversampleSize {
		return nil, "candidate_generation_contract_failed"
	}
	fixedHits := starterHits
	if len(fixedHits) > RetrievedIntentSlots {
		fixedHits = fixedHits[:RetrievedIntentSlots]
	}
	rankedIDs := map[string]struct{}{}
	for _, item := range rankedInterests {
		rankedIDs[item.InterestSummaryID] = struct{}{}
	}
	if interestContextByID != nil {
		if len(interestContextByID) != len(rankedIDs) {
			return nil, "candidate_generation_contract_failed"
		}
		for id := range interestContextByID {
			if _, ok := rankedIDs[id]; !ok {
				return nil, "candidate_generation_contract_failed"
			}
		}
	}

	// 步骤 2：构造 payload（:321-333）
	rankedPayload := make([]any, 0, len(rankedInterests))
	for _, item := range rankedInterests {
		rankedPayload = append(rankedPayload, item.AsPayload())
	}
	payload := map[string]any{
		"prompt_version":            I4PromptVersion,
		"generation_config_version": GenerationConfigVersion,
		"target_count":              targetCount,
		"cold_start":                coldStart,
		"ranked_interests":          rankedPayload,
		"retrieved_fixed_hits":      mapsToAny(fixedHits),
		"feedback_context":          feedbackContext,
	}
	if interestContextByID != nil {
		payload["interest_context_by_id"] = anyValueMapOfMaps(interestContextByID)
	}

	// 步骤 3：调用适配器（:335-341）
	response, err := invokeGenerator(ctx, payload)
	if err != nil {
		return nil, "candidate_generation_failed"
	}

	// 步骤 4：响应契约校验（:343-356）
	resp, ok := response.(map[string]any)
	if !ok || !exactKeys(resp, []string{"candidates"}) {
		return nil, "candidate_generation_contract_failed"
	}
	rawList, ok := resp["candidates"].([]any)
	if !ok || len(rawList) != OversampleSize {
		return nil, "candidate_generation_contract_failed"
	}
	candidates := make([]map[string]any, 0, len(rawList))
	for _, raw := range rawList {
		m, ok := raw.(map[string]any)
		if !ok {
			return nil, "candidate_generation_contract_failed"
		}
		candidates = append(candidates, m)
	}

	// 步骤 5：形状校验 + 唯一 ID（:358-364）
	idSeen := map[string]struct{}{}
	for _, candidate := range candidates {
		if !validCandidateShape(candidate) {
			return nil, "candidate_generation_contract_failed"
		}
		id := candidate["candidate_id"].(string)
		if _, dup := idSeen[id]; dup {
			return nil, "candidate_generation_contract_failed"
		}
		idSeen[id] = struct{}{}
	}

	// 步骤 6：槽位计数（:367-370）
	expectedCounts := coldLaneCounts
	if !coldStart {
		expectedCounts = warmLaneCounts
	}
	laneCount := map[string]int{}
	for _, candidate := range candidates {
		laneCount[candidate["lane"].(string)]++
	}
	if !intMapsEqual(laneCount, expectedCounts) {
		return nil, "candidate_generation_contract_failed"
	}

	// 步骤 7：语义校验（:372-382）
	for _, candidate := range candidates {
		if !validCandidateSemantics(candidate, coldStart, rankedIDs, interestContextByID) {
			return nil, "candidate_generation_contract_failed"
		}
	}

	// 步骤 8：固定命中候选数量（:384-390）
	fixedCount := 0
	for _, candidate := range candidates {
		if candidate["origin"] == "retrieved-fixed" {
			fixedCount++
		}
	}
	if fixedCount != len(fixedHits) {
		return nil, "candidate_generation_contract_failed"
	}

	// 步骤 9：前缀逐位匹配（:392-398，zip strict=False → 按下界长度配对）
	pairLen := len(candidates)
	if len(fixedHits) < pairLen {
		pairLen = len(fixedHits)
	}
	for i := 0; i < pairLen; i++ {
		if !fixedCandidateMatchesHit(candidates[i], fixedHits[i], coldStart) {
			return nil, "candidate_generation_contract_failed"
		}
	}
	return candidates, ""
}

// SelectStarterSlate I5：在保持排序权威可替换的前提下选择候选 ID
// （Python select_starter_slate :407-494，步骤顺序逐条对应）
func SelectStarterSlate(ctx context.Context, groundedCandidates []map[string]any,
	selectionConstraints map[string]any, invokeSelector ApiAdapter,
	targetCount int) ([]OrderedSelection, string) {
	// 步骤 1：候选 ID 校验（:411-424）
	candidateIDs := []string{}
	for _, candidate := range groundedCandidates {
		id, ok := candidate["candidate_id"].(string)
		if !ok || id == "" {
			return nil, "slate_selection_contract_failed"
		}
		candidateIDs = append(candidateIDs, id)
	}
	if targetCount != 4 {
		return nil, "slate_selection_contract_failed"
	}
	seen := map[string]struct{}{}
	for _, id := range candidateIDs {
		if _, dup := seen[id]; dup {
			return nil, "slate_selection_contract_failed"
		}
		seen[id] = struct{}{}
	}

	// 步骤 2：调用适配器（:426-438）
	payload := map[string]any{
		"prompt_version":        I5PromptVersion,
		"target_count":          targetCount,
		"grounded_candidates":   mapsToAny(groundedCandidates),
		"selection_constraints": selectionConstraints,
	}
	response, err := invokeSelector(ctx, payload)
	if err != nil {
		return nil, "slate_selection_failed"
	}

	// 步骤 3：响应契约校验（:440-450）
	resp, ok := response.(map[string]any)
	if !ok || !exactKeys(resp, []string{"selections"}) {
		return nil, "slate_selection_contract_failed"
	}
	rawList, ok := resp["selections"].([]any)
	if !ok {
		return nil, "slate_selection_contract_failed"
	}
	if len(rawList) > targetCount {
		return nil, "slate_selection_contract_failed"
	}

	// 步骤 4：逐条秩与去重校验（:452-478）
	selectedIDs := map[string]struct{}{}
	selections := []OrderedSelection{}
	for expectedRank, raw := range rawList {
		rawMap, ok := raw.(map[string]any)
		if !ok || !exactKeys(rawMap, []string{"candidate_id", "selector_rank", "api_score_nullable"}) {
			return nil, "slate_selection_contract_failed"
		}
		candidateID, ok := rawMap["candidate_id"].(string)
		if !ok {
			return nil, "slate_selection_contract_failed"
		}
		if _, known := seen[candidateID]; !known {
			return nil, "slate_selection_contract_failed"
		}
		if _, dup := selectedIDs[candidateID]; dup {
			return nil, "slate_selection_contract_failed"
		}
		if !plainInt(rawMap["selector_rank"]) || intOf(rawMap["selector_rank"]) != expectedRank+1 {
			return nil, "slate_selection_contract_failed"
		}
		scoreValid, score := finiteNullableScore(rawMap["api_score_nullable"])
		if !scoreValid {
			return nil, "slate_selection_contract_failed"
		}
		selectedIDs[candidateID] = struct{}{}
		selections = append(selections, OrderedSelection{CandidateID: candidateID,
			SelectorRank: expectedRank + 1, APIScoreNullable: score})
	}

	// 步骤 5：趋势去重 + 个性化去重（:480-489）
	candidatesByID := map[string]map[string]any{}
	for _, candidate := range groundedCandidates {
		candidatesByID[candidate["candidate_id"].(string)] = candidate
	}
	trendSeen := false
	eligible := []map[string]any{}
	for _, selection := range selections {
		candidate := candidatesByID[selection.CandidateID]
		if candidate["lane"] == "fresh-trend" {
			if trendSeen {
				continue
			}
			trendSeen = true
		}
		eligible = append(eligible, candidate)
	}
	kept := FilterDistinctPersonalizedCandidates(eligible, targetCount)

	// 步骤 6：retrieved-fixed 保底（:490-497）
	hasFixedGrounded := false
	for _, candidate := range groundedCandidates {
		if candidate["origin"] == "retrieved-fixed" {
			hasFixedGrounded = true
		}
	}
	hasFixedKept := false
	for _, candidate := range kept {
		if candidate["origin"] == "retrieved-fixed" {
			hasFixedKept = true
		}
	}
	if hasFixedGrounded && !hasFixedKept {
		return nil, "slate_selection_contract_failed"
	}

	// 步骤 7：按保留顺序返回（:498-501）
	selectionsByID := map[string]OrderedSelection{}
	for _, selection := range selections {
		selectionsByID[selection.CandidateID] = selection
	}
	result := []OrderedSelection{}
	for _, candidate := range kept {
		result = append(result, selectionsByID[candidate["candidate_id"].(string)])
	}
	return result, ""
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// validCandidateShape 对齐 Python _valid_candidate_shape（:234-273）
func validCandidateShape(candidate map[string]any) bool {
	if candidate == nil || !exactKeys(candidate, mapKeysOfSet(candidateFields)) {
		return false
	}
	for _, fieldName := range []string{
		"candidate_id", "lane", "intent", "persona", "provenance_relation",
		"identity_anchor", "primary_query", "fallback_query", "freshness_class",
		"source_requirement", "title", "origin",
	} {
		if _, ok := candidate[fieldName].(string); !ok {
			return false
		}
	}
	for _, fieldName := range []string{"source_memory_unit_ids", "source_locators", "context_anchors"} {
		if _, ok := candidate[fieldName].([]any); !ok {
			return false
		}
	}
	if _, ok := candidate["bridge"].(map[string]any); !ok {
		return false
	}
	for _, v := range candidate["source_memory_unit_ids"].([]any) {
		s, ok := v.(string)
		if !ok || s == "" {
			return false
		}
	}
	for _, raw := range candidate["source_locators"].([]any) {
		loc, ok := raw.([]any)
		if !ok || len(loc) != 2 {
			return false
		}
		s, ok := loc[0].(string)
		if !ok || s == "" || !plainInt(loc[1]) || intOf(loc[1]) < 0 {
			return false
		}
	}
	for _, raw := range candidate["context_anchors"].([]any) {
		s, ok := raw.(string)
		if !ok || s == "" {
			return false
		}
	}
	for _, fieldName := range []string{"source_starter_id", "interest_summary_id"} {
		if candidate[fieldName] == nil {
			continue
		}
		if _, ok := candidate[fieldName].(string); !ok {
			return false
		}
	}
	if candidate["candidate_id"] == "" || candidate["intent"] == "" ||
		candidate["persona"] == "" || candidate["primary_query"] == "" ||
		candidate["title"] == "" {
		return false
	}
	if candidate["source_requirement"] != "web_required" {
		return false
	}
	if _, ok := freshnessClasses[candidate["freshness_class"].(string)]; !ok {
		return false
	}
	return true
}

// validCandidateSemantics 对齐 Python _valid_candidate_semantics（:276-306）
func validCandidateSemantics(candidate map[string]any, coldStart bool,
	rankedInterestIDs map[string]struct{}, interestContextByID map[string]map[string]any) bool {
	lane := candidate["lane"].(string)
	_, generic := genericLanes[lane]
	interestID, _ := candidate["interest_summary_id"].(string) // nil → ""
	if generic {
		unitIDs := candidate["source_memory_unit_ids"].([]any)
		locators := candidate["source_locators"].([]any)
		anchors := candidate["context_anchors"].([]any)
		if !(candidate["interest_summary_id"] == nil &&
			len(unitIDs) == 0 && len(locators) == 0 &&
			candidate["identity_anchor"] == "" && len(anchors) == 0 &&
			candidate["provenance_relation"] == "generic") {
			return false
		}
	} else {
		_, inRanked := rankedInterestIDs[interestID]
		unitIDs := candidate["source_memory_unit_ids"].([]any)
		locators := candidate["source_locators"].([]any)
		nonGenericInvalid := coldStart || !inRanked ||
			len(unitIDs) == 0 || len(locators) == 0 ||
			candidate["identity_anchor"] == "" ||
			(candidate["provenance_relation"] != "stable_progress" &&
				candidate["provenance_relation"] != "recent_follow_up")
		if nonGenericInvalid {
			return false
		}
	}

	if !generic && interestContextByID != nil {
		context, ok := interestContextByID[interestID]
		if !ok {
			return false
		}
		allowedLocators := map[string]struct{}{}
		for _, raw := range anyToLocatorSlices(context["source_locators"]) {
			if len(raw) != 2 {
				continue
			}
			allowedLocators[fmt.Sprintf("%v|%d", raw[0], intOf(raw[1]))] = struct{}{}
		}
		candidateLocators := map[string]struct{}{}
		for _, raw := range candidate["source_locators"].([]any) {
			loc := raw.([]any)
			candidateLocators[fmt.Sprintf("%v|%d", loc[0], intOf(loc[1]))] = struct{}{}
		}
		allowedAnchors := map[string]struct{}{}
		for _, a := range anyToStrings(context["allowed_anchors"]) {
			allowedAnchors[a] = struct{}{}
		}
		if !anySliceEqual(candidate["source_memory_unit_ids"], context["source_memory_unit_ids"]) {
			return false
		}
		for key := range candidateLocators {
			if _, ok := allowedLocators[key]; !ok {
				return false
			}
		}
		if _, ok := allowedAnchors[candidate["identity_anchor"].(string)]; !ok {
			return false
		}
		for _, a := range candidate["context_anchors"].([]any) {
			if _, ok := allowedAnchors[a.(string)]; !ok {
				return false
			}
		}
		for _, a := range candidate["context_anchors"].([]any) {
			if a.(string) == candidate["identity_anchor"].(string) {
				return false
			}
		}
		if candidate["provenance_relation"] != context["provenance_relation"] {
			return false
		}
	}

	if lane == "perspective-expansion" {
		bridge, ok := candidate["bridge"].(map[string]any)
		if !ok || !exactKeys(bridge, []string{"source", "shared_property", "target"}) {
			return false
		}
		for _, key := range []string{"source", "shared_property", "target"} {
			if fmt.Sprintf("%v", bridge[key]) == "" && bridge[key] != nil {
				return false
			}
			if s, ok := bridge[key].(string); ok && s == "" {
				return false
			}
			if bridge[key] == nil {
				return false
			}
		}
	} else if len(candidate["bridge"].(map[string]any)) != 0 {
		return false
	}

	fixed := candidate["origin"] == "retrieved-fixed"
	fixedOK := fixed && func() bool {
		s, ok := candidate["source_starter_id"].(string)
		return ok && s != ""
	}()
	generatedOK := candidate["origin"] == "generated" && candidate["source_starter_id"] == nil
	return fixedOK || generatedOK
}

// fixedCandidateMatchesHit 对齐 Python _fixed_candidate_matches_hit（:309-327）
func fixedCandidateMatchesHit(candidate, hit map[string]any, coldStart bool) bool {
	expectedLane := "personalized-intent"
	if coldStart {
		expectedLane = "generic-intent"
	}
	expectedInterest := anyValue(hit["interest_summary_id"], coldStart)
	expectedBuckets := anyValue(hit["source_memory_unit_ids"], coldStart)
	expectedLocators := anyValue(hit["source_locators"], coldStart)
	expectedIdentity := anyValue(hit["identity_anchor"], coldStart)
	expectedContext := anyValue(hit["context_anchors"], coldStart)
	return candidate["lane"] == expectedLane &&
		candidate["origin"] == "retrieved-fixed" &&
		candidate["source_starter_id"] == hit["starter_id"] &&
		candidate["title"] == hit["text"] &&
		candidate["interest_summary_id"] == expectedInterest &&
		anySliceEqual(candidate["source_memory_unit_ids"], expectedBuckets) &&
		anySliceEqual(candidate["source_locators"], expectedLocators) &&
		candidate["identity_anchor"] == expectedIdentity &&
		anySliceEqual(candidate["context_anchors"], expectedContext) &&
		candidate["freshness_class"] == hit["freshness_class"]
}

// anyValue 对齐 Python 条件表达式：coldStart → 空值，否则取 hit 字段
func anyValue(value any, coldStart bool) any {
	if coldStart {
		switch value.(type) {
		case string:
			return ""
		case []any:
			return []any{}
		default:
			return nil
		}
	}
	return value
}

// ──────────────────────────── 通用小助手 ────────────────────────────
// （Python 侧无对应实现：仅承载 map/any 的 Go 侧形态转换）

func mapsToAny(maps []map[string]any) []any {
	if maps == nil {
		return []any{}
	}
	out := make([]any, 0, len(maps))
	for _, m := range maps {
		out = append(out, m)
	}
	return out
}

func anyToMaps(value any) []map[string]any {
	list, ok := value.([]any)
	if !ok {
		return nil
	}
	out := make([]map[string]any, 0, len(list))
	for _, item := range list {
		if m, ok := item.(map[string]any); ok {
			out = append(out, m)
		}
	}
	return out
}

// anyToLocatorSlices 提取 [str, int] 二元定位子列表
func anyToLocatorSlices(value any) [][]any {
	list, ok := value.([]any)
	if !ok {
		return nil
	}
	out := make([][]any, 0, len(list))
	for _, item := range list {
		if s, ok := item.([]any); ok {
			out = append(out, s)
		}
	}
	return out
}

func anyToStrings(value any) []string {
	list, ok := value.([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(list))
	for _, item := range list {
		if s, ok := item.(string); ok {
			out = append(out, s)
		}
	}
	return out
}

func anyValueMapOfMaps(m map[string]map[string]any) map[string]any {
	out := make(map[string]any, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

func mapKeysOfSet(set map[string]struct{}) []string {
	out := make([]string, 0, len(set))
	for k := range set {
		out = append(out, k)
	}
	return out
}

// exactKeys 对齐 Python set(x) == {…} 严格键集合比较
func exactKeys(m map[string]any, keys []string) bool {
	if len(m) != len(keys) {
		return false
	}
	set := map[string]struct{}{}
	for _, k := range keys {
		set[k] = struct{}{}
	}
	for k := range m {
		if _, ok := set[k]; !ok {
			return false
		}
	}
	return true
}

func intOf(value any) int {
	switch v := value.(type) {
	case int:
		return v
	case float64:
		return int(v)
	default:
		return 0
	}
}

func intMapsEqual(got, expected map[string]int) bool {
	if len(got) != len(expected) {
		return false
	}
	for k, v := range expected {
		if got[k] != v {
			return false
		}
	}
	return true
}

// anySliceEqual 对齐 Python == 的列表深比较（元素为 JSON 值）
func anySliceEqual(a, b any) bool {
	if a == nil && b == nil {
		return true
	}
	la, aok := a.([]any)
	lb, bok := b.([]any)
	if !aok || !bok {
		return fmt.Sprintf("%v", a) == fmt.Sprintf("%v", b)
	}
	if len(la) != len(lb) {
		return false
	}
	for i := range la {
		if !anyScalarEqual(la[i], lb[i]) {
			return false
		}
	}
	return true
}

func anyScalarEqual(a, b any) bool {
	switch av := a.(type) {
	case string:
		bv, ok := b.(string)
		return ok && av == bv
	case nil:
		return b == nil
	case bool:
		bv, ok := b.(bool)
		return ok && av == bv
	default:
		return fmt.Sprintf("%v", a) == fmt.Sprintf("%v", b)
	}
}

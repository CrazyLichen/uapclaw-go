package recommendation

// topic_pipeline_d10.go 一比一对齐上游 topic_pipeline.py:8562-8871 的
// _utility_invoker/_record_quality_decision/_score_diversity/_score_and_select/
// _start_scoring/_latest_scope_manifest_id，附 :1194-1207 的
// _offered_title_similarity 与 :397-437 的 _selection_candidates。
// 已知偏差（§5 并发映射）：asyncio.create_task/gather → goroutine+channel；
// early._Scoring 的取消语义 → 取消通道。

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"time"
)

// ──────────────────────────── 常量 ────────────────────────────

// DiversityConfigVersion 多样性 API 配置版本（topic_pipeline.py:199）。
const DiversityConfigVersion = "topic-starter-v6-diversity-api-v12"

// UtilityCandidateBatchSize 效用批大小（:204）。
const UtilityCandidateBatchSize = 3

// ──────────────────────────── 结构体 ────────────────────────────

// pipelineScoringEarly 评分并发状态（:2592-2604 _Scoring 的 Go 形态）。
type pipelineScoringEarly struct {
	plans          []map[string]any
	projected      []*SelectionCandidate
	utilityReceipt *LearnedComponentReceipt
	utilityDone    chan utilityTaskResult
	diversityDone  chan diversityTaskResult
	hasDiversity   bool
	cancelled      bool
	cancelMu       sync.Mutex
}

// Cancel 取消未完成任务。
func (e *pipelineScoringEarly) Cancel() {
	e.cancelMu.Lock()
	defer e.cancelMu.Unlock()
	if e.cancelled {
		return
	}
	e.cancelled = true
	go func() {
		if _, ok := <-e.utilityDone; !ok {
			return
		}
	}()
}

// ──────────────────────────── 导出函数 ────────────────────────────

// OfferedTitleSimilarity 标题二元组 Jaccard，忽略空白、标点与引号
// （topic_pipeline.py:1194-1207）。
func OfferedTitleSimilarity(title, offered string) float64 {
	return bigramJaccard(title, offered)
}

// SelectionCandidatesFor 已接地计划按评分器的视角；新闻计划标记为页面新闻卡
// （topic_pipeline.py:397-437 _selection_candidates）。
func SelectionCandidatesFor(plans []map[string]any, contextScope string,
	feedback *InterestFeedback, newsPlan map[string]any) []*SelectionCandidate {
	candidates := make([]*SelectionCandidate, 0, len(plans))
	for index, plan := range plans {
		bundle := payloadMapOrEmpty(plan["evidence_bundle"])
		boundary := payloadMapOrEmpty(bundle["content_boundary"])
		scoringFeatures := map[string]any{
			"title":            plan["title"],
			"starter_prompt":   pyStrOr(plan["prompt"], pyStr(plan["title"])),
			"intent":           plan["intent"],
			"persona_context":  plan["persona_context"],
			"interest_summary": plan["interest_summary"],
			"freshness_class":  plan["freshness_class"],
			"evidence_text":    boundary["text"],
		}
		if newsPlan != nil && sameMapReference(plan, newsPlan) {
			scoringFeatures["news_item"] = true
		}
		unitIDs := make([]string, 0)
		for _, unitID := range pyListStrings(plan["source_memory_unit_ids"]) {
			unitIDs = append(unitIDs, unitID)
		}
		interestID := pyStrOr(plan["interest_summary_id"], "")
		anchor := pyStrOr(plan["identity_anchor"], "")
		candidates = append(candidates, &SelectionCandidate{
			CandidateID: pyStr(plan["candidate_id"]), ContextScope: contextScope,
			Category: pyStr(plan["category"]), StableRank: index + 1,
			EvidenceEligible:  true,
			FreshnessEligible: true,
			// 与池读取方匹配的是同一文本，选中的卡即展示的卡。
			NegativePreferenceHit: !feedback.StillWanted(plan),
			InterestSummaryID:     interestID, IdentityAnchor: anchor,
			SourceMemoryUnitIDs: unitIDs, ScoringFeatures: scoringFeatures,
		})
	}
	return candidates
}

// UtilityInvoker 效用评分调用器：注入发布元数据与来源特征后分批调用
// （topic_pipeline.py:8562-8643 _utility_invoker）。
func (p *TopicPipeline) UtilityInvoker(plans []map[string]any,
	invokeStage TopicStageInvoker) ScoreInvoker {
	plansByID := map[string]map[string]any{}
	for _, plan := range plans {
		plansByID[pyStr(plan["candidate_id"])] = plan
	}
	return ScoreInvoker(func(ctx context.Context, payload map[string]any) (any, error) {
		now := p.Now()
		rawCandidates := pyListMaps(payload["candidates"])
		candidates := make([]any, 0, len(rawCandidates))
		for _, candidate := range rawCandidates {
			plan := plansByID[pyStr(candidate["candidate_id"])]
			evidence := payloadMapOrEmpty(plan["evidence_bundle"])
			retained := pythonTruthy(plan["retained_from_snapshot_id"])
			previousQuestions := sortedBySimilarity(
				pyListStrings(plan["previous_questions"]),
				pyStr(plan["title"]), retained)
			dismissedQuestions := sortedBySimilarity(
				pyListStrings(plan["dismissed_questions"]),
				pyStr(plan["title"]), retained)
			publicationMetadata := map[string]any{}
			if _, hasPublished := evidence["published_at"]; hasPublished {
				publicationMetadata["published_at"] = evidence["published_at"]
			}
			if _, hasProvenance := evidence["publication_date_provenance"]; hasProvenance {
				provenance := payloadMapOrEmpty(evidence["publication_date_provenance"])
				filtered := map[string]any{}
				for _, key := range []string{"source_type", "source_field",
					"raw_value", "listed_at", "page_published_at"} {
					if value, present := provenance[key]; present {
						filtered[key] = value
					}
				}
				publicationMetadata["publication_date_provenance"] = filtered
			}
			scoringFeatures := payloadMapOrEmpty(candidate["scoring_features"])
			mergedFeatures := map[string]any{}
			for key, value := range scoringFeatures {
				mergedFeatures[key] = value
			}
			for key, value := range publicationMetadata {
				mergedFeatures[key] = value
			}
			questionOrigin := "generated"
			if retained {
				questionOrigin = "retained"
			}
			mergedFeatures["question_origin"] = questionOrigin
			mergedFeatures["previous_questions"] = previousQuestions
			mergedFeatures["dismissed_questions"] = dismissedQuestions
			mergedCandidate := map[string]any{}
			for key, value := range candidate {
				mergedCandidate[key] = value
			}
			mergedCandidate["scoring_features"] = mergedFeatures
			candidates = append(candidates, mergedCandidate)
		}
		// 本轮候选与请求 ID 放在跨轮重复内容之后。
		shared := map[string]any{}
		for key, value := range payload {
			if key == "candidates" || key == "request_id" {
				continue
			}
			shared[key] = value
		}
		shared["local_date"] = LocalDay(now)
		shared["discussion_coverage"] = pyListAny(plans[0]["discussion_coverage"])
		currentTime := time.Unix(int64(now), 0).UTC().Format(
			"2006-01-02T15:04:05.000000+00:00")
		count := (len(candidates) + UtilityCandidateBatchSize - 1) /
			UtilityCandidateBatchSize
		if count == 0 {
			count = 1
		}
		base := len(candidates) / count
		remainder := len(candidates) % count
		type batchSlice struct{ start, end int }
		var batches [][]any
		offset := 0
		for index := 0; index < count; index++ {
			size := base
			if index < remainder {
				size++
			}
			batch := candidates[offset : offset+size]
			batches = append(batches, batch)
			offset += size
		}
		var scores []any
		var mu sync.Mutex
		var firstErr error
		var wg sync.WaitGroup
		for _, batch := range batches {
			batch := batch
			wg.Add(1)
			go func() {
				defer wg.Done()
				response, err := invokeStage.Invoke(ctx, "v6_utility_scorer",
					pipelineUtilityPrompt, compactJSON(map[string]any{
						"candidates": batch, "current_time_utc": currentTime,
						"request_id": payload["request_id"],
					}))
				if err != nil {
					mu.Lock()
					if firstErr == nil {
						firstErr = err
					}
					mu.Unlock()
					return
				}
				RecordProviderValidationTopic(invokeStage, response, "business_validated")
				ids := make([]string, 0, len(batch))
				for _, raw := range batch {
					ids = append(ids, pyStr(payloadMapOrEmpty(raw)["candidate_id"]))
				}
				if _, validateErr := UtilityScoresByID(response, ids); validateErr != nil {
					RecordProviderValidationTopic(invokeStage, response, "business_validation_failed")
					mu.Lock()
					if firstErr == nil {
						firstErr = validateErr
					}
					mu.Unlock()
					return
				}
				batchScores := pyListMaps(payloadMapOrEmpty(response)["scores"])
				mu.Lock()
				for _, score := range batchScores {
					scores = append(scores, score)
				}
				mu.Unlock()
			}()
		}
		wg.Wait()
		if firstErr != nil {
			return nil, firstErr
		}
		return map[string]any{"scores": scores}, nil
	})
}

// RecordQualityDecision 落一次质量决策事件
// （topic_pipeline.py:8645-8665）。
func (p *TopicPipeline) RecordQualityDecision(snapshot *V6ContextSnapshot,
	utility *UtilityScoringResult, utilityRequestID string,
	diversityScores []PairwiseDiversityScore, selectedCandidateIDs []string,
	failureReason string) {
	utilityPayloads := make([]any, 0, len(utility.Scores))
	for _, score := range utility.Scores {
		utilityPayloads = append(utilityPayloads, utilityScorePayload(score))
	}
	diversityPayloads := make([]any, 0, len(diversityScores))
	for _, score := range diversityScores {
		diversityPayloads = append(diversityPayloads, map[string]any{
			"left_candidate_id":  score.LeftCandidateID,
			"right_candidate_id": score.RightCandidateID,
			"diversity_score_nullable": func() any {
				if score.DiversityScoreNullable == nil {
					return nil
				}
				return *score.DiversityScoreNullable
			}(),
			"compatible": score.Compatible, "reason": score.Reason,
		})
	}
	selectedIDs := make([]any, 0, len(selectedCandidateIDs))
	for _, id := range selectedCandidateIDs {
		selectedIDs = append(selectedIDs, id)
	}
	_, _ = p.Store.RecordEvent(map[string]any{
		"session_id": "topic-starter-quality",
		"slate_id":   snapshot.InputSnapshotID,
		"topic_id":   utilityRequestID, "event_type": "quality_decision",
		"policy_id": POLICY_ID, "selection_mode": "personalized_dynamic",
		"propensity_reason": "explicit_quality_decisions",
		"created_at":        p.Now(),
		"payload": map[string]any{
			"context_scope":          snapshot.ContextScope,
			"input_snapshot_id":      snapshot.InputSnapshotID,
			"utility":                utilityPayloads,
			"diversity":              diversityPayloads,
			"selected_candidate_ids": selectedIDs,
			"failure_reason":         failureReason,
		},
	})
}

// ScoreDiversityFor 多样性评分（topic_pipeline.py:8667-8696）。
func (p *TopicPipeline) ScoreDiversityFor(candidates []*SelectionCandidate,
	snapshot *V6ContextSnapshot, config *ArkBackgroundConfig,
	invokeStage TopicStageInvoker,
	discussionCoverage []map[string]any) (*DiversityScoringResult, error) {
	providerVersion := pyStrOr(config.ExpectedModel, config.Model)
	binding, err := PipelineAPIBinding(DIVERSITY_COMPONENT_ID, "ark",
		providerVersion, DiversityConfigVersion)
	if err != nil {
		return nil, err
	}
	receipt, err := PipelineSuccessReceipt(binding, snapshot.InputSnapshotID,
		fmt.Sprintf("v6-diversity-%s", newUUIDHex()))
	if err != nil {
		return nil, err
	}
	invokeDiversity := ScoreInvoker(func(ctx context.Context, payload map[string]any) (any, error) {
		merged := map[string]any{}
		for key, value := range payload {
			merged[key] = value
		}
		coverage := make([]any, 0, len(discussionCoverage))
		for _, item := range discussionCoverage {
			coverage = append(coverage, item)
		}
		merged["discussion_coverage"] = coverage
		// 批代理按接口收 invoke：catalog 注入对一切实现一致（无 fallback 旁路）。
		return InvokeBatchedDiversityProxy(ctx, merged, invokeStage)
	})
	return ScoreDiversity(candidates, invokeDiversity, receipt)
}

// ScoreAndSelect 选池：常规卡走列表搜索，新闻卡只看效用
// （topic_pipeline.py:8698-8814 _score_and_select）。新闻候选只在效用的
// 完整与当前意愿步上通过或失败；多样性与列表搜索都不见它。
func (p *TopicPipeline) ScoreAndSelect(plans []map[string]any,
	snapshot *V6ContextSnapshot, feedback *InterestFeedback,
	config *ArkBackgroundConfig, invokeStage TopicStageInvoker,
	requiredFirstWindowInterestSummaryIDs []string,
	early *pipelineScoringEarly) (*ListSearchResult, string, string, error) {
	var newsPlan map[string]any
	for _, plan := range plans {
		if pythonTruthy(plan["news_match"]) {
			newsPlan = plan
			break
		}
	}
	newsID := ""
	if newsPlan != nil {
		newsID = pyStr(newsPlan["candidate_id"])
	}
	var scoring *pipelineScoringEarly
	if early != nil && samePlans(early.plans, plans) {
		scoring = early
	} else {
		if early != nil {
			early.Cancel()
		}
		scoring = p.StartScoring(plans, snapshot, feedback, config, invokeStage)
	}
	projected, utilityReceipt := scoring.projected, scoring.utilityReceipt
	var regular []*SelectionCandidate
	for _, candidate := range projected {
		if candidate.CandidateID != newsID {
			regular = append(regular, candidate)
		}
	}
	utilityResult := <-scoring.utilityDone
	var diversityResult diversityTaskResult
	if scoring.hasDiversity {
		diversityResult = <-scoring.diversityDone
	}
	if utilityResult.err != nil {
		return nil, "", "", utilityResult.err
	}
	utility, utilityOK := utilityResult.value.(*UtilityScoringResult)
	if !utilityOK {
		return nil, "", "", fmt.Errorf("utility scoring returned invalid result")
	}
	var newsRejection string
	if newsPlan != nil {
		if !feedback.StillWanted(newsPlan) {
			newsRejection = "news_dismissed"
		} else if !utilityAdmitted(utility, newsID) {
			newsRejection = "news_utility_rejected"
		}
	}
	newsLeadID := ""
	if newsPlan != nil && newsRejection == "" {
		newsLeadID = newsID
	}
	if len(regular) == 0 {
		failureReason := "no_feasible_complete_list"
		if newsLeadID != "" {
			failureReason = ""
		}
		p.RecordQualityDecision(snapshot, utility, utilityReceipt.RequestID,
			nil, nil, failureReason)
		if newsLeadID == "" {
			return nil, "", "", &TerminalGenerationRejection{Reason: "no_feasible_complete_list"}
		}
		result, err := NewsOnlySearchResult(utility, snapshot.ContextScope)
		if err != nil {
			return nil, "", "", err
		}
		return result, newsLeadID, newsRejection, nil
	}
	if diversityResult.err != nil {
		return nil, "", "", diversityResult.err
	}
	diversity := diversityResult.value
	leadingCards := 0
	if newsLeadID != "" {
		leadingCards = 1
	}
	regularUtility := utility
	if newsID != "" {
		filtered := make([]UtilityScore, 0, len(utility.Scores))
		for _, score := range utility.Scores {
			if score.CandidateID != newsID {
				filtered = append(filtered, score)
			}
		}
		regularUtility = &UtilityScoringResult{Scores: filtered,
			Receipt: utility.Receipt}
	}
	result, searchErr := HybridListSearch(regular, regularUtility, diversity,
		&ListSearchConstraints{
			ContextScope:                          snapshot.ContextScope,
			RequiredFirstWindowInterestSummaryIDs: requiredFirstWindowInterestSummaryIDs,
			LeadingCards:                          leadingCards,
		})
	if searchErr != nil {
		return nil, "", "", searchErr
	}
	if len(requiredFirstWindowInterestSummaryIDs) > 0 &&
		(result.FailureReason != "" || len(result.PreferredList) == 0) {
		// 最新问题的卡没有活过评分；不带它重搜列表而不扣住池。
		result, searchErr = HybridListSearch(regular, regularUtility, diversity,
			&ListSearchConstraints{
				ContextScope: snapshot.ContextScope, LeadingCards: leadingCards,
			})
		if searchErr != nil {
			return nil, "", "", searchErr
		}
	}
	p.RecordQualityDecision(snapshot, utility, utilityReceipt.RequestID,
		diversity.PairwiseScores, result.PreferredList, result.FailureReason)
	// 只有效用未纳入任何常规卡时，被纳入的新闻卡才单独成立。
	rejected := (result.FailureReason != "" || len(result.PreferredList) == 0) &&
		(newsLeadID == "" || result.FailureReason != "quality_abstained")
	if rejected {
		reason := result.FailureReason
		if reason == "" {
			reason = "no_preferred_list"
		}
		return nil, "", "", &TerminalGenerationRejection{Reason: reason}
	}
	return result, newsLeadID, newsRejection, nil
}

// StartScoring 全部计划起效用，常规计划起多样性；新闻卡无多样性
// （topic_pipeline.py:8816-8856）。
func (p *TopicPipeline) StartScoring(plans []map[string]any,
	snapshot *V6ContextSnapshot, feedback *InterestFeedback,
	config *ArkBackgroundConfig, invokeStage TopicStageInvoker) *pipelineScoringEarly {
	var newsPlan map[string]any
	for _, plan := range plans {
		if pythonTruthy(plan["news_match"]) {
			newsPlan = plan
			break
		}
	}
	projected := SelectionCandidatesFor(plans, snapshot.ContextScope, feedback, newsPlan)
	var regular []*SelectionCandidate
	for _, candidate := range projected {
		if newsPlan == nil || candidate.CandidateID != pyStr(newsPlan["candidate_id"]) {
			regular = append(regular, candidate)
		}
	}
	utilityReceipt, err := PipelineSuccessReceipt(UtilityBinding(config),
		snapshot.InputSnapshotID, fmt.Sprintf("v6-utility-%s", newUUIDHex()))
	if err != nil {
		utilityReceipt = &LearnedComponentReceipt{}
	}
	scoring := &pipelineScoringEarly{
		plans: append([]map[string]any(nil), plans...), projected: projected,
		utilityReceipt: utilityReceipt,
		utilityDone:    make(chan utilityTaskResult, 1),
	}
	invoker := ScoreInvoker(func(ctx context.Context, payload map[string]any) (any, error) {
		return p.UtilityInvoker(plans, invokeStage)(ctx, payload)
	})
	go func() {
		result, err := ScoreInteractionUtility(snapshot.ProfileProjection,
			projected, invoker, utilityReceipt)
		scoring.utilityDone <- utilityTaskResult{value: result, err: err}
		close(scoring.utilityDone)
	}()
	if len(regular) > 0 {
		scoring.hasDiversity = true
		scoring.diversityDone = make(chan diversityTaskResult, 1)
		go func() {
			result, err := p.ScoreDiversityFor(regular, snapshot, config,
				invokeStage, pyListMaps(plans[0]["discussion_coverage"]))
			scoring.diversityDone <- diversityTaskResult{value: result, err: err}
			close(scoring.diversityDone)
		}()
	}
	return scoring
}

// LatestScopeManifestID scope 的最新 manifest ID
// （topic_pipeline.py:8858-8870）。
func (p *TopicPipeline) LatestScopeManifestID(contextScope string) string {
	rows, err := p.Store.QueryEvents(QueryEventsOptions{
		EventType:     "starter_slate_manifest",
		PolicyID:      stringPtr(POLICY_ID),
		PayloadEquals: map[string]any{"context_scope": contextScope},
		Limit:         1,
		NewestFirst:   true,
	})
	if err != nil || len(rows) == 0 {
		return ""
	}
	payload := payloadMapOrEmpty(rows[0]["payload"])
	if pyStr(payload["manifest_id"]) == "" {
		return ""
	}
	return pyStr(payload["manifest_id"])
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// sortedBySimilarity 按与标题的相似度降序；携带卡排除自身标题
// （对齐 :8576-8582）。
func sortedBySimilarity(titles []string, anchor string, retained bool) []any {
	filtered := []string{}
	for _, title := range titles {
		if retained && title == anchor {
			continue
		}
		filtered = append(filtered, title)
	}
	sort.SliceStable(filtered, func(i, j int) bool {
		return OfferedTitleSimilarity(anchor, filtered[i]) >
			OfferedTitleSimilarity(anchor, filtered[j])
	})
	out := make([]any, 0, len(filtered))
	for _, title := range filtered {
		out = append(out, title)
	}
	return out
}

// samePlans 计划列表同体。
func samePlans(a, b []map[string]any) bool {
	if len(a) != len(b) {
		return false
	}
	for index := range a {
		if !sameMapReference(a[index], b[index]) {
			return false
		}
	}
	return true
}

// utilityAdmitted 效用是否纳入该候选。
func utilityAdmitted(utility *UtilityScoringResult, candidateID string) bool {
	for _, score := range utility.Scores {
		if score.CandidateID == candidateID {
			return score.Admitted
		}
	}
	return false
}

// utilityScorePayload 效用分载荷。
func utilityScorePayload(score UtilityScore) map[string]any {
	payload := map[string]any{
		"candidate_id": score.CandidateID, "admitted": score.Admitted,
		"reason": score.Reason,
	}
	if score.ClickScoreNullable != nil {
		payload["click_score_nullable"] = *score.ClickScoreNullable
	} else {
		payload["click_score_nullable"] = nil
	}
	if score.DepthScoreNullable != nil {
		payload["depth_score_nullable"] = *score.DepthScoreNullable
	} else {
		payload["depth_score_nullable"] = nil
	}
	if score.CombinedUtilityNullable != nil {
		payload["combined_utility_nullable"] = *score.CombinedUtilityNullable
	} else {
		payload["combined_utility_nullable"] = nil
	}
	return payload
}

package recommendation

// topic_pipeline_d7.go 一比一对齐上游 topic_pipeline.py:6345-7097 的
// _offered_questions/_global_candidate_plans/_retained_candidate_plans/
// _retainable/_retained_continuation_plans/_validate_global_interest_candidates/
// _validate_recent_candidates。
// 已知偏差（§5 并发映射）：asyncio.ensure_future+gather → goroutine+channel；
// OptionalLanes 的 done channel/cancel 与 c2 契约一致。
// 已知偏差：_offered_questions 的项目标识符 Go ProjectRecord 无 name 字段
// （同 c7 登记 ⤴️项目域补字段回填）。

import (
	"context"
	"fmt"
	"sort"
	"strings"

	lifecycle "github.com/uapclaw/uapclaw-go/internal/swarm/runtime/session/lifecycle"

	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 常量 ────────────────────────────

// recentCategories 近期类目集（topic_pipeline.py:232）。
var recentCategories = map[string]bool{
	"profile_recent_progress": true, "project_domain_progress": true,
}

// retainedMinimumRemainingSeconds 携带卡的最短剩余寿命（:212 继承）。
var retainedMinimumRemainingSeconds = V6BackgroundGenerationDeadlineSeconds

// V6BackgroundGenerationDeadlineSeconds 生成时限（topic_service.py:96）。
const V6BackgroundGenerationDeadlineSeconds = 300.0

// ──────────────────────────── 导出函数 ────────────────────────────

// OfferedQuestions 实际曝光供对比；全局对比可含去标识项目标题
// （topic_pipeline.py:6345-6412 _offered_questions）。
func (p *TopicPipeline) OfferedQuestions(snapshot *V6ContextSnapshot,
	includeOtherScopes bool) []map[string]any {
	type questionKey struct {
		title       string
		sourceURL   any
		publishedAt any
	}
	questions := map[questionKey]map[string]any{}
	crossScope := includeOtherScopes && snapshot.ContextScope == GlobalNewChatScope
	projects := map[string]lifecycle.ProjectRecord{}
	if crossScope {
		for _, project := range lifecycle.ListProjects() {
			projects[project.ProjectID] = project
		}
	}
	offset := 0
	cutoff := p.Now() - 90*86400
	for {
		payloadEquals := map[string]any{"context_scope": snapshot.ContextScope}
		if crossScope {
			payloadEquals = nil
		}
		exposures, err := p.Store.QueryEvents(QueryEventsOptions{
			EventType:     "dynamic_exposure",
			PolicyID:      stringPtr(POLICY_ID),
			PayloadEquals: payloadEquals,
			Limit:         100,
			Offset:        offset,
			NewestFirst:   true,
		})
		if err != nil {
			break
		}
		for _, exposure := range exposures {
			if toFloatOr(exposure["created_at"], 0) < cutoff {
				continue
			}
			payload := payloadMapOrEmpty(exposure["payload"])
			shown := map[string]bool{}
			for _, revision := range pyListStrings(payload["card_revision_ids"]) {
				shown[revision] = true
			}
			manifestID := pyStrOr(payload["manifest_id"], pyStr(exposure["topic_id"]))
			cards, _ := p.Store.QueryEvents(QueryEventsOptions{
				EventType: "starter_card",
				SlateID:   stringPtr(manifestID),
				PolicyID:  stringPtr(POLICY_ID),
				Limit:     7,
			})
			for _, row := range cards {
				card := payloadMapOrEmpty(row["payload"])
				revision := pyStr(card["card_revision_id"])
				if !shown[revision] {
					continue
				}
				if pyStr(card["context_scope"]) != pyStr(payload["context_scope"]) {
					continue
				}
				if pythonTruthy(card["news_match"]) {
					continue
				}
				title := pyStr(card["title"])
				otherScope := pyStr(card["context_scope"]) != snapshot.ContextScope
				if otherScope {
					scope := pyStr(card["context_scope"])
					projectID, _ := ProjectIDForScope(scope)
					if projectID == "" {
						projectID = pyStrOr(card["project_id"], "")
					}
					project := projects[projectID]
					identifiers := []string{projectID,
						// 已知偏差：Go ProjectRecord 无 name 字段
						"", project.ProjectDir}
					for _, locator := range pyListAny(card["source_locators"]) {
						if locatorList, ok := locator.([]any); ok && len(locatorList) > 0 {
							identifiers = append(identifiers, pyStr(locatorList[0]))
						} else if locatorMap, ok := locator.(map[string]any); ok {
							identifiers = append(identifiers, pyStr(locatorMap["session_id"]))
						}
					}
					var nonEmpty []string
					for _, identifier := range identifiers {
						if identifier != "" {
							nonEmpty = append(nonEmpty, identifier)
						}
					}
					title = WithoutIdentifiers(title, nonEmpty)
					if title == "" {
						continue
					}
				}
				evidence := map[string]any{}
				if !otherScope && recentCategories[pyStr(card["category"])] {
					if bundle, ok := card["evidence_bundle"].(map[string]any); ok {
						evidence = bundle
					}
				}
				boundary := payloadMapOrEmpty(evidence["content_boundary"])
				key := questionKey{title, evidence["source_url"], evidence["published_at"]}
				questions[key] = map[string]any{
					"title": title, "source_url": evidence["source_url"],
					"published_at":    evidence["published_at"],
					"covered_passage": boundary["text"],
				}
			}
		}
		if len(exposures) < 100 ||
			toFloatOr(exposures[len(exposures)-1]["created_at"], 0) < cutoff {
			result := make([]map[string]any, 0, len(questions))
			for _, question := range questions {
				result = append(result, question)
			}
			return result
		}
		offset += len(exposures)
	}
	result := make([]map[string]any, 0, len(questions))
	for _, question := range questions {
		result = append(result, question)
	}
	return result
}

// GlobalCandidatePlans 全局候选计划：近期槽与兴趣槽并行生成
// （topic_pipeline.py:6414-6692 _global_candidate_plans）。
// 返回 (wanted plans, interest_selection)。
func (p *TopicPipeline) GlobalCandidatePlans(snapshot *V6ContextSnapshot,
	rid *RIDResult, summaries []*SessionSummaryArtifact, feedback *InterestFeedback,
	config *ArkBackgroundConfig, invokeStage TopicStageInvoker,
	offeredTitles map[string][]string, carriedOver int,
	offeredQuestions []map[string]any, newsLead bool, lanes *OptionalLanes,
	onRecentPlans func([]map[string]any)) ([]map[string]any, map[string]any, error) {
	if lanes == nil {
		lanes = NewOptionalLanes()
	}
	// 新闻卡领衔池但在这些生成器填的列表之外。
	listSize := GLOBAL_POOL_TARGET_SIZE
	if newsLead {
		listSize--
	}
	summariesByID := InterestSummaryTextByID(snapshot, summaries)
	accepted := rid.TriggerInterests()
	if len(accepted) == 0 {
		return nil, nil, &TerminalGenerationRejection{Reason: "no_selected_trigger_interest"}
	}
	shortDeltaEventIDs := ShortTriggerNaturalDeltaEventIDs(snapshot)
	deltaEventIDs := map[string]bool{}
	if len(shortDeltaEventIDs) > 0 {
		for id := range shortDeltaEventIDs {
			deltaEventIDs[id] = true
		}
	} else {
		for _, id := range snapshot.DeltaEventIDs {
			deltaEventIDs[id] = true
		}
	}
	// 换手窗口也点名更早的问题；触发槽给持有最新问题的兴趣，并列取更高秩。
	observedAt := map[string]float64{}
	for _, message := range snapshot.Messages {
		observedAt[message.EventID] = message.ObservedAt
	}
	var deltaLinked *RIDInterestDecision
	var deltaLinkedAt = -1.0
	for _, item := range accepted {
		overlap := false
		latest := 0.0
		for _, eventID := range item.SourceEventIDs {
			if deltaEventIDs[eventID] {
				overlap = true
				if observedAt[eventID] > latest {
					latest = observedAt[eventID]
				}
			}
		}
		if overlap && latest > deltaLinkedAt {
			deltaLinked = item
			deltaLinkedAt = latest
		}
	}
	selectedInterests := accepted
	ranked := make([]d7RankedInterest, 0, len(selectedInterests))
	for rank, item := range selectedInterests {
		ranked = append(ranked, d7RankedInterest{
			id: item.InterestSummaryID, summary: summariesByID[item.InterestSummaryID],
			rank: rank + 1,
		})
	}
	selectedIDs := make([]any, 0, len(selectedInterests))
	originalRanks := map[string]any{}
	for _, item := range ranked {
		selectedIDs = append(selectedIDs, item.id)
		originalRanks[item.id] = item.rank
	}
	var deltaLinkedID any
	if deltaLinked != nil {
		deltaLinkedID = deltaLinked.InterestSummaryID
	}
	sortedDelta := make([]string, 0, len(shortDeltaEventIDs))
	for id := range shortDeltaEventIDs {
		sortedDelta = append(sortedDelta, id)
	}
	sort.Strings(sortedDelta)
	deltaIDList := make([]any, 0, len(sortedDelta))
	for _, id := range sortedDelta {
		deltaIDList = append(deltaIDList, id)
	}
	interestSelection := map[string]any{
		"policy":                           "all_rid_selected_v1",
		"selected_interest_summary_ids":    selectedIDs,
		"original_rid_ranks":               originalRanks,
		"delta_linked_interest_summary_id": deltaLinkedID,
		"short_trigger_delta_event_ids":    deltaIDList,
	}
	dismissedTopics := InterestsBehindDismissals(feedback, accepted)
	var slotRanked []d7RankedInterest
	for _, item := range ranked {
		if !dismissedTopics[item.id] {
			slotRanked = append(slotRanked, item)
		}
	}
	recentCount := len(slotRanked)
	if recentCount > 3 {
		recentCount = 3
	}
	var recentInterestSlots []d7RankedInterest
	for _, item := range slotRanked[:recentCount] {
		recentInterestSlots = append(recentInterestSlots, d7RankedInterest{
			id: item.id, rank: item.rank})
	}
	interestSelection["extra_recent_slot_interest_summary_id"] = nil
	if deltaLinked != nil {
		inRecent := false
		for _, item := range slotRanked[:recentCount] {
			if item.id == deltaLinked.InterestSummaryID {
				inRecent = true
			}
		}
		if !inRecent {
			var deltaRanked *d7RankedInterest
			for index := range slotRanked {
				if slotRanked[index].id == deltaLinked.InterestSummaryID {
					deltaRanked = &slotRanked[index]
					break
				}
			}
			if deltaRanked != nil {
				recentInterestSlots = append(recentInterestSlots, d7RankedInterest{
					id: deltaRanked.id, rank: deltaRanked.rank})
				// 该槽即使不出卡也保留：最新问题缺近期卡可与从未得到的槽区分。
				interestSelection["extra_recent_slot_interest_summary_id"] = deltaRanked.id
			}
		}
	}
	var recentSlotInput []RankedInterest
	for _, item := range recentInterestSlots {
		recentSlotInput = append(recentSlotInput, RankedInterest{
			InterestSummaryID: item.id, Rank: item.rank})
	}
	recentCandidateSlots := CandidateSlots(recentSlotInput)
	// 已持有最新问题的 Memory 单元即该问题的主题，故只让问题保留槽，
	// 得到的卡携带触发的血缘。
	var unitsHoldingQuestion map[string]bool
	if deltaLinked != nil && !dismissedTopics[deltaLinked.InterestSummaryID] {
		events := map[string]bool{}
		for _, message := range snapshot.Messages {
			if shortDeltaEventIDs[message.EventID] {
				events[MemoryEvidenceID(message.SessionID, message.EventID)] = true
			}
		}
		var eventList []string
		for event := range events {
			eventList = append(eventList, event)
		}
		unitsHoldingQuestion = feedback.UnitsSharing(eventList)
	} else {
		unitsHoldingQuestion = map[string]bool{}
	}
	var ordinaryInterests []d7RankedInterest
	for index, item := range ranked {
		decision := selectedInterests[index]
		held := false
		for _, unitID := range decision.SourceProfileUnitIDs {
			if unitsHoldingQuestion[unitID] {
				held = true
				break
			}
		}
		if held || dismissedTopics[item.id] {
			continue
		}
		ordinaryInterests = append(ordinaryInterests, item)
	}
	stillOpen := listSize - carriedOver
	if stillOpen < 0 {
		stillOpen = 0
	}
	var interestSlotInterests []d7RankedInterest
	if len(ordinaryInterests) > 0 {
		count := len(ordinaryInterests)
		if stillOpen > count {
			count = stillOpen
		}
		for index := 0; index < count; index++ {
			item := ordinaryInterests[index%len(ordinaryInterests)]
			interestSlotInterests = append(interestSlotInterests, d7RankedInterest{
				id: item.id, rank: item.rank})
		}
	}
	var candidateInput []RankedInterest
	for _, item := range interestSlotInterests {
		candidateInput = append(candidateInput, RankedInterest{
			InterestSummaryID: item.id, Rank: item.rank})
	}
	interestCandidateSlots := CandidateSlots(candidateInput)

	sourceProfileUnitsByInterest := map[string][]string{}
	for _, item := range selectedInterests {
		sourceProfileUnitsByInterest[item.InterestSummaryID] = item.SourceProfileUnitIDs
	}
	if offeredQuestions == nil {
		offeredQuestions = p.OfferedQuestions(snapshot, true)
	}
	searchOfferedQuestions := p.OfferedQuestions(snapshot, false)

	rankedPayloads := make([]any, 0, len(ranked))
	for _, item := range ranked {
		rankedPayloads = append(rankedPayloads, map[string]any{
			"interest_summary_id": item.id, "interest_summary": item.summary,
			"rank": item.rank,
		})
	}
	recentRequestID := "v6-recent-" + newUUIDHex()
	recentPayload := map[string]any{
		"input_snapshot_id":    snapshot.InputSnapshotID,
		"local_date":           localDateString(p.Now()),
		"profile_projection":   snapshot.ProfileProjection,
		"ranked_interests":     rankedPayloads,
		"required_category":    "profile_recent_progress",
		"previously_offered":   searchOfferedQuestions,
		"discussion_coverage":  DiscussionCoverage(summaries, nil),
		"negative_preferences": feedback.ModelPayloads(true),
		"direction_feedback":   feedback.DirectionPayloads(),
		"candidate_slots":      recentCandidateSlots,
		"request_id":           recentRequestID,
	}
	// 近期槽异步跑：验证到达的近期计划并移交仍被想要的。
	type recentOutcome struct {
		plans []map[string]any
		err   error
	}
	recentDone := make(chan recentOutcome, 1)
	recentCtx, recentCancel := context.WithCancel(context.Background())
	go func() {
		defer close(recentDone)
		response, err := invokeStage.Invoke(recentCtx, "v6_recent_candidate_generator",
			pipelineRecentPrompt, compactJSON(recentPayload))
		if err != nil {
			recentDone <- recentOutcome{err: err}
			return
		}
		RecordProviderValidationTopic(invokeStage, response, "business_validated")
		plans, err := ValidateRecentCandidates(response, d7ToRankedInterest(ranked), summariesByID,
			sourceProfileUnitsByInterest, snapshot.ContextScope,
			"profile_recent_progress", 0, recentCandidateSlots)
		if err != nil {
			RecordProviderValidationTopic(invokeStage, response, "business_validation_failed")
			recentDone <- recentOutcome{err: err}
			return
		}
		for _, plan := range plans {
			plan["previously_offered"] = offeredQuestions
		}
		if onRecentPlans != nil {
			var wanted []map[string]any
			for _, plan := range plans {
				if feedback.StillWanted(plan) {
					wanted = append(wanted, plan)
				}
			}
			onRecentPlans(wanted)
		}
		recentDone <- recentOutcome{plans: plans}
	}()

	// 从否定回来的方向以 "reduced" 到达生成器：宿主保留其计数与天数，
	// 其卡排在其余之后。
	interestLineage := map[string][2]map[string]bool{}
	for _, decision := range selectedInterests {
		unitSet := map[string]bool{}
		for _, unitID := range decision.SourceProfileUnitIDs {
			unitSet[unitID] = true
		}
		evidenceSet := map[string]bool{}
		for _, locator := range decision.SourceLocators {
			evidenceSet[MemoryEvidenceID(locator.SessionID, locator.EventID)] = true
		}
		interestLineage[decision.InterestSummaryID] = [2]map[string]bool{unitSet, evidenceSet}
	}
	rankedWithStatus := make([]any, 0, len(ranked))
	for _, item := range ranked {
		status := "available"
		lineage := interestLineage[item.id]
		unitIDs := setKeys(lineage[0])
		events := setKeys(lineage[1])
		if feedback.StatusOf(unitIDs, events, "") == "reduced" {
			status = "reduced"
		}
		rankedWithStatus = append(rankedWithStatus, map[string]any{
			"interest_summary_id": item.id, "interest_summary": item.summary,
			"rank": item.rank, "feedback_status": status,
		})
	}
	alreadyOffered := make([]any, 0, len(offeredTitles))
	for _, item := range ranked {
		titles, present := offeredTitles[item.id]
		if !present {
			continue
		}
		titleList := make([]any, 0, len(titles))
		for _, title := range titles {
			titleList = append(titleList, title)
		}
		alreadyOffered = append(alreadyOffered, map[string]any{
			"interest_summary_id": item.id, "titles": titleList})
	}
	var dismissedQuestions []any
	for _, title := range feedback.DismissedCardTitles() {
		dismissedQuestions = append(dismissedQuestions, title)
	}
	otherScopeCoverage := snapshot.OtherScopeDiscussionCoverage
	interestRequestID := "v6-global-" + newUUIDHex()
	interestPayload := map[string]any{
		"input_snapshot_id":    snapshot.InputSnapshotID,
		"profile_projection":   snapshot.ProfileProjection,
		"ranked_interests":     rankedWithStatus,
		"discussion_coverage":  DiscussionCoverage(summaries, otherScopeCoverage),
		"negative_preferences": feedback.ModelPayloads(true),
		"direction_feedback":   feedback.DirectionPayloads(),
		"dismissed_questions":  dismissedQuestions,
		// 只展示过的问题不进来：列在这里会被复制回来；宿主的复述规则与
		// 效用判定它们。
		"already_offered": alreadyOffered,
		"page_demand": map[string]any{
			"pool_size": listSize, "carried_over": carriedOver, "still_open": stillOpen,
		},
		"candidate_slots": interestCandidateSlots,
		"request_id":      interestRequestID,
	}
	interestResponse, interestErr := invokeStage.Invoke(context.Background(),
		"v6_global_interest_generator", pipelineGlobalInterestPrompt,
		compactJSON(interestPayload))
	var generated []map[string]any
	var generationErr error
	if interestErr != nil {
		generationErr = interestErr
	} else {
		RecordProviderValidationTopic(invokeStage, interestResponse, "business_validated")
		generated, generationErr = ValidateGlobalInterestCandidates(interestResponse,
			snapshot, d7ToRankedInterest(ranked), summariesByID,
			sourceProfileUnitsByInterest, interestCandidateSlots, p.Now())
		if generationErr != nil {
			RecordProviderValidationTopic(invokeStage, interestResponse, "business_validation_failed")
		}
	}
	lanes.MandatoryReady()
	var recent []map[string]any
	if generationErr == nil {
		outcome, ok := <-recentDone
		if !ok || outcome.err != nil {
			reason := "provider_invocation_failed"
			if rejection, isRejection := outcome.err.(*TerminalGenerationRejection); isRejection {
				reason = rejection.Reason
			}
			logger.Warn(logger.ComponentAgentCore).
				Msg("Topic Starter v6 optional global recent generation failed; retaining interest candidates")
			lanes.RecordFailed(PipelineGlobalRecentLane, reason)
		} else {
			recent = outcome.plans
		}
	}
	recentCancel()

	// 池读取方会因在力否定丢弃的计划在接地与评分之前就离开。
	var wanted, withheld []map[string]any
	for _, plan := range append(append([]map[string]any(nil), recent...), generated...) {
		if feedback.StillWanted(plan) {
			wanted = append(wanted, plan)
		} else {
			withheld = append(withheld, plan)
		}
	}
	withheldPayloads := make([]any, 0, len(withheld))
	for _, plan := range withheld {
		withheldPayloads = append(withheldPayloads, map[string]any{
			"candidate_id":        pyStr(plan["candidate_id"]),
			"interest_summary_id": pyStrOr(plan["interest_summary_id"], ""),
			"direction":           pyStrOr(plan["direction"], ""),
			"title":               pyStr(plan["title"]),
		})
	}
	interestSelection["direction_withheld_candidates"] = withheldPayloads
	// 复述本 scope 已有标题或已否定问题的生成计划同样离开。
	var offered []string
	for _, titles := range offeredTitles {
		offered = append(offered, titles...)
	}
	for _, question := range offeredQuestions {
		offered = append(offered, pyStr(question["title"]))
	}
	offered = append(offered, feedback.DismissedCardTitles()...)
	// 复述了本 scope 已有标题或已否定问题的生成计划离开 wanted；
	// 记录只收真正被移除的（对齐 :6686-6691 的 if plan in wanted 分支）。
	var restated []any
	for _, item := range RestatingPlans(generated, offered) {
		for index, plan := range wanted {
			if sameMapReference(plan, item.Plan) {
				wanted = append(wanted[:index], wanted[index+1:]...)
				restated = append(restated, item.Detail)
				break
			}
		}
	}
	interestSelection["restated_offered_candidates"] = restated
	return wanted, interestSelection, generationErr
}

// RetainedCandidatePlans 上一池未打开的卡进入本轮列表选择
// （topic_pipeline.py:6694-6798）。只有生成的兴趣 lane 旅行：其证据是一个
// 释放的画像兴趣，本轮按自己的快照重发并按当日讨论覆盖重评。
func (p *TopicPipeline) RetainedCandidatePlans(snapshot *V6ContextSnapshot,
	rid *RIDResult, feedback *InterestFeedback, triggerKey map[string]any,
	summariesByID map[string]string, now float64) []map[string]any {
	retained, eligibleInterestIDs := p.Retainable(snapshot, rid, feedback,
		triggerKey, now)
	if len(retained.Cards) == 0 {
		return nil
	}
	shortDeltaEventIDs := ShortTriggerNaturalDeltaEventIDs(snapshot)
	if len(shortDeltaEventIDs) > 0 {
		for _, interest := range rid.TriggerInterests() {
			held := false
			for _, eventID := range interest.SourceEventIDs {
				if shortDeltaEventIDs[eventID] {
					held = true
					break
				}
			}
			if held {
				delete(eligibleInterestIDs, interest.InterestSummaryID)
			}
		}
	}
	releasedForUnits := map[string]string{}
	for _, interest := range rid.TriggerInterests() {
		if len(interest.SourceProfileUnitIDs) == 0 {
			continue
		}
		interestID := interest.InterestSummaryID
		if _, eligible := eligibleInterestIDs[interestID]; !eligible {
			continue
		}
		releasedForUnits[strings.Join(interest.SourceProfileUnitIDs, "\x00")] = interestID
	}
	unreleasedInterestIDs := map[string]bool{}
	for _, interest := range rid.RejectedInterests() {
		if !InterestsBehindDismissals(feedback, rid.RejectedInterests())[interest.InterestSummaryID] {
			unreleasedInterestIDs[interest.InterestSummaryID] = true
		}
	}
	unreleasedForUnits := map[string]string{}
	for _, interest := range rid.RejectedInterests() {
		if len(interest.SourceProfileUnitIDs) == 0 {
			continue
		}
		interestID := interest.InterestSummaryID
		if !unreleasedInterestIDs[interestID] {
			continue
		}
		unreleasedForUnits[strings.Join(interest.SourceProfileUnitIDs, "\x00")] = interestID
	}
	var plans []map[string]any
	for _, card := range retained.Cards {
		interestID := pyStr(card["interest_summary_id"])
		unitIDList := pyListStrings(card["source_memory_unit_ids"])
		if _, eligible := eligibleInterestIDs[interestID]; !eligible {
			unitKey := strings.Join(unitIDList, "\x00")
			if len(unitIDList) == 0 {
				events := retained.InterestEvents[interestID]
				var candidates [][]string
				for joined := range releasedForUnits {
					candidates = append(candidates, strings.Split(joined, "\x00"))
				}
				unitIDList = feedback.HoldingUnits(events, candidates)
				unitKey = strings.Join(unitIDList, "\x00")
			}
			if mapped, present := releasedForUnits[unitKey]; present {
				interestID = mapped
			}
		}
		if _, eligible := eligibleInterestIDs[interestID]; !eligible {
			if mapped, present := unreleasedForUnits[strings.Join(unitIDList, "\x00")]; present {
				interestID = mapped
			}
		}
		summary := summariesByID[interestID]
		expireTime := toFloatOr(card["expire_time"], 0)
		if pyStr(card["category"]) != ProfileInterestCategory {
			continue
		}
		_, eligible := eligibleInterestIDs[interestID]
		if !eligible && !unreleasedInterestIDs[interestID] {
			continue
		}
		if summary == "" {
			continue
		}
		if expireTime-now < retainedMinimumRemainingSeconds {
			continue
		}
		plan := ProfileInterestPlan(pyStr(card["card_id"]), interestID, summary,
			pyStr(card["intent"]), pyStr(card["title"]), pyStr(card["prompt"]),
			pyStrOr(card["direction"], ""), unitIDList, snapshot.ContextScope,
			snapshot.InputSnapshotID, now)
		plans = append(plans, CarriedPlan(plan, card))
	}
	return plans
}

// Retainable 上一池的未打开卡与本轮仍释放的兴趣
// （topic_pipeline.py:6800-6824）。
func (p *TopicPipeline) Retainable(snapshot *V6ContextSnapshot, rid *RIDResult,
	feedback *InterestFeedback, triggerKey map[string]any,
	now float64) (RetainablePool, map[string]bool) {
	retained := RetainablePoolFor(p.Store,
		BuildServingCompatibilityKey(triggerKey), snapshot.ContextScope,
		snapshot.ProjectID, feedback, now)
	if len(retained.Cards) == 0 {
		return retained, map[string]bool{}
	}
	eligible := map[string]bool{}
	for _, interest := range rid.TriggerInterests() {
		eligible[interest.InterestSummaryID] = true
	}
	for id := range InterestsBehindDismissals(feedback, rid.TriggerInterests()) {
		delete(eligible, id)
	}
	return retained, eligible
}

// RetainedContinuationPlans 上一项目池的未打开 continuation 卡
// （topic_pipeline.py:6826-6893）。源请求仍是本轮 RID 释放的兴趣之源时卡
// 返回；它跟随该请求到如今持有它的兴趣。
func (p *TopicPipeline) RetainedContinuationPlans(snapshot *V6ContextSnapshot,
	rid *RIDResult, feedback *InterestFeedback, triggerKey map[string]any,
	summariesByID map[string]string, now float64) []map[string]any {
	retained, eligibleInterestIDs := p.Retainable(snapshot, rid, feedback,
		triggerKey, now)
	if len(retained.Cards) == 0 {
		return nil
	}
	interestByLocator := map[string]string{}
	for _, interest := range rid.TriggerInterests() {
		if _, eligible := eligibleInterestIDs[interest.InterestSummaryID]; !eligible {
			continue
		}
		for _, locator := range interest.SourceLocators {
			key := fmt.Sprintf("%s\x00%d\x00%s", locator.SessionID, locator.Ordinal,
				locator.EventID)
			if _, present := interestByLocator[key]; !present {
				interestByLocator[key] = interest.InterestSummaryID
			}
		}
	}
	sources := map[string]*V6SourceMessage{}
	for _, item := range snapshot.Messages {
		if item.Natural() {
			sources[item.EventID] = item
		}
	}
	var plans []map[string]any
	for _, card := range retained.Cards {
		if pyStr(card["category"]) != "project_continuation" {
			continue
		}
		locators := pyListMaps(card["project_source_locators"])
		if len(locators) == 0 {
			continue
		}
		source := sources[pyStr(locators[0]["event_id"])]
		if source == nil {
			continue
		}
		interestID := interestByLocator[fmt.Sprintf("%s\x00%d\x00%s",
			source.SessionID, source.Ordinal, source.EventID)]
		if interestID == "" ||
			toFloatOr(card["expire_time"], 0)-now < retainedMinimumRemainingSeconds {
			continue
		}
		plan := ProjectContinuationPlan(pyStr(card["card_id"]), interestID,
			pyStr(card["intent"]), pyStr(card["title"]), source, snapshot,
			summariesByID[interestID])
		plans = append(plans, CarriedPlan(plan, card))
	}
	return plans
}

// ValidateGlobalInterestCandidates 全局兴趣候选校验
// （topic_pipeline.py:6896-6977）。破坏槽血缘的决策单独丢弃；每槽恰一次
// 决策，只有写下的槽成为候选。
func ValidateGlobalInterestCandidates(response any, snapshot *V6ContextSnapshot,
	ranked []RankedInterest, summariesByID map[string]string,
	sourceProfileUnitsByInterest map[string][]string,
	candidateSlots []map[string]any, now float64) ([]map[string]any, error) {
	const failure = "global_interest_contract_failed"
	root, isMap := response.(map[string]any)
	if !isMap || len(root) != 1 {
		return nil, &TerminalGenerationRejection{Reason: failure}
	}
	rawCandidates, isList := root["candidates"].([]any)
	if !isList {
		return nil, &TerminalGenerationRejection{Reason: failure}
	}
	slotInterestByID := map[string]string{}
	for _, slot := range candidateSlots {
		slotInterestByID[pyStr(slot["slot_id"])] = pyStr(slot["interest_summary_id"])
	}
	interestIDs := map[string]bool{}
	for _, item := range ranked {
		interestIDs[item.InterestSummaryID] = true
	}
	if len(slotInterestByID) != len(candidateSlots) {
		return nil, &TerminalGenerationRejection{Reason: failure}
	}
	for _, interestID := range slotInterestByID {
		if interestID == "" || !interestIDs[interestID] {
			return nil, &TerminalGenerationRejection{Reason: failure}
		}
	}
	required := []string{"slot_id", "interest_summary_id", "positions_left",
		"direction", "blocked_by", "new_angle", "decision", "intent", "title"}
	seenSlots := map[string]bool{}
	var plans []map[string]any
	dropped := 0
	for _, raw := range rawCandidates {
		entry, isMap := raw.(map[string]any)
		shapeOK := isMap && len(entry) == len(required)
		if shapeOK {
			for _, field := range required {
				if _, present := entry[field]; !present {
					shapeOK = false
					break
				}
			}
		}
		if !shapeOK || (pyStr(entry["decision"]) != "write" &&
			pyStr(entry["decision"]) != "skip") {
			dropped++
			continue
		}
		slotID := strings.TrimSpace(pyStr(entry["slot_id"]))
		interestID := strings.TrimSpace(pyStr(entry["interest_summary_id"]))
		if slotInterestByID[slotID] != interestID || seenSlots[slotID] {
			dropped++
			continue
		}
		seenSlots[slotID] = true
		if pyStr(entry["decision"]) == "skip" {
			continue
		}
		intent := strings.TrimSpace(pyStr(entry["intent"]))
		title := strings.TrimSpace(pyStr(entry["title"]))
		direction, directionIsStr := entry["direction"].(string)
		if intent == "" || title == "" || !directionIsStr ||
			strings.TrimSpace(direction) == "" {
			dropped++
			continue
		}
		summary := summariesByID[interestID]
		plans = append(plans, ProfileInterestPlan(slotID, interestID, summary,
			intent, title, title, strings.TrimSpace(direction),
			sourceProfileUnitsByInterest[interestID], snapshot.ContextScope,
			snapshot.InputSnapshotID, now))
	}
	if dropped > 0 {
		logger.Warn(logger.ComponentAgentCore).
			Int("dropped", dropped).
			Msg("[Topic] global interest candidates dropped")
	}
	return plans, nil
}

// ValidateRecentCandidates 近期候选校验（topic_pipeline.py:6980-7097）。
func ValidateRecentCandidates(response any, ranked []RankedInterest,
	summariesByID map[string]string,
	sourceProfileUnitsByInterest map[string][]string, contextScope, category string,
	minimum int, candidateSlots []map[string]any) ([]map[string]any, error) {
	const failure = "recent_candidate_contract_failed"
	root, isMap := response.(map[string]any)
	if !isMap || len(root) != 1 {
		return nil, &TerminalGenerationRejection{Reason: failure}
	}
	rawCandidates, isList := root["candidates"].([]any)
	if !isList {
		return nil, &TerminalGenerationRejection{Reason: failure}
	}
	interestByID := map[string]RankedInterest{}
	for _, item := range ranked {
		interestByID[item.InterestSummaryID] = item
	}
	required := map[string]bool{
		"slot_id": true, "interest_summary_id": true, "identity_anchor": true,
		"intent": true, "title": true, "primary_query": true,
		"freshness_class": true,
	}
	directional := contextScope == GlobalNewChatScope &&
		category == "profile_recent_progress"
	if directional {
		required["direction"] = true
	}
	slotInterestByID := map[string]string{}
	for _, slot := range candidateSlots {
		slotInterestByID[pyStr(slot["slot_id"])] = pyStr(slot["interest_summary_id"])
	}
	if len(slotInterestByID) != len(candidateSlots) {
		return nil, &TerminalGenerationRejection{Reason: failure}
	}
	for _, interestID := range slotInterestByID {
		if interestID == "" {
			return nil, &TerminalGenerationRejection{Reason: failure}
		}
		if _, known := interestByID[interestID]; !known {
			return nil, &TerminalGenerationRejection{Reason: failure}
		}
	}
	seenSlots := map[string]bool{}
	seenInterests := map[string]bool{}
	var plans []map[string]any
	for _, raw := range rawCandidates {
		entry, isMap := raw.(map[string]any)
		if !isMap || len(entry) != len(required) {
			return nil, &TerminalGenerationRejection{Reason: failure}
		}
		for field := range required {
			if _, present := entry[field]; !present {
				return nil, &TerminalGenerationRejection{Reason: failure}
			}
		}
		slotID := strings.TrimSpace(pyStr(entry["slot_id"]))
		interestID := strings.TrimSpace(pyStr(entry["interest_summary_id"]))
		identityAnchor := strings.TrimSpace(pyStr(entry["identity_anchor"]))
		direction, directionIsStr := entry["direction"].(string)
		intent := strings.TrimSpace(pyStr(entry["intent"]))
		title := strings.TrimSpace(pyStr(entry["title"]))
		query := strings.TrimSpace(pyStr(entry["primary_query"]))
		query = strings.ReplaceAll(query, "“", "\"")
		query = strings.ReplaceAll(query, "”", "\"")
		for _, releaseTerm := range []string{"release", "changelog"} {
			if strings.EqualFold(query, identityAnchor+" "+releaseTerm) {
				query = fmt.Sprintf("\"%s\" \"%s\"", identityAnchor, releaseTerm)
				break
			}
		}
		freshness := pyStr(entry["freshness_class"])
		if strings.EqualFold(query, "null") || strings.EqualFold(query, "none") {
			// 模型给它没有的查询写了占位词；搜那个词只会返回无关结果。
			logger.Warn(logger.ComponentAgentCore).
				Msg("Topic Starter v6 dropped a recent candidate without a search query")
			continue
		}
		interestHeld := category == "project_domain_progress" || !seenInterests[interestID]
		rejected := slotInterestByID[slotID] != interestID ||
			seenSlots[slotID] ||
			func() bool { _, known := interestByID[interestID]; return !known }() ||
			!interestHeld ||
			identityAnchor == "" ||
			(directional && (!directionIsStr || strings.TrimSpace(direction) == "")) ||
			intent == "" || title == "" || query == "" ||
			(freshness != "current_event" && freshness != "recent_development")
		if rejected {
			return nil, &TerminalGenerationRejection{Reason: failure}
		}
		seenSlots[slotID] = true
		seenInterests[interestID] = true
		plan := map[string]any{
			"candidate_id": slotID, "interest_summary_id": interestID,
			"identity_anchor":  identityAnchor,
			"interest_summary": summariesByID[interestID],
			"intent":           intent, "title": title, "primary_query": query,
			"freshness_class": freshness, "prompt": title,
			"category": category, "context_scope": contextScope,
			"source_starter_id": nil,
			"persona_context":   summariesByID[interestID],
			"source_locators":   []any{}, "project_source_locators": []any{},
		}
		if directional {
			plan["direction"] = strings.TrimSpace(direction)
		}
		unitIDs := sourceProfileUnitsByInterest[interestID]
		unitList := make([]any, 0, len(unitIDs))
		for _, unitID := range unitIDs {
			unitList = append(unitList, unitID)
		}
		plan["source_memory_unit_ids"] = unitList
		plans = append(plans, plan)
	}
	if len(plans) < minimum {
		return nil, &TerminalGenerationRejection{Reason: "recent_candidate_shortfall"}
	}
	return plans, nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// d7RankedInterest 全局生成的富秩兴趣（RankedStarterInterest 的 asdict 形态）。
type d7RankedInterest struct {
	id      string
	summary string
	rank    int
}

// d7ToRankedInterest 转秩形态。
func d7ToRankedInterest(ranked []d7RankedInterest) []RankedInterest {
	out := make([]RankedInterest, 0, len(ranked))
	for _, item := range ranked {
		out = append(out, RankedInterest{
			InterestSummaryID: item.id, Rank: item.rank})
	}
	return out
}

// setKeys 集合键列表。
func setKeys(set map[string]bool) []string {
	out := make([]string, 0, len(set))
	for key := range set {
		out = append(out, key)
	}
	return out
}

// compactJSON 紧凑序列化（json.dumps ensure_ascii=False 等价）。
func compactJSON(value map[string]any) string {
	return pyWitnessJSON(value)
}

// sameMapReference map 引用同体判定（Python `in` 对 dict 用 ==；Go 侧计划
// 是同一 map 引用流经 wanted/generated，引用判定等价）。
func sameMapReference(a, b map[string]any) bool {
	return fmt.Sprintf("%p", a) == fmt.Sprintf("%p", b)
}

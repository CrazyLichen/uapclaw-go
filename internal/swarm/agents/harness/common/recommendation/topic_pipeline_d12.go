package recommendation

// topic_pipeline_d12.go 一比一对齐上游 topic_pipeline.py:9169-9704 的
// _generate_prepared/_news_pool_after_rid_abstained/_generate_from_summaries。
// 已知偏差（§5 并发映射）：新闻匹配任务/早期证据任务 → goroutine+channel；
// TerminalGenerationRejection 的 grounding 小票携带字段 →
// GroundingReceipts 附带结构（d3 generate 消费）。

import (
	"context"
	"sort"
)

// ──────────────────────────── 常量 ────────────────────────────

// RIDAbstentionReceiptVersion RID 放弃小票版本（topic_pipeline.py:184）。
const RIDAbstentionReceiptVersion = "topic-starter-v6-rid-abstention-v1"

// ──────────────────────────── 结构体 ────────────────────────────

// newsOutcome 新闻匹配结果（对齐 Future[tuple] 形态）。
type newsOutcome struct {
	plan   map[string]any
	reason string
}

// generationInvocation 一次生成调用的共享状态（Python 闭包字段的 Go 形态）。
type generationInvocation struct {
	earlyEvidence map[string]*candidateEvidenceFuture
	fetchTasks    *FetchTasks
	newsDone      chan newsOutcome
	newsCancel    context.CancelFunc
}

// candidateEvidenceFuture 早期候选证据 future。
type candidateEvidenceFuture struct {
	done   chan *CandidateEvidence
	cancel context.CancelFunc
}

// ──────────────────────────── 导出函数 ────────────────────────────

// GeneratePrepared 已备快照的生成（topic_pipeline.py:9169-9225
// _generate_prepared）。新闻匹配与 RID 并行提问；未读匹配的轮取消它。
func (p *TopicPipeline) GeneratePrepared(snapshot *V6ContextSnapshot,
	feedback *InterestFeedback, triggerKey map[string]any, triggerOrigin string,
	config *ArkBackgroundConfig, invokeStage TopicStageInvoker,
	externalAudit *ExternalInvocationAudit, searchProvider WebSearchFn,
	summaryBatch *sessionSummaryBatch) (*BackgroundGenerationResult, error) {
	if summaryBatch == nil {
		var err error
		summaryBatch, err = p.SummarizeSessions(snapshot, config, invokeStage)
		if err != nil {
			return nil, err
		}
	}
	var pull *HotListPull
	invocation := &generationInvocation{
		earlyEvidence: map[string]*candidateEvidenceFuture{},
		fetchTasks:    NewFetchTasks(),
		newsDone:      make(chan newsOutcome, 1),
	}
	if snapshot.ContextScope == GlobalNewChatScope {
		// 匹配读画像兴趣与否定，不读 RID 的选择——所以趁 RID 决策时问。
		// 拉取一秒内应答或取最近半小时的。
		if p.hotList != nil {
			pull = p.hotList.Pull(context.Background())
		}
		_, cancel := context.WithCancel(context.Background())
		invocation.newsCancel = cancel
		go func() {
			defer close(invocation.newsDone)
			plan, reason := p.HotNewsLead(snapshot, pull, feedback, invokeStage)
			var planValue map[string]any
			if plan != nil {
				planValue = *plan
			}
			invocation.newsDone <- newsOutcome{plan: planValue, reason: reason}
		}()
	}
	result, err := p.GenerateFromSummaries(snapshot, feedback, triggerKey,
		triggerOrigin, config, invokeStage, externalAudit, searchProvider,
		summaryBatch, pull, invocation)
	if invocation.newsCancel != nil {
		invocation.newsCancel()
	}
	for _, future := range invocation.earlyEvidence {
		if future.cancel != nil {
			future.cancel()
		}
	}
	if result == nil && err == nil {
		return nil, nil
	}
	return result, err
}

// NewsPoolAfterRIDAbstained RID 未释放任何兴趣后的新闻单卡池
// （topic_pipeline.py:9227-9319）。新闻 lane 自 RID 应答起照常等待；卡由
// 效用单独判定，无多样性与列表搜索。无被纳入的新闻卡时返回 nil。
func (p *TopicPipeline) NewsPoolAfterRIDAbstained(snapshot *V6ContextSnapshot,
	feedback *InterestFeedback, rid *RIDResult,
	summaries []*SessionSummaryArtifact, summaryBatch *sessionSummaryBatch,
	invocation *generationInvocation, triggerKey map[string]any,
	triggerOrigin string, config *ArkBackgroundConfig,
	invokeStage TopicStageInvoker, externalAudit *ExternalInvocationAudit,
	searchProvider WebSearchFn) (*BackgroundGenerationResult, error) {
	lanes := NewOptionalLanes()
	lanes.MandatoryReady()
	newsCancel := context.CancelFunc(func() {})
	if invocation.newsCancel != nil {
		newsCancel = invocation.newsCancel
	}
	var newsPlan map[string]any
	newsAbsentReason := "optional_lane_timeout"
	newsFinished := newsDoneChannel(invocation)
	if lanes.Finish(PipelineHotNewsLane, newsFinished, newsCancel) {
		outcome := <-invocation.newsDone
		newsPlan, newsAbsentReason = outcome.plan, outcome.reason
	}
	if newsPlan == nil {
		return nil, nil
	}
	_ = newsAbsentReason
	plans := []map[string]any{newsPlan}
	AttachInterestSources(plans, snapshot, summaries, feedback)
	newsPlan["discussion_coverage"] = DiscussionCoverage(summaries,
		snapshot.OtherScopeDiscussionCoverage)
	groundingBatch, groundErr := p.GroundPlans(plans, snapshot, triggerKey,
		config, invokeStage, externalAudit, searchProvider, 1, lanes, nil, nil,
		invocation.earlyEvidence, invocation.fetchTasks)
	if groundErr != nil {
		return nil, nil
	}
	if len(groundingBatch.plans) == 0 {
		return nil, nil
	}
	selection, newsLeadID, newsRejection, scoreErr := p.ScoreAndSelect(
		groundingBatch.plans, snapshot, feedback, config, invokeStage, nil, nil)
	if scoreErr != nil {
		return nil, nil
	}
	return p.AssembleResult([]string{newsLeadID}, groundingBatch.plans,
		selection,
		NewsSlotRecord(groundingBatch.plans, firstNonEmpty(newsAbsentReason, newsRejection)),
		snapshot, summaries, summaryBatch.coverageReceipt,
		summaryBatch.generatedSummaryIDs, summaryBatch.summaryConfigVersion,
		rid, map[string]any{
			"policy":                                "all_rid_selected_v1",
			"selected_interest_summary_ids":         []any{},
			"original_rid_ranks":                    map[string]any{},
			"delta_linked_interest_summary_id":      nil,
			"short_trigger_delta_event_ids":         []any{},
			"extra_recent_slot_interest_summary_id": nil,
			"direction_withheld_candidates":         []any{},
			"restated_offered_candidates":           []any{},
		},
		groundingBatch.decisionReceipt, groundingBatch.evidenceReceipt,
		triggerKey, triggerOrigin, config,
		sortedLaneKeys(lanes.TimedOutSet()), lanes.FailedMap())
}

// GenerateFromSummaries 从摘要生成（topic_pipeline.py:9321-9704
// _generate_from_summaries）。
func (p *TopicPipeline) GenerateFromSummaries(snapshot *V6ContextSnapshot,
	feedback *InterestFeedback, triggerKey map[string]any, triggerOrigin string,
	config *ArkBackgroundConfig, invokeStage TopicStageInvoker,
	externalAudit *ExternalInvocationAudit, searchProvider WebSearchFn,
	summaryBatch *sessionSummaryBatch, hotListPull *HotListPull,
	invocation *generationInvocation) (*BackgroundGenerationResult, error) {
	if p.OnCandidatesReady != nil {
		p.OnCandidatesReady(snapshot.ContextScope)
	}
	summaries := summaryBatch.artifacts
	decisionTime := p.Now()
	rid, err := p.DistillInterests(snapshot, summaries, feedback, config,
		invokeStage, decisionTime, true, triggerOrigin == "manual_retry")
	if err != nil {
		return nil, err
	}
	if len(rid.TriggerInterests()) == 0 {
		// RID 判定但未释放任何兴趣；被纳入的新闻卡仍单独成池。
		if invocation.newsDone != nil && rid.Receipt.FallbackReason == "" {
			newsPool, poolErr := p.NewsPoolAfterRIDAbstained(snapshot, feedback,
				rid, summaries, summaryBatch, invocation, triggerKey,
				triggerOrigin, config, invokeStage, externalAudit, searchProvider)
			if poolErr != nil {
				return nil, poolErr
			}
			if newsPool != nil {
				return newsPool, nil
			}
		}
		eligibleSummaries := EligibleNaturalRevisitSummaries(summaries, snapshot.ContextScope)
		decisionContext := RidDecisionContext(snapshot,
			WantedSessionSummaries(snapshot, eligibleSummaries, feedback),
			feedback, decisionTime)
		ridResult := rid.AsDownstreamPayload()
		materializationReceipt, materializationErr := SessionSummaryMaterializationReceipt(
			snapshot, summaries, summaryBatch.generatedSummaryIDs,
			summaryBatch.summaryConfigVersion)
		if materializationErr != nil {
			return nil, materializationErr
		}
		return &BackgroundGenerationResult{
			Cards: nil,
			Manifest: map[string]any{
				"generation_trigger_key":       triggerKey,
				"generation_input_snapshot_id": snapshot.InputSnapshotID,
				"context_scope":                snapshot.ContextScope,
				"project_id":                   snapshot.ProjectID,
				"session_summary_artifacts": func() []any {
					payloads := make([]any, 0, len(summaries))
					for _, summary := range summaries {
						payloads = append(payloads, summary.AsPayload())
					}
					return payloads
				}(),
				"session_summary_coverage_receipt":        mapCopyAny(summaryBatch.coverageReceipt),
				"session_summary_materialization_receipt": materializationReceipt,
				"component_receipts": map[string]any{
					RID_COMPONENT_ID: rid.Receipt.AsPayload(),
				},
				"rid_result": ridResult,
				"rid_abstention_receipt": map[string]any{
					"schema_version":         RIDAbstentionReceiptVersion,
					"generation_trigger_key": triggerKey,
					"input_snapshot_id":      snapshot.InputSnapshotID,
					"context_scope":          snapshot.ContextScope,
					"project_id":             snapshot.ProjectID,
					"trigger_origin":         triggerOrigin,
					"decision_time":          decisionTime,
					"decision_context":       decisionContext,
					"rid_result":             ridResult,
					"session_summary_materialization_receipt": materializationReceipt,
				},
			},
			FailureReason: "rid_abstained",
		}, nil
	}
	var interestSelection map[string]any
	lanes := NewOptionalLanes()
	// 全局页的新闻卡：其候选，或为何没有。项目页没有新闻。
	newsAbsentReason := ""
	newsOnly := false
	// 现在就为近期计划搜索；冻结汇合它们收集的内容。
	startRecentEvidence := func(recent []map[string]any) {
		effectiveSearch := searchProvider
		if effectiveSearch == nil {
			effectiveSearch = p.searchOverride
		}
		if effectiveSearch == nil {
			return
		}
		for _, plan := range recent {
			if HostEvidence(plan) == nil {
				candidateID := pyStr(plan["candidate_id"])
				futureCtx, cancel := context.WithCancel(context.Background())
				_ = futureCtx
				future := &candidateEvidenceFuture{
					done: make(chan *CandidateEvidence, 1), cancel: cancel}
				invocation.earlyEvidence[candidateID] = future
				go func(plan map[string]any, future *candidateEvidenceFuture) {
					defer close(future.done)
					future.done <- p.CandidateEvidenceFor(plan, snapshot,
						externalAudit, effectiveSearch, lanes, invocation.fetchTasks)
				}(plan, future)
			}
		}
	}
	offeredQuestions := p.OfferedQuestions(snapshot, true)
	var retained []map[string]any
	var plans []map[string]any
	if snapshot.ContextScope == GlobalNewChatScope {
		retained = p.RetainedCandidatePlans(snapshot, rid, feedback, triggerKey,
			InterestSummaryTextByID(snapshot, summaries), p.Now())
		// 拿到列表后生成器为新闻卡留位。
		plans, interestSelection, err = p.GlobalCandidatePlans(snapshot, rid,
			summaries, feedback, config, invokeStage,
			OfferedTitlesByInterest(retained), len(retained), offeredQuestions,
			hotListPull != nil, lanes, startRecentEvidence)
		if err != nil {
			return nil, err
		}
		// 新闻 lane 等待
		var newsPlan map[string]any
		newsAbsentReason = "optional_lane_timeout"
		newsCancel := context.CancelFunc(func() {})
		if invocation.newsCancel != nil {
			newsCancel = invocation.newsCancel
		}
		newsFinished := newsDoneChannel(invocation)
		if lanes.Finish(PipelineHotNewsLane, newsFinished, newsCancel) {
			outcome := <-invocation.newsDone
			newsPlan = outcome.plan
			newsAbsentReason = outcome.reason
		}
		// 没有常规计划时池仍可以是新闻卡单独成池。
		newsOnly = len(plans) == 0 && len(retained) == 0
		if newsOnly && newsPlan == nil {
			return nil, &TerminalGenerationRejection{Reason: "no_feasible_complete_list"}
		}
		plans = append(plans, retained...)
		if newsPlan != nil {
			plans = append(plans, newsPlan)
		}
		AttachInterestSources(plans, snapshot, summaries, feedback)
	} else {
		retained = p.RetainedContinuationPlans(snapshot, rid, feedback, triggerKey,
			InterestSummaryTextByID(snapshot, summaries), p.Now())
		generated, projectErr := p.ProjectCandidatePlans(snapshot, rid, summaries,
			invokeStage, OfferedTitlesByInterest(retained),
			feedback.DismissedCardTitles(), offeredQuestions,
			len(retained) > 0, lanes)
		if projectErr != nil {
			return nil, projectErr
		}
		// 携带卡取更早的秩，选择用它们打破并列。
		plans = append(append([]map[string]any(nil), retained...), generated...)
	}
	// 效用对被否定的问题持更严的门槛。
	dismissedSeen := map[string]bool{}
	var dismissedQuestions []string
	for _, title := range feedback.DismissedCardTitles() {
		if !dismissedSeen[title] {
			dismissedSeen[title] = true
			dismissedQuestions = append(dismissedQuestions, title)
		}
	}
	var previousQuestions []string
	for _, plan := range retained {
		previousQuestions = append(previousQuestions, pyStr(plan["title"]))
	}
	for _, question := range offeredQuestions {
		previousQuestions = append(previousQuestions, pyStr(question["title"]))
	}
	for _, plan := range plans {
		plan["dismissed_questions"] = toAnyList(dismissedQuestions)
		previousSeen := map[string]bool{}
		var previousList []any
		for _, title := range previousQuestions {
			if !previousSeen[title] {
				previousSeen[title] = true
				previousList = append(previousList, title)
			}
		}
		for _, question := range pyListMaps(plan["previously_offered"]) {
			title := pyStr(question["title"])
			if !previousSeen[title] {
				previousSeen[title] = true
				previousList = append(previousList, title)
			}
		}
		filtered := make([]any, 0, len(previousList))
		for _, title := range previousList {
			if !dismissedSeen[pyStr(title)] {
				filtered = append(filtered, title)
			}
		}
		plan["previous_questions"] = filtered
		plan["discussion_coverage"] = DiscussionCoverage(summaries,
			snapshot.OtherScopeDiscussionCoverage)
	}
	var earlyScoring *pipelineScoringEarly
	// 项目页没有新闻卡：领域 lane 搜索时其自身卡已在评分。
	localReady := func(local []map[string]any) {
		earlyScoring = p.StartScoring(local, snapshot, feedback, config, invokeStage)
	}
	regularReady := func(ready []map[string]any) {}
	var onRegularReady func([]map[string]any)
	if snapshot.ContextScope == GlobalNewChatScope {
		onRegularReady = regularReady
	}
	var onLocalReady func([]map[string]any)
	if snapshot.ContextScope != GlobalNewChatScope {
		onLocalReady = localReady
	}
	// Go 侧 diversity 早启经 StartScoring 内部完成；这里记录早期评分供复用。
	earlyForReuse := earlyScoring
	groundingBatch, groundErr := p.GroundPlans(plans, snapshot, triggerKey,
		config, invokeStage, externalAudit, searchProvider, 1, lanes,
		onRegularReady, onLocalReady, invocation.earlyEvidence,
		invocation.fetchTasks)
	if groundErr != nil {
		return nil, groundErr
	}
	grounded := groundingBatch.plans
	if len(grounded) == 0 {
		reason := "no_grounded_candidates"
		if newsOnly {
			reason = "no_feasible_complete_list"
		}
		return nil, &TerminalGenerationRejection{Reason: reason}
	}
	var requiredFirstWindowInterestSummaryIDs []string
	if snapshot.ContextScope == GlobalNewChatScope &&
		len(ShortTriggerNaturalDeltaEventIDs(snapshot)) > 0 {
		if interestSelection == nil {
			return nil, &TerminalGenerationRejection{Reason: "missing_required_short_delta_interest"}
		}
		deltaEventIDs := ShortTriggerNaturalDeltaEventIDs(snapshot)
		deltaList := make([]string, 0, len(deltaEventIDs))
		for id := range deltaEventIDs {
			deltaList = append(deltaList, id)
		}
		deltaInterestIDs := ExactDeltaLinkedInterestIds(rid, deltaList)
		groundedInterestIDs := map[string]bool{}
		for _, plan := range grounded {
			groundedInterestIDs[pyStrOr(plan["interest_summary_id"], "")] = true
		}
		for _, interest := range rid.TriggerInterests() {
			interestID := interest.InterestSummaryID
			if !deltaInterestIDs[interestID] {
				continue
			}
			if !groundedInterestIDs[interestID] {
				continue
			}
			requiredFirstWindowInterestSummaryIDs = append(
				requiredFirstWindowInterestSummaryIDs, interestID)
		}
	}
	selection, newsLeadID, newsRejection, scoreErr := p.ScoreAndSelect(grounded,
		snapshot, feedback, config, invokeStage,
		requiredFirstWindowInterestSummaryIDs, earlyForReuse)
	if scoreErr != nil {
		return nil, scoreErr
	}
	selectedIDs := []string{}
	if newsLeadID != "" {
		selectedIDs = append(selectedIDs, newsLeadID)
	}
	selectedIDs = append(selectedIDs, selection.PreferredList...)
	if interestSelection != nil &&
		len(pyListStrings(interestSelection["short_trigger_delta_event_ids"])) > 0 {
		// 新闻卡单独成池时短触发保持挂起。
		firstWindowLimit := 4
		if len(selectedIDs) < firstWindowLimit {
			firstWindowLimit = len(selectedIDs)
		}
		firstInterests := map[string]bool{}
		if len(selection.PreferredList) > 0 {
			for _, plan := range grounded {
				for _, selectedID := range selectedIDs[:firstWindowLimit] {
					if pyStr(plan["candidate_id"]) == selectedID {
						firstInterests[pyStrOr(plan["interest_summary_id"], "")] = true
					}
				}
			}
		}
		deltaInFirstWindow := ""
		for _, interestID := range requiredFirstWindowInterestSummaryIDs {
			if firstInterests[interestID] {
				deltaInFirstWindow = interestID
				break
			}
		}
		updatedSelection := mapCopyAny(interestSelection)
		if deltaInFirstWindow == "" {
			// 没有合格卡回答最新问题：其他卡仍成池，短触发不消费、
			// 挂起给下一个池。
			updatedSelection["short_trigger_delta_event_ids"] = []any{}
		} else {
			updatedSelection["short_trigger_delta_event_ids"] =
				interestSelection["short_trigger_delta_event_ids"]
		}
		updatedSelection["delta_linked_interest_summary_id"] = nilIfEmpty(
			deltaInFirstWindow, deltaInFirstWindow != "")
		interestSelection = updatedSelection
	}
	newsSlot := any(nil)
	if snapshot.ContextScope == GlobalNewChatScope {
		newsSlot = NewsSlotRecord(grounded, firstNonEmpty(newsAbsentReason, newsRejection))
	}
	var newsSlotMap map[string]any
	if slotMap, isMap := newsSlot.(map[string]any); isMap {
		newsSlotMap = slotMap
	}
	return p.AssembleResult(selectedIDs, grounded, selection, newsSlotMap,
		snapshot, summaries, summaryBatch.coverageReceipt,
		summaryBatch.generatedSummaryIDs, summaryBatch.summaryConfigVersion,
		rid, interestSelection, groundingBatch.decisionReceipt,
		groundingBatch.evidenceReceipt, triggerKey, triggerOrigin, config,
		sortedLaneKeys(lanes.TimedOutSet()), lanes.FailedMap())
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// newsDoneChannel newsDone 的完成信号形态（channel 关闭即完成）。
func newsDoneChannel(invocation *generationInvocation) <-chan struct{} {
	wrapped := make(chan struct{})
	go func() {
		if outcome, ok := <-invocation.newsDone; ok {
			invocation.newsDone <- outcome
		}
		close(wrapped)
	}()
	return wrapped
}

// sortedLaneKeys lane 集合键排序。
func sortedLaneKeys(set map[string]bool) []string {
	keys := make([]string, 0, len(set))
	for key := range set {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

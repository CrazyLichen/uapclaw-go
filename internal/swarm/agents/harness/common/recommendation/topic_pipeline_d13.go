package recommendation

// topic_pipeline_d13.go 一比一对齐上游 topic_pipeline.py:4908-5267 的
// generate 与 :5268-5393 的 refresh_news，附 topic_product.py:4553-4637 的
// load_exact_global_pool。
// 已知偏差（§5 登记）：TerminalGenerationRejection 的 grounding 小票携带
// （Python kwargs）未实现——Go 拒绝仅带 reason，失败轮 failed manifest
// 不嵌 grounding 小票（外部审计与厂商小票仍记录）；随 31c reader 面回填。

import (
	"context"
	"fmt"
	"strings"

	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// LoadExactGlobalPool 按 manifest ID 读取仍有效的全局池
// （topic_product.py:4553-4637）。
func LoadExactGlobalPool(store *TopicStarterStore, manifestID any) (map[string]any, []map[string]any) {
	manifestIDStr := pyStr(manifestID)
	if manifestIDStr == "" {
		return nil, nil
	}
	rows, err := store.QueryEvents(QueryEventsOptions{
		EventType:   "starter_slate_manifest",
		SlateID:     stringPtr(manifestIDStr),
		PolicyID:    stringPtr(POLICY_ID),
		Limit:       2,
		NewestFirst: true,
	})
	if err != nil || len(rows) != 1 {
		return nil, nil
	}
	row := rows[0]
	if pyStr(row["topic_id"]) != manifestIDStr {
		return nil, nil
	}
	manifest, _, ok := payloadMap(row["payload"])
	if !ok || pyStr(manifest["manifest_id"]) != manifestIDStr ||
		pyStr(manifest["context_scope"]) != GlobalNewChatScope {
		return nil, nil
	}
	generationBatchID := pyStr(manifest["generation_batch_id"])
	revisions := pyListStrings(manifest["card_revision_ids"])
	if generationBatchID == "" || len(revisions) == 0 {
		return nil, nil
	}
	if len(revisions) > GlobalPoolTargetSizeV6 {
		return nil, nil
	}
	seen := map[string]bool{}
	for _, revision := range revisions {
		if revision == "" || seen[revision] {
			return nil, nil
		}
		seen[revision] = true
	}
	cardRows, err := store.QueryEvents(QueryEventsOptions{
		EventType: "starter_card",
		SlateID:   stringPtr(manifestIDStr),
		PolicyID:  stringPtr(POLICY_ID),
		Limit:     GlobalPoolTargetSizeV6 + 1,
	})
	if err != nil || len(cardRows) != len(revisions) {
		return nil, nil
	}
	byRevision := map[string]map[string]any{}
	for _, cardRow := range cardRows {
		card, _, ok := payloadMap(cardRow["payload"])
		if !ok {
			return nil, nil
		}
		revisionID := pyStr(card["card_revision_id"])
		if revisionID == "" || pyStr(cardRow["topic_id"]) != revisionID ||
			pyStr(card["generation_batch_id"]) != generationBatchID ||
			pyStr(card["context_scope"]) != GlobalNewChatScope {
			return nil, nil
		}
		if _, dup := byRevision[revisionID]; dup {
			return nil, nil
		}
		byRevision[revisionID] = card
	}
	var cards []map[string]any
	for _, revision := range revisions {
		if card, present := byRevision[revision]; present {
			cards = append(cards, card)
		}
	}
	if len(cards) != len(revisions) ||
		!ReadablePoolManifest(manifest, cards, true) {
		return nil, nil
	}
	return manifest, cards
}

// Generate 一次终局生成（topic_pipeline.py:4908-5267 generate）。
// 组装终局组件而不允许任何模型绕过宿主闸门。
func (p *TopicPipeline) Generate(triggerKey map[string]any, triggerOrigin string,
	config *ArkBackgroundConfig, contextScope string, projectID any,
	visitID string) *BackgroundGenerationResult {
	emptyResult := func(reason string, inputSnapshotID string) *BackgroundGenerationResult {
		manifest := map[string]any{
			"generation_trigger_key": triggerKey,
			"trigger_origin":         triggerOrigin,
			"failure_reason":         reason,
		}
		if inputSnapshotID != "" {
			manifest["input_snapshot_id"] = inputSnapshotID
		}
		return &BackgroundGenerationResult{
			Cards: nil, Manifest: manifest, FailureReason: reason,
		}
	}
	if expected, err := ProjectIDForScope(contextScope); err != nil ||
		!scopeProjectNullEqual(projectID, expected) {
		return emptyResult("scope_mismatch", "")
	}
	snapshot := p.contextLoader(contextScope)
	if snapshot == nil {
		return emptyResult("generation_input_contract_failed", "")
	}
	feedback, feedbackErr := InterestFeedbackLoad(p.Store, snapshot.State, contextScope,
		projectID, p.Now())
	if feedbackErr != nil {
		return emptyResult("generation_input_contract_failed", "")
	}
	scopedNegativeVersion := feedback.Version()
	stateReceipt := snapshot.StateMaintenanceReceipt
	inputSnapshotID := snapshot.InputSnapshotID
	versionMismatch := func() bool {
		if pyStr(snapshot.ContextScope) != contextScope ||
			!anyEqual(snapshot.ProjectID, projectID) {
			return true
		}
		if pyIntOf(triggerKey["profile_semantic_version"]) !=
			pyIntOf(snapshot.ProfileProjection["profile_semantic_version"]) {
			return true
		}
		if snapshot.ShortIntentState != nil &&
			pyIntOf(triggerKey["short_intent_version"]) !=
				pyIntOf(snapshot.ShortIntentState.ShortIntentVersion) {
			return true
		}
		if pyIntOf(triggerKey["negative_preference_version"]) != scopedNegativeVersion {
			return true
		}
		if stateReceipt == nil {
			return true
		}
		return pyIntOf(stateReceipt["negative_preference_version"]) != scopedNegativeVersion
	}
	if versionMismatch() {
		return emptyResult("generation_input_version_mismatch", inputSnapshotID)
	}
	externalAudit, auditErr := NewExternalInvocationAudit(p.Store, POLICY_ID,
		inputSnapshotID, snapshot.ContextScope, visitID)
	if auditErr != nil {
		return emptyResult("generation_input_contract_failed", inputSnapshotID)
	}
	failedResult := func(reason string) *BackgroundGenerationResult {
		failure := emptyResult(reason, inputSnapshotID)
		failure.Manifest["external_invocation_audit"] = externalAudit.AsManifestReceipt()
		if invoker := p.lastAuditedInvoker; invoker != nil {
			var receipts []any
			for _, receipt := range invoker.InvocationReceipts() {
				if _, isMap := receipt["lineage"].(map[string]any); isMap {
					receipts = append(receipts, mapCopyAny(receipt))
				}
			}
			if len(receipts) > 0 {
				failure.Manifest["ark_invocation_receipts"] = receipts
			}
		}
		return failure
	}
	ownedStageInvoker := p.stageInvokerFactory(config)
	audited := AuditedArkStageInvoker(arkInvokerOf(ownedStageInvoker), externalAudit)
	p.lastAuditedInvoker = audited
	invokeStage := TopicStageInvoker(audited)
	// 搜索提供方身份三检（:5063-5087）。
	var searchProvider WebSearchFn
	if p.searchOverride == nil {
		return failedResult("search_provider_version_mismatch")
	}
	searchProvider = p.searchOverride
	actualSearchProviderID := INJECTEDSearchAuditProviderID
	expectedSearchProviderID := actualSearchProviderID
	if p.searchProviderID != "" {
		expectedSearchProviderID = p.searchProviderID
	}
	if actualSearchProviderID != OpenjiuwenNativeSearchProviderID ||
		actualSearchProviderID != expectedSearchProviderID ||
		pyStr(triggerKey["search_provider_id"]) != expectedSearchProviderID {
		return failedResult("search_provider_version_mismatch")
	}
	// 本轮摘要与其持久化在同一 scope 锁内（:5089-5095）。
	lock := p.summaryLockFor(snapshot.ContextScope)
	lock.Lock()
	summaryBatch, summaryErr := p.SummarizeSessions(snapshot, config, invokeStage)
	var persistErr error
	if summaryErr == nil {
		persistErr = p.PersistSessionSummaryBatch(summaryBatch)
	}
	lock.Unlock()
	if summaryErr != nil {
		return failedResult(rejectionReasonOf(summaryErr))
	}
	if persistErr != nil {
		return failedResult(rejectionReasonOf(persistErr))
	}
	result, generateErr := p.GeneratePrepared(snapshot, feedback, triggerKey,
		triggerOrigin, config, invokeStage, externalAudit, searchProvider,
		summaryBatch)
	if generateErr != nil {
		return failedResult(rejectionReasonOf(generateErr))
	}
	rawGroundingReceipt := payloadMapOrEmpty(result.Manifest["grounding_decision_receipt"])
	latestGroundingReceipt := rawGroundingReceipt
	rawGroundingEvidenceReceipt := payloadMapOrEmpty(result.Manifest["grounding_evidence_receipt"])
	latestGroundingEvidenceReceipt := rawGroundingEvidenceReceipt
	result.Manifest["external_invocation_audit"] = externalAudit.AsManifestReceipt()
	providerReceipts := audited.InvocationReceipts()
	// 摘要已写入后，RID 无可判的轮次不再向模型提问。
	judgedNothing := result.FailureReason == "rid_abstained" &&
		pyStr(payloadMapOrEmpty(payloadMapOrEmpty(result.Manifest["component_receipts"])[RID_COMPONENT_ID])["fallback_reason"]) == RIDNothingToJudge
	if providerReceipts == nil || (len(providerReceipts) == 0 && !judgedNothing) {
		return failedResult("missing_ark_invocation_receipts")
	}
	var terminalReceipts []map[string]any
	for _, receipt := range providerReceipts {
		if _, isMap := receipt["lineage"].(map[string]any); isMap {
			terminalReceipts = append(terminalReceipts, mapCopyAny(receipt))
		}
	}
	if len(providerReceipts) > 0 && len(terminalReceipts) == 0 {
		return failedResult("missing_ark_invocation_lineage")
	}
	if len(result.Cards) > 0 {
		// 接地改动后早已启动的评分被丢弃：池的血缘只留卡点名的评分请求。
		named := map[string]bool{}
		for _, card := range result.Cards {
			for _, rawReceipt := range payloadMapOrEmpty(card["component_receipts"]) {
				receipt := payloadMapOrEmpty(rawReceipt)
				named[pyStr(receipt["request_id"])] = true
			}
		}
		var keptReceipts []map[string]any
		for _, receipt := range terminalReceipts {
			lineage := payloadMapOrEmpty(receipt["lineage"])
			componentRequestID := pyStr(lineage["component_request_id"])
			stage := pyStr(receipt["stage"])
			if (stage == "v6_utility_scorer" || stage == "v6_diversity_scorer") &&
				!named[componentRequestID] {
				continue
			}
			keptReceipts = append(keptReceipts, receipt)
		}
		terminalReceipts = keptReceipts
	}
	var terminalReceiptList []any
	for _, receipt := range terminalReceipts {
		terminalReceiptList = append(terminalReceiptList, receipt)
	}
	result.Manifest["ark_invocation_receipts"] = terminalReceiptList
	if result.FailureReason == "rid_abstained" {
		rawAbstention, isMap := result.Manifest["rid_abstention_receipt"].(map[string]any)
		if !isMap {
			return failedResult("rid_abstention_receipt_missing")
		}
		abstentionReceipt := mapCopyAny(rawAbstention)
		abstentionReceipt["ark_invocation_receipts"] = terminalReceiptList
		abstentionReceipt["external_invocation_audit"] =
			result.Manifest["external_invocation_audit"]
		result.Manifest["rid_abstention_receipt"] = abstentionReceipt
		abstentionID := CurrentBackgroundGenerationAttemptID(context.Background())
		if abstentionID == "" {
			abstentionID = newUUIDHex()
		}
		abstentionEvent := map[string]any{
			"session_id":        "topic-starter-rid",
			"slate_id":          snapshot.ContextScope,
			"event_type":        "rid_abstention",
			"topic_id":          fmt.Sprintf("rid-abstention:%s", abstentionID),
			"policy_id":         POLICY_ID,
			"selection_mode":    "healthy_abstention",
			"propensity_reason": "not_applicable",
			"payload":           abstentionReceipt,
			"created_at":        toFloatOr(abstentionReceipt["decision_time"], 0),
		}
		created, recordErr := p.Store.RecordEvent(abstentionEvent)
		if recordErr != nil || !created {
			existing, queryErr := p.Store.QueryEvents(QueryEventsOptions{
				EventType:   "rid_abstention",
				TopicID:     stringPtr(fmt.Sprintf("rid-abstention:%s", abstentionID)),
				PolicyID:    stringPtr(POLICY_ID),
				Limit:       2,
				NewestFirst: true,
			})
			if queryErr != nil || len(existing) != 1 ||
				!anyEqual(existing[0]["payload"], abstentionReceipt) {
				return failedResult("rid_abstention_persistence_conflict")
			}
		}
	}
	for _, card := range result.Cards {
		var cardReceipts []any
		for _, receipt := range terminalReceipts {
			if ProviderReceiptAppliesToCard(receipt, card) {
				cardReceipts = append(cardReceipts, mapCopyAny(receipt))
			}
		}
		card["provider_invocation_receipts"] = cardReceipts
	}
	_ = latestGroundingReceipt
	_ = latestGroundingEvidenceReceipt
	return result
}

// RefreshNews 以热榜当前匹配替换一个全局池的新闻卡（topic_pipeline.py
// :5268-5393，D11/D22）。其余卡与池写入时完全一致：不检索、不重评、
// 过期不变。新卡回答池自身的生成输入：同一画像版本匹配、按池的讨论
// 覆盖判定、绑定池的输入快照；只有热榜动了。
func (p *TopicPipeline) RefreshNews(config *ArkBackgroundConfig,
	manifestID string, visitID string) *BackgroundGenerationResult {
	failed := func(triggerKey map[string]any, reason string) *BackgroundGenerationResult {
		return &BackgroundGenerationResult{
			Cards: nil,
			Manifest: map[string]any{
				"generation_trigger_key": triggerKey, "failure_reason": reason,
			},
			FailureReason: reason,
		}
	}
	loadedManifest, predecessorCards := LoadExactGlobalPool(p.Store, manifestID)
	var triggerKey map[string]any
	if loadedManifest != nil {
		triggerKey = payloadMapOrEmpty(loadedManifest["generation_trigger_key"])
	} else {
		triggerKey = map[string]any{}
	}
	if loadedManifest == nil ||
		NewsLeadCardId(loadedManifest) == "" {
		return failed(triggerKey, "no_news_card_to_replace")
	}
	predecessor := loadedManifest
	snapshot := p.contextLoader(GlobalNewChatScope)
	if snapshot == nil {
		return failed(triggerKey, "generation_input_contract_failed")
	}
	if pyIntOf(snapshot.ProfileProjection["profile_semantic_version"]) !=
		pyIntOf(triggerKey["profile_semantic_version"]) ||
		snapshot.ShortIntentState.ShortIntentVersion !=
			pyIntOf(triggerKey["short_intent_version"]) {
		return failed(triggerKey, "generation_input_version_mismatch")
	}
	updated := *snapshot
	updated.InputSnapshotID = pyStr(predecessor["generation_input_snapshot_id"])
	snapshot = &updated
	feedback, feedbackErr := InterestFeedbackLoad(p.Store, snapshot.State,
		GlobalNewChatScope, nil, p.Now())
	if feedbackErr != nil {
		return failed(triggerKey, "generation_input_contract_failed")
	}
	var summaries []*SessionSummaryArtifact
	for _, item := range pyListMaps(predecessor["session_summary_artifacts"]) {
		summary, err := SessionSummaryArtifactFromPayload(item)
		if err != nil {
			return failed(triggerKey, "generation_input_contract_failed")
		}
		summaries = append(summaries, summary)
	}
	externalAudit, auditErr := NewExternalInvocationAudit(p.Store, POLICY_ID,
		snapshot.InputSnapshotID, GlobalNewChatScope, visitID)
	if auditErr != nil {
		return failed(triggerKey, "generation_input_contract_failed")
	}
	ownedStageInvoker := p.stageInvokerFactory(config)
	invokeStage := TopicStageInvoker(AuditedArkStageInvoker(
		arkInvokerOf(ownedStageInvoker), externalAudit))
	var pull *HotListPull
	if p.hotList != nil {
		pull = p.hotList.Pull(context.Background())
	}
	news, absentReason := p.HotNewsLead(snapshot, pull, feedback, invokeStage)
	if news == nil {
		return failed(triggerKey, absentReason)
	}
	newsPlan := *news
	newsPlans := []map[string]any{newsPlan}
	AttachInterestSources(newsPlans, snapshot, summaries, feedback)
	newsPlan = newsPlans[0]
	newsPlan["discussion_coverage"] = DiscussionCoverage(summaries,
		snapshot.OtherScopeDiscussionCoverage)
	// 池的文档保留其编号；新文档接续其后。
	firstDocumentNumber := len(pyListMaps(payloadMapOrEmpty(
		payloadMapOrEmpty(predecessor["grounding_evidence_receipt"])["document_snapshot"])["documents"])) + 1
	grounding, groundErr := p.GroundPlans(newsPlans, snapshot, triggerKey, config,
		invokeStage, externalAudit, nil, firstDocumentNumber, nil, nil, nil,
		nil, nil)
	if groundErr != nil {
		if rejection, isRejection := groundErr.(*TerminalGenerationRejection); isRejection {
			return failed(triggerKey, rejection.Reason)
		}
		logger.Warn(logger.ComponentAgentCore).
			Msg("Topic Starter v6 news refresh failed")
		return failed(triggerKey, "news_refresh_failed")
	}
	if len(grounding.plans) != 1 {
		return failed(triggerKey, "news_refresh_failed")
	}
	grounded := grounding.plans[0]
	if !feedback.StillWanted(grounded) {
		return failed(triggerKey, "news_dismissed")
	}
	utilityReceipt, receiptErr := PipelineSuccessReceipt(
		UtilityBinding(config), snapshot.InputSnapshotID,
		fmt.Sprintf("v6-utility-%s", newUUIDHex()))
	if receiptErr != nil {
		return failed(triggerKey, "news_refresh_failed")
	}
	utility, scoreErr := ScoreInteractionUtility(snapshot.ProfileProjection,
		SelectionCandidatesFor(grounding.plans, GlobalNewChatScope, feedback, grounded),
		p.UtilityInvoker(grounding.plans, invokeStage), utilityReceipt)
	if scoreErr != nil {
		if rejection, isRejection := scoreErr.(*TerminalGenerationRejection); isRejection {
			return failed(triggerKey, rejection.Reason)
		}
		logger.Warn(logger.ComponentAgentCore).
			Msg("Topic Starter v6 news refresh failed")
		return failed(triggerKey, "news_refresh_failed")
	}
	if len(utility.Scores) != 1 {
		return failed(triggerKey, "news_refresh_failed")
	}
	score := utility.Scores[0]
	rejection := ""
	if !score.Admitted {
		rejection = "news_utility_rejected"
	}
	selectedIDs := []string{}
	if score.Admitted {
		selectedIDs = append(selectedIDs, score.CandidateID)
	}
	p.RecordQualityDecision(snapshot, utility, utility.Receipt.RequestID,
		nil, selectedIDs, rejection)
	if rejection != "" {
		return failed(triggerKey, rejection)
	}
	var providerReceipts []map[string]any
	for _, receipt := range p.lastAuditedInvoker.InvocationReceipts() {
		if _, isMap := receipt["lineage"].(map[string]any); isMap {
			providerReceipts = append(providerReceipts, receipt)
		}
	}
	return NewsOnlySuccessorResult(predecessor, predecessorCards, grounded,
		grounding, utilityReceipt, providerReceipts,
		externalAudit.AsManifestReceipt(), config, p.Now())
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// arkInvokerOf 接口到底层 *ArkStageInvoker（audited 包装需要具体类型）。
func arkInvokerOf(invoker TopicStageInvoker) *ArkStageInvoker {
	if direct, ok := invoker.(*ArkStageInvoker); ok {
		return direct
	}
	if wrapper, ok := invoker.(*v6ArkStageInvoker); ok {
		return wrapper.base
	}
	return nil
}

// rejectionReasonOf TerminalGenerationRejection → reason，其他错误统一码。
func rejectionReasonOf(err error) string {
	if rejection, isRejection := err.(*TerminalGenerationRejection); isRejection {
		return rejection.Reason
	}
	return "terminal_generation_failed"
}

var _ = strings.TrimSpace

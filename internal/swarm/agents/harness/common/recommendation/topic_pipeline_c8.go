package recommendation

// topic_pipeline_c8.go 一比一对齐上游 topic_product.py:3762-3840 的
// build_terminal_card/_card_expire_time、:3069-3084 的
// provider_receipt_applies_to_card，与 topic_pipeline.py:4135-4341 的
// _news_only_successor（C 段最后一件）。

import ()

// NewsOnlySuccessorResult 前任池换掉新闻卡、其余卡原样保留
// （topic_pipeline.py:4135-4341 _news_only_successor）。
// 已知偏差：函数名 Python 为 _news_only_successor；Go 同包常量
// NewsOnlySuccessor（topic_product.py:87）已占用该名，函数改后缀 Result。

// ──────────────────────────── 常量 ────────────────────────────

// EVIDENCE_COMPONENT_ID 网页证据组件 ID（topic_retrieval.py:36）
const EVIDENCE_COMPONENT_ID = "verbatim_web_evidence"

// ──────────────────────────── 导出函数 ────────────────────────────

// CardExpireTime 卡的过期时间（topic_product.py:3762-3778）：近期卡一天，
// 其余一周；新闻还在事件后 72 小时截断。
func CardExpireTime(plan map[string]any, now float64) float64 {
	if retained := plan["retained_expire_time"]; pythonTruthy(retained) {
		return toFloatOr(retained, 0)
	}
	expire := now + 7*24*60*60
	switch pyStr(plan["category"]) {
	case "profile_recent_progress", "project_domain_progress":
		expire = now + 24*60*60
	}
	if pyStr(plan["freshness_class"]) == NewsFreshnessClass {
		bundle, _, _ := payloadMap(plan["evidence_bundle"])
		if bundle == nil {
			bundle = map[string]any{}
		}
		published, ok := ParseISOTimestamp(pyStr(bundle["published_at"]))
		if ok {
			window := expire
			cut := published + NewsMaxWindowHours*60*60
			if cut < window {
				window = cut
			}
			expire = window
		}
	}
	return expire
}

// BuildTerminalCard 一份已接地、已评分的计划落成的池卡
// （topic_product.py:3780-3840 build_terminal_card）。
func BuildTerminalCard(plan map[string]any, rank int, batchID, contextScope string,
	projectID any, inputSnapshotID string, triggerKey map[string]any,
	componentReceipts map[string]any, listSearchVersion string,
	stateMaintenanceReceipt map[string]any, now float64) map[string]any {
	card := map[string]any{
		"card_id":                      pyStr(plan["candidate_id"]),
		"card_revision_id":             "v6-revision-" + newUUIDHex(),
		"generation_batch_id":          batchID,
		"selector_rank":                rank,
		"category":                     plan["category"],
		"context_scope":                contextScope,
		"project_id":                   projectID,
		"interest_summary_id":          plan["interest_summary_id"],
		"source_starter_id":            plan["source_starter_id"],
		"intent":                       plan["intent"],
		"title":                        plan["title"],
		"prompt":                       plan["prompt"],
		"effective_time":               now,
		"expire_time":                  CardExpireTime(plan, now),
		"evidence_bundle":              plan["evidence_bundle"],
		"search_provider_id":           triggerKey["search_provider_id"],
		"source_memory_unit_ids":       anySliceCopy(plan["source_memory_unit_ids"]),
		"source_locators":              anySliceCopy(plan["source_locators"]),
		"project_source_locators":      anySliceCopy(plan["project_source_locators"]),
		"generation_input_snapshot_id": inputSnapshotID,
		"profile_semantic_version":     triggerKey["profile_semantic_version"],
		"short_intent_version":         triggerKey["short_intent_version"],
		"component_receipts":           mapCopyAny(componentReceipts),
		"generation_config_version":    triggerKey["generation_config_version"],
		"corpus_version":               triggerKey["corpus_version"],
		"document_snapshot_version":    triggerKey["document_snapshot_version"],
		"retrieval_config_version":     triggerKey["retrieval_config_version"],
		"evidence_extractor_version":   triggerKey["evidence_extractor_version"],
		"list_search_version":          listSearchVersion,
		"policy_id":                    POLICY_ID,
		"payload_version":              PAYLOAD_VERSION,
	}
	if direction := plan["direction"]; pythonTruthy(direction) {
		card["direction"] = direction
	}
	if newsMatch := plan["news_match"]; pythonTruthy(newsMatch) {
		card["news_match"] = newsMatch
	}
	if searchReceipt, ok := plan["search_receipt"].(map[string]any); ok {
		card["search_receipt"] = mapCopyAny(searchReceipt)
	} else {
		card["search_receipt"] = nil
	}
	if stateMaintenanceReceipt != nil {
		card["state_maintenance_receipt"] = mapCopyAny(stateMaintenanceReceipt)
	}
	return card
}

// ProviderReceiptAppliesToCard 一条已校验厂商调用是否参与了该卡
// （topic_product.py:3069-3084 provider_receipt_applies_to_card）。
func ProviderReceiptAppliesToCard(receipt, card map[string]any) bool {
	lineage, ok := receipt["lineage"].(map[string]any)
	if !ok {
		return false
	}
	switch pyStr(lineage["binding_scope"]) {
	case "all_cards":
		return true
	case "candidate":
		for _, candidateID := range pyListStrings(lineage["candidate_ids"]) {
			if pyStr(card["card_id"]) == candidateID {
				return true
			}
		}
		return false
	case "starter":
		for _, starterID := range pyListStrings(lineage["starter_ids"]) {
			if pyStr(card["source_starter_id"]) == starterID {
				return true
			}
		}
		return false
	}
	return false
}

// NewsOnlySuccessor 前任池换掉新闻卡、其余卡原样保留
// （topic_pipeline.py:4135-4341 _news_only_successor）。只为被换新闻候选
// 服务的记录（报告与热榜兜底、接地决策、证据条目、外部操作、组件调用、
// 厂商小票与独占文档）随之离开；其他候选用到的文档留在快照里。
func NewsOnlySuccessorResult(predecessor map[string]any, predecessorCards []map[string]any,
	news map[string]any, grounding *pipelineGroundingBatch,
	utilityReceipt *LearnedComponentReceipt, providerReceipts []map[string]any,
	externalAudit map[string]any, config *ArkBackgroundConfig,
	now float64) *BackgroundGenerationResult {
	leadID := NewsLeadCardId(predecessor)
	replaced := map[string]bool{leadID: true}
	for _, rawReceipt := range pyListMaps(predecessor["ark_invocation_receipts"]) {
		if pyStr(rawReceipt["stage"]) != "v6_hot_news_matcher" {
			continue
		}
		lineage, _, _ := payloadMap(rawReceipt["lineage"])
		if lineage == nil {
			continue
		}
		for _, candidateID := range pyListStrings(lineage["candidate_ids"]) {
			replaced[candidateID] = true
		}
	}
	kept := func(candidateIDs any) bool {
		ids := map[string]bool{}
		for _, value := range pyListStrings(candidateIDs) {
			ids[value] = true
		}
		return len(ids) == 0 || !subsetEvents(ids, replaced)
	}

	newsID := pyStr(news["candidate_id"])
	triggerKey, _, _ := payloadMap(predecessor["generation_trigger_key"])
	if triggerKey == nil {
		triggerKey = map[string]any{}
	}
	evidenceReceipt, _ := news["evidence_receipt"].(*LearnedComponentReceipt)
	newsReceipts := map[string]any{
		RID_COMPONENT_ID:     predecessor["component_receipts"].(map[string]any)[RID_COMPONENT_ID],
		UTILITY_COMPONENT_ID: utilityReceipt.AsPayload(),
	}
	// 只有新闻卡一张的池把它的效用判定只用于那张卡；它同时判定过的常规
	// 候选从未成为卡。
	newsOnlyPredecessor := len(predecessorCards) > 0
	for _, card := range predecessorCards {
		if pyStr(card["card_id"]) != leadID {
			newsOnlyPredecessor = false
			break
		}
	}
	predecessorReceipts, _, _ := payloadMap(predecessor["component_receipts"])
	predecessorUtility := payloadMapOrEmpty(predecessorReceipts[UTILITY_COMPONENT_ID])
	predecessorUtilityRequestID := pyStr(predecessorUtility["request_id"])
	var invocations []map[string]any
	for _, item := range pyListMaps(predecessor["component_invocation_receipts"]) {
		if !kept(item["candidate_ids"]) {
			continue
		}
		newsOnlyUtility := newsOnlyPredecessor &&
			pyStr(item["component_id"]) == UTILITY_COMPONENT_ID &&
			pyStr(item["request_id"]) == predecessorUtilityRequestID
		if newsOnlyUtility {
			continue
		}
		invocations = append(invocations, mapCopyAny(item))
	}
	invocations = append(invocations, ComponentInvocationReceipt(
		utilityReceipt, "candidate", []string{newsID}, nil))
	if evidenceReceipt != nil {
		newsReceipts[EVIDENCE_COMPONENT_ID] = evidenceReceipt.AsPayload()
		invocations = append(invocations, ComponentInvocationReceipt(
			evidenceReceipt, "candidate", []string{newsID}, nil))
	}
	var arkReceipts []map[string]any
	for _, receipt := range pyListMaps(predecessor["ark_invocation_receipts"]) {
		if kept(receipt["lineage"].(map[string]any)["candidate_ids"]) {
			arkReceipts = append(arkReceipts, mapCopyAny(receipt))
		}
	}
	for _, receipt := range providerReceipts {
		arkReceipts = append(arkReceipts, mapCopyAny(receipt))
	}

	batchID := "v6-batch-" + newUUIDHex()
	newsCard := BuildTerminalCard(news, 1, batchID, GlobalNewChatScope, nil,
		pyStr(predecessor["generation_input_snapshot_id"]), triggerKey,
		newsReceipts, pyStr(payloadMapOrEmpty(predecessor["list_search"])["search_version"]),
		payloadMapOrNil(predecessor["state_maintenance_receipt"]), now)
	newsCard["component_invocation_receipts"] = filterMapCopies(invocations,
		func(item map[string]any) bool { return ComponentInvocationAppliesToCard(item, newsCard) })
	newsCard["provider_invocation_receipts"] = filterMapCopies(arkReceipts,
		func(receipt map[string]any) bool { return ProviderReceiptAppliesToCard(receipt, newsCard) })
	cards := []map[string]any{newsCard}
	for _, card := range predecessorCards {
		if pyStr(card["card_id"]) == leadID {
			continue
		}
		updated := jsonRoundTrip(card)
		updated["card_revision_id"] = "v6-revision-" + newUUIDHex()
		updated["generation_batch_id"] = batchID
		cards = append(cards, updated)
	}
	componentReceipts := map[string]any{}
	for componentID, receipt := range payloadMapOrEmpty(predecessor["component_receipts"]) {
		if componentID != EVIDENCE_COMPONENT_ID {
			componentReceipts[componentID] = receipt
		}
	}
	if newsOnlyPredecessor {
		componentReceipts[UTILITY_COMPONENT_ID] = newsReceipts[UTILITY_COMPONENT_ID]
	}
	// 池级证据小票是其第一张网页卡的，按一次生成登记它的方式。
	for _, card := range cards {
		receipts := payloadMapOrEmpty(card["component_receipts"])
		if evidence, present := receipts[EVIDENCE_COMPONENT_ID]; present {
			componentReceipts[EVIDENCE_COMPONENT_ID] = evidence
			break
		}
	}

	predecessorDecisionReceipt := payloadMapOrEmpty(predecessor["grounding_decision_receipt"])
	var decisions []map[string]any
	for _, decision := range pyListMaps(predecessorDecisionReceipt["candidate_decisions"]) {
		if !replaced[pyStr(decision["candidate_id"])] {
			decisions = append(decisions, mapCopyAny(decision))
		}
	}
	if grounding != nil && grounding.decisionReceipt != nil {
		for _, decision := range pyListMaps(grounding.decisionReceipt["candidate_decisions"]) {
			decisions = append(decisions, mapCopyAny(decision))
		}
	}
	admitted := 0
	for _, decision := range decisions {
		if pyStr(decision["decision"]) == "admit" {
			admitted++
		}
	}
	predecessorEvidenceReceipt := payloadMapOrEmpty(predecessor["grounding_evidence_receipt"])
	var keptCandidates []map[string]any
	for _, candidate := range pyListMaps(predecessorEvidenceReceipt["candidates"]) {
		if !replaced[pyStr(candidate["candidate_id"])] {
			keptCandidates = append(keptCandidates, mapCopyAny(candidate))
		}
	}
	referencedDocuments := map[string]bool{}
	for _, candidate := range keptCandidates {
		for _, fetch := range pyListMaps(candidate["fetches"]) {
			if fetch["document_revision_id"] != nil {
				referencedDocuments[pyStr(fetch["document_revision_id"])] = true
			}
		}
	}
	for _, candidate := range keptCandidates {
		extractor, ok := candidate["extractor"].(map[string]any)
		if !ok {
			continue
		}
		for _, documentID := range pyListStrings(extractor["input_document_revision_ids"]) {
			referencedDocuments[documentID] = true
		}
	}
	previousDocuments := payloadMapOrEmpty(predecessorEvidenceReceipt["document_snapshot"])
	var groundingEvidenceReceipt map[string]any
	var documentSnapshot map[string]any
	var addedDocuments map[string]any
	if grounding != nil && grounding.evidenceReceipt != nil {
		addedDocuments = payloadMapOrEmpty(grounding.evidenceReceipt["document_snapshot"])
	}
	var documents []map[string]any
	for _, document := range pyListMaps(previousDocuments["documents"]) {
		if referencedDocuments[pyStr(document["document_revision_id"])] {
			documents = append(documents, document)
		}
	}
	if addedDocuments != nil {
		documents = append(documents, pyListMaps(addedDocuments["documents"])...)
	}
	documentSnapshot = mapCopyAny(previousDocuments)
	documentIDs := make([]any, 0, len(documents))
	for _, document := range documents {
		documentIDs = append(documentIDs, document["document_revision_id"])
	}
	documentSnapshot["document_revision_ids"] = documentIDs
	documentSnapshot["documents"] = documents
	if grounding != nil && grounding.evidenceReceipt != nil {
		groundingEvidenceReceipt = mapCopyAny(predecessorEvidenceReceipt)
		groundingEvidenceReceipt["document_snapshot"] = documentSnapshot
		var candidates []any
		for _, candidate := range keptCandidates {
			candidates = append(candidates, candidate)
		}
		for _, candidate := range pyListMaps(payloadMapOrEmpty(grounding.evidenceReceipt)["candidates"]) {
			candidates = append(candidates, candidate)
		}
		groundingEvidenceReceipt["candidates"] = candidates
	}

	var auditInvocations []map[string]any
	predecessorAudit := payloadMapOrEmpty(predecessor["external_invocation_audit"])
	for _, invocation := range pyListMaps(predecessorAudit["invocations"]) {
		if !replaced[pyStr(invocation["candidate_id"])] {
			auditInvocations = append(auditInvocations, invocation)
		}
	}
	for _, invocation := range pyListMaps(externalAudit["invocations"]) {
		auditInvocations = append(auditInvocations, invocation)
	}

	var newsSearchReceipts map[string]any
	newsSearchReceipts = map[string]any{}
	for _, card := range cards {
		newsSearchReceipts[pyStr(card["card_id"])] = card["search_receipt"]
	}
	var cardRevisionIDs []any
	for _, card := range cards {
		cardRevisionIDs = append(cardRevisionIDs, card["card_revision_id"])
	}

	decisionAggregate := "abstain"
	if admitted == len(decisions) {
		decisionAggregate = "admit"
	} else if admitted > 0 {
		decisionAggregate = "partial"
	}
	manifest := jsonRoundTrip(predecessor)
	manifest["manifest_id"] = "v6-manifest-" + newUUIDHex()
	manifest["generation_batch_id"] = batchID
	manifest["card_revision_ids"] = cardRevisionIDs
	manifest["trigger_origin"] = "news_check"
	manifest["supersedes_manifest_id"] = predecessor["manifest_id"]
	manifest["successor_kind"] = NewsOnlySuccessor
	manifest["news_slot"] = NewsSlotRecord([]map[string]any{news}, "")
	manifest["search_receipts"] = newsSearchReceipts
	manifest["component_receipts"] = componentReceipts
	manifest["component_invocation_receipts"] = invocations
	manifest["ark_invocation_receipts"] = arkReceipts
	decisionReceipt := mapCopyAny(predecessorDecisionReceipt)
	decisionReceipt["decision"] = decisionAggregate
	decisionReceipt["candidate_decisions"] = decisions
	manifest["grounding_decision_receipt"] = decisionReceipt
	if groundingEvidenceReceipt != nil {
		manifest["grounding_evidence_receipt"] = groundingEvidenceReceipt
		manifest["document_snapshot"] = documentSnapshot
	} else {
		manifest["grounding_evidence_receipt"] = mapCopyAny(predecessorEvidenceReceipt)
		manifest["document_snapshot"] = documentSnapshot
	}
	manifest["external_invocation_audit"] = ExternalInvocationAuditReceipt(
		pyStr(predecessor["generation_input_snapshot_id"]), GlobalNewChatScope,
		auditInvocations)
	// 前任池回答了最新问题；本池只换新闻。
	selection := map[string]any{
		// restatements 记录之前落库的前任池没有留出任何一条。
		"restated_offered_candidates": []any{},
	}
	for key, value := range payloadMapOrEmpty(predecessor["rid_interest_selection"]) {
		selection[key] = value
	}
	selection["short_trigger_delta_event_ids"] = []any{}
	manifest["rid_interest_selection"] = selection
	manifest["provider_provenance"] = config.SafeProvenance()
	return &BackgroundGenerationResult{Cards: cards, Manifest: manifest}
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// payloadMapOrEmpty 映射或空表。
func payloadMapOrEmpty(value any) map[string]any {
	if m, ok := value.(map[string]any); ok {
		return m
	}
	return map[string]any{}
}

// payloadMapOrNil 映射或 nil。
func payloadMapOrNil(value any) map[string]any {
	if m, ok := value.(map[string]any); ok {
		return m
	}
	return nil
}

// filterMapCopies 过滤并浅拷贝。
func filterMapCopies(items []map[string]any, predicate func(map[string]any) bool) []map[string]any {
	out := []map[string]any{}
	for _, item := range items {
		if predicate(item) {
			out = append(out, mapCopyAny(item))
		}
	}
	return out
}

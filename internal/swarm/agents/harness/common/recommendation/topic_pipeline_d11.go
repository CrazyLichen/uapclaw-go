package recommendation

// topic_pipeline_d11.go 一比一对齐上游 topic_pipeline.py:8872-9167 的
// _assemble_result 与 :2860-2870 的 _exact_delta_linked_interest_ids。

import (
	"fmt"
)

// ──────────────────────────── 常量 ────────────────────────────

// PipelineCorpusVersion 固定语料版本（topic_sources.py:389）。
const PipelineCorpusVersion = "topic-starter-corpus-v1"

// ──────────────────────────── 导出函数 ────────────────────────────

// ExactDeltaLinkedInterestIds 短增量事件精确链接的兴趣 ID
// （topic_pipeline.py:2860-2870）。
func ExactDeltaLinkedInterestIds(rid *RIDResult, deltaEventIDs []string) map[string]bool {
	linked := map[string]bool{}
	delta := map[string]bool{}
	for _, eventID := range deltaEventIDs {
		delta[eventID] = true
	}
	for _, interest := range rid.TriggerInterests() {
		held := false
		for _, eventID := range interest.SourceEventIDs {
			if delta[eventID] {
				held = true
				break
			}
		}
		if !held {
			for _, locator := range interest.SourceLocators {
				if delta[locator.EventID] {
					held = true
					break
				}
			}
		}
		if held {
			linked[interest.InterestSummaryID] = true
		}
	}
	return linked
}

// AssembleResult 装配池结果：卡与 manifest（topic_pipeline.py:8872-9167
// _assemble_result）。
func (p *TopicPipeline) AssembleResult(selectedIDList []string,
	plans []map[string]any, selection *ListSearchResult, newsSlot map[string]any,
	snapshot *V6ContextSnapshot, summaries []*SessionSummaryArtifact,
	sessionSummaryCoverageReceipt map[string]any,
	generatedSessionSummaryIDs []string, sessionSummaryConfigVersion string,
	rid *RIDResult, interestSelection map[string]any,
	groundingDecisionReceipt, groundingEvidenceReceipt map[string]any,
	triggerKey map[string]any, triggerOrigin string, config *ArkBackgroundConfig,
	optionalLaneTimeouts []string, optionalLaneFailures map[string]string,
) (*BackgroundGenerationResult, error) {
	byID := map[string]map[string]any{}
	for _, plan := range plans {
		byID[pyStr(plan["candidate_id"])] = plan
	}
	selected := make([]map[string]any, 0, len(selectedIDList))
	for _, candidateID := range selectedIDList {
		selected = append(selected, byID[candidateID])
	}
	materializationReceipt, materializationErr := SessionSummaryMaterializationReceipt(snapshot,
		summaries, generatedSessionSummaryIDs, sessionSummaryConfigVersion)
	if materializationErr != nil {
		return nil, materializationErr
	}
	var requiredDeltaEventIDs []string
	if interestSelection != nil {
		requiredDeltaEventIDs = pyListStrings(interestSelection["short_trigger_delta_event_ids"])
	}
	if snapshot.ContextScope == GlobalNewChatScope && len(requiredDeltaEventIDs) > 0 {
		deltaInterestIDs := ExactDeltaLinkedInterestIds(rid, requiredDeltaEventIDs)
		exactDeltaLineage := func(plan map[string]any) bool {
			return deltaInterestIDs[pyStrOr(plan["interest_summary_id"], "")]
		}
		anyInSelected := false
		for _, plan := range selected {
			if exactDeltaLineage(plan) {
				anyInSelected = true
				break
			}
		}
		if !anyInSelected {
			return nil, &TerminalGenerationRejection{Reason: "global_pool_missing_short_delta_lineage"}
		}
		firstExposureHeld := false
		for index, plan := range selected {
			if index >= 4 {
				break
			}
			if exactDeltaLineage(plan) {
				firstExposureHeld = true
				break
			}
		}
		if !firstExposureHeld {
			return nil, &TerminalGenerationRejection{Reason: "first_exposure_missing_short_delta_lineage"}
		}
	}
	var evidenceReceipts []*LearnedComponentReceipt
	for _, plan := range selected {
		if receipt, ok := plan["evidence_receipt"].(*LearnedComponentReceipt); ok {
			evidenceReceipts = append(evidenceReceipts, receipt)
		}
	}
	anyWebPlan := false
	for _, plan := range selected {
		if HostEvidence(plan) == nil {
			anyWebPlan = true
			break
		}
	}
	if len(evidenceReceipts) == 0 && anyWebPlan {
		return nil, &TerminalGenerationRejection{Reason: "missing_web_evidence_receipt"}
	}
	commonReceipts := map[string]*LearnedComponentReceipt{
		RID_COMPONENT_ID: rid.Receipt, UTILITY_COMPONENT_ID: selection.UtilityReceipt,
	}
	if selection.DiversityReceipt != nil {
		commonReceipts[DIVERSITY_COMPONENT_ID] = selection.DiversityReceipt
	}
	if len(evidenceReceipts) > 0 {
		commonReceipts[EVIDENCE_COMPONENT_ID] = evidenceReceipts[0]
	}
	for componentID, receipt := range commonReceipts {
		if !ProviderMatchesTrigger(triggerKey, componentID, receipt) {
			return nil, &TerminalGenerationRejection{Reason: "component_trigger_version_mismatch"}
		}
	}
	candidateIDs := make([]string, 0, len(plans))
	for _, plan := range plans {
		candidateIDs = append(candidateIDs, pyStr(plan["candidate_id"]))
	}
	newsCandidateID := ""
	if newsSlot != nil {
		newsCandidateID = pyStr(newsSlot["candidate_id"])
	}
	componentInvocations := []map[string]any{
		ComponentInvocationReceipt(rid.Receipt, "all_cards", nil, nil),
		ComponentInvocationReceipt(selection.UtilityReceipt, "candidate",
			candidateIDs, nil),
	}
	if selection.DiversityReceipt != nil {
		nonNews := []string{}
		for _, candidateID := range candidateIDs {
			if candidateID != newsCandidateID {
				nonNews = append(nonNews, candidateID)
			}
		}
		componentInvocations = append(componentInvocations,
			ComponentInvocationReceipt(selection.DiversityReceipt, "candidate",
				nonNews, nil))
	}
	for _, plan := range plans {
		planEvidenceReceipt, ok := plan["evidence_receipt"].(*LearnedComponentReceipt)
		if !ok {
			continue
		}
		componentInvocations = append(componentInvocations,
			ComponentInvocationReceipt(planEvidenceReceipt, "candidate",
				[]string{pyStr(plan["candidate_id"])}, nil))
	}

	documentSnapshot := mapCopyAny(payloadMapOrEmpty(groundingEvidenceReceipt["document_snapshot"]))
	for _, plan := range plans {
		if _, hasManifest := plan["document_snapshot_manifest"]; !hasManifest {
			continue
		}
		if !anyEqual(plan["document_snapshot_manifest"], documentSnapshot) {
			return nil, &TerminalGenerationRejection{Reason: "document_snapshot_membership_mismatch"}
		}
	}
	documentMembership := map[string]bool{}
	for _, id := range pyListStrings(documentSnapshot["document_revision_ids"]) {
		documentMembership[id] = true
	}
	anyWebPlanAll := false
	for _, plan := range plans {
		if HostEvidence(plan) == nil {
			anyWebPlanAll = true
			break
		}
	}
	if len(documentMembership) == 0 && anyWebPlanAll {
		return nil, &TerminalGenerationRejection{Reason: "missing_document_snapshot_membership"}
	}
	for _, plan := range plans {
		if HostEvidence(plan) != nil {
			continue
		}
		exactSpans := pyListMaps(payloadMapOrEmpty(plan["evidence_bundle"])["exact_spans"])
		if len(exactSpans) == 0 {
			return nil, &TerminalGenerationRejection{Reason: "evidence_outside_document_snapshot"}
		}
		for _, span := range exactSpans {
			if !documentMembership[pyStr(span["document_revision_id"])] {
				return nil, &TerminalGenerationRejection{Reason: "evidence_outside_document_snapshot"}
			}
		}
	}

	now := p.Now()
	batchID := fmt.Sprintf("v6-batch-%s", newUUIDHex())
	var cards []map[string]any
	for rank, plan := range selected {
		cardReceipts := map[string]*LearnedComponentReceipt{}
		for componentID, receipt := range commonReceipts {
			if componentID == EVIDENCE_COMPONENT_ID {
				continue
			}
			if componentID == DIVERSITY_COMPONENT_ID &&
				pyStr(plan["candidate_id"]) == newsCandidateID {
				continue
			}
			cardReceipts[componentID] = receipt
		}
		if receipt, ok := plan["evidence_receipt"].(*LearnedComponentReceipt); ok {
			cardReceipts[EVIDENCE_COMPONENT_ID] = receipt
		}
		receiptPayloads := map[string]any{}
		for componentID, receipt := range cardReceipts {
			receiptPayloads[componentID] = receipt.AsPayload()
		}
		cards = append(cards, BuildTerminalCard(plan, rank+1, batchID,
			snapshot.ContextScope, snapshot.ProjectID, snapshot.InputSnapshotID,
			triggerKey, receiptPayloads, selection.SearchVersion,
			payloadMapOrNil(snapshot.StateMaintenanceReceipt), now))
	}
	for _, card := range cards {
		var invocations []map[string]any
		for _, invocation := range componentInvocations {
			if ComponentInvocationAppliesToCard(invocation, card) {
				invocations = append(invocations, mapCopyAny(invocation))
			}
		}
		card["component_invocation_receipts"] = invocations
	}
	manifestID := fmt.Sprintf("v6-manifest-%s", newUUIDHex())
	preferencePairs, pairsErr := MineHierarchicalPairs(&selection.BoundedBeamTrace)
	if pairsErr != nil {
		return nil, pairsErr
	}
	pairPayloads := make([]any, 0, len(preferencePairs))
	for _, pair := range preferencePairs {
		pairPayloads = append(pairPayloads, pair.AsPayload())
	}
	searchReceipts := map[string]any{}
	for _, plan := range selected {
		if receipt, ok := plan["search_receipt"].(map[string]any); ok {
			searchReceipts[pyStr(plan["candidate_id"])] = mapCopyAny(receipt)
		} else {
			searchReceipts[pyStr(plan["candidate_id"])] = nil
		}
	}
	commonReceiptPayloads := map[string]any{}
	for componentID, receipt := range commonReceipts {
		commonReceiptPayloads[componentID] = receipt.AsPayload()
	}
	decisionPayloads := make([]any, 0, len(selection.Decisions))
	for _, decision := range selection.Decisions {
		reasons := make([]any, 0, len(decision.Reasons))
		for _, reason := range decision.Reasons {
			reasons = append(reasons, reason)
		}
		decisionPayloads = append(decisionPayloads, map[string]any{
			"candidate_id": decision.CandidateID, "selected": decision.Selected,
			"reasons": reasons,
		})
	}
	traceEntries := make([]any, 0, len(selection.BoundedBeamTrace.Entries))
	for _, entry := range selection.BoundedBeamTrace.Entries {
		entryIDs := make([]any, 0, len(entry.CandidateIDs))
		for _, id := range entry.CandidateIDs {
			entryIDs = append(entryIDs, id)
		}
		entryReasons := make([]any, 0, len(entry.FailureReasons))
		for _, reason := range entry.FailureReasons {
			entryReasons = append(entryReasons, reason)
		}
		traceEntries = append(traceEntries, map[string]any{
			"trace_index": entry.TraceIndex, "candidate_ids": entryIDs,
			"depth": entry.Depth, "status": entry.Status,
			"retained_in_beam":                    entry.RetainedInBeam,
			"hard_constraints_pass":               entry.HardConstraintsPass,
			"utility_pass":                        boolOrNil(entry.UtilityPass),
			"diversity_pass":                      boolOrNil(entry.DiversityPass),
			"mean_utility_nullable":               nilIfEmptyToAny(entry.MeanUtilityNullable),
			"mean_diversity_nullable":             nilIfEmptyToAny(entry.MeanDiversityNullable),
			"minimum_pairwise_diversity_nullable": nilIfEmptyToAny(entry.MinimumPairwiseDiversityNullable),
			"objective_nullable":                  nilIfEmptyToAny(entry.ObjectiveNullable),
			"failure_reasons":                     entryReasons,
		})
	}
	preferredIDs := make([]any, 0, len(selection.BoundedBeamTrace.PreferredCandidateIDs))
	for _, id := range selection.BoundedBeamTrace.PreferredCandidateIDs {
		preferredIDs = append(preferredIDs, id)
	}
	listSearch := map[string]any{
		"search_version":      selection.SearchVersion,
		"candidate_decisions": decisionPayloads,
		"bounded_beam_trace": map[string]any{
			"entries":                  traceEntries,
			"preferred_candidate_ids":  preferredIDs,
			"target_size":              selection.BoundedBeamTrace.TargetSize,
			"expansion_count":          selection.BoundedBeamTrace.ExpansionCount,
			"beam_width":               selection.BoundedBeamTrace.BeamWidth,
			"max_expansions":           selection.BoundedBeamTrace.MaxExpansions,
			"utility_scorer_version":   selection.BoundedBeamTrace.UtilityScorerVersion,
			"diversity_scorer_version": selection.BoundedBeamTrace.DiversityScorerVersion,
			"search_version":           selection.BoundedBeamTrace.SearchVersion,
		},
	}
	if selection.FailureReason != "" {
		// 搜索为何没找到列表——新闻卡在失败的搜索上单独成立。
		listSearch["failure_reason"] = selection.FailureReason
	}
	summaryPayloads := make([]any, 0, len(summaries))
	for _, summary := range summaries {
		summaryPayloads = append(summaryPayloads, summary.AsPayload())
	}
	retainedCandidates := map[string]any{}
	for _, plan := range plans {
		if pythonTruthy(plan["retained_from_snapshot_id"]) {
			retainedCandidates[pyStr(plan["candidate_id"])] =
				pyStr(plan["retained_from_snapshot_id"])
		}
	}
	manifest := map[string]any{
		"manifest_id": manifestID, "generation_batch_id": batchID,
		"card_revision_ids": func() []any {
			ids := make([]any, 0, len(cards))
			for _, card := range cards {
				ids = append(ids, card["card_revision_id"])
			}
			return ids
		}(),
		"trigger_origin":                triggerOrigin,
		"local_date":                    triggerKey["local_date"],
		"context_scope":                 snapshot.ContextScope,
		"project_id":                    snapshot.ProjectID,
		"generation_trigger_key":        triggerKey,
		"generation_input_snapshot_id":  snapshot.InputSnapshotID,
		"serving_compatibility_key":     BuildServingCompatibilityKey(triggerKey),
		"profile_semantic_version":      triggerKey["profile_semantic_version"],
		"short_intent_version":          triggerKey["short_intent_version"],
		"negative_preference_version":   triggerKey["negative_preference_version"],
		"document_snapshot":             documentSnapshot,
		"search_provider_id":            triggerKey["search_provider_id"],
		"search_receipts":               searchReceipts,
		"component_receipts":            commonReceiptPayloads,
		"component_invocation_receipts": componentInvocations,
		"generation_config_version":     triggerKey["generation_config_version"],
		"supersedes_manifest_id": nilIfEmptyToStr(
			p.LatestScopeManifestID(snapshot.ContextScope) != "",
			p.LatestScopeManifestID(snapshot.ContextScope)),
		"rid_result":                              rid.AsDownstreamPayload(),
		"session_summary_artifacts":               summaryPayloads,
		"session_summary_coverage_receipt":        mapCopyAny(sessionSummaryCoverageReceipt),
		"session_summary_materialization_receipt": materializationReceipt,
		"rid_interest_selection": func() any {
			if interestSelection == nil {
				return nil
			}
			return mapCopyAny(interestSelection)
		}(),
		"grounding_decision_receipt": mapCopyAny(groundingDecisionReceipt),
		"grounding_evidence_receipt": mapCopyAny(groundingEvidenceReceipt),
		"retained_candidates":        retainedCandidates,
		"list_search":                listSearch,
		"preference_pairs":           pairPayloads,
		"provider_provenance":        config.SafeProvenance(),
		"policy_id":                  POLICY_ID,
		"payload_version":            PAYLOAD_VERSION,
	}
	if newsSlot != nil {
		manifest["news_slot"] = mapCopyAny(newsSlot)
	}
	if len(optionalLaneTimeouts) > 0 {
		timeoutList := make([]any, 0, len(optionalLaneTimeouts))
		for _, lane := range optionalLaneTimeouts {
			timeoutList = append(timeoutList, lane)
		}
		manifest["optional_lane_timeouts"] = timeoutList
	}
	if len(optionalLaneFailures) > 0 {
		failures := map[string]any{}
		for lane, reason := range optionalLaneFailures {
			failures[lane] = reason
		}
		manifest["optional_lane_failures"] = failures
	}
	if snapshot.StateMaintenanceReceipt != nil {
		manifest["state_maintenance_receipt"] =
			mapCopyAny(snapshot.StateMaintenanceReceipt)
	}
	corpusTextByID := map[string]string{}
	starters, corpusErr := LoadFixedStarterCorpus(PipelineCorpusVersion)
	if corpusErr == nil {
		for _, starter := range starters {
			corpusTextByID[starter.StarterID] = starter.Text
		}
	}
	return &BackgroundGenerationResult{
		Cards: cards, Manifest: manifest, CorpusTextByID: corpusTextByID,
	}, nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// boolOrNil 可空 bool。
func boolOrNil(value *bool) any {
	if value == nil {
		return nil
	}
	return *value
}

// nilIfEmptyToAny 可空浮点。
func nilIfEmptyToAny(value *float64) any {
	if value == nil {
		return nil
	}
	return *value
}

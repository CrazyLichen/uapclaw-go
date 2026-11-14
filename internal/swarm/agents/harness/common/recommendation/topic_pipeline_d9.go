package recommendation

// topic_pipeline_d9.go 一比一对齐上游 topic_pipeline.py:7714-8560 的
// _freeze_documents/_finalize_recent_plans/_ground_plans。
// 已知偏差（§5 并发映射）：候选证据并发 → goroutine+逐候选 channel；
// on_candidate_ready 回调触发的接地任务 → goroutine；
// asyncio.shield → channel 接收等价（冻结循环按计划顺序逐个汇合）。

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"sync"
	"time"
)

// regexpRegexp 别名形态。
type regexpRegexp = regexp.Regexp

// regexpMustCompile 编译。
func regexpMustCompile(pattern string) *regexpRegexp { return regexp.MustCompile(pattern) }

// ──────────────────────────── 常量 ────────────────────────────

// MaxFrozenDocumentChars 冻结文档正文字符上限（topic_pipeline.py:206）。
const MaxFrozenDocumentChars = 8000

// ──────────────────────────── 导出函数 ────────────────────────────

// FreezeDocuments 冻结一批网页候选的搜索行与抓取页
// （topic_pipeline.py:7714-8023 _freeze_documents）。每个候选的抓取在其
// 搜索答复后开始；页面按计划顺序与其他候选一起冻结。
func (p *TopicPipeline) FreezeDocuments(plans []map[string]any,
	snapshot *V6ContextSnapshot, triggerKey map[string]any,
	externalAudit *ExternalInvocationAudit, searchProvider WebSearchFn,
	firstDocumentNumber int, lanes *OptionalLanes,
	earlyEvidence map[string]*candidateEvidenceFuture,
	onCandidateReady func(map[string]any, *frozenEvidenceBatch),
	fetchTasks *FetchTasks) (*frozenEvidenceBatch, error) {
	if lanes == nil {
		lanes = NewOptionalLanes()
	}
	var webPlans []map[string]any
	for _, plan := range plans {
		if HostEvidence(plan) == nil {
			webPlans = append(webPlans, plan)
		}
	}
	snapshotDate := pyStr(triggerKey["local_date"])
	effectiveSearch := searchProvider
	if effectiveSearch == nil {
		effectiveSearch = p.searchOverride
	}
	if effectiveSearch == nil {
		return nil, &TerminalGenerationRejection{Reason: "web_search_provider_unavailable"}
	}
	// 提早开始的候选证据直接汇合，不再搜索。
	ownsFetches := fetchTasks == nil
	if fetchTasks == nil {
		fetchTasks = NewFetchTasks()
	}
	type evidenceOutcome struct {
		plan   map[string]any
		result *CandidateEvidence
		err    error
	}
	candidateChannels := map[string]chan evidenceOutcome{}
	var candidateOrder []string
	for _, plan := range webPlans {
		candidateID := pyStr(plan["candidate_id"])
		candidateOrder = append(candidateOrder, candidateID)
		channel := make(chan evidenceOutcome, 1)
		candidateChannels[candidateID] = channel
		if future, present := earlyEvidence[candidateID]; present {
			go func(future *candidateEvidenceFuture, channel chan evidenceOutcome) {
				channel <- evidenceOutcome{plan: plan, result: <-future.done}
				close(channel)
			}(future, channel)
			continue
		}
		go func(plan map[string]any, channel chan evidenceOutcome) {
			result := p.CandidateEvidenceFor(plan, snapshot, externalAudit,
				effectiveSearch, lanes, fetchTasks)
			channel <- evidenceOutcome{plan: plan, result: result}
			close(channel)
		}(plan, channel)
	}

	searchAuditRefs := map[string]map[string]string{}
	evidenceByCandidate := map[string]map[string]any{}
	searchReceipts := map[string]map[string]any{}
	preExtractionRejections := map[string][2]string{}
	fetchFailureByCandidate := map[string]string{}
	snapshotVersion := pyStr(triggerKey["document_snapshot_version"])
	var documents []FrozenDocumentRevision
	type revisionKey struct {
		finalURL      string
		content       string
		publishedAt   string
		provenanceKey string
	}
	revisionIDs := map[revisionKey]string{}
	fetchedByCandidate := map[string][]string{}
	allowedByCandidate := map[string][]string{}
	operationRefsByCandidate := map[string][]map[string]string{}

	currentBatch := func() *frozenEvidenceBatch {
		var frozen *FrozenDocumentSnapshot
		if len(documents) > 0 {
			frozen = &FrozenDocumentSnapshot{
				SnapshotDate: snapshotDate, SnapshotVersion: snapshotVersion,
				Documents: documents,
			}
		}
		return &frozenEvidenceBatch{
			frozen: frozen, searchReceipts: searchReceipts,
			allowedByCandidate:  allowedByCandidate,
			rejections:          preExtractionRejections,
			operationRefs:       operationRefsByCandidate,
			evidenceByCandidate: evidenceByCandidate,
		}
	}

	for _, plan := range webPlans {
		candidateID := pyStr(plan["candidate_id"])
		outcome := <-candidateChannels[candidateID]
		gathered := outcome.result
		if gathered == nil {
			reason := "provider_invocation_failed"
			if rejection, isRejection := outcome.err.(*TerminalGenerationRejection); isRejection {
				reason = rejection.Reason
			}
			return nil, &TerminalGenerationRejection{Reason: reason}
		}
		searchAuditRefs[candidateID] = gathered.SearchAuditRef
		evidenceByCandidate[candidateID] = gathered.Evidence
		if gathered.SearchRejection != nil {
			preExtractionRejections[candidateID] = *gathered.SearchRejection
		}
		if gathered.SearchReceipt != nil {
			searchReceipts[candidateID] = gathered.SearchReceipt
		}
		fetchAuditRefsByCandidate := gathered.FetchAuditRefs
		if gathered.FetchFailure != "" {
			fetchFailureByCandidate[candidateID] = gathered.FetchFailure
		}
		var candidateRevisionIDs []string
		for _, triple := range gathered.FetchedRows {
			searchRow := triple[0].(map[string]any)
			page := triple[1].(map[string]any)
			fetchEvidence := triple[2].(map[string]any)
			rawURL := pyStr(searchRow["url"])
			status := pyIntOf(page["status_code"])
			content := strings.TrimSpace(pyStr(page["content"]))
			if !(200 <= status && status < 300) || content == "" {
				continue
			}
			finalURL := pyStrOr(page["url"], rawURL)
			var assessment SourceAssessment
			if pyStr(plan["source_channel"]) == "news_feed" {
				assessment = NewsSourceAssessment(finalURL)
			} else {
				assessment = p.classifySource(finalURL, content)
			}
			if assessment.Publisher == "" || assessment.Kind == "unknown" {
				continue
			}
			rawPageDate := strings.TrimSpace(pyStr(page["published_at"]))
			publishedAt := isoPublishedAtOrNone(rawPageDate)
			if recentCategories[pyStr(plan["category"])] &&
				pyStr(page["published_at_source"]) == "sqlite.release_heading" &&
				strings.Contains(content, "Prior changes from version") {
				// 补丁发布页只留具体补丁段（:7821-7828）
				marker := fmt.Sprintf("Changes in this specific patch release, version [0-9.]+ \\(%s\\):",
					regexpEscape(rawPageDate))
				patchRE := regexpCompile(marker)
				if patchRE == nil {
					continue
				}
				loc := patchRE.FindStringIndex(content)
				if loc == nil {
					continue
				}
				content = content[loc[0]:]
			}
			immutableContent := truncateRunes(content, MaxFrozenDocumentChars)
			var provenance *PublicationDateProvenance
			apiURL := strings.TrimSpace(pyStr(page["api_url"]))
			rawListedDate := ""
			if finalURL == rawURL || apiURL != "" {
				rawListedDate = strings.TrimSpace(pyStr(searchRow["structured_search_date"]))
			}
			listedAt := unambiguousSearchPublishedAtOrNone(rawListedDate)
			pageDatedAfterListing := ""
			// 新闻源的 pubDate 早于页面后来被改写的日期。
			if searchRow["is_news_publication_date"] == true &&
				publishedAt != "" && listedAt != "" {
				pageTime, pageOK := ParseISOTimestamp(publishedAt)
				listedTime, listedOK := ParseISOTimestamp(listedAt)
				if pageOK && listedOK {
					datedLater := false
					if strings.ContainsAny(rawListedDate, "T:") {
						datedLater = pageTime > listedTime
					} else {
						datedLater = int(pageTime/86400) > int(listedTime/86400)+1
					}
					if datedLater {
						publishedAt = ""
						pageDatedAfterListing = rawPageDate
					}
				}
			}
			if publishedAt != "" {
				provenance = &PublicationDateProvenance{
					SourceType: "page_explicit_metadata",
					SourceField: pyStrOr(page["published_at_source"],
						"fetch_result.published_at"),
					RawValue: rawPageDate,
					ListedAt: nilIfEmptyToStr(rawListedDate != "", rawListedDate),
					APIURL:   nilIfEmptyToStr(apiURL != "", apiURL),
				}
			} else if finalURL == rawURL && searchRow["is_news_publication_date"] == true {
				searchProviderID := strings.TrimSpace(pyStr(searchRow["search_provider_id"]))
				searchReceiptID := strings.TrimSpace(pyStr(searchRow["search_receipt_id"]))
				if listedAt != "" && searchProviderID != "" && searchReceiptID != "" {
					publishedAt = listedAt
					provenance = &PublicationDateProvenance{
						SourceType: "structured_search_result", SourceField: "date",
						RawValue: rawListedDate, SearchProviderID: searchProviderID,
						SearchReceiptID: searchReceiptID, SearchResultURL: rawURL,
						PagePublishedAt: pageDatedAfterListing,
					}
				}
			}
			if publishedAt == "" {
				identity, _, _ := VersionedDocumentIdentity(finalURL, "")
				for _, triple := range gathered.FetchedRows {
					dateRow := triple[0].(map[string]any)
					datePage := triple[1].(map[string]any)
					dateFetch := triple[2].(map[string]any)
					dateURL := pyStrOr(datePage["url"], pyStr(dateRow["url"]))
					dateValue := isoPublishedAtOrNone(pyStr(datePage["published_at"]))
					dateSource := pyStr(datePage["published_at_source"])
					if identity == "" || dateURL == finalURL ||
						dateValue == "" || dateSource == "" {
						continue
					}
					dateIdentity, _, _ := VersionedDocumentIdentity(dateURL,
						pyStr(datePage["content"]))
					if dateIdentity != identity {
						continue
					}
					if !(200 <= pyIntOf(datePage["status_code"]) &&
						pyIntOf(datePage["status_code"]) < 300) {
						continue
					}
					if strings.TrimSpace(pyStr(datePage["content"])) == "" {
						continue
					}
					// 引用日期不能确立修订版的日期。
					if strings.SplitN(identity, "\x00", 2)[1] != "v1" &&
						dateSource != "arxiv.version_history" {
						continue
					}
					identityParts := strings.SplitN(identity, "\x00", 2)
					linked := map[string]any{
						"date_page_url": dateURL, "body_url": finalURL,
						"document_id": identityParts[0], "version": identityParts[1],
						"date_page_excerpt": truncateRunes(pyStr(datePage["content"]),
							MaxFrozenDocumentChars),
						"operation_id":     dateFetch["operation_id"],
						"request_id":       dateFetch["request_id"],
						"http_status_code": pyIntOf(datePage["status_code"]),
					}
					publishedAt = dateValue
					provenance = &PublicationDateProvenance{
						SourceType: "linked_document_metadata", SourceField: dateSource,
						RawValue: pyStr(datePage["published_at"]), LinkedPage: linked,
					}
					break
				}
			}
			var provenanceKey string
			if provenance != nil {
				provenanceKey = sortedCompactJSON(provenance.AsPayload())
			}
			key := revisionKey{finalURL, immutableContent, publishedAt, provenanceKey}
			documentRevisionID, known := revisionIDs[key]
			if !known {
				documentRevisionID = fmt.Sprintf("doc:%s:%d",
					snapshot.InputSnapshotID, len(documents)+firstDocumentNumber)
				revisionIDs[key] = documentRevisionID
				documents = append(documents, FrozenDocumentRevision{
					DocumentRevisionID: documentRevisionID, SourceURL: finalURL,
					Publisher: assessment.Publisher, SourceKind: assessment.Kind,
					PublishedAt: publishedAt, FetchedAt: p.Now(),
					ImmutableContent: immutableContent,
					SnapshotDate:     snapshotDate, SnapshotVersion: snapshotVersion,
					PublicationDateProvenance: provenance,
				})
			}
			held := false
			for _, id := range candidateRevisionIDs {
				if id == documentRevisionID {
					held = true
				}
			}
			if !held {
				candidateRevisionIDs = append(candidateRevisionIDs, documentRevisionID)
			}
			fetchEvidence["document_revision_id"] = documentRevisionID
		}
		fetchedByCandidate[candidateID] = candidateRevisionIDs
		if failureReason, hasFailure := fetchFailureByCandidate[candidateID]; hasFailure &&
			len(candidateRevisionIDs) == 0 {
			fetchedByCandidate[candidateID] = nil
			preExtractionRejections[candidateID] = [2]string{"fetch", failureReason}
		} else if _, rejected := preExtractionRejections[candidateID]; !rejected &&
			len(candidateRevisionIDs) == 0 {
			preExtractionRejections[candidateID] = [2]string{
				"admission", "no_eligible_fetched_document"}
		}
		documentByID := map[string]FrozenDocumentRevision{}
		for _, document := range documents {
			documentByID[document.DocumentRevisionID] = document
		}
		candidateDocuments := make([]FrozenDocumentRevision, 0, len(candidateRevisionIDs))
		for _, id := range candidateRevisionIDs {
			candidateDocuments = append(candidateDocuments, documentByID[id])
		}
		candidate, err := NewEvidenceCandidate(candidateID, pyStr(plan["title"]),
			pyStr(plan["primary_query"]), pyStr(plan["freshness_class"]),
			snapshot.ContextScope, pyStr(plan["category"]))
		if err != nil {
			return nil, err
		}
		allowed, err := EligibleDocumentRevisionIds(candidate, candidateDocuments, p.Now())
		if err != nil {
			return nil, err
		}
		allowedByCandidate[candidateID] = allowed
		if _, rejected := preExtractionRejections[candidateID]; !rejected &&
			len(allowed) == 0 {
			preExtractionRejections[candidateID] = [2]string{
				"admission", "no_fresh_eligible_document"}
		}
		operationRefs := []map[string]string{searchAuditRefs[candidateID]}
		operationRefs = append(operationRefs, fetchAuditRefsByCandidate...)
		operationRefsByCandidate[candidateID] = operationRefs
		if onCandidateReady != nil {
			onCandidateReady(plan, currentBatch())
		}
	}
	if ownsFetches {
		fetchTasks.CancelAllOwned()
	}
	return currentBatch(), nil
}

// FinalizeRecentPlans 近期计划的终稿标题
// （topic_pipeline.py:8025-8101 _finalize_recent_plans）。
func (p *TopicPipeline) FinalizeRecentPlans(plans []map[string]any,
	frozen *FrozenDocumentSnapshot, allowedByCandidate map[string][]string,
	snapshot *V6ContextSnapshot, config *ArkBackgroundConfig,
	invokeStage TopicStageInvoker, lanes *OptionalLanes) (map[string]string, error) {
	documentsByID := map[string]FrozenDocumentRevision{}
	if frozen != nil {
		for _, document := range frozen.Documents {
			documentsByID[document.DocumentRevisionID] = document
		}
	}
	var finalized []map[string]any
	for _, plan := range plans {
		if !recentCategories[pyStr(plan["category"])] {
			continue
		}
		if len(allowedByCandidate[pyStr(plan["candidate_id"])]) == 0 {
			continue
		}
		finalized = append(finalized, plan)
	}
	candidates := make([]any, 0, len(finalized))
	for _, plan := range finalized {
		docPayloads := make([]any, 0)
		for _, docID := range allowedByCandidate[pyStr(plan["candidate_id"])] {
			document := documentsByID[docID]
			docPayloads = append(docPayloads, document.AsPayload(true))
		}
		interest := pyStrOr(plan["persona_context"], pyStr(plan["identity_anchor"]))
		candidates = append(candidates, map[string]any{
			"candidate_id": plan["candidate_id"], "interest": interest,
			"draft_title": plan["title"], "intent": plan["intent"],
			"primary_query": plan["primary_query"], "documents": docPayloads,
		})
	}
	if len(candidates) == 0 {
		return map[string]string{}, nil
	}
	payload := compactJSON(map[string]any{
		"input_snapshot_id":   snapshot.InputSnapshotID,
		"discussion_coverage": pyListAny(payloadMapOrEmpty(plans[0]["previously_offered"])["discussion_coverage"]),
		"previously_offered":  plans[0]["previously_offered"],
		"candidates":          candidates,
		"request_id":          "v6-finalize-" + newUUIDHex(),
	})
	if p.StageTokenCounterFactory == nil {
		return nil, &TerminalGenerationRejection{Reason: "recent_finalization_context_limit"}
	}
	counter, err := p.StageTokenCounterFactory(config.Model)
	if err != nil {
		return nil, &TerminalGenerationRejection{Reason: "recent_finalization_context_limit"}
	}
	if config.ContextWindowTokens == 0 || counter.EstimateStageTokens(
		pipelineRecentFinalizerPrompt, payload, 4096) > config.ContextWindowTokens {
		return nil, &TerminalGenerationRejection{Reason: "recent_finalization_context_limit"}
	}
	laneSet := map[string]bool{}
	for _, plan := range finalized {
		laneSet[OptionalLaneOf(plan)] = true
	}
	var laneList []string
	for lane := range laneSet {
		laneList = append(laneList, lane)
	}
	withinCtx, withinCancel := context.WithTimeout(context.Background(),
		30*time.Second)
	defer withinCancel()
	response, withinErr := lanes.Within(withinCtx, laneList,
		func(ctx context.Context) (any, error) {
			return invokeStage.Invoke(ctx, "v6_recent_finalizer",
				pipelineRecentFinalizerPrompt, payload)
		})
	if withinErr != nil {
		return nil, withinErr
	}
	RecordProviderValidationTopic(invokeStage, response, "business_validated")
	root, isMap := response.(map[string]any)
	if !isMap || len(root) != 1 {
		RecordProviderValidationTopic(invokeStage, response, "business_validation_failed")
		return nil, &TerminalGenerationRejection{Reason: "recent_finalization_output_invalid"}
	}
	rawCandidates, isList := root["candidates"].([]any)
	if !isList {
		RecordProviderValidationTopic(invokeStage, response, "business_validation_failed")
		return nil, &TerminalGenerationRejection{Reason: "recent_finalization_output_invalid"}
	}
	allowedIDs := map[string]bool{}
	for _, raw := range candidates {
		allowedIDs[pyStr(payloadMapOrEmpty(raw)["candidate_id"])] = true
	}
	titles := map[string]string{}
	for _, raw := range rawCandidates {
		item, isMap := raw.(map[string]any)
		if !isMap || len(item) != 2 {
			RecordProviderValidationTopic(invokeStage, response, "business_validation_failed")
			return nil, &TerminalGenerationRejection{Reason: "recent_finalization_output_invalid"}
		}
		if _, hasID := item["candidate_id"]; !hasID {
			RecordProviderValidationTopic(invokeStage, response, "business_validation_failed")
			return nil, &TerminalGenerationRejection{Reason: "recent_finalization_output_invalid"}
		}
		if _, hasTitle := item["title"]; !hasTitle {
			RecordProviderValidationTopic(invokeStage, response, "business_validation_failed")
			return nil, &TerminalGenerationRejection{Reason: "recent_finalization_output_invalid"}
		}
		candidateID := pyStr(item["candidate_id"])
		if !allowedIDs[candidateID] || func() bool {
			_, dup := titles[candidateID]
			return dup
		}() {
			RecordProviderValidationTopic(invokeStage, response, "business_validation_failed")
			return nil, &TerminalGenerationRejection{Reason: "recent_finalization_output_invalid"}
		}
		title, titleIsStr := item["title"].(string)
		if !titleIsStr || strings.TrimSpace(title) == "" {
			RecordProviderValidationTopic(invokeStage, response, "business_validation_failed")
			return nil, &TerminalGenerationRejection{Reason: "recent_finalization_output_invalid"}
		}
		titles[candidateID] = strings.TrimSpace(title)
	}
	return titles, nil
}

// GroundPlans 把一批计划接地：宿主证据直通、网页候选搜索抓取提取
// （topic_pipeline.py:8103-8560 _ground_plans）。
func (p *TopicPipeline) GroundPlans(plans []map[string]any,
	snapshot *V6ContextSnapshot, triggerKey map[string]any,
	config *ArkBackgroundConfig, invokeStage TopicStageInvoker,
	externalAudit *ExternalInvocationAudit, searchProvider WebSearchFn,
	firstDocumentNumber int, lanes *OptionalLanes,
	onRegularReady func([]map[string]any), onLocalReady func([]map[string]any),
	earlyEvidence map[string]*candidateEvidenceFuture,
	fetchTasks *FetchTasks) (*pipelineGroundingBatch, error) {
	if lanes == nil {
		lanes = NewOptionalLanes()
	}
	if onLocalReady != nil {
		// 用户自身历史接地的计划不需要下面搜索与抓取带来的任何东西。
		var local []map[string]any
		for _, plan := range plans {
			planHostEvidence := HostEvidence(plan)
			if planHostEvidence == nil {
				continue
			}
			if CardCopyRejectionReason(plan["title"], CardTitleMaxChars) != "" {
				continue
			}
			local = append(local, HostGroundedPlan(plan, planHostEvidence))
		}
		if len(local) > 0 {
			onLocalReady(local)
		}
	}
	operationRefsByCandidate := map[string][]map[string]string{}
	finalTitles := map[string]string{}
	finalizationErrors := map[string]string{}
	providerVersion := pyStrOr(config.ExpectedModel, config.Model)
	binding, err := PipelineAPIBinding(EVIDENCE_COMPONENT_ID, EvidenceAPIProviderID,
		providerVersion, EvidenceExtractorVersion)
	if err != nil {
		return nil, err
	}
	decisionsByCandidate := map[string]map[string]any{}
	var decisionsMu sync.Mutex

	recordDecision := func(plan map[string]any, decision, terminalStage, reason string) {
		candidateID := pyStr(plan["candidate_id"])
		refs := operationRefsByCandidate[candidateID]
		externalOperations := make([]any, 0, len(refs))
		for _, item := range refs {
			converted := map[string]any{}
			for key, value := range item {
				converted[key] = value
			}
			externalOperations = append(externalOperations, converted)
		}
		decisionsMu.Lock()
		decisionsByCandidate[candidateID] = map[string]any{
			"candidate_id": candidateID,
			"interest_summary_id": nilIfEmpty(
				pyStrOr(plan["interest_summary_id"], ""),
				pyStr(plan["interest_summary_id"]) != ""),
			"category":            pyStrOr(plan["category"], ""),
			"decision":            decision,
			"terminal_stage":      terminalStage,
			"reason":              reason,
			"external_operations": externalOperations,
		}
		decisionsMu.Unlock()
	}

	hostGrounded := map[string]map[string]any{}
	for _, plan := range plans {
		candidateID := pyStr(plan["candidate_id"])
		hostEvidence := HostEvidence(plan)
		if hostEvidence == nil {
			continue
		}
		projectLane := pyStr(hostEvidence["source_gate_mode"]) == "project_exact_locator"
		if CardCopyRejectionReason(plan["title"], CardTitleMaxChars) != "" {
			reason := "profile_interest_card_copy_invalid"
			if projectLane {
				reason = "project_card_copy_invalid"
			}
			recordDecision(plan, "reject", "admission", reason)
			hostGrounded[candidateID] = nil
			continue
		}
		if projectLane {
			recordDecision(plan, "admit", "project_history", "project_history_exact_locator")
		} else {
			recordDecision(plan, "admit", "profile_interest", "profile_interest_exact_summary")
		}
		hostGrounded[candidateID] = HostGroundedPlan(plan, hostEvidence)
	}

	extract := func(plan map[string]any, evidenceBatch *frozenEvidenceBatch) map[string]any {
		candidateID := pyStr(plan["candidate_id"])
		if grounded, present := hostGrounded[candidateID]; present {
			return grounded
		}
		allowedDocumentRevisionIDs := evidenceBatch.allowedByCandidate[candidateID]
		if evidenceBatch.frozen == nil || len(allowedDocumentRevisionIDs) == 0 {
			pair, hasRejection := evidenceBatch.rejections[candidateID]
			if !hasRejection {
				pair = [2]string{"admission", "no_fresh_eligible_document"}
			}
			recordDecision(plan, "reject", pair[0], pair[1])
			return nil
		}
		if recentCategories[pyStr(plan["category"])] {
			title, hasTitle := finalTitles[candidateID]
			if !hasTitle {
				reason := "recent_finalization_abstained"
				if recorded, hasError := finalizationErrors[candidateID]; hasError {
					reason = recorded
				}
				recordDecision(plan, "reject", "admission", reason)
				return nil
			}
			finalized := mapCopyAny(plan)
			finalized["title"] = title
			finalized["prompt"] = title
			var previouslyOffered []string
			for _, question := range pyListMaps(plan["previously_offered"]) {
				previouslyOffered = append(previouslyOffered, pyStr(question["title"]))
			}
			if len(RestatingPlans([]map[string]any{finalized}, previouslyOffered)) > 0 {
				recordDecision(plan, "reject", "admission", "recent_question_already_offered")
				return nil
			}
			if CardCopyRejectionReason(finalized["title"], CardTitleMaxChars) != "" {
				recordDecision(plan, "reject", "admission", "recent_card_copy_invalid")
				return nil
			}
			plan = finalized
		}
		requestID := fmt.Sprintf("external-request-%s", newUUIDHex())
		operationID := fmt.Sprintf("external-operation-%s", newUUIDHex())
		started := time.Now()
		evidence := evidenceBatch.evidenceByCandidate[candidateID]
		if evidence == nil {
			evidence = map[string]any{"candidate_id": candidateID,
				"search": nil, "fetches": []any{}, "extractor": nil}
			evidenceBatch.evidenceByCandidate[candidateID] = evidence
		}
		payloadMapOrEmpty(evidence)["extractor"] = map[string]any{
			"operation_id": operationID, "request_id": requestID,
			"input_document_revision_ids": toAnyList(allowedDocumentRevisionIDs),
			"component_receipt":           nil,
		}
		operationRefsByCandidate[candidateID] = append(
			operationRefsByCandidate[candidateID],
			map[string]string{"operation": "extractor", "operation_id": operationID,
				"request_id": requestID})
		externalAudit.RecordAttempt("extractor", operationID, requestID,
			"v6_evidence_extractor", "ark", candidateID)
		planLane := OptionalLaneOf(plan)
		invokeExtractor := EvidenceExtractor(func(ctx context.Context, payload map[string]any) (any, error) {
			return lanes.Within(ctx, []string{planLane},
				func(withinCtx context.Context) (any, error) {
					return invokeV6ArkEvidenceSelectorTopic(withinCtx, invokeStage, payload)
				})
		})
		extracted, extractErr := ExtractCandidateEvidence(&EvidenceCandidate{
			CandidateID: candidateID, Title: pyStr(plan["title"]),
			PrimaryQuery:   pyStr(plan["primary_query"]),
			FreshnessClass: pyStr(plan["freshness_class"]),
			ContextScope:   snapshot.ContextScope, Category: pyStr(plan["category"]),
		}, evidenceBatch.frozen, allowedDocumentRevisionIDs,
			snapshot.InputSnapshotID, requestID, binding, invokeExtractor, p.Now())
		if extractErr != nil {
			if contract, isContract := extractErr.(*EvidenceContractError); isContract {
				if contract.ComponentReceipt != nil {
					payloadMapOrEmpty(payloadMapOrEmpty(evidence)["extractor"])["component_receipt"] =
						contract.ComponentReceipt.AsPayload()
				}
				externalAudit.RecordFailure("extractor", operationID,
					ExtractorAuditProviderID, requestID, contract.Reason,
					float64(time.Since(started).Milliseconds()))
				terminalStage := "offset_validation"
				if contract.Reason == "evidence_provider_invocation_failed" {
					terminalStage = "extraction"
				}
				recordDecision(plan, "reject", terminalStage, contract.Reason)
				return nil
			}
			externalAudit.RecordFailure("extractor", operationID,
				ExtractorAuditProviderID, requestID, "extraction_failed",
				float64(time.Since(started).Milliseconds()))
			recordDecision(plan, "reject", "extraction", "extraction_failed")
			return nil
		}
		externalAudit.RecordSuccess("extractor", operationID, ExtractorAuditProviderID,
			requestID, fmt.Sprintf("external-result-%s", newUUIDHex()),
			float64(time.Since(started).Milliseconds()))
		payloadMapOrEmpty(payloadMapOrEmpty(evidence)["extractor"])["component_receipt"] =
			extracted.ComponentReceipt.AsPayload()
		admitted, admitErr := AdmitCandidateEvidence(extracted, p.Now())
		if admitErr != nil {
			if contract, isContract := admitErr.(*EvidenceContractError); isContract {
				recordDecision(plan, "reject", "admission", contract.Reason)
			} else {
				recordDecision(plan, "reject", "admission", "admission_failed")
			}
			return nil
		}
		recordDecision(plan, "admit", "admission", "admitted")
		documentPayloads := make([]any, 0, len(evidenceBatch.frozen.Documents))
		for _, document := range evidenceBatch.frozen.Documents {
			documentPayloads = append(documentPayloads, document.AsPayload(false))
		}
		documentIDs := make([]any, 0, len(evidenceBatch.frozen.Documents))
		for _, document := range evidenceBatch.frozen.Documents {
			documentIDs = append(documentIDs, document.DocumentRevisionID)
		}
		grounded := mapCopyAny(plan)
		grounded["evidence_bundle"] = FlattenEvidence(admitted)
		grounded["evidence_receipt"] = admitted.ComponentReceipt
		grounded["search_receipt"] = evidenceBatch.searchReceipts[candidateID]
		grounded["document_snapshot_manifest"] = map[string]any{
			"snapshot_date":         evidenceBatch.frozen.SnapshotDate,
			"snapshot_version":      evidenceBatch.frozen.SnapshotVersion,
			"document_revision_ids": documentIDs,
			"documents":             documentPayloads,
		}
		return grounded
	}

	// 冻结全部候选的搜索与抓取；on_candidate_ready 在每个候选就绪时触发。
	groundingResults := map[string]func() map[string]any{}
	groundingWg := sync.WaitGroup{}
	var mu sync.Mutex
	onCandidateReady := func(plan map[string]any, evidenceBatch *frozenEvidenceBatch) {
		mu.Lock()
		operationRefsByCandidate = evidenceBatch.operationRefs
		mu.Unlock()
		candidateID := pyStr(plan["candidate_id"])
		groundingWg.Add(1)
		done := make(chan map[string]any, 1)
		mu.Lock()
		groundingResults[candidateID] = func() map[string]any { return <-done }
		mu.Unlock()
		go func() {
			defer groundingWg.Done()
			defer close(done)
			// 近期计划先终稿，再提取（ground_candidate :8387-8400）。
			titles, finalizeErr := p.FinalizeRecentPlans([]map[string]any{plan},
				evidenceBatch.frozen, evidenceBatch.allowedByCandidate, snapshot,
				config, invokeStage, lanes)
			if finalizeErr != nil {
				reason := "recent_finalization_failed"
				if rejection, isRejection := finalizeErr.(*TerminalGenerationRejection); isRejection {
					reason = rejection.Reason
				}
				mu.Lock()
				finalizationErrors[candidateID] = reason
				mu.Unlock()
			} else {
				mu.Lock()
				for id, title := range titles {
					finalTitles[id] = title
				}
				mu.Unlock()
			}
			done <- extract(plan, evidenceBatch)
		}()
	}

	batch, err := p.FreezeDocuments(plans, snapshot, triggerKey, externalAudit,
		searchProvider, firstDocumentNumber, lanes, earlyEvidence,
		onCandidateReady, fetchTasks)
	if err != nil {
		return nil, err
	}
	groundingWg.Wait()
	for _, plan := range plans {
		candidateID := pyStr(plan["candidate_id"])
		if _, present := batch.evidenceByCandidate[candidateID]; !present {
			batch.evidenceByCandidate[candidateID] = map[string]any{
				"candidate_id": candidateID, "search": nil,
				"fetches": []any{}, "extractor": nil,
			}
		}
	}
	ordinary := []map[string]any{}
	for _, plan := range plans {
		if !pythonTruthy(plan["news_match"]) {
			ordinary = append(ordinary, plan)
		}
	}
	if onRegularReady != nil && len(ordinary) > 0 {
		allResolved := true
		for _, plan := range ordinary {
			candidateID := pyStr(plan["candidate_id"])
			_, hosted := hostGrounded[candidateID]
			if !hosted && len(batch.allowedByCandidate[candidateID]) > 0 {
				allResolved = false
				break
			}
		}
		if allResolved {
			var admitted []map[string]any
			for _, plan := range ordinary {
				if grounded := hostGrounded[pyStr(plan["candidate_id"])]; grounded != nil {
					admitted = append(admitted, grounded)
				}
			}
			if len(admitted) > 0 {
				onRegularReady(admitted)
			}
		}
	}

	// 未走 on_candidate_ready 通道的计划（如宿主证据/无网页计划）就地提取。
	var grounded []map[string]any
	for _, plan := range plans {
		candidateID := pyStr(plan["candidate_id"])
		if getter, wasLaunched := groundingResults[candidateID]; wasLaunched {
			grounded = append(grounded, getter())
			continue
		}
		grounded = append(grounded, extract(plan, batch))
	}

	// 每张已纳入的网页卡描述最终共享快照。
	for _, item := range grounded {
		if item == nil {
			continue
		}
		if _, hasManifest := item["document_snapshot_manifest"]; hasManifest && batch.frozen != nil {
			documentPayloads := make([]any, 0, len(batch.frozen.Documents))
			for _, document := range batch.frozen.Documents {
				documentPayloads = append(documentPayloads, document.AsPayload(false))
			}
			documentIDs := make([]any, 0, len(batch.frozen.Documents))
			for _, document := range batch.frozen.Documents {
				documentIDs = append(documentIDs, document.DocumentRevisionID)
			}
			item["document_snapshot_manifest"] = map[string]any{
				"snapshot_date":         batch.frozen.SnapshotDate,
				"snapshot_version":      batch.frozen.SnapshotVersion,
				"document_revision_ids": documentIDs,
				"documents":             documentPayloads,
			}
		}
	}
	finalizationError := ""
	for _, plan := range plans {
		if recorded, hasError := finalizationErrors[pyStr(plan["candidate_id"])]; hasError {
			finalizationError = recorded
			break
		}
	}
	decided := append([]map[string]any(nil), plans...)
	for index, plan := range plans {
		item := grounded[index]
		if item != nil || plan["hot_list_entry"] == nil {
			continue
		}
		// 没有报道通过该词条：词条本身成为新闻卡。
		fallback := HotListPlan(plan, snapshot.InputSnapshotID)
		fallbackID := pyStr(fallback["candidate_id"])
		decided = append(decided, fallback)
		batch.evidenceByCandidate[fallbackID] = map[string]any{
			"candidate_id": fallbackID, "search": nil,
			"fetches": []any{}, "extractor": nil,
		}
		recordDecision(fallback, "admit", "hot_list", "hot_list_entry")
		grounded = append(grounded, func() map[string]any {
			groundedFallback := mapCopyAny(fallback)
			groundedFallback["evidence_bundle"] = mapCopyAny(payloadMapOrEmpty(fallback["hot_list_evidence"]))
			groundedFallback["evidence_receipt"] = nil
			return groundedFallback
		}())
	}
	var candidateDecisions []any
	admittedCount := 0
	for _, plan := range decided {
		decision := decisionsByCandidate[pyStr(plan["candidate_id"])]
		if decision == nil {
			continue
		}
		candidateDecisions = append(candidateDecisions, decision)
		if decision["decision"] == "admit" {
			admittedCount++
		}
	}
	receiptDecision := "abstain"
	if admittedCount == len(candidateDecisions) && len(candidateDecisions) > 0 {
		receiptDecision = "admit"
	} else if admittedCount > 0 {
		receiptDecision = "partial"
	}
	var groundedPlans []map[string]any
	for _, item := range grounded {
		if item != nil {
			groundedPlans = append(groundedPlans, item)
		}
	}
	var documentIDs []any
	var documentPayloads []any
	if batch.frozen != nil {
		for _, document := range batch.frozen.Documents {
			documentIDs = append(documentIDs, document.DocumentRevisionID)
			documentPayloads = append(documentPayloads, document.AsPayload(false))
		}
	}
	var evidenceCandidates []any
	for _, plan := range decided {
		if evidence, present := batch.evidenceByCandidate[pyStr(plan["candidate_id"])]; present {
			evidenceCandidates = append(evidenceCandidates, evidence)
		}
	}
	result := &pipelineGroundingBatch{
		plans: groundedPlans,
		decisionReceipt: map[string]any{
			"schema_version":      GroundingDecisionReceiptVersion,
			"input_snapshot_id":   snapshot.InputSnapshotID,
			"context_scope":       snapshot.ContextScope,
			"decision":            receiptDecision,
			"candidate_decisions": candidateDecisions,
		},
		evidenceReceipt: map[string]any{
			"schema_version":    GroundingEvidenceReceiptVersion,
			"input_snapshot_id": snapshot.InputSnapshotID,
			"context_scope":     snapshot.ContextScope,
			"document_snapshot": map[string]any{
				"snapshot_date":         pyStr(triggerKey["local_date"]),
				"snapshot_version":      pyStr(triggerKey["document_snapshot_version"]),
				"document_revision_ids": documentIDs,
				"documents":             documentPayloads,
			},
			"candidates": evidenceCandidates,
		},
	}
	// 终稿失败只连坐自己的候选；没有其他卡被纳入时才让整轮失败。
	if finalizationError != "" && len(result.plans) == 0 {
		return result, &TerminalGenerationRejection{Reason: finalizationError}
	}
	return result, nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// regexpEscape 正则元字符转义（re.escape）。
func regexpEscape(value string) string {
	escaped := make([]rune, 0, len(value))
	for _, character := range value {
		if strings.ContainsRune(`\.+*?()|[]{}^$`, character) {
			escaped = append(escaped, '\\')
		}
		escaped = append(escaped, character)
	}
	return string(escaped)
}

// regexpCompile 编译补丁段正则。
func regexpCompile(pattern string) *regexp.Regexp {
	return regexp.MustCompile("(?s)" + pattern)
}

// toAnyList 字符串切片转 any 切片（validate3b 已有 toAnySlice 的别名形态）。
func toAnyList(values []string) []any {
	out := make([]any, 0, len(values))
	for _, value := range values {
		out = append(out, value)
	}
	return out
}

// invokeV6ArkEvidenceSelectorTopic 接口形态的证据选择调用
// （topic_pipeline_c.go 的 ArkStageInvoker 形态经接口转发）。
func invokeV6ArkEvidenceSelectorTopic(ctx context.Context,
	invokeStage TopicStageInvoker, payload map[string]any) (map[string]any, error) {
	if direct, ok := invokeStage.(*ArkStageInvoker); ok {
		return InvokeV6ArkEvidenceSelector(ctx, direct, payload)
	}
	response, err := invokeStage.Invoke(ctx, "v6_evidence_extractor",
		pipelineEvidencePrompt, compactJSON(payload))
	if err != nil {
		return nil, err
	}
	return payloadMapOrEmpty(response), nil
}

// ExtractorAuditProviderID 提取器审计厂商 ID。
const ExtractorAuditProviderID = "ark"

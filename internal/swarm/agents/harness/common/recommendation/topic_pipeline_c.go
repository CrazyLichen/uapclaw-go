package recommendation

// topic_pipeline_c.go 一比一对齐上游 topic_pipeline.py:1807-1873/:2282-2479：
// 证据选择器目录映射与多样性评分代理批。

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
)

// InvokeV6ArkEvidenceSelector 把 Ark 目录条目选择映射到宿主的精确偏移契约
// （topic_pipeline.py:1807-1873；分支顺序逐条对应）。
func InvokeV6ArkEvidenceSelector(ctx context.Context, invokeStage *ArkStageInvoker,
	payload map[string]any) (map[string]any, error) {
	selections := map[[2]string][2]int{}
	var documents []any
	for documentIndex, rawDocument := range anyListOf(payload["documents"]) {
		document := copyMapAny(mapAnyOf(rawDocument))
		documentID := pyStr(document["document_revision_id"])
		catalog := []any{}
		for entryIndex, rawEntry := range anyListOf(document["offset_catalog"]) {
			entry := copyMapAny(mapAnyOf(rawEntry))
			entryID := fmt.Sprintf("document-%d-entry-%d", documentIndex, entryIndex)
			entry["catalog_entry_id"] = entryID
			catalog = append(catalog, entry)
			selections[[2]string{documentID, entryID}] = [2]int{
				intOf(entry["start"]), intOf(entry["end"]),
			}
		}
		document["offset_catalog"] = catalog
		documents = append(documents, document)
	}
	merged := copyMapAny(payload)
	merged["documents"] = documents
	response, err := invokeStage.Invoke(ctx, "v6_evidence_extractor",
		pipelineEvidencePrompt, marshalCompactPayload(merged))
	if err != nil {
		return nil, err
	}
	// provider_validation 包装（topic_pipeline.py:1842 with provider_validation(...)）
	defer RecordProviderValidation(invokeStage, response, ProviderValidationOutcome(response))
	root, isMap := response.(map[string]any)
	if !isMap || !mapKeysEqual(root, []string{"candidate_id", "spans"}) ||
		anyListOf(root["spans"]) == nil {
		if _, isMap := response.(map[string]any); isMap {
			if anyListOf(root["spans"]) == nil {
				RecordProviderValidation(invokeStage, response, "invalid")
			}
		} else {
			RecordProviderValidation(invokeStage, response, "invalid")
		}
		return nil, &EvidenceContractError{
			Reason: "evidence_output_contract_failed",
			Detail: "invalid evidence catalog selector response",
		}
	}
	spans := []any{}
	for _, rawSpan := range anyListOf(root["spans"]) {
		span, isMap := rawSpan.(map[string]any)
		if !isMap || !mapKeysEqual(span, []string{"document_revision_id", "catalog_entry_id"}) {
			RecordProviderValidation(invokeStage, response, "invalid")
			return nil, NewEvidenceContractError("evidence_output_contract_failed", "invalid evidence catalog selector response", nil)
		}
		documentID := pyStr(span["document_revision_id"])
		entryID := pyStr(span["catalog_entry_id"])
		offsets, known := selections[[2]string{documentID, entryID}]
		if !known {
			RecordProviderValidation(invokeStage, response, "invalid")
			return nil, NewEvidenceContractError("evidence_output_contract_failed", "unknown evidence catalog entry", nil)
		}
		spans = append(spans, map[string]any{
			"document_revision_id": documentID,
			"start":                offsets[0],
			"end":                  offsets[1],
		})
	}
	return map[string]any{
		"candidate_id": root["candidate_id"],
		"spans":        spans,
	}, nil
}

// canonicalDiversityPair 无序对规范形态（topic_pipeline.py:2282-2297）。
func canonicalDiversityPair(left, right any) ([2]string, error) {
	leftID, leftIsString := left.(string)
	rightID, rightIsString := right.(string)
	if !leftIsString || leftID == "" || !rightIsString || rightID == "" || leftID == rightID {
		return [2]string{}, &TerminalGenerationRejection{
			Reason: "diversity_api_batch_pair_membership_invalid"}
	}
	if leftID < rightID {
		return [2]string{leftID, rightID}, nil
	}
	return [2]string{rightID, leftID}, nil
}

// ValidateDiversityAPIBatch 校验一次多样性批响应
// （topic_pipeline.py:2299-2381；分支顺序逐条对应）。
func ValidateDiversityAPIBatch(raw any, requestedPairs [][2]string,
	requestedMarginalIDs []string) (map[string][]map[string]any, error) {
	root, isMap := raw.(map[string]any)
	if !isMap || !mapKeysEqual(root, []string{"pairwise_scores", "marginal_scores"}) ||
		anyListOf(root["pairwise_scores"]) == nil || anyListOf(root["marginal_scores"]) == nil {
		return nil, &TerminalGenerationRejection{
			Reason: "diversity_api_batch_output_schema_invalid"}
	}
	pairFields := []string{"left_candidate_id", "right_candidate_id",
		"diversity_score_nullable", "compatible", "reason"}
	var pairItems []map[string]any
	var actualPairs [][2]string
	for _, itemValue := range anyListOf(root["pairwise_scores"]) {
		item, isMap := itemValue.(map[string]any)
		if !isMap || !mapKeysEqual(item, pairFields) {
			return nil, &TerminalGenerationRejection{
				Reason: "diversity_api_batch_output_schema_invalid"}
		}
		if _, isBool := item["compatible"].(bool); !isBool ||
			!nonEmptyStr(item["reason"]) {
			return nil, &TerminalGenerationRejection{
				Reason: "diversity_api_batch_decision_invalid"}
		}
		canonical, err := canonicalDiversityPair(item["left_candidate_id"], item["right_candidate_id"])
		if err != nil {
			return nil, err
		}
		actualPairs = append(actualPairs, canonical)
		pairItems = append(pairItems, copyMapAny(item))
	}
	expectedPairs := map[[2]string]bool{}
	for _, pair := range requestedPairs {
		canonical, err := canonicalDiversityPair(pair[0], pair[1])
		if err != nil {
			return nil, err
		}
		expectedPairs[canonical] = true
	}
	pairSet := map[[2]string]bool{}
	for _, pair := range actualPairs {
		if pairSet[pair] {
			return nil, &TerminalGenerationRejection{
				Reason: "diversity_api_batch_pair_membership_invalid"}
		}
		pairSet[pair] = true
	}
	if len(pairSet) != len(expectedPairs) {
		return nil, &TerminalGenerationRejection{
			Reason: "diversity_api_batch_pair_membership_invalid"}
	}
	for pair := range pairSet {
		if !expectedPairs[pair] {
			return nil, &TerminalGenerationRejection{
				Reason: "diversity_api_batch_pair_membership_invalid"}
		}
	}
	marginalFields := []string{"candidate_id", "marginal_diversity_nullable"}
	var marginalItems []map[string]any
	var actualMarginalIDs []string
	marginalSeen := map[string]bool{}
	for _, itemValue := range anyListOf(root["marginal_scores"]) {
		item, isMap := itemValue.(map[string]any)
		if !isMap || !mapKeysEqual(item, marginalFields) {
			return nil, &TerminalGenerationRejection{
				Reason: "diversity_api_batch_output_schema_invalid"}
		}
		candidateID, isString := item["candidate_id"].(string)
		if !isString || candidateID == "" {
			return nil, &TerminalGenerationRejection{
				Reason: "diversity_api_batch_marginal_membership_invalid"}
		}
		if marginalSeen[candidateID] {
			return nil, &TerminalGenerationRejection{
				Reason: "diversity_api_batch_marginal_membership_invalid"}
		}
		marginalSeen[candidateID] = true
		actualMarginalIDs = append(actualMarginalIDs, candidateID)
		marginalItems = append(marginalItems, copyMapAny(item))
	}
	requestedSet := map[string]bool{}
	for _, id := range requestedMarginalIDs {
		requestedSet[id] = true
	}
	if len(actualMarginalIDs) != len(requestedSet) {
		return nil, &TerminalGenerationRejection{
			Reason: "diversity_api_batch_marginal_membership_invalid"}
	}
	for _, id := range actualMarginalIDs {
		if !requestedSet[id] {
			return nil, &TerminalGenerationRejection{
				Reason: "diversity_api_batch_marginal_membership_invalid"}
		}
	}
	return map[string][]map[string]any{
		"pairwise_scores": pairItems,
		"marginal_scores": marginalItems,
	}, nil
}

// InvokeBatchedDiversityProxy 在保持完整评分器契约的同时约束单次 Ark 响应
// （topic_pipeline.py:2383-2479；asyncio.gather → errgroup 并发，§5 已知偏差）。
func InvokeBatchedDiversityProxy(ctx context.Context, payload map[string]any,
	invokeStage *ArkStageInvoker) (map[string][]map[string]any, error) {
	candidates, isList := payload["candidates"].([]any)
	if !isList {
		return nil, &TerminalGenerationRejection{
			Reason: "diversity_api_proxy_candidates_invalid"}
	}
	var typedCandidateIDs []string
	seen := map[string]bool{}
	for _, candidateValue := range candidates {
		candidate, isMap := candidateValue.(map[string]any)
		if !isMap {
			typedCandidateIDs = append(typedCandidateIDs, "")
			continue
		}
		id := pyStr(candidate["candidate_id"])
		if id == "" || seen[id] {
			return nil, &TerminalGenerationRejection{
				Reason: "diversity_api_proxy_candidate_membership_invalid"}
		}
		seen[id] = true
		typedCandidateIDs = append(typedCandidateIDs, id)
	}
	for _, id := range typedCandidateIDs {
		if id == "" {
			return nil, &TerminalGenerationRejection{
				Reason: "diversity_api_proxy_candidate_membership_invalid"}
		}
	}
	var allPairs [][2]string
	for i := 0; i < len(typedCandidateIDs); i++ {
		for j := i + 1; j < len(typedCandidateIDs); j++ {
			allPairs = append(allPairs, [2]string{typedCandidateIDs[i], typedCandidateIDs[j]})
		}
	}
	var pairBatches [][2]string
	for start := 0; start < len(allPairs); start += DiversityPairBatchSize {
		end := start + DiversityPairBatchSize
		if end > len(allPairs) {
			end = len(allPairs)
		}
		pairBatches = append(pairBatches, allPairs[start:end]...)
	}
	// 每批是一段切片：按 DIVERSITY_PAIR_BATCH_SIZE 切批
	var pairBatchSlices [][][2]string
	for start := 0; start < len(allPairs); start += DiversityPairBatchSize {
		end := start + DiversityPairBatchSize
		if end > len(allPairs) {
			end = len(allPairs)
		}
		pairBatchSlices = append(pairBatchSlices, allPairs[start:end])
	}
	var marginalBatchSlices [][]string
	for start := 0; start < len(typedCandidateIDs); start += DiversityMarginalBatchSize {
		end := start + DiversityMarginalBatchSize
		if end > len(typedCandidateIDs) {
			end = len(typedCandidateIDs)
		}
		marginalBatchSlices = append(marginalBatchSlices, typedCandidateIDs[start:end])
	}
	batchCount := len(pairBatchSlices)
	if len(marginalBatchSlices) > batchCount {
		batchCount = len(marginalBatchSlices)
	}
	if batchCount < 1 {
		batchCount = 1
	}
	type batchResult struct {
		pairwise []map[string]any
		marginal []map[string]any
	}
	results := make([]batchResult, batchCount)
	errs := make([]error, batchCount)
	var wg sync.WaitGroup
	for index := 0; index < batchCount; index++ {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			var requestedPairs [][2]string
			if index < len(pairBatchSlices) {
				requestedPairs = pairBatchSlices[index]
			}
			var requestedMarginalIDs []string
			if index < len(marginalBatchSlices) {
				requestedMarginalIDs = marginalBatchSlices[index]
			}
			batchPayload := map[string]any{
				"input_snapshot_id": payload["input_snapshot_id"],
				"discussion_coverage": func() any {
					if value, present := payload["discussion_coverage"]; present {
						return value
					}
					return []any{}
				}(),
				"candidates": candidates,
				"requested_pairs": func() any {
					pairs := []any{}
					for _, pair := range requestedPairs {
						pairs = append(pairs, map[string]any{
							"left_candidate_id": pair[0], "right_candidate_id": pair[1]})
					}
					return pairs
				}(),
				"requested_marginal_candidate_ids": stringsToAny(requestedMarginalIDs),
				"request_id":                       payload["request_id"],
			}
			raw, invokeErr := invokeStage.Invoke(ctx, "v6_diversity_scorer",
				pipelineDiversityPrompt, marshalCompactPayload(batchPayload))
			if invokeErr != nil {
				errs[index] = invokeErr
				return
			}
			RecordProviderValidation(invokeStage, raw, ProviderValidationOutcome(raw))
			validated, validateErr := ValidateDiversityAPIBatch(raw, requestedPairs, requestedMarginalIDs)
			if validateErr != nil {
				errs[index] = validateErr
				return
			}
			results[index] = batchResult{
				pairwise: validated["pairwise_scores"],
				marginal: validated["marginal_scores"],
			}
		}(index)
	}
	wg.Wait()
	for _, err := range errs {
		if err != nil {
			return nil, err
		}
	}
	pairwise := []map[string]any{}
	marginal := []map[string]any{}
	for _, batch := range results {
		pairwise = append(pairwise, batch.pairwise...)
		marginal = append(marginal, batch.marginal...)
	}
	return map[string][]map[string]any{
		"pairwise_scores": pairwise,
		"marginal_scores": marginal,
	}, nil
}

// ProviderValidationOutcome provider_validation 上下文的验证结论判定
// （对齐 with provider_validation(...) 异常即 invalid；正常路径由调用方判定）。
func ProviderValidationOutcome(response any) string {
	if response == nil {
		return "invalid"
	}
	return "validated"
}

var _ = json.Marshal

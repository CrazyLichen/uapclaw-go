package recommendation

// topic_product_validate3b.go 一比一对齐上游 topic_product.py 校验族 v3b：
// :2427-2844 的 news-only/list-search 判定、:4450-4530
// _search_lineage_rejection_reason、:4186-4373 _manifest_rejection_reason、
// :4374-4405 _valid_manifest/_valid_cold_start_pool、:5127-5175
// _load_manifest_cards/_load_card_revisions、:5465-5510 _compatible_pool、
// :6371-6397 _eligible_pool_cards、:6470-6502 retainable_pool（⤴️31c 归位）。
// 已知偏差：_project_sources_still_belong 的会话元数据旁路参数（见 v2）。

import (
	"fmt"
	"sort"
	"strings"

	"github.com/uapclaw/uapclaw-go/internal/common/logger"

	swarmsession "github.com/uapclaw/uapclaw-go/internal/swarm/server/session"
)

// ──────────────────────────── 常量 ────────────────────────────

// listSearchFields 列表搜索字段（topic_product.py:485-487）。
var listSearchFields = map[string]bool{
	"search_version": true, "candidate_decisions": true, "bounded_beam_trace": true,
}

// listSearchFailureReasons 列表搜索失败原因（:491）。
var listSearchFailureReasons = map[string]bool{"quality_abstained": true}

// candidateHostDecisionFields 候选宿主决策字段（:492-494）。
var candidateHostDecisionFields = map[string]bool{
	"candidate_id": true, "selected": true, "reasons": true,
}

// boundedBeamTraceFields 束轨迹字段（:495-507）。
var boundedBeamTraceFields = map[string]bool{
	"entries": true, "preferred_candidate_ids": true, "target_size": true,
	"expansion_count": true, "beam_width": true, "max_expansions": true,
	"utility_scorer_version": true, "diversity_scorer_version": true,
	"search_version": true,
}

// beamTraceEntryFields 束轨迹条目字段（:508-524）。
var beamTraceEntryFields = map[string]bool{
	"trace_index": true, "candidate_ids": true, "depth": true, "status": true,
	"retained_in_beam": true, "hard_constraints_pass": true,
	"utility_pass": true, "diversity_pass": true,
	"mean_utility_nullable": true, "mean_diversity_nullable": true,
	"minimum_pairwise_diversity_nullable": true, "objective_nullable": true,
	"failure_reasons": true,
}

// beamTraceStatuses 束轨迹状态集（:525-535）。
var beamTraceStatuses = map[string]bool{
	"hard_filter_rejected": true, "hard_constraint_rejected": true,
	"complete_retained": true, "complete_pruned": true,
	"partial_retained": true, "partial_pruned": true,
	"search_budget_exhausted": true, "preferred": true,
}

// ──────────────────────────── 导出函数 ────────────────────────────

// DiversityInvocationCovers 池的多样性判定恰被问到这些候选
// （topic_product.py:2436-2452）。
func DiversityInvocationCovers(manifest map[string]any, candidateIDs []string) bool {
	receipts, isMap := manifest["component_receipts"].(map[string]any)
	if !isMap {
		return false
	}
	receipt, isMap := receipts["diversity_scorer"].(map[string]any)
	if !isMap {
		return false
	}
	requestID := pyStr(receipt["request_id"])
	var covered []map[string]bool
	for _, raw := range pyListMaps(manifest["component_invocation_receipts"]) {
		if pyStr(raw["component_id"]) != "diversity_scorer" {
			continue
		}
		if pyStr(raw["request_id"]) != requestID {
			continue
		}
		set := map[string]bool{}
		for _, id := range pyListStrings(raw["candidate_ids"]) {
			set[id] = true
		}
		covered = append(covered, set)
	}
	if len(covered) != 1 {
		return false
	}
	target := map[string]bool{}
	for _, id := range candidateIDs {
		target[id] = true
	}
	if len(covered[0]) != len(target) {
		return false
	}
	for id := range target {
		if !covered[0][id] {
			return false
		}
	}
	return true
}

// NewsOnlyListSearchRejectionReason 新闻单卡的列表搜索：
// 未搜索且未判多样性（topic_product.py:2454-2501）。
func NewsOnlyListSearchRejectionReason(manifest map[string]any,
	cards []map[string]any, trace any) string {
	const failure = "manifest_field_list_search_contract_failed"
	receipts, isMap := manifest["component_receipts"].(map[string]any)
	var utilityVersion string
	if isMap {
		if utility, ok := receipts["interaction_utility_scorer"].(map[string]any); ok {
			if binding, ok := utility["binding"].(map[string]any); ok {
				utilityVersion = pyStr(binding["config_version"])
			}
		}
	}
	traceMap, traceIsMap := trace.(map[string]any)
	if !traceIsMap || len(traceMap) != len(boundedBeamTraceFields) {
		return failure
	}
	for field := range boundedBeamTraceFields {
		if _, present := traceMap[field]; !present {
			return failure
		}
	}
	if utilityVersion == "" {
		return failure
	}
	expected := map[string]any{
		"entries": []any{}, "preferred_candidate_ids": []any{},
		"target_size":              GLOBAL_POOL_TARGET_SIZE - 1,
		"expansion_count":          0,
		"utility_scorer_version":   utilityVersion,
		"diversity_scorer_version": "",
		"search_version":           LIST_SEARCH_VERSION,
	}
	for _, field := range []string{"entries", "preferred_candidate_ids", "target_size",
		"expansion_count", "utility_scorer_version", "diversity_scorer_version",
		"search_version"} {
		if !anyEqual(traceMap[field], expected[field]) {
			return failure
		}
	}
	for _, field := range []string{"beam_width", "max_expansions"} {
		if !plainInt(traceMap[field]) || pyIntOf(traceMap[field]) < 1 {
			return failure
		}
	}
	if receipts != nil {
		if _, hasDiversity := receipts["diversity_scorer"]; hasDiversity {
			return failure
		}
	}
	if !anyEqual(manifest["preference_pairs"], []any{}) {
		return "manifest_field_preference_pairs_contract_failed"
	}
	for _, card := range cards {
		if pyStr(card["list_search_version"]) != LIST_SEARCH_VERSION {
			return failure
		}
	}
	return ""
}

// ListSearchRejectionReason 列表搜索契约（topic_product.py:2502-2844
// _list_search_rejection_reason）。
func ListSearchRejectionReason(manifest map[string]any, cards []map[string]any) string {
	const failure = "manifest_field_list_search_contract_failed"
	listSearch, isMap := manifest["list_search"].(map[string]any)
	if !isMap {
		return failure
	}
	newsOnly := NewsOnlyPool(manifest, cards)
	fieldCountMatch := len(listSearch) == len(listSearchFields)
	if newsOnly {
		fieldCountMatch = len(listSearch) == len(listSearchFields)+1
		if _, hasFailure := listSearch["failure_reason"]; !hasFailure && fieldCountMatch {
			// 多出的一键必须是 failure_reason
			fieldCountMatch = false
			for field := range listSearch {
				if field != "failure_reason" && !listSearchFields[field] {
					fieldCountMatch = false
				}
			}
			fieldCountMatch = true
		}
	}
	if !fieldCountMatch {
		return failure
	}
	for field := range listSearchFields {
		if _, present := listSearch[field]; !present {
			return failure
		}
	}
	if newsOnly {
		if _, present := listSearch["failure_reason"]; !present {
			return failure
		}
	}
	if pyStr(listSearch["search_version"]) != LIST_SEARCH_VERSION {
		return failure
	}
	rawDecisions, isList := listSearch["candidate_decisions"].([]any)
	if !isList {
		return failure
	}
	decisions := map[string]map[string]any{}
	decisionOrder := []string{}
	for _, raw := range rawDecisions {
		decision, isMap := raw.(map[string]any)
		if !isMap || len(decision) != len(candidateHostDecisionFields) {
			return failure
		}
		for field := range candidateHostDecisionFields {
			if _, present := decision[field]; !present {
				return failure
			}
		}
		candidateID := pyStr(decision["candidate_id"])
		if candidateID == "" {
			return failure
		}
		if _, dup := decisions[candidateID]; dup {
			return failure
		}
		if _, isBool := decision["selected"].(bool); !isBool {
			return failure
		}
		reasons := decision["reasons"]
		reasonList, reasonsIsList := reasons.([]any)
		if !reasonsIsList {
			return failure
		}
		if len(reasonList) == 0 {
			return failure
		}
		seenReasons := map[string]bool{}
		for _, reason := range reasonList {
			value := pyStr(reason)
			if value == "" || seenReasons[value] {
				return failure
			}
			seenReasons[value] = true
		}
		decisions[candidateID] = decision
		decisionOrder = append(decisionOrder, candidateID)
	}

	grounding, groundingIsMap := manifest["grounding_decision_receipt"].(map[string]any)
	var groundingDecisions []any
	if groundingIsMap {
		groundingDecisions, _ = grounding["candidate_decisions"].([]any)
	}
	// 新闻候选只由效用判定，从不进入搜索。
	slot := newsSlot(manifest)
	newsCandidateID := ""
	if slot != nil {
		newsCandidateID = pyStrOr(slot["candidate_id"], "")
	}
	newsLeadID := NewsLeadCardId(manifest)
	admittedGroundingIDs := map[string]bool{}
	for _, raw := range pyListMaps(groundingDecisions) {
		if pyStr(raw["decision"]) == "admit" {
			admittedGroundingIDs[pyStr(raw["candidate_id"])] = true
		}
	}
	delete(admittedGroundingIDs, newsCandidateID)
	if len(decisions) != len(admittedGroundingIDs) {
		return failure
	}
	for id := range admittedGroundingIDs {
		if _, present := decisions[id]; !present {
			return failure
		}
	}

	trace, traceIsMap := listSearch["bounded_beam_trace"].(map[string]any)
	if newsOnly && len(decisions) == 0 {
		if _, hasFailure := listSearch["failure_reason"]; hasFailure {
			return failure
		}
		return NewsOnlyListSearchRejectionReason(manifest, cards, listSearch["bounded_beam_trace"])
	}
	if newsOnly && (!listSearchFailureReasons[pyStr(listSearch["failure_reason"])] ||
		!DiversityInvocationCovers(manifest, decisionOrder)) {
		return failure
	}
	if len(decisions) == 0 {
		return failure
	}
	if !traceIsMap || len(trace) != len(boundedBeamTraceFields) {
		return failure
	}
	for field := range boundedBeamTraceFields {
		if _, present := trace[field]; !present {
			return failure
		}
	}
	if pyStr(trace["search_version"]) != LIST_SEARCH_VERSION {
		return failure
	}
	entries, entriesIsList := trace["entries"].([]any)
	if !entriesIsList || len(entries) == 0 {
		return failure
	}
	preferredIDs := pyListStrings(trace["preferred_candidate_ids"])
	if newsOnly {
		if len(preferredIDs) != 0 {
			return failure
		}
	} else if !func() bool {
		anyIDs := make([]any, 0, len(preferredIDs))
		for _, id := range preferredIDs {
			anyIDs = append(anyIDs, id)
		}
		return uniqueNonemptyStrings(anyIDs)
	}() {
		return failure
	}

	orderedCards := append([]map[string]any(nil), cards...)
	sort.SliceStable(orderedCards, func(i, j int) bool {
		return pyIntOf(orderedCards[i]["selector_rank"]) <
			pyIntOf(orderedCards[j]["selector_rank"])
	})
	var orderedCardIDs []string
	for _, card := range orderedCards {
		if pyStr(card["card_id"]) != newsLeadID {
			orderedCardIDs = append(orderedCardIDs, pyStrOr(card["card_id"], ""))
		}
	}
	var selectedDecisionIDs []string
	for _, candidateID := range decisionOrder {
		if payloadMapOrEmpty(decisions[candidateID])["selected"] == true {
			selectedDecisionIDs = append(selectedDecisionIDs, candidateID)
		}
	}
	if !stringSlicesEqual(preferredIDs, orderedCardIDs) {
		return failure
	}
	if !stringBoolSetEqual(toSet(selectedDecisionIDs), toSet(preferredIDs)) {
		return failure
	}
	for _, decision := range decisions {
		if decision["selected"] != true {
			continue
		}
		reasons := pyListStrings(decision["reasons"])
		if len(reasons) != 1 || reasons[0] != "selected_by_host_list_search" {
			return failure
		}
	}

	expectedTargetSize := 4
	if pyStr(manifest["context_scope"]) == GlobalNewChatScope {
		expectedTargetSize = GLOBAL_POOL_TARGET_SIZE
		if newsLeadID != "" {
			expectedTargetSize--
		}
	}
	for _, field := range []string{"target_size", "expansion_count", "beam_width", "max_expansions"} {
		if !plainInt(trace[field]) {
			return failure
		}
	}
	if pyIntOf(trace["target_size"]) != expectedTargetSize {
		return failure
	}
	minExpansion := 1
	if newsOnly {
		minExpansion = 0
	}
	if pyIntOf(trace["expansion_count"]) < minExpansion {
		return failure
	}
	if pyIntOf(trace["beam_width"]) < 1 || pyIntOf(trace["max_expansions"]) < 1 ||
		pyIntOf(trace["expansion_count"]) > pyIntOf(trace["max_expansions"]) {
		return failure
	}

	componentReceipts, receiptsIsMap := manifest["component_receipts"].(map[string]any)
	if !receiptsIsMap {
		return failure
	}
	for _, pair := range [][2]string{
		{"utility_scorer_version", "interaction_utility_scorer"},
		{"diversity_scorer_version", "diversity_scorer"},
	} {
		receipt, isMap := componentReceipts[pair[1]].(map[string]any)
		var version string
		if isMap {
			if binding, ok := receipt["binding"].(map[string]any); ok {
				version = pyStr(binding["config_version"])
			}
		}
		if version == "" || pyStr(trace[pair[0]]) != version {
			return failure
		}
	}

	preferredEntries := 0
	traceCandidateIDs := map[string]bool{}
	for expectedIndex, raw := range entries {
		entry, isMap := raw.(map[string]any)
		if !isMap || len(entry) != len(beamTraceEntryFields) {
			return failure
		}
		for field := range beamTraceEntryFields {
			if _, present := entry[field]; !present {
				return failure
			}
		}
		if pyIntOf(entry["trace_index"]) != expectedIndex+1 {
			return failure
		}
		entryCandidateIDs := pyListStrings(entry["candidate_ids"])
		if !func() bool {
			anyIDs := make([]any, 0, len(entryCandidateIDs))
			for _, id := range entryCandidateIDs {
				anyIDs = append(anyIDs, id)
			}
			return uniqueNonemptyStrings(anyIDs)
		}() {
			return failure
		}
		for _, candidateID := range entryCandidateIDs {
			if _, known := decisions[candidateID]; !known {
				return failure
			}
		}
		if pyIntOf(entry["depth"]) != len(entryCandidateIDs) {
			return failure
		}
		if !beamTraceStatuses[pyStr(entry["status"])] {
			return failure
		}
		if _, isBool := entry["retained_in_beam"].(bool); !isBool {
			return failure
		}
		if _, isBool := entry["hard_constraints_pass"].(bool); !isBool {
			return failure
		}
		if !nullableBool(entry["utility_pass"]) || !nullableBool(entry["diversity_pass"]) {
			return failure
		}
		failureReasons := pyListStrings(entry["failure_reasons"])
		if failureReasons == nil {
			return failure
		}
		seenReasons := map[string]bool{}
		for _, reason := range failureReasons {
			if reason == "" || seenReasons[reason] {
				return failure
			}
			seenReasons[reason] = true
		}
		for _, field := range []string{"mean_utility_nullable",
			"mean_diversity_nullable", "minimum_pairwise_diversity_nullable",
			"objective_nullable"} {
			if !ValidNullableFiniteNumber(entry[field]) {
				return failure
			}
		}
		if pyStr(entry["status"]) == "preferred" {
			preferredEntries++
			if !stringSlicesEqual(entryCandidateIDs, preferredIDs) {
				return failure
			}
			if entry["hard_constraints_pass"] != true ||
				entry["utility_pass"] != true || entry["diversity_pass"] != true {
				return failure
			}
			if len(failureReasons) > 0 {
				return failure
			}
		}
		for _, candidateID := range entryCandidateIDs {
			traceCandidateIDs[candidateID] = true
		}
	}
	if preferredEntries != map[bool]int{true: 0, false: 1}[newsOnly] {
		return failure
	}
	if len(traceCandidateIDs) != len(decisions) {
		return failure
	}
	for id := range decisions {
		if !traceCandidateIDs[id] {
			return failure
		}
	}
	nonRejected := 0
	for _, raw := range entries {
		entry := raw.(map[string]any)
		if pyStr(entry["status"]) != "hard_filter_rejected" {
			nonRejected++
		}
	}
	if pyIntOf(trace["expansion_count"]) != nonRejected {
		return failure
	}

	// 层级偏好对重演（mine_hierarchical_pairs）
	var beamEntries []BeamTraceEntry
	for _, raw := range entries {
		entry := raw.(map[string]any)
		beamEntries = append(beamEntries, BeamTraceEntry{
			TraceIndex:          pyIntOf(entry["trace_index"]),
			CandidateIDs:        pyListStrings(entry["candidate_ids"]),
			Depth:               pyIntOf(entry["depth"]),
			Status:              pyStr(entry["status"]),
			RetainedInBeam:      entry["retained_in_beam"] == true,
			HardConstraintsPass: entry["hard_constraints_pass"] == true,
			FailureReasons:      pyListStrings(entry["failure_reasons"]),
		})
	}
	utilityVersion := pyStr(trace["utility_scorer_version"])
	diversityVersion := pyStr(trace["diversity_scorer_version"])
	beamTrace := &BoundedBeamTrace{
		Entries: beamEntries, PreferredCandidateIDs: preferredIDs,
		TargetSize:             pyIntOf(trace["target_size"]),
		ExpansionCount:         pyIntOf(trace["expansion_count"]),
		BeamWidth:              pyIntOf(trace["beam_width"]),
		MaxExpansions:          pyIntOf(trace["max_expansions"]),
		UtilityScorerVersion:   utilityVersion,
		DiversityScorerVersion: diversityVersion,
		SearchVersion:          LIST_SEARCH_VERSION,
	}
	pairs, err := MineHierarchicalPairs(beamTrace)
	if err != nil {
		return failure
	}
	expectedPairs := make([]any, 0, len(pairs))
	for _, pair := range pairs {
		expectedPairs = append(expectedPairs, pair.AsPayload())
	}
	if !anyEqual(manifest["preference_pairs"], expectedPairs) {
		return "manifest_field_preference_pairs_contract_failed"
	}
	for _, card := range cards {
		if pyStr(card["list_search_version"]) != LIST_SEARCH_VERSION {
			return failure
		}
	}
	return ""
}

// SearchLineageRejectionReason 搜索血缘契约（topic_product.py:4450-4530
// _search_lineage_rejection_reason；原文 _current_search_lineage_rejection_reason
// 同体，常量 lane 集合见 :4439-4448）。
func SearchLineageRejectionReason(manifest map[string]any, cards []map[string]any) string {
	const failure = "search_lineage_contract_failed"
	trigger, _, _ := payloadMap(manifest["generation_trigger_key"])
	if pyStr(manifest["search_provider_id"]) != OpenjiuwenNativeSearchProviderID ||
		pyStr(trigger["search_provider_id"]) != OpenjiuwenNativeSearchProviderID {
		return failure
	}
	manifestReceipts, isMap := manifest["search_receipts"].(map[string]any)
	if !isMap {
		return failure
	}
	for _, card := range cards {
		cardReceipt := card["search_receipt"]
		manifestReceipt := manifestReceipts[pyStr(card["card_id"])]
		if CardUsesHostEvidence(card) {
			if cardReceipt != nil || manifestReceipt != nil {
				return failure
			}
			continue
		}
		if pyStr(card["search_provider_id"]) != OpenjiuwenNativeSearchProviderID {
			return failure
		}
		if !anyEqual(cardReceipt, manifestReceipt) {
			return failure
		}
		if !ValidSearchReceipt(cardReceipt) {
			return failure
		}
		receipt, isMap := cardReceipt.(map[string]any)
		if !isMap {
			return failure
		}
		version := pyStr(receipt["receipt_version"])
		if version != OpenjiuwenNativeSearchReceiptVersion &&
			version != NewsFeedReceiptVersion {
			return failure
		}
	}
	for _, raw := range pyListMaps(payloadMapOrEmpty(manifest["grounding_evidence_receipt"])["candidates"]) {
		search, _, _ := payloadMap(raw["search"])
		if search == nil {
			continue
		}
		providerReceipt, _, _ := payloadMap(search["provider_receipt"])
		if providerReceipt == nil {
			continue
		}
		providerID := pyStr(providerReceipt["provider_id"])
		version := pyStr(providerReceipt["receipt_version"])
		if providerID != OpenjiuwenNativeSearchProviderID &&
			providerID != NewsFeedProviderID {
			return failure
		}
		if version != OpenjiuwenNativeSearchReceiptVersion &&
			version != OpenjiuwenNativeSearchFailureReceiptVersion &&
			version != NewsFeedReceiptVersion {
			return failure
		}
	}
	for _, invocation := range pyListMaps(payloadMapOrEmpty(manifest["external_invocation_audit"])["invocations"]) {
		if pyStr(invocation["operation"]) == "search" &&
			pyStr(invocation["provider_id"]) != OpenjiuwenNativeSearchProviderID &&
			pyStr(invocation["provider_id"]) != NewsFeedProviderID {
			return failure
		}
	}
	return ""
}

// ValidManifest manifest 可读判定（:4374-4387）。
func ValidManifest(manifest any, cards []map[string]any, allowLegacySummaryCoverage bool) bool {
	return ManifestRejectionReason(manifest, cards, allowLegacySummaryCoverage) == ""
}

// ValidColdStartPool 宿主写的通用/热榜卡（无会话前）
// （topic_product.py:4398-4425）。
func ValidColdStartPool(manifest map[string]any, cards []map[string]any) bool {
	if !pythonTruthy(manifest["cold_start"]) {
		return false
	}
	if pyStr(manifest["context_scope"]) != GlobalNewChatScope {
		return false
	}
	if manifest["project_id"] != nil {
		return false
	}
	if len(cards) == 0 || len(cards) > FIRST_SCREEN_CARD_TARGET {
		return false
	}
	var revisions []string
	for _, card := range cards {
		revisions = append(revisions, pyStrOr(card["card_revision_id"], ""))
	}
	var manifestRevisions []string
	for _, revision := range pyListStrings(manifest["card_revision_ids"]) {
		manifestRevisions = append(manifestRevisions, revision)
	}
	if !stringSlicesEqual(revisions, manifestRevisions) {
		return false
	}
	for _, card := range cards {
		if !pythonTruthy(card["cold_start"]) ||
			pyStr(card["card_revision_id"]) == "" ||
			pyStr(card["title"]) == "" || pyStr(card["prompt"]) == "" ||
			pyStr(card["context_scope"]) != GlobalNewChatScope {
			return false
		}
	}
	return true
}

// ReadablePoolManifest manifest 可读（原始或冷启动形态）
// （topic_product.py:4427-4432）。
func ReadablePoolManifest(manifest map[string]any, cards []map[string]any,
	allowLegacySummaryCoverage bool) bool {
	return ValidManifest(manifest, cards, allowLegacySummaryCoverage) ||
		ValidColdStartPool(manifest, cards)
}

// ManifestRejectionReason 共享校验器的确定性、无内容原因
// （topic_product.py:4186-4373 _manifest_rejection_reason）。
func ManifestRejectionReason(manifest any, cards []map[string]any,
	allowLegacySummaryCoverage bool) string {
	manifestMap, isMap := manifest.(map[string]any)
	if !isMap {
		return "manifest_type_contract_failed"
	}
	var missing []string
	for field := range manifestFields {
		if _, present := manifestMap[field]; !present {
			missing = append(missing, field)
		}
	}
	if len(missing) > 0 {
		sort.Strings(missing)
		return fmt.Sprintf("manifest_required_field_%s_missing", missing[0])
	}
	trigger, triggerIsMap := manifestMap["generation_trigger_key"].(map[string]any)
	if !triggerIsMap {
		return "manifest_field_generation_trigger_key_type_failed"
	}
	scope, scopeIsStr := manifestMap["context_scope"].(string)
	if !scopeIsStr {
		return "manifest_field_context_scope_type_failed"
	}
	receipts, receiptsIsMap := manifestMap["component_receipts"].(map[string]any)
	if !receiptsIsMap {
		return "manifest_field_component_receipts_type_failed"
	}
	rawRetained, hasRetained := manifestMap["retained_candidates"]
	if hasRetained && rawRetained != nil {
		retained, isMap := rawRetained.(map[string]any)
		if !isMap {
			return "manifest_field_retained_candidates_type_failed"
		}
		for candidateID, snapshotID := range retained {
			if candidateID == "" || pyStr(snapshotID) == "" {
				return "manifest_field_retained_candidates_type_failed"
			}
		}
	}
	fieldChecks := [][2]any{
		{"policy_id", pyStr(manifestMap["policy_id"]) == POLICY_ID},
		{"payload_version", pyStr(manifestMap["payload_version"]) == PAYLOAD_VERSION},
		{"trigger_origin", containsString([]string{"scheduled_cron", "lazy_seed",
			"manual_retry", "news_check"}, pyStr(manifestMap["trigger_origin"]))},
		{"context_scope", ValidScope(scope, manifestMap["project_id"])},
	}
	for _, check := range fieldChecks {
		if check[1] != true {
			return fmt.Sprintf("manifest_field_%s_contract_failed", check[0])
		}
	}
	triggerMatches := [][2]any{
		{"context_scope", scope == pyStr(trigger["context_scope"])},
		{"project_id", anyEqual(manifestMap["project_id"], trigger["project_id"])},
		{"local_date", pyStr(manifestMap["local_date"]) == pyStr(trigger["local_date"])},
		{"profile_semantic_version", anyEqual(manifestMap["profile_semantic_version"],
			trigger["profile_semantic_version"])},
		{"short_intent_version", anyEqual(manifestMap["short_intent_version"],
			trigger["short_intent_version"])},
		{"negative_preference_version", anyEqual(manifestMap["negative_preference_version"],
			trigger["negative_preference_version"])},
		{"generation_config_version", anyEqual(manifestMap["generation_config_version"],
			trigger["generation_config_version"])},
		{"search_provider_id", anyEqual(manifestMap["search_provider_id"],
			trigger["search_provider_id"])},
	}
	for _, match := range triggerMatches {
		if match[1] != true {
			return fmt.Sprintf("manifest_field_%s_trigger_mismatch", match[0])
		}
	}
	if !anyEqual(manifestMap["serving_compatibility_key"],
		BuildServingCompatibilityKey(trigger)) {
		return "manifest_field_serving_compatibility_key_contract_failed"
	}
	if !validInputRevisionReceipt(trigger["input_revision_receipt"], scope) {
		return "manifest_field_input_revision_receipt_contract_failed"
	}
	if hasSecretField(manifestMap) {
		return "manifest_secret_field_contract_failed"
	}
	if pyStr(manifestMap["generation_input_snapshot_id"]) == "" {
		return "manifest_field_generation_input_snapshot_id_contract_failed"
	}
	if !ValidStateMaintenanceReceipt(manifestMap["state_maintenance_receipt"],
		manifestMap["profile_semantic_version"],
		manifestMap["negative_preference_version"]) {
		return "manifest_field_state_maintenance_receipt_contract_failed"
	}
	if reason := TerminalLearningLineageRejectionReason(manifest, cards,
		allowLegacySummaryCoverage); reason != "" {
		return reason
	}
	allContinuations := len(cards) > 0
	corpusLineage := false
	for _, invocation := range pyListMaps(manifestMap["component_invocation_receipts"]) {
		if pyStr(invocation["component_id"]) == "pa_bridge_fixed_retriever" {
			corpusLineage = true
		}
	}
	hostEvidence := len(cards) > 0
	for _, card := range cards {
		if !CardUsesHostEvidence(card) {
			hostEvidence = false
		}
	}
	newsLead := NewsOnlyPool(manifestMap, cards) &&
		!(func() bool {
			listSearch, isMap := manifestMap["list_search"].(map[string]any)
			return isMap && len(pyListMaps(listSearch["candidate_decisions"])) > 0
		})()
	category := ""
	if allContinuations {
		for _, card := range cards {
			if pyStr(card["category"]) != "project_continuation" {
				allContinuations = false
			}
		}
		if allContinuations {
			category = "project_continuation"
		}
	}
	if !ValidReceipts(receipts, pyStr(manifestMap["generation_input_snapshot_id"]),
		scope, category, corpusLineage, hostEvidence, newsLead) {
		return "manifest_field_component_receipts_contract_failed"
	}
	if !ReceiptsMatchTrigger(receipts, trigger) {
		return "manifest_field_component_receipts_trigger_mismatch"
	}
	if !ValidPoolComposition(scope, cards) {
		return "cards_pool_composition_contract_failed"
	}
	if reason := NewsSlotRejectionReason(manifestMap, cards); reason != "" {
		return reason
	}
	if _, hasSuccessor := manifestMap["successor_kind"]; hasSuccessor &&
		(pyStr(manifestMap["successor_kind"]) != NewsOnlySuccessor ||
			NewsLeadCardId(manifestMap) == "") {
		return "manifest_field_successor_kind_contract_failed"
	}
	ordered := OrderedCardsForDiagnosis(cards)
	if reason := ListSearchRejectionReason(manifestMap, ordered); reason != "" {
		return reason
	}
	for _, reason := range []string{
		ComponentLineageRejectionReason(manifestMap, cards),
		ProviderLineageRejectionReason(manifestMap, cards),
		SearchLineageRejectionReason(manifestMap, cards),
	} {
		if reason != "" {
			return reason
		}
	}
	newsLeadID := NewsLeadCardId(manifestMap)
	for index, card := range ordered {
		if !ValidCard(card, trigger, pyStr(card["card_id"]) == newsLeadID) {
			return fmt.Sprintf("card_index_%d_contract_failed", index)
		}
	}
	var ranks []any
	var revisions []any
	for _, card := range ordered {
		ranks = append(ranks, card["selector_rank"])
		revisions = append(revisions, card["card_revision_id"])
	}
	sequential := make([]any, 0, len(cards))
	for index := 1; index <= len(cards); index++ {
		sequential = append(sequential, index)
	}
	if !anyEqual(ranks, sequential) {
		return "cards_selector_rank_contract_failed"
	}
	seenRevisions := map[string]bool{}
	for _, revision := range revisions {
		if seenRevisions[pyStr(revision)] {
			return "cards_card_revision_id_duplicate"
		}
		seenRevisions[pyStr(revision)] = true
	}
	if !anyEqual(manifestMap["card_revision_ids"], revisions) {
		return "manifest_field_card_revision_ids_membership_mismatch"
	}
	for index, card := range ordered {
		if !anyEqual(card["generation_batch_id"], manifestMap["generation_batch_id"]) {
			return fmt.Sprintf("card_index_%d_generation_batch_mismatch", index)
		}
		if !anyEqual(card["state_maintenance_receipt"],
			manifestMap["state_maintenance_receipt"]) {
			return fmt.Sprintf("card_index_%d_state_maintenance_receipt_mismatch", index)
		}
	}
	shortDeltaID, ok := ShortDeltaInterestID(manifestMap)
	if !ok || shortDeltaID == "" {
		if shortDeltaID == "" {
			return "manifest_field_rid_interest_selection_contract_failed"
		}
		return "manifest_field_rid_interest_selection_contract_failed"
	}
	if shortDeltaID != nil {
		if scope != GlobalNewChatScope {
			return "manifest_short_delta_scope_contract_failed"
		}
		covered := false
		exposureCovered := false
		for index, card := range ordered {
			if anyEqual(card["interest_summary_id"], shortDeltaID) {
				covered = true
				if index < 4 {
					exposureCovered = true
				}
			}
		}
		if !covered {
			return "manifest_short_delta_pool_coverage_failed"
		}
		if !exposureCovered {
			return "manifest_short_delta_exposure_coverage_failed"
		}
	}
	return ""
}

// LoadManifestCards 经不可变 manifest 归属读取一份池
// （topic_product.py:5127-5149 _load_manifest_cards）。
func LoadManifestCards(store *TopicStarterStore, manifestID,
	generationBatchID string) (map[string]any, []map[string]any) {
	rows, err := store.QueryEvents(QueryEventsOptions{
		EventType:   "starter_slate_manifest",
		SlateID:     stringPtr(manifestID),
		PolicyID:    stringPtr(POLICY_ID),
		Limit:       1,
		NewestFirst: true,
	})
	if err != nil {
		return nil, nil
	}
	if len(rows) == 0 {
		return nil, nil
	}
	manifest, _, ok := payloadMap(rows[0]["payload"])
	if !ok {
		return nil, nil
	}
	if pyStr(manifest["generation_batch_id"]) != generationBatchID {
		return nil, nil
	}
	cards := LoadCardRevisions(store, manifest)
	if !ReadablePoolManifest(manifest, cards, true) {
		return nil, nil
	}
	return manifest, cards
}

// LoadCardRevisions manifest 点名的卡修订（topic_product.py:5151-5175）。
func LoadCardRevisions(store *TopicStarterStore, manifest map[string]any) []map[string]any {
	manifestID := pyStr(manifest["manifest_id"])
	generationBatchID := pyStr(manifest["generation_batch_id"])
	if manifestID == "" || generationBatchID == "" {
		return nil
	}
	cardRows, err := store.QueryEvents(QueryEventsOptions{
		EventType: "starter_card",
		SlateID:   stringPtr(manifestID),
		PolicyID:  stringPtr(POLICY_ID),
		Limit:     7,
	})
	if err != nil {
		return nil
	}
	byRevision := map[string]map[string]any{}
	for _, row := range cardRows {
		payload, _, ok := payloadMap(row["payload"])
		if !ok {
			continue
		}
		if pyStr(payload["generation_batch_id"]) != generationBatchID {
			continue
		}
		byRevision[pyStr(payload["card_revision_id"])] = payload
	}
	var revisions []string
	seen := map[string]bool{}
	duplicated := false
	for _, revision := range pyListStrings(manifest["card_revision_ids"]) {
		if seen[revision] {
			duplicated = true
		}
		seen[revision] = true
		revisions = append(revisions, revision)
	}
	if duplicated {
		return nil
	}
	var cards []map[string]any
	for _, revision := range revisions {
		if card, present := byRevision[revision]; present {
			cards = append(cards, card)
		}
	}
	if len(cards) != len(revisions) {
		return nil
	}
	return cards
}

// CompatiblePool 本读取方可服务的最新 manifest 及其卡修订
// （topic_product.py:5465-5509 _compatible_pool）。
func CompatiblePool(store *TopicStarterStore, servingCompatibilityKey map[string]any,
	contextScope string, projectID any, negativePreferenceVersion int) (map[string]any, []map[string]any) {
	rows, err := store.QueryEvents(QueryEventsOptions{
		EventType: "starter_slate_manifest",
		PolicyID:  stringPtr(POLICY_ID),
		PayloadEquals: map[string]any{
			"serving_compatibility_key": servingCompatibilityKey,
			"context_scope":             contextScope,
			"project_id":                projectID,
		},
		PayloadIntegerLTE: &PayloadIntegerLTE{
			Field: "negative_preference_version", Value: negativePreferenceVersion},
		OrderByPayloadIntegerDesc: stringPtr("negative_preference_version"),
		Limit:                     1,
		NewestFirst:               true,
	})
	if err != nil {
		return nil, nil
	}
	if len(rows) == 0 {
		return nil, nil
	}
	manifest, _, ok := payloadMap(rows[0]["payload"])
	if !ok {
		return nil, nil
	}
	cards := LoadCardRevisions(store, manifest)
	if !ReadablePoolManifest(manifest, cards, true) {
		return nil, nil
	}
	return manifest, cards
}

// RetainablePool 继任生成可携带的卡及其兴趣的会话事件
// （topic_product.py:6470-6502）。
type RetainablePool struct {
	// Cards 可携带的卡
	Cards []map[string]any
	// InterestEvents 池兴趣的会话事件
	InterestEvents map[string][]string
}

// RetainablePoolFor 继任可携带集（topic_product.py:6477-6502
// retainable_pool）。
func RetainablePoolFor(store *TopicStarterStore, servingCompatibilityKey map[string]any,
	contextScope string, projectID any, feedback *InterestFeedback,
	now float64) RetainablePool {
	if !ValidScope(contextScope, projectID) {
		return RetainablePool{}
	}
	manifest, cards := CompatiblePool(store, servingCompatibilityKey, contextScope,
		projectID, feedback.Version())
	if manifest == nil {
		return RetainablePool{}
	}
	events := map[string][]string{}
	for interestID, evidence := range InterestEvidenceIds(manifest) {
		events[interestID] = evidence
	}
	return RetainablePool{
		Cards:          EligiblePoolCards(store, manifest, cards, contextScope, projectID, feedback, now),
		InterestEvents: events,
	}
}

// EligiblePoolCards 仍值得展示的池卡：否定放行、在期、未打开
// （topic_product.py:6371-6397）。
func EligiblePoolCards(store *TopicStarterStore, manifest map[string]any,
	cards []map[string]any, contextScope string, projectID any,
	feedback *InterestFeedback, now float64) []map[string]any {
	var eligible []map[string]any
	admittedCards := feedback.Admitted(manifest, cards)
	for _, card := range admittedCards {
		if pyStr(card["context_scope"]) != contextScope {
			continue
		}
		if !anyEqual(card["project_id"], projectID) {
			continue
		}
		if !(toFloatOr(card["effective_time"], 0) <= now &&
			now < toFloatOr(card["expire_time"], 0)) {
			continue
		}
		if !ValidEvidence(card) {
			continue
		}
		if CardOpened(store, card) {
			continue
		}
		if !ProjectSourcesStillBelong(manifest, card) {
			continue
		}
		eligible = append(eligible, card)
	}
	return eligible
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// validInputRevisionReceipt 输入修订小票校验（topic_product.py:3277-3288）。
func validInputRevisionReceipt(receipt any, contextScope string) bool {
	root, isMap := receipt.(map[string]any)
	if !isMap {
		return false
	}
	fields := 3
	if _, hasDigest := root["discussion_coverage_digest"]; hasDigest {
		fields = 4
		if pyStr(root["discussion_coverage_digest"]) == "" {
			return false
		}
	}
	if len(root) != fields {
		return false
	}
	for _, field := range []string{"receipt_version", "context_scope", "revision_digest"} {
		if _, present := root[field]; !present {
			return false
		}
	}
	return pyStr(root["receipt_version"]) == InputRevisionReceiptVersion &&
		pyStr(root["context_scope"]) == contextScope &&
		pyStr(root["revision_digest"]) != ""
}

// nullableBool nil 或 bool 判定。
func nullableBool(value any) bool {
	if value == nil {
		return true
	}
	_, ok := value.(bool)
	return ok
}

// toSet 字符串集合。
func toSet(values []string) map[string]bool {
	set := map[string]bool{}
	for _, value := range values {
		set[value] = true
	}
	return set
}

// stringBoolSetEqual 集合等值。
func stringBoolSetEqual(a, b map[string]bool) bool {
	if len(a) != len(b) {
		return false
	}
	for value := range a {
		if !b[value] {
			return false
		}
	}
	return true
}

// stringSlicesEqual 顺序等值。
func stringSlicesEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for index := range a {
		if a[index] != b[index] {
			return false
		}
	}
	return true
}

// loggerWarnAlias 复用 d5 的 loggerWarn。
var _ = logger.Warn
var _ = swarmsession.GetSessionMetadata
var _ = strings.TrimSpace

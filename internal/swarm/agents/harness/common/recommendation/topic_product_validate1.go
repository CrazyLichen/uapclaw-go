package recommendation

// topic_product_validate1.go 一比一对齐上游 topic_product.py:3392-3790 与
// :1878-1884 的组件小票/宿主小票/证据校验族（⤴️31c 归位：随 topic_product
// 主体迁移时移入对应文件）。既有 Go 已落 ValidSearchReceipt/ValidReceipts/
// hasSecretField/corpusHostReceipt/hotListHostReceipt/CardUsesHostEvidence。

import (
	"fmt"
)

// ──────────────────────────── 常量 ────────────────────────────

// FixedCorpusCategories 固定语料类目集（topic_product.py:101）。
var FixedCorpusCategories = map[string]bool{
	"adjacent_explanation": true, "broad_timely_practical": true,
}

// ──────────────────────────── 导出函数 ────────────────────────────

// ValidStateMaintenanceReceipt 状态维护小票校验
// （topic_product.py:3459-3530 _valid_state_maintenance_receipt）。
func ValidStateMaintenanceReceipt(receipt any, profileSemanticVersion,
	negativePreferenceVersion any) bool {
	root, isMap := receipt.(map[string]any)
	if !isMap || len(root) != len(stateMaintenanceReceiptFields) {
		return false
	}
	for _, field := range stateMaintenanceReceiptFields {
		if _, present := root[field]; !present {
			return false
		}
	}
	for _, field := range []string{"source_watermark", "history_revision",
		"mapping_watermark", "v4_state_version", "profile_semantic_version",
		"negative_preference_version"} {
		if !plainInt(root[field]) {
			return false
		}
	}
	sourceWatermark := pyIntOf(root["source_watermark"])
	historyRevision := pyIntOf(root["history_revision"])
	mappingWatermark := pyIntOf(root["mapping_watermark"])
	stateVersion := pyIntOf(root["v4_state_version"])
	if pyStr(root["receipt_version"]) != StateMaintenanceReceiptVersion {
		return false
	}
	if stateVersion <= 0 || sourceWatermark <= 0 || mappingWatermark <= 0 ||
		mappingWatermark > sourceWatermark || historyRevision < 0 ||
		pyIntOf(root["profile_semantic_version"]) < 0 ||
		pyIntOf(root["negative_preference_version"]) < 0 {
		return false
	}
	if pyIntOf(root["profile_semantic_version"]) != pyIntOf(profileSemanticVersion) {
		return false
	}
	if pyIntOf(root["negative_preference_version"]) != pyIntOf(negativePreferenceVersion) {
		return false
	}
	if pyStr(root["refresh_reason"]) == "" {
		return false
	}
	inputSnapshotID := fmtSprintD("v4-state-input:%d:%d:%d",
		stateVersion, sourceWatermark, historyRevision)
	return pyStr(root["input_snapshot_id"]) == inputSnapshotID &&
		pyStr(root["receipt_id"]) == "state-receipt:"+inputSnapshotID &&
		pyStr(root["resolved_mapping_id"]) == fmtSprintD("v4-mapping:%d:%d",
			stateVersion, mappingWatermark) &&
		pyStr(root["v4_state_id"]) == fmtSprintD("v4-state:%d", stateVersion) &&
		pyStr(root["profile_snapshot_id"]) == fmtSprintD("profile-snapshot:%d", stateVersion)
}

// ProfileInterestHostReceipt 画像兴趣宿主小票（:3565-3577）。
func ProfileInterestHostReceipt(card map[string]any) map[string]any {
	if pyStr(card["category"]) != ProfileInterestCategory {
		return nil
	}
	bundle, isMap := card["evidence_bundle"].(map[string]any)
	if !isMap {
		return nil
	}
	receipt, isMap := bundle["host_receipt"].(map[string]any)
	if isMap && pyStr(receipt["lane"]) == "host_profile_interest_locator" {
		return receipt
	}
	return nil
}

// ValidProfileInterestHostReceipt 画像兴趣宿主小票校验（:3579-3604）。
func ValidProfileInterestHostReceipt(card map[string]any) bool {
	bundleMap, isMap := card["evidence_bundle"].(map[string]any)
	if !isMap {
		return false
	}
	receipt := ProfileInterestHostReceipt(card)
	if receipt == nil {
		return false
	}
	expected := map[string]bool{
		"receipt_version": true, "lane": true, "input_snapshot_id": true,
		"interest_summary_id": true, "document_revision_id": true,
	}
	if len(receipt) != len(expected) {
		return false
	}
	for field := range expected {
		if _, present := receipt[field]; !present {
			return false
		}
	}
	return card["search_receipt"] == nil &&
		pyStr(receipt["receipt_version"]) == ProfileInterestReceiptVersion &&
		pyStr(receipt["input_snapshot_id"]) == pyStr(card["generation_input_snapshot_id"]) &&
		pyStr(receipt["interest_summary_id"]) != "" &&
		pyStr(receipt["interest_summary_id"]) == pyStr(card["interest_summary_id"]) &&
		pyStr(receipt["document_revision_id"]) == pyStr(bundleMap["document_revision_id"]) &&
		pyStr(bundleMap["source_url"]) == "profile://"+pyStr(receipt["interest_summary_id"])
}

// ValidHotListHostReceipt 热榜宿主小票校验（:3607-3665）。
func ValidHotListHostReceipt(card map[string]any) bool {
	bundle, isMap := card["evidence_bundle"].(map[string]any)
	if !isMap {
		return false
	}
	receipt := hotListHostReceipt(card)
	if receipt == nil {
		return false
	}
	expected := map[string]bool{
		"receipt_version": true, "lane": true, "input_snapshot_id": true,
		"pull_receipt_id": true, "provider_id": true, "list_url": true,
		"hot_entry_id": true, "hot_list_rank": true, "document_revision_id": true,
	}
	if len(receipt) != len(expected) {
		return false
	}
	for field := range expected {
		if _, present := receipt[field]; !present {
			return false
		}
	}
	if card["search_receipt"] != nil {
		return false
	}
	if pyStr(bundle["source_kind"]) != HotListSourceKind ||
		pyStr(receipt["receipt_version"]) != HotListEntryReceiptVersion ||
		pyStr(receipt["input_snapshot_id"]) != pyStr(card["generation_input_snapshot_id"]) {
		return false
	}
	for _, field := range []string{"pull_receipt_id", "provider_id", "hot_entry_id"} {
		if pyStr(receipt[field]) == "" {
			return false
		}
	}
	if !ValidHTTPURL(receipt["list_url"]) {
		return false
	}
	if !plainInt(receipt["hot_list_rank"]) || pyIntOf(receipt["hot_list_rank"]) <= 0 {
		return false
	}
	return pyStr(receipt["document_revision_id"]) == pyStr(bundle["document_revision_id"]) &&
		ValidHTTPURL(bundle["source_url"])
}

// ValidProjectHostReceipt 项目定位宿主小票校验（:3677-3710）。
func ValidProjectHostReceipt(card map[string]any) bool {
	bundle, isMap := card["evidence_bundle"].(map[string]any)
	if !isMap {
		return false
	}
	receipt, isMap := bundle["host_receipt"].(map[string]any)
	if !isMap {
		return false
	}
	locators, isList := card["project_source_locators"].([]any)
	if !isList || len(locators) != 1 {
		return false
	}
	expected := map[string]bool{
		"receipt_version": true, "lane": true, "input_snapshot_id": true,
		"project_id": true, "source_locator": true, "document_revision_id": true,
	}
	if len(receipt) != len(expected) {
		return false
	}
	for field := range expected {
		if _, present := receipt[field]; !present {
			return false
		}
	}
	return pyStr(receipt["receipt_version"]) == ProjectLocatorReceiptVersion &&
		pyStr(receipt["lane"]) == "host_project_exact_locator" &&
		pyStr(receipt["input_snapshot_id"]) == pyStr(card["generation_input_snapshot_id"]) &&
		pyStr(receipt["project_id"]) == pyStr(card["project_id"]) &&
		anyEqual(receipt["source_locator"], locators[0]) &&
		pyStr(receipt["document_revision_id"]) == pyStr(bundle["document_revision_id"])
}

// ReceiptsMatchTrigger 小票契约版本与厂商版本对触发键全等
// （topic_product.py:3711-3725 _receipts_match_trigger）。
func ReceiptsMatchTrigger(receipts map[string]any, trigger map[string]any) bool {
	contractVersions, isMap := trigger["component_contract_versions"].(map[string]any)
	if !isMap || pyStr(trigger["model"]) == "" {
		return false
	}
	for componentID, rawReceipt := range receipts {
		receipt, isMap := rawReceipt.(map[string]any)
		if !isMap {
			return false
		}
		binding := payloadMapOrEmpty(receipt["binding"])
		if pyStr(binding["contract_version"]) != pyStr(contractVersions[componentID]) ||
			pyStr(binding["provider_version"]) != pyStr(trigger["model"]) {
			return false
		}
	}
	return true
}

// ValidEvidence 卡证据束校验（topic_product.py:3727-3760 _valid_evidence）。
func ValidEvidence(card map[string]any) bool {
	bundle, isMap := card["evidence_bundle"].(map[string]any)
	if !isMap {
		return false
	}
	boundary, isMap := bundle["content_boundary"].(map[string]any)
	if !isMap {
		return false
	}
	text, textIsStr := boundary["text"].(string)
	start := pyIntOf(boundary["start"])
	end := pyIntOf(boundary["end"])
	baseValid := pyStr(bundle["fetch_status"]) == "success" &&
		pyStr(bundle["source_url"]) != "" &&
		pyStr(bundle["source_kind"]) != "" &&
		pyStr(bundle["document_revision_id"]) != "" &&
		textIsStr && text != "" &&
		plainInt(boundary["start"]) && plainInt(boundary["end"]) &&
		0 <= start && start < end &&
		end-start == len([]rune(text))
	if !baseValid {
		return false
	}
	if pyStr(bundle["source_kind"]) == HotListSourceKind {
		// 热榜词条按上架时间计日，无发布时间。
		return pyStr(card["category"]) == "profile_recent_progress"
	}
	switch pyStr(card["category"]) {
	case "profile_recent_progress", "project_domain_progress":
		return pyStr(bundle["published_at"]) != "" &&
			pyStr(bundle["published_at_source"]) != ""
	}
	return pyStr(bundle["source_kind"]) == "authoritative" ||
		pyStr(bundle["source_kind"]) == "original" ||
		pyStr(bundle["source_kind"]) == "stable"
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// fmtSprintD 带格式的小票 ID 构造。
func fmtSprintD(format string, args ...any) string {
	return fmt.Sprintf(format, args...)
}

// ValidCorpusHostReceipt 固定语料宿主小票校验（:3541-3563）。
func ValidCorpusHostReceipt(card map[string]any) bool {
	bundle, isMap := card["evidence_bundle"].(map[string]any)
	if !isMap {
		return false
	}
	receipt := corpusHostReceipt(card)
	if receipt == nil {
		return false
	}
	expected := map[string]bool{
		"receipt_version": true, "lane": true, "input_snapshot_id": true,
		"corpus_version": true, "starter_id": true, "document_revision_id": true,
	}
	if len(receipt) != len(expected) {
		return false
	}
	for field := range expected {
		if _, present := receipt[field]; !present {
			return false
		}
	}
	return card["search_receipt"] == nil &&
		pyStr(receipt["receipt_version"]) == FixedCorpusReceiptVersion &&
		pyStr(receipt["input_snapshot_id"]) == pyStr(card["generation_input_snapshot_id"]) &&
		pyStr(receipt["corpus_version"]) == pyStr(card["corpus_version"]) &&
		pyStr(receipt["starter_id"]) != "" &&
		pyStr(receipt["starter_id"]) == pyStr(card["source_starter_id"]) &&
		pyStr(receipt["document_revision_id"]) == pyStr(bundle["document_revision_id"]) &&
		pyStr(bundle["source_url"]) == "corpus://"+pyStr(receipt["corpus_version"])+
			"/"+pyStr(receipt["starter_id"])
}

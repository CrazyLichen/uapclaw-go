package recommendation

// topic_pipeline_b.go 一比一对齐上游 topic_pipeline.py:1032-1850（B 段）：
// v6 Ark 阶段调用器工厂、审计包装、宿主证据、候选计划构造与来源分类器。
// news 小助手（normalize_news_query/search_page_url，topic_news.py:64-84）
// ⤴️31c 归位 topic_news.go；TerminalGenerationRejection（:2546）为 C 段类型前置。

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"regexp"
	"strings"
	"time"
)

// ──────────────────────────── 常量 ────────────────────────────

// RestatedOfferedSimilarity 与已提供标题的复述判定阈值（topic_pipeline.py:1173）
const RestatedOfferedSimilarity = 0.45

// titleSimilarityIgnored 标题相似度忽略的标点/空白/引号（:1174）
var titleSimilarityIgnored = regexp.MustCompile(`[\s，。？?！!、：:；;“”"'（）()《》「」]`)

// newsFeedURL Bing 新闻端点（topic_news.py:41 ⤴️31c 归位）
const newsFeedURL = "https://www.bing.com/news/search"

// 来源分类主机集合（topic_pipeline.py:264-330）
var (
	originalSourceHosts = map[string]bool{
		"academic.oup.com": true, "aclanthology.org": true, "arxiv.org": true,
		"cell.com": true, "dl.acm.org": true, "frontiersin.org": true,
		"ieeexplore.ieee.org": true, "link.springer.com": true, "mdpi.com": true,
		"onlinelibrary.wiley.com": true, "openreview.net": true, "plos.org": true,
		"pnas.org": true, "proceedings.neurips.cc": true, "sciencedirect.com": true,
	}
	originalArticlePathHosts = map[string]bool{"nature.com": true}
	authoritativeSourceHosts = map[string]bool{
		"ai.meta.com": true, "anthropic.com": true, "huawei.com": true,
		"microsoft.com": true, "openjiuwen.com": true, "openjiuwen.github.io": true,
		"openai.com": true, "research.google": true, "sqlite.org": true,
		"volcengine.com": true, "who.int": true,
	}
	reliableReportHosts = map[string]bool{
		"apnews.com": true, "bbc.com": true, "nature.com": true,
		"reuters.com": true, "science.org": true, "technologyreview.com": true,
		"thepaper.cn": true, "xinhuanet.com": true,
	}
	stableSourceHosts        = map[string]bool{"britannica.com": true, "wikipedia.org": true}
	githubNonRepositoryRoots = map[string]bool{
		"apps": true, "collections": true, "events": true, "features": true,
		"marketplace": true, "orgs": true, "search": true, "sponsors": true,
		"topics": true, "trending": true, "users": true,
	}
)

// ──────────────────────────── 结构体 ────────────────────────────

// TerminalGenerationRejection 终局生成可拒绝失败（topic_pipeline.py:2546-2549）。
type TerminalGenerationRejection struct {
	// Reason 拒绝原因码
	Reason string
}

// Error 实现 error 接口。
func (e *TerminalGenerationRejection) Error() string { return e.Reason }

// auditedStageInvoker 在进入 Ark 适配器前落宿主请求身份的包装
// （topic_pipeline.py:1067-1103）。
type auditedStageInvoker struct {
	base  *ArkStageInvoker
	audit *ExternalInvocationAudit
}

// Invoke 包装调用（对齐 invoke 闭包）。
func (w *auditedStageInvoker) Invoke(ctx context.Context, stage, systemPrompt, payload string) (any, error) {
	var parsed map[string]any
	if err := json.Unmarshal([]byte(payload), &parsed); err != nil {
		return nil, &TerminalGenerationRejection{Reason: "external_invocation_request_contract_failed"}
	}
	requestID := strings.TrimSpace(pyStr(parsed["request_id"]))
	if requestID == "" {
		return nil, &TerminalGenerationRejection{Reason: "external_invocation_request_id_missing"}
	}
	if stage != "v6_evidence_extractor" {
		if auditErr := w.audit.RecordAttempt("ark",
			fmt.Sprintf("external-operation-%s", newUUIDHex()), requestID,
			stage, "ark", ""); auditErr != nil {
			return nil, auditErr
		}
	}
	return w.base.Invoke(ctx, stage, V6SystemPrompt(stage, systemPrompt), payload)
}

// RecordValidation 校验记录透传（对齐 setattr(record_validation, ...)）。
func (w *auditedStageInvoker) RecordValidation(response any, outcome string) {
	w.base.RecordValidation(response, outcome)
}

// InvocationReceipts 调用小票透传（对齐 setattr(invocation_receipts, ...)）。
func (w *auditedStageInvoker) InvocationReceipts() []map[string]any {
	return w.base.InvocationReceipts()
}

// v6ArkStageInvoker V6 阶段调用器（topic_pipeline.py:1032-1065）。
type v6ArkStageInvoker struct {
	base *ArkStageInvoker
}

// Invoke 先套 V6 系统提示词分发再进基调用（对齐 invoke 闭包）。
func (w *v6ArkStageInvoker) Invoke(ctx context.Context, stage, systemPrompt, payload string) (any, error) {
	return w.base.Invoke(ctx, stage, V6SystemPrompt(stage, systemPrompt), payload)
}

// RecordValidation 校验记录透传。
func (w *v6ArkStageInvoker) RecordValidation(response any, outcome string) {
	w.base.RecordValidation(response, outcome)
}

// InvocationReceipts 调用小票透传。
func (w *v6ArkStageInvoker) InvocationReceipts() []map[string]any {
	return w.base.InvocationReceipts()
}

// ──────────────────────────── 导出函数 ────────────────────────────

// BuildV6ArkStageInvoker 用严格 JSON Schema 并要求不可变 provider 元数据
// （topic_pipeline.py:1032-1065）。
func BuildV6ArkStageInvoker(config *ArkBackgroundConfig, clientFactory ChatClientFactory,
	receiptSink func(map[string]any)) *v6ArkStageInvoker {
	return &v6ArkStageInvoker{
		base: BuildArkStageInvoker(config, clientFactory,
			func(stage, payload string) map[string]any {
				schema, err := V6ResponseSchema(stage, payload)
				if err != nil || schema == nil {
					return nil
				}
				return schema
			},
			true,
			receiptSink,
			func(stage string) int { return intOf(V6MaxTokens(stage)) },
			func(stage string) string { return V6PromptVersion(stage) },
			func(stage, payload string, result any) map[string]any {
				return V6ProviderInvocationLineage(stage, payload, result)
			},
			false,
		),
	}
}

// AuditedArkStageInvoker 在进入 Ark 适配器前落宿主请求身份（:1067-1103）。
func AuditedArkStageInvoker(invokeStage *ArkStageInvoker, audit *ExternalInvocationAudit) *auditedStageInvoker {
	return &auditedStageInvoker{base: invokeStage, audit: audit}
}

// HostEvidence 宿主自有证据（topic_pipeline.py:1105-1113）。
func HostEvidence(plan map[string]any) map[string]any {
	for _, key := range []string{"project_evidence", "profile_evidence", "hot_list_evidence"} {
		if evidence, isMap := plan[key].(map[string]any); isMap {
			return evidence
		}
	}
	return nil
}

// HostGroundedPlan 由用户自身历史或摘要锚定的计划（:1161-1164）。
func HostGroundedPlan(plan, hostEvidence map[string]any) map[string]any {
	merged := map[string]any{}
	for key, value := range plan {
		merged[key] = value
	}
	merged["evidence_bundle"] = copyMapAny(hostEvidence)
	merged["evidence_receipt"] = nil
	return merged
}

// OfferedTitlesByInterest 上一池仍提供的标题（:1166-1171）。
func OfferedTitlesByInterest(retained []map[string]any) map[string][]string {
	offered := map[string][]string{}
	for _, plan := range retained {
		interestID := pyStr(plan["interest_summary_id"])
		title := pyStr(plan["title"])
		if interestID != "" && title != "" {
			offered[interestID] = append(offered[interestID], title)
		}
	}
	return offered
}

// bigramJaccard 忽略空白、标点与引号后的二元组 Jaccard。
func bigramJaccard(title, offered string) float64 {
	bigrams := func(text string) map[string]bool {
		text = titleSimilarityIgnored.ReplaceAllString(text, "")
		runes := []rune(text)
		result := map[string]bool{}
		for i := 0; i+1 < len(runes); i++ {
			result[string(runes[i:i+2])] = true
		}
		return result
	}
	left, right := bigrams(title), bigrams(offered)
	union := len(left)
	for key := range right {
		if !left[key] {
			union++
		}
	}
	intersection := 0
	for key := range right {
		if left[key] {
			intersection++
		}
	}
	denom := union
	if denom < 1 {
		denom = 1
	}
	return float64(intersection) / float64(denom)
}

// RestatingPlans 标题复述已提供或已否定标题的计划（:1191-1206）。
func RestatingPlans(plans []map[string]any, offered []string) []struct {
	Plan   map[string]any
	Detail map[string]any
} {
	var restating []struct {
		Plan   map[string]any
		Detail map[string]any
	}
	for _, plan := range plans {
		bestSimilarity := 0.0
		bestOffered := ""
		for _, title := range offered {
			similarity := bigramJaccard(pyStr(plan["title"]), title)
			if similarity > bestSimilarity || bestOffered == "" {
				bestSimilarity = similarity
				bestOffered = title
			}
		}
		if bestSimilarity >= RestatedOfferedSimilarity {
			restating = append(restating, struct {
				Plan   map[string]any
				Detail map[string]any
			}{plan, map[string]any{
				"candidate_id":        pyStr(plan["candidate_id"]),
				"interest_summary_id": pyStr(plan["interest_summary_id"]),
				"direction":           pyStr(plan["direction"]),
				"title":               pyStr(plan["title"]),
				"offered_title":       bestOffered,
				"similarity":          bestSimilarity,
			}})
		}
	}
	return restating
}

// ProjectContinuationPlan 在其精确来源请求上落一个项目续聊卡计划
// （topic_pipeline.py:1208-1320）。
func ProjectContinuationPlan(candidateID string, interestSummaryID, intent, title string,
	source *V6SourceMessage, snapshot *V6ContextSnapshot, interestSummary string) map[string]any {
	locator := map[string]any{
		"project_id": snapshot.ProjectID,
		"session_id": source.SessionID,
		"ordinal":    source.Ordinal,
		"event_id":   source.EventID,
	}
	return map[string]any{
		"candidate_id": candidateID, "interest_summary_id": interestSummaryID,
		"intent": intent, "title": title,
		"source_event_id": source.EventID, "prompt": title,
		"category": "project_continuation", "context_scope": snapshot.ContextScope,
		"source_starter_id": nil, "identity_anchor": source.Content,
		"interest_summary": interestSummary, "persona_context": source.Content,
		"primary_query": title, "freshness_class": "evergreen_corpus",
		"source_memory_unit_ids":  []any{},
		"source_locators":         []any{[]any{source.SessionID, source.Ordinal}},
		"project_source_locators": []any{locator},
		"project_evidence": map[string]any{
			"candidate_id": candidateID, "fetch_status": "success",
			"source_url": fmt.Sprintf("project://%v/%s/%d",
				snapshot.ProjectID, source.SessionID, source.Ordinal),
			"source_kind": "stable", "publisher": "local_project_history",
			"document_revision_id": fmt.Sprintf("project-history:%s", source.EventID),
			"retrieved_at":         isoFromUnix(source.ObservedAt),
			"snapshot_date":        localDateString(source.ObservedAt),
			"snapshot_version":     fmt.Sprintf("project-history:%s", snapshot.InputSnapshotID),
			"source_gate_mode":     "project_exact_locator",
			"content_boundary": map[string]any{
				"start": 0, "end": len([]rune(source.Content)), "text": source.Content,
			},
			"exact_spans": []any{map[string]any{
				"candidate_id":         candidateID,
				"document_revision_id": fmt.Sprintf("project-history:%s", source.EventID),
				"start":                0, "end": len([]rune(source.Content)), "exact_text": source.Content,
			}},
			"host_receipt": map[string]any{
				"receipt_version":      ProjectLocatorReceiptVersion,
				"lane":                 "host_project_exact_locator",
				"input_snapshot_id":    snapshot.InputSnapshotID,
				"project_id":           snapshot.ProjectID,
				"source_locator":       locator,
				"document_revision_id": fmt.Sprintf("project-history:%s", source.EventID),
			},
		},
	}
}

// CarriedPlan 以卡的过期时间与来源快照重新发出携带卡的计划（:1322-1327）。
func CarriedPlan(plan, card map[string]any) map[string]any {
	merged := map[string]any{}
	for key, value := range plan {
		merged[key] = value
	}
	merged["retained_from_snapshot_id"] = pyStr(card["generation_input_snapshot_id"])
	merged["retained_expire_time"] = toFloatOr(card["expire_time"], 0)
	return merged
}

// CandidateSlots 每个兴趣一个槽，生成器至多填一次（:1329-1338）。
func CandidateSlots(interests []RankedInterest) []map[string]any {
	slots := make([]map[string]any, 0, len(interests))
	for _, item := range interests {
		slots = append(slots, map[string]any{
			"slot_id":             fmt.Sprintf("candidate-%s", newUUIDHex()),
			"interest_summary_id": item.InterestSummaryID,
		})
	}
	return slots
}

// ProfileInterestPlan 把一个释放的兴趣塑形为待锚定候选（:1340-1390）。
func ProfileInterestPlan(candidateID, interestSummaryID, interestSummary, intent,
	title, prompt, direction string, sourceMemoryUnitIDs []string,
	contextScope, inputSnapshotID string, now float64) map[string]any {
	unitIDs := make([]any, 0, len(sourceMemoryUnitIDs))
	for _, id := range sourceMemoryUnitIDs {
		unitIDs = append(unitIDs, id)
	}
	return map[string]any{
		"candidate_id": candidateID, "interest_summary_id": interestSummaryID,
		"intent": intent, "direction": direction, "title": title, "prompt": prompt,
		"category": ProfileInterestCategory, "context_scope": contextScope,
		"source_starter_id": nil, "identity_anchor": interestSummary,
		"interest_summary": interestSummary, "persona_context": interestSummary,
		"primary_query": title, "freshness_class": "evergreen_corpus",
		"source_memory_unit_ids": unitIDs, "source_locators": []any{},
		"project_source_locators": []any{},
		"profile_evidence": profileInterestEvidenceBundle(candidateID,
			interestSummaryID, interestSummary, inputSnapshotID, now),
	}
}

// profileInterestEvidenceBundle 将生成的卡绑定到它所写的释放兴趣
// （topic_pipeline.py:1115-1159）。
func profileInterestEvidenceBundle(candidateID, interestSummaryID, interestSummary,
	inputSnapshotID string, now float64) map[string]any {
	documentRevisionID := fmt.Sprintf("profile-interest:%s", interestSummaryID)
	contentRunes := []rune(interestSummary)
	return map[string]any{
		"candidate_id": candidateID, "fetch_status": "success",
		"source_url":  fmt.Sprintf("profile://%s", interestSummaryID),
		"source_kind": "stable", "publisher": "local_profile_interest",
		"document_revision_id": documentRevisionID,
		"retrieved_at":         isoFromUnix(now),
		"snapshot_date":        localDateString(now),
		"snapshot_version":     fmt.Sprintf("profile-interest:%s", inputSnapshotID),
		"source_gate_mode":     "profile_interest_exact_summary",
		"content_boundary": map[string]any{
			"start": 0, "end": len(contentRunes), "text": interestSummary,
		},
		"exact_spans": []any{map[string]any{
			"candidate_id": candidateID, "document_revision_id": documentRevisionID,
			"start": 0, "end": len(contentRunes), "exact_text": interestSummary,
		}},
		"host_receipt": map[string]any{
			"receipt_version":      ProfileInterestReceiptVersion,
			"lane":                 "host_profile_interest_locator",
			"input_snapshot_id":    inputSnapshotID,
			"interest_summary_id":  interestSummaryID,
			"document_revision_id": documentRevisionID,
		},
	}
}

// HotListTitle 热榜卡只问该词条说的是什么（:1392-1395）。
func HotListTitle(word string) string {
	return fmt.Sprintf("热搜上的「%s」是怎么回事？", word)
}

// HotListText 词条按榜单所示：词条，然后百度的摘要（如有）（:1397-1400）。
func HotListText(entry *HotListEntry) string {
	if entry.Summary == nil {
		return entry.Word
	}
	return fmt.Sprintf("%s\n%s", entry.Word, pyStr(entry.Summary))
}

// HotNewsPlan 匹配词条作为资讯流候选（:1402-1443）。
// closest 词条不属于任何兴趣：事件本身站在兴趣的位置。
func HotNewsPlan(candidateIDs []string, entry *HotListEntry, pull *HotListPull,
	interest *LongTermProfileInterestInput, match, newsQuery string,
	direction any, contextScope string) map[string]any {
	context := HotListText(entry)
	if interest != nil {
		context = interest.SummaryText
	}
	intent := fmt.Sprintf("弄清热搜「%s」说的是什么事", entry.Word)
	if interest != nil {
		intent += "，以及它对用户关心的事有什么影响"
	}
	plan := map[string]any{
		"candidate_id": candidateIDs[0],
		"interest_summary_id": func() any {
			if interest != nil {
				return interest.InterestSummaryID
			}
			return nil
		}(),
		"identity_anchor": entry.Word, "interest_summary": context,
		"intent": intent, "title": entry.Word,
		"primary_query":   NormalizeNewsQuery(newsQuery),
		"freshness_class": NewsFreshnessClass, "source_channel": "news_feed",
		"prompt": entry.Word, "category": "profile_recent_progress",
		"context_scope": contextScope, "source_starter_id": nil,
		"persona_context": context, "source_locators": []any{},
		"project_source_locators": []any{}, "news_match": match,
		"hot_list_entry": map[string]any{
			"candidate_id": candidateIDs[1], "entry": entry, "pull": pull,
		},
	}
	if interest != nil {
		unitIDs := make([]any, 0, len(interest.SourceProfileUnitIDs))
		for _, id := range interest.SourceProfileUnitIDs {
			unitIDs = append(unitIDs, id)
		}
		plan["source_memory_unit_ids"] = unitIDs
	} else {
		plan["source_memory_unit_ids"] = []any{}
	}
	if direction != nil {
		plan["direction"] = direction
	}
	return plan
}

// HotListPlan 无报道的新闻候选：榜单词条上的问题，词条即其证据
// （topic_pipeline.py:1445-1505）。
func HotListPlan(plan map[string]any, inputSnapshotID string) map[string]any {
	source := mapAnyOf(plan["hot_list_entry"])
	entry, _ := source["entry"].(*HotListEntry)
	pull, _ := source["pull"].(*HotListPull)
	candidateID := pyStr(source["candidate_id"])
	title := HotListTitle(entry.Word)
	text := HotListText(entry)
	documentRevisionID := fmt.Sprintf("hot-list:%s:%s", pull.ReceiptID, entry.EntryID)
	publisher := urlHostname(mustParseURL(pull.ListURL))
	publisher = strings.TrimPrefix(publisher, "www.")
	textRunes := []rune(text)
	merged := map[string]any{}
	for key, value := range plan {
		merged[key] = value
	}
	merged["candidate_id"] = candidateID
	merged["title"] = title
	merged["prompt"] = title
	var listedAt any
	if entry.ListedAt != nil {
		listedAt = isoFromUnix(toFloatOr(entry.ListedAt, 0))
	}
	merged["hot_list_evidence"] = map[string]any{
		"candidate_id": candidateID, "fetch_status": "success",
		"source_url":  SearchPageURL(entry.Word),
		"search_url":  SearchPageURL(pyStr(plan["primary_query"])),
		"source_kind": HotListSourceKind, "publisher": publisher,
		"document_revision_id": documentRevisionID,
		"retrieved_at":         isoFromUnix(pull.PulledAt),
		"snapshot_date":        localDateString(pull.PulledAt),
		"snapshot_version":     fmt.Sprintf("hot-list:%s", pull.ReceiptID),
		"source_gate_mode":     "hot_list_entry",
		"listed_at":            listedAt,
		"content_boundary": map[string]any{
			"start": 0, "end": len(textRunes), "text": text,
		},
		"exact_spans": []any{map[string]any{
			"candidate_id": candidateID, "document_revision_id": documentRevisionID,
			"start": 0, "end": len(textRunes), "exact_text": text,
		}},
		"host_receipt": map[string]any{
			"receipt_version":      HotListEntryReceiptVersion,
			"lane":                 "host_hot_list_entry",
			"input_snapshot_id":    inputSnapshotID,
			"pull_receipt_id":      pull.ReceiptID,
			"provider_id":          entry.ProviderID,
			"list_url":             pull.ListURL,
			"hot_entry_id":         entry.EntryID,
			"hot_list_rank":        entry.Rank,
			"document_revision_id": documentRevisionID,
		},
	}
	return merged
}

// NewsSlotRecord 池对新闻卡记录什么（:1507-1526）。
func NewsSlotRecord(plans []map[string]any, absentReason string) map[string]any {
	var matched map[string]any
	for _, plan := range plans {
		if pyStr(plan["news_match"]) != "" {
			matched = plan
			break
		}
	}
	var source map[string]any
	if matched != nil {
		source = mapAnyOf(matched["hot_list_entry"])
	}
	var candidateID, cardForm, match, hotEntryID, pullReceiptID any
	if matched != nil {
		candidateID = pyStr(matched["candidate_id"])
		cardForm = pyStr(mapAnyOf(matched["evidence_bundle"])["source_kind"])
		match = pyStr(matched["news_match"])
	}
	if source != nil {
		entry, _ := source["entry"].(*HotListEntry)
		pull, _ := source["pull"].(*HotListPull)
		if entry != nil {
			hotEntryID = entry.EntryID
		}
		if pull != nil {
			pullReceiptID = pull.ReceiptID
		}
	}
	return map[string]any{
		"candidate_id": candidateID, "card_form": cardForm, "match": match,
		"hot_entry_id": hotEntryID, "hot_list_pull_receipt_id": pullReceiptID,
		"absent_reason": nilIfEmpty(absentReason, absentReason != ""),
	}
}

// NewsSourceAssessment 资讯流文章按 lane 接受，以发布主机为键（:1528-1533）。
func NewsSourceAssessment(targetURL string) SourceAssessment {
	hostname := strings.TrimPrefix(urlHostname(mustParseURL(targetURL)), "www.")
	kind := "unknown"
	if hostname != "" {
		kind = NewsSourceKind
	}
	return SourceAssessment{Kind: kind, Publisher: hostname}
}

// DefaultV6SourceClassifier 分类一手论文、官方、报道与稳定来源 lane
// （topic_pipeline.py:1535-1581）。
func DefaultV6SourceClassifier(targetURL, _ string) SourceAssessment {
	parsed := mustParseURL(targetURL)
	hostname := strings.TrimPrefix(urlHostname(parsed), "www.")
	pathSegments := []string{}
	for _, segment := range strings.Split(parsed.Path, "/") {
		if segment != "" {
			pathSegments = append(pathSegments, segment)
		}
	}
	matches := func(hosts map[string]bool) bool {
		for host := range hosts {
			if hostname == host || strings.HasSuffix(hostname, "."+host) {
				return true
			}
		}
		return false
	}
	githubOwner := ""
	if hostname == "github.com" && len(pathSegments) >= 2 &&
		!githubNonRepositoryRoots[strings.ToLower(pathSegments[0])] {
		githubOwner = strings.ToLower(pathSegments[0])
	}
	var kind string
	publisher := hostname
	switch {
	case githubOwner != "":
		kind = "original"
		publisher = fmt.Sprintf("github.com/%s", githubOwner)
	case hostname == "pypi.org" && len(pathSegments) >= 2 &&
		strings.ToLower(pathSegments[0]) == "project":
		kind = "stable"
	case matches(originalSourceHosts) ||
		(matches(originalArticlePathHosts) && len(pathSegments) >= 1 && pathSegments[0] == "articles"):
		kind = "original"
	case matches(authoritativeSourceHosts) ||
		strings.HasSuffix(hostname, ".gov") || strings.HasSuffix(hostname, ".gov.cn") ||
		strings.HasSuffix(hostname, ".edu") || strings.HasSuffix(hostname, ".edu.cn") ||
		strings.HasSuffix(hostname, ".ac.cn"):
		kind = "authoritative"
	case matches(reliableReportHosts):
		kind = "reliable_report"
	case matches(stableSourceHosts):
		kind = "stable"
	default:
		kind = "unknown"
	}
	return SourceAssessment{Kind: kind, Publisher: publisher}
}

// NormalizeNewsQuery 去掉版本号与日期：标题搜索索引的是主题而非版本
// （topic_news.py:64-72 ⤴️31c 归位）。
var versionOrDateToken = regexp.MustCompile(`(?i)^(?:v(?:er(?:sion)?)?\.?\s*)?\d+(?:\.\d+)+$` +
	`|^v?\d+(?:\.\d+)*[a-z]?$` +
	`|^(?:19|20)\d{2}(?:年|[-/]\d{1,2}(?:[-/]\d{1,2})?)?$`)

func NormalizeNewsQuery(query string) string {
	var tokens []string
	for _, token := range strings.Fields(query) {
		if !versionOrDateToken.MatchString(token) {
			tokens = append(tokens, token)
		}
	}
	normalized := strings.Join(tokens, " ")
	normalized = strings.TrimSpace(normalized)
	if normalized == "" {
		return strings.TrimSpace(query)
	}
	return normalized
}

// SearchPageURL 一个 query 的 Bing News 结果页（topic_news.py:82-84 ⤴️31c 归位）。
func SearchPageURL(query string) string {
	return fmt.Sprintf("%s?q=%s", newsFeedURL, url.QueryEscape(query))
}

// isoFromUnix UNIX 秒 → UTC ISO 格式。
func isoFromUnix(seconds float64) string {
	return time.Unix(int64(seconds), 0).UTC().Format("2006-01-02T15:04:05Z07:00")
}

// mustParseURL 解析 URL（错误回空 URL）。
func mustParseURL(target string) *url.URL {
	parsed, err := url.Parse(target)
	if err != nil {
		return &url.URL{}
	}
	return parsed
}

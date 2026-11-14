package recommendation

// topic_pipeline_d8.go 一比一对齐上游 topic_pipeline.py:7098-7712 的
// _project_candidate_plans/_validate_project_continuations/_candidate_evidence
// 与 :2700-2708 的 _optional_lane。
// 已知偏差（§5 并发映射）：asyncio.ensure_future/gather → goroutine+channel；
// fetch_tasks 共享表以互斥锁保护。
// 已知偏差 ⤴️31c：web 搜索双形态（OpenJiuwenNativeSearchResult 对象 vs
// (engine, rows) 元组）——Go 侧 WebSearchFn 一律返回元组形态；原生搜索主体
// 落地后按规则 8 回填对象分支。

import (
	"context"
	"fmt"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 常量 ────────────────────────────

// V6FetchWaitSeconds 抓取等待上限（topic_pipeline.py:219）。
const V6FetchWaitSeconds = 8.0

// FetchAuditProviderID 抓取审计厂商 ID（topic_pipeline.py:224）。
const FetchAuditProviderID = "openjiuwen-web-fetch"

// InjectedSearchAuditProviderIDReplaced 注入搜索小票版本（:7523）。
const InjectedSearchReceiptVersion = "topic-starter-v6-injected-search-v1"

// privateQueryRE 私密查询特征（:7380-7382）。
var privateQueryRE = regexp.MustCompile(
	`[\w.+-]+@[\w.-]+\.[A-Za-z]{2,}` +
		`|(?:/Users/|/home/|/private/|[A-Za-z]:\\)[^\s]+` +
		`|\b1[3-9]\d{9}\b`)

// ──────────────────────────── 结构体 ────────────────────────────

// CandidateEvidence 一个候选的搜索、选行与抓取记录
// （topic_pipeline.py:7350-7712 _CandidateEvidence）。
type CandidateEvidence struct {
	// SearchAuditRef 搜索审计引用
	SearchAuditRef map[string]string
	// Evidence 证据记录
	Evidence map[string]any
	// SearchRejection 搜索拒绝（阶段, 原因），可空
	SearchRejection *[2]string
	// SearchReceipt 搜索小票（搜索成功且选出行后非空）
	SearchReceipt map[string]any
	// FetchedRows 成功抓取的 (行, 页, 证据) 三元组
	FetchedRows [][3]any
	// FetchAuditRefs 抓取审计引用
	FetchAuditRefs []map[string]string
	// FetchFailure 抓取失败原因
	FetchFailure string
}

// FetchTasks 共享抓取任务表：(lane, url) → (审计引用, 结果)。
type FetchTasks struct {
	mu    sync.Mutex
	tasks map[string]*fetchTaskEntry
}

type fetchTaskEntry struct {
	ref    map[string]string
	done   chan fetchOutcome
	cancel context.CancelFunc
}

type fetchOutcome struct {
	page map[string]any
	err  error
}

// NewFetchTasks 构造共享抓取任务表。
func NewFetchTasks() *FetchTasks {
	return &FetchTasks{tasks: map[string]*fetchTaskEntry{}}
}

// ──────────────────────────── 导出函数 ────────────────────────────

// OptionalLaneOf 网页候选来自的可选 lane（topic_pipeline.py:2700-2708）。
func OptionalLaneOf(plan map[string]any) string {
	if pyStr(plan["category"]) == "project_domain_progress" {
		return PipelineProjectDomainLane
	}
	if pyStr(plan["source_channel"]) == "news_feed" {
		return PipelineHotNewsLane
	}
	return PipelineGlobalRecentLane
}

// ProjectCandidatePlans 项目候选计划：续聊槽与领域槽并行生成
// （topic_pipeline.py:7098-7267 _project_candidate_plans）。
func (p *TopicPipeline) ProjectCandidatePlans(snapshot *V6ContextSnapshot,
	rid *RIDResult, summaries []*SessionSummaryArtifact,
	invokeStage TopicStageInvoker, offeredTitles map[string][]string,
	dismissedTitles []string, offeredQuestions []map[string]any,
	carryingCards bool, lanes *OptionalLanes) ([]map[string]any, error) {
	if lanes == nil {
		lanes = NewOptionalLanes()
	}
	summariesByID := InterestSummaryTextByID(snapshot, summaries)
	type rankedEntry struct {
		id      string
		summary string
		rank    int
	}
	var ranked []rankedEntry
	for _, item := range rid.TriggerInterests() {
		rank := item.Rank
		if rank == 0 {
			rank = item.Rank
		}
		ranked = append(ranked, rankedEntry{
			id: item.InterestSummaryID, summary: summariesByID[item.InterestSummaryID],
			rank: rank,
		})
	}
	if len(ranked) == 0 {
		return nil, &TerminalGenerationRejection{Reason: "insufficient_project_interests"}
	}
	sourceLocatorsByInterest := map[string][]*SessionSourceLocator{}
	for _, interest := range rid.TriggerInterests() {
		sourceLocatorsByInterest[interest.InterestSummaryID] = interest.SourceLocators
	}
	selectedLocators := map[string]bool{}
	for _, locators := range sourceLocatorsByInterest {
		for _, locator := range locators {
			selectedLocators[fmt.Sprintf("%s\x00%d\x00%s", locator.SessionID,
				locator.Ordinal, locator.EventID)] = true
		}
	}
	eventByID := map[string]*V6SourceMessage{}
	var eventOrder []*V6SourceMessage
	for _, item := range snapshot.Messages {
		if !item.Natural() {
			continue
		}
		key := fmt.Sprintf("%s\x00%d\x00%s", item.SessionID, item.Ordinal, item.EventID)
		if !selectedLocators[key] {
			continue
		}
		eventByID[item.EventID] = item
		eventOrder = append(eventOrder, item)
	}
	continuationCandidateSlots := make([]map[string]any, 0, 3)
	for index := 0; index < 3; index++ {
		continuationCandidateSlots = append(continuationCandidateSlots,
			map[string]any{"slot_id": fmt.Sprintf("candidate-%s", newUUIDHex())})
	}
	if offeredQuestions == nil {
		offeredQuestions = p.OfferedQuestions(snapshot, false)
	}
	rankedPayloads := make([]any, 0, len(ranked))
	for _, item := range ranked {
		locatorPayloads := make([]any, 0)
		for _, locator := range sourceLocatorsByInterest[item.id] {
			locatorPayloads = append(locatorPayloads, locator.AsPayload())
		}
		rankedPayloads = append(rankedPayloads, map[string]any{
			"interest_summary_id": item.id, "interest_summary": item.summary,
			"rank": item.rank, "source_locators": locatorPayloads,
		})
	}
	var alreadyOffered []any
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
	for _, title := range dismissedTitles {
		dismissedQuestions = append(dismissedQuestions, title)
	}
	history := make([]any, 0, len(eventOrder))
	for _, item := range eventOrder {
		history = append(history, map[string]any{
			"source_event_id": item.EventID, "session_id": item.SessionID,
			"ordinal": item.Ordinal, "content": item.Content,
		})
	}
	continuationRequestID := "v6-project-continuation-" + newUUIDHex()
	continuationPayload := map[string]any{
		"input_snapshot_id":   snapshot.InputSnapshotID,
		"project_id":          snapshot.ProjectID,
		"ranked_interests":    rankedPayloads,
		"discussion_coverage": DiscussionCoverage(summaries, nil),
		"history":             history,
		"already_offered":     alreadyOffered,
		"dismissed_questions": dismissedQuestions,
		"previously_offered":  offeredQuestions,
		"candidate_slots":     continuationCandidateSlots,
		"request_id":          continuationRequestID,
	}
	domainCandidateSlots := make([]map[string]any, 0)
	slotCount := len(ranked)
	if slotCount < 3 {
		slotCount = 3
	}
	var domainSlotInput []RankedInterest
	for index := 0; index < slotCount; index++ {
		domainCandidateSlots = append(domainCandidateSlots, map[string]any{
			"slot_id":             fmt.Sprintf("candidate-%s", newUUIDHex()),
			"interest_summary_id": ranked[index%len(ranked)].id,
		})
		domainSlotInput = append(domainSlotInput, RankedInterest{
			InterestSummaryID: ranked[index%len(ranked)].id,
			Rank:              ranked[index%len(ranked)].rank})
	}
	domainRankedPayloads := make([]any, 0, len(ranked))
	for _, item := range ranked {
		domainRankedPayloads = append(domainRankedPayloads, map[string]any{
			"interest_summary_id": item.id, "interest_summary": item.summary,
			"rank": item.rank,
		})
	}
	domainRequestID := "v6-project-domain-" + newUUIDHex()
	domainPayload := map[string]any{
		"input_snapshot_id":   snapshot.InputSnapshotID,
		"local_date":          localDateString(p.Now()),
		"ranked_interests":    domainRankedPayloads,
		"required_category":   "project_domain_progress",
		"previously_offered":  offeredQuestions,
		"discussion_coverage": DiscussionCoverage(summaries, nil),
		"candidate_slots":     domainCandidateSlots,
		"request_id":          domainRequestID,
	}

	type invocationOutcome struct {
		response any
		err      error
	}
	domainDone := make(chan invocationOutcome, 1)
	domainCtx, domainCancel := context.WithCancel(context.Background())
	go func() {
		defer close(domainDone)
		response, err := invokeStage.Invoke(domainCtx,
			"v6_project_domain_candidate_generator", pipelineProjectDomainPrompt,
			compactJSON(domainPayload))
		domainDone <- invocationOutcome{response: response, err: err}
	}()

	continuationResponse, continuationErr := invokeStage.Invoke(context.Background(),
		"v6_project_continuation_generator", pipelineProjectContinuationPrompt,
		compactJSON(continuationPayload))
	var continuation []map[string]any
	var generationErr error
	if continuationErr != nil {
		generationErr = continuationErr
	} else {
		RecordProviderValidationTopic(invokeStage, continuationResponse, "business_validated")
		continuation, generationErr = ValidateProjectContinuations(
			continuationResponse, carryingCards, snapshot,
			sourceLocatorsByInterest, summariesByID, eventByID,
			continuationCandidateSlots)
		if generationErr != nil {
			RecordProviderValidationTopic(invokeStage, continuationResponse, "business_validation_failed")
		}
	}
	lanes.MandatoryReady()
	var domain []map[string]any
	if generationErr == nil {
		outcome := <-domainDone
		if outcome.err == nil {
			RecordProviderValidationTopic(invokeStage, outcome.response, "business_validated")
			var domainErr error
			domain, domainErr = ValidateRecentCandidates(outcome.response,
				domainSlotInput, map[string]string{}, map[string][]string{},
				snapshot.ContextScope, "project_domain_progress", 0,
				domainCandidateSlots)
			if domainErr != nil {
				RecordProviderValidationTopic(invokeStage, outcome.response, "business_validation_failed")
			}
		} else {
			logger.Warn(logger.ComponentAgentCore).
				Msg("Topic Starter v6 optional project domain generation failed; retaining project continuations")
		}
	}
	domainCancel()
	for _, plan := range domain {
		plan["previously_offered"] = offeredQuestions
	}
	generated := append(append([]map[string]any(nil), continuation...), domain...)
	// 复述本项目已有标题或已否定问题的计划离开。
	var offered []string
	for _, titles := range offeredTitles {
		offered = append(offered, titles...)
	}
	for _, question := range offeredQuestions {
		offered = append(offered, pyStr(question["title"]))
	}
	offered = append(offered, dismissedTitles...)
	restating := RestatingPlans(generated, offered)
	if len(restating) > 0 {
		records := make([]any, 0, len(restating))
		for _, item := range restating {
			records = append(records, item.Detail)
		}
		logger.Info(logger.ComponentAgentCore).
			Str("records", pyWitnessJSON(records)).
			Msg("Topic Starter v6 left out project plans restating offered titles")
	}
	leftOut := map[string]bool{}
	for _, item := range restating {
		leftOut[fmt.Sprintf("%p", item.Plan)] = true
	}
	var result []map[string]any
	for _, plan := range generated {
		if !leftOut[fmt.Sprintf("%p", plan)] {
			result = append(result, plan)
		}
	}
	return result, generationErr
}

// ValidateProjectContinuations 项目续聊候选校验
// （topic_pipeline.py:7269-7348）。每槽恰一次决策；只有写下的槽成为候选。
func ValidateProjectContinuations(response any, allowEmpty bool,
	snapshot *V6ContextSnapshot,
	sourceLocatorsByInterest map[string][]*SessionSourceLocator,
	summariesByID map[string]string, eventByID map[string]*V6SourceMessage,
	candidateSlots []map[string]any) ([]map[string]any, error) {
	const failure = "project_continuation_contract_failed"
	expectedSlots := map[string]bool{}
	for _, slot := range candidateSlots {
		expectedSlots[pyStr(slot["slot_id"])] = true
	}
	if len(expectedSlots) != 3 || expectedSlots[""] {
		return nil, &TerminalGenerationRejection{Reason: failure}
	}
	required := []string{"slot_id", "interest_summary_id", "blocked_by",
		"new_angle", "nearest_covered", "nearest_offered", "nearest_dismissed",
		"decision", "intent", "title", "source_event_id"}
	root, isMap := response.(map[string]any)
	if !isMap || len(root) != 1 {
		return nil, &TerminalGenerationRejection{Reason: failure}
	}
	rawCandidates, isList := root["candidates"].([]any)
	if !isList {
		return nil, &TerminalGenerationRejection{Reason: failure}
	}
	slotIDs := []string{}
	for _, raw := range rawCandidates {
		entry, isMap := raw.(map[string]any)
		if !isMap || len(entry) != len(required) {
			return nil, &TerminalGenerationRejection{Reason: failure}
		}
		for _, field := range required {
			if _, present := entry[field]; !present {
				return nil, &TerminalGenerationRejection{Reason: failure}
			}
		}
		if pyStr(entry["decision"]) != "write" && pyStr(entry["decision"]) != "skip" {
			return nil, &TerminalGenerationRejection{Reason: failure}
		}
		slotIDs = append(slotIDs, strings.TrimSpace(pyStr(entry["slot_id"])))
	}
	expectedSorted := make([]string, 0, len(expectedSlots))
	for slot := range expectedSlots {
		expectedSorted = append(expectedSorted, slot)
	}
	sort.Strings(expectedSorted)
	actualSorted := append([]string(nil), slotIDs...)
	sort.Strings(actualSorted)
	if !stringSlicesEqual(actualSorted, expectedSorted) {
		return nil, &TerminalGenerationRejection{Reason: failure}
	}
	var written []map[string]any
	for _, raw := range rawCandidates {
		if raw.(map[string]any)["decision"] == "write" {
			written = append(written, raw.(map[string]any))
		}
	}
	if len(written) == 0 {
		if allowEmpty {
			return nil, nil
		}
		return nil, &TerminalGenerationRejection{Reason: "insufficient_active_project_actions"}
	}
	var plans []map[string]any
	for _, raw := range written {
		slotID := strings.TrimSpace(pyStr(raw["slot_id"]))
		eventID := strings.TrimSpace(pyStr(raw["source_event_id"]))
		interestID := strings.TrimSpace(pyStr(raw["interest_summary_id"]))
		title := strings.TrimSpace(pyStr(raw["title"]))
		source := eventByID[eventID]
		rejected := source == nil ||
			pyStrOr(source.ProjectID, "") != pyStrOr(snapshot.ProjectID, "")
		if !rejected {
			locatorHeld := false
			for _, locator := range sourceLocatorsByInterest[interestID] {
				if locator.SessionID == source.SessionID &&
					locator.Ordinal == source.Ordinal &&
					locator.EventID == source.EventID {
					locatorHeld = true
					break
				}
			}
			rejected = !locatorHeld || title == "" ||
				strings.TrimSpace(pyStr(raw["intent"])) == ""
		}
		if rejected {
			return nil, &TerminalGenerationRejection{Reason: failure}
		}
		plans = append(plans, ProjectContinuationPlan(slotID, interestID,
			pyStr(raw["intent"]), title, source, snapshot, summariesByID[interestID]))
	}
	return plans, nil
}

// CandidateEvidenceFor 搜索单个网页候选、选行并抓取
// （topic_pipeline.py:7350-7712 _candidate_evidence）。候选自身的抓取在其
// 搜索答复后开始；页面由 _freeze_documents 按计划顺序与其他候选一起冻结。
func (p *TopicPipeline) CandidateEvidenceFor(plan map[string]any,
	snapshot *V6ContextSnapshot, externalAudit *ExternalInvocationAudit,
	searchProvider WebSearchFn, lanes *OptionalLanes, fetchTasks *FetchTasks) *CandidateEvidence {
	candidateID := pyStr(plan["candidate_id"])
	lane := OptionalLaneOf(plan)
	activeSearch := searchProvider
	if activeSearch == nil {
		activeSearch = p.searchOverride
	}
	configuredProviderID := INJECTEDSearchAuditProviderID

	privateQuery := func(query string) bool {
		if privateQueryRE.MatchString(query) {
			return true
		}
		for _, message := range snapshot.Messages {
			trimmed := strings.TrimSpace(message.Content)
			if len([]rune(trimmed)) >= 20 && strings.Contains(query, trimmed) {
				return true
			}
		}
		return false
	}
	searchAuditRef := map[string]string{}
	evidence := map[string]any{
		"candidate_id": candidateID,
		"search":       nil,
		"fetches":      []any{},
		"extractor":    nil,
	}

	// 搜索阶段（同步执行；Python await search()）
	resultEngine, resultRows, searchErr := func() (string, []map[string]string, error) {
		operationID := fmt.Sprintf("external-operation-%s", newUUIDHex())
		requestID := fmt.Sprintf("external-request-%s", newUUIDHex())
		searchAuditRef = map[string]string{
			"operation": "search", "operation_id": operationID,
			"request_id": requestID,
		}
		started := time.Now()
		query := strings.TrimSpace(pyStr(plan["primary_query"]))
		evidence["search"] = map[string]any{
			"operation_id": operationID, "request_id": requestID,
			"requested_query": query, "provider_receipt": nil,
		}
		newsLane := pyStr(plan["source_channel"]) == "news_feed"
		var laneSearch WebSearchFn
		if newsLane {
			laneSearch = p.newsFeed
		} else {
			laneSearch = activeSearch
		}
		laneProviderID := NewsFeedProviderID
		if !newsLane {
			laneProviderID = configuredProviderID
		}
		externalAudit.RecordAttempt("search", operationID, requestID,
			"live_retrieval_search", laneProviderID, candidateID)
		if privateQuery(query) {
			err := &TerminalGenerationRejection{Reason: "search_query_contains_private_material"}
			externalAudit.RecordFailure("search", operationID, laneProviderID,
				requestID, err.Reason,
				float64(time.Since(started).Milliseconds()))
			return "", nil, err
		}
		searchCtx, cancel := context.WithTimeout(context.Background(),
			SearchTimeoutSeconds*time.Second)
		defer cancel()
		withinResult, withinErr := lanes.Within(searchCtx, []string{lane},
			func(ctx context.Context) (any, error) {
				engine, rows, err := laneSearch(ctx, query, MaxSearchResults,
					SearchTimeoutSeconds)
				return [2]any{engine, rows}, err
			})
		rawEngine, rawRows, resultErr := laneSearch(context.Background(), query,
			MaxSearchResults, SearchTimeoutSeconds)
		if withinErr != nil {
			resultErr = withinErr
		} else if pair, ok := withinResult.([2]any); ok {
			rawEngine, _ = pair[0].(string)
			rawRows, _ = pair[1].([]map[string]string)
		}
		if resultErr != nil {
			externalAudit.RecordFailure("search", operationID, laneProviderID,
				requestID, "provider_invocation_failed",
				float64(time.Since(started).Milliseconds()))
			return "", nil, resultErr
		}
		externalAudit.RecordSuccess("search", operationID, laneProviderID,
			requestID, fmt.Sprintf("external-result-%s", newUUIDHex()),
			float64(time.Since(started).Milliseconds()))
		return rawEngine, rawRows, nil
	}()
	if searchErr != nil {
		reason := "provider_invocation_failed"
		if rejection, isRejection := searchErr.(*TerminalGenerationRejection); isRejection {
			reason = rejection.Reason
		} else if contract, isContract := searchErr.(*OpenJiuwenNativeSearchContractError); isContract {
			reason = contract.Reason
		}
		return &CandidateEvidence{
			SearchAuditRef:  searchAuditRef,
			Evidence:        evidence,
			SearchRejection: &[2]string{"search", reason},
		}
	}
	engine := resultEngine
	bounded := resultRows
	if len(bounded) > MaxSearchResults {
		bounded = bounded[:MaxSearchResults]
	}
	providerReceipt := map[string]any{
		"receipt_version": InjectedSearchReceiptVersion,
		"provider_id":     configuredProviderID,
		"action": map[string]any{
			"type": "search", "query": pyStr(plan["primary_query"]),
		},
	}
	newsLane := pyStr(plan["source_channel"]) == "news_feed"
	var knownRows, unknownRows []map[string]any
	for _, row := range bounded {
		if row == nil {
			continue
		}
		rawURL := strings.TrimSpace(row["url"])
		if !strings.HasPrefix(rawURL, "https://") &&
			!strings.HasPrefix(rawURL, "http://") {
			continue
		}
		selectedRow := map[string]any{
			"url": rawURL, "title": row["title"],
			"structured_search_date": "", "search_provider_id": "",
			"search_receipt_id":        "",
			"is_news_publication_date": false,
		}
		if !newsLane && p.classifySource != nil &&
			p.classifySource(rawURL, "").Kind == "unknown" {
			unknownRows = append(unknownRows, selectedRow)
		} else {
			knownRows = append(knownRows, selectedRow)
		}
	}
	rankedRows := append(append([]map[string]any(nil), knownRows...), unknownRows...)
	// 每个显式版本与表示在各自身份前保留一页；新闻源重复镜像主机，
	// 故新闻按主机先保留一篇报道。
	var distinctRows, duplicateRows []map[string]any
	seenPages := map[string]bool{}
	for _, row := range rankedRows {
		identity, _, _ := VersionedDocumentIdentity(row["url"].(string), "")
		parsed, _ := url.Parse(row["url"].(string))
		representation := ""
		pathParts := strings.Split(parsed.Path, "/")
		if len(pathParts) > 1 {
			representation = pathParts[1]
		}
		key := ""
		if identity != "" {
			key = identity + "\x00" + representation
		} else if newsLane {
			key = parsed.Hostname()
		} else {
			key = row["url"].(string)
		}
		if seenPages[key] {
			duplicateRows = append(duplicateRows, row)
		} else {
			distinctRows = append(distinctRows, row)
		}
		seenPages[key] = true
	}
	var selected []map[string]any
	for _, row := range append(append([]map[string]any(nil), distinctRows...), duplicateRows...) {
		duplicated := false
		for _, previous := range selected {
			if previous["url"] == row["url"] {
				duplicated = true
				break
			}
		}
		if !duplicated {
			selected = append(selected, row)
		}
		if len(selected) == MaxFetchesPerCandidate {
			break
		}
	}
	var searchRejection *[2]string
	if len(selected) == 0 {
		searchRejection = &[2]string{"admission", "no_eligible_search_result"}
	}
	var resultURLs []any
	for _, row := range bounded {
		if row == nil {
			continue
		}
		rawURL := row["url"]
		if !strings.HasPrefix(rawURL, "https://") &&
			!strings.HasPrefix(rawURL, "http://") {
			continue
		}
		resultURLs = append(resultURLs, rawURL)
	}
	evidenceSearchReceipt := map[string]any{}
	for key, value := range providerReceipt {
		evidenceSearchReceipt[key] = value
	}
	evidenceSearchReceipt["requested_query"] = pyStr(plan["primary_query"])
	evidenceSearchReceipt["engine"] = engine
	evidenceSearchReceipt["result_urls"] = resultURLs
	evidenceSearchReceipt["result_count"] = len(bounded)
	searchReceipt := map[string]any{}
	for key, value := range evidenceSearchReceipt {
		searchReceipt[key] = value
	}
	payloadMapOrEmpty(evidence["search"])["provider_receipt"] = evidenceSearchReceipt

	// 抓取阶段：共享任务表去重，等待全部选行完成。
	var auditRefs []map[string]string
	fetchFailure := ""
	var fetchedRows [][3]any
	var wg sync.WaitGroup
	var mu sync.Mutex
	for _, row := range selected {
		row := row
		wg.Add(1)
		go func() {
			defer wg.Done()
			rawURL := row["url"].(string)
			taskKey := lane + "\x00" + rawURL
			fetchTasks.mu.Lock()
			entry, present := fetchTasks.tasks[taskKey]
			if !present {
				ref := map[string]string{
					"operation": "fetch",
					"operation_id": fmt.Sprintf("external-operation-%s",
						newUUIDHex()),
					"request_id": fmt.Sprintf("external-request-%s", newUUIDHex()),
				}
				ctx, cancel := context.WithTimeout(context.Background(),
					V6FetchWaitSeconds*time.Second)
				entry = &fetchTaskEntry{ref: ref,
					done: make(chan fetchOutcome, 1), cancel: cancel}
				fetchTasks.tasks[taskKey] = entry
				go func() {
					defer close(entry.done)
					page, fetchErr := p.fetchPage(ctx, rawURL)
					entry.done <- fetchOutcome{page: page, err: fetchErr}
				}()
			}
			fetchTasks.mu.Unlock()
			mu.Lock()
			auditRefs = append(auditRefs, entry.ref)
			mu.Unlock()
			fetchEvidence := map[string]any{
				"operation_id":  entry.ref["operation_id"],
				"request_id":    entry.ref["request_id"],
				"requested_url": rawURL, "final_url": nil,
				"http_status_code": nil, "document_revision_id": nil,
			}
			mu.Lock()
			evidence["fetches"] = append(pyListAny(evidence["fetches"]), fetchEvidence)
			mu.Unlock()
			outcome, ok := <-entry.done
			if !ok || outcome.err != nil {
				if !ok {
					lanes.RecordTimedOut([]string{lane})
				}
				mu.Lock()
				fetchFailure = "provider_invocation_failed"
				mu.Unlock()
				return
			}
			page := outcome.page
			finalURL := strings.TrimSpace(pyStrOr(page["url"], rawURL))
			statusCode := any(nil)
			if _, hasStatus := page["status_code"]; hasStatus && page["status_code"] != nil {
				if parsed, parseErr := pyIntOf(page["status_code"]), error(nil); true {
					_ = parseErr
					if parsed != 0 {
						statusCode = parsed
					}
				}
			}
			fetchEvidence["final_url"] = finalURL
			if finalURL == "" {
				fetchEvidence["final_url"] = rawURL
			}
			fetchEvidence["http_status_code"] = statusCode
			mu.Lock()
			fetchedRows = append(fetchedRows, [3]any{row, page, fetchEvidence})
			mu.Unlock()
		}()
	}
	wg.Wait()
	return &CandidateEvidence{
		SearchAuditRef: searchAuditRef, Evidence: evidence,
		SearchRejection: searchRejection, SearchReceipt: searchReceipt,
		FetchedRows: fetchedRows, FetchAuditRefs: auditRefs,
		FetchFailure: fetchFailure,
	}
}

// fetchPage 抓取一页（p._fetch 的上限封装，对齐 :7631-7637）。
func (p *TopicPipeline) fetchPage(ctx context.Context, target string) (map[string]any, error) {
	if p.fetch == nil {
		return nil, fmt.Errorf("fetch unavailable")
	}
	return p.fetch(ctx, target, int(V6FetchWaitSeconds))
}

// CancelAllOwned 取消全部未完成抓取任务（owns_fetches 分支的清理等价）。
func (t *FetchTasks) CancelAllOwned() {
	t.mu.Lock()
	defer t.mu.Unlock()
	for _, entry := range t.tasks {
		if entry.cancel != nil {
			entry.cancel()
		}
	}
}

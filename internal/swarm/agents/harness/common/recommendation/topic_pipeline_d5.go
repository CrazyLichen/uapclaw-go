package recommendation

// topic_pipeline_d5.go 一比一对齐上游 topic_pipeline.py:5396-5942 的
// 摘要阶段预算/RID 预检/摘要批/完结会话摘要/持久化，附
// topic_history.py:541-611 的 StagePreflight/PreflightStage/StageTokenCounter
// 纯函数切片（⤴️31d 归位）。
//
// 已知偏差（§5 登记）：StageTokenCounter 的 tiktoken 计数在 Go 侧尚无等价
// 实现（topic_history.py:553 属 31d）——计数函数经构造注入，未注入时
// ContextCountingUnverified fail-closed（Python 装有 tiktoken 时成功）；
// 真实计数随 31d 落地回填。asyncio.Semaphore/gather → errgroup+semaphore
// 等价并发映射。

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"math"
	"sort"
	"strings"
	"sync"

	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	"github.com/uapclaw/uapclaw-go/internal/common/utils"

	swarmsession "github.com/uapclaw/uapclaw-go/internal/swarm/server/session"
)

// GetAllSessionsMetadataForPipeline 会话元数据读取（topic_pipeline.py 多处
// get_all_sessions_metadata(limit=1_000_000, offset=0)）。
func GetAllSessionsMetadataForPipeline() ([]map[string]any, int) {
	return swarmsession.GetAllSessionsMetadata(1_000_000, 0)
}

// ──────────────────────────── 常量 ────────────────────────────

// PreparedSummaryLimit 预备摘要的会话数上限（topic_pipeline.py:231）
const PreparedSummaryLimit = 8

// SummaryBatchConcurrency 摘要批并发上限（:253）
const SummaryBatchConcurrency = 3

// RIDReservedOutputTokens RID 预留输出 token（:261）
const RIDReservedOutputTokens = 2048

// RIDNothingToJudge RID 无可判输入的降级原因（:188）
const RIDNothingToJudge = "no_rid_input"

// TokenSafetyFactor token 估算安全系数（topic_history.py:63）
const TokenSafetyFactor = 1.25

// ──────────────────────────── 结构体 ────────────────────────────

// ContextCountingUnverified token 计数不可信（topic_history.py:549-551 语义）。
type ContextCountingUnverified struct {
	// Reason 底层错误
	Reason string
}

// Error 实现 error 接口。
func (e *ContextCountingUnverified) Error() string { return e.Reason }

// StagePreflight 阶段上下文预检结果（topic_history.py:547-551）。
type StagePreflight struct {
	// Status fit | context_limit | context_limit_unverified
	Status string
	// EstimatedTokens 估算 token（可空）
	EstimatedTokens any
	// Limit 限额（可空）
	Limit any
}

// StageTokenCounter 显式 token 计数，无静默 length//4 回退（topic_history.py
// :553-588 StageTokenCounter）。计数函数经构造注入（⤴️31d tiktoken 等价）。
type StageTokenCounter struct {
	count func(text string) int
}

// NewStageTokenCounter 以注入的计数函数构造；nil 返回计数不可信错误。
func NewStageTokenCounter(count func(string) int) (*StageTokenCounter, error) {
	if count == nil {
		return nil, &ContextCountingUnverified{Reason: "token counter unavailable"}
	}
	return &StageTokenCounter{count: count}, nil
}

// CountText 计数文本。
func (c *StageTokenCounter) CountText(text string) int { return c.count(text) }

// FramedTokens 框架化系统与用户载荷的 token 数（对齐 :571-577）。
func (c *StageTokenCounter) FramedTokens(systemPrompt, userPayload string) int {
	return c.CountText(fmt.Sprintf("<|start|>system\n%s<|end|>", systemPrompt)) +
		c.CountText(fmt.Sprintf("<|start|>user\n%s<|end|>", userPayload)) + 3
}

// EstimateStageTokens 阶段 token 估算（对齐 :579-586）。
func (c *StageTokenCounter) EstimateStageTokens(systemPrompt, userPayload string,
	reservedOutput int) int {
	framed := c.FramedTokens(systemPrompt, userPayload)
	return int(math.Ceil(float64(framed)*TokenSafetyFactor)) + reservedOutput
}

// PreflightStage 阶段上下文预检（topic_history.py:590-603 preflight_stage）。
func PreflightStage(counter *StageTokenCounter, limit ContextLimitInfo,
	systemPrompt, userPayload string, reservedOutput int) StagePreflight {
	if counter == nil || limit.Value == nil {
		return StagePreflight{Status: "context_limit_unverified"}
	}
	estimated := counter.EstimateStageTokens(systemPrompt, userPayload, reservedOutput)
	if estimated > *limit.Value {
		return StagePreflight{Status: "context_limit",
			EstimatedTokens: estimated, Limit: *limit.Value}
	}
	return StagePreflight{Status: "fit", EstimatedTokens: estimated, Limit: *limit.Value}
}

// ──────────────────────────── 导出函数 ────────────────────────────

// SummaryStageBudget 摘要批的计数与限额（fail-closed）
// （topic_pipeline.py:5396-5411 _summary_stage_budget）。
func SummaryStageBudget(config *ArkBackgroundConfig,
	counterFactory func(string) (*StageTokenCounter, error)) (*StageTokenCounter, int, error) {
	if config.ContextWindowTokens == 0 {
		return nil, 0, &TerminalGenerationRejection{
			Reason: "session_summary_context_limit_unverified"}
	}
	counter, err := counterFactory(config.Model)
	if err != nil {
		return nil, 0, &TerminalGenerationRejection{
			Reason: "session_summary_context_limit_unverified"}
	}
	return counter, int(config.ContextWindowTokens), nil
}

// PreflightRIDInputs 以厂商上下文窗口约束单次 RID 调用（fail-closed）
// （topic_pipeline.py:5413-5439 _preflight_rid_inputs）。
func PreflightRIDInputs(config *ArkBackgroundConfig, ridInputs map[string]any,
	counterFactory func(string) (*StageTokenCounter, error)) error {
	if config.ContextWindowTokens == 0 {
		return &TerminalGenerationRejection{Reason: "rid_context_limit_unverified"}
	}
	counter, err := counterFactory(config.Model)
	if err != nil {
		return &TerminalGenerationRejection{Reason: "rid_context_limit_unverified"}
	}
	limit := int(config.ContextWindowTokens)
	preflight := PreflightStage(counter,
		ContextLimitInfo{Value: &limit, Source: "ARK_CONTEXT_WINDOW_TOKENS"},
		RIDAPISystemPrompt(), RIDRequestEstimatePayload(ridInputs), RIDReservedOutputTokens)
	if preflight.Status != "fit" {
		return &TerminalGenerationRejection{Reason: "rid_context_limit"}
	}
	return nil
}

// SummarizeSessions 摘要快照中的会话（topic_pipeline.py:5441-5689
// _summarize_sessions）。批并发 3，答案漏写/重名/空白的会话重问一次。
func (p *TopicPipeline) SummarizeSessions(snapshot *V6ContextSnapshot,
	config *ArkBackgroundConfig, invokeStage TopicStageInvoker) (*sessionSummaryBatch, error) {
	providerVersion := pyStrOr(config.ExpectedModel, config.Model)
	summaryConfigVersion := fmt.Sprintf(
		"%s:request_model=%s:expected_model=%s:temperature=%s:top_p=%s:context=%d:base_url=%s:reasoning_effort=disabled",
		SessionSummaryConfigVersion, config.Model, providerVersion,
		utils.PyFloatStr(config.EffectiveTemperature()), utils.PyFloatStr(config.EffectiveTopP()),
		config.ContextWindowTokens, config.BaseURL)
	grouped := map[string][]*V6SourceMessage{}
	sessionOrder := []string{}
	for _, message := range snapshot.Messages {
		if _, seen := grouped[message.SessionID]; !seen {
			sessionOrder = append(sessionOrder, message.SessionID)
		}
		grouped[message.SessionID] = append(grouped[message.SessionID], message)
	}
	if len(grouped) == 0 {
		return &sessionSummaryBatch{
			artifacts: []*SessionSummaryArtifact{},
			coverageReceipt: map[string]any{
				"schema_version":         SessionSummaryCoverageReceiptVersion,
				"input_snapshot_id":      snapshot.InputSnapshotID,
				"context_scope":          snapshot.ContextScope,
				"summary_config_version": summaryConfigVersion,
				"sessions":               []any{},
			},
			summaryConfigVersion: summaryConfigVersion,
		}, nil
	}
	prefixesBySession := map[string]*CompletedSessionProjection{}
	for _, prefix := range snapshot.CompletedSessionPrefixes {
		prefixesBySession[prefix.Boundary.SessionID] = prefix
	}
	for sessionID := range grouped {
		if _, present := prefixesBySession[sessionID]; !present {
			return nil, &TerminalGenerationRejection{Reason: "session_completion_boundary_missing"}
		}
	}
	deltaEventIDs := make([]string, 0, len(snapshot.DeltaEventIDs))
	deltaEventIDs = append(deltaEventIDs, snapshot.DeltaEventIDs...)
	selectedSessions := RecentSummarySessions(grouped, prefixesBySession,
		deltaEventIDs, p.Now())
	selectedGrouped := map[string][]*V6SourceMessage{}
	for _, sessionID := range selectedSessions {
		selectedGrouped[sessionID] = grouped[sessionID]
	}
	grouped = selectedGrouped
	sessionOrder = selectedSessions
	sort.Strings(sessionOrder)
	artifactsBySession := map[string]*SessionSummaryArtifact{}
	var pendingSessions []map[string]any
	pendingMetadata := map[string]map[string]any{}
	var generatedSummaryIDs []string
	for _, sessionID := range sessionOrder {
		messages := append([]*V6SourceMessage(nil), grouped[sessionID]...)
		sort.SliceStable(messages, func(i, j int) bool {
			return messages[i].Ordinal < messages[j].Ordinal
		})
		inputOrigin, assistanceOrigin := SummaryOrigins(messages)
		// 覆盖、digest 与模型请求共用同一轮选择。
		sessionPayload := SessionIdentityPayload(sessionID, messages)
		boundary := prefixesBySession[sessionID].Boundary
		coverage := SummaryCoverageIDs(messages)
		var covered []*V6SourceMessage
		for _, message := range messages {
			if coverage[message.EventID] {
				covered = append(covered, message)
			}
		}
		sourceEventIDs := make([]any, 0, len(covered))
		sourceLocatorPayloads := make([]any, 0, len(covered))
		for _, item := range covered {
			sourceEventIDs = append(sourceEventIDs, item.EventID)
			sourceLocatorPayloads = append(sourceLocatorPayloads, (&SessionSourceLocator{
				SessionID: item.SessionID, Ordinal: item.Ordinal, EventID: item.EventID,
			}).AsPayload())
		}
		semanticIdentity := map[string]any{
			"context_scope":            snapshot.ContextScope,
			"session":                  sessionPayload,
			"source_event_ids":         sourceEventIDs,
			"source_locators":          sourceLocatorPayloads,
			"completion_boundary":      boundary.AsPayload(),
			"summary_provider_id":      "ark",
			"summary_provider_version": providerVersion,
			"summary_config_version":   summaryConfigVersion,
		}
		// json.dumps(..., ensure_ascii=False, separators=(",", ":"), sort_keys=True)
		semanticDigest := sha256.Sum256([]byte(sortedCompactJSON(semanticIdentity)))
		summaryID := fmt.Sprintf("summary:%s:%s", sessionID,
			hex.EncodeToString(semanticDigest[:]))
		metadata := map[string]any{
			"session_summary_id":       summaryID,
			"session_id":               sessionID,
			"context_scope":            snapshot.ContextScope,
			"input_origin":             inputOrigin,
			"assistance_origin":        assistanceOrigin,
			"source_event_ids":         sourceEventIDs,
			"source_locators":          sourceLocatorPayloads,
			"completion_boundary":      boundary,
			"completed_at":             boundary.CompletedAt,
			"summary_provider_id":      "ark",
			"summary_provider_version": providerVersion,
			"summary_config_version":   summaryConfigVersion,
		}
		rows, err := p.Store.QueryEvents(QueryEventsOptions{
			EventType:   "session_summary",
			TopicID:     stringPtr(summaryID),
			PolicyID:    stringPtr(ProductPolicyID),
			Limit:       2,
			NewestFirst: true,
		})
		if err != nil {
			return nil, err
		}
		if len(rows) > 0 {
			if len(rows) != 1 {
				return nil, &TerminalGenerationRejection{Reason: "session_summary_persistence_conflict"}
			}
			payload, _, ok := payloadMap(rows[0]["payload"])
			if !ok {
				return nil, &TerminalGenerationRejection{Reason: "session_summary_persistence_conflict"}
			}
			cached, err := SessionSummaryArtifactFromPayload(payload)
			if err != nil {
				return nil, &TerminalGenerationRejection{Reason: "session_summary_persistence_conflict"}
			}
			if summaryMetadataDrift(cached, metadata) {
				return nil, &TerminalGenerationRejection{Reason: "session_summary_persistence_conflict"}
			}
			artifactsBySession[sessionID] = cached
			continue
		}
		pendingSessions = append(pendingSessions, SessionModelPayload(sessionID, messages))
		pendingMetadata[sessionID] = metadata
	}

	if len(pendingSessions) > 0 {
		counter, limit, err := SummaryStageBudget(config, p.StageTokenCounterFactory)
		if err != nil {
			return nil, err
		}
		var boundedSessions []map[string]any
		for _, session := range pendingSessions {
			fitted := FitSummarySession(session, counter, snapshot, limit)
			if fitted == nil {
				return nil, &TerminalGenerationRejection{Reason: "session_summary_session_too_large"}
			}
			boundedSessions = append(boundedSessions, fitted)
		}
		batches := PlanSummaryBatches(boundedSessions, counter, snapshot, limit)
		if batches == nil {
			return nil, &TerminalGenerationRejection{Reason: "session_summary_context_limit"}
		}
		var mu sync.Mutex
		summariesBySession := map[string]string{}
		var wg sync.WaitGroup
		semaphore := make(chan struct{}, SummaryBatchConcurrency)
		failures := make(chan error, len(batches))
		for _, positions := range batches {
			positions := positions
			wg.Add(1)
			go func() {
				defer wg.Done()
				semaphore <- struct{}{}
				defer func() { <-semaphore }()
				batch := make([]map[string]any, 0, len(positions))
				for _, position := range positions {
					batch = append(batch, boundedSessions[position])
				}
				answered, err := p.RequestSessionSummaries(snapshot, batch, invokeStage)
				if err != nil {
					failures <- err
					return
				}
				// 答案漏写的会话重问一次；仍未摘出的失败失败整个准备。
				var unsummarized []map[string]any
				for _, session := range batch {
					if _, present := answered[pyStr(session["session_id"])]; !present {
						unsummarized = append(unsummarized, session)
					}
				}
				if len(unsummarized) > 0 {
					retry, err := p.RequestSessionSummaries(snapshot, unsummarized, invokeStage)
					if err != nil {
						failures <- err
						return
					}
					for sessionID, summary := range retry {
						answered[sessionID] = summary
					}
				}
				mu.Lock()
				for sessionID, summary := range answered {
					summariesBySession[sessionID] = summary
				}
				mu.Unlock()
			}()
		}
		wg.Wait()
		close(failures)
		for failure := range failures {
			return nil, failure
		}
		if len(summariesBySession) != len(pendingMetadata) {
			return nil, &TerminalGenerationRejection{Reason: "session_summary_contract_failed"}
		}
		for sessionID, summary := range summariesBySession {
			metadata := pendingMetadata[sessionID]
			artifact, err := SessionSummaryArtifactFromMetadata(summary, metadata)
			if err != nil {
				return nil, err
			}
			artifactsBySession[sessionID] = artifact
			generatedSummaryIDs = append(generatedSummaryIDs, artifact.SessionSummaryID)
		}
	}

	var artifacts []*SessionSummaryArtifact
	for _, sessionID := range sessionOrder {
		artifacts = append(artifacts, artifactsBySession[sessionID])
	}
	generatedSet := map[string]bool{}
	for _, id := range generatedSummaryIDs {
		generatedSet[id] = true
	}
	sessions := make([]any, 0, len(artifacts))
	for _, artifact := range artifacts {
		exclusionReason := any(nilValue())
		if !artifact.NaturalRevisitEligible() {
			exclusionReason = "non_natural_or_assisted_input"
		}
		sessions = append(sessions, map[string]any{
			"session_id":           artifact.SessionID,
			"session_summary_id":   artifact.SessionSummaryID,
			"input_origin":         artifact.InputOrigin,
			"assistance_origin":    artifact.AssistanceOrigin,
			"rid_eligible":         artifact.NaturalRevisitEligible(),
			"rid_exclusion_reason": exclusionReason,
		})
	}
	var generatedIDs []string
	for _, summary := range artifacts {
		if generatedSet[summary.SessionSummaryID] {
			generatedIDs = append(generatedIDs, summary.SessionSummaryID)
		}
	}
	return &sessionSummaryBatch{
		artifacts: artifacts,
		coverageReceipt: map[string]any{
			"schema_version":         SessionSummaryCoverageReceiptVersion,
			"input_snapshot_id":      snapshot.InputSnapshotID,
			"context_scope":          snapshot.ContextScope,
			"summary_config_version": summaryConfigVersion,
			"sessions":               sessions,
		},
		summaryConfigVersion: summaryConfigVersion,
		generatedSummaryIDs:  generatedIDs,
	}, nil
}

// RequestSessionSummaries 请求摘要，保持答案给出的每条摘要恰一次
// （topic_pipeline.py:5691-5734 _request_session_summaries）。
func (p *TopicPipeline) RequestSessionSummaries(snapshot *V6ContextSnapshot,
	sessions []map[string]any, invokeStage TopicStageInvoker) (map[string]string, error) {
	response, err := invokeStage.Invoke(context.Background(), "v6_session_summarizer",
		pipelineSessionSummaryPrompt, SummaryRequestPayload(
			snapshot.InputSnapshotID, snapshot.ContextScope, sessions,
			"v6-session-summary-"+newUUIDHex()))
	if err != nil {
		return nil, err
	}
	var items []any
	if payload, ok := response.(map[string]any); ok {
		if len(payload) == 1 {
			if summaries, ok := payload["summaries"].([]any); ok {
				items = summaries
			}
		}
	}
	expected := map[string]bool{}
	for _, session := range sessions {
		expected[pyStr(session["session_id"])] = true
	}
	named := map[string]int{}
	for _, raw := range items {
		if item, ok := raw.(map[string]any); ok {
			named[pyStr(item["session_id"])]++
		}
	}
	summaries := map[string]string{}
	for _, raw := range items {
		item, ok := raw.(map[string]any)
		if !ok || len(item) != 2 {
			continue
		}
		if _, hasSession := item["session_id"]; !hasSession {
			continue
		}
		if _, hasSummary := item["summary"]; !hasSummary {
			continue
		}
		sessionID := pyStr(item["session_id"])
		summary := strings.TrimSpace(pyStr(item["summary"]))
		if expected[sessionID] && named[sessionID] == 1 && summary != "" {
			summaries[sessionID] = summary
		}
	}
	outcome := "business_validation_failed"
	if len(summaries) == len(expected) {
		outcome = "business_validated"
	}
	RecordProviderValidationTopic(invokeStage, response, outcome)
	return summaries, nil
}

// SummarizeCompletedSessions 在每个 scope 摘要其最新摘要之后完结的会话
// （topic_pipeline.py:5736-5822 _summarize_completed_sessions）。只写摘要：
// 无覆盖小票，因为没有生成消费它们。摘要配置升级与超过
// PreparedSummaryLimit 的 scope 留给该 scope 自己的生成。
func (p *TopicPipeline) SummarizeCompletedSessions(config *ArkBackgroundConfig) {
	defer func() {
		if recovered := recover(); recovered != nil {
			loggerWarn("Topic Starter v6 summary preparation failed")
		}
	}()
	records := p.CompletedRecordsLoader()
	metadata, _ := GetAllSessionsMetadataForPipeline()
	projectBySession := map[string]string{}
	for _, item := range metadata {
		projectBySession[pyStr(item["session_id"])] = pyStr(item["project_id"])
	}
	completedByScope := map[string]map[string][2]any{}
	for sessionID, sessionRecords := range records {
		prefix := ProjectCompletedSessionPrefix(sessionID, sessionRecords, true)
		if prefix == nil {
			continue
		}
		projectID, known := projectBySession[sessionID]
		if !known {
			projectID = ""
		}
		scope := "project:" + projectID
		if PipelineScopeMatches(projectID, GlobalNewChatScope) {
			scope = GlobalNewChatScope
		}
		answers := CompletedAnswers(sessionID, sessionRecords)
		type recordTurn struct {
			eventID string
			content string
			answer  any
		}
		var recordTurns []SummaryTurn
		ordinals := append([]int(nil), prefix.CompletedUserOrdinals...)
		sort.Ints(ordinals)
		for _, ordinal := range ordinals {
			record := sessionRecords[ordinal]
			if record == nil || !SummarizableUserTurn(record) {
				continue
			}
			eventID := pyStrOr(record["id"], pyStr(record["request_id"]))
			content, isStr := record["content"].(string)
			if eventID == "" || !isStr {
				continue
			}
			recordTurns = append(recordTurns, SummaryTurn{
				Message: &V6SourceMessage{EventID: eventID},
				Content: content,
				Answer:  answers[pyStr(record["request_id"])],
			})
		}
		turnIDs := map[string]bool{}
		for _, turn := range SelectSummaryTurns(recordTurns) {
			turnIDs[turn.Message.EventID] = true
		}
		if len(turnIDs) > 0 {
			if completedByScope[scope] == nil {
				completedByScope[scope] = map[string][2]any{}
			}
			completedByScope[scope][sessionID] = [2]any{prefix.Boundary.CompletedAt, turnIDs}
		}
	}
	pendingByScope := map[string]map[string]bool{}
	cutoff := p.Now() - SummarySessionWindowSeconds
	for scope, completed := range completedByScope {
		type rankedEntry struct {
			sessionID   string
			completedAt float64
			turnIDs     map[string]bool
		}
		var ranked []rankedEntry
		for sessionID, value := range completed {
			completedAt := value[0].(float64)
			turnIDs := value[1].(map[string]bool)
			if completedAt >= cutoff && len(turnIDs) > 0 {
				ranked = append(ranked, rankedEntry{sessionID, completedAt, turnIDs})
			}
		}
		sort.Slice(ranked, func(i, j int) bool {
			if ranked[i].completedAt != ranked[j].completedAt {
				return ranked[i].completedAt > ranked[j].completedAt
			}
			return ranked[i].sessionID < ranked[j].sessionID
		})
		if len(ranked) > SummarySessionLimit {
			ranked = ranked[:SummarySessionLimit]
		}
		selected := map[string]bool{}
		for _, entry := range ranked {
			selected[entry.sessionID] = true
		}
		latest, err := LatestSessionSummaries(p.Store, scope, selected)
		if err != nil {
			loggerWarn("Topic Starter v6 summary preparation failed")
			return
		}
		pending := map[string]bool{}
		for _, entry := range ranked {
			if summaryLags(latest[entry.sessionID], entry.completedAt, entry.turnIDs) {
				pending[entry.sessionID] = true
			}
		}
		if len(pending) > 0 && len(pending) <= PreparedSummaryLimit {
			pendingByScope[scope] = pending
		}
	}
	scopes := make([]string, 0, len(pendingByScope))
	for scope := range pendingByScope {
		scopes = append(scopes, scope)
	}
	sort.Strings(scopes)
	for _, scope := range scopes {
		if reason := p.SummarizeScopeSessions(config, scope, pendingByScope[scope]); reason != "" {
			loggerWarn("Topic Starter v6 session summaries not written for " + scope + ": " + reason)
		}
	}
}

// SummarizeScopeSessions 摘要一个 scope 的待补会话
// （topic_pipeline.py:5824-5883 _summarize_scope_sessions）。
func (p *TopicPipeline) SummarizeScopeSessions(config *ArkBackgroundConfig,
	contextScope string, sessionIDs map[string]bool) string {
	snapshot := p.contextLoader(contextScope)
	if snapshot == nil {
		return "summary_input_failed"
	}
	// 快照决定哪些轮成为消息；只有最新摘要缺轮或缺边界的会话才被摘要。
	latest, err := LatestSessionSummaries(p.Store, contextScope, sessionIDs)
	if err != nil {
		return "summary_input_failed"
	}
	boundaries := map[string]*SessionCompletionBoundary{}
	for _, item := range snapshot.CompletedSessionPrefixes {
		boundaries[item.Boundary.SessionID] = item.Boundary
	}
	groupedMessages := map[string][]*V6SourceMessage{}
	for _, item := range snapshot.Messages {
		if sessionIDs[item.SessionID] {
			groupedMessages[item.SessionID] = append(groupedMessages[item.SessionID], item)
		}
	}
	pending := map[string]bool{}
	for sessionID, sessionMessages := range groupedMessages {
		boundary, known := boundaries[sessionID]
		if !known {
			continue
		}
		if summaryLags(latest[sessionID], boundary.CompletedAt,
			SummaryCoverageIDs(sessionMessages)) {
			pending[sessionID] = true
		}
	}
	if len(pending) == 0 {
		return ""
	}
	filtered := &V6ContextSnapshot{}
	*filtered = *snapshot
	var filteredMessages []*V6SourceMessage
	for _, item := range snapshot.Messages {
		if pending[item.SessionID] {
			filteredMessages = append(filteredMessages, item)
		}
	}
	filtered.Messages = filteredMessages
	var filteredPrefixes []*CompletedSessionProjection
	for _, item := range snapshot.CompletedSessionPrefixes {
		if pending[item.Boundary.SessionID] {
			filteredPrefixes = append(filteredPrefixes, item)
		}
	}
	filtered.CompletedSessionPrefixes = filteredPrefixes
	ownedStageInvoker := p.stageInvokerFactory(config)
	p.summaryLockFor(contextScope).Lock()
	summaryBatch, err := p.SummarizeSessions(filtered, config, ownedStageInvoker)
	if err == nil {
		err = p.PersistSessionSummaryBatch(summaryBatch)
	}
	p.summaryLockFor(contextScope).Unlock()
	if err != nil {
		if rejection, ok := err.(*TerminalGenerationRejection); ok {
			return rejection.Reason
		}
		loggerWarn("Topic Starter v6 session summaries failed")
		return "session_summary_failed"
	}
	return ""
}

// PersistSessionSummaries 摘要落库（topic_pipeline.py:5885-5909）。
func (p *TopicPipeline) PersistSessionSummaries(summaries []*SessionSummaryArtifact) error {
	for _, summary := range summaries {
		if PersistOutcomeArtifact(p.Store, summary, summary.CompletedAt) {
			continue
		}
		// 续批3 已批准 outcomes 全量 v6：存储走 ProductPolicyID，冲突复核
		// 查询与存储同策略（Python 侧 POLICY_ID=v5 与其 storage 一致）。
		existing, err := p.Store.QueryEvents(QueryEventsOptions{
			EventType:   "session_summary",
			TopicID:     stringPtr(summary.SessionSummaryID),
			PolicyID:    stringPtr(ProductPolicyID),
			Limit:       2,
			NewestFirst: true,
		})
		if err != nil {
			return err
		}
		if len(existing) != 1 {
			return &TerminalGenerationRejection{Reason: "session_summary_persistence_conflict"}
		}
		// AsPayload 含 []map[string]any 类型切片，先 JSON 往返规整为
		// []any 再做键排序等值（typed 切片绕过 normalizeForStableJSON）。
		if !anyEqual(jsonRoundTrip(mapAnyOf(existing[0]["payload"])),
			jsonRoundTrip(summary.AsPayload())) {
			return &TerminalGenerationRejection{Reason: "session_summary_persistence_conflict"}
		}
	}
	return nil
}

// PersistSessionSummaryBatch 摘要批与覆盖小票落库
// （topic_pipeline.py:5911-5941 _persist_session_summary_batch）。
func (p *TopicPipeline) PersistSessionSummaryBatch(summaryBatch *sessionSummaryBatch) error {
	if err := p.PersistSessionSummaries(summaryBatch.artifacts); err != nil {
		return err
	}
	coverage := mapCopyAny(summaryBatch.coverageReceipt)
	coverageTopicID := SessionSummaryCoverageTopicID(coverage)
	coverageEvent := map[string]any{
		"session_id":        "topic-starter-session-summary",
		"slate_id":          pyStr(coverage["context_scope"]),
		"event_type":        SessionSummaryCoverageEventType,
		"topic_id":          coverageTopicID,
		"policy_id":         POLICY_ID,
		"selection_mode":    "training_data_plane",
		"propensity_reason": "not_applicable",
		"payload":           coverage,
		"created_at":        p.Now(),
	}
	created, err := p.Store.RecordEvent(coverageEvent)
	if err != nil {
		return err
	}
	if created {
		return nil
	}
	existing, err := p.Store.QueryEvents(QueryEventsOptions{
		EventType:   SessionSummaryCoverageEventType,
		TopicID:     stringPtr(coverageTopicID),
		PolicyID:    stringPtr(POLICY_ID),
		Limit:       2,
		NewestFirst: true,
	})
	if err != nil {
		return err
	}
	if len(existing) != 1 || !anyEqual(jsonRoundTrip(mapAnyOf(existing[0]["payload"])),
		jsonRoundTrip(coverage)) {
		return &TerminalGenerationRejection{Reason: "session_summary_coverage_persistence_conflict"}
	}
	return nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// summaryLags 最新摘要早于边界或缺其中一轮（对齐 :2991-2996 _summary_lags）。
func summaryLags(latest map[string]any, completedAt float64, turnIDs map[string]bool) bool {
	completed := toFloatOr(latest["completed_at"], 0)
	missing := 0
	for eventID := range turnIDs {
		if !containsString(pyListStrings(latest["source_event_ids"]), eventID) {
			missing++
		}
	}
	return completed < completedAt || missing > 0
}

// summaryMetadataDrift 缓存摘要与本次元数据是否漂移（对齐 :5566-5572
// getattr 比对；source_locators/boundary 以载荷等值比较）。
func summaryMetadataDrift(cached *SessionSummaryArtifact, metadata map[string]any) bool {
	if cached.SessionSummaryID != pyStr(metadata["session_summary_id"]) ||
		cached.SessionID != pyStr(metadata["session_id"]) ||
		cached.ContextScope != pyStr(metadata["context_scope"]) ||
		cached.InputOrigin != pyStr(metadata["input_origin"]) ||
		cached.AssistanceOrigin != pyStr(metadata["assistance_origin"]) ||
		cached.SummaryProviderID != pyStr(metadata["summary_provider_id"]) ||
		cached.SummaryProviderVersion != pyStr(metadata["summary_provider_version"]) ||
		cached.SummaryConfigVersion != pyStr(metadata["summary_config_version"]) {
		return true
	}
	if cached.CompletedAt != toFloatOr(metadata["completed_at"], 0) {
		return true
	}
	eventIDs := pyListStrings(metadata["source_event_ids"])
	if len(eventIDs) != len(cached.SourceEventIDs) {
		return true
	}
	for index, eventID := range eventIDs {
		if cached.SourceEventIDs[index] != eventID {
			return true
		}
	}
	locators := pyListMaps(metadata["source_locators"])
	if len(locators) != len(cached.SourceLocators) {
		return true
	}
	for index, locator := range locators {
		target := cached.SourceLocators[index]
		if pyStr(locator["session_id"]) != target.SessionID ||
			pyIntOf(locator["ordinal"]) != target.Ordinal ||
			pyStr(locator["event_id"]) != target.EventID {
			return true
		}
	}
	boundary, _ := metadata["completion_boundary"].(*SessionCompletionBoundary)
	if boundary == nil || !anyEqual(cached.CompletionBoundary.AsPayload(), boundary.AsPayload()) {
		return true
	}
	return false
}

// SessionSummaryArtifactFromMetadata 由摘要文本与元数据构造工件
// （对齐 :5651-5654 SessionSummaryArtifact(summary_text=..., **metadata)）。
func SessionSummaryArtifactFromMetadata(summaryText string,
	metadata map[string]any) (*SessionSummaryArtifact, error) {
	var sourceEventIDs []string
	for _, item := range pyListStrings(metadata["source_event_ids"]) {
		sourceEventIDs = append(sourceEventIDs, item)
	}
	var sourceLocators []*SessionSourceLocator
	for _, raw := range pyListMaps(metadata["source_locators"]) {
		sourceLocators = append(sourceLocators, &SessionSourceLocator{
			SessionID: pyStr(raw["session_id"]),
			Ordinal:   pyIntOf(raw["ordinal"]),
			EventID:   pyStr(raw["event_id"]),
		})
	}
	boundary, _ := metadata["completion_boundary"].(*SessionCompletionBoundary)
	return NewSessionSummaryArtifact(
		pyStr(metadata["session_summary_id"]), pyStr(metadata["session_id"]),
		pyStr(metadata["context_scope"]), summaryText,
		pyStr(metadata["input_origin"]), pyStr(metadata["assistance_origin"]),
		sourceEventIDs, sourceLocators, boundary,
		toFloatOr(metadata["completed_at"], 0),
		pyStr(metadata["summary_provider_id"]),
		pyStr(metadata["summary_provider_version"]),
		pyStr(metadata["summary_config_version"]))
}

// sortedCompactJSON json.dumps(..., ensure_ascii=False, separators=(",",":"),
// sort_keys=True) 等价：键排序紧凑序列化。
func sortedCompactJSON(value map[string]any) string {
	encoded, err := utils.MarshalNoEscape(sortKeysDeep(value))
	if err != nil {
		return ""
	}
	return string(encoded)
}

// sortKeysDeep 递归把 map 转为键排序的有序表示（json.Marshal 对 map[string]any
// 本身按键排序，此函数递归规整嵌套 []any 中的 map）。
func sortKeysDeep(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		out := map[string]any{}
		for key, item := range typed {
			out[key] = sortKeysDeep(item)
		}
		return out
	case []any:
		out := make([]any, 0, len(typed))
		for _, item := range typed {
			out = append(out, sortKeysDeep(item))
		}
		return out
	}
	return value
}

// RecordProviderValidationTopic 接口形态的校验记录（对齐 :5729-5733）。
func RecordProviderValidationTopic(invoker TopicStageInvoker, response any, outcome string) {
	if invoker != nil {
		invoker.RecordValidation(response, outcome)
	}
}

// logComponent 摘要链路日志组件。
const logComponentD5 = logger.ComponentAgentCore

// loggerWarn 组件级 Warn 快捷（摘要链路的上游日志）。
func loggerWarn(message string) {
	logger.Warn(logComponentD5).Msg(message)
}

// nilValue JSON null 等价。
func nilValue() any { return nil }

var _ = context.Background
var _ = utils.MarshalNoEscape

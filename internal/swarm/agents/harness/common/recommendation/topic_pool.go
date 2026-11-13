// Package recommendation topic 起始池：追加式卡片池、有序清单与后台生成协调。
//
// 本文件一比一对齐 topic_pool.py（937 行）：
// Append-only topic card pool, ordered manifests, and background generation。
//
// 对应 Python 代码：jiuwenswarm/agents/harness/common/recommendation/topic_pool.py
package recommendation

import (
	"context"
	crand "crypto/rand"
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strings"
	"sync"
	"time"
)

// ──────────────────────────── 结构体 ────────────────────────────

// GenerationTrigger 一个触发：作用域键 + 触发来源（对齐 :589 GenerationTrigger NamedTuple）。
type GenerationTrigger struct {
	// Key 触发键
	Key map[string]any
	// Origin 触发来源
	Origin string
}

// BackgroundGenerationResult 后台生成产出（对齐 :597 frozen dataclass）。
type BackgroundGenerationResult struct {
	// Cards 卡片列表
	Cards []map[string]any
	// Manifest 清单
	Manifest map[string]any
	// CorpusTextByID 语料文本（可空）
	CorpusTextByID map[string]string
	// FailureReason 失败原因（成功为空）
	FailureReason string
}

// PoolWriterFunc 起始池写入器形态（对齐 :612 pool_writer 默认 write_starter_pool）。
// expectedModel 承载 Python writer_kwargs["expected_model"]（见函数注释的已知偏差）。
type PoolWriterFunc = func(store *TopicStarterStore, cards []map[string]any, manifest map[string]any,
	createdAt float64, corpusTextByID map[string]string, expectedModel string) bool

// TerminalExternalAuditLoader 终局外部调用审计加载器形态（对齐 :580）。
type TerminalExternalAuditLoader = func(generationAttemptID string) map[string]any

// BackgroundGenerationCoordinator 每个完整生成触发键一个进程内任务（对齐 :604）。
type BackgroundGenerationCoordinator struct {
	// Store 事件账本
	Store *TopicStarterStore
	// DeadlineSeconds 总期限秒数
	DeadlineSeconds float64
	// poolWriter 池写入器
	poolWriter PoolWriterFunc
	// policyID 策略 ID
	policyID string
	// terminalExternalAuditLoader 审计加载器
	terminalExternalAuditLoader TerminalExternalAuditLoader
	// mu 保护 inflight
	mu sync.Mutex
	// inflight 在飞任务（token → run）
	inflight map[string]*backgroundRun
}

// backgroundRun 一次后台生成运行（对齐 asyncio.Task[str]）。
type backgroundRun struct {
	// done 关闭即结束
	done chan struct{}
	// outcome 结果串
	outcome string
}

// Wait 阻塞至结束并返回结果串（对齐 await task）。
func (r *backgroundRun) Wait() string {
	<-r.done
	return r.outcome
}

// ──────────────────────────── 常量 ────────────────────────────

const (
	// POLICY_ID 策略 ID（对齐 :27）
	POLICY_ID = "topic-starter-mvp-v5"
	// PAYLOAD_VERSION 池 payload 版本（对齐 :28）
	PAYLOAD_VERSION = "topic-starter-pool-v1"
	// BackgroundGenerationDeadlineSeconds 后台生成总期限（对齐 :29）
	BackgroundGenerationDeadlineSeconds = 180.0
)

// ──────────────────────────── 全局变量 ────────────────────────────

// generationAttemptIDKey ctx.Value 键（对齐 :576 _GENERATION_ATTEMPT_ID ContextVar）。
type generationAttemptIDKey struct{}

// poolPromptIDs prompt 版本 ID 集合（对齐 :31 frozenset）。
var poolPromptIDs = map[string]struct{}{
	"i1": {}, "i2": {}, "i3": {}, "i4": {}, "i5": {},
}

// poolCardRequiredFields 卡片必需字段（对齐 :32-60）。
var poolCardRequiredFields = map[string]struct{}{
	"card_id": {}, "card_revision_id": {}, "generation_batch_id": {}, "selector_rank": {},
	"source_starter_id": {}, "interest_summary_id": {}, "lane": {}, "origin": {}, "intent": {},
	"title": {}, "prompt": {}, "freshness_class": {}, "effective_time": {}, "expire_time": {},
	"evidence_bundle": {}, "source_memory_unit_ids": {}, "source_locators": {},
	"identity_anchor": {}, "context_anchors": {}, "model_provenance": {}, "prompt_versions": {},
	"policy_id": {}, "payload_version": {}, "retrieval_config_version": {}, "corpus_version": {},
}

// poolManifestRequiredFields 清单必需字段（对齐 :61-75）。
var poolManifestRequiredFields = map[string]struct{}{
	"manifest_id": {}, "generation_batch_id": {}, "card_revision_ids": {}, "trigger_origin": {},
	"local_date": {}, "generation_trigger_key": {}, "generation_input_snapshot": {},
	"serving_compatibility_key": {}, "negative_preference_version": {}, "policy_id": {},
	"payload_version": {},
}

// ──────────────────────────── 导出函数 ────────────────────────────

// BuildGenerationTriggerKey 新批次为何可以生成；无需摘要或机密（对齐 :83-113）。
func BuildGenerationTriggerKey(localDate string, profileSemanticVersion, negativePreferenceVersion int,
	arkModel, arkBaseURL string, effectiveTemperature, effectiveTopP float64,
	contextWindowTokens int, promptVersions map[string]any, corpusVersion,
	retrievalConfigVersion string) map[string]any {
	promptCopy := map[string]any{}
	for k, v := range promptVersions {
		promptCopy[k] = v
	}
	return map[string]any{
		"local_date":                  localDate,
		"profile_semantic_version":    profileSemanticVersion,
		"negative_preference_version": negativePreferenceVersion,
		"ark_model":                   arkModel,
		"ark_base_url":                arkBaseURL,
		"effective_temperature":       effectiveTemperature,
		"effective_top_p":             effectiveTopP,
		"context_window_tokens":       contextWindowTokens,
		"prompt_versions":             promptCopy,
		"policy_id":                   POLICY_ID,
		"payload_version":             PAYLOAD_VERSION,
		"corpus_version":              corpusVersion,
		"retrieval_config_version":    retrievalConfigVersion,
	}
}

// BuildServingCompatibilityKey 服务兼容键有意只剔除负偏好版本（对齐 :116-125）。
func BuildServingCompatibilityKey(generationTriggerKey map[string]any) map[string]any {
	out := map[string]any{}
	for k, v := range generationTriggerKey {
		if k != "negative_preference_version" {
			out[k] = v
		}
	}
	return out
}

// WriteStarterPool 原子追加 1–4 张不可变卡与有序清单（对齐 :315-362）。
// expectedModel 为 Python writer_kwargs["expected_model"] 的承载位：
// 上游 write_starter_pool 无该形参（bind_pool_writer_model=True 时上游会 TypeError，
// 属上游潜伏缺陷），Go 侧以参数显式承载、默认实现校验后忽略。
func WriteStarterPool(store *TopicStarterStore, cards []map[string]any, manifest map[string]any,
	createdAt float64, corpusTextByID map[string]string, expectedModel string) bool {
	// 对齐 :325-326 数量闸
	if len(cards) < 1 || len(cards) > 4 {
		return false
	}
	// 对齐 :327-330 卡片校验
	for _, card := range cards {
		if !validPoolCard(card, corpusTextByID) {
			return false
		}
	}
	// 对齐 :331-332 清单校验
	if !validPoolManifest(manifest, cards) {
		return false
	}
	manifestID := str(manifest["manifest_id"])
	events := make([]map[string]any, 0, len(cards)+1)
	for _, card := range cards {
		events = append(events, map[string]any{
			"session_id":        "v5-pool",
			"slate_id":          manifestID,
			"event_type":        "starter_card",
			"topic_id":          card["card_revision_id"],
			"position":          card["selector_rank"],
			"policy_id":         POLICY_ID,
			"selection_mode":    "pool_generation",
			"propensity_reason": "not_estimated",
			"payload":           card,
			"created_at":        createdAt,
		})
	}
	events = append(events, map[string]any{
		"session_id":        "v5-pool",
		"slate_id":          manifestID,
		"event_type":        "starter_slate_manifest",
		"topic_id":          manifestID,
		"policy_id":         POLICY_ID,
		"selection_mode":    "pool_generation",
		"propensity_reason": "not_estimated",
		"payload":           manifest,
		"created_at":        createdAt,
	})
	written, err := store.RecordEventsAtomic(events)
	if err != nil {
		return false
	}
	return written
}

// ClassifyStarterSlate 清单分类（对齐 :374-391）。
func ClassifyStarterSlate(cards []map[string]any) map[string]any {
	total := len(cards)
	personalized := 0
	trends := 0
	for _, card := range cards {
		if isPersonalized(card) {
			personalized++
		}
		if str(card["lane"]) == "fresh-trend" {
			trends++
		}
	}
	mode := "personalized_dynamic"
	if total == 0 {
		mode = "static"
	} else if personalized == 0 {
		mode = "generic_dynamic"
	} else if float64(personalized)/float64(total) < 0.75 {
		mode = "mixed_dynamic"
	}
	return map[string]any{
		"selection_mode":          mode,
		"slate_card_count":        total,
		"personalized_card_count": personalized,
		"trend_card_count":        trends,
	}
}

// FindExactPoolCard 把不可变清单成员关系连接到一个精确卡修订（对齐 :404-460）。
func FindExactPoolCard(store *TopicStarterStore, manifestID, cardRevisionID, generationBatchID string) map[string]any {
	events, err := store.QueryEvents(QueryEventsOptions{
		EventType:   "starter_slate_manifest",
		SlateID:     stringPtr(manifestID),
		PolicyID:    stringPtr(POLICY_ID),
		Limit:       1,
		NewestFirst: true,
	})
	if err != nil {
		return nil
	}
	var manifest map[string]any
	for _, event := range events {
		payload, _, isMap := payloadMap(event["payload"])
		if str(event["event_type"]) != "starter_slate_manifest" {
			continue
		}
		if str(event["slate_id"]) != manifestID {
			continue
		}
		if !isMap {
			continue
		}
		if str(payload["manifest_id"]) != manifestID {
			continue
		}
		if str(payload["generation_batch_id"]) != generationBatchID {
			continue
		}
		manifest = payload
		break
	}
	if manifest == nil || !stringInSlice(cardRevisionID, anySliceToStrings(manifest["card_revision_ids"])) {
		return nil
	}
	cardEvents, err := store.QueryEvents(QueryEventsOptions{
		EventType: "starter_card",
		SlateID:   stringPtr(manifestID),
		PolicyID:  stringPtr(POLICY_ID),
		Limit:     7,
	})
	if err != nil {
		return nil
	}
	for _, event := range cardEvents {
		payload, _, isMap := payloadMap(event["payload"])
		if str(event["slate_id"]) != manifestID {
			continue
		}
		if str(event["topic_id"]) != cardRevisionID {
			continue
		}
		if !isMap {
			continue
		}
		if str(payload["card_revision_id"]) != cardRevisionID {
			continue
		}
		if str(payload["generation_batch_id"]) != generationBatchID {
			continue
		}
		return payload
	}
	return nil
}

// ReadStarterPool 按选择序读一个兼容批次；绝不从其他批次补足（对齐 :467-574）。
// 依赖 topic_history 的两个函数经 ⤴️31d 过渡实现（history_state_store.go 同文件）。
func ReadStarterPool(store *TopicStarterStore, servingCompatibilityKey map[string]any,
	negativePreferenceVersion int, negativePreferences []map[string]any, now float64) map[string]any {
	events, err := store.QueryEvents(QueryEventsOptions{
		EventType:   "starter_slate_manifest",
		PolicyID:    stringPtr(POLICY_ID),
		Limit:       64,
		NewestFirst: true,
	})
	if err != nil {
		return staticPoolRead()
	}
	var manifests []map[string]any
	for _, event := range events {
		payload, _, isMap := payloadMap(event["payload"])
		if !isMap {
			continue
		}
		if !anyMapEqual(payload["serving_compatibility_key"], servingCompatibilityKey) {
			continue
		}
		if negativePreferenceVersionOf(payload) > negativePreferenceVersion {
			continue
		}
		merged := map[string]any{}
		for k, v := range payload {
			merged[k] = v
		}
		merged["_created_at"] = num(event["created_at"])
		manifests = append(manifests, merged)
	}
	if len(manifests) == 0 {
		return staticPoolRead()
	}
	var choices []map[string]any
	for _, manifest := range manifests {
		if negativePreferenceVersionOf(manifest) == negativePreferenceVersion {
			choices = append(choices, manifest)
		}
	}
	if len(choices) == 0 {
		choices = manifests
	}
	// 对齐 :509-515 max by (negative_preference_version, _created_at)
	manifest := choices[0]
	for _, candidate := range choices[1:] {
		cKey := [2]float64{float64(negativePreferenceVersionOf(candidate)), num(candidate["_created_at"])}
		mKey := [2]float64{float64(negativePreferenceVersionOf(manifest)), num(manifest["_created_at"])}
		if cKey[0] > mKey[0] || (cKey[0] == mKey[0] && cKey[1] > mKey[1]) {
			manifest = candidate
		}
	}
	manifestID := str(manifest["manifest_id"])
	batchID := str(manifest["generation_batch_id"])
	cardEvents, err := store.QueryEvents(QueryEventsOptions{
		EventType: "starter_card",
		SlateID:   stringPtr(manifestID),
		PolicyID:  stringPtr(POLICY_ID),
		Limit:     7,
	})
	if err != nil {
		return staticPoolRead()
	}
	cardsByRevision := map[string]map[string]any{}
	for _, event := range cardEvents {
		payload, _, isMap := payloadMap(event["payload"])
		if str(event["slate_id"]) != manifestID || !isMap {
			continue
		}
		if str(payload["generation_batch_id"]) != batchID {
			continue
		}
		cardsByRevision[str(payload["card_revision_id"])] = payload
	}
	cards := []map[string]any{}
	trendSeen := false
	for _, revision := range anySliceToStrings(manifest["card_revision_ids"]) {
		card := cardsByRevision[revision]
		if card == nil {
			continue
		}
		effectiveTime := num(card["effective_time"])
		expireTime := num(card["expire_time"])
		if !(effectiveTime <= now && now < expireTime) {
			continue
		}
		if !validPoolEvidence(card) {
			continue
		}
		if str(card["lane"]) == "fresh-trend" {
			if trendSeen {
				continue
			}
			trendSeen = true
		}
		candidateView := map[string]any{}
		for k, v := range card {
			candidateView[k] = v
		}
		candidateView["candidate_id"] = card["card_revision_id"]
		candidateView["persona"] = card["title"]
		candidateView["primary_query"] = card["prompt"]
		candidateView["fallback_query"] = card["title"]
		survivors, _ := ApplyNegativePreferences([]map[string]any{candidateView}, negativePreferences)
		if len(survivors) > 0 {
			cards = append(cards, card)
		}
	}
	cards = FilterDistinctPersonalizedCandidates(cards, 4)
	result := map[string]any{
		"manifest_id":                          manifestID,
		"generation_batch_id":                  batchID,
		"manifest_negative_preference_version": negativePreferenceVersionOf(manifest),
		"cards":                                cards,
	}
	for k, v := range ClassifyStarterSlate(cards) {
		result[k] = v
	}
	return result
}

// CurrentBackgroundGenerationAttemptID 从 ctx 取当前生成尝试 ID（对齐 :583-586）。
func CurrentBackgroundGenerationAttemptID(ctx context.Context) string {
	if v, ok := ctx.Value(generationAttemptIDKey{}).(string); ok {
		return v
	}
	return ""
}

// NewBackgroundGenerationCoordinator 构造（对齐 :607-621 __init__）。
func NewBackgroundGenerationCoordinator(store *TopicStarterStore, deadlineSeconds float64,
	poolWriter PoolWriterFunc, policyID string, auditLoader TerminalExternalAuditLoader) *BackgroundGenerationCoordinator {
	if poolWriter == nil {
		poolWriter = WriteStarterPool
	}
	if policyID == "" {
		policyID = POLICY_ID
	}
	return &BackgroundGenerationCoordinator{
		Store:                       store,
		DeadlineSeconds:             deadlineSeconds,
		poolWriter:                  poolWriter,
		policyID:                    policyID,
		terminalExternalAuditLoader: auditLoader,
		inflight:                    map[string]*backgroundRun{},
	}
}

// InflightCount 在飞任务数（对齐 :623-625）。
func (c *BackgroundGenerationCoordinator) InflightCount() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.inflight)
}

// Start 启动一次生成；restock 表示重新生成已写过的触发（对齐 :708-735）。
// 返回的 run 供调用方 Wait；同 token 在飞时返回既有 run。
func (c *BackgroundGenerationCoordinator) Start(ctx context.Context, triggerKey map[string]any,
	generate func(ctx context.Context) *BackgroundGenerationResult,
	arkConfig *ArkBackgroundConfig, requireArkConfig, bindPoolWriterModel, restock bool) *backgroundRun {
	token := coordinatorToken(triggerKey)
	c.mu.Lock()
	if existing, ok := c.inflight[token]; ok {
		c.mu.Unlock()
		return existing
	}
	run := &backgroundRun{done: make(chan struct{})}
	c.inflight[token] = run
	c.mu.Unlock()
	go func() {
		runCtx, cancel := context.WithCancel(ctx)
		defer close(run.done)
		run.outcome = c.run(runCtx, cancel, token, triggerKey, generate, arkConfig, requireArkConfig, bindPoolWriterModel, restock)
		c.mu.Lock()
		if c.inflight[token] == run {
			delete(c.inflight, token)
		}
		c.mu.Unlock()
	}()
	return run
}

// run 一次生成的执行体（对齐 :737-937 _run）。
func (c *BackgroundGenerationCoordinator) run(ctx context.Context, cancel context.CancelFunc,
	token string, triggerKey map[string]any,
	generate func(ctx context.Context) *BackgroundGenerationResult,
	arkConfig *ArkBackgroundConfig, requireArkConfig, bindPoolWriterModel, restock bool) string {
	defer cancel()
	generationAttemptID := CurrentBackgroundGenerationAttemptID(ctx)
	if generationAttemptID == "" {
		generationAttemptID = "background-attempt-" + newUUIDHex()
	}
	// 对齐 :751-752 已写过（restock 例外）
	if !restock && c.alreadyWritten(triggerKey) {
		return "already_written"
	}
	// 对齐 :753-759 需要后台模型却未配置
	if requireArkConfig && arkConfig == nil {
		c.recordGenerationEvent("background_model_unavailable", triggerKey, generationAttemptID, "", nil, nil, nil, nil)
		return "background_model_unavailable"
	}
	c.recordGenerationEvent("background_generation_started", triggerKey, generationAttemptID, "", nil, nil, nil, nil)
	// 对齐 :766-790 attempt id 注入 generate ctx + 总期限
	generateCtx := context.WithValue(ctx, generationAttemptIDKey{}, generationAttemptID)
	type genOutcome struct {
		result *BackgroundGenerationResult
	}
	generateDone := make(chan genOutcome, 1)
	go func() {
		defer func() {
			// 对齐 :780-781/:800-802 生成器 panic → failed
			if r := recover(); r != nil {
				generateDone <- genOutcome{result: nil}
			}
		}()
		generateDone <- genOutcome{result: generate(generateCtx)}
	}()
	var result *BackgroundGenerationResult
	timedOut := false
	deadlineTimer := time.NewTimer(time.Duration(c.DeadlineSeconds * float64(time.Second)))
	defer deadlineTimer.Stop()
	select {
	case out := <-generateDone:
		result = out.result
	case <-deadlineTimer.C:
		timedOut = true
	case <-ctx.Done():
		// 对齐 :908-925 取消路径
		c.recordGenerationEvent("background_generation_cancelled", triggerKey, generationAttemptID, "", nil, nil, nil, c.terminalExternalAudit(generationAttemptID))
		return "cancelled"
	}
	if timedOut {
		cancel()
		c.recordGenerationEvent("background_generation_deadline", triggerKey, generationAttemptID, "", nil, nil, nil, c.terminalExternalAudit(generationAttemptID))
		return "background_generation_deadline"
	}
	// 对齐 :791-802 生成失败
	if result == nil {
		c.recordGenerationEvent("background_generation_failed", triggerKey, generationAttemptID, "", nil, nil, nil, c.terminalExternalAudit(generationAttemptID))
		return "background_generation_failed"
	}
	// 对齐 :803-828 三段回执/审计提取
	manifest := result.Manifest
	groundingDecisionReceipt := optionalMap(manifest, "grounding_decision_receipt")
	groundingEvidenceReceipt := optionalMap(manifest, "grounding_evidence_receipt")
	externalInvocationAudit := optionalMap(manifest, "external_invocation_audit")
	if externalInvocationAudit == nil {
		externalInvocationAudit = c.terminalExternalAudit(generationAttemptID)
	}
	// 对齐 :829-839 后台模型不可用
	if result.FailureReason == "background_model_unavailable" {
		c.recordGenerationEvent("background_model_unavailable", triggerKey, generationAttemptID,
			result.FailureReason, groundingDecisionReceipt, groundingEvidenceReceipt, externalInvocationAudit, nil)
		return "background_model_unavailable"
	}
	// 对齐 :840-853 触发键不匹配
	if manifest == nil || !anyMapEqual(manifest["generation_trigger_key"], triggerKey) {
		c.recordGenerationEvent("background_generation_rejected", triggerKey, generationAttemptID,
			result.FailureReason, groundingDecisionReceipt, groundingEvidenceReceipt, externalInvocationAudit, nil)
		return "background_generation_rejected"
	}
	// 对齐 :854-864 池写入需绑定模型但未配置
	if bindPoolWriterModel && arkConfig == nil {
		reason := result.FailureReason
		if reason == "" {
			reason = "background_model_unavailable"
		}
		c.recordGenerationEvent("background_generation_rejected", triggerKey, generationAttemptID,
			reason, groundingDecisionReceipt, groundingEvidenceReceipt, externalInvocationAudit, nil)
		return "background_generation_rejected"
	}
	// 对齐 :865-875 弃权集合
	if result.FailureReason == "rid_abstained" || result.FailureReason == "quality_abstained" ||
		result.FailureReason == "no_eligible_history" || result.FailureReason == "unread_cards_kept" {
		c.recordGenerationEvent("background_generation_abstained", triggerKey, generationAttemptID,
			result.FailureReason, nil, nil, externalInvocationAudit, nil)
		return "abstained"
	}
	// 对齐 :876-906 池写入
	expectedModel := ""
	if bindPoolWriterModel {
		if arkConfig == nil {
			// 对齐 :881-882
			panic("pool writer binding requires a background config")
		}
		expectedModel = arkConfig.ExpectedModel
		if expectedModel == "" {
			expectedModel = arkConfig.Model
		}
	}
	writeResult := c.poolWriter(c.Store, result.Cards, manifest, nowFloat(), result.CorpusTextByID, expectedModel)
	if !writeResult {
		c.recordGenerationEvent("background_generation_rejected", triggerKey, generationAttemptID,
			result.FailureReason, groundingDecisionReceipt, groundingEvidenceReceipt, externalInvocationAudit,
			optionalMap(manifest, "rid_result"))
		return "background_generation_rejected"
	}
	return "written"
}

// alreadyWritten 触发键是否已写过清单（对齐 :631-650 _already_written）。
func (c *BackgroundGenerationCoordinator) alreadyWritten(triggerKey map[string]any) bool {
	filters := map[string]any{"generation_trigger_key": triggerKey}
	if _, hasScope := triggerKey["context_scope"]; hasScope {
		filters = map[string]any{"generation_trigger_key.context_scope": triggerKey["context_scope"]}
	}
	rows, err := c.Store.QueryEvents(QueryEventsOptions{
		EventType:     "starter_slate_manifest",
		PolicyID:      stringPtr(c.policyID),
		PayloadEquals: filters,
		Limit:         1,
		NewestFirst:   true,
	})
	if err != nil || len(rows) == 0 {
		return false
	}
	payload, _, isMap := payloadMap(rows[0]["payload"])
	if !isMap {
		return false
	}
	// 预置池与所代页共享触发键，但不是触发想要的个性化池，不关闭本轮
	if payload["cold_start"] == true {
		return false
	}
	return anyMapEqual(payload["generation_trigger_key"], triggerKey)
}

// terminalExternalAudit 审计加载（对齐 :652-659）。
func (c *BackgroundGenerationCoordinator) terminalExternalAudit(generationAttemptID string) map[string]any {
	if c.terminalExternalAuditLoader == nil {
		return nil
	}
	audit := c.terminalExternalAuditLoader(generationAttemptID)
	if _, isMapAny := any(audit).(map[string]any); !isMapAny {
		return nil
	}
	return audit
}

// recordGenerationEvent 记录生成事件（对齐 :661-706 record_generation_event）。
func (c *BackgroundGenerationCoordinator) recordGenerationEvent(eventType string, triggerKey map[string]any,
	generationAttemptID, reason string, groundingDecisionReceipt, groundingEvidenceReceipt,
	externalInvocationAudit, ridResult map[string]any) {
	eventID := newUUIDHex()
	payload := map[string]any{
		"generation_trigger_key": triggerKey,
		"generation_attempt_id":  generationAttemptID,
	}
	if reason != "" {
		payload["reason"] = reason
	}
	if groundingDecisionReceipt != nil {
		payload["grounding_decision_receipt"] = groundingDecisionReceipt
	}
	if groundingEvidenceReceipt != nil {
		payload["grounding_evidence_receipt"] = groundingEvidenceReceipt
	}
	if externalInvocationAudit != nil {
		payload["external_invocation_audit"] = externalInvocationAudit
	}
	if ridResult != nil {
		payload["rid_result"] = ridResult
	}
	_, _ = c.Store.RecordEvent(map[string]any{
		"session_id":        "topic-starter-background",
		"slate_id":          eventID,
		"event_type":        eventType,
		"topic_id":          eventID,
		"policy_id":         c.policyID,
		"selection_mode":    "background_generation",
		"propensity_reason": "not_estimated",
		"payload":           payload,
		"created_at":        nowFloat(),
	})
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// coordinatorToken 触发键的规范化 JSON 串（对齐 :627-629 _token sort_keys 紧凑）。
func coordinatorToken(triggerKey map[string]any) string {
	normalized := normalizeForStableJSON(triggerKey)
	data, err := json.Marshal(normalized)
	if err != nil {
		return fmt.Sprint(len(triggerKey))
	}
	return string(data)
}

// normalizeForStableJSON 递归按键排序展开 map（模拟 json.dumps sort_keys）。
func normalizeForStableJSON(value any) any {
	switch v := value.(type) {
	case map[string]any:
		keys := make([]string, 0, len(v))
		for k := range v {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		out := []any{}
		for _, k := range keys {
			out = append(out, k, normalizeForStableJSON(v[k]))
		}
		return out
	case []any:
		out := make([]any, len(v))
		for i, item := range v {
			out[i] = normalizeForStableJSON(item)
		}
		return out
	case map[string]string:
		converted := map[string]any{}
		for k, item := range v {
			converted[k] = item
		}
		return normalizeForStableJSON(converted)
	default:
		return value
	}
}

// newUUIDHex 32 位十六进制随机 ID（对齐 uuid.uuid4().hex）。
func newUUIDHex() string {
	b := make([]byte, 16)
	_, _ = crand.Read(b)
	return fmt.Sprintf("%x", b)
}

// nowFloat 当前 Unix 秒（小数，对齐 Python time.time()）。
func nowFloat() float64 {
	return float64(time.Now().UnixNano()) / 1e9
}

// hasSecretField 递归探测 api_key/authorization 键（对齐 :128-138 _has_secret_field）。
func hasSecretField(value any) bool {
	switch v := value.(type) {
	case map[string]any:
		for key, child := range v {
			normalized := strings.ToLower(key)
			if strings.Contains(normalized, "api_key") || normalized == "authorization" {
				return true
			}
			if hasSecretField(child) {
				return true
			}
		}
	case []any:
		for _, child := range v {
			if hasSecretField(child) {
				return true
			}
		}
	}
	return false
}

// validPoolEvidence 卡内证据束校验（对齐 :141-174 _valid_evidence）。
func validPoolEvidence(card map[string]any) bool {
	bundle, _, isMap := payloadMap(card["evidence_bundle"])
	if !isMap {
		return false
	}
	boundary, _, boundaryIsMap := payloadMap(bundle["content_boundary"])
	if !boundaryIsMap {
		return false
	}
	text, textIsString := boundary["text"].(string)
	start := boundary["start"]
	end := boundary["end"]
	if str(bundle["fetch_status"]) != "success" {
		return false
	}
	if strings.TrimSpace(str(bundle["source_url"])) == "" {
		return false
	}
	if strings.TrimSpace(str(bundle["source_kind"])) == "" {
		return false
	}
	if !textIsString || text == "" {
		return false
	}
	if !plainInt(start) || !plainInt(end) {
		return false
	}
	startN, endN := int(num(start)), int(num(end))
	if startN < 0 {
		return false
	}
	// 对齐 :167 字符长度语义 → rune
	if endN-startN != len([]rune(text)) {
		return false
	}
	freshness := str(card["freshness_class"])
	if freshness == "current_event" || freshness == "recent_development" {
		return strings.TrimSpace(str(bundle["published_at"])) != "" &&
			strings.TrimSpace(str(bundle["published_at_source"])) != ""
	}
	return true
}

// validPoolCard 卡片校验（对齐 :177-228 _valid_card）。
func validPoolCard(card map[string]any, corpusTextByID map[string]string) bool {
	cardKeys, keysOK := mapKeysOf(card)
	if card == nil || !keysOK || !mapContainsAll(cardKeys, poolCardRequiredFields) {
		return false
	}
	if str(card["policy_id"]) != POLICY_ID || str(card["payload_version"]) != PAYLOAD_VERSION {
		return false
	}
	promptVersions, _, pvIsMap := payloadMap(card["prompt_versions"])
	if !pvIsMap || !stringSetEqual(mapKeysOfMap(promptVersions), sortedPromptIDs()) {
		return false
	}
	if hasSecretField(card) || !validPoolEvidence(card) {
		return false
	}
	effective, effectiveOK := numericField(card["effective_time"])
	expire, expireOK := numericField(card["expire_time"])
	if !effectiveOK || !expireOK || effective >= expire {
		return false
	}
	freshness := str(card["freshness_class"])
	if freshness == "current_event" && expire-effective > 48*60*60 {
		return false
	}
	if freshness == "evergreen_corpus" {
		if expire-effective > 30*24*60*60 {
			return false
		}
		kind := str(mapValueOrEmpty(optionalMap(card, "evidence_bundle"), "source_kind"))
		if kind != "authoritative" && kind != "stable" {
			return false
		}
	}
	if str(card["origin"]) == "retrieved-fixed" {
		sourceID := str(card["source_starter_id"])
		if sourceID == "" || corpusTextByID == nil {
			return false
		}
		if corpusTextByID[sourceID] != str(card["title"]) {
			return false
		}
	}
	for _, field := range []string{"card_id", "card_revision_id", "generation_batch_id", "intent", "title", "prompt"} {
		if strings.TrimSpace(str(card[field])) == "" {
			return false
		}
	}
	return true
}

// validPoolManifest 清单校验（对齐 :231-312 _valid_manifest）。

func validPoolManifest(manifest map[string]any, cards []map[string]any) bool {
	manifestKeys, keysOK := mapKeysOf(manifest)
	if manifest == nil || !keysOK || !mapContainsAll(manifestKeys, poolManifestRequiredFields) {
		return false
	}
	if str(manifest["policy_id"]) != POLICY_ID || str(manifest["payload_version"]) != PAYLOAD_VERSION {
		return false
	}
	origin := str(manifest["trigger_origin"])
	if origin != "scheduled_cron" && origin != "lazy_seed" {
		return false
	}
	triggerKey, _, triggerIsMap := payloadMap(manifest["generation_trigger_key"])
	if !triggerIsMap || hasSecretField(manifest) {
		return false
	}
	triggerPromptVersions, _, tpIsMap := payloadMap(triggerKey["prompt_versions"])
	if !tpIsMap || !stringSetEqual(mapKeysOfMap(triggerPromptVersions), sortedPromptIDs()) {
		return false
	}
	if !anyMapEqual(manifest["serving_compatibility_key"], BuildServingCompatibilityKey(triggerKey)) {
		return false
	}
	if !anyEqual(manifest["negative_preference_version"], triggerKey["negative_preference_version"]) {
		return false
	}
	expectedModelProvenance := map[string]any{
		"model":                 triggerKey["ark_model"],
		"base_url":              triggerKey["ark_base_url"],
		"effective_temperature": triggerKey["effective_temperature"],
		"effective_top_p":       triggerKey["effective_top_p"],
		"context_window_tokens": triggerKey["context_window_tokens"],
	}
	if str(manifest["local_date"]) != str(triggerKey["local_date"]) {
		return false
	}
	for _, card := range cards {
		provenance, _, provIsMap := payloadMap(card["model_provenance"])
		if !provIsMap {
			return false
		}
		for key, value := range expectedModelProvenance {
			if !anyEqual(provenance[key], value) {
				return false
			}
		}
		if str(provenance["context_window_source"]) != "ARK_CONTEXT_WINDOW_TOKENS" {
			return false
		}
		if str(provenance["decoding_parameter_source"]) != "request_parameters" {
			return false
		}
		if !anyEqual(provenance["requested_temperature"], provenance["effective_temperature"]) ||
			!anyEqual(provenance["requested_top_p"], provenance["effective_top_p"]) {
			return false
		}
		if !anyMapEqual(card["prompt_versions"], triggerKey["prompt_versions"]) {
			return false
		}
		if !anyEqual(card["policy_id"], triggerKey["policy_id"]) ||
			!anyEqual(card["payload_version"], triggerKey["payload_version"]) ||
			!anyEqual(card["corpus_version"], triggerKey["corpus_version"]) ||
			!anyEqual(card["retrieval_config_version"], triggerKey["retrieval_config_version"]) {
			return false
		}
	}
	// 对齐 :298-312 排名唯一性 + 有序修订列表
	ranks := make([]int, 0, len(cards))
	for _, card := range cards {
		if !plainInt(card["selector_rank"]) {
			return false
		}
		rank := int(num(card["selector_rank"]))
		if rank < 1 {
			return false
		}
		ranks = append(ranks, rank)
	}
	if hasIntDuplicates(ranks) {
		return false
	}
	ordered := append([]map[string]any{}, cards...)
	sort.SliceStable(ordered, func(i, j int) bool {
		return int(num(ordered[i]["selector_rank"])) < int(num(ordered[j]["selector_rank"]))
	})
	revisions := make([]string, 0, len(ordered))
	for _, card := range ordered {
		revisions = append(revisions, str(card["card_revision_id"]))
	}
	if hasStringDuplicates(revisions) {
		return false
	}
	manifestRevisions := anySliceToStrings(manifest["card_revision_ids"])
	if !stringSliceEqual(revisions, manifestRevisions) {
		return false
	}
	for _, card := range cards {
		if str(card["generation_batch_id"]) != str(manifest["generation_batch_id"]) {
			return false
		}
	}
	return true
}

// isPersonalized 个性化卡判定（对齐 :365-371 _is_personalized）。
func isPersonalized(card map[string]any) bool {
	interestID := card["interest_summary_id"]
	memoryUnits := card["source_memory_unit_ids"]
	locators := card["source_locators"]
	anchor := card["identity_anchor"]
	if interestID == nil || memoryUnits == nil || locators == nil || anchor == nil {
		return false
	}
	if list, ok := memoryUnits.([]any); ok && len(list) == 0 {
		return false
	}
	if list, ok := locators.([]any); ok && len(list) == 0 {
		return false
	}
	return true
}

// staticPoolRead 静态空池兜底（对齐 :394-401 _static_read）。
func staticPoolRead() map[string]any {
	result := map[string]any{
		"manifest_id":                          nil,
		"generation_batch_id":                  nil,
		"manifest_negative_preference_version": nil,
		"cards":                                []map[string]any{},
	}
	for k, v := range ClassifyStarterSlate(nil) {
		result[k] = v
	}
	return result
}

// negativePreferenceVersionOf 负偏好版本（对齐 :463-464）。
func negativePreferenceVersionOf(item map[string]any) int {
	return int(num(item["negative_preference_version"]))
}

// sortedPromptIDs 排序后的 prompt ID 集合（用于集合比较）。
func sortedPromptIDs() []string {
	return []string{"i1", "i2", "i3", "i4", "i5"}
}

// mapKeysOf map 键列表（nil 返回 ok=false）。
func mapKeysOf(value any) ([]string, bool) {
	m, ok := value.(map[string]any)
	if !ok {
		return nil, false
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys, true
}

// mapKeysOfMap 已知 map 的键列表。
func mapKeysOfMap(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}

// mapContainsAll 键集合是否包含全部必需（对齐 Python set.issubset）。
func mapContainsAll(keys []string, required map[string]struct{}) bool {
	set := map[string]struct{}{}
	for _, k := range keys {
		set[k] = struct{}{}
	}
	for r := range required {
		if _, ok := set[r]; !ok {
			return false
		}
	}
	return true
}

// stringSetEqualByID 集合比较（ID 集合无序）。
func stringSetEqualByID(got []string, expected []string) bool {
	if len(got) != len(expected) {
		return false
	}
	set := map[string]struct{}{}
	for _, v := range got {
		set[v] = struct{}{}
	}
	for _, e := range expected {
		if _, ok := set[e]; !ok {
			return false
		}
	}
	return true
}

// numericField 数值字段转换（对齐 float(card["effective_time"]) try/except）。
func numericField(value any) (float64, bool) {
	switch v := value.(type) {
	case float64:
		return v, true
	case int:
		return float64(v), true
	case int64:
		return float64(v), true
	case string:
		var f float64
		if _, err := fmt.Sscanf(strings.TrimSpace(v), "%g", &f); err == nil {
			return f, true
		}
		return 0, false
	default:
		return 0, false
	}
}

// hasIntDuplicates int 切片去重判定。
func hasIntDuplicates(values []int) bool {
	set := map[int]struct{}{}
	for _, v := range values {
		if _, dup := set[v]; dup {
			return true
		}
		set[v] = struct{}{}
	}
	return false
}

// anySliceToStrings []any → []string（非串元素转 str）。
func anySliceToStrings(value any) []string {
	list, ok := value.([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(list))
	for _, item := range list {
		out = append(out, str(item))
	}
	return out
}

// optionalMap 取 dict 字段（非 dict 返回 nil，对齐 isinstance(result.manifest, dict) 守卫）。
func optionalMap(m map[string]any, key string) map[string]any {
	if m == nil {
		return nil
	}
	child, ok := m[key].(map[string]any)
	if !ok {
		return nil
	}
	return child
}

// anyEqual Python == 语义的相等判定：数值跨类型可比，map/切片走稳定 JSON 深比较。
func anyEqual(a, b any) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	af, aIsNum := toFloat(a)
	bf, bIsNum := toFloat(b)
	if aIsNum && bIsNum {
		return af == bf || (math.IsNaN(af) && math.IsNaN(bf))
	}
	switch a.(type) {
	case map[string]any, []any, map[string]string:
		return stableJSON(a) == stableJSON(b)
	}
	return a == b
}

// toFloat 数值统一为 float64（bool 非数值）。
func toFloat(value any) (float64, bool) {
	switch v := value.(type) {
	case float64:
		return v, true
	case int:
		return float64(v), true
	case int64:
		return float64(v), true
	default:
		return 0, false
	}
}

// anyMapEqual map 深相等（经稳定 JSON 比较）。
func anyMapEqual(a, b any) bool {
	am, aOK := a.(map[string]any)
	bm, bOK := b.(map[string]any)
	if aOK && bOK {
		return stringSliceEqual(
			[]string{stableJSON(am)},
			[]string{stableJSON(bm)},
		)
	}
	return anyEqual(a, b)
}

// stableJSON 紧凑稳定 JSON。
func stableJSON(value any) string {
	data, err := json.Marshal(normalizeForStableJSON(value))
	if err != nil {
		return ""
	}
	return string(data)
}

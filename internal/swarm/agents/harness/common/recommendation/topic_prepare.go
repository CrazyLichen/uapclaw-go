package recommendation

// topic_prepare.go 一比一对齐上游 topic_prepare.py（1193 行）。
//
// 预置卡池：后台编排 + 前台池服务。
// ⤴️ 31d 过渡缝：TopicHistoryEngine（:2863-3818）经 HistoryEngine 接口注入，
// fakeHistoryEngine 为过渡实现（用户 2026-11-13 拍板：31d 计划文档标注待回填，
// 真实类型回填后删除接口与 fake，规则 8）；topic_history 纯函数切片直接调用
// history_state_store.go 的 ⤴️31d 归位函数。

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"
)

// ──────────────────────────── 结构体 ────────────────────────────

// V5GenerationInputs 一次性冻结的状态、活跃增量与预生成曝光上下文
// （topic_prepare.py:112-118）。
type V5GenerationInputs struct {
	// State 冻结的 v4 状态
	State map[string]any
	// ActiveBehaviors 活跃行为
	ActiveBehaviors []CanonicalBehavior
	// DeltaBehaviors 增量行为
	DeltaBehaviors []CanonicalBehavior
	// GenerationInputSnapshot 生成输入快照
	GenerationInputSnapshot map[string]any
}

// StateMaintenanceReceiptError 冻结的 v4 状态无法构成生产 S1 小票
// （topic_prepare.py:123-128）。
type StateMaintenanceReceiptError struct {
	// Reason 失败原因码
	Reason string
}

// Error 实现 error 接口。
func (e *StateMaintenanceReceiptError) Error() string { return e.Reason }

// externalInvocationAttemptAudit 只在外部调用前持久化安全身份
// （topic_prepare.py:131-186）。
type externalInvocationAttemptAudit struct {
	store           *TopicStarterStore
	now             func() float64
	generationRunID string
	counts          map[string]int
	sequence        int
	// providerUnavailable 供调用方标记供应商故障（Python nonlocal 等价）
	providerUnavailable bool
}

// TopicPreparePipeline 可替换的五接口流水线；持久化留在包外
// （topic_prepare.py:571-604）。
type TopicPreparePipeline struct {
	store               *TopicStarterStore
	now                 func() float64
	recordsLoader       func() map[string][]map[string]any
	inputLoader         func() *V5GenerationInputs
	stageInvokerFactory func(*ArkBackgroundConfig) *ArkStageInvoker
	search              SearchFn
	fetch               FetchFn
	classifySource      SourceClassifier
	// historyEngine ⤴️31d 过渡缝（Python 无此参数：Python 直接调
	// TopicHistoryEngine；31d 落地后注入真实引擎）
	historyEngine HistoryEngine
}

// TopicPrepareService WebChannel 与定时 cron 共享的进程内单例服务
// （topic_prepare.py:971-991）。
type TopicPrepareService struct {
	store        *TopicStarterStore
	generate     func(ctx context.Context, triggerKey map[string]any, triggerOrigin string, config *ArkBackgroundConfig) (*BackgroundGenerationResult, error)
	prepareState func(ctx context.Context, config *ArkBackgroundConfig) string
	arkConfig    *ArkBackgroundConfig
	now          func() float64
	coordinator  *BackgroundGenerationCoordinator

	mu          sync.Mutex
	tasks       map[*backgroundTask]struct{}
	seedTask    *backgroundTask
	seedRunning bool
}

// backgroundTask 对齐 Python asyncio.Task 的最小承载。
type backgroundTask struct {
	done chan struct{}
}

// wait 等待任务完成。
func (t *backgroundTask) wait() { <-t.done }

// isDone 任务是否已完成。
func (t *backgroundTask) isDone() bool {
	select {
	case <-t.done:
		return true
	default:
		return false
	}
}

// ──────────────────────────── 常量 ────────────────────────────

const (
	// CurrentEventTTLSeconds current_event 保鲜 TTL（topic_prepare.py:74）
	CurrentEventTTLSeconds = 48 * 60 * 60
	// RecentDevelopmentTTLSeconds recent_development 保鲜 TTL（:75）
	RecentDevelopmentTTLSeconds = 7 * 24 * 60 * 60
	// EvergreenTTLSeconds 常青 TTL（:76）
	EvergreenTTLSeconds = 30 * 24 * 60 * 60
	// RecentExposureWindowSeconds 近期曝光窗口（:77）
	RecentExposureWindowSeconds = 7 * 24 * 60 * 60
	// StateMaintenanceReceiptVersion 状态维护小票版本（:78）
	StateMaintenanceReceiptVersion = "topic-starter-v6-state-maintenance-v1"
	// StateMaintenanceEventType 状态维护事件类型（:79）
	StateMaintenanceEventType = "state_maintenance_receipt"
	// StateMaintenancePolicyID 状态维护策略 ID（:80）
	StateMaintenancePolicyID = "topic-starter-state-v1"
	// ExternalInvocationAttemptEventType 外部调用尝试事件类型（:81）
	ExternalInvocationAttemptEventType = "external_invocation_attempt"
	// ExternalInvocationAttemptSchemaVersion 外部调用尝试 schema 版本（:82-84）
	ExternalInvocationAttemptSchemaVersion = "topic-starter-v5-external-invocation-attempt-v1"
	// ExternalInvocationAuditSchemaVersion 外部调用审计 schema 版本（:85-87）
	ExternalInvocationAuditSchemaVersion = "topic-starter-v5-external-invocation-audit-v1"
	// I1PromptVersion I1 逐字切片阶段提示词版本（:954）
	I1PromptVersion = "topic-starter-v5-i1-span-v1"
)

// stateMaintenanceReceiptFields 状态维护小票字段全集（:89-105）。
var stateMaintenanceReceiptFields = []string{
	"receipt_version", "receipt_id", "input_snapshot_id", "source_watermark",
	"history_revision", "resolved_mapping_id", "mapping_watermark",
	"v4_state_id", "v4_state_version", "profile_snapshot_id",
	"profile_semantic_version", "negative_preference_version", "refresh_reason",
}

// PromptVersions 五阶段提示词版本表（:955-961）。
var PromptVersions = map[string]string{
	"i1": I1PromptVersion,
	"i2": I2PromptVersion,
	"i3": I3RetrievalPromptVersion,
	"i4": I4PromptVersion,
	"i5": I5PromptVersion,
}

// ──────────────────────────── 导出函数 ────────────────────────────

// NewTopicPreparePipeline 构造（对齐 __init__ :574-604）。
// recordsLoader 为空时回退空记录表（Python 默认 history.load_all_session_records，
// Go 接线 ⤴️31d：会话历史读取随 31d 会话域落地）。
func NewTopicPreparePipeline(store *TopicStarterStore,
	inputLoader func() *V5GenerationInputs,
	recordsLoader func() map[string][]map[string]any,
	stageInvokerFactory func(*ArkBackgroundConfig) *ArkStageInvoker,
	search SearchFn, fetch FetchFn, classifySource SourceClassifier,
	now func() float64, historyEngine HistoryEngine) *TopicPreparePipeline {
	if now == nil {
		now = func() float64 { return float64(time.Now().UnixNano()) / 1e9 }
	}
	if recordsLoader == nil {
		// Python: records_loader or history.load_all_session_records（:588）
		recordsLoader = func() map[string][]map[string]any { return map[string][]map[string]any{} }
	}
	if inputLoader == nil {
		inputLoader = func() *V5GenerationInputs {
			return LoadGenerationInputs(store, now(), recordsLoader)
		}
	}
	if stageInvokerFactory == nil {
		// Python: partial(build_ark_stage_invoker, receipt_sink=partial(persist_provider_invocation, store))（:596-600）
		sink := func(receipt map[string]any) { PersistProviderInvocation(store, receipt) }
		stageInvokerFactory = func(config *ArkBackgroundConfig) *ArkStageInvoker {
			return BuildArkStageInvoker(config, NewDefaultChatClientFactory(), nil, false, sink, nil, nil, nil, false)
		}
	}
	if search == nil {
		search = DefaultSearch
	}
	if fetch == nil {
		fetch = DefaultFetch
	}
	if classifySource == nil {
		classifySource = DefaultSourceClassifier
	}
	if historyEngine == nil {
		historyEngine = NewFakeHistoryEngine(store)
	}
	return &TopicPreparePipeline{
		store:               store,
		now:                 now,
		recordsLoader:       recordsLoader,
		inputLoader:         inputLoader,
		stageInvokerFactory: stageInvokerFactory,
		search:              search,
		fetch:               fetch,
		classifySource:      classifySource,
		historyEngine:       historyEngine,
	}
}

// NewExternalInvocationAttemptAudit 构造外部调用尝试审计（:131-144）。
func NewExternalInvocationAttemptAudit(store *TopicStarterStore, now func() float64) *externalInvocationAttemptAudit {
	return &externalInvocationAttemptAudit{
		store:           store,
		now:             now,
		generationRunID: fmt.Sprintf("v5-generation-run-%s", newUUIDHex()),
		counts:          map[string]int{"ark": 0, "search": 0, "fetch": 0},
	}
}

// Record 记录一次外部调用尝试（:146-174）。
func (a *externalInvocationAttemptAudit) Record(operation, componentID string) string {
	if _, supported := a.counts[operation]; !supported || componentID == "" {
		panic("unsupported external invocation attempt")
	}
	a.sequence++
	attemptID := fmt.Sprintf("v5-external-attempt-%s", newUUIDHex())
	recorded, err := a.store.RecordEvent(map[string]any{
		"session_id":        a.generationRunID,
		"slate_id":          a.generationRunID,
		"event_type":        ExternalInvocationAttemptEventType,
		"topic_id":          attemptID,
		"policy_id":         POLICY_ID,
		"selection_mode":    "external_invocation_attempt",
		"propensity_reason": "not_estimated",
		"payload": map[string]any{
			"schema_version":    ExternalInvocationAttemptSchemaVersion,
			"generation_run_id": a.generationRunID,
			"attempt_id":        attemptID,
			"sequence":          a.sequence,
			"operation":         operation,
			"component_id":      componentID,
		},
		"created_at": a.now(),
	})
	if err != nil || !recorded {
		panic("external invocation attempt was not persisted")
	}
	a.counts[operation]++
	return attemptID
}

// Receipt 审计小票（:176-186）。
func (a *externalInvocationAttemptAudit) Receipt() map[string]any {
	total := 0
	for _, count := range a.counts {
		total += count
	}
	return map[string]any{
		"schema_version":     ExternalInvocationAuditSchemaVersion,
		"generation_run_id":  a.generationRunID,
		"policy_id":          POLICY_ID,
		"attempt_event_type": ExternalInvocationAttemptEventType,
		"counters": map[string]any{
			"total":  total,
			"ark":    a.counts["ark"],
			"search": a.counts["search"],
			"fetch":  a.counts["fetch"],
		},
	}
}

// BuildStateMaintenanceReceipt 构建状态维护小票（:233-242）。
func BuildStateMaintenanceReceipt(state map[string]any, refreshReason string) (map[string]any, error) {
	reason := str(refreshReason)
	if len([]rune(reason)) == 0 || len([]rune(reason)) != len([]rune(trimOrEmpty(refreshReason))) {
		// 对齐 Python: str(refresh_reason or "").strip() 判空
	}
	if trimOrEmpty(refreshReason) == "" {
		return nil, &StateMaintenanceReceiptError{Reason: "state_receipt_invalid"}
	}
	identity, err := stateMaintenanceIdentity(state)
	if err != nil {
		return nil, err
	}
	identity["refresh_reason"] = trimOrEmpty(refreshReason)
	return identity, nil
}

// PersistStateMaintenanceReceipt 幂等落库状态维护小票（:275-306）。
func PersistStateMaintenanceReceipt(store *TopicStarterStore, state map[string]any,
	refreshReason string, createdAt float64) (map[string]any, error) {
	if existing := loadStateMaintenanceReceipt(store, state); existing != nil {
		return existing, nil
	}
	receipt, err := BuildStateMaintenanceReceipt(state, refreshReason)
	if err != nil {
		return nil, err
	}
	_, _ = store.RecordEvent(map[string]any{
		"session_id":        "v4-state",
		"slate_id":          receipt["receipt_id"],
		"event_type":        StateMaintenanceEventType,
		"topic_id":          receipt["receipt_id"],
		"profile_version":   receipt["v4_state_version"],
		"policy_id":         StateMaintenancePolicyID,
		"selection_mode":    "state_maintenance",
		"propensity_reason": "not_estimated",
		"payload":           receipt,
		"created_at":        createdAt,
	})
	stored := loadStateMaintenanceReceipt(store, state)
	if stored == nil {
		return nil, &StateMaintenanceReceiptError{Reason: "state_receipt_persist_failed"}
	}
	return stored, nil
}

// LoadGenerationInputs 一次性冻结状态、活跃增量与预生成曝光上下文
// （topic_prepare.py:358-393）。
func LoadGenerationInputs(store *TopicStarterStore, now float64,
	recordsLoader func() map[string][]map[string]any) *V5GenerationInputs {
	state, ok := HistoryLoadLatestV4(store)
	if !ok {
		state = HistoryEmptyV4State()
	}
	if recordsLoader == nil {
		recordsLoader = func() map[string][]map[string]any { return map[string][]map[string]any{} }
	}
	records := recordsLoader()
	active := HistoryActiveBehaviorsFromRecords(mapAnyOf(state["history_ledger"]), records)
	cursor := intOf(mapAnyOf(state["memory_cursor"])["consumed_through_behavior_index"])
	delta := HistoryUncuratedMappedBehaviors(state, active)
	activeDeltaVersion := cursor
	for _, behavior := range active {
		if behavior.BehaviorIndex > activeDeltaVersion {
			activeDeltaVersion = behavior.BehaviorIndex
		}
	}
	stateReceipt := loadStateMaintenanceReceipt(store, state)
	generationInputSnapshot := map[string]any{
		"active_delta_version":    activeDeltaVersion,
		"external_memory_version": intOf(state["version"]),
		"recent_exposure_window":  recentExposureWindow(store, now),
	}
	if stateReceipt != nil {
		generationInputSnapshot["state_maintenance_receipt"] = stateReceipt
	}
	return &V5GenerationInputs{
		State:                   state,
		ActiveBehaviors:         active,
		DeltaBehaviors:          delta,
		GenerationInputSnapshot: generationInputSnapshot,
	}
}

// RefreshState 运行 v5 I1–I5 之前的共享历史/映射/记忆通道
// （topic_prepare.py:606-610）。
func (p *TopicPreparePipeline) RefreshState(ctx context.Context, config *ArkBackgroundConfig) string {
	audit := NewExternalInvocationAttemptAudit(p.store, p.now)
	return p.refreshState(ctx, config, audit)
}

// Generate 先刷新状态，成功后进入预生成；状态失败时以空结果回退
// （topic_prepare.py:690-715）。
func (p *TopicPreparePipeline) Generate(ctx context.Context, triggerKey map[string]any,
	triggerOrigin string, config *ArkBackgroundConfig) (*BackgroundGenerationResult, error) {
	audit := NewExternalInvocationAttemptAudit(p.store, p.now)
	stateReason := p.refreshState(ctx, config, audit)
	if stateReason == "" {
		return p.generatePrepared(ctx, triggerKey, triggerOrigin, config, audit)
	}
	inputs := p.inputLoader()
	snapshot := jsonRoundTrip(inputs.GenerationInputSnapshot)
	return emptyResult(triggerKey, triggerOrigin, snapshot, stateReason, audit.Receipt()), nil
}

// GeneratePrepared 对 refresh_state 冻结的状态运行 I1–I5
// （topic_prepare.py:717-730）。
func (p *TopicPreparePipeline) GeneratePrepared(ctx context.Context, triggerKey map[string]any,
	triggerOrigin string, config *ArkBackgroundConfig) (*BackgroundGenerationResult, error) {
	return p.generatePrepared(ctx, triggerKey, triggerOrigin, config,
		NewExternalInvocationAttemptAudit(p.store, p.now))
}

// NewTopicPrepareService 构造（对齐 __init__ :974-991）。
// configLoader 为空时默认 ArkBackgroundConfig.FromEnviron（os.Environ）。
func NewTopicPrepareService(store *TopicStarterStore,
	generate func(ctx context.Context, triggerKey map[string]any, triggerOrigin string, config *ArkBackgroundConfig) (*BackgroundGenerationResult, error),
	prepareState func(ctx context.Context, config *ArkBackgroundConfig) string,
	configLoader func() *ArkBackgroundConfig,
	now func() float64, coordinator *BackgroundGenerationCoordinator) *TopicPrepareService {
	if configLoader == nil {
		configLoader = func() *ArkBackgroundConfig { return FromEnviron(environMap()) }
	}
	if now == nil {
		now = func() float64 { return float64(time.Now().UnixNano()) / 1e9 }
	}
	if coordinator == nil {
		coordinator = NewBackgroundGenerationCoordinator(store, 0, nil, "", nil)
	}
	return &TopicPrepareService{
		store:        store,
		generate:     generate,
		prepareState: prepareState,
		arkConfig:    configLoader(),
		now:          now,
		coordinator:  coordinator,
		tasks:        map[*backgroundTask]struct{}{},
	}
}

// InflightCount 在飞任务数（:993-996）。
func (s *TopicPrepareService) InflightCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	seedRunning := 0
	if s.seedTask != nil && !s.seedTask.isDone() {
		seedRunning = 1
	}
	if s.coordinator.InflightCount() > seedRunning {
		return s.coordinator.InflightCount()
	}
	return seedRunning
}

// ScheduleSeed 仅唤醒的 Cron 回调：入队后立即返回，不等待 180 秒
// （topic_prepare.py:1100-1103）。
func (s *TopicPrepareService) ScheduleSeed() *backgroundTask {
	return s.startSeed("scheduled_cron", nil)
}

// ScheduleSuccessor 在 record_v5_interaction 接受 not_interested 后入队
// （topic_prepare.py:1105-1108）。
func (s *TopicPrepareService) ScheduleSuccessor() *backgroundTask {
	return s.startSeed("lazy_seed", nil)
}

// Serve 同步读取一份清单；未命中只调度后续工作（topic_prepare.py:1110-1181）。
func (s *TopicPrepareService) Serve(sessionID string) map[string]any {
	profileVersion, _, negativeVersion, negativePreferences := s.stateVersions()
	triggerKey := s.triggerKey()
	if s.arkConfig == nil {
		s.startSeed("lazy_seed", triggerKey)
		return map[string]any{
			"available":  false,
			"reason":     "background_model_unavailable",
			"session_id": sessionID,
			"policy_id":  POLICY_ID,
		}
	}

	served := ReadStarterPool(s.store,
		BuildServingCompatibilityKey(triggerKey),
		negativeVersion, negativePreferences, s.now())
	manifestNegativeVersion := served["manifest_negative_preference_version"]
	successorNeeded := manifestNegativeVersion != nil &&
		intOf(manifestNegativeVersion) < negativeVersion
	if successorNeeded || len(anyListOf(served["cards"])) == 0 {
		s.startSeed("lazy_seed", triggerKey)
	}
	cards := anyListOf(served["cards"])
	if len(cards) == 0 {
		return map[string]any{
			"available":  false,
			"reason":     "v5_pool_miss",
			"session_id": sessionID,
			"policy_id":  POLICY_ID,
		}
	}

	slateID := fmt.Sprintf("v5-slate-%s", newUUIDHex())
	topics := make([]any, 0, len(cards))
	for _, cardValue := range cards {
		card := mapAnyOf(cardValue)
		topics = append(topics, map[string]any{
			"id":                  card["card_revision_id"],
			"card_revision_id":    card["card_revision_id"],
			"generation_batch_id": served["generation_batch_id"],
			"interest_summary_id": card["interest_summary_id"],
			"source_starter_id":   card["source_starter_id"],
			"position":            intOf(card["selector_rank"]) - 1,
			"selector_rank":       intOf(card["selector_rank"]),
			"title":               card["title"],
			"prompt":              card["prompt"],
			"intent":              card["intent"],
			"grounding_mode":      "web_required",
		})
	}
	return map[string]any{
		"available":               true,
		"session_id":              sessionID,
		"slate_id":                slateID,
		"manifest_id":             served["manifest_id"],
		"generation_batch_id":     served["generation_batch_id"],
		"profile_version":         profileVersion,
		"policy_id":               POLICY_ID,
		"retrieval_health":        "eligible",
		"selection_mode":          served["selection_mode"],
		"slate_card_count":        served["slate_card_count"],
		"personalized_card_count": served["personalized_card_count"],
		"trend_card_count":        served["trend_card_count"],
		"topics":                  topics,
	}
}

// WaitForBackground 等待全部在飞任务（:1183-1186）。
func (s *TopicPrepareService) WaitForBackground() {
	s.mu.Lock()
	tasks := make([]*backgroundTask, 0, len(s.tasks))
	for task := range s.tasks {
		if !task.isDone() {
			tasks = append(tasks, task)
		}
	}
	s.mu.Unlock()
	for _, task := range tasks {
		task.wait()
	}
}

// Shutdown 取消全部在飞任务（Python task.cancel → Go context 取消；
// 过渡期任务不可取消的以等待完成替代，⤴️31d service 落地时统一）。
func (s *TopicPrepareService) Shutdown() {
	s.WaitForBackground()
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// stateMaintenanceIdentity 从状态抽取 S1 小票身份字段
// （topic_prepare.py:189-230）。
func stateMaintenanceIdentity(state map[string]any) (map[string]any, error) {
	ledger, ok := state["history_ledger"].(map[string]any)
	if !ok {
		return nil, &StateMaintenanceReceiptError{Reason: "state_unavailable"}
	}
	active := HistoryActiveLedgerBehaviors(ledger)
	if len(active) == 0 {
		return nil, &StateMaintenanceReceiptError{Reason: "state_unavailable"}
	}
	stateVersion := intOf(state["version"])
	if stateVersion <= 0 {
		return nil, &StateMaintenanceReceiptError{Reason: "state_unavailable"}
	}
	activeIndices := map[int]bool{}
	for _, item := range active {
		activeIndices[item.BehaviorIndex] = true
	}
	mapping := HistoryStateMappingEntries(state)
	for index := range activeIndices {
		if _, ok := mapping[index]; !ok {
			return nil, &StateMaintenanceReceiptError{Reason: "state_mapping_unavailable"}
		}
	}
	sourceWatermark := intOf(ledger["next_behavior_index"])
	if sourceWatermark < 1 {
		sourceWatermark = 1
	}
	sourceWatermark--
	maxIndex := 0
	for index := range activeIndices {
		if index > maxIndex {
			maxIndex = index
		}
	}
	if maxIndex > sourceWatermark {
		sourceWatermark = maxIndex
	}
	mappingWatermark := maxIndex
	historyRevision := intOf(ledger["history_revision"])
	inputSnapshotID := fmt.Sprintf("v4-state-input:%d:%d:%d", stateVersion, sourceWatermark, historyRevision)
	return map[string]any{
		"receipt_version":             StateMaintenanceReceiptVersion,
		"receipt_id":                  fmt.Sprintf("state-receipt:%s", inputSnapshotID),
		"input_snapshot_id":           inputSnapshotID,
		"source_watermark":            sourceWatermark,
		"history_revision":            historyRevision,
		"resolved_mapping_id":         fmt.Sprintf("v4-mapping:%d:%d", stateVersion, mappingWatermark),
		"mapping_watermark":           mappingWatermark,
		"v4_state_id":                 fmt.Sprintf("v4-state:%d", stateVersion),
		"v4_state_version":            stateVersion,
		"profile_snapshot_id":         fmt.Sprintf("profile-snapshot:%d", stateVersion),
		"profile_semantic_version":    intOf(mapAnyOf(state["memory"])["semantic_version"]),
		"negative_preference_version": intOf(mapAnyOf(state["memory_sidecar"])["negative_preference_version"]),
	}, nil
}

// loadStateMaintenanceReceipt 从事件账本读回已落库小票并做字段级校验
// （topic_prepare.py:245-272）。
func loadStateMaintenanceReceipt(store *TopicStarterStore, state map[string]any) map[string]any {
	identity, err := stateMaintenanceIdentity(state)
	if err != nil {
		return nil
	}
	receiptID := pyStr(identity["receipt_id"])
	rows, err := store.QueryEvents(QueryEventsOptions{
		EventType:   StateMaintenanceEventType,
		SlateID:     &receiptID,
		TopicID:     &receiptID,
		PolicyID:    &stateMaintenancePolicyIDPtr,
		Limit:       1,
		NewestFirst: true,
	})
	if err != nil || len(rows) != 1 {
		return nil
	}
	payload, ok := rows[0]["payload"].(map[string]any)
	if !ok {
		return nil
	}
	if len(payload) != len(stateMaintenanceReceiptFields) {
		return nil
	}
	for _, field := range stateMaintenanceReceiptFields {
		if _, present := payload[field]; !present {
			return nil
		}
	}
	for field, value := range identity {
		if !anyEqual(payload[field], value) {
			return nil
		}
	}
	if trimOrEmpty(pyStr(payload["refresh_reason"])) == "" {
		return nil
	}
	return payload
}

// recentExposureWindow 七天动态曝光窗口去重投影（topic_prepare.py:309-355）。
func recentExposureWindow(store *TopicStarterStore, now float64) []map[string]any {
	cutoff := now - RecentExposureWindowSeconds
	var exposed []map[string]any
	type exposureKey struct{ manifest, batch, revision string }
	seen := map[exposureKey]bool{}
	events, _ := store.QueryEvents(QueryEventsOptions{
		EventType:   "dynamic_exposure",
		PolicyID:    &poolPolicyIDPtr,
		Limit:       256,
		NewestFirst: true,
	})
	for _, event := range events {
		if toFloatOr(event["created_at"], 0.0) < cutoff {
			continue
		}
		payload, ok := event["payload"].(map[string]any)
		if !ok {
			continue
		}
		manifestID := pyStr(payload["manifest_id"])
		batchID := pyStr(payload["generation_batch_id"])
		for _, topicValue := range anyListOf(payload["topics"]) {
			topic, ok := topicValue.(map[string]any)
			if !ok {
				continue
			}
			revisionID := pyStr(topic["card_revision_id"])
			key := exposureKey{manifestID, batchID, revisionID}
			if manifestID == "" || batchID == "" || revisionID == "" || seen[key] {
				continue
			}
			card := FindExactPoolCard(store, manifestID, revisionID, batchID)
			if card == nil {
				continue
			}
			seen[key] = true
			exposed = append(exposed, map[string]any{
				"card_revision_id":  revisionID,
				"source_starter_id": card["source_starter_id"],
				"title":             card["title"],
				"intent":            card["intent"],
				"observed_at":       toFloatOr(event["created_at"], 0.0),
			})
		}
	}
	return exposed
}

// refreshState 内部状态刷新（topic_prepare.py:612-688）。
func (p *TopicPreparePipeline) refreshState(ctx context.Context, config *ArkBackgroundConfig, audit *externalInvocationAttemptAudit) string {
	// 对齐 :617-618 context_window_tokens None 判定
	if config == nil || config.ContextWindowTokens == 0 {
		return "context_limit_unverified"
	}
	baseInvoker, err := p.safeStageInvokerFactory(config)
	if err != nil {
		// 对齐 :621-622 供应商不可用 fail-closed
		return "background_model_unavailable"
	}
	// 对齐 :625-635 audited invoke_stage 闭包（record_validation 经包装结构体透传）
	invokeStage := func(invokeCtx context.Context, stage, systemPrompt, payload string) (any, error) {
		attemptID := audit.Record("ark", stage)
		invokeCtx = withProviderInvocationHostAttempt(invokeCtx, attemptID)
		result, invokeErr := baseInvoker.Invoke(invokeCtx, stage, systemPrompt, payload)
		if invokeErr != nil {
			audit.providerUnavailable = true
		}
		return result, invokeErr
	}

	// 对齐 :637-655 TopicHistoryEngine(...).refresh(state_only_for_v5=True)
	outcome := p.historyEngine.RefreshV5State(ctx, HistoryRefreshRequest{
		ModelName:       config.Model,
		ContextLimit:    ContextLimitInfo{Value: &config.ContextWindowTokens, Source: "ARK_CONTEXT_WINDOW_TOKENS"},
		ModelProvenance: config.SafeProvenance(),
		RecordsLoader:   p.recordsLoader,
		InvokeStage:     invokeStage,
		Now:             p.now,
	})
	if outcome.Status != "ok" {
		if audit.providerUnavailable {
			return "background_model_unavailable"
		}
		if outcome.Reason == "" {
			return "state_refresh_failed"
		}
		return outcome.Reason
	}
	state := outcome.State
	if state == nil {
		var ok bool
		state, ok = HistoryLoadLatestV4(p.store)
		if !ok {
			state = map[string]any{}
		}
	}
	ledger, ledgerOK := state["history_ledger"].(map[string]any)
	if ledgerOK && len(HistoryActiveLedgerBehaviors(ledger)) == 0 {
		return "no_eligible_history"
	}
	refreshReason := "idempotent_reuse"
	if outcome.MemoryOutcome == "success" && outcome.MemoryMode != "" {
		refreshReason = outcome.MemoryMode
	} else if outcome.RewriteDetected {
		refreshReason = "history_rewrite"
	} else if outcome.MappingBatches > 0 {
		refreshReason = "behavior_mapping"
	}
	if _, err := PersistStateMaintenanceReceipt(p.store, state, refreshReason, p.now()); err != nil {
		if receiptErr, ok := err.(*StateMaintenanceReceiptError); ok {
			return receiptErr.Reason
		}
		return "state_receipt_persist_failed"
	}
	return ""
}

// safeStageInvokerFactory 工厂异常 fail-closed（对齐 :619-622）。
func (p *TopicPreparePipeline) safeStageInvokerFactory(config *ArkBackgroundConfig) (inv *ArkStageInvoker, err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			inv, err = nil, fmt.Errorf("stage invoker factory failed")
		}
	}()
	return p.stageInvokerFactory(config), nil
}

// generatePrepared 对冻结状态运行 I1–I5（topic_prepare.py:732-952）。
func (p *TopicPreparePipeline) generatePrepared(ctx context.Context, triggerKey map[string]any,
	triggerOrigin string, config *ArkBackgroundConfig, audit *externalInvocationAttemptAudit) (*BackgroundGenerationResult, error) {
	inputs := p.inputLoader()
	snapshot := jsonRoundTrip(inputs.GenerationInputSnapshot)
	mergedTriggerKey := map[string]any{}
	for key, value := range triggerKey {
		mergedTriggerKey[key] = value
	}
	mergedTriggerKey["profile_semantic_version"] = intOf(mapAnyOf(inputs.State["memory"])["semantic_version"])

	failed := func(reason string) *BackgroundGenerationResult {
		return emptyResult(mergedTriggerKey, triggerOrigin, snapshot, reason, audit.Receipt())
	}
	baseInvoker, err := p.safeStageInvokerFactory(config)
	if err != nil {
		return failed("background_model_unavailable"), nil
	}
	invokeStage := func(invokeCtx context.Context, stage, systemPrompt, payload string) (any, error) {
		attemptID := audit.Record("ark", stage)
		invokeCtx = withProviderInvocationHostAttempt(invokeCtx, attemptID)
		result, invokeErr := baseInvoker.Invoke(invokeCtx, stage, systemPrompt, payload)
		if invokeErr != nil {
			audit.providerUnavailable = true
		}
		return result, invokeErr
	}

	// 对齐 :775-782 I1 逐字切片提议
	sourceMessages := prepareSourceMessages(inputs.ActiveBehaviors)
	spanProposals := HistoryExtractVerbatimSpans(ctx, sourceMessages, inputs.ActiveBehaviors, invokeStage)
	if audit.providerUnavailable {
		return failed("background_model_unavailable"), nil
	}
	// 对齐 :783-789 冻结锚点桶
	buckets := HistoryBuildAllowedGroundedAnchors(
		anySliceOfMaps(mapAnyOf(inputs.State["memory"])["units"]),
		inputs.ActiveBehaviors,
		HistoryStateMappingEntries(inputs.State),
		spanProposals,
		POLICY_ID,
	)
	// 对齐 :790-795 兴趣快照与反馈上下文
	interestSnapshot := prepareInterestSnapshot(inputs, buckets)
	interestIDs := make([]any, 0, len(interestSnapshot))
	for _, item := range interestSnapshot {
		interestIDs = append(interestIDs, item["interest_summary_id"])
	}
	feedbackContext := BuildFeedbackContext(p.store, interestIDs)
	feedbackContext["generation_input_snapshot"] = snapshot
	// 对齐 :796-805 I2 兴趣排序
	ranked, reason := RankInterestSummaries(ctx, interestSnapshot, prepareDeltaPayload(inputs), feedbackContext,
		func(apiCtx context.Context, payload map[string]any) (any, error) {
			return NewArkInterestRankingAdapter(invokeStage).Invoke(apiCtx, payload)
		})
	if audit.providerUnavailable {
		return failed("background_model_unavailable"), nil
	}
	if reason != "" {
		return failed(reason), nil
	}

	contextByID := map[string]map[string]any{}
	for _, item := range interestSnapshot {
		contextByID[pyStr(item["interest_summary_id"])] = item
	}
	for _, interest := range ranked {
		contextByID[interest.InterestSummaryID]["rank"] = interest.Rank
	}
	// 对齐 :812-824 I3 固定起始检索
	hits, retrievalErr := RetrieveFixedStartersSources(ctx,
		corpusRankedInterests(ranked, contextByID), CorpusVersion,
		NewArkFixedStarterRankingAdapter(invokeStage), RetrievalTopK)
	if retrievalErr != nil {
		if audit.providerUnavailable {
			return failed("background_model_unavailable"), nil
		}
		return failed("retrieval_contract_failed"), nil
	}
	fixedHits := make([]map[string]any, 0, len(hits))
	for _, hit := range hits {
		fixedHits = append(fixedHits, fixedHitPayload(hit, contextByID))
	}
	// 对齐 :825-836 I4 候选生成
	candidates, reason := GenerateStarterCandidates(ctx, ranked, fixedHits, feedbackContext,
		len(ranked) == 0,
		func(apiCtx context.Context, payload map[string]any) (any, error) {
			return NewArkCandidateGenerationAdapter(invokeStage).Invoke(apiCtx, payload)
		}, OversampleSize, contextByID)
	if audit.providerUnavailable {
		return failed("background_model_unavailable"), nil
	}
	if reason != "" {
		return failed(reason), nil
	}

	// 对齐 :838-846 近期曝光过滤与负偏好
	candidates = notRecentlyExposed(candidates, anySliceOfMaps(snapshot["recent_exposure_window"]))
	negativePreferences := anyListOf(mapAnyOf(inputs.State["memory_sidecar"])["negative_preferences"])
	negativePreferenceMaps := []map[string]any{}
	for _, item := range negativePreferences {
		negativePreferenceMaps = append(negativePreferenceMaps, mapAnyOf(item))
	}
	candidates, _ = ApplyNegativePreferences(candidates, negativePreferenceMaps)

	// 对齐 :848-869 带审计的检索与抓取
	auditedSearch := func(searchCtx context.Context, query string, maxResults, timeoutSeconds int) (string, []map[string]string, error) {
		audit.Record("search", "live_retrieval.search")
		return p.search(searchCtx, query, maxResults, timeoutSeconds)
	}
	auditedFetch := func(fetchCtx context.Context, target string, timeoutSeconds int) (map[string]any, error) {
		audit.Record("fetch", "live_retrieval.fetch")
		return p.fetch(fetchCtx, target, timeoutSeconds)
	}
	grounded := GroundCandidates(ctx, candidates, auditedSearch, auditedFetch, p.classifySource, p.now())

	// 对齐 :870-884 I5 终局选择
	selections, reason := SelectStarterSlate(ctx, grounded, map[string]any{
		"trend_max":                              1,
		"personalized_interest_buckets_distinct": true,
		"personalized_identity_anchors_distinct": true,
		"require_retrieved_fixed_when_eligible":  true,
		"feedback_context":                       feedbackContext,
	}, func(apiCtx context.Context, payload map[string]any) (any, error) {
		return NewArkSlateSelectionAdapter(invokeStage).Invoke(apiCtx, payload)
	}, SlateSelectionTargetCount)
	if audit.providerUnavailable {
		return failed("background_model_unavailable"), nil
	}
	if reason != "" || len(selections) == 0 {
		if reason == "" {
			reason = "no_eligible_selection"
		}
		return failed(reason), nil
	}

	// 对齐 :886-927 卡片组装
	groundedByID := map[string]map[string]any{}
	for _, candidate := range grounded {
		groundedByID[pyStr(candidate["candidate_id"])] = candidate
	}
	now := p.now()
	batchID := fmt.Sprintf("v5-batch-%s", newUUIDHex())
	cards := make([]map[string]any, 0, len(selections))
	for _, selection := range selections {
		candidate := groundedByID[selection.CandidateID]
		interestID := candidate["interest_summary_id"]
		interestContext := contextByID[pyStr(interestID)]
		if interestContext == nil {
			interestContext = map[string]any{}
		}
		freshnessClass := pyStr(candidate["freshness_class"])
		cards = append(cards, map[string]any{
			"card_id":                  pyStr(candidate["candidate_id"]),
			"card_revision_id":         fmt.Sprintf("v5-revision-%s", newUUIDHex()),
			"generation_batch_id":      batchID,
			"selector_rank":            selection.SelectorRank,
			"source_starter_id":        candidate["source_starter_id"],
			"interest_summary_id":      interestID,
			"interest_rank":            interestContext["rank"],
			"observed_at":              now,
			"lane":                     candidate["lane"],
			"origin":                   candidate["origin"],
			"intent":                   candidate["intent"],
			"title":                    candidate["title"],
			"prompt":                   candidate["title"],
			"freshness_class":          freshnessClass,
			"effective_time":           now,
			"expire_time":              now + float64(ttlSeconds(freshnessClass)),
			"evidence_bundle":          candidate["evidence_bundle"],
			"source_memory_unit_ids":   anySliceCopy(candidate["source_memory_unit_ids"]),
			"source_locators":          anySliceCopy(candidate["source_locators"]),
			"identity_anchor":          candidate["identity_anchor"],
			"context_anchors":          anySliceCopy(candidate["context_anchors"]),
			"model_provenance":         config.SafeProvenance(),
			"prompt_versions":          mapCopyAny(mapAnyOf(mergedTriggerKey["prompt_versions"])),
			"policy_id":                POLICY_ID,
			"payload_version":          PAYLOAD_VERSION,
			"retrieval_config_version": mergedTriggerKey["retrieval_config_version"],
			"corpus_version":           mergedTriggerKey["corpus_version"],
		})
	}

	// 对齐 :929-952 清单与语料回填
	manifestID := fmt.Sprintf("v5-manifest-%s", newUUIDHex())
	manifest := map[string]any{
		"manifest_id":                 manifestID,
		"generation_batch_id":         batchID,
		"card_revision_ids":           cardRevisionIDs(cards),
		"trigger_origin":              triggerOrigin,
		"local_date":                  mergedTriggerKey["local_date"],
		"generation_trigger_key":      mergedTriggerKey,
		"generation_input_snapshot":   snapshot,
		"serving_compatibility_key":   BuildServingCompatibilityKey(mergedTriggerKey),
		"negative_preference_version": mergedTriggerKey["negative_preference_version"],
		"policy_id":                   POLICY_ID,
		"payload_version":             PAYLOAD_VERSION,
		"external_invocation_audit":   audit.Receipt(),
	}
	starters, corpusErr := LoadFixedStarterCorpus(CorpusVersion)
	if corpusErr != nil {
		// 对齐 Python: 异常向调用方传播（coordinator 任务 failed）
		return nil, corpusErr
	}
	corpusTextByID := map[string]string{}
	for _, starter := range starters {
		corpusTextByID[starter.StarterID] = starter.Text
	}
	return &BackgroundGenerationResult{
		Cards:          cards,
		Manifest:       manifest,
		CorpusTextByID: corpusTextByID,
	}, nil
}

// stateVersions 读状态版本四元组（topic_prepare.py:998-1009）。
func (s *TopicPrepareService) stateVersions() (int, int, int, []map[string]any) {
	state, ok := HistoryLoadLatestV4(s.store)
	if !ok {
		state = map[string]any{}
	}
	memory := mapAnyOf(state["memory"])
	sidecar := mapAnyOf(state["memory_sidecar"])
	var negativePreferences []map[string]any
	for _, item := range anyListOf(sidecar["negative_preferences"]) {
		negativePreferences = append(negativePreferences, mapAnyOf(item))
	}
	return intOf(state["version"]), intOf(memory["semantic_version"]),
		intOf(sidecar["negative_preference_version"]), negativePreferences
}

// triggerKey 构造当前触发键（topic_prepare.py:1011-1036）。
func (s *TopicPrepareService) triggerKey() map[string]any {
	_, profileSemanticVersion, negativeVersion, _ := s.stateVersions()
	config := s.arkConfig
	effectiveTemperature := 0.0
	effectiveTopP := 1.0
	model := ""
	baseURL := ""
	contextWindowTokens := 0
	if config != nil {
		model = config.Model
		baseURL = config.BaseURL
		effectiveTemperature = config.EffectiveTemperature()
		effectiveTopP = config.EffectiveTopP()
		contextWindowTokens = config.ContextWindowTokens
	}
	return BuildGenerationTriggerKey(localDateString(s.now()), profileSemanticVersion, negativeVersion,
		model, baseURL, effectiveTemperature, effectiveTopP, contextWindowTokens,
		promptVersionsAsAny(), CorpusVersion, RetrievalConfigVersion)
}

// startSeed 启动一次种子任务（topic_prepare.py:1043-1098）。
func (s *TopicPrepareService) startSeed(triggerOrigin string, triggerKey map[string]any) *backgroundTask {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := triggerKey
	if key == nil {
		key = s.triggerKey()
	}
	config := s.arkConfig

	if s.prepareState == nil || config == nil {
		generate := func(ctx context.Context) *BackgroundGenerationResult {
			if config == nil {
				// 对齐 :1053-1054 coordinator 在调用前已拒绝
				panic("background model unavailable")
			}
			result, err := s.generate(ctx, key, triggerOrigin, config)
			if err != nil {
				return &BackgroundGenerationResult{Manifest: map[string]any{"generation_trigger_key": key}, FailureReason: err.Error()}
			}
			return result
		}
		run := s.coordinator.Start(context.Background(), key, generate, config, true, false, false)
		return s.trackLocked(newTaskFromRun(run))
	}

	if s.seedTask != nil && !s.seedTask.isDone() {
		return s.seedTask
	}
	task := &backgroundTask{done: make(chan struct{})}
	s.seedTask = task
	s.trackLocked(task)
	go func() {
		defer close(task.done)
		defer func() {
			s.mu.Lock()
			if s.seedTask == task {
				s.seedTask = nil
			}
			s.mu.Unlock()
		}()
		// 对齐 :1068-1088 prepare_then_generate
		stateReason := s.prepareState(context.Background(), config)
		refreshedKey := s.triggerKey()
		generate := func(ctx context.Context) *BackgroundGenerationResult {
			if stateReason != "" {
				return &BackgroundGenerationResult{
					Manifest:      map[string]any{"generation_trigger_key": refreshedKey},
					FailureReason: stateReason,
				}
			}
			result, err := s.generate(ctx, refreshedKey, triggerOrigin, config)
			if err != nil {
				return &BackgroundGenerationResult{
					Manifest:      map[string]any{"generation_trigger_key": refreshedKey},
					FailureReason: err.Error(),
				}
			}
			return result
		}
		run := s.coordinator.Start(context.Background(), refreshedKey, generate, config, true, false, false)
		run.Wait()
	}()
	return task
}

// trackLocked 登记任务（对齐 _track :1038-1041；调用方须持锁）。
func (s *TopicPrepareService) trackLocked(task *backgroundTask) *backgroundTask {
	s.tasks[task] = struct{}{}
	go func() {
		task.wait()
		s.mu.Lock()
		delete(s.tasks, task)
		s.mu.Unlock()
	}()
	return task
}

// newTaskFromRun 把 coordinator 的 run 包装成可等待任务。
func newTaskFromRun(run *backgroundRun) *backgroundTask {
	task := &backgroundTask{done: make(chan struct{})}
	go func() {
		run.Wait()
		close(task.done)
	}()
	return task
}

// prepareSourceMessages 源消息投影（topic_prepare.py:396-406）。
func prepareSourceMessages(active []CanonicalBehavior) []map[string]any {
	messages := make([]map[string]any, 0, len(active))
	for _, behavior := range active {
		messages = append(messages, map[string]any{
			"behavior_id":    behavior.BehaviorIndex,
			"source_locator": []any{behavior.SessionID, behavior.Ordinal},
			"content":        behavior.Content,
		})
	}
	return messages
}

// prepareInterestSnapshot 兴趣快照投影（topic_prepare.py:409-441）。
func prepareInterestSnapshot(inputs *V5GenerationInputs, buckets map[string]AnchorBucket) []map[string]any {
	units := map[string]map[string]any{}
	for _, unitValue := range anyListOf(mapAnyOf(inputs.State["memory"])["units"]) {
		unit := mapAnyOf(unitValue)
		units[pyStr(mapAnyOf(unit["companion"])["unit_id"])] = unit
	}
	deltaContents := make([]string, 0, len(inputs.DeltaBehaviors))
	for _, behavior := range inputs.DeltaBehaviors {
		deltaContents = append(deltaContents, behavior.Content)
	}
	var snapshot []map[string]any
	for interestID, bucket := range buckets {
		if len(bucket.Anchors) == 0 || len(bucket.Locators) == 0 {
			continue
		}
		unit := units[interestID]
		if unit == nil {
			unit = map[string]any{}
		}
		summary := pyStr(mapAnyOf(unit["core"])["preference_summary"])
		if trimOrEmpty(pyStr(mapAnyOf(unit["core"])["preference_summary"])) == "" {
			summary = strings.TrimSpace(bucket.Anchors[0])
		}
		supportingDelta := false
		for _, content := range deltaContents {
			for _, anchor := range bucket.Anchors {
				if strings.Contains(content, anchor) {
					supportingDelta = true
				}
			}
		}
		provenanceRelation := "stable_progress"
		if supportingDelta {
			provenanceRelation = "recent_follow_up"
		}
		anchorList := make([]any, 0, len(bucket.Anchors))
		for _, anchor := range bucket.Anchors {
			anchorList = append(anchorList, anchor)
		}
		locators := make([]any, 0, len(bucket.Locators))
		for _, locator := range bucket.Locators {
			locators = append(locators, []any{locator.SessionID, locator.Ordinal})
		}
		snapshot = append(snapshot, map[string]any{
			"interest_summary_id":    interestID,
			"interest_summary":       summary,
			"source_memory_unit_ids": []any{interestID},
			"source_locators":        locators,
			"allowed_anchors":        anchorList,
			"provenance_relation":    provenanceRelation,
		})
	}
	return snapshot
}

// prepareDeltaPayload 增量载荷投影（topic_prepare.py:444-456）。
func prepareDeltaPayload(inputs *V5GenerationInputs) []map[string]any {
	mapping := HistoryStateMappingEntries(inputs.State)
	payload := make([]map[string]any, 0, len(inputs.DeltaBehaviors))
	for _, behavior := range inputs.DeltaBehaviors {
		tag := any(nil)
		if entry, ok := mapping[behavior.BehaviorIndex]; ok {
			tag = entry["tag"]
		}
		payload = append(payload, map[string]any{
			"behavior_id":    behavior.BehaviorIndex,
			"content":        behavior.Content,
			"source_locator": []any{behavior.SessionID, behavior.Ordinal},
			"tag":            tag,
		})
	}
	return payload
}

// corpusRankedInterests 排名兴趣 → 语料检索兴趣（topic_prepare.py:459-478）。
func corpusRankedInterests(ranked []RankedInterest, contextByID map[string]map[string]any) []CorpusRankedInterest {
	result := make([]CorpusRankedInterest, 0, len(ranked))
	for _, interest := range ranked {
		context := contextByID[interest.InterestSummaryID]
		anchors := make([]string, 0, len(anyListOf(context["allowed_anchors"])))
		for _, anchor := range anyListOf(context["allowed_anchors"]) {
			anchors = append(anchors, pyStr(anchor))
		}
		locators := make([]SourceLocator, 0)
		for _, locatorValue := range anyListOf(context["source_locators"]) {
			locator, ok := locatorValue.([]any)
			if !ok || len(locator) != 2 {
				continue
			}
			locators = append(locators, SourceLocator{SessionID: pyStr(locator[0]), Ordinal: intOf(locator[1])})
		}
		result = append(result, CorpusRankedInterest{
			InterestSummaryID: interest.InterestSummaryID,
			InterestSummary:   pyStr(context["interest_summary"]),
			Rank:              interest.Rank,
			AllowedAnchors:    anchors,
			SourceLocators:    locators,
		})
	}
	return result
}

// fixedHitPayload 命中 → 固定命中载荷（topic_prepare.py:481-509）。
func fixedHitPayload(hit StarterHit, contextByID map[string]map[string]any) map[string]any {
	if hit.InterestSummaryID == nil {
		return map[string]any{
			"starter_id":             hit.StarterID,
			"text":                   hit.Text,
			"interest_summary_id":    nil,
			"source_memory_unit_ids": []any{},
			"source_locators":        []any{},
			"identity_anchor":        "",
			"context_anchors":        []any{},
			"freshness_class":        hit.FreshnessClass,
		}
	}
	context := contextByID[pyStr(hit.InterestSummaryID)]
	identity := pyStr(hit.MatchedAnchor)
	locators := make([]any, 0, len(hit.SourceLocators))
	for _, locator := range hit.SourceLocators {
		locators = append(locators, []any{locator.SessionID, locator.Ordinal})
	}
	contextAnchors := []any{}
	for _, anchor := range anyListOf(context["allowed_anchors"]) {
		if pyStr(anchor) != identity {
			contextAnchors = append(contextAnchors, anchor)
		}
	}
	return map[string]any{
		"starter_id":             hit.StarterID,
		"text":                   hit.Text,
		"interest_summary_id":    hit.InterestSummaryID,
		"source_memory_unit_ids": anySliceCopy(context["source_memory_unit_ids"]),
		"source_locators":        locators,
		"identity_anchor":        identity,
		"context_anchors":        contextAnchors,
		"freshness_class":        hit.FreshnessClass,
	}
}

// recentlyExposed 候选是否命中近期曝光窗口（topic_prepare.py:512-523）。
func recentlyExposed(candidate map[string]any, titles, sourceIDs map[string]bool) bool {
	if titles[pyStr(candidate["title"])] {
		return true
	}
	sourceStarterID := candidate["source_starter_id"]
	if sourceStarterID == nil || pyStr(sourceStarterID) == "" {
		return false
	}
	return sourceIDs[pyStr(sourceStarterID)]
}

// notRecentlyExposed 过滤近期曝光候选（topic_prepare.py:526-536）。
func notRecentlyExposed(candidates []map[string]any, window []map[string]any) []map[string]any {
	titles := map[string]bool{}
	sourceIDs := map[string]bool{}
	for _, item := range window {
		titles[pyStr(item["title"])] = true
		sourceIDs[pyStr(item["source_starter_id"])] = true
	}
	result := []map[string]any{}
	for _, candidate := range candidates {
		if !recentlyExposed(candidate, titles, sourceIDs) {
			result = append(result, candidate)
		}
	}
	return result
}

// ttlSeconds 保鲜等级 → TTL 秒（topic_prepare.py:539-544）。
func ttlSeconds(freshnessClass string) int {
	switch freshnessClass {
	case "current_event":
		return CurrentEventTTLSeconds
	case "recent_development":
		return RecentDevelopmentTTLSeconds
	default:
		return EvergreenTTLSeconds
	}
}

// emptyResult 失败空结果（topic_prepare.py:547-568）。
func emptyResult(triggerKey map[string]any, triggerOrigin string,
	generationInputSnapshot map[string]any, reason string, externalInvocationAudit map[string]any) *BackgroundGenerationResult {
	manifest := map[string]any{
		"generation_trigger_key":    triggerKey,
		"trigger_origin":            triggerOrigin,
		"generation_input_snapshot": generationInputSnapshot,
	}
	if externalInvocationAudit != nil {
		manifest["external_invocation_audit"] = externalInvocationAudit
	}
	return &BackgroundGenerationResult{
		Cards:         []map[string]any{},
		Manifest:      manifest,
		FailureReason: reason,
	}
}

// anySliceOfMaps []any → []map[string]any（跳过非映射元素对齐 Python isinstance 守卫）。
func anySliceOfMaps(value any) []map[string]any {
	result := []map[string]any{}
	for _, item := range anyListOf(value) {
		if m, ok := item.(map[string]any); ok {
			result = append(result, m)
		}
	}
	return result
}

// jsonRoundTrip 对齐 Python json.loads(json.dumps(..., ensure_ascii=False)) 深拷贝。
func jsonRoundTrip(value map[string]any) map[string]any {
	raw, err := json.Marshal(value)
	if err != nil {
		return map[string]any{}
	}
	decoded := map[string]any{}
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return map[string]any{}
	}
	return decoded
}

// mapCopyAny map 深浅拷贝（一层）。
func mapCopyAny(source map[string]any) map[string]any {
	result := make(map[string]any, len(source))
	for key, value := range source {
		result[key] = value
	}
	return result
}

// cardRevisionIDs 卡片修订 ID 列表。
func cardRevisionIDs(cards []map[string]any) []any {
	ids := make([]any, 0, len(cards))
	for _, card := range cards {
		ids = append(ids, card["card_revision_id"])
	}
	return ids
}

// trimOrEmpty Python str(value or "").strip() 等价。
func trimOrEmpty(value string) string {
	return strings.TrimSpace(value)
}

// localDateString 本地日期（time.strftime("%Y-%m-%d") 等价）。
func localDateString(now float64) string {
	return time.Unix(int64(now), 0).Format("2006-01-02")
}

// stateMaintenancePolicyIDPtr / poolPolicyIDPtr QueryEventsOptions 指针参数载体。
var (
	stateMaintenancePolicyIDPtr = StateMaintenancePolicyID
	poolPolicyIDPtr             = POLICY_ID
)

// promptVersionsAsAny 提示词版本表 → any 载荷。
func promptVersionsAsAny() map[string]any {
	result := make(map[string]any, len(PromptVersions))
	for key, value := range PromptVersions {
		result[key] = value
	}
	return result
}

// SlateSelectionTargetCount I5 终局选择目标数（topic_generation.py:505 默认 target_count=4）。
const SlateSelectionTargetCount = 4

// environMap 进程环境表（对齐 Python os.environ）。
func environMap() map[string]string {
	environ := map[string]string{}
	for _, entry := range environList() {
		for i := 0; i < len(entry); i++ {
			if entry[i] == '=' {
				environ[entry[:i]] = entry[i+1:]
				break
			}
		}
	}
	return environ
}

// environList 进程环境原始条目。
func environList() []string { return os.Environ() }

// withProviderInvocationHostAttempt 注入 host_attempt_id 调用身份
// （Python provider_invocation_context(host_attempt_id=...) topic_model.py:424-429）。
func withProviderInvocationHostAttempt(ctx context.Context, attemptID string) context.Context {
	identities := map[string]string{}
	if existing, ok := ctx.Value(providerInvocationContextKey{}).(map[string]string); ok {
		for key, value := range existing {
			identities[key] = value
		}
	}
	identities["host_attempt_id"] = attemptID
	return context.WithValue(ctx, providerInvocationContextKey{}, identities)
}

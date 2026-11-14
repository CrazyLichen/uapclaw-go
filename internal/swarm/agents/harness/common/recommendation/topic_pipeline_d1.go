package recommendation

// topic_pipeline_d1.go 一比一对齐上游 topic_pipeline.py:4481-4622 的
// TopicPipeline 类骨架：构造、完结记录投影、search_provider_id、
// refresh_state 与 scope_has_pending_request；以及
// session_history.py:286-301 的 project_completed_session_records
// （⤴️31d 归位会话域）。
//
// 已知偏差（§5 登记）：
// - 默认 records_loader=history.load_all_session_records（topic_history.py:257
//   属 31d）：nil 时回退空表，随 31d 引擎接线回填；
// - 默认 news_feed=BingNewsFeed / hot_list=HotListFeed（topic_news.py 31c
//   剩余）与 web_search_provider_factory=build_topic_search_provider
//   （topic_search.py 31c 剩余）：nil 时保持 nil 并在消费点 fail-loud，
//   随 31c 落地回填；
// - asyncio.Lock → sync.Mutex（§5 并发映射）。

import (
	"context"
	"os"
	"strings"
	"sync"
	"time"

	swarmsession "github.com/uapclaw/uapclaw-go/internal/swarm/server/session"
)

// ──────────────────────────── 常量 ────────────────────────────

// INJECTEDSearchAuditProviderID 注入搜索的审计厂商 ID（topic_pipeline.py:212）
const INJECTEDSearchAuditProviderID = "injected_test_double"

// OpenjiuwenNativeSearchProviderId 原生自由搜索 lane 身份（topic_search.py:24）
const OpenjiuwenNativeSearchProviderId = "openjiuwen-native-free-search"

// LegacyOpenjiuwenNativeSearchProviderId 旧 lane 身份（topic_search.py:34）
const LegacyOpenjiuwenNativeSearchProviderId = "openjiuwen-native-search"

// ──────────────────────────── 结构体 ────────────────────────────

// OpenJiuwenNativeSearchContractError 原生搜索契约错误（topic_search.py:79）。
type OpenJiuwenNativeSearchContractError struct {
	// Reason 契约失败码
	Reason string
}

// Error 实现 error 接口。
func (e *OpenJiuwenNativeSearchContractError) Error() string { return e.Reason }

// HotListFeedInterface 热榜拉取缝（topic_news.py:383 HotListFeed 的消费面；
// ⤴️31c：topic_news 主体落地真实类型后按规则 8 收敛）。
type HotListFeedInterface interface {
	// CachedPull 复用窗口内的最近成功拉取
	CachedPull() *HotListPull
	// Pull 最新热榜，全部来源皆空时返回 nil
	Pull(ctx context.Context) *HotListPull
}

// ──────────────────────────── 结构体 ────────────────────────────

// TopicPipeline 组装终局组件而不允许任何模型绕过宿主闸门
// （topic_pipeline.py:4481-4541 TopicPipeline）。
type TopicPipeline struct {
	// Store 推荐事件库
	Store *TopicStarterStore
	// Now 当前时间函数（可注入）
	Now func() float64

	activeRequestIDsLoader func() []string
	newsFeed               WebSearchFn
	hotList                HotListFeedInterface
	recordsLoader          func() map[string][]map[string]any
	// CompletedRecordsLoader 完结投影记录加载器（对齐 :4509 公开属性）
	CompletedRecordsLoader func() map[string][]map[string]any
	contextLoader          func(scope string) *V6ContextSnapshot
	stageInvokerFactory    func(*ArkBackgroundConfig) TopicStageInvoker
	searchOverride         SearchFn
	searchProviderID       string
	fetch                  FetchFn
	classifySource         SourceClassifier

	stateRefreshMu sync.Mutex
	summaryLocksMu sync.Mutex
	summaryLocks   map[string]*sync.Mutex

	// OnCandidatesReady 候选就绪回调（对齐 :4532 公开属性）
	OnCandidatesReady func(scope string)
	// StageTokenCounterFactory token 计数工厂（⤴️31d：真实 StageTokenCounter
	// 随 topic_history.go 落地；nil 时摘要预算 fail-closed）
	StageTokenCounterFactory func(string) (*StageTokenCounter, error)

	statePipeline *TopicPreparePipeline
}

// NewTopicPipeline 构造（对齐 :4484-4541 __init__）。
// contextLoader/recordsLoader/activeRequestIDsLoader/newsFeed/hotList/search
// 为空时按 Python 默认值接缝（见文件头已知偏差）。
func NewTopicPipeline(store *TopicStarterStore, contextLoader func(string) *V6ContextSnapshot,
	recordsLoader func() map[string][]map[string]any,
	stageInvokerFactory func(*ArkBackgroundConfig) TopicStageInvoker,
	search SearchFn, searchProviderID string, fetch FetchFn,
	classifySource SourceClassifier, newsFeed WebSearchFn, hotList HotListFeedInterface,
	activeRequestIDsLoader func() []string, now func() float64) *TopicPipeline {
	if now == nil {
		now = func() float64 { return float64(time.Now().UnixNano()) / 1e9 }
	}
	if activeRequestIDsLoader == nil {
		activeRequestIDsLoader = func() []string { return nil }
	}
	// Python: records_loader or history.load_all_session_records（⤴️31d）
	if recordsLoader == nil {
		recordsLoader = func() map[string][]map[string]any { return map[string][]map[string]any{} }
	}
	pipeline := &TopicPipeline{
		Store:                  store,
		Now:                    now,
		activeRequestIDsLoader: activeRequestIDsLoader,
		newsFeed:               newsFeed,
		hotList:                hotList,
		recordsLoader:          recordsLoader,
		searchOverride:         search,
		searchProviderID:       searchProviderID,
		fetch:                  fetch,
		classifySource:         classifySource,
		summaryLocks:           map[string]*sync.Mutex{},
	}
	// Python: completed_records_loader = self._load_completed_records
	pipeline.CompletedRecordsLoader = pipeline.LoadCompletedRecords
	// Python: context_loader or 默认快照构建
	if contextLoader != nil {
		pipeline.contextLoader = contextLoader
	} else {
		pipeline.contextLoader = func(scope string) *V6ContextSnapshot {
			snapshot, err := DefaultContextSnapshot(store, scope, now(),
				pipeline.CompletedRecordsLoader)
			if err != nil {
				return nil
			}
			return snapshot
		}
	}
	// Python: 默认工厂挂 receipt_sink=persist_provider_invocation
	if stageInvokerFactory == nil {
		sink := func(receipt map[string]any) { PersistProviderInvocation(store, receipt) }
		stageInvokerFactory = func(config *ArkBackgroundConfig) TopicStageInvoker {
			return BuildV6ArkStageInvoker(config, NewDefaultChatClientFactory(), sink)
		}
	}
	pipeline.stageInvokerFactory = stageInvokerFactory
	if search == nil {
		// Python: search or _unconfigured_v6_search
		pipeline.searchOverride = UnconfiguredV6Search
	}
	pipeline.statePipeline = NewTopicPreparePipeline(store, nil,
		pipeline.CompletedRecordsLoader, stageInvokerFactory,
		pipeline.searchOverride, fetch, classifySource, now, nil)
	return pipeline
}

// ──────────────────────────── 导出函数 ────────────────────────────

// LoadCompletedRecords 把每个加载的会话投影到其已完结用户请求
// （topic_pipeline.py:4543-4553 _load_completed_records）。
func (p *TopicPipeline) LoadCompletedRecords() map[string][]map[string]any {
	result := map[string][]map[string]any{}
	for sessionID, sessionRecords := range p.recordsLoader() {
		result[sessionID] = ProjectCompletedSessionRecords(sessionID, sessionRecords, true)
	}
	return result
}

// SearchProviderID 实际使用的搜索厂商 ID（对齐 :4555-4567 search_provider_id）。
func (p *TopicPipeline) SearchProviderID() string {
	if p.searchProviderID != "" {
		return p.searchProviderID
	}
	if p.searchOverride != nil {
		// Python getattr(search_override, "provider_id", INJECTED_...)：Go 函数
		// 无属性，注入替身一律取审计厂商 ID。
		return INJECTEDSearchAuditProviderID
	}
	id, err := SelectedTopicSearchProviderID(nil)
	if err != nil {
		return ""
	}
	return id
}

// RefreshState 刷新共享自然历史与 Memory（对齐 :4569-4575 refresh_state）。
// 全局与项目生成器共享一份；无刷新原因时补摘要。
// ⤵️d5：_summarize_completed_sessions 摘要链（:5736）落地后接线。

// SelectedTopicSearchProviderID 所选 lane 的精确服务身份
// （topic_search.py:304-326 selected_topic_search_provider_id）。
// 原生自由搜索 lane 是唯一实现：未设变量即选它；显式未知名仍大声失败。
// environ 为 nil 时读进程环境。
func SelectedTopicSearchProviderID(environ map[string]string) (string, error) {
	value := func(key string) string {
		if environ != nil {
			return strings.TrimSpace(environ[key])
		}
		return strings.TrimSpace(os.Getenv(key))
	}
	provider := value("TOPIC_STARTER_SEARCH_PROVIDER")
	if provider == "" {
		provider = value("TOPIC_STARTER_V6_SEARCH_PROVIDER")
	}
	if provider == "" || provider == "openjiuwen-native" {
		if err := requireNativeFreeSearch(environ); err != nil {
			return "", err
		}
		return OpenjiuwenNativeSearchProviderId, nil
	}
	return "", &OpenJiuwenNativeSearchContractError{Reason: "search_provider_not_configured"}
}

// requireNativeFreeSearch 原生 lane 要求两个自由引擎开关同时为 true
// （topic_search.py:288-297 _require_native_free_search）。
func requireNativeFreeSearch(environ map[string]string) error {
	enabled := func(key string) bool {
		var raw string
		if environ != nil {
			raw = environ[key]
		} else {
			raw = os.Getenv(key)
		}
		return strings.ToLower(strings.TrimSpace(raw)) == "true"
	}
	if !enabled("FREE_SEARCH_DDG_ENABLED") || !enabled("FREE_SEARCH_BING_ENABLED") {
		return &OpenJiuwenNativeSearchContractError{
			Reason: "openjiuwen_native_search_requires_free_engines"}
	}
	return nil
}

// ScopeHasPendingRequest scope 的源会话仍有宿主在跑的请求
// （topic_pipeline.py:4577-4621）。历史投影把缺完结行的请求标为
// request_pending；只在网关仍在跑时计数，重启丢失的请求不会拖住补充。
func (p *TopicPipeline) ScopeHasPendingRequest(contextScope string) bool {
	if _, err := ProjectIDForScope(contextScope); err != nil {
		return false
	}
	live := map[string]bool{}
	for _, requestID := range p.activeRequestIDsLoader() {
		live[requestID] = true
	}
	if len(live) == 0 {
		return false
	}
	records := p.CompletedRecordsLoader()
	metadata, _ := swarmsession.GetAllSessionsMetadata(1_000_000, 0)
	projectBySession := map[string]string{}
	for _, item := range metadata {
		sessionID := pyStr(item["session_id"])
		if sessionID != "" {
			projectBySession[sessionID] = pyStr(item["project_id"])
		}
	}
	for sessionID, sessionRecords := range records {
		sessionProjectID, known := projectBySession[sessionID]
		if !known {
			sessionProjectID = ""
		}
		scopeMatch := PipelineScopeMatches(sessionProjectID, contextScope)
		globalOutside := contextScope == GlobalNewChatScope &&
			!GlobalProjectBuckets[sessionProjectID]
		if !scopeMatch && !globalOutside {
			continue
		}
		var trailing map[string]any
		for index := len(sessionRecords) - 1; index >= 0; index-- {
			record := sessionRecords[index]
			if record == nil {
				continue
			}
			role := strings.ToLower(pyStr(record["role"]))
			if role != "user" && role != "human" && role != "request_pending" {
				continue
			}
			trailing = record
			break
		}
		if trailing != nil &&
			strings.ToLower(pyStr(trailing["role"])) == "request_pending" &&
			live[pyStr(trailing["request_id"])] {
			return true
		}
	}
	return false
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// summaryLockFor 每个 scope 一把摘要锁（对齐 :4531 defaultdict(asyncio.Lock)）。
func (p *TopicPipeline) summaryLockFor(scope string) *sync.Mutex {
	p.summaryLocksMu.Lock()
	defer p.summaryLocksMu.Unlock()
	lock, present := p.summaryLocks[scope]
	if !present {
		lock = &sync.Mutex{}
		p.summaryLocks[scope] = lock
	}
	return lock
}

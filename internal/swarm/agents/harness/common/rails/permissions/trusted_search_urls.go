// 会话持有的可信搜索 URL 台账与 Host producer 回调能力。
//
// 对应 Python 代码：
// jiuwenswarm/agents/harness/common/rails/permissions/trusted_search_urls.py（236 行）
//
// 职责：每个根会话维护一份「本次会话内搜索工具真实返回过的 URL」台账，
// 供 url_safety 把「最近搜索结果」作为可信来源（RecentURLSource）用于
// 只读网络抓取的来源比对。台账有容量上限与淘汰，会话关闭后永久失效。
//
// producer 是**一次性租约**：绑定 → 完成（或撤销）→ 不可再用。
// 被复制的 async 上下文共享同一终态（同 generated_artifact_delivery 的 lease 模式）。
//
// 语言差异：Python 用 ContextVar + RLock；Go 用 ctx 派生 + sync.RWMutex。

package permissions

import (
	"context"
	"net/url"
	"strings"
	"sync"

	"github.com/uapclaw/uapclaw-go/internal/swarm/agents/harness/common/tools"
)

// ──────────────────────────── 结构体 ────────────────────────────

// SessionTrustedSearchUrls 只为单个根会话运行时保留规范化 URL。
//
// Python: class SessionTrustedSearchUrls
// 用有序切片维护插入顺序（Python OrderedDict），容量满时不再接收新 URL。
type SessionTrustedSearchUrls struct {
	// mu 保护全部字段
	mu sync.RWMutex
	// rootSessionID 根会话标识
	rootSessionID string
	// urls 已记录 URL（保序，去重）
	urls []string
	// urlSet 去重索引
	urlSet map[string]bool
	// closed 是否已永久关闭
	closed bool
}

// trustedSearchProducerBinding producer 绑定的不可变参数。
//
// Python: @dataclass(frozen=True) _TrustedSearchProducerBinding
type trustedSearchProducerBinding struct {
	// ledger 目标台账
	ledger *SessionTrustedSearchUrls
	// key 调用键
	key ToolInvocationKeyV1
	// toolName 工具名
	toolName string
	// maxResults 本次回调最多接受的 URL 数
	maxResults int
}

// trustedSearchProducerLease 被复制的 async 上下文共享的一次性 Host 能力。
//
// Python: class _TrustedSearchProducerLease
type trustedSearchProducerLease struct {
	// mu 保护 terminal
	mu sync.Mutex
	// binding 绑定参数
	binding trustedSearchProducerBinding
	// terminal 是否已终结
	terminal bool
}

// trustedSearchProducerKey producer 租约的 context key 类型。
type trustedSearchProducerKey struct{}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

const (
	// MaxTrustedSearchUrlsPerSession 单会话可信搜索 URL 上限
	// Python: MAX_TRUSTED_SEARCH_URLS_PER_SESSION = 500
	MaxTrustedSearchUrlsPerSession = 500
	// TrustedSearchSourceTool producer 来源工具标识
	TrustedSearchSourceTool = "host_search_producer"
	// trustedSearchDefaultSourceKind 默认来源类别
	// Python: RecentUrlSource.source_kind 默认值 "recent_search_result"
	trustedSearchDefaultSourceKind = "recent_search_result"
)

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// NewSessionTrustedSearchUrls 构造台账并可选绑定会话。
//
// Python: SessionTrustedSearchUrls.__init__(root_session_id="")
func NewSessionTrustedSearchUrls(rootSessionID string) *SessionTrustedSearchUrls {
	return &SessionTrustedSearchUrls{
		rootSessionID: strings.TrimSpace(rootSessionID),
		urlSet:        map[string]bool{},
	}
}

// RootSessionID 返回当前绑定的根会话标识。
func (s *SessionTrustedSearchUrls) RootSessionID() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.rootSessionID
}

// BindSession 绑定根会话；空/不匹配/已关闭时报错。
//
// Python: SessionTrustedSearchUrls.bind_session(root_session_id)
func (s *SessionTrustedSearchUrls) BindSession(rootSessionID string) error {
	sessionID := strings.TrimSpace(rootSessionID)
	if sessionID == "" {
		return errTrustedSearch("trusted_search_session_missing")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return errTrustedSearch("trusted_search_session_closed")
	}
	if s.rootSessionID != "" && s.rootSessionID != sessionID {
		return errTrustedSearch("trusted_search_session_mismatch")
	}
	s.rootSessionID = sessionID
	return nil
}

// RecordBatch 校验一次 Host 回调，并在容量允许范围内尽量多地新增 URL。
//
// Python: SessionTrustedSearchUrls.record_batch(*, key, urls, max_results)
// 返回 (accepted, dropped)：accepted 为真正写入数，dropped 为因容量满被丢弃数。
// 会话不匹配时返回 (0, len(normalized))——即「解析成功但未接受」。
func (s *SessionTrustedSearchUrls) RecordBatch(
	key ToolInvocationKeyV1,
	urls []string,
	maxResults int,
) (int, int) {
	// Python: limit = max(0, min(int(max_results), MAX_SEARCH_MAX_RESULTS))
	// ⚠️ 与 bind_trusted_search_producer 里的 normalize_search_max_results 不同：
	// 此处是「下界 0」的钳制（0 → 0 直接返回），bind 处是 normalize（0 → 1）。
	limit := maxResults
	if limit > tools.MaxSearchMaxResults {
		limit = tools.MaxSearchMaxResults
	}
	if limit < 0 {
		limit = 0
	}
	if limit == 0 {
		return 0, 0
	}
	normalized := []string{}
	seen := map[string]bool{}
	for _, rawURL := range urls {
		canonical := NormalizeURLForMatch(rawURL)
		if canonical == "" || seen[canonical] {
			continue
		}
		parsed, err := url.Parse(canonical)
		if err != nil {
			continue
		}
		if UnsafePublicHTTPSURLReason(parsed) != "" {
			continue
		}
		normalized = append(normalized, canonical)
		seen[canonical] = true
		if len(normalized) >= limit {
			break
		}
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return 0, 0
	}
	if s.rootSessionID == "" || key.RootSessionID != s.rootSessionID {
		return 0, len(normalized)
	}
	remaining := MaxTrustedSearchUrlsPerSession - len(s.urls)
	if remaining < 0 {
		remaining = 0
	}
	accepted := 0
	dropped := 0
	for _, canonical := range normalized {
		if s.urlSet[canonical] {
			continue
		}
		if accepted >= remaining {
			dropped++
			continue
		}
		s.urls = append(s.urls, canonical)
		s.urlSet[canonical] = true
		accepted++
	}
	return accepted, dropped
}

// Contains 判定某会话是否记录过该 URL（归一化后比对）。
//
// Python: SessionTrustedSearchUrls.contains(*, root_session_id, url)
func (s *SessionTrustedSearchUrls) Contains(rootSessionID, rawURL string) bool {
	canonical := NormalizeURLForMatch(rawURL)
	s.mu.RLock()
	defer s.mu.RUnlock()
	return !s.closed && canonical != "" &&
		rootSessionID == s.rootSessionID && s.urlSet[canonical]
}

// Sources 返回该会话全部可信来源（供 url_safety 的来源比对）。
//
// Python: SessionTrustedSearchUrls.sources(*, root_session_id)
// source_kind 用默认值 "recent_search_result"（Python 构造时不传该参数）。
func (s *SessionTrustedSearchUrls) Sources(rootSessionID string) []RecentURLSource {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.closed || rootSessionID != s.rootSessionID {
		return nil
	}
	out := make([]RecentURLSource, 0, len(s.urls))
	for _, canonical := range s.urls {
		host := ""
		if parsed, err := url.Parse(canonical); err == nil {
			host = strings.ToLower(parsed.Hostname())
		}
		out = append(out, RecentURLSource{
			URL:        canonical,
			Host:       host,
			SourceTool: TrustedSearchSourceTool,
			SourceKind: trustedSearchDefaultSourceKind,
			Trusted:    true,
		})
	}
	return out
}

// Dispose 永久关闭台账并丢弃所有 URL。
//
// Python: SessionTrustedSearchUrls.dispose()
func (s *SessionTrustedSearchUrls) Dispose() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.urls = nil
	s.urlSet = map[string]bool{}
	s.closed = true
}

// Len 返回已记录 URL 数。
//
// Python: SessionTrustedSearchUrls.__len__
func (s *SessionTrustedSearchUrls) Len() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.urls)
}

// BindTrustedSearchProducer 为当前工具任务安装 Host 专属回调能力，返回派生 ctx。
//
// Python: bind_trusted_search_producer(*, ledger, key, tool_name, max_results)
// 已有租约先撤销（对齐 Python 的 current.revoke()）。
func BindTrustedSearchProducer(
	ctx context.Context,
	ledger *SessionTrustedSearchUrls,
	key ToolInvocationKeyV1,
	toolName string,
	maxResults int,
) context.Context {
	if current := producerLeaseFromCtx(ctx); current != nil {
		current.revoke()
	}
	toolNameText := ""
	if strings.TrimSpace(toolName) != "" {
		toolNameText = toolName
	}
	lease := &trustedSearchProducerLease{
		binding: trustedSearchProducerBinding{
			ledger:     ledger,
			key:        key,
			toolName:   toolNameText,
			maxResults: tools.NormalizeSearchMaxResults(maxResults),
		},
	}
	return context.WithValue(ctx, trustedSearchProducerKey{}, lease)
}

// ClearTrustedSearchProducer 撤销并清除当前任务的生产者能力，返回派生 ctx。
//
// Python: clear_trusted_search_producer()
func ClearTrustedSearchProducer(ctx context.Context) context.Context {
	if current := producerLeaseFromCtx(ctx); current != nil {
		current.revoke()
	}
	return context.WithValue(ctx, trustedSearchProducerKey{}, (*trustedSearchProducerLease)(nil))
}

// CompleteTrustedSearchProducer 一次性完成当前 Host 搜索生产者。
//
// Python: complete_trusted_search_producer(*, tool_name, success, urls=())
// 返回 (accepted, dropped)；无租约或已终结返回 (0, 0)。
func CompleteTrustedSearchProducer(
	ctx context.Context,
	toolName string,
	success bool,
	urls []string,
) (int, int) {
	lease := producerLeaseFromCtx(ctx)
	if lease == nil {
		return 0, 0
	}
	return lease.complete(toolName, success, urls)
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// complete 消费本租约一次，并在成功时提交结构化 URL。
//
// Python: _TrustedSearchProducerLease.complete(*, tool_name, success, urls=())
func (l *trustedSearchProducerLease) complete(toolName string, success bool, urls []string) (int, int) {
	l.mu.Lock()
	if l.terminal || toolName != l.binding.toolName {
		l.mu.Unlock()
		return 0, 0
	}
	l.terminal = true
	l.mu.Unlock()
	if !success {
		return 0, 0
	}
	return l.binding.ledger.RecordBatch(l.binding.key, urls, l.binding.maxResults)
}

// revoke 阻止本调用能力后续完成。
//
// Python: _TrustedSearchProducerLease.revoke()
func (l *trustedSearchProducerLease) revoke() {
	l.mu.Lock()
	l.terminal = true
	l.mu.Unlock()
}

// producerLeaseFromCtx 从 ctx 读取生产者租约（未绑定或显式 nil 返回 nil）。
func producerLeaseFromCtx(ctx context.Context) *trustedSearchProducerLease {
	lease, ok := ctx.Value(trustedSearchProducerKey{}).(*trustedSearchProducerLease)
	if !ok {
		return nil
	}
	return lease
}

// errTrustedSearch 构造台账错误。
func errTrustedSearch(msg string) error {
	return &trustedSearchError{msg: msg}
}

// trustedSearchError 台账错误类型（携带稳定错误码）。
type trustedSearchError struct {
	// msg 错误码文本
	msg string
}

// Error 实现 error 接口
func (e *trustedSearchError) Error() string { return e.msg }

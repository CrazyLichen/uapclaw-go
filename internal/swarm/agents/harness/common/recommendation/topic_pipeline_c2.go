package recommendation

// topic_pipeline_c2.go 一比一对齐上游 topic_pipeline.py:2523-2700 的
// 数据类型、讨论覆盖投影与可选 lane 宽限协调器。
// ⤴️31c 并发映射（§5 已知偏差）：_OptionalLanes 的 asyncio.timeout_at/
// task.cancel 用 Go context.WithDeadline + goroutine select 等价实现
// （语义一致：deadline 未定时调用不受限；mandatory_ready 定宽限后
// 超时调用被取消并记录 lane）。

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"
)

// ──────────────────────────── 常量 ────────────────────────────

// V6OptionalLaneGraceSeconds 可选 lane 宽限秒（topic_pipeline.py:216）
const V6OptionalLaneGraceSeconds = 60.0

// ──────────────────────────── 结构体 ────────────────────────────

// StateUnavailableError 生产状态无匹配的冻结 S1 小票（topic_pipeline.py:2542-2544）。
type StateUnavailableError struct {
	// Reason 失败说明
	Reason string
}

// Error 实现 error 接口。
func (e *StateUnavailableError) Error() string { return e.Reason }

// pipelineGroundingBatch 一次 grounding 批的产物（topic_pipeline.py:2574-2579）。
type pipelineGroundingBatch struct {
	plans           []map[string]any
	decisionReceipt map[string]any
	evidenceReceipt map[string]any
}

// frozenEvidenceBatch 已完结计划前缀的冻结文档与候选关联
// （topic_pipeline.py:2581-2590）。
type frozenEvidenceBatch struct {
	frozen              *FrozenDocumentSnapshot
	searchReceipts      map[string]map[string]any
	allowedByCandidate  map[string][]string
	rejections          map[string][2]string
	operationRefs       map[string][]map[string]string
	evidenceByCandidate map[string]map[string]any
}

// pipelineScoring 效用与多样性从这些已锚定计划开始（:2592-2604）。
type pipelineScoring struct {
	plans          []map[string]any
	projected      []any
	utilityReceipt any
	utility        <-chan utilityTaskResult
	diversity      <-chan diversityTaskResult
}

// utilityTaskResult 效用任务结果。
type utilityTaskResult struct {
	value any
	err   error
}

// diversityTaskResult 多样性任务结果。
type diversityTaskResult struct {
	value *DiversityScoringResult
	err   error
}

// candidateEvidence 一个网页候选的搜索、所选行与抓取页（:2606-2622）。
type candidateEvidence struct {
	searchAuditRef     map[string]string
	evidence           map[string]any
	searchRejection    [2]string
	hasSearchRejection bool
	searchReceipt      map[string]any
	fetchedRows        []fetchedRow
	fetchAuditRefs     []map[string]string
	fetchFailure       string
}

// fetchedRow 一次抓取的（行、文档、审计引用）三元组。
type fetchedRow struct {
	row      map[string]any
	document map[string]any
	auditRef map[string]string
}

// sessionSummaryBatch 一次会话摘要批（:2624-2630）。
type sessionSummaryBatch struct {
	artifacts            []*SessionSummaryArtifact
	coverageReceipt      map[string]any
	summaryConfigVersion string
	generatedSummaryIDs  []string
}

// OptionalLaneTimeout 可选 lane 的调用在宽限结束时仍在运行（:2632-2634）。
type OptionalLaneTimeout struct {
	// Lanes 涉及的 lane 名
	Lanes string
}

// Error 实现 error 接口。
func (e *OptionalLaneTimeout) Error() string { return e.Lanes }

// ──────────────────────────── 导出函数 ────────────────────────────

// LocalDay 本地日期串（topic_pipeline.py:2523-2525）。
func LocalDay(timestamp float64) string {
	return time.Unix(int64(timestamp), 0).Format("2006-01-02")
}

// DiscussionCoverage 讨论覆盖投影（:2527-2540）。
func DiscussionCoverage(summaries []*SessionSummaryArtifact,
	otherScopeDiscussion []map[string]any) []map[string]any {
	result := []map[string]any{}
	for _, item := range summaries {
		result = append(result, map[string]any{
			"session_id":        item.SessionID,
			"summary":           item.SummaryText,
			"assistance_origin": item.AssistanceOrigin,
			"completed_on":      LocalDay(item.CompletedAt),
		})
	}
	result = append(result, otherScopeDiscussion...)
	return result
}

// ──────────────────────────── OptionalLanes ────────────────────────────

// OptionalLanes 可选 lane 的唯一约束：mandatory lane 就绪后的固定宽限
// （topic_pipeline.py:2636-2698；Go 并发映射见文件头偏差标注）。
type OptionalLanes struct {
	mu       sync.Mutex
	deadline *time.Time
	timedOut map[string]bool
	failed   map[string]string
	// graceSeconds MandatoryReady 使用的宽限（测试注入可覆盖；生产用常量）
	graceSeconds float64
}

// NewOptionalLanes 构造（生产默认宽限 V6OptionalLaneGraceSeconds）。
func NewOptionalLanes() *OptionalLanes {
	return &OptionalLanes{timedOut: map[string]bool{}, failed: map[string]string{},
		graceSeconds: V6OptionalLaneGraceSeconds}
}

// NewOptionalLanesWithGrace 以指定宽限构造（测试注入；偏差登记 §5）。
func NewOptionalLanesWithGrace(graceSeconds float64) *OptionalLanes {
	return &OptionalLanes{timedOut: map[string]bool{}, failed: map[string]string{},
		graceSeconds: graceSeconds}
}

// MandatoryReady mandatory lane 就绪：为宽限内的未定界调用设定截止
// （:2648-2656）。
func (l *OptionalLanes) MandatoryReady() {
	l.mu.Lock()
	defer l.mu.Unlock()
	deadline := time.Now().Add(time.Duration(l.graceSeconds * float64(time.Second)))
	l.deadline = &deadline
}

// Deadline 当前宽限截止（未定时返回 nil）。
func (l *OptionalLanes) Deadline() *time.Time {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.deadline
}

// Finish 等待 lane 的已启动任务落在宽限内；被切断返回 false（:2658-2670）。
func (l *OptionalLanes) Finish(lane string, done <-chan struct{}, cancel context.CancelFunc) bool {
	l.mu.Lock()
	deadline := l.deadline
	l.mu.Unlock()
	if deadline == nil {
		<-done
		return true
	}
	// 对齐 asyncio.wait：任务已完成立即成功，与 deadline 无关
	select {
	case <-done:
		return true
	default:
	}
	remaining := time.Until(*deadline)
	if remaining <= 0 {
		cancel()
		<-done
		l.mu.Lock()
		l.timedOut[lane] = true
		l.mu.Unlock()
		return false
	}
	select {
	case <-done:
		return true
	case <-time.After(remaining):
		cancel()
		<-done
		l.mu.Lock()
		l.timedOut[lane] = true
		l.mu.Unlock()
		return false
	}
}

// Within 为给定 lane 执行一次调用；宽限结束时取消（:2672-2698）。
func (l *OptionalLanes) Within(ctx context.Context, lanes []string,
	call func(ctx context.Context) (any, error)) (any, error) {
	l.mu.Lock()
	deadline := l.deadline
	l.mu.Unlock()
	// 对齐 Python :2685：deadline 已过期时不执行调用，直接记超时并抛
	if deadline != nil && !time.Now().Before(*deadline) {
		l.RecordTimedOut(lanes)
		return nil, &OptionalLaneTimeout{Lanes: strings.Join(lanes, ", ")}
	}
	// deadline 未定或未到：调用在无界（或既有截止）下执行
	runCtx := ctx
	if deadline != nil {
		var cancel context.CancelFunc
		runCtx, cancel = context.WithDeadline(ctx, *deadline)
		defer cancel()
	} else {
		var cancel context.CancelFunc
		runCtx, cancel = context.WithCancel(ctx)
		defer cancel()
	}
	type callResult struct {
		value any
		err   error
	}
	resultCh := make(chan callResult, 1)
	go func() {
		value, err := call(runCtx)
		resultCh <- callResult{value, err}
	}()
	var timeoutCh <-chan time.Time
	if deadline != nil {
		timer := time.NewTimer(time.Until(*deadline))
		defer timer.Stop()
		timeoutCh = timer.C
	}
	select {
	case result := <-resultCh:
		return result.value, result.err
	case <-timeoutCh:
		l.mu.Lock()
		for _, lane := range lanes {
			l.timedOut[lane] = true
		}
		l.mu.Unlock()
		return nil, &OptionalLaneTimeout{Lanes: strings.Join(lanes, ", ")}
	}
}

// RecordTimedOut 登记 lane 超时。
func (l *OptionalLanes) RecordTimedOut(lanes []string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	for _, lane := range lanes {
		l.timedOut[lane] = true
	}
}

// RecordFailed 登记 lane 失败原因（:2680-2686 语义）。
func (l *OptionalLanes) RecordFailed(lane, reason string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.failed[lane] = reason
}

// TimedOutSet 超时 lane 快照。
func (l *OptionalLanes) TimedOutSet() map[string]bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	result := map[string]bool{}
	for lane := range l.timedOut {
		result[lane] = true
	}
	return result
}

// FailedMap 失败快照。
func (l *OptionalLanes) FailedMap() map[string]string {
	l.mu.Lock()
	defer l.mu.Unlock()
	result := map[string]string{}
	for lane, reason := range l.failed {
		result[lane] = reason
	}
	return result
}

// OptionalLane 判定计划所属可选 lane（topic_pipeline.py:2700-2708）。
func OptionalLane(plan map[string]any) string {
	switch {
	case pyStr(plan["news_match"]) != "":
		return "hot_news"
	case pyStr(plan["category"]) == "profile_recent_progress":
		return "recent"
	case pyStr(plan["category"]) == "project_domain_progress":
		return "project_domain"
	}
	return ""
}

var _ = fmt.Sprintf

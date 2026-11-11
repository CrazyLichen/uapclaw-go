package recommendation

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"math"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"time"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/model_clients"
	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	commonschema "github.com/uapclaw/uapclaw-go/internal/common/schema"
	"github.com/uapclaw/uapclaw-go/internal/common/workspace"
	"github.com/uapclaw/uapclaw-go/internal/swarm/server/session"
	swarmschema "github.com/uapclaw/uapclaw-go/internal/swarm/schema"
)

// ──────────────────────────── 结构体 ────────────────────────────

// SuggestionReadyPusher 建议就绪推帧口，10.7 / 网关章节实现并注入。
// Python: prompt_suggestion.py READY_EVENT 推送
type SuggestionReadyPusher interface {
	PushReady(ctx context.Context, event SuggestionReadyEvent)
}

// SuggestionReadyEvent 推给网关的建议就绪载荷。
// Python: prompt_suggestion.py ready 事件 payload
type SuggestionReadyEvent struct {
	// SessionID 会话标识
	SessionID string
	// RequestID 请求标识
	RequestID string
	// SuggestionID 建议标识
	SuggestionID string
	// Text 建议文本
	Text string
}

// MainCall 一轮的最后一次主模型调用，原样用于重放。
// Python: prompt_suggestion.py:232 _MainCall
type MainCall struct {
	// LLM 父调用使用的客户端（必须同一个，否则缓存 key 可能不同）
	LLM model_clients.BaseModelClient
	// Model 模型名，逐字一致
	Model string
	// Messages 父调用发送的完整消息窗口
	Messages []llmschema.BaseMessage
	// Tools 父调用发送的工具定义
	Tools []commonschema.ToolInfoInterface
	// Answer 父调用的回答
	Answer *llmschema.AssistantMessage
	// MaxIterations 重放的最大轮次
	MaxIterations int
	// Parameters 父调用的全部非默认 kwarg（★ 漏一个即 cache key 失配）
	Parameters map[string]any
	// Planning 该轮处于 plan 模式（计划讨论轮，不出建议）
	Planning bool
}

// TurnEndOptions 轮结束参数。
// Python: prompt_suggestion.py:376 turn_ended 的命名参数
type TurnEndOptions struct {
	// Status 轮终态（"success" / "cancelled" / 其他错误态）
	Status string
	// WaitingForUser 该轮在等用户回答（交互事件）
	WaitingForUser bool
	// Completion 完成行落盘回执（nil 表示无回执；fork 推送前等待）
	Completion <-chan struct{}
}

// answeredCall 会话最近一次成功轮与其主调用。
// Python: prompt_suggestion.py:357 self._answered: dict[str, tuple[str, _MainCall]]
type answeredCall struct {
	// requestID 该轮的请求标识
	requestID string
	// call 该轮的主调用
	call *MainCall
}

// PromptSuggestionForks 每会话在跑的轮、最近主调用与它启动的生成。
// Python: prompt_suggestion.py:349 PromptSuggestionForks
type PromptSuggestionForks struct {
	// mu 保护以下全部字段
	mu sync.Mutex
	// turns 会话标识 → 本轮主调用（nil 表示本轮已开始但尚无主调用）
	turns map[string]*MainCall
	// tasks 会话标识 → 在飞生成的取消函数
	tasks map[string]context.CancelFunc
	// answered 会话标识 → 最近一次成功轮的主调用
	answered map[string]answeredCall
	// readyPusher 就绪推帧口；nil 时不推送（spec DEV-10）
	readyPusher SuggestionReadyPusher
	// config 推荐开关配置（nil 表示默认关闭）
	config map[string]any
	// lastSuppressedReason 最近一次抑制原因（调试与测试用）
	lastSuppressedReason string
}

// ──────────────────────────── 常量 ────────────────────────────

// logComponent 日志组件标识
const logComponent = logger.ComponentAgentCore

// ──────────────────────────── 全局变量 ────────────────────────────

// Forks 全局单例。
// Python: prompt_suggestion.py:535 forks = PromptSuggestionForks()
var Forks = NewPromptSuggestionForks()

// UserInteractionEvents 让轮停在等用户回答的宿主事件。
// Python: prompt_suggestion.py:50 USER_INTERACTION_EVENTS
var UserInteractionEvents = map[string]bool{
	"chat.ask_user_question":       true,
	"harness.activate_interaction": true,
	"plan.approval_required":       true,
}

// ──────────────────────────── 导出函数 ────────────────────────────

// CurrentSuggestions 回收会话最新成功请求下未消费的建议。
// Python: prompt_suggestion.py:583-604 current_suggestions
// 末尾双复查：epoch 或最新完成请求在中途变化 → 整批作废。
func CurrentSuggestions(f *PromptSuggestionForks, store *TopicStarterStore, sessionID string) map[string]any {
	empty := func(enabled bool) map[string]any {
		return map[string]any{"enabled": enabled, "suggestions": []map[string]any{}}
	}
	epoch := ConsentEpoch(f.config)
	if !epoch.Active {
		return empty(false)
	}
	requestID := LatestCompletedRequest(sessionID)
	if requestID == "" {
		return empty(true)
	}
	slateID := "prompt-suggestion:" + requestID
	policyID := PolicyID
	rows, err := store.QueryEvents(QueryEventsOptions{
		EventType: "prompt_suggestion_generation", PolicyID: &policyID,
		SlateID: &slateID, Limit: 2,
	})
	if err != nil {
		logger.Warn(logComponent).Err(err).Str("session_id", sessionID).
			Msg("prompt suggestion: suggestion query failed")
		return empty(true)
	}
	suggestions := make([]map[string]any, 0)
	for _, row := range rows {
		payload, _ := row["payload"].(map[string]any)
		rowSession, _ := row["session_id"].(string)
		createdAt, _ := row["created_at"].(float64)
		if rowSession != sessionID || createdAt < epoch.Value || payload == nil || payload["outcome"] != "candidate" {
			continue
		}
		topicID, _ := row["topic_id"].(string)
		consumed := false
		for _, outcome := range []string{"accepted", "ignored", "suppressed"} {
			outcomeEventType := "prompt_suggestion_" + outcome
			consumedRows, err := store.QueryEvents(QueryEventsOptions{
				EventType: outcomeEventType, PolicyID: &policyID, TopicID: &topicID, Limit: 1,
			})
			if err == nil && len(consumedRows) > 0 {
				consumed = true
				break
			}
		}
		if consumed {
			continue
		}
		suggestions = append(suggestions, map[string]any{
			"session_id":    payload["session_id"],
			"request_id":    payload["request_id"],
			"suggestion_id": payload["suggestion_id"],
			"text":          payload["text"],
		})
	}
	if !ConsentEpoch(f.config).Equal(epoch) || LatestCompletedRequest(sessionID) != requestID {
		return empty(ConsentEpoch(f.config).Active)
	}
	return map[string]any{"enabled": true, "suggestions": suggestions}
}

// StageDelivery 记录送达并返回页面载荷；请求已过期则记 suppressed 且不推。
// 推荐关闭时什么都不记（Python: consent_epoch() is None → None）。
// Python: prompt_suggestion.py:672-700 stage_delivery
func StageDelivery(f *PromptSuggestionForks, store *TopicStarterStore, sessionID, requestID string,
	payload map[string]any,
) map[string]any {
	if !ConsentEpoch(f.config).Active {
		return nil
	}
	suggestionID, _ := payload["suggestion_id"].(string)
	stale := LatestCompletedRequest(sessionID) != requestID
	var reason any
	status := "pushed"
	if stale {
		reason = "stale_request"
		status = "suppressed"
	}
	if _, err := store.RecordEvent(map[string]any{
		"session_id":        sessionID,
		"slate_id":          "prompt-suggestion:" + requestID,
		"event_type":        "prompt_suggestion_delivery",
		"topic_id":          "prompt-suggestion:" + suggestionID,
		"policy_id":         PolicyID,
		"selection_mode":    "prompt_suggestion",
		"propensity_reason": "not_estimated",
		"created_at":        float64(time.Now().UnixNano()) / 1e9,
		"payload": map[string]any{
			"suggestion_id": suggestionID, "session_id": sessionID,
			"request_id": requestID, "status": status, "reason": reason,
		},
	}); err != nil {
		logger.Warn(logComponent).Err(err).Str("session_id", sessionID).
			Msg("prompt suggestion delivery record failed")
		return nil
	}
	if stale {
		return nil
	}
	text, _ := payload["text"].(string)
	return map[string]any{
		"event_type":    ReadyEvent,
		"session_id":    sessionID,
		"request_id":    requestID,
		"suggestion_id": suggestionID,
		"text":          text,
	}
}

// RecordPageOutcome 记录页面对建议的处置（shown/accepted/ignored/suppressed），
// 每条建议每类各记一次（主键幂等）。无效输入返回 nil，有效返回写入结果。
// Python: prompt_suggestion.py:605-637 record_page_outcome（返回 bool | None）
func RecordPageOutcome(store *TopicStarterStore, params map[string]any) *bool {
	sessionID, _ := params["session_id"].(string)
	suggestionID, _ := params["suggestion_id"].(string)
	eventType, _ := params["event_type"].(string)
	var outcome map[string]any
	switch eventType {
	case "shown":
		outcome = map[string]any{}
	case "accepted":
		method, _ := params["method"].(string)
		elapsed := params["time_to_accept_ms"]
		outcome = map[string]any{"method": method, "time_to_accept_ms": elapsed}
		if method != "click" && method != "tab" {
			return nil
		}
		// Python: _plain_int 且 >= 0。已知偏差 D-6：Go JSON 解码统一 float64，
		// 整值 float64 视为 int；bool 显式排除。
		if !plainInt(elapsed) {
			return nil
		}
		if elapsedInt(elapsed) < 0 {
			return nil
		}
	case "ignored":
		reason, _ := params["reason"].(string)
		outcome = map[string]any{"reason": reason}
		switch reason {
		case "dismissed", "sent_other", "left_session", "typed":
		default:
			return nil
		}
	case "suppressed":
		reason, _ := params["reason"].(string)
		outcome = map[string]any{"reason": reason}
		if reason != "timing" {
			return nil
		}
	default:
		return nil
	}
	var generated []map[string]any
	if suggestionID != "" {
		topicID := "prompt-suggestion:" + suggestionID
		policyID := PolicyID
		rows, err := store.QueryEvents(QueryEventsOptions{
			EventType: "prompt_suggestion_generation", PolicyID: &policyID,
			TopicID: &topicID, Limit: 1,
		})
		if err != nil {
			return nil
		}
		generated = rows
	}
	if len(generated) == 0 {
		return nil
	}
	if sid, _ := generated[0]["session_id"].(string); sid != sessionID {
		return nil
	}
	genPayload, _ := generated[0]["payload"].(map[string]any)
	if genPayload == nil || genPayload["outcome"] != "candidate" {
		return nil
	}
	requestID, _ := genPayload["request_id"].(string)
	full := map[string]any{"suggestion_id": suggestionID, "session_id": sessionID, "request_id": requestID}
	for key, value := range outcome {
		full[key] = value
	}
	recorded, err := store.RecordEvent(map[string]any{
		"session_id":        sessionID,
		"slate_id":          "prompt-suggestion:" + requestID,
		"event_type":        "prompt_suggestion_" + eventType,
		"topic_id":          "prompt-suggestion:" + suggestionID,
		"policy_id":         PolicyID,
		"selection_mode":    "prompt_suggestion",
		"propensity_reason": "not_estimated",
		"created_at":        float64(time.Now().UnixNano()) / 1e9,
		"payload":           full,
	})
	if err != nil {
		logger.Warn(logComponent).Err(err).Str("session_id", sessionID).
			Msg("prompt suggestion page outcome record failed")
		return nil
	}
	return &recorded
}

// elapsedInt 把 plainInt 通过的值还原成 int64（D-6：int/int64/整值 float64 三态）。
func elapsedInt(value any) int64 {
	switch v := value.(type) {
	case int:
		return int64(v)
	case int64:
		return v
	case float64:
		return int64(v)
	}
	return 0
}

// plainInt Python isinstance(x, int) and not isinstance(x, bool) 的等价判定。
// JSON 数字在 Go 侧统一为 float64（偏差 D-6）：整值视为 int，非整值与 bool 为假。
func plainInt(value any) bool {
	switch v := value.(type) {
	case int:
		return true
	case int64:
		return true
	case float64:
		return v == math.Trunc(v)
	case bool:
		return false
	}
	return false
}

// LatestCompletedRequest 会话历史里最后一条完成行所指的请求，且它仍是会话最新轮。
// 语义：完成行 status=success 且其后所有行的 request_id 都等于它；否则返回空串。
// Python: prompt_suggestion.py:562-578 _latest_completed_request
func LatestCompletedRequest(sessionID string) string {
	records, err := session.ReadHistoryRecords(sessionID)
	if err != nil {
		logger.Warn(logComponent).Err(err).Str("session_id", sessionID).
			Msg("prompt suggestion: history unreadable")
		return ""
	}
	for index := len(records) - 1; index >= 0; index-- {
		row := records[index]
		if et, _ := row["event_type"].(string); et != string(swarmschema.EventTypeSessionRequestCompleted) {
			continue
		}
		requestID, _ := row["request_id"].(string)
		status, _ := row["status"].(string)
		later := map[string]struct{}{}
		for _, laterRow := range records[index+1:] {
			if rid, _ := laterRow["request_id"].(string); rid != "" {
				later[rid] = struct{}{}
			}
		}
		// Python: later <= {request_id}——后续行的 request_id 集合是 {request_id} 的子集
		onlySelf := len(later) == 0 || (len(later) == 1 && requestID != "")
		if _, hasSelf := later[requestID]; len(later) == 1 && !hasSelf {
			onlySelf = false
		}
		if status == "success" && onlySelf {
			return requestID
		}
		return ""
	}
	return ""
}

// NewPromptSuggestionForks 创建空的 forks 登记台。
func NewPromptSuggestionForks() *PromptSuggestionForks {
	return &PromptSuggestionForks{
		turns:    map[string]*MainCall{},
		tasks:    map[string]context.CancelFunc{},
		answered: map[string]answeredCall{},
	}
}

// RequestStarted 新请求丢弃上一条仍在生成的建议。
// Python: prompt_suggestion.py:360 request_started
func (f *PromptSuggestionForks) RequestStarted(sessionID string, personTurn bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	// Python: 1. task = self._tasks.pop(session_id); if task: task.cancel()
	if cancel, ok := f.tasks[sessionID]; ok {
		delete(f.tasks, sessionID)
		cancel()
	}
	// Python: 2. self._turns.pop(session_id, None)
	delete(f.turns, sessionID)
	// Python: 3. if person_turn: self._turns[session_id] = None
	if personTurn {
		f.turns[sessionID] = nil
	}
}

// SawMainCall 记录该会话本轮的最后一次主调用。
// Python: prompt_suggestion.py:372 saw_main_call
func (f *PromptSuggestionForks) SawMainCall(sessionID string, call *MainCall) {
	f.mu.Lock()
	defer f.mu.Unlock()
	// Python: if session_id in self._turns: self._turns[session_id] = call
	if _, ok := f.turns[sessionID]; ok {
		f.turns[sessionID] = call
	}
}

// TurnEnded 成功轮启动生成；其余记录抑制原因。
// Python: prompt_suggestion.py:376 turn_ended
func (f *PromptSuggestionForks) TurnEnded(sessionID, requestID string, opts TurnEndOptions) {
	f.mu.Lock()
	// Python: 1. if session_id not in self._turns: return
	call, ok := f.turns[sessionID]
	if !ok {
		f.mu.Unlock()
		return
	}
	// Python: 2. call = self._turns.pop(session_id)
	delete(f.turns, sessionID)
	// Python: 3. if status == "success" and call is not None and not call.answer.tool_calls:
	//              self._answered[session_id] = (request_id, call)
	if opts.Status == "success" && call != nil && !hasToolCalls(call.Answer) {
		f.answered[sessionID] = answeredCall{requestID: requestID, call: call}
	}
	// Python: 4. epoch = consent_epoch(); if epoch is None: return
	epoch := ConsentEpoch(f.config)
	if !epoch.Active {
		f.mu.Unlock()
		return
	}
	// Python: 5. 判定抑制原因；有原因则记 suppressed 并返回
	reason := suppressReason(call, opts)
	f.lastSuppressedReason = reason
	completion := opts.Completion
	if reason != "" {
		var uncached, answer any
		if call != nil {
			uncached = nullableTokens(uncachedTokens(call.Answer))
			answer = nullableTokens(answerTokens(call.Answer))
		}
		f.mu.Unlock()
		logger.Debug(logComponent).Str("session_id", sessionID).
			Str("reason", reason).Msg("prompt suggestion generation suppressed")
		f.recordLedgerEvent(epoch, "prompt_suggestion_generation", sessionID, requestID,
			newSuggestionID(), map[string]any{
				"outcome": "suppressed", "reason": reason,
				"parent_uncached_tokens": uncached, "answer_tokens": answer,
			})
		return
	}
	// Python: 6. task = asyncio.create_task(self._fork(...)); self._tasks[session_id] = task
	ctx, cancel := context.WithCancel(context.Background())
	f.tasks[sessionID] = cancel
	f.mu.Unlock()
	go f.runFork(ctx, sessionID, requestID, call, epoch, completion)
}

// AnsweredCall 返回某 requestID 的最近成功主调用，不是最近一轮则返回 nil。
// Python: prompt_suggestion.py:412 answered_call
func (f *PromptSuggestionForks) AnsweredCall(sessionID, requestID string) *MainCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	answered, ok := f.answered[sessionID]
	if !ok || answered.requestID != requestID {
		return nil
	}
	return answered.call
}

// CancelSuggestion 停止该会话的建议生成但保留已完成的主调用。
// Python: prompt_suggestion.py:420 cancel_suggestion
func (f *PromptSuggestionForks) CancelSuggestion(sessionID string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	cancel, ok := f.tasks[sessionID]
	if !ok {
		return false
	}
	delete(f.tasks, sessionID)
	cancel()
	return true
}

// SessionDeleted 删会话时取消在飞生成并清两个内存表。
// Python: prompt_suggestion.py:428 session_deleted
// ★ 不清 topic 库里的历史建议记录——上游审计账不随会话删（spec DEV-7）。
func (f *PromptSuggestionForks) SessionDeleted(sessionID string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	// Python: 1. task = self._tasks.pop(session_id); if task: task.cancel()
	if cancel, ok := f.tasks[sessionID]; ok {
		delete(f.tasks, sessionID)
		cancel()
	}
	// Python: 2. self._turns.pop(session_id, None)
	delete(f.turns, sessionID)
	// Python: 3. self._answered.pop(session_id, None)
	delete(f.answered, sessionID)
}

// Shutdown AgentServer 关闭时取消全部在飞生成。
// Python: prompt_suggestion.py:436 shutdown -> None（无 error，gather 吞异常）
func (f *PromptSuggestionForks) Shutdown() {
	f.mu.Lock()
	cancels := make([]context.CancelFunc, 0, len(f.tasks))
	for sessionID, cancel := range f.tasks {
		cancels = append(cancels, cancel)
		delete(f.tasks, sessionID)
	}
	f.mu.Unlock()
	for _, cancel := range cancels {
		cancel()
	}
}

// SetSuggestionReadyPusher 注入就绪推帧口（10.7 / 网关章节调用）。
func (f *PromptSuggestionForks) SetSuggestionReadyPusher(pusher SuggestionReadyPusher) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.readyPusher = pusher
}

// SetConfig 注入推荐开关配置。
func (f *PromptSuggestionForks) SetConfig(config map[string]any) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.config = config
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// recordLedgerEvent 在 epoch 有效期内追加账本事件；写失败仅 Warn，不影响会话。
// Python: prompt_suggestion.py:322-345 _record（try/except + Warn）
func (f *PromptSuggestionForks) recordLedgerEvent(
	epoch ConsentEpochValue, eventType, sessionID, requestID, suggestionID string,
	payload map[string]any,
) {
	if !ConsentEpoch(f.config).Equal(epoch) {
		return
	}
	full := map[string]any{
		"suggestion_id":  suggestionID,
		"session_id":     sessionID,
		"request_id":     requestID,
		"prompt_version": PromptVersion,
	}
	for key, value := range payload {
		full[key] = value
	}
	store, err := NewTopicStarterStore(filepath.Join(workspace.AgentWorkspaceDir(), ConsentDBName))
	if err != nil {
		logger.Warn(logComponent).Err(err).Str("session_id", sessionID).
			Msg("prompt suggestion ledger write failed")
		return
	}
	defer store.Close()
	event := map[string]any{
		"session_id":        sessionID,
		"slate_id":          "prompt-suggestion:" + requestID,
		"event_type":        eventType,
		"topic_id":          "prompt-suggestion:" + suggestionID,
		"policy_id":         PolicyID,
		"selection_mode":    "prompt_suggestion",
		"propensity_reason": "not_estimated",
		"created_at":        float64(time.Now().UnixNano()) / 1e9,
		"payload":           full,
	}
	if _, err := store.RecordEvent(event); err != nil {
		logger.Warn(logComponent).Err(err).Str("session_id", sessionID).
			Msg("prompt suggestion ledger write failed")
	}
}

// usagePayload 回复用量在账本里的形状；无用量信息返回 nil。
// Python: prompt_suggestion.py:312-320 usage_payload
func usagePayload(reply *llmschema.AssistantMessage) map[string]any {
	if reply == nil || reply.UsageMetadata == nil {
		return nil
	}
	usage := reply.UsageMetadata
	var reasoning any
	if usage.ReasoningTokens != nil {
		reasoning = *usage.ReasoningTokens
	}
	return map[string]any{
		"prompt_tokens":             usage.InputTokens,
		"completion_tokens":         usage.OutputTokens,
		"total_tokens":              usage.TotalTokens,
		"prompt_tokens_details":     map[string]any{"cached_tokens": usage.CacheTokens},
		"completion_tokens_details": map[string]any{"reasoning_tokens": reasoning},
	}
}

// suppressReason 逐条对齐 prompt_suggestion.py:390-408 的 7 个 reason。
func suppressReason(call *MainCall, opts TurnEndOptions) string {
	// Python: if status != "success": reason = "aborted" if cancelled else "last_response_error"
	if opts.Status != "success" {
		if opts.Status == "cancelled" {
			return "aborted"
		}
		return "last_response_error"
	}
	// Python: elif waiting_for_user: reason = "pending_interaction"
	if opts.WaitingForUser {
		return "pending_interaction"
	}
	// Python: elif call is None: reason = "no_model_call"
	if call == nil {
		return "no_model_call"
	}
	// Python: elif call.planning: reason = "plan_mode"
	if call.Planning {
		return "plan_mode"
	}
	// Python: elif call.answer.tool_calls: reason = "unanswered_tool_call"
	if hasToolCalls(call.Answer) {
		return "unanswered_tool_call"
	}
	// Python: elif answer > MAX_FORK_ANSWER_TOKENS: reason = "cache_cold"
	if tokens := answerTokens(call.Answer); tokens >= 0 && tokens > MaxForkAnswerTokens {
		return "cache_cold"
	}
	return ""
}

// hasToolCalls 判断回答是否带工具调用。
func hasToolCalls(answer *llmschema.AssistantMessage) bool {
	return answer != nil && len(answer.ToolCalls) > 0
}

// answerTokens 返回回答的 output token 数；无用量信息时返回 -1。
// Python: prompt_suggestion.py:253 answer_tokens
func answerTokens(answer *llmschema.AssistantMessage) int {
	if answer == nil || answer.UsageMetadata == nil {
		return -1
	}
	return answer.UsageMetadata.OutputTokens
}

// uncachedTokens 返回父调用新增（未命中缓存）的 token 数；无用量信息时返回 -1。
// Python: prompt_suggestion.py:246 uncached_tokens（input - cache + output）
func uncachedTokens(answer *llmschema.AssistantMessage) int {
	if answer == nil || answer.UsageMetadata == nil {
		return -1
	}
	usage := answer.UsageMetadata
	return usage.InputTokens - usage.CacheTokens + usage.OutputTokens
}

// nullableTokens 无用量信息（-1 哨兵）转 nil，对齐 Python 的 None。
func nullableTokens(tokens int) any {
	if tokens < 0 {
		return nil
	}
	return tokens
}

// runFork 重放父调用并解析建议；全量记账后经三道闸推送。
// Python: prompt_suggestion.py:447-536 _fork
func (f *PromptSuggestionForks) runFork(
	ctx context.Context, sessionID, requestID string, call *MainCall, epoch ConsentEpochValue,
	completion <-chan struct{},
) {
	defer func() {
		if recovered := recover(); recovered != nil {
			logger.Debug(logComponent).Any("panic", recovered).
				Msg("prompt suggestion fork recovered")
		}
		f.mu.Lock()
		delete(f.tasks, sessionID)
		f.mu.Unlock()
	}()

	// Python: 1. suggestion_id = uuid.uuid4().hex; record 初始结构
	suggestionID := newSuggestionID()
	started := time.Now()
	totalUsage := map[string]any{
		"prompt_tokens": 0, "completion_tokens": 0, "total_tokens": 0,
		"prompt_tokens_details":     map[string]any{"cached_tokens": 0},
		"completion_tokens_details": map[string]any{"reasoning_tokens": 0},
	}
	receipts := make([]map[string]any, 0)
	record := map[string]any{
		"model": call.Model, "parent_uncached_tokens": nullableTokens(uncachedTokens(call.Answer)),
		"answer_tokens": nullableTokens(answerTokens(call.Answer)), "provider_receipts": receipts,
		"provider_response_id": nil, "usage": nil, "total_usage": totalUsage,
		"missing_provider_receipts": 0, "usage_complete": true,
	}

	// Python: 2. messages = [*call.messages, call.answer, UserMessage(SUGGESTION_PROMPT)]
	// ★ 前缀必须逐字不变，否则服务商侧 prompt cache 无法命中。
	messages := make([]llmschema.BaseMessage, 0, len(call.Messages)+2)
	messages = append(messages, call.Messages...)
	if call.Answer != nil {
		messages = append(messages, call.Answer)
	}
	messages = append(messages, llmschema.NewUserMessage(SuggestionPrompt))

	// Python: 3. for _ in range(call.max_iterations): 重放，遇无 tool_calls 即停
	var replies []*llmschema.AssistantMessage
	countedIDs := map[string]bool{}
	var forkErr error
	for iteration := 0; iteration < maxInt(call.MaxIterations, 1); iteration++ {
		// Python: if consent_epoch() != epoch: return
		if !ConsentEpoch(f.config).Equal(epoch) {
			return
		}
		if ctx.Err() != nil {
			f.recordLedgerEvent(epoch, "prompt_suggestion_generation", sessionID, requestID,
				suggestionID, ledgerPayload(record, map[string]any{
					"outcome": "cancelled",
					"latency_ms": time.Since(started).Milliseconds(),
				}))
			return
		}
		callStarted := time.Now()
		record["missing_provider_receipts"] = record["missing_provider_receipts"].(int) + 1
		record["usage_complete"] = false
		reply, err := invokeMainCall(ctx, call, messages)
		if err != nil {
			// Python: except Exception: logger.warning("[PromptSuggestion] fork failed: ...")
			logger.Warn(logComponent).Err(err).Str("session_id", sessionID).
				Msg("prompt suggestion fork failed")
			forkErr = err
			break
		}
		replies = append(replies, reply)
		// Python: receipts.append + usage 记账
		usage := usagePayload(reply)
		receipts = append(receipts, map[string]any{
			"model": call.Model, "provider_response_id": reply.ResponseID,
			"usage": usage, "latency_ms": time.Since(callStarted).Milliseconds(),
		})
		record["provider_receipts"] = receipts
		record["missing_provider_receipts"] = record["missing_provider_receipts"].(int) - 1
		complete := true
		for _, item := range receipts {
			id, _ := item["provider_response_id"].(string)
			if id == "" || item["usage"] == nil {
				complete = false
			}
		}
		record["usage_complete"] = complete
		if len(replies) == 1 {
			record["provider_response_id"] = reply.ResponseID
			record["usage"] = usage
		}
		// Python: total_usage 按 response_id 去重累加
		if usage != nil && (reply.ResponseID == "" || !countedIDs[reply.ResponseID]) {
			totalUsage["prompt_tokens"] = totalUsage["prompt_tokens"].(int) + usage["prompt_tokens"].(int)
			totalUsage["completion_tokens"] = totalUsage["completion_tokens"].(int) + usage["completion_tokens"].(int)
			totalUsage["total_tokens"] = totalUsage["total_tokens"].(int) + usage["total_tokens"].(int)
			cached := totalUsage["prompt_tokens_details"].(map[string]any)
			if usageDetails, ok := usage["prompt_tokens_details"].(map[string]any); ok {
				if add, ok := usageDetails["cached_tokens"].(int); ok {
					cached["cached_tokens"] = cached["cached_tokens"].(int) + add
				}
			}
			reasoning := totalUsage["completion_tokens_details"].(map[string]any)
			if usageDetails, ok := usage["completion_tokens_details"].(map[string]any); ok {
				if add, ok := usageDetails["reasoning_tokens"].(int); ok {
					reasoning["reasoning_tokens"] = reasoning["reasoning_tokens"].(int) + add
				}
			}
			if reply.ResponseID != "" {
				countedIDs[reply.ResponseID] = true
			}
		}
		// Python: if not reply.tool_calls: break
		if !hasToolCalls(reply) {
			break
		}
		// Python: messages.extend([reply, *ToolMessage("生成建议无需使用工具", tool_call_id)])
		messages = append(messages, reply)
		for _, toolCall := range reply.ToolCalls {
			messages = append(messages, llmschema.NewToolMessage("生成建议无需使用工具", toolCall.ID))
		}
	}
	if forkErr != nil {
		f.recordLedgerEvent(epoch, "prompt_suggestion_generation", sessionID, requestID,
			suggestionID, ledgerPayload(record, map[string]any{
				"outcome": "failed", "reason": errorTypeName(forkErr),
				"latency_ms": time.Since(started).Milliseconds(),
			}))
		return
	}

	// Python: 4-5. text = 首个非空回答正文 → select_suggestions
	text := firstNonEmptyContent(replies)
	suggestions, outcome, reason := SelectSuggestions(text)
	latencyMS := time.Since(started).Milliseconds()
	logger.Debug(logComponent).Str("session_id", sessionID).
		Str("outcome", outcome).Str("reason", reason).
		Int("suggestions", len(suggestions)).Msg("prompt suggestion fork finished")
	if len(suggestions) == 0 {
		f.recordLedgerEvent(epoch, "prompt_suggestion_generation", sessionID, requestID,
			suggestionID, ledgerPayload(record, map[string]any{
				"outcome": outcome, "reason": reasonOrNil(reason),
				"text": textOrNil(text), "latency_ms": latencyMS,
			}))
		return
	}

	// Python: 6. 逐条 candidate（MAX_SUGGESTIONS=1，实际一条）
	suggestionIDs := make([]string, 0, len(suggestions))
	suggestionIDs = append(suggestionIDs, suggestionID)
	for range suggestions[1:] {
		suggestionIDs = append(suggestionIDs, newSuggestionID())
	}
	for i, shownText := range suggestions {
		f.recordLedgerEvent(epoch, "prompt_suggestion_generation", sessionID, requestID,
			suggestionIDs[i], ledgerPayload(record, map[string]any{
				"outcome": "candidate", "reason": nil, "text": shownText, "latency_ms": latencyMS,
			}))
	}

	// Python: await asyncio.shield(wrap_future(completion)) —— 完成行先落盘（三道闸之一）
	if completion != nil {
		select {
		case <-completion:
		case <-ctx.Done():
			f.recordLedgerEvent(epoch, "prompt_suggestion_generation", sessionID, requestID,
				suggestionID, ledgerPayload(record, map[string]any{
					"outcome": "cancelled", "latency_ms": time.Since(started).Milliseconds(),
				}))
			return
		}
	}
	if !ConsentEpoch(f.config).Equal(epoch) {
		return
	}

	f.mu.Lock()
	pusher := f.readyPusher
	f.mu.Unlock()
	if pusher == nil {
		// 未接线（server 未装配）则不推；candidate 已在账本，回读可用
		return
	}
	store, err := NewTopicStarterStore(filepath.Join(workspace.AgentWorkspaceDir(), ConsentDBName))
	if err != nil {
		logger.Warn(logComponent).Err(err).Str("session_id", sessionID).
			Msg("prompt suggestion push store open failed")
		return
	}
	defer store.Close()
	f.pushCheckedSuggestions(ctx, pusher, store, sessionID, requestID, suggestionIDs, suggestions)
}

// ledgerPayload record 字典与本次终局字段合并（Python 的 **record 展开等价）。
func ledgerPayload(record map[string]any, extra map[string]any) map[string]any {
	full := make(map[string]any, len(record)+len(extra))
	for key, value := range record {
		full[key] = value
	}
	for key, value := range extra {
		full[key] = value
	}
	return full
}

// errorTypeName 对齐 Python type(exc).__name__ 的近似：取错误类型名。
func errorTypeName(err error) string {
	if err == nil {
		return ""
	}
	return reflect.TypeOf(err).String()
}

// reasonOrNil 空串转 nil（Python 的 reason=None）。
func reasonOrNil(reason string) any {
	if reason == "" {
		return nil
	}
	return reason
}

// textOrNil 空文本转 nil（Python 的 text or None）。
func textOrNil(text string) any {
	if text == "" {
		return nil
	}
	return text
}

// pushCheckedSuggestions 逐条过保鲜闸后推送；单条失败不影响其余。
// Python: prompt_suggestion.py:639-670 _push_checked_suggestions
func (f *PromptSuggestionForks) pushCheckedSuggestions(ctx context.Context, pusher SuggestionReadyPusher,
	store *TopicStarterStore, sessionID, requestID string, suggestionIDs, suggestions []string,
) {
	for i, shownText := range suggestions {
		func() {
			defer func() {
				if recovered := recover(); recovered != nil {
					logger.Warn(logComponent).Any("recover", recovered).
						Str("session_id", sessionID).Str("suggestion_id", suggestionIDs[i]).
						Msg("prompt suggestion delivery failed")
				}
			}()
			shown := StageDelivery(f, store, sessionID, requestID, map[string]any{
				"suggestion_id": suggestionIDs[i], "text": shownText,
			})
			if shown == nil {
				return
			}
			pusher.PushReady(ctx, SuggestionReadyEvent{
				SessionID: sessionID, RequestID: requestID,
				SuggestionID: suggestionIDs[i], Text: shownText,
			})
		}()
	}
}

// invokeMainCall 用父调用的模型、工具与全部参数重放一次。
// Python: prompt_suggestion.py:258 _MainCall.invoke
func invokeMainCall(
	ctx context.Context, call *MainCall, messages []llmschema.BaseMessage,
) (*llmschema.AssistantMessage, error) {
	options := []model_clients.InvokeOption{
		model_clients.WithInvokeModel(call.Model),
	}
	// Python: tools=call.tools or None
	if len(call.Tools) > 0 {
		options = append(options, model_clients.WithTools(call.Tools...))
	}
	// Python: **call.parameters —— 父调用的全部非默认 kwarg
	if len(call.Parameters) > 0 {
		options = append(options, model_clients.WithInvokeExtra(call.Parameters))
	}
	return call.LLM.Invoke(ctx, model_clients.NewMessagesParam(messages...), options...)
}

// firstNonEmptyContent 返回首个非空回答正文。
// Python: next((r.content.strip() for r in replies if isinstance(r.content, str) and r.content.strip()), "")
func firstNonEmptyContent(replies []*llmschema.AssistantMessage) string {
	for _, reply := range replies {
		if reply == nil {
			continue
		}
		if text := reply.GetContent().Text(); strings.TrimSpace(text) != "" {
			return strings.TrimSpace(text)
		}
	}
	return ""
}

// newSuggestionID 生成 32 位十六进制建议标识。
// Python: uuid.uuid4().hex
func newSuggestionID() string {
	buffer := make([]byte, 16)
	if _, err := rand.Read(buffer); err != nil {
		return ""
	}
	return hex.EncodeToString(buffer)
}

// maxInt 返回两者较大值。
func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// InFlightCount 返回在飞生成任务数（运维与集成测试用）。
// Python: prompt_suggestion.py:417 in_flight 的计数等价物。
func (f *PromptSuggestionForks) InFlightCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.tasks)
}

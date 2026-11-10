package recommendation

import (
	"context"
	"sync"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/model_clients"
	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	commonschema "github.com/uapclaw/uapclaw-go/internal/common/schema"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ForkResultPublisher 建议结果落库口，10.7 实现并注入。
// Python: prompt_suggestion.py:326 _record（内部 TopicStarterStore.record_event）
type ForkResultPublisher interface {
	PublishForkResult(ctx context.Context, result ForkResult)
}

// SuggestionReadyPusher 建议就绪推帧口，10.7 / 网关章节实现并注入。
// Python: prompt_suggestion.py READY_EVENT 推送
type SuggestionReadyPusher interface {
	PushReady(ctx context.Context, event SuggestionReadyEvent)
}

// ForkResult 一次重放的结果（未注入 publisher 时仅记 Debug）。
// Python: prompt_suggestion.py:447 _fork 里的 record 字典
type ForkResult struct {
	// SuggestionID 建议唯一标识（uuid4().hex 等价）
	SuggestionID string
	// SessionID 会话标识
	SessionID string
	// RequestID 产生该建议的请求标识
	RequestID string
	// Text 建议文本；空串表示模型返回 null
	Text string
	// Model 父调用使用的模型名
	Model string
	// ParentUncachedTokens 父调用新增（未命中缓存）的 token 数
	ParentUncachedTokens int
	// AnswerTokens 父调用回答的 token 数
	AnswerTokens int
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
	// publisher 结果落库口；nil 时丢弃（spec DEV-10）
	publisher ForkResultPublisher
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

// ──────────────────────────── 导出函数 ────────────────────────────

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
	if epoch == 0 {
		f.mu.Unlock()
		return
	}
	// Python: 5. 判定抑制原因；有原因则记 suppressed 并返回
	reason := suppressReason(call, opts)
	f.lastSuppressedReason = reason
	if reason != "" {
		logger.Debug(logComponent).Str("session_id", sessionID).
			Str("reason", reason).Msg("prompt suggestion generation suppressed")
		f.mu.Unlock()
		return
	}
	// Python: 6. task = asyncio.create_task(self._fork(...)); self._tasks[session_id] = task
	ctx, cancel := context.WithCancel(context.Background())
	f.tasks[sessionID] = cancel
	f.mu.Unlock()
	go f.runFork(ctx, sessionID, requestID, call, epoch)
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

// SetForkResultPublisher 注入结果落库口（10.7 调用）。
func (f *PromptSuggestionForks) SetForkResultPublisher(publisher ForkResultPublisher) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.publisher = publisher
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

// runFork 重放父调用并解析建议（Task 8 补全模型重放与窄接口落库）。
// Python: prompt_suggestion.py:447 _fork
// 本 Task 先落「取消即退出」的最小真实实现，Task 8 在其内部补重放。
func (f *PromptSuggestionForks) runFork(
	ctx context.Context, sessionID, requestID string, call *MainCall, epoch int64,
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
	if epoch == 0 || ConsentEpoch(f.config) != epoch {
		return
	}
	select {
	case <-ctx.Done():
		return
	default:
	}
}

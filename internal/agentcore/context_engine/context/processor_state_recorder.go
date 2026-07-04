package context

import (
	"context"
	"fmt"
	"math"
	"time"

	iface "github.com/uapclaw/uapclaw-go/internal/agentcore/context_engine/interface"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/context_engine/schema"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/context_engine/token"
	llm_schema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/runner/callback"
	sessioninterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/session/interfaces"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/stream"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ProcessorStateInput 处理器状态输入数据，记录一次处理器执行的关键信息。
//
// 对应 Python: openjiuwen/core/context_engine/context/processor_state_recorder.py (ContextProcessorStateInput)
type ProcessorStateInput struct {
	// OperationID 操作唯一标识
	OperationID string
	// Status 操作状态
	Status schema.CompressionStatus
	// Phase 操作阶段
	Phase schema.CompressionPhase
	// Trigger 触发方式
	Trigger string
	// Processor 处理器实例
	Processor iface.ContextProcessor
	// Reason 原因描述
	Reason string
	// BeforeMessages 处理前消息列表
	BeforeMessages []llm_schema.BaseMessage
	// AfterMessages 处理后消息列表
	AfterMessages []llm_schema.BaseMessage
	// StartedAt 开始时间
	StartedAt time.Time
	// EndedAt 结束时间
	EndedAt time.Time
	// Error 错误信息
	Error string
	// MessagesToModify 修改的消息索引列表
	MessagesToModify []int
	// Force 是否强制执行
	Force bool
	// ContextMax 上下文最大 token 数
	ContextMax int
	// CompactSummary 压缩摘要
	CompactSummary string
	// CompressionUsage 压缩用量信息
	CompressionUsage *schema.ContextCompressionUsage
}

// summaryInput 摘要构建输入
type summaryInput struct {
	status           schema.CompressionStatus
	before           schema.ContextCompressionMetric
	after            *schema.ContextCompressionMetric
	saved            *schema.ContextCompressionSaved
	reason           string
	messagesToModify []int
}

// ProcessorStateRecorder 记录上下文处理器状态变化，包括日志、回调触发、流式推送和历史记录。
//
// 对应 Python: openjiuwen/core/context_engine/context/processor_state_recorder.py (ContextProcessorStateRecorder)
type ProcessorStateRecorder struct {
	// sessionID 会话 ID
	sessionID string
	// contextID 上下文 ID
	contextID string
	// getSessionRef 获取会话引用的回调函数
	getSessionRef func() sessioninterfaces.SessionFacade
	// tokenCounter Token 计数器
	tokenCounter token.TokenCounter
	// historyLimit 历史记录上限
	historyLimit int
	// history 压缩状态历史记录
	history []map[string]any
}

// ──────────────────────────── 导出函数 ────────────────────────────

// NewProcessorStateRecorder 创建处理器状态记录器实例。
//
// 对应 Python: ContextProcessorStateRecorder.__init__()
func NewProcessorStateRecorder(sessionID, contextID string, getSessionRef func() sessioninterfaces.SessionFacade, tokenCounter token.TokenCounter, historyLimit int) *ProcessorStateRecorder {
	return &ProcessorStateRecorder{
		sessionID:     sessionID,
		contextID:     contextID,
		getSessionRef: getSessionRef,
		tokenCounter:  tokenCounter,
		historyLimit:  historyLimit,
		history:       make([]map[string]any, 0),
	}
}

// History 返回历史记录副本。
//
// 对应 Python: ContextProcessorStateRecorder.history 属性
func (r *ProcessorStateRecorder) History() []map[string]any {
	result := make([]map[string]any, len(r.history))
	copy(result, r.history)
	return result
}

// LoadHistory 加载外部历史记录，截取最后 historyLimit 条。
//
// 对应 Python: ContextProcessorStateRecorder._load_history()
func (r *ProcessorStateRecorder) LoadHistory(history []map[string]any) {
	if len(history) == 0 {
		r.history = make([]map[string]any, 0)
		return
	}
	r.history = make([]map[string]any, len(history))
	copy(r.history, history)
	if len(r.history) > r.historyLimit {
		r.history = r.history[len(r.history)-r.historyLimit:]
	}
}

// Emit 记录状态、触发回调和流式推送。
//
// 流程：record(state) → logger.Info → callback.TriggerContext → sessionRef.WriteStream
//
// 对应 Python: ContextProcessorStateRecorder.emit()
func (r *ProcessorStateRecorder) Emit(ctx context.Context, state *schema.ContextCompressionState) {
	// 1. 追加到历史记录
	r.record(state)

	// 2. 记录日志
	logger.Info(logComponent).
		Str("status", string(state.Status)).
		Str("phase", string(state.Phase)).
		Str("processor", state.Processor).
		Str("model", state.Model).
		Str("summary", state.Summary).
		Msg("上下文压缩状态")

	// 3. 触发回调
	callback.GetCallbackFramework().TriggerContext(ctx, &callback.ContextCallEventData{
		Event:     callback.ContextCompressionStateEvent,
		SessionID: r.sessionID,
		ContextID: r.contextID,
		State:     state,
	})

	// 4. 推送流式数据
	sessionRef := r.getSessionRef()
	if sessionRef != nil {
		stateMap := r.stateToMap(state)
		_ = sessionRef.WriteStream(context.Background(), stream.OutputSchema{
			Type:    schema.ContextCompressionStateType,
			Payload: stateMap,
		})
	}
}

// BuildState 构建压缩状态对象。
//
// 对应 Python: ContextProcessorStateRecorder.build_state()
func (r *ProcessorStateRecorder) BuildState(input ProcessorStateInput) *schema.ContextCompressionState {
	// a. 构建 before 指标
	before := r.buildMetric(input.BeforeMessages, input.ContextMax, input.StartedAt)

	// b. 构建 after 指标（如果 AfterMessages 不为 nil）
	var after *schema.ContextCompressionMetric
	if input.AfterMessages != nil {
		m := r.buildMetric(input.AfterMessages, input.ContextMax, input.EndedAt)
		after = &m
	}

	// c. 构建 saved 指标
	saved := r.buildSaved(before, after)

	// d. 构建 ContextCompressionState
	state := &schema.ContextCompressionState{
		Type:             schema.ContextCompressionStateType,
		OperationID:      input.OperationID,
		Status:           input.Status,
		Phase:            input.Phase,
		Before:           before,
		After:            after,
		Saved:            saved,
		CompressionUsage: input.CompressionUsage,
		ContextMax:       input.ContextMax,
		CompactSummary:   input.CompactSummary,
		Error:            input.Error,
	}

	// e. 设置 DurationMs
	state.DurationMs = int(input.EndedAt.Sub(input.StartedAt).Milliseconds())

	// f. 设置 Summary
	state.Summary = r.buildSummary(summaryInput{
		status:           input.Status,
		before:           before,
		after:            after,
		saved:            saved,
		reason:           input.Reason,
		messagesToModify: input.MessagesToModify,
	})

	// g. 设置 Processor
	if input.Processor != nil {
		state.Processor = input.Processor.ProcessorType()
	}

	// h. 设置 Model
	state.Model = resolveModelName(input.Processor, input.Trigger, input.Force)

	// i. 设置 Statistic，对齐 Python build_state(statistic=...)
	statisticMessages := input.AfterMessages
	if statisticMessages == nil {
		statisticMessages = input.BeforeMessages
	}
	state.Statistic = r.buildStatistic(statisticMessages)

	return state
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// buildMetric 构建压缩指标快照。
//
// 对应 Python: ContextProcessorStateRecorder._build_metric()
func (r *ProcessorStateRecorder) buildMetric(messages []llm_schema.BaseMessage, contextMax int, observedAt time.Time) schema.ContextCompressionMetric {
	tokens := r.measureMessages(messages)
	return schema.ContextCompressionMetric{
		Time:           formatTime(observedAt),
		Messages:       len(messages),
		Tokens:         tokens,
		ContextPercent: contextPercent(tokens, contextMax),
	}
}

// measureMessages 测量消息列表的 Token 数量。
//
// 优先使用 tokenCounter，失败时降级为字符数/4 向上取整。
//
// 对应 Python: ContextProcessorStateRecorder._measure_messages()
func (r *ProcessorStateRecorder) measureMessages(messages []llm_schema.BaseMessage) int {
	if r.tokenCounter != nil {
		count, err := r.tokenCounter.CountMessages(messages, "")
		if err == nil {
			return count
		}
	}
	// 降级：字符数/4 向上取整
	totalChars := 0
	for _, msg := range messages {
		totalChars += len(msg.GetContent().Text())
	}
	if totalChars == 0 {
		return 0
	}
	return int(math.Ceil(float64(totalChars) / 4))
}

// buildStatistic 构建上下文统计快照。
//
// 对应 Python: ContextProcessorStateRecorder._build_statistic()
func (r *ProcessorStateRecorder) buildStatistic(messages []llm_schema.BaseMessage) iface.ContextStats {
	stat := iface.ContextStats{}
	for _, msg := range messages {
		stat.TotalMessages++
		tokens := r.countMessageForStatistic(msg)
		switch msg.GetRole() {
		case llm_schema.RoleTypeAssistant:
			stat.AssistantMessages++
			stat.AssistantMessageTokens += tokens
		case llm_schema.RoleTypeUser:
			stat.UserMessages++
			stat.UserMessageTokens += tokens
		case llm_schema.RoleTypeSystem:
			stat.SystemMessages++
			stat.SystemMessageTokens += tokens
		case llm_schema.RoleTypeTool:
			stat.ToolMessages++
			stat.ToolMessageTokens += tokens
		}
		stat.TotalTokens += tokens
	}
	return stat
}

// countMessageForStatistic 统计单条消息的 token 数，用于构建 statistic。
//
// 对应 Python: ContextProcessorStateRecorder._count_message_for_statistic()
func (r *ProcessorStateRecorder) countMessageForStatistic(msg llm_schema.BaseMessage) int {
	if r.tokenCounter != nil {
		count, err := r.tokenCounter.CountMessages([]llm_schema.BaseMessage{msg}, "")
		if err == nil {
			return count
		}
	}
	return len(msg.GetContent().Text()) / 4
}

// buildSaved 构建压缩节省量指标。
//
// 对应 Python: ContextProcessorStateRecorder._build_saved()
func (r *ProcessorStateRecorder) buildSaved(before schema.ContextCompressionMetric, after *schema.ContextCompressionMetric) *schema.ContextCompressionSaved {
	if after == nil {
		return nil
	}
	savedMessages := before.Messages - after.Messages
	savedTokens := before.Tokens - after.Tokens
	var percent float64
	if before.Tokens > 0 {
		percent = math.Round(float64(savedTokens)/float64(before.Tokens)*1000) / 10
	}
	return &schema.ContextCompressionSaved{
		Messages: savedMessages,
		Tokens:   savedTokens,
		Percent:  percent,
	}
}

// buildSummary 构建人类可读的操作摘要。
//
// 对应 Python: ContextProcessorStateRecorder._build_summary()
func (r *ProcessorStateRecorder) buildSummary(input summaryInput) string {
	var summary string

	switch input.status {
	case schema.CompressionStarted:
		summary = fmt.Sprintf("正在压缩 %d 条消息，约 %s tokens",
			input.before.Messages, compactNumber(input.before.Tokens))

	case schema.CompressionFailed:
		summary = fmt.Sprintf("上下文处理器执行失败，上下文约 %s tokens",
			compactNumber(input.before.Tokens))

	default:
		if input.after == nil || input.saved == nil {
			summary = fmt.Sprintf("上下文处理器已跳过: %s", input.reason)
		} else if input.status == schema.CompressionNoop {
			summary = fmt.Sprintf("上下文未变化，约 %s tokens (节省 %.1f%%)",
				compactNumber(input.after.Tokens), input.saved.Percent)
		} else {
			// completed 或其他状态
			summary = fmt.Sprintf("压缩 %d → %d 条消息，约 %s → %s tokens，节省约 %s tokens (%.1f%%)",
				input.before.Messages, input.after.Messages,
				compactNumber(input.before.Tokens), compactNumber(input.after.Tokens),
				compactNumber(input.saved.Tokens), input.saved.Percent)
		}
	}

	// 追加修改消息信息
	if len(input.messagesToModify) > 0 {
		summary += fmt.Sprintf("，修改了 %d 条消息", len(input.messagesToModify))
	}

	return summary
}

// record 追加状态到历史记录，超过 historyLimit 时截取。
//
// 对应 Python: ContextProcessorStateRecorder._record()
func (r *ProcessorStateRecorder) record(state *schema.ContextCompressionState) {
	stateMap := r.stateToMap(state)
	r.history = append(r.history, stateMap)
	if len(r.history) > r.historyLimit {
		r.history = r.history[len(r.history)-r.historyLimit:]
	}
}

// compactNumber 将数字格式化为紧凑表示。
//
// >=1M → "X.Xm"，>=1K → "X.Xk"，否则原样。
//
// 对应 Python: ContextProcessorStateRecorder._compact_number()
func compactNumber(value int) string {
	if value >= 1_000_000 {
		return fmt.Sprintf("%.1fm", float64(value)/1_000_000)
	}
	if value >= 1_000 {
		return fmt.Sprintf("%.1fk", float64(value)/1_000)
	}
	return fmt.Sprintf("%d", value)
}

// formatTime 将时间格式化为 ISO 8601 毫秒精度字符串。
//
// 对应 Python: ContextProcessorStateRecorder._format_time()
func formatTime(t time.Time) string {
	return t.Format("2006-01-02T15:04:05.000Z07:00")
}

// contextPercent 计算上下文使用百分比。
//
// contextMax <= 0 返回 0；否则 clamp 到 [0, 100]。
//
// 对应 Python: ContextProcessorStateRecorder._context_percent()
func contextPercent(tokens, contextMax int) int {
	if contextMax <= 0 {
		return 0
	}
	pct := int(math.Round(float64(tokens) / float64(contextMax) * 100))
	if pct < 0 {
		return 0
	}
	if pct > 100 {
		return 100
	}
	return pct
}

// resolveModelName 解析处理器使用的模型名称。
//
// 优先从 config.model.model_name/model 提取，对齐 Python _resolve_model_name()。
// Go 无反射 getattr，通过 ProcessorConfig 接口的可选 ModelName() 方法获取，
// 若 config 未实现该方法则回退到 BaseProcessor.Config() 上继续尝试。
//
// 对应 Python: ContextProcessorStateRecorder._resolve_model_name()
func resolveModelName(proc iface.ContextProcessor, trigger string, force bool) string {
	_ = trigger
	_ = force
	if proc == nil {
		return ""
	}

	// 尝试从 processor 获取 BaseProcessor 的 Config()
	type configProvider interface {
		Config() iface.ProcessorConfig
	}
	if cp, ok := proc.(configProvider); ok {
		cfg := cp.Config()
		if cfg != nil {
			// 尝试 config 上的 ModelName() 方法
			type modelNameProvider interface {
				ModelName() string
			}
			if mp, ok := cfg.(modelNameProvider); ok {
				if name := mp.ModelName(); name != "" {
					return name
				}
			}
			// 尝试从 config.model 上获取 model_name
			type modelConfigProvider interface {
				GetModel() any
			}
			if mcp, ok := cfg.(modelConfigProvider); ok {
				if modelCfg := mcp.GetModel(); modelCfg != nil {
					if mp, ok := modelCfg.(modelNameProvider); ok {
						if name := mp.ModelName(); name != "" {
							return name
						}
					}
				}
			}
		}
	}
	return ""
}

// stateToMap 将 ContextCompressionState 转换为 map，用于流式推送和历史记录。
func (r *ProcessorStateRecorder) stateToMap(state *schema.ContextCompressionState) map[string]any {
	m := map[string]any{
		"type":            state.Type,
		"operation_id":    state.OperationID,
		"status":          string(state.Status),
		"phase":           string(state.Phase),
		"processor":       state.Processor,
		"model":           state.Model,
		"duration_ms":     state.DurationMs,
		"context_max":     state.ContextMax,
		"summary":         state.Summary,
		"compact_summary": state.CompactSummary,
	}

	// 前置处理
	m["before"] = map[string]any{
		"time":            state.Before.Time,
		"messages":        state.Before.Messages,
		"tokens":          state.Before.Tokens,
		"context_percent": state.Before.ContextPercent,
	}

	// After（可能为 nil）
	if state.After != nil {
		m["after"] = map[string]any{
			"time":            state.After.Time,
			"messages":        state.After.Messages,
			"tokens":          state.After.Tokens,
			"context_percent": state.After.ContextPercent,
		}
	}

	// Saved（可能为 nil）
	if state.Saved != nil {
		m["saved"] = map[string]any{
			"messages": state.Saved.Messages,
			"tokens":   state.Saved.Tokens,
			"percent":  state.Saved.Percent,
		}
	}

	// 统计
	m["statistic"] = map[string]any{
		"total_messages":           state.Statistic.TotalMessages,
		"total_tokens":             state.Statistic.TotalTokens,
		"total_dialogues":          state.Statistic.TotalDialogues,
		"system_messages":          state.Statistic.SystemMessages,
		"user_messages":            state.Statistic.UserMessages,
		"assistant_messages":       state.Statistic.AssistantMessages,
		"tool_messages":            state.Statistic.ToolMessages,
		"tools":                    state.Statistic.Tools,
		"system_message_tokens":    state.Statistic.SystemMessageTokens,
		"user_message_tokens":      state.Statistic.UserMessageTokens,
		"assistant_message_tokens": state.Statistic.AssistantMessageTokens,
		"tool_message_tokens":      state.Statistic.ToolMessageTokens,
		"tool_tokens":              state.Statistic.ToolTokens,
	}

	// CompressionUsage（可能为 nil）
	if state.CompressionUsage != nil {
		m["compression_usage"] = map[string]any{
			"calls":         state.CompressionUsage.Calls,
			"input_tokens":  state.CompressionUsage.InputTokens,
			"output_tokens": state.CompressionUsage.OutputTokens,
			"total_tokens":  state.CompressionUsage.TotalTokens,
			"cache_tokens":  state.CompressionUsage.CacheTokens,
			"input_cost":    state.CompressionUsage.InputCost,
			"output_cost":   state.CompressionUsage.OutputCost,
			"total_cost":    state.CompressionUsage.TotalCost,
			"model_name":    state.CompressionUsage.ModelName,
		}
	}

	// Error（非空时包含）
	if state.Error != "" {
		m["error"] = state.Error
	}

	return m
}

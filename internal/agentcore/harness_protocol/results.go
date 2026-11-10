package harness_protocol

import (
	"math"
	"strings"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ContentBlock 一条有序的 provider-neutral 消息内容块。
// Python: results.py:47-60 ContentBlock
type ContentBlock struct {
	// BlockID 块标识
	BlockID string
	// Kind 块类型
	Kind string
	// Content 块内容（已冻结）
	Content JsonValue
	// Data 附加数据（已冻结）
	Data JsonObject
}

// TurnMessage 终态结果保留的一条归一化消息。
// Python: results.py:63-76 TurnMessage
type TurnMessage struct {
	// MessageID 消息标识
	MessageID string
	// Role 可移植角色
	Role MessageRole
	// Content 内容块序列
	Content []ContentBlock
	// Data 附加数据（已冻结）
	Data JsonObject
}

// TurnTermination 被中断轮次的结构化原因。
// Python: results.py:79-89 TurnTermination
type TurnTermination struct {
	// Kind 终止类别
	Kind TurnTerminationKind
	// Message 说明（可为 nil）
	Message *string
	// Code 错误码（可为 nil）
	Code *string
	// ProviderData provider 扩展数据（已冻结）
	ProviderData JsonObject
}

// MonetaryAmount 以百万分之一货币单位表示的精确金额。
// Python: results.py:92-105 MonetaryAmount
type MonetaryAmount struct {
	// Micros 微单位金额（非负）
	Micros int64
	// Currency 三位货币代码（大写）
	Currency string
}

// TurnUsage 归一化 token 用量，保留 provider 特有计数器。
// Python: results.py:108-129 TurnUsage
type TurnUsage struct {
	// InputTokens 输入 token（可为 nil）
	InputTokens *int
	// OutputTokens 输出 token（可为 nil）
	OutputTokens *int
	// CachedInputTokens 缓存输入 token（可为 nil）
	CachedInputTokens *int
	// ReasoningOutputTokens 推理输出 token（可为 nil）
	ReasoningOutputTokens *int
	// TotalTokens 总 token（可为 nil）
	TotalTokens *int
	// ProviderData provider 扩展数据（已冻结）
	ProviderData JsonObject
}

// TurnError 一轮的归一化失败信息。
// Python: results.py:132-145 TurnError
type TurnError struct {
	// Message 错误说明
	Message string
	// Code 错误码
	Code *string
	// Category 类别
	Category *string
	// Retryable 是否可重试
	Retryable *bool
	// ProviderData provider 扩展数据（已冻结）
	ProviderData JsonObject
}

// TurnResult 一轮完整的 provider-neutral 终态结果。
// Messages 是无损归一化输出；FinalOutput / StructuredOutput 是便捷投影。
// Python: results.py:148-189 TurnResult
type TurnResult struct {
	// Status 终态
	Status TurnStatus
	// Messages 归一化消息序列
	Messages []TurnMessage
	// FinalOutput 便捷终态输出（已冻结）
	FinalOutput JsonValue
	// StructuredOutput 便捷结构化输出（已冻结）
	StructuredOutput JsonValue
	// StopReason 停止原因
	StopReason *string
	// Termination 中断原因（仅 interrupted）
	Termination *TurnTermination
	// Error 失败信息（仅 failed）
	Error *TurnError
	// Usage token 用量
	Usage *TurnUsage
	// Cost 精确金额
	Cost *MonetaryAmount
	// StartedAt 开始 Unix 时间戳
	StartedAt *float64
	// CompletedAt 结束 Unix 时间戳
	CompletedAt *float64
	// DurationMS 单调耗时毫秒
	DurationMS *int
	// ProviderData provider 扩展数据（已冻结）
	ProviderData JsonObject
}

// ResultOptions TurnResult 的可选字段。
// Python: results.py:156-168（dataclass 默认值）
type ResultOptions struct {
	// Messages 归一化消息序列
	Messages []TurnMessage
	// FinalOutput 便捷终态输出
	FinalOutput JsonValue
	// StructuredOutput 便捷结构化输出
	StructuredOutput JsonValue
	// StopReason 停止原因
	StopReason *string
	// Termination 中断原因
	Termination *TurnTermination
	// Error 失败信息
	Error *TurnError
	// Usage token 用量
	Usage *TurnUsage
	// Cost 精确金额
	Cost *MonetaryAmount
	// StartedAt 开始时间戳
	StartedAt *float64
	// CompletedAt 结束时间戳
	CompletedAt *float64
	// DurationMS 单调耗时毫秒
	DurationMS *int
	// ProviderData provider 扩展数据
	ProviderData JsonObject
}

// ──────────────────────────── 枚举 ────────────────────────────

// TurnStatus 一轮的终态。
// Python: results.py:20-25 TurnStatus（3 项）
type TurnStatus string

const (
	// TurnStatusCompleted 正常完成
	TurnStatusCompleted TurnStatus = "completed"
	// TurnStatusInterrupted 被中断
	TurnStatusInterrupted TurnStatus = "interrupted"
	// TurnStatusFailed 失败
	TurnStatusFailed TurnStatus = "failed"
)

// MessageRole 终态结果中保留消息的可移植角色。
// Python: results.py:28-34 MessageRole（4 项）
type MessageRole string

const (
	// MessageRoleUser 用户
	MessageRoleUser MessageRole = "user"
	// MessageRoleAssistant 助手
	MessageRoleAssistant MessageRole = "assistant"
	// MessageRoleSystem 系统
	MessageRoleSystem MessageRole = "system"
	// MessageRoleTool 工具
	MessageRoleTool MessageRole = "tool"
)

// TurnTerminationKind 未正常完成的终止原因。
// Python: results.py:37-44 TurnTerminationKind（5 项）
type TurnTerminationKind string

const (
	// TurnTerminationKindUserAbort 用户中止
	TurnTerminationKindUserAbort TurnTerminationKind = "user_abort"
	// TurnTerminationKindTimeout 超时
	TurnTerminationKindTimeout TurnTerminationKind = "timeout"
	// TurnTerminationKindPolicy 策略拦截
	TurnTerminationKindPolicy TurnTerminationKind = "policy"
	// TurnTerminationKindProvider provider 侧终止
	TurnTerminationKindProvider TurnTerminationKind = "provider"
	// TurnTerminationKindHarnessStop harness 主动停止
	TurnTerminationKindHarnessStop TurnTerminationKind = "harness_stop"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// NewContentBlock 创建内容块。
// Python: results.py:56-60
func NewContentBlock(blockID, kind string, content JsonValue, data JsonObject) (ContentBlock, error) {
	// 步骤 1：id 与 kind 非空
	if blockID == "" || kind == "" {
		return ContentBlock{}, &HarnessProtocolError{Msg: "content block id and kind must not be empty"}
	}
	// 步骤 2：冻结 content
	frozenContent, err := FreezeJsonValue(content)
	if err != nil {
		return ContentBlock{}, err
	}
	// 步骤 3：冻结 data
	frozenData, err := FreezeJsonObject(data)
	if err != nil {
		return ContentBlock{}, err
	}
	return ContentBlock{BlockID: blockID, Kind: kind, Content: frozenContent, Data: frozenData}, nil
}

// NewTurnMessage 创建轮次消息。
// Python: results.py:72-76
func NewTurnMessage(messageID string, role MessageRole, content []ContentBlock, data JsonObject) (TurnMessage, error) {
	// 步骤 1：message_id 非空
	if messageID == "" {
		return TurnMessage{}, &HarnessProtocolError{Msg: "turn message_id must not be empty"}
	}
	// 步骤 2：content 拷贝为独立切片
	copied := make([]ContentBlock, len(content))
	copy(copied, content)
	// 步骤 3：冻结 data
	frozenData, err := FreezeJsonObject(data)
	if err != nil {
		return TurnMessage{}, err
	}
	return TurnMessage{MessageID: messageID, Role: role, Content: copied, Data: frozenData}, nil
}

// NewTurnTermination 创建中断原因。
// Python: results.py:88-89
func NewTurnTermination(kind TurnTerminationKind, message, code *string, providerData JsonObject) (TurnTermination, error) {
	frozen, err := FreezeJsonObject(providerData)
	if err != nil {
		return TurnTermination{}, err
	}
	return TurnTermination{Kind: kind, Message: message, Code: code, ProviderData: frozen}, nil
}

// NewMonetaryAmount 创建金额。
// Python: results.py:99-105
func NewMonetaryAmount(micros int64, currency string) (MonetaryAmount, error) {
	// 步骤 1：micros 非负
	if micros < 0 {
		return MonetaryAmount{}, &HarnessProtocolError{Msg: "monetary micros must be non-negative"}
	}
	// 步骤 2：货币码转大写
	upper := strings.ToUpper(currency)
	// 步骤 3：三位且全字母
	if len(upper) != 3 || !isAlpha(upper) {
		return MonetaryAmount{}, &HarnessProtocolError{Msg: "monetary currency must be a three-letter code"}
	}
	return MonetaryAmount{Micros: micros, Currency: upper}, nil
}

// NewTurnUsage 创建 token 用量。
// Python: results.py:119-129
func NewTurnUsage(
	inputTokens, outputTokens, cachedInputTokens, reasoningOutputTokens, totalTokens *int,
	providerData JsonObject,
) (TurnUsage, error) {
	// 步骤 1：5 个计数均需非负
	for _, counter := range []*int{inputTokens, outputTokens, cachedInputTokens, reasoningOutputTokens, totalTokens} {
		if counter != nil && *counter < 0 {
			return TurnUsage{}, &HarnessProtocolError{Msg: "turn usage counters must be non-negative"}
		}
	}
	// 步骤 2：冻结 provider_data
	frozen, err := FreezeJsonObject(providerData)
	if err != nil {
		return TurnUsage{}, err
	}
	return TurnUsage{
		InputTokens:           inputTokens,
		OutputTokens:          outputTokens,
		CachedInputTokens:     cachedInputTokens,
		ReasoningOutputTokens: reasoningOutputTokens,
		TotalTokens:           totalTokens,
		ProviderData:          frozen,
	}, nil
}

// NewTurnError 创建失败信息。
// Python: results.py:142-145
func NewTurnError(message string, code, category *string, retryable *bool, providerData JsonObject) (TurnError, error) {
	// 步骤 1：message 非空
	if message == "" {
		return TurnError{}, &HarnessProtocolError{Msg: "turn error message must not be empty"}
	}
	// 步骤 2：冻结 provider_data
	frozen, err := FreezeJsonObject(providerData)
	if err != nil {
		return TurnError{}, err
	}
	return TurnError{Message: message, Code: code, Category: category, Retryable: retryable, ProviderData: frozen}, nil
}

// NewTurnResult 创建轮次终态结果，10 步校验顺序与上游一致。
// Python: results.py:170-189
func NewTurnResult(status TurnStatus, opts ResultOptions) (TurnResult, error) {
	// 步骤 1：messages 拷贝为独立切片
	messages := make([]TurnMessage, len(opts.Messages))
	copy(messages, opts.Messages)
	// 步骤 2：冻结 final_output
	frozenFinal, err := FreezeJsonValue(opts.FinalOutput)
	if err != nil {
		return TurnResult{}, err
	}
	// 步骤 3：冻结 structured_output
	frozenStructured, err := FreezeJsonValue(opts.StructuredOutput)
	if err != nil {
		return TurnResult{}, err
	}
	// 步骤 4：冻结 provider_data
	frozenProviderData, err := FreezeJsonObject(opts.ProviderData)
	if err != nil {
		return TurnResult{}, err
	}
	// 步骤 5：failed 必须有 error
	if status == TurnStatusFailed && opts.Error == nil {
		return TurnResult{}, &HarnessProtocolError{Msg: "failed turn result requires error"}
	}
	// 步骤 6：非 failed 不得有 error
	if status != TurnStatusFailed && opts.Error != nil {
		return TurnResult{}, &HarnessProtocolError{Msg: "only failed turn result may contain error"}
	}
	// 步骤 7：interrupted 必须有 termination
	if status == TurnStatusInterrupted && opts.Termination == nil {
		return TurnResult{}, &HarnessProtocolError{Msg: "interrupted turn result requires termination"}
	}
	// 步骤 8：非 interrupted 不得有 termination
	if status != TurnStatusInterrupted && opts.Termination != nil {
		return TurnResult{}, &HarnessProtocolError{Msg: "only interrupted turn result may contain termination"}
	}
	// 步骤 9：时间戳有限且非负
	for _, ts := range []*float64{opts.StartedAt, opts.CompletedAt} {
		if ts != nil && (math.IsNaN(*ts) || math.IsInf(*ts, 0) || *ts < 0) {
			return TurnResult{}, &HarnessProtocolError{Msg: "turn timestamp must be a finite Unix timestamp"}
		}
	}
	// 步骤 10a：completed 不得早于 started
	if opts.StartedAt != nil && opts.CompletedAt != nil && *opts.CompletedAt < *opts.StartedAt {
		return TurnResult{}, &HarnessProtocolError{Msg: "turn completed_at must not precede started_at"}
	}
	// 步骤 10b：duration_ms 非负
	if opts.DurationMS != nil && *opts.DurationMS < 0 {
		return TurnResult{}, &HarnessProtocolError{Msg: "turn duration_ms must be non-negative monotonic elapsed time"}
	}
	return TurnResult{
		Status:           status,
		Messages:         messages,
		FinalOutput:      frozenFinal,
		StructuredOutput: frozenStructured,
		StopReason:       opts.StopReason,
		Termination:      opts.Termination,
		Error:            opts.Error,
		Usage:            opts.Usage,
		Cost:             opts.Cost,
		StartedAt:        opts.StartedAt,
		CompletedAt:      opts.CompletedAt,
		DurationMS:       opts.DurationMS,
		ProviderData:     frozenProviderData,
	}, nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// isAlpha 判断字符串是否全为 ASCII 字母。
// Python: results.py:103 currency.isalpha()
func isAlpha(s string) bool {
	for _, r := range s {
		if (r < 'a' || r > 'z') && (r < 'A' || r > 'Z') {
			return false
		}
	}
	return true
}

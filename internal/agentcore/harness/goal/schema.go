// 本文件对应 Python openjiuwen/harness/goal/schema.py，
// 提供持久化 Goal 领域模型与能力错误契约。
package goal

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
)

// ──────────────────────────── 结构体 ────────────────────────────

// GoalStatus 目标状态。
// Python: GoalStatus (schema.py L14)
type GoalStatus string

// GoalAssessmentStatus 一次评估的结论状态。
// Python: GoalAssessmentStatus (schema.py L21)
type GoalAssessmentStatus string

// GoalStopStrategy 停止策略。
// Python: GoalStopStrategy (schema.py L27)
type GoalStopStrategy string

// GoalAssessment 一次完成度评估的结果。
// Python: GoalAssessment (schema.py L101)
type GoalAssessment struct {
	// Status 评估状态
	Status GoalAssessmentStatus
	// Evidence 判断依据；complete/blocked 时为面向用户的详细报告
	Evidence string
	// RemainingWork 剩余缺口（continue 时填写）
	RemainingWork *string
	// NextInstruction 下一次最具体的可执行动作
	NextInstruction *string
}

// TokenUsage 目标维度的 token 用量累计。
// Python: TokenUsage (schema.py L130)
type TokenUsage struct {
	// InputTokens 输入 token
	InputTokens int
	// OutputTokens 输出 token
	OutputTokens int
	// CachedInputTokens 命中缓存的输入 token
	CachedInputTokens int
	// TotalTokens 输入 + 输出（不含缓存）
	TotalTokens int
}

// GoalRecord 会话级持久目标记录。
// Python: GoalRecord (schema.py L166)
type GoalRecord struct {
	// GoalID 目标标识（uuid4 hex 前 12 位）
	GoalID string
	// SessionID 所属会话
	SessionID string
	// Objective 目标原文
	Objective string
	// Status 目标状态
	Status GoalStatus
	// Revision 代际修订号（pause 不提升）
	Revision int
	// AttemptCount 已开始的尝试次数
	AttemptCount int
	// TokenUsage 累计 token 用量
	TokenUsage TokenUsage
	// MaxAttempts 尝试次数上限（可空）
	MaxAttempts *int
	// TokenBudget token 预算上限（可空）
	TokenBudget *int
	// LastAssessment 最近一次评估
	LastAssessment *GoalAssessment
	// LastStopReason 终态原因（completed/blocked）
	LastStopReason *string
	// TimeUsedSeconds 累计计时秒数
	TimeUsedSeconds int
	// ActiveStartedAt 当前计时段的起点（可空）
	ActiveStartedAt *string
	// CreatedAt 创建时间
	CreatedAt string
	// UpdatedAt 最近更新时间
	UpdatedAt string
}

// GoalStopConfig 停止判定配置。
// Python: GoalStopConfig (schema.py L301)
type GoalStopConfig struct {
	// Strategy 停止策略
	Strategy GoalStopStrategy
	// TranscriptWindowAttempts 监理窗口尝试数（上游无消费点，保留字段）
	TranscriptWindowAttempts int
	// VerificationInterval 抽检间隔（生产不赋值）
	VerificationInterval *int
	// MaxAttempts 尝试次数硬上限
	MaxAttempts *int
	// TokenBudget token 预算硬上限
	TokenBudget *int
}

// GoalOperationError Goal 能力暴露的唯一预期失败类型。
// Python: GoalOperationError (schema.py L84)
type GoalOperationError struct {
	// Operation 操作名（set/pause/…）
	Operation string
	// Code 错误码（invalid_objective / already_exists）
	Code string
	// Message 错误信息
	Message string
	// Goal 出错时的目标记录副本
	Goal *GoalRecord
}

// ──────────────────────────── 枚举 ────────────────────────────

const (
	// GoalStatusActive 进行中
	GoalStatusActive GoalStatus = "active"
	// GoalStatusPaused 已暂停（在飞轮允许自然收尾）
	GoalStatusPaused GoalStatus = "paused"
	// GoalStatusCompleted 已完成
	GoalStatusCompleted GoalStatus = "completed"
	// GoalStatusBlocked 已阻塞
	GoalStatusBlocked GoalStatus = "blocked"
)

const (
	// GoalAssessmentStatusContinue 继续下一轮
	GoalAssessmentStatusContinue GoalAssessmentStatus = "continue"
	// GoalAssessmentStatusComplete 目标完成
	GoalAssessmentStatusComplete GoalAssessmentStatus = "complete"
	// GoalAssessmentStatusBlocked 目标阻塞
	GoalAssessmentStatusBlocked GoalAssessmentStatus = "blocked"
)

const (
	// GoalStopStrategyAgentReport 只信主模型自报
	GoalStopStrategyAgentReport GoalStopStrategy = "agent_report"
	// GoalStopStrategyTranscript 只用独立监理模型
	GoalStopStrategyTranscript GoalStopStrategy = "transcript"
	// GoalStopStrategyHybrid 自报 + 终态复核（默认）
	GoalStopStrategyHybrid GoalStopStrategy = "hybrid"
)

// ──────────────────────────── 常量 ────────────────────────────

const (
	// goalTimestampLayout 产出 Python datetime.isoformat() 同形字符串
	// （UTC 下为 "+00:00"，微秒全零时自动省略小数位）
	goalTimestampLayout = "2006-01-02T15:04:05.999999-07:00"
	// goalTimestampLayoutSpace 容忍空格分隔的合法变体（Python fromisoformat 接受）
	goalTimestampLayoutSpace = "2006-01-02 15:04:05.999999-07:00"
)

// ──────────────────────────── 全局变量 ────────────────────────────

// utcNow 可替换的 UTC 时钟；包内测试覆盖它来冻结时间。
// Python: monkeypatch.setattr(goal_schema, "datetime", FrozenDateTime)
var utcNow = func() time.Time { return time.Now().UTC() }

// ──────────────────────────── 导出函数 ────────────────────────────

// ToDict 转换为持久化/事件载荷形态。
// Python: GoalAssessment.to_dict (schema.py L108)
func (a *GoalAssessment) ToDict() map[string]any {
	return map[string]any{
		"status":           string(a.Status),
		"evidence":         a.Evidence,
		"remaining_work":   optionalStringValue(a.RemainingWork),
		"next_instruction": optionalStringValue(a.NextInstruction),
	}
}

// FromGoalAssessmentDict 从持久化形态还原；非法状态回退 continue（与 GoalRecord 的严格校验不同）。
// Python: GoalAssessment.from_dict (schema.py L116)
func FromGoalAssessmentDict(data map[string]any) *GoalAssessment {
	status := GoalAssessmentStatusContinue
	if raw, exists := data["status"]; exists {
		candidate := GoalAssessmentStatus(pyStr(raw))
		switch candidate {
		case GoalAssessmentStatusContinue,
			GoalAssessmentStatusComplete,
			GoalAssessmentStatusBlocked:
			status = candidate
		}
	}
	var evidence string
	if raw, exists := data["evidence"]; exists && pyTruthy(raw) {
		evidence = pyStr(raw)
	}
	return &GoalAssessment{
		Status:          status,
		Evidence:        evidence,
		RemainingWork:   pyOptionalStr(data["remaining_work"]),
		NextInstruction: pyOptionalStr(data["next_instruction"]),
	}
}

// Accumulate 累加一次用量；total 只加 input+output。
// Python: TokenUsage.accumulate (schema.py L137)
func (u *TokenUsage) Accumulate(inputTokens, outputTokens, cachedInputTokens int) {
	u.InputTokens += inputTokens
	u.OutputTokens += outputTokens
	u.CachedInputTokens += cachedInputTokens
	u.TotalTokens += inputTokens + outputTokens
}

// ToDict 转换为持久化形态。
// Python: TokenUsage.to_dict (schema.py L148)
func (u TokenUsage) ToDict() map[string]any {
	return map[string]any{
		"input_tokens":        u.InputTokens,
		"output_tokens":       u.OutputTokens,
		"cached_input_tokens": u.CachedInputTokens,
		"total_tokens":        u.TotalTokens,
	}
}

// TokenUsageFromDict 从持久化形态还原；缺字段按 0 处理。
// Python: TokenUsage.from_dict (schema.py L156)
func TokenUsageFromDict(data map[string]any) (TokenUsage, error) {
	var usage TokenUsage
	fields := []struct {
		key    string
		target *int
	}{
		{key: "input_tokens", target: &usage.InputTokens},
		{key: "output_tokens", target: &usage.OutputTokens},
		{key: "cached_input_tokens", target: &usage.CachedInputTokens},
		{key: "total_tokens", target: &usage.TotalTokens},
	}
	for _, field := range fields {
		raw, exists := data[field.key]
		if !exists || raw == nil {
			continue
		}
		parsed, err := pyInt(raw)
		if err != nil {
			return TokenUsage{}, fmt.Errorf("invalid persisted GoalRecord token_usage.%s", field.key)
		}
		*field.target = parsed
	}
	return usage, nil
}

// Touch 刷新更新时间；bumpRevision 为真时提升代际（pause 传 false）。
// Python: GoalRecord.touch (schema.py L184)
func (r *GoalRecord) Touch(bumpRevision bool) {
	r.UpdatedAt = utcNowISO()
	if bumpRevision {
		r.Revision++
	}
}

// StartTiming 开启计时：仅在未开时记起点，总是刷新 updated_at。
// Python: GoalRecord.start_timing (schema.py L189)
func (r *GoalRecord) StartTiming() {
	now := utcNowISO()
	if r.ActiveStartedAt == nil {
		r.ActiveStartedAt = &now
	}
	r.UpdatedAt = now
}

// SettleActiveTime 结算当前计时段的秒数。
// Python: GoalRecord.settle_active_time (schema.py L195)
func (r *GoalRecord) SettleActiveTime(keepActive bool) {
	now := utcNow()
	nowISO := now.Format(goalTimestampLayout)
	if r.ActiveStartedAt != nil {
		startedAt, err := parseGoalTimestamp(*r.ActiveStartedAt, "active_started_at")
		if err != nil {
			// 上游会抛 ValueError；进入内存的记录已过 FromDict 校验，
			// 这里按"零耗时"兜底，避免一次脏时间戳打断整轮。
			startedAt = now
		}
		elapsed := int(now.Sub(startedAt).Seconds())
		if elapsed < 0 {
			elapsed = 0
		}
		r.TimeUsedSeconds += elapsed
		if keepActive {
			r.ActiveStartedAt = &nowISO
		} else {
			r.ActiveStartedAt = nil
		}
	} else if keepActive && r.Status == GoalStatusActive {
		r.ActiveStartedAt = &nowISO
	}
	r.UpdatedAt = nowISO
}

// Clone 返回深拷贝，供响应与错误对象隔离使用。
// Python: GoalRecord.copy_for_response (schema.py L207)
func (r *GoalRecord) Clone() *GoalRecord {
	if r == nil {
		return nil
	}
	clone := *r
	if r.MaxAttempts != nil {
		value := *r.MaxAttempts
		clone.MaxAttempts = &value
	}
	if r.TokenBudget != nil {
		value := *r.TokenBudget
		clone.TokenBudget = &value
	}
	if r.LastStopReason != nil {
		value := *r.LastStopReason
		clone.LastStopReason = &value
	}
	if r.ActiveStartedAt != nil {
		value := *r.ActiveStartedAt
		clone.ActiveStartedAt = &value
	}
	if r.LastAssessment != nil {
		assessment := *r.LastAssessment
		if r.LastAssessment.RemainingWork != nil {
			value := *r.LastAssessment.RemainingWork
			assessment.RemainingWork = &value
		}
		if r.LastAssessment.NextInstruction != nil {
			value := *r.LastAssessment.NextInstruction
			assessment.NextInstruction = &value
		}
		clone.LastAssessment = &assessment
	}
	return &clone
}

// ToDict 转换为持久化/事件载荷形态。
// Python: GoalRecord.to_dict (schema.py L210)
func (r *GoalRecord) ToDict() map[string]any {
	var assessment any
	if r.LastAssessment != nil {
		assessment = r.LastAssessment.ToDict()
	}
	return map[string]any{
		"goal_id":           r.GoalID,
		"session_id":        r.SessionID,
		"objective":         r.Objective,
		"status":            string(r.Status),
		"revision":          r.Revision,
		"attempt_count":     r.AttemptCount,
		"token_usage":       r.TokenUsage.ToDict(),
		"max_attempts":      optionalIntValue(r.MaxAttempts),
		"token_budget":      optionalIntValue(r.TokenBudget),
		"last_assessment":   assessment,
		"last_stop_reason":  optionalStringValue(r.LastStopReason),
		"time_used_seconds": r.TimeUsedSeconds,
		"active_started_at": optionalStringValue(r.ActiveStartedAt),
		"created_at":        r.CreatedAt,
		"updated_at":        r.UpdatedAt,
	}
}

// CreateGoalRecord 创建一条新的 ACTIVE 目标记录。
// Python: GoalRecord.create (schema.py L279)
func CreateGoalRecord(sessionID, objective string, maxAttempts, tokenBudget *int) *GoalRecord {
	now := utcNowISO()
	startedAt := now
	return &GoalRecord{
		GoalID:          newGoalID(),
		SessionID:       sessionID,
		Objective:       objective,
		Status:          GoalStatusActive,
		MaxAttempts:     maxAttempts,
		TokenBudget:     tokenBudget,
		ActiveStartedAt: &startedAt,
		CreatedAt:       now,
		UpdatedAt:       now,
	}
}

// FromGoalRecordDict 严格还原持久化记录；任何字段非法都返回错误（由 store 清空）。
// Python: GoalRecord.from_dict (schema.py L229)
func FromGoalRecordDict(data map[string]any) (*GoalRecord, error) {
	objective, objectiveOK := nonEmptyString(data["objective"])
	sessionID, sessionOK := nonEmptyString(data["session_id"])
	goalID, goalOK := nonEmptyString(data["goal_id"])
	if !objectiveOK || !sessionOK || !goalOK {
		return nil, errors.New("invalid persisted GoalRecord identity")
	}

	statusRaw := any(string(GoalStatusActive))
	if raw, exists := data["status"]; exists {
		statusRaw = raw
	}
	status := GoalStatus(pyStr(statusRaw))
	switch status {
	case GoalStatusActive, GoalStatusPaused, GoalStatusCompleted, GoalStatusBlocked:
	default:
		return nil, errors.New("invalid persisted GoalRecord status")
	}

	revision := 0
	if raw, exists := data["revision"]; exists {
		parsed, err := pyInt(raw)
		if err != nil {
			return nil, errors.New("invalid persisted GoalRecord revision")
		}
		revision = parsed
	}
	attemptCount := 0
	if raw, exists := data["attempt_count"]; exists {
		parsed, err := pyInt(raw)
		if err != nil {
			return nil, errors.New("invalid persisted GoalRecord attempt_count")
		}
		attemptCount = parsed
	}

	timeUsedRaw := any(0)
	if raw, exists := data["time_used_seconds"]; exists {
		timeUsedRaw = raw
	}
	timeUsed, err := nonNegativeInt(timeUsedRaw, "time_used_seconds")
	if err != nil {
		return nil, err
	}

	usage := TokenUsage{}
	if raw, ok := data["token_usage"].(map[string]any); ok {
		usage, err = TokenUsageFromDict(raw)
		if err != nil {
			return nil, err
		}
	}

	maxAttempts, err := optionalPositiveInt(data["max_attempts"], "max_attempts")
	if err != nil {
		return nil, err
	}
	tokenBudget, err := optionalPositiveInt(data["token_budget"], "token_budget")
	if err != nil {
		return nil, err
	}

	activeStartedAt, err := optionalTimestamp(data["active_started_at"], "active_started_at")
	if err != nil {
		return nil, err
	}

	now := utcNowISO()
	createdAt := now
	if raw, exists := data["created_at"]; exists && raw != nil {
		createdAt, err = requiredTimestamp(raw, "created_at")
		if err != nil {
			return nil, err
		}
	}
	updatedAt := createdAt
	if raw, exists := data["updated_at"]; exists && raw != nil {
		updatedAt, err = requiredTimestamp(raw, "updated_at")
		if err != nil {
			return nil, err
		}
	}

	// ACTIVE 记录应保留计时起点；持久化丢了这个可选字段时自愈而不是拒绝。
	if status == GoalStatusActive && activeStartedAt == nil {
		healed := updatedAt
		activeStartedAt = &healed
	}

	var assessment *GoalAssessment
	if raw, ok := data["last_assessment"].(map[string]any); ok {
		assessment = FromGoalAssessmentDict(raw)
	}

	return &GoalRecord{
		GoalID:          goalID,
		SessionID:       sessionID,
		Objective:       objective,
		Status:          status,
		Revision:        revision,
		AttemptCount:    attemptCount,
		TokenUsage:      usage,
		MaxAttempts:     maxAttempts,
		TokenBudget:     tokenBudget,
		LastAssessment:  assessment,
		LastStopReason:  pyOptionalStr(data["last_stop_reason"]),
		TimeUsedSeconds: timeUsed,
		ActiveStartedAt: activeStartedAt,
		CreatedAt:       createdAt,
		UpdatedAt:       updatedAt,
	}, nil
}

// NewGoalStopConfig 返回默认停止配置（HYBRID + 窗口 8）。
// Python: GoalStopConfig() (schema.py L301)
func NewGoalStopConfig() GoalStopConfig {
	return GoalStopConfig{
		Strategy:                 GoalStopStrategyHybrid,
		TranscriptWindowAttempts: 8,
	}
}

// Error 实现 error。
// Python: RuntimeError.__str__
func (e *GoalOperationError) Error() string {
	return e.Message
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// utcNowISO 返回当前 UTC 时间戳字符串。
// Python: _utc_now_iso (schema.py L80)
func utcNowISO() string {
	return utcNow().Format(goalTimestampLayout)
}

// parseGoalTimestamp 解析必须带时区的时间戳。
// Python: _parse_timestamp (schema.py L56)
func parseGoalTimestamp(value any, fieldName string) (time.Time, error) {
	text, ok := value.(string)
	if !ok || text == "" {
		return time.Time{}, fmt.Errorf("invalid persisted GoalRecord %s", fieldName)
	}
	for _, layout := range []string{time.RFC3339Nano, goalTimestampLayoutSpace} {
		parsed, err := time.Parse(layout, text)
		if err == nil {
			return parsed, nil
		}
	}
	return time.Time{}, fmt.Errorf("invalid persisted GoalRecord %s", fieldName)
}

// optionalTimestamp 校验并原样返回可选时间戳字符串。
// Python: _optional_timestamp (schema.py L68)
func optionalTimestamp(value any, fieldName string) (*string, error) {
	if value == nil {
		return nil, nil
	}
	if _, err := parseGoalTimestamp(value, fieldName); err != nil {
		return nil, err
	}
	text := value.(string)
	return &text, nil
}

// requiredTimestamp 校验并原样返回必填时间戳字符串。
// Python: _required_timestamp (schema.py L75)
func requiredTimestamp(value any, fieldName string) (string, error) {
	if _, err := parseGoalTimestamp(value, fieldName); err != nil {
		return "", err
	}
	return value.(string), nil
}

// pyInt 复刻 Python int()：接受整数、浮点（向零截断）与数字字符串。
//
// Python 侧值来自内存 int 或 checkpoint JSON 还原的 float64；
// 只认 int 会在会话恢复后翻译失败。
func pyInt(value any) (int, error) {
	switch v := value.(type) {
	case int:
		return v, nil
	case int8:
		return int(v), nil
	case int16:
		return int(v), nil
	case int32:
		return int(v), nil
	case int64:
		return int(v), nil
	case uint:
		return int(v), nil
	case uint8:
		return int(v), nil
	case uint16:
		return int(v), nil
	case uint32:
		return int(v), nil
	case uint64:
		return int(v), nil
	case float32:
		return int(v), nil
	case float64:
		return int(v), nil
	case string:
		trimmed := strings.TrimSpace(v)
		parsed, err := strconv.ParseInt(trimmed, 10, 64)
		if err != nil {
			asFloat, floatErr := strconv.ParseFloat(trimmed, 64)
			if floatErr != nil {
				return 0, err
			}
			return int(asFloat), nil
		}
		return int(parsed), nil
	default:
		return 0, fmt.Errorf("cannot convert %T to int", value)
	}
}

// optionalPositiveInt 解析可选的正整数持久化字段。
// Python: _optional_positive_int (schema.py L33)
func optionalPositiveInt(value any, fieldName string) (*int, error) {
	if value == nil {
		return nil, nil
	}
	parsed, err := pyInt(value)
	if err != nil {
		return nil, fmt.Errorf("invalid persisted GoalRecord %s", fieldName)
	}
	if parsed <= 0 {
		return nil, fmt.Errorf("invalid persisted GoalRecord %s", fieldName)
	}
	return &parsed, nil
}

// nonNegativeInt 解析非负整数持久化字段。
// Python: _non_negative_int (schema.py L46)
func nonNegativeInt(value any, fieldName string) (int, error) {
	parsed, err := pyInt(value)
	if err != nil {
		return 0, fmt.Errorf("invalid persisted GoalRecord %s", fieldName)
	}
	if parsed < 0 {
		return 0, fmt.Errorf("invalid persisted GoalRecord %s", fieldName)
	}
	return parsed, nil
}

// pyTruthy 复刻 Python 真值判定，用于持久化与 assessor JSON 的可选字段。
func pyTruthy(value any) bool {
	switch v := value.(type) {
	case nil:
		return false
	case bool:
		return v
	case string:
		return v != ""
	case int:
		return v != 0
	case int64:
		return v != 0
	case float64:
		return v != 0
	case []any:
		return len(v) > 0
	case map[string]any:
		return len(v) > 0
	default:
		return true
	}
}

// pyStr 复刻 str(value) 的可观测部分（上游只对字符串做有意义的渲染）。
func pyStr(value any) string {
	if text, ok := value.(string); ok {
		return text
	}
	if value == nil {
		return "None"
	}
	return fmt.Sprint(value)
}

// pyOptionalStr 复刻 `data.get(x) or None`：falsy → nil。
func pyOptionalStr(value any) *string {
	if !pyTruthy(value) {
		return nil
	}
	text := pyStr(value)
	return &text
}

// optionalStringValue 把可选字符串转成可直接放进 map/JSON 的值。
func optionalStringValue(value *string) any {
	if value == nil {
		return nil
	}
	return *value
}

// optionalIntValue 把可选整数转成可直接放进 map/JSON 的值。
func optionalIntValue(value *int) any {
	if value == nil {
		return nil
	}
	return *value
}

// derefStr 取可选字符串的值，nil 视为空串。
func derefStr(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

// strPtr 取字符串指针（便于构造可选字段）。
func strPtr(value string) *string { return &value }

// nonEmptyString 判定非空字符串（Python 的 isinstance(x, str) and x）。
func nonEmptyString(value any) (string, bool) {
	text, ok := value.(string)
	if !ok || text == "" {
		return "", false
	}
	return text, true
}

// newGoalID 生成 12 位十六进制目标标识。
// Python: uuid.uuid4().hex[:12]
func newGoalID() string {
	return strings.ReplaceAll(uuid.NewString(), "-", "")[:12]
}

// newGoalOperationError 构造目标操作错误；Goal 保存隔离副本。
// Python: GoalOperationError.__init__ (schema.py L87)
func newGoalOperationError(operation, code, message string, goal *GoalRecord) *GoalOperationError {
	return &GoalOperationError{
		Operation: operation,
		Code:      code,
		Message:   message,
		Goal:      goal.Clone(),
	}
}

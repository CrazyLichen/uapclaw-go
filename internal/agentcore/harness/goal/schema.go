// 本文件对应 Python openjiuwen/harness/goal/schema.py，
// 提供持久化 Goal 领域模型与能力错误契约。
package goal

import (
	"fmt"
	"strconv"
	"strings"
	"time"
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

// derefStr 取可选字符串的值，nil 视为空串。
func derefStr(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

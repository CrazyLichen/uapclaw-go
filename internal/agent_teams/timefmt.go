package agent_teams

import (
	"time"

	"github.com/uapclaw/uapclaw-go/internal/agent_teams/schema"
)

// 本文件提供面向 agent 的团队文本里的人类可读时间渲染。
// 团队数据库把时间戳存为毫秒 UTC epoch——这是正确的存储结构（排序、比较、索引都便宜，
// 跨进程跨机器无歧义）。但原始 epoch 对 LLM 无用：它无法可靠地从裸整数推理顺序或
// 「多久之前」，而消息带延迟到达时这会破坏优先级判断。
// 本文件把 epoch 转成「<绝对本地时间> (<相对差>)」，例如
// 「2026-05-28 14:30:05 +08:00 (3 分钟前)」。相对差给 agent 新鲜度感；绝对时间给
// 可观测性一个稳定锚点。分桶选择逻辑是纯数值、与语言无关——只有文案在 i18n。
// Python: openjiuwen/agent_teams/timefmt.py (125 行)

// ──────────────────────────── 结构体 ────────────────────────────

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

const (
	// justNowSeconds 「刚刚」的时间桶上限（秒）
	justNowSeconds = 10
	// minuteSeconds 一分钟（秒）
	minuteSeconds = 60
	// hourSeconds 一小时（秒）
	hourSeconds = 60 * 60
	// daySeconds 一天（秒）
	daySeconds = 24 * 60 * 60
)

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// FormatAbsoluteTime 把毫秒 epoch 渲染为绝对本地时间，不带相对差。
// 用于写进 agent 对话历史、之后会被重新读取的文本。相对差只在渲染那一刻为真：
// 一条消息以「刚刚」送达，一小时后成员重新推理时仍读作「刚刚」。
// 这类文本只携带绝对时间，读者用每次输入附带的当前时间锚点
// （RenderClock）自行计算经过时长。
// Python: format_absolute_time(timestamp_ms) (timefmt.py L102-122)
func FormatAbsoluteTime(timestampMs int64) string {
	return formatAbsolute(time.Local, timestampMs)
}

// FormatTimeContext 把时间戳渲染为「绝对本地时间 (相对差)」。
// timestampMs 为 nil 时（源字段未设置，例如从未流转过的任务）返回 time.unknown 文案。
// nowMs 永远由调用方传入，保持本函数纯函数可测。
// Python: format_time_context(timestamp_ms, now_ms) (timefmt.py L82-99)
func FormatTimeContext(timestampMs *int64, nowMs int64) string {
	return formatTimeContextAt(time.Local, timestampMs, nowMs)
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// relativeKeyAndValue 选出相对时间的 i18n key 与数值。
// 纯数值逻辑，不带语言耦合：调用方用 schema.T 渲染返回的 key。
// 负 delta（时间戳在未来，即时钟偏斜）与 10 秒以内都归入「刚刚」，
// 让模型永远看不到负数或噪声计数。
// Python: _relative_key_and_value(delta_ms) (timefmt.py L32-58)
func relativeKeyAndValue(deltaMs int64) (string, *int64) {
	if deltaMs < 0 {
		return "time.just_now", nil
	}
	seconds := deltaMs / 1000
	if seconds < justNowSeconds {
		return "time.just_now", nil
	}
	if seconds < minuteSeconds {
		return "time.seconds_ago", &seconds
	}
	if seconds < hourSeconds {
		v := seconds / minuteSeconds
		return "time.minutes_ago", &v
	}
	if seconds < daySeconds {
		v := seconds / hourSeconds
		return "time.hours_ago", &v
	}
	v := seconds / daySeconds
	return "time.days_ago", &v
}

// formatAbsolute 把 epoch 渲染为带数字时区偏移的本地墙钟时间。
// 用传入的 loc 解析（对应上游 astimezone() 取运行时本地时区），并把偏移写成
// +08:00 形式，让跨机器的读者仍能无歧义地对齐两个绝对时间。
// loc 可注入是为了测试固定时区，避免改全局 time.Local 污染其他测试。
// Python: _format_absolute(timestamp_ms) (timefmt.py L61-79)
func formatAbsolute(loc *time.Location, timestampMs int64) string {
	return time.UnixMilli(timestampMs).In(loc).Format("2006-01-02 15:04:05 -07:00")
}

// formatTimeContextAt 可注入时区的 format_time_context 实现（供测试固定时区）。
// Python 侧直接依赖 astimezone() 无可注入点，此处仅为测试可确定性而拆出。
// 步骤与上游逐条对应：nil 检查 → 取 key/value → value 为 None 与不为 None 两分支渲染 → 拼绝对时间。
// Python: format_time_context (timefmt.py L82-99)
func formatTimeContextAt(loc *time.Location, timestampMs *int64, nowMs int64) string {
	if timestampMs == nil {
		msg, err := schema.T("time.unknown")
		if err != nil {
			return "time.unknown"
		}
		return msg
	}
	key, value := relativeKeyAndValue(nowMs - *timestampMs)
	var relative string
	if value == nil {
		rendered, err := schema.T(key)
		if err != nil {
			return formatAbsolute(loc, *timestampMs)
		}
		relative = rendered
	} else {
		rendered, err := schema.T(key, map[string]any{"value": *value})
		if err != nil {
			return formatAbsolute(loc, *timestampMs)
		}
		relative = rendered
	}
	return formatAbsolute(loc, *timestampMs) + " (" + relative + ")"
}

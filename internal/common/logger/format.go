package logger

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/rs/zerolog"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────
const (
	// timestampLayout 日志时间戳布局（本地时区，毫秒精度）
	timestampLayout = "2006-01-02 15:04:05.000"
	// ansiReset ANSI 颜色重置序列
	ansiReset = "\x1b[0m"
)

// ──────────────────────────── 全局变量 ────────────────────────────
var (
	// levelColors 级别→ANSI 前景色；未列出的级别不着色
	levelColors = map[string]string{
		"DEBUG": "\x1b[36m",
		"INFO":  "\x1b[32m",
		"WARN":  "\x1b[33m",
		"ERROR": "\x1b[31m",
		"FATAL": "\x1b[35m",
	}
	// messageEscaper 消息正文控制字符转义器，保证一行一条
	messageEscaper = strings.NewReplacer("\n", `\n`, "\r", `\r`, "\t", `\t`)
)

// ──────────────────────────── 导出函数 ────────────────────────────

// ──────────────────────────── 非导出函数 ────────────────────────────

// newTextWriter 构建文本格式化 writer。
// noColor 为 true 时不着色（写文件时使用）；控制台是否着色由 colorEnabled 决定。
// 输出形态：[时间] [级别] [component] [文件:行号] 描述 key=value
//
// 性能说明：zerolog.ConsoleWriter 每行都要做「JSON 解码 → 重新渲染」，比 JSON
// 直写慢一个量级；流式 chunk、工具增量这类高频日志会被放大。当前没有量化数据，
// 作为文本化的已知成本记录在案；若后续出现日志热点，再按 event 采样或分流处理。
func newTextWriter(out io.Writer, noColor bool) zerolog.ConsoleWriter {
	useColor := !noColor && colorEnabled()
	return zerolog.ConsoleWriter{
		Out:     out,
		NoColor: !useColor,
		PartsOrder: []string{
			zerolog.TimestampFieldName,
			zerolog.LevelFieldName,
			"component",
			zerolog.CallerFieldName,
			zerolog.MessageFieldName,
		},
		// component 已出现在 PartsOrder 中，必须排除在尾部字段之外，否则重复输出 component=xxx
		FieldsExclude:         []string{"component"},
		FormatTimestamp:       formatTimestamp,
		FormatLevel:           func(i any) string { return formatLevel(i, useColor) },
		FormatPartValueByName: formatPartValue,
		FormatCaller:          formatCaller,
		FormatMessage:         formatMessage,
	}
}

// colorEnabled 判定是否启用 ANSI 着色。
// 策略：默认无色（与 Python 完全一致），仅当显式设置 FORCE_COLOR 且未设置
// NO_COLOR 时着色。这样文件/管道/supervisor 场景天然干净，无需 isatty 探测
// ——/dev/null 也是字符设备，ModeCharDevice 会把它误判成终端。
func colorEnabled() bool {
	if os.Getenv("NO_COLOR") != "" {
		return false
	}
	v := strings.TrimSpace(os.Getenv("FORCE_COLOR"))
	if v == "" {
		return false
	}
	switch strings.ToLower(v) {
	case "0", "false":
		return false
	default:
		return true
	}
}

// formatTimestamp 把 Unix 毫秒（或 RFC3339 字符串）渲染为 [YYYY-MM-DD HH:MM:SS.mmm]。
// 无法解析时返回空串，ConsoleWriter 会跳过该栏而不是输出半截括号。
func formatTimestamp(v any) string {
	var ts time.Time
	switch val := v.(type) {
	case json.Number:
		ms, err := val.Int64()
		if err != nil {
			return ""
		}
		ts = time.UnixMilli(ms)
	case float64:
		ts = time.UnixMilli(int64(val))
	case int64:
		ts = time.UnixMilli(val)
	case string:
		parsed, err := time.Parse(time.RFC3339Nano, val)
		if err != nil {
			return ""
		}
		ts = parsed
	default:
		return ""
	}
	return "[" + ts.Local().Format(timestampLayout) + "]"
}

// formatLevel 把级别渲染为大写方括号形式，withColor 为 true 时按级别着色。
func formatLevel(v any, withColor bool) string {
	name := strings.ToUpper(strings.TrimSpace(fmt.Sprintf("%v", v)))
	if name == "" {
		name = "UNKNOWN"
	}
	text := "[" + name + "]"
	if !withColor {
		return text
	}
	code, ok := levelColors[name]
	if !ok {
		return text
	}
	return code + text + ansiReset
}

// formatPartValue 渲染 PartsOrder 中的非内建部件（当前仅 component）。
func formatPartValue(v any, _ string) string {
	if v == nil {
		return ""
	}
	return "[" + fmt.Sprintf("%v", v) + "]"
}

// formatCaller 把调用方路径裁剪为末两段：[config/loader.go:88]。
// 路径段不足两段时保留原值；空值返回空串。
func formatCaller(v any) string {
	s, ok := v.(string)
	if !ok || s == "" {
		return ""
	}
	// 统一分隔符，兼容 Windows 构建
	normalized := strings.ReplaceAll(s, `\`, "/")
	file, line := normalized, ""
	if idx := strings.LastIndex(normalized, ":"); idx > 0 {
		file, line = normalized[:idx], normalized[idx:]
	}
	if parts := strings.Split(file, "/"); len(parts) >= 2 {
		file = parts[len(parts)-2] + "/" + parts[len(parts)-1]
	}
	return "[" + file + line + "]"
}

// formatMessage 渲染消息正文，转义控制字符保证一行一条。
func formatMessage(v any) string {
	return messageEscaper.Replace(fmt.Sprintf("%v", v))
}

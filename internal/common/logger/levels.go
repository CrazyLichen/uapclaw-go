package logger

import (
	"strings"

	"github.com/rs/zerolog"
	"github.com/uapclaw/uapclaw-go/internal/common/config"
)

// ──────────────────────────── 结构体 ────────────────────────────

// LoggingLevels 各输出通道的日志级别配置。
// 对应 Python: LoggingLevels 数据类
type LoggingLevels struct {
	// Logger 根 Logger 级别（取各文件级别的最小值）
	Logger LogLevel
	// Console 控制台级别
	Console LogLevel
	// Common common.log 级别（基础设施层：config/workspace/dotenv/version）
	Common LogLevel
	// Gateway gateway.log 级别
	Gateway LogLevel
	// Channel channel.log 级别
	Channel LogLevel
	// AgentServer agent_server.log 级别
	AgentServer LogLevel
	// Permissions permissions.log 级别
	Permissions LogLevel
	// AgentCore agent_core.log 级别（agentcore/*）
	AgentCore LogLevel
	// Full full.log 级别
	Full LogLevel
}

// ──────────────────────────── 枚举 ────────────────────────────

// LogLevel 日志级别枚举，与 zerolog.Level 对应。
// 对应 Python: logging.DEBUG / INFO / WARNING / ERROR / CRITICAL
type LogLevel int

const (
	// LogLevelDebug 调试级别
	LogLevelDebug LogLevel = iota
	// LogLevelInfo 信息级别
	LogLevelInfo
	// LogLevelWarn 警告级别
	LogLevelWarn
	// LogLevelError 错误级别
	LogLevelError
	// LogLevelFatal 致命级别
	LogLevelFatal
)

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// logLevelStrings LogLevel 枚举到字符串的映射。
var logLevelStrings = [...]string{"debug", "info", "warn", "error", "fatal"}

// ──────────────────────────── 导出函数 ────────────────────────────

// String 返回日志级别的字符串表示。
func (l LogLevel) String() string {
	if l < 0 || int(l) >= len(logLevelStrings) {
		return "unknown"
	}
	return logLevelStrings[l]
}

// MarshalJSON 实现 json.Marshaler 接口。
func (l LogLevel) MarshalJSON() ([]byte, error) {
	return []byte(`"` + l.String() + `"`), nil
}

// UnmarshalJSON 实现 json.Unmarshaler 接口。
func (l *LogLevel) UnmarshalJSON(data []byte) error {
	s := strings.Trim(string(data), `"`)
	parsed := ParseLogLevel(s, LogLevelInfo)
	*l = parsed
	return nil
}

// ToZerologLevel 将 LogLevel 转换为 zerolog.Level。
func (l LogLevel) ToZerologLevel() zerolog.Level {
	switch l {
	case LogLevelDebug:
		return zerolog.DebugLevel
	case LogLevelInfo:
		return zerolog.InfoLevel
	case LogLevelWarn:
		return zerolog.WarnLevel
	case LogLevelError:
		return zerolog.ErrorLevel
	case LogLevelFatal:
		return zerolog.FatalLevel
	default:
		return zerolog.InfoLevel
	}
}

// ParseLogLevel 将字符串解析为 LogLevel。
// 支持大小写不敏感的 "debug"/"info"/"warn"/"error"/"fatal"。
// 空字符串或无法识别时返回 defaultLevel。
// 对应 Python: _parse_log_level
func ParseLogLevel(name string, defaultLevel LogLevel) LogLevel {
	if name == "" {
		return defaultLevel
	}
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "debug":
		return LogLevelDebug
	case "info":
		return LogLevelInfo
	case "warn", "warning":
		return LogLevelWarn
	case "error":
		return LogLevelError
	case "fatal", "critical":
		return LogLevelFatal
	default:
		return defaultLevel
	}
}

// ResolveLoggingLevels 从配置解析各级别。
// 对应 Python: _resolve_logging_levels
//
// 解析优先级：
//  1. cfg.Level 作为基础级别（默认 INFO）
//  2. 各通道有独立级别覆盖（ConsoleLevel/Gateway/Channel/AgentServer/Full）
//  3. envLevel 环境变量仅覆盖控制台级别
//  4. override 统一覆盖所有通道
func ResolveLoggingLevels(cfg *config.LoggingConfig, envLevel string, override string) LoggingLevels {
	base := LogLevelInfo
	if cfg != nil {
		base = ParseLogLevel(cfg.Level, LogLevelInfo)
	}

	// 辅助函数：读取通道独立级别，未配置则跟随基础级别
	coerce := func(val string) LogLevel {
		if val != "" {
			return ParseLogLevel(val, base)
		}
		return base
	}

	console := base
	common := base
	gateway := base
	channel := base
	agentServer := base
	permissions := base
	agentCore := base
	full := base

	if cfg != nil {
		console = coerce(cfg.ConsoleLevel)
		common = coerce(cfg.Common)
		gateway = coerce(cfg.Gateway)
		channel = coerce(cfg.Channel)
		agentServer = coerce(cfg.AgentServer)
		permissions = coerce(cfg.Permissions)
		agentCore = coerce(cfg.AgentCore)
		full = coerce(cfg.Full)
	}

	// 环境变量仅覆盖控制台级别
	if envLevel != "" {
		console = ParseLogLevel(envLevel, console)
	}

	// override 统一覆盖所有通道
	if override != "" {
		v := ParseLogLevel(override, LogLevelInfo)
		console = v
		common = v
		gateway = v
		channel = v
		agentServer = v
		permissions = v
		agentCore = v
		full = v
	}

	// 根 Logger 级别取各文件级别的最小值，确保所有通道的日志都能通过
	loggerLevel := common
	if gateway < loggerLevel {
		loggerLevel = gateway
	}
	if channel < loggerLevel {
		loggerLevel = channel
	}
	if agentServer < loggerLevel {
		loggerLevel = agentServer
	}
	if permissions < loggerLevel {
		loggerLevel = permissions
	}
	if agentCore < loggerLevel {
		loggerLevel = agentCore
	}
	if full < loggerLevel {
		loggerLevel = full
	}

	return LoggingLevels{
		Logger:      loggerLevel,
		Console:     console,
		Common:      common,
		Gateway:     gateway,
		Channel:     channel,
		AgentServer: agentServer,
		Permissions: permissions,
		AgentCore:   agentCore,
		Full:        full,
	}
}

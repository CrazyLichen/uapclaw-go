package logger

import (
	"encoding/json"
	"testing"

	"github.com/rs/zerolog"
	"github.com/uapclaw/uapclaw-go/internal/common/config"
)

// ──────────────────────────── 导出函数 ────────────────────────────

func TestLogLevel_String(t *testing.T) {
	tests := []struct {
		level    LogLevel
		expected string
	}{
		{LogLevelDebug, "debug"},
		{LogLevelInfo, "info"},
		{LogLevelWarn, "warn"},
		{LogLevelError, "error"},
		{LogLevelFatal, "fatal"},
		{LogLevel(99), "unknown"},
	}
	for _, tt := range tests {
		if got := tt.level.String(); got != tt.expected {
			t.Errorf("LogLevel(%d).String() = %q, 期望 %q", tt.level, got, tt.expected)
		}
	}
}

func TestLogLevel_MarshalJSON(t *testing.T) {
	data, err := json.Marshal(LogLevelInfo)
	if err != nil {
		t.Fatalf("MarshalJSON 失败: %v", err)
	}
	if string(data) != `"info"` {
		t.Errorf("期望 \"info\"，实际 %s", string(data))
	}
}

func TestLogLevel_UnmarshalJSON(t *testing.T) {
	var l LogLevel
	if err := json.Unmarshal([]byte(`"warn"`), &l); err != nil {
		t.Fatalf("UnmarshalJSON 失败: %v", err)
	}
	if l != LogLevelWarn {
		t.Errorf("期望 LogLevelWarn，实际 %d", l)
	}
}

func TestParseLogLevel(t *testing.T) {
	tests := []struct {
		name         string
		defaultLevel LogLevel
		expected     LogLevel
	}{
		{"DEBUG", LogLevelInfo, LogLevelDebug},
		{"info", LogLevelDebug, LogLevelInfo},
		{"Warn", LogLevelInfo, LogLevelWarn},
		{"ERROR", LogLevelInfo, LogLevelError},
		{"fatal", LogLevelInfo, LogLevelFatal},
		{"warning", LogLevelInfo, LogLevelWarn},
		{"critical", LogLevelInfo, LogLevelFatal},
		{"", LogLevelWarn, LogLevelWarn},
		{"unknown", LogLevelError, LogLevelError},
	}
	for _, tt := range tests {
		got := ParseLogLevel(tt.name, tt.defaultLevel)
		if got != tt.expected {
			t.Errorf("ParseLogLevel(%q, %v) = %v, 期望 %v", tt.name, tt.defaultLevel, got, tt.expected)
		}
	}
}

func TestLogLevel_ToZerologLevel(t *testing.T) {
	// zerolog 使用有符号整数级别：DebugLevel=-1, InfoLevel=0, WarnLevel=1, ErrorLevel=2, FatalLevel=3
	tests := []struct {
		level    LogLevel
		expected zerolog.Level
	}{
		{LogLevelDebug, zerolog.DebugLevel},
		{LogLevelInfo, zerolog.InfoLevel},
		{LogLevelWarn, zerolog.WarnLevel},
		{LogLevelError, zerolog.ErrorLevel},
		{LogLevelFatal, zerolog.FatalLevel},
	}
	for _, tt := range tests {
		got := tt.level.ToZerologLevel()
		if got != tt.expected {
			t.Errorf("LogLevel(%d).ToZerologLevel() = %v, 期望 %v", tt.level, got, tt.expected)
		}
	}
}

func TestResolveLoggingLevels_默认值(t *testing.T) {
	levels := ResolveLoggingLevels(nil, "", "")
	if levels.Console != LogLevelInfo {
		t.Errorf("期望 Console = info，实际 %s", levels.Console)
	}
	if levels.Gateway != LogLevelInfo {
		t.Errorf("期望 Gateway = info，实际 %s", levels.Gateway)
	}
	if levels.Logger != LogLevelInfo {
		t.Errorf("期望 Logger = info，实际 %s", levels.Logger)
	}
}

func TestResolveLoggingLevels_从配置解析(t *testing.T) {
	cfg := &config.LoggingConfig{
		Level:        "warn",
		ConsoleLevel: "debug",
		Gateway:      "error",
		Channel:      "info",
		AgentServer:  "warn",
		Full:         "debug",
	}
	levels := ResolveLoggingLevels(cfg, "", "")

	if levels.Console != LogLevelDebug {
		t.Errorf("期望 Console = debug，实际 %s", levels.Console)
	}
	if levels.Gateway != LogLevelError {
		t.Errorf("期望 Gateway = error，实际 %s", levels.Gateway)
	}
	if levels.Channel != LogLevelInfo {
		t.Errorf("期望 Channel = info，实际 %s", levels.Channel)
	}
	if levels.AgentServer != LogLevelWarn {
		t.Errorf("期望 AgentServer = warn，实际 %s", levels.AgentServer)
	}
	if levels.Full != LogLevelDebug {
		t.Errorf("期望 Full = debug，实际 %s", levels.Full)
	}
	// Logger 取最小值 = debug (0)
	if levels.Logger != LogLevelDebug {
		t.Errorf("期望 Logger = debug，实际 %s", levels.Logger)
	}
}

func TestResolveLoggingLevels_环境变量仅覆盖控制台(t *testing.T) {
	cfg := &config.LoggingConfig{Level: "warn"}
	levels := ResolveLoggingLevels(cfg, "debug", "")

	if levels.Console != LogLevelDebug {
		t.Errorf("期望 Console = debug，实际 %s", levels.Console)
	}
	// 其他通道应保持 warn
	if levels.Gateway != LogLevelWarn {
		t.Errorf("期望 Gateway = warn，实际 %s", levels.Gateway)
	}
}

func TestResolveLoggingLevels_覆盖全部(t *testing.T) {
	cfg := &config.LoggingConfig{
		Level:        "warn",
		ConsoleLevel: "error",
		Gateway:      "error",
	}
	levels := ResolveLoggingLevels(cfg, "debug", "fatal")

	// override 应覆盖所有通道
	if levels.Console != LogLevelFatal {
		t.Errorf("期望 Console = fatal，实际 %s", levels.Console)
	}
	if levels.Gateway != LogLevelFatal {
		t.Errorf("期望 Gateway = fatal，实际 %s", levels.Gateway)
	}
	if levels.Channel != LogLevelFatal {
		t.Errorf("期望 Channel = fatal，实际 %s", levels.Channel)
	}
}

func TestResolveLoggingLevels_独立级别覆盖基础(t *testing.T) {
	cfg := &config.LoggingConfig{
		Level:   "warn",
		Gateway: "debug",
		Channel: "error",
		// AgentServer 和 Full 未设置，跟随基础 Level=warn
	}
	levels := ResolveLoggingLevels(cfg, "", "")

	if levels.Gateway != LogLevelDebug {
		t.Errorf("期望 Gateway = debug，实际 %s", levels.Gateway)
	}
	if levels.Channel != LogLevelError {
		t.Errorf("期望 Channel = error，实际 %s", levels.Channel)
	}
	if levels.AgentServer != LogLevelWarn {
		t.Errorf("期望 AgentServer = warn（跟随基础），实际 %s", levels.AgentServer)
	}
	if levels.Full != LogLevelWarn {
		t.Errorf("期望 Full = warn（跟随基础），实际 %s", levels.Full)
	}
}

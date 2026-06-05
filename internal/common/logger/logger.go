package logger

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"

	"github.com/rs/zerolog"
	"github.com/uapclaw/uapclaw-go/internal/common/config"
	"github.com/uapclaw/uapclaw-go/internal/common/exception"
	"gopkg.in/natefinch/lumberjack.v2"
)

// ──────────────────────────── 结构体 ────────────────────────────

// Logger 日志管理器（全局单例）。
// 对应 Python: setup_logger() 返回的根 Logger
//
// 每个组件（Common/Gateway/Channel/AgentServer/Permissions）创建独立的 zerolog.Logger 实例，
// 通过 GetLogger(component) 获取。每个 Logger 实例的 writer 同时写入：
//   - 对应的组件日志文件（common.log/gateway.log/channel.log/agent_server.log/permissions.log）
//   - full.log（全量汇总）
//   - 控制台
type Logger struct {
	// componentLoggers 组件→独立 Logger 实例
	componentLoggers map[Component]zerolog.Logger
	// levels 各通道级别
	levels LoggingLevels
	// rotationWriters 组件→轮转 writer（用于 Close 时释放文件句柄）
	rotationWriters map[Component]*lumberjack.Logger
	// fullRotationWriter full.log 轮转 writer
	fullRotationWriter *lumberjack.Logger
	// consoleWriter 控制台 writer
	consoleWriter zerolog.ConsoleWriter
	// sanitizer 脱敏器
	sanitizer *Sanitizer
	// rotationCfg 轮转配置
	rotationCfg RotationConfig
	// outputDir 日志输出目录
	outputDir string
	// mu 保护并发访问
	mu sync.RWMutex
}

// Option 日志选项函数（Functional Options 模式，与项目约定一致）。
type Option func(*Logger)

// ──────────────────────────── 全局变量 ────────────────────────────

var (
	// global 全局 Logger 单例
	global *Logger
	// globalOnce 确保 Setup 只执行一次
	globalOnce sync.Once
	// globalMu 保护 global 的读写
	globalMu sync.RWMutex
)

// ──────────────────────────── 导出函数 ────────────────────────────

// WithConfig 从 Config 对象加载日志配置。
func WithConfig(cfg *config.Config) Option {
	return func(l *Logger) {
		loggingCfg, err := cfg.GetLoggingConfig()
		if err != nil {
			// 配置读取失败，使用默认值
			return
		}
		envLevel := os.Getenv("LOG_LEVEL")
		l.levels = ResolveLoggingLevels(loggingCfg, envLevel, "")
	}
}

// WithLogLevel 统一覆盖所有通道级别。
// 对应 Python: setup_logger(log_level="DEBUG")
func WithLogLevel(level string) Option {
	return func(l *Logger) {
		l.levels = ResolveLoggingLevels(nil, "", level)
	}
}

// WithOutputDir 自定义日志输出目录。
// 对应 Python: get_logs_dir()
func WithOutputDir(dir string) Option {
	return func(l *Logger) {
		l.outputDir = dir
	}
}

// WithRotationConfig 自定义轮转配置。
func WithRotationConfig(cfg RotationConfig) Option {
	return func(l *Logger) {
		l.rotationCfg = cfg
	}
}

// Setup 初始化日志系统（全局单例）。
// 对应 Python: setup_logger()
//
// 多次调用是安全的（由 sync.Once 保护），后续调用不会重复初始化。
func Setup(opts ...Option) error {
	var setupErr error
	globalOnce.Do(func() {
		l := &Logger{
			levels:           ResolveLoggingLevels(nil, "", ""),
			rotationCfg:      NewRotationConfig(),
			componentLoggers: make(map[Component]zerolog.Logger),
			rotationWriters:  make(map[Component]*lumberjack.Logger),
		}

		// 应用选项
		for _, opt := range opts {
			opt(l)
		}

		// 设置默认输出目录
		if l.outputDir == "" {
			home, err := os.UserHomeDir()
			if err != nil {
			setupErr = exception.NewBaseError(exception.StatusCommonLogPathInitFailed,
				exception.WithMsg(fmt.Sprintf("获取用户主目录失败: %v", err)))
				return
			}
			l.outputDir = filepath.Join(home, ".uapclaw", "agent", ".logs")
		}

		// 确保输出目录存在
		if err := os.MkdirAll(l.outputDir, 0o755); err != nil {
			setupErr = exception.NewBaseError(exception.StatusCommonLogPathInitFailed,
				exception.WithMsg(fmt.Sprintf("创建日志目录失败: %v", err)))
			return
		}

		// 创建脱敏器
		l.sanitizer = NewSanitizer()

		// 创建 full.log writer（所有组件共享，需要 mutexWriter 保护）
		l.fullRotationWriter = NewRotatingWriter(
			filepath.Join(l.outputDir, "full.log"), l.rotationCfg)
		fullWriter := NewMutexWriter(l.fullRotationWriter)
		sanitizedFullWriter := NewSanitizerWriter(fullWriter, l.sanitizer)

		// 创建控制台 writer
		l.consoleWriter = zerolog.ConsoleWriter{Out: os.Stdout}
		sanitizedConsoleWriter := NewSanitizerWriter(&l.consoleWriter, l.sanitizer)

		// 为每个组件创建 Logger
		for _, comp := range allComponents() {
			l.createComponentLogger(comp, sanitizedFullWriter, sanitizedConsoleWriter)
		}

		// 设置全局 zerolog 时间格式
		zerolog.TimeFieldFormat = zerolog.TimeFormatUnixMs

		// 保存到全局单例
		globalMu.Lock()
		global = l
		globalMu.Unlock()
	})

	return setupErr
}

// GetLogger 获取指定组件的 Logger 实例。
// 对应 Python: logging.getLogger(__name__)
//
// 用法：
//
//	var log = logger.GetLogger(logger.ComponentChannel)
//	log.Info().Msg("消息内容")
func GetLogger(component Component) zerolog.Logger {
	globalMu.RLock()
	defer globalMu.RUnlock()

	if global == nil {
		// 未初始化时返回默认 Logger
		return zerolog.Nop()
	}

	if lg, ok := global.componentLoggers[component]; ok {
		return lg
	}

	return global.componentLoggers[ComponentCommon]
}

// Close 关闭所有日志 writer，释放文件句柄。
// 对应 Python: _close_log_handlers
func Close() error {
	globalMu.Lock()
	defer globalMu.Unlock()

	if global == nil {
		return nil
	}

	var errs []error

	// 关闭各组件轮转 writer
	for _, w := range global.rotationWriters {
		if err := w.Close(); err != nil {
			errs = append(errs, err)
		}
	}

	// 关闭 full.log 轮转 writer
	if global.fullRotationWriter != nil {
		if err := global.fullRotationWriter.Close(); err != nil {
			errs = append(errs, err)
		}
	}

	if len(errs) > 0 {
		return exception.NewBaseError(exception.StatusCommonLogExecutionRuntimeError,
			exception.WithMsg(fmt.Sprintf("关闭日志 writer 失败: %v", errs)))
	}

	global = nil
	globalOnce = sync.Once{} // 允许重新 Setup
	return nil
}

// Levels 返回当前日志级别配置。
func Levels() LoggingLevels {
	globalMu.RLock()
	defer globalMu.RUnlock()

	if global == nil {
		return ResolveLoggingLevels(nil, "", "")
	}
	return global.levels
}

// OutputDir 返回当前日志输出目录。
func OutputDir() string {
	globalMu.RLock()
	defer globalMu.RUnlock()

	if global == nil {
		return ""
	}
	return global.outputDir
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// createComponentLogger 为指定组件创建 zerolog.Logger 实例。
// 每个组件 Logger 的输出同时写入：组件日志文件 + full.log + 控制台。
// Permissions 组件额外写入 agent_server.log。
func (l *Logger) createComponentLogger(
	comp Component,
	sanitizedFullWriter io.Writer,
	sanitizedConsoleWriter io.Writer,
) {
	// 创建组件日志文件的 writer 链
	compFilePath := filepath.Join(l.outputDir, comp.LogFileName())
	if err := EnsureLogDir(compFilePath); err != nil {
		// 目录创建失败，使用 stderr 作为降级输出
		fmt.Fprintf(os.Stderr, "创建日志目录失败: %v\n", err)
	}

	compRotationWriter := NewRotatingWriter(compFilePath, l.rotationCfg)
	l.rotationWriters[comp] = compRotationWriter

	// 组件文件 writer 是否需要 mutexWriter 保护：
	// - Gateway/Channel/Permissions: 只有一个 Logger 写入，不需要
	// - AgentServer: 被 AgentServer 和 Permissions 共享，需要
	// 但为统一处理，所有组件文件 writer 都使用 mutexWriter
	compWriter := NewMutexWriter(compRotationWriter)
	sanitizedCompWriter := NewSanitizerWriter(compWriter, l.sanitizer)

	// 组装多目标 writer
	var writers []io.Writer
	writers = append(writers, sanitizedCompWriter) // 组件日志文件
	writers = append(writers, sanitizedFullWriter)  // full.log
	writers = append(writers, sanitizedConsoleWriter) // 控制台

	// Permissions 组件额外写入 agent_server.log
	if comp == ComponentPermissions {
		agentServerWriter := l.getOrCreateAgentServerWriter(sanitizedFullWriter, sanitizedConsoleWriter)
		writers = append(writers, agentServerWriter) // permissions → agent_server.log
	}

	multiWriter := zerolog.MultiLevelWriter(writers...)

	// 获取组件日志级别
	level := l.componentLevel(comp)

	// 创建 zerolog.Logger
	zl := zerolog.New(multiWriter).
		Level(level.ToZerologLevel()).
		With().
		Timestamp().
		Str("component", comp.String()).
		Logger()

	l.componentLoggers[comp] = zl
}

// getOrCreateAgentServerWriter 获取 agent_server.log 的写入器。
// 用于 Permissions 组件同时写入 agent_server.log。
func (l *Logger) getOrCreateAgentServerWriter(
	sanitizedFullWriter io.Writer,
	sanitizedConsoleWriter io.Writer,
) io.Writer {
	agentServerComp := ComponentAgentServer
	agentServerFilePath := filepath.Join(l.outputDir, agentServerComp.LogFileName())
	agentServerRotationWriter := NewRotatingWriter(agentServerFilePath, l.rotationCfg)

	// agent_server.log 被 AgentServer 和 Permissions 共享，需要 mutexWriter
	agentServerWriter := NewMutexWriter(agentServerRotationWriter)
	return NewSanitizerWriter(agentServerWriter, l.sanitizer)
}

// componentLevel 返回指定组件的日志级别。
func (l *Logger) componentLevel(comp Component) LogLevel {
	switch comp {
	case ComponentCommon:
		return l.levels.Common
	case ComponentGateway:
		return l.levels.Gateway
	case ComponentChannel:
		return l.levels.Channel
	case ComponentAgentServer, ComponentPermissions:
		return l.levels.AgentServer
	default:
		return l.levels.Common
	}
}

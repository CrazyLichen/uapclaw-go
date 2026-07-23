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
	pathutil "github.com/uapclaw/uapclaw-go/internal/common/utils/path"
	"gopkg.in/natefinch/lumberjack.v2"
	"gopkg.in/yaml.v3"
)

// ──────────────────────────── 结构体 ────────────────────────────

// Logger 日志管理器（全局单例）。
// 对应 Python: setup_logger() 返回的根 Logger
//
// 每个组件（Common/Gateway/Channel/AgentServer/Permissions）创建独立的 zerolog.Logger 实例，
// 通过 Info/Warn/Error/Debug/Fatal 等组件级日志函数使用。每个 Logger 实例的 writer 同时写入：
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
	// mu 保护并发访问（预留，当前未使用）
	_ sync.RWMutex
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
	// globalSetup 标记 Setup 是否已执行
	globalSetup bool
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

// WithConfigFile 从 config.yaml 文件自动加载 logging 段。
//
// 不依赖 config.Config 对象，自己读取和解析 YAML 文件。
// 日志可以在 config.New()/config.Load() 之前初始化，和 Python 的
// configure_log() / setup_logger() 行为一致。
//
// 对应 Python: _load_logging_config_from_yaml() + _resolve_logging_levels()
func WithConfigFile() Option {
	return func(l *Logger) {
		loggingCfg := loadLoggingConfigFromYAML()
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

// WithLoggingLevels 直接设置日志级别。
// 用于 Reconfigure() 时直接传入解析好的级别配置。
func WithLoggingLevels(levels LoggingLevels) Option {
	return func(l *Logger) {
		l.levels = levels
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
		globalSetup = true
		globalMu.Unlock()
	})

	return setupErr
}

// 组件级日志函数

// Info 输出 Info 级别日志。
// 使用方式：logger.Info(logger.ComponentCommon).Str("key", "val").Msg("消息")
func Info(component Component) *zerolog.Event {
	return getLogger(component).Info()
}

// Warn 输出 Warn 级别日志。
// 使用方式：logger.Warn(logger.ComponentGateway).Err(err).Msg("警告")
func Warn(component Component) *zerolog.Event {
	return getLogger(component).Warn()
}

// Error 输出 Error 级别日志。
// 使用方式：logger.Error(logger.ComponentChannel).Err(err).Msg("失败")
func Error(component Component) *zerolog.Event {
	return getLogger(component).Error()
}

// Debug 输出 Debug 级别日志。
// 使用方式：logger.Debug(logger.ComponentCommon).Str("key", "val").Msg("调试")
func Debug(component Component) *zerolog.Event {
	return getLogger(component).Debug()
}

// Fatal 输出 Fatal 级别日志并调用 os.Exit(1)。
// 使用方式：logger.Fatal(logger.ComponentCommon).Err(err).Msg("致命错误")
func Fatal(component Component) *zerolog.Event {
	return getLogger(component).Fatal()
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
	globalSetup = false
	globalOnce = sync.Once{} // 允许重新 Setup
	return nil
}

// Reconfigure 运行时重新配置日志系统（如更新日志级别）。
// 对齐 Python: configure_log_config()
//
// 仅更新 levels 并重建各组件的 zerolog.Logger 实例，
// 不改变输出目录、轮转配置和 writer 链。
// 必须在 Setup() 之后调用，否则返回错误。
func Reconfigure(opts ...Option) error {
	globalMu.Lock()
	defer globalMu.Unlock()

	if global == nil {
		return exception.NewBaseError(exception.StatusCommonLogExecutionRuntimeError,
			exception.WithMsg("日志系统未初始化，无法重新配置"))
	}

	// 应用选项（仅更新 levels 生效，outputDir/rotationCfg 等忽略）
	for _, opt := range opts {
		opt(global)
	}

	// 重建各组件 Logger 的级别（不重建 writer 链，仅调整 level）
	for comp, zl := range global.componentLoggers {
		level := global.componentLevel(comp)
		global.componentLoggers[comp] = zl.Level(level.ToZerologLevel())
	}

	return nil
}

// IsSetup 返回日志系统是否已初始化。
func IsSetup() bool {
	globalMu.RLock()
	defer globalMu.RUnlock()
	return globalSetup
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

// GetLogConfigSnapshot 返回当前日志配置的深拷贝快照。
// 对应 Python: get_log_config_snapshot() (log_config.py L164-166)
// 用于 Spawn 子进程时将主进程的日志配置传递给子进程，确保日志行为一致。
func GetLogConfigSnapshot() map[string]any {
	globalMu.RLock()
	defer globalMu.RUnlock()

	snapshot := make(map[string]any)

	if global != nil {
		// 记录各通道日志级别
		levels := global.levels
		snapshot["logger"] = levels.Logger.String()
		snapshot["console"] = levels.Console.String()
		snapshot["common"] = levels.Common.String()
		snapshot["gateway"] = levels.Gateway.String()
		snapshot["channel"] = levels.Channel.String()
		snapshot["agent_server"] = levels.AgentServer.String()
		snapshot["permissions"] = levels.Permissions.String()
		snapshot["agent_core"] = levels.AgentCore.String()
		snapshot["full"] = levels.Full.String()
		// 记录输出目录
		snapshot["output_dir"] = global.outputDir
	}

	return snapshot
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// loadLoggingConfigFromYAML 读取 config.yaml 中的 logging 段。
//
// 不做 ${VAR:-default} 解析，仅取字面量值。
// 通过 utils/path 包获取配置文件路径，对齐 Python: get_config_file()。
// 对应 Python: _load_logging_config_from_yaml()
func loadLoggingConfigFromYAML() *config.LoggingConfig {
	configPath := pathutil.ConfigFile()
	if configPath == "" {
		return nil
	}

	content, err := os.ReadFile(configPath)
	if err != nil {
		return nil
	}

	var raw map[string]any
	if err := yaml.Unmarshal(content, &raw); err != nil {
		return nil
	}

	loggingVal, ok := raw["logging"]
	if !ok {
		return nil
	}

	bytes, err := yaml.Marshal(loggingVal)
	if err != nil {
		return nil
	}

	var cfg config.LoggingConfig
	if err := yaml.Unmarshal(bytes, &cfg); err != nil {
		return nil
	}

	return &cfg
}

// getLogger 获取指定组件的 Logger 实例指针（非导出）。
// 对应 Python: logging.getLogger(__name__)
//
// 外部包应使用 Info/Warn/Error/Debug/Fatal 等组件级日志函数，
// 传入 Component 参数即可，无需直接获取 Logger 实例。
func getLogger(component Component) *zerolog.Logger {
	globalMu.RLock()
	defer globalMu.RUnlock()

	if global == nil {
		// 未初始化时返回 Nop Logger 指针
		nop := zerolog.Nop()
		return &nop
	}

	if lg, ok := global.componentLoggers[component]; ok {
		return &lg
	}

	lg := global.componentLoggers[ComponentCommon]
	return &lg
}

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
	writers = append(writers, sanitizedCompWriter)    // 组件日志文件
	writers = append(writers, sanitizedFullWriter)    // 全量日志文件
	writers = append(writers, sanitizedConsoleWriter) // 控制台

	// Permissions 组件额外写入 agent_server.log
	if comp == ComponentPermissions {
		agentServerWriter := l.getOrCreateAgentServerWriter(sanitizedFullWriter, sanitizedConsoleWriter)
		writers = append(writers, agentServerWriter) // 权限 → agent_server.log
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
	case ComponentAgentCore:
		return l.levels.AgentCore
	default:
		return l.levels.Common
	}
}

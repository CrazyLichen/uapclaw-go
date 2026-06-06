package logger

import (
	"io"
	"os"
	"path/filepath"
	"sync"

	"gopkg.in/natefinch/lumberjack.v2"
)

// ──────────────────────────── 结构体 ────────────────────────────

// RotationConfig 日志轮转配置。
// 对应 Python: _LOG_FILE_MAX_BYTES + _LOG_FILE_BACKUP_COUNT
type RotationConfig struct {
	// MaxSize 单文件最大字节数，默认 20MB。
	// 对应 Python: _LOG_FILE_MAX_BYTES = 20 * 1024 * 1024
	MaxSize int
	// MaxBackups 最大备份数，默认 20。
	// 对应 Python: _LOG_FILE_BACKUP_COUNT = 20
	MaxBackups int
	// MaxAge 最大保留天数，0 表示不按天数清理。
	MaxAge int
	// Compress 是否压缩旧日志，默认 false。
	Compress bool
}

// mutexWriter 为 io.Writer 添加互斥锁保护。
// 解决多 Logger 实例并发写同一文件的问题。
// 特别用于 full.log（所有组件共享）和 agent_server.log（agent_server + permissions 共享）。
type mutexWriter struct {
	mu      sync.Mutex
	wrapped io.Writer
}

// ──────────────────────────── 常量 ────────────────────────────

const (
	// defaultMaxSizeBytes 单文件最大字节数，20 MB。
	// 对应 Python: _LOG_FILE_MAX_BYTES
	defaultMaxSizeBytes = 20 * 1024 * 1024
	// defaultMaxBackups 最大备份数。
	// 对应 Python: _LOG_FILE_BACKUP_COUNT
	defaultMaxBackups = 20
)

// ──────────────────────────── 导出函数 ────────────────────────────

// NewRotationConfig 创建默认轮转配置。
func NewRotationConfig() RotationConfig {
	return RotationConfig{
		MaxSize:    defaultMaxSizeBytes,
		MaxBackups: defaultMaxBackups,
		MaxAge:     0,
		Compress:   false,
	}
}

// NewRotatingWriter 创建轮转写入器。
// 内部使用 lumberjack.Logger，在 Linux 上使用 rename 策略轮转（安全且高效）。
// 对应 Python: SafeRotatingFileHandler
func NewRotatingWriter(filePath string, cfg RotationConfig) *lumberjack.Logger {
	maxSize := cfg.MaxSize
	if maxSize <= 0 {
		maxSize = defaultMaxSizeBytes
	}

	maxBackups := cfg.MaxBackups
	if maxBackups <= 0 {
		maxBackups = defaultMaxBackups
	}

	return &lumberjack.Logger{
		Filename:   filePath,
		MaxSize:    maxSize / 1024 / 1024, // lumberjack 单位是 MB
		MaxBackups: maxBackups,
		MaxAge:     cfg.MaxAge,
		Compress:   cfg.Compress,
		LocalTime:  true,
	}
}

// NewMutexWriter 为 writer 添加互斥锁保护。
func NewMutexWriter(w io.Writer) *mutexWriter {
	return &mutexWriter{wrapped: w}
}

// Write 实现 io.Writer 接口，加锁后写入底层 writer。
func (w *mutexWriter) Write(p []byte) (n int, err error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.wrapped.Write(p)
}

// Close 关闭底层 writer（如果实现了 io.Closer）。
func (w *mutexWriter) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()

	if closer, ok := w.wrapped.(io.Closer); ok {
		return closer.Close()
	}
	return nil
}

// EnsureLogDir 确保日志文件所在目录存在。
func EnsureLogDir(filePath string) error {
	dir := filepath.Dir(filePath)
	return os.MkdirAll(dir, 0o755)
}

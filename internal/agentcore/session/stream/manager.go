package stream

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/uapclaw/uapclaw-go/internal/common/exception"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// StreamWriter 流写入器接口
type StreamWriter interface {
	// Write 写入流数据
	Write(ctx context.Context, schema Schema) error
}

// InteractionOutputWriterProvider 交互所需的输出写入器提供者接口。
// StreamWriterManager 天然满足此接口。
// 从 interaction 包迁移到 stream 包（原 interaction.InteractionOutputWriterProvider）。
type InteractionOutputWriterProvider interface {
	GetInteractionOutputWriter() InteractionOutputWriter
}

// InteractionOutputWriter 交互输出写入器接口。
// outputWriter 天然满足此接口。
// 从 interaction 包迁移到 stream 包（原 interaction.InteractionOutputWriter）。
type InteractionOutputWriter interface {
	WriteInteraction(outputType string, index int, payload any) error
}

// StreamWriterManager 流写入器管理器，对应 Python StreamWriterManager。
// 持有 StreamEmitter 和 map[string]StreamWriter（key 为 StreamMode.Mode()），
// 统一管理 Writer 集合和消费端。
type StreamWriterManager struct {
	// emitter 流发射器
	emitter *StreamEmitter
	// writersMu 保护 writers map 的读写锁
	writersMu sync.RWMutex
	// writers 流写入器集合，key 为 StreamMode.Mode()（即模式标识字符串）
	writers map[string]StreamWriter
	// defaultModes 默认模式列表（不允许移除默认 Writer）
	defaultModes []StreamMode
}

// streamOutputConfig StreamOutput 内部配置
type streamOutputConfig struct {
	// firstFrameTimeout 首帧超时，对应 Python first_frame_timeout
	firstFrameTimeout time.Duration
	// timeout 帧间隔超时，对应 Python timeout
	timeout time.Duration
}

// StreamOutputOption StreamOutput 的可选配置
type StreamOutputOption func(*streamOutputConfig)

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// NewStreamWriterManager 创建流写入器管理器。
// modes 为空时默认注册 Output/Trace/Custom 三种 Writer。
// 对应 Python: StreamWriterManager(stream_emitter, modes)
func NewStreamWriterManager(emitter *StreamEmitter, modes ...StreamMode) *StreamWriterManager {
	if emitter == nil {
		panic("stream_emitter 不能为 nil")
	}

	defaultModes := modes
	if len(defaultModes) == 0 {
		defaultModes = []StreamMode{StreamModeOutput, StreamModeTrace, StreamModeCustom}
	}

	mgr := &StreamWriterManager{
		emitter:      emitter,
		writers:      make(map[string]StreamWriter),
		defaultModes: defaultModes,
	}
	mgr.addDefaultWriters()
	return mgr
}

// StreamEmitter 返回内部发射器
// 对应 Python: StreamWriterManager.stream_emitter()
func (m *StreamWriterManager) StreamEmitter() *StreamEmitter {
	return m.emitter
}

// AddWriter 添加自定义写入器。
// 对应 Python: StreamWriterManager.add_writer(key, writer)
func (m *StreamWriterManager) AddWriter(key StreamMode, writer StreamWriter) error {
	if writer == nil {
		return exception.NewBaseError(exception.StatusStreamWriterManagerAddWriterError,
			exception.WithMsg("writer 不能为 nil"),
			exception.WithParam("mode", key.Mode()),
		)
	}
	m.writersMu.Lock()
	m.writers[key.Mode()] = writer
	m.writersMu.Unlock()
	return nil
}

// GetWriter 按模式获取写入器。
// 对应 Python: StreamWriterManager.get_writer(key)
func (m *StreamWriterManager) GetWriter(key StreamMode) StreamWriter {
	m.writersMu.RLock()
	defer m.writersMu.RUnlock()
	return m.writers[key.Mode()]
}

// GetOutputWriter 获取标准输出流写入器
// 对应 Python: StreamWriterManager.get_output_writer()
func (m *StreamWriterManager) GetOutputWriter() StreamWriter {
	return m.GetWriter(StreamModeOutput)
}

// GetTraceWriter 获取追踪流写入器
// 对应 Python: StreamWriterManager.get_trace_writer()
func (m *StreamWriterManager) GetTraceWriter() StreamWriter {
	return m.GetWriter(StreamModeTrace)
}

// GetCustomWriter 获取自定义流写入器
// 对应 Python: StreamWriterManager.get_custom_writer()
func (m *StreamWriterManager) GetCustomWriter() StreamWriter {
	return m.GetWriter(StreamModeCustom)
}

// GetInteractionOutputWriter 满足 InteractionOutputWriterProvider 接口
func (m *StreamWriterManager) GetInteractionOutputWriter() InteractionOutputWriter {
	w := m.GetWriter(StreamModeOutput)
	if w == nil {
		return nil
	}
	// outputWriter 天然满足 InteractionOutputWriter
	if iw, ok := w.(InteractionOutputWriter); ok {
		return iw
	}
	return nil
}

// RemoveWriter 移除写入器，不允许移除默认 Writer。
// 对应 Python: StreamWriterManager.remove_writer(key)
func (m *StreamWriterManager) RemoveWriter(key StreamMode) error {
	for _, mode := range m.defaultModes {
		if mode.Mode() == key.Mode() {
			return exception.NewBaseError(exception.StatusStreamWriterManagerRemoveWriterError,
				exception.WithMsg("不允许移除默认 Writer"),
				exception.WithParam("mode", key.Mode()),
			)
		}
	}
	m.writersMu.Lock()
	delete(m.writers, key.Mode())
	m.writersMu.Unlock()
	return nil
}

// WithFirstFrameTimeout 设置首帧超时
func WithFirstFrameTimeout(d time.Duration) StreamOutputOption {
	return func(c *streamOutputConfig) {
		c.firstFrameTimeout = d
	}
}

// WithFrameTimeout 设置帧间隔超时
func WithFrameTimeout(d time.Duration) StreamOutputOption {
	return func(c *streamOutputConfig) {
		c.timeout = d
	}
}

// StreamOutput 返回流输出 channel，消费端通过 range 读取。
// 对应 Python: StreamWriterManager.stream_output(first_frame_timeout, timeout)
// 内部启动 goroutine 从 emitter 的队列读取 Schema 数据，转发到输出 channel。
// 流结束信号：Emitter.Close() 会调 queue.Close()，消费端通过 Receive() 返回
// ErrQueueClosed 感知流结束，等价于 Python 的 END_FRAME 哨兵机制。
func (m *StreamWriterManager) StreamOutput(opts ...StreamOutputOption) <-chan Schema {
	cfg := defaultStreamOutputConfig()
	for _, o := range opts {
		o(&cfg)
	}

	out := make(chan Schema)

	go func() {
		defer close(out)
		queue := m.emitter.StreamQueue()

		// 首帧超时上下文
		var ctx context.Context
		var cancel context.CancelFunc
		if cfg.firstFrameTimeout > 0 {
			ctx, cancel = context.WithTimeout(context.Background(), cfg.firstFrameTimeout)
		} else {
			ctx, cancel = context.WithCancel(context.Background())
		}

		firstFrame := true
		for {
			// 帧间隔超时：首帧后切换为帧间隔超时
			if !firstFrame && cfg.timeout > 0 {
				cancel()
				ctx, cancel = context.WithTimeout(context.Background(), cfg.timeout)
			}

			data, err := queue.Receive(ctx)
			if err != nil {
				// ErrQueueClosed 表示流正常结束，其他错误（超时等）也终止
				logger.Debug(logComponent).
					Str("event_type", "SESSION_STREAM_CHUNK").
					Str("status", "queue_closed").
					Msg("流输出队列已关闭")
				cancel()
				return
			}

			firstFrame = false

			if data != nil {
				logger.Debug(logComponent).
					Str("event_type", "SESSION_STREAM_CHUNK").
					Str("data_type", fmt.Sprintf("%T", data)).
					Msg("流数据接收成功")
				out <- data
			} else {
				logger.Debug(logComponent).
					Str("event_type", "SESSION_STREAM_CHUNK").
					Str("status", "waiting").
					Msg("未收到流数据，继续等待")
			}
		}
	}()

	return out
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// defaultStreamOutputConfig 默认配置（无超时限制，对齐 Python 默认行为）
func defaultStreamOutputConfig() streamOutputConfig {
	return streamOutputConfig{}
}

// addDefaultWriters 注册默认 Writer，对齐 Python StreamWriterManager._add_default_writers()
func (m *StreamWriterManager) addDefaultWriters() {
	for _, mode := range m.defaultModes {
		switch mode.Mode() {
		case StreamModeOutput.Mode():
			m.writers[mode.Mode()] = NewOutputStreamWriter(m.emitter)
		case StreamModeTrace.Mode():
			m.writers[mode.Mode()] = NewTraceStreamWriter(m.emitter)
		case StreamModeCustom.Mode():
			m.writers[mode.Mode()] = NewCustomStreamWriter(m.emitter)
		default:
			panic(exception.NewBaseError(exception.StatusStreamWriterManagerAddWriterError,
				exception.WithMsg("默认模式必须为 OUTPUT/TRACE/CUSTOM"),
				exception.WithParam("mode", mode.Mode()),
			))
		}
	}
}

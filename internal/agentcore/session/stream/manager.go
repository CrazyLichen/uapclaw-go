package stream

import (
	"context"
	"fmt"

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
// 持有 StreamEmitter 和 map[StreamMode]StreamWriter，统一管理 Writer 集合和消费端。
type StreamWriterManager struct {
	// emitter 流发射器
	emitter *StreamEmitter
	// writers 流写入器集合
	writers map[StreamMode]StreamWriter
	// defaultModes 默认模式列表（不允许移除默认 Writer）
	defaultModes []StreamMode
}

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
		writers:      make(map[StreamMode]StreamWriter),
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
			exception.WithParam("mode", key.String()),
		)
	}
	m.writers[key] = writer
	return nil
}

// GetWriter 按模式获取写入器。
// 对应 Python: StreamWriterManager.get_writer(key)
func (m *StreamWriterManager) GetWriter(key StreamMode) StreamWriter {
	return m.writers[key]
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
		if mode == key {
			return exception.NewBaseError(exception.StatusStreamWriterManagerRemoveWriterError,
				exception.WithMsg("不允许移除默认 Writer"),
				exception.WithParam("mode", key.String()),
			)
		}
	}
	delete(m.writers, key)
	return nil
}

// StreamOutput 返回流输出 channel，消费端通过 range 读取。
// 对应 Python: StreamWriterManager.stream_output()
// 内部启动 goroutine 从 emitter 的队列读取数据，转发到输出 channel。
func (m *StreamWriterManager) StreamOutput() <-chan any {
	out := make(chan any)

	go func() {
		defer close(out)
		queue := m.emitter.StreamQueue()
		for {
			ctx := context.Background()
			data, err := queue.Receive(ctx)
			if err != nil {
				// 队列关闭或超时
				logger.Debug(logComponent).
					Str("event_type", "SESSION_STREAM_CHUNK").
					Str("status", "queue_closed").
					Msg("流输出队列已关闭")
				return
			}

			// 收到 endFrame 哨兵 → 关闭输出 channel
			if _, ok := data.(endFrame); ok {
				logger.Debug(logComponent).
					Str("event_type", "SESSION_STREAM_CHUNK").
					Msg("收到 END_FRAME，流输出结束")
				return
			}

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

// addDefaultWriters 注册默认 Writer，对齐 Python StreamWriterManager._add_default_writers()
func (m *StreamWriterManager) addDefaultWriters() {
	for _, mode := range m.defaultModes {
		switch mode {
		case StreamModeOutput:
			m.writers[mode] = NewOutputStreamWriter(m.emitter)
		case StreamModeTrace:
			m.writers[mode] = NewTraceStreamWriter(m.emitter)
		case StreamModeCustom:
			m.writers[mode] = NewCustomStreamWriter(m.emitter)
		default:
			panic(exception.NewBaseError(exception.StatusStreamWriterManagerAddWriterError,
				exception.WithMsg("默认模式必须为 OUTPUT/TRACE/CUSTOM"),
				exception.WithParam("mode", mode.String()),
			))
		}
	}
}

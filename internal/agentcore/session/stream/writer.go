package stream

import (
	"context"

	"github.com/uapclaw/uapclaw-go/internal/common/exception"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// outputWriter 标准输出流写入器，对应 Python OutputStreamWriter。
type outputWriter struct {
	// emitter 流发射器
	emitter *StreamEmitter
}

// traceWriter 追踪流写入器，对应 Python TraceStreamWriter。
type traceWriter struct {
	// emitter 流发射器
	emitter *StreamEmitter
}

// customWriter 自定义流写入器，对应 Python CustomStreamWriter。
type customWriter struct {
	// emitter 流发射器
	emitter *StreamEmitter
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// NewOutputStreamWriter 创建标准输出流写入器
func NewOutputStreamWriter(emitter *StreamEmitter) *outputWriter {
	return &outputWriter{emitter: emitter}
}

// NewTraceStreamWriter 创建追踪流写入器
func NewTraceStreamWriter(emitter *StreamEmitter) *traceWriter {
	return &traceWriter{emitter: emitter}
}

// NewCustomStreamWriter 创建自定义流写入器
func NewCustomStreamWriter(emitter *StreamEmitter) *customWriter {
	return &customWriter{emitter: emitter}
}

// Write 写入标准输出流数据。
// Schema 为 nil 时返回校验错误；emitter 已关闭时丢弃数据并返回 nil。
func (w *outputWriter) Write(ctx context.Context, schema Schema) error {
	return writeStream(w.emitter, ctx, schema)
}

// Write 写入追踪流数据
func (w *traceWriter) Write(ctx context.Context, schema Schema) error {
	return writeStream(w.emitter, ctx, schema)
}

// Write 写入自定义流数据
func (w *customWriter) Write(ctx context.Context, schema Schema) error {
	return writeStream(w.emitter, ctx, schema)
}

// WriteInteraction 实现 InteractionOutputWriter 接口，供交互层写入输出。
func (w *outputWriter) WriteInteraction(outputType string, index int, payload any) error {
	ctx := context.Background()
	schema := OutputSchema{Type: outputType, Index: index, Payload: payload}
	return writeStream(w.emitter, ctx, schema)
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// writeStream 统一的写入逻辑，对齐 Python StreamWriter.write()。
// 1. nil 校验 → STREAM_WRITER_WRITE_STREAM_VALIDATION_ERROR
// 2. emitter 已关闭 → warn 日志 + 丢弃数据（与 Python 一致，返回 nil）
// 3. 正常写入 → emitter.Emit()
func writeStream(emitter *StreamEmitter, ctx context.Context, schema Schema) error {
	if schema == nil {
		return exception.NewBaseError(exception.StatusStreamWriterWriteStreamValidationError,
			exception.WithMsg("stream data is nil"),
		)
	}

	if emitter.IsClosed() {
		logger.Warn(logComponent).
			Str("event_type", "SESSION_STREAM_CHUNK").
			Str("data_type", schema.SchemaType()).
			Msg("流消息已丢弃，emitter 已关闭")
		return nil
	}

	if err := emitter.Emit(ctx, schema); err != nil {
		return exception.NewBaseError(exception.StatusStreamWriterWriteStreamError,
			exception.WithCause(err),
			exception.WithParam("data_type", schema.SchemaType()),
		)
	}
	return nil
}

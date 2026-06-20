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
// Schema 为 nil 时返回校验错误；Schema 类型非 OutputSchema 时返回校验错误（对齐 Python OutputStreamWriter 绑定 OutputSchema 的泛型约束）；
// emitter 已关闭时丢弃数据并返回 nil。
func (w *outputWriter) Write(ctx context.Context, schema Schema) error {
	if _, ok := schema.(OutputSchema); !ok {
		return exception.NewBaseError(exception.StatusStreamWriterWriteStreamValidationError,
			exception.WithMsg("outputWriter 只接受 OutputSchema 类型"),
			exception.WithParam("schema_type", schemaTypeOf(schema)),
		)
	}
	return writeStream(w.emitter, ctx, schema)
}

// Write 写入追踪流数据。
// Schema 类型非 TraceSchema 时返回校验错误（对齐 Python TraceStreamWriter 绑定 TraceSchema 的泛型约束）。
func (w *traceWriter) Write(ctx context.Context, schema Schema) error {
	if _, ok := schema.(TraceSchema); !ok {
		return exception.NewBaseError(exception.StatusStreamWriterWriteStreamValidationError,
			exception.WithMsg("traceWriter 只接受 TraceSchema 类型"),
			exception.WithParam("schema_type", schemaTypeOf(schema)),
		)
	}
	return writeStream(w.emitter, ctx, schema)
}

// Write 写入自定义流数据。
// Schema 类型非 CustomSchema 时返回校验错误（对齐 Python CustomStreamWriter 绑定 CustomSchema 的泛型约束）。
func (w *customWriter) Write(ctx context.Context, schema Schema) error {
	if _, ok := schema.(CustomSchema); !ok {
		return exception.NewBaseError(exception.StatusStreamWriterWriteStreamValidationError,
			exception.WithMsg("customWriter 只接受 CustomSchema 类型"),
			exception.WithParam("schema_type", schemaTypeOf(schema)),
		)
	}
	return writeStream(w.emitter, ctx, schema)
}

// WriteInteraction 实现 InteractionOutputWriter 接口，供交互层写入输出。
// 注意：当前使用 context.Background() 因为接口签名无 ctx 参数。
// ⤵️ 后续优化：InteractionOutputWriter 接口扩展 ctx 参数后替换。
func (w *outputWriter) WriteInteraction(outputType string, index int, payload any) error {
	ctx := context.Background()
	schema := OutputSchema{Type: outputType, Index: index, Payload: payload}
	return writeStream(w.emitter, ctx, schema)
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// writeStream 统一的写入逻辑，对齐 Python StreamWriter.write()。
// 1. nil 校验 → STREAM_WRITER_WRITE_STREAM_VALIDATION_ERROR
// 2. Schema 字段校验（Validate）→ STREAM_WRITER_WRITE_STREAM_VALIDATION_ERROR
// 3. emitter 已关闭 → warn 日志 + 丢弃数据（与 Python 一致，返回 nil）
// 4. 正常写入 → emitter.Emit()
func writeStream(emitter *StreamEmitter, ctx context.Context, schema Schema) error {
	if schema == nil {
		return exception.NewBaseError(exception.StatusStreamWriterWriteStreamValidationError,
			exception.WithMsg("stream data is nil"),
		)
	}

	// 对齐 Python: validated_data = self._schema_type.model_validate(stream_data)
	if err := schema.Validate(); err != nil {
		return err
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

// schemaTypeOf 返回 Schema 的具体类型名称，用于错误信息。
func schemaTypeOf(schema Schema) string {
	if schema == nil {
		return "nil"
	}
	switch schema.(type) {
	case OutputSchema:
		return "OutputSchema"
	case TraceSchema:
		return "TraceSchema"
	case CustomSchema:
		return "CustomSchema"
	default:
		return "unknown"
	}
}

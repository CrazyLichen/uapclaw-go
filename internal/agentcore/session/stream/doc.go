// Package stream 提供会话层流式数据写入与消费能力。
//
// 本包定义了流模式枚举（Output/Trace/Custom）、数据 Schema 结构、
// 流队列（StreamQueue）、流发射器（StreamEmitter）、流写入器（StreamWriter）
// 和流写入器管理器（StreamWriterManager）。
//
// 数据流：
//
//	调用方构造 Schema → Writer.Write(ctx, schema) → Emitter.Emit(schema) → Queue.Send(ctx, schema)
//	                                                                                       │
//	消费方 ← Manager.StreamOutput() <-chan any ← Queue.Ch() <-chan any ← Queue 内部 chan any
//
// 文件目录：
//
//	stream/
//	├── doc.go              # 包文档
//	├── base.go             # StreamMode 枚举 + Schema 接口 + 三种 Schema 结构体
//	├── queue.go            # StreamQueue 封装（chan + Send/Receive/Close + 超时）
//	├── emitter.go          # StreamEmitter（持有 StreamQueue，Emit/Close/END_FRAME）
//	├── writer.go           # StreamWriter 接口 + outputWriter/traceWriter/customWriter
//	└── manager.go          # StreamWriterManager（核心管理器）
//
// 对应 Python 代码：openjiuwen/core/session/stream/
package stream

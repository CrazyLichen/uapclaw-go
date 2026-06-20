// Package stream 提供会话层流式数据写入与消费能力。
//
// 本包定义了流模式结构体（Output/Trace/Custom）、数据 Schema 结构、
// 流队列（StreamQueue）、流发射器（StreamEmitter）、流写入器（StreamWriter）
// 和流写入器管理器（StreamWriterManager）。
//
// 数据流（全链路 Schema 类型安全）：
//
//	调用方构造 Schema → Writer.Write(ctx, schema) → Emitter.Emit(schema) → Queue.Send(ctx, schema)
//	                                                                                       │
//	消费方 ← Manager.StreamOutput() <-chan Schema ← Queue.Receive() (Schema, error)
//
// 流结束信号：Emitter.Close() 直接关闭底层队列，消费端通过 Receive() 返回 ErrQueueClosed 感知流结束，
// 等价于 Python 的 END_FRAME 哨兵机制，但更简洁可靠（消除 END_FRAME 丢失风险）。
//
// 文件目录：
//
//	stream/
//	├── doc.go              # 包文档
//	├── base.go             # StreamMode 结构体 + Schema 接口 + 三种 Schema 结构体 + Validate 校验
//	├── queue.go            # StreamQueue 封装（chan Schema + Send/Receive/Close + 超时）
//	├── emitter.go          # StreamEmitter（持有 StreamQueue，Emit/Close）
//	├── writer.go           # StreamWriter 接口 + outputWriter/traceWriter/customWriter
//	└── manager.go          # StreamWriterManager（核心管理器）
//
// 对应 Python 代码：openjiuwen/core/session/stream/
package stream

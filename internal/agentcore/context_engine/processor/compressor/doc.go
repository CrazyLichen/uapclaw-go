// Package compressor 提供上下文引擎的压缩处理器实现。
//
// 包含多种压缩策略，从轻量级到重量级渐进式介入：
//   - MicroCompactProcessor：清除旧工具结果内容（不调用 LLM）
//   - DialogueCompressor：对话轮次级压缩（调用 LLM）
//   - CurrentRoundCompressor：当轮增量压缩（两阶段，调用 LLM）
//   - FullCompactProcessor：全量压缩，最后防线（调用 LLM 或使用 Session Memory）
//
// 文件目录：
//
//	compressor/
//	├── doc.go                          # 包文档
//	├── util.go                         # 包级共享函数
//	├── dialogue_compressor.go          # DialogueCompressor 对话压缩器
//	├── current_round_compressor.go     # CurrentRoundCompressor 当轮增量压缩器
//	├── micro_compact_processor.go      # MicroCompactProcessor 微压缩处理器
//	└── full_compact_processor.go       # FullCompactProcessor 全量压缩处理器
//
// 对应 Python 代码：openjiuwen/core/context_engine/processor/compressor/
package compressor

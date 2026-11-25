// Package compressor 提供 forked 压缩器家族。
//
// 本批（24.2）仅落地 SessionMemoryCompressor；forked 的 dialogue/current_round/
// round_level/消息卸载器随 24.15 15b 批补齐。
//
// 文件目录：
//
//	compressor/
//	├── doc.go                            # 包文档
//	├── session_memory_compressor.go      # 会话记忆物化压缩器（24.2）
//	├── support/                           # 计数/窗口原语（24.2 差量）
//	└── recall/                            # 归档与召回（24.2）
//
// 对应 Python 代码：openjiuwen/core/context_engine/processor/forked/compressor/
package compressor

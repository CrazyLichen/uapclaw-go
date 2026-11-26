// Package compressor 提供 forked 压缩器家族。
//
// 24.15 已落地压缩基座 PrefixCompactProcessor（PY compressor/base.py 一比一）；
// SessionMemoryCompressor 为 24.2 件；dialogue/current_round/round_level 三压缩器
// 随 24.15 15a 后续任务补齐，offloader 随 15b 补齐。
//
// 文件目录：
//
//	compressor/
//	├── doc.go                            # 包文档
//	├── base.go                           # 压缩基座 PrefixCompactProcessor（24.15-15a）
//	├── base_test.go                      # 基座触发矩阵/溢出收缩/工具边界测试
//	├── session_memory_compressor.go      # 会话记忆物化压缩器（24.2）
//	├── support/                           # 计数/窗口原语 + 压缩执行器/落盘（24.2 差量 + 24.15）
//	├── recall/                            # 归档与召回（24.2）
//	└── reinjection/                       # 状态回注（24.15-15a）
//
// 对应 Python 代码：openjiuwen/core/context_engine/processor/forked/compressor/
package compressor

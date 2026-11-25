// Package support 提供 forked 压缩器家族的支撑原语：token 计数、上下文窗口解析、
// 内部消息前缀表。
//
// 本批（24.2）仅迁 SessionMemoryCompressor 消费的符号；reinjection/offloader 等
// 其余符号随 24.15 15b 批补齐。
//
// 文件目录：
//
//	support/
//	├── doc.go       # 包文档
//	└── util.go     # INTERNAL_USER_PREFIXES + token 计数 + 窗口解析 + 比例阈值
//
// 对应 Python 代码：openjiuwen/core/context_engine/processor/forked/compressor/support/util.py
package support

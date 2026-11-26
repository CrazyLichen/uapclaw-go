// Package support 提供 forked 处理器家族的共享调试落盘原语。
//
// enable_context_debug（或处理器局部 enable_compression_dump/enable_debug_dump）
// 打开时，各处理器调用 WriteDebugRecord 把压缩/卸载流水线各阶段
// （threshold check、span 切分、压缩重试、before/after diff 等）追加为
// JSONL 记录；ReAct agent 也用同一 writer 落最终出站模型载荷。
// 所有文件系统错误均被吞掉——调试器永不打断上下文流水线。
//
// 文件目录：
//
//	support/
//	├── doc.go               # 包文档
//	└── context_debug.go    # WriteDebugRecord / WriteLLMRequestRecord / 目录解析
//
// 对应 Python 代码：openjiuwen/core/context_engine/processor/forked/support/context_debug.py
package support

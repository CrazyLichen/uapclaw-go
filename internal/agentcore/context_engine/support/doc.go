// Package support 提供上下文引擎各层共用的两件"叶级"能力：内容净化与上下文溢出判定。
// 为什么独立成包：这两件事上游都放在 context_engine 根模块
// （content_sanitize.py、ContextEngine.is_context_overflow_error），但 Go 的
// 导入方向是「根包 → processor / token / context」，根包位于下游；把工具函数放回根包
// 会让 token/processor/offloader 反向依赖根包而成环。因此统一下沉到一个叶包，
// 让契约包（interface）保持只有契约。
// 文件目录：
//
//	support/
//	├── doc.go      # 包文档
//	├── sanitize.go # 图片 data URL/base64 净化与占位符
//	└── overflow.go # 上下文溢出短语表与错误链匹配
//
// 对应 Python 代码：
//   - openjiuwen/core/context_engine/content_sanitize.py
//   - openjiuwen/core/context_engine/context_engine.py (is_context_overflow_error)
package support

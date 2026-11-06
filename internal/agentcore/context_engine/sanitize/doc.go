// Package sanitize 提供上下文内容的文本净化：把图片原始字节（base64 / data URL）
// 替换成短占位符，避免其被当作文本计入 token、写进提示词或日志。
//
// 独立成包的原因：内容净化同时被 token 计数（token 包）、处理器估算（processor 包）
// 与上下文引擎（context_engine 根包）使用，而这三者之间存在依赖方向
// （根包 → processor/token），放在任一层都会形成导入环。对应 Python 侧
// openjiuwen/core/context_engine/content_sanitize.py（单模块，无分层问题）。
//
// 文件目录：
//
//	sanitize/
//	├── doc.go      # 包文档
//	└── sanitize.go # 值/内容净化与占位符常量
//
// 对应 Python 代码：openjiuwen/core/context_engine/content_sanitize.py
package sanitize

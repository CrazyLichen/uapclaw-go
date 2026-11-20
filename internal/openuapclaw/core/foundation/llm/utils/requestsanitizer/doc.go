// Package requestsanitizer 提供 JSON provider 请求边界的非变更式归一化：
// 清洗孤立 UTF-16 代理位、把 ToolCall 的 dict 参数转为 JSON 字符串。
//
// 文件目录：
//
//	requestsanitizer/
//	├── doc.go                # 包文档
//	└── request_sanitizer.go # CleanUnicode/JSONArguments/SanitizeChatRequest
//
// 对应 Python 代码：openjiuwen/core/foundation/llm/utils/request_sanitizer.py
package requestsanitizer

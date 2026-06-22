// Package headers_helper 提供 LLM 请求头构建辅助函数。
//
// 包含 HTTP 头清洗、大小写不敏感合并、配置级/请求级 headers 组装等功能。
//
// 文件目录：
//
//	headers_helper/
//	├── doc.go              # 包文档
//	└── headers_helper.go   # 请求头构建辅助函数
//
// 对应 Python 代码：
//   - openjiuwen/core/common/utils/header_utils.py (sanitize_headers, PROTECTED_HEADERS)
//   - openjiuwen/core/foundation/llm/headers_helper.py (merge/build 函数)
package headers_helper

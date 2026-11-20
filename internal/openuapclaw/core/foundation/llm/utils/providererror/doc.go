// Package providererror 提供模型厂商错误文本的折叠与摘要。
// 厂商 catch 块不得把原始 HTML/XML 响应体灌进 error_msg；
// 普通诊断（超时、连接错误、JSON API 错误、本地构建消息）保持原样。
//
// 文件目录：
//
//	providererror/
//	├── doc.go            # 包文档
//	├── provider_error.go # 顶层入口：FormatProviderException / SummarizeProviderErrorText + 状态码提取
//	└── markup.go        # HTML/XML 错误页判定与折叠
//
// 对应 Python 代码：
//
//	openjiuwen/core/foundation/llm/utils/provider_error.py（12 函数，164 行）
package providererror

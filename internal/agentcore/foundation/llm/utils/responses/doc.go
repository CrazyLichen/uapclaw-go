// Package responses 提供 OpenAI Responses API 的数据映射与 HTTP 传输。
//
// 职责分层（对齐 Python）：
//   - transport.go：纯 HTTP I/O（请求发送、SSE 逐行迭代），对应 responses_transport.py
//   - utils.go：请求体构建、消息/工具格式转换、SSE 事件解析、usage 归一，对应 responses_utils.py
//
// 被 model_clients/openai 的 Responses 分流（api_mode=responses）消费；
// transport 也为 23.6 OpenAI 账号 OAuth 客户端提供地基。
//
// 文件目录：
//
//	responses/
//	├── doc.go         # 包文档
//	├── transport.go   # OpenAIAccountResponsesTransport 等价实现（HTTP/SSE）
//	└── utils.go      # build_request_body/convert_messages/StreamParser 等数据映射
//
// 对应 Python 代码：openjiuwen/core/foundation/llm/utils/responses_transport.py + responses_utils.py
// （一 Python 文件对一 Go 文件）
package responses

// Package siliconflow 实现 SiliconFlow 模型客户端。
//
// SiliconFlow API 兼容 OpenAI Chat Completion 协议，客户端嵌入
// openai.OpenAIModelClient 复用 HTTP 请求/响应/SSE 解析等基础能力，
// 覆写 Invoke/Stream，在委托前对消息中的 tool_calls 做清洗（sanitize），
// 仅保留 OpenAI 标准字段（id/type/function.name/function.arguments），
// 强制 type="function"，避免 SiliconFlow API 不识别非标准字段而报错。
//
// sanitize tool_calls 的必要性：
//   - SiliconFlow API 对请求中的非标准字段严格，遇到不认识的字段会报错
//   - OpenAI/DeepSeek/DashScope 的 API 会忽略多余字段，不需要清洗
//   - 多轮对话中，上一轮 LLM 返回的 assistant 消息（含 tool_calls）以 dict 格式透传，
//     dict 内容不可控，必须清洗
//
// 文件目录：
//
//	siliconflow/
//	├── doc.go        # 包文档
//	└── client.go     # SiliconFlow 客户端实现（覆写 Invoke/Stream 补充 sanitize）
//
// 对应 Python: openjiuwen/core/foundation/llm/model_clients/siliconflow_model_client.py
package siliconflow

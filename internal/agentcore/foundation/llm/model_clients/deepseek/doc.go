// Package deepseek 实现 DeepSeek 模型客户端。
//
// DeepSeek API 兼容 OpenAI Chat Completion 协议，客户端嵌入
// openai.OpenAIModelClient 复用 HTTP 请求/响应/SSE 解析等基础能力，
// 覆写 Invoke/Stream 在委托前为所有 assistant 消息补充 reasoning_content。
//
// reasoning_content 补充规则（对齐 DeepSeek 官方文档）：
//   - 有工具调用的多轮对话中，assistant 消息必须包含 reasoning_content 字段，
//     否则 API 返回 400 错误
//   - 无工具调用时，reasoning_content 可不传（传了也会被忽略）
//   - 本客户端对所有 assistant 消息统一兜底补空字符串，确保不会因漏传触发 400
//
// 对应 Python: openjiuwen/core/foundation/llm/model_clients/deepseek_model_client.py
package deepseek

// Package openai 提供 OpenAI 兼容协议的 LLM 模型客户端实现。
//
// 本包实现 BaseModelClient 接口，支持所有兼容 OpenAI Chat Completion API 的服务提供商，
// 包括 OpenAI 官方 API 和 OpenRouter 等第三方代理。
//
// # 核心设计
//
// 不依赖 openai-go SDK，自行实现 HTTP + SSE 解析：
//   - 使用 net/http 直接发送 Chat Completion 请求
//   - 自实现 SSE (Server-Sent Events) 读取器解析流式响应
//   - 兼容多种 OpenAI 兼容 API（OpenRouter、vLLM 等），自行控制更灵活
//
// # 文件清单
//
//	openai/
//	  doc.go                  — 包文档（本文件）
//	  types.go                — OpenAI API 响应 JSON 结构体定义
//	  sse_reader.go           — SSE 流读取器
//	  request_builder.go      — 请求构建（headers、参数调整、SSL/代理）
//	  parse_response.go       — 非流式响应解析
//	  parse_stream_chunk.go   — 流式块解析
//	  client.go               — OpenAIModelClient 主结构体 + Invoke/Stream + init 注册
//
// # 注册
//
// init() 时自动注册到全局 ClientRegistry：
//
//	Register("OpenAI", "llm", ...)    → llm_OpenAI
//	Register("OpenRouter", "llm", ...) → llm_OpenRouter
//
// 后续领域（2.14 Model 门面或 CLI 入口）需 blank import 本包触发注册：
//
//	import _ "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/model_clients/openai"
//
// # Python 对应路径
//
//	openjiuwen/core/foundation/llm/model_clients/openai_model_client.py
package openai

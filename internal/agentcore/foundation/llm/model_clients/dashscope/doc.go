// Package dashscope 提供阿里云百炼 DashScope 模型客户端实现。
//
// 本包实现 BaseModelClient 接口，支持 DashScope（通义千问）的文本聊天和多模态生成能力。
//
// # 核心设计
//
// 嵌入 OpenAIModelClient 复用 Invoke/Stream（通义千问兼容 OpenAI Chat Completion 协议），
// 仅覆写 GenerateImage/GenerateSpeech/GenerateVideo 三个多模态方法。
//
// 不依赖 dashscope-go SDK，自行实现 DashScope 原生 API 的 HTTP 调用
// （与 OpenAI 客户端不依赖 openai-go SDK 的设计一致）：
//   - 使用 DashScope 原生协议格式（input.messages + parameters 分层）
//   - 图片/语音调用多模态生成 API
//   - 视频调用视频生成 API
//   - 自动从 api_base 推导 DashScope 原生 API 的 base URL
//
// # 文件清单
//
//	dashscope/
//	  doc.go              — 包文档（本文件）
//	  types.go            — DashScope API 响应 JSON 结构体定义 + 常量
//	  api_client.go       — DashScope HTTP API 调用封装（认证、URL 推导、错误处理）
//	  client.go           — DashScopeModelClient 主结构体 + GenerateImage/Speech/Video + init 注册
//
// # 注册
//
// init() 时自动注册到全局 ClientRegistry：
//
//	Register("DashScope", "llm", ...) → llm_DashScope
//
// 后续领域（2.14 Model 门面或 CLI 入口）需 blank import 本包触发注册：
//
//	import _ "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/model_clients/dashscope"
//
// # Python 对应路径
//
//	openjiuwen/core/foundation/llm/model_clients/dashscope_model_client.py
package dashscope

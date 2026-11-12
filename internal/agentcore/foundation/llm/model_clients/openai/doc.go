// Package openai 提供 OpenAI 兼容协议的 LLM 模型客户端实现。
// 本包实现 BaseModelClient 接口，支持所有兼容 OpenAI Chat Completion API 的服务提供商，
// 包括 OpenAI 官方 API 和 OpenRouter 等第三方代理。
// # 核心设计
//
// 不依赖 openai-go SDK，自行实现 HTTP + SSE 解析：
//   - 使用 net/http 直接发送 Chat Completion 请求
//   - 自实现 SSE (Server-Sent Events) 读取器解析流式响应
//   - 兼容多种 OpenAI 兼容 API（OpenRouter、vLLM 等），自行控制更灵活
//
// 文件目录：
//
//	openai/
//	├── doc.go               # 包文档
//	├── types.go             # OpenAI API 响应 JSON 结构体定义
//	├── sse_reader.go        # SSE 流读取器
//	├── request_builder.go   # 请求构建（headers、参数调整、extra_body 7 键搬移、孤儿 tool_call 裁剪）
//	├── param_rules.go       # ModelParamRule + MiniMax reasoning_split
//	├── disabled_thinking.go # reasoning plan 接入 + disabled-thinking 三闸门回退
//	├── generation.go        # DashScope 原生多模态三方法（image/speech/video）
//	├── dashscope_native.go  # DashScope 原生 API 响应类型与调用封装
//	├── kv_cache.go          # vLLM KV cache 释放（extensions.kv_cache.mode=affinity）
//	├── parse_response.go    # 非流式响应解析
//	├── parse_stream_chunk.go # 流式块解析
//	├── transcription.go     # 音频转写（Whisper API）
//	├── responses_api.go     # Responses API 分流与调用（api_mode=responses，Python :1540-1841）
//	└── client.go            # OpenAIModelClient 主结构体 + Invoke/Stream + init 注册
//
// # 注册
//
// init() 只注册 3 个名字（其余别名 provider 不需要单独注册）：
//
//	Register("OpenAI", "llm", ...)     → llm_OpenAI
//	Register("OpenRouter", "llm", ...) → llm_OpenRouter
//	Register("DeepSeek", "llm", ...)   → llm_DeepSeek（历史遗留，保持不变）
//
// 其余 11 个别名 provider（DashScope / SiliconFlow / Moonshot / MiniMax /
// ModelArts / VolcEngine / Qianfan / Zhipu / MiMo / InferenceAffinity /
// AscendAffinity）由 model_clients.CreateModelClient 先经
// llm/endpointprofiles.NormalizeModelClientConfig 归一为 client_provider="OpenAI"，
// 再按 ImplementationProvider 分派到本客户端，因此无需在此注册。
//
// 使用 Model 门面或 CLI 入口时需 blank import 本包触发注册：
//
//	import _ "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/model_clients/openai"
//
// 对应 Python 代码：
//
//	openjiuwen/core/foundation/llm/model_clients/openai_model_client.py
package openai

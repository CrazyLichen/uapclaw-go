// Package inferenceaffinity 实现 InferenceAffinity (vLLM) 模型客户端。
//
// InferenceAffinity API 兼容 OpenAI Chat Completion 协议，客户端嵌入
// openai.OpenAIModelClient 复用 HTTP 请求/响应/SSE 解析等基础能力，
// 覆写 Invoke/Stream，在委托前对消息中的 tool_calls 做清洗（sanitize），
// 并注入 cache_sharing/cache_salt 参数以支持 vLLM KV Cache 共享。
//
// # 独有能力
//
//   - Release() 方法：释放 vLLM KV Cache，调用 {api_base}/release_kv_cache
//   - cache_sharing/cache_salt：请求参数中注入缓存共享配置
//
// # sanitize tool_calls 的必要性
//
// vLLM 后端对请求中的非标准字段严格，遇到不认识的字段会报错。
// 多轮对话中 assistant 消息的 tool_calls 需要清洗：
//   - 只保留标准字段：id、type、index、function.name、function.arguments
//   - 强制 type 为 "function"
//   - 移除非标准扩展字段
//
// # 注册
//
// init() 时自动注册到全局 ClientRegistry：
//
//	Register("InferenceAffinity", "llm", ...)
//
// 使用 Model 门面或 CLI 入口时需 blank import 本包触发注册：
//
//	import _ "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/model_clients/inference_affinity"
//
// 文件目录：
//
//	inference_affinity/
//	├── doc.go   # 包文档
//	└── client.go # InferenceAffinityModelClient + Invoke/Stream/Release + sanitize + init 注册
//
// 对应 Python 代码：
//
//	openjiuwen/core/foundation/llm/model_clients/inference_affinity_model_client.py
package inferenceaffinity

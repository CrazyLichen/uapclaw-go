// Package intellirouter 实现 IntelliRouter 智能路由模型客户端。
//
// IntelliRouter 是一个 LLM 智能路由客户端，核心能力是将一个模型名映射到
// 多个 OpenAI 兼容部署端点，然后根据路由策略选择最优端点发送请求。
//
// # 核心特性
//
//   - API 池化：一个模型名映射到多个部署端点
//   - 智能路由：支持四种路由策略（simple-shuffle / round-robin / lowest-latency / adaptive）
//   - 自动重试：Invoke 失败时自动切换到另一个部署端点
//   - 状态管理：跟踪部署端点的健康状态和延迟统计
//   - 流式支持：支持流式响应（Stream 委托给 OpenAI 客户端）
//   - 路由器共享：相同配置的客户端共享同一个 ReliableRouter 实例
//
// # 架构
//
// IntelliRouter 客户端嵌入 openai.OpenAIModelClient 复用 HTTP 请求/SSE 解析/响应转换，
// 覆写 Invoke/Stream，在调用时动态替换 api_key/api_base 为路由选中的部署端点。
//
// # 路由策略
//
//   - simple-shuffle：随机打乱健康端点列表，选择第一个（默认）
//   - round-robin：轮询选择下一个健康端点
//   - lowest-latency：选择历史平均延迟最低的健康端点
//   - adaptive：多因子加权评分（健康度/token/RPM/延迟），带探索机制
//
// # 配置
//
// IntelliRouter 不需要 ModelClientConfig 级别的 api_key/api_base
// （使用 WithSkipValidate 跳过校验），而是在每个 Deployment 中配置。
// 配置通过 ModelClientConfig.Extra 中的 intelli_router_* 前缀字段传递。
//
// # 注册
//
// init() 时自动注册到全局 ClientRegistry：
//
//	Register("intelli_router", "llm", ...)
//
// 使用 Model 门面或 CLI 入口时需 blank import 本包触发注册：
//
//	import _ "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/model_clients/intellirouter"
//
// # Python 对应路径
//
//	openjiuwen/core/foundation/llm/model_clients/intelli_router_model_client.py
package intellirouter

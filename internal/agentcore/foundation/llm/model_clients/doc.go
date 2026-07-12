// Package model_clients 提供 LLM 模型客户端的接口定义、共享实现和注册机制。
//
// 本包是领域二（LLM 基础层）的核心，定义了所有模型客户端必须满足的 BaseModelClient 接口，
// 以及各客户端共享的 BaseClientEmbed 基础实现。
//
// # 类型体系
//
// 接口与基础实现：
//
//	BaseModelClient        — 模型客户端接口（Invoke/Stream/GenerateImage/Speech/Video/Release）
//	BaseOutputParser       — 输出解析器最小接口（2.16 节扩展）
//	BaseClientEmbed        — 共享实现结构体（具体客户端嵌入复用）
//
// 参数类型：
//
//	MessagesParam          — 消息参数（输入侧三态：纯文本/消息列表/dict列表）
//	InvokeParams           — 非流式调用参数 + InvokeOption
//	StreamParams           — 流式调用参数 + StreamOption
//	GenerateImageParams    — 图片生成参数 + GenerateImageOption
//	GenerateSpeechParams   — 语音生成参数 + GenerateSpeechOption
//	GenerateVideoParams    — 视频生成参数 + GenerateVideoOption
//	ReleaseParams          — 缓存释放参数 + ReleaseOption
//
// 注册与工厂：
//
//	ClientRegistry         — 客户端注册表（线程安全）
//	CreateModelClient      — 根据配置创建客户端实例
//
// # 输入侧/输出侧对称设计
//
//	MessagesParam（本包，输入侧）— 整个 messages 列表的格式
//	  IsText     → 纯文本，自动包装为 UserMessage
//	  IsMessages → 消息列表（保留具体类型），转换为 OpenAI dict
//	  IsDicts    → 已是 OpenAI dict 格式，直接透传
//
//	MessageContent（schema 包，输出侧）— 单条消息 content 字段的格式
//	  IsText  → 纯文本
//	  Parts   → 多模态内容分片
//
// 文件目录：
//
//	model_clients/
//	├── doc.go            # 包文档
//	├── base_client.go    # BaseModelClient 接口 + BaseOutputParser + BaseClientEmbed（Stream 返回纯 chunk channel）
//	├── registry.go       # ClientRegistry + CreateModelClient + ProviderValidator 桥接
//	├── messages_param.go # MessagesParam 输入侧三态消息参数
//	└── invoke_params.go  # InvokeParams/StreamParams/Generate*Params + Functional Options
//
// 对应 Python 代码：
//
//	openjiuwen/core/foundation/llm/model_clients/base_model_client.py  — BaseModelClient
//	openjiuwen/core/foundation/llm/model_clients/__init__.py           — create_model_client
//	openjiuwen/core/common/clients/client_registry.py                 — ClientRegistry
//	openjiuwen/core/foundation/llm/output_parsers/output_parser.py    — BaseOutputParser (2.16 节)
//
// # 后续注册点（2.7-2.12 节实现时修改）
//
// ⚠️ 以下位置需要在各节实现时添加 init() 注册调用：
//
//	2.7  OpenAIModelClient      → Register("OpenAI", "llm", ...) + Register("OpenRouter", "llm", ...)
//	2.8  DashScopeModelClient   → Register("DashScope", "llm", ...)
//	2.9  DeepSeekModelClient    → Register("DeepSeek", "llm", ...)
//	2.10 SiliconFlowModelClient → Register("SiliconFlow", "llm", ...)
//	2.11 InferenceAffinity      → Register("InferenceAffinity", "llm", ...)
//	2.12 IntelliRouter          → Register("intelli_router", "llm", ...)
package model_clients

// Package model_clients 提供 LLM 模型客户端的接口定义、共享实现和注册机制。
// 本包是领域二（LLM 基础层）的核心，定义了所有模型客户端必须满足的 BaseModelClient 接口，
// 以及各客户端共享的 BaseClientEmbed 基础实现。
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
//	├── doc.go              # 包文档
//	├── base_client.go      # BaseModelClient 接口 + BaseOutputParser + BaseClientEmbed（Stream 返回纯 chunk channel）
//	├── registry.go         # ClientRegistry + CreateModelClient + ProviderValidator 桥接
//	├── messages_param.go   # MessagesParam 输入侧三态消息参数
//	├── invoke_params.go    # InvokeParams/StreamParams/Generate*Params + Functional Options
//	├── usage_extract.go    # provider usage 抽取四函数 + BuildUsageMetadata 统一入口（C-B）
//	├── reasoning_extract.go # 推理内容四级回退抽取（C-C）
//	└── transport_pool.go   # 共享 Transport 池（按安全形态分桶，C-A）
//
// 对应 Python 代码：
//
//	openjiuwen/core/foundation/llm/model_clients/base_model_client.py  — BaseModelClient
//	openjiuwen/core/foundation/llm/model_clients/__init__.py           — create_model_client
//	openjiuwen/core/common/clients/client_registry.py                 — ClientRegistry
//	openjiuwen/core/foundation/llm/output_parsers/output_parser.py    — BaseOutputParser (2.16 节)
//
// # 客户端注册（对齐上游 4 个实现类）
//
// 上游 openjiuwen/core/foundation/llm/model_clients/__init__.py:24-38 的
// _implementation_provider 只分派到 4 个实现，其余 provider 全部经别名归一
// （llm/endpointprofiles）落到 OpenAI 客户端，靠 endpoint_profile 区分方言：
//
//	OpenAI        → Register("OpenAI" / "OpenRouter" / "DeepSeek" / "DashScope" /
//	                "SiliconFlow" / "Moonshot" / "MiniMax" / "ModelArts" /
//	                "VolcEngine" / "Qianfan" / "Zhipu" / "MiMo" / "InferenceAffinity"
//	                / "AscendAffinity", "llm", openAIFactory)
//	intelli_router → Register("intelli_router", "llm", ...)（独立实现）
//	Anthropic / OpenAIAccount → 23.1 已落地（Anthropic）；23.2 已落地 Responses
//	（api_mode=responses 走 openai 包 responses_api.go 分流）；OpenAI 账号 OAuth
//	客户端本体仍 ⤵️ 23.6
//
// 23.3 已删除 dashscope / siliconflow / inference_affinity 三个独立客户端包：
// 其能力（DashScope 原生多模态三方法、SiliconFlow 工具清洗、KV cache 释放）
// 全部迁入 openai 包，由 endpoint_profile 与 extensions.kv_cache.mode 驱动。
package model_clients

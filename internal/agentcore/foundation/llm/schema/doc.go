// Package schema 定义 LLM 基础层的核心数据模型，包括消息、工具调用和用量元数据。
//
// 本包是 LLM 对话交互的数据核心，几乎所有 LLM 相关模块都直接或间接依赖本包定义的消息类型。
//
// 消息模型体系：
//
//	BaseMessage — 消息接口（定义 getter/setter 协议，role/content/name/metadata 等字段的读写方法）
//	  ├── DefaultMessage      — BaseMessage 的默认实现（嵌入结构体的基础构件）
//	  │     ├── UserMessage       — 用户消息（role="user"）
//	  │     └── SystemMessage     — 系统消息（role="system"）
//	  └── 扩展实现（嵌入 DefaultMessage + 额外字段）
//	        ├── AssistantMessage  — 助手消息（含 tool_calls/usage_metadata/finish_reason 等，自定义序列化）
//	        └── ToolMessage       — 工具返回消息（含 tool_call_id）
//
//	UnmarshalMessage — 通用消息反序列化工厂，根据 role 字段自动还原为对应子类型
//
// 流式消息块模型体系：
//
//	AssistantMessageChunk — 助手流式消息块（嵌入 AssistantMessage，Merge 方法增量合并）
//	ToolMessageChunk      — 工具返回流式消息块（嵌入 ToolMessage，Merge 方法增量合并）
//
// 多模态生成响应体系：
//
//	GenerationResponse         — 多模态生成响应基类（Model 字段）
//	  ├── ImageGenerationResponse   — 图片生成响应（images, images_base64, created）
//	  ├── AudioGenerationResponse   — 音频生成响应（audio_url, audio_data, duration, format）
//	  └── VideoGenerationResponse   — 视频生成响应（video_url, video_data, duration, resolution, format）
//
// 配置与模型信息体系：
//
//	ProviderType       — 模型服务提供商标识枚举（OpenAI/DashScope/DeepSeek 等 7 种）
//	ModelClientConfig  — 模型客户端配置（provider/api_key/api_base/timeout 等，支持 Extra 字段）
//	ModelRequestConfig — 模型请求配置（model/temperature/top_p 等，支持 Extra 字段）
//	BaseModelInfo      — 模型基础信息（合并连接信息+请求参数，支持 model/stream 别名和 Extra 字段）
//	ModelConfig        — 模型配置（组合 provider 名称和 BaseModelInfo）
//	ProviderValidator  — 自定义 Provider 验器接口（预埋，后续领域注入）
//
// 辅助模型：
//
//	ToolCall        — 工具调用信息（扁平格式，支持 OpenAI 嵌套格式双向转换）
//	UsageMetadata   — 模型调用用量元数据（token 消耗、延迟、费用）
//	MessageContent  — 消息内容（支持纯文本和多模态两种格式）
//	ContentPart     — 多模态内容分片
//	RoleType        — 消息角色枚举
//
// 文件目录：
//
//	schema/
//	  doc.go                  — 包文档（本文件）
//	  tool_call.go            — ToolCall 结构体 + OpenAI 格式转换
//	  usage_metadata.go       — UsageMetadata 结构体
//	  message.go              — RoleType 枚举 + MessageContent + BaseMessage 接口 + DefaultMessage 默认实现 + UserMessage + SystemMessage
//	  assistant_message.go    — AssistantMessage（嵌入 DefaultMessage + 扩展字段，自定义序列化）
//	  tool_message.go         — ToolMessage（嵌入 DefaultMessage + 扩展字段）
//	  message_factory.go      — UnmarshalMessage 通用消息反序列化工厂
//	  message_chunk.go        — AssistantMessageChunk + ToolMessageChunk（流式消息块）
//	  generation_response.go  — 多模态生成响应（图片/音频/视频）
//	  config.go               — ProviderType + ModelClientConfig + ModelRequestConfig + ProviderValidator
//	  model_info.go           — BaseModelInfo + ModelConfig
//	  tool_call_test.go       — ToolCall 测试
//	  usage_metadata_test.go  — UsageMetadata 测试
//	  message_test.go         — 消息基础类型测试
//	  assistant_message_test.go — AssistantMessage 测试
//	  tool_message_test.go    — ToolMessage 测试
//	  message_chunk_test.go   — 流式消息块测试
//	  generation_response_test.go — 多模态生成响应测试
//	  config_test.go          — ProviderType/ModelClientConfig/ModelRequestConfig 测试
//	  model_info_test.go      — BaseModelInfo/ModelConfig 测试
//
// 对应 Python 代码路径：
//
//	openjiuwen/core/foundation/llm/schema/message.py      — BaseMessage/AssistantMessage/UserMessage/SystemMessage/ToolMessage/UsageMetadata
//	openjiuwen/core/foundation/llm/schema/tool_call.py    — ToolCall
//	openjiuwen/core/foundation/llm/schema/message_chunk.py — 流式消息块（2.3 节）
//	openjiuwen/core/foundation/llm/schema/generation_response.py — 多模态生成响应（2.4 节）
//	openjiuwen/core/foundation/llm/schema/config.py       — ProviderType/ModelClientConfig/ModelRequestConfig（2.5 节）
//	openjiuwen/core/foundation/llm/schema/mode_info.py    — BaseModelInfo/ModelConfig（2.5 节）
package schema

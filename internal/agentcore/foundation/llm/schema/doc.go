// Package schema 定义 LLM 基础层的核心数据模型，包括消息、工具调用和用量元数据。
//
// 本包是 LLM 对话交互的数据核心，几乎所有 LLM 相关模块都直接或间接依赖本包定义的消息类型。
//
// 消息模型体系：
//
//	BaseMessage — 消息基类（role, content, name, metadata）
//	  ├── UserMessage       — 用户消息（role="user"）
//	  ├── SystemMessage     — 系统消息（role="system"）
//	  ├── AssistantMessage  — 助手消息（role="assistant"，含 tool_calls/usage_metadata/finish_reason 等）
//	  └── ToolMessage       — 工具返回消息（role="tool"，含 tool_call_id）
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
//	  message.go              — RoleType 枚举 + MessageContent + BaseMessage + UserMessage + SystemMessage
//	  assistant_message.go    — AssistantMessage（自定义序列化）
//	  tool_message.go         — ToolMessage
//	  tool_call_test.go       — ToolCall 测试
//	  usage_metadata_test.go  — UsageMetadata 测试
//	  message_test.go         — 消息基础类型测试
//	  assistant_message_test.go — AssistantMessage 测试
//	  tool_message_test.go    — ToolMessage 测试
//
// 对应 Python 代码路径：
//
//	openjiuwen/core/foundation/llm/schema/message.py      — BaseMessage/AssistantMessage/UserMessage/SystemMessage/ToolMessage/UsageMetadata
//	openjiuwen/core/foundation/llm/schema/tool_call.py    — ToolCall
//	openjiuwen/core/foundation/llm/schema/message_chunk.py — 流式消息块（2.3 节，本包暂不实现）
package schema

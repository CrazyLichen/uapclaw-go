// Package tool 提供工具系统的核心抽象，包括 Tool 接口、ToolCard 配置卡片和
// LifecycleTool 生命周期包装器。
//
// Tool 是 Agent 调用外部能力的统一抽象。LLM 返回 ToolCall 后，
// Agent 通过 Tool 接口执行工具调用并拿回结果。
// Tool 接口只定义纯业务方法（Invoke/Stream/Card），
// LifecycleTool 包装器负责在调用前后触发 ToolCallEvents 生命周期回调，
// AbilityManager 注册时自动包装，对调用方透明。
//
// 工具类型体系：
//
//	Tool 接口 — 统一抽象（Invoke/Stream/Card）
//	  ├── LocalFunction   — 本地函数工具（后续 3.3 节）
//	  ├── MCPTool         — MCP 协议远程工具（后续 3.5 节）
//	  └── RestfulApi      — RESTful API 工具（后续 3.8 节）
//
// Card 配置体系：
//
//	BaseCard (common/schema) — 数字名片基类
//	  └── ToolCard — 工具配置卡片（InputParams + Properties）
//	        ├── McpToolCard — MCP 工具卡片（后续 3.5 节）
//	        └── RestfulApiCard — RESTful API 工具卡片（后续 3.8 节）
//
// 回调生命周期：
//
//	LifecycleTool 包装器在 Invoke/Stream 调用前后自动触发以下事件：
//	  TOOL_CALL_STARTED → TOOL_INVOKE_INPUT → [执行] → TOOL_INVOKE_OUTPUT → TOOL_CALL_FINISHED
//	  异常时触发 TOOL_CALL_ERROR
//	  Stream 模式额外触发 TOOL_RESULT_RECEIVED（逐 chunk）
//
// 文件目录：
//
//	tool/
//	├── doc.go                # 包文档
//	├── base.go               # Tool 接口 + ToolCard + ToolOption + ToolCallOptions + StreamChunk
//	├── tool_info.go          # ToolCard.ToolInfo() — Param→JSON Schema 转换
//	├── lifecycle_tool.go     # LifecycleTool 包装器（回调生命周期）
//	└── schema/
//	    └── tool_info.go      # ToolCallEventType + ToolCallEventData + ToolCallbackFramework
//
// 对应 Python 代码：openjiuwen/core/foundation/tool/
package tool

// Package tool 提供工具系统的核心抽象，包括 Tool 接口、ToolCard 配置卡片、
// LifecycleTool 生命周期包装器和 LocalFunction 本地函数工具。
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
//	  ├── InvokeFunction[I,O] — 本地函数工具（Invoke 模式）
//	  ├── StreamFunction[I,O] — 本地函数工具（Stream 模式）
//	  ├── MapFunction         — 弱类型 map 降级工具
//	  ├── MCPTool             — MCP 协议远程工具（3.5 节）
//	  └── RestfulApi          — RESTful API 工具（3.8 节，service_api 子包）
//
// 本地函数工具：
//
//	LocalFunction 将 Go 函数包装为 Tool，LLM 可通过 function calling 调用。
//	用户定义强类型 Input/Output struct（带 json + jsonschema tag），
//	通过 NewInvokeFunction/NewStreamFunction 注册为工具。
//	Go 编译器自动推断泛型参数，schema 从 struct tag 反射提取。
//	便捷函数 NewTool/NewStreamTool 对标 Python @tool 装饰器。
//
//	Schema 提取流程：
//
//	用户定义 SearchInput struct（带 json + jsonschema tag）
//	    ↓
//	StructSchemaExtractor.Extract() — 反射提取 []*schema.Param
//	    ↓ json:"name" → Param.Name
//	    ↓ jsonschema:"description=xxx,required" → Param.Description, Param.Required
//	    ↓ jsonschema:"default=10" → Param.Default
//	    ↓ 递归处理嵌套 struct（→ ParamTypeObject）和 slice（→ ParamTypeArray）
//	ToolCard.InputParams → ToolCard.ToolInfo() → LLM function calling JSON Schema
//
//	调用流程：
//
//	LLM 返回 ToolCall.Arguments = map[string]any
//	    ↓
//	SchemaUtils.FormatWithSchema — 校验 + 填充默认值 + 移除 nil
//	    ↓
//	json.Marshal(map) → json.Unmarshal(&struct) — 类型转换
//	    ↓
//	调用用户函数 → struct → map → 返回
//
// Card 配置体系：
//
//	BaseCard (common/schema) — 数字名片基类
//	  └── ToolCard — 工具配置卡片（InputParams + Properties）
//	        ├── McpToolCard — MCP 工具卡片（3.5 节）
//	        └── RestfulApiCard — RESTful API 工具卡片（3.8 节，InputSchema 替代 InputParams）
//
// 回调生命周期（对齐 Python _ToolMeta 两步装饰链顺序）：
//
//	LifecycleTool 包装器在 Invoke/Stream 调用前后自动触发以下事件
//	（事件定义在 agentcore/runner/callback/ 包中）：
//	  Invoke: emit_before(INVOKE_INPUT) → TransformIO(input) → STARTED → [执行] → FINISHED → TransformIO(output) → emit_after(INVOKE_OUTPUT)
//	  Stream: emit_before(STREAM_INPUT) → TransformIO(input) → STARTED → [执行] → per-chunk{TransformIO(output) → RESULT_RECEIVED → STREAM_OUTPUT} → FINISHED → emit_after(STREAM_OUTPUT)
//	  异常时触发 TOOL_CALL_ERROR
//
// 文件目录：
//
//	tool/
//	├── doc.go                        # 包文档
//	├── base.go                       # Tool 接口 + ToolCard + ToolCard.ToolInfo() + ToolOption + ToolCallOptions + StreamChunk
//	├── lifecycle_tool.go             # LifecycleTool 包装器（回调生命周期）
//	├── struct_schema_extractor.go    # StructSchemaExtractor — struct tag→[]*Param 反射提取器
//	├── schema_utils.go               # SchemaUtils — 参数校验/格式化/RemoveNoneValues/FormatWithSchemaMap
//	├── invoke_function.go            # InvokeFunction[I,O] — 泛型本地函数工具（Invoke 模式）
//	├── stream_function.go            # StreamFunction[I,O] — 泛型本地函数工具（Stream 模式）
//	├── map_function.go               # MapFunction — 弱类型 map 降级工具
//	├── tool_func.go                  # NewTool/NewStreamTool 便捷注册函数 + ToolFuncOption
//	├── auth/
//	│   ├── doc.go              # 认证子包文档
//	│   ├── auth.go             # ToolAuthConfig + ToolAuthResult 数据模型
//	│   └── auth_callback.go    # AuthType + AuthStrategy + Registry + 回调注册 + HeaderQueryProvider
//	├── form_handler/
//	│   ├── doc.go              # 表单处理器子包文档
//	│   └── form_handler.go     # FormHandler 接口 + DefaultFormHandler + FormHandlerManager
//	├── service_api/
//	│   ├── doc.go                    # RESTful API 子包文档
//	│   ├── restful_api.go            # RestfulApiCard + RestfulApi + URL/路径参数校验
//	│   ├── api_param_mapper.go       # APIParamLocation 枚举 + APIParamMapper
//	│   └── response_parser.go        # BaseResponseParser + JSON/Text 解析器 + 解压器 + ParserRegistry
//	└── mcp/
//	    ├── doc.go                        # MCP 包文档
//	    ├── base.go                       # MCPTool + ExtractMCPToolResultContent + 类型重导出
//	    ├── client.go                     # NewMcpClient 工厂函数
//	    ├── types/
//	    │   └── types.go                  # 共享类型（McpServerConfig/McpToolCard/McpClient 接口等）
//	    └── client/
//	        ├── doc.go                    # 客户端子包文档
//	        ├── helpers.go                # 共享辅助函数（结果转换、JSON Schema 解析）
//	        ├── sse_client.go             # SseClient 实现
//	        ├── stdio_client.go           # StdioClient 实现
//	        ├── streamable_http_client.go # StreamableHttpClient 实现
//	        ├── openapi_client.go         # OpenApiClient 实现
//	        └── playwright_client.go      # PlaywrightClient 实现
//
// 对应 Python 代码：openjiuwen/core/foundation/tool/
package tool

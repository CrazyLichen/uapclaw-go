// Package tracer 提供会话追踪的 Span 定义、SpanManager 管理和追踪处理器。
//
// 本包实现了追踪跨度（Span）的核心数据结构、管理器和处理器，
// 完全对齐 Python 的 Span/TraceAgentSpan/TraceWorkflowSpan/SpanManager/TraceBaseHandler 设计：
//
// Span 是基础追踪跨度，包含 traceId/invokeId/startTime/endTime/inputs/outputs 等通用字段；
// TraceAgentSpan 嵌入 Span 并扩展 invokeType/name/elapsedTime/metaData 等 Agent 相关字段；
// TraceWorkflowSpan 嵌入 Span 并扩展 executionId/workflowId/componentId 等工作流相关字段；
// SpanManager 负责管理 Span 的创建、查询、更新和移除生命周期；
// TraceAgentHandler 处理 Agent 层追踪事件（链式/LLM/提示词/插件/检索/评估/工作流调用）；
// TraceWorkflowHandler 处理工作流层追踪事件（组件调用/预调用/流式/交互等）；
// Tracer 管理追踪器生命周期，维护 Agent/Workflow 事件分发表，将 TraceEvent 映射到对应 handler 方法。
//
// 文件目录：
//
//	tracer/
//	├── doc.go           # 包文档
//	├── data.go          # InvokeType/NodeStatus/TraceEvent 枚举定义
//	├── span.go          # Span/TraceAgentSpan/TraceWorkflowSpan 结构体 + SpanManager 管理器
//	├── handler.go       # TraceAgentHandler/TraceWorkflowHandler 追踪处理器
//	├── tracer.go        # Tracer 追踪器 + TriggerParams + 事件分发表
//	├── workflow.go      # BaseWorkflowSession 接口 + TracerWorkflowUtils 工作流追踪工具集
//	└── decorator.go     # TracedModelClient/TracedTool/TracedWorkflow 装饰器 + Decorate*WithTrace 工厂函数
//
// 对应 Python 代码：openjiuwen/core/session/tracer/span.py + openjiuwen/core/session/tracer/data.py + openjiuwen/core/session/tracer/handler.py + openjiuwen/core/session/tracer/tracer.py + openjiuwen/core/session/tracer/workflow_tracer.py
package tracer

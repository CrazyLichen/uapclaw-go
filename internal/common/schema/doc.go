// Package schema 提供两个项目共享的基础数据模型。
//
// 本包定义了 Agent 系统和 LLM function calling 所需的核心元信息类型，
// 作为 agentcore 和 swarm 共用的类型基础层。
//
// 文件目录：
//
//	schema/
//	├── doc.go         # 包文档
//	├── card.go        # BaseCard 数字名片基类 + WorkflowCard 工作流卡片 + AgentCard Agent 卡片
//	├── param.go       # Param / ParamType 参数定义模型，支持嵌套结构
//	└── tool_info.go   # ToolInfoProvider 接口 + ToolInfo 本地工具描述 + McpToolInfo MCP 工具描述
//
// 核心类型：
//
//   - BaseCard：数字名片基类，提供 ID/Name/Description 和 ToolInfo() 方法
//   - WorkflowCard：工作流配置卡片，增加 Version 和 InputParams
//   - AgentCard：Agent 配置卡片，增加 InputParams/OutputParams/InterfaceURL
//   - Param / ParamType：参数定义模型，最终转换为 JSON Schema
//   - ToolInfoProvider：工具信息提供者接口，LLM 层统一消费 ToolInfo 和 McpToolInfo
//   - ToolInfo / McpToolInfo：工具描述信息；McpToolInfo 嵌入 ToolInfo 并扩展 ServerName
//
// 对应 Python 代码：openjiuwen/core/common/schema/
package schema

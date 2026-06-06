// Package schema 提供两个项目共享的基础数据模型。
//
// 本包定义了 Agent 系统和 LLM function calling 所需的核心元信息类型，
// 作为 agentcore 和 swarm 共用的类型基础层。
//
// 文件目录：
//
//	schema/
//	├── doc.go         # 包文档
//	├── card.go        # BaseCard 数字名片基类，所有 Card 类型均嵌入此结构体
//	├── param.go       # Param / ParamType 参数定义模型，支持嵌套结构
//	└── tool_info.go   # ToolInfo / McpToolInfo 工具描述信息
//
// 核心类型：
//
//   - BaseCard：数字名片基类，提供 ID/Name/Description 和 ToolInfo() 方法
//   - Param / ParamType：参数定义模型，最终转换为 JSON Schema
//   - ToolInfo / McpToolInfo：工具描述信息，供 LLM function calling 消费
//
// 对应 Python 代码：openjiuwen/core/common/schema/
package schema

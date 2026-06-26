// Package schema 提供 E2A 协议和 Gateway/AgentServer 通信所需的全部类型定义。
//
// 本包定义了 E2A 协议的核心数据模型，包括 RPC 方法名枚举（ReqMethod）、
// 事件类型枚举（EventType）、运行模式枚举（Mode）、消息方向类型枚举（MessageType）、
// 消息模型（Message）、Agent 请求/响应模型（AgentRequest/AgentResponse/AgentResponseChunk）、
// 权限上下文（PermissionContext）等，作为 swarm 层的类型基础。
//
// 文件目录：
//
//	schema/
//	├── doc.go           # 包文档
//	├── req_method.go    # ReqMethod 枚举（142 个 RPC 方法名）
//	├── event_type.go    # EventType 枚举（26 个事件类型）
//	├── mode.go          # Mode 枚举（6 个运行模式）
//	├── message.go       # MessageType 枚举 + Message 模型 + 工厂函数 + Validate
//	├── agent.go         # AgentRequest/AgentResponse/AgentResponseChunk 模型 + 工厂函数 + Validate
//	└── permission.go    # PermissionContext 权限上下文 + 派生方法 + 序列化 + 工厂函数 + Validate
//
// 对应 Python 代码：jiuwenswarm/common/schema/
package schema

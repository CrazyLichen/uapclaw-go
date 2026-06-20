// Package schema 提供上下文引擎的数据模型定义。
//
// 包含上下文引擎的配置、消息模型、事件模型等数据结构。
// 这些类型由 ContextEngine 和各 Processor 共同使用。
//
// 文件目录：
//
//	schema/
//	├── doc.go       # 包文档
//	├── config.go    # ContextEngineConfig 上下文引擎配置
//	└── offload.go   # Offload 消息模型（OffloadInfo + Offload 子类型 + Offloadable 接口 + 反序列化工厂）
//
// 对应 Python 代码：openjiuwen/core/context_engine/schema/
package schema

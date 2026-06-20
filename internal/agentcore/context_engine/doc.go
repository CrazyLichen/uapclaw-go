// Package context_engine 提供上下文引擎的核心抽象和数据结构。
//
// 上下文引擎负责管理 Agent 会话中的对话消息生命周期、
// 构建 LLM 推理所需的上下文窗口、以及消息压缩和卸载等处理。
// 它是 Session 和 LLM 之间的桥梁：Session 管理会话状态，
// ModelContext 管理 LLM 看到的"上下文视图"。
//
// 文件目录：
//
//	context_engine/
//	├── doc.go           # 包文档
//	├── base.go          # ModelContext 接口 + ContextStats + ContextWindow + ContextEngine 接口
//	│                   # ContextEngine 方法直接使用 *session.Session（循环依赖已通过 single_agent/interfaces 解决）
//	└── token/
//	    ├── doc.go       # Token 子包文档
//	    └── base.go      # TokenCounter 接口定义
//
// 对应 Python 代码：openjiuwen/core/context_engine/
package context_engine

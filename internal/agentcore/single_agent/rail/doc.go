// Package rail 提供 Agent 生命周期 Rail 执行器。
//
// 本包仅保留 RailExecutor（@rail 装饰器的 Go 等价），
// 将函数包裹在 before/after/on_exception 钩子中执行。
//
// Rail 系统的核心类型（AgentCallbackEvent、AgentCallbackContext、
// AgentCallbackManager、AgentRail、BaseRail 等）已迁至
// single_agent/interfaces 包，以打破 rail ↔ interfaces 循环依赖。
//
// 文件目录：
//
//	rail/
//	├── doc.go       # 包文档
//	└── executor.go  # RailExecutor 结构体（@rail 装饰器等价）+ ModelCallRail/ToolCallRail
//
// 核心类型索引：
//
//	RailExecutor — @rail 装饰器等价（before/gate/exception/retry/after 包裹执行）
//	ModelCallRail — 模型调用 Rail 执行器
//	ToolCallRail  — 工具调用 Rail 执行器
//
// 对应 Python 代码：openjiuwen/core/single_agent/rail/base.py（RailExecutor 部分）
package rail

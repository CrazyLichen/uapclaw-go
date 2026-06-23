// Package rail 提供 Agent 生命周期 Rail 系统的基础定义。
//
// Rail 是 class-based 的生命周期钩子机制，允许在 Agent 执行流程的
// 特定时机注入拦截逻辑（重试、提前终止、steering 等）。
//
// 本包与框架层 callback/ 包的事件体系是不同层次：
//   - 本包 AgentCallbackEvent = per-Agent 实例级生命周期事件
//   - callback.AgentCallGlobalEventType = 框架级全局观测事件
//
// 两者不桥接，各自独立触发，与 Python 保持一致。
//
// 文件目录：
//
//	rail/
//	├── doc.go       # 包文档
//	└── event.go     # AgentCallbackEvent 枚举定义
//
// 对应 Python 代码：openjiuwen/core/single_agent/rail/base.py
package rail

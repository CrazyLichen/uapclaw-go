// Package rails 提供 DeepAgent 扩展 Rail 实现。
//
// 在 single_agent/rail 基础上增加：
//   - DeepAgentRail 基类：扩展 AgentRail，增加 workspace/sys_operation 和 task-iteration hooks
//   - ProgressiveToolRail：渐进式工具权限 Rail
//   - TaskCompletionRail：任务完成检测 Rail（注入完成信号提示、检测 promise 标签、通知 LoopCoordinator 停止循环）
//   - TaskPlanningRail：任务规划 Rail（注册 todo 工具、注入规划提示词、模型切换、进度提醒、token 统计、Todo↔TaskPlan 同步）
//   - AgentModeRail：三层防御式 plan 模式约束 Rail（模式切换工具注册、plan 模式提示词注入、白名单+路径校验拦截、task_tool 动态注册）
//   - HeartbeatRail：心跳护栏 Rail（心跳运行时注入 HEARTBEAT.md 内容到系统提示词，非心跳运行时静默跳过）
//   - McpRail：MCP 资源浏览工具注册 Rail（注册 ListMcpResources/ReadMcpResource 到 ResourceMgr + AbilityManager）
//
// 文件目录：
//
//	rails/
//	├── doc.go              # 包文档
//	├── base.go             # DeepAgentRail 基类 + DeepAgentRailProvider 接口
//	├── progressive.go      # ProgressiveToolRail 渐进式工具发现和可调用工具过滤
//	├── task_completion.go  # TaskCompletionRail 任务完成检测
//	├── task_planning.go    # TaskPlanningRail 任务规划（7个钩子）
//	├── agent_mode.go       # AgentModeRail plan 模式约束（3个钩子）
//	├── heartbeat.go        # HeartbeatRail 心跳护栏（3个钩子）
//	└── mcp_rail.go         # McpRail MCP 资源浏览工具注册（2个钩子：Init/Uninit）
//
// 对应 Python 代码：openjiuwen/harness/rails/
package rails

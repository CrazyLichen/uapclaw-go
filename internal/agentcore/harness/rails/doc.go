// Package rails 提供 DeepAgent 扩展 Rail 实现。
//
// 在 single_agent/rail 基础上增加：
//   - DeepAgentRail 基类：扩展 AgentRail，增加 workspace/sys_operation 和 task-iteration hooks
//   - ProgressiveToolRail：渐进式工具权限 Rail
//   - TaskCompletionRail：任务完成检测 Rail（注入完成信号提示、检测 promise 标签、通知 LoopCoordinator 停止循环）
//
// 文件目录：
//
//	rails/
//	├── doc.go              # 包文档
//	├── base.go             # DeepAgentRail 基类
//	├── progressive.go      # ProgressiveToolRail 渐进式工具发现和可调用工具过滤
//	└── task_completion.go  # TaskCompletionRail 任务完成检测
//
// 对应 Python 代码：openjiuwen/harness/rails/
package rails

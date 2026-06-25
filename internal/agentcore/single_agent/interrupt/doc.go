// Package interrupt 提供工具中断（Human-In-The-Loop）机制的处理器和类型别名。
//
// 中断机制允许工具执行过程中暂停 ReAct 循环，等待用户确认或输入后恢复执行。
// 三种触发路径：
//   - Path 1: before hook 返回 ToolInterruptException → RailExecutor 异常路径
//   - Path 2: tool.Invoke() 返回 ToolInterruptException → RailExecutor 异常路径
//   - Path 3: 子 Agent 返回 result_type="interrupt" dict → 正常路径
//
// HITL 中断处理流程（6.14-6.16）：
//   - BuildInterruptState: 收集中断 → 构建 ToolInterruptionState
//   - CommitInterrupt: 持久化上下文 + 保存中断状态 → 构建中断结果
//   - WriteInterruptToStream: 写入中断到 session stream
//   - HandleResume: 恢复时重执行中断工具 → 检查新中断 → 设置恢复迭代
//
// 本包避免对 agents 包的直接依赖，通过 InterruptAgent 最小接口解耦。
//
// 类型定义已迁移至 sa/schema 包（exception.go/response.go/state.go），
// 本包通过类型别名 re-export 保持 API 兼容。
//
// 文件目录：
//
//	interrupt/
//	├── doc.go           # 包文档
//	├── response.go      # InterruptRequest + ToolCallInterruptRequest（re-export 自 sa/schema）
//	├── exception.go     # ToolInterruptException（re-export 自 sa/schema）
//	├── state.go         # 常量 + 中断状态类型（re-export 自 sa/schema）
//	└── handler.go       # ResumeContext + InterruptAgent + ExecuteToolCallFunc + ToolInterruptHandler
//
// 对应 Python 代码：openjiuwen/core/single_agent/interrupt/
package interrupt

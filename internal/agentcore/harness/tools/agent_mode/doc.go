// Package agent_mode 提供 Agent 模式切换工具的实现。
//
// 包含三个工具：
//   - SwitchModeTool: 在 normal/plan 模式间切换
//   - EnterPlanModeTool: 初始化 plan 文件并返回路径
//   - ExitPlanModeTool: 读取 plan 全文并结束规划阶段
//   - CreateTaskTool: 动态创建 task_tool 实例（供 AgentModeRail 注册）
//
// 以及辅助函数：
//   - GenerateWordSlug: 生成 adjective-verb-noun 格式的随机 slug
//   - ResolvePlanFilePath: 根据工作区根路径和 slug 推导 plan 文件路径
//   - GetOrCreatePlanSlug: 生成不与已有 plan 文件冲突的 slug
//
// 文件目录：
//
//	agent_mode/
//	├── doc.go                 # 包文档
//	├── slug.go                # Slug 生成函数
//	├── switch_mode.go         # SwitchModeTool 切换模式
//	├── enter_plan_mode.go     # EnterPlanModeTool 进入规划
//	├── exit_plan_mode.go      # ExitPlanModeTool 退出规划
//	└── task_tool_factory.go   # CreateTaskTool 工厂函数
//
// 对应 Python 代码：openjiuwen/harness/tools/agent_mode_tools.py
package agent_mode

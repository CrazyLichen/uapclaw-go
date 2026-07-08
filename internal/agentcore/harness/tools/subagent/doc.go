// Package subagent 提供子代理会话工具集，包含异步子任务派生（SessionsSpawnTool）、
// 任务状态跟踪（SessionToolkit）和任务列表/取消操作。
// 工具通过 interfaces.DeepAgentInterface 访问 LoopController 获取 TaskManager/TaskScheduler，
// 对齐 Python: loop_controller.task_manager / loop_controller.task_scheduler。
//
// 对齐 Python: openjiuwen/harness/tools/subagent/
//
// 文件目录：
//
//	subagent/
//	├── doc.go              # 包文档
//	├── session_tools.go    # SessionTaskRow + SessionToolkit + SessionsList/Spawn/Cancel 工具
//	└── task_tool.go        # TaskTool 子代理委托工具 + CreateTaskTool 工厂
//
// 对应 Python 代码：openjiuwen/harness/tools/subagent/
package subagent

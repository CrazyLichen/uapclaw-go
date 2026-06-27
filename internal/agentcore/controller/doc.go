// Package controller 提供事件驱动任务编排控制器，是 ControllerAgent 的核心组件。
//
// Controller 负责：
//   - 处理事件（通过 EventQueue + EventHandler）
//   - 管理任务生命周期（通过 TaskManager + TaskScheduler）
//   - 执行意图识别和处理（通过 IntentRecognizer）
//   - 流式/批量输出（通过 Session StreamIterator）
//
// 核心方法：
//   - NewController() + Init()：两阶段初始化
//   - Start()/Stop()：生命周期管理
//   - Stream()/Invoke()：流式/批量执行
//   - BindSession()/UnbindSession()：会话绑定/解绑
//   - SetEventHandler()：注入事件处理器
//   - AddTaskExecutor()/RemoveTaskExecutor()：注册/移除任务执行器
//
// 文件目录：
//
//	controller/
//	├── doc.go           # 包文档
//	├── controller.go    # Controller 主结构体
//	├── config/          # 控制器配置
//	│   ├── doc.go
//	│   └── controller_config.go
//	├── modules/         # 核心子模块
//	│   ├── doc.go
//	│   ├── event_handler.go
//	│   ├── event_queue.go
//	│   ├── intent_recognizer.go
//	│   ├── intent_toolkits.go
//	│   ├── task_executor.go
//	│   ├── task_manager.go
//	│   └── task_scheduler.go
//	└── schema/          # 公共类型定义
//	    ├── doc.go
//	    ├── controller_output.go
//	    ├── dataframe.go
//	    ├── event.go
//	    ├── intent.go
//	    ├── task.go
//	    └── task_status.go
//
// 对应 Python 代码：openjiuwen/core/controller/
package controller

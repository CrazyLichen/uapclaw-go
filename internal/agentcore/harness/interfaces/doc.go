// Package interfaces 提供 harness 层核心接口定义，解耦子包间的编译依赖。
//
// Python 中 DeepAgent 定义在 harness/deep_agent.py，task_loop 和 subagent
// 通过 TYPE_CHECKING 延迟导入。Go 用此接口包达到等价效果：
// 消费者依赖接口定义而非具体实现，避免循环依赖。
//
// 文件目录：
//
//	interfaces/
//	├── doc.go           # 包文档
//	└── deep_agent.go    # DeepAgentInterface + LoopCoordinatorInterface
//
// 对应 Python 代码：openjiuwen/harness/deep_agent.py
package interfaces

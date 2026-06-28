// Package runner 提供全局运行器，编排 Agent/Workflow 的执行生命周期。
//
// Python 中 Runner 是全局单例类（@classmethod 代理到 GLOBAL_RUNNER），
// Go 采用全局 Runner 结构体 + 包级函数代理模式。
//
// 文件目录：
//
//	runner/
//	├── callback/             # 回调框架子包
//	├── config/               # Runner 全局配置子包（RunnerConfig/DistributedConfig/PulsarConfig 等）
//	├── message_queue/        # 消息队列子包（MessageQueueBase 接口 + InMemory/Local 实现）
//	├── resources_manager/    # 资源注册表子包（Agent/Tool/Workflow/Model/Prompt 全局注册）
//	├── spawn/                # 子进程 Spawn 子包（SpawnedProcessHandle/SpawnAgentConfig/Protocol 等）
//	├── doc.go                # 包文档
//	├── ref.go                # AgentRef/WorkflowRef 引用类型
//	└── runner.go             # Runner 结构体 + 全局实例 + 全部包级函数
//
// 对应 Python 代码：openjiuwen/core/runner/runner.py
package runner

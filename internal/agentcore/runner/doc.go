// Package runner 提供全局运行器，编排 Agent/Workflow 的执行生命周期。
//
// Python 中 Runner 是全局单例类（@classmethod 代理到 GLOBAL_RUNNER），
// Go 采用包级全局函数模式，更符合 Go 惯用法。
//
// 文件目录：
//
//	runner/
//	├── callback/             # 回调框架子包
//	├── message_queue/        # 内存消息队列子包
//	├── resources_manager/    # 资源注册表子包（Agent/Tool/Workflow/Model/Prompt 全局注册）
//	├── doc.go                # 包文档
//	└── runner.go             # RunAgent/RunWorkflow 全局函数
//
// 对应 Python 代码：openjiuwen/core/runner/runner.py
package runner

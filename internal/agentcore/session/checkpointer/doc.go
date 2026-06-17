// Package checkpointer 提供会话状态检查点持久化能力。
//
// Checkpointer 在会话生命周期的关键节点（pre/post agent/workflow 执行、
// 中断时）保存和恢复会话状态，支持会话中断后恢复执行。
//
// 工厂模式支持多种存储后端：InMemory（内存）、Persistence（SQLite/Shelve）、
// Redis。通过 CheckpointerProvider 注册，CheckpointerFactory 创建。
//
// 文件目录：
//
//	checkpointer/
//	├── doc.go              # 包文档
//	├── base.go             # Checkpointer/Storage 接口、命名空间常量、Key 构建函数
//	├── serializer.go       # Serializer 接口、JSONSerializer 实现
//	├── inmemory.go         # InMemoryCheckpointer、AgentStorage/AgentTeamStorage/WorkflowStorage
//	├── factory.go          # CheckpointerFactory、CheckpointerProvider、CheckpointerConfig
//	├── base_test.go        # 基础接口和常量测试
//	├── serializer_test.go  # Serializer 测试
//	├── inmemory_test.go    # InMemoryCheckpointer 测试
//	└── factory_test.go     # CheckpointerFactory 测试
//
// 对应 Python 代码：openjiuwen/core/session/checkpointer/
package checkpointer

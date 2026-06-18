// Package checkpointer 提供会话状态检查点持久化能力。
//
// Checkpointer 在会话生命周期的关键节点（pre/post agent/workflow 执行、
// 中断时）保存和恢复会话状态，支持会话中断后恢复执行。
//
// 工厂模式支持多种存储后端：InMemory（内存）、Persistence（SQLite/任何 BaseKVStore）。
// 通过 CheckpointerProvider 注册，CheckpointerFactory 创建。
//
// 文件目录：
//
//	checkpointer/
//	├── doc.go              # 包文档
//	├── base.go             # 接口类型别名（→ interfaces 包）、命名空间常量、Key 构建函数
//	├── serializer.go       # Serializer 接口、JSONSerializer 实现
//	├── inmemory.go         # InMemoryCheckpointer、AgentStorage/AgentTeamStorage/WorkflowStorage
//	├── persistence.go      # PersistenceCheckpointer、EntityHooks、持久化存储实现
//	└── factory.go          # CheckpointerFactory、CheckpointerProvider、CheckpointerConfig
//
// 对应 Python 代码：openjiuwen/core/session/checkpointer/
package checkpointer

// Package checkpointing 提供训练检查点保存/恢复和技能演进数据文件系统 IO。
//
// 本包是自演化框架的持久化层，负责：
//   - 断点续训：CheckpointManager 在关键节点保存 EvolveCheckpoint，
//     Trainer 下次启动时 ResumeIfNeeded 恢复状态
//   - 演进数据持久化：EvolutionStore 是技能文件系统的 IO 门面，
//     管理演进记录的 CRUD、Markdown 投影渲染、归档治理
//   - 待定变更缓冲：DefaultCheckpointManager._pending 在内存中暂存变更
//
// 注意：PendingChange 类型在本包中定义（而非 experience 包），
// 因为 Payload 依赖 EvolutionRecord（同包类型），
// Go 不允许 checkpointing ↔ experience 循环引用。
// experience 包通过类型别名 PendingChange = checkpointing.PendingChange 提供等效访问。
//
// 文件目录：
//
//	checkpointing/
//	├── doc.go               # 包文档
//	├── state.go             # EvolveCheckpoint 训练检查点数据类
//	├── types.go             # UsageStats/EvolutionPatch/EvolutionRecord/EvolutionLog/PendingChange 及序列化
//	├── manager.go           # CheckpointManager 接口 + DefaultCheckpointManager
//	├── store_file.go        # FileCheckpointStore 本地 JSON 检查点存储
//	├── evolution_store.go   # EvolutionStore 核心 IO 门面
//	├── store_records.go     # StoreRecordsHelper 记录 CRUD 持久化
//	├── store_projection.go  # StoreProjectionHelper Markdown 投影渲染
//	├── store_archive.go     # StoreArchiveHelper 归档/清空/创建技能
//	├── skill_package.go     # 打包/解包/skill_id 纯函数
//
// 对应 Python 代码：openjiuwen/agent_evolving/checkpointing/
package checkpointing

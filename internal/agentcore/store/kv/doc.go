// Package kv 提供键值存储的抽象接口定义和多种后端实现。
//
// 本包定义了所有 KV 存储后端必须满足的 BaseKVStore 接口，
// 以及用于批量操作的 KVPipeline 接口和 PipelineResult 结果类型。
// InMemoryKVStore 提供基于内存的并发安全实现，支持惰性过期检查。
// FileKVStore 提供基于 bbolt 的文件持久化实现，对应 Python ShelveStore，
// 严格复刻其语义（包括已知的值解包不一致和过期语义不一致）。
// 其他后端实现（数据库、Redis 等）将在后续版本中提供。
//
// 文件目录：
//
//	kv/
//	├── doc.go           # 包文档
//	├── base.go          # BaseKVStore 接口 + KVPipeline 接口 + PipelineResult 结构体
//	├── in_memory.go     # InMemoryKVStore 内存实现 + inMemoryPipeline
//	└── file.go          # FileKVStore 文件持久化实现 + filePipeline
//
// 对应 Python 代码：openjiuwen/core/foundation/store/base_kv_store.py
//
//	InMemoryKVStore 对应: openjiuwen/core/foundation/store/kv/in_memory_kv_store.py
//	FileKVStore 对应:     openjiuwen/core/foundation/store/kv/shelve_store.py
//
// 核心类型/接口索引：
//
//	BaseKVStore      — KV 存储基础接口，定义 Get/Set/Delete 等单键操作
//	KVPipeline       — 批量操作接口，支持 Set/Get/Exists 管道和 Execute 提交
//	PipelineResult   — 管道操作结果，包含 Op/Key/Value/Exists/Err 字段
//	InMemoryKVStore  — 内存实现，并发安全，支持惰性过期检查
//	FileKVStore      — 文件持久化实现（bbolt），对应 Python ShelveStore
package kv

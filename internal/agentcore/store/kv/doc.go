// Package kv 提供键值存储的抽象接口定义和批量操作管道。
//
// 本包定义了所有 KV 存储后端必须满足的 BaseKVStore 接口，
// 以及用于批量操作的 KVPipeline 接口和 PipelineResult 结果类型。
// 具体实现（内存、文件、数据库、Redis 等）在各子文件中提供。
//
// 文件目录：
//
//	kv/
//	├── doc.go           # 包文档
//	└── base.go          # BaseKVStore 接口 + KVPipeline 接口 + PipelineResult 结构体
//
// 对应 Python 代码：openjiuwen/core/foundation/store/base_kv_store.py
package kv

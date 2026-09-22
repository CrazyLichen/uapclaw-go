// Package persistence 提供上下文记忆演化系统的记忆持久化助手。
//
// MemoryPersistenceHelper 支持 JSON 文件和 Milvus 双后端。
// "auto" 模式探测 Milvus 可达性后回退 JSON。
// P1 阶段只实现 JSON 后端，Milvus 在 P7 启用。
//
// 对应 Python 代码：openjiuwen/extensions/context_evolver/core/persistence.py
package persistence

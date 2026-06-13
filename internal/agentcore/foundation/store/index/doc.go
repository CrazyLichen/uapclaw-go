// Package index 提供记忆索引的抽象接口和数据模型。
//
// 本包定义了所有记忆索引实现必须满足的 BaseMemoryIndex 接口，
// 以及 MemoryDoc 数据模型、StorageCodec 编解码器接口、
// MemorySearchResult 搜索结果类型和 MemoryIndexBase 默认实现基类。
// 具体实现类（如 SimpleMemoryIndex）嵌入 MemoryIndexBase 后
// 只需实现核心抽象方法即可满足 BaseMemoryIndex 接口。
//
// 文件目录：
//
//	index/
//	├── doc.go           # 包文档
//	├── base.go          # StorageCodec + MemoryDoc + MemorySearchResult + UserScope + BaseMemoryIndex + MemoryIndexBase
//	├── base_test.go     # 基类单元测试
//	├── simple.go        # SimpleMemoryIndex 简单记忆索引实现
//	└── simple_test.go   # SimpleMemoryIndex 单元测试
//
// 对应 Python 代码：
//
//	openjiuwen/core/foundation/store/base_memory_index.py
//	openjiuwen/core/foundation/store/index/simple_memory_index.py
//
// 核心类型/接口索引：
//
//	StorageCodec        — 存储编解码器接口，用于对记忆文本加解密
//	MemoryDoc           — 记忆文档数据模型（ID/Text/Type/Timestamp/Fields）
//	MemorySearchResult  — 搜索结果，包含 MemoryDoc 和相关度分数
//	UserScope           — 用户-作用域对，ListUserScopes 返回值
//	BaseMemoryIndex     — 记忆索引抽象接口（16 个方法）
//	MemoryIndexBase     — 默认实现基类，提供 7 个非抽象方法的通用行为
//	SimpleMemoryIndex   — 简单记忆索引，KV + Vector 双存储实现
package index

// Package store 提供 Pregel 引擎的检查点状态持久化地基：GraphState 快照、
// Store 存储抽象、GraphStore 日志壳、InMemoryStore 内存实现与 serde 序列化。
// 依赖方向 store→pregel 单向（对齐 Python TYPE_CHECKING 的 Message 引用）。
//
// 文件目录：
//
//	store/
//	├── doc.go        # 包文档
//	├── base.go       # PendingNode/GraphState/Store/CreateState/GraphStore 壳
//	├── inmemory.go   # InMemoryStore 内存实现
//	└── serde.go      # Serializer 接口与 JSON 实现
//
// 对应 Python 代码：openjiuwen/core/graph/store/
package store

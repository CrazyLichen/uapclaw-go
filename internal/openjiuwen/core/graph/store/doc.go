// Package store 提供 Pregel 引擎的检查点状态持久化地基：GraphStore 日志壳、
// InMemoryStore 内存实现与 serde 序列化。
// GraphState/Store/CreateState 原定义于此，2026-11-15 8.3c 实施时迁入 pregel
// 包（engine 依赖破环，spec 偏差 B5，与 8.3b D6 同源）；依赖方向 store→pregel 单向。
//
// 文件目录：
//
//	store/
//	├── doc.go        # 包文档
//	├── base.go       # GraphStore 日志壳（GraphState/Store/CreateState 已迁 pregel，B5）
//	├── inmemory.go   # InMemoryStore 内存实现
//	└── serde.go      # Serializer 接口与 JSON 实现
//
// 对应 Python 代码：openjiuwen/core/graph/store/
package store

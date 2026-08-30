// Package manage 提供记忆管理器及其子组件。
//
// 本包实现记忆系统的核心管理器（FragmentMemoryManager、SummaryManager、VariableManager）
// 以及统一写入管理器（WriteManager）和搜索管理器（SearchManager）。
// FragmentMemoryManager 和 SummaryManager 通过 BaseMemoryManager 接口统一抽象，
// 通过 BaseMemoryIndex 接口委托存储操作。
// VariableManager 独立实现 BaseMemoryManager 接口，通过 BaseKVStore 委托 KV 存储操作。
// WriteManager 作为写入操作统一路由器，按 mem_type 分发到对应子 Manager。
// SearchManager 作为搜索操作统一路由器，按 search_type 分发语义搜索请求并聚合结果。
//
// 文件目录：
//
//	manage/
//	├── doc.go              # 包文档
//	├── index/              # 记忆管理器实现
//	│   ├── doc.go          # index 包文档
//	│   ├── base_manager.go # BaseMemoryManager 接口 + memoryManagerBase 嵌入结构体
//	│   ├── fragment_manager.go # FragmentMemoryManager 碎片记忆管理器
//	│   ├── summary_manager.go  # SummaryManager 摘要记忆管理器
//	│   ├── variable_manager.go # VariableManager 变量记忆管理器
//	│   └── write_manager.go    # WriteManager 写入操作统一路由器
//	├── mem_model/          # 记忆数据模型和数据库操作
//	│   ├── doc.go          # mem_model 包文档
//	│   ├── memory_unit.go  # 记忆数据模型（MemoryType/OperationType/FragmentMemoryUnit 等）
//	│   └── ...             # 其他数据模型和数据库操作
//	├── search/             # 搜索操作统一路由器
//	│   ├── doc.go          # search 包文档
//	│   └── search_manager.go # SearchManager 搜索路由器
//	└── update/             # 记忆冲突检查
//	    ├── doc.go          # update 包文档
//	    └── update_checker.go # MemUpdateChecker 记忆冲突检查器
//
// 对应 Python 代码：
//
//	openjiuwen/core/memory/manage/
package manage

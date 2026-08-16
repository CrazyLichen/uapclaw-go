// Package manage 提供记忆管理器及其子组件。
//
// 本包实现记忆系统的核心管理器（FragmentMemoryManager、SummaryManager、VariableManager）
// 以及统一写入管理器（WriteManager）和搜索管理器（SearchManager）。
// 所有管理器通过 BaseMemoryManager 接口统一抽象，通过 BaseMemoryIndex 接口委托存储操作。
//
// 文件目录：
//
//	manage/
//	├── doc.go              # 包文档
//	├── index/              # 记忆管理器实现
//	│   ├── doc.go          # index 包文档
//	│   ├── base_manager.go # BaseMemoryManager 接口 + memoryManagerBase 嵌入结构体
//	│   └── fragment_manager.go # FragmentMemoryManager 碎片记忆管理器
//	├── mem_model/          # 记忆数据模型和数据库操作
//	│   ├── doc.go          # mem_model 包文档
//	│   ├── memory_unit.go  # 记忆数据模型（MemoryType/OperationType/FragmentMemoryUnit 等）
//	│   └── ...             # 其他数据模型和数据库操作
//	└── update/             # 记忆冲突检查
//	    ├── doc.go          # update 包文档
//	    └── update_checker.go # MemUpdateChecker 记忆冲突检查器
//
// 对应 Python 代码：
//
//	openjiuwen/core/memory/manage/
package manage

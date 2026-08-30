// Package index 提供记忆管理器接口和实现。
//
// 本包定义了 BaseMemoryManager 抽象接口和 memoryManagerBase 嵌入结构体，
// 提供记忆管理器的公共逻辑（参数校验、异常包装、加解密）。
// FragmentMemoryManager 管理碎片记忆，SummaryManager 管理摘要记忆，
// VariableManager 管理变量记忆（独立实现，不嵌入 memoryManagerBase）。
// WriteManager 作为写入操作统一路由器，按 mem_type 分发到对应子 Manager。
//
// 文件目录：
//
//	index/
//	├── doc.go               # 包文档
//	├── base_manager.go      # BaseMemoryManager 接口 + memoryManagerBase 嵌入结构体
//	├── fragment_manager.go  # FragmentMemoryManager 碎片记忆管理器
//	├── summary_manager.go   # SummaryManager 摘要记忆管理器
//	├── variable_manager.go  # VariableManager 变量记忆管理器
//	└── write_manager.go     # WriteManager 写入操作统一路由器
//
// 对应 Python 代码：
//
//	openjiuwen/core/memory/manage/index/
package index

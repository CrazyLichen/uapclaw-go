// Package index 提供记忆管理器接口和实现。
//
// 本包定义了 BaseMemoryManager 抽象接口和 memoryManagerBase 嵌入结构体，
// 提供记忆管理器的公共逻辑（参数校验、异常包装、加解密）。
// FragmentMemoryManager 是 BaseMemoryManager 的核心实现，
// 管理三种碎片记忆（用户画像、语义记忆、情景记忆）的全生命周期。
//
// 文件目录：
//
//	index/
//	├── doc.go               # 包文档
//	├── base_manager.go      # BaseMemoryManager 接口 + memoryManagerBase 嵌入结构体
//	└── fragment_manager.go  # FragmentMemoryManager 碎片记忆管理器
//
// 对应 Python 代码：
//
//	openjiuwen/core/memory/manage/index/
package index

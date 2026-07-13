// Package sysop_builder 提供系统操作（SysOperation）卡片和实例的构建器。
//
// 根据运行模式（local/sandbox）创建对应的 SysOperationCard 和 SysOperation 实例，
// 供 DeepAdapter._create_sys_operation 调用。
//
// 文件目录：
//
//	sysop_builder/
//	├── doc.go      # 包文档
//	└── builder.go  # 构建器函数
//
// 对应 Python 代码：jiuwenswarm/server/runtime/sysop_builder/
package sysop_builder

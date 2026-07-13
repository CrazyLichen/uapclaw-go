// Package sysop_builder 提供系统操作（SysOperation）卡片和实例的构建器。
//
// 根据运行模式（local/sandbox）创建对应的 SysOperationCard 和 SysOperation 实例，
// 供 DeepAdapter._create_sys_operation 调用。同时提供沙箱 filesystem policy 组装
// 和路径展示辅助。
//
// 文件目录：
//
//	sysop_builder/
//	├── doc.go       # 包文档
//	├── builder.go   # Card 构建 + SysOperation 实例化 + ResolveOperationMode
//	├── policy.go    # BuildFilesystemPolicy + 固有路径收集 + 辅助函数
//	└── display.go   # 展示辅助：ListAutoManaged/FindAutoManaged + 辅助函数
//
// 对应 Python 代码：jiuwenswarm/server/runtime/agent_adapter/sysop_builder.py
package sysop_builder

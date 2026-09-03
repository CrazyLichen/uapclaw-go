// Package common 提供记忆系统的公共工具。
//
// 本包包含 KV 前缀注册表（KvPrefixRegistry）等跨模块共享的基础设施组件，
// 供记忆管理器和迁移器使用。
//
// 文件目录：
//
//	common/
//	├── doc.go                  # 包文档
//	├── kv_prefix_registry.go   # KV 前缀注册表
//	└── base.go                 # 记忆索引名称生成/解析 + 命中结果解析
//
// 对应 Python 代码：
//
//	openjiuwen/core/memory/common/
package common

// Package registry 提供图记忆抽取模块的多语言注册表。
//
// 包含全局字典变量，由 cn/en 子包的 init() 填充。
// extraction 包和其他模块通过导入此包来访问注册表数据。
//
// 文件目录：
//
//	registry/
//	├── doc.go       # 包文档
//	└── registry.go  # 全局注册表变量
//
// 对应 Python 代码：openjiuwen/core/memory/graph/extraction/base.py + entity_extraction/base.py
package registry

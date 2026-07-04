// Package resources 存储 harness 层的静态资源文件。
//
// 本包通过 go:embed 嵌入 YAML 等资源，供 security 等子包在运行时加载，
// 对应 Python 源码 openjiuwen/harness/resources/ 目录。
//
// 文件目录：
//
//	resources/
//	├── doc.go              # 包文档
//	├── resources.go        # 规则结构体定义与解析函数
//	├── builtin_rules.yaml  # 内置参数级安全规则定义
//	└── resources_test.go   # 资源加载与解析测试
//
// 对应 Python 代码：openjiuwen/harness/resources/
package resources

// Package code 提供代码执行工具（CodeTool），封装 sys_operation.CodeOperation 的调用逻辑。
//
// CodeTool 支持 python、shell 等语言的代码执行，提供超时控制和本地模式下的 cwd 注入。
//
// 文件目录：
//
//	code/
//	├── doc.go    # 包文档
//	└── code.go   # CodeTool 代码执行工具
//
// 对应 Python 代码：openjiuwen/harness/tools/code.py
package code

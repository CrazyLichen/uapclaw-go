// Package sys_operation 提供系统操作抽象接口与配置类型。
//
// SysOperation 是 DeepAgent 对文件系统、Shell、代码执行等系统级操作的统一抽象。
// 具体实现分为 LocalSysOperation（本地执行）和 SandboxSysOperation（沙箱执行），
// 由 OperationMode 决定。
//
// 文件目录：
//
//	sys_operation/
//	├── doc.go                   # 包文档
//	├── sys_operation.go         # SysOperation/FsOperation/ShellOperation/CodeOperation 接口 + 枚举
//	└── sys_operation_card.go   # SysOperationCard + WorkConfig 类型
//
// 对应 Python 代码：openjiuwen/core/sys_operation/
package sys_operation

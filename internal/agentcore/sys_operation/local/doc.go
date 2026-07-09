// Package local 提供系统操作的本地执行实现。
//
// 本包实现 sys_operation 根包中定义的 FsOperation、ShellOperation、CodeOperation 接口，
// 在本地进程中直接执行文件系统和 Shell 命令操作。
//
// 文件目录：
//
//	local/
//	├── doc.go              # 包文档
//	├── utils.go            # AsyncProcessHandler + OperationUtils + StreamEvent/StreamEventType
//	├── shell_operation.go  # ShellOperation 本地 Shell 执行
//	├── fs_operation.go     # FsOperation 本地文件系统操作
//	└── code_operation.go   # CodeOperation 本地代码执行
//
// 对应 Python 代码：openjiuwen/core/sys_operation/local/
package local

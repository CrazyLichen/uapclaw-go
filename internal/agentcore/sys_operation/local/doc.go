// Package local 提供系统操作的本地执行实现。
//
// 本包实现 sys_operation 根包中定义的 FsOperation、ShellOperation、CodeOperation 接口，
// 在本地进程中直接执行文件系统和 Shell 命令操作。
//
// 文件目录：
//
//	local/
//	├── doc.go               # 包文档
//	├── shell_operation.go   # ShellOperation 本地 Shell 执行（含 WriteStdin/KillProcess/ListProcesses）
//	├── shell_helpers.go     # Shell 辅助函数（PowerShell/POSIX/Windows 检测与路径归一化）
//	├── fs_operation.go      # FsOperation 本地文件系统操作（含 Stream 上传/下载 + permissions）
//	├── code_operation.go    # CodeOperation 本地代码执行（含 -u/force_file/encoding/FileNotFoundError）
//	└── utils.go             # 公共工具（AsyncProcessHandler + OperationUtils + StreamEvent/StreamEventType）
//
// 对应 Python 代码：openjiuwen/core/sys_operation/local/
package local

// Package local 提供系统操作的本地执行实现。
//
// 本包实现 sys_operation 根包中定义的 FsOperation、ShellOperation、CodeOperation 接口，
// 在本地进程中直接执行文件系统和 Shell 命令操作。本地实现类型分别为 LocalFsOperation、
// LocalShellOperation、LocalCodeOperation。
//
// 文件目录：
//
//	local/
//	├── doc.go               # 包文档
//	├── shell_operation.go   # LocalShellOperation（本地Shell操作） 本地 Shell 执行（含 WriteStdin/KillProcess/ListProcesses）
//	├── shell_helpers.go     # Shell 辅助函数（PowerShell/POSIX/Windows 检测与路径归一化）
//	├── fs_operation.go      # LocalFsOperation（本地文件操作） 本地文件系统操作（含 Stream 上传/下载 + permissions + tail seek）
//	├── code_operation.go    # LocalCodeOperation（本地代码操作） 本地代码执行（含 -u/force_file/encoding/FileNotFoundError）
//	├── file_lock.go         # 跨进程文件锁接口（AcquireFileLock/ReleaseFileLock，对齐 Python _file_lock）
//	├── file_lock_unix.go    # Unix 平台文件锁实现（fcntl.flock + 轮询超时）
//	├── file_lock_windows.go # Windows 平台文件锁实现（LockFileEx + 轮询超时）
//	├── proc_unix.go         # Unix 平台进程组实现（Setpgid + 信号终止进程组）
//	├── proc_windows.go      # Windows 平台进程树终止实现（taskkill /T /F）
//	├── encoding_windows.go  # Windows 平台 ANSI 代码页检测（GetACP → cp936 等）
//	├── encoding_other.go    # 非 Windows 平台代码页检测占位（返回空）
//	└── utils.go             # 公共工具（AsyncProcessHandler + decodeOutput 编码解码 + OperationUtils）
//
// 对应 Python 代码：openjiuwen/core/sys_operation/local/
package local

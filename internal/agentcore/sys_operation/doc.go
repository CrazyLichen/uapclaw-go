// Package sys_operation 提供系统操作抽象接口与配置类型。
//
// SysOperation 是 DeepAgent 对文件系统、Shell、代码执行等系统级操作的统一抽象。
// 具体实现分为 LocalSysOperation（本地执行）和 SandboxSysOperation（沙箱执行），
// 由 OperationMode 决定。NewSysOperation 工厂函数根据 card.Mode 分支构造。
//
// 文件目录：
//
//	sys_operation/
//	├── doc.go                       # 包文档
//	├── sys_operation.go             # SysOperation 接口 + BaseSysOperation + LocalSysOperation + NewSysOperation 工厂
//	├── sys_operation_card.go        # SysOperationCard + LocalWorkConfig + SandboxGatewayConfig + ContainerScope
//	├── config.go                    # OperationMode/OperationMode 枚举 + 全局配置
//	├── base.go                      # BaseOperation 基础结构体
//	├── fs.go                        # FsOperation 接口 + BaseFsOperation + FsOptions
//	├── shell.go                     # ShellOperation 接口 + BaseShellOperation + ShellOptions + ShellType
//	├── code.go                      # CodeOperation 接口 + BaseCodeOperation + CodeOptions
//	├── registry.go                  # OperationRegistry + OperationDef + GlobalRegistry
//	├── shell_process_registry.go    # ShellProcessRegistry 会话级进程追踪 + SessionID context + TerminateShellProcess
//	├── shell_process_registry_unix.go    # POSIX 平台进程终止与等待实现（syscall.Kill/Wait4）
//	├── shell_process_registry_windows.go # Windows 平台进程终止与等待实现（proc.Wait）
//	├── tool_adapter.go              # SysOperationToolAdapter + ExtractTools + GetToolIDPrefix
//	├── cwd/                         # CWD 状态管理（三层 CWD 模型 + context 传播）
//	├── result/                      # 操作结果类型（BaseResult/ExecuteCmdResult/ReadFileResult/...）
//	└── local/                       # 本地实现
//	    ├── doc.go                   # 子包文档
//	    ├── shell_operation.go       # ShellOperation 本地实现
//	    ├── shell_helpers.go         # Shell 辅助函数（PowerShell/POSIX/Windows 检测与归一化）
//	    ├── fs_operation.go          # FsOperation 本地实现
//	    ├── code_operation.go        # CodeOperation 本地实现
//	    └── utils.go                 # 公共工具（AsyncProcessHandler/OperationUtils/...）
//
// 对应 Python 代码：openjiuwen/core/sys_operation/
package sys_operation

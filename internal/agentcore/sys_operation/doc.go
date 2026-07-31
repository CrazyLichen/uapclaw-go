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
//	├── sys_operation.go             # SysOperation（系统操作） 接口 + BaseSysOperation（基础系统操作） + LocalSysOperation（本地系统操作） + NewSysOperation（新建系统操作） 工厂
//	├── sys_operation_card.go        # SysOperationCard（操作卡片） + ToolIdProxy（工具ID代理） + generateIsolationKeyTemplate（生成隔离键模板）
//	├── config.go                    # LocalWorkConfig（本地工作配置） + SandboxIsolationConfig（沙箱隔离配置） + SandboxLauncherConfig（沙箱启动器配置） + PreDeployLauncherConfig（预部署启动器配置） + SandboxGatewayConfig（沙箱网关配置） + GatewayStoreConfig/Config（网关存储配置） + SandboxCreateRequest（沙箱创建请求） + GatewayInvokeRequest（网关调用请求） + OperationMode（操作模式）/ContainerScope（容器范围） 枚举
//	├── base.go                      # BaseOperation（基础操作） 基础结构体
//	├── fs.go                        # FsOperation（文件系统操作） 接口 + BaseFsOperation（基础文件系统操作） + FsOptions（文件系统选项）
//	├── shell.go                     # ShellOperation（Shell操作） 接口 + BaseShellOperation（基础Shell操作） + ShellOptions（Shell选项） + ShellType（Shell类型）
//	├── code.go                      # CodeOperation（代码操作） 接口 + BaseCodeOperation（基础代码操作） + CodeOptions（代码选项）
//	├── registry.go                  # OperationRegistry（操作注册表） + OperationDef（操作定义） + GlobalRegistry（全局注册表）
//	├── shell_process_registry.go    # ShellProcessRegistry 会话级进程追踪 + SessionID context + TerminateShellProcess
//	├── shell_process_registry_unix.go    # POSIX 平台进程终止与等待实现（syscall.Kill/Wait4）
//	├── shell_process_registry_windows.go # Windows 平台进程终止与等待实现（proc.Wait）
//	├── tool_adapter.go              # SysOperationToolAdapter（系统操作工具适配器） + ExtractTools（提取工具） + GetToolIDPrefix（获取工具ID前缀）
//	├── cwd/                         # CWD 状态管理（三层 CWD 模型 + context 传播）
//	├── result/                      # 操作结果类型（BaseResult/ExecuteCmdResult/ReadFileResult/...）
//	├── sandbox/                     # 沙箱执行模式（占位，随 9.34/9.36/9.37 实现）
//	└── local/                       # 本地实现
//	    ├── doc.go                   # 子包文档
//	    ├── shell_operation.go       # LocalShellOperation 本地实现
//	    ├── shell_helpers.go         # Shell 辅助函数（PowerShell/POSIX/Windows 检测与归一化）
//	    ├── fs_operation.go          # LocalFsOperation 本地实现
//	    ├── code_operation.go        # LocalCodeOperation 本地实现
//	    └── utils.go                 # 公共工具（AsyncProcessHandler/OperationUtils/...）
//
// 对应 Python 代码：openjiuwen/core/sys_operation/
package sys_operation

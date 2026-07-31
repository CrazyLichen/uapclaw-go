// Package sandbox 提供沙箱执行模式的实现，对应 OperationMode.Sandbox。
//
// 当前为占位包，沙箱执行的具体实现随 9.34 (SandboxSysOperation) 和 9.36/9.37
// (JiuwenBoxProvider / AioProvider) 逐步补齐。
//
// 对应 Python 代码：openjiuwen/core/sys_operation/sandbox/
//
// 文件目录：
//
//	sandbox/
//	└── doc.go           # 包文档（占位）
//
// 预计后续新增文件：
//
//	sandbox/
//	├── sandbox_mixin.go       # SandboxGatewayClientMixin（沙箱网关客户端混入）/BaseSandboxMixin（沙箱基础混入）
//	├── sandbox_registry.go    # SandboxRegistry（沙箱注册表） 启动器/Provider 注册
//	├── fs_operation.go        # SandboxFsOperation（沙箱文件操作）
//	├── shell_operation.go     # SandboxShellOperation（沙箱Shell操作）
//	├── code_operation.go      # SandboxCodeOperation（沙箱代码操作）
//	├── gateway/
//	│   ├── gateway.go         # SandboxGateway（沙箱网关） 单例
//	│   ├── gateway_client.go  # SandboxGatewayClient（沙箱网关客户端）
//	│   └── sandbox_store.go   # AbstractSandboxStore（抽象沙箱存储）/InMemorySandboxStore（内存沙箱存储）
//	├── launchers/
//	│   ├── base.go            # SandboxLauncher（沙箱启动器）/LaunchedSandbox（已启动沙箱）
//	│   └── pre_deployment_launcher.go  # PreDeploymentLauncher（预部署启动器）
//	└── providers/
//	    ├── base_provider.go   # BaseFSProvider（基础文件系统提供者）/BaseShellProvider（基础Shell提供者）/BaseCodeProvider（基础代码提供者）
//	    ├── jiuwenbox_provider.go  # JiuwenBoxProvider（九文盒子提供者） (9.36)
//	    └── aio_provider.go    # AioProvider（Aio提供者） (9.37)
package sandbox

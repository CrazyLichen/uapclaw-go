// Package sandbox 提供沙箱执行模式的实现，对应 OperationMode.Sandbox。
//
// 当前为占位包，沙箱执行的具体实现随 9.34 (SandboxSysOperation) 和 9.36/9.37
// (JiuwenBoxProvider / AioProvider) 逐步补齐。
//
// 对齐 Python：openjiuwen/core/sys_operation/sandbox/
//
// 文件目录：
//
//	sandbox/
//	└── doc.go           # 包文档（占位）
//
// 预计后续新增文件：
//
//	sandbox/
//	├── sandbox_mixin.go       # SandboxGatewayClientMixin / BaseSandboxMixin
//	├── sandbox_registry.go    # SandboxRegistry 启动器/Provider 注册
//	├── fs_operation.go        # SandboxFsOperation
//	├── shell_operation.go     # SandboxShellOperation
//	├── code_operation.go      # SandboxCodeOperation
//	├── gateway/
//	│   ├── gateway.go         # SandboxGateway 单例
//	│   ├── gateway_client.go  # SandboxGatewayClient
//	│   └── sandbox_store.go   # AbstractSandboxStore / InMemorySandboxStore
//	├── launchers/
//	│   ├── base.go            # SandboxLauncher / LaunchedSandbox
//	│   └── pre_deployment_launcher.go  # PreDeploymentLauncher
//	└── providers/
//	    ├── base_provider.go   # BaseFSProvider / BaseShellProvider / BaseCodeProvider
//	    ├── jiuwenbox_provider.go  # JiuwenBoxProvider (9.36)
//	    └── aio_provider.go    # AioProvider (9.37)
package sandbox

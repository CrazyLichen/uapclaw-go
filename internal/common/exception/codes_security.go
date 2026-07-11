package exception

// ──────────────────────────── 全局变量 ────────────────────────────

var (
	// 安全 / 护栏 190000–190999

	// StatusGuardrailBlocked 安全护栏拦截
	StatusGuardrailBlocked = NewStatusCode(
		"GUARDRAIL_BLOCKED", 190000,
		"guardrail blocked: risk_type='{risk_type}', risk_level='{risk_level}', event='{event}'")
	// StatusWsOriginDenied WebSocket Origin 校验拒绝
	StatusWsOriginDenied = NewStatusCode(
		"WS_ORIGIN_DENIED", 190001,
		"websocket origin denied: origin='{origin}', allowed_hosts='{allowed_hosts}'")

	// 系统操作 199000–199999

	// StatusSysOperationManagerProcessError 系统操作管理器处理错误
	StatusSysOperationManagerProcessError = NewStatusCode(
		"SYS_OPERATION_MANAGER_PROCESS_ERROR", 199001,
		"sys operation manager process error, process: {process}, reason: {error_msg}")
	// StatusSysOperationCardParamError 系统操作卡片参数错误
	StatusSysOperationCardParamError = NewStatusCode(
		"SYS_OPERATION_CARD_PARAM_ERROR", 199002,
		"sys operation card param error, reason: {error_msg}")
	// StatusSysOperationFsExecutionError 文件系统操作执行错误
	StatusSysOperationFsExecutionError = NewStatusCode(
		"SYS_OPERATION_FS_EXECUTION_ERROR", 199003,
		"file system operation execution error, execution: {execution}, reason: {error_msg}")
	// StatusSysOperationShellExecutionError Shell 操作执行错误
	StatusSysOperationShellExecutionError = NewStatusCode(
		"SYS_OPERATION_SHELL_EXECUTION_ERROR", 199004,
		"shell operation execution error, execution: {execution}, reason: {error_msg}")
	// StatusSysOperationCodeExecutionError 代码操作执行错误
	StatusSysOperationCodeExecutionError = NewStatusCode(
		"SYS_OPERATION_CODE_EXECUTION_ERROR", 199005,
		"code operation execution error, execution: {execution}, reason: {error_msg}")
	// StatusSysOperationRegistryError 系统操作注册表错误
	StatusSysOperationRegistryError = NewStatusCode(
		"SYS_OPERATION_REGISTRY_ERROR", 199006,
		"sys operation registry error, process: {process}, reason: {error_msg}")
	// StatusSysOperationSandboxGatewayError 沙箱网关错误
	StatusSysOperationSandboxGatewayError = NewStatusCode(
		"SYS_OPERATION_SANDBOX_GATEWAY_ERROR", 199007,
		"sandbox gateway error, operation: {operation}, reason: {error_msg}")
	// StatusSysOperationSandboxLauncherError 沙箱启动器错误
	StatusSysOperationSandboxLauncherError = NewStatusCode(
		"SYS_OPERATION_SANDBOX_LAUNCHER_ERROR", 199008,
		"sandbox launcher error, operation: {operation}, reason: {error_msg}")
	// StatusSysOperationSandboxProviderError 沙箱 provider 错误
	StatusSysOperationSandboxProviderError = NewStatusCode(
		"SYS_OPERATION_SANDBOX_PROVIDER_ERROR", 199009,
		"sandbox provider error, operation: {operation}, reason: {error_msg}")
	// StatusSysOperationSandboxIsolationKeyError 沙箱隔离 key 错误
	StatusSysOperationSandboxIsolationKeyError = NewStatusCode(
		"SYS_OPERATION_SANDBOX_ISOLATION_KEY_ERROR", 199010,
		"sandbox isolation key error, operation: {operation}, reason: {error_msg}")
)

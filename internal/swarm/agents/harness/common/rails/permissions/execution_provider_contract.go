package permissions

// ──────────────────────────── 常量 ────────────────────────────

// ExecutionProviderContractUnverified 宿主无法验证执行提供方契约时的原因码。
//
// Python: EXECUTION_PROVIDER_CONTRACT_UNVERIFIED
const ExecutionProviderContractUnverified = "execution_provider_contract_unverified"

// ──────────────────────────── 全局变量 ────────────────────────────

// jiuwenBoxSandboxExecutionTools 在 JiuwenBox 沙箱内执行、无需宿主回退的工具。
//
// Python: JIUWENBOX_SANDBOX_EXECUTION_TOOLS
var jiuwenBoxSandboxExecutionTools = map[string]struct{}{
	"bash": {}, "mcp_exec_command": {}, "code": {},
}

// acpIDEExecutionTools 由 ACP IDE 提供的执行工具。
//
// 目前未被两个判定函数使用（Python 侧同样只定义不使用），保留以对齐枚举集合。
//
// Python: ACP_IDE_EXECUTION_TOOLS
var acpIDEExecutionTools = map[string]struct{}{"create_terminal": {}}

// unknownProviderExecutionTools 执行提供方未知的工具。
//
// 同 ACP_IDE 集合，Python 侧也只定义不使用；保留以对齐。
//
// Python: UNKNOWN_PROVIDER_EXECUTION_TOOLS
var unknownProviderExecutionTools = map[string]struct{}{"cmd": {}, "powershell": {}}

// ──────────────────────────── 导出函数 ────────────────────────────

// RequiresNoHostFallback 判定自动执行是否必须留在 JiuwenBox（不允许宿主回退）。
//
// 两个条件同时满足才为真（Python 用 and 连接，不可改为单个集合判断）：
//   - toolCategory 属于 {shell, code}
//   - toolName 属于 JIUWENBOX_SANDBOX_EXECUTION_TOOLS
//
// 注意工具名必须是**归一化后**的规范名（调用方先用 NormalizeToolName）。
//
// Python: requires_no_host_fallback(tool_name, *, tool_category)
func RequiresNoHostFallback(toolName, toolCategory string) bool {
	if toolCategory != "shell" && toolCategory != "code" {
		return false
	}
	_, hit := jiuwenBoxSandboxExecutionTools[toolName]
	return hit
}

// RequiresManualExecutionProviderReview 判定宿主是否需要人工确认执行提供方。
//
// 条件：toolCategory 属于 {shell, code} 且 toolName **不**在 JiuwenBox 集合里。
// 与 RequiresNoHostFallback 在同一类别下恰有一个为真。
//
// Python: requires_manual_execution_provider_review(tool_name, *, tool_category)
func RequiresManualExecutionProviderReview(toolName, toolCategory string) bool {
	if toolCategory != "shell" && toolCategory != "code" {
		return false
	}
	_, hit := jiuwenBoxSandboxExecutionTools[toolName]
	return !hit
}

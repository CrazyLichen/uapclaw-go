package shell

// ──────────────────────────── 常量 ────────────────────────────

const (
	// callTimeoutGraceSeconds 外层调用预算在内层执行预算之上额外给的清理宽限（秒）。
	//
	// 内层超时要来得及 kill 进程树并排空管道；外层若与内层同时到点，
	// 会把还在收尾的调用直接砍掉，导致退出码与已产出内容丢失。
	//
	// Python: _CALL_TIMEOUT_GRACE_SECONDS = 30 (bash/_tool.py:57)
	callTimeoutGraceSeconds = 30
)

// ──────────────────────────── 非导出函数 ────────────────────────────

// bashCallTimeout 返回 Bash 工具的外层调用预算。
//
// 前台命令 = 钳制后的执行预算 + 清理宽限；后台命令立即返回 PID，不需要宽限。
//
// Python: BashTool.get_call_timeout (bash/_tool.py:151-168)
func bashCallTimeout(input *BashInput, defaultTimeout float64) float64 {
	if input == nil {
		return defaultTimeout
	}
	if input.RunInBackground {
		return defaultTimeout
	}
	// p.timeout 在上游是 _parse_inputs 归一后的值，等价于 resolveBashTimeout
	return float64(resolveBashTimeout(input.Timeout)) + callTimeoutGraceSeconds
}

// psCallTimeout 返回 PowerShell 工具的外层调用预算。
//
// Python: PowerShellTool.get_call_timeout (powershell/_tool.py:130-147)
func psCallTimeout(input *PowerShellInput, defaultTimeout float64) float64 {
	if input == nil {
		return defaultTimeout
	}
	if input.Background {
		return defaultTimeout
	}
	return float64(resolvePSTimeout(input.Timeout)) + callTimeoutGraceSeconds
}

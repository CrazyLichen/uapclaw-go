package shell

import "encoding/json"

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

// ──────────────────────────── 参数解码 ────────────────────────────

// parseBashInputFromMap 把原始参数 map 解码为 BashInput。
//
// 外层预算钩子拿到的就是调用方的原始参数，与执行时的解析走同一份结构体，
// 保证"加宽限用的 timeout"与"实际执行的 timeout"完全一致。
func parseBashInputFromMap(inputs map[string]any) BashInput {
	var in BashInput
	if len(inputs) == 0 {
		return in
	}
	if b, err := json.Marshal(inputs); err == nil {
		_ = json.Unmarshal(b, &in)
	}
	return in
}

// parseBashStreamInputFromMap 把原始参数 map 解码为 BashStreamInput。
func parseBashStreamInputFromMap(inputs map[string]any) BashStreamInput {
	var in BashStreamInput
	if len(inputs) == 0 {
		return in
	}
	if b, err := json.Marshal(inputs); err == nil {
		_ = json.Unmarshal(b, &in)
	}
	return in
}

// parsePowerShellInputFromMap 把原始参数 map 解码为 PowerShellInput。
func parsePowerShellInputFromMap(inputs map[string]any) PowerShellInput {
	var in PowerShellInput
	if len(inputs) == 0 {
		return in
	}
	if b, err := json.Marshal(inputs); err == nil {
		_ = json.Unmarshal(b, &in)
	}
	return in
}

// parsePowerShellStreamInputFromMap 把原始参数 map 解码为 PowerShellStreamInput。
func parsePowerShellStreamInputFromMap(inputs map[string]any) PowerShellStreamInput {
	var in PowerShellStreamInput
	if len(inputs) == 0 {
		return in
	}
	if b, err := json.Marshal(inputs); err == nil {
		_ = json.Unmarshal(b, &in)
	}
	return in
}

// bashStreamCallTimeout 返回流式 Bash 工具的外层调用预算。
//
// 与 bashCallTimeout 同语义，只是入参为流式工具自己的输入结构体。
func bashStreamCallTimeout(input *BashStreamInput, defaultTimeout float64) float64 {
	// 流式工具不支持后台运行，恒按前台加宽限
	if input == nil {
		return defaultTimeout
	}
	return float64(resolveBashTimeout(input.Timeout)) + callTimeoutGraceSeconds
}

// psStreamCallTimeout 返回流式 PowerShell 工具的外层调用预算。
func psStreamCallTimeout(input *PowerShellStreamInput, defaultTimeout float64) float64 {
	if input == nil {
		return defaultTimeout
	}
	return float64(resolvePSTimeout(input.Timeout)) + callTimeoutGraceSeconds
}

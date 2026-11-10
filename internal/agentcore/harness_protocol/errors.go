package harness_protocol

import "errors"

// ──────────────────────────── 结构体 ────────────────────────────

// HarnessStateError 命令与 harness 当前状态不匹配。
// Python: errors.py:13-14 HarnessStateError(HarnessError)
type HarnessStateError struct {
	// Msg 错误文案
	Msg string
}

// UnsupportedHarnessCapabilityError 调用方请求了 harness 未声明的能力。
// Python: errors.py:17-18 UnsupportedHarnessCapabilityError(HarnessError)
type UnsupportedHarnessCapabilityError struct {
	// Msg 错误文案
	Msg string
}

// HarnessProtocolError 实现违反了协议不变量。
// Python: errors.py:21-22 HarnessProtocolError(HarnessError)
type HarnessProtocolError struct {
	// Msg 错误文案
	Msg string
}

// CheckpointConflictError checkpoint 写入过期或违反 compare-and-set。
// Python: errors.py:25-26 CheckpointConflictError(HarnessProtocolError)
type CheckpointConflictError struct {
	// Msg 错误文案
	Msg string
}

// ──────────────────────────── 全局变量 ────────────────────────────

// ErrHarness 协议边界基类错误的哨兵。
// Python: errors.py:9-10 HarnessError(RuntimeError)
// Go 用 errors.Is 哨兵 + 类型断言复刻 Python 的 isinstance 层次。
var ErrHarness = errors.New("harness protocol error")

// ErrHarnessProtocol 协议不变量违反的哨兵（ErrHarness 的下一层）。
var ErrHarnessProtocol = errors.New("harness protocol invariant violated")

// ──────────────────────────── 导出函数 ────────────────────────────

// Error 实现 error 接口。
func (e *HarnessStateError) Error() string { return e.Msg }

// Is 使 errors.Is 可把 HarnessStateError 归到 ErrHarness。
func (e *HarnessStateError) Is(target error) bool { return target == ErrHarness }

// Error 实现 error 接口。
func (e *UnsupportedHarnessCapabilityError) Error() string { return e.Msg }

// Is 使 errors.Is 可归到 ErrHarness。
func (e *UnsupportedHarnessCapabilityError) Is(target error) bool { return target == ErrHarness }

// Error 实现 error 接口。
func (e *HarnessProtocolError) Error() string { return e.Msg }

// Is 使 errors.Is 可同时归到 ErrHarnessProtocol 与 ErrHarness。
func (e *HarnessProtocolError) Is(target error) bool {
	return target == ErrHarnessProtocol || target == ErrHarness
}

// Error 实现 error 接口。
func (e *CheckpointConflictError) Error() string { return e.Msg }

// Is 使 errors.Is 可归到 ErrHarnessProtocol 与 ErrHarness。
func (e *CheckpointConflictError) Is(target error) bool {
	return target == ErrHarnessProtocol || target == ErrHarness
}

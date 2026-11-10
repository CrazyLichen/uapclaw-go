package harness_protocol

// ──────────────────────────── 枚举 ────────────────────────────

// HarnessState harness 实现共享的高层生命周期相位。
// PAUSING 是「暂停请求已被接受」到「抵达可恢复边界」之间的过渡相位。
// Python: state.py:9-20 HarnessState
type HarnessState string

const (
	// HarnessStateIdle 空闲（无在飞轮次）
	HarnessStateIdle HarnessState = "idle"
	// HarnessStateRunning 有轮次在执行
	HarnessStateRunning HarnessState = "running"
	// HarnessStatePausing 已接受暂停、尚未抵达可恢复边界
	HarnessStatePausing HarnessState = "pausing"
	// HarnessStatePaused 已暂停在可恢复边界
	HarnessStatePaused HarnessState = "paused"
	// HarnessStateTerminated 已终止（不可逆）
	HarnessStateTerminated HarnessState = "terminated"
)

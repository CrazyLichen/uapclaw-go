package runtime

import (
	sessionpkg "github.com/uapclaw/uapclaw-go/internal/swarm/runtime/session"
)

// 本文件对应上游两份字节相同的副本，合并为一份（spec DEC-13，登记为偏差 D-1）：
//   - gateway/runtime/session_input.py
//   - jiuwenswarm/runtime/session_input.py
//
// 校验函数本体在 session/session_input.go（leaf 包；server/adapter 投递链
// 需要直接调用而 runtime→server/runtime 成环，故函数与错误同居 session 包）。
// 此处仅保留类型与函数别名，保持 runtime 包对外 API 面与上游调用点一致。

// ──────────────────────────── 结构体 ────────────────────────────

// SessionInputMode 会话补充输入的意图（别名）。
type SessionInputMode = sessionpkg.SessionInputMode

const (
	// SessionInputModeSteer 转向（别名）
	SessionInputModeSteer = sessionpkg.SessionInputModeSteer
	// SessionInputModeFollowUp 追加（别名）
	SessionInputModeFollowUp = sessionpkg.SessionInputModeFollowUp
)

// SessionInputRejectedError 已知拒收：输入未到达 SDK 队列（别名）。
type SessionInputRejectedError = sessionpkg.SessionInputRejectedError

// SessionInputTargetError 补充输入不得变成另一个执行的工作（别名）。
type SessionInputTargetError = sessionpkg.SessionInputTargetError

// SessionInputQueueRequiredError 跨会话输入须等普通任务准入（别名）。
type SessionInputQueueRequiredError = sessionpkg.SessionInputQueueRequiredError

// ──────────────────────────── 导出函数 ────────────────────────────

var (
	// ResolveSessionInputMode 归一化 input_mode 与其遗留别名（别名转发）。
	ResolveSessionInputMode = sessionpkg.ResolveSessionInputMode
	// ResolveSessionInputModeChecked 同上但冲突报错（别名转发）。
	ResolveSessionInputModeChecked = sessionpkg.ResolveSessionInputModeChecked
	// ValidateSteerImageReferences steer 仅引用线格式校验（别名转发）。
	ValidateSteerImageReferences = sessionpkg.ValidateSteerImageReferences
	// ValidateSessionInput 补充输入整体形状校验（别名转发）。
	ValidateSessionInput = sessionpkg.ValidateSessionInput
)

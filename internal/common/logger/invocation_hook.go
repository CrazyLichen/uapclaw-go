package logger

import (
	"github.com/rs/zerolog"
)

// 调用日志捕获钩子：logger 写入口单点接线（Python 在 loguru/base/default 三 backend
// 入口调 capture_invocation_log，Go 于 zerolog hook 等价；回调注册避免 invocation→logger import 环）。

// invocationCaptureFn 捕获回调签名（由 invocation 包注册）
type invocationCaptureFn func(level string, logType string, message string, errStr string)

// invocationCapture 已注册回调（nil 表示未注册/未接线）
var invocationCapture invocationCaptureFn

// SetInvocationCapture 注册捕获回调（invocation 包 init 调用，仅一次）
func SetInvocationCapture(fn invocationCaptureFn) {
	invocationCapture = fn
}

// invocationComponentHook 组件级钩子：组件名在 createComponentLogger 构造时固定
// （zerolog Event API 不暴露已写字段，组件经闭包携带；errStr 恒空——Python exc_info
// 等价信息在结构化 .Err 字段中不可达，偏差登记于提交说明）。
type invocationComponentHook struct {
	// comp 组件名（log_type 等价）
	comp string
}

// Run 实现 zerolog.Hook：事件通过级别过滤后、写出前单点捕获
func (h invocationComponentHook) Run(e *zerolog.Event, level zerolog.Level, message string) {
	if invocationCapture == nil {
		return
	}
	invocationCapture(level.String(), h.comp, message, "")
}

package callback

import "context"

// ──────────────────────────── 结构体 ────────────────────────────

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// globalCallbackFramework 全局回调框架单例。
//
// Python: Runner.callback_framework（Runner 初始化时创建的全局单例）
var globalCallbackFramework = NewCallbackFramework()

// ──────────────────────────── 导出函数 ────────────────────────────

// GetCallbackFramework 获取全局回调框架单例。
func GetCallbackFramework() *CallbackFramework {
	return globalCallbackFramework
}

// Trigger 便捷触发函数，触发自定义事件。
//
// Python: openjiuwen/core/runner/callback/utils.py trigger(event, **kwargs)
func Trigger(ctx context.Context, event string, data map[string]any) []any {
	return globalCallbackFramework.TriggerCustom(ctx, event, data)
}

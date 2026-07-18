package callback

import "context"

// globalCallbackFramework 全局回调框架单例。
//
// 对应 Python: Runner.callback_framework（Runner 初始化时创建的全局单例）
// ──────────────────────────── 结构体 ────────────────────────────

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

var globalCallbackFramework = NewCallbackFramework()

// GetCallbackFramework 返回全局回调框架单例。
//
// 对应 Python: openjiuwen/core/runner/callback/utils.py get_callback_framework()
// ──────────────────────────── 导出函数 ────────────────────────────

func GetCallbackFramework() *CallbackFramework {
	return globalCallbackFramework
}

// Trigger 便捷触发函数，触发自定义事件。
//
// 对应 Python: openjiuwen/core/runner/callback/utils.py trigger(event, **kwargs)
func Trigger(ctx context.Context, event string, data map[string]any) []any {
	return globalCallbackFramework.TriggerCustom(ctx, event, data)
}

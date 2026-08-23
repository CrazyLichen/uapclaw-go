package schema

import "encoding/json"

// ──────────────────────────── 结构体 ────────────────────────────

// HookEventBase 钩子事件基类，提供作用域和事件名构建功能。
//
// 对应 Python: jiuwenswarm/common/schema/event_base.py (HookEventBase)
type HookEventBase struct {
	// Scope 事件作用域，默认为 DefaultScope ("_framework")
	Scope string `json:"scope"`
}

// ──────────────────────────── 枚举 ────────────────────────────
// ──────────────────────────── 常量 ────────────────────────────

const (
	// DefaultScope 默认事件作用域，对齐 Python DEFAULT_SCOPE
	DefaultScope = "_framework"
)

// ──────────────────────────── 全局变量 ────────────────────────────
// ──────────────────────────── 导出函数 ────────────────────────────

// GetEvent 构建带作用域的事件名称。
func (h *HookEventBase) GetEvent(eventName string) string {
	return BuildEventName(h.Scope, eventName)
}

// BuildEventName 构建带作用域的事件名称（格式: scope:eventName）。
func BuildEventName(scope, eventName string) string {
	return scope + ":" + eventName
}

// ParseEventName 解析带作用域的事件名称，返回 (scope, eventName)。
// 无冒号时 scope 回退到 DefaultScope。
func ParseEventName(scopedEvent string) (scope, eventName string) {
	for i := 0; i < len(scopedEvent); i++ {
		if scopedEvent[i] == ':' {
			return scopedEvent[:i], scopedEvent[i+1:]
		}
	}
	return DefaultScope, scopedEvent
}

// NewHookEventBase 创建 HookEventBase 实例，使用默认作用域。
func NewHookEventBase() *HookEventBase {
	return &HookEventBase{Scope: DefaultScope}
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// marshalHookEventBase 序列化 HookEventBase 为 JSON。
func marshalHookEventBase(h *HookEventBase) ([]byte, error) {
	return json.Marshal(h)
}

// unmarshalHookEventBase 从 JSON 反序列化 HookEventBase。
func unmarshalHookEventBase(data []byte) (*HookEventBase, error) {
	var h HookEventBase
	if err := json.Unmarshal(data, &h); err != nil {
		return nil, err
	}
	return &h, nil
}

package schema

import "encoding/json"

// ──────────────────────────── 结构体 ────────────────────────────

// HookEventBase 带 scope 的钩子事件名基类（与 openjiuwen 0.1.9 EventBase 行为一致）。
//
// 子类通过组合嵌入 HookEventBase 并调用 GetEvent 构建带作用域前缀的事件名。
// Python 中通过 __init_subclass__ 元编程自动替换 scope 前缀，
// Go 中通过构造函数显式初始化实现等价效果。
//
// 对应 Python: jiuwenswarm/common/schema/event_base.py (HookEventBase)
type HookEventBase struct {
	// Scope 事件作用域，默认为 DefaultScope ("_framework")
	Scope string `json:"scope"`
}

// GetEvent 用当前 scope 构建完整事件名。
//
// 返回格式为 "scope:eventName"，对齐 Python HookEventBase.get_event。
func (h *HookEventBase) GetEvent(eventName string) string {
	return BuildEventName(h.Scope, eventName)
}

// ──────────────────────────── 常量 ────────────────────────────

const (
	// DefaultScope 默认事件作用域，对齐 Python DEFAULT_SCOPE
	DefaultScope = "_framework"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// BuildEventName 拼接作用域事件名。
//
// 返回格式为 "scope:eventName"，对齐 Python build_event_name。
func BuildEventName(scope, eventName string) string {
	return scope + ":" + eventName
}

// ParseEventName 解析作用域事件名。
//
// 按第一个冒号拆分为 (scope, eventName)；无冒号时 scope 回退为 DefaultScope。
// 对齐 Python parse_event_name。
func ParseEventName(scopedEvent string) (scope, eventName string) {
	for i := 0; i < len(scopedEvent); i++ {
		if scopedEvent[i] == ':' {
			return scopedEvent[:i], scopedEvent[i+1:]
		}
	}
	return DefaultScope, scopedEvent
}

// NewHookEventBase 创建 HookEventBase 实例，Scope 默认为 DefaultScope。
func NewHookEventBase() *HookEventBase {
	return &HookEventBase{Scope: DefaultScope}
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// marshalHookEventBase 序列化 HookEventBase 为 JSON 字节。
// 用于测试中验证 JSON 往返一致性。
func marshalHookEventBase(h *HookEventBase) ([]byte, error) {
	return json.Marshal(h)
}

// unmarshalHookEventBase 从 JSON 字节反序列化 HookEventBase。
// 用于测试中验证 JSON 往返一致性。
func unmarshalHookEventBase(data []byte) (*HookEventBase, error) {
	var h HookEventBase
	if err := json.Unmarshal(data, &h); err != nil {
		return nil, err
	}
	return &h, nil
}

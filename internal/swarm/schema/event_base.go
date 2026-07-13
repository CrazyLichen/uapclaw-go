package schema

import "encoding/json"

// ──────────────────────────── 结构体 ────────────────────────────

type HookEventBase struct {
	// Scope 事件作用域，默认为 DefaultScope ("_framework")
	Scope string `json:"scope"`
}

// ──────────────────────────── 常量 ────────────────────────────

const (
	// DefaultScope 默认事件作用域，对齐 Python DEFAULT_SCOPE
	DefaultScope = "_framework"
)

// ──────────────────────────── 导出函数 ────────────────────────────

func (h *HookEventBase) GetEvent(eventName string) string {
	return BuildEventName(h.Scope, eventName)
}

func BuildEventName(scope, eventName string) string {
	return scope + ":" + eventName
}

func ParseEventName(scopedEvent string) (scope, eventName string) {
	for i := 0; i < len(scopedEvent); i++ {
		if scopedEvent[i] == ':' {
			return scopedEvent[:i], scopedEvent[i+1:]
		}
	}
	return DefaultScope, scopedEvent
}

func NewHookEventBase() *HookEventBase {
	return &HookEventBase{Scope: DefaultScope}
}

// ──────────────────────────── 非导出函数 ────────────────────────────

func marshalHookEventBase(h *HookEventBase) ([]byte, error) {
	return json.Marshal(h)
}

func unmarshalHookEventBase(data []byte) (*HookEventBase, error) {
	var h HookEventBase
	if err := json.Unmarshal(data, &h); err != nil {
		return nil, err
	}
	return &h, nil
}

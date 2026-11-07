package iface

// ──────────────────────────── 结构体 ────────────────────────────

// WindowMutator 最终窗口投影函数。
// 约定：
//   - 只作用于本次请求，不得修改入参窗口（需要变更时返回新窗口）
//   - 返回 nil 表示放弃投影，调用方保留原窗口
//
// 对齐上游：get_context_window(window_mutators=[...])。
type WindowMutator func(*ContextWindow) *ContextWindow

// ──────────────────────────── 常量 ────────────────────────────

// WindowMutatorsExtraKey Option.Extra 中存放投影链的键
const WindowMutatorsExtraKey = "window_mutators"

// ──────────────────────────── 导出函数 ────────────────────────────

// WithWindowMutators 追加窗口投影链（按传入顺序执行）。
func WithWindowMutators(mutators ...WindowMutator) Option {
	return func(o *ProcessorOption) {
		if o.Extra == nil {
			o.Extra = map[string]any{}
		}
		existing, _ := o.Extra[WindowMutatorsExtraKey].([]WindowMutator)
		merged := make([]WindowMutator, 0, len(existing)+len(mutators))
		merged = append(merged, existing...)
		merged = append(merged, mutators...)
		o.Extra[WindowMutatorsExtraKey] = merged
	}
}

// WindowMutatorsFromOption 从选项列表取出投影链。
func WindowMutatorsFromOption(opts ...Option) []WindowMutator {
	po := NewProcessorOption(opts...)
	if po.Extra == nil {
		return nil
	}
	mutators, _ := po.Extra[WindowMutatorsExtraKey].([]WindowMutator)
	return mutators
}

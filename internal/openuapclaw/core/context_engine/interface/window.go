package iface

// ──────────────────────────── 结构体 ────────────────────────────

// WindowMutator 最终窗口投影函数接口。
// 约定：
//   - 只作用于本次请求，不得修改入参窗口（需要变更时返回新窗口）
//   - 返回 nil 表示放弃投影，调用方保留原窗口
//
// 对齐上游：get_context_window(window_mutators=[...])。
// 具名标记类型（如多模态图片投影）以接口断言识别——对应 Python 在 mutator
// 闭包上挂 attr 标记（_MULTIMODAL_IMAGE_WINDOW_MUTATOR_ATTR）的替换式安装。
type WindowMutator interface {
	// MutateWindow 应用投影；返回 nil 表示放弃。
	MutateWindow(*ContextWindow) *ContextWindow
}

// WindowMutatorFunc 函数适配器（普通闭包投影用）。
type WindowMutatorFunc func(*ContextWindow) *ContextWindow

// MutateWindow 实现 WindowMutator。
func (f WindowMutatorFunc) MutateWindow(w *ContextWindow) *ContextWindow { return f(w) }

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

// WindowMutatorsRegistrar 可选能力：持久窗口投影链的读改载体。
// Python: context._window_mutators（context.py:82）——持久列表在每次取窗口时
// 与 call 级参数合并应用（context.py:737）。SessionModelContext 实现本接口；
// 测试用 mock可不实现（此时调用方按能力断言跳过）。
type WindowMutatorsRegistrar interface {
	// WindowMutators 返回持久投影链快照（调用方可安全遍历，不持锁）。
	WindowMutators() []WindowMutator
	// SetWindowMutators 整表替换持久投影链（实现方需自持锁）。
	SetWindowMutators([]WindowMutator)
}

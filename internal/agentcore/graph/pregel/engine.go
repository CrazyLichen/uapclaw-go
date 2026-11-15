package pregel

import "fmt"

// 对齐 Python openjiuwen/core/graph/pregel/engine.py（285 行）：
// PregelLoop 单次图执行的超步循环 + Pregel 可执行图对象 + Run 顶层入口。
// 本文件随实施逐步补齐（Task 2 先落 RecursionError；Task 5/6/7 落循环与引擎）。

// ──────────────────────────── 结构体 ────────────────────────────

// RecursionError 递归上限超限（Python 内置 RecursionError 的 Go 等价物，spec D3）。
// engine.py:85 按类型名分类上抛（"RecursionError is caught by name at the workflow
// layer"），Go 以 errors.As 类型断言还原该契约。
type RecursionError struct {
	// Limit 递归上限（max_step）
	Limit int
	// Step 触发时的步号
	Step int
}

// ──────────────────────────── 导出函数 ────────────────────────────

// Error 文案复刻 engine.py:139-141
func (e *RecursionError) Error() string {
	return fmt.Sprintf("Recursion limit of %d reached at step %d", e.Limit, e.Step)
}

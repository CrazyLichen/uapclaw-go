package embedding

import "sync"

// ──────────────────────────── 结构体 ────────────────────────────

// NoopCallback 空回调，不做任何操作。
//
// 对齐 Python: BaseCallback（空操作基类）
type NoopCallback struct {
	// callCounter 调用计数
	callCounter int
	// mu 保护 callCounter
	mu sync.Mutex
}

// ──────────────────────────── 导出函数 ────────────────────────────

// NewNoopCallback 创建空回调实例。
func NewNoopCallback() *NoopCallback {
	return &NoopCallback{}
}

// OnBatchComplete 一批嵌入完成时回调，仅递增计数器。
func (c *NoopCallback) OnBatchComplete(_, _ int, _ []string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.callCounter++
}

// CallCounter 返回回调被调用的次数。
func (c *NoopCallback) CallCounter() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.callCounter
}

package common

import (
	"sync"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/store/embedding"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 类型别名 ────────────────────────────

// DocIndexCallback 文档索引进度回调接口。
//
// 用于追踪嵌入批次的处理进度。对齐 Python BaseCallback。
// 类型别名指向 foundation 层 embedding.Callback，保持编译期类型安全。
//
// Python: retrieval/common/callbacks.py (BaseCallback)
type DocIndexCallback = embedding.Callback

// ──────────────────────────── 结构体 ────────────────────────────

// NoOpDocIndexCallback 空操作回调，默认使用。
//
// 对齐 Python BaseCallback 的空操作语义（仅计数器+1，无进度条）。
// 实现 embedding.Callback 接口。
type NoOpDocIndexCallback struct {
	// callCounter 调用计数器，对齐 Python BaseCallback._call_counter
	callCounter int
	// mu 线程锁，对齐 Python BaseCallback._thread_lock
	mu sync.Mutex
}

// LoggingDocIndexCallback 日志进度回调。
//
// 对齐 Python TqdmCallback 的进度追踪语义。
// 每 100 批或最后一批打 Info 日志，对齐 Python ChromaVectorStore.add 中
// `if processed % 100 == 0: logger.info("Written %d/%d records")` 的逻辑。
type LoggingDocIndexCallback struct {
	// total 总批次数
	total int
	// processed 已处理批次数
	processed int
	// mu 线程锁
	mu sync.Mutex
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// NewLoggingDocIndexCallback 创建日志进度回调。
func NewLoggingDocIndexCallback(total int) *LoggingDocIndexCallback {
	return &LoggingDocIndexCallback{total: total}
}

// OnBatchComplete 实现 embedding.Callback 接口 — NoOpDocIndexCallback。
func (c *NoOpDocIndexCallback) OnBatchComplete(_ int, _ int, _ []string) {
	c.mu.Lock()
	c.callCounter++
	c.mu.Unlock()
}

// CallCounter 返回累计调用次数，对齐 Python BaseCallback.call_counter。
func (c *NoOpDocIndexCallback) CallCounter() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.callCounter
}

// OnBatchComplete 实现 embedding.Callback 接口 — LoggingDocIndexCallback。
func (c *LoggingDocIndexCallback) OnBatchComplete(startIdx, endIdx int, _ []string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.processed++
	// 对齐 Python: if processed % 100 == 0: logger.info("Written %d/%d records to %s")
	if c.processed%100 == 0 || c.processed >= c.total {
		logger.Info(logComponent).
			Int("processed", endIdx).
			Int("total", c.total).
			Msg("嵌入进度")
	}
}

// ──────────────────────────── 非导出函数 ────────────────────────────

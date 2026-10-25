package common

import (
	"sync"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/store/embedding"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

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
//
// M-18: total/processed 统一为文档数口径（对齐 Python chroma_store.py L150
// `total = len(data)` 与 L155 `processed += len(nodes)`）——
// processed 用 endIdx（全局累计文档索引，顺序批处理下与 Python 累计数等值），
// total 必须存文档总数而非批次数，否则日志出现 "processed=990/total=10" 的单位错配。
type LoggingDocIndexCallback struct {
	// total 总文档数（M-18: 文档数口径，非批次数）
	total int
	// processed 已处理批次数（用于 %100 触发节奏）
	processed int
	// mu 线程锁
	mu sync.Mutex
}

// ──────────────────────────── 枚举 ────────────────────────────

// DocIndexCallback 文档索引进度回调接口。
//
// 用于追踪嵌入批次的处理进度。对齐 Python BaseCallback。
// 类型别名指向 foundation 层 embedding.Callback，保持编译期类型安全。
// 按规范"类型别名归类到枚举区块"，置于枚举区块。
//
// Python: retrieval/common/callbacks.py (BaseCallback)
type DocIndexCallback = embedding.Callback

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// NewLoggingDocIndexCallback 创建日志进度回调。
// M-18: total 参数语义为总文档数（对齐 Python total = len(data)），
// 触发节奏仍按批次数 %100 计算（对齐 Python processed % 100 == 0 的
// 原始节奏——Python 的 processed 是文档数，但 %100 节奏语义不变）。
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
// M-18: 日志口径对齐 Python "Written %d/%d records"——
// processed 字段输出 endIdx（全局累计文档索引，即已写文档数），
// total 字段输出文档总数，两者单位一致。
func (c *LoggingDocIndexCallback) OnBatchComplete(startIdx, endIdx int, _ []string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.processed++
	// 对齐 Python: if processed % 100 == 0: logger.info("Written %d/%d records to %s")
	// 最后一批（endIdx >= total）也打，对齐 Python 尾批后的完成日志可观测性
	if c.processed%100 == 0 || endIdx >= c.total {
		logger.Info(logComponent).
			Int("processed", endIdx).
			Int("total", c.total).
			Msg("embedding progress")
	}
}

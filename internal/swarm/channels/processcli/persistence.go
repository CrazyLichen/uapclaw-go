package processcli

import (
	"fmt"
	"math"
	"sync"

	"github.com/uapclaw/uapclaw-go/internal/swarm/server/session"
)

// ──────────────────────────── 结构体 ────────────────────────────

// flushBarrier 一个待等待的写屏障。
type flushBarrier struct {
	// name 屏障名（用于错误消息）
	name string
	// flush 在预算内排空队列，返回是否完成
	flush func(timeoutSeconds float64) bool
}

// ──────────────────────────── 导出函数 ────────────────────────────

// FlushSessionWrites 在一个关闭窗口内等两个独立写屏障。
// Python: persistence.py:10-25 flush_session_writes
//
// 两个屏障（history / metadata）各自有独立队列，必须都排空才算落盘完成。
// 预算是总超时扣掉余量：给「收集结果 + 组装错误」留空间，
// 否则整步卡满后连报错都来不及。
func FlushSessionWrites(timeoutSeconds float64) error {
	// Python: :14-17 queues = (("history", ...), ("metadata", ...))
	queues := []flushBarrier{
		{name: "history", flush: session.FlushHistoryQueue},
		{name: "metadata", flush: session.FlushMetadataQueue},
	}
	// Python: :18 flush_timeout = timeout_seconds - min(0.5, timeout_seconds / 10)
	budget := flushBudget(timeoutSeconds)

	// Python: :19-22 asyncio.gather(*(to_thread(flush, flush_timeout) ...))
	// 两个屏障并发执行，互不等待
	results := make([]bool, len(queues))
	var wg sync.WaitGroup
	for i := range queues {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			results[index] = queues[index].flush(budget)
		}(i)
	}
	wg.Wait()

	// Python: :23 failed = [name for (name, _), result in zip(...) if result is not True]
	failed := make([]string, 0, len(queues))
	for i, barrier := range queues {
		if !results[i] {
			failed = append(failed, barrier.name)
		}
	}
	// Python: :24-25 if failed: raise RuntimeError(f"{', '.join(failed)} writes did not finish before shutdown")
	if len(failed) > 0 {
		joined := ""
		for i, name := range failed {
			if i > 0 {
				joined += ", "
			}
			joined += name
		}
		return fmt.Errorf("%s writes did not finish before shutdown", joined)
	}
	return nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// flushBudget 从总预算里扣出余量。
// Python: persistence.py:18 timeout_seconds - min(0.5, timeout_seconds / 10)
func flushBudget(timeoutSeconds float64) float64 {
	return timeoutSeconds - math.Min(0.5, timeoutSeconds/10)
}

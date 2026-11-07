package utils

import (
	"time"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// DrainWithTimeout 排空通道，最多等待 timeout。
// 返回已排空的元素数与是否在超时前排空完成。典型用法是流式响应的异常路径：
// 消费方中止后必须把内层通道读干净，让生产方 goroutine 走到自己的 defer
// （关闭 resp.Body、释放连接）；但若某个客户端收到 cancel 后既不关闭通道、
// 也不退出发送循环，裸 `for range` 会永久挂住，因此必须有超时兜底。
func DrainWithTimeout[T any](ch <-chan T, timeout time.Duration) (int, bool) {
	drained := 0
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	for {
		select {
		case _, ok := <-ch:
			if !ok {
				return drained, true
			}
			drained++
		case <-timer.C:
			return drained, false
		}
	}
}

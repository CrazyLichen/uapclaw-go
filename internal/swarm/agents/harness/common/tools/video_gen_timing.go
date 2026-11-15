package tools

import "time"

// 视频生成轮询/盯单时序的测试注入点。
// 生产值由 video_gen_tools.go 的包级变量初始化（pollInterval=10s 等）；
// 集成测试经本文件导出的 setter 注入短时序，避免真实等待 120s/30min。
// 仅测试装配层使用；生产代码不得调用。

// SetVideoGenPollTiming 注入提交轮询间隔与预算（秒）。
func SetVideoGenPollTiming(interval time.Duration, budgetSeconds int) {
	pollInterval = interval
	pollBudget = budgetSeconds
}

// SetVideoGenWatchTiming 注入盯单间隔与上限。
func SetVideoGenWatchTiming(interval, max time.Duration) {
	watchInterval = interval
	watchMax = max
}

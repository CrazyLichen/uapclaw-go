package acp

import "time"

// ──────────────────────────── 结构体 ────────────────────────────

// IdleTimer 空闲收尾定时器槽。
//
// 为什么要「槽」而不是直接用 *time.Timer：AfterFunc 的回调可能在闭包变量赋值
// 之前就已启动，回调里读闭包 timer 会拿到 nil 或旧值（§9-4 竞态）。改为回调
// 接收槽本身、与 ctx 上保存的槽做引用比对，即可判定该 timer 是否已被后续调度
// superseded。gateway 桥与 stdio 通道共用本实现，避免同一套比对逻辑写两遍。
type IdleTimer struct {
	// timer 已 arm 的定时器（创建后只读）
	timer *time.Timer
}

// ──────────────────────────── 导出函数 ────────────────────────────

// NewIdleTimer 创建槽并 arm 定时器；fn 收到槽本身用于引用比对。
func NewIdleTimer(d time.Duration, fn func(*IdleTimer)) *IdleTimer {
	slot := &IdleTimer{}
	slot.timer = time.AfterFunc(d, func() {
		if fn != nil {
			fn(slot)
		}
	})
	return slot
}

// Stop 停止定时器。nil 安全（调用方无需判空）、可重复调用。
func (t *IdleTimer) Stop() {
	if t == nil || t.timer == nil {
		return
	}
	t.timer.Stop()
}

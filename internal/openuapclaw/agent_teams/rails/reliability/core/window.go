// Package core 团队可靠性数据层：滑动窗口计数与稳定调用哈希。
//
// 对应 Python：openjiuwen/agent_teams/reliability/window.py
package core

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"

	"github.com/uapclaw/uapclaw-go/internal/common/utils"
)

// ──────────────────────────── 结构体 ────────────────────────────

// SlidingWindowCounter 尾随时间窗口内的事件计数。
//
// Python: window.py SlidingWindowCounter。保留事件时间戳并逐操作驱逐早于
// window_seconds 的旧事件；调用方显式传当前时间戳，计数器不持内部时钟
// （确定性、可测）。Python deque 头部驱逐 → Go 切片 + 头部裁剪等价。
type SlidingWindowCounter struct {
	// windowSeconds 尾随窗口宽度（秒）
	windowSeconds float64
	// events 窗口内事件时间戳（升序）
	events []float64
}

// ──────────────────────────── 导出函数 ────────────────────────────

// NewSlidingWindowCounter 创建窗口计数器。
//
// Python: window.py SlidingWindowCounter.__init__。
func NewSlidingWindowCounter(windowSeconds float64) *SlidingWindowCounter {
	return &SlidingWindowCounter{windowSeconds: windowSeconds}
}

// Add 在 ts 时刻记录一个事件并返回窗口内计数（含本次）。
//
// Python: window.py SlidingWindowCounter.add。
func (w *SlidingWindowCounter) Add(ts float64) int {
	w.events = append(w.events, ts)
	w.evict(ts)
	return len(w.events)
}

// Count 返回截至 ts（不记录）的窗口内计数。
//
// Python: window.py SlidingWindowCounter.count。
func (w *SlidingWindowCounter) Count(ts float64) int {
	w.evict(ts)
	return len(w.events)
}

// Reset 丢弃全部已记录事件。
//
// Python: window.py SlidingWindowCounter.reset。
func (w *SlidingWindowCounter) Reset() {
	w.events = w.events[:0]
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// evict 相对 ts 驱逐早于尾随窗口的事件。
//
// Python: window.py SlidingWindowCounter._evict。
func (w *SlidingWindowCounter) evict(ts float64) {
	cutoff := ts - w.windowSeconds
	drop := 0
	for drop < len(w.events) && w.events[drop] < cutoff {
		drop++
	}
	w.events = w.events[drop:]
}

// StableCallHash 计算工具调用的顺序无关稳定哈希。
//
// Python: window.py stable_call_hash。{"a":1,"b":2} 与 {"b":2,"a":1} 同哈希，
// 防止重复检测漏掉乱序参数的调用；json 对 map 递归按键排序（Python
// json.dumps(sort_keys=True) 递归语义与 Go encoding/json 一致），非可序列化值
// 以 %v 兜底对齐 default=str（不报错）。哈希输入直接序列化本体（json tag
// 生效），无独立 marshal 包装层。
func StableCallHash(toolName string, toolArgs map[string]any) string {
	if toolArgs == nil {
		toolArgs = map[string]any{}
	}
	payload := marshalSorted(map[string]any{"tool": toolName, "args": toolArgs})
	return sha256Hex(payload)
}

// StableResultHash 把工具结果哈希成循环检测用的稳定摘要。
//
// Python: window.py stable_result_hash。区分「同调用同结果」（真循环）与
// 「同调用不同结果」（正当迭代）。分支优先序逐一对齐：nil→"none"、string
// 原文、其余 json 序列化（sort_keys + ensure_ascii=False + 默认分隔符，
// 失败→str 兜底）。
func StableResultHash(result any) string {
	if result == nil {
		return "none"
	}
	if s, ok := result.(string); ok {
		return sha256Hex([]byte(s))
	}
	payload := marshalSorted(result)
	return sha256Hex(payload)
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// marshalSorted 序列化为 UTF-8 JSON，字节级对齐 Python
// json.dumps(v, sort_keys=True, ensure_ascii=False)：
//   - map 键递归有序（Go encoding/json 对 map 天然按键排序）
//   - 不转义 HTML 字符 + 默认分隔符 ", "/": "（utils.MarshalNoEscapeSpaced）
//
// 非可序列化值以 %v 兜底对齐 default=str（不报错）。哈希输入直接序列化
// 本体（json tag 生效），无独立 marshal 包装层。
func marshalSorted(v any) []byte {
	b, err := utils.MarshalNoEscapeSpaced(v)
	if err != nil {
		return []byte(fmt.Sprintf("%v", v))
	}
	return b
}

// sha256Hex 计算字节切片的 SHA-256 十六进制摘要。
func sha256Hex(payload []byte) string {
	sum := sha256.Sum256(payload)
	return hex.EncodeToString(sum[:])
}

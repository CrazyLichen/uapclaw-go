// Package demand 提供全局 trajectory span processor 单例面。
//
// 对齐 Python: openjiuwen/extensions/observability/demand.py:52-66。演进轨迹
// 采集与可观测性导出共用同一批 span，所有 runtime 必须挂同一个 processor
// 实例——第二个实例会重复每条轨迹。
// 独立子包规避 import cycle（trajectory 包引用 observability 的 semconv 常量；
// Python 以函数内局部 import 打破，Go 以子包等价，偏差 D-25.2b-10）。
package demand

import (
	"sync"

	"github.com/uapclaw/uapclaw-go/internal/openjiuwen/agent_evolving/trajectory"

	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

// ──────────────────────────── 全局变量 ────────────────────────────

var (
	// mu 保护 processor 槽位。Python: _DEMAND_LOCK 语义。
	mu sync.Mutex
	// trajectorySpanProcessor 进程级 processor 槽位（nil=未构造）。
	// Python: _TRAJECTORY_SPAN_PROCESSOR (demand.py:41)。
	trajectorySpanProcessor sdktrace.SpanProcessor
)

// ──────────────────────────── 导出函数 ────────────────────────────

// GetTrajectorySpanProcessor 返回进程级 trajectory span processor。
// 首次调用懒构造（Python: get_trajectory_span_processor demand.py:52-66）。
// Go 侧 max_pending 传 0 = 无上限（对齐 Python 构造默认 None）。
func GetTrajectorySpanProcessor() sdktrace.SpanProcessor {
	mu.Lock()
	defer mu.Unlock()
	if trajectorySpanProcessor != nil {
		return trajectorySpanProcessor
	}
	trajectorySpanProcessor = trajectory.NewTrajectorySpanProcessor(0)
	return trajectorySpanProcessor
}

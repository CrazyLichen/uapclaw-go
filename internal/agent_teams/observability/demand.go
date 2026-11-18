// Package observability - demand.go 提供全局 trajectory span processor 单例面。
//
// 对齐 Python: openjiuwen/extensions/observability/demand.py:41/:52-73。
// Python 侧没有 setter——`_TRAJECTORY_SPAN_PROCESSOR` 的唯一赋值点是
// `get_trajectory_span_processor()` 内部的懒构造分支（demand.py:71），
// 构造目标为 agent_evolving/trajectory/processor.py:302 的
// `TrajectorySpanProcessor`（OTel SpanProcessor 子类）。
// Go 侧该类型尚未移植：懒构造分支 ⤴️ evolving trajectory 章节落地时
// 直接补进 GetTrajectorySpanProcessor 内部，并删除本文件的包内测试 set
// helper（无注入缝残留）。
package observability

import (
	"sync"

	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

// ──────────────────────────── 全局变量 ────────────────────────────

var (
	// trajectorySpanProcessorMu 保护 trajectorySpanProcessor。
	// Python: _DEMAND_LOCK 语义（demand.py 模块级锁）。
	trajectorySpanProcessorMu sync.Mutex
	// trajectorySpanProcessor 进程级 trajectory span processor 槽位。
	// Python: _TRAJECTORY_SPAN_PROCESSOR (demand.py:41)，nil 表示未配置。
	trajectorySpanProcessor sdktrace.SpanProcessor
)

// ──────────────────────────── 导出函数 ────────────────────────────

// GetTrajectorySpanProcessor 返回进程级 trajectory span processor。
// 演进轨迹采集与可观测性导出共用同一批 span，所有 runtime 必须挂
// **同一个** processor 实例——第二个实例会重复每条轨迹。
// Python: get_trajectory_span_processor (demand.py:52-73)。
// Go 侧未配置返回 nil；懒构造分支（TrajectorySpanProcessor，Go 侧类型
// ⤴️evolving）落地时补进本函数内部。
func GetTrajectorySpanProcessor() sdktrace.SpanProcessor {
	trajectorySpanProcessorMu.Lock()
	defer trajectorySpanProcessorMu.Unlock()
	return trajectorySpanProcessor
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// setTrajectorySpanProcessorForTest 包内测试 helper（非导出，对齐 Python
// 无 setter 的对外形态）。evolving 章节补懒构造分支时随之删除。
func setTrajectorySpanProcessorForTest(p sdktrace.SpanProcessor) {
	trajectorySpanProcessorMu.Lock()
	defer trajectorySpanProcessorMu.Unlock()
	trajectorySpanProcessor = p
}

// resetTrajectorySpanProcessorForTest 清空槽位（仅测试用）。
func resetTrajectorySpanProcessorForTest() { setTrajectorySpanProcessorForTest(nil) }

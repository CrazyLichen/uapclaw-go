//go:build integration

// Package monitor 测试 TeamMonitor 和事件系统的集成行为。
//
// 覆盖：
//   - TeamMonitor 生命周期（Start/Stop/IsRunning）
//   - FromEventMessage 事件转换
//   - MonitorEvent 类型映射
//   - TeamStreamLogger Feed/Flush
//   - MonitorEventType 枚举完整性
//
// 对齐 Python: openjiuwen/harness/monitor/team_monitor.py
package monitor

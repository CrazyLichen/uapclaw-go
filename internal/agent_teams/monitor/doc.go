// Package monitor 提供团队监控能力：事件观察、状态查询和实时事件流。
//
// TeamMonitor 注册到 TeamAgent 的事件监听器上，将内部 EventMessage 转换为
// 面向前端的 MonitorEvent，并通过 channel 供消费者实时获取。
// 同时提供团队/成员/任务/消息的只读查询接口。
//
// 文件目录：
//
//	monitor/
//	├── doc.go              # 包文档
//	├── models.go           # MonitorEventType 枚举 + TeamInfo/MemberInfo/TaskInfo/MessageInfo/MonitorEvent 模型
//	├── team_monitor.go     # TeamMonitor 事件观察器 + 查询 API
//	└── stream_logger.go    # TeamStreamLogger 诊断日志
//
// 对应 Python 代码：openjiuwen/agent_teams/monitor/
package monitor

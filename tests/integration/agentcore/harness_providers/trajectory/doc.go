// Package trajectory 15.11 轨迹适配集成测试：fake provider 事件流驱动的 span 断言。
//
// 集成边界：harness_protocol（事件信封/终态/因果）×
// agentcore/harness（ExecutionSubject 执行主体身份）×
// agent_teams/observability（轨迹 v2 事件发射链与上下文窗口规范状态）
// 三子系统；OTLP 后端以 in-memory span exporter 承担（进程边界，
// 真实导出属领域 22.2）。
//
// 对应 Python 代码：harness_providers/trajectory.py 的宿主驱动面
// （member_runtime.py / reliability.py / external_cli_spawn.py 的
// create + record_input + observe 驱动顺序，host 接线 ⤵️15.13/15.16）。
package trajectory

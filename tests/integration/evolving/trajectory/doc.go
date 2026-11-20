// Package trajectory 25.2b 观测链集成用例：processor 观测链（spec §7 Task 13 Step 1）。
//
// 覆盖：demand 懒构造单例 + TracerProvider 注册生效 + 跨成员 team scope 可见 +
// agent scope 隔离 + identity 去重 + pending 满丢弃 + demand 并发同实例。
// 真实跨子系统：agent_teams/observability/demand + evolving/trajectory +
// agent_teams/tools/database（轨迹存储 Save/Load）。
package trajectory

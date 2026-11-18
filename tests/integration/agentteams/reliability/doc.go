// Package agentteams_test 团队可靠性子系统（25.1g）集成测试。
//
// 覆盖 agent_teams/rails/reliability 全链（rail 采集 → monitor 检测 →
// remediation 纠偏/上报）、agent_teams/messager（inprocess 总线）、
// agent_teams/agent/coordination/handlers（leader 侧路由）、
// agent_teams/schema/events（事件契约）与 agentcore/single_agent/interfaces
// （steering 队列）。
package agentteams_test

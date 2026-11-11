// Package permissions 提供权限链路的集成测试（仅含 _test.go）。
//
// 覆盖自动权限审查层与权限中断的跨子系统回归：
// 自动权限三层链路（rails → permissions → tool_discovery → security）
// 与权限中断三场景（9.90e Task 10）。
//
// 对应 Python 代码：jiuwenswarm/agents/harness/common/rails/permissions/
package permissions

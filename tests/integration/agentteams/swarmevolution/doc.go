//go:build integration

// Package swarmevolution 15.24d 演进链端到端集成套件（host 推送链 + 全流程）。
//
// 真实跨子系统链路（≥4）：swarm/server/adapter（chat.send 入口 + 推送链）→
// harness/rails/evolution（rail 栈 + commands prompt）→ agent_evolving/tools
// （六主工具 + 审查子 agent）→ agent_evolving/experience + checkpointing
// （落盘）→ agent_evolving/sharing（跨用户共享）。mock 边界只许 LLM 与共享 hub 网络端。
//
// 用例索引（spec docs/superpowers/specs/2026-12-26-15.24d-e2e-host-push-design.md §3）：
// E1 主动链 / E2 团队 leader review_feedback 被动链 / E3 团队 cycle 状态机 /
// E4 成员 auto_save+sharing / E5 follow_up / E6 slash 五命令 / E7 审批拒绝 /
// E8 响应恢复 / E9 跨用户下载 / E10 15.24c 装配闭环 / E11/E12 轨迹完整性。
//
// 对应 Python 代码：jiuwenswarm/server/runtime/agent_adapter/{evolution_slash,
// evolution_helpers,interface_deep,team_helpers}.py 系统行为。
package swarmevolution

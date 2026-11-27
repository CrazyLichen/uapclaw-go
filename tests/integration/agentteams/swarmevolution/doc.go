//go:build integration

// Package swarmevolution 15.24d 演进链端到端集成套件（host 推送链 + 全流程）。
//
// 真实跨子系统链路（≥4）：swarm/server/adapter（chat.send 入口 + 推送链）→
// harness/rails/evolution（rail 栈 + commands prompt）→ agent_evolving/tools
// （六主工具 + 审查子 agent）→ agent_evolving/experience + checkpointing
// （落盘）→ agent_evolving/sharing（跨用户共享）。mock 边界只许 LLM 与共享 hub 网络端。
//
// 用例索引（spec docs/superpowers/specs/2026-12-26-15.24d-e2e-host-push-design.md §3）：
// E1 主动链（含 E5 followup 注入直证 + E11 六环节标记回读⑤⑥）/ E2 团队 leader
// 被动链 / E3 团队全状态机（评审反馈→聚合→auto_save 落盘→多轮→不污染）/
// E4 成员 auto_save+sharing 审批上传 / E6 slash 五命令 / E7 审批拒绝 /
// E8 watcher 超时 hidden end + 重入 / E9 跨用户 SearchSkills 检索下载 /
// E10 15.24c 装配闭环+部分批准 / E12 团队轨迹聚合完整性。
// 并入说明：E5 并入 E1（followup 注入直证）；E11 六环节中①②④⑤⑥由 E1 覆盖，
// ③（审查子 agent prompt）由 agent_evolving/tools/evolve_review_task_test.go
// 单测承接（e2e 夹具中 review scope 以替身 CreateScope 承接）。
//
// 对应 Python 代码：jiuwenswarm/server/runtime/agent_adapter/{evolution_slash,
// evolution_helpers,interface_deep,team_helpers}.py 系统行为。
package swarmevolution

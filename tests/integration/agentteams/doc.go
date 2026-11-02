// Package agentteams_test 覆盖团队 Agent 的中断（HITL）恢复契约集成测试。
//
// 对齐 Python openjiuwen/agent_teams/harness.py 的 has_pending_interrupt /
// is_pending_interrupt_resume_valid：中断状态由 single_agent 的 ToolInterruptHandler
// 写入会话，TeamHarness 再按同一个 key 与同一个状态类型读出来。
//
// 本目录只验证"跨子系统契约"（写入方与读取方的键/类型/语义必须一致），
// 私有状态（activeAgentSession 的回退路径）由
// internal/agent_teams/harness_interrupt_internal_test.go 覆盖。
//
// 文件目录：
//
//	agentteams/
//	├── doc.go                   # 包文档
//	└── interrupt_resume_test.go # TeamInterruptResumeSuite：Save/Load 往返、key 契约、Rail 转调
//
// 对应 Python 代码：openjiuwen/agent_teams/harness.py（中断恢复）
package agentteams_test

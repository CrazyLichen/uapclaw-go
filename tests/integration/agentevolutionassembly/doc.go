//go:build integration

// Package agentevolutionassembly 15.24c 审查子 agent 与 swarm 装配集成套件。
// 链路：evolution（copy-on-write/scope）+ reviewsubagent+tools（子 agent 装配）
// + checkpointing/experience（ForKind 透传）+ core/session（facade 扩面）。
//
// 对应 Python 代码：openjiuwen/harness/rails/evolution/ + agent_evolving/
package agentevolutionassembly

// Package interruptresume 中断恢复全链路集成测试（server build 层 → interaction → 中断 handler 内核）。
//
// 跨子系统（满足审计 ≥3）：swarm/server/runtime（answers → InteractiveInput 构建）+
// core/session/interaction（InteractiveInput.Answer/Update）+
// core/single_agent/interrupt（BuildInterruptState / HandleResume / BuildInterruptResult）+
// core/single_agent/schema（ToolInterruptionState / ToolInterruptEntry）。
//
// 对齐 Python：jiuwenswarm/server/runtime/agent_adapter/interface.py
// _build_interactive_input_from_answers + openjiuwen/core/single_agent/interrupt/handler.py
// handle_resume / _handle_tool_interrupt_exception / _merge_held_calls。
//
// 文件目录：
//
//	interruptresume/
//	└── full_chain_test.go   # 全链套件（build 层各 source 分支 → 恢复链 held/paused_call/答案归属）
package interruptresume

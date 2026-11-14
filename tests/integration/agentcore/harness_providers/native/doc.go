// Package native 提供领域十五 15.6 native provider（DeepAgentHarness）的端到端集成测试。
//
// 测试驱动一个真 DeepAgent（真 rails / 中断处理器 / 恢复路径）+ 脚本化模型，
// 经 DeepAgentHarness.start / send / turn_events 走完整 turn → 中断 → 宿主应答 → resume 链路。
//
// 对应 Python: tests/unit_tests/harness_providers/test_native_interrupt_kinds.py +
// tests/unit_tests/fixtures/deep_agent_interrupts.py + tests/system_tests/harness_providers/test_native_e2e.py
package native

// Package harness_test 覆盖 NativeHarness 生命周期契约集成测试。
//
// 对齐 Python openjiuwen/agent_teams/harness/native_harness.py 的
// start/send/outputs/subscribe/abort 生命周期：单 supervisor 串行化所有输入，
// 状态在 IDLE/RUNNING/TERMINATED 间迁移，输出经单消费者迭代器泵出。
//
// 集成边界：NativeHarness（agent_teams/harness）→ DeepAgent 任务循环
// （agentcore/harness）→ ReAct 执行器 + MockLLM → Session 流。跨 ≥3 个
// internal 二级子系统。
//
// 文件目录：
//
//	harness/
//	├── doc.go                 # 包文档
//	├── native_harness_test.go # NativeHarnessSuite：生命周期契约
//	├── team_harness_test.go   # TeamHarnessSuite：start/send/outputs 生命周期
//	├── spec_assembly_test.go  # SpecAssemblySuite：spec 驱动装配（I-01~I-05、I-08、I-10）
//	├── mid_round_announce_test.go # MidRoundAnnounceSuite：中途唤醒 + 门控删除（I-06/I-07）
//	└── tool_teardown_test.go  # ToolTeardownSuite：轮末工具摘除（I-09）
//
// 对应 Python 代码：openjiuwen/agent_teams/harness/native_harness.py
package harness_test

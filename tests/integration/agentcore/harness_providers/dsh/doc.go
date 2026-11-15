// Package dsh 15.9 dsh provider 集成测试：fake SDK 工厂驱动的完整周期。
//
// 集成边界：harness_providers 基座（SerializedTurnHarness 串行轮次循环）×
// harness_protocol（事件信封/游标/终态）× dsh 适配器（SDK 缝 + overlay +
// 映射）三子系统；SDK 进程边界以脚本化 fake 承担（真实 dsh runtime 依赖
// DEEPSEEK_API_KEY，上游 system e2e 同为 skipif 门卫）。
//
// 对应 Python 代码：tests/system_tests/harness_providers/test_dsh_e2e.py 的
// fake-SDK 等价投影。
package dsh

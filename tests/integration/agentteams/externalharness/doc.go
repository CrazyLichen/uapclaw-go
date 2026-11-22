//go:build integration

// Package externalharness_test 15.18 单会话 External Harness 会话接入集成测试。
//
// 跨子系统：swarm/server/adapter（helpers 全链）→ harness_providers（注册缝 fake
// provider + IOAdapter + CreateHarness/BuildHarnessContext）→ harness_protocol
// （checkpoint 卡片协议）→ swarm/server/session（会话元数据绑定/checkpoint 键）→
// swarm/server/runtime/equipment（15.18a 专家包解析）→ swarm/schema（请求/响应契约）。
//
// 文件目录：
//
//	externalharness/
//	└── binding_lifecycle_suite_test.go   # 绑定冲突/启动停止/checkpoint 往返套件
//
// 对应 Python 代码：jiuwenswarm/server/runtime/agent_adapter/external_harness_helpers.py
package externalharness

// Package team_runtime_test 提供 TeamRuntime 核心集成测试。
//
// 覆盖 TeamRuntime 生命周期、Agent 注册/注销、P2P Send、Pub-Sub 发布/订阅、
// SubscriptionManager 通配符匹配、MessageEnvelope 构建、CommunicableAgent 绑定通信、
// TeamCard/TeamConfig 构建、并发安全等场景。
//
// 对齐 Python: openjiuwen/core/multi_agent/team_runtime/ 单元测试
//
// 文件目录：
//
//	team_runtime/
//	├── doc.go                    # 包文档
//	└── team_runtime_e2e_test.go  # TeamRuntime 核心 E2E 测试
package team_runtime_test

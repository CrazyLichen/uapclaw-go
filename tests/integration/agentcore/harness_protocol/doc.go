//go:build integration

// Package harness_protocol 提供 15.3 交互与钩子控制面的集成测试。
//
// 被测对象是 internal/agentcore/harness_protocol 的控制面契约
// （HarnessInteractionHandler / HarnessHookDispatcher / ValidateInteractionResponse），
// 但它本身是纯契约包（go list -deps 零 internal 依赖），因此真链路必须由
// 真实子系统驱动：
//
//	internal/agent_teams/harness           L2 团队成员交互契约（HarnessState 消费者）
//	internal/agentcore/session             *session.Session 会话生命周期
//	internal/agentcore/session/interaction  *InteractiveInput 应答载体
//	internal/agentcore/harness_protocol    被测契约
//
// fake 只扮「外部 CLI provider」这一进程边界角色：provider 在独立进程里
// 通过 stdio 与宿主交换 15.3 的请求/应答（Python: harness_providers/base.py
// 的 _request_interaction 就是这么用的）。被测子系统（session / agent_teams/harness）
// 一律用真实实现，不 mock。
//
// 审计豁免：本目录登记在 .integration-scope-exempt 的 C 类（过渡豁免），
// 理由见该文件条目。
//
// 文件目录：
//
//	harness_protocol/
//	├── doc.go                            # 包文档
//	├── harness_fake_test.go              # 跨子系统驱动端：fake 外部 CLI provider
//	├── interaction_lifecycle_test.go     # 交互请求-应答全生命周期
//	└── hooks_boundary_test.go            # 钩子四边界 × 并发 × ctx 取消
package harness_protocol

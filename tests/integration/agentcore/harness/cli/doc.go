//go:build integration

// Package cli 提供 harness/cli（第三方 CLI 前端）集成测试的目录结构文档。
//
// 实际测试代码以 cli_test 包（外部测试包）编写。
//
// 测试范围：CLI 后端装配（agent/factory.go 的 CreateAgent → CreateDeepAgent
// 全量 rails）→ runner.Start → RunAgentStreaming → 流式帧回传 → REPL 主循环
// → 会话持久化。链路真实跨越 harness / runner / session / storage 四个
// internal 二级子系统；LLM 端点用本地 httptest 的 OpenAI 兼容 SSE 假服务。
//
// 对应 Python 代码：openjiuwen/harness/cli/
package cli

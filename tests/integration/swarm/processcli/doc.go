//go:build integration

// Package processcli_test 提供 uapclaw-process 命令行的集成测试。
//
// 全部用例走真实二进制：`go build ./cmd/uapclaw-process/` 后以子进程启动，
// 通过 stdin 喂真实输入、读取真实 stdout/stderr 与退出码。这样能覆盖到
// 「装配链是否完整」这类只有进程边界才暴露的问题——例如 P-7 的
// `agent runtime manager is nil`（Runtime 未注入 AgentManager）在单元
// 测试里被 fake 遮住，真跑二进制才报错。
//
// 覆盖五条 dispatch 分支与错误契约：
//   - 帮助 / 参数错误（退出码 0 / 2 + 中文错误文案）
//   - 会话查询（--list-sessions）
//   - --run-json（JSON 输入 → event + result 两行 JSON，退出码随 result）
//   - --query-json（query_result 契约）
//   - 非法输入（INVALID_INPUT + 字段级 details）
//
// 文件目录：
//
//	processcli/
//	├── doc.go              # 包文档
//	└── cli_e2e_test.go     # 真实二进制 + 真实输入输出的端到端用例
//
// 对应 Python 代码：jiuwenswarm/channels/process_cli/main.py（argparse + 五分支分派）
// 与 tests/unit_tests/process_cli/、tests/system_tests/。
package processcli

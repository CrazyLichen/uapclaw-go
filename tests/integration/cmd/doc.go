//go:build integration

// Package cmd_test 提供 CLI 命令的集成测试。
//
// 覆盖两类入口：①ACP 子进程（真实二进制 + 真实 stdio 管道，验证首帧探测、
// Prompt 往返、错误终帧、stdin EOF 退出与远端不可达）；②app 启动命令。
//
// 文件目录：
//
//	cmd/
//	├── doc.go             # 包文档
//	├── acp_stdio_test.go  # ACP 子进程 stdio 协议用例
//	└── app_test.go        # app 启动命令用例
//
// 对应 Python 代码：jiuwenswarm/channels/acp/app_acp.py（run_acp）
// 与 tests/system_tests/test_acp_cli_stdio.py
package cmd

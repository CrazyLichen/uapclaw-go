// Package main 提供 uapclaw-process 命令入口。
//
// 这是一个与 uapclaw 并列的独立二进制，用于单 Agent 的一次性/交互式执行。
// 它对应上游的两个 console script 中的第二个：
//
//	jiuwenswarm         = jiuwenswarm.channels.cli.main:main
//	jiuwenswarm-process = jiuwenswarm.channels.process_cli.main:main
//
// 参数解析与业务流程全部在 internal/swarm/channels/processcli 中，
// 本包只负责入口（规则 4：main.go 加 build tag 只放 Execute()+main()，
// 可测试逻辑放 cmd.go）。
//
// 文件目录：
//
//	uapclaw-process/
//	├── doc.go    # 包文档
//	├── main.go   # //go:build !test，仅 main()
//	├── cmd.go    # Execute() 可测入口
//	└── cmd_test.go
//
// 对应 Python 代码：jiuwenswarm/channels/process_cli/main.py（console script 入口）
package main

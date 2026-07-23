// Package main 提供 jiuwenbox 沙箱系统 CLI 入口。
//
// JiuwenBox 是独立的沙箱系统，提供进程隔离、策略引擎和推理隐私代理。
// 沙箱系统不依赖 agentcore 或 swarm，拥有独立的 CLI 入口。
//
// 文件目录：
//
//	jiuwenbox/
//	├── doc.go    # 包文档
//	├── cmd.go    # 命令定义：newRootCmd / newServeCmd / newRunCmd
//	└── main.go   # Execute() + main()（//go:build !test 隔离，不参与覆盖率）
//
// 对应 Python 代码：无（Go 特有 CLI 入口）
package main

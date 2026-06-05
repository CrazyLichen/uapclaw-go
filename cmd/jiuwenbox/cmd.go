// Package main 提供 jiuwenbox 沙箱系统命令定义。
//
// 可测试的命令构建逻辑（newRootCmd / newServeCmd / newRunCmd）放在此文件，
// main.go 中的 Execute() + main() 用 //go:build !test 隔离，不参与单元测试覆盖率统计。
package main

import (
	"fmt"

	"github.com/spf13/cobra"
	"github.com/uapclaw/uapclaw-go/internal/common/version"
)

// ──────────────────────────── 非导出函数 ────────────────────────────

// newRootCmd 创建根命令
func newRootCmd() *cobra.Command {
	rootCmd := &cobra.Command{
		Use:   "jiuwenbox",
		Short: "JiuwenBox — 沙箱系统命令行工具",
		Long: `JiuwenBox 是独立的沙箱系统，提供进程隔离、策略引擎和推理隐私代理。
沙箱系统不依赖 agentcore 或 swarm，拥有独立的 CLI 入口。`,
		Version: version.Version,
	}

	// 子命令注册
	rootCmd.AddCommand(
		newServeCmd(),
		newRunCmd(),
	)

	return rootCmd
}

// newServeCmd 创建 serve 子命令
func newServeCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "serve",
		Short: "启动沙箱 HTTP API 服务",
		Long:  `启动沙箱 HTTP API 服务，提供沙箱管理、代理和策略接口。`,
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Println("serve 模式尚未实现（领域十二）")
		},
	}
}

// newRunCmd 创建 run 子命令
func newRunCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "run [命令]",
		Short: "在沙箱中运行指定命令",
		Long:  `在沙箱隔离环境中运行指定命令，受策略引擎约束。`,
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Println("run 模式尚未实现（领域十二）")
		},
	}
}

//go:build !test

// Package main 提供 jiuwenbox 沙箱系统 CLI 入口。
//
// JiuwenBox 是独立的沙箱系统，不依赖 openjiuwen 或 jiuwenswarm。
// 代码位于 internal/swarm/jiuwenbox/，CLI 入口独立。
//
// 支持以下子命令：
//   - serve: 启动沙箱 HTTP API 服务
//   - run: 在沙箱中运行指定命令
package main

import "os"

// ──────────────────────────── 导出函数 ────────────────────────────

// Execute 执行根命令
func Execute() {
	if err := newRootCmd().Execute(); err != nil {
		os.Exit(1)
	}
}

// ──────────────────────────── 非导出函数 ────────────────────────────
func main() {
	Execute()
}

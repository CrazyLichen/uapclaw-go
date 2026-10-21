//go:build !test

// Package main 提供 integration-audit CLI 入口。
package main

import "os"

// ──────────────────────────── 导出函数 ────────────────────────────

// Execute 执行根命令
func Execute() {
	os.Exit(runCLI(os.Args[1:]))
}

// ──────────────────────────── 非导出函数 ────────────────────────────

func main() {
	Execute()
}
